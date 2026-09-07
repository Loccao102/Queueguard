package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
	"github.com/Loccao102/queueguard/internal/queue"
	"github.com/Loccao102/queueguard/internal/ratelimit"
)

func TestProxyInterceptionAndPassThrough(t *testing.T) {
	// 1. Create a dummy origin backend server
	originHits := 0
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello from Protected Origin!"))
	}))
	defer originServer.Close()

	// 2. Setup QueueGuard Proxy
	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, err := NewServer(originServer.URL, wr, signer)
	if err != nil {
		t.Fatalf("failed to create proxy server: %v", err)
	}

	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 3 * time.Second}

	// 3. First request from browser without ticket -> should receive Waiting Room HTML
	req, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if !strings.Contains(string(body), "QueueGuard") {
		t.Fatalf("expected waiting room page, got: %s", string(body))
	}
	if originHits != 0 {
		t.Fatalf("origin should NOT have been hit yet, got hits=%d", originHits)
	}

	// Extract session cookie from response
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == CookieSession {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}

	// 4. Advance admission turnstile
	wr.Sequence().AdvanceAdmission(10)

	// 5. Query status to receive admission token
	statusReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/queueguard/status?session_id=%s", proxyHTTP.URL, sessionCookie.Value), nil)
	statusResp, err := client.Do(statusReq)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()

	// 6. Now client accesses with the valid admission ticket
	token, _, err := signer.Issue(sessionCookie.Value, 1)
	if err != nil {
		t.Fatal(err)
	}

	authedReq, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/checkout", nil)
	authedReq.AddCookie(&http.Cookie{
		Name:  CookieTicket,
		Value: token,
	})
	authedReq.AddCookie(sessionCookie)

	authedResp, err := client.Do(authedReq)
	if err != nil {
		t.Fatal(err)
	}
	authedBody, _ := io.ReadAll(authedResp.Body)
	authedResp.Body.Close()

	if authedResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", authedResp.StatusCode)
	}
	if string(authedBody) != "Hello from Protected Origin!" {
		t.Fatalf("expected origin response, got: %s", string(authedBody))
	}
	if originHits != 1 {
		t.Fatalf("expected exactly 1 hit on origin, got: %d", originHits)
	}
}

func TestAPIClientReceives429JSON(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, _ := NewServer(originServer.URL, wr, signer)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/api/v1/tickets", nil)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests for API clients without ticket, got: %d", resp.StatusCode)
	}
}

func TestTicketCannotBeReusedWithDifferentSession(t *testing.T) {
	originHits := 0
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)
	proxyServer, _ := NewServer(originServer.URL, wr, signer)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	token, _, err := signer.Issue("session-a", 1)
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/checkout", nil)
	req.AddCookie(&http.Cookie{Name: CookieSession, Value: "session-b"})
	req.AddCookie(&http.Cookie{Name: CookieTicket, Value: token})
	req.Header.Set("Accept", "text/html")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK || originHits != 0 {
		t.Fatalf("expected mismatched ticket to remain queued, status=%d origin_hits=%d", resp.StatusCode, originHits)
	}
}

func TestStatusDoesNotExposeAdmissionToken(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)
	proxyServer, _ := NewServer(originServer.URL, wr, signer)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	const sessionID = "session-status"
	wr.Enroll(sessionID)
	wr.Sequence().AdvanceAdmission(1)

	req, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/queueguard/status", nil)
	req.AddCookie(&http.Cookie{Name: CookieSession, Value: sessionID})
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["token"]; exists {
		t.Fatal("status endpoint must not expose admission token")
	}
}

