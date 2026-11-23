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

package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// listingContainerRuntime is a container runtime that can enumerate its
// owned containers, which is what makes it reapable.
type listingContainerRuntime struct {
	fakeContainerRuntime

	mu         sync.Mutex
	containers []orchestration.OwnedContainer
	askedOwner string
	removed    []string
}

func (l *listingContainerRuntime) ListOwned(_ context.Context, owner string) ([]orchestration.OwnedContainer, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.askedOwner = owner
	return append([]orchestration.OwnedContainer(nil), l.containers...), nil
}

func (l *listingContainerRuntime) Remove(_ context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.removed = append(l.removed, id)
	return nil
}

// holdingBackend is a backend that holds exactly the sessions it names.
type holdingBackend struct {
	mockBackend
	held map[string]bool
}

func (b *holdingBackend) HoldsSession(id string) bool { return b.held[id] }

// TestRuntime_ContainerReaperSparesEverySessionAnyBackendHolds: the reaper
// claims by this daemon's work directory and asks EVERY backend — a session
// the second backend holds must not be reaped because the first does not.
func TestRuntime_ContainerReaperSparesEverySessionAnyBackendHolds(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	old := time.Now().Add(-time.Hour)
	rt := &listingContainerRuntime{containers: []orchestration.OwnedContainer{
		{ID: "held-by-b", CreatedAt: old, SessionID: "ses-b"},
		{ID: "orphan", CreatedAt: old, SessionID: "ses-gone"},
	}}
	rt.available = true
	runtime, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"a": &holdingBackend{held: map[string]bool{"ses-a": true}},
			"b": &holdingBackend{held: map[string]bool{"ses-b": true}},
		},
		ContainerRuntime: rt,
		Logger:           &testLogger{t: t},
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	reaper, err := runtime.ContainerReaper(time.Minute)
	if err != nil || reaper == nil {
		t.Fatalf("ContainerReaper = %v, %v", reaper, err)
	}
	sweep, err := reaper.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(sweep.Removed) != 1 || sweep.Removed[0] != "orphan" {
		t.Fatalf("removed %v, want only the orphan", sweep.Removed)
	}
	if rt.askedOwner != workDir {
		t.Fatalf("listed by owner %q, want the work directory", rt.askedOwner)
	}
}

// TestRuntime_ContainerReaperIsNilWithoutAnEnumerableRuntime: a daemon that
// launches no containers, or cannot list them, has nothing to reap.
func TestRuntime_ContainerReaperIsNilWithoutAnEnumerableRuntime(t *testing.T) {
	t.Parallel()
	plain := &fakeContainerRuntime{available: true}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: plain,
		Logger:           &testLogger{t: t},
		WorkDir:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if reaper, err := runtime.ContainerReaper(time.Minute); reaper != nil || err != nil {
		t.Fatalf("ContainerReaper = %v, %v; want nil, nil", reaper, err)
	}
	if runtime.HoldsSession("anything") {
		t.Fatal("a backend that cannot hold sessions reported one held")
	}
}

// TestNewRuntime_RefusesAGoCacheTheMountPolicyReaches: a cache a request's
// grant could reach is one another tenant could mount.
func TestNewRuntime_RefusesAGoCacheTheMountPolicyReaches(t *testing.T) {
	t.Parallel()
	projects := filepath.Join(t.TempDir(), "projects")
	_, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: &fakeContainerRuntime{available: true},
		Logger:           &testLogger{t: t},
		WorkDir:          t.TempDir(),
		MountPolicy:      &orchestration.MountPolicy{ProjectsRoot: projects},
		GoBuildCache:     orchestration.GoBuildCache{Root: filepath.Join(projects, "cache")},
	})
	if !errors.Is(err, orchestration.ErrGoBuildCache) {
		t.Fatalf("NewRuntime = %v, want ErrGoBuildCache", err)
	}
}

