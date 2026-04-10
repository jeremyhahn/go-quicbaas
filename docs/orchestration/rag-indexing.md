# RAG Indexing Pipeline

The RAG (Retrieval-Augmented Generation) indexing pipeline provides cross-project code intelligence for the orchestrator. It scans source files, chunks them by language, generates embeddings, and upserts vectors into namespaced storage for semantic search.

## Pipeline Overview

```
Source files --> Language detection --> Chunking --> Embedding generation --> Vector upsert
```

Each step:

1. **Scan** - Read files from disk (single file or project glob)
2. **Detect language** - Map file extension to language identifier
3. **Chunk** - Split content at double-newline boundaries with ~500 character target size
4. **Embed** - Generate embeddings via the configured embedding service
5. **Upsert** - Store vectors in the specified namespace with file metadata

## MCP Tools

### quicbaas_rag_index_file

Index a single file into a vector namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file_path` | string | yes | Absolute path to the file |
| `namespace` | string | yes | Vector namespace for storage |
| `model` | string | no | Embedding model override |

Returns the file path, chunk count, namespace, detected language, and timestamp.

### quicbaas_rag_index_project

Index all matching files in a project directory.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `project_path` | string | yes | Absolute path to the project |
| `namespace` | string | yes | Vector namespace for storage |
| `file_pattern` | string | no | Glob pattern (default: `*.go`) |
| `model` | string | no | Embedding model override |

Walks the project directory matching files against the glob pattern. Files that fail to read or embed are skipped without aborting the batch. Returns aggregate counts of files and chunks indexed.

### quicbaas_rag_index_status

Query indexing statistics for a namespace.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `namespace` | string | yes | Namespace to query |

Returns the vector count, dimension, and timestamp.

## Language Detection

File extensions are mapped to language identifiers:

| Extensions | Language |
|------------|----------|
| `.go` | go |
| `.py` | python |
| `.js` | javascript |
| `.ts` | typescript |
| `.rs` | rust |
| `.java` | java |
| `.c`, `.h` | c |
| `.cpp`, `.cc`, `.cxx`, `.hpp` | cpp |
| `.rb` | ruby |
| `.sh`, `.bash` | shell |
| `.md` | markdown |
| `.yaml`, `.yml` | yaml |
| `.json` | json |
| `.toml` | toml |
| `.sql` | sql |
| Other | text |

## Chunking Strategy

The chunker splits file content using these rules:

1. Split on double newlines (`\n\n`) as a universal section boundary
2. Accumulate sections into a chunk until adding the next section would exceed ~500 characters
3. Flush the current chunk and start a new one when the limit is reached
4. If a single section exceeds the target size, it becomes its own chunk
5. Empty sections are skipped

This approach preserves logical groupings (functions, type declarations, paragraphs) while keeping chunk sizes within embedding model limits.

## Vector Storage

Each chunk is stored with metadata:

```json
{
  "file_path": "/path/to/file.go",
  "language": "go",
  "chunk_index": 0,
  "chunk_text": "first 500 chars of the chunk..."
}
```

Chunk IDs are deterministic, generated via FNV-1a hash of `{file_path}:{chunk_index}`. This means re-indexing the same file overwrites existing vectors rather than creating duplicates, enabling incremental re-indexing after an agent completes work on a project.

## Vector Namespaces

Namespaces isolate vectors for different scopes:

- **Per-project**: Use a project-specific namespace (e.g., `project-go-quicbaas`) for project-scoped queries
- **Cross-project**: Use a shared namespace (e.g., `org-automatethethings`) for cross-project code search

The namespace is caller-specified, giving the orchestrator flexibility to organize vectors by project, team, or organization.

## Error Handling

The pipeline uses typed errors for each failure mode:

| Error Type | Condition |
|------------|-----------|
| `RAGIndexFileReadError` | File cannot be read from disk |
| `RAGIndexEmbeddingError` | Embedding generation fails for a chunk |
| `RAGIndexUpsertError` | Vector upsert fails for a chunk |
| `RAGIndexGlobError` | File glob pattern fails |

Single-file indexing (`quicbaas_rag_index_file`) fails on the first error. Project indexing (`quicbaas_rag_index_project`) skips failing files and continues the batch.

## Build Tags

RAG indexing tools require both the `mcp` and `rag` build tags.
