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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// toolTurnProtoBlocks is a tool call and its failed result as the server puts
// them on the wire, metadata included.
func toolTurnProtoBlocks() []*pb.ContentBlock {
	return []*pb.ContentBlock{
		{Type: "tool_use", Text: "go test", ToolName: "Bash", ToolUseId: "toolu_01", Bytes: 42},
		{Type: "tool_result", ToolUseId: "toolu_01", IsError: true, Bytes: 7},
	}
}

var wantInferenceToolBlocks = []orchestrator.InferenceContentBlock{
	{Type: "tool_use", Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
	{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
}

// toolTurnOrchestratorServer answers inference with toolTurnProtoBlocks.
type toolTurnOrchestratorServer struct {
	pb.UnimplementedOrchestratorServiceServer
}

func (toolTurnOrchestratorServer) Inference(context.Context, *pb.InferenceRequest) (*pb.InferenceResponse, error) {
	return &pb.InferenceResponse{Content: toolTurnProtoBlocks(), Model: "m", StopReason: "end_turn"}, nil
}

func (toolTurnOrchestratorServer) InferenceStream(_ *pb.InferenceRequest, stream grpc.ServerStreamingServer[pb.InferenceChunk]) error {
	return stream.Send(&pb.InferenceChunk{Delta: toolTurnProtoBlocks(), Model: "m", Final: true})
}

func newToolTurnTransport(t *testing.T) *Transport {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, toolTurnOrchestratorServer{})
	})
	return &Transport{
		cfg:  &transport.Config{Timeout: 5 * time.Second},
		conn: srv.dial(t),
	}
}

func TestOrchestratorInference_KeepsToolBlockMetadata(t *testing.T) {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, toolTurnOrchestratorServer{})
	})
	h := newOrchestratorHandler(srv.dial(t))

	result, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "Inference",
		&orchestrator.InferenceRequest{Backend: "claude", Prompt: "work"})
	if err != nil {
		t.Fatalf("Inference: %v", err)
	}
	resp, ok := result.(*orchestrator.InferenceResponse)
	if !ok {
		t.Fatalf("result is %T, want *InferenceResponse", result)
	}
	if !reflect.DeepEqual(resp.Content, wantInferenceToolBlocks) {
		t.Errorf("Content = %+v, want %+v", resp.Content, wantInferenceToolBlocks)
	}
}

func TestOrchestratorInferenceStream_KeepsToolBlockMetadata(t *testing.T) {
	tr := newToolTurnTransport(t)
	stream, err := tr.RequestStream(context.Background(), "quicbaas.Orchestrator", "InferenceStream",
		&orchestrator.InferenceRequest{Backend: "claude", Prompt: "work"})
	if err != nil {
		t.Fatalf("RequestStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	chunk, ok := msg.(*orchestrator.InferenceChunk)
	if !ok {
		t.Fatalf("message is %T, want *InferenceChunk", msg)
	}
	if !reflect.DeepEqual(chunk.Delta, wantInferenceToolBlocks) {
		t.Errorf("Delta = %+v, want %+v", chunk.Delta, wantInferenceToolBlocks)
	}
}

func TestPromptContentBlocksFromProto_KeepsToolBlockMetadata(t *testing.T) {
	want := []orchestrator.PromptContentBlock{
		{Type: "tool_use", Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
		{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
	}
	if got := promptContentBlocksFromProto(toolTurnProtoBlocks()); !reflect.DeepEqual(got, want) {
		t.Errorf("promptContentBlocksFromProto = %+v, want %+v", got, want)
	}
}

func TestPromptContentBlocksFromProto_NilBlockIsAnEmptyBlock(t *testing.T) {
	got := promptContentBlocksFromProto([]*pb.ContentBlock{nil})
	if len(got) != 1 || got[0] != (orchestrator.PromptContentBlock{}) {
		t.Errorf("promptContentBlocksFromProto([nil]) = %+v, want one zero block", got)
	}
}
