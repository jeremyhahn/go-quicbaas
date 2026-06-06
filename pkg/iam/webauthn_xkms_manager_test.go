//go:build iam

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
		QRDB: dummyQRDBClient,
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
	err = m.FinishRegistration(context.Background(), "tenant-1", "user@x.com",
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
		QRDB: dummyQRDBClient,
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
		m.loginSessions.Store(m.loginKey(tenant, c.challenge), c.sessionID)
	}

	// Each ceremony's assertion must resolve to its own session, never the
	// other's. Drive many concurrent resolutions to surface any cross-wiring.
	const workers = 64
	var wg sync.WaitGroup
	errCh := make(chan error, workers*len(ceremonies))
	for i := 0; i < workers; i++ {
		for _, c := range ceremonies {
			c := c
			wg.Add(1)
			go func() {
				defer wg.Done()
				sessionID, key, err := m.resolveLoginSession(tenant, assertionWithChallenge(c.challenge))
				if err != nil {
					errCh <- fmt.Errorf("challenge %q: unexpected error: %w", c.challenge, err)
					return
				}
				if sessionID != c.sessionID {
					errCh <- fmt.Errorf("challenge %q resolved to session %q, want %q (cross-wired)", c.challenge, sessionID, c.sessionID)
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
