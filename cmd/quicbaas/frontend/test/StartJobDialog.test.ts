/**
 * StartJobDialog.test.ts
 *
 * Verifies that StartJobDialog mounts, fills required fields, submits the
 * request with the correct shape, and routes to /jobs on success.
 *
 * The lead is chosen from the tenant's agent catalog: the Agents that declare
 * the lead role and the tenant's pools. "Project default" sends no lead, and
 * the server takes the project's default lead.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// vi.mock() is hoisted, so its factory must not reference top-level consts.
vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
  refresh: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  navigateTo: vi.fn(),
  routerStore: { route: { name: 'jobs', params: {}, hash: '#/jobs' } },
}));

import StartJobDialog from '../src/components/orchestrator/StartJobDialog.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { navigateTo } from '../src/stores/router.svelte.ts';
import { showToast } from '../src/stores/toast.svelte.ts';

const leadAgent = { id: 'claude-lead', name: 'Claude lead', roles: ['lead'], backend: 'claude', model: 'claude-opus-5-5' };
const coder = { id: 'claude-coder', name: 'Claude coder', roles: ['coding'], backend: 'claude', model: 'claude-opus-5-5' };
const leads = { id: 'leads', name: 'Leads', agents: ['claude-lead'] };

let startJob: ReturnType<typeof vi.fn>;

function mockClient(agents: unknown[] = [leadAgent, coder], pools: unknown[] = [leads]): void {
  startJob = vi.fn().mockResolvedValue({ job_id: 'test-job-id-abc', timestamp: 0 });
  vi.mocked(getSDKClient).mockResolvedValue({
    orchestrator: { startJob },
    agents: {
      listAgents: vi.fn().mockResolvedValue(agents),
      listPools: vi.fn().mockResolvedValue(pools),
    },
  } as never);
}

function renderDialog(props: Record<string, unknown> = {}) {
  return render(StartJobDialog, { props: { open: true, ...props } });
}

describe('StartJobDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockClient();
  });

  it('renders form fields when open is true', () => {
    renderDialog();
    expect(screen.getByLabelText(/Description/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Lead Agent/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Max Iterations/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Poll Interval/i)).toBeInTheDocument();
  });

  it('prefills workflow name when prefillWorkflow is provided', () => {
    renderDialog({ prefillWorkflow: 'my-workflow' });
    // The Workflow Name field is a RefPicker now: a prefilled workflow
    // disables it (rather than marking it readOnly) so it cannot be reopened
    // and searched away from the workflow the dialog was launched for.
    const input = screen.getByLabelText(/Workflow Name/i) as HTMLInputElement;
    expect(input.value).toBe('my-workflow');
    expect(input.disabled).toBe(true);
  });

  it('shows validation error when description is empty on submit', async () => {
    renderDialog();
    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));
    expect(screen.getByText(/Description is required/i)).toBeInTheDocument();
  });

  it('offers the lead Agents and the pools, and never an Agent without the lead role', async () => {
    renderDialog();
    const select = screen.getByLabelText(/Lead Agent/i) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option')).toHaveLength(3));
    const values = Array.from(select.querySelectorAll('option')).map((o) => o.value);
    expect(values).toEqual(['', 'agent:claude-lead', 'pool:leads']);
  });

  it('sends the chosen lead as a reference', async () => {
    renderDialog();
    const select = screen.getByLabelText(/Lead Agent/i) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option')).toHaveLength(3));
    await fireEvent.input(screen.getByLabelText(/Description/i), { target: { value: 'Build the feature' } });
    await fireEvent.change(select, { target: { value: 'pool:leads' } });
    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));
    await waitFor(() => expect(startJob).toHaveBeenCalled());
    expect(startJob.mock.calls[0][0].lead).toEqual({ pool_id: 'leads' });
    expect(startJob.mock.calls[0][0].description).toBe('Build the feature');
  });

  it('sends no lead for the project default, and reports success', async () => {
    renderDialog();
    await fireEvent.input(screen.getByLabelText(/Description/i), { target: { value: 'Deploy service' } });
    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));
    await waitFor(() => expect(vi.mocked(showToast)).toHaveBeenCalledWith('success', expect.any(String)));
    expect(startJob.mock.calls[0][0].lead).toEqual({});
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('/jobs');
  });

  it('still starts a job when the catalog cannot be read', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: { startJob: vi.fn().mockResolvedValue({ job_id: 'j', timestamp: 0 }) },
      agents: {
        listAgents: vi.fn().mockRejectedValue(new Error('503')),
        listPools: vi.fn().mockRejectedValue(new Error('503')),
      },
    } as never);
    renderDialog();
    const select = screen.getByLabelText(/Lead Agent/i) as HTMLSelectElement;
    await waitFor(() => expect(select.querySelectorAll('option')).toHaveLength(1));
    const startBtn = screen.getByRole('button', { name: /Start Job/i }) as HTMLButtonElement;
    expect(startBtn.disabled).toBe(false);
  });

  it('reports a refused start', async () => {
    startJob.mockRejectedValue(new Error('agent catalog: unknown agent ghost'));
    renderDialog();
    await fireEvent.input(screen.getByLabelText(/Description/i), { target: { value: 'x' } });
    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));
    await waitFor(() => expect(screen.getByText(/Failed to start job/i)).toBeInTheDocument());
  });

  it('closes the dialog when Cancel is clicked', async () => {
    renderDialog();
    await fireEvent.click(screen.getByRole('button', { name: /^Cancel$/i }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
});

describe('StartJobDialog — run-time prompt material', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockClient();
  });

  it('sends the appended prompt for every session of the job', async () => {
    renderDialog();
    await fireEvent.input(screen.getByLabelText(/Description/i), { target: { value: 'Fix the flaky test' } });
    await fireEvent.input(screen.getByLabelText(/Append to every session/i), { target: { value: 'touch only pkg/cache' } });
    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));
    await waitFor(() => expect(startJob).toHaveBeenCalled());
    expect(startJob.mock.calls[0][0].prompt_append).toBe('touch only pkg/cache');
  });

  it('sends none when the field is left blank', async () => {
    renderDialog();
    await fireEvent.input(screen.getByLabelText(/Description/i), { target: { value: 'Fix the flaky test' } });
    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));
    await waitFor(() => expect(startJob).toHaveBeenCalled());
    expect(startJob.mock.calls[0][0].prompt_append).toBeUndefined();
  });
});
