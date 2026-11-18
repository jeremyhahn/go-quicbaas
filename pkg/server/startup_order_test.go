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

package server

import (
	"slices"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// startupNames is the order orchestrationStartup would run its steps in.
func startupNames(cfg *config.Config) []string {
	var names []string
	for _, step := range (&Server{config: cfg}).orchestrationStartup() {
		names = append(names, step.name)
	}
	return names
}

// TestOrchestrationStartup_TheEmbeddedAgentIsUpBeforeAnythingDispatches is
// the restart regression for go-quicbaas#981's review: the embedded agent's
// startup sweep of orphaned workspace specs ran AFTER recovered jobs were
// relaunched, so it could delete the spec of a session a recovered job had
// just spawned, and a recovered job pinned to the embedded agent was refused
// before the agent's dialer existed. The agent — sweeps and registration —
// must come after the engine and before the SDLC runtime (whose backstops
// dispatch) and before job recovery.
func TestOrchestrationStartup_TheEmbeddedAgentIsUpBeforeAnythingDispatches(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.SDLC.Enabled = true
	cfg.Orchestration.Enabled = true
	cfg.Server.EmbeddedFleetNode.Enabled = true

	want := []string{startupRegisterSDLC, startupOrchestration, startupEmbeddedFleetNode, startupSDLC, startupRecoverJobs, startupTaskDispatcher}
	if got := startupNames(cfg); !slices.Equal(got, want) {
		t.Fatalf("startup order = %v, want %v", got, want)
	}
}

// TestOrchestrationStartup_RunsOnlyWhatIsEnabled: a disabled component
// contributes no step, and the rest keep their order.
func TestOrchestrationStartup_RunsOnlyWhatIsEnabled(t *testing.T) {
	t.Parallel()
	if got := startupNames(&config.Config{}); len(got) != 0 {
		t.Fatalf("nothing enabled ran %v", got)
	}
	cfg := &config.Config{}
	cfg.Orchestration.Enabled = true
	cfg.Server.EmbeddedFleetNode.Enabled = true
	want := []string{startupOrchestration, startupEmbeddedFleetNode, startupRecoverJobs, startupTaskDispatcher}
	if got := startupNames(cfg); !slices.Equal(got, want) {
		t.Fatalf("startup order = %v, want %v", got, want)
	}
}

// TestOrchestrationStartup_TheDispatcherStartsAfterJobRecovery is the
// restart race of 2026-09-23: the dispatcher, started with the engine, swept
// before recovery had re-acquired the resumed jobs' leases, released their
// tasks back to the queue as if stranded, and later dispatched a second job
// that superseded each resumed one. The dispatcher must be the step after
// recover_jobs.
func TestOrchestrationStartup_TheDispatcherStartsAfterJobRecovery(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.SDLC.Enabled = true
	cfg.Orchestration.Enabled = true
	got := startupNames(cfg)
	recoverAt, dispatchAt := slices.Index(got, startupRecoverJobs), slices.Index(got, startupTaskDispatcher)
	if recoverAt < 0 || dispatchAt < 0 || dispatchAt != recoverAt+1 {
		t.Fatalf("startup order = %v, want %s immediately after %s", got, startupTaskDispatcher, startupRecoverJobs)
	}
}

// TestOrchestrationStartup_TheBeforeRecoveryHookRunsJustBeforeRecovery pins
// go-quicbaas#2512: an embedder's WithBeforeJobRecovery hook (where it
// registers the agent backends a resumed job needs) runs immediately before
// interrupted jobs are recovered, and without the option nothing changes.
func TestOrchestrationStartup_TheBeforeRecoveryHookRunsJustBeforeRecovery(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.SDLC.Enabled = true
	cfg.Orchestration.Enabled = true
	without := startupNames(cfg)
	if slices.Contains(without, startupBeforeRecoverJobs) {
		t.Fatalf("startup order without the hook = %v, want no %s", without, startupBeforeRecoverJobs)
	}

	var opts serverOptions
	WithBeforeJobRecovery(func(*Server) error { return nil })(&opts)
	var got []string
	for _, step := range (&Server{config: cfg, opts: opts}).orchestrationStartup() {
		got = append(got, step.name)
	}
	want := slices.Clone(without)
	at := slices.Index(want, startupRecoverJobs)
	if at < 0 {
		t.Fatalf("startup order = %v, want %s", without, startupRecoverJobs)
	}
	want = slices.Insert(want, at, startupBeforeRecoverJobs)
	if !slices.Equal(got, want) {
		t.Fatalf("startup order with the hook = %v, want %v", got, want)
	}

	off := &config.Config{}
	off.SDLC.Enabled = true
	if names := (&Server{config: off, opts: opts}).orchestrationStartup(); slices.ContainsFunc(names, func(s startupStep) bool {
		return s.name == startupBeforeRecoverJobs
	}) {
		t.Fatalf("hook step ran with orchestration disabled")
	}
}

// TestOrchestrationStartup_NoSpawnerStartsNoDispatcher: the dispatcher step
// with no captured spawner logs and returns without starting anything.
func TestOrchestrationStartup_NoSpawnerStartsNoDispatcher(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Orchestration.Enabled = true
	s := &Server{config: cfg, logger: testServerLogger(t)}
	for _, step := range s.orchestrationStartup() {
		if step.name != startupTaskDispatcher {
			continue
		}
		if err := step.run(); err != nil {
			t.Fatalf("dispatcher step: %v", err)
		}
	}
	if s.taskDispatcher != nil {
		t.Fatal("a dispatcher was started with no spawner")
	}
}
