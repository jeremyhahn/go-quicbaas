package orchestration

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	bscommon "github.com/jeremyhahn/go-blockstore/pkg/common"
)

// fakeVolumeProvider is an in-memory go-blockstore VolumeProvider that records
// every call, in order, as "<op> <volume>".
type fakeVolumeProvider struct {
	sizes     map[string]int64
	calls     []string
	failOn    map[string]error // "<op> <volume>" -> error
	devPrefix string
}

func newFakeVolumeProvider() *fakeVolumeProvider {
	return &fakeVolumeProvider{sizes: map[string]int64{}, failOn: map[string]error{}, devPrefix: "/dev/fake/"}
}

func (f *fakeVolumeProvider) record(op, id string) error {
	key := op + " " + id
	f.calls = append(f.calls, key)
	return f.failOn[key]
}

func (f *fakeVolumeProvider) Provision(_ context.Context, spec bscommon.VolumeSpec) (bscommon.Volume, error) {
	if err := f.record("provision", spec.Name); err != nil {
		return bscommon.Volume{}, err
	}
	f.sizes[spec.Name] = spec.SizeBytes
	return bscommon.Volume{ID: spec.Name, Spec: spec}, nil
}

func (f *fakeVolumeProvider) Attach(_ context.Context, id string, node bscommon.NodeID) (bscommon.AttachInfo, error) {
	if err := f.record("attach", id); err != nil {
		return bscommon.AttachInfo{}, err
	}
	return bscommon.AttachInfo{DevicePath: f.devPrefix + id, Node: node}, nil
}

func (f *fakeVolumeProvider) Detach(_ context.Context, id string, _ bscommon.NodeID) error {
	return f.record("detach", id)
}

func (f *fakeVolumeProvider) Mount(_ context.Context, id, _ string, _ bscommon.MountOptions) error {
	return f.record("mount", id)
}

func (f *fakeVolumeProvider) Unmount(_ context.Context, id, _ string) error {
	return f.record("unmount", id)
}

func (f *fakeVolumeProvider) Resize(_ context.Context, id string, n int64) error {
	if err := f.record("resize", id); err != nil {
		return err
	}
	f.sizes[id] = n
	return nil
}

func (f *fakeVolumeProvider) Snapshot(context.Context, string, string) (bscommon.SnapshotID, error) {
	return "", bscommon.ErrUnsupported
}

func (f *fakeVolumeProvider) ListSnapshots(context.Context, string) ([]bscommon.SnapshotInfo, error) {
	return nil, bscommon.ErrUnsupported
}

func (f *fakeVolumeProvider) DeleteSnapshot(context.Context, bscommon.SnapshotID) error {
	return bscommon.ErrUnsupported
}

func (f *fakeVolumeProvider) Delete(_ context.Context, id string) error {
	delete(f.sizes, id)
	return f.record("delete", id)
}

func (f *fakeVolumeProvider) Status(_ context.Context, id string) (bscommon.VolumeStatus, error) {
	n, ok := f.sizes[id]
	if !ok {
		return bscommon.VolumeStatus{}, bscommon.ErrNotFound
	}
	return bscommon.VolumeStatus{ID: id, SizeBytes: n}, nil
}

// fakeReplicatedProvider adds the primary/secondary roles.
type fakeReplicatedProvider struct{ *fakeVolumeProvider }

func (f fakeReplicatedProvider) Promote(_ context.Context, id string) error {
	return f.record("promote", id)
}

func (f fakeReplicatedProvider) Demote(_ context.Context, id string) error {
	return f.record("demote", id)
}

func (f fakeReplicatedProvider) ReplicationState(context.Context, string) (bscommon.ReplState, error) {
	return bscommon.ReplState{}, nil
}

func newTestAttacher(t *testing.T, p VolumeProvider) *VolumeAttacher {
	t.Helper()
	a, err := NewVolumeAttacher(p, "node-1", t.TempDir())
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	return a
}

func TestVolumeClaimValidate(t *testing.T) {
	cases := []struct {
		name  string
		claim VolumeClaim
		ok    bool
	}{
		{"fs claim", VolumeClaim{Name: "data", SizeBytes: 1, FSType: "ext4", Target: "/var/lib/data"}, true},
		{"raw claim", VolumeClaim{Name: "root.img", SizeBytes: 1}, true},
		{"bad name", VolumeClaim{Name: "../x", SizeBytes: 1}, false},
		{"upper-case name", VolumeClaim{Name: "Data", SizeBytes: 1}, false},
		{"zero size", VolumeClaim{Name: "data", SizeBytes: 0}, false},
		{"negative replicas", VolumeClaim{Name: "data", SizeBytes: 1, Replicas: -1}, false},
		{"raw with device target", VolumeClaim{Name: "data", SizeBytes: 1, Target: "/dev/vol/data"}, true},
		{"raw with relative target", VolumeClaim{Name: "data", SizeBytes: 1, Target: "dev/x"}, false},
		{"raw with colon target", VolumeClaim{Name: "data", SizeBytes: 1, Target: "/dev/x:rw"}, false},
		{"fs without target", VolumeClaim{Name: "data", SizeBytes: 1, FSType: "ext4"}, false},
		{"relative target", VolumeClaim{Name: "data", SizeBytes: 1, FSType: "ext4", Target: "x"}, false},
		{"unclean target", VolumeClaim{Name: "data", SizeBytes: 1, FSType: "ext4", Target: "/a/../b"}, false},
		{"root target", VolumeClaim{Name: "data", SizeBytes: 1, FSType: "ext4", Target: "/"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.claim.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrVolumeClaim) {
				t.Fatalf("Validate = %v, want ErrVolumeClaim", err)
			}
		})
	}
}

