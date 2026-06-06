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

//go:build iam

package service

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// iamServiceAdapter adapts IAMComponents to the IAMServiceProvider interface.
// It wraps IAMComponents and provides access to all IAM managers.
type iamServiceAdapter struct {
	components IAMComponents
}

// Compile-time interface verification.
var _ IAMServiceProvider = (*iamServiceAdapter)(nil)

// GetTenantManager returns the tenant manager from IAM components.
// Returns iam.TenantManager directly for use by REST handlers.
func (p *iamServiceAdapter) GetTenantManager(_ context.Context) (iam.TenantManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetTenantManager(), nil
}

// GetIdentityStore returns the identity store from IAM components.
func (p *iamServiceAdapter) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetIdentityStore(), nil
}

// GetPolicyEngine returns the policy engine from IAM components.
func (p *iamServiceAdapter) GetPolicyEngine(_ context.Context) (*iam.PolicyEngine, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetPolicyEngine(), nil
}

// GetAppManager returns the app manager from IAM components.
func (p *iamServiceAdapter) GetAppManager(_ context.Context) (*iam.AppManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetAppManager(), nil
}

// GetAppStorage returns the underlying OAuth2 application storage backend.
func (p *iamServiceAdapter) GetAppStorage(_ context.Context) (iam.AppStorage, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetAppStorage(), nil
}

// GetOIDCProvider returns the OIDC provider from IAM components.
func (p *iamServiceAdapter) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetOIDCProvider(), nil
}

// GetFederationManager returns the federation manager from IAM components.
func (p *iamServiceAdapter) GetFederationManager(_ context.Context) (iam.FederationManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetFederationManager(), nil
}

// GetMFAManager returns the MFA manager from IAM components.
func (p *iamServiceAdapter) GetMFAManager(_ context.Context) (MFAManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetMFAManager(), nil
}

// GetWebAuthnManager returns the WebAuthn manager from IAM components.
func (p *iamServiceAdapter) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetWebAuthnManager(), nil
}

// GetAuditLogger returns the audit logger from IAM components.
func (p *iamServiceAdapter) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetAuditLogger(), nil
}

// GetSessionStorage returns the session storage from IAM components.
func (p *iamServiceAdapter) GetSessionStorage(_ context.Context) (iam.SessionStorage, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetSessionStorage(), nil
}

// GetSessionManager returns the session manager from IAM components.
func (p *iamServiceAdapter) GetSessionManager(_ context.Context) (SessionManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetSessionManager(), nil
}

// GetAdminManager returns the admin manager from IAM components.
func (p *iamServiceAdapter) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetAdminManager(), nil
}

// GetGDPRManager returns the GDPR manager from IAM components.
func (p *iamServiceAdapter) GetGDPRManager(_ context.Context) (*iam.GDPRManager, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetGDPRManager(), nil
}

// GetAuthManager returns the authentication manager from IAM components.
// Returns nil if password authentication is not enabled.
func (p *iamServiceAdapter) GetAuthManager(_ context.Context) (AuthManagerInterface, error) {
	if p.components == nil {
		return nil, nil
	}
	return p.components.GetAuthManager(), nil
}

// initializeIAMProvider initializes the IAM provider for REST handlers.
// This is the iam version that returns nil because IAM components
// are initialized separately by the server and provided via SetIAMProvider().
// This two-phase initialization is required because IAM components depend on
// database storage which may not be available during service construction.
func initializeIAMProvider(_ *serviceBuilder) IAMServiceProvider {
	// IAM components are provided externally via SetIAMProvider() after
	// the server has initialized its dependencies. This deferred initialization
	// allows the IAM subsystem to use the database for persistent storage.
	return nil
}

// initializeIAMService creates the IAM service provider if IAM is enabled.
// This is the iam version that returns a nil provider since
// IAM components are accessed through iamProvider.
func initializeIAMService(_ *serviceBuilder) (*iamServiceProvider, error) {
	// IAM components are accessed through iamProvider field,
	// so this returns nil to satisfy the interface.
	return nil, nil
}
