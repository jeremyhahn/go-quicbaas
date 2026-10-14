//go:build rest && mcp

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

package apimodule

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	sdkgrpc "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
)

// listFacetsAgents answers the orchestrator list verbs the gRPC server pages,
// each with one empty page carrying facets. Any other verb panics on the nil
// embedded service, so a handler reaching past its list verb fails loudly.
type listFacetsAgents struct {
	orchestration.AgentService
	facets orchestration.Facets
}

func (a *listFacetsAgents) ListTasksPage(context.Context, string, *orchestration.TaskFilter, *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	return &orchestration.PageResult[*orchestration.Task]{Items: []*orchestration.Task{}, Facets: a.facets}, nil
}

func (a *listFacetsAgents) ListJobsPage(context.Context, string, *orchestration.JobFilter, *orchestration.Page) (*orchestration.PageResult[*orchestration.JobStatus], error) {
	return &orchestration.PageResult[*orchestration.JobStatus]{Items: []*orchestration.JobStatus{}, Facets: a.facets}, nil
}

func (a *listFacetsAgents) ListWorkflowsPage(context.Context, *orchestration.WorkflowFilter, *orchestration.Page) (*orchestration.PageResult[*orchestration.WorkflowDefinition], error) {
	return &orchestration.PageResult[*orchestration.WorkflowDefinition]{Items: []*orchestration.WorkflowDefinition{}, Facets: a.facets}, nil
}

func (a *listFacetsAgents) ListFleetNodesPage(context.Context, string, *orchestration.FleetNodeFilter, *orchestration.Page) (*orchestration.PageResult[*orchestration.FleetNode], error) {
	return &orchestration.PageResult[*orchestration.FleetNode]{Items: []*orchestration.FleetNode{}, Facets: a.facets}, nil
}

func (a *listFacetsAgents) ListSessions(context.Context, *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
	return &orchestration.ListSessionsResponse{Sessions: []*orchestration.SessionInfo{}, Facets: a.facets}, nil
}

// listFacetsGRPCTransport serves the real Orchestrator and Account gRPC
// servers over facets and returns an SDK gRPC transport dialled to them.
func listFacetsGRPCTransport(t *testing.T, facets orchestration.Facets) transport.Transport {
	t.Helper()

	agents := &listFacetsAgents{facets: facets}
	accounts := &mocks.MockAccountService{
		ListAccountsFunc: func(context.Context, *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{}, Facets: facets, Timestamp: time.Now().UTC()}, nil
		},
	}
	log := testLogger(t)
	serverTLS, clientTLS := parityTLS(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serveParityGRPC(t, func(srv grpclib.ServiceRegistrar) {
		pb.RegisterOrchestratorServiceServer(srv, grpcapi.NewOrchestratorServer(agents, log))
		pb.RegisterAccountServiceServer(srv, grpcapi.NewAccountServer(accounts, log))
	}, serverTLS, lis)

	tr, err := sdkgrpc.New(&transport.Config{
		Address: lis.Addr().String(), Timeout: 5 * time.Second, TLSConfig: clientTLS,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tr.Close()) })
	return tr
}

// facetedList lists one collection through the SDK and returns the facets
// the SDK caller sees.
type facetedList func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error)

// facetedLists names every gRPC list response that carries facets.
var facetedLists = map[string]facetedList{
	"tasks": func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error) {
		resp, err := orchestrator.NewClient(tr).ListTasks(ctx, &orchestrator.ListTasksRequest{TenantID: parityProjectTenant})
		if err != nil {
			return nil, err
		}
		return resp.Facets, nil
	},
	"jobs": func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error) {
		resp, err := orchestrator.NewClient(tr).ListJobs(ctx, &orchestrator.ListJobsRequest{TenantID: parityProjectTenant})
		if err != nil {
			return nil, err
		}
		return resp.Facets, nil
	},
	"workflows": func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error) {
		resp, err := orchestrator.NewClient(tr).ListWorkflows(ctx, &orchestrator.ListWorkflowsRequest{TenantID: parityProjectTenant})
		if err != nil {
			return nil, err
		}
		return resp.Facets, nil
	},
	"fleet nodes": func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error) {
		resp, err := orchestrator.NewClient(tr).ListFleetNodes(ctx, &orchestrator.ListFleetNodesRequest{TenantID: parityProjectTenant})
		if err != nil {
			return nil, err
		}
		return resp.Facets, nil
	},
	"sessions": func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error) {
		resp, err := orchestrator.NewClient(tr).ListSessions(ctx, &orchestrator.ListSessionsRequest{})
		if err != nil {
			return nil, err
		}
		return resp.Facets, nil
	},
	"accounts": func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error) {
		resp, err := account.NewClient(tr).List(ctx, &account.ListAccountsRequest{TenantID: parityProjectTenant})
		if err != nil {
			return nil, err
		}
		return resp.Facets, nil
	},
}

// TestGRPCListFacets_SurviveServerToSDK proves the facets a service counted
// reach an SDK caller over gRPC unchanged, as they do over REST.
func TestGRPCListFacets_SurviveServerToSDK(t *testing.T) {
	facets := orchestration.Facets{
		"status": {"running": 2, "completed": 5},
		"kind":   {"agent": 1},
	}
	tr := listFacetsGRPCTransport(t, facets)

	for name, list := range facetedLists {
		t.Run(name, func(t *testing.T) {
			got, err := list(context.Background(), tr)
			require.NoError(t, err)
			assert.Equal(t, map[string]map[string]int(facets), got)
		})
	}
}

// TestGRPCListFacets_NoneCountedStaysNil proves a list the service counted no
// facets for reaches the SDK with nil facets, not an empty map, so a JSON
// rendering omits the field as REST does.
func TestGRPCListFacets_NoneCountedStaysNil(t *testing.T) {
	cases := map[string]orchestration.Facets{
		"nil facets":   nil,
		"empty facets": {},
	}
	for caseName, facets := range cases {
		tr := listFacetsGRPCTransport(t, facets)
		for name, list := range facetedLists {
			t.Run(caseName+"/"+name, func(t *testing.T) {
				got, err := list(context.Background(), tr)
				require.NoError(t, err)
				assert.Nil(t, got)
			})
		}
	}
}
