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

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Mock Auth Manager for Testing
// =============================================================================

type mockAuthManager struct {
	validateTokenFunc func(ctx context.Context, token string) (interface{}, error)
}

func (m *mockAuthManager) IssueTokenForUser(ctx context.Context, user interfaces.UserInfo) (interface{}, error) {
	return nil, nil
}

func (m *mockAuthManager) Authorize(ctx context.Context, token, resource, action string) error {
	return nil
}

func (m *mockAuthManager) RefreshToken(ctx context.Context, refreshToken string) (interface{}, error) {
	return nil, nil
}

func (m *mockAuthManager) ValidateToken(ctx context.Context, token string) (interface{}, error) {
	if m.validateTokenFunc != nil {
		return m.validateTokenFunc(ctx, token)
	}
	return nil, nil
}

func (m *mockAuthManager) RevokeToken(ctx context.Context, token string) error {
	return nil
}

func (m *mockAuthManager) CreateUser(username string, roles []string) (interface{}, error) {
	return nil, nil
}

func (m *mockAuthManager) GetUserStore() interfaces.UserStore {
	return nil
}

func (m *mockAuthManager) GetRoleManager() interfaces.RoleManager {
	return nil
}

// =============================================================================
// Mock Authorization Service for Testing
// =============================================================================

type mockAuthzService struct {
	authorizeFunc func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error)
}

func (m *mockAuthzService) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	if m.authorizeFunc != nil {
		return m.authorizeFunc(ctx, req)
	}
	return &interfaces.AuthorizationDecision{Allowed: true}, nil
}

func (m *mockAuthzService) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	return nil, nil
}

func (m *mockAuthzService) GetProvider() interfaces.AuthorizationProvider {
	return nil
}

func (m *mockAuthzService) GetProviderType() string {
	return "mock"
}

func (m *mockAuthzService) Close() error {
	return nil
}

// =============================================================================
// Authorization Tests
// =============================================================================

func TestIsPublicOperation(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		expected bool
	}{
		{"Health is public", OpHealth, true},
		{"Liveness is public", OpLiveness, true},
		{"Readiness is public", OpReadiness, true},
		{"Get is not public", OpGet, false},
		{"Put is not public", OpPut, false},
		{"Delete is not public", OpDelete, false},
		{"ClusterStatus is not public", OpClusterStatus, false},
		{"KMSGenerateKey is not public", OpKMSGenerateKey, false},
		{"SecretsGet is not public", OpSecretsGet, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPublicOperation(tt.opcode)
			if result != tt.expected {
				t.Errorf("isPublicOperation(%d) = %v, want %v", tt.opcode, result, tt.expected)
			}
		})
	}
}

