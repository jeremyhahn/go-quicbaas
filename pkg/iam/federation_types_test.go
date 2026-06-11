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

func TestProviderNotFoundError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &ProviderNotFoundError{}
		assert.Equal(t, "external provider not found", err.Error())
	})
}

func TestProviderAlreadyExistsError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &ProviderAlreadyExistsError{}
		assert.Equal(t, "external provider already exists", err.Error())
	})
}

func TestProviderInvalidError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &ProviderInvalidError{}
		assert.Equal(t, "invalid external provider", err.Error())
	})
}

func TestStateNotFoundError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &StateNotFoundError{}
		assert.Equal(t, "state token not found", err.Error())
	})
}

func TestStateExpiredError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &StateExpiredError{}
		assert.Equal(t, "state token expired", err.Error())
	})
}

func TestStateInvalidError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &StateInvalidError{}
		assert.Equal(t, "invalid state token", err.Error())
	})
}

func TestTokenExchangeFailedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &TokenExchangeFailedError{}
		assert.Equal(t, "token exchange failed", err.Error())
	})
}

func TestClaimsExtractionFailedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &ClaimsExtractionFailedError{}
		assert.Equal(t, "failed to extract claims", err.Error())
	})
}

func TestIdentityNotLinkedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &IdentityNotLinkedError{}
		assert.Equal(t, "federated identity not linked", err.Error())
	})
}

func TestIdentityAlreadyLinkedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &IdentityAlreadyLinkedError{}
		assert.Equal(t, "federated identity already linked", err.Error())
	})
}

func TestAutoProvisionDisabledError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &AutoProvisionDisabledError{}
		assert.Equal(t, "auto-provisioning is disabled for this provider", err.Error())
	})
}

func TestLinkingDisabledError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &LinkingDisabledError{}
		assert.Equal(t, "account linking is disabled for this provider", err.Error())
	})
}

func TestProviderDisabledError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &ProviderDisabledError{}
		assert.Equal(t, "external provider is disabled", err.Error())
	})
}

func TestOIDCDiscoveryFailedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &OIDCDiscoveryFailedError{}
		assert.Equal(t, "OIDC discovery failed", err.Error())
	})
}

func TestFederatedIdentityNotFoundError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &FederatedIdentityNotFoundError{}
		assert.Equal(t, "federated identity not found", err.Error())
	})
}

func TestFederatedIdentityAlreadyLinkedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &FederatedIdentityAlreadyLinkedError{}
		assert.Equal(t, "federated identity already linked to a user", err.Error())
	})
}

func TestAccountLinkingDisabledError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &AccountLinkingDisabledError{}
		assert.Equal(t, "account linking is disabled for this provider", err.Error())
	})
}

func TestJITProvisioningDisabledError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &JITProvisioningDisabledError{}
		assert.Equal(t, "JIT provisioning is disabled for this provider", err.Error())
	})
}

func TestOIDCTokenExchangeFailedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &OIDCTokenExchangeFailedError{}
		assert.Equal(t, "OIDC token exchange failed", err.Error())
	})
}

func TestOIDCUserInfoFailedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &OIDCUserInfoFailedError{}
		assert.Equal(t, "failed to fetch user info from OIDC provider", err.Error())
	})
}

func TestOIDCClaimMappingFailedError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &OIDCClaimMappingFailedError{}
		assert.Equal(t, "failed to map OIDC claims", err.Error())
	})
}

func TestFederationStateNotFoundError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &FederationStateNotFoundError{}
		assert.Equal(t, "federation state not found", err.Error())
	})
}

func TestFederationStateExpiredError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &FederationStateExpiredError{}
		assert.Equal(t, "federation state expired", err.Error())
	})
}
