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

package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/jeremyhahn/go-objstore/pkg/factory"

	// Register local storage backend (always available).
	_ "github.com/jeremyhahn/go-objstore/pkg/local"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// LocalStorage implements StorageBackend for local filesystem storage.
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new local filesystem storage backend.
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(basePath, 0o750); err != nil {
		return nil, api.NewOperationFailedError("create backup directory", err)
	}

	return &LocalStorage{
		basePath: basePath,
	}, nil
}

// Upload uploads a backup to local storage.
func (s *LocalStorage) Upload(ctx context.Context, backup *Backup, reader io.Reader) error {
	// Create backup directory
	backupDir := filepath.Join(s.basePath, backup.ID)
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return api.NewOperationFailedError("create backup directory", err)
	}

	// Write backup data
	dataPath := filepath.Join(backupDir, "data")
	dataFile, err := os.Create(dataPath)
	if err != nil {
		return api.NewOperationFailedError("create data file", err)
	}
	defer dataFile.Close()

	size, err := io.Copy(dataFile, reader)
	if err != nil {
		return api.NewOperationFailedError("write backup data", err)
	}
	backup.Size = size

	// Write metadata
	metadataPath := filepath.Join(backupDir, "metadata.json")
	metadataFile, err := os.Create(metadataPath)
	if err != nil {
		return api.NewOperationFailedError("create metadata file", err)
	}
	defer metadataFile.Close()

	encoder := json.NewEncoder(metadataFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(backup); err != nil {
		return api.NewEncodeError("backup.LocalStorage.Upload", "metadata", err)
	}

	return nil
}

// Download downloads a backup from local storage.
func (s *LocalStorage) Download(ctx context.Context, backupID string) (io.ReadCloser, error) {
	dataPath := filepath.Join(s.basePath, backupID, "data")
	file, err := os.Open(dataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, api.NewNotFoundError("backup", backupID)
		}
		return nil, api.NewOperationFailedError("open backup data", err)
	}

	return file, nil
}

// Delete removes a backup from local storage.
func (s *LocalStorage) Delete(ctx context.Context, backupID string) error {
	backupDir := filepath.Join(s.basePath, backupID)
	if err := os.RemoveAll(backupDir); err != nil {
		return api.NewOperationFailedError("delete backup", err)
	}
	return nil
}

// List returns a list of available backups.
func (s *LocalStorage) List(ctx context.Context, limit int, nextToken string) (*BackupList, error) {
	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return nil, api.NewOperationFailedError("read backup directory", err)
	}

	var backups []*Backup
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip if before nextToken (for pagination)
		if nextToken != "" && entry.Name() <= nextToken {
			continue
		}

		metadata, err := s.GetMetadata(ctx, entry.Name())
		if err != nil {
			// Skip invalid backups
			continue
		}

		backups = append(backups, metadata)

		// Apply limit
		if limit > 0 && len(backups) >= limit {
			break
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	// Calculate next token
	var next string
	if limit > 0 && len(backups) >= limit {
		next = backups[len(backups)-1].ID
	}

	return &BackupList{
		Backups:    backups,
		TotalCount: len(backups),
		NextToken:  next,
	}, nil
}

// GetMetadata retrieves metadata for a specific backup.
func (s *LocalStorage) GetMetadata(ctx context.Context, backupID string) (*Backup, error) {
	metadataPath := filepath.Join(s.basePath, backupID, "metadata.json")
	file, err := os.Open(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, api.NewNotFoundError("backup", backupID)
		}
		return nil, api.NewOperationFailedError("open metadata", err)
	}
	defer file.Close()

	var backup Backup
	if err := json.NewDecoder(file).Decode(&backup); err != nil {
		return nil, api.NewDecodeError("backup.LocalStorage.GetMetadata", "metadata", err)
	}

	return &backup, nil
}

// Close closes the storage backend.
func (s *LocalStorage) Close() error {
	return nil
}

