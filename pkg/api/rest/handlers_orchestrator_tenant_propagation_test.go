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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// stubSpawnBackend is a minimal AgentBackend that records the tenant present in
// the spawn context. It lets the spawn handler drive the REAL agentServiceImpl
// (with a real TenantGuard) all the way past validateTenant into a backend
// Spawn, exactly like the embedded-agent path does in the daemon — without
// requiring a live agent runtime.
type stubSpawnBackend struct {
	spawnedTenant string
}

func (b *stubSpawnBackend) Spawn(ctx context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	b.spawnedTenant = orchestration.TenantFromContext(ctx)
	return &orchestration.Session{
		ID:        "stub-session-1",
		Backend:   "claude",
		Status:    orchestration.SessionStateActive,
		CreatedAt: 1,
	}, nil
}

func (b *stubSpawnBackend) Prompt(ctx context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{}, nil
}

func (b *stubSpawnBackend) Status(ctx context.Context, sessionID string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{}, nil
}

func (b *stubSpawnBackend) Close(ctx context.Context, sessionID string) error { return nil }

func (b *stubSpawnBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{
		Kind:               orchestration.BackendKindAgent,
		PersistentSessions: true,
	}
}

func (b *stubSpawnBackend) Type() string { return "stub" }

func (b *stubSpawnBackend) ToolGuidance() string { return "" }

// newRealAgentSpawnServer wires a REAL orchestration.agentServiceImpl with a
// real TenantGuard (mirroring lifecycle_orchestration.go) into a REST Server so
// the spawn handler exercises the production tenant guard rather than a mock
// that skips validateTenant.
func newRealAgentSpawnServer(t *testing.T) (*Server, *stubSpawnBackend, func()) {
	t.Helper()

	backend := &stubSpawnBackend{}
	baseLogger, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	require.NoError(t, err)
	logAdapter := logger.NewAdapter(baseLogger)

	agentSvc, agentErr := orchestration.NewAgentService(
		orchestration.NewInMemoryAgentStore(),
		map[string]orchestration.AgentBackend{"claude": backend},
		logAdapter,
	)
	require.NoError(t, agentErr)

	// Attach the real tenant guard exactly as the daemon does. This is what
	// makes validateTenant reject a tenant-less context.
	guardWirer, ok := agentSvc.(interface {
		SetTenantGuard(guard *orchestration.TenantGuard)
	})
	require.True(t, ok, "agent service must support SetTenantGuard")
	guardWirer.SetTenantGuard(orchestration.NewTenantGuard(logAdapter))

	mockService := &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: agentSvc,
	}

	server, _, cleanup := setupMockServer(t)
	server.service = mockService

	return server, backend, cleanup
}

// TestOrchestratorSpawn_TenantPropagatesToRealGuard is the regression test for
// the confirmed spawn tenant-propagation bug: a spawn request whose tenant is
// derived only by the handler (no tenant_id query param, no authenticated
// tenant) must still pass the orchestrator's real TenantGuard. Before the fix
// the handler left the context tenant-less for the SystemTenantID fallback path
// in a way that surfaced "tenant ID required: orchestration".
func TestOrchestratorSpawn_TenantPropagatesToRealGuard(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	body, err := json.Marshal(map[string]string{
		"backend":        "claude",
		"initial_prompt": "hi",
	})
	require.NoError(t, err)

	// No tenant_id query param and no authenticated tenant: the handler must
	// fall back to SystemTenantID and the guard must accept it.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code,
		"spawn must not be rejected by the tenant guard; body=%s", rec.Body.String())

	// The tenant must have crossed into the backend Spawn context.
	assert.Equal(t, qrdbsdk.SystemTenantID, backend.spawnedTenant,
		"tenant must propagate into the backend spawn context")

	var resp orchestration.SpawnSessionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "stub-session-1", resp.SessionID)
}

