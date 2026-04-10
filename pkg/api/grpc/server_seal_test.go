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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// serverSealTestManager is a stateful mock SealManager for server_seal_test.go
// integration-style tests that exercise the full gRPC -> SealService -> SealManager path.
type serverSealTestManager struct {
	mu          sync.Mutex
	sealed      bool
	initialized bool
	shares      [][]byte
	threshold   int
	totalShares int
	unsealCount int
}

var _ interfaces.SealManager = (*serverSealTestManager)(nil)

func newServerSealTestManager() *serverSealTestManager {
	return &serverSealTestManager{
		sealed:      true,
		initialized: false,
		threshold:   2,
		totalShares: 3,
	}
}

func (m *serverSealTestManager) Status(_ context.Context) (interface{}, error) {
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

func (m *serverSealTestManager) IsSealed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sealed
}

func (m *serverSealTestManager) IsInitialized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initialized
}

func (m *serverSealTestManager) Initialize(_ context.Context, shares, threshold int) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initialized {
		return nil, errors.New("already initialized")
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
	m.sealed = true
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

func (m *serverSealTestManager) Unseal(_ context.Context, _ []byte) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		return nil, errors.New("not initialized")
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

func (m *serverSealTestManager) Seal(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sealed = true
	m.unsealCount = 0
	return nil
}

func (m *serverSealTestManager) Rekey(_ context.Context, shares, threshold int) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sealed {
		return nil, errors.New("barrier is sealed")
	}
	shareBytes := make([][]byte, shares)
	for i := range shareBytes {
		shareBytes[i] = []byte{byte(i + 10)}
	}
	return &service.RekeyResponse{
		Shares:    shareBytes,
		Version:   2,
		Timestamp: time.Now(),
	}, nil
}

func (m *serverSealTestManager) GenerateRecoveryKeys(_ context.Context, _, totalKeys int) ([][]byte, error) {
	keys := make([][]byte, totalKeys)
	for i := range keys {
		keys[i] = []byte{byte(i + 100)}
	}
	return keys, nil
}

func (m *serverSealTestManager) DeleteRecoveryKeys(_ context.Context) error { return nil }

func (m *serverSealTestManager) GenerateRootToken(_ context.Context, _ string, _ [][]byte) (string, error) {
	return "test-root-token", nil
}

func (m *serverSealTestManager) Close() error { return nil }

// createTestBarrierSealManager creates a mock SealManager for integration-style tests.
func createTestBarrierSealManager(t *testing.T) (interfaces.SealManager, *logger.Logger) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return newServerSealTestManager(), log
}

// TestServerWithSealCreation tests the NewServerWithSeal function.
func TestServerWithSealCreation(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func(t *testing.T) (*Server, interfaces.SealManager, *logger.Logger)
		expectError bool
	}{
		{
			name: "successful_creation",
			setupServer: func(t *testing.T) (*Server, interfaces.SealManager, *logger.Logger) {
				manager, log := createTestBarrierSealManager(t)

				srv := &Server{
					logger: log,
				}

				return srv, manager, log
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, manager, log := tt.setupServer(t)
			if manager != nil {
				defer manager.Close()
			}

			sealSrv := NewServerWithSeal(srv, manager)

			require.NotNil(t, sealSrv)
			assert.NotNil(t, sealSrv.Server)
			assert.NotNil(t, sealSrv.sealSvc)
			assert.Equal(t, log, sealSrv.logger)
		})
	}
}

// TestSealStatus tests the SealStatus RPC method.
func TestSealStatus(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T) (*ServerWithSeal, interfaces.SealManager)
		request        *pb.SealStatusRequest
		expectedSealed bool
		expectedInit   bool
		expectError    bool
	}{
		{
			name: "status_before_initialization",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				return NewServerWithSeal(srv, manager), manager
			},
			request:        &pb.SealStatusRequest{},
			expectedSealed: true,
			expectedInit:   false,
			expectError:    false,
		},
		{
			name: "status_after_initialization",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// Initialize the seal
				ctx := context.Background()
				initReq := &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				}
				_, err := sealSrv.SealInit(ctx, initReq)
				require.NoError(t, err)

				return sealSrv, manager
			},
			request:        &pb.SealStatusRequest{},
			expectedSealed: true,
			expectedInit:   true,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealSrv, manager := tt.setup(t)
			defer manager.Close()

			ctx := context.Background()
			resp, err := sealSrv.SealStatus(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, tt.expectedSealed, resp.Sealed)
				assert.Equal(t, tt.expectedInit, resp.Initialized)
				if tt.expectedInit {
					assert.Equal(t, int32(2), resp.Threshold)
					assert.Equal(t, int32(3), resp.TotalShares)
				}
			}
		})
	}
}

