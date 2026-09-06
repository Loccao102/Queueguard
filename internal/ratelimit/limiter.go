package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// clientBucket holds token bucket state for a single client IP.
type clientBucket struct {
	mu       sync.Mutex
	tokens   float64
	lastSeen time.Time
}

// IPRateLimiter provides per-IP rate limiting using a token-bucket algorithm.
type IPRateLimiter struct {
	ratePerSec float64
	burst      float64
	clients    sync.Map // string (IP) -> *clientBucket
	cleanupTTL time.Duration
	cancel     context.CancelFunc
}

// NewIPRateLimiter initializes a rate limiter with requests-per-minute and burst allowance.
func NewIPRateLimiter(ratePerMinute int, burst int) *IPRateLimiter {
	if ratePerMinute <= 0 {
		ratePerMinute = 60
	}
	if burst <= 0 {
		burst = 20
	}

	ctx, cancel := context.WithCancel(context.Background())
	limiter := &IPRateLimiter{
		ratePerSec: float64(ratePerMinute) / 60.0,
		burst:      float64(burst),
		cleanupTTL: 10 * time.Minute,
		cancel:     cancel,
	}

	go limiter.cleanupLoop(ctx)

	return limiter
}

// Allow reports whether a request from the given IP should be allowed.
func (l *IPRateLimiter) Allow(ip string) bool {
	if ip == "" {
		return true
	}

	val, _ := l.clients.LoadOrStore(ip, &clientBucket{
		tokens:   l.burst,
		lastSeen: time.Now(),
	})
	bucket := val.(*clientBucket)

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(bucket.lastSeen).Seconds()
	bucket.lastSeen = now

	// Refill tokens
	bucket.tokens += elapsed * l.ratePerSec
	if bucket.tokens > l.burst {
		bucket.tokens = l.burst
	}

	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		return true
	}

	return false
}

// ExtractIP retrieves the client's public IP from request headers or remote address.
func ExtractIP(r *http.Request) string {
	// 1. X-Forwarded-For: client, proxy1, proxy2...
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// 2. X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		ip := strings.TrimSpace(xri)
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	// 3. RemoteAddr (ip:port)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host) != nil {
		return host
	}

	return r.RemoteAddr
}

// Close stops the background cleanup goroutine.
func (l *IPRateLimiter) Close() {
	if l.cancel != nil {
		l.cancel()
	}
}

func (l *IPRateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			l.clients.Range(func(key, val any) bool {
				b := val.(*clientBucket)
				b.mu.Lock()
				idle := now.Sub(b.lastSeen)
				b.mu.Unlock()

				if idle > l.cleanupTTL {
					l.clients.Delete(key)
				}
				return true
			})
		}
	}
}
