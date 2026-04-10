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
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// CatalogManager provides high-level operations for managing catalog entries.
// It coordinates storage, indexes, and queues for the object storage catalog.
type CatalogManager interface {
	// Entry operations
	Put(ctx context.Context, entry *CatalogEntry) error
	Get(ctx context.Context, key string) (*CatalogEntry, error)
	Delete(ctx context.Context, key string) error
	Update(ctx context.Context, key string, updater func(*CatalogEntry) error) error
	Exists(ctx context.Context, key string) (bool, error)

	// Location operations
	AddLocation(ctx context.Context, key string, loc *StoreLocation) error
	RemoveLocation(ctx context.Context, key string, store string) error
	SetPrimaryStore(ctx context.Context, key string, store string) error
	GetLocations(ctx context.Context, key string) ([]*StoreLocation, error)
	UpdateLocationStatus(ctx context.Context, key, store string, status LocationStatus) error

	// Query operations
	ListByStore(ctx context.Context, store string, opts *ListOptions) (*ListResult, error)
	ListByTenant(ctx context.Context, tenant string, opts *ListOptions) (*ListResult, error)
	ListByPrefix(ctx context.Context, prefix string, opts *ListOptions) (*ListResult, error)
	ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error)
	ListByTag(ctx context.Context, tag, value string, opts *ListOptions) (*ListResult, error)
	ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) (*ListResult, error)
	Search(ctx context.Context, query *SearchQuery) (*SearchResult, error)

	// Async operations
	EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error
	EnqueueArchive(ctx context.Context, key string, targetStore string) error
	EnqueueVerify(ctx context.Context, key string, stores []string) error
	DequeueReplication(ctx context.Context, limit int) ([]*ReplicationTask, error)
	DequeueArchive(ctx context.Context, limit int) ([]*ArchiveTask, error)
	DequeueVerify(ctx context.Context, limit int) ([]*VerifyTask, error)

	// Stats operations
	GetTenantUsage(ctx context.Context, tenant string) (*TenantUsage, error)
	GetStoreUsage(ctx context.Context, store string) (*StoreUsage, error)
	UpdateAccessTime(ctx context.Context, key string) error

	// Lifecycle
	Close() error
	Sync() error
}

// SearchQuery defines parameters for searching catalog entries.
type SearchQuery struct {
	// Prefix filters by key prefix.
	Prefix string

	// Status filters by entry status.
	Status EntryStatus

	// TenantID filters by tenant.
	TenantID string

	// Tags filters by tag name/value pairs.
	Tags map[string]string

	// SizeMin filters by minimum size.
	SizeMin int64

	// SizeMax filters by maximum size.
	SizeMax int64

	// CreatedAfter filters by creation time (Unix nanoseconds).
	CreatedAfter int64

	// CreatedBefore filters by creation time (Unix nanoseconds).
	CreatedBefore int64

	// ModifiedAfter filters by modification time (Unix nanoseconds).
	ModifiedAfter int64

	// ModifiedBefore filters by modification time (Unix nanoseconds).
	ModifiedBefore int64

	// Sort specifies the sort field (created_at, modified_at, size).
	Sort string

	// Order specifies sort order (asc, desc).
	Order string

	// Limit specifies the maximum number of results.
	Limit int

	// Cursor specifies the pagination cursor.
	Cursor string
}

// SearchResult contains the results of a search operation.
type SearchResult struct {
	// Entries contains the matching entries.
	Entries []*CatalogEntry

	// Total is the approximate total count.
	Total int64

	// NextCursor is the cursor for the next page.
	NextCursor string

	// HasMore indicates if more results are available.
	HasMore bool
}

// TenantUsage contains usage statistics for a tenant.
type TenantUsage struct {
	TenantID      string
	ObjectCount   int64
	TotalSize     int64
	ActiveCount   int64
	ArchivedCount int64
}

// StoreUsage contains usage statistics for a storage backend.
type StoreUsage struct {
	Store        string
	ObjectCount  int64
	TotalSize    int64
	HealthyCount int64
	FailedCount  int64
}

// ReplicationTask represents a pending replication operation.
type ReplicationTask struct {
	Key       string
	Targets   []string
	Priority  int
	CreatedAt int64
}

// ArchiveTask represents a pending archive operation.
type ArchiveTask struct {
	Key         string
	TargetStore string
	CreatedAt   int64
}

// VerifyTask represents a pending verification operation.
type VerifyTask struct {
	Key       string
	Stores    []string
	CreatedAt int64
}

