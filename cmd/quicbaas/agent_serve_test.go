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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestStripURLScheme covers the URL-to-grpc-target normaliser used by
// the agent serve loop to convert the persisted "event_ingest_addr"
// (a full https URL set by the server) into the "host:port" form
// google.golang.org/grpc.NewClient consumes. Pass-through behaviour
// for already-normalised strings keeps the helper safe against
// either input shape.
func TestStripURLScheme(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https_url", "https://198.51.100.91:19090", "198.51.100.91:19090"},
		{"http_url", "http://198.51.100.91:19090", "198.51.100.91:19090"},
		{"url_with_trailing_slash", "https://daemon.example.com:19090/", "daemon.example.com:19090"},
		{"already_host_port", "198.51.100.91:19090", "198.51.100.91:19090"},
		{"empty", "", ""},
		{"whitespace", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripURLScheme(tc.in)
			if got != tc.want {
				t.Fatalf("stripURLScheme(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// captureLogger is a minimal interfaces.Logger that records Warnf/
// Infof messages so tests can assert on what the daemon serve path
// logged when event forwarding is disabled vs. started.
type captureLogger struct {
	agentServeLogger
	warns atomic.Pointer[[]string]
	infos atomic.Pointer[[]string]
}

func newCaptureLogger() *captureLogger {
	cl := &captureLogger{}
	w := []string{}
	i := []string{}
	cl.warns.Store(&w)
	cl.infos.Store(&i)
	return cl
}

func (c *captureLogger) Warnf(format string, args ...interface{}) {
	cur := c.warns.Load()
	updated := append(*cur, formatLog(format, args...))
	c.warns.Store(&updated)
}

func (c *captureLogger) Infof(format string, args ...interface{}) {
	cur := c.infos.Load()
	updated := append(*cur, formatLog(format, args...))
	c.infos.Store(&updated)
}

// formatLog mirrors the agentServeLogger output without writing to
// stderr so tests can introspect logger calls without leaking lines
// into the test runner output.
func formatLog(format string, args ...interface{}) string {
	const maxArgs = 16
	if len(args) > maxArgs {
		args = args[:maxArgs]
	}
	out := format
	for _, a := range args {
		switch v := a.(type) {
		case string:
			out += " " + v
		case error:
			out += " " + v.Error()
		default:
			b, _ := json.Marshal(v)
			out += " " + string(b)
		}
	}
	return out
}

// writeTestCredentialsForServe writes a minimal credentials.json
// honouring the on-disk schema the production enroller persists,
// then returns a config pointed at that work dir. Using the on-disk
// path lets startEventForwarder consult ProductionEnroller's
// EventIngestAddrFromDisk without needing a live HTTP server.
func writeTestCredentialsForServe(t *testing.T, ingestAddr string) *agent.Config {
	t.Helper()
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "agent")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	credsPath := filepath.Join(workDir, "credentials.json")
	body := map[string]interface{}{
		"client_id":          "client-x",
		"client_secret":      "secret-x",
		"agent_id":           "agent-x",
		"service_account_id": "sa-x",
		"role":               "agent-runtime",
		"scopes":             []string{"agent:runtime:spawn"},
	}
	if ingestAddr != "" {
		body["event_ingest_addr"] = ingestAddr
	}
	enc, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(credsPath, enc, 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	return &agent.Config{
		ServerAddr: "https://localhost:18080",
		TenantID:   "tenant-a",
		WorkDir:    workDir,
	}
}

// TestStartEventForwarder_DisablesWhenIngestAddrAbsent asserts that a
// daemon enrollment that did NOT advertise event_ingest_addr (older
// server, REST-only deployment) results in the forwarder NOT being
// installed on the runtime — and a clear warning logged. This is the
// "atomic cut" path: rather than dialing a fallback port that fails
// h2 ALPN, the daemon silently disables event forwarding and surfaces
// it to operators via the warn log.
func TestStartEventForwarder_DisablesWhenIngestAddrAbsent(t *testing.T) {
	t.Parallel()

	cfg := writeTestCredentialsForServe(t, "")
	enroller := agent.NewProductionEnroller(agent.ProductionEnrollerDeps{
		Logger: newAgentServeLogger(),
	})
	rt, err := agent.NewRuntime(agent.RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"stub": &serveStubBackend{}},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        newAgentServeLogger(),
		WorkDir:       cfg.WorkDir,
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	logger := newCaptureLogger()
	if err := startEventForwarder(context.Background(), enroller, cfg, rt, &noopTokenSource{}, logger); err != nil {
		t.Fatalf("startEventForwarder: %v", err)
	}

	// Forwarder install must be skipped on the empty-addr path. The
	// runtime exposes no public probe today; we assert via the warn
	// log the disabled-forwarding path emitted instead so a future
	// refactor that silently re-enables forwarding fails the test.
	warns := *logger.warns.Load()
	found := false
	for _, w := range warns {
		if containsAll(w, "event ingest", "disabled") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected warning about disabled event forwarding, got warns=%v", warns)
	}
}

// TestStartEventForwarder_MissingCredentialsIsTypedError asserts that
// invoking startEventForwarder before enrollment has produced
// credentials.json surfaces ErrEnrollmentMissingCredentials so the
// daemon serve loop can react with a precise log line rather than
// guessing at a generic IO error.
func TestStartEventForwarder_MissingCredentialsIsTypedError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	cfg := &agent.Config{
		ServerAddr: "https://localhost:18080",
		TenantID:   "tenant-a",
		WorkDir:    filepath.Join(tmp, "agent"),
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	enroller := agent.NewProductionEnroller(agent.ProductionEnrollerDeps{
		Logger: newAgentServeLogger(),
	})
	rt, err := agent.NewRuntime(agent.RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"stub": &serveStubBackend{}},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        newAgentServeLogger(),
		WorkDir:       cfg.WorkDir,
	})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	logger := newCaptureLogger()
	err = startEventForwarder(context.Background(), enroller, cfg, rt, &noopTokenSource{}, logger)
	if !errors.Is(err, agent.ErrEnrollmentMissingCredentials) {
		t.Fatalf("want ErrEnrollmentMissingCredentials, got %v", err)
	}
}

// noopTokenSource satisfies agent.TokenSource without performing any
// network IO — startEventForwarder only consults it when wiring the
// forwarder, which the disabled path skips.
type noopTokenSource struct{}

func (n *noopTokenSource) Token(ctx context.Context) (string, error) {
	return "", nil
}

func (n *noopTokenSource) Invalidate() {}

// containsAll reports whether every needle is present somewhere in
// haystack — keeps the warning-text assertion robust to formatting
// differences without anchoring on the entire string.
func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !contains(haystack, n) {
			return false
		}
	}
	return true
}

// contains is a strings.Contains replacement to avoid an extra import
// here when the test file already pulls in the common deps.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// serveStubBackend is the minimum AgentBackend implementation needed
// to satisfy NewRuntime in tests that only exercise enrollment + the
// event-forwarder helper. None of its methods are actually invoked.
type serveStubBackend struct{}

func (s *serveStubBackend) Spawn(ctx context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return nil, nil
}
func (s *serveStubBackend) Prompt(ctx context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return nil, nil
}
func (s *serveStubBackend) Status(ctx context.Context, sessionID string) (*orchestration.SessionStatus, error) {
	return nil, nil
}
func (s *serveStubBackend) Close(ctx context.Context, sessionID string) error {
	return nil
}
func (s *serveStubBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{}
}
func (s *serveStubBackend) Type() string         { return "stub" }
func (s *serveStubBackend) ToolGuidance() string { return "" }

// Compile-time assertion the stub satisfies the production interface
// so a future contract drift surfaces here, not in a test runtime.
var _ orchestration.AgentBackend = (*serveStubBackend)(nil)
