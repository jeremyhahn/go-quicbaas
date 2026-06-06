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

package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeEnroller satisfies Enroller without hitting the control plane.
type fakeEnroller struct {
	identity *orchestration.AgentIdentity
	err      error
	calls    atomic.Int32
}

func (f *fakeEnroller) Enroll(ctx context.Context, cfg *Config) (*orchestration.AgentIdentity, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.identity, nil
}

// fakeSpawnServer records lifecycle events for assertions.
type fakeSpawnServer struct {
	addr     string
	startErr error
	stopErr  error
	started  atomic.Bool
	stopped  atomic.Bool
}

func (f *fakeSpawnServer) Start(ctx context.Context) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started.Store(true)
	return nil
}

func (f *fakeSpawnServer) Stop(ctx context.Context) error {
	f.stopped.Store(true)
	return f.stopErr
}

func (f *fakeSpawnServer) Addr() string { return f.addr }

// fakeHeartbeat counts tick sends.
type fakeHeartbeat struct {
	ticks atomic.Int32
	err   error
}

func (f *fakeHeartbeat) SendHeartbeat(ctx context.Context, identity *orchestration.AgentIdentity, status HeartbeatStatus) error {
	f.ticks.Add(1)
	return f.err
}

// testLogger adapts *testing.T to the interfaces.Logger surface.
type testLogger struct {
	t *testing.T
}

func (l *testLogger) Debug(msg string, args ...any) {
	l.t.Log(append([]any{"DEBUG:", msg}, args...)...)
}
func (l *testLogger) Debugf(format string, args ...interface{}) { l.t.Logf("DEBUG: "+format, args...) }
func (l *testLogger) Info(msg string, args ...any)              { l.t.Log(append([]any{"INFO:", msg}, args...)...) }
func (l *testLogger) Infof(format string, args ...interface{})  { l.t.Logf("INFO: "+format, args...) }
func (l *testLogger) Warn(msg string, args ...any)              { l.t.Log(append([]any{"WARN:", msg}, args...)...) }
func (l *testLogger) Warnf(format string, args ...interface{})  { l.t.Logf("WARN: "+format, args...) }
func (l *testLogger) Error(msg string, args ...any) {
	l.t.Log(append([]any{"ERROR:", msg}, args...)...)
}
func (l *testLogger) Errorf(format string, args ...interface{}) { l.t.Logf("ERROR: "+format, args...) }
func (l *testLogger) Fatal(msg string)                          { l.t.Fatal(msg) }
func (l *testLogger) Fatalf(format string, args ...interface{}) { l.t.Fatalf(format, args...) }
func (l *testLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}
func (l *testLogger) WithError(err error) interfaces.Logger {
	return l
}

// mockBackend is a minimal AgentBackend for constructing a Runtime in tests.
type mockBackend struct{}

func (m *mockBackend) Spawn(ctx context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return &orchestration.Session{ID: "sess-test", WorkDir: req.WorkDir}, nil
}

func (m *mockBackend) Prompt(ctx context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{SessionID: req.SessionID}, nil
}

func (m *mockBackend) Status(ctx context.Context, id string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: id}, nil
}

func (m *mockBackend) Close(ctx context.Context, id string) error { return nil }

func (m *mockBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent}
}

func (m *mockBackend) Type() string         { return "mock" }
func (m *mockBackend) ToolGuidance() string { return "" }

func newTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

func newTestConfig(t *testing.T) *Config {
	t.Helper()
	tmp := t.TempDir()
	cert, key, ca := writeTestAgentIdentity(t, tmp)
	return &Config{
		ServerAddr:    "https://control plane.test:8443",
		TenantID:      "tenant-a",
		BindAddr:      "127.0.0.1:0",
		WorkDir:       filepath.Join(tmp, "agent"),
		AgentCertFile: cert,
		AgentKeyFile:  key,
		ServerCAFile:  ca,
	}
}

func TestConfig_Validate_Required(t *testing.T) {
	t.Parallel()
	base := newTestConfig(t)
	if err := base.Validate(); err != nil {
		t.Fatalf("baseline valid config failed: %v", err)
	}

	missingCases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"server_addr", func(c *Config) { c.ServerAddr = "" }},
		{"tenant_id", func(c *Config) { c.TenantID = "" }},
		{"bind_addr", func(c *Config) { c.BindAddr = "" }},
		{"work_dir", func(c *Config) { c.WorkDir = "" }},
		{"no_agent_cert", func(c *Config) { c.AgentCertFile = "" }},
		{"no_agent_key", func(c *Config) { c.AgentKeyFile = "" }},
		{"no_server_ca", func(c *Config) { c.ServerCAFile = "" }},
	}
	for _, tc := range missingCases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestConfig(t)
			tc.mutate(c)
			if err := c.Validate(); err == nil {
				t.Fatalf("expected validation error for missing %s", tc.name)
			}
		})
	}
}

