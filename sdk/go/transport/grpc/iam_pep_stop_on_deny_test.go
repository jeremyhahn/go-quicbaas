// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"slices"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// countingPEPServer evaluates each resource with a counted decision that
// denies "deny" and allows the rest, stopping after the first deny when the
// wire request carries stop_on_deny.
type countingPEPServer struct {
	iampb.UnimplementedPEPServiceServer
	calls atomic.Int32
}

func (s *countingPEPServer) CheckBatch(_ context.Context, req *iampb.CheckBatchRequest) (*iampb.CheckBatchResponse, error) {
	results := make([]bool, 0, len(req.Resources))
	for _, resource := range req.Resources {
		s.calls.Add(1)
		allowed := resource != "deny"
		results = append(results, allowed)
		if !allowed && req.StopOnDeny {
			break
		}
	}
	return &iampb.CheckBatchResponse{Results: results}, nil
}

func TestIAMHandler_PEPCheckBatch_StopOnDeny(t *testing.T) {
	tests := []struct {
		name       string
		stopOnDeny bool
		wantCount  int
	}{
		{name: "flag on stops after the first deny", stopOnDeny: true, wantCount: 2},
		{name: "flag off evaluates every resource", stopOnDeny: false, wantCount: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &countingPEPServer{}
			srv := newHandlerServer(t, func(s *grpc.Server) {
				iampb.RegisterPEPServiceServer(s, fake)
			})
			h := newIAMHandler(srv.dial(t))

			subject := iam.CheckSubject{ID: "user-1"}
			// The SDK PEP client wraps the request the same way.
			req := &struct {
				Request *iam.CheckBatchRequest `json:"request"`
			}{Request: &iam.CheckBatchRequest{
				Requests: []iam.CheckRequest{
					{Subject: subject, Action: "read", Resource: "allow-1"},
					{Subject: subject, Action: "read", Resource: "deny"},
					{Subject: subject, Action: "read", Resource: "allow-2"},
				},
				StopOnDeny: tc.stopOnDeny,
			}}

			out, err := h.handleRequest(context.Background(), "quicbaas.IAM.PEP", "CheckBatch", req)
			if err != nil {
				t.Fatalf("CheckBatch: unexpected error: %v", err)
			}
			resp, ok := out.(*iam.CheckBatchResponse)
			if !ok {
				t.Fatalf("CheckBatch: response type %T, want *iam.CheckBatchResponse", out)
			}
			if len(resp.Results) != tc.wantCount {
				t.Errorf("results = %d, want %d", len(resp.Results), tc.wantCount)
			}
			if got := int(fake.calls.Load()); got != tc.wantCount {
				t.Errorf("decisions = %d, want %d", got, tc.wantCount)
			}
		})
	}
}

// capturingPEPServer records the last wire CheckBatch request and replies
// with err when it is set.
type capturingPEPServer struct {
	iampb.UnimplementedPEPServiceServer
	last *iampb.CheckBatchRequest
	err  error
}

func (s *capturingPEPServer) CheckBatch(_ context.Context, req *iampb.CheckBatchRequest) (*iampb.CheckBatchResponse, error) {
	s.last = req
	if s.err != nil {
		return nil, s.err
	}
	return &iampb.CheckBatchResponse{Results: []bool{true}}, nil
}

func newCapturingPEPHandler(t *testing.T, fake *capturingPEPServer) *iamHandler {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterPEPServiceServer(s, fake)
	})
	return newIAMHandler(srv.dial(t))
}

// TestIAMHandler_PEPCheckBatch_RequestMapping proves both accepted request
// shapes reach the wire with their subject, action, resources, context and
// stop_on_deny intact.
func TestIAMHandler_PEPCheckBatch_RequestMapping(t *testing.T) {
	sdkShape := func(stopOnDeny bool) interface{} {
		subject := iam.CheckSubject{ID: "user-1"}
		ctx := map[string]interface{}{"region": "eu"}
		return &struct {
			Request *iam.CheckBatchRequest `json:"request"`
		}{Request: &iam.CheckBatchRequest{
			Requests: []iam.CheckRequest{
				{Subject: subject, Action: "read", Resource: "doc-1", Context: ctx},
				{Subject: subject, Action: "read", Resource: "doc-2"},
			},
			StopOnDeny: stopOnDeny,
		}}
	}
	protoShape := func(stopOnDeny bool) interface{} {
		return map[string]interface{}{
			"subject":      map[string]interface{}{"id": "user-1"},
			"action":       "read",
			"resources":    []interface{}{"doc-1", "doc-2"},
			"context":      map[string]interface{}{"region": "eu"},
			"stop_on_deny": stopOnDeny,
		}
	}
	tests := []struct {
		name       string
		req        func(bool) interface{}
		stopOnDeny bool
	}{
		{name: "sdk requests shape with stop_on_deny", req: sdkShape, stopOnDeny: true},
		{name: "sdk requests shape without stop_on_deny", req: sdkShape, stopOnDeny: false},
		{name: "proto shape with stop_on_deny", req: protoShape, stopOnDeny: true},
		{name: "proto shape without stop_on_deny", req: protoShape, stopOnDeny: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &capturingPEPServer{}
			h := newCapturingPEPHandler(t, fake)

			if _, err := h.handleRequest(context.Background(), "quicbaas.IAM.PEP", "CheckBatch", tc.req(tc.stopOnDeny)); err != nil {
				t.Fatalf("CheckBatch: unexpected error: %v", err)
			}
			got := fake.last
			if got == nil {
				t.Fatal("server received no request")
			}
			if got.GetStopOnDeny() != tc.stopOnDeny {
				t.Errorf("stop_on_deny = %v, want %v", got.GetStopOnDeny(), tc.stopOnDeny)
			}
			if got.GetSubject().GetId() != "user-1" {
				t.Errorf("subject id = %q, want user-1", got.GetSubject().GetId())
			}
			if got.GetAction() != "read" {
				t.Errorf("action = %q, want read", got.GetAction())
			}
			if want := []string{"doc-1", "doc-2"}; !slices.Equal(got.GetResources(), want) {
				t.Errorf("resources = %v, want %v", got.GetResources(), want)
			}
			if got.GetContext()["region"] != "eu" {
				t.Errorf("context = %v, want region=eu", got.GetContext())
			}
		})
	}
}

// TestIAMHandler_PEPCheckBatch_ServerError proves a server failure surfaces
// as the matching typed SDK error rather than an empty batch.
func TestIAMHandler_PEPCheckBatch_ServerError(t *testing.T) {
	fake := &capturingPEPServer{err: status.Error(codes.PermissionDenied, "denied")}
	h := newCapturingPEPHandler(t, fake)

	out, err := h.handleRequest(context.Background(), "quicbaas.IAM.PEP", "CheckBatch", map[string]interface{}{
		"subject":      map[string]interface{}{"id": "user-1"},
		"action":       "read",
		"resources":    []interface{}{"doc-1"},
		"stop_on_deny": true,
	})
	if err == nil {
		t.Fatalf("CheckBatch: expected an error, got %v", out)
	}
	if !sdkerrors.IsForbidden(err) {
		t.Errorf("got %v, want a Forbidden error", err)
	}
	if out != nil {
		t.Errorf("response = %v, want nil on error", out)
	}
}
