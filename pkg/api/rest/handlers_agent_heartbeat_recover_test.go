//go:build rest

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

package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Test Fixtures
// =============================================================================

// recoverTestLogger is a minimal recoverLogger backed by *testing.T.
type recoverTestLogger struct{ t *testing.T }

func (l *recoverTestLogger) Infof(format string, args ...interface{}) {
	l.t.Logf("INFO: "+format, args...)
}
func (l *recoverTestLogger) Warnf(format string, args ...interface{}) {
	l.t.Logf("WARN: "+format, args...)
}

// recoverTestRegistrar is a thread-safe in-memory iam.AgentRegistrar
// recording every RegisterAgent call. Lets tests assert the recoverer
// re-published the right row without spinning up the full orchestration
// stack.
type recoverTestRegistrar struct {
	mu      sync.Mutex
	calls   []*iam.AgentRegistrarRequest
	failNth int32 // -1 = never; 0 = always; n>0 = nth call fails
	count   atomic.Int32
	err     error
}

func (r *recoverTestRegistrar) RegisterAgent(_ context.Context, req *iam.AgentRegistrarRequest) error {
	n := r.count.Add(1)
	if r.failNth == 0 || (r.failNth > 0 && n == r.failNth) {
		return r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return nil
}

func (r *recoverTestRegistrar) snapshot() []*iam.AgentRegistrarRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*iam.AgentRegistrarRequest, len(r.calls))
	copy(out, r.calls)
	return out
}

// recoveryAgentService is a stub that returns AgentNotFoundError for
// the first N AgentHeartbeat calls and nil thereafter. The recoverer
// gets a chance to re-publish the agent between the two calls, so the
// second call (the retry) succeeds.
type recoveryAgentService struct {
	*mockAgentService
	failsRemaining atomic.Int32
}

func (m *recoveryAgentService) AgentHeartbeat(_ context.Context, tenantID, agentID string) error {
	if m.failsRemaining.Add(-1) >= 0 {
		return &orchestration.AgentNotFoundError{TenantID: tenantID, AgentID: agentID}
	}
	return nil
}

// recoveryServer is a wrapper exposing the recovery-mock agent service.
type recoveryServer struct {
	*testutil.MockQuicBaaS
	agentService orchestration.AgentService
}

func (m *recoveryServer) Agent() orchestration.AgentService { return m.agentService }

// recoverPeerCert mints a self-signed certificate carrying the OU=agents
// stamp + ClientAuth EKU + Organization=tenant the recoverer validates.
func recoverPeerCert(t *testing.T, tenantID, cn string) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(43),
		Subject: pkix.Name{
			CommonName:         cn,
			Organization:       []string{tenantID},
			OrganizationalUnit: []string{agentRegistrarOU},
		},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	return tmpl
}

