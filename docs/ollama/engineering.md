# Ollama — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/ollama/`.

This document describes all configuration options for Ollama integration in QuicBaaS.

## Configuration File

Add the `ollama` section to your QuicBaaS configuration file (`config.yaml`):

```yaml
ollama:
  enabled: true
  base_url: "http://localhost:11434"
  default_embedding_model: "nomic-embed-text"
  timeout: "120s"
  max_retries: 3
  retry_backoff: "1s"
  health_check_interval: "30s"
```

## Configuration Options

### enabled

**Type:** `boolean`
**Default:** `false`

Enables or disables Ollama integration. When disabled, all Ollama-related endpoints and tools return errors.

### base_url

**Type:** `string`
**Default:** `"http://localhost:11434"`

The base URL of the Ollama API. This can point to:
- Local Ollama instance: `http://localhost:11434`
- Remote Ollama server: `http://ollama.example.com:11434`
- Attested container (via go-trusted-platform): `http://ollama.trusted.local:11434`

### default_embedding_model

**Type:** `string`
**Default:** `"nomic-embed-text"`

The default model used for embedding generation when no model is specified in the request. Common embedding models:
- `nomic-embed-text` - 768 dimensions, general purpose
- `mxbai-embed-large` - 1024 dimensions, high quality
- `all-minilm` - 384 dimensions, fast

### timeout

**Type:** `duration`
**Default:** `"120s"`

Request timeout for Ollama API calls. Embedding generation can take significant time for large texts or batches.

### max_retries

**Type:** `integer`
**Default:** `3`

Number of retry attempts for failed requests. Retries use exponential backoff.

### retry_backoff

**Type:** `duration`
**Default:** `"1s"`

Initial backoff duration between retries. Subsequent retries double this value up to a maximum of 30 seconds.

### health_check_interval

**Type:** `duration`
**Default:** `"30s"`

Interval between automatic health checks. QuicBaaS periodically verifies Ollama connectivity and caches the availability status.

## Environment Variables

Configuration options can be overridden via environment variables:

```bash
export QUICBAAS_OLLAMA_ENABLED=true
export QUICBAAS_OLLAMA_BASE_URL=http://localhost:11434
export QUICBAAS_OLLAMA_DEFAULT_EMBEDDING_MODEL=nomic-embed-text
export QUICBAAS_OLLAMA_TIMEOUT=120s
export QUICBAAS_OLLAMA_MAX_RETRIES=3
```

`base_url` defaults to `config.DefaultOllamaBaseURL`, the one Ollama endpoint
the defaults layer states. The embedded fleet node
(`server.embedded_fleet_node.ollama_url`) and the RAG embedding default
(`rag.embedding.api_url`, `QUICBAAS_RAG_EMBEDDING_API_URL`) use the same
endpoint. When RAG is not configured, the server's embedding manager dials
`base_url`.

## Example Configurations

### Development

```yaml
ollama:
  enabled: true
  base_url: "http://localhost:11434"
  default_embedding_model: "nomic-embed-text"
  timeout: "30s"
  max_retries: 1
```

### Production

```yaml
ollama:
  enabled: true
  base_url: "http://ollama-cluster.internal:11434"
  default_embedding_model: "mxbai-embed-large"
  timeout: "120s"
  max_retries: 5
  retry_backoff: "2s"
  health_check_interval: "60s"
```

### With go-trusted-platform

When using QuicBaaS embedded in go-trusted-platform, the `base_url` is typically configured automatically to point to the attested Ollama container:

```yaml
ollama:
  enabled: true
  # Set automatically by go-trusted-platform
  base_url: "http://ollama.trusted.local:11434"
  default_embedding_model: "nomic-embed-text"
  timeout: "120s"
```

## Validation

QuicBaaS validates the Ollama configuration on startup:

- `enabled: true` requires a valid `base_url`
- `base_url` must be a valid URL
- `timeout` must be a positive duration
- `max_retries` must be non-negative

Invalid configuration prevents QuicBaaS from starting with a clear error message.

## Connection Pooling

The Ollama client uses HTTP connection pooling for efficiency:
- Maximum idle connections: 10
- Idle connection timeout: 90 seconds
- Keep-alive duration: 30 seconds

These values are optimized for typical workloads and not currently configurable.
