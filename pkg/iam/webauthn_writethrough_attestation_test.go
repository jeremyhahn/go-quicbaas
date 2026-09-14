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
	"encoding/hex"
	"errors"
	"testing"

	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// =============================================================================
// The assurance gate at the enrollment choke point
//
// These tests drive webAuthnFactorWriteThrough.Save — the single point every
// completed WebAuthn registration converges on, whichever entry point started
// the ceremony. Asserting the policy here is asserting it on the live
// registration route, which dispatches straight into xkms and never touches
// XKMSWebAuthnManager (where the old, bypassable check lived).
// =============================================================================

// attestedCredential builds a credential carrying the VERIFIED attestation type
// go-webauthn writes after running the attestation format's verification
// procedure, plus the AAGUID from the same attested authenticator data.
func attestedCredential(userID uint64, credID []byte, attestationType string, aaguid []byte) *xkmswebauthn.Credential {
	cred := &xkmswebauthn.Credential{
		ID:              credID,
		UserID:          testUserHandle(userID),
		PublicKey:       []byte("cose-public-key"),
		AttestationType: attestationType,
	}
	cred.Authenticator.AAGUID = aaguid
	return cred
}

// newGatedWriteThrough builds the decorator over xkms's real in-memory stores
// with an explicit gate, so the tests exercise genuine persistence semantics.
func newGatedWriteThrough(
	t *testing.T,
	gate *WebAuthnAttestationGate,
	factors *fakeUserAuthFactorClient,
) (*webAuthnFactorWriteThrough, *xkmswebauthn.MemoryUserStore, xkmswebauthn.CredentialStore) {
	t.Helper()
	users := xkmswebauthn.NewMemoryUserStore()
	creds := xkmswebauthn.NewMemoryCredentialStore()
	wt, err := newWebAuthnFactorWriteThrough(creds, users, factors, recorderOf(factors), testWriteThroughTenant, gate, nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}
	return wt, users, creds
}

// TestWriteThroughSave_RefusesUnattestedAdminEnrollment is the end-to-end shape
// of #381's headline case: an operator completes a WebAuthn ceremony with a
// virtual authenticator for an administrative account, and the registration is
// refused before ANYTHING is persisted.
func TestWriteThroughSave_RefusesUnattestedAdminEnrollment(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{"admin@example.com": true}},
	)
	wt, users, creds := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 42, "admin@example.com", "admin's browser")

	credID := []byte{1, 2, 3, 4}
	err := wt.Save(context.Background(), attestedCredential(42, credID, "none", virtualAAGUID))
	if err == nil {
		t.Fatal("a virtual authenticator was enrolled as an administrator credential")
	}
	var required *AttestationRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("want *AttestationRequiredError, got %T: %v", err, err)
	}

	// NOTHING may be written. The refusal runs before the credential Save, so
	// there is no residue for an operator to clean up — and, critically, no
	// window in which the refused credential exists.
	if _, getErr := creds.GetByCredentialID(context.Background(), credID); getErr == nil {
		t.Error("a refused registration persisted the credential")
	}
	if len(factors.added) != 0 {
		t.Errorf("a refused registration enrolled %d auth factor(s)", len(factors.added))
	}
}

// TestWriteThroughSave_AcceptsAttestedHardwareAndRecordsLevel is the positive
// case, and it also pins the second half of scope item 3: the stored factor
// STATES its attested level, so login can later require hardware.
func TestWriteThroughSave_AcceptsAttestedHardwareAndRecordsLevel(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 42, "admin@example.com", "admin's yubikey")

	cred := attestedCredential(42, []byte{1, 2, 3, 4}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(verifiedChainContext(yubiKey5AAGUID), cred); err != nil {
		t.Fatalf("an allowlisted hardware authenticator was refused: %v", err)
	}

	if len(factors.added) != 1 {
		t.Fatalf("want 1 enrolled factor, got %d", len(factors.added))
	}
	meta := factors.added[0].Metadata
	if got := meta[AuthFactorMetadataAttestLevel]; got != string(AttestLevelHardware) {
		t.Errorf("factor metadata %s = %q, want %q", AuthFactorMetadataAttestLevel, got, AttestLevelHardware)
	}
	if got, want := meta[AuthFactorMetadataAttestAAGUID], hex.EncodeToString(yubiKey5AAGUID); got != want {
		t.Errorf("factor metadata aaguid = %q, want %q", got, want)
	}
	if got := meta["attestation_type"]; got != "basic_full" {
		t.Errorf("factor metadata attestation_type = %q, want basic_full", got)
	}
}

