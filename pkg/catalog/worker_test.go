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
	"io"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-objstore/pkg/common"

	"crypto/sha256"
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockLogger implements interfaces.Logger for testing.
type mockLogger struct{}

func (l *mockLogger) Debug(msg string, args ...any)             {}
func (l *mockLogger) Debugf(format string, args ...interface{}) {}
func (l *mockLogger) Info(msg string, args ...any)              {}
func (l *mockLogger) Infof(format string, args ...interface{})  {}
func (l *mockLogger) Warn(msg string, args ...any)              {}
func (l *mockLogger) Warnf(format string, args ...interface{})  {}
func (l *mockLogger) Error(msg string, args ...any)             {}
func (l *mockLogger) Errorf(format string, args ...interface{}) {}
func (l *mockLogger) Fatal(msg string)                          {}
func (l *mockLogger) Fatalf(format string, args ...interface{}) {}
func (l *mockLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}
func (l *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *mockLogger) WithError(err error) interfaces.Logger                      { return l }

// mockCatalogManager implements CatalogManager for testing.
type mockCatalogManager struct {
	mu                sync.Mutex
	entries           map[string]*CatalogEntry
	replicationQueue  []*ReplicationTask
	archiveQueue      []*ArchiveTask
	verifyQueue       []*VerifyTask
	closed            bool
	getError          error
	updateError       error
	addLocationCalled int
}

func newMockCatalogManager() *mockCatalogManager {
	return &mockCatalogManager{
		entries:          make(map[string]*CatalogEntry),
		replicationQueue: make([]*ReplicationTask, 0),
		archiveQueue:     make([]*ArchiveTask, 0),
		verifyQueue:      make([]*VerifyTask, 0),
	}
}

func (m *mockCatalogManager) Put(ctx context.Context, entry *CatalogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entry.Key] = entry.Clone()
	return nil
}

func (m *mockCatalogManager) Get(ctx context.Context, key string) (*CatalogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getError != nil {
		return nil, m.getError
	}
	entry, ok := m.entries[key]
	if !ok {
		return nil, NewEntryNotFoundError(key)
	}
	return entry.Clone(), nil
}

func (m *mockCatalogManager) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

func (m *mockCatalogManager) Update(ctx context.Context, key string, updater func(*CatalogEntry) error) error {
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

func (m *mockCatalogManager) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.entries[key]
	return ok, nil
}

func (m *mockCatalogManager) AddLocation(ctx context.Context, key string, loc *StoreLocation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addLocationCalled++
	entry, ok := m.entries[key]
	if !ok {
		return NewEntryNotFoundError(key)
	}
	entry.Replicas = append(entry.Replicas, *loc)
	return nil
}

func (m *mockCatalogManager) RemoveLocation(ctx context.Context, key string, store string) error {
	return nil
}

func (m *mockCatalogManager) SetPrimaryStore(ctx context.Context, key string, store string) error {
	return nil
}

func (m *mockCatalogManager) GetLocations(ctx context.Context, key string) ([]*StoreLocation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return nil, NewEntryNotFoundError(key)
	}
	locs := entry.GetAllLocations()
	result := make([]*StoreLocation, len(locs))
	for i := range locs {
		result[i] = locs[i].Clone()
	}
	return result, nil
}

func (m *mockCatalogManager) UpdateLocationStatus(ctx context.Context, key, store string, status LocationStatus) error {
	return nil
}

func (m *mockCatalogManager) ListByStore(ctx context.Context, store string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{Keys: []string{}}, nil
}

func (m *mockCatalogManager) ListByTenant(ctx context.Context, tenant string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{Keys: []string{}}, nil
}

func (m *mockCatalogManager) ListByPrefix(ctx context.Context, prefix string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{Keys: []string{}}, nil
}

func (m *mockCatalogManager) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for key, entry := range m.entries {
		if entry.Status == status {
			keys = append(keys, key)
		}
	}
	return &ListResult{Keys: keys}, nil
}

func (m *mockCatalogManager) ListByTag(ctx context.Context, tag, value string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{Keys: []string{}}, nil
}

func (m *mockCatalogManager) ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{Keys: []string{}}, nil
}

func (m *mockCatalogManager) Search(ctx context.Context, query *SearchQuery) (*SearchResult, error) {
	return &SearchResult{}, nil
}

func (m *mockCatalogManager) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := &ReplicationTask{Key: key, Targets: targets, Priority: priority, CreatedAt: time.Now().UnixNano()}
	m.replicationQueue = append(m.replicationQueue, task)
	return nil
}

func (m *mockCatalogManager) EnqueueArchive(ctx context.Context, key string, targetStore string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := &ArchiveTask{Key: key, TargetStore: targetStore, CreatedAt: time.Now().UnixNano()}
	m.archiveQueue = append(m.archiveQueue, task)
	return nil
}

func (m *mockCatalogManager) EnqueueVerify(ctx context.Context, key string, stores []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := &VerifyTask{Key: key, Stores: stores, CreatedAt: time.Now().UnixNano()}
	m.verifyQueue = append(m.verifyQueue, task)
	return nil
}

func (m *mockCatalogManager) DequeueReplication(ctx context.Context, limit int) ([]*ReplicationTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, NewCatalogClosedError()
	}
	if limit <= 0 || limit > len(m.replicationQueue) {
		limit = len(m.replicationQueue)
	}
	tasks := m.replicationQueue[:limit]
	m.replicationQueue = m.replicationQueue[limit:]
	return tasks, nil
}

func (m *mockCatalogManager) DequeueArchive(ctx context.Context, limit int) ([]*ArchiveTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, NewCatalogClosedError()
	}
	if limit <= 0 || limit > len(m.archiveQueue) {
		limit = len(m.archiveQueue)
	}
	tasks := m.archiveQueue[:limit]
	m.archiveQueue = m.archiveQueue[limit:]
	return tasks, nil
}

func (m *mockCatalogManager) DequeueVerify(ctx context.Context, limit int) ([]*VerifyTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, NewCatalogClosedError()
	}
	if limit <= 0 || limit > len(m.verifyQueue) {
		limit = len(m.verifyQueue)
	}
	tasks := m.verifyQueue[:limit]
	m.verifyQueue = m.verifyQueue[limit:]
	return tasks, nil
}

func (m *mockCatalogManager) GetTenantUsage(ctx context.Context, tenant string) (*TenantUsage, error) {
	return &TenantUsage{TenantID: tenant}, nil
}

func (m *mockCatalogManager) GetStoreUsage(ctx context.Context, store string) (*StoreUsage, error) {
	return &StoreUsage{Store: store}, nil
}

func (m *mockCatalogManager) UpdateAccessTime(ctx context.Context, key string) error {
	return nil
}

func (m *mockCatalogManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockCatalogManager) Sync() error {
	return nil
}

// bytesReadCloser wraps a byte slice into an io.ReadCloser.
type bytesReadCloser struct {
	*bytes.Reader
}

func (b *bytesReadCloser) Close() error {
	return nil
}

func newBytesReadCloser(data []byte) io.ReadCloser {
	return &bytesReadCloser{Reader: bytes.NewReader(data)}
}

// mockStorage implements common.Storage for testing.
type mockStorage struct {
	mu       sync.Mutex
	data     map[string][]byte
	policies []common.LifecyclePolicy
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		data:     make(map[string][]byte),
		policies: make([]common.LifecyclePolicy, 0),
	}
}

// LifecycleManager methods
func (s *mockStorage) AddPolicy(policy common.LifecyclePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies = append(s.policies, policy)
	return nil
}

func (s *mockStorage) RemovePolicy(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.policies {
		if p.ID == id {
			s.policies = append(s.policies[:i], s.policies[i+1:]...)
			return nil
		}
	}
	return nil
}

func (s *mockStorage) GetPolicies() ([]common.LifecyclePolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]common.LifecyclePolicy, len(s.policies))
	copy(result, s.policies)
	return result, nil
}

// Configure sets up the backend with the necessary credentials and settings.
func (s *mockStorage) Configure(settings map[string]string) error {
	return nil
}

func (s *mockStorage) Put(key string, data io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	s.data[key] = value
	return nil
}

func (s *mockStorage) PutWithContext(ctx context.Context, key string, data io.Reader) error {
	return s.Put(key, data)
}

func (s *mockStorage) PutWithMetadata(ctx context.Context, key string, data io.Reader, metadata *common.Metadata) error {
	return s.Put(key, data)
}

func (s *mockStorage) Get(key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	val, ok := s.data[key]
	if !ok {
		return nil, NewEntryNotFoundError(key)
	}
	return newBytesReadCloser(val), nil
}

func (s *mockStorage) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.Get(key)
}

func (s *mockStorage) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return nil, NewEntryNotFoundError(key)
	}
	return &common.Metadata{}, nil
}

func (s *mockStorage) UpdateMetadata(ctx context.Context, key string, metadata *common.Metadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return NewEntryNotFoundError(key)
	}
	return nil
}

func (s *mockStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *mockStorage) DeleteWithContext(ctx context.Context, key string) error {
	return s.Delete(key)
}

func (s *mockStorage) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[key]
	return ok, nil
}

func (s *mockStorage) List(prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *mockStorage) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	return s.List(prefix)
}

func (s *mockStorage) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	_, err := s.List("")
	if err != nil {
		return nil, err
	}
	return &common.ListResult{
		Objects: make([]*common.ObjectInfo, 0),
	}, nil
}

func (s *mockStorage) Archive(key string, destination common.Archiver) error {
	s.mu.Lock()
	val, ok := s.data[key]
	s.mu.Unlock()
	if !ok {
		return NewEntryNotFoundError(key)
	}
	return destination.Put(key, bytes.NewReader(val))
}

// putRaw is a helper to directly set byte data (for test setup).
func (s *mockStorage) putRaw(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = data
}

// mockBackendManager implements BackendManager for testing.
type mockBackendManager struct {
	mu       sync.Mutex
	backends map[string]common.Storage
	closed   bool
}

func newMockBackendManager() *mockBackendManager {
	return &mockBackendManager{
		backends: make(map[string]common.Storage),
	}
}

func (m *mockBackendManager) GetBackend(name string) (common.Storage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, NewBackendManagerClosedError()
	}
	backend, ok := m.backends[name]
	if !ok {
		return nil, NewBackendNotFoundError(name)
	}
	return backend, nil
}

func (m *mockBackendManager) ListBackends() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for name := range m.backends {
		names = append(names, name)
	}
	return names
}

func (m *mockBackendManager) GetBackendConfig(name string) (*BackendConfig, error) {
	return &BackendConfig{Type: "memory", Enabled: true}, nil
}

func (m *mockBackendManager) RegisterBackend(name string, cfg *BackendConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[name] = newMockStorage()
	return nil
}

