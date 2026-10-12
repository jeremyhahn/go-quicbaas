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

package iam

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// seedOrder records which seeding steps ran, in order.
type seedOrder struct {
	steps []string
}

type orderedRoleSeeder struct{ order *seedOrder }

func (s orderedRoleSeeder) SeedBuiltInRoles(_ context.Context, tenantID string) error {
	s.order.steps = append(s.order.steps, "roles:"+tenantID)
	return nil
}

func (s orderedRoleSeeder) DeleteBuiltInRoles(_ context.Context, tenantID string) error {
	s.order.steps = append(s.order.steps, "unroles:"+tenantID)
	return nil
}

type orderedPromptSeeder struct {
	order *seedOrder
	err   error
}

func (s orderedPromptSeeder) SeedTenant(_ context.Context, tenantID string) error {
	s.order.steps = append(s.order.steps, "prompts:"+tenantID)
	return s.err
}

// TestCreateTenant_SeedsPromptsAfterRoles proves the prompt set is copied
// into a new tenant, and only once its roles exist.
func TestCreateTenant_SeedsPromptsAfterRoles(t *testing.T) {
	mgr := newTestTenantManager()
	order := &seedOrder{}
	mgr.SetRoleSeeder(orderedRoleSeeder{order: order})
	mgr.SetPromptSeeder(orderedPromptSeeder{order: order})

	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if len(order.steps) != 2 || order.steps[0] != "roles:burnin" || order.steps[1] != "prompts:burnin" {
		t.Fatalf("seeding steps = %v, want roles then prompts", order.steps)
	}
}

// TestCreateTenant_PromptSeedFailureRollsBack proves a prompt-seed failure
// fails the creation and leaves no tenant behind.
func TestCreateTenant_PromptSeedFailureRollsBack(t *testing.T) {
	mgr := newTestTenantManager()
	boom := errors.New("prompt store sealed")
	mgr.SetPromptSeeder(orderedPromptSeeder{order: &seedOrder{}, err: boom})
	engine := &recordingQRDBProvisioner{}
	mgr.SetQRDBTenantProvisioner(engine)
	ctx := context.Background()

	_, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: "burnin", Name: "Burn-in"})
	if !errors.Is(err, boom) {
		t.Fatalf("CreateTenant error = %v, want it to wrap the seed cause", err)
	}
	if _, err := mgr.GetTenant(ctx, "burnin"); err == nil {
		t.Fatal("tenant survived a failed prompt seed; the creation was not rolled back")
	}
	if !engine.createCalled {
		t.Fatal("the engine tenant was never created, so its rollback went unexercised")
	}
}

// TestCreateTenant_PromptSeedFailureUnseedsRoles proves the rollback of a
// failed prompt seed undoes the role seed that succeeded before it.
func TestCreateTenant_PromptSeedFailureUnseedsRoles(t *testing.T) {
	mgr := newTestTenantManager()
	order := &seedOrder{}
	mgr.SetRoleSeeder(orderedRoleSeeder{order: order})
	mgr.SetPromptSeeder(orderedPromptSeeder{order: order, err: errors.New("prompt store sealed")})

	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err == nil {
		t.Fatal("CreateTenant succeeded over a failing prompt seed")
	}
	want := []string{"roles:burnin", "prompts:burnin", "unroles:burnin"}
	if !slices.Equal(order.steps, want) {
		t.Fatalf("seeding steps = %v, want %v", order.steps, want)
	}
}

// TestSetPromptSeeder_Accessor proves the setter/accessor round-trips the
// wired seeder and that an unwired one leaves CreateTenant working.
func TestSetPromptSeeder_Accessor(t *testing.T) {
	mgr := newTestTenantManager()
	if mgr.PromptSeeder() != nil {
		t.Fatal("a fresh manager reports a prompt seeder")
	}
	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err != nil {
		t.Fatalf("CreateTenant with no prompt seeder: %v", err)
	}
	seeder := orderedPromptSeeder{order: &seedOrder{}}
	mgr.SetPromptSeeder(seeder)
	if mgr.PromptSeeder() == nil {
		t.Fatal("PromptSeeder did not return the seeder that was set")
	}
}
