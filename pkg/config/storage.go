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

// Package config provides storage backend factory methods for QuicBaaS.
package config

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/jeremyhahn/go-objstore/pkg/factory"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"

	// Register storage backends.
	_ "github.com/jeremyhahn/go-objstore/pkg/local"
)

// GetStorageBackend returns a storage backend by name from the config.
// If the name references a named backend in ObjectStorage.Backends, that backend is used.
// Otherwise, the name is treated as a backend type for backward compatibility.
func GetStorageBackend(backendName string, cfg *Config) (common.Storage, error) {
	if backendName == "" {
		return nil, api.NewRequiredFieldError("backend name")
	}

	// Check if it's a named backend reference
	if cfg.ObjectStorage.Backends != nil {
		if backendCfg, ok := cfg.ObjectStorage.Backends[backendName]; ok {
			return CreateStorageBackend(backendCfg)
		}
	}

	// Backward compatibility: treat as backend type with empty config
	return CreateStorageBackendByType(backendName, nil)
}

// CreateStorageBackend creates a storage backend from a StorageBackendConfig.
func CreateStorageBackend(cfg *StorageBackendConfig) (common.Storage, error) {
	if cfg == nil {
		return nil, api.NewNilConfigError("config.CreateStorageBackend")
	}

	return CreateStorageBackendByType(cfg.Type, cfg.Config)
}

// CreateStorageBackendByType creates a storage backend from type and config map.
func CreateStorageBackendByType(backendType string, config map[string]string) (common.Storage, error) {
	backendType = strings.ToLower(backendType)

	// Handle memory backend specially (not in go-objstore factory)
	if backendType == "memory" {
		return NewMemoryStorage(), nil
	}

	// Validate backend type
	supportedBackends := []string{"local", "s3", "minio", "gcs", "azure", "memory"}
	isSupported := false
	for _, supported := range supportedBackends {
		if backendType == supported {
			isSupported = true
			break
		}
	}
	if !isSupported {
		return nil, api.NewUnsupportedBackendError("config.CreateStorageBackendByType",
			backendType, supportedBackends)
	}

	// Create storage using go-objstore factory.
	// The factory registers backends by their canonical names (e.g., "local"),
	// so we pass the backend type directly without remapping.
	storage, err := factory.NewStorage(backendType, config)
	if err != nil {
		return nil, api.NewOperationFailedError("create storage backend", err)
	}

	return storage, nil
}

// ResolveBackupStorageBackend returns the storage backend for backups.
func ResolveBackupStorageBackend(cfg *Config) (common.Storage, error) {
	if !cfg.Backup.Enabled {
		return nil, api.NewBackupNotEnabledError("config.ResolveBackupStorageBackend")
	}

	if cfg.Backup.StorageBackend == "" {
		return nil, api.NewBackupBackendRequiredError("config.ResolveBackupStorageBackend")
	}

	return GetStorageBackend(cfg.Backup.StorageBackend, cfg)
}

// MemoryStorage implements common.Storage using in-memory storage.
// This is useful for development and testing. It is thread-safe.
type MemoryStorage struct {
	mu       sync.RWMutex
	data     map[string][]byte
	metadata map[string]*common.Metadata
}

// NewMemoryStorage creates a new in-memory storage backend.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		data:     make(map[string][]byte),
		metadata: make(map[string]*common.Metadata),
	}
}

// Configure configures the storage backend.
func (m *MemoryStorage) Configure(_ map[string]string) error {
	return nil
}

// Put stores data with the given key.
func (m *MemoryStorage) Put(key string, data io.Reader) error {
	b, err := io.ReadAll(data)
	if err != nil {
		return api.NewOperationFailedError("read data", err)
	}
	m.mu.Lock()
	m.data[key] = b
	m.mu.Unlock()
	return nil
}

// PutWithContext stores data with context.
func (m *MemoryStorage) PutWithContext(_ context.Context, key string, data io.Reader) error {
	return m.Put(key, data)
}

