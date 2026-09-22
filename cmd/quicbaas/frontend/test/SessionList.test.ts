/**
 * SessionList.test.ts
 *
 * The session list is a SERVER-PAGED table: it asks the daemon for one page
 * and renders exactly what came back. So the tests are about what it SENDS —
 * the page, the sort, the search, the two filters — and about what it does
 * with the answer, rather than about narrowing rows in the browser.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SessionStatusResponse } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// The page the daemon answers with. `runner` is the ROLE the session played;
// the listing resolves it server-side, per page (go-quicbaas#969).
// ---------------------------------------------------------------------------

const pageSessions: SessionStatusResponse[] = [
  {
    session_id: 'aaa-111-aaa-111-aaa-111',
    backend: 'claude',
    status: 'active',
    created_at: 1700000100,
    last_active: 1700000200,
    job_id: 'job-worker',
    runner: 'sdlc',
  },
  {
    session_id: 'bbb-222-bbb-222-bbb-222',
    backend: 'ollama',
    status: 'closed',
    created_at: 1700000000,
    last_active: 1700000050,
    job_id: 'job-reviewer',
    runner: 'review',
  },
];

/** What listSessionsPage answers; a test may replace it before rendering. */
let answer: { items: SessionStatusResponse[]; total: number; limit: number; offset: number } = {
  items: pageSessions,
  total: 2,
  limit: 25,
  offset: 0,
};

const listSessionsPage = vi.fn(async () => answer);
const listBackends = vi.fn(async () => [{ name: 'claude' }, { name: 'ollama' }]);

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({
    orchestrator: {
      listSessionsPage: (...args: unknown[]) => listSessionsPage(...(args as [])),
      listBackends: () => listBackends(),
    },
  })),
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: {
    get sessions() { return pageSessions; },
    get sessionsLoading() { return false; },
  },
  refreshSessions: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    get activeTenantID() { return 'tenant-a'; },
  },
  // The spawn dialog this view lazily imports reaches the agent store, which
  // reads the scope helper. Mocking it keeps the dialog's import from
  // exploding once the async tests let it resolve.
  tenantScope: () => 'tenant-a',
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeSessionDetail: vi.fn(),
  routerStore: { route: { name: 'sessions', params: {}, hash: '#/sessions' } },
}));

import SessionList from '../src/components/orchestrator/SessionList.svelte';
import { routeSessionDetail } from '../src/stores/router.svelte.ts';
import { refreshSessions } from '../src/stores/sessionStore.svelte.ts';

/** The most recent page request the view sent. */
function lastRequest(): Record<string, unknown> {
  const calls = listSessionsPage.mock.calls;
  return (calls[calls.length - 1]?.[1] ?? {}) as Record<string, unknown>;
}

/**
 * chooseFilter picks one option of a filter selector, once that option EXISTS.
 * The backend options arrive from the daemon's registry a tick after the first
 * render, and assigning a value a <select> does not carry yet leaves it
 * silently unchanged — which would test nothing.
 */
async function chooseFilter(label: string, value: string): Promise<void> {
  const select = await waitFor(() => {
    const el = screen.getByLabelText(label) as HTMLSelectElement;
    expect([...el.options].map((o) => o.value)).toContain(value);
    return el;
  });
  select.value = value;
  await fireEvent.change(select);
}

/** Waits for the rows of the current answer to be on screen. */
async function rendered(count: number): Promise<HTMLElement[]> {
  return waitFor(() => {
    const rows = screen.getAllByRole('button', { name: /Open session/i });
    expect(rows).toHaveLength(count);
    return rows;
  });
}

