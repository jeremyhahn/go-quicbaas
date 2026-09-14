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
 * One definition of "this session is still live", shared by every counter that
 * claims live state.
 *
 * A dashboard tile labelled "Active Sessions" whose value was the length of the
 * session list read 54 to an operator who had one live session and 54 closed
 * ones (go-quicbaas#850). The list is right to hold the closed records — the
 * Sessions view shows them, and a closed session is still openable by id — so
 * the count, not the fetch, is what has to know the difference.
 *
 * THE FIELD'S NAME DEPENDS ON THE TRANSPORT, NOT ON THE STORE. Both stores fill
 * themselves from the same `listSessions` call, and the two transports spell the
 * lifecycle field differently:
 *
 *   REST (browser)   `state`   — `orchestration.SessionInfo`, written straight
 *                                out by RespondJSON and passed through unmapped
 *                                by RestOrchestratorClient.listSessions.
 *   Wails (desktop)  `status`  — renamed in OrchestratorBinding.ListSessions,
 *                                as the Go client's SessionStatusWire.Response()
 *                                also does.
 *
 * So a counter naming ONE of them is dead on the other transport, whichever it
 * picks. Two counters picked differently and each was zero on one platform
 * (go-quicbaas#864). Reading the value here — once, for every counter — is what
 * makes the number true on both, and it is deliberately NOT a per-caller choice:
 * the ability to name the field is exactly what let the two tiles disagree.
 *
 * go-quicbaas#856 is normalising this at the SDK seam, where it belongs, so that
 * every transport hands back `status`. When that lands, `state` below becomes
 * dead and can go — and `restWireSessionShape` in the tests is what will say so,
 * because it asserts what the REST client actually returns.
 */

/**
 * The states a session never comes back from, mirroring
 * pkg/orchestration/backend.go: `closed` is a session that was terminated, and
 * `error` is one whose failure was recorded deliberately so it would stop being
 * offered as live (see SessionRecord.Resumable).
 *
 * The membership test is on the TERMINAL set rather than on a list of live
 * states because SessionState is an open string on the wire: a state this build
 * has never heard of is far better counted — and seen by the operator — than
 * silently dropped from a number they are using to judge the system.
 */
export const TERMINAL_SESSION_STATES: ReadonlySet<string> = new Set(['closed', 'error']);

/**
 * isLiveSessionState reports whether a session in this state is still live.
 *
 * An absent or empty state is NOT live: it is the absence of an observation,
 * and a counter that claims live state must not count a record that never said
 * it was.
 */
export function isLiveSessionState(state: string | null | undefined): boolean {
  if (typeof state !== 'string' || state === '') return false;
  return !TERMINAL_SESSION_STATES.has(state);
}

/**
 * SessionLifecycle is the part of a session record a live count reads: the
 * lifecycle state, under either of the two names the transports give it.
 */
export interface SessionLifecycle {
  /** The name the Wails binding and every SDK use. */
  status?: string | null;
  /** The name the REST wire uses. */
  state?: string | null;
}

/**
 * sessionLifecycleState returns the record's lifecycle state.
 *
 * Exactly one of the two names is present on any given record, because one
 * transport produced it. `status` is preferred because it is the name the SDKs
 * settle on (go-quicbaas#856); `state` is the REST wire's spelling.
 */
export function sessionLifecycleState(
  record: SessionLifecycle | null | undefined,
): string | null | undefined {
  if (record === null || typeof record !== 'object') return undefined;
  return record.status ?? record.state;
}

/**
 * countLiveSessions counts the live sessions in a list.
 *
 * Non-array input counts zero, so a caller may hand it a store getter that has
 * not loaded yet, and a record carrying no lifecycle state at all counts as not
 * live — the same reasoning as isLiveSessionState.
 */
export function countLiveSessions(
  list: readonly SessionLifecycle[] | null | undefined,
): number {
  if (!Array.isArray(list)) return 0;
  let live = 0;
  for (const record of list) {
    if (isLiveSessionState(sessionLifecycleState(record))) live++;
  }
  return live;
}
