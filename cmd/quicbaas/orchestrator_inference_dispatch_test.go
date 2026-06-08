// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// withInferenceFlags saves and restores the inference-mode flags so the
// dispatch tests can flip stream/non-stream without leaking state.
func withInferenceFlags(t *testing.T) {
	t.Helper()
	origStream := inferStream
	origFmt := inferOutputFmt
	t.Cleanup(func() {
		inferStream = origStream
		inferOutputFmt = origFmt
	})
}

// TestRunOrchInference_AggregateDispatch verifies that runOrchInference routes
// to the aggregate path when streaming is disabled.
func TestRunOrchInference_AggregateDispatch(t *testing.T) {
	withInferenceFlags(t)
	called := false
	withMockOrchestrator(t, &mockOrchestratorClient{
		inferenceFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error) {
			called = true
			return &orchestratorSDK.InferenceResponse{
				Content: []orchestratorSDK.InferenceContentBlock{{Type: "text", Text: "aggregated"}},
			}, nil
		},
	})

	cmd := newOrchInferenceCommand()
	inferStream = false
	inferOutputFmt = "text"

	require.NoError(t, runOrchInference(cmd, nil))
	require.True(t, called, "aggregate inference should have been invoked")
}

// TestRunOrchInference_StreamDispatch verifies that runOrchInference routes to
// the streaming path when streaming is enabled.
func TestRunOrchInference_StreamDispatch(t *testing.T) {
	withInferenceFlags(t)
	called := false
	withMockOrchestrator(t, &mockOrchestratorClient{
		inferenceStreamFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error) {
			called = true
			ch := make(chan *orchestratorSDK.InferenceChunk, 1)
			ch <- &orchestratorSDK.InferenceChunk{
				Delta: []orchestratorSDK.InferenceContentBlock{{Type: "text", Text: "chunk"}},
			}
			close(ch)
			return ch, nil
		},
	})

	cmd := newOrchInferenceCommand()
	inferStream = true
	inferOutputFmt = "text"

	require.NoError(t, runOrchInference(cmd, nil))
	require.True(t, called, "stream inference should have been invoked")
}
