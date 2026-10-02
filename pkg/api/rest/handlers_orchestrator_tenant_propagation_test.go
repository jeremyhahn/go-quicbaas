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

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// stubSpawnBackend is a minimal AgentBackend that records the tenant present in
// the spawn context. It lets the spawn handler drive the REAL agentServiceImpl
// (with a real TenantGuard) all the way past validateTenant into a backend
// Spawn, exactly like the embedded-fleet-node path does in the daemon — without
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

// spawnRequest builds a spawn request bound to callerTenant the way the
// authentication layer binds it (tenantctx), or unbound when callerTenant is "".
func spawnRequest(t *testing.T, query, callerTenant string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{"backend": "claude", "initial_prompt": "hi"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions"+query, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if callerTenant != "" {
		req = req.WithContext(tenantctx.WithTenant(req.Context(), callerTenant))
	}
	return req
}

// TestOrchestratorSpawn_CallerTenantPropagatesToRealGuard asserts the tenant
// the authentication layer bound crosses the real TenantGuard into the backend
// spawn context.
func TestOrchestratorSpawn_CallerTenantPropagatesToRealGuard(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, spawnRequest(t, "", "tenant-from-token"))

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	assert.Equal(t, "tenant-from-token", backend.spawnedTenant)

	var resp orchestration.SpawnSessionResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "stub-session-1", resp.SessionID)
}

// TestOrchestratorSpawn_SystemCallerNamingATenantIsRefused asserts the system
// tenant gets no cross-tenant lift through ?tenant_id= (go-quicbaas#1150).
func TestOrchestratorSpawn_SystemCallerNamingATenantIsRefused(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, spawnRequest(t, "?tenant_id=acme", qrdbsdk.SystemTenantID))

	require.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	assert.Empty(t, backend.spawnedTenant, "nothing may be spawned")
}

// TestOrchestratorSpawn_TenantCallerNamingAnotherTenantIsRefused is the
// go-quicbaas#1150 regression: ?tenant_id= used to override the caller's own
// tenant unchecked. A tenant caller naming another tenant is now refused 403
// and nothing is spawned.
func TestOrchestratorSpawn_TenantCallerNamingAnotherTenantIsRefused(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	server.HandleOrchestratorSpawn(rec, spawnRequest(t, "?tenant_id=override", "tenant-from-token"))

	assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	assert.Empty(t, backend.spawnedTenant, "a refused request must not spawn")
}

// TestOrchestratorSpawn_UnboundCallerIsRefused asserts a request with no tenant
// bound is refused rather than falling back to the system tenant.
func TestOrchestratorSpawn_UnboundCallerIsRefused(t *testing.T) {
	server, backend, cleanup := newRealAgentSpawnServer(t)
	defer cleanup()

	for _, query := range []string{"", "?tenant_id=acme"} {
		rec := httptest.NewRecorder()
		server.HandleOrchestratorSpawn(rec, spawnRequest(t, query, ""))
		assert.Equal(t, http.StatusForbidden, rec.Code, "query %q body=%s", query, rec.Body.String())
	}
	assert.Empty(t, backend.spawnedTenant)
}

