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

package config

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetStorageBackend tests the GetStorageBackend function.
func TestGetStorageBackend(t *testing.T) {
	tests := []struct {
		name        string
		backendName string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty backend name",
			backendName: "",
			config:      &Config{},
			wantErr:     true,
			errContains: "backend name is required",
		},
		{
			name:        "named backend exists",
			backendName: "test-backend",
			config: &Config{
				ObjectStorage: ObjectStorageConfig{
					Backends: map[string]*StorageBackendConfig{
						"test-backend": {
							Type:   "memory",
							Config: map[string]string{},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:        "backward compatibility - type as name",
			backendName: "memory",
			config: &Config{
				ObjectStorage: ObjectStorageConfig{
					Backends: nil,
				},
			},
			wantErr: false,
		},
		{
			name:        "unsupported backend type",
			backendName: "invalid-type",
			config: &Config{
				ObjectStorage: ObjectStorageConfig{
					Backends: nil,
				},
			},
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := GetStorageBackend(tt.backendName, tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, storage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, storage)
				// Close if it's MemoryStorage
				if ms, ok := storage.(*MemoryStorage); ok {
					ms.Close()
				}
			}
		})
	}
}

// TestCreateStorageBackend tests the CreateStorageBackend function.
func TestCreateStorageBackend(t *testing.T) {
	tests := []struct {
		name        string
		config      *StorageBackendConfig
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil config",
			config:      nil,
			wantErr:     true,
			errContains: "configuration is nil",
		},
		{
			name: "valid memory backend",
			config: &StorageBackendConfig{
				Type:   "memory",
				Config: map[string]string{},
			},
			wantErr: false,
		},
		{
			name: "unsupported backend type",
			config: &StorageBackendConfig{
				Type:   "redis",
				Config: map[string]string{},
			},
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := CreateStorageBackend(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, storage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, storage)
				if ms, ok := storage.(*MemoryStorage); ok {
					ms.Close()
				}
			}
		})
	}
}

// TestCreateStorageBackendByType tests the CreateStorageBackendByType function.
func TestCreateStorageBackendByType(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		backendType string
		config      map[string]string
		wantErr     bool
		errContains string
	}{
		{
			name:        "memory backend",
			backendType: "memory",
			config:      nil,
			wantErr:     false,
		},
		{
			name:        "unsupported backend",
			backendType: "cassandra",
			config:      nil,
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
		{
			name:        "case insensitivity",
			backendType: "MEMORY",
			config:      nil,
			wantErr:     false,
		},
		{
			name:        "local backend with valid path",
			backendType: "local",
			config:      map[string]string{"path": tempDir},
			wantErr:     false,
		},
		{
			name:        "file is not a valid backend type",
			backendType: "file",
			config:      map[string]string{"path": tempDir},
			wantErr:     true,
			errContains: "unsupported storage backend",
		},
		{
			name:        "s3 backend - invalid without credentials",
			backendType: "s3",
			config:      nil,
			wantErr:     true,
			errContains: "create storage backend failed",
		},
		{
			name:        "gcs backend - invalid without credentials",
			backendType: "gcs",
			config:      nil,
			wantErr:     true,
			errContains: "create storage backend failed",
		},
		{
			name:        "azure backend - invalid without credentials",
			backendType: "azure",
			config:      nil,
			wantErr:     true,
			errContains: "create storage backend failed",
		},
		{
			name:        "minio backend - invalid without credentials",
			backendType: "minio",
			config:      nil,
			wantErr:     true,
			errContains: "create storage backend failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := CreateStorageBackendByType(tt.backendType, tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, storage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, storage)
				if ms, ok := storage.(*MemoryStorage); ok {
					ms.Close()
				}
			}
		})
	}
}

