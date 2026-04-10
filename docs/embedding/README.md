# Embedding Providers

QuicBaaS supports multiple embedding providers for generating vector representations of text. The embedding system is used by both the RAG pipeline and standalone vector operations.

## Supported Providers

| Provider | Type | API Key Required | Default Port |
|----------|------|------------------|--------------|
| Ollama | Local | No | 11434 |
| LMStudio | Local | No | 1234 |
| OpenAI | Cloud | Yes | - |
| Voyage AI | Cloud | Yes | - |

## Provider Interface

All providers implement the `Provider` interface:

```go
type Provider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    MaxTokens() int
    Name() string
    Close() error
}
```

## Quick Start

### Ollama

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/embedding"

cfg := embedding.OllamaConfig{
    BaseURL:    "http://localhost:11434",
    Model:      "nomic-embed-text",
    Timeout:    120 * time.Second,
    MaxRetries: 3,
}

provider, err := embedding.NewOllamaProvider(cfg)
if err != nil {
    log.Fatal(err)
}
defer provider.Close()

vector, err := provider.Embed(ctx, "Hello world")
```

### LMStudio

```go
cfg := embedding.LMStudioConfig{
    BaseURL:    "http://localhost:1234",
    Model:      "nomic-ai/nomic-embed-text-v1.5-GGUF",
    Timeout:    60,
    MaxRetries: 3,
}

provider, err := embedding.NewLMStudioProvider(cfg)
```

### OpenAI

```go
cfg := embedding.Config{
    Provider:   "openai",
    APIURL:     "https://api.openai.com",
    APIKey:     os.Getenv("OPENAI_API_KEY"),
    Model:      "text-embedding-3-small",
    Dimensions: 1536,
}

provider, err := embedding.NewOpenAIProvider(cfg)
```

### Voyage AI

```go
cfg := embedding.Config{
    Provider:   "voyage",
    APIURL:     "https://api.voyageai.com/v1/embeddings",
    APIKey:     os.Getenv("VOYAGE_API_KEY"),
    Model:      "voyage-code-2",
    Dimensions: 1536,
}

provider, err := embedding.NewVoyageProvider(cfg)
```

## Build Tags

Embedding providers require the `vector` build tag:

```bash
go build -tags vector ./...
```

## Configuration

See [providers.md](providers.md) for detailed provider setup and model reference.

## Error Handling

Common errors:

| Error | Description |
|-------|-------------|
| `ErrProviderNotFound` | Unknown provider type |
| `ErrInvalidCredentials` | Invalid or missing API key |
| `ErrRateLimitExceeded` | API rate limit hit |
| `ErrModelNotFound` | Model not available |
| `ErrProviderUnavailable` | Provider not reachable |
| `ErrDimensionsMismatch` | Inconsistent embedding dimensions |

## Multi-Tenant Isolation

The embedding service validates tenant context on all operations using `iam.TenantFromContext(ctx)`.

### Validation Scope

- **Embed**: Tenant ID is extracted and validated before the request reaches the embedding provider
- **EmbedBatch**: Same validation applies to each batch request

Invalid or malformed tenant IDs are rejected at the service layer with a typed error before any provider call executes.

### Cache and Provider Boundary

Embedding caches are managed inside `embedding.Manager`. The service layer is responsible only for validating tenant context, not for cache isolation. Provider selection and invocation proceed normally after validation passes.

### Single-Tenant Mode

When the tenant ID from context is empty, the service operates in single-tenant mode. No tenant validation occurs and requests pass directly to the embedding provider. Existing single-tenant deployments are unaffected.

### Cross-Tenant Access

Requests with invalid tenant IDs return a typed `ErrTenantAccessDenied` error. Validation is enforced at the service layer before any work is delegated to the provider.
