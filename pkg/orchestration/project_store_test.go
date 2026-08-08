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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// newTestProject builds a minimal valid Project for use in store tests.
func newTestProject(id, tenantID, name string) *Project {
	return &Project{
		ID:       id,
		TenantID: tenantID,
		Name:     name,
		Workspace: ProjectWorkspace{
			AgentID: "agent-1",
			Path:    "/workspace/foo",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

// =============================================================================
// Test doubles
// =============================================================================

// projectCountingKV counts the verbs a store issues so a test can prove the
// store really talks to the KV instead of serving reads from private state.
type projectCountingKV struct {
	interfaces.KVStore
	gets    atomic.Int64
	puts    atomic.Int64
	deletes atomic.Int64
	scans   atomic.Int64
}

func (k *projectCountingKV) Get(ctx context.Context, key string) ([]byte, error) {
	k.gets.Add(1)
	return k.KVStore.Get(ctx, key)
}

func (k *projectCountingKV) Put(ctx context.Context, key string, value []byte) error {
	k.puts.Add(1)
	return k.KVStore.Put(ctx, key, value)
}

func (k *projectCountingKV) Delete(ctx context.Context, key string) error {
	k.deletes.Add(1)
	return k.KVStore.Delete(ctx, key)
}

func (k *projectCountingKV) Scan(ctx context.Context, prefix string, fn func(string, []byte) error) error {
	k.scans.Add(1)
	return k.KVStore.Scan(ctx, prefix, fn)
}

// projectKVVerb names a KVStore method for failure injection.
type projectKVVerb string

const (
	projectVerbGet    projectKVVerb = "get"
	projectVerbPut    projectKVVerb = "put"
	projectVerbDelete projectKVVerb = "delete"
	projectVerbScan   projectKVVerb = "scan"
	projectVerbExists projectKVVerb = "exists"
)

// errProjectKVFailure is the injected backend failure.
var errProjectKVFailure = &qerrors.QBError{Code: qerrors.ErrInternal, Op: "kv.test"}

// projectKVFailure selects the calls a projectFailingKV rejects. An empty
// keyPrefix matches every key.
type projectKVFailure struct {
	verb      projectKVVerb
	keyPrefix string
}

// projectFailingKV rejects the chosen calls and delegates the rest, so a test
// can assert how the store reports a backend outage on each code path.
type projectFailingKV struct {
	interfaces.KVStore
	failures []projectKVFailure
}

// newProjectFailingKV wraps kv so that verb calls on keys under keyPrefix fail.
func newProjectFailingKV(kv interfaces.KVStore, verb projectKVVerb, keyPrefix string) *projectFailingKV {
	return &projectFailingKV{
		KVStore:  kv,
		failures: []projectKVFailure{{verb: verb, keyPrefix: keyPrefix}},
	}
}

func (k *projectFailingKV) shouldFail(verb projectKVVerb, key string) bool {
	for _, failure := range k.failures {
		if failure.verb != verb {
			continue
		}
		if failure.keyPrefix == "" || kvstore.HasPrefix(key, failure.keyPrefix) {
			return true
		}
	}
	return false
}

func (k *projectFailingKV) Get(ctx context.Context, key string) ([]byte, error) {
	if k.shouldFail(projectVerbGet, key) {
		return nil, errProjectKVFailure
	}
	return k.KVStore.Get(ctx, key)
}

func (k *projectFailingKV) Put(ctx context.Context, key string, value []byte) error {
	if k.shouldFail(projectVerbPut, key) {
		return errProjectKVFailure
	}
	return k.KVStore.Put(ctx, key, value)
}

func (k *projectFailingKV) Delete(ctx context.Context, key string) error {
	if k.shouldFail(projectVerbDelete, key) {
		return errProjectKVFailure
	}
	return k.KVStore.Delete(ctx, key)
}

func (k *projectFailingKV) Exists(ctx context.Context, key string) (bool, error) {
	if k.shouldFail(projectVerbExists, key) {
		return false, errProjectKVFailure
	}
	return k.KVStore.Exists(ctx, key)
}

func (k *projectFailingKV) Scan(ctx context.Context, prefix string, fn func(string, []byte) error) error {
	if k.shouldFail(projectVerbScan, prefix) {
		return errProjectKVFailure
	}
	return k.KVStore.Scan(ctx, prefix, fn)
}

// projectAuditRecorder captures store audit events for assertion.
type projectAuditRecorder struct {
	mu     sync.Mutex
	events []StoreAuditEvent
}

func (r *projectAuditRecorder) RecordStoreEvent(_ context.Context, event StoreAuditEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *projectAuditRecorder) snapshot() []StoreAuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StoreAuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

// =============================================================================
// Create
// =============================================================================

func TestProjectStore_Create_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "my-project")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectStore_Create_SetsTimestamps(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "stamped")
	p.CreatedAt = time.Time{}
	p.UpdatedAt = time.Time{}

	before := time.Now().UTC()
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.Get(ctx, "tenant-1", "id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CreatedAt.Before(before) {
		t.Errorf("CreatedAt %v not set on create", got.CreatedAt)
	}
	if got.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt %v not set on create", got.UpdatedAt)
	}
}

func TestProjectStore_Create_PreservesSuppliedCreatedAt(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	p := newTestProject("id-1", "tenant-1", "preserved")
	p.CreatedAt = created

	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.Get(ctx, "tenant-1", "id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, created)
	}
}

func TestProjectStore_Create_DuplicateName(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p1 := newTestProject("id-1", "tenant-1", "dup")
	if err := store.Create(ctx, p1); err != nil {
		t.Fatalf("unexpected error creating first: %v", err)
	}

	p2 := newTestProject("id-2", "tenant-1", "dup")
	err := store.Create(ctx, p2)
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
	if !errors.Is(err, ErrProjectAlreadyExists) {
		t.Errorf("expected ErrProjectAlreadyExists, got %T: %v", err, err)
	}

	// The rejected project must not be reachable.
	if _, getErr := store.Get(ctx, "tenant-1", "id-2"); !errors.Is(getErr, ErrProjectNotFound) {
		t.Errorf("rejected project must not be stored, got %v", getErr)
	}
}

