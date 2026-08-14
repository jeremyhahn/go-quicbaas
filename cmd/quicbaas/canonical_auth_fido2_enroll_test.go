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

// Package main: device-free coverage for the FIDO2 enrollment ceremony in
// canonical_auth.go. The CTAP2 device-drive step is exercised through the
// injectable canonicalFIDO2Registerer seam, so these tests prove the
// BeginRegistration -> create -> FinishRegistration -> credential_id flow and
// its error paths without a physical authenticator. The real device path
// remains the production default and is covered by integration tests.
package main

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// fakeFIDO2Client is a test double for quicbaas.FIDO2Client. The enrollment
// ceremony drives BeginRegistration (the single canonical begin surface) and
// FinishRegistration; the remaining methods satisfy the interface and fail
// loudly if a test path reaches them unexpectedly.
type fakeFIDO2Client struct {
	beginResp  *quicbaas.FIDO2BeginRegistrationResponse
	beginErr   error
	beginReq   *quicbaas.FIDO2BeginRegistrationRequest
	finishResp *quicbaas.FIDO2FinishRegistrationResponse
	finishErr  error
	finishReq  *quicbaas.FIDO2FinishRegistrationRequest
	called     bool
}

func (w *fakeFIDO2Client) BeginRegistration(_ context.Context, req *quicbaas.FIDO2BeginRegistrationRequest) (*quicbaas.FIDO2BeginRegistrationResponse, error) {
	w.beginReq = req
	return w.beginResp, w.beginErr
}

func (w *fakeFIDO2Client) FinishRegistration(_ context.Context, req *quicbaas.FIDO2FinishRegistrationRequest) (*quicbaas.FIDO2FinishRegistrationResponse, error) {
	w.called = true
	w.finishReq = req
	return w.finishResp, w.finishErr
}

func (w *fakeFIDO2Client) BeginAuthentication(context.Context, *quicbaas.FIDO2BeginAuthenticationRequest) (*quicbaas.FIDO2BeginAuthenticationResponse, error) {
	return nil, stderrors.New("BeginAuthentication not expected")
}

func (w *fakeFIDO2Client) FinishAuthentication(context.Context, *quicbaas.FIDO2FinishAuthenticationRequest) (*quicbaas.FIDO2FinishAuthenticationResponse, error) {
	return nil, stderrors.New("FinishAuthentication not expected")
}

// attestationDoc is a minimal navigator.credentials.create() PublicKeyCredential
// document carrying the base64url credential id under rawId — the exact field
// and encoding the login lookup key derives from.
const fido2AttestationDoc = `{"id":"AQIDBAUGBwgJCgsMDQ4PEA","rawId":"AQIDBAUGBwgJCgsMDQ4PEA","type":"public-key","response":{"attestationObject":"o2M","clientDataJSON":"eyJ0"}}`

// TestEnrollCanonicalFIDO2_Success proves the full ceremony: BeginRegistration
// issues options + session id, the create seam returns the attestation, the
// credential id is recovered from rawId, FinishRegistration is invoked with the
// issued session id, and the base64url credential id is returned for
// MethodParams["credential_id"].
func TestEnrollCanonicalFIDO2_Success(t *testing.T) {
	fido2 := &fakeFIDO2Client{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{
			SessionID:   "sess-123",
			OptionsJSON: `{"publicKey":{"challenge":"abc"}}`,
		},
		finishResp: &quicbaas.FIDO2FinishRegistrationResponse{Success: true, CredentialID: "AQIDBAUGBwgJCgsMDQ4PEA"},
	}

	var seamOptions []byte
	var seamDevice, seamOrigin string
	register := func(_ context.Context, optionsJSON []byte, device, origin string) ([]byte, error) {
		seamOptions = optionsJSON
		seamDevice = device
		seamOrigin = origin
		return []byte(fido2AttestationDoc), nil
	}

	f := &canonicalAuthFactorAddFlags{
		factorType:  "fido2",
		label:       "yubikey-5",
		user:        "alice",
		fido2Device: "dev-1",
		fido2Origin: "https://rp.example.com",
	}

	credID, err := enrollCanonicalFIDO2(context.Background(), f, fido2, register)
	require.NoError(t, err)

	// The credential id is the base64url rawId from the attestation — the exact
	// encoding the login path's lookup key uses.
	assert.Equal(t, "AQIDBAUGBwgJCgsMDQ4PEA", credID)

	// BeginRegistration carried the username/display name.
	require.NotNil(t, fido2.beginReq)
	assert.Equal(t, "alice", fido2.beginReq.Username)
	assert.Equal(t, "yubikey-5", fido2.beginReq.DisplayName)

	// The create seam received the server options + the device/origin flags.
	assert.Equal(t, `{"publicKey":{"challenge":"abc"}}`, string(seamOptions))
	assert.Equal(t, "dev-1", seamDevice)
	assert.Equal(t, "https://rp.example.com", seamOrigin)

	// FinishRegistration was completed under the issued session id with the
	// authenticator's attestation.
	require.True(t, fido2.called)
	require.NotNil(t, fido2.finishReq)
	assert.Equal(t, "sess-123", fido2.finishReq.SessionID)
	assert.Equal(t, fido2AttestationDoc, fido2.finishReq.AttestationJSON)
}

