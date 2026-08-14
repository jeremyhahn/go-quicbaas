// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
 * Session state store.
 *
 * Maintains the active session and its live event stream. The session list
 * itself is polled centrally by the orchestratorStore poller
 * (startOrchestratorPolling) every 30 s with stale-while-revalidate.
 *
 * This store is responsible only for:
 *   • Holding the active session selection.
 *   • Streaming live session events via subscribeSession.
 *   • Paginating historical events via getSessionHistory.
 *
 * The events array is bounded to MAX_EVENTS entries. When it overflows the
 * oldest events are dropped from the top.
 */

import type { SessionStatusResponse, SessionEventEnvelope } from '@quicbaas/sdk';
import { getSDKClient } from './sdkClient.ts';
import { orchestratorStore } from './orchestrator.svelte.ts';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MAX_EVENTS = 5_000;
const HISTORY_PAGE_SIZE = 100;

// ---------------------------------------------------------------------------
// Parsed event payload
// ---------------------------------------------------------------------------

export type EventKind =
  | 'prompt'
  | 'assistant_chunk'
  | 'thought'
  | 'tool_invocation'
  | 'tool_result'
  | 'file_op'
  | 'system'
  | 'operator_joined'
  | 'operator_left'
  | 'closed'
  | 'resumed'
  | 'turn_started'
  | 'turn_completed'
  | 'turn_failed';

/**
 * TurnStatus is the operator-facing activity state for the active session,
 * derived entirely from real signals already present in the transcript and
 * the live subscription — never from a fabricated timer:
 *
 *   - 'idle'         no turn is outstanding; the agent is waiting on input.
 *   - 'pending'      a prompt was submitted (optimistically or confirmed) but
 *                    the orchestrator hasn't recorded turn_started for it yet
 *                    — the request is in flight but the turn has not opened.
 *   - 'working'      a turn_started event has arrived with no matching
 *                    turn_completed/turn_failed (same turn_id) yet: the agent
 *                    is authoritatively still running the turn.
 *   - 'disconnected' the live SSE tail errored or the active transport does
 *                    not support streaming, so activity can no longer be
 *                    tracked live.
 *
 * The server emits turn_started when it dispatches a turn and
 * turn_completed/turn_failed when it ends (see
 * pkg/orchestration/session_events.go); those events, matched on turn_id,
 * are the authoritative source for this state. The older heuristic —
 * inferring activity from assistant_chunk stop_reason and
 * tool_invocation/tool_result pairing — is kept only as a fallback for
 * transcripts recorded before turn lifecycle events existed.
 */
export type TurnStatus = 'idle' | 'pending' | 'working' | 'disconnected';

/** ConnectionStatus reflects the health of the live SSE subscription. */
type ConnectionStatus = 'connected' | 'unavailable' | 'error';

export interface ParsedSessionEvent {
  /** Sequence number from the server envelope. */
  sequence: number;
  /** ISO-8601 timestamp string. */
  timestamp: string;
  kind: EventKind;
  /** Raw unparsed payload for unknown kinds. */
  raw: Record<string, unknown>;
  /**
   * True for a locally-rendered placeholder that has not yet been confirmed by
   * the server. Currently set only for the operator's own optimistically
   * rendered prompt (appendLocalPrompt); cleared when the SSE prompt_submitted
   * echo reconciles it (appendEvent).
   */
  optimistic?: boolean;

  // kind-specific fields — only the relevant ones will be set.
  prompt?: string;
  assistantChunk?: string;
  stopReason?: string;
  source?: string;

  // thought — the agent's streamed reasoning fragment (see
  // pkg/orchestration/session_events.go's AgentThoughtPayload). Rendered
  // collapsed by default, distinct from the final assistantChunk text.
  thoughtText?: string;

  toolName?: string;
  toolArgs?: Record<string, unknown>;
  invocationId?: string;

  toolResult?: string;
  toolResultInvocationId?: string;

  fileOp?: string;
  filePath?: string;
  fileDiff?: string;

