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
 *   • Paging the session's CONVERSATION via getSessionConversation.
 *
 * The events array is bounded to MAX_EVENTS entries. When it overflows the
 * oldest events are dropped from the top.
 *
 * Events and conversation are two views of one transcript, not alternatives.
 * The event log is every fragment, thought, presence change and file
 * operation in the order the server recorded them — what a live attacher
 * needs. The conversation is the same transcript folded into turns: who said
 * what, when, which tools ran. An operator opening a session came to read the
 * second one, so the store loads both and the page leads with the
 * conversation (go-quicbaas#834).
 */

import type {
  SessionStatusResponse,
  SessionEventEnvelope,
  SessionTurn,
  GetSessionConversationResponse,
} from '@quicbaas/sdk';
import { APIError } from '@quicbaas/sdk';
import { getSDKClient } from './sdkClient.ts';
import { orchestratorStore, tenantScope } from './orchestrator.svelte.ts';
import { authStore, onSessionScopeChange } from './auth.svelte.ts';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MAX_EVENTS = 5_000;
const HISTORY_PAGE_SIZE = 100;

/** Turns requested per conversation page. The server caps a page at 500. */
const CONVERSATION_PAGE_SIZE = 100;

/**
 * Conversation pages fetched in one go, by the initial load and by each press
 * of "load more". The endpoint only pages FORWARD from the start of the
 * retained transcript, so reading to the end of a very long session means
 * walking every page; this bounds one walk at a thousand turns and hands the
 * rest to the operator as an explicit control rather than a silent stall.
 */
const CONVERSATION_AUTO_PAGES = 10;

/**
 * Turns retained in memory. Past this the OLDEST are dropped, which is the
 * opposite end from MAX_EVENTS' bound because a conversation is read newest-
 * last: the turns worth keeping are the ones nearest the end.
 */
const MAX_TURNS = 2_000;

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
 * the live subscription — never from an invented timer:
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
// The reason the last history fetch failed, or '' when it did not.
//
// IT USED TO BE SWALLOWED, AND THAT IS WHY #793 LOOKED LIKE AN EMPTY SESSION.
// A page that cannot be read and a session that said nothing rendered
// identically — the same blank timeline — so an operator had no way to tell a
// transcript that was missing from one that was merely unreachable, and the
// underlying fault went unnoticed for as long as it did.
let historyError = $state('');
let connectionStatus: ConnectionStatus = $state('connected');

/**
 * ConversationErrorKind classifies why a conversation page could not be read,
 * so the page can say something true about it instead of printing a transport
 * message at an operator.
 *
 *   - ''            nothing failed.
 *   - 'forbidden'   the principal may not read this session's transcript
 *                   (401/403) — a tenant they are not scoped to, or an
 *                   expired grant.
 *   - 'unavailable' the daemon has no session event log wired, or the node
 *                   is not serving one yet (503). Transient by nature.
 *   - 'other'       anything else, including a network failure.
 */
export type ConversationErrorKind = '' | 'forbidden' | 'unavailable' | 'other';

// The session's conversation: the event log folded into turns. Empty until
// the first page lands, and gated on the same scope key as everything else
// here — a transcript is the last thing that should survive a tenant switch.
let conversation: SessionTurn[] = $state([]);
// True only while the initial conversation load for a freshly-opened session
// is in flight, exactly as historyLoading is. "Load more" runs without it, so
// pressing it replaces neither the loaded turns nor the page with a spinner.
let conversationLoading = $state(false);
let conversationError = $state('');
let conversationErrorKind: ConversationErrorKind = $state('');
// True when the server reported a next_cursor we have not followed, i.e. the
// transcript continues past what is loaded.
let conversationHasMore = $state(false);
// The lowest sequence the server still holds. Above 1 means retention has
// dropped earlier turns, and the first turn on screen is not the first turn
// of the session.
let conversationOldestSequence = $state(0);

/**
 * Session and tenant the cached session list belongs to; '' before anything is
 * fetched. Separate cache from orchestratorStore's, so it needs the same
 * protection: without it the sessions an operator was looking at stay on screen
 * for whoever signs in next (go-quicbaas#309).
 *
 * activeSession and its event stream are gated on the same key. An open session
 * belongs to the tenant it was opened in, and a conversation transcript is the
 * last thing that should survive into someone else's session.
 */
let scope = $state('');

/** Stable empty arrays for out-of-scope reads — see orchestrator.svelte.ts. */
const NO_SESSIONS: SessionStatusResponse[] = [];
const NO_EVENTS: ParsedSessionEvent[] = [];
const NO_TURNS: SessionTurn[] = [];

/** Whether the cached sessions still belong to the session and tenant in effect. */
function inScope(): boolean {
  return scope === tenantScope();
}

// Private: cursor for history pagination (oldest sequence fetched so far).
let _historyFromSeq = $state(0);
// Private: the next_cursor the last conversation page reported; 0 at the end
// of the transcript.
let _conversationCursor = 0;
// Private: tail-refresh serialisation. A burst of turn-boundary events must
// not put a burst of overlapping reads on the wire, and the last one must
// still happen — so a refresh arriving while one runs sets the pending flag
// and the running one loops again. No lock, no timer.
let _conversationRefreshing = false;
let _conversationRefreshPending = false;
// Private: a forward walk is on the wire. Two overlapping walks from the
// same cursor would append the same turns twice, and the turn list is keyed
// by sequence — a duplicate key is a render error, not a cosmetic one.
let _conversationPaging = false;
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
  get sessions() { return inScope() ? sessions : NO_SESSIONS; },
  get sessionsLoading() { return sessionsLoading; },
  get activeSession() { return inScope() ? activeSession : null; },
  get events() { return inScope() ? events : NO_EVENTS; },
  get historyExhausted() { return historyExhausted; },
  get historyLoading() { return historyLoading; },
  get historyError() { return inScope() ? historyError : ''; },
  get conversation() { return inScope() ? conversation : NO_TURNS; },
  get conversationLoading() { return conversationLoading; },
  get conversationError() { return inScope() ? conversationError : ''; },
  get conversationErrorKind(): ConversationErrorKind { return inScope() ? conversationErrorKind : ''; },
  get conversationHasMore() { return inScope() ? conversationHasMore : false; },
  get conversationOldestSequence() { return inScope() ? conversationOldestSequence : 0; },
  get connectionStatus() { return connectionStatus; },
  get turnStatus() { return computeTurnStatus(); },
};

