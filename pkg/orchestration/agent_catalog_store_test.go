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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// auditActions filters an auditor's trail to one action.
func auditActions(a *recordingAuditor, action string) []StoreAuditEvent {
	var out []StoreAuditEvent
	for _, event := range a.all() {
		if event.Action == action {
			out = append(out, event)
		}
	}
	return out
}

func seedCatalogAgent(t *testing.T, store AgentCatalogStore, tenantID string, agent Agent) {
	t.Helper()
	if err := store.CreateAgent(context.Background(), tenantID, &agent); err != nil {
		t.Fatalf("CreateAgent(%s): %v", agent.ID, err)
	}
}

func TestAgentCatalogStore_AgentLifecycle(t *testing.T) {
	ctx := context.Background()
	auditor := &recordingAuditor{}
	store := NewAgentCatalogStore(kvstore.NewMemory(), WithStoreAuditor(auditor))

	agent := testAgent("claude-coder", "claude", "claude-opus-5-5")
	if err := store.CreateAgent(ctx, "acme", &agent); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if agent.CreatedAt.IsZero() || !agent.CreatedAt.Equal(agent.UpdatedAt) {
		t.Fatal("create stamps both timestamps")
	}
	created := agent.CreatedAt

	got, err := store.GetAgent(ctx, "acme", "claude-coder")
	if err != nil || got.Model != "claude-opus-5-5" {
		t.Fatalf("GetAgent = %+v, %v", got, err)
	}

	agent.Model = "claude-fable-5"
	if err := store.UpdateAgent(ctx, "acme", &agent); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if !agent.CreatedAt.Equal(created) {
		t.Fatal("an update keeps the original creation time")
	}

	list, err := store.ListAgents(ctx, "acme")
	if err != nil || len(list) != 1 || list[0].Model != "claude-fable-5" {
		t.Fatalf("ListAgents = %v, %v", list, err)
	}
	if err := store.DeleteAgent(ctx, "acme", "claude-coder"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if _, err := store.GetAgent(ctx, "acme", "claude-coder"); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("a deleted agent is unknown, got %v", err)
	}
	if len(auditActions(auditor, AuditActionCreate)) != 1 || len(auditActions(auditor, AuditActionDelete)) != 1 {
		t.Fatalf("every mutation is audited: %+v", auditor.events)
	}
}

func TestAgentCatalogStore_RefusesBadAgentWrites(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAgentCatalogStore()
	seedCatalogAgent(t, store, "acme", testAgent("a", "claude", "m"))

	dup := testAgent("a", "claude", "m")
	if err := store.CreateAgent(ctx, "acme", &dup); !errors.Is(err, ErrAgentExists) {
		t.Fatalf("a duplicate create: %v", err)
	}
	ghost := testAgent("ghost", "claude", "m")
	if err := store.UpdateAgent(ctx, "acme", &ghost); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("an update of nothing: %v", err)
	}
	if err := store.CreateAgent(ctx, "acme", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil agent: %v", err)
	}
	noModel := testAgent("b", "claude", "")
	if err := store.CreateAgent(ctx, "acme", &noModel); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an agent with no model: %v", err)
	}
	if err := store.DeleteAgent(ctx, "acme", "ghost"); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("a delete of nothing: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ListAgents(cancelled, "acme"); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled read: %v", err)
	}
	if err := NewAgentCatalogStore(nil).CreateAgent(ctx, "acme", &dup); !errors.Is(err, ErrAgentCatalogStore) {
		t.Fatalf("a store with no kv: %v", err)
	}
}

func TestAgentCatalogStore_TenantsAreIsolated(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAgentCatalogStore()
	seedCatalogAgent(t, store, "acme", testAgent("a", "claude", "m"))
	if _, err := store.GetAgent(ctx, "globex", "a"); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("another tenant's agent is invisible, got %v", err)
	}
	scoped := ContextWithTenant(ctx, "globex")
	if _, err := store.ListAgents(scoped, "acme"); !errors.Is(err, ErrAccountTenantScope) {
		t.Fatalf("a caller of one tenant cannot read another's catalog, got %v", err)
	}
}

