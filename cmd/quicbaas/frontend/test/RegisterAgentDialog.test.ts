/**
 * RegisterAgentDialog.test.ts
 *
 * Verifies that RegisterAgentDialog mounts, validates required fields,
 * calls registerAgent with the correct shape, and shows feedback correctly.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mocks — inline vi.fn() to avoid hoisting issues.
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      registerAgent: vi.fn().mockResolvedValue({
        agent: { id: 'agent-xyz', name: 'agent-01' },
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

import RegisterAgentDialog from '../src/components/orchestrator/RegisterAgentDialog.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { showToast } from '../src/stores/toast.svelte.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('RegisterAgentDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        registerAgent: vi.fn().mockResolvedValue({
          agent: { id: 'agent-xyz', name: 'agent-01' },
        }),
      },
    } as never);
    vi.mocked(showToast).mockReset();
  });

  it('renders all registration fields when open', () => {
    render(RegisterAgentDialog, { props: { open: true } });
    expect(screen.getByLabelText(/Name/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Host/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Backend/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Agent Type/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Metadata/i)).toBeInTheDocument();
  });

  it('shows validation error when name is empty on submit', async () => {
    render(RegisterAgentDialog, { props: { open: true } });
    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));
    expect(screen.getByText(/Name is required/i)).toBeInTheDocument();
    expect(vi.mocked(getSDKClient)).not.toHaveBeenCalled();
  });

  it('shows validation error when host is empty on submit', async () => {
    render(RegisterAgentDialog, { props: { open: true } });
    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'agent-01' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));
    expect(screen.getByText(/Host is required/i)).toBeInTheDocument();
    expect(vi.mocked(getSDKClient)).not.toHaveBeenCalled();
  });

  it('calls getSDKClient on valid submit', async () => {
    render(RegisterAgentDialog, { props: { open: true } });

    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'worker-01' },
    });
    await fireEvent.input(screen.getByLabelText(/Host/i), {
      target: { value: '10.0.0.5:9090' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));

    await waitFor(() => {
      expect(vi.mocked(getSDKClient)).toHaveBeenCalled();
    });
  });

  it('shows success toast after registering an agent', async () => {
    render(RegisterAgentDialog, { props: { open: true } });

    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'my-agent' },
    });
    await fireEvent.input(screen.getByLabelText(/Host/i), {
      target: { value: 'localhost:9090' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));

    await waitFor(() => {
      expect(vi.mocked(showToast)).toHaveBeenCalledWith(
        'success',
        expect.stringContaining('my-agent'),
      );
    });
  });

  it('shows error when metadata JSON is invalid', async () => {
    render(RegisterAgentDialog, { props: { open: true } });

    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'bad-meta' },
    });
    await fireEvent.input(screen.getByLabelText(/Host/i), {
      target: { value: '127.0.0.1:9000' },
    });
    await fireEvent.input(screen.getByLabelText(/Metadata/i), {
      target: { value: 'not valid json' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));

    expect(screen.getByText(/Metadata must be valid JSON/i)).toBeInTheDocument();
    expect(vi.mocked(getSDKClient)).not.toHaveBeenCalled();
  });

  it('shows error message when registerAgent rejects', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      orchestrator: {
        registerAgent: vi.fn().mockRejectedValueOnce(new Error('conflict')),
      },
    } as never);

    render(RegisterAgentDialog, { props: { open: true } });

    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'dup-agent' },
    });
    await fireEvent.input(screen.getByLabelText(/Host/i), {
      target: { value: 'localhost:1234' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to register agent/i)).toBeInTheDocument();
    });
  });
});
