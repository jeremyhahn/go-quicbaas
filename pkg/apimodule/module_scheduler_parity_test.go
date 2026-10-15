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
	"testing"

	grpclib "google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// schedulerParityTransports serves jobs through the real server stack of
// every production transport.
func schedulerParityTransports(t *testing.T, jobs *schedulertest.Jobs) map[string]transport.Transport {
	t.Helper()

	facade := mocks.NewMockQuicBaaS()
	facade.SchedulerServiceFunc = func() service.SchedulerService { return jobs }
	return parityTransports(t, facade, func(srv grpclib.ServiceRegistrar) {
		pb.RegisterSchedulerServiceServer(srv, grpcapi.NewSchedulerServer(jobs, nil, testLogger(t)))
	})
}

// TestSchedulerTransportParity_Jobs drives the whole SDK scheduler job surface
// over every transport.
func TestSchedulerTransportParity_Jobs(t *testing.T) {
	jobs := schedulertest.New()
	for name, tr := range schedulerParityTransports(t, jobs) {
		t.Run(name, func(t *testing.T) {
			schedulertest.RunJobConformance(t, scheduler.NewClient(tr), jobs)
		})
	}
}
