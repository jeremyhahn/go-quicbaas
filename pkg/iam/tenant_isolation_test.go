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

package iam

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// recordingQRDBProvisioner captures the isolationType passed to the engine so
// tests can assert the BaaS IAM layer forwards (rather than discards) the
// per-tenant isolation selection.
type recordingQRDBProvisioner struct {
	createIsolation string
	createCalled    bool
}

func (p *recordingQRDBProvisioner) CreateTenant(_ context.Context, _, _, isolationType string) error {
	p.createCalled = true
	p.createIsolation = isolationType
	return nil
}

func (p *recordingQRDBProvisioner) DeleteTenant(_ context.Context, _ string) error { return nil }

// TestCreateTenant_BarrierIsolationReachesEngine proves a barrier-isolation
// request set on CreateTenantOptions.IsolationType reaches the qrdb engine
// provisioner as "barrier", so qrdb creates a barrier-isolated tenant (own DEK
// + "{tenantID}/" namespace) instead of silently degrading to prefix.
func TestCreateTenant_BarrierIsolationReachesEngine(t *testing.T) {
	mgr := newTestTenantManager()
	prov := &recordingQRDBProvisioner{}
	mgr.SetQRDBTenantProvisioner(prov)

	opts := CreateTenantOptions{
		ID:            "acme",
		Name:          "Acme",
		IsolationType: qrdbsdk.TenantIsolationBarrier,
	}
	if _, err := mgr.CreateTenant(context.Background(), opts); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if !prov.createCalled {
		t.Fatal("engine provisioner CreateTenant was not called")
	}
	if prov.createIsolation != string(qrdbsdk.TenantIsolationBarrier) {
		t.Fatalf("engine isolationType = %q, want %q", prov.createIsolation, qrdbsdk.TenantIsolationBarrier)
	}
}

// TestCreateTenant_EmptyIsolationReachesEngineAsEmpty proves an unspecified
// isolation type is forwarded as empty, letting qrdb apply its default
// (prefix) per TenantInfo.EffectiveIsolationType rather than the BaaS layer
// hardcoding a value.
func TestCreateTenant_EmptyIsolationReachesEngineAsEmpty(t *testing.T) {
	mgr := newTestTenantManager()
	prov := &recordingQRDBProvisioner{}
	mgr.SetQRDBTenantProvisioner(prov)

	opts := CreateTenantOptions{ID: "acme", Name: "Acme"}
	if _, err := mgr.CreateTenant(context.Background(), opts); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if !prov.createCalled {
		t.Fatal("engine provisioner CreateTenant was not called")
	}
	if prov.createIsolation != "" {
		t.Fatalf("engine isolationType = %q, want empty (qrdb defaults to prefix)", prov.createIsolation)
	}
}
