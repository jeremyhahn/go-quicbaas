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
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// dialableEventIngestAddr — wildcard host rewrite contract
// =============================================================================

// TestDialableEventIngestAddr_Concrete asserts that a configured URL
// with a non-wildcard host is returned verbatim. Operators who set the
// gRPC bind to a private IP must see that exact value advertised so
// agents reaching the daemon over a different ingress (e.g., VPN) get
// the operator's intended target.
func TestDialableEventIngestAddr_Concrete(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"ipv4_loopback", "https://127.0.0.1:19090"},
		{"ipv4_lan", "https://198.51.100.91:19090"},
		{"dns_name", "https://daemon.example.com:19090"},
		{"ipv6_bracketed", "https://[fd00::1]:19090"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Host = "example.com:18080"
			got := dialableEventIngestAddr(tc.in, r)
			if got != tc.in {
				t.Fatalf("expected concrete URL %q passed through, got %q", tc.in, got)
			}
		})
	}
}

// TestDialableEventIngestAddr_RewritesWildcard asserts that a configured
// URL with a wildcard host (the default for daemons that bind to
// 0.0.0.0 so multiple LAN clients can reach it) is rewritten to the
// agent's view of the daemon — the request's Host header. The
// configured port is preserved.
func TestDialableEventIngestAddr_RewritesWildcard(t *testing.T) {
	cases := []struct {
		name      string
		configIn  string
		reqHost   string
		expectOut string
	}{
		{
			name:      "ipv4_wildcard_request_with_port",
			configIn:  "https://0.0.0.0:19090",
			reqHost:   "198.51.100.91:18080",
			expectOut: "https://198.51.100.91:19090",
		},
		{
			name:      "ipv4_wildcard_request_without_port",
			configIn:  "https://0.0.0.0:19090",
			reqHost:   "daemon.example.com",
			expectOut: "https://daemon.example.com:19090",
		},
		{
			name:      "ipv6_wildcard",
			configIn:  "https://[::]:19090",
			reqHost:   "[fd00::1]:18080",
			expectOut: "https://[fd00::1]:19090",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Host = tc.reqHost
			got := dialableEventIngestAddr(tc.configIn, r)
			if got != tc.expectOut {
				t.Fatalf("expected %q, got %q", tc.expectOut, got)
			}
		})
	}
}

// TestDialableEventIngestAddr_EmptyOrInvalid asserts the helper returns
// the empty string when the daemon has no gRPC URL configured or when
// the configured URL is unparseable. Returning empty (rather than
// guessing) keeps the agent's "missing addr → forwarding off" branch
// deterministic so operators do not see the daemon dial wrong ports.
func TestDialableEventIngestAddr_EmptyOrInvalid(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Host = "example.com:18080"

	if got := dialableEventIngestAddr("", r); got != "" {
		t.Fatalf("empty configured URL should return \"\", got %q", got)
	}
	if got := dialableEventIngestAddr("   ", r); got != "" {
		t.Fatalf("whitespace-only URL should return \"\", got %q", got)
	}
	// A bare "https://" has no host — refuse to advertise.
	if got := dialableEventIngestAddr("https://", r); got != "" {
		t.Fatalf("URL with no host should return \"\", got %q", got)
	}
	// Wildcard with empty request host (impossible in HTTP/1.1, but
	// guard anyway) returns empty rather than emitting "https://:port".
	r2 := httptest.NewRequest(http.MethodPost, "/", nil)
	r2.Host = ""
	if got := dialableEventIngestAddr("https://0.0.0.0:19090", r2); got != "" {
		t.Fatalf("wildcard config with empty req host should return \"\", got %q", got)
	}
}

// =============================================================================
// HandleAgentRegister — response shape (event_ingest_addr present)
// =============================================================================

// agentRegisterTestService wraps testutil.MockQuicBaaS and adds the
// GetAppManager and GetAppStorage methods the register handler requires via type
// assertion. testutil.MockQuicBaaS itself does not implement the
// agentAppManagerAccessor interface, so the handler short-circuits to
// 500 without this wrapper.
type agentRegisterTestService struct {
	*testutil.MockQuicBaaS
	appManager *iam.AppManager
	appStorage iam.AppStorage
}

