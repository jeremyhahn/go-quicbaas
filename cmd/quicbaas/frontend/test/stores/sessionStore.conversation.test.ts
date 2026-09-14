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
 * The session page reads the CONVERSATION, not only the event log
 * (go-quicbaas#834).
 *
 * The defect these guard was not a wrong answer — it was no question. The
 * server returned four turns for a session the UI drew as empty, the SDK
 * carried getSessionConversation end to end, and nothing in the frontend
 * ever called it. So the first and most important assertion here is made at
 * the wire: opening a session must put a request on
 * /orchestrator/sessions/{id}/history. A store that holds turns because a
 * double handed them over proves nothing about that.
 *
 * The rest state the properties an operator depends on: turns arrive in
 * reading order, a page boundary is followed, and the three ways a read can
 * end badly — refused, unavailable, genuinely empty — are told apart rather
 * than all rendering as the blank page that hid this bug for as long as it
 * did.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import {
  TOKEN_KEY,
  setAuthenticated,
  setUnauthenticated,
} from '../../src/stores/auth.svelte.ts';
import { resetSDKClient } from '../../src/stores/sdkClient.ts';
import { resetOrchestratorStore } from '../../src/stores/orchestrator.svelte.ts';
import {
  sessionStore,
  openSession,
  closeActiveSession,
  loadMoreConversation,
} from '../../src/stores/sessionStore.svelte.ts';
import type { SessionStatusResponse, SessionTurn } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const TENANT = 'automatethethings';
const SESSION_ID = '812c95c1-98d8-496c-93c3-bcd4ab861123';

function sessionToken(tenantID: string): string {
  const header = btoa(JSON.stringify({ alg: 'none', typ: 'at+jwt' }));
  const payload = btoa(
    JSON.stringify({
      sub: 'operator@example.test',
      uid: 'usr_01HZ',
      tenant_id: tenantID,
      roles: ['operator'],
      exp: Math.floor(Date.now() / 1000) + 900,
      sid: 'sess-1',
    }),
  );
  return `${header}.${payload}.sig`;
}

function session(id = SESSION_ID, status = 'active'): SessionStatusResponse {
  return {
    session_id: id,
    backend: 'claude',
    status,
    created_at: 1_767_225_600,
    last_active: 1_767_225_900,
  };
}

function turn(sequence: number, role: SessionTurn['role'], text: string): SessionTurn {
  return {
    sequence,
    session_id: SESSION_ID,
    tenant_id: TENANT,
    role,
    timestamp: `2026-09-07T12:0${sequence}:00Z`,
    text,
  };
}

/** The four turns the live node returns for the session in the bug report. */
const FOUR_TURNS: SessionTurn[] = [
  turn(1, 'system', 'session spawned'),
  turn(2, 'user', 'implement the thing'),
  turn(3, 'assistant', 'Done — here is what changed.'),
  turn(4, 'system', 'turn completed'),
];

// ---------------------------------------------------------------------------
// Transport stub
// ---------------------------------------------------------------------------

const originalFetch = window.fetch;

/** Every URL the SPA requested, in order. */
let requested: string[] = [];

/**
 * Conversation answers, consumed in order — one per /history request — so a
 * case can model paging by queueing two. When the queue runs dry the last
 * answer repeats, which is what a re-read of an unchanged transcript does.
 */
type ConversationAnswer =
  | { ok: true; turns: SessionTurn[]; next_cursor?: number; oldest_sequence?: number }
  | { ok: false; status: number; error: string };

let conversationAnswers: ConversationAnswer[] = [];

function nextConversationAnswer(): ConversationAnswer {
  if (conversationAnswers.length > 1) return conversationAnswers.shift() as ConversationAnswer;
  return conversationAnswers[0] ?? { ok: true, turns: [] };
}

function stubFetch(): void {
  const spy = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url =
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    requested.push(url);

    if (url.includes('/history')) {
      const answer = nextConversationAnswer();
      if (!answer.ok) {
        return new Response(JSON.stringify({ error: answer.error }), {
          status: answer.status,
          headers: { 'Content-Type': 'application/json' },
        });
      }
      return new Response(
        JSON.stringify({
          session_id: SESSION_ID,
          tenant_id: TENANT,
          turns: answer.turns,
          next_cursor: answer.next_cursor ?? 0,
          oldest_sequence: answer.oldest_sequence ?? 1,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    }

    // The live tail. Answered with a real SSE body so the store's own
    // subscription loop runs: the reaction under test is triggered by an
    // event arriving over it, and a double that skipped the loop would be
    // testing the test.
    if (url.includes('/events') && url.includes('follow=1')) {
      return new Response(sseBody(liveFrames), {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream' },
      });
    }

    // The event log and everything else: an empty, well-formed answer, so a
    // conversation case never fails for a reason that is not its own.
    return new Response(JSON.stringify({ events: [], next_seq: 0, sessions: [], valid: true }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  Object.defineProperty(window, 'fetch', { writable: true, configurable: true, value: spy });
}

/** SSE frames the live tail delivers, as (sequence, server event) pairs. */
let liveFrames: { sequence: number; event: Record<string, unknown> }[] = [];

/** Encode frames in the wire format the SDK's SSE parser reads, then close. */
function sseBody(frames: { sequence: number; event: Record<string, unknown> }[]): ReadableStream {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const frame of frames) {
        const data = JSON.stringify({ payload_json: frame.event });
        controller.enqueue(encoder.encode(`id: ${frame.sequence}\ndata: ${data}\n\n`));
      }
      controller.close();
    },
  });
}

/**
 * Let the fire-and-forget work openSession leaves behind — the live
 * subscription and anything it triggers — run to completion.
 */
async function settle(): Promise<void> {
  for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 0));
}

