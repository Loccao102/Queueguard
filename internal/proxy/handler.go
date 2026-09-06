package proxy

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
	"github.com/Loccao102/queueguard/internal/queue"
	"github.com/Loccao102/queueguard/internal/ratelimit"
	"github.com/Loccao102/queueguard/internal/web"
)

const (
	CookieSession = "queueguard_session"
	CookieTicket  = "queueguard_ticket"
	HeaderTicket  = "X-QueueGuard-Ticket"
)

// ServerOption configures the QueueGuard reverse proxy server.
type ServerOption func(*Server)

// WithAdminToken sets the secret token required for administrative API actions.
func WithAdminToken(token string) ServerOption {
	return func(s *Server) {
		s.adminToken = token
	}
}

// WithBypassPaths sets custom path and extension rules to bypass the waiting room.
func WithBypassPaths(paths []string) ServerOption {
	return func(s *Server) {
		s.pathMatcher = NewPathMatcher(paths)
	}
}

// WithRateLimiter sets the client IP rate limiter to protect against spam / bot surges.
func WithRateLimiter(limiter *ratelimit.IPRateLimiter) ServerOption {
	return func(s *Server) {
		s.ipLimiter = limiter
	}
}

// WithDeviceBinding toggles whether tickets are cryptographically locked to the client's device fingerprint.
func WithDeviceBinding(enabled bool) ServerOption {
	return func(s *Server) {
		s.bindDevice = enabled
	}
}

// WithPoWDifficulty sets the proof-of-work challenge difficulty (0 = disabled, 3-5 = active).
func WithPoWDifficulty(diff int) ServerOption {
	return func(s *Server) {
		s.powDifficulty = diff
	}
}

// Server represents the QueueGuard Reverse Proxy & Traffic Shaper engine.
type Server struct {
	targetURL        *url.URL
	reverseProxy     *httputil.ReverseProxy
	waitingRoom      *queue.WaitingRoom
	signer           *crypto.Signer
	htmlContent      []byte
	adminHTMLContent []byte
	adminToken       string
	pathMatcher      *PathMatcher
	ipLimiter        *ratelimit.IPRateLimiter
	startTime        time.Time
	bindDevice       bool
	powDifficulty    int
	powController    *crypto.PoWController

	// Internal metrics
	metricsTotalRequests    uint64
	metricsBypassedRequests uint64
	metricsAdmittedRequests uint64
	metricsLimitedRequests  uint64
}