// Compile-time check that the wrapper satisfies the service contract.
var _ service.QuicBaaS = (*agentRegisterTestService)(nil)

// GetAppManager satisfies the unexported agentAppManagerAccessor
// interface the register handler type-asserts on.
func (s *agentRegisterTestService) GetAppManager(ctx context.Context) (*iam.AppManager, error) {
	return s.appManager, nil
}

// GetAppStorage satisfies the unexported agentAppManagerAccessor
// interface the register handler type-asserts on for the mTLS registration path.
func (s *agentRegisterTestService) GetAppStorage(ctx context.Context) (iam.AppStorage, error) {
	return s.appStorage, nil
}

// agentRegisterTestTenantManager is a minimal in-memory TenantManager
// satisfying iam.TenantManager. AppManager.RegisterApp consults
// CheckQuota which we always allow here (test path).
type agentRegisterTestTenantManager struct{}

func (m *agentRegisterTestTenantManager) CreateTenant(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
	return &iam.Tenant{ID: opts.ID, Name: opts.Name}, nil
}
func (m *agentRegisterTestTenantManager) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	return &iam.Tenant{ID: id}, nil
}
func (m *agentRegisterTestTenantManager) UpdateTenant(ctx context.Context, id string, opts iam.UpdateTenantOptions) (*iam.Tenant, error) {
	return &iam.Tenant{ID: id}, nil
}
func (m *agentRegisterTestTenantManager) DeleteTenant(ctx context.Context, id string) error {
	return nil
}
func (m *agentRegisterTestTenantManager) ListTenants(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error) {
	return nil, nil
}
func (m *agentRegisterTestTenantManager) CheckQuota(ctx context.Context, tenantID, resourceType string) error {
	return nil
}
func (m *agentRegisterTestTenantManager) UpdateEngineQuotas(_ context.Context, _ string, _ qrdbsdk.Quotas) error {
	return nil
}
func (m *agentRegisterTestTenantManager) UpdateEngineEntitlements(_ context.Context, _ string, _ qrdbsdk.Entitlements) error {
	return nil
}

// newAgentRegisterTestServer wires a Server backed by a real
// in-memory iam.AppManager so the register handler can mint OAuth2
// credentials end-to-end. eventIngestURL is set via the production
// SetEventIngestURL setter to mirror the lifecycle wiring path.
func newAgentRegisterTestServer(t *testing.T, eventIngestURL string) *Server {
	t.Helper()

	storage := iam.NewMemoryStorage()
	tenantMgr := &agentRegisterTestTenantManager{}
	appMgr := iam.NewAppManager(storage, tenantMgr)

	mockSvc := testutil.NewMockQuicBaaS()
	wrapper := &agentRegisterTestService{
		MockQuicBaaS: mockSvc,
		appManager:   appMgr,
		appStorage:   storage,
	}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json"})
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.Address = ":0"
	srv, err := NewServer(wrapper, cfg, log)
	require.NoError(t, err)

	srv.SetEventIngestURL(eventIngestURL)
	return srv
}

// agentRegisterTestPeerCert mints a self-signed certificate carrying
// the OU=agents stamp + ClientAuth EKU + Organization=tenant the
// register handler validates. Self-signed is fine here — the handler
// authenticates by inspecting the parsed cert directly, not by
// verifying the chain.
func agentRegisterTestPeerCert(t *testing.T, tenantID, cn string) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject: pkix.Name{
			CommonName:         cn,
			Organization:       []string{tenantID},
			OrganizationalUnit: []string{agentRegistrarOU},
		},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	return tmpl
}

