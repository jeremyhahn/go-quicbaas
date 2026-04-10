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

package testutil

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errTest is a typed error used for test expectations.
var errTest = errors.New("test error")

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewMockQuicBaaS(t *testing.T) {
	t.Run("returns_non_nil_mock", func(t *testing.T) {
		m := NewMockQuicBaaS()
		require.NotNil(t, m)
	})

	t.Run("fields_default_to_nil", func(t *testing.T) {
		m := NewMockQuicBaaS()
		assert.Nil(t, m.CertificateSvc)
		assert.Nil(t, m.EmbeddingService)
		assert.Nil(t, m.OllamaService)
		assert.Nil(t, m.CryptoService)
		assert.Nil(t, m.DynamicSecretsSvc)
		assert.Nil(t, m.KMSService)
		assert.Nil(t, m.ParameterStoreSvc)
		assert.Nil(t, m.VectorService)
		assert.Nil(t, m.ClusterDEKSvc)
		assert.False(t, m.DisableKMS)
	})
}

// ---------------------------------------------------------------------------
// Sub-Service Accessor Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_KMS_DefaultReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	kms := m.KMS()
	assert.NotNil(t, kms)
	// When no override is set, the accessor returns the mock itself.
	assert.Equal(t, m, kms)
}

func TestMockQuicBaaS_KMS_WithOverride(t *testing.T) {
	m := NewMockQuicBaaS()
	override := NewMockQuicBaaS()
	m.KMSService = override
	kms := m.KMS()
	assert.Equal(t, override, kms)
}

func TestMockQuicBaaS_KMS_DisabledReturnsNil(t *testing.T) {
	m := NewMockQuicBaaS()
	m.DisableKMS = true
	assert.Nil(t, m.KMS())
}

func TestMockQuicBaaS_KMS_DisabledTakesPrecedenceOverOverride(t *testing.T) {
	m := NewMockQuicBaaS()
	m.KMSService = NewMockQuicBaaS()
	m.DisableKMS = true
	assert.Nil(t, m.KMS())
}

func TestMockQuicBaaS_Secrets_ReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.Secrets())
}

func TestMockQuicBaaS_Crypto_DefaultReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.Crypto())
}

func TestMockQuicBaaS_Crypto_WithOverride(t *testing.T) {
	m := NewMockQuicBaaS()
	override := NewMockQuicBaaS()
	m.CryptoService = override
	assert.Equal(t, override, m.Crypto())
}

func TestMockQuicBaaS_Vector_DefaultReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.Vector())
}

func TestMockQuicBaaS_Vector_WithOverride(t *testing.T) {
	m := NewMockQuicBaaS()
	override := NewMockQuicBaaS()
	m.VectorService = override
	assert.Equal(t, override, m.Vector())
}

func TestMockQuicBaaS_Embedding_ReturnsNilByDefault(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Nil(t, m.Embedding())
}

func TestMockQuicBaaS_Ollama_ReturnsNilByDefault(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Nil(t, m.Ollama())
}

func TestMockQuicBaaS_Certificate_ReturnsNilByDefault(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Nil(t, m.Certificate())
}

func TestMockQuicBaaS_DynamicSecrets_ReturnsNilByDefault(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Nil(t, m.DynamicSecrets())
}

func TestMockQuicBaaS_ClusterDEK_ReturnsNilByDefault(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Nil(t, m.ClusterDEK())
}

func TestMockQuicBaaS_ClusterDEK_WithOverride(t *testing.T) {
	m := NewMockQuicBaaS()
	override := NewMockClusterDEKService()
	m.ClusterDEKSvc = override
	assert.Equal(t, override, m.ClusterDEK())
}

func TestMockQuicBaaS_ObjectStorageService_ReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.ObjectStorageService())
}

func TestMockQuicBaaS_ObjectStorageAdminService_ReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.ObjectStorageAdminService())
}

func TestMockQuicBaaS_ParameterStoreService_DefaultReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.ParameterStoreService())
}

func TestMockQuicBaaS_ParameterStoreService_WithOverride(t *testing.T) {
	m := NewMockQuicBaaS()
	override := NewMockQuicBaaS()
	m.ParameterStoreSvc = override
	assert.Equal(t, override, m.ParameterStoreService())
}

