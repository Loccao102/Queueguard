package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
)

func TestConcurrentEnrollmentUniqueness(t *testing.T) {
	signer := crypto.NewSigner("test-secret", 5*time.Minute)
	wr := NewWaitingRoom(DefaultConfig(), signer)

	numGoroutines := 1000
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	ticketSet := sync.Map{}

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			sessID := fmt.Sprintf("user_%d", id)
			sess, isNew := wr.Enroll(sessID)
			if !isNew {
				t.Errorf("expected isNew to be true for unique user %s", sessID)
			}
			if _, exists := ticketSet.LoadOrStore(sess.TicketNumber, true); exists {
				t.Errorf("duplicate ticket detected: %d", sess.TicketNumber)
			}
		}(i)
	}

	wg.Wait()

	if wr.sequence.LastIssued() != uint64(numGoroutines) {
		t.Fatalf("expected last issued ticket to be %d, got %d", numGoroutines, wr.sequence.LastIssued())
	}
}

func TestF5RefreshIdempotency(t *testing.T) {
	signer := crypto.NewSigner("test-secret", 5*time.Minute)
	wr := NewWaitingRoom(DefaultConfig(), signer)

	sessID := "browser_tab_1"
	sess1, isNew1 := wr.Enroll(sessID)
	if !isNew1 {
		t.Fatal("expected first enrollment to be new")
	}

	// Client refreshes (F5)
	sess2, isNew2 := wr.Enroll(sessID)
	if isNew2 {
		t.Fatal("expected refresh to recognize existing session")
	}
	if sess1.TicketNumber != sess2.TicketNumber {
		t.Fatalf("expected ticket number to stay unchanged after refresh: %d vs %d", sess1.TicketNumber, sess2.TicketNumber)
	}
}

func TestDischargeAndAdmissionFlow(t *testing.T) {
	signer := crypto.NewSigner("test-secret", 5*time.Minute)
	cfg := DefaultConfig()
	cfg.DischargeRatePerSec = 10
	wr := NewWaitingRoom(cfg, signer)

	// Enroll 25 users
	for i := 1; i <= 25; i++ {
		wr.Enroll(fmt.Sprintf("user_%d", i))
	}

	// Initially, admitted = 0
	admitted, pos, _, _, err := wr.CheckStatus("user_1")
	if err != nil || admitted || pos != 1 {
		t.Fatalf("expected user_1 to be waiting at pos 1, got admitted=%v pos=%d", admitted, pos)
	}

	// Advance admission by 10 tickets
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wr.sequence.AdvanceAdmission(10)

	// user_1 to user_10 should now be admitted!
	admitted, pos, _, token, err := wr.CheckStatus("user_1")
	if err != nil || !admitted || pos != 0 || token == "" {
		t.Fatalf("expected user_1 to be admitted with token, got admitted=%v pos=%d err=%v", admitted, pos, err)
	}

	// Verify the issued token
	ticket, err := signer.Verify(token)
	if err != nil || ticket.SessionID != "user_1" || ticket.QueueNumber != 1 {
		t.Fatalf("invalid admission ticket: %+v, err=%v", ticket, err)
	}

	// user_15 should still be waiting (admitted=10, ticket=15, pos=5)
	admitted15, pos15, est15, _, _ := wr.CheckStatus("user_15")
	if admitted15 || pos15 != 5 || est15 <= 0 {
		t.Fatalf("expected user_15 to be at pos 5, got admitted=%v pos=%d est=%d", admitted15, pos15, est15)
	}
	_ = ctx
}

func TestBypassAndPauseModes(t *testing.T) {
	signer := crypto.NewSigner("test-secret", 5*time.Minute)
	wr := NewWaitingRoom(DefaultConfig(), signer)

	wr.Enroll("user_paused")

	// Set Paused
	wr.SetPaused(true)
	wr.sequence.AdvanceAdmission(10) // even if turnstile advances, paused blocks admission
	admitted, _, _, _, _ := wr.CheckStatus("user_paused")
	if admitted {
		t.Fatal("expected admission to be blocked when waiting room is paused")
	}

	// Set Bypass
	wr.SetPaused(false)
	wr.SetBypass(true)
	admittedBypass, _, _, token, err := wr.CheckStatus("user_paused")
	if err != nil || !admittedBypass || token == "" {
		t.Fatalf("expected bypass mode to grant immediate admission, got admitted=%v err=%v", admittedBypass, err)
	}
}
