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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func newTestPlacementService() PlacementService {
	return NewPlacementService(placement.New(placementtest.New()))
}

// codeOf returns the QBError code of err.
func codeOf(t *testing.T, err error) errors.ErrorCode {
	t.Helper()
	var qb *errors.QBError
	if !stderrors.As(err, &qb) {
		t.Fatalf("err = %v (%T), want a *QBError", err, err)
	}
	return qb.Code
}

func TestPlacementService_FullLifecycleIsTenantScoped(t *testing.T) {
	svc := newTestPlacementService()
	acme := tenantctx.WithTenant(context.Background(), "acme")
	other := tenantctx.WithTenant(context.Background(), "other")

	node, err := svc.RegisterNode(acme, &RegisterPlacementNodeRequest{
		Facts:      placement.NodeFacts{NodeID: "n1", Capacity: map[string]int64{"cpu": 4}},
		TTLSeconds: 30,
	})
	if err != nil || node.TTLSeconds != 30 {
		t.Fatalf("RegisterNode = %+v, %v", node, err)
	}
	if err := svc.RenewNode(acme, "n1"); err != nil {
		t.Fatalf("RenewNode: %v", err)
	}
	if nodes, err := svc.ListNodes(other); err != nil || len(nodes) != 0 {
		t.Fatalf("another tenant saw %d nodes (%v)", len(nodes), err)
	}
	if _, err := svc.GetNode(other, "n1"); codeOf(t, err) != errors.ErrNotFound {
		t.Fatalf("cross-tenant GetNode = %v", err)
	}

	res, err := svc.Place(acme, &placement.PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 1}})
	if err != nil || res.Placement.NodeID != "n1" {
		t.Fatalf("Place = %+v, %v", res, err)
	}
	if pl, err := svc.GetPlacement(acme, "w"); err != nil || pl.NodeID != "n1" {
		t.Fatalf("GetPlacement = %+v, %v", pl, err)
	}
	if list, err := svc.ListPlacements(acme, nil); err != nil || len(list) != 1 {
		t.Fatalf("ListPlacements = %+v, %v", list, err)
	}
	if info, err := svc.CordonNode(acme, "n1"); err != nil || !info.Status.Unschedulable {
		t.Fatalf("CordonNode = %+v, %v", info, err)
	}
	if info, err := svc.UncordonNode(acme, "n1"); err != nil || info.Status.Unschedulable {
		t.Fatalf("UncordonNode = %+v, %v", info, err)
	}
	if moved, err := svc.DrainNode(acme, "n1"); err != nil || len(moved) != 1 {
		t.Fatalf("DrainNode = %+v, %v", moved, err)
	}
	if err := svc.Release(acme, "w"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := svc.DeregisterNode(acme, "n1"); err != nil {
		t.Fatalf("DeregisterNode: %v", err)
	}
}

