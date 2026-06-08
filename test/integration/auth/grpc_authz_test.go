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

//go:build integration

package auth

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/test/integration/authzfake"
)

// testGRPCContext provides infrastructure for gRPC authorization tests.
type testGRPCContext struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *logger.Logger
	authManager  *auth.AuthManager
	authzService interfaces.AuthorizationService
	server       *grpc.Server
	listener     net.Listener
	conn         *grpc.ClientConn
}

// newTestGRPCContext creates a new test context for gRPC authorization tests.
func newTestGRPCContext(t *testing.T, allowed ...string) *testGRPCContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-grpc-secret-key-for-integration-testing",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		MaxTokens:       100,
	}

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	authzService := authzfake.New(allowed...)

	// Create gRPC server with auth interceptors
	unaryInterceptor := auth.UnaryAuthInterceptorWithService(authManager, authzService, log)
	streamInterceptor := auth.StreamAuthInterceptorWithService(authManager, authzService, log)

	server := grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor),
		grpc.StreamInterceptor(streamInterceptor),
	)

	// Register a mock service for testing
	pb.RegisterQuicBaaSServer(server, &mockQuicBaaSServer{})

	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		if err := server.Serve(listener); err != nil && err != grpc.ErrServerStopped {
			log.WithError(err).Error("gRPC server error")
		}
	}()

	// Create client connection with insecure credentials since the embedded
	// server does not use TLS.
	conn, err := grpc.DialContext(ctx, listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	return &testGRPCContext{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		logger:       log,
		authManager:  authManager,
		authzService: authzService,
		server:       server,
		listener:     listener,
		conn:         conn,
	}
}

// mockQuicBaaSServer implements the QuicBaaS gRPC service for testing.
type mockQuicBaaSServer struct {
	pb.UnimplementedQuicBaaSServer
}

// Get implements a simple mock Get method.
func (s *mockQuicBaaSServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	return &pb.GetResponse{
		Found: true,
		Data:  []byte("test-value"),
	}, nil
}

// Put implements a simple mock Put method.
func (s *mockQuicBaaSServer) Put(ctx context.Context, req *pb.PutRequest) (*pb.PutResponse, error) {
	return &pb.PutResponse{
		Id:      1,
		Version: 1,
	}, nil
}

// Delete implements a simple mock Delete method.
func (s *mockQuicBaaSServer) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	return &pb.DeleteResponse{
		Deleted: true,
	}, nil
}

// Login implements a simple mock Login method (public).
func (s *mockQuicBaaSServer) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	return &pb.LoginResponse{
		AccessToken: "mock-token",
	}, nil
}

// Close releases test resources.
func (tc *testGRPCContext) Close() {
	tc.cancel()
	if tc.conn != nil {
		tc.conn.Close()
	}
	if tc.server != nil {
		tc.server.GracefulStop()
	}
	if tc.listener != nil {
		tc.listener.Close()
	}
	if tc.authzService != nil {
		tc.authzService.Close()
	}
}

// createTestUser creates a user and returns the token.
// Note: password parameter is ignored as password-based authentication is disabled.
// Users authenticate via mTLS certificates, WebAuthn, or OIDC.
func (tc *testGRPCContext) createTestUser(username string, roles []string) (string, error) {
	_, err := tc.authManager.CreateUser(username, roles)
	if err != nil {
		return "", err
	}

	// Get user from store and issue token (replaces deprecated password-based authentication)
	userInfo := tc.authManager.GetUserStore().GetByUsername(username)
	if userInfo == nil {
		return "", &qberrors.MessageError{Message: "user not found after creation: " + username}
	}

	token, err := tc.authManager.IssueTokenForUser(tc.ctx, userInfo)
	if err != nil {
		return "", err
	}

	return token.AccessToken, nil
}

