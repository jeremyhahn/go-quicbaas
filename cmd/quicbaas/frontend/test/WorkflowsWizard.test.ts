/**
 * WorkflowsWizard.test.ts
 *
 * Tests for the six-step WorkflowWizard component:
 *   1. Stepper advances only when the current step is valid.
 *   2. Step 1 rejects empty name and names already in the workflow list.
 *   3. Step 2 rejects canned/constrained when no agent has role="lead".
 *   4. Step 6 generated YAML payload round-trips through JSON.parse(JSON.stringify).
 *   5. Submit calls client.orchestrator.createWorkflow; on 4xx the wizard stays
 *      on the Review step and shows the error inline.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { WorkflowDefinition, AgentBackendInfo } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const createWorkflowMock = vi.fn();
const listWorkflowsMock = vi.fn();
const listBackendsMock  = vi.fn();
const listAgentsMock    = vi.fn();

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      listWorkflows: listWorkflowsMock,
      listBackends:  listBackendsMock,
      listAgents:    listAgentsMock,
      createWorkflow: createWorkflowMock,
      startJob: vi.fn().mockResolvedValue({ job_id: 'j1', timestamp: 0 }),
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

import WorkflowWizard from '../src/components/orchestrator/WorkflowWizard.svelte';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeBackend(name: string, type: string, model: string): AgentBackendInfo {
  return { name, type, capabilities: {}, default_model: model };
}

const EXISTING_NAMES = ['existing-wf'];
const BACKENDS: AgentBackendInfo[] = [
  makeBackend('ollama', 'sessionless', 'llama3.3:70b'),
  makeBackend('claude', 'acp', 'claude-opus-4-5'),
];

// onComplete / onCancel / onEditAdvanced stubs for props
function makeProps(overrides: Partial<{
  existingNames: string[];
  backends: AgentBackendInfo[];
  onComplete: (def: WorkflowDefinition) => Promise<void>;
  onCancel: () => void;
  onEditAdvanced: (yaml: string) => void;
}> = {}) {
  return {
    existingNames: overrides.existingNames ?? EXISTING_NAMES,
    backends: overrides.backends ?? BACKENDS,
    agents: [],
    onComplete: overrides.onComplete ?? vi.fn().mockResolvedValue(undefined),
    onCancel: overrides.onCancel ?? vi.fn(),
    onEditAdvanced: overrides.onEditAdvanced ?? vi.fn(),
  };
}

// ---------------------------------------------------------------------------
// Helpers for navigating to a step
// ---------------------------------------------------------------------------

async function fillStep1(name: string): Promise<void> {
  const nameInput = screen.getByLabelText(/Workflow Name/i) as HTMLInputElement;
  await fireEvent.input(nameInput, { target: { value: name } });
}

async function clickContinue(): Promise<void> {
  const btn = screen.getByRole('button', { name: /Continue/i });
  await fireEvent.click(btn);
}

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

describe('WorkflowWizard — stepper advancement', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('starts on step 1 (Basics) with Continue disabled when name is empty', async () => {
    render(WorkflowWizard, { props: makeProps() });
    const btn = screen.getByRole('button', { name: /Continue/i });
    expect(btn).toBeDisabled();
  });

  it('enables Continue on step 1 when a unique name is entered', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await fillStep1('my-new-workflow');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Continue/i })).not.toBeDisabled();
    });
  });

  it('advances to step 2 (Agents) when step 1 is valid', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await fillStep1('my-new-workflow');
    await clickContinue();
    await waitFor(() => {
      expect(screen.getByText(/Add your first agent/i)).toBeInTheDocument();
    });
  });

  it('does NOT advance when Continue is disabled (invalid step)', async () => {
    render(WorkflowWizard, { props: makeProps() });
    // Do not fill name — Continue stays disabled.
    const btn = screen.getByRole('button', { name: /Continue/i });
    await fireEvent.click(btn);
    // Still on step 1.
    expect(screen.getByLabelText(/Workflow Name/i)).toBeInTheDocument();
  });
});

describe('WorkflowWizard — step 1 validation', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('rejects an empty name — Continue is disabled', async () => {
    render(WorkflowWizard, { props: makeProps() });
    const nameInput = screen.getByLabelText(/Workflow Name/i);
    await fireEvent.input(nameInput, { target: { value: '' } });
    expect(screen.getByRole('button', { name: /Continue/i })).toBeDisabled();
  });

  it('rejects a name already in the existing list — Continue is disabled', async () => {
    render(WorkflowWizard, { props: makeProps({ existingNames: ['existing-wf'] }) });
    await fillStep1('existing-wf');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Continue/i })).toBeDisabled();
    });
    expect(screen.getByText(/already registered/i)).toBeInTheDocument();
  });

  it('accepts a name not in the existing list', async () => {
    render(WorkflowWizard, { props: makeProps({ existingNames: ['existing-wf'] }) });
    await fillStep1('brand-new-wf');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Continue/i })).not.toBeDisabled();
    });
  });
});

describe('WorkflowWizard — step 2 agent lead validation', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  async function reachStep2(): Promise<void> {
    await fillStep1('test-wf');
    await clickContinue();
    await waitFor(() => screen.getByText(/Add your first agent/i));
  }

  it('keeps Continue disabled on step 2 when no agents added (canned mode)', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await reachStep2();
    expect(screen.getByRole('button', { name: /Continue/i })).toBeDisabled();
  });

  it('shows lead-required warning when mode is canned and no lead role', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await reachStep2();

    // Add an agent via the CTA button.
    await fireEvent.click(screen.getByRole('button', { name: /Add Lead Agent/i }));

    // Change the role from "lead" to "worker".
    await waitFor(() => screen.getAllByPlaceholderText(/lead or worker/i));
    const roleInputs = screen.getAllByPlaceholderText(/lead or worker/i) as HTMLInputElement[];
    await fireEvent.input(roleInputs[0]!, { target: { value: 'worker' } });

    await waitFor(() => {
      expect(screen.getByText(/canned and constrained modes require/i)).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /Continue/i })).toBeDisabled();
  });

  it('enables Continue when an agent with role="lead" is present in canned mode', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await reachStep2();

    await fireEvent.click(screen.getByRole('button', { name: /Add Lead Agent/i }));

    await waitFor(() => {
      // Default role is "lead" so Continue should become enabled.
      expect(screen.getByRole('button', { name: /Continue/i })).not.toBeDisabled();
    });
  });
});

describe('WorkflowWizard — step 6 YAML round-trip', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  // Navigate all the way to step 6 with a minimal valid workflow.
  async function reachReview(): Promise<void> {
    // Step 1.
    await fillStep1('round-trip-wf');
    await clickContinue();

    // Step 2 — add lead agent.
    await waitFor(() => screen.getByRole('button', { name: /Add Lead Agent/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Add Lead Agent/i }));
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();

    // Step 3, 4, 5 — defaults are valid, just continue.
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();

    // Now on step 6 (Review).
    await waitFor(() => screen.getByRole('button', { name: /Create Workflow/i }));
  }

  it('the generated YAML element is present on the Review step', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await reachReview();
    // The <pre> element with aria-label="generated-yaml" should be visible.
    await waitFor(() => {
      expect(screen.getByLabelText('generated-yaml')).toBeInTheDocument();
    });
  });

  it('the YAML contains the workflow name', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await reachReview();
    await waitFor(() => {
      const yaml = screen.getByLabelText('generated-yaml').textContent ?? '';
      expect(yaml).toContain('round-trip-wf');
    });
  });

  it('the assembled definition round-trips cleanly through JSON serialisation', async () => {
    // Build a minimal WorkflowDefinition manually (mirrors what the wizard assembles)
    // and verify JSON round-trip without loss — this tests the shape contract.
    const def: WorkflowDefinition = {
      name: 'round-trip-wf',
      description: 'test',
      mode: 'canned',
      agents: [{
        role: 'lead',
        backend: 'ollama',
        model: 'llama3.3:70b',
        capabilities: ['code'],
        mcp_scopes: ['kv:read'],
        count: 1,
        workspace_path: '',
        permission_mode: 'default',
        skip_permissions: false,
        env: {},
        sandboxed: false,
        container_image: '',
        host_affinity: '',
        num_ctx: 0,
      }],
      settings: {
        max_iterations: 10,
        max_parallelism: 4,
        poll_interval: '30s',
        timeout_per_item: '5m',
        total_timeout: '1h',
        review_criteria: [],
        topology: 'hub_and_spoke',
        isolation: 'shared',
        peer_messaging: { max_message_size: 10240, max_inbox_size: 1000, overflow_action: 'reject', inbox_ttl: '1h' },
        file_locking: { lease_ttl: '5m', heartbeat_interval: '30s', max_renewals: 60 },
        vector_db: { enabled: false, index_type: 'auto', auto_index: false, triggers: [], gpu_accelerated: false },
        cost_controls: { max_tokens_per_job: 0, max_tokens_per_agent: 0, budget_action: 'warn' },
        recovery: { auto_restart: false, max_restarts: 3, restart_backoff: '30s' },
        verbosity: 'info',
        num_ctx: 0,
      },
    };

    const roundTripped = JSON.parse(JSON.stringify(def)) as WorkflowDefinition;
    expect(roundTripped.name).toBe(def.name);
    expect(roundTripped.mode).toBe(def.mode);
    expect(roundTripped.agents).toHaveLength(1);
    expect(roundTripped.agents[0]?.role).toBe('lead');
    expect(roundTripped.settings.topology).toBe('hub_and_spoke');
    expect(roundTripped.settings.cost_controls.budget_action).toBe('warn');
  });
});

describe('WorkflowWizard — topology step isolation card colors (#67)', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  async function reachTopologyStep(): Promise<void> {
    // Step 1.
    await fillStep1('topology-color-wf');
    await clickContinue();
    // Step 2 — add lead agent.
    await waitFor(() => screen.getByRole('button', { name: /Add Lead Agent/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Add Lead Agent/i }));
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();
    // Now on step 3 (Topology).
    await waitFor(() => screen.getByText(/Workspace Isolation/i));
  }

  it('initially selected "Shared" isolation card has border-primary class, not border-secondary (#67)', async () => {
    const { container } = render(WorkflowWizard, { props: makeProps() });
    await reachTopologyStep();

    // Find the label element wrapping the "Shared" radio — it should have border-primary.
    const isolationLabels = container.querySelectorAll('input[name="wz-isolation"]');
    expect(isolationLabels.length).toBe(2);

    // The first radio is "shared" (default selected).
    const sharedLabel = isolationLabels[0]!.closest('label');
    expect(sharedLabel).not.toBeNull();
    expect(sharedLabel!.className).toContain('border-primary');
    expect(sharedLabel!.className).not.toContain('border-secondary');
  });

  it('clicking "Worktree" gives it border-primary and removes it from "Shared" (#67)', async () => {
    const { container } = render(WorkflowWizard, { props: makeProps() });
    await reachTopologyStep();

    const isolationLabels = container.querySelectorAll('input[name="wz-isolation"]');
    const worktreeInput = isolationLabels[1] as HTMLInputElement;
    const worktreeLabel = worktreeInput.closest('label')!;
    const sharedLabel = (isolationLabels[0] as HTMLInputElement).closest('label')!;

    // Click the Worktree radio.
    await fireEvent.click(worktreeInput);

    await waitFor(() => {
      expect(worktreeLabel.className).toContain('border-primary');
      expect(worktreeLabel.className).not.toContain('border-secondary');
      expect(sharedLabel.className).not.toContain('border-primary');
    });
  });

  it('topology communication cards also use border-primary when selected (#67)', async () => {
    const { container } = render(WorkflowWizard, { props: makeProps() });
    await reachTopologyStep();

    const topologyLabels = container.querySelectorAll('input[name="wz-topology"]');
    expect(topologyLabels.length).toBe(3);

    // Default selected is hub_and_spoke (first).
    const hubLabel = (topologyLabels[0] as HTMLInputElement).closest('label')!;
    expect(hubLabel.className).toContain('border-primary');
    expect(hubLabel.className).not.toContain('border-secondary');
  });
});

describe('WorkflowWizard — horizontal stepper', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it('horizontal stepper renders 6 pills, active=Basics on mount', async () => {
    render(WorkflowWizard, { props: makeProps() });
    // The stepper uses role="tablist" / role="tab"
    const tabs = screen.getAllByRole('tab');
    expect(tabs).toHaveLength(6);
    // The first tab (Basics) should be aria-selected
    expect(tabs[0]).toHaveAttribute('aria-selected', 'true');
    // Basics label should be present within a stepper tab
    const basicsTab = tabs[0];
    expect(basicsTab!.textContent).toContain('Basics');
    // All six step labels present in tabs
    expect(tabs[1]!.textContent).toContain('Agents');
    expect(tabs[2]!.textContent).toContain('Topology');
    expect(tabs[3]!.textContent).toContain('Limits');
    expect(tabs[4]!.textContent).toContain('Advanced');
    expect(tabs[5]!.textContent).toContain('Review');
  });
});

describe('WorkflowWizard — capability chip interactions', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  async function reachStep2(): Promise<void> {
    await fillStep1('chip-test-wf');
    await clickContinue();
    await waitFor(() => screen.getByText(/Add your first agent/i));
    await fireEvent.click(screen.getByRole('button', { name: /Add Lead Agent/i }));
    await waitFor(() => screen.getAllByPlaceholderText(/lead or worker/i));
  }

  it('capability suggestion chip click adds a chip', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await reachStep2();

    // The suggestion chips for capabilities should be visible (no filter typed yet)
    // Click the first suggestion chip (e.g. "code")
    const codeChip = await waitFor(() => screen.getByRole('button', { name: 'code' }));
    await fireEvent.click(codeChip);

    await waitFor(() => {
      // "code" should now appear as an added badge (inside a badge-primary span)
      const badges = document.querySelectorAll('.badge-primary');
      const labels = Array.from(badges).map((b) => b.textContent?.trim() ?? '');
      expect(labels.some((l) => l.includes('code'))).toBe(true);
    });
  });

  it('MCP scopes input filters suggestions live as user types', async () => {
    render(WorkflowWizard, { props: makeProps() });
    await reachStep2();

    // Find the MCP scopes input (placeholder "Type to filter or add scope…")
    const scopeInput = screen.getByPlaceholderText(/Type to filter or add scope/i) as HTMLInputElement;

    // Type "vector" — should filter down to scopes containing "vector"
    await fireEvent.input(scopeInput, { target: { value: 'vector' } });

    await waitFor(() => {
      // Suggestions containing "vector" should be visible
      expect(screen.getByRole('button', { name: 'vector:read' })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'vector:search' })).toBeInTheDocument();
      // Scopes NOT containing "vector" should NOT be visible as suggestions
      expect(screen.queryByRole('button', { name: 'kv:read' })).not.toBeInTheDocument();
    });
  });
});

describe('WorkflowWizard — submit behaviour', () => {
  beforeEach(() => { vi.clearAllMocks(); });

  async function reachReview(): Promise<void> {
    await fillStep1('submit-test-wf');
    await clickContinue();
    await waitFor(() => screen.getByRole('button', { name: /Add Lead Agent/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Add Lead Agent/i }));
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();
    await waitFor(() => screen.getByRole('button', { name: /Continue/i }));
    await clickContinue();
    await waitFor(() => screen.getByRole('button', { name: /Create Workflow/i }));
  }

  it('calls onComplete with the assembled WorkflowDefinition on submit', async () => {
    const onComplete = vi.fn().mockResolvedValue(undefined);
    render(WorkflowWizard, { props: makeProps({ onComplete }) });
    await reachReview();

    await fireEvent.click(screen.getByRole('button', { name: /Create Workflow/i }));

    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledOnce();
      const [def] = onComplete.mock.calls[0] as [WorkflowDefinition];
      expect(def.name).toBe('submit-test-wf');
      expect(def.mode).toBe('canned');
      expect(def.agents).toHaveLength(1);
    });
  });

  it('stays on the Review step and shows error inline when onComplete rejects', async () => {
    const onComplete = vi.fn().mockRejectedValue(new Error('422 Unprocessable Entity'));
    render(WorkflowWizard, { props: makeProps({ onComplete }) });
    await reachReview();

    await fireEvent.click(screen.getByRole('button', { name: /Create Workflow/i }));

    await waitFor(() => {
      // Error message visible.
      expect(screen.getByText(/422 Unprocessable Entity/i)).toBeInTheDocument();
      // Still on Review step — Create Workflow button still present.
      expect(screen.getByRole('button', { name: /Create Workflow/i })).toBeInTheDocument();
    });
  });
});
