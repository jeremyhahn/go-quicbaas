// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// The REST base parse keeps 64-bit identities exact (#555): an id the server
// issued past 2^53 must come back to the server unchanged, while a UnixNano
// timestamp in the same body stays a number.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

// A real content-hash RAG vector id: ~198x Number.MAX_SAFE_INTEGER.
const LARGE_ID = '1788567351228350294';
// A real Go time.UnixNano value; also past 2^53.
const UNIX_NANO = '1778017609149476600';

// rawResponse builds the body from text: JSON.stringify cannot write an
// integer past 2^53, and that integer is the point of these tests.
function rawResponse(text: string): Response {
  return new Response(text, { status: 200, headers: { 'Content-Type': 'application/json' } });
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

describe('REST base parse and 64-bit integers', () => {
  it('round-trips an id above 2^53 exactly', async () => {
    fetchMock
      .mockResolvedValueOnce(
        rawResponse(`{"id": ${LARGE_ID}, "namespace": "ns", "timestamp": ${UNIX_NANO}}`),
      )
      .mockResolvedValueOnce(new Response(null, { status: 204 }));

    const client = new RestClient('http://localhost');
    const doc = await client.rag.getDocument(LARGE_ID);
    expect(doc.id).toBe(LARGE_ID);

    await client.rag.deleteDocument(doc.id);
    const [url] = fetchMock.mock.calls[1];
    expect(url).toBe(`http://localhost/api/v1/rag/documents/${LARGE_ID}`);
  });

  it('keeps a UnixNano timestamp a number that names the right millisecond', async () => {
    fetchMock.mockResolvedValue(
      rawResponse(`{"id": ${LARGE_ID}, "namespace": "ns", "timestamp": ${UNIX_NANO}}`),
    );

    const client = new RestClient('http://localhost');
    const doc = await client.rag.getDocument(LARGE_ID);
    expect(typeof doc.timestamp).toBe('number');
    expect(Math.floor(doc.timestamp / 1_000_000)).toBe(1778017609149);
  });

  it('leaves an id at or below 2^53 a number, as before', async () => {
    fetchMock.mockResolvedValue(
      rawResponse('{"id": 9007199254740991, "namespace": "ns", "timestamp": 1000}'),
    );

    const client = new RestClient('http://localhost');
    const doc = await client.rag.getDocument(9007199254740991);
    expect(doc.id).toBe(9007199254740991);
  });

  it('keeps a placement lease id exact inside a list', async () => {
    fetchMock.mockResolvedValue(
      rawResponse(
        `{"nodes": [{"facts": {"node_id": "n1"}, "status": {"unschedulable": false, "draining": false},` +
          ` "lease_id": ${LARGE_ID}, "ttl_seconds": 30, "placements": 0}]}`,
      ),
    );

    const client = new RestClient('http://localhost');
    const nodes = await client.scheduler.listNodes();
    expect(nodes[0]?.lease_id).toBe(LARGE_ID);
    expect(nodes[0]?.ttl_seconds).toBe(30);
  });
});
