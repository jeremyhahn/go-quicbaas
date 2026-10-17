/**
 * QuicBaaS JavaScript SDK - Ollama Client
 *
 * Provides Ollama embedding service operations including embedding generation,
 * model management, and health checks.
 *
 * @module @quicbaas/client/clients/ollama
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  nonEmptyArray,
} from '../utils/validation.js';

/**
 * @typedef {Object} EmbedResponse
 * @property {number[]} embedding - The embedding vector
 * @property {string} model - Model used for embedding
 * @property {number} dimensions - Embedding vector dimensions
 * @property {number} processingTimeMs - Time taken to generate the embedding in milliseconds
 * @property {string} timestamp - When the embedding was generated
 */

/**
 * @typedef {Object} EmbedBatchResponse
 * @property {number[][]} embeddings - Array of embedding vectors (same order as input)
 * @property {string} model - Model used for embeddings
 * @property {number} dimensions - Embedding vector dimensions
 * @property {number} count - Number of embeddings generated
 * @property {number} processingTimeMs - Total processing time in milliseconds
 * @property {string} timestamp - When the batch was processed
 */

/**
 * @typedef {Object} ModelInfo
 * @property {string} name - Model name
 * @property {number} size - Model size in bytes
 * @property {string} digest - Model digest/hash
 * @property {string} modifiedAt - When the model was last modified
 */

/**
 * @typedef {Object} ListModelsResponse
 * @property {ModelInfo[]} models - Array of available models
 * @property {number} count - Number of models
 * @property {string} timestamp - When the list was retrieved
 */

/**
 * @typedef {Object} ModelDetails
 * @property {string} license - Model license
 * @property {string} modelfile - Model file contents
 * @property {string} format - Model format (e.g., "gguf")
 * @property {string} family - Model family (e.g., "nomic", "llama")
 * @property {string} parameterSize - Model parameter size (e.g., "137M")
 * @property {string} quantizationLevel - Quantization level (e.g., "Q4_0")
 */

/**
 * @typedef {Object} GetModelInfoResponse
 * @property {string} model - Model name
 * @property {ModelDetails} details - Model details
 * @property {string} timestamp - When the info was retrieved
 */

/**
 * @typedef {Object} HealthResponse
 * @property {boolean} available - Whether Ollama is available
 * @property {string} version - Ollama server version
 * @property {string} baseUrl - Ollama API URL
 * @property {string} [error] - Error message if any
 * @property {string} lastChecked - When health was last checked
 * @property {string} timestamp - When this response was generated
 */

/**
 * API base path for Ollama operations.
 * @constant {string}
 */
const API_BASE_PATH = '/api/v1/ollama';

/**
 * OllamaClient provides Ollama embedding service operations for QuicBaaS.
 *
 * Supports embedding generation, model management, and health monitoring
 * for the Ollama embedding service integration.
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const ollamaClient = new OllamaClient(transport);
 *
 * // Generate an embedding using the default model
 * const result = await ollamaClient.embed('Hello, world!');
 * console.log(`Embedding dimensions: ${result.dimensions}`);
 *
 * // Generate an embedding using a specific model
 * const result2 = await ollamaClient.embedWithModel('nomic-embed-text', 'Hello, world!');
 *
 * // Batch embed multiple texts
 * const batchResult = await ollamaClient.embedBatch(['text1', 'text2', 'text3']);
 * console.log(`Generated ${batchResult.count} embeddings`);
 */
class OllamaClient {
  /**
   * Create a new OllamaClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} When transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  // ============================================================
  // Embedding Operations
  // ============================================================

  /**
   * Generate an embedding for a single text using the default model.
   *
   * Uses the configured default embedding model on the server side.
   *
   * @param {string} text - The input text to embed
   * @returns {Promise<EmbedResponse>} The generated embedding
   * @throws {ValidationError} When text is invalid
   *
   * @example
   * const result = await ollamaClient.embed('Hello, world!');
   * console.log(`Embedding: ${result.embedding.slice(0, 5)}...`);
   * console.log(`Model: ${result.model}`);
   * console.log(`Dimensions: ${result.dimensions}`);
   */
  async embed(text) {
    requiredString(text, 'text');

    const path = `${API_BASE_PATH}/embed`;
    const body = { text };

    const response = await this.#transport.post(path, body);
    return this.#formatEmbedResponse(response);
  }

