package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyRateLimiter_AllowsWithinLimit(t *testing.T) {
	l := newKeyRateLimiter(60) // 60 req/min = 1 req/s, burst 10

	// First burst of requests should all be allowed.
	for i := 0; i < 10; i++ {
		assert.True(t, l.Allow("key1"), "request %d should be allowed", i)
	}
}

func TestKeyRateLimiter_BlocksExcessRequests(t *testing.T) {
	l := newKeyRateLimiter(60) // burst = 10

	// Exhaust burst.
	for i := 0; i < 10; i++ {
		l.Allow("key1")
	}

	// Next request should be blocked.
	assert.False(t, l.Allow("key1"), "should be blocked after exhausting burst")
}

func TestKeyRateLimiter_IndependentKeys(t *testing.T) {
	l := newKeyRateLimiter(60)

	// Exhaust key1.
	for i := 0; i < 10; i++ {
		l.Allow("key1")
	}
	assert.False(t, l.Allow("key1"))

	// key2 should still be allowed.
	assert.True(t, l.Allow("key2"))
}

func TestKeyRateLimiter_RetryAfter(t *testing.T) {
	l := newKeyRateLimiter(60)

	// Exhaust burst.
	for i := 0; i < 10; i++ {
		l.Allow("key1")
	}

	d := l.RetryAfter("key1")
	assert.Greater(t, d.Seconds(), float64(0), "RetryAfter should be positive after exhausting burst")
}

func TestKeyRateLimiter_RetryAfterUnknownKey(t *testing.T) {
	l := newKeyRateLimiter(60)
	d := l.RetryAfter("unknown")
	assert.Equal(t, float64(0), d.Seconds())
}

func TestIPRateLimiter_Middleware_AllowsNormalTraffic(t *testing.T) {
	l := newIPRateLimiter(60)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/keys", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIPRateLimiter_Middleware_Returns429(t *testing.T) {
	l := newIPRateLimiter(6) // 6 req/min, burst = 1

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request succeeds.
	req := httptest.NewRequest("GET", "/api/keys", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Second request should be rate limited.
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	assert.NotEmpty(t, w2.Header().Get("Retry-After"))
	assert.Contains(t, w2.Body.String(), "rate limit exceeded")
}
