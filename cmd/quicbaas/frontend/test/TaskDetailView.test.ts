/**
 * TaskDetailView.test.ts
 *
 * Verifies the task detail page (#/tasks/:id) against the generic Task model:
 *   1. Budget renders as `charged / budget (N dispatched)` with the refund
 *      count and the deferral.
 *   2. The "Waiting on a person" box appears for blocked and failed tasks,
 *      with the reason or the explicit "No reason recorded." text, and not
 *      for a working task.
 *   3. The current job links through routeJobDetail; the task's attempts are
 *      read from the server narrowed to this task, not sieved out of the
 *      tenant-wide job list.
 *   4. Pull requests come from output_artifacts, and every empty panel says
 *      WHICH emptiness it means rather than one sentence for all of them.
 *   5. An unknown id renders "not found", and a failed load says why.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mocks — all vi.hoisted factories must be self-contained.
// ---------------------------------------------------------------------------

const { storeMock, routerStoreMock, mockRouteJobDetail, mockRouteTasks, mockLoadTasks, mockLoadTaskJobs, mockRefresh, mockListProjects } = vi.hoisted(() => ({
  storeMock: {
    tasks: [] as unknown[],
    jobs: [] as unknown[],
    taskJobs: [] as unknown[],
    taskJobsTaskID: '',
    taskJobsLoading: false,
    tasksLoading: false,
    tasksError: null as unknown,
    activeTenantID: 'default',
  },
  routerStoreMock: {
    name: 'task-detail',
    params: { id: 'task-1' } as Record<string, string>,
    hash: '#/tasks/task-1',
  },
  mockRouteJobDetail: vi.fn(),
  mockRouteTasks: vi.fn(),
  mockLoadTasks: vi.fn(),
  mockLoadTaskJobs: vi.fn(),
  mockRefresh: vi.fn(),
  mockListProjects: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
  loadTasks: mockLoadTasks,
  loadTaskJobs: mockLoadTaskJobs,
  refresh: mockRefresh,
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routerStore: routerStoreMock,
  routeJobDetail: mockRouteJobDetail,
  routeTasks: mockRouteTasks,
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    projects: { listProjects: mockListProjects },
  }),
}));

vi.mock('lucide-svelte', () => ({
  ChevronLeft: vi.fn(),
  ClipboardList: vi.fn(),
  RefreshCw: vi.fn(),
  AlertTriangle: vi.fn(),
  ExternalLink: vi.fn(),
  GitPullRequest: vi.fn(),
}));

import TaskDetailView from '../src/views/orchestrator/TaskDetailView.svelte';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeTask(overrides: Record<string, unknown> = {}) {
  return {
    id: 'task-1',
    tenant_id: 'default',
    team_name: '',
    subject: 'Add the widget',
    description: 'The issue body.',
    status: 'in_progress',
    assigned_to: 'agent-7',
    created_at: 1_778_017_609,
    updated_at: 1_778_017_700,
    ...overrides,
  };
}

function makeJob(overrides: Record<string, unknown> = {}) {
  return {
    job_id: 'job-current-0001',
    tenant_id: 'default',
    state: 'executing',
    iteration: 2,
    task_count: 1,
    completed: 0,
    failed: 0,
    description: 'Add the widget',
    started_at: 1_778_017_650,
    updated_at: 1_778_017_700,
    config: { tenant_id: 'default', description: 'Add the widget', task_id: 'task-1', attempt: 2 },
    ...overrides,
  };
}

function reset(): void {
  vi.clearAllMocks();
  storeMock.tasks = [];
  storeMock.jobs = [];
  storeMock.taskJobs = [];
  storeMock.taskJobsTaskID = 'task-1';
  storeMock.taskJobsLoading = false;
  storeMock.tasksLoading = false;
  storeMock.tasksError = null;
  routerStoreMock.params = { id: 'task-1' };
  mockLoadTasks.mockResolvedValue(undefined);
  mockLoadTaskJobs.mockResolvedValue(undefined);
  mockRefresh.mockResolvedValue(undefined);
  mockListProjects.mockResolvedValue({ projects: [{ id: 'proj-1', name: 'go-truststrap' }], total: 1 });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TaskDetailView — header and budget', () => {
  beforeEach(reset);

  it('renders the subject, the issue link, and the project name', async () => {
    storeMock.tasks = [makeTask({
      project_id: 'proj-1',
      source: { kind: 'forge_issue', ref: 'owner/repo#42', url: 'https://forge.example/owner/repo/issues/42' },
    })];
    render(TaskDetailView);
    expect(screen.getByText('Add the widget')).toBeInTheDocument();
    const issue = screen.getByTitle('Open issue #42 on the forge');
    expect(issue).toHaveAttribute('href', 'https://forge.example/owner/repo/issues/42');
    const project = await screen.findByText(/Project: go-truststrap/);
    expect(project).toHaveAttribute('href', '#/tasks?project=proj-1');
  });

  it('renders the budget with the dispatched count and refunds', () => {
    storeMock.tasks = [makeTask({ attempts: 3, attempt_budget: 5, environment_faults: 1, consecutive_environment_faults: 1 })];
    render(TaskDetailView);
    expect(screen.getByTestId('budget-label')).toHaveTextContent('2 / 5 (3 dispatched)');
    expect(screen.getByTestId('refunded')).toHaveTextContent('1');
    expect(screen.getByText('1 in a row')).toBeInTheDocument();
    expect(screen.getByText(/1 of 3 dispatched attempts were refunded/)).toBeInTheDocument();
  });

  it('renders em-dashes when the task carries no attempt accounting', () => {
    storeMock.tasks = [makeTask()];
    render(TaskDetailView);
    expect(screen.getByTestId('budget-label')).toHaveTextContent('—');
    expect(screen.getByTestId('refunded')).toHaveTextContent('—');
    expect(screen.getByTestId('retry-not-before')).toHaveTextContent('not deferred');
  });

  it('shows the deferral only when retry_not_before names an instant', () => {
    storeMock.tasks = [makeTask({ retry_not_before: 0 })];
    const first = render(TaskDetailView);
    expect(screen.getByTestId('retry-not-before')).toHaveTextContent('not deferred');
    first.unmount();

    storeMock.tasks = [makeTask({ retry_not_before: Date.now() * 1_000_000 + 60_000_000_000 })];
    render(TaskDetailView);
    expect(screen.getByTestId('retry-not-before')).not.toHaveTextContent('not deferred');
  });
});

describe('TaskDetailView — waiting on a person', () => {
  beforeEach(reset);

  it('shows the box with the recorded reason for a blocked task', () => {
    storeMock.tasks = [makeTask({ status: 'blocked', block_kind: 'needs_info', blocked_reason: 'Which schema version?' })];
    render(TaskDetailView);
    const box = screen.getByTestId('waiting-on-person');
    expect(box).toHaveTextContent('Waiting on a person');
    expect(box).toHaveTextContent('Which schema version?');
    expect(box).toHaveTextContent('Parked (needs info)');
  });

  it('says "No reason recorded." when a blocked task carries none', () => {
    storeMock.tasks = [makeTask({ status: 'blocked' })];
    render(TaskDetailView);
    expect(screen.getByTestId('waiting-on-person')).toHaveTextContent('No reason recorded.');
  });

  it('shows the box for a failed task and says it will not retry on its own', () => {
    storeMock.tasks = [makeTask({ status: 'failed' })];
    render(TaskDetailView);
    const box = screen.getByTestId('waiting-on-person');
    expect(box).toHaveTextContent('No reason recorded.');
    expect(box).toHaveTextContent('will not be retried on its own');
  });

  it('renders no box for a working task', () => {
    storeMock.tasks = [makeTask({ status: 'in_progress' })];
    render(TaskDetailView);
    expect(screen.queryByTestId('waiting-on-person')).toBeNull();
  });
});

describe('TaskDetailView — jobs', () => {
  beforeEach(reset);

  it('links the current job through routeJobDetail and lists this task\'s other attempts', async () => {
    storeMock.tasks = [makeTask({ spawned_job_id: 'job-current-0001' })];
    storeMock.taskJobs = [
      makeJob(),
      makeJob({ job_id: 'job-prior-0001', state: 'failed', attempt: 1, config: { tenant_id: 'default', description: '', task_id: 'task-1', attempt: 1 } }),
    ];
    render(TaskDetailView);
    await fireEvent.click(screen.getByRole('button', { name: 'Open job job-current-0001' }));
    expect(mockRouteJobDetail).toHaveBeenCalledWith('job-current-0001');

    expect(screen.getByRole('button', { name: 'Open job job-prio' })).toBeInTheDocument();
    expect(screen.getByText('Jobs (2)')).toBeInTheDocument();
  });

  it('asks the server for this task\'s attempts rather than sieving the tenant list', () => {
    storeMock.tasks = [makeTask()];
    // A job belonging to ANOTHER task sits in the tenant-wide list. The page
    // must not render it, and must not count it. go-quicbaas#822.
    storeMock.jobs = [makeJob({ job_id: 'job-other-0001', config: { tenant_id: 'default', description: '', task_id: 'task-2', attempt: 1 } })];
    render(TaskDetailView);
    expect(mockLoadTaskJobs).toHaveBeenCalledWith('task-1');
    expect(screen.getByText('Jobs (0)')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Open job job-othe' })).toBeNull();
  });

  it('says a pending task is waiting for an agent', () => {
    storeMock.tasks = [makeTask({ status: 'pending', auto_spawn: true, attempts: 0 })];
    render(TaskDetailView);
    expect(screen.getByTestId('jobs-empty')).toHaveTextContent(
      'Dispatch requested; no agent has matched this task yet.',
    );
  });

  it('says when no job has been dispatched and none was asked for', () => {
    storeMock.tasks = [makeTask({ status: 'pending', attempts: 0 })];
    render(TaskDetailView);
    expect(screen.getByTestId('jobs-empty')).toHaveTextContent(
      'No job has been dispatched for this task.',
    );
  });

  // THE MEASURED DEFECT. A failed task with three charged attempts rendered
  // "no agent has matched this task yet", which is a claim about a task that
  // has never dispatched. go-quicbaas#822.
  it('does not claim a failed task with three attempts was never dispatched', () => {
    storeMock.tasks = [makeTask({ status: 'failed', attempts: 3, attempt_budget: 3 })];
    render(TaskDetailView);
    const empty = screen.getByTestId('jobs-empty');
    expect(empty).not.toHaveTextContent('no agent has matched this task yet');
    expect(empty).toHaveTextContent('3 attempts ran for this task');
    expect(empty).toHaveTextContent('no job record for them could be read');
  });

  it('says a settled task never dispatched when no attempt was ever charged', () => {
    storeMock.tasks = [makeTask({ status: 'failed', attempts: 0 })];
    render(TaskDetailView);
    expect(screen.getByTestId('jobs-empty')).toHaveTextContent(
      'This task ended failed without a job ever being dispatched.',
    );
  });

  it('says it is still reading while the attempt history loads', () => {
    storeMock.tasks = [makeTask({ status: 'failed', attempts: 3 })];
    storeMock.taskJobsLoading = true;
    render(TaskDetailView);
    expect(screen.getByTestId('jobs-empty')).toHaveTextContent('Loading this task’s jobs…');
  });

  it('says when the spawned job has no record', () => {
    storeMock.tasks = [makeTask({ spawned_job_id: 'job-missing' })];
    render(TaskDetailView);
    expect(screen.getByText('no record read for this job')).toBeInTheDocument();
  });

  // THE DEFECT (go-quicbaas#856). `spawned_job_id` is the OPEN attempt, and
  // recordAttemptHistory clears it the instant an attempt concludes, moving
  // the job into metadata.attempt_history. A completed task therefore named
  // no job at all and this panel could not link the run that completed it.
  it('links the job a completed task ran, from the attempt history', async () => {
    storeMock.tasks = [makeTask({
      status: 'completed',
      spawned_job_id: '',
      attempts: 1,
      metadata: { attempt_history: '1f644ffba52203488392749b50573b87' },
    })];
    storeMock.taskJobs = [makeJob({ job_id: '1f644ffba52203488392749b50573b87', state: 'completed' })];
    render(TaskDetailView);

    const latest = screen.getByTestId('task-latest-job');
    expect(latest).toHaveTextContent('Latest');
    await fireEvent.click(screen.getByRole('button', { name: 'Open job 1f644ffba52203488392749b50573b87' }));
    expect(mockRouteJobDetail).toHaveBeenCalledWith('1f644ffba52203488392749b50573b87');
  });

  // "Current" is a claim that an attempt is still running. A settled task's
  // newest attempt is the "Latest" one, not the current one.
  it('calls the newest attempt Current only while one is open', () => {
    storeMock.tasks = [makeTask({ spawned_job_id: 'job-current-0001' })];
    storeMock.taskJobs = [makeJob()];
    render(TaskDetailView);
    expect(screen.getByTestId('task-latest-job')).toHaveTextContent('Current');
  });

  // The newest attempt is shown above the table; it must not be repeated in
  // the list of the task's other attempts.
  it('lists the earlier attempts without repeating the newest', () => {
    storeMock.tasks = [makeTask({
      status: 'failed',
      spawned_job_id: '',
      attempts: 2,
      metadata: { attempt_history: 'job-prior-0001,job-latest-0002' },
    })];
    storeMock.taskJobs = [
      makeJob({ job_id: 'job-prior-0001', state: 'failed', attempt: 1 }),
      makeJob({ job_id: 'job-latest-0002', state: 'failed', attempt: 2 }),
    ];
    render(TaskDetailView);
    expect(screen.getByRole('button', { name: 'Open job job-latest-0002' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open job job-prio' })).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /Open job job-latest-0002/ })).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// The agent working the task (go-quicbaas#856)
// ---------------------------------------------------------------------------

describe('TaskDetailView — the agent working the task', () => {
  beforeEach(reset);

  it('names the claimant when the task has one', () => {
    storeMock.tasks = [makeTask({ assigned_to: 'agent-7' })];
    render(TaskDetailView);
    expect(screen.getByTestId('task-assigned')).toHaveTextContent('agent-7');
  });

  // `assigned_to` is never written on the dispatcher's path, so for every
  // auto-spawned task it is empty for the task's whole life. The agent is on
  // the job record the page already loads.
  it('names the fleet agent the latest attempt ran on when the task has no claimant', () => {
    storeMock.tasks = [makeTask({
      status: 'completed',
      auto_spawn: true,
      assigned_to: '',
      spawned_job_id: '',
      attempts: 1,
      metadata: { attempt_history: 'job-abc' },
    })];
    storeMock.taskJobs = [makeJob({ job_id: 'job-abc', agent_id: 'agent-automatethethings-agent-vm-host-4' })];
    render(TaskDetailView);
    expect(screen.getByTestId('task-assigned'))
      .toHaveTextContent('agent-automatethethings-agent-vm-host-4');
  });

  it('renders an em-dash when neither the task nor its jobs name an agent', () => {
    storeMock.tasks = [makeTask({ assigned_to: '', spawned_job_id: '' })];
    render(TaskDetailView);
    expect(screen.getByTestId('task-assigned')).toHaveTextContent('—');
  });
});

describe('TaskDetailView — pull requests and issue body', () => {
  beforeEach(reset);

  it('lists the pull_request artifact the runner publishes, with its link', () => {
    // go-quicbaas#796: the runner publishes {kind: 'url', name:
    // 'pull_request'} — the engine's artifact validator refuses any kind
    // outside inline/file/commit/kv_key/url — so a fixture with kind
    // 'pull_request' described bytes that cannot exist.
    storeMock.tasks = [makeTask({
      output_artifacts: [
        { kind: 'commit', name: 'head', ref: { sha: 'deadbeef', branch: 'issue/42-widget' } },
        {
          kind: 'url',
          name: 'pull_request',
          ref: { path: 'https://forge.example/owner/repo/pulls/7', branch: 'issue/42-widget' },
          metadata: { number: '7', state: 'open', branch: 'issue/42-widget' },
        },
      ],
    })];
    render(TaskDetailView);
    expect(screen.getByText('Pull requests (1)')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'pull_request' })).toHaveAttribute('href', 'https://forge.example/owner/repo/pulls/7');
    expect(screen.getByText('issue/42-widget')).toBeInTheDocument();
  });

  it('lists EVERY attempt\'s pull request, because this page is per-task', () => {
    // The counterpart to the job page, which shows only its own attempt's
    // delivery (go-quicbaas#796). Here both belong: a reader on the task
    // wants the whole history of what it published.
    storeMock.tasks = [makeTask({
      attempts: 3,
      output_artifacts: [
        {
          kind: 'url',
          name: 'pull_request@a1',
          ref: { path: 'https://forge.example/owner/repo/pulls/40', branch: 'issue/42-widget' },
        },
        {
          kind: 'url',
          name: 'pull_request@a3',
          ref: { path: 'https://forge.example/owner/repo/pulls/57', branch: 'issue/42-widget' },
        },
      ],
    })];
    render(TaskDetailView);
    expect(screen.getByText('Pull requests (2)')).toBeInTheDocument();
    const list = screen.getByLabelText('Pull requests');
    expect(list.querySelectorAll('li').length).toBe(2);
    expect(screen.getByRole('link', { name: 'pull_request@a1' })).toHaveAttribute(
      'href',
      'https://forge.example/owner/repo/pulls/40',
    );
    expect(screen.getByRole('link', { name: 'pull_request@a3' })).toHaveAttribute(
      'href',
      'https://forge.example/owner/repo/pulls/57',
    );
  });

  it('says nothing has run when no attempt was ever charged', () => {
    storeMock.tasks = [makeTask({ attempts: 0, output_artifacts: [{ kind: 'commit', name: 'deadbeef' }] })];
    render(TaskDetailView);
    expect(screen.getByTestId('pull-requests-empty')).toHaveTextContent(
      'No attempt has run, so nothing has been published.',
    );
  });

  // A task that failed before reaching publish and a task that published
  // nothing are different outcomes; one sentence for both is a lie about one
  // of them. go-quicbaas#822.
  it('says a failed task ended before publishing, naming the attempts that ran', () => {
    storeMock.tasks = [makeTask({ status: 'failed', attempts: 3 })];
    render(TaskDetailView);
    expect(screen.getByTestId('pull-requests-empty')).toHaveTextContent(
      'This task ended failed before publishing a pull request (3 attempts ran).',
    );
  });

  it('says a completed task published nothing', () => {
    storeMock.tasks = [makeTask({ status: 'completed', attempts: 1 })];
    render(TaskDetailView);
    expect(screen.getByTestId('pull-requests-empty')).toHaveTextContent(
      'This task completed without publishing a pull request (1 attempt ran).',
    );
  });

  it('says a running task has not published yet', () => {
    storeMock.tasks = [makeTask({ status: 'in_progress', attempts: 1 })];
    render(TaskDetailView);
    expect(screen.getByTestId('pull-requests-empty')).toHaveTextContent(
      'No pull request has been published yet.',
    );
  });

  it('renders the issue body in a collapsible block', () => {
    storeMock.tasks = [makeTask({ source: { kind: 'forge_issue', ref: '1' } })];
    render(TaskDetailView);
    expect(screen.getByText('Issue body')).toBeInTheDocument();
    expect(screen.getByText('The issue body.')).toBeInTheDocument();
  });
});

describe('TaskDetailView — not found and errors', () => {
  beforeEach(reset);

  it('renders "not found" for an unknown id and asks the store to load once', () => {
    routerStoreMock.params = { id: 'task-does-not-exist' };
    render(TaskDetailView);
    expect(screen.getByText('Task not found or not yet loaded.')).toBeInTheDocument();
    expect(mockLoadTasks).toHaveBeenCalled();
  });

  it('shows a spinner while the store is loading, not "not found"', () => {
    storeMock.tasksLoading = true;
    render(TaskDetailView);
    expect(screen.getByText('Loading task…')).toBeInTheDocument();
    expect(screen.queryByText('Task not found or not yet loaded.')).toBeNull();
  });

  it('names the missing permission when the load failed with a 403', () => {
    storeMock.tasksError = { status: 403 };
    render(TaskDetailView);
    expect(screen.getByRole('alert')).toHaveTextContent(
      'You do not have permission to view this task: this account lacks orchestrator:status.',
    );
  });

  it('Back returns to the task list; Reload refetches the task and its attempts', async () => {
    render(TaskDetailView);
    await fireEvent.click(screen.getByText('Back to Tasks'));
    expect(mockRouteTasks).toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Reload task' }));
    expect(mockRefresh).toHaveBeenCalledWith('tasks');
    expect(mockLoadTaskJobs).toHaveBeenCalledWith('task-1');
  });
});
