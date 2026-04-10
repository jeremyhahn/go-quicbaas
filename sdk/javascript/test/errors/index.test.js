/**
 * QuicBaaS JavaScript SDK - Error Types Tests
 *
 * @module @quicbaas/client/errors/tests
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import {
  QuicBaaSError,
  ValidationError,
  NotFoundError,
  AuthenticationError,
  AuthorizationError,
  TimeoutError,
  NetworkError,
  ServerError,
  ConflictError,
  AlreadyExistsError,
  RateLimitError,
  SealedError,
  UnsupportedOperationError,
  ErrorCodes,
  fromResponse,
} from '../../src/errors/index.js';

describe('Errors', () => {
  describe('QuicBaaSError', () => {
    it('should create error with message, code, and default details', () => {
      const error = new QuicBaaSError('Test error', 'TEST_CODE');

      expect(error).toBeInstanceOf(Error);
      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error.name).toBe('QuicBaaSError');
      expect(error.message).toBe('Test error');
      expect(error.code).toBe('TEST_CODE');
      expect(error.details).toEqual({});
      expect(error.timestamp).toBeDefined();
      expect(typeof error.timestamp).toBe('string');
    });

    it('should create error with custom details', () => {
      const details = { field: 'username', context: 'registration' };
      const error = new QuicBaaSError('Test error', 'TEST_CODE', details);

      expect(error.details).toEqual(details);
    });

    it('should have a valid timestamp in ISO format', () => {
      const beforeCreate = new Date().toISOString();
      const error = new QuicBaaSError('Test error', 'TEST_CODE');
      const afterCreate = new Date().toISOString();

      expect(error.timestamp >= beforeCreate).toBe(true);
      expect(error.timestamp <= afterCreate).toBe(true);
    });

    it('should have proper stack trace', () => {
      const error = new QuicBaaSError('Test error', 'TEST_CODE');

      expect(error.stack).toBeDefined();
      expect(error.stack).toContain('QuicBaaSError');
    });

    it('should handle environment without Error.captureStackTrace', () => {
      const originalCaptureStackTrace = Error.captureStackTrace;
      Error.captureStackTrace = undefined;

      try {
        const error = new QuicBaaSError('Test error', 'TEST_CODE');
        expect(error).toBeInstanceOf(Error);
        expect(error.message).toBe('Test error');
        expect(error.stack).toBeDefined();
      } finally {
        Error.captureStackTrace = originalCaptureStackTrace;
      }
    });

    describe('toJSON', () => {
      it('should convert error to JSON representation', () => {
        const details = { field: 'test' };
        const error = new QuicBaaSError('Test message', 'TEST_CODE', details);
        const json = error.toJSON();

        expect(json).toEqual({
          name: 'QuicBaaSError',
          message: 'Test message',
          code: 'TEST_CODE',
          details: { field: 'test' },
          timestamp: error.timestamp,
        });
      });

      it('should produce valid JSON output', () => {
        const error = new QuicBaaSError('Test', 'CODE');
        const jsonString = JSON.stringify(error);
        const parsed = JSON.parse(jsonString);

        expect(parsed.name).toBe('QuicBaaSError');
        expect(parsed.message).toBe('Test');
        expect(parsed.code).toBe('CODE');
      });
    });
  });

  describe('ValidationError', () => {
    it('should create error with message and field', () => {
      const error = new ValidationError('Field is required', 'username');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(ValidationError);
      expect(error.name).toBe('ValidationError');
      expect(error.message).toBe('Field is required');
      expect(error.code).toBe('VALIDATION_ERROR');
      expect(error.details).toEqual({ field: 'username' });
    });

    it('should create error with default null field', () => {
      const error = new ValidationError('Invalid input');

      expect(error.details).toEqual({ field: null });
    });

    it('should handle empty string field', () => {
      const error = new ValidationError('Invalid', '');

      expect(error.details.field).toBe('');
    });
  });

  describe('NotFoundError', () => {
    it('should create error with message, resource, and key', () => {
      const error = new NotFoundError('Key not found', 'kv', 'user:123');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(NotFoundError);
      expect(error.name).toBe('NotFoundError');
      expect(error.message).toBe('Key not found');
      expect(error.code).toBe('NOT_FOUND');
      expect(error.details).toEqual({ resource: 'kv', key: 'user:123' });
    });

    it('should create error with default null values', () => {
      const error = new NotFoundError('Not found');

      expect(error.details).toEqual({ resource: null, key: null });
    });

    it('should create error with only resource', () => {
      const error = new NotFoundError('Resource not found', 'bucket');

      expect(error.details).toEqual({ resource: 'bucket', key: null });
    });
  });

  describe('AuthenticationError', () => {
    it('should create error with custom message', () => {
      const error = new AuthenticationError('Invalid token');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(AuthenticationError);
      expect(error.name).toBe('AuthenticationError');
      expect(error.message).toBe('Invalid token');
      expect(error.code).toBe('AUTHENTICATION_ERROR');
    });

    it('should create error with default message', () => {
      const error = new AuthenticationError();

      expect(error.message).toBe('Authentication failed');
    });
  });

  describe('AuthorizationError', () => {
    it('should create error with custom message', () => {
      const error = new AuthorizationError('Insufficient permissions');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(AuthorizationError);
      expect(error.name).toBe('AuthorizationError');
      expect(error.message).toBe('Insufficient permissions');
      expect(error.code).toBe('AUTHORIZATION_ERROR');
    });

    it('should create error with default message', () => {
      const error = new AuthorizationError();

      expect(error.message).toBe('Access denied');
    });
  });

  describe('TimeoutError', () => {
    it('should create error with custom message and timeout', () => {
      const error = new TimeoutError('Operation timed out', 5000);

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(TimeoutError);
      expect(error.name).toBe('TimeoutError');
      expect(error.message).toBe('Operation timed out');
      expect(error.code).toBe('TIMEOUT');
      expect(error.details).toEqual({ timeoutMs: 5000 });
    });

    it('should create error with default values', () => {
      const error = new TimeoutError();

      expect(error.message).toBe('Request timed out');
      expect(error.details).toEqual({ timeoutMs: null });
    });

    it('should handle zero timeout', () => {
      const error = new TimeoutError('Immediate timeout', 0);

      expect(error.details.timeoutMs).toBe(0);
    });
  });

  describe('NetworkError', () => {
    it('should create error with custom message and cause', () => {
      const cause = new Error('Connection refused');
      const error = new NetworkError('Failed to connect', cause);

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(NetworkError);
      expect(error.name).toBe('NetworkError');
      expect(error.message).toBe('Failed to connect');
      expect(error.code).toBe('NETWORK_ERROR');
      expect(error.cause).toBe(cause);
      expect(error.details.cause).toBe('Connection refused');
    });

    it('should create error with default values', () => {
      const error = new NetworkError();

      expect(error.message).toBe('Network error');
      expect(error.cause).toBe(null);
      expect(error.details.cause).toBeUndefined();
    });

    it('should handle cause without message', () => {
      const cause = {};
      const error = new NetworkError('Network issue', cause);

      expect(error.cause).toBe(cause);
      expect(error.details.cause).toBeUndefined();
    });
  });

  describe('ServerError', () => {
    it('should create error with message, statusCode, and response', () => {
      const response = { data: 'error details' };
      const error = new ServerError('Internal server error', 500, response);

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(ServerError);
      expect(error.name).toBe('ServerError');
      expect(error.message).toBe('Internal server error');
      expect(error.code).toBe('SERVER_ERROR');
      expect(error.statusCode).toBe(500);
      expect(error.details).toEqual({ statusCode: 500, response });
    });

    it('should create error without response', () => {
      const error = new ServerError('Server error', 502);

      expect(error.statusCode).toBe(502);
      expect(error.details.response).toBe(null);
    });

    it('should handle various HTTP status codes', () => {
      const error503 = new ServerError('Service unavailable', 503);
      const error504 = new ServerError('Gateway timeout', 504);

      expect(error503.statusCode).toBe(503);
      expect(error504.statusCode).toBe(504);
    });
  });

  describe('ConflictError', () => {
    it('should create error with custom message and resource', () => {
      const error = new ConflictError('Version mismatch', 'document');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(ConflictError);
      expect(error.name).toBe('ConflictError');
      expect(error.message).toBe('Version mismatch');
      expect(error.code).toBe('CONFLICT');
      expect(error.details).toEqual({ resource: 'document' });
    });

    it('should create error with default values', () => {
      const error = new ConflictError();

      expect(error.message).toBe('Conflict detected');
      expect(error.details).toEqual({ resource: null });
    });
  });

  describe('AlreadyExistsError', () => {
    it('should create error with message, resource, and key', () => {
      const error = new AlreadyExistsError('User already exists', 'user', 'john@example.com');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(AlreadyExistsError);
      expect(error.name).toBe('AlreadyExistsError');
      expect(error.message).toBe('User already exists');
      expect(error.code).toBe('ALREADY_EXISTS');
      expect(error.details).toEqual({ resource: 'user', key: 'john@example.com' });
    });

    it('should create error with only message', () => {
      const error = new AlreadyExistsError('Resource exists');

      expect(error.details).toEqual({ resource: null, key: null });
    });

    it('should create error with message and resource only', () => {
      const error = new AlreadyExistsError('Bucket exists', 'bucket');

      expect(error.details).toEqual({ resource: 'bucket', key: null });
    });
  });

  describe('RateLimitError', () => {
    it('should create error with custom message and retryAfter', () => {
      const error = new RateLimitError('Too many requests', 30);

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(RateLimitError);
      expect(error.name).toBe('RateLimitError');
      expect(error.message).toBe('Too many requests');
      expect(error.code).toBe('RATE_LIMIT');
      expect(error.retryAfter).toBe(30);
      expect(error.details).toEqual({ retryAfter: 30 });
    });

    it('should create error with default values', () => {
      const error = new RateLimitError();

      expect(error.message).toBe('Rate limit exceeded');
      expect(error.retryAfter).toBe(null);
    });

    it('should handle retryAfter as string', () => {
      const error = new RateLimitError('Throttled', '60');

      expect(error.retryAfter).toBe('60');
    });
  });

  describe('SealedError', () => {
    it('should create error with custom message', () => {
      const error = new SealedError('Database is currently sealed');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(SealedError);
      expect(error.name).toBe('SealedError');
      expect(error.message).toBe('Database is currently sealed');
      expect(error.code).toBe('SEALED');
    });

    it('should create error with default message', () => {
      const error = new SealedError();

      expect(error.message).toBe('Database is sealed');
    });
  });

  describe('UnsupportedOperationError', () => {
    it('should create error with message and operation', () => {
      const error = new UnsupportedOperationError('Operation not available', 'batch-delete');

      expect(error).toBeInstanceOf(QuicBaaSError);
      expect(error).toBeInstanceOf(UnsupportedOperationError);
      expect(error.name).toBe('UnsupportedOperationError');
      expect(error.message).toBe('Operation not available');
      expect(error.code).toBe('UNSUPPORTED_OPERATION');
      expect(error.details).toEqual({ operation: 'batch-delete' });
    });

    it('should create error without operation', () => {
      const error = new UnsupportedOperationError('Not supported');

      expect(error.details).toEqual({ operation: null });
    });
  });

  describe('ErrorCodes', () => {
    it('should define all error codes', () => {
      expect(ErrorCodes.VALIDATION_ERROR).toBe('VALIDATION_ERROR');
      expect(ErrorCodes.NOT_FOUND).toBe('NOT_FOUND');
      expect(ErrorCodes.AUTHENTICATION_ERROR).toBe('AUTHENTICATION_ERROR');
      expect(ErrorCodes.AUTHORIZATION_ERROR).toBe('AUTHORIZATION_ERROR');
      expect(ErrorCodes.TIMEOUT).toBe('TIMEOUT');
      expect(ErrorCodes.NETWORK_ERROR).toBe('NETWORK_ERROR');
      expect(ErrorCodes.SERVER_ERROR).toBe('SERVER_ERROR');
      expect(ErrorCodes.CONFLICT).toBe('CONFLICT');
      expect(ErrorCodes.ALREADY_EXISTS).toBe('ALREADY_EXISTS');
      expect(ErrorCodes.RATE_LIMIT).toBe('RATE_LIMIT');
      expect(ErrorCodes.SEALED).toBe('SEALED');
      expect(ErrorCodes.UNSUPPORTED_OPERATION).toBe('UNSUPPORTED_OPERATION');
    });

    it('should have exactly 12 error codes', () => {
      expect(Object.keys(ErrorCodes).length).toBe(12);
    });
  });

  describe('fromResponse', () => {
    const mockResponse = (status, statusText = 'Error') => ({
      status,
      statusText,
      headers: {
        get: jest.fn().mockReturnValue(null),
      },
    });

    describe('400 Bad Request', () => {
      it('should return ValidationError for status 400', () => {
        const response = mockResponse(400, 'Bad Request');
        const body = { message: 'Invalid input' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(ValidationError);
        expect(error.message).toBe('Invalid input');
      });

      it('should use body.error if message is not present', () => {
        const response = mockResponse(400);
        const body = { error: 'Validation failed' };
        const error = fromResponse(response, body);

        expect(error.message).toBe('Validation failed');
      });
    });

    describe('401 Unauthorized', () => {
      it('should return AuthenticationError for status 401', () => {
        const response = mockResponse(401, 'Unauthorized');
        const body = { message: 'Invalid credentials' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(AuthenticationError);
        expect(error.message).toBe('Invalid credentials');
      });

      it('should use statusText when body is null', () => {
        const response = mockResponse(401, 'Unauthorized');
        const error = fromResponse(response, null);

        expect(error).toBeInstanceOf(AuthenticationError);
        expect(error.message).toBe('Unauthorized');
      });
    });

    describe('403 Forbidden', () => {
      it('should return AuthorizationError for status 403', () => {
        const response = mockResponse(403, 'Forbidden');
        const body = { message: 'Access denied' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(AuthorizationError);
        expect(error.message).toBe('Access denied');
      });
    });

    describe('404 Not Found', () => {
      it('should return NotFoundError for status 404', () => {
        const response = mockResponse(404, 'Not Found');
        const body = { message: 'Resource not found' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(NotFoundError);
        expect(error.message).toBe('Resource not found');
      });
    });

    describe('409 Conflict', () => {
      it('should return ConflictError for status 409', () => {
        const response = mockResponse(409, 'Conflict');
        const body = { message: 'Version conflict' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(ConflictError);
        expect(error.message).toBe('Version conflict');
      });
    });

    describe('423 Locked (Sealed)', () => {
      it('should return SealedError for status 423', () => {
        const response = mockResponse(423, 'Locked');
        const body = { message: 'Database is sealed' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(SealedError);
        expect(error.message).toBe('Database is sealed');
      });
    });

    describe('429 Rate Limit', () => {
      it('should return RateLimitError for status 429', () => {
        const response = mockResponse(429, 'Too Many Requests');
        response.headers.get = jest.fn().mockReturnValue('60');
        const body = { message: 'Rate limit exceeded' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(RateLimitError);
        expect(error.message).toBe('Rate limit exceeded');
        expect(error.retryAfter).toBe('60');
      });

      it('should handle missing Retry-After header', () => {
        const response = mockResponse(429);
        const error = fromResponse(response, { message: 'Throttled' });

        expect(error).toBeInstanceOf(RateLimitError);
        expect(error.retryAfter).toBe(null);
      });
    });

    describe('408 Request Timeout', () => {
      it('should return TimeoutError for status 408', () => {
        const response = mockResponse(408, 'Request Timeout');
        const body = { message: 'Request timed out' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(TimeoutError);
        expect(error.message).toBe('Request timed out');
      });
    });

    describe('504 Gateway Timeout', () => {
      it('should return TimeoutError for status 504', () => {
        const response = mockResponse(504, 'Gateway Timeout');
        const body = { message: 'Gateway timed out' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(TimeoutError);
        expect(error.message).toBe('Gateway timed out');
      });
    });

    describe('5xx Server Errors', () => {
      it('should return ServerError for status 500', () => {
        const response = mockResponse(500, 'Internal Server Error');
        const body = { message: 'Internal error' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(ServerError);
        expect(error.message).toBe('Internal error');
        expect(error.statusCode).toBe(500);
      });

      it('should return ServerError for status 502', () => {
        const response = mockResponse(502, 'Bad Gateway');
        const error = fromResponse(response, null);

        expect(error).toBeInstanceOf(ServerError);
        expect(error.statusCode).toBe(502);
      });

      it('should return ServerError for status 503', () => {
        const response = mockResponse(503, 'Service Unavailable');
        const error = fromResponse(response, { error: 'Service down' });

        expect(error).toBeInstanceOf(ServerError);
        expect(error.message).toBe('Service down');
        expect(error.statusCode).toBe(503);
      });
    });

    describe('Unknown status codes', () => {
      it('should return QuicBaaSError for unknown 4xx status', () => {
        const response = mockResponse(418, "I'm a teapot");
        const body = { message: 'Teapot error' };
        const error = fromResponse(response, body);

        expect(error).toBeInstanceOf(QuicBaaSError);
        expect(error.code).toBe('UNKNOWN_ERROR');
        expect(error.details.statusCode).toBe(418);
      });

      it('should return QuicBaaSError for status 422', () => {
        const response = mockResponse(422, 'Unprocessable Entity');
        const error = fromResponse(response, null);

        expect(error).toBeInstanceOf(QuicBaaSError);
        expect(error.code).toBe('UNKNOWN_ERROR');
        expect(error.details.statusCode).toBe(422);
      });
    });

    describe('edge cases', () => {
      it('should handle null body gracefully', () => {
        const response = mockResponse(400, 'Bad Request');
        const error = fromResponse(response, null);

        expect(error).toBeInstanceOf(ValidationError);
        expect(error.message).toBe('Bad Request');
      });

      it('should handle empty body object', () => {
        const response = mockResponse(404, 'Not Found');
        const error = fromResponse(response, {});

        expect(error).toBeInstanceOf(NotFoundError);
        expect(error.message).toBe('Not Found');
      });

      it('should handle body without message or error', () => {
        const response = mockResponse(500, 'Server Error');
        const body = { data: 'some data' };
        const error = fromResponse(response, body);

        expect(error.message).toBe('Server Error');
      });

      it('should prefer body.message over body.error', () => {
        const response = mockResponse(400);
        const body = { message: 'Message text', error: 'Error text' };
        const error = fromResponse(response, body);

        expect(error.message).toBe('Message text');
      });

      it('should handle undefined body', () => {
        const response = mockResponse(401, 'Unauthorized');
        const error = fromResponse(response);

        expect(error).toBeInstanceOf(AuthenticationError);
        expect(error.message).toBe('Unauthorized');
      });
    });
  });
});
