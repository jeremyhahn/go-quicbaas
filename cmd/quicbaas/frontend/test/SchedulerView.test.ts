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
    vi.mocked(getSDKClient).mockResolvedValue({
      scheduler: {
        listJobs: mockListJobs,
        createJob: mockCreateJob,
        deleteJob: mockDeleteJob,
        triggerJob: mockTriggerJob,
        pauseJob: mockPauseJob,
        resumeJob: mockResumeJob,
        updateJob: mockUpdateJob,
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
});
