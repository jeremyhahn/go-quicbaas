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

package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock transport.BarrierClient for unit testing the BarrierService facade.
// =============================================================================

// errBarrierBackend is a typed sentinel error returned by the mock client to
// exercise the error-propagation branches of the BarrierService facade.
type barrierBackendError struct{}

func (e *barrierBackendError) Error() string { return "barrier backend failure" }

var errBarrierBackend = &barrierBackendError{}

// mockBarrierClient implements transport.BarrierClient. Each method returns the
// configured response/error, allowing tests to drive both happy and error
// paths through the facade without a live qrdb transport.
type mockBarrierClient struct {
	statusResp     *transport.BarrierStatusResponse
	statusErr      error
	initErr        error
	unsealErr      error
	sealErr        error
	initShamirResp *transport.BarrierInitializeShamirResponse
	initShamirErr  error
	unsealShareRsp *transport.BarrierUnsealShareResponse
	unsealShareErr error
	unsealSharesEr error
	rekeyResp      *transport.BarrierRekeyResponse
	rekeyErr       error
	recoveryResp   *transport.BarrierRecoveryKeysResponse
	recoveryErr    error
	recoverErr     error
	delRecoveryErr error
	hasRecResp     *transport.BarrierHasRecoveryKeysResponse
	hasRecErr      error
	rootTokenResp  *transport.BarrierRootTokenResponse
	rootTokenErr   error
}

func (m *mockBarrierClient) BarrierInitialize(_ context.Context, _ *transport.BarrierInitializeRequest) error {
	return m.initErr
}

func (m *mockBarrierClient) BarrierUnseal(_ context.Context, _ *transport.BarrierUnsealRequest) error {
	return m.unsealErr
}

func (m *mockBarrierClient) BarrierSeal(_ context.Context) error { return m.sealErr }

func (m *mockBarrierClient) BarrierStatus(_ context.Context) (*transport.BarrierStatusResponse, error) {
	return m.statusResp, m.statusErr
}

func (m *mockBarrierClient) BarrierEnableEncryption(_ context.Context, _ *transport.BarrierEnableEncryptionRequest) (*transport.BarrierEnableEncryptionResponse, error) {
	return &transport.BarrierEnableEncryptionResponse{Status: "migration_started"}, nil
}

func (m *mockBarrierClient) BarrierInitializeShamir(_ context.Context, _ *transport.BarrierInitializeShamirRequest) (*transport.BarrierInitializeShamirResponse, error) {
	return m.initShamirResp, m.initShamirErr
}

func (m *mockBarrierClient) BarrierUnsealWithShare(_ context.Context, _ *transport.BarrierUnsealShareRequest) (*transport.BarrierUnsealShareResponse, error) {
	return m.unsealShareRsp, m.unsealShareErr
}

func (m *mockBarrierClient) BarrierUnsealWithShares(_ context.Context, _ *transport.BarrierUnsealSharesRequest) error {
	return m.unsealSharesEr
}

func (m *mockBarrierClient) BarrierRekey(_ context.Context, _ *transport.BarrierRekeyRequest) (*transport.BarrierRekeyResponse, error) {
	return m.rekeyResp, m.rekeyErr
}

func (m *mockBarrierClient) BarrierGenerateRecoveryKeys(_ context.Context, _ *transport.BarrierGenerateRecoveryKeysRequest) (*transport.BarrierRecoveryKeysResponse, error) {
	return m.recoveryResp, m.recoveryErr
}

func (m *mockBarrierClient) BarrierRecoverWithKeys(_ context.Context, _ *transport.BarrierRecoverWithKeysRequest) error {
	return m.recoverErr
}

func (m *mockBarrierClient) BarrierDeleteRecoveryKeys(_ context.Context) error {
	return m.delRecoveryErr
}

func (m *mockBarrierClient) BarrierHasRecoveryKeys(_ context.Context) (*transport.BarrierHasRecoveryKeysResponse, error) {
	return m.hasRecResp, m.hasRecErr
}