// TestResolveBackupStorageBackend tests backup storage backend resolution.
func TestResolveBackupStorageBackend(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "backup not enabled",
			config: &Config{
				Backup: BackupConfig{
					Enabled: false,
				},
			},
			wantErr:     true,
			errContains: "backup is not enabled",
		},
		{
			name: "backup enabled but no storage backend",
			config: &Config{
				Backup: BackupConfig{
					Enabled:        true,
					StorageBackend: "",
				},
			},
			wantErr:     true,
			errContains: "backup storage backend is required",
		},
		{
			name: "backup enabled with valid backend",
			config: &Config{
				Backup: BackupConfig{
					Enabled:        true,
					StorageBackend: "backup-storage",
				},
				ObjectStorage: ObjectStorageConfig{
					Backends: map[string]*StorageBackendConfig{
						"backup-storage": {
							Type:   "memory",
							Config: map[string]string{},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := ResolveBackupStorageBackend(tt.config)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, storage)
			} else {
				require.NoError(t, err)
				require.NotNil(t, storage)
				if ms, ok := storage.(*MemoryStorage); ok {
					ms.Close()
				}
			}
		})
	}
}

// TestNewMemoryStorage tests memory storage creation.
func TestNewMemoryStorage(t *testing.T) {
	storage := NewMemoryStorage()
	require.NotNil(t, storage)
	assert.NotNil(t, storage.data)
	assert.NotNil(t, storage.metadata)
	storage.Close()
}

// TestMemoryStorage_Configure tests memory storage configuration.
func TestMemoryStorage_Configure(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	err := storage.Configure(map[string]string{"key": "value"})
	assert.NoError(t, err)
}

// TestMemoryStorage_PutAndGet tests put and get operations.
func TestMemoryStorage_PutAndGet(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	testData := []byte("test data")
	key := "test-key"

	// Test Put
	err := storage.Put(key, bytes.NewReader(testData))
	require.NoError(t, err)

	// Test Get
	reader, err := storage.Get(key)
	require.NoError(t, err)
	require.NotNil(t, reader)
	defer reader.Close()

	// Verify data
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, string(testData), buf.String())
}

// TestMemoryStorage_GetNonExistent tests getting a non-existent key.
func TestMemoryStorage_GetNonExistent(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	reader, err := storage.Get("non-existent")
	assert.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "key not found")
}

// TestMemoryStorage_PutWithContext tests put with context.
func TestMemoryStorage_PutWithContext(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	testData := []byte("context test data")
	key := "context-key"

	ctx := context.Background()
	err := storage.PutWithContext(ctx, key, bytes.NewReader(testData))
	require.NoError(t, err)

	// Verify with GetWithContext
	reader, err := storage.GetWithContext(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, reader)
	defer reader.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, string(testData), buf.String())
}

// TestMemoryStorage_PutWithMetadata tests put with metadata.
func TestMemoryStorage_PutWithMetadata(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	testData := []byte("metadata test")
	key := "meta-key"
	metadata := &common.Metadata{
		ContentType: "text/plain",
		Size:        int64(len(testData)),
	}

	ctx := context.Background()
	err := storage.PutWithMetadata(ctx, key, bytes.NewReader(testData), metadata)
	require.NoError(t, err)

	// Verify metadata
	retrievedMeta, err := storage.GetMetadata(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, retrievedMeta)
	assert.Equal(t, metadata.ContentType, retrievedMeta.ContentType)
	assert.Equal(t, metadata.Size, retrievedMeta.Size)
}

// TestMemoryStorage_GetMetadata tests metadata retrieval.
func TestMemoryStorage_GetMetadata(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()

	// Test for non-existent key
	_, err := storage.GetMetadata(ctx, "non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")

	// Test for key without metadata
	err = storage.Put("no-meta", strings.NewReader("data"))
	require.NoError(t, err)

	meta, err := storage.GetMetadata(ctx, "no-meta")
	require.NoError(t, err)
	assert.NotNil(t, meta)
}

