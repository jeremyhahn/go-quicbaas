/**
 * stores/sessionStore.historyError.test.ts
 *
 * A transcript that cannot be fetched must not render as a session that said
 * nothing. Before go-quicbaas#793 the history fetch swallowed every error, so
 * an unreachable conversation and an empty one produced the same blank
 * timeline and the underlying fault went unnoticed.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SessionEventEnvelope, SessionStatusResponse } from '@quicbaas/sdk';

// Mutable config the hoisted vi.mock factory closes over.
const _history: { events: SessionEventEnvelope[]; next_seq: number; error: Error | null } = {
  events: [],
  next_seq: 0,
  error: null,
};

function makeMockClient() {
  return {
    orchestrator: {
      listSessions: async () => ({ sessions: [], count: 0 }),
      getSessionHistory: async () => {
        if (_history.error) throw _history.error;
        return { events: _history.events, next_seq: _history.next_seq };
      },
      // eslint-disable-next-line require-yield
      subscribeSession: async function* (): AsyncGenerator<SessionEventEnvelope> {
        return;
      },
    },
    connection: {},
  };
}

vi.mock('../../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockImplementation(() => Promise.resolve(makeMockClient())),
  resetSDKClient: vi.fn(),
}));

import {
  sessionStore,
  openSession,
  closeActiveSession,
} from '../../src/stores/sessionStore.svelte.ts';

const SESSION: SessionStatusResponse = {
  session_id: 'sess-1',
  backend: 'claude',
  status: 'active',
  last_active: '2025-01-01T00:00:00Z',
};

describe('sessionStore history errors', () => {
  beforeEach(() => {
    closeActiveSession();
    _history.events = [];
    _history.next_seq = 0;
    _history.error = null;
  });

  it('reports a history fetch that failed instead of showing an empty session', async () => {
    _history.error = new Error('permission denied');

    await openSession(SESSION);

    expect(sessionStore.historyError).toBe('permission denied');
    expect(sessionStore.events).toHaveLength(0);
  });

  it('reports no error when the session genuinely has no history', async () => {
    await openSession(SESSION);

    expect(sessionStore.historyError).toBe('');
    expect(sessionStore.events).toHaveLength(0);
  });

  it('clears a previous error when a later session loads cleanly', async () => {
    _history.error = new Error('permission denied');
    await openSession(SESSION);
    expect(sessionStore.historyError).toBe('permission denied');

    _history.error = null;
    _history.events = [
      {
        session_id: 'sess-2',
        sequence: 1,
        timestamp: '2025-01-01T00:00:00Z',
        payload_json: { kind: 'prompt_submitted', prompt: { prompt: 'Hello' } },
      } as SessionEventEnvelope,
    ];
    await openSession({ ...SESSION, session_id: 'sess-2' });

    expect(sessionStore.historyError).toBe('');
    expect(sessionStore.events.length).toBeGreaterThan(0);
  });

  it('drops the error with the session when it is closed', async () => {
    _history.error = new Error('permission denied');
    await openSession(SESSION);
    expect(sessionStore.historyError).toBe('permission denied');

    closeActiveSession();

    expect(sessionStore.historyError).toBe('');
  });
});
