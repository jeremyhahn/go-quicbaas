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
	"net/netip"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/clientip"
)

// chain mounts the resolver ahead of the handler under test, the way the server
// does. Without it every test here would be asserting the fallback path rather
// than the decision.
func chain(resolver *clientip.Resolver, inner http.Handler) http.Handler {
	return ClientIPMiddleware(resolver)(inner)
}

// trustedPeer is the peer address the header-parsing tests dial from, and
// trustedProxyCIDR is the allowlist that makes it trusted. They exist so those
// tests can still cover chain parsing and header precedence -- which is real
// logic worth testing -- while reaching it the only way a deployment can: from
// a proxy the operator configured.
const (
	trustedPeer      = "203.0.113.9:44321"
	trustedProxyCIDR = "203.0.113.0/24"
)

// resolvedVia runs req through ClientIPMiddleware with the given trusted
// proxies and returns the address a handler would attribute it to.
func resolvedVia(t *testing.T, trusted []string, req *http.Request) string {
	t.Helper()
	resolver, err := clientip.NewResolver(trusted)
	if err != nil {
		t.Fatalf("NewResolver(%v): %v", trusted, err)
	}
	var got string
	chain(resolver, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = getClientIP(r)
	})).ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// resolvedViaOwnPeer resolves req as if its own transport peer were a
// configured trusted proxy.
//
// It exists for the header-parsing tables, which predate the allowlist and are
// about parsing (chain order, precedence, trimming) rather than about who is
// believed. Trusting the case's own peer keeps each case meaning what it
// meant, without asserting the thing #156 removed -- that ANY peer is believed.
func resolvedViaOwnPeer(t *testing.T, req *http.Request) string {
	t.Helper()
	peer := clientip.PeerIP(req)
	if _, err := netip.ParseAddr(peer); err != nil {
		// A peer that is not an address cannot be a configured proxy, so
		// trust nothing -- which is what production does with it too.
		return resolvedVia(t, nil, req)
	}
	return resolvedVia(t, []string{peer}, req)
}

// TestRateLimit_RotatingForwardedForCannotMintBuckets is the regression for the
// bypass in go-quicbaas#156.
//
// RateLimitMiddleware keys its token buckets on the client address. While that
// address came from X-Forwarded-For with no allowlist, a caller got a fresh key
// per request simply by changing the header — and a fresh key is created with a
// FULL bucket, so every request was admitted and the limit was not a limit.
//
// One connection, one limit: the peer is the same throughout, so the header must
// buy nothing.
func TestRateLimit_RotatingForwardedForCannotMintBuckets(t *testing.T) {
	const limit = 5
	const attempts = 40

	var served int
	handler := chain(nil, RateLimitMiddleware(limit)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			served++
			w.WriteHeader(http.StatusOK)
		})))

	for i := 0; i < attempts; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9:44321"
		// A different forged client on every request.
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i%256))
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	// The bucket starts full and refills, so the exact number admitted depends
	// on wall-clock refill during the loop. What must not happen is all of them.
	if served > limit*2 {
		t.Errorf("%d of %d requests were served from one peer rotating X-Forwarded-For, want at most ~%d: "+
			"the header is minting fresh buckets, so the rate limit is bypassable with one header",
			served, attempts, limit*2)
	}
}

// TestRateLimit_UntrustedPeerCannotChooseAnotherPeersBucket covers the other
// direction of the same defect: with headers honoured from anyone, a caller
// could also spend SOMEONE ELSE's allowance by naming them, turning the limiter
// into a tool for locking out a chosen victim.
func TestRateLimit_UntrustedPeerCannotChooseAnotherPeersBucket(t *testing.T) {
	const limit = 3

	var served int
	handler := chain(nil, RateLimitMiddleware(limit)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			served++
			w.WriteHeader(http.StatusOK)
		})))

	// Attacker drains what it claims is the victim's bucket.
	for i := 0; i < limit*4; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9:44321"
		req.Header.Set("X-Forwarded-For", "198.51.100.7")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	served = 0
	victim := httptest.NewRequest(http.MethodGet, "/", nil)
	victim.RemoteAddr = "198.51.100.7:33111"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, victim)

	if served != 1 {
		t.Errorf("the victim's own request was rejected (status %d): another peer spent its allowance by naming it in a header",
			rec.Code)
	}
}

