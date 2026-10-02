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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// gRPC List Paging (go-quicbaas#973)
//
// Every paged list RPC is driven through the same table: the same three
// records, the same page questions, the same answers. Each service double
// runs the SHIPPED orchestration.ApplyPage with the endpoint's shipped
// PageSpec, so what is tested is the transport carrying the contract, not a
// second copy of it.
// =============================================================================

// pageQuestion is one list request's paging fields.
type pageQuestion struct {
	limit, offset int32
	sort, q       string
}

// pageAnswer is what a list RPC answered: the record IDs in order and the
// envelope.
type pageAnswer struct {
	ids                              []string
	total, limit, offset, nextOffset int32
}

// pagedRPC asks one list RPC one page question.
type pagedRPC func(t *testing.T, q pageQuestion) (pageAnswer, error)

// The three records every endpoint lists. The IDs order them; the names are
// what `q` searches.
var pagedFixture = []struct{ id, name string }{
	{"r1", "alpha"},
	{"r2", "beta"},
	{"r3", "gamma"},
}

func pagedRPCs() map[string]pagedRPC {
	base := time.Unix(1_700_000_000, 0)
	return map[string]pagedRPC{
		"ListJobs": func(t *testing.T, q pageQuestion) (pageAnswer, error) {
			svc := &grpcTestAgentService{}
			for i, r := range pagedFixture {
				svc.listJobs = append(svc.listJobs, &orchestration.JobStatus{
					JobID: r.id, Description: r.name, StartedAt: base.Add(time.Duration(i) * time.Minute).UnixNano(),
				})
			}
			resp, err := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger()).ListJobs(tenantctx.WithTenant(context.Background(), "t1"),
				&pb.OrchestratorListJobsRequest{TenantId: "t1", Limit: q.limit, Offset: q.offset, Sort: q.sort, Q: q.q})
			if err != nil {
				return pageAnswer{}, err
			}
			ids := make([]string, 0, len(resp.GetJobs()))
			for _, j := range resp.GetJobs() {
				ids = append(ids, j.GetJobId())
			}
			return pageAnswer{ids, resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()}, nil
		},
		"ListTasks": func(t *testing.T, q pageQuestion) (pageAnswer, error) {
			var tasks []*orchestration.Task
			for i, r := range pagedFixture {
				tasks = append(tasks, &orchestration.Task{
					ID: r.id, Subject: r.name, CreatedAt: base.Add(time.Duration(i) * time.Minute).UnixNano(),
				})
			}
			svc := &grpcTestAgentService{
				listTasksFunc: func(context.Context, string, *orchestration.TaskFilter) ([]*orchestration.Task, error) {
					return tasks, nil
				},
			}
			resp, err := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger()).ListTasks(tenantctx.WithTenant(context.Background(), "t1"),
				&pb.ListTasksRequest{TenantId: "t1", Limit: q.limit, Offset: q.offset, Sort: q.sort, Q: q.q})
			if err != nil {
				return pageAnswer{}, err
			}
			assert.Equal(t, resp.GetTotal(), resp.GetTotalCount(), "total_count carries the matched total")
			ids := make([]string, 0, len(resp.GetTasks()))
			for _, task := range resp.GetTasks() {
				ids = append(ids, task.GetId())
			}
			return pageAnswer{ids, resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()}, nil
		},
		"ListFleetNodes": func(t *testing.T, q pageQuestion) (pageAnswer, error) {
			svc := &grpcTestAgentService{}
			for i, r := range pagedFixture {
				svc.listAgents = append(svc.listAgents, &orchestration.FleetNode{
					ID: r.id, Name: r.name, RegisteredAt: base.Add(time.Duration(i) * time.Minute).UnixNano(),
				})
			}
			resp, err := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger()).ListFleetNodes(tenantctx.WithTenant(context.Background(), "t1"),
				&pb.ListFleetNodesRequest{TenantId: "t1", Limit: q.limit, Offset: q.offset, Sort: q.sort, Q: q.q})
			if err != nil {
				return pageAnswer{}, err
			}
			assert.Equal(t, int32(len(resp.GetNodes())), resp.GetCount(), "count is the size of this page")
			ids := make([]string, 0, len(resp.GetNodes()))
			for _, a := range resp.GetNodes() {
				ids = append(ids, a.GetId())
			}
			return pageAnswer{ids, resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()}, nil
		},
		"ListSessions": func(t *testing.T, q pageQuestion) (pageAnswer, error) {
			var sessions []*orchestration.SessionInfo
			for i, r := range pagedFixture {
				sessions = append(sessions, &orchestration.SessionInfo{
					SessionID: r.id, Model: r.name, CreatedAt: base.Add(time.Duration(i) * time.Minute).UnixNano(),
				})
			}
			svc := &grpcTestAgentService{
				listSessionsFunc: func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
					require.NotNil(t, req.Page, "a gRPC listing must reach the service with a page")
					page, err := orchestration.ApplyPage(sessions, req.Page, orchestration.SessionPageSpec())
					if err != nil {
						return nil, err
					}
					return &orchestration.ListSessionsResponse{
						Sessions: page.Items, Total: page.Total, Limit: page.Limit,
						Offset: page.Offset, NextOffset: page.NextOffset,
					}, nil
				},
			}
			resp, err := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger()).ListSessions(tenantctx.WithTenant(context.Background(), "t1"),
				&pb.ListSessionsRequest{TenantId: "t1", Limit: q.limit, Offset: q.offset, Sort: q.sort, Q: q.q})
			if err != nil {
				return pageAnswer{}, err
			}
			ids := make([]string, 0, len(resp.GetSessions()))
			for _, s := range resp.GetSessions() {
				ids = append(ids, s.GetSessionId())
			}
			return pageAnswer{ids, resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()}, nil
		},
		"ListProjects": func(t *testing.T, q pageQuestion) (pageAnswer, error) {
			var projects []*orchestration.Project
			for i, r := range pagedFixture {
				projects = append(projects, &orchestration.Project{
					ID: r.id, TenantID: "t1", Name: r.name, CreatedAt: base.Add(time.Duration(i) * time.Minute),
				})
			}
			svc := &grpcMockProjectService{
				listFunc: func(_ context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
					require.NotNil(t, req.Page, "a gRPC listing must reach the service with a page")
					page, err := orchestration.ApplyPage(projects, req.Page, orchestration.ProjectPageSpec())
					if err != nil {
						return nil, err
					}
					return &service.ListProjectsResponse{
						Projects: page.Items, Total: page.Total, Limit: page.Limit,
						Offset: page.Offset, NextOffset: page.NextOffset,
					}, nil
				},
			}
			resp, err := newProjectServer(t, svc).ListProjects(tenantctx.WithTenant(context.Background(), "t1"),
				&pb.ListProjectsRequest{TenantId: "t1", Limit: q.limit, Offset: q.offset, Sort: q.sort, Q: q.q})
			if err != nil {
				return pageAnswer{}, err
			}
			ids := make([]string, 0, len(resp.GetProjects()))
			for _, p := range resp.GetProjects() {
				ids = append(ids, p.GetId())
			}
			return pageAnswer{ids, resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()}, nil
		},
		"ListAccounts": func(t *testing.T, q pageQuestion) (pageAnswer, error) {
			var accounts []*orchestration.Account
			for i, r := range pagedFixture {
				accounts = append(accounts, &orchestration.Account{
					ID: r.id, TenantID: "t1", DisplayName: r.name, CreatedAt: base.Add(time.Duration(i) * time.Minute),
				})
			}
			svc := &mocks.MockAccountService{
				ListAccountsFunc: func(_ context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
					require.NotNil(t, req.Page, "a gRPC listing must reach the service with a page")
					page, err := orchestration.ApplyPage(accounts, req.Page, orchestration.AccountPageSpec())
					if err != nil {
						return nil, err
					}
					return &service.ListAccountsResponse{
						Accounts: page.Items, Total: page.Total, Limit: page.Limit,
						Offset: page.Offset, NextOffset: page.NextOffset, Timestamp: time.Now().UTC(),
					}, nil
				},
			}
			resp, err := newAccountServer(svc).ListAccounts(tenantctx.WithTenant(context.Background(), "t1"),
				&pb.ListAccountsRequest{TenantId: "t1", Limit: q.limit, Offset: q.offset, Sort: q.sort, Q: q.q})
			if err != nil {
				return pageAnswer{}, err
			}
			ids := make([]string, 0, len(resp.GetAccounts()))
			for _, a := range resp.GetAccounts() {
				ids = append(ids, a.GetId())
			}
			return pageAnswer{ids, resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()}, nil
		},
	}
}

