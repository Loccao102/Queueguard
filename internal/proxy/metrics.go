package proxy

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// handleMetrics formats and exports metrics in Prometheus text exposition format (RFC 0004).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	queueDepth := s.waitingRoom.Sequence().QueueDepth()
	lastIssued := s.waitingRoom.Sequence().LastIssued()
	admitted := s.waitingRoom.Sequence().Admitted()
	rate := s.waitingRoom.GetDischargeRate()

	isPaused := 0
	if s.waitingRoom.IsPaused() {
		isPaused = 1
	}

	isBypass := 0
	if s.waitingRoom.IsBypass() {
		isBypass = 1
	}

	activeSessions := s.waitingRoom.ActiveSessionsCount()
	sseSubscribers := s.waitingRoom.SubscribersCount()

	totalReq := atomic.LoadUint64(&s.metricsTotalRequests)
	bypassedReq := atomic.LoadUint64(&s.metricsBypassedRequests)
	admittedReq := atomic.LoadUint64(&s.metricsAdmittedRequests)
	limitedReq := atomic.LoadUint64(&s.metricsLimitedRequests)

	fmt.Fprintf(w, "# HELP queueguard_queue_depth Current number of users waiting in the queue\n")
	fmt.Fprintf(w, "# TYPE queueguard_queue_depth gauge\n")
	fmt.Fprintf(w, "queueguard_queue_depth %d\n\n", queueDepth)

	fmt.Fprintf(w, "# HELP queueguard_last_issued_ticket The highest ticket number issued\n")
	fmt.Fprintf(w, "# TYPE queueguard_last_issued_ticket counter\n")
	fmt.Fprintf(w, "queueguard_last_issued_ticket %d\n\n", lastIssued)

	fmt.Fprintf(w, "# HELP queueguard_admitted_tickets_total The highest ticket number admitted to origin\n")
	fmt.Fprintf(w, "# TYPE queueguard_admitted_tickets_total counter\n")
	fmt.Fprintf(w, "queueguard_admitted_tickets_total %d\n\n", admitted)

	fmt.Fprintf(w, "# HELP queueguard_discharge_rate Current discharge rate per second\n")
	fmt.Fprintf(w, "# TYPE queueguard_discharge_rate gauge\n")
	fmt.Fprintf(w, "queueguard_discharge_rate %d\n\n", rate)

	fmt.Fprintf(w, "# HELP queueguard_is_paused Whether admission is currently paused (1: paused, 0: running)\n")
	fmt.Fprintf(w, "# TYPE queueguard_is_paused gauge\n")
	fmt.Fprintf(w, "queueguard_is_paused %d\n\n", isPaused)

	fmt.Fprintf(w, "# HELP queueguard_is_bypass Whether queue bypass is currently active (1: active, 0: disabled)\n")
	fmt.Fprintf(w, "# TYPE queueguard_is_bypass gauge\n")
	fmt.Fprintf(w, "queueguard_is_bypass %d\n\n", isBypass)

	fmt.Fprintf(w, "# HELP queueguard_active_sessions Number of sessions currently tracked in memory\n")
	fmt.Fprintf(w, "# TYPE queueguard_active_sessions gauge\n")
	fmt.Fprintf(w, "queueguard_active_sessions %d\n\n", activeSessions)

	fmt.Fprintf(w, "# HELP queueguard_sse_subscribers Number of live SSE streams active\n")
	fmt.Fprintf(w, "# TYPE queueguard_sse_subscribers gauge\n")
	fmt.Fprintf(w, "queueguard_sse_subscribers %d\n\n", sseSubscribers)

	fmt.Fprintf(w, "# HELP queueguard_http_requests_total Total number of HTTP requests processed by type\n")
	fmt.Fprintf(w, "# TYPE queueguard_http_requests_total counter\n")
	fmt.Fprintf(w, "queueguard_http_requests_total{type=\"total\"} %d\n", totalReq)
	fmt.Fprintf(w, "queueguard_http_requests_total{type=\"bypassed\"} %d\n", bypassedReq)
	fmt.Fprintf(w, "queueguard_http_requests_total{type=\"admitted\"} %d\n", admittedReq)
	fmt.Fprintf(w, "queueguard_http_requests_total{type=\"rate_limited\"} %d\n", limitedReq)
}
