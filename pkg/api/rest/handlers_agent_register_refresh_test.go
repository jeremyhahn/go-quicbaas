//go:build rest

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

// handlers_agent_register_refresh_test.go covers the register handler's half
// of the re-registration contract (go-quicbaas#646): the registrar's refusals
// about WHO is registering reach the daemon, and the record names the
// principal and key the refresh is bounded to.

package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// refusingRegistrar answers every registration with a fixed error, standing in
// for the fleet registry's decision about the caller's identity.
type refusingRegistrar struct {
	err error
}

// RegisterAgent implements iam.AgentRegistrar.
func (r *refusingRegistrar) RegisterAgent(_ context.Context, _ *iam.AgentRegistrarRequest) error {
	return r.err
}

// registerRequestWithRegistrar drives one registration through the handler
// with the supplied registrar wired, returning the recorded response.
func registerRequestWithRegistrar(t *testing.T, registrar interface{}, cn string) *httptest.ResponseRecorder {
	t.Helper()

	srv := newAgentRegisterTestServer(t, "https://ingest.example:19090")
	srv.SetAgentRegistrar(registrar)

	body := `{"tenant_id":"acme","bind_addr":"10.0.0.5:9091","hostname":"` + cn + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
	req.Host = "central.example:18080"
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{agentRegisterTestPeerCert(t, "acme", cn)},
	}

	w := httptest.NewRecorder()
	srv.HandleAgentRegister(w, req)
	return w
}

// TestHandleAgentRegister_PrincipalMismatchIsRefused pins that an identity
// takeover is ANSWERED rather than logged. The agent id is a sanitised
// derivation of the certificate CommonName, so two subjects can address one
// record; the caller must be told it may not have that record instead of
// leaving with credentials and a silently unrefreshed registration.
func TestHandleAgentRegister_PrincipalMismatchIsRefused(t *testing.T) {
	w := registerRequestWithRegistrar(t, &refusingRegistrar{
		err: &orchestration.AgentPrincipalMismatchError{
			TenantID: "acme", AgentID: "agent-acme-agent01",
			Recorded: "agent.01", Presented: "agent 01",
		},
	}, "agent01")

	require.Equal(t, http.StatusConflict, w.Code, "body=%s", w.Body.String())

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "agent_id_conflict", resp["error"])
}

// TestHandleAgentRegister_DisabledAgentIsRefused keeps DisableAgent
// meaningful: a daemon an operator took out of service must not re-enroll its
// way back in, and must be told so.
func TestHandleAgentRegister_DisabledAgentIsRefused(t *testing.T) {
	w := registerRequestWithRegistrar(t, &refusingRegistrar{
		err: &orchestration.AgentDisabledError{TenantID: "acme", AgentID: "agent-acme-agent02"},
	}, "agent02")

	require.Equal(t, http.StatusForbidden, w.Code, "body=%s", w.Body.String())

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "agent_disabled", resp["error"])
}

// TestHandleAgentRegister_OperationalRegistrarFailureStaysNonFatal pins the
// other half: a registry that is merely broken must not cost the daemon its
// credentials, because the heartbeat recovery path re-publishes it.
func TestHandleAgentRegister_OperationalRegistrarFailureStaysNonFatal(t *testing.T) {
	w := registerRequestWithRegistrar(t, &refusingRegistrar{
		err: &orchestration.AgentRegistryError{
			Sentinel: orchestration.ErrAgentRegistryRequired,
			Message:  "store is down",
		},
	}, "agent03")

	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())
}

// TestAgentRegistrarRefusal covers the classifier's whole vocabulary: the two
// refusals it names and the operational failures it must not claim.
func TestAgentRegistrarRefusal(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err        error
		wantStatus int
		wantCode   string
		wantRefuse bool
	}{
		"principal mismatch": {
			err:        &orchestration.AgentPrincipalMismatchError{TenantID: "t", AgentID: "a"},
			wantStatus: http.StatusConflict,
			wantCode:   "agent_id_conflict",
			wantRefuse: true,
		},
		"disabled agent": {
			err:        &orchestration.AgentDisabledError{TenantID: "t", AgentID: "a"},
			wantStatus: http.StatusForbidden,
			wantCode:   "agent_disabled",
			wantRefuse: true,
		},
		"already exists is no longer terminal": {
			err: &orchestration.AgentAlreadyExistsError{AgentID: "a"},
		},
		"store failure": {
			err: &orchestration.AgentRegistryError{Sentinel: orchestration.ErrAgentRegistryRequired},
		},
		"no error": {},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			status, code, refused := agentRegistrarRefusal(tt.err)
			assert.Equal(t, tt.wantRefuse, refused)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantCode, code)
		})
	}
}

// TestMergeRegisterMetadata_RecordsThePrincipalAndItsKey pins what the refresh
// guard reads. The CommonName is the record's owner; the SPKI thumbprint is
// the canonical mTLS identifier #674 binds onto the enrolled user, recorded so
// an operator can see which key a record is currently bound to.
func TestMergeRegisterMetadata_RecordsThePrincipalAndItsKey(t *testing.T) {
	t.Parallel()

	cert := agentOAuthSignedPeerCert(t, "acme", "agent01")
	meta := mergeRegisterMetadata(&agentRegisterRequest{Hostname: "agent01"}, cert)

	assert.Equal(t, "agent01", meta[orchestration.AgentPrincipalMetadataKey])
	assert.Equal(t, qrdbsdk.ComputeSPKIThumbprint(cert), meta[orchestration.AgentPrincipalKeyMetadataKey])
	assert.NotEmpty(t, meta[orchestration.AgentPrincipalKeyMetadataKey])
}

// TestMergeRegisterMetadata_NoCertificateNamesNoPrincipal fails closed: a
// registration with no certificate names no owner, and a record naming no
// owner is refreshable by nobody.
func TestMergeRegisterMetadata_NoCertificateNamesNoPrincipal(t *testing.T) {
	t.Parallel()

	meta := mergeRegisterMetadata(&agentRegisterRequest{Hostname: "agent01"}, nil)

	assert.NotContains(t, meta, orchestration.AgentPrincipalMetadataKey)
	assert.NotContains(t, meta, orchestration.AgentPrincipalKeyMetadataKey)
}
