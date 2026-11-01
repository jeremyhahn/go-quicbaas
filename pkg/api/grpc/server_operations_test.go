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
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// createTestServerForOperations creates a server with mock service for operations tests.
func createTestServerForOperations(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-operations-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()

	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)

	return srv, mockSvc
}

// TestMigrateData tests the MigrateData operation.
func TestMigrateData(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("MigrateData", mock.Anything, mock.MatchedBy(func(req *service.MigrateDataRequest) bool {
			return req.SourceShard == 0 && req.TargetShard == 1 && req.Concurrency == 8
		})).Return(&service.MigrateDataResponse{
			MigrationID: "migration-123",
		}, nil).Once()

		req := &pb.MigrateDataRequest{
			SourceBackend:      "pebble",
			DestinationBackend: "rocksdb",
			Concurrency:        8,
		}

		resp, err := srv.MigrateData(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "migration-123", resp.OperationId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("SuccessWithDefaultConcurrency", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("MigrateData", mock.Anything, mock.MatchedBy(func(req *service.MigrateDataRequest) bool {
			return req.Concurrency == 4 // Default concurrency
		})).Return(&service.MigrateDataResponse{
			MigrationID: "migration-124",
		}, nil).Once()

		req := &pb.MigrateDataRequest{
			SourceBackend:      "pebble",
			DestinationBackend: "rocksdb",
			Concurrency:        0, // Use default
		}

		resp, err := srv.MigrateData(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "migration-124", resp.OperationId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("NilRequest", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		resp, err := srv.MigrateData(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("EmptySourceBackend", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.MigrateDataRequest{
			SourceBackend:      "",
			DestinationBackend: "rocksdb",
		}

		resp, err := srv.MigrateData(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("EmptyDestinationBackend", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.MigrateDataRequest{
			SourceBackend:      "pebble",
			DestinationBackend: "",
		}

		resp, err := srv.MigrateData(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("ServiceError", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("MigrateData", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		req := &pb.MigrateDataRequest{
			SourceBackend:      "pebble",
			DestinationBackend: "rocksdb",
		}

		resp, err := srv.MigrateData(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

// TestGetMigrationProgress tests the GetMigrationProgress operation.
func TestGetMigrationProgress(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("GetMigrationProgress", mock.Anything, mock.MatchedBy(func(req *service.GetMigrationProgressRequest) bool {
			return req.MigrationID == "migration-123"
		})).Return(&service.GetMigrationProgressResponse{
			MigrationID:   "migration-123",
			ItemsMigrated: 5000,
			TotalItems:    10000,
			Progress:      50.0,
			ETA:           300,
			Status:        "in_progress",
		}, nil).Once()

		req := &pb.GetMigrationProgressRequest{
			OperationId: "migration-123",
		}

		resp, err := srv.GetMigrationProgress(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "migration-123", resp.OperationId)
		assert.Equal(t, uint64(5000), resp.KeysProcessed)
		assert.Equal(t, uint64(10000), resp.EstimatedTotal)
		assert.Equal(t, 50.0, resp.PercentComplete)
		assert.Equal(t, int64(300), resp.EstimatedRemaining)
		assert.Equal(t, "in_progress", resp.Status)
		mockSvc.AssertExpectations(t)
	})

	t.Run("NilRequest", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		resp, err := srv.GetMigrationProgress(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("EmptyOperationId", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.GetMigrationProgressRequest{
			OperationId: "",
		}

		resp, err := srv.GetMigrationProgress(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("ServiceError", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("GetMigrationProgress", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		req := &pb.GetMigrationProgressRequest{
			OperationId: "migration-123",
		}

		resp, err := srv.GetMigrationProgress(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

// TestGetRebalanceProgress tests the GetRebalanceProgress operation.
func TestGetRebalanceProgress(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("GetRebalanceProgress", mock.Anything, mock.MatchedBy(func(req *service.GetRebalanceProgressRequest) bool {
			return req.OperationID == "rebalance-123"
		})).Return(&service.GetRebalanceProgressResponse{
			OperationID:    "rebalance-123",
			Status:         "in_progress",
			Progress:       50.0,
			ShardsMoved:    1,
			TotalShards:    3,
			ElapsedSeconds: 10.5,
		}, nil).Once()

		req := &pb.GetRebalanceProgressRequest{
			OperationId: "rebalance-123",
		}

		resp, err := srv.GetRebalanceProgress(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "rebalance-123", resp.OperationId)
		assert.Equal(t, int32(3), resp.TotalShards)
		assert.Equal(t, int32(1), resp.ShardsMoved)
		assert.Equal(t, 50.0, resp.PercentComplete)
		assert.Equal(t, "in_progress", resp.Status)
		mockSvc.AssertExpectations(t)
	})

	t.Run("NilRequest", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		resp, err := srv.GetRebalanceProgress(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("EmptyOperationId", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.GetRebalanceProgressRequest{
			OperationId: "",
		}

		resp, err := srv.GetRebalanceProgress(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("ServiceError", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("GetRebalanceProgress", mock.Anything, mock.Anything).Return(
			nil, &qberrors.QBError{Code: qberrors.ErrNotFound, Op: "test", Err: &qberrors.MessageError{Message: "operation not found"}},
		).Once()

		req := &pb.GetRebalanceProgressRequest{
			OperationId: "nonexistent-123",
		}

		resp, err := srv.GetRebalanceProgress(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

// TestReplaceNode tests the ReplaceNode operation.
func TestReplaceNode(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("ReplaceNode", mock.Anything, mock.MatchedBy(func(req *service.ReplaceNodeRequest) bool {
			return req.OldNodeID == 2 && req.NewNodeID == 4 && req.NewAddress == "localhost:5004"
		})).Return(&service.ReplaceNodeResponse{
			JobID:  "replace-123",
			Status: "started",
		}, nil).Once()

		req := &pb.ReplaceNodeRequest{
			FailedNodeId:       2,
			ReplacementNodeId:  4,
			ReplacementAddress: "localhost:5004",
		}

		resp, err := srv.ReplaceNode(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Success)
		assert.Equal(t, "started", resp.Message)
		mockSvc.AssertExpectations(t)
	})

	t.Run("NilRequest", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		resp, err := srv.ReplaceNode(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("InvalidFailedNodeId", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.ReplaceNodeRequest{
			FailedNodeId:       0,
			ReplacementNodeId:  4,
			ReplacementAddress: "localhost:5004",
		}

		resp, err := srv.ReplaceNode(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("InvalidReplacementNodeId", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.ReplaceNodeRequest{
			FailedNodeId:       2,
			ReplacementNodeId:  0,
			ReplacementAddress: "localhost:5004",
		}

		resp, err := srv.ReplaceNode(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("EmptyReplacementAddress", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.ReplaceNodeRequest{
			FailedNodeId:       2,
			ReplacementNodeId:  4,
			ReplacementAddress: "",
		}

		resp, err := srv.ReplaceNode(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("ServiceError", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("ReplaceNode", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		req := &pb.ReplaceNodeRequest{
			FailedNodeId:       2,
			ReplacementNodeId:  4,
			ReplacementAddress: "localhost:5004",
		}

		resp, err := srv.ReplaceNode(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

// TestUpgradeNode tests the UpgradeNode operation.
func TestUpgradeNode(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("UpgradeNode", mock.Anything, mock.MatchedBy(func(req *service.UpgradeNodeRequest) bool {
			return req.NodeID == 1 && req.TargetVersion == "v2.0.0"
		})).Return(&service.UpgradeNodeResponse{
			JobID: "upgrade-123",
		}, nil).Once()

		req := &pb.UpgradeNodeRequest{
			NodeId:        1,
			TargetVersion: "v2.0.0",
		}

		resp, err := srv.UpgradeNode(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "upgrade-123", resp.OperationId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("NilRequest", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		resp, err := srv.UpgradeNode(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("NodeIdZero_AllNodes", func(t *testing.T) {
		// NodeId 0 means upgrade all nodes - this should succeed
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("UpgradeNode", mock.Anything, mock.MatchedBy(func(req *service.UpgradeNodeRequest) bool {
			return req.NodeID == 0 && req.TargetVersion == "v2.0.0"
		})).Return(&service.UpgradeNodeResponse{
			JobID: "upgrade-all-nodes-123",
		}, nil).Once()

		req := &pb.UpgradeNodeRequest{
			NodeId:        0,
			TargetVersion: "v2.0.0",
		}

		resp, err := srv.UpgradeNode(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "upgrade-all-nodes-123", resp.OperationId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("EmptyTargetVersion", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.UpgradeNodeRequest{
			NodeId:        1,
			TargetVersion: "",
		}

		resp, err := srv.UpgradeNode(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("ServiceError", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("UpgradeNode", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		req := &pb.UpgradeNodeRequest{
			NodeId:        1,
			TargetVersion: "v2.0.0",
		}

		resp, err := srv.UpgradeNode(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

// TestGetUpgradeStatus tests the GetUpgradeStatus operation.
func TestGetUpgradeStatus(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("GetUpgradeStatus", mock.Anything, mock.MatchedBy(func(req *service.GetUpgradeStatusRequest) bool {
			return req.JobID == "upgrade-123"
		})).Return(&service.GetUpgradeStatusResponse{
			JobID:          "upgrade-123",
			Progress:       75.0,
			CurrentVersion: "v1.0.0",
			TargetVersion:  "v2.0.0",
			Status:         "upgrading",
		}, nil).Once()

		req := &pb.GetUpgradeStatusRequest{
			OperationId: "upgrade-123",
		}

		resp, err := srv.GetUpgradeStatus(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "upgrade-123", resp.OperationId)
		assert.Equal(t, 75.0, resp.PercentComplete)
		assert.Equal(t, "v1.0.0", resp.CurrentVersion)
		assert.Equal(t, "v2.0.0", resp.TargetVersion)
		assert.Equal(t, "upgrading", resp.Status)
		mockSvc.AssertExpectations(t)
	})

	t.Run("NilRequest", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		resp, err := srv.GetUpgradeStatus(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("EmptyOperationId", func(t *testing.T) {
		srv, _ := createTestServerForOperations(t)

		req := &pb.GetUpgradeStatusRequest{
			OperationId: "",
		}

		resp, err := srv.GetUpgradeStatus(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("ServiceError", func(t *testing.T) {
		srv, mockSvc := createTestServerForOperations(t)

		mockSvc.On("GetUpgradeStatus", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		req := &pb.GetUpgradeStatusRequest{
			OperationId: "upgrade-123",
		}

		resp, err := srv.GetUpgradeStatus(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

// --- Merged from server_comprehensive_coverage_test.go ---

// =============================================================================
// protoToServiceConsistency Tests (converters.go)
// =============================================================================

func TestProtoToServiceConsistency_AllLevels(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.ConsistencyLevel
		expected service.ConsistencyLevel
	}{
		{
			name:     "strong",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
			expected: service.ConsistencyLevelStrong,
		},
		{
			name:     "linearizable",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "eventual",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
			expected: service.ConsistencyLevelEventual,
		},
		{
			name:     "unspecified_defaults_to_linearizable",
			input:    pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
			expected: service.ConsistencyLevelLinearizable,
		},
		{
			name:     "unknown_defaults_to_linearizable",
			input:    pb.ConsistencyLevel(999),
			expected: service.ConsistencyLevelLinearizable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoToServiceConsistency(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// serviceHealthToProto Tests (converters.go)
// =============================================================================

func TestServiceHealthToProto_AllStatuses(t *testing.T) {
	tests := []struct {
		name     string
		input    service.HealthStatus
		expected pb.HealthStatus
	}{
		{
			name:     "healthy",
			input:    service.HealthStatusHealthy,
			expected: pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		},
		{
			name:     "degraded",
			input:    service.HealthStatusDegraded,
			expected: pb.HealthStatus_HEALTH_STATUS_DEGRADED,
		},
		{
			name:     "unhealthy",
			input:    service.HealthStatusUnhealthy,
			expected: pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		},
		{
			name:     "unknown_status",
			input:    service.HealthStatus(999), // Unknown value
			expected: pb.HealthStatus_HEALTH_STATUS_UNKNOWN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceHealthToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// protoToServiceAggregateFunction Tests (converters.go)
// =============================================================================

func TestProtoToServiceAggregateFunction_AllFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.AggregateFunction
		expected service.AggregateFunction
	}{
		{
			name:     "sum",
			input:    pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
			expected: service.AggregateFunctionSum,
		},
		{
			name:     "avg",
			input:    pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
			expected: service.AggregateFunctionAvg,
		},
		{
			name:     "min",
			input:    pb.AggregateFunction_AGGREGATE_FUNCTION_MIN,
			expected: service.AggregateFunctionMin,
		},
		{
			name:     "max",
			input:    pb.AggregateFunction_AGGREGATE_FUNCTION_MAX,
			expected: service.AggregateFunctionMax,
		},
		{
			name:     "count",
			input:    pb.AggregateFunction_AGGREGATE_FUNCTION_COUNT,
			expected: service.AggregateFunctionCount,
		},
		{
			name:     "unspecified",
			input:    pb.AggregateFunction_AGGREGATE_FUNCTION_UNSPECIFIED,
			expected: service.AggregateFunctionUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoToServiceAggregateFunction(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// protoToServiceCompareTarget Tests (converters.go)
// =============================================================================

func TestProtoToServiceCompareTarget_AllTargets(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.CompareTarget
		expected service.CompareTarget
	}{
		{
			name:     "version",
			input:    pb.CompareTarget_COMPARE_TARGET_VERSION,
			expected: service.CompareTargetVersion,
		},
		{
			name:     "value",
			input:    pb.CompareTarget_COMPARE_TARGET_VALUE,
			expected: service.CompareTargetValue,
		},
		{
			name:     "exists",
			input:    pb.CompareTarget_COMPARE_TARGET_EXISTS,
			expected: service.CompareTargetExists,
		},
		{
			name:     "unspecified",
			input:    pb.CompareTarget_COMPARE_TARGET_UNSPECIFIED,
			expected: service.CompareTargetUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoToServiceCompareTarget(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// protoToServiceCompareOperator Tests (converters.go)
// =============================================================================

func TestProtoToServiceCompareOperator_AllOperators(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.CompareOperator
		expected service.CompareOperator
	}{
		{
			name:     "equal",
			input:    pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			expected: service.CompareOperatorEqual,
		},
		{
			name:     "not_equal",
			input:    pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL,
			expected: service.CompareOperatorNotEqual,
		},
		{
			name:     "greater",
			input:    pb.CompareOperator_COMPARE_OPERATOR_GREATER,
			expected: service.CompareOperatorGreater,
		},
		{
			name:     "less",
			input:    pb.CompareOperator_COMPARE_OPERATOR_LESS,
			expected: service.CompareOperatorLess,
		},
		{
			name:     "greater_equal",
			input:    pb.CompareOperator_COMPARE_OPERATOR_GREATER_EQUAL,
			expected: service.CompareOperatorGreaterEqual,
		},
		{
			name:     "less_equal",
			input:    pb.CompareOperator_COMPARE_OPERATOR_LESS_EQUAL,
			expected: service.CompareOperatorLessEqual,
		},
		{
			name:     "unspecified",
			input:    pb.CompareOperator_COMPARE_OPERATOR_UNSPECIFIED,
			expected: service.CompareOperatorUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoToServiceCompareOperator(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// protoToServiceTransactionOpType Tests (converters.go)
// =============================================================================

func TestTransactionOpTypeConversion_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		proto pb.TransactionOpType
		svc   service.TransactionOpType
	}{
		{name: "put", proto: pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, svc: service.TransactionOpTypePut},
		{name: "get", proto: pb.TransactionOpType_TRANSACTION_OP_TYPE_GET, svc: service.TransactionOpTypeGet},
		{name: "delete", proto: pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE, svc: service.TransactionOpTypeDelete},
		{name: "unspecified", proto: pb.TransactionOpType_TRANSACTION_OP_TYPE_UNSPECIFIED, svc: service.TransactionOpTypeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.svc, protoToServiceTransactionOpType(tt.proto))
			assert.Equal(t, tt.proto, serviceTransactionOpTypeToProto(tt.svc))
		})
	}
}

// =============================================================================
// BatchGet Additional Tests
// =============================================================================

func TestBatchGet_Cases(t *testing.T) {
	tests := []struct {
		name      string
		requests  []*pb.GetRequest
		mockResp  *service.BatchGetResponse
		mockErr   error
		wantErr   bool
		wantCode  codes.Code
		wantFound []bool
	}{
		{
			name:     "service error",
			requests: []*pb.GetRequest{{EntityType: "users", Id: 1}},
			mockErr:  assert.AnError,
			wantErr:  true,
		},
		{
			name:     "empty requests",
			requests: []*pb.GetRequest{},
			wantErr:  true,
			wantCode: codes.InvalidArgument,
		},
		{
			name: "partial results",
			requests: []*pb.GetRequest{
				{EntityType: "users", Id: 1},
				{EntityType: "users", Id: 2},
				{EntityType: "users", Id: 3},
			},
			mockResp: &service.BatchGetResponse{
				Responses: []*service.GetResponse{
					{ID: 1, Found: true, Data: []byte(`{"name":"user1"}`)},
					{ID: 2, Found: false},
					{ID: 3, Found: true, Data: []byte(`{"name":"user3"}`)},
				},
			},
			wantFound: []bool{true, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			if tt.mockResp != nil || tt.mockErr != nil {
				mockSvc.On("BatchGet", mock.Anything, mock.Anything).
					Return(tt.mockResp, tt.mockErr).Once()
			}

			resp, err := srv.BatchGet(context.Background(), &pb.BatchGetRequest{Requests: tt.requests})

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantCode != codes.OK {
					assert.Equal(t, tt.wantCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.Len(t, resp.Responses, len(tt.wantFound))
				for i, found := range tt.wantFound {
					assert.Equal(t, found, resp.Responses[i].Found)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// BatchDelete Additional Tests
// =============================================================================

func TestBatchDelete_Cases(t *testing.T) {
	tests := []struct {
		name        string
		requests    []*pb.DeleteRequest
		mockResp    *service.BatchDeleteResponse
		mockErr     error
		wantErr     bool
		wantCode    codes.Code
		wantDeleted []bool
	}{
		{
			name:     "service error",
			requests: []*pb.DeleteRequest{{EntityType: "users", Id: 1}},
			mockErr:  assert.AnError,
			wantErr:  true,
		},
		{
			name:     "empty requests",
			requests: []*pb.DeleteRequest{},
			wantErr:  true,
			wantCode: codes.InvalidArgument,
		},
		{
			name: "partial deletions",
			requests: []*pb.DeleteRequest{
				{EntityType: "users", Id: 1},
				{EntityType: "users", Id: 999},
				{EntityType: "users", Id: 3},
			},
			mockResp: &service.BatchDeleteResponse{
				Responses: []*service.DeleteResponse{
					{Deleted: true, Timestamp: time.Now().UnixNano()},
					{Deleted: false, Timestamp: time.Now().UnixNano()},
					{Deleted: true, Timestamp: time.Now().UnixNano()},
				},
				Timestamp: time.Now().UnixNano(),
			},
			wantDeleted: []bool{true, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			if tt.mockResp != nil || tt.mockErr != nil {
				mockSvc.On("BatchDelete", mock.Anything, mock.Anything).
					Return(tt.mockResp, tt.mockErr).Once()
			}

			resp, err := srv.BatchDelete(context.Background(), &pb.BatchDeleteRequest{Requests: tt.requests})

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantCode != codes.OK {
					assert.Equal(t, tt.wantCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				require.Len(t, resp.Responses, len(tt.wantDeleted))
				for i, deleted := range tt.wantDeleted {
					assert.Equal(t, deleted, resp.Responses[i].Deleted)
				}
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Aggregate Additional Tests
// =============================================================================

func TestAggregate_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	startTime := time.Now().Add(-1 * time.Hour).UnixNano()
	endTime := time.Now().UnixNano()

	mockSvc.On("Aggregate", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.Aggregate(context.Background(), &pb.AggregateRequest{
		EntityType: "metrics",
		FieldName:  "value",
		Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
		StartTime:  startTime,
		EndTime:    endTime,
	})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestAggregate_AllFunctions(t *testing.T) {
	functions := []pb.AggregateFunction{
		pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
		pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
		pb.AggregateFunction_AGGREGATE_FUNCTION_MIN,
		pb.AggregateFunction_AGGREGATE_FUNCTION_MAX,
		pb.AggregateFunction_AGGREGATE_FUNCTION_COUNT,
	}

	for _, fn := range functions {
		t.Run(fn.String(), func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)

			startTime := time.Now().Add(-1 * time.Hour).UnixNano()
			endTime := time.Now().UnixNano()

			mockSvc.On("Aggregate", mock.Anything, mock.Anything).
				Return(&service.AggregateResponse{
					Result:    42.0,
					Count:     10,
					StartTime: startTime,
					EndTime:   endTime,
				}, nil).Once()

			resp, err := srv.Aggregate(context.Background(), &pb.AggregateRequest{
				EntityType: "metrics",
				FieldName:  "value",
				Function:   fn,
				StartTime:  startTime,
				EndTime:    endTime,
			})

			require.NoError(t, err)
			assert.Equal(t, 42.0, resp.Result)
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Transaction Additional Tests
// =============================================================================

func TestTransaction_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         1,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    1,
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         1,
				Data:       []byte(`{}`),
			},
		},
	})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_ConditionNotMet(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(&service.TransactionResponse{
			Succeeded: false,
			Responses: []*service.TransactionOpResponse{},
			Message:   "Condition not met",
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         1,
				Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
				Version:    999, // Wrong version
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         1,
				Data:       []byte(`{}`),
			},
		},
	})

	require.NoError(t, err)
	assert.False(t, resp.Succeeded)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_WithFailureBranch(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(&service.TransactionResponse{
			Succeeded: false,
			Responses: []*service.TransactionOpResponse{
				{
					OpType:     service.TransactionOpTypeGet,
					EntityType: "user",
					ID:         1,
					Success:    true,
					Data:       []byte(`{"name":"existing"}`),
				},
			},
			Message:   "Condition failed, executed failure branch",
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Compare: []*pb.CompareCondition{
			{
				EntityType: "user",
				Id:         1,
				Target:     pb.CompareTarget_COMPARE_TARGET_EXISTS,
				Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
			},
		},
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         1,
				Data:       []byte(`{"name":"new"}`),
			},
		},
		Failure: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_GET,
				EntityType: "user",
				Id:         1,
			},
		},
	})

	require.NoError(t, err)
	assert.False(t, resp.Succeeded)
	assert.Len(t, resp.Responses, 1)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// GetLeader Additional Tests
// =============================================================================

func TestGetLeader_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("GetLeader", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.GetLeader(context.Background(), &pb.GetLeaderRequest{
		ShardId: 1,
	})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// ListEntityTypes Tests
// =============================================================================

func TestListEntityTypes_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("ListEntityTypes", mock.Anything, mock.Anything).
		Return(&service.ListEntityTypesResponse{
			EntityTypes: []string{"users", "orders", "products"},
			TotalCount:  3,
		}, nil).Once()

	resp, err := srv.ListEntityTypes(context.Background(), &pb.ListEntityTypesRequest{})

	require.NoError(t, err)
	assert.Len(t, resp.EntityTypes, 3)
	assert.Equal(t, uint64(3), resp.TotalCount)
	mockSvc.AssertExpectations(t)
}

func TestListEntityTypes_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("ListEntityTypes", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.ListEntityTypes(context.Background(), &pb.ListEntityTypesRequest{})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Server Methods Tests
// =============================================================================

func TestServer_SetShardRouter(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)

	// Verify SetShardRouter works
	mockRouter := &mockShardRouter{}
	srv.SetShardRouter(mockRouter)
	assert.NotNil(t, srv.shardRouter)
}

func TestServer_RecordQuery_NilTracker(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	srv.qpsTracker = nil

	// Should not panic
	srv.RecordQuery()
}

func TestServer_QPS_NilTracker(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	srv.qpsTracker = nil

	// Should return 0 without panic
	qps := srv.QPS()
	assert.Equal(t, float64(0), qps)
}

type mockShardRouter struct{}

func (m *mockShardRouter) Route(entityType string, id uint64) uint64 {
	return 1
}

// --- Merged from server_coverage_boost_test.go ---

// =============================================================================
// Get Additional Tests
// =============================================================================

func TestGet_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Get", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.Get(context.Background(), &pb.GetRequest{
		EntityType: "users",
		Id:         1,
	})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestSingleOperations_EmptyEntityType(t *testing.T) {
	tests := []struct {
		name string
		call func(srv *Server) error
	}{
		{
			name: "Get",
			call: func(srv *Server) error {
				_, err := srv.Get(context.Background(), &pb.GetRequest{EntityType: "", Id: 1})
				return err
			},
		},
		{
			name: "Delete",
			call: func(srv *Server) error {
				_, err := srv.Delete(context.Background(), &pb.DeleteRequest{EntityType: "", Id: 1})
				return err
			},
		},
		{
			name: "Put",
			call: func(srv *Server) error {
				_, err := srv.Put(context.Background(), &pb.PutRequest{EntityType: "", Id: 1, Data: []byte(`{}`)})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)

			require.Error(t, tt.call(srv))
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Delete Additional Tests
// =============================================================================

func TestDelete_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Delete", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.Delete(context.Background(), &pb.DeleteRequest{
		EntityType: "users",
		Id:         1,
	})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

func TestDelete_NotFound(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Delete", mock.Anything, mock.Anything).
		Return(&service.DeleteResponse{
			Deleted:   false,
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := srv.Delete(context.Background(), &pb.DeleteRequest{
		EntityType: "users",
		Id:         999,
	})

	require.NoError(t, err)
	assert.False(t, resp.Deleted)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// BatchPut Additional Tests
// =============================================================================

func TestBatchPut_Cases(t *testing.T) {
	largeRequests := make([]*pb.PutRequest, 100)
	largeResponses := make([]*service.PutResponse, 100)
	for i := range largeRequests {
		largeRequests[i] = &pb.PutRequest{EntityType: "users", Id: uint64(i), Data: []byte(`{"name":"user"}`)}
		largeResponses[i] = &service.PutResponse{ID: uint64(i), Version: 1, Timestamp: time.Now().UnixNano()}
	}

	tests := []struct {
		name     string
		requests []*pb.PutRequest
		mockResp *service.BatchPutResponse
		mockErr  error
		wantErr  bool
		wantCode codes.Code
		wantLen  int
	}{
		{
			name:     "service error",
			requests: []*pb.PutRequest{{EntityType: "users", Id: 1, Data: []byte(`{}`)}},
			mockErr:  assert.AnError,
			wantErr:  true,
		},
		{
			name:     "empty requests",
			requests: []*pb.PutRequest{},
			wantErr:  true,
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "large batch",
			requests: largeRequests,
			mockResp: &service.BatchPutResponse{Responses: largeResponses, Timestamp: time.Now().UnixNano()},
			wantLen:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			if tt.mockResp != nil || tt.mockErr != nil {
				mockSvc.On("BatchPut", mock.Anything, mock.Anything).
					Return(tt.mockResp, tt.mockErr).Once()
			}

			resp, err := srv.BatchPut(context.Background(), &pb.BatchPutRequest{Requests: tt.requests})

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantCode != codes.OK {
					assert.Equal(t, tt.wantCode, status.Code(err))
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, resp.Responses, tt.wantLen)
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Health Additional Tests
// =============================================================================

func TestHealth_Cases(t *testing.T) {
	tests := []struct {
		name       string
		mockResp   *service.HealthResponse
		mockErr    error
		wantErr    bool
		wantStatus pb.HealthStatus
	}{
		{
			name: "healthy",
			mockResp: &service.HealthResponse{
				Status:        service.HealthStatusHealthy,
				NodeID:        1,
				Version:       "v1.0.0",
				UptimeSeconds: 3600,
				Details: map[string]*service.ComponentHealth{
					"database": {Status: service.HealthStatusHealthy, Message: "ok"},
					"raft":     {Status: service.HealthStatusHealthy, Message: "leader"},
				},
			},
			wantStatus: pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		},
		{
			name: "degraded",
			mockResp: &service.HealthResponse{
				Status:        service.HealthStatusDegraded,
				NodeID:        1,
				UptimeSeconds: 3600,
				Details: map[string]*service.ComponentHealth{
					"database": {Status: service.HealthStatusHealthy, Message: "ok"},
					"raft":     {Status: service.HealthStatusUnhealthy, Message: "no leader"},
				},
			},
			wantStatus: pb.HealthStatus_HEALTH_STATUS_DEGRADED,
		},
		{
			name:    "service error",
			mockErr: assert.AnError,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			mockSvc.On("Health", mock.Anything, mock.Anything).
				Return(tt.mockResp, tt.mockErr).Once()

			resp, err := srv.Health(context.Background(), &pb.HealthRequest{})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantStatus, resp.Status)
			}
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// ClusterStatus Additional Tests
// =============================================================================

func TestClusterStatus_Success(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
		Return(&service.ClusterStatusResponse{
			ClusterID: "test-cluster",
			Nodes: []*service.NodeInfo{
				{NodeID: 1, Address: "10.0.0.1:5000", IsLeader: true},
				{NodeID: 2, Address: "10.0.0.2:5000", IsLeader: false},
				{NodeID: 3, Address: "10.0.0.3:5000", IsLeader: false},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := srv.ClusterStatus(context.Background(), &pb.ClusterStatusRequest{})

	require.NoError(t, err)
	assert.Equal(t, "test-cluster", resp.ClusterId)
	assert.Len(t, resp.Nodes, 3)
	mockSvc.AssertExpectations(t)
}

func TestClusterStatus_ServiceError(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
		Return(nil, assert.AnError).Once()

	_, err := srv.ClusterStatus(context.Background(), &pb.ClusterStatusRequest{})

	require.Error(t, err)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Put Additional Tests
// =============================================================================

func TestPut_WithTTL(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Put", mock.Anything, mock.Anything).
		Return(&service.PutResponse{
			ID:        1,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := srv.Put(context.Background(), &pb.PutRequest{
		EntityType: "users",
		Id:         1,
		Data:       []byte(`{"name":"test"}`),
		TtlSeconds: 3600,
	})

	require.NoError(t, err)
	assert.Equal(t, uint64(1), resp.Id)
	mockSvc.AssertExpectations(t)
}

func TestPut_AllConsistencyLevels(t *testing.T) {
	consistencyLevels := []pb.ConsistencyLevel{
		pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
		pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
		pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
		pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	for _, level := range consistencyLevels {
		t.Run(level.String(), func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)

			mockSvc.On("Put", mock.Anything, mock.Anything).
				Return(&service.PutResponse{
					ID:        1,
					Version:   1,
					Timestamp: time.Now().UnixNano(),
				}, nil).Once()

			resp, err := srv.Put(context.Background(), &pb.PutRequest{
				EntityType:  "users",
				Id:          1,
				Data:        []byte(`{"name":"test"}`),
				Consistency: level,
			})

			require.NoError(t, err)
			assert.Equal(t, uint64(1), resp.Id)
			mockSvc.AssertExpectations(t)
		})
	}
}

// =============================================================================
// Transaction Additional Tests
// =============================================================================

func TestTransaction_WithMultipleSuccessOps(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(&service.TransactionResponse{
			Succeeded: true,
			Responses: []*service.TransactionOpResponse{
				{Success: true, ID: 1, Version: 1},
				{Success: true, ID: 2, Version: 1},
				{Success: true, ID: 3, Version: 1},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Success: []*pb.TransactionOp{
			{OpType: pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, EntityType: "users", Id: 1, Data: []byte(`{"name":"a"}`)},
			{OpType: pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, EntityType: "users", Id: 2, Data: []byte(`{"name":"b"}`)},
			{OpType: pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, EntityType: "users", Id: 3, Data: []byte(`{"name":"c"}`)},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Len(t, resp.Responses, 3)
	mockSvc.AssertExpectations(t)
}

func TestTransaction_AllOpTypes(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	mockSvc.On("Transaction", mock.Anything, mock.Anything).
		Return(&service.TransactionResponse{
			Succeeded: true,
			Responses: []*service.TransactionOpResponse{
				{Success: true, ID: 1, Version: 1},
				{Success: true, ID: 2, Data: []byte(`{"name":"test"}`)},
				{Success: true, ID: 3},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

	resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
		Success: []*pb.TransactionOp{
			{OpType: pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, EntityType: "users", Id: 1, Data: []byte(`{"name":"a"}`)},
			{OpType: pb.TransactionOpType_TRANSACTION_OP_TYPE_GET, EntityType: "users", Id: 2},
			{OpType: pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE, EntityType: "users", Id: 3},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	mockSvc.AssertExpectations(t)
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestExtractWriteMode_AllModes(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		expected service.WriteMode
	}{
		{
			name:     "no metadata defaults to sync",
			metadata: nil,
			expected: service.WriteModeSync,
		},
		{
			name:     "empty write-mode header defaults to sync",
			metadata: map[string]string{},
			expected: service.WriteModeSync,
		},
		{
			name:     "explicit sync mode",
			metadata: map[string]string{"write-mode": "sync"},
			expected: service.WriteModeSync,
		},
		{
			name:     "batched mode",
			metadata: map[string]string{"write-mode": "batched"},
			expected: service.WriteModeBatched,
		},
		{
			name:     "wal_durable mode",
			metadata: map[string]string{"write-mode": "wal_durable"},
			expected: service.WriteModeWALDurable,
		},
		{
			name:     "unknown mode defaults to sync",
			metadata: map[string]string{"write-mode": "unknown"},
			expected: service.WriteModeSync,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.metadata != nil {
				md := metadata.New(tt.metadata)
				ctx = metadata.NewIncomingContext(context.Background(), md)
			} else {
				ctx = context.Background()
			}

			result := extractWriteMode(ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractProtocol_AllProtocols(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		expected protocol
		isBinary bool
	}{
		{
			name:     "no metadata",
			metadata: nil,
			expected: protocolJSON,
			isBinary: false,
		},
		{
			name:     "empty protocol header",
			metadata: map[string]string{},
			expected: protocolJSON,
			isBinary: false,
		},
		{
			name:     "binary protocol",
			metadata: map[string]string{"x-quicbaas-protocol": "binary"},
			expected: protocolBinary,
			isBinary: true,
		},
		{
			name:     "json protocol (explicit)",
			metadata: map[string]string{"x-quicbaas-protocol": "json"},
			expected: protocolJSON,
			isBinary: false,
		},
		{
			name:     "unknown protocol defaults to JSON",
			metadata: map[string]string{"x-quicbaas-protocol": "unknown"},
			expected: protocolJSON,
			isBinary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ctx context.Context
			if tt.metadata != nil {
				md := metadata.New(tt.metadata)
				ctx = metadata.NewIncomingContext(context.Background(), md)
			} else {
				ctx = context.Background()
			}

			result := extractProtocol(ctx)
			assert.Equal(t, tt.expected, result)
			assert.Equal(t, tt.isBinary, result.IsBinary())
		})
	}
}