// The sort field every fixture shares an allow-listed name for.
func idSortField(rpc string) string {
	switch rpc {
	case "ListJobs":
		return "job_id"
	case "ListSessions":
		return "session_id"
	default:
		return "id"
	}
}

func TestListRPCs_CarryPaging(t *testing.T) {
	for name, call := range pagedRPCs() {
		t.Run(name, func(t *testing.T) {
			idSort := idSortField(name)

			t.Run("limit cuts the first page and names the next", func(t *testing.T) {
				got, err := call(t, pageQuestion{limit: 2, sort: idSort})
				require.NoError(t, err)
				assert.Equal(t, pageAnswer{[]string{"r1", "r2"}, 3, 2, 0, 2}, got)
			})

			t.Run("offset reaches the last page, which names no next", func(t *testing.T) {
				got, err := call(t, pageQuestion{limit: 2, offset: 2, sort: idSort})
				require.NoError(t, err)
				assert.Equal(t, pageAnswer{[]string{"r3"}, 3, 2, 2, 0}, got)
			})

			t.Run("an allow-listed sort orders descending", func(t *testing.T) {
				got, err := call(t, pageQuestion{sort: idSort + ":desc"})
				require.NoError(t, err)
				assert.Equal(t, []string{"r3", "r2", "r1"}, got.ids)
			})

			t.Run("q narrows and total counts the narrowed set", func(t *testing.T) {
				got, err := call(t, pageQuestion{q: "BETA"})
				require.NoError(t, err)
				assert.Equal(t, []string{"r2"}, got.ids)
				assert.Equal(t, int32(1), got.total)
			})

			t.Run("no paging fields takes the default page", func(t *testing.T) {
				got, err := call(t, pageQuestion{})
				require.NoError(t, err)
				assert.Equal(t, int32(orchestration.DefaultPageLimit), got.limit)
				assert.Equal(t, int32(3), got.total)
				assert.Len(t, got.ids, 3)
			})

			t.Run("a limit above the cap is capped, not refused", func(t *testing.T) {
				got, err := call(t, pageQuestion{limit: 10_000})
				require.NoError(t, err)
				assert.Equal(t, int32(orchestration.MaxPageLimit), got.limit)
			})

			t.Run("an unknown sort field is InvalidArgument naming it", func(t *testing.T) {
				_, err := call(t, pageQuestion{sort: "no_such_field"})
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
				assert.Contains(t, status.Convert(err).Message(), "no_such_field")
			})

			t.Run("a negative limit is InvalidArgument", func(t *testing.T) {
				_, err := call(t, pageQuestion{limit: -1})
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
				assert.Contains(t, status.Convert(err).Message(), "limit")
			})

			t.Run("a negative offset is InvalidArgument", func(t *testing.T) {
				_, err := call(t, pageQuestion{offset: -1})
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err))
				assert.Contains(t, status.Convert(err).Message(), "offset")
			})
		})
	}
}

func TestPageInt32Saturates(t *testing.T) {
	assert.Equal(t, int32(0), pageInt32(-5))
	assert.Equal(t, int32(42), pageInt32(42))
	assert.Equal(t, int32(2147483647), pageInt32(1<<40))
}
