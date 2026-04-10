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

package catalog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-objstore/pkg/common"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// reencryptMockCatalog implements CatalogManager for reencrypt testing.
type reencryptMockCatalog struct {
	entries     map[string]*CatalogEntry
	mu          sync.RWMutex
	getError    error
	updateError error
}

func newReencryptMockCatalog() *reencryptMockCatalog {
	return &reencryptMockCatalog{
		entries: make(map[string]*CatalogEntry),
	}
}

func (m *reencryptMockCatalog) Put(ctx context.Context, entry *CatalogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entry.Key] = entry.Clone()
	return nil
}

func (m *reencryptMockCatalog) Get(ctx context.Context, key string) (*CatalogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getError != nil {
		return nil, m.getError
	}
	entry, ok := m.entries[key]
	if !ok {
		return nil, NewEntryNotFoundError(key)
	}
	return entry.Clone(), nil
}

func (m *reencryptMockCatalog) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

func (m *reencryptMockCatalog) Update(ctx context.Context, key string, updater func(*CatalogEntry) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateError != nil {
		return m.updateError
	}
	entry, ok := m.entries[key]
	if !ok {
		return NewEntryNotFoundError(key)
	}
	if err := updater(entry); err != nil {
		return err
	}
	return nil
}

func (m *reencryptMockCatalog) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.entries[key]
	return ok, nil
}

// Stub implementations for other CatalogManager methods
func (m *reencryptMockCatalog) AddLocation(ctx context.Context, key string, loc *StoreLocation) error {
	return nil
}
func (m *reencryptMockCatalog) RemoveLocation(ctx context.Context, key string, store string) error {
	return nil
}
func (m *reencryptMockCatalog) SetPrimaryStore(ctx context.Context, key string, store string) error {
	return nil
}
func (m *reencryptMockCatalog) GetLocations(ctx context.Context, key string) ([]*StoreLocation, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) UpdateLocationStatus(ctx context.Context, key, store string, status LocationStatus) error {
	return nil
}
func (m *reencryptMockCatalog) ListByStore(ctx context.Context, store string, opts *ListOptions) (*ListResult, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) ListByTenant(ctx context.Context, tenant string, opts *ListOptions) (*ListResult, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) ListByPrefix(ctx context.Context, prefix string, opts *ListOptions) (*ListResult, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) ListByTag(ctx context.Context, tag, value string, opts *ListOptions) (*ListResult, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) (*ListResult, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) Search(ctx context.Context, query *SearchQuery) (*SearchResult, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	return nil
}
func (m *reencryptMockCatalog) EnqueueArchive(ctx context.Context, key string, targetStore string) error {
	return nil
}
func (m *reencryptMockCatalog) EnqueueVerify(ctx context.Context, key string, stores []string) error {
	return nil
}
func (m *reencryptMockCatalog) DequeueReplication(ctx context.Context, limit int) ([]*ReplicationTask, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) DequeueArchive(ctx context.Context, limit int) ([]*ArchiveTask, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) DequeueVerify(ctx context.Context, limit int) ([]*VerifyTask, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) GetTenantUsage(ctx context.Context, tenant string) (*TenantUsage, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) GetStoreUsage(ctx context.Context, store string) (*StoreUsage, error) {
	return nil, nil
}
func (m *reencryptMockCatalog) UpdateAccessTime(ctx context.Context, key string) error {
	return nil
}
func (m *reencryptMockCatalog) Close() error { return nil }
func (m *reencryptMockCatalog) Sync() error  { return nil }

// reencryptMockStorage implements common.Storage for reencrypt testing.
type reencryptMockStorage struct {
	data     map[string][]byte
	mu       sync.RWMutex
	getError error
	putError error
}

func newReencryptMockStorage() *reencryptMockStorage {
	return &reencryptMockStorage{
		data: make(map[string][]byte),
	}
}