func (m *mockBarrierClient) BarrierGenerateRootToken(_ context.Context, _ *transport.BarrierGenerateRootTokenRequest) (*transport.BarrierRootTokenResponse, error) {
	return m.rootTokenResp, m.rootTokenErr
}

// Officer enrollment and share-claim methods are unused by the facade; they
// satisfy the share-free transport.BarrierClient contract.

func (m *mockBarrierClient) OfficerRegister(_ context.Context, _ *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) OfficerList(_ context.Context) ([]*transport.OfficerStatus, error) {
	return nil, nil
}

func (m *mockBarrierClient) OfficerClaimStatus(_ context.Context) ([]*transport.OfficerStatus, error) {
	return nil, nil
}

func (m *mockBarrierClient) ShareClaimBegin(_ context.Context, _ *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) ShareClaimComplete(_ context.Context, _ *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) ShamirSealVote(_ context.Context, _ *transport.ShamirSealVoteRequest) (*transport.ShamirSealVoteResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) RecoveryOfficerRegister(_ context.Context, _ *transport.OfficerRegisterRequest) (*transport.OfficerRegisterResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) RecoveryShareClaimBegin(_ context.Context, _ *transport.ShareClaimBeginRequest) (*transport.ShareClaimBeginResponse, error) {
	return nil, nil
}

func (m *mockBarrierClient) RecoveryShareClaimComplete(_ context.Context, _ *transport.ShareClaimCompleteRequest) (*transport.ShareClaimCompleteResponse, error) {
	return nil, nil
}

// Compile-time check that the mock satisfies the qrdb BarrierClient contract.
var _ transport.BarrierClient = (*mockBarrierClient)(nil)

// =============================================================================
// Missing-client guard: every facade method must short-circuit with
// BarrierServiceMissingError when constructed with a nil client.
// =============================================================================

func TestBarrierService_NilClient_ReturnsMissingError(t *testing.T) {
	svc := service.NewBarrierService(nil)
	ctx := context.Background()

	var missing *service.BarrierServiceMissingError
	assertMissing := func(err error) {
		t.Helper()
		require.Error(t, err)
		assert.ErrorAs(t, err, &missing)
		assert.Equal(t, "barrier service not configured", err.Error())
	}

	_, err := svc.BarrierStatus(ctx)
	assertMissing(err)

	assertMissing(svc.BarrierInitialize(ctx, "secret"))
	assertMissing(svc.BarrierUnseal(ctx, "secret"))
	assertMissing(svc.BarrierSeal(ctx))

	_, err = svc.BarrierInitializeShamir(ctx, "secret", 2, 3)
	assertMissing(err)

	_, err = svc.BarrierUnsealWithShare(ctx, "share")
	assertMissing(err)

	assertMissing(svc.BarrierUnsealWithShares(ctx, []string{"a", "b"}))

	_, err = svc.BarrierRekey(ctx, 2, 3)
	assertMissing(err)

	_, err = svc.BarrierGenerateRecoveryKeys(ctx, 2, 3)
	assertMissing(err)

	assertMissing(svc.BarrierRecoverWithKeys(ctx, []string{"k1"}))
	assertMissing(svc.BarrierDeleteRecoveryKeys(ctx))

	_, err = svc.BarrierHasRecoveryKeys(ctx)
	assertMissing(err)

	_, err = svc.BarrierGenerateRootToken(ctx, []string{"s1"})
	assertMissing(err)
}

// =============================================================================
// BarrierStatus
// =============================================================================

