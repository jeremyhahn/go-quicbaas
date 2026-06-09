// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package platformtoken

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
)

// stubLiveness is a programmable LivenessChecker for tests. results is consumed
// in order; the last entry is repeated once exhausted. calls counts invocations.
type stubLiveness struct {
	results []bool
	err     error
	calls   atomic.Int64
}

func (s *stubLiveness) CheckLiveness(_ context.Context, _ string, _ *Claims) (bool, error) {
	n := s.calls.Add(1)
	if s.err != nil {
		return false, s.err
	}
	idx := int(n) - 1
	if idx >= len(s.results) {
		idx = len(s.results) - 1
	}
	return s.results[idx], nil
}

// newAuthzValidator builds a validator with the given liveness/mode/ttl wiring.
func newAuthzValidator(t *testing.T, key *issuerKey, now time.Time, opts Options) *Validator {
	t.Helper()
	opts.Issuer = testIssuer
	opts.Audience = testAudience
	opts.JWKS = key.jwks()
	opts.Clock = fixedNow(now)
	v, err := NewValidator(opts)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return v
}

func bearerToken(t *testing.T, key *issuerKey, now time.Time, scp []string, tid string) string {
	t.Helper()
	return key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1",
		tid: tid, scp: scp,
		exp: now.Add(10 * time.Minute), iat: now,
	})
}

func TestAuthorize_Offline_ScopeDecision(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOffline },
	})

	tok := bearerToken(t, key, now, []string{"keys:get"}, "tenant-1")

	ac, err := v.Authorize(context.Background(), tok, "keys:get", "", "", "")
	if err != nil {
		t.Fatalf("offline allow: %v", err)
	}
	if ac.Subject != "user-1" || ac.TenantID != "tenant-1" {
		t.Fatalf("unexpected context: %+v", ac)
	}

	_, err = v.Authorize(context.Background(), tok, "keys:delete", "", "", "")
	var denied *ScopeDeniedError
	if !errors.As(err, &denied) || denied.Required != "keys:delete" {
		t.Fatalf("expected ScopeDeniedError(keys:delete), got %v", err)
	}
}

func TestAuthorize_Online_InactiveDenied(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	live := &stubLiveness{results: []bool{false}}
	v := newAuthzValidator(t, key, now, Options{
		LivenessChecker: live,
		ModeResolver:    func(string) AuthzMode { return AuthzModeOnline },
	})

	tok := bearerToken(t, key, now, []string{"keys:get"}, "tenant-1")

	// Scope is granted, but liveness says inactive: must deny.
	_, err := v.Authorize(context.Background(), tok, "keys:get", "", "", "")
	var inactive *TokenInactiveError
	if !errors.As(err, &inactive) {
		t.Fatalf("expected TokenInactiveError, got %v", err)
	}
	if inactive.Subject != "user-1" {
		t.Fatalf("expected sub user-1, got %q", inactive.Subject)
	}
}

func TestAuthorize_Online_ActiveAllowed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	live := &stubLiveness{results: []bool{true}}
	v := newAuthzValidator(t, key, now, Options{
		LivenessChecker: live,
		ModeResolver:    func(string) AuthzMode { return AuthzModeOnline },
	})

	tok := bearerToken(t, key, now, []string{"keys:get"}, "tenant-1")
	if _, err := v.Authorize(context.Background(), tok, "keys:get", "", "", ""); err != nil {
		t.Fatalf("online active allow: %v", err)
	}
}

func TestAuthorize_Online_NoCacheEveryRequest(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	live := &stubLiveness{results: []bool{true}}
	v := newAuthzValidator(t, key, now, Options{
		LivenessChecker:  live,
		LivenessCacheTTL: time.Minute, // must be ignored in online mode
		ModeResolver:     func(string) AuthzMode { return AuthzModeOnline },
	})
	tok := bearerToken(t, key, now, []string{"keys:get"}, "tenant-1")

	for i := 0; i < 3; i++ {
		if _, err := v.Authorize(context.Background(), tok, "keys:get", "", "", ""); err != nil {
			t.Fatalf("online allow #%d: %v", i, err)
		}
	}
	if got := live.calls.Load(); got != 3 {
		t.Fatalf("online mode must live-check every request: want 3 calls, got %d", got)
	}
}

func TestAuthorize_MissingChecker_FailsClosed(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)

	for _, mode := range []AuthzMode{AuthzModeOnline, AuthzModeHybrid} {
		v := newAuthzValidator(t, key, now, Options{
			ModeResolver: func(string) AuthzMode { return mode },
		})
		tok := bearerToken(t, key, now, []string{"keys:get"}, "tenant-1")
		_, err := v.Authorize(context.Background(), tok, "keys:get", "", "", "")
		var cfg *ConfigError
		if !errors.As(err, &cfg) {
			t.Fatalf("mode %s: expected ConfigError when liveness checker nil, got %v", mode, err)
		}
	}
}

