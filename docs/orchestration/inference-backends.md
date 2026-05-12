# Inference Backends

QuicBaaS supports three backend categories for AI inference: sessionless local servers (Ollama), session-based agent platforms (ACP: Claude Code, Codex, OpenCode), and HTTP API inference providers (Anthropic, OpenAI, xAI, etc.). The `APIInferenceBackend` interface abstracts API-based providers, enabling vendor-neutral integrations without importing provider SDKs.

## Backend Categories

| Category | Examples | Session Model | Authentication |
|----------|----------|---------------|-----------------|
| **Sessionless Local** | Ollama | Stateless HTTP requests | Optional (API key) |
| **Session-Based ACP** | Claude Code, Codex, OpenCode | Persistent agent sessions | OAuth2 + mTLS |
| **API Inference** | Anthropic, OpenAI, xAI | Stateless HTTP requests | Secret ref (OAuth, API key) |

**Note:** ACP backends belong in the Sessions and Workflows layer, not here. This package covers stateless HTTP API providers only.

## APIInferenceBackend Interface

All API inference providers must implement this contract, defined in `pkg/orchestration/inference/backend.go`:

```go
type APIInferenceBackend interface {
    // Provider returns the canonical provider identifier.
    // Examples: "anthropic", "openai", "xai"
    Provider() string
    
    // BaseURL returns the configured API endpoint.
    // Used for diagnostics; never used for routing.
    BaseURL() string
    
    // ListModels returns the catalog advertised by the provider instance.
    // An empty slice means "provider reachable but no models advertised".
    ListModels(ctx context.Context) ([]Model, error)
    
    // Generate executes a non-streaming completion and returns the full
    // assistant message plus token usage.
    Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
    
    // Stream executes a streaming completion. Implementations MUST close both
    // returned channels when the stream drains or context cancels.
    // The chunk channel emits GenerateChunk values; the error channel emits
    // at most one error then closes. An empty close indicates clean completion.
    Stream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, <-chan error)
    
    // Close releases underlying resources (HTTP clients, goroutines, pools).
    // Safe to call exactly once.
    Close() error
}

type Model struct {
    ID       string  // Canonical identifier (e.g., "claude-opus-4-1")
    Name     string  // Display name
    Provider string  // Provider (echoed from Provider())
}

type GenerateRequest struct {
    Model    string         // Model ID
    Messages []ChatMessage  // Conversation context
    MaxTokens int          // Max completion tokens
    // ... other fields ...
}

type GenerateResponse struct {
    Content  string         // Full assistant message
    StopReason string       // "end_turn", "max_tokens", etc.
    InputTokens int         // Prompt token count
    OutputTokens int        // Completion token count
}

type GenerateChunk struct {
    Delta      string       // Incremental content
    FinishReason string     // Non-empty on stream end
}
```

## Registry & Factory Pattern

Providers are registered at init-time using a map-based dispatcher:

```go
type ProviderFactory func(ctx context.Context, cfg Config) (APIInferenceBackend, error)

type Registry struct {
    mu        sync.RWMutex
    factories map[string]ProviderFactory
}

// Register adds a provider to the registry.
func (r *Registry) Register(provider string, factory ProviderFactory) error { ... }

// New constructs a backend instance for a provider.
func (r *Registry) New(ctx context.Context, provider string, cfg Config) (APIInferenceBackend, error) { ... }
```

See `pkg/orchestration/inference/registry.go` for the package-level singleton.

## Adding a New Provider

Create one adapter file per provider in `pkg/orchestration/inference/`:

**Example: `pkg/orchestration/inference/anthropic.go`** (pseudocode, not shipped)