func TestGetResourceAndAction(t *testing.T) {
	tests := []struct {
		name         string
		opcode       byte
		wantResource string
		wantAction   string
	}{
		// KV Operations
		{"Get", OpGet, interfaces.ResourceKeys, interfaces.ActionRead},
		{"List", OpList, interfaces.ResourceKeys, interfaces.ActionRead},
		{"Put", OpPut, interfaces.ResourceKeys, interfaces.ActionWrite},
		{"Delete", OpDelete, interfaces.ResourceKeys, interfaces.ActionDelete},
		{"BatchGet", OpBatchGet, interfaces.ResourceKeys, interfaces.ActionRead},
		{"BatchPut", OpBatchPut, interfaces.ResourceKeys, interfaces.ActionWrite},
		{"BatchDelete", OpBatchDelete, interfaces.ResourceKeys, interfaces.ActionDelete},

		// Transaction Operations
		{"Transaction", OpTransaction, interfaces.ResourceTransactions, interfaces.ActionWrite},

		// Query Operations
		{"Query", OpQuery, interfaces.ResourceKeys, interfaces.ActionRead},
		{"ScanIndex", OpScanIndex, interfaces.ResourceKeys, interfaces.ActionRead},

		// Cluster Operations - Read
		{"ClusterStatus", OpClusterStatus, interfaces.ResourceCluster, interfaces.ActionRead},
		{"GetLeader", OpGetLeader, interfaces.ResourceCluster, interfaces.ActionRead},
		{"ListNodes", OpListNodes, interfaces.ResourceCluster, interfaces.ActionRead},

		// Cluster Operations - Admin
		{"AddNode", OpAddNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"RemoveNode", OpRemoveNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"RebalanceCluster", OpRebalanceCluster, interfaces.ResourceCluster, interfaces.ActionAdmin},

		// Barrier Operations (single dispatch opcode; sub-ops validated by handler)
		{"Barrier", OpBarrier, interfaces.ResourceBarrier, interfaces.ActionAdmin},

		// Ceremony Operations (single dispatch opcode)
		{"Ceremony", OpCeremony, interfaces.ResourceCeremony, interfaces.ActionAdmin},

		// KMS Operations
		{"KMSGetKey", OpKMSGetKey, interfaces.ResourceKMS, interfaces.ActionRead},
		{"KMSListKeys", OpKMSListKeys, interfaces.ResourceKMS, interfaces.ActionRead},
		{"KMSGenerateKey", OpKMSGenerateKey, interfaces.ResourceKMS, interfaces.ActionWrite},
		{"KMSDeleteKey", OpKMSDeleteKey, interfaces.ResourceKMS, interfaces.ActionDelete},

		// Secrets Operations
		{"SecretsGet", OpSecretsGet, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"SecretsList", OpSecretsList, interfaces.ResourceSecrets, interfaces.ActionRead},
		{"SecretsStore", OpSecretsStore, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"SecretsDelete", OpSecretsDelete, interfaces.ResourceSecrets, interfaces.ActionDelete},

		// Crypto Operations
		{"CryptoEncrypt", OpCryptoEncrypt, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"CryptoDecrypt", OpCryptoDecrypt, interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"CryptoSign", OpCryptoSign, interfaces.ResourceCrypto, interfaces.ActionWrite},

		// Time-Series Operations
		{"TSQuery", OpTSQuery, interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"TSWritePoint", OpTSWritePoint, interfaces.ResourceTimeseries, interfaces.ActionWrite},
		{"TSDeleteSeries", OpTSDeleteSeries, interfaces.ResourceTimeseries, interfaces.ActionDelete},

		// ObjectStorage Admin (0xCF) - sub-op dispatch
		{"OSAdmin", OpOSAdmin, interfaces.ResourceObjectStore, interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := getResourceAndAction(tt.opcode)
			if resource != tt.wantResource {
				t.Errorf("getResourceAndAction(%d) resource = %q, want %q", tt.opcode, resource, tt.wantResource)
			}
			if action != tt.wantAction {
				t.Errorf("getResourceAndAction(%d) action = %q, want %q", tt.opcode, action, tt.wantAction)
			}
		})
	}
}

func TestHandlerAuthorize_PublicOperationsBypass(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	// Create handler with auth that would deny everything
	mockAuthz := &mockAuthzService{
		authorizeFunc: func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{Allowed: false, Reason: "test deny"}, nil
		},
	}
	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return &auth.TokenValidationResult{Valid: true, UserID: "testuser", Username: "testuser"}, nil
		},
	}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	// Public operations should bypass auth even without token
	publicOps := []byte{OpHealth, OpLiveness, OpReadiness}
	for _, op := range publicOps {
		req := &Request{Opcode: op, Token: ""} // No token
		result := handler.authorize(context.Background(), req)
		if result != nil {
			t.Errorf("Public operation 0x%02X should bypass auth, got response with status %d", op, result.Status)
		}
	}
}

func TestHandlerAuthorize_MissingToken(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuthz := &mockAuthzService{}
	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthorizationService(mockAuthz),
	)

	// Non-public operation without token should return unauthorized
	req := &Request{Opcode: OpGet, Token: ""}
	result := handler.authorize(context.Background(), req)

	if result == nil {
		t.Fatal("Expected unauthorized response, got nil")
	}
	if result.Status != StatusUnauthorized {
		t.Errorf("Expected status %d (Unauthorized), got %d", StatusUnauthorized, result.Status)
	}
	if string(result.Payload) != "authentication required" {
		t.Errorf("Expected 'authentication required', got %q", string(result.Payload))
	}
}

func TestHandlerAuthorize_InvalidToken(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return &auth.TokenValidationResult{Valid: false}, nil
		},
	}
	mockAuthz := &mockAuthzService{}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	req := &Request{Opcode: OpGet, Token: "invalid-token"}
	result := handler.authorize(context.Background(), req)

	if result == nil {
		t.Fatal("Expected unauthorized response, got nil")
	}
	if result.Status != StatusUnauthorized {
		t.Errorf("Expected status %d (Unauthorized), got %d", StatusUnauthorized, result.Status)
	}
	if string(result.Payload) != "invalid token" {
		t.Errorf("Expected 'invalid token', got %q", string(result.Payload))
	}
}

