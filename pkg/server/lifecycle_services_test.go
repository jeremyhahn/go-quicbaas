package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
)

// The node's recovery loop settles a claim abandoned mid-verb (its
// settlement write lost, as in a crash) halted once it is stale, so its
// owner can resume it; until the plane can be built it retries quietly.
func TestServicePlaneRecovery_SettlesAnAbandonedClaim(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	srv := &Server{logger: log}
	var mu sync.Mutex
	clock := time.Unix(1_800_000_000, 0).UTC()
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	rt := serviceplanetest.NewRuntime(t)
	rt.Now = now
	plane := rt.Plane(t)
	ctx := context.Background()
	if _, err := plane.Deploy(ctx, "acme", rt.Sign(t, "acme", "web")); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	// The restart's settlement write is lost: web stays transitioning.
	rt.LoseWritesAfter(1) // the claim lands, the settlement does not
	_, err = plane.Restart(ctx, "acme", "web")
	rt.LoseWritesAfter(-1)
	if err == nil {
		t.Fatal("restart: want the lost settlement")
	}
	if _, err := plane.Resume(ctx, "acme", "web"); !errors.Is(err, serviceplane.ErrTransition) {
		t.Fatalf("resume a transitioning service = %v, want ErrTransition", err)
	}

	var ready atomic.Bool
	resolve := func(context.Context) (*serviceplane.Plane, error) {
		if !ready.Load() {
			return nil, errServicePlaneStore
		}
		return plane, nil
	}
	tick := make(chan time.Time)
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.runServicePlaneRecovery(runCtx, resolve, tick)
	}()
	tick <- time.Time{} // the plane cannot be built yet

	ready.Store(true)
	tick <- time.Time{} // a fresh claim is left alone
	if svc, err := plane.Get(ctx, "acme", "web"); err != nil || svc.State != serviceplane.StateTransitioning {
		t.Fatalf("a fresh claim = %+v, %v; want it still transitioning", svc, err)
	}
	mu.Lock()
	clock = clock.Add(serviceplane.DefaultStaleClaim + time.Second)
	mu.Unlock()
	tick <- time.Time{}
	tick <- time.Time{} // the previous tick's recovery has finished
	stop()
	<-done

	svc, err := plane.Get(ctx, "acme", "web")
	if err != nil || svc.State != serviceplane.StateHalted {
		t.Fatalf("after recovery = %+v, %v; want halted", svc, err)
	}
	if svc, err := plane.Resume(ctx, "acme", "web"); err != nil || svc.State != serviceplane.StateRunning {
		t.Fatalf("resume after recovery = %+v, %v", svc, err)
	}
}
