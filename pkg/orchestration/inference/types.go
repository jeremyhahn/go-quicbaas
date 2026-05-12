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

package inference

// Role identifies the speaker in a chat-style message exchange. It is declared
// as a defined string type so that callers cannot accidentally pass an
// arbitrary string where a role is expected, while still serializing as a
// plain JSON string.
type Role string

// Canonical conversation roles supported by all providers.
const (
	// RoleSystem messages provide global instructions or persona.
	RoleSystem Role = "system"

	// RoleUser messages are end-user inputs.
	RoleUser Role = "user"

	// RoleAssistant messages are prior model outputs replayed as context.
	RoleAssistant Role = "assistant"

	// RoleTool carries the result of a tool invocation back to the model.
	// Reserved for future tool-use orchestration; not all providers support it.
	RoleTool Role = "tool"
)

// FinishReason explains why generation stopped. Providers map their native
// stop reasons onto these canonical values; adapters MUST translate.
type FinishReason string

// Canonical finish reasons.
const (
	// FinishReasonStop indicates the model produced an end-of-turn token.
	FinishReasonStop FinishReason = "stop"

	// FinishReasonLength indicates max_tokens was reached.
	FinishReasonLength FinishReason = "length"

	// FinishReasonToolUse indicates the model wants to call a tool.
	FinishReasonToolUse FinishReason = "tool_use"

	// FinishReasonError indicates generation aborted due to a provider error.
	FinishReasonError FinishReason = "error"
)

// Capability is a feature flag advertised by a model. Adapters populate the
// Model.Capabilities slice using these constants so the orchestrator and UI
// can reason about feature support uniformly across providers.
type Capability string

// Canonical capability identifiers.
const (
	// CapabilityTools indicates the model supports tool / function calling.
	CapabilityTools Capability = "tools"

	// CapabilityVision indicates the model accepts image inputs.
	CapabilityVision Capability = "vision"

	// CapabilityThinking indicates the model exposes a separate "thinking"
	// channel (extended reasoning, scratchpads).
	CapabilityThinking Capability = "thinking"

	// CapabilityStreaming indicates the model supports server-sent streaming.
	CapabilityStreaming Capability = "streaming"
)

// Model describes a single model advertised by a provider.
type Model struct {
	// ID is the provider-native identifier passed back in GenerateRequest.Model.
	ID string `json:"id"`

	// Name is a human-readable label suitable for UI display. May equal ID.
	Name string `json:"name"`

	// ContextWindow is the maximum input+output token count, in tokens. Zero
	// means unknown.
	ContextWindow int `json:"context_window"`

	// Capabilities lists the features the model supports.
	Capabilities []Capability `json:"capabilities,omitempty"`
}

// Message is one entry in a conversation history sent to a provider.
type Message struct {
	// Role identifies the speaker.
	Role Role `json:"role"`

	// Content is the textual payload. Multi-part / multimodal payloads are
	// outside the scope of v1; adapters that support them may extend later.
	Content string `json:"content"`
}

// Tool describes a callable tool exposed to the model. Reserved for future
// tool-use orchestration; today the field is plumbed through but not invoked.
type Tool struct {
	// Name is the unique tool identifier.
	Name string `json:"name"`

	// Description is a natural-language description used by the model to
	// decide when to invoke the tool.
	Description string `json:"description"`

	// InputSchema is a JSON Schema document describing the tool arguments.
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// GenerateRequest is the canonical inference request. Adapters translate it
// into the provider's native wire format.
type GenerateRequest struct {
	// Model selects which advertised model to invoke. Required.
	Model string `json:"model"`

	// Messages is the conversation history. Required and non-empty.
	Messages []Message `json:"messages"`

	// MaxTokens caps generation length. Zero means "use provider default".
	MaxTokens int `json:"max_tokens,omitempty"`

	// Temperature controls sampling randomness. Zero means "use provider default".
	Temperature float64 `json:"temperature,omitempty"`

	// TopP controls nucleus sampling. Zero means "use provider default".
	TopP float64 `json:"top_p,omitempty"`

	// Tools optionally exposes tools to the model. Reserved for tool-use.
	Tools []Tool `json:"tools,omitempty"`

	// Metadata carries opaque provider-specific hints (e.g. "user" id for
	// abuse tracking, request tags). Adapters MAY ignore unknown keys.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Usage reports token accounting for a completed request.
type Usage struct {
	// PromptTokens is the count of tokens billed for input.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the count of tokens billed for output.
	CompletionTokens int `json:"completion_tokens"`

	// TotalTokens is PromptTokens + CompletionTokens. Some providers report
	// this directly; others compute it.
	TotalTokens int `json:"total_tokens"`
}

// GenerateResponse is the canonical non-streaming response.
type GenerateResponse struct {
	// Content is the assistant's reply text.
	Content string `json:"content"`

	// Model is the model that actually served the request (may differ from
	// the request when the provider routes to a versioned alias).
	Model string `json:"model"`

	// Usage reports token billing.
	Usage Usage `json:"usage"`

	// FinishReason explains why generation stopped.
	FinishReason FinishReason `json:"finish_reason"`
}

// GenerateChunk is one delta in a streaming response. Producers send a
// terminal chunk with a non-empty FinishReason to signal end-of-stream;
// callers MUST drain the channel until close to release resources.
type GenerateChunk struct {
	// Delta is the incremental text produced since the previous chunk. May
	// be empty on the terminal chunk.
	Delta string `json:"delta"`

	// FinishReason is empty for intermediate chunks and set on the terminal
	// chunk.
	FinishReason FinishReason `json:"finish_reason,omitempty"`
}

// SecretRef is an indirect handle to a credential stored in the secrets
// engine. The inference layer never sees raw API keys; it resolves them via
// the configured secrets resolver at backend construction time.
type SecretRef struct {
	// Engine names the secrets backend (e.g. "secrets").
	Engine string `json:"engine"`

	// Path is the secrets-engine path to the secret object (e.g.
	// "tenants/foo/inference/anthropic").
	Path string `json:"path"`

	// Key is the field within the secret object that holds the credential.
	Key string `json:"key"`
}

// IsZero reports whether the reference is empty (no engine and no path). A
// zero SecretRef is a valid configuration value meaning "no credential".
func (r SecretRef) IsZero() bool {
	return r.Engine == "" && r.Path == "" && r.Key == ""
}

// Config is the per-instance configuration handed to a Factory when creating
// a backend. The Provider field selects which factory the registry dispatches
// to; everything else is opaque to the registry.
type Config struct {
	// Provider is the registered factory name (e.g. "anthropic", "openai").
	// Required.
	Provider string `json:"provider"`

	// BaseURL overrides the provider's default endpoint. Optional.
	BaseURL string `json:"base_url,omitempty"`

	// APIKeyRef is an indirect reference to the credential. The factory is
	// responsible for resolving it via the secrets engine.
	APIKeyRef SecretRef `json:"api_key_ref"`

	// Models optionally overrides the advertised model catalog. When empty,
	// the adapter returns its provider-native list from ListModels.
	Models []string `json:"models,omitempty"`

	// Metadata carries adapter-specific tunables (timeouts, region, org id).
	Metadata map[string]string `json:"metadata,omitempty"`
}
