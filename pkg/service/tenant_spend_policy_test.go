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
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// routedSpendSource is a tenant spend-policy store over the context-routed KV
// a deployed node runs, with no ledger.
type routedSpendSource struct {
	store orchestration.TenantSpendPolicyStore
}

func (s routedSpendSource) TenantSpendPolicies() orchestration.TenantSpendPolicyStore { return s.store }

func (s routedSpendSource) ConsumptionLedger() orchestration.ConsumptionLedger { return nil }

// adminCallerContext is a system admin's authenticated request: both tenant
// keys name the admin's own tenant, as the transports' auth layer sets them.
func adminCallerContext() context.Context {
	return callerContext(qrdbsdk.SystemTenantID)
}

// TestTenantSpendPolicyIsStoredInTheTargetTenantsNamespace is the
// go-quicbaas#1058 review regression: a system admin setting tenant t1's
// limits wrote them into the ADMIN's namespace, because only the
// orchestration tenant key was scoped and storage routes by the IAM key first.
// t1's own admission reads t1's namespace and would never have seen them.
func TestTenantSpendPolicyIsStoredInTheTargetTenantsNamespace(t *testing.T) {
	kv := newTenantNamespacedKV()
	source := routedSpendSource{store: orchestration.NewTenantSpendPolicyStore(kv)}
	limits := orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
		{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 2_000_000_000},
	}}

	report, err := SetTenantSpendPolicy(adminCallerContext(), source, "t1", limits)
	if err != nil {
		t.Fatalf("SetTenantSpendPolicy: %v", err)
	}
	if report.TenantID != "t1" || len(report.Limits.Windows) != 1 {
		t.Fatalf("report = %+v, want t1's monthly window", report)
	}

	if keys := kv.keysIn(t, qrdbsdk.SystemTenantID, "spendpolicy"); len(keys) != 0 {
		t.Fatalf("admin namespace holds %v; t1's policy must not be written there", keys)
	}
	if keys := kv.keysIn(t, "t1", "spendpolicy"); len(keys) != 1 {
		t.Fatalf("t1 namespace holds %v, want t1's policy", keys)
	}

	// t1 reading its own limits, as its admission does, finds them.
	t1Ctx := orchestration.ContextWithTenant(iam.ContextWithTenant(context.Background(), "t1"), "t1")
	own, err := GetTenantSpendPolicy(t1Ctx, source, "t1")
	if err != nil {
		t.Fatalf("GetTenantSpendPolicy as t1: %v", err)
	}
	if len(own.Limits.Windows) != 1 || own.Limits.Windows[0].MaxEquivalentMicros != 2_000_000_000 {
		t.Fatalf("t1 reads %+v, want the $2,000 monthly window", own.Limits)
	}

	// And the admin reading t1 back reads t1's namespace, not its own.
	back, err := GetTenantSpendPolicy(adminCallerContext(), source, "t1")
	if err != nil || len(back.Limits.Windows) != 1 {
		t.Fatalf("admin read of t1 = %+v, %v; want t1's window", back, err)
	}
}

// TestTenantSpendPolicyRequiresATenant asserts neither verb runs unscoped.
func TestTenantSpendPolicyRequiresATenant(t *testing.T) {
	source := routedSpendSource{store: orchestration.NewInMemoryTenantSpendPolicyStore()}
	if _, err := GetTenantSpendPolicy(context.Background(), source, ""); !errors.Is(err, orchestration.ErrTenantSpendPolicyInvalid) {
		t.Fatalf("get with no tenant = %v, want ErrTenantSpendPolicyInvalid", err)
	}
	if _, err := SetTenantSpendPolicy(context.Background(), source, "", orchestration.SpendLimits{}); !errors.Is(err, orchestration.ErrTenantSpendPolicyInvalid) {
		t.Fatalf("set with no tenant = %v, want ErrTenantSpendPolicyInvalid", err)
	}
}

// TestTenantSpendPolicyReportsAMissingStore asserts a deployment with no store
// fails the read rather than answering "no limits".
func TestTenantSpendPolicyReportsAMissingStore(t *testing.T) {
	_, err := GetTenantSpendPolicy(adminCallerContext(), routedSpendSource{}, "t1")
	if !errors.Is(err, orchestration.ErrTenantSpendPolicyStore) {
		t.Fatalf("err = %v, want ErrTenantSpendPolicyStore", err)
	}
	_, err = SetTenantSpendPolicy(adminCallerContext(), routedSpendSource{}, "t1", orchestration.SpendLimits{})
	if !errors.Is(err, orchestration.ErrTenantSpendPolicyStore) {
		t.Fatalf("set err = %v, want ErrTenantSpendPolicyStore", err)
	}
}

// TestTenantSpendPolicyRefusesAnotherTenantsCaller is the go-quicbaas#1058
// review P1: a caller of tenant A holding spend_policy:write submitted tenant
// B and replaced or cleared B's limits, because the context was switched to B
// before anything compared B with the caller. Both verbs are refused, and B's
// saved limits survive.
func TestTenantSpendPolicyRefusesAnotherTenantsCaller(t *testing.T) {
	store := orchestration.NewInMemoryTenantSpendPolicyStore()
	source := routedSpendSource{store: store}
	limits := orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
		{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 1_000_000},
	}}
	if _, err := SetTenantSpendPolicy(callerContext("tenant-b"), source, "tenant-b", limits); err != nil {
		t.Fatalf("own-tenant set: %v", err)
	}

	var denied *TenantTargetDeniedError
	if _, err := SetTenantSpendPolicy(callerContext("tenant-a"), source, "tenant-b", orchestration.SpendLimits{}); !errors.As(err, &denied) ||
		!errors.Is(err, orchestration.ErrAccountTenantScope) {
		t.Fatalf("cross-tenant set = %v, want a TenantTargetDeniedError", err)
	}
	if _, err := GetTenantSpendPolicy(callerContext("tenant-a"), source, "tenant-b"); !errors.As(err, &denied) {
		t.Fatalf("cross-tenant get = %v, want a TenantTargetDeniedError", err)
	}
	saved, err := GetTenantSpendPolicy(callerContext("tenant-b"), source, "tenant-b")
	if err != nil || len(saved.Limits.Windows) != 1 {
		t.Fatalf("tenant-b limits = %+v, %v; the refused clear must not land", saved, err)
	}
}