// ---------------------------------------------------------------------------
// Session list management
// ---------------------------------------------------------------------------

/** Refresh the session list from the server. */
/**
 * Release everything belonging to a session or tenant no longer in effect.
 *
 * The getters already HIDE it on the next read, which is what keeps it off the
 * screen with no window at all. This is what actually lets it go — including
 * the live event subscription, which would otherwise keep streaming a departed
 * tenant's conversation against whatever bearer is current now.
 */
function releaseDepartedScope(): void {
  if (scope === tenantScope()) return;
  // No `scope === ''` early return. An empty scope means nothing is CACHED, not
  // that nothing is running: a live subscription started just before a sign-out
  // would otherwise keep streaming with no cache to give it away.
  closeSubscription();
  scope = '';
  sessions = [];
  activeSession = null;
  events = [];
  historyExhausted = false;
  historyLoading = false;
  historyError = '';
  connectionStatus = 'connected';
  _historyFromSeq = 0;
  resetConversation();
}

onSessionScopeChange(() => {
  releaseDepartedScope();
  // Same reason as agentStore: the session list belongs to the scope, so a
  // switch must refill it rather than blank it until the auxiliary poll.
  if (authStore.status === 'authenticated') void refreshSessions();
});

export async function refreshSessions(): Promise<void> {
  releaseDepartedScope();
  // Both pinned before any await, so the request and the decision to keep its
  // answer describe the same session and tenant.
  const asked = tenantScope();
  const tenantID = orchestratorStore.activeTenantID;
  sessionsLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listSessions({ tenantID });
    // A late answer for a session that has been replaced is discarded rather
    // than committed under whoever is signed in now.
    if (asked !== tenantScope()) return;
    const raw = resp.sessions ?? [];
    const seen = new Set<string>();
    sessions = raw.filter((s) => {
      if (seen.has(s.session_id)) return false;
      seen.add(s.session_id);
      return true;
    });
    scope = asked;
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
  // Tag the scope here too: a session can be opened before any list refresh has
  // run, and an untagged activeSession would be gated out as if it belonged to
  // someone else.
  scope = tenantScope();
  events = [];
  historyExhausted = false;
  historyError = '';
  connectionStatus = 'connected';
  _historyFromSeq = 1;
  resetConversation();

  // Load the first history page before starting the live tail. historyLoading
  // is scoped to this initial fetch only — EventStream's own isLoadingHistory
  // covers the later scroll-triggered pagination case — so the UI can show a
  // "loading history" state that is visually distinct from "no history at
  // all" (see EventStream's empty-state gating).
  const asked = scope;

  // The conversation and the event log are independent reads of the same
  // transcript, so they go out together rather than one behind the other —
  // the page's primary content does not wait on the log it summarises.
  conversationLoading = true;
  const conversationLoad = pageConversationFrom(session, 0, CONVERSATION_AUTO_PAGES)
    .finally(() => {
      if (asked === tenantScope() && activeSession?.session_id === session.session_id) {
        conversationLoading = false;
      }
    });

  historyLoading = true;
  try {
    await loadMoreHistory();
  } finally {
    if (asked === tenantScope()) historyLoading = false;
  }
  await conversationLoad;

  // The session may have ended while history was loading. Starting a live
  // stream now would resurrect a transcript that releaseDepartedScope has
  // already cleared, and tail it against whatever bearer is current.
  if (asked !== tenantScope() || activeSession?.session_id !== session.session_id) {
    return;
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
  historyError = '';
  connectionStatus = 'connected';
  _historyFromSeq = 0;
  resetConversation();
}

/** Drop every trace of the loaded conversation, cursors included. */
function resetConversation(): void {
  conversation = [];
  conversationLoading = false;
  conversationError = '';
  conversationErrorKind = '';
  conversationHasMore = false;
  conversationOldestSequence = 0;
  _conversationCursor = 0;
  // The in-flight refresh cannot be cancelled, but it checks the scope before
  // committing anything, so clearing the pending flag is enough to stop the
  // loop from starting another read for a session nobody is looking at.
  _conversationRefreshPending = false;
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

  // Pinned before the awaits: a page fetched for one session must not be
  // prepended onto another's transcript, or onto one that has been released.
  const asked = scope;
  const forSession = activeSession.session_id;

  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.getSessionHistory({
      session_id: forSession,
      from_sequence: _historyFromSeq,
      limit: HISTORY_PAGE_SIZE,
    });

    if (asked !== tenantScope() || activeSession?.session_id !== forSession) return;

    historyError = '';
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
  } catch (err: unknown) {
    // REPORTED, NOT SWALLOWED. A transcript that cannot be fetched must not
    // render as a session that said nothing (go-quicbaas#793). The pagination
    // cursor is left where it was so a retry resumes rather than skips.
    if (asked !== tenantScope() || activeSession?.session_id !== forSession) return;
    historyError = err instanceof Error ? err.message : String(err);
  }
}

