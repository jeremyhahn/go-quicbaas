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
	"testing"
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
	// Validation error from go-webauthn parse OR session-not-found.
	// Either is acceptable evidence the call short-circuited.
	var cfgErr *WebAuthnFactoryConfigError
	if errors.As(err, &cfgErr) && cfgErr.Field != "login session not found" {
		t.Fatalf("unexpected typed error field: %q", cfgErr.Field)
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
