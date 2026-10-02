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

package embedded

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// schedulerMock serves a recording job service and a real placement engine.
type schedulerMock struct {
	*mockQuicBaaS
	jobs      service.SchedulerService
	placement service.PlacementService
}

func (m *schedulerMock) Scheduler() service.SchedulerService { return m.jobs }
func (m *schedulerMock) Placement() service.PlacementService { return m.placement }

func newSchedulerClient(t *testing.T, jobs service.SchedulerService, places service.PlacementService) scheduler.Client {
	t.Helper()
	tr, err := New(&schedulerMock{mockQuicBaaS: &mockQuicBaaS{}, jobs: jobs, placement: places})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return scheduler.NewClient(tr)
}

func TestEmbeddedScheduler_JobsReachTheService(t *testing.T) {
	jobs := schedulertest.New()
	schedulertest.RunJobConformance(t, newSchedulerClient(t, jobs, nil), jobs)
}

func TestEmbeddedScheduler_PlacementReachesTheEngine(t *testing.T) {
	places := service.NewPlacementService(placement.New(placementtest.New()))
	schedulertest.RunPlacementConformance(t, transport.ContextWithTenant(context.Background(), "acme"), newSchedulerClient(t, nil, places), "")
}

// TestEmbeddedScheduler_CarriesTheSDKTenant proves an in-process caller's
// transport.ContextWithTenant scopes placement: a node registered as one
// tenant is invisible to another.
func TestEmbeddedScheduler_CarriesTheSDKTenant(t *testing.T) {
	places := service.NewPlacementService(placement.New(placementtest.New()))
	client := newSchedulerClient(t, nil, places)
	acme := transport.ContextWithTenant(context.Background(), "acme")
	other := transport.ContextWithTenant(context.Background(), "other")

	if _, err := client.RegisterNode(acme, &scheduler.RegisterNodeRequest{Facts: scheduler.NodeFacts{NodeID: "n"}, TTLSeconds: 10}); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	if nodes, err := client.ListNodes(other); err != nil || len(nodes) != 0 {
		t.Fatalf("another tenant saw %d nodes (%v)", len(nodes), err)
	}
	if nodes, err := client.ListNodes(acme); err != nil || len(nodes) != 1 {
		t.Fatalf("the registering tenant saw %d nodes (%v)", len(nodes), err)
	}
}

func TestEmbeddedScheduler_AbsentHalvesAreUnavailable(t *testing.T) {
	client := newSchedulerClient(t, nil, nil)
	if _, err := client.GetStatus(context.Background()); err == nil {
		t.Error("GetStatus succeeded with no job service")
	}
	if _, err := client.ListNodes(context.Background()); err == nil {
		t.Error("ListNodes succeeded with no placement service")
	}
}
