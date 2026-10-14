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
)

const (
	scopeUser        uint64 = 50001
	scopeTenant             = "tenant-owner"
	scopeOtherUser   uint64 = 50002
	scopeOtherTenant        = "tenant-intruder"
)

// mfaCaller is the user and tenant an MFA verb is called as.
type mfaCaller struct {
	user   uint64
	tenant string
}

// TestEnrollmentVerbsStayWithOwner proves an enrollment id is only usable by
// the user and tenant that began it: another tenant or another user gets the
// same not-found as a missing id, the enrollment survives untouched, and the
// owner can still complete it.
func TestEnrollmentVerbsStayWithOwner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m, err := NewMFAManager()
	require.NoError(t, err)

	resp, err := m.BeginEnrollment(ctx, scopeUser, scopeTenant, MFAMethodTOTP)
	require.NoError(t, err)
	code := generateValidTOTPCode(t, resp.Secret, m.config)

	intruders := map[string]mfaCaller{
		"other tenant":      {scopeUser, scopeOtherTenant},
		"other user":        {scopeOtherUser, scopeTenant},
		"other user+tenant": {scopeOtherUser, scopeOtherTenant},
	}
	for name, who := range intruders {
		assert.ErrorIs(t, m.CompleteEnrollment(ctx, who.user, who.tenant, resp.EnrollmentID, code),
			ErrMFAEnrollmentNotFound, "complete as %s", name)
		assert.ErrorIs(t, m.CancelEnrollment(ctx, who.user, who.tenant, resp.EnrollmentID),
			ErrMFAEnrollmentNotFound, "cancel as %s", name)
	}
	assert.ErrorIs(t, m.CompleteEnrollment(ctx, 0, scopeOtherTenant, resp.EnrollmentID, code),
		ErrMFAEnrollmentNotFound, "a tenant-only caller must not reach another tenant")

	enrollment, err := m.storage.GetEnrollment(ctx, resp.EnrollmentID)
	require.NoError(t, err, "refused verbs must leave the enrollment in place")
	assert.Zero(t, enrollment.Attempts, "refused verbs must not spend the owner's attempts")

	require.NoError(t, m.CompleteEnrollment(ctx, scopeUser, scopeTenant, resp.EnrollmentID, code))
}

// TestCancelEnrollmentOwnerSucceeds proves the owner, and a tenant-only caller
// in the owner's tenant, can cancel the enrollment.
func TestCancelEnrollmentOwnerSucceeds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m, err := NewMFAManager()
	require.NoError(t, err)

	first, err := m.BeginEnrollment(ctx, scopeUser, scopeTenant, MFAMethodTOTP)
	require.NoError(t, err)
	require.NoError(t, m.CancelEnrollment(ctx, scopeUser, scopeTenant, first.EnrollmentID))

	second, err := m.BeginEnrollment(ctx, scopeUser, scopeTenant, MFAMethodTOTP)
	require.NoError(t, err)
	require.NoError(t, m.CancelEnrollment(ctx, 0, scopeTenant, second.EnrollmentID))

	_, err = m.storage.GetEnrollment(ctx, second.EnrollmentID)
	assert.ErrorIs(t, err, ErrMFAEnrollmentNotFound)
}

// TestVerifyChallengeStaysWithOwner proves a challenge id is only usable by
// the user and tenant it was issued to, and that a refused attempt neither
// consumes the challenge nor spends its attempts.
func TestVerifyChallengeStaysWithOwner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m, err := NewMFAManager()
	require.NoError(t, err)

	enroll, err := m.BeginEnrollment(ctx, scopeUser, scopeTenant, MFAMethodTOTP)
	require.NoError(t, err)
	require.NoError(t, m.CompleteEnrollment(ctx, scopeUser, scopeTenant, enroll.EnrollmentID,
		generateValidTOTPCode(t, enroll.Secret, m.config)))

	challenge, err := m.BeginChallenge(ctx, scopeUser, scopeTenant, "session-token")
	require.NoError(t, err)
	code := generateValidTOTPCode(t, enroll.Secret, m.config)

	for _, who := range []mfaCaller{
		{scopeUser, scopeOtherTenant},
		{scopeOtherUser, scopeTenant},
		{0, scopeOtherTenant},
	} {
		resp, err := m.VerifyChallenge(ctx, who.user, who.tenant, challenge.ID, code)
		assert.ErrorIs(t, err, ErrMFAChallengeNotFound, "verify as user=%d tenant=%q", who.user, who.tenant)
		assert.Nil(t, resp)
	}

	stored, err := m.storage.GetChallenge(ctx, challenge.ID)
	require.NoError(t, err, "refused verifies must leave the challenge in place")
	assert.Zero(t, stored.Attempts, "refused verifies must not spend the owner's attempts")

	resp, err := m.VerifyChallenge(ctx, scopeUser, scopeTenant, challenge.ID, code)
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

// TestMFAObjectVerbsRequireTenant proves the manager refuses to look an
// enrollment or challenge up without a tenant to bind it to.
func TestMFAObjectVerbsRequireTenant(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m, err := NewMFAManager()
	require.NoError(t, err)

	resp, err := m.BeginEnrollment(ctx, scopeUser, scopeTenant, MFAMethodTOTP)
	require.NoError(t, err)

	assert.ErrorIs(t, m.CompleteEnrollment(ctx, scopeUser, "", resp.EnrollmentID, "123456"), ErrMFAInvalid)
	assert.ErrorIs(t, m.CancelEnrollment(ctx, scopeUser, "", resp.EnrollmentID), ErrMFAInvalid)
	_, err = m.VerifyChallenge(ctx, scopeUser, "", "challenge-id", "123456")
	assert.ErrorIs(t, err, ErrMFAInvalid)

	_, err = m.storage.GetEnrollment(ctx, resp.EnrollmentID)
	assert.NoError(t, err)
}
