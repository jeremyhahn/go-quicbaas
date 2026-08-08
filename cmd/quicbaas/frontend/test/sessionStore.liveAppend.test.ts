/**
 * sessionStore.liveAppend.test.ts
 *
 * Complements test/stores/sessionStore.test.ts (which covers history parsing
 * for every event kind and optimistic-prompt/SSE-echo reconciliation) with
 * coverage that is specific to the multi-participant LIVE tail: events of
 * different kinds (prompt, assistant chunk, tool invocation, presence)
 * arriving one at a time over subscribeSession's async stream — not via a
 * history replay — are appended to sessionStore.events live, in arrival
 * order, as soon as each one is pushed. Also confirms the subscription is
 * opened with follow=true so a session stays live for every attacher.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { SessionEventEnvelope, SessionStatusResponse } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// SDK mock — a controllable async-iterable stream for subscribeSession.
// ---------------------------------------------------------------------------

const { streamController, getSessionHistoryMock, subscribeSessionMock } = vi.hoisted(() => {
  let resolveNext: ((value: IteratorResult<SessionEventEnvelope>) => void) | null = null;
  const queue: SessionEventEnvelope[] = [];

  const push = (env: SessionEventEnvelope) => {
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

vi.mock('../src/stores/sdkClient.ts', () => ({
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

// Imported AFTER the mock so the module under test picks up the mocked client.
import { sessionStore, openSession, closeActiveSession } from '../src/stores/sessionStore.svelte.ts';

function makeSession(overrides: Partial<SessionStatusResponse> = {}): SessionStatusResponse {
  return {
    session_id: 'sess-live-1',
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
    session_id: 'sess-live-1',
    sequence: seq,
    timestamp: new Date().toISOString(),
    payload_json: payload,
    from_replay: false,
  };
}

// Waits a microtask tick so pending promises in the subscription loop settle.
async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe('sessionStore — multi-participant live sync', () => {
  beforeEach(() => {
    getSessionHistoryMock.mockClear();
    subscribeSessionMock.mockClear();
  });

  afterEach(() => {
    closeActiveSession();
  });

  it('appends events with mixed roles from the live stream in arrival order', async () => {
    await openSession(makeSession());
    await flush();

    streamController.push(
      envelope(1, { kind: 'prompt_submitted', source: 'operator:alice', prompt: { prompt: 'hi there' } }),
    );
    await flush();
    streamController.push(
      envelope(2, {
        kind: 'assistant_chunk',
        source: 'agent:claude',
        assistant_chunk: { text: 'hello!', stop_reason: 'end_turn' },
      }),
    );
    await flush();
    streamController.push(
      envelope(3, {
        kind: 'tool_invocation',
        source: 'agent:claude',
        tool_invocation: { invocation_id: 'inv-1', name: 'Bash', args: { cmd: 'ls' } },
      }),
    );
    await flush();
    streamController.push(
      envelope(4, { kind: 'operator_joined', source: 'operator:bob', operator_joined: { operator_id: 'bob' } }),
    );
    await flush();

    expect(sessionStore.events).toHaveLength(4);
    expect(sessionStore.events[0]).toMatchObject({ kind: 'prompt', prompt: 'hi there', sequence: 1 });
    expect(sessionStore.events[1]).toMatchObject({ kind: 'assistant_chunk', assistantChunk: 'hello!', sequence: 2 });
    expect(sessionStore.events[2]).toMatchObject({ kind: 'tool_invocation', toolName: 'Bash', sequence: 3 });
    expect(sessionStore.events[3]).toMatchObject({ kind: 'operator_joined', operatorId: 'bob', sequence: 4 });
  });

  it('subscribes with follow=true so multiple attachers keep receiving new events', async () => {
    await openSession(makeSession());
    await flush();

    expect(subscribeSessionMock).toHaveBeenCalledWith(
      expect.objectContaining({ session_id: 'sess-live-1', follow: true }),
    );
  });
});
