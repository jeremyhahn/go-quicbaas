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
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// rateLimitTestRouter builds a REST server from cfg and returns its router.
func rateLimitTestRouter(t *testing.T, cfg *Config) http.Handler {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	server, err := NewServer(testutil.NewMockQuicBaaS(), cfg, log)
	require.NoError(t, err)
	t.Cleanup(func() { server.Close() })
	return server.GetRouter()
}

// burst releases n concurrent requests from one source at once and counts
// how many the router answered with 429. The bucket starts full, so the
// first rate requests always pass; releasing several times the rate
// together outruns the refill even on a slow, race-instrumented run.
func burst(router http.Handler, source string, n int) (limited int64) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodGet, "/health/ping", nil)
			req.RemoteAddr = source
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code == http.StatusTooManyRequests {
				atomic.AddInt64(&limited, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	return limited
}

func TestConfig_EffectiveRateLimit(t *testing.T) {
	tests := []struct {
		name      string
		rateLimit int
		want      int
	}{
		{"zero resolves to the default", 0, DefaultRateLimit},
		{"explicit value is kept", 50, 50},
		{"documented opt-out disables", RateLimitDisabled, 0},
		{"any negative value disables", -7, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{RateLimit: tt.rateLimit}
			if got := cfg.EffectiveRateLimit(); got != tt.want {
				t.Fatalf("EffectiveRateLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDefaultConfig_EnablesRateLimit(t *testing.T) {
	if got := DefaultConfig().EffectiveRateLimit(); got != DefaultRateLimit {
		t.Fatalf("DefaultConfig().EffectiveRateLimit() = %d, want %d", got, DefaultRateLimit)
	}
}

// TestServer_GlobalRateLimitIsOnByDefault covers both ways an operator gets
// the default: DefaultConfig, and a Config whose RateLimit was never set.
func TestServer_GlobalRateLimitIsOnByDefault(t *testing.T) {
	configs := map[string]func() *Config{
		"DefaultConfig": DefaultConfig,
		"zero RateLimit": func() *Config {
			cfg := DefaultConfig()
			cfg.RateLimit = 0
			return cfg
		},
	}
	for name, build := range configs {
		t.Run(name, func(t *testing.T) {
			router := rateLimitTestRouter(t, build())
			const requests = 5 * DefaultRateLimit
			limited := burst(router, uniqueSource(), requests)
			if limited == 0 {
				t.Fatalf("no 429 in a burst of %d requests from one source: the global limiter is off", requests)
			}
			if passed := requests - int(limited); passed < DefaultRateLimit {
				t.Fatalf("%d requests passed, want at least the %d-request burst", passed, DefaultRateLimit)
			}
		})
	}
}

func TestServer_RateLimitOptOutDisablesGlobalLimiter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RateLimit = RateLimitDisabled
	router := rateLimitTestRouter(t, cfg)

	if limited := burst(router, uniqueSource(), 3*DefaultRateLimit); limited != 0 {
		t.Fatalf("%d requests got 429 with the global limiter disabled, want 0", limited)
	}
}

// TestServer_OAuth2LimiterStacksOnGlobalLimiter checks that the
// endpoint-scoped OAuth2 limiter still applies with the global limiter on:
// a source well inside the global allowance is still held to the OAuth2
// rate on /oauth2/token.
func TestServer_OAuth2LimiterStacksOnGlobalLimiter(t *testing.T) {
	server, _, _, _, cleanup := setupIAMTestServer(t)
	defer cleanup()
	require.Equal(t, DefaultRateLimit, server.config.EffectiveRateLimit())
	router := server.GetRouter()

	source := uniqueSource()
	limitedAt := 0
	for i := 1; i <= 2*oauth2ClientAuthRateLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
		req.RemoteAddr = source
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limitedAt = i
			break
		}
	}
	if limitedAt == 0 || limitedAt > oauth2ClientAuthRateLimit+1 {
		t.Fatalf("first 429 on /oauth2/token at request %d, want request %d: the OAuth2 limiter is not applied under the global limiter",
			limitedAt, oauth2ClientAuthRateLimit+1)
	}
}
