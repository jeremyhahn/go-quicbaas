/**
 * SessionList.test.ts
 *
 * Verifies filter/sort behaviour and that clicking a row triggers navigation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SessionStatusResponse } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock sessions data.
// ---------------------------------------------------------------------------

const mockSessions: SessionStatusResponse[] = [
  {
    session_id: 'aaa-111-aaa-111-aaa-111',
    backend: 'claude',
    status: 'active',
    created_at: 1700000100,
    last_active: 1700000200,
  },
  {
    session_id: 'bbb-222-bbb-222-bbb-222',
    backend: 'ollama',
    status: 'closed',
    created_at: 1700000000,
    last_active: 1700000050,
  },
];

// ---------------------------------------------------------------------------
// Mock stores.
// `vi.mock` factories run before any variable declarations, so we cannot
// reference a const declared in the outer scope — use vi.fn() inline and
// retrieve the spy via vi.mocked() after import.
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: {
    get sessions() { return mockSessions; },
    get sessionsLoading() { return false; },
  },
  refreshSessions: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeSessionDetail: vi.fn(),
  routerStore: { route: { name: 'sessions', params: {}, hash: '#/sessions' } },
}));

import SessionList from '../src/components/orchestrator/SessionList.svelte';
import { routeSessionDetail } from '../src/stores/router.svelte.ts';

describe('SessionList', () => {
  beforeEach(() => {
    vi.mocked(routeSessionDetail).mockClear();
  });

  it('renders all sessions by default', () => {
    render(SessionList);
    expect(screen.getAllByRole('button', { name: /Open session/i })).toHaveLength(2);
  });

  it('renders the standard PageHeader with icon and subtitle (#30)', () => {
    const { container } = render(SessionList);
    const h1 = container.querySelector('h1');
    expect(h1).not.toBeNull();
    expect(h1!.textContent).toContain('Sessions');

    // PageHeader's primary-tinted icon tile.
    const tile = container.querySelector('div.bg-primary\\/10');
    expect(tile).not.toBeNull();
    expect(tile!.querySelector('svg')).not.toBeNull();

    // Subtitle paragraph.
    const subtitle = container.querySelector('p.text-base-content\\/60');
    expect(subtitle).not.toBeNull();
    expect(subtitle!.textContent ?? '').toMatch(/persistent backends/i);
  });

  it('uppercases column headers via the uppercase tracking-wider class (#30)', () => {
    const { container } = render(SessionList);
    const headerRow = container.querySelector('thead tr');
    expect(headerRow).not.toBeNull();
    expect(headerRow!.className).toContain('uppercase');
  });

  it('filters sessions by backend', async () => {
    render(SessionList);
    const select = screen.getByLabelText('Filter by backend') as HTMLSelectElement;
    select.value = 'claude';
    await fireEvent.change(select);
    expect(screen.getAllByRole('button', { name: /Open session/i })).toHaveLength(1);
  });

  it('filters sessions by status', async () => {
    render(SessionList);
    const select = screen.getByLabelText('Filter by status') as HTMLSelectElement;
    select.value = 'closed';
    await fireEvent.change(select);
    expect(screen.getAllByRole('button', { name: /Open session/i })).toHaveLength(1);
  });

  it('clicking a row calls routeSessionDetail with the session ID', async () => {
    render(SessionList);
    const rows = screen.getAllByRole('button', { name: /Open session/i });
    await fireEvent.click(rows[0]!);
    expect(vi.mocked(routeSessionDetail)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(routeSessionDetail)).toHaveBeenCalledWith(
      expect.stringContaining('aaa-111'),
    );
  });

  it('keyboard Enter on a row triggers navigation', async () => {
    render(SessionList);
    const rows = screen.getAllByRole('button', { name: /Open session/i });
    await fireEvent.keyDown(rows[0]!, { key: 'Enter' });
    expect(vi.mocked(routeSessionDetail)).toHaveBeenCalledTimes(1);
  });

  it('shows empty-state message when filter matches nothing', async () => {
    render(SessionList);
    // Filter backend=claude AND status=closed — no session matches both simultaneously.
    // (aaa-111 is claude+active, bbb-222 is ollama+closed)
    const backendSelect = screen.getByLabelText('Filter by backend') as HTMLSelectElement;
    backendSelect.value = 'claude';
    await fireEvent.change(backendSelect);

    const statusSelect = screen.getByLabelText('Filter by status') as HTMLSelectElement;
    statusSelect.value = 'closed';
    await fireEvent.change(statusSelect);

    expect(screen.queryAllByRole('button', { name: /Open session/i })).toHaveLength(0);
    expect(screen.getByText(/0 of 2 sessions/)).toBeInTheDocument();
  });

  it('clicking sort header twice cycles through asc and desc', async () => {
    render(SessionList);
    const sortBtn = screen.getByRole('button', { name: /Backend/i });
    // First click on Backend: field changes (from created_at), starts at desc (↓).
    await fireEvent.click(sortBtn);
    expect(sortBtn.textContent).toContain('↓');
    // Second click on Backend: same field, toggles to asc (↑).
    await fireEvent.click(sortBtn);
    expect(sortBtn.textContent).toContain('↑');
    // Third click on Backend: same field, back to desc (↓).
    await fireEvent.click(sortBtn);
    expect(sortBtn.textContent).toContain('↓');
  });
});
