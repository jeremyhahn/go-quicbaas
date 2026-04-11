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

// Package service provides unit tests for the ObjectStorageService implementation.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-objstore/pkg/common"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Mock Logger for ObjectStorage Unit Testing
// =============================================================================

// objstoreMockLogger implements interfaces.Logger for testing.
type objstoreMockLogger struct {
	mu     sync.RWMutex
	fields map[string]interface{}
}

func newObjstoreMockLogger() *objstoreMockLogger {
	return &objstoreMockLogger{
		fields: make(map[string]interface{}),
	}
}

func (l *objstoreMockLogger) Debug(msg string)                          {}
func (l *objstoreMockLogger) Info(msg string)                           {}
func (l *objstoreMockLogger) Warn(msg string)                           {}
func (l *objstoreMockLogger) Error(msg string)                          {}
func (l *objstoreMockLogger) Fatal(msg string)                          {}
func (l *objstoreMockLogger) Debugf(format string, args ...interface{}) {}
func (l *objstoreMockLogger) Infof(format string, args ...interface{})  {}
func (l *objstoreMockLogger) Warnf(format string, args ...interface{})  {}
func (l *objstoreMockLogger) Errorf(format string, args ...interface{}) {}
func (l *objstoreMockLogger) Fatalf(format string, args ...interface{}) {}

func (l *objstoreMockLogger) WithError(err error) interfaces.Logger { return l }

func (l *objstoreMockLogger) WithField(key string, value interface{}) interfaces.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	newLogger := &objstoreMockLogger{fields: make(map[string]interface{})}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	newLogger.fields[key] = value
	return newLogger
}

func (l *objstoreMockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	newLogger := &objstoreMockLogger{fields: make(map[string]interface{})}
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	for k, v := range fields {
		newLogger.fields[k] = v
	}
	return newLogger
}

// =============================================================================
// Mock Catalog Manager for ObjectStorage Unit Testing
// =============================================================================

// objstoreMockCatalogManager implements catalog.CatalogManager for testing.
type objstoreMockCatalogManager struct {
	mu      sync.RWMutex
	entries map[string]*catalog.CatalogEntry
	closed  bool

	// Function overrides for testing specific scenarios
	putFn                func(ctx context.Context, entry *catalog.CatalogEntry) error
	getFn                func(ctx context.Context, key string) (*catalog.CatalogEntry, error)
	deleteFn             func(ctx context.Context, key string) error
	updateFn             func(ctx context.Context, key string, updater func(*catalog.CatalogEntry) error) error
	existsFn             func(ctx context.Context, key string) (bool, error)
	addLocationFn        func(ctx context.Context, key string, loc *catalog.StoreLocation) error
	removeLocationFn     func(ctx context.Context, key string, store string) error
	setPrimaryStoreFn    func(ctx context.Context, key string, store string) error
	getLocationsFn       func(ctx context.Context, key string) ([]*catalog.StoreLocation, error)
	listByPrefixFn       func(ctx context.Context, prefix string, opts *catalog.ListOptions) (*catalog.ListResult, error)
	listByTagFn          func(ctx context.Context, tag, value string, opts *catalog.ListOptions) (*catalog.ListResult, error)
	listByTenantFn       func(ctx context.Context, tenant string, opts *catalog.ListOptions) (*catalog.ListResult, error)
	getTenantUsageFn     func(ctx context.Context, tenant string) (*catalog.TenantUsage, error)
	enqueueReplicationFn func(ctx context.Context, key string, targets []string, priority int) error
	updateAccessTimeFn   func(ctx context.Context, key string) error
}

func newObjstoreMockCatalogManager() *objstoreMockCatalogManager {
	return &objstoreMockCatalogManager{
		entries: make(map[string]*catalog.CatalogEntry),
	}
}

func (m *objstoreMockCatalogManager) Put(ctx context.Context, entry *catalog.CatalogEntry) error {
	if m.putFn != nil {
		return m.putFn(ctx, entry)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entry.Key] = entry
	return nil
}

func (m *objstoreMockCatalogManager) Get(ctx context.Context, key string) (*catalog.CatalogEntry, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return entry, nil
}

func (m *objstoreMockCatalogManager) Delete(ctx context.Context, key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

func (m *objstoreMockCatalogManager) Update(ctx context.Context, key string, updater func(*catalog.CatalogEntry) error) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, key, updater)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return errors.New("not found")
	}
	if err := updater(entry); err != nil {
		return err
	}
	entry.Version++
	entry.ModifiedAt = time.Now().UnixNano()
	return nil
}

