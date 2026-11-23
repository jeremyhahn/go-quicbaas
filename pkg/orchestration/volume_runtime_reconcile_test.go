package orchestration

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// stubOwnedLister returns a fixed listing.
type stubOwnedLister struct {
	cs  []OwnedContainer
	err error
}

func (l *stubOwnedLister) ListOwned(context.Context, string) ([]OwnedContainer, error) {
	return l.cs, l.err
}

func (l *stubOwnedLister) Remove(context.Context, string) error { return nil }

// newOwnedVolumeRuntime builds a VolumeRuntime that checks the owner label
// "/srv/agent".
func newOwnedVolumeRuntime(t *testing.T, opts ...VolumeRuntimeOption) (*VolumeRuntime, *fakeVolumeProvider) {
	t.Helper()
	prov := newFakeVolumeProvider()
	att, err := NewVolumeAttacher(prov, "node-1", t.TempDir())
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := NewVolumeRuntime(&launchRecorder{}, att, append([]VolumeRuntimeOption{WithVolumeOwner("/srv/agent")}, opts...)...)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	t.Cleanup(func() { _ = vr.Close() })
	return vr, prov
}

// ownedDataSpec is dataSpec carrying the owner label "/srv/agent".
func ownedDataSpec(name string) *ContainerSpec {
	s := dataSpec(name)
	s.Labels = map[string]string{ContainerOwnerLabel: "/srv/agent"}
	return s
}

