//go:build mcp

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
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestGetSpendLimits(t *testing.T) {
	args := map[string]interface{}{"spend": map[string]interface{}{"windows": []interface{}{
		map[string]interface{}{"name": "five-hour", "size": "5h", "max_tokens": float64(2_000_000)},
		map[string]interface{}{"name": "monthly", "size": float64(720 * time.Hour), "max_equivalent_micros": float64(500_000_000)},
	}}}
	limits, present, err := getSpendLimits(args, "spend")
	if err != nil || !present {
		t.Fatalf("getSpendLimits = %v present=%t", err, present)
	}
	if len(limits.Windows) != 2 || limits.Windows[0].Size != 5*time.Hour || limits.Windows[1].MaxEquivalentMicros != 500_000_000 {
		t.Fatalf("limits = %+v", limits)
	}

	if _, present, _ := getSpendLimits(map[string]interface{}{}, "spend"); present {
		t.Fatal("an absent argument must read as absent")
	}
	if got, err := optionalSpendLimits(map[string]interface{}{}, "spend"); got != nil || err != nil {
		t.Fatalf("absent update = %+v, %v; want nil (unchanged)", got, err)
	}
	cleared, err := optionalSpendLimits(map[string]interface{}{"spend": map[string]interface{}{}}, "spend")
	if err != nil || cleared == nil || cleared.Metered() {
		t.Fatalf("empty update = %+v, %v; want a present clear", cleared, err)
	}
}

func TestGetSpendLimitsRefusesMalformedArguments(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
	}{
		{"not an object", "weekly"},
		{"windows that are not an array", map[string]interface{}{"windows": "daily"}},
		{"a window that is not an object", map[string]interface{}{"windows": []interface{}{"five-hour"}}},
		{"a size that is not a duration", map[string]interface{}{"windows": []interface{}{
			map[string]interface{}{"name": "w", "size": "forever", "max_tokens": float64(1)},
		}}},
		{"a size of the wrong type", map[string]interface{}{"windows": []interface{}{
			map[string]interface{}{"name": "w", "size": true, "max_tokens": float64(1)},
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := getSpendLimits(map[string]interface{}{"spend": tc.raw}, "spend")
			var invalid *SpendLimitsArgumentError
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want a SpendLimitsArgumentError", err)
			}
		})
	}
}

// TestGetSpendLimitsRefusesAMalformedCap is the go-quicbaas#1058 review
// regression: a cap that is not a whole number read as zero, and zero means
// "no cap", so max_equivalent_micros:"bad" silently removed a dollar limit.
// Each malformed cap is refused with the field it names.
func TestGetSpendLimitsRefusesAMalformedCap(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value interface{}
	}{
		{"a dollar cap that is a string", "max_equivalent_micros", "bad"},
		{"a dollar cap that is a numeric string", "max_equivalent_micros", "500"},
		{"a dollar cap with a fraction", "max_equivalent_micros", 1.5},
		{"a dollar cap past int64", "max_equivalent_micros", 1e300},
		{"a negative dollar cap", "max_equivalent_micros", float64(-1)},
		{"a token cap that is a bool", "max_tokens", true},
		{"a token cap with a fraction", "max_tokens", 0.5},
		{"a token cap past int64", "max_tokens", 1e19},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{"windows": []interface{}{
				map[string]interface{}{"name": "w", "size": "24h", tc.field: tc.value},
			}}
			_, _, err := getSpendLimits(map[string]interface{}{"spend": raw}, "spend")
			var invalid *SpendLimitsArgumentError
			if !errors.As(err, &invalid) || invalid.Field != tc.field || invalid.Index != 0 {
				t.Fatalf("err = %v, want a SpendLimitsArgumentError naming windows[0].%s", err, tc.field)
			}
		})
	}
}

// TestSpendArgumentsRefuseAFractionalSizeAndThreshold covers the two other
// numbers the parser reads: a window size in nanoseconds and a breaker's
// failure threshold.
func TestSpendArgumentsRefuseAFractionalSizeAndThreshold(t *testing.T) {
	_, _, err := getSpendLimits(map[string]interface{}{"spend": map[string]interface{}{"windows": []interface{}{
		map[string]interface{}{"name": "w", "size": 1.5},
	}}}, "spend")
	var invalid *SpendLimitsArgumentError
	if !errors.As(err, &invalid) || invalid.Index != 0 {
		t.Fatalf("fractional size: err = %v, want a SpendLimitsArgumentError on windows[0]", err)
	}

	for _, threshold := range []interface{}{"3", 2.5, float64(-1), float64(1 << 40)} {
		_, _, err := getAccountSpendPolicy(map[string]interface{}{"spend": map[string]interface{}{
			"breaker": map[string]interface{}{"window": "1h", "cooldown": "1m", "failure_threshold": threshold},
		}}, "spend")
		if !errors.As(err, &invalid) || invalid.Field != "failure_threshold" {
			t.Fatalf("threshold %v: err = %v, want a SpendLimitsArgumentError naming failure_threshold", threshold, err)
		}
	}
}

