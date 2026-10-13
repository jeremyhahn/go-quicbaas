# RAG — Engineering

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Implementation contract: exported surface, invariants, errors, configuration, tests. Feature folder: `docs/rag/`.

## Factory Configuration

The `rag.NewSystem()` function accepts a `config.RAGConfig` struct containing all pipeline settings.

### Embedding Configuration

```go
type EmbeddingConfig struct {
    Provider   string        // ollama, lmstudio, openai, voyage
    APIURL     string        // Provider API endpoint
    APIKey     string        // API key (required for openai, voyage)
    Model      string        // Model name
    Dimensions int           // Embedding dimensions
    Timeout    time.Duration // Request timeout
}
```

| Field | Description | Default |
|-------|-------------|---------|
| `Provider` | Embedding provider name | Required |
| `APIURL` | Provider endpoint URL | Provider-specific |
| `APIKey` | Authentication key | Empty (local providers) |
| `Model` | Model identifier | Provider-specific |
| `Dimensions` | Vector dimensions | Auto-detected |
| `Timeout` | HTTP timeout | 30s |

#### Endpoint resolution

`APIURL` resolves through the usual layers: flag, then environment
(`QUICBAAS_RAG_EMBEDDING_API_URL`), then the config file
(`rag.embedding.api_url`), then the defaults layer. The defaults layer is
`pkg/config/upstream_defaults.go`, which states each built-in provider's
endpoint once (`DefaultEmbeddingAPIURL`):

| Provider | Default endpoint |
|----------|------------------|
| `ollama` | `http://localhost:11434` |
| `lmstudio` | `http://localhost:1234` |
| `openai` | `https://api.openai.com` |
| `voyage` | `https://api.voyageai.com/v1/embeddings` |

`EmbeddingConfig.ResolveAPIURL` returns the configured value or the provider
default. The RAG factory and the server's embedding wiring both call it. A
provider with no configured endpoint and no default fails with
`*config.UpstreamEndpointMissingError` (`errors.Is` matches
`config.ErrUpstreamEndpointMissing`). The error names the key to set. When RAG
is not configured, the server's embedding manager dials `ollama.base_url`
instead.

### Vector Configuration

```go
type RAGVectorConfig struct {
    Dimensions   int    // Must match embedding dimensions
    NumCentroids int    // IVF clustering parameter
    NProbe       int    // Search probes
    Metric       string // cosine, l2, ip
}
```

| Field | Description | Default |
|-------|-------------|---------|
| `Dimensions` | Vector dimensions | Required |
| `NumCentroids` | IVF cluster count | 100 |
| `NProbe` | Clusters to search | 10 |
| `Metric` | Distance metric | cosine |

### Ranking Configuration

```go
type RankingConfig struct {
    Strategy      string  // hybrid, vector, keyword
    VectorWeight  float32 // Vector score weight (0-1)
    KeywordWeight float32 // BM25 score weight (0-1)
    BM25K1        float32 // BM25 k1 parameter
    BM25B         float32 // BM25 b parameter
    EnableRerank  bool    // Enable cross-encoder reranking
    RerankTopK    int     // Candidates for reranking
    RRFK          int     // RRF constant (typically 60)
}
```

| Field | Description | Default |
|-------|-------------|---------|
| `Strategy` | Ranking strategy | hybrid |
| `VectorWeight` | Vector score weight | 0.7 |
| `KeywordWeight` | BM25 weight | 0.3 |
| `BM25K1` | Term saturation | 1.2 |
| `BM25B` | Length normalization | 0.75 |
| `EnableRerank` | Cross-encoder rerank | true |
| `RerankTopK` | Rerank candidates | 50 |
| `RRFK` | RRF smoothing constant | 60 |

### Context Configuration

```go
type ContextConfig struct {
    MaxTokens          int    // Maximum context tokens
    DefaultTemplate    string // markdown, json, xml, plain
    TruncationStrategy string // smart, hard, none
    IncludeCitations   bool   // Add source citations
    MaxResultsPerChunk int    // Results per context chunk
    TokenMultiplier    float32 // Token estimation multiplier
}
```

| Field | Description | Default |
|-------|-------------|---------|
| `MaxTokens` | Token limit | 8000 |
| `DefaultTemplate` | Output format | markdown |
| `TruncationStrategy` | Truncation method | smart |
| `IncludeCitations` | Source citations | true |
| `MaxResultsPerChunk` | Results per chunk | 10 |
| `TokenMultiplier` | Estimation factor | 1.3 |

### Pipeline Configuration

```go
type PipelineConfig struct {
    EnableCircuitBreaker bool
    EnableMetrics        bool
    MaxConcurrentQueries int
    Timeout              time.Duration
    DefaultTopK          int
    DefaultMinScore      float32
    DefaultMaxTokens     int
    DefaultNamespace     string
    EnableHybrid         bool
    EnableRerank         bool
}
```

