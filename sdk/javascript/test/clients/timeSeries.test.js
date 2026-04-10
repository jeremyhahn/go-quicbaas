/**
 * TimeSeriesClient Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import TimeSeriesClient, { Aggregation, Interval } from '../../src/clients/timeSeries.js';
import { ValidationError } from '../../src/errors/index.js';

describe('TimeSeriesClient', () => {
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
    client = new TimeSeriesClient(mockTransport);
  });

  // ==========================================================================
  // Constructor Tests
  // ==========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      expect(client).toBeInstanceOf(TimeSeriesClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new TimeSeriesClient()).toThrow(ValidationError);
      expect(() => new TimeSeriesClient(null)).toThrow(ValidationError);
      expect(() => new TimeSeriesClient(undefined)).toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // writePoint Tests
  // ==========================================================================

  describe('writePoint', () => {
    const now = new Date('2025-01-17T12:00:00Z');

    it('should write a single data point with Date timestamp', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      const result = await client.writePoint('cpu.usage', 75.5, now);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/write', {
        metric: 'cpu.usage',
        value: 75.5,
        timestamp: '2025-01-17T12:00:00.000Z',
      });
      expect(result.success).toBe(true);
    });

    it('should write a data point with ISO string timestamp', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      const result = await client.writePoint('cpu.usage', 75.5, '2025-01-17T12:00:00Z');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/write', {
        metric: 'cpu.usage',
        value: 75.5,
        timestamp: '2025-01-17T12:00:00.000Z',
      });
      expect(result.success).toBe(true);
    });

    it('should write a data point with tags', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      const result = await client.writePoint('cpu.usage', 75.5, now, {
        host: 'server1',
        region: 'us-east',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/write', {
        metric: 'cpu.usage',
        value: 75.5,
        timestamp: '2025-01-17T12:00:00.000Z',
        tags: { host: 'server1', region: 'us-east' },
      });
      expect(result.success).toBe(true);
    });

    it('should not include tags when empty', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      await client.writePoint('cpu.usage', 75.5, now, {});

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/write', {
        metric: 'cpu.usage',
        value: 75.5,
        timestamp: '2025-01-17T12:00:00.000Z',
      });
    });

    it('should default success to true when response is empty', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.writePoint('cpu.usage', 75.5, now);

      expect(result.success).toBe(true);
    });

    it('should throw ValidationError when metric is empty', async () => {
      await expect(client.writePoint('', 75.5, now)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metric is not a string', async () => {
      await expect(client.writePoint(123, 75.5, now)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is missing', async () => {
      await expect(client.writePoint('cpu.usage', null, now)).rejects.toThrow(ValidationError);
      await expect(client.writePoint('cpu.usage', undefined, now)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is not a finite number', async () => {
      await expect(client.writePoint('cpu.usage', NaN, now)).rejects.toThrow(ValidationError);
      await expect(client.writePoint('cpu.usage', Infinity, now)).rejects.toThrow(ValidationError);
      await expect(client.writePoint('cpu.usage', 'not-a-number', now)).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when timestamp is missing', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when timestamp is invalid Date', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, new Date('invalid'))).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when timestamp is invalid string', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, 'not-a-date')).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when timestamp is neither Date nor string', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, 12345)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when tags is not an object', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, now, 'invalid')).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when tags is an array', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, now, ['tag1'])).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when tag key is empty', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, now, { '': 'value' })).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when tag value is not a string', async () => {
      await expect(client.writePoint('cpu.usage', 75.5, now, { host: 123 })).rejects.toThrow(
        ValidationError
      );
    });
  });

  // ==========================================================================
  // writeBatch Tests
  // ==========================================================================

  describe('writeBatch', () => {
    const now = new Date('2025-01-17T12:00:00Z');

    it('should write multiple data points', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true, written: 2 });

      const points = [
        { metric: 'cpu.usage', value: 75.5, timestamp: now },
        { metric: 'memory.usage', value: 60.2, timestamp: now },
      ];

      const result = await client.writeBatch(points);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/write/batch', {
        points: [
          { metric: 'cpu.usage', value: 75.5, timestamp: '2025-01-17T12:00:00.000Z' },
          { metric: 'memory.usage', value: 60.2, timestamp: '2025-01-17T12:00:00.000Z' },
        ],
      });
      expect(result.success).toBe(true);
      expect(result.written).toBe(2);
    });

    it('should write data points with tags', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true, written: 1 });

      const points = [
        { metric: 'cpu.usage', value: 75.5, timestamp: now, tags: { host: 'server1' } },
      ];

      const result = await client.writeBatch(points);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/write/batch', {
        points: [
          {
            metric: 'cpu.usage',
            value: 75.5,
            timestamp: '2025-01-17T12:00:00.000Z',
            tags: { host: 'server1' },
          },
        ],
      });
      expect(result.written).toBe(1);
    });

    it('should default written count to points length', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      const points = [
        { metric: 'cpu.usage', value: 75.5, timestamp: now },
        { metric: 'memory.usage', value: 60.2, timestamp: now },
      ];

      const result = await client.writeBatch(points);

      expect(result.written).toBe(2);
    });

    it('should throw ValidationError when points is empty array', async () => {
      await expect(client.writeBatch([])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when points is not an array', async () => {
      await expect(client.writeBatch('not-array')).rejects.toThrow(ValidationError);
      await expect(client.writeBatch(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when point is not an object', async () => {
      await expect(client.writeBatch(['not-object'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when point.metric is missing', async () => {
      await expect(client.writeBatch([{ value: 75.5, timestamp: now }])).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when point.value is not a number', async () => {
      await expect(
        client.writeBatch([{ metric: 'cpu.usage', value: 'invalid', timestamp: now }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when point.timestamp is missing', async () => {
      await expect(client.writeBatch([{ metric: 'cpu.usage', value: 75.5 }])).rejects.toThrow(
        ValidationError
      );
    });
  });

  // ==========================================================================
  // query Tests
  // ==========================================================================

  describe('query', () => {
    const start = new Date('2025-01-17T00:00:00Z');
    const end = new Date('2025-01-17T12:00:00Z');

    it('should query time-series data', async () => {
      const mockResponse = {
        series: [
          {
            tags: { host: 'server1' },
            points: [
              { timestamp: '2025-01-17T01:00:00Z', value: 75.5 },
              { timestamp: '2025-01-17T02:00:00Z', value: 80.2 },
            ],
          },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.query({
        metric: 'cpu.usage',
        start,
        end,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/query', {
        metric: 'cpu.usage',
        start: '2025-01-17T00:00:00.000Z',
        end: '2025-01-17T12:00:00.000Z',
      });
      expect(result.series).toHaveLength(1);
      expect(result.series[0].points).toHaveLength(2);
    });

    it('should query with aggregation and interval', async () => {
      mockTransport.post.mockResolvedValueOnce({ series: [] });

      await client.query({
        metric: 'cpu.usage',
        start,
        end,
        aggregation: Aggregation.AVG,
        interval: Interval.HOUR,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/query', {
        metric: 'cpu.usage',
        start: '2025-01-17T00:00:00.000Z',
        end: '2025-01-17T12:00:00.000Z',
        aggregation: 'AVG',
        interval: 'HOUR',
      });
    });

    it('should query with tags filter', async () => {
      mockTransport.post.mockResolvedValueOnce({ series: [] });

      await client.query({
        metric: 'cpu.usage',
        start,
        end,
        tags: { host: 'server1' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/query', {
        metric: 'cpu.usage',
        start: '2025-01-17T00:00:00.000Z',
        end: '2025-01-17T12:00:00.000Z',
        tags: { host: 'server1' },
      });
    });

    it('should query with groupBy', async () => {
      mockTransport.post.mockResolvedValueOnce({ series: [] });

      await client.query({
        metric: 'cpu.usage',
        start,
        end,
        groupBy: ['host', 'region'],
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/query', {
        metric: 'cpu.usage',
        start: '2025-01-17T00:00:00.000Z',
        end: '2025-01-17T12:00:00.000Z',
        groupBy: ['host', 'region'],
      });
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.query({ metric: 'cpu.usage', start, end });

      expect(result.series).toEqual([]);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.query(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metric is missing', async () => {
      await expect(client.query({ start, end })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when start is missing', async () => {
      await expect(client.query({ metric: 'cpu.usage', end })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when end is missing', async () => {
      await expect(client.query({ metric: 'cpu.usage', start })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when start is invalid', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start: 'invalid', end })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when end is invalid', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start, end: 'invalid' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when end is before start', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start: end, end: start })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when end equals start', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start, end: start })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid aggregation', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start, end, aggregation: 'INVALID' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when aggregation set but interval missing', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start, end, aggregation: Aggregation.AVG })
      ).rejects.toThrow(ValidationError);
    });

    it('should not require interval when aggregation is NONE', async () => {
      mockTransport.post.mockResolvedValueOnce({ series: [] });

      await client.query({
        metric: 'cpu.usage',
        start,
        end,
        aggregation: Aggregation.NONE,
      });

      expect(mockTransport.post).toHaveBeenCalled();
    });

    it('should throw ValidationError for invalid interval', async () => {
      await expect(
        client.query({
          metric: 'cpu.usage',
          start,
          end,
          aggregation: Aggregation.AVG,
          interval: 'INVALID',
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when groupBy is not an array', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start, end, groupBy: 'host' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when groupBy contains non-strings', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start, end, groupBy: [123] })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when groupBy contains empty strings', async () => {
      await expect(
        client.query({ metric: 'cpu.usage', start, end, groupBy: ['host', ''] })
      ).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // aggregate Tests
  // ==========================================================================

  describe('aggregate', () => {
    const start = new Date('2025-01-17T00:00:00Z');
    const end = new Date('2025-01-17T12:00:00Z');

    it('should compute aggregation', async () => {
      mockTransport.post.mockResolvedValueOnce({ value: 75.5, count: 100 });

      const result = await client.aggregate({
        metric: 'cpu.usage',
        start,
        end,
        aggregation: Aggregation.AVG,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/aggregate', {
        metric: 'cpu.usage',
        start: '2025-01-17T00:00:00.000Z',
        end: '2025-01-17T12:00:00.000Z',
        aggregation: 'AVG',
      });
      expect(result.value).toBe(75.5);
      expect(result.count).toBe(100);
    });

    it('should compute aggregation with tags filter', async () => {
      mockTransport.post.mockResolvedValueOnce({ value: 80.0, count: 50 });

      await client.aggregate({
        metric: 'cpu.usage',
        start,
        end,
        aggregation: Aggregation.MAX,
        tags: { host: 'server1' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/timeseries/aggregate', {
        metric: 'cpu.usage',
        start: '2025-01-17T00:00:00.000Z',
        end: '2025-01-17T12:00:00.000Z',
        aggregation: 'MAX',
        tags: { host: 'server1' },
      });
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.aggregate({
        metric: 'cpu.usage',
        start,
        end,
        aggregation: Aggregation.SUM,
      });

      expect(result.value).toBe(0);
      expect(result.count).toBe(0);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.aggregate(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metric is missing', async () => {
      await expect(
        client.aggregate({ start, end, aggregation: Aggregation.AVG })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when aggregation is missing', async () => {
      await expect(
        client.aggregate({ metric: 'cpu.usage', start, end })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when aggregation is NONE', async () => {
      await expect(
        client.aggregate({
          metric: 'cpu.usage',
          start,
          end,
          aggregation: Aggregation.NONE,
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when start is after end', async () => {
      await expect(
        client.aggregate({
          metric: 'cpu.usage',
          start: end,
          end: start,
          aggregation: Aggregation.AVG,
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when start is an invalid ISO string', async () => {
      await expect(
        client.aggregate({
          metric: 'cpu.usage',
          start: 'not-a-valid-date',
          end,
          aggregation: Aggregation.AVG,
        })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.aggregate({
          metric: 'cpu.usage',
          start: 'not-a-valid-date',
          end,
          aggregation: Aggregation.AVG,
        })
      ).rejects.toThrow('start must be a valid Date or ISO 8601 string');
    });

    it('should throw ValidationError when end is an invalid ISO string', async () => {
      await expect(
        client.aggregate({
          metric: 'cpu.usage',
          start,
          end: 'not-a-valid-date',
          aggregation: Aggregation.AVG,
        })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.aggregate({
          metric: 'cpu.usage',
          start,
          end: 'not-a-valid-date',
          aggregation: Aggregation.AVG,
        })
      ).rejects.toThrow('end must be a valid Date or ISO 8601 string');
    });
  });

  // ==========================================================================
  // deleteSeries Tests
  // ==========================================================================

  describe('deleteSeries', () => {
    it('should delete series by metric', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: true, seriesCount: 5 });

      const result = await client.deleteSeries('old.metric');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/timeseries/series', {
        body: { metric: 'old.metric' },
      });
      expect(result.deleted).toBe(true);
      expect(result.seriesCount).toBe(5);
    });

    it('should delete series by metric and tags', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: true, seriesCount: 1 });

      const result = await client.deleteSeries('cpu.usage', { host: 'decommissioned' });

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/timeseries/series', {
        body: { metric: 'cpu.usage', tags: { host: 'decommissioned' } },
      });
      expect(result.seriesCount).toBe(1);
    });

    it('should handle series_count response format', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: true, series_count: 3 });

      const result = await client.deleteSeries('old.metric');

      expect(result.seriesCount).toBe(3);
    });

    it('should default deleted to true', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.deleteSeries('old.metric');

      expect(result.deleted).toBe(true);
    });

    it('should throw ValidationError when metric is empty', async () => {
      await expect(client.deleteSeries('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metric is not a string', async () => {
      await expect(client.deleteSeries(123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when tags is invalid', async () => {
      await expect(client.deleteSeries('cpu.usage', 'invalid')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // listMetrics Tests
  // ==========================================================================

  describe('listMetrics', () => {
    it('should list all metrics', async () => {
      const mockResponse = {
        metrics: ['cpu.usage', 'memory.usage', 'disk.io'],
        hasMore: false,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listMetrics();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/timeseries/metrics', { query: {} });
      expect(result.metrics).toHaveLength(3);
      expect(result.hasMore).toBe(false);
    });

    it('should list metrics with prefix filter', async () => {
      mockTransport.get.mockResolvedValueOnce({ metrics: ['cpu.usage', 'cpu.idle'], hasMore: false });

      await client.listMetrics({ prefix: 'cpu.' });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/timeseries/metrics', {
        query: { prefix: 'cpu.' },
      });
    });

    it('should list metrics with limit', async () => {
      mockTransport.get.mockResolvedValueOnce({
        metrics: ['cpu.usage'],
        hasMore: true,
        nextCursor: 'abc123',
      });

      const result = await client.listMetrics({ limit: 1 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/timeseries/metrics', {
        query: { limit: 1 },
      });
      expect(result.hasMore).toBe(true);
      expect(result.nextCursor).toBe('abc123');
    });

    it('should list metrics with cursor', async () => {
      mockTransport.get.mockResolvedValueOnce({ metrics: ['memory.usage'], hasMore: false });

      await client.listMetrics({ cursor: 'abc123' });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/timeseries/metrics', {
        query: { cursor: 'abc123' },
      });
    });

    it('should handle next_cursor response format', async () => {
      mockTransport.get.mockResolvedValueOnce({ metrics: [], next_cursor: 'xyz', has_more: true });

      const result = await client.listMetrics();

      expect(result.nextCursor).toBe('xyz');
      expect(result.hasMore).toBe(true);
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.listMetrics();

      expect(result.metrics).toEqual([]);
      expect(result.hasMore).toBe(false);
    });

    it('should throw ValidationError when prefix is not a string', async () => {
      await expect(client.listMetrics({ prefix: 123 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(client.listMetrics({ limit: 0 })).rejects.toThrow(ValidationError);
      await expect(client.listMetrics({ limit: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when cursor is not a string', async () => {
      await expect(client.listMetrics({ cursor: 123 })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // getRetentionPolicy Tests
  // ==========================================================================

  describe('getRetentionPolicy', () => {
    it('should get retention policy for a metric', async () => {
      const mockResponse = {
        metric: 'cpu.usage',
        policy: {
          retentionDays: 90,
          aggregation: 'AVG',
          interval: 'HOUR',
        },
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRetentionPolicy('cpu.usage');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/timeseries/retention/cpu.usage');
      expect(result.metric).toBe('cpu.usage');
      expect(result.policy.retentionDays).toBe(90);
      expect(result.policy.aggregation).toBe('AVG');
      expect(result.policy.interval).toBe('HOUR');
    });

    it('should handle snake_case response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        metric: 'cpu.usage',
        policy: { retention_days: 30 },
      });

      const result = await client.getRetentionPolicy('cpu.usage');

      expect(result.policy.retentionDays).toBe(30);
    });

    it('should handle missing policy', async () => {
      mockTransport.get.mockResolvedValueOnce({ metric: 'cpu.usage' });

      const result = await client.getRetentionPolicy('cpu.usage');

      expect(result.policy.retentionDays).toBe(0);
      expect(result.policy.aggregation).toBeNull();
      expect(result.policy.interval).toBeNull();
    });

    it('should URL-encode metric name', async () => {
      mockTransport.get.mockResolvedValueOnce({ metric: 'cpu/usage', policy: {} });

      await client.getRetentionPolicy('cpu/usage');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/timeseries/retention/cpu%2Fusage');
    });

    it('should throw ValidationError when metric is empty', async () => {
      await expect(client.getRetentionPolicy('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metric is not a string', async () => {
      await expect(client.getRetentionPolicy(123)).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // setRetentionPolicy Tests
  // ==========================================================================

  describe('setRetentionPolicy', () => {
    it('should set retention policy', async () => {
      mockTransport.put.mockResolvedValueOnce({ success: true });

      const result = await client.setRetentionPolicy('cpu.usage', {
        retentionDays: 90,
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/timeseries/retention/cpu.usage', {
        retentionDays: 90,
      });
      expect(result.success).toBe(true);
    });

    it('should set retention policy with downsampling', async () => {
      mockTransport.put.mockResolvedValueOnce({ success: true });

      await client.setRetentionPolicy('cpu.usage', {
        retentionDays: 365,
        aggregation: Aggregation.AVG,
        interval: Interval.HOUR,
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/timeseries/retention/cpu.usage', {
        retentionDays: 365,
        aggregation: 'AVG',
        interval: 'HOUR',
      });
    });

    it('should default success to true', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.setRetentionPolicy('cpu.usage', { retentionDays: 30 });

      expect(result.success).toBe(true);
    });

    it('should throw ValidationError when metric is empty', async () => {
      await expect(
        client.setRetentionPolicy('', { retentionDays: 30 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy is null', async () => {
      await expect(client.setRetentionPolicy('cpu.usage', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy is not an object', async () => {
      await expect(client.setRetentionPolicy('cpu.usage', 'invalid')).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when retentionDays is missing', async () => {
      await expect(client.setRetentionPolicy('cpu.usage', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when retentionDays is not positive', async () => {
      await expect(
        client.setRetentionPolicy('cpu.usage', { retentionDays: 0 })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.setRetentionPolicy('cpu.usage', { retentionDays: -1 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid aggregation', async () => {
      await expect(
        client.setRetentionPolicy('cpu.usage', {
          retentionDays: 30,
          aggregation: 'INVALID',
          interval: Interval.HOUR,
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid interval', async () => {
      await expect(
        client.setRetentionPolicy('cpu.usage', {
          retentionDays: 30,
          aggregation: Aggregation.AVG,
          interval: 'INVALID',
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when only aggregation is set without interval', async () => {
      await expect(
        client.setRetentionPolicy('cpu.usage', {
          retentionDays: 30,
          aggregation: Aggregation.AVG,
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when only interval is set without aggregation', async () => {
      await expect(
        client.setRetentionPolicy('cpu.usage', {
          retentionDays: 30,
          interval: Interval.HOUR,
        })
      ).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Enum Exports Tests
  // ==========================================================================

  describe('Aggregation enum', () => {
    it('should export all aggregation values', () => {
      expect(Aggregation.NONE).toBe('NONE');
      expect(Aggregation.SUM).toBe('SUM');
      expect(Aggregation.AVG).toBe('AVG');
      expect(Aggregation.MIN).toBe('MIN');
      expect(Aggregation.MAX).toBe('MAX');
      expect(Aggregation.COUNT).toBe('COUNT');
      expect(Aggregation.FIRST).toBe('FIRST');
      expect(Aggregation.LAST).toBe('LAST');
      expect(Aggregation.STDDEV).toBe('STDDEV');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(Aggregation)).toBe(true);
    });
  });

  describe('Interval enum', () => {
    it('should export all interval values', () => {
      expect(Interval.SECOND).toBe('SECOND');
      expect(Interval.MINUTE).toBe('MINUTE');
      expect(Interval.HOUR).toBe('HOUR');
      expect(Interval.DAY).toBe('DAY');
      expect(Interval.WEEK).toBe('WEEK');
      expect(Interval.MONTH).toBe('MONTH');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(Interval)).toBe(true);
    });
  });
});
