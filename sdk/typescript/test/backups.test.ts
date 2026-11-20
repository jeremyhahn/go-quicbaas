// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestBackupsClient: each method's URL, HTTP method and body
// against pkg/api/rest/handlers_backup.go.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function lastCall(): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit | undefined];
  return {
    url: String(url),
    method: (init?.method ?? 'GET').toUpperCase(),
    body: init?.body === undefined || init?.body === null ? undefined : JSON.parse(String(init.body)),
  };
}

describe('RestBackupsClient', () => {
  it('create POSTs the request to /api/v1/backups', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ backup_id: 'b1', timestamp: 1700000000, size: 0, status: 'pending' }, 201));
    const client = new RestClient('http://localhost');
    const req = { name: 'nightly/full', compression: 'zstd', include_shards: [1, 2] };
    const res = await client.backups.create(req);
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/backups');
    expect(call.method).toBe('POST');
    expect(call.body).toEqual(req);
    expect(res.backup_id).toBe('b1');
    expect(res.timestamp).toBe(1700000000);
  });

  it('create with no request sends {}', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ backup_id: 'b1', timestamp: 1, size: 0, status: 'pending' }, 201));
    const client = new RestClient('http://localhost');
    await client.backups.create();
    expect(lastCall().body).toEqual({});
  });

  it('list sends no query when no options are given', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ backups: [], total_count: 0 }));
    const client = new RestClient('http://localhost');
    const res = await client.backups.list();
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/backups');
    expect(call.method).toBe('GET');
    expect(call.body).toBeUndefined();
    expect(res.backups).toEqual([]);
  });

  it('list sends only the options given', async () => {
    fetchMock.mockImplementation(async () => jsonResponse({ backups: [], total_count: 0 }));
    const client = new RestClient('http://localhost');
    await client.backups.list({ limit: 5 });
    expect(lastCall().url).toBe('http://localhost/api/v1/backups?limit=5');
    await client.backups.list({ offset: 10 });
    expect(lastCall().url).toBe('http://localhost/api/v1/backups?offset=10');
    await client.backups.list({ limit: 5, offset: 0 });
    expect(lastCall().url).toBe('http://localhost/api/v1/backups?limit=5&offset=0');
  });

  it('list returns the backups', async () => {
    const info = { backup_id: 'b1', timestamp: 1, size: 42, status: 'completed', shards: [1] };
    fetchMock.mockResolvedValue(jsonResponse({ backups: [info], total_count: 1 }));
    const client = new RestClient('http://localhost');
    const res = await client.backups.list();
    expect(res.backups).toEqual([info]);
    expect(res.total_count).toBe(1);
  });

  it('restore POSTs target_shards and encodes the ID', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ job_id: 'j1', status: 'running', progress: 0 }));
    const client = new RestClient('http://localhost');
    const res = await client.backups.restore('a/b', { target_shards: [3] });
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/backups/a%2Fb/restore');
    expect(call.method).toBe('POST');
    expect(call.body).toEqual({ target_shards: [3] });
    expect(res.job_id).toBe('j1');
  });

  it('restore with no request sends {}', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ job_id: 'j1', status: 'running', progress: 0 }));
    const client = new RestClient('http://localhost');
    await client.backups.restore('b1');
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/backups/b1/restore');
    expect(call.body).toEqual({});
  });

  it('delete DELETEs the encoded ID and returns success', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ success: true }));
    const client = new RestClient('http://localhost');
    const res = await client.backups.delete('a/b');
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/backups/a%2Fb');
    expect(call.method).toBe('DELETE');
    expect(call.body).toBeUndefined();
    expect(res.success).toBe(true);
  });

  it('verify POSTs with no body to the encoded ID', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ valid: true, checksum_match: true }));
    const client = new RestClient('http://localhost');
    const res = await client.backups.verify('a/b');
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/backups/a%2Fb/verify');
    expect(call.method).toBe('POST');
    expect(call.body).toBeUndefined();
    expect(res.valid).toBe(true);
  });

  it('status GETs the encoded ID', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ backup_id: 'a/b', valid: false, checksum_match: false, issues: ['x'] }));
    const client = new RestClient('http://localhost');
    const res = await client.backups.status('a/b');
    const call = lastCall();
    expect(call.url).toBe('http://localhost/api/v1/backups/a%2Fb/status');
    expect(call.method).toBe('GET');
    expect(call.body).toBeUndefined();
    expect(res.issues).toEqual(['x']);
  });

  it('a 403 rejects', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: 'forbidden' }, 403));
    const client = new RestClient('http://localhost');
    await expect(client.backups.list()).rejects.toThrow();
  });
});