func (s *reencryptMockStorage) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.getError != nil {
		return nil, s.getError
	}
	data, ok := s.data[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *reencryptMockStorage) PutWithContext(ctx context.Context, key string, reader io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putError != nil {
		return s.putError
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.data[key] = data
	return nil
}

// Stub implementations for other common.Storage methods
func (s *reencryptMockStorage) Get(key string) (io.ReadCloser, error) {
	return s.GetWithContext(context.Background(), key)
}
func (s *reencryptMockStorage) Put(key string, reader io.Reader) error {
	return s.PutWithContext(context.Background(), key, reader)
}
func (s *reencryptMockStorage) Delete(key string) error                                 { return nil }
func (s *reencryptMockStorage) DeleteWithContext(ctx context.Context, key string) error { return nil }
func (s *reencryptMockStorage) List(prefix string) ([]string, error)                    { return nil, nil }
func (s *reencryptMockStorage) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}
func (s *reencryptMockStorage) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}
func (s *reencryptMockStorage) Size(key string) (int64, error) { return 0, nil }
func (s *reencryptMockStorage) SizeWithContext(ctx context.Context, key string) (int64, error) {
	return 0, nil
}

// Additional stub implementations for common.Storage interface
func (s *reencryptMockStorage) Configure(settings map[string]string) error {
	return nil
}
func (s *reencryptMockStorage) PutWithMetadata(ctx context.Context, key string, data io.Reader, metadata *common.Metadata) error {
	return s.PutWithContext(ctx, key, data)
}
func (s *reencryptMockStorage) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	return nil, nil
}
func (s *reencryptMockStorage) UpdateMetadata(ctx context.Context, key string, metadata *common.Metadata) error {
	return nil
}
func (s *reencryptMockStorage) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	return nil, nil
}
func (s *reencryptMockStorage) Archive(key string, destination common.Archiver) error {
	return nil
}

// LifecycleManager implementation
func (s *reencryptMockStorage) AddPolicy(policy common.LifecyclePolicy) error {
	return nil
}
func (s *reencryptMockStorage) RemovePolicy(id string) error {
	return nil
}
func (s *reencryptMockStorage) GetPolicies() ([]common.LifecyclePolicy, error) {
	return nil, nil
}

// reencryptMockBackends implements BackendManager for reencrypt testing.
type reencryptMockBackends struct {
	backends map[string]common.Storage
	mu       sync.RWMutex
}

func newReencryptMockBackends() *reencryptMockBackends {
	return &reencryptMockBackends{
		backends: make(map[string]common.Storage),
	}
}

func (m *reencryptMockBackends) RegisterStorage(name string, storage common.Storage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[name] = storage
}

func (m *reencryptMockBackends) GetBackend(name string) (common.Storage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	backend, ok := m.backends[name]
	if !ok {
		return nil, NewBackendNotFoundError(name)
	}
	return backend, nil
}

func (m *reencryptMockBackends) ListBackends() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}
	return names
}

func (m *reencryptMockBackends) GetBackendConfig(name string) (*BackendConfig, error) {
	return nil, nil
}
func (m *reencryptMockBackends) RegisterBackend(name string, cfg *BackendConfig) error {
	return nil
}
func (m *reencryptMockBackends) UnregisterBackend(name string) error { return nil }
func (m *reencryptMockBackends) SelectBackend(criteria *SelectionCriteria) (string, common.Storage, error) {
	return "", nil, nil
}
func (m *reencryptMockBackends) CheckHealth(ctx context.Context, name string) (*BackendHealth, error) {
	return nil, nil
}
func (m *reencryptMockBackends) CheckAllHealth(ctx context.Context) map[string]*BackendHealth {
	return nil
}
func (m *reencryptMockBackends) Close() error { return nil }

// reencryptMockEncryption implements EncryptionManager for reencrypt testing.
type reencryptMockEncryption struct {
	encryptFunc   func(ctx context.Context, data []byte, cfg *config.ObjectStoreEncryption) (*EncryptionResult, error)
	decryptFunc   func(ctx context.Context, data []byte, enc *LocationEncryption) ([]byte, error)
	reencryptFunc func(ctx context.Context, data []byte, oldEnc *LocationEncryption, newKeyID string) (*EncryptionResult, error)
}

func newReencryptMockEncryption() *reencryptMockEncryption {
	return &reencryptMockEncryption{}
}

func (m *reencryptMockEncryption) Encrypt(ctx context.Context, data []byte, cfg *config.ObjectStoreEncryption) (*EncryptionResult, error) {
	if m.encryptFunc != nil {
		return m.encryptFunc(ctx, data, cfg)
	}
	// Default: return "encrypted" data with metadata
	return &EncryptionResult{
		Ciphertext: append([]byte("enc:"), data...),
		Encryption: &LocationEncryption{
			KeyID:     cfg.DefaultKey,
			Algorithm: cfg.Algorithm,
			Nonce:     []byte("testnonce123"),
		},
	}, nil
}

