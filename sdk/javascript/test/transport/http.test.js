/**
 * HttpTransport Tests
 */

import { HttpTransport } from '../../src/transport/http.js';
import {
  QuicBaaSError,
  ValidationError,
  NetworkError,
  TimeoutError,
  AuthenticationError,
  AuthorizationError,
  NotFoundError,
  ServerError,
} from '../../src/errors/index.js';

describe('HttpTransport', () => {
  let transport;

  beforeEach(() => {
    transport = new HttpTransport('https://api.example.com');
    global.fetch = jest.fn();
  });

  describe('constructor', () => {
    it('should create transport with valid baseUrl', () => {
      const t = new HttpTransport('https://api.example.com');
      expect(t).toBeInstanceOf(HttpTransport);
    });

    it('should remove trailing slash from baseUrl', () => {
      const t = new HttpTransport('https://api.example.com/');
      expect(t.baseUrl).toBe('https://api.example.com');
    });

    it('should throw error when baseUrl is missing', () => {
      expect(() => new HttpTransport()).toThrow(QuicBaaSError);
      expect(() => new HttpTransport('')).toThrow(QuicBaaSError);
    });

    it('should accept custom configuration', () => {
      const t = new HttpTransport('https://api.example.com', {
        timeout: 60000,
        retries: 5,
        headers: { 'X-Custom': 'value' },
      });
      expect(t.config.timeout).toBe(60000);
      expect(t.config.retries).toBe(5);
      expect(t.config.headers['X-Custom']).toBe('value');
    });
  });

  describe('auth token management', () => {
    it('should set auth token', () => {
      transport.setAuthToken('test-token');
      expect(transport.authToken).toBe('test-token');
    });

    it('should set auth token with refresh token', () => {
      transport.setAuthToken('test-token', 'refresh-token');
      expect(transport.authToken).toBe('test-token');
      expect(transport.refreshToken).toBe('refresh-token');
    });

    it('should clear auth token', () => {
      transport.setAuthToken('test-token', 'refresh-token');
      transport.clearAuthToken();
      expect(transport.authToken).toBeNull();
      expect(transport.refreshToken).toBeNull();
    });

    it('should set token refresh callback', () => {
      const callback = jest.fn();
      transport.setTokenRefreshCallback(callback);
      expect(transport.onTokenRefresh).toBe(callback);
    });
  });

  describe('interceptors', () => {
    it('should add request interceptor', () => {
      const interceptor = jest.fn(config => config);
      const index = transport.addRequestInterceptor(interceptor);
      expect(typeof index).toBe('number');
    });

    it('should add response interceptor', () => {
      const interceptor = jest.fn(response => response);
      const index = transport.addResponseInterceptor(interceptor);
      expect(typeof index).toBe('number');
    });

    it('should remove request interceptor', () => {
      const interceptor = jest.fn();
      const index = transport.addRequestInterceptor(interceptor);
      transport.removeRequestInterceptor(index);
      expect(transport.interceptors.request.length).toBe(0);
    });

    it('should remove response interceptor', () => {
      const interceptor = jest.fn();
      const index = transport.addResponseInterceptor(interceptor);
      transport.removeResponseInterceptor(index);
      expect(transport.interceptors.response.length).toBe(0);
    });

    it('should execute request interceptor and modify request config', async () => {
      const interceptor = jest.fn(async (config, url) => {
        return {
          ...config,
          headers: { ...config.headers, 'X-Custom-Header': 'intercepted' },
        };
      });
      transport.addRequestInterceptor(interceptor);

      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });

      await transport.get('/test');

      expect(interceptor).toHaveBeenCalled();
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            'X-Custom-Header': 'intercepted',
          }),
        })
      );
    });

    it('should execute response interceptor and modify response', async () => {
      const mockResponse = {
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"original": true}'),
      };

      const modifiedResponse = {
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"modified": true}'),
      };

      const interceptor = jest.fn(async (response) => {
        return modifiedResponse;
      });
      transport.addResponseInterceptor(interceptor);

      global.fetch.mockResolvedValueOnce(mockResponse);

      const result = await transport.get('/test');

      expect(interceptor).toHaveBeenCalled();
      expect(result).toEqual({ modified: true });
    });
  });

  describe('request', () => {
    it('should make GET request', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"data": "test"}'),
      });

      const result = await transport.get('/test');
      expect(result).toEqual({ data: 'test' });
      expect(global.fetch).toHaveBeenCalledWith(
        'https://api.example.com/test',
        expect.objectContaining({
          method: 'GET',
        })
      );
    });

    it('should make POST request with body', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"id": 1}'),
      });

      const result = await transport.post('/test', { name: 'test' });
      expect(result).toEqual({ id: 1 });
      expect(global.fetch).toHaveBeenCalledWith(
        'https://api.example.com/test',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ name: 'test' }),
        })
      );
    });

    it('should include auth token in headers', async () => {
      transport.setAuthToken('my-token');
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });

      await transport.get('/test');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer my-token',
          }),
        })
      );
    });

    it('should let an explicit per-request Authorization override the auth token', async () => {
      transport.setAuthToken('my-token');
      global.fetch.mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });

      await transport.post('/test', null, { headers: { Authorization: 'Bearer other-token' } });
      expect(global.fetch.mock.calls[0][1].headers.Authorization).toBe('Bearer other-token');

      // The override is per-request: the next call carries our own token again.
      await transport.get('/test');
      expect(global.fetch.mock.calls[1][1].headers.Authorization).toBe('Bearer my-token');
    });

    it('should build URL with query parameters', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });

      await transport.get('/test', { query: { limit: 10, offset: 5 } });
      expect(global.fetch).toHaveBeenCalledWith(
        'https://api.example.com/test?limit=10&offset=5',
        expect.any(Object)
      );
    });

    it('should handle array query parameters', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });

      await transport.get('/test', { query: { ids: ['a', 'b', 'c'] } });
      expect(global.fetch).toHaveBeenCalledWith(
        'https://api.example.com/test?ids=a&ids=b&ids=c',
        expect.any(Object)
      );
    });

    it('should throw NetworkError on fetch failure', async () => {
      // Mock fetch to reject for all retry attempts
      global.fetch.mockRejectedValue(new Error('Connection refused'));

      await expect(transport.get('/test')).rejects.toThrow(NetworkError);
    });

    it('should throw TimeoutError on abort', async () => {
      // Mock fetch to reject for all retry attempts (timeout should not retry)
      global.fetch.mockRejectedValue(Object.assign(new Error('Aborted'), { name: 'AbortError' }));

      await expect(transport.get('/test')).rejects.toThrow(TimeoutError);
    });

    it('should handle empty JSON response body', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve(''),
      });

      const result = await transport.get('/test');
      expect(result).toBeNull();
    });

    it('should handle invalid JSON response body gracefully', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('not valid json'),
      });

      const result = await transport.get('/test');
      expect(result).toBeNull();
    });

    it('should throw ValidationError on 400 response', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Invalid input"}'),
      });

      await expect(transport.get('/test')).rejects.toThrow(ValidationError);
    });

    it('should throw AuthenticationError on 401 response', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Invalid token"}'),
      });

      await expect(transport.get('/test')).rejects.toThrow(AuthenticationError);
    });

    it('should throw AuthorizationError on 403 response', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Access denied"}'),
      });

      await expect(transport.get('/test')).rejects.toThrow(AuthorizationError);
    });

    it('should throw NotFoundError on 404 response', async () => {
      // Use transport with no retries to avoid retry attempts
      const noRetryTransport = new HttpTransport('https://api.example.com', { retries: 0 });
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        statusText: 'Not Found',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Resource not found"}'),
      });

      await expect(noRetryTransport.get('/test')).rejects.toThrow(NotFoundError);
    });

    it('should throw ServerError on 500 response', async () => {
      // Use transport with no retries to avoid retry attempts
      const noRetryTransport = new HttpTransport('https://api.example.com', { retries: 0 });
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Server error"}'),
      });

      await expect(noRetryTransport.get('/test')).rejects.toThrow(ServerError);
    });

    it('should not retry on 4xx client errors (statusCode check)', async () => {
      const error400 = new ValidationError('Bad request');
      error400.statusCode = 400;

      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Bad request"}'),
      });

      await expect(transport.get('/test')).rejects.toThrow(ValidationError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should not retry on VALIDATION_ERROR code', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Validation failed"}'),
      });

      await expect(transport.get('/test')).rejects.toThrow(ValidationError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should not retry on AUTHENTICATION_ERROR code', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        statusText: 'Unauthorized',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Not authenticated"}'),
      });

      await expect(transport.get('/test')).rejects.toThrow(AuthenticationError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should not retry on AUTHORIZATION_ERROR code', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: false,
        status: 403,
        statusText: 'Forbidden',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Not authorized"}'),
      });

      await expect(transport.get('/test')).rejects.toThrow(AuthorizationError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should use custom timeout from options', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });

      await transport.get('/test', { timeout: 5000 });
      expect(global.fetch).toHaveBeenCalled();
    });

    it('should handle non-JSON content type', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'text/plain']]),
        text: () => Promise.resolve('plain text response'),
      });

      const result = await transport.get('/test');
      expect(result).toBeNull();
    });

    it('should handle response with null Content-Type header', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map(),
        text: () => Promise.resolve('no content type'),
      });

      const result = await transport.get('/test');
      expect(result).toBeNull();
    });

    it('should skip null and undefined query parameters', async () => {
      global.fetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });

      await transport.get('/test', { query: { valid: 'value', empty: null, missing: undefined } });
      expect(global.fetch).toHaveBeenCalledWith(
        'https://api.example.com/test?valid=value',
        expect.any(Object)
      );
    });

  });

  describe('_shouldNotRetry', () => {
    it('should return true for errors with 4xx statusCode', () => {
      // Create an error with a statusCode property in the 4xx range
      const error = new QuicBaaSError('Client error', 'UNKNOWN_ERROR');
      error.statusCode = 404;

      expect(transport._shouldNotRetry(error)).toBe(true);
    });

    it('should return true for VALIDATION_ERROR code', () => {
      const error = new ValidationError('Invalid input');
      expect(transport._shouldNotRetry(error)).toBe(true);
    });

    it('should return true for AUTHENTICATION_ERROR code', () => {
      const error = new AuthenticationError('Not authenticated');
      expect(transport._shouldNotRetry(error)).toBe(true);
    });

    it('should return true for AUTHORIZATION_ERROR code', () => {
      const error = new AuthorizationError('Access denied');
      expect(transport._shouldNotRetry(error)).toBe(true);
    });

    it('should return false for 5xx server errors', () => {
      const error = new ServerError('Server error', 500);
      expect(transport._shouldNotRetry(error)).toBe(false);
    });

    it('should return false for network errors', () => {
      const error = new NetworkError('Connection refused');
      expect(transport._shouldNotRetry(error)).toBe(false);
    });

    it('should return false for errors without statusCode or special code', () => {
      const error = new QuicBaaSError('Unknown error', 'UNKNOWN');
      expect(transport._shouldNotRetry(error)).toBe(false);
    });
  });

  describe('retry logic', () => {
    beforeEach(() => {
      jest.useFakeTimers();
    });

    afterEach(() => {
      jest.useRealTimers();
    });

    it('should retry on server errors with exponential backoff', async () => {
      const serverError = {
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"message": "Server error"}'),
      };

      const successResponse = {
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"success": true}'),
      };

      global.fetch
        .mockResolvedValueOnce(serverError)
        .mockResolvedValueOnce(successResponse);

      const requestPromise = transport.get('/test');

      // First attempt fails, wait for retry delay
      await jest.advanceTimersByTimeAsync(1000);

      const result = await requestPromise;
      expect(result).toEqual({ success: true });
      expect(global.fetch).toHaveBeenCalledTimes(2);
    });

    it('should retry on network errors with delay', async () => {
      const networkError = new Error('Connection refused');
      const successResponse = {
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{"recovered": true}'),
      };

      global.fetch
        .mockRejectedValueOnce(networkError)
        .mockResolvedValueOnce(successResponse);

      const requestPromise = transport.get('/test');

      // Wait for retry delay
      await jest.advanceTimersByTimeAsync(1000);

      const result = await requestPromise;
      expect(result).toEqual({ recovered: true });
      expect(global.fetch).toHaveBeenCalledTimes(2);
    });

    it('should exhaust all retries before throwing', async () => {
      const retryTransport = new HttpTransport('https://api.example.com', {
        retries: 2,
        retryDelay: 100,
        retryBackoff: 2,
      });

      const networkError = new Error('Connection refused');
      // Mock each call individually
      global.fetch
        .mockRejectedValueOnce(networkError)
        .mockRejectedValueOnce(networkError)
        .mockRejectedValueOnce(networkError);

      let caughtError;
      const requestPromise = retryTransport.get('/test').catch(err => {
        caughtError = err;
      });

      // Run all pending timers and microtasks
      await jest.runAllTimersAsync();
      await requestPromise;

      expect(caughtError).toBeInstanceOf(NetworkError);
      expect(global.fetch).toHaveBeenCalledTimes(3); // Initial + 2 retries
    });
  });

  describe('convenience methods', () => {
    beforeEach(() => {
      global.fetch.mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Map([['Content-Type', 'application/json']]),
        text: () => Promise.resolve('{}'),
      });
    });

    it('should have get method', async () => {
      await transport.get('/test');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'GET' })
      );
    });

    it('should have post method', async () => {
      await transport.post('/test', { data: 'test' });
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'POST' })
      );
    });

    it('should have post method with null body', async () => {
      await transport.post('/test');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'POST' })
      );
    });

    it('should have put method', async () => {
      await transport.put('/test', { data: 'test' });
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'PUT' })
      );
    });

    it('should have put method with null body', async () => {
      await transport.put('/test');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'PUT' })
      );
    });

    it('should have patch method', async () => {
      await transport.patch('/test', { data: 'test' });
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'PATCH' })
      );
    });

    it('should have patch method with null body', async () => {
      await transport.patch('/test');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'PATCH' })
      );
    });

    it('should have delete method', async () => {
      await transport.delete('/test');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ method: 'DELETE' })
      );
    });

    it('should have delete method with options', async () => {
      await transport.delete('/test', { headers: { 'X-Custom': 'value' } });
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          method: 'DELETE',
          headers: expect.objectContaining({ 'X-Custom': 'value' }),
        })
      );
    });
  });
});
