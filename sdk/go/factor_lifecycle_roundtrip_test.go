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

package quicbaas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// These tests drive a factor's whole lifecycle -- enroll several kinds for
// one user, rotate, soft-revoke, and the last-factor guard -- through
// Security().Auth().Factors() over the real qrdb factor store login reads,
// using buildLoginFactorRoundtrip from factor_enroll_login_roundtrip_test.go.

const (
	lifecycleTenant  = "acme"
	lifecycleSubject = "admin"
	lifecycleUserID  = uint64(1)
)

// certKey returns the login lookup key for a certificate-backed factor
// (mtls or piv): the SHA-256 of its DER bytes.
func certKey(der string) []byte {
	sum := sha256.Sum256([]byte(der))
	return sum[:]
}

// certFactor is an mtls or piv enrollment request for the lifecycle subject.
func certFactor(kind, der string) *AuthFactorEnrollRequest {
	return &AuthFactorEnrollRequest{
		FactorName: kind,
		Subject:    lifecycleSubject,
		TenantID:   lifecycleTenant,
		MethodParams: map[string]any{
			"label":             kind + "-" + der,
			"credential_sha256": hex.EncodeToString(certKey(der)),
		},
	}
}

// fido2Factor is a fido2 enrollment request for the lifecycle subject.
func fido2Factor(credentialID, aaguid string) *AuthFactorEnrollRequest {
	return &AuthFactorEnrollRequest{
		FactorName: "fido2",
		Subject:    lifecycleSubject,
		TenantID:   lifecycleTenant,
		MethodParams: map[string]any{
			"label":         "fido2-" + credentialID,
			"credential_id": credentialID,
			"aaguid":        aaguid,
		},
	}
}

// newLifecycleFixture returns the Factors client for a one-user directory and
// the login store it writes to.
func newLifecycleFixture(t *testing.T) (AuthFactorsClient, qrdbsdk.UserAuthFactorServicer, *directoryUserServicer) {
	t.Helper()
	dir := newDirectory(testUser{id: lifecycleUserID, tenantID: lifecycleTenant, username: lifecycleSubject})
	c, loginStore := buildLoginFactorRoundtrip(t, dir)
	return c.Security().Auth().Factors(), loginStore, dir
}

// requireNotDisabled fails the test if the factor service changed the
// user's active state.
func requireNotDisabled(t *testing.T, dir *directoryUserServicer, step string) {
	t.Helper()
	if calls := dir.userStateCalls(); len(calls) != 0 {
		t.Fatalf("%s: SetUserActive called %d times (%+v), want none", step, len(calls), calls)
	}
}

// enroll adds req and returns its enrollment ID.
func enroll(t *testing.T, factors AuthFactorsClient, req *AuthFactorEnrollRequest) string {
	t.Helper()
	resp, err := factors.Add(context.Background(), req)
	if err != nil {
		t.Fatalf("enroll %s: %v", req.FactorName, err)
	}
	return resp.EnrollmentID
}

// requireResolves asserts key resolves to the lifecycle user's factor wantID.
func requireResolves(t *testing.T, store qrdbsdk.UserAuthFactorServicer, kind qrdbsdk.AuthFactorType, key []byte, wantID string) {
	t.Helper()
	got, err := store.LookupByCredentialKey(context.Background(), kind, key)
	if err != nil {
		t.Fatalf("lookup %s factor %s: %v", kind, wantID, err)
	}
	if got.UserID != lifecycleUserID || got.ID != wantID {
		t.Fatalf("%s key resolved to user %d factor %q, want user %d factor %q",
			kind, got.UserID, got.ID, lifecycleUserID, wantID)
	}
}

// requireRevoked asserts key no longer resolves because its factor is revoked.
func requireRevoked(t *testing.T, store qrdbsdk.UserAuthFactorServicer, kind qrdbsdk.AuthFactorType, key []byte) {
	t.Helper()
	_, err := store.LookupByCredentialKey(context.Background(), kind, key)
	if !errors.Is(err, qrdbsdk.ErrFactorRevoked) {
		t.Fatalf("lookup of a revoked %s key: want ErrFactorRevoked, got %v", kind, err)
	}
}

