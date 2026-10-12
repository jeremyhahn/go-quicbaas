// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// ---------------------------------------------------------------------------
// RAG domain types
// ---------------------------------------------------------------------------

export interface RAGCitation {
  index: number;
  file_path: string;
  start_line: number;
  end_line: number;
  score: number;
}

export interface RAGQueryRequest {
  query: string;
  namespace: string;
  top_k?: number;
  max_tokens?: number;
  min_score?: number;
  model?: string;
  template?: string;
  include_citations?: boolean;
}

export interface RAGQueryResponse {
  context: string;
  result_count: number;
  tokens: number;
  truncated: boolean;
  citations: RAGCitation[];
}

export interface RAGResultInput {
  content: string;
  score: number;
  metadata?: Record<string, string>;
}

export interface RAGBuildContextRequest {
  results: RAGResultInput[];
  max_tokens?: number;
  template?: string;
  truncation_strategy?: string;
  include_citations?: boolean;
}

export interface RAGBuildContextResponse {
  context: string;
  result_count: number;
  tokens: number;
  truncated: boolean;
  citations: RAGCitation[];
}

// ---------------------------------------------------------------------------
// RAG document types
// ---------------------------------------------------------------------------

// RAGDocumentID is a document's uint64 vector id. Ids are content hashes, so
// most exceed 2^53: the REST parse delivers such an id as its exact decimal
// string, and a smaller one as a number. Pass it back unchanged.
export type RAGDocumentID = number | string;

// RAGDocument represents a RAG document stored as a vector with content metadata.
export interface RAGDocument {
  id: RAGDocumentID;
  namespace: string;
  content?: string;
  metadata?: Record<string, unknown>;
  timestamp: number;
}

// RAGListDocumentsResponse contains a page of RAG documents.
export interface RAGListDocumentsResponse {
  documents: RAGDocument[];
  next_cursor?: string;
  total: number;
}

// RAGReindexRequest optionally overrides the embedding provider used when
// re-generating the vector for an existing document.
export interface RAGReindexRequest {
  provider?: string;
}

// ---------------------------------------------------------------------------
// RAGClient interface
// ---------------------------------------------------------------------------

// RAGClient exposes retrieval-augmented generation operations.
// Maps to /api/v1/rag/* REST endpoints.
export interface RAGClient {
  // query performs an all-in-one RAG query: embed, vector search, build context.
  // POST /api/v1/rag/query
  query(req: RAGQueryRequest): Promise<RAGQueryResponse>;

  // buildContext assembles context from caller-supplied search results.
  // POST /api/v1/rag/context
  buildContext(req: RAGBuildContextRequest): Promise<RAGBuildContextResponse>;

  // listDocuments returns documents stored in a namespace.
  // GET /api/v1/rag/documents?namespace={namespace}
  listDocuments(namespace: string, limit?: number, cursor?: string): Promise<RAGListDocumentsResponse>;

  // getDocument retrieves a single document by ID.
  // GET /api/v1/rag/documents/{id}
  getDocument(id: RAGDocumentID): Promise<RAGDocument>;

  // deleteDocument removes a document by ID.
  // DELETE /api/v1/rag/documents/{id}
  deleteDocument(id: RAGDocumentID): Promise<void>;

  // reindexDocument re-embeds and re-upserts a document.
  // POST /api/v1/rag/documents/{id}/reindex
  reindexDocument(id: RAGDocumentID, req?: RAGReindexRequest): Promise<RAGDocument>;
}