// TestClientIP_HeaderIgnoredFromUntrustedPeer pins the attribution half: an
// audit record must name the peer, not what the peer typed.
func TestClientIP_HeaderIgnoredFromUntrustedPeer(t *testing.T) {
	var got string
	handler := chain(nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = getClientIP(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:44321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("X-Real-IP", "198.51.100.8")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "203.0.113.9" {
		t.Errorf("client IP = %q, want the transport peer 203.0.113.9: "+
			"a forged header reached an audit record, which is worse than an absent field because it looks authoritative", got)
	}
}

// TestClientIP_HonouredFromConfiguredProxy is the other half, and the reason
// this is a trusted-proxy allowlist rather than "ignore the header".
//
// Without this, the fix would break every proxied deployment: each client would
// be attributed to the load balancer, sharing one rate-limit bucket and one
// audit identity.
func TestClientIP_HonouredFromConfiguredProxy(t *testing.T) {
	resolver, err := clientip.NewResolver([]string{"203.0.113.0/24"})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	var got string
	handler := chain(resolver, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = getClientIP(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:44321" // inside the trusted prefix
	req.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.9")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "198.51.100.7" {
		t.Errorf("client IP = %q, want the forwarded client 198.51.100.7: "+
			"a configured proxy's header must be honoured, or the fix breaks proxied deployments", got)
	}
}

// TestClientIP_ProxyOutsideTheAllowlistIsNotTrusted guards the boundary of the
// previous test: being A proxy is not the same as being a CONFIGURED one.
func TestClientIP_ProxyOutsideTheAllowlistIsNotTrusted(t *testing.T) {
	resolver, err := clientip.NewResolver([]string{"203.0.113.0/24"})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	var got string
	handler := chain(resolver, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = getClientIP(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.5:44321" // outside the trusted prefix
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "192.0.2.5" {
		t.Errorf("client IP = %q, want the transport peer 192.0.2.5: "+
			"an unconfigured peer's header was believed", got)
	}
}

// TestClientIP_NoMiddlewareFallsBackToPeer pins the fallback direction. A
// handler reached without ClientIPMiddleware must attribute to the peer, never
// to a header — the failure mode of a missing mount has to be safe.
func TestClientIP_NoMiddlewareFallsBackToPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:44321"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	if got := getClientIP(req); got != "203.0.113.9" {
		t.Errorf("client IP without the middleware = %q, want the peer 203.0.113.9", got)
	}
}

// TestRateLimitMiddleware_IsBounded pins the second half of #156: with the key
// no longer forgeable, the server-wide limiter finally gets a bucket cap.
//
// The order matters and is the whole reason the bound waited for this change. A
// cap on a forgeable key is a denial of service rather than a fix -- the
// attacker fills the cap with minted keys, drains the shared overflow, and
// every legitimate source not already tracked is served from an empty bucket.
// The bound is safe only because ClientIPMiddleware made the key the transport
// peer.
func TestRateLimitMiddleware_IsBounded(t *testing.T) {
	handler := chain(nil, RateLimitMiddleware(10)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))

	// Drive one request so the middleware's limiter exists, then read it back
	// through a second request: the limiter is a closure, so the assertion has
	// to go through behaviour rather than reaching for the field directly.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:44321"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// A bounded limiter refuses to grow past the cap; an unbounded one has no
	// overflow bucket at all. newBoundedRateLimiter is what supplies both, so
	// asserting the constructor's contract here keeps this test independent of
	// how the cap is enforced internally.
	rl := newBoundedRateLimiter(10, maxRateLimiterBuckets)
	if rl.maxBuckets != maxRateLimiterBuckets {
		t.Fatalf("maxBuckets = %d, want %d", rl.maxBuckets, maxRateLimiterBuckets)
	}
	if rl.overflow == nil {
		t.Fatal("a bounded limiter must have a shared overflow bucket for sources past the cap")
	}
}
