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
	"testing"
)

// forgedSource returns a distinct value for a spoofed X-Forwarded-For header.
//
// THREE varying octets, not two. A two-octet generator yields only 65,536
// distinct values, which is fewer than maxRateLimiterBuckets — so a caller
// asking for 102,000 of them would silently get 65,536, never reach the cap,
// and pass against the very defect it was written to catch. The generator's
// range has to exceed the bound under test.
func forgedSource(i int) string {
	return fmt.Sprintf("198.%d.%d.%d", (i>>16)&0xff, (i>>8)&0xff, i&0xff)
}

// boundsSource returns a distinct IPv4 source for index i. 10.0.0.0/8 gives
// 16M addresses, comfortably more than maxRateLimiterBuckets.
func boundsSource(i int) string {
	return fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xff, (i>>8)&0xff, i&0xff)
}

// TestRateLimiter_BucketMapIsBounded is the property #167 exists for: the map
// must stop growing, whatever the source count.
//
// Without the cap this map is limited only by how many distinct sources a
// sender can produce, and nothing throttles that — a NEW key is always
// admitted, because it is created with a full bucket, so the limiter
// constrains reuse and never creation. Measured at ~140 bytes per entry, that
// is ~136 MB per million sources.
func TestRateLimiter_BucketMapIsBounded(t *testing.T) {
	rl := newBoundedRateLimiter(10, maxRateLimiterBuckets)

	// Drive well past the cap from unique sources.
	const excess = 5000
	for i := range maxRateLimiterBuckets + excess {
		rl.Allow(boundsSource(i))
	}

	rl.mu.RLock()
	size := len(rl.buckets)
	rl.mu.RUnlock()

	if size > maxRateLimiterBuckets {
		t.Fatalf("bucket map holds %d entries after %d distinct sources, cap is %d: the map is unbounded and a flood of distinct sources is a memory-exhaustion path",
			size, maxRateLimiterBuckets+excess, maxRateLimiterBuckets)
	}
}

// TestRateLimiter_CapIsNotABypass pins the reason the cap does not evict.
//
// The tempting implementation is to evict an entry to make room for a new one.
// That turns eviction into the attacker's reset: a source whose bucket is
// dropped gets a FRESH, FULL bucket on its next request, so spending the map
// is how you clear your own limit — and an attacker who can drive eviction can
// flush a victim's bucket deliberately.
//
// The assertion is on bucket IDENTITY, not on a later request being refused.
// The behavioural form looks more natural and is wrong: a token bucket refills
// with wall-clock time, so "still refused after filling the map" actually
// measures how long the fill loop took. It passes normally and fails under
// -race purely because -race is slower — a timing bet dressed as a property.
// Identity is exact and time-independent: an eviction replaces the bucket, and
// nothing else does.
func TestRateLimiter_CapIsNotABypass(t *testing.T) {
	rl := newBoundedRateLimiter(10, maxRateLimiterBuckets)

	victim := "203.0.113.7"
	// Spend the whole allowance, so a reset would be observable.
	for range 40 {
		rl.Allow(victim)
	}

	rl.mu.RLock()
	before := rl.buckets[victim]
	rl.mu.RUnlock()
	if before == nil {
		t.Fatal("victim has no bucket after being served; the test cannot detect a reset")
	}

	// Fill the map from unrelated sources, well past the cap.
	for i := range maxRateLimiterBuckets + 1000 {
		rl.Allow(boundsSource(i))
	}

	rl.mu.RLock()
	after := rl.buckets[victim]
	rl.mu.RUnlock()

	switch {
	case after == nil:
		t.Fatal("the victim's bucket was evicted to make room: its next request gets a fresh, full bucket, so spending the map is how a source clears its own limit — or flushes someone else's")
	case after != before:
		t.Fatal("the victim's bucket was replaced while the map filled: eviction-and-reinsert resets the allowance, which is the same bypass")
	}
}

