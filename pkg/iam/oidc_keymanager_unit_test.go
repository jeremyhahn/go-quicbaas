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
	"errors"
	"strings"
	"testing"
)

// TestSigningKeyManagerErrorTypes asserts the typed errors expose stable,
// identifying messages and unwrap chains (CLAUDE.md mandates typed errors, so
// their behavior is part of the contract).
func TestSigningKeyManagerErrorTypes(t *testing.T) {
	unknown := &UnknownKeyIDError{KID: "abc123"}
	if !strings.Contains(unknown.Error(), "abc123") {
		t.Fatalf("UnknownKeyIDError must name the kid, got %q", unknown.Error())
	}

	unavailable := &SigningKeyUnavailableError{}
	if unavailable.Error() == "" {
		t.Fatal("SigningKeyUnavailableError must have a message")
	}

	cause := errors.New("der boom")
	marshalErr := &PublicKeyMarshalError{Cause: cause}
	if !strings.Contains(marshalErr.Error(), "der boom") {
		t.Fatalf("PublicKeyMarshalError must include the cause, got %q", marshalErr.Error())
	}
	if !errors.Is(marshalErr, cause) {
		t.Fatal("PublicKeyMarshalError must unwrap to its cause")
	}
}

// TestSigningKeyManagerUninitializedSnapshot exercises the defensive nil-snapshot
// guards on every read path. A zero-value manager (no snapshot stored) must
// degrade to safe, typed errors and empty results rather than panic.
func TestSigningKeyManagerUninitializedSnapshot(t *testing.T) {
	var m SigningKeyManager // no snapshot stored

	kid, signer, alg := m.Current()
	if kid != "" || signer != nil || alg != "" {
		t.Fatalf("Current on uninitialized manager must be zero, got (%q,%v,%q)", kid, signer, alg)
	}

	// Algorithm degrades to the constant default even without a snapshot so
	// callers configuring metadata never observe an empty alg.
	if got := m.Algorithm(); got != SigningAlgorithmES256 {
		t.Fatalf("Algorithm default = %q, want %q", got, SigningAlgorithmES256)
	}

	if _, err := m.SignerForKID("any"); !errors.As(err, new(*SigningKeyUnavailableError)) {
		t.Fatalf("SignerForKID on uninitialized manager: want SigningKeyUnavailableError, got %T", err)
	}
	if _, err := m.PublicForKID("any"); !errors.As(err, new(*SigningKeyUnavailableError)) {
		t.Fatalf("PublicForKID on uninitialized manager: want SigningKeyUnavailableError, got %T", err)
	}

	jwks := m.JWKS()
	if jwks == nil || len(jwks.Keys) != 0 {
		t.Fatalf("JWKS on uninitialized manager must be empty, got %+v", jwks)
	}

	if err := m.Rotate(); !errors.As(err, new(*SigningKeyUnavailableError)) {
		t.Fatalf("Rotate on uninitialized manager: want SigningKeyUnavailableError, got %T", err)
	}
}

// TestNewSigningKeyManagerMissingCNTypedError pins the typed error returned when
// the signing key common name is empty.
func TestNewSigningKeyManagerMissingCNTypedError(t *testing.T) {
	_, err := NewSigningKeyManagerFromXKMS("", "")
	var cfgErr *OIDCConfigRequiredError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected OIDCConfigRequiredError for empty CN, got %T (%v)", err, err)
	}
}

// TestRotationKIDFormat verifies the fully-qualified xkms key reference used for
// in-place rotation carries the resolved store type and the ECDSA P-256 signing
// segments, which backends require to address the key for regeneration.
func TestRotationKIDFormat(t *testing.T) {
	m := &SigningKeyManager{cn: "oidc-signing", storeType: "software"}
	got := m.rotationKID()
	const want = "software:signing:ecdsa-p256:oidc-signing"
	if got != want {
		t.Fatalf("rotationKID() = %q, want %q", got, want)
	}
}
