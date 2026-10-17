/**
 * QuicBaaS JavaScript SDK - Error Types
 *
 * @module @quicbaas/client/errors
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

/**
 * Base error class for all QuicBaaS SDK errors.
 */
export class QuicBaaSError extends Error {
  /**
   * @param {string} message - Error message
   * @param {string} code - Error code
   * @param {Object} [details] - Additional error details
   */
  constructor(message, code, details = {}) {
    super(message);
    this.name = 'QuicBaaSError';
    this.code = code;
    this.details = details;
    this.timestamp = new Date().toISOString();

    // Maintains proper stack trace for where error was thrown
    if (Error.captureStackTrace) {
      Error.captureStackTrace(this, this.constructor);
    }
  }

  /**
   * Convert error to JSON representation.
   * @returns {Object}
   */
  toJSON() {
    return {
      name: this.name,
      message: this.message,
      code: this.code,
      details: this.details,
      timestamp: this.timestamp,
    };
  }
}

/**
 * Error thrown when input validation fails.
 */
export class ValidationError extends QuicBaaSError {
  constructor(message, field = null) {
    super(message, 'VALIDATION_ERROR', { field });
    this.name = 'ValidationError';
  }
}

/**
 * Error thrown when a resource is not found.
 */
export class NotFoundError extends QuicBaaSError {
  constructor(message, resource = null, key = null) {
    super(message, 'NOT_FOUND', { resource, key });
    this.name = 'NotFoundError';
  }
}

/**
 * Error thrown when authentication fails.
 */
export class AuthenticationError extends QuicBaaSError {
  constructor(message = 'Authentication failed') {
    super(message, 'AUTHENTICATION_ERROR');
    this.name = 'AuthenticationError';
  }
}

/**
 * Error thrown when authorization fails.
 */
export class AuthorizationError extends QuicBaaSError {
  constructor(message = 'Access denied') {
    super(message, 'AUTHORIZATION_ERROR');
    this.name = 'AuthorizationError';
  }
}

/**
 * Error thrown when a request times out.
 */
export class TimeoutError extends QuicBaaSError {
  constructor(message = 'Request timed out', timeoutMs = null) {
    super(message, 'TIMEOUT', { timeoutMs });
    this.name = 'TimeoutError';
  }
}

/**
 * Error thrown when a network error occurs.
 */
export class NetworkError extends QuicBaaSError {
  constructor(message = 'Network error', cause = null) {
    super(message, 'NETWORK_ERROR', { cause: cause?.message });
    this.name = 'NetworkError';
    this.cause = cause;
  }
}

/**
 * Error thrown when the server returns an error.
 */
export class ServerError extends QuicBaaSError {
  constructor(message, statusCode, response = null) {
    super(message, 'SERVER_ERROR', { statusCode, response });
    this.name = 'ServerError';
    this.statusCode = statusCode;
  }
}

/**
 * Error thrown when a conflict occurs (e.g., concurrent modification).
 */
export class ConflictError extends QuicBaaSError {
  constructor(message = 'Conflict detected', resource = null) {
    super(message, 'CONFLICT', { resource });
    this.name = 'ConflictError';
  }
}

/**
 * Error thrown when a resource already exists.
 */
export class AlreadyExistsError extends QuicBaaSError {
  constructor(message, resource = null, key = null) {
    super(message, 'ALREADY_EXISTS', { resource, key });
    this.name = 'AlreadyExistsError';
  }
}

/**
 * Error thrown when rate limit is exceeded.
 */
export class RateLimitError extends QuicBaaSError {
  constructor(message = 'Rate limit exceeded', retryAfter = null) {
    super(message, 'RATE_LIMIT', { retryAfter });
    this.name = 'RateLimitError';
    this.retryAfter = retryAfter;
  }
}

/**
 * Error thrown when the database is sealed.
 */
export class SealedError extends QuicBaaSError {
  constructor(message = 'Database is sealed') {
    super(message, 'SEALED');
    this.name = 'SealedError';
  }
}

/**
 * Error thrown when an operation is not supported.
 */
export class UnsupportedOperationError extends QuicBaaSError {
  constructor(message, operation = null) {
    super(message, 'UNSUPPORTED_OPERATION', { operation });
    this.name = 'UnsupportedOperationError';
  }
}

/**
 * Error codes mapping.
 */
export const ErrorCodes = {
  VALIDATION_ERROR: 'VALIDATION_ERROR',
  NOT_FOUND: 'NOT_FOUND',
  AUTHENTICATION_ERROR: 'AUTHENTICATION_ERROR',
  AUTHORIZATION_ERROR: 'AUTHORIZATION_ERROR',
  TIMEOUT: 'TIMEOUT',
  NETWORK_ERROR: 'NETWORK_ERROR',
  SERVER_ERROR: 'SERVER_ERROR',
  CONFLICT: 'CONFLICT',
  ALREADY_EXISTS: 'ALREADY_EXISTS',
  RATE_LIMIT: 'RATE_LIMIT',
  SEALED: 'SEALED',
  UNSUPPORTED_OPERATION: 'UNSUPPORTED_OPERATION',
};

/**
 * Create an appropriate error from an HTTP response.
 * @param {Response} response - Fetch API response
 * @param {Object} [body] - Parsed response body
 * @returns {QuicBaaSError}
 */
export function fromResponse(response, body = null) {
  const message = body?.message || body?.error || response.statusText;

  switch (response.status) {
  case 400:
    return new ValidationError(message);
  case 401:
    return new AuthenticationError(message);
  case 403:
    return new AuthorizationError(message);
  case 404:
    return new NotFoundError(message);
  case 409:
    return new ConflictError(message);
  case 423:
    return new SealedError(message);
  case 429:
    return new RateLimitError(message, response.headers.get('Retry-After'));
  case 408:
  case 504:
    return new TimeoutError(message);
  default:
    if (response.status >= 500) {
      return new ServerError(message, response.status, body);
    }
    return new QuicBaaSError(message, 'UNKNOWN_ERROR', { statusCode: response.status });
  }
}
