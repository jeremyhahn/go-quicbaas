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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account
// =============================================================================

// rotatingMockAgentService is a mockAgentService that also implements the
// optional rotation verb. The bare mockAgentService deliberately does not,
// which is what makes the "deployment without rotation" branch reachable.
type rotatingMockAgentService struct {
	*mockAgentService

	rotateFunc func(ctx context.Context, req *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error)

	lastRequest *orchestration.RotateSessionAccountRequest
	lastTenant  string
}

func (m *rotatingMockAgentService) RotateSessionAccount(
	ctx context.Context,
	req *orchestration.RotateSessionAccountRequest,
) (*orchestration.RotateSessionAccountResult, error) {
	m.lastRequest = req
	m.lastTenant = orchestration.TenantFromContext(ctx)
	if m.rotateFunc != nil {
		return m.rotateFunc(ctx, req)
	}
	return &orchestration.RotateSessionAccountResult{
		SessionID:         req.SessionID,
		PreviousAccountID: "acct-a",
		AccountID:         "acct-b",
		Strategy:          "least_usage",
		Mode:              orchestration.AccountRotationModeInPlace,
	}, nil
}

var _ orchestration.SessionAccountRotationService = (*rotatingMockAgentService)(nil)

// setupRotatingOrchestratorServer wires a server whose agent service can
// rotate. It mirrors setupOrchestratorMockServer, which cannot be reused
// directly because its agent service is the non-rotating mock.
func setupRotatingOrchestratorServer(t *testing.T) (*Server, *rotatingMockAgentService, func()) {
	t.Helper()

	agentSvc := &rotatingMockAgentService{mockAgentService: &mockAgentService{}}
	server, _, cleanup := setupMockServer(t)
	server.service = &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: agentSvc,
	}
	return server, agentSvc, cleanup
}

// rotateRequest issues a rotate-account request against sessionID with the
// given raw body, through the chi route context the real router installs.
func rotateRequest(t *testing.T, server *Server, sessionID, rawBody string) *httptest.ResponseRecorder {
	t.Helper()

	url := "/api/v1/orchestrator/sessions/" + sessionID + "/rotate-account"
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(rawBody)))
	req.Header.Set("Content-Type", "application/json")

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("sessionID", sessionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	rec := httptest.NewRecorder()
	server.HandleOrchestratorRotateSessionAccount(rec, req)
	return rec
}

func TestOrchestratorRotateAccount_MovesTheSession(t *testing.T) {
	server, agentSvc, cleanup := setupRotatingOrchestratorServer(t)
	defer cleanup()

	availableAt := time.Now().UTC().Add(45 * time.Minute).Truncate(time.Second)
	body := `{"reason":"seat retired","hold_outgoing":true,"available_at":"` +
		availableAt.Format(time.RFC3339) + `"}`

	rec := rotateRequest(t, server, "session-abc", body)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp orchestration.RotateSessionAccountResult
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "session-abc", resp.SessionID)
	assert.Equal(t, "acct-a", resp.PreviousAccountID)
	assert.Equal(t, "acct-b", resp.AccountID)
	assert.Equal(t, orchestration.AccountRotationModeInPlace, resp.Mode)

	require.NotNil(t, agentSvc.lastRequest)
	assert.Equal(t, "seat retired", agentSvc.lastRequest.Reason)
	assert.True(t, agentSvc.lastRequest.HoldOutgoing)
	assert.True(t, availableAt.Equal(agentSvc.lastRequest.AvailableAt))
	assert.NotEmpty(t, agentSvc.lastTenant, "the tenant guard reads the tenant off the context")
}