func TestMockQuicBaaS_ObjectTransferService_ReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.ObjectTransferService())
}

func TestMockQuicBaaS_Scheduler_ReturnsSelf(t *testing.T) {
	m := NewMockQuicBaaS()
	assert.Equal(t, m, m.Scheduler())
}

// ---------------------------------------------------------------------------
// KV Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Put_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.PutRequest{EntityType: "test", Data: []byte("v1")}
	expected := &service.PutResponse{}
	m.On("Put", ctx, req).Return(expected, nil)

	resp, err := m.Put(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Put_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.PutRequest{EntityType: "test"}
	m.On("Put", ctx, req).Return((*service.PutResponse)(nil), errTest)

	resp, err := m.Put(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Get_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRequest{EntityType: "test", ID: 1}
	expected := &service.GetResponse{Data: []byte("v1")}
	m.On("Get", ctx, req).Return(expected, nil)

	resp, err := m.Get(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Get_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRequest{EntityType: "missing"}
	m.On("Get", ctx, req).Return((*service.GetResponse)(nil), errTest)

	resp, err := m.Get(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Delete_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteRequest{EntityType: "test", ID: 1}
	expected := &service.DeleteResponse{}
	m.On("Delete", ctx, req).Return(expected, nil)

	resp, err := m.Delete(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Delete_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteRequest{EntityType: "missing"}
	m.On("Delete", ctx, req).Return((*service.DeleteResponse)(nil), errTest)

	resp, err := m.Delete(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_List_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListRequest{EntityType: "test"}
	stream := make(chan<- *service.ListResponse, 1)
	m.On("List", ctx, req, stream).Return(nil)

	err := m.List(ctx, req, stream)
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_List_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListRequest{}
	stream := make(chan<- *service.ListResponse, 1)
	m.On("List", ctx, req, stream).Return(errTest)

	err := m.List(ctx, req, stream)
	assert.ErrorIs(t, err, errTest)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Watch_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WatchRequest{}
	stream := make(chan<- *service.WatchResponse, 1)
	m.On("Watch", ctx, req, stream).Return(int64(42), nil)

	revision, err := m.Watch(ctx, req, stream)
	assert.NoError(t, err)
	assert.Equal(t, int64(42), revision)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Watch_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WatchRequest{}
	stream := make(chan<- *service.WatchResponse, 1)
	m.On("Watch", ctx, req, stream).Return(int64(0), errTest)

	revision, err := m.Watch(ctx, req, stream)
	assert.ErrorIs(t, err, errTest)
	assert.Equal(t, int64(0), revision)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListEntityTypes_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListEntityTypesRequest{}
	expected := &service.ListEntityTypesResponse{}
	m.On("ListEntityTypes", ctx, req).Return(expected, nil)

	resp, err := m.ListEntityTypes(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListEntityTypes_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListEntityTypesRequest{}
	m.On("ListEntityTypes", ctx, req).Return((*service.ListEntityTypesResponse)(nil), errTest)

	resp, err := m.ListEntityTypes(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Batch Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_BatchPut_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchPutRequest{}
	expected := &service.BatchPutResponse{}
	m.On("BatchPut", ctx, req).Return(expected, nil)

	resp, err := m.BatchPut(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchPut_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchPutRequest{}
	m.On("BatchPut", ctx, req).Return((*service.BatchPutResponse)(nil), errTest)

	resp, err := m.BatchPut(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchGet_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchGetRequest{}
	expected := &service.BatchGetResponse{}
	m.On("BatchGet", ctx, req).Return(expected, nil)

	resp, err := m.BatchGet(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchGet_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchGetRequest{}
	m.On("BatchGet", ctx, req).Return((*service.BatchGetResponse)(nil), errTest)

	resp, err := m.BatchGet(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchDelete_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchDeleteRequest{}
	expected := &service.BatchDeleteResponse{}
	m.On("BatchDelete", ctx, req).Return(expected, nil)

	resp, err := m.BatchDelete(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_BatchDelete_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.BatchDeleteRequest{}
	m.On("BatchDelete", ctx, req).Return((*service.BatchDeleteResponse)(nil), errTest)

	resp, err := m.BatchDelete(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Transaction Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Transaction_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TransactionRequest{}
	expected := &service.TransactionResponse{}
	m.On("Transaction", ctx, req).Return(expected, nil)

	resp, err := m.Transaction(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Transaction_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TransactionRequest{}
	m.On("Transaction", ctx, req).Return((*service.TransactionResponse)(nil), errTest)

	resp, err := m.Transaction(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Query Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Query_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.QueryRequest{}
	stream := make(chan<- *service.QueryResponse, 1)
	m.On("Query", ctx, req, stream).Return(nil)

	err := m.Query(ctx, req, stream)
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Query_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.QueryRequest{}
	stream := make(chan<- *service.QueryResponse, 1)
	m.On("Query", ctx, req, stream).Return(errTest)

	err := m.Query(ctx, req, stream)
	assert.ErrorIs(t, err, errTest)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ScanIndex_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ScanRequest{}
	stream := make(chan<- *service.ScanResponse, 1)
	m.On("ScanIndex", ctx, req, stream).Return(nil)

	err := m.ScanIndex(ctx, req, stream)
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ScanIndex_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ScanRequest{}
	stream := make(chan<- *service.ScanResponse, 1)
	m.On("ScanIndex", ctx, req, stream).Return(errTest)

	err := m.ScanIndex(ctx, req, stream)
	assert.ErrorIs(t, err, errTest)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Cluster Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_ClusterStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClusterStatusRequest{}
	expected := &service.ClusterStatusResponse{}
	m.On("ClusterStatus", ctx, req).Return(expected, nil)

	resp, err := m.ClusterStatus(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ClusterStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClusterStatusRequest{}
	m.On("ClusterStatus", ctx, req).Return((*service.ClusterStatusResponse)(nil), errTest)

	resp, err := m.ClusterStatus(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetLeader_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetLeaderRequest{}
	expected := &service.GetLeaderResponse{}
	m.On("GetLeader", ctx, req).Return(expected, nil)

	resp, err := m.GetLeader(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetLeader_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetLeaderRequest{}
	m.On("GetLeader", ctx, req).Return((*service.GetLeaderResponse)(nil), errTest)

	resp, err := m.GetLeader(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_AddNode_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AddNodeRequest{}
	expected := &service.AddNodeResponse{}
	m.On("AddNode", ctx, req).Return(expected, nil)

	resp, err := m.AddNode(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_AddNode_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AddNodeRequest{}
	m.On("AddNode", ctx, req).Return((*service.AddNodeResponse)(nil), errTest)

	resp, err := m.AddNode(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RemoveNode_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RemoveNodeRequest{}
	expected := &service.RemoveNodeResponse{}
	m.On("RemoveNode", ctx, req).Return(expected, nil)

	resp, err := m.RemoveNode(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RemoveNode_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RemoveNodeRequest{}
	m.On("RemoveNode", ctx, req).Return((*service.RemoveNodeResponse)(nil), errTest)

	resp, err := m.RemoveNode(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateShard_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateShardRequest{}
	expected := &service.CreateShardResponse{}
	m.On("CreateShard", ctx, req).Return(expected, nil)

	resp, err := m.CreateShard(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateShard_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateShardRequest{}
	m.On("CreateShard", ctx, req).Return((*service.CreateShardResponse)(nil), errTest)

	resp, err := m.CreateShard(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RebalanceCluster_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RebalanceClusterRequest{}
	expected := &service.RebalanceClusterResponse{}
	m.On("RebalanceCluster", ctx, req).Return(expected, nil)

	resp, err := m.RebalanceCluster(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RebalanceCluster_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RebalanceClusterRequest{}
	m.On("RebalanceCluster", ctx, req).Return((*service.RebalanceClusterResponse)(nil), errTest)

	resp, err := m.RebalanceCluster(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRebalanceProgress_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRebalanceProgressRequest{}
	expected := &service.GetRebalanceProgressResponse{}
	m.On("GetRebalanceProgress", ctx, req).Return(expected, nil)

	resp, err := m.GetRebalanceProgress(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRebalanceProgress_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetRebalanceProgressRequest{}
	m.On("GetRebalanceProgress", ctx, req).Return((*service.GetRebalanceProgressResponse)(nil), errTest)

	resp, err := m.GetRebalanceProgress(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Backup Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_CreateBackup_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateBackupRequest{}
	expected := &service.CreateBackupResponse{}
	m.On("CreateBackup", ctx, req).Return(expected, nil)

	resp, err := m.CreateBackup(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateBackup_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.CreateBackupRequest{}
	m.On("CreateBackup", ctx, req).Return((*service.CreateBackupResponse)(nil), errTest)

	resp, err := m.CreateBackup(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListBackups_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListBackupsRequest{}
	expected := &service.ListBackupsResponse{}
	m.On("ListBackups", ctx, req).Return(expected, nil)

	resp, err := m.ListBackups(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListBackups_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ListBackupsRequest{}
	m.On("ListBackups", ctx, req).Return((*service.ListBackupsResponse)(nil), errTest)

	resp, err := m.ListBackups(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RestoreBackup_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RestoreBackupRequest{}
	expected := &service.RestoreBackupResponse{}
	m.On("RestoreBackup", ctx, req).Return(expected, nil)

	resp, err := m.RestoreBackup(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RestoreBackup_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RestoreBackupRequest{}
	m.On("RestoreBackup", ctx, req).Return((*service.RestoreBackupResponse)(nil), errTest)

	resp, err := m.RestoreBackup(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteBackup_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteBackupRequest{}
	expected := &service.DeleteBackupResponse{}
	m.On("DeleteBackup", ctx, req).Return(expected, nil)

	resp, err := m.DeleteBackup(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteBackup_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteBackupRequest{}
	m.On("DeleteBackup", ctx, req).Return((*service.DeleteBackupResponse)(nil), errTest)

	resp, err := m.DeleteBackup(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_VerifyBackup_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyBackupRequest{}
	expected := &service.VerifyBackupResponse{}
	m.On("VerifyBackup", ctx, req).Return(expected, nil)

	resp, err := m.VerifyBackup(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_VerifyBackup_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.VerifyBackupRequest{}
	m.On("VerifyBackup", ctx, req).Return((*service.VerifyBackupResponse)(nil), errTest)

	resp, err := m.VerifyBackup(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Auth Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Login_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.LoginRequest{}
	expected := &service.LoginResponse{AccessToken: "test-token"}
	m.On("Login", ctx, req).Return(expected, nil)

	resp, err := m.Login(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Login_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.LoginRequest{}
	m.On("Login", ctx, req).Return((*service.LoginResponse)(nil), errTest)

	resp, err := m.Login(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Logout_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.LogoutRequest{}
	expected := &service.LogoutResponse{}
	m.On("Logout", ctx, req).Return(expected, nil)

	resp, err := m.Logout(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Logout_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.LogoutRequest{}
	m.On("Logout", ctx, req).Return((*service.LogoutResponse)(nil), errTest)

	resp, err := m.Logout(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RefreshToken_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RefreshTokenRequest{}
	expected := &service.RefreshTokenResponse{}
	m.On("RefreshToken", ctx, req).Return(expected, nil)

	resp, err := m.RefreshToken(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RefreshToken_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RefreshTokenRequest{}
	m.On("RefreshToken", ctx, req).Return((*service.RefreshTokenResponse)(nil), errTest)

	resp, err := m.RefreshToken(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ValidateToken_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ValidateTokenRequest{}
	expected := &service.ValidateTokenResponse{}
	m.On("ValidateToken", ctx, req).Return(expected, nil)

	resp, err := m.ValidateToken(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ValidateToken_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ValidateTokenRequest{}
	m.On("ValidateToken", ctx, req).Return((*service.ValidateTokenResponse)(nil), errTest)

	resp, err := m.ValidateToken(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// RBAC Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_CreateUser_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthCreateUserRequest{}
	expected := &service.AuthCreateUserResponse{}
	m.On("CreateUser", ctx, req).Return(expected, nil)

	resp, err := m.CreateUser(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateUser_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthCreateUserRequest{}
	m.On("CreateUser", ctx, req).Return((*service.AuthCreateUserResponse)(nil), errTest)

	resp, err := m.CreateUser(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteUser_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthDeleteUserRequest{}
	expected := &service.AuthDeleteUserResponse{}
	m.On("DeleteUser", ctx, req).Return(expected, nil)

	resp, err := m.DeleteUser(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteUser_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthDeleteUserRequest{}
	m.On("DeleteUser", ctx, req).Return((*service.AuthDeleteUserResponse)(nil), errTest)

	resp, err := m.DeleteUser(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListUsers_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthListUsersRequest{}
	expected := &service.AuthListUsersResponse{}
	m.On("ListUsers", ctx, req).Return(expected, nil)

	resp, err := m.ListUsers(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListUsers_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthListUsersRequest{}
	m.On("ListUsers", ctx, req).Return((*service.AuthListUsersResponse)(nil), errTest)

	resp, err := m.ListUsers(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateAuthRole_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthCreateRoleRequest{}
	expected := &service.AuthCreateRoleResponse{}
	m.On("CreateAuthRole", ctx, req).Return(expected, nil)

	resp, err := m.CreateAuthRole(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_CreateAuthRole_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthCreateRoleRequest{}
	m.On("CreateAuthRole", ctx, req).Return((*service.AuthCreateRoleResponse)(nil), errTest)

	resp, err := m.CreateAuthRole(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_AssignRole_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthAssignRoleRequest{}
	expected := &service.AuthAssignRoleResponse{}
	m.On("AssignRole", ctx, req).Return(expected, nil)

	resp, err := m.AssignRole(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_AssignRole_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthAssignRoleRequest{}
	m.On("AssignRole", ctx, req).Return((*service.AuthAssignRoleResponse)(nil), errTest)

	resp, err := m.AssignRole(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RevokeRole_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthRevokeRoleRequest{}
	expected := &service.AuthRevokeRoleResponse{}
	m.On("RevokeRole", ctx, req).Return(expected, nil)

	resp, err := m.RevokeRole(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_RevokeRole_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthRevokeRoleRequest{}
	m.On("RevokeRole", ctx, req).Return((*service.AuthRevokeRoleResponse)(nil), errTest)

	resp, err := m.RevokeRole(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteAuthRole_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthDeleteRoleRequest{}
	expected := &service.AuthDeleteRoleResponse{}
	m.On("DeleteAuthRole", ctx, req).Return(expected, nil)

	resp, err := m.DeleteAuthRole(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteAuthRole_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthDeleteRoleRequest{}
	m.On("DeleteAuthRole", ctx, req).Return((*service.AuthDeleteRoleResponse)(nil), errTest)

	resp, err := m.DeleteAuthRole(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListAuthRoles_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthListRolesRequest{}
	expected := &service.AuthListRolesResponse{}
	m.On("ListAuthRoles", ctx, req).Return(expected, nil)

	resp, err := m.ListAuthRoles(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListAuthRoles_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AuthListRolesRequest{}
	m.On("ListAuthRoles", ctx, req).Return((*service.AuthListRolesResponse)(nil), errTest)

	resp, err := m.ListAuthRoles(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Cache Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_GetCacheStats_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetCacheStatsRequest{}
	expected := &service.GetCacheStatsResponse{}
	m.On("GetCacheStats", ctx, req).Return(expected, nil)

	resp, err := m.GetCacheStats(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetCacheStats_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetCacheStatsRequest{}
	m.On("GetCacheStats", ctx, req).Return((*service.GetCacheStatsResponse)(nil), errTest)

	resp, err := m.GetCacheStats(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ClearCache_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClearCacheRequest{}
	expected := &service.ClearCacheResponse{}
	m.On("ClearCache", ctx, req).Return(expected, nil)

	resp, err := m.ClearCache(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ClearCache_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ClearCacheRequest{}
	m.On("ClearCache", ctx, req).Return((*service.ClearCacheResponse)(nil), errTest)

	resp, err := m.ClearCache(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetCacheConfig_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetCacheConfigRequest{}
	expected := &service.GetCacheConfigResponse{}
	m.On("GetCacheConfig", ctx, req).Return(expected, nil)

	resp, err := m.GetCacheConfig(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetCacheConfig_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetCacheConfigRequest{}
	m.On("GetCacheConfig", ctx, req).Return((*service.GetCacheConfigResponse)(nil), errTest)

	resp, err := m.GetCacheConfig(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateCacheConfig_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateCacheConfigRequest{}
	expected := &service.UpdateCacheConfigResponse{}
	m.On("UpdateCacheConfig", ctx, req).Return(expected, nil)

	resp, err := m.UpdateCacheConfig(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpdateCacheConfig_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpdateCacheConfigRequest{}
	m.On("UpdateCacheConfig", ctx, req).Return((*service.UpdateCacheConfigResponse)(nil), errTest)

	resp, err := m.UpdateCacheConfig(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Health Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Health_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HealthRequest{}
	expected := &service.HealthResponse{}
	m.On("Health", ctx, req).Return(expected, nil)

	resp, err := m.Health(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Health_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.HealthRequest{}
	m.On("Health", ctx, req).Return((*service.HealthResponse)(nil), errTest)

	resp, err := m.Health(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Liveness_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.LivenessRequest{}
	expected := &service.LivenessResponse{}
	m.On("Liveness", ctx, req).Return(expected, nil)

	resp, err := m.Liveness(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Liveness_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.LivenessRequest{}
	m.On("Liveness", ctx, req).Return((*service.LivenessResponse)(nil), errTest)

	resp, err := m.Liveness(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Readiness_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ReadinessRequest{}
	expected := &service.ReadinessResponse{}
	m.On("Readiness", ctx, req).Return(expected, nil)

	resp, err := m.Readiness(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Readiness_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ReadinessRequest{}
	m.On("Readiness", ctx, req).Return((*service.ReadinessResponse)(nil), errTest)

	resp, err := m.Readiness(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Time-Series Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_WritePoint_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WritePointRequest{}
	expected := &service.WritePointResponse{}
	m.On("WritePoint", ctx, req).Return(expected, nil)

	resp, err := m.WritePoint(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_WritePoint_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WritePointRequest{}
	m.On("WritePoint", ctx, req).Return((*service.WritePointResponse)(nil), errTest)

	resp, err := m.WritePoint(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Aggregate_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AggregateRequest{}
	expected := &service.AggregateResponse{}
	m.On("Aggregate", ctx, req).Return(expected, nil)

	resp, err := m.Aggregate(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Aggregate_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.AggregateRequest{}
	m.On("Aggregate", ctx, req).Return((*service.AggregateResponse)(nil), errTest)

	resp, err := m.Aggregate(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_TimeSeriesQuery_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TimeSeriesQueryRequest{}
	stream := make(chan<- *service.TimeSeriesQueryResponse, 1)
	m.On("TimeSeriesQuery", ctx, req, stream).Return(nil)

	err := m.TimeSeriesQuery(ctx, req, stream)
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_TimeSeriesQuery_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TimeSeriesQueryRequest{}
	stream := make(chan<- *service.TimeSeriesQueryResponse, 1)
	m.On("TimeSeriesQuery", ctx, req, stream).Return(errTest)

	err := m.TimeSeriesQuery(ctx, req, stream)
	assert.ErrorIs(t, err, errTest)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_WriteBatch_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WriteBatchRequest{}
	expected := &service.WriteBatchResponse{}
	m.On("WriteBatch", ctx, req).Return(expected, nil)

	resp, err := m.WriteBatch(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_WriteBatch_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.WriteBatchRequest{}
	m.On("WriteBatch", ctx, req).Return((*service.WriteBatchResponse)(nil), errTest)

	resp, err := m.WriteBatch(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_TSQuery_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSQueryRequest{}
	expected := &service.TSQueryResponse{}
	m.On("TSQuery", ctx, req).Return(expected, nil)

	resp, err := m.TSQuery(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_TSQuery_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSQueryRequest{}
	m.On("TSQuery", ctx, req).Return((*service.TSQueryResponse)(nil), errTest)

	resp, err := m.TSQuery(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteSeries_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSDeleteRequest{}
	expected := &service.TSDeleteResponse{}
	m.On("DeleteSeries", ctx, req).Return(expected, nil)

	resp, err := m.DeleteSeries(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteSeries_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSDeleteRequest{}
	m.On("DeleteSeries", ctx, req).Return((*service.TSDeleteResponse)(nil), errTest)

	resp, err := m.DeleteSeries(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRetentionPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSGetRetentionPolicyRequest{}
	expected := &service.TSGetRetentionPolicyResponse{}
	m.On("GetRetentionPolicy", ctx, req).Return(expected, nil)

	resp, err := m.GetRetentionPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetRetentionPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSGetRetentionPolicyRequest{}
	m.On("GetRetentionPolicy", ctx, req).Return((*service.TSGetRetentionPolicyResponse)(nil), errTest)

	resp, err := m.GetRetentionPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetRetentionPolicy_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSSetRetentionPolicyRequest{}
	expected := &service.TSSetRetentionPolicyResponse{}
	m.On("SetRetentionPolicy", ctx, req).Return(expected, nil)

	resp, err := m.SetRetentionPolicy(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetRetentionPolicy_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSSetRetentionPolicyRequest{}
	m.On("SetRetentionPolicy", ctx, req).Return((*service.TSSetRetentionPolicyResponse)(nil), errTest)

	resp, err := m.SetRetentionPolicy(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListMetrics_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSListMetricsRequest{}
	expected := &service.TSListMetricsResponse{}
	m.On("ListMetrics", ctx, req).Return(expected, nil)

	resp, err := m.ListMetrics(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ListMetrics_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.TSListMetricsRequest{}
	m.On("ListMetrics", ctx, req).Return((*service.TSListMetricsResponse)(nil), errTest)

	resp, err := m.ListMetrics(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Operations Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_MigrateData_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.MigrateDataRequest{}
	expected := &service.MigrateDataResponse{}
	m.On("MigrateData", ctx, req).Return(expected, nil)

	resp, err := m.MigrateData(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_MigrateData_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.MigrateDataRequest{}
	m.On("MigrateData", ctx, req).Return((*service.MigrateDataResponse)(nil), errTest)

	resp, err := m.MigrateData(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetMigrationProgress_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetMigrationProgressRequest{}
	expected := &service.GetMigrationProgressResponse{}
	m.On("GetMigrationProgress", ctx, req).Return(expected, nil)

	resp, err := m.GetMigrationProgress(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetMigrationProgress_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetMigrationProgressRequest{}
	m.On("GetMigrationProgress", ctx, req).Return((*service.GetMigrationProgressResponse)(nil), errTest)

	resp, err := m.GetMigrationProgress(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ReplaceNode_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ReplaceNodeRequest{}
	expected := &service.ReplaceNodeResponse{}
	m.On("ReplaceNode", ctx, req).Return(expected, nil)

	resp, err := m.ReplaceNode(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_ReplaceNode_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.ReplaceNodeRequest{}
	m.On("ReplaceNode", ctx, req).Return((*service.ReplaceNodeResponse)(nil), errTest)

	resp, err := m.ReplaceNode(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpgradeNode_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpgradeNodeRequest{}
	expected := &service.UpgradeNodeResponse{}
	m.On("UpgradeNode", ctx, req).Return(expected, nil)

	resp, err := m.UpgradeNode(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_UpgradeNode_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UpgradeNodeRequest{}
	m.On("UpgradeNode", ctx, req).Return((*service.UpgradeNodeResponse)(nil), errTest)

	resp, err := m.UpgradeNode(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetUpgradeStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetUpgradeStatusRequest{}
	expected := &service.GetUpgradeStatusResponse{}
	m.On("GetUpgradeStatus", ctx, req).Return(expected, nil)

	resp, err := m.GetUpgradeStatus(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GetUpgradeStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GetUpgradeStatusRequest{}
	m.On("GetUpgradeStatus", ctx, req).Return((*service.GetUpgradeStatusResponse)(nil), errTest)

	resp, err := m.GetUpgradeStatus(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Seal Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_SealStatus_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	expected := &service.SealStatusResponse{}
	m.On("SealStatus", ctx).Return(expected, nil)

	resp, err := m.SealStatus(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SealStatus_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("SealStatus", ctx).Return((*service.SealStatusResponse)(nil), errTest)

	resp, err := m.SealStatus(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Status_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	expected := &service.SealStatusResponse{}
	m.On("Status", ctx).Return(expected, nil)

	resp, err := m.Status(ctx)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Status_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("Status", ctx).Return((*service.SealStatusResponse)(nil), errTest)

	resp, err := m.Status(ctx)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SealInitialize_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SealInitRequest{}
	expected := &service.SealInitResponse{}
	m.On("SealInitialize", ctx, req).Return(expected, nil)

	resp, err := m.SealInitialize(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SealInitialize_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SealInitRequest{}
	m.On("SealInitialize", ctx, req).Return((*service.SealInitResponse)(nil), errTest)

	resp, err := m.SealInitialize(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Initialize_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SealInitRequest{}
	expected := &service.SealInitResponse{}
	m.On("Initialize", ctx, req).Return(expected, nil)

	resp, err := m.Initialize(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Initialize_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.SealInitRequest{}
	m.On("Initialize", ctx, req).Return((*service.SealInitResponse)(nil), errTest)

	resp, err := m.Initialize(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Unseal_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UnsealRequest{}
	expected := &service.UnsealResponse{}
	m.On("Unseal", ctx, req).Return(expected, nil)

	resp, err := m.Unseal(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Unseal_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.UnsealRequest{}
	m.On("Unseal", ctx, req).Return((*service.UnsealResponse)(nil), errTest)

	resp, err := m.Unseal(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Seal_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("Seal", ctx).Return(nil)

	err := m.Seal(ctx)
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Seal_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	m.On("Seal", ctx).Return(errTest)

	err := m.Seal(ctx)
	assert.ErrorIs(t, err, errTest)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Rekey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RekeyRequest{}
	expected := &service.RekeyResponse{}
	m.On("Rekey", ctx, req).Return(expected, nil)

	resp, err := m.Rekey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Rekey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.RekeyRequest{}
	m.On("Rekey", ctx, req).Return((*service.RekeyResponse)(nil), errTest)

	resp, err := m.Rekey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateRecoveryKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateRecoveryKeyRequest{}
	expected := &service.GenerateRecoveryKeyResponse{}
	m.On("GenerateRecoveryKey", ctx, req).Return(expected, nil)

	resp, err := m.GenerateRecoveryKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateRecoveryKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateRecoveryKeyRequest{}
	m.On("GenerateRecoveryKey", ctx, req).Return((*service.GenerateRecoveryKeyResponse)(nil), errTest)

	resp, err := m.GenerateRecoveryKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteRecoveryKey_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteRecoveryKeyRequest{}
	expected := &service.DeleteRecoveryKeyResponse{}
	m.On("DeleteRecoveryKey", ctx, req).Return(expected, nil)

	resp, err := m.DeleteRecoveryKey(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_DeleteRecoveryKey_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.DeleteRecoveryKeyRequest{}
	m.On("DeleteRecoveryKey", ctx, req).Return((*service.DeleteRecoveryKeyResponse)(nil), errTest)

	resp, err := m.DeleteRecoveryKey(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateRootToken_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateRootTokenRequest{}
	expected := &service.GenerateRootTokenResponse{}
	m.On("GenerateRootToken", ctx, req).Return(expected, nil)

	resp, err := m.GenerateRootToken(ctx, req)
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_GenerateRootToken_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	ctx := context.Background()
	req := &service.GenerateRootTokenRequest{}
	m.On("GenerateRootToken", ctx, req).Return((*service.GenerateRootTokenResponse)(nil), errTest)

	resp, err := m.GenerateRootToken(ctx, req)
	assert.ErrorIs(t, err, errTest)
	assert.Nil(t, resp)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Lifecycle Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Close_Success(t *testing.T) {
	m := NewMockQuicBaaS()
	m.On("Close").Return(nil)

	err := m.Close()
	assert.NoError(t, err)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Close_Error(t *testing.T) {
	m := NewMockQuicBaaS()
	m.On("Close").Return(errTest)

	err := m.Close()
	assert.ErrorIs(t, err, errTest)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetIAMProvider(t *testing.T) {
	m := NewMockQuicBaaS()
	m.On("SetIAMProvider", mock.Anything).Return()
	m.SetIAMProvider(nil)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_SetMessagingService(t *testing.T) {
	m := NewMockQuicBaaS()
	m.On("SetMessagingService", mock.Anything).Return()
	m.SetMessagingService(nil)
	m.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// Delegated Accessors (testify mock.Called)
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_KMSManager(t *testing.T) {
	m := NewMockQuicBaaS()
	m.On("KMSManager").Return(nil)

	result := m.KMSManager()
	assert.Nil(t, result)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_Messaging(t *testing.T) {
	m := NewMockQuicBaaS()
	m.On("Messaging").Return(nil)

	result := m.Messaging()
	assert.Nil(t, result)
	m.AssertExpectations(t)
}

func TestMockQuicBaaS_WatchManager(t *testing.T) {
	m := NewMockQuicBaaS()
	m.On("WatchManager").Return(nil)

	result := m.WatchManager()
	assert.Nil(t, result)
	m.AssertExpectations(t)
}