| Field | Description | Default |
|-------|-------------|---------|
| `EnableCircuitBreaker` | Fault tolerance | true |
| `EnableMetrics` | Observability | true |
| `MaxConcurrentQueries` | Concurrency limit | 100 |
| `Timeout` | Query timeout | 30s |
| `DefaultTopK` | Default result count | 10 |
| `DefaultMinScore` | Minimum score | 0.7 |
| `DefaultMaxTokens` | Context token limit | 8000 |
| `DefaultNamespace` | Vector namespace | default |

## Provider Selection

### Ollama (Local)

```go
cfg := config.EmbeddingConfig{
    Provider:   "ollama",
    APIURL:     "http://localhost:11434",
    Model:      "nomic-embed-text",
    Dimensions: 768,
}
```

### LMStudio (Local)

```go
cfg := config.EmbeddingConfig{
    Provider:   "lmstudio",
    APIURL:     "http://localhost:1234",
    Model:      "nomic-ai/nomic-embed-text-v1.5-GGUF",
    Dimensions: 768,
}
```

### OpenAI

```go
cfg := config.EmbeddingConfig{
    Provider:   "openai",
    APIURL:     "https://api.openai.com",
    APIKey:     os.Getenv("OPENAI_API_KEY"),
    Model:      "text-embedding-3-small",
    Dimensions: 1536,
}
```

### Voyage AI

```go
cfg := config.EmbeddingConfig{
    Provider:   "voyage",
    APIURL:     "https://api.voyageai.com/v1/embeddings",
    APIKey:     os.Getenv("VOYAGE_API_KEY"),
    Model:      "voyage-code-2",
    Dimensions: 1536,
}
```

## Query Options

Per-query settings override defaults:

```go
opts := pipeline.QueryOptions{
    // Retrieval
    TopK:      10,
    MinScore:  0.7,
    Namespace: "documents",
    Filters:   map[string]interface{}{"type": "code"},

    // Ranking
    EnableHybrid:  true,
    EnableRerank:  true,
    RerankTopK:    50,
    VectorWeight:  0.7,
    KeywordWeight: 0.3,

    // Context
    MaxTokens:     8000,
    IncludeSource: true,
    Template:      "markdown",
    Format:        "markdown",

    // Performance
    UseCache: true,
    Timeout:  30 * time.Second,
    Parallel: true,
}
```

## Configuration Builder

Use the builder pattern for pipeline config:

```go
cfg := pipeline.NewConfigBuilder().
    WithHybrid(true).
    WithRerank(true).
    WithDefaultTopK(10).
    WithDefaultMaxTokens(8000).
    WithQueryTimeout(30 * time.Second).
    WithCircuitBreaker(true).
    WithMetrics(true).
    Build()
```


---

## Auto-Indexing

Automatic code indexing pipeline: watch files, chunk by language, embed, store vectors.

Source: `pkg/rag/indexing/service.go`, `pkg/rag/indexing/pipeline.go`, `pkg/rag/indexing/config.go`

Build tag: `rag` (all indexing code requires `//go:build rag`)

### Pipeline Architecture

```
File System Watcher
    |
    v
[Event: create/modify/delete/rename]
    |
    v
IndexingService.eventLoop()
    |
    v
IndexingPipeline
    |
    +-- 1. Read file content
    +-- 2. SHA-256 checksum
    +-- 3. Check metadata store (incremental skip if unchanged)
    +-- 4. Detect language (Go, Markdown, etc.)
    +-- 5. Select chunker (language-specific)
    +-- 6. Chunk content
    +-- 7. Batch embed via embedding.Provider
    +-- 8. Remove old vectors (if re-indexing)
    +-- 9. Add new vectors to index
    +-- 10. Update metadata store
```

### Triggers

| Trigger | Constant | Description |
|---------|----------|-------------|
| `file_change` | `TriggerFileChange` | Index on filesystem create/modify/delete/rename events |
| `iteration_complete` | `TriggerIterationComplete` | Index at the end of a workflow iteration cycle |
| `manual` | `TriggerManual` | Index only on explicit `TriggerReindex()` or `IndexFiles()` call |

Default trigger: `file_change`.

### Incremental Indexing

The pipeline computes a SHA-256 checksum for every file. Before chunking and embedding, it checks the metadata store for the file's previous checksum. If the checksum matches, the file is skipped entirely.

This makes re-indexing cheap: only changed files are re-processed.

### Language-Specific Chunking

| Language | Chunker | Strategy |
|----------|---------|----------|
| Go | `GoCodeChunker` | Function-level chunks, preserves imports/comments/package |
| Markdown | `MarkdownChunker` | Header-based sections, preserves code blocks |
| Other | `TextChunker` | Sentence/paragraph-aware splitting with configurable overlap |

### IndexingService

The service coordinates the watcher and pipeline:

- `Start(ctx)` -- Begin watching and processing events
- `Stop()` -- Graceful shutdown
- `TriggerReindex(ctx)` -- Re-index all tracked files
- `IndexFiles(ctx, paths)` -- Index specific files
- `Stats()` -- Lock-free statistics snapshot
- `Events()` -- Observable event channel (capacity 256)

