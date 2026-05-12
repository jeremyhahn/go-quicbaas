/**
 * stores/sessionStore.test.ts
 *
 * Verifies the subscription lifecycle and event parsing with a mocked SDK.
 * Tests the pure parseEnvelope logic by calling openSession and checking
 * that the store's events are populated with correctly parsed fields.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SessionEventEnvelope, SessionStatusResponse } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mutable config objects that vi.mock factories can close over.
// The mock factory itself is hoisted but the .impl closures are not.
// ---------------------------------------------------------------------------

const _mockHistory: { events: SessionEventEnvelope[]; next_seq: number } = {
  events: [],
  next_seq: 0,
};

// Build a fresh client object so vi.clearAllMocks() doesn't wipe out implementations.
function makeMockClient() {
  return {
    orchestrator: {
      listSessions: async () => ({ sessions: [], count: 0 }),
      getSessionHistory: async () => ({
        events: _mockHistory.events,
        next_seq: _mockHistory.next_seq,
      }),
      subscribeSession: () => (async function* () {})(),
    },
    connection: {},
  };
}

vi.mock('../../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockImplementation(() => Promise.resolve(makeMockClient())),
  resetSDKClient: vi.fn(),
}));

// Import the store AFTER setting up mocks.
import {
  sessionStore,
  openSession,
  closeActiveSession,
  loadMoreHistory,
} from '../../src/stores/sessionStore.svelte.ts';

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

const ALL_KINDS_ENVELOPES: SessionEventEnvelope[] = [
  {
    session_id: 'sess-1',
    sequence: 1,
    timestamp: '2025-01-01T00:00:00Z',
    payload_json: { kind: 'prompt', prompt: 'Hello' },
  },
  {
    session_id: 'sess-1',
    sequence: 2,
    timestamp: '2025-01-01T00:00:01Z',
    payload_json: { kind: 'assistant_chunk', assistant_chunk: 'Hi there', stop_reason: 'end_turn' },
  },
  {
    session_id: 'sess-1',
    sequence: 3,
    timestamp: '2025-01-01T00:00:02Z',
    payload_json: { kind: 'tool_invocation', tool_name: 'Read', tool_args: { path: 'main.go' }, invocation_id: 'inv-1' },
  },
  {
    session_id: 'sess-1',
    sequence: 4,
    timestamp: '2025-01-01T00:00:03Z',
    payload_json: { kind: 'tool_result', tool_result: 'package main', invocation_id: 'inv-1' },
  },
  {
    session_id: 'sess-1',
    sequence: 5,
    timestamp: '2025-01-01T00:00:04Z',
    payload_json: { kind: 'file_op', op: 'edit', path: 'main.go', diff: '+added' },
  },
  {
    session_id: 'sess-1',
    sequence: 6,
    timestamp: '2025-01-01T00:00:05Z',
    payload_json: { kind: 'system', system: 'Build done', level: 'info' },
  },
  {
    session_id: 'sess-1',
    sequence: 7,
    timestamp: '2025-01-01T00:00:06Z',
    payload_json: { kind: 'operator_joined', operator_id: 'alice' },
  },
  {
    session_id: 'sess-1',
    sequence: 8,
    timestamp: '2025-01-01T00:00:07Z',
    payload_json: { kind: 'operator_left', operator_id: 'alice' },
  },
  {
    session_id: 'sess-1',
    sequence: 9,
    timestamp: '2025-01-01T00:00:08Z',
    payload_json: { kind: 'closed', reason: 'user request' },
  },
  {
    session_id: 'sess-1',
    sequence: 10,
    timestamp: '2025-01-01T00:00:09Z',
    payload_json: { kind: 'resumed' },
  },
];

const fakeSession: SessionStatusResponse = {
  session_id: 'sess-1',
  backend: 'claude',
  status: 'active',
  created_at: 1700000000,
  last_active: 1700000100,
};

describe('sessionStore', () => {
  beforeEach(() => {
    closeActiveSession();
    // Do NOT call vi.clearAllMocks() here — it would wipe the mock implementations.
    // Reset only the mutable config that tests may vary.
    _mockHistory.events = [...ALL_KINDS_ENVELOPES];
    _mockHistory.next_seq = 0;
  });

  it('starts with empty events', () => {
    expect(sessionStore.events).toHaveLength(0);
  });

  it('starts with no active session', () => {
    expect(sessionStore.activeSession).toBeNull();
  });

  it('openSession loads history and populates events', async () => {
    await openSession(fakeSession);
    expect(sessionStore.events.length).toBeGreaterThan(0);
  });

  it('openSession exhausts history when next_seq is 0', async () => {
    _mockHistory.next_seq = 0;
    await openSession(fakeSession);
    expect(sessionStore.historyExhausted).toBe(true);
  });

  it('parses prompt event correctly', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'prompt');
    expect(ev).toBeDefined();
    expect(ev!.prompt).toBe('Hello');
  });

  it('parses assistant_chunk with stopReason', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'assistant_chunk');
    expect(ev).toBeDefined();
    expect(ev!.assistantChunk).toBe('Hi there');
    expect(ev!.stopReason).toBe('end_turn');
  });

  it('parses tool_invocation with name, args, and invocation_id', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'tool_invocation');
    expect(ev).toBeDefined();
    expect(ev!.toolName).toBe('Read');
    expect(ev!.toolArgs).toEqual({ path: 'main.go' });
    expect(ev!.invocationId).toBe('inv-1');
  });

  it('parses tool_result with content and invocation_id', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'tool_result');
    expect(ev).toBeDefined();
    expect(ev!.toolResult).toBe('package main');
    expect(ev!.toolResultInvocationId).toBe('inv-1');
  });

  it('parses file_op with op, path, and diff', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'file_op');
    expect(ev).toBeDefined();
    expect(ev!.fileOp).toBe('edit');
    expect(ev!.filePath).toBe('main.go');
    expect(ev!.fileDiff).toBe('+added');
  });

  it('parses system message with level', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'system');
    expect(ev).toBeDefined();
    expect(ev!.systemMessage).toBe('Build done');
    expect(ev!.systemLevel).toBe('info');
  });

  it('parses operator_joined with operatorId', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'operator_joined');
    expect(ev).toBeDefined();
    expect(ev!.operatorId).toBe('alice');
  });

  it('parses operator_left with operatorId', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'operator_left');
    expect(ev).toBeDefined();
    expect(ev!.operatorId).toBe('alice');
  });

  it('parses closed event with reason', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'closed');
    expect(ev).toBeDefined();
    expect(ev!.closeReason).toBe('user request');
  });

  it('parses resumed event', async () => {
    await openSession(fakeSession);
    const ev = sessionStore.events.find((e) => e.kind === 'resumed');
    expect(ev).toBeDefined();
    expect(ev!.kind).toBe('resumed');
  });

  it('closeActiveSession clears events and activeSession', async () => {
    await openSession(fakeSession);
    closeActiveSession();
    expect(sessionStore.events).toHaveLength(0);
    expect(sessionStore.activeSession).toBeNull();
  });

  it('historyExhausted stays false when next_seq > 0', async () => {
    _mockHistory.next_seq = 11;
    await openSession(fakeSession);
    expect(sessionStore.historyExhausted).toBe(false);
  });

  it('loadMoreHistory is a no-op when historyExhausted', async () => {
    _mockHistory.next_seq = 0;
    await openSession(fakeSession); // exhausts history
    expect(sessionStore.historyExhausted).toBe(true);
    const { getSDKClient } = await import('../../src/stores/sdkClient.ts');
    const callsBefore = vi.mocked(getSDKClient).mock.calls.length;
    await loadMoreHistory();
    expect(vi.mocked(getSDKClient).mock.calls.length).toBe(callsBefore);
  });

  it('loadMoreHistory is a no-op when no active session', async () => {
    closeActiveSession();
    const { getSDKClient } = await import('../../src/stores/sdkClient.ts');
    vi.mocked(getSDKClient).mockClear();
    await loadMoreHistory();
    expect(vi.mocked(getSDKClient)).not.toHaveBeenCalled();
  });

  it('openSession sets the active session correctly', async () => {
    await openSession(fakeSession);
    expect(sessionStore.activeSession).toEqual(fakeSession);
  });

  it('empty history response exhausts history immediately', async () => {
    _mockHistory.events = [];
    _mockHistory.next_seq = 0;
    await openSession(fakeSession);
    expect(sessionStore.historyExhausted).toBe(true);
    expect(sessionStore.events).toHaveLength(0);
  });
});
