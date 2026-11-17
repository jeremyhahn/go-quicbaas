package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// hookAfterBindWriteKV runs hook once, right after the first binding write
// lands: inside a Bind, after PutBinding and before the fence read.
type hookAfterBindWriteKV struct {
	qrdbsdk.AtomicKVStore
	hook func()
}

func (k *hookAfterBindWriteKV) wrote(key string, applied bool) {
	if applied && strings.HasPrefix(key, "sdlc_binding/") && k.hook != nil {
		hook := k.hook
		k.hook = nil
		hook()
	}
}

func (k *hookAfterBindWriteKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.PutIfAbsent(ctx, key, value)
	k.wrote(key, applied)
	return applied, err
}

func (k *hookAfterBindWriteKV) CompareAndSwap(ctx context.Context, key string, old, value []byte) (bool, error) {
	applied, err := k.AtomicKVStore.CompareAndSwap(ctx, key, old, value)
	k.wrote(key, applied)
	return applied, err
}

// hookAfterBindWrite wraps the rig's tenant store so hook runs once, right
// after the next binding write.
func hookAfterBindWrite(t *testing.T, rig *sdlcServiceRig, hook func()) {
	t.Helper()
	// Resolve once so the tenant store exists to be wrapped.
	if _, err := rig.store.ProjectDeleteFenced(context.Background(), sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	rig.resolver.mu.Lock()
	defer rig.resolver.mu.Unlock()
	held, ok := rig.resolver.stores[sdlcTestTenant].(qrdbsdk.AtomicKVStore)
	if !ok {
		t.Fatal("the rig's tenant store takes no guarded writes")
	}
	rig.resolver.stores[sdlcTestTenant] = &hookAfterBindWriteKV{AtomicKVStore: held, hook: hook}
}

// TestSDLCService_SecondBindRefusedWhileFirstHoldsTheLease: bind A has put
// its binding and not yet read the fence when bind B of the same project
// arrives. B is refused with BindInProgressError and writes nothing; a delete
// then fences the project, A withdraws, and neither a binding nor a route is
// left behind.
func TestSDLCService_SecondBindRefusedWhileFirstHoldsTheLease(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	var bindBErr error
	hookAfterBindWrite(t, rig, func() {
		mine, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
		if err != nil {
			t.Errorf("bind A's binding: %v", err)
			return
		}
		b := rig.binding()
		b.MaxReviewRounds = mine.MaxReviewRounds + 3
		_, bindBErr = rig.svc.Bind(ctx, sdlcTestTenant, b, BindOptions{})
		after, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
		if err != nil || after.WriteToken != mine.WriteToken || after.MaxReviewRounds != mine.MaxReviewRounds {
			t.Errorf("binding after the refused bind B = %+v, %v; want bind A's write untouched", after, err)
		}
		if _, err := rig.store.FenceProjectDelete(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
			t.Errorf("FenceProjectDelete: %v", err)
		}
	})

	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	assertProjectDeleting(t, err)
	var inProgress *sdlc.BindInProgressError
	if !errors.As(bindBErr, &inProgress) {
		t.Fatalf("bind B = %v, want *sdlc.BindInProgressError", bindBErr)
	}
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after A withdrew: %v, want not found", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("route after A withdrew: %v, want not found", err)
	}
	// A released its lease on the way out.
	token, err := rig.store.AcquireBindLease(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatalf("bind lease after A returned: %v", err)
	}
	if err := rig.store.ReleaseBindLease(ctx, sdlcTestTenant, sdlcTestProject, token); err != nil {
		t.Fatal(err)
	}
}

// TestSDLCService_UnbindRefusedWhileABindHoldsTheLease: an unbind takes the
// same lease, so it cannot remove a binding or route under a bind in flight.
func TestSDLCService_UnbindRefusedWhileABindHoldsTheLease(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	token, err := rig.store.AcquireBindLease(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	var inProgress *sdlc.BindInProgressError
	if err := rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject); !errors.As(err, &inProgress) {
		t.Fatalf("Unbind under a held lease = %v, want *sdlc.BindInProgressError", err)
	}
	if err := rig.store.ReleaseBindLease(ctx, sdlcTestTenant, sdlcTestProject, token); err != nil {
		t.Fatal(err)
	}
}