// newRecoveryTestServer wires a Server with a heartbeat-mock agent
// service that fails N times, plus a recoverer backed by an
// in-memory registrar. Returns the test handles needed to drive the
// route under test.
func newRecoveryTestServer(t *testing.T, failsBeforeRecover int32) (*Server, *recoveryAgentService, *recoverTestRegistrar) {
	t.Helper()

	agentSvc := &recoveryAgentService{mockAgentService: &mockAgentService{}}
	agentSvc.failsRemaining.Store(failsBeforeRecover)

	mockSvc := testutil.NewMockQuicBaaS()
	wrapper := &recoveryServer{
		MockQuicBaaS: mockSvc,
		agentService: agentSvc,
	}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, err := NewServer(wrapper, cfg, log)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	registrar := &recoverTestRegistrar{failNth: -1}
	recoverer, err := NewIAMHeartbeatRecoverer(IAMHeartbeatRecovererDeps{
		Registrar: registrar,
		Logger:    &recoverTestLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewIAMHeartbeatRecoverer: %v", err)
	}
	srv.SetAgentHeartbeatRecoverer(recoverer)
	return srv, agentSvc, registrar
}

// makeHeartbeatRequest builds a heartbeat POST that goes through the
// chi router so chi.URLParam picks up the agentID. The TLS state is
// attached on the inbound request so the handler can read peer certs.
func makeHeartbeatRequest(t *testing.T, srv *Server, tenantID, agentID string, peer *x509.Certificate) *httptest.ResponseRecorder {
	t.Helper()
	return makeHeartbeatRequestWithBody(t, srv, tenantID, agentID, peer, "")
}

// makeHeartbeatRequestWithBody is the variant used by the bind-addr
// test cases — the heartbeat body's bind_addr is what drives the
// recovery's host derivation, so a few tests need to vary it.
func makeHeartbeatRequestWithBody(t *testing.T, srv *Server, tenantID, agentID string, peer *x509.Certificate, body string) *httptest.ResponseRecorder {
	t.Helper()

	router := chi.NewRouter()
	router.Post("/api/v1/orchestrator/agents/{agentID}/heartbeat", srv.HandleOrchestratorAgentHeartbeat)

	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	var req *http.Request
	if reqBody != nil {
		req = httptest.NewRequest(http.MethodPost,
			"/api/v1/orchestrator/agents/"+agentID+"/heartbeat?tenant_id="+tenantID, reqBody)
	} else {
		req = httptest.NewRequest(http.MethodPost,
			"/api/v1/orchestrator/agents/"+agentID+"/heartbeat?tenant_id="+tenantID, nil)
	}
	req.Host = "198.51.100.91:18080"
	req.Header.Set("Content-Type", "application/json")
	if peer != nil {
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{peer}}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// =============================================================================
// Tests — happy path: heartbeat for unknown agent triggers re-register
// =============================================================================

// TestHeartbeat_AutoRegistersOnUnknownID asserts that a heartbeat for
// an agent the registry has lost is auto-recovered when the request
// carries a valid mTLS peer cert whose (tenant, CN) maps to the
// claimed agentID. The handler must:
//   - Return 200 with the standard heartbeat payload.
//   - Have asked the registrar to publish the agent exactly once.
//   - Have re-issued the AgentHeartbeat call against the live row.
func TestHeartbeat_AutoRegistersOnUnknownID(t *testing.T) {
	const tenantID = "acme"
	const cn = "198.51.100.91"
	agentID := iam.AgentAppName(tenantID, cn)

	srv, _, registrar := newRecoveryTestServer(t, 1) // fail once, then succeed

	rec := makeHeartbeatRequest(t, srv, tenantID, agentID, recoverPeerCert(t, tenantID, cn))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["heartbeat"] != true {
		t.Errorf("expected heartbeat=true, got %+v", resp)
	}
	if resp["agent_id"] != agentID {
		t.Errorf("expected agent_id=%q, got %v", agentID, resp["agent_id"])
	}

	calls := registrar.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 registrar call, got %d", len(calls))
	}
	got := calls[0]
	if got.AgentID != agentID || got.TenantID != tenantID {
		t.Errorf("wrong registrar payload: %+v", got)
	}
	if got.Metadata["recovered"] != "true" {
		t.Errorf("recovery metadata missing: %+v", got.Metadata)
	}
	if !strings.Contains(got.Host, cn) {
		t.Errorf("host should derive from cert CN %q, got %q", cn, got.Host)
	}
}

// =============================================================================
// Tests — defence: missing cert / mismatched cert / mismatched tenant
// =============================================================================

// TestHeartbeat_RejectsUnknownIDWithoutCert asserts the handler returns
// 404 when the heartbeat targets an unknown agent and the request
// presents no peer certificate. The recoverer cannot authenticate the
// agent and refuses to re-publish.
func TestHeartbeat_RejectsUnknownIDWithoutCert(t *testing.T) {
	const tenantID = "acme"
	agentID := iam.AgentAppName(tenantID, "198.51.100.91")

	srv, _, registrar := newRecoveryTestServer(t, 100) // always fail

	rec := makeHeartbeatRequest(t, srv, tenantID, agentID, nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	if calls := registrar.snapshot(); len(calls) != 0 {
		t.Errorf("registrar must not be called without a cert, got %d", len(calls))
	}
}

// TestHeartbeat_RejectsUnknownIDWithMismatchedCert asserts the handler
// returns 404 when the cert's (tenant, CN) does not derive the
// claimed agentID. A stolen cert issued for daemon-A must not let an
// attacker heartbeat as daemon-B.
func TestHeartbeat_RejectsUnknownIDWithMismatchedCert(t *testing.T) {
	const tenantID = "acme"
	// Cert is for "agent-a", request claims "agent-b" → mismatch.
	certForA := recoverPeerCert(t, tenantID, "agent-a")
	requestedID := iam.AgentAppName(tenantID, "agent-b")

	srv, _, registrar := newRecoveryTestServer(t, 100)

	rec := makeHeartbeatRequest(t, srv, tenantID, requestedID, certForA)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on cert mismatch, got %d body=%s", rec.Code, rec.Body.String())
	}
	if calls := registrar.snapshot(); len(calls) != 0 {
		t.Errorf("registrar must not be called on cert mismatch, got %d calls", len(calls))
	}
}

// TestHeartbeat_RejectsUnknownIDWithWrongTenant asserts a cert minted
// for tenant-X cannot recover an agent claimed under tenant-Y, even
// when the CN matches.
func TestHeartbeat_RejectsUnknownIDWithWrongTenant(t *testing.T) {
	cert := recoverPeerCert(t, "tenant-x", "198.51.100.91")
	requestedID := iam.AgentAppName("tenant-y", "198.51.100.91")

	srv, _, registrar := newRecoveryTestServer(t, 100)

	rec := makeHeartbeatRequest(t, srv, "tenant-y", requestedID, cert)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on tenant mismatch, got %d", rec.Code)
	}
	if calls := registrar.snapshot(); len(calls) != 0 {
		t.Errorf("registrar must not be called on tenant mismatch")
	}
}

