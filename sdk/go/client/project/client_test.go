// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package project

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a minimal transport stub for unit tests.
type mockTransport struct {
	requestFn func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	return nil, nil
}

func (m *mockTransport) Conn() interface{} { return nil }
func (m *mockTransport) Close() error      { return nil }
func (m *mockTransport) Healthy() bool     { return true }

// =============================================================================
// NewClient
// =============================================================================

func TestNewClient_WithTransport(t *testing.T) {
	c := NewClient(&mockTransport{})
	if c == nil {
		t.Error("expected non-nil client")
	}
}

func TestNewClient_NilTransport(t *testing.T) {
	c := NewClient(nil)
	if c != nil {
		t.Error("expected nil client for nil transport")
	}
}

// =============================================================================
// Create
// =============================================================================

func TestClient_Create_HappyPath(t *testing.T) {
	now := time.Now()
	mt := &mockTransport{
		requestFn: func(_ context.Context, svc, method string, req interface{}) (interface{}, error) {
			if svc != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, svc)
			}
			if method != "Create" {
				t.Errorf("expected method 'Create', got %q", method)
			}
			return &ProjectInfo{
				ID:        "proj-1",
				TenantID:  "tenant-1",
				Name:      "my-project",
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		},
	}
	c := NewClient(mt)
	info, err := c.Create(context.Background(), &CreateProjectRequest{
		TenantID: "tenant-1",
		Name:     "my-project",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "proj-1" {
		t.Errorf("expected ID='proj-1', got %q", info.ID)
	}
	if info.Name != "my-project" {
		t.Errorf("expected Name='my-project', got %q", info.Name)
	}
}

func TestClient_Create_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Create(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Create_MissingTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Create(context.Background(), &CreateProjectRequest{Name: "x"})
	if err == nil {
		t.Fatal("expected error for missing tenant ID")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Create_MissingName(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Create(context.Background(), &CreateProjectRequest{TenantID: "t"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Create_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck // intentional nil context test
	_, err := c.Create(nil, &CreateProjectRequest{TenantID: "t", Name: "n"})
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", errors.Code(err))
	}
}

func TestClient_Create_NilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	_, err := c.Create(context.Background(), &CreateProjectRequest{TenantID: "t", Name: "n"})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", errors.Code(err))
	}
}

func TestClient_Create_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return "not-a-project", nil
		},
	}
	c := NewClient(mt)
	_, err := c.Create(context.Background(), &CreateProjectRequest{TenantID: "t", Name: "n"})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", errors.Code(err))
	}
}

func TestClient_Create_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "server unavailable")
		},
	}
	c := NewClient(mt)
	_, err := c.Create(context.Background(), &CreateProjectRequest{TenantID: "t", Name: "n"})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", errors.Code(err))
	}
}

// =============================================================================
// Get
// =============================================================================

func TestClient_Get_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, svc, method string, req interface{}) (interface{}, error) {
			if method != "Get" {
				t.Errorf("expected method 'Get', got %q", method)
			}
			r := req.(*GetProjectRequest)
			if r.TenantID != "tenant-1" || r.ProjectID != "proj-1" {
				t.Errorf("unexpected request fields: %+v", r)
			}
			return &ProjectInfo{ID: "proj-1", TenantID: "tenant-1", Name: "p"}, nil
		},
	}
	c := NewClient(mt)
	info, err := c.Get(context.Background(), "tenant-1", "proj-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "proj-1" {
		t.Errorf("expected ID='proj-1', got %q", info.ID)
	}
}

func TestClient_Get_EmptyTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Get(context.Background(), "", "proj-1")
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Get_EmptyProjectID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Get(context.Background(), "tenant-1", "")
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Get_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck
	_, err := c.Get(nil, "t", "p")
	if err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_Get_NilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	_, err := c.Get(context.Background(), "t", "p")
	if err == nil || errors.Code(err) != errors.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

// =============================================================================
// GetByName
// =============================================================================

func TestClient_GetByName_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			if method != "GetByName" {
				t.Errorf("expected method 'GetByName', got %q", method)
			}
			return &ProjectInfo{ID: "proj-1", TenantID: "tenant-1", Name: "named"}, nil
		},
	}
	c := NewClient(mt)
	info, err := c.GetByName(context.Background(), "tenant-1", "named")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name != "named" {
		t.Errorf("expected Name='named', got %q", info.Name)
	}
}