func (m *reencryptMockEncryption) Decrypt(ctx context.Context, data []byte, enc *LocationEncryption) ([]byte, error) {
	if m.decryptFunc != nil {
		return m.decryptFunc(ctx, data, enc)
	}
	// Default: strip "enc:" prefix
	if bytes.HasPrefix(data, []byte("enc:")) {
		return data[4:], nil
	}
	return data, nil
}

func (m *reencryptMockEncryption) ResolveEncryptionConfig(tenantID string, shardID uint64, objectOverride *ObjectEncryption) (*config.ObjectStoreEncryption, error) {
	return &config.ObjectStoreEncryption{
		Enabled:    true,
		DefaultKey: "default/key",
		Algorithm:  "AES-256-GCM",
	}, nil
}

func (m *reencryptMockEncryption) ShouldEncrypt(cfg *config.ObjectStoreEncryption) bool {
	return cfg != nil && cfg.Enabled && cfg.Enforce
}

func (m *reencryptMockEncryption) Reencrypt(ctx context.Context, data []byte, oldEnc *LocationEncryption, newKeyID string) (*EncryptionResult, error) {
	if m.reencryptFunc != nil {
		return m.reencryptFunc(ctx, data, oldEnc, newKeyID)
	}
	// Default: decrypt and re-encrypt
	plaintext, err := m.Decrypt(ctx, data, oldEnc)
	if err != nil {
		return nil, err
	}
	return &EncryptionResult{
		Ciphertext: append([]byte("enc:"), plaintext...),
		Encryption: &LocationEncryption{
			KeyID:     newKeyID,
			Algorithm: "AES-256-GCM",
			Nonce:     []byte("newnonce1234"),
		},
	}, nil
}

func (m *reencryptMockEncryption) ValidateKey(ctx context.Context, keyID string) error {
	return nil
}

// isLocationNotFound checks if the error is a location not found error.
func isLocationNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeLocationNotFound
	}
	return false
}

// TestReencryptTask tests the ReencryptTask type.
func TestReencryptTask(t *testing.T) {
	t.Run("validate nil task", func(t *testing.T) {
		var task *ReencryptTask
		err := task.Validate()
		if err != ErrReencryptTaskNil {
			t.Errorf("expected ErrReencryptTaskNil, got %v", err)
		}
	})

	t.Run("validate empty key", func(t *testing.T) {
		task := &ReencryptTask{
			Key:      "",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
		}
		err := task.Validate()
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("validate empty old key ID", func(t *testing.T) {
		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "",
			NewKeyID: "new/key",
		}
		err := task.Validate()
		if err == nil {
			t.Error("expected error for empty old key ID")
		}
	})

	t.Run("validate empty new key ID", func(t *testing.T) {
		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "old/key",
			NewKeyID: "",
		}
		err := task.Validate()
		if err == nil {
			t.Error("expected error for empty new key ID")
		}
	})

	t.Run("validate same key IDs", func(t *testing.T) {
		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "same/key",
			NewKeyID: "same/key",
		}
		err := task.Validate()
		if err == nil {
			t.Error("expected error for same key IDs")
		}
	})

	t.Run("validate valid task", func(t *testing.T) {
		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
		}
		err := task.Validate()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("clone nil task", func(t *testing.T) {
		var task *ReencryptTask
		clone := task.Clone()
		if clone != nil {
			t.Error("expected nil clone")
		}
	})

	t.Run("clone task", func(t *testing.T) {
		task := &ReencryptTask{
			Key:       "test/key",
			OldKeyID:  "old/key",
			NewKeyID:  "new/key",
			Stores:    []string{"store1", "store2"},
			Priority:  5,
			CreatedAt: time.Now().UnixNano(),
			Attempts:  2,
			LastError: "previous error",
		}
		clone := task.Clone()
		if clone.Key != task.Key {
			t.Error("key not cloned")
		}
		if clone.OldKeyID != task.OldKeyID {
			t.Error("old key ID not cloned")
		}
		if clone.NewKeyID != task.NewKeyID {
			t.Error("new key ID not cloned")
		}
		if len(clone.Stores) != len(task.Stores) {
			t.Error("stores not cloned")
		}
		// Verify deep copy
		clone.Stores[0] = "modified"
		if task.Stores[0] == "modified" {
			t.Error("stores not deeply cloned")
		}
	})
}

