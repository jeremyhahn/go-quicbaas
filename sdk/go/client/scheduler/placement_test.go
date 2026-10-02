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

package scheduler

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// dispatchingTransport serves requests with Dispatch over in-process
// backends, which is exactly what the embedded transport does.
func dispatchingTransport(b Backends) *mockTransport {
	return &mockTransport{requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
		if svc != ServiceName {
			return nil, stderrors.New("wrong service " + svc)
		}
		return Dispatch(ctx, b, method, req)
	}}
}

func TestPlacementClient_ValidatesBeforeTheTransport(t *testing.T) {
	ctx := context.Background()
	called := false
	c := NewClient(&mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		called = true
		return nil, nil
	}})

	checks := map[string]error{
		"RenewNode":      c.RenewNode(ctx, ""),
		"DeregisterNode": c.DeregisterNode(ctx, ""),
		"Release":        c.Release(ctx, ""),
	}
	_, checks["RegisterNode nil"] = c.RegisterNode(ctx, nil)
	_, checks["RegisterNode id"] = c.RegisterNode(ctx, &RegisterNodeRequest{TTLSeconds: 1})
	_, checks["RegisterNode ttl"] = c.RegisterNode(ctx, &RegisterNodeRequest{Facts: NodeFacts{NodeID: "n"}})
	_, checks["GetNode"] = c.GetNode(ctx, "")
	_, checks["CordonNode"] = c.CordonNode(ctx, "")
	_, checks["UncordonNode"] = c.UncordonNode(ctx, "")
	_, checks["DrainNode"] = c.DrainNode(ctx, "")
	_, checks["Place nil"] = c.Place(ctx, nil)
	_, checks["Place id"] = c.Place(ctx, &PlacementRequest{})
	_, checks["GetPlacement"] = c.GetPlacement(ctx, "")
	//nolint:staticcheck // SA1012: a nil context is the input under test.
	_, checks["nil context"] = c.ListNodes(nil)

	for name, err := range checks {
		if errors.Code(err) != errors.CodeInvalidInput {
			t.Errorf("%s: err = %v, want InvalidInput", name, err)
		}
	}
	if called {
		t.Fatal("an invalid request reached the transport")
	}
}

func TestPlacementClient_RejectsAMistypedResponse(t *testing.T) {
	c := NewClient(&mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &JobInfo{}, nil
	}})
	if _, err := c.GetNode(context.Background(), "n"); errors.Code(err) != errors.CodeInternal {
		t.Fatalf("err = %v, want Internal", err)
	}
	if _, err := c.ListPlacements(context.Background(), nil); errors.Code(err) != errors.CodeInternal {
		t.Fatalf("err = %v, want Internal", err)
	}
}

func TestPlacementClient_PropagatesTransportErrors(t *testing.T) {
	boom := errors.NewUnavailable("t", "down")
	c := NewClient(&mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, boom
	}})
	ctx := context.Background()
	for name, err := range map[string]error{
		"RenewNode":      c.RenewNode(ctx, "n"),
		"DeregisterNode": c.DeregisterNode(ctx, "n"),
		"Release":        c.Release(ctx, "w"),
	} {
		if !stderrors.Is(err, boom) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := c.ListNodes(ctx); !stderrors.Is(err, boom) {
		t.Errorf("ListNodes: %v", err)
	}
	if _, err := c.DrainNode(ctx, "n"); !stderrors.Is(err, boom) {
		t.Errorf("DrainNode: %v", err)
	}
}

