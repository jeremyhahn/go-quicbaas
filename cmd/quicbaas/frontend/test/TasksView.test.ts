/**
 * TasksView.test.ts
 *
 * Regression guard for the TasksView component against the generic Task
 * model.
 *
 * Asserts:
 *   - The column set is Subject · Project · Status · Attempts · Assigned ·
 *     Job · Updated (Team and Created are gone).
 *   - A forge-issue task renders its `#N` chip linking to the source URL.
 *   - A blocked task carries a marker with its reason, or the explicit
 *     "no reason recorded" text.
 *   - Attempts render as `charged / budget (N dispatched)`.
 *   - The spawned job links to `#/jobs/{id}` — the dead `#/orchestrator/jobs/`
 *     href is the regression this pins.
 *   - `?project=` narrows the list and the empty state names the project.
 *   - Clicking a row opens the task detail route.
 *   - A failed load says why (a 403 names the missing permission).
 *
 * THE TABLE IS SERVER-PAGED. Its rows come from `listTasksPage`, and the jobs
 * behind the Assigned column from `listJobsPage` narrowed to the ids this page
 * names — so the doubles below serve pages out of the same `storeTasks` /
 * `storeJobs` fixtures the tests set, applying the narrowings the view claims
 * to send. A view that stopped sending one would fail here rather than quietly
 * filtering in the browser.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Hoisted mocks — declared before any src/ imports.
// ---------------------------------------------------------------------------

const { mockDecomposeTask, mockClaimTask, mockCompleteTask, mockShowToast, mockListProjects, routerStoreMock, mockRouteTaskDetail, mockRouteTasks } = vi.hoisted(() => ({
  mockDecomposeTask: vi.fn(),
  mockClaimTask: vi.fn(),
  mockCompleteTask: vi.fn(),
  mockShowToast: vi.fn(),
  mockListProjects: vi.fn(),
  routerStoreMock: { name: 'tasks', params: {} as Record<string, string>, hash: '#/tasks' },
  mockRouteTaskDetail: vi.fn(),
  mockRouteTasks: vi.fn(),
}));

/** The page requests the view sent, newest last. */
const tasksPageCalls: Array<Record<string, unknown>> = [];
const jobsPageCalls: Array<Record<string, unknown>> = [];

/** Set by a test that wants the tasks page to fail. */
let tasksPageError: unknown = null;

/**
 * The `facets` the tasks page answers with. `null` is a daemon that counts
 * nothing — the case the chips must still work in.
 */
let facetsAnswer: Record<string, Record<string, number>> | null = null;

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({
    orchestrator: {
      // The double applies the SERVER's narrowings — project and dispatch
      // mode — to the fixture, so a filter the view forgets to send shows up
      // as rows it should not have got.
      listTasksPage: async (_tenant: string, page: Record<string, unknown> = {}) => {
        tasksPageCalls.push(page);
        if (tasksPageError !== null) throw tasksPageError;
        const matched = (storeTasks as Array<Record<string, unknown>>).filter((task) => {
          if (page['project_id'] !== undefined && task['project_id'] !== page['project_id']) {
            return false;
          }
          if (page['auto_spawn'] !== undefined && Boolean(task['auto_spawn']) !== page['auto_spawn']) {
            return false;
          }
          const statuses = page['statuses'] as string[] | undefined;
          if (statuses !== undefined && !statuses.includes(task['status'] as string)) {
            return false;
          }
          return true;
        });
        return {
          items: matched,
          total: matched.length,
          limit: 25,
          offset: 0,
          ...(facetsAnswer === null ? {} : { facets: facetsAnswer }),
        };
      },
      listJobsPage: async (_tenant: string, page: Record<string, unknown> = {}) => {
        jobsPageCalls.push(page);
        const ids = (page['job_ids'] ?? []) as string[];
        const matched = (storeJobs as Array<Record<string, unknown>>).filter(
          (job) => ids.includes(job['job_id'] as string),
        );
        return { items: matched, total: matched.length, limit: ids.length, offset: 0 };
      },
      decomposeTask: mockDecomposeTask,
      claimTask: mockClaimTask,
      completeTask: mockCompleteTask,
      // The agent picker and the backend list behind the shared reference
      // options; neither narrows this fixture, both must answer.
      listFleetNodesPage: async () => ({ items: [], total: 0, limit: 25, offset: 0 }),
      listBackends: async () => [],
    },
    projects: {
      listProjectsPage: mockListProjects,
    },
  })),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routerStore: routerStoreMock,
  routeTaskDetail: mockRouteTaskDetail,
  routeTasks: mockRouteTasks,
}));

