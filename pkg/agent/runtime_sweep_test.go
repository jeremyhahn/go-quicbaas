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

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// sweepingContainerRuntime is a container runtime that also knows which
// containers belong to whom, which is what makes it a
// orchestration.ContainerOwnerSweeper.
type sweepingContainerRuntime struct {
	fakeContainerRuntime

	mu sync.Mutex

	// owned maps owner label value -> the containers carrying it.
	owned map[string][]string

	// asked records every owner the runtime was asked to sweep.
	asked []string

	err error
}

func newSweepingRuntime(owned map[string][]string) *sweepingContainerRuntime {
	rt := &sweepingContainerRuntime{owned: owned}
	rt.available = true
	return rt
}

func (s *sweepingContainerRuntime) SweepOwned(_ context.Context, owner string) (orchestration.ContainerSweep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, owner)
	if s.err != nil {
		return orchestration.ContainerSweep{}, s.err
	}
	removed := s.owned[owner]
	delete(s.owned, owner)
	return orchestration.ContainerSweep{Owner: owner, Removed: removed}, nil
}

var _ orchestration.ContainerOwnerSweeper = (*sweepingContainerRuntime)(nil)

// refusingCloseBackend is a backend whose own teardown fails. The daemon still
// stops tracking the session, which is the property under test.
type refusingCloseBackend struct{ mockBackend }

func (b *refusingCloseBackend) Close(context.Context, string) error {
	return errors.New("the transport was already gone")
}

// TestRuntime_SweepOrphanContainersClaimsByThisDaemonsWorkDir: the owner a
// daemon sweeps by is its own state directory, which is the same value its
// backends stamp on the containers they launch. Anything else and a restart
// would either miss its own orphans or claim another daemon's live workers.
func TestRuntime_SweepOrphanContainersClaimsByThisDaemonsWorkDir(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	rt := newSweepingRuntime(map[string][]string{
		workDir:       {"quicbaas-acp-aaaa"},
		"/some/other": {"quicbaas-acp-bbbb"},
	})
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: rt,
		Logger:           &testLogger{t: t},
		WorkDir:          workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sweep, err := runtime.SweepOrphanContainers(context.Background())
	if err != nil {
		t.Fatalf("SweepOrphanContainers: %v", err)
	}
	if strings.Join(sweep.Removed, ",") != "quicbaas-acp-aaaa" {
		t.Fatalf("removed = %v, want this daemon's own orphan", sweep.Removed)
	}
	if len(rt.asked) != 1 || rt.asked[0] != workDir {
		t.Fatalf("swept by %v, want the daemon's work directory %q", rt.asked, workDir)
	}
	if _, ok := rt.owned["/some/other"]; !ok {
		t.Fatal("the sweep claimed another daemon's containers")
	}
}

// TestRuntime_SweepOrphanContainersReportsARuntimeThatWouldNotSweep is the
// error path: a sweep that could not be made is not a sweep that found
// nothing, and the daemon's startup treats it as a failure.
func TestRuntime_SweepOrphanContainersReportsARuntimeThatWouldNotSweep(t *testing.T) {
	t.Parallel()
	rt := newSweepingRuntime(nil)
	rt.err = &orchestration.ContainerSweepRefusedError{Reason: "the runtime declined"}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: rt,
		Logger:           &testLogger{t: t},
		WorkDir:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if _, err := runtime.SweepOrphanContainers(context.Background()); !errors.Is(err, orchestration.ErrContainerSweep) {
		t.Fatalf("err = %v, want the sweep refusal", err)
	}
}

// TestRuntime_SweepOrphanContainersIsANoOpWithNothingToSweep covers the two
// daemons that never created a container: one with no container runtime at
// all, and one whose runtime cannot enumerate its own. Neither can have leaked
// anything, so neither may fail a startup.
func TestRuntime_SweepOrphanContainersIsANoOpWithNothingToSweep(t *testing.T) {
	t.Parallel()
	cases := map[string]orchestration.ContainerRuntime{
		"no container runtime":                    nil,
		"a runtime that cannot enumerate its own": &fakeContainerRuntime{available: true},
	}
	for name, containerRT := range cases {
		t.Run(name, func(t *testing.T) {
			runtime, err := NewRuntime(RuntimeConfig{
				Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
				ContainerRuntime: containerRT,
				Logger:           &testLogger{t: t},
				WorkDir:          t.TempDir(),
			})
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			sweep, err := runtime.SweepOrphanContainers(context.Background())
			if err != nil {
				t.Fatalf("SweepOrphanContainers: %v", err)
			}
			if len(sweep.Removed) != 0 {
				t.Fatalf("removed = %v, want nothing", sweep.Removed)
			}
		})
	}
}