// TestOrchestratorRotateAccount_EmptyBodyIsAPlainRotation keeps "move my
// session now" a one-line curl rather than a mandatory JSON document.
func TestOrchestratorRotateAccount_EmptyBodyIsAPlainRotation(t *testing.T) {
	server, agentSvc, cleanup := setupRotatingOrchestratorServer(t)
	defer cleanup()

	rec := rotateRequest(t, server, "session-abc", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	require.NotNil(t, agentSvc.lastRequest)
	assert.Equal(t, "session-abc", agentSvc.lastRequest.SessionID)
	assert.False(t, agentSvc.lastRequest.HoldOutgoing,
		"an empty POST must not invent a hold on the outgoing account")
}

// TestOrchestratorRotateAccount_PathWinsOverBodySessionID keeps the PEP's
// path-scoped decision honest: the segment the caller was authorized against
// is the session that moves.
func TestOrchestratorRotateAccount_PathWinsOverBodySessionID(t *testing.T) {
	server, agentSvc, cleanup := setupRotatingOrchestratorServer(t)
	defer cleanup()

	rec := rotateRequest(t, server, "session-abc", `{"session_id":"someone-elses-session"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, agentSvc.lastRequest)
	assert.Equal(t, "session-abc", agentSvc.lastRequest.SessionID)
}

func TestOrchestratorRotateAccount_InvalidJSONIsBadRequest(t *testing.T) {
	server, agentSvc, cleanup := setupRotatingOrchestratorServer(t)
	defer cleanup()

	rec := rotateRequest(t, server, "session-abc", `{"reason":`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Nil(t, agentSvc.lastRequest, "a malformed body must not reach the service")
}

func TestOrchestratorRotateAccount_PinnedSessionIsAConflict(t *testing.T) {
	server, agentSvc, cleanup := setupRotatingOrchestratorServer(t)
	defer cleanup()

	agentSvc.rotateFunc = func(context.Context, *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error) {
		return nil, &orchestration.SessionNotRotatableError{
			SessionID: "session-abc",
			Reason:    "the session is pinned to a named account",
		}
	}

	rec := rotateRequest(t, server, "session-abc", "")
	assert.Equal(t, http.StatusConflict, rec.Code,
		"a deliberate pin is a state conflict, not a malformed request: %s", rec.Body.String())
}

func TestOrchestratorRotateAccount_UnknownSessionIsNotFound(t *testing.T) {
	server, agentSvc, cleanup := setupRotatingOrchestratorServer(t)
	defer cleanup()

	agentSvc.rotateFunc = func(context.Context, *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error) {
		return nil, &orchestration.SessionNotFoundError{SessionID: "missing"}
	}

	rec := rotateRequest(t, server, "missing", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestOrchestratorRotateAccount_UnsupportedDeployment covers the wrong-type
// branch: an agent service without the rotation verb answers 501 instead of
// panicking on a type assertion.
func TestOrchestratorRotateAccount_UnsupportedDeployment(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	rec := rotateRequest(t, server, "session-abc", "")
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

// =============================================================================
// Authorization
// =============================================================================

// TestOrchestratorRotateAccount_AuthorizesAsSpawn pins the deliberate choice of
// verb. Rotation closes the agent process and reconstitutes it against a newly
// selected pooled credential, which is a spawn's authority — the weaker close
// and status verbs must never authorize consuming another account's
// credential. The value must match the gRPC, QUIC and MCP tables exactly.
func TestOrchestratorRotateAccount_AuthorizesAsSpawn(t *testing.T) {
	resource, action, ok := mapRequestToPermission(
		"/api/v1/orchestrator/sessions/session-abc/rotate-account", http.MethodPost)
	require.True(t, ok, "rotate-account is unmapped; the PEP would default-deny it")
	assert.Equal(t, "orchestrator", resource)
	assert.Equal(t, "spawn", action)
}

// TestOrchestratorRotateAccount_DoesNotWeakenTheSiblingRoutes proves the new
// leaf rule did not shadow the surrounding session routes.
func TestOrchestratorRotateAccount_DoesNotWeakenTheSiblingRoutes(t *testing.T) {
	tests := []struct {
		path   string
		method string
		action string
	}{
		{"/api/v1/orchestrator/sessions", http.MethodPost, "spawn"},
		{"/api/v1/orchestrator/sessions/session-abc", http.MethodGet, "status"},
		{"/api/v1/orchestrator/sessions/session-abc", http.MethodDelete, "close"},
		{"/api/v1/orchestrator/sessions/session-abc/prompt", http.MethodPost, "prompt"},
	}
	for _, tc := range tests {
		resource, action, ok := mapRequestToPermission(tc.path, tc.method)
		require.True(t, ok, "%s %s", tc.method, tc.path)
		assert.Equal(t, "orchestrator", resource, "%s %s", tc.method, tc.path)
		assert.Equal(t, tc.action, action, "%s %s", tc.method, tc.path)
	}
}

// =============================================================================
// account_allow on the spawn shape
// =============================================================================

// TestOrchestratorSpawn_CarriesAccountAllow is the REST half of gap 2: without
// account_allow reaching the service, a caller could enumerate a rotation pool
// and silently get a session that can never rotate.
func TestOrchestratorSpawn_CarriesAccountAllow(t *testing.T) {
	captured := spawnWithBody(t, `{"backend":"claude","account_id":"acct-a","account_allow":["acct-a","acct-b"]}`)
	assert.Equal(t, []string{"acct-a", "acct-b"}, captured.AccountAllow, "REST spawn dropped account_allow")
	assert.Equal(t, "acct-a", captured.AccountID)
}