// TestSealInit tests the SealInit RPC method.
func TestSealInit(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) (*ServerWithSeal, interfaces.SealManager)
		request     *pb.SealInitRequest
		expectError bool
		checkResp   func(t *testing.T, resp *pb.SealInitResponse)
	}{
		{
			name: "successful_initialization",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				return NewServerWithSeal(srv, manager), manager
			},
			request: &pb.SealInitRequest{
				Threshold:   2,
				TotalShares: 3,
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SealInitResponse) {
				assert.Empty(t, resp.Error)
				assert.Len(t, resp.Shares, 3)
			},
		},
		{
			name: "initialization_with_recovery_keys",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				return NewServerWithSeal(srv, manager), manager
			},
			request: &pb.SealInitRequest{
				Threshold:           2,
				TotalShares:         3,
				RecoveryKeysEnabled: true,
				RecoveryThreshold:   2,
				RecoveryTotalKeys:   3,
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SealInitResponse) {
				assert.Empty(t, resp.Error)
				assert.Len(t, resp.Shares, 3)
			},
		},
		{
			name: "double_initialization_fails",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// First initialization
				ctx := context.Background()
				_, err := sealSrv.SealInit(ctx, &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				})
				require.NoError(t, err)

				return sealSrv, manager
			},
			request: &pb.SealInitRequest{
				Threshold:   2,
				TotalShares: 3,
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SealInitResponse) {
				assert.NotEmpty(t, resp.Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealSrv, manager := tt.setup(t)
			defer manager.Close()

			ctx := context.Background()
			resp, err := sealSrv.SealInit(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestUnseal tests the Unseal RPC method.
func TestUnseal(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) (*ServerWithSeal, interfaces.SealManager, [][]byte)
		getRequest  func(shares [][]byte) *pb.UnsealRequest
		expectError bool
		checkResp   func(t *testing.T, resp *pb.UnsealResponse)
	}{
		{
			name: "successful_unseal_with_threshold_shares",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager, [][]byte) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// Initialize
				ctx := context.Background()
				initResp, err := sealSrv.SealInit(ctx, &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				})
				require.NoError(t, err)

				return sealSrv, manager, initResp.Shares
			},
			getRequest: func(shares [][]byte) *pb.UnsealRequest {
				return &pb.UnsealRequest{Share: shares[0]}
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.UnsealResponse) {
				assert.Empty(t, resp.Error)
				assert.True(t, resp.Sealed) // Still sealed after first share
				assert.Equal(t, int32(1), resp.SharesProvided)
				assert.Equal(t, int32(2), resp.Threshold)
			},
		},
		{
			name: "unseal_completes_with_threshold_shares",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager, [][]byte) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// Initialize
				ctx := context.Background()
				initResp, err := sealSrv.SealInit(ctx, &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				})
				require.NoError(t, err)

				// Provide first share
				_, err = sealSrv.Unseal(ctx, &pb.UnsealRequest{Share: initResp.Shares[0]})
				require.NoError(t, err)

				return sealSrv, manager, initResp.Shares
			},
			getRequest: func(shares [][]byte) *pb.UnsealRequest {
				return &pb.UnsealRequest{Share: shares[1]}
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.UnsealResponse) {
				assert.Empty(t, resp.Error)
				assert.False(t, resp.Sealed) // Now unsealed
				assert.Equal(t, int32(2), resp.SharesProvided)
			},
		},
		{
			name: "unseal_before_initialization",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager, [][]byte) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				return NewServerWithSeal(srv, manager), manager, nil
			},
			getRequest: func(shares [][]byte) *pb.UnsealRequest {
				return &pb.UnsealRequest{Share: []byte("invalid-share")}
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.UnsealResponse) {
				assert.NotEmpty(t, resp.Error)
				assert.True(t, resp.Sealed)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealSrv, manager, shares := tt.setup(t)
			defer manager.Close()

			ctx := context.Background()
			req := tt.getRequest(shares)
			resp, err := sealSrv.Unseal(ctx, req)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestSeal tests the Seal RPC method.
func TestSeal(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) (*ServerWithSeal, interfaces.SealManager)
		request     *pb.SealRequest
		expectError bool
		checkResp   func(t *testing.T, resp *pb.SealResponse)
	}{
		{
			name: "seal_unsealed_system",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// Initialize and unseal
				ctx := context.Background()
				initResp, err := sealSrv.SealInit(ctx, &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				})
				require.NoError(t, err)

				// Unseal with threshold shares
				for i := 0; i < 2; i++ {
					_, err = sealSrv.Unseal(ctx, &pb.UnsealRequest{Share: initResp.Shares[i]})
					require.NoError(t, err)
				}

				return sealSrv, manager
			},
			request:     &pb.SealRequest{Force: false},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SealResponse) {
				assert.Empty(t, resp.Error)
				assert.True(t, resp.Sealed)
			},
		},
		{
			name: "seal_with_force_flag",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// Initialize and unseal
				ctx := context.Background()
				initResp, err := sealSrv.SealInit(ctx, &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				})
				require.NoError(t, err)

				for i := 0; i < 2; i++ {
					_, err = sealSrv.Unseal(ctx, &pb.UnsealRequest{Share: initResp.Shares[i]})
					require.NoError(t, err)
				}

				return sealSrv, manager
			},
			request:     &pb.SealRequest{Force: true},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SealResponse) {
				assert.Empty(t, resp.Error)
				assert.True(t, resp.Sealed)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealSrv, manager := tt.setup(t)
			defer manager.Close()

			ctx := context.Background()
			resp, err := sealSrv.Seal(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// TestSealRekey tests the SealRekey RPC method.
func TestSealRekey(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) (*ServerWithSeal, interfaces.SealManager)
		request     *pb.SealRekeyRequest
		expectError bool
		checkResp   func(t *testing.T, resp *pb.SealRekeyResponse)
	}{
		{
			name: "successful_rekey",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// Initialize and unseal
				ctx := context.Background()
				initResp, err := sealSrv.SealInit(ctx, &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				})
				require.NoError(t, err)

				for i := 0; i < 2; i++ {
					_, err = sealSrv.Unseal(ctx, &pb.UnsealRequest{Share: initResp.Shares[i]})
					require.NoError(t, err)
				}

				return sealSrv, manager
			},
			request: &pb.SealRekeyRequest{
				NewThreshold: 3,
				NewTotal:     5,
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SealRekeyResponse) {
				assert.Empty(t, resp.Error)
				assert.Len(t, resp.Shares, 5)
				assert.NotEmpty(t, resp.Version)
			},
		},
		{
			name: "rekey_while_sealed_fails",
			setup: func(t *testing.T) (*ServerWithSeal, interfaces.SealManager) {
				manager, log := createTestBarrierSealManager(t)
				srv := &Server{logger: log}
				sealSrv := NewServerWithSeal(srv, manager)

				// Initialize but don't unseal
				ctx := context.Background()
				_, err := sealSrv.SealInit(ctx, &pb.SealInitRequest{
					Threshold:   2,
					TotalShares: 3,
				})
				require.NoError(t, err)

				return sealSrv, manager
			},
			request: &pb.SealRekeyRequest{
				NewThreshold: 3,
				NewTotal:     5,
			},
			expectError: false,
			checkResp: func(t *testing.T, resp *pb.SealRekeyResponse) {
				assert.NotEmpty(t, resp.Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealSrv, manager := tt.setup(t)
			defer manager.Close()

			ctx := context.Background()
			resp, err := sealSrv.SealRekey(ctx, tt.request)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
				if tt.checkResp != nil {
					tt.checkResp(t, resp)
				}
			}
		})
	}
}

