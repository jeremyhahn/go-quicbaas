// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package quicbaas

import (
	"context"
	"testing"
)

// sdkOnlyTenantClient implements go-qrdb's TenantClient using only names
// sdk/go exports (#1656): if any request or response type of a TenantClient
// method were not re-exported, this file would not compile.
type sdkOnlyTenantClient struct{}

func (sdkOnlyTenantClient) CreateTenant(context.Context, *QRDBCreateTenantRequest) (*QRDBCreateTenantResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) GetTenant(context.Context, string) (*QRDBGetTenantResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) ListTenants(context.Context) (*QRDBListTenantsResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) ListDeletedTenants(context.Context, *QRDBListDeletedTenantsRequest) (*QRDBListDeletedTenantsResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) ReconcileDeletedTenants(context.Context) (*QRDBReconcileDeletedTenantsResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) DeleteTenant(context.Context, string) error { return nil }
func (sdkOnlyTenantClient) UpdateTenant(context.Context, *QRDBUpdateTenantRequest) (*QRDBUpdateTenantResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) UpdateTenantQuotas(context.Context, *QRDBUpdateTenantQuotasRequest) (*QRDBUpdateTenantQuotasResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) UpdateTenantEntitlements(context.Context, *QRDBUpdateTenantEntitlementsRequest) (*QRDBUpdateTenantEntitlementsResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierInit(context.Context, *QRDBTenantBarrierInitRequest) (*QRDBTenantBarrierInitResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierUnseal(context.Context, *QRDBTenantBarrierUnsealRequest) error {
	return nil
}
func (sdkOnlyTenantClient) TenantBarrierUnsealShare(context.Context, *QRDBTenantBarrierUnsealShareRequest) (*QRDBTenantBarrierUnsealProgress, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierSeal(context.Context, *QRDBTenantBarrierSealRequest) error {
	return nil
}
func (sdkOnlyTenantClient) TenantBarrierStatus(context.Context, *QRDBTenantBarrierStatusRequest) (*QRDBTenantBarrierStatusResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierOfficerRegister(context.Context, *QRDBTenantOfficerRegisterRequest) (*QRDBOfficerRegisterResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierOfficerList(context.Context, string) ([]*QRDBOfficerStatus, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierOfficerClaimStatus(context.Context, string) ([]*QRDBOfficerStatus, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierShareClaimBegin(context.Context, *QRDBTenantShareClaimBeginRequest) (*QRDBShareClaimBeginResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierShareClaimComplete(context.Context, *QRDBTenantShareClaimCompleteRequest) (*QRDBShareClaimCompleteResponse, error) {
	return nil, nil
}
func (sdkOnlyTenantClient) TenantBarrierShamirSealVote(context.Context, *QRDBTenantShamirSealVoteRequest) (*QRDBShamirSealVoteResponse, error) {
	return nil, nil
}

// TestQRDBTenantClient_SurfaceIsReexported proves sdk/go names the whole
// go-qrdb tenant and per-tenant barrier surface: a client written in sdk/go
// names alone satisfies QRDBTenantClient.
func TestQRDBTenantClient_SurfaceIsReexported(t *testing.T) {
	var c QRDBTenantClient = sdkOnlyTenantClient{}
	if c == nil {
		t.Fatal("sdk/go tenant client is nil")
	}
}
