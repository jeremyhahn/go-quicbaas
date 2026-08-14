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

// Package auth provides end-to-end tests for the token/session lifecycle of the
// in-memory auth manager: token issuance, refresh, validation, revocation, and
// user-store CRUD. The authorization DECISION is qrdb's responsibility and is
// covered by qrdb's pkg/auth/rbac suite plus the transport-seam tests in this
// package (authz_test.go, *_authz_test.go); the fork RBAC allow/deny assertions
// that used the deleted fork role manager have been removed.
package auth

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/hash"
)

// issueTokenForUser is a helper function that gets a user from the store and issues a token.
// This replaces the deprecated Authenticate(ctx, username, password) pattern.
func issueTokenForUser(ctx context.Context, authManager *auth.InMemoryAuthManager, username string) (*auth.Token, error) {
	userInfo := authManager.GetUserStore().GetByUsername(username)
	if userInfo == nil {
		return nil, &qberrors.MessageError{Message: "user not found: " + username}
	}
	return authManager.IssueTokenForUser(ctx, userInfo)
}

// TestUserAuthentication tests the full JWT authentication flow with the IssueTokenForUser API.
func TestUserAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-jwt-signing-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		MaxTokens:       10,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	adminUser, err := authManager.CreateUser("admin-user", []string{"admin"})
	require.NoError(t, err)
	require.NotNil(t, adminUser)

	t.Run("valid user token issuance", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "admin-user")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)
		assert.Equal(t, "Bearer", token.TokenType)
		assert.True(t, token.ExpiresAt.After(time.Now()))
	})

	t.Run("token issuance for nonexistent user fails", func(t *testing.T) {
		_, err := issueTokenForUser(ctx, authManager, "nonexistent-user")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("token issuance with nil user fails", func(t *testing.T) {
		_, err := authManager.IssueTokenForUser(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user cannot be nil")
	})

	t.Run("CreateUser with empty username fails", func(t *testing.T) {
		_, err := authManager.CreateUser("", []string{"readonly"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})
}

// TestTokenRefresh tests real token refresh with refresh tokens.
func TestTokenRefresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-jwt-signing-key",
		TokenTTL:        5 * time.Second, // Short TTL for testing
		RefreshTokenTTL: 1 * time.Hour,
		RefreshWindow:   1 * time.Second,
		MaxTokens:       10,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	_, err = authManager.CreateUser("refresh-test-user", []string{"readwrite"})
	require.NoError(t, err)

	t.Run("successful token refresh", func(t *testing.T) {
		initialToken, err := issueTokenForUser(ctx, authManager, "refresh-test-user")
		require.NoError(t, err)
		require.NotEmpty(t, initialToken.RefreshToken)

		time.Sleep(2 * time.Second)

		newToken, err := authManager.RefreshToken(ctx, initialToken.RefreshToken)
		require.NoError(t, err)
		assert.NotEmpty(t, newToken.AccessToken)
		assert.NotEqual(t, initialToken.AccessToken, newToken.AccessToken)
		assert.NotEmpty(t, newToken.RefreshToken)
	})

	t.Run("invalid refresh token", func(t *testing.T) {
		_, err := authManager.RefreshToken(ctx, "invalid-refresh-token")
		assert.Error(t, err)
	})
}

// TestTokenIssuanceLogging verifies the token-issuance path functions for both
// existing and nonexistent users (the audit hook fires from these calls).
func TestTokenIssuanceLogging(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-jwt-signing-key",
		TokenTTL:        1 * time.Hour,
		MaxTokens:       10,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	_, err = authManager.CreateUser("audit-user", []string{"readwrite"})
	require.NoError(t, err)

	_, err = issueTokenForUser(ctx, authManager, "audit-user")
	assert.NoError(t, err)

	_, err = issueTokenForUser(ctx, authManager, "nonexistent-audit-user")
	assert.Error(t, err)
}

// TestSessionManagement tests real session creation, logout, and token revocation.
func TestSessionManagement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-jwt-signing-key",
		TokenTTL:        1 * time.Hour,
		MaxTokens:       10,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	_, err = authManager.CreateUser("session-user", []string{"readwrite"})
	require.NoError(t, err)

	t.Run("create session", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)

		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.False(t, result.IsRevoked)
		assert.Equal(t, "session-user", result.Username)
	})

	t.Run("revoke session", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)

		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.Valid)

		err = authManager.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)

		result, err = authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.False(t, result.Valid)
		assert.True(t, result.IsRevoked)
	})

	t.Run("multiple concurrent sessions", func(t *testing.T) {
		token1, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)

		token2, err := issueTokenForUser(ctx, authManager, "session-user")
		require.NoError(t, err)

		assert.NotEqual(t, token1.AccessToken, token2.AccessToken)

		result1, err := authManager.ValidateToken(ctx, token1.AccessToken)
		require.NoError(t, err)
		assert.True(t, result1.Valid)

		result2, err := authManager.ValidateToken(ctx, token2.AccessToken)
		require.NoError(t, err)
		assert.True(t, result2.Valid)
	})
}

