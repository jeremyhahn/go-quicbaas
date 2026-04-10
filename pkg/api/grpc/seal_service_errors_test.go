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

package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockSealManager implements interfaces.SealManager for testing error paths.
type mockSealManager struct {
	initializeFunc      func(ctx context.Context, shares, threshold int) (interface{}, error)
	unsealFunc          func(ctx context.Context, share []byte) (interface{}, error)
	sealFunc            func(ctx context.Context) error
	statusFunc          func(ctx context.Context) (interface{}, error)
	isSealedFunc        func() bool
	isInitializedFunc   func() bool
	rekeyFunc           func(ctx context.Context, shares, threshold int) (interface{}, error)
	genRecoveryKeysFunc func(ctx context.Context, threshold, totalKeys int) ([][]byte, error)
	delRecoveryKeysFunc func(ctx context.Context) error
	genRootTokenFunc    func(ctx context.Context, strategy string, shares [][]byte) (string, error)
	closeFunc           func() error
}

// Compile-time interface check.
var _ interfaces.SealManager = (*mockSealManager)(nil)

func (m *mockSealManager) Initialize(ctx context.Context, shares, threshold int) (interface{}, error) {
	if m.initializeFunc != nil {
		return m.initializeFunc(ctx, shares, threshold)
	}
	return &service.SealInitResponse{Shares: [][]byte{{1}, {2}, {3}}}, nil
}

func (m *mockSealManager) Unseal(ctx context.Context, share []byte) (interface{}, error) {
	if m.unsealFunc != nil {
		return m.unsealFunc(ctx, share)
	}
	return &service.UnsealResponse{Sealed: false}, nil
}

func (m *mockSealManager) Seal(ctx context.Context) error {
	if m.sealFunc != nil {
		return m.sealFunc(ctx)
	}
	return nil
}

func (m *mockSealManager) Status(ctx context.Context) (interface{}, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}
	return &service.SealStatusResponse{Sealed: true, Initialized: true}, nil
}

func (m *mockSealManager) IsSealed() bool {
	if m.isSealedFunc != nil {
		return m.isSealedFunc()
	}
	return true
}

func (m *mockSealManager) IsInitialized() bool {
	if m.isInitializedFunc != nil {
		return m.isInitializedFunc()
	}
	return false
}

func (m *mockSealManager) Rekey(ctx context.Context, shares, threshold int) (interface{}, error) {
	if m.rekeyFunc != nil {
		return m.rekeyFunc(ctx, shares, threshold)
	}
	return &service.RekeyResponse{}, nil
}

func (m *mockSealManager) GenerateRecoveryKeys(ctx context.Context, threshold, totalKeys int) ([][]byte, error) {
	if m.genRecoveryKeysFunc != nil {
		return m.genRecoveryKeysFunc(ctx, threshold, totalKeys)
	}
	return nil, nil
}

func (m *mockSealManager) DeleteRecoveryKeys(ctx context.Context) error {
	if m.delRecoveryKeysFunc != nil {
		return m.delRecoveryKeysFunc(ctx)
	}
	return nil
}

func (m *mockSealManager) GenerateRootToken(ctx context.Context, strategy string, shares [][]byte) (string, error) {
	if m.genRootTokenFunc != nil {
		return m.genRootTokenFunc(ctx, strategy, shares)
	}
	return "", nil
}

func (m *mockSealManager) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func newTestSealService(mgr interfaces.SealManager) *SealService {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	return NewSealService(mgr, log)
}

