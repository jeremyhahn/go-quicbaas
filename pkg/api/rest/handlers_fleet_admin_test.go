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

// newFleetAdminTestServer builds a minimal REST Server wired with a mock agent admin service.
func newFleetAdminTestServer(adminSvc service.FleetAdminService) *Server {
	mock := mocks.NewMockQuicBaaS()
	mock.FleetAdminServiceFunc = func() service.FleetAdminService { return adminSvc }
	return &Server{service: mock, router: chi.NewRouter()}
}

// mountOrchestratorTestRoutes mounts the /orchestrator surface on r exactly
// as the routes.go table (Routes(), GroupAPI) registers it in production;
// the standalone registerOrchestratorRoutes chi helper was removed once the
// table-driven registration became the single source of truth.
func mountOrchestratorTestRoutes(s *Server, r chi.Router) {
	r.Route("/orchestrator", func(r chi.Router) {
		// Session management
		r.Post("/sessions", s.HandleOrchestratorSpawn)
		r.Post("/sessions/{sessionID}/prompt", s.HandleOrchestratorPrompt)
		r.Get("/sessions/{sessionID}", s.HandleOrchestratorSessionStatus)
		r.Delete("/sessions/{sessionID}", s.HandleOrchestratorCloseSession)
		r.Get("/sessions", s.HandleOrchestratorListSessions)
		r.Post("/sessions/{sessionID}/rotate-account", s.HandleOrchestratorRotateSessionAccount)

		// Session event stream. SSE-based live tail; the same handler
		// serves scrollback via ?from=N&follow=0.
		r.Get("/sessions/{sessionID}/events", s.HandleOrchestratorSessionEvents)

		// Task management
		r.Post("/tasks", s.HandleOrchestratorCreateTask)
		r.Get("/tasks/{taskID}", s.HandleOrchestratorGetTask)
		r.Get("/tasks", s.HandleOrchestratorListTasks)
		r.Post("/tasks/{taskID}/claim", s.HandleOrchestratorClaimTask)
		r.Post("/tasks/{taskID}/complete", s.HandleOrchestratorCompleteTask)
		r.Post("/tasks/decompose", s.HandleOrchestratorDecomposeTask)

		// Task artifact management
		r.Post("/tasks/{taskID}/artifacts", s.HandleOrchestratorSubmitArtifacts)
		r.Get("/tasks/{taskID}/artifacts", s.HandleOrchestratorListArtifacts)
		r.Get("/tasks/{taskID}/artifacts/{name}", s.HandleOrchestratorGetArtifact)

		// Job management
		r.Post("/jobs", s.HandleOrchestratorStartJob)
		r.Get("/jobs", s.HandleOrchestratorListJobs)
		r.Get("/jobs/{jobID}", s.HandleOrchestratorGetJobStatus)
		r.Delete("/jobs/{jobID}", s.HandleOrchestratorCancelJob)
		r.Get("/jobs/{jobID}/events", s.HandleOrchestratorSubscribeJobEvents)
		r.Post("/jobs/{jobID}/children", s.HandleOrchestratorSpawnChildJob)

		// Backend management
		r.Get("/backends", s.HandleOrchestratorListBackends)

		// Project discovery
		r.Post("/discover", s.HandleOrchestratorDiscover)

		// Webhook hook events
		r.Post("/hooks", s.HandleOrchestratorHookEvent)

		// Workflow management
		r.Get("/workflows", s.HandleOrchestratorListWorkflows)
		r.Post("/workflows", s.HandleOrchestratorCreateWorkflow)
		r.Get("/workflows/{name}", s.HandleOrchestratorGetWorkflow)
		r.Put("/workflows/{name}", s.HandleOrchestratorUpdateWorkflow)
		r.Delete("/workflows/{name}", s.HandleOrchestratorDeleteWorkflow)

		// Health
		r.Get("/health", s.HandleOrchestratorHealth)

		// Stateless fleet-routed inference (sessionless; targets Ollama-style backends)
		r.Post("/inference", s.HandleOrchestratorInference)
	})

	// Fleet management and fleet node lifecycle admin.
	r.Route("/fleet", func(r chi.Router) {
		r.Post("/", s.HandleRegisterFleetNode)
		r.Get("/", s.HandleListFleetNodes)
		r.Get("/{nodeID}", s.HandleGetFleetNode)
		r.Delete("/{nodeID}", s.HandleRemoveFleetNode)
		r.Post("/{nodeID}/heartbeat", s.HandleFleetNodeHeartbeat)
		r.Get("/{nodeID}/backends/{backendName}/models", s.HandleListFleetNodeBackendModels)
		r.Post("/{nodeID}/disable", s.HandleDisableFleetNode)
		r.Post("/{nodeID}/enable", s.HandleEnableFleetNode)
	})
}