func TestPathWhitelistBypass(t *testing.T) {
	originHits := 0
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("static asset delivered"))
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, _ := NewServer(
		originServer.URL,
		wr,
		signer,
		WithBypassPaths([]string{"/custom/webhook/*", "/favicon.ico"}),
	)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// 1. Static asset (.css) should bypass directly to origin
	resp1, err := client.Get(proxyHTTP.URL + "/assets/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK || originHits != 1 {
		t.Fatalf("expected .css to bypass to origin, status=%d hits=%d", resp1.StatusCode, originHits)
	}

	// 2. Custom webhook prefix should bypass directly to origin
	resp2, err := client.Get(proxyHTTP.URL + "/custom/webhook/test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK || originHits != 2 {
		t.Fatalf("expected webhook to bypass to origin, status=%d hits=%d", resp2.StatusCode, originHits)
	}

	// 3. Regular path WITHOUT ticket should NOT hit origin (should hit waiting room)
	resp3, err := client.Get(proxyHTTP.URL + "/regular-page")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if originHits != 2 {
		t.Fatalf("expected regular page to be intercepted by waiting room, hits=%d", originHits)
	}
}

func TestAdminControlPlane(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	adminToken := "super-admin-key"
	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, _ := NewServer(
		originServer.URL,
		wr,
		signer,
		WithAdminToken(adminToken),
	)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// 1. Check Admin UI page
	uiResp, err := client.Get(proxyHTTP.URL + "/queueguard/admin")
	if err != nil {
		t.Fatal(err)
	}
	uiBody, _ := io.ReadAll(uiResp.Body)
	uiResp.Body.Close()
	if !strings.Contains(string(uiBody), "QueueGuard") {
		t.Fatalf("expected admin HTML page, got %s", string(uiBody))
	}

	// 2. Unauthorized access to Admin API
	statsReq, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/queueguard/api/admin/stats", nil)
	unauthResp, err := client.Do(statsReq)
	if err != nil {
		t.Fatal(err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got: %d", unauthResp.StatusCode)
	}

	// 3. Authorized access with X-Admin-Token
	statsReq.Header.Set("X-Admin-Token", adminToken)
	authResp, err := client.Do(statsReq)
	if err != nil {
		t.Fatal(err)
	}
	authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for authorized admin, got: %d", authResp.StatusCode)
	}

	// 4. Emergency Pause
	pauseReq, _ := http.NewRequest(http.MethodPost, proxyHTTP.URL+"/queueguard/api/admin/pause", nil)
	pauseReq.Header.Set("Authorization", "Bearer "+adminToken)
	pauseResp, err := client.Do(pauseReq)
	if err != nil {
		t.Fatal(err)
	}
	pauseResp.Body.Close()
	if !wr.IsPaused() {
		t.Fatal("expected waiting room to be paused")
	}

	// 5. Resume
	resumeReq, _ := http.NewRequest(http.MethodPost, proxyHTTP.URL+"/queueguard/api/admin/resume", nil)
	resumeReq.Header.Set("X-Admin-Token", adminToken)
	resumeResp, err := client.Do(resumeReq)
	if err != nil {
		t.Fatal(err)
	}
	resumeResp.Body.Close()
	if wr.IsPaused() {
		t.Fatal("expected waiting room to be resumed")
	}

	// 6. Adjust Discharge Rate
	rateReq, _ := http.NewRequest(http.MethodPost, proxyHTTP.URL+"/queueguard/api/admin/rate?rate=75", nil)
	rateReq.Header.Set("X-Admin-Token", adminToken)
	rateResp, err := client.Do(rateReq)
	if err != nil {
		t.Fatal(err)
	}
	rateResp.Body.Close()
	if wr.GetDischargeRate() != 75 {
		t.Fatalf("expected discharge rate to be 75, got: %d", wr.GetDischargeRate())
	}

	// 7. Toggle Bypass
	bypassReq, _ := http.NewRequest(http.MethodPost, proxyHTTP.URL+"/queueguard/api/admin/bypass?enabled=true", nil)
	bypassReq.Header.Set("X-Admin-Token", adminToken)
	bypassResp, err := client.Do(bypassReq)
	if err != nil {
		t.Fatal(err)
	}
	bypassResp.Body.Close()
	if !wr.IsBypass() {
		t.Fatal("expected waiting room to be in bypass mode")
	}

	// 8. Reset Queue
	resetReq, _ := http.NewRequest(http.MethodPost, proxyHTTP.URL+"/queueguard/api/admin/reset", nil)
	resetReq.Header.Set("X-Admin-Token", adminToken)
	resetResp, err := client.Do(resetReq)
	if err != nil {
		t.Fatal(err)
	}
	resetResp.Body.Close()
	if wr.Sequence().LastIssued() != 0 || wr.Sequence().Admitted() != 0 {
		t.Fatal("expected queue counters to be reset to 0")
	}
}

