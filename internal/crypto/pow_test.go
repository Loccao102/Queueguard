package crypto

import (
	"testing"
	"time"
)

func TestPoWController(t *testing.T) {
	pow := NewPoWController("pow-secret-key-123", 1*time.Minute)

	// Generate difficulty 3 puzzle
	challenge, token := pow.Generate(3)
	if challenge.Difficulty != 3 || token == "" {
		t.Fatalf("unexpected challenge: %+v", challenge)
	}

	// Solve the puzzle
	solution := SolvePoW(challenge.Seed, challenge.Difficulty)
	if solution == "" {
		t.Fatal("expected valid solution")
	}

	// 1. Verify correct solution -> should PASS
	if err := pow.Verify(token, solution); err != nil {
		t.Fatalf("expected valid solution to pass, got: %v", err)
	}

	// 2. Verify invalid solution -> should FAIL
	invalidSolution := solution + "invalid"
	if err := pow.Verify(token, invalidSolution); err != ErrPoWFailed {
		t.Fatalf("expected ErrPoWFailed for incorrect solution, got: %v", err)
	}

	// 3. Tampered token -> should FAIL
	tamperedToken := token + "tampered"
	if err := pow.Verify(tamperedToken, solution); err != ErrInvalidPoWChallenge {
		t.Fatalf("expected ErrInvalidPoWChallenge for tampered token, got: %v", err)
	}

	// 4. Expired challenge -> should FAIL
	expiredPow := NewPoWController("pow-secret", 1*time.Millisecond)
	_, expiredToken := expiredPow.Generate(2)
	time.Sleep(10 * time.Millisecond)
	if err := expiredPow.Verify(expiredToken, "0"); err != ErrPoWExpired {
		t.Fatalf("expected ErrPoWExpired for expired token, got: %v", err)
	}
}
