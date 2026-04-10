# Ollama API Reference

This document describes the REST API endpoints for Ollama integration.

## Endpoints

### Generate Embedding

Generate an embedding vector for a single text.

```
POST /v1/ollama/embed
```

**Request Body:**

```json
{
  "text": "The text to embed",
  "model": "nomic-embed-text"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `text` | string | Yes | The text to generate an embedding for |
| `model` | string | No | Model to use (defaults to configured default) |

**Response:**

```json
{
  "embedding": [0.123, -0.456, ...],
  "model": "nomic-embed-text",
  "dimensions": 768,
  "processing_time": "125ms"
}
```

**Example:**

```bash
curl -X POST http://localhost:8080/v1/ollama/embed \
  -H "Content-Type: application/json" \
  -d '{"text": "What is machine learning?"}'
```

### Batch Embed

Generate embeddings for multiple texts in a single request.

```
POST /v1/ollama/embed/batch
```

**Request Body:**

```json
{
  "texts": ["First text", "Second text", "Third text"],
  "model": "nomic-embed-text"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `texts` | string[] | Yes | Array of texts to embed |
| `model` | string | No | Model to use (defaults to configured default) |

**Response:**

```json
{
  "embeddings": [[0.123, -0.456, ...], [0.789, -0.012, ...]],
  "model": "nomic-embed-text",
  "dimensions": 768,
  "count": 2,
  "processing_time": "250ms"
}
```

### List Models

List all available models from Ollama.

```
GET /v1/ollama/models
```

**Response:**

```json
{
  "models": [
    {
      "name": "nomic-embed-text:latest",
      "size": 274000000,
      "digest": "sha256:abc123...",
      "modified_at": "2025-01-10T15:30:00Z"
    },
    {
      "name": "llama3.2:latest",
      "size": 4700000000,
      "digest": "sha256:def456...",
      "modified_at": "2025-01-09T10:00:00Z"
    }
  ],
  "count": 2
}
```

### Get Model Info

Get detailed information about a specific model.

```
GET /v1/ollama/models/{model}
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `model` | string | Model name (e.g., `nomic-embed-text`) |

**Response:**

```json
{
  "model": "nomic-embed-text",
  "details": {
    "license": "Apache-2.0",
    "format": "gguf",
    "family": "nomic",
    "parameter_size": "137M",
    "quantization_level": "Q4_0"
  }
}
```

### Health Check

Check Ollama connectivity and availability.

```
GET /v1/ollama/health
```

**Response (Healthy):**

```json
{
  "available": true,
  "version": "0.1.0",
  "base_url": "http://localhost:11434",
  "last_checked": "2025-01-12T12:00:00Z"
}
```

**Response (Unhealthy):**

```json
{
  "available": false,
  "base_url": "http://localhost:11434",
  "last_checked": "2025-01-12T12:00:00Z",
  "error": "connection refused"
}
```

## Error Responses

All endpoints return standard error responses:

```json
{
  "error": {
    "code": "OLLAMA_UNAVAILABLE",
    "message": "Ollama service is not available",
    "details": "connection refused"
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `OLLAMA_NOT_CONFIGURED` | 503 | Ollama integration is not configured |
| `OLLAMA_DISABLED` | 503 | Ollama integration is disabled |
| `OLLAMA_UNAVAILABLE` | 503 | Cannot connect to Ollama |
| `MODEL_NOT_FOUND` | 404 | Requested model not found |
| `INVALID_REQUEST` | 400 | Invalid request parameters |
| `TIMEOUT` | 504 | Request timed out |

## MCP Tools

The same functionality is available via MCP tools for AI agent integration:

| Tool | Equivalent Endpoint |
|------|---------------------|
| `quicbaas_ollama_embed` | `POST /v1/ollama/embed` |
| `quicbaas_ollama_embed_batch` | `POST /v1/ollama/embed/batch` |
| `quicbaas_ollama_model_list` | `GET /v1/ollama/models` |
| `quicbaas_ollama_model_info` | `GET /v1/ollama/models/{model}` |
| `quicbaas_ollama_health` | `GET /v1/ollama/health` |

See the MCP documentation for tool-specific usage examples.

## Rate Limiting

Ollama API calls are not rate-limited by QuicBaaS, but may be limited by your Ollama instance's capacity. Consider batch embedding for high-volume workloads.

## Performance Considerations

- **Embedding Generation**: Typical latency is 50-200ms for single texts
- **Batch Processing**: More efficient for multiple texts (reduced round-trips)
- **Model Loading**: First request may take longer if the model needs to be loaded
- **GPU vs CPU**: GPU acceleration significantly improves throughput
