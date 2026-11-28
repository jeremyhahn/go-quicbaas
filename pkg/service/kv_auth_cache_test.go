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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// KV Cache Mock
// =============================================================================

type kvAuthCacheMock struct {
	data     map[string][]byte
	getCalls int
	setCalls int
	delCalls int
}

func newKVAuthCacheMock() *kvAuthCacheMock {
	return &kvAuthCacheMock{
		data: make(map[string][]byte),
	}
}

func (m *kvAuthCacheMock) Get(_ context.Context, key string) ([]byte, error) {
	m.getCalls++
	if v, ok := m.data[key]; ok {
		return v, nil
	}
	return nil, stderrors.New("not found")
}
func (m *kvAuthCacheMock) Set(_ context.Context, key string, value []byte) error {
	m.setCalls++
	m.data[key] = value
	return nil
}
func (m *kvAuthCacheMock) Delete(_ context.Context, key string) error {
	m.delCalls++
	delete(m.data, key)
	return nil
}
func (m *kvAuthCacheMock) Clear()       {}
func (m *kvAuthCacheMock) Close() error { return nil }

// =============================================================================
// Put - Auth Check Rejects Unauthorized
// =============================================================================

// =============================================================================
// Put - With Cache (successful write populates cache)
// =============================================================================

func TestKVService_Put_WithCachePopulation(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
		getLeaderIDFn: func(shardID uint64) (uint64, error) {
			return 1, nil
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft))
	db := svc.(*quicBaaS)
	cacheMock := newKVAuthCacheMock()
	db.cacheManager = cacheMock

	req := &PutRequest{
		EntityType: "user",
		ID:         100,
		Data:       []byte(`{"name":"cached-user"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(100), resp.ID)

	// Cache should have been populated
	assert.Equal(t, 1, cacheMock.setCalls)
}

// =============================================================================
// Put - With Metrics Tracking Success Path
// =============================================================================

func TestKVService_Put_MetricsSuccessPath(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
		getLeaderIDFn: func(shardID uint64) (uint64, error) {
			return 1, nil
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics())
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         200,
		Data:       []byte(`{"name":"metrics-user"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(200), resp.ID)
}

// =============================================================================
// Put - Raft Error Path with Metrics
// =============================================================================

func TestKVService_Put_RaftError_WithMetrics(t *testing.T) {
	putErr := stderrors.New("raft unavailable")
	qrdbProvider := newSvcBoostMockQRDBProviderWithErrors(putErr, nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider), withEnableMetrics())
	defer svc.Close()

	req := &PutRequest{
		EntityType: "user",
		ID:         300,
		Data:       []byte(`{"name":"fail-user"}`),
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Get - Auth Check Rejects Unauthorized
// =============================================================================

// =============================================================================
// Get - Cache Hit Path (Eventual Consistency)
// =============================================================================

func TestKVService_Get_CacheHit_EventualConsistency(t *testing.T) {
	mockRaft := &mockRaftNode{}

	svc := createTestService(t, withMockRaftNode(mockRaft))
	db := svc.(*quicBaaS)
	cacheMock := newKVAuthCacheMock()
	db.cacheManager = cacheMock

	// Build the key the same way the Get method would
	key, err := db.keyspaceResolver.BuildKey(tenantctx.WithSystem(context.Background()), "user", 1)
	require.NoError(t, err)

	// Pre-populate the cache
	cacheMock.data[kvCacheKey(1, key)] = []byte(`{"name":"cached"}`)

	req := &GetRequest{
		EntityType:  "user",
		ID:          1,
		Consistency: ConsistencyLevelEventual,
	}

	resp, err := db.kvService.Get(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Found)
	assert.Equal(t, []byte(`{"name":"cached"}`), resp.Data)
	assert.Equal(t, 1, cacheMock.getCalls)
}

// =============================================================================
// Get - With Metrics on Error Path
// =============================================================================

func TestKVService_Get_RaftError_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return nil, stderrors.New("raft read failed")
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics())
	defer svc.Close()

	req := &GetRequest{
		EntityType: "user",
		ID:         500,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Get - Cache Populates on Successful Read
// =============================================================================

func TestKVService_Get_PopulatesCacheOnRead(t *testing.T) {
	returnData := []byte(`{"name":"from-raft"}`)
	qrdbProvider := newSvcBoostMockQRDBProviderWithGet(returnData, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	db := svc.(*quicBaaS)
	cacheMock := newKVAuthCacheMock()
	db.cacheManager = cacheMock

	req := &GetRequest{
		EntityType: "user",
		ID:         600,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Found)
	assert.Equal(t, returnData, resp.Data)

	// Cache should have been populated after read
	assert.Equal(t, 1, cacheMock.setCalls)
}

// =============================================================================
// Delete - Auth Check Rejects Unauthorized
// =============================================================================

// =============================================================================
// Delete - Cache Invalidation
// =============================================================================

func TestKVService_Delete_InvalidatesCache(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft))
	db := svc.(*quicBaaS)
	cacheMock := newKVAuthCacheMock()
	db.cacheManager = cacheMock

	req := &DeleteRequest{
		EntityType: "user",
		ID:         700,
	}

	resp, err := svc.Delete(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)

	// Cache invalidation should have been called
	assert.Equal(t, 1, cacheMock.delCalls)
}

// =============================================================================
// List - Auth Check Rejects Unauthorized
// =============================================================================

// =============================================================================
// Put - With Specific ShardID
// =============================================================================

func TestKVService_Put_SpecificShardID(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))

	req := &PutRequest{
		EntityType: "user",
		ID:         800,
		Data:       []byte(`{"name":"shard-test"}`),
		ShardID:    5,
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(5), qrdbProvider.lastShardID.Load())
}

