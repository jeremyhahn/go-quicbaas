/**
 * QuicBaaS JavaScript SDK - HTTP Transport Layer
 *
 * @module @quicbaas/client/transport
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import {
  QuicBaaSError,
  NetworkError,
  TimeoutError,
  fromResponse,
} from '../errors/index.js';

/**
 * Default configuration for HTTP transport.
 */
const DEFAULT_CONFIG = {
  timeout: 30000,
  retries: 3,
  retryDelay: 1000,
  retryBackoff: 2,
  headers: {},
};

/**
 * HTTP Transport for QuicBaaS API communication.
 */
export class HttpTransport {
  /**
   * @param {string} baseUrl - Base URL for the QuicBaaS API
   * @param {Object} [config] - Transport configuration
   * @param {number} [config.timeout=30000] - Request timeout in milliseconds
   * @param {number} [config.retries=3] - Number of retry attempts
   * @param {number} [config.retryDelay=1000] - Initial retry delay in milliseconds
   * @param {number} [config.retryBackoff=2] - Retry backoff multiplier
   * @param {Object} [config.headers={}] - Default headers to include
   */
  constructor(baseUrl, config = {}) {
    if (!baseUrl) {
      throw new QuicBaaSError('Base URL is required', 'INVALID_CONFIG');
    }

    this.baseUrl = baseUrl.replace(/\/$/, ''); // Remove trailing slash
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.authToken = null;
    this.refreshToken = null;
    this.onTokenRefresh = null;
    this.interceptors = {
      request: [],
      response: [],
    };
  }

  /**
   * Set the authentication token.
   * @param {string} token - JWT or access token
   * @param {string} [refreshToken] - Refresh token for automatic renewal
   */
  setAuthToken(token, refreshToken = null) {
    this.authToken = token;
    this.refreshToken = refreshToken;
  }

  /**
   * Clear authentication tokens.
   */
  clearAuthToken() {
    this.authToken = null;
    this.refreshToken = null;
  }

  /**
   * Set callback for token refresh.
   * @param {Function} callback - Called with new tokens after refresh
   */
  setTokenRefreshCallback(callback) {
    this.onTokenRefresh = callback;
  }

  /**
   * Add a request interceptor.
   * @param {Function} interceptor - Function to intercept requests
   * @returns {number} Interceptor index for removal
   */
  addRequestInterceptor(interceptor) {
    return this.interceptors.request.push(interceptor) - 1;
  }

  /**
   * Add a response interceptor.
   * @param {Function} interceptor - Function to intercept responses
   * @returns {number} Interceptor index for removal
   */
  addResponseInterceptor(interceptor) {
    return this.interceptors.response.push(interceptor) - 1;
  }

  /**
   * Remove a request interceptor.
   * @param {number} index - Interceptor index
   */
  removeRequestInterceptor(index) {
    this.interceptors.request.splice(index, 1);
  }

  /**
   * Remove a response interceptor.
   * @param {number} index - Interceptor index
   */
  removeResponseInterceptor(index) {
    this.interceptors.response.splice(index, 1);
  }

  /**
   * Build request headers.
   * @param {Object} [additionalHeaders] - Additional headers to include
   * @returns {Object}
   * @private
   */
  _buildHeaders(additionalHeaders = {}) {
    const headers = {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
      ...this.config.headers,
      ...additionalHeaders,
    };

    if (this.authToken) {
      headers['Authorization'] = `Bearer ${this.authToken}`;
    }

    return headers;
  }

  /**
   * Make an HTTP request with retry logic.
   * @param {string} method - HTTP method
   * @param {string} path - API path
   * @param {Object} [options] - Request options
   * @param {Object} [options.body] - Request body
   * @param {Object} [options.headers] - Additional headers
   * @param {Object} [options.query] - Query parameters
   * @param {number} [options.timeout] - Request timeout override
   * @returns {Promise<Object>}
   */
  async request(method, path, options = {}) {
    const url = this._buildUrl(path, options.query);
    let requestConfig = {
      method,
      headers: this._buildHeaders(options.headers),
      signal: this._createAbortSignal(options.timeout || this.config.timeout),
    };

    if (options.body && method !== 'GET' && method !== 'HEAD') {
      requestConfig.body = JSON.stringify(options.body);
    }

    // Apply request interceptors
    for (const interceptor of this.interceptors.request) {
      requestConfig = await interceptor(requestConfig, url);
    }

    let lastError;
    for (let attempt = 0; attempt <= this.config.retries; attempt++) {
      try {
        const response = await this._executeRequest(url, requestConfig);
        return response;
      } catch (error) {
        lastError = error;

        // Don't retry on certain errors
        if (this._shouldNotRetry(error)) {
          throw error;
        }

        // Wait before retrying
        if (attempt < this.config.retries) {
          const delay = this.config.retryDelay * Math.pow(this.config.retryBackoff, attempt);
          await this._sleep(delay);
        }
      }
    }

    throw lastError;
  }

