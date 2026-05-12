//go:build rest

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

package rest

import (
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Inference request / response wire types
// =============================================================================

// inferenceHTTPRequest is the JSON body expected at
// POST /api/v1/orchestrator/inference.
type inferenceHTTPRequest struct {
	// Backend is the named backend to route to (e.g., "ollama"). Required.
	Backend string `json:"backend"`

	// Model is the model to use. When empty the backend's default is used.
	Model string `json:"model,omitempty"`

	// Prompt is the user prompt text. Required.
	Prompt string `json:"prompt"`

	// SystemPrompt is an optional system message.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// HostAffinity pins the call to a specific agent daemon by Name or Host.
	HostAffinity string `json:"host_affinity,omitempty"`

	// GpuRequired restricts dispatch to daemons that advertise "gpu" in
	// their Capabilities slice.
	GpuRequired bool `json:"gpu_required,omitempty"`

	// HostAffinityStrict, when true, forces hard host_affinity semantics
	// for this request: if HostAffinity names a host that is offline the
	// resolver returns NoMatchingAgentError (HTTP 400) rather than falling
	// back to any available daemon. Defers to the operator-level
	// orchestration.host_affinity.mode default when false.
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`

	// Stream controls the response shape:
	//   false (default): aggregated single JSON response
	//   true: NDJSON stream (Content-Type application/x-ndjson)
	Stream bool `json:"stream,omitempty"`

	// Metadata is an opaque string map forwarded to the backend unchanged.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// inferenceHTTPChunk is one NDJSON line in a streaming response.
type inferenceHTTPChunk struct {
	// Delta carries the content blocks for this chunk.
	Delta []orchestration.ContentBlock `json:"delta"`

	// Model is set on the final chunk.
	Model string `json:"model,omitempty"`

	// StopReason is set on the final chunk.
	StopReason string `json:"stop_reason,omitempty"`

	// Final is true on the last chunk.
	Final bool `json:"final,omitempty"`
}

// inferenceHTTPResponse is the aggregated response returned when stream=false.
type inferenceHTTPResponse struct {
	// Content holds the full concatenated content from all chunks.
	Content []orchestration.ContentBlock `json:"content"`

	// Model is the model that produced the response.
	Model string `json:"model,omitempty"`

	// StopReason is the reason the model stopped generating.
	StopReason string `json:"stop_reason,omitempty"`
}

// =============================================================================
// Handler
// =============================================================================

// HandleOrchestratorInference handles
// POST /api/v1/orchestrator/inference
//
// Routes a stateless prompt to a fleet-resident agent daemon and returns the
// response. Supports two modes controlled by the "stream" field in the
// request body:
//   - stream=false (default): waits for the full response and returns a
//     single JSON object with Content-Type application/json.
//   - stream=true: flushes each InferChunk as an NDJSON line with
//     Content-Type application/x-ndjson; the final line has final=true.
func (s *Server) HandleOrchestratorInference(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	agentSvc := s.getAgentService(w, r)
	if agentSvc == nil {
		return
	}

	body, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var httpReq inferenceHTTPRequest
	if err := json.Unmarshal(body, &httpReq); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if httpReq.Backend == "" {
		RespondBadRequest(w, r, "backend is required")
		return
	}
	if httpReq.Prompt == "" {
		RespondBadRequest(w, r, "prompt is required")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		tenantID = "default"
	}
	ctx = orchestration.ContextWithTenant(ctx, tenantID)

	if httpReq.HostAffinity != "" {
		ctx = orchestration.WithHostAffinity(ctx, httpReq.HostAffinity)
	}
	if httpReq.GpuRequired {
		ctx = orchestration.WithGpuRequired(ctx)
	}
	if httpReq.HostAffinityStrict {
		ctx = orchestration.WithHostAffinityStrict(ctx)
	}

	// Install a resolve-error holder so the remote backend resolver can
	// surface a typed NoMatchingAgentError when host_affinity is strict and
	// the named host is offline. Without the holder we'd lose the typed
	// error to the soft (nil, false) fall-through.
	holder := orchestration.NewResolveErrorHolder()
	ctx = orchestration.WithResolveErrorHolder(ctx, holder)

	inferReq := &orchestration.InferRequest{
		Backend:            httpReq.Backend,
		Model:              httpReq.Model,
		Prompt:             httpReq.Prompt,
		SystemPrompt:       httpReq.SystemPrompt,
		TenantID:           tenantID,
		HostAffinity:       httpReq.HostAffinity,
		GpuRequired:        httpReq.GpuRequired,
		HostAffinityStrict: httpReq.HostAffinityStrict,
		Metadata:           httpReq.Metadata,
	}

	chunks, err := agentSvc.Infer(ctx, inferReq)
	if err != nil {
		// Substitute the resolver's typed error when strict host_affinity
		// failed: BackendNotFoundError -> NoMatchingAgentError so the
		// caller sees 400 no_matching_agent rather than 404 backend.
		if resolveErr := holder.Err(); resolveErr != nil {
			handleOrchestratorError(w, r, resolveErr)
			return
		}
		var unsup *orchestration.InferUnsupportedError
		if stderrors.As(err, &unsup) {
			RespondError(w, r, http.StatusUnprocessableEntity, unsup)
			return
		}
		handleOrchestratorError(w, r, err)
		return
	}

	if httpReq.Stream {
		s.writeInferenceStream(w, r, chunks)
		return
	}

	resp, accumErr := orchestration.AccumulateChunks(chunks)
	if accumErr != nil {
		handleOrchestratorError(w, r, accumErr)
		return
	}

	RespondJSON(w, r, http.StatusOK, &inferenceHTTPResponse{
		Content:    resp.Content,
		Model:      resp.Model,
		StopReason: resp.StopReason,
	})
}

// writeInferenceStream writes the InferChunk channel as an NDJSON stream.
// Each chunk becomes one JSON line terminated by '\n'. The HTTP response
// is flushed after every line so interactive callers see deltas as they
// arrive. The final chunk carries final=true and stop_reason.
func (s *Server) writeInferenceStream(w http.ResponseWriter, r *http.Request, chunks <-chan *orchestration.InferChunk) {
	flusher, canFlush := w.(http.Flusher)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	for chunk := range chunks {
		if chunk == nil {
			continue
		}
		line := inferenceHTTPChunk{
			Delta:      chunk.Delta,
			Model:      chunk.Model,
			StopReason: chunk.StopReason,
			Final:      chunk.Final,
		}
		if encErr := enc.Encode(line); encErr != nil {
			// Client disconnected; abort silently.
			return
		}
		if canFlush {
			flusher.Flush()
		}
	}
}
