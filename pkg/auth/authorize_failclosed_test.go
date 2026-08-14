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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apierrors "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// assertAuthorizerNotConfigured asserts err carries the typed
// AuthorizerNotConfiguredError.
func assertAuthorizerNotConfigured(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var typed *apierrors.AuthorizerNotConfiguredError
	assert.True(t, errors.As(err, &typed),
		"expected AuthorizerNotConfiguredError, got %T: %v", err, err)
}

// TestInMemoryAuthorize_FailsClosed asserts the in-memory manager makes no local
// authorization decision: when authentication is enabled it fails closed with the
// typed AuthorizerNotConfiguredError regardless of the caller's roles. The
// authorization decision belongs to qrdb via the platform-token validator at the
// transport layer, not to this in-process manager.
func TestInMemoryAuthorize_FailsClosed(t *testing.T) {
	ctx := context.Background()
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(newTestAuthConfig(t), log)
	require.NoError(t, err)

	admin, err := am.CreateUser("alice", []string{"admin"})
	require.NoError(t, err)
	adminTok, err := am.generateTokens(admin)
	require.NoError(t, err)

	assertAuthorizerNotConfigured(t,
		am.Authorize(ctx, adminTok.AccessToken, interfaces.ResourceKeys, interfaces.ActionRead))

	readonly, err := am.CreateUser("bob", []string{"readonly"})
	require.NoError(t, err)
	roTok, err := am.generateTokens(readonly)
	require.NoError(t, err)

	assertAuthorizerNotConfigured(t,
		am.Authorize(ctx, roTok.AccessToken, interfaces.ResourceKeys, interfaces.ActionWrite))
}

// TestInMemoryAuthorize_DisabledAllows asserts that when authentication is
// disabled the manager allows all (no decision is made).
func TestInMemoryAuthorize_DisabledAllows(t *testing.T) {
	log := createTestLogger(t)
	am, err := NewInMemoryAuthManager(&AuthConfig{Enabled: false}, log)
	require.NoError(t, err)

	require.NoError(t,
		am.Authorize(context.Background(), "", interfaces.ResourceKeys, interfaces.ActionRead))
}

// TestPersistentAuthorize_FailsClosed asserts the persistent manager also fails
// closed with AuthorizerNotConfiguredError when authentication is enabled.
func TestPersistentAuthorize_FailsClosed(t *testing.T) {
	ctx := context.Background()
	log := createPersistentTestLogger(t)
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(newTestAuthConfig(t), storage, log)
	require.NoError(t, err)
	defer pam.Close()

	user, err := pam.CreateUser("admin", []string{"admin"})
	require.NoError(t, err)
	tok, err := pam.generateTokens(user)
	require.NoError(t, err)

	assertAuthorizerNotConfigured(t,
		pam.Authorize(ctx, tok.AccessToken, interfaces.ResourceKeys, interfaces.ActionRead))
}

// TestPersistentAuthorize_DisabledAllows asserts the persistent manager allows
// all when authentication is disabled.
func TestPersistentAuthorize_DisabledAllows(t *testing.T) {
	log := createPersistentTestLogger(t)
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(&AuthConfig{Enabled: false}, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	require.NoError(t,
		pam.Authorize(context.Background(), "", interfaces.ResourceKeys, interfaces.ActionRead))
}
