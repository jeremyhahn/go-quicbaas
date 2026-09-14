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
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

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

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      decomposeTask: mockDecomposeTask,
      claimTask: mockClaimTask,
      completeTask: mockCompleteTask,
    },
    projects: {
      listProjects: mockListProjects,
    },
  }),
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
vi.mock('lucide-svelte', () => ({
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
  routerStoreMock.name = 'tasks';
  routerStoreMock.params = {};
  routerStoreMock.hash = '#/tasks';
  mockListProjects.mockResolvedValue({ projects: [{ id: 'proj-1', name: 'go-truststrap' }], total: 1 });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TasksView — columns', () => {
  beforeEach(resetStores);

  it('renders the generic Task column set in table layout', () => {
    render(TasksView);
    for (const header of ['Subject', 'Project', 'Status', 'Attempts', 'Assigned', 'Job', 'Updated']) {
      expect(screen.getByRole('columnheader', { name: header })).toBeInTheDocument();
    }
  });

  it('no longer renders the Team, Created, or Spawned Job columns', () => {
    render(TasksView);
    expect(screen.queryByRole('columnheader', { name: 'Team' })).toBeNull();
    expect(screen.queryByRole('columnheader', { name: 'Created' })).toBeNull();
    expect(screen.queryByRole('columnheader', { name: 'Spawned Job' })).toBeNull();
  });

  it('keeps the column set when tasks are present', () => {
    storeTasks = [makeTask()];
    render(TasksView);
    expect(screen.getByRole('columnheader', { name: 'Job' })).toBeInTheDocument();
  });
});

describe('TasksView — provenance and job link', () => {
  beforeEach(resetStores);

  it('links the forge issue chip to the source URL, opening on the forge', () => {
    storeTasks = [makeTask({ source: { kind: 'forge_issue', ref: 'owner/repo#42', url: 'https://forge.example/owner/repo/issues/42' } })];
    render(TasksView);
    const chip = screen.getByTitle('Open issue #42 on the forge');
    expect(chip).toHaveTextContent('#42');
    expect(chip).toHaveAttribute('href', 'https://forge.example/owner/repo/issues/42');
    expect(chip).toHaveAttribute('target', '_blank');
  });

  it('renders the issue number without a link when the source carries no URL', () => {
    storeTasks = [makeTask({ source: { kind: 'forge_issue', ref: '7' } })];
    render(TasksView);
    const chip = screen.getByTitle('Forge issue #7');
    expect(chip.tagName).toBe('SPAN');
  });

  it('renders no issue chip for a hand-created task', () => {
    storeTasks = [makeTask({ source: { kind: 'operator', ref: 'alice' } })];
    render(TasksView);
    expect(screen.queryByTitle(/Forge issue|on the forge/)).toBeNull();
  });

  it('links the open attempt to #/jobs/{id}, not the dead #/orchestrator/jobs/ path', () => {
    storeTasks = [makeTask({ spawned_job_id: 'job-abcdef1234567890' })];
    render(TasksView);
    const link = screen.getByTitle(/View job job-abcdef1234567890/i);
    expect(link).toHaveAttribute('href', '#/jobs/job-abcdef1234567890');
  });

  // THE DEFECT (go-quicbaas#856): a completed task has NO `spawned_job_id`.
  // recordAttemptHistory moves it into metadata.attempt_history and clears it
  // the moment the attempt concludes, so the column fell through to
  // `auto_spawn` and printed "no agent matched" for a task that had run a job
  // to completion — the operator's first hop from task to job, gone.
  it('links a completed task to the job that ran it, from the attempt history', () => {
    storeTasks = [makeTask({
      status: 'completed',
      auto_spawn: true,
      spawned_job_id: '',
      attempts: 1,
      metadata: { attempt_history: '1f644ffba52203488392749b50573b87' },
    })];
    render(TasksView);
    const link = screen.getByTitle(/View job 1f644ffba52203488392749b50573b87/i);
    expect(link).toHaveAttribute('href', '#/jobs/1f644ffba52203488392749b50573b87');
    expect(screen.queryByText(/no agent matched/i)).toBeNull();
  });

  // A retried task names every attempt it ran; the column links the newest,
  // because that is the run whose outcome the status reflects.
  it('links the newest attempt when a task ran several', () => {
    storeTasks = [makeTask({
      status: 'failed',
      auto_spawn: true,
      spawned_job_id: '',
      attempts: 3,
      metadata: { attempt_history: 'job-one,job-two,job-three' },
    })];
    render(TasksView);
    expect(screen.getByTitle(/View job job-three/i)).toHaveAttribute('href', '#/jobs/job-three');
    expect(screen.queryByTitle(/View job job-one/i)).toBeNull();
  });

  // "No agent matched" is a claim about DISPATCH, and it stays for the one
  // task it is true of: auto-spawn requested, nothing ever started.
  it('shows "no agent matched" only for an auto-spawn task that has never dispatched', () => {
    storeTasks = [makeTask({ auto_spawn: true, spawned_job_id: '' })];
    render(TasksView);
    expect(screen.getByText(/no agent matched/i)).toBeInTheDocument();
  });

  // A task whose attempts were charged but whose history the page cannot read
  // must not be described as having matched no agent either.
  it('does not claim "no agent matched" for a task whose attempts were charged', () => {
    storeTasks = [makeTask({ auto_spawn: true, spawned_job_id: '', attempts: 2 })];
    render(TasksView);
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

  it('names the claimant when the task has one', () => {
    storeTasks = [makeTask({ assigned_to: 'alice' })];
    render(TasksView);
    expect(screen.getByTestId('task-assigned')).toHaveTextContent('alice');
  });

  it('names the fleet agent the latest attempt ran on when the task has no claimant', () => {
    storeTasks = [makeTask({
      status: 'completed',
      auto_spawn: true,
      assigned_to: '',
      spawned_job_id: '',
      attempts: 1,
      metadata: { attempt_history: 'job-abc' },
    })];
    storeJobs = [{ job_id: 'job-abc', agent_id: 'agent-automatethethings-agent-vm-host-4' }];
    render(TasksView);
    expect(screen.getByTestId('task-assigned'))
      .toHaveTextContent('agent-automatethethings-agent-vm-host-4');
  });

  // The LATEST attempt's agent, not an earlier one's: a task retried onto a
  // second host must not still read as running on the first.
  it('names the latest attempt\'s agent, not an earlier attempt\'s', () => {
    storeTasks = [makeTask({
      status: 'failed',
      auto_spawn: true,
      assigned_to: '',
      spawned_job_id: '',
      attempts: 2,
      metadata: { attempt_history: 'job-one,job-two' },
    })];
    storeJobs = [
      { job_id: 'job-one', agent_id: 'agent-host-a' },
      { job_id: 'job-two', agent_id: 'agent-host-b' },
    ];
    render(TasksView);
    const cell = screen.getByTestId('task-assigned');
    expect(cell).toHaveTextContent('agent-host-b');
    expect(cell).not.toHaveTextContent('agent-host-a');
  });

  // An em-dash is still the right answer when nothing names an agent: the
  // column must not invent one from a job record it does not hold.
  it('renders an em-dash when neither the task nor any job names an agent', () => {
    storeTasks = [makeTask({ assigned_to: '', spawned_job_id: '' })];
    render(TasksView);
    expect(screen.getByTestId('task-assigned')).toHaveTextContent('—');
  });

  it('renders an em-dash when the job record for the latest attempt is not held', () => {
    storeTasks = [makeTask({
      status: 'completed',
      assigned_to: '',
      spawned_job_id: '',
      metadata: { attempt_history: 'job-unread' },
    })];
    storeJobs = [{ job_id: 'job-other', agent_id: 'agent-host-a' }];
    render(TasksView);
    expect(screen.getByTestId('task-assigned')).toHaveTextContent('—');
  });
});

describe('TasksView — status and attempts', () => {
  beforeEach(resetStores);

  it('marks a blocked task with its reason', () => {
    storeTasks = [makeTask({ status: 'blocked', blocked_reason: 'needs an owner ruling on the schema' })];
    render(TasksView);
    const marker = screen.getByTestId('blocked-marker');
    expect(marker).toHaveTextContent('needs an owner ruling on the schema');
    expect(marker).toHaveAttribute('title', 'needs an owner ruling on the schema');
  });

  it('says so when a blocked task has no reason recorded', () => {
    storeTasks = [makeTask({ status: 'blocked' })];
    render(TasksView);
    expect(screen.getByTestId('blocked-marker')).toHaveTextContent('No reason recorded.');
  });

  it('renders no blocked marker for a working task', () => {
    storeTasks = [makeTask({ status: 'in_progress', blocked_reason: 'stale' })];
    render(TasksView);
    expect(screen.queryByTestId('blocked-marker')).toBeNull();
  });

  it('renders attempts as charged / budget with the dispatched count and a muted refund', () => {
    storeTasks = [makeTask({ attempts: 3, attempt_budget: 5, environment_faults: 1 })];
    render(TasksView);
    expect(screen.getByText('2 / 5 (3 dispatched)')).toBeInTheDocument();
    expect(screen.getByText(/1 refunded/)).toBeInTheDocument();
  });

  it('renders an em-dash for a task with no attempt accounting', () => {
    storeTasks = [makeTask()];
    render(TasksView);
    const row = screen.getByRole('button', { name: 'Open task Test task' });
    expect(row.textContent).toContain('—');
  });
});

describe('TasksView — project filter', () => {
  beforeEach(resetStores);

  it('shows only the tasks of the project named in ?project= and labels the filter', async () => {
    routerStoreMock.params = { project: 'proj-1' };
    storeTasks = [
      makeTask({ id: 't1', subject: 'In project', project_id: 'proj-1' }),
      makeTask({ id: 't2', subject: 'Elsewhere', project_id: 'proj-2' }),
    ];
    render(TasksView);
    expect(screen.getByText('In project')).toBeInTheDocument();
    expect(screen.queryByText('Elsewhere')).toBeNull();
    // The project name arrives after the async listProjects resolves.
    expect(await screen.findByText(/Project: go-truststrap/)).toBeInTheDocument();
  });

  it('names the project in the empty state when the filter matches nothing', async () => {
    routerStoreMock.params = { project: 'proj-1' };
    storeTasks = [makeTask({ id: 't2', subject: 'Elsewhere', project_id: 'proj-2' })];
    render(TasksView);
    expect(await screen.findByText('No tasks in go-truststrap.')).toBeInTheDocument();
  });

  it('clears the filter through routeTasks()', async () => {
    routerStoreMock.params = { project: 'proj-1' };
    render(TasksView);
    await fireEvent.click(screen.getByRole('button', { name: 'Show tasks from every project' }));
    expect(mockRouteTasks).toHaveBeenCalledWith();
  });

  it('falls back to the project id when the project list cannot be read', async () => {
    mockListProjects.mockRejectedValue(new Error('forbidden'));
    storeTasks = [makeTask({ project_id: 'proj-9' })];
    render(TasksView);
    expect(await screen.findByText('proj-9')).toBeInTheDocument();
  });
});

describe('TasksView — row click', () => {
  beforeEach(resetStores);

  it('opens the task detail route when a row is clicked', async () => {
    storeTasks = [makeTask({ id: 'task-xyz' })];
    render(TasksView);
    await fireEvent.click(screen.getByRole('button', { name: 'Open task Test task' }));
    expect(mockRouteTaskDetail).toHaveBeenCalledWith('task-xyz');
  });

  it('does not open the detail when an action button inside the row is clicked', async () => {
    mockDecomposeTask.mockResolvedValue({ tasks: [] });
    storeTasks = [makeTask({ id: 'task-xyz' })];
    render(TasksView);
    await fireEvent.click(screen.getByRole('button', { name: 'Decompose task Test task' }));
    expect(mockRouteTaskDetail).not.toHaveBeenCalled();
    expect(mockDecomposeTask).toHaveBeenCalled();
  });
});

describe('TasksView — spawn filter controls', () => {
  beforeEach(resetStores);

  it('renders the Auto-spawned, Queued and All filter buttons', () => {
    render(TasksView);
    expect(screen.getByRole('button', { name: /Show auto-spawned tasks/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Show queued tasks/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Show all tasks/i })).toBeInTheDocument();
  });
});

describe('TasksView — empty and error states', () => {
  beforeEach(resetStores);

  it('renders the "No tasks tracked" empty-state when there are no tasks', () => {
    render(TasksView);
    expect(screen.getByText(/No tasks tracked/i)).toBeInTheDocument();
  });

  it('names the missing permission when the list failed with a 403', () => {
    storeError = { status: 403, message: 'forbidden' };
    render(TasksView);
    expect(screen.getByRole('alert')).toHaveTextContent(
      'You do not have permission to view tasks: this account lacks orchestrator:status.',
    );
    expect(screen.queryByText(/No tasks tracked/i)).toBeNull();
  });

  it('shows a spinner only while loading with nothing to show, never alongside rows', () => {
    storeLoading = true;
    storeTasks = [makeTask()];
    const { container } = render(TasksView);
    expect(container.querySelector('.loading-spinner')).toBeNull();
    expect(screen.getByText('Test task')).toBeInTheDocument();
  });
});
