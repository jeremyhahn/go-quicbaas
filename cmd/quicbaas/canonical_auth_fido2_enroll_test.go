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

// fakeWebAuthnClient is a test double for quicbaas.WebAuthnClient. The
// enrollment ceremony drives BeginRegistration (the single canonical begin
// surface) and FinishRegistration; the remaining methods satisfy the interface
// and fail loudly if a test path reaches them unexpectedly.
type fakeWebAuthnClient struct {
	beginResp  *quicbaas.FIDO2BeginRegistrationResponse
	beginErr   error
	beginUser  *quicbaas.WebAuthnUser
	finishResp *quicbaas.WebAuthnCredential
	finishErr  error
	finishReq  *quicbaas.FIDO2FinishRegistrationRequest
	finishUser *quicbaas.WebAuthnUser
	called     bool
}

func (w *fakeWebAuthnClient) BeginRegistration(_ context.Context, user *quicbaas.WebAuthnUser) (*quicbaas.FIDO2BeginRegistrationResponse, error) {
	w.beginUser = user
	return w.beginResp, w.beginErr
}

func (w *fakeWebAuthnClient) FinishRegistration(_ context.Context, user *quicbaas.WebAuthnUser, response *quicbaas.FIDO2FinishRegistrationRequest) (*quicbaas.WebAuthnCredential, error) {
	w.called = true
	w.finishUser = user
	w.finishReq = response
	return w.finishResp, w.finishErr
}

func (w *fakeWebAuthnClient) BeginLogin(context.Context, *quicbaas.WebAuthnUser) (*quicbaas.FIDO2BeginAuthenticationResponse, error) {
	return nil, stderrors.New("BeginLogin not expected")
}

func (w *fakeWebAuthnClient) FinishLogin(context.Context, *quicbaas.WebAuthnUser, *quicbaas.FIDO2FinishAuthenticationRequest) (*quicbaas.WebAuthnSession, error) {
	return nil, stderrors.New("FinishLogin not expected")
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
	webauthn := &fakeWebAuthnClient{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{
			SessionID:   "sess-123",
			OptionsJSON: `{"publicKey":{"challenge":"abc"}}`,
		},
		finishResp: &quicbaas.WebAuthnCredential{CredentialID: "AQIDBAUGBwgJCgsMDQ4PEA"},
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

	credID, err := enrollCanonicalFIDO2(context.Background(), f, webauthn, register)
	require.NoError(t, err)

	// The credential id is the base64url rawId from the attestation — the exact
	// encoding the login path's lookup key uses.
	assert.Equal(t, "AQIDBAUGBwgJCgsMDQ4PEA", credID)

	// BeginRegistration carried the username/display name.
	require.NotNil(t, webauthn.beginUser)
	assert.Equal(t, "alice", webauthn.beginUser.Username)
	assert.Equal(t, "yubikey-5", webauthn.beginUser.DisplayName)

	// The create seam received the server options + the device/origin flags.
	assert.Equal(t, `{"publicKey":{"challenge":"abc"}}`, string(seamOptions))
	assert.Equal(t, "dev-1", seamDevice)
	assert.Equal(t, "https://rp.example.com", seamOrigin)

	// FinishRegistration was completed under the issued session id with the
	// authenticator's attestation.
	require.True(t, webauthn.called)
	require.NotNil(t, webauthn.finishReq)
	assert.Equal(t, "sess-123", webauthn.finishReq.SessionID)
	assert.Equal(t, fido2AttestationDoc, webauthn.finishReq.AttestationJSON)
}

// TestBuildCanonicalEnrollRequest_FIDO2PropagatesCredentialID proves the enroll
// request carries MethodParams["credential_id"] set to the base64url credential
// id, matching what the login path uses as the lookup key. It drives the full
// build path with the SDK sub-clients and the create seam swapped out.
func TestBuildCanonicalEnrollRequest_FIDO2PropagatesCredentialID(t *testing.T) {
	var gotFlags *canonicalAuthFactorAddFlags
	withCanonicalFIDO2EnrollFunc(t, func(_ context.Context, f *canonicalAuthFactorAddFlags) (string, error) {
		gotFlags = f
		return "AQIDBAUGBwgJCgsMDQ4PEA", nil
	})

	req, err := buildCanonicalEnrollRequest(context.Background(), &canonicalAuthFactorAddFlags{
		factorType:  "fido2",
		label:       "yubikey-5",
		user:        "alice",
		fido2Device: "dev-1",
	})
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, "fido2", req.FactorName)
	assert.Equal(t, "alice", req.Subject)

	// The ceremony received the flag set.
	require.NotNil(t, gotFlags)
	assert.Equal(t, "yubikey-5", gotFlags.label)

	// The lookup key login expects: credential_id == base64url rawId.
	assert.Equal(t, "AQIDBAUGBwgJCgsMDQ4PEA", req.MethodParams["credential_id"])
	// device_id is preserved when --device is set.
	assert.Equal(t, "dev-1", req.MethodParams["device_id"])
}

// TestEnrollCanonicalFIDO2_MissingOptions fails closed with the typed error
// when the server's BeginRegistration response carries no creation options.
func TestEnrollCanonicalFIDO2_MissingOptions(t *testing.T) {
	webauthn := &fakeWebAuthnClient{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: ""},
	}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		t.Fatal("create seam must not run when options are missing")
		return nil, nil
	}

	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		webauthn, register)
	require.Error(t, err)
	var typed *MissingFIDO2RegistrationOptionsError
	require.ErrorAs(t, err, &typed)
	assert.False(t, webauthn.called, "FinishRegistration must not run on a missing-options failure")
}

// TestEnrollCanonicalFIDO2_BeginError surfaces a server BeginRegistration error.
func TestEnrollCanonicalFIDO2_BeginError(t *testing.T) {
	want := stderrors.New("transport down")
	webauthn := &fakeWebAuthnClient{beginErr: want}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		t.Fatal("create seam must not run when BeginRegistration fails")
		return nil, nil
	}
	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		webauthn, register)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// TestEnrollCanonicalFIDO2_DeviceError surfaces a CTAP2 create-step error and
// never reaches FinishRegistration.
func TestEnrollCanonicalFIDO2_DeviceError(t *testing.T) {
	webauthn := &fakeWebAuthnClient{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: `{"publicKey":{}}`},
	}
	want := &FIDO2OperationError{Op: "Register", Cause: stderrors.New("no key touched")}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		return nil, want
	}
	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		webauthn, register)
	require.Error(t, err)
	var typed *FIDO2OperationError
	require.ErrorAs(t, err, &typed)
	assert.False(t, webauthn.called, "FinishRegistration must not run after a device error")
}

// TestEnrollCanonicalFIDO2_FinishError surfaces a server FinishRegistration
// error.
func TestEnrollCanonicalFIDO2_FinishError(t *testing.T) {
	want := stderrors.New("session expired")
	webauthn := &fakeWebAuthnClient{
		beginResp: &quicbaas.FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: `{"publicKey":{}}`},
		finishErr: want,
	}
	register := func(context.Context, []byte, string, string) ([]byte, error) {
		return []byte(fido2AttestationDoc), nil
	}
	_, err := enrollCanonicalFIDO2(context.Background(),
		&canonicalAuthFactorAddFlags{factorType: "fido2", label: "l", user: "u"},
		webauthn, register)
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
