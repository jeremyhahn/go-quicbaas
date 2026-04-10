/**
 * QuicBaaS JavaScript SDK - TimeSeries Client
 *
 * Provides time-series data operations for QuicBaaS including
 * data ingestion, querying, aggregation, and retention policy management.
 *
 * @module @quicbaas/client/clients/timeSeries
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  requiredObject,
  requiredArray,
  nonNegativeInteger,
  positiveInteger,
  oneOf,
  validateRequest,
} from '../utils/validation.js';

/**
 * Aggregation functions for time-series queries.
 * @readonly
 * @enum {string}
 */
export const Aggregation = Object.freeze({
  /** No aggregation, return raw data points */
  NONE: 'NONE',
  /** Sum of values in each interval */
  SUM: 'SUM',
  /** Average of values in each interval */
  AVG: 'AVG',
  /** Minimum value in each interval */
  MIN: 'MIN',
  /** Maximum value in each interval */
  MAX: 'MAX',
  /** Count of values in each interval */
  COUNT: 'COUNT',
  /** First value in each interval */
  FIRST: 'FIRST',
  /** Last value in each interval */
  LAST: 'LAST',
  /** Standard deviation of values in each interval */
  STDDEV: 'STDDEV',
});

/**
 * Valid aggregation values for validation.
 * @type {string[]}
 */
const VALID_AGGREGATIONS = Object.values(Aggregation);

/**
 * Time intervals for aggregation bucketing.
 * @readonly
 * @enum {string}
 */
export const Interval = Object.freeze({
  /** One second intervals */
  SECOND: 'SECOND',
  /** One minute intervals */
  MINUTE: 'MINUTE',
  /** One hour intervals */
  HOUR: 'HOUR',
  /** One day intervals */
  DAY: 'DAY',
  /** One week intervals */
  WEEK: 'WEEK',
  /** One month intervals */
  MONTH: 'MONTH',
});

/**
 * Valid interval values for validation.
 * @type {string[]}
 */
const VALID_INTERVALS = Object.values(Interval);

/**
 * @typedef {Object} DataPoint
 * @property {string} metric - The metric name
 * @property {number} value - The numeric value
 * @property {Date|string} timestamp - The data point timestamp (Date or ISO 8601 string)
 * @property {Object<string, string>} [tags] - Optional key-value tags for filtering and grouping
 */

/**
 * @typedef {Object} WritePointOptions
 * @property {Object<string, string>} [tags] - Optional key-value tags
 */

/**
 * @typedef {Object} WriteResponse
 * @property {boolean} success - Whether the write was successful
 */

/**
 * @typedef {Object} BatchWriteResponse
 * @property {boolean} success - Whether the batch write was successful
 * @property {number} written - Number of points successfully written
 */

/**
 * @typedef {Object} QueryRequest
 * @property {string} metric - The metric name to query
 * @property {Date|string} start - Start of the time range (Date or ISO 8601 string)
 * @property {Date|string} end - End of the time range (Date or ISO 8601 string)
 * @property {Object<string, string>} [tags] - Optional tag filters
 * @property {string} [aggregation] - Aggregation function to apply
 * @property {string[]} [groupBy] - Tag keys to group results by
 * @property {string} [interval] - Aggregation interval (required when aggregation is set)
 */

/**
 * @typedef {Object} Point
 * @property {string} timestamp - ISO 8601 timestamp
 * @property {number} value - The numeric value
 */

/**
 * @typedef {Object} Series
 * @property {Object<string, string>} [tags] - Tags identifying the series
 * @property {Point[]} points - Data points in the series
 */

/**
 * @typedef {Object} QueryResponse
 * @property {Series[]} series - Query result series
 */

/**
 * @typedef {Object} AggregateRequest
 * @property {string} metric - The metric name to aggregate
 * @property {Date|string} start - Start of the time range
 * @property {Date|string} end - End of the time range
 * @property {string} aggregation - Aggregation function (SUM, AVG, MIN, MAX, COUNT)
 * @property {Object<string, string>} [tags] - Optional tag filters
 */

