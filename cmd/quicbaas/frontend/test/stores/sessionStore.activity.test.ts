/**
 * stores/sessionStore.activity.test.ts
 *
 * Covers the additions that back the "new session" empty/loading states and
 * the four-state activity indicator:
 *
 *  - historyLoading is true only while the initial history fetch triggered
 *    by openSession is outstanding, and false once it resolves — even when
 *    the session turns out to have zero events (a brand-new chat).
 *  - turnStatus derives 'idle' / 'pending' / 'working' / 'disconnected' from
 *    the transcript and live-subscription health, never from a timer.
 *  - overlapping sequences delivered by history replay and the live tail are
 *    deduplicated rather than rendered twice.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { SessionEventEnvelope, SessionStatusResponse } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Controllable SDK mocks.
// ---------------------------------------------------------------------------

const { streamController, getSessionHistoryMock, subscribeSessionMock } = vi.hoisted(() => {
  let resolveNext: ((value: IteratorResult<SessionEventEnvelope>) => void) | null = null;
  const queue: SessionEventEnvelope[] = [];

  const push = (env: SessionEventEnvelope): void => {
    if (resolveNext) {
      const r = resolveNext;
      resolveNext = null;
      r({ done: false, value: env });
    } else {
      queue.push(env);
    }
  };

  const stream: AsyncIterable<SessionEventEnvelope> = {
    [Symbol.asyncIterator]() {
      return {
        next(): Promise<IteratorResult<SessionEventEnvelope>> {
          const queued = queue.shift();
          if (queued !== undefined) {
            return Promise.resolve({ done: false, value: queued });
          }
          return new Promise((resolve) => {
            resolveNext = resolve;
          });
        },
      };
    },
  };

  return {
    streamController: { stream, push },
    getSessionHistoryMock: vi.fn().mockResolvedValue({ events: [], next_seq: 0 }),
    subscribeSessionMock: vi.fn().mockReturnValue(stream),
  };
});

vi.mock('../../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      getSessionHistory: getSessionHistoryMock,
      subscribeSession: subscribeSessionMock,
      promptSession: vi.fn().mockResolvedValue(undefined),
      closeSession: vi.fn().mockResolvedValue(undefined),
      listSessions: vi.fn().mockResolvedValue({ sessions: [] }),
    },
  }),
}));

import {
  sessionStore,
  openSession,
  closeActiveSession,
  appendLocalPrompt,
  loadMoreHistory,
} from '../../src/stores/sessionStore.svelte.ts';

function makeSession(overrides: Partial<SessionStatusResponse> = {}): SessionStatusResponse {
  return {
    session_id: 'sess-activity-1',
    backend: 'claude',
    status: 'active',
    created_at: 1700000000,
    last_active: 1700000000,
    tenant_id: 'system',
    ...overrides,
  } as SessionStatusResponse;
}

function envelope(seq: number, payload: Record<string, unknown>): SessionEventEnvelope {
  return {
    session_id: 'sess-activity-1',
    sequence: seq,
    timestamp: new Date().toISOString(),
    payload_json: payload,
  };
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe('sessionStore — historyLoading', () => {
  beforeEach(() => {
    getSessionHistoryMock.mockClear();
    subscribeSessionMock.mockClear();
  });

  afterEach(() => {
    closeActiveSession();
  });

  it('is true while the initial history fetch is outstanding, then false', async () => {
    let release!: (v: { events: SessionEventEnvelope[]; next_seq: number }) => void;
    getSessionHistoryMock.mockImplementationOnce(
      () => new Promise((resolve) => { release = resolve; }),
    );

    const openPromise = openSession(makeSession());
    await flush();
    expect(sessionStore.historyLoading).toBe(true);

    release({ events: [], next_seq: 0 });
    await openPromise;

    expect(sessionStore.historyLoading).toBe(false);
  });

  it('stays true for a second session opened in the same tenant until its own load completes', async () => {
    type HistoryPage = { events: SessionEventEnvelope[]; next_seq: number };
    let releaseFirst!: (v: HistoryPage) => void;
    let releaseSecond!: (v: HistoryPage) => void;
    getSessionHistoryMock
      .mockImplementationOnce(() => new Promise((resolve) => { releaseFirst = resolve; }))
      .mockImplementationOnce(() => new Promise((resolve) => { releaseSecond = resolve; }));

    const firstOpen = openSession(makeSession({ session_id: 'sess-first' }));
    await flush();
    const secondOpen = openSession(makeSession({ session_id: 'sess-second' }));
    await flush();
    expect(sessionStore.historyLoading).toBe(true);

    releaseFirst({ events: [], next_seq: 0 });
    await firstOpen;
    expect(sessionStore.activeSession?.session_id).toBe('sess-second');
    expect(sessionStore.historyLoading).toBe(true);

    releaseSecond({ events: [], next_seq: 0 });
    await secondOpen;
    expect(sessionStore.historyLoading).toBe(false);
  });

  it('pages an earlier session from its own cursor when another session opens mid-request', async () => {
    getSessionHistoryMock
      .mockImplementationOnce(() => Promise.resolve({ events: [envelope(5, {})], next_seq: 9 }))
      .mockImplementationOnce(() => Promise.resolve({ events: [], next_seq: 0 }))
      .mockImplementationOnce(() => Promise.resolve({ events: [], next_seq: 0 }));

    await openSession(makeSession({ session_id: 'sess-first' }));
    // The load-more is still awaiting its client when the second session
    // resets the cursor; the request it sends must use the first session's.
    const loadMore = loadMoreHistory();
    const secondOpen = openSession(makeSession({ session_id: 'sess-second' }));
    await Promise.all([loadMore, secondOpen]);

    const asked = getSessionHistoryMock.mock.calls.map(
      (call) => call[0] as { session_id: string; from_sequence: number },
    );
    expect(asked).toEqual([
      expect.objectContaining({ session_id: 'sess-first', from_sequence: 1 }),
      expect.objectContaining({ session_id: 'sess-first', from_sequence: 9 }),
      expect.objectContaining({ session_id: 'sess-second', from_sequence: 1 }),
    ]);
  });

  it('is false once history resolves even when the session has zero events', async () => {
    getSessionHistoryMock.mockResolvedValueOnce({ events: [], next_seq: 0 });
    await openSession(makeSession());
    expect(sessionStore.historyLoading).toBe(false);
    expect(sessionStore.events).toHaveLength(0);
  });
});

describe('sessionStore — turnStatus', () => {
  beforeEach(() => {
    getSessionHistoryMock.mockClear();
    getSessionHistoryMock.mockResolvedValue({ events: [], next_seq: 0 });
    subscribeSessionMock.mockClear();
  });

  afterEach(() => {
    closeActiveSession();
  });

  it('is idle when no prompt has been sent yet', async () => {
    await openSession(makeSession());
    expect(sessionStore.turnStatus).toBe('idle');
  });

  it('is pending immediately after the operator submits a prompt, before any reply', async () => {
    await openSession(makeSession());
    appendLocalPrompt('hello');
    expect(sessionStore.turnStatus).toBe('pending');
  });

  it('is working while an assistant_chunk has not yet reported a stop_reason', async () => {
    await openSession(makeSession());
    await flush();
    appendLocalPrompt('hello');
    streamController.push(envelope(1, { kind: 'prompt_submitted', prompt: { prompt: 'hello' } }));
    await flush();
    streamController.push(envelope(2, { kind: 'assistant_chunk', assistant_chunk: { text: 'Hi' } }));
    await flush();
    expect(sessionStore.turnStatus).toBe('working');
  });

  it('is working while a tool_invocation has no matching tool_result yet', async () => {
    await openSession(makeSession());
    await flush();
    appendLocalPrompt('run it');
    streamController.push(envelope(1, { kind: 'prompt_submitted', prompt: { prompt: 'run it' } }));
    await flush();
    streamController.push(envelope(2, {
      kind: 'tool_invocation',
      tool_invocation: { invocation_id: 'inv-1', name: 'Bash', args: {} },
    }));
    await flush();
    expect(sessionStore.turnStatus).toBe('working');
  });

  it('returns to idle once the assistant_chunk reports a stop_reason', async () => {
    await openSession(makeSession());
    await flush();
    appendLocalPrompt('hello');
    streamController.push(envelope(1, { kind: 'prompt_submitted', prompt: { prompt: 'hello' } }));
    await flush();
    streamController.push(envelope(2, {
      kind: 'assistant_chunk',
      assistant_chunk: { text: 'Hi', stop_reason: 'end_turn' },
    }));
    await flush();
    expect(sessionStore.turnStatus).toBe('idle');
  });

  // ---------------------------------------------------------------------------
  // Authoritative turn_started/turn_completed/turn_failed signal.
  //
  // Once these events exist in the transcript, turnStatus must be driven
  // exclusively by turn_id matching — not by the chunk-stop_reason /
  // tool-call heuristic exercised above, which is retained only as a
  // fallback for transcripts that predate these events.
  // ---------------------------------------------------------------------------

  it('is working from turn_started until the matching turn_completed arrives', async () => {
    await openSession(makeSession());
    await flush();
    appendLocalPrompt('hello');
    streamController.push(envelope(1, { kind: 'turn_started', turn_started: { turn_id: 'turn-1' } }));
    await flush();
    expect(sessionStore.turnStatus).toBe('working');

    // An assistant_chunk with no stop_reason must not itself flip status —
    // only the matching terminal event does, now that real signals exist.
    streamController.push(envelope(2, { kind: 'assistant_chunk', assistant_chunk: { text: 'partial' } }));
    await flush();
    expect(sessionStore.turnStatus).toBe('working');

    streamController.push(envelope(3, {
      kind: 'turn_completed',
      turn_completed: { turn_id: 'turn-1', stop_reason: 'end_turn' },
    }));
    await flush();
    expect(sessionStore.turnStatus).toBe('idle');
  });

  it('is working from turn_started until the matching turn_failed arrives, and renders idle after', async () => {
    await openSession(makeSession());
    await flush();
    appendLocalPrompt('hello');
    streamController.push(envelope(1, { kind: 'turn_started', turn_started: { turn_id: 'turn-1' } }));
    await flush();
    expect(sessionStore.turnStatus).toBe('working');

    streamController.push(envelope(2, {
      kind: 'turn_failed',
      turn_failed: { turn_id: 'turn-1', error: 'boom' },
    }));
    await flush();
    expect(sessionStore.turnStatus).toBe('idle');
  });

  it('ignores a terminal event for a different turn_id', async () => {
    await openSession(makeSession());
    await flush();
    appendLocalPrompt('hello');
    streamController.push(envelope(1, { kind: 'turn_started', turn_started: { turn_id: 'turn-1' } }));
    await flush();

    streamController.push(envelope(2, {
      kind: 'turn_completed',
      turn_completed: { turn_id: 'someone-elses-turn' },
    }));
    await flush();
    expect(sessionStore.turnStatus).toBe('working');
  });

  it('is disconnected after the live subscription errors', async () => {
    subscribeSessionMock.mockImplementationOnce(() => {
      throw new Error('boom');
    });
    await openSession(makeSession());
    await flush();
    expect(sessionStore.turnStatus).toBe('disconnected');
  });

  it('is disconnected when the transport does not support streaming', async () => {
    subscribeSessionMock.mockImplementationOnce(() => {
      throw new Error('streaming unsupported on this transport');
    });
    await openSession(makeSession());
    await flush();
    expect(sessionStore.turnStatus).toBe('disconnected');
  });
});

describe('sessionStore — replay/live overlap dedup', () => {
  beforeEach(() => {
    getSessionHistoryMock.mockClear();
    subscribeSessionMock.mockClear();
  });

  afterEach(() => {
    closeActiveSession();
  });

  it('does not double-render a sequence delivered by both history and the live tail', async () => {
    // History already contains sequence 1.
    getSessionHistoryMock.mockResolvedValueOnce({
      events: [envelope(1, { kind: 'system_message', system: { message: 'boot', level: 'info' } })],
      next_seq: 0,
    });

    await openSession(makeSession());
    await flush();
    expect(sessionStore.events).toHaveLength(1);

    // The live tail races and re-delivers the same sequence once more.
    streamController.push(
      envelope(1, { kind: 'system_message', system: { message: 'boot', level: 'info' } }),
    );
    await flush();

    expect(sessionStore.events).toHaveLength(1);
  });
});