func TestProjectStore_Create_DuplicateID(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "first")); err != nil {
		t.Fatalf("create first: %v", err)
	}
	err := store.Create(ctx, newTestProject("id-1", "tenant-1", "second"))
	if !errors.Is(err, ErrProjectAlreadyExists) {
		t.Fatalf("want ErrProjectAlreadyExists for duplicate ID, got %v", err)
	}

	// The failed create must not have reserved the second name.
	if _, getErr := store.GetByName(ctx, "tenant-1", "second"); !errors.Is(getErr, ErrProjectNotFound) {
		t.Errorf("failed create must not reserve a name, got %v", getErr)
	}
}

func TestProjectStore_Create_NilProject(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Create(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil project, got nil")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %T: %v", err, err)
	}
}

func TestProjectStore_Create_EmptyID(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("", "tenant-1", "name")
	err := store.Create(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

func TestProjectStore_Create_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("id-1", "", "name")
	err := store.Create(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

func TestProjectStore_Create_EmptyName(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("id-1", "tenant-1", "")
	err := store.Create(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// Get
// =============================================================================

func TestProjectStore_Get_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "proj")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.Get(ctx, "tenant-1", "id-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "proj" {
		t.Errorf("expected name 'proj', got %q", got.Name)
	}
}

func TestProjectStore_Get_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.Get(context.Background(), "tenant-1", "missing-id")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_Get_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.Get(context.Background(), "", "id-1")
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// TestProjectStore_Get_CorruptRecord asserts a record that is not decodable is
// reported as a store failure rather than as a missing project, so operators
// can tell corruption apart from absence.
func TestProjectStore_Get_CorruptRecord(t *testing.T) {
	kv := kvstore.NewMemory()
	store := NewProjectStore(kv)
	ctx := context.Background()

	key, err := projectRecordKey("tenant-1", "id-1")
	if err != nil {
		t.Fatalf("projectRecordKey: %v", err)
	}
	if err := kv.Put(ctx, key, []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	_, err = store.Get(ctx, "tenant-1", "id-1")
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("want *ProjectStoreError for corrupt record, got %T: %v", err, err)
	}
}

// =============================================================================
// GetByName
// =============================================================================

func TestProjectStore_GetByName_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "by-name")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.GetByName(ctx, "tenant-1", "by-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "id-1" {
		t.Errorf("expected ID 'id-1', got %q", got.ID)
	}
}

func TestProjectStore_GetByName_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.GetByName(context.Background(), "tenant-1", "ghost")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_GetByName_EmptyName(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.GetByName(context.Background(), "tenant-1", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// TestProjectStore_GetByName_DanglingIndex asserts a name reservation whose
// record is gone reads as not-found rather than as an internal failure.
func TestProjectStore_GetByName_DanglingIndex(t *testing.T) {
	kv := kvstore.NewMemory()
	store := NewProjectStore(kv)
	ctx := context.Background()

	nameKey, err := projectNameIndexKey("tenant-1", "orphan")
	if err != nil {
		t.Fatalf("projectNameIndexKey: %v", err)
	}
	if err := kv.Put(ctx, nameKey, []byte("id-gone")); err != nil {
		t.Fatalf("seed name index: %v", err)
	}

	_, err = store.GetByName(ctx, "tenant-1", "orphan")
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound for dangling index, got %T: %v", err, err)
	}
}

// =============================================================================
// Update
// =============================================================================

func TestProjectStore_Update_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "old-name")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	p.Name = "new-name"
	if err := store.Update(ctx, p); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	got, err := store.GetByName(ctx, "tenant-1", "new-name")
	if err != nil {
		t.Fatalf("get by new name failed: %v", err)
	}
	if got.ID != "id-1" {
		t.Errorf("expected ID 'id-1', got %q", got.ID)
	}

	// The old reservation must be released so the name can be reused.
	if _, oldErr := store.GetByName(ctx, "tenant-1", "old-name"); !errors.Is(oldErr, ErrProjectNotFound) {
		t.Errorf("old name must be released, got %v", oldErr)
	}
}

func TestProjectStore_Update_PreservesCreatedAtAndBumpsUpdatedAt(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "stamped")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	stored, err := store.Get(ctx, "tenant-1", "id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// A caller that drops CreatedAt must not erase it.
	update := newTestProject("id-1", "tenant-1", "stamped")
	update.CreatedAt = time.Time{}
	update.Description = "changed"
	if err := store.Update(ctx, update); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.Get(ctx, "tenant-1", "id-1")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if !got.CreatedAt.Equal(stored.CreatedAt) {
		t.Errorf("CreatedAt = %v, want preserved %v", got.CreatedAt, stored.CreatedAt)
	}
	if got.UpdatedAt.Before(stored.UpdatedAt) {
		t.Errorf("UpdatedAt %v went backwards from %v", got.UpdatedAt, stored.UpdatedAt)
	}
	if got.Description != "changed" {
		t.Errorf("Description = %q, want %q", got.Description, "changed")
	}
}

// TestProjectStore_Update_EmptyNameKeepsCurrentName asserts an update that
// omits the name leaves both the record and its index entry on the stored name.
func TestProjectStore_Update_EmptyNameKeepsCurrentName(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "keeper")); err != nil {
		t.Fatalf("create: %v", err)
	}

	update := newTestProject("id-1", "tenant-1", "")
	update.Description = "no rename"
	if err := store.Update(ctx, update); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.GetByName(ctx, "tenant-1", "keeper")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if got.Name != "keeper" {
		t.Errorf("Name = %q, want %q", got.Name, "keeper")
	}
	if got.Description != "no rename" {
		t.Errorf("Description = %q, want %q", got.Description, "no rename")
	}
}