func TestBarrierService_BarrierStatus_Success(t *testing.T) {
	initAt := "2026-05-29T10:00:00Z"
	mc := &mockBarrierClient{
		statusResp: &transport.BarrierStatusResponse{
			Sealed:         true,
			Strategy:       "shamir",
			HardwareBacked: true,
			InitializedAt:  initAt,
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.True(t, info.Sealed)
	assert.Equal(t, "shamir", info.Strategy)
	assert.True(t, info.HardwareBacked)

	want, parseErr := time.Parse(time.RFC3339, initAt)
	require.NoError(t, parseErr)
	assert.True(t, info.InitializedAt.Equal(want))
}

func TestBarrierService_BarrierStatus_InvalidTimestampIgnored(t *testing.T) {
	mc := &mockBarrierClient{
		statusResp: &transport.BarrierStatusResponse{
			Sealed:        false,
			Strategy:      "passphrase",
			InitializedAt: "not-a-timestamp",
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	// An unparseable timestamp must leave InitializedAt at its zero value
	// rather than propagating a parse error.
	assert.True(t, info.InitializedAt.IsZero())
}

func TestBarrierService_BarrierStatus_Error(t *testing.T) {
	mc := &mockBarrierClient{statusErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierStatus(context.Background())
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errBarrierBackend)
}

// =============================================================================
// BarrierInitialize / Unseal / Seal
// =============================================================================

func TestBarrierService_BarrierInitialize(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierInitialize(context.Background(), "secret"))

	mc.initErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierInitialize(context.Background(), "secret"), errBarrierBackend)
}

func TestBarrierService_BarrierUnseal(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierUnseal(context.Background(), "secret"))

	mc.unsealErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierUnseal(context.Background(), "secret"), errBarrierBackend)
}

func TestBarrierService_BarrierSeal(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierSeal(context.Background()))

	mc.sealErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierSeal(context.Background()), errBarrierBackend)
}

// =============================================================================
// Shamir initialize / unseal
// =============================================================================

func TestBarrierService_BarrierInitializeShamir_Success(t *testing.T) {
	mc := &mockBarrierClient{
		initShamirResp: &transport.BarrierInitializeShamirResponse{
			Threshold:   2,
			TotalShares: 3,
			ShareCount:  3,
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierInitializeShamir(context.Background(), "secret", 2, 3)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, 2, info.Threshold)
	assert.Equal(t, 3, info.TotalShares)
	assert.Equal(t, 3, info.ShareCount)
}

func TestBarrierService_BarrierInitializeShamir_Error(t *testing.T) {
	mc := &mockBarrierClient{initShamirErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierInitializeShamir(context.Background(), "secret", 2, 3)
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errBarrierBackend)
}

func TestBarrierService_BarrierUnsealWithShare_Success(t *testing.T) {
	mc := &mockBarrierClient{
		unsealShareRsp: &transport.BarrierUnsealShareResponse{
			Required:  3,
			Submitted: 1,
			Complete:  false,
		},
	}
	svc := service.NewBarrierService(mc)

	prog, err := svc.BarrierUnsealWithShare(context.Background(), "share")
	require.NoError(t, err)
	require.NotNil(t, prog)
	assert.Equal(t, 3, prog.Required)
	assert.Equal(t, 1, prog.Submitted)
	assert.False(t, prog.Complete)
}

func TestBarrierService_BarrierUnsealWithShare_Error(t *testing.T) {
	mc := &mockBarrierClient{unsealShareErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	prog, err := svc.BarrierUnsealWithShare(context.Background(), "share")
	require.Error(t, err)
	assert.Nil(t, prog)
	assert.ErrorIs(t, err, errBarrierBackend)
}

func TestBarrierService_BarrierUnsealWithShares(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierUnsealWithShares(context.Background(), []string{"a", "b"}))

	mc.unsealSharesEr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierUnsealWithShares(context.Background(), []string{"a", "b"}), errBarrierBackend)
}

// =============================================================================
// Shamir share management
// =============================================================================

// =============================================================================
// Rekey
// =============================================================================

