package sdlc

import (
	"context"
	"errors"
	"strings"
	"sync"
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

// A withdrawal of a fresh write leaves a void marker, not an empty slot.
func TestWithdrawBinding_FreshWriteLeavesAVoidMarker(t *testing.T) {
	store, resolver := newSettleStore(t)
	ctx := context.Background()
	binding := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatal(err)
	}
	withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, binding.WriteToken, nil)
	if err != nil || !withdrawn {
		t.Fatalf("WithdrawBinding = %v, %v; want true, nil", withdrawn, err)
	}
	voidToken(t, resolver)
	assertSlotHoldsNoBinding(t, store)
}

// rejectSwapKV rejects the first binding compare-and-swap as a definite
// conflict, then either cancels the caller's context or fails the next
// binding read.
type rejectSwapKV struct {
	qrdbsdk.AtomicKVStore
	cancel   context.CancelFunc
	failRead bool

	mu       sync.Mutex
	rejected bool
}

func (k *rejectSwapKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	k.mu.Lock()
	reject := !k.rejected && strings.HasPrefix(key, "sdlc_binding/")
	k.rejected = k.rejected || reject
	k.mu.Unlock()
	if !reject {
		return k.AtomicKVStore.CompareAndSwap(ctx, key, old, value)
	}
	if k.cancel != nil {
		k.cancel()
	}
	return false, nil
}

func (k *rejectSwapKV) Get(ctx context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	fail := k.failRead && k.rejected && strings.HasPrefix(key, "sdlc_binding/")
	k.mu.Unlock()
	if fail {
		return nil, errors.New("read failed")
	}
	return k.AtomicKVStore.Get(ctx, key)
}

// boundBehind binds testProject, then installs wrap around the tenant store;
// it returns the store and the first binding.
func boundBehind(t *testing.T, wrap *rejectSwapKV) (*Store, *ProjectBinding) {
	t.Helper()
	store, resolver := newSettleStore(t)
	first := testBinding()
	if _, err := store.PutBinding(context.Background(), testTenant, first); err != nil {
		t.Fatal(err)
	}
	resolver.mu.Lock()
	held, ok := resolver.stores[testTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		resolver.mu.Unlock()
		t.Fatal("the test tenant store takes no guarded writes")
	}
	wrap.AtomicKVStore = held
	resolver.stores[testTenant] = wrap
	resolver.mu.Unlock()
	return store, first
}

// A rebind whose swap was rejected and whose context then ended attempted
// no write that can land, so it carries no token to settle.
func TestPutBinding_RejectedSwapThenCanceledCarriesNoToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, first := boundBehind(t, &rejectSwapKV{cancel: cancel})
	second := testBinding()
	if _, err := store.PutBinding(ctx, testTenant, second); err == nil {
		t.Fatal("PutBinding after a rejected swap and a cancel succeeded, want an error")
	}
	if second.WriteToken != "" {
		t.Fatalf("WriteToken = %q after a rejected swap, want empty", second.WriteToken)
	}
	got, err := store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.WriteToken != first.WriteToken {
		t.Fatalf("stored token = %q, want the first write's %q", got.WriteToken, first.WriteToken)
	}
}

// A rebind whose swap was rejected and whose next read failed carries no
// token either.
func TestPutBinding_RejectedSwapThenFailedReadCarriesNoToken(t *testing.T) {
	wrap := &rejectSwapKV{failRead: true}
	store, first := boundBehind(t, wrap)
	second := testBinding()
	if _, err := store.PutBinding(context.Background(), testTenant, second); err == nil {
		t.Fatal("PutBinding after a rejected swap and a failed read succeeded, want an error")
	}
	if second.WriteToken != "" {
		t.Fatalf("WriteToken = %q after a rejected swap, want empty", second.WriteToken)
	}
	wrap.mu.Lock()
	wrap.failRead = false
	wrap.mu.Unlock()
	got, err := store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if got.WriteToken != first.WriteToken {
		t.Fatalf("stored token = %q, want the first write's %q", got.WriteToken, first.WriteToken)
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
	if withdrawn, err := store.WithdrawBinding(ctx, testTenant, testProject, "tok", nil); err != nil || withdrawn {
		t.Fatalf("WithdrawBinding of an absent binding = %v, %v; want false, nil", withdrawn, err)
	}
	if _, err := store.WithdrawBinding(ctx, testTenant, testProject, "", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("WithdrawBinding with no token = %v, want invalid input", err)
	}
	if _, err := store.WithdrawBinding(ctx, testTenant, "", "tok", nil); err == nil {
		t.Fatal("WithdrawBinding accepted an empty project id")
	}
}