func TestNewVolumeAttacherRejectsBadArguments(t *testing.T) {
	p := newFakeVolumeProvider()
	for name, call := range map[string]func() (*VolumeAttacher, error){
		"nil provider":     func() (*VolumeAttacher, error) { return NewVolumeAttacher(nil, "n", "/s") },
		"empty node":       func() (*VolumeAttacher, error) { return NewVolumeAttacher(p, "", "/s") },
		"relative staging": func() (*VolumeAttacher, error) { return NewVolumeAttacher(p, "n", "s") },
	} {
		if _, err := call(); !errors.Is(err, ErrVolumeInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrVolumeInvalidArgument", name, err)
		}
	}
}

func TestVolumeAttacherEnsure(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	a := newTestAttacher(t, p)
	claim := VolumeClaim{Name: "data", SizeBytes: 100}

	// Provision on a miss.
	if id, err := a.Ensure(ctx, claim); err != nil || id != "data" {
		t.Fatalf("Ensure (miss) = %q, %v", id, err)
	}
	// Reuse on a hit of the same size.
	if _, err := a.Ensure(ctx, claim); err != nil {
		t.Fatalf("Ensure (hit): %v", err)
	}
	// Grow on a larger claim; a smaller claim leaves the volume as it is.
	claim.SizeBytes = 200
	if _, err := a.Ensure(ctx, claim); err != nil {
		t.Fatalf("Ensure (grow): %v", err)
	}
	claim.SizeBytes = 50
	if _, err := a.Ensure(ctx, claim); err != nil {
		t.Fatalf("Ensure (smaller): %v", err)
	}
	want := []string{"provision data", "resize data"}
	if !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls = %v, want %v", p.calls, want)
	}
	if p.sizes["data"] != 200 {
		t.Fatalf("size = %d, want 200", p.sizes["data"])
	}
}

func TestVolumeAttacherResizeIsGrowOnly(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	p.sizes["data"] = 100
	a := newTestAttacher(t, p)

	if err := a.Resize(ctx, "data", 50); !errors.Is(err, ErrVolumeShrink) {
		t.Fatalf("shrink: err = %v, want ErrVolumeShrink", err)
	}
	if len(p.calls) != 0 {
		t.Fatalf("a refused shrink reached the provider: %v", p.calls)
	}
	if err := a.Resize(ctx, "data", 0); !errors.Is(err, ErrVolumeInvalidArgument) {
		t.Fatalf("zero: err = %v, want ErrVolumeInvalidArgument", err)
	}
	if err := a.Resize(ctx, "data", 100); err != nil {
		t.Fatalf("equal size: %v", err)
	}
	if err := a.Resize(ctx, "data", 300); err != nil {
		t.Fatalf("grow: %v", err)
	}
	if p.sizes["data"] != 300 {
		t.Fatalf("size = %d, want 300", p.sizes["data"])
	}
	if err := a.Resize(ctx, "missing", 10); !errors.Is(err, ErrVolumeNotFound) {
		t.Fatalf("missing: err = %v, want ErrVolumeNotFound", err)
	}
}

func TestVolumeAttacherPromoteDemote(t *testing.T) {
	ctx := context.Background()

	plain := newTestAttacher(t, newFakeVolumeProvider())
	if err := plain.Promote(ctx, "data"); !errors.Is(err, ErrVolumeUnsupported) {
		t.Fatalf("Promote on a plain provider: err = %v, want ErrVolumeUnsupported", err)
	}
	if err := plain.Demote(ctx, "data"); !errors.Is(err, ErrVolumeUnsupported) {
		t.Fatalf("Demote on a plain provider: err = %v, want ErrVolumeUnsupported", err)
	}

	rp := fakeReplicatedProvider{newFakeVolumeProvider()}
	repl := newTestAttacher(t, rp)
	if err := repl.Promote(ctx, "data"); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if err := repl.Demote(ctx, "data"); err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if want := []string{"promote data", "demote data"}; !reflect.DeepEqual(rp.calls, want) {
		t.Fatalf("calls = %v, want %v", rp.calls, want)
	}
}

