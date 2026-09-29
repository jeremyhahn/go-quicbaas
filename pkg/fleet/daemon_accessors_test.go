// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestDaemon_Runtime_Config_RunError_Accessors exercises the three trivial
// getter methods that returned 0% coverage: Runtime(), Config(), RunError().
func TestDaemon_Runtime_Config_RunError_Accessors(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	rt := newTestRuntime(t)
	identity := &orchestration.AgentIdentity{AgentID: "acc-test", TenantID: cfg.TenantID}

	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     rt,
		Enroller:    &fakeEnroller{identity: identity},
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	// Runtime() must return the exact runtime we wired in.
	if got := d.Runtime(); got != rt {
		t.Errorf("Runtime(): got %p, want %p", got, rt)
	}

	// Config() must return the exact config we wired in.
	if got := d.Config(); got != cfg {
		t.Errorf("Config(): got %p, want %p", got, cfg)
	}

	// RunError() must be nil on a freshly created daemon.
	if got := d.RunError(); got != nil {
		t.Errorf("RunError() on fresh daemon: got %v, want nil", got)
	}

	// After a failed Start, RunError must carry the error.
	enrollErr := errors.New("enrollment failure")
	failDeps := DaemonDeps{
		Runtime:     rt,
		Enroller:    &fakeEnroller{err: enrollErr},
		SpawnServer: &fakeSpawnServer{},
		Logger:      &testLogger{t: t},
	}
	failCfg := newTestConfig(t)
	d2, err := NewDaemon(failCfg, failDeps)
	if err != nil {
		t.Fatalf("NewDaemon(fail): %v", err)
	}
	_ = d2.Start(context.Background())
	if got := d2.RunError(); got == nil {
		t.Error("RunError() after enrollment failure: expected non-nil error")
	}
}

// TestDaemon_State_AllTransitions confirms every state label is reachable and
// that the "unknown" branch is unreachable via normal paths. We cover each
// daemonState constant by driving the daemon through its lifecycle.
func TestDaemon_State_AllTransitions(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	identity := &orchestration.AgentIdentity{AgentID: "state-test", TenantID: cfg.TenantID}

	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: identity},
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	// stateNew → "new"
	if got := d.State(); got != "new" {
		t.Errorf("State() before Start: got %q, want %q", got, "new")
	}

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// stateRunning → "running"
	if got := d.State(); got != "running" {
		t.Errorf("State() after Start: got %q, want %q", got, "running")
	}

	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// stateStopped → "stopped"
	if got := d.State(); got != "stopped" {
		t.Errorf("State() after Stop: got %q, want %q", got, "stopped")
	}

	// stateFailed → "failed" (drive a daemon whose enroller always fails)
	failDeps := DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{err: errors.New("boom")},
		SpawnServer: &fakeSpawnServer{},
		Logger:      &testLogger{t: t},
	}
	fd, err := NewDaemon(newTestConfig(t), failDeps)
	if err != nil {
		t.Fatalf("NewDaemon(fail): %v", err)
	}
	_ = fd.Start(context.Background())
	if got := fd.State(); got != "failed" {
		t.Errorf("State() after enroll fail: got %q, want %q", got, "failed")
	}
}

// TestDaemon_SendHeartbeat_ErrorIsLogged verifies the error-logging branch
// inside sendHeartbeat when the HeartbeatSender returns an error. The daemon
// must NOT crash or propagate the error — it logs it and continues.
func TestDaemon_SendHeartbeat_ErrorIsLogged(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	identity := &orchestration.AgentIdentity{AgentID: "hb-err-test", TenantID: cfg.TenantID}
	hb := &fakeHeartbeat{err: errors.New("connection reset")}

	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: identity},
		Heartbeat:   hb,
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	// Set the identity so sendHeartbeat has a non-nil identity.
	d.identity.Store(identity)

	// Call sendHeartbeat directly — it must not panic or return an error to
	// the caller even when the sender itself returns one.
	d.sendHeartbeat(context.Background())
	// Verify the heartbeat sender was actually called.
	if got := hb.ticks.Load(); got != 1 {
		t.Errorf("expected 1 heartbeat tick, got %d", got)
	}
}

// TestDaemon_EnsureWorkDir_SucceedsOnExistingDir verifies ensureWorkDir does
// not fail when the directory already exists (idempotent).
func TestDaemon_EnsureWorkDir_SucceedsOnExistingDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := ensureWorkDir(dir); err != nil {
		t.Fatalf("ensureWorkDir on existing dir: %v", err)
	}
}

// TestDaemon_EnsureWorkDir_CreatesDir verifies ensureWorkDir creates the
// directory when it does not yet exist.
func TestDaemon_EnsureWorkDir_CreatesDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() + "/subdir"
	if err := ensureWorkDir(dir); err != nil {
		t.Fatalf("ensureWorkDir create: %v", err)
	}
}
