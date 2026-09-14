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

package iam

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// TestCreateUser_ConcurrentMembersDoNotOverwriteEachOther is go-quicbaas#341.
//
// Registering a new user in a group was read-append-write on the WHOLE group
// record. Two CreateUser calls naming the same group each read the same member
// list, each appended their own user, and the second write erased the first —
// so both users carried the group in User.Groups while only one appeared in
// Group.Members, and no error was returned to either caller. Membership is an
// authorization input, so the vanished half is a grant the system believes it
// made.
//
// The invariant is bidirectional and this asserts BOTH directions, because the
// defect satisfied one of them: every user names the group, and the group names
// every user.
func TestCreateUser_ConcurrentMembersDoNotOverwriteEachOther(t *testing.T) {
	const users = 16

	ctx := context.Background()
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  NewMemoryStorage(),
		GroupStorage: NewMemoryStorage(),
		RoleStorage:  NewMemoryStorage(),
	})

	group, err := store.CreateGroup(ctx, "t1", CreateGroupOptions{Name: "engineers"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Every create runs against the same group, and they are released together
	// so the read-modify-write windows overlap rather than queue.
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	errs := make(chan error, users)
	created := make(chan string, users)

	for i := 0; i < users; i++ {
		done.Add(1)
		go func(n int) {
			defer done.Done()
			start.Wait()
			u, cErr := store.CreateUser(ctx, "t1", CreateUserOptions{
				Email:    "member" + strconv.Itoa(n) + "@example.com",
				Username: "member" + strconv.Itoa(n),
				Name:     "Member " + strconv.Itoa(n),
				Groups:   []string{group.ID},
			})
			if cErr != nil {
				errs <- cErr
				return
			}
			created <- u.ID
		}(i)
	}
	start.Done()
	done.Wait()
	close(errs)
	close(created)

	for err := range errs {
		t.Fatalf("CreateUser: %v", err)
	}

	ids := make(map[string]struct{}, users)
	for id := range created {
		ids[id] = struct{}{}
	}
	if len(ids) != users {
		t.Fatalf("created %d users, want %d", len(ids), users)
	}

	stored, err := store.GetGroup(ctx, "t1", group.ID)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	members := make(map[string]struct{}, len(stored.Members))
	for _, id := range stored.Members {
		members[id] = struct{}{}
	}
	if len(members) != users {
		t.Fatalf("group holds %d members, want %d — a concurrent membership write was lost",
			len(members), users)
	}
	for id := range ids {
		if _, ok := members[id]; !ok {
			t.Fatalf("user %s holds the group but the group does not hold the user", id)
		}
		u, gErr := store.GetUser(ctx, "t1", id)
		if gErr != nil {
			t.Fatalf("GetUser: %v", gErr)
		}
		if !contains(u.Groups, group.ID) {
			t.Fatalf("user %s is a member but does not carry the group", id)
		}
	}
}

// TestBackendStorage_MembershipIsGuardedByCompareAndSwap covers the durable
// store, where the fix is a compare-and-swap rather than a mutex: the in-memory
// store serializes within one process, and a green test over it says nothing
// about the store the daemon actually writes through.
func TestBackendStorage_MembershipIsGuardedByCompareAndSwap(t *testing.T) {
	const users = 16

	ctx := context.Background()
	store := NewBackendStorage(newGuardedTestKV())

	group := &Group{ID: "g1", TenantID: "t1", Name: "engineers"}
	if err := store.CreateGroup(ctx, group); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	errs := make(chan error, users)

	for i := 0; i < users; i++ {
		done.Add(1)
		go func(n int) {
			defer done.Done()
			start.Wait()
			if err := store.AddGroupMember(ctx, "t1", "g1", strconv.Itoa(n+1)); err != nil {
				errs <- err
			}
		}(i)
	}
	start.Done()
	done.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("AddGroupMember: %v", err)
	}

	got, err := store.GetGroup(ctx, "t1", "g1")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(got.Members) != users {
		t.Fatalf("group holds %d members, want %d — a concurrent addition was lost",
			len(got.Members), users)
	}

	// Removal is guarded too, and both are idempotent so a compensating write
	// can be retried without the second attempt reporting failure.
	if err := store.RemoveGroupMember(ctx, "t1", "g1", "1"); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if err := store.RemoveGroupMember(ctx, "t1", "g1", "1"); err != nil {
		t.Fatalf("removing a non-member must be a no-op, got %v", err)
	}
	if err := store.AddGroupMember(ctx, "t1", "g1", "2"); err != nil {
		t.Fatalf("adding an existing member must be a no-op, got %v", err)
	}
	got, err = store.GetGroup(ctx, "t1", "g1")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(got.Members) != users-1 {
		t.Fatalf("group holds %d members, want %d", len(got.Members), users-1)
	}
}

