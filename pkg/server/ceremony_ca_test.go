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

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// ---------------------------------------------------------------------------
// ceremonyCAAdapter
// ---------------------------------------------------------------------------

func TestCeremonyCAAdapter_NilWithoutCA(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.xkmsCA)
	assert.Nil(t, srv.ceremonyCAAdapter(),
		"a CA-less server must report no adapter rather than one wrapping nil")
}

func TestCeremonyCAAdapter_SatisfiesEveryBootstrapSeam(t *testing.T) {
	srv := withTempServer(t)
	srv.config.IAM.SPIFFETrustDomain = "example.test"
	srv.xkmsCA = newFakeTrustedCA(t)

	adapter := srv.ceremonyCAAdapter()
	require.NotNil(t, adapter)

	// The whole point of item 2/1: ONE adapter covers the ceremony CA and both
	// admin-mTLS enrollment paths, so the stack issues from a single CA.
	var _ qrdbsdk.CeremonyCA = adapter
	var _ qrdbsdk.CertProvider = adapter
	var _ qrdbsdk.CertSigner = adapter
}

// ---------------------------------------------------------------------------
// lateBoundCeremonyCA
// ---------------------------------------------------------------------------

// TestLateBoundCeremonyCA_FailsClosedBeforeBinding asserts that every verb
// returns the typed unavailable error while the CA is unbound. The wrapper is
// handed to the embedded qrdb node before initializeCA runs, so this is the
// state it is genuinely in during node construction.
func TestLateBoundCeremonyCA_FailsClosedBeforeBinding(t *testing.T) {
	var ca lateBoundCeremonyCA
	ctx := context.Background()

	_, err := ca.SignCSR(ctx, []byte("csr"), &qrdbsdk.SignCSROptions{})
	requireCeremonyCAUnavailable(t, err, "SignCSR")

	_, err = ca.CACertificate(context.Background())
	requireCeremonyCAUnavailable(t, err, "CACertificate")

	_, err = ca.CABundle()
	requireCeremonyCAUnavailable(t, err, "CABundle")

	_, err = ca.IssueCertificate(ctx, &qrdbsdk.IssueCertRequest{CommonName: "x"})
	requireCeremonyCAUnavailable(t, err, "IssueCertificate")
}

// TestLateBoundCeremonyCA_ResolvesAfterBinding is the regression test for the
// missing WithCeremonyCA wiring: once bindCeremonyCA has run, the same wrapper
// the node holds issues real certificates instead of failing closed.
func TestLateBoundCeremonyCA_ResolvesAfterBinding(t *testing.T) {
	srv := withTempServer(t)
	srv.xkmsCA = newFakeTrustedCA(t)

	// Before binding the node's wrapper is inert.
	_, err := srv.ceremonyCA.CABundle()
	requireCeremonyCAUnavailable(t, err, "CABundle")

	srv.bindCeremonyCA()

	bundle, err := srv.ceremonyCA.CABundle()
	require.NoError(t, err)
	assert.NotEmpty(t, bundle, "the bound CA must publish its bundle")

	caCert, err := srv.ceremonyCA.CACertificate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, caCert)

	// The requester keeps its key: the CA certifies the public half.
	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	issued, err := srv.ceremonyCA.IssueCertificate(context.Background(), &qrdbsdk.IssueCertRequest{
		CommonName:   "node.example.test",
		ValidityDays: 30,
		PublicKey:    &nodeKey.PublicKey,
	})
	require.NoError(t, err)
	require.NotNil(t, issued)
	assert.NotEmpty(t, issued.CertPEM)
	require.NotNil(t, issued.Certificate)
	assert.True(t, nodeKey.PublicKey.Equal(issued.Certificate.PublicKey), "the CA must certify the requester's key")
}

// TestLateBoundCeremonyCA_BindWithoutCAStaysClosed covers the CA-less
// deployment: bindCeremonyCA must not attach anything, so the wrapper keeps
// failing closed instead of panicking on a nil inner CA.
func TestLateBoundCeremonyCA_BindWithoutCAStaysClosed(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.xkmsCA)

	srv.bindCeremonyCA()

	_, err := srv.ceremonyCA.CABundle()
	requireCeremonyCAUnavailable(t, err, "CABundle")
}

