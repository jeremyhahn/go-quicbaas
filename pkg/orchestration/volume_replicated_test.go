package orchestration

import (
	"context"
	"errors"
	"testing"

	bscommon "github.com/jeremyhahn/go-blockstore/pkg/common"
)

// roleProvider is a replicated fake that tracks each volume's local role:
// Promote makes it primary, Demote secondary, and ReplicationState reports it.
type roleProvider struct {
	*fakeVolumeProvider
	role map[string]bscommon.ReplRole
}

func newRoleProvider() *roleProvider {
	return &roleProvider{fakeVolumeProvider: newFakeVolumeProvider(), role: map[string]bscommon.ReplRole{}}
}

func (p *roleProvider) Promote(_ context.Context, id string) error {
	if err := p.record("promote", id); err != nil {
		return err
	}
	p.role[id] = bscommon.ReplRolePrimary
	return nil
}

func (p *roleProvider) Demote(_ context.Context, id string) error {
	if err := p.record("demote", id); err != nil {
		return err
	}
	p.role[id] = bscommon.ReplRoleSecondary
	return nil
}

func (p *roleProvider) ReplicationState(_ context.Context, id string) (bscommon.ReplState, error) {
	if err := p.record("state", id); err != nil {
		return bscommon.ReplState{}, err
	}
	role, ok := p.role[id]
	if !ok {
		role = bscommon.ReplRoleSecondary
	}
	return bscommon.ReplState{Role: role}, nil
}

// assertOrder fails unless each call appears in calls, in the given order.
func assertOrder(t *testing.T, calls []string, order ...string) {
	t.Helper()
	last := -1
	for _, c := range order {
		i := callIndex(calls, c)
		if i < 0 || i <= last {
			t.Fatalf("calls = %v, want %v in this order", calls, order)
		}
		last = i
	}
}

var dataClaim = VolumeClaim{Name: "data", SizeBytes: 100, FSType: "xfs", Target: "/d"}

func TestReplicatedVolumeAttachPromotesBeforeMount(t *testing.T) {
	p := newRoleProvider()
	a := newTestAttacher(t, p)
	vols, err := a.Attach(context.Background(), []VolumeClaim{dataClaim})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	assertOrder(t, p.calls, "attach data", "promote data", "mount data")
	if len(vols) != 1 || !vols[0].Promoted {
		t.Fatalf("vols = %+v, want one promoted volume", vols)
	}
}

func TestReplicatedVolumeReleaseDemotesBeforeDetach(t *testing.T) {
	ctx := context.Background()
	p := newRoleProvider()
	a := newTestAttacher(t, p)
	vols, err := a.Attach(ctx, []VolumeClaim{dataClaim})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	p.calls = nil
	if err := a.Release(ctx, vols); err != nil {
		t.Fatalf("Release: %v", err)
	}
	assertOrder(t, p.calls, "unmount data", "demote data", "detach data")
	if p.role["data"] != bscommon.ReplRoleSecondary {
		t.Fatalf("role = %q, want secondary", p.role["data"])
	}
}

func TestReplicatedVolumeRawClaimIsPromoted(t *testing.T) {
	ctx := context.Background()
	p := newRoleProvider()
	a := newTestAttacher(t, p)
	raw := VolumeClaim{Name: "dev", SizeBytes: 100, Target: "/dev/xvdb"}
	vols, err := a.Attach(ctx, []VolumeClaim{raw})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(vols) != 1 || !vols[0].Promoted || vols[0].HostPath != "" {
		t.Fatalf("vols = %+v, want one promoted raw volume", vols)
	}
	if err := a.Release(ctx, vols); err != nil {
		t.Fatalf("Release: %v", err)
	}
	assertOrder(t, p.calls, "attach dev", "promote dev", "demote dev", "detach dev")
	if callIndex(p.calls, "mount dev") >= 0 || callIndex(p.calls, "unmount dev") >= 0 {
		t.Fatalf("calls = %v, want no mount for a raw claim", p.calls)
	}
}

