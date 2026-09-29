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
	"testing"
	"time"
)

// A session spawned as a tenant Agent attributes every turn to that Agent; a
// session on a raw backend attributes its turns to none.
func TestSpawnByAgent_AttributesTheSessionsTurnsToTheAgent(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	ledger := NewInMemoryConsumptionLedger()
	f.svc.SetConsumptionLedger(ledger)
	ctx := context.Background()

	resp, err := f.svc.SpawnSession(ctx, &SpawnSessionRequest{PoolID: "chain"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	mem, ok := f.svc.liveSessionRecord(resp.SessionID)
	if !ok || mem.ResolvedAgentID != "first" {
		t.Fatalf("live record = %+v, want it to run as agent first", mem)
	}
	durable, err := f.svc.store.GetSession(ctx, SystemTenantID, resp.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if durable.ResolvedAgentID != "first" {
		t.Fatalf("durable record names agent %q, want first: a restored session would stop attributing", durable.ResolvedAgentID)
	}

	turn := completedTurn{turnID: "turn-1", sessionID: resp.SessionID, engine: "primary", model: "model-a"}
	f.svc.recordConsumption(ctx, turn, TurnAttribution{}, f.svc.sessionOfTurn(turn), &TokenUsage{InputTokens: 40}, ConsumptionOutcomeSuccess)
	usage, err := ledger.AgentWindowUsage(ctx, SystemTenantID, "first", time.Now().Add(-time.Hour), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("AgentWindowUsage: %v", err)
	}
	if usage.TotalTokens != 40 {
		t.Fatalf("agent first's window = %d tokens, want the turn's 40", usage.TotalTokens)
	}
}

func TestSpawnByBackend_AttributesToNoAgent(t *testing.T) {
	f := newSpawnFixture(t, nil, nil)
	resp, err := f.svc.SpawnSession(context.Background(), &SpawnSessionRequest{Backend: "primary"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	mem, ok := f.svc.liveSessionRecord(resp.SessionID)
	if !ok || mem.ResolvedAgentID != "" {
		t.Fatalf("live record = %+v, want no Agent", mem)
	}
}
