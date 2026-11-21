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
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// legacySweepRuntime is a sweeping runtime that refuses the owners in
// failing, so one legacy owner's failure can be told from the next's.
type legacySweepRuntime struct {
	*sweepingContainerRuntime
	failing map[string]error
}

func (l *legacySweepRuntime) SweepOwned(ctx context.Context, owner string) (orchestration.ContainerSweep, error) {
	if err, ok := l.failing[owner]; ok {
		l.sweepingContainerRuntime.mu.Lock()
		l.sweepingContainerRuntime.asked = append(l.sweepingContainerRuntime.asked, owner)
		l.sweepingContainerRuntime.mu.Unlock()
		return orchestration.ContainerSweep{}, err
	}
	return l.sweepingContainerRuntime.SweepOwned(ctx, owner)
}

var _ orchestration.ContainerOwnerSweeper = (*legacySweepRuntime)(nil)

// newLegacyTestRuntime builds a Runtime over rt with workDir as its own owner.
func newLegacyTestRuntime(t *testing.T, rt orchestration.ContainerRuntime, workDir string) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: rt,
		Logger:           &testLogger{t: t},
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return runtime
}

func TestRuntime_SweepLegacyContainersRemovesOnlyTheNamedOwners(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	dataDir := t.TempDir()
	orchestrator := "orchestrator:" + dataDir
	rt := newSweepingRuntime(map[string][]string{
		orchestrator:    {"o1"},
		"relative/node": {"e1"},
		workDir:         {"own"},
		"/other":        {"keep"},
	})
	runtime := newLegacyTestRuntime(t, rt, workDir)

	sweeps := runtime.SweepLegacyContainers(context.Background(), []string{orchestrator, "relative/node"})

	if len(sweeps) != 2 {
		t.Fatalf("got %d sweeps, want 2: %+v", len(sweeps), sweeps)
	}
	if !reflect.DeepEqual(sweeps[0].Removed, []string{"o1"}) || !reflect.DeepEqual(sweeps[1].Removed, []string{"e1"}) {
		t.Fatalf("sweeps removed %v and %v, want [o1] and [e1]", sweeps[0].Removed, sweeps[1].Removed)
	}
	if want := []string{orchestrator, "relative/node"}; !reflect.DeepEqual(rt.asked, want) {
		t.Fatalf("swept owners %v, want %v", rt.asked, want)
	}
	if _, ok := rt.owned[workDir]; !ok {
		t.Fatal("the daemon's own containers were swept as legacy ones")
	}
	if _, ok := rt.owned["/other"]; !ok {
		t.Fatal("an owner nobody named was swept")
	}
}

func TestRuntime_SweepLegacyContainersSkipsBlankAndOwnOwner(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	rt := newSweepingRuntime(map[string][]string{workDir: {"own"}})
	runtime := newLegacyTestRuntime(t, rt, workDir)

	if sweeps := runtime.SweepLegacyContainers(context.Background(), []string{"", "  ", workDir}); sweeps != nil {
		t.Fatalf("got sweeps %+v, want none", sweeps)
	}
	if len(rt.asked) != 0 {
		t.Fatalf("swept owners %v, want none", rt.asked)
	}
}

func TestRuntime_SweepLegacyContainersLogsAFailureAndGoesOn(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	rt := &legacySweepRuntime{
		sweepingContainerRuntime: newSweepingRuntime(map[string][]string{"/legacy/node": {"l1"}}),
		failing: map[string]error{
			"orchestrator:/gone": &orchestration.ContainerSweepError{Owner: "orchestrator:/gone", Cause: errors.New("cannot connect")},
		},
	}
	runtime := newLegacyTestRuntime(t, rt, workDir)

	sweeps := runtime.SweepLegacyContainers(context.Background(), []string{"orchestrator:/gone", "/legacy/node"})

	if want := []string{"orchestrator:/gone", "/legacy/node"}; !reflect.DeepEqual(rt.asked, want) {
		t.Fatalf("swept owners %v, want %v", rt.asked, want)
	}
	if len(sweeps) != 1 || !reflect.DeepEqual(sweeps[0].Removed, []string{"l1"}) {
		t.Fatalf("got sweeps %+v, want one removing [l1]", sweeps)
	}
}