// TestBuildCanonicalEnrollRequest_RejectsFIDO2 pins the post-write-through
// contract: FIDO2 has NO client-issued enroll request. Its qrdb factor is
// written server-side by the WebAuthn registration ceremony
// (pkg/iam.webAuthnFactorWriteThrough), so a client-built enroll request would
// double-enroll the same credential id and be rejected as a duplicate. The
// builder fails closed instead.
func TestBuildCanonicalEnrollRequest_RejectsFIDO2(t *testing.T) {
	withCanonicalFIDO2EnrollFunc(t, func(context.Context, *canonicalAuthFactorAddFlags) (string, error) {
		t.Fatal("the enroll-request builder must not run the FIDO2 ceremony")
		return "", nil
	})

	req, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType:  "fido2",
		label:       "yubikey-5",
		user:        "alice",
		fido2Device: "dev-1",
	})
	require.Error(t, err)
	assert.Nil(t, req)
	var typed *InvalidFactorTypeError
	require.ErrorAs(t, err, &typed)
}

// TestRunCanonicalAuthFactorAdd_FIDO2DoesNotDoubleEnroll is the regression
// guard for the split-store fix: "auth factor add --type fido2" must run the
// registration ceremony and STOP. The server enrolls the qrdb factor as it
// persists the credential, so a client-side Factors().Add would fail with a
// duplicate-credential error.
func TestRunCanonicalAuthFactorAdd_FIDO2DoesNotDoubleEnroll(t *testing.T) {
	factors := &fakeAuthFactorsClient{
		addFunc: func(context.Context, *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
			t.Fatal("fido2 enrollment must not call Factors().Add — the ceremony already enrolled the factor")
			return nil, nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)

	var gotFlags *canonicalAuthFactorAddFlags
	withCanonicalFIDO2EnrollFunc(t, func(_ context.Context, f *canonicalAuthFactorAddFlags) (string, error) {
		gotFlags = f
		return "AQIDBAUGBwgJCgsMDQ4PEA", nil
	})

	require.NoError(t, runCanonicalAuthFactorAdd(context.Background(), &canonicalAuthFactorAddFlags{
		factorType:  "fido2",
		label:       "yubikey-5",
		user:        "alice",
		fido2Device: "dev-1",
	}))

	// The ceremony received the operator's flag set — --label travels as the
	// WebAuthn display name and becomes the enrolled factor's label.
	require.NotNil(t, gotFlags)
	assert.Equal(t, "yubikey-5", gotFlags.label)
	assert.Equal(t, "alice", gotFlags.user)
	assert.Equal(t, "dev-1", gotFlags.fido2Device)
}

// TestRunCanonicalAuthFactorAdd_FIDO2CeremonyFailure proves a failed
// registration ceremony surfaces to the operator and never falls through to a
// factor write.
func TestRunCanonicalAuthFactorAdd_FIDO2CeremonyFailure(t *testing.T) {
	factors := &fakeAuthFactorsClient{
		addFunc: func(context.Context, *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
			t.Fatal("a failed ceremony must not enroll anything")
			return nil, nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)

	sentinel := stderrors.New("authenticator refused the ceremony")
	withCanonicalFIDO2EnrollFunc(t, func(context.Context, *canonicalAuthFactorAddFlags) (string, error) {
		return "", sentinel
	})

	err := runCanonicalAuthFactorAdd(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "fido2", label: "yubikey-5", user: "alice",
	})
	require.ErrorIs(t, err, sentinel)
}

// TestRunCanonicalAuthFactorAdd_FIDO2MissingLabel proves the shared flag
// validation still runs for the ceremony path — the ceremony must not start
// before the flag set is known good.
func TestRunCanonicalAuthFactorAdd_FIDO2MissingLabel(t *testing.T) {
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(&fakeAuthFactorsClient{}), nil)
	withCanonicalFIDO2EnrollFunc(t, func(context.Context, *canonicalAuthFactorAddFlags) (string, error) {
		t.Fatal("the ceremony must not start with an invalid flag set")
		return "", nil
	})

	err := runCanonicalAuthFactorAdd(context.Background(), &canonicalAuthFactorAddFlags{
		factorType: "fido2", user: "alice",
	})
	require.Error(t, err)
	var typed *MissingLabelError
	require.ErrorAs(t, err, &typed)
}

