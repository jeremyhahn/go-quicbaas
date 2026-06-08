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

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingDecisionPoint is a test double for the QRDBDecisionPoint SEAM (not
// for qrdb's decision logic, which is exercised against the real qrdb PDP in
// pkg/auth/qrdbauthz). It records the routed call and returns a configured
// outcome so the manager's routing and subject construction can be verified.
type recordingDecisionPoint struct {
	called   bool
	subject  QRDBSubject
	resource string
	action   string
	ret      error
}

func (d *recordingDecisionPoint) Authorize(_ context.Context, subject QRDBSubject, resource, action string) error {
	d.called = true
	d.subject = subject
	d.resource = resource
	d.action = action
	return d.ret
}

// newAuthorizedManager builds an in-memory auth manager with a single user and
// returns the manager plus a valid access token for that user.
func newAuthorizedManager(t *testing.T, roles []string) (*InMemoryAuthManager, string) {
	t.Helper()
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  time.Hour,
	}, log)
	require.NoError(t, err)

	user, err := am.CreateUser("alice", roles)
	require.NoError(t, err)
	user.TenantID = "system"

	token, err := am.generateTokens(user)
	require.NoError(t, err)
	return am, token.AccessToken
}

// TestAuthorize_NoDecisionPoint_PreservesRoleManager asserts that without a
// qrdb decision point installed (the default, RequireAuthz=false posture), the
// manager preserves the legacy roleManager.HasPermission outcome: an admin is
// allowed and an unprivileged role is denied — exactly as before this change.
func TestAuthorize_NoDecisionPoint_PreservesRoleManager(t *testing.T) {
	ctx := context.Background()

	amAdmin, adminTok := newAuthorizedManager(t, []string{RoleNameAdmin})
	require.Nil(t, amAdmin.qrdbAuthz, "no decision point installed by default")
	assert.NoError(t, amAdmin.Authorize(ctx, adminTok, ResourceKeys, ActionRead),
		"admin must still be allowed via the legacy roleManager")

	amReadOnly, roTok := newAuthorizedManager(t, []string{RoleNameReadOnly})
	assert.Error(t, amReadOnly.Authorize(ctx, roTok, ResourceKeys, ActionWrite),
		"read-only must still be denied a write via the legacy roleManager")
}

// TestAuthorize_DecisionPoint_RoutesAndAllows asserts that when a decision point
// is installed the manager routes the decision through it (with the claims-built
// subject) and honors an allow.
func TestAuthorize_DecisionPoint_RoutesAndAllows(t *testing.T) {
	ctx := context.Background()
	am, tok := newAuthorizedManager(t, []string{RoleNameAdmin})

	dp := &recordingDecisionPoint{ret: nil}
	am.SetQRDBDecisionPoint(dp)

	err := am.Authorize(ctx, tok, "keys", "get")
	require.NoError(t, err)

	assert.True(t, dp.called, "decision must route through the qrdb decision point")
	assert.Equal(t, "keys", dp.resource)
	assert.Equal(t, "get", dp.action)
	assert.Equal(t, "system", dp.subject.TenantID)
	assert.Contains(t, dp.subject.Roles, RoleNameAdmin)
}

// TestAuthorize_DecisionPoint_Denies asserts a decision-point denial surfaces as
// the manager's typed insufficient-permissions error.
func TestAuthorize_DecisionPoint_Denies(t *testing.T) {
	ctx := context.Background()
	am, tok := newAuthorizedManager(t, []string{RoleNameAdmin})

	dp := &recordingDecisionPoint{ret: errors.New("denied by qrdb pdp")}
	am.SetQRDBDecisionPoint(dp)

	err := am.Authorize(ctx, tok, "keys", "sign")
	require.Error(t, err)
	assert.True(t, dp.called)
}

// TestPersistentAuthorize_DecisionPoint asserts the persistent manager routes
// the decision through an installed qrdb decision point (and honors deny),
// while preserving the legacy roleManager outcome when none is installed.
func TestPersistentAuthorize_DecisionPoint(t *testing.T) {
	log := createPersistentTestLogger(t)
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(&AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  time.Hour,
	}, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	user, err := pam.CreateUser("admin", []string{RoleNameAdmin})
	require.NoError(t, err)
	user.TenantID = "system"
	tok, err := pam.generateTokens(user)
	require.NoError(t, err)
	ctx := context.Background()

	// No decision point: legacy roleManager admits the admin.
	require.Nil(t, pam.qrdbAuthz)
	assert.NoError(t, pam.Authorize(ctx, tok.AccessToken, ResourceKeys, ActionRead))

	// Decision point installed and allowing: routes through it.
	allowDP := &recordingDecisionPoint{ret: nil}
	pam.SetQRDBDecisionPoint(allowDP)
	require.NoError(t, pam.Authorize(ctx, tok.AccessToken, "keys", "get"))
	assert.True(t, allowDP.called)
	assert.Equal(t, "keys", allowDP.resource)
	assert.Equal(t, "get", allowDP.action)
	assert.Equal(t, "system", allowDP.subject.TenantID)

	// Decision point denying: surfaces a typed error.
	denyDP := &recordingDecisionPoint{ret: errors.New("denied")}
	pam.SetQRDBDecisionPoint(denyDP)
	assert.Error(t, pam.Authorize(ctx, tok.AccessToken, "keys", "sign"))
	assert.True(t, denyDP.called)
}
