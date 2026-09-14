/**
 * JobDetailView.test.ts
 *
 * Verifies:
 * 1. Back button renders with ChevronLeft icon (not plain text arrow).
 * 2. Summary fields (workflow, agent/backend, timing) are populated from job data.
 * 3. Events panel renders when the "events" tab is active.
 * 4. Cancel button is shown for cancellable states.
 * 5. Raw JSON drawer toggles open/closed.
 * 6. The overview reads the source Task for its link and its forge issue, and
 *    the JOB's own delivery for the pull request — see
 *    JobDetailView.runner.test.ts for why the two sources differ.
 * 7. Sessions the job holds are chips that route to the session detail.
 * 8. A running job holds an event stream open, and releases it on unmount.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mocks — factories must not reference outer-scope const before initialization.
// ---------------------------------------------------------------------------

const { storeMock } = vi.hoisted(() => ({
  storeMock: {
    selectedJob: null as unknown,
    events: [] as unknown[],
    activeTenantID: 'acme',
    jobs: [] as unknown[],
    tasks: [] as unknown[],
  },
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
  selectJob: vi.fn(),
  isTerminalJobState: (state: string) => state === 'completed' || state === 'failed',
}));

vi.mock('../src/stores/navigation.svelte.ts', () => ({
  navigate: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeJobs: vi.fn(),
  routeSessionDetail: vi.fn(),
  routeTaskDetail: vi.fn(),
  routerStore: {
    route: { name: 'job-detail', params: { id: 'job-abc-def-123' }, hash: '#/jobs/job-abc-def-123' },
  },
}));

vi.mock('../src/stores/job-events.svelte.ts', () => ({
  startEventStream: vi.fn(),
  stopEventStream: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      cancelJob: vi.fn().mockResolvedValue(undefined),
    },
  }),
}));

// Stub child components that use canvas or complex DOM APIs.
// vi.mock factories are hoisted; each returns a fresh function stub.
vi.mock('../src/components/orchestrator/JobStateMachine.svelte', () => ({
  default: vi.fn(),
}));
vi.mock('../src/components/orchestrator/DAGGraph.svelte', () => ({
  default: vi.fn(),
}));
vi.mock('../src/components/orchestrator/EventLog.svelte', () => ({
  default: vi.fn(),
}));
vi.mock('../src/components/orchestrator/BudgetGauge.svelte', () => ({
  default: vi.fn(),
}));

import JobDetailView from '../src/views/orchestrator/JobDetailView.svelte';
import { routeJobs, routeSessionDetail, routeTaskDetail } from '../src/stores/router.svelte.ts';
import { startEventStream, stopEventStream } from '../src/stores/job-events.svelte.ts';

// ---------------------------------------------------------------------------
// Shared mock job fixture
// ---------------------------------------------------------------------------

const mockJob = {
  job_id: 'job-abc-def-123',
  tenant_id: 'default',
  state: 'executing' as const,
  iteration: 3,
  task_count: 10,
  completed: 5,
  failed: 0,
  description: 'Test job description',
  started_at: 1700000000,
  updated_at: 1700001000,
  workflow_name: 'my-workflow',
  config: {
    tenant_id: 'default',
    description: 'Test',
    lead_backend: 'claude',
    lead_model: 'claude-3-5-sonnet',
    max_iterations: 10,
  },
  events: [
    { timestamp: 1700000100, type: 'state_change', details: 'executing' },
  ],
  task_id: 'task-77',
  attempt: 2,
  lead_session_id: 'sess-lead-000001',
  session_ids: ['sess-lead-000001', 'sess-worker-00002'],
  // What THIS attempt delivered. The server scopes it to the attempt; the
  // page renders it rather than the task's all-attempt artifact list.
  delivery: {
    branch: 'issue/580-x',
    head_sha: 'feed0123456789ab',
    pull_request_number: 581,
    pull_request_url: 'http://forge.example/jeremyhahn/go-quicbaas/pulls/581',
    pull_request_state: 'open',
  },
};

// The task the job is an attempt at: adopted from a forge issue, with one
// pull request already delivered.
const mockTask = {
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
  output_artifacts: [
    {
      name: 'pull_request',
      kind: 'url',
      ref: { path: 'http://forge.example/jeremyhahn/go-quicbaas/pulls/581' },
      metadata: { number: '581', state: 'open', branch: 'issue/580-x' },
    },
  ],
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('JobDetailView', () => {
  beforeEach(() => {
    storeMock.selectedJob = { ...mockJob };
    storeMock.jobs = [mockJob];
    storeMock.tasks = [mockTask];
    vi.mocked(routeJobs).mockClear();
    vi.mocked(routeSessionDetail).mockClear();
    vi.mocked(routeTaskDetail).mockClear();
    vi.mocked(startEventStream).mockClear();
    vi.mocked(stopEventStream).mockClear();
  });

  it('renders the back button with ChevronLeft icon (svg), not plain text arrow', () => {
    render(JobDetailView);
    const backBtn = screen.getByRole('button', { name: /back to jobs list/i });
    expect(backBtn).toBeTruthy();
    expect(backBtn.querySelector('svg')).not.toBeNull();
    expect(backBtn.textContent).not.toContain('←');
  });

  it('displays the job description in the heading', () => {
    render(JobDetailView);
    expect(screen.getByText('Test job description')).toBeInTheDocument();
  });

  it('populates summary card with workflow, backend, and model', () => {
    render(JobDetailView);
    // workflow_name appears in both summary card and overview tab — use getAllByText.
    const workflowEls = screen.getAllByText('my-workflow');
    expect(workflowEls.length).toBeGreaterThan(0);
    expect(screen.getByText('Lead backend: claude')).toBeInTheDocument();
    expect(screen.getByText('claude-3-5-sonnet')).toBeInTheDocument();
  });

  it('shows progress bar with correct value and max attributes', () => {
    const { container } = render(JobDetailView);
    const bars = container.querySelectorAll('progress[aria-label="Task completion progress"]');
    expect(bars.length).toBeGreaterThan(0);
    const bar = bars[0] as HTMLProgressElement;
    expect(bar.value).toBe(5);
    expect(bar.max).toBe(10);
  });

  it('shows Cancel button for executing state', () => {
    render(JobDetailView);
    expect(screen.getByRole('button', { name: /cancel job/i })).toBeInTheDocument();
  });

  it('does not show Cancel button for completed state', () => {
    storeMock.selectedJob = { ...mockJob, state: 'completed' as const };
    render(JobDetailView);
    expect(screen.queryByRole('button', { name: /cancel job/i })).toBeNull();
  });

  it('offers Readmit for a failed job, enabled only when the server allows it', () => {
    // go-quicbaas#796: the action used to key off `state === 'failed'` and was
    // permanently disabled. Whether the work can run again is the task's
    // answer — parked may be readmitted, failed is terminal — and only the
    // server knows which.
    storeMock.selectedJob = { ...mockJob, state: 'failed' as const, readmit_allowed: false };
    render(JobDetailView);
    const refused = screen.getByRole('button', { name: /readmit task/i });
    expect(refused).toBeInTheDocument();
    expect((refused as HTMLButtonElement).disabled).toBe(true);
  });

  it('clicking back button calls routeJobs', async () => {
    render(JobDetailView);
    const backBtn = screen.getByRole('button', { name: /back to jobs list/i });
    await fireEvent.click(backBtn);
    expect(vi.mocked(routeJobs)).toHaveBeenCalledTimes(1);
  });

  it('renders audit events in the iteration timeline', () => {
    render(JobDetailView);
    expect(screen.getByText('state_change')).toBeInTheDocument();
  });

  it('raw JSON drawer is initially collapsed and opens on click', async () => {
    const { container } = render(JobDetailView);
    expect(container.querySelector('#raw-json-panel')).toBeNull();

    const drawerBtn = screen.getByRole('button', { name: /raw job state/i });
    await fireEvent.click(drawerBtn);
    expect(container.querySelector('#raw-json-panel')).not.toBeNull();
    expect(container.querySelector('#raw-json-panel')!.textContent).toContain('job-abc-def-123');
  });

  it('shows "No job selected" when selectedJob is null', () => {
    storeMock.selectedJob = null;
    render(JobDetailView);
    expect(screen.getByText('No job selected')).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // The source Task: what the job is an attempt at, and what it delivered.
  // -------------------------------------------------------------------------

  it('links the overview to the Task named by task_id', async () => {
    render(JobDetailView);
    const link = screen.getByRole('link', { name: /Wire the job event stream/ });
    expect(link.getAttribute('href')).toBe('#/tasks/task-77');
    await fireEvent.click(link);
    expect(vi.mocked(routeTaskDetail)).toHaveBeenCalledWith('task-77');
  });

  it('finds the Task from the other end when the job carries no task_id', () => {
    storeMock.selectedJob = { ...mockJob, task_id: undefined };
    render(JobDetailView);
    // The task still names the job it spawned, so the binding is readable.
    expect(screen.getByRole('link', { name: /Wire the job event stream/ })).toBeInTheDocument();
  });

  it('links the forge issue the Task was adopted from', () => {
    render(JobDetailView);
    const link = screen.getByRole('link', { name: /#580/ });
    expect(link.getAttribute('href')).toBe('http://forge.example/jeremyhahn/go-quicbaas/issues/580');
    expect(link.getAttribute('rel')).toContain('noopener');
  });

  it('links the delivered pull request and offers "Open PR"', () => {
    render(JobDetailView);
    const pr = screen.getByRole('link', { name: /#581/ });
    expect(pr.getAttribute('href')).toBe('http://forge.example/jeremyhahn/go-quicbaas/pulls/581');
    const open = screen.getByRole('link', { name: /Open PR/ });
    expect(open.getAttribute('href')).toBe('http://forge.example/jeremyhahn/go-quicbaas/pulls/581');
  });

  it('renders no Task or Issue link for a job with no source task', () => {
    storeMock.tasks = [];
    storeMock.selectedJob = { ...mockJob, task_id: undefined };
    render(JobDetailView);
    expect(screen.queryByRole('link', { name: /Wire the job event stream/ })).toBeNull();
    expect(screen.queryByRole('link', { name: /#580/ })).toBeNull();
  });

  it('renders no PR link for an attempt that delivered none', () => {
    // The pull request row is the ATTEMPT's, so it goes empty when the
    // attempt published nothing — even though the task it belongs to still
    // carries an earlier attempt's pull_request artifact.
    storeMock.selectedJob = { ...mockJob, delivery: undefined };
    const { container } = render(JobDetailView);
    expect(
      container.querySelector('[data-testid="summary-pull-request"]')!.textContent,
    ).toContain('—');
    expect(screen.queryByRole('link', { name: /Open PR/ })).toBeNull();
    expect(screen.queryByRole('link', { name: /581/ })).toBeNull();
  });

  // -------------------------------------------------------------------------
  // Review round
  // -------------------------------------------------------------------------

  it('shows the review round while the job is reviewing', () => {
    storeMock.selectedJob = { ...mockJob, state: 'reviewing' as const, iteration: 2 };
    render(JobDetailView);
    expect(screen.getByText('Review round')).toBeInTheDocument();
    expect(screen.getByText('2 / 10')).toBeInTheDocument();
  });

  it('shows the review round while the job is sending work back', () => {
    storeMock.selectedJob = { ...mockJob, state: 're-delegating' as const, iteration: 3 };
    render(JobDetailView);
    expect(screen.getByText('3 / 10')).toBeInTheDocument();
  });

  it('hides the review round when the job is not in a review', () => {
    render(JobDetailView); // executing
    expect(screen.queryByText('Review round')).toBeNull();
  });

  // -------------------------------------------------------------------------
  // Sessions
  // -------------------------------------------------------------------------

  it('renders one chip per session, the lead marked, routing to the session', async () => {
    render(JobDetailView);
    const chips = screen.getByLabelText('Job sessions').querySelectorAll('a');
    expect(chips.length).toBe(2);
    expect(chips[0].textContent).toContain('lead');
    expect(chips[0].getAttribute('href')).toBe('#/sessions/sess-lead-000001');
    expect(chips[1].getAttribute('href')).toBe('#/sessions/sess-worker-00002');
    await fireEvent.click(chips[1]);
    expect(vi.mocked(routeSessionDetail)).toHaveBeenCalledWith('sess-worker-00002');
  });

  it('does not repeat the lead session when session_ids also carries it', () => {
    storeMock.selectedJob = {
      ...mockJob,
      lead_session_id: 'sess-lead-000001',
      session_ids: ['sess-lead-000001'],
    };
    render(JobDetailView);
    expect(screen.getByLabelText('Job sessions').querySelectorAll('a').length).toBe(1);
  });

  it('says the job has no session rather than showing an empty chip row', () => {
    storeMock.selectedJob = { ...mockJob, lead_session_id: undefined, session_ids: [] };
    render(JobDetailView);
    expect(screen.queryByLabelText('Job sessions')).toBeNull();
    expect(screen.getByText(/no open session/i)).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // Live event stream
  // -------------------------------------------------------------------------

  it('opens the event stream for a running job and releases it on unmount', () => {
    const { unmount } = render(JobDetailView);
    expect(vi.mocked(startEventStream)).toHaveBeenCalledWith('acme', 'job-abc-def-123');
    expect(vi.mocked(stopEventStream)).not.toHaveBeenCalled();
    unmount();
    expect(vi.mocked(stopEventStream)).toHaveBeenCalled();
  });

  it('opens no stream for a job that has already stopped', () => {
    storeMock.selectedJob = { ...mockJob, state: 'completed' as const };
    render(JobDetailView);
    expect(vi.mocked(startEventStream)).not.toHaveBeenCalled();
  });
});
