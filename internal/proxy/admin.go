package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleAdmin routes requests under /queueguard/admin and /queueguard/api/admin/*.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	// Serve Dashboard UI
	if r.URL.Path == "/queueguard/admin" {
		// If query param token is present, store it as cookie
		if token := r.URL.Query().Get("token"); token != "" && token == s.adminToken {
			http.SetCookie(w, &http.Cookie{
				Name:     "queueguard_admin_token",
				Value:    token,
				Path:     "/queueguard",
				HttpOnly: false,
				SameSite: http.SameSiteLaxMode,
			})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(s.adminHTMLContent)
		return
	}

	// Verify Admin Token for API endpoints
	if !s.authorizeAdmin(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "unauthorized: valid admin token required",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.URL.Path {
	case "/queueguard/api/admin/stats":
		s.handleAdminStats(w, r)
	case "/queueguard/api/admin/pause":
		s.handleAdminPause(w, r)
	case "/queueguard/api/admin/resume":
		s.handleAdminResume(w, r)
	case "/queueguard/api/admin/rate":
		s.handleAdminRate(w, r)
	case "/queueguard/api/admin/bypass":
		s.handleAdminBypass(w, r)
	case "/queueguard/api/admin/reset":
		s.handleAdminReset(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) authorizeAdmin(r *http.Request) bool {
	if s.adminToken == "" {
		return true // No admin token configured
	}

	// 1. Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		if strings.TrimPrefix(authHeader, "Bearer ") == s.adminToken {
			return true
		}
	}

	// 2. X-Admin-Token header
	if r.Header.Get("X-Admin-Token") == s.adminToken {
		return true
	}

	// 3. Query parameter ?token=<token>
	if r.URL.Query().Get("token") == s.adminToken {
		return true
	}

	// 4. Cookie queueguard_admin_token
	if cookie, err := r.Cookie("queueguard_admin_token"); err == nil && cookie.Value == s.adminToken {
		return true
	}

	return false
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"queue_depth":     s.waitingRoom.Sequence().QueueDepth(),
		"last_issued":     s.waitingRoom.Sequence().LastIssued(),
		"admitted":        s.waitingRoom.Sequence().Admitted(),
		"rate":            s.waitingRoom.GetDischargeRate(),
		"paused":          s.waitingRoom.IsPaused(),
		"bypass":          s.waitingRoom.IsBypass(),
		"active_sessions": s.waitingRoom.ActiveSessionsCount(),
		"subscribers":     s.waitingRoom.SubscribersCount(),
		"uptime_seconds":  int64(time.Since(s.startTime).Seconds()),
	})
}

func (s *Server) handleAdminPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.waitingRoom.SetPaused(true)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "emergency pause activated: admissions stopped",
		"paused":  true,
	})
}

func (s *Server) handleAdminResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.waitingRoom.SetPaused(false)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "admissions resumed",
		"paused":  false,
	})
}

func (s *Server) handleAdminRate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newRate uint64

	// Try reading JSON body
	var payload struct {
		Rate uint64 `json:"rate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err == nil && payload.Rate > 0 {
		newRate = payload.Rate
	} else {
		// Try query param ?rate=50
		if qRate := r.URL.Query().Get("rate"); qRate != "" {
			if parsed, err := strconv.ParseUint(qRate, 10, 64); err == nil && parsed > 0 {
				newRate = parsed
			}
		}
	}

	if newRate == 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid rate parameter, must be positive integer"})
		return
	}

	s.waitingRoom.SetDischargeRate(newRate)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "discharge rate updated",
		"rate":    newRate,
	})
}

func (s *Server) handleAdminBypass(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bypassVal := false
	var payload struct {
		Bypass bool `json:"bypass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
		bypassVal = payload.Bypass
	} else {
		if qVal := r.URL.Query().Get("enabled"); qVal != "" {
			bypassVal = (qVal == "true" || qVal == "1")
		}
	}

	s.waitingRoom.SetBypass(bypassVal)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "bypass mode updated",
		"bypass":  bypassVal,
	})
}

func (s *Server) handleAdminReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.waitingRoom.Reset()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "waiting room and turnstile sequence reset to zero",
	})
}
