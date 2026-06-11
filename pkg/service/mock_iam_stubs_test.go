// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

// This file contains missing method stubs for IAM mock types.

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// mockIAMServiceProvider implements IAMServiceProvider
func (p *mockIAMServiceProvider) GetTenantManager(_ context.Context) (iam.TenantManager, error) {
	return p.tenantMgr, p.tenantMgrErr
}
func (p *mockIAMServiceProvider) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return p.identityStore, p.identityStoreErr
}
func (p *mockIAMServiceProvider) GetPolicyEngine(_ context.Context) (*iam.PolicyEngine, error) {
	return p.policyEngine, p.policyEngineErr
}
func (p *mockIAMServiceProvider) GetAppManager(_ context.Context) (*iam.AppManager, error) {
	return p.appMgr, p.appMgrErr
}
func (p *mockIAMServiceProvider) GetAppStorage(_ context.Context) (iam.AppStorage, error) {
	return p.appStorage, p.appStorageErr
}
func (p *mockIAMServiceProvider) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return p.oidcProvider, p.oidcProviderErr
}
func (p *mockIAMServiceProvider) GetFederationManager(_ context.Context) (iam.FederationManager, error) {
	return p.federationMgr, p.federationMgrErr
}
func (p *mockIAMServiceProvider) GetMFAManager(_ context.Context) (MFAManager, error) {
	return p.mfaMgr, p.mfaMgrErr
}
func (p *mockIAMServiceProvider) GetWebAuthnManager(_ context.Context) (WebAuthnManager, error) {
	return p.webAuthnMgr, p.webAuthnMgrErr
}
func (p *mockIAMServiceProvider) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return p.auditLogger, p.auditLoggerErr
}
func (p *mockIAMServiceProvider) GetSessionStorage(_ context.Context) (iam.SessionStorage, error) {
	return p.sessionStorage, p.sessionStorageErr
}
func (p *mockIAMServiceProvider) GetSessionManager(_ context.Context) (SessionManager, error) {
	return p.sessionMgr, p.sessionMgrErr
}
func (p *mockIAMServiceProvider) GetAdminManager(_ context.Context) (*iam.AdminManager, error) {
	return p.adminMgr, p.adminMgrErr
}
func (p *mockIAMServiceProvider) GetGDPRManager(_ context.Context) (*iam.GDPRManager, error) {
	return p.gdprMgr, p.gdprMgrErr
}
func (p *mockIAMServiceProvider) GetAuthManager(_ context.Context) (AuthManagerInterface, error) {
	return p.authMgr, p.authMgrErr
}
