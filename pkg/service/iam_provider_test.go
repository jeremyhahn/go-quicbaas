//go:build iam

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// SPDX-License-Identifier: GPL-3.0-only
// See the LICENSE file in the root of this repository.

package service

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockIAMComponents struct {
	tenantManager       iam.TenantManager
	identityStore       *iam.IdentityStore
	policyEngine        *iam.PolicyEngine
	appManager          *iam.AppManager
	oidcProvider        *iam.OIDCProvider
	federationManager   iam.FederationManager
	mfaManager          MFAManager
	webAuthnMgr         WebAuthnManager
	auditLogger         iam.AuditLogger
	sessionStorage      iam.SessionStorage
	sessionManager      iam.SessionManager
	adminManager        *iam.AdminManager
	gdprManager         *iam.GDPRManager
	authManager         AuthManagerInterface
	internalAuthManager interfaces.AuthManager
}

var _ IAMComponents = (*mockIAMComponents)(nil)

func TestIAMServiceAdapter_GetTenantManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	tm, err := adapter.GetTenantManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, tm)
}

func TestIAMServiceAdapter_GetTenantManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	tm, err := adapter.GetTenantManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, tm)
}

func TestIAMServiceAdapter_GetIdentityStore_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	is, err := adapter.GetIdentityStore(context.Background())
	require.NoError(t, err)
	assert.Nil(t, is)
}

func TestIAMServiceAdapter_GetIdentityStore_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	is, err := adapter.GetIdentityStore(context.Background())
	require.NoError(t, err)
	assert.Nil(t, is)
}

func TestIAMServiceAdapter_GetPolicyEngine_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	pe, err := adapter.GetPolicyEngine(context.Background())
	require.NoError(t, err)
	assert.Nil(t, pe)
}

func TestIAMServiceAdapter_GetPolicyEngine_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	pe, err := adapter.GetPolicyEngine(context.Background())
	require.NoError(t, err)
	assert.Nil(t, pe)
}

func TestIAMServiceAdapter_GetAppManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	am, err := adapter.GetAppManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestIAMServiceAdapter_GetAppManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	am, err := adapter.GetAppManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestIAMServiceAdapter_GetOIDCProvider_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	op, err := adapter.GetOIDCProvider(context.Background())
	require.NoError(t, err)
	assert.Nil(t, op)
}

func TestIAMServiceAdapter_GetOIDCProvider_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	op, err := adapter.GetOIDCProvider(context.Background())
	require.NoError(t, err)
	assert.Nil(t, op)
}

func TestIAMServiceAdapter_GetFederationManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	fm, err := adapter.GetFederationManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, fm)
}

func TestIAMServiceAdapter_GetFederationManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	fm, err := adapter.GetFederationManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, fm)
}

func TestIAMServiceAdapter_GetMFAManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	mm, err := adapter.GetMFAManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, mm)
}

func TestIAMServiceAdapter_GetMFAManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	mm, err := adapter.GetMFAManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, mm)
}

func TestIAMServiceAdapter_GetWebAuthnManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	wam, err := adapter.GetWebAuthnManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, wam)
}

func TestIAMServiceAdapter_GetWebAuthnManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	wam, err := adapter.GetWebAuthnManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, wam)
}

func TestIAMServiceAdapter_GetAuditLogger_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	al, err := adapter.GetAuditLogger(context.Background())
	require.NoError(t, err)
	assert.Nil(t, al)
}

func TestIAMServiceAdapter_GetAuditLogger_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	al, err := adapter.GetAuditLogger(context.Background())
	require.NoError(t, err)
	assert.Nil(t, al)
}

func TestIAMServiceAdapter_GetSessionStorage_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	ss, err := adapter.GetSessionStorage(context.Background())
	require.NoError(t, err)
	assert.Nil(t, ss)
}

func TestIAMServiceAdapter_GetSessionStorage_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	ss, err := adapter.GetSessionStorage(context.Background())
	require.NoError(t, err)
	assert.Nil(t, ss)
}

func TestIAMServiceAdapter_GetSessionManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	sm, err := adapter.GetSessionManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, sm)
}

func TestIAMServiceAdapter_GetSessionManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	sm, err := adapter.GetSessionManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, sm)
}

func TestIAMServiceAdapter_GetAdminManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	am, err := adapter.GetAdminManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestIAMServiceAdapter_GetAdminManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	am, err := adapter.GetAdminManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestIAMServiceAdapter_GetGDPRManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	gm, err := adapter.GetGDPRManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, gm)
}

func TestIAMServiceAdapter_GetGDPRManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	gm, err := adapter.GetGDPRManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, gm)
}

func TestIAMServiceAdapter_GetAuthManager_WithComponents(t *testing.T) {
	components := &mockIAMComponents{}
	adapter := &iamServiceAdapter{components: components}

	am, err := adapter.GetAuthManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestIAMServiceAdapter_GetAuthManager_NilComponents(t *testing.T) {
	adapter := &iamServiceAdapter{components: nil}

	am, err := adapter.GetAuthManager(context.Background())
	require.NoError(t, err)
	assert.Nil(t, am)
}

type mockIAMServiceProvider struct {
	tenantMgr      iam.TenantManager
	identityStore  *iam.IdentityStore
	policyEngine   *iam.PolicyEngine
	appMgr         *iam.AppManager
	appStorage     iam.AppStorage
	oidcProvider   *iam.OIDCProvider
	federationMgr  iam.FederationManager
	mfaMgr         MFAManager
	webAuthnMgr    WebAuthnManager
	auditLogger    iam.AuditLogger
	sessionStorage iam.SessionStorage
	sessionMgr     SessionManager
	adminMgr       *iam.AdminManager
	gdprMgr        *iam.GDPRManager
	authMgr        AuthManagerInterface

	tenantMgrErr      error
	identityStoreErr  error
	policyEngineErr   error
	appMgrErr         error
	appStorageErr     error
	oidcProviderErr   error
	federationMgrErr  error
	mfaMgrErr         error
	webAuthnMgrErr    error
	auditLoggerErr    error
	sessionStorageErr error
	sessionMgrErr     error
	adminMgrErr       error
	gdprMgrErr        error
	authMgrErr        error
}

type mockTenantManager struct {
	createFn func(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error)
	getFn    func(ctx context.Context, id string) (*iam.Tenant, error)
	updateFn func(ctx context.Context, id string, opts iam.UpdateTenantOptions) (*iam.Tenant, error)
	deleteFn func(ctx context.Context, id string) error
	listFn   func(ctx context.Context, opts iam.ListOptions) ([]*iam.Tenant, error)
}

type testIAMError struct {
	msg string
}
