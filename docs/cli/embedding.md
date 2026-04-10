# quicbaas embedding

Embedding generation and management commands for QuicBaaS.

Aliases: `embed`, `emb`

## Overview

These commands allow you to generate embeddings from text, manage embedding providers, and control the embedding cache. All commands connect to a running QuicBaaS server.

Supported providers:
- **voyage**: Voyage AI embedding models
- **openai**: OpenAI embedding models
- **ollama**: Local Ollama embedding models
- **lmstudio**: Local LM Studio embedding models

## Embedding Generation

### generate

Generate an embedding vector for a single text input.

```bash
quicbaas embedding generate <text>
```

#### Flags
- `--provider string`: Embedding provider (voyage, openai, ollama, lmstudio)
- `--metadata string`: Metadata as JSON string
- `--input-type string`: Input type hint (document, query, code)
- `--source string`: Source identifier (file path, URL, etc.)
- `--language string`: Programming or natural language
- `--cache-bypass`: Bypass cache and force regeneration

#### Examples

```bash
# Generate embedding with default provider
quicbaas embedding generate "Hello, world!"

# Generate with specific provider
quicbaas embedding generate "Sample text" --provider openai

# Generate with metadata
quicbaas embedding generate "Document content" --metadata '{"doc_id":"123","type":"article"}'

# Generate with input type hint
quicbaas embedding generate "search query" --input-type query

# Bypass cache and force regeneration
quicbaas embedding generate "Hello" --cache-bypass
```

### batch

Generate embedding vectors for multiple texts in a single request.

```bash
quicbaas embedding batch <texts-json>
```

#### Flags
- `--provider string`: Embedding provider (voyage, openai, ollama, lmstudio)
- `--cache-bypass`: Bypass cache and force regeneration

#### Input Formats

Simple JSON array of strings:
```json
["text1", "text2", "text3"]
```

Full item specification:
```json
[{"id":"doc1","text":"Hello"},{"id":"doc2","text":"World"}]
```

#### Examples

```bash
# Batch generate from JSON array
quicbaas embedding batch '["text1", "text2", "text3"]'

# Batch generate with full item specification
quicbaas embedding batch '[{"id":"doc1","text":"Hello"},{"id":"doc2","text":"World"}]'

# Batch generate from file
quicbaas embedding batch @texts.json

# Batch with specific provider
quicbaas embedding batch '["text1", "text2"]' --provider voyage
```

## Provider Management

### providers

List all available embedding providers.

```bash
quicbaas embedding providers
```

#### Flags
- `--include-unavailable`: Include unavailable providers in listing

#### Examples

```bash
# List available providers
quicbaas embedding providers

# Include unavailable providers
quicbaas embedding providers --include-unavailable
```

### provider info

Get detailed information about a specific provider.

```bash
quicbaas embedding provider info <name>
```

#### Examples

```bash
quicbaas embedding provider info voyage
quicbaas embedding provider info openai
```

### provider set-default

Set the default embedding provider.

```bash
quicbaas embedding provider set-default <name>
```

#### Examples

```bash
quicbaas embedding provider set-default voyage
quicbaas embedding provider set-default openai
```

## Cache Management

### cache stats

Get embedding cache statistics.

```bash
quicbaas embedding cache stats
```

#### Examples

```bash
quicbaas embedding cache stats
quicbaas embedding cache stats --output json
```

### cache clear

Clear the embedding cache.

```bash
quicbaas embedding cache clear
```

#### Flags
- `--provider string`: Clear cache only for this provider (clears all if not specified)

#### Examples

```bash
# Clear entire cache
quicbaas embedding cache clear

# Clear cache for specific provider
quicbaas embedding cache clear --provider voyage
```

## Notes

- Embeddings are cached by default for subsequent requests
- Use `--cache-bypass` to force regeneration when needed
- Batch operations are more efficient than individual requests
- Provider availability depends on server configuration and API keys
