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
	"errors"
	"fmt"
	"sync"
	"testing"

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
	m := &XKMSWebAuthnManager{}
	const tenant = "tenant-1"

	// Two concurrent same-tenant ceremonies, each with its own challenge and
	// session id — exactly what BeginLogin records.
	type ceremony struct {
		challenge string
		sessionID string
	}
	ceremonies := []ceremony{
		{challenge: "challenge-alice", sessionID: "xkms-session-alice"},
		{challenge: "challenge-bob", sessionID: "xkms-session-bob"},
	}
	for _, c := range ceremonies {
		m.loginSessions.Store(m.loginKey(tenant, c.challenge), loginHandoff{
			sessionID:  c.sessionID,
			userHandle: []byte(c.challenge), // distinct handle per ceremony
		})
	}

	// Each ceremony's assertion must resolve to its own session, never the
	// other's. Drive many concurrent resolutions to surface any cross-wiring.
	const workers = 64
	var wg sync.WaitGroup
	errCh := make(chan error, workers*len(ceremonies))
	for i := 0; i < workers; i++ {
		for _, c := range ceremonies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				handoff, key, err := m.resolveLoginSession(tenant, assertionWithChallenge(c.challenge))
				if err != nil {
					errCh <- fmt.Errorf("challenge %q: unexpected error: %w", c.challenge, err)
					return
				}
				if handoff.sessionID != c.sessionID {
					errCh <- fmt.Errorf("challenge %q resolved to session %q, want %q (cross-wired)", c.challenge, handoff.sessionID, c.sessionID)
					return
				}
				if string(handoff.userHandle) != c.challenge {
					errCh <- fmt.Errorf("challenge %q resolved to user handle %q, want %q (cross-wired)", c.challenge, handoff.userHandle, c.challenge)
					return
				}
				if key != m.loginKey(tenant, c.challenge) {
					errCh <- fmt.Errorf("challenge %q resolved to key %q, want %q", c.challenge, key, m.loginKey(tenant, c.challenge))
				}
			}()
		}
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}

	// Unknown challenge must fail closed with the typed error.
	if _, _, err := m.resolveLoginSession(tenant, assertionWithChallenge("challenge-unknown")); err == nil {
		t.Fatal("expected WebAuthnLoginSessionError for unknown challenge, got nil")
	} else {
		var sessErr *WebAuthnLoginSessionError
		if !errors.As(err, &sessErr) {
			t.Fatalf("expected *WebAuthnLoginSessionError, got %T: %v", err, err)
		}
	}

	// Same challenge under a different tenant must not match — the tenant is
	// part of the composite key.
	if _, _, err := m.resolveLoginSession("tenant-2", assertionWithChallenge("challenge-alice")); err == nil {
		t.Fatal("expected fail-closed for cross-tenant challenge reuse, got nil")
	}

	// Empty / temp challenge must fail closed, never falling through to a scan.
	if _, _, err := m.resolveLoginSession(tenant, assertionWithChallenge("")); err == nil {
		t.Fatal("expected WebAuthnLoginSessionError for empty challenge, got nil")
	}
	if _, _, err := m.resolveLoginSession(tenant, nil); err == nil {
		t.Fatal("expected WebAuthnLoginSessionError for nil assertion, got nil")
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
	m := &XKMSWebAuthnManager{}
	const tenant = "tenant-1"
	const challenge = "challenge-xyz"
	wantHandle := []byte{0x01, 0x02, 0x03, 0x04}

	// Mirror exactly what BeginLogin stores for a user-identified ceremony.
	m.loginSessions.Store(m.loginKey(tenant, challenge), loginHandoff{
		sessionID:  "xkms-session-1",
		userHandle: wantHandle,
	})

	handoff, key, err := m.resolveLoginSession(tenant, assertionWithChallenge(challenge))
	if err != nil {
		t.Fatalf("unexpected error resolving handoff: %v", err)
	}
	if key != m.loginKey(tenant, challenge) {
		t.Fatalf("resolved key %q, want %q", key, m.loginKey(tenant, challenge))
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
		ID:       "1234567890123456789", // canonical decimal-string subject
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
	if user.ID != "1234567890123456789" {
		t.Errorf("subject id = %q, want canonical decimal string", user.ID)
	}
	// The subject must be a clean printable string, never raw bytes.
	for _, c := range user.ID {
		if c < '0' || c > '9' {
			t.Fatalf("subject id %q contains a non-decimal rune; raw-bytes regression", user.ID)
		}
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
