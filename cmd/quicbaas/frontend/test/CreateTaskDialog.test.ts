/**
 * CreateTaskDialog.test.ts
 *
 * Verifies that CreateTaskDialog mounts, validates inputs, calls createTask
 * with the correct shape, and invokes the onCreated callback on success.
 * Also covers the host affinity dropdown and GPU required toggle introduced
 * alongside the new CreateTaskRequest fields.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Hoisted mocks — must be declared before any src/ imports.
// ---------------------------------------------------------------------------

const { mockCreateTask, mockListAgents, mockShowToast } = vi.hoisted(() => ({
  mockCreateTask: vi.fn(),
  mockListAgents: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      createTask: mockCreateTask,
      listFleetNodes: mockListAgents,
    },
  }),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

import CreateTaskDialog from '../src/components/orchestrator/CreateTaskDialog.svelte';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const AGENTS = [
  { id: 'agent-1', name: 'gpu-host-01', host: '10.0.0.1', capabilities: ['gpu'] },
  { id: 'agent-2', name: 'cpu-host-02', host: '10.0.0.2', capabilities: [] },
];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('CreateTaskDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateTask.mockResolvedValue({ task: { id: 'task-001' } });
    mockListAgents.mockResolvedValue({ nodes: AGENTS });
  });

  it('renders subject, team name, and description fields when open', () => {
    render(CreateTaskDialog, { props: { open: true } });
    expect(screen.getByLabelText(/Subject/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Team Name/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Description/i)).toBeInTheDocument();
  });

  it('renders the host affinity select and GPU required toggle when open', async () => {
    render(CreateTaskDialog, { props: { open: true } });

    // Agents load asynchronously via listFleetNodes — wait for select to populate.
    await waitFor(() => {
      const hostSelect = screen.getByLabelText(/Host affinity — pin task to a specific fleet node/i);
      expect(hostSelect).toBeInTheDocument();
    });

    const gpuToggle = screen.getByLabelText(/Require a GPU-capable fleet node/i);
    expect(gpuToggle).toBeInTheDocument();
    expect((gpuToggle as HTMLInputElement).type).toBe('checkbox');
  });

  it('populates the host affinity dropdown with agents from listFleetNodes', async () => {
    render(CreateTaskDialog, { props: { open: true } });

    await waitFor(() => {
      expect(mockListAgents).toHaveBeenCalledOnce();
    });

    const hostSelect = screen.getByLabelText(/Host affinity — pin task to a specific fleet node/i) as HTMLSelectElement;
    const options = Array.from(hostSelect.options).map((o) => o.value);
    expect(options).toContain('gpu-host-01');
    expect(options).toContain('cpu-host-02');
    // "Any" option is always first.
    expect(options[0]).toBe('');
  });

  it('shows validation error when subject is empty on submit', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));
    expect(screen.getByText(/Subject is required/i)).toBeInTheDocument();
    // createTask must NOT be called — only listFleetNodes (on mount) is allowed.
    expect(mockCreateTask).not.toHaveBeenCalled();
  });

  it('calls createTask on valid submit', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'Implement auth module' },
    });
    await fireEvent.input(screen.getByLabelText(/Team Name/i), {
      target: { value: 'backend' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));

    await waitFor(() => {
      expect(mockCreateTask).toHaveBeenCalledOnce();
    });
  });

  it('passes host_affinity to createTask when agent is selected', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'Run model inference' },
    });

    const hostSelect = screen.getByLabelText(/Host affinity — pin task to a specific fleet node/i);
    await fireEvent.change(hostSelect, { target: { value: 'gpu-host-01' } });

    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));

    await waitFor(() => {
      expect(mockCreateTask).toHaveBeenCalledWith(
        expect.objectContaining({ host_affinity: 'gpu-host-01' }),
      );
    });
  });

  it('passes gpu_required: true to createTask when GPU toggle is checked', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'GPU workload' },
    });

    const gpuToggle = screen.getByLabelText(/Require a GPU-capable fleet node/i);
    await fireEvent.click(gpuToggle);

    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));

    await waitFor(() => {
      expect(mockCreateTask).toHaveBeenCalledWith(
        expect.objectContaining({ gpu_required: true }),
      );
    });
  });

  it('shows informational text when GPU toggle is enabled', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    const gpuToggle = screen.getByLabelText(/Require a GPU-capable fleet node/i);
    await fireEvent.click(gpuToggle);

    expect(
      screen.getByText(/Task will only be assigned to fleet nodes that advertise GPU capability/i),
    ).toBeInTheDocument();
  });

  it('calls onCreated callback after successful create', async () => {
    const onCreated = vi.fn();
    render(CreateTaskDialog, { props: { open: true, onCreated } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'Fix bug' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));

    await waitFor(() => {
      expect(onCreated).toHaveBeenCalledTimes(1);
    });
  });

  it('shows success toast after creating a task', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'Write tests' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));

    await waitFor(() => {
      expect(mockShowToast).toHaveBeenCalledWith(
        'success',
        expect.stringContaining('Write tests'),
      );
    });
  });

  it('shows error message when createTask rejects', async () => {
    mockCreateTask.mockRejectedValueOnce(new Error('quota exceeded'));

    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'Broken task' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to create task/i)).toBeInTheDocument();
    });
  });

  it('falls back gracefully when listFleetNodes rejects', async () => {
    mockListAgents.mockRejectedValueOnce(new Error('network error'));

    render(CreateTaskDialog, { props: { open: true } });

    // Component should still render with an empty agent list (no crash).
    await waitFor(() => {
      const hostSelect = screen.getByLabelText(/Host affinity — pin task to a specific fleet node/i) as HTMLSelectElement;
      // Only the "Any" option present when agents fail to load.
      expect(hostSelect.options.length).toBe(1);
      expect(hostSelect.options[0].value).toBe('');
    });
  });

  // -------------------------------------------------------------------------
  // Auto-spawn affordances
  // -------------------------------------------------------------------------

  it('renders the "Run immediately" auto-spawn toggle', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));
    expect(screen.getByText(/Run immediately/i)).toBeInTheDocument();
  });

  it('reveals the Backend selector once Run immediately is enabled', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    // Backend selector hidden until auto-spawn is on
    expect(screen.queryByLabelText(/^Backend/i)).toBeNull();

    const toggle = screen.getByLabelText(
      /Spawn a job immediately on the best-matching fleet node/i,
    );
    await fireEvent.click(toggle);

    await waitFor(() => {
      expect(
        screen.getByLabelText(
          /Backend to route the auto-spawned job to/i,
        ),
      ).toBeInTheDocument();
    });
  });

  it('submit button text changes to "Create & Run Now" when auto-spawn is on', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    expect(screen.getByRole('button', { name: /^Create Task$/i })).toBeInTheDocument();

    const toggle = screen.getByLabelText(
      /Spawn a job immediately on the best-matching fleet node/i,
    );
    await fireEvent.click(toggle);

    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: /Create & Run Now/i }),
      ).toBeInTheDocument();
    });
  });

  it('passes auto_spawn=true when Run immediately is enabled', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'spawn now' },
    });

    const toggle = screen.getByLabelText(
      /Spawn a job immediately on the best-matching fleet node/i,
    );
    await fireEvent.click(toggle);

    await fireEvent.click(screen.getByRole('button', { name: /Create & Run Now/i }));

    await waitFor(() => {
      expect(mockCreateTask).toHaveBeenCalledWith(
        expect.objectContaining({ auto_spawn: true }),
      );
    });
  });

  it('omits auto_spawn from the payload when Run immediately is off (default)', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'queued task' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Create Task$/i }));

    await waitFor(() => {
      expect(mockCreateTask).toHaveBeenCalledOnce();
    });
    const arg = mockCreateTask.mock.calls[0][0];
    expect(arg.auto_spawn).toBeUndefined();
  });

  it('forwards backend value with auto_spawn when both are set', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await waitFor(() => screen.getByLabelText(/Host affinity/i));

    await fireEvent.input(screen.getByLabelText(/Subject/i), {
      target: { value: 'route to ollama' },
    });

    const toggle = screen.getByLabelText(
      /Spawn a job immediately on the best-matching fleet node/i,
    );
    await fireEvent.click(toggle);

    const backendInput = await waitFor(() =>
      screen.getByLabelText(/Backend to route the auto-spawned job to/i),
    );
    await fireEvent.input(backendInput, { target: { value: 'ollama' } });

    await fireEvent.click(screen.getByRole('button', { name: /Create & Run Now/i }));

    await waitFor(() => {
      expect(mockCreateTask).toHaveBeenCalledWith(
        expect.objectContaining({ auto_spawn: true, backend: 'ollama' }),
      );
    });
  });
});

describe('CreateTaskDialog — run-time prompt material', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateTask.mockResolvedValue({ task: { id: 'task-001' } });
    mockListAgents.mockResolvedValue({ nodes: AGENTS });
  });

  it('sends the appended prompt every attempt carries', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await fireEvent.input(screen.getByLabelText(/Subject/i), { target: { value: 'Fix it' } });
    await fireEvent.input(screen.getByLabelText(/Append to the agent's prompt/i), { target: { value: 'use the staging cluster' } });
    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));
    await waitFor(() => expect(mockCreateTask).toHaveBeenCalled());
    expect(mockCreateTask.mock.calls[0][0].prompt_append).toBe('use the staging cluster');
  });

  it('sends none when the field is left blank', async () => {
    render(CreateTaskDialog, { props: { open: true } });
    await fireEvent.input(screen.getByLabelText(/Subject/i), { target: { value: 'Fix it' } });
    await fireEvent.click(screen.getByRole('button', { name: /Create Task/i }));
    await waitFor(() => expect(mockCreateTask).toHaveBeenCalled());
    expect(mockCreateTask.mock.calls[0][0].prompt_append).toBeUndefined();
  });
});
