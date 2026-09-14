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
	"errors"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// testHardwareAttestation builds a hardware AttestationResult. It lives in the
// test package on purpose: producing one requires being INSIDE pkg/iam, which
// is the property that stops a REST handler asserting hardware it never proved.
func testHardwareAttestation(aaguid string) *AttestationResult {
	return &AttestationResult{
		level:  AttestLevelHardware,
		aaguid: aaguid,
		format: "basic_full",
		method: AttestMethodWebAuthn,
	}
}

// TestAttestLevel_IsHardware checks the single predicate every policy keys on.
func TestAttestLevel_IsHardware(t *testing.T) {
	if !AttestLevelHardware.IsHardware() {
		t.Error("AttestLevelHardware.IsHardware() = false")
	}
	for _, level := range []AttestLevel{AttestLevelSoftware, AttestLevelNone, "", "HARDWARE"} {
		if level.IsHardware() {
			t.Errorf("%q.IsHardware() = true, want false", level)
		}
	}
}

// TestAttestLevel_Valid proves the empty level is NOT valid. An unset level is
// an unanswered question, and treating it as a default would reintroduce the
// ambiguity this type removes.
func TestAttestLevel_Valid(t *testing.T) {
	for _, level := range []AttestLevel{AttestLevelNone, AttestLevelSoftware, AttestLevelHardware} {
		if !level.Valid() {
			t.Errorf("%q.Valid() = false, want true", level)
		}
	}
	for _, level := range []AttestLevel{"", "unknown", "Hardware"} {
		if level.Valid() {
			t.Errorf("%q.Valid() = true, want false", level)
		}
	}
}

// TestAttestationResult_NilIsNotHardware is the contract every caller relies
// on: an absent proof must never satisfy a hardware requirement, and reading a
// nil result must not panic.
func TestAttestationResult_NilIsNotHardware(t *testing.T) {
	var result *AttestationResult

	if result.IsHardware() {
		t.Fatal("a nil AttestationResult reported hardware")
	}
	if got := result.Level(); got != "" {
		t.Errorf("nil Level() = %q, want empty", got)
	}
	if got := result.AAGUID(); got != "" {
		t.Errorf("nil AAGUID() = %q, want empty", got)
	}
	if got := result.Format(); got != "" {
		t.Errorf("nil Format() = %q, want empty", got)
	}
	if got := result.Method(); got != "" {
		t.Errorf("nil Method() = %q, want empty", got)
	}
}

// TestAttestationResult_Accessors covers the populated case.
func TestAttestationResult_Accessors(t *testing.T) {
	result := testHardwareAttestation("cb69481e")

	if !result.IsHardware() {
		t.Error("IsHardware() = false")
	}
	if got := result.Level(); got != AttestLevelHardware {
		t.Errorf("Level() = %q, want %q", got, AttestLevelHardware)
	}
	if got := result.AAGUID(); got != "cb69481e" {
		t.Errorf("AAGUID() = %q", got)
	}
	if got := result.Format(); got != "basic_full" {
		t.Errorf("Format() = %q", got)
	}
	if got := result.Method(); got != AttestMethodWebAuthn {
		t.Errorf("Method() = %q", got)
	}
}

// TestAttestationRequiredError_Message checks each refusal names what was
// wrong AND what the operator should do about it — the "operator-facing
// reason" #381 asks for.
func TestAttestationRequiredError_Message(t *testing.T) {
	none := (&AttestationRequiredError{
		Operation: "webauthn enrollment",
		Required:  AttestLevelHardware,
		Proven:    AttestLevelNone,
		Format:    "none",
	}).Error()
	for _, want := range []string{"webauthn enrollment", "hardware", "no attestation statement", "hardware security key"} {
		if !strings.Contains(none, want) {
			t.Errorf("no-attestation refusal missing %q: %s", want, none)
		}
	}

	software := (&AttestationRequiredError{
		Operation: "piv login",
		Required:  AttestLevelHardware,
		Proven:    AttestLevelSoftware,
	}).Error()
	if !strings.Contains(software, "self-signed") {
		t.Errorf("self-attestation refusal missing the reason: %s", software)
	}

	// A nil receiver must still produce a usable message rather than panic.
	var nilErr *AttestationRequiredError
	if msg := nilErr.Error(); msg == "" {
		t.Error("nil AttestationRequiredError produced an empty message")
	}
}

// TestAttestationUnprovenError_Message covers the wiring-defect error.
func TestAttestationUnprovenError_Message(t *testing.T) {
	msg := (&AttestationUnprovenError{Operation: "piv login"}).Error()
	if !strings.Contains(msg, "piv login") || !strings.Contains(msg, "never asserted by the caller") {
		t.Errorf("unexpected message: %s", msg)
	}

	var nilErr *AttestationUnprovenError
	if got := nilErr.Error(); got == "" {
		t.Error("nil AttestationUnprovenError produced an empty message")
	}
	if got := (&AttestationUnprovenError{}).Error(); got == "" {
		t.Error("empty AttestationUnprovenError produced an empty message")
	}
}

// TestAttestLevelOrNone maps an absent result onto a nameable level so refusal
// messages never print an empty string.
func TestAttestLevelOrNone(t *testing.T) {
	if got := attestLevelOrNone(nil); got != AttestLevelNone {
		t.Errorf("attestLevelOrNone(nil) = %q, want %q", got, AttestLevelNone)
	}
	if got := attestLevelOrNone(&AttestationResult{}); got != AttestLevelNone {
		t.Errorf("attestLevelOrNone(zero) = %q, want %q", got, AttestLevelNone)
	}
	if got := attestLevelOrNone(testHardwareAttestation("x")); got != AttestLevelHardware {
		t.Errorf("attestLevelOrNone(hardware) = %q, want %q", got, AttestLevelHardware)
	}
}

