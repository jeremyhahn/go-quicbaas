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

// newProjectTestServer builds a minimal REST Server wired with a mock project service.
func newProjectTestServer(projectSvc service.ProjectService) *Server {
	mock := mocks.NewMockQuicBaaS()
	mock.ProjectServiceFunc = func() service.ProjectService { return projectSvc }
	return &Server{service: mock, router: chi.NewRouter()}
}

// =============================================================================
// HandleCreateProject
// =============================================================================

func TestHandleCreateProject_HappyPath(t *testing.T) {
	mockProject := &orchestration.Project{
		ID:        "proj-1",
		TenantID:  "tenant-1",
		Name:      "my-project",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	mockSvc := &mocks.MockProjectService{
		CreateProjectFunc: func(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			return &service.CreateProjectResponse{
				Project:   mockProject,
				Timestamp: time.Now(),
			}, nil
		},
	}

	srv := newProjectTestServer(mockSvc)

	body := map[string]interface{}{
		"tenant_id": "tenant-1",
		"name":      "my-project",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.HandleCreateProject(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp CreateProjectRESTResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Project == nil {
		t.Fatal("expected project in response")
	}
	if resp.Project.Name != "my-project" {
		t.Errorf("expected 'my-project', got %q", resp.Project.Name)
	}
}

func TestHandleCreateProject_MissingTenantID(t *testing.T) {
	mockSvc := &mocks.MockProjectService{}
	srv := newProjectTestServer(mockSvc)

	body := map[string]interface{}{"name": "x"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	srv.HandleCreateProject(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCreateProject_MissingName(t *testing.T) {
	mockSvc := &mocks.MockProjectService{}
	srv := newProjectTestServer(mockSvc)

	body := map[string]interface{}{"tenant_id": "tenant-1"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	srv.HandleCreateProject(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCreateProject_InvalidJSON(t *testing.T) {
	mockSvc := &mocks.MockProjectService{}
	srv := newProjectTestServer(mockSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()

	srv.HandleCreateProject(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCreateProject_ServiceUnavailable(t *testing.T) {
	mock := mocks.NewMockQuicBaaS()
	// Leave ProjectServiceFunc nil so Project() returns nil.
	srv := &Server{service: mock, router: chi.NewRouter()}

	body := map[string]interface{}{"tenant_id": "t", "name": "n"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	srv.HandleCreateProject(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// =============================================================================
// HandleGetProject
// =============================================================================

func TestHandleGetProject_HappyPath(t *testing.T) {
	mockProject := &orchestration.Project{
		ID:        "proj-1",
		TenantID:  "tenant-1",
		Name:      "fetched",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	mockSvc := &mocks.MockProjectService{
		GetProjectFunc: func(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return &service.GetProjectResponse{Project: mockProject, Timestamp: time.Now()}, nil
		},
	}

	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Get("/projects/{id}", srv.HandleGetProject)

	req := httptest.NewRequest(http.MethodGet, "/projects/proj-1?tenant_id=tenant-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetProject_MissingTenantID(t *testing.T) {
	mockSvc := &mocks.MockProjectService{}
	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Get("/projects/{id}", srv.HandleGetProject)

	req := httptest.NewRequest(http.MethodGet, "/projects/proj-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetProject_NotFound(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		GetProjectFunc: func(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return nil, &service.ProjectNotFoundError{TenantID: "t", ProjectID: "ghost"}
		},
	}

	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Get("/projects/{id}", srv.HandleGetProject)

	req := httptest.NewRequest(http.MethodGet, "/projects/ghost?tenant_id=t", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// HandleListProjects
// =============================================================================

func TestHandleListProjects_HappyPath(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		ListProjectsFunc: func(ctx context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			return &service.ListProjectsResponse{
				Projects: []*orchestration.Project{
					{ID: "1", TenantID: "t", Name: "a", CreatedAt: time.Now(), UpdatedAt: time.Now()},
					{ID: "2", TenantID: "t", Name: "b", CreatedAt: time.Now(), UpdatedAt: time.Now()},
				},
				Total:     2,
				Timestamp: time.Now(),
			}, nil
		},
	}

	srv := newProjectTestServer(mockSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/?tenant_id=t", nil)
	w := httptest.NewRecorder()

	srv.HandleListProjects(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ListProjectsRESTResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
}

func TestHandleListProjects_MissingTenantID(t *testing.T) {
	mockSvc := &mocks.MockProjectService{}
	srv := newProjectTestServer(mockSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/", nil)
	w := httptest.NewRecorder()
	srv.HandleListProjects(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// =============================================================================
// HandleDeleteProject
// =============================================================================

func TestHandleDeleteProject_HappyPath(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		DeleteProjectFunc: func(ctx context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			return &service.DeleteProjectResponse{
				ProjectID: "proj-1",
				Deleted:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}

	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Delete("/projects/{id}", srv.HandleDeleteProject)

	req := httptest.NewRequest(http.MethodDelete, "/projects/proj-1?tenant_id=t", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteProject_NotFound(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		DeleteProjectFunc: func(ctx context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			return nil, &service.ProjectNotFoundError{TenantID: "t", ProjectID: "ghost"}
		},
	}

	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Delete("/projects/{id}", srv.HandleDeleteProject)

	req := httptest.NewRequest(http.MethodDelete, "/projects/ghost?tenant_id=t", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// =============================================================================
// HandleUpdateProject
// =============================================================================

func TestHandleUpdateProject_HappyPath(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		UpdateProjectFunc: func(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			return &service.UpdateProjectResponse{
				Project: &orchestration.Project{
					ID:          "proj-1",
					TenantID:    "t",
					Name:        req.Name,
					Description: req.Description,
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Put("/projects/{id}", srv.HandleUpdateProject)

	body := map[string]interface{}{"description": "updated"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/projects/proj-1?tenant_id=t", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateProject_MissingTenantID(t *testing.T) {
	mockSvc := &mocks.MockProjectService{}
	srv := newProjectTestServer(mockSvc)

	r := chi.NewRouter()
	r.Put("/projects/{id}", srv.HandleUpdateProject)

	body := map[string]interface{}{"description": "x"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/projects/proj-1", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
