//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// MockClusterDEKService is a mock implementation of service.ClusterDEKService for testing.
type MockClusterDEKService struct {
	mock.Mock
}

func (m *MockClusterDEKService) InitializeClusterDEK(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.InitClusterDEKResponse), args.Error(1)
}

func (m *MockClusterDEKService) GetClusterDEKStatus(ctx context.Context) (*service.ClusterDEKStatusResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ClusterDEKStatusResponse), args.Error(1)
}

func (m *MockClusterDEKService) RotateClusterDEK(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RotateClusterDEKResponse), args.Error(1)
}

func (m *MockClusterDEKService) SyncNodeDEK(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SyncNodeDEKResponse), args.Error(1)
}

func (m *MockClusterDEKService) GetNodeDEKStatus(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetNodeDEKStatusResponse), args.Error(1)
}

func (m *MockClusterDEKService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.StartReencryptionResponse), args.Error(1)
}

func (m *MockClusterDEKService) GetReencryptionStatus(ctx context.Context) (*service.ReencryptionStatusResponse, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ReencryptionStatusResponse), args.Error(1)
}

func (m *MockClusterDEKService) CancelReencryption(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockClusterDEKService) RecoverDEK(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RecoverDEKResponse), args.Error(1)
}

// MockQuicBaaSWithClusterDEK wraps MockQuicBaaS to provide a ClusterDEK service.
type MockQuicBaaSWithClusterDEK struct {
	*testutil.MockQuicBaaS
	ClusterDEKSvc *MockClusterDEKService
}

func (m *MockQuicBaaSWithClusterDEK) ClusterDEK() service.ClusterDEKService {
	return m.ClusterDEKSvc
}

// setupMockServerWithClusterDEK creates a test server with ClusterDEK service support.
func setupMockServerWithClusterDEK(t *testing.T) (*Server, *MockClusterDEKService, func()) {
	t.Helper()

	server, mockService, cleanup := setupMockServer(t)

	// Create mock cluster DEK service
	mockClusterDEK := &MockClusterDEKService{}

	// Wrap the mock service to include ClusterDEK
	wrappedMock := &MockQuicBaaSWithClusterDEK{
		MockQuicBaaS:  mockService,
		ClusterDEKSvc: mockClusterDEK,
	}

	// Replace server's service with wrapped mock
	server.service = wrappedMock

	return server, mockClusterDEK, cleanup
}

// =============================================================================
// TestHandleInitializeClusterDEK
// =============================================================================

