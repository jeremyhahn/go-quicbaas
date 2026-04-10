//go:build !integration

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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-objstore/pkg/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Mock Backend Manager for Admin Testing
// =============================================================================

type adminMockBackendManager struct {
	mu       sync.RWMutex
	backends map[string]*adminMockBackendEntry

	getBackendFn        func(name string) (common.Storage, error)
	getBackendConfigFn  func(name string) (*catalog.BackendConfig, error)
	checkHealthFn       func(ctx context.Context, name string) (*catalog.BackendHealth, error)
	registerBackendFn   func(name string, cfg *catalog.BackendConfig) error
	unregisterBackendFn func(name string) error
}

type adminMockBackendEntry struct {
	storage common.Storage
	config  *catalog.BackendConfig
}

func newAdminMockBackendManager() *adminMockBackendManager {
	return &adminMockBackendManager{
		backends: make(map[string]*adminMockBackendEntry),
	}
}

func (m *adminMockBackendManager) AddBackend(name string, storage common.Storage, cfg *catalog.BackendConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[name] = &adminMockBackendEntry{storage: storage, config: cfg}
}

func (m *adminMockBackendManager) GetBackend(name string) (common.Storage, error) {
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

func (m *adminMockBackendManager) ListBackends() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}
	return names
}

func (m *adminMockBackendManager) GetBackendConfig(name string) (*catalog.BackendConfig, error) {
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

func (m *adminMockBackendManager) RegisterBackend(name string, cfg *catalog.BackendConfig) error {
	if m.registerBackendFn != nil {
		return m.registerBackendFn(name, cfg)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[name] = &adminMockBackendEntry{config: cfg}
	return nil
}

func (m *adminMockBackendManager) UnregisterBackend(name string) error {
	if m.unregisterBackendFn != nil {
		return m.unregisterBackendFn(name)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.backends, name)
	return nil
}

func (m *adminMockBackendManager) SelectBackend(criteria *catalog.SelectionCriteria) (string, common.Storage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name, entry := range m.backends {
		return name, entry.storage, nil
	}
	return "", nil, errors.New("no backends available")
}

func (m *adminMockBackendManager) CheckHealth(ctx context.Context, name string) (*catalog.BackendHealth, error) {
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

func (m *adminMockBackendManager) CheckAllHealth(ctx context.Context) map[string]*catalog.BackendHealth {
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

func (m *adminMockBackendManager) Close() error {
	return nil
}

// =============================================================================
// Mock Catalog Manager for Admin Testing
// =============================================================================

type adminMockCatalogManager struct {
	mu      sync.RWMutex
	entries map[string]*catalog.CatalogEntry
	closed  bool

	getFn           func(ctx context.Context, key string) (*catalog.CatalogEntry, error)
	getStoreUsageFn func(ctx context.Context, store string) (*catalog.StoreUsage, error)
}

func newAdminMockCatalogManager() *adminMockCatalogManager {
	return &adminMockCatalogManager{
		entries: make(map[string]*catalog.CatalogEntry),
	}
}

func (m *adminMockCatalogManager) Get(ctx context.Context, key string) (*catalog.CatalogEntry, error) {
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

func (m *adminMockCatalogManager) Put(ctx context.Context, entry *catalog.CatalogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[entry.Key] = entry
	return nil
}

func (m *adminMockCatalogManager) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return nil
}

func (m *adminMockCatalogManager) Update(ctx context.Context, key string, updater func(*catalog.CatalogEntry) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return errors.New("not found")
	}
	return updater(entry)
}

func (m *adminMockCatalogManager) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.entries[key]
	return ok, nil
}

func (m *adminMockCatalogManager) AddLocation(ctx context.Context, key string, loc *catalog.StoreLocation) error {
	return nil
}

func (m *adminMockCatalogManager) RemoveLocation(ctx context.Context, key string, store string) error {
	return nil
}

func (m *adminMockCatalogManager) SetPrimaryStore(ctx context.Context, key string, store string) error {
	return nil
}

func (m *adminMockCatalogManager) GetLocations(ctx context.Context, key string) ([]*catalog.StoreLocation, error) {
	return nil, nil
}

func (m *adminMockCatalogManager) UpdateLocationStatus(ctx context.Context, key, store string, status catalog.LocationStatus) error {
	return nil
}

func (m *adminMockCatalogManager) ListByStore(ctx context.Context, store string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *adminMockCatalogManager) ListByTenant(ctx context.Context, tenant string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *adminMockCatalogManager) ListByPrefix(ctx context.Context, prefix string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *adminMockCatalogManager) ListByStatus(ctx context.Context, status catalog.EntryStatus, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *adminMockCatalogManager) ListByTag(ctx context.Context, tag, value string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *adminMockCatalogManager) ListByEncryptionKey(ctx context.Context, keyID string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
	return &catalog.ListResult{Keys: []string{}}, nil
}

func (m *adminMockCatalogManager) Search(ctx context.Context, query *catalog.SearchQuery) (*catalog.SearchResult, error) {
	return &catalog.SearchResult{Entries: []*catalog.CatalogEntry{}}, nil
}

func (m *adminMockCatalogManager) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	return nil
}

func (m *adminMockCatalogManager) EnqueueArchive(ctx context.Context, key string, targetStore string) error {
	return nil
}

func (m *adminMockCatalogManager) EnqueueVerify(ctx context.Context, key string, stores []string) error {
	return nil
}

func (m *adminMockCatalogManager) DequeueReplication(ctx context.Context, limit int) ([]*catalog.ReplicationTask, error) {
	return nil, nil
}

func (m *adminMockCatalogManager) DequeueArchive(ctx context.Context, limit int) ([]*catalog.ArchiveTask, error) {
	return nil, nil
}

func (m *adminMockCatalogManager) DequeueVerify(ctx context.Context, limit int) ([]*catalog.VerifyTask, error) {
	return nil, nil
}

func (m *adminMockCatalogManager) GetTenantUsage(ctx context.Context, tenant string) (*catalog.TenantUsage, error) {
	return &catalog.TenantUsage{TenantID: tenant}, nil
}

func (m *adminMockCatalogManager) GetStoreUsage(ctx context.Context, store string) (*catalog.StoreUsage, error) {
	if m.getStoreUsageFn != nil {
		return m.getStoreUsageFn(ctx, store)
	}
	return &catalog.StoreUsage{Store: store, ObjectCount: 10, TotalSize: 1000}, nil
}

func (m *adminMockCatalogManager) UpdateAccessTime(ctx context.Context, key string) error {
	return nil
}

func (m *adminMockCatalogManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *adminMockCatalogManager) Sync() error {
	return nil
}

// =============================================================================
// Test Helper Functions
// =============================================================================

func createTestAdminService(t *testing.T) (*objectStorageAdminService, *adminMockCatalogManager, *adminMockBackendManager) {
	catalogMgr := newAdminMockCatalogManager()
	backendMgr := newAdminMockBackendManager()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageAdminService(catalogMgr, backendMgr, nil, logger)
	require.NoError(t, err)

	return svc.(*objectStorageAdminService), catalogMgr, backendMgr
}

// =============================================================================
// NewObjectStorageAdminService Tests
// =============================================================================

func TestNewObjectStorageAdminService_Success(t *testing.T) {
	catalogMgr := newAdminMockCatalogManager()
	backendMgr := newAdminMockBackendManager()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageAdminService(catalogMgr, backendMgr, nil, logger)
	require.NoError(t, err)
	assert.NotNil(t, svc)
}

func TestNewObjectStorageAdminService_NilCatalogManager(t *testing.T) {
	backendMgr := newAdminMockBackendManager()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageAdminService(nil, backendMgr, nil, logger)
	assert.Error(t, err)
	assert.Nil(t, svc)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestNewObjectStorageAdminService_NilBackendManager(t *testing.T) {
	catalogMgr := newAdminMockCatalogManager()
	logger := newObjstoreMockLogger()

	svc, err := NewObjectStorageAdminService(catalogMgr, nil, nil, logger)
	assert.Error(t, err)
	assert.Nil(t, svc)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestNewObjectStorageAdminService_NilLogger(t *testing.T) {
	catalogMgr := newAdminMockCatalogManager()
	backendMgr := newAdminMockBackendManager()

	svc, err := NewObjectStorageAdminService(catalogMgr, backendMgr, nil, nil)
	assert.Error(t, err)
	assert.Nil(t, svc)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

// =============================================================================
// CreateBackend Tests
// =============================================================================

func TestCreateBackend_Success(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &CreateBackendRequest{
		Name:    "test-backend",
		Type:    BackendTypeLocal,
		Tier:    StorageTierHot,
		Config:  map[string]string{"path": "/tmp/test"},
		Enabled: true,
	}

	resp, err := svc.CreateBackend(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.NotZero(t, resp.CreatedAt)
}

func TestCreateBackend_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.CreateBackend(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestCreateBackend_ValidationError_EmptyName(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &CreateBackendRequest{
		Type: BackendTypeLocal,
		Tier: StorageTierHot,
	}

	resp, err := svc.CreateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestCreateBackend_ValidationError_MissingType(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &CreateBackendRequest{
		Name: "test-backend",
		Tier: StorageTierHot,
	}

	resp, err := svc.CreateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestCreateBackend_ValidationError_MissingTier(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &CreateBackendRequest{
		Name: "test-backend",
		Type: BackendTypeLocal,
	}

	resp, err := svc.CreateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestCreateBackend_AlreadyExists(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Pre-add the backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local"})

	req := &CreateBackendRequest{
		Name:    "test-backend",
		Type:    BackendTypeLocal,
		Tier:    StorageTierHot,
		Enabled: true,
	}

	resp, err := svc.CreateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrAlreadyExists, dragonErr.Code)
}

func TestCreateBackend_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	// Close the service
	svc.closed.Store(true)

	req := &CreateBackendRequest{
		Name:    "test-backend",
		Type:    BackendTypeLocal,
		Tier:    StorageTierHot,
		Enabled: true,
	}

	resp, err := svc.CreateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// UpdateBackend Tests
// =============================================================================

func TestUpdateBackend_Success(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Pre-add the backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Tier:    catalog.TierHot,
	})

	req := &UpdateBackendRequest{
		Name:   "test-backend",
		Config: map[string]string{"new-setting": "value"},
		Tier:   StorageTierWarm,
	}

	resp, err := svc.UpdateBackend(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.NotZero(t, resp.UpdatedAt)
}

func TestUpdateBackend_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.UpdateBackend(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestUpdateBackend_ValidationError_EmptyName(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &UpdateBackendRequest{
		Config: map[string]string{"key": "value"},
	}

	resp, err := svc.UpdateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestUpdateBackend_NotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &UpdateBackendRequest{
		Name:   "nonexistent",
		Config: map[string]string{"key": "value"},
	}

	resp, err := svc.UpdateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestUpdateBackend_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &UpdateBackendRequest{
		Name:   "test-backend",
		Config: map[string]string{"key": "value"},
	}

	resp, err := svc.UpdateBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// DeleteBackend Tests
// =============================================================================

func TestDeleteBackend_Success(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Pre-add the backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local"})

	req := &DeleteBackendRequest{
		Name:  "test-backend",
		Force: true,
	}

	resp, err := svc.DeleteBackend(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.True(t, resp.Deleted)
}

func TestDeleteBackend_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.DeleteBackend(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestDeleteBackend_ValidationError_EmptyName(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &DeleteBackendRequest{
		Force: true,
	}

	resp, err := svc.DeleteBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestDeleteBackend_NotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &DeleteBackendRequest{
		Name:  "nonexistent",
		Force: true,
	}

	resp, err := svc.DeleteBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestDeleteBackend_ObjectsExistNoForce(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Pre-add the backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local"})

	// Mock objects exist in this backend
	catalogMgr.getStoreUsageFn = func(ctx context.Context, store string) (*catalog.StoreUsage, error) {
		return &catalog.StoreUsage{Store: store, ObjectCount: 100, TotalSize: 10000}, nil
	}

	req := &DeleteBackendRequest{
		Name: "test-backend",
		// Force: false, MigrateTo: ""
	}

	resp, err := svc.DeleteBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestDeleteBackend_WithMigration(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Pre-add both backends
	backendMgr.AddBackend("source-backend", nil, &catalog.BackendConfig{Type: "local"})
	backendMgr.AddBackend("target-backend", nil, &catalog.BackendConfig{Type: "local"})

	// Mock objects exist
	catalogMgr.getStoreUsageFn = func(ctx context.Context, store string) (*catalog.StoreUsage, error) {
		if store == "source-backend" {
			return &catalog.StoreUsage{Store: store, ObjectCount: 50, TotalSize: 5000}, nil
		}
		return &catalog.StoreUsage{Store: store}, nil
	}

	req := &DeleteBackendRequest{
		Name:      "source-backend",
		MigrateTo: "target-backend",
	}

	resp, err := svc.DeleteBackend(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "source-backend", resp.Name)
	assert.True(t, resp.Deleted)
	assert.Equal(t, int64(50), resp.MigratedObjects)
}

func TestDeleteBackend_MigrationTargetNotFound(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Pre-add source backend only
	backendMgr.AddBackend("source-backend", nil, &catalog.BackendConfig{Type: "local"})

	// Mock objects exist
	catalogMgr.getStoreUsageFn = func(ctx context.Context, store string) (*catalog.StoreUsage, error) {
		return &catalog.StoreUsage{Store: store, ObjectCount: 50, TotalSize: 5000}, nil
	}

	req := &DeleteBackendRequest{
		Name:      "source-backend",
		MigrateTo: "nonexistent-target",
	}

	resp, err := svc.DeleteBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrInvalidInput, dragonErr.Code)
}

func TestDeleteBackend_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &DeleteBackendRequest{
		Name:  "test-backend",
		Force: true,
	}

	resp, err := svc.DeleteBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// SetBackendStatus Tests
// =============================================================================

func TestSetBackendStatus_Success(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Pre-add the backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{
		Type:    "local",
		Enabled: false,
	})

	req := &SetBackendStatusRequest{
		Name:    "test-backend",
		Enabled: true,
	}

	resp, err := svc.SetBackendStatus(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.True(t, resp.Enabled)
	assert.NotZero(t, resp.UpdatedAt)
}

func TestSetBackendStatus_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.SetBackendStatus(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestSetBackendStatus_ValidationError_EmptyName(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &SetBackendStatusRequest{
		Enabled: true,
	}

	resp, err := svc.SetBackendStatus(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestSetBackendStatus_NotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &SetBackendStatusRequest{
		Name:    "nonexistent",
		Enabled: true,
	}

	resp, err := svc.SetBackendStatus(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestSetBackendStatus_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &SetBackendStatusRequest{
		Name:    "test-backend",
		Enabled: true,
	}

	resp, err := svc.SetBackendStatus(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GetStorageStats Tests
// =============================================================================

func TestGetStorageStats_Success(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Add some backends
	backendMgr.AddBackend("backend1", nil, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Tier:    catalog.TierHot,
	})
	backendMgr.AddBackend("backend2", nil, &catalog.BackendConfig{
		Type:    "s3",
		Enabled: true,
		Tier:    catalog.TierWarm,
	})

	req := &GetStorageStatsRequest{
		IncludeBackends: true,
		IncludeTenants:  true,
	}

	resp, err := svc.GetStorageStats(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, int32(2), resp.TotalBackends)
	assert.NotZero(t, resp.CalculatedAt)
}

func TestGetStorageStats_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.GetStorageStats(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetStorageStats_WithBackendDetails(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{
		Type:    "local",
		Enabled: true,
		Tier:    catalog.TierHot,
		Region:  "us-west-2",
	})

	req := &GetStorageStatsRequest{
		IncludeBackends: true,
	}

	resp, err := svc.GetStorageStats(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.BackendStats)
	assert.Len(t, resp.BackendStats, 1)
	assert.Equal(t, "test-backend", resp.BackendStats[0].Name)
}

func TestGetStorageStats_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &GetStorageStatsRequest{}

	resp, err := svc.GetStorageStats(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// RunGarbageCollection Tests
// =============================================================================

func TestRunGarbageCollection_Success(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Add a backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local"})

	req := &RunGarbageCollectionRequest{
		Backends:   []string{"test-backend"},
		DryRun:     false,
		Aggressive: true,
	}

	resp, err := svc.RunGarbageCollection(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.OperationID)
	assert.NotZero(t, resp.StartedAt)
}

func TestRunGarbageCollection_AllBackends(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Add backends
	backendMgr.AddBackend("backend1", nil, &catalog.BackendConfig{Type: "local"})
	backendMgr.AddBackend("backend2", nil, &catalog.BackendConfig{Type: "local"})

	req := &RunGarbageCollectionRequest{
		// Empty backends list means all backends
		Backends: []string{},
		DryRun:   true,
	}

	resp, err := svc.RunGarbageCollection(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.OperationID)
}

func TestRunGarbageCollection_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.RunGarbageCollection(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRunGarbageCollection_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &RunGarbageCollectionRequest{}

	resp, err := svc.RunGarbageCollection(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GetGarbageCollectionProgress Tests
// =============================================================================

func TestGetGarbageCollectionProgress_Success(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Add a backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local"})

	// Start a GC operation
	gcReq := &RunGarbageCollectionRequest{
		Backends: []string{"test-backend"},
	}

	gcResp, err := svc.RunGarbageCollection(ctx, gcReq)
	require.NoError(t, err)

	// Wait a bit for GC to make progress
	time.Sleep(50 * time.Millisecond)

	// Get progress
	req := &GetGarbageCollectionProgressRequest{
		OperationID: gcResp.OperationID,
	}

	resp, err := svc.GetGarbageCollectionProgress(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, gcResp.OperationID, resp.OperationID)
}

func TestGetGarbageCollectionProgress_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.GetGarbageCollectionProgress(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetGarbageCollectionProgress_ValidationError_EmptyOperationID(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &GetGarbageCollectionProgressRequest{
		OperationID: "",
	}

	resp, err := svc.GetGarbageCollectionProgress(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetGarbageCollectionProgress_NotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &GetGarbageCollectionProgressRequest{
		OperationID: "nonexistent-operation-id",
	}

	resp, err := svc.GetGarbageCollectionProgress(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestGetGarbageCollectionProgress_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &GetGarbageCollectionProgressRequest{
		OperationID: "test-op-id",
	}

	resp, err := svc.GetGarbageCollectionProgress(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// RepairObject Tests
// =============================================================================

func TestRepairObject_Success(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Setup mock storage using objstoreMockStorage (reuse from objectstorage_test.go)
	storage1 := newObjstoreMockStorage()
	storage1.objects["obj-ref-1"] = []byte("test data")

	storage2 := newObjstoreMockStorage()
	storage2.objects["obj-ref-2"] = []byte("test data")

	backendMgr.AddBackend("primary", storage1, &catalog.BackendConfig{Type: "local", Enabled: true})
	backendMgr.AddBackend("replica", storage2, &catalog.BackendConfig{Type: "local", Enabled: true})

	// Add catalog entry with multiple locations
	catalogMgr.entries["test-key"] = &catalog.CatalogEntry{
		Key: "test-key",
		PrimaryStore: catalog.StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref-1",
		},
		Replicas: []catalog.StoreLocation{
			{Store: "replica", ObjectRef: "obj-ref-2"},
		},
	}

	req := &RepairObjectRequest{
		Key: "test-key",
	}

	resp, err := svc.RepairObject(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.Key)
	assert.True(t, resp.Success)
}

func TestRepairObject_SingleLocation_NoRepairNeeded(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Setup mock storage
	storage := newObjstoreMockStorage()
	storage.objects["obj-ref"] = []byte("test data")
	backendMgr.AddBackend("primary", storage, &catalog.BackendConfig{Type: "local", Enabled: true})

	// Add catalog entry with single location
	catalogMgr.entries["test-key"] = &catalog.CatalogEntry{
		Key: "test-key",
		PrimaryStore: catalog.StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref",
		},
		Replicas: []catalog.StoreLocation{},
	}

	req := &RepairObjectRequest{
		Key: "test-key",
	}

	resp, err := svc.RepairObject(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Empty(t, resp.RepairedLocations)
}

func TestRepairObject_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.RepairObject(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRepairObject_ValidationError_EmptyKey(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &RepairObjectRequest{
		Key: "",
	}

	resp, err := svc.RepairObject(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRepairObject_ObjectNotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &RepairObjectRequest{
		Key: "nonexistent-key",
	}

	resp, err := svc.RepairObject(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestRepairObject_SourceLocationNotFound(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	storage := newObjstoreMockStorage()
	backendMgr.AddBackend("primary", storage, &catalog.BackendConfig{Type: "local", Enabled: true})

	catalogMgr.entries["test-key"] = &catalog.CatalogEntry{
		Key: "test-key",
		PrimaryStore: catalog.StoreLocation{
			Store:     "primary",
			ObjectRef: "obj-ref",
		},
		Replicas: []catalog.StoreLocation{
			{Store: "replica", ObjectRef: "obj-ref-2"},
		},
	}

	req := &RepairObjectRequest{
		Key:            "test-key",
		SourceLocation: "nonexistent-source",
	}

	resp, err := svc.RepairObject(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRepairObject_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &RepairObjectRequest{
		Key: "test-key",
	}

	resp, err := svc.RepairObject(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// CompactBackend Tests
// =============================================================================

func TestCompactBackend_Success(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	// Add the backend
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local", Enabled: true})

	req := &CompactBackendRequest{
		Name:       "test-backend",
		Aggressive: false,
	}

	resp, err := svc.CompactBackend(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-backend", resp.Name)
	assert.NotEmpty(t, resp.OperationID)
	assert.NotZero(t, resp.StartedAt)
}

func TestCompactBackend_Aggressive(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	ctx := context.Background()

	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local", Enabled: true})

	req := &CompactBackendRequest{
		Name:       "test-backend",
		Aggressive: true,
	}

	resp, err := svc.CompactBackend(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.OperationID)
}

func TestCompactBackend_ValidationError_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	resp, err := svc.CompactBackend(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestCompactBackend_ValidationError_EmptyName(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &CompactBackendRequest{
		Name: "",
	}

	resp, err := svc.CompactBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestCompactBackend_NotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	req := &CompactBackendRequest{
		Name: "nonexistent-backend",
	}

	resp, err := svc.CompactBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)

	var dragonErr *dberrors.DragonError
	require.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, dberrors.ErrNotFound, dragonErr.Code)
}

func TestCompactBackend_ServiceClosed(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	ctx := context.Background()

	svc.closed.Store(true)

	req := &CompactBackendRequest{
		Name: "test-backend",
	}

	resp, err := svc.CompactBackend(ctx, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Request Validation Tests
// =============================================================================

func TestCreateBackendRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *CreateBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty name",
			req: &CreateBackendRequest{
				Type: BackendTypeLocal,
				Tier: StorageTierHot,
			},
			wantErr: true,
		},
		{
			name: "missing type",
			req: &CreateBackendRequest{
				Name: "test",
				Tier: StorageTierHot,
			},
			wantErr: true,
		},
		{
			name: "missing tier",
			req: &CreateBackendRequest{
				Name: "test",
				Type: BackendTypeLocal,
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &CreateBackendRequest{
				Name: "test",
				Type: BackendTypeLocal,
				Tier: StorageTierHot,
			},
			wantErr: false,
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

func TestUpdateBackendRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *UpdateBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty name",
			req: &UpdateBackendRequest{
				Config: map[string]string{"key": "value"},
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &UpdateBackendRequest{
				Name:   "test",
				Config: map[string]string{"key": "value"},
			},
			wantErr: false,
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

func TestDeleteBackendRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *DeleteBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty name",
			req: &DeleteBackendRequest{
				Force: true,
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &DeleteBackendRequest{
				Name:  "test",
				Force: true,
			},
			wantErr: false,
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

func TestSetBackendStatusRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *SetBackendStatusRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty name",
			req: &SetBackendStatusRequest{
				Enabled: true,
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &SetBackendStatusRequest{
				Name:    "test",
				Enabled: true,
			},
			wantErr: false,
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

func TestGetStorageStatsRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *GetStorageStatsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetStorageStatsRequest{},
			wantErr: false,
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

func TestRunGarbageCollectionRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *RunGarbageCollectionRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RunGarbageCollectionRequest{},
			wantErr: false,
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

func TestGetGarbageCollectionProgressRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *GetGarbageCollectionProgressRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty operation ID",
			req: &GetGarbageCollectionProgressRequest{
				OperationID: "",
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &GetGarbageCollectionProgressRequest{
				OperationID: "test-op-id",
			},
			wantErr: false,
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

func TestRepairObjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *RepairObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty key",
			req: &RepairObjectRequest{
				Key: "",
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &RepairObjectRequest{
				Key: "test-key",
			},
			wantErr: false,
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

func TestCompactBackendRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     *CompactBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name: "empty name",
			req: &CompactBackendRequest{
				Name: "",
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &CompactBackendRequest{
				Name: "test-backend",
			},
			wantErr: false,
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
// Storage Tier Conversion Tests
// =============================================================================

func TestConvertStorageTier(t *testing.T) {
	tests := []struct {
		input    StorageTier
		expected catalog.StorageTier
	}{
		{StorageTierHot, catalog.TierHot},
		{StorageTierWarm, catalog.TierWarm},
		{StorageTierCold, catalog.TierCold},
		{StorageTierUnspecified, ""},
		{StorageTier("unknown"), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := convertStorageTier(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
