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
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// daoFactory.GetKVStore Tests
// =============================================================================

// testKVStore implements interfaces.KVStore for testing.
type testKVStore struct {
	id int // Unique identifier to distinguish instances
}

func (m *testKVStore) Get(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (m *testKVStore) Put(_ context.Context, _ string, _ []byte) error { return nil }
func (m *testKVStore) Delete(_ context.Context, _ string) error        { return nil }
func (m *testKVStore) Scan(_ context.Context, _ string, _ func(string, []byte) error) error {
	return nil
}
func (m *testKVStore) List(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (m *testKVStore) Exists(_ context.Context, _ string) (bool, error)   { return false, nil }

func TestDAOFactory_GetKVStore_NilProvider(t *testing.T) {
	factory := &daoFactory{kvStoreProvider: nil}
	result := factory.GetKVStore(1)
	assert.Nil(t, result)
}

func TestDAOFactory_GetKVStore_ReturnsStore(t *testing.T) {
	store := &testKVStore{id: 1}
	factory := &daoFactory{
		kvStoreProvider: func(shardID uint64) interfaces.KVStore {
			return store
		},
	}

	result := factory.GetKVStore(1)
	assert.Same(t, store, result)
}

func TestDAOFactory_GetKVStore_DifferentShards(t *testing.T) {
	store1 := &testKVStore{id: 1}
	store2 := &testKVStore{id: 2}
	factory := &daoFactory{
		kvStoreProvider: func(shardID uint64) interfaces.KVStore {
			if shardID == 1 {
				return store1
			}
			return store2
		},
	}

	result1 := factory.GetKVStore(1)
	result2 := factory.GetKVStore(2)
	assert.Same(t, store1, result1)
	assert.Same(t, store2, result2)

	// Verify routing to different shards returns distinct stores
	kvs1 := result1.(*testKVStore)
	kvs2 := result2.(*testKVStore)
	assert.NotEqual(t, kvs1.id, kvs2.id)
}

func TestDAOFactory_GetKVStore_ProviderReturnsNil(t *testing.T) {
	factory := &daoFactory{
		kvStoreProvider: func(shardID uint64) interfaces.KVStore {
			return nil
		},
	}

	result := factory.GetKVStore(1)
	assert.Nil(t, result)
}
