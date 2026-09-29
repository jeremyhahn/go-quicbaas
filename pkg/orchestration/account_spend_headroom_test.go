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

package orchestration

import (
	"context"
	stderrors "errors"
	"testing"
	"time"
)

// TestSpendHeadroomRoundTripsEveryRefusal asserts a verdict projected onto the
// wire rebuilds the same typed refusal on the other side, with its evidence.
func TestSpendHeadroomRoundTripsEveryRefusal(t *testing.T) {
	account := &Account{ID: "a1", Provider: AccountProviderClaude}
	freesAt := time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		err   error
		check func(t *testing.T, rebuilt error)
	}{
		{"admits", nil, func(t *testing.T, rebuilt error) {
			if rebuilt != nil {
				t.Fatalf("rebuilt = %v, want nil", rebuilt)
			}
		}},
		{"spent window", &AccountQuotaExhaustedError{AccountID: "a1", Window: "weekly", Axis: QuotaAxisTokens, Used: 12, Limit: 10, RetryAt: freesAt},
			func(t *testing.T, rebuilt error) {
				var exhausted *AccountQuotaExhaustedError
				if !stderrors.As(rebuilt, &exhausted) || exhausted.Window != "weekly" || exhausted.Used != 12 ||
					exhausted.Limit != 10 || !exhausted.RetryAt.Equal(freesAt) {
					t.Fatalf("rebuilt = %#v", rebuilt)
				}
			}},
		{"open breaker", &AccountBreakerOpenError{AccountID: "a1", Failures: 3, OpenUntil: freesAt},
			func(t *testing.T, rebuilt error) {
				var open *AccountBreakerOpenError
				if !stderrors.As(rebuilt, &open) || open.Failures != 3 || !open.OpenUntil.Equal(freesAt) {
					t.Fatalf("rebuilt = %#v", rebuilt)
				}
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			headroom, err := SpendHeadroomOf(tc.err)
			if err != nil {
				t.Fatalf("SpendHeadroomOf: %v", err)
			}
			if headroom.Admits != (tc.err == nil) {
				t.Fatalf("Admits = %v", headroom.Admits)
			}
			tc.check(t, headroom.Err(account))
		})
	}
}

// TestSpendHeadroomRefusesWhatItCannotName asserts a failure that is not a
// refusal is returned as itself, and a refusal this build does not know is
// refused rather than admitted.
func TestSpendHeadroomRefusesWhatItCannotName(t *testing.T) {
	boom := stderrors.New("ledger unreadable")
	if _, err := SpendHeadroomOf(boom); !stderrors.Is(err, boom) {
		t.Fatalf("SpendHeadroomOf(read failure) = %v, want the failure", err)
	}
	unknown := SpendHeadroom{Refusal: "from_a_newer_build"}
	if err := unknown.Err(&Account{ID: "a1"}); !stderrors.Is(err, ErrAccountSpendUnknownVerdict) {
		t.Fatalf("Err(unknown refusal) = %v, want an unknown-verdict refusal", err)
	}
}

// TestReadSpendReportCarriesTheScopesOwnLimits asserts a project report
// carries the project's windows and a tenant report the tenant's, each with
// its usage, and that the deeper levels carry none.
func TestReadSpendReportCarriesTheScopesOwnLimits(t *testing.T) {
	ctx := context.Background()
	ledger := NewInMemoryConsumptionLedger()
	now := time.Now().UTC()
	if err := ledger.Record(ctx, spendEntry("e1", "p1", "task-a", "", "", 0, "", 600, ConsumptionOutcomeSuccess, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Record: %v", err)
	}
	projects := NewInMemoryProjectStore()
	if err := projects.Create(ctx, &Project{ID: "p1", TenantID: "t1", Name: "p1", Spend: dailyTokens(1000)}); err != nil {
		t.Fatalf("Create project: %v", err)
	}
	tenants := NewInMemoryTenantSpendPolicyStore()
	sources := SpendSources{Ledger: ledger, Projects: projects, TenantPolicies: tenants}

	project, err := ReadSpendReport(ctx, sources, &SpendReportRequest{TenantID: "t1", ProjectID: "p1", Now: now})
	if err != nil {
		t.Fatalf("project report: %v", err)
	}
	if project.ScopePolicyStatus != SpendPolicyMetered || len(project.ScopeWindows) != 1 ||
		project.ScopeWindows[0].UsedTokens != 1200 || !project.ScopeWindows[0].Exhausted {
		t.Fatalf("project limits = %s %+v, want the spent daily window", project.ScopePolicyStatus, project.ScopeWindows)
	}

	tenant, err := ReadSpendReport(ctx, sources, &SpendReportRequest{TenantID: "t1", Now: now})
	if err != nil {
		t.Fatalf("tenant report: %v", err)
	}
	if tenant.ScopePolicyStatus != SpendPolicyNone || len(tenant.ScopeWindows) != 0 {
		t.Fatalf("tenant limits = %s %+v, want none: the tenant set no limits", tenant.ScopePolicyStatus, tenant.ScopeWindows)
	}

	task, err := ReadSpendReport(ctx, sources, &SpendReportRequest{TenantID: "t1", ProjectID: "p1", TaskID: "task-a", Now: now})
	if err != nil {
		t.Fatalf("task report: %v", err)
	}
	if task.ScopePolicyStatus != "" || task.ScopeWindows != nil {
		t.Fatalf("a task carries no limits of its own, got %s %+v", task.ScopePolicyStatus, task.ScopeWindows)
	}

	unwired, err := ReadSpendReport(ctx, SpendSources{Ledger: ledger}, &SpendReportRequest{TenantID: "t1", Now: now})
	if err != nil {
		t.Fatalf("unwired report: %v", err)
	}
	if unwired.ScopePolicyStatus != SpendPolicyUnknown {
		t.Fatalf("a deployment with no policy store reported %s, want unknown", unwired.ScopePolicyStatus)
	}
}

// TestReadSpendReportFailsWhenTheScopeLimitsCannotBeRead asserts a policy
// store that fails fails the report rather than reporting no limits.
func TestReadSpendReportFailsWhenTheScopeLimitsCannotBeRead(t *testing.T) {
	boom := stderrors.New("store down")
	report, err := ReadSpendReport(context.Background(),
		SpendSources{Ledger: NewInMemoryConsumptionLedger(), TenantPolicies: &failingTenantPolicies{err: boom}},
		&SpendReportRequest{TenantID: "t1"})
	if report != nil || !stderrors.Is(err, boom) {
		t.Fatalf("report=%v err=%v, want no report and the store's failure", report, err)
	}
}
