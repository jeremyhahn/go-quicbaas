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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

func testTenantAgent(principal string) *TenantAgent {
	return &TenantAgent{
		Agent:        AgentRefTo("lead-1"),
		Principal:    principal,
		SigningKeyID: "tenant-agent-signing-tenant-a",
	}
}

func TestTenantAgentStore_Lifecycle(t *testing.T) {
	t.Parallel()

	auditor := &recordingAuditor{}
	store := NewTenantAgentStore(kvstore.NewMemory(), WithStoreAuditor(auditor)).(*tenantAgentStore)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	ctx := context.Background()

	if err := store.CreateTenantAgent(ctx, "tenant-a", testTenantAgent("tenant-agent-a")); err != nil {
		t.Fatalf("CreateTenantAgent() error = %v", err)
	}
	got, err := store.GetTenantAgent(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("GetTenantAgent() error = %v", err)
	}
	if got.TenantID != "tenant-a" || got.Agent != AgentRefTo("lead-1") || got.Principal != "tenant-agent-a" ||
		got.SigningKeyID != "tenant-agent-signing-tenant-a" || !got.CreatedAt.Equal(clock) || !got.UpdatedAt.Equal(clock) {
		t.Fatalf("round trip = %+v", got)
	}

	err = store.CreateTenantAgent(ctx, "tenant-a", testTenantAgent("tenant-agent-other"))
	if !errors.Is(err, ErrTenantAgentExists) {
		t.Fatalf("second create error = %v, want ErrTenantAgentExists", err)
	}
	var exists *TenantAgentExistsError
	if !errors.As(err, &exists) || exists.Principal != "tenant-agent-a" {
		t.Fatalf("second create error = %#v, want the holder named", err)
	}
	if held, _ := store.GetTenantAgent(ctx, "tenant-a"); held.Principal != "tenant-agent-a" {
		t.Fatalf("a refused create changed the record: %+v", held)
	}

	later := clock.Add(time.Hour)
	store.now = func() time.Time { return later }
	update := testTenantAgent("tenant-agent-a")
	update.Agent = AgentRef{PoolID: "leads"}
	if err := store.UpdateTenantAgent(ctx, "tenant-a", update); err != nil {
		t.Fatalf("UpdateTenantAgent() error = %v", err)
	}
	got, err = store.GetTenantAgent(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("GetTenantAgent() after update error = %v", err)
	}
	if !got.CreatedAt.Equal(clock) || !got.UpdatedAt.Equal(later) || got.Agent.PoolID != "leads" {
		t.Fatalf("after update = %+v", got)
	}

	if err := store.DeleteTenantAgent(ctx, "tenant-a"); err != nil {
		t.Fatalf("DeleteTenantAgent() error = %v", err)
	}
	if _, err := store.GetTenantAgent(ctx, "tenant-a"); !errors.Is(err, ErrTenantAgentUnknown) {
		t.Fatalf("get after delete error = %v, want ErrTenantAgentUnknown", err)
	}
	if err := store.DeleteTenantAgent(ctx, "tenant-a"); err != nil {
		t.Fatalf("second delete error = %v, want nil", err)
	}

	creates := auditActions(auditor, AuditActionCreate)
	if len(creates) != 2 || !creates[0].Success || creates[1].Success {
		t.Fatalf("create events = %+v, want one success then one refusal", creates)
	}
	if updates := auditActions(auditor, AuditActionUpdate); len(updates) != 1 || !updates[0].Success {
		t.Fatalf("update events = %+v", updates)
	}
	if deletes := auditActions(auditor, AuditActionDelete); len(deletes) != 2 {
		t.Fatalf("delete events = %+v, want 2", deletes)
	}
	for _, event := range auditor.all() {
		if event.Resource != AuditResourceTenantAgent || event.ResourceID != TenantAgentRecordID || event.TenantID != "tenant-a" {
			t.Fatalf("event = %+v", event)
		}
	}
	if n := len(auditor.all()); n != 5 {
		t.Fatalf("recorded %d events, want 5", n)
	}
}

