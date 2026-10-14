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

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// routedCostSettings is a cost settings store over the context-routed KV a
// deployed node runs, with no price table.
type routedCostSettings struct {
	store orchestration.CostSettingsStore
}

func (s routedCostSettings) CostSettingsStore() orchestration.CostSettingsStore { return s.store }

func (s routedCostSettings) PriceBook() *orchestration.PriceBook { return nil }

// callerContext is an authenticated request of tenant: both tenant keys name
// the caller's own tenant, as the transports' auth layer sets them.
func callerContext(tenant string) context.Context {
	return orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), tenant), tenant)
}

// TestCostSettingsAreStoredInTheTargetTenantsNamespace asserts t1's admin —
// authenticated on pkg/iam's key only, the shape the REST path hands the
// service — setting t1's electricity rate writes it where t1's turns are
// priced from: t1's namespace, not the system's.
func TestCostSettingsAreStoredInTheTargetTenantsNamespace(t *testing.T) {
	kv := newTenantNamespacedKV()
	source := routedCostSettings{store: orchestration.NewCostSettingsStore(kv)}
	rate := 0.21

	caller := tenantctx.WithTenant(context.Background(), "t1")
	report, err := SetCostSettings(caller, source, "t1", orchestration.CostSettings{
		ElectricityPricePerKWh: &rate, EnginePowerWatts: map[string]float64{"openvino": 12},
	})
	if err != nil {
		t.Fatalf("SetCostSettings: %v", err)
	}
	if report.Electricity.PricePerKWh != rate || report.Electricity.Source != orchestration.ElectricityFromTenantSettings {
		t.Fatalf("rate = %+v, want t1's $0.21 from its settings", report.Electricity)
	}
	if keys := kv.keysIn(t, qrdbsdk.SystemTenantID, "costsettings"); len(keys) != 0 {
		t.Fatalf("system namespace holds %v; t1's settings must not be written there", keys)
	}
	if keys := kv.keysIn(t, "t1", "costsettings"); len(keys) != 1 {
		t.Fatalf("t1 namespace holds %v, want t1's settings", keys)
	}

	back, err := GetCostSettings(caller, source, "t1")
	if err != nil || back.Settings.EnginePowerWatts["openvino"] != 12 {
		t.Fatalf("t1 read of t1 = %+v, %v; want t1's settings", back, err)
	}
}

// TestCostSettingsDefaultRateWithNothingSet asserts a tenant that set nothing
// is costed at the default electricity rate.
func TestCostSettingsDefaultRateWithNothingSet(t *testing.T) {
	source := routedCostSettings{store: orchestration.NewInMemoryCostSettingsStore()}
	report, err := GetCostSettings(callerContext("t1"), source, "t1")
	if err != nil {
		t.Fatalf("GetCostSettings: %v", err)
	}
	if report.Electricity.PricePerKWh != 0.12 || report.Electricity.Source != orchestration.ElectricityFromDefault {
		t.Fatalf("rate = %+v, want the $0.12 default", report.Electricity)
	}
}

func TestCostSettingsRequireATenantAndAStore(t *testing.T) {
	source := routedCostSettings{store: orchestration.NewInMemoryCostSettingsStore()}
	if _, err := GetCostSettings(tenantctx.WithSystem(context.Background()), source, ""); !errors.Is(err, orchestration.ErrCostSettingsInvalid) {
		t.Fatalf("get with no tenant = %v, want ErrCostSettingsInvalid", err)
	}
	if _, err := SetCostSettings(tenantctx.WithSystem(context.Background()), source, "", orchestration.CostSettings{}); !errors.Is(err, orchestration.ErrCostSettingsInvalid) {
		t.Fatalf("set with no tenant = %v, want ErrCostSettingsInvalid", err)
	}
	if _, err := GetCostSettings(callerContext("t1"), routedCostSettings{}, "t1"); !errors.Is(err, orchestration.ErrCostSettingsStore) {
		t.Fatalf("get with no store = %v, want ErrCostSettingsStore", err)
	}
	if _, err := SetCostSettings(callerContext("t1"), routedCostSettings{}, "t1", orchestration.CostSettings{}); !errors.Is(err, orchestration.ErrCostSettingsStore) {
		t.Fatalf("set with no store = %v, want ErrCostSettingsStore", err)
	}
}

// TestCostSettingsRefuseAnotherTenantsCaller is the go-quicbaas#1058 review P1
// for this API: a caller of tenant A holding the write permission may not
// read or replace tenant B's settings. The check is made against the caller's
// own identity BEFORE the context is switched to B, after which every scope
// check would pass. Since #1150 the system tenant is refused the same way.
func TestCostSettingsRefuseAnotherTenantsCaller(t *testing.T) {
	store := orchestration.NewInMemoryCostSettingsStore()
	source := routedCostSettings{store: store}
	rate := 0.5

	// Neither another tenant's caller nor the system tenant may name tenant-b:
	// the system tenant gets no cross-tenant lift (#1150).
	for _, caller := range []string{"tenant-a", qrdbsdk.SystemTenantID} {
		_, err := SetCostSettings(callerContext(caller), source, "tenant-b", orchestration.CostSettings{ElectricityPricePerKWh: &rate})
		var denied *TenantTargetDeniedError
		if !errors.As(err, &denied) || !errors.Is(err, orchestration.ErrAccountTenantScope) {
			t.Fatalf("%s set of tenant-b = %v, want a TenantTargetDeniedError", caller, err)
		}
		if !strings.Contains(denied.Error(), caller) {
			t.Fatalf("error %q does not name the caller %q", denied.Error(), caller)
		}
		if _, err := GetCostSettings(callerContext(caller), source, "tenant-b"); !errors.As(err, &denied) {
			t.Fatalf("%s get of tenant-b = %v, want a TenantTargetDeniedError", caller, err)
		}
	}
	saved, err := store.Get(callerContext("tenant-b"), "tenant-b")
	if err != nil || !saved.Empty() {
		t.Fatalf("tenant-b settings = %+v, %v; the refused writes must not land", saved, err)
	}

	// A caller of the target tenant is allowed.
	if _, err := SetCostSettings(callerContext("tenant-b"), source, "tenant-b", orchestration.CostSettings{ElectricityPricePerKWh: &rate}); err != nil {
		t.Fatalf("own-tenant set: %v", err)
	}
	if _, err := GetCostSettings(callerContext("tenant-b"), source, "tenant-b"); err != nil {
		t.Fatalf("own-tenant get: %v", err)
	}
}