func TestBarrierService_BarrierRekey_Success(t *testing.T) {
	mc := &mockBarrierClient{
		rekeyResp: &transport.BarrierRekeyResponse{
			Threshold:   2,
			TotalShares: 2,
			ShareCount:  2,
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierRekey(context.Background(), 2, 2)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, 2, info.Threshold)
	assert.Equal(t, 2, info.TotalShares)
	assert.Equal(t, 2, info.ShareCount)
}

func TestBarrierService_BarrierRekey_Error(t *testing.T) {
	mc := &mockBarrierClient{rekeyErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierRekey(context.Background(), 2, 2)
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errBarrierBackend)
}

// =============================================================================
// Recovery keys
// =============================================================================

func TestBarrierService_BarrierGenerateRecoveryKeys_Success(t *testing.T) {
	mc := &mockBarrierClient{
		recoveryResp: &transport.BarrierRecoveryKeysResponse{
			Threshold:  2,
			Total:      3,
			ShareCount: 3,
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierGenerateRecoveryKeys(context.Background(), 2, 3)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, 2, info.Threshold)
	assert.Equal(t, 3, info.Total)
	assert.Equal(t, 3, info.ShareCount)
}

func TestBarrierService_BarrierGenerateRecoveryKeys_Error(t *testing.T) {
	mc := &mockBarrierClient{recoveryErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierGenerateRecoveryKeys(context.Background(), 2, 3)
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errBarrierBackend)
}

func TestBarrierService_BarrierRecoverWithKeys(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierRecoverWithKeys(context.Background(), []string{"r1", "r2"}))

	mc.recoverErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierRecoverWithKeys(context.Background(), []string{"r1", "r2"}), errBarrierBackend)
}

func TestBarrierService_BarrierDeleteRecoveryKeys(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierDeleteRecoveryKeys(context.Background()))

	mc.delRecoveryErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierDeleteRecoveryKeys(context.Background()), errBarrierBackend)
}

func TestBarrierService_BarrierHasRecoveryKeys_Success(t *testing.T) {
	mc := &mockBarrierClient{
		hasRecResp: &transport.BarrierHasRecoveryKeysResponse{HasKeys: true},
	}
	svc := service.NewBarrierService(mc)

	has, err := svc.BarrierHasRecoveryKeys(context.Background())
	require.NoError(t, err)
	assert.True(t, has)
}

func TestBarrierService_BarrierHasRecoveryKeys_Error(t *testing.T) {
	mc := &mockBarrierClient{hasRecErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	has, err := svc.BarrierHasRecoveryKeys(context.Background())
	require.Error(t, err)
	assert.False(t, has)
	assert.ErrorIs(t, err, errBarrierBackend)
}

// =============================================================================
// Root token
// =============================================================================

func TestBarrierService_BarrierGenerateRootToken_Success(t *testing.T) {
	createdAt := "2026-05-29T11:30:00Z"
	mc := &mockBarrierClient{
		rootTokenResp: &transport.BarrierRootTokenResponse{
			Token:     "root-token-hex",
			CreatedAt: createdAt,
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierGenerateRootToken(context.Background(), []string{"s1", "s2"})
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "root-token-hex", info.Token)

	want, parseErr := time.Parse(time.RFC3339, createdAt)
	require.NoError(t, parseErr)
	assert.True(t, info.CreatedAt.Equal(want))
}

func TestBarrierService_BarrierGenerateRootToken_InvalidTimestampIgnored(t *testing.T) {
	mc := &mockBarrierClient{
		rootTokenResp: &transport.BarrierRootTokenResponse{
			Token:     "root-token-hex",
			CreatedAt: "garbage",
		},
	}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierGenerateRootToken(context.Background(), []string{"s1"})
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "root-token-hex", info.Token)
	assert.True(t, info.CreatedAt.IsZero())
}

func TestBarrierService_BarrierGenerateRootToken_Error(t *testing.T) {
	mc := &mockBarrierClient{rootTokenErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)

	info, err := svc.BarrierGenerateRootToken(context.Background(), []string{"s1"})
	require.Error(t, err)
	assert.Nil(t, info)
	assert.ErrorIs(t, err, errBarrierBackend)
}

// Guard against the sentinel being accidentally reclassified as a wrapped
// chain error; the facade returns it verbatim.
func TestBarrierService_ErrorIsVerbatim(t *testing.T) {
	mc := &mockBarrierClient{statusErr: errBarrierBackend}
	svc := service.NewBarrierService(mc)
	_, err := svc.BarrierStatus(context.Background())
	assert.True(t, errors.Is(err, errBarrierBackend))
}