// guardedTestKV is go-qrdb's memory KV — the supported way to exercise DAO code
// without booting a node, and a real implementation of the guarded writes —
// behind the one translation the production router also applies: qrdb answers a
// missing key with (nil, nil), while BackendStorage's existence checks read
// err == nil as "the record is there". Without it every create would report
// already_exists.
type guardedTestKV struct {
	inner *qrdbsdk.MemoryKVStore
}

func newGuardedTestKV() *guardedTestKV {
	return &guardedTestKV{inner: qrdbsdk.NewMemoryKVStore()}
}

func (k *guardedTestKV) Put(ctx context.Context, key string, value []byte) error {
	return k.inner.Put(ctx, key, value)
}

func (k *guardedTestKV) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := k.inner.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, ErrGroupNotFound
	}
	return data, nil
}

func (k *guardedTestKV) Delete(ctx context.Context, key string) error {
	return k.inner.Delete(ctx, key)
}

func (k *guardedTestKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return k.inner.Scan(ctx, prefix, fn)
}

func (k *guardedTestKV) List(ctx context.Context, prefix string) ([]string, error) {
	return k.inner.List(ctx, prefix)
}

func (k *guardedTestKV) Exists(ctx context.Context, key string) (bool, error) {
	return k.inner.Exists(ctx, key)
}

func (k *guardedTestKV) CompareAndSwap(ctx context.Context, key string, expected, next []byte) (bool, error) {
	return k.inner.CompareAndSwap(ctx, key, expected, next)
}

// unguardedKV is a KVStore with no conditional writes, standing in for a
// backing store that cannot decide a contested write.
type unguardedKV struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newUnguardedKV() *unguardedKV {
	return &unguardedKV{values: make(map[string][]byte)}
}

func (k *unguardedKV) Put(_ context.Context, key string, value []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.values[key] = append([]byte(nil), value...)
	return nil
}

func (k *unguardedKV) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	value, ok := k.values[key]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return append([]byte(nil), value...), nil
}

func (k *unguardedKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.values, key)
	return nil
}

func (k *unguardedKV) Scan(_ context.Context, _ string, _ func(key string, value []byte) error) error {
	return nil
}

func (k *unguardedKV) List(_ context.Context, _ string) ([]string, error) { return nil, nil }

func (k *unguardedKV) Exists(_ context.Context, key string) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, ok := k.values[key]
	return ok, nil
}

// TestBackendStorage_MembershipFailsClosedWithoutGuardedWrites proves the
// refusal is a refusal and not a fallback.
//
// A store with no compare-and-swap cannot edit a member list without the lost
// update this fix removes, so the edit is REFUSED. Falling back to
// read-modify-write would reinstate the defect while reporting success, which
// is the outcome the whole change exists to prevent.
func TestBackendStorage_MembershipFailsClosedWithoutGuardedWrites(t *testing.T) {
	ctx := context.Background()
	store := NewBackendStorage(newUnguardedKV())

	if err := store.CreateGroup(ctx, &Group{ID: "g1", TenantID: "t1", Name: "engineers"}); err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	err := store.AddGroupMember(ctx, "t1", "g1", "1")
	if !errors.Is(err, ErrAtomicMembershipUnsupported) {
		t.Fatalf("AddGroupMember error = %v, want ErrAtomicMembershipUnsupported", err)
	}
	if err := store.RemoveGroupMember(ctx, "t1", "g1", "1"); !errors.Is(err, ErrAtomicMembershipUnsupported) {
		t.Fatalf("RemoveGroupMember error = %v, want ErrAtomicMembershipUnsupported", err)
	}

	// Nothing was written on the way to the refusal.
	got, err := store.GetGroup(ctx, "t1", "g1")
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if len(got.Members) != 0 {
		t.Fatalf("members = %v, want the refused edit to have written nothing", got.Members)
	}
}
