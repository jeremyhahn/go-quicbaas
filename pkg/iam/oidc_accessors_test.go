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

package iam

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaims_Accessors(t *testing.T) {
	claims := &Claims{
		Subject:     "user-123",
		Issuer:      "https://auth.example.com",
		Audience:    []string{"https://api.example.com", "https://api2.example.com"},
		TenantID:    "tenant-abc",
		Roles:       []string{"admin", "operator"},
		Groups:      []string{"engineering", "platform"},
		Email:       "user@example.com",
		Name:        "Test User",
		Scope:       "openid profile email mcp:tools",
		ClientID:    "client-xyz",
		MFAVerified: true,
	}

	t.Run("GetSubject", func(t *testing.T) {
		assert.Equal(t, "user-123", claims.GetSubject())
	})

	t.Run("GetIssuer", func(t *testing.T) {
		assert.Equal(t, "https://auth.example.com", claims.GetIssuer())
	})

	t.Run("GetAudience", func(t *testing.T) {
		assert.Equal(t, []string{"https://api.example.com", "https://api2.example.com"}, claims.GetAudience())
	})

	t.Run("GetTenantID", func(t *testing.T) {
		assert.Equal(t, "tenant-abc", claims.GetTenantID())
	})

	t.Run("GetRoles", func(t *testing.T) {
		assert.Equal(t, []string{"admin", "operator"}, claims.GetRoles())
	})

	t.Run("GetGroups", func(t *testing.T) {
		assert.Equal(t, []string{"engineering", "platform"}, claims.GetGroups())
	})

	t.Run("GetEmail", func(t *testing.T) {
		assert.Equal(t, "user@example.com", claims.GetEmail())
	})

	t.Run("GetName", func(t *testing.T) {
		assert.Equal(t, "Test User", claims.GetName())
	})

	t.Run("GetScope", func(t *testing.T) {
		assert.Equal(t, "openid profile email mcp:tools", claims.GetScope())
	})

	t.Run("GetClientID", func(t *testing.T) {
		assert.Equal(t, "client-xyz", claims.GetClientID())
	})

	t.Run("IsMFAVerified", func(t *testing.T) {
		assert.True(t, claims.IsMFAVerified())
	})
}

func TestClaims_Accessors_Empty(t *testing.T) {
	claims := &Claims{}

	assert.Equal(t, "", claims.GetSubject())
	assert.Equal(t, "", claims.GetIssuer())
	assert.Nil(t, claims.GetAudience())
	assert.Equal(t, "", claims.GetTenantID())
	assert.Nil(t, claims.GetRoles())
	assert.Nil(t, claims.GetGroups())
	assert.Equal(t, "", claims.GetEmail())
	assert.Equal(t, "", claims.GetName())
	assert.Equal(t, "", claims.GetScope())
	assert.Equal(t, "", claims.GetClientID())
	assert.False(t, claims.IsMFAVerified())
}

func TestOAuthASMetadata_Accessors(t *testing.T) {
	metadata := &OAuthASMetadata{
		Issuer:                            "https://auth.example.com",
		AuthorizationEndpoint:             "https://auth.example.com/authorize",
		TokenEndpoint:                     "https://auth.example.com/token",
		JWKSURI:                           "https://auth.example.com/.well-known/jwks.json",
		RegistrationEndpoint:              "https://auth.example.com/register",
		RevocationEndpoint:                "https://auth.example.com/revoke",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "client_credentials"},
		ScopesSupported:                   []string{"openid", "profile", "mcp:tools"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		ResourceIndicatorsSupported:       true,
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	}

	t.Run("GetIssuer", func(t *testing.T) {
		assert.Equal(t, "https://auth.example.com", metadata.GetIssuer())
	})

	t.Run("GetAuthorizationEndpoint", func(t *testing.T) {
		assert.Equal(t, "https://auth.example.com/authorize", metadata.GetAuthorizationEndpoint())
	})

	t.Run("GetTokenEndpoint", func(t *testing.T) {
		assert.Equal(t, "https://auth.example.com/token", metadata.GetTokenEndpoint())
	})

	t.Run("GetJWKSURI", func(t *testing.T) {
		assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", metadata.GetJWKSURI())
	})

	t.Run("GetRegistrationEndpoint", func(t *testing.T) {
		assert.Equal(t, "https://auth.example.com/register", metadata.GetRegistrationEndpoint())
	})

	t.Run("GetRevocationEndpoint", func(t *testing.T) {
		assert.Equal(t, "https://auth.example.com/revoke", metadata.GetRevocationEndpoint())
	})

	t.Run("GetResponseTypesSupported", func(t *testing.T) {
		assert.Equal(t, []string{"code"}, metadata.GetResponseTypesSupported())
	})

	t.Run("GetGrantTypesSupported", func(t *testing.T) {
		assert.Equal(t, []string{"authorization_code", "client_credentials"}, metadata.GetGrantTypesSupported())
	})

	t.Run("GetScopesSupported", func(t *testing.T) {
		assert.Equal(t, []string{"openid", "profile", "mcp:tools"}, metadata.GetScopesSupported())
	})

	t.Run("GetCodeChallengeMethodsSupported", func(t *testing.T) {
		assert.Equal(t, []string{"S256"}, metadata.GetCodeChallengeMethodsSupported())
	})

	t.Run("IsResourceIndicatorsSupported", func(t *testing.T) {
		assert.True(t, metadata.IsResourceIndicatorsSupported())
	})
}

func TestOAuthASMetadata_Accessors_Empty(t *testing.T) {
	metadata := &OAuthASMetadata{}

	assert.Equal(t, "", metadata.GetIssuer())
	assert.Equal(t, "", metadata.GetAuthorizationEndpoint())
	assert.Equal(t, "", metadata.GetTokenEndpoint())
	assert.Equal(t, "", metadata.GetJWKSURI())
	assert.Equal(t, "", metadata.GetRegistrationEndpoint())
	assert.Equal(t, "", metadata.GetRevocationEndpoint())
	assert.Nil(t, metadata.GetResponseTypesSupported())
	assert.Nil(t, metadata.GetGrantTypesSupported())
	assert.Nil(t, metadata.GetScopesSupported())
	assert.Nil(t, metadata.GetCodeChallengeMethodsSupported())
	assert.False(t, metadata.IsResourceIndicatorsSupported())
}
