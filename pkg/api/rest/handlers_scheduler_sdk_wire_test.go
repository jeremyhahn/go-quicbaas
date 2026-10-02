//go:build rest

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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	resttransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// The SDK's REST transport (and QUIC, which is the same code over HTTP/3)
// driven against this package's real router: the JSON the SDK writes is
// the JSON these handlers read, and the reverse.

// schedulerWireService serves a recording job service and a real placement
// engine through the router.
type schedulerWireService struct {
	*testutil.MockQuicBaaS
	jobs      service.SchedulerService
	placement service.PlacementService
}

func (m *schedulerWireService) Scheduler() service.SchedulerService { return m.jobs }
func (m *schedulerWireService) Placement() service.PlacementService { return m.placement }

func newSchedulerWireClient(t *testing.T, jobs service.SchedulerService, places service.PlacementService) scheduler.Client {
	t.Helper()
	server, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	server.service = &schedulerWireService{MockQuicBaaS: testutil.NewMockQuicBaaS(), jobs: jobs, placement: places}

	// Bind the tenant go-qrdb's AuthMiddleware binds for an authenticated
	// caller; the scheduler refuses a request with none.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.router.ServeHTTP(w, r.WithContext(tenantctx.WithTenant(r.Context(), "acme")))
	}))
	t.Cleanup(srv.Close)
	tr, err := resttransport.New(&sdktransport.Config{Address: srv.URL})
	require.NoError(t, err)
	return scheduler.NewClient(tr)
}

func TestSDKRESTScheduler_JobsReachTheService(t *testing.T) {
	jobs := schedulertest.New()
	schedulertest.RunJobConformance(t, newSchedulerWireClient(t, jobs, nil), jobs)
}

func TestSDKRESTScheduler_PlacementReachesTheEngine(t *testing.T) {
	places := service.NewPlacementService(placement.New(placementtest.New()))
	schedulertest.RunPlacementConformance(t, context.Background(), newSchedulerWireClient(t, nil, places), "")
}
