/**
 * SchedulerView.test.ts
 *
 * Verifies that SchedulerView renders correctly and the primary actions call
 * the SDK with the correct shape:
 *  - Job table is populated from client.scheduler.listJobs on mount
 *  - "Create Job" button opens the dialog
 *  - Create dialog calls client.scheduler.createJob with the right payload
 *  - "Run Now" invokes client.scheduler.triggerJob with the correct job ID
 *  - Delete button invokes client.scheduler.deleteJob
 *  - Error states surface when the SDK rejects
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mock SDK client
// ---------------------------------------------------------------------------

const { mockListJobs, mockCreateJob, mockDeleteJob, mockTriggerJob, mockPauseJob, mockResumeJob, mockUpdateJob } = vi.hoisted(() => ({
  mockListJobs: vi.fn(),
  mockCreateJob: vi.fn(),
  mockDeleteJob: vi.fn(),
  mockTriggerJob: vi.fn(),
  mockPauseJob: vi.fn(),
  mockResumeJob: vi.fn(),
  mockUpdateJob: vi.fn(),
}));

const P = vi.hoisted(() => ({
  getStatus: vi.fn(),
  listNodes: vi.fn(),
  getNode: vi.fn(),
  cordonNode: vi.fn(),
  uncordonNode: vi.fn(),
  drainNode: vi.fn(),
  deregisterNode: vi.fn(),
  listPlacements: vi.fn(),
  getPlacement: vi.fn(),
  release: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    scheduler: {
      listJobs: mockListJobs,
      createJob: mockCreateJob,
      deleteJob: mockDeleteJob,
      triggerJob: mockTriggerJob,
      pauseJob: mockPauseJob,
      resumeJob: mockResumeJob,
      updateJob: mockUpdateJob,
      ...P,
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  navigateTo: vi.fn(),
  routerStore: { route: { name: 'scheduler', params: {}, hash: '#/scheduler' } },
}));

// Stub window.confirm so delete tests don't hang
vi.stubGlobal('confirm', vi.fn().mockReturnValue(true));

import SchedulerView from '../src/views/async/SchedulerView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { showToast } from '../src/stores/toast.svelte.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const JOBS = [
  {
    id: 'job-001',
    name: 'nightly-cleanup',
    type: 'cron',
    schedule: '0 0 * * *',
    handler: 'cleanup.run',
    status: 'active',
    next_run: '2025-04-18T00:00:00Z',
    run_count: 7,
    fail_count: 0,
    payload: null,
  },
  {
    id: 'job-002',
    name: 'send-report',
    type: 'one_time',
    schedule: undefined,
    handler: 'reports.send',
    status: 'paused',
    next_run: undefined,
    run_count: 0,
    fail_count: 0,
    payload: null,
  },
];

const STATUS = {
  running: true,
  stats: { job_count: 2, running_jobs: 1, success_count: 5, failure_count: 0, started_at: '2025-04-17T00:00:00Z' },
  timestamp: '2025-04-17T12:00:00Z',
};

const NODES = [
  {
    facts: { node_id: 'node-a', capacity: { cpu: 4 } },
    status: { unschedulable: false, draining: false },
    lease_id: 1,
    ttl_seconds: 30,
    allocated: { cpu: 1 },
    placements: 1,
    registered_at: '2025-04-17T00:00:00Z',
  },
  {
    facts: { node_id: 'node-b', capacity: { cpu: 8 } },
    status: { unschedulable: true, draining: false },
    lease_id: 2,
    ttl_seconds: 30,
    placements: 0,
    registered_at: '2025-04-17T00:00:00Z',
  },
];

const PLACEMENTS = [
  {
    workload_id: 'wl-1',
    group: 'web',
    node_id: 'node-a',
    resources: { cpu: 1 },
    score: 0.5,
    created_at: '2025-04-17T01:00:00Z',
  },
];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SchedulerView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListJobs.mockResolvedValue({ jobs: JOBS });
    mockCreateJob.mockResolvedValue({ job: { ...JOBS[0], id: 'job-new', name: 'new-job' } });
    mockDeleteJob.mockResolvedValue({ deleted: true });
    mockTriggerJob.mockResolvedValue({ triggered: true, job_id: 'job-001' });
    mockPauseJob.mockResolvedValue({});
    mockResumeJob.mockResolvedValue({});
    mockUpdateJob.mockResolvedValue({ job: JOBS[0] });
    P.getStatus.mockResolvedValue(STATUS);
    P.listNodes.mockResolvedValue(NODES);
    P.getNode.mockResolvedValue(NODES[0]);
    P.cordonNode.mockResolvedValue(NODES[0]);
    P.uncordonNode.mockResolvedValue(NODES[1]);
    P.drainNode.mockResolvedValue([PLACEMENTS[0]]);
    P.deregisterNode.mockResolvedValue(undefined);
    P.listPlacements.mockResolvedValue(PLACEMENTS);
    P.getPlacement.mockResolvedValue(PLACEMENTS[0]);
    P.release.mockResolvedValue(undefined);
    vi.mocked(getSDKClient).mockResolvedValue({
      scheduler: {
        listJobs: mockListJobs,
        createJob: mockCreateJob,
        deleteJob: mockDeleteJob,
        triggerJob: mockTriggerJob,
        pauseJob: mockPauseJob,
        resumeJob: mockResumeJob,
        updateJob: mockUpdateJob,
      ...P,
      },
    } as never);
  });

  it('calls listJobs on mount', async () => {
    render(SchedulerView);
    await waitFor(() => {
      expect(mockListJobs).toHaveBeenCalledWith(
        expect.objectContaining({ limit: 100 }),
      );
    });
  });

  it('renders job names from listJobs response', async () => {
    render(SchedulerView);
    await waitFor(() => {
      expect(screen.getByText('nightly-cleanup')).toBeInTheDocument();
      expect(screen.getByText('send-report')).toBeInTheDocument();
    });
  });

  it('renders the "Create Job" button', async () => {
    render(SchedulerView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Create Job/i })).toBeInTheDocument();
    });
  });

  it('clicking "Create Job" opens the dialog', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: /Create Job/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Job/i }));
    expect(screen.getByRole('dialog', { name: /Create Job/i })).toBeInTheDocument();
  });

  it('calls client.scheduler.createJob with correct shape for cron job', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: /Create Job/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Job/i }));

    const dialog = screen.getByRole('dialog', { name: /Create Job/i });

    // Fill job name
    const nameInput = dialog.querySelector('input[placeholder="e.g. nightly-cleanup"]') as HTMLInputElement;
    expect(nameInput).toBeDefined();
    await fireEvent.input(nameInput, { target: { value: 'my-cron-job' } });

    // Fill cron schedule (default type is cron so the field should be visible)
    const scheduleInput = dialog.querySelector('input[placeholder="0 0 * * *"]') as HTMLInputElement;
    expect(scheduleInput).toBeDefined();
    await fireEvent.input(scheduleInput, { target: { value: '*/5 * * * *' } });

    // Fill handler
    const handlerInput = dialog.querySelector('input[placeholder="e.g. cleanup.run"]') as HTMLInputElement;
    expect(handlerInput).toBeDefined();
    await fireEvent.input(handlerInput, { target: { value: 'my.handler' } });

    await fireEvent.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(mockCreateJob).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-cron-job',
          type: 'cron',
          schedule: '*/5 * * * *',
          handler: 'my.handler',
        }),
      );
    });
  });

  it('shows validation error when Create is submitted without a name', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: /Create Job/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Job/i }));

    // Click Create without filling in a name
    await fireEvent.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Job name is required/i)).toBeInTheDocument();
    });
    expect(mockCreateJob).not.toHaveBeenCalled();
  });

  it('renders "Run Now" buttons for each job', async () => {
    render(SchedulerView);
    await waitFor(() => {
      const runBtns = screen.getAllByRole('button', { name: /Run job .* now/i });
      expect(runBtns).toHaveLength(JOBS.length);
    });
  });

  it('calls client.scheduler.triggerJob with the correct job ID', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: /Run job nightly-cleanup now/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Run job nightly-cleanup now/i }));

    await waitFor(() => {
      expect(mockTriggerJob).toHaveBeenCalledWith('job-001');
    });
  });

  it('calls client.scheduler.deleteJob with the correct job ID', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: /Delete job nightly-cleanup/i }));

    // Click the row-level delete button — this opens the confirm dialog.
    await fireEvent.click(screen.getByRole('button', { name: /Delete job nightly-cleanup/i }));

    // The confirm dialog should now be visible; click the "Delete" confirm button.
    await waitFor(() => screen.getByRole('dialog', { name: /Delete job confirmation/i }));
    const confirmBtn = screen.getByRole('button', { name: /^Delete$/i });
    await fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(mockDeleteJob).toHaveBeenCalledWith('job-001');
    });
  });

  it('shows error alert when listJobs fails', async () => {
    mockListJobs.mockRejectedValueOnce(new Error('scheduler not running'));
    render(SchedulerView);
    await waitFor(() => {
      expect(screen.getByText(/scheduler not running/i)).toBeInTheDocument();
    });
  });

  it('shows error when cron schedule is missing for a cron job', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: /Create Job/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Job/i }));

    const dialog = screen.getByRole('dialog', { name: /Create Job/i });

    // Fill name and handler but leave schedule empty
    const nameInput = dialog.querySelector('input[placeholder="e.g. nightly-cleanup"]') as HTMLInputElement;
    await fireEvent.input(nameInput, { target: { value: 'test-job' } });
    const handlerInput = dialog.querySelector('input[placeholder="e.g. cleanup.run"]') as HTMLInputElement;
    await fireEvent.input(handlerInput, { target: { value: 'my.handler' } });

    await fireEvent.click(screen.getByRole('button', { name: /^Create$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Cron schedule is required/i)).toBeInTheDocument();
    });
    expect(mockCreateJob).not.toHaveBeenCalled();
  });

  // -------------------------------------------------------------------------
  // Placement section
  // -------------------------------------------------------------------------

  it('renders placement status, nodes and placements', async () => {
    render(SchedulerView);
    await waitFor(() => {
      expect(screen.getByRole('table', { name: 'Placement nodes' })).toBeInTheDocument();
      expect(screen.getByRole('table', { name: 'Workload placements' })).toBeInTheDocument();
    });
    expect(P.getStatus).toHaveBeenCalled();
    expect(P.listNodes).toHaveBeenCalled();
    expect(P.listPlacements).toHaveBeenCalled();
    expect(screen.getByText('wl-1')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Cordon node node-a' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Uncordon node node-b' })).toBeInTheDocument();
    expect(screen.getByText('cordoned')).toBeInTheDocument();
  });

  it('shows empty-state lines when there are no nodes or placements', async () => {
    P.listNodes.mockResolvedValue([]);
    P.listPlacements.mockResolvedValue([]);
    render(SchedulerView);
    await waitFor(() => {
      expect(screen.getByText('No placement nodes registered.')).toBeInTheDocument();
      expect(screen.getByText('No workload placements.')).toBeInTheDocument();
    });
  });

  it('cordon calls cordonNode with the node id and reloads', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: 'Cordon node node-a' }));
    const before = P.listNodes.mock.calls.length;
    await fireEvent.click(screen.getByRole('button', { name: 'Cordon node node-a' }));
    await waitFor(() => {
      expect(P.cordonNode).toHaveBeenCalledWith('node-a');
      expect(P.listNodes.mock.calls.length).toBeGreaterThan(before);
    });
    expect(P.uncordonNode).not.toHaveBeenCalled();
  });

  it('drain is called only after the confirm and toasts the moved count', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: 'Drain node node-a' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Drain node node-a' }));
    const dialog = screen.getByRole('dialog', { name: 'Confirm drain' });
    expect(dialog).toHaveTextContent('node-a');
    expect(P.drainNode).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: /^Drain$/ }));
    await waitFor(() => {
      expect(P.drainNode).toHaveBeenCalledWith('node-a');
      expect(vi.mocked(showToast)).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'success', message: expect.stringContaining('1 placement') }),
      );
    });
  });

  it('release is called only after the confirm', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: 'Release placement wl-1' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Release placement wl-1' }));
    expect(screen.getByRole('dialog', { name: 'Confirm release' })).toBeInTheDocument();
    expect(P.release).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: /^Release$/ }));
    await waitFor(() => {
      expect(P.release).toHaveBeenCalledWith('wl-1');
    });
  });

  it('a deregisterNode rejection toasts its message', async () => {
    P.deregisterNode.mockRejectedValueOnce(new Error('lease revoke failed'));
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: 'Deregister node node-b' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Deregister node node-b' }));
    await fireEvent.click(screen.getByRole('button', { name: /^Deregister$/ }));
    await waitFor(() => {
      expect(P.deregisterNode).toHaveBeenCalledWith('node-b');
      expect(vi.mocked(showToast)).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'error', message: expect.stringContaining('lease revoke failed') }),
      );
    });
  });

  it('details opens a dialog from getNode', async () => {
    render(SchedulerView);
    await waitFor(() => screen.getByRole('button', { name: 'Details for node node-a' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Details for node node-a' }));
    await waitFor(() => {
      expect(P.getNode).toHaveBeenCalledWith('node-a');
      expect(screen.getByRole('dialog', { name: 'Node details' })).toBeInTheDocument();
    });
  });
});
