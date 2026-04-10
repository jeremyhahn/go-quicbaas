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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// ServiceBuilder WithXxx Option Tests - Cover 0% functions in service.go
// =============================================================================

func TestServiceBuilder_WithCryptoManager(t *testing.T) {
	builder := NewServiceBuilder()

	// Should accept nil
	result := builder.WithCryptoManager(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.cryptoManager)
}

func TestServiceBuilder_WithKVStoreProvider(t *testing.T) {
	builder := NewServiceBuilder()

	provider := func(shardID uint64) interface{} {
		return nil
	}

	result := builder.WithKVStoreProvider(provider)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.NotNil(t, sb.kvStoreProvider)
}

func TestServiceBuilder_WithKVStoreProvider_NilProvider(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithKVStoreProvider(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.kvStoreProvider)
}

func TestServiceBuilder_WithCAService(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithCAService("mock-provider")
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, "mock-provider", sb.caService)
}

func TestServiceBuilder_WithCAService_Nil(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithCAService(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.caService)
}

func TestServiceBuilder_WithCatalogManager(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithCatalogManager(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.catalogManager)
}

func TestServiceBuilder_WithBackendManager(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithBackendManager(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.backendManager)
}

func TestServiceBuilder_WithObjectStoreConfigResolver(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithObjectStoreConfigResolver(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.objectStoreConfigResolver)
}

func TestServiceBuilder_WithWorkerManager(t *testing.T) {
	builder := NewServiceBuilder()

	var workerMgr *catalog.WorkerManager
	result := builder.WithWorkerManager(workerMgr)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.workerManager)
}

func TestServiceBuilder_WithVectorService(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithVectorService(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.vectorService)
}

func TestServiceBuilder_WithMessagingService(t *testing.T) {
	builder := NewServiceBuilder()

	result := builder.WithMessagingService(nil)
	require.NotNil(t, result)

	sb, ok := result.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.messagingService)
}

// =============================================================================
// Build with KMS Manager and KVStoreProvider integration
// =============================================================================

func TestServiceBuilder_Build_WithKMSManager(t *testing.T) {
	mockKMS := newMockKMSManager()
	builder := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithLogger(createTestLogger(t)).
		WithKMSManager(mockKMS)

	svc, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	// KMS manager should be set
	assert.NotNil(t, svc.KMSManager())
	// KMS service should also be initialized
	assert.NotNil(t, svc.KMS())
}

func TestServiceBuilder_Build_WithInvalidKMSManager(t *testing.T) {
	builder := &serviceBuilder{
		raftNode:   &mockRaftNode{},
		config:     &ServiceConfig{NodeID: 1},
		logger:     createTestLogger(t),
		kmsManager: "invalid-type", // Not interfaces.KMSManager
	}

	svc, err := newQuicBaaS(builder)
	require.Error(t, err)
	assert.Nil(t, svc)
	assert.Contains(t, err.Error(), "KMS manager must be interfaces.KMSManager")
}

func TestServiceBuilder_Build_WithKVStoreProvider(t *testing.T) {
	store := &testKVStore{}
	builder := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithLogger(createTestLogger(t)).
		WithKVStoreProvider(func(shardID uint64) interface{} {
			return store
		})

	svc, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	// DAO factory should be set
	impl := svc.(*quicBaaS)
	dao := impl.DAO()
	require.NotNil(t, dao)

	// Should return the testKVStore
	kvStore := dao.GetKVStore(1)
	assert.Same(t, store, kvStore)
}

func TestServiceBuilder_Build_WithKVStoreProvider_InvalidReturn(t *testing.T) {
	builder := NewServiceBuilder().
		WithRaftNode(&mockRaftNode{}).
		WithLogger(createTestLogger(t)).
		WithKVStoreProvider(func(shardID uint64) interface{} {
			return "not-a-kvstore" // Wrong type
		})

	svc, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	// DAO factory should be set but return nil for invalid type
	impl := svc.(*quicBaaS)
	dao := impl.DAO()
	require.NotNil(t, dao)
	kvStore := dao.GetKVStore(1)
	assert.Nil(t, kvStore)
}

func TestServiceBuilder_WithCryptoManager_ValidManager(t *testing.T) {
	cm := newMockCryptoManager()
	builder := NewServiceBuilder().WithCryptoManager(cm)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)

	// Verify the interface type is stored properly
	var result interfaces.CryptoManager = sb.cryptoManager
	assert.Same(t, cm, result)
}
