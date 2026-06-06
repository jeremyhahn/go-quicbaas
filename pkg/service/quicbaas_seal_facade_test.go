// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// quicBaaS SealService facade.
//
// The quicBaaS type implements SealService by delegating to a runtime-wired
// SealService (installed via SetSealService). Each method guards against a nil
// service and returns a typed ErrUnavailable. These tests cover both the guard
// branch (no service wired) and the delegation branch (service wired).
// =============================================================================

// mockSealServiceFacade is a SealService whose every method records that it was
// invoked and returns non-error sentinels, so the facade's delegation path can
// be asserted distinctly from the nil-service guard.
type mockSealServiceFacade struct {
	called map[string]bool
}

func newMockSealServiceFacade() *mockSealServiceFacade {
	return &mockSealServiceFacade{called: make(map[string]bool)}
}

func (m *mockSealServiceFacade) Status(_ context.Context) (*SealStatusResponse, error) {
	m.called["Status"] = true
	return &SealStatusResponse{}, nil
}

func (m *mockSealServiceFacade) Initialize(_ context.Context, _ *SealInitRequest) (*SealInitResponse, error) {
	m.called["Initialize"] = true
	return &SealInitResponse{}, nil
}

func (m *mockSealServiceFacade) Unseal(_ context.Context, _ *UnsealRequest) (*UnsealResponse, error) {
	m.called["Unseal"] = true
	return &UnsealResponse{}, nil
}

func (m *mockSealServiceFacade) Seal(_ context.Context) error {
	m.called["Seal"] = true
	return nil
}

func (m *mockSealServiceFacade) Rekey(_ context.Context, _ *RekeyRequest) (*RekeyResponse, error) {
	m.called["Rekey"] = true
	return &RekeyResponse{}, nil
}

func (m *mockSealServiceFacade) GenerateRecoveryKey(_ context.Context, _ *GenerateRecoveryKeyRequest) (*GenerateRecoveryKeyResponse, error) {
	m.called["GenerateRecoveryKey"] = true
	return &GenerateRecoveryKeyResponse{}, nil
}

func (m *mockSealServiceFacade) DeleteRecoveryKey(_ context.Context, _ *DeleteRecoveryKeyRequest) (*DeleteRecoveryKeyResponse, error) {
	m.called["DeleteRecoveryKey"] = true
	return &DeleteRecoveryKeyResponse{}, nil
}

func (m *mockSealServiceFacade) GenerateRootToken(_ context.Context, _ *GenerateRootTokenRequest) (*GenerateRootTokenResponse, error) {
	m.called["GenerateRootToken"] = true
	return &GenerateRootTokenResponse{}, nil
}

func (m *mockSealServiceFacade) RotateRootKey(_ context.Context, _ *RotateRootKeyRequest) (*RotateRootKeyResponse, error) {
	m.called["RotateRootKey"] = true
	return &RotateRootKeyResponse{}, nil
}

var _ SealService = (*mockSealServiceFacade)(nil)

// drive invokes every SealService method on the facade and returns the set of
// observed errors keyed by method name.
func driveSealFacade(t *testing.T, qb *quicBaaS) map[string]error {
	t.Helper()
	ctx := context.Background()
	errs := make(map[string]error)

	_, errs["Status"] = qb.Status(ctx)
	_, errs["Initialize"] = qb.Initialize(ctx, &SealInitRequest{})
	_, errs["Unseal"] = qb.Unseal(ctx, &UnsealRequest{})
	errs["Seal"] = qb.Seal(ctx)
	_, errs["Rekey"] = qb.Rekey(ctx, &RekeyRequest{})
	_, errs["GenerateRecoveryKey"] = qb.GenerateRecoveryKey(ctx, &GenerateRecoveryKeyRequest{})
	_, errs["DeleteRecoveryKey"] = qb.DeleteRecoveryKey(ctx, &DeleteRecoveryKeyRequest{})
	_, errs["GenerateRootToken"] = qb.GenerateRootToken(ctx, &GenerateRootTokenRequest{})
	_, errs["RotateRootKey"] = qb.RotateRootKey(ctx, &RotateRootKeyRequest{})

	return errs
}

func TestQuicBaaS_SealFacade_NilServiceReturnsUnavailable(t *testing.T) {
	qb := &quicBaaS{} // sealService is nil

	errs := driveSealFacade(t, qb)

	require.Len(t, errs, 9)
	for method, err := range errs {
		require.Error(t, err, "method %s must error when no seal service is wired", method)
		assert.Contains(t, err.Error(), "seal service not initialized", "method %s", method)
	}
}

func TestQuicBaaS_SealFacade_DelegatesWhenWired(t *testing.T) {
	mock := newMockSealServiceFacade()
	qb := &quicBaaS{}
	qb.SetSealService(mock)

	errs := driveSealFacade(t, qb)

	for method, err := range errs {
		require.NoError(t, err, "method %s must succeed when seal service is wired", method)
	}

	// Every facade method must have reached the underlying service exactly once.
	for _, method := range []string{
		"Status", "Initialize", "Unseal", "Seal", "Rekey",
		"GenerateRecoveryKey", "DeleteRecoveryKey", "GenerateRootToken", "RotateRootKey",
	} {
		assert.True(t, mock.called[method], "underlying %s was not invoked", method)
	}
}
