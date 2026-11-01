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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
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
	sealCalls      int
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

func (m *mockBarrierClient) BarrierSeal(_ context.Context) error {
	m.sealCalls++
	return m.sealErr
}

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

	assertMissing(svc.BarrierInitialize(ctx, []byte("secret")))
	assertMissing(svc.BarrierUnseal(ctx, []byte("secret")))
	assertMissing(svc.BarrierSeal(ctx, &service.BarrierSealRequest{}))

	_, err = svc.BarrierInitializeShamir(ctx, []byte("secret"), 2, 3)
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

// TestBarrierService_Delegation checks, for every method that maps a
// client response, that a success maps each response field and that a
// client error comes back verbatim with the zero result.
func TestBarrierService_Delegation(t *testing.T) {
	tests := []struct {
		name    string
		mock    *mockBarrierClient
		call    func(svc *service.BarrierService) (any, error)
		want    any
		wantErr error
	}{
		{
			name: "BarrierStatus success",
			mock: &mockBarrierClient{statusResp: &transport.BarrierStatusResponse{
				Sealed:         true,
				Strategy:       "shamir",
				HardwareBacked: true,
				InitializedAt:  "2026-05-29T10:00:00Z",
			}},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierStatus(context.Background())
			},
			want: &service.BarrierStatusInfo{
				Sealed:         true,
				Strategy:       "shamir",
				HardwareBacked: true,
				InitializedAt:  time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "BarrierStatus error",
			mock: &mockBarrierClient{statusErr: errBarrierBackend},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierStatus(context.Background())
			},
			want:    (*service.BarrierStatusInfo)(nil),
			wantErr: errBarrierBackend,
		},
		{
			name: "BarrierInitializeShamir success",
			mock: &mockBarrierClient{initShamirResp: &transport.BarrierInitializeShamirResponse{
				Threshold:   2,
				TotalShares: 3,
				ShareCount:  3,
			}},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierInitializeShamir(context.Background(), []byte("secret"), 2, 3)
			},
			want: &service.ShamirInitInfo{Threshold: 2, TotalShares: 3, ShareCount: 3},
		},
		{
			name: "BarrierInitializeShamir error",
			mock: &mockBarrierClient{initShamirErr: errBarrierBackend},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierInitializeShamir(context.Background(), []byte("secret"), 2, 3)
			},
			want:    (*service.ShamirInitInfo)(nil),
			wantErr: errBarrierBackend,
		},
		{
			name: "BarrierUnsealWithShare success",
			mock: &mockBarrierClient{unsealShareRsp: &transport.BarrierUnsealShareResponse{
				Required:  3,
				Submitted: 1,
				Complete:  false,
			}},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierUnsealWithShare(context.Background(), "share")
			},
			want: &service.UnsealShareProgress{Required: 3, Submitted: 1, Complete: false},
		},
		{
			name: "BarrierUnsealWithShare error",
			mock: &mockBarrierClient{unsealShareErr: errBarrierBackend},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierUnsealWithShare(context.Background(), "share")
			},
			want:    (*service.UnsealShareProgress)(nil),
			wantErr: errBarrierBackend,
		},
		{
			name: "BarrierRekey success",
			mock: &mockBarrierClient{rekeyResp: &transport.BarrierRekeyResponse{
				Threshold:   2,
				TotalShares: 2,
				ShareCount:  2,
			}},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierRekey(context.Background(), 2, 2)
			},
			want: &service.ShamirInitInfo{Threshold: 2, TotalShares: 2, ShareCount: 2},
		},
		{
			name: "BarrierRekey error",
			mock: &mockBarrierClient{rekeyErr: errBarrierBackend},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierRekey(context.Background(), 2, 2)
			},
			want:    (*service.ShamirInitInfo)(nil),
			wantErr: errBarrierBackend,
		},
		{
			name: "BarrierGenerateRecoveryKeys success",
			mock: &mockBarrierClient{recoveryResp: &transport.BarrierRecoveryKeysResponse{
				Threshold:  2,
				Total:      3,
				ShareCount: 3,
			}},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierGenerateRecoveryKeys(context.Background(), 2, 3)
			},
			want: &service.RecoveryKeysInfo{Threshold: 2, Total: 3, ShareCount: 3},
		},
		{
			name: "BarrierGenerateRecoveryKeys error",
			mock: &mockBarrierClient{recoveryErr: errBarrierBackend},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierGenerateRecoveryKeys(context.Background(), 2, 3)
			},
			want:    (*service.RecoveryKeysInfo)(nil),
			wantErr: errBarrierBackend,
		},
		{
			name: "BarrierHasRecoveryKeys success",
			mock: &mockBarrierClient{hasRecResp: &transport.BarrierHasRecoveryKeysResponse{HasKeys: true}},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierHasRecoveryKeys(context.Background())
			},
			want: true,
		},
		{
			name: "BarrierHasRecoveryKeys error",
			mock: &mockBarrierClient{hasRecErr: errBarrierBackend},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierHasRecoveryKeys(context.Background())
			},
			want:    false,
			wantErr: errBarrierBackend,
		},
		{
			name: "BarrierGenerateRootToken success",
			mock: &mockBarrierClient{rootTokenResp: &transport.BarrierRootTokenResponse{
				Token:     "root-token-hex",
				CreatedAt: "2026-05-29T11:30:00Z",
			}},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierGenerateRootToken(context.Background(), []string{"s1", "s2"})
			},
			want: &service.RootTokenInfo{
				Token:     "root-token-hex",
				CreatedAt: time.Date(2026, 5, 29, 11, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "BarrierGenerateRootToken error",
			mock: &mockBarrierClient{rootTokenErr: errBarrierBackend},
			call: func(svc *service.BarrierService) (any, error) {
				return svc.BarrierGenerateRootToken(context.Background(), []string{"s1"})
			},
			want:    (*service.RootTokenInfo)(nil),
			wantErr: errBarrierBackend,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.call(service.NewBarrierService(tc.mock))
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, got)
		})
	}
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

