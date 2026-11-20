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
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

type fleetPlacementProvider struct{ svc service.FleetPlacementService }

func (p fleetPlacementProvider) FleetPlacement() service.FleetPlacementService { return p.svc }

// newFleetPlacementGRPCServer serves fleet placement over hosts h1 and h2
// with one live agent session on h2.
func newFleetPlacementGRPCServer(t *testing.T) *OrchestratorServer {
	t.Helper()
	ctx := context.Background()
	registry := orchestration.NewInMemoryFleetRegistry()
	for _, node := range []*orchestration.FleetNode{
		{ID: "n1", TenantID: "acme", Name: "n1", Host: "h1", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning},
		{ID: "n2", TenantID: "acme", Name: "n2", Host: "h2", NodeType: "worker", Status: orchestration.FleetNodeStatusRunning},
	} {
		if err := registry.RegisterFleetNode(ctx, node); err != nil {
			t.Fatalf("RegisterFleetNode: %v", err)
		}
	}
	sessions := orchestration.NewInMemoryAgentStore()
	if err := sessions.PutSession(ctx, &orchestration.SessionRecord{
		SessionID: "s1", TenantID: "acme", Backend: "bot", HostAffinity: "h2", Status: orchestration.SessionStateActive,
	}); err != nil {
		t.Fatalf("PutSession: %v", err)
	}
	specs, err := orchestration.NewPlacementStore(orchestration.NewMemoryTaskKV())
	if err != nil {
		t.Fatalf("NewPlacementStore: %v", err)
	}
	inventory, err := orchestration.NewFleetInventory(registry)
	if err != nil {
		t.Fatalf("NewFleetInventory: %v", err)
	}
	work, err := orchestration.NewSessionWorkSource(sessions, func(*orchestration.SessionRecord) string { return "agent" })
	if err != nil {
		t.Fatalf("NewSessionWorkSource: %v", err)
	}
	drift, err := orchestration.NewDriftReporter(specs, inventory, work)
	if err != nil {
		t.Fatalf("NewDriftReporter: %v", err)
	}
	return NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger()).
		WithFleetPlacement(fleetPlacementProvider{service.NewFleetPlacementService(specs, drift)})
}

func TestFleetPlacementOverGRPC(t *testing.T) {
	srv := newFleetPlacementGRPCServer(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")

	stored, err := srv.DeclareFleetPlacementSpec(acme, &pb.DeclareFleetPlacementSpecRequest{
		Spec: &pb.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}},
	})
	if err != nil {
		t.Fatalf("DeclareFleetPlacementSpec: %v", err)
	}
	if stored.GetTenantId() != "acme" || stored.GetGeneration() != 1 {
		t.Fatalf("stored = %v, want acme at generation 1", stored)
	}
	got, err := srv.GetFleetPlacementSpec(acme, &pb.FleetPlacementSpecRequest{Name: "agents"})
	if err != nil || got.GetRole() != "agent" {
		t.Fatalf("GetFleetPlacementSpec = %v, %v", got, err)
	}
	list, err := srv.ListFleetPlacementSpecs(acme, &pb.ListFleetPlacementSpecsRequest{})
	if err != nil || list.GetCount() != 1 || list.GetSpecs()[0].GetName() != "agents" {
		t.Fatalf("ListFleetPlacementSpecs = %v, %v", list, err)
	}
	report, err := srv.GetFleetPlacementDrift(acme, &pb.FleetPlacementSpecRequest{Name: "agents"})
	if err != nil {
		t.Fatalf("GetFleetPlacementDrift: %v", err)
	}
	if report.GetObservedWork() != 1 || len(report.GetFindings()) != 1 ||
		report.GetFindings()[0].GetHost() != "h2" ||
		report.GetFindings()[0].GetKind() != string(orchestration.DriftHostNotAdmitted) {
		t.Fatalf("report = %v, want s1 on h2 not admitted", report)
	}
	withdrawn, err := srv.WithdrawFleetPlacementSpec(acme, &pb.FleetPlacementSpecRequest{Name: "agents"})
	if err != nil || !withdrawn.GetWithdrawn() {
		t.Fatalf("WithdrawFleetPlacementSpec = %v, %v", withdrawn, err)
	}
}

