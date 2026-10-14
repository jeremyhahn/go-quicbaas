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

package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// nonIDUserIDs are string user ids that do not name a uint64 Snowflake:
// non-numeric, past the uint64 range, and the "not created yet" zero.
var nonIDUserIDs = []string{"user-1", "18446744073709551616", "0"}

// countingTargetResolver records whether StartImpersonation consulted it.
type countingTargetResolver struct {
	calls int
}

func (r *countingTargetResolver) GetUser(ctx context.Context, tenantID string, userID uint64) (*User, error) {
	r.calls++
	return &User{ID: userID, TenantID: tenantID}, nil
}

func TestProjectInvite_ParsesTheUserID(t *testing.T) {
	record, err := projectInvite(&qrdbsdk.InviteEntity{
		UserID:   "9007199254740993",
		TenantID: "tenant-1",
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(9007199254740993), record.UserID)
	assert.Equal(t, "tenant-1", record.TenantID)

	record, err = projectInvite(nil)
	require.NoError(t, err)
	assert.Nil(t, record)
}

func TestProjectInvite_RefusesANonID(t *testing.T) {
	for _, value := range nonIDUserIDs {
		record, err := projectInvite(&qrdbsdk.InviteEntity{UserID: value, TenantID: "tenant-1"})
		assert.Nil(t, record, "value %q", value)
		assert.ErrorIs(t, err, ErrInvalidUserID, "value %q", value)
	}
}

// TestAdminManager_StartImpersonation_ANonIDTargetIsNotFound proves a target
// id that cannot name a user is refused as not found before the resolver is
// asked, and no session is minted.
func TestAdminManager_StartImpersonation_ANonIDTargetIsNotFound(t *testing.T) {
	resolver := &countingTargetResolver{}
	manager, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         NewMemoryAdminStorage(),
		ImpersonationStorage: NewMemoryImpersonationStorage(),
		TargetResolver:       resolver,
	})
	require.NoError(t, err)

	_, err = manager.CreateAssignment(context.Background(), CreateAdminAssignmentOptions{
		UserID:    "admin-1",
		AdminRole: AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant-1"},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	for _, value := range nonIDUserIDs {
		session, err := manager.StartImpersonation(ctxWithActor("admin-1"), &StartImpersonationOptions{
			TargetUserID: value,
			TenantID:     "tenant-1",
			Reason:       "malformed target",
		})
		assert.Nil(t, session, "value %q", value)
		assert.True(t, errors.Is(err, ErrImpersonationTargetNotFound),
			"value %q: want ErrImpersonationTargetNotFound, got %v", value, err)
	}
	assert.Zero(t, resolver.calls, "the resolver must not be asked about a non-id")
}

// TestOAuthRefreshGrant_RefusesANonIDSubject proves a refresh token whose
// subject is not a user id is refused as invalid_grant, not looked up.
func TestOAuthRefreshGrant_RefusesANonIDSubject(t *testing.T) {
	t.Parallel()

	provider, _ := newCeremonyProvider(t, nil)
	ctx := context.Background()

	const clientID, clientSecret = "app_burnin_client", "s3cret-for-the-test"
	require.NoError(t, provider.appStorage.CreateApp(ctx, &Application{
		ClientID:     clientID,
		ClientSecret: currentSecretHash(t, clientSecret),
		TenantID:     burninTenant,
	}))

	refreshWithSubject := func(subject string) error {
		refresh, err := provider.tokenService.GenerateRefreshToken(ctx, SessionRefreshOptions{
			Subject:  subject,
			TenantID: burninTenant,
			ClientID: clientID,
			Scopes:   []string{"openid"},
		})
		require.NoError(t, err)

		_, err = provider.handleRefreshTokenGrant(ctx, &TokenRequest{
			GrantType:    "refresh_token",
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RefreshToken: refresh,
		})
		return err
	}

	// Control: a well-formed id for an absent user gets past the token
	// lookup to the user fetch, so the refusals below come from the parse.
	var controlErr *OAuthError
	require.ErrorAs(t, refreshWithSubject("424242"), &controlErr)
	require.Equal(t, ErrOAuthServerError, controlErr.ErrorCode)

	for _, subject := range nonIDUserIDs {
		err := refreshWithSubject(subject)
		var oauthErr *OAuthError
		require.ErrorAs(t, err, &oauthErr, "subject %q", subject)
		assert.Equal(t, ErrOAuthInvalidGrant, oauthErr.ErrorCode, "subject %q", subject)
		assert.Equal(t, "invalid refresh token", oauthErr.ErrorDescription, "subject %q", subject)
	}
}