/**
 * @typedef {Object} AggregateResponse
 * @property {number} value - The aggregated value
 * @property {number} count - Number of points included in aggregation
 */

/**
 * @typedef {Object} DeleteSeriesRequest
 * @property {string} metric - The metric name
 * @property {Object<string, string>} [tags] - Optional tags to identify specific series
 */

/**
 * @typedef {Object} DeleteSeriesResponse
 * @property {boolean} deleted - Whether the series was deleted
 * @property {number} seriesCount - Number of series deleted
 */

/**
 * @typedef {Object} ListMetricsOptions
 * @property {string} [prefix] - Filter metrics by prefix
 * @property {number} [limit] - Maximum number of metrics to return
 * @property {string} [cursor] - Pagination cursor
 */

/**
 * @typedef {Object} ListMetricsResponse
 * @property {string[]} metrics - List of metric names
 * @property {string} [nextCursor] - Cursor for next page
 * @property {boolean} hasMore - Whether more results are available
 */

/**
 * @typedef {Object} RetentionPolicy
 * @property {number} retentionDays - Number of days to retain data
 * @property {string} [aggregation] - Aggregation to apply for downsampling
 * @property {string} [interval] - Downsampling interval
 */

/**
 * @typedef {Object} GetRetentionPolicyResponse
 * @property {string} metric - The metric name
 * @property {RetentionPolicy} policy - The retention policy
 */

/**
 * @typedef {Object} SetRetentionPolicyResponse
 * @property {boolean} success - Whether the policy was set successfully
 */

/**
 * TimeSeries Client for QuicBaaS time-series data operations.
 *
 * Provides methods for writing, querying, and managing time-series data
 * with support for aggregation, downsampling, and retention policies.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import TimeSeriesClient, { Aggregation, Interval } from '@quicbaas/client/clients/timeSeries';
 *
 * const transport = new HttpTransport('http://localhost:8080');
 * const tsClient = new TimeSeriesClient(transport);
 *
 * // Write a data point
 * await tsClient.writePoint('cpu.usage', 75.5, new Date(), { host: 'server1' });
 *
 * // Query with aggregation
 * const results = await tsClient.query({
 *   metric: 'cpu.usage',
 *   start: new Date(Date.now() - 3600000),
 *   end: new Date(),
 *   aggregation: Aggregation.AVG,
 *   interval: Interval.MINUTE,
 *   groupBy: ['host'],
 * });
 */
class TimeSeriesClient {
  /**
   * API endpoint base path for time-series operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/timeseries';

  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   */
  #transport;

  /**
   * Create a new TimeSeriesClient instance.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /**
   * Write a single time-series data point.
   *
   * @param {string} metric - The metric name
   * @param {number} value - The numeric value
   * @param {Date|string} timestamp - The data point timestamp (Date or ISO 8601 string)
   * @param {Object<string, string>} [tags={}] - Optional key-value tags
   * @returns {Promise<WriteResponse>} Write response
   * @throws {ValidationError} If required parameters are missing or invalid
   *
   * @example
   * await tsClient.writePoint('temperature', 23.5, new Date(), { location: 'room1' });
   */
  async writePoint(metric, value, timestamp, tags = {}) {
    requiredString(metric, 'metric');
    required(value, 'value');
    required(timestamp, 'timestamp');

    this.#validateValue(value);
    const isoTimestamp = this.#formatTimestamp(timestamp);
    this.#validateTags(tags);

    const body = {
      metric,
      value,
      timestamp: isoTimestamp,
    };

    if (tags && Object.keys(tags).length > 0) {
      body.tags = tags;
    }

    const response = await this.#transport.post(
      `${TimeSeriesClient.API_PREFIX}/write`,
      body
    );