// --- Merged from server_seal_coverage_test.go ---

// mockSealManagerForServerWithSeal provides a configurable seal manager for
// testing the ServerWithSeal wrapper error paths.
type mockSealManagerForServerWithSeal struct {
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

var _ interfaces.SealManager = (*mockSealManagerForServerWithSeal)(nil)

func (m *mockSealManagerForServerWithSeal) Initialize(ctx context.Context, shares, threshold int) (interface{}, error) {
	if m.initializeFunc != nil {
		return m.initializeFunc(ctx, shares, threshold)
	}
	return &service.SealInitResponse{Shares: [][]byte{{1}, {2}, {3}}}, nil
}

func (m *mockSealManagerForServerWithSeal) Unseal(ctx context.Context, share []byte) (interface{}, error) {
	if m.unsealFunc != nil {
		return m.unsealFunc(ctx, share)
	}
	return &service.UnsealResponse{Sealed: false}, nil
}

func (m *mockSealManagerForServerWithSeal) Seal(ctx context.Context) error {
	if m.sealFunc != nil {
		return m.sealFunc(ctx)
	}
	return nil
}

func (m *mockSealManagerForServerWithSeal) Status(ctx context.Context) (interface{}, error) {
	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}
	return &service.SealStatusResponse{Sealed: true, Initialized: true}, nil
}

