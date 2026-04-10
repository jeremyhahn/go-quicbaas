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

func TestNewAdminClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewAdminClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestAdminClient_CreateAssignment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != adminServiceName {
					t.Errorf("expected service %s, got %s", adminServiceName, service)
				}
				if method != "CreateAssignment" {
					t.Errorf("expected method CreateAssignment, got %s", method)
				}
				createReq, ok := req.(*createAdminAssignmentRequest)
				if !ok {
					t.Error("expected createAdminAssignmentRequest")
				}
				if createReq.AdminID != "admin-123" {
					t.Errorf("expected admin ID admin-123, got %s", createReq.AdminID)
				}
				return &AdminAssignment{
					ID:          "assignment-123",
					AdminID:     createReq.AdminID,
					Scope:       createReq.Scope,
					Permissions: createReq.Permissions,
					Status:      AdminAssignmentStatusActive,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
		}

		client := NewAdminClient(mt)
		assignment, err := client.CreateAssignment(context.Background(), &CreateAdminAssignmentRequest{
			AdminID:     "admin-123",
			Scope:       AdminScopeGlobal,
			Permissions: []string{"users.read", "users.write"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if assignment.ID != "assignment-123" {
			t.Errorf("expected assignment ID assignment-123, got %s", assignment.ID)
		}
		if assignment.Status != AdminAssignmentStatusActive {
			t.Errorf("expected status active, got %s", assignment.Status)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CreateAssignment(nil, &CreateAdminAssignmentRequest{
			AdminID:     "admin-123",
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CreateAssignment(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty admin ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CreateAssignment(context.Background(), &CreateAdminAssignmentRequest{
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for empty admin ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("no permissions or role", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CreateAssignment(context.Background(), &CreateAdminAssignmentRequest{
			AdminID: "admin-123",
		})
		if err == nil {
			t.Error("expected error for missing permissions and role")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("non-global scope requires scope ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CreateAssignment(context.Background(), &CreateAdminAssignmentRequest{
			AdminID:     "admin-123",
			Scope:       AdminScopeTenant,
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for missing scope ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewUnavailable("test", "service unavailable")
			},
		}

		client := NewAdminClient(mt)
		_, err := client.CreateAssignment(context.Background(), &CreateAdminAssignmentRequest{
			AdminID:     "admin-123",
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsUnavailable(err) {
			t.Errorf("expected unavailable error, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.CreateAssignment(context.Background(), &CreateAdminAssignmentRequest{
			AdminID:     "admin-123",
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.CreateAssignment(context.Background(), &CreateAdminAssignmentRequest{
			AdminID:     "admin-123",
			Permissions: []string{"read"},
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_GetAssignment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "GetAssignment" {
					t.Errorf("expected method GetAssignment, got %s", method)
				}
				getReq, ok := req.(*getAdminAssignmentRequest)
				if !ok {
					t.Error("expected getAdminAssignmentRequest")
				}
				if getReq.AssignmentID != "assignment-123" {
					t.Errorf("expected assignment ID assignment-123, got %s", getReq.AssignmentID)
				}
				return &AdminAssignment{
					ID:      "assignment-123",
					AdminID: "admin-123",
					Status:  AdminAssignmentStatusActive,
				}, nil
			},
		}

		client := NewAdminClient(mt)
		assignment, err := client.GetAssignment(context.Background(), "assignment-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if assignment.ID != "assignment-123" {
			t.Errorf("expected assignment ID assignment-123, got %s", assignment.ID)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.GetAssignment(nil, "assignment-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty assignment ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.GetAssignment(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty assignment ID")
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

		client := NewAdminClient(mt)
		_, err := client.GetAssignment(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.GetAssignment(context.Background(), "assignment-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_UpdateAssignment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "UpdateAssignment" {
					t.Errorf("expected method UpdateAssignment, got %s", method)
				}
				updateReq, ok := req.(*updateAdminAssignmentRequest)
				if !ok {
					t.Error("expected updateAdminAssignmentRequest")
				}
				if updateReq.AssignmentID != "assignment-123" {
					t.Errorf("expected assignment ID assignment-123, got %s", updateReq.AssignmentID)
				}
				return &AdminAssignment{
					ID:          "assignment-123",
					Permissions: updateReq.Update.Permissions,
				}, nil
			},
		}

		client := NewAdminClient(mt)
		assignment, err := client.UpdateAssignment(context.Background(), "assignment-123", &UpdateAdminAssignmentRequest{
			Permissions: []string{"admin.full"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(assignment.Permissions) != 1 {
			t.Errorf("expected 1 permission, got %d", len(assignment.Permissions))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.UpdateAssignment(nil, "assignment-123", &UpdateAdminAssignmentRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty assignment ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.UpdateAssignment(context.Background(), "", &UpdateAdminAssignmentRequest{})
		if err == nil {
			t.Error("expected error for empty assignment ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.UpdateAssignment(context.Background(), "assignment-123", nil)
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

		client := NewAdminClient(mt)
		_, err := client.UpdateAssignment(context.Background(), "assignment-123", &UpdateAdminAssignmentRequest{})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.UpdateAssignment(context.Background(), "assignment-123", &UpdateAdminAssignmentRequest{})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_DeleteAssignment(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "DeleteAssignment" {
					t.Errorf("expected method DeleteAssignment, got %s", method)
				}
				deleteReq, ok := req.(*deleteAdminAssignmentRequest)
				if !ok {
					t.Error("expected deleteAdminAssignmentRequest")
				}
				if deleteReq.AssignmentID != "assignment-123" {
					t.Errorf("expected assignment ID assignment-123, got %s", deleteReq.AssignmentID)
				}
				return nil, nil
			},
		}

		client := NewAdminClient(mt)
		err := client.DeleteAssignment(context.Background(), "assignment-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &deleteAdminAssignmentResponse{}, nil
			},
		}

		client := NewAdminClient(mt)
		err := client.DeleteAssignment(context.Background(), "assignment-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		err := client.DeleteAssignment(nil, "assignment-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty assignment ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		err := client.DeleteAssignment(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty assignment ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("test", "assignment not found")
			},
		}

		client := NewAdminClient(mt)
		err := client.DeleteAssignment(context.Background(), "assignment-123")
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		err := client.DeleteAssignment(context.Background(), "assignment-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_ListAssignments(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "ListAssignments" {
					t.Errorf("expected method ListAssignments, got %s", method)
				}
				return &ListAdminAssignmentsResponse{
					Assignments: []AdminAssignment{
						{ID: "assignment-1", AdminID: "admin-1"},
						{ID: "assignment-2", AdminID: "admin-2"},
					},
					TotalCount: 2,
					HasMore:    false,
				}, nil
			},
		}

		client := NewAdminClient(mt)
		resp, err := client.ListAssignments(context.Background(), &ListAdminAssignmentsRequest{
			AdminID: "admin-1",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Assignments) != 2 {
			t.Errorf("expected 2 assignments, got %d", len(resp.Assignments))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.ListAssignments(nil, &ListAdminAssignmentsRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request uses defaults", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				listReq := req.(*listAdminAssignmentsRequest)
				if listReq.Filter.Limit != 100 {
					t.Errorf("expected default limit 100, got %d", listReq.Filter.Limit)
				}
				return &ListAdminAssignmentsResponse{Assignments: []AdminAssignment{}}, nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.ListAssignments(context.Background(), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("limit capped at 1000", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				listReq := req.(*listAdminAssignmentsRequest)
				if listReq.Filter.Limit != 1000 {
					t.Errorf("expected max limit 1000, got %d", listReq.Filter.Limit)
				}
				return &ListAdminAssignmentsResponse{Assignments: []AdminAssignment{}}, nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.ListAssignments(context.Background(), &ListAdminAssignmentsRequest{
			Limit: 5000,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAdminClient(mt)
		resp, err := client.ListAssignments(context.Background(), &ListAdminAssignmentsRequest{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Assignments) != 0 {
			t.Errorf("expected 0 assignments, got %d", len(resp.Assignments))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.ListAssignments(context.Background(), &ListAdminAssignmentsRequest{})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_CheckPermission(t *testing.T) {
	t.Run("success allowed", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "CheckPermission" {
					t.Errorf("expected method CheckPermission, got %s", method)
				}
				checkReq, ok := req.(*adminCheckRequest)
				if !ok {
					t.Error("expected adminCheckRequest")
				}
				if checkReq.Check.AdminID != "admin-123" {
					t.Errorf("expected admin ID admin-123, got %s", checkReq.Check.AdminID)
				}
				return &AdminCheckResponse{
					Allowed:      true,
					AssignmentID: "assignment-123",
					Scope:        AdminScopeGlobal,
				}, nil
			},
		}

		client := NewAdminClient(mt)
		resp, err := client.CheckPermission(context.Background(), &AdminCheckRequest{
			AdminID:    "admin-123",
			Permission: "users.write",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Allowed {
			t.Error("expected permission to be allowed")
		}
	})

	t.Run("success denied", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &AdminCheckResponse{
					Allowed:      false,
					DenialReason: "no matching assignment",
				}, nil
			},
		}

		client := NewAdminClient(mt)
		resp, err := client.CheckPermission(context.Background(), &AdminCheckRequest{
			AdminID:    "admin-123",
			Permission: "admin.delete",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Allowed {
			t.Error("expected permission to be denied")
		}
		if resp.DenialReason != "no matching assignment" {
			t.Errorf("expected denial reason, got %s", resp.DenialReason)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CheckPermission(nil, &AdminCheckRequest{
			AdminID:    "admin-123",
			Permission: "read",
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CheckPermission(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty admin ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CheckPermission(context.Background(), &AdminCheckRequest{
			Permission: "read",
		})
		if err == nil {
			t.Error("expected error for empty admin ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("no permission or action", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.CheckPermission(context.Background(), &AdminCheckRequest{
			AdminID: "admin-123",
		})
		if err == nil {
			t.Error("expected error for missing permission and action")
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

		client := NewAdminClient(mt)
		_, err := client.CheckPermission(context.Background(), &AdminCheckRequest{
			AdminID:    "admin-123",
			Permission: "read",
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.CheckPermission(context.Background(), &AdminCheckRequest{
			AdminID:    "admin-123",
			Permission: "read",
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_StartImpersonation(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "StartImpersonation" {
					t.Errorf("expected method StartImpersonation, got %s", method)
				}
				impReq, ok := req.(*startImpersonationRequest)
				if !ok {
					t.Error("expected startImpersonationRequest")
				}
				if impReq.Impersonation.TargetUserID != "user-456" {
					t.Errorf("expected target user ID user-456, got %s", impReq.Impersonation.TargetUserID)
				}
				return &ImpersonationSession{
					ID:           "session-123",
					AdminID:      "admin-123",
					TargetUserID: impReq.Impersonation.TargetUserID,
					Reason:       impReq.Impersonation.Reason,
					StartedAt:    now,
					ExpiresAt:    now.Add(impReq.Impersonation.Duration),
					Active:       true,
				}, nil
			},
		}

		client := NewAdminClient(mt)
		session, err := client.StartImpersonation(context.Background(), &ImpersonationRequest{
			TargetUserID: "user-456",
			Reason:       "support ticket #123",
			Duration:     30 * time.Minute,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.ID != "session-123" {
			t.Errorf("expected session ID session-123, got %s", session.ID)
		}
		if !session.Active {
			t.Error("expected session to be active")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.StartImpersonation(nil, &ImpersonationRequest{
			TargetUserID: "user-456",
			Reason:       "test",
			Duration:     time.Hour,
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.StartImpersonation(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty target user ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.StartImpersonation(context.Background(), &ImpersonationRequest{
			Reason:   "test",
			Duration: time.Hour,
		})
		if err == nil {
			t.Error("expected error for empty target user ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty reason", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.StartImpersonation(context.Background(), &ImpersonationRequest{
			TargetUserID: "user-456",
			Duration:     time.Hour,
		})
		if err == nil {
			t.Error("expected error for empty reason")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("zero duration", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.StartImpersonation(context.Background(), &ImpersonationRequest{
			TargetUserID: "user-456",
			Reason:       "test",
		})
		if err == nil {
			t.Error("expected error for zero duration")
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

		client := NewAdminClient(mt)
		_, err := client.StartImpersonation(context.Background(), &ImpersonationRequest{
			TargetUserID: "user-456",
			Reason:       "test",
			Duration:     time.Hour,
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.StartImpersonation(context.Background(), &ImpersonationRequest{
			TargetUserID: "user-456",
			Reason:       "test",
			Duration:     time.Hour,
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_EndImpersonation(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "EndImpersonation" {
					t.Errorf("expected method EndImpersonation, got %s", method)
				}
				endReq, ok := req.(*endImpersonationRequest)
				if !ok {
					t.Error("expected endImpersonationRequest")
				}
				if endReq.SessionID != "session-123" {
					t.Errorf("expected session ID session-123, got %s", endReq.SessionID)
				}
				return nil, nil
			},
		}

		client := NewAdminClient(mt)
		err := client.EndImpersonation(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("success with valid response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &endImpersonationResponse{}, nil
			},
		}

		client := NewAdminClient(mt)
		err := client.EndImpersonation(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		err := client.EndImpersonation(nil, "session-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty session ID", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		err := client.EndImpersonation(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		err := client.EndImpersonation(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminClient_ListActiveImpersonations(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "ListActiveImpersonations" {
					t.Errorf("expected method ListActiveImpersonations, got %s", method)
				}
				return &ListImpersonationsResponse{
					Sessions: []ImpersonationSession{
						{ID: "session-1", Active: true},
						{ID: "session-2", Active: true},
					},
					TotalCount: 2,
				}, nil
			},
		}

		client := NewAdminClient(mt)
		resp, err := client.ListActiveImpersonations(context.Background(), "test-admin")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Sessions) != 2 {
			t.Errorf("expected 2 sessions, got %d", len(resp.Sessions))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewAdminClient(&mockTransport{})
		_, err := client.ListActiveImpersonations(nil, "test-admin")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty response", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}

		client := NewAdminClient(mt)
		resp, err := client.ListActiveImpersonations(context.Background(), "test-admin")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Sessions) != 0 {
			t.Errorf("expected 0 sessions, got %d", len(resp.Sessions))
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}

		client := NewAdminClient(mt)
		_, err := client.ListActiveImpersonations(context.Background(), "test-admin")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

func TestAdminScope_String(t *testing.T) {
	tests := []struct {
		scope    AdminScope
		expected string
	}{
		{AdminScopeGlobal, "global"},
		{AdminScopeTenant, "tenant"},
		{AdminScopeOrganization, "organization"},
		{AdminScopeGroup, "group"},
		{AdminScopeProject, "project"},
		{AdminScopeResource, "resource"},
		{AdminScope(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.scope.String(); got != tt.expected {
				t.Errorf("AdminScope.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAdminAssignmentStatus_String(t *testing.T) {
	tests := []struct {
		status   AdminAssignmentStatus
		expected string
	}{
		{AdminAssignmentStatusActive, "active"},
		{AdminAssignmentStatusPending, "pending"},
		{AdminAssignmentStatusSuspended, "suspended"},
		{AdminAssignmentStatusExpired, "expired"},
		{AdminAssignmentStatusRevoked, "revoked"},
		{AdminAssignmentStatus(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("AdminAssignmentStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