func TestClient_GetByName_EmptyName(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GetByName(context.Background(), "tenant-1", "")
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_GetByName_EmptyTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GetByName(context.Background(), "", "some-name")
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_GetByName_NilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	_, err := c.GetByName(context.Background(), "t", "name")
	if err == nil || errors.Code(err) != errors.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

// =============================================================================
// Update
// =============================================================================

func TestClient_Update_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			if method != "Update" {
				t.Errorf("expected method 'Update', got %q", method)
			}
			return &ProjectInfo{ID: "proj-1", TenantID: "tenant-1", Name: "updated", Description: "new desc"}, nil
		},
	}
	c := NewClient(mt)
	info, err := c.Update(context.Background(), &UpdateProjectRequest{
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		Description: "new desc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Description != "new desc" {
		t.Errorf("expected Description='new desc', got %q", info.Description)
	}
}

func TestClient_Update_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Update(context.Background(), nil)
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Update_MissingTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Update(context.Background(), &UpdateProjectRequest{ProjectID: "p"})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Update_MissingProjectID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.Update(context.Background(), &UpdateProjectRequest{TenantID: "t"})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Update_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck
	_, err := c.Update(nil, &UpdateProjectRequest{TenantID: "t", ProjectID: "p"})
	if err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_Update_NilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	_, err := c.Update(context.Background(), &UpdateProjectRequest{TenantID: "t", ProjectID: "p"})
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

// =============================================================================
// Delete
// =============================================================================

func TestClient_Delete_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			if method != "Delete" {
				t.Errorf("expected method 'Delete', got %q", method)
			}
			return &DeleteProjectResponse{Deleted: true}, nil
		},
	}
	c := NewClient(mt)
	if err := c.Delete(context.Background(), "tenant-1", "proj-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_Delete_NilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	// nil response is treated as success (delete acknowledged without body)
	if err := c.Delete(context.Background(), "t", "p"); err != nil {
		t.Fatalf("unexpected error for nil response: %v", err)
	}
}

func TestClient_Delete_EmptyTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	err := c.Delete(context.Background(), "", "p")
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Delete_EmptyProjectID(t *testing.T) {
	c := NewClient(&mockTransport{})
	err := c.Delete(context.Background(), "t", "")
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_Delete_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck
	err := c.Delete(nil, "t", "p")
	if err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_Delete_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "unavailable")
		},
	}
	c := NewClient(mt)
	err := c.Delete(context.Background(), "t", "p")
	if err == nil || errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

func TestClient_Delete_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return "bad", nil
		},
	}
	c := NewClient(mt)
	err := c.Delete(context.Background(), "t", "p")
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

// =============================================================================
// List
// =============================================================================

func TestClient_List_HappyPath(t *testing.T) {
	now := time.Now()
	mt := &mockTransport{
		requestFn: func(_ context.Context, svc, method string, req interface{}) (interface{}, error) {
			if svc != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, svc)
			}
			if method != "List" {
				t.Errorf("expected method 'List', got %q", method)
			}
			r := req.(*ListProjectsRequest)
			if r.TenantID != "tenant-1" {
				t.Errorf("expected TenantID='tenant-1', got %q", r.TenantID)
			}
			return &ListProjectsResponse{
				Projects: []*ProjectInfo{
					{ID: "p1", TenantID: "tenant-1", Name: "a", CreatedAt: now},
					{ID: "p2", TenantID: "tenant-1", Name: "b", CreatedAt: now},
				},
				Total: 2,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.List(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected Total=2, got %d", resp.Total)
	}
	if len(resp.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(resp.Projects))
	}
}

func TestClient_List_EmptyTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.List(context.Background(), "")
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_List_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck
	_, err := c.List(nil, "t")
	if err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_List_NilResponse_ReturnsEmpty(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.List(context.Background(), "t")
	if err != nil {
		t.Fatalf("unexpected error for nil response: %v", err)
	}
	if len(resp.Projects) != 0 {
		t.Errorf("expected empty projects, got %d", len(resp.Projects))
	}
}

func TestClient_List_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return 123, nil
		},
	}
	c := NewClient(mt)
	_, err := c.List(context.Background(), "t")
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

func TestClient_List_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "unavailable")
		},
	}
	c := NewClient(mt)
	_, err := c.List(context.Background(), "t")
	if err == nil || errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("expected CodeUnavailable, got %v", err)
	}
}

// =============================================================================
// Validation helpers
// =============================================================================

func TestValidateCreateRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *CreateProjectRequest
		wantErr bool
	}{
		{"nil request", nil, true},
		{"missing tenant ID", &CreateProjectRequest{Name: "x"}, true},
		{"missing name", &CreateProjectRequest{TenantID: "t"}, true},
		{"valid minimal", &CreateProjectRequest{TenantID: "t", Name: "n"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCreateRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCreateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateUpdateRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *UpdateProjectRequest
		wantErr bool
	}{
		{"nil request", nil, true},
		{"missing tenant ID", &UpdateProjectRequest{ProjectID: "p"}, true},
		{"missing project ID", &UpdateProjectRequest{TenantID: "t"}, true},
		{"valid", &UpdateProjectRequest{TenantID: "t", ProjectID: "p"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUpdateRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUpdateRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ProjectInfo field coverage
// =============================================================================

// =============================================================================
// GrantAgentRole (Gap 4)
// =============================================================================

func TestClient_GrantAgentRole_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			if method != "GrantAgentRole" {
				t.Errorf("expected method 'GrantAgentRole', got %q", method)
			}
			r, ok := req.(*GrantAgentRoleRequest)
			if !ok {
				t.Errorf("expected *GrantAgentRoleRequest, got %T", req)
			}
			if r.AgentID != "agent-1" || r.Role != ProjectRoleWorker {
				t.Errorf("unexpected request: %+v", r)
			}
			return &GrantAgentRoleResponse{
				ProjectID: r.ProjectID,
				AgentID:   r.AgentID,
				Role:      r.Role,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		TenantID:  "tenant-1",
		ProjectID: "proj-1",
		AgentID:   "agent-1",
		Role:      ProjectRoleWorker,
	})
	if err != nil {
		t.Fatalf("GrantAgentRole: %v", err)
	}
	if resp.AgentID != "agent-1" || resp.Role != ProjectRoleWorker {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_GrantAgentRole_MissingTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		ProjectID: "proj-1",
		AgentID:   "agent-1",
		Role:      ProjectRoleWorker,
	})
	if err == nil {
		t.Fatal("expected error for missing TenantID, got nil")
	}
}

func TestClient_GrantAgentRole_MissingAgentID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		TenantID:  "tenant-1",
		ProjectID: "proj-1",
		Role:      ProjectRoleWorker,
	})
	if err == nil {
		t.Fatal("expected error for missing AgentID, got nil")
	}
}

func TestClient_GrantAgentRole_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("project.GrantAgentRole", "grant failed")
		},
	}
	c := NewClient(mt)
	_, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		TenantID:  "tenant-1",
		ProjectID: "proj-1",
		AgentID:   "agent-1",
		Role:      ProjectRoleWorker,
	})
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

// =============================================================================
// RevokeAgentRole (Gap 4)
// =============================================================================

func TestClient_RevokeAgentRole_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			if method != "RevokeAgentRole" {
				t.Errorf("expected method 'RevokeAgentRole', got %q", method)
			}
			r, ok := req.(*RevokeAgentRoleRequest)
			if !ok {
				t.Errorf("expected *RevokeAgentRoleRequest, got %T", req)
			}
			return &RevokeAgentRoleResponse{
				ProjectID: r.ProjectID,
				AgentID:   r.AgentID,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.RevokeAgentRole(context.Background(), &RevokeAgentRoleRequest{
		TenantID:  "tenant-1",
		ProjectID: "proj-1",
		AgentID:   "agent-1",
	})
	if err != nil {
		t.Fatalf("RevokeAgentRole: %v", err)
	}
	if resp.AgentID != "agent-1" {
		t.Errorf("expected AgentID='agent-1', got %q", resp.AgentID)
	}
}

func TestClient_RevokeAgentRole_MissingProjectID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.RevokeAgentRole(context.Background(), &RevokeAgentRoleRequest{
		TenantID: "tenant-1",
		AgentID:  "agent-1",
	})
	if err == nil {
		t.Fatal("expected error for missing ProjectID, got nil")
	}
}

func TestClient_RevokeAgentRole_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("project.RevokeAgentRole", "revoke failed")
		},
	}
	c := NewClient(mt)
	_, err := c.RevokeAgentRole(context.Background(), &RevokeAgentRoleRequest{
		TenantID:  "tenant-1",
		ProjectID: "proj-1",
		AgentID:   "agent-1",
	})
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestProjectInfo_AllFields(t *testing.T) {
	now := time.Now()
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return &ProjectInfo{
				ID:               "proj-full",
				TenantID:         "tenant-1",
				Name:             "full-project",
				Description:      "desc",
				Workspace:        ProjectWorkspace{AgentID: "agent-1", Path: "/ws"},
				HostAffinity:     "host-a",
				DefaultLeadAgent: "lead-1",
				Git: ProjectGit{
					RepoURL:       "https://github.com/example/repo",
					DefaultBranch: "main",
					CurrentSHA:    "abc123",
				},
				Permissions: ProjectPermissions{
					AgentRoles: map[string]ProjectRole{"agent-1": ProjectRoleLead},
				},
				Quotas: ProjectQuotas{
					TokenBudget:    1000000,
					GPUMinutes:     60,
					MaxConcurrency: 4,
				},
				Metadata:  map[string]string{"env": "prod"},
				CreatedAt: now,
				UpdatedAt: now,
			}, nil
		},
	}
	c := NewClient(mt)
	info, err := c.Create(context.Background(), &CreateProjectRequest{TenantID: "t", Name: "n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Workspace.AgentID != "agent-1" {
		t.Errorf("expected Workspace.AgentID='agent-1', got %q", info.Workspace.AgentID)
	}
	if info.Git.RepoURL != "https://github.com/example/repo" {
		t.Errorf("unexpected Git.RepoURL: %q", info.Git.RepoURL)
	}
	if info.Quotas.TokenBudget != 1000000 {
		t.Errorf("expected TokenBudget=1000000, got %d", info.Quotas.TokenBudget)
	}
	if info.Permissions.AgentRoles["agent-1"] != ProjectRoleLead {
		t.Errorf("expected lead role for agent-1")
	}
}

