// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package server

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// qrdbTenantProvisionerAdapter adapts a qrdb ServiceTenantServicer
// (= tenant.TenantServicer) to the iam.QRDBTenantProvisioner interface.
// It is injected into the iam.DefaultTenantManager so that CreateTenant
// and DeleteTenant in the BaaS IAM layer trigger the corresponding
// engine-side lifecycle (per-tenant barrier init, DEK, quota bootstrap)
// atomically before any BaaS-layer metadata is persisted.
type qrdbTenantProvisionerAdapter struct {
	svc qrdbsdk.ServiceTenantServicer
}

// Compile-time assertion.
var _ iam.QRDBTenantProvisioner = (*qrdbTenantProvisionerAdapter)(nil)

// newQRDBTenantProvisionerAdapter wraps svc as an iam.QRDBTenantProvisioner.
func newQRDBTenantProvisionerAdapter(svc qrdbsdk.ServiceTenantServicer) iam.QRDBTenantProvisioner {
	return &qrdbTenantProvisionerAdapter{svc: svc}
}

// CreateTenant calls the qrdb engine's TenantServicer.CreateTenant,
// initialising the per-tenant barrier, DEK, and quota in the engine.
// isolationType is cast to tenant.IsolationType; an empty string uses the
// engine default (IsolationBarrier).
func (a *qrdbTenantProvisionerAdapter) CreateTenant(ctx context.Context, id, name, isolationType string) error {
	_, err := a.svc.CreateTenant(ctx, id, name, qrdbsdk.TenantIsolationType(isolationType))
	return err
}

// DeleteTenant calls the qrdb engine's TenantServicer.DeleteTenant,
// destroying the per-tenant barrier and purging its DEK.
func (a *qrdbTenantProvisionerAdapter) DeleteTenant(ctx context.Context, id string) error {
	return a.svc.DeleteTenant(ctx, id)
}