func TestConfig_Validate_InvalidEnums(t *testing.T) {
	t.Parallel()
	c := newTestConfig(t)
	c.Backends = []BackendKind{BackendKind("bogus")}
	if err := c.Validate(); !errors.Is(err, ErrConfigUnknownBackend) {
		t.Fatalf("expected ErrConfigUnknownBackend, got %v", err)
	}

	c = newTestConfig(t)
	c.ContainerRuntime = ContainerRuntimeSelector("rkt")
	if err := c.Validate(); !errors.Is(err, ErrConfigUnknownRuntime) {
		t.Fatalf("expected ErrConfigUnknownRuntime, got %v", err)
	}
}

func TestConfig_DefaultsFillIn(t *testing.T) {
	t.Parallel()
	c := newTestConfig(t)
	if backends := c.EffectiveBackends(); len(backends) != 2 {
		t.Errorf("default backends: got %v, want 2", backends)
	}
	if c.EffectiveHeartbeat() != 30*time.Second {
		t.Errorf("default heartbeat: got %v, want 30s", c.EffectiveHeartbeat())
	}
	if c.EffectiveShutdownGrace() != 30*time.Second {
		t.Errorf("default shutdown grace: got %v, want 30s", c.EffectiveShutdownGrace())
	}
	if c.EffectiveContainerRuntime() != ContainerRuntimeAuto {
		t.Errorf("default container runtime: got %v, want auto", c.EffectiveContainerRuntime())
	}
}

func TestDaemon_Start_TransitionsThroughStates(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	identity := &orchestration.AgentIdentity{AgentID: "agent-test", TenantID: cfg.TenantID}
	deps := DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: identity},
		Heartbeat:   &fakeHeartbeat{},
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	}
	d, err := NewDaemon(cfg, deps)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if got := d.State(); got != "new" {
		t.Errorf("initial state: got %q, want %q", got, "new")
	}

	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := d.State(); got != "running" {
		t.Errorf("post-Start state: got %q, want %q", got, "running")
	}
	if d.Identity() == nil || d.Identity().AgentID != identity.AgentID {
		t.Errorf("identity not stored: got %v", d.Identity())
	}

	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := d.State(); got != "stopped" {
		t.Errorf("post-Stop state: got %q, want %q", got, "stopped")
	}
}

func TestDaemon_Start_EnrollFailurePreventsStart(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	enrollErr := errors.New("boom")
	server := &fakeSpawnServer{}
	deps := DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{err: enrollErr},
		SpawnServer: server,
		Logger:      &testLogger{t: t},
	}
	d, err := NewDaemon(cfg, deps)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); !errors.Is(err, enrollErr) {
		t.Fatalf("Start err: got %v, want wraps %v", err, enrollErr)
	}
	if got := d.State(); got != "failed" {
		t.Errorf("state: got %q, want %q", got, "failed")
	}
	if server.started.Load() {
		t.Errorf("spawn server should not have started after enroll failure")
	}
}

func TestDaemon_Start_IsIdempotent(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	enroller := &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}}
	deps := DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    enroller,
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	}
	d, err := NewDaemon(cfg, deps)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if got := enroller.calls.Load(); got != 1 {
		t.Errorf("Enroll calls: got %d, want 1 (startOnce)", got)
	}
	_ = d.Stop(context.Background())
}

func TestDaemon_HeartbeatLoop_Emits(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.HeartbeatInterval = 10 * time.Millisecond
	hb := &fakeHeartbeat{}
	deps := DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}},
		Heartbeat:   hb,
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	}
	d, err := NewDaemon(cfg, deps)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Wait for several ticks without using time.Sleep in the test body
	// itself — use a bounded poll with a short-lived ticker.
	deadline := time.Now().Add(500 * time.Millisecond)
	for hb.ticks.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := hb.ticks.Load(); got < 3 {
		t.Errorf("heartbeat ticks: got %d, want >=3", got)
	}
	_ = d.Stop(context.Background())
}

// TestDaemon_HeartbeatLoop_ExitsOnReregisterFailure asserts that a
// terminal ErrAgentReRegisterFailed reported by the HeartbeatSender
// causes the heartbeatLoop to exit and RunError() to surface the
// terminal cause. Systemd uses RunError as the exit-code signal so the
// unit's Restart=on-failure policy kicks in.
func TestDaemon_HeartbeatLoop_ExitsOnReregisterFailure(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.HeartbeatInterval = 10 * time.Millisecond
	hb := &fakeHeartbeat{err: ErrAgentReRegisterFailed}
	deps := DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}},
		Heartbeat:   hb,
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	}
	d, err := NewDaemon(cfg, deps)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait until the heartbeat loop reports the terminal error via
	// RunError. Bounded poll keeps the test fast and deterministic.
	deadline := time.Now().Add(500 * time.Millisecond)
	for d.RunError() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := d.RunError(); got == nil || !errors.Is(got, ErrAgentReRegisterFailed) {
		t.Fatalf("expected RunError to surface ErrAgentReRegisterFailed, got %v", got)
	}
	if d.State() != "failed" {
		t.Errorf("expected state=failed, got %q", d.State())
	}

	// Stop must still tear down the spawn server cleanly.
	if err := d.Stop(context.Background()); err != nil {
		t.Errorf("Stop after re-register failure: %v", err)
	}
}

