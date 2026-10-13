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

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// toolTurnBlocks is a tool call and its failed result, each carrying the
// metadata the service fills in and the SDK must not drop.
var toolTurnBlocks = []orchestration.ContentBlock{
	{Type: orchestration.ContentBlockToolUse, Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
	{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
}

var wantInferenceToolBlocks = []orchestratorSDK.InferenceContentBlock{
	{Type: orchestration.ContentBlockToolUse, Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
	{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
}

func toolTurnInferService() *mockQuicBaaSForInfer {
	return &mockQuicBaaSForInfer{agentSvc: &mockAgentSvcForInfer{
		inferFunc: func(context.Context, *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return inferChunkChannel(&orchestration.InferChunk{
				Delta: toolTurnBlocks, Model: "m", StopReason: "end_turn", Final: true,
			}), nil
		},
	}}
}

func TestHandleOrchestratorInference_KeepsToolBlockMetadata(t *testing.T) {
	result, err := handleOrchestratorInference(context.Background(), toolTurnInferService(),
		&orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "work"})
	require.NoError(t, err)

	resp, ok := result.(*orchestratorSDK.InferenceResponse)
	require.True(t, ok, "result is %T, want *InferenceResponse", result)
	require.Equal(t, wantInferenceToolBlocks, resp.Content)
}

func TestHandleOrchestratorInferenceStream_KeepsToolBlockMetadata(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newStream(ctx, 10)

	errCh := make(chan error, 1)
	go func() {
		errCh <- handleOrchestratorInferenceStream(ctx, toolTurnInferService(),
			&orchestratorSDK.InferenceRequest{Backend: "ollama", Prompt: "work"}, s)
	}()

	msg, err := s.Recv()
	require.NoError(t, err)
	require.NoError(t, <-errCh)
	chunk, ok := msg.(*orchestratorSDK.InferenceChunk)
	require.True(t, ok, "message is %T, want *InferenceChunk", msg)
	require.Equal(t, wantInferenceToolBlocks, chunk.Delta)
}

func TestPromptContentBlocks_KeepsToolBlockMetadata(t *testing.T) {
	require.Equal(t, []orchestratorSDK.PromptContentBlock{
		{Type: orchestration.ContentBlockToolUse, Text: "go test", ToolName: "Bash", ToolUseID: "toolu_01", Bytes: 42},
		{Type: "tool_result", ToolUseID: "toolu_01", IsError: true, Bytes: 7},
	}, promptContentBlocks(toolTurnBlocks))
}

func TestPromptContentBlocks_EmptyTurnHasNoBlocks(t *testing.T) {
	require.Empty(t, promptContentBlocks(nil))
}