func TestAuthorize_Hybrid_CachesPositiveAcrossTTL(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	clock := &mutableClock{t: base}
	key := newIssuerKey(t, testKID)
	live := &stubLiveness{results: []bool{true}}

	v, err := NewValidator(Options{
		Issuer:           testIssuer,
		Audience:         testAudience,
		JWKS:             key.jwks(),
		Clock:            clock.now,
		LivenessChecker:  live,
		LivenessCacheTTL: 30 * time.Second,
		ModeResolver:     func(string) AuthzMode { return AuthzModeHybrid },
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	tok := bearerToken(t, key, base, []string{"keys:get"}, "tenant-1")

	// First request live-checks.
	if _, err := v.Authorize(context.Background(), tok, "keys:get", "", "", ""); err != nil {
		t.Fatalf("hybrid #1: %v", err)
	}
	if got := live.calls.Load(); got != 1 {
		t.Fatalf("after #1 want 1 call, got %d", got)
	}

	// Second request within TTL served from cache.
	clock.advance(10 * time.Second)
	if _, err := v.Authorize(context.Background(), tok, "keys:get", "", "", ""); err != nil {
		t.Fatalf("hybrid #2: %v", err)
	}
	if got := live.calls.Load(); got != 1 {
		t.Fatalf("within TTL must use cache: want 1 call, got %d", got)
	}

	// Advance past TTL: re-checks.
	clock.advance(30 * time.Second)
	if _, err := v.Authorize(context.Background(), tok, "keys:get", "", "", ""); err != nil {
		t.Fatalf("hybrid #3: %v", err)
	}
	if got := live.calls.Load(); got != 2 {
		t.Fatalf("after TTL expiry must re-check: want 2 calls, got %d", got)
	}
}

func TestAuthorize_PerTenantModeRouting(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	// Liveness denies; only tenant B (online) should feel it.
	live := &stubLiveness{results: []bool{false}}
	resolver := func(tid string) AuthzMode {
		if tid == "tenant-a" {
			return AuthzModeOffline
		}
		return AuthzModeOnline
	}
	v := newAuthzValidator(t, key, now, Options{
		LivenessChecker: live,
		ModeResolver:    resolver,
	})

	tokA := bearerToken(t, key, now, []string{"keys:get"}, "tenant-a")
	if _, err := v.Authorize(context.Background(), tokA, "keys:get", "", "", ""); err != nil {
		t.Fatalf("tenant-a offline should allow without liveness: %v", err)
	}
	if got := live.calls.Load(); got != 0 {
		t.Fatalf("offline tenant must not call liveness, got %d calls", got)
	}

	tokB := bearerToken(t, key, now, []string{"keys:get"}, "tenant-b")
	_, err := v.Authorize(context.Background(), tokB, "keys:get", "", "", "")
	var inactive *TokenInactiveError
	if !errors.As(err, &inactive) {
		t.Fatalf("tenant-b online must enforce liveness, got %v", err)
	}
}

func TestAuthorize_DPoPEnforcedForSenderConstrained(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)

	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("dpop key: %v", err)
	}
	jkt, err := dpop.Thumbprint(dpopKey.Public())
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}

	htm, htu := "GET", "https://api.app.example/v1/keys"
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1",
		tid: "tenant-1", scp: []string{"keys:get"},
		exp: now.Add(10 * time.Minute), iat: now, jkt: jkt,
	})

	v := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOffline },
		ReplayCache:  dpop.NewMemoryReplayCache(&fixedDPoPClock{now}),
	})

	// No proof: must be rejected.
	_, err = v.Authorize(context.Background(), tok, "keys:get", "", htm, htu)
	var missing *dpop.MissingProofError
	if !errors.As(err, &missing) {
		t.Fatalf("expected MissingProofError for sender-constrained token, got %v", err)
	}

	// Valid proof: allowed. Fresh validator to avoid the replay cache rejecting
	// the proof jti reused across calls.
	v2 := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOffline },
		ReplayCache:  dpop.NewMemoryReplayCache(&fixedDPoPClock{now}),
	})
	proof, err := dpop.NewProofWithClock(dpopKey, htm, htu, &fixedDPoPClock{now})
	if err != nil {
		t.Fatalf("NewProof: %v", err)
	}
	if _, err := v2.Authorize(context.Background(), tok, "keys:get", proof, htm, htu); err != nil {
		t.Fatalf("valid DPoP authorize: %v", err)
	}
}

