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
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Type() — the PKCS11 verifier must report the correct factor type.
// ---------------------------------------------------------------------------

func TestPKCS11FactorVerifier_Type(t *testing.T) {
	v := xkmssdk.NewPKCS11FactorVerifier()
	assert.Equal(t, qrdbtransport.FactorTypePKCS11, v.Type())
}

// ---------------------------------------------------------------------------
// buildLoginFactorVerifiers includes a ready PKCS11 verifier.
// ---------------------------------------------------------------------------

func TestBuildLoginFactorVerifiers_IncludesPKCS11(t *testing.T) {
	s := &Server{}
	s.buildLoginFactorVerifiers()

	var found bool
	for _, v := range s.loginFactorVerifiers {
		if _, ok := v.(*xkmssdk.PKCS11FactorVerifier); ok {
			found = true
			assert.Equal(t, qrdbtransport.FactorTypePKCS11, v.Type(),
				"PKCS11 verifier must advertise the pkcs11 factor type")
		}
	}
	assert.True(t, found, "PKCS11 verifier must be present in the built verifier set")
}

// ---------------------------------------------------------------------------
// Happy path: a valid ECDSA signature over the server nonce is accepted.
// ---------------------------------------------------------------------------

func TestPKCS11FactorVerifier_Verify_ECDSA_Happy(t *testing.T) {
	key, factor, _ := buildPKCS11ECDSAFactor(t)

	nonce := []byte("server-nonce-bytes")
	digest := sha256.Sum256(nonce)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	require.NoError(t, err)

	v := xkmssdk.NewPKCS11FactorVerifier()
	err = v.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{
		Type:      qrdbtransport.FactorTypePKCS11,
		Nonce:     nonce,
		Signature: sig,
	})
	require.NoError(t, err, "valid ECDSA signature over nonce must be accepted")
}

// ---------------------------------------------------------------------------
// Fail paths: the verifier must reject invalid or missing inputs.
// ---------------------------------------------------------------------------

func TestPKCS11FactorVerifier_Verify_BadSignature(t *testing.T) {
	_, factor, _ := buildPKCS11ECDSAFactor(t)

	v := xkmssdk.NewPKCS11FactorVerifier()
	err := v.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{
		Type:      qrdbtransport.FactorTypePKCS11,
		Nonce:     []byte("some-nonce"),
		Signature: []byte("this-is-not-a-valid-signature"),
	})
	require.Error(t, err, "bad signature must be rejected")

	// The error must be a typed error from the xkms auth_factors package —
	// either a SignatureVerifyError or a public-key error — not a raw string.
	var sigErr *xkmssdk.SignatureVerifyError
	var pubErr *xkmssdk.PublicKeyDecodeError
	isKnownType := errors.As(err, &sigErr) || errors.As(err, &pubErr)
	assert.True(t, isKnownType,
		"bad signature must produce a typed auth_factors error, got %T: %v", err, err)
}

func TestPKCS11FactorVerifier_Verify_RevokedFactor(t *testing.T) {
	_, factor, _ := buildPKCS11ECDSAFactor(t)

	revokedAt := time.Now().UTC()
	factor.RevokedAt = &revokedAt

	v := xkmssdk.NewPKCS11FactorVerifier()
	err := v.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{
		Type:  qrdbtransport.FactorTypePKCS11,
		Nonce: []byte("nonce"),
	})
	require.Error(t, err, "revoked factor must be rejected")

	var revoked *qrdbsdk.FactorRevokedError
	assert.True(t, errors.As(err, &revoked),
		"revoked factor must produce FactorRevokedError, got %T", err)
}

func TestPKCS11FactorVerifier_Verify_MissingNonce(t *testing.T) {
	_, factor, _ := buildPKCS11ECDSAFactor(t)

	v := xkmssdk.NewPKCS11FactorVerifier()
	err := v.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{
		Type:      qrdbtransport.FactorTypePKCS11,
		Signature: []byte("sig"),
	})
	require.Error(t, err, "missing nonce must be rejected")

	var missing *xkmssdk.MissingChallengeFieldError
	assert.True(t, errors.As(err, &missing),
		"missing nonce must produce MissingChallengeFieldError, got %T", err)
	assert.Equal(t, "nonce", missing.Field)
}

func TestPKCS11FactorVerifier_Verify_MissingSignature(t *testing.T) {
	_, factor, _ := buildPKCS11ECDSAFactor(t)

	v := xkmssdk.NewPKCS11FactorVerifier()
	err := v.Verify(context.Background(), factor, qrdbtransport.VerifyChallenge{
		Type:  qrdbtransport.FactorTypePKCS11,
		Nonce: []byte("nonce"),
	})
	require.Error(t, err, "missing signature must be rejected")

	var missing *xkmssdk.MissingChallengeFieldError
	assert.True(t, errors.As(err, &missing),
		"missing signature must produce MissingChallengeFieldError, got %T", err)
	assert.Equal(t, "signature", missing.Field)
}

// ---------------------------------------------------------------------------
// buildPKCS11ECDSAFactor is a test helper that generates a fresh ECDSA key,
// marshals it to PKIX DER, derives the credential (SHA-256 of DER), and
// returns a factor record wired for PKCS11 possession verification.
// ---------------------------------------------------------------------------

func buildPKCS11ECDSAFactor(t *testing.T) (*ecdsa.PrivateKey, *qrdbauth.AuthFactor, []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	pkixDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	credSum := sha256.Sum256(pkixDER)

	factor := &qrdbauth.AuthFactor{
		ID:         "pkcs11-test-factor",
		Type:       qrdbauth.AuthFactorPKCS11,
		Credential: credSum[:],
		Metadata: map[string]string{
			"public_key_der": base64.StdEncoding.EncodeToString(pkixDER),
		},
	}
	return key, factor, pkixDER
}