func TestFleetPlacementOverGRPCRefuses(t *testing.T) {
	srv := newFleetPlacementGRPCServer(t)
	acme := tenantctx.WithTenant(context.Background(), "acme")
	if _, err := srv.DeclareFleetPlacementSpec(acme, &pb.DeclareFleetPlacementSpecRequest{
		Spec: &pb.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}},
	}); err != nil {
		t.Fatalf("DeclareFleetPlacementSpec: %v", err)
	}
	bare := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())

	cases := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"declare with no spec", func() error {
			_, err := srv.DeclareFleetPlacementSpec(acme, &pb.DeclareFleetPlacementSpecRequest{})
			return err
		}, codes.InvalidArgument},
		{"create an existing spec", func() error {
			_, err := srv.DeclareFleetPlacementSpec(acme, &pb.DeclareFleetPlacementSpecRequest{
				Spec: &pb.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h2"}},
			})
			return err
		}, codes.AlreadyExists},
		{"declare a stale generation", func() error {
			_, err := srv.DeclareFleetPlacementSpec(acme, &pb.DeclareFleetPlacementSpecRequest{
				Spec: &pb.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h2"}, Generation: 5},
			})
			return err
		}, codes.AlreadyExists},
		{"get an absent spec", func() error {
			_, err := srv.GetFleetPlacementSpec(acme, &pb.FleetPlacementSpecRequest{Name: "absent"})
			return err
		}, codes.NotFound},
		{"list another tenant", func() error {
			_, err := srv.ListFleetPlacementSpecs(acme, &pb.ListFleetPlacementSpecsRequest{TenantId: "other"})
			return err
		}, codes.PermissionDenied},
		{"no service", func() error {
			_, err := bare.ListFleetPlacementSpecs(acme, &pb.ListFleetPlacementSpecsRequest{})
			return err
		}, codes.Unavailable},
	}
	for _, tc := range cases {
		if got := status.Code(tc.call()); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Every field of a spec survives the trip to proto and back, so a field
// added to the server type and not the message fails here.
func TestFleetPlacementSpecProtoRoundTrip(t *testing.T) {
	spec := &orchestration.PlacementSpec{
		TenantID: "acme", Name: "n", Role: "r", HostCount: 2, Hosts: []string{"h"},
		Profile: &orchestration.FleetJobProfile{CPUCores: 1.5, MemoryBytes: 3, Measured: true, SampleSize: 4,
			UpdatedAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)},
		RequiredCapabilities: []string{"gpu"}, Actuate: true, Generation: 7,
	}
	if back := fleetPlacementSpecFromProto(fleetPlacementSpecToProto(spec)); !reflect.DeepEqual(spec, back) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", back, spec)
	}
	if n := reflect.TypeOf(orchestration.PlacementSpec{}).NumField(); n != 9 {
		t.Fatalf("PlacementSpec has %d fields; map the new one in server_fleet_placement.go and this count", n)
	}
	if n := reflect.TypeOf(orchestration.DriftReport{}).NumField(); n != 8 {
		t.Fatalf("DriftReport has %d fields; map the new one in server_fleet_placement.go and this count", n)
	}
}

// newFleetPlacementConsentGRPCServer returns a server whose fleet placement
// service has a consent ledger bound, and the ledger.
func newFleetPlacementConsentGRPCServer(t *testing.T) (*OrchestratorServer, orchestration.ConsentLedger) {
	t.Helper()
	srv := newFleetPlacementGRPCServer(t)
	ledger, err := orchestration.NewConsentLedger(orchestration.NewMemoryTaskKV())
	if err != nil {
		t.Fatalf("NewConsentLedger: %v", err)
	}
	svc, err := srv.fleetPlacementSvc()
	if err != nil {
		t.Fatalf("fleetPlacementSvc: %v", err)
	}
	svc.(service.FleetPlacementConsentBinder).BindFleetPlacementConsent(ledger)
	return srv, ledger
}

func raiseFleetPlacementConsentGRPC(t *testing.T, ledger orchestration.ConsentLedger, workID string) *orchestration.PlacementConsentRequest {
	t.Helper()
	req, err := ledger.Raise(orchestration.ContextWithTenant(context.Background(), "acme"), "acme", orchestration.ConsentMove{
		SpecName: "agents", SpecGeneration: 1, Class: "agent", WorkID: workID, SourceHost: "h1",
	})
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	return req
}

func fleetPlacementConsentGRPCSubject(id string) context.Context {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	if id == "" {
		return ctx
	}
	return auth.SubjectIntoContext(ctx, interfaces.Subject{ID: id, TenantID: "acme", Authenticated: true})
}

func TestFleetPlacementConsentOverGRPC(t *testing.T) {
	srv, ledger := newFleetPlacementConsentGRPCServer(t)
	first := raiseFleetPlacementConsentGRPC(t, ledger, "s1")
	raiseFleetPlacementConsentGRPC(t, ledger, "s2")
	alice := fleetPlacementConsentGRPCSubject("alice")

	listed, err := srv.ListFleetPlacementConsents(alice, &pb.ListFleetPlacementConsentsRequest{State: "pending"})
	if err != nil {
		t.Fatalf("ListFleetPlacementConsents: %v", err)
	}
	if listed.GetCount() != 2 || len(listed.GetRequests()) != 2 {
		t.Fatalf("listed = %v, want two pending requests", listed)
	}
	got := listed.GetRequests()[0]
	if got.GetMove().GetSpecName() != "agents" || got.GetState() != "pending" || got.GetRaisedAt() == nil || got.GetAnsweredAt() != nil {
		t.Fatalf("listed request = %v, want a pending move of agents", got)
	}

	answered, err := srv.AnswerFleetPlacementConsent(alice, &pb.AnswerFleetPlacementConsentRequest{
		TenantId: "acme", Id: first.ID, State: "granted", Reason: "window",
	})
	if err != nil {
		t.Fatalf("AnswerFleetPlacementConsent: %v", err)
	}
	if answered.GetState() != "granted" || answered.GetBy() != "alice" || answered.GetReason() != "window" || answered.GetAnsweredAt() == nil {
		t.Fatalf("answered = %v, want granted by alice", answered)
	}
	granted, err := srv.ListFleetPlacementConsents(alice, &pb.ListFleetPlacementConsentsRequest{TenantId: "acme", State: "granted"})
	if err != nil || granted.GetCount() != 1 || granted.GetRequests()[0].GetId() != first.ID {
		t.Fatalf("granted = %v, %v; want only %s", granted, err, first.ID)
	}
}

