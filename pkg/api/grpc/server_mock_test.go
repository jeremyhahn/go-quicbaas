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
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// mockTestHelper provides common test setup utilities for mock-based tests.
type mockTestHelper struct {
	mock   *mocks.MockQuicBaaS
	server *Server
	logger *logger.Logger
}

// newMockTestHelper creates a new test helper with default setup.
func newMockTestHelper(t *testing.T) *mockTestHelper {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-mock-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := &mocks.MockQuicBaaS{}
	server, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)

	return &mockTestHelper{
		mock:   mockSvc,
		server: server,
		logger: log,
	}
}

// =============================================================================
// Put Handler Tests
// =============================================================================

func TestServerMockPut_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return &service.PutResponse{
			ID:        req.ID,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
			LeaderID:  1,
		}, nil
	}

	ctx := context.Background()
	req := &pb.PutRequest{
		EntityType:  "user",
		Id:          123,
		Data:        []byte(`{"name":"test"}`),
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	resp, err := h.server.Put(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, uint64(123), resp.Id)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestServerMockPut_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "Put",
			Err:  &errors.MessageError{Message: "raft consensus failed"},
		}
	}

	ctx := context.Background()
	req := &pb.PutRequest{
		EntityType: "user",
		Id:         123,
		Data:       []byte(`{"name":"test"}`),
	}

	resp, err := h.server.Put(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockPut_EmptyEntityType(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.PutRequest{
		EntityType: "",
		Id:         1,
		Data:       []byte("data"),
	}

	resp, err := h.server.Put(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Get Handler Tests
// =============================================================================

func TestServerMockGet_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		return &service.GetResponse{
			ID:        req.ID,
			Data:      []byte(`{"name":"test"}`),
			Found:     true,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	ctx := context.Background()
	req := &pb.GetRequest{
		EntityType:  "user",
		Id:          123,
		Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
	}

	resp, err := h.server.Get(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, uint64(123), resp.Id)
	assert.True(t, resp.Found)
	assert.Equal(t, []byte(`{"name":"test"}`), resp.Data)
}

func TestServerMockGet_NotFound(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		return &service.GetResponse{
			ID:    req.ID,
			Found: false,
		}, nil
	}

	ctx := context.Background()
	req := &pb.GetRequest{
		EntityType: "user",
		Id:         999,
	}

	resp, err := h.server.Get(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockGet_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "Get",
			Err:  &errors.MessageError{Message: "operation timed out"},
		}
	}

	ctx := context.Background()
	req := &pb.GetRequest{
		EntityType: "user",
		Id:         123,
	}

	resp, err := h.server.Get(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockGet_EmptyEntityType(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.GetRequest{
		EntityType: "",
		Id:         1,
	}

	resp, err := h.server.Get(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Delete Handler Tests
// =============================================================================

func TestServerMockDelete_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
		return &service.DeleteResponse{
			Deleted:   true,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	ctx := context.Background()
	req := &pb.DeleteRequest{
		EntityType: "user",
		Id:         123,
	}

	resp, err := h.server.Delete(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Deleted)
}

func TestServerMockDelete_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   "Delete",
			Err:  &errors.MessageError{Message: "entity not found"},
		}
	}

	ctx := context.Background()
	req := &pb.DeleteRequest{
		EntityType: "user",
		Id:         123,
	}

	resp, err := h.server.Delete(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockDelete_EmptyEntityType(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.DeleteRequest{
		EntityType: "",
		Id:         1,
	}

	resp, err := h.server.Delete(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// BatchPut Handler Tests
// =============================================================================

func TestServerMockBatchPut_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.BatchPutFunc = func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
		responses := make([]*service.PutResponse, len(req.Requests))
		for i, r := range req.Requests {
			responses[i] = &service.PutResponse{
				ID:        r.ID,
				Version:   1,
				Timestamp: time.Now().UnixNano(),
			}
		}
		return &service.BatchPutResponse{
			Responses: responses,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	ctx := context.Background()
	req := &pb.BatchPutRequest{
		Requests: []*pb.PutRequest{
			{EntityType: "user", Id: 1, Data: []byte(`{"name":"user1"}`)},
			{EntityType: "user", Id: 2, Data: []byte(`{"name":"user2"}`)},
		},
	}

	resp, err := h.server.BatchPut(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Responses, 2)
}

func TestServerMockBatchPut_EmptyBatch(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.BatchPutRequest{
		Requests: []*pb.PutRequest{},
	}

	resp, err := h.server.BatchPut(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockBatchPut_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.BatchPutFunc = func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "BatchPut",
			Err:  &errors.MessageError{Message: "batch operation failed"},
		}
	}

	ctx := context.Background()
	req := &pb.BatchPutRequest{
		Requests: []*pb.PutRequest{
			{EntityType: "user", Id: 1, Data: []byte(`{"name":"user1"}`)},
		},
	}

	resp, err := h.server.BatchPut(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// BatchGet Handler Tests
// =============================================================================

func TestServerMockBatchGet_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.BatchGetFunc = func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
		responses := make([]*service.GetResponse, len(req.Requests))
		for i, r := range req.Requests {
			responses[i] = &service.GetResponse{
				ID:    r.ID,
				Data:  []byte(`{"name":"test"}`),
				Found: true,
			}
		}
		return &service.BatchGetResponse{Responses: responses}, nil
	}

	ctx := context.Background()
	req := &pb.BatchGetRequest{
		Requests: []*pb.GetRequest{
			{EntityType: "user", Id: 1},
			{EntityType: "user", Id: 2},
		},
	}

	resp, err := h.server.BatchGet(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Responses, 2)
}

func TestServerMockBatchGet_EmptyBatch(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.BatchGetRequest{
		Requests: []*pb.GetRequest{},
	}

	resp, err := h.server.BatchGet(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockBatchGet_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.BatchGetFunc = func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "BatchGet",
			Err:  &errors.MessageError{Message: "internal error"},
		}
	}

	ctx := context.Background()
	req := &pb.BatchGetRequest{
		Requests: []*pb.GetRequest{
			{EntityType: "user", Id: 1},
		},
	}

	resp, err := h.server.BatchGet(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// BatchDelete Handler Tests
// =============================================================================

func TestServerMockBatchDelete_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.BatchDeleteFunc = func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
		responses := make([]*service.DeleteResponse, len(req.Requests))
		for i := range req.Requests {
			responses[i] = &service.DeleteResponse{
				Deleted:   true,
				Timestamp: time.Now().UnixNano(),
			}
		}
		return &service.BatchDeleteResponse{
			Responses: responses,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	ctx := context.Background()
	req := &pb.BatchDeleteRequest{
		Requests: []*pb.DeleteRequest{
			{EntityType: "user", Id: 1},
			{EntityType: "user", Id: 2},
		},
	}

	resp, err := h.server.BatchDelete(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Responses, 2)
}

func TestServerMockBatchDelete_EmptyBatch(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.BatchDeleteRequest{
		Requests: []*pb.DeleteRequest{},
	}

	resp, err := h.server.BatchDelete(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockBatchDelete_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.BatchDeleteFunc = func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "BatchDelete",
			Err:  &errors.MessageError{Message: "raft error"},
		}
	}

	ctx := context.Background()
	req := &pb.BatchDeleteRequest{
		Requests: []*pb.DeleteRequest{
			{EntityType: "user", Id: 1},
		},
	}

	resp, err := h.server.BatchDelete(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Transaction Handler Tests
// =============================================================================

func TestServerMockTransaction_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.TransactionFunc = func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
		return &service.TransactionResponse{
			Succeeded: true,
			Timestamp: time.Now().UnixNano(),
			Message:   "transaction committed",
		}, nil
	}

	ctx := context.Background()
	req := &pb.TransactionRequest{
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
				Data:       []byte(`{"name":"updated"}`),
			},
		},
	}

	resp, err := h.server.Transaction(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Succeeded)
}

func TestServerMockTransaction_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.TransactionFunc = func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "Transaction",
			Err:  &errors.MessageError{Message: "transaction failed"},
		}
	}

	ctx := context.Background()
	req := &pb.TransactionRequest{
		Success: []*pb.TransactionOp{
			{
				OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
				EntityType: "user",
				Id:         1,
			},
		},
	}

	resp, err := h.server.Transaction(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Health Handler Tests
// =============================================================================

func TestServerMockHealth_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
		return &service.HealthResponse{
			Status:        service.HealthStatusHealthy,
			NodeID:        1,
			Version:       "v1.0.0",
			UptimeSeconds: 3600,
			Details: map[string]*service.ComponentHealth{
				"storage": {
					Status:    service.HealthStatusHealthy,
					Message:   "OK",
					LastCheck: time.Now().UnixNano(),
				},
			},
		}, nil
	}

	ctx := context.Background()
	req := &pb.HealthRequest{IncludeDetails: true}

	resp, err := h.server.Health(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Status)
	assert.Equal(t, uint64(1), resp.NodeId)
	assert.NotEmpty(t, resp.Details)
}

