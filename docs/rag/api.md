# RAG — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/rag/`.


## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.

### `quicbaas.RAG` — SDK package `sdk/go/client/rag`

Dedicated transport handlers: rest=yes, grpc=yes, mcp=no; embedded routes registered: 2 (`rag` builds); MCP tools registered server-side: 2.

MCP tools: `quicbaas_rag_build_context`, `quicbaas_rag_query`

| SDK method | REST route (transport) | Served? | gRPC | MCP | Embedded | Permission |
|---|---|---|---|---|---|---|
| `BuildContext` | `POST /api/v1/rag/context` | yes | yes | MISSING | yes | `rag:query` |
| `Query` | `POST /api/v1/rag/query` | yes | yes | MISSING | yes | `rag:query` |

One service, every transport (#1611): `pkg/rag/ragservice` holds the only
`Query` and `BuildContext`. The REST handlers, the server-side MCP tools,
the node's gRPC `quicbaas.rag.v1.RAGService` (`pkg/api/grpc/server_rag.go`,
registered by `registerRAGService`, authorized as `rag:query`) and the
embedded routes `quicbaas.RAG/Query` and `quicbaas.RAG/BuildContext`
(`sdk/go/transport/embedded/handlers_rag.go`) only convert their own types
and call it. Its typed errors come back as invalid input (no query, no
namespace, no results) or unavailable (Ollama or the vector store not
configured), on every transport.

### go-qrdb's RAG service — `quicbaas.QRDBRAG(c)` (`qrdb_vector` builds)

`sdk/go` re-exports go-qrdb's own RAG client (`sdk/go/qrdb_rag.go`): the
interface as `QRDBRAGClient`, and its request and response types as
`QRDBRAGIngestRequest`, `QRDBRAGQueryRequest`, `QRDBRAGDeleteDocumentRequest`,
`QRDBRAGListDocumentsRequest` and their responses. They are aliases, so they
are go-qrdb's types and not copies. `quicbaas.QRDBRAG(c)` returns the client.
It runs over the qrdb sub-client, which uses the same transport and address
as `c`. It returns an error if that sub-client cannot be built, or if its
transport does not implement go-qrdb's RAG service.

| Method | Does |
|---|---|
| `RAGIngestDocument` | chunks (`fixed`, `sentence`, `paragraph`), embeds and stores a document |
| `RAGQuery` | embeds the query and returns the nearest passages of a namespace |
| `RAGDeleteDocument` | removes a document, its chunk vectors and KV entries |
| `RAGListDocuments` | pages through a namespace's ingested documents |

**Which node serves it.** Over REST the client calls go-qrdb's routes:
`POST /v1/rag/ingest`, `POST /v1/rag/query`, `POST /v1/rag/documents/delete`
and `GET /v1/rag/documents` (`TestQRDBRAG_RESTCallsGoQRDBRoutes` pins them).
A QuicBaaS node serves none of them. Its REST RAG routes are QuicBaaS's own
`/api/v1/rag/*`, and on gRPC it serves `quicbaas.rag.v1.RAGService`. Its
`qrdb.v1.QRDBService` gets no RAG servicer, because go-qrdb's SDK exports no
RAG register option (v0.30.1-alpha). So point `c` at a go-qrdb node's address
to use `QRDBRAG`. Calls through a QuicBaaS node fail with not found or
unimplemented. go-qrdb's embedded transport has no RAG service either.

go-qrdb builds its RAG client only under the `qrdb_vector` tag, so the
re-export does too. `quicbaas.RAG` (above, `Client.RAG()`) is still QuicBaaS's
own pipeline. It is the parallel implementation the SPEC row keeps until the
owner retires it.

## Overview

QuicBaaS provides 2 RAG-specific MCP tools:

| Tool | Description |
|------|-------------|
| `quicbaas_rag_query` | All-in-one: embed → search → build context |
| `quicbaas_rag_build_context` | Build context from user-provided results |

## quicbaas_rag_query

Performs a complete RAG pipeline: generates an embedding for the query, searches for similar vectors, and assembles the results into LLM-ready context.

### Schema

```json
{
  "name": "quicbaas_rag_query",
  "description": "All-in-one RAG query: generates embedding, searches vectors, and assembles context for LLM consumption",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "The query text to search for"
      },
      "namespace": {
        "type": "string",
        "description": "Vector namespace to search in"
      },
      "top_k": {
        "type": "integer",
        "description": "Number of results to return (default: 10)"
      },
      "max_tokens": {
        "type": "integer",
        "description": "Maximum tokens for assembled context (default: 4000)"
      },
      "min_score": {
        "type": "string",
        "description": "Minimum similarity score 0.0-1.0 (default: 0.7)"
      },
      "template": {
        "type": "string",
        "enum": ["markdown", "json", "xml", "plain"],
        "description": "Output format template (default: markdown)"
      },
      "include_citations": {
        "type": "string",
        "enum": ["true", "false"],
        "description": "Include source citations in context (default: true)"
      },
      "model": {
        "type": "string",
        "description": "Embedding model to use (uses default if not specified)"
      }
    },
    "required": ["query", "namespace"]
  }
}
```