Stats use `atomic.Int64` for lock-free reads:

| Stat | Description |
|------|-------------|
| `FilesIndexed` | Total files processed |
| `TotalChunks` | Total chunks created |
| `TotalVectors` | Total vectors stored |
| `IndexBackend` | Current backend name |
| `LastIndexedAt` | Unix timestamp of last indexing |
| `IsRunning` | Whether the service is active |

### Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | `bool` | `true` | Enable/disable indexing |
| `auto_index` | `bool` | `true` | Auto-index on file changes |
| `index_type` | `string` | `"auto"` | Vector backend: `auto`, `memory`, `hnsw` |
| `gpu_accelerated` | `bool` | `false` | Request GPU acceleration |
| `triggers` | `[]string` | `["file_change"]` | Events that trigger indexing |
| `batch_size` | `int` | `32` | Files per embedding batch |
| `max_concurrency` | `int` | `runtime.NumCPU()` | Concurrent indexing operations |
| `watch_exclusions` | `[]string` | `.git/`, `node_modules/`, `vendor/`, `*.tmp`, `*.log`, `.DS_Store`, `*.swp` | Glob patterns to exclude |
| `chunk_max_tokens` | `int` | `2000` | Max tokens per chunk |
| `chunk_overlap` | `int` | `200` | Overlapping tokens between chunks (0 = no overlap) |
| `project_id` | `string` | - | Project identifier for namespacing |
| `project_path` | `string` | - | Root directory to watch and index |
| `namespace` | `string` | `project_id` | Scope for metadata and vector storage |

### Example Configuration

```yaml
settings:
  vector_db:
    enabled: true
    auto_index: true
    index_type: auto
    gpu_accelerated: false
    triggers:
      - file_change
      - iteration_complete
```

### CPU-First Design

The indexing pipeline is designed to run on CPU by default. GPU acceleration is a future enhancement (Phase 10). The `gpu_accelerated: true` setting currently returns `GPUUnavailableError`.

Concurrency defaults to `runtime.NumCPU()`, and embedding batches are sized at 32 files to balance throughput with memory usage.


---

## Vector Backends

Vector index backend selection for the RAG auto-indexing pipeline.

Source: `pkg/rag/indexing/vector_adapter.go`

Build tag: `rag`

### Backend Selection

The `NewVectorIndexFromConfig()` function selects a vector index backend based on configuration:

| Backend | Config Value | Description | Status |
|---------|-------------|-------------|--------|
| Auto | `"auto"` | Try HNSW, fall back to memory | Currently resolves to memory |
| Memory | `"memory"` | In-memory brute-force search | Available |
| HNSW | `"hnsw"` | Hierarchical Navigable Small World graph | Falls back to memory (Phase 10) |
| GPU CAGRA | `"gpu_cagra"` | GPU-accelerated approximate nearest neighbor | Not available (`GPUUnavailableError`) |

### Auto-Detection Logic

```
NewVectorIndexFromConfig(cfg, dimensions)
    |
    +-- gpu_accelerated=true? --> GPUUnavailableError
    |
    +-- index_type="auto"?   --> try HNSW, fall back to memory
    +-- index_type="memory"? --> in-memory brute-force
    +-- index_type="hnsw"?   --> try HNSW, fall back to memory
    +-- unknown?             --> UnsupportedIndexTypeError
```

The handler map provides O(1) dispatch:

```go
var indexTypeHandlers = map[string]func(cfg IndexingConfig, dimensions int) (rag.VectorIndex, error){
    "auto":   newAutoIndex,
    "memory": newMemoryIndex,
    "hnsw":   newHNSWOrMemoryIndex,
}
```

### Build Tags

| Tag | Effect |
|-----|--------|
| `rag` | Enables the indexing package (required) |
| `rag` + `qrdb_vector` | Enables QRDB-backed vector storage (future) |

Without the `rag` build tag, the entire `pkg/rag/indexing/` package is excluded from compilation.

### Performance Characteristics

| Backend | Search | Insert | Memory | Accuracy | Best For |
|---------|--------|--------|--------|----------|----------|
| Memory (brute-force) | O(n) | O(1) | Linear | Exact (100%) | Small indices (<100K vectors) |
| HNSW | O(log n) | O(log n) | ~1.5x | Approximate (~95-99%) | Medium indices (100K-10M) |
| GPU CAGRA | O(1) amortized | O(1) batched | GPU VRAM | Approximate (~95-99%) | Large indices (>10M) |

### Configuration

```yaml
settings:
  vector_db:
    index_type: auto         # auto | memory | hnsw
    gpu_accelerated: false   # true requires GPU hardware + build tags
```

### Current State

As of the current implementation:

- `auto` and `hnsw` both resolve to the in-memory brute-force index
- GPU acceleration returns `GPUUnavailableError`
- The HNSW integration is planned for Phase 10 via the `qrdb` package
- All backends use cosine distance (`rag.CosineDistance`) as the default metric