func TestHandleInitializeClusterDEK(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "initialize cluster DEK successfully",
			body: InitClusterDEKRESTRequest{
				Algorithm:               "AES-256-GCM",
				KeySize:                 256,
				Metadata:                map[string]string{"env": "production"},
				RotationPolicyEnabled:   true,
				RotationIntervalSeconds: 86400,
				AutoReencrypt:           true,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("InitializeClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.InitClusterDEKRequest) bool {
					return req.Algorithm == "AES-256-GCM" &&
						req.KeySize == 256 &&
						req.RotationPolicy != nil &&
						req.RotationPolicy.Enabled
				})).Return(&service.InitClusterDEKResponse{
					Version:     1,
					CreatedAt:   time.Now(),
					NodesSynced: 3,
					Algorithm:   "AES-256-GCM",
					KeySize:     256,
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp InitClusterDEKRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(1), resp.Version)
				assert.Equal(t, 3, resp.NodesSynced)
				assert.Equal(t, "AES-256-GCM", resp.Algorithm)
				assert.Equal(t, 256, resp.KeySize)
			},
		},
		{
			name: "initialize with minimal request",
			body: InitClusterDEKRESTRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("InitializeClusterDEK", mock.Anything, mock.Anything).Return(&service.InitClusterDEKResponse{
					Version:     1,
					CreatedAt:   time.Now(),
					NodesSynced: 1,
					Algorithm:   "AES-256-GCM",
					KeySize:     256,
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name: "initialize with empty body",
			body: map[string]interface{}{},
			setupMock: func(m *MockClusterDEKService) {
				m.On("InitializeClusterDEK", mock.Anything, mock.Anything).Return(&service.InitClusterDEKResponse{
					Version:     1,
					CreatedAt:   time.Now(),
					NodesSynced: 1,
					Algorithm:   "AES-256-GCM",
					KeySize:     256,
				}, nil).Once()
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid JSON")
			},
		},
		{
			name: "service error",
			body: InitClusterDEKRESTRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("InitializeClusterDEK", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "InitializeClusterDEK",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
		{
			name: "already initialized error",
			body: InitClusterDEKRESTRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("InitializeClusterDEK", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrAlreadyExists,
					Op:   "InitializeClusterDEK",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/init", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleInitializeClusterDEK(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleInitializeClusterDEK_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body, _ := json.Marshal(InitClusterDEKRESTRequest{
		Algorithm: "AES-256-GCM",
		KeySize:   256,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/init", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleInitializeClusterDEK(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleGetClusterDEKStatus
// =============================================================================

func TestHandleGetClusterDEKStatus(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get status successfully",
			setupMock: func(m *MockClusterDEKService) {
				rotatedAt := time.Now().Add(-24 * time.Hour)
				nextRotation := time.Now().Add(7 * 24 * time.Hour)
				m.On("GetClusterDEKStatus", mock.Anything).Return(&service.ClusterDEKStatusResponse{
					Initialized:            true,
					CurrentVersion:         3,
					CreatedAt:              time.Now().Add(-30 * 24 * time.Hour),
					RotatedAt:              &rotatedAt,
					NextRotation:           &nextRotation,
					NodesSynced:            5,
					TotalNodes:             5,
					Algorithm:              "AES-256-GCM",
					KeySize:                256,
					ReencryptionInProgress: false,
					ReencryptionProgress:   0,
					NodesOutOfSync:         nil,
					PreviousVersions: []service.DEKVersionInfo{
						{
							Version:           2,
							CreatedAt:         time.Now().Add(-60 * 24 * time.Hour),
							EntitiesEncrypted: 0,
							CanDelete:         true,
						},
					},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterDEKStatusRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Initialized)
				assert.Equal(t, uint64(3), resp.CurrentVersion)
				assert.Equal(t, 5, resp.NodesSynced)
				assert.Equal(t, 5, resp.TotalNodes)
				assert.Equal(t, "AES-256-GCM", resp.Algorithm)
				assert.NotZero(t, resp.RotatedAt)
				assert.NotZero(t, resp.NextRotation)
			},
		},
		{
			name: "get status not initialized",
			setupMock: func(m *MockClusterDEKService) {
				m.On("GetClusterDEKStatus", mock.Anything).Return(&service.ClusterDEKStatusResponse{
					Initialized:    false,
					CurrentVersion: 0,
					NodesSynced:    0,
					TotalNodes:     3,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterDEKStatusRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.False(t, resp.Initialized)
				assert.Equal(t, uint64(0), resp.CurrentVersion)
			},
		},
		{
			name: "get status with nodes out of sync",
			setupMock: func(m *MockClusterDEKService) {
				syncedAt := time.Now().Add(-2 * time.Hour)
				m.On("GetClusterDEKStatus", mock.Anything).Return(&service.ClusterDEKStatusResponse{
					Initialized:    true,
					CurrentVersion: 2,
					NodesSynced:    2,
					TotalNodes:     3,
					NodesOutOfSync: []service.NodeDEKInfo{
						{
							NodeID:         3,
							NodeName:       "node-3",
							CurrentVersion: 1,
							LastSyncedAt:   syncedAt,
						},
					},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterDEKStatusRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, 2, resp.NodesSynced)
				assert.Equal(t, 3, resp.TotalNodes)
				assert.Len(t, resp.NodesOutOfSync, 1)
				assert.Equal(t, uint64(3), resp.NodesOutOfSync[0].NodeID)
			},
		},
		{
			name: "service error",
			setupMock: func(m *MockClusterDEKService) {
				m.On("GetClusterDEKStatus", mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "GetClusterDEKStatus",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/dek/status", nil)
			w := httptest.NewRecorder()

			server.HandleGetClusterDEKStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleGetClusterDEKStatus_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/dek/status", nil)
	w := httptest.NewRecorder()

	server.HandleGetClusterDEKStatus(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleRotateClusterDEK
// =============================================================================

func TestHandleRotateClusterDEK(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "rotate cluster DEK successfully",
			body: RotateClusterDEKRESTRequest{
				Reason:                "scheduled rotation",
				AutoStartReencryption: true,
				Metadata:              map[string]string{"operator": "admin"},
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RotateClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateClusterDEKRequest) bool {
					return req.Reason == "scheduled rotation" && req.AutoStartReencryption
				})).Return(&service.RotateClusterDEKResponse{
					NewVersion:                2,
					OldVersion:                1,
					RotatedAt:                 time.Now(),
					RotationID:                "rot-1-2",
					NodesSynced:               3,
					ReencryptionStarted:       true,
					EstimatedReencryptionTime: time.Hour,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RotateClusterDEKRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(2), resp.NewVersion)
				assert.Equal(t, uint64(1), resp.OldVersion)
				assert.Equal(t, "rot-1-2", resp.RotationID)
				assert.True(t, resp.ReencryptionStarted)
				assert.Equal(t, int64(3600), resp.EstimatedReencryptionTime)
			},
		},
		{
			name: "rotate with scheduled time",
			body: RotateClusterDEKRESTRequest{
				Reason:        "maintenance window",
				ScheduledTime: time.Now().Add(24 * time.Hour).Unix(),
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RotateClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateClusterDEKRequest) bool {
					return req.ScheduledTime != nil
				})).Return(&service.RotateClusterDEKResponse{
					NewVersion:  2,
					OldVersion:  1,
					RotatedAt:   time.Now(),
					RotationID:  "rot-1-2",
					NodesSynced: 3,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "rotate with empty body",
			body: map[string]interface{}{},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RotateClusterDEK", mock.Anything, mock.Anything).Return(&service.RotateClusterDEKResponse{
					NewVersion:  2,
					OldVersion:  1,
					RotatedAt:   time.Now(),
					RotationID:  "rot-1-2",
					NodesSynced: 3,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error - rotation in progress",
			body: RotateClusterDEKRESTRequest{
				Reason: "emergency rotation",
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RotateClusterDEK", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrConflict,
					Op:   "RotateClusterDEK",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/rotate", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleRotateClusterDEK(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleRotateClusterDEK_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body, _ := json.Marshal(RotateClusterDEKRESTRequest{Reason: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/rotate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleRotateClusterDEK(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleSyncNodeDEK
// =============================================================================

func TestHandleSyncNodeDEK(t *testing.T) {
	tests := []struct {
		name           string
		nodeID         string
		body           interface{}
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:   "sync node DEK successfully",
			nodeID: "12345",
			body: SyncNodeDEKRESTRequest{
				CurrentVersion: 1,
				ForceResync:    false,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("SyncNodeDEK", mock.Anything, mock.MatchedBy(func(req *service.SyncNodeDEKRequest) bool {
					return req.NodeID == 12345 && req.CurrentVersion == 1
				})).Return(&service.SyncNodeDEKResponse{
					NodeID:         12345,
					VersionsSynced: []uint64{2, 3},
					CurrentVersion: 3,
					SyncedAt:       time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp SyncNodeDEKRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(12345), resp.NodeID)
				assert.Equal(t, uint64(3), resp.CurrentVersion)
				assert.Contains(t, resp.VersionsSynced, uint64(2))
				assert.Contains(t, resp.VersionsSynced, uint64(3))
			},
		},
		{
			name:   "sync with force resync",
			nodeID: "456",
			body: SyncNodeDEKRESTRequest{
				ForceResync: true,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("SyncNodeDEK", mock.Anything, mock.MatchedBy(func(req *service.SyncNodeDEKRequest) bool {
					return req.NodeID == 456 && req.ForceResync
				})).Return(&service.SyncNodeDEKResponse{
					NodeID:         456,
					VersionsSynced: []uint64{3},
					CurrentVersion: 3,
					SyncedAt:       time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "sync with empty body",
			nodeID: "789",
			body:   map[string]interface{}{},
			setupMock: func(m *MockClusterDEKService) {
				m.On("SyncNodeDEK", mock.Anything, mock.MatchedBy(func(req *service.SyncNodeDEKRequest) bool {
					return req.NodeID == 789
				})).Return(&service.SyncNodeDEKResponse{
					NodeID:         789,
					VersionsSynced: []uint64{3},
					CurrentVersion: 3,
					SyncedAt:       time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing node ID",
			nodeID:         "",
			body:           SyncNodeDEKRESTRequest{},
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "node ID is required")
			},
		},
		{
			name:           "invalid node ID format",
			nodeID:         "invalid",
			body:           SyncNodeDEKRESTRequest{},
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid node ID format")
			},
		},
		{
			name:           "invalid JSON body",
			nodeID:         "123",
			body:           "invalid-json",
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:   "service error",
			nodeID: "999",
			body:   SyncNodeDEKRESTRequest{},
			setupMock: func(m *MockClusterDEKService) {
				m.On("SyncNodeDEK", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "SyncNodeDEK",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/sync/"+tt.nodeID, bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("nodeId", tt.nodeID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleSyncNodeDEK(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleSyncNodeDEK_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body, _ := json.Marshal(SyncNodeDEKRESTRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/sync/123", bytes.NewReader(body))
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("nodeId", "123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleSyncNodeDEK(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleGetNodeDEKStatus
// =============================================================================

func TestHandleGetNodeDEKStatus(t *testing.T) {
	tests := []struct {
		name           string
		nodeID         string
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:   "get node DEK status successfully",
			nodeID: "12345",
			setupMock: func(m *MockClusterDEKService) {
				m.On("GetNodeDEKStatus", mock.Anything, mock.MatchedBy(func(req *service.GetNodeDEKStatusRequest) bool {
					return req.NodeID == 12345
				})).Return(&service.GetNodeDEKStatusResponse{
					NodeID:            12345,
					NodeName:          "node-1",
					CurrentVersion:    3,
					LastSyncedAt:      time.Now().Add(-1 * time.Hour),
					Status:            service.NodeDEKSynced,
					VersionsAvailable: []uint64{2, 3},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp NodeDEKStatusRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, uint64(12345), resp.NodeID)
				assert.Equal(t, "node-1", resp.NodeName)
				assert.Equal(t, uint64(3), resp.CurrentVersion)
				assert.Equal(t, "synced", resp.Status)
				assert.NotZero(t, resp.LastSyncedAt)
			},
		},
		{
			name:   "get status with error",
			nodeID: "456",
			setupMock: func(m *MockClusterDEKService) {
				m.On("GetNodeDEKStatus", mock.Anything, mock.Anything).Return(&service.GetNodeDEKStatusResponse{
					NodeID:         456,
					CurrentVersion: 1,
					Status:         service.NodeDEKSyncFailed,
					Error:          "network timeout",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp NodeDEKStatusRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "sync_failed", resp.Status)
				assert.Equal(t, "network timeout", resp.Error)
			},
		},
		{
			name:           "missing node ID",
			nodeID:         "",
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "node ID is required")
			},
		},
		{
			name:           "invalid node ID format",
			nodeID:         "abc",
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid node ID format")
			},
		},
		{
			name:   "node not found error",
			nodeID: "999",
			setupMock: func(m *MockClusterDEKService) {
				m.On("GetNodeDEKStatus", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "GetNodeDEKStatus",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/dek/nodes/"+tt.nodeID, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("nodeId", tt.nodeID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetNodeDEKStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleGetNodeDEKStatus_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/dek/nodes/123", nil)
	w := httptest.NewRecorder()

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("nodeId", "123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleGetNodeDEKStatus(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleStartClusterReencryption
// =============================================================================

func TestHandleStartClusterReencryption(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "start reencryption successfully",
			body: StartClusterReencryptionRESTRequest{
				FromVersion: 1,
				ToVersion:   2,
				BatchSize:   100,
				Concurrency: 4,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
					return req.FromVersion == 1 && req.ToVersion == 2 && req.BatchSize == 100
				})).Return(&service.StartReencryptionResponse{
					OperationID:         "reenc-1-2-123456",
					Started:             true,
					StartedAt:           time.Now(),
					TotalEntities:       10000,
					EstimatedCompletion: time.Now().Add(2 * time.Hour),
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp StartClusterReencryptionRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "reenc-1-2-123456", resp.OperationID)
				assert.True(t, resp.Started)
				assert.Equal(t, int64(10000), resp.TotalEntities)
			},
		},
		{
			name: "start with minimal request",
			body: StartClusterReencryptionRESTRequest{},
			setupMock: func(m *MockClusterDEKService) {
				m.On("StartReencryption", mock.Anything, mock.Anything).Return(&service.StartReencryptionResponse{
					OperationID: "reenc-0-0-123456",
					Started:     true,
					StartedAt:   time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
		},
		{
			name: "start with throttle rate",
			body: StartClusterReencryptionRESTRequest{
				FromVersion:  1,
				ToVersion:    2,
				ThrottleRate: 1000,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
					return req.ThrottleRate == 1000
				})).Return(&service.StartReencryptionResponse{
					OperationID: "reenc-1-2-123456",
					Started:     true,
					StartedAt:   time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error - already running",
			body: StartClusterReencryptionRESTRequest{
				FromVersion: 1,
				ToVersion:   2,
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("StartReencryption", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrConflict,
					Op:   "StartReencryption",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/reencryption/start", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleStartClusterReencryption(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleStartClusterReencryption_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body, _ := json.Marshal(StartClusterReencryptionRESTRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/reencryption/start", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleStartClusterReencryption(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleGetClusterReencryptionStatus
// =============================================================================

func TestHandleGetClusterReencryptionStatus(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "get reencryption status successfully",
			setupMock: func(m *MockClusterDEKService) {
				completedAt := time.Now().Add(2 * time.Hour)
				m.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
					OperationID:         "reenc-1-2-123456",
					State:               service.ReencryptionRunning,
					StartedAt:           time.Now().Add(-30 * time.Minute),
					EstimatedCompletion: &completedAt,
					Progress:            45.5,
					TotalEntities:       10000,
					ProcessedEntities:   4550,
					FailedEntities:      5,
					ThroughputRate:      150.5,
					NodeProgress: []service.NodeReencryptionProgress{
						{
							NodeID:            1,
							NodeName:          "node-1",
							Progress:          50.0,
							ProcessedEntities: 2500,
							TotalEntities:     5000,
							ThroughputRate:    80.0,
							LastUpdatedAt:     time.Now(),
						},
					},
					Errors: []service.ReencryptionError{
						{
							Timestamp: time.Now().Add(-10 * time.Minute),
							NodeID:    1,
							EntityID:  "entity-123",
							Error:     "encryption failed",
							Retried:   true,
						},
					},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterReencryptionStatusRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "reenc-1-2-123456", resp.OperationID)
				assert.Equal(t, "running", resp.State)
				assert.InDelta(t, 45.5, resp.Progress, 0.1)
				assert.Equal(t, int64(10000), resp.TotalEntities)
				assert.Equal(t, int64(4550), resp.ProcessedEntities)
				assert.Len(t, resp.NodeProgress, 1)
				assert.Len(t, resp.Errors, 1)
			},
		},
		{
			name: "get completed reencryption status",
			setupMock: func(m *MockClusterDEKService) {
				completedAt := time.Now().Add(-10 * time.Minute)
				m.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
					OperationID:       "reenc-1-2-123456",
					State:             service.ReencryptionCompleted,
					StartedAt:         time.Now().Add(-2 * time.Hour),
					CompletedAt:       &completedAt,
					Progress:          100,
					TotalEntities:     10000,
					ProcessedEntities: 10000,
					FailedEntities:    0,
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp ClusterReencryptionStatusRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, "completed", resp.State)
				assert.Equal(t, float64(100), resp.Progress)
				assert.NotZero(t, resp.CompletedAt)
			},
		},
		{
			name: "service error",
			setupMock: func(m *MockClusterDEKService) {
				m.On("GetReencryptionStatus", mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "GetReencryptionStatus",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/dek/reencryption/status", nil)
			w := httptest.NewRecorder()

			server.HandleGetClusterReencryptionStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleGetClusterReencryptionStatus_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cluster/dek/reencryption/status", nil)
	w := httptest.NewRecorder()

	server.HandleGetClusterReencryptionStatus(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleCancelClusterReencryption
// =============================================================================

func TestHandleCancelClusterReencryption(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "cancel reencryption successfully",
			setupMock: func(m *MockClusterDEKService) {
				m.On("CancelReencryption", mock.Anything).Return(nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]interface{}
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.Equal(t, true, resp["success"])
				assert.NotNil(t, resp["timestamp"])
			},
		},
		{
			name: "cancel when no reencryption running",
			setupMock: func(m *MockClusterDEKService) {
				m.On("CancelReencryption", mock.Anything).Return(&errors.DragonError{
					Code: errors.ErrNotFound,
					Op:   "CancelReencryption",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			setupMock: func(m *MockClusterDEKService) {
				m.On("CancelReencryption", mock.Anything).Return(&errors.DragonError{
					Code: errors.ErrInternal,
					Op:   "CancelReencryption",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/dek/reencryption", nil)
			w := httptest.NewRecorder()

			server.HandleCancelClusterReencryption(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleCancelClusterReencryption_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/cluster/dek/reencryption", nil)
	w := httptest.NewRecorder()

	server.HandleCancelClusterReencryption(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// =============================================================================
// TestHandleRecoverDEK
// =============================================================================

func TestHandleRecoverDEK(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		setupMock      func(*MockClusterDEKService)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "recover DEK with recovery keys",
			body: RecoverDEKRESTRequest{
				RecoveryKeys: []string{
					base64.StdEncoding.EncodeToString([]byte("key-part-1")),
					base64.StdEncoding.EncodeToString([]byte("key-part-2")),
					base64.StdEncoding.EncodeToString([]byte("key-part-3")),
				},
				RecoveryMethod: "recovery_keys",
				Reason:         "disaster recovery",
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
					return req.RecoveryMethod == service.RecoveryMethodKeys &&
						len(req.RecoveryKeys) == 3 &&
						req.Reason == "disaster recovery"
				})).Return(&service.RecoverDEKResponse{
					Success:          true,
					RecoveredVersion: 3,
					RecoveredAt:      time.Now(),
					NodesSynced:      5,
					AuditID:          "recovery-123456",
					Warnings:         []string{"some nodes were slow to sync"},
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp RecoverDEKRESTResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Success)
				assert.Equal(t, uint64(3), resp.RecoveredVersion)
				assert.Equal(t, 5, resp.NodesSynced)
				assert.Equal(t, "recovery-123456", resp.AuditID)
				assert.Len(t, resp.Warnings, 1)
			},
		},
		{
			name: "recover DEK from backup",
			body: RecoverDEKRESTRequest{
				BackupLocation: "s3://my-bucket/dek-backup.enc",
				RecoveryMethod: "backup",
				Reason:         "cluster rebuild",
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
					return req.RecoveryMethod == service.RecoveryMethodBackup &&
						req.BackupLocation == "s3://my-bucket/dek-backup.enc"
				})).Return(&service.RecoverDEKResponse{
					Success:          true,
					RecoveredVersion: 2,
					RecoveredAt:      time.Now(),
					NodesSynced:      3,
					AuditID:          "recovery-654321",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "recover DEK with manual key",
			body: RecoverDEKRESTRequest{
				ManualKey:      base64.StdEncoding.EncodeToString([]byte("manual-dek-key-32-bytes-long!!!!")),
				RecoveryMethod: "manual",
				Reason:         "break-glass procedure",
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
					return req.RecoveryMethod == service.RecoveryMethodManual &&
						len(req.ManualKey) > 0
				})).Return(&service.RecoverDEKResponse{
					Success:          true,
					RecoveredVersion: 1,
					RecoveredAt:      time.Now(),
					NodesSynced:      1,
					AuditID:          "recovery-manual-123",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing recovery method",
			body:           RecoverDEKRESTRequest{Reason: "test"},
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "recovery_method is required")
			},
		},
		{
			name: "missing reason",
			body: RecoverDEKRESTRequest{
				RecoveryMethod: "backup",
				BackupLocation: "s3://bucket/backup",
			},
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "reason is required")
			},
		},
		{
			name: "invalid base64 in recovery keys",
			body: RecoverDEKRESTRequest{
				RecoveryKeys:   []string{"not-valid-base64!!!"},
				RecoveryMethod: "recovery_keys",
				Reason:         "test",
			},
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid base64 encoding in recovery_keys")
			},
		},
		{
			name: "invalid base64 in manual key",
			body: RecoverDEKRESTRequest{
				ManualKey:      "not-valid-base64!!!",
				RecoveryMethod: "manual",
				Reason:         "test",
			},
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var errResp ErrorResponse
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				assert.Contains(t, errResp.Error, "invalid base64 encoding in manual_key")
			},
		},
		{
			name:           "invalid JSON body",
			body:           "invalid-json",
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty JSON body",
			body:           map[string]interface{}{},
			setupMock:      func(m *MockClusterDEKService) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error - threshold not met",
			body: RecoverDEKRESTRequest{
				RecoveryKeys: []string{
					base64.StdEncoding.EncodeToString([]byte("key-1")),
				},
				RecoveryMethod: "recovery_keys",
				Reason:         "test",
			},
			setupMock: func(m *MockClusterDEKService) {
				m.On("RecoverDEK", mock.Anything, mock.Anything).Return(nil, &errors.DragonError{
					Code: errors.ErrUnauthorized,
					Op:   "RecoverDEK",
					Err:  assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockClusterDEK, cleanup := setupMockServerWithClusterDEK(t)
			defer cleanup()
			tt.setupMock(mockClusterDEK)

			var bodyBytes []byte
			if str, ok := tt.body.(string); ok {
				bodyBytes = []byte(str)
			} else {
				bodyBytes, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/recover", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.HandleRecoverDEK(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
			mockClusterDEK.AssertExpectations(t)
		})
	}
}

func TestHandleRecoverDEK_ServiceNotAvailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	body, _ := json.Marshal(RecoverDEKRESTRequest{
		RecoveryMethod: "backup",
		BackupLocation: "s3://bucket/backup",
		Reason:         "test",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cluster/dek/recover", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleRecoverDEK(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
