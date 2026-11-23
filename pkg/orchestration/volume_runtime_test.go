package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// launchRecorder is a ContainerRuntime that records the specs it is handed
// and the containers it is asked to stop or remove.
type launchRecorder struct {
	ContainerRuntime // nil: only the methods below are used

	mu        sync.Mutex
	specs     []*ContainerSpec
	runErr    error
	stopErr   error  // returned by Stop after recording
	removeErr error  // returned by Remove after recording
	onRun     func() // called first in Run and RunInteractive when set
	stopped   []string
	removed   []string
}

func (r *launchRecorder) Run(_ context.Context, spec *ContainerSpec) (string, error) {
	if r.onRun != nil {
		r.onRun()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs = append(r.specs, spec)
	if r.runErr != nil {
		return "", r.runErr
	}
	return "ctr-" + spec.Name, nil
}

func (r *launchRecorder) RunInteractive(_ context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	if r.onRun != nil {
		r.onRun()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs = append(r.specs, spec)
	if r.runErr != nil {
		return nil, r.runErr
	}
	return &InteractiveProcess{ContainerID: "ctr-" + spec.Name}, nil
}

func (r *launchRecorder) Stop(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = append(r.stopped, id)
	return r.stopErr
}

func (r *launchRecorder) Remove(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, id)
	return r.removeErr
}

func newTestVolumeRuntime(t *testing.T) (*VolumeRuntime, *launchRecorder, *fakeVolumeProvider, string) {
	t.Helper()
	inner := &launchRecorder{}
	prov := newFakeVolumeProvider()
	staging := t.TempDir()
	att, err := NewVolumeAttacher(prov, "node-1", staging)
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := NewVolumeRuntime(inner, att)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	t.Cleanup(func() { _ = vr.Close() })
	return vr, inner, prov, staging
}

func volumeSpec() *ContainerSpec {
	return &ContainerSpec{
		Name:   "svc",
		Image:  "registry.example.com/svc:1",
		Mounts: []ContainerMount{{Source: "/etc/svc", Target: "/etc/svc", ReadOnly: true}},
		Volumes: []VolumeClaim{
			{Name: "data", SizeBytes: 1 << 30, FSType: "ext4", Target: "/var/lib/svc"},
			{Name: "logs", SizeBytes: 1 << 20, FSType: "xfs", Target: "/var/log/svc", ReadOnly: true},
		},
	}
}

func countCalls(calls []string, prefix string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestNewVolumeRuntimeRejectsNil(t *testing.T) {
	att, _ := NewVolumeAttacher(newFakeVolumeProvider(), "n", "/run/v")
	if _, err := NewVolumeRuntime(nil, att); !errors.Is(err, ErrVolumeInvalidArgument) {
		t.Errorf("nil runtime: err = %v", err)
	}
	if _, err := NewVolumeRuntime(&launchRecorder{}, nil); !errors.Is(err, ErrVolumeInvalidArgument) {
		t.Errorf("nil attacher: err = %v", err)
	}
}

// A spec carrying volumes is attached before the container runs; the runtime
// it reaches has the volumes resolved into bind mounts after its own mounts.
func TestVolumeRuntimeAttachesBeforeRun(t *testing.T) {
	vr, inner, prov, staging := newTestVolumeRuntime(t)
	spec := volumeSpec()
	id, err := vr.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if id != "ctr-svc" {
		t.Errorf("id = %q", id)
	}
	if got := countCalls(prov.calls, "mount "); got != 2 {
		t.Errorf("mounts = %d, want 2 (calls %v)", got, prov.calls)
	}
	ran := inner.specs[0]
	if len(ran.Volumes) != 0 {
		t.Errorf("inner spec still carries %d unresolved volumes", len(ran.Volumes))
	}
	want := []ContainerMount{
		{Source: "/etc/svc", Target: "/etc/svc", ReadOnly: true},
		{Source: staging + "/data", Target: "/var/lib/svc", Type: MountTypeBind},
		{Source: staging + "/logs", Target: "/var/log/svc", ReadOnly: true, Type: MountTypeBind},
	}
	if !reflect.DeepEqual(ran.Mounts, want) {
		t.Errorf("mounts = %+v\nwant %+v", ran.Mounts, want)
	}
	if len(spec.Mounts) != 1 || len(spec.Volumes) != 2 {
		t.Errorf("caller's spec was modified: %d mounts, %d volumes", len(spec.Mounts), len(spec.Volumes))
	}
	if got := vr.Attached("ctr-svc"); len(got) != 2 {
		t.Errorf("Attached = %d volumes, want 2", len(got))
	}
}

func TestVolumeRuntimeReleasesOnFailedStart(t *testing.T) {
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	inner.runErr = errors.New("boom")
	if _, err := vr.Run(context.Background(), volumeSpec()); err == nil {
		t.Fatal("Run = nil error, want the runtime's")
	}
	if got := countCalls(prov.calls, "detach "); got != 2 {
		t.Errorf("detaches = %d, want 2 (calls %v)", got, prov.calls)
	}
	if _, err := vr.RunInteractive(context.Background(), volumeSpec()); err == nil {
		t.Fatal("RunInteractive = nil error, want the runtime's")
	}
	if got := countCalls(prov.calls, "detach "); got != 4 {
		t.Errorf("detaches = %d, want 4 after the interactive failure", got)
	}
}

func TestVolumeRuntimeReleasesOnStopAndRemove(t *testing.T) {
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	ctx := context.Background()
	id, err := vr.Run(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := vr.Stop(ctx, id); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := countCalls(prov.calls, "unmount "); got != 2 {
		t.Errorf("unmounts after Stop = %d, want 2", got)
	}
	if err := vr.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 2 {
		t.Errorf("detaches = %d, want 2 (released once)", got)
	}
	if !reflect.DeepEqual(inner.stopped, []string{id}) || !reflect.DeepEqual(inner.removed, []string{id}) {
		t.Errorf("inner stop/remove = %v/%v", inner.stopped, inner.removed)
	}

	p, err := vr.RunInteractive(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	if err := vr.Remove(ctx, p.ContainerID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 4 {
		t.Errorf("detaches = %d, want 4", got)
	}
}

func TestVolumeRuntimePassesSpecsWithoutVolumes(t *testing.T) {
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	spec := &ContainerSpec{Name: "plain", Image: "registry.example.com/p:1"}
	if _, err := vr.Run(context.Background(), spec); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if inner.specs[0] != spec || len(prov.calls) != 0 {
		t.Errorf("a spec without volumes was not passed through untouched (calls %v)", prov.calls)
	}
}

func rawSpec(class string) *ContainerSpec {
	return &ContainerSpec{Name: "nbd", Image: "registry.example.com/nbd:1", PrivilegeClass: class,
		Volumes: []VolumeClaim{
			{Name: "root", SizeBytes: 1 << 30, Target: "/dev/vol/root"},
			{Name: "seed", SizeBytes: 1 << 20, Target: "/dev/vol/seed", ReadOnly: true},
		}}
}

// A raw claim outside the broker class is refused before the provider or the
// runtime sees it.
func TestVolumeRuntimeRefusesRawClaimsOutsideBroker(t *testing.T) {
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	if _, err := vr.Run(context.Background(), rawSpec("")); !errors.Is(err, ErrDevicePassthroughDenied) {
		t.Errorf("Run(raw, ordinary class) = %v, want ErrDevicePassthroughDenied", err)
	}
	if len(inner.specs) != 0 || len(prov.calls) != 0 {
		t.Errorf("a refused raw claim reached the provider or runtime")
	}
}

// A broker spec's raw claims are attached and handed to the runtime as
// devices at their targets, and the resolved spec validates.
func TestVolumeRuntimePassesRawClaimsToBroker(t *testing.T) {
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	id, err := vr.Run(context.Background(), rawSpec(PrivilegeClassBroker))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := countCalls(prov.calls, "mount "); got != 0 {
		t.Errorf("raw volumes were mounted: %v", prov.calls)
	}
	vols := vr.Attached(id)
	if len(vols) != 2 {
		t.Fatalf("Attached = %d volumes, want 2", len(vols))
	}
	ran := inner.specs[0]
	want := []ContainerDevice{
		{HostPath: vols[0].DevicePath, ContainerPath: "/dev/vol/root", Permissions: "rw"},
		{HostPath: vols[1].DevicePath, ContainerPath: "/dev/vol/seed", Permissions: "r"},
	}
	if !reflect.DeepEqual(ran.Devices, want) || len(ran.Mounts) != 0 {
		t.Errorf("devices = %+v, mounts = %+v; want devices %+v", ran.Devices, ran.Mounts, want)
	}
	if err := ran.Validate(); err != nil {
		t.Errorf("resolved broker spec: Validate = %v", err)
	}
}

// A runtime that is not a VolumeRuntime cannot honour volumes, so a spec that
// still carries them is invalid rather than launched without its storage.
func TestContainerSpecValidateRefusesUnresolvedVolumes(t *testing.T) {
	spec := volumeSpec()
	if err := spec.Validate(); !errors.Is(err, ErrVolumeUnsupported) {
		t.Errorf("Validate = %v, want ErrVolumeUnsupported", err)
	}
	spec.Volumes = nil
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate without volumes = %v", err)
	}
}

func wrapInVolumeRuntime(t *testing.T, inner ContainerRuntime) *VolumeRuntime {
	t.Helper()
	att, err := NewVolumeAttacher(newFakeVolumeProvider(), "node-1", t.TempDir())
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := NewVolumeRuntime(inner, att)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	return vr
}

func TestRuntimeAsWalksTheWrapperChain(t *testing.T) {
	inner := &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}}
	vr := wrapInVolumeRuntime(t, inner)
	if got, ok := RuntimeAs[pinVerifyingRuntime](vr); !ok || got != pinVerifyingRuntime(inner) {
		t.Errorf("RuntimeAs[pinVerifyingRuntime] = %v, %v; want the inner runtime", got, ok)
	}
	if _, ok := RuntimeAs[ImageLister](vr); ok {
		t.Error("RuntimeAs[ImageLister] found a capability neither runtime has")
	}
	if got, ok := RuntimeAs[*VolumeRuntime](vr); !ok || got != vr {
		t.Errorf("RuntimeAs[*VolumeRuntime] = %v, %v; want the wrapper itself", got, ok)
	}
	if _, ok := RuntimeAs[ImageLister](nil); ok {
		t.Error("RuntimeAs over nil reported a capability")
	}
}

func TestVolumeRuntimeKeepsTrustCapabilities(t *testing.T) {
	pinning := wrapInVolumeRuntime(t, &pinningContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}})
	if got := runtimeTrustCapabilities(pinning); got != (TrustCapabilities{DigestPinning: true}) {
		t.Errorf("pinning runtime behind the wrapper: %+v, want digest pinning", got)
	}
	attesting := &attestingContainerRuntime{mockContainerRuntime: &mockContainerRuntime{available: true}}
	if got := runtimeTrustCapabilities(wrapInVolumeRuntime(t, attesting)); got != attesting.TrustCapabilities() {
		t.Errorf("attesting runtime behind the wrapper: %+v, want %+v", got, attesting.TrustCapabilities())
	}
}

