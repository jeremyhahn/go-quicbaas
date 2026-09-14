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

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestErrorsIsCannotDistinguishIAMSentinels pins the reason the predicates in
// errors_conditions.go exist, so nobody re-derives the obvious wrong
// implementation. Every assertion here is about errors.Is, and every one of
// them is TRUE for a pair of sentinels that mean different things: QBError is
// an alias for go-qrdb's QRDBError, whose Is compares the error CODE alone.
//
// The paired predicate assertion is what the call sites depend on.
func TestErrorsIsCannotDistinguishIAMSentinels(t *testing.T) {
	tests := []struct {
		name string
		// err and target are two DIFFERENT conditions carrying one code.
		err, target error
		// narrow is the predicate written for target's condition. It must
		// refuse err even though errors.Is accepts it.
		narrow func(error) bool
	}{
		{
			name:   "a rejected PIV certificate is forbidden, like a spent invite",
			err:    ErrPIVCertRejected,
			target: ErrInviteExpired,
			narrow: IsInviteUnusable,
		},
		{
			name:   "a spent invite is forbidden, like a rejected PIV certificate",
			err:    ErrInviteUsed,
			target: ErrPIVCertRejected,
			narrow: IsPIVCertRejected,
		},
		{
			name:   "a suspended tenant is forbidden, like an expired invite",
			err:    ErrTenantSuspended,
			target: ErrInviteExpired,
			narrow: IsInviteUnusable,
		},
		{
			name:   "an absent role is not-found, like an absent user",
			err:    ErrRoleNotFound,
			target: ErrUserNotFound,
			narrow: IsUserNotFound,
		},
		{
			name:   "an absent user is not-found, like an absent MFA enrollment",
			err:    ErrUserNotFound,
			target: ErrMFANotEnrolled,
			narrow: IsMFANotEnrolled,
		},
		{
			name:   "an absent session is not-found, like an absent role",
			err:    ErrSessionNotFound,
			target: ErrRoleNotFound,
			narrow: IsRoleNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, errors.Is(tc.err, tc.target),
				"errors.Is stopped comparing codes; the predicates may no longer be needed")
			assert.False(t, tc.narrow(tc.err),
				"the narrow predicate matched a different condition with the same code")
		})
	}
}

// TestIAMConditionPredicatesMatchTheirOwnSentinel is the positive half: each
// predicate must still recognise the condition it is named for, both as the
// bare sentinel and wrapped.
func TestIAMConditionPredicatesMatchTheirOwnSentinel(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		narrow func(error) bool
	}{
		{"user not found", ErrUserNotFound, IsUserNotFound},
		{"role not found", ErrRoleNotFound, IsRoleNotFound},
		{"MFA not enrolled", ErrMFANotEnrolled, IsMFANotEnrolled},
		{"invite absent", ErrInviteNotFound, IsInviteUnusable},
		{"invite expired", ErrInviteExpired, IsInviteUnusable},
		{"invite spent", ErrInviteUsed, IsInviteUnusable},
		{"user not pending", ErrUserNotPending, IsUserNotPending},
		{"PIV certificate rejected", ErrPIVCertRejected, IsPIVCertRejected},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, tc.narrow(tc.err), "the predicate refused its own sentinel")
			assert.True(t, tc.narrow(&IAMContextError{Message: "wrapped", Cause: tc.err}),
				"the predicate refused its own sentinel through a wrapper")
			assert.False(t, tc.narrow(nil), "the predicate matched a nil error")
		})
	}
}

// TestIAMSentinelMessagesAreUnchanged guards the swap from IAMMessageError to
// the dedicated condition types: the rendered text is part of the API surface
// (it reaches operators through the REST error envelope), so it must not have
// moved when the inner error's TYPE did.
func TestIAMSentinelMessagesAreUnchanged(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrUserNotFound, "iam.user: not_found: user not found"},
		{ErrRoleNotFound, "iam.role: not_found: role not found"},
		{ErrMFANotEnrolled, "iam.mfa: not_found: MFA not enrolled"},
		{ErrInviteNotFound, "iam.invite: not_found: invite not found"},
		{ErrInviteExpired, "iam.invite: forbidden: invite has expired"},
		{ErrInviteUsed, "iam.invite: forbidden: invite has already been used"},
		{ErrUserNotPending, "iam.invite.activate: conflict: user is not in pending state"},
		{ErrPIVCertRejected, "iam.invite.activate.piv: forbidden: PIV certificate rejected"},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.err.Error())
	}
}

// TestMemoryStorageAnswersIdentityLookupsWithSentinels proves the in-memory
// backend reports an absent user or role as the SENTINEL rather than a bare
// not-found code. Callers narrow on the sentinel's typed inner error; a bare
// QBError{ErrNotFound} carries no such identity and would be read as an
// unrelated store failure.
func TestMemoryStorageAnswersIdentityLookupsWithSentinels(t *testing.T) {
	m := NewMemoryStorage()
	ctx := context.Background()

	_, err := m.GetUser(ctx, "acme", "404")
	assert.True(t, IsUserNotFound(err), "GetUser: %v", err)

	_, err = m.GetUserByEmail(ctx, "acme", "nobody@acme.com")
	assert.True(t, IsUserNotFound(err), "GetUserByEmail: %v", err)

	_, err = m.GetUserByUsername(ctx, "acme", "nobody")
	assert.True(t, IsUserNotFound(err), "GetUserByUsername: %v", err)

	_, err = m.GetUserByPIVThumbprint(ctx, "no-such-thumbprint")
	assert.True(t, IsUserNotFound(err), "GetUserByPIVThumbprint: %v", err)

	_, err = m.GetRole(ctx, "acme", "404")
	assert.True(t, IsRoleNotFound(err), "GetRole: %v", err)

	// The code is still what the REST bridges map to a status.
	require.True(t, dberrors.IsNotFound(err))
}
