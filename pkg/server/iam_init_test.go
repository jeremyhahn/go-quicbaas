//go:build iam && !integration

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// WebAuthn adapter: exercising parse functions with proper error paths
// =============================================================================

func TestFTFinal_WebAuthnAdapter_ParseCredentialCreation_EmptyMap(t *testing.T) {
	result, err := parseCredentialCreationResponse(map[string]interface{}{})
	// Empty map doesn't have required fields - should error from go-webauthn
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestFTFinal_WebAuthnAdapter_ParseCredentialAssertion_EmptyMap(t *testing.T) {
	result, err := parseCredentialAssertionResponse(map[string]interface{}{})
	// Empty map doesn't have required fields - should error from go-webauthn
	assert.Error(t, err)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// validateBootstrapCredentials tests
// ---------------------------------------------------------------------------

func TestValidateBootstrapCredentials(t *testing.T) {
	t.Run("insecure dev mode skips all checks", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "admin", true)
		assert.NoError(t, err)
	})

	t.Run("empty password in production rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
		assert.Contains(t, err.Error(), "bootstrap_admin_password is required")
	})

	t.Run("default password admin rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "admin", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("password 'password' rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("user", "password", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("password 'changeme' rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("user", "changeme", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("password same as username rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("myuser", "myuser", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})

	t.Run("short password rejected", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "short", false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
		assert.Contains(t, err.Error(), "at least")
	})

	t.Run("password at minimum length accepted", func(t *testing.T) {
		longPassword := "abcdefghijkl" // 12 chars = config.MinBootstrapPasswordLength
		require.GreaterOrEqual(t, len(longPassword), config.MinBootstrapPasswordLength)
		err := validateBootstrapCredentials("admin", longPassword, false)
		assert.NoError(t, err)
	})

	t.Run("strong password accepted", func(t *testing.T) {
		err := validateBootstrapCredentials("admin", "super-secure-password-2025!", false)
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// resolveBootstrapCredentials tests
// ---------------------------------------------------------------------------

func TestServer_ResolveBootstrapCredentials(t *testing.T) {
	t.Run("uses configured credentials", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = "myadmin"
		srv.config.Auth.BootstrapAdminPassword = "mysecret"
		srv.config.Auth.InsecureDevMode = false

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "myadmin", username)
		assert.Equal(t, "mysecret", password)
	})

	t.Run("defaults username to admin when empty", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = ""
		srv.config.Auth.BootstrapAdminPassword = "pass"
		srv.config.Auth.InsecureDevMode = false

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "admin", username)
		assert.Equal(t, "pass", password)
	})

	t.Run("uses dev default password in insecure mode", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = ""
		srv.config.Auth.BootstrapAdminPassword = ""
		srv.config.Auth.InsecureDevMode = true

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "admin", username)
		assert.Equal(t, "quicbaas-dev-admin", password)
	})

	t.Run("leaves password empty in production mode", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Auth.BootstrapAdminUsername = ""
		srv.config.Auth.BootstrapAdminPassword = ""
		srv.config.Auth.InsecureDevMode = false

		username, password := srv.resolveBootstrapCredentials()
		assert.Equal(t, "admin", username)
		assert.Equal(t, "", password)
	})
}

// ---------------------------------------------------------------------------
// iamComponents getter tests
// ---------------------------------------------------------------------------

func TestIAMComponents_GettersReturnNil(t *testing.T) {
	t.Run("all getters return nil when not set", func(t *testing.T) {
		c := &iamComponents{}

		assert.Nil(t, c.GetTenantManager())
		assert.Nil(t, c.GetIdentityStore())
		assert.Nil(t, c.GetPolicyEngine())
		assert.Nil(t, c.GetAppManager())
		assert.Nil(t, c.GetOIDCProvider())
		assert.Nil(t, c.GetMFAManager())
		assert.Nil(t, c.GetWebAuthnManager())
		assert.Nil(t, c.GetAuditLogger())
		assert.Nil(t, c.GetAdminManager())
		assert.Nil(t, c.GetGDPRManager())
		assert.Nil(t, c.GetSessionStorage())
		assert.Nil(t, c.GetSessionManager())
		assert.Nil(t, c.GetAuthManager())
		assert.Nil(t, c.GetInternalAuthManager())
	})
}

// ---------------------------------------------------------------------------
// iamComponents.GetAuthManager with non-nil value
// ---------------------------------------------------------------------------

func TestIAMComponents_GetAuthManager_NonNil(t *testing.T) {
	t.Run("returns non-nil when set", func(t *testing.T) {
		adapter := &inMemoryAuthManagerAdapter{manager: nil}
		c := &iamComponents{
			authManager: adapter,
		}
		assert.NotNil(t, c.GetAuthManager())
	})
}

func TestIAMComponents_GetInternalAuthManager_NonNil(t *testing.T) {
	t.Run("returns non-nil adapter when auth manager set", func(t *testing.T) {
		adapter := &inMemoryAuthManagerAdapter{manager: nil}
		c := &iamComponents{
			authManager: adapter,
		}
		// GetInternalAuthManager creates an interfacesAuthManagerAdapter
		// but requires a non-nil manager field. Since we pass nil, this
		// will work but the adapter wraps nil.
		result := c.GetInternalAuthManager()
		assert.NotNil(t, result)
	})
}
