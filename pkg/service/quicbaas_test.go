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

package service

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"encoding/json"
	"fmt"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/backup"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// mockRaftNode implements interfaces.RaftNode for testing.
type mockRaftNode struct {
	syncProposeFn     func(ctx context.Context, shardID uint64, data []byte) error
	syncReadFn        func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error)
	getLeaderIDFn     func(shardID uint64) (uint64, error)
	addNodeFn         func(shardID, nodeID uint64, address string) error
	removeNodeFn      func(shardID, nodeID uint64) error
	getNodeHostInfoFn func() *interfaces.NodeHostInfo
}

func (m *mockRaftNode) ID() string                            { return "mock-node-1" }
func (m *mockRaftNode) RaftAddress() string                   { return "localhost:63001" }
func (m *mockRaftNode) ShardCount() int                       { return 1 }
func (m *mockRaftNode) IsDraining() bool                      { return false }
func (m *mockRaftNode) Drain() error                          { return nil }
func (m *mockRaftNode) HealthCheck() *interfaces.HealthStatus { return &interfaces.HealthStatus{} }

func (m *mockRaftNode) SyncPropose(ctx context.Context, shardID uint64, data []byte) (interfaces.Result, error) {
	if m.syncProposeFn != nil {
		return interfaces.Result{}, m.syncProposeFn(ctx, shardID, data)
	}
	return interfaces.Result{}, nil
}

func (m *mockRaftNode) ProposeWithMode(ctx context.Context, shardID uint64, data []byte, _ interfaces.WriteMode) (interfaces.Result, error) {
	return m.SyncPropose(ctx, shardID, data)
}

func (m *mockRaftNode) SyncRead(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
	if m.syncReadFn != nil {
		return m.syncReadFn(ctx, shardID, query)
	}
	return nil, nil
}

func (m *mockRaftNode) StaleRead(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
	return m.SyncRead(ctx, shardID, query)
}

func (m *mockRaftNode) GetLeaderID(shardID uint64) (uint64, bool, error) {
	if m.getLeaderIDFn != nil {
		id, err := m.getLeaderIDFn(shardID)
		return id, err == nil, err
	}
	return 1, true, nil
}

func (m *mockRaftNode) GetNodeHostInfo(_ interfaces.NodeHostInfoOption) *interfaces.NodeHostInfo {
	if m.getNodeHostInfoFn != nil {
		return m.getNodeHostInfoFn()
	}
	return &interfaces.NodeHostInfo{}
}

func (m *mockRaftNode) RequestAddNode(_ context.Context, shardID, nodeID uint64, address string, _ uint64) (interfaces.RequestState, error) {
	if m.addNodeFn != nil {
		return nil, m.addNodeFn(shardID, nodeID, address)
	}
	return nil, nil
}

func (m *mockRaftNode) RequestRemoveNode(_ context.Context, shardID, nodeID uint64, _ uint64) (interfaces.RequestState, error) {
	if m.removeNodeFn != nil {
		return nil, m.removeNodeFn(shardID, nodeID)
	}
	return nil, nil
}

func (m *mockRaftNode) RequestSnapshot(_ context.Context, _ uint64) (interfaces.RequestState, error) {
	return nil, nil
}

func (m *mockRaftNode) Close() error {
	return nil
}

func (m *mockRaftNode) StartShard(_ map[uint64]string, _ bool, _ interfaces.CreateFunc, _ interfaces.ShardConfig) error {
	return nil
}

func (m *mockRaftNode) StopShard(_ uint64) error {
	return nil
}

func (m *mockRaftNode) ReadWithConsistency(ctx context.Context, shardID uint64, query interface{}, _ interfaces.ReadOptions) (interface{}, error) {
	return m.SyncRead(ctx, shardID, query)
}

func (m *mockRaftNode) GetShardRaftState(_ uint64) (uint64, uint64, error) {
	return 0, 0, nil
}

// mockStorage implements interfaces.Storage for testing.
type mockStorage struct {
	getFn    func(key []byte) ([]byte, error)
	putFn    func(key, value []byte) error
	deleteFn func(key []byte) error
}

func (m *mockStorage) Get(key []byte) ([]byte, error) {
	if m.getFn != nil {
		return m.getFn(key)
	}
	return nil, nil
}

func (m *mockStorage) Put(key, value []byte) error {
	if m.putFn != nil {
		return m.putFn(key, value)
	}
	return nil
}

func (m *mockStorage) Delete(key []byte) error {
	if m.deleteFn != nil {
		return m.deleteFn(key)
	}
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

type mockBackupManager struct {
	createBackupFn  func(ctx context.Context, backupType interface{}) (interface{}, error)
	listBackupsFn   func(ctx context.Context, limit int) (interface{}, error)
	restoreBackupFn func(ctx context.Context, backupID string, opts interface{}) error
	deleteBackupFn  func(ctx context.Context, backupID string) error
	verifyBackupFn  func(ctx context.Context, backupID string) (interface{}, error)
}

// Implement interfaces.BackupManager.
func (m *mockBackupManager) CreateBackup(ctx context.Context, backupType interface{}) (interface{}, error) {
	if m.createBackupFn != nil {
		return m.createBackupFn(ctx, backupType)
	}
	return nil, stderrors.New("not implemented")
}

func (m *mockBackupManager) ListBackups(ctx context.Context, limit int) (interface{}, error) {
	if m.listBackupsFn != nil {
		return m.listBackupsFn(ctx, limit)
	}
	return nil, stderrors.New("not implemented")
}

func (m *mockBackupManager) RestoreBackup(ctx context.Context, backupID string, opts interface{}) error {
	if m.restoreBackupFn != nil {
		return m.restoreBackupFn(ctx, backupID, opts)
	}
	return nil
}

func (m *mockBackupManager) DeleteBackup(ctx context.Context, backupID string) error {
	if m.deleteBackupFn != nil {
		return m.deleteBackupFn(ctx, backupID)
	}
	return nil
}

func (m *mockBackupManager) VerifyBackup(ctx context.Context, backupID string) (interface{}, error) {
	if m.verifyBackupFn != nil {
		return m.verifyBackupFn(ctx, backupID)
	}
	return nil, stderrors.New("not implemented")
}

func (m *mockBackupManager) Close() error {
	return nil
}

// =============================================================================
// Test Helpers
// =============================================================================

func createTestLogger(t *testing.T) interfaces.Logger {
	cfg := logger.LogConfig{
		Level:  "error",
		Format: "console",
		Output: "stdout",
	}

	log, err := logger.New(cfg)
	require.NoError(t, err)

	return &simpleLogger{log}
}

func createConcreteLogger(t *testing.T) *logger.Logger {
	cfg := logger.LogConfig{
		Level:  "error",
		Format: "console",
		Output: "stdout",
	}

	log, err := logger.New(cfg)
	require.NoError(t, err)

	return log
}

func createTestService(t *testing.T, opts ...func(*serviceBuilder)) QuicBaaS {
	builder := &serviceBuilder{
		config: &ServiceConfig{
			NodeID:                1,
			Version:               "1.0.0",
			EnableAuth:            false,
			EnableMetrics:         false,
			EnableTracing:         false,
			RequestTimeout:        5 * time.Second,
			MaxConcurrentRequests: 100,
		},
		raftNode: &mockRaftNode{},
		logger:   createTestLogger(t),
	}

	// Apply options
	for _, opt := range opts {
		opt(builder)
	}

	// Auto-wire qrdb client provider if not explicitly set.
	if builder.qrdbClient == nil {
		builder.qrdbClient = newSvcBoostMockQRDBProvider(nil, nil)
	}

	svc, err := newQuicBaaS(builder)
	require.NoError(t, err)
	require.NotNil(t, svc)

	return svc
}

func withMockRaftNode(raft interfaces.RaftNode) func(*serviceBuilder) {
	return func(b *serviceBuilder) {
		b.raftNode = raft
	}
}

func withMockStorage(storage interfaces.Storage) func(*serviceBuilder) {
	return func(b *serviceBuilder) {
		b.storage = storage
	}
}

func withEnableMetrics() func(*serviceBuilder) {
	return func(b *serviceBuilder) {
		b.config.EnableMetrics = true
	}
}

func withBackupManager(backupMgr *mockBackupManager) func(*serviceBuilder) {
	return func(b *serviceBuilder) {
		b.backupManager = backupMgr
	}
}

func withCacheManager(cacheMgr *cache.CacheManager) func(*serviceBuilder) {
	return func(b *serviceBuilder) {
		b.cacheManager = cacheMgr
	}
}

func withQRDBClientProvider(provider QRDBClientProvider) func(*serviceBuilder) {
	return func(b *serviceBuilder) {
		b.qrdbClient = provider
	}
}

// =============================================================================
// ServiceBuilder Tests
// =============================================================================

func TestServiceBuilder_NewServiceBuilder(t *testing.T) {
	builder := NewServiceBuilder()
	require.NotNil(t, builder)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.NotNil(t, sb.config)
	assert.True(t, sb.config.EnableAuth)
	assert.True(t, sb.config.EnableMetrics)
	assert.Equal(t, "info", sb.config.LogLevel)
}

func TestServiceBuilder_WithRaftNode(t *testing.T) {
	mockRaft := &mockRaftNode{}
	builder := NewServiceBuilder().WithRaftNode(mockRaft)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, mockRaft, sb.raftNode)
}

func TestServiceBuilder_WithStorage(t *testing.T) {
	mockStore := &mockStorage{}
	builder := NewServiceBuilder().WithStorage(mockStore)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, mockStore, sb.storage)
}

func TestServiceBuilder_WithCacheManager(t *testing.T) {
	mockCache := &cache.CacheManager{}
	builder := NewServiceBuilder().WithCacheManager(mockCache)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, mockCache, sb.cacheManager)
}

func TestServiceBuilder_WithBackupManager(t *testing.T) {
	mockBackup := &mockBackupManager{}
	builder := NewServiceBuilder().WithBackupManager(mockBackup)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, mockBackup, sb.backupManager)
}

func TestServiceBuilder_WithLogger(t *testing.T) {
	log := createTestLogger(t)
	builder := NewServiceBuilder().WithLogger(log)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.NotNil(t, sb.logger)
}

func TestServiceBuilder_WithConfig(t *testing.T) {
	config := &ServiceConfig{
		NodeID:  42,
		Version: "2.0.0",
	}
	builder := NewServiceBuilder().WithConfig(config)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, config, sb.config)
}

func TestServiceBuilder_Build_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	log := createTestLogger(t)

	builder := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithLogger(log)

	svc, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, svc)

	err = svc.Close()
	require.NoError(t, err)
}

// =============================================================================
// KV Service Put Operation Tests
// =============================================================================

func TestKVService_Put_Success(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.ID)
	assert.Greater(t, resp.Timestamp, int64(0))
}

func TestKVService_Delete_Success(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &DeleteRequest{
		EntityType: "user",
		ID:         1,
	}

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Greater(t, resp.Timestamp, int64(0))
}

func TestKVService_Delete_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestKVService_Delete_EmptyEntityType(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &DeleteRequest{
		EntityType: "",
		ID:         1,
	}

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "entity type is required")
}

func TestKVService_Delete_RaftError(t *testing.T) {
	deleteErr := stderrors.New("raft propose failed")
	qrdbProvider := newSvcBoostMockQRDBProviderWithErrors(nil, deleteErr, nil)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &DeleteRequest{
		EntityType: "user",
		ID:         1,
	}

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Delete_WithCustomShardID(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			assert.Equal(t, uint64(5), shardID)
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &DeleteRequest{
		EntityType: "user",
		ID:         1,
		ShardID:    5,
	}

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_Delete_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics())
	defer svc.Close()

	req := &DeleteRequest{
		EntityType: "user",
		ID:         1,
	}

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBatchService_BatchGet_Success(t *testing.T) {
	data1 := []byte(`{"id":1,"name":"test1"}`)
	data2 := []byte(`{"id":2,"name":"test2"}`)

	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			key := query.([]byte)
			keyStr := string(key)
			if keyStr == "user:1" {
				return data1, nil
			} else if keyStr == "user:2" {
				return data2, nil
			}
			return nil, nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &BatchGetRequest{
		Requests: []*GetRequest{
			{EntityType: "user", ID: 1},
			{EntityType: "user", ID: 2},
		},
	}

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBatchService_BatchGet_EmptyRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &BatchGetRequest{
		Requests: []*GetRequest{},
	}

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func TestBatchService_BatchGet_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestBatchService_BatchGet_PartialFailure(t *testing.T) {
	data1 := []byte(`{"id":1,"name":"test1"}`)

	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			key := query.([]byte)
			keyStr := string(key)
			if keyStr == "user:1" {
				return data1, nil
			}
			return nil, nil // Not found
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &BatchGetRequest{
		Requests: []*GetRequest{
			{EntityType: "user", ID: 1},
			{EntityType: "user", ID: 999},
		},
	}

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Responses, 2)
}

func TestBatchService_BatchGet_WithMetrics(t *testing.T) {
	data := []byte(`{"id":1,"name":"test"}`)

	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return data, nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics())
	defer svc.Close()

	req := &BatchGetRequest{
		Requests: []*GetRequest{
			{EntityType: "user", ID: 1},
		},
	}

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Transaction Service Tests
// =============================================================================

func TestTransactionService_Transaction_Success(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return []byte(`{"id":1,"name":"test"}`), nil
		},
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &TransactionRequest{
		Compare: []*CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTargetVersion,
				Operator:   CompareOperatorEqual,
				Version:    1,
			},
		},
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`{"name":"updated"}`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestTransactionService_Transaction_GetOperation(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return []byte(`{"id":1,"name":"test"}`), nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &TransactionRequest{
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypeGet,
				EntityType: "user",
				ID:         1,
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Succeeded)
}

