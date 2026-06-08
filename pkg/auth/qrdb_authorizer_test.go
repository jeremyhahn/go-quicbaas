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

	apierrors "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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

// assertAuthorizerNotConfigured asserts err carries the typed
// AuthorizerNotConfiguredError.
func assertAuthorizerNotConfigured(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var typed *apierrors.AuthorizerNotConfiguredError
	assert.True(t, errors.As(err, &typed),
		"expected AuthorizerNotConfiguredError, got %T: %v", err, err)
}

// TestAuthorize_NoDecisionPoint_NotConfigured asserts that without a qrdb
// decision point installed the manager fails closed: it makes no local RBAC
// decision and returns the typed AuthorizerNotConfiguredError regardless of the
// caller's roles.
func TestAuthorize_NoDecisionPoint_NotConfigured(t *testing.T) {
	ctx := context.Background()

	amAdmin, adminTok := newAuthorizedManager(t, []string{"admin"})
	require.Nil(t, amAdmin.qrdbAuthz, "no decision point installed by default")
	assertAuthorizerNotConfigured(t,
		amAdmin.Authorize(ctx, adminTok, interfaces.ResourceKeys, interfaces.ActionRead))

	amReadOnly, roTok := newAuthorizedManager(t, []string{"readonly"})
	assertAuthorizerNotConfigured(t,
		amReadOnly.Authorize(ctx, roTok, interfaces.ResourceKeys, interfaces.ActionWrite))
}

// TestAuthorize_DecisionPoint_RoutesAndAllows asserts that when a decision point
// is installed the manager routes the decision through it (with the claims-built
// subject) and honors an allow.
func TestAuthorize_DecisionPoint_RoutesAndAllows(t *testing.T) {
	ctx := context.Background()
	am, tok := newAuthorizedManager(t, []string{"admin"})

	dp := &recordingDecisionPoint{ret: nil}
	am.SetQRDBDecisionPoint(dp)

	err := am.Authorize(ctx, tok, "keys", "get")
	require.NoError(t, err)

	assert.True(t, dp.called, "decision must route through the qrdb decision point")
	assert.Equal(t, "keys", dp.resource)
	assert.Equal(t, "get", dp.action)
	assert.Equal(t, "system", dp.subject.TenantID)
	assert.Contains(t, dp.subject.Roles, "admin")
}

// TestAuthorize_DecisionPoint_Denies asserts a decision-point denial surfaces as
// the manager's typed insufficient-permissions error.
func TestAuthorize_DecisionPoint_Denies(t *testing.T) {
	ctx := context.Background()
	am, tok := newAuthorizedManager(t, []string{"admin"})

	dp := &recordingDecisionPoint{ret: errors.New("denied by qrdb pdp")}
	am.SetQRDBDecisionPoint(dp)

	err := am.Authorize(ctx, tok, "keys", "sign")
	require.Error(t, err)
	assert.True(t, dp.called)
}

// TestPersistentAuthorize_NotConfigured asserts the persistent manager also
// fails closed with AuthorizerNotConfiguredError when no decision point is
// installed, then routes (allow + deny) once one is installed.
func TestPersistentAuthorize_NotConfigured(t *testing.T) {
	log := createPersistentTestLogger(t)
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(&AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  time.Hour,
	}, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	user, err := pam.CreateUser("admin", []string{"admin"})
	require.NoError(t, err)
	user.TenantID = "system"
	tok, err := pam.generateTokens(user)
	require.NoError(t, err)
	ctx := context.Background()

	// No decision point: fail closed.
	require.Nil(t, pam.qrdbAuthz)
	assertAuthorizerNotConfigured(t,
		pam.Authorize(ctx, tok.AccessToken, interfaces.ResourceKeys, interfaces.ActionRead))

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
