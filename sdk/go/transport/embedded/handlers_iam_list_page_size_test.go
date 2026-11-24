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
	"fmt"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// embeddedListUsersEnvelope and embeddedListRolesEnvelope mirror the SDK
// client's listUsersRequestInternal and listRolesRequestInternal: the page
// size travels inside Request, not on the envelope.
type embeddedListUsersEnvelope struct {
	TenantID string
	Request  *iamsdk.ListUsersRequest
}

type embeddedListRolesEnvelope struct {
	TenantID string
	Request  *iamsdk.ListRolesRequest
}

// TestHandleIAMUserList_HonorsWrappedPageSize proves the embedded user list
// reads the page size from the client's wrapper request (#1846).
func TestHandleIAMUserList_HonorsWrappedPageSize(t *testing.T) {
	svc, _ := newIAMTestService()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("pager%d", i)
		if _, err := handleIAMUserCreate(ctx, svc, &embeddedCreateUserEnvelope{
			TenantID: "t-1",
			Request:  &iamsdk.CreateUserRequest{Username: name, Email: name + "@example.com"},
		}); err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}
	}

	out, err := handleIAMUserList(ctx, svc, &embeddedListUsersEnvelope{
		TenantID: "t-1",
		Request:  &iamsdk.ListUsersRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("handleIAMUserList failed: %v", err)
	}
	resp, ok := out.(*iamsdk.ListUsersResponse)
	if !ok {
		t.Fatalf("expected *iamsdk.ListUsersResponse, got %T", out)
	}
	if len(resp.Users) != 2 {
		t.Fatalf("page size 2: got %d users, want 2", len(resp.Users))
	}
}

// TestHandleIAMRoleList_HonorsWrappedPageSize proves the embedded role list
// reads the page size from the client's wrapper request (#1846).
func TestHandleIAMRoleList_HonorsWrappedPageSize(t *testing.T) {
	svc, store := newIAMTestService()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{Name: fmt.Sprintf("pager-role-%d", i)}); err != nil {
			t.Fatalf("create role %d: %v", i, err)
		}
	}

	out, err := handleIAMRoleList(ctx, svc, &embeddedListRolesEnvelope{
		TenantID: "t-1",
		Request:  &iamsdk.ListRolesRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("handleIAMRoleList failed: %v", err)
	}
	resp, ok := out.(*iamsdk.ListRolesResponse)
	if !ok {
		t.Fatalf("expected *iamsdk.ListRolesResponse, got %T", out)
	}
	if len(resp.Roles) != 2 {
		t.Fatalf("page size 2: got %d roles, want 2", len(resp.Roles))
	}
}
