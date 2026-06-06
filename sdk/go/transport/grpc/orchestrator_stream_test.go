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
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newOrchestratorTransport returns a Transport whose single connection points
// at an in-memory orchestrator server, suitable for exercising RequestStream.
func newOrchestratorTransport(t *testing.T) *Transport {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, &fakeOrchestratorServer{fail: false})
	})
	return &Transport{
		cfg:  &transport.Config{Timeout: 5 * time.Second},
		conn: srv.dial(t),
	}
}

func TestOrchestratorRequestStream_SubscribeSessionEvents(t *testing.T) {
	tr := newOrchestratorTransport(t)
	stream, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "SubscribeSessionEvents",
		&orchestrator.SubscribeSessionEventsRequest{SessionID: "s1", Follow: false})
	if err != nil {
		t.Fatalf("RequestStream failed: %v", err)
	}
	defer func() { _ = stream.Close() }()

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	env, ok := msg.(*orchestrator.SessionEventEnvelope)
	if !ok || env.SessionID != "s1" {
		t.Fatalf("unexpected message: %#v", msg)
	}
	// Server-streaming: Send must be unsupported, CloseSend a no-op.
	if err := stream.Send(nil); err == nil {
		t.Error("expected Send to be unsupported on server-streaming RPC")
	}
	if err := stream.CloseSend(); err != nil {
		t.Errorf("CloseSend should be a no-op: %v", err)
	}
	// Next Recv reaches EOF.
	if _, err := stream.Recv(); err == nil {
		t.Error("expected EOF/error after the single streamed message")
	}
}

func TestOrchestratorRequestStream_SubscribeJobEvents(t *testing.T) {
	tr := newOrchestratorTransport(t)
	stream, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "SubscribeJobEvents",
		&orchestrator.SubscribeJobEventsRequest{JobID: "j1"})
	if err != nil {
		t.Fatalf("RequestStream failed: %v", err)
	}
	defer func() { _ = stream.Close() }()

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	evt, ok := msg.(*orchestrator.JobEvent)
	if !ok || evt.JobID != "j1" {
		t.Fatalf("unexpected message: %#v", msg)
	}
	if err := stream.Send(nil); err == nil {
		t.Error("expected Send to be unsupported")
	}
	if err := stream.CloseSend(); err != nil {
		t.Errorf("CloseSend failed: %v", err)
	}
}

func TestOrchestratorRequestStream_InferenceStream(t *testing.T) {
	tr := newOrchestratorTransport(t)
	stream, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "InferenceStream",
		&orchestrator.InferenceRequest{Backend: "claude", Prompt: "hi"})
	if err != nil {
		t.Fatalf("RequestStream failed: %v", err)
	}
	defer func() { _ = stream.Close() }()

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	chunk, ok := msg.(*orchestrator.InferenceChunk)
	if !ok || !chunk.Final || chunk.Model != "m" {
		t.Fatalf("unexpected message: %#v", msg)
	}
	if err := stream.Send(nil); err == nil {
		t.Error("expected Send to be unsupported")
	}
	if err := stream.CloseSend(); err != nil {
		t.Errorf("CloseSend should be a no-op: %v", err)
	}
}

func TestOrchestratorRequestStream_UnsupportedMethod(t *testing.T) {
	tr := newOrchestratorTransport(t)
	if _, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "Nope", nil); err == nil {
		t.Fatal("expected error for unsupported streaming method")
	}
}

func TestOrchestratorRequestStream_InvalidRequestTypes(t *testing.T) {
	tr := newOrchestratorTransport(t)
	methods := []string{"SubscribeSessionEvents", "SubscribeJobEvents", "InferenceStream"}
	for _, m := range methods {
		if _, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", m, "wrong"); err == nil {
			t.Errorf("%s: expected invalid-request-type error", m)
		}
	}
}

func TestOrchestratorRequestStream_ClosedRecv(t *testing.T) {
	tr := newOrchestratorTransport(t)
	stream, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "InferenceStream",
		&orchestrator.InferenceRequest{Backend: "claude", Prompt: "hi"})
	if err != nil {
		t.Fatalf("RequestStream failed: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	// After Close, Recv must report a closed stream.
	if _, err := stream.Recv(); err == nil {
		t.Error("expected error receiving on a closed stream")
	}
	// Drain guard so an EOF helper import is exercised consistently.
	_ = io.EOF
}
