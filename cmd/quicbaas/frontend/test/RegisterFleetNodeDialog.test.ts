/**
 * RegisterFleetNodeDialog.test.ts
 *
 * Verifies that RegisterFleetNodeDialog mounts, validates required fields,
 * calls registerFleetNode with the correct shape, and shows feedback correctly.
 *
 * The Tenant ID field is a RefPicker (kind="tenant") now, and a RefPicker
 * reads its own provider on mount to resolve a friendly label for whatever
 * value it opens with — so getSDKClient is no longer a reliable proxy for
 * "did the form submit". Assertions below target registerFleetNode itself,
 * the call that actually registers something.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';

const { mockRegisterFleetNode, mockGetSDKClient } = vi.hoisted(() => ({
  mockRegisterFleetNode: vi.fn().mockResolvedValue({ agent: { id: 'agent-xyz', name: 'agent-01' } }),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: mockGetSDKClient }));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import RegisterFleetNodeDialog from '../src/components/orchestrator/RegisterFleetNodeDialog.svelte';
import { showToast } from '../src/stores/toast.svelte.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('RegisterFleetNodeDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockRegisterFleetNode.mockResolvedValue({ agent: { id: 'agent-xyz', name: 'agent-01' } });
    // The Tenant ID RefPicker asks client.iam.listTenants() on mount; an iam
    // client that resolves an empty list keeps that best-effort call quiet
    // without needing every test to know about it.
    mockGetSDKClient.mockResolvedValue({
      orchestrator: { registerFleetNode: mockRegisterFleetNode },
      iam: { listTenants: vi.fn().mockResolvedValue({ tenants: [], count: 0 }) },
    });
    vi.mocked(showToast).mockReset();
  });

  it('renders all registration fields when open', () => {
    render(RegisterFleetNodeDialog, { props: { open: true } });
    expect(screen.getByLabelText(/Name/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Host/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Backend/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Node type/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Metadata/i)).toBeInTheDocument();
  });

  it('shows validation error when name is empty on submit', async () => {
    render(RegisterFleetNodeDialog, { props: { open: true } });
    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));
    expect(screen.getByText(/Name is required/i)).toBeInTheDocument();
    expect(mockRegisterFleetNode).not.toHaveBeenCalled();
  });

  it('shows validation error when host is empty on submit', async () => {
    render(RegisterFleetNodeDialog, { props: { open: true } });
    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'agent-01' },
    });
    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));
    expect(screen.getByText(/Host is required/i)).toBeInTheDocument();
    expect(mockRegisterFleetNode).not.toHaveBeenCalled();
  });

  it('calls registerFleetNode on valid submit', async () => {
    render(RegisterFleetNodeDialog, { props: { open: true } });

    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'worker-01' },
    });
    await fireEvent.input(screen.getByLabelText(/Host/i), {
      target: { value: '10.0.0.5:9090' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));

    await waitFor(() => {
      expect(mockRegisterFleetNode).toHaveBeenCalled();
    });
  });

  it('shows success toast after registering an agent', async () => {
    render(RegisterFleetNodeDialog, { props: { open: true } });

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
    render(RegisterFleetNodeDialog, { props: { open: true } });

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
    expect(mockRegisterFleetNode).not.toHaveBeenCalled();
  });

  it('shows error message when registerFleetNode rejects', async () => {
    mockRegisterFleetNode.mockRejectedValue(new Error('conflict'));

    render(RegisterFleetNodeDialog, { props: { open: true } });

    await fireEvent.input(screen.getByLabelText(/Name/i), {
      target: { value: 'dup-agent' },
    });
    await fireEvent.input(screen.getByLabelText(/Host/i), {
      target: { value: 'localhost:1234' },
    });

    await fireEvent.click(screen.getByRole('button', { name: /^Register$/i }));

    await waitFor(() => {
      expect(screen.getByText(/Failed to register node/i)).toBeInTheDocument();
    });
  });
});
