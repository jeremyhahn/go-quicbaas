// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for REST and Wails getSessionHistory pagination.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { SessionEventEnvelope } from '../src/types.js';
import { RestClient } from '../src/rest.js';
import { WailsClient } from '../src/wails.js';
import { APIError } from '../src/errors.js';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeEnvelope(seq: number, sessionID = 'sess'): SessionEventEnvelope {
  return {
    session_id: sessionID,
    sequence: seq,
    payload_json: { kind: 'output', seq },
    timestamp: new Date().toISOString(),
    from_replay: true,
  };
}

// mockOK returns a minimal fetch Response-alike that supports both .json()
// and .text() so tests remain compatible with fetchJSON implementations that
// use resp.text() (post BUG-2 fix) and those that use resp.json().
function mockOK(payload: unknown): { ok: boolean; status: number; json: () => Promise<unknown>; text: () => Promise<string> } {
  const body = JSON.stringify(payload);
  return { ok: true, status: 200, json: async () => JSON.parse(body) as unknown, text: async () => body };
}

// ---------------------------------------------------------------------------
// REST pagination tests
// ---------------------------------------------------------------------------

describe('REST getSessionHistory pagination', () => {
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

  it('first page: from=0, limit=5 — returns events and next_seq', async () => {
    const page1 = {
      events: [makeEnvelope(1), makeEnvelope(2), makeEnvelope(3), makeEnvelope(4), makeEnvelope(5)],
      next_seq: 6,
    };
    fetchMock.mockResolvedValueOnce(mockOK(page1));

    const resp = await client.orchestrator.getSessionHistory({
      session_id: 'sess',
      from_sequence: 0,
      limit: 5,
    });

    expect(resp.events).toHaveLength(5);
    expect(resp.next_seq).toBe(6);
    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).toContain('from=0');
    expect(calledUrl).toContain('limit=5');
  });

  it('second page: from=6, limit=5 — returns remaining events and next_seq=0', async () => {
    const page2 = {
      events: [makeEnvelope(6), makeEnvelope(7)],
      next_seq: 0,
    };
    fetchMock.mockResolvedValueOnce(mockOK(page2));

    const resp = await client.orchestrator.getSessionHistory({
      session_id: 'sess',
      from_sequence: 6,
      limit: 5,
    });

    expect(resp.events).toHaveLength(2);
    expect(resp.next_seq).toBe(0);
    const calledUrl = fetchMock.mock.calls[0][0] as string;
    expect(calledUrl).toContain('from=6');
  });

  it('simulate full pagination loop across two pages', async () => {
    const page1 = {
      events: [makeEnvelope(1), makeEnvelope(2)],
      next_seq: 3,
    };
    const page2 = {
      events: [makeEnvelope(3)],
      next_seq: 0,
    };
    fetchMock
      .mockResolvedValueOnce(mockOK(page1))
      .mockResolvedValueOnce(mockOK(page2));

    // Emulate a scrollback pager.
    const allEvents: SessionEventEnvelope[] = [];
    let fromSeq = 0;
    while (true) {
      const page = await client.orchestrator.getSessionHistory({
        session_id: 'sess',
        from_sequence: fromSeq,
        limit: 2,
      });
      allEvents.push(...page.events);
      if (page.next_seq === 0) break;
      fromSeq = page.next_seq;
    }

    expect(allEvents).toHaveLength(3);
    expect(allEvents.map((e) => e.sequence)).toEqual([1, 2, 3]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('returns empty events array when session has no history', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ events: [], next_seq: 0 }));

    const resp = await client.orchestrator.getSessionHistory({ session_id: 'empty-sess' });

    expect(resp.events).toHaveLength(0);
    expect(resp.next_seq).toBe(0);
  });

  it('throws APIError on 404 (session not found)', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 404,
      statusText: 'Not Found',
      json: async () => ({ error: 'session not found' }),
    });

    await expect(
      client.orchestrator.getSessionHistory({ session_id: 'gone' }),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 500 with message from body', async () => {
    fetchMock.mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: async () => ({ error: 'storage unavailable' }),
    });

    await expect(
      client.orchestrator.getSessionHistory({ session_id: 'err-sess' }),
    ).rejects.toThrow('storage unavailable');
  });
});