// A container gone before any listing has its volumes released, so its claims can be launched again.
func TestVolumeRuntimeReconcileReleasesAContainerGoneBeforeAnyListing(t *testing.T) {
	ctx := context.Background()
	vr, prov := newOwnedVolumeRuntime(t, WithVolumeReconcileGrace(0))
	p, err := vr.RunInteractive(ctx, ownedDataSpec("a"))
	if err != nil {
		t.Fatalf("RunInteractive(a): %v", err)
	}
	cs, err := vr.ReconcileVolumes(ctx, "/srv/agent", &stubOwnedLister{})
	if err != nil || len(cs) != 0 {
		t.Fatalf("ReconcileVolumes = %v, %v; want none, nil", cs, err)
	}
	if got := countCalls(prov.calls, "detach "); got != 1 {
		t.Errorf("detaches = %d, want 1", got)
	}
	if got := vr.Attached(p.ContainerID); len(got) != 0 {
		t.Errorf("Attached(%s) = %+v, want none", p.ContainerID, got)
	}
	if got := vr.Attached("a"); len(got) != 0 {
		t.Errorf("Attached(a) = %+v, want none", got)
	}
	if _, err := vr.RunInteractive(ctx, ownedDataSpec("b")); err != nil {
		t.Fatalf("RunInteractive(b) on the released claim: %v", err)
	}

	// A detached container removed outside the runtime is released the same way.
	if err := vr.Remove(ctx, "ctr-b"); err != nil {
		t.Fatalf("Remove(ctr-b): %v", err)
	}
	if _, err := vr.Run(ctx, ownedDataSpec("c")); err != nil {
		t.Fatalf("Run(c): %v", err)
	}
	if _, err := vr.ReconcileVolumes(ctx, "/srv/agent", &stubOwnedLister{}); err != nil {
		t.Fatalf("ReconcileVolumes: %v", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 3 {
		t.Errorf("detaches = %d, want 3", got)
	}
	if got := vr.Attached("ctr-c"); len(got) != 0 {
		t.Errorf("Attached(ctr-c) = %+v, want none", got)
	}
}

// A container the listing shows, by name, full ID or short ID, keeps its volumes.
func TestVolumeRuntimeReconcileKeepsListedContainers(t *testing.T) {
	cases := []struct {
		name        string
		interactive bool
		spec        string
		listed      OwnedContainer
	}{
		{"by name", true, "a", OwnedContainer{ID: "0123456789abcdef", Name: "ctr-a"}},
		{"by full ID", false, "a", OwnedContainer{ID: "ctr-a", Name: "other"}},
		{"by short ID", false, "abcdef012345", OwnedContainer{ID: "ctr-abcdef012345ffffffff", Name: "other"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			vr, prov := newOwnedVolumeRuntime(t, WithVolumeReconcileGrace(0))
			var id string
			if tc.interactive {
				p, err := vr.RunInteractive(ctx, ownedDataSpec(tc.spec))
				if err != nil {
					t.Fatalf("RunInteractive: %v", err)
				}
				id = p.ContainerID
			} else {
				var err error
				if id, err = vr.Run(ctx, ownedDataSpec(tc.spec)); err != nil {
					t.Fatalf("Run: %v", err)
				}
			}
			listing := []OwnedContainer{tc.listed}
			cs, err := vr.ReconcileVolumes(ctx, "/srv/agent", &stubOwnedLister{cs: listing})
			if err != nil {
				t.Fatalf("ReconcileVolumes: %v", err)
			}
			if !slices.Equal(cs, listing) {
				t.Errorf("ReconcileVolumes listing = %+v, want %+v", cs, listing)
			}
			if got := countCalls(prov.calls, "detach "); got != 0 {
				t.Errorf("detaches = %d, want 0", got)
			}
			if got := vr.Attached(id); len(got) == 0 {
				t.Errorf("Attached(%s) = none, want the volume", id)
			}
		})
	}
}

// A launch younger than the grace keeps its volumes though the listing does not show it.
func TestVolumeRuntimeReconcileWaitsOutTheGrace(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []VolumeRuntimeOption
	}{
		{"default", nil},
		{"an hour", []VolumeRuntimeOption{WithVolumeReconcileGrace(time.Hour)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			vr, prov := newOwnedVolumeRuntime(t, tc.opts...)
			p, err := vr.RunInteractive(ctx, ownedDataSpec("a"))
			if err != nil {
				t.Fatalf("RunInteractive: %v", err)
			}
			if _, err := vr.ReconcileVolumes(ctx, "/srv/agent", &stubOwnedLister{}); err != nil {
				t.Fatalf("ReconcileVolumes: %v", err)
			}
			if got := countCalls(prov.calls, "detach "); got != 0 {
				t.Errorf("detaches = %d, want 0", got)
			}
			if got := vr.Attached(p.ContainerID); len(got) == 0 {
				t.Errorf("Attached(%s) = none, want the volume", p.ContainerID)
			}
		})
	}
}

// Without the runtime's owner label, or for another owner, reconcile only lists.
func TestVolumeRuntimeReconcileNeedsTheOwnerLabel(t *testing.T) {
	ctx := context.Background()
	t.Run("no owner label", func(t *testing.T) {
		vr, _, prov, _ := newTestVolumeRuntime(t)
		vr.reconcileGrace = 0
		p, err := vr.RunInteractive(ctx, dataSpec("a"))
		if err != nil {
			t.Fatalf("RunInteractive: %v", err)
		}
		if _, err := vr.ReconcileVolumes(ctx, "/srv/agent", &stubOwnedLister{}); err != nil {
			t.Fatalf("ReconcileVolumes: %v", err)
		}
		if got := countCalls(prov.calls, "detach "); got != 0 {
			t.Errorf("detaches = %d, want 0", got)
		}
		if got := vr.Attached(p.ContainerID); len(got) == 0 {
			t.Errorf("Attached(%s) = none, want the volume", p.ContainerID)
		}
	})
	t.Run("another owner", func(t *testing.T) {
		vr, prov := newOwnedVolumeRuntime(t, WithVolumeReconcileGrace(0))
		p, err := vr.RunInteractive(ctx, ownedDataSpec("a"))
		if err != nil {
			t.Fatalf("RunInteractive: %v", err)
		}
		listing := []OwnedContainer{{ID: "zzz", Name: "other"}}
		cs, err := vr.ReconcileVolumes(ctx, "/srv/other", &stubOwnedLister{cs: listing})
		if err != nil {
			t.Fatalf("ReconcileVolumes: %v", err)
		}
		if !slices.Equal(cs, listing) {
			t.Errorf("ReconcileVolumes listing = %+v, want %+v", cs, listing)
		}
		if got := countCalls(prov.calls, "detach "); got != 0 {
			t.Errorf("detaches = %d, want 0", got)
		}
		if got := vr.Attached(p.ContainerID); len(got) == 0 {
			t.Errorf("Attached(%s) = none, want the volume", p.ContainerID)
		}
	})
}

// A listing that fails releases nothing and its error is returned.
func TestVolumeRuntimeReconcileReleasesNothingWhenListingFails(t *testing.T) {
	ctx := context.Background()
	vr, prov := newOwnedVolumeRuntime(t, WithVolumeReconcileGrace(0))
	p, err := vr.RunInteractive(ctx, ownedDataSpec("a"))
	if err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	listErr := errors.New("list failed")
	if _, err := vr.ReconcileVolumes(ctx, "/srv/agent", &stubOwnedLister{err: listErr}); !errors.Is(err, listErr) {
		t.Fatalf("ReconcileVolumes error = %v, want %v", err, listErr)
	}
	if got := countCalls(prov.calls, "detach "); got != 0 {
		t.Errorf("detaches = %d, want 0", got)
	}
	if got := vr.Attached(p.ContainerID); len(got) == 0 {
		t.Errorf("Attached(%s) = none, want the volume", p.ContainerID)
	}
}
