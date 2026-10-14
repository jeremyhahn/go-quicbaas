// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.
package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestSpendLimitsProtoRoundTrip is the one limits shape on the wire
// (go-quicbaas#1058): every window crosses whole, limits that meter nothing
// are absent, and an update distinguishes absent (unchanged) from empty
// (clear).
func TestSpendLimitsProtoRoundTrip(t *testing.T) {
	limits := orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
		{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 2_000_000},
		{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 500_000_000},
	}}
	wire := spendLimitsToProto(limits)
	if len(wire.GetWindows()) != 2 || wire.GetWindows()[1].GetMaxEquivalentMicros() != 500_000_000 {
		t.Fatalf("wire = %+v", wire)
	}
	back := spendLimitsFromProto(wire)
	for i := range limits.Windows {
		if back.Windows[i] != limits.Windows[i] {
			t.Fatalf("window %d = %+v, want %+v", i, back.Windows[i], limits.Windows[i])
		}
	}
	if spendLimitsToProto(orchestration.SpendLimits{}) != nil {
		t.Fatal("limits that meter nothing must be absent on the wire")
	}
	if optionalSpendLimits(nil) != nil {
		t.Fatal("an absent update must leave the limits unchanged")
	}
	if cleared := optionalSpendLimits(&pb.SpendLimits{}); cleared == nil || cleared.Metered() {
		t.Fatalf("an empty update = %+v, want a present clear", cleared)
	}
}

// TestProjectServer_SpendCrossesTheWire is go-quicbaas#1058 over gRPC: a
// project's spend limits are set on create, replaced on update only when
// present, and rendered on the record.
func TestProjectServer_SpendCrossesTheWire(t *testing.T) {
	limits := orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
		{Name: "weekly", Size: 168 * time.Hour, MaxEquivalentMicros: 250_000_000},
	}}
	var created *service.CreateProjectRequest
	var updates []*service.UpdateProjectRequest
	project := sampleProject()
	project.Spend = limits
	srv := newProjectServer(t, &grpcMockProjectService{
		createFunc: func(_ context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			created = req
			return &service.CreateProjectResponse{Project: project, Timestamp: time.Now()}, nil
		},
		updateFunc: func(_ context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			updates = append(updates, req)
			return &service.UpdateProjectResponse{Project: project, Timestamp: time.Now()}, nil
		},
	})
	ctx := callerContext("tenant-1")

	resp, err := srv.CreateProject(ctx, &pb.CreateProjectRequest{TenantId: "tenant-1", Name: "demo", Spend: spendLimitsToProto(limits)})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if len(created.Spend.Windows) != 1 || created.Spend.Windows[0] != limits.Windows[0] {
		t.Fatalf("service saw %+v, want %+v", created.Spend, limits)
	}
	if got := resp.GetProject().GetSpend().GetWindows(); len(got) != 1 || got[0].GetMaxEquivalentMicros() != 250_000_000 {
		t.Fatalf("record spend = %+v", got)
	}

	for _, req := range []*pb.UpdateProjectRequest{
		{TenantId: "tenant-1", ProjectId: "proj-1", Name: "x"},
		{TenantId: "tenant-1", ProjectId: "proj-1", Spend: &pb.SpendLimits{}},
	} {
		if _, err := srv.UpdateProject(ctx, req); err != nil {
			t.Fatalf("UpdateProject: %v", err)
		}
	}
	if updates[0].Spend != nil {
		t.Fatalf("an update without spend sent %+v", updates[0].Spend)
	}
	if updates[1].Spend == nil || updates[1].Spend.Metered() {
		t.Fatalf("an empty spend update = %+v, want a present clear", updates[1].Spend)
	}
}

// TestTenantSpendPolicyOverGRPC is go-quicbaas#1058: a tenant's own limits are
// set and read back with the usage against each window.
func TestTenantSpendPolicyOverGRPC(t *testing.T) {
	ledger := orchestration.NewInMemoryConsumptionLedger()
	srv := NewOrchestratorServer(&spendLedgerAgentService{
		ledger: ledger, tenant: orchestration.NewInMemoryTenantSpendPolicyStore(),
	}, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	limits := orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
		{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 2_000_000_000},
	}}

	unset, err := srv.GetTenantSpendPolicy(ctx, &pb.GetTenantSpendPolicyRequest{TenantId: "t1"})
	if err != nil || unset.GetLimits() != nil || unset.GetUpdatedAt() != nil {
		t.Fatalf("unset = %+v, %v; want an unmetered tenant", unset, err)
	}
	set, err := srv.SetTenantSpendPolicy(ctx, &pb.SetTenantSpendPolicyRequest{TenantId: "t1", Limits: spendLimitsToProto(limits)})
	if err != nil {
		t.Fatalf("SetTenantSpendPolicy: %v", err)
	}
	if len(set.GetLimits().GetWindows()) != 1 || set.GetUpdatedAt() == nil || len(set.GetWindows()) != 1 {
		t.Fatalf("set = %+v", set)
	}
	got, err := srv.GetTenantSpendPolicy(ctx, &pb.GetTenantSpendPolicyRequest{TenantId: "t1"})
	if err != nil || got.GetLimits().GetWindows()[0].GetMaxEquivalentMicros() != 2_000_000_000 {
		t.Fatalf("got = %+v, %v", got, err)
	}
}

func TestTenantSpendPolicyOverGRPCRefuses(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	system := tenantctx.WithSystem(context.Background())
	wired := NewOrchestratorServer(&spendLedgerAgentService{
		ledger: orchestration.NewInMemoryConsumptionLedger(), tenant: orchestration.NewInMemoryTenantSpendPolicyStore(),
	}, createOrchestratorGRPCTestLogger())
	unwired := NewOrchestratorServer(&spendLedgerAgentService{}, createOrchestratorGRPCTestLogger())
	bare := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	tests := []struct {
		name string
		call func() error
		code codes.Code
	}{
		{"no tenant", func() error { _, err := wired.GetTenantSpendPolicy(ctx, &pb.GetTenantSpendPolicyRequest{}); return err }, codes.InvalidArgument},
		{"a set with no tenant", func() error {
			_, err := wired.SetTenantSpendPolicy(ctx, &pb.SetTenantSpendPolicyRequest{})
			return err
		}, codes.InvalidArgument},
		{"a malformed window", func() error {
			_, err := wired.SetTenantSpendPolicy(ctx, &pb.SetTenantSpendPolicyRequest{TenantId: "t1",
				Limits: &pb.SpendLimits{Windows: []*pb.SpendLimitWindow{{Name: "w", SizeNanos: int64(time.Hour)}}}})
			return err
		}, codes.InvalidArgument},
		{"the system tenant naming another tenant", func() error {
			_, err := wired.SetTenantSpendPolicy(system, &pb.SetTenantSpendPolicyRequest{TenantId: "t1"})
			return err
		}, codes.PermissionDenied},
		{"a service with no store wired", func() error {
			_, err := unwired.GetTenantSpendPolicy(ctx, &pb.GetTenantSpendPolicyRequest{TenantId: "t1"})
			return err
		}, codes.Unavailable},
		{"a service that has no spend policy at all", func() error {
			_, err := bare.SetTenantSpendPolicy(ctx, &pb.SetTenantSpendPolicyRequest{TenantId: "t1"})
			return err
		}, codes.Unavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := status.Code(tc.call()); got != tc.code {
				t.Fatalf("code = %s, want %s", got, tc.code)
			}
		})
	}
}
