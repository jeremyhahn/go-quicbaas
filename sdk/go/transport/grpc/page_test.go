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
	"math"
	"slices"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// List Paging over gRPC (go-quicbaas#973)
//
// The fake servers below record the paging fields that arrived on the wire
// and answer with an envelope, so each list handler is held to both halves:
// page.Params reach the request, and the response's total, limit, offset and
// next_offset reach page.Envelope. A sort the fake refuses stands in for the
// server's allow-list, proving the refusal surfaces as invalid input.
// =============================================================================

// refusedSort is the sort field the fakes answer InvalidArgument for.
const refusedSort = "no_such_field"

// pagingFake records the paging fields of whichever list RPC it served.
type pagingFake struct {
	got     chan pageFields
	jobReqs chan *pb.OrchestratorListJobsRequest
}

// answer records the fields and computes the envelope a three-record
// listing would answer the page with, or refuses the unknown sort.
func (f *pagingFake) answer(fields pageFields) (total, limit, offset, next int32, err error) {
	f.got <- fields
	if fields.sort == refusedSort {
		return 0, 0, 0, 0, status.Error(codes.InvalidArgument, "page: unknown sort field: "+refusedSort)
	}
	total, limit, offset = 3, fields.limit, fields.offset
	if offset+limit < total {
		next = offset + limit
	}
	return total, limit, offset, next, nil
}

type pagingOrchestratorFake struct {
	pb.UnimplementedOrchestratorServiceServer
	*pagingFake
}

func (f *pagingOrchestratorFake) ListSessions(_ context.Context, req *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	total, limit, offset, next, err := f.answer(pageFields{req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ()})
	if err != nil {
		return nil, err
	}
	return &pb.ListSessionsResponse{Sessions: []*pb.SessionInfo{{SessionId: "s1"}}, Total: total, Limit: limit, Offset: offset, NextOffset: next}, nil
}

func (f *pagingOrchestratorFake) ListTasks(_ context.Context, req *pb.ListTasksRequest) (*pb.ListTasksResponse, error) {
	total, limit, offset, next, err := f.answer(pageFields{req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ()})
	if err != nil {
		return nil, err
	}
	return &pb.ListTasksResponse{Tasks: []*pb.Task{{Id: "t1"}}, TotalCount: total, Total: total, Limit: limit, Offset: offset, NextOffset: next}, nil
}

func (f *pagingOrchestratorFake) ListJobs(_ context.Context, req *pb.OrchestratorListJobsRequest) (*pb.OrchestratorListJobsResponse, error) {
	f.jobReqs <- req
	total, limit, offset, next, err := f.answer(pageFields{req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ()})
	if err != nil {
		return nil, err
	}
	return &pb.OrchestratorListJobsResponse{Jobs: []*pb.OrchestratorJobSummary{{JobId: "j1"}}, Total: total, Limit: limit, Offset: offset, NextOffset: next}, nil
}

func (f *pagingOrchestratorFake) ListFleetNodes(_ context.Context, req *pb.ListFleetNodesRequest) (*pb.ListFleetNodesResponse, error) {
	total, limit, offset, next, err := f.answer(pageFields{req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ()})
	if err != nil {
		return nil, err
	}
	return &pb.ListFleetNodesResponse{Nodes: []*pb.FleetNode{{Id: "a1"}}, Count: 1, Total: total, Limit: limit, Offset: offset, NextOffset: next}, nil
}

type pagingProjectFake struct {
	pb.UnimplementedProjectServiceServer
	*pagingFake
}

func (f *pagingProjectFake) ListProjects(_ context.Context, req *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	total, limit, offset, next, err := f.answer(pageFields{req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ()})
	if err != nil {
		return nil, err
	}
	return &pb.ListProjectsResponse{Projects: []*pb.Project{{Id: "p1"}}, Total: total, Limit: limit, Offset: offset, NextOffset: next}, nil
}

type pagingAccountFake struct {
	pb.UnimplementedAccountServiceServer
	*pagingFake
}

func (f *pagingAccountFake) ListAccounts(_ context.Context, req *pb.ListAccountsRequest) (*pb.ListAccountsResponse, error) {
	total, limit, offset, next, err := f.answer(pageFields{req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ()})
	if err != nil {
		return nil, err
	}
	return &pb.ListAccountsResponse{Accounts: []*pb.Account{{Id: "acct-1"}}, Total: total, Limit: limit, Offset: offset, NextOffset: next}, nil
}

