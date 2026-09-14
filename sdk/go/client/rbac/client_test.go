// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package rbac

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

// =============================================================================
// NewClient Tests
// =============================================================================

func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

// The RBAC client has no user verbs; see the Client doc comment and issue #905.

// =============================================================================
// CreateRole Tests
// =============================================================================

func TestClient_CreateRole_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "CreateRole" {
				t.Errorf("expected method %q, got %q", "CreateRole", method)
			}
			createReq, ok := req.(*CreateRoleRequest)
			if !ok {
				t.Errorf("expected *CreateRoleRequest, got %T", req)
			}
			if createReq.RoleName != "editor" {
				t.Errorf("expected role name %q, got %q", "editor", createReq.RoleName)
			}
			return &CreateRoleResponse{
				RoleID:    "role-123",
				Timestamp: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.CreateRole(ctx, &CreateRoleRequest{
		RoleName:    "editor",
		Permissions: []string{"kv:read", "kv:write"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RoleID != "role-123" {
		t.Errorf("expected RoleID %q, got %q", "role-123", resp.RoleID)
	}
}

func TestClient_CreateRole_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &CreateRoleResponse{RoleID: "role-123", Timestamp: 1234567890}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *CreateRoleRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty role name",
			req:     &CreateRoleRequest{RoleName: "", Permissions: []string{"kv:read"}},
			wantErr: true,
		},
		{
			name:    "valid request without permissions",
			req:     &CreateRoleRequest{RoleName: "empty-role"},
			wantErr: false,
		},
		{
			name:    "valid request with permissions",
			req:     &CreateRoleRequest{RoleName: "editor", Permissions: []string{"kv:read", "kv:write"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CreateRole(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_CreateRole_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewAlreadyExists("rbac.CreateRole", "role already exists")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CreateRole(ctx, &CreateRoleRequest{RoleName: "admin"})
	if err == nil {
		t.Error("expected error from transport")
	}
	if !errors.IsAlreadyExists(err) {
		t.Errorf("expected AlreadyExists error, got %v", err)
	}
}

func TestClient_CreateRole_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CreateRole(ctx, &CreateRoleRequest{RoleName: "editor"})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_CreateRole_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid type", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CreateRole(ctx, &CreateRoleRequest{RoleName: "editor"})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

// =============================================================================
// DeleteRole Tests
// =============================================================================

func TestClient_DeleteRole_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "DeleteRole" {
				t.Errorf("expected method %q, got %q", "DeleteRole", method)
			}
			return &DeleteRoleResponse{
				Success: true,
				Message: "role deleted",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.DeleteRole(ctx, &DeleteRoleRequest{RoleName: "editor"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected Success to be true")
	}
}

func TestClient_DeleteRole_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DeleteRoleResponse{Success: true}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *DeleteRoleRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty role name",
			req:     &DeleteRoleRequest{RoleName: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DeleteRoleRequest{RoleName: "editor"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.DeleteRole(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_DeleteRole_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewNotFound("rbac.DeleteRole", "role not found")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.DeleteRole(ctx, &DeleteRoleRequest{RoleName: "unknown"})
	if err == nil {
		t.Error("expected error from transport")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_DeleteRole_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.DeleteRole(ctx, &DeleteRoleRequest{RoleName: "editor"})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_DeleteRole_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid type", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.DeleteRole(ctx, &DeleteRoleRequest{RoleName: "editor"})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

// =============================================================================
// ListRoles Tests
// =============================================================================

func TestClient_ListRoles_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "ListRoles" {
				t.Errorf("expected method %q, got %q", "ListRoles", method)
			}
			return &ListRolesResponse{
				Roles: []*RoleInfo{
					{RoleID: "role-1", RoleName: "admin", Permissions: []string{"*"}},
					{RoleID: "role-2", RoleName: "reader", Permissions: []string{"kv:read"}},
				},
				TotalCount: 2,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListRoles(ctx, &ListRolesRequest{Limit: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(resp.Roles))
	}
	if resp.TotalCount != 2 {
		t.Errorf("expected TotalCount 2, got %d", resp.TotalCount)
	}
}

func TestClient_ListRoles_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListRolesResponse{Roles: []*RoleInfo{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ListRolesRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid request with defaults",
			req:     &ListRolesRequest{},
			wantErr: false,
		},
		{
			name:    "valid request with pagination",
			req:     &ListRolesRequest{Limit: 100, Offset: 50},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ListRoles(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListRoles() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_ListRoles_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListRoles(ctx, &ListRolesRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Roles == nil {
		t.Error("expected non-nil Roles slice")
	}
	if len(resp.Roles) != 0 {
		t.Errorf("expected empty Roles slice, got %d", len(resp.Roles))
	}
}

func TestClient_ListRoles_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid type", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.ListRoles(ctx, &ListRolesRequest{})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

// =============================================================================
// AssignRole Tests
// =============================================================================

func TestClient_AssignRole_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "AssignRole" {
				t.Errorf("expected method %q, got %q", "AssignRole", method)
			}
			assignReq, ok := req.(*AssignRoleRequest)
			if !ok {
				t.Errorf("expected *AssignRoleRequest, got %T", req)
			}
			if assignReq.Username != "alice" {
				t.Errorf("expected username %q, got %q", "alice", assignReq.Username)
			}
			if assignReq.RoleName != "admin" {
				t.Errorf("expected role name %q, got %q", "admin", assignReq.RoleName)
			}
			return &AssignRoleResponse{
				Success: true,
				Message: "role assigned",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.AssignRole(ctx, &AssignRoleRequest{
		Username: "alice",
		RoleName: "admin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected Success to be true")
	}
}

func TestClient_AssignRole_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &AssignRoleResponse{Success: true}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *AssignRoleRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty username",
			req:     &AssignRoleRequest{RoleName: "admin"},
			wantErr: true,
		},
		{
			name:    "empty role name",
			req:     &AssignRoleRequest{Username: "alice"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &AssignRoleRequest{Username: "alice", RoleName: "admin"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.AssignRole(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("AssignRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_AssignRole_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewNotFound("rbac.AssignRole", "user or role not found")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.AssignRole(ctx, &AssignRoleRequest{
		Username: "unknown",
		RoleName: "admin",
	})
	if err == nil {
		t.Error("expected error from transport")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_AssignRole_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.AssignRole(ctx, &AssignRoleRequest{
		Username: "alice",
		RoleName: "admin",
	})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_AssignRole_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid type", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.AssignRole(ctx, &AssignRoleRequest{
		Username: "alice",
		RoleName: "admin",
	})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}

// =============================================================================
// RevokeRole Tests
// =============================================================================

func TestClient_RevokeRole_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "RevokeRole" {
				t.Errorf("expected method %q, got %q", "RevokeRole", method)
			}
			revokeReq, ok := req.(*RevokeRoleRequest)
			if !ok {
				t.Errorf("expected *RevokeRoleRequest, got %T", req)
			}
			if revokeReq.Username != "alice" {
				t.Errorf("expected username %q, got %q", "alice", revokeReq.Username)
			}
			if revokeReq.RoleName != "admin" {
				t.Errorf("expected role name %q, got %q", "admin", revokeReq.RoleName)
			}
			return &RevokeRoleResponse{
				Success: true,
				Message: "role revoked",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.RevokeRole(ctx, &RevokeRoleRequest{
		Username: "alice",
		RoleName: "admin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected Success to be true")
	}
}

func TestClient_RevokeRole_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RevokeRoleResponse{Success: true}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RevokeRoleRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty username",
			req:     &RevokeRoleRequest{RoleName: "admin"},
			wantErr: true,
		},
		{
			name:    "empty role name",
			req:     &RevokeRoleRequest{Username: "alice"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RevokeRoleRequest{Username: "alice", RoleName: "admin"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RevokeRole(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RevokeRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_RevokeRole_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewNotFound("rbac.RevokeRole", "user or role not found")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.RevokeRole(ctx, &RevokeRoleRequest{
		Username: "unknown",
		RoleName: "admin",
	})
	if err == nil {
		t.Error("expected error from transport")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_RevokeRole_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.RevokeRole(ctx, &RevokeRoleRequest{
		Username: "alice",
		RoleName: "admin",
	})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_RevokeRole_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid type", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.RevokeRole(ctx, &RevokeRoleRequest{
		Username: "alice",
		RoleName: "admin",
	})
	if err == nil {
		t.Error("expected error for invalid response type")
	}
}
