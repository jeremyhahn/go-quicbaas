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
	"errors"
	"strings"
	"testing"
	"time"
)

// limitFirst gives the fixture's "first" Agent a one-hour window of maxTokens
// and, when spent > 0, records that much consumption against it.
func limitFirst(t *testing.T, f *spawnFixture, maxTokens, spent int64, withLedger bool) {
	t.Helper()
	ctx := context.Background()
	dir := f.svc.AgentDirectory()
	first, err := dir.GetAgent(ctx, SystemTenantID, "first")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	first.Spend = SpendLimits{Windows: []QuotaWindow{{Name: "hourly", Size: time.Hour, MaxTokens: maxTokens}}}
	if _, err := dir.UpdateAgent(ctx, SystemTenantID, first); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if !withLedger {
		return
	}
	ledger := NewInMemoryConsumptionLedger()
	f.svc.SetConsumptionLedger(ledger)
	if spent == 0 {
		return
	}
	if err := ledger.Record(ctx, ConsumptionEntry{
		EntryID: "spent", TenantID: SystemTenantID, SessionID: "s0", ResolvedAgentID: "first",
		Engine: "primary", Model: "model-a", Outcome: ConsumptionOutcomeSuccess,
		InputTokens: spent, OccurredAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestSpawnSession_APoolPassesAnOverBudgetAgentAndAuditsIt(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	limitFirst(t, f, 100, 150, true)
	resp, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{PoolID: "chain"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if resp.ResolvedAgentID != "second" || len(resp.Failovers) != 1 || resp.Failovers[0].Reason != AgentFailoverOverBudget {
		t.Fatalf("response = %+v, want second after first was passed over as over_budget", resp)
	}
	if len(f.primary.requests()) != 0 {
		t.Fatal("an Agent over its budget must not be spawned at all")
	}
	events := auditActions(f.auditor, AuditActionAgentFailover)
	if len(events) != 1 || !strings.Contains(events[0].ResourceID, "first -> second (over_budget)") {
		t.Fatalf("the switch must be audited: %+v", events)
	}
}

func TestSpawnSession_AnOverBudgetAgentAloneIsRefusedWithTheSpendError(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	limitFirst(t, f, 100, 150, true)
	_, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{AgentID: "first"})
	var spent *AgentSpendExhaustedError
	if !errors.As(err, &spent) || errors.Is(err, ErrAgentPoolExhausted) {
		t.Fatalf("SpawnSession = %v, want AgentSpendExhaustedError itself", err)
	}
	if spent.AgentID != "first" || spent.Window != "hourly" || spent.Used != 150 || spent.Limit != 100 || spent.Axis != QuotaAxisTokens {
		t.Fatalf("refusal = %+v", spent)
	}
	if spent.RetryAt.IsZero() || !strings.Contains(err.Error(), "frees up at") {
		t.Fatalf("the refusal must say when the window frees up: %v", err)
	}
}

func TestSpawnSession_AnAgentWithinItsBudgetRuns(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	limitFirst(t, f, 1000, 150, true)
	resp, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{PoolID: "chain"})
	if err != nil || resp.ResolvedAgentID != "first" || len(resp.Failovers) != 0 {
		t.Fatalf("SpawnSession = %+v, %v; want first, within its budget", resp, err)
	}
}

func TestSpawnSession_LimitsThatCannotBeCheckedFailClosedWithoutFailover(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	limitFirst(t, f, 100, 0, false)
	_, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{PoolID: "chain"})
	var undecidable *AgentSpendUndecidableError
	if !errors.As(err, &undecidable) || undecidable.AgentID != "first" {
		t.Fatalf("SpawnSession = %v, want AgentSpendUndecidableError", err)
	}
	if len(f.primary.requests())+len(f.secondary.requests()) != 0 {
		t.Fatal("nothing may be spawned when a limit cannot be checked")
	}
	if fault, ok := ClassifyAgentCatalogError(err); !ok || fault != AgentCatalogFaultUnavailable {
		t.Fatalf("an unchecked limit must read as unavailable on every transport: %v, %v", fault, ok)
	}
}

func TestAgentSpend_AnAgentWithoutLimitsNeedsNoLedger(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	if err := f.svc.checkAgentSpend(context.Background(), SystemTenantID, testAgent("x", "primary", "m")); err != nil {
		t.Fatalf("checkAgentSpend = %v, want nothing to check", err)
	}
}

func TestAdmitScope_RefusesAProjectAndAnAgentTogether(t *testing.T) {
	_, err := NewQuotaAuthority(NewInMemoryConsumptionLedger()).AdmitScope(context.Background(),
		ScopeAdmitRequest{TenantID: "t", ProjectID: "p", AgentID: "a"}, time.Now())
	var invalid *QuotaInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "agent_id" {
		t.Fatalf("AdmitScope = %v, want a QuotaInvalidError on agent_id", err)
	}
}

func TestClassifyEnvironmentFault_AnOverBudgetAgentDefersUncharged(t *testing.T) {
	retryAt := time.Now().Add(time.Hour).UTC()
	err := &AgentSpendExhaustedError{AgentID: "claude-coder", Window: "daily", Axis: QuotaAxisTokens, Used: 12, Limit: 10, RetryAt: retryAt}
	fault, ok := classifyEnvironmentFault(err)
	if !ok || fault.Class != FaultAgentOverBudget || !fault.RetryNotBefore.Equal(retryAt) {
		t.Fatalf("classifyEnvironmentFault = (%+v, %v)", fault, ok)
	}
	if !strings.Contains(fault.Reason, "claude-coder") || !strings.Contains(fault.Reason, "not charged") {
		t.Fatalf("reason = %q", fault.Reason)
	}
	outcome := faultOutcomeFor(&Task{ConsecutiveEnvironmentFaults: MaxConsecutiveEnvironmentFaults}, fault)
	if !outcome.refund || !outcome.requeue || !outcome.notBefore.Equal(retryAt) || outcome.bounded {
		t.Fatalf("outcome = %+v, want a refunded deferral however often it repeats", outcome)
	}
	if meaning, known := FaultClassMeaning(FaultAgentOverBudget); !known || meaning == "" {
		t.Fatal("agent_over_budget has no meaning for the forge comment")
	}
	// Reached through a pool every member of which was over budget.
	pooled := &AgentPoolExhaustedError{Ref: AgentRef{PoolID: "chain"}, Last: err}
	if fault, ok := classifyEnvironmentFault(pooled); !ok || fault.Class != FaultAgentOverBudget {
		t.Fatalf("a pool exhausted by budgets = (%+v, %v)", fault, ok)
	}
	if reason, failover := AgentFailoverReason(err); !failover || reason != AgentFailoverOverBudget {
		t.Fatalf("AgentFailoverReason = %q, %v", reason, failover)
	}
}
