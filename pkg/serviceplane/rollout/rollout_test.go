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

package rollout

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
)

// memGuard arbitrates writes to an in-memory store, as cas.Writer does to
// the platform shard.
type memGuard struct {
	mu sync.Mutex
	kv *kvstore.Store
}

func (g *memGuard) Create(ctx context.Context, key string, value []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok, err := g.kv.Exists(ctx, key); err != nil || ok {
		return false, err
	}
	return true, g.kv.Put(ctx, key, value)
}

func (g *memGuard) Swap(ctx context.Context, key string, held, next []byte) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.kv.CompareAndSwap(ctx, key, held, next)
}

// slotRig records every slot, appraisal and name call. failAppraise fails
// appraisal of a version; failDrain fails every drain; failSwitch fails a
// switch to that slot; block, when set, holds every fill until it is
// closed, after signalling filling. Fills, drains and switches pass its
// EpochGate, as a runtime and a registry do; routed is the slot the name
// points at.
type slotRig struct {
	mu           sync.Mutex
	gate         EpochGate
	calls        []string
	routed       Slot
	failAppraise string
	failDrain    bool
	failFill     bool
	failSwitch   string
	filling      chan struct{}
	block        chan struct{}
}

func (r *slotRig) log(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *slotRig) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *slotRig) Routed() Slot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.routed
}

func (r *slotRig) Fill(_ context.Context, f Fence, slot Slot, rel *serviceplane.Release) error {
	if r.block != nil {
		r.filling <- struct{}{}
		<-r.block
	}
	return r.gate.Do(f, func() error {
		r.log("fill " + string(slot) + " " + rel.Version)
		if r.failFill {
			return errors.New("slot fill failed")
		}
		return nil
	})
}

func (r *slotRig) Drain(_ context.Context, f Fence, slot Slot) error {
	return r.gate.Do(f, func() error {
		r.log("drain " + string(slot))
		if r.failDrain {
			return errors.New("slot will not stop")
		}
		return nil
	})
}

func (r *slotRig) Switch(_ context.Context, f Fence, slot Slot) error {
	return r.gate.Do(f, func() error {
		r.log("switch " + string(slot))
		r.mu.Lock()
		r.routed = slot
		r.mu.Unlock()
		if string(slot) == r.failSwitch {
			return errors.New("the switch took effect but did not answer")
		}
		return nil
	})
}

// delayedSwitch holds a controller's switch on its way to the shared
// registry until release is closed, after signalling switching: the
// superseded leader's delayed side effect.
type delayedSwitch struct {
	*slotRig
	switching chan struct{}
	release   chan struct{}
}

func (d delayedSwitch) Switch(ctx context.Context, f Fence, slot Slot) error {
	d.switching <- struct{}{}
	<-d.release
	return d.slotRig.Switch(ctx, f, slot)
}

func (r *slotRig) Appraise(_ context.Context, _, _ string, slot Slot, rel *serviceplane.Release) error {
	r.log("appraise " + string(slot) + " " + rel.Version)
	if rel.Version == r.failAppraise {
		return errors.New("measurement does not match the reference value")
	}
	return nil
}

type leader bool

func (l leader) IsLeader() (bool, error) { return bool(l), nil }

type world struct {
	rt    *serviceplanetest.Runtime
	guard *memGuard
	kv    *kvstore.Store
}

func newWorld(t *testing.T) *world {
	kv := kvstore.NewMemory()
	return &world{rt: serviceplanetest.NewRuntime(t), guard: &memGuard{kv: kv}, kv: kv}
}

func (w *world) controller(id string, rig *slotRig, isLeader bool) *Controller {
	return &Controller{
		ID: id, Guard: w.guard, KV: w.kv, Leader: leader(isLeader), Verifier: w.rt.Verifier,
		Strategy: ABSwitch{Slots: rig, Appraiser: rig, Names: rig},
	}
}

