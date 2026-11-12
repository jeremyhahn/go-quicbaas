// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Tests for RestObjectStorageAdminClient: every method's URL, HTTP method and
// body, name encoding, PascalCase replies returned as is, and error rejection.

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
  vi.restoreAllMocks();
});

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function reply(body: unknown, status = 200): void {
  fetchMock.mockResolvedValue(jsonResponse(status, body));
}

function call(): { url: string; method: string; body: unknown } {
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit | undefined];
  const raw = init?.body;
  return {
    url: String(url),
    method: init?.method ?? 'GET',
    body: typeof raw === 'string' ? JSON.parse(raw) : raw,
  };
}

const base = 'http://localhost';
const client = (): RestClient => new RestClient(base);

describe('RestObjectStorageAdminClient', () => {
  it('createBackend POSTs the snake_case body and returns the PascalCase reply', async () => {
    const wire = { Name: 'b1', CreatedAt: 1700000000000000000 };
    reply(wire, 201);
    const req = { name: 'b1', type: 's3', tier: 'hot', config: { bucket: 'x' }, enabled: true };
    const res = await client().objectStorageAdmin.createBackend(req);
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/object-backends`);
    expect(c.method).toBe('POST');
    expect(c.body).toEqual(req);
  });

  it('updateBackend PUTs to the encoded name', async () => {
    reply({ Name: 'a/b', UpdatedAt: 5 });
    const res = await client().objectStorageAdmin.updateBackend('a/b', { tier: 'cold' });
    expect(res).toEqual({ Name: 'a/b', UpdatedAt: 5 });
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/object-backends/a%2Fb`);
    expect(c.method).toBe('PUT');
    expect(c.body).toEqual({ tier: 'cold' });
  });

  it('deleteBackend DELETEs with the migrate body, {} by default', async () => {
    const wire = { Name: 'b1', Deleted: true, MigratedObjects: 3 };
    reply(wire);
    const res = await client().objectStorageAdmin.deleteBackend('a/b', { migrate_to: 'b2', force: true });
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/object-backends/a%2Fb`);
    expect(c.method).toBe('DELETE');
    expect(c.body).toEqual({ migrate_to: 'b2', force: true });

    fetchMock.mockReset();
    reply(wire);
    await client().objectStorageAdmin.deleteBackend('b1');
    expect(call().body).toEqual({});
  });

  it('setBackendStatus PUTs {enabled} to /status', async () => {
    const wire = { Name: 'b1', Enabled: false, UpdatedAt: 9 };
    reply(wire);
    const res = await client().objectStorageAdmin.setBackendStatus('a/b', false);
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/object-backends/a%2Fb/status`);
    expect(c.method).toBe('PUT');
    expect(c.body).toEqual({ enabled: false });
  });

  it('compactBackend POSTs to /compact', async () => {
    const wire = { Name: 'b1', OperationID: 'op1', StartedAt: 7 };
    reply(wire, 202);
    const res = await client().objectStorageAdmin.compactBackend('a/b', { aggressive: true });
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/object-backends/a%2Fb/compact`);
    expect(c.method).toBe('POST');
    expect(c.body).toEqual({ aggressive: true });
  });

  it('storageStats GETs with the optional breakdown flags', async () => {
    const wire = {
      TotalBytes: 10,
      TotalObjects: 2,
      TotalBackends: 1,
      HealthyBackends: 1,
      BackendStats: [
        {
          Name: 'b1',
          Type: 's3',
          Enabled: true,
          Default: true,
          Tier: 'hot',
          Region: 'r',
          ObjectCount: 2,
          TotalSize: 10,
          Health: { Healthy: true, Latency: 1, LastCheck: 2, Error: '' },
        },
      ],
      TenantStats: null,
      CalculatedAt: 3,
    };
    reply(wire);
    const res = await client().objectStorageAdmin.storageStats({ backends: true, tenants: true });
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/storage/stats?backends=true&tenants=true`);
    expect(c.method).toBe('GET');

    fetchMock.mockReset();
    reply(wire);
    await client().objectStorageAdmin.storageStats();
    expect(call().url).toBe(`${base}/api/v1/admin/storage/stats`);
  });

  it('runGarbageCollection POSTs the request, {} when none', async () => {
    const wire = { OperationID: 'gc1', StartedAt: 4 };
    reply(wire, 202);
    const res = await client().objectStorageAdmin.runGarbageCollection();
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/storage/gc`);
    expect(c.method).toBe('POST');
    expect(c.body).toEqual({});

    fetchMock.mockReset();
    reply(wire, 202);
    await client().objectStorageAdmin.runGarbageCollection({ backends: ['b1'], dry_run: true });
    expect(call().body).toEqual({ backends: ['b1'], dry_run: true });
  });

  it('garbageCollectionProgress GETs with the encoded operation_id', async () => {
    const wire = {
      OperationID: 'op/1',
      Status: 'running',
      ObjectsScanned: 5,
      ObjectsDeleted: 1,
      BytesReclaimed: 100,
      PercentComplete: 50,
      Error: '',
      CompletedAt: 0,
    };
    reply(wire);
    const res = await client().objectStorageAdmin.garbageCollectionProgress('op/1');
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/storage/gc/progress?operation_id=op%2F1`);
    expect(c.method).toBe('GET');
  });

  it('repairObject POSTs the repair body', async () => {
    const wire = { Key: 'k', Success: true, RepairedLocations: ['b2'], Error: '' };
    reply(wire);
    const req = { key: 'k', source_location: 'b1', timeout_ms: 1000 };
    const res = await client().objectStorageAdmin.repairObject(req);
    expect(res).toEqual(wire);
    const c = call();
    expect(c.url).toBe(`${base}/api/v1/admin/objects/repair`);
    expect(c.method).toBe('POST');
    expect(c.body).toEqual(req);
  });

  it('rejects on a 403', async () => {
    reply({ error: 'forbidden' }, 403);
    await expect(client().objectStorageAdmin.storageStats()).rejects.toThrow();
  });
});
