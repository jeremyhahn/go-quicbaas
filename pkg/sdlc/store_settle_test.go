package sdlc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// delayedBindWriteKV holds back the first binding write: it records the
// call, does not apply it, and reports an unknown outcome. commit replays the
// held call later, as a proposal still pending in the backend would commit
// after its caller had given up on it.
type delayedBindWriteKV struct {
	qrdbsdk.AtomicKVStore

	mu       sync.Mutex
	held     bool
	op       string
	key      string
	old, val []byte
}

func (k *delayedBindWriteKV) hold(op, key string, old, value []byte) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.held || !strings.HasPrefix(key, "sdlc_binding/") {
		return false
	}
	k.held = true
	k.op, k.key = op, key
	k.old = append([]byte(nil), old...)
	k.val = append([]byte(nil), value...)
	return true
}

func (k *delayedBindWriteKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if k.hold("put", key, nil, value) {
		return false, errors.New("proposal outcome unknown")
	}
	return k.AtomicKVStore.PutIfAbsent(ctx, key, value)
}

func (k *delayedBindWriteKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	if k.hold("cas", key, old, value) {
		return false, errors.New("proposal outcome unknown")
	}
	return k.AtomicKVStore.CompareAndSwap(ctx, key, old, value)
}

// commit applies the held write now, against whatever the slot holds.
func (k *delayedBindWriteKV) commit(ctx context.Context) (bool, error) {
	k.mu.Lock()
	op, key, old, val := k.op, k.key, k.old, k.val
	held := k.held
	k.mu.Unlock()
	if !held {
		return false, errors.New("no binding write was held")
	}
	if op == "put" {
		return k.AtomicKVStore.PutIfAbsent(ctx, key, val)
	}
	return k.AtomicKVStore.CompareAndSwap(ctx, key, old, val)
}

func installDelayedBindWrite(t *testing.T, resolver *memoryResolver) *delayedBindWriteKV {
	t.Helper()
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	inner, ok := resolver.stores[testTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the test tenant store takes no guarded writes")
	}
	delayed := &delayedBindWriteKV{AtomicKVStore: inner}
	resolver.stores[testTenant] = delayed
	return delayed
}

func newSettleStore(t *testing.T) (*Store, *memoryResolver) {
	t.Helper()
	resolver := newMemoryResolver()
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	// Touch the tenant so its store exists before a wrapper is installed.
	if _, err := store.ListBindings(context.Background(), testTenant); err != nil {
		t.Fatal(err)
	}
	return store, resolver
}

// pendingCreate leaves a create of testProject held back with an unknown
// outcome and returns the held write and its token.
func pendingCreate(t *testing.T, store *Store, resolver *memoryResolver) (*delayedBindWriteKV, string) {
	t.Helper()
	delayed := installDelayedBindWrite(t, resolver)
	binding := testBinding()
	prior, err := store.PutBinding(context.Background(), testTenant, binding)
	if err == nil || !strings.Contains(err.Error(), "proposal outcome unknown") {
		t.Fatalf("PutBinding = %v, want the held write's error", err)
	}
	if prior != nil {
		t.Fatalf("held create reported prior %+v, want nil", prior)
	}
	if binding.WriteToken == "" {
		t.Fatal("held create carries no write token")
	}
	return delayed, binding.WriteToken
}

// A create whose outcome is unknown is settled as not landed, and the held
// proposal, committed after, does not apply.
func TestSettleBindingWrite_PendingCreateCannotCommitLater(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	delayed, token := pendingCreate(t, store, resolver)

	landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, token)
	if err != nil || landed {
		t.Fatalf("SettleBindingWrite = %v, %v; want false, nil", landed, err)
	}
	applied, err := delayed.commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("the held create applied after it was settled")
	}
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBinding = %v, want not found", err)
	}
	listed, err := store.ListBindings(ctx, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("ListBindings = %d records, want none", len(listed))
	}
}

// assertSlotHoldsNoBinding checks that no binding is visible for testProject.
func assertSlotHoldsNoBinding(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBinding = %v, want not found", err)
	}
	listed, err := store.ListBindings(ctx, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("ListBindings = %d records, want none", len(listed))
	}
}

