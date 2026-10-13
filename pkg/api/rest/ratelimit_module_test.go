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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// moduleRateLimit is small so a handful of sequential requests exhausts a
// bucket; its refill (one token per 500 ms) is far slower than the requests.
const moduleRateLimit = 2

// rateLimitTestServer builds a REST server whose global limit is limit and
// which trusts the given proxies.
func rateLimitTestServer(t *testing.T, limit int, trustedProxies ...string) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	cfg := DefaultConfig()
	cfg.RateLimit = limit
	cfg.TrustedProxies = trustedProxies
	server, err := NewServer(testutil.NewMockQuicBaaS(), cfg, log)
	require.NoError(t, err)
	t.Cleanup(func() { server.Close() })
	return server
}

// moduleOKSpec is a public module route whose handler always answers 200,
// so any other status comes from the middleware BindRoute applies.
func moduleOKSpec() RouteSpec {
	return RouteSpec{
		Method:  http.MethodGet,
		Pattern: "/api/v1/module-ratelimit-test",
		Group:   GroupAPI,
		Auth:    RouteAuthPublic,
		Handler: func(s *Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}
		},
	}
}

// serveFrom sends one GET for path to h from source.
func serveFrom(h http.Handler, path, source string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = source
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// rateLimitBody is the part of a 429 body that must not depend on which
// route answered it (timestamp and request id are per request).
type rateLimitBody struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
}

func decodeRateLimitBody(t *testing.T, rec *httptest.ResponseRecorder) rateLimitBody {
	t.Helper()
	var body rateLimitBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// requireRateLimited asserts rec is the limiter's typed refusal: 429, the
// rate_limited code, retryable, and a Retry-After a client can wait on.
func requireRateLimited(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, rateLimitRetryAfter, rec.Header().Get("Retry-After"))
	body := decodeRateLimitBody(t, rec)
	require.Equal(t, errors.ErrRateLimited.String(), body.Code)
	require.True(t, body.Retryable, "a rate-limited request is retryable")
}

// requireNotRateLimited asserts rec carries none of the limiter's refusal.
func requireNotRateLimited(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.NotEqual(t, http.StatusTooManyRequests, rec.Code)
	require.Empty(t, rec.Header().Get("Retry-After"))
}

// TestRateLimit_RefusalCarriesTypedCode pins the 429 body a client keys on:
// core and module routes both answer with the rate_limited code, retryable
// set, and Retry-After, so a limiter refusal is told apart without parsing
// the message.
func TestRateLimit_RefusalCarriesTypedCode(t *testing.T) {
	server := rateLimitTestServer(t, moduleRateLimit)
	routes := map[string]struct {
		handler http.Handler
		path    string
	}{
		"core route":   {server.GetRouter(), "/health/ping"},
		"module route": {server.BindRoute(moduleOKSpec()), "/api/v1/module-ratelimit-test"},
	}
	for name, route := range routes {
		t.Run(name, func(t *testing.T) {
			source := uniqueSource()
			for range moduleRateLimit {
				serveFrom(route.handler, route.path, source)
			}
			requireRateLimited(t, serveFrom(route.handler, route.path, source))
		})
	}
}

// TestBindRoute_ModuleRoutesShareGlobalRateLimit pins #158: a module route
// (served by the unified API server through BindRoute, outside the core
// router's middleware chain) draws on the SAME per-client-IP budget as the
// core routes, answers with the same 429, and leaves other clients alone.
func TestBindRoute_ModuleRoutesShareGlobalRateLimit(t *testing.T) {
	server := rateLimitTestServer(t, moduleRateLimit)
	core := server.GetRouter()
	module := server.BindRoute(moduleOKSpec())
	const modulePath = "/api/v1/module-ratelimit-test"

	t.Run("module route is limited on its own", func(t *testing.T) {
		source := uniqueSource()
		for i := range moduleRateLimit {
			rec := serveFrom(module, modulePath, source)
			require.Equal(t, http.StatusOK, rec.Code, "request %d inside the burst", i+1)
		}
		rec := serveFrom(module, modulePath, source)
		require.Equal(t, http.StatusTooManyRequests, rec.Code,
			"a module route past the limit must be rate limited")
	})

	t.Run("one budget across core and module routes", func(t *testing.T) {
		source := uniqueSource()
		for range moduleRateLimit {
			serveFrom(core, "/health/ping", source)
		}
		coreLimited := serveFrom(core, "/health/ping", source)
		require.Equal(t, http.StatusTooManyRequests, coreLimited.Code)

		moduleLimited := serveFrom(module, modulePath, source)
		require.Equal(t, http.StatusTooManyRequests, moduleLimited.Code,
			"the core routes spent this client's budget; a module route must not grant a second one")
		coreBody := decodeRateLimitBody(t, coreLimited)
		require.Equal(t, coreBody, decodeRateLimitBody(t, moduleLimited))
		require.Equal(t, coreLimited.Header().Get("Content-Type"), moduleLimited.Header().Get("Content-Type"))
		require.Equal(t, coreLimited.Header().Get("Retry-After"), moduleLimited.Header().Get("Retry-After"))
		require.Equal(t, errors.ErrRateLimited.String(), coreBody.Code,
			"a limiter refusal must carry the typed rate-limit code")

		other := serveFrom(module, modulePath, uniqueSource())
		require.Equal(t, http.StatusOK, other.Code, "a second client must keep its own budget")
	})
}

// TestBindRoute_ModuleRateLimitHonoursTrustedProxies checks module routes
// key their bucket on the client a configured proxy vouches for, as core
// routes do (#156), rather than putting every client behind the proxy into
// the proxy's one bucket.
func TestBindRoute_ModuleRateLimitHonoursTrustedProxies(t *testing.T) {
	const proxy = "192.0.2.1"
	server := rateLimitTestServer(t, moduleRateLimit, proxy)
	module := server.BindRoute(moduleOKSpec())

	viaProxy := func(client string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/module-ratelimit-test", nil)
		req.RemoteAddr = proxy + ":443"
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		module.ServeHTTP(rec, req)
		return rec.Code
	}

	for range moduleRateLimit {
		require.Equal(t, http.StatusOK, viaProxy("198.51.100.7"))
	}
	require.Equal(t, http.StatusTooManyRequests, viaProxy("198.51.100.7"))
	require.Equal(t, http.StatusOK, viaProxy("198.51.100.8"),
		"a second client behind the same trusted proxy must keep its own budget")
}

// TestBindRoute_RateLimitOptOutCoversModuleRoutes checks the opt-out turns
// the limiter off for module routes as well as core routes.
func TestBindRoute_RateLimitOptOutCoversModuleRoutes(t *testing.T) {
	server := rateLimitTestServer(t, RateLimitDisabled)
	core := server.GetRouter()
	module := server.BindRoute(moduleOKSpec())
	source := uniqueSource()
	for range 5 * moduleRateLimit {
		requireNotRateLimited(t, serveFrom(core, "/health/ping", source))
		requireNotRateLimited(t, serveFrom(module, "/api/v1/module-ratelimit-test", source))
	}
}