  /**
   * Execute a single HTTP request.
   * @param {string} url - Full URL
   * @param {Object} config - Request configuration
   * @returns {Promise<Object>}
   * @private
   */
  async _executeRequest(url, config) {
    let response;

    try {
      response = await fetch(url, config);
    } catch (error) {
      if (error.name === 'AbortError') {
        throw new TimeoutError('Request timed out', this.config.timeout);
      }
      throw new NetworkError('Failed to connect to server', error);
    }

    // Apply response interceptors
    for (const interceptor of this.interceptors.response) {
      response = await interceptor(response);
    }

    // Parse response body
    let body = null;
    const contentType = response.headers.get('Content-Type');
    if (contentType && contentType.includes('application/json')) {
      try {
        const text = await response.text();
        body = text ? JSON.parse(text) : null;
      } catch {
        body = null;
      }
    }

    // Handle error responses
    if (!response.ok) {
      throw fromResponse(response, body);
    }

    return body;
  }

  /**
   * Build full URL with query parameters.
   * @param {string} path - API path
   * @param {Object} [query] - Query parameters
   * @returns {string}
   * @private
   */
  _buildUrl(path, query = null) {
    const url = new URL(path, this.baseUrl);

    if (query) {
      Object.entries(query).forEach(([key, value]) => {
        if (value !== undefined && value !== null) {
          if (Array.isArray(value)) {
            value.forEach(v => url.searchParams.append(key, v));
          } else {
            url.searchParams.append(key, value);
          }
        }
      });
    }

    return url.toString();
  }

  /**
   * Create an abort signal for request timeout.
   * @param {number} timeout - Timeout in milliseconds
   * @returns {AbortSignal}
   * @private
   */
  _createAbortSignal(timeout) {
    const controller = new AbortController();
    setTimeout(() => controller.abort(), timeout);
    return controller.signal;
  }

  /**
   * Check if an error should not be retried.
   * @param {Error} error - The error to check
   * @returns {boolean}
   * @private
   */
  _shouldNotRetry(error) {
    // Don't retry client errors (4xx), only server errors (5xx) and network errors
    if (error.statusCode && error.statusCode >= 400 && error.statusCode < 500) {
      return true;
    }
    // Don't retry validation errors
    if (error.code === 'VALIDATION_ERROR') {
      return true;
    }
    // Don't retry authentication/authorization errors
    if (error.code === 'AUTHENTICATION_ERROR' || error.code === 'AUTHORIZATION_ERROR') {
      return true;
    }
    return false;
  }

  /**
   * Sleep for a specified duration.
   * @param {number} ms - Duration in milliseconds
   * @returns {Promise<void>}
   * @private
   */
  _sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
  }

  // Convenience methods

  /**
   * Make a GET request.
   * @param {string} path - API path
   * @param {Object} [options] - Request options
   * @returns {Promise<Object>}
   */
  get(path, options = {}) {
    return this.request('GET', path, options);
  }

  /**
   * Make a POST request.
   * @param {string} path - API path
   * @param {Object} [body] - Request body
   * @param {Object} [options] - Request options
   * @returns {Promise<Object>}
   */
  post(path, body = null, options = {}) {
    return this.request('POST', path, { ...options, body });
  }

  /**
   * Make a PUT request.
   * @param {string} path - API path
   * @param {Object} [body] - Request body
   * @param {Object} [options] - Request options
   * @returns {Promise<Object>}
   */
  put(path, body = null, options = {}) {
    return this.request('PUT', path, { ...options, body });
  }

  /**
   * Make a PATCH request.
   * @param {string} path - API path
   * @param {Object} [body] - Request body
   * @param {Object} [options] - Request options
   * @returns {Promise<Object>}
   */
  patch(path, body = null, options = {}) {
    return this.request('PATCH', path, { ...options, body });
  }

  /**
   * Make a DELETE request.
   * @param {string} path - API path
   * @param {Object} [options] - Request options
   * @returns {Promise<Object>}
   */
  delete(path, options = {}) {
    return this.request('DELETE', path, options);
  }
}

export default HttpTransport;