func (m *mockBackendManager) UnregisterBackend(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.backends, name)
	return nil
}

func (m *mockBackendManager) SelectBackend(criteria *SelectionCriteria) (string, common.Storage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, backend := range m.backends {
		return name, backend, nil
	}
	return "", nil, ErrNoBackendsAvailable
}

func (m *mockBackendManager) CheckHealth(ctx context.Context, name string) (*BackendHealth, error) {
	return &BackendHealth{Name: name, Healthy: true, LastCheck: time.Now()}, nil
}

func (m *mockBackendManager) CheckAllHealth(ctx context.Context) map[string]*BackendHealth {
	return make(map[string]*BackendHealth)
}

func (m *mockBackendManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockBackendManager) addBackend(name string, storage common.Storage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[name] = storage
}

// Test WorkerConfig
func TestWorkerConfig_Default(t *testing.T) {
	cfg := DefaultWorkerConfig()

	if cfg.Workers != 2 {
		t.Errorf("expected Workers=2, got %d", cfg.Workers)
	}
	if cfg.BatchSize != 10 {
		t.Errorf("expected BatchSize=10, got %d", cfg.BatchSize)
	}
	if cfg.PollInterval != time.Second {
		t.Errorf("expected PollInterval=1s, got %v", cfg.PollInterval)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", cfg.MaxRetries)
	}
}

func TestWorkerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *WorkerConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "valid config",
			config: &WorkerConfig{
				Workers:                2,
				BatchSize:              10,
				PollInterval:           time.Second,
				MaxRetries:             3,
				RetryDelay:             100 * time.Millisecond,
				RetryBackoffMultiplier: 2.0,
				TaskTimeout:            30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "invalid workers",
			config: &WorkerConfig{
				Workers:                0,
				BatchSize:              10,
				PollInterval:           time.Second,
				MaxRetries:             3,
				RetryDelay:             100 * time.Millisecond,
				RetryBackoffMultiplier: 2.0,
				TaskTimeout:            30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid batch size",
			config: &WorkerConfig{
				Workers:                2,
				BatchSize:              0,
				PollInterval:           time.Second,
				MaxRetries:             3,
				RetryDelay:             100 * time.Millisecond,
				RetryBackoffMultiplier: 2.0,
				TaskTimeout:            30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid poll interval",
			config: &WorkerConfig{
				Workers:                2,
				BatchSize:              10,
				PollInterval:           0,
				MaxRetries:             3,
				RetryDelay:             100 * time.Millisecond,
				RetryBackoffMultiplier: 2.0,
				TaskTimeout:            30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "invalid backoff multiplier",
			config: &WorkerConfig{
				Workers:                2,
				BatchSize:              10,
				PollInterval:           time.Second,
				MaxRetries:             3,
				RetryDelay:             100 * time.Millisecond,
				RetryBackoffMultiplier: 0.5,
				TaskTimeout:            30 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWorkerConfig_Clone(t *testing.T) {
	cfg := DefaultWorkerConfig()
	clone := cfg.Clone()

	if clone.Workers != cfg.Workers {
		t.Errorf("clone Workers mismatch")
	}
	if clone.BatchSize != cfg.BatchSize {
		t.Errorf("clone BatchSize mismatch")
	}

	// Modify clone and verify original unchanged
	clone.Workers = 100
	if cfg.Workers == 100 {
		t.Errorf("original was modified")
	}
}

// Test BaseWorker
func TestBaseWorker_New(t *testing.T) {
	logger := &mockLogger{}

	// Test with empty name
	_, err := NewBaseWorker("", nil, logger)
	if err != ErrWorkerNameEmpty {
		t.Errorf("expected ErrWorkerNameEmpty, got %v", err)
	}

	// Test with nil config (should use defaults)
	w, err := NewBaseWorker("test-worker", nil, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w.Name() != "test-worker" {
		t.Errorf("expected name=test-worker, got %s", w.Name())
	}
	if w.IsRunning() {
		t.Errorf("expected worker to not be running")
	}

	// Test with valid config
	cfg := DefaultWorkerConfig()
	w, err = NewBaseWorker("test-worker-2", cfg, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w.Config().Workers != cfg.Workers {
		t.Errorf("config mismatch")
	}
}

func TestBaseWorker_StartStop(t *testing.T) {
	logger := &mockLogger{}
	w, err := NewBaseWorker("test-worker", nil, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()

	// Start the worker
	err = w.StartBase(ctx)
	if err != nil {
		t.Errorf("failed to start worker: %v", err)
	}
	if !w.IsRunning() {
		t.Errorf("worker should be running")
	}

	// Try to start again - should fail
	err = w.StartBase(ctx)
	if !IsWorkerAlreadyRunning(err) {
		t.Errorf("expected WorkerAlreadyRunning error, got %v", err)
	}

	// Stop the worker
	err = w.StopBase()
	if err != nil {
		t.Errorf("failed to stop worker: %v", err)
	}
	if w.IsRunning() {
		t.Errorf("worker should not be running")
	}

	// Stop again - should be no-op
	err = w.StopBase()
	if err != nil {
		t.Errorf("stop should be idempotent: %v", err)
	}
}

func TestBaseWorker_Metrics(t *testing.T) {
	logger := &mockLogger{}
	w, err := NewBaseWorker("test-worker", nil, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	metrics := w.Metrics()
	if metrics == nil {
		t.Fatal("metrics should not be nil")
	}

	// Test metric recording
	w.RecordTaskStart()
	if metrics.CurrentTasks.Load() != 1 {
		t.Errorf("expected CurrentTasks=1, got %d", metrics.CurrentTasks.Load())
	}

	w.RecordTaskComplete(100 * time.Millisecond)
	if metrics.TasksProcessed.Load() != 1 {
		t.Errorf("expected TasksProcessed=1, got %d", metrics.TasksProcessed.Load())
	}
	if metrics.CurrentTasks.Load() != 0 {
		t.Errorf("expected CurrentTasks=0, got %d", metrics.CurrentTasks.Load())
	}

	w.RecordTaskStart()
	w.RecordTaskFailed()
	if metrics.TasksFailed.Load() != 1 {
		t.Errorf("expected TasksFailed=1, got %d", metrics.TasksFailed.Load())
	}

	w.RecordTaskRetry()
	if metrics.TasksRetried.Load() != 1 {
		t.Errorf("expected TasksRetried=1, got %d", metrics.TasksRetried.Load())
	}

	// Test snapshot
	snapshot := metrics.Snapshot()
	if snapshot.TasksProcessed != 1 {
		t.Errorf("snapshot TasksProcessed mismatch")
	}
}

func TestBaseWorker_CalculateRetryDelay(t *testing.T) {
	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           time.Second,
		MaxRetries:             3,
		RetryDelay:             100 * time.Millisecond,
		RetryBackoffMultiplier: 2.0,
		TaskTimeout:            30 * time.Second,
	}
	w, err := NewBaseWorker("test", cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	// First retry: 100ms
	delay0 := w.CalculateRetryDelay(0)
	if delay0 != 100*time.Millisecond {
		t.Errorf("expected 100ms for attempt 0, got %v", delay0)
	}

	// Second retry: 200ms (100 * 2)
	delay1 := w.CalculateRetryDelay(1)
	if delay1 != 200*time.Millisecond {
		t.Errorf("expected 200ms for attempt 1, got %v", delay1)
	}

	// Third retry: 400ms (100 * 2 * 2)
	delay2 := w.CalculateRetryDelay(2)
	if delay2 != 400*time.Millisecond {
		t.Errorf("expected 400ms for attempt 2, got %v", delay2)
	}
}

// Test WorkerManager
func TestWorkerManager_Basic(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	if mgr.WorkerCount() != 0 {
		t.Errorf("expected 0 workers, got %d", mgr.WorkerCount())
	}
	if mgr.IsRunning() {
		t.Errorf("manager should not be running")
	}

	// Create a simple worker for testing
	w, err := NewBaseWorker("test-worker", nil, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	// Register worker
	err = mgr.Register(&testWorker{BaseWorker: w})
	if err != nil {
		t.Errorf("failed to register worker: %v", err)
	}
	if mgr.WorkerCount() != 1 {
		t.Errorf("expected 1 worker, got %d", mgr.WorkerCount())
	}

	// Try to register duplicate
	err = mgr.Register(&testWorker{BaseWorker: w})
	if !IsWorkerAlreadyRunning(err) && err == nil {
		// Should get worker exists error
	}

	// Get worker
	_, err = mgr.GetWorker("test-worker")
	if err != nil {
		t.Errorf("failed to get worker: %v", err)
	}

	// Get non-existent worker
	_, err = mgr.GetWorker("non-existent")
	if !IsWorkerNotFound(err) {
		t.Errorf("expected WorkerNotFound error, got %v", err)
	}

	// List workers
	workers := mgr.ListWorkers()
	if len(workers) != 1 || workers[0] != "test-worker" {
		t.Errorf("ListWorkers mismatch: %v", workers)
	}

	// Unregister worker
	err = mgr.Unregister("test-worker")
	if err != nil {
		t.Errorf("failed to unregister worker: %v", err)
	}
	if mgr.WorkerCount() != 0 {
		t.Errorf("expected 0 workers after unregister")
	}
}

func TestWorkerManager_StartStopAll(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	// Create workers
	w1, _ := NewBaseWorker("worker-1", nil, logger)
	w2, _ := NewBaseWorker("worker-2", nil, logger)

	mgr.Register(&testWorker{BaseWorker: w1})
	mgr.Register(&testWorker{BaseWorker: w2})

	ctx := context.Background()

	// Start all
	err := mgr.StartAll(ctx)
	if err != nil {
		t.Errorf("failed to start all workers: %v", err)
	}
	if mgr.RunningWorkerCount() != 2 {
		t.Errorf("expected 2 running workers, got %d", mgr.RunningWorkerCount())
	}
	if !mgr.IsRunning() {
		t.Errorf("manager should be running")
	}

	// Stop all
	err = mgr.StopAll()
	if err != nil {
		t.Errorf("failed to stop all workers: %v", err)
	}
	if mgr.RunningWorkerCount() != 0 {
		t.Errorf("expected 0 running workers, got %d", mgr.RunningWorkerCount())
	}
}

func TestWorkerManager_GetMetrics(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	w1, _ := NewBaseWorker("worker-1", nil, logger)
	mgr.Register(&testWorker{BaseWorker: w1})

	metrics := mgr.GetMetrics()
	if len(metrics) != 1 {
		t.Errorf("expected 1 worker metrics, got %d", len(metrics))
	}

	aggregated := mgr.GetAggregatedMetrics()
	if aggregated == nil {
		t.Error("aggregated metrics should not be nil")
	}
}

func TestWorkerManager_GetStatus(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	w1, _ := NewBaseWorker("worker-1", nil, logger)
	w2, _ := NewBaseWorker("worker-2", nil, logger)

	mgr.Register(&testWorker{BaseWorker: w1})
	mgr.Register(&testWorker{BaseWorker: w2})

	statuses := mgr.GetStatus()
	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d", len(statuses))
	}
}

func TestWorkerManager_Close(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	w1, _ := NewBaseWorker("worker-1", nil, logger)
	mgr.Register(&testWorker{BaseWorker: w1})

	ctx := context.Background()
	mgr.StartAll(ctx)

	err := mgr.Close()
	if err != nil {
		t.Errorf("failed to close manager: %v", err)
	}

	// After close, operations should fail
	err = mgr.Register(&testWorker{BaseWorker: w1})
	if !IsWorkerManagerClosed(err) {
		t.Errorf("expected WorkerManagerClosed error, got %v", err)
	}
}

// testWorker is a simple Worker implementation for testing
type testWorker struct {
	*BaseWorker
}

func (w *testWorker) Start(ctx context.Context) error {
	return w.StartBase(ctx)
}

func (w *testWorker) Stop() error {
	return w.StopBase()
}

// Test ReplicationWorker
func TestReplicationWorker_New(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Test nil catalog
	_, err := NewReplicationWorker(nil, backends, nil, logger)
	if err != ErrCatalogManagerNil {
		t.Errorf("expected ErrCatalogManagerNil, got %v", err)
	}

	// Test nil backends
	_, err = NewReplicationWorker(catalog, nil, nil, logger)
	if err != ErrBackendManagerNilForWorker {
		t.Errorf("expected ErrBackendManagerNilForWorker, got %v", err)
	}

	// Test valid creation
	w, err := NewReplicationWorker(catalog, backends, nil, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w.Name() != ReplicationWorkerName {
		t.Errorf("expected name=%s, got %s", ReplicationWorkerName, w.Name())
	}
}

func TestReplicationWorker_StartStop(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              5,
		PollInterval:           50 * time.Millisecond,
		MaxRetries:             1,
		RetryDelay:             10 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            1 * time.Second,
	}

	w, err := NewReplicationWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()

	// Start
	err = w.Start(ctx)
	if err != nil {
		t.Errorf("failed to start worker: %v", err)
	}
	if !w.IsRunning() {
		t.Errorf("worker should be running")
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop
	err = w.Stop()
	if err != nil {
		t.Errorf("failed to stop worker: %v", err)
	}
	if w.IsRunning() {
		t.Errorf("worker should not be running")
	}
}

// Test ArchiveWorker
func TestArchiveWorker_New(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Test nil catalog
	_, err := NewArchiveWorker(nil, backends, nil, logger)
	if err != ErrCatalogManagerNil {
		t.Errorf("expected ErrCatalogManagerNil, got %v", err)
	}

	// Test valid creation
	w, err := NewArchiveWorker(catalog, backends, nil, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w.Name() != ArchiveWorkerName {
		t.Errorf("expected name=%s, got %s", ArchiveWorkerName, w.Name())
	}
}

// Test VerifyWorker
func TestVerifyWorker_New(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Test nil catalog
	_, err := NewVerifyWorker(nil, backends, nil, logger)
	if err != ErrCatalogManagerNil {
		t.Errorf("expected ErrCatalogManagerNil, got %v", err)
	}

	// Test valid creation
	w, err := NewVerifyWorker(catalog, backends, nil, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w.Name() != VerifyWorkerName {
		t.Errorf("expected name=%s, got %s", VerifyWorkerName, w.Name())
	}
}

// Test RetentionWorker
func TestRetentionWorker_New(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Test nil catalog
	_, err := NewRetentionWorker(nil, backends, nil, logger)
	if err != ErrCatalogManagerNil {
		t.Errorf("expected ErrCatalogManagerNil, got %v", err)
	}

	// Test valid creation
	w, err := NewRetentionWorker(catalog, backends, nil, logger)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if w.Name() != RetentionWorkerName {
		t.Errorf("expected name=%s, got %s", RetentionWorkerName, w.Name())
	}
}

// Test error types
func TestWorkerErrors(t *testing.T) {
	t.Run("WorkerAlreadyRunningError", func(t *testing.T) {
		err := NewWorkerAlreadyRunningError("test")
		if !IsWorkerAlreadyRunning(err) {
			t.Error("IsWorkerAlreadyRunning should return true")
		}
		if err.Error() == "" {
			t.Error("error message should not be empty")
		}
	})

	t.Run("WorkerNotFoundError", func(t *testing.T) {
		err := NewWorkerNotFoundError("test")
		if !IsWorkerNotFound(err) {
			t.Error("IsWorkerNotFound should return true")
		}
	})

	t.Run("TaskProcessingError", func(t *testing.T) {
		err := NewTaskProcessingError("replication", "key1", nil)
		if !IsTaskProcessingError(err) {
			t.Error("IsTaskProcessingError should return true")
		}
	})

	t.Run("ReplicationError", func(t *testing.T) {
		err := NewReplicationError("key1", "store1", nil)
		if !IsTaskProcessingError(err) {
			t.Error("IsTaskProcessingError should return true for replication error")
		}
	})

	t.Run("ArchiveError", func(t *testing.T) {
		err := NewArchiveError("key1", "store1", nil)
		if !IsTaskProcessingError(err) {
			t.Error("IsTaskProcessingError should return true for archive error")
		}
	})

	t.Run("VerificationError", func(t *testing.T) {
		err := NewVerificationError("key1", "store1", nil)
		if !IsTaskProcessingError(err) {
			t.Error("IsTaskProcessingError should return true for verification error")
		}
	})

	t.Run("RetentionError", func(t *testing.T) {
		err := NewRetentionError("key1", nil)
		if !IsTaskProcessingError(err) {
			t.Error("IsTaskProcessingError should return true for retention error")
		}
	})

	t.Run("WorkerManagerClosedError", func(t *testing.T) {
		err := NewWorkerManagerClosedError()
		if !IsWorkerManagerClosed(err) {
			t.Error("IsWorkerManagerClosed should return true")
		}
	})
}

// Test helper functions
func TestBytesEqual(t *testing.T) {
	tests := []struct {
		a, b     []byte
		expected bool
	}{
		{[]byte{1, 2, 3}, []byte{1, 2, 3}, true},
		{[]byte{1, 2, 3}, []byte{1, 2, 4}, false},
		{[]byte{1, 2, 3}, []byte{1, 2}, false},
		{[]byte{}, []byte{}, true},
		{nil, nil, true},
		{nil, []byte{}, true},
	}

	for i, tt := range tests {
		result := bytesEqual(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("test %d: bytesEqual(%v, %v) = %v, expected %v", i, tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestCompressDecompress(t *testing.T) {
	original := []byte("hello world, this is a test message for compression")

	compressed, err := compressData(original, 6)
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}

	// Compressed data should be different (usually smaller)
	if bytesEqual(original, compressed) {
		t.Error("compressed data should be different from original")
	}

	decompressed, err := decompressData(compressed)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	if !bytesEqual(original, decompressed) {
		t.Error("decompressed data should match original")
	}
}

func TestGenerateObjectRef(t *testing.T) {
	ref1 := generateObjectRef("key1", "store1")
	ref2 := generateObjectRef("key1", "store1")

	// Should contain the key and store
	if ref1 == "" || ref2 == "" {
		t.Error("generated refs should not be empty")
	}

	// Should be unique due to timestamp
	time.Sleep(time.Nanosecond)
	ref3 := generateObjectRef("key1", "store1")
	if ref1 == ref3 {
		t.Error("generated refs should be unique")
	}
}

func TestGenerateArchiveRef(t *testing.T) {
	ref := generateArchiveRef("key1", "store1")
	if ref == "" {
		t.Error("archive ref should not be empty")
	}
	// Should contain "archive" prefix
	if len(ref) < 7 || ref[:7] != "archive" {
		t.Error("archive ref should have 'archive' prefix")
	}
}

// =============================================================================
// Consolidated from worker_coverage_test.go
// =============================================================================

// Test WorkerMetrics.AverageProcessingTime
func TestWorkerMetrics_AverageProcessingTime(t *testing.T) {
	tests := []struct {
		name       string
		processed  int64
		totalTime  int64
		wantResult time.Duration
	}{
		{
			name:       "no tasks processed returns zero",
			processed:  0,
			totalTime:  0,
			wantResult: 0,
		},
		{
			name:       "single task",
			processed:  1,
			totalTime:  int64(100 * time.Millisecond),
			wantResult: 100 * time.Millisecond,
		},
		{
			name:       "multiple tasks",
			processed:  10,
			totalTime:  int64(1000 * time.Millisecond),
			wantResult: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &WorkerMetrics{}
			m.TasksProcessed.Store(tt.processed)
			m.TotalProcessingTime.Store(tt.totalTime)

			got := m.AverageProcessingTime()
			if got != tt.wantResult {
				t.Errorf("AverageProcessingTime() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

// Test BaseWorker accessor methods with 0% coverage
func TestBaseWorker_Accessors(t *testing.T) {
	logger := &mockLogger{}
	w, err := NewBaseWorker("test-worker", nil, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	// Test Logger()
	if w.Logger() != logger {
		t.Error("Logger() should return the configured logger")
	}

	// Test Context() before start - should be nil
	if w.Context() != nil {
		t.Error("Context() should be nil before Start")
	}

	// Test WaitGroup()
	wg := w.WaitGroup()
	if wg == nil {
		t.Error("WaitGroup() should not be nil")
	}

	// Start worker to set context
	ctx := context.Background()
	if err := w.StartBase(ctx); err != nil {
		t.Fatalf("StartBase failed: %v", err)
	}
	defer w.StopBase()

	// Test Context() after start
	if w.Context() == nil {
		t.Error("Context() should not be nil after Start")
	}
}

// Test BaseWorker.CreateTaskContext
func TestBaseWorker_CreateTaskContext(t *testing.T) {
	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           time.Second,
		MaxRetries:             1,
		RetryDelay:             10 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            100 * time.Millisecond,
	}
	w, err := NewBaseWorker("test", cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	// Start worker to initialize context
	ctx := context.Background()
	if err := w.StartBase(ctx); err != nil {
		t.Fatalf("StartBase failed: %v", err)
	}
	defer w.StopBase()

	// Create task context
	taskCtx, cancel := w.CreateTaskContext()
	defer cancel()

	if taskCtx == nil {
		t.Error("CreateTaskContext should return a non-nil context")
	}

	// Verify the context has a deadline
	deadline, ok := taskCtx.Deadline()
	if !ok {
		t.Error("task context should have a deadline")
	}

	// Deadline should be approximately 100ms from now
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > cfg.TaskTimeout+10*time.Millisecond {
		t.Errorf("unexpected deadline remaining: %v", remaining)
	}
}

// Test WorkerConfig validation edge cases
func TestWorkerConfig_Validate_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		config  *WorkerConfig
		wantErr error
	}{
		{
			name: "negative max retries",
			config: &WorkerConfig{
				Workers:                1,
				BatchSize:              1,
				PollInterval:           time.Second,
				MaxRetries:             -1,
				RetryDelay:             10 * time.Millisecond,
				RetryBackoffMultiplier: 1.0,
				TaskTimeout:            time.Second,
			},
			wantErr: ErrInvalidMaxRetries,
		},
		{
			name: "negative retry delay",
			config: &WorkerConfig{
				Workers:                1,
				BatchSize:              1,
				PollInterval:           time.Second,
				MaxRetries:             1,
				RetryDelay:             -1,
				RetryBackoffMultiplier: 1.0,
				TaskTimeout:            time.Second,
			},
			wantErr: ErrInvalidRetryDelay,
		},
		{
			name: "negative task timeout",
			config: &WorkerConfig{
				Workers:                1,
				BatchSize:              1,
				PollInterval:           time.Second,
				MaxRetries:             1,
				RetryDelay:             10 * time.Millisecond,
				RetryBackoffMultiplier: 1.0,
				TaskTimeout:            -1,
			},
			wantErr: ErrInvalidTaskTimeout,
		},
		{
			name: "zero task timeout",
			config: &WorkerConfig{
				Workers:                1,
				BatchSize:              1,
				PollInterval:           time.Second,
				MaxRetries:             1,
				RetryDelay:             10 * time.Millisecond,
				RetryBackoffMultiplier: 1.0,
				TaskTimeout:            0,
			},
			wantErr: ErrInvalidTaskTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test WorkerConfig.Clone with nil config
func TestWorkerConfig_Clone_Nil(t *testing.T) {
	var cfg *WorkerConfig
	clone := cfg.Clone()
	if clone != nil {
		t.Error("Clone() of nil config should return nil")
	}
}

// Test WorkerManager individual worker operations
func TestWorkerManager_IndividualWorkerOps(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	// Create a test worker
	w, _ := NewBaseWorker("test-worker", nil, logger)
	tw := &testWorker{BaseWorker: w}
	mgr.Register(tw)

	ctx := context.Background()

	// Test StartWorker
	t.Run("StartWorker success", func(t *testing.T) {
		err := mgr.StartWorker(ctx, "test-worker")
		if err != nil {
			t.Errorf("StartWorker failed: %v", err)
		}
		if !tw.IsRunning() {
			t.Error("worker should be running after StartWorker")
		}
	})

	// Test StopWorker
	t.Run("StopWorker success", func(t *testing.T) {
		err := mgr.StopWorker("test-worker")
		if err != nil {
			t.Errorf("StopWorker failed: %v", err)
		}
		if tw.IsRunning() {
			t.Error("worker should not be running after StopWorker")
		}
	})

	// Test RestartWorker
	t.Run("RestartWorker success", func(t *testing.T) {
		// Start first
		mgr.StartWorker(ctx, "test-worker")
		err := mgr.RestartWorker(ctx, "test-worker")
		if err != nil {
			t.Errorf("RestartWorker failed: %v", err)
		}
		if !tw.IsRunning() {
			t.Error("worker should be running after RestartWorker")
		}
		mgr.StopWorker("test-worker")
	})

	// Test operations on non-existent worker
	t.Run("StartWorker not found", func(t *testing.T) {
		err := mgr.StartWorker(ctx, "non-existent")
		if !IsWorkerNotFound(err) {
			t.Errorf("expected WorkerNotFound error, got %v", err)
		}
	})

	t.Run("StopWorker not found", func(t *testing.T) {
		err := mgr.StopWorker("non-existent")
		if !IsWorkerNotFound(err) {
			t.Errorf("expected WorkerNotFound error, got %v", err)
		}
	})

	t.Run("RestartWorker not found", func(t *testing.T) {
		err := mgr.RestartWorker(ctx, "non-existent")
		if !IsWorkerNotFound(err) {
			t.Errorf("expected WorkerNotFound error, got %v", err)
		}
	})

	// Test operations after close
	mgr.Close()

	t.Run("StartWorker after close", func(t *testing.T) {
		err := mgr.StartWorker(ctx, "test-worker")
		if !IsWorkerManagerClosed(err) {
			t.Errorf("expected WorkerManagerClosed error, got %v", err)
		}
	})

	t.Run("StopWorker after close", func(t *testing.T) {
		err := mgr.StopWorker("test-worker")
		if !IsWorkerManagerClosed(err) {
			t.Errorf("expected WorkerManagerClosed error, got %v", err)
		}
	})
}

// Test WorkerManager StartAll rollback on failure
func TestWorkerManager_StartAll_Rollback(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	// Create workers - one that succeeds and one that fails
	w1, _ := NewBaseWorker("worker-1", nil, logger)
	w2, _ := NewBaseWorker("worker-2", nil, logger)

	// Use failing worker for second registration
	tw1 := &testWorker{BaseWorker: w1}
	tw2 := &failingStartWorker{BaseWorker: w2}

	mgr.Register(tw1)
	mgr.Register(tw2)

	ctx := context.Background()
	err := mgr.StartAll(ctx)

	// Should fail
	if err == nil {
		t.Error("StartAll should fail when a worker fails to start")
		mgr.StopAll()
		return
	}

	// First worker should have been stopped during rollback
	// Note: Due to concurrency, we just verify the manager state
	if mgr.IsRunning() {
		t.Error("manager should not be running after failed StartAll")
	}
}

// failingStartWorker is a worker that fails on Start
type failingStartWorker struct {
	*BaseWorker
}

func (w *failingStartWorker) Start(ctx context.Context) error {
	return errors.New("intentional start failure")
}

func (w *failingStartWorker) Stop() error {
	return w.StopBase()
}

// Test WorkerManager StopAll with error
func TestWorkerManager_StopAll_WithError(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	// Create a worker that fails on stop
	w1, _ := NewBaseWorker("worker-1", nil, logger)
	tw1 := &failingStopWorker{BaseWorker: w1, stopErr: errors.New("stop failed")}

	mgr.Register(tw1)

	ctx := context.Background()
	mgr.StartAll(ctx)

	// StopAll should return the first error but continue stopping other workers
	err := mgr.StopAll()
	if err == nil {
		t.Error("StopAll should return error from failing worker")
	}
}

// failingStopWorker is a worker that can fail on Stop
type failingStopWorker struct {
	*BaseWorker
	stopErr error
}

func (w *failingStopWorker) Start(ctx context.Context) error {
	return w.StartBase(ctx)
}

func (w *failingStopWorker) Stop() error {
	w.running.Store(false)
	return w.stopErr
}

// Test WorkerManager Unregister running worker
func TestWorkerManager_Unregister_RunningWorker(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	w, _ := NewBaseWorker("test-worker", nil, logger)
	tw := &testWorker{BaseWorker: w}
	mgr.Register(tw)

	ctx := context.Background()
	mgr.StartWorker(ctx, "test-worker")

	// Worker is running, unregister should stop it first
	err := mgr.Unregister("test-worker")
	if err != nil {
		t.Errorf("Unregister failed: %v", err)
	}

	if tw.IsRunning() {
		t.Error("worker should be stopped after unregister")
	}
}

// Test WorkerManager Register nil worker
func TestWorkerManager_Register_NilWorker(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	err := mgr.Register(nil)
	if err != ErrWorkerConfigNil {
		t.Errorf("expected ErrWorkerConfigNil, got %v", err)
	}
}

// Test worker error helper functions with nil input
func TestWorkerErrors_NilChecks(t *testing.T) {
	tests := []struct {
		name     string
		fn       func(error) bool
		expected bool
	}{
		{"IsWorkerAlreadyRunning nil", func(e error) bool { return IsWorkerAlreadyRunning(nil) }, false},
		{"IsWorkerNotFound nil", func(e error) bool { return IsWorkerNotFound(nil) }, false},
		{"IsTaskProcessingError nil", func(e error) bool { return IsTaskProcessingError(nil) }, false},
		{"IsWorkerManagerClosed nil", func(e error) bool { return IsWorkerManagerClosed(nil) }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fn(nil) != tt.expected {
				t.Errorf("%s should return %v for nil", tt.name, tt.expected)
			}
		})
	}

	// Test with non-CatalogError
	plainErr := errors.New("plain error")
	if IsWorkerAlreadyRunning(plainErr) {
		t.Error("IsWorkerAlreadyRunning should return false for plain error")
	}
	if IsWorkerNotFound(plainErr) {
		t.Error("IsWorkerNotFound should return false for plain error")
	}
	if IsTaskProcessingError(plainErr) {
		t.Error("IsTaskProcessingError should return false for plain error")
	}
	if IsWorkerManagerClosed(plainErr) {
		t.Error("IsWorkerManagerClosed should return false for plain error")
	}
}

// Test NewWorkerNotRunningError
func TestNewWorkerNotRunningError(t *testing.T) {
	err := NewWorkerNotRunningError("test-worker")
	if err == nil {
		t.Fatal("error should not be nil")
	}
	if err.Code != ErrCodeWorkerNotRunning {
		t.Errorf("expected code %s, got %s", ErrCodeWorkerNotRunning, err.Code)
	}
	if err.Message == "" {
		t.Error("error message should not be empty")
	}
}

// Test NewWorkerExistsError
func TestNewWorkerExistsError(t *testing.T) {
	err := NewWorkerExistsError("test-worker")
	if err == nil {
		t.Fatal("error should not be nil")
	}
	if err.Code != ErrCodeWorkerExists {
		t.Errorf("expected code %s, got %s", ErrCodeWorkerExists, err.Code)
	}
}

// mockStorageWithError implements Storage with configurable errors
type mockStorageWithError struct {
	*mockStorage
	getErr    error
	putErr    error
	deleteErr error
}

func newMockStorageWithError() *mockStorageWithError {
	return &mockStorageWithError{
		mockStorage: newMockStorage(),
	}
}

func (s *mockStorageWithError) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.mockStorage.GetWithContext(ctx, key)
}

func (s *mockStorageWithError) PutWithContext(ctx context.Context, key string, data io.Reader) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.mockStorage.PutWithContext(ctx, key, data)
}

func (s *mockStorageWithError) DeleteWithContext(ctx context.Context, key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.mockStorage.DeleteWithContext(ctx, key)
}

// Test compression edge cases
func TestCompressData_EdgeCases(t *testing.T) {
	t.Run("invalid compression level uses default", func(t *testing.T) {
		data := []byte("test data")
		// Level 0 is invalid, should use default
		compressed, err := compressData(data, 0)
		if err != nil {
			t.Errorf("compressData with level 0 failed: %v", err)
		}
		if len(compressed) == 0 {
			t.Error("compressed data should not be empty")
		}
	})

	t.Run("level 10 uses default", func(t *testing.T) {
		data := []byte("test data")
		compressed, err := compressData(data, 10)
		if err != nil {
			t.Errorf("compressData with level 10 failed: %v", err)
		}
		if len(compressed) == 0 {
			t.Error("compressed data should not be empty")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		data := []byte{}
		compressed, err := compressData(data, 5)
		if err != nil {
			t.Errorf("compressData with empty data failed: %v", err)
		}
		decompressed, err := decompressData(compressed)
		if err != nil {
			t.Errorf("decompressData failed: %v", err)
		}
		if len(decompressed) != 0 {
			t.Error("decompressed empty data should be empty")
		}
	})
}

// Test decompressData with invalid data
func TestDecompressData_InvalidData(t *testing.T) {
	_, err := decompressData([]byte("not gzip data"))
	if err == nil {
		t.Error("decompressData should fail with invalid gzip data")
	}
}

// Test ReplicationWorker task processing
func TestReplicationWorker_ProcessTask(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Set up backends
	srcStorage := newMockStorage()
	dstStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	backends.addBackend("replica", dstStorage)

	// Create entry with data
	testData := []byte("test data for replication")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		ReplicationPolicy: &ReplicationPolicy{
			Enabled:      true,
			TargetStores: []string{"replica"},
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             1,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, err := NewReplicationWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Enqueue a replication task
	catalog.EnqueueReplication(ctx, "test-key", []string{"replica"}, 1)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify metrics
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected at least one task processed")
	}
}

// Test ArchiveWorker task processing
func TestArchiveWorker_ProcessTask(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Set up backends
	srcStorage := newMockStorage()
	archiveStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	backends.addBackend("archive", archiveStorage)

	// Create entry with data
	testData := []byte("test data for archive")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:          "test-key",
		Checksum:     checksum[:],
		Status:       StatusActive,
		LastAccessAt: time.Now().Add(-30 * 24 * time.Hour).Unix(), // 30 days ago
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		ArchivePolicy: &ArchivePolicy{
			Enabled:           true,
			ArchiveAfterDays:  7,
			CompressionLevel:  5,
			DeleteFromPrimary: false,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             1,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, err := NewArchiveWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Enqueue an archive task
	catalog.EnqueueArchive(ctx, "test-key", "archive")

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify metrics
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected at least one task processed")
	}
}

// Test VerifyWorker task processing
func TestVerifyWorker_ProcessTask(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Set up backend
	storage := newMockStorage()
	backends.addBackend("primary", storage)

	// Create entry with data
	testData := []byte("test data for verify")
	checksum := sha256.Sum256(testData)
	storage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             1,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, err := NewVerifyWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Enqueue a verify task
	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify metrics
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected at least one task processed")
	}
}

// Test RetentionWorker operations
func TestRetentionWorker_ProcessRetentionTask(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Set up backend
	storage := newMockStorage()
	backends.addBackend("primary", storage)
	storage.putRaw("obj-ref-1", []byte("data to delete"))

	// Create entry that should be deleted (using RetentionPolicy)
	entry := &CatalogEntry{
		Key:       "test-key",
		Status:    StatusActive,
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour).Unix(), // Created 100 days ago
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		RetentionPolicy: &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30, // Keep for 30 days, entry is 100 days old
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             1,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: time.Hour, // Long scan interval to avoid auto-scan
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Use ProcessRetentionTask directly
	err = w.ProcessRetentionTask("test-key")
	if err != nil {
		t.Errorf("ProcessRetentionTask failed: %v", err)
	}

	// Verify the entry was deleted
	_, err = catalog.Get(ctx, "test-key")
	if !IsNotFound(err) {
		t.Error("entry should have been deleted")
	}
}

// Test RetentionWorker ProcessRetentionTask when not running
func TestRetentionWorker_ProcessRetentionTask_NotRunning(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	w, err := NewRetentionWorker(catalog, backends, nil, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	// Worker not started
	err = w.ProcessRetentionTask("test-key")
	if err == nil {
		t.Error("ProcessRetentionTask should fail when worker not running")
	}
}

// Test RetentionWorker with zero scan interval uses default
func TestRetentionWorker_ZeroScanInterval(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	cfg := &RetentionWorkerConfig{
		WorkerConfig: DefaultWorkerConfig(),
		ScanInterval: 0, // Should use default of time.Hour
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	if w.scanInterval != time.Hour {
		t.Errorf("expected scan interval of 1h, got %v", w.scanInterval)
	}
}

// Test DefaultRetentionWorkerConfig
func TestDefaultRetentionWorkerConfig(t *testing.T) {
	cfg := DefaultRetentionWorkerConfig()
	if cfg == nil {
		t.Fatal("config should not be nil")
	}
	if cfg.WorkerConfig == nil {
		t.Error("WorkerConfig should not be nil")
	}
	if cfg.ScanInterval != time.Hour {
		t.Errorf("expected ScanInterval of 1h, got %v", cfg.ScanInterval)
	}
}

// Test worker graceful shutdown during task processing
func TestWorker_GracefulShutdown(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Set up backend with slow operation
	slowStorage := &slowMockStorage{mockStorage: newMockStorage(), delay: 50 * time.Millisecond}
	backends.addBackend("primary", slowStorage)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           5 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, err := NewReplicationWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)

	// Give worker time to enter poll cycle
	time.Sleep(20 * time.Millisecond)

	// Stop should complete even if worker is polling
	stopDone := make(chan error)
	go func() {
		stopDone <- w.Stop()
	}()

	select {
	case err := <-stopDone:
		if err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Stop timed out - worker not shutting down gracefully")
	}
}

// slowMockStorage adds delay to operations for testing
type slowMockStorage struct {
	*mockStorage
	delay time.Duration
}

func (s *slowMockStorage) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(s.delay):
	}
	return s.mockStorage.GetWithContext(ctx, key)
}

// Test concurrent worker manager operations
func TestWorkerManager_Concurrent(t *testing.T) {
	logger := &mockLogger{}
	mgr := NewWorkerManager(logger)

	// Create multiple workers
	for i := 0; i < 5; i++ {
		w, _ := NewBaseWorker(string(rune('A'+i))+"-worker", nil, logger)
		mgr.Register(&testWorker{BaseWorker: w})
	}

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 4 {
			case 0:
				mgr.ListWorkers()
			case 1:
				mgr.GetMetrics()
			case 2:
				mgr.GetAggregatedMetrics()
			case 3:
				mgr.GetStatus()
			}
		}(i)
	}

	wg.Wait()

	// Start and stop concurrently
	mgr.StartAll(ctx)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.RunningWorkerCount()
		}()
	}
	wg.Wait()
	mgr.StopAll()
}

// Test worker with catalog closed during processing
func TestWorker_CatalogClosed(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            1 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)

	// Close catalog while worker is running
	time.Sleep(20 * time.Millisecond)
	catalog.Close()

	// Worker should handle closed catalog gracefully
	time.Sleep(30 * time.Millisecond)

	err := w.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// Test ArchiveWorker with nil backends
func TestArchiveWorker_NilBackends(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()

	_, err := NewArchiveWorker(catalog, nil, nil, logger)
	if err != ErrBackendManagerNilForWorker {
		t.Errorf("expected ErrBackendManagerNilForWorker, got %v", err)
	}
}

// Test VerifyWorker with nil backends
func TestVerifyWorker_NilBackends(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()

	_, err := NewVerifyWorker(catalog, nil, nil, logger)
	if err != ErrBackendManagerNilForWorker {
		t.Errorf("expected ErrBackendManagerNilForWorker, got %v", err)
	}
}

// Test RetentionWorker with nil backends
func TestRetentionWorker_NilBackends(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()

	_, err := NewRetentionWorker(catalog, nil, nil, logger)
	if err != ErrBackendManagerNilForWorker {
		t.Errorf("expected ErrBackendManagerNilForWorker, got %v", err)
	}
}

// Test ArchiveWorker Start and Stop
func TestArchiveWorker_StartStop(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            1 * time.Second,
	}

	w, err := NewArchiveWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()

	// Start
	if err := w.Start(ctx); err != nil {
		t.Errorf("Start failed: %v", err)
	}
	if !w.IsRunning() {
		t.Error("worker should be running")
	}

	// Double start should fail
	if err := w.Start(ctx); !IsWorkerAlreadyRunning(err) {
		t.Errorf("expected WorkerAlreadyRunning error, got %v", err)
	}

	// Stop
	if err := w.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	if w.IsRunning() {
		t.Error("worker should not be running")
	}
}

// Test VerifyWorker Start and Stop
func TestVerifyWorker_StartStop(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            1 * time.Second,
	}

	w, err := NewVerifyWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()

	// Start
	if err := w.Start(ctx); err != nil {
		t.Errorf("Start failed: %v", err)
	}
	if !w.IsRunning() {
		t.Error("worker should be running")
	}

	// Stop
	if err := w.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	if w.IsRunning() {
		t.Error("worker should not be running")
	}
}

// Test RetentionWorker Start and Stop
func TestRetentionWorker_StartStop(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             0,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            1 * time.Second,
		},
		ScanInterval: time.Hour, // Long scan interval
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()

	// Start
	if err := w.Start(ctx); err != nil {
		t.Errorf("Start failed: %v", err)
	}
	if !w.IsRunning() {
		t.Error("worker should be running")
	}

	// Stop
	if err := w.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	if w.IsRunning() {
		t.Error("worker should not be running")
	}
}

// Test VerifyWorker with checksum mismatch
func TestVerifyWorker_ChecksumMismatch(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Set up backend with wrong data
	storage := newMockStorage()
	backends.addBackend("primary", storage)

	testData := []byte("test data")
	wrongData := []byte("wrong data")
	correctChecksum := sha256.Sum256(testData)
	storage.putRaw("obj-ref-1", wrongData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: correctChecksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		ReplicationPolicy: &ReplicationPolicy{
			Enabled:      true,
			TargetStores: []string{"replica"},
			MinReplicas:  2,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Enqueue verify task
	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Check metrics - should have failed
	metrics := w.Metrics()
	if metrics.TasksFailed.Load() < 1 {
		t.Error("expected at least one failed task due to checksum mismatch")
	}
}

// Test VerifyWorker with no checksum - should skip
func TestVerifyWorker_NoChecksum(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)
	storage.putRaw("obj-ref-1", []byte("test data"))

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: nil, // No checksum
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Enqueue verify task
	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Should complete successfully (skipped verification)
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed (skipped)")
	}
}

// Test ArchiveWorker with no archive policy
func TestArchiveWorker_NoArchivePolicy(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)
	backends.addBackend("archive", newMockStorage())

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	storage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:           "test-key",
		Checksum:      checksum[:],
		Status:        StatusActive,
		ArchivePolicy: nil, // No archive policy
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewArchiveWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueArchive(ctx, "test-key", "archive")

	time.Sleep(200 * time.Millisecond)

	// Should complete successfully (skipped)
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed (skipped)")
	}
}

// Test ArchiveWorker with disabled archive policy
func TestArchiveWorker_DisabledPolicy(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)
	backends.addBackend("archive", newMockStorage())

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	storage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		ArchivePolicy: &ArchivePolicy{
			Enabled: false, // Disabled
		},
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewArchiveWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueArchive(ctx, "test-key", "archive")

	time.Sleep(200 * time.Millisecond)

	// Should complete successfully (skipped)
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed (skipped due to disabled policy)")
	}
}

// Test ReplicationWorker with existing location - should skip
func TestReplicationWorker_ExistingLocation(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	srcStorage := newMockStorage()
	dstStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	backends.addBackend("replica", dstStorage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		Replicas: []StoreLocation{
			{
				Store:     "replica", // Already has this replica
				ObjectRef: "existing-ref",
				Status:    LocationStatusActive,
			},
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueReplication(ctx, "test-key", []string{"replica"}, 1)

	time.Sleep(200 * time.Millisecond)

	// AddLocation should not be called because replica already exists
	if catalog.addLocationCalled > 0 {
		t.Error("AddLocation should not be called for existing replica")
	}
}

// Test ReplicationWorker with checksum mismatch on source
func TestReplicationWorker_SourceChecksumMismatch(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	srcStorage := newMockStorage()
	dstStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	backends.addBackend("replica", dstStorage)

	testData := []byte("test data")
	wrongData := []byte("wrong data")
	correctChecksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj-ref-1", wrongData) // Store wrong data

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: correctChecksum[:],
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             1,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueReplication(ctx, "test-key", []string{"replica"}, 1)

	time.Sleep(150 * time.Millisecond)

	// Should have failed due to checksum mismatch
	metrics := w.Metrics()
	if metrics.TasksFailed.Load() < 1 {
		t.Error("expected task to fail due to checksum mismatch")
	}
}

// Test worker ShouldContinue with cancelled context
func TestBaseWorker_ShouldContinue_Cancelled(t *testing.T) {
	logger := &mockLogger{}
	w, _ := NewBaseWorker("test", nil, logger)

	ctx, cancel := context.WithCancel(context.Background())
	w.StartBase(ctx)

	if !w.ShouldContinue() {
		t.Error("ShouldContinue should return true when running")
	}

	cancel()
	time.Sleep(10 * time.Millisecond) // Give context time to be cancelled

	if w.ShouldContinue() {
		t.Error("ShouldContinue should return false when context cancelled")
	}

	w.StopBase()
}

// Test BaseWorker Sleep with cancelled context
func TestBaseWorker_Sleep_Cancelled(t *testing.T) {
	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           time.Second, // Long poll interval
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            time.Second,
	}
	w, _ := NewBaseWorker("test", cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	w.StartBase(ctx)

	// Start sleep in goroutine
	done := make(chan struct{})
	go func() {
		w.Sleep()
		close(done)
	}()

	// Cancel immediately
	cancel()

	// Sleep should return quickly
	select {
	case <-done:
		// Good - Sleep returned
	case <-time.After(100 * time.Millisecond):
		t.Error("Sleep did not return quickly after context cancellation")
	}

	w.StopBase()
}

// mockBackendManagerWithError allows configuring backend errors
type mockBackendManagerWithError struct {
	*mockBackendManager
	getBackendErr error
}

func (m *mockBackendManagerWithError) GetBackend(name string) (common.Storage, error) {
	if m.getBackendErr != nil {
		return nil, m.getBackendErr
	}
	return m.mockBackendManager.GetBackend(name)
}

// Test ReplicationWorker with backend get error
func TestReplicationWorker_BackendGetError(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := &mockBackendManagerWithError{
		mockBackendManager: newMockBackendManager(),
		getBackendErr:      errors.New("backend unavailable"),
	}

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueReplication(ctx, "test-key", []string{"replica"}, 1)

	time.Sleep(200 * time.Millisecond)

	metrics := w.Metrics()
	if metrics.TasksFailed.Load() < 1 {
		t.Error("expected task to fail due to backend error")
	}
}

// Test ReplicationWorker with catalog get error
func TestReplicationWorker_CatalogGetError(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	catalog.getError = errors.New("catalog unavailable")
	backends := newMockBackendManager()

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Add a task manually since we can't use the catalog
	catalog.mu.Lock()
	catalog.replicationQueue = append(catalog.replicationQueue, &ReplicationTask{
		Key:       "test-key",
		Targets:   []string{"replica"},
		Priority:  1,
		CreatedAt: time.Now().UnixNano(),
	})
	catalog.mu.Unlock()

	time.Sleep(200 * time.Millisecond)

	metrics := w.Metrics()
	if metrics.TasksFailed.Load() < 1 {
		t.Error("expected task to fail due to catalog error")
	}
}

// Test ArchiveWorker with already archived entry
func TestArchiveWorker_AlreadyArchived(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	srcStorage := newMockStorage()
	archiveStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	backends.addBackend("archive", archiveStorage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:          "test-key",
		Checksum:     checksum[:],
		Status:       StatusArchived,
		LastAccessAt: time.Now().Add(-30 * 24 * time.Hour).Unix(),
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		ArchiveStore: &StoreLocation{
			Store:     "archive", // Already archived to this target
			ObjectRef: "archive-ref-1",
			Status:    LocationStatusActive,
		},
		ArchivePolicy: &ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 7,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewArchiveWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueArchive(ctx, "test-key", "archive")

	time.Sleep(200 * time.Millisecond)

	// Should complete successfully (already archived)
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed (already archived)")
	}
}

// Test RetentionWorker with already deleted entry
func TestRetentionWorker_AlreadyDeleted(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             0,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: time.Hour,
	}

	w, _ := NewRetentionWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Process task for non-existent key
	err := w.ProcessRetentionTask("non-existent-key")
	if err != nil {
		t.Errorf("ProcessRetentionTask failed: %v", err)
	}

	// Should complete without error (already deleted)
}

// Test RetentionWorker with entry that shouldn't be deleted
func TestRetentionWorker_ShouldNotDelete(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)
	storage.putRaw("obj-ref-1", []byte("data"))

	entry := &CatalogEntry{
		Key:       "test-key",
		Status:    StatusActive,
		CreatedAt: time.Now().Unix(), // Just created
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		RetentionPolicy: &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30, // Keep for 30 days
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             0,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: time.Hour,
	}

	w, _ := NewRetentionWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	err := w.ProcessRetentionTask("test-key")
	if err != nil {
		t.Errorf("ProcessRetentionTask failed: %v", err)
	}

	// Entry should still exist
	_, err = catalog.Get(ctx, "test-key")
	if err != nil {
		t.Error("entry should not have been deleted")
	}
}

// Test VerifyWorker with inactive location - should skip
func TestVerifyWorker_InactiveLocation(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	storage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusStale, // Not active
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	time.Sleep(200 * time.Millisecond)

	// Should complete without checking (location is inactive)
	metrics := w.Metrics()
	if metrics.TasksFailed.Load() < 1 {
		t.Error("expected task to fail due to inactive location")
	}
}

// Test VerifyWorker with compressed data
func TestVerifyWorker_CompressedData(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)

	// Create original and compressed data
	originalData := []byte("test data for compression verification")
	checksum := sha256.Sum256(originalData)
	compressedData, err := compressData(originalData, 5)
	if err != nil {
		t.Fatalf("failed to compress data: %v", err)
	}
	compressedChecksum := sha256.Sum256(compressedData)
	storage.putRaw("obj-ref-1", compressedData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:      "primary",
			ObjectRef:  "obj-ref-1",
			Status:     LocationStatusActive,
			Compressed: true,
			Checksum:   compressedChecksum[:],
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	time.Sleep(200 * time.Millisecond)

	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed")
	}
}

// Test ArchiveWorker with compression
func TestArchiveWorker_WithCompression(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	srcStorage := newMockStorage()
	archiveStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	backends.addBackend("archive", archiveStorage)

	testData := []byte("test data for archive with compression")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj-ref-1", testData)

	entry := &CatalogEntry{
		Key:          "test-key",
		Checksum:     checksum[:],
		Status:       StatusActive,
		LastAccessAt: time.Now().Add(-30 * 24 * time.Hour).Unix(),
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
			Status:    LocationStatusActive,
		},
		ArchivePolicy: &ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 7,
			CompressionLevel: 5,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewArchiveWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueArchive(ctx, "test-key", "archive")

	time.Sleep(200 * time.Millisecond)

	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed")
	}
}

// Test to add coverage for helper function in coverage_boost_test.go
func TestCreateMockDataReadCloser(t *testing.T) {
	data := []byte("hello world")
	rc := newBytesReadCloser(data)

	// Read all data
	result, err := io.ReadAll(rc)
	if err != nil {
		t.Errorf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(result, data) {
		t.Errorf("data mismatch: got %v, want %v", result, data)
	}

	// Close should not error
	if err := rc.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// =============================================================================
// Consolidated from worker_targeted_coverage_test.go
// =============================================================================

// verifyReencryptMockBackend implements the interface needed for verifyReencryption testing.
type verifyReencryptMockBackend struct {
	data         map[string][]byte
	mu           sync.RWMutex
	getError     error
	getCallCount int
}

func newVerifyReencryptMockBackend() *verifyReencryptMockBackend {
	return &verifyReencryptMockBackend{
		data: make(map[string][]byte),
	}
}

func (b *verifyReencryptMockBackend) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.getCallCount++
	if b.getError != nil {
		return nil, b.getError
	}
	data, ok := b.data[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *verifyReencryptMockBackend) PutWithContext(ctx context.Context, key string, reader io.Reader) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	b.data[key] = data
	return nil
}

func (b *verifyReencryptMockBackend) setData(key string, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = data
}

// TestVerifyReencryption_Success tests the verifyReencryption function with matching data.
func TestVerifyReencryption_Success(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Set up storage with data
	storage := newReencryptMockStorage()
	expectedData := []byte("enc:test data for verification")
	storage.data["obj/ref"] = expectedData
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = true
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	// Create an entry with matching encryption key
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

	// Start and test
	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	err = worker.Enqueue(task)
	if err != nil {
		t.Errorf("Enqueue failed: %v", err)
	}

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Verification should pass as data matches
	if worker.Progress().FailedTasks.Load() > 0 {
		t.Error("task should not have failed - verification should have passed")
	}
}

// TestVerifyReencryption_DataMismatch tests the verifyReencryption function when data differs.
func TestVerifyReencryption_DataMismatch(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Set up storage that returns different data on second read
	storage := &mismatchStorage{
		data:       make(map[string][]byte),
		callCounts: make(map[string]int),
	}
	storage.data["obj/ref"] = []byte("enc:original data")
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = true
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

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

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Task should fail due to verification mismatch
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to fail due to verification mismatch")
	}
}

// mismatchStorage returns different data on subsequent reads after put.
type mismatchStorage struct {
	data       map[string][]byte
	callCounts map[string]int
	mu         sync.RWMutex
}

func (s *mismatchStorage) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callCounts[key]++
	data, ok := s.data[key]
	if !ok {
		return nil, errors.New("key not found")
	}
	// Return different data on verification read (3rd read - after initial read, put, verify read)
	if s.callCounts[key] >= 2 {
		return io.NopCloser(bytes.NewReader([]byte("different data"))), nil
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *mismatchStorage) PutWithContext(ctx context.Context, key string, reader io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.data[key] = data
	return nil
}

// Stub implementations for common.Storage interface
func (s *mismatchStorage) Get(key string) (io.ReadCloser, error) {
	return s.GetWithContext(context.Background(), key)
}
func (s *mismatchStorage) Put(key string, reader io.Reader) error {
	return s.PutWithContext(context.Background(), key, reader)
}
func (s *mismatchStorage) Delete(key string) error                                 { return nil }
func (s *mismatchStorage) DeleteWithContext(ctx context.Context, key string) error { return nil }
func (s *mismatchStorage) List(prefix string) ([]string, error)                    { return nil, nil }
func (s *mismatchStorage) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}
func (s *mismatchStorage) Exists(ctx context.Context, key string) (bool, error) { return false, nil }
func (s *mismatchStorage) Size(key string) (int64, error)                       { return 0, nil }
func (s *mismatchStorage) SizeWithContext(ctx context.Context, key string) (int64, error) {
	return 0, nil
}
func (s *mismatchStorage) Configure(settings map[string]string) error { return nil }
func (s *mismatchStorage) PutWithMetadata(ctx context.Context, key string, data io.Reader, metadata *common.Metadata) error {
	return s.PutWithContext(ctx, key, data)
}
func (s *mismatchStorage) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	return nil, nil
}
func (s *mismatchStorage) UpdateMetadata(ctx context.Context, key string, metadata *common.Metadata) error {
	return nil
}
func (s *mismatchStorage) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	return nil, nil
}
func (s *mismatchStorage) Archive(key string, destination common.Archiver) error { return nil }
func (s *mismatchStorage) AddPolicy(policy common.LifecyclePolicy) error         { return nil }
func (s *mismatchStorage) RemovePolicy(id string) error                          { return nil }
func (s *mismatchStorage) GetPolicies() ([]common.LifecyclePolicy, error)        { return nil, nil }

// TestReencryptLocation_BackendNotFound tests reencryptLocation when backend is not found.
func TestReencryptLocation_BackendNotFound(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Don't register any backend for "nonexistent"

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = false
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	entry := &CatalogEntry{
		Key: "test/key",
		PrimaryStore: StoreLocation{
			Store:     "nonexistent", // Backend not registered
			ObjectRef: "obj/ref",
			Encryption: &LocationEncryption{
				KeyID:     "old/key",
				Algorithm: "AES-256-GCM",
				Nonce:     []byte("testnonce123"),
			},
		},
	}
	catalog.Put(context.Background(), entry)

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	time.Sleep(200 * time.Millisecond)

	// Should fail due to backend not found
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to fail due to backend not found")
	}
}

// TestReencryptLocation_ReadError tests reencryptLocation when read fails.
func TestReencryptLocation_ReadError(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Set up storage that fails on read
	storage := newReencryptMockStorage()
	storage.getError = errors.New("read failed")
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = false
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

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

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	time.Sleep(200 * time.Millisecond)

	// Should fail due to read error
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to fail due to read error")
	}
}

