package queueguard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Loccao102/queueguard/internal/crypto"
)

func TestGoMiddleware_PassWithValidTicket(t *testing.T) {
	secret := "my-secret-key-12345"
	signer := crypto.NewSigner(secret, 5*time.Minute)

	ua := "TestBrowser/1.0"
	ip := "192.168.1.50"
	devHash := ComputeDeviceFingerprint(ua, ip)

	token, _, err := signer.IssueWithRoom("sess-1", 42, devHash, "vip")
	if err != nil {
		t.Fatal(err)
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticket, ok := FromContext(r.Context())
		if !ok || ticket == nil {
			t.Fatal("expected ticket to be present in context")
		}
		if ticket.RoomID != "vip" || ticket.SessionID != "sess-1" {
			t.Fatalf("unexpected ticket values: %+v", ticket)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("origin secret content"))
	})

	mw := New(secret, WithRoom("vip"), WithDeviceBinding(true))
	handler := mw(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/checkout", nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("X-Forwarded-For", ip)
	req.AddCookie(&http.Cookie{Name: "queueguard_ticket", Value: token})
	req.AddCookie(&http.Cookie{Name: "queueguard_session", Value: "sess-1"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d", rec.Code)
	}
}

func TestGoMiddleware_RejectWithoutTicket(t *testing.T) {
	mw := New("my-secret", WithRedirectURL("https://queue.example.com"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/vip-page", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got: %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://queue.example.com" {
		t.Fatalf("expected redirect to queue, got: %s", loc)
	}
}

func TestGoMiddleware_RejectWrongRoom(t *testing.T) {
	secret := "secret-xyz"
	signer := crypto.NewSigner(secret, 5*time.Minute)
	tokenGeneral, _, _ := signer.IssueWithRoom("sess-2", 1, "", "general")

	mw := New(secret, WithRoom("vip"), WithDeviceBinding(false))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/vip-only", nil)
	req.AddCookie(&http.Cookie{Name: "queueguard_ticket", Value: tokenGeneral})
	req.AddCookie(&http.Cookie{Name: "queueguard_session", Value: "sess-2"})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong room ticket, got: %d", rec.Code)
	}
}
