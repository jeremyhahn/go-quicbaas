/**
 * SessionDetail.test.ts
 *
 * Verifies:
 * 1. Header renders session ID, backend chip, and status badge.
 * 2. "Prompt" and "Close" action buttons are present for an active session.
 * 3. Send Prompt button is absent when session is closed.
 * 4. Clicking "Close" opens the confirm dialog.
 * 5. Metadata panel renders tenant info.
 * 6. Metadata panel links the orchestration job the session works for.
 * 7. The body leads with the conversation and keeps the event log reachable.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mocks — all vi.hoisted factories must be self-contained.
// ---------------------------------------------------------------------------

// Define the session shapes inside vi.hoisted so they are available when
// vi.mock factories are evaluated (factories are hoisted before top-level consts).
const { sessions } = vi.hoisted(() => {
  const active = {
    session_id: 'sess-aaa-bbb-ccc-111',
    backend: 'claude',
    status: 'active',
    created_at: 1700000000,
    last_active: 1700001000,
    model: 'claude-3-5-sonnet',
    tenant_id: 'default',
  };
  const closed = {
    ...active,
    session_id: 'sess-closed-222',
    status: 'closed',
  };
  return { sessions: { active, closed } };
});

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: {
    sessions: [sessions.active, sessions.closed],
    events: [],
    activeSession: null,
    historyExhausted: false,
    historyLoading: false,
    historyError: '',
    connectionStatus: 'connected',
    turnStatus: 'idle',
    // The conversation half of the transcript. A double that omitted these
    // would not be a smaller store, it would be a different one — the page
    // renders the conversation first, so leaving them out makes every case
    // in this file fail for a reason none of them is about.
    conversation: [],
    conversationLoading: false,
    conversationError: '',
    conversationErrorKind: '',
    conversationHasMore: false,
    conversationOldestSequence: 1,
  },
  openSession: vi.fn().mockResolvedValue(undefined),
  closeActiveSession: vi.fn(),
  loadMoreHistory: vi.fn().mockResolvedValue(undefined),
  loadMoreConversation: vi.fn().mockResolvedValue(undefined),
  requestCloseSession: vi.fn().mockResolvedValue(undefined),
}));

const { mockRotateSessionAccount } = vi.hoisted(() => ({
  mockRotateSessionAccount: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      closeSession: vi.fn().mockResolvedValue(undefined),
      promptSession: vi.fn().mockResolvedValue(undefined),
      subscribeSession: vi.fn().mockReturnValue({
        [Symbol.asyncIterator]: () => ({ next: async () => ({ done: true, value: undefined }) }),
      }),
      getSessionHistory: vi.fn().mockResolvedValue({ events: [], next_seq: 0 }),
      rotateSessionAccount: mockRotateSessionAccount,
    },
  }),
}));

// Stub EventStream (uses IntersectionObserver, complex DOM). The stub is
// identifiable in the DOM so a case can assert WHICH view of the transcript
// the body is showing.
vi.mock('../src/components/orchestrator/EventStream.svelte', async () => ({
  default: (await import('./stubs/EventStreamStub.svelte')).default,
}));

// Stub PromptComposer.
vi.mock('../src/components/orchestrator/PromptComposer.svelte', () => ({
  default: vi.fn(),
}));

// Stub PresenceIndicator.
vi.mock('../src/components/orchestrator/PresenceIndicator.svelte', () => ({
  default: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeJobDetail: vi.fn(),
}));

import SessionDetail from '../src/components/orchestrator/SessionDetail.svelte';
import { routeJobDetail } from '../src/stores/router.svelte.ts';
import { sessionStore } from '../src/stores/sessionStore.svelte.ts';

// Convenient local references derived after all mocks are set up.
const activeSession = sessions.active;
const closedSession = sessions.closed;

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SessionDetail — active session', () => {
  it('renders truncated session ID in the header', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByText('sess-aaa-bbb-ccc-111'.slice(0, 16) + '…')).toBeInTheDocument();
  });

  it('renders the backend chip', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByText('claude')).toBeInTheDocument();
  });

  it('renders the active status badge', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByText('active')).toBeInTheDocument();
  });

  it('shows Prompt action button for active session', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByRole('button', { name: /send prompt to session/i })).toBeInTheDocument();
  });

  it('shows Close action button for active session', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByRole('button', { name: /close session/i })).toBeInTheDocument();
  });

  it('Close button opens the confirm dialog', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    const closeBtn = screen.getByRole('button', { name: /close session/i });
    await fireEvent.click(closeBtn);
    expect(screen.getByText('Close session?')).toBeInTheDocument();
  });

  it('confirming close reflects the closed state immediately (optimistic)', async () => {
    const { container } = render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /^close session$/i }));
    // The confirm button lives inside a native <dialog>; jsdom hides a
    // non-showModal()'d dialog's contents from the a11y tree (daisyUI uses a
    // CSS .modal-open, visible in a real browser), so query it from the DOM.
    const confirmBtn = container.querySelector(
      'button[aria-label="Confirm close session"]',
    ) as HTMLButtonElement | null;
    expect(confirmBtn).not.toBeNull();
    await fireEvent.click(confirmBtn!);
    // The closed sentinel text must appear immediately, without any SSE echo.
    const closed = await screen.findByText(/This session is closed/i);
    expect(closed).toBeInTheDocument();
  });

  it('confirm dialog has Keep Open and Close Session buttons', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    const closeBtn = screen.getByRole('button', { name: /close session/i });
    await fireEvent.click(closeBtn);
    // The dialog heading is visible.
    expect(screen.getByText('Close session?')).toBeInTheDocument();
    // Both dialog action buttons are present (identified by text content).
    expect(screen.getByText('Keep Open')).toBeInTheDocument();
    expect(screen.getByText('Close Session')).toBeInTheDocument();
  });

  it('Keep Open button dismisses the confirm dialog', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    const closeBtn = screen.getByRole('button', { name: /close session/i });
    await fireEvent.click(closeBtn);
    expect(screen.getByText('Close session?')).toBeInTheDocument();
    // Click "Keep Open" button.
    await fireEvent.click(screen.getByText('Keep Open'));
    expect(screen.queryByText('Close session?')).toBeNull();
  });

  it('renders the metadata panel with Session Info label', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByText('Session Info')).toBeInTheDocument();
  });

  it('renders tenant information in the metadata panel', () => {
    render(SessionDetail, { props: { session: activeSession } });
    // "default" appears in the metadata aside.
    const tenantEls = screen.getAllByText('default');
    expect(tenantEls.length).toBeGreaterThan(0);
  });

  it('Prompt modal opens when Prompt button is clicked', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    const promptBtn = screen.getByRole('button', { name: /send prompt to session/i });
    await fireEvent.click(promptBtn);
    // The modal heading should appear.
    expect(screen.getByText('Send Prompt')).toBeInTheDocument();
  });

  it('Prompt modal Cancel button closes the modal', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    const promptBtn = screen.getByRole('button', { name: /send prompt to session/i });
    await fireEvent.click(promptBtn);
    expect(screen.getByText('Send Prompt')).toBeInTheDocument();
    // Click the Cancel button inside the modal.
    await fireEvent.click(screen.getByText('Cancel'));
    expect(screen.queryByText('Send Prompt')).toBeNull();
  });
});

describe('SessionDetail — closed session', () => {
  it('does not show Prompt or Close action buttons for a closed session', () => {
    render(SessionDetail, { props: { session: closedSession } });
    expect(screen.queryByRole('button', { name: /send prompt to session/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /close session/i })).toBeNull();
  });

  it('does not show the Rotate Account button for a closed session', () => {
    render(SessionDetail, { props: { session: closedSession } });
    expect(screen.queryByRole('button', { name: /rotate session account/i })).toBeNull();
  });

  it('shows "This session is closed" message at the bottom', () => {
    render(SessionDetail, { props: { session: closedSession } });
    expect(screen.getByText(/This session is closed/i)).toBeInTheDocument();
  });
});

describe('SessionDetail — rotate account', () => {
  beforeEach(() => {
    mockRotateSessionAccount.mockReset();
  });

  it('shows the Rotate Account button for an active session', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByRole('button', { name: /rotate session account/i })).toBeInTheDocument();
  });

  it('opens the rotate dialog with a resume-mode warning, without calling the API (confirm-gated)', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    expect(screen.getByText('Rotate Account', { selector: 'h3' })).toBeInTheDocument();
    expect(screen.getByText(/restarts/i)).toBeInTheDocument();
    expect(screen.getByText(/mode: resume/i)).toBeInTheDocument();
    expect(mockRotateSessionAccount).not.toHaveBeenCalled();
  });

  it('Cancel closes the dialog without calling the API', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    await fireEvent.click(screen.getByText('Cancel'));
    expect(screen.queryByText('Rotate Account', { selector: 'h3' })).toBeNull();
    expect(mockRotateSessionAccount).not.toHaveBeenCalled();
  });

  it('confirming calls rotateSessionAccount with the session ID and no optional fields by default', async () => {
    mockRotateSessionAccount.mockResolvedValue({
      session_id: activeSession.session_id,
      previous_account_id: 'acct-a',
      account_id: 'acct-b',
      strategy: 'least_usage',
      mode: 'resume',
    });
    const { container } = render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    const confirmBtn = container.querySelector(
      'button[aria-label="Confirm rotate account"]',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);
    expect(mockRotateSessionAccount).toHaveBeenCalledWith({
      session_id: activeSession.session_id,
      reason: undefined,
      hold_outgoing: undefined,
      available_at: undefined,
    });
  });

  it('sends a trimmed reason and hold_outgoing when provided', async () => {
    mockRotateSessionAccount.mockResolvedValue({
      session_id: activeSession.session_id,
      account_id: 'acct-b',
      mode: 'in_place',
    });
    const { container } = render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    const reasonInput = screen.getByLabelText(/rotation reason/i) as HTMLInputElement;
    await fireEvent.input(reasonInput, { target: { value: '  seat retired  ' } });
    const holdCheckbox = screen.getByLabelText(/hold outgoing account/i) as HTMLInputElement;
    await fireEvent.click(holdCheckbox);
    const confirmBtn = container.querySelector(
      'button[aria-label="Confirm rotate account"]',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);
    expect(mockRotateSessionAccount).toHaveBeenCalledWith(
      expect.objectContaining({
        session_id: activeSession.session_id,
        reason: 'seat retired',
        hold_outgoing: true,
      }),
    );
  });

  it('reveals the "available at" input only when the hold checkbox is checked', async () => {
    render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    expect(screen.queryByLabelText(/outgoing account available at/i)).toBeNull();
    const holdCheckbox = screen.getByLabelText(/hold outgoing account/i) as HTMLInputElement;
    await fireEvent.click(holdCheckbox);
    expect(screen.getByLabelText(/outgoing account available at/i)).toBeInTheDocument();
  });

  it('shows the previous -> new account and mode after a successful rotation', async () => {
    mockRotateSessionAccount.mockResolvedValue({
      session_id: activeSession.session_id,
      previous_account_id: 'acct-a',
      account_id: 'acct-b',
      strategy: 'least_usage',
      mode: 'resume',
    });
    const { container } = render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    const confirmBtn = container.querySelector(
      'button[aria-label="Confirm rotate account"]',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);
    const result = await screen.findByTestId('rotate-result');
    expect(result.textContent).toContain('acct-a');
    expect(result.textContent).toContain('acct-b');
    expect(result.textContent).toContain('resume');
  });

  it('shows a graceful error when rotation fails (e.g. SessionNotRotatable) and keeps the dialog open', async () => {
    mockRotateSessionAccount.mockRejectedValue(
      new Error('{"error":"session is not rotatable: pinned to a single account"}'),
    );
    const { container } = render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    const confirmBtn = container.querySelector(
      'button[aria-label="Confirm rotate account"]',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);
    // The alert lives inside a native <dialog> that jsdom hides from the a11y
    // tree when it isn't opened via showModal() (same caveat as the
    // close-confirm dialog above), so query it from the DOM directly.
    const error = await waitFor(() => {
      const el = container.querySelector('[role="alert"]');
      if (!el) throw new Error('alert not rendered yet');
      return el;
    });
    expect(error.textContent).toMatch(/not rotatable/i);
    // The dialog stays open — the operator can adjust and retry rather than
    // losing their reason/hold selections to a closed dialog.
    expect(screen.getByText('Rotate Account', { selector: 'h3' })).toBeInTheDocument();
  });

  it('resets the form when the dialog is reopened after a previous rotation', async () => {
    mockRotateSessionAccount.mockResolvedValue({
      session_id: activeSession.session_id,
      account_id: 'acct-b',
      mode: 'in_place',
    });
    const { container } = render(SessionDetail, { props: { session: activeSession } });
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    const confirmBtn = container.querySelector(
      'button[aria-label="Confirm rotate account"]',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);
    await screen.findByTestId('rotate-result');
    await fireEvent.click(screen.getByText('Done'));

    // Reopen — the result panel from the previous rotation must not reappear.
    await fireEvent.click(screen.getByRole('button', { name: /rotate session account/i }));
    expect(screen.queryByTestId('rotate-result')).toBeNull();
    expect(screen.getByLabelText(/rotation reason/i)).toHaveValue('');
  });
});

// ---------------------------------------------------------------------------
// The job the session works for (go-quicbaas#580)
// ---------------------------------------------------------------------------

describe('SessionDetail — job binding', () => {
  beforeEach(() => {
    vi.mocked(routeJobDetail).mockClear();
  });

  it('links the metadata panel to the job the session works for', async () => {
    render(SessionDetail, {
      props: { session: { ...activeSession, job_id: 'job-abc-def-123' } },
    });
    const links = screen.getAllByRole('link', { name: /job-abc-def-123/ });
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) {
      expect(link.getAttribute('href')).toBe('#/jobs/job-abc-def-123');
    }
    await fireEvent.click(links[0]);
    expect(vi.mocked(routeJobDetail)).toHaveBeenCalledWith('job-abc-def-123');
  });

  it('shows no Job row for a session that belongs to no job', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.queryByText('Job')).toBeNull();
    expect(screen.queryByRole('link', { name: /^job-/ })).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Conversation vs event log (go-quicbaas#834)
// ---------------------------------------------------------------------------

/**
 * The page's body used to be the raw event log and nothing else, so a session
 * holding a four-turn conversation rendered as an empty timeline. These state
 * what the body shows now: the conversation, without the operator having to
 * find it, and the event log still one click away.
 */
