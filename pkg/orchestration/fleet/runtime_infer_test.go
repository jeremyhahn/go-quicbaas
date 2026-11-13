// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package fleet

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Runtime.Infer tests
// =============================================================================

// TestRuntime_Infer_OllamaBackend_HappyPath verifies that Infer delegates to
// OllamaBackend.Prompt and wraps the result as a single final chunk.
func TestRuntime_Infer_OllamaBackend_HappyPath(t *testing.T) {
	t.Parallel()

	ollamaBackend := &fakeOllamaBackend{
		promptResp: &orchestration.PromptResponse{
			Content:    []orchestration.ContentBlock{{Type: "text", Text: "haiku text here"}},
			Model:      "qwen3:1.7b",
			StopReason: "end_turn",
		},
	}

	rt := newInferTestRuntime(t, map[string]orchestration.AgentBackend{
		"ollama": ollamaBackend,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := rt.Infer(ctx, "ollama", &orchestration.InferRequest{
		Backend:  "ollama",
		Prompt:   "write a haiku about mTLS",
		Model:    "qwen3:1.7b",
		TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatalf("Infer returned unexpected error: %v", err)
	}

	resp, accumErr := orchestration.AccumulateChunks(ch)
	if accumErr != nil {
		t.Fatalf("AccumulateChunks: %v", accumErr)
	}

	if len(resp.Content) == 0 {
		t.Fatal("expected non-empty Content")
	}
	if resp.Content[0].Text != "haiku text here" {
		t.Errorf("expected 'haiku text here', got %q", resp.Content[0].Text)
	}
	if resp.Model != "qwen3:1.7b" {
		t.Errorf("expected model 'qwen3:1.7b', got %q", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", resp.StopReason)
	}
}

// TestRuntime_Infer_UnknownBackend verifies that Infer returns ErrRuntimeUnknownBackend
// when the backend name is not registered.
func TestRuntime_Infer_UnknownBackend(t *testing.T) {
	t.Parallel()

	rt := newInferTestRuntime(t, map[string]orchestration.AgentBackend{
		"ollama": &fakeOllamaBackend{},
	})

	_, err := rt.Infer(context.Background(), "unknown-backend", &orchestration.InferRequest{
		Backend: "unknown-backend",
		Prompt:  "test",
	})
	if err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
	if !errors.Is(err, ErrRuntimeUnknownBackend) {
		t.Errorf("expected ErrRuntimeUnknownBackend, got %T: %v", err, err)
	}
}

// TestRuntime_Infer_ACPBackend_Rejected verifies that Infer rejects
// session-based backends with InferUnsupportedError.
func TestRuntime_Infer_ACPBackend_Rejected(t *testing.T) {
	t.Parallel()

	acpBackend := &fakeACPBackend{}

	rt := newInferTestRuntime(t, map[string]orchestration.AgentBackend{
		"acp": acpBackend,
	})

	_, err := rt.Infer(context.Background(), "acp", &orchestration.InferRequest{
		Backend: "acp",
		Prompt:  "test",
	})
	if err == nil {
		t.Fatal("expected InferUnsupportedError for ACP backend, got nil")
	}
	var unsup *orchestration.InferUnsupportedError
	if !errors.As(err, &unsup) {
		t.Errorf("expected InferUnsupportedError, got %T: %v", err, err)
	}
	if !errors.Is(err, orchestration.ErrInferUnsupported) {
		t.Errorf("expected errors.Is ErrInferUnsupported, got %v", err)
	}
}

// TestRuntime_Infer_BackendError verifies that a backend Prompt error is
// wrapped as an InferBackendError.
func TestRuntime_Infer_BackendError(t *testing.T) {
	t.Parallel()

	ollamaBackend := &fakeOllamaBackend{
		promptErr: &orchestration.PromptError{SessionID: "", Cause: errors.New("model not found")},
	}

	rt := newInferTestRuntime(t, map[string]orchestration.AgentBackend{
		"ollama": ollamaBackend,
	})

	_, err := rt.Infer(context.Background(), "ollama", &orchestration.InferRequest{
		Backend: "ollama",
		Prompt:  "test",
	})
	if err == nil {
		t.Fatal("expected InferBackendError, got nil")
	}
	var ibErr *orchestration.InferBackendError
	if !errors.As(err, &ibErr) {
		t.Errorf("expected InferBackendError, got %T: %v", err, err)
	}
}

// TestRuntime_Infer_EmitsSyntheticSessionID verifies that Infer emits the
// KindInferenceSubmitted and KindInferenceCompleted lifecycle events under
// a single synthesized session_id of the form "inference-<uuid>". This
// satisfies the control plane PushEvents handler which rejects events with an
// empty session_id, and pairs both lifecycle events under one queryable
// key in the session event log.
func TestRuntime_Infer_EmitsSyntheticSessionID(t *testing.T) {
	t.Parallel()

	ollamaBackend := &fakeOllamaBackend{
		promptResp: &orchestration.PromptResponse{
			Content:    []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
			Model:      "qwen3:1.7b",
			StopReason: "end_turn",
		},
	}

	fwd := newQueueOnlyForwarder(t)
	rt := newInferTestRuntimeWithForwarder(t, map[string]orchestration.AgentBackend{
		"ollama": ollamaBackend,
	}, fwd)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := rt.Infer(ctx, "ollama", &orchestration.InferRequest{
		Backend:  "ollama",
		Prompt:   "ping",
		Model:    "qwen3:1.7b",
		TenantID: "tenant-a",
	})
	if err != nil {
		t.Fatalf("Infer returned unexpected error: %v", err)
	}
	if _, accumErr := orchestration.AccumulateChunks(ch); accumErr != nil {
		t.Fatalf("AccumulateChunks: %v", accumErr)
	}

	events := drainForwarderQueue(fwd)
	if len(events) != 2 {
		t.Fatalf("expected 2 lifecycle events (Submitted, Completed), got %d", len(events))
	}

	submitted, completed := events[0], events[1]

	if submitted.Kind != orchestration.KindInferenceSubmitted {
		t.Errorf("expected first event KindInferenceSubmitted, got %q", submitted.Kind)
	}
	if completed.Kind != orchestration.KindInferenceCompleted {
		t.Errorf("expected second event KindInferenceCompleted, got %q", completed.Kind)
	}

	if !strings.HasPrefix(submitted.SessionID, "inference-") {
		t.Errorf("expected SessionID prefix 'inference-', got %q", submitted.SessionID)
	}
	if submitted.SessionID != completed.SessionID {
		t.Errorf("expected Submitted+Completed share same SessionID, got submitted=%q completed=%q",
			submitted.SessionID, completed.SessionID)
	}
	if submitted.TenantID != "tenant-a" || completed.TenantID != "tenant-a" {
		t.Errorf("expected both events tagged tenant-a, got submitted=%q completed=%q",
			submitted.TenantID, completed.TenantID)
	}
	if submitted.Sequence != 1 || completed.Sequence != 2 {
		t.Errorf("expected per-session sequences 1 and 2, got submitted=%d completed=%d",
			submitted.Sequence, completed.Sequence)
	}
}

// TestRuntime_Infer_NoEventsWhenTenantUnknown verifies that Infer emits no
// lifecycle events when req.TenantID is empty. The control plane PushEvents
// handler rejects events lacking tenant_id, so emitting them just produces
// log noise. The Infer call itself must still succeed.
func TestRuntime_Infer_NoEventsWhenTenantUnknown(t *testing.T) {
	t.Parallel()

	ollamaBackend := &fakeOllamaBackend{
		promptResp: &orchestration.PromptResponse{
			Content:    []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
			Model:      "qwen3:1.7b",
			StopReason: "end_turn",
		},
	}

	fwd := newQueueOnlyForwarder(t)
	rt := newInferTestRuntimeWithForwarder(t, map[string]orchestration.AgentBackend{
		"ollama": ollamaBackend,
	}, fwd)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := rt.Infer(ctx, "ollama", &orchestration.InferRequest{
		Backend:  "ollama",
		Prompt:   "ping",
		Model:    "qwen3:1.7b",
		TenantID: "", // explicit: tenant unknown
	})
	if err != nil {
		t.Fatalf("Infer returned unexpected error: %v", err)
	}
	if _, accumErr := orchestration.AccumulateChunks(ch); accumErr != nil {
		t.Fatalf("AccumulateChunks: %v", accumErr)
	}

	events := drainForwarderQueue(fwd)
	if len(events) != 0 {
		t.Fatalf("expected zero events when tenant unknown, got %d (first kind=%q)",
			len(events), events[0].Kind)
	}
}

// =============================================================================
// Fake backends for Runtime.Infer tests
// =============================================================================

// fakeOllamaBackend simulates OllamaBackend (stateless, no PersistentSessions).
type fakeOllamaBackend struct {
	promptResp *orchestration.PromptResponse
	promptErr  error
}

func (b *fakeOllamaBackend) Spawn(_ context.Context, _ *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return nil, &orchestration.UnsupportedOperationError{Backend: "fake-ollama", Operation: "spawn"}
}
func (b *fakeOllamaBackend) Prompt(_ context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	if b.promptErr != nil {
		return nil, b.promptErr
	}
	if b.promptResp != nil {
		return b.promptResp, nil
	}
	return &orchestration.PromptResponse{
		Content:    []orchestration.ContentBlock{{Type: "text", Text: "default response"}},
		Model:      "fake-model",
		StopReason: "end_turn",
	}, nil
}
func (b *fakeOllamaBackend) Status(_ context.Context, _ string) (*orchestration.SessionStatus, error) {
	return nil, &orchestration.UnsupportedOperationError{Backend: "fake-ollama", Operation: "status"}
}
func (b *fakeOllamaBackend) Close(_ context.Context, _ string) error {
	return &orchestration.UnsupportedOperationError{Backend: "fake-ollama", Operation: "close"}
}
func (b *fakeOllamaBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{
		Kind:               orchestration.BackendKindModelServer,
		PersistentSessions: false,
	}
}
func (b *fakeOllamaBackend) Type() string         { return "fake-ollama" }
func (b *fakeOllamaBackend) ToolGuidance() string { return "" }

// fakeACPBackend simulates an ACP-session backend (PersistentSessions=true).
type fakeACPBackend struct{}

func (b *fakeACPBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return &orchestration.Session{ID: "sess-1", Backend: "fake-acp"}, nil
}
func (b *fakeACPBackend) Prompt(_ context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{Content: []orchestration.ContentBlock{{Type: "text", Text: "ok"}}}, nil
}
func (b *fakeACPBackend) Status(_ context.Context, _ string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{}, nil
}
func (b *fakeACPBackend) Close(_ context.Context, _ string) error { return nil }
func (b *fakeACPBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{
		Kind:               orchestration.BackendKindAgent,
		PersistentSessions: true, // session-based: Infer should reject this
		ACP:                true,
	}
}
func (b *fakeACPBackend) Type() string         { return "fake-acp" }
func (b *fakeACPBackend) ToolGuidance() string { return "" }

// newInferTestRuntime creates a minimal Runtime wired with the given backends
// for Infer-path tests.
func newInferTestRuntime(t *testing.T, backends map[string]orchestration.AgentBackend) *Runtime {
	t.Helper()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: backends,
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

// newInferTestRuntimeWithForwarder is newInferTestRuntime + an installed
// EventForwarder so tests can assert on emitted lifecycle events.
func newInferTestRuntimeWithForwarder(t *testing.T, backends map[string]orchestration.AgentBackend, fwd *EventForwarder) *Runtime {
	t.Helper()
	rt, err := NewRuntime(RuntimeConfig{
		Backends:       backends,
		Logger:         &testLogger{t: t},
		EventForwarder: fwd,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

// newQueueOnlyForwarder constructs an EventForwarder that is never started,
// so events accumulate in its in-memory queue without being shipped. Tests
// then inspect the queue directly via drainForwarderQueue.
func newQueueOnlyForwarder(t *testing.T) *EventForwarder {
	t.Helper()
	fwd, err := NewEventForwarder(EventForwarderConfig{
		ServerAddr: "test-only:0",
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewEventForwarder: %v", err)
	}
	return fwd
}

// drainForwarderQueue drains all currently-queued events (non-blocking) and
// returns them in observe order. The forwarder must NOT have been started.
func drainForwarderQueue(fwd *EventForwarder) []*orchestration.SessionEvent {
	var out []*orchestration.SessionEvent
	for {
		select {
		case entry := <-fwd.queue:
			out = append(out, entry.event)
		default:
			return out
		}
	}
}
