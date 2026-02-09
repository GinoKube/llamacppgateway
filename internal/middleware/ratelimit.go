package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/llamawrapper/gateway/internal/config"
)

type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	burst    int
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

func newRateLimiter(requestsPerMin int, burstSize int) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(requestsPerMin) / 60.0,
		burst:   burstSize,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(rl.burst), lastTime: now}
		rl.buckets[key] = b
	}

	// Add tokens based on elapsed time
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// cleanup removes stale buckets (call periodically)
func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-5 * time.Minute)
	for key, b := range rl.buckets {
		if b.lastTime.Before(cutoff) {
			delete(rl.buckets, key)
		}
	}
}

// RateLimit returns middleware that enforces per-IP token bucket rate limiting.
func RateLimit(cfg config.RateLimitConfig) func(http.Handler) http.Handler {
	rl := newRateLimiter(cfg.RequestsPerMin, cfg.BurstSize)

	// Periodic cleanup of stale buckets
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()
	// Note: this goroutine runs for the lifetime of the process.
	// Since RateLimit is only called once at startup, this is acceptable.

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Skip rate limiting for health/metrics
			if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			// Use API key if present, otherwise IP
			key := extractAPIKey(r)
			if key == "" {
				key = r.RemoteAddr
			}

			if !rl.allow(key) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error","code":"rate_limit_exceeded"}}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
