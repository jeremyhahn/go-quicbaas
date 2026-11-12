// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for the batch object routes of RestObjectsClient:
// POST /api/v1/objects/batch, /batch/get and /batch/delete.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';
import type {
  ObjectsBatchPutResponse,
  ObjectsBatchGetResponse,
  ObjectsBatchDeleteResponse,
} from '../src/objects_batch';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function lastCall(): { url: string; init: RequestInit; body: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit];
  return { url, init, body: JSON.parse(init.body as string) };
}

describe('RestObjectsClient batch routes', () => {
  it('batchPut posts the objects and returns the reply', async () => {
    const reply: ObjectsBatchPutResponse = {
      results: [{ key: 'a', object_id: 7, success: true, version: 1, size: 5 }],
      success: 1,
      failed: 0,
      timestamp: 1700000000,
    };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, reply));
    const client = new RestClient('http://localhost/');
    const ops = [{ key: 'a', value: 'hello', content_type: 'text/plain', tags: { t: 'v' } }];
    const got = await client.objects.batchPut(ops);
    expect(got).toEqual(reply);
    const { url, init, body } = lastCall();
    expect(url).toBe('http://localhost/api/v1/objects/batch');
    expect(init.method).toBe('POST');
    expect(body).toEqual({ objects: ops });
  });

  it('batchGet posts the keys without include_metadata when not given', async () => {
    const reply: ObjectsBatchGetResponse = {
      results: [{ key: 'a', value: 'hello', success: true }, { key: 'b', success: false, error: 'not found' }],
      success: 1,
      failed: 1,
      timestamp: 1700000001,
    };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, reply));
    const client = new RestClient('http://localhost');
    const got = await client.objects.batchGet(['a', 'b']);
    expect(got).toEqual(reply);
    const { url, init, body } = lastCall();
    expect(url).toBe('http://localhost/api/v1/objects/batch/get');
    expect(init.method).toBe('POST');
    expect(body).toEqual({ keys: ['a', 'b'] });
    expect(body).not.toHaveProperty('include_metadata');
  });

  it('batchGet sends include_metadata when given', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { results: [], success: 0, failed: 0, timestamp: 1 }));
    const client = new RestClient('http://localhost');
    await client.objects.batchGet(['a'], { includeMetadata: true });
    expect(lastCall().body).toEqual({ keys: ['a'], include_metadata: true });
  });

  it('batchDelete posts the keys and returns the reply', async () => {
    const reply: ObjectsBatchDeleteResponse = {
      results: [{ key: 'a', success: true, size: 5 }],
      success: 1,
      failed: 0,
      timestamp: 1700000002,
    };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, reply));
    const client = new RestClient('http://localhost');
    const got = await client.objects.batchDelete(['a']);
    expect(got).toEqual(reply);
    const { url, init, body } = lastCall();
    expect(url).toBe('http://localhost/api/v1/objects/batch/delete');
    expect(init.method).toBe('POST');
    expect(body).toEqual({ keys: ['a'] });
  });

  it('rejects with an APIError on 403', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(403, { error: 'forbidden' }));
    const client = new RestClient('http://localhost');
    const err = await client.objects.batchDelete(['a']).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(403);
    expect((err as APIError).message).toContain('forbidden');
  });
});
