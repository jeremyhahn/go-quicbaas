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
// appraisal of a version; failDrain fails every drain; block, when set,
// holds every fill until it is closed, after signalling filling.
type slotRig struct {
	mu           sync.Mutex
	calls        []string
	failAppraise string
	failDrain    bool
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

func (r *slotRig) Fill(_ context.Context, _, _ string, slot Slot, rel *serviceplane.Release) error {
	r.log("fill " + string(slot) + " " + rel.Version)
	if r.block != nil {
		r.filling <- struct{}{}
		<-r.block
	}
	return nil
}

func (r *slotRig) Drain(_ context.Context, _, _ string, slot Slot) error {
	r.log("drain " + string(slot))
	if r.failDrain {
		return errors.New("slot will not stop")
	}
	return nil
}

func (r *slotRig) Appraise(_ context.Context, _, _ string, slot Slot, rel *serviceplane.Release) error {
	r.log("appraise " + string(slot) + " " + rel.Version)
	if rel.Version == r.failAppraise {
		return errors.New("measurement does not match the reference value")
	}
	return nil
}

func (r *slotRig) Switch(_ context.Context, _, _ string, slot Slot) error {
	r.log("switch " + string(slot))
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