// NewStorageBackend creates a storage backend using go-objstore factory.
// This returns go-objstore's common.Storage interface directly.
// Supported backends: local, s3, minio, gcs, azure, mock (for testing).
func NewStorageBackend(config *Config) (common.Storage, error) {
	backendType := strings.ToLower(config.StorageBackend)

	// Handle mock backend for testing
	if backendType == "mock" {
		return NewMockStorage(), nil
	}

	// Validate backend type
	supportedBackends := []string{"local", "s3", "minio", "gcs", "azure", "mock"}
	isSupported := false
	for _, supported := range supportedBackends {
		if backendType == supported {
			isSupported = true
			break
		}
	}
	if !isSupported {
		return nil, api.NewUnsupportedValueError("backup.NewStorageBackend", "storage backend",
			config.StorageBackend+" (supported: "+strings.Join(supportedBackends, ", ")+")")
	}

	factoryType := backendType

	// Create storage using go-objstore factory
	storage, err := factory.NewStorage(factoryType, config.StorageConfig)
	if err != nil {
		return nil, api.NewOperationFailedError("create storage backend", err)
	}

	return storage, nil
}

// MockStorage implements both common.Storage and the backup-specific StorageBackend interface for testing.
// It provides in-memory storage for unit tests.
type MockStorage struct {
	mu       sync.RWMutex
	data     map[string][]byte
	metadata map[string]*common.Metadata
	policies map[string]common.LifecyclePolicy
	settings map[string]string

	// Backup-specific storage
	backups map[string]*Backup
}

// NewMockStorage creates a new mock storage backend for testing.
func NewMockStorage() *MockStorage {
	return &MockStorage{
		data:     make(map[string][]byte),
		metadata: make(map[string]*common.Metadata),
		policies: make(map[string]common.LifecyclePolicy),
		settings: make(map[string]string),
		backups:  make(map[string]*Backup),
	}
}

// common.Storage interface implementation

// Configure configures the storage backend with the provided settings.
func (s *MockStorage) Configure(settings map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range settings {
		s.settings[k] = v
	}
	return nil
}

// Put stores data with the given key.
func (s *MockStorage) Put(key string, data io.Reader) error {
	return s.PutWithContext(context.Background(), key, data)
}

// PutWithContext stores data with the given key using the provided context.
func (s *MockStorage) PutWithContext(ctx context.Context, key string, data io.Reader) error {
	return s.PutWithMetadata(ctx, key, data, nil)
}

// PutWithMetadata stores data with the given key and metadata.
func (s *MockStorage) PutWithMetadata(ctx context.Context, key string, data io.Reader, metadata *common.Metadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read all data
	b, err := io.ReadAll(data)
	if err != nil {
		return api.NewReadError("backup.MockStorage.PutWithMetadata", "data", err)
	}

	// Store data
	s.data[key] = b

	// Store metadata if provided
	if metadata != nil {
		s.metadata[key] = metadata
	}

	return nil
}

// Get retrieves data for the given key.
func (s *MockStorage) Get(key string) (io.ReadCloser, error) {
	return s.GetWithContext(context.Background(), key)
}

// GetWithContext retrieves data for the given key using the provided context.
func (s *MockStorage) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.data[key]
	if !ok {
		return nil, api.NewNotFoundError("key", key)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// GetMetadata retrieves metadata for the given key (common.Storage interface).
// For backup-specific metadata retrieval, use GetBackupMetadata instead.
func (s *MockStorage) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if key exists
	if _, ok := s.data[key]; !ok {
		return nil, api.NewNotFoundError("key", key)
	}

	// Return metadata if it exists, otherwise return empty metadata
	if metadata, ok := s.metadata[key]; ok {
		return metadata, nil
	}

	return &common.Metadata{}, nil
}

// UpdateMetadata updates metadata for the given key.
func (s *MockStorage) UpdateMetadata(ctx context.Context, key string, metadata *common.Metadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if key exists
	if _, ok := s.data[key]; !ok {
		return api.NewNotFoundError("key", key)
	}

	s.metadata[key] = metadata
	return nil
}

// Delete removes data for the given key (common.Storage interface).
func (s *MockStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	delete(s.metadata, key)
	delete(s.backups, key)
	return nil
}

// DeleteWithContext removes data for the given key using the provided context.
func (s *MockStorage) DeleteWithContext(ctx context.Context, key string) error {
	return s.Delete(key)
}

