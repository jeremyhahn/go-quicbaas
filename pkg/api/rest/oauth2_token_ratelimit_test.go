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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The OAuth2 endpoints that authenticate their caller through
// ValidateClientSecret, and so spend a PBKDF2 derivation per unauthenticated
// attempt. These are the routes the shared limiter must cover (#118 F1).
var oauth2ClientAuthRoutes = []struct {
	method  string
	pattern string
}{
	{http.MethodPost, "/oauth2/token"},
	{http.MethodPost, "/oauth2/revoke"},
	{http.MethodPost, "/oauth2/introspect"},
}

// The sibling GroupOAuth2Core routes that must NOT be limited. Neither lets an
// anonymous caller reach the KDF: token-mtls authenticates by client
// certificate on the TLS handshake, userinfo by bearer token, and authorize
// verifies no client secret at all.
var oauth2UnlimitedRoutes = []struct {
	method  string
	pattern string
}{
	{http.MethodGet, "/oauth2/authorize"},
	{http.MethodPost, "/oauth2/token-mtls"},
	{http.MethodGet, "/oauth2/userinfo"},
}

// sourceCounter hands out a distinct source address to every caller of
// uniqueSource.
//
// oauth2ClientAuthLimiter is a package-level singleton (deliberately — see
// middleware.go), so its buckets outlive an individual test AND outlive an
// individual -count iteration. A test pinned to a literal address would drain
// that address's bucket on the first iteration and then measure a
// pre-exhausted limiter on every later one, which is the difference between
// "passes" and "passes repeatedly". Every test here draws its sources from
// here so that -count=N exercises the same fresh-bucket path N times.
var sourceCounter atomic.Uint64

// uniqueSource returns a RemoteAddr no other test has used, drawn from
// 10.0.0.0/8 for the address space (16M distinct sources, far past any -count
// this suite will see).
func uniqueSource() string {
	n := sourceCounter.Add(1)
	return fmt.Sprintf("10.%d.%d.%d:44444", (n>>16)&0xff, (n>>8)&0xff, n&0xff)
}

// countingHandler records how many requests reached the wrapped handler.
func countingHandler(n *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*n++
		w.WriteHeader(http.StatusOK)
	}
}

// drive sends one request at h and returns the status code.
func drive(t *testing.T, h http.HandlerFunc, method, target, remoteAddr string, headers map[string]string) int {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Code
}

// specHandler returns the bound handler for one route from the live table, or
// fails the test if the route is gone.
func specHandler(t *testing.T, server *Server, method, pattern string) http.HandlerFunc {
	t.Helper()
	for _, spec := range Routes() {
		if spec.Method == method && spec.Pattern == pattern {
			return spec.Handler(server)
		}
	}
	t.Fatalf("no %s %s RouteSpec in Routes(): the endpoint moved, and this guard no longer covers it", method, pattern)
	return nil
}

// TestRateLimitOAuth2ClientAuth_BoundsOneSource is the property F1 needs: a
// single source cannot spend the host's cores on derivations without limit.
func TestRateLimitOAuth2ClientAuth_BoundsOneSource(t *testing.T) {
	reached := 0
	h := RateLimitOAuth2ClientAuth(countingHandler(&reached))

	// The bucket starts full at oauth2ClientAuthRateLimit and refills at that
	// rate, so a burst beyond the capacity must be refused within the same
	// second.
	const attempts = oauth2ClientAuthRateLimit * 5
	source := uniqueSource()
	refused := 0
	for range attempts {
		if drive(t, h, http.MethodPost, "/oauth2/token", source, nil) == http.StatusTooManyRequests {
			refused++
		}
	}

	if refused == 0 {
		t.Fatalf("%d rapid requests from one source were all admitted; the endpoint is unbounded", attempts)
	}
	if reached > oauth2ClientAuthRateLimit+1 {
		t.Fatalf("%d requests reached the KDF from one source in a burst, want at most ~%d",
			reached, oauth2ClientAuthRateLimit+1)
	}
}

