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

//go:build integration && fleet_runtime

package fleet_runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/agentcert"
)

// ----------------------------------------------------------------------------
// testLogger — forwards to testing.T
// ----------------------------------------------------------------------------

// testLogger implements interfaces.Logger and forwards all output to t.Log so
// messages appear in verbose test output and are discarded on success.
type testLogger struct {
	t *testing.T
}

// Compile-time interface guard.
var _ interfaces.Logger = (*testLogger)(nil)

func (l *testLogger) Debug(msg string, _ ...any)                            { l.t.Helper(); l.t.Log("[DEBUG] " + msg) }
func (l *testLogger) Debugf(f string, a ...interface{})                     { l.t.Helper(); l.t.Logf("[DEBUG] "+f, a...) }
func (l *testLogger) Info(msg string, _ ...any)                             { l.t.Helper(); l.t.Log("[INFO] " + msg) }
func (l *testLogger) Infof(f string, a ...interface{})                      { l.t.Helper(); l.t.Logf("[INFO] "+f, a...) }
func (l *testLogger) Warn(msg string, _ ...any)                             { l.t.Helper(); l.t.Log("[WARN] " + msg) }
func (l *testLogger) Warnf(f string, a ...interface{})                      { l.t.Helper(); l.t.Logf("[WARN] "+f, a...) }
func (l *testLogger) Error(msg string, _ ...any)                            { l.t.Helper(); l.t.Log("[ERROR] " + msg) }
func (l *testLogger) Errorf(f string, a ...interface{})                     { l.t.Helper(); l.t.Logf("[ERROR] "+f, a...) }
func (l *testLogger) Fatal(msg string)                                      { l.t.Helper(); l.t.Fatal("[FATAL] " + msg) }
func (l *testLogger) Fatalf(f string, a ...interface{})                     { l.t.Helper(); l.t.Fatalf("[FATAL] "+f, a...) }
func (l *testLogger) WithField(_ string, _ interface{}) interfaces.Logger   { return l }
func (l *testLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return l }
func (l *testLogger) WithError(_ error) interfaces.Logger                   { return l }

// ----------------------------------------------------------------------------
// stubEnroller — satisfies fleet.Enroller for unit-style lifecycle tests
// ----------------------------------------------------------------------------

// stubEnroller is a test double for fleet.Enroller. It records the number of
// Enroll calls and returns the pre-configured identity (or error).
//
// Phase 3 dependency: swap NewProductionEnroller in once
// enrollment.go is merged (see daemon_lifecycle_test.go).
type stubEnroller struct {
	identity *orchestration.AgentIdentity
	err      error
	calls    atomic.Int32
}

func (e *stubEnroller) Enroll(_ context.Context, _ *fleet.Config) (*orchestration.AgentIdentity, error) {
	e.calls.Add(1)
	if e.err != nil {
		return nil, e.err
	}
	return e.identity, nil
}

// enrollCalls returns the total number of times Enroll was called.
func (e *stubEnroller) enrollCalls() int {
	return int(e.calls.Load())
}

// ----------------------------------------------------------------------------
// stubHeartbeat — satisfies fleet.HeartbeatSender
// ----------------------------------------------------------------------------

// stubHeartbeat records heartbeats for assertion and optionally injects errors.
type stubHeartbeat struct {
	beats     atomic.Int32
	beatsCh   chan fleet.HeartbeatStatus
	injectErr error
}

// newStubHeartbeat creates a stubHeartbeat with a buffered channel so sends
// never block. Callers can drain beatsCh to assert heartbeat delivery.
func newStubHeartbeat(bufSize int) *stubHeartbeat {
	return &stubHeartbeat{beatsCh: make(chan fleet.HeartbeatStatus, bufSize)}
}

func (h *stubHeartbeat) SendHeartbeat(_ context.Context, _ *orchestration.AgentIdentity, status fleet.HeartbeatStatus) error {
	if h.injectErr != nil {
		return h.injectErr
	}
	h.beats.Add(1)
	select {
	case h.beatsCh <- status:
	default:
	}
	return nil
}

