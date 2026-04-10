/**
 * HealthClient Tests
 *
 * Comprehensive tests for the QuicBaaS health check client.
 */

import HealthClient, { HealthStatus } from '../../src/clients/health.js';
import { ValidationError } from '../../src/errors/index.js';

describe('HealthClient', () => {
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
    client = new HealthClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const c = new HealthClient(mockTransport);
      expect(c).toBeInstanceOf(HealthClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new HealthClient()).toThrow(ValidationError);
      expect(() => new HealthClient(null)).toThrow(ValidationError);
      expect(() => new HealthClient(undefined)).toThrow(ValidationError);
    });
  });

  describe('health', () => {
    it('should return health status for healthy service', async () => {
      const mockResponse = {
        healthy: true,
        status: 'healthy',
        timestamp: '2025-01-15T00:00:00Z',
        version: '1.0.0',
        uptime: '24h',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.health();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health', { query: {} });
      expect(result.healthy).toBe(true);
      expect(result.status).toBe('healthy');
      expect(result.timestamp).toBe('2025-01-15T00:00:00Z');
      expect(result.version).toBe('1.0.0');
      expect(result.uptime).toBe('24h');
    });

    it('should return health status for unhealthy service', async () => {
      const mockResponse = {
        healthy: false,
        status: 'unhealthy',
        timestamp: '2025-01-15T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.health();

      expect(result.healthy).toBe(false);
      expect(result.status).toBe('unhealthy');
    });

    it('should return health status for degraded service', async () => {
      const mockResponse = {
        healthy: false,
        status: 'degraded',
        timestamp: '2025-01-15T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.health();

      expect(result.healthy).toBe(false);
      expect(result.status).toBe('degraded');
    });

    it('should include details when includeDetails is true', async () => {
      const mockResponse = {
        healthy: true,
        status: 'healthy',
        timestamp: '2025-01-15T00:00:00Z',
        details: {
          storage: { status: 'healthy', latencyMs: 5 },
          raft: { status: 'healthy', latencyMs: 2 },
        },
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.health({ includeDetails: true });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health', {
        query: { details: 'true' },
      });
      expect(result.details).toEqual({
        storage: { status: 'healthy', latencyMs: 5 },
        raft: { status: 'healthy', latencyMs: 2 },
      });
    });

    it('should not include details when includeDetails is false', async () => {
      mockTransport.get.mockResolvedValueOnce({ status: 'healthy' });

      await client.health({ includeDetails: false });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health', { query: {} });
    });

    it('should filter by specific components', async () => {
      mockTransport.get.mockResolvedValueOnce({ status: 'healthy' });

      await client.health({ components: ['storage', 'raft'] });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health', {
        query: { components: 'storage,raft' },
      });
    });

    it('should handle both includeDetails and components', async () => {
      mockTransport.get.mockResolvedValueOnce({ status: 'healthy' });

      await client.health({ includeDetails: true, components: ['storage'] });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health', {
        query: { details: 'true', components: 'storage' },
      });
    });

    it('should throw ValidationError when includeDetails is not boolean', async () => {
      await expect(client.health({ includeDetails: 'true' })).rejects.toThrow(ValidationError);
      await expect(client.health({ includeDetails: 1 })).rejects.toThrow(ValidationError);
      await expect(client.health({ includeDetails: null })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when components is not an array', async () => {
      await expect(client.health({ components: 'storage' })).rejects.toThrow(ValidationError);
      await expect(client.health({ components: { name: 'storage' } })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when component is not a non-empty string', async () => {
      await expect(client.health({ components: [''] })).rejects.toThrow(ValidationError);
      await expect(client.health({ components: ['storage', ''] })).rejects.toThrow(ValidationError);
      await expect(client.health({ components: [123] })).rejects.toThrow(ValidationError);
      await expect(client.health({ components: [null] })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when component contains only whitespace', async () => {
      await expect(client.health({ components: ['   '] })).rejects.toThrow(ValidationError);
    });

    it('should handle empty components array', async () => {
      mockTransport.get.mockResolvedValueOnce({ status: 'healthy' });

      await client.health({ components: [] });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health', { query: {} });
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.health();

      expect(result.healthy).toBe(true);
      expect(result.status).toBe(HealthStatus.HEALTHY);
      expect(result.timestamp).toBeDefined();
    });

    it('should derive healthy from status when not explicitly provided', async () => {
      mockTransport.get.mockResolvedValueOnce({ status: 'healthy' });

      const result = await client.health();

      expect(result.healthy).toBe(true);
    });

    it('should not include undefined optional fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        healthy: true,
        status: 'healthy',
        timestamp: '2025-01-15T00:00:00Z',
      });

      const result = await client.health();

      expect(result).not.toHaveProperty('details');
      expect(result).not.toHaveProperty('version');
      expect(result).not.toHaveProperty('uptime');
    });

    it('should not include null details', async () => {
      mockTransport.get.mockResolvedValueOnce({
        healthy: true,
        status: 'healthy',
        timestamp: '2025-01-15T00:00:00Z',
        details: null,
      });

      const result = await client.health();

      expect(result).not.toHaveProperty('details');
    });
  });

  describe('liveness', () => {
    it('should return liveness status for alive service', async () => {
      const mockResponse = {
        alive: true,
        status: 'healthy',
        timestamp: '2025-01-15T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.liveness();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health/live');
      expect(result.alive).toBe(true);
      expect(result.status).toBe('healthy');
      expect(result.timestamp).toBe('2025-01-15T00:00:00Z');
    });

    it('should return liveness status for dead service', async () => {
      const mockResponse = {
        alive: false,
        status: 'unhealthy',
        timestamp: '2025-01-15T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.liveness();

      expect(result.alive).toBe(false);
      expect(result.status).toBe('unhealthy');
    });

    it('should derive alive from healthy when alive not provided', async () => {
      mockTransport.get.mockResolvedValueOnce({
        healthy: false,
        status: 'unhealthy',
      });

      const result = await client.liveness();

      expect(result.alive).toBe(false);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.liveness();

      expect(result.alive).toBe(true);
      expect(result.status).toBe(HealthStatus.HEALTHY);
      expect(result.timestamp).toBeDefined();
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.liveness();

      expect(result.alive).toBe(true);
      expect(result.status).toBe(HealthStatus.HEALTHY);
      expect(result.timestamp).toBeDefined();
    });
  });

  describe('readiness', () => {
    it('should return readiness status for ready service', async () => {
      const mockResponse = {
        ready: true,
        status: 'healthy',
        timestamp: '2025-01-15T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.readiness();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/health/ready');
      expect(result.ready).toBe(true);
      expect(result.status).toBe('healthy');
      expect(result.timestamp).toBe('2025-01-15T00:00:00Z');
      expect(result.reason).toBeNull();
    });

    it('should return readiness status for not ready service', async () => {
      const mockResponse = {
        ready: false,
        status: 'unhealthy',
        reason: 'Database connection failed',
        timestamp: '2025-01-15T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.readiness();

      expect(result.ready).toBe(false);
      expect(result.status).toBe('unhealthy');
      expect(result.reason).toBe('Database connection failed');
    });

    it('should derive ready from healthy when ready not provided', async () => {
      mockTransport.get.mockResolvedValueOnce({
        healthy: false,
        status: 'degraded',
      });

      const result = await client.readiness();

      expect(result.ready).toBe(false);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.readiness();

      expect(result.ready).toBe(true);
      expect(result.status).toBe(HealthStatus.HEALTHY);
      expect(result.reason).toBeNull();
      expect(result.timestamp).toBeDefined();
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.readiness();

      expect(result.ready).toBe(true);
      expect(result.status).toBe(HealthStatus.HEALTHY);
      expect(result.reason).toBeNull();
      expect(result.timestamp).toBeDefined();
    });
  });

  describe('HealthStatus enum', () => {
    it('should export HealthStatus enum with correct values', () => {
      expect(HealthStatus.HEALTHY).toBe('healthy');
      expect(HealthStatus.DEGRADED).toBe('degraded');
      expect(HealthStatus.UNHEALTHY).toBe('unhealthy');
    });

    it('should have frozen HealthStatus enum', () => {
      expect(Object.isFrozen(HealthStatus)).toBe(true);
    });
  });
});