// ManagerConfig configures the catalog manager.
type ManagerConfig struct {
	// Storage is the PebbleDB storage backend.
	Storage PebbleStorage

	// Logger is the logger instance.
	Logger interfaces.Logger
}

// Validate validates the manager configuration.
func (c *ManagerConfig) Validate() error {
	if c == nil {
		return ErrConfigNil
	}
	if c.Storage == nil {
		return ErrStorageNil
	}
	return nil
}

// manager implements CatalogManager.
type manager struct {
	storage PebbleStorage
	logger  interfaces.Logger
	closed  atomic.Bool
	mu      sync.RWMutex

	// In-memory queues for async operations
	replicationQueue []*ReplicationTask
	archiveQueue     []*ArchiveTask
	verifyQueue      []*VerifyTask
	queueMu          sync.Mutex
}

// NewCatalogManager creates a new catalog manager.
func NewCatalogManager(config *ManagerConfig) (CatalogManager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &manager{
		storage:          config.Storage,
		logger:           config.Logger,
		replicationQueue: make([]*ReplicationTask, 0),
		archiveQueue:     make([]*ArchiveTask, 0),
		verifyQueue:      make([]*VerifyTask, 0),
	}, nil
}

// Put stores a catalog entry with all indexes.
func (m *manager) Put(ctx context.Context, entry *CatalogEntry) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if entry == nil {
		return ErrEntryNil
	}
	if err := entry.Validate(); err != nil {
		return err
	}

	return m.storage.PutEntry(ctx, entry)
}

// Get retrieves a catalog entry by key.
func (m *manager) Get(ctx context.Context, key string) (*CatalogEntry, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, NewKeyEmptyError()
	}

	return m.storage.GetEntry(ctx, key)
}

// Delete removes a catalog entry and all its indexes.
func (m *manager) Delete(ctx context.Context, key string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}

	return m.storage.DeleteEntry(ctx, key)
}

// Update performs an atomic update on a catalog entry using optimistic concurrency.
func (m *manager) Update(ctx context.Context, key string, updater func(*CatalogEntry) error) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}
	if updater == nil {
		return ErrUpdaterNil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Get current entry
	entry, err := m.storage.GetEntry(ctx, key)
	if err != nil {
		return err
	}

	// Store original version for optimistic concurrency
	originalVersion := entry.Version

	// Apply updates
	if err := updater(entry); err != nil {
		return err
	}

	// Validate the updated entry
	if err := entry.Validate(); err != nil {
		return err
	}

	// Re-read to check version hasn't changed (optimistic concurrency)
	current, err := m.storage.GetEntry(ctx, key)
	if err != nil {
		return err
	}
	if current.Version != originalVersion {
		return NewVersionMismatchError(key, originalVersion, current.Version)
	}

	// Increment version and update timestamp
	entry.Version = originalVersion + 1
	entry.ModifiedAt = time.Now().UnixNano()

	return m.storage.PutEntry(ctx, entry)
}

// Exists checks if a catalog entry exists.
func (m *manager) Exists(ctx context.Context, key string) (bool, error) {
	if err := m.checkClosed(); err != nil {
		return false, err
	}
	if key == "" {
		return false, NewKeyEmptyError()
	}

	_, err := m.storage.GetEntry(ctx, key)
	if err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// AddLocation adds a storage location to an entry.
func (m *manager) AddLocation(ctx context.Context, key string, loc *StoreLocation) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}
	if loc == nil {
		return ErrLocationNil
	}
	if err := loc.Validate(); err != nil {
		return err
	}

	return m.Update(ctx, key, func(entry *CatalogEntry) error {
		// Check if location already exists
		if entry.HasLocation(loc.Store) {
			return nil // Already exists, no-op
		}

		// Add as replica
		entry.Replicas = append(entry.Replicas, *loc)
		return nil
	})
}

// RemoveLocation removes a storage location from an entry.
func (m *manager) RemoveLocation(ctx context.Context, key string, store string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}
	if store == "" {
		return ErrStoreNameEmpty
	}

	return m.Update(ctx, key, func(entry *CatalogEntry) error {
		// Cannot remove primary store
		if entry.PrimaryStore.Store == store {
			return ErrCannotRemovePrimary
		}

		// Remove from replicas
		entry.RemoveReplica(store)

		// Remove archive store if it matches
		if entry.ArchiveStore != nil && entry.ArchiveStore.Store == store {
			entry.ArchiveStore = nil
		}

		return nil
	})
}

