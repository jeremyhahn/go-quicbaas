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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// noopLogger implements interfaces.Logger as a no-op for unit tests.
type noopLogger struct{}

func (l *noopLogger) Debug(msg string)                                           {}
func (l *noopLogger) Debugf(format string, args ...interface{})                  {}
func (l *noopLogger) Info(msg string)                                            {}
func (l *noopLogger) Infof(format string, args ...interface{})                   {}
func (l *noopLogger) Warn(msg string)                                            {}
func (l *noopLogger) Warnf(format string, args ...interface{})                   {}
func (l *noopLogger) Error(msg string)                                           {}
func (l *noopLogger) Errorf(format string, args ...interface{})                  {}
func (l *noopLogger) Fatal(msg string)                                           {}
func (l *noopLogger) Fatalf(format string, args ...interface{})                  {}
func (l *noopLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *noopLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *noopLogger) WithError(err error) interfaces.Logger                      { return l }

// testSealManager is a mock SealManager that tracks state for integration-style
// tests at the gRPC layer.
type testSealManager struct {
	mu          sync.Mutex
	sealed      bool
	initialized bool
	shares      [][]byte
	threshold   int
	totalShares int
	unsealCount int
}

var _ interfaces.SealManager = (*testSealManager)(nil)

func newTestSealManager() *testSealManager {
	return &testSealManager{
		sealed:      true,
		initialized: false,
		threshold:   2,
		totalShares: 3,
	}
}

func (m *testSealManager) Status(_ context.Context) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &service.SealStatusResponse{
		Sealed:      m.sealed,
		Initialized: m.initialized,
		Mode:        "manual",
		Threshold:   m.threshold,
		TotalShares: m.totalShares,
		Version:     1,
		Timestamp:   time.Now(),
	}, nil
}

func (m *testSealManager) IsSealed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sealed
}

func (m *testSealManager) IsInitialized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initialized
}

func (m *testSealManager) Initialize(_ context.Context, shares, threshold int) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initialized {
		return nil, &initAlreadyError{}
	}
	if shares <= 0 {
		shares = m.totalShares
	}
	if threshold <= 0 {
		threshold = m.threshold
	}
	shareBytes := make([][]byte, shares)
	for i := range shareBytes {
		shareBytes[i] = []byte{byte(i + 1)}
	}
	m.shares = shareBytes
	m.initialized = true
	m.sealed = true // sealed after init
	m.threshold = threshold
	m.totalShares = shares
	m.unsealCount = 0
	return &service.SealInitResponse{
		Shares:    shareBytes,
		RootToken: "test-root-token",
		Version:   1,
		Timestamp: time.Now(),
	}, nil
}

func (m *testSealManager) Unseal(_ context.Context, share []byte) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		return nil, &notInitError{}
	}
	m.unsealCount++
	complete := m.unsealCount >= m.threshold
	if complete {
		m.sealed = false
	}
	return &service.UnsealResponse{
		Sealed:         !complete,
		SharesProvided: m.unsealCount,
		Threshold:      m.threshold,
		Timestamp:      time.Now(),
	}, nil
}

func (m *testSealManager) Seal(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sealed = true
	m.unsealCount = 0
	return nil
}

func (m *testSealManager) Rekey(_ context.Context, shares, threshold int) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return nil, &sealedError{}
	}
	shareBytes := make([][]byte, shares)
	for i := range shareBytes {
		shareBytes[i] = []byte{byte(i + 10)}
	}
	m.shares = shareBytes
	m.threshold = threshold
	m.totalShares = shares
	return &service.RekeyResponse{
		Shares:    shareBytes,
		Version:   2,
		Timestamp: time.Now(),
	}, nil
}

func (m *testSealManager) GenerateRecoveryKeys(_ context.Context, threshold, totalKeys int) ([][]byte, error) {
	keys := make([][]byte, totalKeys)
	for i := range keys {
		keys[i] = []byte{byte(i + 100)}
	}
	return keys, nil
}

func (m *testSealManager) DeleteRecoveryKeys(_ context.Context) error {
	return nil
}

