package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		if cookie, err := r.Cookie(CookieSession); err == nil {
			sessionID = cookie.Value
		}
	}

	if sessionID == "" {
		http.Error(w, "session_id required", http.StatusBadRequest)
		return
	}

	roomID := r.URL.Query().Get("room")
	room := s.waitingRoom
	if roomID != "" && s.roomManager != nil {
		if rm, ok := s.roomManager.Get(roomID); ok {
			room = rm
		}
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable Nginx buffering if deployed behind proxy

	// Subscribe to queue advance events
	subCh, unsubscribe := room.Subscribe()
	defer unsubscribe()

	heartbeatTicker := time.NewTicker(2 * time.Second)
	defer heartbeatTicker.Stop()

	// Function to send current status payload
	sendUpdate := func() bool {
		admitted, pos, estSec, token, err := room.CheckStatus(sessionID)
		if err != nil {
			return false
		}

		sess, _ := room.Enroll(sessionID)

		isPreQueue := room.IsPreQueue()
		payload := map[string]any{
			"ticket_number": sess.TicketNumber,
			"position":      pos,
			"est_seconds":   estSec,
			"admitted":      admitted,
			"token":         token,
			"is_prequeue":   isPreQueue,
			"room":          room.ID(),
			"room_name":     room.Name(),
		}

		data, _ := json.Marshal(payload)
		_, writeErr := fmt.Fprintf(w, "data: %s\n\n", data)
		if writeErr != nil {
			return false
		}
		flusher.Flush()

		// If user was admitted, they have their token and will redirect
		return !admitted
	}

	// Send immediate initial status
	if !sendUpdate() {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return

		case <-subCh:
			if !sendUpdate() {
				return
			}

		case <-heartbeatTicker.C:
			room.Heartbeat(sessionID)
			if !sendUpdate() {
				return
			}
		}
	}
}
