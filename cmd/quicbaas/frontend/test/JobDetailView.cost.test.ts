/**
 * JobDetailView.cost.test.ts
 *
 * go-quicbaas#794's acceptance check for the job page:
 *   - the Cost tab reads THIS job's consumption-ledger entries, not job memory;
 *   - an sdlc job renders spend and NO iteration gauge;
 *   - a workflow job renders both;
 *   - an unpriced cost is a dash, never a zero.
 *
 * The runner-built layout itself (#796) is JobDetailView.runner.test.ts's;
 * this file owns only the cost half.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { GetSpendReportResponse } from '@quicbaas/sdk';

const { storeMock, getSpendReport } = vi.hoisted(() => ({
  storeMock: {
    selectedJob: null as unknown,
    events: [] as unknown[],
    activeTenantID: 'acme',
    jobs: [] as unknown[],
    tasks: [] as unknown[],
  },
  getSpendReport: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
  selectJob: vi.fn(),
  isTerminalJobState: (state: string) => state === 'completed' || state === 'failed',
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeAgentDetail: vi.fn(),
  routeJobs: vi.fn(),
  routeSessionDetail: vi.fn(),
  routeTaskDetail: vi.fn(),
  routerStore: { route: { name: 'job-detail', params: { id: 'job-3' }, hash: '#/jobs/job-3' } },
}));

vi.mock('../src/stores/job-events.svelte.ts', () => ({
  startEventStream: vi.fn(),
  stopEventStream: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      cancelJob: vi.fn().mockResolvedValue(undefined),
      readmitTask: vi.fn(),
      listRunners: vi.fn().mockResolvedValue([]),
      getSpendReport,
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

/** job builds a record in the shape the server sends. */
function job(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    job_id: 'job-3',
    tenant_id: 'acme',
    state: 'completed',
    iteration: 0,
    task_count: 1,
    completed: 1,
    failed: 0,
    description: 'go-truststrap#26: harden the bootstrap',
    started_at: 1_000 * MS,
    updated_at: 10_000 * MS,
    error: '',
    events: [],
    work_items: [],
    memory: null,
    workflow_name: '',
    runner: 'sdlc',
    task_id: 'task-26',
    config: {
      tenant_id: 'acme',
      description: 'go-truststrap#26',
      lead_backend: 'acp',
      lead_model: 'claude-opus-5',
      project_id: 'proj-1',
      poll_interval: 0,
      max_iterations: 10,
      workflow_name: '',
      task_id: 'task-26',
    },
    ...over,
  };
}

const jobSpend: GetSpendReportResponse = {
  level: 'job',
  tenant_id: 'acme',
  project_id: 'proj-1',
  task_id: 'task-26',
  job_id: 'job-3',
  entries: 4,
  input_tokens: 4000,
  output_tokens: 1200,
  total_tokens: 5200,
  cost_micros: 0,
  cost_measured: false,
  successes: 4,
  failures: 0,
  accounts: [
    { account_id: 'acct-1', display_name: 'alice-work', total_tokens: 5200, policy_status: 'none' },
  ],
};

beforeEach(() => {
  getSpendReport.mockReset();
  getSpendReport.mockResolvedValue(jobSpend);
  storeMock.selectedJob = null;
  storeMock.tasks = [];
  storeMock.events = [];
});

/** openCost selects the Cost tab. */
async function openCost(): Promise<void> {
  await fireEvent.click(await screen.findByRole('tab', { name: /^Cost$/i }));
}

// ---------------------------------------------------------------------------
// The Cost tab reads the ledger
// ---------------------------------------------------------------------------

describe('JobDetailView Cost tab', () => {
  it('reads the job’s ledger entries, addressed through project and task', async () => {
    storeMock.selectedJob = job();
    render(JobDetailView);

    await waitFor(() => expect(getSpendReport).toHaveBeenCalled());
    expect(getSpendReport.mock.calls[0]![0]).toMatchObject({
      tenant_id: 'acme',
      project_id: 'proj-1',
      task_id: 'task-26',
      job_id: 'job-3',
    });

    await openCost();
    const tokens = await screen.findByTestId('job-total-tokens');
    expect(tokens.textContent?.trim()).toBe('5,200');
    expect(screen.getByText('4,000')).toBeTruthy();
    expect(screen.getByText('1,200')).toBeTruthy();
  });

  it('names the account that paid and its spend policy', async () => {
    storeMock.selectedJob = job();
    render(JobDetailView);
    await openCost();

    expect(await screen.findByTestId('job-spend-account')).toBeTruthy();
    expect(screen.getByText('alice-work')).toBeTruthy();
    expect(screen.getByText('No spend policy')).toBeTruthy();
  });

  it('renders an unpriced cost as a dash, never as 0', async () => {
    storeMock.selectedJob = job();
    render(JobDetailView);
    await openCost();

    const cost = await screen.findByTestId('job-cost');
    expect(cost.textContent?.trim()).toBe('—');
    expect(screen.getByText(/cost unmeasured/i)).toBeTruthy();
  });

  it('addresses a job with no project or task by the ledger sentinel', async () => {
    storeMock.selectedJob = job({ task_id: undefined, config: null });
    render(JobDetailView);

    await waitFor(() => expect(getSpendReport).toHaveBeenCalled());
    expect(getSpendReport.mock.calls[0]![0]).toMatchObject({
      project_id: '-',
      task_id: '-',
      job_id: 'job-3',
    });
  });

  it('reports a failed spend read instead of rendering it as no spend', async () => {
    getSpendReport.mockRejectedValue({ status: 503, message: 'ledger not available' });
    storeMock.selectedJob = job();
    render(JobDetailView);
    await openCost();

    expect(await screen.findByText(/not recording provider consumption/i)).toBeTruthy();
    expect(screen.queryByTestId('job-total-tokens')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// The iteration gauge is the workflow runner's; the spend is everyone's
// ---------------------------------------------------------------------------

describe('JobDetailView Cost tab — the iteration gauge', () => {
  it('an sdlc job reports spend and says it has no iteration budget', async () => {
    storeMock.selectedJob = job();
    render(JobDetailView);
    await openCost();

    const notice = await screen.findByTestId('cost-no-iteration-budget');
    expect(notice.textContent).toContain('sdlc');
    expect(notice.textContent).toContain('does not iterate');
    expect(screen.queryByText('Iterations Used')).toBeNull();
    // Its spend is still reported: that is what #794 was filed for.
    expect(await screen.findByTestId('job-total-tokens')).toBeTruthy();
  });

  it('a workflow job reports both its iteration budget and its spend', async () => {
    storeMock.selectedJob = job({ runner: 'workflow', iteration: 3 });
    render(JobDetailView);
    await openCost();

    expect(await screen.findByText('3 of 10 max')).toBeTruthy();
    expect(screen.queryByTestId('cost-no-iteration-budget')).toBeNull();
    expect(await screen.findByTestId('job-total-tokens')).toBeTruthy();
  });

  it('a job naming no runner is the workflow runner, and keeps its gauge', async () => {
    // The server omits `runner` for its own workflow runner, so an absent
    // value must not be read as "unknown runner, hide the budget".
    storeMock.selectedJob = job({ runner: undefined, iteration: 2 });
    render(JobDetailView);
    await openCost();

    expect(await screen.findByText('2 of 10 max')).toBeTruthy();
    expect(screen.queryByTestId('cost-no-iteration-budget')).toBeNull();
  });
});
