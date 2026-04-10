/**
 * OllamaClient Tests
 *
 * Comprehensive test suite for the QuicBaaS Ollama Client.
 * Tests all public methods with success and error cases.
 */

import OllamaClient from '../../src/clients/ollama.js';
import { ValidationError } from '../../src/errors/index.js';

describe('OllamaClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
    };
    client = new OllamaClient(mockTransport);
  });

  // ============================================================
  // Constructor Tests
  // ============================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const newClient = new OllamaClient(mockTransport);
      expect(newClient).toBeInstanceOf(OllamaClient);
    });

    it('should throw ValidationError when transport is not provided', () => {
      expect(() => new OllamaClient()).toThrow(ValidationError);
      expect(() => new OllamaClient(null)).toThrow(ValidationError);
      expect(() => new OllamaClient(undefined)).toThrow(ValidationError);
    });
  });

  // ============================================================
  // embed Tests
  // ============================================================

  describe('embed', () => {
    it('should generate embedding for text', async () => {
      const mockResponse = {
        embedding: [0.1, 0.2, 0.3, 0.4, 0.5],
        model: 'nomic-embed-text',
        dimensions: 5,
        processingTimeMs: 25,
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.embed('Hello, world!');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/ollama/embed', {
        text: 'Hello, world!',
      });
      expect(result.embedding).toEqual([0.1, 0.2, 0.3, 0.4, 0.5]);
      expect(result.model).toBe('nomic-embed-text');
      expect(result.dimensions).toBe(5);
      expect(result.processingTimeMs).toBe(25);
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should handle response with processingTime field', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        model: 'test',
        dimensions: 1,
        processingTime: 50,
      });

      const result = await client.embed('test');

      expect(result.processingTimeMs).toBe(50);
    });

    it('should throw ValidationError when text is missing', async () => {
      await expect(client.embed()).rejects.toThrow(ValidationError);
      await expect(client.embed(null)).rejects.toThrow(ValidationError);
      await expect(client.embed(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when text is empty', async () => {
      await expect(client.embed('')).rejects.toThrow(ValidationError);
      await expect(client.embed('   ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when text is not a string', async () => {
      await expect(client.embed(123)).rejects.toThrow(ValidationError);
      await expect(client.embed({})).rejects.toThrow(ValidationError);
      await expect(client.embed(['array'])).rejects.toThrow(ValidationError);
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.embed('test');

      expect(result.embedding).toEqual([]);
      expect(result.model).toBe('');
      expect(result.dimensions).toBe(0);
      expect(result.processingTimeMs).toBe(0);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.embed('test');

      expect(result.embedding).toEqual([]);
      expect(result.model).toBe('');
      expect(result.dimensions).toBe(0);
    });
  });

  // ============================================================
  // embedWithModel Tests
  // ============================================================

  describe('embedWithModel', () => {
    it('should generate embedding with specified model', async () => {
      const mockResponse = {
        embedding: [0.5, 0.6, 0.7],
        model: 'mxbai-embed-large',
        dimensions: 3,
        processingTimeMs: 30,
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.embedWithModel('mxbai-embed-large', 'Hello, world!');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/ollama/embed', {
        model: 'mxbai-embed-large',
        text: 'Hello, world!',
      });
      expect(result.model).toBe('mxbai-embed-large');
      expect(result.embedding).toEqual([0.5, 0.6, 0.7]);
    });

    it('should throw ValidationError when model is missing', async () => {
      await expect(client.embedWithModel(null, 'text')).rejects.toThrow(ValidationError);
      await expect(client.embedWithModel(undefined, 'text')).rejects.toThrow(ValidationError);
      await expect(client.embedWithModel('', 'text')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when model is empty', async () => {
      await expect(client.embedWithModel('  ', 'text')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when model is not a string', async () => {
      await expect(client.embedWithModel(123, 'text')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when text is missing', async () => {
      await expect(client.embedWithModel('model', null)).rejects.toThrow(ValidationError);
      await expect(client.embedWithModel('model', undefined)).rejects.toThrow(ValidationError);
      await expect(client.embedWithModel('model', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when text is empty', async () => {
      await expect(client.embedWithModel('model', '   ')).rejects.toThrow(ValidationError);
    });
  });

  // ============================================================
  // embedBatch Tests
  // ============================================================

  describe('embedBatch', () => {
    it('should generate batch embeddings without model', async () => {
      const mockResponse = {
        embeddings: [
          [0.1, 0.2, 0.3],
          [0.4, 0.5, 0.6],
          [0.7, 0.8, 0.9],
        ],
        model: 'nomic-embed-text',
        dimensions: 3,
        count: 3,
        processingTimeMs: 75,
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.embedBatch(['First', 'Second', 'Third']);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/ollama/embed/batch', {
        texts: ['First', 'Second', 'Third'],
      });
      expect(result.embeddings).toHaveLength(3);
      expect(result.model).toBe('nomic-embed-text');
      expect(result.dimensions).toBe(3);
      expect(result.count).toBe(3);
      expect(result.processingTimeMs).toBe(75);
    });

    it('should generate batch embeddings with specified model', async () => {
      const mockResponse = {
        embeddings: [[0.1, 0.2]],
        model: 'custom-model',
        dimensions: 2,
        count: 1,
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.embedBatch(['Test'], 'custom-model');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/ollama/embed/batch', {
        texts: ['Test'],
        model: 'custom-model',
      });
      expect(result.model).toBe('custom-model');
    });

    it('should handle response with processingTime field', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embeddings: [[0.1]],
        processingTime: 100,
      });

      const result = await client.embedBatch(['test']);

      expect(result.processingTimeMs).toBe(100);
    });

    it('should derive count from embeddings length if missing', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embeddings: [[0.1], [0.2], [0.3]],
        model: 'test',
        dimensions: 1,
      });

      const result = await client.embedBatch(['a', 'b', 'c']);

      expect(result.count).toBe(3);
    });

    it('should throw ValidationError when texts is empty array', async () => {
      await expect(client.embedBatch([])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when texts is not an array', async () => {
      await expect(client.embedBatch('invalid')).rejects.toThrow(ValidationError);
      await expect(client.embedBatch(null)).rejects.toThrow(ValidationError);
      await expect(client.embedBatch(undefined)).rejects.toThrow(ValidationError);
      await expect(client.embedBatch({})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when texts contains non-string elements', async () => {
      await expect(client.embedBatch([123])).rejects.toThrow(ValidationError);
      await expect(client.embedBatch([null])).rejects.toThrow(ValidationError);
      await expect(client.embedBatch([{}])).rejects.toThrow(ValidationError);
      await expect(client.embedBatch(['valid', 123])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when texts contains empty strings', async () => {
      await expect(client.embedBatch([''])).rejects.toThrow(ValidationError);
      await expect(client.embedBatch(['   '])).rejects.toThrow(ValidationError);
      await expect(client.embedBatch(['valid', ''])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when model is not a string', async () => {
      await expect(client.embedBatch(['test'], 123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when model is empty string', async () => {
      await expect(client.embedBatch(['test'], '')).rejects.toThrow(ValidationError);
      await expect(client.embedBatch(['test'], '   ')).rejects.toThrow(ValidationError);
    });

    it('should handle null model correctly', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embeddings: [[0.1]],
        model: 'default',
      });

      await client.embedBatch(['test'], null);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/ollama/embed/batch', {
        texts: ['test'],
      });
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.embedBatch(['test']);

      expect(result.embeddings).toEqual([]);
      expect(result.model).toBe('');
      expect(result.dimensions).toBe(0);
      expect(result.count).toBe(0);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.embedBatch(['test']);

      expect(result.embeddings).toEqual([]);
      expect(result.count).toBe(0);
    });
  });

  // ============================================================
  // listModels Tests
  // ============================================================

  describe('listModels', () => {
    it('should list available models', async () => {
      const mockResponse = {
        models: [
          {
            name: 'nomic-embed-text',
            size: 274000000,
            digest: 'sha256:abc123',
            modifiedAt: '2025-01-10T10:00:00Z',
          },
          {
            name: 'mxbai-embed-large',
            size: 670000000,
            digest: 'sha256:def456',
            modifiedAt: '2025-01-12T10:00:00Z',
          },
        ],
        count: 2,
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listModels();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/ollama/models');
      expect(result.models).toHaveLength(2);
      expect(result.models[0].name).toBe('nomic-embed-text');
      expect(result.models[0].size).toBe(274000000);
      expect(result.models[0].digest).toBe('sha256:abc123');
      expect(result.models[0].modifiedAt).toBe('2025-01-10T10:00:00Z');
      expect(result.count).toBe(2);
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should derive count from models length if missing', async () => {
      mockTransport.get.mockResolvedValueOnce({
        models: [
          { name: 'model1' },
          { name: 'model2' },
        ],
      });

      const result = await client.listModels();

      expect(result.count).toBe(2);
    });

    it('should handle empty models array', async () => {
      mockTransport.get.mockResolvedValueOnce({
        models: [],
        count: 0,
      });

      const result = await client.listModels();

      expect(result.models).toEqual([]);
      expect(result.count).toBe(0);
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listModels();

      expect(result.models).toEqual([]);
      expect(result.count).toBe(0);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.listModels();

      expect(result.models).toEqual([]);
      expect(result.count).toBe(0);
    });

    it('should handle models with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        models: [
          { name: 'partial-model' },
          null,
        ],
      });

      const result = await client.listModels();

      expect(result.models[0].name).toBe('partial-model');
      expect(result.models[0].size).toBe(0);
      expect(result.models[0].digest).toBe('');
      expect(result.models[0].modifiedAt).toBe(null);
      expect(result.models[1].name).toBe('');
    });
  });

  // ============================================================
  // getModelInfo Tests
  // ============================================================

  describe('getModelInfo', () => {
    it('should get model info successfully', async () => {
      const mockResponse = {
        model: 'nomic-embed-text',
        details: {
          license: 'Apache-2.0',
          modelfile: 'FROM nomic-embed-text...',
          format: 'gguf',
          family: 'nomic',
          parameterSize: '137M',
          quantizationLevel: 'Q4_0',
        },
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getModelInfo('nomic-embed-text');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/ollama/models/nomic-embed-text');
      expect(result.model).toBe('nomic-embed-text');
      expect(result.details.license).toBe('Apache-2.0');
      expect(result.details.modelfile).toBe('FROM nomic-embed-text...');
      expect(result.details.format).toBe('gguf');
      expect(result.details.family).toBe('nomic');
      expect(result.details.parameterSize).toBe('137M');
      expect(result.details.quantizationLevel).toBe('Q4_0');
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should URL encode model name with special characters', async () => {
      mockTransport.get.mockResolvedValueOnce({
        model: 'model/with/slashes',
        details: {},
      });

      await client.getModelInfo('model/with/slashes');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/ollama/models/model%2Fwith%2Fslashes'
      );
    });

    it('should throw ValidationError when model is missing', async () => {
      await expect(client.getModelInfo()).rejects.toThrow(ValidationError);
      await expect(client.getModelInfo(null)).rejects.toThrow(ValidationError);
      await expect(client.getModelInfo(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when model is empty', async () => {
      await expect(client.getModelInfo('')).rejects.toThrow(ValidationError);
      await expect(client.getModelInfo('   ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when model is not a string', async () => {
      await expect(client.getModelInfo(123)).rejects.toThrow(ValidationError);
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getModelInfo('test');

      expect(result.model).toBe('');
      expect(result.details.license).toBe('');
      expect(result.details.modelfile).toBe('');
      expect(result.details.format).toBe('');
      expect(result.details.family).toBe('');
      expect(result.details.parameterSize).toBe('');
      expect(result.details.quantizationLevel).toBe('');
    });

    it('should handle response with missing details', async () => {
      mockTransport.get.mockResolvedValueOnce({
        model: 'test',
      });

      const result = await client.getModelInfo('test');

      expect(result.model).toBe('test');
      expect(result.details.license).toBe('');
    });
  });

  // ============================================================
  // health Tests
  // ============================================================

  describe('health', () => {
    it('should return health status when available', async () => {
      const mockResponse = {
        available: true,
        version: '0.5.1',
        baseUrl: 'http://localhost:11434',
        lastChecked: '2025-01-15T09:59:00Z',
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.health();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/ollama/health');
      expect(result.available).toBe(true);
      expect(result.version).toBe('0.5.1');
      expect(result.baseUrl).toBe('http://localhost:11434');
      expect(result.error).toBe(null);
      expect(result.lastChecked).toBe('2025-01-15T09:59:00Z');
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should return health status when unavailable', async () => {
      const mockResponse = {
        available: false,
        version: '',
        baseUrl: 'http://localhost:11434',
        error: 'Connection refused',
        lastChecked: '2025-01-15T09:58:00Z',
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.health();

      expect(result.available).toBe(false);
      expect(result.error).toBe('Connection refused');
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.health();

      expect(result.available).toBe(false);
      expect(result.version).toBe('');
      expect(result.baseUrl).toBe('');
      expect(result.error).toBe(null);
      expect(result.lastChecked).toBe(null);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.health();

      expect(result.available).toBe(false);
    });
  });

  // ============================================================
  // isAvailable Tests
  // ============================================================

  describe('isAvailable', () => {
    it('should return true when Ollama is available', async () => {
      mockTransport.get.mockResolvedValueOnce({ available: true });

      const result = await client.isAvailable();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/ollama/available');
      expect(result).toBe(true);
    });

    it('should return false when Ollama is not available', async () => {
      mockTransport.get.mockResolvedValueOnce({ available: false });

      const result = await client.isAvailable();

      expect(result).toBe(false);
    });

    it('should return false on transport error', async () => {
      mockTransport.get.mockRejectedValueOnce(new Error('Network error'));

      const result = await client.isAvailable();

      expect(result).toBe(false);
    });

    it('should return false when response is null', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.isAvailable();

      expect(result).toBe(false);
    });

    it('should return false when available field is missing', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.isAvailable();

      expect(result).toBe(false);
    });

    it('should return false when available is not true', async () => {
      mockTransport.get.mockResolvedValueOnce({ available: 'yes' });

      const result = await client.isAvailable();

      expect(result).toBe(false);
    });
  });

  // ============================================================
  // getDefaultModel Tests
  // ============================================================

  describe('getDefaultModel', () => {
    it('should return default model name', async () => {
      mockTransport.get.mockResolvedValueOnce({ model: 'nomic-embed-text' });

      const result = await client.getDefaultModel();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/ollama/config/default-model');
      expect(result).toBe('nomic-embed-text');
    });

    it('should return empty string when model is missing', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getDefaultModel();

      expect(result).toBe('');
    });

    it('should return empty string when response is null', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getDefaultModel();

      expect(result).toBe('');
    });
  });

  // ============================================================
  // getBaseURL Tests
  // ============================================================

  describe('getBaseURL', () => {
    it('should return Ollama base URL', async () => {
      mockTransport.get.mockResolvedValueOnce({ baseUrl: 'http://localhost:11434' });

      const result = await client.getBaseURL();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/ollama/config/base-url');
      expect(result).toBe('http://localhost:11434');
    });

    it('should return empty string when baseUrl is missing', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getBaseURL();

      expect(result).toBe('');
    });

    it('should return empty string when response is null', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getBaseURL();

      expect(result).toBe('');
    });
  });

  // ============================================================
  // Transport Error Handling Tests
  // ============================================================

  describe('transport error handling', () => {
    it('should propagate transport errors from embed', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.embed('test')).rejects.toThrow('Network error');
    });

    it('should propagate transport errors from embedWithModel', async () => {
      const error = new Error('Server error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.embedWithModel('model', 'test')).rejects.toThrow('Server error');
    });

    it('should propagate transport errors from embedBatch', async () => {
      const error = new Error('Batch error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.embedBatch(['test'])).rejects.toThrow('Batch error');
    });

    it('should propagate transport errors from listModels', async () => {
      const error = new Error('List error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.listModels()).rejects.toThrow('List error');
    });

    it('should propagate transport errors from getModelInfo', async () => {
      const error = new Error('Model not found');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getModelInfo('unknown')).rejects.toThrow('Model not found');
    });

    it('should propagate transport errors from health', async () => {
      const error = new Error('Health check failed');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.health()).rejects.toThrow('Health check failed');
    });

    it('should propagate transport errors from getDefaultModel', async () => {
      const error = new Error('Config error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getDefaultModel()).rejects.toThrow('Config error');
    });

    it('should propagate transport errors from getBaseURL', async () => {
      const error = new Error('Config error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getBaseURL()).rejects.toThrow('Config error');
    });
  });

  // ============================================================
  // Edge Cases Tests
  // ============================================================

  describe('edge cases', () => {
    it('should handle large text for embedding', async () => {
      const largeText = 'a'.repeat(10000);
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        model: 'test',
        dimensions: 1,
      });

      await client.embed(largeText);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/ollama/embed', {
        text: largeText,
      });
    });

    it('should handle large batch', async () => {
      const texts = Array(1000).fill('test text');
      mockTransport.post.mockResolvedValueOnce({
        embeddings: texts.map(() => [0.1]),
        model: 'test',
        count: 1000,
      });

      const result = await client.embedBatch(texts);

      expect(result.count).toBe(1000);
    });

    it('should handle unicode text', async () => {
      const unicodeText = 'Hello, World! Test data.';
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        model: 'test',
      });

      await client.embed(unicodeText);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/ollama/embed', {
        text: unicodeText,
      });
    });

    it('should handle special characters in model name', async () => {
      const modelName = 'model:latest';
      mockTransport.get.mockResolvedValueOnce({
        model: modelName,
        details: {},
      });

      await client.getModelInfo(modelName);

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/ollama/models/model%3Alatest'
      );
    });
  });
});