// TestHandleAgentRegister_AdvertisesEventIngestAddr asserts the
// handler returns the configured event ingest URL verbatim in the
// response when the daemon's bind is a concrete address. This is the
// happy path that protects against a regression to the old behaviour
// (response had no event_ingest_addr; agent dialed the REST host and
// failed h2 ALPN).
func TestHandleAgentRegister_AdvertisesEventIngestAddr(t *testing.T) {
	const ingestURL = "https://198.51.100.91:19090"
	srv := newAgentRegisterTestServer(t, ingestURL)

	body := `{"tenant_id":"acme","bind_addr":"192.168.1.50:9091","hostname":"agent01"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
	req.Host = "198.51.100.91:18080"
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			agentRegisterTestPeerCert(t, "acme", "agent01"),
		},
	}

	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp agentRegisterResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if resp.EventIngestAddr != ingestURL {
		t.Fatalf("expected event_ingest_addr=%q, got %q", ingestURL, resp.EventIngestAddr)
	}
	if resp.ClientID == "" || resp.ClientSecret == "" {
		t.Fatalf("expected client credentials populated, got %+v", resp)
	}
}

// TestHandleAgentRegister_RewritesWildcardIngestAddr asserts that when
// the operator binds gRPC to 0.0.0.0 (the default for fleet-mode
// daemons that need to accept connections from multiple LAN agents),
// the handler rewrites the host to the request's Host header — the
// dialable address the agent already proved it can reach. Without this
// rewrite, the daemon would advertise "https://0.0.0.0:19090" and the
// agent would fail to dial.
func TestHandleAgentRegister_RewritesWildcardIngestAddr(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "https://0.0.0.0:19090")

	body := `{"tenant_id":"acme","bind_addr":"10.0.0.5:9091","hostname":"agent02"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
	req.Host = "198.51.100.91:18080"
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			agentRegisterTestPeerCert(t, "acme", "agent02"),
		},
	}

	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp agentRegisterResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	const expect = "https://198.51.100.91:19090"
	if resp.EventIngestAddr != expect {
		t.Fatalf("expected wildcard rewritten to %q, got %q", expect, resp.EventIngestAddr)
	}
}

// TestHandleAgentRegister_OmitsEventIngestAddr asserts the handler
// omits event_ingest_addr from the response when the daemon has no
// gRPC ingest URL configured (e.g., REST-only deployment). The agent
// then disables event forwarding rather than dialing a wrong port.
// Both literal absence and JSON-omitempty semantics are validated.
func TestHandleAgentRegister_OmitsEventIngestAddr(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")

	body := `{"tenant_id":"acme","bind_addr":"10.0.0.7:9091","hostname":"agent03"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
	req.Host = "198.51.100.91:18080"
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			agentRegisterTestPeerCert(t, "acme", "agent03"),
		},
	}

	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	// Decode into a generic map so we can assert the field is absent
	// (omitempty), not merely empty after struct-decode.
	raw := map[string]interface{}{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, present := raw["event_ingest_addr"]; present {
		t.Fatalf("event_ingest_addr must be omitted when unconfigured, got: %s", w.Body.String())
	}
}

// TestAgentRegister_ReturnsMTLSClientID asserts that when AppStorage is
// available, the register handler creates a secondary self_signed_tls_client_auth
// application and returns its client_id as mtls_client_id in the response.
// This enables new agents to obtain tokens without a client_secret on disk.
func TestAgentRegister_ReturnsMTLSClientID(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")

	body := `{"tenant_id":"acme","bind_addr":"10.0.0.9:9091","hostname":"agent-mtls"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
	req.Host = "198.51.100.91:18080"
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			agentRegisterTestPeerCert(t, "acme", "agent-mtls"),
		},
	}

	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}

	var resp agentRegisterResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}

	// Legacy fields must be present for backward compatibility.
	if resp.ClientID == "" {
		t.Error("expected client_id to be set")
	}
	if resp.ClientSecret == "" {
		t.Error("expected client_secret to be set")
	}

	// mtls_client_id must be present when AppStorage is wired.
	if resp.MTLSClientID == "" {
		t.Fatalf("expected mtls_client_id to be set, got empty (response: %s)", w.Body.String())
	}
	if resp.MTLSClientID == resp.ClientID {
		t.Errorf("mtls_client_id should differ from client_id, both were %q", resp.ClientID)
	}
}
