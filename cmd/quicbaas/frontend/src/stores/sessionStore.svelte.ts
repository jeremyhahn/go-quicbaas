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
  | 'tool_invocation'
  | 'tool_result'
  | 'file_op'
  | 'system'
  | 'operator_joined'
  | 'operator_left'
  | 'closed'
  | 'resumed';

export interface ParsedSessionEvent {
  /** Sequence number from the server envelope. */
  sequence: number;
  /** ISO-8601 timestamp string. */
  timestamp: string;
  kind: EventKind;
  /** Raw unparsed payload for unknown kinds. */
  raw: Record<string, unknown>;

  // kind-specific fields — only the relevant ones will be set.
  prompt?: string;
  assistantChunk?: string;
  stopReason?: string;
  source?: string;

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

// Private: cursor for history pagination (oldest sequence fetched so far).
let _historyFromSeq = $state(0);
// Private: in-flight subscription AbortController.
let _subscribeAC: AbortController | null = null;

// ---------------------------------------------------------------------------
// Public store object (read-only getters)
// ---------------------------------------------------------------------------

export const sessionStore = {
  get sessions() { return sessions; },
  get sessionsLoading() { return sessionsLoading; },
  get activeSession() { return activeSession; },
  get events() { return events; },
  get historyExhausted() { return historyExhausted; },
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
  _historyFromSeq = 1;

  // Load the first history page before starting the live tail.
  await loadMoreHistory();

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
  _historyFromSeq = 0;
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
        console.info('[sessionStore] live updates unavailable on REST transport (use QUIC or gRPC for tailing)');
        return;
      }
      console.error('[sessionStore] subscription error', err);
    }
  })();
}

function appendEvent(ev: ParsedSessionEvent): void {
  events = [...events, ev].slice(-MAX_EVENTS);
}

// ---------------------------------------------------------------------------
// Envelope → ParsedSessionEvent
// ---------------------------------------------------------------------------

function parseEnvelope(env: SessionEventEnvelope): ParsedSessionEvent {
  const payload = env.payload_json as Record<string, unknown>;
  const kind = (payload['kind'] as EventKind | undefined) ?? 'system';

  const base: ParsedSessionEvent = {
    sequence: env.sequence,
    timestamp: env.timestamp,
    kind,
    raw: payload,
  };

  switch (kind) {
    case 'prompt':
      base.prompt = String(payload['prompt'] ?? '');
      break;

    case 'assistant_chunk':
      base.assistantChunk = String(payload['assistant_chunk'] ?? '');
      base.stopReason = payload['stop_reason'] as string | undefined;
      base.source = payload['source'] as string | undefined;
      break;

    case 'tool_invocation': {
      const args = payload['tool_args'];
      base.toolName = String(payload['tool_name'] ?? '');
      base.toolArgs = (typeof args === 'object' && args !== null)
        ? (args as Record<string, unknown>)
        : {};
      base.invocationId = payload['invocation_id'] as string | undefined;
      break;
    }

    case 'tool_result':
      base.toolResult = String(payload['tool_result'] ?? '');
      base.toolResultInvocationId = payload['invocation_id'] as string | undefined;
      break;

    case 'file_op':
      base.fileOp = String(payload['op'] ?? '');
      base.filePath = String(payload['path'] ?? '');
      base.fileDiff = payload['diff'] as string | undefined;
      break;

    case 'system': {
      base.systemMessage = String(payload['system'] ?? '');
      const lvl = payload['level'] as string | undefined;
      base.systemLevel =
        lvl === 'warn' ? 'warn' : lvl === 'error' ? 'error' : 'info';
      break;
    }

    case 'operator_joined':
    case 'operator_left':
      base.operatorId = String(payload['operator_id'] ?? '');
      break;

    case 'closed':
      base.closeReason = payload['reason'] as string | undefined;
      break;

    case 'resumed':
      break;

    default:
      // Unknown kind — leave raw field for inspection.
      break;
  }

  return base;
}
