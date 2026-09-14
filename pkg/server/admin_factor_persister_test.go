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
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// recordingFactorServicer captures Add calls so tests can assert exactly what
// would be persisted.
type recordingFactorServicer struct {
	qrdbsdk.UserAuthFactorServicer
	added  []*qrdbsdk.AuthFactor
	userID uint64
	addErr error
}

// Add records the factor and returns the injected error.
func (r *recordingFactorServicer) Add(_ context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	r.userID = userID
	if r.addErr != nil {
		return r.addErr
	}
	r.added = append(r.added, factor)
	return nil
}

// TestPersistAdminFactor_FIDO2UsesTheCredentialID is the load-bearing test for
// this file. qrdb indexes a fido2 factor by the credential ID verbatim, and a
// login assertion reduces to that same value. Persisting anything else (the
// COSE key, a hash) yields a credential that stores fine and can never
// authenticate — an operator holding a passkey that silently does not work.
func TestPersistAdminFactor_FIDO2UsesTheCredentialID(t *testing.T) {
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 42, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorFIDO2,
		&qrdbsdk.AuthFactorEnrollment{
			FactorName: "fido2",
			PublicKey:  []byte("cose-key-bytes-not-the-lookup-key"),
			Metadata: map[string]string{
				"credential_id": "Y3JlZC1pZA",
				"rp_id":         "console.example.com",
			},
		})
	require.NoError(t, err)

	require.Len(t, svc.added, 1)
	assert.Equal(t, uint64(42), svc.userID)
	assert.Equal(t, []byte("Y3JlZC1pZA"), svc.added[0].Credential,
		"fido2 must be indexed by the credential ID, not the COSE key")
	assert.Equal(t, qrdbsdk.AuthFactorType("fido2"), svc.added[0].Type)
	assert.Contains(t, svc.added[0].Label, "console.example.com")
	assert.Equal(t, adminFactorCreatedBy, svc.added[0].CreatedBy)
}

// TestPersistAdminFactor_FIDO2WithoutCredentialIDFails verifies the error path.
// Silently skipping would leave the operator believing a passkey was enrolled.
func TestPersistAdminFactor_FIDO2WithoutCredentialIDFails(t *testing.T) {
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 42, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorFIDO2,
		&qrdbsdk.AuthFactorEnrollment{FactorName: "fido2", PublicKey: []byte("cose")})
	require.Error(t, err)

	var typed *AdminFactorCredentialError
	require.ErrorAs(t, err, &typed)
	assert.Empty(t, svc.added, "nothing may be stored when the credential cannot be derived")
}

// TestPersistAdminFactor_PIVUsesThePublicKeyBlob verifies PIV persists the
// enrolled certificate/public-key blob, which qrdb hashes into the lookup key.
func TestPersistAdminFactor_PIVStoresTheKeysLoginActuallyPresents(t *testing.T) {
	cert, certPEM := mintAdminFactorCert(t)
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 7, nil)
	enrollmentMeta := map[string]string{"slot": "9a"}

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorPIV,
		&qrdbsdk.AuthFactorEnrollment{
			FactorName: "piv",
			PublicKey:  certPEM,
			Metadata:   enrollmentMeta,
		})
	require.NoError(t, err)
	require.Len(t, svc.added, 1)
	stored := svc.added[0]

	// The DER digest: the prover's alternate lookup key AND the value go-xkms's
	// verifier re-derives and compares against Credential.
	derDigest := sha256.Sum256(cert.Raw)
	assert.Equal(t, derDigest[:], stored.Credential,
		"the credential is not the DER digest, so login cannot resolve or verify this factor")

	// The specific regression: the PEM digest is what was stored before, and it
	// can never equal what any login presents.
	pemDigest := sha256.Sum256(certPEM)
	assert.NotEqual(t, pemDigest[:], stored.Credential,
		"the credential is a digest of the PEM encoding — the #380 defect")

	// The SPKI digest: the prover's PRIMARY key, and the one that survives a
	// certificate reissued over the same keypair.
	spkiDigest := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	assert.Equal(t, spkiDigest[:], stored.SPKICredential,
		"no renewal-stable key was stored, so a reissued certificate loses the login")

	// The certificate itself: without it the verifier fails with
	// MissingChallengeFieldError even once the lookup resolves.
	require.Contains(t, stored.Metadata, "cert_pem",
		"the verifier decodes the certificate from metadata; without it verification cannot run")
	block, _ := pem.Decode([]byte(stored.Metadata["cert_pem"]))
	require.NotNil(t, block, "cert_pem is not decodable PEM")
	assert.Equal(t, cert.Raw, block.Bytes, "cert_pem is a different certificate")

	// The ceremony's own metadata survives, and the label still names the slot.
	assert.Equal(t, "9a", stored.Metadata["slot"])
	assert.Contains(t, stored.Label, "9a")

	// The enrollment record is the ceremony's, not the persister's to edit.
	assert.NotContains(t, enrollmentMeta, "cert_pem",
		"the persister mutated the caller's enrollment metadata in place")
}

