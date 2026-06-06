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

// =============================================================================
// HandleDisableAgent
// =============================================================================

func TestHandleDisableAgent_HappyPath(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockAgentAdminService{
		DisableAgentFunc: func(ctx context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return &service.DisableAgentResponse{
				AgentID:    req.AgentID,
				Disabled:   true,
				DisabledAt: now,
				Timestamp:  now,
			}, nil
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/disable?tenant_id=t1", nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", "agent-1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
	w := httptest.NewRecorder()

	s.HandleDisableAgent(w, r)

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
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents//disable?tenant_id=t1", nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", "")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
	w := httptest.NewRecorder()

	s.HandleDisableAgent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDisableAgent_MissingTenantID(t *testing.T) {
	s := newAgentAdminTestServer(&mocks.MockAgentAdminService{})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/disable", nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", "agent-1")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
	w := httptest.NewRecorder()

	s.HandleDisableAgent(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDisableAgent_AlreadyDisabled(t *testing.T) {
	adminSvc := &mocks.MockAgentAdminService{
		DisableAgentFunc: func(ctx context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return nil, &service.AgentAlreadyDisabledError{TenantID: req.TenantID, AgentID: req.AgentID}
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-2/disable?tenant_id=t1", nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", "agent-2")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
	w := httptest.NewRecorder()

	s.HandleDisableAgent(w, r)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestHandleDisableAgent_NotFound(t *testing.T) {
	adminSvc := &mocks.MockAgentAdminService{
		DisableAgentFunc: func(ctx context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return nil, &service.AgentAdminNotFoundError{TenantID: req.TenantID, AgentID: req.AgentID}
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents/missing/disable?tenant_id=t1", nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", "missing")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
	w := httptest.NewRecorder()

	s.HandleDisableAgent(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =============================================================================
// HandleEnableAgent
// =============================================================================

func TestHandleEnableAgent_HappyPath(t *testing.T) {
	now := time.Now()
	adminSvc := &mocks.MockAgentAdminService{
		EnableAgentFunc: func(ctx context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
			return &service.EnableAgentResponse{
				AgentID:   req.AgentID,
				Enabled:   true,
				Timestamp: now,
			}, nil
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-3/enable?tenant_id=t1", nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", "agent-3")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
	w := httptest.NewRecorder()

	s.HandleEnableAgent(w, r)

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
		EnableAgentFunc: func(ctx context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
			return nil, &service.AgentNotDisabledError{TenantID: req.TenantID, AgentID: req.AgentID}
		},
	}

	s := newAgentAdminTestServer(adminSvc)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-4/enable?tenant_id=t1", nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", "agent-4")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
	w := httptest.NewRecorder()

	s.HandleEnableAgent(w, r)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func enableAgentReq(agentID, query string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/enable"+query, nil)
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("id", agentID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtx))
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
