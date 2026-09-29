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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// lookupFailingUserStorage answers the two identity lookups the liveness rules
// use with a chosen error and delegates everything else.
type lookupFailingUserStorage struct {
	UserStorage
	err error
}

func (s *lookupFailingUserStorage) GetUser(context.Context, string, string) (*User, error) {
	return nil, s.err
}

func (s *lookupFailingUserStorage) GetUserByUsername(context.Context, string, string) (*User, error) {
	return nil, s.err
}

func livenessProvider(t *testing.T, storeErr error) *OIDCProvider {
	t.Helper()
	return &OIDCProvider{
		identityStore: NewIdentityStore(IdentityStoreOptions{
			UserStorage: &lookupFailingUserStorage{
				UserStorage: NewMemoryStorage(),
				err:         storeErr,
			},
			RoleStorage: NewMemoryStorage(),
		}),
	}
}

// errForeignNotFound is a not-found that is NOT "this user is absent" -- the shape
// a store, a shard router or a tenant lookup produces when it fails.
var errForeignNotFound = &dberrors.QBError{
	Code: dberrors.ErrNotFound,
	Op:   "iam.user.storage",
	Err:  &IAMMessageError{Message: "user shard is unavailable"},
}

// The two TestLegacySubjectLive_* tests that stood here were deleted with the
// function they named (#390). What they GUARDED is preserved, and the two tests
// below plus TestPrincipalLive_NeitherClaimConsultsNoStore replace them:
//
// The fail-open they caught was "the lookup came up empty, therefore this is not
// a user, therefore a service principal, therefore live" — reached by a FOREIGN
// not-found because the emptiness test compared the QBError code (#434). That
// inference is gone, not relocated. After the deletion principalLive can return
// true from exactly two places: after a SUCCESSFUL GetUser, and for a token that
// positively declares ptyp=service without consulting the store at all. No store
// error reaches either — the error branch returns first, and #434's
// IsUserNotFound narrowing (kept, above) decides only between (false, nil) and
// (false, err). A token carrying neither claim performs no lookup and denies.
//
// So the property is unreachable by construction rather than untested, and the
// test below pins the construction: with a store that fails every lookup, a
// neither-claim token still returns (false, nil) — which it could not do if any
// lookup were still happening.

// TestPrincipalLive_NeitherClaimConsultsNoStore is the successor to the deleted
// TestLegacySubjectLive_ForeignNotFoundDeniesRatherThanAdmitting.
//
// The store fails EVERY lookup. A token carrying neither uid nor ptyp must
// still deny cleanly: an error would prove a lookup happened, and a lookup is
// the thing whose failure used to be read as "service principal, therefore
// live".
func TestPrincipalLive_NeitherClaimConsultsNoStore(t *testing.T) {
	p := livenessProvider(t, errForeignNotFound)

	live, err := p.principalLive(context.Background(), "acme", "operator", "", "")

	require.NoError(t, err, "a lookup was performed for a token that names no principal")
	assert.False(t, live, "a token carrying neither uid nor ptyp must not be live")
}

// TestPrincipalLive_ServicePrincipalNeedsNoLookup pins the other surviving
// true: it comes from the POSITIVE ptyp claim, not from an absent lookup. The
// store fails every call, and the answer is still live — which is only correct
// because nothing was asked of it.
func TestPrincipalLive_ServicePrincipalNeedsNoLookup(t *testing.T) {
	p := livenessProvider(t, errForeignNotFound)

	live, err := p.principalLive(context.Background(), "acme", "app_acme_x", "", PrincipalTypeService)

	require.NoError(t, err)
	assert.True(t, live, "a service principal is live on its own claim")
}

// TestPrincipalLive_ForeignNotFoundIsNotADeletedAccount proves the uid path
// tells a deleted account apart from a store that failed. Both answers deny, so
// the consequence here is the operator's, not the caller's: (false, nil) says
// the account is gone, and nothing establishes that.
func TestPrincipalLive_ForeignNotFoundIsNotADeletedAccount(t *testing.T) {
	p := livenessProvider(t, errForeignNotFound)

	live, err := p.principalLive(context.Background(), "acme", "operator", "77", "")

	require.Error(t, err, "a failed identity lookup was reported as a deleted account")
	assert.False(t, live)
}

// TestPrincipalLive_DeletedAccountStillDeniesQuietly is the positive half.
func TestPrincipalLive_DeletedAccountStillDeniesQuietly(t *testing.T) {
	p := livenessProvider(t, ErrUserNotFound)

	live, err := p.principalLive(context.Background(), "acme", "operator", "77", "")

	require.NoError(t, err)
	assert.False(t, live)
}
