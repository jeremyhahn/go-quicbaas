export interface EmbeddingProviderInfo {
    name: string;
    type: string;
    model: string;
    dimensions: number;
    max_tokens: number;
    max_batch_size: number;
    status: string;
    api_url: string;
    rate_limit_per_minute: number;
    is_default: boolean;
    last_error?: string;
    success_count: number;
    error_count: number;
}
export interface EmbeddingMetadata {
    model: string;
    provider: string;
    dimensions: number;
    tokens: number;
    source?: string;
    language?: string;
    input_type?: string;
    cache_hit: boolean;
    processing_time: string;
}
export interface GenerateEmbeddingRequest {
    text: string;
    provider?: string;
    input_type?: string;
    metadata?: Record<string, string>;
    source?: string;
    language?: string;
    cache_bypass?: boolean;
}
export interface GenerateEmbeddingResponse {
    embedding: number[];
    timestamp: string;
    metadata?: EmbeddingMetadata;
}
export interface ListProvidersRequest {
    include_unavailable?: boolean;
}
export interface ListProvidersResponse {
    providers: EmbeddingProviderInfo[];
    default_provider: string;
    timestamp: string;
}
export interface SetProviderStatusResponse {
    name: string;
    enabled: boolean;
    timestamp: string;
}
export interface EmbeddingsClient {
    generate(req: GenerateEmbeddingRequest): Promise<GenerateEmbeddingResponse>;
    listProviders(req?: ListProvidersRequest): Promise<ListProvidersResponse>;
    setDefaultProvider(providerName: string): Promise<void>;
    setProviderStatus(name: string, enabled: boolean): Promise<SetProviderStatusResponse>;
}
//# sourceMappingURL=embedding_sdk.d.ts.map