// TestConfig_GoBuildCacheDefaultsUnderTheWorkDir covers the default, the
// override, and the refusal of a cache the allowlist reaches.
func TestConfig_GoBuildCacheDefaultsUnderTheWorkDir(t *testing.T) {
	t.Parallel()
	cfg := &Config{WorkDir: "/var/lib/quicbaas-agent"}
	if got := cfg.GoBuildCache().Root; got != "/var/lib/quicbaas-agent/cache/go" {
		t.Fatalf("default root = %q", got)
	}
	cfg.GoCacheDir = " /srv/go-cache/ "
	if got := cfg.GoBuildCache().Root; got != "/srv/go-cache" {
		t.Fatalf("override root = %q", got)
	}
	if (&Config{}).GoBuildCache().Enabled() {
		t.Fatal("a config with no work dir enabled a cache")
	}

	reached := &Config{WorkDir: "/var/lib/quicbaas-agent", AllowedMountRoots: []string{"/var/lib/quicbaas-agent"}}
	if err := reached.GoBuildCache().Validate(reached.MountPolicy()); !errors.Is(err, orchestration.ErrGoBuildCache) {
		t.Fatalf("Validate = %v, want the allowlist overlap refused", err)
	}
}

// capableContainerRuntime adds the orphan sweep and image listing to a
// reapable runtime.
type capableContainerRuntime struct {
	listingContainerRuntime
	swept  []string
	images []string
}

func (c *capableContainerRuntime) SweepOwned(_ context.Context, owner string) (orchestration.ContainerSweep, error) {
	c.swept = append(c.swept, owner)
	return orchestration.ContainerSweep{Owner: owner}, nil
}

func (c *capableContainerRuntime) ListImages(context.Context) ([]string, error) {
	return c.images, nil
}

// TestRuntime_VolumeRuntimeKeepsOptionalCapabilities: wrapped in a
// VolumeRuntime, the node's runtime still lists images, sweeps orphans and is
// reaped, and the reaper removes through the wrapper. Not parallel:
// volumes_test.go stubs package variables.
func TestRuntime_VolumeRuntimeKeepsOptionalCapabilities(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()
	inner := &capableContainerRuntime{images: []string{"img:1"}}
	inner.available = true
	inner.containers = []orchestration.OwnedContainer{{ID: "orphan", CreatedAt: time.Now().Add(-time.Hour), SessionID: "ses-gone"}}
	att, err := orchestration.NewVolumeAttacher(nopVolumeProvider{}, "n", t.TempDir())
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := orchestration.NewVolumeRuntime(inner, att)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: vr,
		Logger:           &testLogger{t: t},
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if got := runtime.Images(ctx); len(got) != 1 || got[0] != "img:1" {
		t.Errorf("Images = %v, want [img:1]", got)
	}
	if _, err := runtime.SweepOrphanContainers(ctx); err != nil {
		t.Fatalf("SweepOrphanContainers: %v", err)
	}
	if len(inner.swept) != 1 || inner.swept[0] != workDir {
		t.Errorf("swept owners = %v, want the work directory", inner.swept)
	}
	reaper, err := runtime.ContainerReaper(time.Minute)
	if err != nil || reaper == nil {
		t.Fatalf("ContainerReaper = %v, %v; want a reaper", reaper, err)
	}
	sweep, err := reaper.Reap(ctx)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(sweep.Removed) != 1 || sweep.Removed[0] != "orphan" {
		t.Errorf("reaped %v, want the orphan", sweep.Removed)
	}
	if len(inner.removed) != 1 || inner.removed[0] != "orphan" {
		t.Errorf("inner removed %v, want the orphan", inner.removed)
	}
}

// nameIDContainerRuntime returns the container's name from RunInteractive,
// as the exec runtime does, while its listing reports full container IDs.
type nameIDContainerRuntime struct {
	capableContainerRuntime
}

func (n *nameIDContainerRuntime) RunInteractive(_ context.Context, spec *orchestration.ContainerSpec) (*orchestration.InteractiveProcess, error) {
	return &orchestration.InteractiveProcess{ContainerID: spec.Name}, nil
}