func (m *mockSealManagerForServerWithSeal) IsSealed() bool {
	if m.isSealedFunc != nil {
		return m.isSealedFunc()
	}
	return true
}

func (m *mockSealManagerForServerWithSeal) IsInitialized() bool {
	if m.isInitializedFunc != nil {
		return m.isInitializedFunc()
	}
	return false
}

func (m *mockSealManagerForServerWithSeal) Rekey(ctx context.Context, shares, threshold int) (interface{}, error) {
	if m.rekeyFunc != nil {
		return m.rekeyFunc(ctx, shares, threshold)
	}
	return &service.RekeyResponse{}, nil
}

func (m *mockSealManagerForServerWithSeal) GenerateRecoveryKeys(ctx context.Context, threshold, totalKeys int) ([][]byte, error) {
	if m.genRecoveryKeysFunc != nil {
		return m.genRecoveryKeysFunc(ctx, threshold, totalKeys)
	}
	return nil, nil
}

func (m *mockSealManagerForServerWithSeal) DeleteRecoveryKeys(ctx context.Context) error {
	if m.delRecoveryKeysFunc != nil {
		return m.delRecoveryKeysFunc(ctx)
	}
	return nil
}

func (m *mockSealManagerForServerWithSeal) GenerateRootToken(ctx context.Context, strategy string, shares [][]byte) (string, error) {
	if m.genRootTokenFunc != nil {
		return m.genRootTokenFunc(ctx, strategy, shares)
	}
	return "", nil
}

func (m *mockSealManagerForServerWithSeal) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func newTestServerWithSeal(t *testing.T, mgr interfaces.SealManager) *ServerWithSeal {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	srv := &Server{logger: log}
	return NewServerWithSeal(srv, mgr)
}

// --- SealStatus tests ---