// TestReencryptProgress tests the ReencryptProgress type.
func TestReencryptProgress(t *testing.T) {
	t.Run("snapshot captures current state", func(t *testing.T) {
		progress := &ReencryptProgress{
			StartTime: time.Now().UnixNano(),
		}
		progress.TotalTasks.Store(100)
		progress.CompletedTasks.Store(50)
		progress.FailedTasks.Store(5)
		progress.BytesProcessed.Store(1024 * 1024)

		snapshot := progress.Snapshot()
		if snapshot.TotalTasks != 100 {
			t.Errorf("expected 100 total tasks, got %d", snapshot.TotalTasks)
		}
		if snapshot.CompletedTasks != 50 {
			t.Errorf("expected 50 completed tasks, got %d", snapshot.CompletedTasks)
		}
	})

	t.Run("percent complete with zero tasks", func(t *testing.T) {
		progress := &ReencryptProgress{}
		if progress.PercentComplete() != 0 {
			t.Error("expected 0% with no tasks")
		}
	})

	t.Run("percent complete calculation", func(t *testing.T) {
		progress := &ReencryptProgress{}
		progress.TotalTasks.Store(100)
		progress.CompletedTasks.Store(25)
		pct := progress.PercentComplete()
		if pct != 25.0 {
			t.Errorf("expected 25%%, got %.1f%%", pct)
		}
	})
}

// TestReencryptWorkerConfig tests the ReencryptWorkerConfig type.
func TestReencryptWorkerConfig(t *testing.T) {
	t.Run("default config is valid", func(t *testing.T) {
		cfg := DefaultReencryptWorkerConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("default config should be valid: %v", err)
		}
	})

	t.Run("nil config returns error", func(t *testing.T) {
		var cfg *ReencryptWorkerConfig
		if err := cfg.Validate(); err != ErrWorkerConfigNil {
			t.Errorf("expected ErrWorkerConfigNil, got %v", err)
		}
	})

	t.Run("nil worker config uses default", func(t *testing.T) {
		cfg := &ReencryptWorkerConfig{
			WorkerConfig: nil,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("should use default worker config: %v", err)
		}
	})

	t.Run("negative max bytes returns error", func(t *testing.T) {
		cfg := DefaultReencryptWorkerConfig()
		cfg.MaxBytesPerTask = -1
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for negative max bytes")
		}
	})
}

// TestNewReencryptWorker tests the constructor.
func TestNewReencryptWorker(t *testing.T) {
	t.Run("nil catalog returns error", func(t *testing.T) {
		backends := newReencryptMockBackends()
		encryption := newReencryptMockEncryption()
		_, err := NewReencryptWorker(nil, backends, encryption, nil, nil)
		if err != ErrCatalogManagerNil {
			t.Errorf("expected ErrCatalogManagerNil, got %v", err)
		}
	})

	t.Run("nil backends returns error", func(t *testing.T) {
		catalog := newReencryptMockCatalog()
		encryption := newReencryptMockEncryption()
		_, err := NewReencryptWorker(catalog, nil, encryption, nil, nil)
		if err != ErrBackendManagerNilForWorker {
			t.Errorf("expected ErrBackendManagerNilForWorker, got %v", err)
		}
	})

	t.Run("nil encryption returns error", func(t *testing.T) {
		catalog := newReencryptMockCatalog()
		backends := newReencryptMockBackends()
		_, err := NewReencryptWorker(catalog, backends, nil, nil, nil)
		if err != ErrEncryptionManagerNil {
			t.Errorf("expected ErrEncryptionManagerNil, got %v", err)
		}
	})

	t.Run("nil config uses defaults", func(t *testing.T) {
		catalog := newReencryptMockCatalog()
		backends := newReencryptMockBackends()
		encryption := newReencryptMockEncryption()
		worker, err := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if worker == nil {
			t.Fatal("expected non-nil worker")
		}
	})

	t.Run("valid parameters succeeds", func(t *testing.T) {
		catalog := newReencryptMockCatalog()
		backends := newReencryptMockBackends()
		encryption := newReencryptMockEncryption()
		cfg := DefaultReencryptWorkerConfig()
		worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if worker.Name() != "reencrypt" {
			t.Errorf("expected name 'reencrypt', got %q", worker.Name())
		}
	})
}

