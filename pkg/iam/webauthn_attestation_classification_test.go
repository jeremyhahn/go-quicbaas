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

package iam

import (
	"context"
	"errors"
	"net/http"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// The gate says what its refusals ARE, once, for every transport.
//
// A refusal here is a decision about the caller's authenticator, not a fault in
// the server. Six surfaces can reach this gate — self-service registration,
// invite activation, the xkms compat bridge, gRPC, MCP and the embedded
// transport — and each has its own error translator. Every one of them already
// routes by the QBError code, so carrying the classification on the error is
// what makes them all correct without any of them knowing these types exist.
//
// The alternative, which is what shipped, is each translator matching on the
// concrete types. Two of six were taught; the rest returned 500 for a rejected
// security key.
// =============================================================================

// wantForbidden asserts that a gate refusal carries the canonical forbidden
// classification AND still exposes its concrete type, since a caller that needs
// the detail — which level was proven, which model was claimed — must not lose
// it to the wrapping.
func wantForbidden(t *testing.T, err error) {
	t.Helper()

	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Fatalf("refusal carries no QBError classification, so every transport must guess: %T: %v", err, err)
	}
	if qbErr.Code != dberrors.ErrForbidden {
		t.Errorf("refusal classified as %v, want %v", qbErr.Code, dberrors.ErrForbidden)
	}
	if got := qbErr.Code.HTTPStatus(); got != http.StatusForbidden {
		t.Errorf("refusal maps to HTTP %d, want %d", got, http.StatusForbidden)
	}
}

// TestAttestationGateRefusals_AreClassifiedForbidden covers all three ways the
// gate turns an enrollment away.
func TestAttestationGateRefusals_AreClassifiedForbidden(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)
	otherAAGUID := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}

	t.Run("software authenticator", func(t *testing.T) {
		_, err := gate.Evaluate(context.Background(), "t1", "admin@example.com", "basic_surrogate", yubiKey5AAGUID)
		wantForbidden(t, err)

		var required *AttestationRequiredError
		if !errors.As(err, &required) {
			t.Fatalf("the concrete refusal was lost in the wrapping: %T", err)
		}
		if required.Proven != AttestLevelSoftware {
			t.Errorf("Proven = %q, want %q", required.Proven, AttestLevelSoftware)
		}
	})

	t.Run("unverified attestation chain", func(t *testing.T) {
		_, err := gate.Evaluate(context.Background(), "t1", "admin@example.com", "basic_full", yubiKey5AAGUID)
		wantForbidden(t, err)

		var unverified *AttestationChainUnverifiedError
		if !errors.As(err, &unverified) {
			t.Fatalf("the concrete refusal was lost in the wrapping: %T", err)
		}
	})

	t.Run("chain vouches for another model", func(t *testing.T) {
		// A verified path covering a DIFFERENT authenticator than the
		// credential reports. Building it requires being inside this package,
		// which is the point: the evidence cannot be fabricated from outside.
		_, err := gate.Evaluate(verifiedChainContext(otherAAGUID), "t1", "admin@example.com",
			"basic_full", yubiKey5AAGUID)
		wantForbidden(t, err)

		var mismatch *AttestationChainMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("the concrete refusal was lost in the wrapping: %T", err)
		}
	})
}

// TestAttestationGateRefusals_AAGUIDDenialIsAlreadyForbidden records that the
// sibling refusal shares the classification.
//
// It is not new behaviour — the allowlist denial has always been a forbidden
// QBError — but it is the reason the attestation refusals were the anomaly
// rather than the pattern, and pinning it here keeps the family from drifting
// apart again.
func TestAttestationGateRefusals_AAGUIDDenialIsAlreadyForbidden(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)
	otherAAGUID := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
		0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}

	// Genuinely attested hardware, of a model the operator did not allowlist.
	_, err := gate.Evaluate(verifiedChainContext(otherAAGUID), "t1", "admin@example.com",
		"basic_full", otherAAGUID)
	if err == nil {
		t.Fatal("a non-allowlisted model was accepted")
	}
	wantForbidden(t, err)
}

// TestAttestationGateRefusals_AdmittedEnrollmentIsNotAnError is the control.
// The classification must attach to refusals only: an accepted authenticator
// returns a result and no error at all, so nothing downstream can read a
// success as a forbidden decision.
func TestAttestationGateRefusals_AdmittedEnrollmentIsNotAnError(t *testing.T) {
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)

	result, err := gate.Evaluate(verifiedChainContext(yubiKey5AAGUID), "t1", "admin@example.com",
		"basic_full", yubiKey5AAGUID)
	if err != nil {
		t.Fatalf("allowlisted, chain-verified hardware was refused: %v", err)
	}
	if !result.IsHardware() {
		t.Fatalf("Level = %q, want %q", result.Level(), AttestLevelHardware)
	}
}