// ----------------------------------------------------------------------------
// stubSpawnServer — satisfies fleet.SpawnServer
// ----------------------------------------------------------------------------

// stubSpawnServer is a no-op SpawnServer used when the real FleetRuntime
// gRPC service is not yet available (Phase 4 dependency).
type stubSpawnServer struct {
	addr    string
	started atomic.Bool
	stopped atomic.Bool
}

func newStubSpawnServer(addr string) *stubSpawnServer {
	return &stubSpawnServer{addr: addr}
}

func (s *stubSpawnServer) Start(_ context.Context) error {
	s.started.Store(true)
	return nil
}

func (s *stubSpawnServer) Stop(_ context.Context) error {
	s.stopped.Store(true)
	return nil
}

func (s *stubSpawnServer) Addr() string { return s.addr }

// ----------------------------------------------------------------------------
// stubAgentBackend — satisfies orchestration.AgentBackend
// ----------------------------------------------------------------------------

// stubAgentBackend records Spawn/Prompt/Close calls. It never actually
// executes a subprocess, making daemon-level tests fast and hermetic.
type stubAgentBackend struct {
	backendType string
	spawnErr    error
	promptErr   error
	closeErr    error
	spawnCount  atomic.Int32
}

func (b *stubAgentBackend) Spawn(_ context.Context, _ *orchestration.SpawnRequest) (*orchestration.Session, error) {
	b.spawnCount.Add(1)
	if b.spawnErr != nil {
		return nil, b.spawnErr
	}
	return &orchestration.Session{
		ID:      "test-session-id",
		Backend: b.backendType,
		Status:  orchestration.SessionStateIdle,
	}, nil
}

func (b *stubAgentBackend) Prompt(_ context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	if b.promptErr != nil {
		return nil, b.promptErr
	}
	return &orchestration.PromptResponse{
		Content: []orchestration.ContentBlock{{Type: "text", Text: "stub response"}},
	}, nil
}

func (b *stubAgentBackend) Status(_ context.Context, sessionID string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{
		SessionID: sessionID,
		State:     orchestration.SessionStateIdle,
	}, nil
}

func (b *stubAgentBackend) Close(_ context.Context, _ string) error { return b.closeErr }

func (b *stubAgentBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{
		Kind:               orchestration.BackendKindAgent,
		PersistentSessions: true,
		ACP:                true,
	}
}

func (b *stubAgentBackend) Type() string         { return b.backendType }
func (b *stubAgentBackend) ToolGuidance() string { return "" }

// ----------------------------------------------------------------------------
// stubContainerRuntime — satisfies orchestration.ContainerRuntime
// ----------------------------------------------------------------------------

// stubContainerRuntime simulates an available container runtime without
// calling docker or podman.
type stubContainerRuntime struct {
	available bool
}

func (r *stubContainerRuntime) Run(_ context.Context, _ *orchestration.ContainerSpec) (string, error) {
	return "stub-container-id", nil
}

func (r *stubContainerRuntime) RunInteractive(_ context.Context, _ *orchestration.ContainerSpec) (*orchestration.InteractiveProcess, error) {
	return nil, nil
}

func (r *stubContainerRuntime) Stop(_ context.Context, _ string) error { return nil }

// Exec satisfies the runtime seam. Nothing in this test runs a command
// inside a container; a call would be a test asserting behaviour it never
// arranged, so it refuses rather than answering.
func (r *stubContainerRuntime) Exec(context.Context, string, *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	return nil, &orchestration.ContainerExecError{ContainerID: "fake", Cause: errNoExecInThisTest}
}
func (r *stubContainerRuntime) Remove(_ context.Context, _ string) error { return nil }
func (r *stubContainerRuntime) Logs(_ context.Context, _ string) (io.ReadCloser, error) {
	return nil, nil
}
func (r *stubContainerRuntime) IsRunning(_ context.Context, _ string) (bool, error) { return true, nil }
func (r *stubContainerRuntime) IsAvailable() bool                                   { return r.available }
func (r *stubContainerRuntime) Pull(_ context.Context, _ string) error              { return nil }