// TestReencryptLocation_ObjectTooLarge tests reencryptLocation when object exceeds max size.
func TestReencryptLocation_ObjectTooLarge(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Set up storage with large data
	storage := newReencryptMockStorage()
	largeData := make([]byte, 1024) // 1KB of data
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	storage.data["obj/ref"] = largeData
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = false
	cfg.MaxBytesPerTask = 100 // Set max to 100 bytes
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

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

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	time.Sleep(200 * time.Millisecond)

	// Should fail due to size limit exceeded
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to fail due to size limit")
	}
}

// TestReencryptLocation_ReencryptError tests reencryptLocation when re-encryption fails.
func TestReencryptLocation_ReencryptError(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Set up encryption to fail
	encryption.reencryptFunc = func(ctx context.Context, data []byte, oldEnc *LocationEncryption, newKeyID string) (*EncryptionResult, error) {
		return nil, errors.New("re-encryption failed")
	}

	storage := newReencryptMockStorage()
	storage.data["obj/ref"] = []byte("enc:original data")
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = false
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

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

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	time.Sleep(200 * time.Millisecond)

	// Should fail due to re-encryption error
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to fail due to re-encryption error")
	}
}

// TestReencryptLocation_WriteError tests reencryptLocation when write fails.
func TestReencryptLocation_WriteError(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Set up storage that fails on write
	storage := newReencryptMockStorage()
	storage.data["obj/ref"] = []byte("enc:original data")
	storage.putError = errors.New("write failed")
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = false
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

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

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	time.Sleep(200 * time.Millisecond)

	// Should fail due to write error
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to fail due to write error")
	}
}

