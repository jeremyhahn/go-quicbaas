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
 * SessionConversation renders the turns, and tells the four ways of having
 * none of them apart (go-quicbaas#834).
 *
 * "No silent empty page" is the requirement, and it has teeth only if every
 * state that could produce a blank one says something different: still
 * loading, refused, unavailable, and genuinely empty — with the empty case
 * further split by whether the session can still speak. A component that
 * drew the same nothing for all of them is what made the original defect
 * invisible, so these assert on the distinguishing text, not on emptiness.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import type { SessionTurn } from '@quicbaas/sdk';
import type { ConversationErrorKind } from '../src/stores/sessionStore.svelte.ts';

// ---------------------------------------------------------------------------
// Store double
// ---------------------------------------------------------------------------

let mockTurns: SessionTurn[] = [];
let mockLoading = false;
let mockError = '';
let mockErrorKind: ConversationErrorKind = '';
let mockHasMore = false;
let mockOldestSequence = 1;

const loadMoreConversation = vi.fn().mockResolvedValue(undefined);

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: {
    get conversation() { return mockTurns; },
    get conversationLoading() { return mockLoading; },
    get conversationError() { return mockError; },
    get conversationErrorKind() { return mockErrorKind; },
    get conversationHasMore() { return mockHasMore; },
    get conversationOldestSequence() { return mockOldestSequence; },
  },
  loadMoreConversation: (): Promise<void> => loadMoreConversation() as Promise<void>,
}));

import SessionConversation from '../src/components/orchestrator/SessionConversation.svelte';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function turn(overrides: Partial<SessionTurn> & Pick<SessionTurn, 'sequence' | 'role'>): SessionTurn {
  return {
    session_id: 'sess-1',
    tenant_id: 'automatethethings',
    timestamp: '2026-09-07T12:00:00Z',
    text: '',
    ...overrides,
  };
}