func (m *testSealManager) GenerateRootToken(_ context.Context, strategy string, shares [][]byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return "", &sealedError{}
	}
	return "generated-root-token", nil
}

func (m *testSealManager) Close() error {
	return nil
}

// Error types for the test mock.
type initAlreadyError struct{}

func (e *initAlreadyError) Error() string { return "already initialized" }

type notInitError struct{}

func (e *notInitError) Error() string { return "not initialized" }

type sealedError struct{}

func (e *sealedError) Error() string { return "barrier is sealed" }

func createTestSealManager(t *testing.T) (interfaces.SealManager, *logger.Logger) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
	})
	require.NoError(t, err)
	return newTestSealManager(), log
}

// bootstrapAndUnseal bootstraps the seal service and unseals it, returning
// the bootstrap response containing the shares.
func bootstrapAndUnseal(t *testing.T, ctx context.Context, svc *SealService) *BootstrapResponse {
	t.Helper()

	bootstrapReq := &BootstrapRequest{}
	bootstrapResp, err := svc.Bootstrap(ctx, bootstrapReq)
	require.NoError(t, err)
	require.Empty(t, bootstrapResp.Error)
	require.Len(t, bootstrapResp.Shares, 3)

	// Unseal with threshold shares (2 of 3).
	for i := 0; i < 2; i++ {
		unsealReq := &UnsealRequestDTO{Share: bootstrapResp.Shares[i]}
		_, err := svc.Unseal(ctx, unsealReq)
		require.NoError(t, err)
	}
	require.False(t, svc.IsSealed())

	return bootstrapResp
}

func TestSealServiceBootstrap(t *testing.T) {
	t.Run("successful bootstrap", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()
		req := &BootstrapRequest{
			Threshold:           2,
			TotalShares:         3,
			RecoveryKeysEnabled: true,
		}

		resp, err := svc.Bootstrap(ctx, req)
		require.NoError(t, err)
		assert.Empty(t, resp.Error)
		assert.Len(t, resp.Shares, 3)
	})

	t.Run("bootstrap already initialized", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()
		req := &BootstrapRequest{}

		// First bootstrap should succeed
		_, err := svc.Bootstrap(ctx, req)
		require.NoError(t, err)

		// Second bootstrap should fail
		resp, err := svc.Bootstrap(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Error)
	})
}

func TestSealServiceUnseal(t *testing.T) {
	t.Run("successful unseal", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()

		// Bootstrap first
		bootstrapReq := &BootstrapRequest{}
		bootstrapResp, err := svc.Bootstrap(ctx, bootstrapReq)
		require.NoError(t, err)
		require.Len(t, bootstrapResp.Shares, 3)

		// Unseal with first share
		unsealReq1 := &UnsealRequestDTO{Share: bootstrapResp.Shares[0]}
		resp1, err := svc.Unseal(ctx, unsealReq1)
		require.NoError(t, err)
		assert.True(t, resp1.Sealed) // Still sealed
		assert.Equal(t, 1, resp1.SharesProvided)

		// Unseal with second share
		unsealReq2 := &UnsealRequestDTO{Share: bootstrapResp.Shares[1]}
		resp2, err := svc.Unseal(ctx, unsealReq2)
		require.NoError(t, err)
		assert.False(t, resp2.Sealed) // Now unsealed
		assert.Equal(t, 2, resp2.SharesProvided)
	})

	t.Run("unseal not initialized", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()
		req := &UnsealRequestDTO{Share: []byte("invalid")}

		resp, err := svc.Unseal(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Error)
	})
}

func TestSealServiceSeal(t *testing.T) {
	t.Run("successful seal", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()

		// Bootstrap and unseal
		bootstrapAndUnseal(t, ctx, svc)

		// Should be unsealed
		assert.False(t, svc.IsSealed())

		// Seal
		sealReq := &SealRequestDTO{}
		sealResp, err := svc.Seal(ctx, sealReq)
		require.NoError(t, err)
		assert.True(t, sealResp.Sealed)
		assert.Empty(t, sealResp.Error)
	})
}

