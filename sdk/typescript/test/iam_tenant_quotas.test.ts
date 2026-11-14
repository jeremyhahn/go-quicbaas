// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Wire-format tests for the tenant quota, entitlement and provisioning
// methods of RestIAMClient: each one must hit the /api/v1/tenants route the
// REST server mounts, with the HTTP method and body the Go handlers read.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';
import type { TenantQuotas, ProvisionTenantRequest } from '../src/index';

function makeJSON(body: unknown = {}, status = 200): Response {
  // fetchJSON reads a body even for 204 handlers, so the mock always has one.
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

const quotas: TenantQuotas = {
  max_databases: 10,
  max_raft_groups: 4,
  max_replicas_per_group: 3,
  max_total_replicas: 12,
  max_collections: 100,
  max_keys: 1000000,
  max_storage_bytes: 1073741824,
  max_database_size_bytes: 536870912,
  max_users: 50,
  max_roles: 20,
  max_certificates: 100,
  max_backups: 10,
  max_vector_dims: 1536,
  max_ts_metrics: 500,
  max_dedicated_wal_groups: 1,
  system_barrier_quota: 0,
};

describe('RestIAMClient.updateTenantQuotas', () => {
  it('PATCHes the quotas to the encoded tenant ID', async () => {
    fetchMock.mockResolvedValue(makeJSON());

    const client = new RestClient('http://localhost');
    await client.iam.updateTenantQuotas('a/b', quotas);

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/a%2Fb/quotas');
    expect(init?.method).toBe('PATCH');
    expect(JSON.parse(init?.body as string)).toEqual(quotas);
  });
});

describe('RestIAMClient.updateTenantEntitlements', () => {
  it('PATCHes the entitlements to the encoded tenant ID', async () => {
    fetchMock.mockResolvedValue(makeJSON());
    const entitlements = {
      allowed_isolation_modes: ['prefix'],
      allow_dedicated_raft: true,
      allow_volume_storage: false,
      allowed_barrier_modes: ['tenant'],
    };

    const client = new RestClient('http://localhost');
    await client.iam.updateTenantEntitlements('a/b', entitlements);

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/a%2Fb/entitlements');
    expect(init?.method).toBe('PATCH');
    expect(JSON.parse(init?.body as string)).toEqual(entitlements);
  });
});

describe('RestIAMClient.provisionTenant', () => {
  it('POSTs the whole request to the encoded tenant_id in cert mode', async () => {
    const resp = {
      tenant_id: 'a/b',
      admin_user_id: '18446744073709551615',
      admin_username: 'admin',
      admin_role_id: 'r1',
      admin_pending: false,
      admin_cert_pem: '-----BEGIN CERTIFICATE-----',
      admin_cert_thumbprint: 'abc',
      timestamp: '2026-11-12T00:00:00Z',
    };
    fetchMock.mockResolvedValue(makeJSON(resp, 201));
    const req: ProvisionTenantRequest = {
      tenant_id: 'a/b',
      name: 'Acme',
      isolation_type: 'prefix',
      admin_username: 'admin',
      admin_enrollment: 'cert',
      admin_csr_pem: '-----BEGIN CERTIFICATE REQUEST-----',
    };

    const client = new RestClient('http://localhost');
    const got = await client.iam.provisionTenant(req);

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/a%2Fb/provision');
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(got).toEqual(resp);
    expect(got.admin_user_id).toBe('18446744073709551615');
  });

  it('parses an invite-mode response with admin_pending true', async () => {
    fetchMock.mockResolvedValue(
      makeJSON(
        {
          tenant_id: 't1',
          admin_user_id: '7',
          admin_username: 'admin',
          admin_role_id: 'r1',
          admin_pending: true,
          invite_token: 'tok',
          invite_expires_at: '2026-11-13T00:00:00Z',
          timestamp: '2026-11-12T00:00:00Z',
        },
        201,
      ),
    );

    const client = new RestClient('http://localhost');
    const got = await client.iam.provisionTenant({
      tenant_id: 't1',
      admin_enrollment: 'invite',
      invite_expiry_hours: 24,
    });

    const [, init] = lastCall();
    expect(JSON.parse(init?.body as string)).toEqual({
      tenant_id: 't1',
      admin_enrollment: 'invite',
      invite_expiry_hours: 24,
    });
    expect(got.admin_pending).toBe(true);
    expect(got.invite_token).toBe('tok');
    expect(got.admin_cert_pem).toBeUndefined();
  });
});

describe('RestIAMClient tenant quota errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));

    const client = new RestClient('http://localhost');
    await expect(client.iam.updateTenantQuotas('t1', quotas)).rejects.toBeInstanceOf(APIError);
  });
});
