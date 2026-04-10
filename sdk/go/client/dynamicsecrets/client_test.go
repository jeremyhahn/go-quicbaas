// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package dynamicsecrets

import (
	"context"
	"testing"
	"time"

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

func TestClient_CreateRole_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &CreateRoleResponse{
				Role:      &Role{Name: "test-role"},
				CreatedAt: time.Now(),
			}, nil
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
			name: "empty name",
			req: &CreateRoleRequest{
				GeneratorType: "postgres",
				DefaultTTL:    time.Hour,
				MaxTTL:        24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "empty generator type",
			req: &CreateRoleRequest{
				Name:       "test-role",
				DefaultTTL: time.Hour,
				MaxTTL:     24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "zero default TTL",
			req: &CreateRoleRequest{
				Name:          "test-role",
				GeneratorType: "postgres",
				DefaultTTL:    0,
				MaxTTL:        24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "negative default TTL",
			req: &CreateRoleRequest{
				Name:          "test-role",
				GeneratorType: "postgres",
				DefaultTTL:    -time.Hour,
				MaxTTL:        24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "zero max TTL",
			req: &CreateRoleRequest{
				Name:          "test-role",
				GeneratorType: "postgres",
				DefaultTTL:    time.Hour,
				MaxTTL:        0,
			},
			wantErr: true,
		},
		{
			name: "default TTL exceeds max TTL",
			req: &CreateRoleRequest{
				Name:          "test-role",
				GeneratorType: "postgres",
				DefaultTTL:    48 * time.Hour,
				MaxTTL:        24 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &CreateRoleRequest{
				Name:          "postgres-readonly",
				GeneratorType: "postgres",
				Config:        map[string]string{"connection_url": "postgres://localhost:5432/db"},
				DefaultTTL:    time.Hour,
				MaxTTL:        24 * time.Hour,
			},
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

func TestClient_CreateRole(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful create",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "CreateRole" {
						t.Errorf("expected method %q, got %q", "CreateRole", method)
					}
					return &CreateRoleResponse{
						Role:      &Role{Name: "test-role"},
						CreatedAt: time.Now(),
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "connection failed")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "postgres",
		Config:        map[string]string{"connection_url": "postgres://localhost/db"},
		DefaultTTL:    time.Hour,
		MaxTTL:        24 * time.Hour,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.CreateRole(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetRole_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetRoleResponse{Role: &Role{Name: "test-role"}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetRoleRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &GetRoleRequest{Name: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetRoleRequest{Name: "postgres-readonly"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetRole(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GetRole(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetRole" {
						t.Errorf("expected method %q, got %q", "GetRole", method)
					}
					return &GetRoleResponse{
						Role: &Role{
							Name:          "postgres-readonly",
							GeneratorType: "postgres",
							DefaultTTL:    time.Hour,
							MaxTTL:        24 * time.Hour,
						},
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &GetRoleRequest{Name: "test-role"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetRole(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_ListRoles(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		req       *ListRolesRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		roleCount int
	}{
		{
			name: "successful list",
			ctx:  context.Background(),
			req:  &ListRolesRequest{Prefix: "postgres-"},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ListRoles" {
						t.Errorf("expected method %q, got %q", "ListRoles", method)
					}
					return &ListRolesResponse{
						Roles: []*Role{
							{Name: "postgres-readonly"},
							{Name: "postgres-admin"},
						},
						Count: 2,
					}, nil
				},
			},
			wantErr:   false,
			roleCount: 2,
		},
		{
			name: "empty list with nil response",
			ctx:  context.Background(),
			req:  &ListRolesRequest{Prefix: "nonexistent-"},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:   false,
			roleCount: 0,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       &ListRolesRequest{},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			req:  &ListRolesRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListRoles(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListRoles() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Roles) != tt.roleCount {
				t.Errorf("expected %d roles, got %d", tt.roleCount, len(resp.Roles))
			}
		})
	}
}

func TestClient_DeleteRole_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DeleteRoleResponse{DeletedAt: time.Now()}, nil
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
			name:    "empty name",
			req:     &DeleteRoleRequest{Name: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DeleteRoleRequest{Name: "postgres-readonly"},
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

func TestClient_DeleteRole(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful delete",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "DeleteRole" {
						t.Errorf("expected method %q, got %q", "DeleteRole", method)
					}
					return &DeleteRoleResponse{DeletedAt: time.Now()}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "not found error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "role not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &DeleteRoleRequest{Name: "test-role"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.DeleteRole(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteRole() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GenerateCredential_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GenerateCredentialResponse{
				Credential: &Credential{CredentialID: "cred-123"},
				Lease:      &Lease{LeaseID: "lease-123"},
				LeaseID:    "lease-123",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GenerateCredentialRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty role name",
			req:     &GenerateCredentialRequest{RoleName: ""},
			wantErr: true,
		},
		{
			name:    "negative TTL",
			req:     &GenerateCredentialRequest{RoleName: "test-role", TTL: -time.Hour},
			wantErr: true,
		},
		{
			name:    "valid request without TTL",
			req:     &GenerateCredentialRequest{RoleName: "postgres-readonly"},
			wantErr: false,
		},
		{
			name:    "valid request with TTL",
			req:     &GenerateCredentialRequest{RoleName: "postgres-readonly", TTL: 2 * time.Hour},
			wantErr: false,
		},
		{
			name:    "valid request with zero TTL (uses default)",
			req:     &GenerateCredentialRequest{RoleName: "postgres-readonly", TTL: 0},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateCredential(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateCredential() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GenerateCredential(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)

	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful generate",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GenerateCredential" {
						t.Errorf("expected method %q, got %q", "GenerateCredential", method)
					}
					return &GenerateCredentialResponse{
						Credential: &Credential{
							CredentialID: "cred-abc123",
							Type:         "database",
							Data: map[string]string{
								"username": "v_postgres_readonly_abc123",
								"password": "s3cr3t-p4ssw0rd",
							},
							LeaseID:   "lease-xyz789",
							CreatedAt: now,
							ExpiresAt: expiresAt,
						},
						Lease: &Lease{
							LeaseID:   "lease-xyz789",
							TTL:       time.Hour,
							IssuedAt:  now,
							ExpiresAt: expiresAt,
							Renewable: true,
						},
						LeaseID: "lease-xyz789",
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "role not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "role not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &GenerateCredentialRequest{RoleName: "postgres-readonly", TTL: time.Hour}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GenerateCredential(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateCredential() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_RenewLease_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RenewLeaseResponse{
				Lease:     &Lease{LeaseID: "lease-123"},
				RenewedAt: time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RenewLeaseRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty lease ID",
			req:     &RenewLeaseRequest{LeaseID: ""},
			wantErr: true,
		},
		{
			name:    "negative increment",
			req:     &RenewLeaseRequest{LeaseID: "lease-123", Increment: -time.Hour},
			wantErr: true,
		},
		{
			name:    "valid request without increment",
			req:     &RenewLeaseRequest{LeaseID: "lease-123"},
			wantErr: false,
		},
		{
			name:    "valid request with increment",
			req:     &RenewLeaseRequest{LeaseID: "lease-123", Increment: 30 * time.Minute},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RenewLease(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenewLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_RenewLease(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful renew",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "RenewLease" {
						t.Errorf("expected method %q, got %q", "RenewLease", method)
					}
					return &RenewLeaseResponse{
						Lease: &Lease{
							LeaseID:    "lease-123",
							RenewCount: 1,
							ExpiresAt:  now.Add(2 * time.Hour),
						},
						RenewedAt: now,
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "lease not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "lease not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &RenewLeaseRequest{LeaseID: "lease-123", Increment: 30 * time.Minute}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.RenewLease(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenewLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_RevokeLease_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RevokeLeaseResponse{RevokedAt: time.Now()}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RevokeLeaseRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty lease ID",
			req:     &RevokeLeaseRequest{LeaseID: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RevokeLeaseRequest{LeaseID: "lease-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RevokeLease(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RevokeLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_RevokeLease(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful revoke",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "RevokeLease" {
						t.Errorf("expected method %q, got %q", "RevokeLease", method)
					}
					return &RevokeLeaseResponse{RevokedAt: time.Now()}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "lease not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "lease not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &RevokeLeaseRequest{LeaseID: "lease-123"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.RevokeLease(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("RevokeLease() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_ListLeases(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		ctx        context.Context
		req        *ListLeasesRequest
		transport  *mockTransport
		wantErr    bool
		errCode    errors.ErrorCode
		leaseCount int
	}{
		{
			name: "successful list",
			ctx:  context.Background(),
			req:  &ListLeasesRequest{Prefix: "database/"},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ListLeases" {
						t.Errorf("expected method %q, got %q", "ListLeases", method)
					}
					return &ListLeasesResponse{
						Leases: []*Lease{
							{LeaseID: "lease-1", SecretPath: "database/prod", ExpiresAt: now.Add(time.Hour)},
							{LeaseID: "lease-2", SecretPath: "database/staging", ExpiresAt: now.Add(2 * time.Hour)},
							{LeaseID: "lease-3", SecretPath: "database/dev", ExpiresAt: now.Add(30 * time.Minute)},
						},
						Count: 3,
					}, nil
				},
			},
			wantErr:    false,
			leaseCount: 3,
		},
		{
			name: "empty list with nil response",
			ctx:  context.Background(),
			req:  &ListLeasesRequest{Prefix: "nonexistent/"},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:    false,
			leaseCount: 0,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       &ListLeasesRequest{},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			req:  &ListLeasesRequest{},
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListLeases(tt.ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListLeases() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Leases) != tt.leaseCount {
				t.Errorf("expected %d leases, got %d", tt.leaseCount, len(resp.Leases))
			}
		})
	}
}

func TestRole_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetRoleResponse{
				Role: &Role{
					Name:          "postgres-readonly",
					GeneratorType: "postgres",
					Config: map[string]string{
						"connection_url":      "postgres://localhost:5432/mydb",
						"creation_statements": "CREATE ROLE {{name}} WITH LOGIN PASSWORD '{{password}}';",
					},
					DefaultTTL: time.Hour,
					MaxTTL:     24 * time.Hour,
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GetRole(context.Background(), &GetRoleRequest{Name: "postgres-readonly"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	role := resp.Role
	if role.Name != "postgres-readonly" {
		t.Errorf("expected Name='postgres-readonly', got %q", role.Name)
	}
	if role.GeneratorType != "postgres" {
		t.Errorf("expected GeneratorType='postgres', got %q", role.GeneratorType)
	}
	if role.Config["connection_url"] != "postgres://localhost:5432/mydb" {
		t.Errorf("expected connection_url in config, got %v", role.Config)
	}
	if role.DefaultTTL != time.Hour {
		t.Errorf("expected DefaultTTL=1h, got %v", role.DefaultTTL)
	}
	if role.MaxTTL != 24*time.Hour {
		t.Errorf("expected MaxTTL=24h, got %v", role.MaxTTL)
	}
}

func TestCredential_Fields(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GenerateCredentialResponse{
				Credential: &Credential{
					CredentialID: "cred-abc123",
					Type:         "database",
					Data: map[string]string{
						"username": "v_postgres_readonly_abc123",
						"password": "generated-password",
					},
					LeaseID:   "lease-xyz789",
					CreatedAt: now,
					ExpiresAt: expiresAt,
				},
				Lease: &Lease{
					LeaseID:     "lease-xyz789",
					SecretPath:  "database/postgres-readonly",
					IssuedAt:    now,
					ExpiresAt:   expiresAt,
					TTL:         time.Hour,
					RenewCount:  0,
					MaxRenewals: 5,
					Renewable:   true,
				},
				LeaseID: "lease-xyz789",
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GenerateCredential(context.Background(), &GenerateCredentialRequest{
		RoleName: "postgres-readonly",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cred := resp.Credential
	if cred.CredentialID != "cred-abc123" {
		t.Errorf("expected CredentialID='cred-abc123', got %q", cred.CredentialID)
	}
	if cred.Type != "database" {
		t.Errorf("expected Type='database', got %q", cred.Type)
	}
	if cred.Data["username"] != "v_postgres_readonly_abc123" {
		t.Errorf("expected username in Data, got %v", cred.Data)
	}
	if cred.LeaseID != "lease-xyz789" {
		t.Errorf("expected LeaseID='lease-xyz789', got %q", cred.LeaseID)
	}

	lease := resp.Lease
	if lease.LeaseID != "lease-xyz789" {
		t.Errorf("expected Lease.LeaseID='lease-xyz789', got %q", lease.LeaseID)
	}
	if !lease.Renewable {
		t.Error("expected lease to be renewable")
	}
	if lease.MaxRenewals != 5 {
		t.Errorf("expected MaxRenewals=5, got %d", lease.MaxRenewals)
	}
}

func TestLease_Fields(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListLeasesResponse{
				Leases: []*Lease{
					{
						LeaseID:     "lease-123",
						SecretPath:  "database/prod",
						IssuedAt:    now,
						ExpiresAt:   expiresAt,
						TTL:         time.Hour,
						RenewCount:  2,
						MaxRenewals: 10,
						Renewable:   true,
						Metadata: map[string]string{
							"role": "postgres-readonly",
						},
					},
				},
				Count: 1,
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.ListLeases(context.Background(), &ListLeasesRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Leases) != 1 {
		t.Fatalf("expected 1 lease, got %d", len(resp.Leases))
	}

	lease := resp.Leases[0]
	if lease.LeaseID != "lease-123" {
		t.Errorf("expected LeaseID='lease-123', got %q", lease.LeaseID)
	}
	if lease.SecretPath != "database/prod" {
		t.Errorf("expected SecretPath='database/prod', got %q", lease.SecretPath)
	}
	if lease.TTL != time.Hour {
		t.Errorf("expected TTL=1h, got %v", lease.TTL)
	}
	if lease.RenewCount != 2 {
		t.Errorf("expected RenewCount=2, got %d", lease.RenewCount)
	}
	if lease.Metadata["role"] != "postgres-readonly" {
		t.Errorf("expected role metadata, got %v", lease.Metadata)
	}
}