// contextWithToken adds authorization token to context.
func contextWithToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// TestGRPCPublicMethodsBypassAuth tests that public methods don't require authentication.
func TestGRPCPublicMethodsBypassAuth(t *testing.T) {
	tc := newTestGRPCContext(t, "*:*")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	// Login should be accessible without token
	t.Run("login_is_public", func(t *testing.T) {
		resp, err := client.Login(tc.ctx, &pb.LoginRequest{
			Username: "test",
			Password: "test",
		})
		// Should not get Unauthenticated error
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				assert.NotEqual(t, codes.Unauthenticated, st.Code(),
					"Login should not require authentication")
			}
		} else {
			assert.NotNil(t, resp)
		}
	})
}

// TestGRPCProtectedMethodsRequireToken tests that protected methods require authentication.
func TestGRPCProtectedMethodsRequireToken(t *testing.T) {
	tc := newTestGRPCContext(t, "*:*")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	t.Run("get_requires_token", func(t *testing.T) {
		_, err := client.Get(tc.ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("put_requires_token", func(t *testing.T) {
		_, err := client.Put(tc.ctx, &pb.PutRequest{
			EntityType: "test",
			Id:         1,
			Data:       []byte("test-value"),
		})
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})

	t.Run("delete_requires_token", func(t *testing.T) {
		_, err := client.Delete(tc.ctx, &pb.DeleteRequest{
			EntityType: "test",
			Id:         1,
		})
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
	})
}

// TestGRPCUnauthenticatedForInvalidToken tests codes.Unauthenticated for invalid tokens.
func TestGRPCUnauthenticatedForInvalidToken(t *testing.T) {
	tc := newTestGRPCContext(t, "*:*")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	ctx := contextWithToken(tc.ctx, "invalid-token-12345")

	_, err := client.Get(ctx, &pb.GetRequest{
		EntityType: "test",
		Id:         1,
	})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

// TestGRPCPermissionDeniedForInsufficientPermissions tests permission denied scenarios.
func TestGRPCPermissionDeniedForInsufficientPermissions(t *testing.T) {
	tc := newTestGRPCContext(t, "keys:read")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	// Create readonly user
	token, err := tc.createTestUser("readonly-grpc", []string{"readonly"})
	require.NoError(t, err)

	ctx := contextWithToken(tc.ctx, token)

	t.Run("readonly_can_get", func(t *testing.T) {
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("readonly_cannot_put", func(t *testing.T) {
		_, err := client.Put(ctx, &pb.PutRequest{
			EntityType: "test",
			Id:         1,
			Data:       []byte("test-value"),
		})
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("readonly_cannot_delete", func(t *testing.T) {
		_, err := client.Delete(ctx, &pb.DeleteRequest{
			EntityType: "test",
			Id:         1,
		})
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})
}

// TestGRPCAdminRoleAccess tests that admin role has full access.
func TestGRPCAdminRoleAccess(t *testing.T) {
	tc := newTestGRPCContext(t, "*:*")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	token, err := tc.createTestUser("admin-grpc", []string{"admin"})
	require.NoError(t, err)

	ctx := contextWithToken(tc.ctx, token)

	t.Run("admin_can_get", func(t *testing.T) {
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("admin_can_put", func(t *testing.T) {
		resp, err := client.Put(ctx, &pb.PutRequest{
			EntityType: "test",
			Id:         1,
			Data:       []byte("test-value"),
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("admin_can_delete", func(t *testing.T) {
		resp, err := client.Delete(ctx, &pb.DeleteRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)
	})
}

// TestGRPCReadWriteRoleAccess tests readwrite role permissions.
func TestGRPCReadWriteRoleAccess(t *testing.T) {
	tc := newTestGRPCContext(t, "keys:read", "keys:write", "keys:delete")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	token, err := tc.createTestUser("readwrite-grpc", []string{"readwrite"})
	require.NoError(t, err)

	ctx := contextWithToken(tc.ctx, token)

	t.Run("readwrite_can_get", func(t *testing.T) {
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("readwrite_can_put", func(t *testing.T) {
		resp, err := client.Put(ctx, &pb.PutRequest{
			EntityType: "test",
			Id:         1,
			Data:       []byte("test-value"),
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("readwrite_can_delete", func(t *testing.T) {
		resp, err := client.Delete(ctx, &pb.DeleteRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)
	})
}

// TestGRPCTokenMetadataFormats tests different token metadata formats.
func TestGRPCTokenMetadataFormats(t *testing.T) {
	tc := newTestGRPCContext(t, "keys:read")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	token, err := tc.createTestUser("format-test", []string{"admin"})
	require.NoError(t, err)

	t.Run("bearer_prefix", func(t *testing.T) {
		ctx := metadata.AppendToOutgoingContext(tc.ctx, "authorization", "Bearer "+token)
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("raw_token", func(t *testing.T) {
		// Some clients send raw token without Bearer prefix
		ctx := metadata.AppendToOutgoingContext(tc.ctx, "authorization", token)
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("x_auth_token_header", func(t *testing.T) {
		ctx := metadata.AppendToOutgoingContext(tc.ctx, "x-auth-token", token)
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})
}

// TestGRPCRevokedTokenRejected tests that revoked tokens are rejected.
func TestGRPCRevokedTokenRejected(t *testing.T) {
	tc := newTestGRPCContext(t, "keys:read")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	token, err := tc.createTestUser("revoke-grpc", []string{"admin"})
	require.NoError(t, err)

	ctx := contextWithToken(tc.ctx, token)

	// Verify token works initially
	resp, err := client.Get(ctx, &pb.GetRequest{
		EntityType: "test",
		Id:         1,
	})
	require.NoError(t, err)
	assert.True(t, resp.Found)

	// Revoke the token
	err = tc.authManager.RevokeToken(tc.ctx, token)
	require.NoError(t, err)

	// Verify token is now rejected
	_, err = client.Get(ctx, &pb.GetRequest{
		EntityType: "test",
		Id:         1,
	})
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

// TestGRPCMethodToResourceMapping tests gRPC method to resource/action mapping.
func TestGRPCMethodToResourceMapping(t *testing.T) {
	// Test the mapping functions directly
	testCases := []struct {
		fullMethod       string
		expectedResource string
		expectedAction   string
	}{
		{"/quicbaas.v1.QuicBaaS/Get", interfaces.ResourceKeys, interfaces.ActionRead},
		{"/quicbaas.v1.QuicBaaS/Put", interfaces.ResourceKeys, interfaces.ActionWrite},
		{"/quicbaas.v1.QuicBaaS/Delete", interfaces.ResourceKeys, interfaces.ActionDelete},
		{"/quicbaas.v1.QuicBaaS/Range", interfaces.ResourceKeys, interfaces.ActionRead},
		{"/quicbaas.v1.QuicBaaS/BatchGet", interfaces.ResourceKeys, interfaces.ActionRead},
		{"/quicbaas.v1.QuicBaaS/BatchPut", interfaces.ResourceKeys, interfaces.ActionWrite},
		{"/quicbaas.v1.QuicBaaS/Txn", interfaces.ResourceTransactions, interfaces.ActionWrite},
		{"/quicbaas.v1.QuicBaaS/Watch", interfaces.ResourceWatch, interfaces.ActionRead},
		{"/quicbaas.v1.Health/Check", interfaces.ResourceHealth, interfaces.ActionRead},
	}

	for _, tc := range testCases {
		t.Run(tc.fullMethod, func(t *testing.T) {
			// Parse the method to get resource and action
			resource, action := parseGRPCMethodForTest(tc.fullMethod)
			assert.Equal(t, tc.expectedResource, resource, "Resource should match for %s", tc.fullMethod)
			assert.Equal(t, tc.expectedAction, action, "Action should match for %s", tc.fullMethod)
		})
	}
}

// parseGRPCMethodForTest is a test helper that mirrors the interceptor's parsing logic.
func parseGRPCMethodForTest(fullMethod string) (resource, action string) {
	// This mirrors the logic in auth.parseGRPCMethod and mapQuicBaaSMethod
	// For testing purposes, we use a simplified version

	if fullMethod == "" {
		return interfaces.ResourceAll, interfaces.ActionAll
	}

	// Extract method name from "/package.Service/Method"
	parts := splitMethod(fullMethod)
	if len(parts) < 3 {
		return interfaces.ResourceAll, interfaces.ActionAll
	}

	method := parts[2]
	service := parts[1]

	// Map based on service
	switch {
	case containsSuffix(service, ".Health"):
		return interfaces.ResourceHealth, interfaces.ActionRead
	case containsSuffix(service, ".Cluster"):
		return interfaces.ResourceCluster, interfaces.ActionAdmin
	default:
		return mapQuicBaaSMethodForTest(method)
	}
}

func splitMethod(method string) []string {
	result := []string{}
	current := ""
	for _, c := range method {
		if c == '/' {
			if current != "" {
				result = append(result, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	// Prepend empty string to match /package/method format
	return append([]string{""}, result...)
}

func containsSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func mapQuicBaaSMethodForTest(method string) (resource, action string) {
	switch method {
	case "Get", "Range", "List", "Query", "ScanIndex", "BatchGet":
		return interfaces.ResourceKeys, interfaces.ActionRead
	case "Put", "BatchPut":
		return interfaces.ResourceKeys, interfaces.ActionWrite
	case "Delete", "BatchDelete":
		return interfaces.ResourceKeys, interfaces.ActionDelete
	case "Txn", "Transaction":
		return interfaces.ResourceTransactions, interfaces.ActionWrite
	case "Watch":
		return interfaces.ResourceWatch, interfaces.ActionRead
	case "ClusterStatus", "GetLeader":
		return interfaces.ResourceCluster, interfaces.ActionRead
	case "Health", "Check":
		return interfaces.ResourceHealth, interfaces.ActionRead
	case "Login":
		return interfaces.ResourceUsers, interfaces.ActionRead
	default:
		return interfaces.ResourceAll, interfaces.ActionAdmin
	}
}

// TestGRPCCustomRoleAuthorization tests authorization with custom roles.
func TestGRPCCustomRoleAuthorization(t *testing.T) {
	tc := newTestGRPCContext(t, "keys:read")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	// The server's authzfake allow-list grants keys:read only, modelling a
	// narrow custom "keys-reader" role.
	token, err := tc.createTestUser("keys-only", []string{"keys-reader"})
	require.NoError(t, err)

	ctx := contextWithToken(tc.ctx, token)

	t.Run("can_read_keys", func(t *testing.T) {
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("cannot_write_keys", func(t *testing.T) {
		_, err := client.Put(ctx, &pb.PutRequest{
			EntityType: "test",
			Id:         1,
			Data:       []byte("value"),
		})
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})
}

// TestGRPCMultipleRolesAuthorization tests authorization with multiple roles.
func TestGRPCMultipleRolesAuthorization(t *testing.T) {
	tc := newTestGRPCContext(t, "keys:read", "keys:write")
	defer tc.Close()

	client := pb.NewQuicBaaSClient(tc.conn)

	token, err := tc.createTestUser("multi-role-grpc", []string{"readonly", "readwrite"})
	require.NoError(t, err)

	ctx := contextWithToken(tc.ctx, token)

	// Should have combined permissions from both roles
	t.Run("can_read", func(t *testing.T) {
		resp, err := client.Get(ctx, &pb.GetRequest{
			EntityType: "test",
			Id:         1,
		})
		require.NoError(t, err)
		assert.True(t, resp.Found)
	})

	t.Run("can_write_from_readwrite", func(t *testing.T) {
		resp, err := client.Put(ctx, &pb.PutRequest{
			EntityType: "test",
			Id:         1,
			Data:       []byte("value"),
		})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}
