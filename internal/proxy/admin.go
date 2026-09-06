package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Loccao102/queueguard/internal/queue"
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
	case "/queueguard/api/admin/rooms":
		s.handleAdminRooms(w, r)
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
	case "/queueguard/api/admin/event-time":
		s.handleAdminEventTime(w, r)
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

func (s *Server) targetRooms(r *http.Request) []*queue.WaitingRoom {
	roomParam := r.URL.Query().Get("room")
	if roomParam == "" || roomParam == "default" {
		return []*queue.WaitingRoom{s.waitingRoom}
	}
	if roomParam == "all" && s.roomManager != nil {
		all := s.roomManager.All()
		list := make([]*queue.WaitingRoom, 0, len(all))
		for _, rm := range all {
			list = append(list, rm)
		}
		return list
	}
	if s.roomManager != nil {
		if rm, ok := s.roomManager.Get(roomParam); ok {
			return []*queue.WaitingRoom{rm}
		}
	}
	return []*queue.WaitingRoom{s.waitingRoom}
}

func (s *Server) handleAdminRooms(w http.ResponseWriter, r *http.Request) {
	if s.roomManager == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": []any{}})
		return
	}

	defs := s.roomManager.Definitions()
	rooms := s.roomManager.All()
	var list []map[string]any

	for id, def := range defs {
		room := rooms[id]
		if room == nil {
			continue
		}
		roomInfo := map[string]any{
			"id":          id,
			"name":        def.Name,
			"path_prefix": def.PathPrefix,
			"rate":        room.GetDischargeRate(),
			"queue_depth": room.Sequence().QueueDepth(),
			"last_issued": room.Sequence().LastIssued(),
			"admitted":    room.Sequence().Admitted(),
			"paused":      room.IsPaused(),
			"bypass":      room.IsBypass(),
			"subscribers": room.SubscribersCount(),
		}
		list = append(list, roomInfo)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"rooms": list,
	})
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	targetRoom := s.waitingRoom
	if roomParam := r.URL.Query().Get("room"); roomParam != "" && s.roomManager != nil {
		if rm, ok := s.roomManager.Get(roomParam); ok {
			targetRoom = rm
		}
	}

	preQueueCount := 0
	if pq := targetRoom.PreQueue(); pq != nil {
		preQueueCount = pq.Count()
	}

	payload := map[string]any{
		"room":            targetRoom.ID(),
		"room_name":       targetRoom.Name(),
		"queue_depth":     targetRoom.Sequence().QueueDepth(),
		"last_issued":     targetRoom.Sequence().LastIssued(),
		"admitted":        targetRoom.Sequence().Admitted(),
		"rate":            targetRoom.GetDischargeRate(),
		"paused":          targetRoom.IsPaused(),
		"bypass":          targetRoom.IsBypass(),
		"is_prequeue":     targetRoom.IsPreQueue(),
		"prequeue_count":  preQueueCount,
		"active_sessions": targetRoom.ActiveSessionsCount(),
		"subscribers":     targetRoom.SubscribersCount(),
		"uptime_seconds":  int64(time.Since(s.startTime).Seconds()),
	}

	if s.roomManager != nil && len(s.roomManager.All()) > 1 {
		var roomSummaries []map[string]any
		for id, def := range s.roomManager.Definitions() {
			if rm, ok := s.roomManager.Get(id); ok {
				roomSummaries = append(roomSummaries, map[string]any{
					"id":          id,
					"name":        def.Name,
					"path_prefix": def.PathPrefix,
					"queue_depth": rm.Sequence().QueueDepth(),
					"admitted":    rm.Sequence().Admitted(),
					"rate":        rm.GetDischargeRate(),
					"paused":      rm.IsPaused(),
					"bypass":      rm.IsBypass(),
				})
			}
		}
		payload["rooms"] = roomSummaries
	}

	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleAdminEventTime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var targetTime time.Time
	if inSec := r.URL.Query().Get("seconds"); inSec != "" {
		if sec, err := strconv.Atoi(inSec); err == nil && sec > 0 {
			targetTime = time.Now().Add(time.Duration(sec) * time.Second)
		}
	} else {
		var payload struct {
			StartTime string `json:"start_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil && payload.StartTime != "" {
			if parsed, err := time.Parse(time.RFC3339, payload.StartTime); err == nil {
				targetTime = parsed
			}
		}
	}

	if targetTime.IsZero() {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "valid start_time (RFC3339) or seconds parameter required"})
		return
	}

	rooms := s.targetRooms(r)
	for _, rm := range rooms {
		rm.SetEventStartTime(targetTime)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"message":    "pre-queue event start time updated",
		"start_time": targetTime.Format(time.RFC3339),
	})
}

func (s *Server) handleAdminPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rooms := s.targetRooms(r)
	for _, rm := range rooms {
		rm.SetPaused(true)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "emergency pause activated: admissions stopped",
		"paused":  true,
		"count":   len(rooms),
	})
}

func (s *Server) handleAdminResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rooms := s.targetRooms(r)
	for _, rm := range rooms {
		rm.SetPaused(false)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "admissions resumed",
		"paused":  false,
		"count":   len(rooms),
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

	rooms := s.targetRooms(r)
	for _, rm := range rooms {
		rm.SetDischargeRate(newRate)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "discharge rate updated",
		"rate":    newRate,
		"count":   len(rooms),
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

	rooms := s.targetRooms(r)
	for _, rm := range rooms {
		rm.SetBypass(bypassVal)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "bypass mode updated",
		"bypass":  bypassVal,
		"count":   len(rooms),
	})
}

func (s *Server) handleAdminReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rooms := s.targetRooms(r)
	for _, rm := range rooms {
		rm.Reset()
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": "waiting room and turnstile sequence reset to zero",
		"count":   len(rooms),
	})
}
