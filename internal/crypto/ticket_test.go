package crypto

import (
	"testing"
	"time"
)

func TestTicketIssueAndVerify(t *testing.T) {
	secret := "super-secret-key-12345"
	signer := NewSigner(secret, 5*time.Second)

	token, ticket, err := signer.Issue("sess_abc123", 42)
	if err != nil {
		t.Fatalf("failed to issue ticket: %v", err)
	}

	if ticket.SessionID != "sess_abc123" || ticket.QueueNumber != 42 {
		t.Fatalf("unexpected ticket payload: %+v", ticket)
	}

	// Verify valid token
	verified, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("expected token to verify successfully, got: %v", err)
	}
	if verified.SessionID != "sess_abc123" || verified.QueueNumber != 42 {
		t.Fatalf("verified payload mismatch: %+v", verified)
	}
}

func TestTamperedTicket(t *testing.T) {
	signer := NewSigner("secret-key", 5*time.Minute)
	token, _, err := signer.Issue("sess_valid", 100)
	if err != nil {
		t.Fatal(err)
	}

	// Tamper signature
	tampered := token + "tampered"
	_, err = signer.Verify(tampered)
	if err == nil {
		t.Fatal("expected signature verification failure for tampered token")
	}

	// Different signer key
	anotherSigner := NewSigner("different-secret-key", 5*time.Minute)
	_, err = anotherSigner.Verify(token)
	if err == nil {
		t.Fatal("expected signature verification failure with different key")
	}
}

func TestExpiredTicket(t *testing.T) {
	// TTL of 1 millisecond
	signer := NewSigner("secret-key", 1*time.Millisecond)
	token, _, err := signer.Issue("sess_expire", 1)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)

	_, err = signer.Verify(token)
	if err != ErrTicketExpired {
		t.Fatalf("expected ErrTicketExpired, got: %v", err)
	}
}
