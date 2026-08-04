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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// newAgentAdminTestServer builds a minimal REST Server wired with a mock agent admin service.
func newAgentAdminTestServer(adminSvc service.AgentAdminService) *Server {
	mock := mocks.NewMockQuicBaaS()
	mock.AgentAdminServiceFunc = func() service.AgentAdminService { return adminSvc }
	return &Server{service: mock, router: chi.NewRouter()}
}

// agentAdminRouterForTest builds a real chi router with the orchestrator
// subrouter registered exactly as production does via registerOrchestratorRoutes.
// This lets routing tests exercise the full chi dispatch, not just handler logic.
func agentAdminRouterForTest(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		registerOrchestratorRoutes(s, r)
	})
	return r
}

// disableAgentReq builds a handler-unit-test request: chi URL params are
// injected manually so the test exercises the handler in isolation without
// going through a router.
func disableAgentReq(agentID, query string) *http.Request {
	r := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/orchestrator/agents/"+agentID+"/disable"+query,
		nil,
	)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("agentID", agentID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
}

// enableAgentReq mirrors disableAgentReq for the enable endpoint.
func enableAgentReq(agentID, query string) *http.Request {
	r := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/orchestrator/agents/"+agentID+"/enable"+query,
		nil,
	)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("agentID", agentID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
}

// =============================================================================
// Routing regression — canonical path /api/v1/orchestrator/agents/{id}/disable|enable
// =============================================================================

// TestDisableEnableRoute_CanonicalPathReachable is the regression test for the
// routing bug where disable/enable were registered at /api/v1/agents/{id}/...
// (outside the orchestrator subrouter) while the SDK always prepended
// /api/v1/orchestrator, causing 404s. The test drives the full chi router to
// confirm both endpoints are reachable at the canonical orchestrator-prefixed path.
func TestDisableEnableRoute_CanonicalPathReachable(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockAgentAdminService{
		DisableAgentFunc: func(_ context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return &service.DisableAgentResponse{
				AgentID:    req.AgentID,
				Disabled:   true,
				DisabledAt: now,
				Timestamp:  now,
			}, nil
		},
		EnableAgentFunc: func(_ context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
			return &service.EnableAgentResponse{
				AgentID:   req.AgentID,
				Enabled:   true,
				Timestamp: now,
			}, nil
		},
	}

	router := agentAdminRouterForTest(newAgentAdminTestServer(adminSvc))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "disable at orchestrator path",
			method: http.MethodPost,
			path:   "/api/v1/orchestrator/agents/agent-99/disable?tenant_id=system",
		},
		{
			name:   "enable at orchestrator path",
			method: http.MethodPost,
			path:   "/api/v1/orchestrator/agents/agent-99/enable?tenant_id=system",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// 404 means the route was not found — the routing bug is back.
			// 405 would mean wrong method on a known route.
			// Any 2xx confirms the route is registered and the handler ran.
			if w.Code == http.StatusNotFound {
				t.Errorf("route %s %s returned 404 — disable/enable not registered under /orchestrator subrouter", tc.method, tc.path)
			}
			if w.Code == http.StatusMethodNotAllowed {
				t.Errorf("route %s %s returned 405 — method mismatch at the orchestrator path", tc.method, tc.path)
			}
			if w.Code != http.StatusOK {
				t.Errorf("route %s %s returned %d, want 200; body: %s", tc.method, tc.path, w.Code, w.Body.String())
			}
		})
	}
}

// TestDisableEnableRoute_OldNonOrchestratorPath404 documents that the old
// /api/v1/agents/{id}/disable|enable path is NOT registered — confirming the
// routes were moved and not aliased. A request to the old path must 404.
func TestDisableEnableRoute_OldNonOrchestratorPath404(t *testing.T) {
	adminSvc := &mocks.MockAgentAdminService{}
	router := agentAdminRouterForTest(newAgentAdminTestServer(adminSvc))

	paths := []string{
		"/api/v1/agents/agent-99/disable?tenant_id=system",
		"/api/v1/agents/agent-99/enable?tenant_id=system",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("old path %s returned %d, want 404 — old route must not be registered", path, w.Code)
			}
		})
	}
}

// =============================================================================
// HandleDisableAgent — handler unit tests
// =============================================================================

func TestHandleDisableAgent_HappyPath(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockAgentAdminService{
		DisableAgentFunc: func(_ context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return &service.DisableAgentResponse{
				AgentID:    req.AgentID,
				Disabled:   true,
				DisabledAt: now,
				Timestamp:  now,
			}, nil
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleDisableAgent(w, disableAgentReq("agent-1", "?tenant_id=t1"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp DisableAgentRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Disabled {
		t.Error("expected Disabled=true")
	}
	if resp.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", resp.AgentID, "agent-1")
	}
}

func TestHandleDisableAgent_MissingAgentID(t *testing.T) {
	s := newAgentAdminTestServer(&mocks.MockAgentAdminService{})
	w := httptest.NewRecorder()
	s.HandleDisableAgent(w, disableAgentReq("", "?tenant_id=t1"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDisableAgent_MissingTenantID(t *testing.T) {
	s := newAgentAdminTestServer(&mocks.MockAgentAdminService{})
	w := httptest.NewRecorder()
	s.HandleDisableAgent(w, disableAgentReq("agent-1", ""))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDisableAgent_AlreadyDisabled(t *testing.T) {
	adminSvc := &mocks.MockAgentAdminService{
		DisableAgentFunc: func(_ context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return nil, &service.AgentAlreadyDisabledError{TenantID: req.TenantID, AgentID: req.AgentID}
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleDisableAgent(w, disableAgentReq("agent-2", "?tenant_id=t1"))
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleDisableAgent_NotFound(t *testing.T) {
	adminSvc := &mocks.MockAgentAdminService{
		DisableAgentFunc: func(_ context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return nil, &service.AgentAdminNotFoundError{TenantID: req.TenantID, AgentID: req.AgentID}
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleDisableAgent(w, disableAgentReq("missing", "?tenant_id=t1"))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =============================================================================
// HandleEnableAgent — handler unit tests
// =============================================================================

func TestHandleEnableAgent_HappyPath(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockAgentAdminService{
		EnableAgentFunc: func(_ context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
			return &service.EnableAgentResponse{
				AgentID:   req.AgentID,
				Enabled:   true,
				Timestamp: now,
			}, nil
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleEnableAgent(w, enableAgentReq("agent-3", "?tenant_id=t1"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp EnableAgentRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestHandleEnableAgent_NotDisabled(t *testing.T) {
	adminSvc := &mocks.MockAgentAdminService{
		EnableAgentFunc: func(_ context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
			return nil, &service.AgentNotDisabledError{TenantID: req.TenantID, AgentID: req.AgentID}
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleEnableAgent(w, enableAgentReq("agent-4", "?tenant_id=t1"))
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleEnableAgent_MissingParams(t *testing.T) {
	s := newAgentAdminTestServer(&mocks.MockAgentAdminService{})

	w := httptest.NewRecorder()
	s.HandleEnableAgent(w, enableAgentReq("", "?tenant_id=t1"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing id: status = %d, want 400", w.Code)
	}

	w = httptest.NewRecorder()
	s.HandleEnableAgent(w, enableAgentReq("agent-1", ""))
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing tenant: status = %d, want 400", w.Code)
	}
}
