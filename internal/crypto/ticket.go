package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidTicket   = errors.New("invalid admission ticket format")
	ErrSignatureFailed = errors.New("ticket signature verification failed")
	ErrTicketExpired   = errors.New("admission ticket has expired")
	ErrDeviceMismatch  = errors.New("admission ticket device fingerprint mismatch")
	ErrRoomMismatch    = errors.New("admission ticket room ID mismatch")
)

// AdmissionTicket represents a cryptographically verifiable pass granted to a client
// who has successfully waited through the virtual waiting room.
type AdmissionTicket struct {
	SessionID   string `json:"sid"`
	QueueNumber uint64 `json:"qnum"`
	RoomID      string `json:"rid,omitempty"`
	DeviceHash  string `json:"dev,omitempty"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

// ComputeDeviceFingerprint hashes User-Agent and client IP to produce a compact device fingerprint.
func ComputeDeviceFingerprint(userAgent, clientIP string) string {
	if userAgent == "" && clientIP == "" {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(userAgent))
	h.Write([]byte("|"))
	h.Write([]byte(clientIP))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Signer handles generating and validating admission tickets with HMAC-SHA256.
type Signer struct {
	secretKey []byte
	ttl       time.Duration
}

// NewSigner creates a new ticket signer with a secret key and TTL duration.
func NewSigner(secretKey string, ttl time.Duration) *Signer {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &Signer{
		secretKey: []byte(secretKey),
		ttl:       ttl,
	}
}

// Issue generates a signed admission ticket token string for a session.
func (s *Signer) Issue(sessionID string, queueNum uint64) (string, *AdmissionTicket, error) {
	return s.IssueWithDevice(sessionID, queueNum, "")
}

// IssueWithDevice generates a signed admission ticket token bound to a device fingerprint.
func (s *Signer) IssueWithDevice(sessionID string, queueNum uint64, deviceHash string) (string, *AdmissionTicket, error) {
	return s.IssueTTL(sessionID, queueNum, deviceHash, s.ttl)
}

// IssueWithRoom generates a signed admission ticket token bound to a device fingerprint and room ID.
func (s *Signer) IssueWithRoom(sessionID string, queueNum uint64, deviceHash, roomID string) (string, *AdmissionTicket, error) {
	return s.IssueRoomTTL(sessionID, queueNum, deviceHash, roomID, s.ttl)
}

// IssueTTL generates a signed admission ticket token with a custom TTL duration.
func (s *Signer) IssueTTL(sessionID string, queueNum uint64, deviceHash string, customTTL time.Duration) (string, *AdmissionTicket, error) {
	return s.IssueRoomTTL(sessionID, queueNum, deviceHash, "", customTTL)
}

// IssueRoomTTL generates a signed admission ticket token with room ID and custom TTL.
func (s *Signer) IssueRoomTTL(sessionID string, queueNum uint64, deviceHash, roomID string, customTTL time.Duration) (string, *AdmissionTicket, error) {
	if customTTL <= 0 {
		customTTL = s.ttl
	}
	now := time.Now()
	ticket := &AdmissionTicket{
		SessionID:   sessionID,
		QueueNumber: queueNum,
		RoomID:      roomID,
		DeviceHash:  deviceHash,
		IssuedAt:    now.UnixMilli(),
		ExpiresAt:   now.Add(customTTL).UnixMilli(),
	}

	payloadBytes, err := json.Marshal(ticket)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal ticket: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature := s.sign(payloadB64)

	token := fmt.Sprintf("%s.%s", payloadB64, signature)
	return token, ticket, nil
}

// Verify validates a signed ticket token string.
func (s *Signer) Verify(token string) (*AdmissionTicket, error) {
	return s.VerifyWithDevice(token, "")
}

// VerifyWithDevice validates a signed ticket token and verifies matching device fingerprint.
func (s *Signer) VerifyWithDevice(token string, expectedDeviceHash string) (*AdmissionTicket, error) {
	return s.VerifyWithRoom(token, expectedDeviceHash, "")
}

// VerifyWithRoom validates a signed ticket token and verifies matching device fingerprint and room ID.
func (s *Signer) VerifyWithRoom(token string, expectedDeviceHash string, expectedRoomID string) (*AdmissionTicket, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidTicket
	}

	payloadB64, signature := parts[0], parts[1]

	expectedSignature := s.sign(payloadB64)
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return nil, ErrSignatureFailed
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, ErrInvalidTicket
	}

	var ticket AdmissionTicket
	if err := json.Unmarshal(payloadBytes, &ticket); err != nil {
		return nil, ErrInvalidTicket
	}

	if time.Now().UnixMilli() > ticket.ExpiresAt {
		return nil, ErrTicketExpired
	}

	if expectedDeviceHash != "" && ticket.DeviceHash != "" && ticket.DeviceHash != expectedDeviceHash {
		return nil, ErrDeviceMismatch
	}

	if expectedRoomID != "" && ticket.RoomID != "" && ticket.RoomID != expectedRoomID {
		return nil, ErrRoomMismatch
	}

	return &ticket, nil
}

func (s *Signer) sign(data string) string {
	h := hmac.New(sha256.New, s.secretKey)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