func TestVolumeAttacherAttachAndRelease(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	staging := t.TempDir()
	a, err := NewVolumeAttacher(p, "node-1", staging)
	if err != nil {
		t.Fatal(err)
	}
	claims := []VolumeClaim{
		{Name: "data", SizeBytes: 10, FSType: "ext4", Target: "/data", ReadOnly: true},
		{Name: "raw", SizeBytes: 10},
	}
	vols, err := a.Attach(ctx, claims)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("attached %d volumes, want 2", len(vols))
	}
	if vols[0].DevicePath != "/dev/fake/data" || vols[0].HostPath != filepath.Join(staging, "data") {
		t.Fatalf("fs volume = %+v", vols[0])
	}
	if vols[1].DevicePath != "/dev/fake/raw" || vols[1].HostPath != "" {
		t.Fatalf("raw volume = %+v", vols[1])
	}
	mounts := ContainerMounts(vols)
	want := []ContainerMount{{Source: filepath.Join(staging, "data"), Target: "/data", ReadOnly: true, Type: MountTypeBind}}
	if !reflect.DeepEqual(mounts, want) {
		t.Fatalf("mounts = %+v, want %+v", mounts, want)
	}

	if err := a.Release(ctx, vols); err != nil {
		t.Fatalf("Release: %v", err)
	}
	wantCalls := []string{
		"provision data", "attach data", "mount data",
		"provision raw", "attach raw",
		"detach raw", "unmount data", "detach data",
	}
	if !reflect.DeepEqual(p.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", p.calls, wantCalls)
	}
}

func TestVolumeAttacherAttachRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	boom := errors.New("mount failed")
	p.failOn["mount second"] = boom
	a := newTestAttacher(t, p)
	claims := []VolumeClaim{
		{Name: "first", SizeBytes: 10, FSType: "ext4", Target: "/a"},
		{Name: "second", SizeBytes: 10, FSType: "ext4", Target: "/b"},
	}
	vols, err := a.Attach(ctx, claims)
	if !errors.Is(err, boom) || vols != nil {
		t.Fatalf("Attach = %v, %v; want nil, the mount error", vols, err)
	}
	want := []string{
		"provision first", "attach first", "mount first",
		"provision second", "attach second", "mount second",
		// The half-attached second volume is detached, then the first.
		"detach second", "unmount first", "detach first",
	}
	if !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls = %v, want %v", p.calls, want)
	}
}

func TestVolumeAttacherReleaseCarriesOnPastFailure(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	boom := errors.New("busy")
	p.failOn["unmount a"] = boom
	a := newTestAttacher(t, p)
	vols := []AttachedVolume{
		{Claim: VolumeClaim{Name: "a"}, VolumeID: "a", HostPath: "/s/a"},
		{Claim: VolumeClaim{Name: "b"}, VolumeID: "b"},
	}
	if err := a.Release(ctx, vols); !errors.Is(err, boom) {
		t.Fatalf("Release = %v, want the unmount error", err)
	}
	// A volume still mounted is not detached; the others still are.
	if want := []string{"detach b", "unmount a"}; !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls = %v, want %v", p.calls, want)
	}
}

func TestVolumeAttacherAttachRejectsDuplicateClaims(t *testing.T) {
	ctx := context.Background()
	for name, claims := range map[string][]VolumeClaim{
		"same name": {
			{Name: "a", SizeBytes: 1},
			{Name: "a", SizeBytes: 1},
		},
		"same target": {
			{Name: "a", SizeBytes: 1, FSType: "ext4", Target: "/x"},
			{Name: "b", SizeBytes: 1, FSType: "ext4", Target: "/x"},
		},
	} {
		p := newFakeVolumeProvider()
		a := newTestAttacher(t, p)
		if _, err := a.Attach(ctx, claims); !errors.Is(err, ErrVolumeClaim) {
			t.Errorf("%s: err = %v, want ErrVolumeClaim", name, err)
		}
		if len(p.calls) != 0 {
			t.Errorf("%s: provider reached before validation: %v", name, p.calls)
		}
	}
}

func TestVolumeAttacherRejectsUnusableProviderID(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	p.sizes["data"] = 10
	a := newTestAttacher(t, idProvider{p, "../escape"})
	if _, err := a.Attach(ctx, []VolumeClaim{{Name: "data", SizeBytes: 10, FSType: "ext4", Target: "/d"}}); !errors.Is(err, ErrVolumeInvalidArgument) {
		t.Fatalf("Attach = %v, want ErrVolumeInvalidArgument", err)
	}
	for _, c := range p.calls {
		if c == "attach ../escape" {
			t.Fatalf("an unusable id reached Attach")
		}
	}
}

// idProvider reports a fixed volume id from Status.
type idProvider struct {
	*fakeVolumeProvider
	id string
}

func (p idProvider) Status(ctx context.Context, name string) (bscommon.VolumeStatus, error) {
	st, err := p.fakeVolumeProvider.Status(ctx, name)
	st.ID = p.id
	return st, err
}

func TestNewVolumeProvider(t *testing.T) {
	if _, err := NewVolumeProvider("no-such-provider", nil); err == nil {
		t.Fatal("unknown provider: want an error")
	}
	if _, err := NewVolumeProvider("local", map[string]string{"no_such_key": "x"}); err == nil {
		t.Fatal("unknown settings key: want an error")
	}
}
