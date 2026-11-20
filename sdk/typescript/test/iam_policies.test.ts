// Tests for the RestIAMClient policy read, validate, test, simulate and
// effective-permissions methods: each maps to one route under
// /api/v1/tenants/{tenant_id}/policies, with every path segment encoded.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';
import type { IAMCheckRequest, IAMPolicyDocument } from '../src/interfaces';

function makeJSON(body: unknown = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
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

const base = 'http://localhost/api/v1/tenants/t%2F1/policies';

const check: IAMCheckRequest = {
  subject: { id: 'u1', tenant_id: 't/1', authenticated: true, roles: ['admin'] },
  action: 'read',
  resource: 'doc:1',
};

const doc: IAMPolicyDocument = {
  apiVersion: 'v1',
  kind: 'Policy',
  metadata: { name: 'p1', tenant: 't/1' },
  spec: { subjects: {}, rules: [] },
};

describe('RestIAMClient policy read', () => {
  it('getPolicy GETs the encoded policy name with no body', async () => {
    fetchMock.mockResolvedValue(makeJSON(doc));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.getPolicy('t/1', 'p/1');

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/p%2F1`);
    expect(init?.method ?? 'GET').toBe('GET');
    expect(init?.body).toBeUndefined();
    expect(resp.metadata.name).toBe('p1');
  });
});

describe('RestIAMClient policy validate', () => {
  const req = { name: 'p1', spec_yaml: 'rules: []' };

  it('validatePolicy POSTs the create body to validate', async () => {
    fetchMock.mockResolvedValue(makeJSON({ valid: true }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.validatePolicy('t/1', req);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/validate`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(resp.valid).toBe(true);
  });

  it('validatePolicy encodes the name query', async () => {
    fetchMock.mockResolvedValue(makeJSON({ valid: true }));
    const client = new RestClient('http://localhost');
    await client.iam.validatePolicy('t/1', req, 'a b/&c');

    const [url] = lastCall();
    expect(url).toBe(`${base}/validate?name=a%20b%2F%26c`);
  });

  it('validatePolicy resolves an invalid policy (200 valid=false)', async () => {
    fetchMock.mockResolvedValue(makeJSON({ valid: false, errors: ['bad rule'] }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.validatePolicy('t/1', req);

    expect(resp.valid).toBe(false);
    expect(resp.errors).toEqual(['bad rule']);
  });
});

describe('RestIAMClient policy evaluation', () => {
  it('testPolicy POSTs the check request to test', async () => {
    fetchMock.mockResolvedValue(makeJSON({ allowed: true, matched_policy: 'p1' }));
    const client = new RestClient('http://localhost');
    const resp = await client.iam.testPolicy('t/1', check);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/test`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(check);
    expect(resp.allowed).toBe(true);
    expect(resp.matched_policy).toBe('p1');
  });

  it('simulatePolicy POSTs policy and check_request to simulate', async () => {
    fetchMock.mockResolvedValue(
      makeJSON({
        allowed: false,
        evaluation_steps: [{ step_number: 1, policy_name: 'p1', action: 'read', result: false }],
      }),
    );
    const client = new RestClient('http://localhost');
    const req = { policy: doc, check_request: check, include_trace: true };
    const resp = await client.iam.simulatePolicy('t/1', req);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/simulate`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(resp.allowed).toBe(false);
    expect(resp.evaluation_steps?.[0].policy_name).toBe('p1');
  });

  it('getEffectivePermissions POSTs to effective-permissions', async () => {
    fetchMock.mockResolvedValue(
      makeJSON({
        subject: check.subject,
        permissions: { 'doc:*': { allowed: ['read'], denied: ['write'] } },
        total_policies_evaluated: 3,
      }),
    );
    const client = new RestClient('http://localhost');
    const req = {
      subject: check.subject,
      resource_patterns: ['doc:*'],
      actions: ['read', 'write'],
      include_deny_details: true,
    };
    const resp = await client.iam.getEffectivePermissions('t/1', req);

    const [url, init] = lastCall();
    expect(url).toBe(`${base}/effective-permissions`);
    expect(init?.method).toBe('POST');
    expect(JSON.parse(init?.body as string)).toEqual(req);
    expect(resp.permissions['doc:*'].allowed).toEqual(['read']);
    expect(resp.total_policies_evaluated).toBe(3);
  });
});

describe('RestIAMClient policy errors', () => {
  it('rejects with APIError on 403', async () => {
    fetchMock.mockResolvedValue(makeError(403, 'access denied'));
    const client = new RestClient('http://localhost');
    await expect(client.iam.testPolicy('t1', check)).rejects.toBeInstanceOf(APIError);
  });
});
