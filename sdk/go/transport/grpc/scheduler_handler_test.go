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

package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newSchedulerTransport serves the real pkg/api/grpc SchedulerServer over an
// in-memory connection, so a scheduler request crosses both conversion
// directions: SDK -> proto in this package, proto -> service on the server.
func newSchedulerTransport(t *testing.T, jobs service.SchedulerService, places service.PlacementService) scheduler.Client {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterSchedulerServiceServer(s, apigrpc.NewSchedulerServer(jobs, places, log))
	}, grpc.UnaryInterceptor(bindAuthenticatedTenant))
	return scheduler.NewClient(&Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)})
}

// bindAuthenticatedTenant stands in for the platform-token interceptor, which
// binds the authenticated caller's tenant (pkg/api/grpc authz_interceptor);
// the scheduler refuses a request with none.
func bindAuthenticatedTenant(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	return handler(tenantctx.WithTenant(ctx, "acme"), req)
}

func TestGRPCScheduler_JobsReachTheService(t *testing.T) {
	jobs := schedulertest.New()
	schedulertest.RunJobConformance(t, newSchedulerTransport(t, jobs, nil), jobs)
}

func TestGRPCScheduler_PlacementReachesTheEngine(t *testing.T) {
	places := service.NewPlacementService(placement.New(placementtest.New()))
	schedulertest.RunPlacementConformance(t, context.Background(), newSchedulerTransport(t, nil, places), "")
}

func TestGRPCScheduler_RoutesOnlyTheSchedulerService(t *testing.T) {
	if !isSchedulerService(scheduler.ServiceName) || isSchedulerService("quicbaas.Messaging") {
		t.Fatal("isSchedulerService misroutes")
	}
}
