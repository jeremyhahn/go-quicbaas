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

//go:build integration

package factors

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// vendorAAGUIDs is a deterministic set of fake AAGUIDs used to
// emulate distinct FIDO2 hardware vendors. The exact byte patterns are
// not significant; what matters is that they are pairwise unique.
var (
	vendorAAGUIDYubico   = [16]byte{0x59, 0x55, 0x42, 0x49, 0x43, 0x4f, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	vendorAAGUIDSoloKeys = [16]byte{0x53, 0x4f, 0x4c, 0x4f, 0x4b, 0x45, 0x59, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}
)

// TestM13_PerFactorLogin_SameSubject verifies spec §M13.1: a single
// administrator may enrol heterogeneous hardware factors and
// authenticate with any of them, with every successful resolution
// yielding the same Principal.
//
// Enrolment matrix (5 factors / 3 types / 4 distinct "vendors"):
//
//	mTLS  : 1 self-signed client cert
//	FIDO2 : 2 virtual authenticators, distinct AAGUIDs (Yubico, SoloKeys)
//	PIV   : 2 self-signed slot certs, distinct vendor/slot metadata
//
// Acceptance: resolver.Resolve(type, key) returns principal.ID = admin
// for every factor and the returned factor's ID matches the enrolled ID.
func TestM13_PerFactorLogin_SameSubject(t *testing.T) {
	h := newHarness(t)

	// mTLS factor.
	mtls := newMTLSClientCert(t, "admin-mtls-laptop")
	mtlsID := h.addFactor(t, mtls.Factor)

	// FIDO2 factors with distinct AAGUIDs.
	fidoA := newFIDO2VendorAuthenticator(t, vendorAAGUIDYubico, "yubico-yubikey-5c")
	fidoB := newFIDO2VendorAuthenticator(t, vendorAAGUIDSoloKeys, "solokeys-solo2")
	fidoAID := h.addFactor(t, fidoA.Factor)
	fidoBID := h.addFactor(t, fidoB.Factor)

	// PIV factors with distinct vendor/slot metadata.
	pivA := newPIVVendorCard(t, "yubico", "9a")
	pivB := newPIVVendorCard(t, "thales", "9c")
	pivAID := h.addFactor(t, pivA.Factor)
	pivBID := h.addFactor(t, pivB.Factor)

	// Drive every factor through the resolver and assert identity
	// invariance plus per-factor traceability.
	cases := []struct {
		name string
		ft   qrdbsdk.AuthFactorType
		key  []byte
		want string
	}{
		{name: "mtls/laptop", ft: qrdbsdk.AuthFactorMTLS, key: mtls.Factor.Credential, want: mtlsID},
		{name: "fido2/yubico", ft: qrdbsdk.AuthFactorFIDO2, key: fidoA.Factor.Credential, want: fidoAID},
		{name: "fido2/solokeys", ft: qrdbsdk.AuthFactorFIDO2, key: fidoB.Factor.Credential, want: fidoBID},
		{name: "piv/yubico-9a", ft: qrdbsdk.AuthFactorPIV, key: pivA.Factor.Credential, want: pivAID},
		{name: "piv/thales-9c", ft: qrdbsdk.AuthFactorPIV, key: pivB.Factor.Credential, want: pivBID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			principal, factor := h.resolveOrFatal(t, tc.ft, tc.key)
			if principal == nil {
				t.Fatalf("nil principal")
			}
			if principal.ID != testAdminUserID {
				t.Fatalf("principal.ID = %d, want %d", principal.ID, testAdminUserID)
			}
			if principal.Username != testAdminUsername {
				t.Fatalf("principal.Username = %q, want %q", principal.Username, testAdminUsername)
			}
			if factor == nil {
				t.Fatalf("nil factor")
			}
			if factor.ID != tc.want {
				t.Fatalf("factor.ID = %q, want %q", factor.ID, tc.want)
			}
			if factor.Type != tc.ft {
				t.Fatalf("factor.Type = %q, want %q", factor.Type, tc.ft)
			}
		})
	}
}

