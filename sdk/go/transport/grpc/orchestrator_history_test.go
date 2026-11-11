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
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// historyOrchestratorServer replays a session log of `events` events,
// sequences 1..events, from the requested sequence, the way the server's
// SubscribeSessionEvents does with Follow false.
type historyOrchestratorServer struct {
	pb.UnimplementedOrchestratorServiceServer
	events uint64
}

func (f *historyOrchestratorServer) SubscribeSessionEvents(req *pb.SubscribeSessionRequest, stream grpc.ServerStreamingServer[pb.SessionEventEnvelope]) error {
	from := req.FromSequence
	if from == 0 {
		from = 1
	}
	for seq := from; seq <= f.events; seq++ {
		if err := stream.Send(&pb.SessionEventEnvelope{SessionId: req.SessionId, TenantId: req.TenantId, Sequence: seq, PayloadJson: []byte("{}")}); err != nil {
			return err
		}
	}
	return nil
}

func sessionHistoryPage(t *testing.T, events uint64, req *orchestrator.GetSessionHistoryRequest) *orchestrator.GetSessionHistoryResponse {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &historyOrchestratorServer{events: events})
	})
	h := newOrchestratorHandler(srv.dial(t))
	out, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "GetSessionHistory", req)
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	resp, ok := out.(*orchestrator.GetSessionHistoryResponse)
	if !ok {
		t.Fatalf("GetSessionHistory returned %T", out)
	}
	return resp
}

// A history whose last page is exactly full ends there: NextSeq is 0, as
// the client contract documents, so a caller does not page once more for
// an empty page.
func TestOrchestratorGetSessionHistory_ExactlyFullLastPageEndsHistory(t *testing.T) {
	resp := sessionHistoryPage(t, 2, &orchestrator.GetSessionHistoryRequest{TenantID: "t1", SessionID: "s1", Limit: 2})
	if len(resp.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(resp.Events))
	}
	if resp.NextSeq != 0 {
		t.Fatalf("NextSeq = %d, want 0 at the end of the history", resp.NextSeq)
	}
}

// A full page with more history after it names the next page's first
// sequence, and that page carries on from there.
func TestOrchestratorGetSessionHistory_FullPageNamesNextSequence(t *testing.T) {
	resp := sessionHistoryPage(t, 3, &orchestrator.GetSessionHistoryRequest{TenantID: "t1", SessionID: "s1", Limit: 2})
	if len(resp.Events) != 2 || resp.NextSeq != 3 {
		t.Fatalf("got %d events and NextSeq %d, want 2 and 3", len(resp.Events), resp.NextSeq)
	}
	next := sessionHistoryPage(t, 3, &orchestrator.GetSessionHistoryRequest{TenantID: "t1", SessionID: "s1", FromSequence: resp.NextSeq, Limit: 2})
	if len(next.Events) != 1 || next.Events[0].Sequence != 3 || next.NextSeq != 0 {
		t.Fatalf("second page: %d events, NextSeq %d; want event 3 alone and NextSeq 0", len(next.Events), next.NextSeq)
	}
}

// FromSequence 0 replays the history from the first event.
func TestOrchestratorGetSessionHistory_FromSequenceZeroReplaysFromStart(t *testing.T) {
	resp := sessionHistoryPage(t, 3, &orchestrator.GetSessionHistoryRequest{TenantID: "t1", SessionID: "s1"})
	if len(resp.Events) != 3 || resp.Events[0].Sequence != 1 || resp.NextSeq != 0 {
		t.Fatalf("got %d events from %v, NextSeq %d; want 3 from sequence 1 and NextSeq 0", len(resp.Events), resp.Events, resp.NextSeq)
	}
}
