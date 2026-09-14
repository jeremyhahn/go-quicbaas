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
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

const { storeMock } = vi.hoisted(() => ({
  storeMock: {
    jobs: [] as unknown[],
    tasks: [] as unknown[],
    jobsLoading: false,
    activeTenantID: 'acme',
  },
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
  selectJob: vi.fn(),
  refresh: vi.fn(),
  isTerminalJobState: (state: string) => state === 'completed' || state === 'failed',
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeJobDetail: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: { cancelJob: vi.fn().mockResolvedValue(undefined) },
  }),
}));

vi.mock('../src/components/orchestrator/StartJobDialog.svelte', () => ({
  default: vi.fn(),
}));

import JobsView from '../src/views/orchestrator/JobsView.svelte';
import { routeJobDetail } from '../src/stores/router.svelte.ts';

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

describe('JobsView — the issue behind a job', () => {
  beforeEach(() => {
    storeMock.jobs = [factoryJob, leadJob];
    storeMock.tasks = [factoryTask];
    storeMock.jobsLoading = false;
    vi.mocked(routeJobDetail).mockClear();
  });

  it('chips the forge issue the job is an attempt at', () => {
    render(JobsView);
    const chip = screen.getByRole('link', { name: '#580' });
    expect(chip.getAttribute('href')).toBe('http://forge.example/jeremyhahn/go-quicbaas/issues/580');
    expect(chip.getAttribute('rel')).toContain('noopener');
  });

  it('finds the issue from the task when the job carries no task_id', () => {
    storeMock.jobs = [{ ...factoryJob, task_id: undefined }];
    render(JobsView);
    // The task still names the job it spawned, so the binding is readable.
    expect(screen.getByRole('link', { name: '#580' })).toBeInTheDocument();
  });

  it('shows no chip for a job with no forge issue behind it', () => {
    storeMock.jobs = [leadJob];
    storeMock.tasks = [];
    render(JobsView);
    expect(screen.queryByRole('link', { name: /^#\d+$/ })).toBeNull();
    expect(screen.getByText('An operator-started job')).toBeInTheDocument();
  });

  it('renders the chip as plain text when the source records no URL', () => {
    storeMock.tasks = [{ ...factoryTask, source: { kind: 'forge_issue', ref: '580', url: '' } }];
    render(JobsView);
    expect(screen.queryByRole('link', { name: '#580' })).toBeNull();
    expect(screen.getByTitle('Forge issue #580')).toBeInTheDocument();
  });

  it('opening the issue does not also open the job', async () => {
    render(JobsView);
    await fireEvent.click(screen.getByRole('link', { name: '#580' }));
    expect(vi.mocked(routeJobDetail)).not.toHaveBeenCalled();
  });

  it('clicking the row still opens the job detail', async () => {
    render(JobsView);
    await fireEvent.click(screen.getByText('An operator-started job'));
    expect(vi.mocked(routeJobDetail)).toHaveBeenCalledWith('job-lead-999');
  });

  it('keeps the description beside the chip', () => {
    render(JobsView);
    expect(screen.getByText('Wire the job event stream')).toBeInTheDocument();
  });
});