// ---------------------------------------------------------------------------
// Wails IPC history tests
// ---------------------------------------------------------------------------

describe('Wails getSessionHistory', () => {
  let getHistoryMock: ReturnType<typeof vi.fn>;
  let client: WailsClient;

  beforeEach(() => {
    getHistoryMock = vi.fn().mockResolvedValue({ events: [], next_seq: 0 });

    Object.defineProperty(globalThis, 'window', {
      value: {
        go: {
          main: {
            ConnectionBinding: {
              Connect: vi.fn().mockResolvedValue(undefined),
              Disconnect: vi.fn().mockResolvedValue(undefined),
            },
            OrchestratorBinding: {},
            SessionBinding: {
              Subscribe: vi.fn().mockResolvedValue([]),
              GetHistory: getHistoryMock,
              Spawn: vi.fn(),
              Prompt: vi.fn(),
              Close: vi.fn(),
            },
          },
        },
        runtime: {
          EventsOn: vi.fn().mockReturnValue(vi.fn()),
          EventsOff: vi.fn(),
        },
      },
      writable: true,
      configurable: true,
    });

    client = new WailsClient();
  });

  afterEach(() => {
    Object.defineProperty(globalThis, 'window', {
      value: undefined,
      writable: true,
      configurable: true,
    });
    vi.clearAllMocks();
  });

  it('calls SessionBinding.GetHistory with the request object', async () => {
    const history = [makeEnvelope(1), makeEnvelope(2)];
    getHistoryMock.mockResolvedValueOnce({ events: history, next_seq: 0 });

    const resp = await client.orchestrator.getSessionHistory({
      session_id: 'wails-sess',
      from_sequence: 0,
      limit: 10,
    });

    expect(resp.events).toHaveLength(2);
    expect(getHistoryMock).toHaveBeenCalledWith({
      session_id: 'wails-sess',
      from_sequence: 0,
      limit: 10,
    });
  });

  it('returns next_seq > 0 on partial page', async () => {
    const history = [makeEnvelope(1), makeEnvelope(2)];
    getHistoryMock.mockResolvedValueOnce({ events: history, next_seq: 3 });

    const resp = await client.orchestrator.getSessionHistory({
      session_id: 'wails-partial',
      from_sequence: 1,
      limit: 2,
    });

    expect(resp.next_seq).toBe(3);
  });

  it('simulate full Wails pagination loop', async () => {
    getHistoryMock
      .mockResolvedValueOnce({ events: [makeEnvelope(1), makeEnvelope(2)], next_seq: 3 })
      .mockResolvedValueOnce({ events: [makeEnvelope(3)], next_seq: 0 });

    const allEvents: SessionEventEnvelope[] = [];
    let fromSeq = 0;
    while (true) {
      const page = await client.orchestrator.getSessionHistory({
        session_id: 'wails-full',
        from_sequence: fromSeq,
        limit: 2,
      });
      allEvents.push(...page.events);
      if (page.next_seq === 0) break;
      fromSeq = page.next_seq;
    }

    expect(allEvents).toHaveLength(3);
    expect(getHistoryMock).toHaveBeenCalledTimes(2);
  });

  it('propagates errors from the Go binding', async () => {
    getHistoryMock.mockRejectedValueOnce(new Error('binding error'));

    await expect(
      client.orchestrator.getSessionHistory({ session_id: 'err-wails' }),
    ).rejects.toThrow('binding error');
  });
});

// ---------------------------------------------------------------------------
// SandboxMode const validation
// ---------------------------------------------------------------------------

describe('SandboxMode constants', () => {
  it('exports Default=0, Required=1, None=2', async () => {
    const { SandboxMode } = await import('../src/types.js');
    expect(SandboxMode.Default).toBe(0);
    expect(SandboxMode.Required).toBe(1);
    expect(SandboxMode.None).toBe(2);
  });

  it('SandboxMode values are numeric and stable across module reloads', async () => {
    const a = await import('../src/types.js');
    const b = await import('../src/types.js');
    expect(a.SandboxMode.Required).toBe(b.SandboxMode.Required);
  });
});