  systemMessage?: string;
  systemLevel?: 'info' | 'warn' | 'error';

  operatorId?: string;

  closeReason?: string;

  // turn_started / turn_completed / turn_failed — see
  // pkg/orchestration/session_events.go's TurnStartedPayload,
  // TurnCompletedPayload, and TurnFailedPayload. turnId is populated for all
  // three kinds and is what correlates a turn_started with the
  // turn_completed/turn_failed that closes it.
  turnId?: string;
  turnBackend?: string;
  turnModel?: string;
  turnStopReason?: string;
  turnDurationMs?: number;
  turnError?: string;
}

// ---------------------------------------------------------------------------
// Reactive state (Svelte 5 module-level runes)
// ---------------------------------------------------------------------------

// sessions list is kept here for components that need the richer
// SessionStatusResponse SDK type. The canonical list used for display lives
// in orchestratorStore.sessions (plain SessionInfo). refreshSessions() keeps
// both in sync.
let sessions: SessionStatusResponse[] = $state([]);
let sessionsLoading = $state(false);
let activeSession: SessionStatusResponse | null = $state(null);
let events: ParsedSessionEvent[] = $state([]);
let historyExhausted = $state(false);
// True only for the initial history fetch a session open triggers — distinct
// from EventStream's own pagination spinner — so the UI can tell "we haven't
// looked yet" apart from "we looked, and there's genuinely nothing here".
let historyLoading = $state(false);
let connectionStatus: ConnectionStatus = $state('connected');

// Private: cursor for history pagination (oldest sequence fetched so far).
let _historyFromSeq = $state(0);
// Private: in-flight subscription AbortController.
let _subscribeAC: AbortController | null = null;
// Private: monotonically-decreasing sentinel sequence for optimistic prompt
// placeholders. Real server sequences are positive (>= 1); negative values
// keep the keyed event list collision-free even when several unconfirmed
// prompts are in flight at once.
let _optimisticSeq = -1;

// ---------------------------------------------------------------------------
// Public store object (read-only getters)
// ---------------------------------------------------------------------------

export const sessionStore = {
  get sessions() { return sessions; },
  get sessionsLoading() { return sessionsLoading; },
  get activeSession() { return activeSession; },
  get events() { return events; },
  get historyExhausted() { return historyExhausted; },
  get historyLoading() { return historyLoading; },
  get connectionStatus() { return connectionStatus; },
  get turnStatus() { return computeTurnStatus(); },
};

// ---------------------------------------------------------------------------
// Session list management
// ---------------------------------------------------------------------------

/** Refresh the session list from the server. */
export async function refreshSessions(): Promise<void> {
  sessionsLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listSessions({ tenantID: orchestratorStore.activeTenantID });
    const raw = resp.sessions ?? [];
    const seen = new Set<string>();
    sessions = raw.filter((s) => {
      if (seen.has(s.session_id)) return false;
      seen.add(s.session_id);
      return true;
    });
  } catch {
    // Silently ignore transient errors; the next poll will retry.
  } finally {
    sessionsLoading = false;
  }
}

// ---------------------------------------------------------------------------
// Active session + live event subscription
// ---------------------------------------------------------------------------

/** Select a session and start streaming its events. */
export async function openSession(session: SessionStatusResponse): Promise<void> {
  // Tear down any existing subscription first.
  closeSubscription();

  activeSession = session;
  events = [];
  historyExhausted = false;
  connectionStatus = 'connected';
  _historyFromSeq = 1;

  // Load the first history page before starting the live tail. historyLoading
  // is scoped to this initial fetch only — EventStream's own isLoadingHistory
  // covers the later scroll-triggered pagination case — so the UI can show a
  // "loading history" state that is visually distinct from "no history at
  // all" (see EventStream's empty-state gating).
  historyLoading = true;
  try {
    await loadMoreHistory();
  } finally {
    historyLoading = false;
  }

  // Start live subscription from wherever history ended.
  const latestSeq = events.length > 0 ? (events[events.length - 1]?.sequence ?? 0) + 1 : 0;
  startLiveSubscription(session.session_id, latestSeq);
}

