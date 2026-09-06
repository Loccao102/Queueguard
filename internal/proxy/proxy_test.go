package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
	"github.com/Loccao102/queueguard/internal/queue"
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