// detachCountingProvider is a provider that provisions, attaches and mounts
// anything, and counts Detach.
type detachCountingProvider struct {
	nopVolumeProvider

	mu       sync.Mutex
	detaches int
	// failDetach fails that many Detach calls before they succeed.
	failDetach int
}

func (p *detachCountingProvider) Status(context.Context, string) (orchestration.VolumeStatus, error) {
	return orchestration.VolumeStatus{}, orchestration.ErrVolumeNotFound
}

func (p *detachCountingProvider) Provision(_ context.Context, spec orchestration.VolumeSpec) (orchestration.Volume, error) {
	return orchestration.Volume{ID: spec.Name, Spec: spec}, nil
}

func (p *detachCountingProvider) Attach(_ context.Context, id string, node orchestration.VolumeNodeID) (orchestration.VolumeAttachInfo, error) {
	return orchestration.VolumeAttachInfo{DevicePath: "/dev/fake/" + id, Node: node}, nil
}

func (p *detachCountingProvider) Mount(context.Context, string, string, orchestration.VolumeMountOptions) error {
	return nil
}

func (p *detachCountingProvider) Unmount(context.Context, string, string) error { return nil }

func (p *detachCountingProvider) Detach(context.Context, string, orchestration.VolumeNodeID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failDetach > 0 {
		p.failDetach--
		return errors.New("detach: device busy")
	}
	p.detaches++
	return nil
}

func (p *detachCountingProvider) detachCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.detaches
}

func volumeSvcSpec() *orchestration.ContainerSpec {
	return &orchestration.ContainerSpec{
		Name:    "svc",
		Image:   "img:1",
		Volumes: []orchestration.VolumeClaim{{Name: "data", SizeBytes: 1 << 20, FSType: "ext4", Target: "/data"}},
	}
}

// TestRuntime_ReaperReleasesVolumesOfAnInteractiveContainer: the reaper lists
// full container IDs, an interactive launch files its volumes under the
// container's name, and reaping it still releases them. Not parallel:
// volumes_test.go stubs package variables.
func TestRuntime_ReaperReleasesVolumesOfAnInteractiveContainer(t *testing.T) {
	ctx := context.Background()
	inner := &nameIDContainerRuntime{}
	inner.available = true
	inner.containers = []orchestration.OwnedContainer{{
		ID: "full-id-1", Name: "svc", CreatedAt: time.Now().Add(-time.Hour), SessionID: "ses-gone",
	}}
	prov := &detachCountingProvider{}
	att, err := orchestration.NewVolumeAttacher(prov, "n", t.TempDir())
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := orchestration.NewVolumeRuntime(inner, att)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	if _, err := vr.RunInteractive(ctx, volumeSvcSpec()); err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: vr,
		Logger:           &testLogger{t: t},
		WorkDir:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	reaper, err := runtime.ContainerReaper(time.Minute)
	if err != nil || reaper == nil {
		t.Fatalf("ContainerReaper = %v, %v; want a reaper", reaper, err)
	}
	if _, err := reaper.Reap(ctx); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if len(inner.removed) != 1 || inner.removed[0] != "full-id-1" {
		t.Errorf("inner removed %v, want full-id-1", inner.removed)
	}
	if got := prov.detachCount(); got != 1 {
		t.Errorf("detaches = %d, want 1", got)
	}
	if got := vr.Attached("svc"); len(got) != 0 {
		t.Errorf("Attached(svc) = %+v, want none", got)
	}
}

