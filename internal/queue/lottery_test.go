package queue

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPreQueueLifecycle(t *testing.T) {
	// Set start time 150ms in the future
	startTime := time.Now().Add(150 * time.Millisecond)
	pq := NewPreQueueManager(startTime)

	if !pq.IsActive() {
		t.Fatal("expected pre-queue to be active before start time")
	}

	sessions := &sync.Map{}
	numUsers := 50

	// Enroll 50 users into pre-queue
	for i := 0; i < numUsers; i++ {
		sID := fmt.Sprintf("pre_user_%d", i)
		sess := &Session{
			ID:           sID,
			TicketNumber: 0, // initially 0 in pre-queue
			Status:       StatusWaiting,
		}
		sessions.Store(sID, sess)
		pq.Enroll(sID)
	}

	if pq.Count() != numUsers {
		t.Fatalf("expected %d users in pre-queue, got %d", numUsers, pq.Count())
	}

	seq := NewSequenceController()

	// Wait until event start time
	time.Sleep(160 * time.Millisecond)

	if !pq.ShouldTriggerLottery() {
		t.Fatal("expected ShouldTriggerLottery to be true after start time passed")
	}

	// Execute lottery shuffle
	shuffledCount := pq.ExecuteFairLottery(seq, sessions)
	if shuffledCount != numUsers {
		t.Fatalf("expected %d shuffled users, got %d", numUsers, shuffledCount)
	}

	if seq.LastIssued() != uint64(numUsers) {
		t.Fatalf("expected lastIssued to be %d, got %d", numUsers, seq.LastIssued())
	}

	// Verify every user has a unique ticket between 1 and 50
	seenTickets := make(map[uint64]string)
	for i := 0; i < numUsers; i++ {
		sID := fmt.Sprintf("pre_user_%d", i)
		val, ok := sessions.Load(sID)
		if !ok {
			t.Fatalf("session %s missing", sID)
		}
		sess := val.(*Session)
		if sess.TicketNumber == 0 || sess.TicketNumber > uint64(numUsers) {
			t.Fatalf("invalid ticket number %d for user %s", sess.TicketNumber, sID)
		}
		if prev, exists := seenTickets[sess.TicketNumber]; exists {
			t.Fatalf("duplicate ticket %d detected between %s and %s", sess.TicketNumber, prev, sID)
		}
		seenTickets[sess.TicketNumber] = sID
	}

	// Next normal ticket should be 51
	nextTicket := seq.NextTicket()
	if nextTicket != uint64(numUsers+1) {
		t.Fatalf("expected next ticket to be %d, got %d", numUsers+1, nextTicket)
	}
}