// ---------------------------------------------------------------------------
// Conversation
// ---------------------------------------------------------------------------

/**
 * Fetch the next slice of the conversation the operator has not seen.
 *
 * Only meaningful while conversationHasMore is true — the transcript runs
 * past what is loaded. Exposed so the page can offer the walk as a control
 * instead of pretending a thousand turns is the whole of a longer session.
 * The caller owns its own busy state; this does not raise
 * conversationLoading, which belongs to the initial load alone.
 */
export async function loadMoreConversation(): Promise<void> {
  if (!conversationHasMore || activeSession === null || _conversationPaging) return;
  await pageConversationFrom(activeSession, _conversationCursor, CONVERSATION_AUTO_PAGES);
}

/**
 * Re-read the end of the conversation after the live event tail reported a
 * turn boundary.
 *
 * A conversation page is a FOLD of events, so a turn already on screen is not
 * final while its answer is still streaming: re-reading from that turn's own
 * sequence returns it complete, followed by whatever has happened since. That
 * is why the refresh starts at the last turn we hold rather than at the
 * cursor past it.
 *
 * It is a no-op when the transcript already runs past what is loaded — a new
 * turn is then beyond the loaded window anyway, and the operator reaches it
 * with loadMoreConversation.
 */
async function refreshConversationTail(): Promise<void> {
  if (activeSession === null || conversationHasMore) return;

  if (_conversationRefreshing) {
    // One read is already on the wire and will not have seen this event.
    // Ask it to go round again rather than racing a second read against it.
    _conversationRefreshPending = true;
    return;
  }

  _conversationRefreshing = true;
  try {
    do {
      _conversationRefreshPending = false;
      const session = activeSession;
      if (session === null) return;
      const last = conversation[conversation.length - 1];
      await pageConversationFrom(session, last?.sequence ?? 0, CONVERSATION_AUTO_PAGES);
    } while (_conversationRefreshPending);
  } finally {
    _conversationRefreshing = false;
  }
}

/**
 * Walk conversation pages forward from startCursor, committing what comes
 * back and stopping at the end of the transcript or after maxPages.
 *
 * The first page REPLACES every loaded turn at or after startCursor and later
 * pages append, which is what lets one function serve all three callers: the
 * initial load starts at 0 and so replaces everything, "load more" starts at
 * the cursor past the last loaded turn and so replaces nothing, and the tail
 * refresh starts at the last loaded turn and so re-states exactly that turn.
 * Committing the replacement together with the first page's answer is also
 * what keeps a refresh from blanking the last turn for a frame.
 */