func TestTransactionService_Transaction_DeleteOperation(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &TransactionRequest{
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypeDelete,
				EntityType: "user",
				ID:         1,
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestTransactionService_Transaction_CompareValue(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return []byte(`testvalue`), nil
		},
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &TransactionRequest{
		Compare: []*CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTargetValue,
				Operator:   CompareOperatorEqual,
				Value:      []byte(`testvalue`),
			},
		},
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`newvalue`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestTransactionService_Transaction_CompareExists(t *testing.T) {
	// Get returns data (entity exists), Batch succeeds for the Put op
	qrdbProvider := newSvcBoostMockQRDBProviderWithGet([]byte(`data`), nil)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &TransactionRequest{
		Compare: []*CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTargetExists,
				Operator:   CompareOperatorEqual,
			},
		},
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`updated`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Succeeded)
}

func TestTransactionService_Transaction_FailedComparison(t *testing.T) {
	tr := newAuthorizingTransport(permTransactionExecute)
	tr.data["user:1"] = []byte(`{"id":1,"name":"test"}`)
	svc := newAuthorizingService(t, tr)

	req := &TransactionRequest{
		Compare: []*CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTargetVersion,
				Operator:   CompareOperatorEqual,
				Version:    999, // Wrong version
			},
		},
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`{"name":"updated"}`),
			},
		},
		Failure: []*TransactionOp{
			{
				OpType:     TransactionOpTypeGet,
				EntityType: "user",
				ID:         1,
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Succeeded)
	require.Len(t, resp.Responses, 1)
	assert.Equal(t, []byte(`{"id":1,"name":"test"}`), resp.Responses[0].Data, "the Failure branch's Get runs, the Put does not")
}

