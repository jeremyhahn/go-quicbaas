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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// newPermTestServer builds a minimal REST Server wired with a mock project service.
func newPermTestServer(projectSvc service.ProjectService) *Server {
	mock := mocks.NewMockQuicBaaS()
	mock.ProjectServiceFunc = func() service.ProjectService { return projectSvc }
	return &Server{service: mock, router: chi.NewRouter()}
}

// chiCtxWith builds a chi route context with the given URL parameters.
func chiCtxWith(params map[string]string) *chi.Context {
	ctx := chi.NewRouteContext()
	for k, v := range params {
		ctx.URLParams.Add(k, v)
	}
	return ctx
}

// =============================================================================
// HandleGrantAgentRole
// =============================================================================

func TestHandleGrantAgentRole_HappyPath(t *testing.T) {
	now := time.Now()
	projectSvc := &mocks.MockProjectService{
		GrantAgentRoleFunc: func(ctx context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			return &service.GrantAgentRoleResponse{
				ProjectID: req.ProjectID,
				AgentID:   req.AgentID,
				Role:      req.Role,
				Timestamp: now,
			}, nil
		},
	}

	body, _ := json.Marshal(GrantAgentRoleRESTRequest{Role: orchestration.ProjectRoleWorker})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/projects/proj-1/permissions/agent-A?tenant_id=t1", bytes.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtxWith(map[string]string{
		"id":       "proj-1",
		"agent_id": "agent-A",
	})))
	w := httptest.NewRecorder()

	s := newPermTestServer(projectSvc)
	s.HandleGrantAgentRole(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp GrantAgentRoleRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Role != orchestration.ProjectRoleWorker {
		t.Errorf("role = %q, want %q", resp.Role, orchestration.ProjectRoleWorker)
	}
	if resp.AgentID != "agent-A" {
		t.Errorf("agent_id = %q, want %q", resp.AgentID, "agent-A")
	}
}

func TestHandleGrantAgentRole_MissingProjectID(t *testing.T) {
	s := newPermTestServer(&mocks.MockProjectService{})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/projects//permissions/agent-A?tenant_id=t1", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtxWith(map[string]string{
		"id":       "",
		"agent_id": "agent-A",
	})))
	w := httptest.NewRecorder()

	s.HandleGrantAgentRole(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGrantAgentRole_MissingRole(t *testing.T) {
	s := newPermTestServer(&mocks.MockProjectService{})
	body, _ := json.Marshal(map[string]string{}) // no role field
	r := httptest.NewRequest(http.MethodPut, "/api/v1/projects/proj-1/permissions/agent-A?tenant_id=t1", bytes.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtxWith(map[string]string{
		"id":       "proj-1",
		"agent_id": "agent-A",
	})))
	w := httptest.NewRecorder()

	s.HandleGrantAgentRole(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGrantAgentRole_ProjectNotFound(t *testing.T) {
	projectSvc := &mocks.MockProjectService{
		GrantAgentRoleFunc: func(ctx context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
		},
	}

	body, _ := json.Marshal(GrantAgentRoleRESTRequest{Role: orchestration.ProjectRoleWorker})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/projects/no-such/permissions/agent-A?tenant_id=t1", bytes.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtxWith(map[string]string{
		"id":       "no-such",
		"agent_id": "agent-A",
	})))
	w := httptest.NewRecorder()

	s := newPermTestServer(projectSvc)
	s.HandleGrantAgentRole(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =============================================================================
// HandleRevokeAgentRole
// =============================================================================

func TestHandleRevokeAgentRole_HappyPath(t *testing.T) {
	now := time.Now()
	projectSvc := &mocks.MockProjectService{
		RevokeAgentRoleFunc: func(ctx context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
			return &service.RevokeAgentRoleResponse{
				ProjectID: req.ProjectID,
				AgentID:   req.AgentID,
				Timestamp: now,
			}, nil
		},
	}

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/proj-1/permissions/agent-A?tenant_id=t1", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtxWith(map[string]string{
		"id":       "proj-1",
		"agent_id": "agent-A",
	})))
	w := httptest.NewRecorder()

	s := newPermTestServer(projectSvc)
	s.HandleRevokeAgentRole(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp RevokeAgentRoleRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.AgentID != "agent-A" {
		t.Errorf("agent_id = %q, want %q", resp.AgentID, "agent-A")
	}
}

func TestHandleRevokeAgentRole_MissingAgentID(t *testing.T) {
	s := newPermTestServer(&mocks.MockProjectService{})
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/proj-1/permissions/?tenant_id=t1", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtxWith(map[string]string{
		"id":       "proj-1",
		"agent_id": "",
	})))
	w := httptest.NewRecorder()

	s.HandleRevokeAgentRole(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRevokeAgentRole_ProjectNotFound(t *testing.T) {
	projectSvc := &mocks.MockProjectService{
		RevokeAgentRoleFunc: func(ctx context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
			return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
		},
	}

	r := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/no-such/permissions/agent-A?tenant_id=t1", nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chiCtxWith(map[string]string{
		"id":       "no-such",
		"agent_id": "agent-A",
	})))
	w := httptest.NewRecorder()

	s := newPermTestServer(projectSvc)
	s.HandleRevokeAgentRole(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
