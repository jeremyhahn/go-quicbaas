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
    job_id: 'job-worker',
  },
  {
    session_id: 'bbb-222-bbb-222-bbb-222',
    backend: 'ollama',
    status: 'closed',
    created_at: 1700000000,
    last_active: 1700000050,
    job_id: 'job-reviewer',
  },
];

/** The jobs behind the Role column: one driven by each runner. */
const mockJobs = [
  { job_id: 'job-worker', runner: 'sdlc' },
  { job_id: 'job-reviewer', runner: 'review' },
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

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    get jobs() { return mockJobs; },
    get activeTenantID() { return 'tenant-a'; },
  },
  loadJobs: vi.fn().mockResolvedValue(undefined),
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
    // The shared table says the filter emptied the page, not the tenant.
    expect(screen.getByTestId('data-table-no-match')).toBeInTheDocument();
  });

  it('cycles a sort header ascending, descending, off', async () => {
    render(SessionList);
    const sortBtn = screen.getByRole('button', { name: 'Sort by Backend' });
    await fireEvent.click(sortBtn);
    expect(sortBtn.textContent).toContain('↑');
    await fireEvent.click(sortBtn);
    expect(sortBtn.textContent).toContain('↓');
    await fireEvent.click(sortBtn);
    expect(sortBtn.textContent).not.toContain('↓');
    expect(sortBtn.textContent).not.toContain('↑');
  });

  it('pages the sessions through the shared table', () => {
    render(SessionList);
    expect(screen.getByTestId('data-table-range').textContent).toContain('1–2 of 2');
    expect(screen.getByLabelText('Rows per page')).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // The Role column (go-quicbaas#969)
  // -------------------------------------------------------------------------

  it('names the role each session played, from the runner of its job', () => {
    render(SessionList);
    const roles = screen.getAllByTestId('session-role').map((el) => el.textContent?.trim());
    expect(roles).toContain('worker');
    expect(roles).toContain('reviewer');
  });

  it('claims no role for a session whose job is not among the records held', () => {
    const orphan: SessionStatusResponse = {
      session_id: 'ccc-333-ccc-333-ccc-333',
      backend: 'codex',
      status: 'active',
      created_at: 1700000300,
      last_active: 1700000400,
      job_id: 'job-nobody-read',
    };
    mockSessions.push(orphan);
    try {
      render(SessionList);
      expect(screen.getAllByTestId('session-role')).toHaveLength(2);
    } finally {
      mockSessions.pop();
    }
  });
});
