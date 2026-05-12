// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Regression test for RestDataClient.kvPut: the server's PUT /api/v1/kv/{type}/{key}
// endpoint expects a {"value": <JSON>} envelope. Sending the raw user-supplied
// JSON directly causes the server to interpret a top-level "value" field of
// the user's payload as the actual stored value, silently dropping every
// other field. This regression test pins the wire shape so that bug cannot
// recur.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';

function makeFetch(): typeof globalThis.fetch {
  return vi.fn(async () =>
    new Response('{}', {
      status: 200,
      headers: { 'content-type': 'application/json' },
    }),
  ) as unknown as typeof globalThis.fetch;
}

describe('RestDataClient.kvPut wire envelope', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('wraps a JSON object payload inside {"value": <object>}', async () => {
    const fetchSpy = makeFetch();
    globalThis.fetch = fetchSpy;
    const client = new RestClient('http://localhost:18080');
    await client.data.kvPut('default', 'test-key', { hello: 'world', n: 42 });

    const calls = (fetchSpy as unknown as { mock: { calls: unknown[][] } }).mock.calls;
    expect(calls).toHaveLength(1);
    const [, init] = calls[0] as [string, RequestInit];
    expect(init.method).toBe('PUT');
    const body = JSON.parse(init.body as string);
    expect(body).toEqual({ value: { hello: 'world', n: 42 } });
    expect(body.value).toEqual({ hello: 'world', n: 42 });
    // Critical regression guard: a top-level user-supplied "value" field
    // must NOT collapse into the envelope's "value" — they're nested.
    expect(body.value.hello).toBe('world');
  });

  it('wraps a primitive (number) payload inside {"value": <number>}', async () => {
    const fetchSpy = makeFetch();
    globalThis.fetch = fetchSpy;
    const client = new RestClient('http://localhost:18080');
    await client.data.kvPut('default', 'k', 42);

    const calls = (fetchSpy as unknown as { mock: { calls: unknown[][] } }).mock.calls;
    const [, init] = calls[0] as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({ value: 42 });
  });

  it('wraps a payload that already contains a "value" key correctly', async () => {
    // Critical: user payload {"value": "leaf"} must NOT be sent as the body
    // directly (which the server would unwrap into stored=leaf), but rather
    // wrapped: {"value": {"value": "leaf"}}.
    const fetchSpy = makeFetch();
    globalThis.fetch = fetchSpy;
    const client = new RestClient('http://localhost:18080');
    await client.data.kvPut('default', 'k', { value: 'leaf', other: 'kept' });

    const calls = (fetchSpy as unknown as { mock: { calls: unknown[][] } }).mock.calls;
    const [, init] = calls[0] as [string, RequestInit];
    const body = JSON.parse(init.body as string);
    expect(body.value).toEqual({ value: 'leaf', other: 'kept' });
    expect(body.value.other).toBe('kept');
  });

  it('targets PUT /api/v1/kv/{type}/{key} with URL-encoded path', async () => {
    const fetchSpy = makeFetch();
    globalThis.fetch = fetchSpy;
    const client = new RestClient('http://localhost:18080');
    await client.data.kvPut('my type', 'my/key', 'x');

    const calls = (fetchSpy as unknown as { mock: { calls: unknown[][] } }).mock.calls;
    const [url] = calls[0] as [string];
    expect(url).toContain('/api/v1/kv/my%20type/my%2Fkey');
  });
});
