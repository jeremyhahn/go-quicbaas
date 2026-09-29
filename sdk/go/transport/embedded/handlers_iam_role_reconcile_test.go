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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// driftingRoleStorage is a role storage that speaks the built-in role
// contract the way qrdb-backed storage does: reads carry the retired
// permissions derived from the live registry, and ReconcileBuiltInRoles
// reports what converging the tenant changed. The in-memory storage the
// other IAM tests use deliberately does not implement
// iam.BuiltInRoleReconciler, so it cannot exercise either path.
type driftingRoleStorage struct {
	iam.RoleStorage

	// retired is stamped onto every role read, standing in for the
	// registry-derived drift column.
	retired []string

	// report is what reconciliation returns.
	report *iam.RoleReconcileReport

	// err, when set, is returned instead of report.
	err error

	// tenants records the tenant IDs reconciliation was asked to converge.
	tenants []string
}

func (s *driftingRoleStorage) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	role, err := s.RoleStorage.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}
	role.RetiredPermissions = s.retired
	return role, nil
}

func (s *driftingRoleStorage) ListRoles(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.Role, error) {
	roles, err := s.RoleStorage.ListRoles(ctx, tenantID, opts)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		role.RetiredPermissions = s.retired
	}
	return roles, nil
}

func (s *driftingRoleStorage) ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*iam.RoleReconcileReport, error) {
	s.tenants = append(s.tenants, tenantID)
	if s.err != nil {
		return nil, s.err
	}
	return s.report, nil
}

// newReconcilingIAMTestService builds an embedded service whose role storage
// can reconcile built-in roles.
func newReconcilingIAMTestService(roles *driftingRoleStorage) (*mockQuicBaaSForIAM, *iam.IdentityStore) {
	mem := iam.NewMemoryStorage()
	roles.RoleStorage = mem
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: roles,
	})
	return &mockQuicBaaSForIAM{store: store}, store
}

// embeddedReconcileRolesEnvelope mirrors the SDK client's internal transport
// wrapper; the handler extracts TenantID via reflection.
type embeddedReconcileRolesEnvelope struct {
	TenantID string
}

// TestHandleIAMRoleReconcile_ReportsChangedRoles proves the embedded transport
// carries the whole reconcile report — the per-role added/removed permissions
// included — rather than collapsing it to a bare success.
func TestHandleIAMRoleReconcile_ReportsChangedRoles(t *testing.T) {
	roles := &driftingRoleStorage{
		report: &iam.RoleReconcileReport{
			TenantID: "t-1",
			Roles: []iam.RoleReconcileResult{
				{
					RoleID:  "role-admin",
					Name:    "admin",
					Added:   []string{"sdlc:write"},
					Removed: []string{"legacy:read"},
				},
				{
					RoleID:  "role-operator",
					Name:    "operator",
					Created: true,
					Added:   []string{"kv:read", "kv:write"},
				},
			},
		},
	}
	svc, _ := newReconcilingIAMTestService(roles)

	out, err := handleIAMRoleReconcile(context.Background(), svc, &embeddedReconcileRolesEnvelope{TenantID: "t-1"})
	if err != nil {
		t.Fatalf("handleIAMRoleReconcile failed: %v", err)
	}

	resp, ok := out.(*iamsdk.ReconcileRolesResponse)
	if !ok {
		t.Fatalf("expected *iamsdk.ReconcileRolesResponse, got %T", out)
	}
	if resp.TenantID != "t-1" {
		t.Errorf("expected tenant t-1, got %s", resp.TenantID)
	}
	if !resp.Changed {
		t.Error("expected changed to be true")
	}
	if len(resp.Roles) != 2 {
		t.Fatalf("expected 2 reconciled roles, got %d", len(resp.Roles))
	}
	if resp.Roles[0].Name != "admin" || len(resp.Roles[0].Added) != 1 || resp.Roles[0].Added[0] != "sdlc:write" {
		t.Errorf("unexpected admin result: %+v", resp.Roles[0])
	}
	if len(resp.Roles[0].Removed) != 1 || resp.Roles[0].Removed[0] != "legacy:read" {
		t.Errorf("expected admin to drop legacy:read, got %v", resp.Roles[0].Removed)
	}
	if !resp.Roles[1].Created || resp.Roles[1].RoleID != "role-operator" {
		t.Errorf("unexpected operator result: %+v", resp.Roles[1])
	}
	if len(roles.tenants) != 1 || roles.tenants[0] != "t-1" {
		t.Errorf("expected reconciliation of t-1 only, got %v", roles.tenants)
	}
}

// TestHandleIAMRoleReconcile_Idempotent proves a tenant that already agrees
// with the build reports no changes, so the verb is safe to run repeatedly.
func TestHandleIAMRoleReconcile_Idempotent(t *testing.T) {
	roles := &driftingRoleStorage{report: &iam.RoleReconcileReport{TenantID: "t-1"}}
	svc, _ := newReconcilingIAMTestService(roles)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		out, err := handleIAMRoleReconcile(ctx, svc, &embeddedReconcileRolesEnvelope{TenantID: "t-1"})
		if err != nil {
			t.Fatalf("handleIAMRoleReconcile failed on call %d: %v", i, err)
		}
		resp, ok := out.(*iamsdk.ReconcileRolesResponse)
		if !ok {
			t.Fatalf("expected *iamsdk.ReconcileRolesResponse, got %T", out)
		}
		if resp.Changed {
			t.Errorf("call %d: expected changed to be false", i)
		}
		if len(resp.Roles) != 0 {
			t.Errorf("call %d: expected no reconciled roles, got %v", i, resp.Roles)
		}
	}
}

