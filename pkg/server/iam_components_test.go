//go:build raft && iam

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ================================================================================
// IAMComponents Getter Tests - Cover all getter methods
// ================================================================================

func TestIAMComponents_Getters(t *testing.T) {
	// Create iamComponents with nil values (simulating uninitialized state)
	components := &iamComponents{}

	t.Run("GetTenantManager_Nil", func(t *testing.T) {
		result := components.GetTenantManager()
		assert.Nil(t, result)
	})

	t.Run("GetIdentityStore_Nil", func(t *testing.T) {
		result := components.GetIdentityStore()
		assert.Nil(t, result)
	})

	t.Run("GetPolicyEngine_Nil", func(t *testing.T) {
		result := components.GetPolicyEngine()
		assert.Nil(t, result)
	})

	t.Run("GetAppManager_Nil", func(t *testing.T) {
		result := components.GetAppManager()
		assert.Nil(t, result)
	})

	t.Run("GetOIDCProvider_Nil", func(t *testing.T) {
		result := components.GetOIDCProvider()
		assert.Nil(t, result)
	})

	t.Run("GetMFAManager_Nil", func(t *testing.T) {
		result := components.GetMFAManager()
		assert.Nil(t, result)
	})

	t.Run("GetWebAuthnManager_Nil", func(t *testing.T) {
		result := components.GetWebAuthnManager()
		assert.Nil(t, result)
	})

	t.Run("GetAuditLogger_Nil", func(t *testing.T) {
		result := components.GetAuditLogger()
		assert.Nil(t, result)
	})

	t.Run("GetAdminManager_Nil", func(t *testing.T) {
		result := components.GetAdminManager()
		assert.Nil(t, result)
	})

	t.Run("GetGDPRManager_Nil", func(t *testing.T) {
		result := components.GetGDPRManager()
		assert.Nil(t, result)
	})

	t.Run("GetSessionStorage_Nil", func(t *testing.T) {
		result := components.GetSessionStorage()
		assert.Nil(t, result)
	})

	t.Run("GetSessionManager_Nil", func(t *testing.T) {
		result := components.GetSessionManager()
		assert.Nil(t, result)
	})

	t.Run("GetAuthManager_Nil", func(t *testing.T) {
		result := components.GetAuthManager()
		assert.Nil(t, result)
	})
}

// ================================================================================
// getIssuerURL Tests
// ================================================================================

func TestServer_GetIssuerURL(t *testing.T) {
	t.Run("HTTPWithLocalhost", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.TLSEnabled = false
		cfg.Server.Host = ""
		cfg.Server.REST.Port = 8080

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		url := srv.getIssuerURL()
		assert.Contains(t, url, "http://")
		assert.Contains(t, url, "localhost")
		assert.Contains(t, url, "8080")
	})

	t.Run("HTTPSWithSpecificHost", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.TLSEnabled = true
		cfg.Server.Host = "example.com"
		cfg.Server.REST.Port = 8443

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		url := srv.getIssuerURL()
		assert.Contains(t, url, "https://")
		assert.Contains(t, url, "example.com")
		assert.Contains(t, url, "8443")
	})

	t.Run("DefaultPort", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.TLSEnabled = true
		cfg.Server.Host = "test.com"
		cfg.Server.REST.Port = 0

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		url := srv.getIssuerURL()
		assert.Contains(t, url, "https://")
		assert.Contains(t, url, "8443")
	})

	t.Run("ZeroZeroZeroZeroHost", func(t *testing.T) {
		cfg := createTestConfig(t)
		defer cleanupTestData(t, cfg)

		cfg.Security.TLSEnabled = false
		cfg.Server.Host = "0.0.0.0"
		cfg.Server.REST.Port = 9000

		srv, err := NewServer(cfg)
		require.NoError(t, err)

		url := srv.getIssuerURL()
		assert.Contains(t, url, "localhost")
	})
}

// ================================================================================
// Session Storage Mock for coverage
// ================================================================================

type mockSessionStorage struct {
	sessions map[string]*iam.Session
}

func newMockSessionStorage() *mockSessionStorage {
	return &mockSessionStorage{
		sessions: make(map[string]*iam.Session),
	}
}
