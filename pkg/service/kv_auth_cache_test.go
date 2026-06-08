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
	"google.golang.org/grpc/metadata"
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

	resp, err := svc.Put(context.Background(), req)
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

	resp, err := svc.Put(context.Background(), req)
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

	resp, err := svc.Put(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Get - Auth Check Rejects Unauthorized
// =============================================================================

func TestKVService_Get_AuthEnabled_Unauthorized(t *testing.T) {
	mockRaft := &mockRaftNode{}

	mockAuth := &mockAuthManager{
		authorizeFn: func(ctx context.Context, token, resource, permission string) error {
			return stderrors.New("unauthorized")
		},
	}

	svc := createTestService(t, withMockRaftNode(mockRaft), withAuthManager(mockAuth))
	db := svc.(*quicBaaS)
	db.config.EnableAuth = true

	md := metadata.New(map[string]string{
		"authorization": "Bearer invalid-token",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)

	req := &GetRequest{
		EntityType: "user",
		ID:         1,
	}

	resp, err := svc.Get(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

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
	key, err := db.keyspaceResolver.BuildKey(context.Background(), "user", 1)
	require.NoError(t, err)

	// Pre-populate the cache
	cacheMock.data[string(key)] = []byte(`{"name":"cached"}`)

	req := &GetRequest{
		EntityType:  "user",
		ID:          1,
		Consistency: ConsistencyLevelEventual,
	}

	resp, err := db.kvService.Get(context.Background(), req)
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

	resp, err := svc.Get(context.Background(), req)
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

	resp, err := svc.Get(context.Background(), req)
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

	resp, err := svc.Delete(context.Background(), req)
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

	resp, err := svc.Put(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(5), qrdbProvider.lastShardID.Load())
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

	resp, err := svc.Put(context.Background(), req)
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

	resp, err := svc.Get(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)
}
