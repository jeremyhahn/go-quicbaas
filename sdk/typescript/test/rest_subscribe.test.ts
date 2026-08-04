// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestOrchestratorClient.subscribeSession: SSE parsing over fetch.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { SessionEventEnvelope, SubscribeSessionEventsRequest } from '../src/types.js';
import { RestClient } from '../src/rest.js';
import { APIError } from '../src/errors.js';

// ---------------------------------------------------------------------------
// mockOK returns a minimal fetch Response-alike that supports both .json()
// and .text() so tests remain compatible with fetchJSON implementations that
// use resp.text() (post BUG-2 fix) and those that use resp.json().
// ---------------------------------------------------------------------------
function mockOK(payload: unknown): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  const body = JSON.stringify(payload);
  return { ok: true, status: 200, json: async () => JSON.parse(body) as unknown, text: async () => body };
}

// ---------------------------------------------------------------------------
// Helper: craft an SSE ReadableStream from a string of raw SSE bytes.
// ---------------------------------------------------------------------------

function makeSseStream(raw: string): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  const bytes = encoder.encode(raw);
  return new ReadableStream<Uint8Array>({
    start(controller) {
      // Deliver all bytes in a single chunk to keep the test simple.
      controller.enqueue(bytes);
      controller.close();
    },
  });
}

// Consumes an AsyncIterable and returns all yielded items in an array.
async function collect<T>(iter: AsyncIterable<T>): Promise<T[]> {
  const results: T[] = [];
  for await (const item of iter) {
    results.push(item);
  }
  return results;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('RestOrchestratorClient.subscribeSession (SSE)', () => {
  const BASE = 'http://test-server';
  let client: RestClient;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    client = new RestClient(BASE);
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('parses a single SSE frame and yields one envelope', async () => {
    // The server sends the full SessionEventEnvelope as the data field of each
    // SSE frame. The SSE id: line carries the sequence number; the rest.ts
    // parser merges sequence from the id field into the parsed envelope.
    const envelope: SessionEventEnvelope = {
      session_id: 'sess-1',
      sequence: 1,
      payload_json: { kind: 'output', text: 'hello' },
      timestamp: '2025-04-16T00:00:00Z',
    };
    const sseRaw = [
      'id: 1',
      `data: ${JSON.stringify(envelope)}`,
      '',
      '',
    ].join('\n');

    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(sseRaw),
    });

    const req: SubscribeSessionEventsRequest = { session_id: 'sess-1', follow: false };
    const events = await collect(client.orchestrator.subscribeSession(req));

    expect(events).toHaveLength(1);
    expect(events[0].sequence).toBe(1);
    expect(events[0].session_id).toBe('sess-1');
    const pj = events[0].payload_json as Record<string, unknown>;
    expect(pj.kind).toBe('output');
    expect(pj.text).toBe('hello');
  });

  it('parses multiple SSE frames in sequence', async () => {
    const frames = [1, 2, 3].map((seq) => {
      const env: SessionEventEnvelope = {
        session_id: 'sess-2',
        sequence: seq,
        payload_json: { kind: 'tick', seq },
        timestamp: '2025-04-16T00:00:00Z',
      };
      return [`id: ${seq}`, `data: ${JSON.stringify(env)}`, '', ''].join('\n');
    }).join('');

    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(frames),
    });

    const req: SubscribeSessionEventsRequest = { session_id: 'sess-2', follow: false };
    const events = await collect(client.orchestrator.subscribeSession(req));

    expect(events).toHaveLength(3);
    expect(events.map((e) => e.sequence)).toEqual([1, 2, 3]);
  });

  it('builds the correct SSE URL with from and follow params', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(''),
    });

    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-abc',
      from_sequence: 42,
      follow: true,
    };
    await collect(client.orchestrator.subscribeSession(req));

    expect(fetchMock).toHaveBeenCalledOnce();
    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).toContain('/sessions/sess-abc/events');
    expect(calledUrl).toContain('from=42');
    expect(calledUrl).toContain('follow=1');
  });

  it('uses follow=0 when follow is false', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(''),
    });

    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-xyz',
      follow: false,
    };
    await collect(client.orchestrator.subscribeSession(req));

    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).toContain('follow=0');
  });

  it('sends Accept: text/event-stream header', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(''),
    });

    const req: SubscribeSessionEventsRequest = { session_id: 'sess-hdr', follow: false };
    await collect(client.orchestrator.subscribeSession(req));

    const init = fetchMock.mock.calls[0][1] as RequestInit;
    const headers = init.headers as Record<string, string>;
    expect(headers['Accept']).toBe('text/event-stream');
  });

  it('throws APIError on non-2xx response', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 404,
      statusText: 'Not Found',
      json: async () => ({ error: 'session not found' }),
    });

    const req: SubscribeSessionEventsRequest = { session_id: 'missing', follow: false };
    await expect(collect(client.orchestrator.subscribeSession(req))).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError with server error message when body has error field', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: async () => ({ error: 'database unavailable' }),
    });

    const req: SubscribeSessionEventsRequest = { session_id: 'err-sess', follow: false };
    await expect(collect(client.orchestrator.subscribeSession(req))).rejects.toThrow(
      'database unavailable',
    );
  });

  it('skips blank data frames (SSE keep-alive)', async () => {
    // An empty id/data sequence should not yield an event.
    const pingEnv: SessionEventEnvelope = {
      session_id: 'sess-ka',
      sequence: 1,
      payload_json: { kind: 'ping' },
      timestamp: '2025-04-16T00:00:00Z',
    };
    const sseRaw = `id: 0\n\nid: 1\ndata: ${JSON.stringify(pingEnv)}\n\n`;

    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(sseRaw),
    });

    const req: SubscribeSessionEventsRequest = { session_id: 'sess-ka', follow: false };
    const events = await collect(client.orchestrator.subscribeSession(req));

    // The first blank frame (no data) must not produce an event.
    expect(events).toHaveLength(1);
    expect(events[0].sequence).toBe(1);
  });

  it('includes tenant_id query param when provided in request', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(''),
    });

    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-t',
      tenant_id: 'acme',
      follow: false,
    };
    await collect(client.orchestrator.subscribeSession(req));

    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).toContain('tenant_id=acme');
  });

  it('propagates AbortSignal to fetch', async () => {
    const controller = new AbortController();
    fetchMock.mockResolvedValueOnce({
      ok: true,
      body: makeSseStream(''),
    });

    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-abort',
      follow: false,
      signal: controller.signal,
    };
    await collect(client.orchestrator.subscribeSession(req));

    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.signal).toBe(controller.signal);
  });
});