// TestRateLimiter_CapServesNewcomersFromSharedAllowance pins the other half of
// the failure mode. Refusing every un-tracked source once the map is full would
// hand an attacker a global denial of service for the price of filling a map.
//
// The honest property is narrower than "newcomers are fine", and worth stating
// exactly: past the cap a source is served from a SHARED allowance. It is not
// categorically refused, but it does contend with everything else past the cap
// — so under a sustained flood a newcomer competes for the overflow rate
// rather than getting a private bucket. That is the trade the cap buys, and it
// is still strictly better than refusing outright.
func TestRateLimiter_CapServesNewcomersFromSharedAllowance(t *testing.T) {
	const rate = 10
	rl := newBoundedRateLimiter(rate, maxRateLimiterBuckets)

	// Fill the map to exactly the cap. Each source draws only from its own
	// fresh bucket, so the overflow bucket is still untouched here.
	for i := range maxRateLimiterBuckets {
		rl.Allow(boundsSource(i))
	}

	// A source past the cap gets no bucket of its own, and must still be
	// served rather than refused.
	if !rl.Allow("198.51.100.42") {
		t.Fatal("a new source was refused once the map was full: the cap locks everyone out, which is a global denial of service an attacker triggers by filling the map")
	}

	// It is a SHARED allowance, not a private one: drain it and a different
	// newcomer is refused, proving they contend rather than each getting a
	// full bucket (which would make the cap meaningless).
	for range rate {
		rl.Allow("198.51.100.43")
	}
	if rl.Allow("198.51.100.44") {
		t.Fatal("a third newcomer was admitted after the shared allowance was drained: sources past the cap are getting private buckets, so the cap bounds memory but not admission")
	}
}

// TestLimiterKey_AggregatesIPv6ByPrefix pins the keying decision.
//
// A /64 is the smallest routine IPv6 allocation, so without aggregation one
// subscriber owns 2^64 keys — and, just as importantly, an IPv6 client evades
// the rate limit entirely by rotating within its own prefix, which makes the
// limit IPv4-only in practice.
func TestLimiterKey_AggregatesIPv6ByPrefix(t *testing.T) {
	tests := []struct {
		name  string
		a, b  string
		same  bool
		about string
	}{
		{
			name: "same /64 shares a key", a: "2001:db8:1:2::1", b: "2001:db8:1:2:ffff:ffff:ffff:ffff",
			same: true, about: "two addresses in one allocation must share an allowance",
		},
		{
			name: "different /64 does not", a: "2001:db8:1:2::1", b: "2001:db8:1:3::1",
			same: false, about: "separate allocations must not share an allowance",
		},
		{
			name: "IPv4 is untouched", a: "192.0.2.1", b: "192.0.2.1",
			same: true, about: "a /32 is already the allocation unit",
		},
		{
			name: "distinct IPv4 stays distinct", a: "192.0.2.1", b: "192.0.2.2",
			same: false, about: "IPv4 aggregation would be over-limiting",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ka, kb := limiterKey(tc.a), limiterKey(tc.b)
			if (ka == kb) != tc.same {
				t.Fatalf("limiterKey(%q)=%q, limiterKey(%q)=%q — same=%v, want same=%v: %s",
					tc.a, ka, tc.b, kb, ka == kb, tc.same, tc.about)
			}
		})
	}
}

// TestLimiterKey_PassesThroughNonAddresses guards the getClientIP case.
// That helper returns whatever an X-Forwarded-For header contained, which need
// not be an address at all. Inventing structure for such a value would hide
// that it is untrusted; it must pass through unchanged.
func TestLimiterKey_PassesThroughNonAddresses(t *testing.T) {
	for _, v := range []string{"", "not-an-ip", "example.com", "1.2.3.4.5", "2001:db8::/64"} {
		if got := limiterKey(v); got != v {
			t.Errorf("limiterKey(%q) = %q, want it returned unchanged", v, got)
		}
	}
}

// TestLimiterKey_IPv4InIPv6IsNotAggregated pins a case that would otherwise
// collapse unrelated IPv4 clients together: ::ffff:a.b.c.d is an IPv4 address
// in IPv6 clothing, and taking its /64 would put every IPv4 source in the
// world into one bucket.
func TestLimiterKey_IPv4InIPv6IsNotAggregated(t *testing.T) {
	a := limiterKey("::ffff:192.0.2.1")
	b := limiterKey("::ffff:192.0.2.2")
	if a == b {
		t.Fatalf("two IPv4-mapped IPv6 addresses collapsed to the same key (%q): every IPv4 client would share one bucket", a)
	}
}

// TestRateLimitMiddleware_AggregatesBracketedIPv6 drives the real middleware
// with the RemoteAddr shape Go actually produces, which is the gap the
// limiterKey unit tests missed: they fed bare addresses, and the middleware
// never sees a bare address.
//
// net/http sets RemoteAddr to "[2001:db8::1]:1234" for an IPv6 peer. Aggregation
// is worthless if it only works on values the code path cannot produce — the
// rotation bypass it exists to close lives precisely here.
func TestRateLimitMiddleware_AggregatesBracketedIPv6(t *testing.T) {
	const rate = 10
	reached := 0
	h := RateLimitMiddleware(rate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	}))

	// Two addresses in ONE /64, each with a port, bracketed the way net/http
	// writes them.
	addrs := []string{"[2001:db8:aa:bb::1]:1234", "[2001:db8:aa:bb::9999]:5678"}
	const attempts = rate * 4
	for i := range attempts {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addrs[i%len(addrs)]
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	if reached > rate+1 {
		t.Fatalf("%d of %d requests reached the handler from two addresses in one /64, want at most ~%d: the addresses did not share an allowance, so a client bypasses the limit by rotating within its own prefix",
			reached, attempts, rate+1)
	}
}