func TestProjectStore_Update_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	p := newTestProject("ghost", "tenant-1", "ghost")
	err := store.Update(context.Background(), p)
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_Update_NilProject(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Update(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil project")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// Delete
// =============================================================================

func TestProjectStore_Delete_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	p := newTestProject("id-1", "tenant-1", "to-delete")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := store.Delete(ctx, "tenant-1", "id-1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := store.Get(ctx, "tenant-1", "id-1")
	if err == nil {
		t.Fatal("expected not-found after delete, got nil")
	}

	// The name must be free for reuse.
	if err := store.Create(ctx, newTestProject("id-2", "tenant-1", "to-delete")); err != nil {
		t.Fatalf("name must be released by delete: %v", err)
	}
}

func TestProjectStore_Delete_NotFound(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Delete(context.Background(), "tenant-1", "ghost")
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("expected ErrProjectNotFound, got %T: %v", err, err)
	}
}

func TestProjectStore_Delete_EmptyID(t *testing.T) {
	store := NewInMemoryProjectStore()
	err := store.Delete(context.Background(), "tenant-1", "")
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

// =============================================================================
// ListByTenant
// =============================================================================

func TestProjectStore_ListByTenant_HappyPath(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		p := newTestProject("id-"+name, "tenant-1", name)
		if err := store.Create(ctx, p); err != nil {
			t.Fatalf("create %q failed: %v", name, err)
		}
	}
	// Create a project for a different tenant — must not appear.
	other := newTestProject("other-id", "tenant-2", "other")
	if err := store.Create(ctx, other); err != nil {
		t.Fatalf("create other-tenant project failed: %v", err)
	}

	projects, err := store.ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(projects) != 3 {
		t.Errorf("expected 3 projects, got %d", len(projects))
	}
}

func TestProjectStore_ListByTenant_EmptyTenantID(t *testing.T) {
	store := NewInMemoryProjectStore()
	_, err := store.ListByTenant(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty tenantID")
	}
	if !errors.Is(err, ErrProjectInvalid) {
		t.Errorf("expected ErrProjectInvalid, got %v", err)
	}
}

func TestProjectStore_ListByTenant_Empty(t *testing.T) {
	store := NewInMemoryProjectStore()
	projects, err := store.ListByTenant(context.Background(), "tenant-nobody")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

// TestProjectStore_ListByTenant_CorruptRecord asserts a record that cannot be
// decoded fails the whole listing rather than silently shortening it.
func TestProjectStore_ListByTenant_CorruptRecord(t *testing.T) {
	kv := kvstore.NewMemory()
	store := NewProjectStore(kv)
	ctx := context.Background()

	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "good")); err != nil {
		t.Fatalf("create: %v", err)
	}
	key, err := projectRecordKey("tenant-1", "id-2")
	if err != nil {
		t.Fatalf("projectRecordKey: %v", err)
	}
	if err := kv.Put(ctx, key, []byte("{not json")); err != nil {
		t.Fatalf("seed corrupt record: %v", err)
	}

	_, err = store.ListByTenant(ctx, "tenant-1")
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
	}
}

// =============================================================================
// CRUD round-trip across backends
// =============================================================================

// projectBackend names one interfaces.KVStore binding of the single store
// implementation.
type projectBackend struct {
	name string
	kv   func() interfaces.KVStore
}

// projectBackends enumerates the bindings every behavioural test runs against.
// Both are memory-backed so the suite stays fast and touches no disk; the
// counting binding additionally proves the store issues real KV reads.
func projectBackends() []projectBackend {
	return []projectBackend{
		{name: "memory", kv: func() interfaces.KVStore { return kvstore.NewMemory() }},
		{
			name: "memory_counted",
			kv:   func() interfaces.KVStore { return &projectCountingKV{KVStore: kvstore.NewMemory()} },
		},
	}
}

// TestProjectStore_CRUDRoundTrip runs one CRUD table against every backend
// binding. Identical results across bindings are what makes "memory versus
// persistent is only a matter of which KVStore is injected" a tested property
// rather than a claim.
func TestProjectStore_CRUDRoundTrip(t *testing.T) {
	steps := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, store ProjectStore)
	}{
		{
			name: "create then get",
			run: func(t *testing.T, ctx context.Context, store ProjectStore) {
				p := newTestProject("id-1", "tenant-1", "alpha")
				p.Description = "first"
				if err := store.Create(ctx, p); err != nil {
					t.Fatalf("create: %v", err)
				}
				got, err := store.Get(ctx, "tenant-1", "id-1")
				if err != nil {
					t.Fatalf("get: %v", err)
				}
				if got.Description != "first" || got.Name != "alpha" {
					t.Fatalf("round-trip mismatch: %+v", got)
				}
			},
		},
		{
			name: "get by name",
			run: func(t *testing.T, ctx context.Context, store ProjectStore) {
				got, err := store.GetByName(ctx, "tenant-1", "alpha")
				if err != nil {
					t.Fatalf("get by name: %v", err)
				}
				if got.ID != "id-1" {
					t.Fatalf("ID = %q, want id-1", got.ID)
				}
			},
		},
		{
			name: "update renames",
			run: func(t *testing.T, ctx context.Context, store ProjectStore) {
				p := newTestProject("id-1", "tenant-1", "beta")
				if err := store.Update(ctx, p); err != nil {
					t.Fatalf("update: %v", err)
				}
				if _, err := store.GetByName(ctx, "tenant-1", "beta"); err != nil {
					t.Fatalf("get by new name: %v", err)
				}
				if _, err := store.GetByName(ctx, "tenant-1", "alpha"); !errors.Is(err, ErrProjectNotFound) {
					t.Fatalf("old name still resolves: %v", err)
				}
			},
		},
		{
			name: "list by tenant",
			run: func(t *testing.T, ctx context.Context, store ProjectStore) {
				if err := store.Create(ctx, newTestProject("id-2", "tenant-1", "gamma")); err != nil {
					t.Fatalf("create second: %v", err)
				}
				projects, err := store.ListByTenant(ctx, "tenant-1")
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if len(projects) != 2 {
					t.Fatalf("len = %d, want 2", len(projects))
				}
			},
		},
		{
			name: "delete",
			run: func(t *testing.T, ctx context.Context, store ProjectStore) {
				if err := store.Delete(ctx, "tenant-1", "id-1"); err != nil {
					t.Fatalf("delete: %v", err)
				}
				if _, err := store.Get(ctx, "tenant-1", "id-1"); !errors.Is(err, ErrProjectNotFound) {
					t.Fatalf("deleted project still readable: %v", err)
				}
				projects, err := store.ListByTenant(ctx, "tenant-1")
				if err != nil {
					t.Fatalf("list after delete: %v", err)
				}
				if len(projects) != 1 {
					t.Fatalf("len = %d, want 1", len(projects))
				}
			},
		},
	}

	for _, backend := range projectBackends() {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			kv := backend.kv()
			store := NewProjectStore(kv)

			for _, step := range steps {
				t.Run(step.name, func(t *testing.T) {
					step.run(t, ctx, store)
				})
			}

			counted, ok := kv.(*projectCountingKV)
			if !ok {
				return
			}
			// Every read the table performed had to reach the KV, which is
			// only true if the store keeps no private copy of the data.
			if counted.gets.Load() == 0 || counted.puts.Load() == 0 ||
				counted.deletes.Load() == 0 || counted.scans.Load() == 0 {
				t.Errorf("store bypassed the KV: gets=%d puts=%d deletes=%d scans=%d",
					counted.gets.Load(), counted.puts.Load(),
					counted.deletes.Load(), counted.scans.Load())
			}
		})
	}
}