// Provide a controllable orchestratorStore so tests can inject tasks.
let storeTasks: unknown[] = [];
let storeJobs: unknown[] = [];
let storeLoading = false;
let storeError: unknown = null;

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    get tasks() { return storeTasks; },
    get jobs() { return storeJobs; },
    get tasksLoading() { return storeLoading; },
    get tasksError() { return storeError; },
    activeTenantID: 'default',
  },
  loadJobs: vi.fn().mockResolvedValue(undefined),
  refresh: vi.fn().mockResolvedValue(undefined),
}));

// CreateTaskDialog imports from the SDK — stub it so the module graph stays
// simple. PageHeader, StatusBadge, and EmptyState have no SDK dependencies
// and are rendered normally so snippet content (filter buttons, empty state)
// appears in the DOM.
vi.mock('../src/components/orchestrator/CreateTaskDialog.svelte', () => ({
  default: vi.fn(),
}));

// Stub lucide icon components. jsdom has no SVG layout engine; rendering real
// icons causes no errors but we silence them to keep test output clean.
//
// THE MOCK IS PARTIAL ON PURPOSE. An exhaustive one breaks the moment a shared
// component this view renders — DataTable's sort arrows, FilterBar's chip
// dismissals — reaches for an icon nobody listed here, which is a failure
// about this file's bookkeeping and not about the view under test.
vi.mock('lucide-svelte', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  LayoutList: vi.fn(),
  Columns3: vi.fn(),
  Plus: vi.fn(),
  ClipboardList: vi.fn(),
  UserCheck: vi.fn(),
  CheckCircle: vi.fn(),
  Zap: vi.fn(),
  AlertTriangle: vi.fn(),
  X: vi.fn(),
}));

import TasksView from '../src/views/orchestrator/TasksView.svelte';
import { forgetReferenceOptions } from '../src/stores/referenceOptions.svelte.ts';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeTask(overrides: Record<string, unknown> = {}) {
  return {
    id: 'task-001',
    subject: 'Test task',
    description: '',
    team_name: 'eng',
    status: 'pending',
    assigned_to: '',
    spawned_job_id: '',
    auto_spawn: false,
    created_at: 0,
    updated_at: 0,
    ...overrides,
  };
}

function resetStores(): void {
  vi.clearAllMocks();
  storeTasks = [];
  storeJobs = [];
  storeLoading = false;
  storeError = null;
  tasksPageError = null;
  facetsAnswer = null;
  tasksPageCalls.length = 0;
  jobsPageCalls.length = 0;
  window.location.hash = '#/tasks';
  routerStoreMock.name = 'tasks';
  routerStoreMock.params = {};
  routerStoreMock.hash = '#/tasks';
  mockListProjects.mockResolvedValue({
    items: [{ id: 'proj-1', name: 'go-truststrap' }],
    total: 1,
    limit: 25,
    offset: 0,
  });
  // The reference options are cached per tenant across the whole module; a
  // test that changes what the project read answers must start from nothing.
  forgetReferenceOptions();
}

/**
 * renderTasks renders the view and waits for its first page to arrive. The
 * table reads from the server, so nothing is on screen until it answers.
 */
async function renderTasks(): Promise<ReturnType<typeof render>> {
  const result = render(TasksView);
  await waitFor(() => { expect(tasksPageCalls.length).toBeGreaterThan(0); });
  await waitFor(() => {
    expect(document.querySelector('[data-testid="data-table"] tbody')).not.toBeNull();
  });
  return result;
}

