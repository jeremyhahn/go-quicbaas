/**
 * connection.test.ts
 *
 * Verifies the persisted connection override in stores/connection.svelte.ts:
 * URL validation, the save / read / clear round trip, rejection of corrupt or
 * foreign stored values, the error when storage is blocked, the reachability
 * probe, and what
 * activeConnection() in stores/sdkClient.ts reports with and without one.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  isValidBaseURL,
  normalizeBaseURL,
  connectionOverride,
  saveConnectionOverride,
  clearConnectionOverride,
  probeDaemon,
} from '../src/stores/connection.svelte.ts';
import { activeConnection } from '../src/stores/sdkClient.ts';

const KEY = 'quicbaas.connection.v1';

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('probeDaemon', () => {
  it('resolves true on any HTTP answer', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 200 })));
    expect(await probeDaemon('https://h:8443')).toBe(true);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 503 })));
    expect(await probeDaemon('https://h:8443')).toBe(true);
  });

  it('calls the readiness endpoint without credentials or a bearer', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('', { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    await probeDaemon('https://h:8443');
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('https://h:8443/api/v1/health/ready');
    expect(init.credentials).toBe('omit');
    expect(new Headers(init.headers).has('Authorization')).toBe(false);
  });

  it('resolves false when fetch rejects', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    expect(await probeDaemon('https://h:8443')).toBe(false);
  });

  it('resolves false on timeout', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(
        (_url: string, init: RequestInit) =>
          new Promise((_resolve, reject) => {
            init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
          }),
      ),
    );
    expect(await probeDaemon('https://h:8443', 10)).toBe(false);
  });
});

describe('isValidBaseURL', () => {
  it('accepts http and https origins', () => {
    expect(isValidBaseURL('https://h:8443')).toBe(true);
    expect(isValidBaseURL('http://h')).toBe(true);
    expect(isValidBaseURL('https://h:8443/')).toBe(true);
  });

  it('rejects other schemes, relative values, paths, queries and empty input', () => {
    expect(isValidBaseURL('ftp://h')).toBe(false);
    expect(isValidBaseURL('h:8443')).toBe(false);
    expect(isValidBaseURL('https://h/x')).toBe(false);
    expect(isValidBaseURL('https://h?q')).toBe(false);
    expect(isValidBaseURL('https://h#frag')).toBe(false);
    expect(isValidBaseURL('')).toBe(false);
  });
});

describe('normalizeBaseURL', () => {
  it('returns the origin', () => {
    expect(normalizeBaseURL('https://h:8443/')).toBe('https://h:8443');
  });
});

describe('connection override storage', () => {
  it('round-trips a saved override', () => {
    saveConnectionOverride({ transport: 'rest', baseURL: 'https://h:8443' });
    expect(connectionOverride()).toEqual({ transport: 'rest', baseURL: 'https://h:8443' });
  });

  it('returns null when nothing is stored', () => {
    expect(connectionOverride()).toBeNull();
  });

  it('returns null for corrupt JSON', () => {
    window.localStorage.setItem(KEY, '{not json');
    expect(connectionOverride()).toBeNull();
  });

  it('returns null for a transport other than rest', () => {
    window.localStorage.setItem(KEY, JSON.stringify({ transport: 'quic', baseURL: 'https://h' }));
    expect(connectionOverride()).toBeNull();
  });

  it('returns null for an invalid base URL', () => {
    window.localStorage.setItem(KEY, JSON.stringify({ transport: 'rest', baseURL: 'ftp://h' }));
    expect(connectionOverride()).toBeNull();
  });

  it('clear removes the key', () => {
    saveConnectionOverride({ transport: 'rest', baseURL: 'https://h' });
    clearConnectionOverride();
    expect(window.localStorage.getItem(KEY)).toBeNull();
    expect(connectionOverride()).toBeNull();
  });

  it('save throws when storage is unavailable', () => {
    vi.spyOn(window.localStorage, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    expect(() => saveConnectionOverride({ transport: 'rest', baseURL: 'https://h' })).toThrow(
      'Browser storage is unavailable',
    );
  });
});

describe('activeConnection', () => {
  it('reports the override when one is saved', () => {
    saveConnectionOverride({ transport: 'rest', baseURL: 'https://h:8443' });
    expect(activeConnection()).toEqual({
      transport: 'rest',
      address: 'https://h:8443',
      overridden: true,
    });
  });

  it('reports the page origin without an override', () => {
    expect(activeConnection()).toEqual({
      transport: 'rest',
      address: window.location.origin,
      overridden: false,
    });
  });
});