// TestAttestLevel_String keeps the level printable for logs and claims.
func TestAttestLevel_String(t *testing.T) {
	for level, want := range map[AttestLevel]string{
		AttestLevelHardware: "hardware",
		AttestLevelSoftware: "software",
		AttestLevelNone:     "none",
		AttestLevel(""):     "",
	} {
		if got := level.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", level, got, want)
		}
	}
}

// -----------------------------------------------------------------------------
// Auth-factor metadata: the rule every raw write surface shares
// -----------------------------------------------------------------------------

// TestValidateAuthFactorMetadata_RefusesAttestationFields is the guard on every
// surface that writes an auth factor WITHOUT running a ceremony — the SDK's Add
// and Rotate, and the qrdb servicer proxy behind the gRPC UserAuthFactorService
// and the MCP qrdb_user_authfactor_* tools.
//
// Those surfaces proved nothing, so a value they supplied would be a forged
// claim wearing the field name login policy trusts.
func TestValidateAuthFactorMetadata_RefusesAttestationFields(t *testing.T) {
	for _, field := range []string{AuthFactorMetadataAttestLevel, AuthFactorMetadataAttestAAGUID} {
		t.Run(field, func(t *testing.T) {
			err := ValidateAuthFactorMetadata(map[string]string{field: "hardware"})
			if err == nil {
				t.Fatalf("a caller-supplied %s was accepted", field)
			}
			var typed *AuthFactorAttestationMetadataError
			if !errors.As(err, &typed) {
				t.Fatalf("want *AuthFactorAttestationMetadataError, got %T", err)
			}
			if typed.Field != field {
				t.Errorf("Field = %q, want %q", typed.Field, field)
			}
			if !strings.Contains(err.Error(), field) {
				t.Errorf("the refusal does not name the field: %v", err)
			}
		})
	}
}

// TestValidateAuthFactorMetadata_AllowsOrdinaryMetadata keeps the refusal
// narrow.
func TestValidateAuthFactorMetadata_AllowsOrdinaryMetadata(t *testing.T) {
	for name, metadata := range map[string]map[string]string{
		"nil":      nil,
		"empty":    {},
		"ordinary": {"tenant_id": "acme", "aaguid": "cb69481e", "transports": "usb"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAuthFactorMetadata(metadata); err != nil {
				t.Fatalf("ordinary metadata was refused: %v", err)
			}
		})
	}

	// A nil receiver on the error type still renders.
	var nilErr *AuthFactorAttestationMetadataError
	if nilErr.Error() == "" {
		t.Error("nil AuthFactorAttestationMetadataError produced an empty message")
	}
	if (&AuthFactorAttestationMetadataError{}).Error() == "" {
		t.Error("empty AuthFactorAttestationMetadataError produced an empty message")
	}
}

// TestVerifiedAttestationFor renders a ceremony result as the value qrdb's
// AttestationRecorder records: the proven level and model when there is one,
// and qrdb's Unattested — never a guess — when there is not.
func TestVerifiedAttestationFor(t *testing.T) {
	if got := VerifiedAttestationFor(nil, "aabb"); got != qrdbsdk.Unattested() {
		t.Errorf("nil result = %+v, want Unattested", got)
	}
	if got := VerifiedAttestationFor(&AttestationResult{level: "bogus"}, "aabb"); got != qrdbsdk.Unattested() {
		t.Errorf("invalid level = %+v, want Unattested", got)
	}
	got := VerifiedAttestationFor(&AttestationResult{level: AttestLevelHardware}, "aabb")
	if got.Level != qrdbsdk.AttestationLevelHardware || got.AAGUID != "aabb" {
		t.Errorf("hardware result = %+v", got)
	}
	// The vocabulary is the SDK's, byte for byte, so a level crosses the
	// boundary by conversion alone.
	if string(AttestLevelNone) != string(qrdbsdk.AttestationLevelNone) ||
		string(AttestLevelSoftware) != string(qrdbsdk.AttestationLevelSoftware) ||
		string(AttestLevelHardware) != string(qrdbsdk.AttestationLevelHardware) {
		t.Error("AttestLevel values drifted from the SDK's AttestationLevel values")
	}
	if AuthFactorMetadataAttestLevel != qrdbsdk.MetadataAttestLevel || AuthFactorMetadataAttestAAGUID != qrdbsdk.MetadataAttestAAGUID {
		t.Error("metadata keys drifted from the SDK's")
	}
}

// TestValidateAuthFactorMetadata_ReservedPrefix pins that the refusal is
// qrdb's prefix rule: any attest_* key, in any case, is refused, and a key that
// merely starts with "attestation" is not.
func TestValidateAuthFactorMetadata_ReservedPrefix(t *testing.T) {
	for _, key := range []string{"attest_level", "ATTEST_LEVEL", "attest_aaguid", "attest_future_field", "Attest_X"} {
		err := ValidateAuthFactorMetadata(map[string]string{key: "hardware"})
		var reserved *AuthFactorAttestationMetadataError
		if !errors.As(err, &reserved) || reserved.Field != key {
			t.Errorf("%q: expected AuthFactorAttestationMetadataError naming the key, got %v", key, err)
		}
	}
	for _, key := range []string{"attestation_root", "attestation_type", "tenant_id", "attes"} {
		if err := ValidateAuthFactorMetadata(map[string]string{key: "x"}); err != nil {
			t.Errorf("%q: unexpected refusal %v", key, err)
		}
	}
	if err := ValidateAuthFactorMetadata(nil); err != nil {
		t.Errorf("nil metadata refused: %v", err)
	}
}
