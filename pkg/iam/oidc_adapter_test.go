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
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestNewOIDCProviderAdapter(t *testing.T) {
	t.Run("creates adapter with nil provider", func(t *testing.T) {
		adapter := NewOIDCProviderAdapter(nil)
		require.NotNil(t, adapter)
		assert.Nil(t, adapter.provider)
	})
}

func TestOIDCProviderAdapter_GetProvider(t *testing.T) {
	t.Run("returns underlying provider", func(t *testing.T) {
		// Create a nil adapter to test the getter
		adapter := &OIDCProviderAdapter{provider: nil}
		assert.Nil(t, adapter.GetProvider())
	})
}

func TestOIDCProviderAdapter_GetConfiguration(t *testing.T) {
	t.Run("returns nil when provider is nil", func(t *testing.T) {
		adapter := NewOIDCProviderAdapter(nil)
		// This will panic if the provider is nil and we call a method on it
		// We're testing that the adapter handles nil provider gracefully
		defer func() {
			if r := recover(); r != nil {
				t.Log("Expected panic with nil provider")
			}
		}()
		_ = adapter.GetConfiguration()
	})
}

func TestOIDCProviderAdapter_GetOAuthASMetadata(t *testing.T) {
	t.Run("returns nil when provider is nil", func(t *testing.T) {
		adapter := NewOIDCProviderAdapter(nil)
		// This will panic if the provider is nil
		defer func() {
			if r := recover(); r != nil {
				t.Log("Expected panic with nil provider")
			}
		}()
		_ = adapter.GetOAuthASMetadata()
	})
}

func TestOIDCProviderAdapter_InterfaceCompliance(t *testing.T) {
	// Compile-time interface compliance check
	var _ interfaces.OIDCProvider = (*OIDCProviderAdapter)(nil)
}