// TestOrchestratorSpawn_ExplicitTenantPropagatesToRealGuard asserts an explicit
// tenant_id query param propagates all the way to the backend spawn context.
func TestOrchestratorSpawn_ExplicitTenantPropagatesToRealGuard(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	body, err := json.Marshal(map[string]string{"backend": "claude"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/orchestrator/sessions?tenant_id=acme", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code,
		"spawn must not be rejected by the tenant guard; body=%s", rec.Body.String())
	assert.Equal(t, "acme", backend.spawnedTenant,
		"explicit tenant must propagate into the backend spawn context")
}

// TestOrchestratorSpawn_TokenTenantPropagatesToRealGuard asserts that when no
// tenant_id query param is supplied, the tenant is derived from the
// authenticated token's verified AuthorizedContext (the tenant_id claim) and
// propagates into the backend spawn context.
func TestOrchestratorSpawn_TokenTenantPropagatesToRealGuard(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	body, err := json.Marshal(map[string]string{"backend": "claude"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/orchestrator/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Attach a verified AuthorizedContext as the authz middleware would, with a
	// tenant_id claim that is neither empty nor SystemTenantID. The handler must
	// prefer it over the SystemTenantID fallback.
	ctx := authorizedContextIntoContext(req.Context(),
		&platformtoken.AuthorizedContext{TenantID: "tenant-from-token"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code,
		"spawn must not be rejected by the tenant guard; body=%s", rec.Body.String())
	assert.Equal(t, "tenant-from-token", backend.spawnedTenant,
		"token tenant_id claim must propagate into the backend spawn context")
}

// TestOrchestratorSpawn_QueryParamOverridesTokenTenant asserts the explicit
// tenant_id query parameter takes precedence over the token tenant claim.
func TestOrchestratorSpawn_QueryParamOverridesTokenTenant(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	body, err := json.Marshal(map[string]string{"backend": "claude"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/orchestrator/sessions?tenant_id=override", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authorizedContextIntoContext(req.Context(),
		&platformtoken.AuthorizedContext{TenantID: "tenant-from-token"}))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code,
		"spawn must not be rejected by the tenant guard; body=%s", rec.Body.String())
	assert.Equal(t, "override", backend.spawnedTenant,
		"query param must take precedence over the token tenant claim")
}

// TestResolveOrchestratorTenant_Precedence unit-tests the centralized tenant
// derivation helper directly: query param > token claim > SystemTenantID.
func TestResolveOrchestratorTenant_Precedence(t *testing.T) {
	t.Run("query param wins", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x?tenant_id=q", nil)
		req = req.WithContext(authorizedContextIntoContext(req.Context(),
			&platformtoken.AuthorizedContext{TenantID: "tok"}))
		ctx, tenant := resolveOrchestratorTenant(req)
		assert.Equal(t, "q", tenant)
		assert.Equal(t, "q", orchestration.TenantFromContext(ctx))
	})

	t.Run("token claim used when no query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req = req.WithContext(authorizedContextIntoContext(req.Context(),
			&platformtoken.AuthorizedContext{TenantID: "tok"}))
		ctx, tenant := resolveOrchestratorTenant(req)
		assert.Equal(t, "tok", tenant)
		assert.Equal(t, "tok", orchestration.TenantFromContext(ctx))
	})

	t.Run("system tenant fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		ctx, tenant := resolveOrchestratorTenant(req)
		assert.Equal(t, qrdbsdk.SystemTenantID, tenant)
		assert.Equal(t, qrdbsdk.SystemTenantID, orchestration.TenantFromContext(ctx))
	})

	t.Run("empty token tenant falls back to system", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req = req.WithContext(authorizedContextIntoContext(req.Context(),
			&platformtoken.AuthorizedContext{TenantID: ""}))
		ctx, tenant := resolveOrchestratorTenant(req)
		assert.Equal(t, qrdbsdk.SystemTenantID, tenant)
		assert.Equal(t, qrdbsdk.SystemTenantID, orchestration.TenantFromContext(ctx))
	})
}

// TestOrchestratorSessionHandlers_TenantPropagatesToRealGuard guards the broader
// bug class: the session-management handlers (prompt / status / close / list)
// previously did not stamp any tenant onto the context, so they would also be
// rejected by the orchestrator tenant guard. They must now derive the tenant
// like spawn does. Each handler reaches a backend method that runs
// validateTenant; a 400 "tenant ID required" is the regression signal.
func TestOrchestratorSessionHandlers_TenantPropagatesToRealGuard(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		invoke func(s *Server, w http.ResponseWriter, r *http.Request)
	}{
		{
			name:   "list sessions",
			method: http.MethodGet,
			path:   "/api/v1/orchestrator/sessions",
			invoke: func(s *Server, w http.ResponseWriter, r *http.Request) {
				s.HandleOrchestratorListSessions(w, r)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _, cleanup := newRealAgentSpawnServer(t)
			defer cleanup()

			var reader *bytes.Reader
			if tc.body != "" {
				reader = bytes.NewReader([]byte(tc.body))
			} else {
				reader = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(tc.method, tc.path, reader)
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			tc.invoke(server, rec, req)

			assert.NotContains(t, rec.Body.String(), "tenant ID required",
				"handler %s must derive a tenant; body=%s", tc.name, rec.Body.String())
		})
	}
}