// TestKVService_Cache_KeepsShardsApart: the same tenant key on two shards is
// two values, so the cache keys on the shard (go-quicbaas#1537, Codex P1).
func TestKVService_Cache_KeepsShardsApart(t *testing.T) {
	qrdbProvider := newSvcBoostMockQRDBProvider(nil, nil)

	svc := createTestService(t, withQRDBClientProvider(qrdbProvider))
	db := svc.(*quicBaaS)
	cacheMock := newKVAuthCacheMock()
	db.cacheManager = cacheMock

	ctx := tenantctx.WithSystem(context.Background())
	for shard, data := range map[uint64]string{2: "two", 3: "three"} {
		_, err := svc.Put(ctx, &PutRequest{
			EntityType: "user",
			ID:         801,
			Data:       []byte(data),
			ShardID:    shard,
		})
		require.NoError(t, err)
	}

	for shard, want := range map[uint64]string{2: "two", 3: "three"} {
		resp, err := svc.Get(ctx, &GetRequest{
			EntityType:  "user",
			ID:          801,
			ShardID:     shard,
			Consistency: ConsistencyLevelEventual,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, want, string(resp.Data), "shard %d", shard)
	}

	_, err := svc.Delete(ctx, &DeleteRequest{EntityType: "user", ID: 801, ShardID: 2})
	require.NoError(t, err)
	assert.Len(t, cacheMock.data, 1, "a delete on shard 2 leaves shard 3's value cached")
}

// =============================================================================
// Put - Consistency Level Tracking with Metrics
// =============================================================================

func TestKVService_Put_ConsistencyTracking_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncProposeFn: func(ctx context.Context, shardID uint64, data []byte) error {
			return nil
		},
		getLeaderIDFn: func(shardID uint64) (uint64, error) {
			return 1, nil
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics())
	defer svc.Close()

	req := &PutRequest{
		EntityType:  "user",
		ID:          900,
		Data:        []byte(`{"name":"consistent"}`),
		Consistency: ConsistencyLevelStrong,
	}

	resp, err := svc.Put(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// Get - With Metrics on NotFound
// =============================================================================

func TestKVService_Get_NotFound_WithMetrics(t *testing.T) {
	mockRaft := &mockRaftNode{
		syncReadFn: func(ctx context.Context, shardID uint64, query interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft), withEnableMetrics())
	defer svc.Close()

	req := &GetRequest{
		EntityType: "user",
		ID:         999,
	}

	resp, err := svc.Get(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}
