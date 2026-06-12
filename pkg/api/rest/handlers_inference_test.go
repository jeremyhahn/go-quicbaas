//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// inferMockAgentService — narrowly-scoped mock for inference tests
// =============================================================================

// inferMockAgentService is a stand-alone mock that delegates Infer to a
// configurable function. All other AgentService methods are no-ops so the
// mock compiles without changes when the interface grows.
type inferMockAgentService struct {
	mockAgentService // embed for all other interface methods

	inferFunc func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error)
}

func (m *inferMockAgentService) Infer(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	if m.inferFunc != nil {
		return m.inferFunc(ctx, req)
	}
	ch := make(chan *orchestration.InferChunk)
	close(ch)
	return ch, nil
}

// =============================================================================
// Helpers
// =============================================================================

// setupInferenceTestServer builds a REST server wired to the given mock.
func setupInferenceTestServer(t *testing.T, svc *inferMockAgentService) (*Server, func()) {
	t.Helper()
	server, _, cleanup := setupOrchestratorMockServer(t)
	server.service = &MockQuicBaaSWithAgent{
		MockQuicBaaS: server.service.(*MockQuicBaaSWithAgent).MockQuicBaaS,
		agentService: svc,
	}
	return server, cleanup
}

// makeInferBody serialises an inferenceHTTPRequest to JSON bytes.
func makeInferBody(t *testing.T, r inferenceHTTPRequest) []byte {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal infer body: %v", err)
	}
	return b
}

// singleChunkInfer returns an Infer func that sends one final chunk.
func singleChunkInfer(text, model string) func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
	return func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
		ch := make(chan *orchestration.InferChunk, 1)
		ch <- &orchestration.InferChunk{
			Delta:      []orchestration.ContentBlock{{Type: "text", Text: text}},
			Model:      model,
			StopReason: "end_turn",
			Final:      true,
		}
		close(ch)
		return ch, nil
	}
}

// =============================================================================
// Aggregated (stream=false) tests
// =============================================================================

// TestHandleOrchestratorInference_Aggregate_HappyPath verifies that a valid
// request returns a 200 with the aggregated content block.
func TestHandleOrchestratorInference_Aggregate_HappyPath(t *testing.T) {
	svc := &inferMockAgentService{
		inferFunc: singleChunkInfer("haiku about mTLS", "qwen3:1.7b"),
	}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "ollama",
		Prompt:  "write a haiku about mTLS",
		Model:   "qwen3:1.7b",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference?tenant_id=tenant-a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp inferenceHTTPResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
	if resp.Content[0].Text != "haiku about mTLS" {
		t.Errorf("unexpected text: %q", resp.Content[0].Text)
	}
	if resp.Model != "qwen3:1.7b" {
		t.Errorf("expected model 'qwen3:1.7b', got %q", resp.Model)
	}
}

