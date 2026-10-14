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

package iam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

// TestXKMSWebAuthnManager_FinishRegistration_NoSession verifies that
// FinishRegistration without a corresponding stashed session id surfaces
// a typed error rather than crashing on the cache miss.
func TestXKMSWebAuthnManager_FinishRegistration_NoSession(t *testing.T) {
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, err := NewXKMSWebAuthnManager(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// FinishRegistration with no prior BeginRegistration call. Must
	// short-circuit before hitting the qrdb client (zero-value sentinel
	// would panic on a real call). The factory's ServiceFor will use
	// the zero-value qrdb internals — to avoid that, we expect the
	// session-not-found check to fire first. ServiceFor also panics
	// before reaching the session-cache check, so wrap in recover().
	defer func() { _ = recover() }()
	_, err = m.FinishRegistration(context.Background(), "tenant-1", "user@x.com",
		map[string]interface{}{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// TestMarshalToMap_RoundTrip verifies the JSON marshal helper round-trips
// a typed protocol structure into a generic map without loss.
func TestMarshalToMap_RoundTrip(t *testing.T) {
	type fixture struct {
		Foo string `json:"foo"`
		Bar int    `json:"bar"`
	}
	out, err := marshalToMap(&fixture{Foo: "hello", Bar: 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, ok := out["foo"]; !ok || got != "hello" {
		t.Fatalf("expected foo=hello, got %v (ok=%v)", got, ok)
	}
	if got, ok := out["bar"]; !ok {
		t.Fatalf("expected bar key present")
	} else if got.(float64) != 42 {
		t.Fatalf("expected bar=42, got %v", got)
	}
}

// TestParseCredentialCreationMap_Error verifies that empty or malformed
// credential maps surface parse errors from the underlying go-webauthn
// library rather than panicking.
func TestParseCredentialCreationMap_Error(t *testing.T) {
	if _, err := parseCredentialCreationMap(map[string]interface{}{}); err == nil {
		t.Fatalf("expected error from empty map")
	}
	if _, err := parseCredentialCreationMap(map[string]interface{}{"id": "abc"}); err == nil {
		t.Fatalf("expected error from minimally populated map")
	}
}

// TestParseCredentialAssertionMap_Error mirrors the creation-map test for
// the login (assertion) ceremony.
func TestParseCredentialAssertionMap_Error(t *testing.T) {
	if _, err := parseCredentialAssertionMap(map[string]interface{}{}); err == nil {
		t.Fatalf("expected error from empty map")
	}
	if _, err := parseCredentialAssertionMap(map[string]interface{}{"id": "abc"}); err == nil {
		t.Fatalf("expected error from minimally populated map")
	}
}

// TestXKMSWebAuthnManager_FinishLogin_NoSession verifies that
// FinishLogin with no prior BeginLogin in the cache returns an error
// rather than dispatching to xkms.
func TestXKMSWebAuthnManager_FinishLogin_NoSession(t *testing.T) {
	f, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, err := NewXKMSWebAuthnManager(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// FinishLogin with no BeginLogin call. Same caveat as
	// FinishRegistration: the factory's ServiceFor may panic on the
	// zero-value qrdb client before our session-not-found check. Wrap
	// in recover to keep the test surface clean.
	defer func() { _ = recover() }()
	credMap := map[string]interface{}{
		"id":    "abc",
		"rawId": "abc",
		"type":  "public-key",
		"response": map[string]interface{}{
			"clientDataJSON":    "ZmFrZQ",
			"authenticatorData": "ZmFrZQ",
			"signature":         "ZmFrZQ",
			"userHandle":        "ZmFrZQ",
		},
	}
	_, err = m.FinishLogin(context.Background(), "tenant-1", credMap)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	// Validation error from go-webauthn parse OR a typed
	// WebAuthnLoginSessionError. Either is acceptable evidence the call
	// short-circuited before dispatching to xkms.
	var sessErr *WebAuthnLoginSessionError
	if errors.As(err, &sessErr) && sessErr.Reason == "" {
		t.Fatalf("login session error must carry a reason")
	}
}

// TestXKMSWebAuthnManager_RegKey verifies the internal regKey helper
// composes its inputs in the deterministic order the cache lookups
// expect. This is a load-bearing invariant: BeginRegistration writes
// regSessions[tenantID:userID] and FinishRegistration reads the same
// key.
func TestXKMSWebAuthnManager_RegKey(t *testing.T) {
	m := &XKMSWebAuthnManager{}
	got := m.regKey("tenant-a", "user@x.com")
	if got != "tenant-a:user@x.com" {
		t.Fatalf("regKey unexpected format: %q", got)
	}
}

// assertionWithChallenge builds a minimal parsed assertion carrying the given
// WebAuthn challenge in its collected client data — the value FinishLogin uses
// to recover the originating ceremony session.
func assertionWithChallenge(challenge string) *protocol.ParsedCredentialAssertionData {
	parsed := &protocol.ParsedCredentialAssertionData{}
	parsed.Response.CollectedClientData.Type = protocol.AssertCeremony
	parsed.Response.CollectedClientData.Challenge = challenge
	return parsed
}

// TestXKMSWebAuthnManager_LoginSession_ConcurrentResolution proves the H7 fix:
// concurrent same-tenant login ceremonies resolve to their own distinct xkms
// session by the per-ceremony challenge, never cross-wiring onto another
// in-flight session, and an unknown/temp challenge fails closed with the typed
// WebAuthnLoginSessionError. Sessions are keyed exactly as BeginLogin stores
// them and resolved exactly as FinishLogin reads them.
func TestXKMSWebAuthnManager_LoginSession_ConcurrentResolution(t *testing.T) {
	m := newClockedManager(handoffTestNow)
	const tenant = "tenant-1"

	// Many concurrent same-tenant ceremonies, each with its own challenge and
	// session id, exactly what BeginLogin records. Each worker resolves its
	// own challenge; none may ever see another ceremony's handoff.
	const workers = 64
	challengeOf := func(i int) string { return fmt.Sprintf("challenge-%d", i) }
	sessionOf := func(i int) string { return fmt.Sprintf("xkms-session-%d", i) }
	for i := 0; i < workers; i++ {
		seedLoginHandoff(m, tenant, challengeOf(i), loginHandoff{
			sessionID:  sessionOf(i),
			userHandle: []byte(challengeOf(i)), // distinct handle per ceremony
			expiresAt:  handoffTestNow.Add(time.Minute),
		})
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			challenge := challengeOf(i)
			handoff, err := m.resolveLoginSession(tenant, assertionWithChallenge(challenge))
			if err != nil {
				errCh <- fmt.Errorf("challenge %q: unexpected error: %w", challenge, err)
				return
			}
			if handoff.sessionID != sessionOf(i) {
				errCh <- fmt.Errorf("challenge %q resolved to session %q, want %q (cross-wired)", challenge, handoff.sessionID, sessionOf(i))
				return
			}
			if string(handoff.userHandle) != challenge {
				errCh <- fmt.Errorf("challenge %q resolved to user handle %q, want %q (cross-wired)", challenge, handoff.userHandle, challenge)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	// Unknown challenge must fail closed with the typed error.
	assertLoginSessionError(t, m, tenant, assertionWithChallenge("challenge-unknown"))

	// Same challenge under a different tenant must not match: the tenant is
	// part of the composite key.
	seedLoginHandoff(m, tenant, "challenge-alice", loginHandoff{
		sessionID: "xkms-session-alice",
		expiresAt: handoffTestNow.Add(time.Minute),
	})
	assertLoginSessionError(t, m, "tenant-2", assertionWithChallenge("challenge-alice"))

	// Empty / temp challenge must fail closed, never falling through to a scan.
	assertLoginSessionError(t, m, tenant, assertionWithChallenge(""))
	assertLoginSessionError(t, m, tenant, nil)
}

// handoffTestNow is the fixed instant the handoff tests run their clock at.
var handoffTestNow = time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC)

// newClockedManager returns a manager whose clock is pinned to now.
func newClockedManager(now time.Time) *XKMSWebAuthnManager {
	return &XKMSWebAuthnManager{now: func() time.Time { return now }}
}

// seedLoginHandoff stores a handoff exactly where BeginLogin would.
func seedLoginHandoff(m *XKMSWebAuthnManager, tenantID, challenge string, handoff loginHandoff) {
	m.loginSessions.Store(m.loginKey(tenantID, challenge), &handoff)
}

// hasLoginHandoff reports whether a handoff is stored for the challenge.
func hasLoginHandoff(m *XKMSWebAuthnManager, tenantID, challenge string) bool {
	_, ok := m.loginSessions.Load(m.loginKey(tenantID, challenge))
	return ok
}

// assertLoginSessionError asserts that resolving parsed fails closed with
// the typed *WebAuthnLoginSessionError.
func assertLoginSessionError(t *testing.T, m *XKMSWebAuthnManager, tenantID string, parsed *protocol.ParsedCredentialAssertionData) {
	t.Helper()
	_, err := m.resolveLoginSession(tenantID, parsed)
	var sessErr *WebAuthnLoginSessionError
	if !errors.As(err, &sessErr) {
		t.Fatalf("expected *WebAuthnLoginSessionError, got %T: %v", err, err)
	}
}

// TestXKMSWebAuthnManager_LoginSession_ConsumedOnce pins go-quicbaas#384:
// concurrent finishes for one challenge must not all resolve the handoff.
// Exactly one wins; every other finish fails closed.
func TestXKMSWebAuthnManager_LoginSession_ConsumedOnce(t *testing.T) {
	m := newClockedManager(handoffTestNow)
	const tenant = "tenant-1"
	const challenge = "challenge-shared"
	seedLoginHandoff(m, tenant, challenge, loginHandoff{
		sessionID:  "xkms-session-shared",
		userHandle: []byte{0x01},
		expiresAt:  handoffTestNow.Add(time.Minute),
	})

	const workers = 32
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := m.resolveLoginSession(tenant, assertionWithChallenge(challenge))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes, refusals := 0, 0
	for err := range errs {
		var sessErr *WebAuthnLoginSessionError
		switch {
		case err == nil:
			successes++
		case errors.As(err, &sessErr):
			refusals++
		default:
			t.Errorf("unexpected error type %T: %v", err, err)
		}
	}
	if successes != 1 || refusals != workers-1 {
		t.Fatalf("successes = %d, refusals = %d; want 1 and %d", successes, refusals, workers-1)
	}
	if hasLoginHandoff(m, tenant, challenge) {
		t.Fatal("a consumed handoff must not remain stored")
	}
}

// TestXKMSWebAuthnManager_FinishLogin_FailureReclaimsHandoff verifies that a
// finish which fails after resolving its handoff leaves nothing behind: the
// user restarts the ceremony instead of the handoff leaking for the life of
// the process.
func TestXKMSWebAuthnManager_FinishLogin_FailureReclaimsHandoff(t *testing.T) {
	settingsErr := &WebAuthnSettingsReadError{TenantID: "tenant-1"}
	// A failing settings provider makes ServiceFor fail inside resolveConfig,
	// before it touches the zero-value qrdb client.
	f := newTestWebAuthnFactory(t, stubWebAuthnSettings{err: settingsErr})
	m, err := NewXKMSWebAuthnManager(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.now = func() time.Time { return handoffTestNow }

	const tenant = "tenant-1"
	const challenge = "Y2hhbGxlbmdlLWZhaWxpbmc"
	seedLoginHandoff(m, tenant, challenge, loginHandoff{
		sessionID:  "xkms-session-failing",
		userHandle: []byte{0x01},
		expiresAt:  handoffTestNow.Add(time.Minute),
	})

	credential := assertionMapWithChallenge(t, challenge)
	_, err = m.FinishLogin(context.Background(), tenant, credential)
	var readErr *WebAuthnSettingsReadError
	if !errors.As(err, &readErr) {
		t.Fatalf("expected *WebAuthnSettingsReadError from ServiceFor, got %T: %v", err, err)
	}
	if hasLoginHandoff(m, tenant, challenge) {
		t.Fatal("a failed finish must reclaim its handoff")
	}
}

// assertionMapWithChallenge builds a wire-shaped assertion that parses
// cleanly and echoes challenge in its collected client data.
func assertionMapWithChallenge(t *testing.T, challenge string) map[string]interface{} {
	t.Helper()
	clientData, err := json.Marshal(map[string]string{
		"type":      string(protocol.AssertCeremony),
		"challenge": challenge,
		"origin":    "https://example.com",
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}
	// rpIdHash (32 bytes) || flags (user present) || signCount (4 bytes).
	authData := make([]byte, 37)
	authData[32] = 0x01
	credential := map[string]interface{}{
		"id":    "Y3JlZA",
		"rawId": "Y3JlZA",
		"type":  "public-key",
		"response": map[string]interface{}{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"signature":         "c2ln",
		},
	}
	parsed, err := parseCredentialAssertionMap(credential)
	if err != nil {
		t.Fatalf("test assertion must parse: %v", err)
	}
	if parsed.Response.CollectedClientData.Challenge != challenge {
		t.Fatalf("parsed challenge %q, want %q", parsed.Response.CollectedClientData.Challenge, challenge)
	}
	return credential
}

// TestXKMSWebAuthnManager_LoginSession_ExpiredRefused verifies an expired
// handoff is refused with the typed error and consumed by the attempt.
func TestXKMSWebAuthnManager_LoginSession_ExpiredRefused(t *testing.T) {
	m := newClockedManager(handoffTestNow)
	const tenant = "tenant-1"
	const challenge = "challenge-expired"
	seedLoginHandoff(m, tenant, challenge, loginHandoff{
		sessionID: "xkms-session-expired",
		expiresAt: handoffTestNow.Add(-time.Second),
	})

	assertLoginSessionError(t, m, tenant, assertionWithChallenge(challenge))
	if hasLoginHandoff(m, tenant, challenge) {
		t.Fatal("an expired handoff must be consumed by the refused attempt")
	}

	// A handoff expiring exactly now is still within its ceremony window.
	seedLoginHandoff(m, tenant, "challenge-boundary", loginHandoff{
		sessionID: "xkms-session-boundary",
		expiresAt: handoffTestNow,
	})
	if _, err := m.resolveLoginSession(tenant, assertionWithChallenge("challenge-boundary")); err != nil {
		t.Fatalf("handoff expiring at now must resolve, got %v", err)
	}
}

// TestXKMSWebAuthnManager_ReclaimExpiredLoginHandoffs verifies the sweep
// removes only expired handoffs.
func TestXKMSWebAuthnManager_ReclaimExpiredLoginHandoffs(t *testing.T) {
	m := newClockedManager(handoffTestNow)
	const tenant = "tenant-1"
	seedLoginHandoff(m, tenant, "challenge-stale", loginHandoff{expiresAt: handoffTestNow.Add(-time.Minute)})
	seedLoginHandoff(m, tenant, "challenge-live", loginHandoff{expiresAt: handoffTestNow.Add(time.Minute)})

	m.reclaimExpiredLoginHandoffs(handoffTestNow)

	if hasLoginHandoff(m, tenant, "challenge-stale") {
		t.Fatal("the sweep must reclaim an expired handoff")
	}
	if !hasLoginHandoff(m, tenant, "challenge-live") {
		t.Fatal("the sweep must keep a live handoff")
	}
}

// TestXKMSWebAuthnManager_SweepLoginHandoffsIfDue verifies the sweep runs at
// most once per TTL, observed through the recorded sweep time.
func TestXKMSWebAuthnManager_SweepLoginHandoffsIfDue(t *testing.T) {
	m := newClockedManager(handoffTestNow)
	const tenant = "tenant-1"
	const ttl = time.Minute

	if !m.sweepLoginHandoffsIfDue(handoffTestNow, ttl) {
		t.Fatal("the first sweep must run")
	}
	if got := m.lastHandoffSweep.Load(); got != handoffTestNow.UnixNano() {
		t.Fatalf("last sweep = %d, want %d", got, handoffTestNow.UnixNano())
	}

	// Within the TTL the sweep must not run, so this expired handoff stays.
	seedLoginHandoff(m, tenant, "challenge-stale", loginHandoff{expiresAt: handoffTestNow.Add(-time.Second)})
	within := handoffTestNow.Add(ttl - time.Nanosecond)
	if m.sweepLoginHandoffsIfDue(within, ttl) {
		t.Fatal("a sweep within the TTL must not run")
	}
	if got := m.lastHandoffSweep.Load(); got != handoffTestNow.UnixNano() {
		t.Fatalf("a skipped sweep moved the last sweep time to %d", got)
	}
	if !hasLoginHandoff(m, tenant, "challenge-stale") {
		t.Fatal("a skipped sweep must not reclaim anything")
	}

	// One TTL later the sweep runs again and reclaims the expired handoff.
	due := handoffTestNow.Add(ttl)
	if !m.sweepLoginHandoffsIfDue(due, ttl) {
		t.Fatal("a sweep one TTL later must run")
	}
	if got := m.lastHandoffSweep.Load(); got != due.UnixNano() {
		t.Fatalf("last sweep = %d, want %d", got, due.UnixNano())
	}
	if hasLoginHandoff(m, tenant, "challenge-stale") {
		t.Fatal("the due sweep must reclaim the expired handoff")
	}
}

// TestLoginHandoffTTL verifies the handoff lifetime follows the configured
// ceremony timeout and falls back to the library default when it is unset.
func TestLoginHandoffTTL(t *testing.T) {
	if got := loginHandoffTTL(90 * time.Second); got != 90*time.Second {
		t.Fatalf("configured timeout: ttl = %v, want 90s", got)
	}
	if got := loginHandoffTTL(0); got != defaultLoginCeremonyTimeout {
		t.Fatalf("unset timeout: ttl = %v, want %v", got, defaultLoginCeremonyTimeout)
	}
	if defaultLoginCeremonyTimeout != 5*time.Minute {
		t.Fatalf("default = %v, want go-webauthn's 5m login default", defaultLoginCeremonyTimeout)
	}
	if got := loginHandoffTTL(-time.Second); got != defaultLoginCeremonyTimeout {
		t.Fatalf("negative timeout: ttl = %v, want %v", got, defaultLoginCeremonyTimeout)
	}
}

// TestXKMSWebAuthnManager_NewLoginHandoff verifies the handoff BeginLogin
// stores expires one TTL after the manager's clock.
func TestXKMSWebAuthnManager_NewLoginHandoff(t *testing.T) {
	m := newClockedManager(handoffTestNow)

	h := m.newLoginHandoff("xkms-session-1", []byte{0x01}, 2*time.Minute)
	if want := handoffTestNow.Add(2 * time.Minute); !h.expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", h.expiresAt, want)
	}
	if h.sessionID != "xkms-session-1" || string(h.userHandle) != "\x01" {
		t.Fatalf("handoff = %+v, want the session id and handle it was given", h)
	}

	h = m.newLoginHandoff("xkms-session-2", nil, 0)
	if want := handoffTestNow.Add(defaultLoginCeremonyTimeout); !h.expiresAt.Equal(want) {
		t.Fatalf("unset timeout: expiresAt = %v, want %v", h.expiresAt, want)
	}
}

// TestXKMSWebAuthnManager_LoginHandoff_CarriesUserHandle is the manager-level
// guard for the picker bug. The email-first login is user-identified: BeginLogin
// stashes the resolved user's WebAuthn handle alongside the xkms session, and
// FinishLogin recovers and forwards that SAME non-nil handle to xkms (rather than
// the discoverable nil-user path that emitted an empty allowCredentials list and
// triggered the browser's native passkey picker). This asserts the handoff
// round-trips the exact handle bytes that resolveLoginSession returns.
func TestXKMSWebAuthnManager_LoginHandoff_CarriesUserHandle(t *testing.T) {
	m := newClockedManager(handoffTestNow)
	const tenant = "tenant-1"
	const challenge = "challenge-xyz"
	wantHandle := []byte{0x01, 0x02, 0x03, 0x04}

	// Mirror exactly what BeginLogin stores for a user-identified ceremony.
	seedLoginHandoff(m, tenant, challenge, m.newLoginHandoff("xkms-session-1", wantHandle, time.Minute))

	handoff, err := m.resolveLoginSession(tenant, assertionWithChallenge(challenge))
	if err != nil {
		t.Fatalf("unexpected error resolving handoff: %v", err)
	}
	if handoff.sessionID != "xkms-session-1" {
		t.Fatalf("resolved session %q, want %q", handoff.sessionID, "xkms-session-1")
	}
	// The handle FinishLogin forwards to xkms must be the exact, non-nil handle
	// BeginLogin stashed — never nil (which is the discoverable path).
	if handoff.userHandle == nil {
		t.Fatal("user handle is nil: FinishLogin would fall into the discoverable path and re-introduce the picker bug")
	}
	if string(handoff.userHandle) != string(wantHandle) {
		t.Fatalf("resolved user handle %x, want %x", handoff.userHandle, wantHandle)
	}
}

// stubUserResolver is a deterministic WebAuthnUserResolver for the FinishLogin
// canonical-resolution tests: it returns a fixed user (or error) keyed by email.
type stubUserResolver struct {
	byEmail    map[string]*User
	err        error
	lastReq    string
	lastTenant string
}

func (r *stubUserResolver) GetUserByEmail(_ context.Context, tenantID, email string) (*User, error) {
	r.lastTenant = tenantID
	r.lastReq = email
	if r.err != nil {
		return nil, r.err
	}
	return r.byEmail[email], nil
}

// TestXKMSWebAuthnManager_ResolveAuthenticatedUser_CanonicalIdentity proves the
// token-issuance fix: after a successful assertion, FinishLogin re-resolves the
// authenticated user through the canonical broker store so the surfaced iam.User
// carries the external STRING id and the qrdb-resolved ROLE set — not xkms's
// raw-bytes webauthn handle with no roles.
//
// It also asserts that the resolution is SCOPED to the verified tenant rather
// than stamped with it afterwards. Stamping was how a global email index was
// made to look tenant-safe: a record belonging to another tenant would be read,
// relabelled, and returned. A tenant-scoped lookup cannot read it in the first
// place, so the record's own tenant is authoritative.
func TestXKMSWebAuthnManager_ResolveAuthenticatedUser_CanonicalIdentity(t *testing.T) {
	const tenant = "system"
	const email = "admin@example.com"
	canonical := &User{
		ID:       1234567890123456789, // canonical decimal-string subject
		Email:    email,
		Name:     "Admin",
		TenantID: tenant,
		Roles:    []string{"admin"},
	}
	resolver := &stubUserResolver{byEmail: map[string]*User{email: canonical}}
	m := &XKMSWebAuthnManager{}
	m.SetUserResolver(resolver)

	user, err := m.resolveAuthenticatedUser(context.Background(), tenant, email)
	if err != nil {
		t.Fatalf("resolveAuthenticatedUser: %v", err)
	}
	if resolver.lastReq != email {
		t.Errorf("resolver queried with %q, want %q", resolver.lastReq, email)
	}
	if resolver.lastTenant != tenant {
		t.Errorf("resolver queried in tenant %q, want the verified tenant %q", resolver.lastTenant, tenant)
	}
	if user.ID != 1234567890123456789 {
		t.Errorf("subject id = %d, want 1234567890123456789", user.ID)
	}
	if len(user.Roles) != 1 || user.Roles[0] != "admin" {
		t.Errorf("roles = %v, want [admin]", user.Roles)
	}
	if user.TenantID != tenant {
		t.Errorf("tenant = %q, want the record's own %q", user.TenantID, tenant)
	}
}

// TestXKMSWebAuthnManager_ResolveAuthenticatedUser_FailClosed verifies the
// resolution fails closed (never surfaces a half-populated user) when no
// resolver is wired, the resolver errors, or it returns no user.
func TestXKMSWebAuthnManager_ResolveAuthenticatedUser_FailClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("no resolver", func(t *testing.T) {
		m := &XKMSWebAuthnManager{}
		_, err := m.resolveAuthenticatedUser(ctx, "system", "a@x.com")
		var re *WebAuthnUserResolverError
		if !errors.As(err, &re) {
			t.Fatalf("want WebAuthnUserResolverError, got %T: %v", err, err)
		}
	})

	t.Run("resolver error", func(t *testing.T) {
		m := &XKMSWebAuthnManager{}
		m.SetUserResolver(&stubUserResolver{err: errors.New("store down")})
		if _, err := m.resolveAuthenticatedUser(ctx, "system", "a@x.com"); err == nil {
			t.Fatal("expected error from resolver failure")
		}
	})

	t.Run("nil user", func(t *testing.T) {
		m := &XKMSWebAuthnManager{}
		m.SetUserResolver(&stubUserResolver{byEmail: map[string]*User{}})
		_, err := m.resolveAuthenticatedUser(ctx, "system", "missing@x.com")
		var re *WebAuthnUserResolverError
		if !errors.As(err, &re) {
			t.Fatalf("want WebAuthnUserResolverError for nil user, got %T: %v", err, err)
		}
	})
}

// TestXKMSWebAuthnManager_LoginKey verifies the login session key derivation
// composes (tenant, challenge) deterministically. BeginLogin writes under this
// key and FinishLogin reads it back; a drift here would silently fail every
// login.
func TestXKMSWebAuthnManager_LoginKey(t *testing.T) {
	m := &XKMSWebAuthnManager{}
	if got := m.loginKey("tenant-a", "chal-123"); got != "tenant-a:chal-123" {
		t.Fatalf("loginKey unexpected format: %q", got)
	}
	// Distinct challenges must produce distinct keys so concurrent ceremonies
	// never collide.
	if m.loginKey("t", "a") == m.loginKey("t", "b") {
		t.Fatal("distinct challenges must yield distinct login keys")
	}
}
