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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Facets are the same on every transport
//
// The orchestration layer counts facets for projects, accounts, tasks, jobs,
// fleet nodes, sessions and workflows. This lists each collection through the
// SDK over REST, gRPC, unix, MCP and embedded against ONE set of services and
// asserts every transport hands the caller the counts the service produced,
// and that a list the service counted nothing for arrives with nil facets.
//
// QUIC is the REST transport over HTTP/3 (sdk/go/transport/quic embeds the
// REST transport and the server mounts the same router), so the REST row
// covers its wire format.
// =============================================================================

// listFacetsFacade is the mock facade with an orchestration service, which
// the REST, MCP and embedded transports reach through Agent().
type listFacetsFacade struct {
	*mocks.MockQuicBaaS
	agents orchestration.AgentService
}

func (f *listFacetsFacade) Agent() orchestration.AgentService { return f.agents }

// parityFacetedLists are the lists facetedLists does not already name: the
// project list.
var parityFacetedLists = map[string]facetedList{
	"projects": func(ctx context.Context, tr transport.Transport) (map[string]map[string]int, error) {
		resp, err := project.NewClient(tr).List(ctx, &project.ListProjectsRequest{TenantID: parityProjectTenant})
		if err != nil {
			return nil, err
		}
		return resp.Facets, nil
	},
}

// listFacetsParityTransports serves one set of list services over every
// transport, each answering with facets.
func listFacetsParityTransports(t *testing.T, facets orchestration.Facets) map[string]transport.Transport {
	t.Helper()

	agents := &listFacetsAgents{facets: facets}
	accounts := &mocks.MockAccountService{
		ListAccountsFunc: func(context.Context, *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			return &service.ListAccountsResponse{Accounts: []*orchestration.Account{}, Facets: facets, Timestamp: time.Now().UTC()}, nil
		},
	}
	projects := &mocks.MockProjectService{
		ListProjectsFunc: func(context.Context, *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			return &service.ListProjectsResponse{Projects: []*orchestration.Project{}, Facets: facets}, nil
		},
	}

	facade := &listFacetsFacade{MockQuicBaaS: mocks.NewMockQuicBaaS(), agents: agents}
	facade.AccountServiceFunc = func() service.AccountService { return accounts }
	facade.ProjectServiceFunc = func() service.ProjectService { return projects }
	log := testLogger(t)
	return parityTransports(t, facade, func(srv grpclib.ServiceRegistrar) {
		pb.RegisterOrchestratorServiceServer(srv, grpcapi.NewOrchestratorServer(agents, log))
		pb.RegisterAccountServiceServer(srv, grpcapi.NewAccountServer(accounts, log))
		pb.RegisterProjectServiceServer(srv, grpcapi.NewProjectServer(projects, log))
	})
}

// forEachFacetedList runs check for every faceted list on every transport
// that carries its facets.
func forEachFacetedList(t *testing.T, transports map[string]transport.Transport, check func(t *testing.T, got map[string]map[string]int)) {
	t.Helper()

	lists := map[string]facetedList{}
	for name, list := range facetedLists {
		lists[name] = list
	}
	for name, list := range parityFacetedLists {
		lists[name] = list
	}
	for name, list := range lists {
		for transportName, tr := range transports {
			t.Run(name+"/"+transportName, func(t *testing.T) {
				// The session list names no tenant in its request; an SDK
				// caller binds one on the context.
				ctx := transport.ContextWithTenant(context.Background(), parityProjectTenant)
				got, err := list(ctx, tr)
				require.NoError(t, err)
				check(t, got)
			})
		}
	}
}

// TestListFacetsTransportParity_EveryTransportCarriesTheCounts proves the
// facets a service counted reach the SDK caller unchanged on every transport.
func TestListFacetsTransportParity_EveryTransportCarriesTheCounts(t *testing.T) {
	facets := orchestration.Facets{
		"status": {"running": 2, "completed": 5},
		"kind":   {"agent": 1},
	}
	transports := listFacetsParityTransports(t, facets)

	forEachFacetedList(t, transports, func(t *testing.T, got map[string]map[string]int) {
		assert.Equal(t, map[string]map[string]int(facets), got)
	})
}

// TestListFacetsTransportParity_NoneCountedStaysNil proves a list the service
// counted no facets for reaches the SDK caller with nil facets on every
// transport, so a JSON rendering omits the field everywhere.
func TestListFacetsTransportParity_NoneCountedStaysNil(t *testing.T) {
	transports := listFacetsParityTransports(t, nil)

	forEachFacetedList(t, transports, func(t *testing.T, got map[string]map[string]int) {
		assert.Nil(t, got)
	})
}