func TestRuntime_SweepLegacyContainersNoSweeperIsANoOp(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	if sweeps := newLegacyTestRuntime(t, nil, workDir).SweepLegacyContainers(context.Background(), []string{"/legacy"}); sweeps != nil {
		t.Fatalf("no container runtime: got sweeps %+v, want none", sweeps)
	}
	plain := &fakeContainerRuntime{available: true}
	if sweeps := newLegacyTestRuntime(t, plain, workDir).SweepLegacyContainers(context.Background(), []string{"/legacy"}); sweeps != nil {
		t.Fatalf("non-sweeping runtime: got sweeps %+v, want none", sweeps)
	}
	rt := newSweepingRuntime(map[string][]string{"/legacy": {"l1"}})
	if sweeps := newLegacyTestRuntime(t, rt, workDir).SweepLegacyContainers(context.Background(), nil); sweeps != nil {
		t.Fatalf("no owners: got sweeps %+v, want none", sweeps)
	}
	if len(rt.asked) != 0 {
		t.Fatalf("swept owners %v with none configured", rt.asked)
	}
}

func TestDaemon_StartSweepsTheLegacyOwnersOnce(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.LegacyContainerOwners = []string{"orchestrator:/var/lib/quicbaas/data", "/var/lib/quicbaas/node"}
	rt := newSweepingRuntime(map[string][]string{
		"orchestrator:/var/lib/quicbaas/data": {"quicbaas-acp-orch"},
		"/var/lib/quicbaas/node":              {"quicbaas-acp-node"},
	})
	runtime := newLegacyTestRuntime(t, rt, cfg.WorkDir)

	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     runtime,
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "agent-test", TenantID: cfg.TenantID}},
		SpawnServer: &orderedSpawnServer{runtime: rt},
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if want := []string{cfg.WorkDir, "orchestrator:/var/lib/quicbaas/data", "/var/lib/quicbaas/node"}; !reflect.DeepEqual(rt.asked, want) {
		t.Fatalf("startup swept %v, want %v", rt.asked, want)
	}
	for _, owner := range cfg.LegacyContainerOwners {
		if _, ok := rt.owned[owner]; ok {
			t.Fatalf("legacy owner %q's containers were not swept", owner)
		}
	}
}

func TestDaemon_StartGoesOnWhenALegacySweepFails(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.LegacyContainerOwners = []string{"orchestrator:/gone"}
	rt := &legacySweepRuntime{
		sweepingContainerRuntime: newSweepingRuntime(map[string][]string{}),
		failing: map[string]error{
			"orchestrator:/gone": &orchestration.ContainerSweepError{Owner: "orchestrator:/gone", Cause: errors.New("cannot connect")},
		},
	}
	runtime := newLegacyTestRuntime(t, rt, cfg.WorkDir)
	spawnServer := &fakeSpawnServer{}

	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     runtime,
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "agent-test", TenantID: cfg.TenantID}},
		SpawnServer: spawnServer,
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := d.State(); got != "running" {
		t.Fatalf("state %q, want running", got)
	}
	if !spawnServer.started.Load() {
		t.Fatal("the spawn server did not start after a failed legacy sweep")
	}
}

func TestConfig_LegacyContainerOwnersDecodeFromYAML(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	if err := DecodeConfig([]byte("legacy_container_owners:\n  - orchestrator:/data\n  - ./node\n"), cfg); err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if want := []string{"orchestrator:/data", "./node"}; !reflect.DeepEqual(cfg.LegacyContainerOwners, want) {
		t.Fatalf("legacy_container_owners decoded as %v, want %v", cfg.LegacyContainerOwners, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
