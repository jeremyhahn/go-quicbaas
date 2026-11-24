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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
)

// appraisal is an Appraiser that answers err.
type appraisal struct{ err error }

func (a appraisal) Appraise(context.Context, string, string, Slot, *serviceplane.Release) error {
	return a.err
}

// planeRig is a real plane with acme/web deployed, and the one controller
// that rolls its releases through the plane's slot verbs.
type planeRig struct {
	w     *world
	plane *serviceplane.Plane
	c     *Controller
}

func newPlaneRig(t *testing.T, appraiser Appraiser) *planeRig {
	t.Helper()
	w := newWorld(t)
	plane := w.rt.Plane(t)
	if _, err := plane.Deploy(context.Background(), "acme", w.rt.Sign(t, "acme", "web")); err != nil {
		t.Fatal(err)
	}
	c := &Controller{
		ID: "node-1", Guard: w.guard, KV: w.kv, Verifier: w.rt.Verifier,
		Strategy: NewPlaneStrategy(plane, appraiser),
	}
	plane.SetRoller(PlaneRoller{Controller: c})
	return &planeRig{w: w, plane: plane, c: c}
}

func TestPlaneRoller_RollsAReleaseIntoTheInactiveSlot(t *testing.T) {
	ctx := context.Background()
	r := newPlaneRig(t, appraisal{})
	svc, err := r.plane.Redeploy(ctx, "acme", r.w.release(t, "acme", "2.0.0", serviceplane.ReleaseService))
	if err != nil {
		t.Fatalf("Redeploy: %v", err)
	}
	if svc.State != serviceplane.StateRunning {
		t.Fatalf("state = %s, want running", svc.State)
	}
	if svc.Slot != string(SlotA) {
		t.Fatalf("slot = %q, want %q", svc.Slot, SlotA)
	}
	if svc.Release == nil || svc.Release.Version != "2.0.0" {
		t.Fatalf("release = %+v, want 2.0.0", svc.Release)
	}
	if len(svc.Grants) != len(serviceplane.GrantKinds) {
		t.Fatalf("grants = %v, want all %d", svc.Grants, len(serviceplane.GrantKinds))
	}
	// Deploy launched the first instance; the fill launched the slot's.
	if got := r.w.rt.Launcher.Launched(); len(got) != 2 || got[1] != "acme/web" {
		t.Fatalf("launched = %v, want the deployed and the slot instance", got)
	}
	slots, err := r.c.Slots(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if slots.Rollout == nil || slots.Rollout.Phase != PhaseDone || slots.Active != SlotA {
		t.Fatalf("slots = %+v, want the rollout done with slot a active", slots)
	}
}

func TestPlaneRoller_AnAppraisalFailureKeepsTheActiveInstance(t *testing.T) {
	ctx := context.Background()
	r := newPlaneRig(t, appraisal{err: errors.New("evidence does not match")})
	_, err := r.plane.Redeploy(ctx, "acme", r.w.release(t, "acme", "2.0.0", serviceplane.ReleaseService))
	if !errors.Is(err, serviceplane.ErrRollout) {
		t.Fatalf("Redeploy = %v, want ErrRollout", err)
	}
	svc, err := r.plane.Get(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if svc.State != serviceplane.StateRunning || svc.Slot != "" || svc.Release != nil {
		t.Fatalf("service = %s slot %q release %+v, want the deployed instance running", svc.State, svc.Slot, svc.Release)
	}
	if len(svc.Slots) != 0 {
		t.Fatalf("slots = %v, want the filled slot drained", svc.Slots)
	}
	slots, err := r.c.Slots(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if slots.Rollout == nil || slots.Rollout.Phase != PhaseRolledBack || slots.Active != "" {
		t.Fatalf("slots = %+v, want the rollout rolled back and no active slot", slots)
	}
}

func TestPlaneRoller_AStaleEpochIsFenced(t *testing.T) {
	ctx := context.Background()
	r := newPlaneRig(t, appraisal{})
	sr := r.w.release(t, "acme", "2.0.0", serviceplane.ReleaseService)
	if _, err := r.plane.Redeploy(ctx, "acme", sr); err != nil {
		t.Fatal(err)
	}
	before, err := r.plane.Get(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	rel := *before.Release
	rel.Version = "3.0.0"
	stale := Fence{Tenant: "acme", Service: "web", Epoch: before.Epoch - 1}
	if err := (PlaneSlots{Plane: r.plane}).Fill(ctx, stale, SlotB, &rel); !errors.Is(err, ErrFenced) {
		t.Fatalf("Fill = %v, want ErrFenced", err)
	}
	if err := (PlaneNames{Plane: r.plane}).Switch(ctx, stale, SlotB); !errors.Is(err, ErrFenced) {
		t.Fatalf("Switch = %v, want ErrFenced", err)
	}
	after, err := r.plane.Get(ctx, "acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if _, filled := after.Slots[string(SlotB)]; filled || after.Slot != before.Slot || after.Epoch != before.Epoch ||
		after.Revision != before.Revision {
		t.Fatalf("service = %+v, want it unchanged from %+v", after, before)
	}
}

func TestPlaneRoller_NoRollerRefuses(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	plane := w.rt.Plane(t)
	if _, err := plane.Deploy(ctx, "acme", w.rt.Sign(t, "acme", "web")); err != nil {
		t.Fatal(err)
	}
	if _, err := plane.Redeploy(ctx, "acme", w.release(t, "acme", "2.0.0", serviceplane.ReleaseService)); !errors.Is(err, serviceplane.ErrConfig) {
		t.Fatalf("Redeploy = %v, want ErrConfig", err)
	}
	if got := w.rt.Launcher.Launched(); len(got) != 1 {
		t.Fatalf("launched = %v, want only the deployed instance", got)
	}
}