// NewServer initializes a new QueueGuard Reverse Proxy pointing to targetOrigin.
func NewServer(targetOrigin string, wr *queue.WaitingRoom, signer *crypto.Signer, opts ...ServerOption) (*Server, error) {
	originURL, err := url.Parse(targetOrigin)
	if err != nil {
		return nil, fmt.Errorf("invalid origin target URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(originURL)

	// Customize director to maintain host headers correctly
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = originURL.Host
	}

	s := &Server{
		targetURL:        originURL,
		reverseProxy:     proxy,
		waitingRoom:      wr,
		signer:           signer,
		htmlContent:      web.WaitingRoomHTML,
		adminHTMLContent: web.AdminHTML,
		adminToken:       "queueguard-admin-secret",
		pathMatcher:      NewPathMatcher(nil),
		startTime:        time.Now(),
		bindDevice:       true,
		powDifficulty:    0,
		powController:    crypto.NewPoWController("queueguard-pow-internal-secret", 5*time.Minute),
	}

	for _, opt := range opts {
		opt(s)
	}

	// Customize response to handle dynamic sliding ticket extension (X-QueueGuard-Extend) from Origin
	proxy.ModifyResponse = func(resp *http.Response) error {
		if extendVal := resp.Header.Get("X-QueueGuard-Extend"); extendVal != "" {
			if duration, err := time.ParseDuration(extendVal); err == nil && duration > 0 {
				req := resp.Request
				if cookie, err := req.Cookie(CookieTicket); err == nil && cookie.Value != "" {
					var devHash string
					if s.bindDevice {
						devHash = crypto.ComputeDeviceFingerprint(req.UserAgent(), ratelimit.ExtractIP(req))
					}
					if ticket, err := s.signer.VerifyWithDevice(cookie.Value, devHash); err == nil {
						newToken, _, err := s.signer.IssueTTL(ticket.SessionID, ticket.QueueNumber, devHash, duration)
						if err == nil {
							newCookie := &http.Cookie{
								Name:     CookieTicket,
								Value:    newToken,
								Path:     "/",
								MaxAge:   int(duration.Seconds()),
								HttpOnly: false,
								SameSite: http.SameSiteLaxMode,
							}
							resp.Header.Add("Set-Cookie", newCookie.String())
						}
					}
				}
			}
		}
		return nil
	}

	return s, nil
}

// ServeHTTP inspects incoming traffic, enforces waiting room turnstiles, and proxies admitted requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	atomic.AddUint64(&s.metricsTotalRequests, 1)

	// 1. Healthcheck endpoint
	if r.URL.Path == "/queueguard/healthz" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "time": time.Now()})
		return
	}

	// 2. Metrics endpoint
	if r.URL.Path == "/queueguard/metrics" || r.URL.Path == "/metrics" {
		s.handleMetrics(w, r)
		return
	}

	// 3. PoW Challenge endpoint
	if r.URL.Path == "/queueguard/pow/challenge" {
		diff := s.powDifficulty
		if diff <= 0 {
			diff = 3
		}
		ch, token := s.powController.Generate(diff)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"challenge":  token,
			"seed":       ch.Seed,
			"difficulty": ch.Difficulty,
			"expires_at": ch.ExpiresAt,
		})
		return
	}

	// 4. Admin endpoints (Dashboard UI & Control API)
	if r.URL.Path == "/queueguard/admin" || strings.HasPrefix(r.URL.Path, "/queueguard/api/admin/") {
		s.handleAdmin(w, r)
		return
	}

	// 5. SSE position stream
	if r.URL.Path == "/queueguard/sse" {
		s.handleSSE(w, r)
		return
	}

	// 6. Status query endpoint
	if r.URL.Path == "/queueguard/status" {
		s.handleStatus(w, r)
		return
	}

	// 7. Check path whitelist / static assets
	if s.pathMatcher != nil && s.pathMatcher.ShouldBypass(r.URL.Path) {
		atomic.AddUint64(&s.metricsBypassedRequests, 1)
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// 8. Check if bypass mode is actively enabled
	if s.waitingRoom.IsBypass() {
		atomic.AddUint64(&s.metricsBypassedRequests, 1)
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// 9. Check if request holds a cryptographically valid admission ticket
	if s.hasValidTicket(r) {
		atomic.AddUint64(&s.metricsAdmittedRequests, 1)
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// 10. Client has no ticket. Check IP rate limit for new queue enrollments
	clientIP := ratelimit.ExtractIP(r)
	if s.ipLimiter != nil && !s.ipLimiter.Allow(clientIP) {
		atomic.AddUint64(&s.metricsLimitedRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "rate_limited",
			"message": "Too many queue enrollment requests from this IP. Please wait a moment.",
		})
		return
	}

	// 11. Check Proof-of-Work Challenge if active
	if s.powDifficulty > 0 {
		powToken := r.Header.Get("X-QueueGuard-PoW-Token")
		powNonce := r.Header.Get("X-QueueGuard-PoW-Nonce")
		if powToken == "" || powNonce == "" {
			powToken = r.URL.Query().Get("pow_token")
			powNonce = r.URL.Query().Get("pow_nonce")
		}

		if powToken == "" || powNonce == "" || s.powController.Verify(powToken, powNonce) != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":     "pow_required",
				"message":   "Proof of work challenge required to enter queue",
				"challenge": "/queueguard/pow/challenge",
			})
			return
		}
	}

	// 12. User has no ticket. Identify session
	sessionID := s.getOrCreateSessionID(w, r)

	// Enroll session in waiting room (idempotent across F5)
	sess, _ := s.waitingRoom.Enroll(sessionID)

	// Check if turnstile has already reached this ticket number
	admitted, pos, estSec, token, err := s.waitingRoom.CheckStatus(sessionID)
	if err == nil && admitted && token != "" {
		if s.bindDevice {
			devHash := crypto.ComputeDeviceFingerprint(r.UserAgent(), clientIP)
			if tokenWithDev, _, err := s.signer.IssueWithDevice(sessionID, sess.TicketNumber, devHash); err == nil {
				token = tokenWithDev
			}
		}
		// User is admitted! Set ticket cookie and forward to origin
		http.SetCookie(w, &http.Cookie{
			Name:     CookieTicket,
			Value:    token,
			Path:     "/",
			MaxAge:   600, // 10 minutes
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
		atomic.AddUint64(&s.metricsAdmittedRequests, 1)
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// User must wait in line. Determine client type (API vs Browser Navigation)
	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "application/json") && !strings.Contains(acceptHeader, "text/html") {
		// API client receives structured 429 response
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", estSec))
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":         "queued",
			"ticket_number": sess.TicketNumber,
			"position":      pos,
			"est_seconds":   estSec,
			"session_id":    sessionID,
		})
		return
	}

	// Browser client receives the live virtual waiting room webpage
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.htmlContent)
}

func (s *Server) hasValidTicket(r *http.Request) bool {
	sessionCookie, err := r.Cookie(CookieSession)
	if err != nil || sessionCookie.Value == "" {
		return false
	}

	var devHash string
	if s.bindDevice {
		devHash = crypto.ComputeDeviceFingerprint(r.UserAgent(), ratelimit.ExtractIP(r))
	}

	for _, token := range []string{
		cookieValue(r, CookieTicket),
		r.Header.Get(HeaderTicket),
	} {
		if token == "" {
			continue
		}
		ticket, err := s.signer.VerifyWithDevice(token, devHash)
		if err == nil && subtle.ConstantTimeCompare([]byte(ticket.SessionID), []byte(sessionCookie.Value)) == 1 {
			return true
		}
	}

	return false
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (s *Server) getOrCreateSessionID(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(CookieSession); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// Generate random session ID
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	sessionID := hex.EncodeToString(b)

	http.SetCookie(w, &http.Cookie{
		Name:     CookieSession,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400, // 24 hours
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return sessionID
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		if cookie, err := r.Cookie(CookieSession); err == nil {
			sessionID = cookie.Value
		}
	}

	w.Header().Set("Content-Type", "application/json")

	if sessionID == "" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"queue_depth": s.waitingRoom.Sequence().QueueDepth(),
			"last_issued": s.waitingRoom.Sequence().LastIssued(),
			"admitted":    s.waitingRoom.Sequence().Admitted(),
			"rate":        s.waitingRoom.GetDischargeRate(),
			"paused":      s.waitingRoom.IsPaused(),
			"bypass":      s.waitingRoom.IsBypass(),
		})
		return
	}

	admitted, pos, estSec, _, err := s.waitingRoom.CheckStatus(sessionID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id":  sessionID,
		"admitted":    admitted,
		"position":    pos,
		"est_seconds": estSec,
	})
}