func TestServerMockHealth_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.HealthFunc = func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Health",
			Err:  &errors.MessageError{Message: "health check failed"},
		}
	}

	ctx := context.Background()
	req := &pb.HealthRequest{}

	resp, err := h.server.Health(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// ClusterStatus Handler Tests
// =============================================================================

func TestServerMockClusterStatus_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.ClusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return &service.ClusterStatusResponse{
			ClusterID: "test-cluster-id",
			Nodes: []*service.NodeInfo{
				{
					NodeID:        1,
					IsLeader:      true,
					HealthStatus:  service.HealthStatusHealthy,
					Version:       "v1.0.0",
					UptimeSeconds: 3600,
				},
			},
			Shards: []*service.ShardInfo{
				{
					ShardID:  1,
					LeaderID: 1,
					Replicas: []uint64{1, 2, 3},
					Status:   service.HealthStatusHealthy,
				},
			},
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	ctx := context.Background()
	req := &pb.ClusterStatusRequest{IncludeMetrics: true}

	resp, err := h.server.ClusterStatus(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-cluster-id", resp.ClusterId)
	assert.Len(t, resp.Nodes, 1)
	assert.Len(t, resp.Shards, 1)
}

func TestServerMockClusterStatus_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.ClusterStatusFunc = func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrRaft,
			Op:   "ClusterStatus",
			Err:  &errors.MessageError{Message: "failed to get cluster status"},
		}
	}

	ctx := context.Background()
	req := &pb.ClusterStatusRequest{}

	resp, err := h.server.ClusterStatus(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GetLeader Handler Tests
// =============================================================================

func TestServerMockGetLeader_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.GetLeaderFunc = func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
		return &service.GetLeaderResponse{
			ShardID:  req.ShardID,
			LeaderID: 1,
			Address:  "localhost:9090",
		}, nil
	}

	ctx := context.Background()
	req := &pb.GetLeaderRequest{ShardId: 1}

	resp, err := h.server.GetLeader(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.ShardId)
	assert.Equal(t, uint64(1), resp.LeaderId)
}

