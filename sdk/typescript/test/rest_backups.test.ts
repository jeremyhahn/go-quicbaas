// RestBackupClient: client.backups over /api/v1/backups.
//
// Each test stubs globalThis.fetch and checks the HTTP method, the URL suffix
// and, where the method sends one, the JSON body.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';
import type { BackupInfo, CreateBackupRequest } from '../src/types.js';

const sample: BackupInfo[] = [
  { backup_id: 'b1', name: 'full', timestamp: 1700000000, size: 2048, status: 'completed', shards: [1, 2] },
];

function makeFetch(body: unknown, status = 200) {
  return vi.fn(async (_url: string, _init?: RequestInit) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    }),
  );
}

function call(fetchMock: ReturnType<typeof makeFetch>, i = 0): { url: string; init: RequestInit } {
  const [url, init] = fetchMock.mock.calls[i] as [string, RequestInit];
  return { url: String(url), init: init ?? {} };
}

describe('RestBackupClient', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  function client(fetchMock: ReturnType<typeof makeFetch>) {
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    return new RestClient('https://example').backups;
  }

  it('list() sends GET /api/v1/backups with no query', async () => {
    const f = makeFetch({ backups: sample, total_count: 1 });
    const resp = await client(f).list();
    const { url, init } = call(f);
    expect(url.endsWith('/api/v1/backups')).toBe(true);
    expect(init.method).toBe('GET');
    expect(resp.backups).toEqual(sample);
    expect(resp.total_count).toBe(1);
  });

  it('list({limit}) puts limit in the query and sends no offset', async () => {
    const f = makeFetch({ backups: sample, total_count: 1 });
    await client(f).list({ limit: 10 });
    const { url } = call(f);
    const q = new URL(url).searchParams;
    expect(new URL(url).pathname).toBe('/api/v1/backups');
    expect(q.get('limit')).toBe('10');
    expect(q.has('offset')).toBe(false);
  });

  it('list() turns backups: null into []', async () => {
    const f = makeFetch({ backups: null, total_count: 0 });
    const resp = await client(f).list();
    expect(resp.backups).toEqual([]);
  });

  it('create() POSTs the type as the server\'s name selector', async () => {
    const f = makeFetch({ backup_id: 'b2', timestamp: 1, size: 0, status: 'pending' }, 201);
    const c = client(f);
    const resp = await c.create({ type: 'incremental' });
    await c.create();
    const { url, init } = call(f);
    expect(url.endsWith('/api/v1/backups')).toBe(true);
    expect(init.method).toBe('POST');
    expect(JSON.parse(String(init.body))).toEqual({ name: 'incremental' });
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
    expect(resp.backup_id).toBe('b2');
    expect(JSON.parse(String(call(f, 1).init.body))).toEqual({ name: 'full' });
  });

  it('create() forwards no option the server ignores', async () => {
    const f = makeFetch({ backup_id: 'b3', timestamp: 1, size: 0, status: 'pending' }, 201);
    const req = { type: 'full', encryption: 'aes256', description: 'd' } as unknown as CreateBackupRequest;
    await client(f).create(req);
    expect(JSON.parse(String(call(f).init.body))).toEqual({ name: 'full' });
  });

  it('delete() sends DELETE /api/v1/backups/{id}', async () => {
    const f = makeFetch({ success: true });
    const resp = await client(f).delete('b1');
    const { url, init } = call(f);
    expect(url.endsWith('/api/v1/backups/b1')).toBe(true);
    expect(init.method).toBe('DELETE');
    expect(resp.success).toBe(true);
  });

  it('restore() POSTs target_shards, and {} without a request', async () => {
    const f = makeFetch({ job_id: 'j', status: 'running', progress: 0 });
    const c = client(f);
    await c.restore('b1', { target_shards: [3] });
    await c.restore('b1');
    const first = call(f, 0);
    expect(first.url.endsWith('/api/v1/backups/b1/restore')).toBe(true);
    expect(first.init.method).toBe('POST');
    expect(JSON.parse(String(first.init.body))).toEqual({ target_shards: [3] });
    expect(JSON.parse(String(call(f, 1).init.body))).toEqual({});
  });

  it('status() sends GET /api/v1/backups/{id}/status', async () => {
    const f = makeFetch({ backup_id: 'b1', valid: true, checksum_match: true });
    const resp = await client(f).status('b1');
    const { url, init } = call(f);
    expect(url.endsWith('/api/v1/backups/b1/status')).toBe(true);
    expect(init.method).toBe('GET');
    expect(resp.valid).toBe(true);
  });

  it('verify() POSTs /api/v1/backups/{id}/verify', async () => {
    const f = makeFetch({ valid: false, checksum_match: false, issues: ['bad'] });
    const resp = await client(f).verify('b1');
    const { url, init } = call(f);
    expect(url.endsWith('/api/v1/backups/b1/verify')).toBe(true);
    expect(init.method).toBe('POST');
    expect(resp.issues).toEqual(['bad']);
  });

  it('encodes an id with a slash', async () => {
    const f = makeFetch({ success: true });
    await client(f).delete('a/b');
    expect(call(f).url.endsWith('/api/v1/backups/a%2Fb')).toBe(true);
  });

  it('rejects on a 500', async () => {
    const f = makeFetch({ error: 'boom' }, 500);
    await expect(client(f).list()).rejects.toThrow();
  });
});