func (w *world) release(t *testing.T, tenant, version string, kind serviceplane.ReleaseKind) *serviceplane.SignedRelease {
	t.Helper()
	sr, err := serviceplane.SignRelease(&serviceplane.Release{
		Kind:         kind,
		Version:      version,
		Manifest:     *serviceplanetest.Manifest(tenant, "web"),
		PolicyDigest: "sha256:" + strings.Repeat("cd", 32),
		Provenance:   "slsa://registry/web/" + version,
	}, serviceplanetest.KeyID, w.rt.Key)
	if err != nil {
		t.Fatal(err)
	}
	return sr
}

func wantCalls(t *testing.T, rig *slotRig, want ...string) {
	t.Helper()
	if got := rig.Calls(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", got, want)
	}
}

func TestABSwitch_FillsTheInactiveSlotAndSwitchesTheName(t *testing.T) {
	w, rig := newWorld(t), &slotRig{}
	c := w.controller("node-1", rig, true)
	ctx := context.Background()

	slots, err := c.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService))
	if err != nil {
		t.Fatal(err)
	}
	if slots.Active != SlotA || slots.Current().Version != "1" || slots.Rollout.Phase != PhaseDone {
		t.Fatalf("first release: %+v", slots)
	}
	if _, err := c.Roll(ctx, "acme", w.release(t, "acme", "2", serviceplane.ReleaseService)); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, rig, "fill a 1", "appraise a 1", "switch a", "fill b 2", "appraise b 2", "switch b")

	stored, err := c.Slots(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Active != SlotB || stored.Current().Version != "2" || stored.Releases[SlotA].Version != "1" {
		t.Fatalf("the record does not show the switch: %+v", stored)
	}
	if stored.Rollout.From != SlotA || stored.Rollout.To != SlotB || stored.Rollout.Owner != "node-1" {
		t.Fatalf("rollout = %+v", stored.Rollout)
	}
}

func TestABSwitch_AnAppraisalFailureRollsBack(t *testing.T) {
	w, rig := newWorld(t), &slotRig{failAppraise: "2"}
	c := w.controller("node-1", rig, true)
	ctx := context.Background()
	if _, err := c.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService)); err != nil {
		t.Fatal(err)
	}

	_, err := c.Roll(ctx, "acme", w.release(t, "acme", "2", serviceplane.ReleaseService))
	var step *StepError
	if !errors.Is(err, ErrRolledBack) || !errors.As(err, &step) || step.Phase != PhaseAppraise {
		t.Fatalf("roll = %v", err)
	}
	wantCalls(t, rig, "fill a 1", "appraise a 1", "switch a", "fill b 2", "appraise b 2", "drain b")
	stored, err := c.Slots(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Active != SlotA || stored.Current().Version != "1" || stored.Rollout.Phase != PhaseRolledBack ||
		!strings.Contains(stored.Rollout.Reason, "reference value") {
		t.Fatalf("after rollback: %+v %+v", stored, stored.Rollout)
	}

	// The rolled-back rollout holds nothing: the next release proceeds.
	rig.failAppraise = ""
	if slots, err := c.Roll(ctx, "acme", w.release(t, "acme", "3", serviceplane.ReleaseService)); err != nil || slots.Active != SlotB {
		t.Fatalf("roll after rollback = %+v, %v", slots, err)
	}
}

func TestABSwitch_AFailedRollbackIsRecorded(t *testing.T) {
	w, rig := newWorld(t), &slotRig{failAppraise: "1", failDrain: true}
	c := w.controller("node-1", rig, true)
	_, err := c.Roll(context.Background(), "acme", w.release(t, "acme", "1", serviceplane.ReleaseService))
	if !errors.Is(err, ErrRollback) {
		t.Fatalf("roll = %v", err)
	}
	stored, _ := c.Slots(context.Background(), "acme", "web")
	if stored.Rollout.Phase != PhaseFailed || stored.Active != "" {
		t.Fatalf("after failed rollback: %+v", stored.Rollout)
	}
}

