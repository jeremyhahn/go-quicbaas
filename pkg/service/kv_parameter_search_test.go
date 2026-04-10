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
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// SearchByTags Deep Path Tests
// =============================================================================

// newSearchByTagsProvider creates a QRDBClientProvider backed by an in-memory
// parameter store for testing SearchByTags functionality.
func newSearchByTagsProvider(params map[string]*storedParameter) *mockParamClientProvider {
	transport := &mockKVTransport{
		getFn: func(_ context.Context, key string) ([]byte, error) {
			// Strip "param:" prefix to look up in params map
			paramKey := key
			if strings.HasPrefix(key, "param:") {
				paramKey = key[6:]
			}
			if param, ok := params[paramKey]; ok {
				data, err := json.Marshal(param)
				if err != nil {
					return nil, err
				}
				return data, nil
			}
			return nil, &qrdbtransport.KeyNotFoundError{Key: key}
		},
		scanFn: func(_ context.Context, prefix string) (map[string][]byte, error) {
			results := make(map[string][]byte)
			for key, param := range params {
				fullKey := "param:" + key
				if prefix == "" || strings.HasPrefix(fullKey, prefix) {
					data, err := json.Marshal(param)
					if err != nil {
						continue
					}
					results[fullKey] = data
				}
			}
			return results, nil
		},
	}
	return newMockProvider(transport)
}

func TestKVParameterManager_SearchByTags_WithMatches(t *testing.T) {
	params := map[string]*storedParameter{
		"/app/db-host": {
			Value: []byte("localhost"),
			Metadata: &interfaces.ParameterMetadata{
				Type:      "string",
				Tags:      map[string]string{"env": "prod", "tier": "db"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		"/app/cache-host": {
			Value: []byte("redis.local"),
			Metadata: &interfaces.ParameterMetadata{
				Type:      "string",
				Tags:      map[string]string{"env": "prod", "tier": "cache"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		"/app/dev-host": {
			Value: []byte("dev.local"),
			Metadata: &interfaces.ParameterMetadata{
				Type:      "string",
				Tags:      map[string]string{"env": "dev", "tier": "app"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	provider := newSearchByTagsProvider(params)
	pm := newTestKVParameterManager(t, provider)

	// Search for all prod parameters
	paths, _, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod"}, "/app/", 100, "")
	require.NoError(t, err)
	assert.Len(t, paths, 2)
	assert.Contains(t, paths, "/app/db-host")
	assert.Contains(t, paths, "/app/cache-host")
}

func TestKVParameterManager_SearchByTags_MultipleTagMatch(t *testing.T) {
	params := map[string]*storedParameter{
		"/app/db-host": {
			Value: []byte("localhost"),
			Metadata: &interfaces.ParameterMetadata{
				Type:      "string",
				Tags:      map[string]string{"env": "prod", "tier": "db"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		"/app/cache-host": {
			Value: []byte("redis.local"),
			Metadata: &interfaces.ParameterMetadata{
				Type:      "string",
				Tags:      map[string]string{"env": "prod", "tier": "cache"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	provider := newSearchByTagsProvider(params)
	pm := newTestKVParameterManager(t, provider)

	// Search for prod + db tier -- should only match db-host
	paths, _, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod", "tier": "db"}, "/app/", 100, "")
	require.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Equal(t, "/app/db-host", paths[0])
}

func TestKVParameterManager_SearchByTags_WithCursor(t *testing.T) {
	params := map[string]*storedParameter{
		"/app/a": {
			Value: []byte("val-a"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "prod"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		"/app/b": {
			Value: []byte("val-b"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "prod"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		"/app/c": {
			Value: []byte("val-c"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "prod"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	provider := newSearchByTagsProvider(params)
	pm := newTestKVParameterManager(t, provider)

	// Use cursor to skip past "/app/b"
	paths, _, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod"}, "/app/", 100, "/app/b")
	require.NoError(t, err)
	assert.Len(t, paths, 1)
	assert.Equal(t, "/app/c", paths[0])
}

func TestKVParameterManager_SearchByTags_WithLimit(t *testing.T) {
	params := map[string]*storedParameter{
		"/app/a": {
			Value: []byte("val-a"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "prod"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		"/app/b": {
			Value: []byte("val-b"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "prod"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		"/app/c": {
			Value: []byte("val-c"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "prod"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	provider := newSearchByTagsProvider(params)
	pm := newTestKVParameterManager(t, provider)

	// Limit to 2 results
	paths, cursor, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod"}, "/app/", 2, "")
	require.NoError(t, err)
	assert.Len(t, paths, 2)
	assert.NotEmpty(t, cursor)
}

func TestKVParameterManager_SearchByTags_EmptyTags(t *testing.T) {
	params := map[string]*storedParameter{
		"/app/key1": {
			Value: []byte("val1"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "prod"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	provider := newSearchByTagsProvider(params)
	pm := newTestKVParameterManager(t, provider)

	// Empty tags should match everything
	paths, _, err := pm.SearchByTags(context.Background(), map[string]string{}, "/app/", 100, "")
	require.NoError(t, err)
	assert.Len(t, paths, 1)
}

func TestKVParameterManager_SearchByTags_NoMatchingTags(t *testing.T) {
	params := map[string]*storedParameter{
		"/app/key1": {
			Value: []byte("val1"),
			Metadata: &interfaces.ParameterMetadata{
				Tags:      map[string]string{"env": "dev"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}

	provider := newSearchByTagsProvider(params)
	pm := newTestKVParameterManager(t, provider)

	paths, _, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod"}, "/app/", 100, "")
	require.NoError(t, err)
	assert.Len(t, paths, 0)
}

func TestKVParameterManager_SearchByTags_ListError(t *testing.T) {
	transport := &mockKVTransport{
		scanFn: func(_ context.Context, _ string) (map[string][]byte, error) {
			return nil, errors.New("scan failed")
		},
	}
	pm := newTestKVParameterManager(t, newMockProvider(transport))

	// When scanKeys fails gracefully, search returns empty
	paths, _, err := pm.SearchByTags(context.Background(), map[string]string{"env": "prod"}, "/app/", 100, "")
	require.NoError(t, err)
	assert.Len(t, paths, 0)
}
