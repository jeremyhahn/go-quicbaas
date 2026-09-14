// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for reconcileRoles — the verb that converges a tenant's built-in roles
// onto the role definitions the running server build carries. Built-in roles
// are seeded once at tenant creation, so a permission rename in the server
// leaves every existing tenant behind until reconcile repairs it.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeResponse(body: unknown, status = 200): Response {
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

// =============================================================================
// reconcileRoles — wire contract + response normalization
// =============================================================================

describe('RestIAMClient.reconcileRoles', () => {
  it('POSTs to the tenant reconcile URL and returns the per-role diff', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      tenant_id: 'acme-corp',
      changed: true,
      roles: [
        { role_id: 'r1', name: 'admin', added: ['kv:write'], removed: ['kv:put'] },
        { role_id: 'r2', name: 'operator', created: true, added: ['kv:read'] },
      ],
    }));

    const client = new RestClient('http://localhost');
    const result = await client.iam.reconcileRoles('acme-corp');

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/tenants/acme-corp/roles/reconcile');
    expect(init.method).toBe('POST');

    expect(result.tenant_id).toBe('acme-corp');
    expect(result.changed).toBe(true);
    expect(result.roles).toHaveLength(2);
    expect(result.roles?.[0].added).toEqual(['kv:write']);
    expect(result.roles?.[0].removed).toEqual(['kv:put']);
    expect(result.roles?.[1].created).toBe(true);
  });

  it('normalizes the omitted roles array when nothing drifted', async () => {
    fetchMock.mockResolvedValue(makeResponse({ tenant_id: 'acme-corp', changed: false }));

    const client = new RestClient('http://localhost');
    const result = await client.iam.reconcileRoles('acme-corp');

    expect(result.roles).toEqual([]);
    expect(result.changed).toBe(false);
    expect(result.tenant_id).toBe('acme-corp');
  });

  it('URL-encodes the tenant ID', async () => {
    fetchMock.mockResolvedValue(makeResponse({ tenant_id: 'my tenant', changed: false }));

    const client = new RestClient('http://localhost');
    await client.iam.reconcileRoles('my tenant');

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('my%20tenant');
  });

  it('throws APIError on 404 (unknown tenant)', async () => {
    fetchMock.mockResolvedValue(makeError(404, 'tenant not found'));

    const client = new RestClient('http://localhost');
    await expect(client.iam.reconcileRoles('nope')).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 403 (caller lacks the reconcile permission)', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'permission denied'));

    const client = new RestClient('http://localhost');
    await expect(client.iam.reconcileRoles('acme-corp')).rejects.toBeInstanceOf(APIError);
  });
});