func TestDaemon_Stop_IsIdempotent(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	deps := DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}},
		SpawnServer: &fakeSpawnServer{},
		Logger:      &testLogger{t: t},
	}
	d, err := NewDaemon(cfg, deps)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Errorf("second Stop should be no-op, got %v", err)
	}
}

func TestNewDaemon_MissingDeps(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	rt := newTestRuntime(t)
	enroller := &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a"}}
	server := &fakeSpawnServer{}
	logger := &testLogger{t: t}

	cases := []struct {
		name string
		deps DaemonDeps
		want error
	}{
		{"no runtime", DaemonDeps{Enroller: enroller, SpawnServer: server, Logger: logger}, ErrDaemonNoRuntime},
		{"no enroller", DaemonDeps{Runtime: rt, SpawnServer: server, Logger: logger}, ErrDaemonNoEnroller},
		{"no spawn server", DaemonDeps{Runtime: rt, Enroller: enroller, Logger: logger}, ErrDaemonNoSpawnServer},
		{"no logger", DaemonDeps{Runtime: rt, Enroller: enroller, SpawnServer: server}, ErrDaemonNoLogger},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDaemon(cfg, tc.deps)
			if !errors.Is(err, tc.want) {
				t.Errorf("got %v, want wraps %v", err, tc.want)
			}
		})
	}
}

func TestRuntime_Spawn_ResolvesSandboxMode(t *testing.T) {
	t.Parallel()
	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	// DEFAULT under OPT-IN → NONE (no runtime needed).
	req := &orchestration.SpawnRequest{WorkDir: "/tmp/x"}
	if _, err := rt.Spawn(context.Background(), "t1", nil, "mock", req); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if req.SandboxMode != orchestration.SandboxModeNone {
		t.Errorf("effective mode: got %v, want NONE", req.SandboxMode)
	}

	// REQUIRED with no container runtime → violation.
	req = &orchestration.SpawnRequest{WorkDir: "/tmp/x", SandboxMode: orchestration.SandboxModeRequired}
	_, err = rt.Spawn(context.Background(), "t1", nil, "mock", req)
	var v *orchestration.SandboxModeViolationError
	if !errors.As(err, &v) {
		t.Fatalf("expected SandboxModeViolationError, got %T: %v", err, err)
	}
}

func TestRuntime_Spawn_UnknownBackend(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	_, err := rt.Spawn(context.Background(), "t1", nil, "does-not-exist", &orchestration.SpawnRequest{WorkDir: "/tmp/x"})
	if !errors.Is(err, ErrRuntimeUnknownBackend) {
		t.Fatalf("got %v, want wraps ErrRuntimeUnknownBackend", err)
	}
}

func TestRuntime_ImageFor_QualifiesWithRegistry(t *testing.T) {
	t.Parallel()
	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"mock": &mockBackend{}},
		ImageMap:      map[string]string{"mock": "mock:v1", "full": "ghcr.io/x/full:v1"},
		ImageRegistry: "ghcr.io/example",
		Logger:        &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	img, ok := rt.ImageFor("mock")
	if !ok {
		t.Fatal("mock not found")
	}
	if want := "ghcr.io/example/mock:v1"; img != want {
		t.Errorf("unqualified image: got %q, want %q", img, want)
	}
	img, _ = rt.ImageFor("full")
	if want := "ghcr.io/x/full:v1"; img != want {
		t.Errorf("already-qualified image: got %q, want %q (no double-prefix)", img, want)
	}
}

// TestSandboxResolver_TenantOverride_Tightens mirrors the orchestration-level
// test but verifies it goes through the agent-package wrapper.
func TestSandboxResolver_TenantOverride_Tightens(t *testing.T) {
	t.Parallel()
	r := NewSandboxResolver(orchestration.SandboxPolicyOptIn, false)

	// Baseline: opt-in + DEFAULT → NONE.
	mode, err := r.Resolve(orchestration.SandboxModeDefault)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mode != orchestration.SandboxModeNone {
		t.Errorf("opt-in default: got %v, want NONE", mode)
	}

	// Tenant override to REQUIRED without runtime → violation.
	tightened := orchestration.SandboxPolicyRequired
	_, err = r.WithTenantOverride(&tightened).Resolve(orchestration.SandboxModeDefault)
	var v *orchestration.SandboxModeViolationError
	if !errors.As(err, &v) {
		t.Fatalf("expected violation, got %v", err)
	}

	// Same tenant override with runtime present → REQUIRED.
	withRT := NewSandboxResolver(orchestration.SandboxPolicyOptIn, true).WithTenantOverride(&tightened)
	mode, err = withRT.Resolve(orchestration.SandboxModeDefault)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mode != orchestration.SandboxModeRequired {
		t.Errorf("got %v, want REQUIRED", mode)
	}
}

// helper to eyeball the state enum for debugging.
var _ = fmt.Sprintf
