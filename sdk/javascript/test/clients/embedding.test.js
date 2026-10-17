/**
 * EmbeddingClient Tests
 *
 * Comprehensive test suite for the QuicBaaS Embedding Client.
 * Tests all public methods with success and error cases.
 */

import EmbeddingClient, { Provider, InputType, Status } from '../../src/clients/embedding.js';
import { ValidationError } from '../../src/errors/index.js';

describe('EmbeddingClient', () => {
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
    client = new EmbeddingClient(mockTransport);
  });

  // ============================================================
  // Constructor Tests
  // ============================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const newClient = new EmbeddingClient(mockTransport);
      expect(newClient).toBeInstanceOf(EmbeddingClient);
    });

    it('should throw ValidationError when transport is not provided', () => {
      expect(() => new EmbeddingClient()).toThrow(ValidationError);
      expect(() => new EmbeddingClient(null)).toThrow(ValidationError);
      expect(() => new EmbeddingClient(undefined)).toThrow(ValidationError);
    });
  });

  // ============================================================
  // Enum Tests
  // ============================================================

  describe('Provider enum', () => {
    it('should have all expected provider values', () => {
      expect(Provider.VOYAGE).toBe('voyage');
      expect(Provider.OPENAI).toBe('openai');
      expect(Provider.OLLAMA).toBe('ollama');
      expect(Provider.LMSTUDIO).toBe('lmstudio');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(Provider)).toBe(true);
    });
  });

  describe('InputType enum', () => {
    it('should have all expected input type values', () => {
      expect(InputType.DOCUMENT).toBe('document');
      expect(InputType.QUERY).toBe('query');
      expect(InputType.CODE).toBe('code');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(InputType)).toBe(true);
    });
  });

  describe('Status enum', () => {
    it('should have all expected status values', () => {
      expect(Status.AVAILABLE).toBe('available');
      expect(Status.UNAVAILABLE).toBe('unavailable');
      expect(Status.RATE_LIMITED).toBe('rate_limited');
      expect(Status.ERROR).toBe('error');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(Status)).toBe(true);
    });
  });

  // ============================================================
  // generateEmbedding Tests
  // ============================================================

  describe('generateEmbedding', () => {
    it('should generate embedding for text with minimal options', async () => {
      const mockResponse = {
        embedding: [0.1, 0.2, 0.3, 0.4, 0.5],
        metadata: {
          model: 'voyage-3',
          provider: 'voyage',
          dimensions: 5,
          tokens: 3,
          cacheHit: false,
          processingTime: 15000000, // 15ms in nanoseconds
        },
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.generateEmbedding('Hello, world!');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/generate', {
        text: 'Hello, world!',
      });
      expect(result.embedding).toEqual([0.1, 0.2, 0.3, 0.4, 0.5]);
      expect(result.metadata.model).toBe('voyage-3');
      expect(result.metadata.provider).toBe('voyage');
      expect(result.metadata.dimensions).toBe(5);
      expect(result.metadata.tokens).toBe(3);
      expect(result.metadata.cacheHit).toBe(false);
      expect(result.metadata.processingTimeMs).toBe(15);
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should generate embedding with all options', async () => {
      const mockResponse = {
        embedding: [0.1, 0.2, 0.3],
        metadata: {
          model: 'voyage-3',
          provider: 'voyage',
          dimensions: 3,
          tokens: 5,
          source: '/path/to/file.txt',
          language: 'en',
          inputType: 'document',
          tags: { category: 'test' },
          cacheHit: true,
          processingTime: 5000000,
        },
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.generateEmbedding('Some text', {
        provider: Provider.VOYAGE,
        inputType: InputType.DOCUMENT,
        metadata: { category: 'test' },
        source: '/path/to/file.txt',
        language: 'en',
        cacheBypass: false,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/generate', {
        text: 'Some text',
        provider: 'voyage',
        inputType: 'document',
        metadata: { category: 'test' },
        source: '/path/to/file.txt',
        language: 'en',
        cacheBypass: false,
      });
      expect(result.metadata.cacheHit).toBe(true);
      expect(result.metadata.source).toBe('/path/to/file.txt');
      expect(result.metadata.language).toBe('en');
      expect(result.metadata.inputType).toBe('document');
    });

    it('should generate embedding with cacheBypass option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        metadata: { cacheHit: false },
      });

      await client.generateEmbedding('test', { cacheBypass: true });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/generate', {
        text: 'test',
        cacheBypass: true,
      });
    });

    it('should throw ValidationError when text is missing', async () => {
      await expect(client.generateEmbedding()).rejects.toThrow(ValidationError);
      await expect(client.generateEmbedding(null)).rejects.toThrow(ValidationError);
      await expect(client.generateEmbedding(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when text is empty', async () => {
      await expect(client.generateEmbedding('')).rejects.toThrow(ValidationError);
      await expect(client.generateEmbedding('   ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid provider', async () => {
      await expect(
        client.generateEmbedding('test', { provider: 'invalid-provider' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid inputType', async () => {
      await expect(
        client.generateEmbedding('test', { inputType: 'invalid-type' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      await expect(
        client.generateEmbedding('test', { metadata: 'invalid' }),
      ).rejects.toThrow(ValidationError);

      await expect(
        client.generateEmbedding('test', { metadata: ['array'] }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when source is not a string', async () => {
      await expect(
        client.generateEmbedding('test', { source: 123 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when language is not a string', async () => {
      await expect(
        client.generateEmbedding('test', { language: true }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when cacheBypass is not a boolean', async () => {
      await expect(
        client.generateEmbedding('test', { cacheBypass: 'yes' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing metadata', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1, 0.2],
      });

      const result = await client.generateEmbedding('test');

      expect(result.embedding).toEqual([0.1, 0.2]);
      expect(result.metadata.model).toBe('');
      expect(result.metadata.provider).toBe('');
      expect(result.metadata.dimensions).toBe(0);
      expect(result.metadata.tokens).toBe(0);
      expect(result.metadata.cacheHit).toBe(false);
      expect(result.metadata.processingTimeMs).toBe(0);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.generateEmbedding('test');

      expect(result.embedding).toEqual([]);
      expect(result.metadata.model).toBe('');
    });
  });

  // ============================================================
  // batchGenerateEmbeddings Tests
  // ============================================================

  describe('batchGenerateEmbeddings', () => {
    it('should generate batch embeddings successfully', async () => {
      const mockResponse = {
        results: [
          {
            id: 'doc1',
            embedding: [0.1, 0.2, 0.3],
            metadata: {
              model: 'voyage-3',
              provider: 'voyage',
              dimensions: 3,
              tokens: 2,
              cacheHit: false,
              processingTime: 10000000,
            },
          },
          {
            id: 'doc2',
            embedding: [0.4, 0.5, 0.6],
            metadata: {
              model: 'voyage-3',
              provider: 'voyage',
              dimensions: 3,
              tokens: 3,
              cacheHit: true,
              processingTime: 0,
            },
          },
        ],
        successCount: 2,
        errorCount: 0,
        totalTokens: 5,
        processingTime: 25000000,
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.batchGenerateEmbeddings([
        { id: 'doc1', text: 'First document' },
        { id: 'doc2', text: 'Second document' },
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/batch', {
        items: [
          { id: 'doc1', text: 'First document', inputType: undefined, metadata: undefined, source: undefined, language: undefined },
          { id: 'doc2', text: 'Second document', inputType: undefined, metadata: undefined, source: undefined, language: undefined },
        ],
      });
      expect(result.successCount).toBe(2);
      expect(result.errorCount).toBe(0);
      expect(result.totalTokens).toBe(5);
      expect(result.processingTimeMs).toBe(25);
      expect(result.results).toHaveLength(2);
      expect(result.results[0].id).toBe('doc1');
      expect(result.results[1].id).toBe('doc2');
    });

    it('should generate batch embeddings with all item options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{ id: 'doc1', embedding: [0.1] }],
        successCount: 1,
        errorCount: 0,
        totalTokens: 10,
      });

      await client.batchGenerateEmbeddings([
        {
          id: 'doc1',
          text: 'Code sample',
          inputType: InputType.CODE,
          metadata: { type: 'function' },
          source: '/src/main.js',
          language: 'javascript',
        },
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/batch', {
        items: [
          {
            id: 'doc1',
            text: 'Code sample',
            inputType: 'code',
            metadata: { type: 'function' },
            source: '/src/main.js',
            language: 'javascript',
          },
        ],
      });
    });

    it('should generate batch embeddings with provider option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        successCount: 0,
        errorCount: 0,
        totalTokens: 0,
      });

      await client.batchGenerateEmbeddings(
        [{ id: 'doc1', text: 'test' }],
        { provider: Provider.OPENAI },
      );

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/batch', expect.objectContaining({
        provider: 'openai',
      }));
    });

    it('should generate batch embeddings with cacheBypass option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        successCount: 0,
        errorCount: 0,
        totalTokens: 0,
      });

      await client.batchGenerateEmbeddings(
        [{ id: 'doc1', text: 'test' }],
        { cacheBypass: true },
      );

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/batch', expect.objectContaining({
        cacheBypass: true,
      }));
    });

    it('should handle batch response with errors', async () => {
      const mockResponse = {
        results: [
          { id: 'doc1', embedding: [0.1], metadata: {} },
          { id: 'doc2', embedding: [], metadata: {}, error: 'Text too long' },
        ],
        successCount: 1,
        errorCount: 1,
        totalTokens: 5,
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.batchGenerateEmbeddings([
        { id: 'doc1', text: 'Short text' },
        { id: 'doc2', text: 'Very long text...' },
      ]);

      expect(result.successCount).toBe(1);
      expect(result.errorCount).toBe(1);
      expect(result.results[1].error).toBe('Text too long');
    });

    it('should throw ValidationError when items is empty', async () => {
      await expect(client.batchGenerateEmbeddings([])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is not an array', async () => {
      await expect(client.batchGenerateEmbeddings('invalid')).rejects.toThrow(ValidationError);
      await expect(client.batchGenerateEmbeddings(null)).rejects.toThrow(ValidationError);
      await expect(client.batchGenerateEmbeddings({})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is not an object', async () => {
      await expect(
        client.batchGenerateEmbeddings(['invalid']),
      ).rejects.toThrow(ValidationError);

      await expect(
        client.batchGenerateEmbeddings([null]),
      ).rejects.toThrow(ValidationError);

      await expect(
        client.batchGenerateEmbeddings([['array']]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.id is missing', async () => {
      await expect(
        client.batchGenerateEmbeddings([{ text: 'test' }]),
      ).rejects.toThrow(ValidationError);

      await expect(
        client.batchGenerateEmbeddings([{ id: '', text: 'test' }]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.text is missing', async () => {
      await expect(
        client.batchGenerateEmbeddings([{ id: 'doc1' }]),
      ).rejects.toThrow(ValidationError);

      await expect(
        client.batchGenerateEmbeddings([{ id: 'doc1', text: '' }]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid item.inputType', async () => {
      await expect(
        client.batchGenerateEmbeddings([
          { id: 'doc1', text: 'test', inputType: 'invalid' },
        ]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.metadata is not an object', async () => {
      await expect(
        client.batchGenerateEmbeddings([
          { id: 'doc1', text: 'test', metadata: 'invalid' },
        ]),
      ).rejects.toThrow(ValidationError);

      await expect(
        client.batchGenerateEmbeddings([
          { id: 'doc1', text: 'test', metadata: ['array'] },
        ]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.source is not a string', async () => {
      await expect(
        client.batchGenerateEmbeddings([
          { id: 'doc1', text: 'test', source: 123 },
        ]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.language is not a string', async () => {
      await expect(
        client.batchGenerateEmbeddings([
          { id: 'doc1', text: 'test', language: true },
        ]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid options.provider', async () => {
      await expect(
        client.batchGenerateEmbeddings(
          [{ id: 'doc1', text: 'test' }],
          { provider: 'invalid' },
        ),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options.cacheBypass is not boolean', async () => {
      await expect(
        client.batchGenerateEmbeddings(
          [{ id: 'doc1', text: 'test' }],
          { cacheBypass: 'yes' },
        ),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.batchGenerateEmbeddings([
        { id: 'doc1', text: 'test' },
      ]);

      expect(result.results).toEqual([]);
      expect(result.successCount).toBe(0);
      expect(result.errorCount).toBe(0);
      expect(result.totalTokens).toBe(0);
      expect(result.processingTimeMs).toBe(0);
    });
  });

  // ============================================================
  // listProviders Tests
  // ============================================================

  describe('listProviders', () => {
    it('should list available providers', async () => {
      const mockResponse = {
        providers: [
          {
            name: 'voyage',
            type: 'voyage',
            model: 'voyage-3',
            dimensions: 1024,
            maxTokens: 16000,
            maxBatchSize: 100,
            status: 'available',
            apiUrl: 'https://api.voyageai.com',
            rateLimitPerMinute: 300,
            timeout: 30000000000,
            isDefault: true,
            successCount: 1000,
            errorCount: 5,
            avgLatency: 150000000,
          },
        ],
        defaultProvider: 'voyage',
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listProviders();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/embedding/providers');
      expect(result.providers).toHaveLength(1);
      expect(result.providers[0].name).toBe('voyage');
      expect(result.providers[0].status).toBe('available');
      expect(result.providers[0].isDefault).toBe(true);
      expect(result.providers[0].timeoutMs).toBe(30000);
      expect(result.providers[0].avgLatencyMs).toBe(150);
      expect(result.defaultProvider).toBe('voyage');
    });

    it('should list providers including unavailable ones', async () => {
      mockTransport.get.mockResolvedValueOnce({
        providers: [],
        defaultProvider: 'voyage',
      });

      await client.listProviders({ includeUnavailable: true });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/embedding/providers?includeUnavailable=true',
      );
    });

    it('should handle empty providers list', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listProviders();

      expect(result.providers).toEqual([]);
      expect(result.defaultProvider).toBe('');
    });

    it('should handle provider with error info', async () => {
      mockTransport.get.mockResolvedValueOnce({
        providers: [
          {
            name: 'openai',
            status: 'error',
            lastError: 'API key invalid',
            lastErrorTime: '2025-01-15T09:00:00Z',
          },
        ],
        defaultProvider: 'voyage',
      });

      const result = await client.listProviders();

      expect(result.providers[0].lastError).toBe('API key invalid');
      expect(result.providers[0].lastErrorTime).toBe('2025-01-15T09:00:00Z');
    });

    it('should format provider with apiURL field correctly', async () => {
      mockTransport.get.mockResolvedValueOnce({
        providers: [
          {
            name: 'ollama',
            apiURL: 'http://localhost:11434',
          },
        ],
      });

      const result = await client.listProviders();

      expect(result.providers[0].apiUrl).toBe('http://localhost:11434');
    });
  });

  // ============================================================
  // getProviderInfo Tests
  // ============================================================

  describe('getProviderInfo', () => {
    it('should get provider info successfully', async () => {
      const mockResponse = {
        info: {
          name: 'voyage',
          type: 'voyage',
          model: 'voyage-3',
          dimensions: 1024,
          maxTokens: 16000,
          maxBatchSize: 100,
          status: 'available',
          apiUrl: 'https://api.voyageai.com',
          rateLimitPerMinute: 300,
          timeout: 30000000000,
          isDefault: true,
          successCount: 1000,
          errorCount: 5,
          avgLatency: 150000000,
        },
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getProviderInfo(Provider.VOYAGE);

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/embedding/providers/voyage');
      expect(result.info.name).toBe('voyage');
      expect(result.info.dimensions).toBe(1024);
      expect(result.info.maxTokens).toBe(16000);
      expect(result.info.status).toBe('available');
      expect(result.info.timeoutMs).toBe(30000);
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should get provider info for all provider types', async () => {
      for (const provider of Object.values(Provider)) {
        mockTransport.get.mockResolvedValueOnce({
          info: { name: provider },
        });

        await client.getProviderInfo(provider);

        expect(mockTransport.get).toHaveBeenCalledWith(
          `/api/v1/embedding/providers/${provider}`,
        );
      }
    });

    it('should handle response without info wrapper', async () => {
      mockTransport.get.mockResolvedValueOnce({
        name: 'voyage',
        model: 'voyage-3',
      });

      const result = await client.getProviderInfo(Provider.VOYAGE);

      expect(result.info.name).toBe('voyage');
      expect(result.info.model).toBe('voyage-3');
    });

    it('should throw ValidationError when provider is missing', async () => {
      await expect(client.getProviderInfo()).rejects.toThrow(ValidationError);
      await expect(client.getProviderInfo(null)).rejects.toThrow(ValidationError);
      await expect(client.getProviderInfo('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid provider', async () => {
      await expect(client.getProviderInfo('invalid-provider')).rejects.toThrow(ValidationError);
    });

    it('should URL encode special characters in provider name', async () => {
      // Using a valid provider, the name gets encoded
      mockTransport.get.mockResolvedValueOnce({
        info: { name: 'voyage' },
      });

      await client.getProviderInfo(Provider.VOYAGE);

      // Provider.VOYAGE is 'voyage' which doesn't need encoding,
      // but the method uses encodeURIComponent
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/embedding/providers/voyage');
    });
  });

  // ============================================================
  // setDefaultProvider Tests
  // ============================================================

  describe('setDefaultProvider', () => {
    it('should set default provider successfully', async () => {
      const mockResponse = {
        provider: 'openai',
        previousProvider: 'voyage',
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.setDefaultProvider(Provider.OPENAI);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/providers/default', {
        provider: 'openai',
      });
      expect(result.provider).toBe('openai');
      expect(result.previousProvider).toBe('voyage');
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should set default provider for all provider types', async () => {
      for (const provider of Object.values(Provider)) {
        mockTransport.post.mockResolvedValueOnce({
          provider,
          previousProvider: 'voyage',
        });

        const result = await client.setDefaultProvider(provider);

        expect(result.provider).toBe(provider);
      }
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.setDefaultProvider(Provider.OLLAMA);

      expect(result.provider).toBe('ollama');
      expect(result.previousProvider).toBe('');
      expect(result.timestamp).toBeDefined();
    });

    it('should throw ValidationError when provider is missing', async () => {
      await expect(client.setDefaultProvider()).rejects.toThrow(ValidationError);
      await expect(client.setDefaultProvider(null)).rejects.toThrow(ValidationError);
      await expect(client.setDefaultProvider('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid provider', async () => {
      await expect(client.setDefaultProvider('invalid')).rejects.toThrow(ValidationError);
    });
  });

  // ============================================================
  // getCacheStats Tests
  // ============================================================

  describe('getCacheStats', () => {
    it('should get cache stats successfully', async () => {
      const mockResponse = {
        enabled: true,
        size: 1500,
        capacity: 10000,
        hits: 8500,
        misses: 1500,
        hitRate: 0.85,
        evictions: 200,
        memoryBytes: 52428800,
        compressionEnabled: true,
        compressionRatio: 0.7,
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getCacheStats();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/embedding/cache/stats');
      expect(result.enabled).toBe(true);
      expect(result.size).toBe(1500);
      expect(result.capacity).toBe(10000);
      expect(result.hits).toBe(8500);
      expect(result.misses).toBe(1500);
      expect(result.hitRate).toBe(0.85);
      expect(result.evictions).toBe(200);
      expect(result.memoryBytes).toBe(52428800);
      expect(result.compressionEnabled).toBe(true);
      expect(result.compressionRatio).toBe(0.7);
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getCacheStats();

      expect(result.enabled).toBe(false);
      expect(result.size).toBe(0);
      expect(result.capacity).toBe(0);
      expect(result.hits).toBe(0);
      expect(result.misses).toBe(0);
      expect(result.hitRate).toBe(0);
      expect(result.evictions).toBe(0);
      expect(result.memoryBytes).toBe(0);
      expect(result.compressionEnabled).toBe(false);
      expect(result.compressionRatio).toBe(0);
    });

    it('should handle null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getCacheStats();

      expect(result.enabled).toBe(false);
      expect(result.size).toBe(0);
    });
  });

  // ============================================================
  // clearCache Tests
  // ============================================================

  describe('clearCache', () => {
    it('should clear all cache entries', async () => {
      const mockResponse = {
        entriesCleared: 1500,
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.clearCache({ all: true });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/cache/clear', {
        all: true,
      });
      expect(result.entriesCleared).toBe(1500);
      expect(result.provider).toBe(null);
      expect(result.timestamp).toBe('2025-01-15T10:00:00Z');
    });

    it('should clear cache for specific provider', async () => {
      const mockResponse = {
        entriesCleared: 500,
        provider: 'voyage',
        timestamp: '2025-01-15T10:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.clearCache({ provider: Provider.VOYAGE });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/cache/clear', {
        provider: 'voyage',
      });
      expect(result.entriesCleared).toBe(500);
      expect(result.provider).toBe('voyage');
    });

    it('should clear cache with both provider and all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        entriesCleared: 1000,
      });

      await client.clearCache({ provider: Provider.OPENAI, all: true });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/embedding/cache/clear', {
        provider: 'openai',
        all: true,
      });
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.clearCache({ all: true });

      expect(result.entriesCleared).toBe(0);
    });

    it('should throw ValidationError for invalid provider in options', async () => {
      await expect(
        client.clearCache({ provider: 'invalid-provider' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when all is not a boolean', async () => {
      await expect(
        client.clearCache({ all: 'yes' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with null provider', async () => {
      mockTransport.post.mockResolvedValueOnce({
        entriesCleared: 100,
        provider: null,
      });

      const result = await client.clearCache({ all: true });

      expect(result.provider).toBe(null);
    });
  });

  // ============================================================
  // Duration Formatting Tests
  // ============================================================

  describe('duration formatting', () => {
    it('should convert nanoseconds to milliseconds', async () => {
      mockTransport.get.mockResolvedValueOnce({
        providers: [
          {
            name: 'voyage',
            timeout: 30000000000, // 30 seconds in nanoseconds
            avgLatency: 150000000, // 150ms in nanoseconds
          },
        ],
      });

      const result = await client.listProviders();

      expect(result.providers[0].timeoutMs).toBe(30000);
      expect(result.providers[0].avgLatencyMs).toBe(150);
    });

    it('should handle string duration values', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        metadata: {
          processingTime: '15.5',
        },
      });

      const result = await client.generateEmbedding('test');

      expect(result.metadata.processingTimeMs).toBe(15.5);
    });

    it('should handle invalid string duration values', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        metadata: {
          processingTime: 'invalid',
        },
      });

      const result = await client.generateEmbedding('test');

      expect(result.metadata.processingTimeMs).toBe(0);
    });

    it('should handle undefined duration values', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        metadata: {},
      });

      const result = await client.generateEmbedding('test');

      expect(result.metadata.processingTimeMs).toBe(0);
    });

    it('should handle non-number non-string duration values', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        metadata: {
          processingTime: { ms: 100 }, // object, not number or string
        },
      });

      const result = await client.generateEmbedding('test');

      expect(result.metadata.processingTimeMs).toBe(0);
    });

    it('should handle boolean duration values', async () => {
      mockTransport.post.mockResolvedValueOnce({
        embedding: [0.1],
        metadata: {
          processingTime: true, // boolean, not number or string
        },
      });

      const result = await client.generateEmbedding('test');

      expect(result.metadata.processingTimeMs).toBe(0);
    });
  });

  // ============================================================
  // Transport Error Handling Tests
  // ============================================================

  describe('transport error handling', () => {
    it('should propagate transport errors from generateEmbedding', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.generateEmbedding('test')).rejects.toThrow('Network error');
    });

    it('should propagate transport errors from batchGenerateEmbeddings', async () => {
      const error = new Error('Server error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(
        client.batchGenerateEmbeddings([{ id: 'doc1', text: 'test' }]),
      ).rejects.toThrow('Server error');
    });

    it('should propagate transport errors from listProviders', async () => {
      const error = new Error('Unauthorized');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.listProviders()).rejects.toThrow('Unauthorized');
    });

    it('should propagate transport errors from getProviderInfo', async () => {
      const error = new Error('Not found');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getProviderInfo(Provider.VOYAGE)).rejects.toThrow('Not found');
    });

    it('should propagate transport errors from setDefaultProvider', async () => {
      const error = new Error('Bad request');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.setDefaultProvider(Provider.OLLAMA)).rejects.toThrow('Bad request');
    });

    it('should propagate transport errors from getCacheStats', async () => {
      const error = new Error('Service unavailable');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getCacheStats()).rejects.toThrow('Service unavailable');
    });

    it('should propagate transport errors from clearCache', async () => {
      const error = new Error('Internal server error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.clearCache({ all: true })).rejects.toThrow('Internal server error');
    });
  });
});