// ----------------------------------------------------------------------------
// buildOllamaBackend creates a real OllamaBackend pointed at the test
// environment's Ollama service. Only call when env.ollamaAvail is true.
func buildOllamaBackend(t *testing.T) *orchestration.OllamaBackend {
	t.Helper()
	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    env.ollamaURL,
		MaxRetries: 2,
	})
	chatSvc := ollama.NewChatService(client, env.ollamaModel)
	return orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
		ChatService:  chatSvc,
		DefaultModel: env.ollamaModel,
	})
}

// ----------------------------------------------------------------------------
// mockCentral — in-process HTTP server standing in for the QuicBaaS central
// ----------------------------------------------------------------------------

// enrollRequest mirrors the Phase 3 enrollment wire format. The exact field
// names must be honored by enrollment.go (Phase 3 subagent).
//
// PHASE-3-DEP: this struct mirrors what NewProductionEnroller POSTs to
// /api/v1/agents/enroll. Phase 3 must use these JSON field names on the wire.
type enrollRequest struct {
	TenantID      string `json:"tenant_id"`
	BindAddr      string `json:"bind_addr"`
	SandboxPolicy string `json:"sandbox_policy"`
}

// enrollResponse mirrors the Phase 3 enrollment response.
//
// PHASE-3-DEP: NewProductionEnroller must decode this JSON and populate
// an orchestration.AgentIdentity from it.
type enrollResponse struct {
	AgentID          string   `json:"agent_id"`
	ServiceAccountID string   `json:"service_account_id"`
	ClientID         string   `json:"client_id"`
	ClientSecret     string   `json:"client_secret"`
	OIDCSubject      string   `json:"oidc_subject"`
	Role             string   `json:"role"`
	Scopes           []string `json:"scopes"`
}

// tokenResponse mirrors the OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// mockCentral is an in-process httptest.Server that mimics the subset of the
// QuicBaaS central API exercised by the fleet node daemon lifecycle.
type mockCentral struct {
	server         *httptest.Server
	enrollCount    atomic.Int32
	heartbeatCount atomic.Int32

	// enrolledCh is closed when the first successful enroll completes so
	// tests can synchronise without polling.
	enrolledCh chan struct{}

	// tenantID is the tenant this mock central accepts enrollments for.
	tenantID string
}

