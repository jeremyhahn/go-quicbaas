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
	"errors"
	"testing"

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
func TestPersistAdminFactor_PIVUsesThePublicKeyBlob(t *testing.T) {
	svc := &recordingFactorServicer{}
	p := newAdminFactorPersister(svc, 7, nil)

	err := p.PersistAdminFactor(context.Background(), qrdbsdk.FactorPIV,
		&qrdbsdk.AuthFactorEnrollment{
			FactorName: "piv",
			PublicKey:  []byte("-----BEGIN CERTIFICATE-----"),
			Metadata:   map[string]string{"slot": "9a"},
		})
	require.NoError(t, err)

	require.Len(t, svc.added, 1)
	assert.Equal(t, []byte("-----BEGIN CERTIFICATE-----"), svc.added[0].Credential)
	assert.Contains(t, svc.added[0].Label, "9a")
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