// newPagingConn serves every paged list RPC from one recording fake.
func newPagingConn(t *testing.T) (*grpc.ClientConn, *pagingFake) {
	t.Helper()
	fake := &pagingFake{got: make(chan pageFields, 1), jobReqs: make(chan *pb.OrchestratorListJobsRequest, 1)}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &pagingOrchestratorFake{pagingFake: fake})
		pb.RegisterProjectServiceServer(s, &pagingProjectFake{pagingFake: fake})
		pb.RegisterAccountServiceServer(s, &pagingAccountFake{pagingFake: fake})
	})
	return srv.dial(t), fake
}

// pagedHandler lists one endpoint through its SDK gRPC handler and returns
// the envelope it answered with and how many records it carried.
type pagedHandler func(ctx context.Context, conn *grpc.ClientConn, params page.Params) (page.Envelope, int, error)

func pagedHandlers() map[string]pagedHandler {
	return map[string]pagedHandler{
		"ListSessions": func(ctx context.Context, conn *grpc.ClientConn, params page.Params) (page.Envelope, int, error) {
			out, err := handleOrchestratorListSessions(ctx, conn, &orchestrator.ListSessionsRequest{Params: params})
			if err != nil {
				return page.Envelope{}, 0, err
			}
			resp := out.(*orchestrator.ListSessionsResponse)
			return resp.Envelope, len(resp.Sessions), nil
		},
		"ListTasks": func(ctx context.Context, conn *grpc.ClientConn, params page.Params) (page.Envelope, int, error) {
			out, err := handleOrchestratorListTasks(ctx, conn, &orchestrator.ListTasksRequest{TenantID: "t1", Params: params})
			if err != nil {
				return page.Envelope{}, 0, err
			}
			resp := out.(*orchestrator.ListTasksResponse)
			return resp.Envelope, len(resp.Tasks), nil
		},
		"ListJobs": func(ctx context.Context, conn *grpc.ClientConn, params page.Params) (page.Envelope, int, error) {
			out, err := handleOrchestratorListJobs(ctx, conn, &orchestrator.ListJobsRequest{Params: params})
			if err != nil {
				return page.Envelope{}, 0, err
			}
			resp := out.(*orchestrator.ListJobsResponse)
			return resp.Envelope, len(resp.Jobs), nil
		},
		"ListFleetNodes": func(ctx context.Context, conn *grpc.ClientConn, params page.Params) (page.Envelope, int, error) {
			out, err := handleFleetList(ctx, conn, &orchestrator.ListFleetNodesRequest{TenantID: "t1", Params: params})
			if err != nil {
				return page.Envelope{}, 0, err
			}
			resp := out.(*orchestrator.ListFleetNodesResponse)
			return resp.Envelope, len(resp.Nodes), nil
		},
		"ListProjects": func(ctx context.Context, conn *grpc.ClientConn, params page.Params) (page.Envelope, int, error) {
			out, err := handleProjectList(ctx, conn, &project.ListProjectsRequest{TenantID: "t1", Params: params})
			if err != nil {
				return page.Envelope{}, 0, err
			}
			resp := out.(*project.ListProjectsResponse)
			return resp.Envelope, len(resp.Projects), nil
		},
		"ListAccounts": func(ctx context.Context, conn *grpc.ClientConn, params page.Params) (page.Envelope, int, error) {
			out, err := handleAccountList(ctx, conn, &account.ListAccountsRequest{TenantID: "t1", Params: params})
			if err != nil {
				return page.Envelope{}, 0, err
			}
			resp := out.(*account.ListAccountsResponse)
			return resp.Envelope, len(resp.Accounts), nil
		},
	}
}

