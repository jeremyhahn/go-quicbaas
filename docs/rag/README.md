# RAG Pipeline

The RAG (Retrieval-Augmented Generation) Pipeline in QuicBaaS provides a complete system for semantic search and context generation. It integrates vector search, hybrid ranking, and context building for LLM applications.

## Features

- **Multiple Embedding Providers**: Ollama, LMStudio, OpenAI, Voyage AI
- **Hybrid Search**: Combines vector similarity with BM25 keyword scoring
- **RRF Fusion**: Reciprocal Rank Fusion for result combination
- **Cross-Encoder Reranking**: Optional reranking for improved relevance
- **Context Building**: Token-aware context formatting with templates
- **Circuit Breaker**: Resilience pattern for fault tolerance
- **Metrics**: Built-in observability and performance tracking

## Go API

### Quick Start

```go
import (
    "context"
    "time"

    "github.com/jeremyhahn/go-quicbaas/pkg/config"
    "github.com/jeremyhahn/go-quicbaas/pkg/rag"
    "github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// Configure the RAG system
cfg := config.RAGConfig{
    Embedding: config.EmbeddingConfig{
        Provider:   "ollama",
        APIURL:     "http://localhost:11434",
        Model:      "nomic-embed-text",
        Dimensions: 768,
    },
    Vector: config.RAGVectorConfig{
        Dimensions:   768,
        NumCentroids: 100,
        NProbe:       10,
        Metric:       "cosine",
    },
    Ranking: config.RankingConfig{
        Strategy:      "hybrid",
        VectorWeight:  0.7,
        KeywordWeight: 0.3,
        EnableRerank:  true,
    },
    Pipeline: config.PipelineConfig{
        MaxConcurrentQueries: 100,
        Timeout:              30 * time.Second,
        DefaultTopK:          10,
    },
}

// Create the system
system, err := rag.NewSystem(cfg)
if err != nil {
    log.Fatal(err)
}
defer system.Close()

// Index content
ctx := context.Background()
err = system.IndexContent(ctx, "doc1", "Your document content", nil)

// Query
response, err := system.Query(ctx, "search query", pipeline.QueryOptions{
    TopK:     10,
    MinScore: 0.7,
})

fmt.Printf("Context: %s\n", response.Context)
fmt.Printf("Found %d results in %v\n", len(response.Results), response.ProcessingTime)
```

### Build Tags

The RAG pipeline requires the `rag` build tag:

```bash
go build -tags rag ./...
go test -tags rag ./pkg/rag/...
```

### Components

| Component | Description |
|-----------|-------------|
| `rag.System` | Main entry point, orchestrates all components |
| `pipeline.Pipeline` | Query execution orchestrator |
| `adapters.RankerAdapter` | Bridges ranking to pipeline interface |
| `adapters.ContextBuilderAdapter` | Bridges context builder to pipeline |
| `ranking.HybridRanker` | BM25 + vector fusion |
| `context.Builder` | Token-aware context formatting |

## MCP Tools

The RAG pipeline is also exposed via MCP (Model Context Protocol) tools for LLM integration.

### Overview

The MCP interface enables:

- **All-in-One RAG Query**: Single tool to embed query, search vectors, and build context
- **Context Assembly**: Intelligent token-aware context building from search results
- **Multiple Output Formats**: Markdown, JSON, XML, or plain text
- **Citation Tracking**: Automatic source citations for transparency
- **Smart Truncation**: Intelligent content truncation when context exceeds limits

## Quick Start

### Using the All-in-One RAG Tool (Recommended)

The simplest way to perform RAG is using the `quicbaas_rag_query` tool:

```json
{
  "tool": "quicbaas_rag_query",
  "arguments": {
    "query": "How does the authentication system work?",
    "namespace": "codebase",
    "top_k": 10,
    "max_tokens": 4000,
    "template": "markdown",
    "include_citations": "true"
  }
}
```

This performs:
1. Generates an embedding for the query using Ollama
2. Searches the vector namespace for similar content
3. Assembles the results into LLM-ready context

**Response:**