// TestM13_MTLSRotation_OldRejected_NewAccepted verifies spec §M13.2:
// rotating an mTLS factor revokes the old credential atomically with
// the addition of the new credential. The user's identity (Principal)
// must remain unchanged.
//
// Acceptance:
//
//	resolver.Resolve(MTLS, oldFingerprint) -> ErrFactorRevoked
//	resolver.Resolve(MTLS, newFingerprint) -> success, principal.ID unchanged
func TestM13_MTLSRotation_OldRejected_NewAccepted(t *testing.T) {
	h := newHarness(t)

	// Enrol the original mTLS factor.
	oldCert := newMTLSClientCert(t, "admin-mtls-old")
	oldID := h.addFactor(t, oldCert.Factor)

	// Confirm the original works before rotation.
	principalBefore, _ := h.resolveOrFatal(t, qrdbsdk.AuthFactorMTLS, oldCert.Factor.Credential)
	if principalBefore.ID != testAdminUserID {
		t.Fatalf("pre-rotation principal.ID = %d, want %d", principalBefore.ID, testAdminUserID)
	}

	// Mint the replacement credential and rotate.
	newCert := newMTLSClientCert(t, "admin-mtls-new")
	if err := h.svc.Rotate(context.Background(), oldID, newCert.Factor); err != nil {
		t.Fatalf("svc.Rotate: %v", err)
	}

	// Old credential must now resolve to FactorRevokedError.
	h.resolveExpectErr(t, qrdbsdk.AuthFactorMTLS, oldCert.Factor.Credential, qrdbsdk.ErrFactorRevoked)

	// New credential must succeed AND map to the same admin Principal.
	principalAfter, factorAfter := h.resolveOrFatal(t, qrdbsdk.AuthFactorMTLS, newCert.Factor.Credential)
	if principalAfter.ID != testAdminUserID {
		t.Fatalf("post-rotation principal.ID = %d, want %d", principalAfter.ID, testAdminUserID)
	}
	if principalAfter.ID != principalBefore.ID {
		t.Fatalf("principal.ID changed across rotation: before=%d after=%d",
			principalBefore.ID, principalAfter.ID)
	}
	if factorAfter.ID != newCert.Factor.ID {
		t.Fatalf("post-rotation factor.ID = %q, want %q", factorAfter.ID, newCert.Factor.ID)
	}
	if factorAfter.ID == oldID {
		t.Fatalf("rotation reused old factor ID %q (expected a fresh ID)", oldID)
	}
}

// TestM13_FIDO2_SoftRevoke_PeerFactorStillWorks verifies spec §M13.3:
// soft-revoking one FIDO2 credential from a vendor MUST NOT impact a
// peer FIDO2 credential from a different vendor enrolled by the same
// user.
//
// Acceptance:
//
//	svc.Remove(vendorAID, false)                            -> nil (peer keeps user active)
//	resolver.Resolve(FIDO2, vendorACredID)                  -> ErrFactorRevoked
//	resolver.Resolve(FIDO2, vendorBCredID)                  -> success
//	userStateStub.SetUserActive was NOT invoked (force=false path)
func TestM13_FIDO2_SoftRevoke_PeerFactorStillWorks(t *testing.T) {
	h := newHarness(t)

	vendorA := newFIDO2VendorAuthenticator(t, vendorAAGUIDYubico, "yubico-yubikey-5c")
	vendorB := newFIDO2VendorAuthenticator(t, vendorAAGUIDSoloKeys, "solokeys-solo2")
	vendorAID := h.addFactor(t, vendorA.Factor)
	h.addFactor(t, vendorB.Factor)

	// Soft revoke vendor A. Because vendor B is still active, this must
	// succeed without tripping the last-factor guard.
	if err := h.svc.Remove(context.Background(), vendorAID, false); err != nil {
		t.Fatalf("svc.Remove(vendorA, force=false): %v", err)
	}

	// Vendor A credential must now be rejected with FactorRevokedError.
	h.resolveExpectErr(t, qrdbsdk.AuthFactorFIDO2, vendorA.Factor.Credential, qrdbsdk.ErrFactorRevoked)

	// Vendor B credential must continue to authenticate the same admin.
	principal, factor := h.resolveOrFatal(t, qrdbsdk.AuthFactorFIDO2, vendorB.Factor.Credential)
	if principal.ID != testAdminUserID {
		t.Fatalf("vendorB principal.ID = %d, want %d", principal.ID, testAdminUserID)
	}
	if factor.ID != vendorB.Factor.ID {
		t.Fatalf("vendorB factor.ID = %q, want %q", factor.ID, vendorB.Factor.ID)
	}

	// Soft-revoke MUST NOT invoke SetUserActive. The user-disable hook
	// is only fired by force=true Remove on the user's last active
	// factor.
	if calls := h.users.Calls(); len(calls) != 0 {
		t.Fatalf("SetUserActive must not be invoked by force=false Remove; got %d calls: %+v",
			len(calls), calls)
	}
}