// TestXKMSCryptoOptions_AppendsCeremonyCA asserts the ceremony CA reaches the
// embedded qrdb node's option set. Without it the node's ceremony service runs
// with a nil CA and every CA-dependent verb fails closed.
func TestXKMSCryptoOptions_AppendsCeremonyCA(t *testing.T) {
	withoutCA, err := XKMSCryptoOptions(&XKMSCryptoConfig{})
	require.NoError(t, err)

	withCA, err := XKMSCryptoOptions(&XKMSCryptoConfig{CeremonyCA: &lateBoundCeremonyCA{}})
	require.NoError(t, err)

	assert.Len(t, withCA, len(withoutCA)+1,
		"a supplied ceremony CA must add exactly one server option")
}

// ---------------------------------------------------------------------------
// EnrollInitPINs
// ---------------------------------------------------------------------------

// TestEnrollInitPINs_SetsBothPINs is the regression test for --so-pin /
// --user-pin being required by the CLI and then discarded: the flags must land
// in the PIN backend the embedded node verifies against.
func TestEnrollInitPINs_SetsBothPINs(t *testing.T) {
	srv := withTempServer(t)
	backend, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.AutoDetectHashConfig())
	require.NoError(t, err)
	srv.pinBackend = backend

	require.False(t, backend.SOPINSet())
	require.NoError(t, srv.EnrollInitPINs("so-pin-12345678", "user-pin-12345678"))

	assert.True(t, backend.SOPINSet(), "the SO PIN must be enrolled")
	assert.True(t, backend.UserPINSet(), "the user PIN must be enrolled")
	assert.NoError(t, backend.VerifySOPIN("so-pin-12345678"))
	assert.NoError(t, backend.VerifyUserPIN("user-pin-12345678"))
}

// TestEnrollInitPINs_IdempotentAcrossReruns asserts a second init against an
// existing data dir does not silently rotate credentials already in use.
func TestEnrollInitPINs_IdempotentAcrossReruns(t *testing.T) {
	srv := withTempServer(t)
	backend, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.AutoDetectHashConfig())
	require.NoError(t, err)
	srv.pinBackend = backend

	require.NoError(t, srv.EnrollInitPINs("so-pin-12345678", "user-pin-12345678"))
	require.NoError(t, srv.EnrollInitPINs("different-so-pin", "different-user-pin"))

	assert.NoError(t, backend.VerifySOPIN("so-pin-12345678"),
		"a re-run must not rotate the SO PIN the operator already holds")
	assert.NoError(t, backend.VerifyUserPIN("user-pin-12345678"))
}

// TestEnrollInitPINs_NoBackendFailsClosed covers the error path: PINs supplied
// but no node running to enroll them into.
func TestEnrollInitPINs_NoBackendFailsClosed(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.pinBackend)

	err := srv.EnrollInitPINs("so-pin-12345678", "user-pin-12345678")
	require.Error(t, err)
	var typed *PINBackendUnavailableError
	assert.True(t, errors.As(err, &typed),
		"got %T (%v), want *PINBackendUnavailableError", err, err)
}

// TestEnrollInitPINs_NoPINsIsNoOp covers the "none" barrier strategy, which
// legitimately supplies no PINs at all.
func TestEnrollInitPINs_NoPINsIsNoOp(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.pinBackend)
	assert.NoError(t, srv.EnrollInitPINs("", ""),
		"no PINs supplied must not require a backend")
}

// requireCeremonyCAUnavailable asserts err is the typed fail-closed error for
// the named operation.
func requireCeremonyCAUnavailable(t *testing.T, err error, op string) {
	t.Helper()
	require.Error(t, err)
	var typed *CeremonyCAUnavailableError
	require.True(t, errors.As(err, &typed), "got %T (%v), want *CeremonyCAUnavailableError", err, err)
	assert.Equal(t, op, typed.Op)
}