```go
package inference

import (
    "context"
    "net/http"
    "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func init() {
    // Register the factory with the default registry.
    defaultRegistry.MustRegister("anthropic", newAnthropic)
}

// anthropicBackend implements APIInferenceBackend.
type anthropicBackend struct {
    client  *http.Client
    baseURL string
    keyRef  service.SecretRef // Never embed raw API key
}

// newAnthropic constructs a backend from config.
func newAnthropic(ctx context.Context, cfg Config) (APIInferenceBackend, error) {
    // 1. Validate cfg
    if cfg.BaseURL == "" {
        cfg.BaseURL = "https://api.anthropic.com"
    }
    
    // 2. Resolve API key from secrets engine (never inline).
    keyValue, err := cfg.SecretResolver(ctx, cfg.APIKeyRef)
    if err != nil {
        return nil, err
    }
    
    // 3. Create HTTP client (reusable).
    client := &http.Client{
        Timeout: 30 * time.Second,
    }
    
    // 4. Return backend.
    return &anthropicBackend{
        client:  client,
        baseURL: cfg.BaseURL,
        keyRef:  cfg.APIKeyRef, // Store ref, not key
    }, nil
}

func (b *anthropicBackend) Provider() string { return "anthropic" }

func (b *anthropicBackend) BaseURL() string { return b.baseURL }

func (b *anthropicBackend) ListModels(ctx context.Context) ([]Model, error) {
    // Fetch model list from Anthropic API or return static catalog.
    return []Model{
        {ID: "claude-opus-4-1", Name: "Claude Opus 4.1", Provider: "anthropic"},
        {ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Provider: "anthropic"},
    }, nil
}

func (b *anthropicBackend) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
    // POST to https://api.anthropic.com/v1/messages
    // Use keyRef to fetch key from secrets engine at call time.
    // Return GenerateResponse with token counts.
    return &GenerateResponse{...}, nil
}

func (b *anthropicBackend) Stream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, <-chan error) {
    chunks := make(chan GenerateChunk)
    errs := make(chan error, 1)
    
    go func() {
        // Stream completion, emit GenerateChunk per delta, close on finish.
        defer close(chunks)
        defer close(errs)
        // ... streaming logic ...
    }()
    
    return chunks, errs
}

func (b *anthropicBackend) Close() error {
    b.client.CloseIdleConnections()
    return nil
}
```

## Credential Management

**Critical:** Never embed raw API keys in the backend struct. Always use `SecretRef`:

```go
type SecretRef struct {
    // SecretName is the key in the secrets store (e.g., "anthropic-api-key").
    SecretName string
    // Version is optional; defaults to latest.
    Version string
}

// Resolve at request time (not init time):
keyValue, err := cfg.SecretResolver(ctx, cfg.APIKeyRef)
if err != nil {
    return nil, err
}
```

This ensures:

1. Raw keys never appear in config files or logs
2. Key rotation doesn't require backend restart
3. Audit trail tracks all key access

## Configuration

Provider backends are configured in `pkg/config/inference.go`:

```go
type InferenceConfig struct {
    // Providers is a map of provider_id → provider configuration.
    Providers map[string]ProviderConfig
}

type ProviderConfig struct {
    Provider       string       // "anthropic", "openai", etc.
    BaseURL        string       // Override default endpoint (optional)
    APIKeyRef      SecretRef    // Reference to secrets store
    MaxRetries     int          // HTTP retry policy
    TimeoutSeconds int          // Request timeout
}
```

Example YAML:

```yaml
inference:
  providers:
    anthropic:
      provider: anthropic
      base_url: https://api.anthropic.com
      api_key_ref:
        secret_name: anthropic-api-key
        version: latest
      max_retries: 3
      timeout_seconds: 30
```

## Current Status

**No providers are shipped with QuicBaaS.** The interface and registry exist to enable fast integration. To add a provider:

1. Implement `APIInferenceBackend` in `pkg/orchestration/inference/PROVIDER.go`
2. Call `Register()` from the file's `init()`
3. Add configuration to the YAML
4. Test via `pkg/orchestration/inference/PROVIDER_test.go`

## UI Integration

The QuicBaaS Inference view shows a message when no backends are configured:

> No provider configured for this backend — see Settings → Inference Providers

Once a provider is configured, the view auto-discovers models via `ListModels()` and allows users to select a model for generation tasks.

**Note:** ACP backends (Claude Code, Codex) appear in Sessions/Workflows, not the Inference view, because they have persistent session semantics. They are greyed out in the Inference view if presented.

## Error Handling

Adapters should return typed errors from `pkg/orchestration/inference/errors.go`:

```go
type ProviderUnreachableError struct {
    Provider string
    Err      error
}

type InvalidModelError struct {
    Model string
    Err   error
}

type RateLimitError struct {
    RetryAfter time.Duration
}

// ... others ...
```

This allows callers to distinguish transient (retry) from fatal (give up) errors.

## Performance Considerations

- **HTTP clients**: Reuse across requests (connection pooling)
- **Context cancellation**: Respect context.Done() in Stream()
- **Token counting**: Deferred until response to save computation
- **Model caching**: Cache ListModels() result with TTL (30m default)

## Source Files

- `pkg/orchestration/inference/backend.go:34-74` — APIInferenceBackend interface
- `pkg/orchestration/inference/registry.go` — Registry and factory pattern
- `pkg/config/inference.go` — Configuration types
- `pkg/orchestration/inference/errors.go` — Typed errors
- `pkg/api/rest/handlers_inference.go` — REST endpoints for discovery

## Cross-References

- Session-based backends (ACP): `docs/orchestration/acp.md`
- Local model servers: `docs/orchestration/backends.md`
- Secrets management: `docs/secrets/api-reference.md`
- Configuration: `docs/configuration/workflow-settings.md`