// TestM13_LastFactorProtection_ReturnsErrLastFactor verifies spec
// §M13.4: the service's last-factor guard prevents accidental lockout.
// A force=false Remove on the user's last active factor MUST return a
// typed LastFactorError without disabling the user. A subsequent
// force=true Remove on the same factor MUST succeed AND disable the
// user via the UserStateUpdater callback.
//
// Acceptance:
//
//	svc.Remove(last, false) -> LastFactorError; errors.Is(err, ErrLastFactor)
//	                           AND errors.As to *LastFactorError
//	                           AND userStateStub.Calls() is empty
//	svc.Remove(last, true)  -> nil
//	                           AND userStateStub.Calls() contains
//	                               (adminUserID, false)
func TestM13_LastFactorProtection_ReturnsErrLastFactor(t *testing.T) {
	h := newHarness(t)

	// Enrol three factors of different types so the test exercises the
	// full chain of soft-removes down to the last credential.
	mtls := newMTLSClientCert(t, "admin-mtls-laptop")
	mtlsID := h.addFactor(t, mtls.Factor)

	fido := newFIDO2VendorAuthenticator(t, vendorAAGUIDYubico, "yubico-yubikey-5c")
	fidoID := h.addFactor(t, fido.Factor)

	piv := newPIVVendorCard(t, "yubico", "9a")
	pivID := h.addFactor(t, piv.Factor)

	// Soft-remove down to the last factor (PIV). These calls must
	// succeed because there are still other active factors.
	if err := h.svc.Remove(context.Background(), mtlsID, false); err != nil {
		t.Fatalf("svc.Remove(mtls, force=false): %v", err)
	}
	if err := h.svc.Remove(context.Background(), fidoID, false); err != nil {
		t.Fatalf("svc.Remove(fido, force=false): %v", err)
	}

	// Attempt to soft-remove the final remaining factor — this MUST be
	// blocked by the last-factor guard.
	err := h.svc.Remove(context.Background(), pivID, false)
	if err == nil {
		t.Fatalf("svc.Remove(piv, force=false): expected LastFactorError, got nil")
	}
	if !errors.Is(err, qrdbsdk.ErrLastFactor) {
		t.Fatalf("svc.Remove(piv, force=false): errors.Is(err, ErrLastFactor) = false; err=%v (%T)", err, err)
	}
	var lfe *qrdbsdk.LastFactorError
	if !errors.As(err, &lfe) {
		t.Fatalf("svc.Remove(piv, force=false): errors.As(err, *LastFactorError) = false; err=%v (%T)", err, err)
	}
	if lfe.UserID != testAdminUserID {
		t.Fatalf("LastFactorError.UserID = %d, want %d", lfe.UserID, testAdminUserID)
	}
	if lfe.FactorID != pivID {
		t.Fatalf("LastFactorError.FactorID = %q, want %q", lfe.FactorID, pivID)
	}

	// The user MUST NOT have been force-disabled by the rejected
	// force=false call.
	if calls := h.users.Calls(); len(calls) != 0 {
		t.Fatalf("SetUserActive must not be invoked when Remove returns LastFactorError; got %d calls: %+v",
			len(calls), calls)
	}

	// The PIV factor MUST still resolve successfully — the failed
	// Remove must not have left the record in a partial state.
	principal, factor := h.resolveOrFatal(t, qrdbsdk.AuthFactorPIV, piv.Factor.Credential)
	if principal.ID != testAdminUserID {
		t.Fatalf("post-reject principal.ID = %d, want %d", principal.ID, testAdminUserID)
	}
	if factor.ID != pivID {
		t.Fatalf("post-reject factor.ID = %q, want %q", factor.ID, pivID)
	}

	// Now force-remove. This MUST succeed AND fire the user-disable
	// hook exactly once.
	if err := h.svc.Remove(context.Background(), pivID, true); err != nil {
		t.Fatalf("svc.Remove(piv, force=true): %v", err)
	}

	calls := h.users.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected SetUserActive to be invoked exactly once; got %d: %+v", len(calls), calls)
	}
	if calls[0].UserID != testAdminUserID {
		t.Fatalf("SetUserActive userID = %d, want %d", calls[0].UserID, testAdminUserID)
	}
	if calls[0].Active {
		t.Fatalf("SetUserActive active = true, want false (force-removed last factor must disable user)")
	}

	// And the credential must now be permanently rejected.
	h.resolveExpectErr(t, qrdbsdk.AuthFactorPIV, piv.Factor.Credential, qrdbsdk.ErrFactorRevoked)
}
