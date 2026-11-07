//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTokenBucket_Consume(t *testing.T) {
	bucket := &tokenBucket{
		tokens:    10.0,
		capacity:  10.0,
		rate:      10.0,
		lastCheck: time.Now(),
	}

	// Should be able to consume tokens
	assert.True(t, bucket.consume())
	assert.True(t, bucket.consume())

	// Verify tokens decreased
	assert.Less(t, bucket.tokens, 10.0)
}

func TestTokenBucket_Refill(t *testing.T) {
	bucket := &tokenBucket{
		tokens:    0.0,
		capacity:  10.0,
		rate:      10.0,
		lastCheck: time.Now().Add(-1 * time.Second), // 1 second ago
	}

	// Should refill and allow consumption
	assert.True(t, bucket.consume())
}

func TestTokenBucket_CapacityLimit(t *testing.T) {
	bucket := &tokenBucket{
		tokens:    5.0,
		capacity:  10.0,
		rate:      10.0,
		lastCheck: time.Now().Add(-10 * time.Second), // Long time ago
	}

	// Consume once to trigger refill
	bucket.consume()

	// Should not exceed capacity
	assert.LessOrEqual(t, bucket.tokens, 10.0)
}

func TestTokenBucket_ExhaustedTokens(t *testing.T) {
	bucket := &tokenBucket{
		tokens:    0.0,
		capacity:  1.0,
		rate:      1.0,
		lastCheck: time.Now(),
	}

	// Should fail when no tokens available
	assert.False(t, bucket.consume())
}

func TestRateLimiter_Allow(t *testing.T) {
	limiter := newRateLimiter(10)
	defer limiter.cleanup.Stop()

	clientIP := "192.0.2.1"

	// First request should be allowed
	assert.True(t, limiter.Allow(clientIP))

	// Should create bucket for this client
	limiter.mu.RLock()
	_, exists := limiter.buckets[clientIP]
	limiter.mu.RUnlock()
	assert.True(t, exists)
}

func TestRateLimiter_MultipleClients(t *testing.T) {
	limiter := newRateLimiter(5)
	defer limiter.cleanup.Stop()

	client1 := "192.0.2.1"
	client2 := "192.0.2.2"

	// Both clients should be allowed
	assert.True(t, limiter.Allow(client1))
	assert.True(t, limiter.Allow(client2))

	// Verify both have buckets
	limiter.mu.RLock()
	assert.Len(t, limiter.buckets, 2)
	limiter.mu.RUnlock()
}

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	// Rate limiting disabled (0 or negative rate)
	middleware := RateLimitMiddleware(0)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	// Should pass through without rate limiting
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimitMiddleware_RateLimited(t *testing.T) {
	// Very low rate limit for testing
	middleware := RateLimitMiddleware(1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.0.2.1:12345"

	// First request should succeed
	w1 := httptest.NewRecorder()
	wrapped.ServeHTTP(w1, req)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Immediate second request should be rate limited
	w2 := httptest.NewRecorder()
	wrapped.ServeHTTP(w2, req)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
}

func TestResponseWriter_CaptureStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Write header
	rw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rw.statusCode)
}

func TestResponseWriter_CaptureBytesWritten(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Write data
	data := []byte("test response")
	n, err := rw.Write(data)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, len(data), rw.bytesWritten)

	// Write more data
	moreData := []byte(" more data")
	n, err = rw.Write(moreData)
	assert.NoError(t, err)
	assert.Equal(t, len(data)+len(moreData), rw.bytesWritten)
}
