/**
 * connection.test.ts
 *
 * Verifies the persisted connection override in stores/connection.svelte.ts:
 * URL validation, the save / read / clear round trip, rejection of corrupt or
 * foreign stored values, the error when storage is blocked, and what
 * activeConnection() in stores/sdkClient.ts reports with and without one.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  isValidBaseURL,
  normalizeBaseURL,
  connectionOverride,
  saveConnectionOverride,
  clearConnectionOverride,
} from '../src/stores/connection.svelte.ts';
import { activeConnection } from '../src/stores/sdkClient.ts';

const KEY = 'quicbaas.connection.v1';

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
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
