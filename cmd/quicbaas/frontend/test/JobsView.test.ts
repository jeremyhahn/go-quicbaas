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
 * JobsView.test.ts
 *
 * A job started by the Software Factory is an attempt at a forge issue, and
 * the listing said nothing about which one (go-quicbaas#580). The Description
 * cell carries an `#N` chip for the issue behind the job, and the chip opens
 * the issue without also opening the job.
 *
 * The rows come from the SERVER now: the view hands its loader
 * {limit, offset, sort, q} and renders the page that comes back, so the tests
 * mock `listJobsPage` and also check what the view ASKS FOR — the state
 * filter, the allow-listed sort fields, and the re-read a job event triggers.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const { storeMock } = vi.hoisted(() => ({
  storeMock: {
    jobs: [] as unknown[],
    tasks: [] as unknown[],
    jobsLoading: false,
    activeTenantID: 'acme',
  },
}));

/** The page listJobsPage answers with; tests replace it before rendering. */
const { pageMock } = vi.hoisted(() => ({
  pageMock: { answer: { items: [] as unknown[], total: 0, limit: 25, offset: 0 } },
}));

const listJobsPage = vi.fn(async () => pageMock.answer);
const cancelJob = vi.fn().mockResolvedValue(undefined);

// jobEventRevision must be REACTIVE for the live re-read to be observable at
// all: a plain property would never wake the effect watching it. Runes live in
// `.svelte.ts`, hence the stub beside this file.
vi.mock('../src/stores/orchestrator.svelte.ts', async () => {
  const live = await import('./stubs/jobEventRevision.svelte.ts');
  return {
    orchestratorStore: {
      get jobs() { return storeMock.jobs; },
      get tasks() { return storeMock.tasks; },
      get jobsLoading() { return storeMock.jobsLoading; },
      get activeTenantID() { return storeMock.activeTenantID; },
      get jobEventRevision() { return live.jobEventRevision(); },
    },
    selectJob: vi.fn(),
    refresh: vi.fn(),
    isTerminalJobState: (state: string) => state === 'completed' || state === 'failed',
    ACTIVE_JOB_STATES: ['executing', 'reviewing'],
  };
});

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeJobDetail: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({
    orchestrator: {
      listJobsPage: (...args: unknown[]) => listJobsPage(...(args as [])),
      cancelJob: (...args: unknown[]) => cancelJob(...args),
      // The project and agent pickers name their options from these.
      listFleetNodesPage: async () => ({
        items: [{ id: 'agent-1', name: 'burnin-01' }],
        total: 1,
        limit: 25,
        offset: 0,
      }),
      listBackends: async () => [{ name: 'claude' }],
    },
    projects: {
      listProjectsPage: async () => ({
        items: [{ id: 'proj-1', name: 'go-truststrap' }],
        total: 1,
        limit: 25,
        offset: 0,
      }),
    },
  })),
}));

vi.mock('../src/components/orchestrator/StartJobDialog.svelte', () => ({
  default: vi.fn(),
}));

import JobsView from '../src/views/orchestrator/JobsView.svelte';
import { routeJobDetail } from '../src/stores/router.svelte.ts';
import { bumpJobEventRevision, resetJobEventRevision } from './stubs/jobEventRevision.svelte.ts';

const factoryJob = {
  job_id: 'job-abc-def-123',
  tenant_id: 'acme',
  state: 'executing',
  iteration: 2,
  task_count: 4,
  completed: 1,
  failed: 0,
  description: 'Wire the job event stream',
  started_at: 1700000000,
  updated_at: 1700001000,
  task_id: 'task-77',
};

const leadJob = {
  ...factoryJob,
  job_id: 'job-lead-999',
  description: 'An operator-started job',
  task_id: undefined,
};

const factoryTask = {
  id: 'task-77',
  tenant_id: 'acme',
  team_name: 'delivery',
  subject: 'Wire the job event stream',
  description: '',
  status: 'in_progress',
  created_at: 1700000000,
  updated_at: 1700000500,
  spawned_job_id: 'job-abc-def-123',
  source: {
    kind: 'forge_issue',
    ref: 'jeremyhahn/go-quicbaas#580',
    url: 'http://forge.example/jeremyhahn/go-quicbaas/issues/580',
  },
};

/** The page the daemon answers with for this test. */
function serving(jobs: unknown[]): void {
  pageMock.answer = { items: jobs, total: jobs.length, limit: 25, offset: 0 };
}

