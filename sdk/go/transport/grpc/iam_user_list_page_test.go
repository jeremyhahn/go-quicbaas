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

// pagingIAMServer serves a 150-user tenant: it records the offset each
// ListUsers call asks for and returns up to Limit users from there.
type pagingIAMServer struct {
	iampb.UnimplementedIAMServiceServer
	gotOffset int32
}

const pagingTotal = 150

func (p *pagingIAMServer) ListUsers(_ context.Context, req *iampb.ListUsersRequest) (*iampb.ListUsersResponse, error) {
	p.gotOffset = req.Offset
	n := int(req.Limit)
	if rest := pagingTotal - int(req.Offset); n > rest {
		n = rest
	}
	users := make([]*iampb.User, 0, n)
	for i := 0; i < n; i++ {
		users = append(users, sampleUser())
	}
	return &iampb.ListUsersResponse{Users: users, Total: pagingTotal}, nil
}

func newPagingIAMHandler(t *testing.T) (*iamHandler, *pagingIAMServer) {
	t.Helper()
	fake := &pagingIAMServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, fake)
	})
	return newIAMHandler(srv.dial(t)), fake
}

func listUsersAt(t *testing.T, h *iamHandler, token string) (*iam.ListUsersResponse, error) {
	t.Helper()
	out, err := h.handleRequest(context.Background(), "quicbaas.IAM.User", "List", map[string]interface{}{
		"tenant_id": "t1",
		"request":   map[string]interface{}{"page_size": 100, "page_token": token},
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
	h, fake := newPagingIAMHandler(t)

	first, err := listUsersAt(t, h, "")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if fake.gotOffset != 0 {
		t.Fatalf("first page sent Offset %d, want 0", fake.gotOffset)
	}
	if len(first.Users) != 100 || !first.Pagination.HasMore || first.Pagination.NextPageToken != "100" {
		t.Fatalf("first page: %d users, HasMore %v, token %q; want 100, true, \"100\"",
			len(first.Users), first.Pagination.HasMore, first.Pagination.NextPageToken)
	}
	if first.Pagination.PageSize != 100 || first.Pagination.TotalCount != pagingTotal {
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
	h, _ := newPagingIAMHandler(t)
	for _, token := range []string{"x", "-1"} {
		if _, err := listUsersAt(t, h, token); err == nil {
			t.Fatalf("page token %q must be rejected", token)
		}
	}
}