// TestRunCanonicalAuthFactorRotate_FIDO2RevokesOldFactor proves fido2 rotation
// is enroll-then-revoke: the ceremony enrolls the replacement server-side and
// the CLI revokes the superseded factor. The atomic Rotate RPC is NOT used —
// it would re-enroll the credential the ceremony already wrote.
func TestRunCanonicalAuthFactorRotate_FIDO2RevokesOldFactor(t *testing.T) {
	var (
		removedID    string
		removedForce bool
	)
	factors := &fakeAuthFactorsClient{
		rotateFunc: func(context.Context, string, *quicbaas.AuthFactorEnrollRequest) (*quicbaas.AuthFactorEnrollResponse, error) {
			t.Fatal("fido2 rotation must not call Factors().Rotate")
			return nil, nil
		},
		removeFunc: func(_ context.Context, factorID string, force bool) error {
			removedID = factorID
			removedForce = force
			return nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)
	withCanonicalFIDO2EnrollFunc(t, func(context.Context, *canonicalAuthFactorAddFlags) (string, error) {
		return "AQIDBAUGBwgJCgsMDQ4PEA", nil
	})

	require.NoError(t, runCanonicalAuthFactorRotate(context.Background(), "old-id",
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "yubikey-5", user: "alice"}))

	assert.Equal(t, "old-id", removedID)
	assert.False(t, removedForce,
		"the replacement is already enrolled, so the revoke never needs --force")
}

// TestRunCanonicalAuthFactorRotate_FIDO2CeremonyFailureKeepsOldFactor proves
// the old factor survives a failed replacement ceremony: enroll-then-revoke
// ordering must never leave the user without a usable factor.
func TestRunCanonicalAuthFactorRotate_FIDO2CeremonyFailureKeepsOldFactor(t *testing.T) {
	factors := &fakeAuthFactorsClient{
		removeFunc: func(context.Context, string, bool) error {
			t.Fatal("the old factor must not be revoked when the replacement ceremony fails")
			return nil
		},
	}
	withFakeSDKClientForCanonicalAuth(t, newFakeFactorsClient(factors), nil)

	sentinel := stderrors.New("authenticator refused the ceremony")
	withCanonicalFIDO2EnrollFunc(t, func(context.Context, *canonicalAuthFactorAddFlags) (string, error) {
		return "", sentinel
	})

	err := runCanonicalAuthFactorRotate(context.Background(), "old-id",
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "yubikey-5", user: "alice"})
	require.ErrorIs(t, err, sentinel)
}

// TestEnrollCanonicalFIDO2_MissingOptions fails closed with the typed error
// when the server's BeginRegistration response carries no creation options.
func TestEnrollCanonicalFIDO2_MissingOptions(t *testing.T) {
	fido2 := &fakeFIDO2Client{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: ""},
	}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		t.Fatal("create seam must not run when options are missing")
		return nil, nil
	}

	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		fido2, register)
	require.Error(t, err)
	var typed *MissingFIDO2RegistrationOptionsError
	require.ErrorAs(t, err, &typed)
	assert.False(t, fido2.called, "FinishRegistration must not run on a missing-options failure")
}

// TestEnrollCanonicalFIDO2_BeginError surfaces a server BeginRegistration error.
func TestEnrollCanonicalFIDO2_BeginError(t *testing.T) {
	want := stderrors.New("transport down")
	fido2 := &fakeFIDO2Client{beginErr: want}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		t.Fatal("create seam must not run when BeginRegistration fails")
		return nil, nil
	}
	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		fido2, register)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestEnrollCanonicalFIDO2_DeviceError surfaces a CTAP2 create-step error and
