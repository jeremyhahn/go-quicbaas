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
	stderrors "errors"
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

// TestGRPCScheduler_ByIDRequestsCarryTheJobScope proves the scope a by-ID
// request names crosses the wire to the service, and that no scope reaches
// it as the caller's own.
func TestGRPCScheduler_ByIDRequestsCarryTheJobScope(t *testing.T) {
	jobs := schedulertest.New()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterSchedulerServiceServer(s, apigrpc.NewSchedulerServer(jobs, nil, log))
	}, grpc.UnaryInterceptor(bindAuthenticatedTenant))
	conn := srv.dial(t)

	byID := []struct {
		method string
		req    func(scope *scheduler.OwnerType) any
		scope  func(received any) service.SchedulerOwnerType
	}{
		{"GetJob",
			func(s *scheduler.OwnerType) any { return &scheduler.GetJobRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.GetSchedulerJobRequest).Scope }},
		{"UpdateJob",
			func(s *scheduler.OwnerType) any { return &scheduler.UpdateJobRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.UpdateSchedulerJobRequest).Scope }},
		{"DeleteJob",
			func(s *scheduler.OwnerType) any { return &scheduler.DeleteJobRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.DeleteSchedulerJobRequest).Scope }},
		{"TriggerJob",
			func(s *scheduler.OwnerType) any { return &scheduler.TriggerJobRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.TriggerSchedulerJobRequest).Scope }},
		{"PauseJob",
			func(s *scheduler.OwnerType) any { return &scheduler.PauseJobRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.PauseSchedulerJobRequest).Scope }},
		{"ResumeJob",
			func(s *scheduler.OwnerType) any { return &scheduler.ResumeJobRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.ResumeSchedulerJobRequest).Scope }},
		{"CancelJob",
			func(s *scheduler.OwnerType) any { return &scheduler.CancelJobRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.CancelSchedulerJobRequest).Scope }},
		{"GetJobHistory",
			func(s *scheduler.OwnerType) any { return &scheduler.GetJobHistoryRequest{JobID: 7, Scope: s} },
			func(r any) service.SchedulerOwnerType { return r.(*service.GetSchedulerJobHistoryRequest).Scope }},
	}

	delegated := scheduler.OwnerTypeDelegated
	scopes := []struct {
		name  string
		scope *scheduler.OwnerType
		want  service.SchedulerOwnerType
	}{
		{"delegated", &delegated, service.SchedulerOwnerTypeDelegated},
		{"own", nil, ""},
	}

	for _, m := range byID {
		for _, s := range scopes {
			t.Run(m.method+"/"+s.name, func(t *testing.T) {
				if _, err := handleSchedulerRequest(context.Background(), conn, m.method, m.req(s.scope)); err != nil {
					t.Fatalf("%s: %v", m.method, err)
				}
				if got := m.scope(jobs.Received(m.method)); got != s.want {
					t.Errorf("%s: service scope = %q, want %q", m.method, got, s.want)
				}
			})
		}
	}
}

// TestGRPCScheduler_UnknownJobScopeIsRefused proves a scope the wire cannot
// carry is refused, not sent as the caller's own.
func TestGRPCScheduler_UnknownJobScopeIsRefused(t *testing.T) {
	jobs := schedulertest.New()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterSchedulerServiceServer(s, apigrpc.NewSchedulerServer(jobs, nil, log))
	}, grpc.UnaryInterceptor(bindAuthenticatedTenant))

	unknown := scheduler.OwnerType(99)
	_, err = handleSchedulerRequest(context.Background(), srv.dial(t), "GetJob", &scheduler.GetJobRequest{JobID: 7, Scope: &unknown})
	if !stderrors.Is(err, apigrpc.ErrSchedulerJobScopeInvalid) {
		t.Fatalf("GetJob with an unknown scope: err = %v, want ErrSchedulerJobScopeInvalid", err)
	}
	if got := jobs.Received("GetJob"); got != nil {
		t.Errorf("the service received %v, want no request", got)
	}
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
