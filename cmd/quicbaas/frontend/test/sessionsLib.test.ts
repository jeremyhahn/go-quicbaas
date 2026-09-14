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
 * Tests for the shared session-liveness rule (go-quicbaas#850).
 *
 * The defect these guard: a dashboard tile labelled "Active Sessions" whose
 * value was the length of the session list, which holds closed sessions too. An
 * operator with one live session and 54 closed ones was told 54.
 */

import { describe, it, expect } from 'vitest';
import {
  TERMINAL_SESSION_STATES,
  countLiveSessions,
  isLiveSessionState,
  sessionLifecycleState,
} from '../src/lib/sessions.ts';

describe('isLiveSessionState', () => {
  it('counts the states a session can still be addressed in', () => {
    // Mirrors pkg/orchestration/backend.go: active, idle and resumable are all
    // sessions the operator can still prompt.
    expect(isLiveSessionState('active')).toBe(true);
    expect(isLiveSessionState('idle')).toBe(true);
    expect(isLiveSessionState('resumable')).toBe(true);
  });

  it('rejects the terminal states', () => {
    expect(isLiveSessionState('closed')).toBe(false);
    expect(isLiveSessionState('error')).toBe(false);
    expect(TERMINAL_SESSION_STATES.has('closed')).toBe(true);
    expect(TERMINAL_SESSION_STATES.has('error')).toBe(true);
  });

  it('rejects an absent or empty state rather than assuming it is live', () => {
    expect(isLiveSessionState(undefined)).toBe(false);
    expect(isLiveSessionState(null)).toBe(false);
    expect(isLiveSessionState('')).toBe(false);
    expect(isLiveSessionState(42 as unknown as string)).toBe(false);
  });

  it('counts a state this build has never heard of', () => {
    // SessionState is an open string on the wire. A state added server-side
    // must show up in the number an operator is judging the system by, not
    // vanish from it.
    expect(isLiveSessionState('draining')).toBe(true);
  });
});

describe('countLiveSessions', () => {
  // The two shapes the two transports actually produce for the same session.
  // REST writes orchestration.SessionInfo verbatim; the Wails binding renames.
  const restWireSessionShape = (id: string, state: string) => ({
    session_id: id,
    project_id: 'proj-1',
    backend: 'claude-code',
    state,
    created_at: 1,
    last_activity: 2,
  });
  const wailsBindingSessionShape = (id: string, status: string) => ({
    session_id: id,
    backend: 'claude-code',
    status,
    created_at: 1,
    last_active: 2,
  });

  it('counts the live records in a REST-shaped list', () => {
    // The browser's shape. A counter that named `status` read 0 here
    // (go-quicbaas#864).
    const sessions = [
      restWireSessionShape('s1', 'active'),
      restWireSessionShape('s2', 'closed'),
      restWireSessionShape('s3', 'closed'),
      restWireSessionShape('s4', 'idle'),
      restWireSessionShape('s5', 'error'),
      restWireSessionShape('s6', 'resumable'),
    ];

    expect(countLiveSessions(sessions)).toBe(3);
    expect(sessions.length).toBe(6);
  });

  it('counts the live records in a Wails-shaped list', () => {
    // The desktop's shape. A counter that named `state` read 0 here.
    const sessions = [
      wailsBindingSessionShape('s1', 'active'),
      wailsBindingSessionShape('s2', 'closed'),
      wailsBindingSessionShape('s3', 'idle'),
    ];

    expect(countLiveSessions(sessions)).toBe(2);
  });

  it('counts one list holding both shapes', () => {
    // Not a state the app reaches — one transport fills a store — but the
    // counter must not depend on the list being uniform to be right.
    expect(
      countLiveSessions([
        restWireSessionShape('s1', 'active'),
        wailsBindingSessionShape('s2', 'active'),
        restWireSessionShape('s3', 'closed'),
        wailsBindingSessionShape('s4', 'error'),
      ]),
    ).toBe(2);
  });

  it('counts zero for a list that has not loaded', () => {
    expect(countLiveSessions(undefined)).toBe(0);
    expect(countLiveSessions(null)).toBe(0);
    expect(countLiveSessions([])).toBe(0);
    expect(countLiveSessions('nonsense' as unknown as [])).toBe(0);
  });

  it('counts zero when every record is terminal', () => {
    expect(
      countLiveSessions([
        restWireSessionShape('s1', 'closed'),
        wailsBindingSessionShape('s2', 'error'),
      ]),
    ).toBe(0);
  });

  it('counts a record with no lifecycle field at all as not live', () => {
    expect(countLiveSessions([{ session_id: 's1' } as never])).toBe(0);
  });

  it('survives a null record in the list', () => {
    const ragged = [
      restWireSessionShape('s1', 'active'),
      null,
      wailsBindingSessionShape('s2', 'closed'),
    ] as unknown as Array<{ status?: string }>;
    expect(countLiveSessions(ragged)).toBe(1);
  });
});

describe('sessionLifecycleState', () => {
  it('reads the lifecycle state under either transport\'s name', () => {
    expect(sessionLifecycleState({ state: 'active' })).toBe('active');
    expect(sessionLifecycleState({ status: 'idle' })).toBe('idle');
  });

  it('prefers the SDK name when a record somehow carries both', () => {
    // Not reachable today; pinned so the preference is a decision, not an
    // accident of property order. go-quicbaas#856 makes `status` the only one.
    expect(sessionLifecycleState({ status: 'closed', state: 'active' })).toBe('closed');
  });

  it('returns nothing for a record that carries neither, or no record', () => {
    expect(sessionLifecycleState({})).toBeUndefined();
    expect(sessionLifecycleState(null)).toBeUndefined();
    expect(sessionLifecycleState(undefined)).toBeUndefined();
    expect(sessionLifecycleState('nonsense' as unknown as { status?: string })).toBeUndefined();
  });
});