func TestABSwitch_AFailedSwitchPointsTheNameBack(t *testing.T) {
	w, rig := newWorld(t), &slotRig{}
	c := w.controller("node-1", rig, true)
	ctx := context.Background()
	if _, err := c.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService)); err != nil {
		t.Fatal(err)
	}
	rig.failSwitch = "b"
	_, err := c.Roll(ctx, "acme", w.release(t, "acme", "2", serviceplane.ReleaseService))
	var step *StepError
	if !errors.Is(err, ErrRolledBack) || !errors.As(err, &step) || step.Phase != PhaseSwitch {
		t.Fatalf("roll = %v", err)
	}
	wantCalls(t, rig, "fill a 1", "appraise a 1", "switch a", "fill b 2", "appraise b 2", "switch b", "switch a", "drain b")
	stored, err := c.Slots(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Active != SlotA || stored.Rollout.Phase != PhaseRolledBack {
		t.Fatalf("after a failed switch: %+v %+v", stored, stored.Rollout)
	}
}

func TestController_ASecondControllerNeverRuns(t *testing.T) {
	w := newWorld(t)
	first := &slotRig{filling: make(chan struct{}, 1), block: make(chan struct{})}
	second := &slotRig{}
	c1, c2 := w.controller("node-1", first, true), w.controller("node-2", second, true)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := c1.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService))
		done <- err
	}()
	<-first.filling

	// The first controller holds the service: the second refuses before it
	// plans, fills or switches anything.
	if _, err := c2.Roll(ctx, "acme", w.release(t, "acme", "2", serviceplane.ReleaseService)); !errors.Is(err, ErrBusy) {
		t.Fatalf("second controller = %v", err)
	}
	wantCalls(t, second)

	close(first.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	wantCalls(t, first, "fill a 1", "appraise a 1", "switch a")
}

func TestController_ATakenOverRolloutStopsTheFirstController(t *testing.T) {
	w := newWorld(t)
	first := &slotRig{filling: make(chan struct{}, 1), block: make(chan struct{})}
	second := &slotRig{}
	c1, c2 := w.controller("node-1", first, true), w.controller("node-2", second, true)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := c1.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService))
		done <- err
	}()
	<-first.filling

	// A new leader resumes the rollout; its claim is written first, so the
	// old leader's next write loses and it stops before appraising.
	slots, err := c2.Resume(ctx, "acme", "web")
	if err != nil || slots.Active != SlotA || slots.Rollout.Owner != "node-2" {
		t.Fatalf("resume = %+v, %v", slots, err)
	}
	close(first.block)
	if err := <-done; !errors.Is(err, ErrBusy) {
		t.Fatalf("the old controller went on: %v", err)
	}
	wantCalls(t, first, "fill a 1")
	wantCalls(t, second, "fill a 1", "appraise a 1", "switch a")

	// Resuming a finished rollout does nothing.
	if _, err := c2.Resume(ctx, "acme", "web"); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, second, "fill a 1", "appraise a 1", "switch a")
}

// TestController_ASupersededStepThatFailsNeverRollsBack has the old leader's
// blocked step fail only after the new leader finished the rollout: the old
// controller must not roll back (drain the slot, point the name back) what
// the new leader made active.
func TestController_ASupersededStepThatFailsNeverRollsBack(t *testing.T) {
	w := newWorld(t)
	first := &slotRig{filling: make(chan struct{}, 1), block: make(chan struct{}), failFill: true}
	second := &slotRig{}
	c1, c2 := w.controller("node-1", first, true), w.controller("node-2", second, true)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := c1.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService))
		done <- err
	}()
	<-first.filling

	slots, err := c2.Resume(ctx, "acme", "web")
	if err != nil || slots.Active != SlotA || slots.Rollout.Phase != PhaseDone {
		t.Fatalf("resume = %+v, %v", slots, err)
	}
	close(first.block)
	if err := <-done; !errors.Is(err, ErrBusy) || errors.Is(err, ErrRolledBack) {
		t.Fatalf("the superseded controller = %v, want busy and no rollback", err)
	}
	wantCalls(t, first, "fill a 1")
	wantCalls(t, second, "fill a 1", "appraise a 1", "switch a")

	after, err := c2.Resume(ctx, "acme", "web")
	if err != nil || after.Active != SlotA || after.Rollout.Phase != PhaseDone || after.Rollout.Owner != "node-2" {
		t.Fatalf("after the superseded failure = %+v, %v", after, err)
	}
}

