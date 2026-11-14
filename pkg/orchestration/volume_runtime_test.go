package orchestration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// launchRecorder is a ContainerRuntime that records the specs it is handed
// and the containers it is asked to stop or remove.
type launchRecorder struct {
	ContainerRuntime // nil: only the methods below are used

	mu      sync.Mutex
	specs   []*ContainerSpec
	runErr  error
	stopped []string
	removed []string
}

func (r *launchRecorder) Run(_ context.Context, spec *ContainerSpec) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs = append(r.specs, spec)
	if r.runErr != nil {
		return "", r.runErr
	}
	return "ctr-" + spec.Name, nil
}

func (r *launchRecorder) RunInteractive(_ context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs = append(r.specs, spec)
	if r.runErr != nil {
		return nil, r.runErr
	}
	return &InteractiveProcess{ContainerID: "ctr-" + spec.Name}, nil
}

func (r *launchRecorder) Stop(_ context.Context, id string) error {
	r.stopped = append(r.stopped, id)
	return nil
}

func (r *launchRecorder) Remove(_ context.Context, id string) error {
	r.removed = append(r.removed, id)
	return nil
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
