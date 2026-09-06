package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPRateLimiter_AllowAndBurst(t *testing.T) {
	// 60 requests per minute = 1 per second, burst of 5
	limiter := NewIPRateLimiter(60, 5)
	defer limiter.Close()

	ip := "192.168.1.100"

	// Initial burst of 5 should all pass
	for i := 0; i < 5; i++ {
		if !limiter.Allow(ip) {
			t.Fatalf("request %d in burst was unexpectedly denied", i+1)
		}
	}

	// 6th request immediately should be denied
	if limiter.Allow(ip) {
		t.Fatal("request beyond burst should have been denied")
	}

	// Different IP should still have its full burst available
	otherIP := "10.0.0.1"
	if !limiter.Allow(otherIP) {
		t.Fatal("different IP should have full burst available")
	}

	// Wait 1.1s to allow 1 token to refill
	time.Sleep(1100 * time.Millisecond)
	if !limiter.Allow(ip) {
		t.Fatal("expected 1 token to be refilled after 1 second")
	}

	// Next immediate request should be denied again
	if limiter.Allow(ip) {
		t.Fatal("expected request after 1 token consumption to be denied")
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remote   string
		expected string
	}{
		{
			name:     "Direct RemoteAddr",
			headers:  map[string]string{},
			remote:   "203.0.113.195:54321",
			expected: "203.0.113.195",
		},
		{
			name: "X-Forwarded-For single",
			headers: map[string]string{
				"X-Forwarded-For": "198.51.100.1",
			},
			remote:   "10.0.0.1:8080",
			expected: "198.51.100.1",
		},
		{
			name: "X-Forwarded-For multiple proxies",
			headers: map[string]string{
				"X-Forwarded-For": "198.51.100.2, 10.0.0.2, 10.0.0.3",
			},
			remote:   "10.0.0.1:8080",
			expected: "198.51.100.2",
		},
		{
			name: "X-Real-IP fallback",
			headers: map[string]string{
				"X-Real-IP": "198.51.100.3",
			},
			remote:   "10.0.0.1:8080",
			expected: "198.51.100.3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			actual := ExtractIP(req)
			if actual != tc.expected {
				t.Fatalf("expected %s, got %s", tc.expected, actual)
			}
		})
	}
}
