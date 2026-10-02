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
)

// asTenantCaller returns r carrying the context go-qrdb's AuthMiddleware gives
// a request it authenticated for tenantID on the unified server: the identity
// and its tenant under go-qrdb's central key.
func asTenantCaller(r *http.Request, tenantID string) *http.Request {
	ctx := qrdbsdk.WithIdentity(r.Context(), &qrdbsdk.Identity{Subject: "alice", TenantID: tenantID})
	return r.WithContext(qrdbsdk.ContextWithTenant(ctx, tenantID))
}

// TestHooks_XTenantIDNamingAnotherTenantIsRefused is a go-quicbaas#1150
// regression: the hook handlers took X-Tenant-ID as the tenant to write into,
// unchecked, so a tenant-a caller filed tasks in tenant-b.
func TestHooks_XTenantIDNamingAnotherTenantIsRefused(t *testing.T) {
	server, taskStore, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	body, err := json.Marshal(HookPayload{
		HookEventName: "TaskCreated", SessionID: "s1", TaskID: "t-1",
		TaskSubject: "subject", TeammateName: "bob", TeamName: "team",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/hooks/task-created", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant-b")
	rec := httptest.NewRecorder()

	server.HandleHookTaskCreated(rec, asTenantCaller(req, "tenant-a"))

	assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	_, err = taskStore.GetTask(context.Background(), "tenant-b", "t-1")
	assert.Error(t, err, "no task may be filed in tenant-b")
}

// TestHooks_TeamNameIsNotATenant is the other half: with no header the hook
// filed the task under the payload's team name, so naming a team "tenant-b"
// wrote into tenant-b. The task belongs to the caller's tenant.
func TestHooks_TeamNameIsNotATenant(t *testing.T) {
	server, taskStore, _, cleanup := setupHookTestServer(t)
	defer cleanup()

	body, err := json.Marshal(HookPayload{
		HookEventName: "TaskCreated", SessionID: "s1", TaskID: "t-2",
		TaskSubject: "subject", TeammateName: "bob", TeamName: "tenant-b",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/hooks/task-created", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	server.HandleHookTaskCreated(rec, asTenantCaller(req, "tenant-a"))

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	_, err = taskStore.GetTask(context.Background(), "tenant-b", "t-2")
	assert.Error(t, err, "the team name must not select tenant-b")
	task, err := taskStore.GetTask(context.Background(), "tenant-a", "t-2")
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", task.TenantID)
}

// newTenantTargetOrchestratorServer serves the orchestrator REST surface over
// the real in-memory agent service.
func newTenantTargetOrchestratorServer(t *testing.T) (*Server, func()) {
	t.Helper()
	baseLogger, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	require.NoError(t, err)
	agentSvc, err := orchestration.NewAgentService(
		orchestration.NewInMemoryAgentStore(), map[string]orchestration.AgentBackend{}, logger.NewAdapter(baseLogger))
	require.NoError(t, err)

	server, _, cleanup := setupMockServer(t)
	server.service = &MockQuicBaaSWithAgent{MockQuicBaaS: testutil.NewMockQuicBaaS(), agentService: agentSvc}
	return server, cleanup
}

// TestOrchestrator_TenantIDNamingAnotherTenantIsRefused is a go-quicbaas#1150
// regression: ?tenant_id= named the tenant an orchestrator request acted for
// with no check against the caller, so a tenant-a caller listed tenant-b's
// sessions.
func TestOrchestrator_TenantIDNamingAnotherTenantIsRefused(t *testing.T) {
	server, cleanup := newTenantTargetOrchestratorServer(t)
	defer cleanup()

	for _, target := range []string{"tenant-b", qrdbsdk.SystemTenantID} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id="+target, nil)
		rec := httptest.NewRecorder()
		server.HandleOrchestratorListSessions(rec, asTenantCaller(req, "tenant-a"))
		assert.Equal(t, http.StatusForbidden, rec.Code, "tenant-a naming %s: %s", target, rec.Body.String())
	}
}

// TestOrchestrator_SystemCallerNamesOnlyItself proves the system tenant gets
// no cross-tenant lift through ?tenant_id=, and still acts for itself.
func TestOrchestrator_SystemCallerNamesOnlyItself(t *testing.T) {
	server, cleanup := newTenantTargetOrchestratorServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id=tenant-b", nil)
	rec := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(rec, asTenantCaller(req, qrdbsdk.SystemTenantID))
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/sessions", nil)
	rec = httptest.NewRecorder()
	server.HandleOrchestratorListSessions(rec, asTenantCaller(req, qrdbsdk.SystemTenantID))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