func TestPlacementService_ClassifiesEngineErrors(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	svc := newTestPlacementService()
	if _, err := svc.RegisterNode(ctx, &RegisterPlacementNodeRequest{Facts: placement.NodeFacts{NodeID: "n", Capacity: map[string]int64{"cpu": 1}}, TTLSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Place(ctx, &placement.PlacementRequest{WorkloadID: "w", Resources: map[string]int64{"cpu": 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DrainNode(ctx, "n"); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		err  error
		want errors.ErrorCode
	}{}
	_, e := svc.Place(ctx, &placement.PlacementRequest{})
	cases["invalid"] = struct {
		err  error
		want errors.ErrorCode
	}{e, errors.ErrInvalidInput}
	_, e = svc.GetNode(ctx, "missing")
	cases["node"] = struct {
		err  error
		want errors.ErrorCode
	}{e, errors.ErrNotFound}
	cases["placement"] = struct {
		err  error
		want errors.ErrorCode
	}{svc.Release(ctx, "missing"), errors.ErrNotFound}
	_, e = svc.Place(ctx, &placement.PlacementRequest{WorkloadID: "w"})
	cases["conflict"] = struct {
		err  error
		want errors.ErrorCode
	}{e, errors.ErrPreconditionFailed}
	_, e = svc.Place(ctx, &placement.PlacementRequest{WorkloadID: "w2"})
	cases["exhausted"] = struct {
		err  error
		want errors.ErrorCode
	}{e, errors.ErrPreconditionFailed}

	for name, tc := range cases {
		if got := codeOf(t, tc.err); got != tc.want {
			t.Errorf("%s: code = %v, want %v", name, got, tc.want)
		}
	}

	// The typed engine error stays reachable through the QBError.
	var conflict *placement.PlacementConflictError
	if !stderrors.As(cases["conflict"].err, &conflict) {
		t.Fatal("PlacementConflictError not reachable through the classification")
	}
}

func TestClassifyPlacementError_CoversEveryEngineType(t *testing.T) {
	cases := map[error]errors.ErrorCode{
		&placement.ContendedError{Op: "place", Attempts: 1}:              errors.ErrAborted,
		&placement.CorruptRecordError{Key: "k", Err: stderrors.New("x")}: errors.ErrDataLoss,
		&placement.StoreError{Op: "scan", Err: stderrors.New("x")}:       errors.ErrInternal,
	}
	for err, want := range cases {
		if got := codeOf(t, classifyPlacementError("Op", err)); got != want {
			t.Errorf("%T: code = %v, want %v", err, got, want)
		}
	}
	if classifyPlacementError("Op", nil) != nil {
		t.Fatal("nil error classified as an error")
	}
}

func TestPlacementService_RejectsNilRequestsAndBadTenants(t *testing.T) {
	svc := newTestPlacementService()
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	if _, err := svc.RegisterNode(ctx, nil); !stderrors.Is(err, ErrPlacementNilRequest) {
		t.Errorf("RegisterNode(nil) = %v", err)
	}
	if _, err := svc.Place(ctx, nil); !stderrors.Is(err, ErrPlacementNilRequest) {
		t.Errorf("Place(nil) = %v", err)
	}

	bad := tenantctx.WithTenant(ctx, "not a/tenant!")
	checks := map[string]error{
		"RenewNode":      svc.RenewNode(bad, "n"),
		"DeregisterNode": svc.DeregisterNode(bad, "n"),
		"Release":        svc.Release(bad, "w"),
	}
	_, checks["RegisterNode"] = svc.RegisterNode(bad, &RegisterPlacementNodeRequest{})
	_, checks["GetNode"] = svc.GetNode(bad, "n")
	_, checks["ListNodes"] = svc.ListNodes(bad)
	_, checks["CordonNode"] = svc.CordonNode(bad, "n")
	_, checks["UncordonNode"] = svc.UncordonNode(bad, "n")
	_, checks["DrainNode"] = svc.DrainNode(bad, "n")
	_, checks["Place"] = svc.Place(bad, &placement.PlacementRequest{WorkloadID: "w"})
	_, checks["GetPlacement"] = svc.GetPlacement(bad, "w")
	_, checks["ListPlacements"] = svc.ListPlacements(bad, nil)
	for op, err := range checks {
		if got := codeOf(t, err); got != errors.ErrInvalidInput {
			t.Errorf("%s with an invalid tenant: code = %v", op, got)
		}
	}
}

// Placement fails closed: a request with no tenant bound is refused on every
// operation, never treated as the system tenant.
func TestPlacementService_RefusesARequestWithNoTenant(t *testing.T) {
	svc := newTestPlacementService()
	none := context.Background()
	checks := map[string]error{
		"RenewNode":      svc.RenewNode(none, "n"),
		"DeregisterNode": svc.DeregisterNode(none, "n"),
		"Release":        svc.Release(none, "w"),
	}
	_, checks["RegisterNode"] = svc.RegisterNode(none, &RegisterPlacementNodeRequest{Facts: placement.NodeFacts{NodeID: "n"}, TTLSeconds: 10})
	_, checks["GetNode"] = svc.GetNode(none, "n")
	_, checks["ListNodes"] = svc.ListNodes(none)
	_, checks["CordonNode"] = svc.CordonNode(none, "n")
	_, checks["UncordonNode"] = svc.UncordonNode(none, "n")
	_, checks["DrainNode"] = svc.DrainNode(none, "n")
	_, checks["Place"] = svc.Place(none, &placement.PlacementRequest{WorkloadID: "w"})
	_, checks["GetPlacement"] = svc.GetPlacement(none, "w")
	_, checks["ListPlacements"] = svc.ListPlacements(none, nil)
	for op, err := range checks {
		var missing *tenantctx.MissingTenantError
		if !stderrors.As(err, &missing) || codeOf(t, err) != errors.ErrForbidden {
			t.Errorf("%s with no tenant = %v, want ErrForbidden wrapping *tenantctx.MissingTenantError", op, err)
		}
	}
}

// The system tenant is served only when bound explicitly, and it is a tenant
// like any other: it does not see another tenant's nodes.
func TestPlacementService_SystemTenantIsBoundExplicitly(t *testing.T) {
	svc := newTestPlacementService()
	acme := tenantctx.WithTenant(context.Background(), "acme")
	if _, err := svc.RegisterNode(acme, &RegisterPlacementNodeRequest{Facts: placement.NodeFacts{NodeID: "n"}, TTLSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	nodes, err := svc.ListNodes(tenantctx.WithSystem(context.Background()))
	if err != nil {
		t.Fatalf("ListNodes as the bound system tenant: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("the system tenant saw %d of acme's nodes", len(nodes))
	}
}

// failingShardProvider fails ForShard until ok is set.
type failingShardProvider struct {
	ok     bool
	client *svcBoostMockQRDBProvider
	calls  int
}

func (p *failingShardProvider) ForShard(id uint64) (*qrdbsdk.Client, error) {
	p.calls++
	if !p.ok {
		return nil, stderrors.New("shard unavailable")
	}
	return p.client.ForShard(id)
}

func (p *failingShardProvider) Close() error { return nil }

func TestLazyPlacementService_ResolvesOnFirstUseAndRetriesFailures(t *testing.T) {
	provider := &failingShardProvider{client: newSvcBoostMockQRDBProvider(nil, nil)}
	svc := newLazyPlacementService(provider, 1)
	if provider.calls != 0 {
		t.Fatal("the shard client was resolved before first use")
	}

	acme := tenantctx.WithTenant(context.Background(), "acme")
	_, err := svc.GetNode(acme, "n")
	if got := codeOf(t, err); got != errors.ErrUnavailable {
		t.Fatalf("failed resolution code = %v", got)
	}

	provider.ok = true
	if _, err := svc.ListNodes(acme); err != nil {
		t.Fatalf("ListNodes after recovery: %v", err)
	}
	if _, err := svc.ListNodes(acme); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("ForShard called %d times; want one failure then one cached success", provider.calls)
	}
}