// TestWriteThroughSave_PermissiveTenantRecordsSoftware proves the explicit
// opt-in does not produce an ambiguous record. A tenant user may enroll an
// unattested authenticator, but the stored factor says so — which is what stops
// it from later being mistaken for hardware.
func TestWriteThroughSave_PermissiveTenantRecordsSoftware(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{"admin@example.com": true}},
	)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 7, "tenant@example.com", "tenant laptop")

	cred := attestedCredential(7, []byte{9, 9, 9, 9}, "none", virtualAAGUID)
	if err := wt.Save(context.Background(), cred); err != nil {
		t.Fatalf("permissive tenant enrollment refused: %v", err)
	}

	if len(factors.added) != 1 {
		t.Fatalf("want 1 enrolled factor, got %d", len(factors.added))
	}
	if got := factors.added[0].Metadata[AuthFactorMetadataAttestLevel]; got != string(AttestLevelNone) {
		t.Errorf("factor metadata %s = %q, want %q", AuthFactorMetadataAttestLevel, got, AttestLevelNone)
	}
}

// TestWriteThroughSave_NilGateRefuses is the write-through's own "unconfigured
// deployment denies" regression: a decorator constructed without a gate must
// refuse rather than persist.
func TestWriteThroughSave_NilGateRefuses(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	wt, users, _ := newGatedWriteThrough(t, nil, factors)
	seedUser(t, users, 42, "admin@example.com", "admin's yubikey")

	cred := attestedCredential(42, []byte{1, 2, 3, 4}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(verifiedChainContext(yubiKey5AAGUID), cred); err == nil {
		t.Fatal("a write-through with no gate persisted a registration")
	}
	if len(factors.added) != 0 {
		t.Errorf("a refused registration enrolled %d auth factor(s)", len(factors.added))
	}
}

// TestWriteThroughSave_UnresolvableRegistrantIsHeldToHardware covers the
// self-service-registration ordering: that flow finishes the ceremony BEFORE
// creating the user, so the registrant cannot be resolved here — and it then
// grants that user Admin. Treating an unresolvable registrant as a tenant user
// would let a permissive deployment enroll a virtual authenticator as a tenant
// administrator.
func TestWriteThroughSave_UnresolvableRegistrantIsHeldToHardware(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{}},
	)
	// Deliberately no seedUser: the registrant is not in the store yet.
	wt, _, _ := newGatedWriteThrough(t, gate, factors)

	cred := attestedCredential(42, []byte{1, 2, 3, 4}, "none", virtualAAGUID)
	if err := wt.Save(context.Background(), cred); err == nil {
		t.Fatal("an unresolvable registrant was admitted under the permissive tenant policy")
	}
	if len(factors.added) != 0 {
		t.Errorf("a refused registration enrolled %d auth factor(s)", len(factors.added))
	}
}

// TestWriteThroughSave_RefusesUnverifiedAttestationChainForAdmin drives the
// trust-path control through the persistence choke point: a forged but
// well-formed manufacturer attestation, with an allowlisted AAGUID and no
// verified chain, writes nothing.
func TestWriteThroughSave_RefusesUnverifiedAttestationChainForAdmin(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)
	wt, users, creds := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 42, "admin@example.com", "admin's totally-real yubikey")

	credID := []byte{7, 7, 7, 7}
	cred := attestedCredential(42, credID, "basic_full", yubiKey5AAGUID)

	// context.Background(): no ceremony entry point verified a trust path.
	err := wt.Save(context.Background(), cred)
	if err == nil {
		t.Fatal("a forged manufacturer attestation was enrolled as hardware")
	}
	var unverified *AttestationChainUnverifiedError
	if !errors.As(err, &unverified) {
		t.Fatalf("want *AttestationChainUnverifiedError, got %T: %v", err, err)
	}
	if _, getErr := creds.GetByCredentialID(context.Background(), credID); getErr == nil {
		t.Error("a refused registration persisted the credential")
	}
	if len(factors.added) != 0 {
		t.Errorf("a refused registration enrolled %d auth factor(s)", len(factors.added))
	}
}

// TestWriteThroughSave_PermissiveTenantNeverRecordsUnverifiedHardware is the
// regression for a defect this work briefly SHIPPED and a test briefly
// codified.
//
// A tenant enrollment under the permissive policy is admitted without a trust
// path — correctly, because that policy does not demand one. But the stored
// factor must still say what was actually proven. Recording
// attest_level=hardware because the authenticator SAID "basic_full" would put a
// forged hardware claim into the exact record login policy reads, which is the
// silent downgrade this whole change exists to remove — just arriving through
// the permissive door instead of the administrative one.
func TestWriteThroughSave_PermissiveTenantNeverRecordsUnverifiedHardware(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{"admin@example.com": true}},
	)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 7, "tenant@example.com", "tenant laptop")

	// The credential claims a manufacturer attestation for an allowlisted
	// model, and no ceremony verified its chain.
	cred := attestedCredential(7, []byte{5, 5, 5, 5}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(context.Background(), cred); err != nil {
		t.Fatalf("permissive tenant enrollment refused: %v", err)
	}

	if len(factors.added) != 1 {
		t.Fatalf("want 1 enrolled factor, got %d", len(factors.added))
	}
	got := factors.added[0].Metadata[AuthFactorMetadataAttestLevel]
	if got == string(AttestLevelHardware) {
		t.Fatal("an unverified basic_full was persisted as attest_level=hardware")
	}
	if got != string(AttestLevelSoftware) {
		t.Errorf("%s = %q, want %q", AuthFactorMetadataAttestLevel, got, AttestLevelSoftware)
	}
}