func TestGRPCListHandlers_CarryPaging(t *testing.T) {
	ctx := transport.ContextWithTenant(context.Background(), "t1")
	for name, list := range pagedHandlers() {
		t.Run(name, func(t *testing.T) {
			t.Run("params reach the wire and the envelope comes back", func(t *testing.T) {
				conn, fake := newPagingConn(t)
				env, n, err := list(ctx, conn, page.Params{Limit: 2, Offset: 0, Sort: "created_at:desc", Query: "beta"})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				if got, want := <-fake.got, (pageFields{2, 0, "created_at:desc", "beta"}); got != want {
					t.Errorf("outbound paging = %+v, want %+v", got, want)
				}
				want := page.Envelope{Total: 3, Limit: 2, Offset: 0, NextOffset: 2}
				if env.Total != want.Total || env.Limit != want.Limit || env.Offset != want.Offset || env.NextOffset != want.NextOffset {
					t.Errorf("envelope = %+v, want %+v", env, want)
				}
				if !env.HasMore() {
					t.Error("HasMore = false on a first page of three with limit 2")
				}
				if n != 1 {
					t.Errorf("records = %d, want 1", n)
				}
			})

			t.Run("the last page names no next offset", func(t *testing.T) {
				conn, fake := newPagingConn(t)
				env, _, err := list(ctx, conn, page.Params{Limit: 2, Offset: 2})
				if err != nil {
					t.Fatalf("list: %v", err)
				}
				<-fake.got
				if env.Offset != 2 || env.NextOffset != 0 || env.HasMore() {
					t.Errorf("envelope = %+v, want offset 2 and no next page", env)
				}
			})

			t.Run("a negative value is sent as the server default", func(t *testing.T) {
				conn, fake := newPagingConn(t)
				if _, _, err := list(ctx, conn, page.Params{Limit: -5, Offset: -1}); err != nil {
					t.Fatalf("list: %v", err)
				}
				if got := <-fake.got; got.limit != 0 || got.offset != 0 {
					t.Errorf("outbound limit/offset = %d/%d, want 0/0", got.limit, got.offset)
				}
			})

			t.Run("a refused sort surfaces as invalid input", func(t *testing.T) {
				conn, fake := newPagingConn(t)
				_, _, err := list(ctx, conn, page.Params{Sort: refusedSort})
				<-fake.got
				if !errors.IsInvalidInput(err) {
					t.Errorf("err = %v, want invalid input", err)
				}
			})
		})
	}
}

func TestGRPCListJobs_CarriesFilters(t *testing.T) {
	ctx := transport.ContextWithTenant(context.Background(), "t1")
	conn, fake := newPagingConn(t)

	_, _, err := pagedHandlers()["ListJobs"](ctx, conn, page.Params{Limit: 1})
	if err != nil {
		t.Fatalf("baseline list jobs: %v", err)
	}
	<-fake.got
	<-fake.jobReqs

	req := &orchestrator.ListJobsRequest{
		TenantID:  "t1",
		TaskID:    "task-a",
		Runner:    "triage",
		ProjectID: "project-a",
		Statuses:  []string{"completed", "failed"},
		JobIDs:    []string{"job-1", "job-2"},
		AgentID:   "agent-a",
		Params:    page.Params{Limit: 2, Sort: "job_id"},
	}
	resp, err := handleOrchestratorListJobs(ctx, conn, req)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if _, ok := resp.(*orchestrator.ListJobsResponse); !ok {
		t.Fatalf("response = %T, want *orchestrator.ListJobsResponse", resp)
	}
	<-fake.got
	got := <-fake.jobReqs
	if got.GetTaskId() != "task-a" || got.GetRunner() != "triage" || got.GetProjectId() != "project-a" || got.GetAgentId() != "agent-a" {
		t.Fatalf("filters = task:%q runner:%q project:%q agent:%q", got.GetTaskId(), got.GetRunner(), got.GetProjectId(), got.GetAgentId())
	}
	if want := []string{"completed", "failed"}; !slices.Equal(got.GetStatuses(), want) {
		t.Fatalf("statuses = %v, want %v", got.GetStatuses(), want)
	}
	if want := []string{"job-1", "job-2"}; !slices.Equal(got.GetJobIds(), want) {
		t.Fatalf("job_ids = %v, want %v", got.GetJobIds(), want)
	}
}

func TestWireInt32Saturates(t *testing.T) {
	cases := map[int]int32{-1: 0, 0: 0, 25: 25, math.MaxInt32 + 10: math.MaxInt32}
	for in, want := range cases {
		if got := wireInt32(in); got != want {
			t.Errorf("wireInt32(%d) = %d, want %d", in, got, want)
		}
	}
}
