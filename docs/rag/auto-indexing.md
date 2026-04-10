# Auto-Indexing

Automatic code indexing pipeline: watch files, chunk by language, embed, store vectors.

Source: `pkg/rag/indexing/service.go`, `pkg/rag/indexing/pipeline.go`, `pkg/rag/indexing/config.go`

Build tag: `rag` (all indexing code requires `//go:build rag`)

## Pipeline Architecture

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

## Triggers

| Trigger | Constant | Description |
|---------|----------|-------------|
| `file_change` | `TriggerFileChange` | Index on filesystem create/modify/delete/rename events |
| `iteration_complete` | `TriggerIterationComplete` | Index at the end of a workflow iteration cycle |
| `manual` | `TriggerManual` | Index only on explicit `TriggerReindex()` or `IndexFiles()` call |

Default trigger: `file_change`.

## Incremental Indexing

The pipeline computes a SHA-256 checksum for every file. Before chunking and embedding, it checks the metadata store for the file's previous checksum. If the checksum matches, the file is skipped entirely.

This makes re-indexing cheap: only changed files are re-processed.

## Language-Specific Chunking

| Language | Chunker | Strategy |
|----------|---------|----------|
| Go | `GoCodeChunker` | Function-level chunks, preserves imports/comments/package |
| Markdown | `MarkdownChunker` | Header-based sections, preserves code blocks |
| Other | `TextChunker` | Sentence/paragraph-aware splitting with configurable overlap |

## IndexingService

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

## Configuration Reference

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

## Example Configuration

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

## CPU-First Design

The indexing pipeline is designed to run on CPU by default. GPU acceleration is a future enhancement (Phase 10). The `gpu_accelerated: true` setting currently returns `GPUUnavailableError`.

Concurrency defaults to `runtime.NumCPU()`, and embedding batches are sized at 32 files to balance throughput with memory usage.
