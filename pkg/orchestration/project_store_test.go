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
	"strconv"
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
			FleetNodeID: "agent-1",
			Path:        "/workspace/foo",
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

// PutIfAbsent counts as a put: a guarded create IS the write. Embedding
// interfaces.KVStore does not carry this verb (see forwardPutIfAbsent), so
// without this method every create through the wrapper is refused.
func (k *projectCountingKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	k.puts.Add(1)
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

// CompareAndDelete counts as a delete, for the same reason.
func (k *projectCountingKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	k.deletes.Add(1)
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
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

// PutIfAbsent honours an injected put failure: a guarded create is a write, so
// a store failing writes must fail creates.
func (k *projectFailingKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if k.shouldFail(projectVerbPut, key) {
		return false, errProjectKVFailure
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

// CompareAndDelete honours an injected delete failure, likewise.
func (k *projectFailingKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	if k.shouldFail(projectVerbDelete, key) {
		return false, errProjectKVFailure
	}
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
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

func TestProjectStore_Create_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		project *Project
	}{
		{"nil project", nil},
		{"empty ID", newTestProject("", "tenant-1", "name")},
		{"empty tenantID", newTestProject("id-1", "", "name")},
		{"empty name", newTestProject("id-1", "tenant-1", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryProjectStore()
			err := store.Create(context.Background(), tt.project)
			if !errors.Is(err, ErrProjectInvalid) {
				t.Errorf("expected ErrProjectInvalid, got %v", err)
			}
		})
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

// TestProjectStore_ArgumentAndNotFoundErrors drives each method with an
// invalid argument or an absent record and checks the typed error it returns.
func TestProjectStore_ArgumentAndNotFoundErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		call func(s ProjectStore) error
		want error
	}{
		{"Get not found", func(s ProjectStore) error {
			_, err := s.Get(ctx, "tenant-1", "missing-id")
			return err
		}, ErrProjectNotFound},
		{"Get empty tenantID", func(s ProjectStore) error {
			_, err := s.Get(ctx, "", "id-1")
			return err
		}, ErrProjectInvalid},
		{"GetByName not found", func(s ProjectStore) error {
			_, err := s.GetByName(ctx, "tenant-1", "ghost")
			return err
		}, ErrProjectNotFound},
		{"GetByName empty name", func(s ProjectStore) error {
			_, err := s.GetByName(ctx, "tenant-1", "")
			return err
		}, ErrProjectInvalid},
		{"Update not found", func(s ProjectStore) error {
			return s.Update(ctx, newTestProject("ghost", "tenant-1", "ghost"))
		}, ErrProjectNotFound},
		{"Update nil project", func(s ProjectStore) error {
			return s.Update(ctx, nil)
		}, ErrProjectInvalid},
		{"Delete not found", func(s ProjectStore) error {
			return s.Delete(ctx, "tenant-1", "ghost")
		}, ErrProjectNotFound},
		{"Delete empty ID", func(s ProjectStore) error {
			return s.Delete(ctx, "tenant-1", "")
		}, ErrProjectInvalid},
		{"ListByTenant empty tenantID", func(s ProjectStore) error {
			_, err := s.ListByTenant(ctx, "")
			return err
		}, ErrProjectInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(NewInMemoryProjectStore())
			if !errors.Is(err, tt.want) {
				t.Errorf("expected %v, got %T: %v", tt.want, err, err)
			}
		})
	}
}

// TestProjectStore_Get_CorruptRecord asserts a record that is not decodable is
// reported as a store failure rather than as a missing project, so operators
// can tell corruption apart from absence.
func TestProjectStore_Get_CorruptRecord(t *testing.T) {
	kv := NewMemoryTaskKV()
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

// TestProjectStore_GetByName_DanglingIndex asserts a name reservation whose
// record is gone reads as not-found rather than as an internal failure.
func TestProjectStore_GetByName_DanglingIndex(t *testing.T) {
	kv := NewMemoryTaskKV()
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
	kv := NewMemoryTaskKV()
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
		{name: "memory", kv: func() interfaces.KVStore { return NewMemoryTaskKV() }},
		{
			name: "memory_counted",
			kv:   func() interfaces.KVStore { return &projectCountingKV{KVStore: NewMemoryTaskKV()} },
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
	kv := NewMemoryTaskKV()

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
			store := NewProjectStore(NewMemoryTaskKV())

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
	kv := NewMemoryTaskKV()
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
			store := NewProjectStore(NewMemoryTaskKV())
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
	store := NewProjectStore(NewMemoryTaskKV(), WithStoreAuditor(auditor))

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
	store := NewProjectStore(NewMemoryTaskKV(), WithStoreAuditor(auditor))

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
	store := NewProjectStore(NewMemoryTaskKV(), WithStoreAuditor(nil))
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
		// No "create probe fails" case: the guarded create issues ONE operation,
		// so there is no probe left to fail independently of the write. The put
		// cases below cover the whole create path.
		{
			// CONTESTED on purpose. The create path reads the name index only
			// when its guarded claim loses, to learn who holds the name, so an
			// uncontested create issues no Get for this failure to land on.
			name: "create name index read fails",
			verb: projectVerbGet,
			call: func(ctx context.Context, s ProjectStore) error {
				return s.Create(ctx, newTestProject("id-2", "tenant-1", "seed"))
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
			kv := NewMemoryTaskKV()

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
//
// Under the record-before-name order the reservation is never written at all,
// which is a stronger form of the same guarantee. The assertion is on the
// observable outcome rather than on which write was skipped, so it holds
// either way.
func TestProjectStore_Create_RollsBackNameOnRecordFailure(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryTaskKV()

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

// TestProjectStore_Create_RollbackFailureIsReported asserts that when a create
// loses its name AND cannot discard the record it already wrote, the caller
// learns about both failures instead of only the first.
//
// Create writes the record first, so the rollback it can fail at is the record
// discard, not a reservation release. The reservation release still exists on
// the rename path, which
// TestProjectStore_Update_RenameRollbackFailureIsReported covers.
func TestProjectStore_Create_RollbackFailureIsReported(t *testing.T) {
	ctx := context.Background()
	failing := &projectFailingKV{
		KVStore: NewMemoryTaskKV(),
		failures: []projectKVFailure{
			{verb: projectVerbPut, keyPrefix: projectNameKeyspace},
			{verb: projectVerbDelete, keyPrefix: projectKeyspace},
		},
	}

	err := NewProjectStore(failing).Create(ctx, newTestProject("id-1", "tenant-1", "doomed"))
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
	}
	if !strings.Contains(storeErr.Message, "record discard failed") {
		t.Errorf("message %q does not report the failed discard", storeErr.Message)
	}
}

// TestProjectStore_Update_RenameRollbackFailureIsReported asserts the same for
// the one path that still rolls back a reservation: a rename that reserves the
// new name, fails to write the record, and then cannot give the reservation
// back.
func TestProjectStore_Update_RenameRollbackFailureIsReported(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryTaskKV()
	if err := NewProjectStore(kv).Create(ctx, newTestProject("id-1", "tenant-1", "seed")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	failing := &projectFailingKV{
		KVStore: kv,
		failures: []projectKVFailure{
			{verb: projectVerbPut, keyPrefix: projectKeyspace},
			{verb: projectVerbDelete, keyPrefix: projectNameKeyspace},
		},
	}

	err := NewProjectStore(failing).Update(ctx, newTestProject("id-1", "tenant-1", "renamed"))
	var storeErr *ProjectStoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("want *ProjectStoreError, got %T: %v", err, err)
	}
	if !strings.Contains(storeErr.Message, "name reservation release failed") {
		t.Errorf("message %q does not report the failed release", storeErr.Message)
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
	store := NewProjectStore(&projectNilValueKV{KVStore: NewMemoryTaskKV()})
	if _, err := store.Get(context.Background(), "tenant-1", "id-1"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound for a nil value, got %T: %v", err, err)
	}
}

func TestProjectStore_GetByName_NilValueIsMissing(t *testing.T) {
	store := NewProjectStore(&projectNilValueKV{KVStore: NewMemoryTaskKV()})
	if _, err := store.GetByName(context.Background(), "tenant-1", "name"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("want ErrProjectNotFound for a nil value, got %T: %v", err, err)
	}
}

// TestProjectStore_GetByName_CorruptRecord asserts a decode failure behind a
// valid name reservation is reported as corruption, not as absence.
func TestProjectStore_GetByName_CorruptRecord(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryTaskKV()
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
	kv := NewMemoryTaskKV()
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
		kv := NewMemoryTaskKV()
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
	kv := NewMemoryTaskKV()
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
	kv := NewMemoryTaskKV()

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

// =============================================================================
// Multi-node name uniqueness
// =============================================================================

// projectHoldBeforeRecord parks one node on the threshold of its record write,
// which under the record-before-name order is the first thing a create does.
// A node held there has claimed nothing yet, so it is the slowest possible
// loser of the name race.
type projectHoldBeforeRecord struct {
	interfaces.KVStore

	recordKey string
	parked    chan struct{}
	release   chan struct{}
	once      sync.Once
}

func newProjectHoldBeforeRecord(inner interfaces.KVStore, recordKey string) *projectHoldBeforeRecord {
	return &projectHoldBeforeRecord{
		KVStore:   inner,
		recordKey: recordKey,
		parked:    make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (k *projectHoldBeforeRecord) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if key == k.recordKey {
		k.once.Do(func() { close(k.parked) })
		select {
		case <-k.release:
		case <-time.After(barrierWait):
		}
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *projectHoldBeforeRecord) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

// TestProjectStore_Create_NineNodesOneNameProduceOneProject proves the name is a
// single-winner decision across the fleet, not a probe under a local lock.
func TestProjectStore_Create_NineNodesOneNameProduceOneProject(t *testing.T) {
	const nodes = 9

	ctx := context.Background()
	shared := NewMemoryTaskKV()

	var (
		wg    sync.WaitGroup
		start = make(chan struct{})
		errs  = make([]error, nodes)
	)
	for i := 0; i < nodes; i++ {
		wg.Add(1)
		go func(node int) {
			defer wg.Done()
			store := NewProjectStore(shared)
			<-start
			errs[node] = store.Create(ctx, newTestProject("id-"+strconv.Itoa(node), "tenant-1", "contested"))
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	winner := -1
	for node, err := range errs {
		if err == nil {
			winners++
			winner = node
			continue
		}
		if !errors.Is(err, ErrProjectAlreadyExists) {
			t.Fatalf("node %d lost but got %T: %v, want ErrProjectAlreadyExists", node, err, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d nodes created a project named %q, want exactly 1", winners, "contested")
	}

	// The reservation must name the winner. A name pointing at a loser means a
	// reservation outlived the create that lost.
	nameKey, err := projectNameIndexKey("tenant-1", "contested")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	owner, err := shared.Get(ctx, nameKey)
	if err != nil {
		t.Fatalf("Get(nameKey) error = %v", err)
	}
	if want := "id-" + strconv.Itoa(winner); string(owner) != want {
		t.Fatalf("name reserved for %q, but %q won", string(owner), want)
	}
}

// TestProjectStore_Create_SlowCreatorWithdrawsWhenItLosesTheName pins what the
// LOSER of the name race owes: a typed conflict and no residue.
//
// Node A stalls before its first write while node B creates the same name end
// to end. A then runs: it writes its record, finds the name taken by a project
// whose record exists — so not abandoned, and not healable — and must discard
// the record it just wrote. A record for a project that never got its name is
// reachable by ID and invisible by name, so leaving one behind would be the
// orphan this ordering is supposed to confine to a crash.
func TestProjectStore_Create_SlowCreatorWithdrawsWhenItLosesTheName(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()

	slowRecordKey, err := projectRecordKey("tenant-1", "id-slow")
	if err != nil {
		t.Fatalf("projectRecordKey() error = %v", err)
	}
	held := newProjectHoldBeforeRecord(shared, slowRecordKey)

	var (
		wg      sync.WaitGroup
		slowErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		slowErr = NewProjectStore(held).Create(ctx, newTestProject("id-slow", "tenant-1", "contested"))
	}()

	select {
	case <-held.parked:
	case <-time.After(barrierWait):
		t.Fatal("the slow node never reached its record write")
	}

	if err := NewProjectStore(shared).Create(ctx, newTestProject("id-fast", "tenant-1", "contested")); err != nil {
		t.Fatalf("the fast node could not create an uncontested name: %v", err)
	}

	close(held.release)
	wg.Wait()

	if !errors.Is(slowErr, ErrProjectAlreadyExists) {
		t.Fatalf("the slow node kept a name the fast node holds: err = %T: %v, want ErrProjectAlreadyExists",
			slowErr, slowErr)
	}

	// It must also leave nothing behind: a record for a project that lost its
	// name is unreachable by name and owned by nobody.
	//
	// Absence is checked by VALUE, not by error. Both shapes exist in this tree
	// — some stores report a missing key as a not-found error and go-qrdb's
	// memory store reports (nil, nil) — so an error-only check passes vacuously
	// against the store this test actually uses.
	leftover, err := shared.Get(ctx, slowRecordKey)
	if err == nil && len(leftover) > 0 {
		t.Errorf("the withdrawing node left its record behind: %s", leftover)
	}

	nameKey, err := projectNameIndexKey("tenant-1", "contested")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	owner, err := shared.Get(ctx, nameKey)
	if err != nil {
		t.Fatalf("Get(nameKey) error = %v", err)
	}
	if string(owner) != "id-fast" {
		t.Fatalf("name reserved for %q, want id-fast", string(owner))
	}
}

// TestProjectStore_Create_ReclaimsAReservationWhoseProjectIsGone covers the one
// state the heal still exists for, and the only writer that can produce it.
//
// Delete removes the record and then the name. A Delete that dies between the
// two leaves a reservation naming a project that no longer exists — the
// predicate reserveName heals on. It is decidable precisely because no creator
// can be in that state: a reservation is published only after its record.
//
// The half-finished Delete is performed, not simulated by planting a key, so
// the test would notice if Delete ever stopped producing it.
func TestProjectStore_Create_ReclaimsAReservationWhoseProjectIsGone(t *testing.T) {
	ctx := context.Background()
	kv := NewMemoryTaskKV()
	healthy := NewProjectStore(kv)

	if err := healthy.Create(ctx, newTestProject("id-gone", "tenant-1", "taken")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// A Delete that cannot release the name: the record goes, the reservation
	// stays.
	crashing := NewProjectStore(newProjectFailingKV(kv, projectVerbDelete, projectNameKeyspace))
	if err := crashing.Delete(ctx, "tenant-1", "id-gone"); err == nil {
		t.Fatal("the name release was supposed to fail, leaving the reservation behind")
	}

	nameKey, err := projectNameIndexKey("tenant-1", "taken")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	stranded, err := kv.Get(ctx, nameKey)
	if err != nil || string(stranded) != "id-gone" {
		t.Fatalf("want the reservation still naming id-gone, got %q (err %v)", string(stranded), err)
	}

	if err := healthy.Create(ctx, newTestProject("id-new", "tenant-1", "taken")); err != nil {
		t.Fatalf("a name whose project is gone must be reclaimable: %v", err)
	}

	reclaimed, err := healthy.GetByName(ctx, "tenant-1", "taken")
	if err != nil {
		t.Fatalf("GetByName() error = %v", err)
	}
	if reclaimed.ID != "id-new" {
		t.Fatalf("name resolves to %q, want id-new", reclaimed.ID)
	}
}

// projectHoldAfterReservation parks one node the instant its name reservation
// lands, and holds it there until the test lets go.
//
// It parks AFTER the write rather than before it, because the state under test
// is the one that only exists once the reservation is durable and its creator
// has not yet returned. A hook that parked before the write would never
// produce it.
type projectHoldAfterReservation struct {
	interfaces.KVStore

	nameKey string
	parked  chan struct{}
	release chan struct{}
	once    sync.Once
}

func newProjectHoldAfterReservation(inner interfaces.KVStore, nameKey string) *projectHoldAfterReservation {
	return &projectHoldAfterReservation{
		KVStore: inner,
		nameKey: nameKey,
		parked:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (k *projectHoldAfterReservation) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	stored, err := forwardPutIfAbsent(ctx, k.KVStore, key, value)
	if key != k.nameKey || !stored || err != nil {
		return stored, err
	}
	k.once.Do(func() { close(k.parked) })
	select {
	case <-k.release:
	case <-time.After(barrierWait):
	}
	return stored, err
}

func (k *projectHoldAfterReservation) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

// TestProjectStore_Create_LiveCreatorKeepsItsReservation is the invariant, and
// it is driven rather than raced.
//
//	A CREATOR THAT HAS NOT CRASHED CANNOT HAVE ITS RESERVATION TAKEN.
//
// The holder is parked the instant its reservation lands and the rival then
// runs its ENTIRE create — claim, heal probe, re-claim, record — against a
// creator that is demonstrably alive and has not returned. That is the exact
// interleaving that admitted two projects under one name: a healer that reads
// "the holder has no record" as "the holder crashed" steals the name here, and
// both creates then write their own uncontested record keys and both succeed.
//
// DETERMINISM IS THE POINT. The same defect was reachable only under load: it
// survived 200 sequential runs, 100 under -race, and 4800 runs across twelve
// contending GOMAXPROCS=1 workers, and appeared on a loaded CI runner. A
// regression test that needs the scheduler to cooperate is not a regression
// test, so this one removes the scheduler from the question — the rival cannot
// run until the holder is parked, and the holder cannot resume until the rival
// is done.
//
// It is scheduled against go-qrdb's own memory KVStore, whose PutIfAbsent and
// CompareAndDelete are the real guarded primitives, not a double written to
// agree with this store. Nothing here asks a consensus layer to arbitrate —
// the interleaving is imposed, not discovered — so the heavier real-node
// fixture would add seconds and answer a question this test is not asking.
func TestProjectStore_Create_LiveCreatorKeepsItsReservation(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()

	nameKey, err := projectNameIndexKey("tenant-1", "contested")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	held := newProjectHoldAfterReservation(shared, nameKey)

	var (
		wg        sync.WaitGroup
		holderErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		holderErr = NewProjectStore(held).Create(ctx, newTestProject("id-holder", "tenant-1", "contested"))
	}()

	select {
	case <-held.parked:
	case <-time.After(barrierWait):
		t.Fatal("the holding node never published its reservation")
	}

	rivalErr := NewProjectStore(shared).Create(ctx, newTestProject("id-rival", "tenant-1", "contested"))

	close(held.release)
	wg.Wait()

	if holderErr != nil {
		t.Fatalf("a live creator lost the name it had already reserved: %T: %v; "+
			"its reservation may only be taken if it crashed, and it did not", holderErr, holderErr)
	}
	if !errors.Is(rivalErr, ErrProjectAlreadyExists) {
		t.Fatalf("the rival took a live creator's name: err = %T: %v, want ErrProjectAlreadyExists",
			rivalErr, rivalErr)
	}

	owner, err := shared.Get(ctx, nameKey)
	if err != nil {
		t.Fatalf("Get(nameKey) error = %v", err)
	}
	if string(owner) != "id-holder" {
		t.Fatalf("name reserved for %q, want id-holder", string(owner))
	}

	// The name deciding one winner is only half of it. The defect this pins
	// admitted two RECORDS, so the tenant is checked for the second one.
	projects, err := NewProjectStore(shared).ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("ListByTenant() error = %v", err)
	}
	if len(projects) != 1 {
		ids := make([]string, 0, len(projects))
		for _, project := range projects {
			ids = append(ids, project.ID)
		}
		t.Fatalf("tenant holds %d projects (%v), want exactly 1 named %q", len(projects), ids, "contested")
	}
	if projects[0].ID != "id-holder" {
		t.Fatalf("the surviving project is %q, want id-holder", projects[0].ID)
	}
}

// projectHoldAfterHealProbe parks one node immediately after it has asked
// whether a name's holder still has a record — the read on which the heal
// decision turns — and reports what the probe saw.
type projectHoldAfterHealProbe struct {
	interfaces.KVStore

	parked  chan struct{}
	release chan struct{}
	once    sync.Once
	saw     atomic.Bool
}

func newProjectHoldAfterHealProbe(inner interfaces.KVStore) *projectHoldAfterHealProbe {
	return &projectHoldAfterHealProbe{
		KVStore: inner,
		parked:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (k *projectHoldAfterHealProbe) Exists(ctx context.Context, key string) (bool, error) {
	exists, err := k.KVStore.Exists(ctx, key)
	if err != nil {
		return exists, err
	}
	k.saw.Store(exists)
	k.once.Do(func() { close(k.parked) })
	select {
	case <-k.release:
	case <-time.After(barrierWait):
	}
	return exists, err
}

func (k *projectHoldAfterHealProbe) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *projectHoldAfterHealProbe) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

// TestProjectStore_Create_TwoNodesCannotBothWinOneName drives the interleaving
// from the CI failure itself, step by step, with nothing left to the scheduler.
//
//	t1  the holder publishes its reservation and parks
//	t2  the rival loses the claim, probes the holder's record, and parks
//	t3  the holder runs to completion and reports SUCCESS
//	t4  the rival resumes, acts on the probe it took at t2, and finishes
//
// At t2 the probe is the whole decision. Under the old reserve-then-record
// order it answered "no record", which the heal read as "the creator crashed"
// even though the creator was merely between its two writes; the rival then
// cleared a LIVE reservation at t4, took the name, and wrote its own record
// key, which no one contested. Both creates returned nil and the tenant held
// two projects called "contested" — the exact observation that reddened
// test-lean, and which the holder's own post-write re-read could not catch
// because it had already run at t3.
//
// Writing the record first makes the t2 probe answer "record present", so the
// heal does not fire at all. The assertion is on the outcome the defect
// violated: ONE project, and the node that reserved the name is the one that
// has it.
func TestProjectStore_Create_TwoNodesCannotBothWinOneName(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()

	nameKey, err := projectNameIndexKey("tenant-1", "contested")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	holderKV := newProjectHoldAfterReservation(shared, nameKey)
	rivalKV := newProjectHoldAfterHealProbe(shared)

	var (
		holderDone = make(chan struct{})
		rivalDone  = make(chan struct{})
		holderErr  error
		rivalErr   error
	)
	go func() {
		defer close(holderDone)
		holderErr = NewProjectStore(holderKV).Create(ctx, newTestProject("id-holder", "tenant-1", "contested"))
	}()

	// t1.
	select {
	case <-holderKV.parked:
	case <-time.After(barrierWait):
		t.Fatal("the holding node never published its reservation")
	}

	go func() {
		defer close(rivalDone)
		rivalErr = NewProjectStore(rivalKV).Create(ctx, newTestProject("id-rival", "tenant-1", "contested"))
	}()

	// t2.
	select {
	case <-rivalKV.parked:
	case <-time.After(barrierWait):
		t.Fatal("the rival node never probed the reservation holder")
	}

	// t3 — the holder finishes while the rival still holds a stale probe.
	close(holderKV.release)
	select {
	case <-holderDone:
	case <-time.After(barrierWait):
		t.Fatal("the holding node never completed")
	}

	// t4.
	close(rivalKV.release)
	select {
	case <-rivalDone:
	case <-time.After(barrierWait):
		t.Fatal("the rival node never completed")
	}

	// The probe is the defect in one bit: a live holder must look live.
	if !rivalKV.saw.Load() {
		t.Error("the rival probed a LIVE holder and saw no record; a reservation is " +
			"published only after its record, so that reads as a crash it can heal")
	}

	if holderErr != nil {
		t.Fatalf("the holder lost a name it had reserved: %T: %v", holderErr, holderErr)
	}
	if !errors.Is(rivalErr, ErrProjectAlreadyExists) {
		t.Fatalf("both nodes created %q: rival err = %T: %v, want ErrProjectAlreadyExists",
			"contested", rivalErr, rivalErr)
	}

	projects, err := NewProjectStore(shared).ListByTenant(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("ListByTenant() error = %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "id-holder" {
		ids := make([]string, 0, len(projects))
		for _, project := range projects {
			ids = append(ids, project.ID)
		}
		t.Fatalf("tenant holds %v, want exactly [id-holder]", ids)
	}
}

// TestProjectStore_Create_DoesNotHealARenameInFlight is the deliberate
// non-healing, and it guards a decision that is easy to "improve" into a bug.
//
// A reservation naming a LIVE project whose current name differs is ambiguous:
// it is either a rename toward this name still in flight, or a rename away from
// it that crashed. Those two produce byte-identical key states, so no read
// distinguishes them. Healing on that predicate would kill live renames.
//
// This test pins the safe answer: the create is REFUSED. If someone later
// extends the heal predicate to cover name mismatches, this fails — which is
// the point.
func TestProjectStore_Create_DoesNotHealARenameInFlight(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()
	store := NewProjectStore(shared)

	// A live project under its current name.
	if err := store.Create(ctx, newTestProject("id-1", "tenant-1", "old-name")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// The state a rename toward "new-name" has after its first write: the
	// reservation exists, the record still says "old-name".
	newNameKey, err := projectNameIndexKey("tenant-1", "new-name")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	if err := shared.Put(ctx, newNameKey, []byte("id-1")); err != nil {
		t.Fatalf("Put(newNameKey) error = %v", err)
	}

	err = store.Create(ctx, newTestProject("id-2", "tenant-1", "new-name"))
	if !errors.Is(err, ErrProjectAlreadyExists) {
		t.Fatalf("a create healed a reservation held by a LIVE project: err = %T: %v; "+
			"that state is indistinguishable from a rename in flight, and healing it "+
			"steals the name from a rename about to complete", err, err)
	}

	// And the reservation is untouched.
	owner, err := shared.Get(ctx, newNameKey)
	if err != nil {
		t.Fatalf("Get(newNameKey) error = %v", err)
	}
	if string(owner) != "id-1" {
		t.Fatalf("reservation now names %q; the refused create must not have touched it", string(owner))
	}
}

// projectHoldAfterRecordDelete parks a Delete the instant its record removal
// lands — after the record is gone and before the name reservation is
// released — and holds it there until the test lets go.
type projectHoldAfterRecordDelete struct {
	interfaces.KVStore

	recordKey string
	parked    chan struct{}
	release   chan struct{}
	once      sync.Once
}

func newProjectHoldAfterRecordDelete(inner interfaces.KVStore, recordKey string) *projectHoldAfterRecordDelete {
	return &projectHoldAfterRecordDelete{
		KVStore:   inner,
		recordKey: recordKey,
		parked:    make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (k *projectHoldAfterRecordDelete) Delete(ctx context.Context, key string) error {
	err := k.KVStore.Delete(ctx, key)
	if key != k.recordKey || err != nil {
		return err
	}
	k.once.Do(func() { close(k.parked) })
	select {
	case <-k.release:
	case <-time.After(barrierWait):
	}
	return nil
}

func (k *projectHoldAfterRecordDelete) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

func (k *projectHoldAfterRecordDelete) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return forwardCompareAndDelete(ctx, k.KVStore, key, expected)
}

// TestProjectStore_Delete_DoesNotReleaseAReservationItNoLongerHolds drives the
// interleaving an unguarded name release loses (go-qrdb#41, issue #846):
//
//  1. Delete removes id-A's record and is parked before releasing the name.
//  2. A create of the same name finds the reservation naming id-A with no
//     record, correctly heals it, claims the name for id-B and succeeds.
//  3. Delete resumes. An unguarded release would now remove id-B's live
//     reservation; the guarded one releases only id-A's own hold.
//
// Driven, not raced: the create cannot run until the Delete is parked, and the
// Delete cannot resume until the create has returned.
func TestProjectStore_Delete_DoesNotReleaseAReservationItNoLongerHolds(t *testing.T) {
	ctx := context.Background()
	shared := NewMemoryTaskKV()

	if err := NewProjectStore(shared).Create(ctx, newTestProject("id-A", "tenant-1", "contested")); err != nil {
		t.Fatalf("seed Create() error = %v", err)
	}

	recordKey, err := projectRecordKey("tenant-1", "id-A")
	if err != nil {
		t.Fatalf("projectRecordKey() error = %v", err)
	}
	nameKey, err := projectNameIndexKey("tenant-1", "contested")
	if err != nil {
		t.Fatalf("projectNameIndexKey() error = %v", err)
	}
	held := newProjectHoldAfterRecordDelete(shared, recordKey)

	var (
		wg        sync.WaitGroup
		deleteErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		deleteErr = NewProjectStore(held).Delete(ctx, "tenant-1", "id-A")
	}()

	select {
	case <-held.parked:
	case <-time.After(barrierWait):
		t.Fatal("the Delete never removed its record")
	}

	createErr := NewProjectStore(shared).Create(ctx, newTestProject("id-B", "tenant-1", "contested"))

	close(held.release)
	wg.Wait()

	if createErr != nil {
		t.Fatalf("Create() of a name whose project was deleted: %T: %v; the reservation "+
			"named a project with no record and should have been healed", createErr, createErr)
	}
	if deleteErr != nil {
		t.Fatalf("Delete() error = %T: %v; finding the name already reclaimed is a no-op, "+
			"the project record is gone either way", deleteErr, deleteErr)
	}

	owner, err := shared.Get(ctx, nameKey)
	if err != nil {
		t.Fatalf("Get(nameKey) error = %v; the resumed Delete released id-B's reservation", err)
	}
	if string(owner) != "id-B" {
		t.Fatalf("name reserved for %q, want id-B", string(owner))
	}

	got, err := NewProjectStore(shared).GetByName(ctx, "tenant-1", "contested")
	if err != nil {
		t.Fatalf("GetByName() error = %T: %v; id-B is live and must stay reachable by name", err, err)
	}
	if got.ID != "id-B" {
		t.Fatalf("GetByName() = %q, want id-B", got.ID)
	}
}