// TestHandleIAMRoleReconcile_Errors covers the refusal paths: a missing tenant
// ID, a role storage that cannot reach the build's built-in definitions, and a
// storage failure.
func TestHandleIAMRoleReconcile_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("empty tenant ID", func(t *testing.T) {
		roles := &driftingRoleStorage{report: &iam.RoleReconcileReport{}}
		svc, _ := newReconcilingIAMTestService(roles)

		if _, err := handleIAMRoleReconcile(ctx, svc, &embeddedReconcileRolesEnvelope{}); err == nil {
			t.Fatal("expected an error for an empty tenant ID")
		}
		if len(roles.tenants) != 0 {
			t.Errorf("expected no reconciliation attempt, got %v", roles.tenants)
		}
	})

	t.Run("storage cannot reconcile", func(t *testing.T) {
		svc, _ := newIAMTestService()

		_, err := handleIAMRoleReconcile(ctx, svc, &embeddedReconcileRolesEnvelope{TenantID: "t-1"})
		if err == nil {
			t.Fatal("expected an error for a storage without built-in definitions")
		}
		if !errors.Is(err, iam.ErrRoleReconcileUnsupported) {
			t.Errorf("expected ErrRoleReconcileUnsupported, got %v", err)
		}
	})

	t.Run("storage failure", func(t *testing.T) {
		roles := &driftingRoleStorage{err: errors.New("role storage unavailable")}
		svc, _ := newReconcilingIAMTestService(roles)

		if _, err := handleIAMRoleReconcile(ctx, svc, &embeddedReconcileRolesEnvelope{TenantID: "t-1"}); err == nil {
			t.Fatal("expected the storage failure to surface")
		}
	})

	t.Run("identity store not configured", func(t *testing.T) {
		svc := &mockQuicBaaSForIAM{}

		if _, err := handleIAMRoleReconcile(ctx, svc, &embeddedReconcileRolesEnvelope{TenantID: "t-1"}); err == nil {
			t.Fatal("expected an error when no identity store is configured")
		}
	})
}

// TestIAMRoleReconcileDispatchRegistered proves the verb is reachable over the
// embedded transport under the wire name every other transport uses; an
// unregistered method would fail with unknown-method, the regression this
// issue forbids.
func TestIAMRoleReconcileDispatchRegistered(t *testing.T) {
	if _, ok := iamUnaryHandlers["quicbaas.IAM.Role/Reconcile"]; !ok {
		t.Fatal("expected quicbaas.IAM.Role/Reconcile in the IAM dispatch table")
	}
	if _, ok := unaryHandlers["quicbaas.IAM.Role/Reconcile"]; !ok {
		t.Fatal("expected quicbaas.IAM.Role/Reconcile in the global dispatch table")
	}
}

// TestHandleIAMRoleReadsCarryRetiredPermissions proves the embedded role reads
// carry the drift column, so a listing shows which stored permissions the
// running build no longer defines before anyone reconciles.
func TestHandleIAMRoleReadsCarryRetiredPermissions(t *testing.T) {
	roles := &driftingRoleStorage{retired: []string{"legacy:read"}}
	svc, store := newReconcilingIAMTestService(roles)
	ctx := context.Background()

	role, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{
		Name:        "auditor",
		Description: "Reads audit records",
		Permissions: []string{"audit:read", "legacy:read"},
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	t.Run("list", func(t *testing.T) {
		out, err := handleIAMRoleList(ctx, svc, &embeddedReconcileRolesEnvelope{TenantID: "t-1"})
		if err != nil {
			t.Fatalf("handleIAMRoleList failed: %v", err)
		}
		resp, ok := out.(*iamsdk.ListRolesResponse)
		if !ok {
			t.Fatalf("expected *iamsdk.ListRolesResponse, got %T", out)
		}
		if len(resp.Roles) != 1 {
			t.Fatalf("expected 1 role, got %d", len(resp.Roles))
		}
		if len(resp.Roles[0].Permissions) != 2 {
			t.Errorf("expected the role's permissions, got %v", resp.Roles[0].Permissions)
		}
		if len(resp.Roles[0].RetiredPermissions) != 1 || resp.Roles[0].RetiredPermissions[0] != "legacy:read" {
			t.Errorf("expected retired [legacy:read], got %v", resp.Roles[0].RetiredPermissions)
		}
		if resp.Roles[0].Description != "Reads audit records" {
			t.Errorf("expected the role description, got %q", resp.Roles[0].Description)
		}
	})

	t.Run("get", func(t *testing.T) {
		out, err := handleIAMRoleGet(ctx, svc, &embeddedGetRoleEnvelope{TenantID: "t-1", RoleID: role.ID})
		if err != nil {
			t.Fatalf("handleIAMRoleGet failed: %v", err)
		}
		got, ok := out.(*iamsdk.Role)
		if !ok {
			t.Fatalf("expected *iamsdk.Role, got %T", out)
		}
		if len(got.Permissions) != 2 {
			t.Errorf("expected the role's permissions, got %v", got.Permissions)
		}
		if len(got.RetiredPermissions) != 1 || got.RetiredPermissions[0] != "legacy:read" {
			t.Errorf("expected retired [legacy:read], got %v", got.RetiredPermissions)
		}
	})

	t.Run("get unknown role", func(t *testing.T) {
		if _, err := handleIAMRoleGet(ctx, svc, &embeddedGetRoleEnvelope{TenantID: "t-1", RoleID: "missing"}); err == nil {
			t.Fatal("expected an error for an unknown role")
		}
	})
}

// embeddedGetRoleEnvelope mirrors the SDK client's internal get/delete wrapper.
type embeddedGetRoleEnvelope struct {
	TenantID string
	RoleID   string
}