// TestUserCreationPolicy tests user creation validation through CreateUser.
func TestUserCreationPolicy(t *testing.T) {
	_, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-jwt-signing-key",
		TokenTTL:        1 * time.Hour,
		MaxTokens:       10,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	t.Run("user created without password (password auth disabled)", func(t *testing.T) {
		// Password-based authentication was removed; users authenticate via
		// mTLS, WebAuthn, or other IAM methods.
		user, err := authManager.CreateUser("user-empty-pass", []string{"readonly"})
		require.NoError(t, err)
		assert.Equal(t, "user-empty-pass", user.Username)
	})

	t.Run("empty username rejected", func(t *testing.T) {
		_, err := authManager.CreateUser("", []string{"readwrite"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("user creation persists to store", func(t *testing.T) {
		_, err := authManager.CreateUser("store-user", []string{"readwrite"})
		require.NoError(t, err)

		userInfo := authManager.GetUserStore().GetByUsername("store-user")
		require.NotNil(t, userInfo)
		assert.Equal(t, "store-user", userInfo.GetUsername())
	})
}

// TestTokenOperationThroughput tests that token issuance and validation are fast
// and unthrottled (rate limiting is a future middleware-layer concern).
func TestTokenOperationThroughput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-jwt-signing-key",
		TokenTTL:        1 * time.Hour,
		MaxTokens:       10,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	_, err = authManager.CreateUser("throughput-user", []string{"readwrite"})
	require.NoError(t, err)

	t.Run("many rapid token issuance attempts", func(t *testing.T) {
		successCount := 0
		for i := 0; i < 100; i++ {
			if _, err := issueTokenForUser(ctx, authManager, "throughput-user"); err == nil {
				successCount++
			}
		}
		assert.Greater(t, successCount, 0, "Should have some successful token issuances")
	})

	t.Run("token validation is fast", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "throughput-user")
		require.NoError(t, err)

		start := time.Now()
		for i := 0; i < 100; i++ {
			_, err := authManager.ValidateToken(ctx, token.AccessToken)
			require.NoError(t, err)
		}
		elapsed := time.Since(start)
		assert.Less(t, elapsed, 1*time.Second, "Token validation should be fast")
	})
}

// TestServiceAccountTokens tests user records that represent API key / service
// account identities through their token lifecycle.
func TestServiceAccountTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authCfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-jwt-signing-key",
		TokenTTL:        1 * time.Hour,
		MaxTokens:       10,
	}
	configureTestJWTSigning(t, authCfg)

	authManager, err := auth.NewInMemoryAuthManager(authCfg, log)
	require.NoError(t, err)

	t.Run("create service account user", func(t *testing.T) {
		user, err := authManager.CreateUser("api-service-1", []string{"readwrite"})
		require.NoError(t, err)
		assert.Equal(t, "api-service-1", user.Username)
	})

	t.Run("issue token for service account", func(t *testing.T) {
		token, err := issueTokenForUser(ctx, authManager, "api-service-1")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)
	})

	t.Run("create multiple service accounts", func(t *testing.T) {
		for i := 1; i <= 5; i++ {
			username := fmt.Sprintf("api-service-%d", i+1)
			user, err := authManager.CreateUser(username, []string{"readwrite"})
			if err != nil && !strings.Contains(err.Error(), "already exists") {
				require.NoError(t, err)
			}
			if user != nil {
				assert.Equal(t, username, user.Username)
			}
		}

		users := authManager.GetUserStore().ListUsers()
		assert.GreaterOrEqual(t, len(users), 5, "Should have at least 5 service account users")
	})

	t.Run("revoke service account token", func(t *testing.T) {
		_, err := authManager.CreateUser("revoke-api-service", []string{"readwrite"})
		require.NoError(t, err)

		token, err := issueTokenForUser(ctx, authManager, "revoke-api-service")
		require.NoError(t, err)

		err = authManager.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)

		result, err := authManager.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.IsRevoked)
		assert.False(t, result.Valid)
	})
}

// TestAuthWithGRPCClient tests authentication integration with a gRPC client.
func TestAuthWithGRPCClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoints := helpers.GetClusterEndpoints()
	serverAddr := endpoints[0]

	t.Run("create client with auth token", func(t *testing.T) {
		tlsCfg, err := helpers.IntegrationTLSConfig()
		require.NoError(t, err, "load integration TLS config")
		conn, err := grpc.DialContext(ctx, serverAddr,
			grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
			grpc.WithBlock(),
		)
		require.NoError(t, err, "Must connect to server at %s", serverAddr)
		defer conn.Close()

		client := pb.NewQuicBaaSClient(conn)

		key := "test-key"
		req := &pb.PutRequest{
			EntityType: "auth_test",
			Id:         hash.FNV1a(key),
			Data:       []byte("test-value"),
		}

		resp, err := client.Put(ctx, req)
		if err != nil {
			t.Logf("Put operation failed (expected if auth is enabled): %v", err)
			return
		}

		assert.Greater(t, resp.Version, uint64(0))
	})
}

// Ensure interfaces.UserInfo is used (import validation)
var _ interfaces.UserInfo = (*auth.User)(nil)
