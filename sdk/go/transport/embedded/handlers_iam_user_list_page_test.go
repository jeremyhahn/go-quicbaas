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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// userListWire mirrors the request shape the SDK user client sends for
// List: the tenant beside the caller's ListUsersRequest.
type userListWire struct {
	TenantID string
	Request  *iamsdk.ListUsersRequest
}

func listUsersPage(t *testing.T, svc *mockQuicBaaSForIAM, size int, token string) (*iamsdk.ListUsersResponse, error) {
	t.Helper()
	out, err := handleIAMUserList(context.Background(), svc, &userListWire{
		TenantID: "t-1",
		Request:  &iamsdk.ListUsersRequest{PageSize: size, PageToken: token},
	})
	if err != nil {
		return nil, err
	}
	resp, ok := out.(*iamsdk.ListUsersResponse)
	require.True(t, ok, "reply must be a *ListUsersResponse")
	return resp, nil
}

func TestEmbeddedIAMUserList_PagesPastFirstPage(t *testing.T) {
	svc, store := newIAMTestService()
	for i := 0; i < 150; i++ {
		_, err := store.CreateUser(context.Background(), "t-1", iam.CreateUserOptions{
			Email:     fmt.Sprintf("user%03d@acme.test", i),
			Username:  fmt.Sprintf("user%03d", i),
			Name:      fmt.Sprintf("User %d", i),
			Lifecycle: iam.LifecycleActive,
		})
		require.NoError(t, err)
	}

	page1, err := listUsersPage(t, svc, 100, "")
	require.NoError(t, err)
	assert.Len(t, page1.Users, 100)
	assert.True(t, page1.Pagination.HasMore)
	assert.Equal(t, 100, page1.Pagination.PageSize)
	require.NotEmpty(t, page1.Pagination.NextPageToken)

	page2, err := listUsersPage(t, svc, 100, page1.Pagination.NextPageToken)
	require.NoError(t, err)
	assert.Len(t, page2.Users, 50)
	assert.False(t, page2.Pagination.HasMore)
	assert.Empty(t, page2.Pagination.NextPageToken)

	seen := make(map[string]bool, 100)
	for _, u := range page1.Users {
		seen[u.ID] = true
	}
	for _, u := range page2.Users {
		assert.False(t, seen[u.ID], "user %s is on both pages", u.ID)
	}
}

func TestEmbeddedIAMUserList_BadPageToken(t *testing.T) {
	svc, _ := newIAMTestService()
	for _, token := range []string{"x", "-1"} {
		_, err := listUsersPage(t, svc, 10, token)
		assert.Error(t, err, "page token %q must be rejected", token)
	}
}
