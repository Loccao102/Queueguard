package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidPoWChallenge = errors.New("invalid pow challenge token")
	ErrPoWExpired          = errors.New("pow challenge has expired")
	ErrPoWFailed           = errors.New("pow solution does not meet required difficulty")
)

// PoWChallenge represents a cryptographic puzzle issued to a client.
type PoWChallenge struct {
	Seed       string `json:"seed"`
	Difficulty int    `json:"difficulty"`
	ExpiresAt  int64  `json:"expires_at"`
	Signature  string `json:"signature"`
}

// PoWController handles generation and validation of stateless proof-of-work puzzles.
type PoWController struct {
	secretKey []byte
	ttl       time.Duration
}

// NewPoWController initializes a PoW controller with a secret key and TTL.
func NewPoWController(secretKey string, ttl time.Duration) *PoWController {
	if ttl <= 0 {
		ttl = 3 * time.Minute
	}
	return &PoWController{
		secretKey: []byte(secretKey),
		ttl:       ttl,
	}
}

// Generate creates a signed PoW puzzle with a given difficulty (e.g., 3 = "000", 4 = "0000").
func (p *PoWController) Generate(difficulty int) (*PoWChallenge, string) {
	if difficulty <= 0 {
		difficulty = 3
	}

	b := make([]byte, 16)
	_, _ = rand.Read(b)
	seed := hex.EncodeToString(b)
	expiresAt := time.Now().Add(p.ttl).UnixMilli()

	sig := p.sign(seed, difficulty, expiresAt)
	challenge := &PoWChallenge{
		Seed:       seed,
		Difficulty: difficulty,
		ExpiresAt:  expiresAt,
		Signature:  sig,
	}

	// Encoded string token: seed.difficulty.expiresAt.sig
	token := fmt.Sprintf("%s.%d.%d.%s", seed, difficulty, expiresAt, sig)
	return challenge, token
}

// Verify checks whether the client's proposed candidate nonce solves the puzzle.
func (p *PoWController) Verify(challengeToken string, solutionNonce string) error {
	parts := strings.Split(challengeToken, ".")
	if len(parts) != 4 {
		return ErrInvalidPoWChallenge
	}

	seed := parts[0]
	difficulty, err := strconv.Atoi(parts[1])
	if err != nil || difficulty <= 0 {
		return ErrInvalidPoWChallenge
	}

	expiresAt, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return ErrInvalidPoWChallenge
	}
	signature := parts[3]

	// 1. Verify HMAC signature
	expectedSig := p.sign(seed, difficulty, expiresAt)
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return ErrInvalidPoWChallenge
	}

	// 2. Verify expiration
	if time.Now().UnixMilli() > expiresAt {
		return ErrPoWExpired
	}

	// 3. Verify solution meets difficulty
	targetPrefix := strings.Repeat("0", difficulty)
	hash := sha256.Sum256([]byte(seed + solutionNonce))
	hashHex := hex.EncodeToString(hash[:])

	if !strings.HasPrefix(hashHex, targetPrefix) {
		return ErrPoWFailed
	}

	return nil
}

// Solve is a helper to solve a puzzle (useful for client SDKs and testing).
func SolvePoW(seed string, difficulty int) string {
	targetPrefix := strings.Repeat("0", difficulty)
	for i := uint64(0); ; i++ {
		nonce := strconv.FormatUint(i, 10)
		hash := sha256.Sum256([]byte(seed + nonce))
		if strings.HasPrefix(hex.EncodeToString(hash[:]), targetPrefix) {
			return nonce
		}
	}
}

func (p *PoWController) sign(seed string, difficulty int, expiresAt int64) string {
	data := fmt.Sprintf("%s:%d:%d", seed, difficulty, expiresAt)
	h := hmac.New(sha256.New, p.secretKey)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))[:16]
}