// TestController_ADelayedSwitchOfASupersededLeaderNeverRoutes delays the
// old leader's switch to A at the registry's door while a new leader takes
// the rollout over, finishes it and rolls the service on to B: the stale
// switch is fenced, so the name stays on B, where the record says it is.
func TestController_ADelayedSwitchOfASupersededLeaderNeverRoutes(t *testing.T) {
	w := newWorld(t)
	shared := &slotRig{}
	late := delayedSwitch{slotRig: shared, switching: make(chan struct{}, 1), release: make(chan struct{})}
	c1 := &Controller{
		ID: "node-1", Guard: w.guard, KV: w.kv, Leader: leader(true), Verifier: w.rt.Verifier,
		Strategy: ABSwitch{Slots: shared, Appraiser: shared, Names: late},
	}
	c2 := w.controller("node-2", shared, true)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := c1.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService))
		done <- err
	}()
	<-late.switching

	if slots, err := c2.Resume(ctx, "acme", "web"); err != nil || slots.Active != SlotA || slots.Rollout.Phase != PhaseDone {
		t.Fatalf("takeover = %+v, %v", slots, err)
	}
	slots, err := c2.Roll(ctx, "acme", w.release(t, "acme", "2", serviceplane.ReleaseService))
	if err != nil || slots.Active != SlotB {
		t.Fatalf("roll to b = %+v, %v", slots, err)
	}

	close(late.release)
	err = <-done
	if !errors.Is(err, ErrFenced) || !errors.Is(err, ErrBusy) || errors.Is(err, ErrRolledBack) {
		t.Fatalf("the superseded leader = %v, want its switch fenced and no rollback", err)
	}
	if got := shared.Routed(); got != SlotB {
		t.Fatalf("the name routes to %q, want b", got)
	}
	stored, err := c2.Slots(ctx, "acme", "web")
	if err != nil || stored.Active != SlotB {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	wantCalls(t, shared, "fill a 1", "appraise a 1", "switch a", "fill b 2", "appraise b 2", "switch b")
}

func TestEpochGate_RefusesAnOlderClaim(t *testing.T) {
	var g EpochGate
	ran := 0
	apply := func() error { ran++; return nil }
	web := func(epoch uint64) Fence { return Fence{Tenant: "acme", Service: "web", Epoch: epoch} }
	for _, epoch := range []uint64{1, 3, 3} {
		if err := g.Do(web(epoch), apply); err != nil {
			t.Fatalf("epoch %d: %v", epoch, err)
		}
	}
	if err := g.Do(web(2), apply); !errors.Is(err, ErrFenced) {
		t.Fatalf("an older epoch = %v, want ErrFenced", err)
	}
	// Each service has its own newest claim.
	if err := g.Do(Fence{Tenant: "acme", Service: "api", Epoch: 1}, apply); err != nil {
		t.Fatalf("another service: %v", err)
	}
	if ran != 4 {
		t.Fatalf("applied %d times, want 4", ran)
	}
}

// stepDown answers leader to the first check only, so a Roll claims its
// rollout and then strands it before the first phase.
type stepDown struct{ checks atomic.Int32 }

func (s *stepDown) IsLeader() (bool, error) { return s.checks.Add(1) == 1, nil }