// TestWriteThroughSave_VerifiedChainRecordsHardware is the counterpart: the
// same credential, with a verified trust path, IS hardware. Without this the
// test above could be satisfied by never recording hardware at all.
func TestWriteThroughSave_VerifiedChainRecordsHardware(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{}},
	)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 7, "tenant@example.com", "tenant yubikey")

	cred := attestedCredential(7, []byte{6, 6, 6, 6}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(verifiedChainContext(yubiKey5AAGUID), cred); err != nil {
		t.Fatalf("a verified manufacturer chain was refused: %v", err)
	}
	if got := factors.added[0].Metadata[AuthFactorMetadataAttestLevel]; got != string(AttestLevelHardware) {
		t.Errorf("%s = %q, want %q", AuthFactorMetadataAttestLevel, got, AttestLevelHardware)
	}
}

// TestWriteThroughSave_RecordsTheProvenResultForTheCaller covers the hand-off
// back out of the ceremony.
//
// The gate runs deep inside the credential store, but self-service registration
// mints a session for the credential it just enrolled and needs to know what
// was proven. Without this, a registrant who had just cleared the hardware bar
// would receive an unattested session and appear weaker than their credential
// until they logged in a second time.
func TestWriteThroughSave_RecordsTheProvenResultForTheCaller(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(WebAuthnAssuranceHardware, hardwareAllowlist(), nil)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 42, "admin@example.com", "admin's yubikey")

	ctx, recorder := contextWithAttestationRecorder(verifiedChainContext(yubiKey5AAGUID))
	cred := attestedCredential(42, []byte{3, 3, 3, 3}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(ctx, cred); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := recorder.get()
	if !got.IsHardware() {
		t.Fatalf("the recorded result is %q, want hardware", got.Level())
	}
	if got.AAGUID() != hex.EncodeToString(yubiKey5AAGUID) {
		t.Errorf("AAGUID = %q", got.AAGUID())
	}
}

// TestWriteThroughSave_RecordsSoftwareForAnUnprovenCredential is the other
// direction: the recorder must carry the level that was actually established,
// so a caller cannot read "hardware" out of a permissive enrollment.
func TestWriteThroughSave_RecordsSoftwareForAnUnprovenCredential(t *testing.T) {
	factors := &fakeUserAuthFactorClient{}
	gate := NewWebAuthnAttestationGate(
		WebAuthnAssuranceAny,
		hardwareAllowlist(),
		&stubAdminResolver{admins: map[string]bool{}},
	)
	wt, users, _ := newGatedWriteThrough(t, gate, factors)
	seedUser(t, users, 7, "tenant@example.com", "tenant laptop")

	ctx, recorder := contextWithAttestationRecorder(context.Background())
	cred := attestedCredential(7, []byte{8, 8, 8, 8}, "basic_full", yubiKey5AAGUID)
	if err := wt.Save(ctx, cred); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if recorder.get().IsHardware() {
		t.Fatal("an unverified credential was recorded as hardware for the caller")
	}
	if got := recorder.get().Level(); got != AttestLevelSoftware {
		t.Errorf("Level = %q, want %q", got, AttestLevelSoftware)
	}
}

// TestAttestationRecorder_NilSafety keeps the hand-off optional: a ceremony
// driven by a path that attached no recorder must not panic, and reading an
// unfilled one yields nothing rather than a false positive.
func TestAttestationRecorder_NilSafety(t *testing.T) {
	var recorder *attestationRecorder
	recorder.set(testHardwareAttestation("x"))
	if recorder.get() != nil {
		t.Error("a nil recorder returned a result")
	}

	// A context with no recorder attached must absorb the call silently.
	recordAttestation(context.Background(), testHardwareAttestation("x"))
	//nolint:staticcheck // deliberately exercising the nil-context guard
	recordAttestation(nil, testHardwareAttestation("x"))

	fresh := &attestationRecorder{}
	if fresh.get() != nil {
		t.Error("an unfilled recorder returned a result")
	}
}
