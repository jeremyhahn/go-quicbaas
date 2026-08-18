# Ollama — Architecture

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Design and boundaries of the Ollama client integration. Feature folder:
> `docs/ollama/`.

## The problem

Several parts of the product need a local model server: embeddings for RAG and
vector indexing, chat completions for the orchestrator's inference path, and a
model list so an operator can see what a host can actually run. Ollama is the
common denominator for "a model server on this machine or on that one", and it
speaks a small, stable HTTP API.

The failure this feature prevents is three packages each hand-rolling that HTTP
client, each with its own idea of what a timeout is and none of them retrying
the same way.

## The shape chosen

A thin, typed HTTP client with one shared request path, wrapped by a service
that the rest of the product reaches through the SDK.

`pkg/ollama/` is deliberately small:

| File | Holds |
|---|---|
| `client.go` | `Client`, `ClientConfig`, `NewClient`, and the single `doRequest` path every call funnels through |
| `embeddings.go` | embedding generation, single and batch |
| `chat.go`, `chat_types.go` | chat completion |
| `models.go` | model listing and model info |
| `health.go` | availability probing |
| `errors.go` | the typed errors, including the connection-refused classification |
| `stub.go` | the build-tag-gated no-op used when the integration is compiled out |

Retries, backoff and error classification live once, in `doRequest`
(`pkg/ollama/client.go:141`), which wraps `doRequestOnce`
(`:180`). `handleErrorResponse` (`:226`) turns an HTTP status into a typed
error, and `isConnectionRefused` (`:255`) is what lets a caller distinguish
"Ollama is not running" from "Ollama said no" — the distinction an operator
actually needs and the one a bare `error` loses.

### Alternatives rejected

- **A generic OpenAI-compatible client pointed at Ollama.** Rejected because
  Ollama's model listing, model info and health endpoints have no
  OpenAI-compatible equivalent, and those are precisely the operator-facing
  parts. The compatible subset would have covered chat and left the rest
  unimplemented.
- **Folding Ollama into `pkg/embedding` as one more provider and nothing else.**
  Rejected because Ollama is used for chat and model inventory too, which have
  nothing to do with embeddings. `OllamaProvider`
  (`pkg/embedding/ollama.go:48`) is one *consumer* of this client, not its
  container.
- **Making it non-optional.** Rejected: `stub.go` exists so a build that will
  never talk to Ollama does not carry the client or its dependencies.

## Component boundaries

- **`pkg/ollama/`** — this feature. Owns the wire protocol, retries and typed
  errors.
- **`pkg/embedding/`** (`../embeddings/`) — `OllamaProvider` calls this client
  to satisfy the `Provider` interface. Provider *selection* is that feature's.
- **`pkg/rag/`** (`../rag/`) — reaches Ollama for generation through the RAG
  pipeline, not directly.
- **`pkg/orchestration/`** (`../agent-orchestration/`) — the `ollama` inference
  backend and the fleet-agent `--backend ollama` path.
- **go-trusted-platform** — the platform layer can point `base_url` at an
  attested container rather than a plain host process. That is stated in the
  config field's own doc comment (`pkg/config/ollama_config.go:32`) and is the
  reason the endpoint is configuration rather than a constant.

## Data flow

```
caller (embedding provider | RAG | orchestrator inference | MCP tool)
  │
  ▼
pkg/ollama.Client.<Embed|Chat|ListModels|Health>
  │
  ▼
doRequest  ── retry with exponential backoff (MaxRetries, RetryBackoff) ──┐
  │                                                                       │
  ▼                                                                       │
doRequestOnce ── HTTP to OllamaConfig.BaseURL ── non-2xx ─► handleErrorResponse
  │                                                                       │
  ▼                                                                       ▼
typed result                                                    typed error
```

Nothing is persisted by this feature. Model weights live in the Ollama
installation, not here.

## Configuration and its defaults

From `pkg/config/ollama_config.go`:

| Key | Default | Meaning |
|---|---|---|
| `ollama.enabled` | `false` | When false, Ollama-dependent features return errors rather than silently degrading |
| `ollama.base_url` | `http://localhost:11434` | API endpoint; may point at a remote or attested host |
| `ollama.default_embedding_model` | `nomic-embed-text` | Used when a call names no model |
| `ollama.timeout` | `120s` | Deliberately long: batch embedding is slow |
| `ollama.max_retries` | `3` | Exponential backoff between attempts |
| `ollama.retry_backoff` | `1s` | Initial backoff; each retry doubles it |
| `ollama.health_check_interval` | `30s` | Availability polling; `0` disables it |

Unusually for this codebase, **every one of these has a working environment
override**: `QUICBAAS_OLLAMA_ENABLED`, `_BASE_URL`,
`_DEFAULT_EMBEDDING_MODEL`, `_TIMEOUT`, `_MAX_RETRIES`, `_RETRY_BACKOFF` and
`_HEALTH_CHECK_INTERVAL` all appear in the explicit list `pkg/config/loader.go`
reads. Most config blocks in this repository are file-only; this one is not.

## Trust boundaries

- **`base_url` is a trust decision.** Pointing it at a remote host sends every
  prompt and every document chunk to that host in plaintext unless the URL is
  `https` and that endpoint is one you control. The default is loopback for
  exactly this reason.
- **No authentication.** Ollama's API has none, so this client sends none.
  Access control is network placement — loopback, a private network, or the
  platform's attested-container path. A deployment that exposes Ollama on a
  routable address has published an unauthenticated inference endpoint.
- **Prompts are not redacted.** Whatever a caller passes goes on the wire and
  into the Ollama host's logs.

## What this feature deliberately does not do

- **It does not manage models.** No pull, no delete, no quantisation. Model
  lifecycle is the operator's, through Ollama itself.
- **It does not choose backends.** Selecting Ollama over another inference
  backend is orchestration's decision (`../agent-orchestration/`).
- **It does not cache.** Response caching for embeddings lives in
  `../embeddings/`; this client issues the request it is given.
- **It is not a chat product.** The chat surface exists to serve the
  orchestrator's inference path, not as a user-facing assistant.
