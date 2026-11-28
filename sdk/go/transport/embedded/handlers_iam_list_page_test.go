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

package embedded

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// The real SDK client sends its list requests wrapped with the tenant ID
// (#1846): a page of size N holds at most N items, and following
// NextPageToken visits every item exactly once.

const listPageItems = 5

func TestEmbeddedIAMUserList_HonorsPageSizeAndToken(t *testing.T) {
	client, store := newUserClient(t)
	ctx := context.Background()
	want := map[string]bool{}
	for i := 0; i < listPageItems; i++ {
		u, err := store.CreateUser(ctx, "t-1", iam.CreateUserOptions{
			Email:     fmt.Sprintf("u%d@acme.test", i),
			Username:  fmt.Sprintf("user%d", i),
			Name:      fmt.Sprintf("User %d", i),
			Lifecycle: iam.LifecycleActive,
		})
		require.NoError(t, err)
		want[strconv.FormatUint(u.ID, 10)] = true
	}

	seen := map[string]bool{}
	token := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, listPageItems, "paging must end")
		resp, err := client.List(ctx, "t-1", &iamsdk.ListUsersRequest{PageSize: 2, PageToken: token})
		require.NoError(t, err)
		require.LessOrEqual(t, len(resp.Users), 2, "a page must hold at most the page size")
		for _, u := range resp.Users {
			assert.False(t, seen[u.ID], "user %s returned twice", u.ID)
			seen[u.ID] = true
		}
		assert.Equal(t, resp.Pagination.NextPageToken != "", resp.Pagination.HasMore)
		if resp.Pagination.NextPageToken == "" {
			break
		}
		token = resp.Pagination.NextPageToken
	}
	assert.Equal(t, want, seen, "the pages must cover every user")

	all, err := client.List(ctx, "t-1", nil)
	require.NoError(t, err)
	assert.Len(t, all.Users, listPageItems, "no page size lists every user")
	assert.False(t, all.Pagination.HasMore)
}

func TestEmbeddedIAMRoleList_HonorsPageSizeAndToken(t *testing.T) {
	svc, store := newIAMTestService()
	tr, err := New(svc)
	require.NoError(t, err)
	client := iamsdk.NewRoleClient(tr)
	ctx := context.Background()
	want := map[string]bool{}
	for i := 0; i < listPageItems; i++ {
		r, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{Name: fmt.Sprintf("role-%d", i)})
		require.NoError(t, err)
		want[r.ID] = true
	}

	seen := map[string]bool{}
	token := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, listPageItems, "paging must end")
		resp, err := client.List(ctx, "t-1", &iamsdk.ListRolesRequest{PageSize: 2, PageToken: token})
		require.NoError(t, err)
		require.LessOrEqual(t, len(resp.Roles), 2, "a page must hold at most the page size")
		for _, r := range resp.Roles {
			assert.False(t, seen[r.ID], "role %s returned twice", r.ID)
			seen[r.ID] = true
		}
		assert.Equal(t, resp.Pagination.NextPageToken != "", resp.Pagination.HasMore)
		if resp.Pagination.NextPageToken == "" {
			break
		}
		token = resp.Pagination.NextPageToken
	}
	assert.Equal(t, want, seen, "the pages must cover every role")
}

func TestEmbeddedIAMList_RefusesMalformedPageToken(t *testing.T) {
	svc, _ := newIAMTestService()
	tr, err := New(svc)
	require.NoError(t, err)
	ctx := context.Background()

	for _, token := range []string{"next", "-1"} {
		_, err := iamsdk.NewUserClient(tr).List(ctx, "t-1", &iamsdk.ListUsersRequest{PageSize: 2, PageToken: token})
		assert.ErrorContains(t, err, "page token", "user list token %q", token)
		_, err = iamsdk.NewRoleClient(tr).List(ctx, "t-1", &iamsdk.ListRolesRequest{PageSize: 2, PageToken: token})
		assert.ErrorContains(t, err, "page token", "role list token %q", token)
	}
}
