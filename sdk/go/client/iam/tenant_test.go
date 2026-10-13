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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport implements transport.Transport for testing.
type mockTransport struct {
	requestFunc func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	healthy     bool
	closed      bool
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFunc != nil {
		return m.requestFunc(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	return nil, errors.NewInternal("mockTransport", "streaming not supported")
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func (m *mockTransport) Healthy() bool {
	return m.healthy
}

func TestNewTenantClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewTenantClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})
}

func TestTenantClient_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		now := time.Now()
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != tenantServiceName {
					t.Errorf("expected service %s, got %s", tenantServiceName, service)
				}
				if method != "Create" {
					t.Errorf("expected method Create, got %s", method)
				}
				createReq, ok := req.(*CreateTenantRequest)
				if !ok {
					t.Error("expected CreateTenantRequest")
				}
				if createReq.Name != "test-tenant" {
					t.Errorf("expected name test-tenant, got %s", createReq.Name)
				}
				return &Tenant{
					ID:          "tenant-123",
					Name:        createReq.Name,
					DisplayName: createReq.DisplayName,
					Status:      TenantStatusActive,
					CreatedAt:   now,
					UpdatedAt:   now,
				}, nil
			},
		}

		client := NewTenantClient(mt)
		tenant, err := client.Create(context.Background(), &CreateTenantRequest{
			Name:        "test-tenant",
			DisplayName: "Test Tenant",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tenant.ID != "tenant-123" {
			t.Errorf("expected ID tenant-123, got %s", tenant.ID)
		}
		if tenant.Name != "test-tenant" {
			t.Errorf("expected name test-tenant, got %s", tenant.Name)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Create(nil, &CreateTenantRequest{Name: "test"})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Create(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Create(context.Background(), &CreateTenantRequest{})
		if err == nil {
			t.Error("expected error for empty name")
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

		client := NewTenantClient(mt)
		_, err := client.Create(context.Background(), &CreateTenantRequest{Name: "test"})
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

		client := NewTenantClient(mt)
		_, err := client.Create(context.Background(), &CreateTenantRequest{Name: "test"})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})
}

func TestTenantClient_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Get" {
					t.Errorf("expected method Get, got %s", method)
				}
				getReq, ok := req.(*getTenantRequest)
				if !ok {
					t.Error("expected getTenantRequest")
				}
				if getReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", getReq.TenantID)
				}
				return &Tenant{
					ID:     "tenant-123",
					Name:   "test-tenant",
					Status: TenantStatusActive,
				}, nil
			},
		}

		client := NewTenantClient(mt)
		tenant, err := client.Get(context.Background(), "tenant-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tenant.ID != "tenant-123" {
			t.Errorf("expected ID tenant-123, got %s", tenant.ID)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Get(nil, "tenant-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Get(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("iam.tenant.Get", "tenant not found")
			},
		}

		client := NewTenantClient(mt)
		_, err := client.Get(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected not found error")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestTenantClient_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Update" {
					t.Errorf("expected method Update, got %s", method)
				}
				updateReq, ok := req.(*updateTenantRequestInternal)
				if !ok {
					t.Error("expected updateTenantRequestInternal")
				}
				if updateReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", updateReq.TenantID)
				}
				return &Tenant{
					ID:          "tenant-123",
					Name:        "test-tenant",
					DisplayName: updateReq.Request.DisplayName,
					Status:      TenantStatusActive,
				}, nil
			},
		}

		client := NewTenantClient(mt)
		tenant, err := client.Update(context.Background(), "tenant-123", &UpdateTenantRequest{
			DisplayName: "Updated Name",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tenant.DisplayName != "Updated Name" {
			t.Errorf("expected display name Updated Name, got %s", tenant.DisplayName)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Update(nil, "tenant-123", &UpdateTenantRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Update(context.Background(), "", &UpdateTenantRequest{})
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.Update(context.Background(), "tenant-123", nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})
}

func TestTenantClient_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "Delete" {
					t.Errorf("expected method Delete, got %s", method)
				}
				deleteReq, ok := req.(*deleteTenantRequest)
				if !ok {
					t.Error("expected deleteTenantRequest")
				}
				if deleteReq.TenantID != "tenant-123" {
					t.Errorf("expected tenant ID tenant-123, got %s", deleteReq.TenantID)
				}
				return nil, nil
			},
		}

		client := NewTenantClient(mt)
		err := client.Delete(context.Background(), "tenant-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		err := client.Delete(nil, "tenant-123")
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty tenant ID", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		err := client.Delete(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty tenant ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("test", "tenant not found")
			},
		}

		client := NewTenantClient(mt)
		err := client.Delete(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestTenantClient_Provision(t *testing.T) {
	inviteReq := &ProvisionTenantRequest{TenantID: "acme", AdminEnrollment: TenantAdminEnrollInvite}

	t.Run("map response from REST, gRPC or MCP", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != tenantServiceName || method != "Provision" {
					t.Errorf("unexpected route %s/%s", service, method)
				}
				if req != inviteReq {
					t.Error("expected the caller's request to be sent unchanged")
				}
				return map[string]interface{}{
					"tenant_id":         "acme",
					"admin_user_id":     "7",
					"admin_pending":     true,
					"invite_token":      "tok",
					"invite_expires_at": "2026-10-13T00:00:00Z",
				}, nil
			},
		}

		resp, err := NewTenantClient(mt).Provision(context.Background(), inviteReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.AdminUserID != "7" || !resp.AdminPending || resp.InviteToken != "tok" {
			t.Errorf("unexpected response: %+v", resp)
		}
		if resp.InviteExpiresAt == nil || resp.InviteExpiresAt.Year() != 2026 {
			t.Errorf("expected the invite expiry to be decoded, got %v", resp.InviteExpiresAt)
		}
	})

	t.Run("typed response from the embedded transport", func(t *testing.T) {
		want := &ProvisionTenantResponse{TenantID: "acme", AdminCertPEM: "pem"}
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return want, nil
			},
		}

		resp, err := NewTenantClient(mt).Provision(context.Background(), &ProvisionTenantRequest{TenantID: "acme"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != want {
			t.Errorf("expected the typed response to be returned as is, got %+v", resp)
		}
	})

	t.Run("invalid input never reaches the transport", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				t.Error("transport must not be called")
				return nil, nil
			},
		})
		cases := map[string]struct {
			ctx context.Context
			req *ProvisionTenantRequest
		}{
			"nil request":     {ctx: context.Background(), req: nil},
			"empty tenant ID": {ctx: context.Background(), req: &ProvisionTenantRequest{}},
			"nil context":     {ctx: nil, req: inviteReq},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				_, err := client.Provision(tc.ctx, tc.req)
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected invalid input error, got %v", err)
				}
			})
		}
	})

	t.Run("transport error", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, errors.NewNotFound("test", "no such route")
			},
		}
		_, err := NewTenantClient(mt).Provision(context.Background(), inviteReq)
		if !errors.IsNotFound(err) {
			t.Errorf("expected the transport error, got %v", err)
		}
	})
}