func TestReplicatedVolumePromoteFailureRollsBack(t *testing.T) {
	p := newRoleProvider()
	p.failOn["promote data"] = errors.New("peer is primary")
	a := newTestAttacher(t, p)
	vols, held, err := a.attach(context.Background(), []VolumeClaim{dataClaim}, nil)
	if err == nil {
		t.Fatalf("attach succeeded with %+v, want the promote error", vols)
	}
	if len(vols) != 0 || len(held) != 0 {
		t.Fatalf("vols = %+v, held = %+v, want nothing", vols, held)
	}
	assertOrder(t, p.calls, "attach data", "promote data", "demote data", "detach data")
	if callIndex(p.calls, "mount data") >= 0 {
		t.Fatalf("calls = %v, want no mount after a failed promote", p.calls)
	}
}

func TestReplicatedVolumeDemoteFailureKeepsItHeld(t *testing.T) {
	ctx := context.Background()
	p := newRoleProvider()
	a := newTestAttacher(t, p)
	vols, err := a.Attach(ctx, []VolumeClaim{dataClaim})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	p.calls = nil
	p.failOn["demote data"] = errors.New("device busy")
	held, err := a.release(ctx, vols)
	if err == nil {
		t.Fatal("release succeeded, want the demote error")
	}
	if len(held) != 1 || !held[0].Promoted || held[0].HostPath != "" {
		t.Fatalf("held = %+v, want one promoted, unmounted volume", held)
	}
	if callIndex(p.calls, "detach data") >= 0 {
		t.Fatalf("calls = %v, want no detach of a primary", p.calls)
	}
	delete(p.failOn, "demote data")
	p.calls = nil
	held, err = a.release(ctx, held)
	if err != nil || len(held) != 0 {
		t.Fatalf("retry release = %+v, %v, want nothing held", held, err)
	}
	assertOrder(t, p.calls, "demote data", "detach data")
	if callIndex(p.calls, "unmount data") >= 0 {
		t.Fatalf("calls = %v, want no second unmount", p.calls)
	}
}

func TestReplicatedVolumeDemoteFailureOnASecondaryCountsAsDemoted(t *testing.T) {
	ctx := context.Background()
	p := newRoleProvider()
	a := newTestAttacher(t, p)
	vols, err := a.Attach(ctx, []VolumeClaim{dataClaim})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	p.role["data"] = bscommon.ReplRoleSecondary
	p.failOn["demote data"] = errors.New("not primary")
	p.calls = nil
	held, err := a.release(ctx, vols)
	if err != nil || len(held) != 0 {
		t.Fatalf("release = %+v, %v, want nothing held", held, err)
	}
	assertOrder(t, p.calls, "demote data", "state data", "detach data")
}

func TestReplicatedVolumeGrowRunsAfterPromote(t *testing.T) {
	p := newRoleProvider()
	p.sizes["data"] = 50
	a := newTestAttacher(t, p)
	if _, err := a.Attach(context.Background(), []VolumeClaim{dataClaim}); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	promote := callIndex(p.calls, "promote data")
	last := -1
	for i, c := range p.calls {
		if c == "resize data" {
			last = i
		}
	}
	if promote < 0 || last < promote {
		t.Fatalf("calls = %v, want the attached resize after promote", p.calls)
	}
}

func TestVolumeAttacherAttachNeverPromotesANonReplicatedVolume(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	a := newTestAttacher(t, p)
	vols, err := a.Attach(ctx, []VolumeClaim{dataClaim})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(vols) != 1 || vols[0].Promoted {
		t.Fatalf("vols = %+v, want one volume not promoted", vols)
	}
	if err := a.Release(ctx, vols); err != nil {
		t.Fatalf("Release: %v", err)
	}
	for _, c := range p.calls {
		if c == "promote data" || c == "demote data" {
			t.Fatalf("calls = %v, want no promote or demote", p.calls)
		}
	}
}
