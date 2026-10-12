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
	"testing"
)

// recordingRoleSeeder records the tenants it was asked to seed and can be told
// to fail, so the tests can assert both that CreateTenant seeds and that a
// seeding failure aborts the creation.
type recordingRoleSeeder struct {
	seeded []string
	err    error
}

func (s *recordingRoleSeeder) DeleteBuiltInRoles(context.Context, string) error { return nil }

func (s *recordingRoleSeeder) SeedBuiltInRoles(_ context.Context, tenantID string) error {
	s.seeded = append(s.seeded, tenantID)
	return s.err
}

// TestCreateTenant_SeedsBuiltInRoles proves tenant creation materializes the
// built-in roles for the new tenant. Without this a fresh tenant holds no
// roles at all and cannot hold a user until an operator recreates them by
// their exact, undocumented names (#269).
func TestCreateTenant_SeedsBuiltInRoles(t *testing.T) {
	mgr := newTestTenantManager()
	seeder := &recordingRoleSeeder{}
	mgr.SetRoleSeeder(seeder)

	tenant, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{
		ID:   "burnin",
		Name: "Burn-in",
	})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if len(seeder.seeded) != 1 || seeder.seeded[0] != "burnin" {
		t.Fatalf("seeded tenants = %v, want [burnin]", seeder.seeded)
	}
	if tenant.ID != "burnin" {
		t.Fatalf("tenant ID = %q, want burnin", tenant.ID)
	}
}

// TestCreateTenant_SeedsGeneratedTenantID proves the seeder is handed the ID
// the manager generated, not the empty one the caller supplied.
func TestCreateTenant_SeedsGeneratedTenantID(t *testing.T) {
	mgr := newTestTenantManager()
	seeder := &recordingRoleSeeder{}
	mgr.SetRoleSeeder(seeder)

	tenant, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{Name: "Generated"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if len(seeder.seeded) != 1 || seeder.seeded[0] != tenant.ID {
		t.Fatalf("seeded tenants = %v, want [%s]", seeder.seeded, tenant.ID)
	}
}

// TestCreateTenant_RoleSeedFailureRollsBack proves a seeding failure fails the
// whole creation and leaves no tenant behind. A tenant that exists but cannot
// hold a user is exactly the trap #269 reports, so half-created is worse than
// not created.
func TestCreateTenant_RoleSeedFailureRollsBack(t *testing.T) {
	mgr := newTestTenantManager()
	boom := errors.New("role store unavailable")
	mgr.SetRoleSeeder(&recordingRoleSeeder{err: boom})
	engine := &recordingQRDBProvisioner{}
	mgr.SetQRDBTenantProvisioner(engine)
	ctx := context.Background()

	if _, err := mgr.CreateTenant(ctx, CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err == nil {
		t.Fatal("CreateTenant succeeded despite a role-seed failure")
	} else if !errors.Is(err, boom) {
		t.Fatalf("CreateTenant error = %v, want it to wrap the seed cause", err)
	}

	if _, err := mgr.GetTenant(ctx, "burnin"); err == nil {
		t.Fatal("tenant survived a failed role seed; the creation was not rolled back")
	}
}

// TestCreateTenant_NoRoleSeederIsANoOp proves an unwired seeder leaves
// CreateTenant working, so embedded consumers that never wire one are not
// broken by the new step.
func TestCreateTenant_NoRoleSeederIsANoOp(t *testing.T) {
	mgr := newTestTenantManager()
	// Deliberately do NOT call SetRoleSeeder.
	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err != nil {
		t.Fatalf("CreateTenant with no seeder: %v", err)
	}
}

// TestSetRoleSeeder_Accessor proves the setter/accessor round-trips the wired
// seeder and tolerates nil.
func TestSetRoleSeeder_Accessor(t *testing.T) {
	mgr := newTestTenantManager()
	if mgr.RoleSeeder() != nil {
		t.Fatal("a fresh manager reports a role seeder")
	}
	seeder := &recordingRoleSeeder{}
	mgr.SetRoleSeeder(seeder)
	if mgr.RoleSeeder() != seeder {
		t.Fatal("RoleSeeder did not return the seeder that was set")
	}
	mgr.SetRoleSeeder(nil)
	if mgr.RoleSeeder() != nil {
		t.Fatal("RoleSeeder did not return nil after being cleared")
	}
}
