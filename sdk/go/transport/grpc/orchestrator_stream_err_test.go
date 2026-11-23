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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// streamErrOrchestratorServer sends one envelope for the requested session,
// then fails the stream when fail is set, or ends it cleanly otherwise.
type streamErrOrchestratorServer struct {
	pb.UnimplementedOrchestratorServiceServer
	fail bool
}

func (f *streamErrOrchestratorServer) SubscribeSessionEvents(req *pb.SubscribeSessionRequest, stream grpc.ServerStreamingServer[pb.SessionEventEnvelope]) error {
	if err := stream.Send(&pb.SessionEventEnvelope{SessionId: req.SessionId, Sequence: 1, PayloadJson: []byte("{}")}); err != nil {
		return err
	}
	if f.fail {
		return status.Error(codes.Unavailable, "node lost")
	}
	return nil
}

func subscribeThroughGRPC(t *testing.T, fail bool) []*orchestrator.SessionEventEnvelope {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &streamErrOrchestratorServer{fail: fail})
	})
	tr := &Transport{
		cfg:  &transport.Config{Timeout: 5 * time.Second},
		conn: srv.dial(t),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := orchestrator.NewClient(tr).SubscribeSessionEvents(ctx, &orchestrator.SubscribeSessionEventsRequest{SessionID: "s1"})
	if err != nil {
		t.Fatalf("SubscribeSessionEvents: %v", err)
	}
	var got []*orchestrator.SessionEventEnvelope
	timeout := time.After(5 * time.Second)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, env)
		case <-timeout:
			t.Fatalf("channel did not close; got %d values", len(got))
		}
	}
}

func TestSubscribeSessionEvents_GRPC_StreamErrorReachesChannel(t *testing.T) {
	got := subscribeThroughGRPC(t, true)
	if len(got) != 2 {
		t.Fatalf("got %d values, want the envelope then the error", len(got))
	}
	if got[0].Err != nil || got[0].Sequence != 1 {
		t.Fatalf("first value = %+v, want the envelope", got[0])
	}
	if got[1].Err == nil || !strings.Contains(got[1].Err.Error(), "node lost") {
		t.Fatalf("last value Err = %v, want one containing %q", got[1].Err, "node lost")
	}
	if got[1].SessionID != "s1" {
		t.Fatalf("error value SessionID = %q, want s1", got[1].SessionID)
	}
}

func TestSubscribeSessionEvents_GRPC_CleanEndDeliversNoErr(t *testing.T) {
	got := subscribeThroughGRPC(t, false)
	if len(got) != 1 {
		t.Fatalf("got %d values, want one envelope", len(got))
	}
	if got[0].Err != nil {
		t.Fatalf("clean end delivered Err = %v", got[0].Err)
	}
}
