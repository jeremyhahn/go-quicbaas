// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package grpc

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestOrchestratorPromptSession_ReturnsCompletedTurn pins the gRPC prompt
// contract: the proto turn is flattened into the SDK response with its text
// intact and reported as done. gRPC used to derive Done from the stop reason,
// which the claude stream-json backend leaves empty on every completed turn,
// so a real answer was reported as unfinished.
func TestOrchestratorPromptSession_ReturnsCompletedTurn(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)

	result, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "PromptSession",
		&orchestrator.PromptSessionRequest{SessionID: "s1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("PromptSession() error = %v", err)
	}

	resp, ok := result.(*orchestrator.PromptSessionResponse)
	if !ok {
		t.Fatalf("result type = %T, want *orchestrator.PromptSessionResponse", result)
	}
	if resp.Response != "hi" {
		t.Errorf("Response = %q, want the turn's text", resp.Response)
	}
	if !resp.Done {
		t.Error("Done = false for a completed turn")
	}
	if resp.SessionID != "s1" {
		t.Errorf("SessionID = %q", resp.SessionID)
	}
}