func TestServerMockGetLeader_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.GetLeaderFunc = func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrLeaderNotAvailable,
			Op:   "GetLeader",
			Err:  &errors.MessageError{Message: "leader not available"},
		}
	}

	ctx := context.Background()
	req := &pb.GetLeaderRequest{ShardId: 1}

	resp, err := h.server.GetLeader(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Aggregate Handler Tests
// =============================================================================

func TestServerMockAggregate_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.AggregateFunc = func(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
		return &service.AggregateResponse{
			Result:    150.5,
			Count:     10,
			StartTime: time.Now().Add(-time.Hour).UnixNano(),
			EndTime:   time.Now().UnixNano(),
		}, nil
	}

	ctx := context.Background()
	req := &pb.AggregateRequest{
		EntityType: "metrics",
		FieldName:  "value",
		Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
		StartTime:  time.Now().Add(-time.Hour).UnixNano(),
		EndTime:    time.Now().UnixNano(),
	}

	resp, err := h.server.Aggregate(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 150.5, resp.Result)
	assert.Equal(t, uint64(10), resp.Count)
}

func TestServerMockAggregate_EmptyEntityType(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.AggregateRequest{
		EntityType: "", // Empty entity type should fail validation
		FieldName:  "value",
	}

	resp, err := h.server.Aggregate(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockAggregate_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.AggregateFunc = func(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Aggregate",
			Err:  &errors.MessageError{Message: "invalid aggregation"},
		}
	}

	ctx := context.Background()
	req := &pb.AggregateRequest{
		EntityType: "metrics",
		FieldName:  "value",
		Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
	}

	resp, err := h.server.Aggregate(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Backup Handler Tests
// =============================================================================

func TestServerMockCreateBackup_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.CreateBackupFunc = func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
		return &service.CreateBackupResponse{
			BackupID:  "backup-123",
			Size:      1024 * 1024,
			Timestamp: time.Now().UnixNano(),
			Status:    "completed",
		}, nil
	}

	ctx := context.Background()
	req := &pb.CreateBackupRequest{
		BackupType:  pb.BackupType_BACKUP_TYPE_FULL,
		Compression: pb.CompressionType_COMPRESSION_TYPE_GZIP,
	}

	resp, err := h.server.CreateBackup(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "backup-123", resp.BackupId)
}

func TestServerMockCreateBackup_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.CreateBackupFunc = func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "CreateBackup",
			Err:  &errors.MessageError{Message: "backup failed"},
		}
	}

	ctx := context.Background()
	req := &pb.CreateBackupRequest{}

	resp, err := h.server.CreateBackup(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockListBackups_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.ListBackupsFunc = func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
		return &service.ListBackupsResponse{
			Backups: []*service.BackupMetadata{
				{
					BackupID:  "backup-1",
					Name:      "full",
					Size:      1024,
					Timestamp: time.Now().UnixNano(),
					Shards:    []uint64{1, 2},
				},
			},
			TotalCount: 1,
		}, nil
	}

	ctx := context.Background()
	req := &pb.ListBackupsRequest{Limit: 10}

	resp, err := h.server.ListBackups(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Backups, 1)
}