// TestGetClientIP_IPv6RemoteAddr pins the parsing getClientIP does on the
// transport peer address. It fed the limiter a bracketed string that no IP
// parser accepts, and — worse — mangled a bare IPv6 address by splitting on
// its last colon, so "2001:db8::1" became "2001:db8:". That value also reaches
// audit records, where a mangled address is worse than an absent one.
func TestGetClientIP_IPv6RemoteAddr(t *testing.T) {
	tests := []struct {
		remoteAddr string
		want       string
	}{
		{"[2001:db8:1:2::1]:1234", "2001:db8:1:2::1"},
		{"192.0.2.1:443", "192.0.2.1"},
		{"2001:db8::1", "2001:db8::1"}, // no port: must not be split on a colon
		{"192.0.2.1", "192.0.2.1"},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tc.remoteAddr
		if got := getClientIP(req); got != tc.want {
			t.Errorf("getClientIP(RemoteAddr=%q) = %q, want %q", tc.remoteAddr, got, tc.want)
		}
	}
}

// TestLimiterKey_ToleratesBrackets covers the other source of bracketed
// values: getClientIP also returns X-Forwarded-For content verbatim, and that
// header legitimately carries "[2001:db8::1]" forms. A bracketed address must
// aggregate the same as its bare form, or the bracketing itself becomes the
// bypass.
func TestLimiterKey_ToleratesBrackets(t *testing.T) {
	bare := limiterKey("2001:db8:1:2::1")
	bracketed := limiterKey("[2001:db8:1:2::1]")
	if bare != bracketed {
		t.Fatalf("limiterKey(bare)=%q but limiterKey(bracketed)=%q: adding brackets yields a different bucket, so bracketing is itself a bypass", bare, bracketed)
	}
}

// TestRateLimiter_UnboundedByDefault pins the rule that a cap is opt-in.
//
// A cap and a forgeable key combine into a denial of service that neither has
// alone: an attacker mints keys to fill the cap, drains the shared overflow,
// and every legitimate source not already tracked is then served from that
// empty bucket. So bounding is only ever safe on a limiter keyed to the
// transport peer, and the default must be unbounded.
func TestRateLimiter_UnboundedByDefault(t *testing.T) {
	rl := newRateLimiter(10)

	if rl.maxBuckets != 0 {
		t.Fatalf("newRateLimiter produced maxBuckets=%d, want 0: a cap must be opted into, never inherited", rl.maxBuckets)
	}
	if rl.overflow != nil {
		t.Fatal("newRateLimiter produced an overflow bucket: an unbounded limiter has nothing to overflow into, and its presence invites a cap being added without the key being checked")
	}
}

// TestRateLimitMiddleware_ForgedHeadersCannotThrottleAnotherPeer is the
// regression test for the defect that made this the shape it is.
//
// RateLimitMiddleware keys on getClientIP, which honours X-Forwarded-For with
// no trusted-proxy allowlist (#156). An earlier revision of this change capped
// that limiter too, which handed one peer a service-wide throttle: mint 100k
// forged header values to fill the cap, keep minting to drain the shared
// overflow, and every legitimate newcomer is then served from the empty
// bucket.
//
// The fix was to leave that limiter unbounded until #156 makes its key
// trustworthy. This test pins the consequence: forged headers from one peer
// must not consume anything a different peer depends on.
func TestRateLimitMiddleware_ForgedHeadersCannotThrottleAnotherPeer(t *testing.T) {
	const rate = 10
	h := RateLimitMiddleware(rate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// One attacker peer, a fresh forged identity on every request. The count
	// must EXCEED maxRateLimiterBuckets: with fewer, a bounded limiter would
	// never reach its cap and this test would pass against the very defect it
	// exists to catch.
	for i := range maxRateLimiterBuckets + 2000 {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "203.0.113.9:5555"
		req.Header.Set("X-Forwarded-For", forgedSource(i))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	// An unrelated client, never seen before, must still be served.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.77:4444"
	req.Header.Set("X-Forwarded-For", "192.0.2.77")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusTooManyRequests {
		t.Fatal("an unrelated client was throttled after one peer sent forged X-Forwarded-For values: the limiter is bounded on a forgeable key, so filling it is a service-wide denial of service")
	}
}