/** The parameters of the most recent tasks page request. */
function lastTasksRequest(): Record<string, unknown> {
  return tasksPageCalls[tasksPageCalls.length - 1] ?? {};
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TasksView — columns', () => {
  beforeEach(resetStores);

  it('renders the generic Task column set in table layout', async () => {
    await renderTasks();
    for (const header of ['Subject', 'Project', 'Status', 'Attempts', 'Assigned', 'Job', 'Updated']) {
      expect(screen.getByRole('columnheader', { name: header })).toBeInTheDocument();
    }
    // Only the endpoint's own sort fields claim to sort.
    expect(screen.getByRole('button', { name: 'Sort by Subject' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sort by Attempts' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Sort by Assigned' })).toBeNull();
  });

  it('no longer renders the Team, Created, or Spawned Job columns', async () => {
    await renderTasks();
    expect(screen.queryByRole('columnheader', { name: 'Team' })).toBeNull();
    expect(screen.queryByRole('columnheader', { name: 'Created' })).toBeNull();
    expect(screen.queryByRole('columnheader', { name: 'Spawned Job' })).toBeNull();
  });

  it('keeps the column set when tasks are present', async () => {
    storeTasks = [makeTask()];
    await renderTasks();
    expect(screen.getByRole('columnheader', { name: 'Job' })).toBeInTheDocument();
  });
});

