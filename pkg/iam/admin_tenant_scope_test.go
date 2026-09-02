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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// mapTargetResolver resolves only the (tenant, user) pairs it was built
// with — the strict resolver the #357 verification tests use, in contrast
// to admin_test.go's allowAllTargetResolver.
type mapTargetResolver struct {
	users map[string]map[string]bool // tenantID -> userID -> exists
}

func (r *mapTargetResolver) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	if r.users[tenantID][userID] {
		return &User{ID: userID, TenantID: tenantID}, nil
	}
	return nil, ErrUserNotFound
}

// newTenantScopeTestManager builds an AdminManager whose resolver knows
// exactly: tenant-user-1 in tenant-1, and system-operator-2 in the system
// tenant.
func newTenantScopeTestManager(t *testing.T) *AdminManager {
	t.Helper()
	manager, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         NewMemoryAdminStorage(),
		ImpersonationStorage: NewMemoryImpersonationStorage(),
		TargetResolver: &mapTargetResolver{users: map[string]map[string]bool{
			"tenant-1":             {"tenant-user-1": true},
			qrdbsdk.SystemTenantID: {"system-operator-2": true},
		}},
	})
	require.NoError(t, err)
	return manager
}

// TestAdminManager_CheckPermission_EmptyTenantIDs is the go-quicbaas#357
// regression guard for the tenant rule: an assignment with EMPTY TenantIDs
// is scoped to the system tenant alone — it matches an unscoped check or a
// system-tenant target and NEVER another tenant. Before the fix, empty
// matched every tenant, so the firstboot super_admin passed tenant-scoped
// checks (impersonation included) for any tenant on the platform.
func TestAdminManager_CheckPermission_EmptyTenantIDs(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	// The firstboot super_admin's shape: nil TenantIDs, granted by
	// system:first-boot (pkg/server/iam_init.go).
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "firstboot-admin",
		AdminRole: AdminRoleSuperAdmin,
		TenantIDs: nil,
		GrantedBy: "system:first-boot",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		tenantID    string
		wantAllowed bool
	}{
		{
			name:        "unscoped check is allowed (platform management keeps working)",
			tenantID:    "",
			wantAllowed: true,
		},
		{
			name:        "system-tenant target is allowed (the assignment's own scope)",
			tenantID:    qrdbsdk.SystemTenantID,
			wantAllowed: true,
		},
		{
			name:        "tenant-scoped check is denied — empty TenantIDs is not all-tenants (#357)",
			tenantID:    "tenant-1",
			wantAllowed: false,
		},
		{
			name:        "another tenant-scoped check is denied too",
			tenantID:    "tenant-2",
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := manager.CheckPermission(ctx, &AdminCheckRequest{
				UserID:     "firstboot-admin",
				Permission: PermUserImpersonate,
				TenantID:   tt.tenantID,
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.wantAllowed, resp.Allowed, "reason: %s", resp.Reason)
		})
	}
}

// TestAdminManager_StartImpersonation_SystemPrincipalDenied pins the
// go-quicbaas#357 impersonation consequences: the firstboot super_admin
// (empty TenantIDs — system-tenant scope) cannot impersonate any tenant's
// users; impersonating a verified SYSTEM-tenant user — the assignment's
// own scope — still works; the retained break-glass path is an assignment
// explicitly naming the target tenant; and no session is ever minted for
// a target the identity resolver cannot place in the target tenant.
func TestAdminManager_StartImpersonation_SystemPrincipalDenied(t *testing.T) {
	ctx := context.Background()
	manager := newTenantScopeTestManager(t)

	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "firstboot-admin",
		AdminRole: AdminRoleSuperAdmin,
		TenantIDs: nil,
		GrantedBy: "system:first-boot",
	})
	require.NoError(t, err)

	t.Run("impersonating a tenant user is denied by default", func(t *testing.T) {
		_, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
			AdminUserID:  "firstboot-admin",
			TargetUserID: "tenant-user-1",
			TenantID:     "tenant-1",
			Reason:       "operator overreach attempt",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrImpersonationNotAllowed),
			"want ErrImpersonationNotAllowed, got %v", err)
	})

	t.Run("impersonating a verified system-tenant user still works", func(t *testing.T) {
		session, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
			AdminUserID:  "firstboot-admin",
			TargetUserID: "system-operator-2",
			TenantID:     qrdbsdk.SystemTenantID,
			Reason:       "system-scope support session",
		})
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.Equal(t, qrdbsdk.SystemTenantID, session.TenantID)
	})

	t.Run("a tenant user ID smuggled under a system-tenant session mints nothing", func(t *testing.T) {
		// tenant-user-1 is not a system-tenant user, so the identity
		// resolver cannot place it there and no session comes into
		// existence — the unverified-principal path fails closed (#357).
		_, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
			AdminUserID:  "firstboot-admin",
			TargetUserID: "tenant-user-1",
			TenantID:     qrdbsdk.SystemTenantID,
			Reason:       "ID smuggling attempt",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrImpersonationTargetNotFound),
			"want ErrImpersonationTargetNotFound, got %v", err)
	})

	t.Run("an explicit tenant-scoped grant is the retained break-glass path", func(t *testing.T) {
		_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
			UserID:    "breakglass-admin",
			AdminRole: AdminRoleSuperAdmin,
			TenantIDs: []string{"tenant-1"},
			GrantedBy: "owner:break-glass",
		})
		require.NoError(t, err)

		session, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
			AdminUserID:  "breakglass-admin",
			TargetUserID: "tenant-user-1",
			TenantID:     "tenant-1",
			Reason:       "explicit audited grant",
		})
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.Equal(t, "tenant-1", session.TenantID)

		// The same grant does not reach a tenant it does not name.
		_, err = manager.StartImpersonation(ctx, &StartImpersonationOptions{
			AdminUserID:  "breakglass-admin",
			TargetUserID: "tenant-user-2",
			TenantID:     "tenant-2",
			Reason:       "should be denied",
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrImpersonationNotAllowed))
	})
}

