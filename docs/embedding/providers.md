# Embedding Providers

## Ollama Provider

Local embedding generation using Ollama.

### Setup

```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Pull embedding model
ollama pull nomic-embed-text
```

### Configuration

```go
cfg := embedding.OllamaConfig{
    BaseURL:      "http://localhost:11434",
    Model:        "nomic-embed-text",
    Dimensions:   0,  // Auto-detect from model
    Timeout:      120 * time.Second,
    MaxRetries:   3,
    RetryBackoff: 1 * time.Second,
}

provider, err := embedding.NewOllamaProvider(cfg)
```

### Supported Models

| Model | Dimensions | Max Tokens |
|-------|------------|------------|
| nomic-embed-text | 768 | 8192 |
| mxbai-embed-large | 1024 | 8192 |
| all-minilm | 384 | 8192 |
| all-minilm-l6-v2 | 384 | 8192 |
| all-minilm-l12-v2 | 384 | 8192 |
| bge-small-en-v1.5 | 384 | 8192 |
| bge-base-en-v1.5 | 768 | 8192 |
| bge-large-en-v1.5 | 1024 | 8192 |
| bge-m3 | 1024 | 8192 |
| snowflake-arctic-embed | 1024 | 8192 |
| e5-small-v2 | 384 | 8192 |
| e5-base-v2 | 768 | 8192 |
| e5-large-v2 | 1024 | 8192 |

## LMStudio Provider

Local embedding using LMStudio's OpenAI-compatible API.

### Setup

1. Download LMStudio from https://lmstudio.ai
2. Load an embedding model
3. Start the local server (default port 1234)

### Configuration

```go
cfg := embedding.LMStudioConfig{
    BaseURL:        "http://localhost:1234",
    Model:          "nomic-ai/nomic-embed-text-v1.5-GGUF",
    Dimensions:     0,  // Auto-detect
    Timeout:        60,
    MaxRetries:     3,
    RetryBackoffMS: 1000,
}

provider, err := embedding.NewLMStudioProvider(cfg)
```

### Supported Models

| Model | Dimensions | Max Tokens |
|-------|------------|------------|
| nomic-ai/nomic-embed-text-v1.5-GGUF | 768 | 8192 |
| BAAI/bge-small-en-v1.5-GGUF | 384 | 8192 |
| BAAI/bge-base-en-v1.5-GGUF | 768 | 8192 |
| BAAI/bge-large-en-v1.5-GGUF | 1024 | 8192 |
| sentence-transformers/all-MiniLM-L6-v2-GGUF | 384 | 8192 |
| sentence-transformers/all-MiniLM-L12-v2-GGUF | 384 | 8192 |
| mixedbread-ai/mxbai-embed-large-v1-GGUF | 1024 | 8192 |
| intfloat/e5-small-v2-GGUF | 384 | 8192 |
| intfloat/e5-base-v2-GGUF | 768 | 8192 |
| intfloat/e5-large-v2-GGUF | 1024 | 8192 |
| Snowflake/snowflake-arctic-embed-m-v1.5-GGUF | 768 | 8192 |
| Snowflake/snowflake-arctic-embed-l-v2.0-GGUF | 1024 | 8192 |

## OpenAI Provider

Cloud embeddings via OpenAI API.

### Configuration

```go
cfg := embedding.Config{
    Provider:       "openai",
    APIURL:         "https://api.openai.com",
    APIKey:         os.Getenv("OPENAI_API_KEY"),
    Model:          "text-embedding-3-small",
    Dimensions:     1536,
    Timeout:        30,
    MaxRetries:     3,
    RetryBackoffMS: 1000,
}

provider, err := embedding.NewOpenAIProvider(cfg)
```

### Supported Models

| Model | Dimensions | Max Tokens |
|-------|------------|------------|
| text-embedding-3-small | 1536 | 8191 |
| text-embedding-3-large | 3072 | 8191 |
| text-embedding-ada-002 | 1536 | 8191 |

### Error Handling

The provider handles rate limits with exponential backoff:

```go
// Automatic retry on 429 (rate limit) and 5xx errors
// Max retries configurable, default: 3
// Backoff: 1s, 2s, 4s (exponential)
```

## Voyage AI Provider

Specialized embeddings for code and text.

### Configuration

```go
cfg := embedding.Config{
    Provider:       "voyage",
    APIURL:         "https://api.voyageai.com/v1/embeddings",
    APIKey:         os.Getenv("VOYAGE_API_KEY"),
    Model:          "voyage-code-2",
    Dimensions:     1536,
    Timeout:        30,
    MaxRetries:     3,
    RetryBackoffMS: 1000,
}

provider, err := embedding.NewVoyageProvider(cfg)
```

### Supported Models

| Model | Dimensions | Max Tokens | Use Case |
|-------|------------|------------|----------|
| voyage-code-2 | 1536 | 32000 | Code embeddings |
| voyage-2 | 1024 | 16000 | General text |
| voyage-large-2 | 1536 | 16000 | High quality |

## Model Dimension Reference

### 384 Dimensions

| Provider | Model |
|----------|-------|
| Ollama | all-minilm, all-minilm-l6-v2, all-minilm-l12-v2 |
| Ollama | bge-small-en-v1.5, e5-small-v2 |
| LMStudio | BAAI/bge-small-en-v1.5-GGUF |
| LMStudio | sentence-transformers/all-MiniLM-* |
| LMStudio | intfloat/e5-small-v2-GGUF |

### 768 Dimensions

| Provider | Model |
|----------|-------|
| Ollama | nomic-embed-text, bge-base-en-v1.5, e5-base-v2 |
| LMStudio | nomic-ai/nomic-embed-text-v1.5-GGUF |
| LMStudio | BAAI/bge-base-en-v1.5-GGUF |
| LMStudio | Snowflake/snowflake-arctic-embed-m-v1.5-GGUF |

### 1024 Dimensions

| Provider | Model |
|----------|-------|
| Ollama | mxbai-embed-large, bge-large-en-v1.5, bge-m3 |
| Ollama | snowflake-arctic-embed, e5-large-v2 |
| LMStudio | BAAI/bge-large-en-v1.5-GGUF |
| LMStudio | mixedbread-ai/mxbai-embed-large-v1-GGUF |
| Voyage | voyage-2 |

### 1536 Dimensions

| Provider | Model |
|----------|-------|
| OpenAI | text-embedding-3-small, text-embedding-ada-002 |
| Voyage | voyage-code-2, voyage-large-2 |

### 3072 Dimensions

| Provider | Model |
|----------|-------|
| OpenAI | text-embedding-3-large |

## Batch Embedding

All providers support batch embedding for efficiency:

```go
texts := []string{"Hello", "World", "Test"}
embeddings, err := provider.EmbedBatch(ctx, texts)

// embeddings[0] corresponds to "Hello"
// embeddings[1] corresponds to "World"
// embeddings[2] corresponds to "Test"
```

## Dimension Auto-Detection

When `Dimensions` is set to 0, providers auto-detect from:
1. Known model specifications
2. First embedding response

```go
cfg := embedding.OllamaConfig{
    Model:      "nomic-embed-text",
    Dimensions: 0, // Will be set to 768 automatically
}
```