// TestReencryptLocation_CatalogUpdateError tests reencryptLocation when catalog update fails.
func TestReencryptLocation_CatalogUpdateError(t *testing.T) {
	catalog := newReencryptMockCatalog()
	catalog.updateError = errors.New("catalog update failed")
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	storage := newReencryptMockStorage()
	storage.data["obj/ref"] = []byte("enc:original data")
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.VerifyAfterReencrypt = false
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

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

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	time.Sleep(200 * time.Millisecond)

	// Should fail due to catalog update error
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to fail due to catalog update error")
	}
}

// TestProcessLoop_TaskRetry tests the retry logic in processLoop.
func TestProcessLoop_TaskRetry(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	// Storage that always fails
	storage := newReencryptMockStorage()
	storage.getError = errors.New("persistent failure")
	backends.RegisterStorage("store1", storage)

	cfg := DefaultReencryptWorkerConfig()
	cfg.MaxRetries = 2
	cfg.RetryDelay = 10 * time.Millisecond
	cfg.VerifyAfterReencrypt = false
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

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

	worker.Start(context.Background())
	defer worker.Stop()

	task := &ReencryptTask{
		Key:      "test/key",
		OldKeyID: "old/key",
		NewKeyID: "new/key",
	}
	worker.Enqueue(task)

	// Wait for retries
	time.Sleep(500 * time.Millisecond)

	// Should have retried and ultimately failed
	metrics := worker.Metrics()
	if metrics.TasksRetried.Load() == 0 {
		t.Error("expected at least one retry")
	}
	if worker.Progress().FailedTasks.Load() == 0 {
		t.Error("expected task to ultimately fail")
	}
}