// TestPersistAdminFactor_PIVAcceptsBareDER covers key material handed over as
// DER rather than PEM. The field is typed as opaque bytes, so refusing DER
// would make the persister depend on an encoding chosen in another module.
func TestPersistAdminFactor_PIVAcceptsBareDER(t *testing.T) {
	cert, _ := mintAdminFactorCert(t)
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 7, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorPIV,
		&qrdbsdk.AuthFactorEnrollment{FactorName: "piv", PublicKey: cert.Raw})
	require.NoError(t, err)
	require.Len(t, svc.added, 1)

	derDigest := sha256.Sum256(cert.Raw)
	assert.Equal(t, derDigest[:], svc.added[0].Credential)
}

// TestPersistAdminFactor_PIVRejectsNonCertificateMaterial keeps the failure
// loud. Key material that is not a certificate yields no login key at all, and
// storing something derived from it anyway is how the original defect looked
// like success.
func TestPersistAdminFactor_PIVRejectsNonCertificateMaterial(t *testing.T) {
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 7, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorPIV,
		&qrdbsdk.AuthFactorEnrollment{
			FactorName: "piv",
			PublicKey:  []byte("-----BEGIN CERTIFICATE-----"),
		})

	require.Error(t, err, "unparseable key material was accepted and a factor persisted")
	var credErr *AdminFactorCredentialError
	require.ErrorAs(t, err, &credErr)
	assert.Empty(t, svc.added, "a factor was stored despite the failure")
}

// mintAdminFactorCert returns a real self-signed certificate and its PEM
// encoding — the shape go-xkms's PIV factor hands the persister.
func mintAdminFactorCert(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(20260904),
		Subject:      pkix.Name{CommonName: "admin"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// TestPersistAdminFactor_PIVWithoutKeyMaterialFails verifies the error path for
// the non-fido2 branch.
func TestPersistAdminFactor_PIVWithoutKeyMaterialFails(t *testing.T) {
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 7, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorPIV,
		&qrdbsdk.AuthFactorEnrollment{FactorName: "piv"})
	require.Error(t, err)
	assert.Empty(t, svc.added)
}

// TestPersistAdminFactor_MTLSIsSkipped verifies mTLS is NOT written here.
// first-boot's enrollFirstBootAdminCert already enrolls it and additionally
// stores the renewal-stable SPKI credential; a second write would either
// collide or register a weaker duplicate that breaks on cert rotation.
func TestPersistAdminFactor_MTLSIsSkipped(t *testing.T) {
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 42, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorMTLS,
		&qrdbsdk.AuthFactorEnrollment{
			FactorName: "mtls",
			PublicKey:  []byte("cert"),
		})
	require.NoError(t, err)
	assert.Empty(t, svc.added,
		"mtls is owned by enrollFirstBootAdminCert, which also stores the SPKI credential")
}

// TestPersistAdminFactor_NilEnrollmentFails verifies the nil guard.
func TestPersistAdminFactor_NilEnrollmentFails(t *testing.T) {
	p := newAdminFactorPersister(&recordingFactorServicer{}, 1, nil)
	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorFIDO2, nil)
	require.Error(t, err)
}

// TestPersistAdminFactor_StoreErrorSurfaces verifies a storage failure is
// reported rather than swallowed — an operator must never be told a hardware
// credential is live when it was not written.
func TestPersistAdminFactor_StoreErrorSurfaces(t *testing.T) {
	wantErr := errors.New("kv unavailable")
	svc := &recordingFactorServicer{addErr: wantErr}
	p := newAdminFactorPersister(svc, 42, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorFIDO2,
		&qrdbsdk.AuthFactorEnrollment{
			FactorName: "fido2",
			Metadata:   map[string]string{"credential_id": "abc"},
		})
	require.Error(t, err)
	require.ErrorIs(t, err, wantErr)

	var typed *AdminFactorPersistError
	require.ErrorAs(t, err, &typed)
}

// TestAdminFactorPlan_ParsesAndRejects covers the config-driven plan.
func TestAdminFactorPlan_ParsesAndRejects(t *testing.T) {
	t.Run("no selection means the mtls baseline", func(t *testing.T) {
		s := &Server{config: newTestConfigForFactors(nil)}
		plan, err := s.adminFactorPlan()
		require.NoError(t, err)
		assert.Nil(t, plan, "an empty selection must not synthesise a plan")
	})

	t.Run("valid selection parses", func(t *testing.T) {
		s := &Server{config: newTestConfigForFactors([]string{"fido2", "mtls"})}
		plan, err := s.adminFactorPlan()
		require.NoError(t, err)
		require.NotNil(t, plan)
		assert.True(t, plan.Includes(qrdbsdk.FactorFIDO2))
	})

	t.Run("unknown factor is rejected", func(t *testing.T) {
		s := &Server{config: newTestConfigForFactors([]string{"smartwatch"})}
		_, err := s.adminFactorPlan()
		require.Error(t, err)
		var typed *AdminFactorPlanError
		require.ErrorAs(t, err, &typed)
	})
}

// newTestConfigForFactors builds a minimal config carrying only the
// admin-factor selection under test.
func newTestConfigForFactors(factors []string) *config.Config {
	cfg := config.Default()
	cfg.Auth.AdminFactors = factors
	return cfg
}
