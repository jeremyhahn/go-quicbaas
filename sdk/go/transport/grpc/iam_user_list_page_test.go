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
	"testing"

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// userPagingIAMServer serves a 150-user tenant: it records the offset and limit
// each ListUsers call asks for and returns up to Limit users from there.
type userPagingIAMServer struct {
	iampb.UnimplementedIAMServiceServer
	gotOffset int32
	gotLimit  int32
}

const userPagingTotal = 150

// pagingWindow returns the users of the 150-user tenant in [offset, offset+limit).
func pagingWindow(offset, limit int32) []*iampb.User {
	n := int(limit)
	if rest := userPagingTotal - int(offset); n > rest {
		n = rest
	}
	if n < 0 {
		n = 0
	}
	users := make([]*iampb.User, 0, n)
	for i := 0; i < n; i++ {
		users = append(users, sampleUser())
	}
	return users
}

func (p *userPagingIAMServer) ListUsers(_ context.Context, req *iampb.ListUsersRequest) (*iampb.ListUsersResponse, error) {
	p.gotOffset = req.Offset
	p.gotLimit = req.Limit
	return &iampb.ListUsersResponse{Users: pagingWindow(req.Offset, req.Limit), Total: userPagingTotal}, nil
}

// rowCountIAMServer is the production shape: it serves the same 150-user
// window but reports as Total only the rows it returned, exactly as
// pkg/api/grpc's IAMServiceAdapter does.
type rowCountIAMServer struct {
	iampb.UnimplementedIAMServiceServer
	gotLimit int32
}

func (r *rowCountIAMServer) ListUsers(_ context.Context, req *iampb.ListUsersRequest) (*iampb.ListUsersResponse, error) {
	r.gotLimit = req.Limit
	users := pagingWindow(req.Offset, req.Limit)
	return &iampb.ListUsersResponse{Users: users, Total: int32(len(users))}, nil
}

// newFakeIAMHandler serves fake on a test gRPC server and returns a handler dialled to it.
func newFakeIAMHandler(t *testing.T, fake iampb.IAMServiceServer) *iamHandler {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, fake)
	})
	return newIAMHandler(srv.dial(t))
}

func newUserPagingIAMHandler(t *testing.T) (*iamHandler, *userPagingIAMServer) {
	t.Helper()
	fake := &userPagingIAMServer{}
	return newFakeIAMHandler(t, fake), fake
}

func listUsersAt(t *testing.T, h *iamHandler, token string) (*iam.ListUsersResponse, error) {
	t.Helper()
	return listUsersWith(t, h, map[string]interface{}{"page_size": 100, "page_token": token})
}

func listUsersWith(t *testing.T, h *iamHandler, request map[string]interface{}) (*iam.ListUsersResponse, error) {
	t.Helper()
	out, err := h.handleRequest(context.Background(), "quicbaas.IAM.User", "List", map[string]interface{}{
		"tenant_id": "t1",
		"request":   request,
	})
	if err != nil {
		return nil, err
	}
	resp, ok := out.(*iam.ListUsersResponse)
	if !ok {
		t.Fatalf("reply is %T, want *iam.ListUsersResponse", out)
	}
	return resp, nil
}

func TestIAMService_ListUsers_PageTokenIsOffset(t *testing.T) {
	h, fake := newUserPagingIAMHandler(t)

	first, err := listUsersAt(t, h, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if fake.gotOffset != 0 {
		t.Fatalf("first page sent Offset %d, want 0", fake.gotOffset)
	}
	if fake.gotLimit != 101 {
		t.Fatalf("first page sent Limit %d, want 101 (the page and one lookahead row)", fake.gotLimit)
	}
	if len(first.Users) != 100 || !first.Pagination.HasMore || first.Pagination.NextPageToken != "100" {
		t.Fatalf("first page: %d users, HasMore %v, token %q; want 100, true, \"100\"",
			len(first.Users), first.Pagination.HasMore, first.Pagination.NextPageToken)
	}
	if first.Pagination.PageSize != 100 || first.Pagination.TotalCount != userPagingTotal {
		t.Fatalf("first page: PageSize %d, TotalCount %d", first.Pagination.PageSize, first.Pagination.TotalCount)
	}

	last, err := listUsersAt(t, h, "100")
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if fake.gotOffset != 100 {
		t.Fatalf("page token \"100\" sent Offset %d, want 100", fake.gotOffset)
	}
	if len(last.Users) != 50 || last.Pagination.HasMore || last.Pagination.NextPageToken != "" {
		t.Fatalf("last page: %d users, HasMore %v, token %q; want 50, false, \"\"",
			len(last.Users), last.Pagination.HasMore, last.Pagination.NextPageToken)
	}
}

func TestIAMService_ListUsers_BadPageToken(t *testing.T) {
	h, _ := newUserPagingIAMHandler(t)
	for _, token := range []string{"x", "-1"} {
		if _, err := listUsersAt(t, h, token); err == nil {
			t.Fatalf("page token %q must be rejected", token)
		}
	}
}

func TestIAMService_ListUsers_PagesWhenTotalIsRowCount(t *testing.T) {
	h := newFakeIAMHandler(t, &rowCountIAMServer{})

	first, err := listUsersAt(t, h, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Users) != 100 || !first.Pagination.HasMore || first.Pagination.NextPageToken != "100" {
		t.Fatalf("first page: %d users, HasMore %v, token %q; want 100, true, \"100\"",
			len(first.Users), first.Pagination.HasMore, first.Pagination.NextPageToken)
	}
	if first.Pagination.TotalCount != 100 {
		t.Fatalf("first page: TotalCount %d, want 100", first.Pagination.TotalCount)
	}

	last, err := listUsersAt(t, h, first.Pagination.NextPageToken)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(last.Users) != 50 || last.Pagination.HasMore || last.Pagination.NextPageToken != "" {
		t.Fatalf("last page: %d users, HasMore %v, token %q; want 50, false, \"\"",
			len(last.Users), last.Pagination.HasMore, last.Pagination.NextPageToken)
	}
	if last.Pagination.TotalCount != 50 {
		t.Fatalf("last page: TotalCount %d, want 50", last.Pagination.TotalCount)
	}
}

func TestIAMService_ListUsers_DefaultPageSize(t *testing.T) {
	fake := &rowCountIAMServer{}
	h := newFakeIAMHandler(t, fake)

	resp, err := listUsersWith(t, h, map[string]interface{}{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if fake.gotLimit != 101 {
		t.Fatalf("no page_size sent Limit %d, want 101", fake.gotLimit)
	}
	if resp.Pagination.PageSize != 100 {
		t.Fatalf("no page_size reported PageSize %d, want 100", resp.Pagination.PageSize)
	}
}
