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
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMemoryStorage_ListRoles_OffsetPagesCoverEachRoleOnce pages through a
// tenant's roles by offset and checks every role comes back exactly once, in
// role ID order (#1848). Map iteration order is random, so without a sort two
// offset pages can repeat a role or skip one.
func TestMemoryStorage_ListRoles_OffsetPagesCoverEachRoleOnce(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStorage()

	const tenantID = "tenant-role-paging"
	const n, k = 23, 4

	var want []string
	for i := range n {
		id := fmt.Sprintf("role-%02d", i)
		want = append(want, id)
		require.NoError(t, m.CreateRole(ctx, &Role{ID: id, TenantID: tenantID, Name: "name-" + id}))
	}
	// A role in another tenant must not appear in this tenant's pages.
	require.NoError(t, m.CreateRole(ctx, &Role{ID: "role-other", TenantID: "tenant-other", Name: "other"}))

	seen := make(map[string]int, n)
	var got []string
	for offset := 0; ; offset += k {
		page, err := m.ListRoles(ctx, tenantID, ListOptions{Offset: offset, Limit: k})
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		require.LessOrEqual(t, len(page), k)
		for _, r := range page {
			seen[r.ID]++
			got = append(got, r.ID)
		}
	}

	for _, id := range want {
		require.Equalf(t, 1, seen[id], "role %s seen %d times across pages", id, seen[id])
	}
	require.Len(t, seen, n, "pages returned a role outside the tenant's set")
	require.True(t, slices.IsSorted(got), "pages are not in role ID order: %v", got)
}
