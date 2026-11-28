// Tests for the RestIAMClient audit methods: each maps to one route under
// /api/v1/iam/audit, with every path segment and query value encoded.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';
import type { IAMAuditEvent } from '../src/interfaces';

function makeJSON(body: unknown = {}, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeError(status: number, message = 'error'): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function lastCall(): [string, RequestInit | undefined] {
  return fetchMock.mock.calls[0] as [string, RequestInit | undefined];
}

const base = 'http://localhost/api/v1/iam/audit';

const event: IAMAuditEvent = {
  id: 'e/1',
  tenant_id: 't1',
  timestamp: '2026-11-12T10:00:00Z',
  event_type: 'user.login',
  category: 'authentication',
  actor: { user_id: 'u1', roles: ['admin'] },
  resource: { type: 'user', id: 'u1' },
  action: 'login',
  outcome: 'success',
};

describe('RestIAMClient audit list', () => {
  it('listAuditEvents GETs audit with no query when no option is set', async () => {
    fetchMock.mockResolvedValue(makeJSON({ events: [event], total: 1, limit: 50, offset: 0 }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.listAuditEvents();

    const [url, init] = lastCall();
    expect(url).toBe(base);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.total).toBe(1);
    expect(resp.events[0].id).toBe('e/1');
  });

  it('listAuditEvents sends only the set values, snake_case and encoded', async () => {
    fetchMock.mockResolvedValue(makeJSON({ events: [], total: 0, limit: 10, offset: 20 }));
    const client = new RestClient('http://localhost');
    await client.iam.listAuditEvents({
      tenantID: 't/1',
      eventType: 'user.login',
      actorID: 'u 1',
      startTime: '2026-11-01T00:00:00+01:00',
      limit: 10,
      offset: 20,
    });

    const [url] = lastCall();
    expect(url).toBe(
      `${base}?tenant_id=t%2F1&event_type=user.login&actor_id=u%201` +
        '&start_time=2026-11-01T00%3A00%3A00%2B01%3A00&limit=10&offset=20',
    );
  });

  it('listAuditEvents sends the remaining filters', async () => {
    fetchMock.mockResolvedValue(makeJSON({ events: [], total: 0, limit: 0, offset: 0 }));
    const client = new RestClient('http://localhost');
    await client.iam.listAuditEvents({
      category: 'authz',
      outcome: 'failure',
      resourceID: 'r/1',
      endTime: '2026-11-12T00:00:00Z',
    });

    const [url] = lastCall();
    expect(url).toBe(`${base}?category=authz&outcome=failure&resource_id=r%2F1&end_time=2026-11-12T00%3A00%3A00Z`);
  });
});

describe('RestIAMClient audit get', () => {
  it('getAuditEvent GETs the encoded event ID', async () => {
    fetchMock.mockResolvedValue(makeJSON(event));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.getAuditEvent('e/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/e%2F1`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.event_type).toBe('user.login');
  });

  it('getAuditEvent sends tenant_id when given', async () => {
    fetchMock.mockResolvedValue(makeJSON(event));
    const client = new RestClient('http://localhost');
    await client.iam.getAuditEvent('e/1', 't/1');

    const [url] = lastCall();
    expect(url).toBe(`${base}/e%2F1?tenant_id=t%2F1`);
  });
});

describe('RestIAMClient audit record', () => {
  it('recordAuditEvent POSTs { tenant_id, event, async } to audit', async () => {
    fetchMock.mockResolvedValue(makeJSON({ event_id: 'new-1' }, 201));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.recordAuditEvent({ tenant_id: 't1', event, async: false });

    const [url, init] = lastCall();
    expect(url).toBe(base);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({ tenant_id: 't1', event, async: false });
    expect(resp.event_id).toBe('new-1');
  });
});

describe('RestIAMClient audit prune', () => {
  it('deleteAuditEventsBefore DELETEs with only the encoded before', async () => {
    fetchMock.mockResolvedValue(makeJSON({ deleted_count: 3, before: '2026-10-01T00:00:00Z' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.deleteAuditEventsBefore('2026-10-01T00:00:00Z');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}?before=2026-10-01T00%3A00%3A00Z`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
    expect(resp.deleted_count).toBe(3);
  });

  it('deleteAuditEventsBefore adds tenant_id when given', async () => {
    fetchMock.mockResolvedValue(makeJSON({ deleted_count: 0, before: 'x' }));
    const client = new RestClient('http://localhost');
    await client.iam.deleteAuditEventsBefore('2026-10-01T00:00:00Z', 't/1');

    const [url] = lastCall();
    expect(url).toBe(`${base}?before=2026-10-01T00%3A00%3A00Z&tenant_id=t%2F1`);
  });
});

describe('RestIAMClient audit errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(client.iam.listAuditEvents({ tenantID: 'other' })).rejects.toBeInstanceOf(APIError);
  });
});