func TestAuthorize_SenderConstrained_NoReplayCache(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	tok := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1",
		scp: []string{"keys:get"},
		exp: now.Add(10 * time.Minute), iat: now, jkt: "some-jkt",
	})
	v := newAuthzValidator(t, key, now, Options{
		ModeResolver: func(string) AuthzMode { return AuthzModeOffline },
	})
	_, err := v.Authorize(context.Background(), tok, "keys:get", "proof", "GET", "https://x/y")
	var cfg *ConfigError
	if !errors.As(err, &cfg) {
		t.Fatalf("expected ConfigError when replay cache missing, got %v", err)
	}
}

func TestCachedLiveness_PositiveOnly_RevocationImmediate(t *testing.T) {
	clock := &mutableClock{t: time.Unix(1_700_000_000, 0)}
	// active=false first, then active=true: a negative is never cached, so the
	// second call sees the flipped-to-true result.
	live := &stubLiveness{results: []bool{false, true}}
	cached := NewCachedLivenessChecker(live, time.Minute, clock.now)

	claims := &Claims{}
	claims.ID = "jti-x"
	claims.Subject = "user-1"

	active, err := cached.CheckLiveness(context.Background(), "tok", claims)
	if err != nil || active {
		t.Fatalf("first call: want active=false, got active=%v err=%v", active, err)
	}
	active, err = cached.CheckLiveness(context.Background(), "tok", claims)
	if err != nil || !active {
		t.Fatalf("second call must re-check (negatives not cached): want active=true, got active=%v err=%v", active, err)
	}
	if got := live.calls.Load(); got != 2 {
		t.Fatalf("both calls must hit base (no negative cache): want 2, got %d", got)
	}
}

func TestCachedLiveness_PositiveCached_DoesNotReflectLaterFlip(t *testing.T) {
	clock := &mutableClock{t: time.Unix(1_700_000_000, 0)}
	// active=true then false: the first positive is cached for the TTL, so the
	// second call within the window returns the cached true without re-checking.
	live := &stubLiveness{results: []bool{true, false}}
	cached := NewCachedLivenessChecker(live, time.Minute, clock.now)

	claims := &Claims{}
	claims.ID = "jti-y"

	active, _ := cached.CheckLiveness(context.Background(), "tok", claims)
	if !active {
		t.Fatal("first call should be active")
	}
	clock.advance(10 * time.Second)
	active, _ = cached.CheckLiveness(context.Background(), "tok", claims)
	if !active {
		t.Fatal("within TTL the cached positive should be returned")
	}
	if got := live.calls.Load(); got != 1 {
		t.Fatalf("cached positive must not re-check within TTL: want 1, got %d", got)
	}
}

func TestCachedLiveness_TTLZeroNeverCaches(t *testing.T) {
	live := &stubLiveness{results: []bool{true}}
	cached := NewCachedLivenessChecker(live, 0, nil)
	claims := &Claims{}
	claims.ID = "jti-z"
	for i := 0; i < 3; i++ {
		if _, err := cached.CheckLiveness(context.Background(), "tok", claims); err != nil {
			t.Fatalf("call #%d: %v", i, err)
		}
	}
	if got := live.calls.Load(); got != 3 {
		t.Fatalf("ttl=0 must always delegate: want 3, got %d", got)
	}
}

func TestCachedLiveness_KeyFallsBackToSubject(t *testing.T) {
	clock := &mutableClock{t: time.Unix(1_700_000_000, 0)}
	live := &stubLiveness{results: []bool{true}}
	cached := NewCachedLivenessChecker(live, time.Minute, clock.now)

	// Empty jti: cache key must fall back to subject. Two distinct subjects must
	// not collide; the same subject must hit the cache.
	c1 := &Claims{}
	c1.Subject = "sub-1"
	c2 := &Claims{}
	c2.Subject = "sub-1"

	if _, err := cached.CheckLiveness(context.Background(), "tok", c1); err != nil {
		t.Fatalf("c1: %v", err)
	}
	if _, err := cached.CheckLiveness(context.Background(), "tok", c2); err != nil {
		t.Fatalf("c2: %v", err)
	}
	if got := live.calls.Load(); got != 1 {
		t.Fatalf("same subject (empty jti) must share cache entry: want 1, got %d", got)
	}
}

func TestIntrospectionLivenessChecker_ActiveAndInactive(t *testing.T) {
	active := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.PostForm.Get("token") != "raw-token" {
			t.Errorf("expected token=raw-token, got %q", r.PostForm.Get("token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": active})
	}))
	defer srv.Close()

	checker := NewIntrospectionLivenessChecker(srv.URL, srv.Client())

	got, err := checker.CheckLiveness(context.Background(), "raw-token", &Claims{})
	if err != nil || !got {
		t.Fatalf("active=true: got active=%v err=%v", got, err)
	}

	active = false
	got, err = checker.CheckLiveness(context.Background(), "raw-token", &Claims{})
	if err != nil || got {
		t.Fatalf("active=false: got active=%v err=%v", got, err)
	}
}

