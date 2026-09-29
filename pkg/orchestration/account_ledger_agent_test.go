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

// agentEntry is validEntry run by the tenant Agent agentID, at offset from
// ledgerBase.
func agentEntry(id, agentID string, offset time.Duration, tokens int64) ConsumptionEntry {
	entry := validEntry()
	entry.EntryID = id
	entry.ResolvedAgentID = agentID
	entry.OccurredAt = ledgerBase.Add(offset)
	entry.InputTokens, entry.OutputTokens = tokens, 0
	return entry
}

func TestConsumptionLedgerRecordWritesTheAgentPlane(t *testing.T) {
	ledger, kv := newLedgerFixture()
	ctx := context.Background()
	if err := ledger.Record(ctx, agentEntry("e1", "claude-coder", 0, 10)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	key := "orch/costagent/t1/claude-coder/" + nanosSegment(ledgerBase.Truncate(time.Hour)) + "/" + nanosSegment(ledgerBase) + "/e1"
	if _, err := kv.Get(ctx, key); err != nil {
		t.Fatalf("record missing at %s: %v", key, err)
	}
	// A raw-backend session's turn belongs to no Agent and has no plane.
	if err := ledger.Record(ctx, agentEntry("e2", "", time.Minute, 10)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var agentKeys int
	if err := kv.Scan(ctx, "orch/costagent/", func(string, []byte) error { agentKeys++; return nil }); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if agentKeys != 1 {
		t.Fatalf("agent plane holds %d entries, want only the Agent's", agentKeys)
	}
}

func TestConsumptionLedgerAgentWindowUsageIsTheAgentsOwn(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()
	for _, entry := range []ConsumptionEntry{
		agentEntry("e1", "claude-coder", 0, 100),
		agentEntry("e2", "claude-coder", 10*time.Minute, 50),
		agentEntry("e3", "codex-reviewer", 5*time.Minute, 7),
		agentEntry("e4", "", 5*time.Minute, 1000),
		agentEntry("e5", "claude-coder", -3*time.Hour, 9999),
	} {
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("Record(%s): %v", entry.EntryID, err)
		}
	}
	usage, err := ledger.AgentWindowUsage(ctx, "t1", "claude-coder", ledgerBase.Add(-time.Hour), ledgerBase.Add(time.Hour))
	if err != nil {
		t.Fatalf("AgentWindowUsage: %v", err)
	}
	if usage.TotalTokens != 150 || usage.Entries != 2 {
		t.Fatalf("claude-coder window = %d tokens over %d entries, want 150 over 2", usage.TotalTokens, usage.Entries)
	}
}

func TestConsumptionLedgerAgentWindowUsageRefusesBadInput(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()
	from, to := ledgerBase, ledgerBase.Add(time.Hour)
	for field, read := range map[string]func() error{
		"tenant_id": func() error { _, err := ledger.AgentWindowUsage(ctx, "", "a", from, to); return err },
		"agent_id":  func() error { _, err := ledger.AgentWindowUsage(ctx, "t1", "", from, to); return err },
		"window":    func() error { _, err := ledger.AgentWindowUsage(ctx, "t1", "a", to, from); return err },
	} {
		var invalid *ConsumptionInvalidError
		if err := read(); !stderrors.As(err, &invalid) || invalid.Field != field {
			t.Fatalf("%s: err = %v, want a ConsumptionInvalidError on it", field, err)
		}
	}
}

func TestConsumptionLedgerRecordRefusesAnAgentThatSpellsTheSentinel(t *testing.T) {
	ledger, _ := newLedgerFixture()
	var invalid *ConsumptionInvalidError
	err := ledger.Record(context.Background(), agentEntry("e1", consumptionUnattributed, 0, 1))
	if !stderrors.As(err, &invalid) || invalid.Field != "resolved_agent_id" {
		t.Fatalf("err = %v, want a ConsumptionInvalidError on resolved_agent_id", err)
	}
}

func TestConsumptionLedgerRollupBreaksDownAndFiltersByAgent(t *testing.T) {
	ledger, _ := newLedgerFixture()
	ctx := context.Background()
	for _, entry := range []ConsumptionEntry{
		agentEntry("e1", "claude-coder", 0, 100),
		agentEntry("e2", "codex-reviewer", time.Minute, 7),
		agentEntry("e3", "", 2*time.Minute, 3),
	} {
		if err := ledger.Record(ctx, entry); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	all, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if all.ByAgent["claude-coder"].TotalTokens != 100 || all.ByAgent["codex-reviewer"].TotalTokens != 7 ||
		all.ByAgent[consumptionUnattributed].TotalTokens != 3 {
		t.Fatalf("by agent = %+v", all.ByAgent)
	}
	one, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1", Agent: "codex-reviewer"})
	if err != nil {
		t.Fatalf("Rollup(agent): %v", err)
	}
	if one.TotalTokens != 7 || len(one.ByAgent) != 1 {
		t.Fatalf("narrowed to codex-reviewer = %d tokens, %d agents", one.TotalTokens, len(one.ByAgent))
	}
	raw, err := ledger.Rollup(ctx, ConsumptionScope{TenantID: "t1", Agent: consumptionUnattributed})
	if err != nil {
		t.Fatalf("Rollup(no agent): %v", err)
	}
	if raw.TotalTokens != 3 {
		t.Fatalf("the raw-backend turns = %d tokens, want 3", raw.TotalTokens)
	}
}
