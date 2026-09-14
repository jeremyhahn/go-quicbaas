// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

	"github.com/stretchr/testify/require"
)

// ListImpersonations returns every session, filters by active-only, and by admin.
func TestAdminManager_ListImpersonations(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	s1, err := manager.StartImpersonation(ctxWithActor("admin_user"), &StartImpersonationOptions{
		TargetUserID: "target1",
		TenantID:     "tenant1",
		Reason:       "one",
	})
	require.NoError(t, err)
	s2, err := manager.StartImpersonation(ctxWithActor("admin_user"), &StartImpersonationOptions{
		TargetUserID: "target2",
		TenantID:     "tenant1",
		Reason:       "two",
	})
	require.NoError(t, err)

	// End one so active-only filtering is meaningful.
	require.NoError(t, manager.EndImpersonation(ctx, s2.ID))

	// All sessions for the admin.
	all, err := manager.ListImpersonations(ctx, false, "admin_user")
	require.NoError(t, err)
	if len(all) != 2 {
		t.Fatalf("ListImpersonations(all) = %d, want 2", len(all))
	}

	// Active-only sessions for the admin.
	active, err := manager.ListImpersonations(ctx, true, "admin_user")
	require.NoError(t, err)
	if len(active) != 1 || active[0].ID != s1.ID {
		t.Fatalf("ListImpersonations(active) = %v, want [%s]", active, s1.ID)
	}

	// No sessions for a different admin.
	other, err := manager.ListImpersonations(ctx, false, "other_admin")
	require.NoError(t, err)
	if len(other) != 0 {
		t.Fatalf("ListImpersonations(other) = %d, want 0", len(other))
	}
}

// GetImpersonationSession resolves a known session and reports not-found.
func TestAdminManager_GetImpersonationSession(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	s, err := manager.StartImpersonation(ctxWithActor("admin_user"), &StartImpersonationOptions{
		TargetUserID: "target1",
		TenantID:     "tenant1",
		Reason:       "ok",
	})
	require.NoError(t, err)

	got, err := manager.GetImpersonationSession(ctx, s.ID)
	require.NoError(t, err)
	if got.ID != s.ID {
		t.Fatalf("GetImpersonationSession id = %q, want %q", got.ID, s.ID)
	}

	if _, err := manager.GetImpersonationSession(ctx, "does-not-exist"); !errors.Is(err, ErrImpersonationSessionNotFound) {
		t.Fatalf("GetImpersonationSession(missing) err = %v, want ErrImpersonationSessionNotFound", err)
	}
}

// HasAdmin reflects whether any active admin assignment exists.
func TestAdminManager_HasAdmin(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	has, err := manager.HasAdmin(ctx)
	require.NoError(t, err)
	if has {
		t.Fatal("HasAdmin = true before any assignment, want false")
	}

	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "admin_user",
		AdminRole: AdminRoleSuperAdmin,
		GrantedBy: "system",
	})
	require.NoError(t, err)

	has, err = manager.HasAdmin(ctx)
	require.NoError(t, err)
	if !has {
		t.Fatal("HasAdmin = false after creating an admin, want true")
	}
}