// =============================================================================
// Restart durability
// =============================================================================

// TestProjectStore_RestartDurability builds a second store over the same KV and
// asserts it sees everything the first one wrote. A store that cached anything
// privately would fail here, which is what makes the KV the single source of
// truth.
func TestProjectStore_RestartDurability(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()

	seed := []*Project{
		newTestProject("id-1", "tenant-1", "alpha"),
		newTestProject("id-2", "tenant-1", "beta"),
		newTestProject("id-3", "tenant-2", "gamma"),
	}
	writer := NewProjectStore(kv)
	for _, p := range seed {
		p.Description = "desc-" + p.ID
		if err := writer.Create(ctx, p); err != nil {
			t.Fatalf("create %s: %v", p.ID, err)
		}
	}

	// A brand new store over the same KV — the "restart".
	reader := NewProjectStore(kv)

	for _, want := range seed {
		got, err := reader.Get(ctx, want.TenantID, want.ID)
		if err != nil {
			t.Fatalf("get %s after restart: %v", want.ID, err)
		}
		assertProjectEqual(t, got, want)

		byName, err := reader.GetByName(ctx, want.TenantID, want.Name)
		if err != nil {
			t.Fatalf("get by name %s after restart: %v", want.Name, err)
		}
		assertProjectEqual(t, byName, want)
	}

	tenant1, err := reader.ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list tenant-1 after restart: %v", err)
	}
	if len(tenant1) != 2 {
		t.Fatalf("tenant-1 list len = %d, want 2", len(tenant1))
	}
	tenant2, err := reader.ListByTenant(ctx, "tenant-2")
	if err != nil {
		t.Fatalf("list tenant-2 after restart: %v", err)
	}
	if len(tenant2) != 1 {
		t.Fatalf("tenant-2 list len = %d, want 1", len(tenant2))
	}
}