func TestHandlerAuthorize_TokenValidationError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return nil, &testError{msg: "token expired"}
		},
	}
	mockAuthz := &mockAuthzService{}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	req := &Request{Opcode: OpGet, Token: "expired-token"}
	result := handler.authorize(context.Background(), req)

	if result == nil {
		t.Fatal("Expected unauthorized response, got nil")
	}
	if result.Status != StatusUnauthorized {
		t.Errorf("Expected status %d (Unauthorized), got %d", StatusUnauthorized, result.Status)
	}
}

func TestHandlerAuthorize_PermissionDenied(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return &auth.TokenValidationResult{
				Valid:    true,
				UserID:   "testuser",
				Username: "testuser",
				Roles:    []string{"reader"},
			}, nil
		},
	}
	mockAuthz := &mockAuthzService{
		authorizeFunc: func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{
				Allowed: false,
				Reason:  "insufficient permissions",
			}, nil
		},
	}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	req := &Request{Opcode: OpPut, Token: "valid-token"}
	result := handler.authorize(context.Background(), req)

	if result == nil {
		t.Fatal("Expected permission denied response, got nil")
	}
	if result.Status != StatusPermissionDenied {
		t.Errorf("Expected status %d (PermissionDenied), got %d", StatusPermissionDenied, result.Status)
	}
	expected := "permission denied: insufficient permissions"
	if string(result.Payload) != expected {
		t.Errorf("Expected %q, got %q", expected, string(result.Payload))
	}
}

func TestHandlerAuthorize_AuthorizationAllowed(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return &auth.TokenValidationResult{
				Valid:    true,
				UserID:   "admin",
				Username: "admin",
				Roles:    []string{"admin"},
			}, nil
		},
	}
	mockAuthz := &mockAuthzService{
		authorizeFunc: func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			// Verify subject was extracted correctly
			if req.Subject.ID != "admin" {
				t.Errorf("Expected subject ID 'admin', got %q", req.Subject.ID)
			}
			if req.Subject.Username != "admin" {
				t.Errorf("Expected subject username 'admin', got %q", req.Subject.Username)
			}
			if !req.Subject.Authenticated {
				t.Error("Expected subject to be authenticated")
			}
			return &interfaces.AuthorizationDecision{Allowed: true}, nil
		},
	}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	req := &Request{Opcode: OpPut, Token: "valid-admin-token"}
	result := handler.authorize(context.Background(), req)

	if result != nil {
		t.Errorf("Expected nil (allowed), got response with status %d", result.Status)
	}
}

func TestHandlerAuthorize_NoAuthConfigured(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	// Handler without auth services configured (backward compatibility)
	handler := NewHandler(nil, nil, nil, nil, log)

	req := &Request{Opcode: OpPut, Token: ""}
	result := handler.authorize(context.Background(), req)

	// Should allow without auth when no auth services configured
	if result != nil {
		t.Errorf("Expected nil (allowed) when no auth configured, got response with status %d", result.Status)
	}
}

func TestHandlerAuthorize_AuthzOnlyNoAuthManager(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	// Only authz service configured, no auth manager
	mockAuthz := &mockAuthzService{
		authorizeFunc: func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			// Should have a default subject with ID "unknown"
			if req.Subject.ID != "unknown" {
				t.Errorf("Expected subject ID 'unknown', got %q", req.Subject.ID)
			}
			return &interfaces.AuthorizationDecision{Allowed: true}, nil
		},
	}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthorizationService(mockAuthz),
	)

	// With only authz service, token is still required
	req := &Request{Opcode: OpPut, Token: "any-token"}
	result := handler.authorize(context.Background(), req)

	if result != nil {
		t.Errorf("Expected nil (allowed), got response with status %d", result.Status)
	}
}

