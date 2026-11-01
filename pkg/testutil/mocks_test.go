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
		assert.False(t, m.DisableKMS)
	})
}

// ---------------------------------------------------------------------------
// Sub-Service Accessor Methods
// ---------------------------------------------------------------------------

// TestMockQuicBaaS_Accessors covers every service accessor: what it returns
// with no override (the mock itself, or nil), what it returns when its
// override field is set, and the DisableKMS switch.
func TestMockQuicBaaS_Accessors(t *testing.T) {
	tests := []struct {
		name string
		// setup configures the mock; it returns the override it installed,
		// or nil when the row exercises the default.
		setup func(m *MockQuicBaaS) *MockQuicBaaS
		get   func(m *MockQuicBaaS) any
		// want is "self", "override" or "nil".
		want string
	}{
		{
			name: "KMS default returns self",
			get:  func(m *MockQuicBaaS) any { return m.KMS() },
			want: "self",
		},
		{
			name: "KMS returns override",
			setup: func(m *MockQuicBaaS) *MockQuicBaaS {
				o := NewMockQuicBaaS()
				m.KMSService = o
				return o
			},
			get:  func(m *MockQuicBaaS) any { return m.KMS() },
			want: "override",
		},
		{
			name: "KMS disabled returns nil",
			setup: func(m *MockQuicBaaS) *MockQuicBaaS {
				m.DisableKMS = true
				return nil
			},
			get:  func(m *MockQuicBaaS) any { return m.KMS() },
			want: "nil",
		},
		{
			name: "KMS disabled takes precedence over override",
			setup: func(m *MockQuicBaaS) *MockQuicBaaS {
				m.KMSService = NewMockQuicBaaS()
				m.DisableKMS = true
				return nil
			},
			get:  func(m *MockQuicBaaS) any { return m.KMS() },
			want: "nil",
		},
		{
			name: "Secrets returns self",
			get:  func(m *MockQuicBaaS) any { return m.Secrets() },
			want: "self",
		},
		{
			name: "Crypto default returns self",
			get:  func(m *MockQuicBaaS) any { return m.Crypto() },
			want: "self",
		},
		{
			name: "Crypto returns override",
			setup: func(m *MockQuicBaaS) *MockQuicBaaS {
				o := NewMockQuicBaaS()
				m.CryptoService = o
				return o
			},
			get:  func(m *MockQuicBaaS) any { return m.Crypto() },
			want: "override",
		},
		{
			name: "Vector default returns self",
			get:  func(m *MockQuicBaaS) any { return m.Vector() },
			want: "self",
		},
		{
			name: "Vector returns override",
			setup: func(m *MockQuicBaaS) *MockQuicBaaS {
				o := NewMockQuicBaaS()
				m.VectorService = o
				return o
			},
			get:  func(m *MockQuicBaaS) any { return m.Vector() },
			want: "override",
		},
		{
			name: "Embedding returns nil by default",
			get:  func(m *MockQuicBaaS) any { return m.Embedding() },
			want: "nil",
		},
		{
			name: "Ollama returns nil by default",
			get:  func(m *MockQuicBaaS) any { return m.Ollama() },
			want: "nil",
		},
		{
			name: "Certificate returns nil by default",
			get:  func(m *MockQuicBaaS) any { return m.Certificate() },
			want: "nil",
		},
		{
			name: "DynamicSecrets returns nil by default",
			get:  func(m *MockQuicBaaS) any { return m.DynamicSecrets() },
			want: "nil",
		},
		{
			name: "ObjectStorageService returns self",
			get:  func(m *MockQuicBaaS) any { return m.ObjectStorageService() },
			want: "self",
		},
		{
			name: "ObjectStorageAdminService returns self",
			get:  func(m *MockQuicBaaS) any { return m.ObjectStorageAdminService() },
			want: "self",
		},
		{
			name: "ParameterStoreService default returns self",
			get:  func(m *MockQuicBaaS) any { return m.ParameterStoreService() },
			want: "self",
		},
		{
			name: "ParameterStoreService returns override",
			setup: func(m *MockQuicBaaS) *MockQuicBaaS {
				o := NewMockQuicBaaS()
				m.ParameterStoreSvc = o
				return o
			},
			get:  func(m *MockQuicBaaS) any { return m.ParameterStoreService() },
			want: "override",
		},
		{
			name: "ObjectTransferService returns self",
			get:  func(m *MockQuicBaaS) any { return m.ObjectTransferService() },
			want: "self",
		},
		{
			name: "Scheduler returns self",
			get:  func(m *MockQuicBaaS) any { return m.Scheduler() },
			want: "self",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMockQuicBaaS()
			var override *MockQuicBaaS
			if tt.setup != nil {
				override = tt.setup(m)
			}
			got := tt.get(m)
			switch tt.want {
			case "self":
				assert.Same(t, m, got)
			case "override":
				require.NotNil(t, override)
				assert.Same(t, override, got)
			default:
				assert.Nil(t, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// KV Service Methods
// ---------------------------------------------------------------------------

// mockCall describes one testify-backed method of MockQuicBaaS: the
// expectation it registers and the call that must hit it.
type mockCall struct {
	method string
	args   []any
	// ok is the value returned on the success path.
	ok any
	// fail is the zero value returned alongside the error.
	fail any
	// errOnly marks methods that return only an error.
	errOnly bool
	call    func(m *MockQuicBaaS) (any, error)
}

func (c mockCall) returns(v any, err error) []any {
	if c.errOnly {
		return []any{err}
	}
	return []any{v, err}
}

// runMockCalls checks, for every method, that the mock returns the
// registered value on success and the registered error on failure.
func runMockCalls(t *testing.T, calls []mockCall) {
	t.Helper()
	for _, c := range calls {
		t.Run(c.method+"/success", func(t *testing.T) {
			m := NewMockQuicBaaS()
			m.On(c.method, c.args...).Return(c.returns(c.ok, nil)...)

			got, err := c.call(m)
			require.NoError(t, err)
			assert.Equal(t, c.ok, got)
			m.AssertExpectations(t)
		})
		t.Run(c.method+"/error", func(t *testing.T) {
			m := NewMockQuicBaaS()
			m.On(c.method, c.args...).Return(c.returns(c.fail, errTest)...)

			got, err := c.call(m)
			require.ErrorIs(t, err, errTest)
			assert.Equal(t, c.fail, got)
			m.AssertExpectations(t)
		})
	}
}

func TestMockQuicBaaS_KV(t *testing.T) {
	ctx := context.Background()
	putReq := &service.PutRequest{EntityType: "test", Data: []byte("v1")}
	getReq := &service.GetRequest{EntityType: "test", ID: 1}
	deleteReq := &service.DeleteRequest{EntityType: "test", ID: 1}
	listReq := &service.ListRequest{EntityType: "test"}
	listStream := make(chan<- *service.ListResponse, 1)
	watchReq := &service.WatchRequest{}
	watchStream := make(chan<- *service.WatchResponse, 1)
	typesReq := &service.ListEntityTypesRequest{}

	runMockCalls(t, []mockCall{
		{
			method: "Put",
			args:   []any{ctx, putReq},
			ok:     &service.PutResponse{},
			fail:   (*service.PutResponse)(nil),
			call:   func(m *MockQuicBaaS) (any, error) { return m.Put(ctx, putReq) },
		},
		{
			method: "Get",
			args:   []any{ctx, getReq},
			ok:     &service.GetResponse{Data: []byte("v1")},
			fail:   (*service.GetResponse)(nil),
			call:   func(m *MockQuicBaaS) (any, error) { return m.Get(ctx, getReq) },
		},
		{
			method: "Delete",
			args:   []any{ctx, deleteReq},
			ok:     &service.DeleteResponse{},
			fail:   (*service.DeleteResponse)(nil),
			call:   func(m *MockQuicBaaS) (any, error) { return m.Delete(ctx, deleteReq) },
		},
		{
			method:  "List",
			args:    []any{ctx, listReq, listStream},
			errOnly: true,
			call:    func(m *MockQuicBaaS) (any, error) { return nil, m.List(ctx, listReq, listStream) },
		},
		{
			method: "Watch",
			args:   []any{ctx, watchReq, watchStream},
			ok:     int64(42),
			fail:   int64(0),
			call:   func(m *MockQuicBaaS) (any, error) { return m.Watch(ctx, watchReq, watchStream) },
		},
		{
			method: "ListEntityTypes",
			args:   []any{ctx, typesReq},
			ok:     &service.ListEntityTypesResponse{},
			fail:   (*service.ListEntityTypesResponse)(nil),
			call:   func(m *MockQuicBaaS) (any, error) { return m.ListEntityTypes(ctx, typesReq) },
		},
	})
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
