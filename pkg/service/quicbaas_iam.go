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
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// SetIAMProvider sets the IAM components provider for REST handlers.
// This is called by the server after IAM components are initialized.
// It also wires up the auth manager from IAM components to enable
// authentication operations via MCP and other transports.
func (s *quicBaaS) SetIAMProvider(provider IAMComponents) {
	if provider != nil {
		s.iamProvider = &iamServiceAdapter{components: provider}

		// Wire up the auth manager from IAM components for MCP transport and other
		// internal operations that require the full interfaces.AuthManager interface.
		// This enables Login/Logout/RefreshToken/ValidateToken operations via MCP.
		if authMgr := provider.GetInternalAuthManager(); authMgr != nil {
			s.authManager = authMgr
		}
	}
}

// getIAMProvider returns the IAM service provider.
func (s *quicBaaS) getIAMProvider() IAMServiceProvider {
	return s.iamProvider
}

// GetInternalAuthManager returns the internal auth manager for MCP and other
// internal operations that require the full interfaces.AuthManager interface.
// This method enables the MCP server to validate tokens for authenticated operations.
func (s *quicBaaS) GetInternalAuthManager() interfaces.AuthManager {
	return s.authManager
}

// GetTenantManager returns the tenant manager from IAM components.
// This method satisfies the interface expected by REST handlers.
func (s *quicBaaS) GetTenantManager(ctx context.Context) (iam.TenantManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetTenantManager(ctx)
}

// GetIdentityStore returns the identity store from IAM components.
func (s *quicBaaS) GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetIdentityStore(ctx)
}

// GetPolicyEngine returns the policy engine from IAM components.
func (s *quicBaaS) GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetPolicyEngine(ctx)
}

// GetAppManager returns the app manager from IAM components.
func (s *quicBaaS) GetAppManager(ctx context.Context) (*iam.AppManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetAppManager(ctx)
}

// GetAppStorage returns the underlying OAuth2 application storage backend.
// Used by lifecycle_iam_mtls.go to wire self_signed and PKI authenticators
// without a runtime interface assertion.
func (s *quicBaaS) GetAppStorage(ctx context.Context) (iam.AppStorage, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetAppStorage(ctx)
}

// GetOIDCProvider returns the OIDC provider from IAM components.
func (s *quicBaaS) GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetOIDCProvider(ctx)
}

// GetFederationManager returns the federation manager from IAM components.
// This method satisfies the interface expected by MCP and REST handlers
// for federation provider management and account linking operations.
func (s *quicBaaS) GetFederationManager(ctx context.Context) (iam.FederationManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetFederationManager(ctx)
}

// GetMFAManager returns the MFA manager from IAM components.
func (s *quicBaaS) GetMFAManager(ctx context.Context) (MFAManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetMFAManager(ctx)
}

// GetWebAuthnManager returns the WebAuthn manager from IAM components.
func (s *quicBaaS) GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetWebAuthnManager(ctx)
}

// GetAuditLogger returns the audit logger from IAM components.
func (s *quicBaaS) GetAuditLogger(ctx context.Context) (iam.AuditLogger, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetAuditLogger(ctx)
}

// GetSessionStorage returns the session storage from IAM components.
func (s *quicBaaS) GetSessionStorage(ctx context.Context) (iam.SessionStorage, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetSessionStorage(ctx)
}

// GetSessionManager returns the session manager from IAM components.
func (s *quicBaaS) GetSessionManager(ctx context.Context) (SessionManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetSessionManager(ctx)
}

// GetAdminManager returns the admin manager from IAM components.
// This method satisfies the interface expected by REST handlers for delegated administration.
func (s *quicBaaS) GetAdminManager(ctx context.Context) (*iam.AdminManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetAdminManager(ctx)
}

// GetGDPRManager returns the GDPR manager from IAM components.
// This method satisfies the interface expected by REST handlers for GDPR compliance.
func (s *quicBaaS) GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error) {
	provider := s.getIAMProvider()
	if provider == nil {
		return nil, nil
	}
	return provider.GetGDPRManager(ctx)
}

