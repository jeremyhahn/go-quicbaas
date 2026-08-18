# Ollama — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/ollama/`.

QuicBaaS provides native integration with Ollama for embedding generation and model management. This integration enables RAG (Retrieval-Augmented Generation) workflows by allowing QuicBaaS to generate embeddings for text data and search for similar vectors.

## Overview

QuicBaaS acts as an Ollama API client, connecting to an external Ollama instance to generate embeddings and query model information. This separation of concerns allows:

- **QuicBaaS**: Vector storage, similarity search, RAG context building
- **Ollama**: Model inference, embedding generation, GPU handling
- **go-trusted-platform** (optional): Container orchestration, attestation

## Features

- **Embedding Generation**: Generate embeddings for single texts or batches
- **Model Management**: List available models, get model details
- **Health Checks**: Monitor Ollama connectivity and availability
- **MCP Tools**: Full MCP tool support for AI agent integration

## Quick Start

### 1. Configure Ollama

Add Ollama configuration to your QuicBaaS config file:

```yaml
ollama:
  enabled: true
  base_url: "http://localhost:11434"
  default_embedding_model: "nomic-embed-text"
  timeout: "120s"
  max_retries: 3
  health_check_interval: "30s"
```

### 2. Start Ollama

Ensure Ollama is running with an embedding model:

```bash
ollama pull nomic-embed-text
ollama serve
```

### 3. Use the API

Generate embeddings via REST:

```bash
curl -X POST http://localhost:8080/v1/ollama/embed \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello world"}'
```

Or via MCP tools:

```json
{
  "tool": "quicbaas_ollama_embed",
  "arguments": {
    "text": "Hello world"
  }
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Application                           │
│   (Claude Code, oterm, custom AI agents)                    │
└────────────────────────────┬────────────────────────────────┘
                             │
                             │ MCP / REST / gRPC
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                        QuicBaaS                              │
│                                                              │
│  ┌───────────────┐  ┌───────────────┐  ┌──────────────────┐ │
│  │ Ollama Client │  │ Vector Search │  │ RAG Context      │ │
│  │               │──│               │──│ Builder          │ │
│  │ - Embeddings  │  │ - HNSW Index  │  │ - Templating     │ │
│  │ - Model Info  │  │ - Similarity  │  │ - Token Counting │ │
│  │ - Health      │  │ - Hybrid      │  │ - Citations      │ │
│  └───────────────┘  └───────────────┘  └──────────────────┘ │
└────────────────────────────┬────────────────────────────────┘
                             │
                             │ HTTP API
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                        Ollama                                │
│                                                              │
│  • nomic-embed-text    • llama3.2    • Other models         │
│  • GPU acceleration    • CPU fallback                        │
└─────────────────────────────────────────────────────────────┘
```

## MCP Tools

QuicBaaS exposes 5 Ollama-related MCP tools:

| Tool | Description |
|------|-------------|
| `quicbaas_ollama_embed` | Generate embedding for a single text |
| `quicbaas_ollama_embed_batch` | Generate embeddings for multiple texts |
| `quicbaas_ollama_model_list` | List available Ollama models |
| `quicbaas_ollama_model_info` | Get detailed information about a model |
| `quicbaas_ollama_health` | Check Ollama connectivity |

See [MCP Tools Reference](../mcp/ollama-tools.md) for detailed usage.

## Service Layer

The Ollama integration provides a service interface for programmatic access:

```go
type OllamaService interface {
    Embed(ctx context.Context, req *OllamaEmbedRequest) (*OllamaEmbedResponse, error)
    EmbedWithModel(ctx context.Context, req *OllamaEmbedWithModelRequest) (*OllamaEmbedResponse, error)
    EmbedBatch(ctx context.Context, req *OllamaEmbedBatchRequest) (*OllamaEmbedBatchResponse, error)
    ListModels(ctx context.Context, req *OllamaListModelsRequest) (*OllamaListModelsResponse, error)
    GetModelInfo(ctx context.Context, req *OllamaGetModelInfoRequest) (*OllamaGetModelInfoResponse, error)
    Health(ctx context.Context, req *OllamaHealthRequest) (*OllamaHealthResponse, error)
    IsAvailable() bool
    DefaultModel() string
    BaseURL() string
}
```

## Next Steps

- [Configuration Guide](configuration.md) - Detailed configuration options
- [API Reference](api-reference.md) - REST API documentation
- [RAG Integration](../rag/README.md) - Building RAG context with embeddings
