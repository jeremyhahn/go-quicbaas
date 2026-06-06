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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Backup Handler Success + Service Error Tests (33.3% -> ~91%)
// =============================================================================

func TestHandleListBackups_Success(t *testing.T) {
	svc := &mockService{
		listBackupsFn: func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
			return &service.ListBackupsResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListBackupsRequest{})
	resp := handler.handleListBackups(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleListBackups_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ListBackupsRequest{})
	resp := handler.handleListBackups(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleListBackups_ServiceError(t *testing.T) {
	svc := &mockService{
		listBackupsFn: func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
			return nil, errors.New("list failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ListBackupsRequest{})
	resp := handler.handleListBackups(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleRestoreBackup_Success(t *testing.T) {
	svc := &mockService{
		restoreBackupFn: func(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
			return &service.RestoreBackupResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RestoreBackupRequest{})
	resp := handler.handleRestoreBackup(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleRestoreBackup_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RestoreBackupRequest{})
	resp := handler.handleRestoreBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleRestoreBackup_ServiceError(t *testing.T) {
	svc := &mockService{
		restoreBackupFn: func(ctx context.Context, req *service.RestoreBackupRequest) (*service.RestoreBackupResponse, error) {
			return nil, errors.New("restore failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RestoreBackupRequest{})
	resp := handler.handleRestoreBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleDeleteBackup_Success(t *testing.T) {
	svc := &mockService{
		deleteBackupFn: func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
			return &service.DeleteBackupResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.DeleteBackupRequest{})
	resp := handler.handleDeleteBackup(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDeleteBackup_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.DeleteBackupRequest{})
	resp := handler.handleDeleteBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleDeleteBackup_ServiceError(t *testing.T) {
	svc := &mockService{
		deleteBackupFn: func(ctx context.Context, req *service.DeleteBackupRequest) (*service.DeleteBackupResponse, error) {
			return nil, errors.New("delete failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.DeleteBackupRequest{})
	resp := handler.handleDeleteBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleVerifyBackup_Success(t *testing.T) {
	svc := &mockService{
		verifyBackupFn: func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
			return &service.VerifyBackupResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.VerifyBackupRequest{})
	resp := handler.handleVerifyBackup(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleVerifyBackup_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.VerifyBackupRequest{})
	resp := handler.handleVerifyBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleVerifyBackup_ServiceError(t *testing.T) {
	svc := &mockService{
		verifyBackupFn: func(ctx context.Context, req *service.VerifyBackupRequest) (*service.VerifyBackupResponse, error) {
			return nil, errors.New("verify failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.VerifyBackupRequest{})
	resp := handler.handleVerifyBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Auth Handler Tests
// (Password-based login has been removed; use REST WebAuthn/PIV endpoints.)
// =============================================================================

func TestHandleLogout_Success(t *testing.T) {
	svc := &mockService{
		logoutFn: func(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
			return &service.LogoutResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.LogoutRequest{})
	resp := handler.handleLogout(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleLogout_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.LogoutRequest{})
	resp := handler.handleLogout(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleLogout_ServiceError(t *testing.T) {
	svc := &mockService{
		logoutFn: func(ctx context.Context, req *service.LogoutRequest) (*service.LogoutResponse, error) {
			return nil, errors.New("logout failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.LogoutRequest{})
	resp := handler.handleLogout(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleRefreshToken_Success(t *testing.T) {
	svc := &mockService{
		refreshTokenFn: func(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
			return &service.RefreshTokenResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RefreshTokenRequest{})
	resp := handler.handleRefreshToken(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleRefreshToken_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RefreshTokenRequest{})
	resp := handler.handleRefreshToken(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleRefreshToken_ServiceError(t *testing.T) {
	svc := &mockService{
		refreshTokenFn: func(ctx context.Context, req *service.RefreshTokenRequest) (*service.RefreshTokenResponse, error) {
			return nil, errors.New("refresh failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RefreshTokenRequest{})
	resp := handler.handleRefreshToken(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleValidateToken_Success(t *testing.T) {
	svc := &mockService{
		validateTokenFn: func(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
			return &service.ValidateTokenResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ValidateTokenRequest{})
	resp := handler.handleValidateToken(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleValidateToken_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ValidateTokenRequest{})
	resp := handler.handleValidateToken(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleValidateToken_ServiceError(t *testing.T) {
	svc := &mockService{
		validateTokenFn: func(ctx context.Context, req *service.ValidateTokenRequest) (*service.ValidateTokenResponse, error) {
			return nil, errors.New("validate failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ValidateTokenRequest{})
	resp := handler.handleValidateToken(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// RBAC Handler Success + Service Error Tests (33.3% -> ~91%)
// =============================================================================

func TestHandleCreateUser_Success(t *testing.T) {
	svc := &mockService{
		createUserFn: func(ctx context.Context, req *service.AuthCreateUserRequest) (*service.AuthCreateUserResponse, error) {
			return &service.AuthCreateUserResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthCreateUserRequest{})
	resp := handler.handleCreateUser(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCreateUser_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthCreateUserRequest{})
	resp := handler.handleCreateUser(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleCreateUser_ServiceError(t *testing.T) {
	svc := &mockService{
		createUserFn: func(ctx context.Context, req *service.AuthCreateUserRequest) (*service.AuthCreateUserResponse, error) {
			return nil, errors.New("create user failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthCreateUserRequest{})
	resp := handler.handleCreateUser(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleDeleteUser_Success(t *testing.T) {
	svc := &mockService{
		deleteUserFn: func(ctx context.Context, req *service.AuthDeleteUserRequest) (*service.AuthDeleteUserResponse, error) {
			return &service.AuthDeleteUserResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthDeleteUserRequest{})
	resp := handler.handleDeleteUser(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDeleteUser_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthDeleteUserRequest{})
	resp := handler.handleDeleteUser(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleListUsers_Success(t *testing.T) {
	svc := &mockService{
		listUsersFn: func(ctx context.Context, req *service.AuthListUsersRequest) (*service.AuthListUsersResponse, error) {
			return &service.AuthListUsersResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthListUsersRequest{})
	resp := handler.handleListUsers(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleListUsers_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthListUsersRequest{})
	resp := handler.handleListUsers(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleCreateRole_Success(t *testing.T) {
	svc := &mockService{
		createAuthRoleFn: func(ctx context.Context, req *service.AuthCreateRoleRequest) (*service.AuthCreateRoleResponse, error) {
			return &service.AuthCreateRoleResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthCreateRoleRequest{})
	resp := handler.handleCreateRole(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCreateRole_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthCreateRoleRequest{})
	resp := handler.handleCreateRole(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleDeleteRole_Success(t *testing.T) {
	svc := &mockService{
		deleteAuthRoleFn: func(ctx context.Context, req *service.AuthDeleteRoleRequest) (*service.AuthDeleteRoleResponse, error) {
			return &service.AuthDeleteRoleResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthDeleteRoleRequest{})
	resp := handler.handleDeleteRole(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleDeleteRole_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthDeleteRoleRequest{})
	resp := handler.handleDeleteRole(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleListRoles_Success(t *testing.T) {
	svc := &mockService{
		listAuthRolesFn: func(ctx context.Context, req *service.AuthListRolesRequest) (*service.AuthListRolesResponse, error) {
			return &service.AuthListRolesResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthListRolesRequest{})
	resp := handler.handleListRoles(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleListRoles_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AuthListRolesRequest{})
	resp := handler.handleListRoles(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleAssignRole_Success(t *testing.T) {
	svc := &mockService{
		assignRoleFn: func(ctx context.Context, req *service.AssignRoleRequest) (*service.AssignRoleResponse, error) {
			return &service.AssignRoleResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AssignRoleRequest{})
	resp := handler.handleAssignRole(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleAssignRole_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AssignRoleRequest{})
	resp := handler.handleAssignRole(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleRevokeRole_Success(t *testing.T) {
	svc := &mockService{
		revokeRoleFn: func(ctx context.Context, req *service.RevokeRoleRequest) (*service.RevokeRoleResponse, error) {
			return &service.RevokeRoleResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RevokeRoleRequest{})
	resp := handler.handleRevokeRole(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleRevokeRole_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.RevokeRoleRequest{})
	resp := handler.handleRevokeRole(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

// =============================================================================
// Cache Handler Success + Service Error Tests (33.3% -> ~91%)
// =============================================================================

func TestHandleGetCacheStats_Success(t *testing.T) {
	svc := &mockService{
		getCacheStatsFn: func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
			return &service.GetCacheStatsResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.GetCacheStatsRequest{})
	resp := handler.handleGetCacheStats(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleGetCacheStats_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.GetCacheStatsRequest{})
	resp := handler.handleGetCacheStats(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleClearCache_Success(t *testing.T) {
	svc := &mockService{
		clearCacheFn: func(ctx context.Context, req *service.ClearCacheRequest) (*service.ClearCacheResponse, error) {
			return &service.ClearCacheResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ClearCacheRequest{})
	resp := handler.handleClearCache(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClearCache_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ClearCacheRequest{})
	resp := handler.handleClearCache(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleGetCacheConfig_Success(t *testing.T) {
	svc := &mockService{
		getCacheConfigFn: func(ctx context.Context, req *service.GetCacheConfigRequest) (*service.GetCacheConfigResponse, error) {
			return &service.GetCacheConfigResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.GetCacheConfigRequest{})
	resp := handler.handleGetCacheConfig(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleGetCacheConfig_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.GetCacheConfigRequest{})
	resp := handler.handleGetCacheConfig(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleUpdateCacheConfig_Success(t *testing.T) {
	svc := &mockService{
		updateCacheConfigFn: func(ctx context.Context, req *service.UpdateCacheConfigRequest) (*service.UpdateCacheConfigResponse, error) {
			return &service.UpdateCacheConfigResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.UpdateCacheConfigRequest{})
	resp := handler.handleUpdateCacheConfig(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleUpdateCacheConfig_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.UpdateCacheConfigRequest{})
	resp := handler.handleUpdateCacheConfig(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

// =============================================================================
// Health Handler Success + Service Error Tests (33.3% -> ~91%)
// =============================================================================

func TestHandleHealth_Success(t *testing.T) {
	svc := &mockService{
		healthFn: func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
			return &service.HealthResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.HealthRequest{})
	resp := handler.handleHealth(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleHealth_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.HealthRequest{})
	resp := handler.handleHealth(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleLiveness_Success(t *testing.T) {
	svc := &mockService{
		livenessFn: func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
			return &service.LivenessResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.LivenessRequest{})
	resp := handler.handleLiveness(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleLiveness_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.LivenessRequest{})
	resp := handler.handleLiveness(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleReadiness_Success(t *testing.T) {
	svc := &mockService{
		readinessFn: func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
			return &service.ReadinessResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ReadinessRequest{})
	resp := handler.handleReadiness(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleReadiness_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ReadinessRequest{})
	resp := handler.handleReadiness(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

// =============================================================================
// Aggregate Handler Success + Service Error Tests (33.3% -> ~91%)
// =============================================================================

func TestHandleAggregate_Success(t *testing.T) {
	svc := &mockService{
		aggregateFn: func(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
			return &service.AggregateResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AggregateRequest{})
	resp := handler.handleAggregate(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleAggregate_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AggregateRequest{})
	resp := handler.handleAggregate(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleAggregate_ServiceError(t *testing.T) {
	svc := &mockService{
		aggregateFn: func(ctx context.Context, req *service.AggregateRequest) (*service.AggregateResponse, error) {
			return nil, errors.New("aggregate failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.AggregateRequest{})
	resp := handler.handleAggregate(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}