describe('SessionList', () => {
  beforeEach(() => {
    answer = { items: pageSessions, total: 2, limit: 25, offset: 0 };
    vi.mocked(routeSessionDetail).mockClear();
    vi.mocked(refreshSessions).mockClear();
    listSessionsPage.mockClear();
    listBackends.mockClear();
    window.location.hash = '#/sessions';
  });

  afterEach(() => {
    window.location.hash = '';
  });

  it('renders the page the server answered with', async () => {
    render(SessionList);
    await rendered(2);
    expect(listSessionsPage).toHaveBeenCalled();
    expect(listSessionsPage.mock.calls[0]?.[0]).toBe('tenant-a');
  });

  it('renders the standard PageHeader with icon and subtitle (#30)', () => {
    const { container } = render(SessionList);
    const h1 = container.querySelector('h1');
    expect(h1).not.toBeNull();
    expect(h1!.textContent).toContain('Sessions');

    const tile = container.querySelector('div.bg-primary\\/10');
    expect(tile).not.toBeNull();
    expect(tile!.querySelector('svg')).not.toBeNull();

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

  // -------------------------------------------------------------------------
  // What it asks for
  // -------------------------------------------------------------------------

  it('asks the server for the backend filter rather than narrowing the page', async () => {
    render(SessionList);
    await rendered(2);

    answer = { items: [pageSessions[0]!], total: 1, limit: 25, offset: 0 };
    await chooseFilter('Filter by backend', 'claude');

    await waitFor(() => { expect(lastRequest()['backend']).toBe('claude'); });
    await rendered(1);
  });

  it('asks the server for the status filter', async () => {
    render(SessionList);
    await rendered(2);

    answer = { items: [pageSessions[1]!], total: 1, limit: 25, offset: 0 };
    await chooseFilter('Filter by status', 'closed');

    await waitFor(() => { expect(lastRequest()['status']).toBe('closed'); });
  });

  it('offers the daemon registry backends and the service lifecycle states', async () => {
    render(SessionList);
    await waitFor(() => { expect(listBackends).toHaveBeenCalled(); });

    const backends = await waitFor(() => {
      const select = screen.getByLabelText('Filter by backend') as HTMLSelectElement;
      expect(select.options.length).toBe(3);
      return select;
    });
    // The options are NOT derived from the rows: a page with no codex session
    // must still be able to ask for codex.
    expect([...backends.options].map((o) => o.value)).toEqual(['', 'claude', 'ollama']);

    const states = screen.getByLabelText('Filter by status') as HTMLSelectElement;
    expect([...states.options].map((o) => o.value)).toEqual([
      '', 'active', 'idle', 'closed', 'error',
    ]);
  });

  it('sorts on the field names the endpoint orders by, not the column labels', async () => {
    render(SessionList);
    await rendered(2);

    await fireEvent.click(screen.getByRole('button', { name: 'Sort by Status' }));
    await waitFor(() => { expect(lastRequest()['sort']).toBe('state'); });

    await fireEvent.click(screen.getByRole('button', { name: 'Sort by Last Active' }));
    await waitFor(() => { expect(lastRequest()['sort']).toBe('last_activity'); });
  });

  it('does not offer a sort on a column the endpoint cannot order by', async () => {
    render(SessionList);
    await rendered(2);
    expect(screen.queryByRole('button', { name: 'Sort by Role' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Sort by Container' })).toBeNull();
  });

  it('re-asks the server when Refresh is pressed', async () => {
    render(SessionList);
    await rendered(2);
    const before = listSessionsPage.mock.calls.length;

    await fireEvent.click(screen.getByLabelText('Refresh sessions'));

    await waitFor(() => {
      expect(listSessionsPage.mock.calls.length).toBeGreaterThan(before);
    });
    // The store is refreshed too: the dashboard's live count reads it.
    expect(vi.mocked(refreshSessions)).toHaveBeenCalled();
  });

  // -------------------------------------------------------------------------
  // What it does with the answer
  // -------------------------------------------------------------------------

  it('reports the range from the envelope, not from the rows in hand', async () => {
    answer = { items: pageSessions, total: 54, limit: 25, offset: 0 };
    render(SessionList);
    await rendered(2);
    await waitFor(() => {
      expect(screen.getByTestId('data-table-range').textContent).toContain('1–2 of 54');
    });
  });

  it('says the filter emptied the page rather than the tenant', async () => {
    answer = { items: [], total: 0, limit: 25, offset: 0 };
    render(SessionList);
    await chooseFilter('Filter by backend', 'claude');

    await waitFor(() => {
      expect(screen.getByTestId('data-table-no-match')).toBeInTheDocument();
    });
  });

  it('surfaces a refused page instead of an empty table', async () => {
    listSessionsPage.mockRejectedValueOnce(new Error('page: unknown sort field: role'));
    render(SessionList);
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('unknown sort field');
    });
  });

  it('clicking a row calls routeSessionDetail with the session ID', async () => {
    render(SessionList);
    const rows = await rendered(2);
    await fireEvent.click(rows[0]!);
    expect(vi.mocked(routeSessionDetail)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(routeSessionDetail)).toHaveBeenCalledWith(
      expect.stringContaining('aaa-111'),
    );
  });

  it('keyboard Enter on a row triggers navigation', async () => {
    render(SessionList);
    const rows = await rendered(2);
    await fireEvent.keyDown(rows[0]!, { key: 'Enter' });
    expect(vi.mocked(routeSessionDetail)).toHaveBeenCalledTimes(1);
  });

  // -------------------------------------------------------------------------
  // The Role column (go-quicbaas#969)
  // -------------------------------------------------------------------------

  it('names the role each session played, from the runner on the row', async () => {
    render(SessionList);
    await rendered(2);
    const roles = screen.getAllByTestId('session-role').map((el) => el.textContent?.trim());
    expect(roles).toEqual(['worker', 'reviewer']);
  });

  it('claims no role when the listing resolved no runner', async () => {
    answer = {
      items: [
        ...pageSessions,
        {
          session_id: 'ccc-333-ccc-333-ccc-333',
          backend: 'codex',
          status: 'active',
          created_at: 1700000300,
          last_active: 1700000400,
          job_id: 'job-pruned',
        },
      ],
      total: 3,
      limit: 25,
      offset: 0,
    };
    render(SessionList);
    await rendered(3);
    // Two badges for three rows: the session whose job could not be read shows
    // none rather than a guess.
    expect(screen.getAllByTestId('session-role')).toHaveLength(2);
  });
});