// TestReencryptWorker_StartStop tests worker lifecycle.
func TestReencryptWorker_StartStop(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	t.Run("start and stop", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)

		ctx := context.Background()
		if err := worker.Start(ctx); err != nil {
			t.Fatalf("start failed: %v", err)
		}
		if !worker.IsRunning() {
			t.Error("expected worker to be running")
		}

		if err := worker.Stop(); err != nil {
			t.Fatalf("stop failed: %v", err)
		}
		if worker.IsRunning() {
			t.Error("expected worker to be stopped")
		}
	})

	t.Run("double start returns error", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)

		ctx := context.Background()
		worker.Start(ctx)
		defer worker.Stop()

		err := worker.Start(ctx)
		if !IsWorkerAlreadyRunning(err) {
			t.Errorf("expected worker already running error, got %v", err)
		}
	})

	t.Run("stop without start is safe", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		if err := worker.Stop(); err != nil {
			t.Errorf("stop without start should not error: %v", err)
		}
	})
}

// TestReencryptWorker_Enqueue tests task enqueuing.
func TestReencryptWorker_Enqueue(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	t.Run("enqueue when not running returns error", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
		}
		err := worker.Enqueue(task)
		if !IsWorkerNotRunning(err) {
			t.Errorf("expected worker not running error, got %v", err)
		}
	})

	t.Run("enqueue invalid task returns error", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		task := &ReencryptTask{
			Key:      "",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
		}
		err := worker.Enqueue(task)
		if err == nil {
			t.Error("expected error for invalid task")
		}
	})

	t.Run("enqueue valid task succeeds", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
		}
		err := worker.Enqueue(task)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Check progress was updated
		if worker.Progress().TotalTasks.Load() != 1 {
			t.Error("expected total tasks to be 1")
		}
	})
}

// TestReencryptWorker_EnqueueBulk tests bulk enqueuing.
func TestReencryptWorker_EnqueueBulk(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	t.Run("bulk enqueue when not running returns error", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		err := worker.EnqueueBulk("old/key", "new/key", []string{"key1", "key2"}, 1)
		if !IsWorkerNotRunning(err) {
			t.Errorf("expected worker not running error, got %v", err)
		}
	})

	t.Run("bulk enqueue empty old key returns error", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		err := worker.EnqueueBulk("", "new/key", []string{"key1"}, 1)
		if err != ErrOldKeyIDEmpty {
			t.Errorf("expected ErrOldKeyIDEmpty, got %v", err)
		}
	})

	t.Run("bulk enqueue empty new key returns error", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		err := worker.EnqueueBulk("old/key", "", []string{"key1"}, 1)
		if err != ErrNewKeyIDEmpty {
			t.Errorf("expected ErrNewKeyIDEmpty, got %v", err)
		}
	})

	t.Run("bulk enqueue multiple keys", func(t *testing.T) {
		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		keys := []string{"key1", "key2", "key3"}
		err := worker.EnqueueBulk("old/key", "new/key", keys, 5)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Allow time for tasks to be queued
		time.Sleep(10 * time.Millisecond)

		if worker.Progress().TotalTasks.Load() != 3 {
			t.Errorf("expected 3 total tasks, got %d", worker.Progress().TotalTasks.Load())
		}
	})
}