describe('RestOrchestratorClient.getSessionHistory', () => {
  const BASE = 'http://test-server';
  let client: RestClient;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    client = new RestClient(BASE);
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  const makeEnvelope = (seq: number): SessionEventEnvelope => ({
    session_id: 'sess-hist',
    sequence: seq,
    payload_json: { kind: 'output', text: `line ${seq}` },
    timestamp: new Date().toISOString(),
    from_replay: true,
  });

  it('returns events and next_seq from the REST response', async () => {
    const responseBody = {
      events: [makeEnvelope(1), makeEnvelope(2)],
      next_seq: 3,
    };
    fetchMock.mockResolvedValueOnce(mockOK(responseBody));

    const resp = await client.orchestrator.getSessionHistory({
      session_id: 'sess-hist',
      from_sequence: 1,
      limit: 2,
    });

    expect(resp.events).toHaveLength(2);
    expect(resp.next_seq).toBe(3);
  });

  it('builds correct query params: from, follow=0, limit, tenant_id', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ events: [], next_seq: 0 }));

    await client.orchestrator.getSessionHistory({
      session_id: 'sess-q',
      from_sequence: 10,
      limit: 5,
      tenant_id: 'corp',
    });

    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).toContain('from=10');
    expect(calledUrl).toContain('follow=0');
    expect(calledUrl).toContain('limit=5');
    expect(calledUrl).toContain('tenant_id=corp');
  });

  it('uses from=0 when from_sequence is omitted', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ events: [], next_seq: 0 }));

    await client.orchestrator.getSessionHistory({ session_id: 'sess-z' });

    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).toContain('from=0');
  });

  it('omits limit param when limit is 0 or omitted', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ events: [], next_seq: 0 }));

    await client.orchestrator.getSessionHistory({ session_id: 'sess-nolimit', limit: 0 });

    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).not.toContain('limit=');
  });

  it('throws APIError on non-2xx response', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 403,
      statusText: 'Forbidden',
      json: async () => ({ error: 'not authorised' }),
    });

    await expect(
      client.orchestrator.getSessionHistory({ session_id: 'forbidden' }),
    ).rejects.toBeInstanceOf(APIError);
  });
});

describe('RestOrchestratorClient.spawnSession', () => {
  const BASE = 'http://test-server';
  let client: RestClient;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    client = new RestClient(BASE);
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('POSTs to /sessions with the request body', async () => {
    fetchMock.mockResolvedValueOnce(
      mockOK({ session_id: 'new-sess', backend: 'claude', container_id: '' }),
    );

    const resp = await client.orchestrator.spawnSession({
      tenantID: 'acme',
      backend: 'claude',
      model: 'claude-opus-4-5',
      sandbox_mode: 1,
      host_affinity: 'worker-01',
    });

    expect(resp.session_id).toBe('new-sess');
    expect(resp.backend).toBe('claude');

    const url = fetchMock.mock.calls[0][0] as string;
    expect(new URL(url).searchParams.get('tenant_id')).toBe('acme');

    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body.tenantID).toBeUndefined();
    expect(body.sandbox_mode).toBe(1);
    expect(body.host_affinity).toBe('worker-01');
  });

  it('throws APIError on non-2xx spawn response', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
      json: async () => ({ error: 'backend unknown' }),
    });

    await expect(
      client.orchestrator.spawnSession({ tenantID: 'acme', backend: 'unknown' }),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws synchronously when tenantID missing (Bug #3 regression)', async () => {
    await expect(
      client.orchestrator.spawnSession({ backend: 'claude' } as never),
    ).rejects.toThrow('spawnSession: tenantID is required');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