func TestSealService_Seal_Error(t *testing.T) {
	mgr := &mockSealManager{
		sealFunc: func(ctx context.Context) error {
			return errors.New("seal failed: not unsealed")
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Seal(context.Background(), &SealRequestDTO{Force: true})
	require.NoError(t, err)
	assert.False(t, resp.Sealed)
	assert.Contains(t, resp.Error, "seal failed")
}

func TestSealService_Seal_Success(t *testing.T) {
	mgr := &mockSealManager{
		sealFunc: func(ctx context.Context) error {
			return nil
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Seal(context.Background(), &SealRequestDTO{})
	require.NoError(t, err)
	assert.True(t, resp.Sealed)
	assert.Empty(t, resp.Error)
}

func TestSealService_Status_Error(t *testing.T) {
	mgr := &mockSealManager{
		statusFunc: func(ctx context.Context) (interface{}, error) {
			return nil, errors.New("status retrieval failed")
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Status(context.Background())
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "status retrieval failed")
}

func TestSealService_Status_Success(t *testing.T) {
	mgr := &mockSealManager{
		statusFunc: func(ctx context.Context) (interface{}, error) {
			return &service.SealStatusResponse{
				Sealed:         false,
				Initialized:    true,
				Mode:           "manual",
				Threshold:      2,
				TotalShares:    3,
				SharesProvided: 2,
				ClusterNodes:   1,
				Version:        1,
			}, nil
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Status(context.Background())
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.False(t, resp.Sealed)
	assert.True(t, resp.Initialized)
	assert.Equal(t, "manual", resp.Mode)
	assert.Equal(t, 2, resp.Threshold)
	assert.Equal(t, 3, resp.TotalShares)
	assert.Equal(t, 2, resp.SharesProvided)
	assert.Equal(t, 1, resp.ClusterNodes)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestSealService_Bootstrap_Error(t *testing.T) {
	mgr := &mockSealManager{
		initializeFunc: func(ctx context.Context, shares, threshold int) (interface{}, error) {
			return nil, errors.New("already initialized")
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Bootstrap(context.Background(), &BootstrapRequest{})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "already initialized")
}

func TestSealService_Bootstrap_Success(t *testing.T) {
	mgr := &mockSealManager{
		initializeFunc: func(ctx context.Context, shares, threshold int) (interface{}, error) {
			return &service.SealInitResponse{
				Shares:       [][]byte{{1}, {2}, {3}},
				RecoveryKeys: [][]byte{{4}, {5}},
				RootToken:    "root-token",
				Version:      1,
			}, nil
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Bootstrap(context.Background(), &BootstrapRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Len(t, resp.Shares, 3)
	assert.Len(t, resp.RecoveryKeys, 2)
	assert.Equal(t, "root-token", resp.RootToken)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestSealService_Unseal_Error(t *testing.T) {
	mgr := &mockSealManager{
		unsealFunc: func(ctx context.Context, share []byte) (interface{}, error) {
			return nil, errors.New("not initialized")
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Unseal(context.Background(), &UnsealRequestDTO{Share: []byte("share")})
	require.NoError(t, err)
	assert.True(t, resp.Sealed)
	assert.Contains(t, resp.Error, "not initialized")
}

func TestSealService_Unseal_Success(t *testing.T) {
	mgr := &mockSealManager{
		unsealFunc: func(ctx context.Context, share []byte) (interface{}, error) {
			return &service.UnsealResponse{
				Sealed:         false,
				SharesProvided: 2,
				Threshold:      2,
			}, nil
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Unseal(context.Background(), &UnsealRequestDTO{Share: []byte("share"), NodeID: 1})
	require.NoError(t, err)
	assert.False(t, resp.Sealed)
	assert.Equal(t, 2, resp.SharesProvided)
	assert.Equal(t, 2, resp.Threshold)
	assert.Empty(t, resp.Error)
}

func TestSealService_Rekey_Error(t *testing.T) {
	mgr := &mockSealManager{
		rekeyFunc: func(ctx context.Context, shares, threshold int) (interface{}, error) {
			return nil, errors.New("still sealed")
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Rekey(context.Background(), &RekeyRequestDTO{NewThreshold: 3, NewTotal: 5})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "still sealed")
}

func TestSealService_Rekey_Success(t *testing.T) {
	mgr := &mockSealManager{
		rekeyFunc: func(ctx context.Context, shares, threshold int) (interface{}, error) {
			return &service.RekeyResponse{
				Shares:  [][]byte{{1}, {2}, {3}, {4}, {5}},
				Version: 2,
			}, nil
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Rekey(context.Background(), &RekeyRequestDTO{NewThreshold: 3, NewTotal: 5})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Len(t, resp.Shares, 5)
	assert.Equal(t, uint64(2), resp.Version)
}

func TestSealService_Recover_Error(t *testing.T) {
	mgr := &mockSealManager{
		genRootTokenFunc: func(ctx context.Context, strategy string, shares [][]byte) (string, error) {
			return "", errors.New("insufficient keys")
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Recover(context.Background(), &RecoverRequestDTO{RecoveryKeys: [][]byte{{1}}})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "insufficient keys")
}

func TestSealService_Recover_Success(t *testing.T) {
	mgr := &mockSealManager{
		genRootTokenFunc: func(ctx context.Context, strategy string, shares [][]byte) (string, error) {
			return "root-token", nil
		},
	}
	svc := newTestSealService(mgr)

	resp, err := svc.Recover(context.Background(), &RecoverRequestDTO{RecoveryKeys: [][]byte{{1}, {2}}})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.Error)
}

func TestSealService_IsSealed(t *testing.T) {
	mgr := &mockSealManager{isSealedFunc: func() bool { return false }}
	svc := newTestSealService(mgr)
	assert.False(t, svc.IsSealed())

	mgr2 := &mockSealManager{isSealedFunc: func() bool { return true }}
	svc2 := newTestSealService(mgr2)
	assert.True(t, svc2.IsSealed())
}

func TestSealService_IsInitialized(t *testing.T) {
	mgr := &mockSealManager{isInitializedFunc: func() bool { return true }}
	svc := newTestSealService(mgr)
	assert.True(t, svc.IsInitialized())

	mgr2 := &mockSealManager{isInitializedFunc: func() bool { return false }}
	svc2 := newTestSealService(mgr2)
	assert.False(t, svc2.IsInitialized())
}

func TestSealService_Close_Mock(t *testing.T) {
	mgr := &mockSealManager{
		closeFunc: func() error {
			return nil
		},
	}
	svc := newTestSealService(mgr)

	err := svc.Close()
	assert.NoError(t, err)
}

func TestSealService_Close_Error(t *testing.T) {
	mgr := &mockSealManager{
		closeFunc: func() error {
			return errors.New("close failed")
		},
	}
	svc := newTestSealService(mgr)

	err := svc.Close()
	assert.Error(t, err)
}

func TestNewSealService_Mock(t *testing.T) {
	mgr := &mockSealManager{}
	svc := newTestSealService(mgr)
	assert.NotNil(t, svc)
}
