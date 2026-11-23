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

package grpc

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const pagingTotal = 5

// pagingIAMServer serves five users, roles and tenants, sliced by the
// request's Offset and Limit as the IAM server does (Limit 0 means 100).
type pagingIAMServer struct {
	iampb.UnimplementedIAMServiceServer
}

func pageBounds(offset, limit int32) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	lo := int(offset)
	if lo > pagingTotal {
		lo = pagingTotal
	}
	hi := min(lo+int(limit), pagingTotal)
	return lo, hi
}

func (pagingIAMServer) ListUsers(_ context.Context, req *iampb.ListUsersRequest) (*iampb.ListUsersResponse, error) {
	lo, hi := pageBounds(req.Offset, req.Limit)
	resp := &iampb.ListUsersResponse{Total: pagingTotal}
	for i := lo; i < hi; i++ {
		resp.Users = append(resp.Users, &iampb.User{Id: fmt.Sprintf("u%d", i)})
	}
	return resp, nil
}

func (pagingIAMServer) ListRoles(_ context.Context, req *iampb.ListRolesRequest) (*iampb.ListRolesResponse, error) {
	lo, hi := pageBounds(req.Offset, req.Limit)
	resp := &iampb.ListRolesResponse{Total: pagingTotal}
	for i := lo; i < hi; i++ {
		resp.Roles = append(resp.Roles, &iampb.Role{Id: fmt.Sprintf("r%d", i)})
	}
	return resp, nil
}

func (pagingIAMServer) ListTenants(_ context.Context, req *iampb.ListTenantsRequest) (*iampb.ListTenantsResponse, error) {
	lo, hi := pageBounds(req.Offset, req.Limit)
	resp := &iampb.ListTenantsResponse{Total: pagingTotal}
	for i := lo; i < hi; i++ {
		resp.Tenants = append(resp.Tenants, &iampb.Tenant{Id: fmt.Sprintf("t%d", i)})
	}
	return resp, nil
}

// continuationTotal is the total the IAM server reports for the app and
// policy lists: one past the page while more items follow, else the end of
// the page.
func continuationTotal(hi int) int32 {
	if hi < pagingTotal {
		return int32(hi + 1)
	}
	return int32(hi)
}

func (pagingIAMServer) ListPolicies(_ context.Context, req *iampb.ListPoliciesRequest) (*iampb.ListPoliciesResponse, error) {
	lo, hi := pageBounds(req.Offset, req.Limit)
	resp := &iampb.ListPoliciesResponse{Total: continuationTotal(hi)}
	for i := lo; i < hi; i++ {
		resp.Policies = append(resp.Policies, &iampb.Policy{Name: fmt.Sprintf("p%d", i), TenantId: req.TenantId})
	}
	return resp, nil
}

func (pagingIAMServer) ListApps(_ context.Context, req *iampb.ListAppsRequest) (*iampb.ListAppsResponse, error) {
	lo, hi := pageBounds(req.Offset, req.Limit)
	resp := &iampb.ListAppsResponse{Total: continuationTotal(hi)}
	for i := lo; i < hi; i++ {
		resp.Apps = append(resp.Apps, &iampb.Application{ClientId: fmt.Sprintf("a%d", i), Name: fmt.Sprintf("app%d", i)})
	}
	return resp, nil
}

func newPagingIAMHandler(t *testing.T) *iamHandler {
	srv := &pagingIAMServer{}
	return newIAMHandler(newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, srv)
	}).dial(t))
}

// pagingList names one list call and how to read a page of its response.
type pagingList struct {
	name    string
	service string
	req     func(tok string) map[string]interface{}
	page    func(t *testing.T, resp interface{}) ([]string, iam.PaginationInfo)
}

var pagingLists = []pagingList{
	{
		name:    "Users",
		service: "quicbaas.IAM.User",
		req: func(tok string) map[string]interface{} {
			return map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"page_size": 2, "page_token": tok}}
		},
		page: func(t *testing.T, resp interface{}) ([]string, iam.PaginationInfo) {
			r, ok := resp.(*iam.ListUsersResponse)
			if !ok {
				t.Fatalf("response is %T, want *iam.ListUsersResponse", resp)
			}
			ids := make([]string, 0, len(r.Users))
			for _, u := range r.Users {
				ids = append(ids, u.ID)
			}
			return ids, r.Pagination
		},
	},
	{
		name:    "Roles",
		service: "quicbaas.IAM.Role",
		req: func(tok string) map[string]interface{} {
			return map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"page_size": 2, "page_token": tok}}
		},
		page: func(t *testing.T, resp interface{}) ([]string, iam.PaginationInfo) {
			r, ok := resp.(*iam.ListRolesResponse)
			if !ok {
				t.Fatalf("response is %T, want *iam.ListRolesResponse", resp)
			}
			ids := make([]string, 0, len(r.Roles))
			for _, x := range r.Roles {
				ids = append(ids, x.ID)
			}
			return ids, r.Pagination
		},
	},
	{
		name:    "Tenants",
		service: "quicbaas.IAM.Tenant",
		req: func(tok string) map[string]interface{} {
			return map[string]interface{}{"page_size": 2, "page_token": tok}
		},
		page: func(t *testing.T, resp interface{}) ([]string, iam.PaginationInfo) {
			r, ok := resp.(*iam.ListTenantsResponse)
			if !ok {
				t.Fatalf("response is %T, want *iam.ListTenantsResponse", resp)
			}
			ids := make([]string, 0, len(r.Tenants))
			for _, x := range r.Tenants {
				ids = append(ids, x.ID)
			}
			return ids, r.Pagination
		},
	},
}