// TestRuntime_ReaperRetriesPendingVolumeReleases: a reaped container whose
// volume release failed is gone from the next listing, and the next reaper
// pass still releases its volumes. Not parallel: volumes_test.go stubs package
// variables.
func TestRuntime_ReaperRetriesPendingVolumeReleases(t *testing.T) {
	ctx := context.Background()
	inner := &nameIDContainerRuntime{}
	inner.available = true
	inner.containers = []orchestration.OwnedContainer{{
		ID: "full-id-1", Name: "svc", CreatedAt: time.Now().Add(-time.Hour), SessionID: "ses-gone",
	}}
	prov := &detachCountingProvider{failDetach: 1}
	att, err := orchestration.NewVolumeAttacher(prov, "n", t.TempDir())
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := orchestration.NewVolumeRuntime(inner, att)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	t.Cleanup(func() { _ = vr.Close() })
	if _, err := vr.RunInteractive(ctx, volumeSvcSpec()); err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: vr,
		Logger:           &testLogger{t: t},
		WorkDir:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	reaper, err := runtime.ContainerReaper(time.Minute)
	if err != nil || reaper == nil {
		t.Fatalf("ContainerReaper = %v, %v; want a reaper", reaper, err)
	}
	if _, err := reaper.Reap(ctx); err == nil {
		t.Fatal("first Reap = nil error, want the failed release")
	}
	if len(inner.removed) != 1 || inner.removed[0] != "full-id-1" {
		t.Fatalf("inner removed %v, want full-id-1", inner.removed)
	}
	if got := prov.detachCount(); got != 0 {
		t.Fatalf("detaches after the failed release = %d, want 0", got)
	}

	inner.mu.Lock()
	inner.containers = nil // the reaper removed it
	inner.mu.Unlock()
	if _, err := reaper.Reap(ctx); err != nil {
		t.Fatalf("second Reap: %v", err)
	}
	if got := prov.detachCount(); got != 1 {
		t.Errorf("detaches after the second pass = %d, want 1", got)
	}
	if _, err := vr.RunInteractive(ctx, volumeSvcSpec()); err != nil {
		t.Errorf("RunInteractive after the retry: %v", err)
	}
}

// TestRuntime_SweepReleasesRecoveredVolumes: a node restarted while a
// container held volumes recovers them from the staging dir and releases them
// after the startup sweep. Not parallel: volumes_test.go stubs package
// variables.
func TestRuntime_SweepReleasesRecoveredVolumes(t *testing.T) {
	ctx := context.Background()
	inner := &capableContainerRuntime{}
	inner.available = true
	prov := &detachCountingProvider{}
	staging := t.TempDir()
	att, err := orchestration.NewVolumeAttacher(prov, "n", staging)
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	before, err := orchestration.NewVolumeRuntime(inner, att)
	if err != nil {
		t.Fatalf("NewVolumeRuntime: %v", err)
	}
	if _, err := before.Run(ctx, volumeSvcSpec()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if err := before.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	att2, err := orchestration.NewVolumeAttacher(prov, "n", staging)
	if err != nil {
		t.Fatalf("NewVolumeAttacher: %v", err)
	}
	vr, err := orchestration.NewVolumeRuntime(inner, att2)
	if err != nil {
		t.Fatalf("NewVolumeRuntime after restart: %v", err)
	}
	if _, err := vr.Run(ctx, volumeSvcSpec()); !errors.Is(err, orchestration.ErrVolumeInUse) {
		t.Fatalf("Run before the sweep = %v, want ErrVolumeInUse", err)
	}
	workDir := t.TempDir()
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: vr,
		Logger:           &testLogger{t: t},
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if got := prov.detachCount(); got != 0 {
		t.Fatalf("detaches before the sweep = %d, want 0", got)
	}
	if _, err := runtime.SweepOrphanContainers(ctx); err != nil {
		t.Fatalf("SweepOrphanContainers: %v", err)
	}
	if len(inner.swept) != 1 || inner.swept[0] != workDir {
		t.Errorf("swept owners = %v, want the work directory", inner.swept)
	}
	if got := prov.detachCount(); got != 1 {
		t.Errorf("detaches after the sweep = %d, want 1", got)
	}
	if _, err := vr.Run(ctx, volumeSvcSpec()); err != nil {
		t.Errorf("Run after the sweep: %v", err)
	}
}