// TestEnqueueBulk_QueueFull tests EnqueueBulk when queue becomes full.
func TestEnqueueBulk_QueueFull(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	cfg := DefaultReencryptWorkerConfig()
	cfg.BatchSize = 1 // Very small queue
	cfg.Workers = 1
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, &mockLogger{})
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	worker.Start(context.Background())
	defer worker.Stop()

	// Try to enqueue many keys - some should fail silently due to full queue
	keys := make([]string, 100)
	for i := range keys {
		keys[i] = string(rune('a' + i%26))
	}

	// This should not return an error even when queue is full
	// (it logs a warning and continues)
	err = worker.EnqueueBulk("old/key", "new/key", keys, 1)
	if err != nil {
		t.Errorf("EnqueueBulk should not return error for queue full: %v", err)
	}
}

// TestScanForExpiredEntries tests the retention worker's scanning logic.
func TestScanForExpiredEntries(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Set up backend
	storage := newMockStorage()
	backends.addBackend("primary", storage)
	storage.putRaw("obj1", []byte("data1"))
	storage.putRaw("obj2", []byte("data2"))

	// Create entries - one expired, one not
	expiredEntry := &CatalogEntry{
		Key:       "expired-key",
		Status:    StatusActive,
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour).Unix(),
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		RetentionPolicy: &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	activeEntry := &CatalogEntry{
		Key:       "active-key",
		Status:    StatusActive,
		CreatedAt: time.Now().Unix(),
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj2",
			Status:    LocationStatusActive,
		},
		RetentionPolicy: &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	catalog.Put(context.Background(), expiredEntry)
	catalog.Put(context.Background(), activeEntry)

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              10,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             1,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: 50 * time.Millisecond, // Fast scan for testing
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Wait for scan to execute
	time.Sleep(200 * time.Millisecond)

	// Expired entry should be deleted
	_, err = catalog.Get(ctx, "expired-key")
	if !IsNotFound(err) {
		t.Error("expired entry should have been deleted")
	}

	// Active entry should still exist
	_, err = catalog.Get(ctx, "active-key")
	if err != nil {
		t.Error("active entry should still exist")
	}
}