func TestIAMListPaging_WalksEveryPage(t *testing.T) {
	h := newPagingIAMHandler(t)
	ctx := context.Background()
	for _, l := range pagingLists {
		t.Run(l.name, func(t *testing.T) {
			seen := map[string]int{}
			tok := ""
			calls := 0
			var last iam.PaginationInfo
			for calls < 10 {
				resp, err := h.handleRequest(ctx, l.service, "List", l.req(tok))
				if err != nil {
					t.Fatalf("call %d: %v", calls, err)
				}
				calls++
				ids, p := l.page(t, resp)
				for _, id := range ids {
					seen[id]++
				}
				last = p
				if p.NextPageToken == "" {
					break
				}
				tok = p.NextPageToken
			}
			if calls != 3 {
				t.Errorf("calls = %d, want 3", calls)
			}
			if len(seen) != pagingTotal {
				t.Errorf("saw %d distinct ids, want %d: %v", len(seen), pagingTotal, seen)
			}
			for id, n := range seen {
				if n != 1 {
					t.Errorf("id %s seen %d times, want 1", id, n)
				}
			}
			if last.HasMore {
				t.Errorf("last page HasMore = true, want false")
			}
			if last.TotalCount != pagingTotal {
				t.Errorf("TotalCount = %d, want %d", last.TotalCount, pagingTotal)
			}
		})
	}
}

func TestIAMListPaging_RejectsBadToken(t *testing.T) {
	h := newPagingIAMHandler(t)
	for _, l := range pagingLists {
		if _, err := h.handleRequest(context.Background(), l.service, "List", l.req("x")); err == nil {
			t.Errorf("%s: page_token \"x\" returned no error", l.name)
		}
	}
}

func policyListRequest(cursor string) map[string]interface{} {
	return map[string]interface{}{"tenant_id": "t1", "request": map[string]interface{}{"limit": 2, "cursor": cursor}}
}

func TestIAMListPaging_PoliciesWalkEveryPage(t *testing.T) {
	h := newPagingIAMHandler(t)
	ctx := context.Background()
	seen := map[string]int{}
	cursor := ""
	calls := 0
	for {
		if calls >= 10 {
			t.Fatalf("the walk did not end after %d calls", calls)
		}
		resp, err := h.handleRequest(ctx, iamPolicyServicePrefix, "List", policyListRequest(cursor))
		if err != nil {
			t.Fatalf("call %d: %v", calls, err)
		}
		calls++
		r, ok := resp.(*iam.ListPoliciesResponse)
		if !ok {
			t.Fatalf("response is %T, want *iam.ListPoliciesResponse", resp)
		}
		if len(r.Policies) > 2 {
			t.Fatalf("call %d returned %d policies, want at most 2", calls, len(r.Policies))
		}
		for _, p := range r.Policies {
			seen[p.Name]++
		}
		if !r.HasMore {
			if r.NextCursor != "" {
				t.Errorf("last page NextCursor = %q, want empty", r.NextCursor)
			}
			break
		}
		if r.NextCursor == "" {
			t.Fatalf("call %d: HasMore with no NextCursor", calls)
		}
		cursor = r.NextCursor
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	if len(seen) != pagingTotal {
		t.Errorf("saw %d distinct policies, want %d: %v", len(seen), pagingTotal, seen)
	}
	for name, n := range seen {
		if n != 1 {
			t.Errorf("policy %s seen %d times, want 1", name, n)
		}
	}
}

func TestIAMListPaging_PoliciesRejectBadCursor(t *testing.T) {
	h := newPagingIAMHandler(t)
	for _, cursor := range []string{"x", "-1"} {
		_, err := h.handleRequest(context.Background(), iamPolicyServicePrefix, "List", policyListRequest(cursor))
		if err == nil {
			t.Errorf("cursor %q returned no error", cursor)
			continue
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("cursor %q: error %v is not invalid input", cursor, err)
		}
	}
}
