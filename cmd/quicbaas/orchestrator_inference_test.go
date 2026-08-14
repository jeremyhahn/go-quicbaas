// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// runOrchInferenceAggregate tests
// =============================================================================

// TestRunOrchInferenceAggregate_TextOutput verifies that the aggregate path
// prints plain text when --output=text (the default).
// Not parallel: redirects os.Stdout, which is a shared global.
func TestRunOrchInferenceAggregate_TextOutput(t *testing.T) {
	client := &mockOrchestratorClient{
		inferenceFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error) {
			return &orchestratorSDK.InferenceResponse{
				Content:    []orchestratorSDK.InferenceContentBlock{{Type: "text", Text: "hello mTLS"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
			}, nil
		},
	}

	// Capture stdout.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	inferOutputFmt = "text"
	err := runOrchInferenceAggregate(context.Background(), client, &orchestratorSDK.InferenceRequest{
		Backend: "ollama",
		Prompt:  "write a haiku about mTLS",
	})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "hello mTLS") {
		t.Errorf("expected output to contain 'hello mTLS', got: %q", buf.String())
	}
}

// TestRunOrchInferenceAggregate_JSONOutput verifies that --output=json
// serialises the full InferenceResponse.
// Not parallel: redirects os.Stdout, which is a shared global.
func TestRunOrchInferenceAggregate_JSONOutput(t *testing.T) {
	client := &mockOrchestratorClient{
		inferenceFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error) {
			return &orchestratorSDK.InferenceResponse{
				Content:    []orchestratorSDK.InferenceContentBlock{{Type: "text", Text: "haiku"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
			}, nil
		},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	inferOutputFmt = "json"
	err := runOrchInferenceAggregate(context.Background(), client, &orchestratorSDK.InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp orchestratorSDK.InferenceResponse
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &resp); err != nil {
		t.Fatalf("failed to parse JSON output: %v\noutput: %s", err, buf.String())
	}
	if len(resp.Content) == 0 || resp.Content[0].Text != "haiku" {
		t.Errorf("unexpected content: %+v", resp.Content)
	}
}

// TestRunOrchInferenceAggregate_ClientError verifies that a client error is
// propagated as a non-nil error return.
func TestRunOrchInferenceAggregate_ClientError(t *testing.T) {
	t.Parallel()

	client := &mockOrchestratorClient{
		inferenceFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (*orchestratorSDK.InferenceResponse, error) {
			return nil, &mockOrchError{msg: "backend unavailable"}
		},
	}

	inferOutputFmt = "text"
	err := runOrchInferenceAggregate(context.Background(), client, &orchestratorSDK.InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// =============================================================================
// runOrchInferenceStream tests
// =============================================================================

// TestRunOrchInferenceStream_TextOutput verifies that stream mode prints each
// delta as it arrives.
// Not parallel: redirects os.Stdout, which is a shared global.
func TestRunOrchInferenceStream_TextOutput(t *testing.T) {
	client := &mockOrchestratorClient{
		inferenceStreamFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error) {
			ch := make(chan *orchestratorSDK.InferenceChunk, 2)
			ch <- &orchestratorSDK.InferenceChunk{
				Delta: []orchestratorSDK.InferenceContentBlock{{Type: "text", Text: "hello "}},
				Final: false,
			}
			ch <- &orchestratorSDK.InferenceChunk{
				Delta:      []orchestratorSDK.InferenceContentBlock{{Type: "text", Text: "world"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
				Final:      true,
			}
			close(ch)
			return ch, nil
		},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	inferOutputFmt = "text"
	err := runOrchInferenceStream(context.Background(), client, &orchestratorSDK.InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "hello ") || !strings.Contains(out, "world") {
		t.Errorf("unexpected stream output: %q", out)
	}
}

// TestRunOrchInferenceStream_JSONOutput verifies that --output=json writes one
// JSON line per chunk.
// Not parallel: redirects os.Stdout, which is a shared global.
func TestRunOrchInferenceStream_JSONOutput(t *testing.T) {
	client := &mockOrchestratorClient{
		inferenceStreamFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error) {
			ch := make(chan *orchestratorSDK.InferenceChunk, 1)
			ch <- &orchestratorSDK.InferenceChunk{
				Delta:      []orchestratorSDK.InferenceContentBlock{{Type: "text", Text: "done"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
				Final:      true,
			}
			close(ch)
			return ch, nil
		},
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	inferOutputFmt = "json"
	err := runOrchInferenceStream(context.Background(), client, &orchestratorSDK.InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each line must be valid JSON.
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var chunk orchestratorSDK.InferenceChunk
		if jsonErr := json.Unmarshal([]byte(line), &chunk); jsonErr != nil {
			t.Errorf("invalid JSON line %q: %v", line, jsonErr)
		}
	}
}

// TestRunOrchInferenceStream_ClientError verifies that a stream dial error is
// propagated.
func TestRunOrchInferenceStream_ClientError(t *testing.T) {
	t.Parallel()

	client := &mockOrchestratorClient{
		inferenceStreamFn: func(_ context.Context, _ *orchestratorSDK.InferenceRequest) (<-chan *orchestratorSDK.InferenceChunk, error) {
			return nil, &mockOrchError{msg: "connection refused"}
		},
	}

	inferOutputFmt = "text"
	err := runOrchInferenceStream(context.Background(), client, &orchestratorSDK.InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// =============================================================================
// extractText / inferenceBlockText tests
// =============================================================================

// TestExtractText verifies that text blocks are concatenated and non-text
// blocks are ignored.
func TestExtractText(t *testing.T) {
	t.Parallel()

	blocks := []orchestratorSDK.InferenceContentBlock{
		{Type: "text", Text: "hello "},
		{Type: "tool_use", Text: "ignored"},
		{Type: "text", Text: "world"},
	}
	got := extractText(blocks)
	want := "hello world"
	if got != want {
		t.Errorf("extractText = %q, want %q", got, want)
	}
}

// TestExtractText_Empty verifies that an empty slice produces an empty string.
func TestExtractText_Empty(t *testing.T) {
	t.Parallel()

	got := extractText(nil)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// =============================================================================
// Shared error helper
// =============================================================================

// mockOrchError is a minimal error type for CLI test assertions.
type mockOrchError struct{ msg string }

func (e *mockOrchError) Error() string { return e.msg }

// TestRunOrchInference_FactoryError exercises the top-level
// runOrchInference factory error branch.
func TestRunOrchInference_FactoryError(t *testing.T) {
	withFailingOrchestratorFactory(t, stderrors_NewForInference("factory boom"))
	err := runOrchInference(newOrchInferenceCommand(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// stderrors_NewForInference avoids name clashes with the existing test
// file's local errors helper by using a unique name.
func stderrors_NewForInference(msg string) error {
	return localInferenceError(msg)
}

type localInferenceError string

func (e localInferenceError) Error() string { return string(e) }
