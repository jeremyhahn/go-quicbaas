// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// The task store addresses records under their own tenant — go-quicbaas#918
// =============================================================================
//
// THE KV UNDERNEATH ROUTES ON THE CONTEXT, NOT ON THE KEY, and every test in
// task_store_test.go is blind to that because NewMemoryTaskKV is one flat map.
// A flat map answers every context identically, so a store that never binds its
// tenant to the context passes the entire existing suite while, in production,
// writing to one namespace and reading from another. That is exactly how #918
// shipped: the SDLC label-reconciliation sweep listed a tenant's tasks on a
// background context, was told the tenant had none, and posted `abandoned` over
// live runs — twice, on issues whose pull requests opened minutes later.
//
// So these tests supply the routing the real deployment has.

// tenantRoutingTaskKV is a KV that resolves its backing map from the tenant
// bound to the CONTEXT, the way pkg/server's tenantRoutingKVStore resolves a
// barrier-isolated tenant's store. An unbound context gets the shared namespace,
// which is go-qrdb's behaviour and the reason the defect was silent rather than
// an error.
type tenantRoutingTaskKV struct {
	mu     sync.Mutex
	stores map[string]interfaces.KVStore

	// seen records every tenant a call resolved to, so a test can assert which
	// namespace was addressed and not merely what came back.
	seen map[string]int
}

func newTenantRoutingTaskKV() *tenantRoutingTaskKV {
	return &tenantRoutingTaskKV{stores: map[string]interfaces.KVStore{}, seen: map[string]int{}}
}

// resolve returns the namespace ctx addresses, creating it on first use.
func (k *tenantRoutingTaskKV) resolve(ctx context.Context) interfaces.KVStore {
	tenant := TenantFromContext(ctx)
	k.mu.Lock()
	defer k.mu.Unlock()
	k.seen[tenant]++
	store, ok := k.stores[tenant]
	if !ok {
		store = NewMemoryTaskKV()
		k.stores[tenant] = store
	}
	return store
}

// addressed reports how many calls resolved to tenant's namespace.
func (k *tenantRoutingTaskKV) addressed(tenant string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.seen[tenant]
}

func (k *tenantRoutingTaskKV) Put(ctx context.Context, key string, value []byte) error {
	return k.resolve(ctx).Put(ctx, key, value)
}

func (k *tenantRoutingTaskKV) Get(ctx context.Context, key string) ([]byte, error) {
	return k.resolve(ctx).Get(ctx, key)
}

func (k *tenantRoutingTaskKV) Delete(ctx context.Context, key string) error {
	return k.resolve(ctx).Delete(ctx, key)
}

func (k *tenantRoutingTaskKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return k.resolve(ctx).Scan(ctx, prefix, fn)
}

func (k *tenantRoutingTaskKV) List(ctx context.Context, prefix string) ([]string, error) {
	return k.resolve(ctx).List(ctx, prefix)
}

func (k *tenantRoutingTaskKV) Exists(ctx context.Context, key string) (bool, error) {
	return k.resolve(ctx).Exists(ctx, key)
}

// PutIfAbsent and CompareAndDelete forward the guarded pair the task store
// requires for a create, routed to the same namespace as everything else — the
// real tenant router forwards them together for exactly this reason.
func (k *tenantRoutingTaskKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	store, ok := k.resolve(ctx).(guardedCreator)
	if !ok {
		return false, errNoGuardedCreate
	}
	return store.PutIfAbsent(ctx, key, value)
}

func (k *tenantRoutingTaskKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	store, ok := k.resolve(ctx).(guardedDeleter)
	if !ok {
		return false, errNoGuardedCreate
	}
	return store.CompareAndDelete(ctx, key, expected)
}

// errNoGuardedCreate reports a namespace store without the guarded pair, which
// would make the fixture silently untestable rather than loudly broken.
var errNoGuardedCreate = &TaskStoreError{
	Sentinel: ErrTaskStoreInvalidInput,
	Message:  "test fixture: the routed namespace store offers no guarded create",
}