// A settled create must stay unable to apply after a later bind is undone
// by an unbind: the slot is never emptied again.
func TestSettleBindingWrite_PendingCreateCannotCommitAfterBindUnbind(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	delayed, token := pendingCreate(t, store, resolver)

	if landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, token); err != nil || landed {
		t.Fatalf("SettleBindingWrite = %v, %v; want false, nil", landed, err)
	}
	if _, err := store.PutBinding(ctx, testTenant, testBinding()); err != nil {
		t.Fatalf("PutBinding B = %v", err)
	}
	if err := store.DeleteBinding(ctx, testTenant, testProject); err != nil {
		t.Fatalf("DeleteBinding B = %v", err)
	}
	applied, err := delayed.commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("the held create applied after a bind and an unbind")
	}
	assertSlotHoldsNoBinding(t, store)
}

// A settled create must stay unable to apply after a later bind is withdrawn.
func TestSettleBindingWrite_PendingCreateCannotCommitAfterWithdrawal(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	delayed, token := pendingCreate(t, store, resolver)

	if landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, token); err != nil || landed {
		t.Fatalf("SettleBindingWrite = %v, %v; want false, nil", landed, err)
	}
	second := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, second); err != nil {
		t.Fatalf("PutBinding B = %v", err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, second.WriteToken, nil)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding B = %v, %v; want true, nil", withdrawn, err)
	}
	voidToken(t, resolver)
	applied, err := delayed.commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("the held create applied after a bind was withdrawn")
	}
	assertSlotHoldsNoBinding(t, store)
}

// An unbind leaves a void marker: the binding reads as absent everywhere and
// a later bind creates afresh.
func TestDeleteBinding_LeavesAVoidMarker(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	if _, err := store.PutBinding(ctx, testTenant, testBinding()); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteBinding(ctx, testTenant, testProject); err != nil {
		t.Fatalf("DeleteBinding = %v", err)
	}
	voidToken(t, resolver)
	assertSlotHoldsNoBinding(t, store)
	if err := store.DeleteBinding(ctx, testTenant, testProject); err != nil {
		t.Fatalf("DeleteBinding over a void marker = %v, want nil", err)
	}
	prior, err := store.PutBinding(ctx, testTenant, testBinding())
	if err != nil {
		t.Fatal(err)
	}
	if prior != nil {
		t.Fatalf("PutBinding over a void marker reported prior %+v, want nil", prior)
	}
}

// A rebind whose outcome is unknown is settled as not landed: the held
// record keeps its content and gets a fresh token, so the held swap, committed
// after, does not apply.
func TestSettleBindingWrite_PendingRebindCannotCommitLater(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	first := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, first); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	delayed := installDelayedBindWrite(t, resolver)
	second := testBinding()
	second.MaxReviewRounds = first.MaxReviewRounds + 3
	if _, err := store.PutBinding(ctx, testTenant, second); err == nil {
		t.Fatal("the held rebind reported no error")
	}

	landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, second.WriteToken)
	if err != nil || landed {
		t.Fatalf("SettleBindingWrite = %v, %v; want false, nil", landed, err)
	}
	applied, err := delayed.commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("the held rebind applied after it was settled")
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxReviewRounds != first.MaxReviewRounds {
		t.Fatalf("MaxReviewRounds = %d, want the first binding's %d", got.MaxReviewRounds, first.MaxReviewRounds)
	}
	if got.WriteToken == first.WriteToken || got.WriteToken == second.WriteToken || got.WriteToken == "" {
		t.Fatalf("WriteToken = %q, want a fresh token (first %q, attempt %q)", got.WriteToken, first.WriteToken, second.WriteToken)
	}
	if !got.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want the first binding's %v", got.UpdatedAt, stored.UpdatedAt)
	}
}

// A write that landed but lost its response is reported as landed, and the
// record is left as it is.
func TestSettleBindingWrite_LandedWriteIsReported(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	resolver.mu.Lock()
	inner, ok := resolver.stores[testTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		resolver.mu.Unlock()
		t.Fatal("the test tenant store takes no guarded writes")
	}
	resolver.stores[testTenant] = &lostAckKV{AtomicKVStore: inner}
	resolver.mu.Unlock()

	binding := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, binding); err == nil {
		t.Fatal("the lost-ack put reported no error")
	}
	landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, binding.WriteToken)
	if err != nil || !landed {
		t.Fatalf("SettleBindingWrite = %v, %v; want true, nil", landed, err)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.WriteToken != binding.WriteToken {
		t.Fatalf("WriteToken = %q, want the landed write's %q", got.WriteToken, binding.WriteToken)
	}
}