// assertProjectEqual compares the fields a store round-trip must preserve.
func assertProjectEqual(t *testing.T, got, want *Project) {
	t.Helper()
	if got.ID != want.ID || got.TenantID != want.TenantID || got.Name != want.Name ||
		got.Description != want.Description || got.Workspace != want.Workspace {
		t.Errorf("project mismatch:\n got = %+v\nwant = %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}
}

// =============================================================================
// Tenant isolation
// =============================================================================

// TestProjectStore_TenantIsolation asserts at the store layer that a caller
// presenting tenant A can neither read, rename nor delete tenant B's project,
// including when one tenant ID is a lexical prefix of the other.
func TestProjectStore_TenantIsolation(t *testing.T) {
	tests := []struct {
		name    string
		tenantA string
		tenantB string
	}{
		{name: "disjoint tenants", tenantA: "tenant-a", tenantB: "tenant-b"},
		{name: "prefix tenants", tenantA: "t1", tenantB: "t10"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewProjectStore(kvstore.NewMemory())

			ownA := newTestProject("a-1", tc.tenantA, "shared-name")
			ownB := newTestProject("b-1", tc.tenantB, "b-name")
			for _, p := range []*Project{ownA, ownB} {
				if err := store.Create(ctx, p); err != nil {
					t.Fatalf("create %s: %v", p.ID, err)
				}
			}

			// Cross-tenant point read by ID.
			if _, err := store.Get(ctx, tc.tenantA, ownB.ID); !errors.Is(err, ErrProjectNotFound) {
				t.Errorf("Get across tenants leaked: %v", err)
			}
			// Cross-tenant read by name.
			if _, err := store.GetByName(ctx, tc.tenantA, ownB.Name); !errors.Is(err, ErrProjectNotFound) {
				t.Errorf("GetByName across tenants leaked: %v", err)
			}
			// Cross-tenant update.
			hijack := newTestProject(ownB.ID, tc.tenantA, "hijacked")
			if err := store.Update(ctx, hijack); !errors.Is(err, ErrProjectNotFound) {
				t.Errorf("Update across tenants was accepted: %v", err)
			}
			// Cross-tenant delete.
			if err := store.Delete(ctx, tc.tenantA, ownB.ID); !errors.Is(err, ErrProjectNotFound) {
				t.Errorf("Delete across tenants was accepted: %v", err)
			}

			// B is untouched.
			gotB, err := store.Get(ctx, tc.tenantB, ownB.ID)
			if err != nil {
				t.Fatalf("tenant B record damaged: %v", err)
			}
			if gotB.Name != "b-name" {
				t.Errorf("tenant B name = %q, want b-name", gotB.Name)
			}

			// Listing is bounded to the caller's subtree.
			listA, err := store.ListByTenant(ctx, tc.tenantA)
			if err != nil {
				t.Fatalf("list tenant A: %v", err)
			}
			if len(listA) != 1 || listA[0].ID != ownA.ID {
				t.Fatalf("tenant A list = %+v, want only %s", listA, ownA.ID)
			}
			listB, err := store.ListByTenant(ctx, tc.tenantB)
			if err != nil {
				t.Fatalf("list tenant B: %v", err)
			}
			if len(listB) != 1 || listB[0].ID != ownB.ID {
				t.Fatalf("tenant B list = %+v, want only %s", listB, ownB.ID)
			}

			// The same project name in two tenants is not a conflict.
			if err := store.Create(ctx, newTestProject("b-2", tc.tenantB, "shared-name")); err != nil {
				t.Fatalf("same name in another tenant must be allowed: %v", err)
			}
		})
	}
}

// TestProjectStore_ForeignRecordUnderTenantKey asserts the body-level owner
// check: a record filed under tenant A whose body claims tenant B is reported
// absent instead of being handed to A.
func TestProjectStore_ForeignRecordUnderTenantKey(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()
	store := NewProjectStore(kv)

	foreign := newTestProject("id-1", "tenant-b", "foreign")
	data, err := marshalProject(foreign)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	key, keyErr := projectRecordKey("tenant-a", "id-1")
	if keyErr != nil {
		t.Fatalf("projectRecordKey: %v", keyErr)
	}
	if putErr := kv.Put(ctx, key, data); putErr != nil {
		t.Fatalf("seed foreign record: %v", putErr)
	}

	if _, getErr := store.Get(ctx, "tenant-a", "id-1"); !errors.Is(getErr, ErrProjectNotFound) {
		t.Errorf("foreign record leaked through Get: %v", getErr)
	}
	projects, listErr := store.ListByTenant(ctx, "tenant-a")
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(projects) != 0 {
		t.Errorf("foreign record leaked through ListByTenant: %+v", projects)
	}
}

// =============================================================================
// Key-segment rejection
// =============================================================================

// TestProjectStore_KeySegmentRejection asserts identifiers that would break the
// key layout are refused on every method. Accepting one would let a crafted
// identifier address another tenant's subtree.
func TestProjectStore_KeySegmentRejection(t *testing.T) {
	tests := []struct {
		name string
		call func(ctx context.Context, store ProjectStore) error
	}{
		{
			name: "create tenant with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-1", "tenant/evil", "n"))
			},
		},
		{
			name: "create empty tenant",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-1", "", "n"))
			},
		},
		{
			name: "create id with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("tenant-b/id-1", "tenant-a", "n"))
			},
		},
		{
			name: "create empty id",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("", "tenant-a", "n"))
			},
		},
		{
			name: "create name with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-1", "tenant-a", "bad/name"))
			},
		},
		{
			name: "create empty name",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-1", "tenant-a", ""))
			},
		},
		{
			name: "get id with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.Get(ctx, "tenant-a", "tenant-b/id-1")
				return err
			},
		},
		{
			name: "get empty tenant",
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.Get(ctx, "", "id-1")
				return err
			},
		},
		{
			name: "get by name with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.GetByName(ctx, "tenant-a", "bad/name")
				return err
			},
		},
		{
			name: "get by empty name",
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.GetByName(ctx, "tenant-a", "")
				return err
			},
		},
		{
			name: "update tenant with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Update(ctx, newTestProject("id-1", "tenant/evil", "n"))
			},
		},
		{
			name: "update empty id",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Update(ctx, newTestProject("", "tenant-a", "n"))
			},
		},
		{
			name: "update name with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Update(ctx, newTestProject("id-1", "tenant-a", "bad/name"))
			},
		},
		{
			name: "delete id with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Delete(ctx, "tenant-a", "tenant-b/id-1")
			},
		},
		{
			name: "delete empty id",
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Delete(ctx, "tenant-a", "")
			},
		},
		{
			name: "list tenant with separator",
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.ListByTenant(ctx, "tenant/evil")
				return err
			},
		},
		{
			name: "list empty tenant",
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.ListByTenant(ctx, "")
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := NewProjectStore(kvstore.NewMemory())
			// Seed a project the rename cases can find.
			if err := store.Create(ctx, newTestProject("id-1", "tenant-a", "seed")); err != nil {
				t.Fatalf("seed: %v", err)
			}

			err := tc.call(ctx, store)
			if err == nil {
				t.Fatal("expected rejection, got nil")
			}
			if !errors.Is(err, ErrInvalidKeySegment) {
				t.Errorf("want ErrInvalidKeySegment, got %T: %v", err, err)
			}
			// The store's own vocabulary must match too, because pkg/service
			// maps ErrProjectInvalid onto a client error.
			if !errors.Is(err, ErrProjectInvalid) {
				t.Errorf("want ErrProjectInvalid, got %T: %v", err, err)
			}
		})
	}
}

// TestProjectKeySegmentError_Fields asserts the dual-contract error keeps the
// offending field addressable by callers that type-assert.
func TestProjectKeySegmentError_Fields(t *testing.T) {
	_, err := projectRecordKey("tenant-a", "")
	var invalid *ProjectInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("want *ProjectInvalidError, got %T: %v", err, err)
	}
	if invalid.Field != "id" {
		t.Errorf("Field = %q, want %q", invalid.Field, "id")
	}

	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		t.Fatalf("want *KeySegmentError, got %T: %v", err, err)
	}
	if segment.Field != "id" {
		t.Errorf("segment Field = %q, want %q", segment.Field, "id")
	}
	if err.Error() != invalid.Error() {
		t.Errorf("Error() = %q, want %q", err.Error(), invalid.Error())
	}
}

// TestProjectKeyError_PassThrough asserts a non key-segment error is returned
// unchanged rather than being mislabelled as a validation failure.
func TestProjectKeyError_PassThrough(t *testing.T) {
	if got := projectKeyError(errProjectKVFailure); !errors.Is(got, errProjectKVFailure) {
		t.Fatalf("want the original error back, got %T: %v", got, got)
	}
}

// =============================================================================
// Audit
// =============================================================================

