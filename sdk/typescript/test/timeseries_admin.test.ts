// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for the time-series batch-write and retention-policy methods of
// RestTimeSeriesClient: URL (metric encoded), HTTP method, body, the
// returned reply, and rejection on 403.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

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

function lastCall(): { url: string; init: RequestInit } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit];
  return { url, init };
}

describe('RestTimeSeriesClient admin methods', () => {
  it('writeBatch POSTs the points and returns the reply', async () => {
    const reply = { success: true, points_written: 2, timestamp: '2026-11-12T00:00:00Z' };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, reply));
    const client = new RestClient('http://localhost');
    const points = [
      { metric: 'cpu', value: 1.5, timestamp: '2026-11-12T00:00:00Z', tags: { host: 'a' } },
      { metric: 'mem', value: 2, timestamp: '2026-11-12T00:00:01Z' },
    ];
    const got = await client.timeseries.writeBatch(points);
    expect(got).toEqual(reply);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/timeseries/write-batch');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ points });
  });

  it('getRetentionPolicy GETs the encoded metric and returns the reply', async () => {
    const reply = {
      metric: 'app/cpu',
      policy: { duration_seconds: 86400, resolution_seconds: 60, aggregation: 'avg' },
      timestamp: '2026-11-12T00:00:00Z',
    };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, reply));
    const client = new RestClient('http://localhost');
    const got = await client.timeseries.getRetentionPolicy('app/cpu');
    expect(got).toEqual(reply);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/timeseries/retention/app%2Fcpu');
    expect(init.method ?? 'GET').toBe('GET');
    expect(init.body).toBeUndefined();
  });

  it('getRetentionPolicy returns a null policy', async () => {
    const reply = { metric: 'cpu', policy: null, timestamp: '2026-11-12T00:00:00Z' };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, reply));
    const client = new RestClient('http://localhost');
    const got = await client.timeseries.getRetentionPolicy('cpu');
    expect(got.policy).toBeNull();
  });

  it('setRetentionPolicy PUTs the policy to the encoded metric and returns the reply', async () => {
    const reply = { success: true, timestamp: '2026-11-12T00:00:00Z' };
    fetchMock.mockResolvedValueOnce(jsonResponse(200, reply));
    const client = new RestClient('http://localhost');
    const policy = { duration_seconds: 3600 };
    const got = await client.timeseries.setRetentionPolicy('app/mem', policy);
    expect(got).toEqual(reply);
    const { url, init } = lastCall();
    expect(url).toBe('http://localhost/api/v1/timeseries/retention/app%2Fmem');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body as string)).toEqual({ policy });
  });

  it('rejects with an APIError on 403', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(403, { error: 'forbidden' }));
    const client = new RestClient('http://localhost');
    const err = await client.timeseries
      .setRetentionPolicy('cpu', { duration_seconds: 60 })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect((err as APIError).status).toBe(403);
  });
});