describe('TasksView — provenance and job link', () => {
  beforeEach(resetStores);

  it('links the forge issue chip to the source URL, opening on the forge', async () => {
    storeTasks = [makeTask({ source: { kind: 'forge_issue', ref: 'owner/repo#42', url: 'https://forge.example/owner/repo/issues/42' } })];
    await renderTasks();
    const chip = await waitFor(() => screen.getByTitle('Open issue #42 on the forge'));
    expect(chip).toHaveTextContent('#42');
    expect(chip).toHaveAttribute('href', 'https://forge.example/owner/repo/issues/42');
    expect(chip).toHaveAttribute('target', '_blank');
  });

  it('renders the issue number without a link when the source carries no URL', async () => {
    storeTasks = [makeTask({ source: { kind: 'forge_issue', ref: '7' } })];
    await renderTasks();
    const chip = await waitFor(() => screen.getByTitle('Forge issue #7'));
    expect(chip.tagName).toBe('SPAN');
  });

  it('renders no issue chip for a hand-created task', async () => {
    storeTasks = [makeTask({ source: { kind: 'operator', ref: 'alice' } })];
    await renderTasks();
    expect(screen.queryByTitle(/Forge issue|on the forge/)).toBeNull();
  });

  it('links the open attempt to #/jobs/{id}, not the dead #/orchestrator/jobs/ path', async () => {
    storeTasks = [makeTask({ spawned_job_id: 'job-abcdef1234567890' })];
    await renderTasks();
    const link = await waitFor(() => screen.getByTitle(/View job job-abcdef1234567890/i));
    expect(link).toHaveAttribute('href', '#/jobs/job-abcdef1234567890');
  });

  // THE DEFECT (go-quicbaas#856): a completed task has NO `spawned_job_id`.
  // recordAttemptHistory moves it into metadata.attempt_history and clears it
  // the moment the attempt concludes, so the column fell through to
  // `auto_spawn` and printed "no agent matched" for a task that had run a job
  // to completion — the operator's first hop from task to job, gone.
  it('links a completed task to the job that ran it, from the attempt history', async () => {
    storeTasks = [makeTask({
      status: 'completed',
      auto_spawn: true,
      spawned_job_id: '',
      attempts: 1,
      metadata: { attempt_history: '1f644ffba52203488392749b50573b87' },
    })];
    await renderTasks();
    const link = await waitFor(() => screen.getByTitle(/View job 1f644ffba52203488392749b50573b87/i));
    expect(link).toHaveAttribute('href', '#/jobs/1f644ffba52203488392749b50573b87');
    expect(screen.queryByText(/no agent matched/i)).toBeNull();
  });

  // A retried task names every attempt it ran; the column links the newest,
  // because that is the run whose outcome the status reflects.
  it('links the newest attempt when a task ran several', async () => {
    storeTasks = [makeTask({
      status: 'failed',
      auto_spawn: true,
      spawned_job_id: '',
      attempts: 3,
      metadata: { attempt_history: 'job-one,job-two,job-three' },
    })];
    await renderTasks();
    expect(await waitFor(() => screen.getByTitle(/View job job-three/i)))
      .toHaveAttribute('href', '#/jobs/job-three');
    expect(screen.queryByTitle(/View job job-one/i)).toBeNull();
  });

  // "No agent matched" is a claim about DISPATCH, and it stays for the one
  // task it is true of: auto-spawn requested, nothing ever started.
  it('shows "no agent matched" only for an auto-spawn task that has never dispatched', async () => {
    storeTasks = [makeTask({ auto_spawn: true, spawned_job_id: '' })];
    await renderTasks();
    expect(await waitFor(() => screen.getByText(/no agent matched/i))).toBeInTheDocument();
  });

  // A task whose attempts were charged but whose history the page cannot read
  // must not be described as having matched no agent either.
  it('does not claim "no agent matched" for a task whose attempts were charged', async () => {
    storeTasks = [makeTask({ auto_spawn: true, spawned_job_id: '', attempts: 2 })];
    await renderTasks();
    expect(screen.queryByText(/no agent matched/i)).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// The Assigned column (go-quicbaas#856)
// ---------------------------------------------------------------------------
//
// `assigned_to` is the CLAIMANT. ClaimTask writes it; the dispatcher's own
// path, which is how every SDLC task is picked up, never does. So the column
// printed an em-dash — "nobody ran this" — for work that ran on a named fleet
// agent. The agent is on the JOB record, as `agent_id`.

describe('TasksView — the agent working the task', () => {
  beforeEach(resetStores);

  it('names the claimant when the task has one', async () => {
    storeTasks = [makeTask({ assigned_to: 'alice' })];
    await renderTasks();
    expect(await waitFor(() => screen.getByTestId('task-assigned'))).toHaveTextContent('alice');
  });

  // The SERVER resolves the latest attempt's agent onto every listed task as
  // `agent_id`, so the page names it straight off the row and asks for no job
  // listing of its own. The follow-up job request this replaced cost the page
  // a second whole-tenant job scan on the server.
  it('names the fleet node the server resolved onto the row, without a job request', async () => {
    storeTasks = [makeTask({
      status: 'completed',
      auto_spawn: true,
      assigned_to: '',
      spawned_job_id: '',
      attempts: 1,
      metadata: { attempt_history: 'job-abc' },
      agent_id: 'agent-automatethethings-agent-04',
    })];
    await renderTasks();
    await waitFor(() => {
      expect(screen.getByTestId('task-assigned'))
        .toHaveTextContent('agent-automatethethings-agent-04');
    });
    expect(jobsPageCalls).toHaveLength(0);
  });

  // The LATEST attempt's agent, not an earlier one's: a task retried onto a
  // second host must not still read as running on the first, even when the
  // store happens to hold the first attempt's job.
  it('names the latest attempt\'s agent, not an earlier attempt\'s', async () => {
    storeTasks = [makeTask({
      status: 'failed',
      auto_spawn: true,
      assigned_to: '',
      spawned_job_id: '',
      attempts: 2,
      metadata: { attempt_history: 'job-one,job-two' },
      agent_id: 'agent-host-b',
    })];
    storeJobs = [{ job_id: 'job-one', agent_id: 'agent-host-a' }];
    await renderTasks();
    const cell = await waitFor(() => {
      const el = screen.getByTestId('task-assigned');
      expect(el).toHaveTextContent('agent-host-b');
      return el;
    });
    expect(cell).not.toHaveTextContent('agent-host-a');
  });

  // An em-dash is still the right answer when nothing names an agent: the
  // column must not invent one from a job record it does not hold.
  it('renders an em-dash when neither the task nor any job names an agent', async () => {
    storeTasks = [makeTask({ assigned_to: '', spawned_job_id: '' })];
    await renderTasks();
    expect(await waitFor(() => screen.getByTestId('task-assigned'))).toHaveTextContent('—');
  });

  it('renders an em-dash when the job record for the latest attempt is not held', async () => {
    storeTasks = [makeTask({
      status: 'completed',
      assigned_to: '',
      spawned_job_id: '',
      metadata: { attempt_history: 'job-unread' },
    })];
    storeJobs = [{ job_id: 'job-other', agent_id: 'agent-host-a' }];
    await renderTasks();
    expect(screen.getByTestId('task-assigned')).toHaveTextContent('—');
  });
});

describe('TasksView — status and attempts', () => {
  beforeEach(resetStores);

  it('marks a blocked task with its reason', async () => {
    storeTasks = [makeTask({ status: 'blocked', blocked_reason: 'needs an owner ruling on the schema' })];
    await renderTasks();
    const marker = await waitFor(() => screen.getByTestId('blocked-marker'));
    expect(marker).toHaveTextContent('needs an owner ruling on the schema');
    expect(marker).toHaveAttribute('title', 'needs an owner ruling on the schema');
  });

  it('says so when a blocked task has no reason recorded', async () => {
    storeTasks = [makeTask({ status: 'blocked' })];
    await renderTasks();
    expect(await waitFor(() => screen.getByTestId('blocked-marker')))
      .toHaveTextContent('No reason recorded.');
  });

  it('renders no blocked marker for a working task', async () => {
    storeTasks = [makeTask({ status: 'in_progress', blocked_reason: 'stale' })];
    await renderTasks();
    expect(screen.queryByTestId('blocked-marker')).toBeNull();
  });

  it('renders attempts as charged / budget with the dispatched count and a muted refund', async () => {
    storeTasks = [makeTask({ attempts: 3, attempt_budget: 5, environment_faults: 1 })];
    await renderTasks();
    expect(await waitFor(() => screen.getByText('2 / 5 (3 dispatched)'))).toBeInTheDocument();
    expect(screen.getByText(/1 refunded/)).toBeInTheDocument();
  });

  // go-quicbaas#947: the page must say WHOSE failure stopped a task from the
  // server's typed fields, never by reading result.output.
  it('renders a failure on an environment fault differently from a failure of the work', async () => {
    storeTasks = [
      makeTask({
        id: 'task-env', subject: 'Env task', status: 'blocked', block_kind: 'blocked',
        fault_class: 'forge_unreachable', fault_origin: 'environment', conclusion_reason: 'fault_bounded',
        result: { output: 'the same prose' },
      }),
      makeTask({
        id: 'task-work', subject: 'Work task', status: 'failed',
        fault_class: 'stalled', fault_origin: 'work', conclusion_reason: 'budget_exhausted',
        result: { output: 'the same prose' },
      }),
    ];
    await renderTasks();
    const envRow = await waitFor(() => screen.getByRole('button', { name: 'Open task Env task' }));
    const workRow = screen.getByRole('button', { name: 'Open task Work task' });
    const envStop = envRow.querySelector('[data-testid="task-stop"]');
    const workStop = workRow.querySelector('[data-testid="task-stop"]');
    expect(envStop).toHaveTextContent('Environment fault: forge_unreachable');
    expect(workStop).toHaveTextContent('Failed on the work');
    expect(envStop?.textContent).not.toEqual(workStop?.textContent);
  });

  it('names a task blocked for an owner ruling apart from one awaiting operator repair', async () => {
    storeTasks = [
      makeTask({ id: 'task-owner', subject: 'Owner task', status: 'blocked', block_kind: 'needs_info', conclusion_reason: 'parked_needs_info' }),
      makeTask({
        id: 'task-op', subject: 'Operator task', status: 'blocked', block_kind: 'blocked',
        block_fault_class: 'workspace_unfit', fault_class: 'workspace_unfit', fault_origin: 'environment',
      }),
    ];
    await renderTasks();
    const ownerRow = await waitFor(() => screen.getByRole('button', { name: 'Open task Owner task' }));
    const opRow = screen.getByRole('button', { name: 'Open task Operator task' });
    expect(ownerRow.querySelector('[data-testid="task-stop"]')).toHaveTextContent('Awaiting owner ruling');
    expect(opRow.querySelector('[data-testid="task-stop"]')).toHaveTextContent('Environment fault: workspace_unfit');
  });

  it('renders no stop badge for a task that is not stopped', async () => {
    storeTasks = [makeTask({ status: 'completed', fault_origin: '', conclusion_reason: 'completed' })];
    await renderTasks();
    await waitFor(() => screen.getByRole('button', { name: 'Open task Test task' }));
    expect(screen.queryByTestId('task-stop')).toBeNull();
  });

  it('renders an em-dash for a task with no attempt accounting', async () => {
    storeTasks = [makeTask()];
    await renderTasks();
    const row = await waitFor(() => screen.getByRole('button', { name: 'Open task Test task' }));
    expect(row.textContent).toContain('—');
  });
});

describe('TasksView — project filter', () => {
  beforeEach(resetStores);

  it('shows only the tasks of the project a link names, and says which', async () => {
    window.location.hash = '#/tasks?tasks_project=proj-1';
    storeTasks = [
      makeTask({ id: 't1', subject: 'In project', project_id: 'proj-1' }),
      makeTask({ id: 't2', subject: 'Elsewhere', project_id: 'proj-2' }),
    ];
    await renderTasks();
    expect(await waitFor(() => screen.getByText('In project'))).toBeInTheDocument();
    expect(screen.queryByText('Elsewhere')).toBeNull();
    // The narrowing is the SERVER's: the request names the project.
    expect(lastTasksRequest()['project_id']).toBe('proj-1');
    // And the operator can see what is narrowing the list, by name.
    expect(await screen.findByText(/Project: go-truststrap/)).toBeInTheDocument();
  });

  it('names the project in the empty state when the filter matches nothing', async () => {
    window.location.hash = '#/tasks?tasks_project=proj-1';
    storeTasks = [makeTask({ id: 't2', subject: 'Elsewhere', project_id: 'proj-2' })];
    await renderTasks();
    expect(
      await screen.findByText('No tasks in go-truststrap match the current filter.'),
    ).toBeInTheDocument();
  });

  it('clears the filter from the chip that shows it', async () => {
    window.location.hash = '#/tasks?tasks_project=proj-1';
    await renderTasks();
    await fireEvent.click(
      await screen.findByTestId('active-filter-remove-project-proj-1'),
    );
    await waitFor(() => { expect(lastTasksRequest()['project_id']).toBeUndefined(); });
    expect(window.location.hash).not.toContain('tasks_project');
  });

  it('falls back to the project id when the project list cannot be read', async () => {
    mockListProjects.mockRejectedValue(new Error('forbidden'));
    storeTasks = [makeTask({ project_id: 'proj-9' })];
    await renderTasks();
    expect(await screen.findByText('proj-9')).toBeInTheDocument();
  });
});

describe('TasksView — row click', () => {
  beforeEach(resetStores);

  it('opens the task detail route when a row is clicked', async () => {
    storeTasks = [makeTask({ id: 'task-xyz' })];
    await renderTasks();
    await fireEvent.click(
      await waitFor(() => screen.getByRole('button', { name: 'Open task Test task' })),
    );
    expect(mockRouteTaskDetail).toHaveBeenCalledWith('task-xyz');
  });

  it('does not open the detail when an action button inside the row is clicked', async () => {
    mockDecomposeTask.mockResolvedValue({ tasks: [] });
    storeTasks = [makeTask({ id: 'task-xyz' })];
    await renderTasks();
    await fireEvent.click(
      await waitFor(() => screen.getByRole('button', { name: 'Decompose task Test task' })),
    );
    expect(mockRouteTaskDetail).not.toHaveBeenCalled();
    expect(mockDecomposeTask).toHaveBeenCalled();
  });
});

describe('TasksView — faceted filters', () => {
  beforeEach(resetStores);

  it('offers a chip group per facet the listing narrows by', async () => {
    await renderTasks();
    expect(screen.getByTestId('filter-field-status')).toBeInTheDocument();
    expect(screen.getByTestId('filter-field-dispatch')).toBeInTheDocument();
    expect(screen.getByTestId('filter-field-project')).toBeInTheDocument();
    expect(screen.getByTestId('filter-field-node')).toBeInTheDocument();
  });

  // The dispatch mode is a SERVER parameter: a page of twenty-five narrowed in
  // the browser would hide matches and miscount the rest.
  it('asks the server for the dispatch mode rather than narrowing the page', async () => {
    storeTasks = [
      makeTask({ id: 'auto', subject: 'Dispatched', auto_spawn: true }),
      makeTask({ id: 'queued', subject: 'Waiting', auto_spawn: false }),
    ];
    await renderTasks();
    expect(lastTasksRequest()['auto_spawn']).toBeUndefined();

    await fireEvent.click(screen.getByTestId('filter-chip-dispatch-true'));
    await waitFor(() => { expect(lastTasksRequest()['auto_spawn']).toBe(true); });
    await waitFor(() => { expect(screen.queryByText('Waiting')).toBeNull(); });

    await fireEvent.click(screen.getByTestId('filter-chip-dispatch-false'));
    await waitFor(() => { expect(lastTasksRequest()['auto_spawn']).toBe(false); });

    await fireEvent.click(screen.getByTestId('filter-clear-dispatch'));
    await waitFor(() => { expect(lastTasksRequest()['auto_spawn']).toBeUndefined(); });
  });

  // Status is multi-valued because the endpoint takes `statuses` and
  // "pending or blocked" is one question, not two lists to reconcile by eye.
  it('sends several statuses as one narrowing', async () => {
    storeTasks = [
      makeTask({ id: 'p', subject: 'Waiting', status: 'pending' }),
      makeTask({ id: 'b', subject: 'Parked', status: 'blocked' }),
      makeTask({ id: 'd', subject: 'Done', status: 'completed' }),
    ];
    await renderTasks();

    await fireEvent.click(screen.getByTestId('filter-chip-status-pending'));
    await fireEvent.click(screen.getByTestId('filter-chip-status-blocked'));
    await waitFor(() => {
      expect(lastTasksRequest()['statuses']).toEqual(['pending', 'blocked']);
    });
    await waitFor(() => { expect(screen.queryByText('Done')).toBeNull(); });
  });

  it('puts the selection in the hash and reads it back from a link', async () => {
    await renderTasks();
    await fireEvent.click(screen.getByTestId('filter-chip-status-blocked'));
    await waitFor(() => {
      expect(window.location.hash).toContain('tasks_status=blocked');
    });
  });

  it('counts each value from the facets the server answered with', async () => {
    facetsAnswer = { status: { pending: 7, blocked: 2 } };
    await renderTasks();
    expect(await screen.findByTestId('filter-count-status-pending')).toHaveTextContent('7');
    expect(screen.getByTestId('filter-count-status-blocked')).toHaveTextContent('2');
    // A declared value the server counted none of reads 0 rather than
    // vanishing: "no failed tasks" is the answer, not a missing question.
    expect(screen.getByTestId('filter-count-status-failed')).toHaveTextContent('0');
  });

  it('offers the vocabulary uncounted when the server counts nothing', async () => {
    facetsAnswer = null;
    await renderTasks();
    expect(screen.getByTestId('filter-chip-status-pending')).toBeInTheDocument();
    expect(screen.queryByTestId('filter-count-status-pending')).toBeNull();
  });
});

describe('TasksView — empty and error states', () => {
  beforeEach(resetStores);

  it('renders the "No tasks tracked" empty-state when there are no tasks', async () => {
    await renderTasks();
    expect(await waitFor(() => screen.getByText(/No tasks tracked/i))).toBeInTheDocument();
  });

  it('names the missing permission when the list failed with a 403', async () => {
    // The TABLE's read is the one that failed, and it says which permission
    // the account lacks rather than reading as a transport hiccup.
    tasksPageError = { status: 403, message: 'forbidden' };
    render(TasksView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        'You do not have permission to view tasks: this account lacks orchestrator:status.',
      );
    });
    expect(screen.queryByText(/No tasks tracked/i)).toBeNull();
  });

  it('shows a spinner only while loading with nothing to show, never alongside rows', async () => {
    storeLoading = true;
    storeTasks = [makeTask()];
    const { container } = await renderTasks();
    await waitFor(() => { expect(screen.getByText('Test task')).toBeInTheDocument(); });
    expect(container.querySelector('.loading-spinner')).toBeNull();
  });
});
