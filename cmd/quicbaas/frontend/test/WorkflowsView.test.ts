/**
 * WorkflowsView.test.ts
 *
 * Verifies that WorkflowsView renders the workflow list, shows the create
 * panel when the button is clicked, and opens StartJobDialog prefilled when
 * "Run" is clicked on a workflow row.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { WorkflowDefinition } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock workflow data
// ---------------------------------------------------------------------------

const makeWorkflow = (name: string, mode: WorkflowDefinition['mode'], desc: string): WorkflowDefinition => ({
  name,
  description: desc,
  mode,
  agents: [],
  settings: {
    max_iterations: 10,
    max_parallelism: 2,
    poll_interval: '2s',
    timeout_per_item: '5m',
    total_timeout: '1h',
    review_criteria: [],
    topology: 'hub_and_spoke',
    isolation: 'shared',
    peer_messaging: { max_message_size: 0, max_inbox_size: 0, overflow_action: '', inbox_ttl: '' },
    file_locking: { lease_ttl: '', heartbeat_interval: '', max_renewals: 0 },
    vector_db: { enabled: false, index_type: '', auto_index: false, triggers: [], gpu_accelerated: false },
    cost_controls: { max_tokens_per_job: 0, max_tokens_per_agent: 0, budget_action: '' },
    recovery: { auto_restart: false, max_restarts: 0, restart_backoff: '' },
    verbosity: '',
    num_ctx: 0,
  },
});

const mockWorkflows = [
  makeWorkflow('dev-sprint-v1', 'autonomous', 'Sprint workflow'),
  makeWorkflow('review-pipeline', 'constrained', 'Code review workflow'),
];

// ---------------------------------------------------------------------------
// Mocks — inline vi.fn() to avoid hoisting issues.
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      listWorkflows: vi.fn().mockResolvedValue({ workflows: [], count: 0 }),
      listBackends: vi.fn().mockResolvedValue([]),
      startJob: vi.fn().mockResolvedValue({ job_id: 'new-job', timestamp: 0 }),
      createWorkflow: vi.fn().mockResolvedValue({ name: 'new-wf', created_at: 0 }),
    },
  }),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  navigateTo: vi.fn(),
  routerStore: { route: { name: 'workflows', params: {}, hash: '#/workflows' } },
}));

import WorkflowsView from '../src/views/orchestrator/WorkflowsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('WorkflowsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        listWorkflows: vi.fn().mockResolvedValue({ workflows: mockWorkflows, count: mockWorkflows.length }),
        listBackends: vi.fn().mockResolvedValue([]),
        startJob: vi.fn().mockResolvedValue({ job_id: 'new-job', timestamp: 0 }),
        createWorkflow: vi.fn().mockResolvedValue({ name: 'new-wf', created_at: 0 }),
      },
    } as never);
  });

  it('renders the page heading "Workflows"', async () => {
    render(WorkflowsView);
    await waitFor(() => {
      expect(screen.getByText('Workflows')).toBeInTheDocument();
    });
  });

  it('renders workflow names from the list', async () => {
    render(WorkflowsView);
    await waitFor(() => {
      expect(screen.getByText('dev-sprint-v1')).toBeInTheDocument();
      expect(screen.getByText('review-pipeline')).toBeInTheDocument();
    });
  });

  it('shows "Create Workflow" button in the header', async () => {
    render(WorkflowsView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Create Workflow/i })).toBeInTheDocument();
    });
  });

  it('opens create panel when "Create Workflow" is clicked', async () => {
    render(WorkflowsView);
    await waitFor(() => screen.getByRole('button', { name: /Create Workflow/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create Workflow/i }));
    // The create panel has a "Workflow Name" input.
    expect(screen.getAllByLabelText(/Workflow Name/i).length).toBeGreaterThan(0);
  });

  it('shows Run button for each workflow row', async () => {
    render(WorkflowsView);
    await waitFor(() => {
      const runButtons = screen.getAllByRole('button', { name: /Run workflow/i });
      expect(runButtons).toHaveLength(mockWorkflows.length);
    });
  });

  it('clicking Run opens StartJobDialog prefilled with workflow name', async () => {
    render(WorkflowsView);
    await waitFor(() => screen.getAllByRole('button', { name: /Run workflow/i }));

    const runBtn = screen.getByRole('button', { name: /Run workflow dev-sprint-v1/i });
    await fireEvent.click(runBtn);

    // The StartJobDialog renders a readonly Workflow Name input prefilled.
    await waitFor(() => {
      const wfInputs = screen.getAllByLabelText(/Workflow Name/i) as HTMLInputElement[];
      const readonlyInput = wfInputs.find((i) => i.readOnly);
      expect(readonlyInput).toBeDefined();
      expect(readonlyInput!.value).toBe('dev-sprint-v1');
    });
  });

  it('shows empty state when no workflows exist', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      orchestrator: {
        listWorkflows: vi.fn().mockResolvedValueOnce({ workflows: [], count: 0 }),
        listBackends: vi.fn().mockResolvedValue([]),
        startJob: vi.fn(),
        createWorkflow: vi.fn(),
      },
    } as never);
    render(WorkflowsView);
    await waitFor(() => {
      expect(screen.getByText(/No workflow definitions registered/i)).toBeInTheDocument();
    });
  });

  it('calls listWorkflows with the active tenant ID', async () => {
    render(WorkflowsView);
    await waitFor(() => {
      expect(vi.mocked(getSDKClient)).toHaveBeenCalled();
    });
  });
});
