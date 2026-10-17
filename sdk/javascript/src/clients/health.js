/**
 * QuicBaaS JavaScript SDK - Health Client
 *
 * @module @quicbaas/client/clients/health
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import { required } from '../utils/validation.js';

/**
 * Health status enumeration for service health states.
 * @readonly
 * @enum {string}
 */
export const HealthStatus = Object.freeze({
  /** Service is fully operational */
  HEALTHY: 'healthy',
  /** Service is operational but experiencing issues */
  DEGRADED: 'degraded',
  /** Service is not operational */
  UNHEALTHY: 'unhealthy',
});

/**
 * @typedef {Object} HealthOptions
 * @property {boolean} [includeDetails=false] - Include detailed component health information
 * @property {Array<string>} [components] - Specific components to check
 */

/**
 * @typedef {Object} ComponentHealth
 * @property {string} name - Component name
 * @property {string} status - Component health status
 * @property {string} [message] - Optional status message
 * @property {number} [latencyMs] - Component response latency in milliseconds
 */

/**
 * @typedef {Object} HealthResponse
 * @property {boolean} healthy - Whether the service is healthy
 * @property {string} status - Overall health status (healthy, degraded, unhealthy)
 * @property {Object<string, *>} [details] - Detailed health information by component
 * @property {string} [version] - Service version
 * @property {string} [uptime] - Service uptime
 * @property {string} timestamp - Health check timestamp
 */

/**
 * @typedef {Object} LivenessResponse
 * @property {boolean} alive - Whether the service is alive
 * @property {string} status - Liveness status
 * @property {string} timestamp - Check timestamp
 */

/**
 * @typedef {Object} ReadinessResponse
 * @property {boolean} ready - Whether the service is ready to accept traffic
 * @property {string} status - Readiness status
 * @property {string} [reason] - Reason if not ready
 * @property {string} timestamp - Check timestamp
 */

/**
 * Health client for QuicBaaS.
 * Provides health check operations for monitoring service availability and readiness.
 * Designed for use with Kubernetes liveness and readiness probes.
 */
class HealthClient {
  /**
   * API path prefix for health operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/health';

  /**
   * Create a new Health client.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  /**
   * Perform a full health check with optional component details.
   * Returns comprehensive health information about the service and its dependencies.
   *
   * @param {HealthOptions} [options={}] - Health check options
   * @returns {Promise<HealthResponse>} The health response with status and details
   * @throws {ValidationError} If options contain invalid values
   *
   * @example
   * // Basic health check
   * const health = await healthClient.health();
   * console.log(health.status); // 'healthy'
   *
   * @example
   * // Health check with component details
   * const health = await healthClient.health({ includeDetails: true });
   * console.log(health.details.storage); // { status: 'healthy', latencyMs: 5 }
   */
  async health(options = {}) {
    this.#validateHealthOptions(options);

    const query = {};

    if (options.includeDetails === true) {
      query.details = 'true';
    }

    if (options.components && options.components.length > 0) {
      query.components = options.components.join(',');
    }

    const response = await this.#transport.get(HealthClient.API_PREFIX, { query });

    return this.#normalizeHealthResponse(response);
  }

  /**
   * Perform a simple liveness check.
   * Used by Kubernetes liveness probes to determine if the service is running.
   * This is a lightweight check that returns quickly.
   *
   * @returns {Promise<LivenessResponse>} The liveness response
   *
   * @example
   * // Kubernetes liveness probe
   * const liveness = await healthClient.liveness();
   * if (!liveness.alive) {
   *   // Service needs restart
   * }
   */
  async liveness() {
    const path = `${HealthClient.API_PREFIX}/live`;
    const response = await this.#transport.get(path);

    return {
      alive: response?.alive ?? response?.healthy ?? true,
      status: response?.status ?? HealthStatus.HEALTHY,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Perform a readiness check.
   * Used by Kubernetes readiness probes to determine if the service
   * is ready to accept traffic. Unlike liveness, this checks if
   * dependencies are available and the service can serve requests.
   *
   * @returns {Promise<ReadinessResponse>} The readiness response
   *
   * @example
   * // Kubernetes readiness probe
   * const readiness = await healthClient.readiness();
   * if (!readiness.ready) {
   *   console.log('Service not ready:', readiness.reason);
   * }
   */
  async readiness() {
    const path = `${HealthClient.API_PREFIX}/ready`;
    const response = await this.#transport.get(path);

    return {
      ready: response?.ready ?? response?.healthy ?? true,
      status: response?.status ?? HealthStatus.HEALTHY,
      reason: response?.reason ?? null,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Validate health check options.
   * @param {HealthOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateHealthOptions(options) {
    if (options.includeDetails !== undefined && typeof options.includeDetails !== 'boolean') {
      throw new ValidationError('options.includeDetails must be a boolean', 'options.includeDetails');
    }

    if (options.components !== undefined) {
      if (!Array.isArray(options.components)) {
        throw new ValidationError('options.components must be an array', 'options.components');
      }

      for (let i = 0; i < options.components.length; i++) {
        const component = options.components[i];
        if (typeof component !== 'string' || component.trim() === '') {
          throw new ValidationError(
            `options.components[${i}] must be a non-empty string`,
            `options.components[${i}]`,
          );
        }
      }
    }
  }

  /**
   * Normalize the health response to ensure consistent structure.
   * @param {Object} response - Raw response from the server
   * @returns {HealthResponse} Normalized health response
   * @private
   */
  #normalizeHealthResponse(response) {
    const status = response?.status ?? HealthStatus.HEALTHY;
    const healthy = response?.healthy ?? (status === HealthStatus.HEALTHY);

    const normalized = {
      healthy,
      status,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };

    if (response?.details !== undefined && response.details !== null) {
      normalized.details = response.details;
    }

    if (response?.version !== undefined) {
      normalized.version = response.version;
    }

    if (response?.uptime !== undefined) {
      normalized.uptime = response.uptime;
    }

    return normalized;
  }
}

export default HealthClient;