func TestPrometheusMetricsEndpoint(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, _ := NewServer(originServer.URL, wr, signer)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	for _, endpoint := range []string{"/metrics", "/queueguard/metrics"} {
		resp, err := client.Get(proxyHTTP.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: expected 200 OK, got: %d", endpoint, resp.StatusCode)
		}

		bodyStr := string(body)
		if !strings.Contains(bodyStr, "queueguard_queue_depth") ||
			!strings.Contains(bodyStr, "queueguard_discharge_rate") ||
			!strings.Contains(bodyStr, "queueguard_is_paused") {
			t.Fatalf("%s: missing expected Prometheus metric names in:\n%s", endpoint, bodyStr)
		}
	}
}

func TestProxyIPRateLimiterEnforcement(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	// Burst of 3 requests
	limiter := ratelimit.NewIPRateLimiter(60, 3)
	defer limiter.Close()

	proxyServer, _ := NewServer(
		originServer.URL,
		wr,
		signer,
		WithRateLimiter(limiter),
	)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// 3 requests within burst should pass (hitting waiting room)
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200 (waiting room), got: %d", i+1, resp.StatusCode)
		}
	}

	// 4th request from same IP exceeds burst and should receive 429 rate_limited
	req, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
	req.Header.Set("X-Forwarded-For", "192.0.2.1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests for rate limited IP, got: %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "rate_limited") {
		t.Fatalf("expected rate_limited error message, got: %s", string(body))
	}
}

func TestPreQueueCountdownAndFairLottery(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wrCfg := queue.DefaultConfig()
	// Set start time 200ms into the future
	wrCfg.EventStartTime = time.Now().Add(200 * time.Millisecond)
	wr := queue.NewWaitingRoom(wrCfg, signer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go wr.StartDischargeWorker(ctx)

	proxyServer, _ := NewServer(originServer.URL, wr, signer)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// 1. First user arrives during pre-queue
	req, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == CookieSession {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}

	// In pre-queue, status should report pre-queue active
	if !wr.IsPreQueue() {
		t.Fatal("expected waiting room to be in pre-queue mode before start time")
	}

	// Query status
	statusReq, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/queueguard/status?session_id=%s", proxyHTTP.URL, sessionCookie.Value), nil)
	statusResp, err := client.Do(statusReq)
	if err != nil {
		t.Fatal(err)
	}
	var statusData map[string]any
	_ = json.NewDecoder(statusResp.Body).Decode(&statusData)
	statusResp.Body.Close()

	// Wait for event start time + discharge tick
	time.Sleep(300 * time.Millisecond)

	// Pre-queue should now be completed
	if wr.IsPreQueue() {
		t.Fatal("expected pre-queue mode to be completed after start time")
	}

	// After lottery, user should receive a valid ticket number
	sess, isNew := wr.Enroll(sessionCookie.Value)
	if isNew {
		t.Fatal("expected existing session to be found")
	}
	if sess.TicketNumber == 0 {
		t.Fatalf("expected assigned ticket number after lottery, got: %d", sess.TicketNumber)
	}
}

func TestDeviceBindingInProxy(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("admitted to origin"))
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, _ := NewServer(
		originServer.URL,
		wr,
		signer,
		WithDeviceBinding(true),
	)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	ua1 := "Browser-A-MacBook"
	ip1 := "192.168.1.50"
	devHash1 := crypto.ComputeDeviceFingerprint(ua1, ip1)

	// Issue device-bound ticket for device 1
	token, _, err := signer.IssueWithDevice("sess_bound", 1, devHash1)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Request with matching device should pass to origin
	req1, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
	req1.Header.Set("User-Agent", ua1)
	req1.Header.Set("X-Forwarded-For", ip1)
	req1.AddCookie(&http.Cookie{Name: CookieSession, Value: "sess_bound"})
	req1.AddCookie(&http.Cookie{Name: CookieTicket, Value: token})

	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	if !strings.Contains(string(body1), "admitted to origin") {
		t.Fatalf("expected matching device to reach origin, got: %s", string(body1))
	}

	// 2. Request with DIFFERENT device (scalper resale attempt) should FAIL and hit waiting room
	req2, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
	req2.Header.Set("User-Agent", "Browser-B-iPhone")
	req2.Header.Set("X-Forwarded-For", "203.0.113.88")
	req2.AddCookie(&http.Cookie{Name: CookieSession, Value: "sess_bound"})
	req2.AddCookie(&http.Cookie{Name: CookieTicket, Value: token})

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if strings.Contains(string(body2), "admitted to origin") {
		t.Fatal("expected different device to be denied access to origin")
	}
}