// TestMemoryStorage_UpdateMetadata tests metadata update.
func TestMemoryStorage_UpdateMetadata(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()
	key := "update-meta-key"

	// First put some data
	err := storage.Put(key, strings.NewReader("data"))
	require.NoError(t, err)

	// Update metadata
	newMeta := &common.Metadata{
		ContentType: "application/json",
		Size:        100,
	}
	err = storage.UpdateMetadata(ctx, key, newMeta)
	require.NoError(t, err)

	// Verify updated metadata
	retrievedMeta, err := storage.GetMetadata(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, newMeta.ContentType, retrievedMeta.ContentType)
	assert.Equal(t, newMeta.Size, retrievedMeta.Size)
}

// TestMemoryStorage_UpdateMetadataNonExistent tests updating metadata for non-existent key.
func TestMemoryStorage_UpdateMetadataNonExistent(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()
	err := storage.UpdateMetadata(ctx, "non-existent", &common.Metadata{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
}

// TestMemoryStorage_Delete tests delete operation.
func TestMemoryStorage_Delete(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	key := "delete-key"

	// Put data
	err := storage.Put(key, strings.NewReader("data to delete"))
	require.NoError(t, err)

	// Verify exists
	_, err = storage.Get(key)
	require.NoError(t, err)

	// Delete
	err = storage.Delete(key)
	require.NoError(t, err)

	// Verify deleted
	_, err = storage.Get(key)
	assert.Error(t, err)
}

// TestMemoryStorage_DeleteWithContext tests delete with context.
func TestMemoryStorage_DeleteWithContext(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()
	key := "delete-ctx-key"

	// Put data
	err := storage.PutWithContext(ctx, key, strings.NewReader("data"))
	require.NoError(t, err)

	// Delete with context
	err = storage.DeleteWithContext(ctx, key)
	require.NoError(t, err)

	// Verify deleted
	_, err = storage.Get(key)
	assert.Error(t, err)
}

// TestMemoryStorage_Exists tests exists check.
func TestMemoryStorage_Exists(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()
	key := "exists-key"

	// Check non-existent key
	exists, err := storage.Exists(ctx, "non-existent")
	require.NoError(t, err)
	assert.False(t, exists)

	// Put data
	err = storage.Put(key, strings.NewReader("data"))
	require.NoError(t, err)

	// Check existent key
	exists, err = storage.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestMemoryStorage_List tests list operation.
func TestMemoryStorage_List(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	// Put multiple keys with different prefixes
	testKeys := map[string]string{
		"prefix/key1": "data1",
		"prefix/key2": "data2",
		"other/key3":  "data3",
	}

	for key, data := range testKeys {
		err := storage.Put(key, strings.NewReader(data))
		require.NoError(t, err)
	}

	// List with prefix
	keys, err := storage.List("prefix/")
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "prefix/key1")
	assert.Contains(t, keys, "prefix/key2")

	// List all
	keys, err = storage.List("")
	require.NoError(t, err)
	assert.Len(t, keys, 3)
}

// TestMemoryStorage_ListWithContext tests list with context.
func TestMemoryStorage_ListWithContext(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()

	// Put data
	err := storage.Put("ctx/key1", strings.NewReader("data1"))
	require.NoError(t, err)
	err = storage.Put("ctx/key2", strings.NewReader("data2"))
	require.NoError(t, err)

	// List with context
	keys, err := storage.ListWithContext(ctx, "ctx/")
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

// TestMemoryStorage_ListWithOptions tests list with options.
func TestMemoryStorage_ListWithOptions(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()

	// Put data with metadata
	testData := map[string]string{
		"opt/key1": "data1",
		"opt/key2": "data2",
		"other":    "data3",
	}

	for key, data := range testData {
		meta := &common.Metadata{
			ContentType: "text/plain",
			Size:        int64(len(data)),
		}
		err := storage.PutWithMetadata(ctx, key, strings.NewReader(data), meta)
		require.NoError(t, err)
	}

	// List with options
	opts := &common.ListOptions{
		Prefix: "opt/",
	}

	result, err := storage.ListWithOptions(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Objects, 2)
	assert.False(t, result.Truncated)

	// Verify objects have metadata
	for _, obj := range result.Objects {
		assert.NotNil(t, obj.Metadata)
		assert.Equal(t, "text/plain", obj.Metadata.ContentType)
	}

	// Test with nil options
	result, err = storage.ListWithOptions(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, result.Objects, 3)
}

// TestMemoryStorage_Archive tests archive operation (should error).
func TestMemoryStorage_Archive(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	err := storage.Archive("key", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "archive not supported")
}

// TestMemoryStorage_AddPolicy tests add policy (no-op).
func TestMemoryStorage_AddPolicy(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	err := storage.AddPolicy(common.LifecyclePolicy{})
	assert.NoError(t, err)
}

// TestMemoryStorage_RemovePolicy tests remove policy (no-op).
func TestMemoryStorage_RemovePolicy(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	err := storage.RemovePolicy("policy-id")
	assert.NoError(t, err)
}

// TestMemoryStorage_GetPolicies tests get policies.
func TestMemoryStorage_GetPolicies(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	policies, err := storage.GetPolicies()
	assert.NoError(t, err)
	assert.Nil(t, policies)
}

// TestMemoryStorage_Close tests close operation.
func TestMemoryStorage_Close(t *testing.T) {
	storage := NewMemoryStorage()

	// Put some data
	err := storage.Put("key", strings.NewReader("data"))
	require.NoError(t, err)

	// Close
	err = storage.Close()
	assert.NoError(t, err)

	// Verify data and metadata are cleared
	assert.Nil(t, storage.data)
	assert.Nil(t, storage.metadata)
}

// TestMemoryStorage_CompleteWorkflow tests a complete workflow.
func TestMemoryStorage_CompleteWorkflow(t *testing.T) {
	storage := NewMemoryStorage()
	defer storage.Close()

	ctx := context.Background()

	// 1. Put data with metadata
	key := "workflow/test"
	data := []byte("workflow test data")
	meta := &common.Metadata{
		ContentType: "application/octet-stream",
		Size:        int64(len(data)),
	}

	err := storage.PutWithMetadata(ctx, key, bytes.NewReader(data), meta)
	require.NoError(t, err)

	// 2. Verify exists
	exists, err := storage.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)

	// 3. Get data
	reader, err := storage.GetWithContext(ctx, key)
	require.NoError(t, err)
	defer reader.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(reader)
	require.NoError(t, err)
	assert.Equal(t, string(data), buf.String())

	// 4. Get metadata
	retrievedMeta, err := storage.GetMetadata(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, meta.ContentType, retrievedMeta.ContentType)

	// 5. Update metadata
	newMeta := &common.Metadata{
		ContentType: "text/plain",
		Size:        int64(len(data)),
	}
	err = storage.UpdateMetadata(ctx, key, newMeta)
	require.NoError(t, err)

	// 6. Verify updated metadata
	retrievedMeta, err = storage.GetMetadata(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "text/plain", retrievedMeta.ContentType)

	// 7. List keys
	keys, err := storage.ListWithContext(ctx, "workflow/")
	require.NoError(t, err)
	assert.Contains(t, keys, key)

	// 8. Delete
	err = storage.DeleteWithContext(ctx, key)
	require.NoError(t, err)

	// 9. Verify deleted
	exists, err = storage.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

// BenchmarkMemoryStorage_Put benchmarks put operation.
func BenchmarkMemoryStorage_Put(b *testing.B) {
	storage := NewMemoryStorage()
	defer storage.Close()

	data := []byte("benchmark test data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := string(rune(i))
		_ = storage.Put(key, bytes.NewReader(data))
	}
}

// BenchmarkMemoryStorage_Get benchmarks get operation.
func BenchmarkMemoryStorage_Get(b *testing.B) {
	storage := NewMemoryStorage()
	defer storage.Close()

	// Setup: put data
	data := []byte("benchmark test data")
	for i := 0; i < 1000; i++ {
		key := string(rune(i))
		_ = storage.Put(key, bytes.NewReader(data))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := string(rune(i % 1000))
		reader, _ := storage.Get(key)
		if reader != nil {
			reader.Close()
		}
	}
}