func TestSealServiceStatus(t *testing.T) {
	t.Run("status before bootstrap", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()
		resp, err := svc.Status(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Sealed)
		assert.False(t, resp.Initialized)
	})

	t.Run("status after bootstrap", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()

		// Bootstrap
		bootstrapReq := &BootstrapRequest{}
		_, err := svc.Bootstrap(ctx, bootstrapReq)
		require.NoError(t, err)

		// Check status - barrier is sealed after bootstrap
		resp, err := svc.Status(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Sealed)
		assert.True(t, resp.Initialized)
		assert.Equal(t, "manual", resp.Mode)
		assert.Equal(t, 2, resp.Threshold)
		assert.Equal(t, 3, resp.TotalShares)
	})
}

func TestSealServiceRekey(t *testing.T) {
	t.Run("successful rekey", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()

		// Bootstrap and unseal
		bootstrapAndUnseal(t, ctx, svc)

		// Rekey
		rekeyReq := &RekeyRequestDTO{
			NewThreshold: 3,
			NewTotal:     5,
		}
		rekeyResp, err := svc.Rekey(ctx, rekeyReq)
		require.NoError(t, err)
		assert.Empty(t, rekeyResp.Error)
		assert.Len(t, rekeyResp.Shares, 5)
	})

	t.Run("rekey while sealed", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()

		// Bootstrap (barrier stays sealed)
		bootstrapReq := &BootstrapRequest{}
		_, err := svc.Bootstrap(ctx, bootstrapReq)
		require.NoError(t, err)

		// Try to rekey while sealed (should fail)
		rekeyReq := &RekeyRequestDTO{
			NewThreshold: 3,
			NewTotal:     5,
		}
		rekeyResp, err := svc.Rekey(ctx, rekeyReq)
		require.NoError(t, err)
		assert.NotEmpty(t, rekeyResp.Error)
	})
}

func TestSealServiceRecover(t *testing.T) {
	t.Run("successful recovery", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()

		// Bootstrap and unseal so we can generate recovery keys.
		bootstrapAndUnseal(t, ctx, svc)

		// Generate recovery keys while unsealed.
		recoveryKeys, err := manager.GenerateRecoveryKeys(ctx, 2, 3)
		require.NoError(t, err)
		require.Len(t, recoveryKeys, 3)

		// Recover with recovery keys (barrier must be unsealed).
		recoverReq := &RecoverRequestDTO{
			RecoveryKeys: recoveryKeys,
		}
		recoverResp, err := svc.Recover(ctx, recoverReq)
		require.NoError(t, err)
		assert.True(t, recoverResp.Success)
		assert.Empty(t, recoverResp.Error)
	})

	t.Run("recovery while sealed fails", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		ctx := context.Background()

		// Bootstrap (stays sealed)
		bootstrapReq := &BootstrapRequest{}
		_, err := svc.Bootstrap(ctx, bootstrapReq)
		require.NoError(t, err)

		recoverReq := &RecoverRequestDTO{
			RecoveryKeys: [][]byte{{1}, {2}},
		}
		recoverResp, err := svc.Recover(ctx, recoverReq)
		require.NoError(t, err)
		assert.False(t, recoverResp.Success)
		assert.NotEmpty(t, recoverResp.Error)
	})
}

func TestSealServiceHelperMethods(t *testing.T) {
	t.Run("IsSealed and IsInitialized", func(t *testing.T) {
		manager, log := createTestSealManager(t)
		defer manager.Close()

		svc := NewSealService(manager, log)

		// Before bootstrap
		assert.True(t, svc.IsSealed())
		assert.False(t, svc.IsInitialized())

		// After bootstrap (barrier is sealed after init)
		ctx := context.Background()
		bootstrapReq := &BootstrapRequest{}
		_, err := svc.Bootstrap(ctx, bootstrapReq)
		require.NoError(t, err)

		assert.True(t, svc.IsSealed())
		assert.True(t, svc.IsInitialized())
	})
}