  /**
   * Generate an embedding using a specific model.
   *
   * @param {string} model - The embedding model to use
   * @param {string} text - The input text to embed
   * @returns {Promise<EmbedResponse>} The generated embedding
   * @throws {ValidationError} When model or text is invalid
   *
   * @example
   * const result = await ollamaClient.embedWithModel('nomic-embed-text', 'Hello, world!');
   * console.log(`Used model: ${result.model}`);
   */
  async embedWithModel(model, text) {
    requiredString(model, 'model');
    requiredString(text, 'text');

    const path = `${API_BASE_PATH}/embed`;
    const body = { model, text };

    const response = await this.#transport.post(path, body);
    return this.#formatEmbedResponse(response);
  }

  /**
   * Generate embeddings for multiple texts.
   *
   * More efficient than individual embed calls for batch operations.
   * All embeddings are generated using the same model.
   *
   * @param {string[]} texts - Array of input texts to embed
   * @param {string} [model] - Optional model to use (uses default if not specified)
   * @returns {Promise<EmbedBatchResponse>} The generated embeddings
   * @throws {ValidationError} When texts array is invalid
   *
   * @example
   * const result = await ollamaClient.embedBatch([
   *   'First document',
   *   'Second document',
   *   'Third document'
   * ]);
   * console.log(`Generated ${result.count} embeddings`);
   * result.embeddings.forEach((emb, i) => {
   *   console.log(`Document ${i}: ${emb.length} dimensions`);
   * });
   */
  async embedBatch(texts, model = null) {
    this.#validateTextsArray(texts);

    const path = `${API_BASE_PATH}/embed/batch`;
    const body = { texts };

    if (model !== null) {
      requiredString(model, 'model');
      body.model = model;
    }

    const response = await this.#transport.post(path, body);
    return this.#formatEmbedBatchResponse(response);
  }

  // ============================================================
  // Model Management
  // ============================================================

  /**
   * List all available Ollama models.
   *
   * @returns {Promise<ListModelsResponse>} List of available models
   *
   * @example
   * const result = await ollamaClient.listModels();
   * console.log(`Available models: ${result.count}`);
   * result.models.forEach(model => {
   *   console.log(`  ${model.name} (${model.size} bytes)`);
   * });
   */
  async listModels() {
    const path = `${API_BASE_PATH}/models`;
    const response = await this.#transport.get(path);

    return this.#formatListModelsResponse(response);
  }

  /**
   * Get detailed information about a specific model.
   *
   * @param {string} model - The model name to get info for
   * @returns {Promise<GetModelInfoResponse>} Detailed model information
   * @throws {ValidationError} When model name is invalid
   *
   * @example
   * const info = await ollamaClient.getModelInfo('nomic-embed-text');
   * console.log(`Model: ${info.model}`);
   * console.log(`Family: ${info.details.family}`);
   * console.log(`Parameters: ${info.details.parameterSize}`);
   */
  async getModelInfo(model) {
    requiredString(model, 'model');

    const path = `${API_BASE_PATH}/models/${encodeURIComponent(model)}`;
    const response = await this.#transport.get(path);

    return this.#formatGetModelInfoResponse(response);
  }

  // ============================================================
  // Health Operations
  // ============================================================

  /**
   * Check Ollama service health and availability.
   *
   * Returns detailed health information including version and connectivity status.
   *
   * @returns {Promise<HealthResponse>} Health status information
   *
   * @example
   * const health = await ollamaClient.health();
   * if (health.available) {
   *   console.log(`Ollama v${health.version} is available at ${health.baseUrl}`);
   * } else {
   *   console.log(`Ollama unavailable: ${health.error}`);
   * }
   */
  async health() {
    const path = `${API_BASE_PATH}/health`;
    const response = await this.#transport.get(path);

    return this.#formatHealthResponse(response);
  }