const CONVERSATION: SessionTurn[] = [
  turn({ sequence: 1, role: 'user', text: 'implement the thing' }),
  turn({
    sequence: 2,
    role: 'assistant',
    text: 'Done.\n  Indented line kept verbatim.',
    model: 'claude-opus-5',
    stop_reason: 'end_turn',
    tools: [
      { invocation_id: 'inv-1', name: 'Read' },
      { invocation_id: 'inv-2', name: 'Bash', is_error: true },
    ],
  }),
  turn({ sequence: 3, role: 'system', text: 'session closed' }),
];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SessionConversation', () => {
  beforeEach(() => {
    mockTurns = [];
    mockLoading = false;
    mockError = '';
    mockErrorKind = '';
    mockHasMore = false;
    mockOldestSequence = 1;
    loadMoreConversation.mockClear();
  });

  it('renders one block per turn, oldest first', () => {
    mockTurns = CONVERSATION;

    const { container } = render(SessionConversation);

    const blocks = container.querySelectorAll('[data-testid="conversation-turn"]');
    expect(blocks).toHaveLength(3);
    expect([...blocks].map((b) => b.getAttribute('data-role'))).toEqual([
      'user',
      'assistant',
      'system',
    ]);
    // Newest last: the reading order of a conversation, and the order the
    // composer below the page continues.
    expect([...blocks].map((b) => b.getAttribute('data-sequence'))).toEqual(['1', '2', '3']);
  });

  it('shows each turn’s role, timestamp and text', () => {
    mockTurns = CONVERSATION;

    const { container } = render(SessionConversation);

    expect(screen.getByText('implement the thing')).toBeInTheDocument();
    expect(screen.getByText('claude-opus-5')).toBeInTheDocument();
    expect(container.querySelector('time[datetime="2026-09-07T12:00:00Z"]')).not.toBeNull();
    expect(screen.getByText('end_turn')).toBeInTheDocument();
  });

  it('keeps the text verbatim, wrapped rather than reflowed', () => {
    mockTurns = [CONVERSATION[1] as SessionTurn];

    const { container } = render(SessionConversation);

    const body = container.querySelector('[data-testid="conversation-turn"] pre');
    expect(body).not.toBeNull();
    // An agent's answer is code as often as it is prose: the newline and the
    // leading spaces are content, and a long line must wrap instead of
    // making the page scroll sideways.
    expect(body?.textContent).toBe('Done.\n  Indented line kept verbatim.');
    expect(body?.className).toContain('whitespace-pre-wrap');
    expect(body?.className).toContain('break-words');
  });

  it('indexes the tools a turn used, marking the ones that failed', () => {
    mockTurns = [CONVERSATION[1] as SessionTurn];

    render(SessionConversation);

    expect(screen.getByText('Read')).toBeInTheDocument();
    expect(screen.getByText('Bash').className).toContain('badge-error');
  });

  it('says it is still looking, rather than showing an empty conversation', () => {
    mockLoading = true;

    render(SessionConversation);

    expect(screen.getByTestId('conversation-loading')).toBeInTheDocument();
    expect(screen.queryByTestId('conversation-empty')).toBeNull();
  });

  it('says a refused read was refused', () => {
    mockError = 'tenant mismatch';
    mockErrorKind = 'forbidden';

    render(SessionConversation);

    const box = screen.getByTestId('conversation-error');
    expect(box).toHaveTextContent(/not permitted/i);
    expect(box).toHaveTextContent('tenant mismatch');
    expect(screen.queryByTestId('conversation-empty')).toBeNull();
  });

  it('says an unavailable node is unavailable, not that the session said nothing', () => {
    mockError = 'session event log not available';
    mockErrorKind = 'unavailable';

    render(SessionConversation);

    expect(screen.getByTestId('conversation-error')).toHaveTextContent(/not available on this node/i);
  });

  it('falls back to a plain failure for anything else', () => {
    mockError = 'NetworkError when attempting to fetch resource';
    mockErrorKind = 'other';

    render(SessionConversation);

    const box = screen.getByTestId('conversation-error');
    expect(box).toHaveTextContent(/could not be loaded/i);
    expect(box).toHaveTextContent('NetworkError when attempting to fetch resource');
  });

  it('distinguishes an open session waiting for a prompt from a closed one that never spoke', () => {
    const open = render(SessionConversation);
    expect(open.getByTestId('conversation-empty')).toHaveTextContent(/No turns yet/i);
    open.unmount();

    const closed = render(SessionConversation, { props: { sessionClosed: true } });
    expect(closed.getByTestId('conversation-empty')).toHaveTextContent(/closed without a conversation/i);
  });

  it('keeps the turns on screen when a later read fails', () => {
    mockTurns = CONVERSATION;
    mockError = 'node draining';
    mockErrorKind = 'unavailable';

    const { container } = render(SessionConversation);

    // A failure after something has been read is a warning about the rest,
    // not a reason to take away what was read.
    expect(container.querySelectorAll('[data-testid="conversation-turn"]')).toHaveLength(3);
    expect(screen.getByRole('alert')).toHaveTextContent('node draining');
  });

  it('says the top of the list is the beginning when nothing was dropped', () => {
    mockTurns = CONVERSATION;
    mockOldestSequence = 1;

    render(SessionConversation);

    expect(screen.getByText(/Beginning of conversation/i)).toBeInTheDocument();
  });

  it('says so when retention has dropped the earlier turns', () => {
    mockTurns = CONVERSATION;
    mockOldestSequence = 91;

    render(SessionConversation);

    expect(screen.getByText(/no longer retained/i)).toBeInTheDocument();
    expect(screen.queryByText(/Beginning of conversation/i)).toBeNull();
  });

  it('offers the rest of a transcript that runs past what is loaded', async () => {
    mockTurns = CONVERSATION;
    mockHasMore = true;

    render(SessionConversation);
    const button = screen.getByTestId('conversation-load-more');
    await fireEvent.click(button);

    expect(loadMoreConversation).toHaveBeenCalledTimes(1);
  });

  it('offers no such control when the whole transcript is loaded', () => {
    mockTurns = CONVERSATION;
    mockHasMore = false;

    render(SessionConversation);

    expect(screen.queryByTestId('conversation-load-more')).toBeNull();
  });
});
