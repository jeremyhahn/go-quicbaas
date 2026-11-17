package iam

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// roleLister is the ListRoles seam both in-process role stores implement.
type roleLister interface {
	CreateRole(ctx context.Context, role *Role) error
	ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error)
}

// assertOffsetPagesCoverEachRoleOnce creates 25 roles in one tenant and 3 in
// another, pages through the first tenant by offset at size 4, and requires
// every role exactly once and no other tenant's role (#1848).
func assertOffsetPagesCoverEachRoleOnce(t *testing.T, store roleLister) {
	t.Helper()
	ctx := context.Background()
	const tenant, other, total, size = "tenant-order", "tenant-other", 25, 4

	want := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("role-%02d", i)
		want[id] = true
		require.NoError(t, store.CreateRole(ctx, &Role{ID: id, TenantID: tenant, Name: "Role " + id}))
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("other-%02d", i)
		require.NoError(t, store.CreateRole(ctx, &Role{ID: id, TenantID: other, Name: "Role " + id}))
	}

	seen := make(map[string]int, total)
	for offset := 0; offset < total+size; offset += size {
		page, err := store.ListRoles(ctx, tenant, ListOptions{Offset: offset, Limit: size})
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), size)
		for _, r := range page {
			require.Truef(t, want[r.ID], "role %q is not in tenant %q", r.ID, tenant)
			seen[r.ID]++
		}
	}
	for id := range want {
		require.Equalf(t, 1, seen[id], "role %q listed %d times across offset pages", id, seen[id])
	}
}

func TestMemoryStorage_ListRolesOffsetPagesCoverEachRoleOnce(t *testing.T) {
	assertOffsetPagesCoverEachRoleOnce(t, NewMemoryStorage())
}

func TestBackendStorage_ListRolesOffsetPagesCoverEachRoleOnce(t *testing.T) {
	assertOffsetPagesCoverEachRoleOnce(t, NewBackendStorage(newMockKVStore()))
}