  /**
   * Quick check if Ollama is available.
   *
   * A lightweight check that returns a boolean indicating availability.
   * Does not throw errors on failure, instead returns false.
   *
   * @returns {Promise<boolean>} True if Ollama is available, false otherwise
   *
   * @example
   * if (await ollamaClient.isAvailable()) {
   *   console.log('Ollama is ready');
   * } else {
   *   console.log('Ollama is not available');
   * }
   */
  async isAvailable() {
    try {
      const path = `${API_BASE_PATH}/available`;
      const response = await this.#transport.get(path);
      return response?.available === true;
    } catch {
      return false;
    }
  }

  /**
   * Get the configured default embedding model.
   *
   * @returns {Promise<string>} The default model name
   *
   * @example
   * const defaultModel = await ollamaClient.getDefaultModel();
   * console.log(`Default model: ${defaultModel}`);
   */
  async getDefaultModel() {
    const path = `${API_BASE_PATH}/config/default-model`;
    const response = await this.#transport.get(path);
    return response?.model || '';
  }

  /**
   * Get the configured Ollama API base URL.
   *
   * @returns {Promise<string>} The Ollama base URL
   *
   * @example
   * const baseUrl = await ollamaClient.getBaseURL();
   * console.log(`Ollama API: ${baseUrl}`);
   */
  async getBaseURL() {
    const path = `${API_BASE_PATH}/config/base-url`;
    const response = await this.#transport.get(path);
    return response?.baseUrl || '';
  }

  // ============================================================
  // Private Validation Methods
  // ============================================================

  /**
   * Validate texts array for batch embedding.
   * @param {string[]} texts - Texts to validate
   * @throws {ValidationError} If texts are invalid
   * @private
   */
  #validateTextsArray(texts) {
    nonEmptyArray(texts, 'texts');

    for (let i = 0; i < texts.length; i++) {
      const text = texts[i];
      if (typeof text !== 'string') {
        throw new ValidationError(
          `texts[${i}] must be a string`,
          `texts[${i}]`,
        );
      }
      if (text.trim() === '') {
        throw new ValidationError(
          `texts[${i}] cannot be empty`,
          `texts[${i}]`,
        );
      }
    }
  }

  // ============================================================
  // Private Helper Methods
  // ============================================================

  /**
   * Format embed response.
   * @param {Object} response - Raw response
   * @returns {EmbedResponse} Formatted response
   * @private
   */
  #formatEmbedResponse(response) {
    return {
      embedding: response?.embedding || [],
      model: response?.model || '',
      dimensions: response?.dimensions ?? 0,
      processingTimeMs: response?.processingTimeMs ?? response?.processingTime ?? 0,
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Format embed batch response.
   * @param {Object} response - Raw response
   * @returns {EmbedBatchResponse} Formatted response
   * @private
   */
  #formatEmbedBatchResponse(response) {
    return {
      embeddings: response?.embeddings || [],
      model: response?.model || '',
      dimensions: response?.dimensions ?? 0,
      count: response?.count ?? (response?.embeddings?.length || 0),
      processingTimeMs: response?.processingTimeMs ?? response?.processingTime ?? 0,
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Format list models response.
   * @param {Object} response - Raw response
   * @returns {ListModelsResponse} Formatted response
   * @private
   */
  #formatListModelsResponse(response) {
    const models = (response?.models || []).map(m => ({
      name: m?.name || '',
      size: m?.size ?? 0,
      digest: m?.digest || '',
      modifiedAt: m?.modifiedAt || null,
    }));

    return {
      models,
      count: response?.count ?? models.length,
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Format get model info response.
   * @param {Object} response - Raw response
   * @returns {GetModelInfoResponse} Formatted response
   * @private
   */
  #formatGetModelInfoResponse(response) {
    const details = response?.details || {};

    return {
      model: response?.model || '',
      details: {
        license: details?.license || '',
        modelfile: details?.modelfile || '',
        format: details?.format || '',
        family: details?.family || '',
        parameterSize: details?.parameterSize || '',
        quantizationLevel: details?.quantizationLevel || '',
      },
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Format health response.
   * @param {Object} response - Raw response
   * @returns {HealthResponse} Formatted response
   * @private
   */
  #formatHealthResponse(response) {
    return {
      available: response?.available === true,
      version: response?.version || '',
      baseUrl: response?.baseUrl || '',
      error: response?.error || null,
      lastChecked: response?.lastChecked || null,
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }
}

export default OllamaClient;
