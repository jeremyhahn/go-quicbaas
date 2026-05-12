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

package orchestration

import "errors"

// =============================================================================
// Inference sentinel errors
// =============================================================================

// ErrInferNoChunks is returned when the remote daemon closes the Infer
// stream without emitting any chunk frames.
var ErrInferNoChunks = errors.New("orchestration: infer stream closed with no chunks")

// ErrInferBackend is the sentinel wrapped by InferBackendError.
var ErrInferBackend = errors.New("orchestration: infer backend error")

// ErrInferUnsupported is returned when Infer is called on a backend that
// requires a prior Spawn (ACP-session backends).
var ErrInferUnsupported = errors.New("orchestration: infer not supported on session-based backends")

// =============================================================================
// Inference typed errors
// =============================================================================

// InferNoChunksError is returned when the Infer stream closes before
// yielding any content. This usually indicates a daemon-side panic or
// premature stream close.
type InferNoChunksError struct {
	Backend string
}

// Error implements the error interface.
func (e *InferNoChunksError) Error() string {
	return ErrInferNoChunks.Error() + ": backend=" + e.Backend
}

// Unwrap returns the sentinel for errors.Is support.
func (e *InferNoChunksError) Unwrap() error { return ErrInferNoChunks }

// InferBackendError wraps a downstream RPC or backend error so callers can
// distinguish an inference-path failure from other remote errors.
type InferBackendError struct {
	Op  string
	Err error
}

// Error implements the error interface.
func (e *InferBackendError) Error() string {
	return ErrInferBackend.Error() + ": op=" + e.Op + ": " + e.Err.Error()
}

// Unwrap returns both the sentinel and the cause for errors.Is/As support.
func (e *InferBackendError) Unwrap() []error { return []error{ErrInferBackend, e.Err} }

// InferUnsupportedError is returned by Runtime.Infer when the requested
// backend requires session state (e.g., ACP-session backends like claude or
// codex). Callers should fall back to the Spawn+Prompt path.
type InferUnsupportedError struct {
	Backend   string
	Operation string
}

// Error implements the error interface.
func (e *InferUnsupportedError) Error() string {
	return ErrInferUnsupported.Error() + ": backend=" + e.Backend + " op=" + e.Operation
}

// Unwrap returns the sentinel for errors.Is support.
func (e *InferUnsupportedError) Unwrap() error { return ErrInferUnsupported }

// =============================================================================
// Inference request / response / chunk types
// =============================================================================

// InferRequest is the orchestration-layer request for a stateless inference
// call. It mirrors the proto shape but uses native Go types so callers never
// import the proto package directly.
type InferRequest struct {
	// Backend is the named backend to route to (e.g., "ollama"). Required.
	Backend string `json:"backend"`

	// Model is the model to use. When empty the backend's default is used.
	Model string `json:"model,omitempty"`

	// Prompt is the user prompt text. Required.
	Prompt string `json:"prompt"`

	// SystemPrompt is an optional system message prepended before the user
	// prompt.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// TenantID scopes the call for multi-tenant enforcement. Populated from
	// context at the service boundary; callers can omit it.
	TenantID string `json:"tenant_id,omitempty"`

	// HostAffinity is an optional hint to pin the call to a specific daemon
	// by Name or Host. Passed through WithHostAffinity context decoration.
	HostAffinity string `json:"host_affinity,omitempty"`

	// GpuRequired restricts dispatch to daemons that advertise "gpu" in
	// their Capabilities slice. Passed through WithGpuRequired context
	// decoration.
	GpuRequired bool `json:"gpu_required,omitempty"`

	// HostAffinityStrict, when true, forces hard host_affinity semantics for
	// this inference call: if HostAffinity is set but the named host is
	// offline the resolver returns NoMatchingAgentError rather than falling
	// back to any available daemon. Empty / false defers to the operator
	// orchestration.host_affinity.mode default (soft by default).
	HostAffinityStrict bool `json:"host_affinity_strict,omitempty"`

	// Metadata is an opaque string map forwarded unchanged to the backend.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// InferChunk is one content fragment emitted by a streaming Infer call.
// Callers that want a single aggregated response accumulate chunks until
// Final is true.
type InferChunk struct {
	// Delta holds the content blocks emitted in this chunk.
	Delta []ContentBlock `json:"delta"`

	// Model is set on the final chunk.
	Model string `json:"model,omitempty"`

	// StopReason is set on the final chunk (e.g., "end_turn").
	StopReason string `json:"stop_reason,omitempty"`

	// Final is true on the last chunk. The channel is closed immediately
	// after the final chunk is sent.
	Final bool `json:"final"`
}

// InferResponse is the aggregated response returned by callers that read the
// full stream before returning to the user.
type InferResponse struct {
	// Content is the concatenated content from all chunks.
	Content []ContentBlock `json:"content"`

	// Model is the model that produced the response.
	Model string `json:"model,omitempty"`

	// StopReason is the reason the model stopped generating.
	StopReason string `json:"stop_reason,omitempty"`
}

// AccumulateChunks drains the channel returned by an Infer call and returns
// the aggregated InferResponse. Blocks until the channel is closed.
// Returns InferNoChunksError when the channel closes without a final chunk.
func AccumulateChunks(chunks <-chan *InferChunk) (*InferResponse, error) {
	resp := &InferResponse{}
	got := false
	for chunk := range chunks {
		if chunk == nil {
			continue
		}
		got = true
		resp.Content = append(resp.Content, chunk.Delta...)
		if chunk.Model != "" {
			resp.Model = chunk.Model
		}
		if chunk.StopReason != "" {
			resp.StopReason = chunk.StopReason
		}
	}
	if !got {
		return nil, &InferNoChunksError{}
	}
	return resp, nil
}
