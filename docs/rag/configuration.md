# RAG Configuration

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
