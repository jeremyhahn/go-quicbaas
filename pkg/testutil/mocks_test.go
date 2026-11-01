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

// reqRespCall builds the mockCall for the common method shape
// Method(ctx, *Req) (*Resp, error), given its method expression.
func reqRespCall[Req, Resp any](method string, fn func(*MockQuicBaaS, context.Context, *Req) (*Resp, error)) mockCall {
	ctx := context.Background()
	req := new(Req)
	return mockCall{
		method: method,
		args:   []any{ctx, req},
		ok:     new(Resp),
		fail:   (*Resp)(nil),
		call:   func(m *MockQuicBaaS) (any, error) { return fn(m, ctx, req) },
	}
}

func TestMockQuicBaaS_KV(t *testing.T) {
	ctx := context.Background()
	listReq := &service.ListRequest{EntityType: "test"}
	listStream := make(chan<- *service.ListResponse, 1)
	watchReq := &service.WatchRequest{}
	watchStream := make(chan<- *service.WatchResponse, 1)

	runMockCalls(t, []mockCall{
		reqRespCall("Put", (*MockQuicBaaS).Put),
		reqRespCall("Get", (*MockQuicBaaS).Get),
		reqRespCall("Delete", (*MockQuicBaaS).Delete),
		reqRespCall("ListEntityTypes", (*MockQuicBaaS).ListEntityTypes),
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
	})
}

// ---------------------------------------------------------------------------
// Batch, Transaction and Query Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_BatchTransactionQuery(t *testing.T) {
	ctx := context.Background()
	queryReq := &service.QueryRequest{}
	queryStream := make(chan<- *service.QueryResponse, 1)
	scanReq := &service.ScanRequest{}
	scanStream := make(chan<- *service.ScanResponse, 1)

	runMockCalls(t, []mockCall{
		reqRespCall("BatchPut", (*MockQuicBaaS).BatchPut),
		reqRespCall("BatchGet", (*MockQuicBaaS).BatchGet),
		reqRespCall("BatchDelete", (*MockQuicBaaS).BatchDelete),
		reqRespCall("Transaction", (*MockQuicBaaS).Transaction),
		{
			method:  "Query",
			args:    []any{ctx, queryReq, queryStream},
			errOnly: true,
			call:    func(m *MockQuicBaaS) (any, error) { return nil, m.Query(ctx, queryReq, queryStream) },
		},
		{
			method:  "ScanIndex",
			args:    []any{ctx, scanReq, scanStream},
			errOnly: true,
			call:    func(m *MockQuicBaaS) (any, error) { return nil, m.ScanIndex(ctx, scanReq, scanStream) },
		},
	})
}

// ---------------------------------------------------------------------------
// Cluster Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Cluster(t *testing.T) {
	runMockCalls(t, []mockCall{
		reqRespCall("ClusterStatus", (*MockQuicBaaS).ClusterStatus),
		reqRespCall("GetLeader", (*MockQuicBaaS).GetLeader),
		reqRespCall("AddNode", (*MockQuicBaaS).AddNode),
		reqRespCall("RemoveNode", (*MockQuicBaaS).RemoveNode),
		reqRespCall("CreateShard", (*MockQuicBaaS).CreateShard),
		reqRespCall("RebalanceCluster", (*MockQuicBaaS).RebalanceCluster),
		reqRespCall("GetRebalanceProgress", (*MockQuicBaaS).GetRebalanceProgress),
	})
}

// ---------------------------------------------------------------------------
// Backup Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Backup(t *testing.T) {
	runMockCalls(t, []mockCall{
		reqRespCall("CreateBackup", (*MockQuicBaaS).CreateBackup),
		reqRespCall("ListBackups", (*MockQuicBaaS).ListBackups),
		reqRespCall("RestoreBackup", (*MockQuicBaaS).RestoreBackup),
		reqRespCall("DeleteBackup", (*MockQuicBaaS).DeleteBackup),
		reqRespCall("VerifyBackup", (*MockQuicBaaS).VerifyBackup),
	})
}