// TestResolveOrchestratorTenant_Rule unit-tests the centralized tenant
// derivation helper against the tenantctx.Target rule.
func TestResolveOrchestratorTenant_Rule(t *testing.T) {
	resolve := func(query, callerTenant string) (int, context.Context, string, bool) {
		req := httptest.NewRequest(http.MethodGet, "/x"+query, nil)
		if callerTenant != "" {
			req = req.WithContext(tenantctx.WithTenant(req.Context(), callerTenant))
		}
		rec := httptest.NewRecorder()
		ctx, tenant, ok := resolveOrchestratorTenant(rec, req)
		return rec.Code, ctx, tenant, ok
	}

	t.Run("caller's own tenant when none is named", func(t *testing.T) {
		_, ctx, tenant, ok := resolve("", "tok")
		require.True(t, ok)
		assert.Equal(t, "tok", tenant)
		assert.Equal(t, "tok", orchestration.TenantFromContext(ctx))
	})

	t.Run("caller may name its own tenant", func(t *testing.T) {
		_, _, tenant, ok := resolve("?tenant_id=tok", "tok")
		require.True(t, ok)
		assert.Equal(t, "tok", tenant)
	})

	t.Run("tenant caller naming another tenant is refused", func(t *testing.T) {
		code, _, _, ok := resolve("?tenant_id=q", "tok")
		assert.False(t, ok)
		assert.Equal(t, http.StatusForbidden, code)
	})

	t.Run("tenant caller naming the system tenant is refused", func(t *testing.T) {
		code, _, _, ok := resolve("?tenant_id="+qrdbsdk.SystemTenantID, "tok")
		assert.False(t, ok)
		assert.Equal(t, http.StatusForbidden, code)
	})

	t.Run("system caller naming another tenant is refused", func(t *testing.T) {
		code, _, _, ok := resolve("?tenant_id=q", qrdbsdk.SystemTenantID)
		assert.False(t, ok)
		assert.Equal(t, http.StatusForbidden, code)
	})

	t.Run("system caller acts for itself", func(t *testing.T) {
		_, ctx, tenant, ok := resolve("", qrdbsdk.SystemTenantID)
		require.True(t, ok)
		assert.Equal(t, qrdbsdk.SystemTenantID, tenant)
		assert.Equal(t, qrdbsdk.SystemTenantID, orchestration.TenantFromContext(ctx))
	})

	t.Run("unbound caller is refused, not defaulted to system", func(t *testing.T) {
		code, _, _, ok := resolve("", "")
		assert.False(t, ok)
		assert.Equal(t, http.StatusForbidden, code)
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
			req = req.WithContext(tenantctx.WithTenant(req.Context(), "acme"))

			rec := httptest.NewRecorder()
			tc.invoke(server, rec, req)

			assert.NotContains(t, rec.Body.String(), "tenant ID required",
				"handler %s must derive a tenant; body=%s", tc.name, rec.Body.String())
		})
	}
}

// TestOrchestratorJobHandlers_DeriveTheTenantLikeEveryOtherHandler closes the
// last two handlers that read the tenant straight off the query string.
//
// `GET /orchestrator/jobs` and `GET /orchestrator/jobs/{id}` were the only
// orchestrator routes that did not go through resolveOrchestratorTenant: they
// took `?tenant_id=` verbatim, so a caller that omitted it was refused with
// "tenant ID required" rather than being served its OWN tenant. Raised in
// review on go-quicbaas#822. Since go-quicbaas#1150 an unbound caller is
// refused 403 instead of falling back to the system tenant.
func TestOrchestratorJobHandlers_DeriveTheTenantLikeEveryOtherHandler(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		invoke func(s *Server, w http.ResponseWriter, r *http.Request)
	}{
		{
			name: "list jobs",
			path: "/api/v1/orchestrator/jobs",
			invoke: func(s *Server, w http.ResponseWriter, r *http.Request) {
				s.HandleOrchestratorListJobs(w, r)
			},
		},
		{
			name: "get job status",
			path: "/api/v1/orchestrator/jobs/job-1",
			invoke: func(s *Server, w http.ResponseWriter, r *http.Request) {
				s.HandleOrchestratorGetJobStatus(w, r)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+" — token claim, no query param", func(t *testing.T) {
			server, _, cleanup := newRealAgentSpawnServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(tenantctx.WithTenant(req.Context(), "tenant-from-token"))
			req = withJobIDParam(req, "job-1")

			rec := httptest.NewRecorder()
			tc.invoke(server, rec, req)

			assert.NotContains(t, rec.Body.String(), "tenant ID required",
				"handler %s must derive the tenant from the token; body=%s", tc.name, rec.Body.String())
		})

		t.Run(tc.name+" — no tenant bound is refused", func(t *testing.T) {
			server, _, cleanup := newRealAgentSpawnServer(t)
			defer cleanup()

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = withJobIDParam(req, "job-1")

			rec := httptest.NewRecorder()
			tc.invoke(server, rec, req)

			assert.Equal(t, http.StatusForbidden, rec.Code,
				"handler %s must refuse an unbound caller; body=%s", tc.name, rec.Body.String())
		})
	}
}

// withJobIDParam attaches the {jobID} route parameter the job handlers read,
// which httptest.NewRequest alone does not populate.
func withJobIDParam(r *http.Request, jobID string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("jobID", jobID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
}