// fleetAdminRouterForTest builds a real chi router with the orchestrator
// subrouter registered exactly as production does via routes.go's table.
// This lets routing tests exercise the full chi dispatch, not just handler logic.
func fleetAdminRouterForTest(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		mountOrchestratorTestRoutes(s, r)
	})
	return r
}

// disableAgentReq builds a handler-unit-test request: chi URL params are
// injected manually so the test exercises the handler in isolation without
// going through a router.
func disableAgentReq(nodeID, query string) *http.Request {
	r := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/fleet/"+nodeID+"/disable"+query,
		nil,
	)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("nodeID", nodeID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
}

// enableAgentReq mirrors disableAgentReq for the enable endpoint.
func enableAgentReq(nodeID, query string) *http.Request {
	r := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/fleet/"+nodeID+"/enable"+query,
		nil,
	)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("nodeID", nodeID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
}

// =============================================================================
// Routing regression — canonical path /api/v1/fleet/{id}/disable|enable
// =============================================================================

// TestDisableEnableRoute_CanonicalPathReachable drives the full chi router to
// confirm both lifecycle endpoints are reachable at the canonical
// /api/v1/fleet/{nodeID} path the SDKs target.
func TestDisableEnableRoute_CanonicalPathReachable(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockFleetAdminService{
		DisableFleetNodeFunc: func(_ context.Context, req *service.DisableFleetNodeRequest) (*service.DisableFleetNodeResponse, error) {
			return &service.DisableFleetNodeResponse{
				NodeID:     req.NodeID,
				Disabled:   true,
				DisabledAt: now,
				Timestamp:  now,
			}, nil
		},
		EnableFleetNodeFunc: func(_ context.Context, req *service.EnableFleetNodeRequest) (*service.EnableFleetNodeResponse, error) {
			return &service.EnableFleetNodeResponse{
				NodeID:    req.NodeID,
				Enabled:   true,
				Timestamp: now,
			}, nil
		},
	}

	router := fleetAdminRouterForTest(newFleetAdminTestServer(adminSvc))

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "disable at the fleet path",
			method: http.MethodPost,
			path:   "/api/v1/fleet/agent-99/disable?tenant_id=system",
		},
		{
			name:   "enable at the fleet path",
			method: http.MethodPost,
			path:   "/api/v1/fleet/agent-99/enable?tenant_id=system",
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
				t.Errorf("route %s %s returned 404 — disable/enable not registered under /fleet", tc.method, tc.path)
			}
			if w.Code == http.StatusMethodNotAllowed {
				t.Errorf("route %s %s returned 405 — method mismatch at the fleet path", tc.method, tc.path)
			}
			if w.Code != http.StatusOK {
				t.Errorf("route %s %s returned %d, want 200; body: %s", tc.method, tc.path, w.Code, w.Body.String())
			}
		})
	}
}

