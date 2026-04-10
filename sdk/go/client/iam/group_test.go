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

func TestNewGroupClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewGroupClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestGroupClient_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != groupServiceName {
					t.Errorf("expected service %s, got %s", groupServiceName, service)
				}
				if method != "Create" {
					t.Errorf("expected method Create, got %s", method)
				}
				createReq, ok := req.(*createGroupRequestInternal)
				if !ok {
					t.Error("expected createGroupRequestInternal")
				}
				if createReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", createReq.TenantID)
				}
				return &Group{
					ID:          "group-123",
					TenantID:    createReq.TenantID,
					Name:        createReq.Request.Name,
					Description: createReq.Request.Description,
					MemberCount: 0,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
		}

		client := NewGroupClient(mt)
		group, err := client.Create(context.Background(), "tenant-123", &CreateGroupRequest{
			Name:        "engineering",
			Description: "Engineering team",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if group.ID != "group-123" {
			t.Errorf("expected ID group-123, got %s", group.ID)
		}
		if group.Name != "engineering" {
			t.Errorf("expected name engineering, got %s", group.Name)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Create(nil, "tenant-123", &CreateGroupRequest{Name: "test"})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Create(context.Background(), "", &CreateGroupRequest{Name: "test"})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Create(context.Background(), "tenant-123", &CreateGroupRequest{})
		if err == nil {
			t.Error("expected error for empty name")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

func TestGroupClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				getReq, ok := req.(*getGroupRequest)
				if !ok {
					t.Error("expected getGroupRequest")
				}
				if getReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", getReq.TenantID)
				}
				if getReq.GroupID != "group-123" {
					t.Errorf("expected group ID group-123, got %s", getReq.GroupID)
				}
				return &Group{
					ID:       "group-123",
					TenantID: "tenant-123",
					Name:     "engineering",
				}, nil
			},
		}

		client := NewGroupClient(mt)
		group, err := client.Get(context.Background(), "tenant-123", "group-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if group.ID != "group-123" {
			t.Errorf("expected ID group-123, got %s", group.ID)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Get(nil, "tenant-123", "group-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Get(context.Background(), "", "group-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty group ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Get(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty group ID")
		}
	})

	t.Run("not found", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewGroupClient(mt)
		_, err := client.Get(context.Background(), "tenant-123", "nonexistent")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestGroupClient_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Update" {
					t.Errorf("expected method Update, got %s", method)
				}
				updateReq, ok := req.(*updateGroupRequestInternal)
				if !ok {
					t.Error("expected updateGroupRequestInternal")
				}
				return &Group{
					ID:          updateReq.GroupID,
					TenantID:    updateReq.TenantID,
					Name:        "engineering",
					Description: updateReq.Request.Description,
				}, nil
			},
		}

		client := NewGroupClient(mt)
		group, err := client.Update(context.Background(), "tenant-123", "group-123", &UpdateGroupRequest{
			Description: "Updated description",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if group.Description != "Updated description" {
			t.Errorf("expected description Updated description, got %s", group.Description)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Update(nil, "tenant-123", "group-123", &UpdateGroupRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Update(context.Background(), "", "group-123", &UpdateGroupRequest{})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty group ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "", &UpdateGroupRequest{})
		if err == nil {
			t.Error("expected error for empty group ID")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", "group-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})
}

func TestGroupClient_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Delete" {
					t.Errorf("expected method Delete, got %s", method)
				}
				return nil, nil
			},
		}

		client := NewGroupClient(mt)
		err := client.Delete(context.Background(), "tenant-123", "group-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.Delete(nil, "tenant-123", "group-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.Delete(context.Background(), "", "group-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty group ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.Delete(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty group ID")
		}
	})
}

func TestGroupClient_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "List" {
					t.Errorf("expected method List, got %s", method)
				}
				return &ListGroupsResponse{
					Groups: []Group{
						{ID: "group-1", Name: "engineering"},
						{ID: "group-2", Name: "marketing"},
					},
					Pagination: PaginationInfo{
						TotalCount: 2,
						PageSize:   10,
					},
				}, nil
			},
		}

		client := NewGroupClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", &ListGroupsRequest{PageSize: 10})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Groups) != 2 {
			t.Errorf("expected 2 groups, got %d", len(resp.Groups))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.List(nil, "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.List(context.Background(), "", nil)
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("nil request defaults", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &ListGroupsResponse{Groups: []Group{}}, nil
			},
		}

		client := NewGroupClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Error("expected non-nil response")
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewGroupClient(mt)
		resp, err := client.List(context.Background(), "tenant-123", nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(resp.Groups))
		}
	})
}

func TestGroupClient_AddMember(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "AddMember" {
					t.Errorf("expected method AddMember, got %s", method)
				}
				memberReq, ok := req.(*groupMemberRequest)
				if !ok {
					t.Error("expected groupMemberRequest")
				}
				if memberReq.UserID != "user-123" {
					t.Errorf("expected user ID user-123, got %s", memberReq.UserID)
				}
				return nil, nil
			},
		}

		client := NewGroupClient(mt)
		err := client.AddMember(context.Background(), "tenant-123", "group-123", "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.AddMember(nil, "tenant-123", "group-123", "user-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.AddMember(context.Background(), "", "group-123", "user-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty group ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.AddMember(context.Background(), "tenant-123", "", "user-123")
		if err == nil {
			t.Error("expected error for empty group ID")
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.AddMember(context.Background(), "tenant-123", "group-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
	})
}

func TestGroupClient_RemoveMember(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "RemoveMember" {
					t.Errorf("expected method RemoveMember, got %s", method)
				}
				return nil, nil
			},
		}

		client := NewGroupClient(mt)
		err := client.RemoveMember(context.Background(), "tenant-123", "group-123", "user-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.RemoveMember(nil, "tenant-123", "group-123", "user-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty user ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		err := client.RemoveMember(context.Background(), "tenant-123", "group-123", "")
		if err == nil {
			t.Error("expected error for empty user ID")
		}
	})
}

func TestGroupClient_ListMembers(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "ListMembers" {
					t.Errorf("expected method ListMembers, got %s", method)
				}
				return &ListMembersResponse{
					Members: []GroupMember{
						{UserID: "user-1", Username: "user1", JoinedAt: now},
						{UserID: "user-2", Username: "user2", JoinedAt: now},
					},
					Pagination: PaginationInfo{
						TotalCount: 2,
					},
				}, nil
			},
		}

		client := NewGroupClient(mt)
		resp, err := client.ListMembers(context.Background(), "tenant-123", "group-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Members) != 2 {
			t.Errorf("expected 2 members, got %d", len(resp.Members))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.ListMembers(nil, "tenant-123", "group-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.ListMembers(context.Background(), "", "group-123")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
	})

	t.Run("empty group ID", func(t *testing.T) {
		client := NewGroupClient(&mockTransport{})
		_, err := client.ListMembers(context.Background(), "tenant-123", "")
		if err == nil {
			t.Error("expected error for empty group ID")
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewGroupClient(mt)
		resp, err := client.ListMembers(context.Background(), "tenant-123", "group-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Members) != 0 {
			t.Errorf("expected 0 members, got %d", len(resp.Members))
		}
	})
}