    return {
      success: response?.success ?? true,
    };
  }

  /**
   * Write multiple time-series data points in a single batch.
   *
   * @param {DataPoint[]} points - Array of data points to write
   * @returns {Promise<BatchWriteResponse>} Batch write response
   * @throws {ValidationError} If points array is empty or invalid
   *
   * @example
   * await tsClient.writeBatch([
   *   { metric: 'cpu.usage', value: 75.5, timestamp: new Date(), tags: { host: 'server1' } },
   *   { metric: 'memory.usage', value: 60.2, timestamp: new Date(), tags: { host: 'server1' } },
   * ]);
   */
  async writeBatch(points) {
    requiredArray(points, 'points');
    if (points.length === 0) {
      throw new ValidationError('points cannot be empty', 'points');
    }

    const formattedPoints = points.map((point, index) => {
      this.#validatePoint(point, `points[${index}]`);
      return {
        metric: point.metric,
        value: point.value,
        timestamp: this.#formatTimestamp(point.timestamp),
        ...(point.tags && Object.keys(point.tags).length > 0 && { tags: point.tags }),
      };
    });

    const response = await this.#transport.post(
      `${TimeSeriesClient.API_PREFIX}/write/batch`,
      { points: formattedPoints }
    );

    return {
      success: response?.success ?? true,
      written: response?.written ?? formattedPoints.length,
    };
  }

  /**
   * Query time-series data with optional aggregation.
   *
   * @param {QueryRequest} request - Query request parameters
   * @returns {Promise<QueryResponse>} Query results
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const response = await tsClient.query({
   *   metric: 'cpu.usage',
   *   start: new Date(Date.now() - 3600000),
   *   end: new Date(),
   *   aggregation: Aggregation.AVG,
   *   interval: Interval.MINUTE,
   *   tags: { host: 'server1' },
   *   groupBy: ['host'],
   * });
   *
   * response.series.forEach(series => {
   *   console.log(series.tags, series.points);
   * });
   */
  async query(request) {
    this.#validateQueryRequest(request);

    const body = this.#buildQueryRequestBody(request);
    const response = await this.#transport.post(
      `${TimeSeriesClient.API_PREFIX}/query`,
      body
    );

    return this.#normalizeQueryResponse(response);
  }

  /**
   * Compute an aggregation over a time range.
   *
   * @param {AggregateRequest} request - Aggregation request parameters
   * @returns {Promise<AggregateResponse>} Aggregation result
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const result = await tsClient.aggregate({
   *   metric: 'cpu.usage',
   *   start: new Date(Date.now() - 86400000),
   *   end: new Date(),
   *   aggregation: Aggregation.AVG,
   *   tags: { host: 'server1' },
   * });
   *
   * console.log(`Average: ${result.value}, Count: ${result.count}`);
   */
  async aggregate(request) {
    this.#validateAggregateRequest(request);

    const body = {
      metric: request.metric,
      start: this.#formatTimestamp(request.start),
      end: this.#formatTimestamp(request.end),
      aggregation: request.aggregation,
    };

    if (request.tags && Object.keys(request.tags).length > 0) {
      body.tags = request.tags;
    }

    const response = await this.#transport.post(
      `${TimeSeriesClient.API_PREFIX}/aggregate`,
      body
    );

    return {
      value: response?.value ?? 0,
      count: response?.count ?? 0,
    };
  }

  /**
   * Delete a time-series by metric name and optional tags.
   *
   * @param {string} metric - The metric name
   * @param {Object<string, string>} [tags] - Optional tags to identify specific series
   * @returns {Promise<DeleteSeriesResponse>} Delete response
   * @throws {ValidationError} If metric is missing
   *
   * @example
   * // Delete all series for a metric
   * await tsClient.deleteSeries('old.metric');
   *
   * // Delete specific series with tags
   * await tsClient.deleteSeries('cpu.usage', { host: 'decommissioned-server' });
   */
  async deleteSeries(metric, tags) {
    requiredString(metric, 'metric');

    if (tags !== undefined && tags !== null) {
      this.#validateTags(tags);
    }

    const body = { metric };

    if (tags && Object.keys(tags).length > 0) {
      body.tags = tags;
    }

    const response = await this.#transport.delete(
      `${TimeSeriesClient.API_PREFIX}/series`,
      { body }
    );

    return {
      deleted: response?.deleted ?? true,
      seriesCount: response?.seriesCount ?? response?.series_count ?? 0,
    };
  }

  /**
   * List all available metrics.
   *
   * @param {ListMetricsOptions} [options={}] - List options
   * @returns {Promise<ListMetricsResponse>} List of metrics with pagination
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * const response = await tsClient.listMetrics({ prefix: 'cpu.', limit: 100 });
   * console.log(response.metrics);
   *
   * if (response.hasMore) {
   *   const nextPage = await tsClient.listMetrics({ cursor: response.nextCursor });
   * }
   */
  async listMetrics(options = {}) {
    this.#validateListMetricsOptions(options);

    const query = {};

    if (options.prefix) {
      query.prefix = options.prefix;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor) {
      query.cursor = options.cursor;
    }

    const response = await this.#transport.get(
      `${TimeSeriesClient.API_PREFIX}/metrics`,
      { query }
    );

    return {
      metrics: response?.metrics ?? [],
      nextCursor: response?.nextCursor ?? response?.next_cursor ?? null,
      hasMore: Boolean(response?.hasMore ?? response?.has_more),
    };
  }

  /**
   * Get the retention policy for a metric.
   *
   * @param {string} metric - The metric name
   * @returns {Promise<GetRetentionPolicyResponse>} Retention policy
   * @throws {ValidationError} If metric is missing
   *
   * @example
   * const policy = await tsClient.getRetentionPolicy('cpu.usage');
   * console.log(`Retention: ${policy.policy.retentionDays} days`);
   */
  async getRetentionPolicy(metric) {
    requiredString(metric, 'metric');

    const response = await this.#transport.get(
      `${TimeSeriesClient.API_PREFIX}/retention/${encodeURIComponent(metric)}`
    );

    return {
      metric: response?.metric ?? metric,
      policy: this.#normalizeRetentionPolicy(response?.policy),
    };
  }

  /**
   * Set the retention policy for a metric.
   *
   * @param {string} metric - The metric name
   * @param {RetentionPolicy} policy - The retention policy to set
   * @returns {Promise<SetRetentionPolicyResponse>} Set response
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * await tsClient.setRetentionPolicy('cpu.usage', {
   *   retentionDays: 90,
   *   aggregation: Aggregation.AVG,
   *   interval: Interval.HOUR,
   * });
   */
  async setRetentionPolicy(metric, policy) {
    requiredString(metric, 'metric');
    requiredObject(policy, 'policy');
    this.#validateRetentionPolicy(policy);

    const body = {
      retentionDays: policy.retentionDays,
    };

    if (policy.aggregation) {
      body.aggregation = policy.aggregation;
    }
    if (policy.interval) {
      body.interval = policy.interval;
    }

    const response = await this.#transport.put(
      `${TimeSeriesClient.API_PREFIX}/retention/${encodeURIComponent(metric)}`,
      body
    );

    return {
      success: response?.success ?? true,
    };
  }

  /**
   * Format a timestamp to ISO 8601 string.
   * @param {Date|string} timestamp - The timestamp to format
   * @returns {string} ISO 8601 formatted timestamp
   * @throws {ValidationError} If timestamp is invalid
   * @private
   */
  #formatTimestamp(timestamp) {
    if (timestamp instanceof Date) {
      if (isNaN(timestamp.getTime())) {
        throw new ValidationError('timestamp is an invalid Date', 'timestamp');
      }
      return timestamp.toISOString();
    }

    if (typeof timestamp === 'string') {
      const parsed = new Date(timestamp);
      if (isNaN(parsed.getTime())) {
        throw new ValidationError('timestamp is not a valid ISO 8601 string', 'timestamp');
      }
      return parsed.toISOString();
    }

    throw new ValidationError('timestamp must be a Date or ISO 8601 string', 'timestamp');
  }

  /**
   * Validate a numeric value.
   * @param {number} value - Value to validate
   * @throws {ValidationError} If value is not a finite number
   * @private
   */
  #validateValue(value) {
    if (typeof value !== 'number' || !isFinite(value)) {
      throw new ValidationError('value must be a finite number', 'value');
    }
  }

  /**
   * Validate tags object.
   * @param {Object<string, string>} tags - Tags to validate
   * @throws {ValidationError} If tags are invalid
   * @private
   */
  #validateTags(tags) {
    if (tags !== undefined && tags !== null) {
      if (typeof tags !== 'object' || Array.isArray(tags)) {
        throw new ValidationError('tags must be an object', 'tags');
      }

      for (const [key, value] of Object.entries(tags)) {
        if (typeof key !== 'string' || key.trim() === '') {
          throw new ValidationError('tag keys must be non-empty strings', 'tags');
        }
        if (typeof value !== 'string') {
          throw new ValidationError('tag values must be strings', 'tags');
        }
      }
    }
  }

  /**
   * Validate a single data point.
   * @param {DataPoint} point - Point to validate
   * @param {string} path - Path for error messages
   * @throws {ValidationError} If point is invalid
   * @private
   */
  #validatePoint(point, path) {
    requiredObject(point, path);
    requiredString(point.metric, `${path}.metric`);
    required(point.value, `${path}.value`);
    required(point.timestamp, `${path}.timestamp`);

    if (typeof point.value !== 'number' || !isFinite(point.value)) {
      throw new ValidationError(`${path}.value must be a finite number`, `${path}.value`);
    }

    if (point.tags !== undefined) {
      this.#validateTags(point.tags);
    }
  }

  /**
   * Validate a query request.
   * @param {QueryRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateQueryRequest(request) {
    validateRequest(request);
    requiredString(request.metric, 'metric');
    required(request.start, 'start');
    required(request.end, 'end');

    const startDate = new Date(request.start);
    const endDate = new Date(request.end);

    if (isNaN(startDate.getTime())) {
      throw new ValidationError('start must be a valid Date or ISO 8601 string', 'start');
    }
    if (isNaN(endDate.getTime())) {
      throw new ValidationError('end must be a valid Date or ISO 8601 string', 'end');
    }
    if (endDate <= startDate) {
      throw new ValidationError('end must be after start', 'end');
    }

    if (request.tags !== undefined) {
      this.#validateTags(request.tags);
    }

    if (request.aggregation !== undefined) {
      oneOf(request.aggregation, VALID_AGGREGATIONS, 'aggregation');

      if (request.aggregation !== Aggregation.NONE) {
        if (!request.interval) {
          throw new ValidationError('interval is required when aggregation is set', 'interval');
        }
        oneOf(request.interval, VALID_INTERVALS, 'interval');
      }
    }

    if (request.groupBy !== undefined) {
      if (!Array.isArray(request.groupBy)) {
        throw new ValidationError('groupBy must be an array of strings', 'groupBy');
      }
      for (let i = 0; i < request.groupBy.length; i++) {
        if (typeof request.groupBy[i] !== 'string' || request.groupBy[i].trim() === '') {
          throw new ValidationError(`groupBy[${i}] must be a non-empty string`, `groupBy[${i}]`);
        }
      }
    }
  }

  /**
   * Validate an aggregate request.
   * @param {AggregateRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateAggregateRequest(request) {
    validateRequest(request);
    requiredString(request.metric, 'metric');
    required(request.start, 'start');
    required(request.end, 'end');
    requiredString(request.aggregation, 'aggregation');

    const startDate = new Date(request.start);
    const endDate = new Date(request.end);

    if (isNaN(startDate.getTime())) {
      throw new ValidationError('start must be a valid Date or ISO 8601 string', 'start');
    }
    if (isNaN(endDate.getTime())) {
      throw new ValidationError('end must be a valid Date or ISO 8601 string', 'end');
    }
    if (endDate <= startDate) {
      throw new ValidationError('end must be after start', 'end');
    }

    oneOf(request.aggregation, VALID_AGGREGATIONS, 'aggregation');

    if (request.aggregation === Aggregation.NONE) {
      throw new ValidationError('aggregation cannot be NONE for aggregate operation', 'aggregation');
    }

    if (request.tags !== undefined) {
      this.#validateTags(request.tags);
    }
  }

  /**
   * Validate list metrics options.
   * @param {ListMetricsOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListMetricsOptions(options) {
    if (options.prefix !== undefined && options.prefix !== null) {
      if (typeof options.prefix !== 'string') {
        throw new ValidationError('prefix must be a string', 'prefix');
      }
    }

    positiveInteger(options.limit, 'limit');

    if (options.cursor !== undefined && options.cursor !== null) {
      if (typeof options.cursor !== 'string') {
        throw new ValidationError('cursor must be a string', 'cursor');
      }
    }
  }

  /**
   * Validate a retention policy.
   * @param {RetentionPolicy} policy - Policy to validate
   * @throws {ValidationError} If policy is invalid
   * @private
   */
  #validateRetentionPolicy(policy) {
    required(policy.retentionDays, 'policy.retentionDays');
    positiveInteger(policy.retentionDays, 'policy.retentionDays');

    if (policy.aggregation !== undefined) {
      oneOf(policy.aggregation, VALID_AGGREGATIONS, 'policy.aggregation');
    }

    if (policy.interval !== undefined) {
      oneOf(policy.interval, VALID_INTERVALS, 'policy.interval');
    }

    // If one of aggregation/interval is set, the other should be too
    if ((policy.aggregation && !policy.interval) || (!policy.aggregation && policy.interval)) {
      throw new ValidationError(
        'both aggregation and interval must be set together for downsampling',
        'policy'
      );
    }
  }

  /**
   * Build the request body for a query operation.
   * @param {QueryRequest} request - Query request
   * @returns {Object} Request body
   * @private
   */
  #buildQueryRequestBody(request) {
    const body = {
      metric: request.metric,
      start: this.#formatTimestamp(request.start),
      end: this.#formatTimestamp(request.end),
    };

    if (request.tags && Object.keys(request.tags).length > 0) {
      body.tags = request.tags;
    }

    if (request.aggregation) {
      body.aggregation = request.aggregation;
    }

    if (request.interval) {
      body.interval = request.interval;
    }

    if (request.groupBy && request.groupBy.length > 0) {
      body.groupBy = request.groupBy;
    }

    return body;
  }

  /**
   * Normalize query response from API.
   * @param {Object} response - Raw API response
   * @returns {QueryResponse} Normalized response
   * @private
   */
  #normalizeQueryResponse(response) {
    if (!response) {
      return { series: [] };
    }

    const series = response.series ?? [];

    return {
      series: series.map(s => ({
        tags: s.tags ?? {},
        points: (s.points ?? []).map(p => ({
          timestamp: p.timestamp,
          value: p.value,
        })),
      })),
    };
  }

  /**
   * Normalize retention policy from API.
   * @param {Object} policy - Raw policy from API
   * @returns {RetentionPolicy} Normalized policy
   * @private
   */
  #normalizeRetentionPolicy(policy) {
    if (!policy) {
      return {
        retentionDays: 0,
        aggregation: null,
        interval: null,
      };
    }

    return {
      retentionDays: policy.retentionDays ?? policy.retention_days ?? 0,
      aggregation: policy.aggregation ?? null,
      interval: policy.interval ?? null,
    };
  }
}

export default TimeSeriesClient;
