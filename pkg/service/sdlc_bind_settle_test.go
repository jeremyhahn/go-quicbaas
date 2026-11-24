package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// delayedBindWriteKV holds back the first binding write: it records the
// call, does not apply it, runs hook, and reports an unknown outcome. commit
// replays the held call later, as a proposal still pending in the backend
// would commit after the bind had returned.
type delayedBindWriteKV struct {
	qrdbsdk.AtomicKVStore
	hook func()

	mu       sync.Mutex
	held     bool
	op       string
	key      string
	old, val []byte
}

func (k *delayedBindWriteKV) hold(op, key string, old, value []byte) bool {
	k.mu.Lock()
	if k.held || !strings.HasPrefix(key, "sdlc_binding/") {
		k.mu.Unlock()
		return false
	}
	k.held = true
	k.op, k.key = op, key
	k.old = append([]byte(nil), old...)
	k.val = append([]byte(nil), value...)
	k.mu.Unlock()
	if k.hook != nil {
		k.hook()
	}
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
func (k *delayedBindWriteKV) commit(t *testing.T) bool {
	t.Helper()
	k.mu.Lock()
	op, key, old, val, held := k.op, k.key, k.old, k.val, k.held
	k.mu.Unlock()
	if !held {
		t.Fatal("no binding write was held")
	}
	var applied bool
	var err error
	if op == "put" {
		applied, err = k.AtomicKVStore.PutIfAbsent(context.Background(), key, val)
	} else {
		applied, err = k.AtomicKVStore.CompareAndSwap(context.Background(), key, old, val)
	}
	if err != nil {
		t.Fatalf("late commit: %v", err)
	}
	return applied
}

func installDelayedBindWrite(t *testing.T, rig *sdlcServiceRig, hook func()) *delayedBindWriteKV {
	t.Helper()
	// Resolve once so the tenant store exists to be wrapped.
	if _, err := rig.store.ProjectDeleteFenced(context.Background(), sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	rig.resolver.mu.Lock()
	defer rig.resolver.mu.Unlock()
	inner, ok := rig.resolver.stores[sdlcTestTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the rig's tenant store takes no guarded writes")
	}
	delayed := &delayedBindWriteKV{AtomicKVStore: inner, hook: hook}
	rig.resolver.stores[sdlcTestTenant] = delayed
	return delayed
}

// assertPendingWriteError checks a bind returned its write's own error, not a
// withdrawal failure.
func assertPendingWriteError(t *testing.T, err error) {
	t.Helper()
	var withdrawErr *sdlc.BindWithdrawError
	if err == nil || errors.As(err, &withdrawErr) || !strings.Contains(err.Error(), "proposal outcome unknown") {
		t.Fatalf("Bind = %v, want the write's own error", err)
	}
}

func assertNoBindingOrRoute(t *testing.T, rig *sdlcServiceRig, projectID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, projectID); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the settled bind: %v, want not found", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, projectID); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after the settled bind: %v, want not found", err)
	}
}

// The binding write's outcome is unknown and is still pending when the bind
// returns, and a delete fences meanwhile. The bind settles the write, so
// committing it afterwards changes nothing and no binding or route is left.
func TestSDLCService_BindWithPendingWriteBehindADeleteNeverLands(t *testing.T) {
	rig := newSDLCServiceRig(t)
	delayed := installDelayedBindWrite(t, rig, fenceRigProject(t, rig))

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding(), BindOptions{})
	if delayed.commit(t) {
		t.Fatal("the pending binding write applied after the bind returned")
	}
	assertNoBindingOrRoute(t, rig, sdlcTestProject)
	assertPendingWriteError(t, err)
}

// With no delete in flight, a bind whose write is still pending leaves no
// binding and no route, even once the write commits late.
func TestSDLCService_BindWithPendingWriteAndNoDeleteNeverLands(t *testing.T) {
	rig := newSDLCServiceRig(t)
	delayed := installDelayedBindWrite(t, rig, nil)

	_, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding(), BindOptions{})
	assertPendingWriteError(t, err)
	if delayed.commit(t) {
		t.Fatal("the pending binding write applied after the bind returned")
	}
	assertNoBindingOrRoute(t, rig, sdlcTestProject)
}

