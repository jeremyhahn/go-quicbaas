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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// workerImageServer answers the two worker image RPCs.
type workerImageServer struct {
	pb.UnimplementedSDLCServiceServer
	fail bool
}

func workerImageStatusProto() *pb.WorkerImageStatus {
	return &pb.WorkerImageStatus{
		Serving: true, Queued: true,
		Last: &pb.WorkerImageReport{
			Trigger: "manual", Outcome: "failed", FinishedAtUnix: 1_800_000_000,
			Latest:     &pb.WorkerImageAgents{ClaudeCode: "2.1.280", Codex: "0.156.0"},
			Smokes:     []*pb.WorkerImageCheck{{Name: "codex/gpt-5.5", Passed: false, Detail: "model not found"}},
			Promotions: []*pb.WorkerImagePromotion{{TenantId: "acme", AgentId: "claude-coder", From: []string{"w:old"}}},
			Error:      "gate failed",
			Bound:      []*pb.WorkerImageBound{{Image: "w:old", Agents: &pb.WorkerImageAgents{ClaudeCode: "2.1.278"}, Uses: 3, Stale: true}},
		},
	}
}

func (s *workerImageServer) GetWorkerImageStatus(context.Context, *pb.GetWorkerImageStatusRequest) (*pb.GetWorkerImageStatusResponse, error) {
	if err := failErr(s.fail); err != nil {
		return nil, err
	}
	return &pb.GetWorkerImageStatusResponse{Status: workerImageStatusProto()}, nil
}

func (s *workerImageServer) RefreshWorkerImage(context.Context, *pb.RefreshWorkerImageRequest) (*pb.RefreshWorkerImageResponse, error) {
	if err := failErr(s.fail); err != nil {
		return nil, err
	}
	return &pb.RefreshWorkerImageResponse{Status: workerImageStatusProto()}, nil
}

func workerImageConn(t *testing.T, fail bool) *grpc.ClientConn {
	t.Helper()
	fake := &workerImageServer{fail: fail}
	return newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
}

func TestWorkerImageVerbsAreDispatchedOverGRPC(t *testing.T) {
	for _, method := range []string{"WorkerImageStatus", "RefreshWorkerImage"} {
		if _, ok := sdlcMethodMap[method]; !ok {
			t.Errorf("%s is not in sdlcMethodMap: the SDK refuses the verb before dialling", method)
		}
	}
}

func TestWorkerImageHandlers_RoundTrip(t *testing.T) {
	for name, handler := range map[string]sdlcHandlerFunc{
		"status":  handleSDLCWorkerImageStatus,
		"refresh": handleSDLCRefreshWorkerImage,
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := handler(context.Background(), workerImageConn(t, false), nil)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			status, ok := resp.(*sdlc.WorkerImageStatus)
			if !ok {
				t.Fatalf("got %T", resp)
			}
			last := status.Last
			if !status.Serving || !status.Queued || last == nil || last.Outcome != "failed" || last.Error != "gate failed" ||
				last.FinishedAt.Unix() != 1_800_000_000 || !last.StartedAt.IsZero() || last.Latest.Codex != "0.156.0" ||
				last.Smokes[0].Detail != "model not found" || last.Promotions[0].From[0] != "w:old" ||
				last.Bound[0].Uses != 3 || last.Bound[0].Agents.ClaudeCode != "2.1.278" || !last.Bound[0].Stale {
				t.Fatalf("decoded %+v / %+v", status, last)
			}
		})
	}
}

func TestWorkerImageHandlers_ServerError(t *testing.T) {
	for _, handler := range []sdlcHandlerFunc{handleSDLCWorkerImageStatus, handleSDLCRefreshWorkerImage} {
		if _, err := handler(context.Background(), workerImageConn(t, true), nil); err == nil {
			t.Fatal("a failing server produced no error")
		}
	}
}

func TestSDLCWorkerImageStatusFromProto_NilIsNotServing(t *testing.T) {
	if got := sdlcWorkerImageStatusFromProto(nil); got.Serving || got.Last != nil {
		t.Fatalf("got %+v", got)
	}
}