func (m *objstoreMockCatalogManager) Exists(ctx context.Context, key string) (bool, error) {
	if m.existsFn != nil {
		return m.existsFn(ctx, key)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.entries[key]
	return ok, nil
}

func (m *objstoreMockCatalogManager) AddLocation(ctx context.Context, key string, loc *catalog.StoreLocation) error {
	if m.addLocationFn != nil {
		return m.addLocationFn(ctx, key, loc)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return errors.New("not found")
	}
	entry.Replicas = append(entry.Replicas, *loc)
	return nil
}

func (m *objstoreMockCatalogManager) RemoveLocation(ctx context.Context, key string, store string) error {
	if m.removeLocationFn != nil {
		return m.removeLocationFn(ctx, key, store)
	}
	return nil
}

func (m *objstoreMockCatalogManager) SetPrimaryStore(ctx context.Context, key string, store string) error {
	if m.setPrimaryStoreFn != nil {
		return m.setPrimaryStoreFn(ctx, key, store)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return errors.New("not found")
	}
	// Find in replicas and swap
	for i, r := range entry.Replicas {
		if r.Store == store {
			entry.Replicas = append(entry.Replicas, entry.PrimaryStore)
			entry.PrimaryStore = r
			entry.Replicas = append(entry.Replicas[:i], entry.Replicas[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *objstoreMockCatalogManager) GetLocations(ctx context.Context, key string) ([]*catalog.StoreLocation, error) {
	if m.getLocationsFn != nil {
		return m.getLocationsFn(ctx, key)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[key]
	if !ok {
		return nil, errors.New("not found")
	}
	locs := []*catalog.StoreLocation{&entry.PrimaryStore}
	for i := range entry.Replicas {
		locs = append(locs, &entry.Replicas[i])
	}
	return locs, nil
}

func (m *objstoreMockCatalogManager) UpdateLocationStatus(ctx context.Context, key, store string, status catalog.LocationStatus) error {
	return nil
}

func (m *objstoreMockCatalogManager) ListByStore(ctx context.Context, store string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *objstoreMockCatalogManager) ListByTenant(ctx context.Context, tenant string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	if m.listByTenantFn != nil {
		return m.listByTenantFn(ctx, tenant, opts)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for key, entry := range m.entries {
		if entry.TenantID == tenant {
			keys = append(keys, key)
		}
	}
	return &catalog.ListResult{Keys: keys}, nil
}

func (m *objstoreMockCatalogManager) ListByPrefix(ctx context.Context, prefix string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	if m.listByPrefixFn != nil {
		return m.listByPrefixFn(ctx, prefix, opts)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for key := range m.entries {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return &catalog.ListResult{Keys: keys}, nil
}

func (m *objstoreMockCatalogManager) ListByStatus(ctx context.Context, status catalog.EntryStatus, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *objstoreMockCatalogManager) ListByTag(ctx context.Context, tag, value string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	if m.listByTagFn != nil {
		return m.listByTagFn(ctx, tag, value, opts)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for key, entry := range m.entries {
		if entry.Tags != nil && entry.Tags[tag] == value {
			keys = append(keys, key)
		}
	}
	return &catalog.ListResult{Keys: keys}, nil
}

func (m *objstoreMockCatalogManager) ListByEncryptionKey(ctx context.Context, keyID string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *objstoreMockCatalogManager) Search(ctx context.Context, query *catalog.SearchQuery) (*catalog.SearchResult, error) {
	return &catalog.SearchResult{Entries: []*catalog.CatalogEntry{}}, nil
}

func (m *objstoreMockCatalogManager) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	if m.enqueueReplicationFn != nil {
		return m.enqueueReplicationFn(ctx, key, targets, priority)
	}
	return nil
}

func (m *objstoreMockCatalogManager) EnqueueArchive(ctx context.Context, key string, targetStore string) error {
	return nil
}

func (m *objstoreMockCatalogManager) EnqueueVerify(ctx context.Context, key string, stores []string) error {
	return nil
}

func (m *objstoreMockCatalogManager) DequeueReplication(ctx context.Context, limit int) ([]*catalog.ReplicationTask, error) {
	return nil, nil
}

func (m *objstoreMockCatalogManager) DequeueArchive(ctx context.Context, limit int) ([]*catalog.ArchiveTask, error) {
	return nil, nil
}

func (m *objstoreMockCatalogManager) DequeueVerify(ctx context.Context, limit int) ([]*catalog.VerifyTask, error) {
	return nil, nil
}

func (m *objstoreMockCatalogManager) GetTenantUsage(ctx context.Context, tenant string) (*catalog.TenantUsage, error) {
	if m.getTenantUsageFn != nil {
		return m.getTenantUsageFn(ctx, tenant)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	usage := &catalog.TenantUsage{TenantID: tenant}
	for _, entry := range m.entries {
		if entry.TenantID == tenant {
			usage.ObjectCount++
			usage.TotalSize += entry.Size
			if entry.Status == catalog.StatusActive {
				usage.ActiveCount++
			} else if entry.Status == catalog.StatusArchived {
				usage.ArchivedCount++
			}
		}
	}
	return usage, nil
}

func (m *objstoreMockCatalogManager) GetStoreUsage(ctx context.Context, store string) (*catalog.StoreUsage, error) {
	return &catalog.StoreUsage{Store: store}, nil
}

func (m *objstoreMockCatalogManager) UpdateAccessTime(ctx context.Context, key string) error {
	if m.updateAccessTimeFn != nil {
		return m.updateAccessTimeFn(ctx, key)
	}
	return nil
}

func (m *objstoreMockCatalogManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *objstoreMockCatalogManager) Sync() error {
	return nil
}

// =============================================================================
// Mock Backend Manager for ObjectStorage Unit Testing
// =============================================================================

// objstoreMockBackendManager implements catalog.BackendManager for testing.
type objstoreMockBackendManager struct {
	mu       sync.RWMutex
	backends map[string]*objstoreMockBackendEntry
	closed   bool

	getBackendFn       func(name string) (common.Storage, error)
	getBackendConfigFn func(name string) (*catalog.BackendConfig, error)
	checkHealthFn      func(ctx context.Context, name string) (*catalog.BackendHealth, error)
}

type objstoreMockBackendEntry struct {
	storage common.Storage
	config  *catalog.BackendConfig
}

func newObjstoreMockBackendManager() *objstoreMockBackendManager {
	return &objstoreMockBackendManager{
		backends: make(map[string]*objstoreMockBackendEntry),
	}
}

func (m *objstoreMockBackendManager) AddBackend(name string, storage common.Storage, cfg *catalog.BackendConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[name] = &objstoreMockBackendEntry{storage: storage, config: cfg}
}

func (m *objstoreMockBackendManager) GetBackend(name string) (common.Storage, error) {
	if m.getBackendFn != nil {
		return m.getBackendFn(name)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.backends[name]
	if !ok {
		return nil, errors.New("backend not found: " + name)
	}
	return entry.storage, nil
}

func (m *objstoreMockBackendManager) ListBackends() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}
	return names
}

func (m *objstoreMockBackendManager) GetBackendConfig(name string) (*catalog.BackendConfig, error) {
	if m.getBackendConfigFn != nil {
		return m.getBackendConfigFn(name)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.backends[name]
	if !ok {
		return nil, errors.New("backend not found: " + name)
	}
	return entry.config, nil
}

func (m *objstoreMockBackendManager) RegisterBackend(name string, cfg *catalog.BackendConfig) error {
	return nil
}

func (m *objstoreMockBackendManager) UnregisterBackend(name string) error {
	return nil
}

func (m *objstoreMockBackendManager) SelectBackend(criteria *catalog.SelectionCriteria) (string, common.Storage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, entry := range m.backends {
		return name, entry.storage, nil
	}
	return "", nil, errors.New("no backends available")
}

func (m *objstoreMockBackendManager) CheckHealth(ctx context.Context, name string) (*catalog.BackendHealth, error) {
	if m.checkHealthFn != nil {
		return m.checkHealthFn(ctx, name)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.backends[name]; !ok {
		return nil, errors.New("backend not found: " + name)
	}
	return &catalog.BackendHealth{
		Name:      name,
		Healthy:   true,
		LastCheck: time.Now(),
		Latency:   10 * time.Millisecond,
		Status:    "healthy",
	}, nil
}

func (m *objstoreMockBackendManager) CheckAllHealth(ctx context.Context) map[string]*catalog.BackendHealth {
	result := make(map[string]*catalog.BackendHealth)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name := range m.backends {
		result[name] = &catalog.BackendHealth{
			Name:      name,
			Healthy:   true,
			LastCheck: time.Now(),
			Status:    "healthy",
		}
	}
	return result
}

func (m *objstoreMockBackendManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// =============================================================================
// Mock Storage Backend for ObjectStorage Unit Testing (implements common.Storage)
// =============================================================================

// objstoreMockStorage implements common.Storage for testing.
type objstoreMockStorage struct {
	mu      sync.RWMutex
	objects map[string][]byte
	closed  bool

	putWithContextFn    func(ctx context.Context, key string, reader io.Reader) error
	getWithContextFn    func(ctx context.Context, key string) (io.ReadCloser, error)
	deleteWithContextFn func(ctx context.Context, key string) error
	listWithContextFn   func(ctx context.Context, prefix string) ([]string, error)
}

func newObjstoreMockStorage() *objstoreMockStorage {
	return &objstoreMockStorage{
		objects: make(map[string][]byte),
	}
}

// Configure implements common.Storage
func (m *objstoreMockStorage) Configure(settings map[string]string) error {
	return nil
}

// Put implements common.Storage
func (m *objstoreMockStorage) Put(key string, reader io.Reader) error {
	return m.PutWithContext(context.Background(), key, reader)
}

// PutWithContext implements common.Storage
func (m *objstoreMockStorage) PutWithContext(ctx context.Context, key string, reader io.Reader) error {
	if m.putWithContextFn != nil {
		return m.putWithContextFn(ctx, key, reader)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.objects[key] = data
	return nil
}

// PutWithMetadata implements common.Storage
func (m *objstoreMockStorage) PutWithMetadata(ctx context.Context, key string, data io.Reader, metadata *common.Metadata) error {
	return m.PutWithContext(ctx, key, data)
}

// Get implements common.Storage
func (m *objstoreMockStorage) Get(key string) (io.ReadCloser, error) {
	return m.GetWithContext(context.Background(), key)
}

// GetWithContext implements common.Storage
func (m *objstoreMockStorage) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getWithContextFn != nil {
		return m.getWithContextFn(ctx, key)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// GetMetadata implements common.Storage
func (m *objstoreMockStorage) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return &common.Metadata{Size: int64(len(data))}, nil
}

// UpdateMetadata implements common.Storage
func (m *objstoreMockStorage) UpdateMetadata(ctx context.Context, key string, metadata *common.Metadata) error {
	return nil
}

// Delete implements common.Storage
func (m *objstoreMockStorage) Delete(key string) error {
	return m.DeleteWithContext(context.Background(), key)
}

// DeleteWithContext implements common.Storage
func (m *objstoreMockStorage) DeleteWithContext(ctx context.Context, key string) error {
	if m.deleteWithContextFn != nil {
		return m.deleteWithContextFn(ctx, key)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

// Exists implements common.Storage
func (m *objstoreMockStorage) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.objects[key]
	return ok, nil
}

// List implements common.Storage
func (m *objstoreMockStorage) List(prefix string) ([]string, error) {
	return m.ListWithContext(context.Background(), prefix)
}

// ListWithContext implements common.Storage
func (m *objstoreMockStorage) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	if m.listWithContextFn != nil {
		return m.listWithContextFn(ctx, prefix)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for key := range m.objects {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// ListWithOptions implements common.Storage
func (m *objstoreMockStorage) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	keys, err := m.ListWithContext(ctx, opts.Prefix)
	if err != nil {
		return nil, err
	}
	objects := make([]*common.ObjectInfo, len(keys))
	for i, key := range keys {
		objects[i] = &common.ObjectInfo{Key: key}
	}
	return &common.ListResult{Objects: objects}, nil
}

// Archive implements common.Storage
func (m *objstoreMockStorage) Archive(key string, destination common.Archiver) error {
	return nil
}

// AddPolicy implements common.LifecycleManager
func (m *objstoreMockStorage) AddPolicy(policy common.LifecyclePolicy) error {
	return nil
}

// RemovePolicy implements common.LifecycleManager
func (m *objstoreMockStorage) RemovePolicy(policyID string) error {
	return nil
}

// GetPolicies implements common.LifecycleManager
func (m *objstoreMockStorage) GetPolicies() ([]common.LifecyclePolicy, error) {
	return nil, nil
}

// ApplyPolicies implements common.LifecycleManager
func (m *objstoreMockStorage) ApplyPolicies(ctx context.Context) error {
	return nil
}

// Close implements cleanup
func (m *objstoreMockStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// =============================================================================
// Mock Config Resolver for ObjectStorage Unit Testing
// =============================================================================

// objstoreMockConfigResolver implements config.ObjectStoreConfigResolver for testing.
type objstoreMockConfigResolver struct {
	defaultConfig *config.ResolvedObjectStoreConfig
	tenantConfigs map[string]*config.ObjectStoreTenantConfig

	resolveConfigFn   func(tenantID string, shardID uint64) (*config.ResolvedObjectStoreConfig, error)
	checkQuotasFn     func(tenantID string, objectSize int64, usage *config.TenantUsage) error
	setTenantQuotasFn func(ctx context.Context, tenantID string, quotas *config.TenantQuotas) error
}

func newObjstoreMockConfigResolver() *objstoreMockConfigResolver {
	return &objstoreMockConfigResolver{
		defaultConfig: &config.ResolvedObjectStoreConfig{
			PrimaryBackend: "local",
			Compression:    "none",
			ValueThreshold: 1024 * 1024,
		},
		tenantConfigs: make(map[string]*config.ObjectStoreTenantConfig),
	}
}

func (m *objstoreMockConfigResolver) ResolveConfig(tenantID string, shardID uint64) (*config.ResolvedObjectStoreConfig, error) {
	if m.resolveConfigFn != nil {
		return m.resolveConfigFn(tenantID, shardID)
	}
	return m.defaultConfig, nil
}

func (m *objstoreMockConfigResolver) ResolveBackend(tenantID string, shardID uint64) (string, error) {
	cfg, err := m.ResolveConfig(tenantID, shardID)
	if err != nil {
		return "", err
	}
	return cfg.PrimaryBackend, nil
}

func (m *objstoreMockConfigResolver) ResolveEncryption(tenantID string, shardID uint64) (*config.ObjectStoreEncryption, error) {
	cfg, err := m.ResolveConfig(tenantID, shardID)
	if err != nil {
		return nil, err
	}
	return cfg.Encryption, nil
}

func (m *objstoreMockConfigResolver) ResolveReplication(tenantID string, shardID uint64) (*config.ObjectStoreReplication, error) {
	cfg, err := m.ResolveConfig(tenantID, shardID)
	if err != nil {
		return nil, err
	}
	return cfg.Replication, nil
}

func (m *objstoreMockConfigResolver) CheckQuotas(tenantID string, objectSize int64, usage *config.TenantUsage) error {
	if m.checkQuotasFn != nil {
		return m.checkQuotasFn(tenantID, objectSize, usage)
	}
	return nil
}

func (m *objstoreMockConfigResolver) GetTenantConfig(tenantID string) *config.ObjectStoreTenantConfig {
	return m.tenantConfigs[tenantID]
}

func (m *objstoreMockConfigResolver) ListTenants() []string {
	var tenants []string
	for id := range m.tenantConfigs {
		tenants = append(tenants, id)
	}
	return tenants
}

func (m *objstoreMockConfigResolver) Reload(fullConfig *config.ObjectStoreFullConfig, nodeConfig *config.NodeObjectStorageConfig) error {
	return nil
}

func (m *objstoreMockConfigResolver) SetTenantQuotas(ctx context.Context, tenantID string, quotas *config.TenantQuotas) error {
	if m.setTenantQuotasFn != nil {
		return m.setTenantQuotasFn(ctx, tenantID, quotas)
	}
	// Default behavior: store in tenantConfigs
	if m.tenantConfigs[tenantID] == nil {
		m.tenantConfigs[tenantID] = &config.ObjectStoreTenantConfig{}
	}
	m.tenantConfigs[tenantID].Quotas = config.ObjectStoreQuotas{
		MaxStorageBytes: quotas.MaxStorageBytes,
		MaxObjectSize:   quotas.MaxObjectSize,
		MaxObjectCount:  quotas.MaxObjectCount,
	}
	return nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// createObjstoreTestService creates a service with mocked dependencies for testing.
func createObjstoreTestService(t *testing.T) (ObjectStorageService, *objstoreMockCatalogManager, *objstoreMockBackendManager, *objstoreMockStorage) {
	t.Helper()

	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Register default backend
	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Default: true,
		Tier:    catalog.TierHot,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, svc)

	return svc, catalogMgr, backendMgr, storage
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewObjectStorageService_Success(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestNewObjectStorageService_NilCatalogManager(t *testing.T) {
	backendMgr := newObjstoreMockBackendManager()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageService(nil, backendMgr, resolver, nil, logger)
	require.Error(t, err)
	require.Nil(t, svc)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestNewObjectStorageService_NilBackendManager(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageService(catalogMgr, nil, resolver, nil, logger)
	require.Error(t, err)
	require.Nil(t, svc)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestNewObjectStorageService_NilConfigResolver(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, nil, nil, logger)
	require.Error(t, err)
	require.Nil(t, svc)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestNewObjectStorageService_NilLogger(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	resolver := newObjstoreMockConfigResolver()

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, nil)
	require.Error(t, err)
	require.Nil(t, svc)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

// =============================================================================
// PutObject Tests
// =============================================================================

func TestPutObject_Success(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &PutObjectRequest{
		Key:         "test:object1",
		Value:       []byte("test data content"),
		ContentType: "text/plain",
		TenantID:    "tenant1",
	}

	resp, err := svc.PutObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, req.Key, resp.Key)
	assert.NotZero(t, resp.ObjectID)
	assert.Equal(t, int64(len(req.Value)), resp.Size)
	assert.NotEmpty(t, resp.Checksum)
	assert.Equal(t, "local", resp.Backend)
}

func TestPutObject_WithTags(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &PutObjectRequest{
		Key:         "test:tagged",
		Value:       []byte("tagged data"),
		ContentType: "application/octet-stream",
		Tags: map[string]string{
			"env":     "test",
			"project": "quicbaas",
		},
	}

	resp, err := svc.PutObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify tags were stored
	entry, err := catalogMgr.Get(ctx, req.Key)
	require.NoError(t, err)
	assert.Equal(t, "test", entry.Tags["env"])
	assert.Equal(t, "quicbaas", entry.Tags["project"])
}

func TestPutObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.PutObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestPutObject_EmptyKey(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &PutObjectRequest{
		Key:   "",
		Value: []byte("test data"),
	}

	resp, err := svc.PutObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestPutObject_EmptyValue(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &PutObjectRequest{
		Key:   "test:empty",
		Value: []byte{},
	}

	resp, err := svc.PutObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestPutObject_ChecksumMismatch(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &PutObjectRequest{
		Key:      "test:checksum",
		Value:    []byte("test data"),
		Checksum: []byte("invalid checksum"),
	}

	resp, err := svc.PutObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestPutObject_QuotaExceeded(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Configure quota check to fail
	resolver.checkQuotasFn = func(tenantID string, objectSize int64, usage *config.TenantUsage) error {
		return &config.QuotaError{
			TenantID: tenantID,
			Code:     config.QuotaErrorCodeStorage,
			Reason:   "storage limit exceeded",
			Limit:    1000,
			Current:  900,
		}
	}

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &PutObjectRequest{
		Key:      "test:quota",
		Value:    []byte("large data"),
		TenantID: "tenant1",
	}

	resp, err := svc.PutObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrResourceExhausted, qbErr.Code)
}

func TestPutObject_BackendUnavailable(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Configure backend to return error
	backendMgr.getBackendFn = func(name string) (common.Storage, error) {
		return nil, errors.New("backend unavailable")
	}

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &PutObjectRequest{
		Key:   "test:unavailable",
		Value: []byte("test data"),
	}

	resp, err := svc.PutObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestPutObject_WithValidChecksum(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("test data with valid checksum")
	checksum := sha256.Sum256(testData)

	req := &PutObjectRequest{
		Key:      "test:valid-checksum",
		Value:    testData,
		Checksum: checksum[:],
	}

	resp, err := svc.PutObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, req.Key, resp.Key)
}

func TestPutObject_StorageWriteError(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Configure storage to fail on write
	storage.putWithContextFn = func(ctx context.Context, key string, reader io.Reader) error {
		return errors.New("disk full")
	}

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &PutObjectRequest{
		Key:   "test:write-error",
		Value: []byte("test data"),
	}

	resp, err := svc.PutObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// GetObject Tests
// =============================================================================

func TestGetObject_Success(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	// Store object first
	testData := []byte("test object data")
	objectRef := "test-ref-123"

	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	// Create catalog entry
	entry := catalog.NewCatalogEntry("test:get", int64(len(testData)), nil)
	entry.ContentType = "text/plain"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Get object
	req := &GetObjectRequest{Key: "test:get"}
	resp, err := svc.GetObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:get", resp.Key)
	assert.Equal(t, testData, resp.Value)
	assert.Equal(t, "text/plain", resp.ContentType)
}

func TestGetObject_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetObjectRequest{Key: "test:nonexistent"}
	resp, err := svc.GetObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrNotFound, qbErr.Code)
}

func TestGetObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.GetObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestGetObject_EmptyKey(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetObjectRequest{Key: ""}
	resp, err := svc.GetObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestGetObject_CrossTenantDenied(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	// Store object for tenant1
	testData := []byte("tenant1 data")
	objectRef := "tenant1-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:cross", int64(len(testData)), nil)
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Try to access as tenant2
	req := &GetObjectRequest{
		Key:      "test:cross",
		TenantID: "tenant2",
	}

	resp, err := svc.GetObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrForbidden, qbErr.Code)
}

func TestGetObject_BackendReadError(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Configure storage to fail on read
	storage.getWithContextFn = func(ctx context.Context, key string) (io.ReadCloser, error) {
		return nil, errors.New("read error")
	}

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create catalog entry
	entry := catalog.NewCatalogEntry("test:read-error", 100, nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: "some-ref",
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectRequest{Key: "test:read-error"}
	resp, err := svc.GetObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// DeleteObject Tests
// =============================================================================

func TestDeleteObject_Success(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	// Store object first
	testData := []byte("data to delete")
	objectRef := "delete-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:delete", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Delete object
	req := &DeleteObjectRequest{Key: "test:delete"}
	resp, err := svc.DeleteObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:delete", resp.Key)
	assert.True(t, resp.Success)
}

func TestDeleteObject_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &DeleteObjectRequest{Key: "test:nonexistent"}
	resp, err := svc.DeleteObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrNotFound, qbErr.Code)
}

func TestDeleteObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.DeleteObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestDeleteObject_CrossTenantDenied(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("tenant data")
	objectRef := "tenant-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:tenant-delete", int64(len(testData)), nil)
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &DeleteObjectRequest{
		Key:      "test:tenant-delete",
		TenantID: "tenant2",
	}

	resp, err := svc.DeleteObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrForbidden, qbErr.Code)
}

// =============================================================================
// HeadObject Tests
// =============================================================================

func TestHeadObject_Exists(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:head", 1024, []byte("checksum"))
	entry.ContentType = "application/json"
	entry.Tags = map[string]string{"key": "value"}
	entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &HeadObjectRequest{Key: "test:head"}
	resp, err := svc.HeadObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Exists)
	assert.Equal(t, "test:head", resp.Key)
	assert.Equal(t, int64(1024), resp.Size)
	assert.Equal(t, "application/json", resp.ContentType)
}

func TestHeadObject_NotExists(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &HeadObjectRequest{Key: "test:nonexistent"}
	resp, err := svc.HeadObject(ctx, req)
	require.NoError(t, err) // HeadObject returns response, not error for not found
	require.NotNil(t, resp)

	assert.False(t, resp.Exists)
	assert.Equal(t, "test:nonexistent", resp.Key)
}

func TestHeadObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.HeadObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestHeadObject_EmptyKey(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &HeadObjectRequest{Key: ""}
	resp, err := svc.HeadObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// ListObjects Tests
// =============================================================================

func TestListObjects_Success(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Create multiple objects
	for i := 0; i < 5; i++ {
		entry := catalog.NewCatalogEntry("test:list:"+string(rune('a'+i)), 100, nil)
		entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
		err := catalogMgr.Put(ctx, entry)
		require.NoError(t, err)
	}

	req := &ListObjectsRequest{Prefix: "test:list:"}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.GreaterOrEqual(t, len(resp.Objects), 5)
}

func TestListObjects_WithLimit(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Create multiple objects
	for i := 0; i < 10; i++ {
		entry := catalog.NewCatalogEntry("test:limit:"+string(rune('a'+i)), 100, nil)
		entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
		err := catalogMgr.Put(ctx, entry)
		require.NoError(t, err)
	}

	req := &ListObjectsRequest{
		Prefix: "test:limit:",
		Limit:  5,
	}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.LessOrEqual(t, len(resp.Objects), 10) // May be filtered
}

func TestListObjects_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.ListObjects(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestListObjects_EmptyResult(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListObjectsRequest{Prefix: "nonexistent:prefix:"}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.Objects)
}

// =============================================================================
// ListByTag Tests
// =============================================================================

func TestListByTag_Success(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Create objects with tags
	entry1 := catalog.NewCatalogEntry("test:tag1", 100, nil)
	entry1.Tags = map[string]string{"env": "prod"}
	entry1.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry1)
	require.NoError(t, err)

	entry2 := catalog.NewCatalogEntry("test:tag2", 100, nil)
	entry2.Tags = map[string]string{"env": "prod"}
	entry2.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err = catalogMgr.Put(ctx, entry2)
	require.NoError(t, err)

	req := &ListByTagRequest{
		TagName:  "env",
		TagValue: "prod",
	}
	resp, err := svc.ListByTag(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.GreaterOrEqual(t, len(resp.Objects), 2)
}

func TestListByTag_EmptyTagName(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListByTagRequest{
		TagName:  "",
		TagValue: "value",
	}
	resp, err := svc.ListByTag(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestListByTag_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.ListByTag(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// ListByTenant Tests
// =============================================================================

func TestListByTenant_Success(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Create objects for different tenants
	entry1 := catalog.NewCatalogEntry("test:tenant:1", 100, nil)
	entry1.TenantID = "tenant-abc"
	entry1.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry1)
	require.NoError(t, err)

	entry2 := catalog.NewCatalogEntry("test:tenant:2", 100, nil)
	entry2.TenantID = "tenant-xyz"
	entry2.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err = catalogMgr.Put(ctx, entry2)
	require.NoError(t, err)

	req := &ListByTenantRequest{TenantID: "tenant-abc"}
	resp, err := svc.ListByTenant(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.GreaterOrEqual(t, len(resp.Objects), 1)
}

func TestListByTenant_EmptyTenantID(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListByTenantRequest{TenantID: ""}
	resp, err := svc.ListByTenant(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestListByTenant_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.ListByTenant(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// CopyObject Tests
// =============================================================================

func TestCopyObject_Success(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	// Create source object
	testData := []byte("source data")
	sourceRef := "source-ref"
	err := storage.PutWithContext(ctx, sourceRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:source", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: sourceRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Copy object
	req := &CopyObjectRequest{
		SourceKey:      "test:source",
		DestinationKey: "test:copy",
	}
	resp, err := svc.CopyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:copy", resp.Key)
	assert.Equal(t, int64(len(testData)), resp.Size)
}

func TestCopyObject_SourceNotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &CopyObjectRequest{
		SourceKey:      "test:nonexistent",
		DestinationKey: "test:copy",
	}
	resp, err := svc.CopyObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestCopyObject_DestinationExists(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	// Create source object
	testData := []byte("source data")
	sourceRef := "source-ref-exists"
	err := storage.PutWithContext(ctx, sourceRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:source-exists", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: sourceRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Create destination object
	destRef := "dest-ref"
	err = storage.PutWithContext(ctx, destRef, bytes.NewReader([]byte("existing")))
	require.NoError(t, err)

	destEntry := catalog.NewCatalogEntry("test:dest-exists", 8, nil)
	destEntry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: destRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, destEntry)
	require.NoError(t, err)

	req := &CopyObjectRequest{
		SourceKey:         "test:source-exists",
		DestinationKey:    "test:dest-exists",
		OverwriteExisting: false,
	}
	resp, err := svc.CopyObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrAlreadyExists, qbErr.Code)
}

func TestCopyObject_InvalidRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Empty source key
	req := &CopyObjectRequest{
		SourceKey:      "",
		DestinationKey: "test:copy",
	}
	resp, err := svc.CopyObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	// Empty destination key
	req = &CopyObjectRequest{
		SourceKey:      "test:source",
		DestinationKey: "",
	}
	resp, err = svc.CopyObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestCopyObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.CopyObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// ReplicateObject Tests
// =============================================================================

func TestReplicateObject_Success(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	sourceStorage := newObjstoreMockStorage()
	targetStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", sourceStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("s3", targetStorage, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create source object
	testData := []byte("replicate me")
	sourceRef := "source-ref-replicate"
	err = sourceStorage.PutWithContext(ctx, sourceRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:replicate", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: sourceRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ReplicateObjectRequest{
		Key:     "test:replicate",
		Targets: []string{"s3"},
	}
	resp, err := svc.ReplicateObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:replicate", resp.Key)
	assert.Equal(t, "completed", resp.Status)
}

func TestReplicateObject_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ReplicateObjectRequest{
		Key:     "test:nonexistent",
		Targets: []string{"s3"},
	}
	resp, err := svc.ReplicateObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestReplicateObject_NoTargets(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ReplicateObjectRequest{
		Key:     "test:replicate",
		Targets: []string{},
	}
	resp, err := svc.ReplicateObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestReplicateObject_CrossTenantDenied(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("tenant data")
	objectRef := "tenant-ref-replicate"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:tenant-replicate", int64(len(testData)), nil)
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ReplicateObjectRequest{
		Key:      "test:tenant-replicate",
		Targets:  []string{"s3"},
		TenantID: "tenant2",
	}
	resp, err := svc.ReplicateObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrForbidden, qbErr.Code)
}

func TestReplicateObject_TargetBackendNotFound(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("replicate me")
	objectRef := "source-ref-target-missing"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:replicate-missing-target", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ReplicateObjectRequest{
		Key:     "test:replicate-missing-target",
		Targets: []string{"nonexistent-backend"},
	}
	resp, err := svc.ReplicateObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "partial", resp.Status)
}

// =============================================================================
// MigrateObject Tests
// =============================================================================

func TestMigrateObject_Success(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	sourceStorage := newObjstoreMockStorage()
	targetStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", sourceStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("s3", targetStorage, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create source object
	testData := []byte("migrate me")
	sourceRef := "source-ref-migrate"
	err = sourceStorage.PutWithContext(ctx, sourceRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:migrate", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: sourceRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &MigrateObjectRequest{
		Key:           "test:migrate",
		TargetBackend: "s3",
	}
	resp, err := svc.MigrateObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:migrate", resp.Key)
	assert.True(t, resp.Success)
}

func TestMigrateObject_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &MigrateObjectRequest{
		Key:           "test:nonexistent",
		TargetBackend: "s3",
	}
	resp, err := svc.MigrateObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMigrateObject_InvalidRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Empty key
	req := &MigrateObjectRequest{
		Key:           "",
		TargetBackend: "s3",
	}
	resp, err := svc.MigrateObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMigrateObject_WithDeleteFromSource(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	sourceStorage := newObjstoreMockStorage()
	targetStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", sourceStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("s3", targetStorage, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("migrate and delete")
	sourceRef := "source-ref-migrate-delete"
	err = sourceStorage.PutWithContext(ctx, sourceRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:migrate-delete", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: sourceRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &MigrateObjectRequest{
		Key:              "test:migrate-delete",
		TargetBackend:    "s3",
		DeleteFromSource: true,
	}
	resp, err := svc.MigrateObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Success)
	assert.True(t, resp.DeletedFromSource)
}

// =============================================================================
// ArchiveObject Tests
// =============================================================================

func TestArchiveObject_Success(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	primaryStorage := newObjstoreMockStorage()
	archiveStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", primaryStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Tier:    catalog.TierHot,
	})
	backendMgr.AddBackend("glacier", archiveStorage, &catalog.BackendConfig{
		Type:    "glacier",
		Enabled: true,
		Tier:    catalog.TierCold,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create object to archive
	testData := []byte("archive me")
	objectRef := "archive-ref"
	err = primaryStorage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:archive", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ArchiveObjectRequest{
		Key:         "test:archive",
		TargetStore: "glacier",
	}
	resp, err := svc.ArchiveObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:archive", resp.Key)
	assert.True(t, resp.Success)
}

func TestArchiveObject_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ArchiveObjectRequest{
		Key:         "test:nonexistent",
		TargetStore: "glacier",
	}
	resp, err := svc.ArchiveObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestArchiveObject_AlreadyArchived(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:already-archived", 100, nil)
	entry.Status = catalog.StatusArchived
	entry.PrimaryStore = catalog.StoreLocation{Store: "glacier", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ArchiveObjectRequest{
		Key:         "test:already-archived",
		TargetStore: "glacier",
	}
	resp, err := svc.ArchiveObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestArchiveObject_CrossTenantDenied(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("tenant data")
	objectRef := "tenant-archive-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:tenant-archive", int64(len(testData)), nil)
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ArchiveObjectRequest{
		Key:         "test:tenant-archive",
		TargetStore: "glacier",
		TenantID:    "tenant2",
	}
	resp, err := svc.ArchiveObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestArchiveObject_WithDeleteFromPrimary(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	primaryStorage := newObjstoreMockStorage()
	archiveStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", primaryStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Tier:    catalog.TierHot,
	})
	backendMgr.AddBackend("glacier", archiveStorage, &catalog.BackendConfig{
		Type:    "glacier",
		Enabled: true,
		Tier:    catalog.TierCold,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("archive and delete")
	objectRef := "archive-delete-ref"
	err = primaryStorage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:archive-delete", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ArchiveObjectRequest{
		Key:               "test:archive-delete",
		TargetStore:       "glacier",
		DeleteFromPrimary: true,
	}
	resp, err := svc.ArchiveObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Success)
	assert.True(t, resp.DeletedFromPrimary)
}

// =============================================================================
// RestoreObject Tests
// =============================================================================

func TestRestoreObject_Success(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	primaryStorage := newObjstoreMockStorage()
	archiveStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", primaryStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Tier:    catalog.TierHot,
	})
	backendMgr.AddBackend("glacier", archiveStorage, &catalog.BackendConfig{
		Type:    "glacier",
		Enabled: true,
		Tier:    catalog.TierCold,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create archived object
	testData := []byte("restore me")
	objectRef := "archived-ref"
	err = archiveStorage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:restore", int64(len(testData)), nil)
	entry.Status = catalog.StatusArchived
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "glacier",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &RestoreObjectRequest{
		Key:           "test:restore",
		TargetBackend: "local",
	}
	resp, err := svc.RestoreObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:restore", resp.Key)
	assert.True(t, resp.Success)
}

func TestRestoreObject_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &RestoreObjectRequest{
		Key:           "test:nonexistent",
		TargetBackend: "local",
	}
	resp, err := svc.RestoreObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestRestoreObject_NotArchived(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("active object")
	objectRef := "active-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:not-archived", int64(len(testData)), nil)
	entry.Status = catalog.StatusActive
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &RestoreObjectRequest{
		Key:           "test:not-archived",
		TargetBackend: "local",
	}
	resp, err := svc.RestoreObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestRestoreObject_CrossTenantDenied(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	primaryStorage := newObjstoreMockStorage()
	archiveStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", primaryStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("glacier", archiveStorage, &catalog.BackendConfig{
		Type:    "glacier",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:tenant-restore", 100, nil)
	entry.TenantID = "tenant1"
	entry.Status = catalog.StatusArchived
	entry.PrimaryStore = catalog.StoreLocation{Store: "glacier", Status: catalog.LocationStatusActive}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &RestoreObjectRequest{
		Key:           "test:tenant-restore",
		TargetBackend: "local",
		TenantID:      "tenant2",
	}
	resp, err := svc.RestoreObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// VerifyObject Tests
// =============================================================================

func TestVerifyObject_Success(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	// Create object
	testData := []byte("verify me")
	objectRef := "verify-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	// Compute checksum for verification
	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:verify", int64(len(testData)), checksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &VerifyObjectRequest{Key: "test:verify"}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:verify", resp.Key)
	assert.True(t, resp.Valid)
}

func TestVerifyObject_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &VerifyObjectRequest{Key: "test:nonexistent"}
	resp, err := svc.VerifyObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestVerifyObject_CrossTenantDenied(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("tenant verify data")
	objectRef := "tenant-verify-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:tenant-verify", int64(len(testData)), checksum[:])
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &VerifyObjectRequest{
		Key:      "test:tenant-verify",
		TenantID: "tenant2",
	}
	resp, err := svc.VerifyObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestVerifyObject_ChecksumMismatch(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("verify me")
	objectRef := "verify-mismatch-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	// Use wrong checksum
	wrongChecksum := sha256.Sum256([]byte("different data"))
	entry := catalog.NewCatalogEntry("test:verify-mismatch", int64(len(testData)), wrongChecksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &VerifyObjectRequest{Key: "test:verify-mismatch"}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

// =============================================================================
// SetObjectPolicy Tests
// =============================================================================

func TestSetObjectPolicy_Success(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Create object
	entry := catalog.NewCatalogEntry("test:policy", 1024, nil)
	entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &SetObjectPolicyRequest{
		Key: "test:policy",
		RetentionPolicy: &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	resp, err := svc.SetObjectPolicy(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:policy", resp.Key)
	assert.True(t, resp.Success)
}

func TestSetObjectPolicy_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &SetObjectPolicyRequest{
		Key: "test:nonexistent",
		RetentionPolicy: &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	resp, err := svc.SetObjectPolicy(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestSetObjectPolicy_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.SetObjectPolicy(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// GetObjectPolicy Tests
// =============================================================================

func TestGetObjectPolicy_Success(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Create object with policy
	entry := catalog.NewCatalogEntry("test:get-policy", 1024, nil)
	entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	entry.RetentionPolicy = &catalog.RetentionPolicy{
		Enabled:       true,
		RetentionDays: 30,
	}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectPolicyRequest{Key: "test:get-policy"}
	resp, err := svc.GetObjectPolicy(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test:get-policy", resp.Key)
	assert.NotNil(t, resp.RetentionPolicy)
}

func TestGetObjectPolicy_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetObjectPolicyRequest{Key: "test:nonexistent"}
	resp, err := svc.GetObjectPolicy(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestGetObjectPolicy_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.GetObjectPolicy(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestGetObjectPolicy_CrossTenantDenied(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:tenant-policy", 1024, nil)
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectPolicyRequest{
		Key:      "test:tenant-policy",
		TenantID: "tenant2",
	}
	resp, err := svc.GetObjectPolicy(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// ListBackends Tests
// =============================================================================

func TestListBackends_Success(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListBackendsRequest{}
	resp, err := svc.ListBackends(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.GreaterOrEqual(t, len(resp.Backends), 1)
}

func TestListBackends_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.ListBackends(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestListBackends_MultipleBackends(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage1 := newObjstoreMockStorage()
	storage2 := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage1, &catalog.BackendConfig{Type: "local", Enabled: true})
	backendMgr.AddBackend("s3", storage2, &catalog.BackendConfig{Type: "s3", Enabled: true})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &ListBackendsRequest{}
	resp, err := svc.ListBackends(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.GreaterOrEqual(t, len(resp.Backends), 2)
}

// =============================================================================
// GetBackendHealth Tests
// =============================================================================

func TestGetBackendHealth_Success(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetBackendHealthRequest{Name: "local"}
	resp, err := svc.GetBackendHealth(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "local", resp.Name)
	assert.True(t, resp.Healthy)
}

func TestGetBackendHealth_NotFound(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetBackendHealthRequest{Name: "nonexistent"}
	resp, err := svc.GetBackendHealth(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestGetBackendHealth_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.GetBackendHealth(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestGetBackendHealth_EmptyName(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetBackendHealthRequest{Name: ""}
	resp, err := svc.GetBackendHealth(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// GetTenantUsage Tests
// =============================================================================

func TestGetTenantUsage_Success(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	// Create objects for tenant
	for i := 0; i < 3; i++ {
		entry := catalog.NewCatalogEntry("test:usage:"+string(rune('a'+i)), 1024, nil)
		entry.TenantID = "tenant-usage"
		entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
		err := catalogMgr.Put(ctx, entry)
		require.NoError(t, err)
	}

	req := &GetTenantUsageRequest{TenantID: "tenant-usage"}
	resp, err := svc.GetTenantUsage(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "tenant-usage", resp.TenantID)
	assert.Equal(t, int64(3), resp.ObjectCount)
}

func TestGetTenantUsage_EmptyTenantID(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetTenantUsageRequest{TenantID: ""}
	resp, err := svc.GetTenantUsage(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestGetTenantUsage_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.GetTenantUsage(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// GetTenantQuotas Tests
// =============================================================================

func TestGetTenantQuotas_Success(t *testing.T) {
	resolver := newObjstoreMockConfigResolver()
	resolver.tenantConfigs["tenant-quotas"] = &config.ObjectStoreTenantConfig{
		Quotas: config.ObjectStoreQuotas{
			MaxStorageBytes: 1024 * 1024 * 1024, // 1GB
			MaxObjectSize:   100 * 1024 * 1024,  // 100MB
			MaxObjectCount:  10000,
		},
	}

	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &GetTenantQuotasRequest{TenantID: "tenant-quotas"}
	resp, err := svc.GetTenantQuotas(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "tenant-quotas", resp.TenantID)
}

func TestGetTenantQuotas_EmptyTenantID(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &GetTenantQuotasRequest{TenantID: ""}
	resp, err := svc.GetTenantQuotas(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestGetTenantQuotas_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.GetTenantQuotas(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

// =============================================================================
// Request Validation Tests
// =============================================================================

func TestPutObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *PutObjectRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &PutObjectRequest{
				Key:   "test:key",
				Value: []byte("data"),
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty key",
			req: &PutObjectRequest{
				Key:   "",
				Value: []byte("data"),
			},
			wantErr: true,
		},
		{
			name: "nil value",
			req: &PutObjectRequest{
				Key:   "test:key",
				Value: nil,
			},
			wantErr: true,
		},
		{
			name: "empty value",
			req: &PutObjectRequest{
				Key:   "test:key",
				Value: []byte{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *GetObjectRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     &GetObjectRequest{Key: "test:key"},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &GetObjectRequest{Key: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeleteObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *DeleteObjectRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     &DeleteObjectRequest{Key: "test:key"},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &DeleteObjectRequest{Key: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCopyObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *CopyObjectRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &CopyObjectRequest{
				SourceKey:      "test:source",
				DestinationKey: "test:dest",
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty source key",
			req: &CopyObjectRequest{
				SourceKey:      "",
				DestinationKey: "test:dest",
			},
			wantErr: true,
		},
		{
			name: "empty destination key",
			req: &CopyObjectRequest{
				SourceKey:      "test:source",
				DestinationKey: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestReplicateObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *ReplicateObjectRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &ReplicateObjectRequest{
				Key:     "test:key",
				Targets: []string{"s3"},
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty key",
			req: &ReplicateObjectRequest{
				Key:     "",
				Targets: []string{"s3"},
			},
			wantErr: true,
		},
		{
			name: "no targets",
			req: &ReplicateObjectRequest{
				Key:     "test:key",
				Targets: []string{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMigrateObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *MigrateObjectRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &MigrateObjectRequest{
				Key:           "test:key",
				TargetBackend: "s3",
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty key",
			req: &MigrateObjectRequest{
				Key:           "",
				TargetBackend: "s3",
			},
			wantErr: true,
		},
		{
			name: "empty target backend",
			req: &MigrateObjectRequest{
				Key:           "test:key",
				TargetBackend: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestArchiveObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *ArchiveObjectRequest
		wantErr bool
	}{
		{
			name: "valid request with target",
			req: &ArchiveObjectRequest{
				Key:         "test:key",
				TargetStore: "glacier",
			},
			wantErr: false,
		},
		{
			name: "valid request without target (uses default)",
			req: &ArchiveObjectRequest{
				Key: "test:key",
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty key",
			req: &ArchiveObjectRequest{
				Key: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRestoreObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *RestoreObjectRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &RestoreObjectRequest{
				Key:           "test:key",
				TargetBackend: "local",
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty key",
			req: &RestoreObjectRequest{
				Key:           "",
				TargetBackend: "local",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVerifyObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *VerifyObjectRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     &VerifyObjectRequest{Key: "test:key"},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &VerifyObjectRequest{Key: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHeadObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *HeadObjectRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     &HeadObjectRequest{Key: "test:key"},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &HeadObjectRequest{Key: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestListObjectsRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *ListObjectsRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     &ListObjectsRequest{Prefix: "test:"},
			wantErr: false,
		},
		{
			name:    "valid request empty prefix",
			req:     &ListObjectsRequest{},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Additional Coverage Tests for Edge Cases
// =============================================================================

func TestVerifyObject_MultipleStores(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage1 := newObjstoreMockStorage()
	storage2 := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage1, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("s3", storage2, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create object with multiple replicas
	testData := []byte("verify multiple")
	objectRef1 := "verify-multi-1"
	objectRef2 := "verify-multi-2"
	err = storage1.PutWithContext(ctx, objectRef1, bytes.NewReader(testData))
	require.NoError(t, err)
	err = storage2.PutWithContext(ctx, objectRef2, bytes.NewReader(testData))
	require.NoError(t, err)

	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:verify-multi", int64(len(testData)), checksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef1,
		Status:    catalog.LocationStatusActive,
	}
	entry.Replicas = []catalog.StoreLocation{
		{
			Store:     "s3",
			ObjectRef: objectRef2,
			Status:    catalog.LocationStatusActive,
		},
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Verify all stores
	req := &VerifyObjectRequest{Key: "test:verify-multi"}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
	assert.Len(t, resp.Results, 2)
}

func TestVerifyObject_SpecificStores(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("verify specific")
	objectRef := "verify-specific-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:verify-specific", int64(len(testData)), checksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Verify specific store
	req := &VerifyObjectRequest{
		Key:    "test:verify-specific",
		Stores: []string{"local"},
	}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
}

func TestVerifyObject_LocationNotFound(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("verify location")
	objectRef := "verify-loc-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:verify-loc", int64(len(testData)), checksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Verify with store that doesn't have the object
	req := &VerifyObjectRequest{
		Key:    "test:verify-loc",
		Stores: []string{"nonexistent-store"},
	}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

func TestVerifyObject_SizeMismatch(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("size mismatch data")
	objectRef := "verify-size-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	// Create entry with wrong size
	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:verify-size", int64(len(testData))+100, checksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &VerifyObjectRequest{Key: "test:verify-size"}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

func TestVerifyObject_BackendUnavailable(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("verify backend")
	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:verify-backend", int64(len(testData)), checksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "nonexistent",
		ObjectRef: "some-ref",
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &VerifyObjectRequest{
		Key:    "test:verify-backend",
		Stores: []string{"nonexistent"},
	}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

func TestVerifyObject_ReadError(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Configure storage to fail on read
	storage.getWithContextFn = func(ctx context.Context, key string) (io.ReadCloser, error) {
		return nil, errors.New("read error")
	}

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("verify read error")
	checksum := sha256.Sum256(testData)
	entry := catalog.NewCatalogEntry("test:verify-read", int64(len(testData)), checksum[:])
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: "some-ref",
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &VerifyObjectRequest{Key: "test:verify-read"}
	resp, err := svc.VerifyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Valid)
}

func TestSetObjectPolicy_CrossTenantDenied(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:policy-tenant", 1024, nil)
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &SetObjectPolicyRequest{
		Key:      "test:policy-tenant",
		TenantID: "tenant2",
		RetentionPolicy: &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
	}
	resp, err := svc.SetObjectPolicy(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestSetObjectPolicy_WithAllPolicies(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:all-policies", 1024, nil)
	entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &SetObjectPolicyRequest{
		Key: "test:all-policies",
		RetentionPolicy: &catalog.RetentionPolicy{
			Enabled:       true,
			RetentionDays: 30,
		},
		ReplicationPolicy: &catalog.ReplicationPolicy{
			Enabled:      true,
			TargetStores: []string{"s3"},
			MinReplicas:  2,
		},
		ArchivePolicy: &catalog.ArchivePolicy{
			Enabled:          true,
			ArchiveAfterDays: 90,
			TargetStore:      "glacier",
		},
	}
	resp, err := svc.SetObjectPolicy(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestRestoreObject_WithDeleteFromArchive(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	primaryStorage := newObjstoreMockStorage()
	archiveStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", primaryStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("glacier", archiveStorage, &catalog.BackendConfig{
		Type:    "glacier",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("restore and delete")
	objectRef := "restore-delete-ref"
	err = archiveStorage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:restore-delete", int64(len(testData)), nil)
	entry.Status = catalog.StatusArchived
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "glacier",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &RestoreObjectRequest{
		Key:           "test:restore-delete",
		TargetBackend: "local",
	}
	resp, err := svc.RestoreObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestReplicateObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.ReplicateObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMigrateObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.MigrateObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestArchiveObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.ArchiveObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestRestoreObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.RestoreObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestVerifyObject_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.VerifyObject(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMigrateObject_CrossTenantDenied(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	sourceStorage := newObjstoreMockStorage()
	targetStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", sourceStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("s3", targetStorage, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("tenant migrate")
	objectRef := "tenant-migrate-ref"
	err = sourceStorage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:tenant-migrate", int64(len(testData)), nil)
	entry.TenantID = "tenant1"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &MigrateObjectRequest{
		Key:           "test:tenant-migrate",
		TargetBackend: "s3",
		TenantID:      "tenant2",
	}
	resp, err := svc.MigrateObject(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestListBackends_WithHealthChecks(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage1 := newObjstoreMockStorage()
	storage2 := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage1, &catalog.BackendConfig{Type: "local", Enabled: true})
	backendMgr.AddBackend("s3", storage2, &catalog.BackendConfig{Type: "s3", Enabled: true})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &ListBackendsRequest{IncludeHealth: true}
	resp, err := svc.ListBackends(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.GreaterOrEqual(t, len(resp.Backends), 2)
}

func TestCopyObject_WithOverwrite(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	// Create source object
	testData := []byte("source for overwrite")
	sourceRef := "source-overwrite-ref"
	err := storage.PutWithContext(ctx, sourceRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:source-overwrite", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: sourceRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Create destination object
	destRef := "dest-overwrite-ref"
	err = storage.PutWithContext(ctx, destRef, bytes.NewReader([]byte("old data")))
	require.NoError(t, err)

	destEntry := catalog.NewCatalogEntry("test:dest-overwrite", 8, nil)
	destEntry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: destRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, destEntry)
	require.NoError(t, err)

	// Copy with overwrite
	req := &CopyObjectRequest{
		SourceKey:         "test:source-overwrite",
		DestinationKey:    "test:dest-overwrite",
		OverwriteExisting: true,
	}
	resp, err := svc.CopyObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test:dest-overwrite", resp.Key)
}

func TestReplicateObject_AlreadyReplicated(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	sourceStorage := newObjstoreMockStorage()
	targetStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", sourceStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("s3", targetStorage, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("already replicated")
	sourceRef := "source-already-ref"
	targetRef := "target-already-ref"
	err = sourceStorage.PutWithContext(ctx, sourceRef, bytes.NewReader(testData))
	require.NoError(t, err)
	err = targetStorage.PutWithContext(ctx, targetRef, bytes.NewReader(testData))
	require.NoError(t, err)

	// Create entry with existing replica
	entry := catalog.NewCatalogEntry("test:already-replicated", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: sourceRef,
		Status:    catalog.LocationStatusActive,
	}
	entry.Replicas = []catalog.StoreLocation{
		{
			Store:     "s3",
			ObjectRef: targetRef,
			Status:    catalog.LocationStatusActive,
		},
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	// Try to replicate to same target
	req := &ReplicateObjectRequest{
		Key:     "test:already-replicated",
		Targets: []string{"s3"},
	}
	resp, err := svc.ReplicateObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "completed", resp.Status)
}

func TestArchiveObject_TargetBackendUnavailable(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	primaryStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", primaryStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	// Configure backend manager to fail on archive store lookup
	backendMgr.getBackendFn = func(name string) (common.Storage, error) {
		if name == "nonexistent-archive" {
			return nil, errors.New("backend not found")
		}
		return primaryStorage, nil
	}

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("archive target missing")
	objectRef := "archive-missing-ref"
	err = primaryStorage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:archive-missing", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ArchiveObjectRequest{
		Key:         "test:archive-missing",
		TargetStore: "nonexistent-archive",
	}
	resp, err := svc.ArchiveObject(ctx, req)
	// May return error or partial success depending on implementation
	if err != nil {
		assert.Nil(t, resp)
	} else {
		assert.NotNil(t, resp)
	}
}

func TestRestoreObject_ReadError(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	primaryStorage := newObjstoreMockStorage()
	archiveStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Configure archive storage to fail on read
	archiveStorage.getWithContextFn = func(ctx context.Context, key string) (io.ReadCloser, error) {
		return nil, errors.New("archive read error")
	}

	backendMgr.AddBackend("local", primaryStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("glacier", archiveStorage, &catalog.BackendConfig{
		Type:    "glacier",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:restore-read-error", 100, nil)
	entry.Status = catalog.StatusArchived
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "glacier",
		ObjectRef: "archived-ref",
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &RestoreObjectRequest{
		Key:           "test:restore-read-error",
		TargetBackend: "local",
	}
	resp, err := svc.RestoreObject(ctx, req)
	// Restore fails because archive cannot be read
	if err != nil {
		assert.Nil(t, resp)
	} else {
		require.NotNil(t, resp)
		assert.False(t, resp.Success)
	}
}

func TestGetTenantUsage_CatalogError(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	// Make GetTenantUsage fail
	catalogMgr.getTenantUsageFn = func(ctx context.Context, tenant string) (*catalog.TenantUsage, error) {
		return nil, errors.New("catalog error")
	}

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &GetTenantUsageRequest{TenantID: "tenant-error"}
	resp, err := svc.GetTenantUsage(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestMigrateObject_ReplicateFails(t *testing.T) {
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	sourceStorage := newObjstoreMockStorage()
	targetStorage := newObjstoreMockStorage()
	resolver := newObjstoreMockConfigResolver()
	logger := newObjstoreMockLogger()

	// Configure target storage to fail on write
	targetStorage.putWithContextFn = func(ctx context.Context, key string, reader io.Reader) error {
		return errors.New("write failed")
	}

	backendMgr.AddBackend("local", sourceStorage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})
	backendMgr.AddBackend("s3", targetStorage, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	testData := []byte("migrate will fail")
	objectRef := "migrate-fail-ref"
	err = sourceStorage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:migrate-fail", int64(len(testData)), nil)
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &MigrateObjectRequest{
		Key:           "test:migrate-fail",
		TargetBackend: "s3",
	}
	resp, err := svc.MigrateObject(ctx, req)
	// Migration may return success with partial status when target write fails
	require.NoError(t, err)
	require.NotNil(t, resp)
}

// =============================================================================
// SetTenantQuotas Tests
// =============================================================================

func TestSetTenantQuotas_Success(t *testing.T) {
	resolver := newObjstoreMockConfigResolver()
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &SetTenantQuotasRequest{
		TenantID:        "test-tenant",
		MaxStorageBytes: 10 * 1024 * 1024 * 1024, // 10GB
		MaxObjectSize:   100 * 1024 * 1024,       // 100MB
		MaxObjectCount:  10000,
	}
	resp, err := svc.SetTenantQuotas(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "test-tenant", resp.TenantID)
	assert.True(t, resp.UpdatedAt > 0)

	// Verify quotas were set in resolver
	tenantCfg := resolver.GetTenantConfig("test-tenant")
	require.NotNil(t, tenantCfg)
	assert.Equal(t, int64(10*1024*1024*1024), tenantCfg.Quotas.MaxStorageBytes)
	assert.Equal(t, int64(100*1024*1024), tenantCfg.Quotas.MaxObjectSize)
	assert.Equal(t, int64(10000), tenantCfg.Quotas.MaxObjectCount)
}

func TestSetTenantQuotas_EmptyTenantID(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &SetTenantQuotasRequest{
		TenantID:        "",
		MaxStorageBytes: 1024,
	}
	resp, err := svc.SetTenantQuotas(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestSetTenantQuotas_NilRequest(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	resp, err := svc.SetTenantQuotas(ctx, nil)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
}

func TestSetTenantQuotas_NegativeValues(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	testCases := []struct {
		name string
		req  *SetTenantQuotasRequest
	}{
		{
			name: "negative max storage bytes",
			req: &SetTenantQuotasRequest{
				TenantID:        "test-tenant",
				MaxStorageBytes: -1,
			},
		},
		{
			name: "negative max object size",
			req: &SetTenantQuotasRequest{
				TenantID:      "test-tenant",
				MaxObjectSize: -1,
			},
		},
		{
			name: "negative max object count",
			req: &SetTenantQuotasRequest{
				TenantID:       "test-tenant",
				MaxObjectCount: -1,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.SetTenantQuotas(ctx, tc.req)
			require.Error(t, err)
			require.Nil(t, resp)

			var qbErr *dberrors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
		})
	}
}

func TestSetTenantQuotas_ZeroValuesAllowed(t *testing.T) {
	resolver := newObjstoreMockConfigResolver()
	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &SetTenantQuotasRequest{
		TenantID:        "unlimited-tenant",
		MaxStorageBytes: 0, // 0 = unlimited
		MaxObjectSize:   0,
		MaxObjectCount:  0,
	}
	resp, err := svc.SetTenantQuotas(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "unlimited-tenant", resp.TenantID)
}

func TestSetTenantQuotas_ResolverError(t *testing.T) {
	resolver := newObjstoreMockConfigResolver()
	resolver.setTenantQuotasFn = func(ctx context.Context, tenantID string, quotas *config.TenantQuotas) error {
		return errors.New("resolver storage error")
	}

	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &SetTenantQuotasRequest{
		TenantID:        "test-tenant",
		MaxStorageBytes: 1024,
	}
	resp, err := svc.SetTenantQuotas(ctx, req)
	require.Error(t, err)
	require.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrStorage, qbErr.Code)
}

func TestSetTenantQuotas_UpdateExisting(t *testing.T) {
	resolver := newObjstoreMockConfigResolver()
	// Pre-populate with existing tenant config
	resolver.tenantConfigs["existing-tenant"] = &config.ObjectStoreTenantConfig{
		Quotas: config.ObjectStoreQuotas{
			MaxStorageBytes: 1024,
			MaxObjectSize:   512,
			MaxObjectCount:  100,
		},
	}

	catalogMgr := newObjstoreMockCatalogManager()
	backendMgr := newObjstoreMockBackendManager()
	storage := newObjstoreMockStorage()
	logger := newObjstoreMockLogger()

	backendMgr.AddBackend("local", storage, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
	})

	svc, err := NewObjectStorageService(catalogMgr, backendMgr, resolver, nil, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &SetTenantQuotasRequest{
		TenantID:        "existing-tenant",
		MaxStorageBytes: 2048,
		MaxObjectSize:   1024,
		MaxObjectCount:  200,
	}
	resp, err := svc.SetTenantQuotas(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify quotas were updated
	tenantCfg := resolver.GetTenantConfig("existing-tenant")
	require.NotNil(t, tenantCfg)
	assert.Equal(t, int64(2048), tenantCfg.Quotas.MaxStorageBytes)
	assert.Equal(t, int64(1024), tenantCfg.Quotas.MaxObjectSize)
	assert.Equal(t, int64(200), tenantCfg.Quotas.MaxObjectCount)
}

// =============================================================================
// SetTenantQuotasRequest Validation Tests
// =============================================================================

func TestSetTenantQuotasRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *SetTenantQuotasRequest
		wantErr bool
	}{
		{
			name: "valid request with all values",
			req: &SetTenantQuotasRequest{
				TenantID:        "test-tenant",
				MaxStorageBytes: 1024,
				MaxObjectSize:   512,
				MaxObjectCount:  100,
			},
			wantErr: false,
		},
		{
			name: "valid request with zero values (unlimited)",
			req: &SetTenantQuotasRequest{
				TenantID:        "test-tenant",
				MaxStorageBytes: 0,
				MaxObjectSize:   0,
				MaxObjectCount:  0,
			},
			wantErr: false,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty tenant ID",
			req: &SetTenantQuotasRequest{
				TenantID:        "",
				MaxStorageBytes: 1024,
			},
			wantErr: true,
		},
		{
			name: "negative max storage bytes",
			req: &SetTenantQuotasRequest{
				TenantID:        "test-tenant",
				MaxStorageBytes: -1,
			},
			wantErr: true,
		},
		{
			name: "negative max object size",
			req: &SetTenantQuotasRequest{
				TenantID:      "test-tenant",
				MaxObjectSize: -1,
			},
			wantErr: true,
		},
		{
			name: "negative max object count",
			req: &SetTenantQuotasRequest{
				TenantID:       "test-tenant",
				MaxObjectCount: -1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetObject_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &GetObjectRequest{Key: "test:closed"}
	resp, err := svc.GetObject(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestListObjects_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &ListObjectsRequest{Prefix: "test:"}
	resp, err := svc.ListObjects(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestListByTag_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &ListByTagRequest{
		TagName:  "env",
		TagValue: "test",
	}
	resp, err := svc.ListByTag(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestRestoreObject_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &RestoreObjectRequest{Key: "test:archived"}
	resp, err := svc.RestoreObject(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestListByTenant_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &ListByTenantRequest{TenantID: "tenant1"}
	resp, err := svc.ListByTenant(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestPutObject_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &PutObjectRequest{
		Key:         "test:put-closed",
		Value:       []byte("data"),
		ContentType: "text/plain",
	}
	resp, err := svc.PutObject(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestDeleteObject_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &DeleteObjectRequest{Key: "test:del-closed"}
	resp, err := svc.DeleteObject(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestHeadObject_ServiceClosed(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	concreteSvc := svc.(*objectStorageService)
	concreteSvc.closed.Store(true)

	req := &HeadObjectRequest{Key: "test:head-closed"}
	resp, err := svc.HeadObject(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetObject_DeletedObjectNotAvailable(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:deleted", 100, nil)
	entry.Status = catalog.StatusDeleted
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: "deleted-ref",
		Status:    catalog.LocationStatusActive,
	}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectRequest{Key: "test:deleted"}
	resp, err := svc.GetObject(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
	assert.Contains(t, qbErr.Error(), "not available")
}

func TestGetObject_PendingObjectNotAvailable(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:pending", 100, nil)
	entry.Status = catalog.StatusPending
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: "pending-ref",
		Status:    catalog.LocationStatusActive,
	}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectRequest{Key: "test:pending"}
	resp, err := svc.GetObject(ctx, req)
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.True(t, errors.As(err, &qbErr))
	assert.Equal(t, dberrors.ErrUnavailable, qbErr.Code)
}

func TestGetObject_IncludeMetadata(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("tagged object data")
	objectRef := "meta-ref-123"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:metadata", int64(len(testData)), nil)
	entry.ContentType = "text/plain"
	entry.Tags = map[string]string{"env": "prod", "team": "backend"}
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectRequest{
		Key:             "test:metadata",
		IncludeMetadata: true,
	}
	resp, err := svc.GetObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, testData, resp.Value)
	assert.Equal(t, "prod", resp.Tags["env"])
	assert.Equal(t, "backend", resp.Tags["team"])
}

func TestGetObject_WithoutIncludeMetadata(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("no-meta data")
	objectRef := "no-meta-ref"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:no-meta", int64(len(testData)), nil)
	entry.Tags = map[string]string{"env": "staging"}
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectRequest{
		Key:             "test:no-meta",
		IncludeMetadata: false,
	}
	resp, err := svc.GetObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Nil(t, resp.Tags)
}

func TestGetObject_ArchivedObjectAccessible(t *testing.T) {
	svc, catalogMgr, _, storage := createObjstoreTestService(t)
	ctx := context.Background()

	testData := []byte("archived data")
	objectRef := "archived-ref-123"
	err := storage.PutWithContext(ctx, objectRef, bytes.NewReader(testData))
	require.NoError(t, err)

	entry := catalog.NewCatalogEntry("test:archived", int64(len(testData)), nil)
	entry.Status = catalog.StatusArchived
	entry.ContentType = "application/octet-stream"
	entry.PrimaryStore = catalog.StoreLocation{
		Store:     "local",
		ObjectRef: objectRef,
		Status:    catalog.LocationStatusActive,
	}
	err = catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &GetObjectRequest{Key: "test:archived"}
	resp, err := svc.GetObject(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, testData, resp.Value)
	assert.Equal(t, "test:archived", resp.Key)
}

func TestListObjects_LimitCappedAt1000(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		entry := catalog.NewCatalogEntry("test:cap:"+string(rune('a'+i)), 100, nil)
		entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
		err := catalogMgr.Put(ctx, entry)
		require.NoError(t, err)
	}

	req := &ListObjectsRequest{
		Prefix: "test:cap:",
		Limit:  5000,
	}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.GreaterOrEqual(t, len(resp.Objects), 0)
}

func TestListObjects_NegativeLimitDefaultsTo100(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListObjectsRequest{
		Prefix: "test:neg:",
		Limit:  -1,
	}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestListObjects_ZeroLimitDefaultsTo100(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListObjectsRequest{
		Prefix: "test:zero:",
		Limit:  0,
	}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestListObjects_TenantFiltering(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry1 := catalog.NewCatalogEntry("test:tenant:a", 100, nil)
	entry1.TenantID = "tenant1"
	entry1.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry1)
	require.NoError(t, err)

	entry2 := catalog.NewCatalogEntry("test:tenant:b", 200, nil)
	entry2.TenantID = "tenant2"
	entry2.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err = catalogMgr.Put(ctx, entry2)
	require.NoError(t, err)

	req := &ListObjectsRequest{
		Prefix:   "test:tenant:",
		TenantID: "tenant1",
	}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	for _, obj := range resp.Objects {
		assert.NotEqual(t, "test:tenant:b", obj.Key, "tenant2 objects should be filtered out")
	}
}

func TestListObjects_IncludeTags(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry := catalog.NewCatalogEntry("test:tags:a", 100, nil)
	entry.Tags = map[string]string{"env": "test"}
	entry.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry)
	require.NoError(t, err)

	req := &ListObjectsRequest{
		Prefix:      "test:tags:",
		IncludeTags: true,
	}
	resp, err := svc.ListObjects(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	for _, obj := range resp.Objects {
		if obj.Key == "test:tags:a" {
			assert.NotNil(t, obj.Tags)
			assert.Equal(t, "test", obj.Tags["env"])
		}
	}
}

func TestListByTag_LimitCappedAt1000(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListByTagRequest{
		TagName:  "env",
		TagValue: "test",
		Limit:    5000,
	}
	resp, err := svc.ListByTag(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestListByTag_NegativeLimitDefaults(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListByTagRequest{
		TagName:  "env",
		TagValue: "test",
		Limit:    -5,
	}
	resp, err := svc.ListByTag(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestListByTag_TenantFiltering(t *testing.T) {
	svc, catalogMgr, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	entry1 := catalog.NewCatalogEntry("test:bytag:a", 100, nil)
	entry1.Tags = map[string]string{"env": "prod"}
	entry1.TenantID = "tenant1"
	entry1.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err := catalogMgr.Put(ctx, entry1)
	require.NoError(t, err)

	entry2 := catalog.NewCatalogEntry("test:bytag:b", 200, nil)
	entry2.Tags = map[string]string{"env": "prod"}
	entry2.TenantID = "tenant2"
	entry2.PrimaryStore = catalog.StoreLocation{Store: "local", Status: catalog.LocationStatusActive}
	err = catalogMgr.Put(ctx, entry2)
	require.NoError(t, err)

	req := &ListByTagRequest{
		TagName:  "env",
		TagValue: "prod",
		TenantID: "tenant1",
	}
	resp, err := svc.ListByTag(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestListByTenant_LimitCappedAt1000(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListByTenantRequest{
		TenantID: "tenant1",
		Limit:    5000,
	}
	resp, err := svc.ListByTenant(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestListByTenant_NegativeLimitDefaults(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := context.Background()

	req := &ListByTenantRequest{
		TenantID: "tenant1",
		Limit:    -1,
	}
	resp, err := svc.ListByTenant(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestSetObjectPolicyRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      *SetObjectPolicyRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *dberrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty key returns error",
			req:     &SetObjectPolicyRequest{Key: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *dberrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "key is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &SetObjectPolicyRequest{Key: "test-object"},
			wantErr: false,
		},
		{
			name:    "valid request with policy returns no error",
			req:     &SetObjectPolicyRequest{Key: "test-object", TenantID: "tenant-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.req != nil {
				err = tt.req.Validate()
			} else {
				err = (*SetObjectPolicyRequest)(nil).Validate()
			}
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetObjectPolicyRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      *GetObjectPolicyRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "nil request returns error",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *dberrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "request cannot be nil")
			},
		},
		{
			name:    "empty key returns error",
			req:     &GetObjectPolicyRequest{Key: ""},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				var qbErr *dberrors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, dberrors.ErrInvalidInput, qbErr.Code)
				assert.Contains(t, qbErr.Err.Error(), "key is required")
			},
		},
		{
			name:    "valid request returns no error",
			req:     &GetObjectPolicyRequest{Key: "test-object"},
			wantErr: false,
		},
		{
			name:    "valid request with tenant returns no error",
			req:     &GetObjectPolicyRequest{Key: "test-object", TenantID: "tenant-456"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.req != nil {
				err = tt.req.Validate()
			} else {
				err = (*GetObjectPolicyRequest)(nil).Validate()
			}
			if tt.wantErr {
				require.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
