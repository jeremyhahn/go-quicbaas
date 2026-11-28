// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Wire-format tests for the GDPR consent methods of RestIAMClient. The Go
// routes are mounted under /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr
// (pkg/api/rest/iam_gdpr.go RegisterGDPRRoutes).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';
import type { ConsentPurpose, UserConsent } from '../src/interfaces';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const consent: UserConsent = {
  id: 'c-1',
  user_id: '18446744073709551615',
  tenant_id: 'acme',
  purpose: 'marketing',
  status: 'granted',
  version: 'v1',
  granted_at: '2026-11-12T00:00:00Z',
  created: '2026-11-12T00:00:00Z',
  updated: '2026-11-12T00:00:00Z',
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
});

// The tenant, user and purpose all carry characters that must be encoded,
// the purpose a '/', so a path segment can never split.
const tenant = 'my tenant';
const user = 'user/1';
const purpose = 'a/b' as ConsentPurpose;
const base = 'http://localhost/api/v1/tenants/my%20tenant/users/user%2F1/gdpr';

function lastCall(): [string, RequestInit | undefined] {
  return fetchMock.mock.calls[0] as [string, RequestInit | undefined];
}

// =============================================================================
// grantConsent
// =============================================================================

describe('RestIAMClient.grantConsent', () => {
  it('POSTs the request to …/gdpr/consent and returns the consent', async () => {
    fetchMock.mockResolvedValue(makeJSON(consent, 201));

    const client = new RestClient('http://localhost');
    const got = await client.iam.grantConsent(tenant, user, {
      purpose: 'marketing',
      version: 'v1',
      expires_at: '2027-01-01T00:00:00Z',
      ip_address: '192.0.2.1',
      user_agent: 'vitest',
    });

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/consent`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual({
      purpose: 'marketing',
      version: 'v1',
      expires_at: '2027-01-01T00:00:00Z',
      ip_address: '192.0.2.1',
      user_agent: 'vitest',
    });
    expect(got).toEqual(consent);
  });
});

// =============================================================================
// listUserConsents
// =============================================================================

describe('RestIAMClient.listUserConsents', () => {
  it('GETs …/gdpr/consents and returns the bare array', async () => {
    fetchMock.mockResolvedValue(makeJSON([consent]));

    const client = new RestClient('http://localhost');
    const got = await client.iam.listUserConsents(tenant, user);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/consents`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(got).toEqual([consent]);
  });

  it('returns [] when the body is null', async () => {
    fetchMock.mockResolvedValue(makeJSON(null));

    const client = new RestClient('http://localhost');
    await expect(client.iam.listUserConsents('acme', '1')).resolves.toEqual([]);
  });

  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeJSON({ error: 'forbidden' }, 403));

    const client = new RestClient('http://localhost');
    await expect(client.iam.listUserConsents('acme', '1')).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// getConsent
// =============================================================================

describe('RestIAMClient.getConsent', () => {
  it('GETs …/gdpr/consent/{purpose} with the purpose encoded', async () => {
    fetchMock.mockResolvedValue(makeJSON(consent));

    const client = new RestClient('http://localhost');
    const got = await client.iam.getConsent(tenant, user, purpose);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/consent/a%2Fb`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(got).toEqual(consent);
  });
});

// =============================================================================
// withdrawConsent
// =============================================================================

describe('RestIAMClient.withdrawConsent', () => {
  it('PUTs the request to …/gdpr/consent/{purpose}/withdraw', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...consent, status: 'withdrawn' }));

    const client = new RestClient('http://localhost');
    const got = await client.iam.withdrawConsent(tenant, user, purpose, {
      ip_address: '192.0.2.1',
      user_agent: 'vitest',
    });

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/consent/a%2Fb/withdraw`);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(init?.body as string)).toEqual({
      ip_address: '192.0.2.1',
      user_agent: 'vitest',
    });
    expect(got.status).toBe('withdrawn');
  });

  it('sends {} when no request is given', async () => {
    fetchMock.mockResolvedValue(makeJSON({ ...consent, status: 'withdrawn' }));

    const client = new RestClient('http://localhost');
    await client.iam.withdrawConsent('acme', '1', 'analytics');

    const [url, init] = lastCall();
    expect(url).toBe('http://localhost/api/v1/tenants/acme/users/1/gdpr/consent/analytics/withdraw');
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(init?.body as string)).toEqual({});
  });
});