// TestProjectStore_Audit_Mutations asserts each successful mutation emits
// exactly one event carrying the identifiers an auditor needs.
func TestProjectStore_Audit_Mutations(t *testing.T) {
	ctx := context.Background()
	auditor := &projectAuditRecorder{}
	store := NewProjectStore(kvstore.NewMemory(), WithStoreAuditor(auditor))

	p := newTestProject("id-1", "tenant-1", "audited")
	if err := store.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	p.Name = "audited-renamed"
	if err := store.Update(ctx, p); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := store.Delete(ctx, "tenant-1", "id-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	want := []StoreAuditEvent{
		{TenantID: "tenant-1", Resource: AuditResourceProject, ResourceID: "id-1", Action: AuditActionCreate, Success: true},
		{TenantID: "tenant-1", Resource: AuditResourceProject, ResourceID: "id-1", Action: AuditActionUpdate, Success: true},
		{TenantID: "tenant-1", Resource: AuditResourceProject, ResourceID: "id-1", Action: AuditActionDelete, Success: true},
	}
	got := auditor.snapshot()
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestProjectStore_Audit_ReadsAndFailures asserts reads are never audited while
// a rejected mutation is, which is the "attempted change was denied" signal.
func TestProjectStore_Audit_ReadsAndFailures(t *testing.T) {
	ctx := context.Background()
	auditor := &projectAuditRecorder{}
	store := NewProjectStore(kvstore.NewMemory(), WithStoreAuditor(auditor))

	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "audited")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Get(ctx, "tenant-1", "id-1"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := store.GetByName(ctx, "tenant-1", "audited"); err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if _, err := store.ListByTenant(ctx, "tenant-1"); err != nil {
		t.Fatalf("list: %v", err)
	}

	if got := auditor.snapshot(); len(got) != 1 {
		t.Fatalf("reads must not be audited, got %+v", got)
	}

	// A duplicate-name create is a denied change and must be recorded.
	if err := store.Create(ctx, newTestProject("id-2", "tenant-1", "audited")); !errors.Is(err, ErrProjectAlreadyExists) {
		t.Fatalf("want ErrProjectAlreadyExists, got %v", err)
	}
	got := auditor.snapshot()
	if len(got) != 2 {
		t.Fatalf("event count = %d, want 2: %+v", len(got), got)
	}
	denied := got[1]
	if denied.Success || denied.Action != AuditActionCreate || denied.ResourceID != "id-2" ||
		denied.TenantID != "tenant-1" || denied.Resource != AuditResourceProject {
		t.Errorf("denied event = %+v", denied)
	}
}

// TestProjectStore_Audit_NilAuditorIsInert asserts the default (unaudited)
// construction stays usable.
func TestProjectStore_Audit_NilAuditorIsInert(t *testing.T) {
	store := NewProjectStore(kvstore.NewMemory(), WithStoreAuditor(nil))
	if err := store.Create(context.Background(), newTestProject("id-1", "tenant-1", "quiet")); err != nil {
		t.Fatalf("create with nil auditor: %v", err)
	}
}

// =============================================================================
// Backend failure paths
// =============================================================================

// TestProjectStore_KVFailures asserts every method reports a backend outage as
// a typed store error instead of as a missing or corrupt project.
func TestProjectStore_KVFailures(t *testing.T) {
	tests := []struct {
		name      string
		verb      projectKVVerb
		keyPrefix string
		call      func(ctx context.Context, store ProjectStore) error
	}{
		{
			name: "create probe fails",
			verb: projectVerbExists,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-2", "tenant-1", "new"))
			},
		},
		{
			name: "create name index read fails",
			verb: projectVerbGet,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-2", "tenant-1", "new"))
			},
		},
		{
			name:      "create name reservation fails",
			verb:      projectVerbPut,
			keyPrefix: projectNameKeyspace,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-2", "tenant-1", "new"))
			},
		},
		{
			name:      "create record write fails",
			verb:      projectVerbPut,
			keyPrefix: projectKeyspace,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-2", "tenant-1", "new"))
			},
		},
		{
			name: "get fails",
			verb: projectVerbGet,
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.Get(ctx, "tenant-1", "id-1")
				return err
			},
		},
		{
			name: "get by name fails",
			verb: projectVerbGet,
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.GetByName(ctx, "tenant-1", "seed")
				return err
			},
		},
		{
			name:      "update record write fails",
			verb:      projectVerbPut,
			keyPrefix: projectKeyspace,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Update(ctx, newTestProject("id-1", "tenant-1", "seed"))
			},
		},
		{
			name:      "update rename reservation fails",
			verb:      projectVerbPut,
			keyPrefix: projectNameKeyspace,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Update(ctx, newTestProject("id-1", "tenant-1", "renamed"))
			},
		},
		{
			name:      "update rename release fails",
			verb:      projectVerbDelete,
			keyPrefix: projectNameKeyspace,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Update(ctx, newTestProject("id-1", "tenant-1", "renamed"))
			},
		},
		{
			name:      "delete record fails",
			verb:      projectVerbDelete,
			keyPrefix: projectKeyspace,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Delete(ctx, "tenant-1", "id-1")
			},
		},
		{
			name:      "delete name release fails",
			verb:      projectVerbDelete,
			keyPrefix: projectNameKeyspace,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Delete(ctx, "tenant-1", "id-1")
			},
		},
		{
			name: "list fails",
			verb: projectVerbScan,
			call: func(ctx context.Context, s ProjectStore) error {
				_, err := s.ListByTenant(ctx, "tenant-1")
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			kv := kvstore.NewMemory()

			// Seed through a healthy store so the failure is isolated to the
			// call under test.
			if err := NewProjectStore(kv).Create(ctx, newTestProject("id-1", "tenant-1", "seed")); err != nil {
				t.Fatalf("seed: %v", err)
			}

			failing := newProjectFailingKV(kv, tc.verb, tc.keyPrefix)
			err := tc.call(ctx, NewProjectStore(failing))

			var storeErr *ProjectStoreError
			if !errors.As(err, &storeErr) {
				t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
			}
		})
	}
}

