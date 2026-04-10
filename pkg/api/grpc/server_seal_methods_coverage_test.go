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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/seal"
)

// =============================================================================
// defaultSealManager Tests
// =============================================================================

// TestNewDefaultSealManagerWithConfig tests the exported NewDefaultSealManager
// constructor that accepts a seal.Config.
func TestNewDefaultSealManagerWithConfig(t *testing.T) {
	t.Run("uses_config_threshold_and_shares", func(t *testing.T) {
		cfg := &seal.Config{Threshold: 3, TotalShares: 5}
		mgr := NewDefaultSealManager(cfg)
		require.NotNil(t, mgr)
		dm, ok := mgr.(*defaultSealManager)
		require.True(t, ok)
		assert.Equal(t, 3, dm.threshold)
		assert.Equal(t, 5, dm.totalShares)
		assert.True(t, dm.sealed)
		assert.False(t, dm.initialized)
	})

	t.Run("defaults_threshold_when_zero", func(t *testing.T) {
		mgr := NewDefaultSealManager(&seal.Config{Threshold: 0, TotalShares: 5})
		dm := mgr.(*defaultSealManager)
		assert.Equal(t, 2, dm.threshold)
	})

	t.Run("defaults_total_shares_when_zero", func(t *testing.T) {
		mgr := NewDefaultSealManager(&seal.Config{Threshold: 2, TotalShares: 0})
		dm := mgr.(*defaultSealManager)
		assert.Equal(t, 3, dm.totalShares)
	})

	t.Run("defaults_both_when_negative", func(t *testing.T) {
		mgr := NewDefaultSealManager(&seal.Config{Threshold: -1, TotalShares: -1})
		dm := mgr.(*defaultSealManager)
		assert.Equal(t, 2, dm.threshold)
		assert.Equal(t, 3, dm.totalShares)
	})
}

func TestDefaultSealManagerIsSealed(t *testing.T) {
	t.Run("returns_true_initially", func(t *testing.T) {
		assert.True(t, newDefaultSealManager().IsSealed())
	})

	t.Run("returns_false_after_unseal", func(t *testing.T) {
		mgr := newDefaultSealManager()
		_, err := mgr.Initialize(context.Background(), 3, 2)
		require.NoError(t, err)
		_, err = mgr.Unseal(context.Background(), []byte("s1"))
		require.NoError(t, err)
		assert.True(t, mgr.IsSealed())
		_, err = mgr.Unseal(context.Background(), []byte("s2"))
		require.NoError(t, err)
		assert.False(t, mgr.IsSealed())
	})
}

func TestDefaultSealManagerIsInitialized(t *testing.T) {
	t.Run("returns_false_initially", func(t *testing.T) {
		assert.False(t, newDefaultSealManager().IsInitialized())
	})

	t.Run("returns_true_after_initialization", func(t *testing.T) {
		mgr := newDefaultSealManager()
		_, err := mgr.Initialize(context.Background(), 3, 2)
		require.NoError(t, err)
		assert.True(t, mgr.IsInitialized())
	})
}

func TestAlreadyInitializedError(t *testing.T) {
	t.Run("error_message", func(t *testing.T) {
		assert.Equal(t, "seal manager already initialized", (&AlreadyInitializedError{}).Error())
	})

	t.Run("returned_on_double_initialize", func(t *testing.T) {
		mgr := newDefaultSealManager()
		_, err := mgr.Initialize(context.Background(), 3, 2)
		require.NoError(t, err)
		_, err = mgr.Initialize(context.Background(), 3, 2)
		var alreadyInit *AlreadyInitializedError
		assert.ErrorAs(t, err, &alreadyInit)
	})
}

func TestInvalidThresholdError(t *testing.T) {
	t.Run("error_message", func(t *testing.T) {
		err := &InvalidThresholdError{Threshold: 5, Total: 3}
		assert.Contains(t, err.Error(), "invalid threshold")
	})

	t.Run("returned_on_bad_threshold_initialize", func(t *testing.T) {
		mgr := newDefaultSealManager()
		_, err := mgr.Initialize(context.Background(), 2, 5)
		var invalidThreshold *InvalidThresholdError
		assert.ErrorAs(t, err, &invalidThreshold)
	})

	t.Run("returned_on_bad_threshold_rekey", func(t *testing.T) {
		mgr := newDefaultSealManager()
		_, _ = mgr.Initialize(context.Background(), 3, 2)
		_, _ = mgr.Unseal(context.Background(), []byte("s1"))
		_, _ = mgr.Unseal(context.Background(), []byte("s2"))
		_, err := mgr.Rekey(context.Background(), 2, 5)
		var invalidThreshold *InvalidThresholdError
		assert.ErrorAs(t, err, &invalidThreshold)
	})
}

