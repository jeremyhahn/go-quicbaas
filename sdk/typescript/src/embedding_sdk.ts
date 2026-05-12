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
// Embeddings domain types
// ---------------------------------------------------------------------------

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

// SetProviderStatusResponse confirms the provider status change.
export interface SetProviderStatusResponse {
  name: string;
  enabled: boolean;
  timestamp: string;
}

// ---------------------------------------------------------------------------
// EmbeddingsClient interface
// ---------------------------------------------------------------------------

// EmbeddingsClient exposes embedding generation and provider management.
// Maps to /api/v1/embedding/* REST endpoints.
export interface EmbeddingsClient {
  // generate creates a vector embedding for the supplied text.
  // POST /api/v1/embedding/generate
  generate(req: GenerateEmbeddingRequest): Promise<GenerateEmbeddingResponse>;

  // listProviders returns all registered embedding providers.
  // GET /api/v1/embedding/providers
  listProviders(req?: ListProvidersRequest): Promise<ListProvidersResponse>;

  // setDefaultProvider sets a provider as the default for embedding generation.
  // PUT /api/v1/embedding/providers/default
  setDefaultProvider(providerName: string): Promise<void>;

  // setProviderStatus enables or disables an embedding provider.
  // PUT /api/v1/embedding/providers/{name}/status
  setProviderStatus(name: string, enabled: boolean): Promise<SetProviderStatusResponse>;
}
