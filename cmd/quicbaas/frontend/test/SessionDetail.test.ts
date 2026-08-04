/**
 * SessionDetail.test.ts
 *
 * Verifies:
 * 1. Header renders session ID, backend chip, and status badge.
 * 2. "Prompt" and "Close" action buttons are present for an active session.
 * 3. Send Prompt button is absent when session is closed.
 * 4. Clicking "Close" opens the confirm dialog.
 * 5. Metadata panel renders tenant info.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

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
  },
  openSession: vi.fn().mockResolvedValue(undefined),
  closeActiveSession: vi.fn(),
  loadMoreHistory: vi.fn().mockResolvedValue(undefined),
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
    },
  }),
}));

// Stub EventStream (uses IntersectionObserver, complex DOM).
vi.mock('../src/components/orchestrator/EventStream.svelte', () => ({
  default: vi.fn(),
}));

// Stub PromptComposer.
vi.mock('../src/components/orchestrator/PromptComposer.svelte', () => ({
  default: vi.fn(),
}));

// Stub PresenceIndicator.
vi.mock('../src/components/orchestrator/PresenceIndicator.svelte', () => ({
  default: vi.fn(),
}));

import SessionDetail from '../src/components/orchestrator/SessionDetail.svelte';

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

  it('shows "This session is closed" message at the bottom', () => {
    render(SessionDetail, { props: { session: closedSession } });
    expect(screen.getByText(/This session is closed/i)).toBeInTheDocument();
  });
});