/** The parameters of the most recent page request. */
function lastRequest(): Record<string, unknown> {
  const calls = listJobsPage.mock.calls;
  return (calls[calls.length - 1]?.[1] ?? {}) as Record<string, unknown>;
}

/** Waits for the page's rows to be on screen. */
async function rendered(description: string): Promise<HTMLElement> {
  return waitFor(() => screen.getByText(description));
}

describe('JobsView — the issue behind a job', () => {
  beforeEach(() => {
    serving([factoryJob, leadJob]);
    storeMock.jobs = [];
    storeMock.tasks = [factoryTask];
    storeMock.jobsLoading = false;
    resetJobEventRevision();
    listJobsPage.mockClear();
    vi.mocked(routeJobDetail).mockClear();
    window.location.hash = '#/jobs';
  });

  it('chips the forge issue the job is an attempt at', async () => {
    render(JobsView);
    const chip = await waitFor(() => screen.getByRole('link', { name: '#580' }));
    expect(chip.getAttribute('href')).toBe('http://forge.example/jeremyhahn/go-quicbaas/issues/580');
    expect(chip.getAttribute('rel')).toContain('noopener');
  });

  // -------------------------------------------------------------------------
  // go-quicbaas#976: the row names its own issue
  // -------------------------------------------------------------------------

  it('chips the issue the ROW carries, with no tasks loaded at all', async () => {
    // This is page 2 of a server-paged listing: the store holds one page of
    // tasks and none of them is this job's. Before the server filled `source`
    // the chip blanked here, which is the whole defect.
    storeMock.tasks = [];
    serving([{ ...factoryJob, job_id: 'job-page-2', task_id: 'task-999', source: factoryTask.source }]);
    render(JobsView);
    const chip = await waitFor(() => screen.getByRole('link', { name: '#580' }));
    expect(chip.getAttribute('href')).toBe('http://forge.example/jeremyhahn/go-quicbaas/issues/580');
  });

  it("prefers the row's own source over a task page that disagrees", async () => {
    // The task in the store is stale or is another job's; the server's answer
    // for THIS row is the one that renders.
    storeMock.tasks = [{ ...factoryTask, source: { kind: 'forge_issue', ref: '#111', url: '' } }];
    serving([{ ...factoryJob, source: { kind: 'forge_issue', ref: 'jeremyhahn/go-quicbaas#222', url: '' } }]);
    render(JobsView);
    expect(await waitFor(() => screen.getByTitle('Forge issue #222'))).toBeInTheDocument();
    expect(screen.queryByTitle('Forge issue #111')).toBeNull();
  });

  it('shows no chip when neither the row nor the store names an issue', async () => {
    storeMock.tasks = [];
    serving([{ ...factoryJob, source: { kind: 'scheduler', ref: 'nightly', url: '' } }]);
    render(JobsView);
    await rendered('Wire the job event stream');
    expect(screen.queryByRole('link', { name: /^#\d+$/ })).toBeNull();
  });

  it('finds the issue from the task when the job carries no task_id', async () => {
    serving([{ ...factoryJob, task_id: undefined }]);
    render(JobsView);
    // The task still names the job it spawned, so the binding is readable.
    expect(await waitFor(() => screen.getByRole('link', { name: '#580' }))).toBeInTheDocument();
  });

  it('shows no chip for a job with no forge issue behind it', async () => {
    serving([leadJob]);
    storeMock.tasks = [];
    render(JobsView);
    await rendered('An operator-started job');
    expect(screen.queryByRole('link', { name: /^#\d+$/ })).toBeNull();
  });

  it('renders the chip as plain text when the source records no URL', async () => {
    storeMock.tasks = [{ ...factoryTask, source: { kind: 'forge_issue', ref: '580', url: '' } }];
    render(JobsView);
    expect(await waitFor(() => screen.getByTitle('Forge issue #580'))).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: '#580' })).toBeNull();
  });

  it('opening the issue does not also open the job', async () => {
    render(JobsView);
    await fireEvent.click(await waitFor(() => screen.getByRole('link', { name: '#580' })));
    expect(vi.mocked(routeJobDetail)).not.toHaveBeenCalled();
  });

  it('clicking the row still opens the job detail', async () => {
    render(JobsView);
    await fireEvent.click(await rendered('An operator-started job'));
    expect(vi.mocked(routeJobDetail)).toHaveBeenCalledWith('job-lead-999');
  });

  it('keeps the description beside the chip', async () => {
    render(JobsView);
    expect(await rendered('Wire the job event stream')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// What the page asks the server for
// ---------------------------------------------------------------------------

describe('JobsView — the page it asks for', () => {
  beforeEach(() => {
    serving([factoryJob, leadJob]);
    storeMock.jobs = [];
    storeMock.tasks = [factoryTask];
    resetJobEventRevision();
    listJobsPage.mockClear();
    window.location.hash = '#/jobs';
  });

  it('asks for the tenant page with the table default sort', async () => {
    render(JobsView);
    await rendered('An operator-started job');
    expect(listJobsPage.mock.calls[0]?.[0]).toBe('acme');
    expect(lastRequest()['limit']).toBe(25);
    expect(lastRequest()['offset']).toBe(0);
  });

  it('turns the state chips into the states the SERVER narrows on', async () => {
    render(JobsView);
    await rendered('An operator-started job');

    // Multi-select: two chips are ONE question — "executing or reviewing" —
    // which is what `statuses` takes and what an operator watching a fleet
    // actually asks.
    await fireEvent.click(screen.getByTestId('filter-chip-state-executing'));
    await waitFor(() => { expect(lastRequest()['statuses']).toEqual(['executing']); });

    await fireEvent.click(screen.getByTestId('filter-chip-state-reviewing'));
    await waitFor(() => {
      expect(lastRequest()['statuses']).toEqual(['executing', 'reviewing']);
    });

    // Clicking a lit chip switches it off with the control that switched it on.
    await fireEvent.click(screen.getByTestId('filter-chip-state-executing'));
    await waitFor(() => { expect(lastRequest()['statuses']).toEqual(['reviewing']); });

    // Clearing asks for no narrowing at all rather than for every state by name.
    await fireEvent.click(screen.getByTestId('filter-clear-all'));
    await waitFor(() => { expect(lastRequest()['statuses']).toBeUndefined(); });
  });

  it('carries the runner, project and agent pickers to the server', async () => {
    render(JobsView);
    await rendered('An operator-started job');

    // Three runners is a vocabulary you can read at a glance, so it is chips.
    await fireEvent.click(screen.getByTestId('filter-chip-runner-review'));
    await waitFor(() => { expect(lastRequest()['runner']).toBe('review'); });

    // A project is not an enumeration — a chip per project would be a
    // thousand chips — so it is a picker, labelled by name.
    const project = await screen.findByTestId('filter-select-project');
    await fireEvent.change(project, { target: { value: 'proj-1' } });
    await waitFor(() => { expect(lastRequest()['project_id']).toBe('proj-1'); });
  });

  it('puts the narrowing in the hash, so a filtered list is a link', async () => {
    render(JobsView);
    await rendered('An operator-started job');
    await fireEvent.click(screen.getByTestId('filter-chip-state-failed'));
    await waitFor(() => { expect(window.location.hash).toContain('jobs_state=failed'); });
  });

  it('opens narrowed when the link says so', async () => {
    window.location.hash = '#/jobs?jobs_state=executing,reviewing';
    render(JobsView);
    await waitFor(() => {
      expect(lastRequest()['statuses']).toEqual(['executing', 'reviewing']);
    });
  });

  it('sorts on the allow-listed fields and offers no sort on the others', async () => {
    render(JobsView);
    await rendered('An operator-started job');

    await fireEvent.click(screen.getByRole('button', { name: 'Sort by State' }));
    await waitFor(() => { expect(lastRequest()['sort']).toBe('state'); });

    // Iteration and Progress are not in the endpoint's allow-list, so their
    // headers do not pretend to sort one page's worth of rows.
    expect(screen.queryByRole('button', { name: 'Sort by Iteration' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Sort by Progress' })).toBeNull();
  });

  it('re-reads the page when the job event stream reports a change', async () => {
    vi.useFakeTimers();
    try {
      render(JobsView);
      await vi.advanceTimersByTimeAsync(10);
      const before = listJobsPage.mock.calls.length;

      // A burst from several running jobs costs ONE re-read, not three: each
      // event restarts the debounce and only the quiet at the end fires.
      bumpJobEventRevision();
      await vi.advanceTimersByTimeAsync(50);
      bumpJobEventRevision();
      await vi.advanceTimersByTimeAsync(50);
      bumpJobEventRevision();
      await vi.advanceTimersByTimeAsync(1000);

      expect(listJobsPage.mock.calls.length).toBe(before + 1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('surfaces a refused page instead of an empty table', async () => {
    listJobsPage.mockRejectedValueOnce(new Error('page: unknown sort field: iteration'));
    render(JobsView);
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('unknown sort field');
    });
  });
});