// TestTaskStore_ListTasksReadsTheNamespaceTheWriteLandedIn is go-quicbaas#918.
//
// The write arrives the way the task-source poller makes it, on a context that
// names the tenant. The read arrives the way the label-reconciliation sweep
// makes it, on a background context that names none — and it must still be the
// same namespace, because the tenant is an argument of the call either way.
//
// Before the fix this returned zero tasks and the sweep concluded, from that
// emptiness, that the tenant's live work had been abandoned.
func TestTaskStore_ListTasksReadsTheNamespaceTheWriteLandedIn(t *testing.T) {
	t.Parallel()

	const tenantID, projectID = "automatethethings", "f335251a-5efd-4eef-9847-202acc971516"
	kv := newTenantRoutingTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())

	// The poller stamps the tenant before it creates the task.
	writeCtx := ContextWithTenant(context.Background(), tenantID)
	task := &Task{ID: "cb1aa210ef49", TenantID: tenantID, ProjectID: projectID, Subject: "adopted from a forge issue"}
	if err := store.CreateTask(writeCtx, task); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The sweep does not. It has the tenant as an argument and nothing else.
	listed, err := store.ListTasks(context.Background(), tenantID, &TaskFilter{ProjectID: projectID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("ListTasks returned %d task(s), want 1: an unstamped reader is looking in the wrong "+
			"namespace, and the label sweep reads that emptiness as an abandoned run (go-quicbaas#918)", len(listed))
	}
	if listed[0].ID != task.ID {
		t.Fatalf("ListTasks returned task %q, want %q", listed[0].ID, task.ID)
	}
	if shared := kv.addressed(""); shared != 0 {
		t.Fatalf("%d call(s) addressed the shared namespace, want none: the tenant's records do not live there", shared)
	}

	// GetTask is the same call shape and must agree, or a sweep that lists
	// nothing and a hook that fetches the record disagree about one task.
	got, err := store.GetTask(context.Background(), tenantID, task.ID)
	if err != nil {
		t.Fatalf("get on an unstamped context: %v", err)
	}
	if got.ID != task.ID {
		t.Fatalf("GetTask returned %q, want %q", got.ID, task.ID)
	}
}

// TestTaskStore_TenantsStayIsolatedOnAnUnstampedRead is the other half: binding
// the tenant must not turn every unstamped read into a read of everything. Two
// tenants hold a task apiece and each read sees only its own.
func TestTaskStore_TenantsStayIsolatedOnAnUnstampedRead(t *testing.T) {
	t.Parallel()

	kv := newTenantRoutingTaskKV()
	store := NewTaskStore(kv, NewProcessClaimArbiter())

	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		ctx := ContextWithTenant(context.Background(), tenantID)
		if err := store.CreateTask(ctx, &Task{ID: "task-" + tenantID, TenantID: tenantID, Subject: "work"}); err != nil {
			t.Fatalf("create for %s: %v", tenantID, err)
		}
	}

	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		listed, err := store.ListTasks(context.Background(), tenantID, nil)
		if err != nil {
			t.Fatalf("list for %s: %v", tenantID, err)
		}
		if len(listed) != 1 {
			t.Fatalf("%s saw %d task(s), want exactly its own", tenantID, len(listed))
		}
		if want := "task-" + tenantID; listed[0].ID != want {
			t.Fatalf("%s saw task %q, want %q: a read reached another tenant's namespace", tenantID, listed[0].ID, want)
		}
	}
}

// TestTaskTenantContext covers the helper's admission rule directly, including
// the inputs it must refuse to act on.
//
// IT MUST NEVER REDIRECT A CONTEXT THAT ALREADY NAMES A TENANT. Which tenant a
// caller may act as is decided above this layer, so a helper that overrode a
// bound tenant with a method argument would be a way to write into a tenant the
// caller was never admitted to.
func TestTaskTenantContext(t *testing.T) {
	t.Parallel()

	t.Run("stamps a context that names no tenant", func(t *testing.T) {
		t.Parallel()
		ctx := taskTenantContext(context.Background(), "tenant-a")
		if got := TenantFromContext(ctx); got != "tenant-a" {
			t.Fatalf("tenant = %q, want tenant-a", got)
		}
	})

	t.Run("leaves a context that already names one", func(t *testing.T) {
		t.Parallel()
		bound := ContextWithTenant(context.Background(), "tenant-a")
		ctx := taskTenantContext(bound, "tenant-b")
		if got := TenantFromContext(ctx); got != "tenant-a" {
			t.Fatalf("tenant = %q, want tenant-a: the argument overrode an admitted tenant", got)
		}
	})

	t.Run("an empty tenant stamps nothing", func(t *testing.T) {
		t.Parallel()
		ctx := taskTenantContext(context.Background(), "")
		if got := TenantFromContext(ctx); got != "" {
			t.Fatalf("tenant = %q, want empty: an empty argument is not a tenant to bind", got)
		}
	})
}
