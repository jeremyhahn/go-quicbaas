# AI → RAG

## Purpose

The RAG (Retrieval-Augmented Generation) view manages document ingestion, semantic search, and context building for LLM-powered question-answering. Use this view to upload documents, organize them into namespaces, search semantically for similar documents, and generate LLM responses grounded in retrieved context.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Ingest", "Search", "Documents", "Namespaces"

**Tab: Ingest**

```
Namespace:    [default________]        (or create new)
Document ID:  [doc_quarterly-report]   (or auto-generate)
Title:        [Q1 2026 Report______________]  (optional)
Content:      [Paste document text here or upload a file...]
              
              [Choose File...] or [Paste Text]

File Format:  [Auto ▼]  (or PDF, TXT, MD, HTML)

              [Clear] [Ingest]
```

- **Namespace** — organizes documents into logical groups (e.g., "product-docs", "company-wiki")
- **Document ID** — unique identifier; auto-generated if empty
- **Title** — optional display name
- **Content** — document text or file upload
- **File Format** — auto-detect or specify

**Tab: Search**

```
Namespace:    [default________]
Query:        [What is our quarterly revenue?_____]

Top-K:        [5]  (number of results)

              [Search]

Results:

Score   Document ID       Title                Source
─────────────────────────────────────────────────────────
0.92    doc_q1-summary    Q1 2026 Summary      Q1 2026 Report
0.87    doc_revenue       Revenue Breakdown    Finance Doc
0.79    doc_analysis      Market Analysis      Strategic Plan

[Build Context] (generates prompt context from selected results)
```

- **Query** — natural language question
- **Top-K** — number of documents to retrieve
- **Results** — ranked by relevance score
- **Build Context** — constructs a prompt with retrieved documents

**Tab: Documents**

```
Namespace:    [default________]

              [List Documents]

Documents:

Document ID              Title                  Ingested           Size
──────────────────────────────────────────────────────────────────────
doc_quarterly-report     Q1 2026 Report        2026-04-25T14:30Z  4.2 KB
doc_revenue              Revenue Breakdown      2026-04-25T14:25Z  2.1 KB
doc_analysis             Market Analysis        2026-04-25T14:20Z  3.8 KB

Total Documents: 3 | Avg Size: 3.4 KB

[Edit Metadata] [Delete]  (per document)
```

- List and manage documents in the namespace
- Edit metadata or delete documents

**Tab: Namespaces**

```
Namespaces:

Name                  Documents  Created            Actions
────────────────────────────────────────────────────────────
default               3          2026-04-01T00:00Z  [Details] [Delete]
product-docs         12          2026-04-10T10:00Z  [Details] [Delete]
company-wiki         45         2026-04-15T09:00Z  [Details] [Delete]

[+ Create Namespace]
```

- View all namespaces and their document counts
- Create or delete namespaces

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Ingest Document | Upload | Stores a document and generates embeddings | `POST /v1/rag/documents` |
| Search Documents | Search | Semantic similarity search for k nearest documents | `POST /v1/rag/query` (search phase) |
| Build Context | Sparkles | Generates a prompt context from search results | `POST /v1/rag/build-context` |
| List Documents | List | Lists all documents in a namespace | `GET /v1/rag/documents?namespace=X` |
| Delete Document | Trash | Removes a document from the namespace | `DELETE /v1/rag/documents/{id}` |
| Reindex Document | Refresh | Re-embeds a document (after content update) | `POST /v1/rag/documents/{id}/reindex` |
| Create Namespace | Plus | Creates a new namespace for organizing documents | — (client-side; first ingest to a new namespace) |

## Walkthrough

### Ingest and search your first document

1. Open the RAG view (`#/ai/rag`).
2. Click the **Ingest** tab.
3. **Namespace** defaults to "default".
4. **Document ID** auto-generates a unique ID; you can customize it to `doc_welcome`.
5. **Title** — enter "Welcome to QuicBaaS".
6. **Content** — paste or type:

