package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

// TestSDLCService_RenewedLeaseRefusesUnbindWhileBindOnboards: the bind
// renews its lease while it works, so an unbind past the original
// BindLeaseTTL is still refused, and the bind onboards and keeps its
// binding, route and webhook.
func TestSDLCService_RenewedLeaseRefusesUnbindWhileBindOnboards(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	var clock steppedClock
	clock.set(renewBase)
	rig.store.SetClock(clock.now)
	ticks := make(chan time.Time)
	rig.svc.bindLeaseTicks = ticks
	var unbindErr error
	hookAfterBindWrite(t, rig, func() {
		clock.set(renewBase.Add(sdlc.BindLeaseTTL - 10*time.Second))
		sendTick(t, ticks)
		sendTick(t, ticks)
		clock.set(renewBase.Add(sdlc.BindLeaseTTL + 30*time.Second))
		unbindErr = rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject)
	})

	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	var inProgress *sdlc.BindInProgressError
	if !errors.As(unbindErr, &inProgress) {
		t.Fatalf("Unbind under a renewed lease = %v, want *sdlc.BindInProgressError", unbindErr)
	}
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatalf("binding after the bind: %v", err)
	}
	if _, err := rig.svc.Routes().Tenant(ctx, sdlcTestProject); err != nil {
		t.Fatalf("route after the bind: %v", err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 1 || hooks[0] != expectedWebhookURL {
		t.Fatalf("hooks = %v, want [%s]", hooks, expectedWebhookURL)
	}
}

// TestSDLCService_BindThatLostItsLeaseStopsBeforeOnboard: the bind stalled
// past BindLeaseTTL without renewing, so an unbind took the lease and removed
// the binding. The bind finds its lease lost and stops before the forge is
// told to deliver.
func TestSDLCService_BindThatLostItsLeaseStopsBeforeOnboard(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	var clock steppedClock
	clock.set(renewBase)
	rig.store.SetClock(clock.now)
	ticks := make(chan time.Time)
	rig.svc.bindLeaseTicks = ticks
	var unbindErr error
	hookAfterBindWrite(t, rig, func() {
		clock.set(renewBase.Add(sdlc.BindLeaseTTL + time.Second))
		unbindErr = rig.svc.Unbind(ctx, sdlcTestTenant, sdlcTestProject)
	})

	_, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{})
	if unbindErr != nil {
		t.Fatalf("Unbind past the stale lease: %v, want nil", unbindErr)
	}
	var conflict *sdlc.VersionConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, sdlc.ErrVersionConflict) {
		t.Fatalf("Bind after losing its lease = %v, want *sdlc.VersionConflictError", err)
	}
	if created := rig.forge.Calls("POST /hooks"); created != 0 {
		t.Fatalf("hooks created = %d, want 0", created)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
		t.Fatalf("hooks = %v, want none", hooks)
	}
	if _, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject); !errors.Is(err, sdlc.ErrNotFound) {
		t.Fatalf("binding after the unbind: %v, want not found", err)
	}
}

// TestSDLCService_RebindToNewCoordinatesBehindAFenceKeepsItsDelivery: a
// rebind that moves the forge coordinates while a delete holds the fence is
// refused before retirePriorDelivery runs, so repository A keeps its webhook
// and B is never provisioned.
func TestSDLCService_RebindToNewCoordinatesBehindAFenceKeepsItsDelivery(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind A: %v", err)
	}
	original, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rig.store.FenceProjectDelete(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
		t.Fatal(err)
	}
	other, rebound := rebindTarget(t, rig)

	_, err = rig.svc.Bind(ctx, sdlcTestTenant, rebound, BindOptions{})
	assertProjectDeleting(t, err)
	// An offboard and re-onboard would show as a second webhook creation.
	if created := rig.forge.Calls("POST /hooks"); created != 1 {
		t.Fatalf("repo A webhook creations = %d, want 1: the refused rebind touched its delivery", created)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 1 {
		t.Fatalf("repo A hooks = %v, want its one webhook kept", hooks)
	}
	if hooks := other.HookURLs(); len(hooks) != 0 {
		t.Fatalf("repo B hooks = %v, want none", hooks)
	}
	got, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil || got.WriteToken != original.WriteToken {
		t.Fatalf("binding after the refused rebind = %+v, %v; want the original untouched", got, err)
	}
}

// TestSDLCService_FenceAfterThePreCheckRestoresAndReonboardsThePrior: the
// fence lands after the pre-check, once the rebind has retired repository
// A's delivery and put its binding. The withdrawal restores A's binding and
// onboards it again, so A delivers once more.
func TestSDLCService_FenceAfterThePreCheckRestoresAndReonboardsThePrior(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()
	if _, err := rig.svc.Bind(ctx, sdlcTestTenant, rig.binding(), BindOptions{}); err != nil {
		t.Fatalf("Bind A: %v", err)
	}
	original, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil {
		t.Fatal(err)
	}
	other, rebound := rebindTarget(t, rig)
	hookAfterBindWrite(t, rig, func() {
		if hooks := rig.forge.HookURLs(); len(hooks) != 0 {
			t.Errorf("repo A hooks before the fence = %v, want the rebind to have retired them", hooks)
		}
		if _, err := rig.store.FenceProjectDelete(ctx, sdlcTestTenant, sdlcTestProject); err != nil {
			t.Errorf("FenceProjectDelete: %v", err)
		}
	})

	_, err = rig.svc.Bind(ctx, sdlcTestTenant, rebound, BindOptions{})
	assertProjectDeleting(t, err)
	got, err := rig.store.GetBinding(ctx, sdlcTestTenant, sdlcTestProject)
	if err != nil || got.WriteToken != original.WriteToken || got.Forge.Repo != original.Forge.Repo {
		t.Fatalf("binding after the withdrawn rebind = %+v, %v; want A's restored", got, err)
	}
	if hooks := rig.forge.HookURLs(); len(hooks) != 1 || hooks[0] != expectedWebhookURL {
		t.Fatalf("repo A hooks = %v, want [%s] onboarded again", hooks, expectedWebhookURL)
	}
	if hooks := other.HookURLs(); len(hooks) != 0 {
		t.Fatalf("repo B hooks = %v, want none", hooks)
	}
}
