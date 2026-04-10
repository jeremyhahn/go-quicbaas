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

func TestNewUserClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewUserClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestUserClient_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != userServiceName {
					t.Errorf("expected service %s, got %s", userServiceName, service)
				}
				if method != "Create" {
					t.Errorf("expected method Create, got %s", method)
				}
				createReq, ok := req.(*createUserRequestInternal)
				if !ok {
					t.Error("expected createUserRequestInternal")
				}
				if createReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", createReq.TenantID)
				}
				return &User{
					ID:        "user-123",
					TenantID:  createReq.TenantID,
					Username:  createReq.Request.Username,
					Email:     createReq.Request.Email,
					Status:    UserStatusActive,
					CreatedAt: now,
					UpdatedAt: now,
				}, nil
			},
		}

		client := NewUserClient(mt)
		user, err := client.Create(context.Background(), "tenant-123", &CreateUserRequest{
			Username: "testuser",
			Email:    "test@example.com",
			Password: "password123",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != "user-123" {
			t.Errorf("expected ID user-123, got %s", user.ID)
		}
		if user.Username != "testuser" {
			t.Errorf("expected username testuser, got %s", user.Username)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Create(nil, "tenant-123", &CreateUserRequest{
			Username: "test",
			Email:    "test@example.com",
			Password: "password",
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Create(context.Background(), "", &CreateUserRequest{
			Username: "test",
			Email:    "test@example.com",
			Password: "password",
		})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty username", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", &CreateUserRequest{
			Email:    "test@example.com",
			Password: "password",
		})
		if err == nil {
			t.Error("expected error for empty username")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty email", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", &CreateUserRequest{
			Username: "test",
			Password: "password",
		})
		if err == nil {
			t.Error("expected error for empty email")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty password", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", &CreateUserRequest{
			Username: "test",
			Email:    "test@example.com",
		})
		if err == nil {
			t.Error("expected error for empty password")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

func TestUserClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				getReq, ok := req.(*getUserRequest)
				if !ok {
					t.Error("expected getUserRequest")
				}
				if getReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", getReq.TenantID)
				}
				if getReq.UserID != "user-123" {
					t.Errorf("expected user ID user-123, got %s", getReq.UserID)
				}
				return &User{
					ID:       "user-123",
					TenantID: "tenant-123",
					Username: "testuser",
					Email:    "test@example.com",
					Status:   UserStatusActive,
				}, nil
			},
		}

		client := NewUserClient(mt)
		user, err := client.Get(context.Background(), "tenant-123", "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.ID != "user-123" {
			t.Errorf("expected ID user-123, got %s", user.ID)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Get(nil, "tenant-123", "user-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Get(context.Background(), "", "user-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Get(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
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

		client := NewUserClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "nonexistent")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestUserClient_GetByEmail(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "GetByEmail" {
					t.Errorf("expected method GetByEmail, got %s", method)
				}
				getReq, ok := req.(*getUserByEmailRequest)
				if !ok {
					t.Error("expected getUserByEmailRequest")
				}
				if getReq.Email != "test@example.com" {
					t.Errorf("expected email test@example.com, got %s", getReq.Email)
				}
				return &User{
					ID:       "user-123",
					TenantID: "tenant-123",
					Username: "testuser",
					Email:    "test@example.com",
				}, nil
			},
		}

		client := NewUserClient(mt)
		user, err := client.GetByEmail(context.Background(), "tenant-123", "test@example.com")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Email != "test@example.com" {
			t.Errorf("expected email test@example.com, got %s", user.Email)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.GetByEmail(nil, "tenant-123", "test@example.com")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.GetByEmail(context.Background(), "", "test@example.com")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty email", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.GetByEmail(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty email")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

func TestUserClient_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Update" {
					t.Errorf("expected method Update, got %s", method)
				}
				updateReq, ok := req.(*updateUserRequestInternal)
				if !ok {
					t.Error("expected updateUserRequestInternal")
				}
				return &User{
					ID:          updateReq.UserID,
					TenantID:    updateReq.TenantID,
					Username:    "testuser",
					Email:       updateReq.Request.Email,
					DisplayName: updateReq.Request.DisplayName,
				}, nil
			},
		}

		client := NewUserClient(mt)
		user, err := client.Update(context.Background(), "tenant-123", "user-123", &UpdateUserRequest{
			DisplayName: "Updated Name",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.DisplayName != "Updated Name" {
			t.Errorf("expected display name Updated Name, got %s", user.DisplayName)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Update(nil, "tenant-123", "user-123", &UpdateUserRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Update(context.Background(), "", "user-123", &UpdateUserRequest{})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "", &UpdateUserRequest{})
		if err == nil {
			t.Error("expected error for empty user ID")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "user-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})
}

func TestUserClient_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Delete" {
					t.Errorf("expected method Delete, got %s", method)
				}
				return nil, nil
			},
		}

		client := NewUserClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Delete(nil, "tenant-123", "user-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Delete(context.Background(), "", "user-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Delete(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
	})
}