```
QuicBaaS is a high-performance, multi-faceted distributed key-value database 
built on multi-Raft consensus and PebbleDB storage. It is designed for speed, 
strong consistency, horizontal scalability, security, and operational simplicity.

Key features include:
- Multi-protocol support (gRPC, REST, QUIC, MCP)
- Distributed storage with Raft consensus
- Full-text search and vector search
- KMS and HSM integration
- Audit logging and RBAC
```

7. **File Format** defaults to "Auto".
8. Click **Ingest**.

**Expected outcome:**

```
✓ Tab resets
✓ Success toast: "Document ingested: doc_welcome"
```

9. Click the **Documents** tab.
10. Click **List Documents**.

**Expected outcome:**

```
✓ Table shows your new document:
  doc_welcome     Welcome to QuicBaaS     2026-04-25T14:35Z   1.2 KB
```

11. Click the **Search** tab.
12. **Namespace** defaults to "default".
13. In **Query**, enter: `What is QuicBaaS?`.
14. Leave **Top-K** as 5.
15. Click **Search**.

**Expected outcome:**

```
✓ Results table appears:
  Score   Document ID     Title                Source
  ─────────────────────────────────────────────────────
  1.00    doc_welcome     Welcome to QuicBaaS  Ingest
```

The score is 1.00 because the document is a perfect match for the query.

16. Select the result (click the row; a checkbox should appear).
17. Click **Build Context** (or observe a context panel on the right showing the built context).

**Expected outcome:**

```
✓ A context block appears (or a new modal opens):

Context for LLM:

System: Use the following documents to answer the question.

Documents:
- doc_welcome: "QuicBaaS is a high-performance..."

Question: What is QuicBaaS?

[Copy Context] [Use in Prompt]
```

### Ingest more documents and test multi-document search

18. Click the **Ingest** tab again.
19. Create a new document with ID `doc_architecture`, title "Architecture", and content:

```
QuicBaaS uses a distributed Raft consensus model for replication 
and PebbleDB as the storage engine. The architecture consists of:

- Leader node: coordinates writes
- Follower nodes: replicate state
- Storage layer: PebbleDB with block-based storage
- API layer: multi-protocol (gRPC, REST, QUIC, MCP)
```

20. Click **Ingest**.
21. Ingest one more document with ID `doc_security`, title "Security", and content about KMS, encryption, etc.
22. Click the **Search** tab.
23. In **Query**, enter: `How does QuicBaaS handle replication?`.
24. Click **Search**.

**Expected outcome:**

```
✓ Results now include both relevant documents:
  Score   Document ID     Title              
  ─────────────────────────────────────────
  0.94    doc_architecture Architecture  
  0.78    doc_welcome     Welcome to QuicBaaS
```

### Manage namespaces

25. Click the **Namespaces** tab.
26. You should see "default" with 3 documents.
27. Click **[+ Create Namespace]** (or a similar button).
28. A form appears:

```
Create Namespace

Name:  [product-docs___________]

[Cancel] [Create]
```

29. Enter `product-docs` and click **Create**.

**Expected outcome:**

```
✓ "product-docs" now appears in the Namespaces list
```

30. Go back to **Ingest** and ingest a document to the "product-docs" namespace by selecting it in the **Namespace** dropdown.

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Ingest button is disabled | Content is empty | Paste or type document content |
| Search returns "No results" | No documents in namespace | Ingest at least one document first |
| Search returns low-relevance results (score < 0.5) | Query does not match document content | Try rephrasing the query; ingest more relevant documents |
| Embedding generation fails | Embedding provider not configured | Check Settings; configure an embedding provider (OpenAI, local, etc.) |
| Namespaces dropdown is empty | No namespaces created | Ingest a document to "default" namespace first |
| Reindex is slow | Large document being re-embedded | This is normal; wait or check daemon logs |
| Build Context is empty | No documents selected from search results | Click on a search result row to select it before clicking Build Context |

## See also

- [CLI: quicbaas rag](../cli/rag.md) — manage RAG via CLI
- [handlers_rag.go](../../pkg/api/rest/handlers_rag.go) — RAG implementation
- [AI → Embeddings](ai-embeddings.md) — embedding provider configuration
- [Data → Vectors](data-vectors.md) — vector storage and search
