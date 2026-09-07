package queueguard

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
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

// AdmissionTicket holds the decoded ticket claims.
type AdmissionTicket struct {
	SessionID   string `json:"sid"`
	QueueNumber uint64 `json:"qnum"`
	RoomID      string `json:"rid,omitempty"`
	DeviceHash  string `json:"dev,omitempty"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

type contextKey struct{}

var ticketContextKey = contextKey{}

// FromContext extracts the validated AdmissionTicket from the request context.
func FromContext(ctx context.Context) (*AdmissionTicket, bool) {
	ticket, ok := ctx.Value(ticketContextKey).(*AdmissionTicket)
	return ticket, ok
}

// Option configures the QueueGuard middleware.
type Option func(*middlewareConfig)

type middlewareConfig struct {
	roomID          string
	bindDevice      bool
	redirectURL     string
	ticketCookie    string
	sessionCookie   string
	ticketHeader    string
}

// WithRoom requires tickets to match a specific room ID.
func WithRoom(roomID string) Option {
	return func(c *middlewareConfig) {
		c.roomID = roomID
	}
}

// WithDeviceBinding enforces client IP & User-Agent fingerprint matching.
func WithDeviceBinding(enabled bool) Option {
	return func(c *middlewareConfig) {
		c.bindDevice = enabled
	}
}

// WithRedirectURL redirects unadmitted requests to this URL (e.g. QueueGuard waiting room).
func WithRedirectURL(url string) Option {
	return func(c *middlewareConfig) {
		c.redirectURL = url
	}
}

// ComputeDeviceFingerprint hashes User-Agent and client IP to produce a fingerprint.
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

// ExtractIP extracts real client IP from request headers or RemoteAddr.
func ExtractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// VerifyTicket validates a token string against the secret key and optional claims.
func VerifyTicket(tokenStr, secretKey, expectedDeviceHash, expectedRoomID string) (*AdmissionTicket, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidTicket
	}

	payloadB64, signature := parts[0], parts[1]

	// Compute expected signature
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(payloadB64))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
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

// New constructs standard net/http middleware protecting endpoints with QueueGuard ticket validation.
func New(secretKey string, opts ...Option) func(http.Handler) http.Handler {
	cfg := &middlewareConfig{
		bindDevice:    true,
		ticketCookie:  "queueguard_ticket",
		sessionCookie: "queueguard_session",
		ticketHeader:  "X-QueueGuard-Ticket",
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var token string
			if cookie, err := r.Cookie(cfg.ticketCookie); err == nil && cookie.Value != "" {
				token = cookie.Value
			} else if hdr := r.Header.Get(cfg.ticketHeader); hdr != "" {
				token = hdr
			}

			if token == "" {
				handleUnauthorized(w, r, cfg)
				return
			}

			var devHash string
			if cfg.bindDevice {
				devHash = ComputeDeviceFingerprint(r.UserAgent(), ExtractIP(r))
			}

			ticket, err := VerifyTicket(token, secretKey, devHash, cfg.roomID)
			if err != nil {
				handleUnauthorized(w, r, cfg)
				return
			}

			// If session cookie exists, verify session ID match
			if sessCookie, err := r.Cookie(cfg.sessionCookie); err == nil && sessCookie.Value != "" {
				if subtle.ConstantTimeCompare([]byte(ticket.SessionID), []byte(sessCookie.Value)) != 1 {
					handleUnauthorized(w, r, cfg)
					return
				}
			}

			// Inject ticket into request context
			ctx := context.WithValue(r.Context(), ticketContextKey, ticket)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func handleUnauthorized(w http.ResponseWriter, r *http.Request, cfg *middlewareConfig) {
	if cfg.redirectURL != "" {
		http.Redirect(w, r, cfg.redirectURL, http.StatusFound)
		return
	}

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "unauthorized",
			"message": "Valid QueueGuard admission ticket required to access this resource",
		})
		return
	}

	http.Error(w, "Access Denied: Valid QueueGuard admission ticket required", http.StatusUnauthorized)
}
