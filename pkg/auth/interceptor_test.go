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

// Package auth provides gRPC interceptor tests
package auth

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth/providers"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestUnaryAuthInterceptor(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create test user and generate token directly (password auth is disabled)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	// Get auth token via generateTokens
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	interceptor := UnaryAuthInterceptor(am, log)

	tests := []struct {
		name         string
		setupCtx     func() context.Context
		method       string
		expectError  bool
		expectedCode codes.Code
	}{
		{
			name: "valid token for read operation",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/Get",
			expectError: false,
		},
		{
			name: "valid token for write operation",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/Put",
			expectError: false,
		},
		{
			name: "missing authorization header",
			setupCtx: func() context.Context {
				return metadata.NewIncomingContext(context.Background(), metadata.MD{})
			},
			method:       "/quicbaas.v1.QuicBaaS/Get",
			expectError:  true,
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "invalid token",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer invalid-token")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:       "/quicbaas.v1.QuicBaaS/Get",
			expectError:  true,
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "token without Bearer prefix",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/Get",
			expectError: false,
		},
		{
			name: "x-auth-token header",
			setupCtx: func() context.Context {
				md := metadata.Pairs("x-auth-token", token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/Get",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()

			info := &grpc.UnaryServerInfo{
				FullMethod: tt.method,
			}

			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return "success", nil
			}

			_, err := interceptor(ctx, nil, info, handler)

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedCode != 0 {
					st, ok := status.FromError(err)
					assert.True(t, ok)
					assert.Equal(t, tt.expectedCode, st.Code())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUnaryAuthInterceptorWithReadOnlyUser(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create readonly user and generate token directly (password auth is disabled)
	user, err := am.CreateUser("readonly", "", []string{RoleNameReadOnly})
	require.NoError(t, err)

	token, err := am.generateTokens(user)
	require.NoError(t, err)

	interceptor := UnaryAuthInterceptor(am, log)

	tests := []struct {
		name        string
		method      string
		expectError bool
	}{
		{
			name:        "readonly can read",
			method:      "/quicbaas.v1.QuicBaaS/Get",
			expectError: false,
		},
		{
			name:        "readonly cannot write",
			method:      "/quicbaas.v1.QuicBaaS/Put",
			expectError: true,
		},
		{
			name:        "readonly cannot delete",
			method:      "/quicbaas.v1.QuicBaaS/Delete",
			expectError: true,
		},
		{
			name:        "readonly can query",
			method:      "/quicbaas.v1.QuicBaaS/Query",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
			ctx := metadata.NewIncomingContext(context.Background(), md)

			info := &grpc.UnaryServerInfo{
				FullMethod: tt.method,
			}

			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return "success", nil
			}

			_, err := interceptor(ctx, nil, info, handler)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStreamAuthInterceptor(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create test user and generate token directly (password auth is disabled)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	token, err := am.generateTokens(user)
	require.NoError(t, err)

	interceptor := StreamAuthInterceptor(am, log)

	tests := []struct {
		name        string
		setupCtx    func() context.Context
		method      string
		expectError bool
	}{
		{
			name: "valid token for stream",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/List",
			expectError: false,
		},
		{
			name: "missing token for stream",
			setupCtx: func() context.Context {
				return metadata.NewIncomingContext(context.Background(), metadata.MD{})
			},
			method:      "/quicbaas.v1.QuicBaaS/List",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()

			info := &grpc.StreamServerInfo{
				FullMethod: tt.method,
			}

			stream := &mockServerStream{ctx: ctx}

			handler := func(srv interface{}, stream grpc.ServerStream) error {
				return nil
			}

			err := interceptor(nil, stream, info, handler)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseGRPCMethod(t *testing.T) {
	tests := []struct {
		method           string
		expectedResource string
		expectedAction   string
	}{
		{
			method:           "/quicbaas.v1.QuicBaaS/Get",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/Put",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/Delete",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionDelete,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/List",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/Query",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/Transaction",
			expectedResource: interfaces.ResourceTransactions,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/Txn",
			expectedResource: interfaces.ResourceTransactions,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/ClusterStatus",
			expectedResource: interfaces.ResourceCluster,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/Health",
			expectedResource: interfaces.ResourceHealth,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/BatchGet",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/BatchPut",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/Range",
			expectedResource: interfaces.ResourceKeys,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.QuicBaaS/UnknownMethod",
			expectedResource: interfaces.ResourceAll,
			expectedAction:   interfaces.ActionAdmin,
		},
		{
			method:           "invalid-method",
			expectedResource: ResourceAll,
			expectedAction:   ActionAll,
		},
		// Service-specific tests
		{
			method:           "/quicbaas.v1.Cluster/GetMembers",
			expectedResource: interfaces.ResourceCluster,
			expectedAction:   interfaces.ActionAdmin,
		},
		{
			method:           "/quicbaas.v1.KMS/GetKey",
			expectedResource: interfaces.ResourceKMS,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.KMS/CreateKey",
			expectedResource: interfaces.ResourceKMS,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			method:           "/quicbaas.v1.KMS/DeleteKey",
			expectedResource: interfaces.ResourceKMS,
			expectedAction:   interfaces.ActionDelete,
		},
		{
			method:           "/quicbaas.v1.Secrets/GetSecret",
			expectedResource: interfaces.ResourceSecrets,
			expectedAction:   interfaces.ActionRead,
		},
		{
			method:           "/quicbaas.v1.Secrets/SetSecret",
			expectedResource: interfaces.ResourceSecrets,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			method:           "/quicbaas.v1.Crypto/Encrypt",
			expectedResource: interfaces.ResourceCrypto,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			method:           "/quicbaas.v1.Seal/Initialize",
			expectedResource: interfaces.ResourceSeal,
			expectedAction:   interfaces.ActionAdmin,
		},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			resource, action := parseGRPCMethod(tt.method)
			assert.Equal(t, tt.expectedResource, resource)
			assert.Equal(t, tt.expectedAction, action)
		})
	}
}

func TestExtractToken(t *testing.T) {
	tests := []struct {
		name          string
		setupCtx      func() context.Context
		expectedToken string
		expectError   bool
	}{
		{
			name: "authorization header with Bearer",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer test-token-123")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedToken: "test-token-123",
			expectError:   false,
		},
		{
			name: "authorization header without Bearer",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "test-token-123")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedToken: "test-token-123",
			expectError:   false,
		},
		{
			name: "x-auth-token header",
			setupCtx: func() context.Context {
				md := metadata.Pairs("x-auth-token", "test-token-123")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedToken: "test-token-123",
			expectError:   false,
		},
		{
			name: "missing metadata",
			setupCtx: func() context.Context {
				return context.Background()
			},
			expectError: true,
		},
		{
			name: "missing token",
			setupCtx: func() context.Context {
				return metadata.NewIncomingContext(context.Background(), metadata.MD{})
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()
			token, err := extractToken(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedToken, token)
			}
		})
	}
}

func TestAddTokenToContext(t *testing.T) {
	tests := []struct {
		name          string
		token         string
		expectedValue string
	}{
		{
			name:          "token without Bearer prefix",
			token:         "test-token-123",
			expectedValue: "Bearer test-token-123",
		},
		{
			name:          "token with Bearer prefix",
			token:         "Bearer test-token-123",
			expectedValue: "Bearer test-token-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx := AddTokenToContext(ctx, tt.token)

			md, ok := metadata.FromOutgoingContext(newCtx)
			assert.True(t, ok)

			values := md.Get("authorization")
			assert.Len(t, values, 1)
			assert.Equal(t, tt.expectedValue, values[0])
		})
	}
}

func TestAuthInterceptorDisabled(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled: false, // Auth disabled
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	interceptor := UnaryAuthInterceptor(am, log)

	// Should succeed even without token when auth is disabled
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.v1.QuicBaaS/Get",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	_, err = interceptor(ctx, nil, info, handler)
	assert.NoError(t, err)
}

// TestUnaryAuthInterceptorWithAuthorizationService tests the new AuthorizationService integration.
func TestUnaryAuthInterceptorWithAuthorizationService(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create test user with readwrite role and generate token directly (password auth is disabled)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	// Get auth token via generateTokens
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Create RBAC provider and AuthorizationService
	rbacProvider, err := providers.NewRBACProvider(am.GetRoleManager(), log)
	require.NoError(t, err)

	authzService, err := NewAuthorizationService(rbacProvider, log)
	require.NoError(t, err)
	defer authzService.Close()

	// Create interceptor with AuthorizationService
	interceptor := UnaryAuthInterceptorWithService(am, authzService, log)

	tests := []struct {
		name         string
		setupCtx     func() context.Context
		method       string
		expectError  bool
		expectedCode codes.Code
	}{
		{
			name: "valid token with authz service - read allowed",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/Get",
			expectError: false,
		},
		{
			name: "valid token with authz service - write allowed",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/Put",
			expectError: false,
		},
		{
			name: "missing token with authz service",
			setupCtx: func() context.Context {
				return metadata.NewIncomingContext(context.Background(), metadata.MD{})
			},
			method:       "/quicbaas.v1.QuicBaaS/Get",
			expectError:  true,
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "invalid token with authz service",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer invalid-token")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:       "/quicbaas.v1.QuicBaaS/Get",
			expectError:  true,
			expectedCode: codes.Unauthenticated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()

			info := &grpc.UnaryServerInfo{
				FullMethod: tt.method,
			}

			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return "success", nil
			}

			_, err := interceptor(ctx, nil, info, handler)

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedCode != 0 {
					st, ok := status.FromError(err)
					assert.True(t, ok, "expected gRPC status error")
					assert.Equal(t, tt.expectedCode, st.Code())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestStreamAuthInterceptorWithAuthorizationService tests stream interceptor with AuthorizationService.
func TestStreamAuthInterceptorWithAuthorizationService(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create test user and generate token directly (password auth is disabled)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Create RBAC provider and AuthorizationService
	rbacProvider, err := providers.NewRBACProvider(am.GetRoleManager(), log)
	require.NoError(t, err)

	authzService, err := NewAuthorizationService(rbacProvider, log)
	require.NoError(t, err)
	defer authzService.Close()

	interceptor := StreamAuthInterceptorWithService(am, authzService, log)

	tests := []struct {
		name         string
		setupCtx     func() context.Context
		method       string
		expectError  bool
		expectedCode codes.Code
	}{
		{
			name: "valid token for stream with authz service",
			setupCtx: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
				return metadata.NewIncomingContext(context.Background(), md)
			},
			method:      "/quicbaas.v1.QuicBaaS/List",
			expectError: false,
		},
		{
			name: "missing token for stream with authz service",
			setupCtx: func() context.Context {
				return metadata.NewIncomingContext(context.Background(), metadata.MD{})
			},
			method:       "/quicbaas.v1.QuicBaaS/List",
			expectError:  true,
			expectedCode: codes.Unauthenticated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()

			info := &grpc.StreamServerInfo{
				FullMethod: tt.method,
			}

			stream := &mockServerStream{ctx: ctx}

			handler := func(srv interface{}, stream grpc.ServerStream) error {
				return nil
			}

			err := interceptor(nil, stream, info, handler)

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedCode != 0 {
					st, ok := status.FromError(err)
					assert.True(t, ok, "expected gRPC status error")
					assert.Equal(t, tt.expectedCode, st.Code())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPublicMethodsBypass tests that public methods skip authentication.
func TestPublicMethodsBypass(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create interceptor (no users created, so any auth attempt would fail)
	interceptor := UnaryAuthInterceptor(am, log)

	publicMethods := []string{
		"/quicbaas.v1.QuicBaaS/Login",
		"/quicbaas.v1.Health/Check",
		"/grpc.health.v1.Health/Check",
		"/grpc.health.v1.Health/Watch",
	}

	for _, method := range publicMethods {
		t.Run(method, func(t *testing.T) {
			// Use empty metadata - no token provided
			ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})

			info := &grpc.UnaryServerInfo{
				FullMethod: method,
			}

			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return "success", nil
			}

			result, err := interceptor(ctx, nil, info, handler)
			assert.NoError(t, err, "public method %s should not require auth", method)
			assert.Equal(t, "success", result)
		})
	}
}

// TestCustomPublicMethods tests adding custom public methods.
func TestCustomPublicMethods(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create interceptor with custom public methods
	interceptor := UnaryAuthInterceptorWithConfig(&InterceptorConfig{
		AuthManager:   am,
		Logger:        log,
		PublicMethods: []string{"/custom.Service/PublicMethod"},
	})

	// Test custom public method bypasses auth
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})

	info := &grpc.UnaryServerInfo{
		FullMethod: "/custom.Service/PublicMethod",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	result, err := interceptor(ctx, nil, info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "success", result)

	// Test non-public method still requires auth
	info2 := &grpc.UnaryServerInfo{
		FullMethod: "/custom.Service/PrivateMethod",
	}

	_, err = interceptor(ctx, nil, info2, handler)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

// TestPermissionDeniedWithAuthzService tests that permission denied errors use correct code.
func TestPermissionDeniedWithAuthzService(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create readonly user and generate token directly (password auth is disabled)
	user, err := am.CreateUser("readonly", "", []string{RoleNameReadOnly})
	require.NoError(t, err)

	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Create RBAC provider and AuthorizationService
	rbacProvider, err := providers.NewRBACProvider(am.GetRoleManager(), log)
	require.NoError(t, err)

	authzService, err := NewAuthorizationService(rbacProvider, log)
	require.NoError(t, err)
	defer authzService.Close()

	interceptor := UnaryAuthInterceptorWithService(am, authzService, log)

	// Test that write operation is denied with PermissionDenied code
	md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.v1.QuicBaaS/Put",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	_, err = interceptor(ctx, nil, info, handler)
	assert.Error(t, err)

	st, ok := status.FromError(err)
	assert.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

// TestBackwardCompatibility tests that existing callers without AuthorizationService still work.
func TestBackwardCompatibility(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create test user and generate token directly (password auth is disabled)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Use legacy function signature (no AuthorizationService)
	interceptor := UnaryAuthInterceptor(am, log)

	md := metadata.Pairs("authorization", "Bearer "+token.AccessToken)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	info := &grpc.UnaryServerInfo{
		FullMethod: "/quicbaas.v1.QuicBaaS/Get",
	}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	result, err := interceptor(ctx, nil, info, handler)
	assert.NoError(t, err)
	assert.Equal(t, "success", result)
}

// TestBuildSubjectFromToken tests subject creation from token.
func TestBuildSubjectFromToken(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create test user and generate token directly (password auth is disabled)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite, RoleNameAdmin})
	require.NoError(t, err)

	ctx := context.Background()
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	tests := []struct {
		name              string
		token             string
		expectError       bool
		expectAuth        bool
		expectedUsername  string
		expectedRoleCount int
	}{
		{
			name:              "valid token",
			token:             token.AccessToken,
			expectError:       false,
			expectAuth:        true,
			expectedUsername:  "testuser",
			expectedRoleCount: 2,
		},
		{
			name:              "empty token (anonymous)",
			token:             "",
			expectError:       false,
			expectAuth:        false,
			expectedUsername:  "anonymous",
			expectedRoleCount: 0,
		},
		{
			name:        "invalid token",
			token:       "invalid-token",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, err := buildSubjectFromToken(ctx, am, tt.token)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, subject)
			assert.Equal(t, tt.expectAuth, subject.Authenticated)
			assert.Equal(t, tt.expectedUsername, subject.Username)
			assert.Len(t, subject.Roles, tt.expectedRoleCount)
		})
	}
}

// TestIsPublicMethod tests the public method detection.
func TestIsPublicMethod(t *testing.T) {
	publicMethods := map[string]bool{
		"/quicbaas.v1.QuicBaaS/Login": true,
		"/grpc.health.v1.Health/*":    true,
	}

	tests := []struct {
		method   string
		expected bool
	}{
		{"/quicbaas.v1.QuicBaaS/Login", true},
		{"/quicbaas.v1.QuicBaaS/Get", false},
		{"/grpc.health.v1.Health/Check", true},
		{"/grpc.health.v1.Health/Watch", true},
		{"/grpc.health.v1.Health/AnyMethod", true},
		{"/other.Service/Method", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := isPublicMethod(tt.method, publicMethods)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMapMethodToResourceAction tests the detailed method mapping.
func TestMapMethodToResourceAction(t *testing.T) {
	tests := []struct {
		service          string
		method           string
		expectedResource string
		expectedAction   string
	}{
		// KMS methods
		{"quicbaas.v1.KMS", "GetKey", interfaces.ResourceKMS, interfaces.ActionRead},
		{"quicbaas.v1.KMS", "ListKeys", interfaces.ResourceKMS, interfaces.ActionRead},
		{"quicbaas.v1.KMS", "CreateKey", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"quicbaas.v1.KMS", "DeleteKey", interfaces.ResourceKMS, interfaces.ActionDelete},
		{"quicbaas.v1.KMS", "EnableKey", interfaces.ResourceKMS, interfaces.ActionAdmin},

		// Secrets methods
		{"quicbaas.v1.Secrets", "GetSecret", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"quicbaas.v1.Secrets", "SetSecret", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"quicbaas.v1.Secrets", "DeleteSecret", interfaces.ResourceSecrets, interfaces.ActionDelete},

		// Crypto methods
		{"quicbaas.v1.Crypto", "Encrypt", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas.v1.Crypto", "Decrypt", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas.v1.Crypto", "GenerateRandom", interfaces.ResourceCrypto, interfaces.ActionRead},

		// TimeSeries methods
		{"quicbaas.v1.TimeSeries", "Query", interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"quicbaas.v1.TimeSeries", "Write", interfaces.ResourceTimeseries, interfaces.ActionWrite},
		{"quicbaas.v1.TimeSeries", "Delete", interfaces.ResourceTimeseries, interfaces.ActionDelete},

		// Vector methods - use fine-grained vector-specific actions
		{"quicbaas.v1.Vector", "Search", interfaces.ResourceVector, interfaces.ActionVectorSearch},
		{"quicbaas.v1.Vector", "Upsert", interfaces.ResourceVector, interfaces.ActionVectorWrite},
		{"quicbaas.v1.Vector", "Delete", interfaces.ResourceVector, interfaces.ActionVectorDelete},

		// Cluster and Seal
		{"quicbaas.v1.Cluster", "GetMembers", interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"quicbaas.v1.Seal", "Initialize", interfaces.ResourceSeal, interfaces.ActionAdmin},
	}

	for _, tt := range tests {
		name := tt.service + "/" + tt.method
		t.Run(name, func(t *testing.T) {
			resource, action := mapMethodToResourceAction(tt.service, tt.method)
			assert.Equal(t, tt.expectedResource, resource)
			assert.Equal(t, tt.expectedAction, action)
		})
	}
}

// mockServerStream implements grpc.ServerStream for testing.
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func (m *mockServerStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *mockServerStream) RecvMsg(msg interface{}) error {
	return nil
}

func TestExtractTokenFromContext(t *testing.T) {
	tests := []struct {
		name          string
		setupContext  func() context.Context
		expectedToken string
		expectError   bool
	}{
		{
			name: "incoming context with Bearer authorization",
			setupContext: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer test-token-123")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedToken: "test-token-123",
			expectError:   false,
		},
		{
			name: "incoming context without Bearer prefix",
			setupContext: func() context.Context {
				md := metadata.Pairs("authorization", "raw-token-456")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedToken: "raw-token-456",
			expectError:   false,
		},
		{
			name: "incoming context with x-auth-token",
			setupContext: func() context.Context {
				md := metadata.Pairs("x-auth-token", "x-auth-token-789")
				return metadata.NewIncomingContext(context.Background(), md)
			},
			expectedToken: "x-auth-token-789",
			expectError:   false,
		},
		{
			name: "outgoing context with Bearer authorization",
			setupContext: func() context.Context {
				md := metadata.Pairs("authorization", "Bearer outgoing-token")
				return metadata.NewOutgoingContext(context.Background(), md)
			},
			expectedToken: "outgoing-token",
			expectError:   false,
		},
		{
			name: "outgoing context with x-auth-token",
			setupContext: func() context.Context {
				md := metadata.Pairs("x-auth-token", "outgoing-x-token")
				return metadata.NewOutgoingContext(context.Background(), md)
			},
			expectedToken: "outgoing-x-token",
			expectError:   false,
		},
		{
			name: "empty context returns empty token",
			setupContext: func() context.Context {
				return context.Background()
			},
			expectedToken: "",
			expectError:   false,
		},
		{
			name: "context with empty metadata returns empty token",
			setupContext: func() context.Context {
				return metadata.NewIncomingContext(context.Background(), metadata.MD{})
			},
			expectedToken: "",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			token, err := ExtractTokenFromContext(ctx)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedToken, token)
		})
	}
}
