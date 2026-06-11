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
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func agentRegisterReq(t *testing.T, body string, cert *x509.Certificate) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
	req.Host = "192.168.1.1:18080"
	req.Header.Set("Content-Type", "application/json")
	if cert != nil {
		req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	}
	return req
}

func TestHandleAgentRegister_NoPeerCert(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")
	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, agentRegisterReq(t, `{"tenant_id":"acme"}`, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleAgentRegister_InvalidOU(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")
	// Cert without the OU=agents stamp.
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "agent01", Organization: []string{"acme"}},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, agentRegisterReq(t, `{"tenant_id":"acme"}`, cert))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleAgentRegister_InvalidJSON(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")
	cert := agentRegisterTestPeerCert(t, "acme", "agent01")
	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, agentRegisterReq(t, "{bad", cert))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAgentRegister_MissingTenant(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")
	cert := agentRegisterTestPeerCert(t, "acme", "agent01")
	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, agentRegisterReq(t, `{"tenant_id":""}`, cert))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAgentRegister_TenantMismatch(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")
	// Cert Organization=acme but request tenant=other -> 403.
	cert := agentRegisterTestPeerCert(t, "acme", "agent01")
	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, agentRegisterReq(t, `{"tenant_id":"other"}`, cert))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAgentRegister_CertMissingTenant(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")
	// Cert has the OU + EKU but no Organization -> tenant claim missing -> 401.
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "agent01", OrganizationalUnit: []string{agentRegistrarOU}},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, agentRegisterReq(t, `{"tenant_id":"acme"}`, cert))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// fakeAgentRegistrar captures the publish request.
type fakeAgentRegistrar struct {
	called bool
	req    *iam.AgentRegistrarRequest
	err    error
}

func (f *fakeAgentRegistrar) RegisterAgent(_ context.Context, req *iam.AgentRegistrarRequest) error {
	f.called = true
	f.req = req
	return f.err
}

// With an agent registrar wired, a successful registration publishes the agent
// into the fleet registry (exercising the dialable-host + metadata path).
func TestHandleAgentRegister_PublishesToRegistry(t *testing.T) {
	srv := newAgentRegisterTestServer(t, "")
	reg := &fakeAgentRegistrar{}
	srv.SetAgentRegistrar(reg)

	cert := agentRegisterTestPeerCert(t, "acme", "10.0.0.5")
	body := `{"tenant_id":"acme","bind_addr":"0.0.0.0:9091","hostname":"agent01","metadata":{"backends":"kv,blob"}}`
	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, agentRegisterReq(t, body, cert))

	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	require.True(t, reg.called, "registrar must be invoked")
	assert.Equal(t, "acme", reg.req.TenantID)
	assert.Equal(t, []string{"kv", "blob"}, reg.req.Backends)
}
