//go:build !integration

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

// ceremony_wiring_errors_test.go covers the typed-error Error() / Unwrap()
// surface of the ceremony wiring path. The happy-path constructor lives in
// ceremony_wiring.go and is tested elsewhere via the lifecycle integration
// tests; this file exercises the diagnostic shape operators see when the
// wiring fails.

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCeremonyWiringError_FormatAndUnwrap(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("disk full")
		e := &CeremonyWiringError{Stage: "barrier", Cause: cause}
		got := e.Error()
		assert.Contains(t, got, "ceremony wiring")
		assert.Contains(t, got, "barrier")
		assert.Contains(t, got, "disk full")
		assert.Same(t, cause, e.Unwrap())
		assert.True(t, errors.Is(e, cause))
	})

	t.Run("without cause", func(t *testing.T) {
		e := &CeremonyWiringError{Stage: "credential-core"}
		got := e.Error()
		assert.Contains(t, got, "ceremony wiring")
		assert.Contains(t, got, "credential-core")
		// Trailing ": " from the with-cause branch must NOT be present.
		assert.NotContains(t, got, "failed:")
		assert.Nil(t, e.Unwrap())
	})
}

func TestSOCertSPKIThumbprintError_Format(t *testing.T) {
	e := &SOCertSPKIThumbprintError{Reason: "missing SPKI"}
	got := e.Error()
	assert.Contains(t, got, "so cert spki thumbprint")
	assert.Contains(t, got, "missing SPKI")
}

// =============================================================================
// SOCertSPKIThumbprint / extractSPKI
//
// These pure functions compute the RFC 7517 §4.8 SPKI thumbprint used for
// SO-cert admin binding. They are exercised here against a real ECDSA
// certificate generated in-test (via the CA-adapter test helper) plus the
// empty / malformed-input guards.
// =============================================================================

func TestSOCertSPKIThumbprint_ValidCertProducesStableThumbprint(t *testing.T) {
	ca := newFakeTrustedCA(t)

	tp1, err := SOCertSPKIThumbprint(ca.bundlePEM)
	if err != nil {
		t.Fatalf("SOCertSPKIThumbprint: %v", err)
	}
	if tp1 == "" {
		t.Fatal("expected a non-empty thumbprint")
	}

	// The thumbprint is deterministic for the same certificate.
	tp2, err := SOCertSPKIThumbprint(ca.bundlePEM)
	if err != nil {
		t.Fatalf("SOCertSPKIThumbprint (2): %v", err)
	}
	if tp1 != tp2 {
		t.Fatalf("thumbprint must be stable: %q != %q", tp1, tp2)
	}
}

func TestSOCertSPKIThumbprint_EmptyInputReturnsTypedError(t *testing.T) {
	_, err := SOCertSPKIThumbprint(nil)
	if !errors.Is(err, ErrSOCertEmpty) {
		t.Fatalf("want ErrSOCertEmpty, got %v", err)
	}
}

func TestSOCertSPKIThumbprint_NonPEMInputReturnsTypedError(t *testing.T) {
	_, err := SOCertSPKIThumbprint([]byte("this is not a pem block"))
	if !errors.Is(err, ErrSOCertNoPEMBlock) {
		t.Fatalf("want ErrSOCertNoPEMBlock, got %v", err)
	}
}

func TestSOCertSPKIThumbprint_MalformedCertBodyReturnsThumbprintError(t *testing.T) {
	// A valid PEM frame whose body is not a parseable certificate forces
	// extractSPKI's x509.ParseCertificate to fail; SOCertSPKIThumbprint then
	// wraps it as a SOCertSPKIThumbprintError.
	badPEM := []byte("-----BEGIN CERTIFICATE-----\nQUJD\n-----END CERTIFICATE-----\n")
	_, err := SOCertSPKIThumbprint(badPEM)
	if err == nil {
		t.Fatal("expected error for malformed certificate body")
	}
	var tpErr *SOCertSPKIThumbprintError
	if !errors.As(err, &tpErr) {
		t.Fatalf("want *SOCertSPKIThumbprintError, got %T: %v", err, err)
	}
}

// Note: the SPKI-extraction + thumbprint algorithm is owned by go-qrdb
// (pkg/user.ComputeSPKIThumbprint) and tested there; quicbaas only wraps it via
// SOCertSPKIThumbprint (covered by the tests above).