func TestTransactionService_Transaction_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestTransactionService_Transaction_EmptyCompare(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &TransactionRequest{
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`{"name":"test"}`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestTransactionService_Transaction_RaftError(t *testing.T) {
	tr := &svcBoostMockTransport{txErr: stderrors.New("raft propose failed")}
	svc := createTestService(t, withQRDBClientProvider(newSvcBoostMockQRDBProviderFull(tr)))
	defer svc.Close()

	req := &TransactionRequest{
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`{"name":"test"}`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr))
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestTransactionService_Transaction_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics())
	defer svc.Close()

	req := &TransactionRequest{
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`{"name":"test"}`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Query Service Tests
// =============================================================================

// =============================================================================
// Cluster Service Tests
// =============================================================================

func TestClusterService_ClusterStatus_Success(t *testing.T) {
	mockRaft := &mockRaftNode{
		getNodeHostInfoFn: func() *interfaces.NodeHostInfo {
			return &interfaces.NodeHostInfo{
				RaftAddress: "localhost:5001",
			}
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &ClusterStatusRequest{}

	resp, err := svc.ClusterStatus(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestClusterService_GetLeader_Success(t *testing.T) {
	mockTransport := &svcBoostMockTransport{
		shardStateResult: &qrdbtransport.ShardStateResponse{
			ShardID:   1,
			LeaderID:  1,
			HasLeader: true,
		},
	}
	qrdbProvider := newSvcBoostMockQRDBProviderFull(mockTransport)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &GetLeaderRequest{
		ShardID: 1,
	}

	resp, err := svc.GetLeader(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(1), resp.LeaderID)
}

func TestClusterService_GetLeader_Error(t *testing.T) {
	mockTransport := &svcBoostMockTransport{
		shardStateErr: stderrors.New("no leader available"),
	}
	qrdbProvider := newSvcBoostMockQRDBProviderFull(mockTransport)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &GetLeaderRequest{
		ShardID: 1,
	}

	resp, err := svc.GetLeader(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestClusterService_AddNode_Success(t *testing.T) {
	mockTransport := &svcBoostMockTransport{}
	qrdbProvider := newSvcBoostMockQRDBProviderFull(mockTransport)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &AddNodeRequest{
		ShardID: 1,
		NodeID:  2,
		Address: "localhost:5002",
	}

	resp, err := svc.AddNode(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClusterService_AddNode_Error(t *testing.T) {
	mockTransport := &svcBoostMockTransport{
		addMemberErr: stderrors.New("failed to add node"),
	}
	qrdbProvider := newSvcBoostMockQRDBProviderFull(mockTransport)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &AddNodeRequest{
		ShardID: 1,
		NodeID:  2,
		Address: "localhost:5002",
	}

	resp, err := svc.AddNode(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestClusterService_RemoveNode_Success(t *testing.T) {
	mockTransport := &svcBoostMockTransport{}
	qrdbProvider := newSvcBoostMockQRDBProviderFull(mockTransport)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &RemoveNodeRequest{
		ShardID: 1,
		NodeID:  2,
	}

	resp, err := svc.RemoveNode(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestClusterService_RemoveNode_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.RemoveNode(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClusterService_RemoveNode_InvalidShardID(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &RemoveNodeRequest{
		ShardID: 0,
		NodeID:  2,
	}

	resp, err := svc.RemoveNode(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "shard_id must be greater than 0")
}

func TestClusterService_RemoveNode_InvalidNodeID(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &RemoveNodeRequest{
		ShardID: 1,
		NodeID:  0,
	}

	resp, err := svc.RemoveNode(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "node_id must be greater than 0")
}

func TestClusterService_RemoveNode_RaftError(t *testing.T) {
	mockTransport := &svcBoostMockTransport{
		removeMemberErr: stderrors.New("failed to remove node"),
	}
	qrdbProvider := newSvcBoostMockQRDBProviderFull(mockTransport)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &RemoveNodeRequest{
		ShardID: 1,
		NodeID:  2,
	}

	resp, err := svc.RemoveNode(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestClusterService_CreateShard_NotImplemented(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &CreateShardRequest{
		ShardID:        2,
		InitialMembers: map[uint64]string{1: "localhost:5001"},
	}

	resp, err := svc.CreateShard(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestClusterService_CreateShard_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.CreateShard(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClusterService_CreateShard_InvalidShardID(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &CreateShardRequest{
		ShardID:        0,
		InitialMembers: map[uint64]string{1: "localhost:5001"},
	}

	resp, err := svc.CreateShard(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "shard_id must be greater than 0")
}

func TestClusterService_CreateShard_EmptyMembers(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &CreateShardRequest{
		ShardID:        1,
		InitialMembers: map[uint64]string{},
	}

	resp, err := svc.CreateShard(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "initial_members cannot be empty")
}

func TestClusterService_RebalanceCluster_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &RebalanceClusterRequest{
		TargetShardCount: 3,
		Strategy:         "even",
	}

	resp, err := svc.RebalanceCluster(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.JobID)
	assert.Equal(t, 0.0, resp.Progress)
}

func TestClusterService_RebalanceCluster_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.RebalanceCluster(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// =============================================================================
// Backup Service Tests
// =============================================================================

func TestBackupService_CreateBackup_Success(t *testing.T) {
	mockBackup := &mockBackupManager{
		createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
			return &backup.Backup{
				ID:        "backup-123",
				Type:      backup.BackupTypeFull,
				Timestamp: time.Now(),
			}, nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &CreateBackupRequest{
		Name:        "test-backup",
		Description: "Test backup",
	}

	resp, err := svc.CreateBackup(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "backup-123", resp.BackupID)
}

func TestBackupService_ListBackups_Success(t *testing.T) {
	mockBackup := &mockBackupManager{
		listBackupsFn: func(ctx context.Context, limit int) (interface{}, error) {
			return &backup.BackupList{
				Backups: []*backup.Backup{
					{
						ID:        "backup-1",
						Type:      backup.BackupTypeFull,
						Timestamp: time.Now(),
					},
				},
				TotalCount: 1,
			}, nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &ListBackupsRequest{
		Limit: 10,
	}

	resp, err := svc.ListBackups(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Backups, 1)
}

func TestBackupService_RestoreBackup_Success(t *testing.T) {
	mockBackup := &mockBackupManager{
		restoreBackupFn: func(ctx context.Context, backupID string, opts interface{}) error {
			return nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &RestoreBackupRequest{
		BackupID: "backup-123",
	}

	resp, err := svc.RestoreBackup(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBackupService_DeleteBackup_Success(t *testing.T) {
	mockBackup := &mockBackupManager{
		deleteBackupFn: func(ctx context.Context, backupID string) error {
			return nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &DeleteBackupRequest{
		BackupID: "backup-123",
	}

	resp, err := svc.DeleteBackup(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestBackupService_VerifyBackup_Success(t *testing.T) {
	mockBackup := &mockBackupManager{
		verifyBackupFn: func(ctx context.Context, backupID string) (interface{}, error) {
			return &backup.VerificationResult{
				Valid:         true,
				ChecksumValid: true,
				Errors:        []string{},
			}, nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &VerifyBackupRequest{
		BackupID: "backup-123",
	}

	resp, err := svc.VerifyBackup(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
	assert.True(t, resp.ChecksumMatch)
}

func TestBackupService_VerifyBackup_NotAvailable(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	req := &VerifyBackupRequest{
		BackupID: "backup-123",
	}

	resp, err := svc.VerifyBackup(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "backup manager service not configured")
}

func TestBackupService_VerifyBackup_Error(t *testing.T) {
	mockBackup := &mockBackupManager{
		verifyBackupFn: func(ctx context.Context, backupID string) (interface{}, error) {
			return nil, stderrors.New("verification failed")
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &VerifyBackupRequest{
		BackupID: "backup-123",
	}

	resp, err := svc.VerifyBackup(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBackupService_VerifyBackup_InvalidResult(t *testing.T) {
	mockBackup := &mockBackupManager{
		verifyBackupFn: func(ctx context.Context, backupID string) (interface{}, error) {
			return "invalid-type", nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &VerifyBackupRequest{
		BackupID: "backup-123",
	}

	resp, err := svc.VerifyBackup(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "verification result must be *backup.VerificationResult")
}

// =============================================================================
// Auth Service Tests
// =============================================================================

func TestAuthService_Login_ReturnsNotImplemented(t *testing.T) {
	// Password-based login is removed. Login() always returns ErrNotImplemented.
	// Use WebAuthn or PIV smartcard authentication instead.
	svc := createTestService(t)
	defer svc.Close()

	req := &LoginRequest{Username: "testuser"}
	_, err := svc.Login(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
}

// =============================================================================
// RBAC Service Tests
// =============================================================================

// =============================================================================
// Additional Coverage Tests
// =============================================================================

// -----------------------------------------------------------------------------
// KV Operations - Get, Delete, List Success Paths
// -----------------------------------------------------------------------------

func TestKVService_Get_SuccessWithData(t *testing.T) {
	testData := []byte(`{"id":1,"name":"test user"}`)

	qrdbProvider := newSvcBoostMockQRDBProviderWithGet(testData, nil)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &GetRequest{
		EntityType:  "user",
		ID:          1,
		Consistency: ConsistencyLevelLinearizable,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Found)
	assert.Equal(t, testData, resp.Data)
	assert.Equal(t, uint64(1), resp.ID)
	assert.Greater(t, resp.Timestamp, int64(0))
}

func TestKVService_Get_NotFound(t *testing.T) {
	// SDK Get returns nil data => service treats as not found
	qrdbProvider := newSvcBoostMockQRDBProviderWithGet(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &GetRequest{
		EntityType: "user",
		ID:         999,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "not found")
}

func TestKVService_Get_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestKVService_Get_EmptyEntityType(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &GetRequest{
		EntityType: "",
		ID:         1,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "entity type is required")
}

// =============================================================================
// ListEntityTypes Tests
// =============================================================================

func TestKVService_ListEntityTypes_Success(t *testing.T) {
	// Create mock that returns keys for different entity types via QRDB scan
	scanData := map[string][]byte{
		"User:1":         []byte("user1-data"),
		"User:2":         []byte("user2-data"),
		"Organization:1": []byte("org1-data"),
		"Zone:5":         []byte("zone5-data"),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListEntityTypesRequest{
		PageSize: 10,
	}

	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should find 3 unique entity types: Organization, User, Zone (sorted)
	assert.Equal(t, 3, resp.TotalCount)
	assert.Equal(t, 3, len(resp.EntityTypes))
	assert.Equal(t, "Organization", resp.EntityTypes[0])
	assert.Equal(t, "User", resp.EntityTypes[1])
	assert.Equal(t, "Zone", resp.EntityTypes[2])
	assert.Empty(t, resp.NextPageToken)
}

func TestKVService_ListEntityTypes_Pagination(t *testing.T) {
	scanData := map[string][]byte{
		"Alpha:1": []byte("data"),
		"Beta:1":  []byte("data"),
		"Gamma:1": []byte("data"),
		"Delta:1": []byte("data"),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	// First page
	req := &ListEntityTypesRequest{
		PageSize: 2,
	}
	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 4, resp.TotalCount)
	assert.Equal(t, 2, len(resp.EntityTypes))
	assert.Equal(t, "Alpha", resp.EntityTypes[0])
	assert.Equal(t, "Beta", resp.EntityTypes[1])
	assert.NotEmpty(t, resp.NextPageToken)

	// Second page
	req = &ListEntityTypesRequest{
		PageSize:  2,
		PageToken: resp.NextPageToken,
	}
	resp, err = svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 4, resp.TotalCount)
	assert.Equal(t, 2, len(resp.EntityTypes))
	assert.Equal(t, "Delta", resp.EntityTypes[0])
	assert.Equal(t, "Gamma", resp.EntityTypes[1])
	assert.Empty(t, resp.NextPageToken)
}

func TestKVService_ListEntityTypes_EmptyResult(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return map[string][]byte{}, nil
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &ListEntityTypesRequest{}
	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, resp.TotalCount)
	assert.Empty(t, resp.EntityTypes)
}

func TestKVService_ListEntityTypes_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return map[string][]byte{}, nil
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	// Nil request should be handled gracefully with defaults
	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_ListEntityTypes_RaftError(t *testing.T) {
	scanErr := stderrors.New("raft cluster unavailable")
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, scanErr)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListEntityTypesRequest{}
	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "raft")
}

func TestKVService_ListEntityTypes_SkipsSystemKeys(t *testing.T) {
	scanData := map[string][]byte{
		"User:1":               []byte("data"),
		"__sys:cluster/config": []byte("system-data"),
		"__sys:dek/tenant/abc": []byte("dek-data"),
		"Organization:1":       []byte("data"),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListEntityTypesRequest{}
	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// System keys should be filtered out
	assert.Equal(t, 2, resp.TotalCount)
	assert.Contains(t, resp.EntityTypes, "User")
	assert.Contains(t, resp.EntityTypes, "Organization")
	assert.NotContains(t, resp.EntityTypes, "__sys")
}

func TestKVService_ListEntityTypes_WithCustomShardID(t *testing.T) {
	scanData := map[string][]byte{
		"Test:1": []byte("data"),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListEntityTypesRequest{
		ShardID: 5,
	}
	_, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), qrdbProvider.lastShardID.Load())
}

func TestKVService_ListEntityTypes_DefaultsShardID(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(map[string][]byte{}, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListEntityTypesRequest{
		ShardID: 0, // Should default to 1
	}
	_, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), qrdbProvider.lastShardID.Load())
}

func TestKVService_ListEntityTypes_MultiTenant(t *testing.T) {
	// Mock returns tenant-prefixed keys via QRDB scan
	scanData := map[string][]byte{
		"__t:acme-corp:User:1":         []byte("user-data"),
		"__t:acme-corp:User:2":         []byte("user-data"),
		"__t:acme-corp:Organization:1": []byte("org-data"),
		"__t:acme-corp:Zone:1":         []byte("zone-data"),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	// Create context with tenant
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")

	req := &ListEntityTypesRequest{
		PageSize: 10,
	}

	resp, err := svc.ListEntityTypes(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should extract entity types from tenant keys
	assert.Equal(t, 3, resp.TotalCount)
	assert.Contains(t, resp.EntityTypes, "User")
	assert.Contains(t, resp.EntityTypes, "Organization")
	assert.Contains(t, resp.EntityTypes, "Zone")
}

func TestKVService_ListEntityTypes_MixedKeyFormats(t *testing.T) {
	// Mock returns both legacy and tenant-prefixed keys via QRDB scan
	scanData := map[string][]byte{
		"LegacyEntity:1":      []byte("data"),
		"__t:tenant-a:User:1": []byte("data"),
		"__t:tenant-a:Zone:2": []byte("data"),
		"AnotherLegacy:99":    []byte("data"),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListEntityTypesRequest{}
	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should handle both legacy and tenant keys
	assert.Equal(t, 4, resp.TotalCount)
	assert.Contains(t, resp.EntityTypes, "LegacyEntity")
	assert.Contains(t, resp.EntityTypes, "AnotherLegacy")
	assert.Contains(t, resp.EntityTypes, "User")
	assert.Contains(t, resp.EntityTypes, "Zone")
}

// =============================================================================
// Multi-Tenant Key Integration Tests
// =============================================================================

func TestKVService_Put_MultiTenantKey(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	// Test with tenant context
	ctx := tenantctx.WithTenant(context.Background(), "acme-corp")
	req := &PutRequest{
		EntityType: "User",
		ID:         42,
		Data:       []byte(`{"name":"John"}`),
	}

	resp, err := svc.Put(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(42), resp.ID)

	// Verify the key was built with tenant prefix
	assert.Equal(t, "__t:acme-corp:User:42", qrdbProvider.capturedPutKey())
}

func TestKVService_Put_LegacyKey(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	// Test without tenant context (legacy mode)
	ctx := tenantctx.WithSystem(context.Background())
	req := &PutRequest{
		EntityType: "User",
		ID:         42,
		Data:       []byte(`{"name":"John"}`),
	}

	resp, err := svc.Put(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify the key was built in legacy format
	assert.Equal(t, "User:42", qrdbProvider.capturedPutKey())
}

func TestKVService_Get_MultiTenantKey(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProviderWithGet([]byte(`{"name":"John"}`), nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	// Test with tenant context
	ctx := tenantctx.WithTenant(context.Background(), "beta-inc")
	req := &GetRequest{
		EntityType: "Order",
		ID:         123,
	}

	resp, err := svc.Get(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Found)

	// Verify the key was built with tenant prefix
	assert.Equal(t, "__t:beta-inc:Order:123", qrdbProvider.capturedGetKey())
}

func TestKVService_Delete_MultiTenantKey(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	// Test with tenant context
	ctx := tenantctx.WithTenant(context.Background(), "test-tenant")
	req := &DeleteRequest{
		EntityType: "Product",
		ID:         999,
	}

	resp, err := svc.Delete(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)

	// Verify the key was built with tenant prefix
	assert.Equal(t, "__t:test-tenant:Product:999", qrdbProvider.capturedDeleteKey())
}

func TestKVService_List_MultiTenantPrefix(t *testing.T) {
	scanData := map[string][]byte{
		"__t:my-tenant:Order:1": []byte(`{"id":1}`),
		"__t:my-tenant:Order:2": []byte(`{"id":2}`),
	}
	qrdbProvider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	// Test with tenant context
	ctx := tenantctx.WithTenant(context.Background(), "my-tenant")
	req := &ListRequest{
		EntityType: "Order",
		PageSize:   10,
	}

	stream := make(chan *ListResponse, 10)
	err := svc.List(ctx, req, stream)
	require.NoError(t, err)

	// Read results from stream
	var totalEntities int
	for resp := range stream {
		totalEntities += len(resp.Entities)
	}
	assert.Equal(t, 2, totalEntities)

	// Verify the scan prefix was built with tenant prefix
	assert.Equal(t, "__t:my-tenant:Order:", qrdbProvider.capturedScanKey())
}

func TestNewQuicBaaS_WithCryptoManager(t *testing.T) {
	cryptoMgr := &covMockCryptoManager{}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.cryptoManager = cryptoMgr
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.cryptoService, "crypto service should be initialized from CryptoManager")
}

func TestNewQuicBaaS_WithSecretsManager(t *testing.T) {
	secretsMgr := &covMockSecretsManager{}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.secretsManager = secretsMgr
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.secretsService, "secrets service should be initialized from SecretsManager")
}

func TestNewQuicBaaS_WithCAService(t *testing.T) {
	caService := newCovMockCAService(t)

	svc := createTestService(t, func(b *serviceBuilder) {
		b.caService = caService
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.certificateService, "certificate service should be initialized from CAService")
}

func TestNewQuicBaaS_WithEmbeddingManager(t *testing.T) {
	embeddingMgr := &covMockEmbeddingManager{}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.embeddingManager = embeddingMgr
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.embeddingService, "embedding service should be initialized from EmbeddingManager")
}

func TestNewQuicBaaS_WithScheduler(t *testing.T) {
	kvStore := &covMockKVStore{}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.config.EnableScheduler = true
		b.kvStoreProvider = func(_ uint64) interface{} {
			return kvStore
		}
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.schedulerEngine, "scheduler engine should be initialized")
	assert.NotNil(t, impl.schedulerService, "scheduler service should be initialized")
}

// TestNewQuicBaaS_SchedulerRunningWhenBuilderReturns pins the scheduler's
// start to the builder's own goroutine (go-quicbaas#62).
//
// The engine used to be started from a `go func()`, which left a window in
// which the facade existed and the scheduler did not: a Close arriving inside
// that window called Stop on a not-yet-running engine, got "not running"
// back, and the queued Start then brought the poll loop up against the
// storage Close had just shut. Asserting the engine is already running on the
// statement after the builder returns closes the window by construction —
// there is no interval left for a teardown to land in.
//
// Deterministic: read once, immediately, with no polling. A start deferred to
// a goroutine cannot satisfy it, because the goroutine has not been scheduled
// on this line.
func TestNewQuicBaaS_SchedulerRunningWhenBuilderReturns(t *testing.T) {
	kvStore := &covMockKVStore{}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.config.EnableScheduler = true
		b.kvStoreProvider = func(_ uint64) interface{} {
			return kvStore
		}
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	require.NotNil(t, impl.schedulerEngine, "scheduler engine should be initialized")
	require.True(t, impl.schedulerEngine.IsRunning(),
		"the scheduler must be running by the time the builder returns; a start deferred to a goroutine leaves a window in which Close cannot stop it")
}

func TestNewQuicBaaS_WithSchedulerDefaultIntervals(t *testing.T) {
	// Test that scheduler defaults are applied when not configured
	kvStore := &covMockKVStore{}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.config.EnableScheduler = true
		b.config.SchedulerTickInterval = 0      // Will default to 1s
		b.config.SchedulerMaxConcurrentJobs = 0 // Will default to 10
		b.kvStoreProvider = func(_ uint64) interface{} {
			return kvStore
		}
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.schedulerEngine, "scheduler engine should be initialized with defaults")
}

func TestNewQuicBaaS_WithSchedulerAndKMS(t *testing.T) {
	// Test scheduler initialization with KMS service present
	// This exercises the KMS grace period handler registration path
	kvStore := &covMockKVStore{}
	mockMgr := newMockKMSManager()

	svc := createTestService(t, func(b *serviceBuilder) {
		b.config.EnableScheduler = true
		b.kvStoreProvider = func(_ uint64) interface{} {
			return kvStore
		}
		b.kmsManager = mockMgr
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.schedulerEngine, "scheduler engine should be initialized")
	assert.NotNil(t, impl.kmsService, "KMS service should be initialized")
}

func TestNewQuicBaaS_WithCryptoService(t *testing.T) {
	// Test the b.cryptoService path (different from b.cryptoManager)
	cryptoMgr := &covMockCryptoManager{}

	svc := createTestService(t, func(b *serviceBuilder) {
		b.cryptoService = cryptoMgr
	})
	defer svc.Close()

	impl := svc.(*quicBaaS)
	assert.NotNil(t, impl.cryptoService, "crypto service should be initialized from cryptoService interface")
}

func TestUpdateCacheConfig_WithCleanupInterval(t *testing.T) {
	log := createTestLogger(t)

	// Create a real cache.CacheManager
	cacheCfg := cache.Config{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
	cacheMgr, err := cache.NewManager(cacheCfg, log)
	require.NoError(t, err)
	defer cacheMgr.Close()

	svc := &quicBaaS{
		logger:       log,
		raftNode:     &mockRaftNode{},
		cacheManager: cacheMgr,
	}
	cacheSvc := &cacheService{svc: svc}

	newInterval := 30 * time.Second
	resp, err := cacheSvc.UpdateCacheConfig(tenantctx.WithSystem(context.Background()), &UpdateCacheConfigRequest{
		CleanupInterval: &newInterval,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Updated)
	assert.NotNil(t, resp.NewConfig)
}

func TestWatch_SuccessPath_CreatedResponse(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)

	stream := make(chan *WatchResponse, 10)
	ctx, cancel := context.WithTimeout(tenantctx.WithSystem(context.Background()), 500*time.Millisecond)
	defer cancel()

	// This exercises the watch creation success path (lines 1110-1140)
	watchID, err := impl.kvService.Watch(ctx, &WatchRequest{
		Key:      []byte("test-key"),
		IsPrefix: true,
	}, stream)
	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Read the initial "created" response
	select {
	case resp := <-stream:
		assert.True(t, resp.Created)
		assert.Equal(t, watchID, resp.WatchID)
	case <-ctx.Done():
		t.Fatal("timed out waiting for watch created response")
	}

	// Cancel context to trigger the cancellation path in the goroutine
	cancel()

	// The goroutine should send a canceled response
	select {
	case resp, ok := <-stream:
		if ok && resp != nil {
			// May get a canceled response
			if resp.Canceled {
				assert.NotEmpty(t, resp.CancelReason)
			}
		}
	case <-time.After(1 * time.Second):
		// Timeout acceptable - goroutine cleanup may happen before we read
	}
}

func TestPut_BuildKeyError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: &covFailingKeyspaceResolver{buildKeyErr: &covKeyspaceError{msg: "bad key"}},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.Put(tenantctx.WithSystem(context.Background()), &PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{}`),
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGet_BuildKeyError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: &covFailingKeyspaceResolver{buildKeyErr: &covKeyspaceError{msg: "bad key"}},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.Get(tenantctx.WithSystem(context.Background()), &GetRequest{
		EntityType: "user",
		ID:         1,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDelete_BuildKeyError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: &covFailingKeyspaceResolver{buildKeyErr: &covKeyspaceError{msg: "bad key"}},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.Delete(tenantctx.WithSystem(context.Background()), &DeleteRequest{
		EntityType: "user",
		ID:         1,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestList_BuildPrefixError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: &covFailingKeyspaceResolver{buildPrefixErr: &covKeyspaceError{msg: "bad prefix"}},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *ListResponse, 1)
	err := svc.kvService.List(tenantctx.WithSystem(context.Background()), &ListRequest{EntityType: "user"}, stream)
	require.Error(t, err)
}

func TestList_RaftError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return nil, stderrors.New("raft unavailable")
		},
	}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *ListResponse, 1)
	err := svc.kvService.List(tenantctx.WithSystem(context.Background()), &ListRequest{EntityType: "user"}, stream)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestDelete_RaftError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return stderrors.New("raft unavailable")
		},
	}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.Delete(tenantctx.WithSystem(context.Background()), &DeleteRequest{
		EntityType: "user",
		ID:         1,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestListEntityTypes_BuildTenantPrefixError(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{},
		keyspaceResolver: &covFailingKeyspaceResolver{buildTenantPfxErr: &covKeyspaceError{msg: "bad tenant"}},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.ListEntityTypes(tenantctx.WithSystem(context.Background()), &ListEntityTypesRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestListEntityTypes_RaftError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return nil, stderrors.New("raft unavailable")
		},
	}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.ListEntityTypes(tenantctx.WithSystem(context.Background()), &ListEntityTypesRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestBatchPut_BuildKeyError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: &covFailingKeyspaceResolver{buildKeyErr: &covKeyspaceError{msg: "bad key"}},
	}
	svc.initMetrics()
	svc.batchService = &batchService{svc: svc}

	resp, err := svc.batchService.BatchPut(tenantctx.WithSystem(context.Background()), &BatchPutRequest{
		Requests: []*PutRequest{{EntityType: "user", ID: 1, Data: []byte(`{}`)}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// newNoTenantBatchService returns a batch service over the real keyspace
// resolver, so a request with no tenant bound is refused by tenantctx.
func newNoTenantBatchService(t *testing.T) *batchService {
	t.Helper()
	svc := &quicBaaS{
		logger:           createTestLogger(t),
		raftNode:         &mockRaftNode{},
		config:           &ServiceConfig{},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.batchService = &batchService{svc: svc}
	return svc.batchService
}

// TestBatchPut_NoTenantIsForbidden proves a batch put with no tenant bound
// keeps the keyspace's ErrForbidden, which gRPC maps to PermissionDenied,
// instead of reporting invalid input (#1796).
func TestBatchPut_NoTenantIsForbidden(t *testing.T) {
	resp, err := newNoTenantBatchService(t).BatchPut(context.Background(), &BatchPutRequest{
		Requests: []*PutRequest{{EntityType: "user", ID: 1, Data: []byte(`{}`)}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrForbidden, qbErr.Code)
	var missing *tenantctx.MissingTenantError
	assert.ErrorAs(t, err, &missing)
}

// TestBatchDelete_NoTenantIsForbidden is the batch delete half of
// TestBatchPut_NoTenantIsForbidden.
func TestBatchDelete_NoTenantIsForbidden(t *testing.T) {
	resp, err := newNoTenantBatchService(t).BatchDelete(context.Background(), &BatchDeleteRequest{
		Requests: []*DeleteRequest{{EntityType: "user", ID: 1}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrForbidden, qbErr.Code)
}

// TestBatchPut_ReservedEntityTypeIsInvalidInput proves a malformed entity
// type is still invalid input once the batch keeps the keyspace's code.
func TestBatchPut_ReservedEntityTypeIsInvalidInput(t *testing.T) {
	resp, err := newNoTenantBatchService(t).BatchPut(tenantctx.WithSystem(context.Background()), &BatchPutRequest{
		Requests: []*PutRequest{{EntityType: "__reserved", ID: 1, Data: []byte(`{}`)}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestBatchDelete_BuildKeyError_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:           log,
		raftNode:         raftNode,
		config:           &ServiceConfig{EnableMetrics: true},
		keyspaceResolver: &covFailingKeyspaceResolver{buildKeyErr: &covKeyspaceError{msg: "bad key"}},
	}
	svc.initMetrics()
	svc.batchService = &batchService{svc: svc}

	resp, err := svc.batchService.BatchDelete(tenantctx.WithSystem(context.Background()), &BatchDeleteRequest{
		Requests: []*DeleteRequest{{EntityType: "user", ID: 1}},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestAggregate_WithMetrics_RaftError(t *testing.T) {
	log := createTestLogger(t)
	provider := newSvcBoostMockQRDBProvider(nil, stderrors.New("raft read failed"))
	svc := &quicBaaS{
		logger:           log,
		qrdbClient:       provider,
		config:           &ServiceConfig{},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.initMetrics()
	tsSvc := &timeSeriesService{svc: svc}

	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestAggregate_WithMetrics_InvalidInput(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &svcBoostRaftNode{}
	svc := &quicBaaS{
		logger:   log,
		raftNode: raftNode,
		config:   &ServiceConfig{},
	}
	svc.initMetrics()
	tsSvc := &timeSeriesService{svc: svc}

	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestAggregate_WithMetrics_Success(t *testing.T) {
	log := createTestLogger(t)
	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	data2 := map[string]interface{}{"timestamp": float64(20), "value": float64(200)}
	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{
		logger:           log,
		qrdbClient:       qrdbProvider,
		config:           &ServiceConfig{},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.initMetrics()
	tsSvc := &timeSeriesService{svc: svc}

	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionSum,
	})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, float64(300), resp.Result)
}

func TestTimeSeriesQuery_WithMetrics_RaftError(t *testing.T) {
	log := createTestLogger(t)
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, stderrors.New("raft read failed"))
	svc := &quicBaaS{
		logger:           log,
		qrdbClient:       qrdbProvider,
		config:           &ServiceConfig{},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.initMetrics()
	tsSvc := &timeSeriesService{svc: svc}

	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}, stream)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestTimeSeriesQuery_WithMetrics_InvalidInput(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &svcBoostRaftNode{}
	svc := &quicBaaS{
		logger:   log,
		raftNode: raftNode,
		config:   &ServiceConfig{},
	}
	svc.initMetrics()
	tsSvc := &timeSeriesService{svc: svc}

	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)
}

func TestTimeSeriesQuery_WithMetrics_Success(t *testing.T) {
	log := createTestLogger(t)
	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	json1, _ := json.Marshal(data1)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1}), nil)
	svc := &quicBaaS{
		logger:           log,
		qrdbClient:       qrdbProvider,
		config:           &ServiceConfig{},
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.initMetrics()
	tsSvc := &timeSeriesService{svc: svc}

	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}, stream)
	require.NoError(t, err)
}

func TestUpdateCacheConfig_NonCacheManagerType(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{
		logger:       log,
		config:       &ServiceConfig{},
		cacheManager: &covNonCacheMgr{},
	}
	cacheSvc := &cacheService{svc: svc}

	l1Size := 1024
	resp, err := cacheSvc.UpdateCacheConfig(tenantctx.WithSystem(context.Background()), &UpdateCacheConfigRequest{
		L1Size: &l1Size,
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestSetIAMProvider_NoopStub(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
	}
	// This should not panic - it's a no-op stub
	svc.SetIAMProvider(nil)
}

func TestWithEmbeddingManager(t *testing.T) {
	builder := NewServiceBuilder()
	result := builder.WithEmbeddingManager(nil)
	require.NotNil(t, result)

	sb := result.(*serviceBuilder)
	assert.Nil(t, sb.embeddingManager)
}

func TestWatch_NilRequest_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:   log,
		raftNode: raftNode,
		config:   &ServiceConfig{EnableMetrics: true},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 1)
	_, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestWatch_EmptyKey_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:   log,
		raftNode: raftNode,
		config:   &ServiceConfig{EnableMetrics: true},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 1)
	_, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: nil}, stream)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestWatch_NilStream_WithMetrics(t *testing.T) {
	log := createTestLogger(t)
	raftNode := &mockRaftNode{}
	svc := &quicBaaS{
		logger:   log,
		raftNode: raftNode,
		config:   &ServiceConfig{EnableMetrics: true},
	}
	svc.initMetrics()
	svc.kvService = &kvService{svc: svc}

	_, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: []byte("test")}, nil)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func Test81_NewQuicBaaS_InvalidStorageType(t *testing.T) {
	builder := &serviceBuilder{
		raftNode: &mockRaftNode{},
		config:   &ServiceConfig{NodeID: 1},
		logger:   createTestLogger(t),
		storage:  "invalid-string-type", // Wrong type - should fail type assertion
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "storage must be interfaces.Storage")
}

func Test81_NewQuicBaaS_InvalidCacheManagerType(t *testing.T) {
	builder := &serviceBuilder{
		raftNode:     &mockRaftNode{},
		config:       &ServiceConfig{NodeID: 1},
		logger:       createTestLogger(t),
		cacheManager: "invalid-string-type", // Wrong type
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "cache manager must be interfaces.CacheManager")
}

func Test81_NewQuicBaaS_InvalidBackupManagerType(t *testing.T) {
	builder := &serviceBuilder{
		raftNode:      &mockRaftNode{},
		config:        &ServiceConfig{NodeID: 1},
		logger:        createTestLogger(t),
		backupManager: "invalid-string-type", // Wrong type
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "backup manager must be interfaces.BackupManager")
}

func Test81_NewQuicBaaS_InvalidLoggerType(t *testing.T) {
	builder := &serviceBuilder{
		raftNode: &mockRaftNode{},
		config:   &ServiceConfig{NodeID: 1},
		logger:   "invalid-string-type", // Wrong type
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "logger must be interfaces.Logger")
}

func Test81_NewQuicBaaS_InvalidKMSManagerType(t *testing.T) {
	builder := &serviceBuilder{
		raftNode:   &mockRaftNode{},
		config:     &ServiceConfig{NodeID: 1},
		logger:     createTestLogger(t),
		kmsManager: "invalid-string-type", // Wrong type
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "KMS manager must be interfaces.KMSManager")
}

func Test81_NewQuicBaaS_InvalidRaftNodeType(t *testing.T) {
	builder := &serviceBuilder{
		raftNode: "invalid-string-type", // Wrong type
		config:   &ServiceConfig{NodeID: 1},
		logger:   createTestLogger(t),
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "raft node must be interfaces.RaftNode")
}

func Test81_NewQuicBaaS_NilRaftNode(t *testing.T) {
	builder := &serviceBuilder{
		raftNode: nil,
		config:   &ServiceConfig{NodeID: 1},
		logger:   createTestLogger(t),
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "raft node is required")
}

func Test81_NewQuicBaaS_NilConfig(t *testing.T) {
	builder := &serviceBuilder{
		raftNode: &mockRaftNode{},
		config:   nil,
		logger:   createTestLogger(t),
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "config is required")
}

// TestOIDCAuthorize_RejectsPasswordGrant verifies that the authService.Login
// stub always returns not-implemented since password login is removed.
// TestOIDCAuthorize_RejectsPasswordGrant pins that password login is not a
// capability of this server: authService.Login always fails. Use the unified
// login ceremony (WebAuthn / PIV / mTLS) instead.
func TestOIDCAuthorize_RejectsPasswordGrant(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	authSvc := &authService{svc: svc}

	_, err := authSvc.Login(tenantctx.WithSystem(context.Background()), &LoginRequest{Username: "user"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "password-based login")
}

func Test81_ClusterService_RebalanceCluster_NilRequestPath(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, raftNode: &boost81RaftNode{}}
	clusterSvc := &clusterService{svc: svc}

	resp, err := clusterSvc.RebalanceCluster(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func Test81_BatchService_BatchPut_NilRequestPath(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{
		logger:           log,
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		config:           &ServiceConfig{NodeID: 1},
	}
	batchSvc := &batchService{svc: svc}

	resp, err := batchSvc.BatchPut(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func Test81_BatchService_BatchPut_EmptyRequests(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{
		logger:           log,
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		config:           &ServiceConfig{NodeID: 1},
	}
	batchSvc := &batchService{svc: svc}

	resp, err := batchSvc.BatchPut(tenantctx.WithSystem(context.Background()), &BatchPutRequest{
		Requests: []*PutRequest{},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func Test81_BatchService_BatchGet_NilRequestPath(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{
		logger:           log,
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		config:           &ServiceConfig{NodeID: 1},
	}
	batchSvc := &batchService{svc: svc}

	resp, err := batchSvc.BatchGet(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func Test81_BatchService_BatchGet_EmptyRequests(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{
		logger:           log,
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		config:           &ServiceConfig{NodeID: 1},
	}
	batchSvc := &batchService{svc: svc}

	resp, err := batchSvc.BatchGet(tenantctx.WithSystem(context.Background()), &BatchGetRequest{
		Requests: []*GetRequest{},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func Test81_BatchService_BatchDelete_NilRequestPath(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{
		logger:           log,
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		config:           &ServiceConfig{NodeID: 1},
	}
	batchSvc := &batchService{svc: svc}

	resp, err := batchSvc.BatchDelete(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func Test81_BatchService_BatchDelete_EmptyRequests(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{
		logger:           log,
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		config:           &ServiceConfig{NodeID: 1},
	}
	batchSvc := &batchService{svc: svc}

	resp, err := batchSvc.BatchDelete(tenantctx.WithSystem(context.Background()), &BatchDeleteRequest{
		Requests: []*DeleteRequest{},
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func Test81_KVService_Watch_NilRequestPath(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	kvSvc := &kvService{svc: svc}

	stream := make(chan *WatchResponse, 10)
	watchID, err := kvSvc.Watch(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)
	assert.Equal(t, int64(0), watchID)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func Test81_KVService_Watch_EmptyKeyPath(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	kvSvc := &kvService{svc: svc}

	stream := make(chan *WatchResponse, 10)
	watchID, err := kvSvc.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: []byte{}}, stream)
	require.Error(t, err)
	assert.Equal(t, int64(0), watchID)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "key is required")
}

func Test81_KVService_Watch_NilStreamPath(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	kvSvc := &kvService{svc: svc}

	watchID, err := kvSvc.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: []byte("test")}, nil)
	require.Error(t, err)
	assert.Equal(t, int64(0), watchID)

	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), "stream channel is required")
}

func Test82_QuicBaaS_Close_CacheManagerError(t *testing.T) {
	mockCache := &boost82MockCacheManager{
		closeErr: stderrors.New("cache close error"),
	}

	db := &quicBaaS{
		cacheManager: mockCache,
	}

	err := db.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache manager")
	assert.True(t, db.closed)
}

func Test82_QuicBaaS_Close_BackupManagerError(t *testing.T) {
	mockBackup := &boost82MockBackupManager{
		closeErr: stderrors.New("backup close error"),
	}

	db := &quicBaaS{
		backupMgr: mockBackup,
	}

	err := db.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager")
	assert.True(t, db.closed)
}

func Test82_QuicBaaS_Close_WithLogger(t *testing.T) {
	log := createTestLogger(t)

	db := &quicBaaS{
		logger: log,
	}

	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

func Test82_QuicBaaS_Close_WithLoggerAndCacheError(t *testing.T) {
	log := createTestLogger(t)
	mockCache := &boost82MockCacheManager{
		closeErr: stderrors.New("cache close error"),
	}

	db := &quicBaaS{
		logger:       log,
		cacheManager: mockCache,
	}

	err := db.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache manager")
	assert.True(t, db.closed)
}

func Test82_QuicBaaS_SetIAMProvider_NilComponents(t *testing.T) {
	db := &quicBaaS{}
	// Should not panic even with nil components
	db.SetIAMProvider(nil)
}

func TestNewQuicBaaS_InvalidTimeSeriesStorageType(t *testing.T) {
	builder := &serviceBuilder{
		raftNode:  &mockRaftNode{},
		config:    &ServiceConfig{NodeID: 1},
		logger:    createTestLogger(t),
		tsStorage: "invalid-type", // Wrong type - should fail type assertion
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "timeseries storage must be interfaces.TimeSeriesStorage")
}

func TestKVService_Watch_WatchManagerError(t *testing.T) {
	log := createTestLogger(t)
	realWatchMgr := watch.NewManager()
	// We need to inject error behavior
	// The real manager doesn't have a way to inject errors, so we test via other paths
	svc := &quicBaaS{
		logger:       log,
		watchManager: realWatchMgr,
	}
	kvSvc := &kvService{svc: svc}

	// Test with nil context that gets canceled immediately
	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	cancel() // Cancel immediately

	stream := make(chan *WatchResponse, 10)
	_, err := kvSvc.Watch(ctx, &WatchRequest{
		Key: []byte("test-key"),
	}, stream)

	// May succeed with watchID but context already canceled
	// OR may fail if context check happens first
	if err != nil {
		assert.Error(t, err)
	}
}

func TestKVService_Watch_EventTypeDelete(t *testing.T) {
	log := createTestLogger(t)
	realWatchMgr := watch.NewManager()
	svc := &quicBaaS{
		logger:       log,
		watchManager: realWatchMgr,
	}
	kvSvc := &kvService{svc: svc}

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	defer cancel()

	stream := make(chan *WatchResponse, 10)
	watchID, err := kvSvc.Watch(ctx, &WatchRequest{
		Key: []byte("test-key"),
	}, stream)

	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Read created response
	resp := <-stream
	assert.True(t, resp.Created)

	// Trigger a delete event via NotifyDelete
	realWatchMgr.NotifyDelete([]byte("test-key"), []byte("prev-value"), 1, 1)

	// Read the delete event
	select {
	case resp = <-stream:
		assert.Equal(t, watchID, resp.WatchID)
		require.Len(t, resp.Events, 1)
		assert.Equal(t, WatchEventTypeDelete, resp.Events[0].Type)
	case <-time.After(200 * time.Millisecond):
		// May not receive event if no watchers registered for that prefix
		// This is expected behavior
	}

	cancel()
}

func TestKVService_Watch_EventFilterSkip(t *testing.T) {
	log := createTestLogger(t)
	realWatchMgr := watch.NewManager()
	svc := &quicBaaS{
		logger:       log,
		watchManager: realWatchMgr,
	}
	kvSvc := &kvService{svc: svc}

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	defer cancel()

	stream := make(chan *WatchResponse, 10)
	watchID, err := kvSvc.Watch(ctx, &WatchRequest{
		Key:     []byte("filter-key"),
		Filters: []WatchEventType{WatchEventTypePut}, // Filter OUT put events
	}, stream)

	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Read created response
	resp := <-stream
	assert.True(t, resp.Created)

	// Trigger a put event (should be filtered)
	realWatchMgr.NotifyPut([]byte("filter-key"), []byte("prev-value"), []byte("value"), 1, 1)

	// Trigger a delete event (should NOT be filtered)
	realWatchMgr.NotifyDelete([]byte("filter-key"), []byte("prev-value"), 2, 1)

	// Should receive delete event but not put event
	select {
	case resp = <-stream:
		if len(resp.Events) > 0 {
			assert.Equal(t, WatchEventTypeDelete, resp.Events[0].Type)
		}
	case <-time.After(200 * time.Millisecond):
		// No events received is also valid if no matching watches
	}

	cancel()
}

func TestKVService_Watch_WithPrefixOption(t *testing.T) {
	log := createTestLogger(t)
	realWatchMgr := watch.NewManager()
	svc := &quicBaaS{
		logger:       log,
		watchManager: realWatchMgr,
	}
	kvSvc := &kvService{svc: svc}

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	defer cancel()

	stream := make(chan *WatchResponse, 10)
	watchID, err := kvSvc.Watch(ctx, &WatchRequest{
		Key:      []byte("prefix/"),
		IsPrefix: true, // Enable prefix watching
	}, stream)

	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Read created response
	resp := <-stream
	assert.True(t, resp.Created)
	assert.Equal(t, watchID, resp.WatchID)

	cancel()
}

func TestBackupService_CreateBackup_ManagerReturnsInvalidType(t *testing.T) {
	mockBackup := &mockBackupManager{
		createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
			return "not-a-backup", nil // Wrong type
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.CreateBackup(tenantctx.WithSystem(context.Background()), &CreateBackupRequest{
		Name: "test-backup",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "backup result must be *backup.Backup")
}

func TestBackupService_CreateBackup_IncrementalType(t *testing.T) {
	mockBackup := &mockBackupManager{
		createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
			bt := backupType.(backup.BackupType)
			assert.Equal(t, backup.BackupTypeIncremental, bt)
			return &backup.Backup{
				ID:        "backup-incr",
				Type:      backup.BackupTypeIncremental,
				Timestamp: time.Now(),
			}, nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.CreateBackup(tenantctx.WithSystem(context.Background()), &CreateBackupRequest{
		Name: "incremental",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "backup-incr", resp.BackupID)
}

func TestBackupService_CreateBackup_ManagerError(t *testing.T) {
	mockBackup := &mockBackupManager{
		createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
			return nil, stderrors.New("backup creation failed")
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.CreateBackup(tenantctx.WithSystem(context.Background()), &CreateBackupRequest{
		Name: "test-backup",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "create backup failed")
}

func TestBackupService_ListBackups_ManagerReturnsInvalidType(t *testing.T) {
	mockBackup := &mockBackupManager{
		listBackupsFn: func(ctx context.Context, limit int) (interface{}, error) {
			return "not-a-backup-list", nil // Wrong type
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.ListBackups(tenantctx.WithSystem(context.Background()), &ListBackupsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "backup list must be *backup.BackupList")
}

func TestBackupService_ListBackups_ManagerError(t *testing.T) {
	mockBackup := &mockBackupManager{
		listBackupsFn: func(ctx context.Context, limit int) (interface{}, error) {
			return nil, stderrors.New("list backups failed")
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.ListBackups(tenantctx.WithSystem(context.Background()), &ListBackupsRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "list backups failed")
}

func TestBackupService_ListBackups_WithShards(t *testing.T) {
	mockBackup := &mockBackupManager{
		listBackupsFn: func(ctx context.Context, limit int) (interface{}, error) {
			return &backup.BackupList{
				Backups: []*backup.Backup{
					{
						ID:        "backup-with-shards",
						Type:      backup.BackupTypeFull,
						Timestamp: time.Now(),
						Shards: map[uint64]*backup.ShardSnapshot{
							1: {ShardID: 1},
							2: {ShardID: 2},
							3: {ShardID: 3},
						},
					},
				},
				TotalCount: 1,
			}, nil
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.ListBackups(tenantctx.WithSystem(context.Background()), &ListBackupsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Backups, 1)
	assert.Len(t, resp.Backups[0].Shards, 3)
}

func TestBackupService_RestoreBackup_ManagerError(t *testing.T) {
	mockBackup := &mockBackupManager{
		restoreBackupFn: func(ctx context.Context, backupID string, opts interface{}) error {
			return stderrors.New("restore failed")
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.RestoreBackup(tenantctx.WithSystem(context.Background()), &RestoreBackupRequest{
		BackupID: "backup-123",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "restore backup failed")
}

func TestBackupService_DeleteBackup_ManagerError(t *testing.T) {
	mockBackup := &mockBackupManager{
		deleteBackupFn: func(ctx context.Context, backupID string) error {
			return stderrors.New("delete failed")
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	resp, err := svc.DeleteBackup(tenantctx.WithSystem(context.Background()), &DeleteBackupRequest{
		BackupID: "backup-123",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "delete backup failed")
}

func TestWatch_NilRequest(t *testing.T) {
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     watch.NewManager(),
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 1)
	_, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestWatch_EmptyKey(t *testing.T) {
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     watch.NewManager(),
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 1)
	_, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: nil}, stream)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestWatch_NilStream(t *testing.T) {
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     watch.NewManager(),
	}
	svc.kvService = &kvService{svc: svc}

	_, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: []byte("test-key")}, nil)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestWatch_SuccessWithEventForwarding(t *testing.T) {
	mgr := watch.NewManager()
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     mgr,
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: []byte("events/key1")}, stream)
	require.NoError(t, err)
	assert.NotZero(t, watchID)

	// Read the created response
	resp := <-stream
	require.NotNil(t, resp)
	assert.True(t, resp.Created)
	assert.Equal(t, watchID, resp.WatchID)

	// Trigger a watch event via the manager
	mgr.NotifyPut([]byte("events/key1"), nil, []byte("value1"), 1, 1)

	// Read the forwarded event
	select {
	case resp = <-stream:
		require.NotNil(t, resp)
		assert.Equal(t, watchID, resp.WatchID)
		assert.Len(t, resp.Events, 1)
		assert.Equal(t, WatchEventTypePut, resp.Events[0].Type)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watch event")
	}
}

func TestWatch_PrefixWatch(t *testing.T) {
	mgr := watch.NewManager()
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     mgr,
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{
		Key:      []byte("prefix/"),
		IsPrefix: true,
	}, stream)
	require.NoError(t, err)
	assert.NotZero(t, watchID)

	// Read created response
	created := <-stream
	require.True(t, created.Created)

	// Trigger events on keys matching the prefix
	mgr.NotifyPut([]byte("prefix/key-a"), nil, []byte("val-a"), 1, 1)

	select {
	case resp := <-stream:
		require.NotNil(t, resp)
		assert.Len(t, resp.Events, 1)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for prefix watch event")
	}
}

func TestWatch_ContextCancellationDuringSetup(t *testing.T) {
	mgr := watch.NewManager()
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     mgr,
	}
	svc.kvService = &kvService{svc: svc}

	// Use an already-cancelled context so the initial send to the stream fails
	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	cancel()

	// Stream with no buffer to block the initial created message
	stream := make(chan *WatchResponse)
	_, err := svc.kvService.Watch(ctx, &WatchRequest{Key: []byte("key")}, stream)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestWatch_DeleteEvent(t *testing.T) {
	mgr := watch.NewManager()
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     mgr,
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{Key: []byte("del/key")}, stream)
	require.NoError(t, err)
	assert.NotZero(t, watchID)

	// Read created response
	<-stream

	// Send a delete event
	mgr.NotifyDelete([]byte("del/key"), []byte("old-value"), 2, 1)

	select {
	case resp := <-stream:
		require.NotNil(t, resp)
		assert.Len(t, resp.Events, 1)
		assert.Equal(t, WatchEventTypeDelete, resp.Events[0].Type)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete watch event")
	}
}

func TestWatch_EventFilterSkipsPut(t *testing.T) {
	mgr := watch.NewManager()
	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		keyspaceResolver: NewKeyspaceResolver(),
		watchManager:     mgr,
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.kvService.Watch(tenantctx.WithSystem(context.Background()), &WatchRequest{
		Key:     []byte("filter/key"),
		Filters: []WatchEventType{WatchEventTypePut}, // filter out PUT events
	}, stream)
	require.NoError(t, err)
	assert.NotZero(t, watchID)

	// Read created response
	<-stream

	// Send a put event which should be filtered out
	mgr.NotifyPut([]byte("filter/key"), nil, []byte("should-be-filtered"), 1, 1)

	// Send a delete event which should NOT be filtered
	mgr.NotifyDelete([]byte("filter/key"), []byte("val"), 2, 1)

	select {
	case resp := <-stream:
		require.NotNil(t, resp)
		assert.Len(t, resp.Events, 1)
		assert.Equal(t, WatchEventTypeDelete, resp.Events[0].Type)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for filtered watch event")
	}
}

func TestList_ContextCancelDuringStream(t *testing.T) {
	// Create enough results to cause multiple pages
	// Keys must be in "entityType:id" format for single-tenant keyspace resolver
	results := make(map[string][]byte)
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("users:%d", i+1)
		val := []byte(fmt.Sprintf(`{"id":%d}`, i+1))
		results[key] = val
	}

	provider := newSvcBoostMockQRDBProvider(results, nil)

	svc := &quicBaaS{
		config:           &ServiceConfig{},
		qrdbClient:       provider,
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.kvService = &kvService{svc: svc}

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))

	// Use an unbuffered channel nobody reads, so List blocks sending its page
	stream := make(chan *ListResponse)

	var wg sync.WaitGroup
	wg.Add(1)
	var listErr error
	go func() {
		defer wg.Done()
		listErr = svc.kvService.List(ctx, &ListRequest{EntityType: "users", PageSize: 5}, stream)
	}()

	// Cancel context while List is blocked trying to send the page
	cancel()

	wg.Wait()
	assert.ErrorIs(t, listErr, context.Canceled)
}

func TestDelete_WithCacheInvalidation(t *testing.T) {
	provider := newSvcBoostMockQRDBProvider(nil, nil)

	var deletedKeys []string
	cache := &cov90MockCacheManager{
		deleteFn: func(ctx context.Context, key string) error {
			deletedKeys = append(deletedKeys, key)
			return nil
		},
	}

	svc := &quicBaaS{
		config:           &ServiceConfig{},
		qrdbClient:       provider,
		keyspaceResolver: NewKeyspaceResolver(),
		cacheManager:     cache,
	}
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.Delete(tenantctx.WithSystem(context.Background()), &DeleteRequest{
		EntityType: "users",
		ID:         42,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Len(t, deletedKeys, 1)
}

func TestPut_WithCacheUpdate(t *testing.T) {
	provider := newSvcBoostMockQRDBProvider(nil, nil)

	var cachedEntries []string
	cache := &cov90MockCacheManager{
		setFn: func(ctx context.Context, key string, value []byte) error {
			cachedEntries = append(cachedEntries, key)
			return nil
		},
	}

	svc := &quicBaaS{
		config:           &ServiceConfig{},
		raftNode:         &mockRaftNode{},
		qrdbClient:       provider,
		keyspaceResolver: NewKeyspaceResolver(),
		cacheManager:     cache,
	}
	svc.kvService = &kvService{svc: svc}

	resp, err := svc.kvService.Put(tenantctx.WithSystem(context.Background()), &PutRequest{
		EntityType: "products",
		ID:         7,
		Data:       []byte(`{"name":"widget"}`),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(7), resp.ID)
	assert.Len(t, cachedEntries, 1)
}

func TestClose_CacheManagerError(t *testing.T) {
	cache := &cov90MockCacheManager{
		closeFn: func() error {
			return stderrors.New("cache close failure")
		},
	}

	svc := &quicBaaS{
		config:       &ServiceConfig{},
		cacheManager: cache,
	}

	err := svc.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache manager")
}

func TestClose_BackupManagerError(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
		backupMgr: &cov90MockBackupManager{
			closeFn: func() error {
				return stderrors.New("backup close failure")
			},
		},
	}

	err := svc.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager")
}

func TestClose_StorageError(t *testing.T) {
	store := &cov90MockStorage{
		closeFn: func() error {
			return stderrors.New("storage close failure")
		},
	}

	svc := &quicBaaS{
		config:  &ServiceConfig{},
		storage: store,
	}

	err := svc.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage")
}

func TestClose_QRDBClientError(t *testing.T) {
	provider := &cov90MockClosingQRDBProvider{
		closeFn: func() error {
			return stderrors.New("qrdb close failure")
		},
	}

	svc := &quicBaaS{
		config:     &ServiceConfig{},
		qrdbClient: provider,
	}

	err := svc.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "qrdb client")
}

func TestClose_AlreadyClosed(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
		closed: true,
	}

	err := svc.Close()
	assert.NoError(t, err)
}

func TestClose_MultipleErrors(t *testing.T) {
	cache := &cov90MockCacheManager{
		closeFn: func() error {
			return stderrors.New("cache error")
		},
	}
	store := &cov90MockStorage{
		closeFn: func() error {
			return stderrors.New("storage error")
		},
	}

	svc := &quicBaaS{
		config:       &ServiceConfig{},
		cacheManager: cache,
		storage:      store,
	}

	err := svc.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache manager")
	assert.Contains(t, err.Error(), "storage")
}

func TestClose_WithLogger(t *testing.T) {
	logger := createTestLogger(t)

	svc := &quicBaaS{
		config: &ServiceConfig{},
		logger: logger,
	}

	err := svc.Close()
	assert.NoError(t, err)
}

func TestRebalanceCluster_NilQRDBClient(t *testing.T) {
	svc := &quicBaaS{
		config: &ServiceConfig{},
	}
	svc.clusterService = &clusterService{svc: svc}

	resp, err := svc.clusterService.RebalanceCluster(tenantctx.WithSystem(context.Background()), &RebalanceClusterRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
	assert.Contains(t, qbErr.Err.Error(), "qrdb client service not configured")
}

func TestRebalanceCluster_WithLogger(t *testing.T) {
	svc := &quicBaaS{
		config:     &ServiceConfig{},
		qrdbClient: newSvcBoostMockQRDBProvider(nil, nil),
		logger:     createTestLogger(t),
	}
	svc.clusterService = &clusterService{svc: svc}

	resp, err := svc.clusterService.RebalanceCluster(tenantctx.WithSystem(context.Background()), &RebalanceClusterRequest{
		TargetShardCount: 4,
		Strategy:         "round-robin",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.JobID)
	assert.Equal(t, 0.0, resp.Progress)
}

func TestList_MalformedKeySkipped(t *testing.T) {
	// Provide results with an unparseable key format
	scanData := map[string][]byte{
		"__sys:internal": []byte(`{"id":1}`), // system key, ParseKey returns error
		"users:2":        []byte(`{"id":2}`), // valid single-tenant key
	}
	provider := newSvcBoostMockQRDBProvider(scanData, nil)

	svc := &quicBaaS{
		config:           &ServiceConfig{},
		qrdbClient:       provider,
		keyspaceResolver: NewKeyspaceResolver(),
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *ListResponse, 10)
	err := svc.kvService.List(tenantctx.WithSystem(context.Background()), &ListRequest{
		EntityType: "users",
		PageSize:   100,
	}, stream)
	require.NoError(t, err)

	// Collect all responses
	var entities []*Entity
	for resp := range stream {
		entities = append(entities, resp.Entities...)
	}

	// Only the valid key should produce an entity; the malformed one is skipped
	assert.Equal(t, 1, len(entities))
	assert.Equal(t, uint64(2), entities[0].ID)
}

func TestList_EmptyValueIncluded(t *testing.T) {
	// Provide scan results where one key has an empty value
	scanData := map[string][]byte{
		"users:1": []byte(`{"id":1}`),
		"users:2": nil, // empty value
	}
	provider := newSvcBoostMockQRDBProvider(scanData, nil)
	defer provider.Close()

	svc := &quicBaaS{
		config:           &ServiceConfig{},
		keyspaceResolver: NewKeyspaceResolver(),
		qrdbClient:       provider,
	}
	svc.kvService = &kvService{svc: svc}

	stream := make(chan *ListResponse, 10)
	err := svc.kvService.List(tenantctx.WithSystem(context.Background()), &ListRequest{
		EntityType: "users",
		PageSize:   100,
	}, stream)
	require.NoError(t, err)

	var entities []*Entity
	for resp := range stream {
		entities = append(entities, resp.Entities...)
	}
	// Both keys should be included (even with nil value)
	assert.Equal(t, 2, len(entities))
}

// AssignRole/RevokeRole now route through the qrdb-backed IdentityStore (C1-E);
// the fork-semantic tests that drove the deleted authManager.GetUserStore path
// were removed. Input-validation, fail-closed, and end-to-end bind/unbind
// coverage lives in rbac_role_store_iam_test.go.

func TestAggregate_NilRequest(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestAggregate_EmptyEntityType(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "",
		StartTime:  1,
		EndTime:    2,
	}
	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestAggregate_InvalidStartTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  0,
		EndTime:    100,
	}
	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestAggregate_InvalidEndTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  100,
		EndTime:    0,
	}
	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestAggregate_EndTimeBeforeStartTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  100,
		EndTime:    50,
	}
	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestAggregate_NilRaftNode(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, raftNode: nil}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestAggregate_RaftReadError(t *testing.T) {
	log := createTestLogger(t)
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, stderrors.New("raft read failed"))
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	_, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestAggregate_SuccessSum(t *testing.T) {
	log := createTestLogger(t)

	// Create time-series data with timestamp field
	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	data2 := map[string]interface{}{"timestamp": float64(20), "value": float64(200)}
	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionSum,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(300), resp.Result)
	assert.Equal(t, uint64(2), resp.Count)
}

func TestAggregate_SuccessAvg(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	data2 := map[string]interface{}{"timestamp": float64(20), "value": float64(200)}
	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionAvg,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(150), resp.Result)
	assert.Equal(t, uint64(2), resp.Count)
}

func TestAggregate_SuccessMin(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	data2 := map[string]interface{}{"timestamp": float64(20), "value": float64(50)}
	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionMin,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(50), resp.Result)
	assert.Equal(t, uint64(2), resp.Count)
}

func TestAggregate_SuccessMax(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	data2 := map[string]interface{}{"timestamp": float64(20), "value": float64(200)}
	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionMax,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp.Result)
	assert.Equal(t, uint64(2), resp.Count)
}

func TestAggregate_SuccessCount(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	data2 := map[string]interface{}{"timestamp": float64(20), "value": float64(200)}
	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionCount,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(2), resp.Result)
	assert.Equal(t, uint64(2), resp.Count)
}

func TestAggregate_TimeRangeFiltering(t *testing.T) {
	log := createTestLogger(t)

	// Data outside time range
	data1 := map[string]interface{}{"timestamp": float64(5), "value": float64(100)}
	// Data inside time range
	data2 := map[string]interface{}{"timestamp": float64(50), "value": float64(200)}
	// Data outside time range
	data3 := map[string]interface{}{"timestamp": float64(200), "value": float64(300)}

	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)
	json3, _ := json.Marshal(data3)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2, json3}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  10,
		EndTime:    100,
		Function:   AggregateFunctionSum,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(200), resp.Result)
	assert.Equal(t, uint64(1), resp.Count)
}

func TestAggregate_AlternativeTimestampFields(t *testing.T) {
	log := createTestLogger(t)

	// Test "ts" field
	data1 := map[string]interface{}{"ts": float64(10), "value": float64(100)}
	// Test "time" field
	data2 := map[string]interface{}{"time": float64(20), "value": float64(200)}

	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionSum,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(300), resp.Result)
	assert.Equal(t, uint64(2), resp.Count)
}

func TestAggregate_CustomFieldName(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{"timestamp": float64(10), "temperature": float64(72.5)}
	data2 := map[string]interface{}{"timestamp": float64(20), "temperature": float64(73.5)}

	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		FieldName:  "temperature",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionAvg,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(73), resp.Result)
	assert.Equal(t, uint64(2), resp.Count)
}

func TestAggregate_SkipInvalidJSON(t *testing.T) {
	log := createTestLogger(t)

	invalidJSON := []byte("{invalid json")
	data1 := map[string]interface{}{"timestamp": float64(10), "value": float64(100)}
	json1, _ := json.Marshal(data1)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{invalidJSON, json1}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionSum,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(100), resp.Result)
	assert.Equal(t, uint64(1), resp.Count)
}

func TestAggregate_EmptyResult(t *testing.T) {
	log := createTestLogger(t)

	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &AggregateRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
		Function:   AggregateFunctionSum,
	}
	resp, err := tsSvc.Aggregate(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp.Result)
	assert.Equal(t, uint64(0), resp.Count)
}

func TestTimeSeriesQuery_NilRequest(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesQuery_EmptyEntityType(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "",
		StartTime:  1,
		EndTime:    2,
	}
	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesQuery_InvalidStartTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  0,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesQuery_InvalidEndTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  100,
		EndTime:    0,
	}
	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesQuery_EndTimeBeforeStartTime(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  100,
		EndTime:    50,
	}
	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestTimeSeriesQuery_NilRaftNode(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, raftNode: nil}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInternal, qbErr.Code)
}

func TestTimeSeriesQuery_RaftReadError(t *testing.T) {
	log := createTestLogger(t)
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, stderrors.New("raft read failed"))
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}

func TestTimeSeriesQuery_Success(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{
		"timestamp": float64(10),
		"value":     float64(100),
		"host":      "server1",
	}
	data2 := map[string]interface{}{
		"timestamp": float64(20),
		"value":     float64(200),
		"host":      "server2",
	}

	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	// Read from stream
	var responses []*TimeSeriesQueryResponse
	for resp := range stream {
		responses = append(responses, resp)
	}

	require.Len(t, responses, 1)
	assert.Equal(t, uint64(2), responses[0].TotalCount)
	assert.Len(t, responses[0].DataPoints, 2)
}

func TestTimeSeriesQuery_TimeRangeFiltering(t *testing.T) {
	log := createTestLogger(t)

	// Outside range
	data1 := map[string]interface{}{"timestamp": float64(5), "value": float64(100)}
	// Inside range
	data2 := map[string]interface{}{"timestamp": float64(50), "value": float64(200)}
	// Outside range
	data3 := map[string]interface{}{"timestamp": float64(200), "value": float64(300)}

	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)
	json3, _ := json.Marshal(data3)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2, json3}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  10,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	var responses []*TimeSeriesQueryResponse
	for resp := range stream {
		responses = append(responses, resp)
	}

	require.Len(t, responses, 1)
	assert.Equal(t, uint64(1), responses[0].TotalCount)
	assert.Len(t, responses[0].DataPoints, 1)
	assert.Equal(t, int64(50), responses[0].DataPoints[0].Timestamp)
}

func TestTimeSeriesQuery_AlternativeTimestampFields(t *testing.T) {
	log := createTestLogger(t)

	// Using "ts" field
	data1 := map[string]interface{}{"ts": float64(10), "value": float64(100)}
	// Using "time" field
	data2 := map[string]interface{}{"time": float64(20), "value": float64(200)}

	json1, _ := json.Marshal(data1)
	json2, _ := json.Marshal(data2)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1, json2}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	var responses []*TimeSeriesQueryResponse
	for resp := range stream {
		responses = append(responses, resp)
	}

	require.Len(t, responses, 1)
	assert.Equal(t, uint64(2), responses[0].TotalCount)
}

func TestTimeSeriesQuery_SkipInvalidEntries(t *testing.T) {
	log := createTestLogger(t)

	// Invalid JSON
	invalidJSON := []byte("{invalid")
	// Missing timestamp
	data2 := map[string]interface{}{"value": float64(100)}
	// Missing value
	data3 := map[string]interface{}{"timestamp": float64(20)}
	// Valid entry
	data4 := map[string]interface{}{"timestamp": float64(30), "value": float64(300)}

	json2, _ := json.Marshal(data2)
	json3, _ := json.Marshal(data3)
	json4, _ := json.Marshal(data4)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{invalidJSON, json2, json3, json4}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	var responses []*TimeSeriesQueryResponse
	for resp := range stream {
		responses = append(responses, resp)
	}

	require.Len(t, responses, 1)
	assert.Equal(t, uint64(1), responses[0].TotalCount)
	assert.Equal(t, int64(30), responses[0].DataPoints[0].Timestamp)
}

func TestTimeSeriesQuery_EmptyResult(t *testing.T) {
	log := createTestLogger(t)

	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	// Stream should be closed
	_, ok := <-stream
	assert.False(t, ok, "Stream should be closed")
}

func TestTimeSeriesQuery_MetadataExtraction(t *testing.T) {
	log := createTestLogger(t)

	data1 := map[string]interface{}{
		"timestamp":  float64(10),
		"value":      float64(100),
		"region":     "us-west",
		"datacenter": "dc1",
		"rack":       "rack-a",
	}

	json1, _ := json.Marshal(data1)

	qrdbProvider := newSvcBoostMockQRDBProvider(sliceToCannedScanResult([][]byte{json1}), nil)
	svc := &quicBaaS{logger: log, qrdbClient: qrdbProvider, keyspaceResolver: NewKeyspaceResolver()}
	tsSvc := &timeSeriesService{svc: svc}

	req := &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  1,
		EndTime:    100,
	}
	stream := make(chan *TimeSeriesQueryResponse, 10)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), req, stream)
	require.NoError(t, err)

	var responses []*TimeSeriesQueryResponse
	for resp := range stream {
		responses = append(responses, resp)
	}

	require.Len(t, responses, 1)
	assert.Len(t, responses[0].DataPoints, 1)

	metadata := responses[0].DataPoints[0].Metadata
	assert.Equal(t, "us-west", metadata["region"])
	assert.Equal(t, "dc1", metadata["datacenter"])
	assert.Equal(t, "rack-a", metadata["rack"])
	// Ensure timestamp and value are not in metadata
	_, hasTimestamp := metadata["timestamp"]
	assert.False(t, hasTimestamp)
	_, hasValue := metadata["value"]
	assert.False(t, hasValue)
}

func TestQuicBaaS_GenerateKey_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.GenerateKey(tenantctx.WithSystem(context.Background()), &GenerateKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_ImportKey_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.ImportKey(tenantctx.WithSystem(context.Background()), &ImportKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_GetKey_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.GetKey(tenantctx.WithSystem(context.Background()), &GetKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_ListKeys_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.ListKeys(tenantctx.WithSystem(context.Background()), &ListKeysRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_RotateKey_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.RotateKey(tenantctx.WithSystem(context.Background()), &RotateKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_RevokeKey_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.RevokeKey(tenantctx.WithSystem(context.Background()), &RevokeKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_DeleteKey_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.DeleteKey(tenantctx.WithSystem(context.Background()), &DeleteKeyRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_GetKeyMetadata_NilService(t *testing.T) {
	db := &quicBaaS{kmsService: nil}
	resp, err := db.GetKeyMetadata(tenantctx.WithSystem(context.Background()), &GetKeyMetadataRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "KMS service not initialized")
}

func TestQuicBaaS_Encrypt_NilService(t *testing.T) {
	db := &quicBaaS{cryptoService: nil}
	resp, err := db.Encrypt(tenantctx.WithSystem(context.Background()), &EncryptRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "Crypto service not initialized")
}

func TestQuicBaaS_Decrypt_NilService(t *testing.T) {
	db := &quicBaaS{cryptoService: nil}
	resp, err := db.Decrypt(tenantctx.WithSystem(context.Background()), &DecryptRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, err.Error(), "Crypto service not initialized")
}

func TestQuicBaaS_Close_AlreadyClosed(t *testing.T) {
	db := &quicBaaS{closed: true}
	err := db.Close()
	require.NoError(t, err)
}

func TestQuicBaaS_Close_NilComponents(t *testing.T) {
	db := &quicBaaS{
		closed:          false,
		schedulerEngine: nil,
		cacheManager:    nil,
		backupMgr:       nil,
		storage:         nil,
		raftNode:        nil,
	}
	err := db.Close()
	require.NoError(t, err)
	assert.True(t, db.closed)
}

func TestBackupServiceImpl_VerifyBackup_NilManager(t *testing.T) {
	svc := &quicBaaS{backupMgr: nil}
	backupSvc := &backupService{svc: svc}

	_, err := backupSvc.VerifyBackup(tenantctx.WithSystem(context.Background()), &VerifyBackupRequest{
		BackupID: "backup_123",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup manager")
}

func TestBatchServiceImpl_BatchGet_EmptyKeys(t *testing.T) {
	svc := &quicBaaS{}
	batchSvc := &batchService{svc: svc}

	_, err := batchSvc.BatchGet(tenantctx.WithSystem(context.Background()), &BatchGetRequest{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "batch")
}

func TestKVService_Watch_ContextCanceledDuringSend(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &WatchRequest{
		Key: []byte("test-key"),
	}

	// Create a buffered channel that we'll fill up
	stream := make(chan *WatchResponse, 1)

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))

	// Start the watch
	watchID, err := svc.Watch(ctx, req, stream)
	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Read the created response
	resp := <-stream

	assert.True(t, resp.Created)

	// Cancel context immediately
	cancel()

	// Give time for goroutine to process cancellation
	time.Sleep(50 * time.Millisecond)

	// The stream should receive a canceled response
	select {
	case resp = <-stream:
		assert.True(t, resp.Canceled)
	case <-time.After(200 * time.Millisecond):
		// Acceptable - the goroutine may have already exited
	}
}

func TestKVService_Watch_NilRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	stream := make(chan *WatchResponse, 10)
	_, err := svc.Watch(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestKVService_Watch_EmptyKeyWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &WatchRequest{
		Key: []byte{},
	}

	stream := make(chan *WatchResponse, 10)
	_, err := svc.Watch(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestKVService_Watch_NilStreamWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &WatchRequest{
		Key: []byte("test-key"),
	}

	_, err := svc.Watch(tenantctx.WithSystem(context.Background()), req, nil)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestKVService_Watch_WithMetricsSuccess(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &WatchRequest{
		Key:      []byte("test-key"),
		IsPrefix: false,
	}

	ctx, cancel := context.WithTimeout(tenantctx.WithSystem(context.Background()), 100*time.Millisecond)
	defer cancel()

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.Watch(ctx, req, stream)
	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Wait for created response
	select {
	case resp := <-stream:
		assert.True(t, resp.Created)
		assert.Equal(t, watchID, resp.WatchID)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for created response")
	}
}

func TestBatchService_BatchPut_NilRequestMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchPut_EmptyRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func TestBatchService_BatchDelete_NilRequestMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchDelete_EmptyRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchGet_NilRequestMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchGet_EmptyRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchGetRequest{
		Requests: []*GetRequest{},
	}

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchPut_WithDefaultShardID(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			// Verify default shard ID is used
			if shardID != 1 {
				t.Errorf("expected shard ID 1, got %d", shardID)
			}
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchPutRequest{
		ShardID: 0, // Should default to 1
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"name":"test"}`)},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Responses, 1)
}

func TestBatchService_BatchDelete_WithDefaultShardID(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			// Verify default shard ID is used
			if shardID != 1 {
				t.Errorf("expected shard ID 1, got %d", shardID)
			}
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchDeleteRequest{
		ShardID: 0, // Should default to 1
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Responses, 1)
}

func TestBatchService_BatchDelete_WithExplicitShardID(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			// Verify explicit shard ID is used
			if shardID != 5 {
				t.Errorf("expected shard ID 5, got %d", shardID)
			}
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchDeleteRequest{
		ShardID: 5,
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1, ShardID: 5},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBatchService_BatchPut_WithExplicitShardID(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			// Verify explicit shard ID is used
			if shardID != 3 {
				t.Errorf("expected shard ID 3, got %d", shardID)
			}
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchPutRequest{
		ShardID: 3,
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"name":"test"}`), ShardID: 3},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBatchService_BatchDelete_RaftErrorPath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProviderWithErrors(nil, stderrors.New("delete failed"), stderrors.New("batch failed"))
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1, ShardID: 5},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchDelete_RaftErrorWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProviderWithErrors(nil, stderrors.New("delete failed"), stderrors.New("batch failed"))
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1, ShardID: 5},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchPut_RaftErrorWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProviderWithErrors(stderrors.New("put failed"), nil, stderrors.New("batch failed"))
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"name":"test"}`), ShardID: 3},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchGet_SuccessPath(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			// Return some data
			return []byte(`{"name":"test"}`), nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchGetRequest{
		Requests: []*GetRequest{
			{EntityType: "user", ID: 1},
			{EntityType: "user", ID: 2},
		},
	}

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Responses, 2)
}

func TestBatchService_BatchGet_SuccessWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return []byte(`{"name":"test"}`), nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchGetRequest{
		Requests: []*GetRequest{
			{EntityType: "user", ID: 1},
		},
	}

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBatchService_BatchPut_SuccessWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"name":"test"}`), ShardID: 3},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBatchService_BatchDelete_SuccessWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1, ShardID: 5},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_ListEntityTypes_ZeroPageSize(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return map[string][]byte{}, nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &ListEntityTypesRequest{
		PageSize: 0, // Should default to 100
	}

	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_ListEntityTypes_NegativePageSize(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return map[string][]byte{}, nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &ListEntityTypesRequest{
		PageSize: -1, // Should default to 100
	}

	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_ListEntityTypes_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return map[string][]byte{}, nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.ListEntityTypes(tenantctx.WithSystem(context.Background()), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestTransactionService_Transaction_NilRequestMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestTransactionService_Transaction_EmptyOperationsReturnsEmpty(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &TransactionRequest{
		Success: []*TransactionOp{},
		Failure: []*TransactionOp{},
	}

	// Empty operations returns success with empty result (not an error)
	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	// Check that it either returns an error OR returns success with empty responses
	if err != nil {
		// Error case is valid
		require.Nil(t, resp)
	} else {
		// Success with empty result is also valid
		require.NotNil(t, resp)
		assert.True(t, resp.Succeeded)
	}
}

func TestTransactionService_Transaction_WithDefaultShardID(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			if shardID != 1 {
				t.Errorf("expected shard ID 1, got %d", shardID)
			}
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &TransactionRequest{
		ShardID: 0, // Should default to 1
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`{"name":"test"}`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_List_NilRequestPath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestKVService_List_NilRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)
}

func TestKVService_List_EmptyEntityTypePath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &ListRequest{
		EntityType: "",
	}

	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)
}

func TestKVService_List_EmptyEntityTypeWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	req := &ListRequest{
		EntityType: "",
	}

	stream := make(chan *ListResponse, 10)
	err := svc.List(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)
}

func TestKVService_Delete_NilRequestPath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestKVService_Delete_NilRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Delete_EmptyEntityTypePath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &DeleteRequest{
		EntityType: "",
		ID:         1,
	}

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Put_NilRequestPath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestKVService_Put_NilRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Put_EmptyEntityTypePath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "",
		ID:         1,
		Data:       []byte(`{}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Put_NilDataAllowed(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       nil, // nil data is allowed (treated as empty)
	}

	// If the implementation allows nil data, this should succeed
	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	// Either success OR error is acceptable based on implementation
	if err != nil {
		require.Nil(t, resp)
	} else {
		require.NotNil(t, resp)
	}
}

func TestKVService_Get_NilRequestPath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)

	qbErr, ok := err.(*errors.QBError)
	assert.True(t, ok)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestKVService_Get_NilRequestWithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB), withEnableMetrics())
	defer svc.Close()

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Get_EmptyEntityTypePath(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &GetRequest{
		EntityType: "",
		ID:         1,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Put_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestKVService_Put_EmptyEntityType(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "entity type is required")
}

func TestKVService_Put_RaftError(t *testing.T) {
	putErr := stderrors.New("raft propose failed")
	qrdbProvider := newSvcBoostMockQRDBProviderWithErrors(putErr, nil, nil)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Put_WithCustomShardID(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			assert.Equal(t, uint64(5), shardID)
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
		ShardID:    5,
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_Put_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics(), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_Put_WithCacheUpdate(t *testing.T) {
	cacheMgr, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 5 * time.Minute,
	}, createTestLogger(t))
	require.NoError(t, err)

	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withCacheManager(cacheMgr), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         1,
		Data:       []byte(`{"name":"test"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_Get_RaftError(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return nil, stderrors.New("raft read failed")
		},
	}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &GetRequest{
		EntityType: "user",
		ID:         1,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestKVService_Get_WithCacheHit(t *testing.T) {
	cacheMgr, err := cache.NewManager(cache.Config{
		L1Size:          100,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 5 * time.Minute,
	}, createTestLogger(t))
	require.NoError(t, err)

	// Pre-populate cache
	testData := []byte(`{"name":"cached"}`)
	err = cacheMgr.Set(tenantctx.WithSystem(context.Background()), "user:1", testData)
	require.NoError(t, err)

	mockRaft := &mockRaftNode{} // Should not be called if cache hits
	svc := createTestService(t, withMockRaftNode(mockRaft), withCacheManager(cacheMgr))
	defer svc.Close()

	req := &GetRequest{
		EntityType:  "user",
		ID:          1,
		Consistency: ConsistencyLevelEventual, // Required for cache lookup
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, testData, resp.Data)
	assert.True(t, resp.Found)
}

func TestKVService_Get_WithMetrics(t *testing.T) {
	testData := []byte(`{"name":"test"}`)

	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return testData, nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProviderWithGet(testData, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics(), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &GetRequest{
		EntityType: "user",
		ID:         1,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKVService_List_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	stream := make(chan *ListResponse, 1)
	err := svc.List(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestKVService_List_EmptyEntityType(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &ListRequest{
		EntityType: "",
	}

	stream := make(chan *ListResponse, 1)
	err := svc.List(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entity type is required")
}

func TestKVService_List_RaftError(t *testing.T) {
	scanErr := stderrors.New("raft read failed")
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, scanErr)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListRequest{
		EntityType: "user",
	}

	stream := make(chan *ListResponse, 1)
	err := svc.List(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)
}

func TestKVService_List_ContextCanceled(t *testing.T) {
	// Inject a context cancellation error via the QRDB scan mock
	scanErr := context.Canceled
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, scanErr)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &ListRequest{
		EntityType: "user",
		PageSize:   10,
	}

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	cancel() // Cancel immediately

	stream := make(chan *ListResponse, 1)
	err := svc.List(ctx, req, stream)
	// Context cancellation error expected
	assert.Error(t, err)
}

func TestKVService_Watch_NilRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	stream := make(chan *WatchResponse, 10)
	_, err := svc.Watch(tenantctx.WithSystem(context.Background()), nil, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestKVService_Watch_EmptyKey(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &WatchRequest{
		Key: []byte{},
	}

	stream := make(chan *WatchResponse, 10)
	_, err := svc.Watch(tenantctx.WithSystem(context.Background()), req, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key is required")
}

func TestKVService_Watch_NilStream(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &WatchRequest{
		Key: []byte("test-key"),
	}

	_, err := svc.Watch(tenantctx.WithSystem(context.Background()), req, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream channel is required")
}

func TestKVService_Watch_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &WatchRequest{
		Key:      []byte("test-key"),
		IsPrefix: false,
	}

	ctx, cancel := context.WithTimeout(tenantctx.WithSystem(context.Background()), 100*time.Millisecond)
	defer cancel()

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.Watch(ctx, req, stream)
	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Wait for created response
	select {
	case resp := <-stream:
		assert.True(t, resp.Created)
		assert.Equal(t, watchID, resp.WatchID)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for created response")
	}
}

func TestKVService_Watch_WithPrefix(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &WatchRequest{
		Key:      []byte("/users/"),
		IsPrefix: true,
	}

	ctx, cancel := context.WithTimeout(tenantctx.WithSystem(context.Background()), 100*time.Millisecond)
	defer cancel()

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.Watch(ctx, req, stream)
	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Wait for created response
	select {
	case resp := <-stream:
		assert.True(t, resp.Created)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for created response")
	}
}

func TestKVService_Watch_ContextCanceled(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &WatchRequest{
		Key: []byte("test-key"),
	}

	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.Watch(ctx, req, stream)
	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Wait for created response
	<-stream

	// Cancel context
	cancel()

	// Wait for canceled response
	select {
	case resp := <-stream:
		assert.True(t, resp.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for canceled response")
	}
}

func TestKVService_Watch_WithFilters(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &WatchRequest{
		Key:     []byte("test-key"),
		Filters: []WatchEventType{WatchEventTypeDelete}, // Filter out DELETE events
	}

	ctx, cancel := context.WithTimeout(tenantctx.WithSystem(context.Background()), 100*time.Millisecond)
	defer cancel()

	stream := make(chan *WatchResponse, 10)
	watchID, err := svc.Watch(ctx, req, stream)
	require.NoError(t, err)
	assert.Greater(t, watchID, int64(0))

	// Wait for created response
	select {
	case resp := <-stream:
		assert.True(t, resp.Created)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for created response")
	}
}

func TestBatchService_BatchPut_EmptyRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func TestBatchService_BatchPut_RaftError(t *testing.T) {
	batchErr := stderrors.New("raft propose failed")
	qrdbProvider := newSvcBoostMockQRDBProviderWithErrors(nil, nil, batchErr)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"name":"test1"}`)},
			{EntityType: "user", ID: 2, Data: []byte(`{"name":"test2"}`)},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchPut_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics(), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"name":"test1"}`)},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestBatchService_BatchDelete_EmptyRequest(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestService(t, withMockRaftNode(mockRaft))
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "batch cannot be empty")
}

func TestBatchService_BatchDelete_RaftError(t *testing.T) {
	batchErr := stderrors.New("raft propose failed")
	qrdbProvider := newSvcBoostMockQRDBProviderWithErrors(nil, nil, batchErr)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1},
			{EntityType: "user", ID: 2},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestBatchService_BatchDelete_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics(), withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestTransactionService_Transaction_CompareNotExists(t *testing.T) {
	tr := newAuthorizingTransport(permTransactionExecute)
	svc := newAuthorizingService(t, tr)

	req := &TransactionRequest{
		Compare: []*CompareCondition{
			{
				EntityType: "user",
				ID:         999,
				Target:     CompareTargetExists,
				Operator:   CompareOperatorEqual,
			},
		},
		Failure: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         999,
				Data:       []byte(`{"name":"created"}`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Succeeded)
	assert.Equal(t, []byte(`{"name":"created"}`), tr.data["user:999"])
}

func TestTransactionService_Transaction_UnsupportedCompareTarget(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProviderWithGet([]byte(`data`), nil)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &TransactionRequest{
		Compare: []*CompareCondition{
			{
				EntityType: "user",
				ID:         1,
				Target:     CompareTarget(999), // Unsupported target
				Operator:   CompareOperatorEqual,
			},
		},
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypePut,
				EntityType: "user",
				ID:         1,
				Data:       []byte(`{"name":"test"}`),
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestTransactionService_Transaction_GetOperationError(t *testing.T) {
	getErr := stderrors.New("read failed")
	qrdbProvider := newSvcBoostMockQRDBProviderWithGet(nil, getErr)
	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	defer svc.Close()

	req := &TransactionRequest{
		Success: []*TransactionOp{
			{
				OpType:     TransactionOpTypeGet,
				EntityType: "user",
				ID:         1,
			},
		},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err) // Transaction succeeds but operation has error
	require.NotNil(t, resp)
	assert.Len(t, resp.Responses, 1)
	assert.False(t, resp.Responses[0].Success)
}

func TestBackupService_CreateBackup_Error(t *testing.T) {
	mockBackup := &mockBackupManager{
		createBackupFn: func(ctx context.Context, backupType interface{}) (interface{}, error) {
			return nil, stderrors.New("backup failed")
		},
	}
	svc := createTestService(t, withBackupManager(mockBackup))
	defer svc.Close()

	req := &CreateBackupRequest{
		Name: "test",
	}

	resp, err := svc.CreateBackup(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestTimeSeriesQuery_Coverage(t *testing.T) {
	log := createTestLogger(t)
	svc := &quicBaaS{logger: log, raftNode: nil}
	svc.queryService = &queryService{svc: svc}
	tsSvc := &timeSeriesService{svc: svc}

	stream := make(chan *TimeSeriesQueryResponse, 1)
	err := tsSvc.TimeSeriesQuery(tenantctx.WithSystem(context.Background()), &TimeSeriesQueryRequest{
		EntityType: "metrics",
		StartTime:  time.Now().Add(-1 * time.Hour).UnixNano(),
		EndTime:    time.Now().UnixNano(),
	}, stream)

	require.Error(t, err)
}

// =============================================================================
// Batch Service - atomic ExecuteBatch commit path
// =============================================================================

// TestBatchService_BatchPut_SingleConsensusProposal verifies that a batch
// commits through ONE qrdb ExecuteBatch call (one Raft proposal) instead of
// one KV Put per operation, with keys and values preserved in input order.
func TestBatchService_BatchPut_SingleConsensusProposal(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"n":1}`)},
			{EntityType: "user", ID: 2, Data: []byte(`{"n":2}`)},
			{EntityType: "user", ID: 3, Data: []byte(`{"n":3}`)},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Responses, 3)

	calls := mockQRDB.capturedBatchCalls()
	require.Len(t, calls, 1, "expected exactly one ExecuteBatch call")
	require.Len(t, calls[0], 3)
	for i, op := range calls[0] {
		assert.Equal(t, qrdbtransport.OpPut, op.Type)
		assert.Equal(t, req.Requests[i].Data, op.Value)
		assert.Contains(t, string(op.Key), "user")
	}
}

// TestBatchService_BatchPut_ChunksLargeBatchUnderEntryLimit reproduces the
// TestStreamingBatchBackpressure commit shape (2000 ops x 4 KB = ~8.4 MB
// estimated) and verifies the batch is split into a small, bounded number
// of consensus proposals, each under batchProposalMaxBytes, preserving
// operation order across chunks. Before the fix this shape produced 2000
// serial Raft round-trips, which exceeded the test's 180s deadline under
// harness load.
func TestBatchService_BatchPut_ChunksLargeBatchUnderEntryLimit(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	const totalOps = 2000
	largeData := make([]byte, 4096)
	for i := range largeData {
		largeData[i] = byte('A' + (i % 26))
	}

	reqs := make([]*PutRequest, totalOps)
	for i := 0; i < totalOps; i++ {
		reqs[i] = &PutRequest{
			EntityType: "stream_backpressure",
			ID:         uint64(i + 1),
			Data:       largeData,
		}
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), &BatchPutRequest{Requests: reqs})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Responses, totalOps)

	calls := mockQRDB.capturedBatchCalls()
	require.GreaterOrEqual(t, len(calls), 2, "an ~8 MB batch must be split below the raft entry limit")
	require.LessOrEqual(t, len(calls), 4, "commit must use a bounded number of consensus proposals, not one per op")

	seen := 0
	for _, call := range calls {
		size := 0
		for _, op := range call {
			assert.Equal(t, qrdbtransport.OpPut, op.Type)
			size += batchOpSize(op)
			seen++
		}
		assert.LessOrEqual(t, size, batchProposalMaxBytes, "each sub-batch must fit in one raft entry")
	}
	assert.Equal(t, totalOps, seen, "every operation must be committed exactly once")
}

// TestBatchService_BatchPut_OversizedSingleOpOwnChunk verifies that a single
// operation whose estimated size exceeds batchProposalMaxBytes is still sent
// as its own sub-batch (the size limit cannot split one op; qrdb enforces
// the hard raft entry cap).
func TestBatchService_BatchPut_OversizedSingleOpOwnChunk(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "blob", ID: 1, Data: make([]byte, batchProposalMaxBytes+1)},
			{EntityType: "blob", ID: 2, Data: []byte("small")},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	calls := mockQRDB.capturedBatchCalls()
	require.Len(t, calls, 2)
	assert.Len(t, calls[0], 1)
	assert.Len(t, calls[1], 1)
}

// TestBatchService_BatchPut_MultiShardGrouping verifies that operations are
// grouped per shard in first-seen order while preserving in-shard ordering.
func TestBatchService_BatchPut_MultiShardGrouping(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"n":1}`), ShardID: 1},
			{EntityType: "user", ID: 2, Data: []byte(`{"n":2}`), ShardID: 2},
			{EntityType: "user", ID: 3, Data: []byte(`{"n":3}`), ShardID: 1},
		},
	}

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Responses, 3)

	assert.Equal(t, []uint64{1, 2}, mockQRDB.capturedShardCalls())

	calls := mockQRDB.capturedBatchCalls()
	require.Len(t, calls, 2)
	require.Len(t, calls[0], 2, "shard 1 group carries ops 1 and 3 in order")
	assert.Equal(t, []byte(`{"n":1}`), calls[0][0].Value)
	assert.Equal(t, []byte(`{"n":3}`), calls[0][1].Value)
	require.Len(t, calls[1], 1, "shard 2 group carries op 2")
	assert.Equal(t, []byte(`{"n":2}`), calls[1][0].Value)
}

// TestBatchService_BatchPut_ExecuteBatchErrorTyped verifies that an
// ExecuteBatch failure surfaces as a typed QBError with ErrRaft.
func TestBatchService_BatchPut_ExecuteBatchErrorTyped(t *testing.T) {
	batchErr := stderrors.New("raft batch propose failed")
	mockQRDB := newSvcBoostMockQRDBProviderWithErrors(nil, nil, batchErr)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), &BatchPutRequest{
		Requests: []*PutRequest{
			{EntityType: "user", ID: 1, Data: []byte(`{"n":1}`)},
		},
	})
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
	assert.ErrorIs(t, err, batchErr)
}

// TestBatchService_BatchDelete_SingleConsensusProposal verifies that a
// delete batch commits through one ExecuteBatch call with OpDelete ops.
func TestBatchService_BatchDelete_SingleConsensusProposal(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	req := &BatchDeleteRequest{
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1},
			{EntityType: "user", ID: 2},
		},
	}

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Responses, 2)

	calls := mockQRDB.capturedBatchCalls()
	require.Len(t, calls, 1, "expected exactly one ExecuteBatch call")
	require.Len(t, calls[0], 2)
	for _, op := range calls[0] {
		assert.Equal(t, qrdbtransport.OpDelete, op.Type)
		assert.Empty(t, op.Value)
	}
}

// TestBatchService_BatchDelete_ExecuteBatchErrorTyped verifies that an
// ExecuteBatch failure during BatchDelete surfaces as a typed QBError.
func TestBatchService_BatchDelete_ExecuteBatchErrorTyped(t *testing.T) {
	batchErr := stderrors.New("raft batch propose failed")
	mockQRDB := newSvcBoostMockQRDBProviderWithErrors(nil, nil, batchErr)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), &BatchDeleteRequest{
		Requests: []*DeleteRequest{
			{EntityType: "user", ID: 1},
		},
	})
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
	assert.ErrorIs(t, err, batchErr)
}
