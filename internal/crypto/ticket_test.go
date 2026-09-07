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

func TestDeviceBinding(t *testing.T) {
	signer := NewSigner("secret-key", 5*time.Minute)

	ua1 := "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
	ip1 := "192.168.1.100"
	devHash1 := ComputeDeviceFingerprint(ua1, ip1)

	token, _, err := signer.IssueWithDevice("sess_dev_1", 1, devHash1)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Verify with matching device -> should PASS
	verified, err := signer.VerifyWithDevice(token, devHash1)
	if err != nil || verified == nil {
		t.Fatalf("expected matching device to pass, got err=%v", err)
	}

	// 2. Verify with different device / IP (scalper attempting to sell cookie) -> should FAIL
	ua2 := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)"
	ip2 := "203.0.113.50"
	devHash2 := ComputeDeviceFingerprint(ua2, ip2)

	_, err = signer.VerifyWithDevice(token, devHash2)
	if err != ErrDeviceMismatch {
		t.Fatalf("expected ErrDeviceMismatch for transferred ticket, got: %v", err)
	}
}

func TestRoomBinding(t *testing.T) {
	signer := NewSigner("secret-key", 5*time.Minute)

	tokenVIP, _, err := signer.IssueWithRoom("sess_user", 10, "dev123", "room-vip")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Verifying against same room -> PASS
	verified, err := signer.VerifyWithRoom(tokenVIP, "dev123", "room-vip")
	if err != nil || verified == nil || verified.RoomID != "room-vip" {
		t.Fatalf("expected successful verification for room-vip, got err=%v", err)
	}

	// 2. Verifying against different room (e.g. VIP ticket used for General or vice versa) -> FAIL
	_, err = signer.VerifyWithRoom(tokenVIP, "dev123", "room-general")
	if err != ErrRoomMismatch {
		t.Fatalf("expected ErrRoomMismatch, got err=%v", err)
	}

	// 3. Verifying with empty expected room (wildcard / fallback) -> PASS
	_, err = signer.VerifyWithRoom(tokenVIP, "dev123", "")
	if err != nil {
		t.Fatalf("expected empty room verification to pass, got err=%v", err)
	}
}
