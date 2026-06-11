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
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// OIDCProviderAdapter wraps OIDCProvider to implement interfaces.OIDCProvider.
// This adapter enables MCP and other services to validate tokens without
// direct dependency on the concrete IAM types.
type OIDCProviderAdapter struct {
	provider *OIDCProvider
}

// NewOIDCProviderAdapter creates a new adapter wrapping the given OIDCProvider.
func NewOIDCProviderAdapter(provider *OIDCProvider) *OIDCProviderAdapter {
	return &OIDCProviderAdapter{provider: provider}
}

// ValidateAccessToken validates an access token and returns its claims.
func (a *OIDCProviderAdapter) ValidateAccessToken(ctx context.Context, tokenString string) (interfaces.OIDCClaims, error) {
	claims, err := a.provider.ValidateAccessToken(ctx, tokenString)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// GetConfiguration returns the OIDC discovery configuration as a map.
func (a *OIDCProviderAdapter) GetConfiguration() map[string]interface{} {
	return a.provider.GetConfiguration()
}

// GetOAuthASMetadata returns the OAuth 2.0 Authorization Server Metadata.
func (a *OIDCProviderAdapter) GetOAuthASMetadata() interfaces.OAuthASMetadata {
	metadata := a.provider.GetOAuthASMetadata()
	if metadata == nil {
		return nil
	}
	return metadata
}

// GetProvider returns the underlying OIDCProvider.
// This is useful when direct access to the concrete provider is needed.
func (a *OIDCProviderAdapter) GetProvider() *OIDCProvider {
	return a.provider
}

// Compile-time interface compliance check.
var _ interfaces.OIDCProvider = (*OIDCProviderAdapter)(nil)
