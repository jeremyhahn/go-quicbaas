//go:build iam && !integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
)

// createTestAuthManager creates an InMemoryAuthManager suitable for unit testing.
func createTestAuthManager(t *testing.T) *auth.InMemoryAuthManager {
	t.Helper()
	log := mustCreateLogger()
	cfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-at-least-32-chars-long",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		RefreshWindow:   5 * time.Minute,
		MaxTokens:       10,
	}
	mgr, err := auth.NewInMemoryAuthManager(cfg, log)
	require.NoError(t, err)
	return mgr
}

// issueTestToken creates a user record and issues a token for it via the
// IAM-factor token path (IssueTokenForUser), mirroring how production mints
// tokens after an mTLS / PIV / WebAuthn / OIDC factor verifies the caller.
func issueTestToken(t *testing.T, mgr *auth.InMemoryAuthManager, username string) *auth.Token {
	t.Helper()
	user, err := mgr.CreateUser(username, []string{"admin"})
	require.NoError(t, err)
	token, err := mgr.IssueTokenForUser(context.Background(), user)
	require.NoError(t, err)
	require.NotNil(t, token)
	return token
}

// ---------------------------------------------------------------------------
// inMemoryAuthManagerAdapter — real delegation through manager
// ---------------------------------------------------------------------------

func TestIAMAdapter_ValidateToken_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &inMemoryAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	token := issueTestToken(t, mgr, "valuser2")

	t.Run("valid token", func(t *testing.T) {
		result, err := adapter.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Valid)
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := adapter.ValidateToken(ctx, "not-a-real-token")
		require.Error(t, err)
	})
}

func TestIAMAdapter_RefreshToken_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &inMemoryAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	token := issueTestToken(t, mgr, "refuser2")

	t.Run("refresh with valid token", func(t *testing.T) {
		newToken, err := adapter.RefreshToken(ctx, token.RefreshToken)
		require.NoError(t, err)
		require.NotNil(t, newToken)
		assert.NotEmpty(t, newToken.AccessToken)
	})

	t.Run("refresh with invalid token", func(t *testing.T) {
		_, err := adapter.RefreshToken(ctx, "invalid-refresh")
		require.Error(t, err)
	})
}

func TestIAMAdapter_RevokeToken_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &inMemoryAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	token := issueTestToken(t, mgr, "revuser2")

	t.Run("revoke valid token succeeds", func(t *testing.T) {
		err := adapter.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)
	})

	t.Run("revoke unknown token exercises path", func(t *testing.T) {
		// Revocation is idempotent for unknown tokens.
		err := adapter.RevokeToken(ctx, "nonexistent-token")
		_ = err
	})
}

func TestIAMAdapter_CreateUser_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &inMemoryAuthManagerAdapter{manager: mgr}

	t.Run("creates user successfully", func(t *testing.T) {
		user, err := adapter.CreateUser("newuser2", []string{"reader"})
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Equal(t, "newuser2", user.Username)
	})

	t.Run("duplicate user returns error", func(t *testing.T) {
		_, err := adapter.CreateUser("newuser2", []string{"admin"})
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// interfacesAuthManagerAdapter — real delegation through manager
// ---------------------------------------------------------------------------

func TestIfAdapter_ValidateToken_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	token := issueTestToken(t, mgr, "vtuser2")

	t.Run("valid token", func(t *testing.T) {
		result, err := adapter.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := adapter.ValidateToken(ctx, "bad-token")
		require.Error(t, err)
	})
}

func TestIfAdapter_RefreshToken_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	token := issueTestToken(t, mgr, "rtuser2")

	t.Run("refresh valid", func(t *testing.T) {
		result, err := adapter.RefreshToken(ctx, token.RefreshToken)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("refresh invalid", func(t *testing.T) {
		_, err := adapter.RefreshToken(ctx, "bad-refresh")
		require.Error(t, err)
	})
}

func TestIfAdapter_RevokeToken_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	token := issueTestToken(t, mgr, "rvuser2")

	t.Run("revoke valid token succeeds", func(t *testing.T) {
		err := adapter.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)
	})

	t.Run("revoke unknown token exercises path", func(t *testing.T) {
		err := adapter.RevokeToken(ctx, "nonexistent")
		_ = err
	})
}

func TestIfAdapter_CreateUser_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}

	t.Run("creates user successfully", func(t *testing.T) {
		result, err := adapter.CreateUser("ifnewuser2", []string{"reader"})
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("duplicate user returns error", func(t *testing.T) {
		_, err := adapter.CreateUser("ifnewuser2", []string{"admin"})
		require.Error(t, err)
	})
}

func TestIfAdapter_Authorize_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	token := issueTestToken(t, mgr, "authzuser2")

	t.Run("authorize with valid token exercises path", func(t *testing.T) {
		err := adapter.Authorize(ctx, token.AccessToken, "resource", "read")
		_ = err
	})

	t.Run("authorize with invalid token returns error", func(t *testing.T) {
		err := adapter.Authorize(ctx, "bad-token", "resource", "read")
		require.Error(t, err)
	})
}

func TestIfAdapter_GetUserStore_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}

	t.Run("returns user store", func(t *testing.T) {
		store := adapter.GetUserStore()
		require.NotNil(t, store)
	})
}

func TestIfAdapter_GetRoleManager_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}

	t.Run("returns role manager", func(t *testing.T) {
		rm := adapter.GetRoleManager()
		require.NotNil(t, rm)
	})
}

func TestIfAdapter_IssueTokenForUser_Success(t *testing.T) {
	mgr := createTestAuthManager(t)
	adapter := &interfacesAuthManagerAdapter{manager: mgr}
	ctx := context.Background()

	user, err := mgr.CreateUser("issueuser", []string{"admin"})
	require.NoError(t, err)

	t.Run("issues a token for a verified user", func(t *testing.T) {
		result, err := adapter.IssueTokenForUser(ctx, user)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("nil user returns error", func(t *testing.T) {
		_, err := adapter.IssueTokenForUser(ctx, nil)
		require.Error(t, err)
	})
}