// TestRateLimitOAuth2ClientAuth_IgnoresForwardedHeaders is the reason this
// limiter does not use getClientIP. Keying on a caller-controlled header is not
// a limit: a fresh forged value yields a fresh full bucket, so every request is
// admitted and the bucket map grows without bound. If this test fails, the
// endpoints are bypassable by anyone who can set a header.
func TestRateLimitOAuth2ClientAuth_IgnoresForwardedHeaders(t *testing.T) {
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		t.Run(header, func(t *testing.T) {
			reached := 0
			h := RateLimitOAuth2ClientAuth(countingHandler(&reached))

			const attempts = oauth2ClientAuthRateLimit * 5
			source := uniqueSource()
			refused := 0
			for i := range attempts {
				// A different forged client identity on every request, from
				// one real peer.
				forged := map[string]string{header: "198.51.100." + strconv.Itoa(i%256)}
				if drive(t, h, http.MethodPost, "/oauth2/token", source, forged) == http.StatusTooManyRequests {
					refused++
				}
			}

			if refused == 0 {
				t.Fatalf("all %d requests were admitted while rotating %s; the limiter keys on a caller-controlled header and is bypassable",
					attempts, header)
			}
			if reached > oauth2ClientAuthRateLimit+1 {
				t.Fatalf("%d requests reached the KDF while rotating %s, want at most ~%d",
					reached, header, oauth2ClientAuthRateLimit+1)
			}
		})
	}
}

// TestRateLimitOAuth2ClientAuth_SeparatesDistinctSources confirms the bound is
// per-source and not global: one flooding client must not lock everyone out.
func TestRateLimitOAuth2ClientAuth_SeparatesDistinctSources(t *testing.T) {
	reached := 0
	h := RateLimitOAuth2ClientAuth(countingHandler(&reached))

	// Exhaust one source.
	flooder := uniqueSource()
	for range oauth2ClientAuthRateLimit * 3 {
		drive(t, h, http.MethodPost, "/oauth2/token", flooder, nil)
	}

	// A different source still gets through.
	if code := drive(t, h, http.MethodPost, "/oauth2/token", uniqueSource(), nil); code != http.StatusOK {
		t.Fatalf("an unrelated source got %d after another source was throttled; the limit is global, not per-source", code)
	}
}

// TestRouteTable_RateLimitsEveryClientAuthEndpoint guards the unified API
// server's path to each of the three endpoints.
//
// Their GroupOAuth2Core specs are bound by apimodule.RegisterHTTP (through
// ModuleRoutes and Server.BindRoute), which applies no middleware of its own to
// a RouteAuthPublic route. So if a limiter is not bound at the spec, the
// unified server serves that endpoint's KDF unmetered while the limiter's own
// unit tests stay green. Server.registerRoutes is NOT a consumer of this group
// — the legacy listener gets these from IAMHandler.RegisterRoutes, covered
// below.
//
// Each endpoint is its own subtest so that unwrapping exactly one of them names
// exactly one failure.
func TestRouteTable_RateLimitsEveryClientAuthEndpoint(t *testing.T) {
	server, _, _, _, cleanup := setupIAMTestServer(t)
	defer cleanup()

	for _, route := range oauth2ClientAuthRoutes {
		t.Run(route.pattern, func(t *testing.T) {
			handler := specHandler(t, server, route.method, route.pattern)

			const attempts = oauth2ClientAuthRateLimit * 5
			source := uniqueSource()
			throttled := 0
			for range attempts {
				if drive(t, handler, route.method, route.pattern, source, nil) == http.StatusTooManyRequests {
					throttled++
				}
			}

			if throttled == 0 {
				t.Fatalf("the %s %s route spec admitted all %d rapid requests from one source: the unified server (apimodule.RegisterHTTP) serves an unbounded path to the KDF",
					route.method, route.pattern, attempts)
			}
		})
	}
}

