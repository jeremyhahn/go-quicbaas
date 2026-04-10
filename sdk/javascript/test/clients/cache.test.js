/**
 * CacheClient Tests
 *
 * Comprehensive test suite for the QuicBaaS Cache Client.
 * Tests all public methods with success and error cases.
 */

import CacheClient from '../../src/clients/cache.js';
import { ValidationError } from '../../src/errors/index.js';

describe('CacheClient', () => {
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
    client = new CacheClient(mockTransport);
  });

  // ============================================================
  // Constructor Tests
  // ============================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const newClient = new CacheClient(mockTransport);
      expect(newClient).toBeInstanceOf(CacheClient);
    });

    it('should throw ValidationError when transport is not provided', () => {
      expect(() => new CacheClient()).toThrow(ValidationError);
      expect(() => new CacheClient(null)).toThrow(ValidationError);
      expect(() => new CacheClient(undefined)).toThrow(ValidationError);
    });
  });

  // ============================================================
  // Static Properties Tests
  // ============================================================

  describe('static properties', () => {
    it('should have correct API_PREFIX', () => {
      expect(CacheClient.API_PREFIX).toBe('/api/v1/cache');
    });
  });

  // ============================================================
  // getCacheStats Tests
  // ============================================================

  describe('getCacheStats', () => {
    it('should get cache statistics without options', async () => {
      const mockResponse = {
        hitRate: 0.85,
        size: 1500,
        capacity: 10000,
        hits: 8500,
        misses: 1500,
        evictions: 200,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getCacheStats();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cache/stats', { query: {} });
      expect(result.hitRate).toBe(0.85);
      expect(result.size).toBe(1500);
      expect(result.capacity).toBe(10000);
      expect(result.hits).toBe(8500);
      expect(result.misses).toBe(1500);
      expect(result.evictions).toBe(200);
    });

    it('should get cache statistics with entityType filter', async () => {
      const mockResponse = {
        hitRate: 0.9,
        size: 500,
        capacity: 10000,
        hits: 4500,
        misses: 500,
        evictions: 50,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getCacheStats({ entityType: 'user' });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cache/stats', {
        query: { entityType: 'user' },
      });
      expect(result.hitRate).toBe(0.9);
    });

    it('should handle maxSize field in response as capacity', async () => {
      mockTransport.get.mockResolvedValueOnce({
        hitRate: 0.5,
        size: 100,
        maxSize: 1000,
        hits: 50,
        misses: 50,
        evictions: 0,
      });

      const result = await client.getCacheStats();

      expect(result.capacity).toBe(1000);
    });

    it('should prefer maxSize over capacity if both present', async () => {
      mockTransport.get.mockResolvedValueOnce({
        hitRate: 0.5,
        size: 100,
        capacity: 2000,
        maxSize: 1000,
        hits: 50,
        misses: 50,
        evictions: 0,
      });

      const result = await client.getCacheStats();

      // Implementation uses maxSize first, then falls back to capacity
      expect(result.capacity).toBe(1000);
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getCacheStats();

      expect(result.hitRate).toBe(0);
      expect(result.size).toBe(0);
      expect(result.capacity).toBe(0);
      expect(result.hits).toBe(0);
      expect(result.misses).toBe(0);
      expect(result.evictions).toBe(0);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getCacheStats();

      expect(result.hitRate).toBe(0);
      expect(result.size).toBe(0);
      expect(result.capacity).toBe(0);
    });

    it('should throw ValidationError when options is null', async () => {
      await expect(client.getCacheStats(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is not an object', async () => {
      await expect(client.getCacheStats('invalid')).rejects.toThrow(ValidationError);
      await expect(client.getCacheStats(123)).rejects.toThrow(ValidationError);
      await expect(client.getCacheStats(['array'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is not a string', async () => {
      await expect(client.getCacheStats({ entityType: 123 })).rejects.toThrow(ValidationError);
      await expect(client.getCacheStats({ entityType: {} })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is empty string', async () => {
      await expect(client.getCacheStats({ entityType: '' })).rejects.toThrow(ValidationError);
      await expect(client.getCacheStats({ entityType: '   ' })).rejects.toThrow(ValidationError);
    });

    it('should ignore null entityType value', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getCacheStats({ entityType: null });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cache/stats', { query: {} });
    });
  });

  // ============================================================
  // clearCache Tests
  // ============================================================

  describe('clearCache', () => {
    it('should clear all cache entries', async () => {
      const mockResponse = {
        entriesCleared: 1500,
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.clearCache({ all: true });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cache/clear', {
        all: true,
      });
      expect(result.entriesCleared).toBe(1500);
    });

    it('should clear cache for specific entity type', async () => {
      const mockResponse = {
        entriesCleared: 500,
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.clearCache({ entityType: 'session' });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cache/clear', {
        entityType: 'session',
      });
      expect(result.entriesCleared).toBe(500);
    });

    it('should clear cache with both entityType and all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        entriesCleared: 1000,
      });

      await client.clearCache({ entityType: 'user', all: true });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cache/clear', {
        entityType: 'user',
        all: true,
      });
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.clearCache({ all: true });

      expect(result.entriesCleared).toBe(0);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.clearCache({ all: true });

      expect(result.entriesCleared).toBe(0);
    });

    it('should throw ValidationError when options is null', async () => {
      await expect(client.clearCache(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is not an object', async () => {
      await expect(client.clearCache('invalid')).rejects.toThrow(ValidationError);
      await expect(client.clearCache(123)).rejects.toThrow(ValidationError);
      await expect(client.clearCache(['array'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when neither entityType nor all is specified', async () => {
      await expect(client.clearCache({})).rejects.toThrow(ValidationError);
      await expect(client.clearCache({ all: false })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is not a string', async () => {
      await expect(client.clearCache({ entityType: 123 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is empty string', async () => {
      await expect(client.clearCache({ entityType: '' })).rejects.toThrow(ValidationError);
      await expect(client.clearCache({ entityType: '   ' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when all is not a boolean', async () => {
      await expect(client.clearCache({ all: 'yes' })).rejects.toThrow(ValidationError);
      await expect(client.clearCache({ all: 1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when all is invalid even with entityType present', async () => {
      // This tests the validation path where entityType is valid but all is invalid
      await expect(client.clearCache({ entityType: 'user', all: 'invalid' })).rejects.toThrow(ValidationError);
      await expect(client.clearCache({ entityType: 'user', all: 0 })).rejects.toThrow(ValidationError);
    });

    it('should not include all in body when false', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      await client.clearCache({ entityType: 'user', all: false });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cache/clear', {
        entityType: 'user',
      });
    });
  });

  // ============================================================
  // getCacheConfig Tests
  // ============================================================

  describe('getCacheConfig', () => {
    it('should get cache configuration', async () => {
      const mockResponse = {
        l1Size: 10000,
        l1TTL: 300000,
        cleanupInterval: 60000,
        enabled: true,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getCacheConfig();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cache/config');
      expect(result.l1Size).toBe(10000);
      expect(result.l1TTL).toBe(300000);
      expect(result.cleanupInterval).toBe(60000);
      expect(result.enabled).toBe(true);
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getCacheConfig();

      expect(result.l1Size).toBe(0);
      expect(result.l1TTL).toBe(0);
      expect(result.cleanupInterval).toBe(0);
      expect(result.enabled).toBe(false);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getCacheConfig();

      expect(result.l1Size).toBe(0);
      expect(result.l1TTL).toBe(0);
      expect(result.cleanupInterval).toBe(0);
      expect(result.enabled).toBe(false);
    });

    it('should handle partial response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        l1Size: 5000,
        enabled: true,
      });

      const result = await client.getCacheConfig();

      expect(result.l1Size).toBe(5000);
      expect(result.l1TTL).toBe(0);
      expect(result.cleanupInterval).toBe(0);
      expect(result.enabled).toBe(true);
    });
  });

  // ============================================================
  // updateCacheConfig Tests
  // ============================================================

  describe('updateCacheConfig', () => {
    it('should update l1Size', async () => {
      const mockResponse = {
        updated: true,
        newConfig: {
          l1Size: 20000,
          l1TTL: 300000,
          cleanupInterval: 60000,
          enabled: true,
        },
      };
      mockTransport.put.mockResolvedValueOnce(mockResponse);

      const result = await client.updateCacheConfig({ l1Size: 20000 });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        l1Size: 20000,
      });
      expect(result.updated).toBe(true);
      expect(result.newConfig.l1Size).toBe(20000);
    });

    it('should update l1TTL', async () => {
      mockTransport.put.mockResolvedValueOnce({
        updated: true,
        newConfig: { l1TTL: 600000 },
      });

      const result = await client.updateCacheConfig({ l1TTL: 600000 });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        l1TTL: 600000,
      });
      expect(result.updated).toBe(true);
    });

    it('should update cleanupInterval', async () => {
      mockTransport.put.mockResolvedValueOnce({
        updated: true,
        newConfig: { cleanupInterval: 120000 },
      });

      const result = await client.updateCacheConfig({ cleanupInterval: 120000 });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        cleanupInterval: 120000,
      });
      expect(result.updated).toBe(true);
    });

    it('should update enabled status', async () => {
      mockTransport.put.mockResolvedValueOnce({
        updated: true,
        newConfig: { enabled: false },
      });

      const result = await client.updateCacheConfig({ enabled: false });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        enabled: false,
      });
      expect(result.updated).toBe(true);
    });

    it('should update multiple fields at once', async () => {
      mockTransport.put.mockResolvedValueOnce({
        updated: true,
        newConfig: {
          l1Size: 15000,
          l1TTL: 600000,
          cleanupInterval: 120000,
          enabled: true,
        },
      });

      const result = await client.updateCacheConfig({
        l1Size: 15000,
        l1TTL: 600000,
        cleanupInterval: 120000,
        enabled: true,
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        l1Size: 15000,
        l1TTL: 600000,
        cleanupInterval: 120000,
        enabled: true,
      });
      expect(result.updated).toBe(true);
      expect(result.newConfig.l1Size).toBe(15000);
    });

    it('should handle response without newConfig', async () => {
      mockTransport.put.mockResolvedValueOnce({
        updated: true,
      });

      const result = await client.updateCacheConfig({ l1Size: 5000 });

      expect(result.updated).toBe(true);
      expect(result.newConfig).toBeUndefined();
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.updateCacheConfig({ l1Size: 5000 });

      expect(result.updated).toBe(false);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.put.mockResolvedValueOnce(null);

      const result = await client.updateCacheConfig({ l1Size: 5000 });

      expect(result.updated).toBe(false);
    });

    it('should throw ValidationError when config is missing', async () => {
      await expect(client.updateCacheConfig()).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig(null)).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when config is not an object', async () => {
      await expect(client.updateCacheConfig('invalid')).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig(123)).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig(['array'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when no fields are specified', async () => {
      await expect(client.updateCacheConfig({})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when l1Size is not an integer', async () => {
      await expect(client.updateCacheConfig({ l1Size: 10.5 })).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig({ l1Size: '10000' })).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig({ l1Size: NaN })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when l1Size is negative', async () => {
      await expect(client.updateCacheConfig({ l1Size: -1 })).rejects.toThrow(ValidationError);
    });

    it('should accept l1Size of zero', async () => {
      mockTransport.put.mockResolvedValueOnce({ updated: true });

      await client.updateCacheConfig({ l1Size: 0 });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        l1Size: 0,
      });
    });

    it('should throw ValidationError when l1TTL is not an integer', async () => {
      await expect(client.updateCacheConfig({ l1TTL: 10.5 })).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig({ l1TTL: '60000' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when l1TTL is negative', async () => {
      await expect(client.updateCacheConfig({ l1TTL: -1 })).rejects.toThrow(ValidationError);
    });

    it('should accept l1TTL of zero', async () => {
      mockTransport.put.mockResolvedValueOnce({ updated: true });

      await client.updateCacheConfig({ l1TTL: 0 });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        l1TTL: 0,
      });
    });

    it('should throw ValidationError when cleanupInterval is not an integer', async () => {
      await expect(client.updateCacheConfig({ cleanupInterval: 30.5 })).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig({ cleanupInterval: '30000' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when cleanupInterval is negative', async () => {
      await expect(client.updateCacheConfig({ cleanupInterval: -1 })).rejects.toThrow(ValidationError);
    });

    it('should accept cleanupInterval of zero', async () => {
      mockTransport.put.mockResolvedValueOnce({ updated: true });

      await client.updateCacheConfig({ cleanupInterval: 0 });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        cleanupInterval: 0,
      });
    });

    it('should throw ValidationError when enabled is not a boolean', async () => {
      await expect(client.updateCacheConfig({ enabled: 'true' })).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig({ enabled: 1 })).rejects.toThrow(ValidationError);
      await expect(client.updateCacheConfig({ enabled: null })).rejects.toThrow(ValidationError);
    });
  });

  // ============================================================
  // Transport Error Handling Tests
  // ============================================================

  describe('transport error handling', () => {
    it('should propagate transport errors from getCacheStats', async () => {
      const error = new Error('Network error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getCacheStats()).rejects.toThrow('Network error');
    });

    it('should propagate transport errors from clearCache', async () => {
      const error = new Error('Server error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.clearCache({ all: true })).rejects.toThrow('Server error');
    });

    it('should propagate transport errors from getCacheConfig', async () => {
      const error = new Error('Unauthorized');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getCacheConfig()).rejects.toThrow('Unauthorized');
    });

    it('should propagate transport errors from updateCacheConfig', async () => {
      const error = new Error('Bad request');
      mockTransport.put.mockRejectedValueOnce(error);

      await expect(client.updateCacheConfig({ l1Size: 5000 })).rejects.toThrow('Bad request');
    });
  });

  // ============================================================
  // Edge Cases Tests
  // ============================================================

  describe('edge cases', () => {
    it('should handle very large cache stats values', async () => {
      mockTransport.get.mockResolvedValueOnce({
        hitRate: 0.999999,
        size: Number.MAX_SAFE_INTEGER,
        capacity: Number.MAX_SAFE_INTEGER,
        hits: Number.MAX_SAFE_INTEGER,
        misses: 1,
        evictions: Number.MAX_SAFE_INTEGER,
      });

      const result = await client.getCacheStats();

      expect(result.size).toBe(Number.MAX_SAFE_INTEGER);
      expect(result.capacity).toBe(Number.MAX_SAFE_INTEGER);
    });

    it('should handle zero hit rate', async () => {
      mockTransport.get.mockResolvedValueOnce({
        hitRate: 0,
        size: 0,
        capacity: 1000,
        hits: 0,
        misses: 100,
        evictions: 0,
      });

      const result = await client.getCacheStats();

      expect(result.hitRate).toBe(0);
    });

    it('should handle special characters in entityType', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getCacheStats({ entityType: 'user-session:v2' });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cache/stats', {
        query: { entityType: 'user-session:v2' },
      });
    });

    it('should handle very large config values', async () => {
      mockTransport.put.mockResolvedValueOnce({ updated: true });

      await client.updateCacheConfig({
        l1Size: 1000000,
        l1TTL: 86400000,
        cleanupInterval: 3600000,
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/cache/config', {
        l1Size: 1000000,
        l1TTL: 86400000,
        cleanupInterval: 3600000,
      });
    });

    it('should handle clearing empty cache', async () => {
      mockTransport.post.mockResolvedValueOnce({
        entriesCleared: 0,
      });

      const result = await client.clearCache({ all: true });

      expect(result.entriesCleared).toBe(0);
    });
  });

  // ============================================================
  // Response Normalization Tests
  // ============================================================

  describe('response normalization', () => {
    it('should normalize cache stats with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        hitRate: 0.5,
      });

      const result = await client.getCacheStats();

      expect(result).toEqual({
        hitRate: 0.5,
        size: 0,
        capacity: 0,
        hits: 0,
        misses: 0,
        evictions: 0,
      });
    });

    it('should normalize clear cache response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({
        someOtherField: 'value',
      });

      const result = await client.clearCache({ all: true });

      expect(result).toEqual({
        entriesCleared: 0,
      });
    });

    it('should normalize cache config response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        l1Size: 5000,
      });

      const result = await client.getCacheConfig();

      expect(result).toEqual({
        l1Size: 5000,
        l1TTL: 0,
        cleanupInterval: 0,
        enabled: false,
      });
    });

    it('should normalize update cache config response with newConfig', async () => {
      mockTransport.put.mockResolvedValueOnce({
        updated: true,
        newConfig: {
          l1Size: 10000,
        },
      });

      const result = await client.updateCacheConfig({ l1Size: 10000 });

      expect(result.updated).toBe(true);
      expect(result.newConfig).toEqual({
        l1Size: 10000,
        l1TTL: 0,
        cleanupInterval: 0,
        enabled: false,
      });
    });
  });
});
