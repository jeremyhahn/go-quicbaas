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
	"strings"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// =============================================================================
// The spend axis: a project's and its tenant's own limits (ruling F6)
// =============================================================================

// spendAxisRig is a real scope-spend reader over an in-memory ledger and
// tenant policy store, reporting to a recording auditor.
type spendAxisRig struct {
	ledger   ConsumptionLedger
	tenants  TenantSpendPolicyStore
	auditor  *recordingAuditor
	reader   ScopeSpendReader
	quotaNow time.Time
}

func newSpendAxisRig(t *testing.T) *spendAxisRig {
	t.Helper()
	rig := &spendAxisRig{
		ledger:   NewInMemoryConsumptionLedger(),
		tenants:  NewInMemoryTenantSpendPolicyStore(),
		auditor:  &recordingAuditor{},
		quotaNow: admissionClock,
	}
	reader, err := NewScopeSpendAdmission(NewQuotaAuthority(rig.ledger), rig.tenants,
		WithScopeSpendReporting(rig.auditor, nil))
	if err != nil {
		t.Fatalf("NewScopeSpendAdmission: %v", err)
	}
	rig.reader = reader
	return rig
}

// charge records an unpooled local turn against a project, which the project
// and tenant planes count although no account paid for it.
func (r *spendAxisRig) charge(t *testing.T, projectID, entryID string, tokens int64) {
	t.Helper()
	if err := r.ledger.Record(context.Background(), ConsumptionEntry{
		EntryID: entryID, TenantID: admissionTenant, ProjectID: projectID,
		Engine: "openvino", Model: "qwen3-8b", Outcome: ConsumptionOutcomeSuccess,
		InputTokens: tokens, OccurredAt: r.quotaNow.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("record %s: %v", entryID, err)
	}
}

func dailyTokens(max int64) SpendLimits {
	return SpendLimits{Windows: []QuotaWindow{{Name: "daily", Size: 24 * time.Hour, MaxTokens: max}}}
}

// TestPlan_DefersAProjectOverItsOwnSpendLimit is the issue's verification: a
// spent project defers with its typed reason, and it reaches the audit trail.
func TestPlan_DefersAProjectOverItsOwnSpendLimit(t *testing.T) {
	spendRig := newSpendAxisRig(t)
	spendRig.charge(t, "spent", "e1", 1500)
	rig := newAdmissionRig(t)
	rig.spend = spendRig.reader
	rig.projectWith(t, &Project{ID: "spent", TenantID: admissionTenant, Name: "spent", Autonomous: true,
		Quotas: ProjectQuotas{MaxConcurrency: 5}, Spend: dailyTokens(1000)})
	rig.project(t, admissionTenant, "funded", 5)
	rig.queued(admissionTenant, "spent", 5)
	rig.queued(admissionTenant, "funded", 5)
	deferrals := func() float64 {
		return promtestutil.ToFloat64(metrics.GetMetrics().FactorySpendDeferrals.WithLabelValues(
			admissionTenant, "spent", string(AdmissionAxisProjectSpend)))
	}
	before := deferrals()

	plan, err := rig.controller(t, 8).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	spent := plan.For(admissionTenant, "spent")
	if !spent.Deferred || spent.DispatchSlots() != 0 || spent.Axis != AdmissionAxisProjectSpend {
		t.Fatalf("spent = %+v, want a deferral on %s with no slots", spent, AdmissionAxisProjectSpend)
	}
	if !spent.RetryAt.Equal(spendRig.quotaNow.Add(23 * time.Hour)) {
		t.Fatalf("RetryAt = %v, want the day's oldest turn ageing out", spent.RetryAt)
	}
	if !strings.Contains(spent.Reason, `"daily"`) || !strings.Contains(spent.Reason, "1500 of 1000 tokens") {
		t.Fatalf("Reason = %q, want the window and its usage named", spent.Reason)
	}
	if funded := plan.For(admissionTenant, "funded"); funded.Deferred || funded.DispatchSlots() != 5 {
		t.Fatalf("a project within its limits is unaffected: %+v", funded)
	}

	events := spendRig.auditor.all()
	if len(events) != 1 || events[0].Action != AuditActionSpendDeferred ||
		events[0].ResourceID != "spent/project_spend/token_limit/daily" {
		t.Fatalf("audit = %+v, want one spend_deferred event naming the project, axis, refusal and window", events)
	}

	// A second sweep over the same state is not a second audit event.
	if _, err := rig.controller(t, 8).Plan(context.Background()); err != nil {
		t.Fatalf("Plan (again): %v", err)
	}
	if got := len(spendRig.auditor.all()); got != 1 {
		t.Fatalf("a deferral that did not change was audited %d times, want once", got)
	}
	if got := deferrals() - before; got != 1 {
		t.Fatalf("the deferral was counted %v times on %s, want once", got, "quicbaas_factory_spend_deferrals_total")
	}
}

// TestPlan_ASpentTenantDefersEveryProject asserts the tenant's own limits,
// measured over every turn it was charged, stop all of its projects.
func TestPlan_ASpentTenantDefersEveryProject(t *testing.T) {
	spendRig := newSpendAxisRig(t)
	spendRig.charge(t, "a", "e1", 600)
	spendRig.charge(t, "b", "e2", 600)
	if err := spendRig.tenants.Put(context.Background(), &TenantSpendPolicy{
		TenantID: admissionTenant,
		Limits:   SpendLimits{Windows: []QuotaWindow{{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 1, MaxTokens: 1000}}},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rig := newAdmissionRig(t)
	rig.spend = spendRig.reader
	rig.project(t, admissionTenant, "a", 5)
	rig.project(t, admissionTenant, "b", 5)

	plan, err := rig.controller(t, 8).Plan(context.Background())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		decision := plan.For(admissionTenant, id)
		if !decision.Deferred || decision.Axis != AdmissionAxisTenantSpend {
			t.Fatalf("project %s = %+v, want deferred by the tenant's limit", id, decision)
		}
	}
}

// TestPlan_SpendAxisAloneStillPlans asserts a deployment that declared no
// fleet capacity and pools no accounts still enforces spend limits.
func TestPlan_SpendAxisAloneStillPlans(t *testing.T) {
	spendRig := newSpendAxisRig(t)
	rig := newAdmissionRig(t)
	controller, err := NewAdmissionController(AdmissionControllerConfig{
		Tenants: rig.tenants, Projects: rig.projects, Queue: rig.queue, Leases: rig.leases,
		LeaseTTL: testPollerLeaseTTL, Spend: spendRig.reader,
		Now: func() time.Time { return admissionClock },
	})
	if err != nil {
		t.Fatalf("NewAdmissionController: %v", err)
	}
	plan, err := controller.Plan(context.Background())
	if err != nil || plan == nil {
		t.Fatalf("Plan = (%v, %v), want a plan: the spend axis alone is something to defer on", plan, err)
	}
}

// failingTenantPolicies fails every read.
type failingTenantPolicies struct {
	TenantSpendPolicyStore
	err error
}

func (f *failingTenantPolicies) Get(context.Context, string) (*TenantSpendPolicy, error) {
	return nil, f.err
}

// TestScopeSpendFailsClosed asserts a policy or window that cannot be read is
// an undecidable admission, never an admitted one.
func TestScopeSpendFailsClosed(t *testing.T) {
	boom := stderrors.New("store down")
	project := &Project{ID: "p", TenantID: admissionTenant, Spend: dailyTokens(10)}

	reader, err := NewScopeSpendAdmission(NewQuotaAuthority(NewInMemoryConsumptionLedger()), &failingTenantPolicies{err: boom})
	if err != nil {
		t.Fatalf("NewScopeSpendAdmission: %v", err)
	}
	if _, err := reader.ScopeSpend(context.Background(), project, admissionClock); !stderrors.Is(err, ErrAdmissionUndecidable) {
		t.Fatalf("unreadable tenant policy = %v, want undecidable", err)
	}

	reader, err = NewScopeSpendAdmission(NewQuotaAuthority(NewConsumptionLedger(&failingScanKV{err: boom})),
		NewInMemoryTenantSpendPolicyStore())
	if err != nil {
		t.Fatalf("NewScopeSpendAdmission: %v", err)
	}
	if _, err := reader.ScopeSpend(context.Background(), project, admissionClock); !stderrors.Is(err, ErrAdmissionUndecidable) {
		t.Fatalf("unreadable project window = %v, want undecidable", err)
	}
}

// TestScopeSpendAdmitsWithinLimitsAndUnmetered asserts a project under its
// limits, and one with none, are admitted without a deferral.
func TestScopeSpendAdmitsWithinLimitsAndUnmetered(t *testing.T) {
	spendRig := newSpendAxisRig(t)
	spendRig.charge(t, "p", "e1", 10)
	for _, limits := range []SpendLimits{{}, dailyTokens(1000)} {
		verdict, err := spendRig.reader.ScopeSpend(context.Background(),
			&Project{ID: "p", TenantID: admissionTenant, Spend: limits}, admissionClock)
		if err != nil || verdict.Deferred {
			t.Fatalf("limits %+v: verdict=%+v err=%v, want an admission", limits, verdict, err)
		}
	}
}

// TestNewScopeSpendAdmissionRefusesMissingInputs asserts the constructor and
// the question refuse what they cannot use.
func TestNewScopeSpendAdmissionRefusesMissingInputs(t *testing.T) {
	var config *AdmissionConfigError
	if _, err := NewScopeSpendAdmission(nil, NewInMemoryTenantSpendPolicyStore()); !stderrors.As(err, &config) {
		t.Fatalf("no quota authority = %v, want a config error", err)
	}
	if _, err := NewScopeSpendAdmission(NewQuotaAuthority(NewInMemoryConsumptionLedger()), nil); !stderrors.As(err, &config) {
		t.Fatalf("no tenant policies = %v, want a config error", err)
	}
	spendRig := newSpendAxisRig(t)
	if _, err := spendRig.reader.ScopeSpend(context.Background(), nil, admissionClock); !stderrors.As(err, &config) {
		t.Fatalf("no project = %v, want a config error", err)
	}
}

// TestSpendDeferralReasonNamesDollars asserts a dollar window reads in dollars.
func TestSpendDeferralReasonNamesDollars(t *testing.T) {
	reason := spendDeferralReason("project p", &QuotaDecision{
		Window: "weekly", Axis: QuotaAxisEquivalentMicros, Used: 12_500_000, Limit: 10_000_000,
	})
	if !strings.Contains(reason, "$12.50 of $10.00 at list price") || strings.Contains(reason, "frees up") {
		t.Fatalf("reason = %q", reason)
	}
}