/** Close the active session and stop the subscription. */
export function closeActiveSession(): void {
  closeSubscription();
  activeSession = null;
  events = [];
  historyExhausted = false;
  historyLoading = false;
  connectionStatus = 'connected';
  _historyFromSeq = 0;
}

/**
 * Clear the locally-rendered transcript without touching the durable,
 * server-side session log. Used by the composer's `/clear` slash command —
 * the terminal-style "clear screen" a user expects does not imply deleting
 * anyone else's history, only this client's current view of it. Further
 * automatic history backfill is suppressed (historyExhausted=true) so the
 * cleared view doesn't immediately repopulate from a stale pagination
 * cursor; switching sessions (openSession) resets this normally.
 */
export function clearLocalTranscript(): void {
  events = [];
  historyExhausted = true;
}

/**
 * Request the active (or a specified) session be closed via the SDK. Shared
 * by the SessionDetail close button and the composer's `/close` slash
 * command so there is exactly one code path that talks to the server for
 * this action.
 */
export async function requestCloseSession(sessionId: string): Promise<void> {
  const client = await getSDKClient();
  await client.orchestrator.closeSession({ session_id: sessionId });
}

/** Fetch the next page of history and prepend to the events array. */
export async function loadMoreHistory(): Promise<void> {
  if (historyExhausted || activeSession === null) return;

  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.getSessionHistory({
      session_id: activeSession.session_id,
      from_sequence: _historyFromSeq,
      limit: HISTORY_PAGE_SIZE,
    });

    if (resp.events.length === 0) {
      // No events returned — we are at the beginning of the session log.
      historyExhausted = true;
      return;
    }

    const parsed = resp.events.map(parseEnvelope);
    // Prepend older events; keep total within MAX_EVENTS.
    const combined = [...parsed, ...events];
    events = combined.slice(0, MAX_EVENTS);

    // next_seq === 0 signals that the server has no more history pages.
    if (resp.next_seq === 0) {
      historyExhausted = true;
    } else {
      // Advance cursor to the sequence after the last one we fetched.
      _historyFromSeq = resp.next_seq;
    }
  } catch {
    // Silently ignore.
  }
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function closeSubscription(): void {
  if (_subscribeAC !== null) {
    _subscribeAC.abort();
    _subscribeAC = null;
  }
}

function startLiveSubscription(sessionID: string, fromSeq: number): void {
  const ac = new AbortController();
  _subscribeAC = ac;

  void (async () => {
    try {
      const client = await getSDKClient();
      const stream = client.orchestrator.subscribeSession({
        session_id: sessionID,
        from_sequence: fromSeq,
        follow: true,
        signal: ac.signal,
      });

      for await (const envelope of stream) {
        if (ac.signal.aborted) break;
        appendEvent(parseEnvelope(envelope));
      }
    } catch (err: unknown) {
      // AbortError is expected on teardown; ignore it.
      if (err instanceof Error && err.name === 'AbortError') return;
      // The REST transport doesn't support live streaming — that's a known
      // capability gap, not an error worth surfacing every time SessionDetail
      // mounts. Demote it to a single info log so the page still works (history
      // is loaded synchronously in openSession; only the live tail is missing).
      // Other transport errors stay at error level.
      const msg = err instanceof Error ? err.message : String(err);
      if (/streaming unsupported/i.test(msg)) {
        connectionStatus = 'unavailable';
        console.info('[sessionStore] live updates unavailable on REST transport (use QUIC or gRPC for tailing)');
        return;
      }
      connectionStatus = 'error';
      console.error('[sessionStore] subscription error', err);
    }
  })();
}

