# Ollama — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/ollama/`.

Ollama integration commands for interacting with local Ollama embedding models.

## Overview

These commands allow you to check Ollama health, list available models, get model information, and generate embeddings. All commands connect to a running QuicBaaS server via gRPC.

## Commands

### health

Check the health status of the Ollama service.

```bash
quicbaas ollama health
```

#### Flags
- `--full-check`: Include full health check with model availability

#### Examples

```bash
# Check Ollama health
quicbaas ollama health

# Check with full model availability check
quicbaas ollama health --full-check

# Output as JSON
quicbaas ollama health --output json
```

### models

List all available models from the Ollama service.

Alias: `list`

```bash
quicbaas ollama models
```

#### Flags
- `--prefix string`: Filter models by name prefix
- `-l, --list`: List models (same as running without flags)

#### Examples

```bash
# List all models
quicbaas ollama models

# List models with name prefix filter
quicbaas ollama models --prefix nomic

# Output as JSON
quicbaas ollama models --output json
```

### model info

Retrieve detailed information about a specific Ollama model.

```bash
quicbaas ollama model info <model-name>
```

#### Examples

```bash
# Get info for nomic-embed-text model
quicbaas ollama model info nomic-embed-text

# Output as JSON
quicbaas ollama model info nomic-embed-text --output json

# Output as YAML
quicbaas ollama model info nomic-embed-text --output yaml
```

### embed

Generate an embedding vector for the provided text.

```bash
quicbaas ollama embed <text>
```

#### Flags
- `--model string`: Model to use for embedding (uses default if not specified)

#### Examples

```bash
# Generate embedding using default model
quicbaas ollama embed "Hello, world!"

# Generate embedding using specific model
quicbaas ollama embed "Hello, world!" --model nomic-embed-text

# Output as JSON
quicbaas ollama embed "Hello, world!" --output json
```

### embed-batch

Generate embedding vectors for multiple texts in a single request.

```bash
quicbaas ollama embed-batch
```

#### Flags
- `--texts string`: JSON array of texts to embed (required)
- `--model string`: Model to use for embedding (uses default if not specified)

#### Examples

```bash
# Generate embeddings for multiple texts
quicbaas ollama embed-batch --texts '["Hello", "World", "Test"]'

# Generate embeddings using specific model
quicbaas ollama embed-batch --texts '["Hello", "World"]' --model nomic-embed-text

# Output as JSON
quicbaas ollama embed-batch --texts '["Hello", "World"]' --output json
```

## Global Flags

All ollama commands support these flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--timeout` | Request timeout | `30s` |
| `--output` | Output format (json, yaml, table) | `table` |
| `--tls` | Enable TLS | `false` |
| `--tls-cert` | TLS client certificate file | - |
| `--tls-key` | TLS client key file | - |
| `--tls-ca` | TLS CA certificate file | - |
| `--max-retries` | Maximum number of retry attempts | `3` |
| `--retry-backoff` | Initial retry backoff duration | `100ms` |

## Notes

- Ollama must be running and accessible from the QuicBaaS server
- Model availability depends on which models have been pulled in Ollama
- Batch operations are more efficient for generating multiple embeddings
- The default model is configured server-side
