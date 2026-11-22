package sdlc

import (
	"context"
	"errors"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// PutBinding stamps a fresh write token on every put and reports the record
// it replaced: nil on a create, the held record on a replacement.
func TestPutBinding_ReportsPriorAndStampsWriteToken(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := testBinding()
	prior, err := store.PutBinding(ctx, testTenant, first)
	if err != nil {
		t.Fatal(err)
	}
	if prior != nil {
		t.Fatalf("create reported prior %+v, want nil", prior)
	}
	if first.WriteToken == "" {
		t.Fatal("PutBinding stamped no write token")
	}

	second := testBinding()
	second.Strategy = first.Strategy
	second.WriteToken = "sent-in"
	prior, err = store.PutBinding(ctx, testTenant, second)
	if err != nil {
		t.Fatal(err)
	}
	if prior == nil || prior.WriteToken != first.WriteToken {
		t.Fatalf("replace reported prior %+v, want the first write (token %q)", prior, first.WriteToken)
	}
	if second.WriteToken == "" || second.WriteToken == "sent-in" || second.WriteToken == first.WriteToken {
		t.Fatalf("second write token = %q, want a fresh store-drawn one", second.WriteToken)
	}
}

// lostAckKV applies the first binding write and then reports it failed, as
// a backend does when the response of a committed write is lost.
type lostAckKV struct {
	qrdbsdk.AtomicKVStore
	lost bool
}

func (k *lostAckKV) loseAck(key string, applied bool, err error) (bool, error) {
	if err != nil || !applied || k.lost || !strings.HasPrefix(key, "sdlc_binding/") {
		return applied, err
	}
	k.lost = true
	return false, errors.New("proposal outcome unknown")
}

func (k *lostAckKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.PutIfAbsent(ctx, key, value)
	return k.loseAck(key, applied, err)
}

func (k *lostAckKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.CompareAndSwap(ctx, key, old, value)
	return k.loseAck(key, applied, err)
}

// TestPutBinding_LostWriteAckReportsThePriorAndTheToken: a write that lands
// but reports an error still returns the record it replaced, and leaves the
// attempt's token on the binding, so the caller can withdraw exactly it.
func TestPutBinding_LostWriteAckReportsThePriorAndTheToken(t *testing.T) {
	resolver := newMemoryResolver()
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, first); err != nil {
		t.Fatal(err)
	}
	resolver.mu.Lock()
	held, ok := resolver.stores[testTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		resolver.mu.Unlock()
		t.Fatal("the test tenant store takes no guarded writes")
	}
	resolver.stores[testTenant] = &lostAckKV{AtomicKVStore: held}
	resolver.mu.Unlock()

	second := testBinding()
	second.Strategy = first.Strategy
	prior, err := store.PutBinding(ctx, testTenant, second)
	if err == nil || !strings.Contains(err.Error(), "proposal outcome unknown") {
		t.Fatalf("PutBinding = %v, want the write's error", err)
	}
	if prior == nil || prior.WriteToken != first.WriteToken {
		t.Fatalf("lost-ack put reported prior %+v, want the first write (token %q)", prior, first.WriteToken)
	}
	if second.WriteToken == "" || second.WriteToken == first.WriteToken {
		t.Fatalf("lost-ack write token = %q, want the attempt's fresh token", second.WriteToken)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.WriteToken != second.WriteToken {
		t.Fatalf("stored token = %q, want the landed attempt's %q", got.WriteToken, second.WriteToken)
	}
}

// A bind that withdraws a fresh write removes the binding it created.
func TestWithdrawBinding_RemovesAFreshWrite(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	binding := testBinding()
	prior, err := store.PutBinding(ctx, testTenant, binding)
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, binding.WriteToken, prior)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding = %v, %v; want true, nil", withdrawn, err)
	}
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binding after withdrawal: %v, want not found", err)
	}
}

// A rebind that withdraws restores the record it replaced.
func TestWithdrawBinding_RebindRestoresPrior(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	original := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, original); err != nil {
		t.Fatal(err)
	}
	rebind := testBinding()
	rebind.MaxReviewRounds = original.MaxReviewRounds + 3
	prior, err := store.PutBinding(ctx, testTenant, rebind)
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, rebind.WriteToken, prior)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding = %v, %v; want true, nil", withdrawn, err)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.WriteToken != original.WriteToken || got.MaxReviewRounds != original.MaxReviewRounds ||
		!got.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("after withdrawal: %+v, want the original record back", got)
	}
}

// A bind replaced by a concurrent bind is not removed by the first bind's
// withdrawal: the token no longer matches.
func TestWithdrawBinding_TokenMismatchLeavesTheLaterBind(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	first := testBinding()
	firstPrior, err := store.PutBinding(ctx, testTenant, first)
	if err != nil {
		t.Fatal(err)
	}
	second := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, second); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, first.WriteToken, firstPrior)
	if err != nil || withdrawn {
		t.Fatalf("WithdrawBinding with a replaced token = %v, %v; want false, nil", withdrawn, err)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("the later bind was removed: %v", err)
	}
	if got.WriteToken != second.WriteToken {
		t.Fatalf("binding token = %q, want the later bind's %q", got.WriteToken, second.WriteToken)
	}
}