func TestDynamicTicketSlidingExtension(t *testing.T) {
	// Origin backend that returns X-QueueGuard-Extend header
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-QueueGuard-Extend", "20m")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("payment processed"))
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, _ := NewServer(originServer.URL, wr, signer)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	token, _, _ := signer.Issue("sess_extend", 1)

	req, _ := http.NewRequest(http.MethodPost, proxyHTTP.URL+"/checkout", nil)
	req.AddCookie(&http.Cookie{Name: CookieSession, Value: "sess_extend"})
	req.AddCookie(&http.Cookie{Name: CookieTicket, Value: token})

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Verify that Set-Cookie for queueguard_ticket was returned with extended TTL
	var newTicketCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == CookieTicket {
			newTicketCookie = c
			break
		}
	}
	if newTicketCookie == nil {
		t.Fatal("expected extended ticket cookie to be set in response")
	}

	// Verify the new token has the 20m TTL (~1200 seconds)
	if newTicketCookie.MaxAge != 1200 {
		t.Fatalf("expected cookie MaxAge to be 1200s (20m), got: %d", newTicketCookie.MaxAge)
	}
}

func TestPoWEnforcementInProxy(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer originServer.Close()

	signer := crypto.NewSigner("secret-key", 5*time.Minute)
	wr := queue.NewWaitingRoom(queue.DefaultConfig(), signer)

	proxyServer, _ := NewServer(
		originServer.URL,
		wr,
		signer,
		WithPoWDifficulty(2), // low difficulty for fast test
	)
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// 1. Request without PoW should receive 403 pow_required
	req1, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden without PoW, got: %d", resp1.StatusCode)
	}

	// 2. Fetch PoW challenge
	chResp, err := client.Get(proxyHTTP.URL + "/queueguard/pow/challenge")
	if err != nil {
		t.Fatal(err)
	}
	var chData struct {
		Challenge  string `json:"challenge"`
		Seed       string `json:"seed"`
		Difficulty int    `json:"difficulty"`
	}
	_ = json.NewDecoder(chResp.Body).Decode(&chData)
	chResp.Body.Close()

	// 3. Solve the challenge
	nonce := crypto.SolvePoW(chData.Seed, chData.Difficulty)

	// 4. Request with solved PoW should now be accepted!
	req2, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/", nil)
	req2.Header.Set("X-QueueGuard-PoW-Token", chData.Challenge)
	req2.Header.Set("X-QueueGuard-PoW-Nonce", nonce)

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after solving PoW, got: %d", resp2.StatusCode)
	}
}

