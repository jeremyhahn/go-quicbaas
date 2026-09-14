// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * The live session stream is torn down when its session ends (go-quicbaas#309).
 *
 * Why this file exists separately: the tenant-scope suite drives the REAL SDK
 * over a stubbed fetch, and under jsdom the SSE path dies immediately with
 * `RequestInit: Expected signal ("AbortSignal {}") to be an instance of
 * AbortSignal` — a cross-realm AbortSignal, not a bug in the store. The
 * subscription therefore never established there, so the teardown assertion in
 * that suite was hollow: it passed with closeSubscription() deleted, which makes
 * it evidence of nothing.
 *
 * Here the SDK client is a double whose subscribeSession stays open until its
 * signal is aborted, so there is a real stream to tear down and the assertions
 * can be about the teardown itself:
 *
 *   • signing out ABORTS the subscription's signal, and
 *   • the consumer loop stops, so an event the server pushes afterwards is
 *     never applied to the store.
 *
 * Mutation criterion: with closeSubscription() removed from
 * releaseDepartedScope(), this file MUST fail.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { SessionEventEnvelope } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// A subscription that behaves like a live stream: it stays open, delivering
// nothing, until a test pushes an event or the consumer abandons it.
// ---------------------------------------------------------------------------

class EventChannel {
  /** Signal the store handed us, so a test can assert it was aborted. */
  signal: AbortSignal | null = null;
  /** Set when the consumer's for-await loop exits, however it exits. */
  abandonedByConsumer = false;
  /** Envelopes the consumer actually pulled. */
  delivered = 0;

  private queued: SessionEventEnvelope[] = [];
  private waiting: ((v: SessionEventEnvelope | null) => void) | null = null;

  push(env: SessionEventEnvelope): void {
    const w = this.waiting;
    if (w !== null) {
      this.waiting = null;
      w(env);
      return;
    }
    this.queued.push(env);
  }

  /**
   * End the stream when the signal aborts, which is what the real transport
   * does: aborting kills the SSE response body, the reader throws, and the
   * generator ends. A double that ignored the signal would leave the consumer
   * parked on an await forever and could never show a teardown at all.
   */
  attach(signal: AbortSignal | null): void {
    this.signal = signal;
    if (signal === null) return;
    signal.addEventListener('abort', () => {
      const w = this.waiting;
      if (w !== null) {
        this.waiting = null;
        w(null);
      }
    });
  }

  async *stream(): AsyncGenerator<SessionEventEnvelope> {
    try {
      for (;;) {
        const queued = this.queued.shift();
        if (queued !== undefined) {
          this.delivered++;
          yield queued;
          continue;
        }
        const next = await new Promise<SessionEventEnvelope | null>((resolve) => {
          this.waiting = resolve;
        });
        if (next === null) return;
        this.delivered++;
        yield next;
      }
    } finally {
      // for-await calls the generator's return() when the body breaks, so this
      // runs exactly when the store abandons the stream.
      this.abandonedByConsumer = true;
    }
  }
}

let channel = new EventChannel();

function envelope(sequence: number, text: string): SessionEventEnvelope {
  return {
    session_id: 'sess-1',
    sequence,
    timestamp: new Date().toISOString(),
    payload_json: { kind: 'prompt', source: 'user', prompt: { prompt: text } },
  };
}

const mockGetSessionHistory = vi.fn().mockResolvedValue({ events: [], next_seq: 0 });

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockImplementation(() =>
    Promise.resolve({
      orchestrator: {
        getSessionHistory: mockGetSessionHistory,
        subscribeSession: (req: { signal?: AbortSignal }) => {
          channel.attach(req.signal ?? null);
          return channel.stream();
        },
        listSessions: vi.fn().mockResolvedValue({ sessions: [] }),
      },
    }),
  ),
  resetSDKClient: vi.fn(),
}));

import {
  TOKEN_KEY,
  setAuthenticated,
  setUnauthenticated,
} from '../src/stores/auth.svelte.ts';
import { sessionStore, openSession } from '../src/stores/sessionStore.svelte.ts';
import { resetOrchestratorStore } from '../src/stores/orchestrator.svelte.ts';

function sessionToken(tenantID: string, uid: string): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({
      uid,
      tenant_id: tenantID,
      roles: ['operator'],
      exp: Math.floor(Date.now() / 1000) + 900,
    }),
  );
  return `${header}.${payload}.sig`;
}

/** Let pending microtasks and the subscription's async loop run. */
async function settle(): Promise<void> {
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
}

const openable = {
  session_id: 'sess-1',
  backend: 'claude',
  state: 'running',
  created_at: 0,
} as unknown as Parameters<typeof openSession>[0];

describe('the live session stream does not outlive its session', () => {
  beforeEach(async () => {
    vi.clearAllMocks();
    mockGetSessionHistory.mockResolvedValue({ events: [], next_seq: 0 });
    channel = new EventChannel();
    setUnauthenticated();
    resetOrchestratorStore();
    window.localStorage.removeItem(TOKEN_KEY);
  });

  afterEach(() => {
    setUnauthenticated();
    resetOrchestratorStore();
  });

  it('aborts the subscription signal when the session ends', async () => {
    setAuthenticated({ access_token: sessionToken('burnin', 'usr_A') });
    await openSession(openable);
    await settle();

    // A real, live stream: established, open, and nothing has ended it.
    expect(channel.signal).not.toBeNull();
    expect(channel.signal?.aborted).toBe(false);
    expect(sessionStore.activeSession?.session_id).toBe('sess-1');

    setUnauthenticated();

    // THE assertion. Without closeSubscription() in releaseDepartedScope this
    // signal is never aborted and the stream keeps tailing against whatever
    // bearer is current.
    expect(channel.signal?.aborted).toBe(true);
  });

  it('applies no event the server sends after the session has ended', async () => {
    setAuthenticated({ access_token: sessionToken('burnin', 'usr_A') });
    await openSession(openable);
    await settle();

    // Prove the stream is genuinely live first: an event sent BEFORE the
    // sign-out is delivered and stored.
    channel.push(envelope(1, 'before'));
    await settle();
    expect(channel.delivered).toBe(1);
    expect(sessionStore.events).toHaveLength(1);

    setUnauthenticated();
    await settle();

    // The consumer must have abandoned the stream, not merely stopped rendering.
    expect(channel.abandonedByConsumer).toBe(true);

    // Anything the server pushes now belongs to a session that is over: nobody
    // is reading, so it is never even delivered, let alone stored.
    channel.push(envelope(2, 'after'));
    await settle();
    expect(channel.delivered).toBe(1);
    expect(sessionStore.events).toHaveLength(0);

    // And it must not come back for the next person to sign in.
    setAuthenticated({ access_token: sessionToken('burnin', 'usr_A') });
    expect(sessionStore.events).toHaveLength(0);
    expect(sessionStore.activeSession).toBeNull();
  });
});
