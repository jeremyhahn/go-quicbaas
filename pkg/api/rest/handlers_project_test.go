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

// =============================================================================
// DefaultWorkflow + git identity — the REST wire carries every new field
// =============================================================================

// TestHandleCreateProject_ForwardsDefaultWorkflowAndGitIdentity pins the flat
// REST field names against the service request and the response.
func TestHandleCreateProject_ForwardsDefaultWorkflowAndGitIdentity(t *testing.T) {
	var captured *service.CreateProjectRequest
	mockSvc := &mocks.MockProjectService{
		CreateProjectFunc: func(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			captured = req
			return &service.CreateProjectResponse{
				Project: &orchestration.Project{
					ID: "proj-1", TenantID: req.TenantID, Name: req.Name, DefaultWorkflow: req.DefaultWorkflow,
					Git: orchestration.ProjectGit{
						Author: orchestration.GitAuthor{Name: req.GitAuthorName, Email: req.GitAuthorEmail},
						Signing: orchestration.CommitSigning{
							Enabled:   req.GitSigningEnabled,
							Algorithm: req.GitSigningAlgorithm,
							Key:       orchestration.KMSKeyRef{Name: req.GitSigningKeyName, Scope: req.GitSigningKeyScope, Owner: req.GitSigningKeyOwner},
						},
						SSHKey: orchestration.KMSKeyRef{Name: req.GitSSHKeyName, Scope: req.GitSSHKeyScope, Owner: req.GitSSHKeyOwner},
					},
				},
				Timestamp: time.Now(),
			}, nil
		},
	}
	srv := newProjectTestServer(mockSvc)

	body := `{
		"tenant_id": "tenant-1", "name": "api",
		"default_workflow": "sdlc",
		"git_author_name": "agent-api", "git_author_email": "agent-api@example.test",
		"git_signing_enabled": true, "git_signing_algorithm": "ECDSA-P256",
		"git_signing_key_name": "api-signing", "git_signing_key_scope": "tenant", "git_signing_key_owner": "tenant-1",
		"git_ssh_key_name": "api-access", "git_ssh_key_scope": "service", "git_ssh_key_owner": "deploy"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.HandleCreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if captured == nil {
		t.Fatal("service was not called")
	}
	if captured.DefaultWorkflow != "sdlc" {
		t.Errorf("DefaultWorkflow = %q", captured.DefaultWorkflow)
	}
	if captured.GitAuthorName != "agent-api" || captured.GitAuthorEmail != "agent-api@example.test" {
		t.Errorf("author lost on the wire: %+v", captured)
	}
	if !captured.GitSigningEnabled || captured.GitSigningAlgorithm != "ECDSA-P256" ||
		captured.GitSigningKeyName != "api-signing" || captured.GitSigningKeyScope != "tenant" || captured.GitSigningKeyOwner != "tenant-1" {
		t.Errorf("signing lost on the wire: %+v", captured)
	}
	if captured.GitSSHKeyName != "api-access" || captured.GitSSHKeyScope != "service" || captured.GitSSHKeyOwner != "deploy" {
		t.Errorf("ssh key lost on the wire: %+v", captured)
	}

	var resp CreateProjectRESTResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p := resp.Project
	if p.DefaultWorkflow != "sdlc" || p.GitAuthorName != "agent-api" || p.GitAuthorEmail != "agent-api@example.test" ||
		!p.GitSigningEnabled || p.GitSigningAlgorithm != "ECDSA-P256" || p.GitSigningKeyName != "api-signing" ||
		p.GitSigningKeyScope != "tenant" || p.GitSigningKeyOwner != "tenant-1" ||
		p.GitSSHKeyName != "api-access" || p.GitSSHKeyScope != "service" || p.GitSSHKeyOwner != "deploy" {
		t.Errorf("response lost fields: %+v", p)
	}
}

// TestHandleUpdateProject_SigningEnabledIsTriState: an absent
// git_signing_enabled leaves signing unchanged (nil), an explicit false turns
// it off, and clear_default_workflow rides through.
func TestHandleUpdateProject_SigningEnabledIsTriState(t *testing.T) {
	var captured *service.UpdateProjectRequest
	mockSvc := &mocks.MockProjectService{
		UpdateProjectFunc: func(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			captured = req
			return &service.UpdateProjectResponse{Project: &orchestration.Project{ID: req.ProjectID}, Timestamp: time.Now()}, nil
		},
	}
	srv := newProjectTestServer(mockSvc)

	send := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/projects/proj-1?tenant_id=tenant-1", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "proj-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		srv.HandleUpdateProject(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	send(`{"default_workflow": "sdlc", "git_author_name": "agent"}`)
	if captured.GitSigningEnabled != nil {
		t.Error("an absent git_signing_enabled reached the service as a value")
	}
	if captured.DefaultWorkflow != "sdlc" || captured.GitAuthorName != "agent" {
		t.Errorf("fields lost on the wire: %+v", captured)
	}

	send(`{"git_signing_enabled": false, "clear_default_workflow": true, "git_ssh_key_name": "access"}`)
	if captured.GitSigningEnabled == nil || *captured.GitSigningEnabled {
		t.Error("an explicit false did not reach the service as false")
	}
	if !captured.ClearDefaultWorkflow || captured.GitSSHKeyName != "access" {
		t.Errorf("fields lost on the wire: %+v", captured)
	}

	send(`{"git_signing_enabled": true, "git_signing_key_name": "k", "git_signing_key_scope": "tenant", "git_signing_key_owner": "o", "git_signing_algorithm": "ED25519"}`)
	if captured.GitSigningEnabled == nil || !*captured.GitSigningEnabled ||
		captured.GitSigningKeyName != "k" || captured.GitSigningKeyScope != "tenant" || captured.GitSigningKeyOwner != "o" || captured.GitSigningAlgorithm != "ED25519" {
		t.Errorf("signing fields lost on the wire: %+v", captured)
	}
}

// =============================================================================
// Autonomy + context repositories — the REST wire carries both
// =============================================================================

// TestHandleProject_AutonomyAndContextReposRoundTrip drives create → get and
// update → get over the REST handlers, asserting both fields survive in each
// direction and that autonomous stays tri-state on update.
func TestHandleProject_AutonomyAndContextReposRoundTrip(t *testing.T) {
	stored := &orchestration.Project{ID: "proj-1", TenantID: "tenant-1", Name: "api"}
	var createReq *service.CreateProjectRequest
	var updateReq *service.UpdateProjectRequest

	mockSvc := &mocks.MockProjectService{
		CreateProjectFunc: func(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			createReq = req
			stored.Autonomous = req.Autonomous
			stored.ContextRepos = req.ContextRepos
			return &service.CreateProjectResponse{Project: stored, Timestamp: time.Now()}, nil
		},
		UpdateProjectFunc: func(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			updateReq = req
			if req.Autonomous != nil {
				stored.Autonomous = *req.Autonomous
			}
			if req.ClearContextRepos {
				stored.ContextRepos = nil
			} else if req.ContextRepos != nil {
				stored.ContextRepos = req.ContextRepos
			}
			return &service.UpdateProjectResponse{Project: stored, Timestamp: time.Now()}, nil
		},
		GetProjectFunc: func(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return &service.GetProjectResponse{Project: stored, Timestamp: time.Now()}, nil
		},
	}
	srv := newProjectTestServer(mockSvc)

	get := func() *ProjectRESTResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-1?tenant_id=tenant-1", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "proj-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		srv.HandleGetProject(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp GetProjectRESTResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode get: %v", err)
		}
		return resp.Project
	}

	update := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/projects/proj-1?tenant_id=tenant-1", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "proj-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		srv.HandleUpdateProject(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	createBody := `{
		"tenant_id": "tenant-1", "name": "api",
		"autonomous": true,
		"context_repos": [{"name": "handbook", "repo_url": "https://forge.example/org/handbook", "ref": "main"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader([]byte(createBody)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.HandleCreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if createReq == nil {
		t.Fatal("create service was not called")
	}
	if !createReq.Autonomous {
		t.Error("autonomous did not reach the service as true")
	}
	if len(createReq.ContextRepos) != 1 || createReq.ContextRepos[0].Name != "handbook" ||
		createReq.ContextRepos[0].RepoURL != "https://forge.example/org/handbook" || createReq.ContextRepos[0].Ref != "main" {
		t.Fatalf("context_repos lost on the wire: %+v", createReq.ContextRepos)
	}

	got := get()
	if !got.Autonomous {
		t.Error("autonomous lost on the way back")
	}
	if len(got.ContextRepos) != 1 || got.ContextRepos[0].Name != "handbook" {
		t.Fatalf("context_repos lost on the way back: %+v", got.ContextRepos)
	}

	// An absent autonomous leaves the switch alone; an absent context_repos
	// leaves the list alone.
	update(`{"name": "renamed"}`)
	if updateReq.Autonomous != nil {
		t.Error("an absent autonomous reached the service as a value")
	}
	if updateReq.ContextRepos != nil {
		t.Errorf("an absent context_repos reached the service as %+v", updateReq.ContextRepos)
	}
	if got = get(); !got.Autonomous || len(got.ContextRepos) != 1 {
		t.Errorf("an unrelated update changed the fields: %+v", got)
	}

	// An explicit false turns it off; a list replaces the whole list.
	update(`{"autonomous": false, "context_repos": [{"name": "specs", "repo_url": "https://forge.example/org/specs", "ref": "v2"}]}`)
	if updateReq.Autonomous == nil || *updateReq.Autonomous {
		t.Error("an explicit false did not reach the service as false")
	}
	got = get()
	if got.Autonomous {
		t.Error("autonomous is still on after an explicit false")
	}
	if len(got.ContextRepos) != 1 || got.ContextRepos[0].Name != "specs" {
		t.Fatalf("context_repos = %+v, want exactly the specs repo", got.ContextRepos)
	}

	// And clear_context_repos empties it.
	update(`{"clear_context_repos": true}`)
	if !updateReq.ClearContextRepos {
		t.Error("clear_context_repos did not reach the service")
	}
	if got = get(); len(got.ContextRepos) != 0 {
		t.Fatalf("context_repos = %+v after clear, want none", got.ContextRepos)
	}
}

// TestHandleCreateProject_RefusesInvalidContextRepos: the service's typed
// refusal becomes a 400, not a 500.
func TestHandleCreateProject_RefusesInvalidContextRepos(t *testing.T) {
	mockSvc := &mocks.MockProjectService{
		CreateProjectFunc: func(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			return nil, &service.ProjectInvalidError{
				Field:   "context_repos",
				Message: "name must be one path segment, with no separator",
			}
		},
	}
	srv := newProjectTestServer(mockSvc)

	body := `{"tenant_id": "tenant-1", "name": "api",
		"context_repos": [{"name": "a/b", "repo_url": "https://forge.example/a", "ref": "main"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.HandleCreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