// never reaches FinishRegistration.
func TestEnrollCanonicalFIDO2_DeviceError(t *testing.T) {
	fido2 := &fakeFIDO2Client{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: `{"publicKey":{}}`},
	}
	want := &FIDO2OperationError{Op: "Register", Cause: stderrors.New("no key touched")}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		return nil, want
	}
	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		fido2, register)
	require.Error(t, err)
	var typed *FIDO2OperationError
	require.ErrorAs(t, err, &typed)
	assert.False(t, fido2.called, "FinishRegistration must not run after a device error")
}

// TestEnrollCanonicalFIDO2_FinishError surfaces a server FinishRegistration
// error.
func TestEnrollCanonicalFIDO2_FinishError(t *testing.T) {
	want := stderrors.New("session expired")
	fido2 := &fakeFIDO2Client{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: `{"publicKey":{}}`},
		finishErr: want,
	}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		return []byte(fido2AttestationDoc), nil
	}
	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		fido2, register)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestCanonicalFIDO2CredentialIDFromAttestation_RawID recovers the base64url
// credential id from rawId.
func TestCanonicalFIDO2CredentialIDFromAttestation_RawID(t *testing.T) {
	id, err := canonicalFIDO2CredentialIDFromAttestation([]byte(fido2AttestationDoc))
	require.NoError(t, err)
	assert.Equal(t, "AQIDBAUGBwgJCgsMDQ4PEA", id)
}

// TestCanonicalFIDO2CredentialIDFromAttestation_FallbackID falls back to "id"
// when "rawId" is absent.
func TestCanonicalFIDO2CredentialIDFromAttestation_FallbackID(t *testing.T) {
	id, err := canonicalFIDO2CredentialIDFromAttestation([]byte(`{"id":"ZmFsbGJhY2s"}`))
	require.NoError(t, err)
	assert.Equal(t, "ZmFsbGJhY2s", id)
}

// TestCanonicalFIDO2CredentialIDFromAttestation_Invalid returns the typed parse
// error for malformed JSON and for an attestation with no credential id.
func TestCanonicalFIDO2CredentialIDFromAttestation_Invalid(t *testing.T) {
	_, err := canonicalFIDO2CredentialIDFromAttestation([]byte(`{not json`))
	require.Error(t, err)
	var typed *FIDO2RegistrationParseError
	require.ErrorAs(t, err, &typed)

	_, err = canonicalFIDO2CredentialIDFromAttestation([]byte(`{"type":"public-key"}`))
	require.Error(t, err)
	require.ErrorAs(t, err, &typed)
}

// TestFIDO2CredentialIDMatchesLoginLookupKey is the parity guard: the credential
// id the enroll path stores under MethodParams["credential_id"] is byte-for-byte
// the lookup key the login path derives from the same rawId. If these diverged a
// later fido2 login would never resolve the enrolled factor.
func TestFIDO2CredentialIDMatchesLoginLookupKey(t *testing.T) {
	enrollID, err := canonicalFIDO2CredentialIDFromAttestation([]byte(fido2AttestationDoc))
	require.NoError(t, err)

	// The login path derives its lookup key from the assertion rawId via
	// canonicalFIDO2CredentialID; both documents carry the same rawId encoding.
	loginID, err := canonicalFIDO2CredentialID([]byte(fido2AttestationDoc))
	require.NoError(t, err)

	assert.Equal(t, loginID, enrollID,
		"enroll credential_id and login lookup key must use identical encoding")
}

// ---------------------------------------------------------------------------
// test seams
// ---------------------------------------------------------------------------

// withCanonicalFIDO2RegisterFunc swaps the device-create seam for the duration
// of a test and restores it afterwards.
func withCanonicalFIDO2RegisterFunc(t *testing.T, fn canonicalFIDO2Registerer) {
	t.Helper()
	prev := canonicalFIDO2RegisterFunc
	canonicalFIDO2RegisterFunc = fn
	t.Cleanup(func() { canonicalFIDO2RegisterFunc = prev })
}

// withCanonicalFIDO2EnrollFunc swaps the FIDO2 enroll ceremony seam used by
// buildCanonicalEnrollRequest for the duration of a test. This isolates the
// credential-id propagation logic from the live SDK Security() surface.
func withCanonicalFIDO2EnrollFunc(t *testing.T, fn func(context.Context, *canonicalAuthFactorAddFlags) (string, error)) {
	t.Helper()
	prev := canonicalFIDO2EnrollFunc
	canonicalFIDO2EnrollFunc = fn
	t.Cleanup(func() { canonicalFIDO2EnrollFunc = prev })
}
