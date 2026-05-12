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
export interface RAGDocument {
    id: number;
    namespace: string;
    content?: string;
    metadata?: Record<string, unknown>;
    timestamp: number;
}
export interface RAGListDocumentsResponse {
    documents: RAGDocument[];
    next_cursor?: string;
    total: number;
}
export interface RAGReindexRequest {
    provider?: string;
}
export interface RAGClient {
    query(req: RAGQueryRequest): Promise<RAGQueryResponse>;
    buildContext(req: RAGBuildContextRequest): Promise<RAGBuildContextResponse>;
    listDocuments(namespace: string, limit?: number, cursor?: string): Promise<RAGListDocumentsResponse>;
    getDocument(id: number): Promise<RAGDocument>;
    deleteDocument(id: number): Promise<void>;
    reindexDocument(id: number, req?: RAGReindexRequest): Promise<RAGDocument>;
}
//# sourceMappingURL=rag.d.ts.map