func TestMultiRoomProxyRouting(t *testing.T) {
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Origin-Hit", "true")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("origin response"))
	}))
	defer originServer.Close()

	secret := "secret-12345"
	signer := crypto.NewSigner(secret, 10*time.Minute)

	defCfg := queue.DefaultConfig()
	defCfg.RoomID = "default"
	defaultRoom := queue.NewWaitingRoom(defCfg, signer)

	vipCfg := queue.DefaultConfig()
	vipCfg.RoomID = "vip"
	vipCfg.Name = "VIP Lounge"
	vipRoom := queue.NewWaitingRoom(vipCfg, signer)

	genCfg := queue.DefaultConfig()
	genCfg.RoomID = "general"
	genCfg.Name = "General Admission"
	genRoom := queue.NewWaitingRoom(genCfg, signer)

	rm := queue.NewRoomManager(defaultRoom)
	_ = rm.Register(queue.RoomDefinition{ID: "vip", Name: "VIP Lounge", PathPrefix: "/tickets/vip"}, vipRoom)
	_ = rm.Register(queue.RoomDefinition{ID: "general", Name: "General Admission", PathPrefix: "/tickets"}, genRoom)

	proxyServer, err := NewServer(
		originServer.URL,
		defaultRoom,
		signer,
		WithRoomManager(rm),
		WithBranding("Mega Concert", "https://example.com/logo.png", "#10b981", "Welcome!"),
	)
	if err != nil {
		t.Fatal(err)
	}
	proxyHTTP := httptest.NewServer(proxyServer)
	defer proxyHTTP.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	// 1. Request to /tickets/vip should be governed by VIP room
	respVIP, err := client.Get(proxyHTTP.URL + "/tickets/vip")
	if err != nil {
		t.Fatal(err)
	}
	roomHdr := respVIP.Header.Get("X-QueueGuard-Room")
	respVIP.Body.Close()
	if roomHdr != "vip" {
		t.Fatalf("expected X-QueueGuard-Room to be 'vip', got: '%s'", roomHdr)
	}

	// 2. Request to /tickets/general should be governed by general room
	respGen, err := client.Get(proxyHTTP.URL + "/tickets/general")
	if err != nil {
		t.Fatal(err)
	}
	roomHdrGen := respGen.Header.Get("X-QueueGuard-Room")
	respGen.Body.Close()
	if roomHdrGen != "general" {
		t.Fatalf("expected X-QueueGuard-Room to be 'general', got: '%s'", roomHdrGen)
	}

	// 3. Request to /contact should fallback to default room
	respDef, err := client.Get(proxyHTTP.URL + "/contact")
	if err != nil {
		t.Fatal(err)
	}
	roomHdrDef := respDef.Header.Get("X-QueueGuard-Room")
	respDef.Body.Close()
	if roomHdrDef != "default" {
		t.Fatalf("expected X-QueueGuard-Room to be 'default', got: '%s'", roomHdrDef)
	}

	// 4. Admin rooms API should report all registered rooms
	reqAdmin, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/queueguard/api/admin/rooms", nil)
	reqAdmin.Header.Set("X-Admin-Token", "queueguard-admin-secret")
	respAdmin, err := client.Do(reqAdmin)
	if err != nil {
		t.Fatal(err)
	}
	defer respAdmin.Body.Close()
	if respAdmin.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /queueguard/api/admin/rooms, got: %d", respAdmin.StatusCode)
	}
	var adminRooms struct {
		Rooms []map[string]any `json:"rooms"`
	}
	if err := json.NewDecoder(respAdmin.Body).Decode(&adminRooms); err != nil {
		t.Fatal(err)
	}
	if len(adminRooms.Rooms) != 3 {
		t.Fatalf("expected 3 rooms in admin API, got: %d", len(adminRooms.Rooms))
	}

	// 5. Test Room Cryptographic Isolation:
	// A ticket issued for "vip" cannot bypass the "general" waiting room!
	sessionID := "user-room-test"
	tokenVIP, _, err := signer.IssueWithRoom(sessionID, 1, "", "vip")
	if err != nil {
		t.Fatal(err)
	}

	reqWithVIPTicket, _ := http.NewRequest(http.MethodGet, proxyHTTP.URL+"/tickets/general", nil)
	reqWithVIPTicket.AddCookie(&http.Cookie{Name: CookieSession, Value: sessionID})
	reqWithVIPTicket.AddCookie(&http.Cookie{Name: CookieTicket, Value: tokenVIP})

	respCheck, err := client.Do(reqWithVIPTicket)
	if err != nil {
		t.Fatal(err)
	}
	respCheck.Body.Close()

	// Should be caught in the general waiting room, NOT forwarded to origin!
	if respCheck.Header.Get("X-Origin-Hit") == "true" {
		t.Fatal("VIP ticket should NOT be accepted for General room URL!")
	}
	if respCheck.Header.Get("X-QueueGuard-Room") != "general" {
		t.Fatalf("expected to be queued in general room, got: %s", respCheck.Header.Get("X-QueueGuard-Room"))
	}
}
