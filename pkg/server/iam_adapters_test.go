//go:build !integration

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
)

// createTestAuthManager creates an InMemoryAuthManager suitable for unit testing.
func createTestAuthManager(t *testing.T) *auth.InMemoryAuthManager {
	t.Helper()
	log := mustCreateLogger()
	signingKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	cfg := &auth.AuthConfig{
		Enabled:         true,
		JWTSigningKeyID: "test-signing-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		RefreshWindow:   5 * time.Minute,
		MaxTokens:       10,
	}
	auth.SetAsymmetricSigning(cfg, signingKey)

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
