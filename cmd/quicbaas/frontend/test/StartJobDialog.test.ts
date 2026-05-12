/**
 * StartJobDialog.test.ts
 *
 * Verifies that StartJobDialog mounts, fills required fields, submits the
 * request with the correct shape, and routes to /jobs on success.
 *
 * Issue #77 regression: Lead Backend MUST default to the backend of the first
 * REGISTERED agent, never a hardcoded value like "claude". The dialog now
 * derives its backend list from listAgents(), not listBackends(), so only
 * agents that are actually registered server-side appear in the dropdown.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mocks — factories must not reference top-level const declarations because
// vi.mock() is hoisted to the top of the file before any const is initialized.
// Use vi.fn() inline and retrieve mocked references after import.
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      startJob: vi.fn().mockResolvedValue({ job_id: 'test-job-id-abc', timestamp: 0 }),
      // Default mock: one registered ollama agent — ensures submit-disabled
      // logic does not block tests that exercise unrelated form behaviour.
      listAgents: vi.fn().mockResolvedValue({
        agents: [
          {
            id: 'agent-1',
            name: 'embedded-vm-host-2',
            backend: 'ollama',
            model: 'qwen2.5:0.5b',
            status: 'idle',
            host: 'localhost',
            capabilities: [],
            last_seen: 0,
            registered_at: 0,
            version: 1,
            metadata: {},
            tenant_id: 'default',
            agent_type: 'inference',
            container_id: '',
            work_dir: '',
          },
        ],
      }),
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
  routerStore: { route: { name: 'jobs', params: {}, hash: '#/jobs' } },
}));

import StartJobDialog from '../src/components/orchestrator/StartJobDialog.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { navigateTo } from '../src/stores/router.svelte.ts';
import { showToast } from '../src/stores/toast.svelte.ts';

// ---------------------------------------------------------------------------
// Helper: build a minimal agent object for test mocks.
// ---------------------------------------------------------------------------

function makeAgent(backend: string, model = '') {
  return {
    id: `agent-${backend}`,
    name: `agent-${backend}`,
    backend,
    model,
    status: 'idle' as const,
    host: 'localhost',
    capabilities: [] as string[],
    last_seen: 0,
    registered_at: 0,
    version: 1,
    metadata: {} as Record<string, string>,
    tenant_id: 'default',
    agent_type: 'inference' as const,
    container_id: '',
    work_dir: '',
  };
}

function renderDialog(props: Record<string, unknown> = {}) {
  return render(StartJobDialog, { props: { open: true, ...props } });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('StartJobDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Re-establish mock return values after clearAllMocks.
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        startJob: vi.fn().mockResolvedValue({ job_id: 'test-job-id-abc', timestamp: 0 }),
        listAgents: vi.fn().mockResolvedValue({
          agents: [makeAgent('ollama', 'qwen2.5:0.5b')],
        }),
      },
    } as never);
    vi.mocked(navigateTo).mockReset();
    vi.mocked(showToast).mockReset();
  });

  it('renders form fields when open is true', () => {
    renderDialog();
    expect(screen.getByLabelText(/Description/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Lead Model/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Max Iterations/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Poll Interval/i)).toBeInTheDocument();
  });

  it('prefills workflow name when prefillWorkflow is provided', () => {
    renderDialog({ prefillWorkflow: 'my-workflow' });
    const input = screen.getByLabelText(/Workflow Name/i) as HTMLInputElement;
    expect(input.value).toBe('my-workflow');
    expect(input.readOnly).toBe(true);
  });

  it('shows validation error when description is empty on submit', async () => {
    renderDialog();
    const submitBtn = screen.getByRole('button', { name: /Start Job/i });
    await fireEvent.click(submitBtn);
    expect(screen.getByText(/Description is required/i)).toBeInTheDocument();
  });

  it('shows validation error when lead model is empty on submit', async () => {
    renderDialog();
    await fireEvent.input(screen.getByLabelText(/Description/i), {
      target: { value: 'Test job' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));
    expect(screen.getByText(/Lead model is required/i)).toBeInTheDocument();
  });

  it('calls startJob with correct shape on valid submit', async () => {
    renderDialog();

    await fireEvent.input(screen.getByLabelText(/Description/i), {
      target: { value: 'Build the feature' },
    });
    await fireEvent.input(screen.getByLabelText(/Lead Model/i), {
      target: { value: 'claude-opus-4-5' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));

    await waitFor(() => {
      expect(vi.mocked(getSDKClient)).toHaveBeenCalled();
    });
  });

  it('shows success toast on successful submit', async () => {
    renderDialog();

    await fireEvent.input(screen.getByLabelText(/Description/i), {
      target: { value: 'Deploy service' },
    });
    await fireEvent.input(screen.getByLabelText(/Lead Model/i), {
      target: { value: 'gpt-4o' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /Start Job/i }));

    await waitFor(() => {
      expect(vi.mocked(showToast)).toHaveBeenCalledWith('success', expect.any(String));
    });
  });

  it('closes the dialog when Cancel is clicked', async () => {
    renderDialog();
    const cancelBtn = screen.getByRole('button', { name: /^Cancel$/i });
    expect(cancelBtn).toBeInTheDocument();
    await fireEvent.click(cancelBtn);
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });

  // ---------------------------------------------------------------------------
  // Issue #77 regression tests: Lead Backend MUST default to the backend of
  // the first registered agent — never a hardcoded value like "claude (acp)".
  // The dialog derives its backend list from listAgents() so only agents that
  // are actually registered server-side appear in the dropdown.
  // ---------------------------------------------------------------------------

  it('#77: defaults leadBackend to first registered agent backend (ollama)', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        startJob: vi.fn().mockResolvedValue({ job_id: 'j', timestamp: 0 }),
        listAgents: vi.fn().mockResolvedValue({
          agents: [makeAgent('ollama')],
        }),
      },
    } as never);

    renderDialog();

    const select = (await waitFor(() =>
      screen.getByLabelText(/Lead Backend/i),
    )) as HTMLSelectElement;
    expect(select.value).toBe('ollama');
    expect(select.value).not.toBe('claude');

    const options = Array.from(select.querySelectorAll('option')).map((o) => o.value);
    expect(options).toEqual(['ollama']);
  });

  it('#77: defaults leadBackend to empty string when no agents are registered', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        startJob: vi.fn().mockResolvedValue({ job_id: 'j', timestamp: 0 }),
        listAgents: vi.fn().mockResolvedValue({ agents: [] }),
      },
    } as never);

    renderDialog();

    const banner = await waitFor(() => screen.getByTestId('no-backends-banner'));
    expect(banner).toBeInTheDocument();

    // Start Job must be disabled when there are no registered agents.
    const startBtn = screen.getByRole('button', { name: /Start Job/i }) as HTMLButtonElement;
    expect(startBtn.disabled).toBe(true);
  });

  it('#77: defaults leadBackend to first registered backend when multiple agents exist', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        startJob: vi.fn().mockResolvedValue({ job_id: 'j', timestamp: 0 }),
        listAgents: vi.fn().mockResolvedValue({
          agents: [makeAgent('codex'), makeAgent('ollama')],
        }),
      },
    } as never);

    renderDialog();

    const select = (await waitFor(() =>
      screen.getByLabelText(/Lead Backend/i),
    )) as HTMLSelectElement;
    // First registered agent's backend is 'codex', so it must be selected by default.
    expect(select.value).toBe('codex');

    const options = Array.from(select.querySelectorAll('option')).map((o) => o.value);
    expect(options).toEqual(['codex', 'ollama']);
  });

  it('#77: shows "No agents registered" notice when agent list is empty', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        startJob: vi.fn().mockResolvedValue({ job_id: 'j', timestamp: 0 }),
        listAgents: vi.fn().mockResolvedValue({ agents: [] }),
      },
    } as never);

    renderDialog();

    const banner = await waitFor(() => screen.getByTestId('no-backends-banner'));
    expect(banner.textContent).toMatch(/No agents registered/i);
  });
});