describe('SessionDetail — transcript views', () => {
  const turns = [
    {
      sequence: 1,
      session_id: activeSession.session_id,
      role: 'user' as const,
      timestamp: '2026-09-07T12:00:00Z',
      text: 'implement the thing',
    },
    {
      sequence: 2,
      session_id: activeSession.session_id,
      role: 'assistant' as const,
      timestamp: '2026-09-07T12:01:00Z',
      text: 'Done — here is what changed.',
    },
  ];

  // The double is a plain object, so a case sets what it wants to be true and
  // puts it back afterwards rather than leaking into the next file.
  beforeEach(() => {
    (sessionStore as { conversation: unknown[] }).conversation = turns;
  });

  afterEach(() => {
    (sessionStore as { conversation: unknown[] }).conversation = [];
  });

  it('shows the conversation without the operator selecting anything', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByTestId('session-conversation')).toBeInTheDocument();
    expect(screen.getByText('implement the thing')).toBeInTheDocument();
    expect(screen.getByText('Done — here is what changed.')).toBeInTheDocument();
  });

  it('marks the conversation tab as the selected one', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByRole('tab', { name: /Conversation/i })).toHaveAttribute(
      'aria-selected',
      'true',
    );
  });

  it('keeps the raw event log one click away', async () => {
    render(SessionDetail, { props: { session: activeSession } });

    await fireEvent.click(screen.getByRole('tab', { name: /Event Log/i }));

    expect(screen.queryByTestId('session-conversation')).toBeNull();
    expect(screen.getByTestId('event-stream-stub')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Event Log/i })).toHaveAttribute(
      'aria-selected',
      'true',
    );
  });

  it('returns to the conversation when the operator comes back', async () => {
    render(SessionDetail, { props: { session: activeSession } });

    await fireEvent.click(screen.getByRole('tab', { name: /Event Log/i }));
    await fireEvent.click(screen.getByRole('tab', { name: /Conversation/i }));

    expect(screen.getByTestId('session-conversation')).toBeInTheDocument();
  });

  it('says a closed session with nothing in it closed without a conversation', () => {
    (sessionStore as { conversation: unknown[] }).conversation = [];

    render(SessionDetail, { props: { session: closedSession } });

    expect(screen.getByTestId('conversation-empty')).toHaveTextContent(
      /closed without a conversation/i,
    );
  });

  it('counts the loaded turns in the metadata panel', () => {
    render(SessionDetail, { props: { session: activeSession } });
    expect(screen.getByText('Turns')).toBeInTheDocument();
    expect(screen.getByText('2 loaded')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Last Active, from the payload the server actually sends (go-quicbaas#855)
// ---------------------------------------------------------------------------
//
// These cases do NOT hand the component a hand-written fixture. Every other
// case in this file does, and that is exactly why the defect survived: a
// fixture written in the SDK's own field names asserts the component against
// itself, and the component was right — the SDK was not.
//
// So the session here is built by the REAL SDK client from the REAL wire body
// (orchestration.SessionInfo, which names the fields `state` and
// `last_activity`), and the component renders whatever that produces. The
// browser path and the desktop path now agree, which they did not: the Wails
// binding renamed both fields and the REST client renamed neither, so the
// identical component showed the time on the desktop and an em-dash in a tab.

import { createClient } from '@quicbaas/sdk';
import type { SessionStatusResponse } from '@quicbaas/sdk';

/** One session, as the REST service writes it. Unix nanoseconds, both fields. */
const WIRE_CREATED_AT = 1789185149_000000000;
const WIRE_LAST_ACTIVITY = 1789185263_664211875;

/**
 * sessionFromServer asks the real SDK client for the tenant's sessions with a
 * stubbed transport, and returns the one it produced. Whatever the SDK hands
 * a caller is what the page gets.
 */
async function sessionFromServer(
  overrides: Record<string, unknown> = {},
): Promise<SessionStatusResponse> {
  const body = {
    sessions: [{
      session_id: 'sess-bafb0b7d',
      project_id: 'proj-1',
      backend: 'claude',
      model: 'claude-3-5-sonnet',
      state: 'active',
      created_at: WIRE_CREATED_AT,
      last_activity: WIRE_LAST_ACTIVITY,
      ...overrides,
    }],
  };
  const originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }),
  ) as unknown as typeof fetch;
  try {
    const client = await createClient({ transport: 'rest', baseURL: 'http://localhost' });
    const resp = await client.orchestrator.listSessions({ tenantID: 'tenant-a' });
    const session = resp.sessions[0];
    if (session === undefined) throw new Error('the SDK returned no session');
    return session;
  } finally {
    globalThis.fetch = originalFetch;
  }
}

describe('SessionDetail — Last Active, from the server’s own payload', () => {
  // THE MEASURED DEFECT: all 57 live sessions carried a last_activity and the
  // panel showed an em-dash for every one of them.
  it('renders the activity instant rather than an em-dash', async () => {
    const session = await sessionFromServer();
    render(SessionDetail, { props: { session } });
    const cell = screen.getByTestId('session-last-active');
    expect(cell).not.toHaveTextContent('—');
    expect(cell.textContent?.trim()).not.toBe('');
  });

  // THE UNIT, which is the half that comes back if only the name is fixed.
  // The two instants are 114 seconds apart on the wire. Rendered, they must
  // land on the same day — a seconds value read as nanoseconds lands in 1970,
  // and the two dates would be 56 years apart while the panel looked fine.
  it('renders two instants of one session as the same moment, to the minute', async () => {
    const session = await sessionFromServer();
    render(SessionDetail, { props: { session } });
    const created = screen.getByTestId('session-created').textContent ?? '';
    const active = screen.getByTestId('session-last-active').textContent ?? '';

    const createdMs = Date.parse(created);
    const activeMs = Date.parse(active);
    expect(Number.isFinite(createdMs)).toBe(true);
    expect(Number.isFinite(activeMs)).toBe(true);
    // 114 seconds on the wire; anything beyond ten minutes means the two
    // fields were read in different units.
    expect(Math.abs(activeMs - createdMs)).toBeLessThan(10 * 60 * 1000);
  });

  // A session that has never been active says so with an em-dash. "Not yet
  // active" and "active at the epoch" are different claims.
  it('renders an em-dash for a session with no activity instant', async () => {
    const session = await sessionFromServer({ last_activity: 0 });
    render(SessionDetail, { props: { session } });
    expect(screen.getByTestId('session-last-active')).toHaveTextContent('—');
  });

  // The lifecycle field drifted the same way: the service names it `state`,
  // every SDK exposes it as `status`, and this client renamed neither.
  it('renders the lifecycle state the server reported', async () => {
    const session = await sessionFromServer({ state: 'closed' });
    render(SessionDetail, { props: { session } });
    expect(screen.getByText('closed')).toBeInTheDocument();
  });
});