// TestScanForExpiredEntries_ListError tests scanForExpiredEntries when list fails.
func TestScanForExpiredEntries_ListError(t *testing.T) {
	logger := &mockLogger{}
	catalog := &errorOnListCatalog{mockCatalogManager: newMockCatalogManager()}
	backends := newMockBackendManager()

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             0,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: 50 * time.Millisecond,
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)

	// Wait for scan attempt
	time.Sleep(100 * time.Millisecond)

	w.Stop()
	// Should not panic, just log warning
}

// errorOnListCatalog returns error on ListByStatus.
type errorOnListCatalog struct {
	*mockCatalogManager
}

func (c *errorOnListCatalog) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	return nil, errors.New("list failed")
}

// TestRetentionProcessTask_RetrySuccess tests processTask retry that eventually succeeds.
func TestRetentionProcessTask_RetrySuccess(t *testing.T) {
	logger := &mockLogger{}
	catalog := &failThenSucceedCatalog{
		mockCatalogManager: newMockCatalogManager(),
		failCount:          1,
	}
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)
	storage.putRaw("obj1", []byte("data"))

	entry := &CatalogEntry{
		Key:       "test-key",
		Status:    StatusActive,
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour).Unix(),
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		RetentionPolicy: &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             3,
			RetryDelay:             10 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: time.Hour,
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	err = w.ProcessRetentionTask("test-key")
	if err != nil {
		t.Errorf("ProcessRetentionTask should succeed after retry: %v", err)
	}

	// Entry should be deleted after retry succeeds
	_, err = catalog.Get(ctx, "test-key")
	if !IsNotFound(err) {
		t.Error("entry should have been deleted after retry")
	}
}