// TestRouteTable_ClientAuthEndpointsShareOneBucket pins the ONE-bucket
// decision: the three endpoints draw from a single per-source allowance.
//
// The capped resource is total KDF CPU per source, not per-route fairness. If
// each route were given its own limiter, a single sender's effective budget
// would be three times the configured rate for the same derivation work — and
// every other test in this file would still pass, because each drives one route
// in isolation. Exhausting the budget on one endpoint and then finding a
// sibling already refused is what tells one bucket from three.
func TestRouteTable_ClientAuthEndpointsShareOneBucket(t *testing.T) {
	server, _, _, _, cleanup := setupIAMTestServer(t)
	defer cleanup()

	token := specHandler(t, server, http.MethodPost, "/oauth2/token")
	source := uniqueSource()

	// Spend the whole per-source allowance on /oauth2/token.
	for range oauth2ClientAuthRateLimit * 3 {
		drive(t, token, http.MethodPost, "/oauth2/token", source, nil)
	}

	// Every sibling must already be refused for that source.
	for _, route := range oauth2ClientAuthRoutes[1:] {
		handler := specHandler(t, server, route.method, route.pattern)
		if code := drive(t, handler, route.method, route.pattern, source, nil); code != http.StatusTooManyRequests {
			t.Errorf("%s %s returned %d for a source that had already exhausted the allowance on /oauth2/token, want %d: the endpoints hold separate buckets, so one sender's KDF budget is %d× the configured rate",
				route.method, route.pattern, code, http.StatusTooManyRequests, len(oauth2ClientAuthRoutes))
		}
	}
}

// TestRouteTable_LimitsOnlyClientAuthEndpoints pins the SCOPE of the bound:
// exactly the three client-authenticating endpoints are limited, and nothing
// else in the group is.
//
// The alternative F1 shape was a whole-group limit, and the difference is
// invisible to every test above — they only drive the limited routes, so a
// group-wide wrapper would leave them all green while throttling the
// authorization endpoint, which spends no KDF and carries interactive browser
// traffic. Driving each sibling far past the allowance and requiring that it is
// never refused is what tells the two apart.
func TestRouteTable_LimitsOnlyClientAuthEndpoints(t *testing.T) {
	server, _, _, _, cleanup := setupIAMTestServer(t)
	defer cleanup()

	for _, route := range oauth2UnlimitedRoutes {
		t.Run(route.pattern, func(t *testing.T) {
			handler := specHandler(t, server, route.method, route.pattern)

			source := uniqueSource()
			for i := range oauth2ClientAuthRateLimit * 5 {
				if drive(t, handler, route.method, route.pattern, source, nil) == http.StatusTooManyRequests {
					t.Fatalf("%s %s was rate limited on request %d: the bound is applied more widely than the client-authenticating endpoints, and this route reaches no KDF as an anonymous caller",
						route.method, route.pattern, i+1)
				}
			}
		})
	}
}

// TestIAMHandler_RegisterRoutes_RateLimitsClientAuthEndpoints pins the
// registrations the route table does not feed.
//
// IAMHandler.RegisterRoutes builds its chi routes by hand, so the spec-level
// wrappers do not reach them and each carries its own. The limiter being
// correct is worth nothing if a registration path forgets to wrap a handler,
// and removing a wrapper is exactly the kind of edit that leaves a whole
// unit-test suite green.
func TestIAMHandler_RegisterRoutes_RateLimitsClientAuthEndpoints(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	for _, route := range oauth2ClientAuthRoutes {
		t.Run(route.pattern, func(t *testing.T) {
			// Far more than the per-second allowance, from one source, as fast
			// as the test can drive it.
			const attempts = oauth2ClientAuthRateLimit * 5
			source := uniqueSource()
			throttled := 0
			for range attempts {
				req := httptest.NewRequest(route.method, route.pattern, nil)
				req.RemoteAddr = source
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code == http.StatusTooManyRequests {
					throttled++
				}
			}

			if throttled == 0 {
				t.Fatalf("%s %s admitted all %d rapid requests from one source: the registered route is NOT rate limited, so the legacy listener serves an unbounded path to the KDF",
					route.method, route.pattern, attempts)
			}
		})
	}
}