// TestDaemon_StartSweepsTheContainersAKilledPredecessorLeft is the startup
// half: the sweep happens, and it happens BEFORE the spawn server accepts a
// request that would create a container this daemon could not tell apart from
// an orphan.
func TestDaemon_StartSweepsTheContainersAKilledPredecessorLeft(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	rt := newSweepingRuntime(map[string][]string{cfg.WorkDir: {"quicbaas-acp-aaaa"}})
	spawnServer := &orderedSpawnServer{runtime: rt}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: rt,
		Logger:           &testLogger{t: t},
		WorkDir:          cfg.WorkDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

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

	if len(rt.asked) != 1 || rt.asked[0] != cfg.WorkDir {
		t.Fatalf("startup swept %v, want the daemon's own work directory", rt.asked)
	}
	if !spawnServer.sweptFirst {
		t.Fatal("the spawn server was accepting spawns before the orphan sweep ran")
	}
}

// TestDaemon_StartRefusesASweepItCouldNotComplete: an orphan that survives its
// sweep accumulates without bound, so a daemon that cannot sweep does not
// start — the same call the signing-socket sweep makes (go-quicbaas#676).
func TestDaemon_StartRefusesASweepItCouldNotComplete(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	rt := newSweepingRuntime(nil)
	rt.err = &orchestration.ContainerSweepError{Owner: cfg.WorkDir, Cause: errors.New("cannot connect to the daemon")}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ContainerRuntime: rt,
		Logger:           &testLogger{t: t},
		WorkDir:          cfg.WorkDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	spawnServer := &fakeSpawnServer{addr: "127.0.0.1:0"}

	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     runtime,
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "agent-test", TenantID: cfg.TenantID}},
		SpawnServer: spawnServer,
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); !errors.Is(err, orchestration.ErrContainerSweep) {
		t.Fatalf("Start err = %v, want the sweep failure", err)
	}
	if d.State() != "failed" {
		t.Errorf("state = %q, want failed", d.State())
	}
	if spawnServer.started.Load() {
		t.Error("the spawn server started behind a sweep that failed")
	}
}

// orderedSpawnServer records whether the sweep had already run by the time it
// was started. The ORDER is the property: once this daemon has spawned a
// container of its own, the sweep can no longer tell it from an orphan.
type orderedSpawnServer struct {
	fakeSpawnServer
	runtime    *sweepingContainerRuntime
	sweptFirst bool
}

func (o *orderedSpawnServer) Start(ctx context.Context) error {
	o.runtime.mu.Lock()
	o.sweptFirst = len(o.runtime.asked) > 0
	o.runtime.mu.Unlock()
	return o.fakeSpawnServer.Start(ctx)
}

// TestRuntime_CloseReapsThePersistedRecordEvenWhenTheBackendRefuses: the
// durable record and the in-memory mapping both mean "this daemon holds this
// session", and a close that dropped one and kept the other is how
// <workDir>/sessions filled up with records for sessions nothing could answer
// for (go-quicbaas#750).
func TestRuntime_CloseReapsThePersistedRecordEvenWhenTheBackendRefuses(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	backend := &refusingCloseBackend{}
	runtime, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"mock": backend},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	record := filepath.Join(workDir, "sessions", "ses-1.json")
	if err := os.MkdirAll(filepath.Dir(record), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"session_id":"ses-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runtime.Close(context.Background(), "mock", "ses-1"); err == nil {
		t.Fatal("a backend teardown that failed was reported as success")
	}
	if _, statErr := os.Stat(record); !os.IsNotExist(statErr) {
		t.Fatalf("the session record outlived the close: %v", statErr)
	}
}

// TestRuntime_CloseLeavesAnotherSessionsRecord is the bad-input twin: closing
// one session must not reap the records of the others the daemon holds.
func TestRuntime_CloseLeavesAnotherSessionsRecord(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	runtime, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	sessions := filepath.Join(workDir, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ses-1", "ses-2"} {
		if err := os.WriteFile(filepath.Join(sessions, id+".json"), []byte(`{"session_id":"`+id+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := runtime.Close(context.Background(), "mock", "ses-1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(sessions, "ses-2.json")); statErr != nil {
		t.Fatalf("closing one session reaped another's record: %v", statErr)
	}
}
