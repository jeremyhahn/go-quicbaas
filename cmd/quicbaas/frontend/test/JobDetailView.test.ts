/**
 * JobDetailView.test.ts
 *
 * Verifies:
 * 1. Back button renders with ChevronLeft icon (not plain text arrow).
 * 2. Summary fields (workflow, agent/backend, timing) are populated from job data.
 * 3. Events panel renders when the "events" tab is active.
 * 4. Cancel button is shown for cancellable states.
 * 5. Raw JSON drawer toggles open/closed.
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
    activeTenantID: 'default',
    jobs: [] as unknown[],
  },
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
  selectJob: vi.fn(),
}));

vi.mock('../src/stores/navigation.svelte.ts', () => ({
  navigate: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeJobs: vi.fn(),
  routerStore: {
    route: { name: 'job-detail', params: { id: 'job-abc-def-123' }, hash: '#/jobs/job-abc-def-123' },
  },
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
import { routeJobs } from '../src/stores/router.svelte.ts';

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
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('JobDetailView', () => {
  beforeEach(() => {
    storeMock.selectedJob = { ...mockJob };
    storeMock.jobs = [mockJob];
    vi.mocked(routeJobs).mockClear();
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

  it('shows Retry button for failed state', () => {
    storeMock.selectedJob = { ...mockJob, state: 'failed' as const };
    render(JobDetailView);
    expect(screen.getByRole('button', { name: /retry job/i })).toBeInTheDocument();
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
});