// A rebind whose write is still pending behind a delete keeps the prior
// binding and its route, and the late commit does not apply.
func TestSDLCService_RebindWithPendingWriteKeepsThePrior(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	original, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	delayed := installDelayedBindWrite(t, rig, fenceRigProject(t, rig))

	rebind := rig.binding()
	rebind.MaxReviewRounds = original.MaxReviewRounds + 2
	_, bindErr := rig.svc.Bind(ctx, sdlcTestTenant, rebind, BindOptions{})
	if delayed.commit(t) {
		t.Fatal("the pending rebind applied after the bind returned")
	}
	got, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("binding after the settled rebind: %v", err)
	}
	if got.MaxReviewRounds != original.MaxReviewRounds {
		t.Fatalf("MaxReviewRounds = %d, want the prior's %d", got.MaxReviewRounds, original.MaxReviewRounds)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); err != nil {
		t.Fatalf("the settled rebind dropped the prior binding's route: %v", err)
	}
	assertPendingWriteError(t, bindErr)
}

// rejectBindSwapKV rejects the first binding compare-and-swap as a definite
// conflict and then cancels the bind's context.
type rejectBindSwapKV struct {
	qrdbsdk.AtomicKVStore
	cancel context.CancelFunc

	mu       sync.Mutex
	rejected bool
}

func (k *rejectBindSwapKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	k.mu.Lock()
	reject := !k.rejected && strings.HasPrefix(key, "sdlc_binding/")
	k.rejected = k.rejected || reject
	k.mu.Unlock()
	if !reject {
		return k.AtomicKVStore.CompareAndSwap(ctx, key, old, value)
	}
	k.cancel()
	return false, nil
}

// A rebind whose swap was rejected wrote nothing, so the binding it would
// have replaced and that binding's route both stand.
func TestSDLCService_RebindWithRejectedSwapKeepsBindingAndRoute(t *testing.T) {
	rig := newSDLCServiceRig(t)
	if _, err := rig.svc.Bind(context.Background(), sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	original, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rig.resolver.mu.Lock()
	inner, ok := rig.resolver.stores[sdlcTestTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		rig.resolver.mu.Unlock()
		t.Fatal("the rig's tenant store takes no guarded writes")
	}
	rig.resolver.stores[sdlcTestTenant] = &rejectBindSwapKV{AtomicKVStore: inner, cancel: cancel}
	rig.resolver.mu.Unlock()

	rebind := rig.binding()
	rebind.MaxReviewRounds = original.MaxReviewRounds + 2
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rebind, BindOptions{}); err == nil {
		t.Fatal("a rebind whose swap was rejected and whose context ended succeeded, want an error")
	}
	got, err := rig.store.GetBinding(context.Background(), sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("binding after the rejected rebind: %v", err)
	}
	if got.WriteToken != original.WriteToken || got.MaxReviewRounds != original.MaxReviewRounds {
		t.Fatalf("binding after the rejected rebind = %+v, want the original", got)
	}
	if _, err := rig.svc.Routes().Tenant(context.Background(), sdlcTestProject); err != nil {
		t.Fatalf("the rejected rebind dropped the binding's route: %v", err)
	}
}

// A bind whose write is still pending returns; the project is then deleted,
// and the write commits late. It does not apply, so no binding stands on the
// deleted project.
func TestDeleteProject_AfterASettledPendingBindLeavesNoBinding(t *testing.T) {
	rig := newBindDeleteRig(t)
	delayed := installDelayedBindWrite(t, rig.sdlc, nil)

	bindErr := rig.bind(context.Background())
	if err := rig.delete(); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if delayed.commit(t) {
		t.Fatal("the pending binding write applied after the delete")
	}
	rig.assertUnbound(t)
	assertPendingWriteError(t, bindErr)
}