func TestFleetPlacementConsentOverGRPCRefuses(t *testing.T) {
	srv, ledger := newFleetPlacementConsentGRPCServer(t)
	req := raiseFleetPlacementConsentGRPC(t, ledger, "s1")
	alice := fleetPlacementConsentGRPCSubject("alice")
	bare := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
	answer := func(ctx context.Context, s *OrchestratorServer, tenant, id, state string) error {
		_, err := s.AnswerFleetPlacementConsent(ctx, &pb.AnswerFleetPlacementConsentRequest{TenantId: tenant, Id: id, State: state})
		return err
	}

	cases := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"answer with no subject", func() error { return answer(fleetPlacementConsentGRPCSubject(""), srv, "", req.ID, "granted") }, codes.PermissionDenied},
		{"answer in another tenant", func() error { return answer(alice, srv, "other", req.ID, "granted") }, codes.PermissionDenied},
		{"list another tenant", func() error {
			_, err := srv.ListFleetPlacementConsents(alice, &pb.ListFleetPlacementConsentsRequest{TenantId: "other"})
			return err
		}, codes.PermissionDenied},
		{"answer unknown", func() error { return answer(alice, srv, "", "unknown", "granted") }, codes.NotFound},
		{"answer maybe", func() error { return answer(alice, srv, "", req.ID, "maybe") }, codes.InvalidArgument},
		{"list bogus", func() error {
			_, err := srv.ListFleetPlacementConsents(alice, &pb.ListFleetPlacementConsentsRequest{State: "bogus"})
			return err
		}, codes.InvalidArgument},
		{"answer", func() error { return answer(alice, srv, "", req.ID, "granted") }, codes.OK},
		{"second answer", func() error { return answer(alice, srv, "", req.ID, "refused") }, codes.AlreadyExists},
		{"no service", func() error { return answer(alice, bare, "", req.ID, "granted") }, codes.Unavailable},
		{"no ledger", func() error {
			_, err := newFleetPlacementGRPCServer(t).ListFleetPlacementConsents(alice, &pb.ListFleetPlacementConsentsRequest{})
			return err
		}, codes.Unavailable},
	}
	for _, tc := range cases {
		if got := status.Code(tc.call()); got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFleetPlacementConsentProtoRoundTrip(t *testing.T) {
	in := &orchestration.PlacementConsentRequest{
		ID: "id", TenantID: "acme",
		Move:  orchestration.ConsentMove{SpecName: "agents", SpecGeneration: 3, Class: "agent", WorkID: "s1", SourceHost: "h1"},
		State: orchestration.ConsentRefused, By: "alice", Reason: "not now",
		RaisedAt:   time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
		AnsweredAt: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC),
	}
	raw, err := proto.Marshal(fleetPlacementConsentToProto(in))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out pb.FleetPlacementConsentRequest
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	back := &orchestration.PlacementConsentRequest{
		ID: out.GetId(), TenantID: out.GetTenantId(),
		Move: orchestration.ConsentMove{
			SpecName: out.GetMove().GetSpecName(), SpecGeneration: out.GetMove().GetSpecGeneration(),
			Class: out.GetMove().GetClass(), WorkID: out.GetMove().GetWorkId(), SourceHost: out.GetMove().GetSourceHost(),
		},
		State: orchestration.ConsentState(out.GetState()), By: out.GetBy(), Reason: out.GetReason(),
		RaisedAt: out.GetRaisedAt().AsTime(), AnsweredAt: out.GetAnsweredAt().AsTime(),
	}
	if !reflect.DeepEqual(in, back) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", back, in)
	}
	if fleetPlacementConsentToProto(nil) != nil {
		t.Fatal("a nil request maps to a non-nil message")
	}
	if n := reflect.TypeOf(orchestration.PlacementConsentRequest{}).NumField(); n != 8 {
		t.Fatalf("PlacementConsentRequest has %d fields; map the new one in server_fleet_placement.go and this count", n)
	}
	if n := reflect.TypeOf(orchestration.ConsentMove{}).NumField(); n != 5 {
		t.Fatalf("ConsentMove has %d fields; map the new one in server_fleet_placement.go and this count", n)
	}
}
