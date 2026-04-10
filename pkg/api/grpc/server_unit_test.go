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
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// Test helpers.
func createTestServerWithMockService(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
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

	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	require.NoError(t, err)
	return srv, mockSvc
}

// TestNewServer tests server creation.
func TestNewServer(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()

	srv, err := NewServer(mockSvc, log, 1, "test-v1.0.0")
	assert.NoError(t, err)
	assert.NotNil(t, srv)
	assert.Equal(t, uint64(1), srv.nodeID)
	assert.Equal(t, "test-v1.0.0", srv.version)

	// Test with nil service
	_, err = NewServer(nil, log, 1, "test-v1.0.0")
	assert.Error(t, err)

	// Test with nil logger
	_, err = NewServer(mockSvc, nil, 1, "test-v1.0.0")
	assert.Error(t, err)
}

// TestPut tests the Put operation.
func TestPut(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful put", func(t *testing.T) {
		mockSvc.On("Put", mock.Anything, mock.MatchedBy(func(req *service.PutRequest) bool {
			return req.EntityType == "user" && req.ID == 42
		})).Return(&service.PutResponse{
			ID:        42,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
			LeaderID:  1,
		}, nil).Once()

		resp, err := srv.Put(context.Background(), &pb.PutRequest{
			EntityType:  "user",
			Id:          42,
			Data:        []byte(`{"name":"alice"}`),
			Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
		})

		require.NoError(t, err)
		assert.Equal(t, uint64(42), resp.Id)
		assert.Equal(t, uint64(1), resp.Version)
		mockSvc.AssertExpectations(t)
	})

	t.Run("put with invalid request", func(t *testing.T) {
		_, err := srv.Put(context.Background(), &pb.PutRequest{
			EntityType: "", // Invalid: empty entity type
			Id:         42,
			Data:       []byte(`{}`),
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("put with service error", func(t *testing.T) {
		mockSvc.On("Put", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		_, err := srv.Put(context.Background(), &pb.PutRequest{
			EntityType: "user",
			Id:         42,
			Data:       []byte(`{}`),
		})
		assert.Error(t, err)
		mockSvc.AssertExpectations(t)
	})
}

// TestGet tests the Get operation.
func TestGet(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful get", func(t *testing.T) {
		mockSvc.On("Get", mock.Anything, mock.MatchedBy(func(req *service.GetRequest) bool {
			return req.EntityType == "user" && req.ID == 42
		})).Return(&service.GetResponse{
			ID:        42,
			Data:      []byte(`{"name":"alice"}`),
			Found:     true,
			Version:   1,
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

		resp, err := srv.Get(context.Background(), &pb.GetRequest{
			EntityType:  "user",
			Id:          42,
			Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
		})

		require.NoError(t, err)
		assert.Equal(t, uint64(42), resp.Id)
		assert.True(t, resp.Found)
		assert.Equal(t, []byte(`{"name":"alice"}`), resp.Data)
		mockSvc.AssertExpectations(t)
	})

	t.Run("get not found", func(t *testing.T) {
		mockSvc.On("Get", mock.Anything, mock.Anything).
			Return(&service.GetResponse{
				ID:    42,
				Found: false,
			}, nil).Once()

		_, err := srv.Get(context.Background(), &pb.GetRequest{
			EntityType: "user",
			Id:         42,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
		mockSvc.AssertExpectations(t)
	})

	t.Run("get with invalid request", func(t *testing.T) {
		_, err := srv.Get(context.Background(), &pb.GetRequest{
			EntityType: "", // Invalid: empty entity type
			Id:         42,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// TestDelete tests the Delete operation.
func TestDelete(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful delete", func(t *testing.T) {
		mockSvc.On("Delete", mock.Anything, mock.MatchedBy(func(req *service.DeleteRequest) bool {
			return req.EntityType == "user" && req.ID == 42
		})).Return(&service.DeleteResponse{
			Deleted:   true,
			Timestamp: time.Now().UnixNano(),
		}, nil).Once()

		resp, err := srv.Delete(context.Background(), &pb.DeleteRequest{
			EntityType:  "user",
			Id:          42,
			Consistency: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
		})

		require.NoError(t, err)
		assert.True(t, resp.Deleted)
		mockSvc.AssertExpectations(t)
	})

	t.Run("delete with invalid request", func(t *testing.T) {
		_, err := srv.Delete(context.Background(), &pb.DeleteRequest{
			EntityType: "", // Invalid: empty entity type
			Id:         42,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// TestBatchPut tests the BatchPut operation.
func TestBatchPut(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful batch put", func(t *testing.T) {
		mockSvc.On("BatchPut", mock.Anything, mock.Anything).
			Return(&service.BatchPutResponse{
				Responses: []*service.PutResponse{
					{ID: 1, Version: 1, Timestamp: time.Now().UnixNano()},
					{ID: 2, Version: 1, Timestamp: time.Now().UnixNano()},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil).Once()

		resp, err := srv.BatchPut(context.Background(), &pb.BatchPutRequest{
			Requests: []*pb.PutRequest{
				{EntityType: "user", Id: 1, Data: []byte(`{"name":"alice"}`)},
				{EntityType: "user", Id: 2, Data: []byte(`{"name":"bob"}`)},
			},
		})

		require.NoError(t, err)
		assert.Len(t, resp.Responses, 2)
		mockSvc.AssertExpectations(t)
	})

	t.Run("batch put with empty requests", func(t *testing.T) {
		_, err := srv.BatchPut(context.Background(), &pb.BatchPutRequest{
			Requests: []*pb.PutRequest{},
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// TestBatchGet tests the BatchGet operation.
func TestBatchGet(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful batch get", func(t *testing.T) {
		mockSvc.On("BatchGet", mock.Anything, mock.Anything).
			Return(&service.BatchGetResponse{
				Responses: []*service.GetResponse{
					{ID: 1, Data: []byte(`{"name":"alice"}`), Found: true},
					{ID: 2, Data: []byte(`{"name":"bob"}`), Found: true},
				},
			}, nil).Once()

		resp, err := srv.BatchGet(context.Background(), &pb.BatchGetRequest{
			Requests: []*pb.GetRequest{
				{EntityType: "user", Id: 1},
				{EntityType: "user", Id: 2},
			},
		})

		require.NoError(t, err)
		assert.Len(t, resp.Responses, 2)
		mockSvc.AssertExpectations(t)
	})
}

// TestBatchDelete tests the BatchDelete operation.
func TestBatchDelete(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful batch delete", func(t *testing.T) {
		mockSvc.On("BatchDelete", mock.Anything, mock.Anything).
			Return(&service.BatchDeleteResponse{
				Responses: []*service.DeleteResponse{
					{Deleted: true, Timestamp: time.Now().UnixNano()},
					{Deleted: true, Timestamp: time.Now().UnixNano()},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil).Once()

		resp, err := srv.BatchDelete(context.Background(), &pb.BatchDeleteRequest{
			Requests: []*pb.DeleteRequest{
				{EntityType: "user", Id: 1},
				{EntityType: "user", Id: 2},
			},
		})

		require.NoError(t, err)
		assert.Len(t, resp.Responses, 2)
		mockSvc.AssertExpectations(t)
	})
}

// TestClusterStatus tests the ClusterStatus operation.
func TestClusterStatus(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful cluster status", func(t *testing.T) {
		mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
			Return(&service.ClusterStatusResponse{
				ClusterID: "test-cluster",
				Nodes: []*service.NodeInfo{
					{NodeID: 1, IsLeader: true, Version: "1.0.0", HealthStatus: service.HealthStatusHealthy},
					{NodeID: 2, IsLeader: false, Version: "1.0.0", HealthStatus: service.HealthStatusHealthy},
				},
				Shards: []*service.ShardInfo{
					{ShardID: 1, LeaderID: 1, Replicas: []uint64{1, 2}, Status: service.HealthStatusHealthy},
				},
				Timestamp: time.Now().UnixNano(),
			}, nil).Once()

		resp, err := srv.ClusterStatus(context.Background(), &pb.ClusterStatusRequest{
			IncludeMetrics: true,
		})

		require.NoError(t, err)
		assert.Equal(t, "test-cluster", resp.ClusterId)
		assert.Len(t, resp.Nodes, 2)
		assert.Len(t, resp.Shards, 1)
		mockSvc.AssertExpectations(t)
	})
}

// TestGetLeader tests the GetLeader operation.
func TestGetLeader(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful get leader", func(t *testing.T) {
		mockSvc.On("GetLeader", mock.Anything, mock.MatchedBy(func(req *service.GetLeaderRequest) bool {
			return req.ShardID == 1
		})).Return(&service.GetLeaderResponse{
			ShardID:  1,
			LeaderID: 1,
			Address:  "127.0.0.1:9000",
		}, nil).Once()

		resp, err := srv.GetLeader(context.Background(), &pb.GetLeaderRequest{
			ShardId: 1,
		})

		require.NoError(t, err)
		assert.Equal(t, uint64(1), resp.ShardId)
		assert.Equal(t, uint64(1), resp.LeaderId)
		mockSvc.AssertExpectations(t)
	})
}

// TestHealth tests the Health operation.
func TestHealth(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful health check", func(t *testing.T) {
		mockSvc.On("Health", mock.Anything, mock.Anything).
			Return(&service.HealthResponse{
				Status:        service.HealthStatusHealthy,
				NodeID:        1,
				Version:       "1.0.0",
				UptimeSeconds: 100,
				Details: map[string]*service.ComponentHealth{
					"storage": {
						Status:    service.HealthStatusHealthy,
						Message:   "OK",
						LastCheck: time.Now().UnixNano(),
					},
				},
			}, nil).Once()

		resp, err := srv.Health(context.Background(), &pb.HealthRequest{
			IncludeDetails: true,
		})

		require.NoError(t, err)
		assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Status)
		assert.Equal(t, uint64(1), resp.NodeId)
		assert.Len(t, resp.Details, 1)
		mockSvc.AssertExpectations(t)
	})
}

// TestAggregate tests the Aggregate operation.
func TestAggregate(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful aggregate", func(t *testing.T) {
		mockSvc.On("Aggregate", mock.Anything, mock.Anything).
			Return(&service.AggregateResponse{
				Result:    100.5,
				Count:     10,
				StartTime: time.Now().Add(-1 * time.Hour).UnixNano(),
				EndTime:   time.Now().UnixNano(),
			}, nil).Once()

		resp, err := srv.Aggregate(context.Background(), &pb.AggregateRequest{
			EntityType: "metrics",
			FieldName:  "cpu_usage",
			Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
			StartTime:  time.Now().Add(-1 * time.Hour).UnixNano(),
			EndTime:    time.Now().UnixNano(),
		})

		require.NoError(t, err)
		assert.Equal(t, float64(100.5), resp.Result)
		assert.Equal(t, uint64(10), resp.Count)
		mockSvc.AssertExpectations(t)
	})

	t.Run("aggregate with invalid request", func(t *testing.T) {
		_, err := srv.Aggregate(context.Background(), &pb.AggregateRequest{
			EntityType: "", // Invalid: empty entity type
			FieldName:  "cpu_usage",
			Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_AVG,
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// TestTransaction tests the Transaction operation.
func TestTransaction(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)

	t.Run("successful transaction", func(t *testing.T) {
		mockSvc.On("Transaction", mock.Anything, mock.Anything).
			Return(&service.TransactionResponse{
				Succeeded: true,
				Responses: []*service.TransactionOpResponse{
					{
						OpType:     service.TransactionOpTypePut,
						EntityType: "user",
						ID:         42,
						Success:    true,
						Version:    2,
						Timestamp:  time.Now().UnixNano(),
					},
				},
				Timestamp: time.Now().UnixNano(),
				Message:   "Transaction succeeded",
				Revision:  1,
			}, nil).Once()

		resp, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{
				{
					EntityType: "user",
					Id:         42,
					Target:     pb.CompareTarget_COMPARE_TARGET_VERSION,
					Operator:   pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
					Version:    1,
				},
			},
			Success: []*pb.TransactionOp{
				{
					OpType:     pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
					EntityType: "user",
					Id:         42,
					Data:       []byte(`{"name":"updated"}`),
				},
			},
		})

		require.NoError(t, err)
		assert.True(t, resp.Succeeded)
		assert.Len(t, resp.Responses, 1)
		mockSvc.AssertExpectations(t)
	})

	t.Run("transaction with invalid request", func(t *testing.T) {
		_, err := srv.Transaction(context.Background(), &pb.TransactionRequest{
			Compare: []*pb.CompareCondition{}, // Invalid: no conditions
			Success: []*pb.TransactionOp{},
		})
		assert.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// TestExtractWriteMode tests write mode extraction from metadata.
func TestExtractWriteMode(t *testing.T) {
	tests := []struct {
		name     string
		mdValue  string
		expected service.WriteMode
	}{
		{"default (no metadata)", "", service.WriteModeSync},
		{"sync mode", "sync", service.WriteModeSync},
		{"batched mode", "batched", service.WriteModeBatched},
		{"wal_durable mode", "wal_durable", service.WriteModeWALDurable},
		{"unknown mode", "unknown", service.WriteModeSync},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.mdValue != "" {
				md := metadata.Pairs("write-mode", tt.mdValue)
				ctx = metadata.NewIncomingContext(ctx, md)
			}

			mode := extractWriteMode(ctx)
			assert.Equal(t, tt.expected, mode)
		})
	}
}

// TestExtractProtocol tests protocol extraction from metadata.
func TestExtractProtocol(t *testing.T) {
	tests := []struct {
		name     string
		mdValue  string
		expected protocol
	}{
		{"default (no metadata)", "", protocolJSON},
		{"json protocol", "json", protocolJSON},
		{"binary protocol", "binary", protocolBinary},
		{"unknown protocol", "unknown", protocolJSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.mdValue != "" {
				md := metadata.Pairs("x-quicbaas-protocol", tt.mdValue)
				ctx = metadata.NewIncomingContext(ctx, md)
			}

			proto := extractProtocol(ctx)
			assert.Equal(t, tt.expected, proto)
		})
	}
}

// TestProtocolMethods tests protocol helper methods.
func TestProtocolMethods(t *testing.T) {
	jsonProto := protocolJSON
	binaryProto := protocolBinary

	assert.True(t, jsonProto.IsJSON())
	assert.False(t, jsonProto.IsBinary())

	assert.True(t, binaryProto.IsBinary())
	assert.False(t, binaryProto.IsJSON())
}

// TestServerMetrics tests that server properly initializes metrics.
func TestServerMetrics(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	assert.NotNil(t, srv.metrics)
	assert.NotNil(t, srv.metrics.requestsTotal)
	assert.NotNil(t, srv.metrics.requestDuration)
	assert.NotNil(t, srv.metrics.activeStreams)
	assert.NotNil(t, srv.metrics.batchSizeHist)
	assert.NotNil(t, srv.metrics.consistencyLevel)
}

// TestServerStartTime tests that server tracks start time.
func TestServerStartTime(t *testing.T) {
	srv, _ := createTestServerWithMockService(t)
	assert.False(t, srv.startTime.IsZero())
	assert.True(t, srv.startTime.Before(time.Now()))
}
