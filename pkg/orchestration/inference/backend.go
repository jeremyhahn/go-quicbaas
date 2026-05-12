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

// Package inference defines a generic, provider-agnostic abstraction for
// frontier-model HTTP inference (Anthropic, OpenAI, xAI, Mistral, ...). It
// is intentionally narrow: it covers stateless prompt/response and streaming
// generation, and a small amount of model discovery. Persistent agentic
// sessions live in pkg/orchestration alongside the ACP backends; local model
// servers (Ollama) also live there. This package is for HTTP-based provider
// APIs only.
//
// Design goals:
//
//   - Vendor-neutral: no provider SDK is imported here.
//   - Add a new provider as a single adapter file under this package by
//     implementing APIInferenceBackend and calling Register from init.
//   - Credentials are passed as indirect SecretRef values; raw API keys never
//     appear in this package's surface.
//   - Map-based dispatch via Registry — O(1) provider lookup, no switches.
package inference

import "context"

// APIInferenceBackend is the contract every API-based inference provider
// adapter MUST implement. Implementations are constructed by a Factory
// registered with the package-level Registry; they are then handed to the
// orchestrator via RegisterAPIInferenceBackend.
//
// Lifecycle: Close MUST be safe to call exactly once. After Close, all other
// methods MAY return errors but MUST NOT panic.
type APIInferenceBackend interface {
	// Provider returns the canonical provider identifier (e.g. "anthropic",
	// "openai", "xai"). Stable across the backend's lifetime.
	Provider() string

	// BaseURL returns the configured endpoint. Used by diagnostics and the
	// UI; never used for routing.
	BaseURL() string

	// ListModels returns the catalog advertised by the configured provider
	// instance. Adapters may fetch this from the provider on demand or
	// return a static list. An empty slice is valid: it means "provider
	// reachable but no models advertised".
	ListModels(ctx context.Context) ([]Model, error)

	// Generate executes a single non-streaming completion. The returned
	// response captures the full assistant message plus token usage.
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)

	// Stream executes a streaming completion. Implementations MUST close
	// both returned channels when the stream is fully drained or the
	// context is cancelled. Callers MUST select on ctx.Done() in addition
	// to the returned channels.
	//
	// The chunk channel emits zero or more GenerateChunk values; the final
	// chunk has a non-empty FinishReason. The error channel emits at most
	// one error and is then closed; an empty close indicates clean
	// completion.
	Stream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, <-chan error)

	// Close releases any underlying resources (HTTP clients, goroutines,
	// pooled buffers). Safe to call exactly once.
	Close() error
}

// Example future adapter file — DO NOT uncomment, this is documentation only.
//
//   // pkg/orchestration/inference/anthropic.go
//   //
//   //   package inference
//   //
//   //   func init() { defaultRegistry.MustRegister("anthropic", newAnthropic) }
//   //
//   //   type anthropicBackend struct { /* http.Client, key, baseURL */ }
//   //
//   //   func newAnthropic(ctx context.Context, cfg Config) (APIInferenceBackend, error) {
//   //       /* validate cfg, resolve cfg.APIKeyRef, return &anthropicBackend{...} */
//   //   }
//   //
//   //   func (b *anthropicBackend) Provider() string { return "anthropic" }
//   //   func (b *anthropicBackend) BaseURL() string  { return b.baseURL }
//   //   /* ListModels, Generate, Stream, Close ... */
//
// The orchestrator never imports anthropic directly — it just looks up
// "anthropic" in the registry. That is what keeps the architecture single-
// adapter-file-per-provider.