// SetPrimaryStore sets a new primary store for an entry.
func (m *manager) SetPrimaryStore(ctx context.Context, key string, store string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}
	if store == "" {
		return ErrStoreNameEmpty
	}

	return m.Update(ctx, key, func(entry *CatalogEntry) error {
		// If already primary, no-op
		if entry.PrimaryStore.Store == store {
			return nil
		}

		// Find the location in replicas
		var newPrimary *StoreLocation
		for i, r := range entry.Replicas {
			if r.Store == store {
				newPrimary = &entry.Replicas[i]
				break
			}
		}

		if newPrimary == nil {
			return NewLocationNotFoundError(key, store)
		}

		// Demote current primary to replica
		oldPrimary := entry.PrimaryStore
		entry.Replicas = append(entry.Replicas, oldPrimary)

		// Promote replica to primary
		entry.PrimaryStore = *newPrimary

		// Remove from replicas
		entry.RemoveReplica(store)

		return nil
	})
}

// GetLocations returns all storage locations for an entry.
func (m *manager) GetLocations(ctx context.Context, key string) ([]*StoreLocation, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, NewKeyEmptyError()
	}

	entry, err := m.storage.GetEntry(ctx, key)
	if err != nil {
		return nil, err
	}

	locations := entry.GetAllLocations()
	result := make([]*StoreLocation, len(locations))
	for i := range locations {
		result[i] = locations[i].Clone()
	}
	return result, nil
}

// UpdateLocationStatus updates the status of a specific location.
func (m *manager) UpdateLocationStatus(ctx context.Context, key, store string, status LocationStatus) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}
	if store == "" {
		return ErrStoreNameEmpty
	}
	if !status.IsValid() {
		return ErrInvalidLocationStatus
	}

	return m.Update(ctx, key, func(entry *CatalogEntry) error {
		if entry.PrimaryStore.Store == store {
			entry.PrimaryStore.Status = status
			if status == LocationStatusActive {
				entry.PrimaryStore.SyncedAt = time.Now().UnixNano()
			}
			return nil
		}

		for i := range entry.Replicas {
			if entry.Replicas[i].Store == store {
				entry.Replicas[i].Status = status
				if status == LocationStatusActive {
					entry.Replicas[i].SyncedAt = time.Now().UnixNano()
				}
				return nil
			}
		}

		if entry.ArchiveStore != nil && entry.ArchiveStore.Store == store {
			entry.ArchiveStore.Status = status
			if status == LocationStatusActive {
				entry.ArchiveStore.SyncedAt = time.Now().UnixNano()
			}
			return nil
		}

		return NewLocationNotFoundError(key, store)
	})
}

// ListByStore returns keys for entries in a specific store.
func (m *manager) ListByStore(ctx context.Context, store string, opts *ListOptions) (*ListResult, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if store == "" {
		return nil, ErrStoreNameEmpty
	}

	return m.storage.GetPebbleIndexManager().ListByStore(ctx, store, opts)
}

// ListByTenant returns keys for entries owned by a tenant.
func (m *manager) ListByTenant(ctx context.Context, tenant string, opts *ListOptions) (*ListResult, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if tenant == "" {
		return nil, ErrTenantEmpty
	}

	return m.storage.GetPebbleIndexManager().ListByTenant(ctx, tenant, opts)
}

// ListByPrefix returns keys matching a prefix.
func (m *manager) ListByPrefix(ctx context.Context, prefix string, opts *ListOptions) (*ListResult, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}

	limit := 100
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	result := &ListResult{
		Keys:  make([]string, 0, limit),
		Total: -1,
	}

	count := 0
	err := m.storage.IterateKeys(ctx, prefix, func(key string) bool {
		if count < limit {
			result.Keys = append(result.Keys, key)
		} else {
			result.HasMore = true
			return false
		}
		count++
		return true
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListByStatus returns keys for entries with a specific status.
func (m *manager) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if !status.IsValid() {
		return nil, ErrInvalidStatus
	}

	return m.storage.GetPebbleIndexManager().ListByStatus(ctx, status, opts)
}

// ListByTag returns keys for entries with a specific tag.
func (m *manager) ListByTag(ctx context.Context, tag, value string, opts *ListOptions) (*ListResult, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if tag == "" {
		return nil, ErrTagEmpty
	}

	return m.storage.GetPebbleIndexManager().ListByTag(ctx, tag, value, opts)
}

// ListByEncryptionKey returns keys for entries encrypted with a specific key.
func (m *manager) ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) (*ListResult, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if keyID == "" {
		return nil, ErrKeyIDEmpty
	}

	return m.storage.GetPebbleIndexManager().ListByEncryptionKey(ctx, keyID, opts)
}