// =============================================================================
// Additional branch coverage: Get / GetByName / Update
// =============================================================================

func TestClient_Get_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("project.Get", "boom")
		},
	}
	c := NewClient(mt)
	if _, err := c.Get(context.Background(), "t", "p"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestClient_Get_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return "not-project-info", nil
		},
	}
	c := NewClient(mt)
	_, err := c.Get(context.Background(), "t", "p")
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

func TestClient_GetByName_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck
	if _, err := c.GetByName(nil, "t", "name"); err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_GetByName_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("project.GetByName", "boom")
		},
	}
	c := NewClient(mt)
	if _, err := c.GetByName(context.Background(), "t", "name"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestClient_GetByName_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return 123, nil
		},
	}
	c := NewClient(mt)
	_, err := c.GetByName(context.Background(), "t", "name")
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

func TestClient_Update_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("project.Update", "boom")
		},
	}
	c := NewClient(mt)
	_, err := c.Update(context.Background(), &UpdateProjectRequest{TenantID: "t", ProjectID: "p"})
	if err == nil {
		t.Fatal("expected transport error")
	}
}

func TestClient_Update_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return "nope", nil
		},
	}
	c := NewClient(mt)
	_, err := c.Update(context.Background(), &UpdateProjectRequest{TenantID: "t", ProjectID: "p"})
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

// =============================================================================
// Additional branch coverage: GrantAgentRole
// =============================================================================

func TestClient_GrantAgentRole_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck
	_, err := c.GrantAgentRole(nil, &GrantAgentRoleRequest{
		TenantID: "t", ProjectID: "p", AgentID: "a", Role: ProjectRoleWorker,
	})
	if err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_GrantAgentRole_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	if _, err := c.GrantAgentRole(context.Background(), nil); err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_GrantAgentRole_MissingProjectID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		TenantID: "t", AgentID: "a", Role: ProjectRoleWorker,
	})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_GrantAgentRole_MissingRole(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		TenantID: "t", ProjectID: "p", AgentID: "a",
	})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_GrantAgentRole_NilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	_, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		TenantID: "t", ProjectID: "p", AgentID: "a", Role: ProjectRoleWorker,
	})
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

func TestClient_GrantAgentRole_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return "nope", nil
		},
	}
	c := NewClient(mt)
	_, err := c.GrantAgentRole(context.Background(), &GrantAgentRoleRequest{
		TenantID: "t", ProjectID: "p", AgentID: "a", Role: ProjectRoleWorker,
	})
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

// =============================================================================
// Additional branch coverage: RevokeAgentRole
// =============================================================================

func TestClient_RevokeAgentRole_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	//nolint:staticcheck
	_, err := c.RevokeAgentRole(nil, &RevokeAgentRoleRequest{
		TenantID: "t", ProjectID: "p", AgentID: "a",
	})
	if err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_RevokeAgentRole_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	if _, err := c.RevokeAgentRole(context.Background(), nil); err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_RevokeAgentRole_MissingTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.RevokeAgentRole(context.Background(), &RevokeAgentRoleRequest{
		ProjectID: "p", AgentID: "a",
	})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_RevokeAgentRole_MissingAgentID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.RevokeAgentRole(context.Background(), &RevokeAgentRoleRequest{
		TenantID: "t", ProjectID: "p",
	})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_RevokeAgentRole_NilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)
	_, err := c.RevokeAgentRole(context.Background(), &RevokeAgentRoleRequest{
		TenantID: "t", ProjectID: "p", AgentID: "a",
	})
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}

func TestClient_RevokeAgentRole_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return 99, nil
		},
	}
	c := NewClient(mt)
	_, err := c.RevokeAgentRole(context.Background(), &RevokeAgentRoleRequest{
		TenantID: "t", ProjectID: "p", AgentID: "a",
	})
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}