/** The /history requests made so far, parsed. */
function conversationRequests(): URL[] {
  return requested
    .filter((u) => u.includes('/orchestrator/sessions/') && u.includes('/history'))
    .map((u) => new URL(u, window.location.origin));
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('sessionStore conversation', () => {
  beforeEach(() => {
    requested = [];
    conversationAnswers = [];
    liveFrames = [];
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    stubFetch();
    setAuthenticated({ access_token: sessionToken(TENANT), refresh_token: 'refresh-1' });
  });

  afterEach(() => {
    closeActiveSession();
    setUnauthenticated();
    resetOrchestratorStore();
    resetSDKClient();
    window.localStorage.removeItem(TOKEN_KEY);
    Object.defineProperty(window, 'fetch', {
      writable: true,
      configurable: true,
      value: originalFetch,
    });
    vi.restoreAllMocks();
  });

  it('asks the server for the conversation when a session is opened', async () => {
    conversationAnswers = [{ ok: true, turns: FOUR_TURNS }];

    await openSession(session());

    const reqs = conversationRequests();
    expect(reqs).toHaveLength(1);
    expect(reqs[0]?.pathname).toBe(
      `/api/v1/orchestrator/sessions/${SESSION_ID}/history`,
    );
    expect(reqs[0]?.searchParams.get('tenant_id')).toBe(TENANT);
    expect(reqs[0]?.searchParams.get('limit')).toBe('100');
    // No cursor on the first page: the read starts at the beginning of what
    // the server still retains.
    expect(reqs[0]?.searchParams.get('cursor')).toBeNull();
  });

  it('exposes the turns in reading order, oldest first', async () => {
    conversationAnswers = [{ ok: true, turns: FOUR_TURNS }];

    await openSession(session());

    expect(sessionStore.conversation.map((t) => t.role)).toEqual([
      'system',
      'user',
      'assistant',
      'system',
    ]);
    expect(sessionStore.conversation.map((t) => t.text)).toEqual([
      'session spawned',
      'implement the thing',
      'Done — here is what changed.',
      'turn completed',
    ]);
    expect(sessionStore.conversationLoading).toBe(false);
    expect(sessionStore.conversationError).toBe('');
    expect(sessionStore.conversationErrorKind).toBe('');
    expect(sessionStore.conversationHasMore).toBe(false);
  });

  it('keeps the event log working alongside the conversation', async () => {
    conversationAnswers = [{ ok: true, turns: FOUR_TURNS }];

    await openSession(session());

    // The conversation is an addition, not a replacement: the events read
    // still goes out, and its (empty) result is still what events reports.
    expect(requested.some((u) => u.includes('/events'))).toBe(true);
    expect(sessionStore.events).toEqual([]);
    expect(sessionStore.conversation).toHaveLength(4);
  });

  it('follows next_cursor until the transcript is exhausted', async () => {
    conversationAnswers = [
      { ok: true, turns: [turn(1, 'user', 'first')], next_cursor: 2 },
      { ok: true, turns: [turn(2, 'assistant', 'second')], next_cursor: 0 },
    ];

    await openSession(session());

    const reqs = conversationRequests();
    expect(reqs).toHaveLength(2);
    expect(reqs[1]?.searchParams.get('cursor')).toBe('2');
    expect(sessionStore.conversation.map((t) => t.sequence)).toEqual([1, 2]);
    expect(sessionStore.conversationHasMore).toBe(false);
  });

  it('reports a session that has genuinely said nothing as empty, not as an error', async () => {
    conversationAnswers = [{ ok: true, turns: [] }];

    await openSession(session());

    expect(sessionStore.conversation).toEqual([]);
    expect(sessionStore.conversationError).toBe('');
    expect(sessionStore.conversationErrorKind).toBe('');
    expect(sessionStore.conversationLoading).toBe(false);
  });

  it('reports a refused read as forbidden rather than as an empty session', async () => {
    conversationAnswers = [{ ok: false, status: 403, error: 'tenant mismatch' }];

    await openSession(session());

    expect(sessionStore.conversation).toEqual([]);
    expect(sessionStore.conversationErrorKind).toBe('forbidden');
    expect(sessionStore.conversationError).toContain('tenant mismatch');
    expect(sessionStore.conversationLoading).toBe(false);
  });

  it('reports a node with no session log as unavailable', async () => {
    conversationAnswers = [
      { ok: false, status: 503, error: 'session event log not available' },
    ];

    await openSession(session());

    expect(sessionStore.conversationErrorKind).toBe('unavailable');
    expect(sessionStore.conversationError).toContain('session event log not available');
  });

  it('classifies any other failure as other, and still says so', async () => {
    conversationAnswers = [{ ok: false, status: 500, error: 'fold failed' }];

    await openSession(session());

    expect(sessionStore.conversationErrorKind).toBe('other');
    expect(sessionStore.conversationError).toContain('fold failed');
  });

  it('leaves the cursor where it was when a page fails, so a retry resumes', async () => {
    conversationAnswers = [
      { ok: true, turns: [turn(1, 'user', 'first')], next_cursor: 2 },
      { ok: false, status: 503, error: 'node draining' },
    ];

    await openSession(session());

    expect(sessionStore.conversation.map((t) => t.sequence)).toEqual([1]);
    expect(sessionStore.conversationHasMore).toBe(true);
    expect(sessionStore.conversationErrorKind).toBe('unavailable');

    // The retry resumes at the page that failed rather than re-reading the
    // one that succeeded or skipping past it.
    conversationAnswers = [{ ok: true, turns: [turn(2, 'assistant', 'second')], next_cursor: 0 }];
    const before = conversationRequests().length;
    await loadMoreConversation();

    const reqs = conversationRequests();
    expect(reqs).toHaveLength(before + 1);
    expect(reqs[before]?.searchParams.get('cursor')).toBe('2');
    expect(sessionStore.conversation.map((t) => t.sequence)).toEqual([1, 2]);
    expect(sessionStore.conversationHasMore).toBe(false);
    expect(sessionStore.conversationError).toBe('');
  });

  it('surfaces the retention floor the server reported', async () => {
    conversationAnswers = [{ ok: true, turns: FOUR_TURNS, oldest_sequence: 91 }];

    await openSession(session());

    expect(sessionStore.conversationOldestSequence).toBe(91);
  });

  it('does not put two overlapping walks on the wire', async () => {
    // Two walks from the same cursor would append the same turns twice, and
    // the turn list is keyed by sequence — a duplicate key is a render
    // error, not a cosmetic one.
    // The initial walk stops one page short, so the transcript genuinely
    // continues and both calls below have somewhere to go.
    conversationAnswers = [
      { ok: true, turns: [turn(1, 'user', 'first')], next_cursor: 2 },
      { ok: false, status: 503, error: 'node draining' },
    ];
    await openSession(session());
    expect(sessionStore.conversationHasMore).toBe(true);

    conversationAnswers = [{ ok: true, turns: [turn(2, 'assistant', 'second')], next_cursor: 0 }];
    const before = conversationRequests().length;
    await Promise.all([loadMoreConversation(), loadMoreConversation()]);

    expect(conversationRequests()).toHaveLength(before + 1);
    expect(sessionStore.conversation.map((t) => t.sequence)).toEqual([1, 2]);
  });

  it('does not walk a page the server did not offer', async () => {
    conversationAnswers = [{ ok: true, turns: FOUR_TURNS, next_cursor: 0 }];

    await openSession(session());
    const before = conversationRequests().length;
    await loadMoreConversation();

    expect(conversationRequests()).toHaveLength(before);
  });

  it('releases the conversation when the session is closed', async () => {
    conversationAnswers = [{ ok: true, turns: FOUR_TURNS }];

    await openSession(session());
    expect(sessionStore.conversation).toHaveLength(4);

    closeActiveSession();

    expect(sessionStore.conversation).toEqual([]);
    expect(sessionStore.conversationOldestSequence).toBe(0);
    expect(sessionStore.conversationHasMore).toBe(false);
  });

  it('re-reads the end of the conversation when the live tail closes a turn', async () => {
    conversationAnswers = [{ ok: true, turns: [turn(1, 'user', 'implement the thing')] }];
    liveFrames = [
      { sequence: 9, event: { kind: 'turn_completed', turn_completed: { turn_id: 't1' } } },
    ];

    await openSession(session());
    const afterOpen = conversationRequests().length;
    expect(afterOpen).toBe(1);

    // The answer the re-read finds: the turn the tail just closed.
    conversationAnswers = [
      {
        ok: true,
        turns: [turn(1, 'user', 'implement the thing'), turn(3, 'assistant', 'Done.')],
      },
    ];
    await settle();

    const reqs = conversationRequests();
    expect(reqs.length).toBeGreaterThan(afterOpen);
    // The re-read starts at the last turn already held, not past it: a turn
    // is a fold of events and may have grown since it was last read.
    expect(reqs[afterOpen]?.searchParams.get('cursor')).toBe('1');
    expect(sessionStore.conversation.map((t) => t.sequence)).toEqual([1, 3]);
  });

  it('does not re-read the conversation for an event that is not a turn boundary', async () => {
    conversationAnswers = [{ ok: true, turns: [turn(1, 'user', 'implement the thing')] }];
    liveFrames = [
      { sequence: 9, event: { kind: 'assistant_chunk', assistant_chunk: { text: 'Do' } } },
      { sequence: 10, event: { kind: 'assistant_chunk', assistant_chunk: { text: 'ne.' } } },
    ];

    await openSession(session());
    const afterOpen = conversationRequests().length;
    await settle();

    // A streamed answer is dozens of chunks and one turn. Re-reading on each
    // would put dozens of requests on the wire to watch it grow.
    expect(conversationRequests()).toHaveLength(afterOpen);
    expect(sessionStore.events.length).toBeGreaterThan(0);
  });

  it('does not carry one session’s turns into the next', async () => {
    conversationAnswers = [{ ok: true, turns: FOUR_TURNS }];
    await openSession(session());
    expect(sessionStore.conversation).toHaveLength(4);

    conversationAnswers = [{ ok: true, turns: [] }];
    await openSession(session('other-session'));

    expect(sessionStore.conversation).toEqual([]);
  });
});