// A container that exited under --rm is already gone: its volumes are still
// released, the not-found error is kept, and a release failure is returned
// alone so a caller treating not-found as success still sees it.
func TestVolumeRuntimeReleasesAfterContainerAlreadyGone(t *testing.T) {
	ctx := context.Background()
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	id, err := vr.Run(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	inner.removeErr = &ContainerNotFoundError{ContainerID: id}
	var notFound *ContainerNotFoundError
	if err := vr.Remove(ctx, id); !errors.As(err, &notFound) {
		t.Errorf("Remove = %v, want the not-found error", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 2 {
		t.Errorf("detaches = %d, want 2 (calls %v)", got, prov.calls)
	}
	if got := vr.Attached(id); len(got) != 0 {
		t.Errorf("Attached = %v, want none", got)
	}

	p, err := vr.RunInteractive(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	inner.stopErr = &ContainerNotFoundError{ContainerID: p.ContainerID}
	if err := vr.Stop(ctx, p.ContainerID); !errors.As(err, &notFound) {
		t.Errorf("Stop = %v, want the not-found error", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 4 {
		t.Errorf("detaches = %d, want 4", got)
	}
	if got := vr.Attached(p.ContainerID); len(got) != 0 {
		t.Errorf("Attached = %v, want none", got)
	}

	spec := volumeSpec()
	spec.Name = "third"
	id, err = vr.Run(ctx, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	busy := errors.New("busy")
	inner.removeErr = &ContainerNotFoundError{ContainerID: id}
	prov.failOn["detach data"] = busy
	err = vr.Remove(ctx, id)
	if !errors.Is(err, busy) || errors.As(err, &notFound) {
		t.Errorf("Remove = %v, want the release failure alone", err)
	}
}

func TestVolumeRuntimeRetriesAFailedRelease(t *testing.T) {
	ctx := context.Background()
	vr, _, prov, _ := newTestVolumeRuntime(t)
	id, err := vr.Run(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	busy := errors.New("busy")
	prov.failOn["unmount data"] = busy
	if err := vr.Stop(ctx, id); !errors.Is(err, busy) {
		t.Fatalf("Stop = %v, want busy", err)
	}
	if held := vr.Attached(id); len(held) != 1 || held[0].VolumeID != "data" {
		t.Fatalf("Attached after a failed release = %+v, want only data", held)
	}
	delete(prov.failOn, "unmount data")
	if err := vr.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := countCalls(prov.calls, "unmount data"); got != 2 {
		t.Errorf("unmount data = %d, want 2", got)
	}
	if got := countCalls(prov.calls, "detach logs"); got != 1 {
		t.Errorf("detach logs = %d, want 1", got)
	}
	if held := vr.Attached(id); len(held) != 0 {
		t.Errorf("Attached = %+v, want none", held)
	}
}

// gateUnmountProvider holds the first Unmount until the gate opens.
type gateUnmountProvider struct {
	*fakeVolumeProvider
	entered chan struct{}
	gate    chan struct{}
}

func (p gateUnmountProvider) Unmount(ctx context.Context, id, host string) error {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-p.gate
	return p.fakeVolumeProvider.Unmount(ctx, id, host)
}

func TestVolumeRuntimeConcurrentTeardownReleasesOnce(t *testing.T) {
	ctx := context.Background()
	prov := newFakeVolumeProvider()
	gp := gateUnmountProvider{prov, make(chan struct{}, 1), make(chan struct{})}
	att, err := NewVolumeAttacher(gp, "node-1", t.TempDir())
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := NewVolumeRuntime(&launchRecorder{}, att)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	id, err := vr.Run(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = vr.Stop(ctx, id) }()
	go func() { defer wg.Done(); _ = vr.Remove(ctx, id) }()
	<-gp.entered
	close(gp.gate)
	wg.Wait()
	if got := countCalls(prov.calls, "unmount "); got != 2 {
		t.Errorf("unmounts = %d, want 2 (calls %v)", got, prov.calls)
	}
	if got := countCalls(prov.calls, "detach "); got != 2 {
		t.Errorf("detaches = %d, want 2 (calls %v)", got, prov.calls)
	}
}

func TestVolumeRuntimeRollbackSurvivesCancelledLaunch(t *testing.T) {
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inner.onRun = cancel
	inner.runErr = context.Canceled
	_, err := vr.Run(ctx, volumeSpec())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), "release volumes after failed start") {
		t.Errorf("the rollback failed: %v", err)
	}
	if got := countCalls(prov.calls, "unmount "); got != 2 {
		t.Errorf("unmounts = %d, want 2 (calls %v)", got, prov.calls)
	}
	if got := countCalls(prov.calls, "detach "); got != 2 {
		t.Errorf("detaches = %d, want 2 (calls %v)", got, prov.calls)
	}
	if len(vr.pending) != 0 {
		t.Errorf("pending = %+v, want none", vr.pending)
	}
}

func TestVolumeRuntimeRetriesPendingRollback(t *testing.T) {
	ctx := context.Background()
	vr, inner, prov, _ := newTestVolumeRuntime(t)
	inner.runErr = errors.New("boom")
	prov.failOn["detach data"] = errors.New("busy")
	if _, err := vr.Run(ctx, volumeSpec()); err == nil {
		t.Fatal("Run = nil error, want the runtime's")
	}
	if len(vr.pending) != 1 || vr.pending[0].VolumeID != "data" {
		t.Fatalf("pending = %+v, want data", vr.pending)
	}
	delete(prov.failOn, "detach data")
	inner.runErr = nil
	if _, err := vr.Run(ctx, volumeSpec()); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(vr.pending) != 0 {
		t.Errorf("pending = %+v, want none", vr.pending)
	}
	if got := countCalls(prov.calls, "detach data"); got != 2 {
		t.Errorf("detach data = %d, want 2 (calls %v)", got, prov.calls)
	}
}

// dataSpec is a second container claiming volumeSpec's "data" volume.
func dataSpec(name string) *ContainerSpec {
	return &ContainerSpec{
		Name:    name,
		Image:   "registry.example.com/other:1",
		Volumes: []VolumeClaim{{Name: "data", SizeBytes: 1 << 30, FSType: "ext4", Target: "/data"}},
	}
}

func TestVolumeRuntimeRefusesAVolumeAnotherContainerHolds(t *testing.T) {
	ctx := context.Background()
	vr, _, prov, _ := newTestVolumeRuntime(t)
	id, err := vr.Run(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	attaches, detaches := countCalls(prov.calls, "attach "), countCalls(prov.calls, "detach ")
	if _, err := vr.Run(ctx, dataSpec("b")); !errors.Is(err, ErrVolumeInUse) {
		t.Fatalf("second Run = %v, want ErrVolumeInUse", err)
	}
	if _, err := vr.RunInteractive(ctx, dataSpec("b")); !errors.Is(err, ErrVolumeInUse) {
		t.Fatalf("second RunInteractive = %v, want ErrVolumeInUse", err)
	}
	if a, d := countCalls(prov.calls, "attach "), countCalls(prov.calls, "detach "); a != attaches || d != detaches {
		t.Errorf("attach/detach = %d/%d, want %d/%d unchanged (calls %v)", a, d, attaches, detaches, prov.calls)
	}
	if err := vr.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := vr.Run(ctx, dataSpec("b")); err != nil {
		t.Fatalf("Run after the owner's Remove: %v", err)
	}
}

func TestVolumeRuntimeFailedLaunchFreesOrKeepsItsVolumes(t *testing.T) {
	ctx := context.Background()
	vr, inner, prov, _ := newTestVolumeRuntime(t)

	// A launch that fails in the inner Run releases its volumes and frees
	// their names.
	inner.runErr = errors.New("boom")
	if _, err := vr.Run(ctx, volumeSpec()); err == nil {
		t.Fatal("Run = nil error, want the runtime's")
	}
	inner.runErr = nil
	id, err := vr.Run(ctx, dataSpec("b"))
	if err != nil {
		t.Fatalf("Run after a failed launch: %v", err)
	}
	if err := vr.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// One whose rollback left "data" pending keeps it reserved until
	// retryPending releases it; "logs" is freed.
	inner.runErr = errors.New("boom")
	prov.failOn["detach data"] = errors.New("busy")
	if _, err := vr.Run(ctx, volumeSpec()); err == nil {
		t.Fatal("Run = nil error, want the runtime's")
	}
	inner.runErr = nil
	vr.mu.Lock()
	_, dataOwned := vr.owned["data"]
	_, logsOwned := vr.owned["logs"]
	vr.mu.Unlock()
	if !dataOwned || logsOwned {
		t.Fatalf("owned data/logs = %v/%v, want true/false", dataOwned, logsOwned)
	}
	if _, err := vr.Run(ctx, dataSpec("c")); !errors.Is(err, ErrVolumeInUse) {
		t.Fatalf("Run while data is pending = %v, want ErrVolumeInUse", err)
	}
	delete(prov.failOn, "detach data")
	if _, err := vr.Run(ctx, dataSpec("c")); err != nil {
		t.Fatalf("Run once pending is released: %v", err)
	}
}

func TestVolumeRuntimeReleaseContainerByName(t *testing.T) {
	ctx := context.Background()
	vr, _, prov, _ := newTestVolumeRuntime(t)
	p, err := vr.RunInteractive(ctx, volumeSpec())
	if err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	if err := vr.ReleaseContainer(ctx, "unknown", p.ContainerID); err != nil {
		t.Fatalf("ReleaseContainer: %v", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 2 {
		t.Fatalf("detaches = %d, want 2 (calls %v)", got, prov.calls)
	}
	if got := vr.Attached(p.ContainerID); len(got) != 0 {
		t.Errorf("Attached = %+v, want none", got)
	}

	// A Run record is released by Remove under the name as well as the ID.
	spec := volumeSpec()
	id, err := vr.Run(ctx, spec)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := vr.Remove(ctx, spec.Name); err != nil {
		t.Fatalf("Remove(name): %v", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 4 {
		t.Fatalf("detaches = %d, want 4 (calls %v)", got, prov.calls)
	}
	if got := vr.Attached(id); len(got) != 0 {
		t.Errorf("Attached(id) = %+v, want none", got)
	}
	if _, err := vr.Run(ctx, spec); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := vr.Remove(ctx, id); err != nil {
		t.Fatalf("Remove(id): %v", err)
	}
	if got := countCalls(prov.calls, "detach "); got != 6 {
		t.Fatalf("detaches = %d, want 6 (calls %v)", got, prov.calls)
	}
}

func readStateFile(t *testing.T, staging string) volumeState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(staging, VolumeStateFile))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st volumeState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("decode state file: %v", err)
	}
	return st
}

func TestVolumeRuntimeRecoversAttachmentsAfterRestart(t *testing.T) {
	ctx := context.Background()
	vr, inner, prov, staging := newTestVolumeRuntime(t)
	if _, err := vr.Run(ctx, volumeSpec()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st := readStateFile(t, staging); len(st.Volumes) != 2 || st.Volumes[0].Claim.Name != "data" || st.Volumes[1].Claim.Name != "logs" {
		t.Fatalf("state = %+v, want data and logs", st)
	}

	if err := vr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := NewVolumeRuntime(inner, vr.Attacher())
	if err != nil {
		t.Fatalf("NewVolumeRuntime after restart: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if _, err := restarted.Run(ctx, dataSpec("b")); !errors.Is(err, ErrVolumeInUse) {
		t.Fatalf("Run of a recovered volume = %v, want ErrVolumeInUse", err)
	}
	unmounts, detaches := countCalls(prov.calls, "unmount "), countCalls(prov.calls, "detach ")
	if err := restarted.ReleaseRecovered(ctx); err != nil {
		t.Fatalf("ReleaseRecovered: %v", err)
	}
	if u, d := countCalls(prov.calls, "unmount ")-unmounts, countCalls(prov.calls, "detach ")-detaches; u != 2 || d != 2 {
		t.Fatalf("unmounts/detaches = %d/%d, want 2/2 (calls %v)", u, d, prov.calls)
	}
	id, err := restarted.Run(ctx, dataSpec("b"))
	if err != nil {
		t.Fatalf("Run after ReleaseRecovered: %v", err)
	}
	if err := restarted.Remove(ctx, id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if st := readStateFile(t, staging); len(st.Volumes) != 0 {
		t.Errorf("state = %+v, want no volumes", st)
	}
}

func TestNewVolumeRuntimeRefusesAMalformedStateFile(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, VolumeStateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	att, err := NewVolumeAttacher(newFakeVolumeProvider(), "node-1", staging)
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	if _, err := NewVolumeRuntime(&launchRecorder{}, att); err == nil {
		t.Fatal("NewVolumeRuntime = nil error, want the state file's")
	}
}

// TestNewVolumeRuntimeRefusesALockedStagingDir: one runtime at a time owns a
// staging dir's attachment state.
func TestNewVolumeRuntimeRefusesALockedStagingDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the staging dir lock is a Linux flock")
	}
	first, _, _, _ := newTestVolumeRuntime(t)
	second, err := NewVolumeRuntime(&launchRecorder{}, first.Attacher())
	if !errors.Is(err, ErrVolumeStateLocked) || second != nil {
		t.Fatalf("second NewVolumeRuntime = %v, %v; want nil, ErrVolumeStateLocked", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	third, err := NewVolumeRuntime(&launchRecorder{}, first.Attacher())
	if err != nil {
		t.Fatalf("NewVolumeRuntime after Close: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
