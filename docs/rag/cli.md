# RAG — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/rag/`.

Retrieval-Augmented Generation (RAG) commands for combining vector search with context generation.

## Overview

RAG commands allow you to perform operations that combine embedding generation, vector similarity search, and context building for use with language models.

RAG uses the REST API since it orchestrates multiple services (Ollama for embeddings, vector storage for search, and context builder for formatting).

## Commands

### query

Perform an all-in-one RAG query that:
1. Generates an embedding for the query text using Ollama
2. Searches for similar vectors in the specified namespace
3. Builds formatted context from the search results

```bash
quicbaas rag query <query-text>
```

#### Flags
- `--namespace string`: Vector namespace to search (required)
- `--top-k int`: Number of results to retrieve (default: 10)
- `--max-tokens int`: Maximum tokens in generated context (default: 4000)
- `--min-score float32`: Minimum similarity score threshold (default: 0.7)
- `--model string`: Embedding model to use (default: server's default model)
- `--template string`: Context template format (markdown, json, xml, plain) (default: markdown)
- `--include-citations`: Include source citations in response

#### Examples

```bash
# Basic RAG query
quicbaas rag query "How do I configure logging?" --namespace docs

# Query with custom parameters
quicbaas rag query "What is the database schema?" --namespace code \
  --top-k 20 --max-tokens 8000 --min-score 0.75

# Query with specific embedding model
quicbaas rag query "Explain the authentication flow" --namespace docs \
  --model nomic-embed-text

# Query with JSON template and citations
quicbaas rag query "List all API endpoints" --namespace api \
  --template json --include-citations

# Output as JSON
quicbaas rag query "What are the configuration options?" --namespace docs \
  --output json
```

### context

Build formatted context from user-provided search results.

This command allows you to build context from your own search results, useful when you want to use pre-computed embeddings or external search.

```bash
quicbaas rag context
```

#### Flags
- `--results string`: JSON file containing search results (required)
- `--max-tokens int`: Maximum tokens in generated context (default: 4000)
- `--template string`: Context template format (markdown, json, xml, plain) (default: markdown)
- `--truncation string`: Truncation strategy (smart, hard, none) (default: smart)
- `--include-citations`: Include source citations in response

#### Results File Format

```json
[
  {
    "content": "The text content to include",
    "score": 0.95,
    "metadata": {
      "file_path": "/path/to/file.go",
      "start_line": 10,
      "end_line": 20
    }
  }
]
```

#### Examples

```bash
# Build context from results file
quicbaas rag context --results results.json

# Build with custom max tokens
quicbaas rag context --results results.json --max-tokens 8000

# Build with JSON template
quicbaas rag context --results results.json --template json

# Build with smart truncation and citations
quicbaas rag context --results results.json \
  --truncation smart --include-citations
```

## Global Flags

All RAG commands support these flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--timeout` | Request timeout | `30s` |
| `--output` | Output format (json, yaml, table) | `table` |
| `--tls` | Enable TLS | `false` |
| `--tls-cert` | TLS client certificate file | - |
| `--tls-key` | TLS client key file | - |
| `--tls-ca` | TLS CA certificate file | - |

## Template Formats

- **markdown**: Human-readable markdown format with headers and code blocks
- **json**: Structured JSON format for programmatic use
- **xml**: XML format for systems requiring XML input
- **plain**: Plain text without formatting

## Truncation Strategies

- **smart**: Intelligently truncates content while preserving complete sentences/sections
- **hard**: Truncates at exact token limit
- **none**: No truncation (may exceed max_tokens)

## Notes

- RAG queries require a populated vector namespace
- The `--min-score` threshold filters out low-quality matches
- Citations include file path, line numbers, and similarity scores
- Context building respects token limits to fit within LLM context windows