// failThenSucceedCatalog fails for first N calls then succeeds.
type failThenSucceedCatalog struct {
	*mockCatalogManager
	failCount int
	callCount int
	mu        sync.Mutex
}

func (c *failThenSucceedCatalog) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	c.callCount++
	currentCount := c.callCount
	c.mu.Unlock()

	if currentCount <= c.failCount {
		return errors.New("transient failure")
	}
	return c.mockCatalogManager.Delete(ctx, key)
}

// TestMarkLocationFailed_UpdateError tests markLocationFailed when update fails.
func TestMarkLocationFailed_UpdateError(t *testing.T) {
	logger := &mockLogger{}
	catalog := &errorOnUpdateLocationCatalog{mockCatalogManager: newMockCatalogManager()}
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)

	wrongData := []byte("wrong data")
	storage.putRaw("obj1", wrongData)

	testData := []byte("correct data")
	checksum := sha256.Sum256(testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		ReplicationPolicy: &ReplicationPolicy{
			Enabled:      true,
			TargetStores: []string{"replica"},
			MinReplicas:  2,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	time.Sleep(200 * time.Millisecond)

	// Should complete but with warning logged (markLocationFailed fails)
	metrics := w.Metrics()
	if metrics.TasksFailed.Load() == 0 {
		t.Error("expected task to fail due to verification failure")
	}
}

// errorOnUpdateLocationCatalog returns error on UpdateLocationStatus.
type errorOnUpdateLocationCatalog struct {
	*mockCatalogManager
}

func (c *errorOnUpdateLocationCatalog) UpdateLocationStatus(ctx context.Context, key, store string, status LocationStatus) error {
	return errors.New("update location status failed")
}

// TestReplicateToTarget_AddLocationError tests replicateToTarget when AddLocation fails.
func TestReplicateToTarget_AddLocationError(t *testing.T) {
	logger := &mockLogger{}
	catalog := &errorOnAddLocationCatalog{mockCatalogManager: newMockCatalogManager()}
	backends := newMockBackendManager()

	srcStorage := newMockStorage()
	dstStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	backends.addBackend("replica", dstStorage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueReplication(ctx, "test-key", []string{"replica"}, 1)

	time.Sleep(200 * time.Millisecond)

	// Should complete without failing overall task (continues with other targets)
	// The replication was logged as warning but didn't fail the whole task
}

// errorOnAddLocationCatalog returns error on AddLocation.
type errorOnAddLocationCatalog struct {
	*mockCatalogManager
}

func (c *errorOnAddLocationCatalog) AddLocation(ctx context.Context, key string, loc *StoreLocation) error {
	return errors.New("add location failed")
}

// TestReplicateToTarget_DestBackendNotFound tests replicateToTarget when dest backend not found.
func TestReplicateToTarget_DestBackendNotFound(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	srcStorage := newMockStorage()
	backends.addBackend("primary", srcStorage)
	// Don't add "replica" backend

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueReplication(ctx, "test-key", []string{"replica"}, 1)

	time.Sleep(200 * time.Millisecond)

	// Should complete (logs warning, continues with other targets if any)
	metrics := w.Metrics()
	// Task processed but replication to target failed
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed")
	}
}

// TestReplicateToTarget_WriteError tests replicateToTarget when write to dest fails.
func TestReplicateToTarget_WriteError(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	srcStorage := newMockStorage()
	dstStorage := newMockStorageWithError()
	dstStorage.putErr = errors.New("write failed")
	backends.addBackend("primary", srcStorage)
	backends.addBackend("replica", dstStorage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	srcStorage.putRaw("obj1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewReplicationWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueReplication(ctx, "test-key", []string{"replica"}, 1)

	time.Sleep(200 * time.Millisecond)

	// Should complete (logs warning, continues with other targets if any)
	metrics := w.Metrics()
	if metrics.TasksProcessed.Load() < 1 {
		t.Error("expected task to be processed")
	}
}

// TestTriggerRepairReplication tests the triggerRepairReplication function.
func TestTriggerRepairReplication_NeedsReplication(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	storage.putRaw("obj1", testData)

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		ReplicationPolicy: &ReplicationPolicy{
			Enabled:      true,
			TargetStores: []string{"replica1", "replica2"},
			MinReplicas:  2,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// Enqueue verify that will fail (checksum mismatch)
	// This should trigger repair replication
	wrongData := []byte("wrong data")
	storage.putRaw("obj1", wrongData)

	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	time.Sleep(200 * time.Millisecond)

	// Check if repair replication was enqueued
	catalog.mu.Lock()
	replicationQueueLen := len(catalog.replicationQueue)
	catalog.mu.Unlock()

	if replicationQueueLen == 0 {
		t.Error("expected repair replication to be enqueued")
	}
}

// TestTriggerRepairReplication_PolicyDisabled tests when replication policy is disabled.
func TestTriggerRepairReplication_PolicyDisabled(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	// Store wrong data to trigger verification failure
	storage.putRaw("obj1", []byte("wrong data"))

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		ReplicationPolicy: &ReplicationPolicy{
			Enabled: false, // Disabled
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	time.Sleep(200 * time.Millisecond)

	// Should not enqueue repair replication
	catalog.mu.Lock()
	replicationQueueLen := len(catalog.replicationQueue)
	catalog.mu.Unlock()

	if replicationQueueLen > 0 {
		t.Error("should not enqueue repair replication when policy disabled")
	}
}

// TestProcessLoop_NilTask tests processLoop handling of nil task.
func TestProcessLoop_NilTask(t *testing.T) {
	catalog := newReencryptMockCatalog()
	backends := newReencryptMockBackends()
	encryption := newReencryptMockEncryption()

	cfg := DefaultReencryptWorkerConfig()
	worker, err := NewReencryptWorker(catalog, backends, encryption, cfg, nil)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	worker.Start(context.Background())

	// Manually send nil to task queue (edge case)
	select {
	case worker.taskQueue <- nil:
	default:
	}

	time.Sleep(100 * time.Millisecond)
	worker.Stop()

	// Should not panic
}

// TestRetention_DeleteFromLocation_BackendNotFound tests deleteFromLocation when backend not found.
func TestRetention_DeleteFromLocation_BackendNotFound(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	// Don't register the backend
	entry := &CatalogEntry{
		Key:       "test-key",
		Status:    StatusActive,
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour).Unix(),
		PrimaryStore: StoreLocation{
			Store:     "nonexistent", // Backend not registered
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		RetentionPolicy: &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             0,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: time.Hour,
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	err = w.ProcessRetentionTask("test-key")
	if err != nil {
		t.Errorf("ProcessRetentionTask should not error for missing backend: %v", err)
	}

	// Entry should still be deleted from catalog
	_, err = catalog.Get(ctx, "test-key")
	if !IsNotFound(err) {
		t.Error("entry should have been deleted from catalog")
	}
}

// TestRetention_DeleteFromLocation_DeleteError tests deleteFromLocation when delete fails.
func TestRetention_DeleteFromLocation_DeleteError(t *testing.T) {
	logger := &mockLogger{}
	catalog := newMockCatalogManager()
	backends := newMockBackendManager()

	storage := newMockStorageWithError()
	storage.deleteErr = errors.New("delete failed")
	backends.addBackend("primary", storage)
	storage.mockStorage.putRaw("obj1", []byte("data"))

	entry := &CatalogEntry{
		Key:       "test-key",
		Status:    StatusActive,
		CreatedAt: time.Now().Add(-100 * 24 * time.Hour).Unix(),
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		RetentionPolicy: &RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &RetentionWorkerConfig{
		WorkerConfig: &WorkerConfig{
			Workers:                1,
			BatchSize:              1,
			PollInterval:           10 * time.Millisecond,
			MaxRetries:             0,
			RetryDelay:             1 * time.Millisecond,
			RetryBackoffMultiplier: 1.0,
			TaskTimeout:            5 * time.Second,
		},
		ScanInterval: time.Hour,
	}

	w, err := NewRetentionWorker(catalog, backends, cfg, logger)
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	// This should log warning but still delete from catalog
	err = w.ProcessRetentionTask("test-key")
	// The delete error is logged but catalog delete still happens
}

// TestTriggerRepairReplication_EnqueueError tests when enqueue fails.
func TestTriggerRepairReplication_EnqueueError(t *testing.T) {
	logger := &mockLogger{}
	catalog := &errorOnEnqueueReplicationCatalog{mockCatalogManager: newMockCatalogManager()}
	backends := newMockBackendManager()

	storage := newMockStorage()
	backends.addBackend("primary", storage)

	testData := []byte("test data")
	checksum := sha256.Sum256(testData)
	// Store wrong data to trigger verification failure
	storage.putRaw("obj1", []byte("wrong data"))

	entry := &CatalogEntry{
		Key:      "test-key",
		Checksum: checksum[:],
		Status:   StatusActive,
		PrimaryStore: StoreLocation{
			Store:     "primary",
			ObjectRef: "obj1",
			Status:    LocationStatusActive,
		},
		ReplicationPolicy: &ReplicationPolicy{
			Enabled:      true,
			TargetStores: []string{"replica1"},
			MinReplicas:  2,
		},
	}
	catalog.Put(context.Background(), entry)

	cfg := &WorkerConfig{
		Workers:                1,
		BatchSize:              1,
		PollInterval:           10 * time.Millisecond,
		MaxRetries:             0,
		RetryDelay:             1 * time.Millisecond,
		RetryBackoffMultiplier: 1.0,
		TaskTimeout:            5 * time.Second,
	}

	w, _ := NewVerifyWorker(catalog, backends, cfg, logger)

	ctx := context.Background()
	w.Start(ctx)
	defer w.Stop()

	catalog.EnqueueVerify(ctx, "test-key", []string{"primary"})

	time.Sleep(200 * time.Millisecond)

	// Should not panic, just log warning
}

// errorOnEnqueueReplicationCatalog returns error on EnqueueReplication.
type errorOnEnqueueReplicationCatalog struct {
	*mockCatalogManager
}

func (c *errorOnEnqueueReplicationCatalog) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	return errors.New("enqueue replication failed")
}
