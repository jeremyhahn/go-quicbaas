// Tests for the GDPR export and deletion methods of RestIAMClient.
//
// Each method maps to one route of RegisterGDPRRoutes (pkg/api/rest/iam_gdpr.go)
// under /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr. The tests pin the
// URL (every segment encoded), the HTTP method, the body and the decoded
// response.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';
import type { DataExportRequest, DeletionRequest, UserDataExport } from '../src/interfaces';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const exportReq: DataExportRequest = {
  id: 'e-1',
  user_id: '18446744073709551615',
  tenant_id: 'acme',
  requested_by: '18446744073709551615',
  format: 'json',
  status: 'pending',
  requested_at: '2026-11-12T10:00:00Z',
};

const deletionReq: DeletionRequest = {
  id: 'd-1',
  user_id: '18446744073709551615',
  tenant_id: 'acme',
  requested_by: '18446744073709551615',
  reason: 'leaving',
  status: 'pending',
  requested_at: '2026-11-12T10:00:00Z',
  grace_period_ends_at: '2026-12-12T10:00:00Z',
};

const exportData: UserDataExport = {
  export_metadata: {
    export_id: 'e-1',
    user_id: '18446744073709551615',
    tenant_id: 'acme',
    exported_at: '2026-11-12T10:05:00Z',
    format: 'json',
    categories: ['profile', 'consents'],
    schema_version: '1.0',
  },
  profile: {
    id: '18446744073709551615',
    email: 'a@example.com',
    name: 'A',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-02T00:00:00Z',
  },
  consents: [{ purpose: 'marketing', status: 'granted', version: '1' }],
};

function makeJSON(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
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
  vi.restoreAllMocks();
});

const tenant = 'my tenant';
const user = 'user/1';
const id = 'x/y';
const base = 'http://localhost/api/v1/tenants/my%20tenant/users/user%2F1/gdpr';

function lastCall(): [string, RequestInit | undefined] {
  return fetchMock.mock.calls[0] as [string, RequestInit | undefined];
}

// ---------------------------------------------------------------------------
// Exports
// ---------------------------------------------------------------------------

describe('RestIAMClient.requestDataExport', () => {
  it('POSTs the request to …/gdpr/export and returns the export', async () => {
    fetchMock.mockResolvedValue(makeJSON(exportReq, 202));

    const client = new RestClient('http://localhost');
    const got = await client.iam.requestDataExport(tenant, user, {
      format: 'csv',
      requested_by: '7',
      include_categories: ['profile'],
      exclude_categories: ['audit_logs'],
    });

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/export`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({
      format: 'csv',
      requested_by: '7',
      include_categories: ['profile'],
      exclude_categories: ['audit_logs'],
    });
    expect(got).toEqual(exportReq);
  });

  it('sends an empty object when no request is given', async () => {
    fetchMock.mockResolvedValue(makeJSON(exportReq, 202));

    const client = new RestClient('http://localhost');
    await client.iam.requestDataExport(tenant, user);

    const [, init] = lastCall();
    expect(JSON.parse(init?.body as string)).toEqual({});
  });
});

describe('RestIAMClient.getExportStatus', () => {
  it('GETs …/gdpr/export/{export_id} with the ID encoded', async () => {
    const done = { ...exportReq, status: 'completed' as const, completed_at: '2026-11-12T10:05:00Z' };
    fetchMock.mockResolvedValue(makeJSON(done));

    const client = new RestClient('http://localhost');
    const got = await client.iam.getExportStatus(tenant, user, id);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/export/x%2Fy`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(got).toEqual(done);
  });
});

describe('RestIAMClient.downloadExport', () => {
  it('GETs …/gdpr/export/{export_id}/download and returns the JSON document', async () => {
    fetchMock.mockResolvedValue(
      new Response(JSON.stringify(exportData), {
        status: 200,
        headers: {
          'Content-Type': 'application/json',
          'Content-Disposition': 'attachment; filename="user_data_export.json"',
        },
      }),
    );

    const client = new RestClient('http://localhost');
    const got = await client.iam.downloadExport(tenant, user, id);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/export/x%2Fy/download`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(got).toEqual(exportData);
  });

  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeJSON({ error: 'forbidden' }, 403));

    const client = new RestClient('http://localhost');
    const err = await client.iam.downloadExport('acme', '1', 'e-1').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(403);
  });
});

// ---------------------------------------------------------------------------
// Deletions
// ---------------------------------------------------------------------------

describe('RestIAMClient.requestDeletion', () => {
  it('POSTs the request to …/gdpr/deletion and returns the deletion', async () => {
    fetchMock.mockResolvedValue(makeJSON(deletionReq, 202));

    const client = new RestClient('http://localhost');
    const got = await client.iam.requestDeletion(tenant, user, {
      reason: 'leaving',
      delete_categories: ['profile'],
      preserve_categories: ['audit_logs'],
      grace_period_days: 30,
    });

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/deletion`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({
      reason: 'leaving',
      delete_categories: ['profile'],
      preserve_categories: ['audit_logs'],
      grace_period_days: 30,
    });
    expect(got).toEqual(deletionReq);
  });
});

describe('RestIAMClient.getDeletionStatus', () => {
  it('GETs …/gdpr/deletion/{deletion_id} with the ID encoded', async () => {
    fetchMock.mockResolvedValue(makeJSON(deletionReq));

    const client = new RestClient('http://localhost');
    const got = await client.iam.getDeletionStatus(tenant, user, id);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/deletion/x%2Fy`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(got).toEqual(deletionReq);
  });
});

describe('RestIAMClient.cancelDeletion', () => {
  it('DELETEs …/gdpr/deletion/{deletion_id} and returns the cancelled request', async () => {
    const cancelled = { ...deletionReq, status: 'cancelled' as const, cancelled_by: '18446744073709551615' };
    fetchMock.mockResolvedValue(makeJSON(cancelled));

    const client = new RestClient('http://localhost');
    const got = await client.iam.cancelDeletion(tenant, user, id);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/deletion/x%2Fy`);
    expect(init?.method).toBe('DELETE');
    expect(init?.body).toBeUndefined();
    expect(got).toEqual(cancelled);
  });

  it('sends cancelled_by as a query parameter', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...deletionReq, status: 'cancelled' }));

    const client = new RestClient('http://localhost');
    await client.iam.cancelDeletion(tenant, user, id, 'admin 1');

    const [url] = lastCall();
    expect(url).toBe(`${base}/deletion/x%2Fy?cancelled_by=admin+1`);
  });
});
