//go:build quic

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

package quic

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Dynamic Secrets Service
// =============================================================================

type mockDynamicSecretsService struct {
	createRoleFn         func(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error)
	getRoleFn            func(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error)
	listRolesFn          func(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error)
	deleteRoleFn         func(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error)
	generateCredentialFn func(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error)
	renewLeaseFn         func(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error)
	revokeLeaseFn        func(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error)
	listLeasesFn         func(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error)
}

func (m *mockDynamicSecretsService) CreateRole(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
	if m.createRoleFn != nil {
		return m.createRoleFn(ctx, req)
	}
	return &service.CreateRoleResponse{
		Role: &credentials.Role{
			Name:          req.Name,
			GeneratorType: req.GeneratorType,
			DefaultTTL:    req.DefaultTTL,
			MaxTTL:        req.MaxTTL,
		},
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockDynamicSecretsService) GetRole(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
	if m.getRoleFn != nil {
		return m.getRoleFn(ctx, req)
	}
	return &service.GetRoleResponse{
		Role: &credentials.Role{
			Name:          req.Name,
			GeneratorType: "postgres",
			DefaultTTL:    time.Hour,
			MaxTTL:        24 * time.Hour,
		},
	}, nil
}

func (m *mockDynamicSecretsService) ListRoles(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
	if m.listRolesFn != nil {
		return m.listRolesFn(ctx, req)
	}
	return &service.ListRolesResponse{
		Roles: []*credentials.Role{
			{Name: "role-1", GeneratorType: "postgres"},
			{Name: "role-2", GeneratorType: "mysql"},
		},
		Count: 2,
	}, nil
}

func (m *mockDynamicSecretsService) DeleteRole(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
	if m.deleteRoleFn != nil {
		return m.deleteRoleFn(ctx, req)
	}
	return &service.DeleteRoleResponse{
		DeletedAt: time.Now(),
	}, nil
}

func (m *mockDynamicSecretsService) GenerateCredential(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
	if m.generateCredentialFn != nil {
		return m.generateCredentialFn(ctx, req)
	}
	return &service.GenerateCredentialResponse{
		Credential: &credentials.Credential{
			CredentialID: "cred-12345",
			Type:         "database",
			Data: map[string]string{
				"role_name": req.RoleName,
				"username":  "dynamic_user_12345",
				"password":  "generated_password",
			},
			LeaseID: "lease-12345",
		},
		Lease: &lease.Lease{
			LeaseID:    "lease-12345",
			SecretPath: "credentials/" + req.RoleName + "/cred-12345",
			TTL:        time.Hour,
			Renewable:  true,
			RenewCount: 0,
		},
		LeaseID: "lease-12345",
	}, nil
}

func (m *mockDynamicSecretsService) RenewLease(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
	if m.renewLeaseFn != nil {
		return m.renewLeaseFn(ctx, req)
	}
	return &service.RenewLeaseResponse{
		Lease: &lease.Lease{
			LeaseID:    req.LeaseID,
			TTL:        time.Hour,
			RenewCount: 1,
			Renewable:  true,
		},
		RenewedAt: time.Now(),
	}, nil
}

func (m *mockDynamicSecretsService) RevokeLease(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
	if m.revokeLeaseFn != nil {
		return m.revokeLeaseFn(ctx, req)
	}
	return &service.RevokeLeaseResponse{
		RevokedAt: time.Now(),
	}, nil
}

func (m *mockDynamicSecretsService) ListLeases(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
	if m.listLeasesFn != nil {
		return m.listLeasesFn(ctx, req)
	}
	return &service.ListLeasesResponse{
		Leases: []*lease.Lease{
			{LeaseID: "lease-1", SecretPath: "credentials/role-1/cred-1"},
			{LeaseID: "lease-2", SecretPath: "credentials/role-1/cred-2"},
		},
		Count: 2,
	}, nil
}

// =============================================================================
// Error Mock Dynamic Secrets Service
// =============================================================================

type errorDynamicSecretsService struct{}

func (e *errorDynamicSecretsService) CreateRole(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "DynamicSecretsService.CreateRole",
		Err:  errServiceError,
	}
}