func TestServerWithSeal_SealStatus_ManagerError(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		statusFunc: func(_ context.Context) (interface{}, error) {
			return nil, errors.New("status retrieval failed")
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.SealStatus(context.Background(), &pb.SealStatusRequest{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Sealed)
	assert.False(t, resp.Initialized)
}

func TestServerWithSeal_SealStatus_Success(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		statusFunc: func(_ context.Context) (interface{}, error) {
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
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.SealStatus(context.Background(), &pb.SealStatusRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.False(t, resp.Sealed)
	assert.True(t, resp.Initialized)
	assert.Equal(t, int32(2), resp.Threshold)
	assert.Equal(t, int32(3), resp.TotalShares)
}

// --- SealInit tests ---

func TestServerWithSeal_SealInit_Error(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		initializeFunc: func(_ context.Context, _, _ int) (interface{}, error) {
			return nil, errors.New("already initialized")
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.SealInit(context.Background(), &pb.SealInitRequest{
		Threshold:   2,
		TotalShares: 3,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "already initialized")
}

func TestServerWithSeal_SealInit_Success(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		initializeFunc: func(_ context.Context, _, _ int) (interface{}, error) {
			return &service.SealInitResponse{
				Shares:       [][]byte{{1}, {2}, {3}},
				RecoveryKeys: [][]byte{{4}, {5}},
				RootToken:    "root-token",
				Version:      1,
			}, nil
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.SealInit(context.Background(), &pb.SealInitRequest{
		Threshold:   2,
		TotalShares: 3,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Len(t, resp.Shares, 3)
	assert.Len(t, resp.RecoveryKeys, 2)
	assert.Equal(t, "root-token", resp.RootToken)
}

// --- Unseal tests ---

func TestServerWithSeal_Unseal_Error(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		unsealFunc: func(_ context.Context, _ []byte) (interface{}, error) {
			return nil, errors.New("invalid share")
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.Unseal(context.Background(), &pb.UnsealRequest{
		Share: []byte("bad-share"),
	})
	require.NoError(t, err)
	assert.True(t, resp.Sealed)
	assert.Contains(t, resp.Error, "invalid share")
}

func TestServerWithSeal_Unseal_Success(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		unsealFunc: func(_ context.Context, _ []byte) (interface{}, error) {
			return &service.UnsealResponse{
				Sealed:         false,
				SharesProvided: 2,
				Threshold:      2,
			}, nil
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.Unseal(context.Background(), &pb.UnsealRequest{
		Share:  []byte("good-share"),
		NodeId: 1,
	})
	require.NoError(t, err)
	assert.False(t, resp.Sealed)
	assert.Equal(t, int32(2), resp.SharesProvided)
}

// --- Seal tests ---

func TestServerWithSeal_Seal_Error(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		sealFunc: func(_ context.Context) error {
			return errors.New("seal failed: not unsealed")
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.Seal(context.Background(), &pb.SealRequest{Force: true})
	require.NoError(t, err)
	assert.False(t, resp.Sealed)
	assert.Contains(t, resp.Error, "seal failed")
}

func TestServerWithSeal_Seal_Success(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		sealFunc: func(_ context.Context) error { return nil },
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.Seal(context.Background(), &pb.SealRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Sealed)
	assert.Empty(t, resp.Error)
}

// --- SealRekey tests ---

func TestServerWithSeal_SealRekey_Error(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		rekeyFunc: func(_ context.Context, _, _ int) (interface{}, error) {
			return nil, errors.New("still sealed")
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.SealRekey(context.Background(), &pb.SealRekeyRequest{
		NewThreshold: 3,
		NewTotal:     5,
	})
	require.NoError(t, err)
	assert.Contains(t, resp.Error, "still sealed")
}

func TestServerWithSeal_SealRekey_Success(t *testing.T) {
	mgr := &mockSealManagerForServerWithSeal{
		rekeyFunc: func(_ context.Context, _, _ int) (interface{}, error) {
			return &service.RekeyResponse{
				Shares:  [][]byte{{1}, {2}, {3}, {4}, {5}},
				Version: 2,
			}, nil
		},
	}
	sws := newTestServerWithSeal(t, mgr)

	resp, err := sws.SealRekey(context.Background(), &pb.SealRekeyRequest{
		NewThreshold: 3,
		NewTotal:     5,
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.Len(t, resp.Shares, 5)
	assert.Equal(t, uint64(2), resp.Version)
}

// --- convertSealMode tests ---

func TestServerWithSeal_ConvertSealMode_AllModes(t *testing.T) {
	assert.Equal(t, pb.SealMode_SEAL_MODE_MANUAL, convertSealMode("manual"))
	assert.Equal(t, pb.SealMode_SEAL_MODE_AUTO, convertSealMode("auto"))
	assert.Equal(t, pb.SealMode_SEAL_MODE_CRYPTO, convertSealMode("crypto"))
	assert.Equal(t, pb.SealMode_SEAL_MODE_UNSPECIFIED, convertSealMode("unknown"))
	assert.Equal(t, pb.SealMode_SEAL_MODE_UNSPECIFIED, convertSealMode(""))
}