// One user enrolls five factors of three kinds from different vendors; each
// resolves to that same user and to its own enrollment.
func TestFactorLifecycle_EveryKindResolvesToTheSameUser(t *testing.T) {
	factors, store, _ := newLifecycleFixture(t)

	mtlsID := enroll(t, factors, certFactor("mtls", "laptop-cert"))
	yubicoID := enroll(t, factors, fido2Factor("yubico-cred", "59554249-434f-0001-0000-000000000001"))
	soloID := enroll(t, factors, fido2Factor("solokeys-cred", "534f4c4f-4b45-5900-0000-000000000002"))
	piv9aID := enroll(t, factors, certFactor("piv", "yubico-9a-cert"))
	piv9cID := enroll(t, factors, certFactor("piv", "nitrokey-9c-cert"))

	requireResolves(t, store, qrdbsdk.AuthFactorMTLS, certKey("laptop-cert"), mtlsID)
	requireResolves(t, store, qrdbsdk.AuthFactorFIDO2, []byte("yubico-cred"), yubicoID)
	requireResolves(t, store, qrdbsdk.AuthFactorFIDO2, []byte("solokeys-cred"), soloID)
	requireResolves(t, store, qrdbsdk.AuthFactorPIV, certKey("yubico-9a-cert"), piv9aID)
	requireResolves(t, store, qrdbsdk.AuthFactorPIV, certKey("nitrokey-9c-cert"), piv9cID)
}

// Rotating an mtls factor revokes the old certificate and enrolls the new one
// under a fresh ID for the same user.
func TestFactorLifecycle_RotateRejectsOldCertAcceptsNew(t *testing.T) {
	factors, store, _ := newLifecycleFixture(t)
	oldID := enroll(t, factors, certFactor("mtls", "old-cert"))

	rotated, err := factors.Rotate(context.Background(), oldID, certFactor("mtls", "new-cert"))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if rotated.EnrollmentID == "" || rotated.EnrollmentID == oldID {
		t.Fatalf("rotated enrollment ID = %q, want a fresh ID other than %q", rotated.EnrollmentID, oldID)
	}

	requireRevoked(t, store, qrdbsdk.AuthFactorMTLS, certKey("old-cert"))
	requireResolves(t, store, qrdbsdk.AuthFactorMTLS, certKey("new-cert"), rotated.EnrollmentID)
}

// Revoking one vendor's FIDO2 credential leaves the user's other FIDO2
// credential working.
func TestFactorLifecycle_SoftRevokeLeavesPeerFactorWorking(t *testing.T) {
	factors, store, dir := newLifecycleFixture(t)
	yubicoID := enroll(t, factors, fido2Factor("yubico-cred", "59554249-434f-0001-0000-000000000001"))
	soloID := enroll(t, factors, fido2Factor("solokeys-cred", "534f4c4f-4b45-5900-0000-000000000002"))

	if err := factors.Remove(context.Background(), yubicoID, false); err != nil {
		t.Fatalf("Remove with a peer still active: %v", err)
	}

	requireRevoked(t, store, qrdbsdk.AuthFactorFIDO2, []byte("yubico-cred"))
	requireResolves(t, store, qrdbsdk.AuthFactorFIDO2, []byte("solokeys-cred"), soloID)
	requireNotDisabled(t, dir, "soft Remove with a peer active")
}

// A soft remove of the user's last active factor is refused with a typed
// LastFactorError and leaves the factor working; a forced remove revokes it.
func TestFactorLifecycle_LastFactorGuard(t *testing.T) {
	ctx := context.Background()
	factors, store, dir := newLifecycleFixture(t)
	mtlsID := enroll(t, factors, certFactor("mtls", "laptop-cert"))
	fidoID := enroll(t, factors, fido2Factor("yubico-cred", "59554249-434f-0001-0000-000000000001"))
	pivID := enroll(t, factors, certFactor("piv", "yubico-9a-cert"))

	for _, id := range []string{mtlsID, fidoID} {
		if err := factors.Remove(ctx, id, false); err != nil {
			t.Fatalf("Remove %s with others active: %v", id, err)
		}
	}

	err := factors.Remove(ctx, pivID, false)
	var last *qrdbsdk.LastFactorError
	if !errors.Is(err, qrdbsdk.ErrLastFactor) || !errors.As(err, &last) {
		t.Fatalf("Remove of the last factor: want LastFactorError, got %T: %v", err, err)
	}
	if last.UserID != lifecycleUserID || last.FactorID != pivID {
		t.Fatalf("LastFactorError names user %d factor %q, want %d %q", last.UserID, last.FactorID, lifecycleUserID, pivID)
	}
	requireResolves(t, store, qrdbsdk.AuthFactorPIV, certKey("yubico-9a-cert"), pivID)
	requireNotDisabled(t, dir, "soft and refused Removes")

	if err := factors.Remove(ctx, pivID, true); err != nil {
		t.Fatalf("forced Remove of the last factor: %v", err)
	}
	requireRevoked(t, store, qrdbsdk.AuthFactorPIV, certKey("yubico-9a-cert"))
	want := []userStateCall{{userID: lifecycleUserID, active: false}}
	if calls := dir.userStateCalls(); !slices.Equal(calls, want) {
		t.Fatalf("forced Remove of the last factor: SetUserActive calls %+v, want %+v", calls, want)
	}
}
