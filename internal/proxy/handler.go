package proxy

import (
	_ "embed"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
	"github.com/Loccao102/queueguard/internal/queue"
	"github.com/Loccao102/queueguard/internal/web"
)

const (
	CookieSession = "queueguard_session"
	CookieTicket  = "queueguard_ticket"
	HeaderTicket  = "X-QueueGuard-Ticket"
)

// Server represents the QueueGuard Reverse Proxy & Traffic Shaper engine.
type Server struct {
	targetURL    *url.URL
	reverseProxy *httputil.ReverseProxy
	waitingRoom  *queue.WaitingRoom
	signer       *crypto.Signer
	htmlContent  []byte
}

// NewServer initializes a new QueueGuard Reverse Proxy pointing to targetOrigin.
func NewServer(targetOrigin string, wr *queue.WaitingRoom, signer *crypto.Signer) (*Server, error) {
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

	return &Server{
		targetURL:    originURL,
		reverseProxy: proxy,
		waitingRoom:  wr,
		signer:       signer,
		htmlContent:  web.WaitingRoomHTML,
	}, nil
}

// ServeHTTP inspects incoming traffic, enforces waiting room turnstiles, and proxies admitted requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Healthcheck endpoint
	if r.URL.Path == "/queueguard/healthz" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "time": time.Now()})
		return
	}

	// 2. SSE position stream
	if r.URL.Path == "/queueguard/sse" {
		s.handleSSE(w, r)
		return
	}

	// 3. Status query endpoint
	if r.URL.Path == "/queueguard/status" {
		s.handleStatus(w, r)
		return
	}

	// 4. Check if bypass mode is actively enabled
	if s.waitingRoom.IsBypass() {
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// 5. Check if request holds a cryptographically valid admission ticket
	if s.hasValidTicket(r) {
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// 6. User has no ticket. Identify session
	sessionID := s.getOrCreateSessionID(w, r)

	// Enroll session in waiting room (idempotent across F5)
	sess, _ := s.waitingRoom.Enroll(sessionID)

	// Check if turnstile has already reached this ticket number
	admitted, pos, estSec, token, err := s.waitingRoom.CheckStatus(sessionID)
	if err == nil && admitted && token != "" {
		// User is admitted! Set ticket cookie and forward to origin
		http.SetCookie(w, &http.Cookie{
			Name:     CookieTicket,
			Value:    token,
			Path:     "/",
			MaxAge:   600, // 10 minutes
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
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
	// Check Cookie
	if cookie, err := r.Cookie(CookieTicket); err == nil && cookie.Value != "" {
		if _, err := s.signer.Verify(cookie.Value); err == nil {
			return true
		}
	}

	// Check Header
	if headerVal := r.Header.Get(HeaderTicket); headerVal != "" {
		if _, err := s.signer.Verify(headerVal); err == nil {
			return true
		}
	}

	return false
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
		HttpOnly: false,
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

	admitted, pos, estSec, token, err := s.waitingRoom.CheckStatus(sessionID)
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
		"token":       token,
	})
}