// failingTargetResolver fails every lookup with a non-not-found error —
// the shape of a sealed or unavailable identity store.
type failingTargetResolver struct{}

func (failingTargetResolver) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	return nil, errors.New("identity store unavailable")
}

// TestAdminManager_StartImpersonation_ResolverErrorIsNotNotFound proves a
// resolver failure that is NOT a not-found is propagated as an
// operation failure rather than masked as ErrImpersonationTargetNotFound:
// callers must be able to tell "user does not exist" from "lookup could
// not be performed" (a sealed store, cancellation, storage fault). Either
// way, no session is minted.
func TestAdminManager_StartImpersonation_ResolverErrorIsNotNotFound(t *testing.T) {
	ctx := context.Background()
	manager, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         NewMemoryAdminStorage(),
		ImpersonationStorage: NewMemoryImpersonationStorage(),
		TargetResolver:       failingTargetResolver{},
	})
	require.NoError(t, err)

	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "admin-1",
		AdminRole: AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant-1"},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	_, err = manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin-1",
		TargetUserID: "tenant-user-1",
		TenantID:     "tenant-1",
		Reason:       "resolver outage",
	})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrImpersonationTargetNotFound),
		"a lookup failure must not masquerade as target-not-found, got %v", err)
}

// TestAdminManager_StartImpersonation_ResolverRequired proves a manager
// with no target resolver wired cannot impersonate at all: verification
// cannot happen, so the path fails closed rather than minting a session
// for an unverified principal (#357).
func TestAdminManager_StartImpersonation_ResolverRequired(t *testing.T) {
	ctx := context.Background()
	manager, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         NewMemoryAdminStorage(),
		ImpersonationStorage: NewMemoryImpersonationStorage(),
	})
	require.NoError(t, err)

	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "admin-1",
		AdminRole: AdminRoleSuperAdmin,
		TenantIDs: []string{"tenant-1"},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	_, err = manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin-1",
		TargetUserID: "tenant-user-1",
		TenantID:     "tenant-1",
		Reason:       "no resolver wired",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrImpersonationTargetResolverRequired),
		"want ErrImpersonationTargetResolverRequired, got %v", err)
}

// TestListAssignments_TenantFilter_EmptyTenantIDs proves both assignment
// storages apply the SAME tenant rule as CheckPermission
// (assignmentMatchesTenant): a tenant-filtered listing surfaces only
// assignments naming that tenant; an empty-TenantIDs (system-scope)
// assignment appears under an unfiltered or system-tenant listing, never
// under a customer tenant's (#357).
func TestListAssignments_TenantFilter_EmptyTenantIDs(t *testing.T) {
	ctx := context.Background()

	stores := map[string]AdminStorage{
		"memory":  NewMemoryAdminStorage(),
		"backend": NewBackendAdminStorage(newMockKVStore()),
	}

	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{
				ID: "sys-scope", UserID: "firstboot-admin", AdminRole: AdminRoleSuperAdmin,
				TenantIDs: nil, Active: true,
			}))
			require.NoError(t, store.CreateAssignment(ctx, &AdminAssignment{
				ID: "t1-scope", UserID: "tenant-admin", AdminRole: AdminRoleTenantAdmin,
				TenantIDs: []string{"tenant-1"}, Active: true,
			}))

			ids := func(as []*AdminAssignment) []string {
				out := make([]string, 0, len(as))
				for _, a := range as {
					out = append(out, a.ID)
				}
				return out
			}

			unfiltered, err := store.ListAssignments(ctx, ListAdminAssignmentsOptions{})
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"sys-scope", "t1-scope"}, ids(unfiltered))

			tenantFiltered, err := store.ListAssignments(ctx, ListAdminAssignmentsOptions{TenantID: "tenant-1"})
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"t1-scope"}, ids(tenantFiltered),
				"a tenant-filtered listing must not surface system-scope grants")

			systemFiltered, err := store.ListAssignments(ctx, ListAdminAssignmentsOptions{TenantID: qrdbsdk.SystemTenantID})
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"sys-scope"}, ids(systemFiltered))
		})
	}
}