// newMockCentral creates and starts an in-process mock central server.
func newMockCentral(t *testing.T, tenantID string) *mockCentral {
	t.Helper()
	mc := &mockCentral{
		tenantID:   tenantID,
		enrolledCh: make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/enroll", mc.handleEnroll)
	mux.HandleFunc("/api/v1/agents/deregister", mc.handleDeregister)
	mux.HandleFunc("/api/v1/agents/heartbeat", mc.handleHeartbeat)
	mux.HandleFunc("/oauth/token", mc.handleToken)
	mux.HandleFunc("/api/v1/health/ready", mc.handleReady)

	mc.server = httptest.NewServer(mux)
	t.Cleanup(mc.server.Close)

	return mc
}

func (mc *mockCentral) URL() string { return mc.server.URL }

func (mc *mockCentral) handleReady(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (mc *mockCentral) handleToken(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := tokenResponse{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (mc *mockCentral) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.TenantID != mc.tenantID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	count := mc.enrollCount.Add(1)
	// Signal first enrollment via channel (idempotent close guard).
	if count == 1 {
		close(mc.enrolledCh)
	}

	w.Header().Set("Content-Type", "application/json")
	resp := enrollResponse{
		AgentID:          "agent-" + req.TenantID,
		ServiceAccountID: "sa-" + req.TenantID,
		ClientID:         "client-" + req.TenantID,
		ClientSecret:     "secret-test",
		OIDCSubject:      "system:agent:" + req.TenantID,
		Role:             "worker",
		Scopes:           []string{"orchestrator:*", "kms:read"},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (mc *mockCentral) handleDeregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (mc *mockCentral) handleHeartbeat(w http.ResponseWriter, _ *http.Request) {
	mc.heartbeatCount.Add(1)
	w.WriteHeader(http.StatusNoContent)
}

// waitEnrolled blocks until the first enrollment is accepted or the context
// is cancelled. Never uses time.Sleep.
func (mc *mockCentral) waitEnrolled(ctx context.Context) bool {
	select {
	case <-mc.enrolledCh:
		return true
	case <-ctx.Done():
		return false
	}
}

// ----------------------------------------------------------------------------
// testDaemon — wires a minimal Daemon with stub dependencies
// ----------------------------------------------------------------------------

// testDaemonConfig holds all overridable pieces for building a test daemon.
type testDaemonConfig struct {
	centralURL     string
	tenantID       string
	sandboxPolicy  orchestration.SandboxPolicy
	hasContainerRT bool
	enrollErr      error
}

// newTestDaemon builds a daemon pre-wired with stub dependencies. The caller
// is responsible for calling Stop when the test finishes.
func newTestDaemon(t *testing.T, cfg testDaemonConfig) (*fleet.Daemon, *stubEnroller, *stubHeartbeat, *stubSpawnServer) {
	t.Helper()

	enroller := &stubEnroller{
		identity: &orchestration.AgentIdentity{
			AgentID:          "test-agent-id",
			ServiceAccountID: "test-sa-id",
			TenantID:         cfg.tenantID,
			Role:             "worker",
			Scopes:           []string{"orchestrator:*"},
			ClientID:         "test-client-id",
		},
		err: cfg.enrollErr,
	}

	hb := newStubHeartbeat(16)

	spawnAddr := "127.0.0.1:0"
	spawn := newStubSpawnServer(spawnAddr)

	runtime, err := fleet.NewRuntime(fleet.RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"acp": &stubAgentBackend{backendType: "acp"},
		},
		ContainerRuntime: &stubContainerRuntime{available: cfg.hasContainerRT},
		SandboxPolicy:    cfg.sandboxPolicy,
		ImageMap:         map[string]string{"acp": "ghcr.io/test/acp:latest"},
		Logger:           &testLogger{t: t},
	})
	require.NoError(t, err)

	workDir := t.TempDir()

	daemonCfg := withAgentIdentity(t, fleet.Config{
		ServerAddr:    cfg.centralURL,
		TenantID:      cfg.tenantID,
		BindAddr:      "127.0.0.1:0",
		SandboxPolicy: cfg.sandboxPolicy,
		WorkDir:       workDir,
	})

	d, err := fleet.NewDaemon(daemonCfg, fleet.DaemonDeps{
		Runtime:     runtime,
		Enroller:    enroller,
		Heartbeat:   hb,
		SpawnServer: spawn,
		Logger:      &testLogger{t: t},
	})
	require.NoError(t, err)

	return d, enroller, hb, spawn
}

// withAgentIdentity fills cfg's certificate fields with fresh material and
// returns it. Enrollment is certificate-based -- the daemon authenticates to
// central with an mTLS client certificate and verifies central against a CA --
// so fleet.NewDaemon refuses a Config that names none of the three files.
func withAgentIdentity(t *testing.T, cfg fleet.Config) *fleet.Config {
	t.Helper()
	cfg.NodeCertFile, cfg.NodeKeyFile, cfg.ServerCAFile = agentcert.Write(t, t.TempDir())
	return &cfg
}

// errNoExecInThisTest is what a fake container runtime answers when a test
// that arranged no container command is asked to run one. Answering an empty
// success instead would let a caller's command silently do nothing and the
// test pass on it.
var errNoExecInThisTest = errors.New("this fake runs no container commands")