// ---------------------------------------------------------------------------
// Auth Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Auth(t *testing.T) {
	runMockCalls(t, []mockCall{
		reqRespCall("Login", (*MockQuicBaaS).Login),
		reqRespCall("Logout", (*MockQuicBaaS).Logout),
		reqRespCall("RefreshToken", (*MockQuicBaaS).RefreshToken),
		reqRespCall("ValidateToken", (*MockQuicBaaS).ValidateToken),
	})
}

// ---------------------------------------------------------------------------
// RBAC Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_RBAC(t *testing.T) {
	runMockCalls(t, []mockCall{
		reqRespCall("CreateAuthRole", (*MockQuicBaaS).CreateAuthRole),
		reqRespCall("AssignRole", (*MockQuicBaaS).AssignRole),
		reqRespCall("RevokeRole", (*MockQuicBaaS).RevokeRole),
		reqRespCall("DeleteAuthRole", (*MockQuicBaaS).DeleteAuthRole),
		reqRespCall("ListAuthRoles", (*MockQuicBaaS).ListAuthRoles),
	})
}

// ---------------------------------------------------------------------------
// Cache Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Cache(t *testing.T) {
	runMockCalls(t, []mockCall{
		reqRespCall("GetCacheStats", (*MockQuicBaaS).GetCacheStats),
		reqRespCall("ClearCache", (*MockQuicBaaS).ClearCache),
		reqRespCall("GetCacheConfig", (*MockQuicBaaS).GetCacheConfig),
		reqRespCall("UpdateCacheConfig", (*MockQuicBaaS).UpdateCacheConfig),
	})
}

// ---------------------------------------------------------------------------
// Health Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Health(t *testing.T) {
	runMockCalls(t, []mockCall{
		reqRespCall("Health", (*MockQuicBaaS).Health),
		reqRespCall("Liveness", (*MockQuicBaaS).Liveness),
		reqRespCall("Readiness", (*MockQuicBaaS).Readiness),
	})
}

// ---------------------------------------------------------------------------
// Time-Series Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_TimeSeries(t *testing.T) {
	ctx := context.Background()
	queryReq := &service.TimeSeriesQueryRequest{}
	queryStream := make(chan<- *service.TimeSeriesQueryResponse, 1)

	runMockCalls(t, []mockCall{
		reqRespCall("WritePoint", (*MockQuicBaaS).WritePoint),
		reqRespCall("Aggregate", (*MockQuicBaaS).Aggregate),
		reqRespCall("WriteBatch", (*MockQuicBaaS).WriteBatch),
		reqRespCall("TSQuery", (*MockQuicBaaS).TSQuery),
		reqRespCall("DeleteSeries", (*MockQuicBaaS).DeleteSeries),
		reqRespCall("GetRetentionPolicy", (*MockQuicBaaS).GetRetentionPolicy),
		reqRespCall("SetRetentionPolicy", (*MockQuicBaaS).SetRetentionPolicy),
		reqRespCall("ListMetrics", (*MockQuicBaaS).ListMetrics),
		{
			method:  "TimeSeriesQuery",
			args:    []any{ctx, queryReq, queryStream},
			errOnly: true,
			call: func(m *MockQuicBaaS) (any, error) {
				return nil, m.TimeSeriesQuery(ctx, queryReq, queryStream)
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Operations Service Methods
// ---------------------------------------------------------------------------

func TestMockQuicBaaS_Operations(t *testing.T) {
	runMockCalls(t, []mockCall{
		reqRespCall("MigrateData", (*MockQuicBaaS).MigrateData),
		reqRespCall("GetMigrationProgress", (*MockQuicBaaS).GetMigrationProgress),
		reqRespCall("ReplaceNode", (*MockQuicBaaS).ReplaceNode),
		reqRespCall("UpgradeNode", (*MockQuicBaaS).UpgradeNode),
		reqRespCall("GetUpgradeStatus", (*MockQuicBaaS).GetUpgradeStatus),
	})
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