// voidToken reads the void marker in testProject's slot, failing when the
// slot holds anything else.
func voidToken(t *testing.T, resolver *memoryResolver) string {
	t.Helper()
	resolver.mu.Lock()
	kv := resolver.stores[testTenant]
	resolver.mu.Unlock()
	raw, err := kv.Get(context.Background(), bindingKey(testProject))
	if err != nil {
		t.Fatal(err)
	}
	var marker struct {
		ProjectID string `json:"project_id"`
		VoidToken string `json:"void_token"`
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		t.Fatalf("slot holds %q: %v", raw, err)
	}
	if marker.ProjectID != "" || marker.VoidToken == "" {
		t.Fatalf("slot holds %q, want a void marker", raw)
	}
	return marker.VoidToken
}

// A void marker reads as no binding: settling another token over it leaves a
// fresh marker, and the next bind of the project is a create over it.
func TestSettleBindingWrite_VoidMarkerThenFreshBind(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	delayed, token := pendingCreate(t, store, resolver)
	if landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, token); err != nil || landed {
		t.Fatalf("SettleBindingWrite = %v, %v; want false, nil", landed, err)
	}
	if applied, err := delayed.commit(ctx); err != nil || applied {
		t.Fatalf("late commit = %v, %v; want false", applied, err)
	}
	firstVoid := voidToken(t, resolver)

	landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, "another-token")
	if err != nil || landed {
		t.Fatalf("settling over a void marker = %v, %v; want false, nil", landed, err)
	}
	if again := voidToken(t, resolver); again == firstVoid {
		t.Fatal("settling over a void marker left the same marker")
	}

	binding := testBinding()
	prior, err := store.PutBinding(ctx, testTenant, binding)
	if err != nil {
		t.Fatal(err)
	}
	if prior != nil {
		t.Fatalf("a bind over a void marker reported prior %+v, want nil", prior)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.WriteToken != binding.WriteToken {
		t.Fatalf("WriteToken = %q, want the fresh bind's %q", got.WriteToken, binding.WriteToken)
	}
}

func TestSettleBindingWrite_RefusesBadInput(t *testing.T) {
	ctx := context.Background()
	store, _ := newSettleStore(t)
	if _, err := store.SettleBindingWrite(ctx, testTenant, testProject, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty token = %v, want invalid input", err)
	}
	if _, err := store.SettleBindingWrite(ctx, testTenant, "", "token"); err == nil {
		t.Fatal("an empty project id was accepted")
	}
}

// Listing and the agent migration skip a void marker: it binds no project.
func TestListBindings_SkipsVoidMarker(t *testing.T) {
	ctx := context.Background()
	store, resolver := newSettleStore(t)
	other := testBinding()
	other.ProjectID = "proj-other"
	if _, err := store.PutBinding(ctx, testTenant, other); err != nil {
		t.Fatal(err)
	}
	delayed, token := pendingCreate(t, store, resolver)
	if landed, err := store.SettleBindingWrite(ctx, testTenant, testProject, token); err != nil || landed {
		t.Fatalf("SettleBindingWrite = %v, %v; want false, nil", landed, err)
	}
	if applied, err := delayed.commit(ctx); err != nil || applied {
		t.Fatalf("late commit = %v, %v; want false", applied, err)
	}
	voidToken(t, resolver)

	listed, err := store.ListBindings(ctx, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ProjectID != other.ProjectID {
		t.Fatalf("ListBindings = %+v, want only %q", listed, other.ProjectID)
	}

	seeder, err := orchestration.NewAgentSeeder(ctx, orchestration.NewInMemoryAgentCatalogStore(), testTenant)
	if err != nil {
		t.Fatal(err)
	}
	report, err := MigrateBindingAgents(ctx, store, seeder, testTenant)
	if err != nil {
		t.Fatalf("MigrateBindingAgents over a void marker = %v", err)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("MigrateBindingAgents failed %v", report.Failed)
	}
}