func TestController_ConcurrentResumesRunEachPhaseOnce(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.controller("node-0", &slotRig{}, true).Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService)); err != nil {
		t.Fatal(err)
	}
	stranded := w.controller("node-0", &slotRig{}, true)
	stranded.Leader = &stepDown{}
	if _, err := stranded.Roll(ctx, "acme", w.release(t, "acme", "2", serviceplane.ReleaseService)); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("stranded roll = %v", err)
	}

	first := &slotRig{filling: make(chan struct{}, 1), block: make(chan struct{})}
	second := &slotRig{}
	c1 := w.controller("node-1", first, true)
	c2 := w.controller("node-1", second, true)
	done := make(chan error, 1)
	go func() {
		_, err := c1.Resume(ctx, "acme", "web")
		done <- err
	}()
	<-first.filling

	// The same controller is fenced in process: it runs no phase twice.
	if _, err := c1.Resume(ctx, "acme", "web"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second resume on the same controller = %v", err)
	}
	// Another controller with the same id takes over under a new epoch,
	// so the first one's next write loses even though the owner matches.
	slots, err := c2.Resume(ctx, "acme", "web")
	if err != nil || slots.Active != SlotB || slots.Rollout.Phase != PhaseDone {
		t.Fatalf("takeover = %+v, %v", slots, err)
	}
	close(first.block)
	if err := <-done; !errors.Is(err, ErrBusy) {
		t.Fatalf("the superseded resume went on: %v", err)
	}
	wantCalls(t, first, "fill b 2")
	wantCalls(t, second, "fill b 2", "appraise b 2", "switch b")
}

func TestController_ResumeDuringRollIsBusy(t *testing.T) {
	w := newWorld(t)
	rig := &slotRig{filling: make(chan struct{}, 1), block: make(chan struct{})}
	c := w.controller("node-1", rig, true)
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		_, err := c.Roll(ctx, "acme", w.release(t, "acme", "1", serviceplane.ReleaseService))
		done <- err
	}()
	<-rig.filling
	if _, err := c.Resume(ctx, "acme", "web"); !errors.Is(err, ErrBusy) {
		t.Fatalf("resume during roll = %v", err)
	}
	close(rig.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	wantCalls(t, rig, "fill a 1", "appraise a 1", "switch a")
	slots, err := c.Slots(ctx, "acme", "web")
	if err != nil || slots.Rollout.Epoch != 1 {
		t.Fatalf("slots = %+v, %v", slots, err)
	}
}

func TestController_RefusesWhenNotLeader(t *testing.T) {
	w, rig := newWorld(t), &slotRig{}
	c := w.controller("node-2", rig, false)
	if _, err := c.Roll(context.Background(), "acme", w.release(t, "acme", "1", serviceplane.ReleaseService)); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("roll = %v", err)
	}
	if _, err := c.Resume(context.Background(), "acme", "web"); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("resume = %v", err)
	}
	wantCalls(t, rig)
}

func TestController_RefusesReleasesItMustNotRoll(t *testing.T) {
	w, rig := newWorld(t), &slotRig{}
	c := w.controller("node-1", rig, true)
	ctx := context.Background()

	forged := w.release(t, "acme", "1", serviceplane.ReleaseService)
	forged.Signature[0] ^= 0xff
	for name, sr := range map[string]*serviceplane.SignedRelease{
		"forged":         forged,
		"another tenant": w.release(t, "globex", "1", serviceplane.ReleaseService),
		"a node release": w.release(t, "acme", "1", serviceplane.ReleaseNode),
	} {
		if _, err := c.Roll(ctx, "acme", sr); !errors.Is(err, ErrRelease) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	wantCalls(t, rig)

	if _, err := (&Controller{}).Roll(ctx, "acme", forged); !errors.Is(err, ErrConfig) {
		t.Fatalf("empty controller = %v", err)
	}
	if _, err := (ABSwitch{}).Plan(&Slots{}, &serviceplane.Release{}); !errors.Is(err, ErrConfig) {
		t.Fatalf("empty strategy = %v", err)
	}
}