// TestHeartbeat_RejectsUnknownIDWithMissingOU asserts a cert that does
// not carry the canonical OU=agents stamp is refused even when the
// (tenant, CN) would otherwise derive a matching agentID. This blocks
// any cert minted for a different role from driving recovery.
func TestHeartbeat_RejectsUnknownIDWithMissingOU(t *testing.T) {
	const tenantID = "acme"
	const cn = "198.51.100.91"
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(99),
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{tenantID},
			// No OU stamp.
		},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	agentID := iam.AgentAppName(tenantID, cn)

	srv, _, registrar := newRecoveryTestServer(t, 100)
	rec := makeHeartbeatRequest(t, srv, tenantID, agentID, cert)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on missing OU, got %d", rec.Code)
	}
	if calls := registrar.snapshot(); len(calls) != 0 {
		t.Errorf("registrar must not be called without OU=agents")
	}
}

// =============================================================================
// Tests — recoverer construction
// =============================================================================

// TestNewIAMHeartbeatRecoverer_ValidatesDeps asserts the constructor
// rejects nil dependencies with typed sentinel errors so a
// misconfigured server fails fast.
func TestNewIAMHeartbeatRecoverer_ValidatesDeps(t *testing.T) {
	cases := []struct {
		name string
		deps IAMHeartbeatRecovererDeps
		want error
	}{
		{
			name: "no_registrar",
			deps: IAMHeartbeatRecovererDeps{Logger: &recoverTestLogger{t: t}},
			want: ErrHeartbeatRecoverNoRegistrar,
		},
		{
			name: "no_logger",
			deps: IAMHeartbeatRecovererDeps{Registrar: &recoverTestRegistrar{}},
			want: ErrHeartbeatRecoverNoLogger,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewIAMHeartbeatRecoverer(tc.deps)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// TestRecoverer_NoRegistrarSetIsNoop asserts that when no recoverer is
// wired on the server, the heartbeat handler preserves the original
// 404 contract — recovery is opt-in.
func TestRecoverer_NoRegistrarSetIsNoop(t *testing.T) {
	const tenantID = "acme"
	const cn = "198.51.100.91"
	agentID := iam.AgentAppName(tenantID, cn)

	// Build a server with the recovery-mock service but DO NOT wire
	// the recoverer.
	agentSvc := &recoveryAgentService{mockAgentService: &mockAgentService{}}
	agentSvc.failsRemaining.Store(100)

	mockSvc := testutil.NewMockQuicBaaS()
	wrapper := &recoveryServer{MockQuicBaaS: mockSvc, agentService: agentSvc}
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, err := NewServer(wrapper, cfg, log)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	rec := makeHeartbeatRequest(t, srv, tenantID, agentID, recoverPeerCert(t, tenantID, cn))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 with no recoverer wired, got %d", rec.Code)
	}
}

// TestHeartbeat_AutoRegisterUsesBindAddrFromBody asserts that when the
// agent's heartbeat body carries a concrete bind_addr (the daemon
// reports its spawn-server address on every tick), the recovered
// registry entry's Host is set to that bind_addr verbatim — not to the
// port-0 fallback derived from the cert. This is what makes inference
// dispatch work after a recovery: the orchestrator dials the agent's
// real port, not 0.
func TestHeartbeat_AutoRegisterUsesBindAddrFromBody(t *testing.T) {
	const tenantID = "acme"
	const cn = "198.51.100.91"
	agentID := iam.AgentAppName(tenantID, cn)

	srv, _, registrar := newRecoveryTestServer(t, 1)

	body := `{"active_sessions":0,"sandbox_policy":"required","backends":["ollama"],"has_container_runtime":true,"sequence":1,"bind_addr":"198.51.100.91:9091"}`
	rec := makeHeartbeatRequestWithBody(t, srv, tenantID, agentID, recoverPeerCert(t, tenantID, cn), body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	calls := registrar.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 registrar call, got %d", len(calls))
	}
	got := calls[0]
	if got.Host != "198.51.100.91:9091" {
		t.Errorf("expected Host=%q, got %q", "198.51.100.91:9091", got.Host)
	}
	if len(got.Backends) != 1 || got.Backends[0] != "ollama" {
		t.Errorf("expected Backends=[ollama], got %v", got.Backends)
	}
}

// TestHeartbeat_AutoRegisterRewritesWildcardBindAddr asserts that when
// a daemon bound to 0.0.0.0:9091 (the fleet-mode default) sends its
// heartbeat with that wildcard bind_addr, the recovery rewrites the
// host component to a dialable address (the cert CN's IP) while
// preserving the port.
func TestHeartbeat_AutoRegisterRewritesWildcardBindAddr(t *testing.T) {
	const tenantID = "acme"
	const cn = "198.51.100.91"
	agentID := iam.AgentAppName(tenantID, cn)

	srv, _, registrar := newRecoveryTestServer(t, 1)
	body := `{"bind_addr":"0.0.0.0:9091"}`
	rec := makeHeartbeatRequestWithBody(t, srv, tenantID, agentID, recoverPeerCert(t, tenantID, cn), body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	calls := registrar.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 registrar call, got %d", len(calls))
	}
	if got := calls[0].Host; got != "198.51.100.91:9091" {
		t.Errorf("expected wildcard rewritten to %q, got %q", "198.51.100.91:9091", got)
	}
}