func TestIntrospectionLivenessChecker_TransportFailures(t *testing.T) {
	// 500 status.
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()
	checker := NewIntrospectionLivenessChecker(srv500.URL, srv500.Client())
	_, err := checker.CheckLiveness(context.Background(), "raw-token", &Claims{})
	var lce *LivenessCheckError
	if !errors.As(err, &lce) {
		t.Fatalf("500 must yield LivenessCheckError, got %v", err)
	}

	// Malformed body.
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srvBad.Close()
	checker = NewIntrospectionLivenessChecker(srvBad.URL, srvBad.Client())
	_, err = checker.CheckLiveness(context.Background(), "raw-token", &Claims{})
	if !errors.As(err, &lce) {
		t.Fatalf("malformed body must yield LivenessCheckError, got %v", err)
	}
}

func TestIntrospectionLivenessChecker_BasicAuthAndBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"active": true})
	}))
	defer srv.Close()

	// Basic auth: client_id/client_secret.
	basic := NewIntrospectionLivenessChecker(srv.URL, srv.Client()).WithBasicAuth("cid", "secret")
	if _, err := basic.CheckLiveness(context.Background(), "tok", &Claims{}); err != nil {
		t.Fatalf("basic auth check: %v", err)
	}
	if len(gotAuth) < 6 || gotAuth[:6] != "Basic " {
		t.Fatalf("expected Basic auth header, got %q", gotAuth)
	}

	// Bearer takes precedence over basic when both set.
	bearer := NewIntrospectionLivenessChecker(srv.URL, srv.Client()).
		WithBasicAuth("cid", "secret").WithBearer("brr")
	if _, err := bearer.CheckLiveness(context.Background(), "tok", &Claims{}); err != nil {
		t.Fatalf("bearer check: %v", err)
	}
	if gotAuth != "Bearer brr" {
		t.Fatalf("expected Bearer header to win, got %q", gotAuth)
	}
}

func TestIntrospectionLivenessChecker_NilClientUsesDefault(t *testing.T) {
	c := NewIntrospectionLivenessChecker("https://example.invalid/introspect", nil)
	if c.httpClient != http.DefaultClient {
		t.Fatal("nil client must default to http.DefaultClient")
	}
}

func TestIntrospectionStatusError_Message(t *testing.T) {
	e := &introspectionStatusError{status: 503}
	if got := e.Error(); got == "" {
		t.Fatal("status error message should be non-empty")
	}
}

// enforceLivenessStub returns a pre-typed *LivenessCheckError to prove the
// validator surfaces it verbatim rather than double-wrapping.
type prewrappedLiveness struct{ inner *LivenessCheckError }

func (p *prewrappedLiveness) CheckLiveness(_ context.Context, _ string, _ *Claims) (bool, error) {
	return false, p.inner
}

func TestAuthorize_LivenessTransportError_SurfacesTyped(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	inner := &LivenessCheckError{Cause: &introspectionStatusError{status: 500}}
	v := newAuthzValidator(t, key, now, Options{
		LivenessChecker: &prewrappedLiveness{inner: inner},
		ModeResolver:    func(string) AuthzMode { return AuthzModeOnline },
	})
	tok := bearerToken(t, key, now, []string{"keys:get"}, "tenant-1")
	_, err := v.Authorize(context.Background(), tok, "keys:get", "", "", "")
	var lce *LivenessCheckError
	if !errors.As(err, &lce) {
		t.Fatalf("expected LivenessCheckError, got %v", err)
	}
	if lce != inner {
		t.Fatalf("expected the already-typed error surfaced verbatim (no double wrap)")
	}
}

func TestAuthorize_DefaultModeIsHybrid(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	live := &stubLiveness{results: []bool{true}}
	// No ModeResolver -> default Hybrid -> liveness enforced via cached path.
	v := newAuthzValidator(t, key, now, Options{LivenessChecker: live})
	tok := bearerToken(t, key, now, []string{"keys:get"}, "tenant-1")
	if _, err := v.Authorize(context.Background(), tok, "keys:get", "", "", ""); err != nil {
		t.Fatalf("default hybrid allow: %v", err)
	}
	if got := live.calls.Load(); got != 1 {
		t.Fatalf("default hybrid must live-check: want 1, got %d", got)
	}
}

// advance moves the shared mutableClock (declared in edge_test.go) forward for
// cache TTL tests without sleeping.
func (m *mutableClock) advance(d time.Duration) { m.t = m.t.Add(d) }