function appendEvent(ev: ParsedSessionEvent): void {
  // Reconcile the operator's own optimistic prompt: when the server echoes its
  // prompt_submitted event back over the SSE tail, replace the placeholder we
  // already rendered (matched on prompt text) with the confirmed, sequenced
  // event instead of rendering a duplicate chat bubble. We match the FIRST
  // still-unconfirmed placeholder so repeated identical prompts reconcile in
  // submit order.
  if (ev.kind === 'prompt') {
    const idx = events.findIndex(
      (e) => e.optimistic === true && e.kind === 'prompt' && e.prompt === ev.prompt,
    );
    if (idx !== -1) {
      const next = [...events];
      next[idx] = ev; // confirmed event carries the real sequence, no optimistic flag
      events = next;
      return;
    }
  }

  // Guard against duplicate delivery when the tail of the history replay
  // overlaps the start of the live SSE subscription (openSession resumes the
  // live tail from the sequence right after the last history page, but a
  // slow-arriving history page and a fast-arriving live event could in
  // principle race). Real server sequences are unique and strictly
  // increasing, so a duplicate can only ever appear at or behind the last
  // event we already hold — this keeps the common append path O(1) and only
  // pays for the lookup in the actual overlap case.
  const last = events[events.length - 1];
  if (ev.sequence > 0 && last !== undefined && ev.sequence <= last.sequence) {
    if (events.some((e) => e.sequence === ev.sequence)) return;
  }

  events = [...events, ev].slice(-MAX_EVENTS);
}

/**
 * Derive the current TurnStatus from the transcript and connection health.
 * See the TurnStatus doc comment for the full state-to-signal mapping.
 *
 * turn_started/turn_completed/turn_failed, matched by turn_id, are
 * authoritative whenever the loaded transcript contains at least one of
 * them: "working" holds from turn_started until the matching terminal event
 * arrives, full stop — no chunk/tool-call guessing. The older heuristic only
 * runs for transcripts that predate these events (no turn lifecycle event
 * has ever been observed in the currently-loaded window).
 */
function computeTurnStatus(): TurnStatus {
  if (connectionStatus === 'error' || connectionStatus === 'unavailable') return 'disconnected';

  let lastPromptIdx = -1;
  for (let i = events.length - 1; i >= 0; i--) {
    if (events[i]?.kind === 'prompt') {
      lastPromptIdx = i;
      break;
    }
  }
  if (lastPromptIdx === -1) return 'idle'; // no prompt has been sent yet this session.

  const hasTurnLifecycleEvents = events.some(
    (e) => e.kind === 'turn_started' || e.kind === 'turn_completed' || e.kind === 'turn_failed',
  );
  if (hasTurnLifecycleEvents) {
    return computeTurnStatusFromLifecycleEvents(lastPromptIdx);
  }
  return computeTurnStatusFromHeuristic(lastPromptIdx);
}

/**
 * Authoritative turn-status derivation: find the most recently started turn
 * and check whether its terminal event (same turn_id) has arrived.
 */
function computeTurnStatusFromLifecycleEvents(lastPromptIdx: number): TurnStatus {
  let lastTurnStartIdx = -1;
  let lastTurnId: string | undefined;
  for (let i = events.length - 1; i >= 0; i--) {
    if (events[i]?.kind === 'turn_started') {
      lastTurnStartIdx = i;
      lastTurnId = events[i]?.turnId;
      break;
    }
  }

  if (lastTurnStartIdx === -1) {
    // No turn_started in the currently-loaded window. If a terminal event
    // still arrived after the last prompt, its turn_started has simply
    // scrolled out of view (e.g. paginated history) rather than never
    // having happened, so the turn is resolved; otherwise it's genuinely
    // in flight and the orchestrator hasn't recorded turn_started yet.
    for (let i = lastPromptIdx + 1; i < events.length; i++) {
      const ev = events[i];
      if (ev?.kind === 'turn_completed' || ev?.kind === 'turn_failed') return 'idle';
    }
    return 'pending';
  }

  for (let i = lastTurnStartIdx + 1; i < events.length; i++) {
    const ev = events[i];
    if (ev === undefined) continue;
    if ((ev.kind === 'turn_completed' || ev.kind === 'turn_failed') && ev.turnId === lastTurnId) {
      // The turn resolved. If a newer prompt was submitted after that with no
      // turn_started of its own yet, that next turn is in flight.
      return lastPromptIdx > i ? 'pending' : 'idle';
    }
  }

  // turn_started with no terminal event yet: the agent is authoritatively
  // still working the turn.
  return 'working';
}

