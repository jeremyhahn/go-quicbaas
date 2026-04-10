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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// mockQuicBaaSWithClusterDEK wraps MockQuicBaaS and provides a ClusterDEKService.
type mockQuicBaaSWithClusterDEK struct {
	*testutil.MockQuicBaaS
	clusterDEKSvc *testutil.MockClusterDEKService
}

func (m *mockQuicBaaSWithClusterDEK) ClusterDEK() service.ClusterDEKService {
	return m.clusterDEKSvc
}

func createTestServerWithClusterDEKService(t *testing.T) (*Server, *testutil.MockClusterDEKService) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	mockClusterDEK := testutil.NewMockClusterDEKService()

	// Create a wrapper that provides ClusterDEK
	wrapper := &mockQuicBaaSWithClusterDEK{
		MockQuicBaaS:  mockSvc,
		clusterDEKSvc: mockClusterDEK,
	}

	srv, err := NewServer(wrapper, log, 1, "test-v1.0.0")
	require.NoError(t, err)
	return srv, mockClusterDEK
}

// =============================================================================
// InitializeClusterDEK Tests
// =============================================================================

func TestInitializeClusterDEK(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful initialization", func(t *testing.T) {
		mockClusterDEK.On("InitializeClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.InitClusterDEKRequest) bool {
			return req.Algorithm == "AES-256-GCM" && req.KeySize == 256
		})).Return(&service.InitClusterDEKResponse{
			Version:     1,
			CreatedAt:   time.Now(),
			NodesSynced: 3,
			Algorithm:   "AES-256-GCM",
			KeySize:     256,
		}, nil).Once()

		resp, err := srv.InitializeClusterDEK(ctx, &pb.InitClusterDEKRequest{
			Algorithm: "AES-256-GCM",
			KeySize:   256,
			Metadata:  map[string]string{"env": "test"},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(1), resp.Version)
		assert.Equal(t, int32(3), resp.NodesSynced)
		assert.Equal(t, "AES-256-GCM", resp.Algorithm)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("initialization with rotation policy", func(t *testing.T) {
		mockClusterDEK.On("InitializeClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.InitClusterDEKRequest) bool {
			return req.RotationPolicy != nil && req.RotationPolicy.Enabled
		})).Return(&service.InitClusterDEKResponse{
			Version:     1,
			CreatedAt:   time.Now(),
			NodesSynced: 3,
			Algorithm:   "AES-256-GCM",
			KeySize:     256,
		}, nil).Once()

		resp, err := srv.InitializeClusterDEK(ctx, &pb.InitClusterDEKRequest{
			Algorithm: "AES-256-GCM",
			KeySize:   256,
			RotationPolicy: &pb.DEKRotationPolicy{
				Enabled:          true,
				RotationInterval: durationpb.New(24 * time.Hour),
				AutoReencrypt:    true,
			},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.InitializeClusterDEK(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty algorithm", func(t *testing.T) {
		_, err := srv.InitializeClusterDEK(ctx, &pb.InitClusterDEKRequest{
			Algorithm: "",
			KeySize:   256,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("invalid key size", func(t *testing.T) {
		_, err := srv.InitializeClusterDEK(ctx, &pb.InitClusterDEKRequest{
			Algorithm: "AES-256-GCM",
			KeySize:   0,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("negative key size", func(t *testing.T) {
		_, err := srv.InitializeClusterDEK(ctx, &pb.InitClusterDEKRequest{
			Algorithm: "AES-256-GCM",
			KeySize:   -1,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("InitializeClusterDEK", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.InitializeClusterDEK(ctx, &pb.InitClusterDEKRequest{
			Algorithm: "AES-256-GCM",
			KeySize:   256,
		})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// GetClusterDEKStatus Tests
// =============================================================================

func TestGetClusterDEKStatus(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful status retrieval", func(t *testing.T) {
		rotatedAt := time.Now().Add(-24 * time.Hour)
		nextRotation := time.Now().Add(7 * 24 * time.Hour)
		deprecatedAt := time.Now().Add(-48 * time.Hour)

		mockClusterDEK.On("GetClusterDEKStatus", mock.Anything).Return(&service.ClusterDEKStatusResponse{
			Initialized:            true,
			CurrentVersion:         2,
			CreatedAt:              time.Now().Add(-48 * time.Hour),
			RotatedAt:              &rotatedAt,
			NextRotation:           &nextRotation,
			NodesSynced:            3,
			TotalNodes:             3,
			Algorithm:              "AES-256-GCM",
			KeySize:                256,
			ReencryptionInProgress: false,
			ReencryptionProgress:   0,
			NodesOutOfSync: []service.NodeDEKInfo{
				{NodeID: 4, NodeName: "node-4", CurrentVersion: 1, LastSyncedAt: time.Now().Add(-1 * time.Hour)},
			},
			PreviousVersions: []service.DEKVersionInfo{
				{Version: 1, CreatedAt: time.Now().Add(-72 * time.Hour), DeprecatedAt: &deprecatedAt, EntitiesEncrypted: 100, CanDelete: false},
			},
		}, nil).Once()

		resp, err := srv.GetClusterDEKStatus(ctx, &pb.GetClusterDEKStatusRequest{})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Initialized)
		assert.Equal(t, uint64(2), resp.CurrentVersion)
		assert.Equal(t, int32(3), resp.NodesSynced)
		assert.Equal(t, int32(3), resp.TotalNodes)
		assert.NotNil(t, resp.RotatedAt)
		assert.NotNil(t, resp.NextRotation)
		assert.Len(t, resp.NodesOutOfSync, 1)
		assert.Len(t, resp.PreviousVersions, 1)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.GetClusterDEKStatus(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("GetClusterDEKStatus", mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.GetClusterDEKStatus(ctx, &pb.GetClusterDEKStatusRequest{})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// RotateClusterDEK Tests
// =============================================================================

func TestRotateClusterDEK(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful rotation", func(t *testing.T) {
		mockClusterDEK.On("RotateClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateClusterDEKRequest) bool {
			return req.Reason == "scheduled rotation"
		})).Return(&service.RotateClusterDEKResponse{
			NewVersion:                2,
			OldVersion:                1,
			RotatedAt:                 time.Now(),
			RotationID:                "rot-123",
			NodesSynced:               3,
			ReencryptionStarted:       true,
			EstimatedReencryptionTime: 2 * time.Hour,
		}, nil).Once()

		resp, err := srv.RotateClusterDEK(ctx, &pb.RotateClusterDEKRequest{
			Reason:                "scheduled rotation",
			AutoStartReencryption: true,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(2), resp.NewVersion)
		assert.Equal(t, uint64(1), resp.OldVersion)
		assert.Equal(t, "rot-123", resp.RotationId)
		assert.True(t, resp.ReencryptionStarted)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("rotation with scheduled time", func(t *testing.T) {
		scheduledTime := time.Now().Add(1 * time.Hour)
		mockClusterDEK.On("RotateClusterDEK", mock.Anything, mock.MatchedBy(func(req *service.RotateClusterDEKRequest) bool {
			return req.ScheduledTime != nil
		})).Return(&service.RotateClusterDEKResponse{
			NewVersion:          2,
			OldVersion:          1,
			RotatedAt:           time.Now(),
			RotationID:          "rot-456",
			NodesSynced:         3,
			ReencryptionStarted: false,
		}, nil).Once()

		resp, err := srv.RotateClusterDEK(ctx, &pb.RotateClusterDEKRequest{
			Reason:        "planned maintenance",
			ScheduledTime: timestamppb.New(scheduledTime),
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.RotateClusterDEK(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty reason", func(t *testing.T) {
		_, err := srv.RotateClusterDEK(ctx, &pb.RotateClusterDEKRequest{
			Reason: "",
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("RotateClusterDEK", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.RotateClusterDEK(ctx, &pb.RotateClusterDEKRequest{
			Reason: "test rotation",
		})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// SyncNodeDEK Tests
// =============================================================================

func TestSyncNodeDEK(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful sync", func(t *testing.T) {
		mockClusterDEK.On("SyncNodeDEK", mock.Anything, mock.MatchedBy(func(req *service.SyncNodeDEKRequest) bool {
			return req.NodeID == 2
		})).Return(&service.SyncNodeDEKResponse{
			NodeID:         2,
			VersionsSynced: []uint64{1, 2},
			CurrentVersion: 2,
			SyncedAt:       time.Now(),
		}, nil).Once()

		resp, err := srv.SyncNodeDEK(ctx, &pb.SyncNodeDEKRequest{
			NodeId:         2,
			CurrentVersion: 1,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(2), resp.NodeId)
		assert.Equal(t, uint64(2), resp.CurrentVersion)
		assert.Len(t, resp.VersionsSynced, 2)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("force resync", func(t *testing.T) {
		mockClusterDEK.On("SyncNodeDEK", mock.Anything, mock.MatchedBy(func(req *service.SyncNodeDEKRequest) bool {
			return req.ForceResync
		})).Return(&service.SyncNodeDEKResponse{
			NodeID:         2,
			VersionsSynced: []uint64{2},
			CurrentVersion: 2,
			SyncedAt:       time.Now(),
		}, nil).Once()

		resp, err := srv.SyncNodeDEK(ctx, &pb.SyncNodeDEKRequest{
			NodeId:         2,
			CurrentVersion: 2,
			ForceResync:    true,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.SyncNodeDEK(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("zero node id", func(t *testing.T) {
		_, err := srv.SyncNodeDEK(ctx, &pb.SyncNodeDEKRequest{
			NodeId: 0,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("SyncNodeDEK", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.SyncNodeDEK(ctx, &pb.SyncNodeDEKRequest{
			NodeId: 2,
		})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// GetNodeDEKStatus Tests
// =============================================================================

func TestGetNodeDEKStatus(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful status retrieval", func(t *testing.T) {
		mockClusterDEK.On("GetNodeDEKStatus", mock.Anything, mock.MatchedBy(func(req *service.GetNodeDEKStatusRequest) bool {
			return req.NodeID == 2
		})).Return(&service.GetNodeDEKStatusResponse{
			NodeID:            2,
			NodeName:          "node-2",
			CurrentVersion:    2,
			LastSyncedAt:      time.Now(),
			Status:            service.NodeDEKSynced,
			VersionsAvailable: []uint64{1, 2},
		}, nil).Once()

		resp, err := srv.GetNodeDEKStatus(ctx, &pb.GetNodeDEKStatusRequest{
			NodeId: 2,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(2), resp.NodeId)
		assert.Equal(t, "node-2", resp.NodeName)
		assert.Equal(t, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_SYNCED, resp.Status)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("node out of date", func(t *testing.T) {
		mockClusterDEK.On("GetNodeDEKStatus", mock.Anything, mock.Anything).Return(&service.GetNodeDEKStatusResponse{
			NodeID:            3,
			NodeName:          "node-3",
			CurrentVersion:    1,
			LastSyncedAt:      time.Now().Add(-24 * time.Hour),
			Status:            service.NodeDEKOutOfDate,
			VersionsAvailable: []uint64{1},
		}, nil).Once()

		resp, err := srv.GetNodeDEKStatus(ctx, &pb.GetNodeDEKStatusRequest{
			NodeId: 3,
		})

		require.NoError(t, err)
		assert.Equal(t, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_OUT_OF_DATE, resp.Status)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("node missing dek", func(t *testing.T) {
		mockClusterDEK.On("GetNodeDEKStatus", mock.Anything, mock.Anything).Return(&service.GetNodeDEKStatusResponse{
			NodeID:   4,
			NodeName: "node-4",
			Status:   service.NodeDEKMissing,
		}, nil).Once()

		resp, err := srv.GetNodeDEKStatus(ctx, &pb.GetNodeDEKStatusRequest{
			NodeId: 4,
		})

		require.NoError(t, err)
		assert.Equal(t, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_MISSING, resp.Status)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("node sync failed", func(t *testing.T) {
		mockClusterDEK.On("GetNodeDEKStatus", mock.Anything, mock.Anything).Return(&service.GetNodeDEKStatusResponse{
			NodeID:   5,
			NodeName: "node-5",
			Status:   service.NodeDEKSyncFailed,
			Error:    "network timeout",
		}, nil).Once()

		resp, err := srv.GetNodeDEKStatus(ctx, &pb.GetNodeDEKStatusRequest{
			NodeId: 5,
		})

		require.NoError(t, err)
		assert.Equal(t, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_SYNC_FAILED, resp.Status)
		assert.Equal(t, "network timeout", resp.Error)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.GetNodeDEKStatus(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("zero node id", func(t *testing.T) {
		_, err := srv.GetNodeDEKStatus(ctx, &pb.GetNodeDEKStatusRequest{
			NodeId: 0,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("GetNodeDEKStatus", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.GetNodeDEKStatus(ctx, &pb.GetNodeDEKStatusRequest{
			NodeId: 2,
		})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// ClusterStartReencryption Tests
// =============================================================================

func TestClusterStartReencryption(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful start", func(t *testing.T) {
		mockClusterDEK.On("StartReencryption", mock.Anything, mock.Anything).Return(&service.StartReencryptionResponse{
			OperationID:         "reenc-123",
			Path:                "/",
			Started:             true,
			EstimatedItems:      1000,
			StartedAt:           time.Now(),
			TotalEntities:       1000,
			EstimatedCompletion: time.Now().Add(2 * time.Hour),
		}, nil).Once()

		resp, err := srv.ClusterStartReencryption(ctx, &pb.ClusterStartReencryptionRequest{
			FromVersion: 1,
			ToVersion:   2,
			BatchSize:   100,
			Concurrency: 4,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "reenc-123", resp.OperationId)
		assert.True(t, resp.Started)
		assert.Equal(t, int32(1000), resp.EstimatedItems)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("with throttle rate", func(t *testing.T) {
		mockClusterDEK.On("StartReencryption", mock.Anything, mock.MatchedBy(func(req *service.StartReencryptionRequest) bool {
			return req.ThrottleRate == 500
		})).Return(&service.StartReencryptionResponse{
			OperationID:   "reenc-456",
			Started:       true,
			StartedAt:     time.Now(),
			TotalEntities: 500,
		}, nil).Once()

		resp, err := srv.ClusterStartReencryption(ctx, &pb.ClusterStartReencryptionRequest{
			FromVersion:  1,
			ToVersion:    2,
			ThrottleRate: 500,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.ClusterStartReencryption(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("StartReencryption", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.ClusterStartReencryption(ctx, &pb.ClusterStartReencryptionRequest{
			FromVersion: 1,
			ToVersion:   2,
		})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// GetReencryptionStatus Tests
// =============================================================================

func TestGetReencryptionStatus(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("running status", func(t *testing.T) {
		completionTime := time.Now().Add(1 * time.Hour)
		mockClusterDEK.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
			OperationID:         "reenc-123",
			State:               service.ReencryptionRunning,
			StartedAt:           time.Now().Add(-30 * time.Minute),
			EstimatedCompletion: &completionTime,
			Progress:            50.0,
			TotalEntities:       1000,
			ProcessedEntities:   500,
			FailedEntities:      5,
			ThroughputRate:      16.7,
			NodeProgress: []service.NodeReencryptionProgress{
				{NodeID: 1, NodeName: "node-1", Progress: 55.0, ProcessedEntities: 275, TotalEntities: 500, ThroughputRate: 8.5, LastUpdatedAt: time.Now()},
				{NodeID: 2, NodeName: "node-2", Progress: 45.0, ProcessedEntities: 225, TotalEntities: 500, ThroughputRate: 8.2, LastUpdatedAt: time.Now()},
			},
			Errors: []service.ReencryptionError{
				{Timestamp: time.Now().Add(-10 * time.Minute), NodeID: 1, EntityID: "entity-123", Error: "decryption failed", Retried: true},
			},
		}, nil).Once()

		resp, err := srv.GetReencryptionStatus(ctx, &pb.GetReencryptionStatusRequest{})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "reenc-123", resp.OperationId)
		assert.Equal(t, pb.ReencryptionState_REENCRYPTION_STATE_RUNNING, resp.State)
		assert.Equal(t, 50.0, resp.Progress)
		assert.Len(t, resp.NodeProgress, 2)
		assert.Len(t, resp.Errors, 1)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("completed status", func(t *testing.T) {
		completedAt := time.Now().Add(-10 * time.Minute)
		mockClusterDEK.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
			OperationID:       "reenc-456",
			State:             service.ReencryptionCompleted,
			StartedAt:         time.Now().Add(-2 * time.Hour),
			CompletedAt:       &completedAt,
			Progress:          100.0,
			TotalEntities:     1000,
			ProcessedEntities: 1000,
			FailedEntities:    0,
		}, nil).Once()

		resp, err := srv.GetReencryptionStatus(ctx, &pb.GetReencryptionStatusRequest{})

		require.NoError(t, err)
		assert.Equal(t, pb.ReencryptionState_REENCRYPTION_STATE_COMPLETED, resp.State)
		assert.NotNil(t, resp.CompletedAt)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("paused status", func(t *testing.T) {
		mockClusterDEK.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
			OperationID: "reenc-789",
			State:       service.ReencryptionPaused,
			StartedAt:   time.Now().Add(-1 * time.Hour),
			Progress:    25.0,
		}, nil).Once()

		resp, err := srv.GetReencryptionStatus(ctx, &pb.GetReencryptionStatusRequest{})

		require.NoError(t, err)
		assert.Equal(t, pb.ReencryptionState_REENCRYPTION_STATE_PAUSED, resp.State)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("failed status", func(t *testing.T) {
		mockClusterDEK.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
			OperationID: "reenc-fail",
			State:       service.ReencryptionFailed,
			StartedAt:   time.Now().Add(-30 * time.Minute),
			Progress:    10.0,
		}, nil).Once()

		resp, err := srv.GetReencryptionStatus(ctx, &pb.GetReencryptionStatusRequest{})

		require.NoError(t, err)
		assert.Equal(t, pb.ReencryptionState_REENCRYPTION_STATE_FAILED, resp.State)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("cancelled status", func(t *testing.T) {
		mockClusterDEK.On("GetReencryptionStatus", mock.Anything).Return(&service.ReencryptionStatusResponse{
			OperationID: "reenc-cancel",
			State:       service.ReencryptionCancelled,
			StartedAt:   time.Now().Add(-20 * time.Minute),
			Progress:    30.0,
		}, nil).Once()

		resp, err := srv.GetReencryptionStatus(ctx, &pb.GetReencryptionStatusRequest{})

		require.NoError(t, err)
		assert.Equal(t, pb.ReencryptionState_REENCRYPTION_STATE_CANCELLED, resp.State)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.GetReencryptionStatus(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("GetReencryptionStatus", mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.GetReencryptionStatus(ctx, &pb.GetReencryptionStatusRequest{})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// CancelReencryption Tests
// =============================================================================

func TestCancelReencryption(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful cancellation", func(t *testing.T) {
		mockClusterDEK.On("CancelReencryption", mock.Anything).Return(nil).Once()

		resp, err := srv.CancelReencryption(ctx, &pb.CancelReencryptionRequest{
			OperationId: "reenc-123",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Success)
		assert.Equal(t, "reenc-123", resp.OperationId)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.CancelReencryption(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("CancelReencryption", mock.Anything).
			Return(assert.AnError).Once()

		_, err := srv.CancelReencryption(ctx, &pb.CancelReencryptionRequest{
			OperationId: "reenc-456",
		})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// RecoverDEK Tests
// =============================================================================

func TestRecoverDEK(t *testing.T) {
	srv, mockClusterDEK := createTestServerWithClusterDEKService(t)
	ctx := context.Background()

	t.Run("successful recovery with keys", func(t *testing.T) {
		mockClusterDEK.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
			return req.RecoveryMethod == service.RecoveryMethodKeys
		})).Return(&service.RecoverDEKResponse{
			Success:          true,
			RecoveredVersion: 2,
			RecoveredAt:      time.Now(),
			NodesSynced:      3,
			AuditID:          "audit-123",
		}, nil).Once()

		resp, err := srv.RecoverDEK(ctx, &pb.RecoverDEKRequest{
			RecoveryMethod: pb.RecoveryMethod_RECOVERY_METHOD_KEYS,
			RecoveryKeys:   [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")},
			Reason:         "disaster recovery",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Success)
		assert.Equal(t, uint64(2), resp.RecoveredVersion)
		assert.Equal(t, "audit-123", resp.AuditId)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("recovery with backup", func(t *testing.T) {
		mockClusterDEK.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
			return req.RecoveryMethod == service.RecoveryMethodBackup
		})).Return(&service.RecoverDEKResponse{
			Success:          true,
			RecoveredVersion: 1,
			RecoveredAt:      time.Now(),
			NodesSynced:      3,
			AuditID:          "audit-456",
			Warnings:         []string{"backup is 2 days old"},
		}, nil).Once()

		resp, err := srv.RecoverDEK(ctx, &pb.RecoverDEKRequest{
			RecoveryMethod: pb.RecoveryMethod_RECOVERY_METHOD_BACKUP,
			BackupLocation: "s3://bucket/backup/dek.enc",
			Reason:         "node failure recovery",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Len(t, resp.Warnings, 1)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("recovery with manual key", func(t *testing.T) {
		mockClusterDEK.On("RecoverDEK", mock.Anything, mock.MatchedBy(func(req *service.RecoverDEKRequest) bool {
			return req.RecoveryMethod == service.RecoveryMethodManual
		})).Return(&service.RecoverDEKResponse{
			Success:          true,
			RecoveredVersion: 3,
			RecoveredAt:      time.Now(),
			NodesSynced:      3,
			AuditID:          "audit-789",
		}, nil).Once()

		resp, err := srv.RecoverDEK(ctx, &pb.RecoverDEKRequest{
			RecoveryMethod: pb.RecoveryMethod_RECOVERY_METHOD_MANUAL,
			ManualKey:      []byte("manual-dek-key"),
			Reason:         "break glass emergency",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
		mockClusterDEK.AssertExpectations(t)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := srv.RecoverDEK(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("empty reason", func(t *testing.T) {
		_, err := srv.RecoverDEK(ctx, &pb.RecoverDEKRequest{
			RecoveryMethod: pb.RecoveryMethod_RECOVERY_METHOD_KEYS,
			RecoveryKeys:   [][]byte{[]byte("key1")},
			Reason:         "",
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("unspecified recovery method", func(t *testing.T) {
		_, err := srv.RecoverDEK(ctx, &pb.RecoverDEKRequest{
			RecoveryMethod: pb.RecoveryMethod_RECOVERY_METHOD_UNSPECIFIED,
			Reason:         "test",
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("service error", func(t *testing.T) {
		mockClusterDEK.On("RecoverDEK", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.RecoverDEK(ctx, &pb.RecoverDEKRequest{
			RecoveryMethod: pb.RecoveryMethod_RECOVERY_METHOD_KEYS,
			RecoveryKeys:   [][]byte{[]byte("key1")},
			Reason:         "test recovery",
		})
		assert.Error(t, err)
		mockClusterDEK.AssertExpectations(t)
	})
}

// =============================================================================
// Conversion Helper Tests
// =============================================================================

func TestNodeDEKSyncStatusToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    service.NodeDEKSyncStatus
		expected pb.NodeDEKSyncStatus
	}{
		{"synced", service.NodeDEKSynced, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_SYNCED},
		{"out of date", service.NodeDEKOutOfDate, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_OUT_OF_DATE},
		{"missing", service.NodeDEKMissing, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_MISSING},
		{"sync failed", service.NodeDEKSyncFailed, pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_SYNC_FAILED},
		{"unknown", service.NodeDEKSyncStatus("unknown"), pb.NodeDEKSyncStatus_NODE_DEK_SYNC_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := nodeDEKSyncStatusToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReencryptionStateToProto(t *testing.T) {
	tests := []struct {
		name     string
		input    service.ReencryptionState
		expected pb.ReencryptionState
	}{
		{"running", service.ReencryptionRunning, pb.ReencryptionState_REENCRYPTION_STATE_RUNNING},
		{"paused", service.ReencryptionPaused, pb.ReencryptionState_REENCRYPTION_STATE_PAUSED},
		{"completed", service.ReencryptionCompleted, pb.ReencryptionState_REENCRYPTION_STATE_COMPLETED},
		{"failed", service.ReencryptionFailed, pb.ReencryptionState_REENCRYPTION_STATE_FAILED},
		{"cancelled", service.ReencryptionCancelled, pb.ReencryptionState_REENCRYPTION_STATE_CANCELLED},
		{"unknown", service.ReencryptionState("unknown"), pb.ReencryptionState_REENCRYPTION_STATE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reencryptionStateToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoRecoveryMethodToService(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.RecoveryMethod
		expected service.RecoveryMethod
	}{
		{"keys", pb.RecoveryMethod_RECOVERY_METHOD_KEYS, service.RecoveryMethodKeys},
		{"backup", pb.RecoveryMethod_RECOVERY_METHOD_BACKUP, service.RecoveryMethodBackup},
		{"manual", pb.RecoveryMethod_RECOVERY_METHOD_MANUAL, service.RecoveryMethodManual},
		{"unspecified defaults to keys", pb.RecoveryMethod_RECOVERY_METHOD_UNSPECIFIED, service.RecoveryMethodKeys},
		{"unknown defaults to keys", pb.RecoveryMethod(999), service.RecoveryMethodKeys},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoRecoveryMethodToService(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Service Not Available Tests
// =============================================================================

func TestClusterDEKServiceNotAvailable(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	// Create server without ClusterDEK service (returns nil)
	mockSvc := testutil.NewMockQuicBaaS()
	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("InitializeClusterDEK not available", func(t *testing.T) {
		_, err := srv.InitializeClusterDEK(ctx, &pb.InitClusterDEKRequest{
			Algorithm: "AES-256-GCM",
			KeySize:   256,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetClusterDEKStatus not available", func(t *testing.T) {
		_, err := srv.GetClusterDEKStatus(ctx, &pb.GetClusterDEKStatusRequest{})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("RotateClusterDEK not available", func(t *testing.T) {
		_, err := srv.RotateClusterDEK(ctx, &pb.RotateClusterDEKRequest{
			Reason: "test",
		})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("SyncNodeDEK not available", func(t *testing.T) {
		_, err := srv.SyncNodeDEK(ctx, &pb.SyncNodeDEKRequest{
			NodeId: 1,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetNodeDEKStatus not available", func(t *testing.T) {
		_, err := srv.GetNodeDEKStatus(ctx, &pb.GetNodeDEKStatusRequest{
			NodeId: 1,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("ClusterStartReencryption not available", func(t *testing.T) {
		_, err := srv.ClusterStartReencryption(ctx, &pb.ClusterStartReencryptionRequest{})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("GetReencryptionStatus not available", func(t *testing.T) {
		_, err := srv.GetReencryptionStatus(ctx, &pb.GetReencryptionStatusRequest{})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("CancelReencryption not available", func(t *testing.T) {
		_, err := srv.CancelReencryption(ctx, &pb.CancelReencryptionRequest{})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("RecoverDEK not available", func(t *testing.T) {
		_, err := srv.RecoverDEK(ctx, &pb.RecoverDEKRequest{
			RecoveryMethod: pb.RecoveryMethod_RECOVERY_METHOD_KEYS,
			Reason:         "test",
		})
		assert.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}
