# RAG Pipeline Architecture

## Overview

The RAG pipeline follows a modular design with clear separation between components. Adapters bridge implementation details to pipeline interfaces.

```
Query -> QueryHandler -> VectorSearcher -> Ranker -> ContextBuilder -> Response
           |                |                |            |
           v                v                v            v
       Embedding        IVF Index      HybridRanker    Builder
       Provider                        (BM25 + RRF)   (Templates)
```

## Pipeline Components

### 1. Query Handler

Processes incoming queries and generates embeddings.

```go
type QueryHandler interface {
    Process(ctx context.Context, query string) (*ProcessedQuery, error)
    Close() error
}

type ProcessedQuery struct {
    Original   string
    Cleaned    string
    Embedding  []float32
    Intent     string
    CacheHit   bool
    CachedResp *QueryResponse
}
```

### 2. Vector Searcher

Performs similarity search against the vector index.

```go
type VectorSearcher interface {
    Search(ctx context.Context, embedding []float32, opts SearchOptions) ([]Result, error)
    Close() error
}

type SearchOptions struct {
    TopK      int
    MinScore  float32
    Namespace string
    Filters   map[string]interface{}
}
```

### 3. Ranker

Ranks and fuses results from multiple sources.

```go
type Ranker interface {
    Rank(ctx context.Context, query string, vectorResults, keywordResults []Result) ([]Result, error)
    Close() error
}
```

The `HybridRanker` implementation provides:
- **BM25 Scoring**: Term frequency-inverse document frequency scoring
- **RRF Fusion**: Reciprocal Rank Fusion combining vector and keyword results
- **Reranking**: Optional cross-encoder reranking for improved relevance

### 4. Context Builder

Builds LLM-ready context from ranked results.

```go
type ContextBuilder interface {
    Build(ctx context.Context, results []Result, opts ContextOptions) (*BuiltContext, error)
    Close() error
}

type BuiltContext struct {
    Content   string
    Tokens    int
    Truncated bool
}
```

## Adapter Pattern

Adapters bridge internal implementations to pipeline interfaces:

```
pipeline.Ranker         <-- RankerAdapter         <-- ranking.HybridRanker
pipeline.ContextBuilder <-- ContextBuilderAdapter <-- context.Builder
```

### RankerAdapter

Converts between `pipeline.Result` and `ranking.Result` types:

```go
adapter, err := adapters.NewRankerAdapter(hybridRanker,
    adapters.WithRankerConfig(&adapters.RankerConfig{
        EnableHybrid:  true,
        VectorWeight:  0.7,
        KeywordWeight: 0.3,
    }),
)
```

### ContextBuilderAdapter

Converts between `pipeline.Result` and `context.Result` types:

```go
adapter, err := adapters.NewContextBuilderAdapter(builder,
    adapters.WithContextBuilderConfig(&adapters.ContextBuilderConfig{
        DefaultMaxTokens: 8000,
        DefaultTemplate:  ragcontext.TemplateMarkdown,
        IncludeCitations: true,
    }),
)
```

## Data Flow

### Query Execution

```
1. Query received
   |
2. QueryHandler.Process()
   - Clean query text
   - Generate embedding via Provider.Embed()
   - Check cache (optional)
   |
3. VectorSearcher.Search()
   - Search IVF index with embedding
   - Apply filters and score threshold
   |
4. Ranker.Rank()
   - Score with BM25
   - Fuse with RRF (if hybrid enabled)
   - Rerank top-K (if enabled)
   |
5. ContextBuilder.Build()
   - Format results with template
   - Truncate to token limit
   - Add citations
   |
6. Return QueryResponse
```

### Indexing Flow

```
1. Content received
   |
2. Provider.Embed()
   - Generate embedding vector
   |
3. VectorIndex.Add()
   - Store vector with metadata
   |
4. Return success
```

## Resilience

### Circuit Breaker

Protects against cascading failures:

```go
type CircuitBreaker struct {
    threshold int           // Failures before opening
    timeout   time.Duration // Time before half-open
    state     atomic.Value  // closed/open/half-open
}
```

States:
- **Closed**: Normal operation, counting failures
- **Open**: Rejecting requests after threshold exceeded
- **Half-Open**: Testing with limited requests after timeout

### Concurrency Control

Semaphore limits concurrent queries:

```go
semaphore := make(chan struct{}, config.MaxConcurrentQueries)

select {
case semaphore <- struct{}{}:
    defer func() { <-semaphore }()
    // Execute query
case <-ctx.Done():
    return nil, ErrQueryTimeout
}
```

## Metrics

Built-in observability:

| Metric | Description |
|--------|-------------|
| `queries_total` | Total queries processed |
| `query_latency` | Query processing time |
| `cache_hits` | Cache hit count |
| `errors_total` | Error count by type |
| `avg_relevance` | Average result relevance |
