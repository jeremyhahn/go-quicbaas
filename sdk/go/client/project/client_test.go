// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package project

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
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

// stubTransport returns a transport whose every request answers resp, err.
func stubTransport(resp interface{}, err error) *mockTransport {
	return &mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return resp, err
		},
	}
}

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

func TestClient_Create_Errors(t *testing.T) {
	valid := &CreateProjectRequest{TenantID: "t", Name: "n"}
	tests := []struct {
		name     string
		ctx      context.Context
		req      *CreateProjectRequest
		resp     interface{}
		respErr  error
		wantCode errors.ErrorCode
	}{
		{"nil context", nil, valid, nil, nil, errors.CodeInvalidInput},
		{"invalid request", context.Background(), nil, nil, nil, errors.CodeInvalidInput},
		{"transport error", context.Background(), valid, nil, errors.NewUnavailable("test", "server unavailable"), errors.CodeUnavailable},
		{"nil response", context.Background(), valid, nil, nil, errors.CodeInternal},
		{"wrong response type", context.Background(), valid, "not-a-project", nil, errors.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(stubTransport(tt.resp, tt.respErr)).Create(tt.ctx, tt.req)
			if errors.Code(err) != tt.wantCode {
				t.Errorf("Create() error = %v, want code %v", err, tt.wantCode)
			}
		})
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
	var nilCtx context.Context
	_, err := c.Get(nilCtx, "t", "p")
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
	var nilCtx context.Context
	_, err := c.Update(nilCtx, &UpdateProjectRequest{TenantID: "t", ProjectID: "p"})
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
	var nilCtx context.Context
	err := c.Delete(nilCtx, "t", "p")
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
	resp, err := c.List(context.Background(), &ListProjectsRequest{TenantID: "tenant-1"})
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

// TestClient_List_CarriesPageParams proves the request's paging, sorting and
// search reach the transport, which writes them onto the wire.
func TestClient_List_CarriesPageParams(t *testing.T) {
	want := page.Params{Limit: 10, Offset: 20, Sort: "name:desc", Query: "web"}
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
			r := req.(*ListProjectsRequest)
			if r.Params != want {
				t.Errorf("params = %+v, want %+v", r.Params, want)
			}
			return &ListProjectsResponse{}, nil
		},
	}
	c := NewClient(mt)
	if _, err := c.List(context.Background(), &ListProjectsRequest{TenantID: "t", Params: want}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_List_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.List(context.Background(), nil)
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_List_EmptyTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.List(context.Background(), &ListProjectsRequest{})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_List_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	var nilCtx context.Context
	_, err := c.List(nilCtx, &ListProjectsRequest{TenantID: "t"})
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
	resp, err := c.List(context.Background(), &ListProjectsRequest{TenantID: "t"})
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
	_, err := c.List(context.Background(), &ListProjectsRequest{TenantID: "t"})
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
	_, err := c.List(context.Background(), &ListProjectsRequest{TenantID: "t"})
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
			if tt.wantErr && !errors.IsInvalidInput(err) {
				t.Errorf("validateCreateRequest() error = %v, want InvalidInput", err)
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
// GrantFleetNodeRole (Gap 4)
// =============================================================================

func TestClient_GrantAgentRole_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			if method != "GrantFleetNodeRole" {
				t.Errorf("expected method 'GrantFleetNodeRole', got %q", method)
			}
			r, ok := req.(*GrantFleetNodeRoleRequest)
			if !ok {
				t.Errorf("expected *GrantFleetNodeRoleRequest, got %T", req)
			}
			if r.FleetNodeID != "agent-1" || r.Role != ProjectRoleWorker {
				t.Errorf("unexpected request: %+v", r)
			}
			return &GrantFleetNodeRoleResponse{
				ProjectID:   r.ProjectID,
				FleetNodeID: r.FleetNodeID,
				Role:        r.Role,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.GrantFleetNodeRole(context.Background(), &GrantFleetNodeRoleRequest{
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		FleetNodeID: "agent-1",
		Role:        ProjectRoleWorker,
	})
	if err != nil {
		t.Fatalf("GrantFleetNodeRole: %v", err)
	}
	if resp.FleetNodeID != "agent-1" || resp.Role != ProjectRoleWorker {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClient_GrantAgentRole_Errors(t *testing.T) {
	valid := func(mutate func(*GrantFleetNodeRoleRequest)) *GrantFleetNodeRoleRequest {
		r := &GrantFleetNodeRoleRequest{TenantID: "t", ProjectID: "p", FleetNodeID: "a", Role: ProjectRoleWorker}
		if mutate != nil {
			mutate(r)
		}
		return r
	}
	tests := []struct {
		name     string
		ctx      context.Context
		req      *GrantFleetNodeRoleRequest
		resp     interface{}
		respErr  error
		wantCode errors.ErrorCode
	}{
		{"nil context", nil, valid(nil), nil, nil, errors.CodeInvalidInput},
		{"nil request", context.Background(), nil, nil, nil, errors.CodeInvalidInput},
		{"missing tenant ID", context.Background(), valid(func(r *GrantFleetNodeRoleRequest) { r.TenantID = "" }), nil, nil, errors.CodeInvalidInput},
		{"missing project ID", context.Background(), valid(func(r *GrantFleetNodeRoleRequest) { r.ProjectID = "" }), nil, nil, errors.CodeInvalidInput},
		{"missing agent ID", context.Background(), valid(func(r *GrantFleetNodeRoleRequest) { r.FleetNodeID = "" }), nil, nil, errors.CodeInvalidInput},
		{"missing role", context.Background(), valid(func(r *GrantFleetNodeRoleRequest) { r.Role = "" }), nil, nil, errors.CodeInvalidInput},
		{"transport error", context.Background(), valid(nil), nil, errors.NewUnavailable("test", "grant failed"), errors.CodeUnavailable},
		{"nil response", context.Background(), valid(nil), nil, nil, errors.CodeInternal},
		{"wrong response type", context.Background(), valid(nil), "nope", nil, errors.CodeInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClient(stubTransport(tt.resp, tt.respErr)).GrantFleetNodeRole(tt.ctx, tt.req)
			if errors.Code(err) != tt.wantCode {
				t.Errorf("GrantFleetNodeRole() error = %v, want code %v", err, tt.wantCode)
			}
		})
	}
}

// =============================================================================
// RevokeFleetNodeRole (Gap 4)
// =============================================================================

func TestClient_RevokeAgentRole_HappyPath(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			if method != "RevokeFleetNodeRole" {
				t.Errorf("expected method 'RevokeFleetNodeRole', got %q", method)
			}
			r, ok := req.(*RevokeFleetNodeRoleRequest)
			if !ok {
				t.Errorf("expected *RevokeFleetNodeRoleRequest, got %T", req)
			}
			return &RevokeFleetNodeRoleResponse{
				ProjectID:   r.ProjectID,
				FleetNodeID: r.FleetNodeID,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.RevokeFleetNodeRole(context.Background(), &RevokeFleetNodeRoleRequest{
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		FleetNodeID: "agent-1",
	})
	if err != nil {
		t.Fatalf("RevokeFleetNodeRole: %v", err)
	}
	if resp.FleetNodeID != "agent-1" {
		t.Errorf("expected AgentID='agent-1', got %q", resp.FleetNodeID)
	}
}

func TestClient_RevokeAgentRole_MissingProjectID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.RevokeFleetNodeRole(context.Background(), &RevokeFleetNodeRoleRequest{
		TenantID:    "tenant-1",
		FleetNodeID: "agent-1",
	})
	if err == nil {
		t.Fatal("expected error for missing ProjectID, got nil")
	}
}

func TestClient_RevokeAgentRole_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("project.RevokeFleetNodeRole", "revoke failed")
		},
	}
	c := NewClient(mt)
	_, err := c.RevokeFleetNodeRole(context.Background(), &RevokeFleetNodeRoleRequest{
		TenantID:    "tenant-1",
		ProjectID:   "proj-1",
		FleetNodeID: "agent-1",
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
				Workspace:        ProjectWorkspace{FleetNodeID: "agent-1", Path: "/ws"},
				HostAffinity:     "host-a",
				DefaultLeadAgent: agents.AgentRef{AgentID: "lead-1"},
				Git: ProjectGit{
					RepoURL:       "https://github.com/example/repo",
					DefaultBranch: "main",
					CurrentSHA:    "abc123",
				},
				Permissions: ProjectPermissions{
					FleetNodeRoles: map[string]ProjectRole{"agent-1": ProjectRoleLead},
				},
				Spend: spend.Limits{Windows: []spend.Window{{Name: "daily", Size: time.Hour, MaxTokens: 1000000}}},
				Quotas: ProjectQuotas{
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
	if info.Workspace.FleetNodeID != "agent-1" {
		t.Errorf("expected Workspace.AgentID='agent-1', got %q", info.Workspace.FleetNodeID)
	}
	if info.Git.RepoURL != "https://github.com/example/repo" {
		t.Errorf("unexpected Git.RepoURL: %q", info.Git.RepoURL)
	}
	if len(info.Spend.Windows) != 1 || info.Spend.Windows[0].MaxTokens != 1000000 {
		t.Errorf("expected the spend window to survive, got %+v", info.Spend)
	}
	if info.Permissions.FleetNodeRoles["agent-1"] != ProjectRoleLead {
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
	var nilCtx context.Context
	if _, err := c.GetByName(nilCtx, "t", "name"); err == nil || errors.Code(err) != errors.CodeInvalidInput {
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
// Additional branch coverage: GrantFleetNodeRole
// =============================================================================

// =============================================================================
// Additional branch coverage: RevokeFleetNodeRole
// =============================================================================

func TestClient_RevokeAgentRole_NilContext(t *testing.T) {
	c := NewClient(&mockTransport{})
	var nilCtx context.Context
	_, err := c.RevokeFleetNodeRole(nilCtx, &RevokeFleetNodeRoleRequest{
		TenantID: "t", ProjectID: "p", FleetNodeID: "a",
	})
	if err == nil || errors.Code(err) != errors.CodeInvalidInput {
		t.Errorf("expected CodeInvalidInput, got %v", err)
	}
}

func TestClient_RevokeAgentRole_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	if _, err := c.RevokeFleetNodeRole(context.Background(), nil); err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_RevokeAgentRole_MissingTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.RevokeFleetNodeRole(context.Background(), &RevokeFleetNodeRoleRequest{
		ProjectID: "p", FleetNodeID: "a",
	})
	if err == nil || !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

func TestClient_RevokeAgentRole_MissingAgentID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.RevokeFleetNodeRole(context.Background(), &RevokeFleetNodeRoleRequest{
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
	_, err := c.RevokeFleetNodeRole(context.Background(), &RevokeFleetNodeRoleRequest{
		TenantID: "t", ProjectID: "p", FleetNodeID: "a",
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
	_, err := c.RevokeFleetNodeRole(context.Background(), &RevokeFleetNodeRoleRequest{
		TenantID: "t", ProjectID: "p", FleetNodeID: "a",
	})
	if err == nil || errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", err)
	}
}