func TestDefaultSealManagerGenerateRecoveryKeys(t *testing.T) {
	t.Run("generates_requested_number_of_keys", func(t *testing.T) {
		keys, err := newDefaultSealManager().GenerateRecoveryKeys(context.Background(), 2, 5)
		require.NoError(t, err)
		require.Len(t, keys, 5)
		for _, key := range keys {
			assert.Len(t, key, 32)
		}
	})
}

func TestDefaultSealManagerDeleteRecoveryKeys(t *testing.T) {
	assert.NoError(t, newDefaultSealManager().DeleteRecoveryKeys(context.Background()))
}

func TestDefaultSealManagerGenerateRootToken(t *testing.T) {
	token, err := newDefaultSealManager().GenerateRootToken(context.Background(), "otp", nil)
	require.NoError(t, err)
	assert.Equal(t, "generated-root-token", token)
}

func TestDefaultSealManagerClose(t *testing.T) {
	assert.NoError(t, newDefaultSealManager().Close())
}

func TestDefaultSealManagerUnsealNotInitialized(t *testing.T) {
	_, err := newDefaultSealManager().Unseal(context.Background(), []byte("share"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestDefaultSealManagerRekeySealed(t *testing.T) {
	mgr := newDefaultSealManager()
	_, _ = mgr.Initialize(context.Background(), 3, 2)
	_, err := mgr.Rekey(context.Background(), 5, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sealed")
}

func TestDefaultSealManagerRekeySuccess(t *testing.T) {
	mgr := newDefaultSealManager()
	_, _ = mgr.Initialize(context.Background(), 3, 2)
	_, _ = mgr.Unseal(context.Background(), []byte("s1"))
	_, _ = mgr.Unseal(context.Background(), []byte("s2"))
	result, err := mgr.Rekey(context.Background(), 5, 3)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, uint64(2), mgr.version)
}

func TestDefaultSealManagerSealReseals(t *testing.T) {
	mgr := newDefaultSealManager()
	_, _ = mgr.Initialize(context.Background(), 3, 2)
	_, _ = mgr.Unseal(context.Background(), []byte("s1"))
	_, _ = mgr.Unseal(context.Background(), []byte("s2"))
	assert.False(t, mgr.IsSealed())
	require.NoError(t, mgr.Seal(context.Background()))
	assert.True(t, mgr.IsSealed())
}

func TestDefaultSealManagerInitializeDefaultShares(t *testing.T) {
	mgr := newDefaultSealManager()
	result, err := mgr.Initialize(context.Background(), 0, 0)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, mgr.threshold)
	assert.Equal(t, 3, mgr.totalShares)
}

// =============================================================================
// Server setter/getter Tests
// =============================================================================

func TestServerSetVectorShardID(t *testing.T) {
	srv := &Server{}
	srv.SetVectorShardID(42)
	assert.Equal(t, uint64(42), srv.vectorShardID)
}

func TestServerSetSealManager(t *testing.T) {
	t.Run("sets_seal_manager", func(t *testing.T) {
		srv := &Server{}
		mgr := newDefaultSealManager()
		srv.SetSealManager(mgr)
		assert.Equal(t, mgr, srv.sealManager)
	})

	t.Run("get_seal_manager_prefers_instance", func(t *testing.T) {
		oldGlobal := globalSealManager
		defer func() { globalSealManager = oldGlobal }()
		globalSealManager = newDefaultSealManager()
		instanceMgr := newDefaultSealManager()
		srv := &Server{sealManager: instanceMgr}
		assert.Equal(t, instanceMgr, srv.getSealManager())
	})

	t.Run("get_seal_manager_falls_back_to_global", func(t *testing.T) {
		oldGlobal := globalSealManager
		defer func() { globalSealManager = oldGlobal }()
		globalMgr := newDefaultSealManager()
		globalSealManager = globalMgr
		srv := &Server{}
		assert.Equal(t, globalMgr, srv.getSealManager())
	})
}

// =============================================================================
// mockBadSealManager returns wrong types from Status/Initialize/Unseal/Rekey
// to cover type assertion failure paths in the Server methods.
// =============================================================================

type mockBadSealManager struct {
	statusErr     error
	initErr       error
	unsealErr     error
	sealErr       error
	rekeyErr      error
	returnBadType bool
}

func (m *mockBadSealManager) Status(_ context.Context) (interface{}, error) {
	if m.statusErr != nil {
		return nil, m.statusErr
	}
	if m.returnBadType {
		return "not-a-status-response", nil
	}
	return nil, nil
}

func (m *mockBadSealManager) IsSealed() bool      { return true }
func (m *mockBadSealManager) IsInitialized() bool { return true }

func (m *mockBadSealManager) Initialize(_ context.Context, _, _ int) (interface{}, error) {
	if m.initErr != nil {
		return nil, m.initErr
	}
	if m.returnBadType {
		return "not-an-init-response", nil
	}
	return nil, nil
}

func (m *mockBadSealManager) Unseal(_ context.Context, _ []byte) (interface{}, error) {
	if m.unsealErr != nil {
		return nil, m.unsealErr
	}
	if m.returnBadType {
		return "not-an-unseal-response", nil
	}
	return nil, nil
}

func (m *mockBadSealManager) Seal(_ context.Context) error {
	return m.sealErr
}

func (m *mockBadSealManager) Rekey(_ context.Context, _, _ int) (interface{}, error) {
	if m.rekeyErr != nil {
		return nil, m.rekeyErr
	}
	if m.returnBadType {
		return "not-a-rekey-response", nil
	}
	return nil, nil
}

func (m *mockBadSealManager) GenerateRecoveryKeys(_ context.Context, _, _ int) ([][]byte, error) {
	return nil, nil
}
func (m *mockBadSealManager) DeleteRecoveryKeys(_ context.Context) error { return nil }
func (m *mockBadSealManager) GenerateRootToken(_ context.Context, _ string, _ [][]byte) (string, error) {
	return "", nil
}
func (m *mockBadSealManager) Close() error { return nil }

func newTestServerWithLogger() *Server {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return &Server{logger: log}
}

// =============================================================================
// Server SealStatus Error Paths
// =============================================================================

func TestServerSealStatus_ErrorFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{statusErr: errors.New("status check failed")}

	resp, err := srv.SealStatus(context.Background(), &pb.SealStatusRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "status check failed")
}

func TestServerSealStatus_BadTypeFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{returnBadType: true}

	resp, err := srv.SealStatus(context.Background(), &pb.SealStatusRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "unexpected status response type")
}