func TestTenantClient_List(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "List" {
					t.Errorf("expected method List, got %s", method)
				}
				listReq, ok := req.(*ListTenantsRequest)
				if !ok {
					t.Error("expected ListTenantsRequest")
				}
				if listReq.PageSize != 10 {
					t.Errorf("expected page size 10, got %d", listReq.PageSize)
				}
				return &ListTenantsResponse{
					Tenants: []Tenant{
						{ID: "tenant-1", Name: "tenant-1"},
						{ID: "tenant-2", Name: "tenant-2"},
					},
					Pagination: PaginationInfo{
						TotalCount: 2,
						PageSize:   10,
						HasMore:    false,
					},
				}, nil
			},
		}

		client := NewTenantClient(mt)
		resp, err := client.List(context.Background(), &ListTenantsRequest{PageSize: 10})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Tenants) != 2 {
			t.Errorf("expected 2 tenants, got %d", len(resp.Tenants))
		}
	})

	t.Run("nil context", func(t *testing.T) {
		client := NewTenantClient(&mockTransport{})
		_, err := client.List(nil, &ListTenantsRequest{})
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request defaults", func(t *testing.T) {
		mt := &mockTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return &ListTenantsResponse{Tenants: []Tenant{}}, nil
			},
		}

		client := NewTenantClient(mt)
		resp, err := client.List(context.Background(), nil)

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

		client := NewTenantClient(mt)
		resp, err := client.List(context.Background(), &ListTenantsRequest{})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Tenants) != 0 {
			t.Errorf("expected 0 tenants, got %d", len(resp.Tenants))
		}
	})
}

func TestTenantStatus_String(t *testing.T) {
	tests := []struct {
		status   TenantStatus
		expected string
	}{
		{TenantStatusUnspecified, "UNSPECIFIED"},
		{TenantStatusActive, "ACTIVE"},
		{TenantStatusSuspended, "SUSPENDED"},
		{TenantStatusDeleted, "DELETED"},
		{TenantStatus(99), "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("TenantStatus.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
