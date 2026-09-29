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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

func TestHandleGrantAgentRole_Success(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		GrantFleetNodeRoleFunc: func(_ context.Context, req *service.GrantFleetNodeRoleRequest) (*service.GrantFleetNodeRoleResponse, error) {
			return &service.GrantFleetNodeRoleResponse{
				ProjectID: req.ProjectID, FleetNodeID: req.FleetNodeID, Role: req.Role, Timestamp: time.Now(),
			}, nil
		},
	}
	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Post("/projects/{id}/permissions/{node_id}", srv.HandleGrantFleetNodeRole)

	req := httptest.NewRequest(http.MethodPost, "/projects/p1/permissions/a1?tenant_id=t1",
		bytes.NewReader([]byte(`{"role":"lead"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestHandleGrantAgentRole_Validation(t *testing.T) {
	srv := newProjectTestServer(&mocks.MockProjectService{})
	r := chi.NewRouter()
	r.Post("/projects/{id}/permissions/{node_id}", srv.HandleGrantFleetNodeRole)

	// Missing tenant_id
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/projects/p1/permissions/a1",
		bytes.NewReader([]byte(`{"role":"lead"}`))))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing role
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/projects/p1/permissions/a1?tenant_id=t1",
		bytes.NewReader([]byte(`{}`))))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/projects/p1/permissions/a1?tenant_id=t1",
		bytes.NewReader([]byte("{bad"))))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGrantAgentRole_NotFound(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		GrantFleetNodeRoleFunc: func(_ context.Context, _ *service.GrantFleetNodeRoleRequest) (*service.GrantFleetNodeRoleResponse, error) {
			return nil, &service.ProjectNotFoundError{ProjectID: "p1"}
		},
	}
	srv := newProjectTestServer(mockSvc)
	r := chi.NewRouter()
	r.Post("/projects/{id}/permissions/{node_id}", srv.HandleGrantFleetNodeRole)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/projects/p1/permissions/a1?tenant_id=t1",
		bytes.NewReader([]byte(`{"role":"lead"}`))))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleRevokeAgentRole_Success(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		RevokeFleetNodeRoleFunc: func(_ context.Context, req *service.RevokeFleetNodeRoleRequest) (*service.RevokeFleetNodeRoleResponse, error) {
			return &service.RevokeFleetNodeRoleResponse{ProjectID: req.ProjectID, FleetNodeID: req.FleetNodeID, Timestamp: time.Now()}, nil
		},
	}
	srv := newProjectTestServer(mockSvc)
	r := chi.NewRouter()
	r.Delete("/projects/{id}/permissions/{node_id}", srv.HandleRevokeFleetNodeRole)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/projects/p1/permissions/a1?tenant_id=t1", nil))
	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestHandleRevokeAgentRole_Validation(t *testing.T) {
	srv := newProjectTestServer(&mocks.MockProjectService{})
	r := chi.NewRouter()
	r.Delete("/projects/{id}/permissions/{node_id}", srv.HandleRevokeFleetNodeRole)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/projects/p1/permissions/a1", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateProject_ServiceError(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		UpdateProjectFunc: func(_ context.Context, _ *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			return nil, &service.ProjectInvalidError{Field: "name", Message: "bad"}
		},
	}
	srv := newProjectTestServer(mockSvc)
	r := chi.NewRouter()
	r.Put("/projects/{id}", srv.HandleUpdateProject)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/projects/p1?tenant_id=t1",
		bytes.NewReader([]byte(`{"name":"x"}`))))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

var _ = orchestration.ProjectRoleLead

func TestHandleDeleteProject_Cover(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		DeleteProjectFunc: func(_ context.Context, _ *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			return &service.DeleteProjectResponse{}, nil
		},
	}
	srv := newProjectTestServer(mockSvc)
	r := chi.NewRouter()
	r.Delete("/projects/{id}", srv.HandleDeleteProject)

	// Missing tenant_id -> 400
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/projects/p1", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Success
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/projects/p1?tenant_id=t1", nil))
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusNoContent)
}

func TestHandleUpdateProject_AllFields(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		UpdateProjectFunc: func(_ context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			// go-quicbaas#1058: the project's spend limits reach the service.
			if req.Spend == nil || len(req.Spend.Windows) != 1 || req.Spend.Windows[0].Size != 24*time.Hour ||
				req.Spend.Windows[0].MaxTokens != 100 {
				t.Errorf("spend = %+v, want the daily window", req.Spend)
			}
			return &service.UpdateProjectResponse{
				Project:   &orchestration.Project{ID: req.ProjectID, TenantID: req.TenantID, Name: "n"},
				Timestamp: time.Now(),
			}, nil
		},
	}
	srv := newProjectTestServer(mockSvc)
	r := chi.NewRouter()
	r.Put("/projects/{id}", srv.HandleUpdateProject)

	body := `{"name":"n","description":"d","clear_description":false,"workspace_agent_id":"a","workspace_path":"/w","host_affinity":"strict","default_lead_agent":{"agent_id":"lead"},"git_repo_url":"https://g","git_default_branch":"main","git_current_sha":"abc","spend":{"windows":[{"name":"daily","size":86400000000000,"max_tokens":100}]},"gpu_minutes":5,"max_concurrency":3}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/projects/p1?tenant_id=t1", bytes.NewReader([]byte(body))))
	assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestHandleUpdateProject_MissingProjectID(t *testing.T) {
	srv := newProjectTestServer(&mocks.MockProjectService{})
	r := chi.NewRouter()
	r.Put("/projects/{id}", srv.HandleUpdateProject)

	// invalid JSON via direct call with empty id chi param
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/projects/x?tenant_id=t1", bytes.NewReader([]byte("{bad")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "p1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	srv.HandleUpdateProject(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
