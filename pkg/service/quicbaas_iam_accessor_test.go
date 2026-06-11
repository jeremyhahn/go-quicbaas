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

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// quicBaaS.SetIAMProvider Tests
// =============================================================================

func TestQuicBaaS_SetIAMProvider_SetsProviderWhenNotNil(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	components := &mockIAMComponents{}
	impl := svc.(*quicBaaS)

	impl.SetIAMProvider(components)

	provider := impl.getIAMProvider()
	require.NotNil(t, provider)
}

func TestQuicBaaS_SetIAMProvider_IgnoresNilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)

	impl.SetIAMProvider(nil)

	provider := impl.getIAMProvider()
	assert.Nil(t, provider)
}

// =============================================================================
// quicBaaS.getIAMProvider Tests
// =============================================================================

func TestQuicBaaS_GetIAMProvider_ReturnsNilWhenNotSet(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)

	provider := impl.getIAMProvider()
	assert.Nil(t, provider)
}

// =============================================================================
// quicBaaS IAM Accessor Methods - nil provider returns nil
// =============================================================================

func TestQuicBaaS_GetTenantManager_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	tm, err := impl.GetTenantManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, tm)
}

func TestQuicBaaS_GetTenantManager_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	tm, err := impl.GetTenantManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, tm) // mock returns nil tenant manager
}

func TestQuicBaaS_GetIdentityStore_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	is, err := impl.GetIdentityStore(ctx)
	require.NoError(t, err)
	assert.Nil(t, is)
}

func TestQuicBaaS_GetIdentityStore_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	is, err := impl.GetIdentityStore(ctx)
	require.NoError(t, err)
	assert.Nil(t, is)
}

func TestQuicBaaS_GetPolicyEngine_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	pe, err := impl.GetPolicyEngine(ctx)
	require.NoError(t, err)
	assert.Nil(t, pe)
}

func TestQuicBaaS_GetPolicyEngine_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	pe, err := impl.GetPolicyEngine(ctx)
	require.NoError(t, err)
	assert.Nil(t, pe)
}

func TestQuicBaaS_GetAppManager_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	am, err := impl.GetAppManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestQuicBaaS_GetAppManager_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	am, err := impl.GetAppManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestQuicBaaS_GetOIDCProvider_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	op, err := impl.GetOIDCProvider(ctx)
	require.NoError(t, err)
	assert.Nil(t, op)
}

func TestQuicBaaS_GetOIDCProvider_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	op, err := impl.GetOIDCProvider(ctx)
	require.NoError(t, err)
	assert.Nil(t, op)
}

func TestQuicBaaS_GetMFAManager_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	mm, err := impl.GetMFAManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, mm)
}

func TestQuicBaaS_GetMFAManager_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	mm, err := impl.GetMFAManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, mm)
}

func TestQuicBaaS_GetWebAuthnManager_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	wam, err := impl.GetWebAuthnManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, wam)
}

func TestQuicBaaS_GetWebAuthnManager_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	wam, err := impl.GetWebAuthnManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, wam)
}

func TestQuicBaaS_GetAuditLogger_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	al, err := impl.GetAuditLogger(ctx)
	require.NoError(t, err)
	assert.Nil(t, al)
}

func TestQuicBaaS_GetAuditLogger_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	al, err := impl.GetAuditLogger(ctx)
	require.NoError(t, err)
	assert.Nil(t, al)
}

func TestQuicBaaS_GetSessionStorage_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	ss, err := impl.GetSessionStorage(ctx)
	require.NoError(t, err)
	assert.Nil(t, ss)
}

func TestQuicBaaS_GetSessionStorage_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	ss, err := impl.GetSessionStorage(ctx)
	require.NoError(t, err)
	assert.Nil(t, ss)
}

func TestQuicBaaS_GetSessionManager_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	sm, err := impl.GetSessionManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, sm)
}

func TestQuicBaaS_GetSessionManager_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	sm, err := impl.GetSessionManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, sm)
}

func TestQuicBaaS_GetAdminManager_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	am, err := impl.GetAdminManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestQuicBaaS_GetAdminManager_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	am, err := impl.GetAdminManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, am)
}

func TestQuicBaaS_GetGDPRManager_NilProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	ctx := context.Background()

	gm, err := impl.GetGDPRManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, gm)
}

func TestQuicBaaS_GetGDPRManager_WithProvider(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})
	ctx := context.Background()

	gm, err := impl.GetGDPRManager(ctx)
	require.NoError(t, err)
	assert.Nil(t, gm)
}

// =============================================================================
// quicBaaS.IAM() Tests
// =============================================================================

func TestQuicBaaS_IAM_ReturnsNilWhenProviderNotSet(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)

	iamSvc := impl.IAM()
	assert.Nil(t, iamSvc)
}

func TestQuicBaaS_IAM_ReturnsServiceWhenProviderSet(t *testing.T) {
	svc := createTestService(t)
	defer svc.Close()

	impl := svc.(*quicBaaS)
	impl.SetIAMProvider(&mockIAMComponents{})

	iamSvc := impl.IAM()
	require.NotNil(t, iamSvc)
}