```json
{
  "context": "## Relevant Code\n\n[1] src/auth/handler.go:45-78\n```go\nfunc HandleLogin(w http.ResponseWriter, r *http.Request) {\n    // Authentication logic here\n}\n```\n\n[2] src/auth/middleware.go:12-35\n...",
  "tokens": 3500,
  "truncated": false,
  "result_count": 10,
  "citations": [
    {"index": 1, "file_path": "src/auth/handler.go", "start_line": 45, "end_line": 78, "score": 0.92},
    {"index": 2, "file_path": "src/auth/middleware.go", "start_line": 12, "end_line": 35, "score": 0.88}
  ]
}
```

### Using Custom Search Workflow

For advanced use cases, you can perform the search step separately and use `quicbaas_rag_build_context`:

```json
{
  "tool": "quicbaas_rag_build_context",
  "arguments": {
    "results": "[{\"content\": \"...\", \"score\": 0.95, \"metadata\": {...}}]",
    "max_tokens": 4000,
    "template": "json"
  }
}
```

## RAG Workflow

```
┌─────────────────────────────────────────────────────────────┐
│                      User Query                              │
│         "How does the authentication system work?"           │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                   1. Generate Embedding                      │
│                                                              │
│     Query ──► Ollama ──► [0.12, -0.45, 0.78, ...]           │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                   2. Vector Search                           │
│                                                              │
│     Embedding ──► HNSW Index ──► Top-K Similar Chunks       │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                   3. Build Context                           │
│                                                              │
│     Chunks ──► Score/Rank ──► Template ──► LLM Context      │
└────────────────────────────┬────────────────────────────────┘
                             │
                             ▼
┌─────────────────────────────────────────────────────────────┐
│                   Assembled Context                          │
│                                                              │
│     "## Relevant Code\n[1] src/auth/handler.go..."          │
└─────────────────────────────────────────────────────────────┘
```

## MCP Tools

### quicbaas_rag_query

All-in-one RAG query tool. **Recommended for most use cases.**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `query` | string | Yes | The search query text |
| `namespace` | string | Yes | Vector namespace to search |
| `top_k` | integer | No | Number of results (default: 10) |
| `max_tokens` | integer | No | Max tokens for context (default: 4000) |
| `min_score` | string | No | Minimum similarity score (default: "0.7") |
| `template` | string | No | Output format: markdown, json, xml, plain |
| `include_citations` | string | No | Include source citations (default: "true") |
| `model` | string | No | Embedding model (uses default if not specified) |

### quicbaas_rag_build_context

Assemble context from user-provided search results.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `results` | string | Yes | JSON array of result objects |
| `max_tokens` | integer | No | Max tokens for context (default: 4000) |
| `template` | string | No | Output format: markdown, json, xml, plain |
| `include_citations` | string | No | Include source citations (default: "true") |
| `truncation_strategy` | string | No | Truncation method: smart, hard, none |

## Output Templates

### Markdown (Default)

```markdown
## Relevant Context

[1] src/auth/handler.go:45-78
```go
func HandleLogin(w http.ResponseWriter, r *http.Request) {
    // Authentication logic
}
```

[2] src/auth/middleware.go:12-35
```go
func AuthMiddleware(next http.Handler) http.Handler {
    // Middleware logic
}
```
```

### JSON

```json
{
  "results": [
    {
      "index": 1,
      "file_path": "src/auth/handler.go",
      "lines": "45-78",
      "content": "func HandleLogin..."
    }
  ]
}
```

### XML

```xml
<context>
  <result index="1" file="src/auth/handler.go" lines="45-78">
    <content>func HandleLogin...</content>
  </result>
</context>
```

### Plain

```
[1] src/auth/handler.go:45-78
func HandleLogin(w http.ResponseWriter, r *http.Request) {
    // Authentication logic
}

[2] src/auth/middleware.go:12-35
...
```

## Truncation Strategies

When the assembled context exceeds `max_tokens`:

| Strategy | Description |
|----------|-------------|
| `smart` | Prioritizes high-scoring results, truncates low-scoring ones first |
| `hard` | Simple truncation at token limit |
| `none` | No truncation (may exceed token limit) |

## Configuration

The RAG context builder uses these defaults:

```go
config := ragctx.Config{
    DefaultMaxTokens:   4000,
    DefaultTemplate:    ragctx.TemplateMarkdown,
    IncludeCitations:   true,
    TruncationStrategy: ragctx.TruncationSmart,
    MaxResultsPerChunk: 20,
    TokenMultiplier:    1.3,
}
```

## Best Practices

1. **Use quicbaas_rag_query**: The all-in-one tool handles the entire pipeline
2. **Set appropriate max_tokens**: Consider your LLM's context window
3. **Use citations**: Helps LLMs cite sources in responses
4. **Choose the right template**: Markdown works well for most LLMs
5. **Tune min_score**: Higher values = more relevant but fewer results

## Integration Example

Using with Claude Code:

```
User: "How does our authentication system work?"

Claude Code:
1. Calls quicbaas_rag_query(query="authentication system", namespace="codebase")
2. Receives assembled context with relevant code snippets
3. Uses context to generate accurate, sourced response
```

## Further Reading

| Document | Description |
|----------|-------------|
| [Architecture](architecture.md) | Pipeline components, data flow, adapter pattern |
| [Configuration](configuration.md) | Detailed configuration reference |
| [Embedding Providers](../embedding/README.md) | Embedding provider setup |
| [Provider Reference](../embedding/providers.md) | Model dimensions and specifications |