func TestAgentCatalogStore_PoolLifecycleAndMembership(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryAgentCatalogStore()
	seedCatalogAgent(t, store, "acme", testAgent("a", "claude", "m"))
	seedCatalogAgent(t, store, "acme", testAgent("b", "codex", "gpt-5.5"))

	pool := AgentPool{ID: "coders", Name: "Coders", Agents: []string{"a", "b"}}
	if err := store.CreatePool(ctx, "acme", &pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	dup := pool
	if err := store.CreatePool(ctx, "acme", &dup); !errors.Is(err, ErrAgentExists) {
		t.Fatalf("a duplicate pool: %v", err)
	}
	dangling := AgentPool{ID: "bad", Name: "Bad", Agents: []string{"a", "ghost"}}
	if err := store.CreatePool(ctx, "acme", &dangling); !errors.Is(err, ErrAgentUnknown) {
		t.Fatalf("a pool naming nothing: %v", err)
	}

	// An agent a pool lists cannot be deleted, and the refusal names the pool.
	err := store.DeleteAgent(ctx, "acme", "b")
	var inUse *AgentInUseError
	if !errors.As(err, &inUse) || len(inUse.UsedBy) != 1 || inUse.UsedBy[0].ID != "coders" {
		t.Fatalf("DeleteAgent of a pool member = %v", err)
	}

	pool.Agents = []string{"a"}
	if err := store.UpdatePool(ctx, "acme", &pool); err != nil {
		t.Fatalf("UpdatePool: %v", err)
	}
	if err := store.DeleteAgent(ctx, "acme", "b"); err != nil {
		t.Fatalf("an agent no pool lists deletes: %v", err)
	}
	pools, err := store.ListPools(ctx, "acme")
	if err != nil || len(pools) != 1 {
		t.Fatalf("ListPools = %v, %v", pools, err)
	}
	catalogue, err := store.Catalogue(ctx, "acme")
	if err != nil || len(catalogue.Agents) != 1 || len(catalogue.Pools) != 1 {
		t.Fatalf("Catalogue = %+v, %v", catalogue, err)
	}
	if err := store.DeletePool(ctx, "acme", "coders"); err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
	if _, err := store.GetPool(ctx, "acme", "coders"); !errors.Is(err, ErrAgentPoolUnknown) {
		t.Fatalf("a deleted pool is unknown: %v", err)
	}
	if err := store.DeletePool(ctx, "acme", "coders"); !errors.Is(err, ErrAgentPoolUnknown) {
		t.Fatalf("a delete of nothing: %v", err)
	}
	ghost := AgentPool{ID: "ghost", Name: "G", Agents: []string{"a"}}
	if err := store.UpdatePool(ctx, "acme", &ghost); !errors.Is(err, ErrAgentPoolUnknown) {
		t.Fatalf("an update of nothing: %v", err)
	}
	if err := store.CreatePool(ctx, "acme", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil pool: %v", err)
	}
}

func TestAgentCatalogStore_CorruptRecordIsAStoreError(t *testing.T) {
	ctx := context.Background()
	kv := kvstore.NewMemory()
	store := NewAgentCatalogStore(kv)
	if err := kv.Put(ctx, agentDefinitionKeyspace+"acme/broken", []byte("{not json")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := store.ListAgents(ctx, "acme"); !errors.Is(err, ErrAgentCatalogStore) {
		t.Fatalf("a corrupt record: %v", err)
	}
	if _, err := store.GetAgent(ctx, "acme", "broken"); !errors.Is(err, ErrAgentCatalogStore) {
		t.Fatalf("a corrupt record: %v", err)
	}
	if _, err := store.GetAgent(ctx, "acme", "a/b"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a malformed id: %v", err)
	}
}