// TestProjectStore_Create_RollsBackNameOnRecordFailure asserts a failed record
// write leaves no reservation behind, so the name stays claimable.
func TestProjectStore_Create_RollsBackNameOnRecordFailure(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()

	failing := newProjectFailingKV(kv, projectVerbPut, projectKeyspace)
	if err := NewProjectStore(failing).Create(ctx, newTestProject("id-1", "tenant-1", "doomed")); err == nil {
		t.Fatal("expected record write failure")
	}

	healthy := NewProjectStore(kv)
	if _, err := healthy.GetByName(ctx, "tenant-1", "doomed"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("name reservation was not rolled back: %v", err)
	}
	if err := healthy.Create(ctx, newTestProject("id-2", "tenant-1", "doomed")); err != nil {
		t.Fatalf("name must be reusable after rollback: %v", err)
	}
}

// TestProjectStore_NilKVStore asserts a store built without a backend reports a
// typed failure on every method rather than panicking.
func TestProjectStore_NilKVStore(t *testing.T) {
	ctx := context.Background()
	store := NewProjectStore(nil)

	calls := map[string]func() error{
		"create": func() error { return store.Create(ctx, newTestProject("id-1", "tenant-1", "n")) },
		"get": func() error {
			_, err := store.Get(ctx, "tenant-1", "id-1")
			return err
		},
		"get_by_name": func() error {
			_, err := store.GetByName(ctx, "tenant-1", "n")
			return err
		},
		"update": func() error { return store.Update(ctx, newTestProject("id-1", "tenant-1", "n")) },
		"delete": func() error { return store.Delete(ctx, "tenant-1", "id-1") },
		"list": func() error {
			_, err := store.ListByTenant(ctx, "tenant-1")
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var storeErr *ProjectStoreError
			if err := call(); !errors.As(err, &storeErr) {
				t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
			}
		})
	}
}

// TestProjectStore_CancelledContext asserts every method honours cancellation
// before touching the backend.
func TestProjectStore_CancelledContext(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := map[string]func() error{
		"create": func() error { return store.Create(ctx, newTestProject("id-1", "tenant-1", "n")) },
		"get": func() error {
			_, err := store.Get(ctx, "tenant-1", "id-1")
			return err
		},
		"get_by_name": func() error {
			_, err := store.GetByName(ctx, "tenant-1", "n")
			return err
		},
		"update": func() error { return store.Update(ctx, newTestProject("id-1", "tenant-1", "n")) },
		"delete": func() error { return store.Delete(ctx, "tenant-1", "id-1") },
		"list": func() error {
			_, err := store.ListByTenant(ctx, "tenant-1")
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("want context.Canceled, got %T: %v", err, err)
			}
		})
	}
}

// TestProjectStore_Create_RollbackFailureIsReported asserts that when the
// rollback of a name reservation also fails, the caller learns about both
// failures instead of only the first.
func TestProjectStore_Create_RollbackFailureIsReported(t *testing.T) {
	ctx := context.Background()
	failing := &projectFailingKV{
		KVStore: kvstore.NewMemory(),
		failures: []projectKVFailure{
			{verb: projectVerbPut, keyPrefix: projectKeyspace},
			{verb: projectVerbDelete, keyPrefix: projectNameKeyspace},
		},
	}

	err := NewProjectStore(failing).Create(ctx, newTestProject("id-1", "tenant-1", "doomed"))
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
	}
	if !strings.Contains(storeErr.Message, "rollback failed") {
		t.Errorf("message %q does not report the failed rollback", storeErr.Message)
	}
}

// projectNilValueKV models a backend that reports a missing key as (nil, nil)
// rather than as a not-found error. Both shapes exist in the tree, and both
// must read as "absent".
type projectNilValueKV struct {
	interfaces.KVStore
}

func (k *projectNilValueKV) Get(context.Context, string) ([]byte, error) { return nil, nil }

func TestProjectStore_Get_NilValueIsMissing(t *testing.T) {
	store := NewProjectStore(&projectNilValueKV{KVStore: kvstore.NewMemory()})
	if _, err := store.Get(context.Background(), "tenant-1", "id-1"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound for a nil value, got %T: %v", err, err)
	}
}

func TestProjectStore_GetByName_NilValueIsMissing(t *testing.T) {
	store := NewProjectStore(&projectNilValueKV{KVStore: kvstore.NewMemory()})
	if _, err := store.GetByName(context.Background(), "tenant-1", "name"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound for a nil value, got %T: %v", err, err)
	}
}

// TestProjectStore_GetByName_CorruptRecord asserts a decode failure behind a
// valid name reservation is reported as corruption, not as absence.
func TestProjectStore_GetByName_CorruptRecord(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()
	store := NewProjectStore(kv)

	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "named")); err != nil {
		t.Fatalf("create: %v", err)
	}
	recordKey, err := projectRecordKey("tenant-1", "id-1")
	if err != nil {
		t.Fatalf("projectRecordKey: %v", err)
	}
	if err := kv.Put(ctx, recordKey, []byte("{not json")); err != nil {
		t.Fatalf("corrupt record: %v", err)
	}

	_, err = store.GetByName(ctx, "tenant-1", "named")
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
	}
}

// TestProjectStore_Update_RenameConflict asserts renaming onto a name another
// project in the tenant already holds is refused and changes nothing.
func TestProjectStore_Update_RenameConflict(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryProjectStore()

	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "alpha")); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if err := store.Create(ctx, newTestProject("id-2", "tenant-1", "beta")); err != nil {
		t.Fatalf("create beta: %v", err)
	}

	err := store.Update(ctx, newTestProject("id-2", "tenant-1", "alpha"))
	if !errors.Is(err, ErrProjectAlreadyExists) {
		t.Fatalf("want ErrProjectAlreadyExists, got %T: %v", err, err)
	}

	// Both projects keep their original names.
	beta, err := store.GetByName(ctx, "tenant-1", "beta")
	if err != nil {
		t.Fatalf("beta must survive the rejected rename: %v", err)
	}
	if beta.ID != "id-2" {
		t.Errorf("beta ID = %q, want id-2", beta.ID)
	}
	alpha, err := store.GetByName(ctx, "tenant-1", "alpha")
	if err != nil {
		t.Fatalf("alpha must survive the rejected rename: %v", err)
	}
	if alpha.ID != "id-1" {
		t.Errorf("alpha ID = %q, want id-1", alpha.ID)
	}
}

