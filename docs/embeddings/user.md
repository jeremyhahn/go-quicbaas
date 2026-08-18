# Embeddings — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/embeddings/`.

QuicBaaS supports multiple embedding providers for generating vector representations of text. The embedding system is used by both the RAG pipeline and standalone vector operations.

## Ownership

The provider implementations belong to **go-qrdb**, not to QuicBaaS. `pkg/embedding` is a thin adapter: `embedding.Manager` wraps `qrdbsdk.EmbeddingManager`, and every provider constructor delegates to its `qrdbsdk` counterpart. The adapter exists only to bridge QuicBaaS's single-text `Embed(text) -> []float32` shape onto qrdb's batch-first `Embed(texts, model) -> EmbeddingResult` shape (`pkg/embedding/bridge.go`).

**When QuicBaaS code needs embeddings it goes through the manager, never through a provider constructor.** The manager is what carries tenant scoping and the shared provider configuration; constructing a provider directly bypasses both and creates a second, unscoped client. See [Inference](../agents/inference.md#embedding-providers) for the rule and its rationale.

## Supported Providers

| Provider | Type | API Key Required | Default Port |
|----------|------|------------------|--------------|
| Ollama | Local | No | 11434 |
| LMStudio | Local | No | 1234 |
| OpenAI | Cloud | Yes | - |
| Voyage AI | Cloud | Yes | - |

## Quick Start

### In-process: the manager

`embedding.NewManager` selects the provider named in `Config.Provider` and delegates to `qrdbsdk.EmbeddingManager`:

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/embedding"

cfg := embedding.Config{
    Provider:     "ollama",
    APIURL:       "http://localhost:11434",
    Model:        "nomic-embed-text",
    Dimensions:   768,
    Timeout:      120, // seconds
    MaxRetries:   3,
    CacheEnabled: true,
    CacheSize:    1024,
}

manager, err := embedding.NewManager(cfg)
if err != nil {
    log.Fatal(err)
}
defer manager.Close()

vector, err := manager.Embed(ctx, "Hello world")
vectors, err := manager.EmbedBatch(ctx, []string{"first", "second"})
dims, err := manager.Dimensions()
```

Switching providers is a configuration change, not a code change: set `Provider` to `openai`, `lmstudio` or `voyage` and supply the matching `APIURL`, `APIKey` and `Model`. `embedding.Config` is an alias for `qrdbsdk.EmbeddingConfig`, so one source of truth governs every provider.

### Over the SDK: the embedding client

SDK consumers reach embeddings through the transport rather than in-process:

```go
resp, err := sdk.AI().Embedding().GenerateEmbedding(ctx, &embedding.GenerateEmbeddingRequest{
    Text:     "Hello world",
    Provider: embedding.ProviderOllama,
})
```

The client also exposes `BatchGenerateEmbeddings`, `ListProviders`, `GetProviderInfo`, `SetDefaultProvider`, `GetCacheStats` and `ClearCache` (`sdk/go/client/embedding`).

### Custom providers

A caller that needs a provider qrdb does not ship implements the `Provider` interface and registers it on the manager:

```go
type Provider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    MaxTokens() int
    Name() string
    Close() error
}

err := manager.RegisterProvider("my-provider", myProvider)
```

The direct constructors — `NewOllamaProvider`, `NewLMStudioProvider`, `NewOpenAIProvider`, `NewVoyageProvider` — still exist as thin wrappers over their qrdb counterparts, but they are not the path for QuicBaaS code. Use them only when you need a bare provider outside a manager.

## Build Tags

The embedding package requires **both** the `vector` and `qrdb_vector` build tags; without them `pkg/embedding` compiles to stubs that return `ErrVectorNotEnabled`:

```bash
make build WITH_VECTOR=1        # sets: vector qrdb_vector
go build -tags 'vector qrdb_vector' ./...
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

The embedding service validates tenant context on all operations using `iam.TenantFromContext(ctx)`, via `prepareEmbeddingTenantContext` (`pkg/service/embedding_tenant.go`).

### Validation Scope

- **Embed**: `EmbeddingManagerAdapter.Embed` calls `prepareEmbeddingTenantContext(ctx)` before delegating to `Manager.Embed`, so a malformed tenant ID never reaches a provider (`pkg/service/embedding_adapter.go`).
- **EmbedBatch**: `EmbeddingManagerAdapter.EmbedBatch` performs the same check once per call, before the batch is delegated.
- **Dimensions / GetProvider**: not tenant-validated — they read provider metadata, not tenant data.

`validateEmbeddingTenantID` rejects a tenant ID longer than 128 characters or containing anything other than alphanumerics, hyphens and underscores.

### Errors

The embedding service has **no** `ErrTenantAccessDenied` sentinel — that name belongs to `pkg/orchestration` (`tenant_guard.go`) and is unrelated. The embedding paths return typed errors wrapped in a `*errors.QBError`:

| Condition | Wrapped error | `QBError.Code` |
|-----------|---------------|----------------|
| Malformed or over-long tenant ID | `*EmbeddingTenantValidationError` | `ErrInvalidInput` |
| Cache key owned by another tenant | `*EmbeddingCrossTenantAccessError` | `ErrCrossTenantAccess` |

### Cache and Provider Boundary

Embedding caching is delegated: `embedding.Manager` wraps `qrdbsdk.EmbeddingManager`, which owns the LRU cache configured by `CacheEnabled` / `CacheSize` / `CacheTTL` / `CacheCompress`.

The service layer additionally ships tenant-scoped cache-key helpers — `embeddingTenantScopedCacheKey` (prefix `emb-tenant/<tenantID>/`), `embeddingTenantStripCacheKey`, `embeddingCacheKeyBelongsToTenant` and `enforceEmbeddingTenantAccess`. **These are not wired into any request path today**: the only production caller in `pkg/service` is `prepareEmbeddingTenantContext`. Treat per-tenant cache-key scoping as a planned capability with the primitives already in place and tested, not as an enforced boundary.

### Single-Tenant Mode

The helpers treat a tenant as "system" via `qrdbsdk.IsSystemTenant`, which is true for the **empty** tenant ID *and* for the reserved system tenant ID. In that mode cache keys are unprefixed and access control is skipped. Existing single-tenant deployments are unaffected.
