// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestKMSAdminClient (client.kmsAdmin): each method's URL, HTTP
// method and body against /api/v1/kms/keys/{name}/{metadata,policy,
// rotation-policy}.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type { KMSKeyPolicyInput } from '../src/kms_admin';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

interface Call {
  url: string;
  method: string;
  body: unknown;
}

function lastCall(): Call {
  const [input, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [
    string | URL | Request,
    RequestInit | undefined,
  ];
  const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
  const raw = init?.body;
  return {
    url,
    method: (init?.method ?? 'GET').toUpperCase(),
    body: typeof raw === 'string' ? JSON.parse(raw) : undefined,
  };
}

const NAME = 'team/key';
const ENC = 'team%2Fkey';
const BASE = 'http://localhost/api/v1/kms/keys/';

const metadata = {
  key_id: 'k1',
  name: 'team/key',
  algorithm: 'AES-256-GCM',
  status: 'enabled',
  version: 2,
  exportable: false,
  deletion_protection: true,
  created_at: 1700000000,
  updated_at: 1700000100,
  tags: { env: 'prod' },
};

const policyInput: KMSKeyPolicyInput = {
  version: 1,
  allowed_operations: ['encrypt', 'decrypt'],
  allowed_principals: ['alice'],
  denied_principals: [],
  expires_at: '2027-01-01T00:00:00Z',
  rotation_required: false,
  max_usage_count: null,
  require_mfa: true,
};

describe('RestKMSAdminClient', () => {
  it('getKeyMetadata GETs the encoded metadata path', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ metadata, timestamp: 1700000200 }));
    const client = new RestClient('http://localhost');
    const res = await client.kmsAdmin.getKeyMetadata(NAME);
    const call = lastCall();
    expect(call.url).toBe(`${BASE}${ENC}/metadata`);
    expect(call.method).toBe('GET');
    expect(call.body).toBeUndefined();
    expect(res.metadata.key_id).toBe('k1');
    expect(res.timestamp).toBe(1700000200);
  });

  it('updateKeyMetadata PUTs the request body', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ metadata, timestamp: 1 }));
    const client = new RestClient('http://localhost');
    const req = { description: 'd', tags: { env: 'prod' }, remove_tags: ['old'] };
    const res = await client.kmsAdmin.updateKeyMetadata(NAME, req);
    const call = lastCall();
    expect(call.url).toBe(`${BASE}${ENC}/metadata`);
    expect(call.method).toBe('PUT');
    expect(call.body).toEqual(req);
    expect(res.metadata.tags).toEqual({ env: 'prod' });
  });

  it('getKeyPolicy GETs the encoded policy path', async () => {
    const policy = { ...policyInput, key_id: 'k1', updated_at: 5 };
    fetchMock.mockResolvedValueOnce(jsonResponse({ policy, timestamp: 1 }));
    const client = new RestClient('http://localhost');
    const res = await client.kmsAdmin.getKeyPolicy(NAME);
    const call = lastCall();
    expect(call.url).toBe(`${BASE}${ENC}/policy`);
    expect(call.method).toBe('GET');
    expect(res.policy.allowed_operations).toEqual(['encrypt', 'decrypt']);
  });

  it('setKeyPolicy PUTs the policy wrapped as {policy}', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ key_id: 'k1', policy_version: 2, timestamp: 1 }));
    const client = new RestClient('http://localhost');
    const res = await client.kmsAdmin.setKeyPolicy(NAME, policyInput);
    const call = lastCall();
    expect(call.url).toBe(`${BASE}${ENC}/policy`);
    expect(call.method).toBe('PUT');
    expect(call.body).toEqual({ policy: policyInput });
    expect(res.policy_version).toBe(2);
  });

  it('getKeyRotationPolicy GETs the encoded rotation-policy path', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        key_id: 'k1',
        policy: { enabled: true, interval_seconds: 86400, next_rotation_unix: 1700086400, auto_rotate: true },
        timestamp: 1,
      }),
    );
    const client = new RestClient('http://localhost');
    const res = await client.kmsAdmin.getKeyRotationPolicy(NAME);
    const call = lastCall();
    expect(call.url).toBe(`${BASE}${ENC}/rotation-policy`);
    expect(call.method).toBe('GET');
    expect(res.policy?.next_rotation_unix).toBe(1700086400);
  });

  it('setKeyRotationPolicy PUTs the request body', async () => {
    const req = { enabled: true, interval_seconds: 3600, retain_versions: 3, auto_rotate: false };
    fetchMock.mockResolvedValueOnce(jsonResponse({ key_id: 'k1', policy: req, timestamp: 1 }));
    const client = new RestClient('http://localhost');
    const res = await client.kmsAdmin.setKeyRotationPolicy(NAME, req);
    const call = lastCall();
    expect(call.url).toBe(`${BASE}${ENC}/rotation-policy`);
    expect(call.method).toBe('PUT');
    expect(call.body).toEqual(req);
    expect(res.policy.retain_versions).toBe(3);
  });

  it('deleteKeyRotationPolicy DELETEs and reads the JSON body', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ key_id: 'k1', timestamp: 1700000300 }));
    const client = new RestClient('http://localhost');
    const res = await client.kmsAdmin.deleteKeyRotationPolicy(NAME);
    const call = lastCall();
    expect(call.url).toBe(`${BASE}${ENC}/rotation-policy`);
    expect(call.method).toBe('DELETE');
    expect(res).toEqual({ key_id: 'k1', timestamp: 1700000300 });
  });

  it('rejects on 403', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'forbidden' }, 403));
    const client = new RestClient('http://localhost');
    await expect(client.kmsAdmin.getKeyPolicy(NAME)).rejects.toThrow();
  });
});