// TestReencryptWorker_ProcessTask tests task processing.
func TestReencryptWorker_ProcessTask(t *testing.T) {
	t.Run("process task with no matching locations", func(t *testing.T) {
		catalog := newReencryptMockCatalog()
		backends := newReencryptMockBackends()
		encryption := newReencryptMockEncryption()

		// Add entry without matching encryption key
		entry := &CatalogEntry{
			Key: "test/key",
			PrimaryStore: StoreLocation{
				Store:     "store1",
				ObjectRef: "obj/ref",
				Encryption: &LocationEncryption{
					KeyID:     "different/key",
					Algorithm: "AES-256-GCM",
				},
			},
		}
		catalog.Put(context.Background(), entry)

		worker, _ := NewReencryptWorker(catalog, backends, encryption, nil, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
		}
		worker.Enqueue(task)

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Should complete without error (no matching locations to process)
		if worker.Progress().FailedTasks.Load() > 0 {
			t.Error("task should not fail when no locations match")
		}
	})

	t.Run("process task with matching location", func(t *testing.T) {
		catalog := newReencryptMockCatalog()
		backends := newReencryptMockBackends()
		encryption := newReencryptMockEncryption()

		// Set up storage
		storage := newReencryptMockStorage()
		storage.data["obj/ref"] = []byte("enc:original data")
		backends.RegisterStorage("store1", storage)

		// Add entry with matching encryption key
		entry := &CatalogEntry{
			Key: "test/key",
			PrimaryStore: StoreLocation{
				Store:     "store1",
				ObjectRef: "obj/ref",
				Encryption: &LocationEncryption{
					KeyID:     "old/key",
					Algorithm: "AES-256-GCM",
					Nonce:     []byte("testnonce123"),
				},
			},
		}
		catalog.Put(context.Background(), entry)

		cfg := DefaultReencryptWorkerConfig()
		cfg.VerifyAfterReencrypt = false // Disable verification for simplicity
		worker, _ := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
		}
		worker.Enqueue(task)

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Check that entry was updated
		updatedEntry, _ := catalog.Get(context.Background(), "test/key")
		if updatedEntry.PrimaryStore.Encryption.KeyID != "new/key" {
			t.Errorf("expected new key ID, got %s", updatedEntry.PrimaryStore.Encryption.KeyID)
		}

		// Check progress
		if worker.Progress().CompletedTasks.Load() != 1 {
			t.Errorf("expected 1 completed task, got %d", worker.Progress().CompletedTasks.Load())
		}
	})

	t.Run("process task with specific stores filter", func(t *testing.T) {
		catalog := newReencryptMockCatalog()
		backends := newReencryptMockBackends()
		encryption := newReencryptMockEncryption()

		// Set up storage
		storage1 := newReencryptMockStorage()
		storage1.data["obj/ref1"] = []byte("enc:data1")
		backends.RegisterStorage("store1", storage1)

		storage2 := newReencryptMockStorage()
		storage2.data["obj/ref2"] = []byte("enc:data2")
		backends.RegisterStorage("store2", storage2)

		// Add entry with replicas
		entry := &CatalogEntry{
			Key: "test/key",
			PrimaryStore: StoreLocation{
				Store:     "store1",
				ObjectRef: "obj/ref1",
				Encryption: &LocationEncryption{
					KeyID:     "old/key",
					Algorithm: "AES-256-GCM",
					Nonce:     []byte("nonce1234567"),
				},
			},
			Replicas: []StoreLocation{
				{
					Store:     "store2",
					ObjectRef: "obj/ref2",
					Encryption: &LocationEncryption{
						KeyID:     "old/key",
						Algorithm: "AES-256-GCM",
						Nonce:     []byte("nonce7654321"),
					},
				},
			},
		}
		catalog.Put(context.Background(), entry)

		cfg := DefaultReencryptWorkerConfig()
		cfg.VerifyAfterReencrypt = false
		worker, _ := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
		worker.Start(context.Background())
		defer worker.Stop()

		// Only process store1
		task := &ReencryptTask{
			Key:      "test/key",
			OldKeyID: "old/key",
			NewKeyID: "new/key",
			Stores:   []string{"store1"},
		}
		worker.Enqueue(task)

		// Wait for processing
		time.Sleep(200 * time.Millisecond)

		// Check that only store1 was updated
		updatedEntry, _ := catalog.Get(context.Background(), "test/key")
		if updatedEntry.PrimaryStore.Encryption.KeyID != "new/key" {
			t.Error("store1 should have been updated")
		}
		if updatedEntry.Replicas[0].Encryption.KeyID != "old/key" {
			t.Error("store2 should not have been updated")
		}
	})
}

// TestReencryptErrors tests error type functions.
func TestReencryptErrors(t *testing.T) {
	t.Run("NewReencryptError creates correct error", func(t *testing.T) {
		err := NewReencryptError("test/key", errors.New("cause"))
		if err.Code != ErrCodeReencryptFailed {
			t.Errorf("expected code %s, got %s", ErrCodeReencryptFailed, err.Code)
		}
		if err.Key != "test/key" {
			t.Errorf("expected key test/key, got %s", err.Key)
		}
	})

	t.Run("NewReencryptTaskInvalidError creates correct error", func(t *testing.T) {
		err := NewReencryptTaskInvalidError("test reason")
		if err.Code != ErrCodeReencryptTaskInvalid {
			t.Errorf("expected code %s, got %s", ErrCodeReencryptTaskInvalid, err.Code)
		}
	})

	t.Run("IsReencryptError detects errors", func(t *testing.T) {
		if IsReencryptError(nil) {
			t.Error("expected false for nil")
		}
		if IsReencryptError(errors.New("random")) {
			t.Error("expected false for non-catalog error")
		}
		if !IsReencryptError(NewReencryptError("key", nil)) {
			t.Error("expected true for reencrypt error")
		}
		if !IsReencryptError(NewReencryptTaskInvalidError("reason")) {
			t.Error("expected true for task invalid error")
		}
	})

	t.Run("IsQueueFull detects queue full errors", func(t *testing.T) {
		if IsQueueFull(nil) {
			t.Error("expected false for nil")
		}
		if IsQueueFull(errors.New("random")) {
			t.Error("expected false for non-catalog error")
		}
		if !IsQueueFull(NewQueueFullError("test")) {
			t.Error("expected true for queue full error")
		}
	})
}

