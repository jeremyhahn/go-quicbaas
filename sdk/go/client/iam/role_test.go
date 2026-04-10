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

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestNewRoleClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewRoleClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestRoleClient_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != roleServiceName {
					t.Errorf("expected service %s, got %s", roleServiceName, service)
				}
				if method != "Create" {
					t.Errorf("expected method Create, got %s", method)
				}
				createReq, ok := req.(*createRoleRequestInternal)
				if !ok {
					t.Error("expected createRoleRequestInternal")
				}
				if createReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", createReq.TenantID)
				}
				return &Role{
					ID:          "role-123",
					TenantID:    createReq.TenantID,
					Name:        createReq.Request.Name,
					Description: createReq.Request.Description,
					Permissions: createReq.Request.Permissions,
					IsSystem:    false,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
		}

		client := NewRoleClient(mt)
		role, err := client.Create(context.Background(), "tenant-123", &CreateRoleRequest{
			Name:        "admin",
			Description: "Administrator role",
			Permissions: []string{"*"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if role.ID != "role-123" {
			t.Errorf("expected ID role-123, got %s", role.ID)
		}
		if role.Name != "admin" {
			t.Errorf("expected name admin, got %s", role.Name)
		}
		if len(role.Permissions) != 1 || role.Permissions[0] != "*" {
			t.Errorf("expected permissions [*], got %v", role.Permissions)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Create(nil, "tenant-123", &CreateRoleRequest{
			Name:        "test",
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Create(context.Background(), "", &CreateRoleRequest{
			Name:        "test",
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", &CreateRoleRequest{
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for empty name")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty permissions", func(t *testing.T) {
		// Permissions are optional - roles can be created without permissions
		// and permissions can be added later via update.
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &Role{
					ID:       "role-123",
					TenantID: "tenant-123",
					Name:     "test",
				}, nil
			},
		}
		client := NewRoleClient(mt)
		role, err := client.Create(context.Background(), "tenant-123", &CreateRoleRequest{
			Name: "test",
		})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if role == nil {
			t.Error("expected role to be returned")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewAlreadyExists("test", "role already exists")
			},
		}

		client := NewRoleClient(mt)
		_, err := client.Create(context.Background(), "tenant-123", &CreateRoleRequest{
			Name:        "admin",
			Permissions: []string{"*"},
		})
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsAlreadyExists(err) {
			t.Errorf("expected already exists error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewRoleClient(mt)
		_, err := client.Create(context.Background(), "tenant-123", &CreateRoleRequest{
			Name:        "test",
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})
}

func TestRoleClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				getReq, ok := req.(*getRoleRequest)
				if !ok {
					t.Error("expected getRoleRequest")
				}
				if getReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", getReq.TenantID)
				}
				if getReq.RoleID != "role-123" {
					t.Errorf("expected role ID role-123, got %s", getReq.RoleID)
				}
				return &Role{
					ID:          "role-123",
					TenantID:    "tenant-123",
					Name:        "admin",
					Permissions: []string{"*"},
				}, nil
			},
		}

		client := NewRoleClient(mt)
		role, err := client.Get(context.Background(), "tenant-123", "role-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if role.ID != "role-123" {
			t.Errorf("expected ID role-123, got %s", role.ID)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Get(nil, "tenant-123", "role-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Get(context.Background(), "", "role-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty role ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Get(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty role ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewRoleClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "nonexistent")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestRoleClient_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Update" {
					t.Errorf("expected method Update, got %s", method)
				}
				updateReq, ok := req.(*updateRoleRequestInternal)
				if !ok {
					t.Error("expected updateRoleRequestInternal")
				}
				return &Role{
					ID:          updateReq.RoleID,
					TenantID:    updateReq.TenantID,
					Name:        "admin",
					Description: updateReq.Request.Description,
					Permissions: updateReq.Request.Permissions,
				}, nil
			},
		}

		client := NewRoleClient(mt)
		role, err := client.Update(context.Background(), "tenant-123", "role-123", &UpdateRoleRequest{
			Description: "Updated description",
			Permissions: []string{"read", "write"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if role.Description != "Updated description" {
			t.Errorf("expected description Updated description, got %s", role.Description)
		}
		if len(role.Permissions) != 2 {
			t.Errorf("expected 2 permissions, got %d", len(role.Permissions))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Update(nil, "tenant-123", "role-123", &UpdateRoleRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Update(context.Background(), "", "role-123", &UpdateRoleRequest{})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty role ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "", &UpdateRoleRequest{})
		if err == nil {
			t.Error("expected error for empty role ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "role-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewRoleClient(mt)
		_, err := client.Update(context.Background(), "tenant-123", "role-123", &UpdateRoleRequest{
			Description: "test",
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})
}

func TestRoleClient_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Delete" {
					t.Errorf("expected method Delete, got %s", method)
				}
				deleteReq, ok := req.(*deleteRoleRequest)
				if !ok {
					t.Error("expected deleteRoleRequest")
				}
				if deleteReq.RoleID != "role-123" {
					t.Errorf("expected role ID role-123, got %s", deleteReq.RoleID)
				}
				return nil, nil
			},
		}

		client := NewRoleClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "role-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		err := client.Delete(nil, "tenant-123", "role-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		err := client.Delete(context.Background(), "", "role-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty role ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		err := client.Delete(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty role ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewForbidden("test", "cannot delete system role")
			},
		}

		client := NewRoleClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "system-admin")
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsForbidden(err) {
			t.Errorf("expected forbidden error, got %v", err)
		}
	})
}

func TestRoleClient_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "List" {
					t.Errorf("expected method List, got %s", method)
				}
				listReq, ok := req.(*listRolesRequestInternal)
				if !ok {
					t.Error("expected listRolesRequestInternal")
				}
				if listReq.Request.PageSize != 10 {
					t.Errorf("expected page size 10, got %d", listReq.Request.PageSize)
				}
				return &ListRolesResponse{
					Roles: []Role{
						{ID: "role-1", Name: "admin", Permissions: []string{"*"}},
						{ID: "role-2", Name: "reader", Permissions: []string{"read"}},
					},
					Pagination: PaginationInfo{
						TotalCount: 2,
						PageSize:   10,
					},
				}, nil
			},
		}

		client := NewRoleClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", &ListRolesRequest{PageSize: 10})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(resp.Roles))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.List(nil, "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewRoleClient(&mockTransport{})
		_, err := client.List(context.Background(), "", nil)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request defaults", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &ListRolesResponse{Roles: []Role{}}, nil
			},
		}

		client := NewRoleClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Error("expected non-nil response")
		}
	})

	t.Run("include system roles", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				listReq, ok := req.(*listRolesRequestInternal)
				if !ok {
					t.Error("expected listRolesRequestInternal")
				}
				if !listReq.Request.IncludeSystem {
					t.Error("expected IncludeSystem to be true")
				}
				return &ListRolesResponse{
					Roles: []Role{
						{ID: "role-1", Name: "system-admin", IsSystem: true},
					},
				}, nil
			},
		}

		client := NewRoleClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", &ListRolesRequest{
			IncludeSystem: true,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Roles) != 1 {
			t.Errorf("expected 1 role, got %d", len(resp.Roles))
		}
		if !resp.Roles[0].IsSystem {
			t.Error("expected role to be system role")
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewRoleClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Roles) != 0 {
			t.Errorf("expected 0 roles, got %d", len(resp.Roles))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewRoleClient(mt)
		_, err := client.List(context.Background(), "tenant-123", nil)
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}
