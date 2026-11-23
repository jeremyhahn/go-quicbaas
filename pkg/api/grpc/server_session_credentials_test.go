// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package grpc

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

const (
	credentialsSubject   = "user-42"
	credentialsTenant    = "t1"
	credentialsUserID    = uint64(71)
	credentialsSessionID = "sess-user-42"
)

// revokeCall is one RevokeSubjectCredentials call a recordingCredentialRevoker saw.
type revokeCall struct{ tenantID, subject string }

// recordingCredentialRevoker is a CredentialRevoker that records each call and
// returns err. It lives in this file, which has no build constraint, because
// session tests built under the integration tag use it too.
type recordingCredentialRevoker struct {
	mu    sync.Mutex
	calls []revokeCall
	err   error
}

func (r *recordingCredentialRevoker) RevokeSubjectCredentials(_ context.Context, tenantID, subject string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, revokeCall{tenantID: tenantID, subject: subject})
	return len(r.calls), r.err
}

// credentialsFixture builds a SessionServer whose caller is credentialsSubject
// in credentialsTenant, with one session record seeded for that caller. A nil
// revoker leaves the server without one.
func credentialsFixture(t *testing.T, revoker CredentialRevoker) (*SessionServer, *iam.MemorySessionStorage) {
	t.Helper()
	storage := iam.NewMemorySessionStorage()
	now := time.Now()
	require.NoError(t, storage.CreateSession(context.Background(), &iam.Session{
		ID:           credentialsSessionID,
		TenantID:     credentialsTenant,
		UserID:       credentialsUserID,
		Created:      now,
		LastActivity: now,
		Expires:      now.Add(time.Hour),
	}))
	callers := fakeCallerAccounts{credentialsTenant + "/" + credentialsSubject: credentialsUserID}
	server := NewSessionServer(NewSessionServiceAdapter(storage), callers, newTestSessionLogger())
	if revoker != nil {
		server = server.WithCredentialRevoker(revoker)
	}
	return server, storage
}

// TestSessionServer_RevokeAllRevokesCallerCredentials proves logout-all ends
// the caller's own login credentials in the caller's tenant, then deletes the
// caller's session records.
func TestSessionServer_RevokeAllRevokesCallerCredentials(t *testing.T) {
	revoker := &recordingCredentialRevoker{}
	server, storage := credentialsFixture(t, revoker)

	resp, err := server.RevokeAllSessions(sessionCallerContext(credentialsTenant, credentialsSubject), &iampb.RevokeAllSessionsRequest{})
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetRevokedCount())

	require.Equal(t, []revokeCall{{tenantID: credentialsTenant, subject: credentialsSubject}}, revoker.calls)
	_, err = storage.GetSession(context.Background(), credentialsSessionID)
	require.Error(t, err, "the caller's session record must be deleted")
}

// TestSessionServer_RevokeAllWithoutRevokerIsUnavailable proves logout-all
// refuses, and deletes nothing, when it cannot end the caller's credentials.
func TestSessionServer_RevokeAllWithoutRevokerIsUnavailable(t *testing.T) {
	server, storage := credentialsFixture(t, nil)

	_, err := server.RevokeAllSessions(sessionCallerContext(credentialsTenant, credentialsSubject), &iampb.RevokeAllSessionsRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err), "err = %v", err)
	requireSessionKept(t, storage, credentialsSessionID)
}

// TestSessionServer_RevokeAllRevokerFailureKeepsSessionRecords proves a failed
// credential revocation is reported and leaves the session records in place.
func TestSessionServer_RevokeAllRevokerFailureKeepsSessionRecords(t *testing.T) {
	revoker := &recordingCredentialRevoker{err: errors.New("token store down")}
	server, storage := credentialsFixture(t, revoker)

	_, err := server.RevokeAllSessions(sessionCallerContext(credentialsTenant, credentialsSubject), &iampb.RevokeAllSessionsRequest{})
	require.Equal(t, codes.Internal, status.Code(err), "err = %v", err)
	require.NotContains(t, status.Convert(err).Message(), "token store down")
	requireSessionKept(t, storage, credentialsSessionID)
}

// TestSessionServer_RevokeAllEndsRealCredentials proves that, with the OIDC
// provider as the revoker, the caller's access token no longer validates and
// its refresh credential no longer redeems after logout-all.
func TestSessionServer_RevokeAllEndsRealCredentials(t *testing.T) {
	ctx := context.Background()
	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	keys, err := iam.NewSigningKeyManagerFromSigner(key, "RS256")
	require.NoError(t, err)

	tokens := iam.NewMemoryStorage()
	require.NoError(t, tokens.CreateUser(ctx, &iam.User{
		ID:        credentialsUserID,
		TenantID:  credentialsTenant,
		Username:  credentialsSubject,
		Name:      "Logout User",
		Email:     "user42@example.com",
		Lifecycle: iam.LifecycleActive,
	}))
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            "https://logout.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: keys,
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: tokens, RoleStorage: tokens}),
		AppStorage:    tokens,
		TokenStorage:  tokens,
	})
	require.NoError(t, err)

	access, _, err := provider.IssueSessionToken(ctx, iam.SessionIdentity{
		Subject:    credentialsSubject,
		TenantID:   credentialsTenant,
		UserID:     credentialsUserID,
		AuthMethod: "fido2",
	})
	require.NoError(t, err)
	refresh, _, err := provider.IssueSessionRefresh(ctx, iam.SessionRefreshOptions{
		Subject:  credentialsSubject,
		TenantID: credentialsTenant,
		Scopes:   []string{"openid"},
		Session: iam.TokenSessionContext{
			UserID:     credentialsUserID,
			Audience:   []string{"https://logout.example.com"},
			AuthMethod: "fido2",
		},
	})
	require.NoError(t, err)
	_, err = provider.ValidateAccessToken(ctx, access)
	require.NoError(t, err, "the access token must validate before logout")

	server, _ := credentialsFixture(t, provider)
	_, err = server.RevokeAllSessions(sessionCallerContext(credentialsTenant, credentialsSubject), &iampb.RevokeAllSessionsRequest{})
	require.NoError(t, err)

	_, err = provider.ValidateAccessToken(ctx, access)
	require.Error(t, err, "the access token still validates after logout-all")
	_, err = provider.RefreshSession(ctx, iam.SessionRefreshRequest{RefreshToken: refresh})
	require.Error(t, err, "the refresh credential still redeems after logout-all")
}