/**
 * Legacy heuristic, kept only for transcripts recorded before turn_started/
 * turn_completed/turn_failed existed on the wire: infer activity from the
 * most recent assistant_chunk's stop_reason and tool_invocation/tool_result
 * pairing since the last prompt.
 */
function computeTurnStatusFromHeuristic(lastPromptIdx: number): TurnStatus {
  if (lastPromptIdx === events.length - 1) return 'pending'; // sent, nothing back yet.

  let chunkOpen = false;
  const pendingInvocations = new Set<string>();
  for (let i = lastPromptIdx + 1; i < events.length; i++) {
    const ev = events[i];
    if (ev === undefined) continue;
    if (ev.kind === 'assistant_chunk') {
      chunkOpen = ev.stopReason === undefined || ev.stopReason === '';
    } else if (ev.kind === 'tool_invocation' && ev.invocationId) {
      pendingInvocations.add(ev.invocationId);
    } else if (ev.kind === 'tool_result' && ev.toolResultInvocationId) {
      pendingInvocations.delete(ev.toolResultInvocationId);
    }
  }

  return chunkOpen || pendingInvocations.size > 0 ? 'working' : 'idle';
}

/**
 * Optimistically render the operator's own prompt as a chat bubble the instant
 * they hit Send, without waiting for the server's prompt_submitted event to
 * echo back over the SSE tail. The placeholder carries a unique negative
 * sequence (so the keyed event list never collides with real, positive
 * sequences) and an `optimistic` flag so the SSE echo can reconcile it in place
 * (see appendEvent) rather than double-rendering. No-op when no session is
 * active.
 */
export function appendLocalPrompt(prompt: string): void {
  if (activeSession === null) return;
  const ev: ParsedSessionEvent = {
    sequence: _optimisticSeq--,
    timestamp: new Date().toISOString(),
    kind: 'prompt',
    raw: {},
    prompt,
    optimistic: true,
  };
  events = [...events, ev].slice(-MAX_EVENTS);
}

/**
 * Roll back an optimistic prompt placeholder. Called when promptSession fails.
 * Removes only a still-unconfirmed (optimistic) placeholder for the given text;
 * once the SSE echo has reconciled it into a confirmed event we leave it in
 * place, because at that point the server did record the prompt (the daemon
 * appends prompt_submitted before dispatching to the backend, so a backend
 * failure does not unrecord it).
 */
export function removeLocalPrompt(prompt: string): void {
  const idx = events.findIndex(
    (e) => e.optimistic === true && e.kind === 'prompt' && e.prompt === prompt,
  );
  if (idx === -1) return;
  events = [...events.slice(0, idx), ...events.slice(idx + 1)];
}

// ---------------------------------------------------------------------------
// Envelope → ParsedSessionEvent
// ---------------------------------------------------------------------------

// Server SessionEvent kind constants (the canonical wire vocabulary defined in
// pkg/orchestration/session_events.go). The store's internal EventKind uses
// shorter names for the UI switch; serverKindToEventKind bridges the two.
const SERVER_KIND_TO_EVENT_KIND: Record<string, EventKind> = {
  prompt_submitted: 'prompt',
  assistant_chunk: 'assistant_chunk',
  agent_thought: 'thought',
  tool_invocation: 'tool_invocation',
  tool_result: 'tool_result',
  file_op: 'file_op',
  system_message: 'system',
  operator_joined: 'operator_joined',
  operator_left: 'operator_left',
  session_closed: 'closed',
  session_resumed: 'resumed',
  turn_started: 'turn_started',
  turn_completed: 'turn_completed',
  turn_failed: 'turn_failed',
};