// TestHandleOrchestratorInference_Aggregate_MissingBackend verifies that an
// empty backend field returns 400.
func TestHandleOrchestratorInference_Aggregate_MissingBackend(t *testing.T) {
	svc := &inferMockAgentService{}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "",
		Prompt:  "hello",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleOrchestratorInference_Aggregate_MissingPrompt verifies that an
// empty prompt field returns 400.
func TestHandleOrchestratorInference_Aggregate_MissingPrompt(t *testing.T) {
	svc := &inferMockAgentService{}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "ollama",
		Prompt:  "",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleOrchestratorInference_Aggregate_InvalidJSON verifies that a
// malformed JSON body returns 400.
func TestHandleOrchestratorInference_Aggregate_InvalidJSON(t *testing.T) {
	svc := &inferMockAgentService{}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestHandleOrchestratorInference_Aggregate_SessionBackendRejected verifies
// that InferUnsupportedError maps to 422.
func TestHandleOrchestratorInference_Aggregate_SessionBackendRejected(t *testing.T) {
	svc := &inferMockAgentService{
		inferFunc: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return nil, &orchestration.InferUnsupportedError{Backend: req.Backend}
		},
	}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "acp",
		Prompt:  "hello",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Streaming (stream=true) tests
// =============================================================================

// TestHandleOrchestratorInference_Stream_HappyPath verifies that stream=true
// returns NDJSON with two lines, the last carrying final=true.
func TestHandleOrchestratorInference_Stream_HappyPath(t *testing.T) {
	svc := &inferMockAgentService{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			ch := make(chan *orchestration.InferChunk, 2)
			ch <- &orchestration.InferChunk{
				Delta: []orchestration.ContentBlock{{Type: "text", Text: "hello "}},
				Final: false,
			}
			ch <- &orchestration.InferChunk{
				Delta:      []orchestration.ContentBlock{{Type: "text", Text: "world"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
				Final:      true,
			}
			close(ch)
			return ch, nil
		},
	}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "ollama",
		Prompt:  "hello",
		Stream:  true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/x-ndjson") {
		t.Errorf("expected NDJSON content-type, got %q", ct)
	}

	// Parse each NDJSON line.
	var lines []inferenceHTTPChunk
	scanner := bufio.NewScanner(rec.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var chunk inferenceHTTPChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			t.Fatalf("unmarshal NDJSON line %q: %v", line, err)
		}
		lines = append(lines, chunk)
	}

	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d", len(lines))
	}
	if lines[0].Delta[0].Text != "hello " {
		t.Errorf("unexpected first delta: %q", lines[0].Delta[0].Text)
	}
	if !lines[1].Final {
		t.Error("expected final=true on last line")
	}
	if lines[1].Model != "qwen3:1.7b" {
		t.Errorf("expected model 'qwen3:1.7b', got %q", lines[1].Model)
	}
}

// TestHandleOrchestratorInference_Stream_EmptyChannel verifies that an empty
// chunk channel still returns 200 with no NDJSON lines.
func TestHandleOrchestratorInference_Stream_EmptyChannel(t *testing.T) {
	svc := &inferMockAgentService{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			ch := make(chan *orchestration.InferChunk)
			close(ch)
			return ch, nil
		},
	}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "ollama",
		Prompt:  "hello",
		Stream:  true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even for empty stream, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Errorf("expected empty body, got %q", rec.Body.String())
	}
}

// TestHandleOrchestratorInference_TenantDefaulting verifies that when no
// tenant_id query param is present, the handler defaults to the system tenant
// (qrdbsdk.SystemTenantID); the legacy "default" tenant was retired by qrdb.
func TestHandleOrchestratorInference_TenantDefaulting(t *testing.T) {
	t.Parallel()

	var capturedTenant string
	svc := &inferMockAgentService{
		inferFunc: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			capturedTenant = req.TenantID
			return singleChunkInfer("ok", "m")(ctx, req)
		},
	}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})

	// No ?tenant_id= query param.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedTenant != qrdbsdk.SystemTenantID {
		t.Errorf("expected tenant %q, got %q", qrdbsdk.SystemTenantID, capturedTenant)
	}
}

// TestRESTInference_PassesGpuRequiredToOrchestrator verifies that a REST body
// with gpu_required=true sets InferRequest.GpuRequired and decorates the
// context via WithGpuRequired before calling agentSvc.Infer.
func TestRESTInference_PassesGpuRequiredToOrchestrator(t *testing.T) {
	t.Parallel()

	var capturedReq *orchestration.InferRequest
	var capturedCtxGpu bool
	svc := &inferMockAgentService{
		inferFunc: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			capturedReq = req
			capturedCtxGpu = orchestration.GpuRequiredFromContext(ctx)
			return singleChunkInfer("pong", "qwen3:1.7b")(ctx, req)
		},
	}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend:     "ollama",
		Prompt:      "ping",
		GpuRequired: true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedReq == nil {
		t.Fatal("Infer was not called")
	}
	if !capturedReq.GpuRequired {
		t.Error("InferRequest.GpuRequired should be true")
	}
	if !capturedCtxGpu {
		t.Error("context should carry GPU requirement flag via WithGpuRequired")
	}
}

// TestRESTInference_GpuRequiredFalseByDefault verifies that omitting
// gpu_required from the JSON body leaves InferRequest.GpuRequired false.
func TestRESTInference_GpuRequiredFalseByDefault(t *testing.T) {
	t.Parallel()

	var capturedReq *orchestration.InferRequest
	svc := &inferMockAgentService{
		inferFunc: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			capturedReq = req
			return singleChunkInfer("ok", "m")(ctx, req)
		},
	}
	server, cleanup := setupInferenceTestServer(t, svc)
	defer cleanup()

	body := makeInferBody(t, inferenceHTTPRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/inference", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.HandleOrchestratorInference(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if capturedReq == nil {
		t.Fatal("Infer was not called")
	}
	if capturedReq.GpuRequired {
		t.Error("InferRequest.GpuRequired should be false when not set in body")
	}
}