// Search performs a complex search across catalog entries.
func (m *manager) Search(ctx context.Context, query *SearchQuery) (*SearchResult, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}

	limit := 100
	if query != nil && query.Limit > 0 {
		limit = query.Limit
	}

	result := &SearchResult{
		Entries: make([]*CatalogEntry, 0, limit),
		Total:   0,
	}

	var prefix string
	if query != nil && query.Prefix != "" {
		prefix = query.Prefix
	}

	var candidates []*CatalogEntry
	err := m.storage.Iterate(ctx, prefix, func(entry *CatalogEntry) bool {
		if m.matchesQuery(entry, query) {
			candidates = append(candidates, entry)
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	// Sort candidates
	if query != nil && query.Sort != "" {
		m.sortEntries(candidates, query.Sort, query.Order)
	}

	// Apply pagination
	result.Total = int64(len(candidates))
	start := 0
	if query != nil && query.Cursor != "" {
		// Find cursor position
		for i, e := range candidates {
			if e.Key == query.Cursor {
				start = i + 1
				break
			}
		}
	}

	end := start + limit
	if end > len(candidates) {
		end = len(candidates)
	}

	if start < len(candidates) {
		result.Entries = candidates[start:end]
		if end < len(candidates) {
			result.HasMore = true
			result.NextCursor = candidates[end-1].Key
		}
	}

	return result, nil
}

// matchesQuery checks if an entry matches search criteria.
func (m *manager) matchesQuery(entry *CatalogEntry, query *SearchQuery) bool {
	if query == nil {
		return true
	}

	if query.Status != "" && entry.Status != query.Status {
		return false
	}

	if query.TenantID != "" && entry.TenantID != query.TenantID {
		return false
	}

	if query.SizeMin > 0 && entry.Size < query.SizeMin {
		return false
	}

	if query.SizeMax > 0 && entry.Size > query.SizeMax {
		return false
	}

	if query.CreatedAfter > 0 && entry.CreatedAt < query.CreatedAfter {
		return false
	}

	if query.CreatedBefore > 0 && entry.CreatedAt > query.CreatedBefore {
		return false
	}

	if query.ModifiedAfter > 0 && entry.ModifiedAt < query.ModifiedAfter {
		return false
	}

	if query.ModifiedBefore > 0 && entry.ModifiedAt > query.ModifiedBefore {
		return false
	}

	if len(query.Tags) > 0 {
		for tag, value := range query.Tags {
			if entry.GetTag(tag) != value {
				return false
			}
		}
	}

	return true
}

// sortEntries sorts entries by the specified field.
func (m *manager) sortEntries(entries []*CatalogEntry, sortField, order string) {
	desc := order == "desc"

	sort.Slice(entries, func(i, j int) bool {
		var less bool
		switch sortField {
		case "created_at":
			less = entries[i].CreatedAt < entries[j].CreatedAt
		case "modified_at":
			less = entries[i].ModifiedAt < entries[j].ModifiedAt
		case "size":
			less = entries[i].Size < entries[j].Size
		case "key":
			less = entries[i].Key < entries[j].Key
		default:
			less = entries[i].Key < entries[j].Key
		}

		if desc {
			return !less
		}
		return less
	})
}

// EnqueueReplication adds a replication task to the queue.
func (m *manager) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}
	if len(targets) == 0 {
		return ErrTargetsEmpty
	}

	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	task := &ReplicationTask{
		Key:       key,
		Targets:   targets,
		Priority:  priority,
		CreatedAt: time.Now().UnixNano(),
	}

	// Insert in priority order (higher priority first)
	inserted := false
	for i, t := range m.replicationQueue {
		if priority > t.Priority {
			m.replicationQueue = append(m.replicationQueue[:i], append([]*ReplicationTask{task}, m.replicationQueue[i:]...)...)
			inserted = true
			break
		}
	}
	if !inserted {
		m.replicationQueue = append(m.replicationQueue, task)
	}

	return nil
}

// EnqueueArchive adds an archive task to the queue.
func (m *manager) EnqueueArchive(ctx context.Context, key string, targetStore string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}
	if targetStore == "" {
		return ErrStoreNameEmpty
	}

	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	task := &ArchiveTask{
		Key:         key,
		TargetStore: targetStore,
		CreatedAt:   time.Now().UnixNano(),
	}
	m.archiveQueue = append(m.archiveQueue, task)

	return nil
}