// PutWithMetadata stores data with metadata.
func (m *MemoryStorage) PutWithMetadata(_ context.Context, key string, data io.Reader, meta *common.Metadata) error {
	b, err := io.ReadAll(data)
	if err != nil {
		return api.NewOperationFailedError("read data", err)
	}
	m.mu.Lock()
	m.data[key] = b
	if meta != nil {
		m.metadata[key] = meta
	}
	m.mu.Unlock()
	return nil
}

// Get retrieves data for the given key.
func (m *MemoryStorage) Get(key string) (io.ReadCloser, error) {
	m.mu.RLock()
	data, ok := m.data[key]
	m.mu.RUnlock()
	if !ok {
		return nil, api.NewStorageKeyNotFoundError("MemoryStorage.Get", key)
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

// GetWithContext retrieves data with context.
func (m *MemoryStorage) GetWithContext(_ context.Context, key string) (io.ReadCloser, error) {
	return m.Get(key)
}

// GetMetadata retrieves metadata for the given key.
func (m *MemoryStorage) GetMetadata(_ context.Context, key string) (*common.Metadata, error) {
	m.mu.RLock()
	_, dataOk := m.data[key]
	meta := m.metadata[key]
	m.mu.RUnlock()
	if !dataOk {
		return nil, api.NewStorageKeyNotFoundError("MemoryStorage.GetMetadata", key)
	}
	if meta != nil {
		return meta, nil
	}
	return &common.Metadata{}, nil
}

// UpdateMetadata updates metadata for the given key.
func (m *MemoryStorage) UpdateMetadata(_ context.Context, key string, meta *common.Metadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[key]; !ok {
		return api.NewStorageKeyNotFoundError("MemoryStorage.UpdateMetadata", key)
	}
	m.metadata[key] = meta
	return nil
}

// Delete removes data for the given key.
func (m *MemoryStorage) Delete(key string) error {
	m.mu.Lock()
	delete(m.data, key)
	delete(m.metadata, key)
	m.mu.Unlock()
	return nil
}

// DeleteWithContext removes data with context.
func (m *MemoryStorage) DeleteWithContext(_ context.Context, key string) error {
	return m.Delete(key)
}

// Exists checks if a key exists.
func (m *MemoryStorage) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	_, ok := m.data[key]
	m.mu.RUnlock()
	return ok, nil
}

// List returns all keys with the given prefix.
func (m *MemoryStorage) List(prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for key := range m.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// ListWithContext returns all keys with context.
func (m *MemoryStorage) ListWithContext(_ context.Context, prefix string) ([]string, error) {
	return m.List(prefix)
}

// ListWithOptions returns keys with options.
func (m *MemoryStorage) ListWithOptions(_ context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	prefix := ""
	if opts != nil && opts.Prefix != "" {
		prefix = opts.Prefix
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for key := range m.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	objects := make([]*common.ObjectInfo, 0, len(keys))
	for _, key := range keys {
		meta := m.metadata[key]
		if meta == nil {
			meta = &common.Metadata{}
		}
		objects = append(objects, &common.ObjectInfo{
			Key:      key,
			Metadata: meta,
		})
	}

	return &common.ListResult{
		Objects:   objects,
		Truncated: false,
	}, nil
}

// Archive archives the key to the destination.
func (m *MemoryStorage) Archive(key string, _ common.Archiver) error {
	return api.NewArchiveNotSupportedError("MemoryStorage.Archive", "memory")
}

// AddPolicy adds a lifecycle policy.
func (m *MemoryStorage) AddPolicy(_ common.LifecyclePolicy) error {
	return nil // No-op for memory storage
}

// RemovePolicy removes a lifecycle policy.
func (m *MemoryStorage) RemovePolicy(_ string) error {
	return nil // No-op for memory storage
}

// GetPolicies returns all lifecycle policies.
func (m *MemoryStorage) GetPolicies() ([]common.LifecyclePolicy, error) {
	return nil, nil
}

// Close closes the storage backend.
func (m *MemoryStorage) Close() error {
	m.data = nil
	m.metadata = nil
	return nil
}

// Ensure fmt is available for methods that still need it (e.g., Sprintf for version numbers).
var _ = fmt.Sprintf