// TestProjectStore_Update_RenameIsIdempotentOnOwnName asserts re-reserving a
// name a project already holds is not treated as a conflict.
func TestProjectStore_Update_ReclaimsOwnDanglingName(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()
	store := NewProjectStore(kv)

	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "alpha")); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Point the target name at this same project, as a half-applied rename
	// would leave it.
	nameKey, err := projectNameIndexKey("tenant-1", "beta")
	if err != nil {
		t.Fatalf("projectNameIndexKey: %v", err)
	}
	if err := kv.Put(ctx, nameKey, []byte("id-1")); err != nil {
		t.Fatalf("seed name index: %v", err)
	}

	if err := store.Update(ctx, newTestProject("id-1", "tenant-1", "beta")); err != nil {
		t.Fatalf("rename onto its own reservation: %v", err)
	}
	got, err := store.GetByName(ctx, "tenant-1", "beta")
	if err != nil {
		t.Fatalf("get by name: %v", err)
	}
	if got.ID != "id-1" {
		t.Errorf("ID = %q, want id-1", got.ID)
	}
}

// TestProjectStore_UnindexableStoredName asserts a record whose stored name
// could never have been indexed stays fully manageable: it can be renamed and
// deleted rather than wedging on a missing index entry.
func TestProjectStore_UnindexableStoredName(t *testing.T) {
	ctx := context.Background()

	seed := func(t *testing.T) (interfaces.KVStore, ProjectStore) {
		t.Helper()
		kv := kvstore.NewMemory()
		corrupt := newTestProject("id-1", "tenant-1", "bad/name")
		data, err := marshalProject(corrupt)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		key, keyErr := projectRecordKey("tenant-1", "id-1")
		if keyErr != nil {
			t.Fatalf("projectRecordKey: %v", keyErr)
		}
		if putErr := kv.Put(ctx, key, data); putErr != nil {
			t.Fatalf("seed record: %v", putErr)
		}
		return kv, NewProjectStore(kv)
	}

	t.Run("rename", func(t *testing.T) {
		_, store := seed(t)
		if err := store.Update(ctx, newTestProject("id-1", "tenant-1", "good-name")); err != nil {
			t.Fatalf("rename: %v", err)
		}
		got, err := store.GetByName(ctx, "tenant-1", "good-name")
		if err != nil {
			t.Fatalf("get by new name: %v", err)
		}
		if got.ID != "id-1" {
			t.Errorf("ID = %q, want id-1", got.ID)
		}
	})

	t.Run("delete", func(t *testing.T) {
		_, store := seed(t)
		if err := store.Delete(ctx, "tenant-1", "id-1"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := store.Get(ctx, "tenant-1", "id-1"); !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("record survived delete: %v", err)
		}
	})
}

// TestProjectStore_GetByName_UnsafeIndexValue asserts a name reservation whose
// value is not a usable project ID is refused rather than used to build a key
// that could address another tenant's subtree.
func TestProjectStore_GetByName_UnsafeIndexValue(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()
	store := NewProjectStore(kv)

	nameKey, err := projectNameIndexKey("tenant-a", "poisoned")
	if err != nil {
		t.Fatalf("projectNameIndexKey: %v", err)
	}
	if err := kv.Put(ctx, nameKey, []byte("../tenant-b/id-1")); err != nil {
		t.Fatalf("seed name index: %v", err)
	}

	if _, err := store.GetByName(ctx, "tenant-a", "poisoned"); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("want ErrInvalidKeySegment, got %T: %v", err, err)
	}
}

// TestProjectStore_Update_RenameRollsBackOnRecordFailure asserts a rename whose
// record write fails releases the new reservation and leaves the project
// reachable under its original name.
func TestProjectStore_Update_RenameRollsBackOnRecordFailure(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()

	if err := NewProjectStore(kv).Create(ctx, newTestProject("id-1", "tenant-1", "seed")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	failing := newProjectFailingKV(kv, projectVerbPut, projectKeyspace)
	err := NewProjectStore(failing).Update(ctx, newTestProject("id-1", "tenant-1", "renamed"))
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
	}

	healthy := NewProjectStore(kv)
	if _, nameErr := healthy.GetByName(ctx, "tenant-1", "renamed"); !errors.Is(nameErr, ErrProjectNotFound) {
		t.Errorf("new name reservation was not rolled back: %v", nameErr)
	}
	got, nameErr := healthy.GetByName(ctx, "tenant-1", "seed")
	if nameErr != nil {
		t.Fatalf("original name must still resolve: %v", nameErr)
	}
	if got.Name != "seed" {
		t.Errorf("Name = %q, want seed", got.Name)
	}
}

// =============================================================================
// Concurrency
// =============================================================================

func TestProjectStore_ConcurrentCreate(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			id := "id-" + string(rune('a'+n))
			name := "proj-" + string(rune('a'+n))
			errs <- store.Create(ctx, newTestProject(id, "tenant-1", name))
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent create error: %v", err)
		}
	}

	projects, err := store.ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(projects) != 10 {
		t.Errorf("expected 10 projects, got %d", len(projects))
	}
}

// TestProjectStore_ConcurrentCreate_SameName asserts the tenant name scope
// really serializes reservations: exactly one of many racing creates wins.
func TestProjectStore_ConcurrentCreate_SameName(t *testing.T) {
	store := NewInMemoryProjectStore()
	ctx := context.Background()

	const writers = 8
	errs := make(chan error, writers)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < writers; i++ {
		go func(n int) {
			start.Wait()
			errs <- store.Create(ctx, newTestProject("id-"+string(rune('a'+n)), "tenant-1", "contested"))
		}(i)
	}
	start.Done()

	var succeeded int
	for i := 0; i < writers; i++ {
		err := <-errs
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrProjectAlreadyExists):
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d creates succeeded, want exactly 1", succeeded)
	}

	projects, err := store.ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("stored %d projects, want 1: %+v", len(projects), projects)
	}
}