func TestWithdrawBinding_AbsentAndBadInput(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	// An absent create is settled by a marker, which reads as no binding.
	if withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, "tok", nil); err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding of an absent create = %v, %v; want true, nil", withdrawn, err)
	}
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binding after withdrawing an absent create: %v, want not found", err)
	}
	// An absent replace has nothing to settle.
	if withdrawn, err := store.WithdrawBinding(ctx, testTenant, "absent-project", "tok", testBinding()); err != nil || withdrawn {
		t.Fatalf("WithdrawBinding of an absent replace = %v, %v; want false, nil", withdrawn, err)
	}
	if _, err := store.WithdrawBinding(ctx, testTenant, testProject, "", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("WithdrawBinding with no token = %v, want invalid input", err)
	}
	if _, err := store.WithdrawBinding(ctx, testTenant, "", "tok", nil); err == nil {
		t.Fatal("WithdrawBinding accepted an empty project id")
	}
}

// deferredApplyKV does not apply the first binding write: it reports the
// outcome unknown and keeps the write, as a proposal that timed out but is
// still in the log does. apply commits it later, against whatever the slot
// holds then.
type deferredApplyKV struct {
	qrdbsdk.AtomicKVStore
	apply func() (bool, error)
}

func (k *deferredApplyKV) hold(key string, write func() (bool, error)) (bool, error) {
	if k.apply != nil || !strings.HasPrefix(key, "sdlc_binding/") {
		return write()
	}
	k.apply = write
	return false, errors.New("proposal outcome unknown")
}

func (k *deferredApplyKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return k.hold(key, func() (bool, error) { return k.AtomicKVStore.PutIfAbsent(ctx, key, value) })
}

func (k *deferredApplyKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	return k.hold(key, func() (bool, error) { return k.AtomicKVStore.CompareAndSwap(ctx, key, old, value) })
}

// deferTenantWrites wraps the test tenant's store in a deferredApplyKV.
func deferTenantWrites(t *testing.T, resolver *memoryResolver) *deferredApplyKV {
	t.Helper()
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	held, ok := resolver.stores[testTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the test tenant store takes no guarded writes")
	}
	deferred := &deferredApplyKV{AtomicKVStore: held}
	resolver.stores[testTenant] = deferred
	return deferred
}

// A create whose outcome is unknown and which has not applied is settled by
// the withdrawal: when it applies later, after the binding was also deleted,
// it fails, and the project stays unbound until a later bind.
func TestWithdrawBinding_SettlesAPendingCreate(t *testing.T) {
	resolver := newMemoryResolver()
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Touch the tenant so its store exists to wrap.
	if _, err := store.ListBindings(ctx, testTenant); err != nil {
		t.Fatal(err)
	}
	deferred := deferTenantWrites(t, resolver)

	binding := testBinding()
	prior, err := store.PutBinding(ctx, testTenant, binding)
	if err == nil || prior != nil || deferred.apply == nil {
		t.Fatalf("PutBinding = %+v, %v; want nil and the deferred write's error", prior, err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, binding.WriteToken, nil)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding = %v, %v; want true, nil", withdrawn, err)
	}
	if err := store.DeleteBinding(ctx, testTenant, testProject); err != nil {
		t.Fatalf("DeleteBinding: %v", err)
	}
	if applied, err := deferred.apply(); err != nil || applied {
		t.Fatalf("pending create applied after withdrawal: %v, %v; want false, nil", applied, err)
	}
	var notFound *NotFoundError
	if _, err := store.GetBinding(ctx, testTenant, testProject); !errors.As(err, &notFound) {
		t.Fatalf("binding after the pending create: %v, want not found", err)
	}
	if list, err := store.ListBindings(ctx, testTenant); err != nil || len(list) != 0 {
		t.Fatalf("ListBindings = %+v, %v; want none", list, err)
	}

	later := testBinding()
	prior, err = store.PutBinding(ctx, testTenant, later)
	if err != nil || prior != nil {
		t.Fatalf("later PutBinding = %+v, %v; want nil, nil", prior, err)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil || got.WriteToken != later.WriteToken || got.Withdrawn {
		t.Fatalf("binding after the later bind = %+v, %v; want the later bind", got, err)
	}
}

// A replace whose outcome is unknown and which has not applied is settled by
// the withdrawal: the prior record is re-stamped, so the pending swap fails
// when it applies and the prior binding's content stays.
func TestWithdrawBinding_SettlesAPendingReplace(t *testing.T) {
	resolver := newMemoryResolver()
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, first); err != nil {
		t.Fatal(err)
	}
	deferred := deferTenantWrites(t, resolver)

	second := testBinding()
	second.MaxReviewRounds = first.MaxReviewRounds + 3
	prior, err := store.PutBinding(ctx, testTenant, second)
	if err == nil || prior == nil || prior.WriteToken != first.WriteToken || deferred.apply == nil {
		t.Fatalf("PutBinding = %+v, %v; want the first record and the deferred write's error", prior, err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, second.WriteToken, prior)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding = %v, %v; want true, nil", withdrawn, err)
	}
	if applied, err := deferred.apply(); err != nil || applied {
		t.Fatalf("pending replace applied after withdrawal: %v, %v; want false, nil", applied, err)
	}
	got, err := store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxReviewRounds != first.MaxReviewRounds || got.Epoch != first.Epoch ||
		!got.UpdatedAt.Equal(first.UpdatedAt) || !got.CreatedAt.Equal(first.CreatedAt) || got.Withdrawn {
		t.Fatalf("after withdrawal: %+v, want the first record's content", got)
	}
	if got.WriteToken == first.WriteToken || got.WriteToken == second.WriteToken || got.WriteToken == "" {
		t.Fatalf("token after withdrawal = %q, want a fresh one", got.WriteToken)
	}
	// An update holding the first record's stamp still lands.
	first.MaxReviewRounds++
	if err := store.UpdateBinding(ctx, testTenant, first); err != nil {
		t.Fatalf("UpdateBinding with the first record's stamp: %v", err)
	}
}