// TestARejectedSpendUpdateLeavesTheSavedPolicyIntact proves the refusal is
// end to end: a tenant policy set with a malformed cap fails and the saved
// dollar cap survives it.
func TestARejectedSpendUpdateLeavesTheSavedPolicyIntact(t *testing.T) {
	reg := newFleetRegistry(t, &ledgerAgentService{
		mockAgentService: &mockAgentService{},
		ledger:           orchestration.NewInMemoryConsumptionLedger(),
		tenant:           orchestration.NewInMemoryTenantSpendPolicyStore(),
	})
	ctx := tenantctx.WithSystem(context.Background())
	set := func(micros interface{}) error {
		_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spend_policy_set", map[string]interface{}{
			"tenant_id": "t1",
			"limits": map[string]interface{}{"windows": []interface{}{
				map[string]interface{}{"name": "monthly", "size": "720h", "max_equivalent_micros": micros},
			}},
		})
		return err
	}
	if err := set(float64(2_000_000_000)); err != nil {
		t.Fatalf("set: %v", err)
	}
	var invalid *SpendLimitsArgumentError
	if err := set("bad"); !errors.As(err, &invalid) {
		t.Fatalf("malformed set: err = %v, want a SpendLimitsArgumentError", err)
	}

	raw, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spend_policy_get", map[string]interface{}{"tenant_id": "t1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var report orchestration.TenantSpendPolicyReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	if len(report.Limits.Windows) != 1 || report.Limits.Windows[0].MaxEquivalentMicros != 2_000_000_000 {
		t.Fatalf("saved limits = %+v, want the $2,000 monthly cap intact", report.Limits)
	}
}

// TestAMalformedAccountCapNeverReachesTheService asserts an account update
// carrying a malformed cap is refused before the service can replace the
// saved spend policy.
func TestAMalformedAccountCapNeverReachesTheService(t *testing.T) {
	called := false
	reg := newAccountRegistry(t, &mocks.MockAccountService{
		UpdateAccountFunc: func(_ context.Context, _ *service.UpdateAccountRequest) (*service.UpdateAccountResponse, error) {
			called = true
			return &service.UpdateAccountResponse{Account: mcpSampleAccount()}, nil
		},
	})
	_, err := reg.ExecuteTool(tenantctx.WithSystem(context.Background()), "quicbaas_agent_account_update", map[string]interface{}{
		"tenant_id": "t1", "account_id": "acct-1",
		"spend": map[string]interface{}{"quota": map[string]interface{}{"windows": []interface{}{
			map[string]interface{}{"name": "daily", "size": "24h", "max_equivalent_micros": "bad"},
		}}},
	})
	var invalid *SpendLimitsArgumentError
	if !errors.As(err, &invalid) || invalid.Field != "max_equivalent_micros" {
		t.Fatalf("err = %v, want a SpendLimitsArgumentError naming max_equivalent_micros", err)
	}
	if called {
		t.Fatal("the service was called with a malformed cap; the saved policy would be replaced")
	}
}

func TestGetAccountSpendPolicy(t *testing.T) {
	args := map[string]interface{}{"spend": map[string]interface{}{
		"quota":   map[string]interface{}{"windows": []interface{}{map[string]interface{}{"name": "daily", "size": "24h", "max_tokens": float64(5)}}},
		"breaker": map[string]interface{}{"window": "1h", "failure_threshold": float64(3), "cooldown": "30m"},
	}}
	policy, present, err := getAccountSpendPolicy(args, "spend")
	if err != nil || !present {
		t.Fatalf("getAccountSpendPolicy = %v present=%t", err, present)
	}
	if len(policy.Quota.Windows) != 1 || policy.Breaker == nil || policy.Breaker.Cooldown != 30*time.Minute || policy.Breaker.FailureThreshold != 3 {
		t.Fatalf("policy = %+v", policy)
	}
	if got, err := optionalAccountSpendPolicy(map[string]interface{}{}, "spend"); got != nil || err != nil {
		t.Fatalf("absent = %+v, %v; want nil", got, err)
	}
	for _, bad := range []interface{}{
		"daily",
		map[string]interface{}{"breaker": "yes"},
		map[string]interface{}{"breaker": map[string]interface{}{"window": "soon", "cooldown": "1m"}},
		map[string]interface{}{"breaker": map[string]interface{}{"window": "1h", "cooldown": false}},
		map[string]interface{}{"quota": "none"},
	} {
		_, _, err := getAccountSpendPolicy(map[string]interface{}{"spend": bad}, "spend")
		var invalid *SpendLimitsArgumentError
		if !errors.As(err, &invalid) {
			t.Fatalf("spend %+v: err = %v, want a SpendLimitsArgumentError", bad, err)
		}
	}
}

// TestTenantSpendPolicyTools is go-quicbaas#1058 over MCP: set replaces a
// tenant's limits and get reads them back with the usage against each window.
func TestTenantSpendPolicyTools(t *testing.T) {
	reg := newFleetRegistry(t, &ledgerAgentService{
		mockAgentService: &mockAgentService{},
		ledger:           orchestration.NewInMemoryConsumptionLedger(),
		tenant:           orchestration.NewInMemoryTenantSpendPolicyStore(),
	})
	ctx := tenantctx.WithSystem(context.Background())
	_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spend_policy_set", map[string]interface{}{
		"tenant_id": "t1",
		"limits": map[string]interface{}{"windows": []interface{}{
			map[string]interface{}{"name": "monthly", "size": "720h", "max_equivalent_micros": float64(2_000_000_000)},
		}},
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	raw, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spend_policy_get", map[string]interface{}{"tenant_id": "t1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var report orchestration.TenantSpendPolicyReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	if len(report.Limits.Windows) != 1 || report.Limits.Windows[0].Size != 720*time.Hour || len(report.Windows) != 1 {
		t.Fatalf("report = %+v", report)
	}

	if _, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spend_policy_set", map[string]interface{}{"tenant_id": "t1"}); err == nil {
		t.Fatal("a set with no limits must be refused")
	}
	if _, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spend_policy_get", map[string]interface{}{}); err == nil {
		t.Fatal("a get with no tenant must be refused")
	}
	bare := newFleetRegistry(t, &mockAgentService{})
	if _, err := bare.executeAgentTool(ctx, "quicbaas_orchestrator_spend_policy_get", map[string]interface{}{"tenant_id": "t1"}); err == nil {
		t.Fatal("a deployment with no spend policy store must refuse")
	}
}
