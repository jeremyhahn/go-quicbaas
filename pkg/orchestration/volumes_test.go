package orchestration

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	bscommon "github.com/jeremyhahn/go-blockstore/pkg/common"
	bslocal "github.com/jeremyhahn/go-blockstore/pkg/local"
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

// Detach and Unmount fail on a done context, as the real ones do, so a
// cleanup run on a cancelled launch context fails visibly.
func (f *fakeVolumeProvider) Detach(ctx context.Context, id string, _ bscommon.NodeID) error {
	if err := f.record("detach", id); err != nil {
		return err
	}
	return ctx.Err()
}

func (f *fakeVolumeProvider) Mount(_ context.Context, id, _ string, _ bscommon.MountOptions) error {
	return f.record("mount", id)
}

func (f *fakeVolumeProvider) Unmount(ctx context.Context, id, _ string) error {
	if err := f.record("unmount", id); err != nil {
		return err
	}
	return ctx.Err()
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
		// The second volume, whose mount failed, is unmounted and detached,
		// then the first.
		"unmount second", "detach second", "unmount first", "detach first",
	}
	if !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls = %v, want %v", p.calls, want)
	}
}

// TestVolumeAttacherUnmountsAfterAFailedMount: a Mount that fails may have
// left a live mount (the provider mounted, refused a later check and failed
// its own unmount), so the volume is held with its target and a release
// unmounts it before detaching.
func TestVolumeAttacherUnmountsAfterAFailedMount(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	boom := errors.New("left a live mount")
	p.failOn["mount only"] = boom
	p.failOn["unmount only"] = errors.New("target busy")
	a := newTestAttacher(t, p)
	claims := []VolumeClaim{{Name: "only", SizeBytes: 10, FSType: "ext4", Target: "/a"}}
	vols, held, err := a.attach(ctx, claims)
	if !errors.Is(err, boom) || vols != nil {
		t.Fatalf("attach = %v, %v; want nil, the mount error", vols, err)
	}
	if len(held) != 1 || held[0].HostPath != filepath.Join(a.StagingDir(), "only") {
		t.Fatalf("held = %+v, want the volume with its mount target", held)
	}
	for _, c := range p.calls {
		if c == "detach only" {
			t.Fatalf("calls = %v: detached while still mounted", p.calls)
		}
	}

	delete(p.failOn, "unmount only")
	held, err = a.release(ctx, held)
	if err != nil || len(held) != 0 {
		t.Fatalf("release = %+v, %v; want nothing held", held, err)
	}
	if n := len(p.calls); n < 2 || p.calls[n-2] != "unmount only" || p.calls[n-1] != "detach only" {
		t.Fatalf("calls = %v, want them to end with unmount only, detach only", p.calls)
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

// cancelOnMountProvider cancels the launch context when the second volume
// mounts, as a launch cancelled part way does.
type cancelOnMountProvider struct {
	*fakeVolumeProvider
	cancel context.CancelFunc
}

func (p cancelOnMountProvider) Mount(ctx context.Context, id, host string, o bscommon.MountOptions) error {
	if id == "second" {
		_ = p.record("mount", id)
		p.cancel()
		return context.Canceled
	}
	return p.fakeVolumeProvider.Mount(ctx, id, host, o)
}

// A rollback after a cancelled launch runs on its own context, so it still
// unmounts and detaches.
func TestVolumeAttacherAttachRollbackSurvivesCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := newFakeVolumeProvider()
	a := newTestAttacher(t, cancelOnMountProvider{p, cancel})
	_, err := a.Attach(ctx, []VolumeClaim{
		{Name: "first", SizeBytes: 10, FSType: "ext4", Target: "/a"},
		{Name: "second", SizeBytes: 10, FSType: "ext4", Target: "/b"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Attach = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), "release after failed attach") {
		t.Errorf("the rollback failed: %v", err)
	}
	for _, want := range []string{"unmount first", "detach first", "detach second"} {
		if countCalls(p.calls, want) != 1 {
			t.Errorf("calls %v lack %q", p.calls, want)
		}
	}
}

// A grow of an existing volume is finished after mounting (XFS grows only
// mounted), on every launch, and not for a read-only claim.
func TestVolumeAttacherGrowsFilesystemAfterMount(t *testing.T) {
	ctx := context.Background()
	p := newFakeVolumeProvider()
	p.sizes["data"] = 100
	a := newTestAttacher(t, p)
	claim := VolumeClaim{Name: "data", SizeBytes: 200, FSType: "xfs", Target: "/d"}
	vols, err := a.Attach(ctx, []VolumeClaim{claim})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	want := []string{"resize data", "attach data", "mount data", "resize data"}
	if !reflect.DeepEqual(p.calls, want) || p.sizes["data"] != 200 {
		t.Fatalf("calls = %v (size %d), want %v (size 200)", p.calls, p.sizes["data"], want)
	}
	if err := a.Release(ctx, vols); err != nil {
		t.Fatalf("Release: %v", err)
	}
	want = append(want, "unmount data", "detach data", "attach data", "mount data", "resize data")
	if vols, err = a.Attach(ctx, []VolumeClaim{claim}); err != nil {
		t.Fatalf("second Attach: %v", err)
	}
	if !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls = %v, want %v", p.calls, want)
	}
	if err := a.Release(ctx, vols); err != nil {
		t.Fatalf("Release: %v", err)
	}
	claim.ReadOnly = true
	if _, err := a.Attach(ctx, []VolumeClaim{claim}); err != nil {
		t.Fatalf("read-only Attach: %v", err)
	}
	if got := countCalls(p.calls, "resize "); got != 3 {
		t.Errorf("resizes = %d, want 3: a read-only claim is not grown (calls %v)", got, p.calls)
	}
}

// attachedResizeRefuser refuses Resize while attached, as the local provider
// does for an encrypted volume.
type attachedResizeRefuser struct{ *fakeVolumeProvider }

func (p attachedResizeRefuser) Resize(_ context.Context, id string, _ int64) error {
	_ = p.record("resize", id)
	return bslocal.ErrResizeWhileAttached
}

// The local provider refuses any Resize of an attached encrypted volume; the
// grow after mount then does not fail the launch.
func TestVolumeAttacherToleratesAttachedResizeRefusal(t *testing.T) {
	p := newFakeVolumeProvider()
	p.sizes["data"] = 200
	a := newTestAttacher(t, attachedResizeRefuser{p})
	if _, err := a.Attach(context.Background(), []VolumeClaim{{Name: "data", SizeBytes: 200, FSType: "xfs", Target: "/d"}}); err != nil {
		t.Fatalf("Attach = %v, want the refusal tolerated", err)
	}
	if got := countCalls(p.calls, "resize data"); got != 1 {
		t.Errorf("resizes = %d, want 1 (calls %v)", got, p.calls)
	}
}

// release reports what it could not release, in order: a volume still mounted
// as it was, one that only failed to detach without its HostPath.
func TestVolumeAttacherReleaseReportsHeldVolumes(t *testing.T) {
	p := newFakeVolumeProvider()
	p.failOn["unmount a"] = errors.New("busy")
	p.failOn["detach b"] = errors.New("busy")
	a := newTestAttacher(t, p)
	vols := []AttachedVolume{
		{Claim: VolumeClaim{Name: "a"}, VolumeID: "a", HostPath: "/s/a"},
		{Claim: VolumeClaim{Name: "b"}, VolumeID: "b", HostPath: "/s/b"},
		{Claim: VolumeClaim{Name: "c"}, VolumeID: "c"},
	}
	held, err := a.release(context.Background(), vols)
	if err == nil {
		t.Fatal("release = nil error, want the failures")
	}
	if len(held) != 2 || held[0].VolumeID != "a" || held[1].VolumeID != "b" {
		t.Fatalf("held = %+v, want a then b", held)
	}
	if held[0].HostPath != "/s/a" || held[1].HostPath != "" {
		t.Errorf("held host paths = %q, %q; want /s/a and empty", held[0].HostPath, held[1].HostPath)
	}
}

// detachedResizeRefuser refuses a Resize of a volume that is not attached,
// as the drbd provider does.
type detachedResizeRefuser struct {
	*fakeVolumeProvider
	attached map[string]bool
}

func newDetachedResizeRefuser() *detachedResizeRefuser {
	return &detachedResizeRefuser{fakeVolumeProvider: newFakeVolumeProvider(), attached: map[string]bool{}}
}

func (p *detachedResizeRefuser) Attach(ctx context.Context, id string, node bscommon.NodeID) (bscommon.AttachInfo, error) {
	info, err := p.fakeVolumeProvider.Attach(ctx, id, node)
	if err == nil {
		p.attached[id] = true
	}
	return info, err
}

func (p *detachedResizeRefuser) Detach(ctx context.Context, id string, node bscommon.NodeID) error {
	err := p.fakeVolumeProvider.Detach(ctx, id, node)
	if err == nil {
		delete(p.attached, id)
	}
	return err
}

func (p *detachedResizeRefuser) Resize(ctx context.Context, id string, n int64) error {
	if !p.attached[id] {
		_ = p.record("resize-refused", id)
		return ErrVolumeNotAttached
	}
	return p.fakeVolumeProvider.Resize(ctx, id, n)
}

func callIndex(calls []string, call string) int {
	for i, c := range calls {
		if c == call {
			return i
		}
	}
	return -1
}

func TestVolumeAttacherGrowsADetachedRefusingVolumeAfterAttach(t *testing.T) {
	ctx := context.Background()
	p := newDetachedResizeRefuser()
	p.sizes["data"] = 1 << 20
	a := newTestAttacher(t, p)
	claim := VolumeClaim{Name: "data", SizeBytes: 1 << 30, FSType: "ext4", Target: "/data"}
	vols, err := a.Attach(ctx, []VolumeClaim{claim})
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(vols) != 1 || vols[0].HostPath == "" {
		t.Fatalf("vols = %+v, want data mounted", vols)
	}
	if got := p.sizes["data"]; got != 1<<30 {
		t.Errorf("size = %d, want %d", got, 1<<30)
	}
	attach, resize, mount := callIndex(p.calls, "attach data"), callIndex(p.calls, "resize data"), callIndex(p.calls, "mount data")
	if attach < 0 || resize < attach || mount < resize {
		t.Errorf("calls = %v, want the grow after attach and before mount", p.calls)
	}
}

func TestVolumeAttacherRollsBackAFailedGrowAfterAttach(t *testing.T) {
	ctx := context.Background()
	p := newDetachedResizeRefuser()
	p.sizes["data"] = 1 << 20
	p.failOn["resize data"] = errors.New("grow failed")
	a := newTestAttacher(t, p)
	claim := VolumeClaim{Name: "data", SizeBytes: 1 << 30, FSType: "ext4", Target: "/data"}
	vols, held, err := a.attach(ctx, []VolumeClaim{claim})
	if err == nil || !strings.Contains(err.Error(), "grow failed") {
		t.Fatalf("attach = %v, want the grow error", err)
	}
	if len(vols) != 0 || len(held) != 0 {
		t.Errorf("vols = %+v, held = %+v, want none", vols, held)
	}
	resize, detach := callIndex(p.calls, "resize data"), callIndex(p.calls, "detach data")
	if resize < 0 || detach < resize {
		t.Errorf("calls = %v, want detach data after the failed grow", p.calls)
	}
	if countCalls(p.calls, "mount data") != 0 {
		t.Errorf("calls = %v, want no mount", p.calls)
	}
}