func TestDispatch_ErrorsAndAbsentHalves(t *testing.T) {
	ctx := context.Background()
	if _, err := Dispatch(ctx, Backends{}, "Teleport", nil); errors.Code(err) != errors.CodeUnimplemented {
		t.Errorf("unknown method: %v", err)
	}
	for _, method := range Methods() {
		if _, err := Dispatch(ctx, Backends{}, method, struct{}{}); errors.Code(err) != errors.CodeInvalidInput {
			t.Errorf("%s accepted a foreign request type: %v", method, err)
		}
	}
	if _, err := Dispatch(ctx, Backends{}, "GetStatus", &GetStatusRequest{}); errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("job half absent: %v", err)
	}
	if _, err := Dispatch(ctx, Backends{}, "ListNodes", &ListNodesRequest{}); errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("placement half absent: %v", err)
	}
	if len(Methods()) != 23 {
		t.Errorf("Methods() = %d, want the 11 job and 12 placement methods", len(Methods()))
	}
}

func TestDispatch_ServiceErrorsPassThrough(t *testing.T) {
	places := service.NewPlacementService(placement.New(placementtest.New()))
	c := NewClient(dispatchingTransport(Backends{Placement: places}))
	_, err := c.GetNode(tenantctx.WithTenant(context.Background(), "acme"), "missing")
	var notFound *placement.NodeNotFoundError
	if !stderrors.As(err, &notFound) {
		t.Fatalf("err = %v, want the engine's NodeNotFoundError", err)
	}
}

func TestConversions_MapEveryEnumBothWays(t *testing.T) {
	for sdk, svc := range jobTypeToService {
		if jobTypeFromService[svc] != sdk {
			t.Errorf("job type %v does not round-trip", sdk)
		}
	}
	for sdk, svc := range ownerTypeToService {
		if ownerTypeFromService[svc] != sdk {
			t.Errorf("owner type %v does not round-trip", sdk)
		}
	}
	for sdk, svc := range statusToService {
		if statusFromService[svc] != sdk {
			t.Errorf("status %v does not round-trip", sdk)
		}
	}
}

func TestConversions_NilAndEmptyInputs(t *testing.T) {
	if JobInfoFromService(nil) != nil {
		t.Error("nil job converted")
	}
	if r := CreateJobResponseFromService(nil); r.JobID != 0 {
		t.Error("nil create response produced an ID")
	}
	if r := ListJobsResponseFromService(nil); r == nil || len(r.Jobs) != 0 {
		t.Error("nil list response not empty")
	}
	if r := JobHistoryResponseFromService(3, &service.GetSchedulerJobHistoryResponse{Executions: []*service.SchedulerJobExecution{nil}}); r.JobID != 3 || len(r.Executions) != 0 {
		t.Errorf("nil execution not skipped: %+v", r)
	}
	if r := SchedulerStatusFromService(nil, time.Now()); r.Healthy {
		t.Error("nil status reported healthy")
	}
	if HistoryLimit(nil) != 0 || HistoryLimit(&HistoryOptions{Limit: 4}) != 4 {
		t.Error("HistoryLimit")
	}
	if r := ListJobsOptionsToService(nil); r == nil {
		t.Error("nil options")
	}
	owner, status := OwnerTypeSystem, JobStatusFailed
	r := ListJobsOptionsToService(&ListJobsOptions{OwnerType: &owner, Status: &status, OwnerID: "o", Tags: map[string]string{"a": "b"}, Limit: 3})
	if r.OwnerType != service.SchedulerOwnerTypeSystem || r.Status != service.SchedulerJobStatusFailed || r.Labels["a"] != "b" || r.Limit != 3 {
		t.Errorf("ListJobsOptionsToService = %+v", r)
	}
	up := UpdateJobRequestToService(&UpdateJobRequest{JobID: 1, Tags: map[string]string{"x": "y"}, MaxRetries: 2})
	if up.JobID != 1 || up.Labels["x"] != "y" || up.MaxRetries != 2 {
		t.Errorf("UpdateJobRequestToService = %+v", up)
	}
	started := time.Now().Add(-time.Hour)
	st := SchedulerStatusFromService(&service.GetSchedulerStatusResponse{Running: true, Stats: &service.SchedulerStats{StartedAt: started}}, started.Add(time.Hour))
	if st.UptimeDuration != time.Hour {
		t.Errorf("uptime = %v", st.UptimeDuration)
	}
}