### Examples

**Basic Query:**

```json
{
  "tool": "quicbaas_rag_query",
  "arguments": {
    "query": "How does error handling work?",
    "namespace": "codebase"
  }
}
```

**Advanced Query:**

```json
{
  "tool": "quicbaas_rag_query",
  "arguments": {
    "query": "database connection pooling",
    "namespace": "documentation",
    "top_k": 15,
    "max_tokens": 8000,
    "min_score": "0.8",
    "template": "json",
    "include_citations": "true",
    "model": "mxbai-embed-large"
  }
}
```

### Response

```json
{
  "context": "## Relevant Code\n\n[1] src/db/pool.go:23-45\n```go\n// Connection pooling implementation\ntype Pool struct {...}\n```\n...",
  "tokens": 3200,
  "truncated": false,
  "result_count": 12,
  "citations": [
    {
      "index": 1,
      "file_path": "src/db/pool.go",
      "start_line": 23,
      "end_line": 45,
      "score": 0.94
    },
    {
      "index": 2,
      "file_path": "src/db/config.go",
      "start_line": 10,
      "end_line": 25,
      "score": 0.89
    }
  ]
}
```

## quicbaas_rag_build_context

Assembles LLM context from user-provided search results. Use this when you want to perform the search step yourself or have results from a custom pipeline.

### Schema

```json
{
  "name": "quicbaas_rag_build_context",
  "description": "Assemble LLM context from user-provided search results or chunks",
  "inputSchema": {
    "type": "object",
    "properties": {
      "results": {
        "type": "string",
        "description": "JSON array of result objects with content, metadata, and scores"
      },
      "max_tokens": {
        "type": "integer",
        "description": "Maximum tokens for assembled context (default: 4000)"
      },
      "template": {
        "type": "string",
        "enum": ["markdown", "json", "xml", "plain"],
        "description": "Output format template (default: markdown)"
      },
      "include_citations": {
        "type": "string",
        "enum": ["true", "false"],
        "description": "Include source citations in context (default: true)"
      },
      "truncation_strategy": {
        "type": "string",
        "enum": ["smart", "hard", "none"],
        "description": "How to handle context overflow (default: smart)"
      }
    },
    "required": ["results"]
  }
}
```

### Result Object Format

Each result in the `results` array should have:

```json
{
  "content": "The actual text content",
  "score": 0.95,
  "metadata": {
    "file_path": "src/example.go",
    "file_name": "example.go",
    "language": "go",
    "start_line": "10",
    "end_line": "25"
  }
}
```

### Examples

**Build Context from Custom Results:**

```json
{
  "tool": "quicbaas_rag_build_context",
  "arguments": {
    "results": "[{\"content\": \"func main() {...}\", \"score\": 0.92, \"metadata\": {\"file_path\": \"main.go\", \"start_line\": \"1\", \"end_line\": \"20\"}}]",
    "max_tokens": 2000,
    "template": "markdown"
  }
}
```

**With Hard Truncation:**

```json
{
  "tool": "quicbaas_rag_build_context",
  "arguments": {
    "results": "[...]",
    "max_tokens": 1000,
    "truncation_strategy": "hard",
    "include_citations": "false"
  }
}
```

### Response

```json
{
  "context": "## Context\n\n[1] main.go:1-20\n```go\nfunc main() {...}\n```",
  "tokens": 150,
  "truncated": false,
  "result_count": 1,
  "citations": [
    {
      "index": 1,
      "file_path": "main.go",
      "start_line": 1,
      "end_line": 20,
      "score": 0.92
    }
  ]
}
```

## Error Handling

Both tools return errors for invalid inputs:

```json
{
  "error": "query is required"
}
```

```json
{
  "error": "namespace is required"
}
```

```json
{
  "error": "ollama service not configured"
}
```

```json
{
  "error": "vector service not configured"
}
```

## Workflow Comparison

### Use quicbaas_rag_query when:
- You want the simplest approach
- Standard vector search meets your needs
- You don't need custom reranking

### Use quicbaas_rag_build_context when:
- You have custom search logic (hybrid search, filters, etc.)
- You want to rerank results before context building
- You're integrating with an external search system
- You need to combine results from multiple namespaces

## Performance Tips

1. **Appropriate top_k**: Start with 10, increase if context quality is low
2. **Token limits**: Match your LLM's context window (minus prompt/response)
3. **min_score**: Higher values (0.8+) for precision, lower (0.5+) for recall
4. **Batch queries**: For multiple queries, consider parallel tool calls

## Integration with Vector Tools

RAG tools work seamlessly with vector tools:

```
1. quicbaas_vector_upsert      - Index documents with embeddings
2. quicbaas_rag_query          - Search and build context
3. quicbaas_vector_get         - Retrieve specific vectors
```

See Vector Tools Reference for vector operations.