// =============================================================================
// Server SealInit Error Paths
// =============================================================================

func TestServerSealInit_ErrorFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{initErr: errors.New("init failed")}

	resp, err := srv.SealInit(context.Background(), &pb.SealInitRequest{Threshold: 2, TotalShares: 3})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "init failed")
}

func TestServerSealInit_BadTypeFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{returnBadType: true}

	resp, err := srv.SealInit(context.Background(), &pb.SealInitRequest{Threshold: 2, TotalShares: 3})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "unexpected initialization response type")
}

// =============================================================================
// Server Unseal Error Paths
// =============================================================================

func TestServerUnseal_ErrorFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{unsealErr: errors.New("unseal failed")}

	resp, err := srv.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("share")})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Sealed)
	assert.Contains(t, resp.Error, "unseal failed")
}

func TestServerUnseal_BadTypeFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{returnBadType: true}

	resp, err := srv.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("share")})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Sealed)
	assert.Contains(t, resp.Error, "unexpected unseal response type")
}

// =============================================================================
// Server Seal Error Paths
// =============================================================================

func TestServerSeal_ErrorFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{sealErr: errors.New("seal failed")}

	resp, err := srv.Seal(context.Background(), &pb.SealRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Sealed)
	assert.Contains(t, resp.Error, "seal failed")
}

// =============================================================================
// Server SealRekey Error Paths
// =============================================================================

func TestServerSealRekey_ErrorFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{rekeyErr: errors.New("rekey failed")}

	resp, err := srv.SealRekey(context.Background(), &pb.SealRekeyRequest{NewThreshold: 3, NewTotal: 5})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "rekey failed")
}

func TestServerSealRekey_BadTypeFromManager(t *testing.T) {
	srv := newTestServerWithLogger()
	srv.sealManager = &mockBadSealManager{returnBadType: true}

	resp, err := srv.SealRekey(context.Background(), &pb.SealRekeyRequest{NewThreshold: 3, NewTotal: 5})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Error, "unexpected rekey response type")
}

// TestServerSealRekey_SuccessWithRealManager tests the full success path
// through SealRekey using the real defaultSealManager.
func TestServerSealRekey_SuccessWithRealManager(t *testing.T) {
	oldGlobal := globalSealManager
	defer func() { globalSealManager = oldGlobal }()
	globalSealManager = nil

	srv := newTestServerWithLogger()

	// Initialize
	initResp, err := srv.SealInit(context.Background(), &pb.SealInitRequest{Threshold: 2, TotalShares: 3})
	require.NoError(t, err)
	assert.Empty(t, initResp.Error)

	// Unseal
	unsealResp, err := srv.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("s1")})
	require.NoError(t, err)
	assert.Empty(t, unsealResp.Error)

	unsealResp, err = srv.Unseal(context.Background(), &pb.UnsealRequest{Share: []byte("s2")})
	require.NoError(t, err)
	assert.False(t, unsealResp.Sealed)

	// Rekey
	rekeyResp, err := srv.SealRekey(context.Background(), &pb.SealRekeyRequest{NewThreshold: 3, NewTotal: 5})
	require.NoError(t, err)
	assert.Empty(t, rekeyResp.Error)
	assert.NotEmpty(t, rekeyResp.Shares)
	assert.Equal(t, uint64(2), rekeyResp.Version)
}