func TestUserClient_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "List" {
					t.Errorf("expected method List, got %s", method)
				}
				return &ListUsersResponse{
					Users: []User{
						{ID: "user-1", Username: "user1"},
						{ID: "user-2", Username: "user2"},
					},
					Pagination: PaginationInfo{
						TotalCount: 2,
						PageSize:   10,
					},
				}, nil
			},
		}

		client := NewUserClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", &ListUsersRequest{PageSize: 10})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Users) != 2 {
			t.Errorf("expected 2 users, got %d", len(resp.Users))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.List(nil, "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		_, err := client.List(context.Background(), "", nil)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("nil request defaults", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &ListUsersResponse{Users: []User{}}, nil
			},
		}

		client := NewUserClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Error("expected non-nil response")
		}
	})
}

func TestUserClient_AssignRole(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "AssignRole" {
					t.Errorf("expected method AssignRole, got %s", method)
				}
				roleReq, ok := req.(*userRoleRequest)
				if !ok {
					t.Error("expected userRoleRequest")
				}
				if roleReq.RoleID != "role-123" {
					t.Errorf("expected role ID role-123, got %s", roleReq.RoleID)
				}
				return nil, nil
			},
		}

		client := NewUserClient(mt)
		err := client.AssignRole(context.Background(), "tenant-123", "user-123", "role-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.AssignRole(nil, "tenant-123", "user-123", "role-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.AssignRole(context.Background(), "", "user-123", "role-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.AssignRole(context.Background(), "tenant-123", "", "role-123")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
	})

	t.Run("empty role ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.AssignRole(context.Background(), "tenant-123", "user-123", "")
		if err == nil {
			t.Error("expected error for empty role ID")
		}
	})
}

func TestUserClient_RevokeRole(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RevokeRole" {
					t.Errorf("expected method RevokeRole, got %s", method)
				}
				return nil, nil
			},
		}

		client := NewUserClient(mt)
		err := client.RevokeRole(context.Background(), "tenant-123", "user-123", "role-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.RevokeRole(nil, "tenant-123", "user-123", "role-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty role ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.RevokeRole(context.Background(), "tenant-123", "user-123", "")
		if err == nil {
			t.Error("expected error for empty role ID")
		}
	})
}

func TestUserClient_AddToGroup(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "AddToGroup" {
					t.Errorf("expected method AddToGroup, got %s", method)
				}
				groupReq, ok := req.(*userGroupRequest)
				if !ok {
					t.Error("expected userGroupRequest")
				}
				if groupReq.GroupID != "group-123" {
					t.Errorf("expected group ID group-123, got %s", groupReq.GroupID)
				}
				return nil, nil
			},
		}

		client := NewUserClient(mt)
		err := client.AddToGroup(context.Background(), "tenant-123", "user-123", "group-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.AddToGroup(nil, "tenant-123", "user-123", "group-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty group ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.AddToGroup(context.Background(), "tenant-123", "user-123", "")
		if err == nil {
			t.Error("expected error for empty group ID")
		}
	})
}

func TestUserClient_RemoveFromGroup(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RemoveFromGroup" {
					t.Errorf("expected method RemoveFromGroup, got %s", method)
				}
				return nil, nil
			},
		}

		client := NewUserClient(mt)
		err := client.RemoveFromGroup(context.Background(), "tenant-123", "user-123", "group-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.RemoveFromGroup(nil, "tenant-123", "user-123", "group-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty group ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.RemoveFromGroup(context.Background(), "tenant-123", "user-123", "")
		if err == nil {
			t.Error("expected error for empty group ID")
		}
	})
}

func TestUserClient_Suspend(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Suspend" {
					t.Errorf("expected method Suspend, got %s", method)
				}
				return nil, nil
			},
		}

		client := NewUserClient(mt)
		err := client.Suspend(context.Background(), "tenant-123", "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Suspend(nil, "tenant-123", "user-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Suspend(context.Background(), "", "user-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Suspend(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
	})
}

func TestUserClient_Unsuspend(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Unsuspend" {
					t.Errorf("expected method Unsuspend, got %s", method)
				}
				return nil, nil
			},
		}

		client := NewUserClient(mt)
		err := client.Unsuspend(context.Background(), "tenant-123", "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Unsuspend(nil, "tenant-123", "user-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewUserClient(&mockTransport{})
		err := client.Unsuspend(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
	})
}

func TestUserStatus_String(t *testing.T) {
	tests := []struct {
		status   UserStatus
		expected string
	}{
		{UserStatusUnspecified, "UNSPECIFIED"},
		{UserStatusActive, "ACTIVE"},
		{UserStatusSuspended, "SUSPENDED"},
		{UserStatusPendingVerification, "PENDING_VERIFICATION"},
		{UserStatusDeleted, "DELETED"},
		{UserStatus(99), "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("UserStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