// Exists checks if a key exists in storage.
func (s *MockStorage) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.data[key]
	return ok, nil
}

// List returns all keys with the given prefix (common.Storage interface).
func (s *MockStorage) List(prefix string) ([]string, error) {
	return s.ListWithContext(context.Background(), prefix)
}

// ListWithContext returns all keys with the given prefix using the provided context.
func (s *MockStorage) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	for key := range s.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)
	return keys, nil
}

// ListWithOptions returns keys with the given options.
func (s *MockStorage) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := ""
	if opts != nil && opts.Prefix != "" {
		prefix = opts.Prefix
	}

	var keys []string
	for key := range s.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)

	// Apply pagination
	maxResults := 1000
	if opts != nil && opts.MaxResults > 0 {
		maxResults = opts.MaxResults
	}

	startIdx := 0
	if opts != nil && opts.ContinueFrom != "" {
		// Find the index after the continuation token
		for i, key := range keys {
			if key == opts.ContinueFrom {
				startIdx = i + 1
				break
			}
		}
	}

	endIdx := startIdx + maxResults
	if endIdx > len(keys) {
		endIdx = len(keys)
	}

	// Build ObjectInfo list
	objects := make([]*common.ObjectInfo, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		key := keys[i]
		meta := s.metadata[key]
		if meta == nil {
			meta = &common.Metadata{}
		}
		objects = append(objects, &common.ObjectInfo{
			Key:      key,
			Metadata: meta,
		})
	}

	result := &common.ListResult{
		Objects:   objects,
		Truncated: endIdx < len(keys),
	}

	if result.Truncated {
		result.NextToken = keys[endIdx-1]
	}

	return result, nil
}

// Archive archives the key to the destination archiver.
func (s *MockStorage) Archive(key string, destination common.Archiver) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.data[key]
	if !ok {
		return api.NewNotFoundError("key", key)
	}

	return destination.Put(key, bytes.NewReader(data))
}

// AddPolicy adds a lifecycle policy.
func (s *MockStorage) AddPolicy(policy common.LifecyclePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.policies[policy.ID] = policy
	return nil
}

// RemovePolicy removes a lifecycle policy by ID.
func (s *MockStorage) RemovePolicy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.policies, id)
	return nil
}

// GetPolicies returns all lifecycle policies.
func (s *MockStorage) GetPolicies() ([]common.LifecyclePolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policies := make([]common.LifecyclePolicy, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, policy)
	}

	return policies, nil
}

// Backup-specific interface methods for test compatibility.
// These methods use the backup-specific types and may have overlapping functionality
// with common.Storage methods but with different signatures.

// Upload uploads a backup to storage.
func (s *MockStorage) Upload(ctx context.Context, backup *Backup, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.backups[backup.ID] = backup
	s.data[backup.ID] = data
	backup.Size = int64(len(data))

	return nil
}

// Download downloads a backup from storage.
func (s *MockStorage) Download(ctx context.Context, backupID string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.data[backupID]
	if !ok {
		return nil, api.NewNotFoundError("backup", backupID)
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// Close closes the storage backend.
func (s *MockStorage) Close() error {
	return nil
}

// ListBackups returns a list of backups (backup-specific method, different name from List).
func (s *MockStorage) ListBackups(ctx context.Context, limit int, nextToken string) (*BackupList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var backups []*Backup
	for _, backup := range s.backups {
		backups = append(backups, backup)
	}

	// Sort by timestamp
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	if limit > 0 && len(backups) > limit {
		backups = backups[:limit]
	}

	return &BackupList{
		Backups:    backups,
		TotalCount: len(backups),
	}, nil
}

// DeleteBackup removes a backup (backup-specific method, different name from Delete).
func (s *MockStorage) DeleteBackup(ctx context.Context, backupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.backups, backupID)
	delete(s.data, backupID)
	delete(s.metadata, backupID)
	return nil
}

// GetBackupMetadata retrieves backup metadata (backup-specific method, different name from GetMetadata).
func (s *MockStorage) GetBackupMetadata(ctx context.Context, backupID string) (*Backup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	backup, ok := s.backups[backupID]
	if !ok {
		return nil, api.NewNotFoundError("backup", backupID)
	}
	return backup, nil
}
