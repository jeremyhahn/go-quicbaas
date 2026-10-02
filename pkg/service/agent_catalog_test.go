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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func newCatalogService(t *testing.T) *AgentCatalogService {
	t.Helper()
	dir, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store: orchestration.NewInMemoryAgentCatalogStore(), Models: noModels{},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewAgentCatalogService(dir)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func catalogTenantCtx(tenant string) context.Context {
	return orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), tenant), tenant)
}

func catalogAgent(id string, roles ...orchestration.AgentRole) *orchestration.Agent {
	return &orchestration.Agent{ID: id, Name: id, Backend: "claude", Model: "claude-opus-5-5", Roles: roles}
}

func TestNewAgentCatalogService_NeedsADirectory(t *testing.T) {
	if _, err := NewAgentCatalogService(nil); !errors.Is(err, orchestration.ErrAgentCatalogUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := AgentCatalogServiceFrom(nil); !errors.Is(err, orchestration.ErrAgentCatalogUnavailable) {
		t.Fatalf("an agent service with no catalog = %v", err)
	}
}

func TestAgentCatalogService_AgentLifecycle(t *testing.T) {
	svc := newCatalogService(t)
	ctx := catalogTenantCtx("acme")
	if _, err := svc.CreateAgent(ctx, "acme", catalogAgent("coder", orchestration.AgentRoleCoding)); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	agents, err := svc.ListAgents(ctx, "acme")
	if err != nil || len(agents) != 1 {
		t.Fatalf("ListAgents = %v, %v", agents, err)
	}
	update := catalogAgent("", orchestration.AgentRoleCoding, orchestration.AgentRoleTriage)
	update.Name = "Coder"
	got, err := svc.UpdateAgent(ctx, "acme", "coder", update)
	if err != nil || got.ID != "coder" || len(got.Roles) != 2 {
		t.Fatalf("UpdateAgent = %+v, %v", got, err)
	}
	if a, err := svc.GetAgent(ctx, "acme", "coder"); err != nil || a.Name != "Coder" {
		t.Fatalf("GetAgent = %+v, %v", a, err)
	}
	if used, err := svc.AgentUsedBy(ctx, "acme", "coder"); err != nil || len(used) != 0 {
		t.Fatalf("AgentUsedBy = %v, %v", used, err)
	}
	if err := svc.DeleteAgent(ctx, "acme", "coder"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
}

func TestAgentCatalogService_RefusesBadInputAndOtherTenants(t *testing.T) {
	svc := newCatalogService(t)
	ctx := catalogTenantCtx("acme")
	if _, err := svc.ListAgents(ctx, ""); !errors.Is(err, orchestration.ErrInvalidInput) {
		t.Fatalf("no tenant = %v", err)
	}
	var denied *TenantTargetDeniedError
	if _, err := svc.ListAgents(ctx, "globex"); !errors.As(err, &denied) {
		t.Fatalf("another tenant = %v", err)
	}
	if _, err := svc.UpdateAgent(ctx, "acme", "coder", nil); !errors.Is(err, orchestration.ErrInvalidInput) {
		t.Fatalf("a nil agent = %v", err)
	}
	if _, err := svc.UpdateAgent(ctx, "acme", "coder", catalogAgent("other", orchestration.AgentRoleCoding)); !errors.Is(err, orchestration.ErrInvalidInput) {
		t.Fatalf("a re-keying update = %v", err)
	}
	if _, err := svc.UpdatePool(ctx, "acme", "p", nil); !errors.Is(err, orchestration.ErrInvalidInput) {
		t.Fatalf("a nil pool = %v", err)
	}
	if _, err := svc.UpdatePool(ctx, "acme", "p", &orchestration.AgentPool{ID: "q"}); !errors.Is(err, orchestration.ErrInvalidInput) {
		t.Fatalf("a re-keying pool update = %v", err)
	}
	for name, call := range map[string]func() error{
		"get":       func() error { _, err := svc.GetAgent(ctx, "globex", "a"); return err },
		"create":    func() error { _, err := svc.CreateAgent(ctx, "globex", catalogAgent("a")); return err },
		"update":    func() error { _, err := svc.UpdateAgent(ctx, "globex", "a", catalogAgent("a")); return err },
		"delete":    func() error { return svc.DeleteAgent(ctx, "globex", "a") },
		"used by":   func() error { _, err := svc.AgentUsedBy(ctx, "globex", "a"); return err },
		"pools":     func() error { _, err := svc.ListPools(ctx, "globex"); return err },
		"get pool":  func() error { _, err := svc.GetPool(ctx, "globex", "p"); return err },
		"new pool":  func() error { _, err := svc.CreatePool(ctx, "globex", &orchestration.AgentPool{}); return err },
		"edit pool": func() error { _, err := svc.UpdatePool(ctx, "globex", "p", &orchestration.AgentPool{}); return err },
		"del pool":  func() error { return svc.DeletePool(ctx, "globex", "p") },
		"pool used": func() error { _, err := svc.PoolUsedBy(ctx, "globex", "p"); return err },
		"catalogue": func() error { _, err := svc.Catalogue(ctx, "globex"); return err },
		"image":     func() error { _, err := svc.ReplaceAgentImage(ctx, "globex", "a", []string{"x"}, "y"); return err },
	} {
		if err := call(); !errors.As(err, &denied) {
			t.Fatalf("%s across tenants = %v", name, err)
		}
	}
}

func TestAgentCatalogService_PoolLifecycle(t *testing.T) {
	svc := newCatalogService(t)
	ctx := catalogTenantCtx("acme")
	for _, id := range []string{"a", "b"} {
		if _, err := svc.CreateAgent(ctx, "acme", catalogAgent(id, orchestration.AgentRoleReview)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.CreatePool(ctx, "acme", &orchestration.AgentPool{ID: "p", Name: "P", Agents: []string{"a", "b"}}); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	updated, err := svc.UpdatePool(ctx, "acme", "p", &orchestration.AgentPool{Name: "P", Agents: []string{"b", "a"}})
	if err != nil || updated.Agents[0] != "b" {
		t.Fatalf("UpdatePool = %+v, %v", updated, err)
	}
	if pools, err := svc.ListPools(ctx, "acme"); err != nil || len(pools) != 1 {
		t.Fatalf("ListPools = %v, %v", pools, err)
	}
	if p, err := svc.GetPool(ctx, "acme", "p"); err != nil || p.Name != "P" {
		t.Fatalf("GetPool = %+v, %v", p, err)
	}
	used, err := svc.AgentUsedBy(ctx, "acme", "a")
	if err != nil || len(used) != 1 || used[0].Kind != orchestration.AgentUsageKindPool {
		t.Fatalf("AgentUsedBy = %v, %v", used, err)
	}
	if err := svc.DeleteAgent(ctx, "acme", "a"); !errors.Is(err, orchestration.ErrAgentInUse) {
		t.Fatalf("deleting a pool member = %v", err)
	}
	if used, err := svc.PoolUsedBy(ctx, "acme", "p"); err != nil || len(used) != 0 {
		t.Fatalf("PoolUsedBy = %v, %v", used, err)
	}
	catalogue, err := svc.Catalogue(ctx, "acme")
	if err != nil || len(catalogue.Pools) != 1 {
		t.Fatalf("Catalogue = %+v, %v", catalogue, err)
	}
	if err := svc.DeletePool(ctx, "acme", "p"); err != nil {
		t.Fatalf("DeletePool: %v", err)
	}
}

func TestAgentCatalogService_ReplaceAgentImage(t *testing.T) {
	svc := newCatalogService(t)
	ctx := catalogTenantCtx("acme")
	agent := catalogAgent("coder", orchestration.AgentRoleCoding)
	agent.Image = "w:1"
	if _, err := svc.CreateAgent(ctx, "acme", agent); err != nil {
		t.Fatal(err)
	}
	changed, err := svc.ReplaceAgentImage(ctx, "acme", "coder", []string{"w:1"}, "w:2")
	if err != nil || !changed {
		t.Fatalf("ReplaceAgentImage = %v, %v", changed, err)
	}
	if _, err := svc.ReplaceAgentImage(ctx, "acme", "coder", nil, "w:3"); !errors.Is(err, orchestration.ErrInvalidInput) {
		t.Fatalf("no stale images = %v", err)
	}
}