func TestTenantAgentStore_RefusesBadWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		agent *TenantAgent
	}{
		{name: "nil", agent: nil},
		{name: "zero agent ref", agent: &TenantAgent{Principal: "p-1", SigningKeyID: "k"}},
		{name: "agent and pool", agent: &TenantAgent{Agent: AgentRef{AgentID: "lead-1", PoolID: "leads"}, Principal: "p-1", SigningKeyID: "k"}},
		{name: "blank principal", agent: &TenantAgent{Agent: AgentRefTo("lead-1"), Principal: " ", SigningKeyID: "k"}},
		{name: "blank key", agent: &TenantAgent{Agent: AgentRefTo("lead-1"), Principal: "p-1", SigningKeyID: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := NewInMemoryTenantAgentStore()
			ctx := context.Background()
			err := store.CreateTenantAgent(ctx, "tenant-a", tt.agent)
			var invalid *InputValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v, want *InputValidationError", err)
			}
			if _, err := store.GetTenantAgent(ctx, "tenant-a"); !errors.Is(err, ErrTenantAgentUnknown) {
				t.Fatalf("a refused write stored a record: %v", err)
			}
		})
	}
}

func TestTenantAgentStore_UpdateRequiresAnAppointment(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTenantAgentStore()
	err := store.UpdateTenantAgent(context.Background(), "tenant-a", testTenantAgent("tenant-agent-a"))
	if !errors.Is(err, ErrTenantAgentUnknown) {
		t.Fatalf("error = %v, want ErrTenantAgentUnknown", err)
	}
}

func TestTenantAgentStore_TenantsAreIsolated(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTenantAgentStore()
	ctx := context.Background()

	if err := store.CreateTenantAgent(ctx, "tenant-a", testTenantAgent("tenant-agent-a")); err != nil {
		t.Fatalf("create in tenant-a: %v", err)
	}
	if _, err := store.GetTenantAgent(ctx, "tenant-b"); !errors.Is(err, ErrTenantAgentUnknown) {
		t.Fatalf("tenant-b sees an appointment: %v", err)
	}
	if err := store.CreateTenantAgent(ctx, "tenant-b", testTenantAgent("tenant-agent-b")); err != nil {
		t.Fatalf("create in tenant-b: %v", err)
	}
	if err := store.DeleteTenantAgent(ctx, "tenant-b"); err != nil {
		t.Fatalf("delete tenant-b: %v", err)
	}
	a, err := store.GetTenantAgent(ctx, "tenant-a")
	if err != nil || a.Principal != "tenant-agent-a" {
		t.Fatalf("tenant-a after tenant-b's delete = %+v, %v", a, err)
	}

	other := NewInMemoryTenantAgentStore()
	forged := testTenantAgent("tenant-agent-a")
	forged.TenantID = "tenant-b"
	if err := other.CreateTenantAgent(ctx, "tenant-a", forged); err != nil {
		t.Fatalf("create with a foreign body tenant: %v", err)
	}
	if forged.TenantID != "tenant-a" {
		t.Fatalf("stored body names tenant %q, want tenant-a", forged.TenantID)
	}
	got, err := other.GetTenantAgent(ctx, "tenant-a")
	if err != nil || got.TenantID != "tenant-a" {
		t.Fatalf("read back = %+v, %v; want tenant-a", got, err)
	}
	if _, err := other.GetTenantAgent(ctx, "tenant-b"); !errors.Is(err, ErrTenantAgentUnknown) {
		t.Fatalf("the body's tenant gained an appointment: %v", err)
	}
}

func TestTenantAgentStore_RefusesAContextScopedToAnotherTenant(t *testing.T) {
	t.Parallel()

	store := NewInMemoryTenantAgentStore()
	ctx := ContextWithTenant(context.Background(), "tenant-b")

	checks := map[string]error{
		"create": store.CreateTenantAgent(ctx, "tenant-a", testTenantAgent("tenant-agent-a")),
		"update": store.UpdateTenantAgent(ctx, "tenant-a", testTenantAgent("tenant-agent-a")),
		"delete": store.DeleteTenantAgent(ctx, "tenant-a"),
	}
	_, checks["get"] = store.GetTenantAgent(ctx, "tenant-a")
	for name, err := range checks {
		if !errors.Is(err, ErrAccountTenantScope) {
			t.Errorf("%s error = %v, want ErrAccountTenantScope", name, err)
		}
	}
}

func TestTenantAgentStore_CorruptRecordIsAStoreError(t *testing.T) {
	t.Parallel()

	kv := kvstore.NewMemory()
	store := NewTenantAgentStore(kv)
	ctx := context.Background()
	key, err := tenantAgentKey("tenant-a")
	if err != nil {
		t.Fatalf("tenantAgentKey() error = %v", err)
	}
	if err := kv.Put(ctx, key, []byte("{not json")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := store.GetTenantAgent(ctx, "tenant-a"); !errors.Is(err, ErrAgentCatalogStore) {
		t.Fatalf("error = %v, want ErrAgentCatalogStore", err)
	}
}