async function pageConversationFrom(
  session: SessionStatusResponse,
  startCursor: number,
  maxPages: number,
): Promise<void> {
  // Pinned before the awaits, for the same reason loadMoreHistory pins them:
  // a page fetched for one session must never land on another's transcript.
  const asked = scope;
  const forSession = session.session_id;

  let cursor = startCursor;
  let replaced = false;

  _conversationPaging = true;
  try {
    for (let page = 0; page < maxPages; page++) {
      let resp: GetSessionConversationResponse;
      try {
        resp = await fetchConversationPage(session, cursor);
      } catch (err: unknown) {
        // REPORTED, NOT SWALLOWED — the whole point of go-quicbaas#793 and
        // of this change. A conversation that cannot be read must not render
        // as a session that said nothing. The cursor is left where it was so
        // a retry resumes rather than skips.
        if (stale(asked, forSession)) return;
        conversationError = err instanceof Error ? err.message : String(err);
        conversationErrorKind = classifyConversationError(err);
        return;
      }
      if (stale(asked, forSession)) return;

      conversationError = '';
      conversationErrorKind = '';
      conversationOldestSequence = resp.oldest_sequence ?? 0;

      const turns = resp.turns ?? [];
      if (!replaced) {
        conversation = [...conversation.filter((t) => t.sequence < startCursor), ...turns]
          .slice(-MAX_TURNS);
        replaced = true;
      } else if (turns.length > 0) {
        conversation = [...conversation, ...turns].slice(-MAX_TURNS);
      }

      cursor = resp.next_cursor ?? 0;
      _conversationCursor = cursor;
      conversationHasMore = cursor > 0;
      if (cursor === 0) return;
    }
  } finally {
    _conversationPaging = false;
  }
}

/**
 * Read one conversation page.
 *
 * The tenant is named explicitly, from the same source refreshSessions read
 * to find this session in the first place: an operator viewing a tenant
 * other than their own default must get that tenant's transcript, not an
 * empty page from their own. The server falls back to the authenticated
 * principal's tenant when the parameter is absent, so an unset active tenant
 * is left off rather than sent empty.
 */
async function fetchConversationPage(
  session: SessionStatusResponse,
  cursor: number,
): Promise<GetSessionConversationResponse> {
  const client = await getSDKClient();
  const tenantID = orchestratorStore.activeTenantID;
  return client.orchestrator.getSessionConversation({
    session_id: session.session_id,
    ...(tenantID ? { tenant_id: tenantID } : {}),
    cursor,
    limit: CONVERSATION_PAGE_SIZE,
  });
}

/** Map a failed read onto the reason the page will show. */
function classifyConversationError(err: unknown): ConversationErrorKind {
  if (err instanceof APIError) {
    if (err.status === 401 || err.status === 403) return 'forbidden';
    if (err.status === 503) return 'unavailable';
  }
  return 'other';
}

/**
 * Whether an answer pinned to `asked`/`forSession` still describes what is on
 * screen. Both halves matter: the tenant can change under a signed-in
 * operator, and the session can change without it.
 */
function stale(asked: string, forSession: string): boolean {
  return asked !== tenantScope() || activeSession?.session_id !== forSession;
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

  // A turn boundary just went past on the live tail, so the folded view of
  // this transcript is now behind the raw one. Re-read its end. Optimistic
  // placeholders (sequence <= 0) are skipped: the server has not recorded
  // them yet, so there is nothing new to fold.
  if (ev.sequence > 0 && CONVERSATION_BOUNDARY_KINDS.has(ev.kind)) {
    void refreshConversationTail();
  }
}

/**
 * Event kinds that open or close a conversation turn, and so change what a
 * conversation page would return. A Set rather than a chain of comparisons:
 * this runs on every event the live tail delivers.
 *
 * assistant_chunk is deliberately absent. A streamed answer is dozens of
 * them and exactly one turn, so refreshing on each would put dozens of reads
 * on the wire to watch one turn grow; turn_completed/turn_failed close it,
 * and the event log tab carries the answer live in the meantime.
 */
const CONVERSATION_BOUNDARY_KINDS: ReadonlySet<EventKind> = new Set<EventKind>([
  'prompt',
  'turn_completed',
  'turn_failed',
  'system',
  'closed',
  'resumed',
]);

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