func (e *errorDynamicSecretsService) GetRole(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "DynamicSecretsService.GetRole",
		Err:  errServiceError,
	}
}

func (e *errorDynamicSecretsService) ListRoles(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "DynamicSecretsService.ListRoles",
		Err:  errServiceError,
	}
}

func (e *errorDynamicSecretsService) DeleteRole(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "DynamicSecretsService.DeleteRole",
		Err:  errServiceError,
	}
}

func (e *errorDynamicSecretsService) GenerateCredential(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "DynamicSecretsService.GenerateCredential",
		Err:  errServiceError,
	}
}

func (e *errorDynamicSecretsService) RenewLease(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "DynamicSecretsService.RenewLease",
		Err:  errServiceError,
	}
}

func (e *errorDynamicSecretsService) RevokeLease(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "DynamicSecretsService.RevokeLease",
		Err:  errServiceError,
	}
}

func (e *errorDynamicSecretsService) ListLeases(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
	return nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "DynamicSecretsService.ListLeases",
		Err:  errServiceError,
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

var errServiceError = &errors.QBError{
	Code: errors.ErrInternal,
	Op:   "test",
	Err:  nil,
}

func createDynamicSecretsTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-dynamic-secrets-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createDynamicSecretsTestHandler(svc service.DynamicSecretsService) *Handler {
	return NewHandler(&mockService{}, nil, nil, nil, nil, createDynamicSecretsTestLogger(), WithDynamicSecretsService(svc))
}

// =============================================================================
// handleDynamicSecretsCreateRole Tests
// =============================================================================

func TestHandleDynamicSecretsCreateRole_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		createRoleFn: func(ctx context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
			return &service.CreateRoleResponse{
				Role: &credentials.Role{
					Name:          req.Name,
					GeneratorType: req.GeneratorType,
					DefaultTTL:    req.DefaultTTL,
					MaxTTL:        req.MaxTTL,
				},
				CreatedAt: time.Now(),
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.CreateRoleRequest{
		Name:          "postgres-readonly",
		GeneratorType: "postgres",
		Config:        map[string]string{"connection_url": "postgres://localhost:5432/db"},
		DefaultTTL:    time.Hour,
		MaxTTL:        24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsCreateRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsCreateRole_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "postgres",
	})
	req := &Request{
		Opcode:  OpDynamicSecretsCreateRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsCreateRole_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsCreateRole,
		Payload: []byte("invalid payload data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsCreateRole_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.CreateRoleRequest{
		Name:          "test-role",
		GeneratorType: "postgres",
	})
	req := &Request{
		Opcode:  OpDynamicSecretsCreateRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleDynamicSecretsGetRole Tests
// =============================================================================

func TestHandleDynamicSecretsGetRole_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		getRoleFn: func(ctx context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
			return &service.GetRoleResponse{
				Role: &credentials.Role{
					Name:          req.Name,
					GeneratorType: "postgres",
					DefaultTTL:    time.Hour,
					MaxTTL:        24 * time.Hour,
				},
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.GetRoleRequest{
		Name: "postgres-readonly",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsGetRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsGetRole_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.GetRoleRequest{Name: "test-role"})
	req := &Request{
		Opcode:  OpDynamicSecretsGetRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsGetRole_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsGetRole,
		Payload: []byte("not valid msgpack"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsGetRole_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.GetRoleRequest{Name: "nonexistent"})
	req := &Request{
		Opcode:  OpDynamicSecretsGetRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleDynamicSecretsListRoles Tests
// =============================================================================

func TestHandleDynamicSecretsListRoles_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		listRolesFn: func(ctx context.Context, req *service.ListRolesRequest) (*service.ListRolesResponse, error) {
			return &service.ListRolesResponse{
				Roles: []*credentials.Role{
					{Name: "postgres-readonly", GeneratorType: "postgres"},
					{Name: "mysql-admin", GeneratorType: "mysql"},
					{Name: "mongodb-writer", GeneratorType: "mongodb"},
				},
				Count: 3,
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.ListRolesRequest{
		Prefix: "postgres",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsListRoles,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsListRoles_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.ListRolesRequest{})
	req := &Request{
		Opcode:  OpDynamicSecretsListRoles,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsListRoles_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsListRoles,
		Payload: []byte("garbage data here"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsListRoles_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.ListRolesRequest{})
	req := &Request{
		Opcode:  OpDynamicSecretsListRoles,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleDynamicSecretsDeleteRole Tests
// =============================================================================

func TestHandleDynamicSecretsDeleteRole_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		deleteRoleFn: func(ctx context.Context, req *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
			return &service.DeleteRoleResponse{
				DeletedAt: time.Now(),
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.DeleteRoleRequest{
		Name: "postgres-readonly",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsDeleteRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsDeleteRole_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.DeleteRoleRequest{Name: "test-role"})
	req := &Request{
		Opcode:  OpDynamicSecretsDeleteRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsDeleteRole_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsDeleteRole,
		Payload: []byte("bad payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsDeleteRole_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.DeleteRoleRequest{Name: "nonexistent"})
	req := &Request{
		Opcode:  OpDynamicSecretsDeleteRole,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleDynamicSecretsGenerate Tests
// =============================================================================

func TestHandleDynamicSecretsGenerate_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		generateCredentialFn: func(ctx context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
			return &service.GenerateCredentialResponse{
				Credential: &credentials.Credential{
					CredentialID: "cred-67890",
					Type:         "database",
					Data: map[string]string{
						"role_name": req.RoleName,
						"username":  "dynamic_user_67890",
						"password":  "secure_password_here",
					},
					LeaseID: "lease-67890",
				},
				Lease: &lease.Lease{
					LeaseID:    "lease-67890",
					SecretPath: "credentials/" + req.RoleName + "/cred-67890",
					TTL:        req.TTL,
					Renewable:  true,
					RenewCount: 0,
				},
				LeaseID: "lease-67890",
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.GenerateCredentialRequest{
		RoleName: "postgres-readonly",
		TTL:      2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsGenerate_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.GenerateCredentialRequest{RoleName: "test-role"})
	req := &Request{
		Opcode:  OpDynamicSecretsGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsGenerate_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsGenerate,
		Payload: []byte("invalid msgpack content"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsGenerate_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.GenerateCredentialRequest{RoleName: "nonexistent"})
	req := &Request{
		Opcode:  OpDynamicSecretsGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleDynamicSecretsRenewLease Tests
// =============================================================================

func TestHandleDynamicSecretsRenewLease_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		renewLeaseFn: func(ctx context.Context, req *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
			return &service.RenewLeaseResponse{
				Lease: &lease.Lease{
					LeaseID:    req.LeaseID,
					TTL:        req.Increment,
					RenewCount: 2,
					Renewable:  true,
				},
				RenewedAt: time.Now(),
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.RenewLeaseRequest{
		LeaseID:   "lease-12345",
		Increment: time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsRenewLease,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsRenewLease_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.RenewLeaseRequest{LeaseID: "lease-12345"})
	req := &Request{
		Opcode:  OpDynamicSecretsRenewLease,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsRenewLease_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsRenewLease,
		Payload: []byte("corrupted payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsRenewLease_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.RenewLeaseRequest{LeaseID: "nonexistent"})
	req := &Request{
		Opcode:  OpDynamicSecretsRenewLease,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleDynamicSecretsRevokeLease Tests
// =============================================================================

func TestHandleDynamicSecretsRevokeLease_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		revokeLeaseFn: func(ctx context.Context, req *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
			return &service.RevokeLeaseResponse{
				RevokedAt: time.Now(),
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.RevokeLeaseRequest{
		LeaseID: "lease-12345",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsRevokeLease,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsRevokeLease_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.RevokeLeaseRequest{LeaseID: "lease-12345"})
	req := &Request{
		Opcode:  OpDynamicSecretsRevokeLease,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsRevokeLease_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsRevokeLease,
		Payload: []byte("not valid data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsRevokeLease_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.RevokeLeaseRequest{LeaseID: "nonexistent"})
	req := &Request{
		Opcode:  OpDynamicSecretsRevokeLease,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleDynamicSecretsListLeases Tests
// =============================================================================

func TestHandleDynamicSecretsListLeases_Success(t *testing.T) {
	svc := &mockDynamicSecretsService{
		listLeasesFn: func(ctx context.Context, req *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
			return &service.ListLeasesResponse{
				Leases: []*lease.Lease{
					{LeaseID: "lease-1", SecretPath: "credentials/postgres-readonly/cred-1", TTL: time.Hour},
					{LeaseID: "lease-2", SecretPath: "credentials/postgres-readonly/cred-2", TTL: 2 * time.Hour},
					{LeaseID: "lease-3", SecretPath: "credentials/mysql-admin/cred-1", TTL: 30 * time.Minute},
				},
				Count: 3,
			}, nil
		},
	}
	handler := createDynamicSecretsTestHandler(svc)

	payload, err := EncodePayload(&service.ListLeasesRequest{
		Prefix: "credentials/postgres",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpDynamicSecretsListLeases,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDynamicSecretsListLeases_NilService(t *testing.T) {
	handler := createDynamicSecretsTestHandler(nil)

	payload, _ := EncodePayload(&service.ListLeasesRequest{})
	req := &Request{
		Opcode:  OpDynamicSecretsListLeases,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil dynamic secrets service")
	}
}

func TestHandleDynamicSecretsListLeases_DecodeError(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	req := &Request{
		Opcode:  OpDynamicSecretsListLeases,
		Payload: []byte("broken list request"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleDynamicSecretsListLeases_ServiceError(t *testing.T) {
	svc := &errorDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.ListLeasesRequest{})
	req := &Request{
		Opcode:  OpDynamicSecretsListLeases,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Table-Driven Tests for All Dynamic Secrets Operations
// =============================================================================

func TestHandleDynamicSecretsOperations_AllOpcodes(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"DynamicSecretsCreateRole", OpDynamicSecretsCreateRole, &service.CreateRoleRequest{Name: "test", GeneratorType: "postgres", DefaultTTL: time.Hour, MaxTTL: 24 * time.Hour}},
		{"DynamicSecretsGetRole", OpDynamicSecretsGetRole, &service.GetRoleRequest{Name: "test"}},
		{"DynamicSecretsListRoles", OpDynamicSecretsListRoles, &service.ListRolesRequest{}},
		{"DynamicSecretsDeleteRole", OpDynamicSecretsDeleteRole, &service.DeleteRoleRequest{Name: "test"}},
		{"DynamicSecretsGenerate", OpDynamicSecretsGenerate, &service.GenerateCredentialRequest{RoleName: "test"}},
		{"DynamicSecretsRenewLease", OpDynamicSecretsRenewLease, &service.RenewLeaseRequest{LeaseID: "lease-1"}},
		{"DynamicSecretsRevokeLease", OpDynamicSecretsRevokeLease, &service.RevokeLeaseRequest{LeaseID: "lease-1"}},
		{"DynamicSecretsListLeases", OpDynamicSecretsListLeases, &service.ListLeasesRequest{}},
	}

	// Verify we have all 8 operations
	if len(tests) != 8 {
		t.Errorf("expected 8 test cases, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK for %s, got %d", tt.name, resp.Status)
			}
		})
	}
}

// =============================================================================
// Table-Driven Tests for All Error Cases
// =============================================================================

func TestHandleDynamicSecretsOperations_AllErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		opcode      byte
		payload     interface{}
		useNilSvc   bool
		useErrorSvc bool
		expectError bool
	}{
		// Nil service tests
		{"CreateRole_NilService", OpDynamicSecretsCreateRole, &service.CreateRoleRequest{Name: "test"}, true, false, true},
		{"GetRole_NilService", OpDynamicSecretsGetRole, &service.GetRoleRequest{Name: "test"}, true, false, true},
		{"ListRoles_NilService", OpDynamicSecretsListRoles, &service.ListRolesRequest{}, true, false, true},
		{"DeleteRole_NilService", OpDynamicSecretsDeleteRole, &service.DeleteRoleRequest{Name: "test"}, true, false, true},
		{"Generate_NilService", OpDynamicSecretsGenerate, &service.GenerateCredentialRequest{RoleName: "test"}, true, false, true},
		{"RenewLease_NilService", OpDynamicSecretsRenewLease, &service.RenewLeaseRequest{LeaseID: "lease-1"}, true, false, true},
		{"RevokeLease_NilService", OpDynamicSecretsRevokeLease, &service.RevokeLeaseRequest{LeaseID: "lease-1"}, true, false, true},
		{"ListLeases_NilService", OpDynamicSecretsListLeases, &service.ListLeasesRequest{}, true, false, true},

		// Error service tests
		{"CreateRole_ErrorService", OpDynamicSecretsCreateRole, &service.CreateRoleRequest{Name: "test"}, false, true, true},
		{"GetRole_ErrorService", OpDynamicSecretsGetRole, &service.GetRoleRequest{Name: "test"}, false, true, true},
		{"ListRoles_ErrorService", OpDynamicSecretsListRoles, &service.ListRolesRequest{}, false, true, true},
		{"DeleteRole_ErrorService", OpDynamicSecretsDeleteRole, &service.DeleteRoleRequest{Name: "test"}, false, true, true},
		{"Generate_ErrorService", OpDynamicSecretsGenerate, &service.GenerateCredentialRequest{RoleName: "test"}, false, true, true},
		{"RenewLease_ErrorService", OpDynamicSecretsRenewLease, &service.RenewLeaseRequest{LeaseID: "lease-1"}, false, true, true},
		{"RevokeLease_ErrorService", OpDynamicSecretsRevokeLease, &service.RevokeLeaseRequest{LeaseID: "lease-1"}, false, true, true},
		{"ListLeases_ErrorService", OpDynamicSecretsListLeases, &service.ListLeasesRequest{}, false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var handler *Handler
			if tt.useNilSvc {
				handler = createDynamicSecretsTestHandler(nil)
			} else if tt.useErrorSvc {
				handler = createDynamicSecretsTestHandler(&errorDynamicSecretsService{})
			} else {
				handler = createDynamicSecretsTestHandler(&mockDynamicSecretsService{})
			}

			payload, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)

			if tt.expectError && resp.Status == StatusOK {
				t.Errorf("expected error for %s, got StatusOK", tt.name)
			}
			if !tt.expectError && resp.Status != StatusOK {
				t.Errorf("expected StatusOK for %s, got %d", tt.name, resp.Status)
			}
		})
	}
}

// =============================================================================
// Decode Error Tests for All Operations
// =============================================================================

func TestHandleDynamicSecretsOperations_AllDecodeErrors(t *testing.T) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	tests := []struct {
		name   string
		opcode byte
	}{
		{"CreateRole_DecodeError", OpDynamicSecretsCreateRole},
		{"GetRole_DecodeError", OpDynamicSecretsGetRole},
		{"ListRoles_DecodeError", OpDynamicSecretsListRoles},
		{"DeleteRole_DecodeError", OpDynamicSecretsDeleteRole},
		{"Generate_DecodeError", OpDynamicSecretsGenerate},
		{"RenewLease_DecodeError", OpDynamicSecretsRenewLease},
		{"RevokeLease_DecodeError", OpDynamicSecretsRevokeLease},
		{"ListLeases_DecodeError", OpDynamicSecretsListLeases},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{
				Opcode:  tt.opcode,
				Payload: []byte("invalid msgpack data for decode error test"),
			}

			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status == StatusOK {
				t.Errorf("expected decode error for %s, got StatusOK", tt.name)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkHandler_HandleDynamicSecretsCreateRole(b *testing.B) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.CreateRoleRequest{
		Name:          "benchmark-role",
		GeneratorType: "postgres",
		DefaultTTL:    time.Hour,
		MaxTTL:        24 * time.Hour,
	})
	req := &Request{
		Opcode:  OpDynamicSecretsCreateRole,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleDynamicSecretsGenerate(b *testing.B) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.GenerateCredentialRequest{
		RoleName: "benchmark-role",
		TTL:      time.Hour,
	})
	req := &Request{
		Opcode:  OpDynamicSecretsGenerate,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleDynamicSecretsListLeases(b *testing.B) {
	svc := &mockDynamicSecretsService{}
	handler := createDynamicSecretsTestHandler(svc)

	payload, _ := EncodePayload(&service.ListLeasesRequest{
		Prefix: "credentials/",
	})
	req := &Request{
		Opcode:  OpDynamicSecretsListLeases,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}