func TestHandlerAuthorize_CorrectResourceActionMapping(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	tests := []struct {
		opcode       byte
		wantResource string
		wantAction   string
	}{
		{OpGet, interfaces.ResourceKeys, interfaces.ActionRead},
		{OpPut, interfaces.ResourceKeys, interfaces.ActionWrite},
		{OpDelete, interfaces.ResourceKeys, interfaces.ActionDelete},
		{OpClusterStatus, interfaces.ResourceCluster, interfaces.ActionRead},
		{OpAddNode, interfaces.ResourceCluster, interfaces.ActionAdmin},
		{OpBarrier, interfaces.ResourceBarrier, interfaces.ActionAdmin},
		{OpCeremony, interfaces.ResourceCeremony, interfaces.ActionAdmin},
		{OpKMSGetKey, interfaces.ResourceKMS, interfaces.ActionRead},
		{OpKMSGenerateKey, interfaces.ResourceKMS, interfaces.ActionWrite},
		{OpSecretsGet, interfaces.ResourceSecrets, interfaces.ActionRead},
		{OpSecretsStore, interfaces.ResourceSecrets, interfaces.ActionWrite},
		{OpCryptoEncrypt, interfaces.ResourceCrypto, interfaces.ActionWrite},
	}

	for _, tt := range tests {
		mockAuth := &mockAuthManager{
			validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
				return &auth.TokenValidationResult{Valid: true, UserID: "test", Username: "test"}, nil
			},
		}

		var capturedReq *interfaces.AuthorizationRequest
		mockAuthz := &mockAuthzService{
			authorizeFunc: func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
				capturedReq = req
				return &interfaces.AuthorizationDecision{Allowed: true}, nil
			},
		}

		handler := NewHandler(nil, nil, nil, nil, log,
			WithHandlerAuthManager(mockAuth),
			WithHandlerAuthorizationService(mockAuthz),
		)

		req := &Request{Opcode: tt.opcode, Token: "token"}
		handler.authorize(context.Background(), req)

		if capturedReq == nil {
			t.Errorf("Opcode 0x%02X: authorization request not captured", tt.opcode)
			continue
		}
		if capturedReq.Resource != tt.wantResource {
			t.Errorf("Opcode 0x%02X: resource = %q, want %q", tt.opcode, capturedReq.Resource, tt.wantResource)
		}
		if capturedReq.Action != tt.wantAction {
			t.Errorf("Opcode 0x%02X: action = %q, want %q", tt.opcode, capturedReq.Action, tt.wantAction)
		}
	}
}

func TestHandlerRequest_AuthorizationIntegration(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return &auth.TokenValidationResult{Valid: false}, nil
		},
	}
	mockAuthz := &mockAuthzService{}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	// HandleRequest should check authorization first
	req := &Request{Opcode: OpGet, Token: "invalid", Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusUnauthorized {
		t.Errorf("Expected status %d (Unauthorized), got %d", StatusUnauthorized, resp.Status)
	}
}

func TestHandlerAuthorize_AuthzErrorReturnsInternalError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return &auth.TokenValidationResult{Valid: true, UserID: "test", Username: "test"}, nil
		},
	}
	mockAuthz := &mockAuthzService{
		authorizeFunc: func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return nil, &testError{msg: "authorization service unavailable"}
		},
	}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	req := &Request{Opcode: OpGet, Token: "valid-token"}
	result := handler.authorize(context.Background(), req)

	if result == nil {
		t.Fatal("Expected error response, got nil")
	}
	if result.Status != StatusInternalError {
		t.Errorf("Expected status %d (InternalError), got %d", StatusInternalError, result.Status)
	}
	if string(result.Payload) != "authorization check failed" {
		t.Errorf("Expected 'authorization check failed', got %q", string(result.Payload))
	}
}

func TestHandlerAuthorize_PermissionDeniedDefaultReason(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "text", Output: "stdout", Component: "test"})

	mockAuth := &mockAuthManager{
		validateTokenFunc: func(ctx context.Context, token string) (interface{}, error) {
			return &auth.TokenValidationResult{Valid: true, UserID: "test", Username: "test"}, nil
		},
	}
	mockAuthz := &mockAuthzService{
		authorizeFunc: func(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
			return &interfaces.AuthorizationDecision{Allowed: false, Reason: ""}, nil
		},
	}

	handler := NewHandler(nil, nil, nil, nil, log,
		WithHandlerAuthManager(mockAuth),
		WithHandlerAuthorizationService(mockAuthz),
	)

	req := &Request{Opcode: OpPut, Token: "valid-token"}
	result := handler.authorize(context.Background(), req)

	if result == nil {
		t.Fatal("Expected permission denied response, got nil")
	}
	// Should use default reason when Reason is empty
	if string(result.Payload) != "permission denied: access denied to keys:write" {
		t.Errorf("Expected default reason, got %q", string(result.Payload))
	}
}

// testError implements error interface for tests.
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