// TestUpdateLocationEncryption tests the updateLocationEncryption function.
func TestUpdateLocationEncryption(t *testing.T) {
	t.Run("nil entry returns error", func(t *testing.T) {
		err := updateLocationEncryption(nil, "store", &LocationEncryption{})
		if err != ErrEntryNil {
			t.Errorf("expected ErrEntryNil, got %v", err)
		}
	})

	t.Run("update primary store", func(t *testing.T) {
		entry := &CatalogEntry{
			Key: "test/key",
			PrimaryStore: StoreLocation{
				Store: "primary",
				Encryption: &LocationEncryption{
					KeyID: "old/key",
				},
			},
		}
		newEnc := &LocationEncryption{
			KeyID:     "new/key",
			Algorithm: "AES-256-GCM",
		}

		err := updateLocationEncryption(entry, "primary", newEnc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.PrimaryStore.Encryption.KeyID != "new/key" {
			t.Error("primary store encryption not updated")
		}
	})

	t.Run("update replica", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:          "test/key",
			PrimaryStore: StoreLocation{Store: "primary"},
			Replicas: []StoreLocation{
				{Store: "replica1", Encryption: &LocationEncryption{KeyID: "old/key"}},
			},
		}
		newEnc := &LocationEncryption{KeyID: "new/key"}

		err := updateLocationEncryption(entry, "replica1", newEnc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.Replicas[0].Encryption.KeyID != "new/key" {
			t.Error("replica encryption not updated")
		}
	})

	t.Run("update archive store", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:          "test/key",
			PrimaryStore: StoreLocation{Store: "primary"},
			ArchiveStore: &StoreLocation{Store: "archive", Encryption: &LocationEncryption{KeyID: "old/key"}},
		}
		newEnc := &LocationEncryption{KeyID: "new/key"}

		err := updateLocationEncryption(entry, "archive", newEnc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if entry.ArchiveStore.Encryption.KeyID != "new/key" {
			t.Error("archive store encryption not updated")
		}
	})

	t.Run("store not found returns error", func(t *testing.T) {
		entry := &CatalogEntry{
			Key:          "test/key",
			PrimaryStore: StoreLocation{Store: "primary"},
		}
		newEnc := &LocationEncryption{KeyID: "new/key"}

		err := updateLocationEncryption(entry, "nonexistent", newEnc)
		if !isLocationNotFound(err) {
			t.Errorf("expected location not found error, got %v", err)
		}
	})
}

// TestContainsStore tests the containsStore helper.
func TestContainsStore(t *testing.T) {
	stores := []string{"store1", "store2", "store3"}

	if !containsStore(stores, "store1") {
		t.Error("expected true for store1")
	}
	if !containsStore(stores, "store2") {
		t.Error("expected true for store2")
	}
	if containsStore(stores, "store4") {
		t.Error("expected false for store4")
	}
	if containsStore(nil, "store1") {
		t.Error("expected false for nil stores")
	}
	if containsStore([]string{}, "store1") {
		t.Error("expected false for empty stores")
	}
}

// TestIsWorkerNotRunning tests the worker not running error check.
func TestIsWorkerNotRunning(t *testing.T) {
	err := NewWorkerNotRunningError("test")
	if err.Code != ErrCodeWorkerNotRunning {
		t.Errorf("expected code %s, got %s", ErrCodeWorkerNotRunning, err.Code)
	}
}

// IsWorkerNotRunning checks if error indicates worker not running.
func IsWorkerNotRunning(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeWorkerNotRunning
	}
	return false
}