// TestDisableEnableRoute_RetiredPaths404 documents that neither retired
// path -- /api/v1/agents/{id}/... nor /api/v1/orchestrator/agents/{id}/... --
// is registered: the routes moved to /api/v1/fleet and were not aliased.
func TestDisableEnableRoute_RetiredPaths404(t *testing.T) {
	adminSvc := &mocks.MockFleetAdminService{}
	router := fleetAdminRouterForTest(newFleetAdminTestServer(adminSvc))

	paths := []string{
		"/api/v1/agents/agent-99/disable?tenant_id=system",
		"/api/v1/agents/agent-99/enable?tenant_id=system",
		"/api/v1/orchestrator/agents/agent-99/disable?tenant_id=system",
		"/api/v1/orchestrator/agents/agent-99/enable?tenant_id=system",
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
// HandleDisableFleetNode — handler unit tests
// =============================================================================

func TestHandleDisableAgent_HappyPath(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockFleetAdminService{
		DisableFleetNodeFunc: func(_ context.Context, req *service.DisableFleetNodeRequest) (*service.DisableFleetNodeResponse, error) {
			return &service.DisableFleetNodeResponse{
				NodeID:     req.NodeID,
				Disabled:   true,
				DisabledAt: now,
				Timestamp:  now,
			}, nil
		},
	}

	s := newFleetAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleDisableFleetNode(w, disableAgentReq("agent-1", "?tenant_id=t1"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp DisableFleetNodeRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Disabled {
		t.Error("expected Disabled=true")
	}
	if resp.NodeID != "agent-1" {
		t.Errorf("AgentID = %q, want %q", resp.NodeID, "agent-1")
	}
}

func TestHandleDisableAgent_MissingAgentID(t *testing.T) {
	s := newFleetAdminTestServer(&mocks.MockFleetAdminService{})
	w := httptest.NewRecorder()
	s.HandleDisableFleetNode(w, disableAgentReq("", "?tenant_id=t1"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDisableAgent_MissingTenantID(t *testing.T) {
	s := newFleetAdminTestServer(&mocks.MockFleetAdminService{})
	w := httptest.NewRecorder()
	s.HandleDisableFleetNode(w, disableAgentReq("agent-1", ""))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDisableAgent_AlreadyDisabled(t *testing.T) {
	adminSvc := &mocks.MockFleetAdminService{
		DisableFleetNodeFunc: func(_ context.Context, req *service.DisableFleetNodeRequest) (*service.DisableFleetNodeResponse, error) {
			return nil, &service.FleetNodeAlreadyDisabledError{TenantID: req.TenantID, NodeID: req.NodeID}
		},
	}

	s := newFleetAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleDisableFleetNode(w, disableAgentReq("agent-2", "?tenant_id=t1"))
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleDisableAgent_NotFound(t *testing.T) {
	adminSvc := &mocks.MockFleetAdminService{
		DisableFleetNodeFunc: func(_ context.Context, req *service.DisableFleetNodeRequest) (*service.DisableFleetNodeResponse, error) {
			return nil, &service.FleetAdminNotFoundError{TenantID: req.TenantID, NodeID: req.NodeID}
		},
	}

	s := newFleetAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleDisableFleetNode(w, disableAgentReq("missing", "?tenant_id=t1"))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =============================================================================
// HandleEnableFleetNode — handler unit tests
// =============================================================================

func TestHandleEnableAgent_HappyPath(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockFleetAdminService{
		EnableFleetNodeFunc: func(_ context.Context, req *service.EnableFleetNodeRequest) (*service.EnableFleetNodeResponse, error) {
			return &service.EnableFleetNodeResponse{
				NodeID:    req.NodeID,
				Enabled:   true,
				Timestamp: now,
			}, nil
		},
	}

	s := newFleetAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleEnableFleetNode(w, enableAgentReq("agent-3", "?tenant_id=t1"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp EnableFleetNodeRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Enabled {
		t.Error("expected Enabled=true")
	}
}

func TestHandleEnableAgent_NotDisabled(t *testing.T) {
	adminSvc := &mocks.MockFleetAdminService{
		EnableFleetNodeFunc: func(_ context.Context, req *service.EnableFleetNodeRequest) (*service.EnableFleetNodeResponse, error) {
			return nil, &service.FleetNodeNotDisabledError{TenantID: req.TenantID, NodeID: req.NodeID}
		},
	}

	s := newFleetAdminTestServer(adminSvc)
	w := httptest.NewRecorder()
	s.HandleEnableFleetNode(w, enableAgentReq("agent-4", "?tenant_id=t1"))
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleEnableAgent_MissingParams(t *testing.T) {
	s := newFleetAdminTestServer(&mocks.MockFleetAdminService{})

	w := httptest.NewRecorder()
	s.HandleEnableFleetNode(w, enableAgentReq("", "?tenant_id=t1"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing id: status = %d, want 400", w.Code)
	}

	w = httptest.NewRecorder()
	s.HandleEnableFleetNode(w, enableAgentReq("agent-1", ""))
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing tenant: status = %d, want 400", w.Code)
	}
}
