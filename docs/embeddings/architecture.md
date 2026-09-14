# Embeddings — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design and boundaries of the embedding provider layer. Feature folder:
> `docs/embeddings/`.

## The problem

Vector search, RAG retrieval and semantic indexing all need the same thing: text
in, a float vector out. The vectors have to come from somewhere, and "somewhere"
varies by deployment — a local Ollama for an air-gapped install, a hosted API
where latency matters less than quality, a GPU host shared across a fleet.

Making each consumer talk to its own embedding backend would put four HTTP
clients, four auth schemes and four retry policies into four unrelated packages.
This feature is the single seam where that variation is absorbed.

## The shape chosen

One narrow interface, several implementations, one manager that owns selection.

`pkg/embedding/provider.go:45` defines `Provider`:

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

Five implementations satisfy it, each asserted at compile time with the
`var _ Provider = (*T)(nil)` idiom:

| Provider | File | Shape |
|---|---|---|
| `OllamaProvider` | `pkg/embedding/ollama.go:48` | local or LAN Ollama daemon |
| `OpenAIProvider` | `pkg/embedding/openai.go:28` | hosted API, key required |
| `VoyageProvider` | `pkg/embedding/voyage.go:28` | hosted API, key required |
| `LMStudioProvider` | `pkg/embedding/lmstudio.go:46` | local LM Studio server |
| `NPUProvider` | `pkg/embedding/npu.go:169` | Intel NPU via an OpenVINO sidecar |

The first four delegate to a go-qrdb provider; `NPUProvider` is implemented
locally because go-qrdb has no NPU counterpart to delegate to, and it reaches its
OpenVINO sidecar over HTTP for the same reason `OllamaProvider` does — the device
may be on a different host, and the daemon must not link a vendor runtime. See
[`npu.md`](npu.md), which also records why bucketing arbitrary text into the
NPU's mandatory static shapes costs no accuracy.

`Manager` (`pkg/embedding/manager.go:38`) holds the registry:
`RegisterProvider` adds one under a name, `GetProvider` resolves one, and the
manager's own `Embed` / `EmbedBatch` delegate to the configured default. Every
consumer talks to the `Manager`, never to a concrete provider.

One asymmetry is worth knowing: `RegisterProvider` accepts any name, but go-qrdb's
`validateManagerConfig` (`pkg/embedding/manager.go:83` in go-qrdb) allowlists only
`openai`, `ollama`, `lmstudio` and `voyage` for the *configured default*. A
provider outside that list can therefore be registered and resolved by name, but
cannot yet be named as the default in configuration — go-qrdb#528 tracks
validating against the registry instead of a literal list.

### Alternatives rejected

- **A provider per consumer.** Rejected because the retry, timeout and
  dimension-mismatch handling would be written four times and diverge three
  ways. The bug this prevents is specific: a consumer that silently indexes
  768-dimension vectors into a 1536-dimension namespace.
- **Model name as a parameter on every call.** Rejected for the local interface:
  `Provider.Embed` takes text only. Model is a property of the configured
  provider, which keeps the call sites free of a value they have no basis to
  choose. The go-qrdb side does take a model parameter, which is exactly what
  the bridge below exists to reconcile.
- **Embedding as a go-qrdb capability only.** Rejected because provider
  selection, credential handling and the tenant-facing API are application
  concerns, not database concerns. What lives below is the vector primitive.

## Component boundaries

- **`pkg/embedding/`** — this repository. The `Provider` interface, the four
  implementations, `Manager`, and the bridge.
- **go-qrdb** — owns vector storage and the vector index. This feature produces
  vectors; it never stores or indexes them. Reached through `go-qrdb/sdk/go`.
- **`pkg/rag/`** — the largest consumer. RAG orchestration is a separate feature
  (`../rag/`) and owns chunking, ranking and context assembly.
- **`pkg/vector/`** — the vector service surface (`../vector/`), which persists
  what this feature produces.

### The go-qrdb bridge

`pkg/embedding/bridge.go` reconciles two interface shapes that were designed
independently:

- go-qrdb's `EmbeddingProvider` is **batch-first and takes a model**:
  `Embed(ctx, []string, model)`.
- this repository's `Provider` is **single-text and model-free**.

`providerBridgeIn` adapts a qrdb provider *into* the local interface, carrying
the default model in the adapter rather than the call. A qrdb provider retrieved
through the `Manager` therefore looks like any other provider to a caller.

This is a deliberate adapter at an SDK edge, not a duplicated implementation:
the qrdb provider does the work, and the bridge only reshapes the signature.

## Data flow

```
caller (RAG, vector service, MCP tool, REST handler)
  │
  ▼
Manager.Embed / EmbedBatch          pkg/embedding/manager.go
  │   resolve configured provider by name
  ▼
Provider implementation             ollama.go | openai.go | voyage.go | lmstudio.go
  │   one HTTP request to the backend
  ▼
[]float32 (or [][]float32)
  │
  ▼
caller stores it via the vector service, or ranks with it in RAG
```

Nothing here is persisted. The embedding cache that `GetCacheStats` and
`ClearCache` report on is a request-level cache in front of the provider, not a
store of record — losing it costs latency, never data.

## Trust boundaries

- **Provider credentials.** `EmbeddingConfig.APIKey`
  (`pkg/config/rag_config.go:40`) carries a third-party API key. It is a secret
  in the ordinary sense: it belongs in the secrets engine
  (`../secrets/`) and reaches the config through a secret reference, not as a
  literal in a checked-in YAML file.
- **Text leaves the deployment.** With `openai` or `voyage` selected, every
  string passed to `Embed` is sent to a third party. That is a data-residency
  decision, not a performance one, and it is the reason `ollama` and `lmstudio`
  exist as first-class options rather than test doubles.
- **No tenant isolation in the cache today.** Tenant-scoped cache-key helpers
  exist and are tested, but are not wired into a request path, so the embedding
  cache is not an enforced per-tenant boundary. Recorded here because a reader
  sizing this feature for a multi-tenant deployment needs to know it, and
  because writing "per-tenant cache isolation" without that caveat would be
  false. See `engineering.md`.

## What this feature deliberately does not do

- **It does not choose a model per call.** The provider is configured; callers
  do not shop for models at the call site.
- **It does not store vectors.** Storage and indexing are go-qrdb's, surfaced
  by `../vector/`.
- **It does not chunk text.** Splitting a document into embeddable units is RAG
  orchestration's job (`../rag/`).
- **It does not implement a local embedding model.** Every provider is a client
  of something else. There is no in-process inference here.
