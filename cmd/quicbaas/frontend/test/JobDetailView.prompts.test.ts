/**
 * JobDetailView — the Prompts group (go-quicbaas#806).
 *
 * A job records the exact prompt version every prompt it rendered came from
 * (JobStatus.prompts). The job page lists them, one line per ref reading
 * `set/name · scope · vN · hash8`, each linking to the Prompts page at that
 * version, for both runners. A job that recorded no refs shows no group.
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/svelte';

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

vi.mock('../src/stores/router.svelte.ts', () => ({
  listReturnHash: vi.fn().mockReturnValue('#/jobs'),
  navigateTo: vi.fn(),
  routeFleetNode: vi.fn(),
  routeJobs: vi.fn(),
  routeSessionDetail: vi.fn(),
  routeTaskDetail: vi.fn(),
  routerStore: { name: 'job-detail', params: { id: 'job-7' }, hash: '#/jobs/job-7' },
}));

vi.mock('../src/stores/job-events.svelte.ts', () => ({
  startEventStream: vi.fn(),
  stopEventStream: vi.fn(),
  retryEventStream: vi.fn(),
  eventSubStore: { liveUpdatesUnavailable: () => false },
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      cancelJob: vi.fn().mockResolvedValue(undefined),
      readmitTask: vi.fn(),
      listRunners: vi.fn().mockResolvedValue([]),
    },
  }),
}));

vi.mock('../src/components/orchestrator/JobStateMachine.svelte', () => ({ default: vi.fn() }));
vi.mock('../src/components/orchestrator/DAGGraph.svelte', () => ({ default: vi.fn() }));
vi.mock('../src/components/orchestrator/EventLog.svelte', () => ({ default: vi.fn() }));
vi.mock('../src/components/orchestrator/BudgetGauge.svelte', () => ({ default: vi.fn() }));

import JobDetailView from '../src/views/orchestrator/JobDetailView.svelte';

/** MS is one millisecond in the server's Unix nanoseconds. */
const MS = 1_000_000;

const HASH_PLAN = 'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
const HASH_REVIEW = 'sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210';

/** job is a finished job, as GET /api/v1/orchestrator/jobs/{id} returns it. */
function job(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    job_id: 'job-7',
    tenant_id: 'acme',
    state: 'completed',
    iteration: 1,
    task_count: 1,
    completed: 1,
    failed: 0,
    description: 'Tighten the plan prompt',
    started_at: 1_000 * MS,
    updated_at: 5_000 * MS,
    runner: 'sdlc',
    events: [],
    ...over,
  };
}

describe('JobDetailView — the prompt versions a job ran with', () => {
  beforeEach(() => {
    storeMock.jobs = [];
    storeMock.tasks = [];
  });

  it('lists one line per ref, each linking to the Prompts page at its version', () => {
    storeMock.selectedJob = job({
      prompts: [
        { set: 'sdlc', name: 'plan', scope: 'project', scope_id: 'proj-1', version: 4, hash: HASH_PLAN },
        { set: 'sdlc', name: 'review', scope: 'embedded', version: 0, hash: HASH_REVIEW },
      ],
    });
    render(JobDetailView);

    const group = screen.getByTestId('job-prompts');
    const links = within(group).getAllByRole('link');
    expect(links).toHaveLength(2);

    expect(links[0]).toHaveTextContent('sdlc/plan · project · v4 · 01234567');
    const plan = new URLSearchParams((links[0]?.getAttribute('href') ?? '').split('?')[1]);
    expect(links[0]?.getAttribute('href')).toMatch(/^#\/prompts\?/);
    expect(plan.get('set')).toBe('sdlc');
    expect(plan.get('scope')).toBe('project');
    expect(plan.get('project')).toBe('proj-1');
    expect(plan.get('name')).toBe('plan');
    expect(plan.get('version')).toBe('4');

    // The embedded body is what the system scope inherits: its link opens there.
    expect(links[1]).toHaveTextContent('sdlc/review · embedded · v0 · fedcba98');
    const review = new URLSearchParams((links[1]?.getAttribute('href') ?? '').split('?')[1]);
    expect(review.get('scope')).toBe('system');
    expect(review.get('project')).toBeNull();
  });

  it('shows the group for a workflow job too', () => {
    storeMock.selectedJob = job({
      runner: 'workflow',
      work_items: [],
      prompts: [{ set: 'workflow', name: 'lead', scope: 'tenant', scope_id: 'acme', version: 2, hash: HASH_PLAN }],
    });
    render(JobDetailView);

    expect(within(screen.getByTestId('job-prompts')).getByRole('link')).toHaveTextContent(
      'workflow/lead · tenant · v2 · 01234567',
    );
  });

  it('shows no group for a job that recorded no refs', () => {
    storeMock.selectedJob = job();
    render(JobDetailView);
    expect(screen.queryByTestId('job-prompts')).not.toBeInTheDocument();
  });

  it('shows no group for a job whose ref list is empty', () => {
    storeMock.selectedJob = job({ prompts: [] });
    render(JobDetailView);
    expect(screen.queryByTestId('job-prompts')).not.toBeInTheDocument();
  });
});
