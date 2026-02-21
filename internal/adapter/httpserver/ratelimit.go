package httpserver

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// keyRateLimiter provides per-key rate limiting using token buckets.
type keyRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
}

func newKeyRateLimiter(reqPerMinute float64) *keyRateLimiter {
	burst := int(reqPerMinute / 6) // 10 seconds worth
	if burst < 1 {
		burst = 1
	}
	return &keyRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(reqPerMinute / 60), // convert per-minute to per-second
		burst:    burst,
	}
}

// Allow checks if the given key is within its rate limit.
func (l *keyRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	limiter, ok := l.limiters[key]
	if !ok {
		limiter = rate.NewLimiter(l.rate, l.burst)
		l.limiters[key] = limiter
	}
	l.mu.Unlock()
	return limiter.Allow()
}

// RetryAfter returns an estimate of when the next request will be allowed.
func (l *keyRateLimiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	limiter, ok := l.limiters[key]
	l.mu.Unlock()
	if !ok {
		return 0
	}
	reservation := limiter.Reserve()
	delay := reservation.Delay()
	reservation.Cancel()
	return delay
}

// ipRateLimiter is chi middleware that rate limits by client IP.
type ipRateLimiter struct {
	inner *keyRateLimiter
}

func newIPRateLimiter(reqPerMinute float64) *ipRateLimiter {
	return &ipRateLimiter{inner: newKeyRateLimiter(reqPerMinute)}
}

// Middleware returns a chi-compatible middleware that rate limits by IP.
func (l *ipRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr // chi RealIP middleware has already normalised this
		if !l.inner.Allow(ip) {
			retryAfter := l.inner.RetryAfter(ip)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