func TestServerMockRestoreBackup_EmptyBackupID(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.RestoreBackupRequest{
		BackupId: "", // Empty backup ID
	}

	resp, err := h.server.RestoreBackup(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockRestoreBackup_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.RestoreBackupFunc = func(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
		return &service.RestoreBackupResponse{
			Status:   "completed",
			Progress: 100.0,
		}, nil
	}

	ctx := context.Background()
	req := &pb.RestoreBackupRequest{
		BackupId: "backup-123",
	}

	resp, err := h.server.RestoreBackup(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestServerMockDeleteBackup_EmptyBackupID(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.DeleteBackupRequest{
		BackupId: "", // Empty backup ID
	}

	resp, err := h.server.DeleteBackup(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockDeleteBackup_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.DeleteBackupFunc = func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
		return &service.DeleteBackupResponse{Success: true}, nil
	}

	ctx := context.Background()
	req := &pb.DeleteBackupRequest{BackupId: "backup-123"}

	resp, err := h.server.DeleteBackup(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestServerMockVerifyBackup_EmptyBackupID(t *testing.T) {
	h := newMockTestHelper(t)

	ctx := context.Background()
	req := &pb.VerifyBackupRequest{
		BackupId: "", // Empty backup ID
	}

	resp, err := h.server.VerifyBackup(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestServerMockVerifyBackup_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.VerifyBackupFunc = func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
		return &service.VerifyBackupResponse{
			Valid:         true,
			ChecksumMatch: true,
		}, nil
	}

	ctx := context.Background()
	req := &pb.VerifyBackupRequest{BackupId: "backup-123"}

	resp, err := h.server.VerifyBackup(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Valid)
	assert.True(t, resp.ChecksumValid)
}

func TestServerMockVerifyBackup_Invalid(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.VerifyBackupFunc = func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
		return &service.VerifyBackupResponse{
			Valid:         false,
			ChecksumMatch: false,
			Issues:        []string{"corrupted data", "missing shard"},
		}, nil
	}

	ctx := context.Background()
	req := &pb.VerifyBackupRequest{BackupId: "backup-123"}

	resp, err := h.server.VerifyBackup(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Valid)
	assert.Contains(t, resp.Message, "corrupted")
}

// =============================================================================
// ListEntityTypes Handler Tests
// =============================================================================

func TestServerMockListEntityTypes_Success(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.ListEntityTypesFunc = func(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
		return &service.ListEntityTypesResponse{
			EntityTypes:   []string{"user", "order", "product"},
			NextPageToken: "",
			TotalCount:    3,
		}, nil
	}

	ctx := context.Background()
	req := &pb.ListEntityTypesRequest{}

	resp, err := h.server.ListEntityTypes(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.EntityTypes, 3)
	assert.Contains(t, resp.EntityTypes, "user")
}

func TestServerMockListEntityTypes_ServiceError(t *testing.T) {
	h := newMockTestHelper(t)

	h.mock.ListEntityTypesFunc = func(ctx context.Context, req *service.ListEntityTypesRequest) (*service.ListEntityTypesResponse, error) {
		return nil, &errors.QBError{
			Code: errors.ErrTimeout,
			Op:   "ListEntityTypes",
			Err:  &errors.MessageError{Message: "timeout"},
		}
	}

	ctx := context.Background()
	req := &pb.ListEntityTypesRequest{}

	resp, err := h.server.ListEntityTypes(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Server Utility Method Tests
// =============================================================================

func TestServerMockAddress_SetAndGet(t *testing.T) {
	h := newMockTestHelper(t)

	h.server.SetAddress("localhost:9090")
	assert.Equal(t, "localhost:9090", h.server.Address())
}

func TestServerMockQPS_Tracking(t *testing.T) {
	h := newMockTestHelper(t)

	// Initially QPS should be 0
	assert.Equal(t, float64(0), h.server.QPS())

	// Record some queries
	h.server.RecordQuery()
	h.server.RecordQuery()

	// QPS should now be greater than 0
	// Note: Actual value depends on timing, so we just check it works
	h.server.QPS() // Should not panic
}

func TestServerMockSetShardRouter(t *testing.T) {
	h := newMockTestHelper(t)

	// Create a custom shard router
	router := &testMockShardRouter{defaultShard: 5}
	h.server.SetShardRouter(router)

	// Router should be set (we can't easily verify without exposing internal state)
	// This test mainly ensures no panic occurs
}

// testMockShardRouter implements a simple shard router for testing.
type testMockShardRouter struct {
	defaultShard uint64
}

func (m *testMockShardRouter) Route(entityType string, id uint64) uint64 {
	return m.defaultShard
}
