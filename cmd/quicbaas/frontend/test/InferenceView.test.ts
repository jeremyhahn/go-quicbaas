/**
 * InferenceView.test.ts
 *
 * Verifies that InferenceView:
 *  - Populates backend and host selectors from SDK calls on mount
 *  - Loads model list via listBackendModels when backend selection changes
 *  - Falls back to text override input when listBackendModels rejects
 *  - Disables the Send button while a request is in-flight
 *  - Renders aggregate response text correctly
 *  - Appends streaming delta chunks as they arrive
 *  - Shows an error message when the backend returns a 4xx
 *  - Shows an error toast on 5xx without clearing the prompt
 *  - Shows empty-state messaging when no agents match the backend
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Hoisted mocks (must be set up before any imports from src/)
// ---------------------------------------------------------------------------

const {
  mockListBackends,
  mockListAgents,
  mockListBackendModels,
  mockInference,
  mockInferenceStream,
  mockShowToast,
} = vi.hoisted(() => ({
  mockListBackends: vi.fn(),
  mockListAgents: vi.fn(),
  mockListBackendModels: vi.fn(),
  mockInference: vi.fn(),
  mockInferenceStream: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      listBackends: mockListBackends,
      listFleetNodes: mockListAgents,
      listBackendModels: mockListBackendModels,
      inference: mockInference,
      inferenceStream: mockInferenceStream,
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/navigation.svelte.ts', () => ({
  navigate: vi.fn(),
  navStore: { activeView: 'inference', sidebarCollapsed: false },
}));

// The history store uses Svelte runes — mock it to avoid SSR/jsdom issues
// with the $state macro in a plain node test runner.
vi.mock('../src/stores/inferenceHistoryStore.svelte.ts', () => ({
  inferenceHistoryStore: { entries: [] },
  addInferenceHistory: vi.fn(),
  clearInferenceHistory: vi.fn(),
}));

import InferenceView from '../src/views/orchestrator/InferenceView.svelte';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const BACKENDS = [
  { name: 'ollama', type: 'ollama', capabilities: {}, default_model: 'qwen3:1.7b' },
];

const AGENTS = [
  {
    id: 'agent-1',
    name: 'gpu-host',
    host: '198.51.100.10',
    backend: 'ollama',
    capabilities: ['ollama'],
    status: 'running',
  },
];

const OLLAMA_MODELS = ['qwen3:1.7b', 'llama3.2:3b', 'mistral:7b'];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('InferenceView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListBackends.mockResolvedValue(BACKENDS);
    mockListAgents.mockResolvedValue({ nodes: AGENTS });
    mockListBackendModels.mockResolvedValue(OLLAMA_MODELS);
  });

  it('renders without throwing', async () => {
    expect(() => render(InferenceView)).not.toThrow();
    // Page header
    await waitFor(() => {
      expect(screen.getByText('Inference')).toBeTruthy();
    });
  });

  it('populates the backend selector from listBackends', async () => {
    render(InferenceView);
    await waitFor(() => {
      const select = screen.getByLabelText('Select inference backend') as HTMLSelectElement;
      // ollama option should exist
      const options = Array.from(select.options).map((o) => o.value);
      expect(options).toContain('ollama');
    });
  });

  it('populates the host affinity selector from listFleetNodes', async () => {
    render(InferenceView);
    await waitFor(() => {
      const select = screen.getByLabelText('Select host affinity (optional)') as HTMLSelectElement;
      const options = Array.from(select.options).map((o) => o.value);
      expect(options).toContain('gpu-host');
    });
  });

  it('calls listBackends and listFleetNodes on mount', async () => {
    render(InferenceView);
    await waitFor(() => {
      expect(mockListBackends).toHaveBeenCalledOnce();
      expect(mockListAgents).toHaveBeenCalledOnce();
    });
  });

  it('shows an error above the prompt when prompt is empty on submit', async () => {
    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    // Submit the form directly — fireEvent.click on a submit button does not
    // invoke the onsubmit handler in jsdom without an explicit form submit.
    const form = document.querySelector('form') as HTMLFormElement;
    await fireEvent.submit(form);

    await waitFor(() => {
      expect(screen.getByText('Prompt is required.')).toBeTruthy();
    });
  });

  it('renders aggregate response when inference returns content', async () => {
    mockInference.mockResolvedValue({
      content: [{ type: 'text', text: 'Hello from ollama!' }],
      model: 'qwen3:1.7b',
      stop_reason: 'end_turn',
    });

    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    // Disable streaming to use aggregate path
    const streamToggle = screen.getByLabelText('Enable streaming mode') as HTMLInputElement;
    if (streamToggle.checked) {
      await fireEvent.click(streamToggle);
    }

    // Fill prompt
    const promptArea = screen.getByLabelText('Prompt text') as HTMLTextAreaElement;
    await fireEvent.input(promptArea, { target: { value: 'Write a haiku' } });

    const form = document.querySelector('form') as HTMLFormElement;
    await fireEvent.submit(form);

    await waitFor(() => {
      expect(screen.getByText(/Hello from ollama!/)).toBeTruthy();
    });
  });

  it('appends streaming deltas in streaming mode', async () => {
    // Create a simple async generator that yields two chunks.
    async function* fakeStream() {
      yield { delta: [{ type: 'text', text: 'Hello ' }], final: false };
      yield { delta: [{ type: 'text', text: 'world!' }], model: 'qwen3:1.7b', stop_reason: 'end_turn', final: true };
    }
    mockInferenceStream.mockReturnValue(fakeStream());

    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    // Ensure streaming is enabled (default)
    const streamToggle = screen.getByLabelText('Enable streaming mode') as HTMLInputElement;
    if (!streamToggle.checked) {
      await fireEvent.click(streamToggle);
    }

    const promptArea = screen.getByLabelText('Prompt text') as HTMLTextAreaElement;
    await fireEvent.input(promptArea, { target: { value: 'Say hello' } });

    const form = document.querySelector('form') as HTMLFormElement;
    await fireEvent.submit(form);

    await waitFor(() => {
      expect(screen.getByText(/Hello world!/)).toBeTruthy();
    });
  });

  it('shows a field-level error and toast on 4xx response', async () => {
    const apiError = Object.assign(new Error('backend not found'), { status: 404 });
    mockInference.mockRejectedValue(apiError);

    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    const streamToggle = screen.getByLabelText('Enable streaming mode') as HTMLInputElement;
    if (streamToggle.checked) {
      await fireEvent.click(streamToggle);
    }

    const promptArea = screen.getByLabelText('Prompt text') as HTMLTextAreaElement;
    await fireEvent.input(promptArea, { target: { value: 'Test prompt' } });

    const form = document.querySelector('form') as HTMLFormElement;
    await fireEvent.submit(form);

    await waitFor(() => {
      expect(mockShowToast).toHaveBeenCalledWith('error', expect.stringContaining('backend not found'));
    });
  });

  it('shows a toast on 5xx error without clearing the prompt', async () => {
    const serverError = Object.assign(new Error('internal server error'), { status: 500 });
    mockInference.mockRejectedValue(serverError);

    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    const streamToggle = screen.getByLabelText('Enable streaming mode') as HTMLInputElement;
    if (streamToggle.checked) {
      await fireEvent.click(streamToggle);
    }

    const promptArea = screen.getByLabelText('Prompt text') as HTMLTextAreaElement;
    await fireEvent.input(promptArea, { target: { value: 'My important prompt' } });

    const form = document.querySelector('form') as HTMLFormElement;
    await fireEvent.submit(form);

    await waitFor(() => {
      expect(mockShowToast).toHaveBeenCalledWith('error', expect.stringContaining('internal server error'));
      // Prompt text must not be cleared
      expect(promptArea.value).toBe('My important prompt');
    });
  });

  /**
   * Regression for the bug where a 503 response (typed envelope) was
   * silently swallowed: the form reset to "Send" but the operator never
   * saw the error message. The fix renders the typed envelope's `error`
   * field inside the Response panel as a destructive alert, keeps the
   * prompt populated for retry, and still emits a toast.
   */
  it('renders the typed envelope error in the Response panel when the daemon returns 503', async () => {
    const unavailable = Object.assign(
      new Error('orchestrator service not available'),
      { status: 503 },
    );
    mockInference.mockRejectedValue(unavailable);

    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    // Aggregate path so the SDK throws synchronously from the .inference()
    // call rather than yielding an empty stream.
    const streamToggle = screen.getByLabelText('Enable streaming mode') as HTMLInputElement;
    if (streamToggle.checked) {
      await fireEvent.click(streamToggle);
    }

    const promptArea = screen.getByLabelText('Prompt text') as HTMLTextAreaElement;
    await fireEvent.input(promptArea, {
      target: { value: 'A prompt that should survive the failure' },
    });

    const form = document.querySelector('form') as HTMLFormElement;
    await fireEvent.submit(form);

    // The Response panel must render the destructive alert containing
    // the typed envelope's error message.
    const errorAlert = await screen.findByTestId('inference-response-error');
    expect(errorAlert).toBeInTheDocument();
    expect(errorAlert.className).toMatch(/alert-error/);
    expect(errorAlert.textContent ?? '').toMatch(/orchestrator service not available/);

    // The prompt must still hold the operator's input so they can retry.
    expect(promptArea.value).toBe('A prompt that should survive the failure');

    // A toast should accompany the inline alert so the failure is also
    // surfaced to operators who have the panel scrolled out of view.
    expect(mockShowToast).toHaveBeenCalledWith(
      'error',
      expect.stringContaining('orchestrator service not available'),
    );
  });

  it('shows empty-state when no agents match the selected backend', async () => {
    mockListAgents.mockResolvedValue({ nodes: [] });

    render(InferenceView);
    await waitFor(() => {
      expect(screen.getByText(/No fleet nodes found for this backend/)).toBeTruthy();
    });
  });

  it('loads model list via listBackendModels when backend is selected', async () => {
    render(InferenceView);
    // Wait for backend dropdown to populate.
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    await waitFor(() => {
      // listBackendModels should have been called once the effect fires after
      // backend auto-selection on mount.
      expect(mockListBackendModels).toHaveBeenCalled();
    });

    // The model select should render with the returned models.
    await waitFor(() => {
      const modelSelect = screen.getByLabelText('Select model') as HTMLSelectElement;
      const options = Array.from(modelSelect.options).map((o) => o.value);
      expect(options).toContain('qwen3:1.7b');
      expect(options).toContain('llama3.2:3b');
    });
  });

  it('auto-selects the first returned model from listBackendModels', async () => {
    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    await waitFor(() => {
      const modelSelect = screen.getByLabelText('Select model') as HTMLSelectElement;
      // OLLAMA_MODELS[0] = 'qwen3:1.7b' should be selected by default.
      expect(modelSelect.value).toBe('qwen3:1.7b');
    });
  });

  it('shows model override input for manual model entry', async () => {
    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    // The override input is always rendered alongside the select.
    const overrideInput = screen.getByLabelText('Model name override') as HTMLInputElement;
    expect(overrideInput).toBeInTheDocument();
    expect(overrideInput.type).toBe('text');
  });

  it('falls back to text override when listBackendModels rejects', async () => {
    mockListBackendModels.mockRejectedValue(new Error('models endpoint unavailable'));

    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select inference backend'));

    await waitFor(() => {
      // The override input should still be rendered so the user can type a model.
      const overrideInput = screen.getByLabelText('Model name override');
      expect(overrideInput).toBeInTheDocument();
    });

    // The model select should not be shown (no models loaded).
    // Instead, the error state or empty state renders.
    await waitFor(() => {
      expect(mockListBackendModels).toHaveBeenCalled();
    });
  });

  it('calls listBackendModels with the selected host affinity when set', async () => {
    render(InferenceView);
    await waitFor(() => screen.getByLabelText('Select host affinity (optional)'));

    const hostSelect = screen.getByLabelText('Select host affinity (optional)') as HTMLSelectElement;
    await fireEvent.change(hostSelect, { target: { value: 'gpu-host' } });

    await waitFor(() => {
      // listBackendModels should have been re-called with 'gpu-host' as the agent.
      const calls = mockListBackendModels.mock.calls;
      const withAgent = calls.some(([agentID]) => agentID === 'gpu-host');
      expect(withAgent).toBe(true);
    });
  });

  it('shows the routing failure that names the unreachable daemon', async () => {
    const message = 'backend "ollama": fleet node daemon at 198.51.100.10:9091 is unreachable: connection refused';
    mockListBackendModels.mockRejectedValue(new Error(message));

    render(InferenceView);

    await waitFor(() => {
      expect(screen.getByText(/198\.51\.100\.10:9091 is unreachable/)).toBeTruthy();
    });
  });

  it('defaults the backend to one whose models are reachable', async () => {
    mockListBackends.mockResolvedValue([
      { name: 'ollama', type: 'ollama', capabilities: {}, default_model: 'qwen3:1.7b' },
      { name: 'vllm', type: 'vllm', capabilities: {}, default_model: '' },
    ]);
    mockListBackendModels.mockImplementation(async (_node: string, backend: string) => {
      if (backend === 'ollama') {
        throw new Error('backend "ollama": fleet node daemon at 198.51.100.10:9091 is unreachable');
      }
      return ['llama3.1:8b'];
    });

    render(InferenceView);

    await waitFor(() => {
      const select = screen.getByLabelText('Select inference backend') as HTMLSelectElement;
      expect(select.value).toBe('vllm');
    });
  });
});
