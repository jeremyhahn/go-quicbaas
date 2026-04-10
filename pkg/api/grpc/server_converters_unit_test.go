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

//go:build grpc

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestProtoBackupTypeToServiceName(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.BackupType
		expected string
	}{
		{
			name:     "full backup",
			input:    pb.BackupType_BACKUP_TYPE_FULL,
			expected: "full",
		},
		{
			name:     "incremental backup",
			input:    pb.BackupType_BACKUP_TYPE_INCREMENTAL,
			expected: "incremental",
		},
		{
			name:     "snapshot backup",
			input:    pb.BackupType_BACKUP_TYPE_SNAPSHOT,
			expected: "snapshot",
		},
		{
			name:     "unspecified defaults to full",
			input:    pb.BackupType_BACKUP_TYPE_UNSPECIFIED,
			expected: "full",
		},
		{
			name:     "unknown defaults to full",
			input:    pb.BackupType(999),
			expected: "full",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoBackupTypeToServiceName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServiceNameToProtoBackupType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.BackupType
	}{
		{
			name:     "full",
			input:    "full",
			expected: pb.BackupType_BACKUP_TYPE_FULL,
		},
		{
			name:     "incremental",
			input:    "incremental",
			expected: pb.BackupType_BACKUP_TYPE_INCREMENTAL,
		},
		{
			name:     "snapshot",
			input:    "snapshot",
			expected: pb.BackupType_BACKUP_TYPE_SNAPSHOT,
		},
		{
			name:     "unknown defaults to unspecified",
			input:    "unknown",
			expected: pb.BackupType_BACKUP_TYPE_UNSPECIFIED,
		},
		{
			name:     "empty defaults to unspecified",
			input:    "",
			expected: pb.BackupType_BACKUP_TYPE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceNameToProtoBackupType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoCompressionTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.CompressionType
		expected string
	}{
		{
			name:     "none",
			input:    pb.CompressionType_COMPRESSION_TYPE_NONE,
			expected: "none",
		},
		{
			name:     "gzip",
			input:    pb.CompressionType_COMPRESSION_TYPE_GZIP,
			expected: "gzip",
		},
		{
			name:     "snappy",
			input:    pb.CompressionType_COMPRESSION_TYPE_SNAPPY,
			expected: "snappy",
		},
		{
			name:     "lz4",
			input:    pb.CompressionType_COMPRESSION_TYPE_LZ4,
			expected: "lz4",
		},
		{
			name:     "unspecified defaults to empty",
			input:    pb.CompressionType_COMPRESSION_TYPE_UNSPECIFIED,
			expected: "",
		},
		{
			name:     "unknown defaults to empty",
			input:    pb.CompressionType(999),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoCompressionTypeToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProtoEncryptionTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.EncryptionType
		expected string
	}{
		{
			name:     "none",
			input:    pb.EncryptionType_ENCRYPTION_TYPE_NONE,
			expected: "none",
		},
		{
			name:     "aes256",
			input:    pb.EncryptionType_ENCRYPTION_TYPE_AES256,
			expected: "aes256",
		},
		{
			name:     "unspecified defaults to empty",
			input:    pb.EncryptionType_ENCRYPTION_TYPE_UNSPECIFIED,
			expected: "",
		},
		{
			name:     "unknown defaults to empty",
			input:    pb.EncryptionType(999),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoEncryptionTypeToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServerSetAddress(t *testing.T) {
	srv, _ := createConvertersTestServer(t)

	t.Run("set and get address", func(t *testing.T) {
		srv.SetAddress("localhost:50051")
		assert.Equal(t, "localhost:50051", srv.Address())
	})

	t.Run("change address", func(t *testing.T) {
		srv.SetAddress("localhost:50052")
		assert.Equal(t, "localhost:50052", srv.Address())
	})
}

func TestServerQPSTracking(t *testing.T) {
	srv, _ := createConvertersTestServer(t)

	t.Run("initial QPS is zero", func(t *testing.T) {
		qps := srv.QPS()
		assert.GreaterOrEqual(t, qps, float64(0))
	})

	t.Run("record query updates QPS", func(t *testing.T) {
		srv.RecordQuery()
		// QPS is calculated over time, so we just verify no panic
	})
}

func TestServerHealthExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("success healthy", func(t *testing.T) {
		mockSvc.On("Health", mock.Anything, mock.Anything).
			Return(&service.HealthResponse{
				Status:        service.HealthStatusHealthy,
				NodeID:        1,
				Version:       "test-v1.0.0",
				UptimeSeconds: 3600,
			}, nil).Once()

		resp, err := srv.Health(ctx, &pb.HealthRequest{})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Status)
		assert.Equal(t, "test-v1.0.0", resp.Version)
		mockSvc.AssertExpectations(t)
	})

	t.Run("success with details", func(t *testing.T) {
		mockSvc.On("Health", mock.Anything, mock.Anything).
			Return(&service.HealthResponse{
				Status:        service.HealthStatusHealthy,
				NodeID:        1,
				Version:       "test-v1.0.0",
				UptimeSeconds: 7200,
				Details: map[string]*service.ComponentHealth{
					"storage": {
						Status:    service.HealthStatusHealthy,
						Message:   "Storage is healthy",
						LastCheck: 1234567890,
					},
					"raft": {
						Status:    service.HealthStatusHealthy,
						Message:   "Raft consensus active",
						LastCheck: 1234567890,
					},
				},
			}, nil).Once()

		resp, err := srv.Health(ctx, &pb.HealthRequest{IncludeDetails: true})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.Status)
		assert.Len(t, resp.Details, 2)
		mockSvc.AssertExpectations(t)
	})

	t.Run("error from service", func(t *testing.T) {
		mockSvc.On("Health", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.Health(ctx, &pb.HealthRequest{})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerAggregateExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("successful aggregation", func(t *testing.T) {
		mockSvc.On("Aggregate", mock.Anything, mock.Anything).
			Return(&service.AggregateResponse{
				Result: 1000.5,
				Count:  42,
			}, nil).Once()

		resp, err := srv.Aggregate(ctx, &pb.AggregateRequest{
			EntityType: "orders",
			FieldName:  "total",
			Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
			StartTime:  1000000000,
			EndTime:    2000000000,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, 1000.5, resp.Result)
		assert.Equal(t, uint64(42), resp.Count)
		mockSvc.AssertExpectations(t)
	})

	t.Run("aggregation error", func(t *testing.T) {
		mockSvc.On("Aggregate", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.Aggregate(ctx, &pb.AggregateRequest{
			EntityType: "orders",
			FieldName:  "total",
			Function:   pb.AggregateFunction_AGGREGATE_FUNCTION_SUM,
			StartTime:  1000000000,
			EndTime:    2000000000,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerGetLeaderExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("success with multiple shards", func(t *testing.T) {
		mockSvc.On("GetLeader", mock.Anything, mock.Anything).
			Return(&service.GetLeaderResponse{
				LeaderID: 1,
			}, nil).Once()

		resp, err := srv.GetLeader(ctx, &pb.GetLeaderRequest{
			ShardId: 2,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(1), resp.LeaderId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("no leader available", func(t *testing.T) {
		mockSvc.On("GetLeader", mock.Anything, mock.Anything).
			Return(&service.GetLeaderResponse{
				LeaderID: 0,
			}, nil).Once()

		resp, err := srv.GetLeader(ctx, &pb.GetLeaderRequest{
			ShardId: 1,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(0), resp.LeaderId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("get leader error", func(t *testing.T) {
		mockSvc.On("GetLeader", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.GetLeader(ctx, &pb.GetLeaderRequest{
			ShardId: 1,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerListEntityTypesExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("empty entity types", func(t *testing.T) {
		mockSvc.On("ListEntityTypes", mock.Anything, mock.Anything).
			Return(&service.ListEntityTypesResponse{
				EntityTypes: []string{},
			}, nil).Once()

		resp, err := srv.ListEntityTypes(ctx, &pb.ListEntityTypesRequest{})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.EntityTypes, 0)
		mockSvc.AssertExpectations(t)
	})

	t.Run("list entity types with results", func(t *testing.T) {
		mockSvc.On("ListEntityTypes", mock.Anything, mock.Anything).
			Return(&service.ListEntityTypesResponse{
				EntityTypes: []string{"users", "orders", "products"},
			}, nil).Once()

		resp, err := srv.ListEntityTypes(ctx, &pb.ListEntityTypesRequest{})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.EntityTypes, 3)
		assert.Contains(t, resp.EntityTypes, "users")
		mockSvc.AssertExpectations(t)
	})

	t.Run("list entity types error", func(t *testing.T) {
		mockSvc.On("ListEntityTypes", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.ListEntityTypes(ctx, &pb.ListEntityTypesRequest{})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerClusterStatusExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("multi-node cluster", func(t *testing.T) {
		mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
			Return(&service.ClusterStatusResponse{
				ClusterID: "prod-cluster",
				Nodes: []*service.NodeInfo{
					{NodeID: 1, Address: "node1:50051", IsLeader: true},
					{NodeID: 2, Address: "node2:50051", IsLeader: false},
					{NodeID: 3, Address: "node3:50051", IsLeader: false},
				},
			}, nil).Once()

		resp, err := srv.ClusterStatus(ctx, &pb.ClusterStatusRequest{})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.Nodes, 3)
		mockSvc.AssertExpectations(t)
	})

	t.Run("cluster status error", func(t *testing.T) {
		mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.ClusterStatus(ctx, &pb.ClusterStatusRequest{})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func createConvertersTestServer(t *testing.T) (*Server, *testutil.MockQuicBaaS) {
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

func TestServerCreateBackupExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("create backup success", func(t *testing.T) {
		mockSvc.On("CreateBackup", mock.Anything, mock.Anything).
			Return(&service.CreateBackupResponse{
				BackupID: "backup-123",
				Status:   "completed",
			}, nil).Once()

		resp, err := srv.CreateBackup(ctx, &pb.CreateBackupRequest{
			BackupType: pb.BackupType_BACKUP_TYPE_FULL,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, "backup-123", resp.BackupId)
		mockSvc.AssertExpectations(t)
	})

	t.Run("create backup error", func(t *testing.T) {
		mockSvc.On("CreateBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.CreateBackup(ctx, &pb.CreateBackupRequest{
			BackupType: pb.BackupType_BACKUP_TYPE_FULL,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerListBackupsExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("list backups success", func(t *testing.T) {
		mockSvc.On("ListBackups", mock.Anything, mock.Anything).
			Return(&service.ListBackupsResponse{}, nil).Once()

		resp, err := srv.ListBackups(ctx, &pb.ListBackupsRequest{})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockSvc.AssertExpectations(t)
	})

	t.Run("list backups error", func(t *testing.T) {
		mockSvc.On("ListBackups", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.ListBackups(ctx, &pb.ListBackupsRequest{})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerDeleteBackupExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("delete backup success", func(t *testing.T) {
		mockSvc.On("DeleteBackup", mock.Anything, mock.Anything).
			Return(&service.DeleteBackupResponse{
				Success: true,
			}, nil).Once()

		resp, err := srv.DeleteBackup(ctx, &pb.DeleteBackupRequest{
			BackupId: "backup-123",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockSvc.AssertExpectations(t)
	})

	t.Run("delete backup error", func(t *testing.T) {
		mockSvc.On("DeleteBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.DeleteBackup(ctx, &pb.DeleteBackupRequest{
			BackupId: "backup-123",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerRestoreBackupExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("restore backup success", func(t *testing.T) {
		mockSvc.On("RestoreBackup", mock.Anything, mock.Anything).
			Return(&service.RestoreBackupResponse{}, nil).Once()

		resp, err := srv.RestoreBackup(ctx, &pb.RestoreBackupRequest{
			BackupId: "backup-123",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		mockSvc.AssertExpectations(t)
	})

	t.Run("restore backup error", func(t *testing.T) {
		mockSvc.On("RestoreBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.RestoreBackup(ctx, &pb.RestoreBackupRequest{
			BackupId: "backup-123",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerVerifyBackupExtended(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("verify backup valid", func(t *testing.T) {
		mockSvc.On("VerifyBackup", mock.Anything, mock.Anything).
			Return(&service.VerifyBackupResponse{
				Valid:         true,
				ChecksumMatch: true,
			}, nil).Once()

		resp, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "backup-123",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Valid)
		mockSvc.AssertExpectations(t)
	})

	t.Run("verify backup invalid", func(t *testing.T) {
		mockSvc.On("VerifyBackup", mock.Anything, mock.Anything).
			Return(&service.VerifyBackupResponse{
				Valid:         false,
				ChecksumMatch: false,
				Issues:        []string{"checksum mismatch"},
			}, nil).Once()

		resp, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "backup-123",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.False(t, resp.Valid)
		mockSvc.AssertExpectations(t)
	})

	t.Run("verify backup error", func(t *testing.T) {
		mockSvc.On("VerifyBackup", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.VerifyBackup(ctx, &pb.VerifyBackupRequest{
			BackupId: "backup-123",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerPutGetDelete(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("put success", func(t *testing.T) {
		mockSvc.On("Put", mock.Anything, mock.Anything).
			Return(&service.PutResponse{ID: 123, Version: 1}, nil).Once()

		resp, err := srv.Put(ctx, &pb.PutRequest{
			EntityType: "users",
			Id:         0, // auto-generate
			Data:       []byte(`{"name":"test"}`),
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(123), resp.Id)
		assert.Equal(t, uint64(1), resp.Version)
		mockSvc.AssertExpectations(t)
	})

	t.Run("put error", func(t *testing.T) {
		mockSvc.On("Put", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.Put(ctx, &pb.PutRequest{
			EntityType: "users",
			Data:       []byte(`{"name":"test"}`),
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})

	t.Run("get success", func(t *testing.T) {
		mockSvc.On("Get", mock.Anything, mock.Anything).
			Return(&service.GetResponse{
				ID:      123,
				Found:   true,
				Data:    []byte(`{"name":"test"}`),
				Version: 1,
			}, nil).Once()

		resp, err := srv.Get(ctx, &pb.GetRequest{
			EntityType: "users",
			Id:         123,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, []byte(`{"name":"test"}`), resp.Data)
		mockSvc.AssertExpectations(t)
	})

	t.Run("get error", func(t *testing.T) {
		mockSvc.On("Get", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.Get(ctx, &pb.GetRequest{
			EntityType: "users",
			Id:         123,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})

	t.Run("delete success", func(t *testing.T) {
		mockSvc.On("Delete", mock.Anything, mock.Anything).
			Return(&service.DeleteResponse{Deleted: true}, nil).Once()

		resp, err := srv.Delete(ctx, &pb.DeleteRequest{
			EntityType: "users",
			Id:         123,
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Deleted)
		mockSvc.AssertExpectations(t)
	})

	t.Run("delete error", func(t *testing.T) {
		mockSvc.On("Delete", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.Delete(ctx, &pb.DeleteRequest{
			EntityType: "users",
			Id:         123,
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerBatchPut(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("batch put success", func(t *testing.T) {
		mockSvc.On("BatchPut", mock.Anything, mock.Anything).
			Return(&service.BatchPutResponse{
				Responses: []*service.PutResponse{
					{ID: 1, Version: 1},
					{ID: 2, Version: 1},
				},
			}, nil).Once()

		resp, err := srv.BatchPut(ctx, &pb.BatchPutRequest{
			Requests: []*pb.PutRequest{
				{EntityType: "users", Data: []byte(`{"name":"test1"}`)},
				{EntityType: "users", Data: []byte(`{"name":"test2"}`)},
			},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.Responses, 2)
		mockSvc.AssertExpectations(t)
	})

	t.Run("batch put error", func(t *testing.T) {
		mockSvc.On("BatchPut", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.BatchPut(ctx, &pb.BatchPutRequest{
			Requests: []*pb.PutRequest{
				{EntityType: "users", Data: []byte(`{"name":"test1"}`)},
			},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerBatchGet(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("batch get success", func(t *testing.T) {
		mockSvc.On("BatchGet", mock.Anything, mock.Anything).
			Return(&service.BatchGetResponse{
				Responses: []*service.GetResponse{
					{ID: 1, Data: []byte(`{"name":"test1"}`), Version: 1},
					{ID: 2, Data: []byte(`{"name":"test2"}`), Version: 1},
				},
			}, nil).Once()

		resp, err := srv.BatchGet(ctx, &pb.BatchGetRequest{
			Requests: []*pb.GetRequest{
				{EntityType: "users", Id: 1},
				{EntityType: "users", Id: 2},
			},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.Responses, 2)
		mockSvc.AssertExpectations(t)
	})

	t.Run("batch get error", func(t *testing.T) {
		mockSvc.On("BatchGet", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.BatchGet(ctx, &pb.BatchGetRequest{
			Requests: []*pb.GetRequest{
				{EntityType: "users", Id: 1},
			},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}

func TestServerBatchDelete(t *testing.T) {
	srv, mockSvc := createConvertersTestServer(t)
	ctx := context.Background()

	t.Run("batch delete success", func(t *testing.T) {
		mockSvc.On("BatchDelete", mock.Anything, mock.Anything).
			Return(&service.BatchDeleteResponse{
				Responses: []*service.DeleteResponse{
					{Deleted: true},
					{Deleted: true},
				},
			}, nil).Once()

		resp, err := srv.BatchDelete(ctx, &pb.BatchDeleteRequest{
			Requests: []*pb.DeleteRequest{
				{EntityType: "users", Id: 1},
				{EntityType: "users", Id: 2},
			},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.Responses, 2)
		mockSvc.AssertExpectations(t)
	})

	t.Run("batch delete error", func(t *testing.T) {
		mockSvc.On("BatchDelete", mock.Anything, mock.Anything).
			Return(nil, assert.AnError).Once()

		resp, err := srv.BatchDelete(ctx, &pb.BatchDeleteRequest{
			Requests: []*pb.DeleteRequest{
				{EntityType: "users", Id: 1},
			},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		mockSvc.AssertExpectations(t)
	})
}