// =============================================================================
// BarrierInitialize / Unseal / Seal
// =============================================================================

func TestBarrierService_BarrierInitialize(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierInitialize(context.Background(), []byte("secret")))

	mc.initErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierInitialize(context.Background(), []byte("secret")), errBarrierBackend)
}

func TestBarrierService_BarrierUnseal(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierUnseal(context.Background(), []byte("secret")))

	mc.unsealErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierUnseal(context.Background(), []byte("secret")), errBarrierBackend)
}

func TestBarrierService_BarrierSeal(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	require.NoError(t, svc.BarrierSeal(context.Background(), nil))
	assert.Equal(t, 1, mc.sealCalls)

	mc.sealErr = errBarrierBackend
	assert.ErrorIs(t, svc.BarrierSeal(context.Background(), &service.BarrierSealRequest{}), errBarrierBackend)
}

// newSOPINBackend returns an in-memory qrdb software PIN backend, with
// soPIN set as its SO PIN when soPIN is not empty.
func newSOPINBackend(t *testing.T, soPIN string) qrdbsdk.PINBackend {
	t.Helper()
	backend, err := qrdbsdk.NewSoftwareBackend(nil, qrdbsdk.DefaultHashConfig())
	require.NoError(t, err)
	if soPIN != "" {
		require.NoError(t, backend.SetSOPIN("", soPIN))
	}
	return backend
}

func TestBarrierService_BarrierSeal_VerifiesSOPIN(t *testing.T) {
	const soPIN = "so-pin-1234"
	backend := newSOPINBackend(t, soPIN)

	tests := []struct {
		name    string
		req     *service.BarrierSealRequest
		wantErr error
	}{
		{name: "right PIN seals", req: &service.BarrierSealRequest{SOPIN: []byte(soPIN)}},
		{name: "nil request is refused", req: nil, wantErr: qrdbsdk.ErrSOPINRequired},
		{name: "empty PIN is refused", req: &service.BarrierSealRequest{}, wantErr: qrdbsdk.ErrSOPINRequired},
		{name: "wrong PIN is refused", req: &service.BarrierSealRequest{SOPIN: []byte("not-the-pin")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &mockBarrierClient{}
			svc := service.NewBarrierService(mc)
			svc.SetPINBackend(backend)

			err := svc.BarrierSeal(context.Background(), tt.req)
			if tt.req != nil && string(tt.req.SOPIN) == soPIN {
				require.NoError(t, err)
				assert.Equal(t, 1, mc.sealCalls)
				return
			}
			var qbErr *dberrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, dberrors.ErrForbidden, qbErr.Code)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			assert.Zero(t, mc.sealCalls, "a refused seal must leave the barrier untouched")
		})
	}
}

func TestBarrierService_BarrierSeal_NoSOPINSetIsUngated(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	svc.SetPINBackend(newSOPINBackend(t, ""))

	require.NoError(t, svc.BarrierSeal(context.Background(), &service.BarrierSealRequest{}))
	assert.Equal(t, 1, mc.sealCalls)
}

func TestBarrierService_SetPINBackend_NilClearsTheGate(t *testing.T) {
	mc := &mockBarrierClient{}
	svc := service.NewBarrierService(mc)
	svc.SetPINBackend(newSOPINBackend(t, "so-pin-1234"))
	require.Error(t, svc.BarrierSeal(context.Background(), nil))

	svc.SetPINBackend(nil)
	require.NoError(t, svc.BarrierSeal(context.Background(), nil))
	assert.Equal(t, 1, mc.sealCalls)
}

// =============================================================================
// Shamir initialize / unseal
// =============================================================================

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

// =============================================================================
// Recovery keys
// =============================================================================

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

// =============================================================================
// Root token
// =============================================================================

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

// Guard against the sentinel being accidentally reclassified as a wrapped
// chain error; the facade returns it verbatim.