// EnqueueVerify adds a verify task to the queue.
func (m *manager) EnqueueVerify(ctx context.Context, key string, stores []string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}

	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	task := &VerifyTask{
		Key:       key,
		Stores:    stores,
		CreatedAt: time.Now().UnixNano(),
	}
	m.verifyQueue = append(m.verifyQueue, task)

	return nil
}

// DequeueReplication removes and returns replication tasks from the queue.
func (m *manager) DequeueReplication(ctx context.Context, limit int) ([]*ReplicationTask, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}

	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	if limit <= 0 || limit > len(m.replicationQueue) {
		limit = len(m.replicationQueue)
	}

	tasks := m.replicationQueue[:limit]
	m.replicationQueue = m.replicationQueue[limit:]

	return tasks, nil
}

// DequeueArchive removes and returns archive tasks from the queue.
func (m *manager) DequeueArchive(ctx context.Context, limit int) ([]*ArchiveTask, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}

	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	if limit <= 0 || limit > len(m.archiveQueue) {
		limit = len(m.archiveQueue)
	}

	tasks := m.archiveQueue[:limit]
	m.archiveQueue = m.archiveQueue[limit:]

	return tasks, nil
}

// DequeueVerify removes and returns verify tasks from the queue.
func (m *manager) DequeueVerify(ctx context.Context, limit int) ([]*VerifyTask, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}

	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	if limit <= 0 || limit > len(m.verifyQueue) {
		limit = len(m.verifyQueue)
	}

	tasks := m.verifyQueue[:limit]
	m.verifyQueue = m.verifyQueue[limit:]

	return tasks, nil
}

// GetTenantUsage returns usage statistics for a tenant.
func (m *manager) GetTenantUsage(ctx context.Context, tenant string) (*TenantUsage, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if tenant == "" {
		return nil, ErrTenantEmpty
	}

	usage := &TenantUsage{
		TenantID: tenant,
	}

	// Get all entries for tenant
	result, err := m.storage.GetPebbleIndexManager().ListByTenant(ctx, tenant, nil)
	if err != nil {
		return nil, err
	}

	for _, key := range result.Keys {
		entry, err := m.storage.GetEntry(ctx, key)
		if err != nil {
			if IsNotFound(err) {
				continue
			}
			return nil, err
		}

		usage.ObjectCount++
		usage.TotalSize += entry.Size

		switch entry.Status {
		case StatusActive:
			usage.ActiveCount++
		case StatusArchived:
			usage.ArchivedCount++
		}
	}

	return usage, nil
}

// GetStoreUsage returns usage statistics for a storage backend.
func (m *manager) GetStoreUsage(ctx context.Context, store string) (*StoreUsage, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if store == "" {
		return nil, ErrStoreNameEmpty
	}

	usage := &StoreUsage{
		Store: store,
	}

	// Get all entries in store
	result, err := m.storage.GetPebbleIndexManager().ListByStore(ctx, store, nil)
	if err != nil {
		return nil, err
	}

	for _, key := range result.Keys {
		entry, err := m.storage.GetEntry(ctx, key)
		if err != nil {
			if IsNotFound(err) {
				continue
			}
			return nil, err
		}

		usage.ObjectCount++

		// Get the location for this store
		loc := entry.GetLocation(store)
		if loc != nil {
			usage.TotalSize += loc.StoredSize

			if loc.IsHealthy() {
				usage.HealthyCount++
			}
			if loc.IsFailed() {
				usage.FailedCount++
			}
		}
	}

	return usage, nil
}

// UpdateAccessTime updates the last access time for an entry.
func (m *manager) UpdateAccessTime(ctx context.Context, key string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		return NewKeyEmptyError()
	}

	return m.Update(ctx, key, func(entry *CatalogEntry) error {
		entry.LastAccessAt = time.Now().UnixNano()
		return nil
	})
}

// Close closes the catalog manager.
func (m *manager) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.storage.Close()
}

// Sync flushes all pending writes to disk.
func (m *manager) Sync() error {
	if err := m.checkClosed(); err != nil {
		return err
	}

	return m.storage.Sync()
}

// checkClosed returns an error if the manager is closed.
func (m *manager) checkClosed() error {
	if m.closed.Load() {
		return NewCatalogClosedError()
	}
	return nil
}