/** Safely read a nested object field from an unknown payload value. */
function objField(payload: Record<string, unknown>, key: string): Record<string, unknown> {
  const v = payload[key];
  return typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};
}

function parseEnvelope(env: SessionEventEnvelope): ParsedSessionEvent {
  // payload_json carries the full server-side SessionEvent verbatim: a
  // top-level `kind` discriminator, a `source` string, and exactly one nested
  // payload object keyed by the event family (prompt, assistant_chunk, …).
  const payload = (env.payload_json ?? {}) as Record<string, unknown>;
  const serverKind = String(payload['kind'] ?? '');
  const kind = SERVER_KIND_TO_EVENT_KIND[serverKind] ?? 'system';

  const base: ParsedSessionEvent = {
    sequence: env.sequence,
    timestamp: env.timestamp,
    kind,
    raw: payload,
    source: payload['source'] as string | undefined,
  };

  switch (kind) {
    case 'prompt': {
      const p = objField(payload, 'prompt');
      base.prompt = String(p['prompt'] ?? '');
      break;
    }

    case 'assistant_chunk': {
      const a = objField(payload, 'assistant_chunk');
      base.assistantChunk = String(a['text'] ?? '');
      base.stopReason = a['stop_reason'] as string | undefined;
      break;
    }

    case 'thought': {
      const t = objField(payload, 'thought');
      base.thoughtText = String(t['text'] ?? '');
      break;
    }

    case 'tool_invocation': {
      const ti = objField(payload, 'tool_invocation');
      const args = ti['args'];
      base.toolName = String(ti['name'] ?? '');
      base.toolArgs = (typeof args === 'object' && args !== null)
        ? (args as Record<string, unknown>)
        : {};
      base.invocationId = ti['invocation_id'] as string | undefined;
      break;
    }

    case 'tool_result': {
      const tr = objField(payload, 'tool_result');
      base.toolResult = String(tr['content'] ?? '');
      base.toolResultInvocationId = tr['invocation_id'] as string | undefined;
      break;
    }

    case 'file_op': {
      const f = objField(payload, 'file_op');
      base.fileOp = String(f['op'] ?? '');
      base.filePath = String(f['path'] ?? '');
      base.fileDiff = f['diff'] as string | undefined;
      break;
    }

    case 'system': {
      const sys = objField(payload, 'system');
      base.systemMessage = String(sys['message'] ?? '');
      const lvl = sys['level'] as string | undefined;
      base.systemLevel =
        lvl === 'warn' ? 'warn' : lvl === 'error' ? 'error' : 'info';
      break;
    }

    case 'operator_joined': {
      base.operatorId = String(objField(payload, 'operator_joined')['operator_id'] ?? '');
      break;
    }

    case 'operator_left': {
      base.operatorId = String(objField(payload, 'operator_left')['operator_id'] ?? '');
      break;
    }

    case 'closed': {
      base.closeReason = objField(payload, 'closed')['reason'] as string | undefined;
      break;
    }

    case 'resumed':
      break;

    case 'turn_started': {
      const t = objField(payload, 'turn_started');
      base.turnId = t['turn_id'] as string | undefined;
      base.turnBackend = t['backend'] as string | undefined;
      base.turnModel = t['model'] as string | undefined;
      break;
    }

    case 'turn_completed': {
      const t = objField(payload, 'turn_completed');
      base.turnId = t['turn_id'] as string | undefined;
      base.turnStopReason = t['stop_reason'] as string | undefined;
      base.turnModel = t['model'] as string | undefined;
      base.turnDurationMs = t['duration_ms'] as number | undefined;
      break;
    }

    case 'turn_failed': {
      const t = objField(payload, 'turn_failed');
      base.turnId = t['turn_id'] as string | undefined;
      base.turnError = t['error'] as string | undefined;
      base.turnDurationMs = t['duration_ms'] as number | undefined;
      break;
    }

    default:
      // Unknown kind — leave raw field for inspection.
      break;
  }

  return base;
}
