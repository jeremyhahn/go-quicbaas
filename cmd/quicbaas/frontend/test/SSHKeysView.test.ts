/**
 * SSHKeysView.test.ts
 *
 * Verifies that SSHKeysView:
 *   - Mounts and shows loading skeleton, then renders the keys table.
 *   - Scope tab click triggers refetch with the new scope.
 *   - Empty state renders the "Generate Key" CTA when no keys are returned.
 *   - SSHUnavailableError renders the "SSH not configured" banner.
 *   - Delete confirmation dialog appears and calls sdk.ssh.keys.deleteScoped.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SSHKey } from '@quicbaas/sdk';
import { SSHUnavailableError } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockKeys: SSHKey[] = [
  {
    name: 'laptop-key',
    algorithm: 'ed25519',
    fingerprint: 'SHA256:abcdefghijklmnopqrstuvwxyz1234567890ABCDEFG',
    public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@laptop',
    scope: 'user',
    owner: 'default',
    created_at: '2025-01-01T00:00:00Z',
  },
  {
    name: 'server-key',
    algorithm: 'rsa-4096',
    fingerprint: 'SHA256:zyxwvutsrqponmlkjihgfedcba9876543210ZYXWVUTS',
    public_key: 'ssh-rsa AAAAB3NzaC1yc2E... user@server',
    scope: 'user',
    owner: 'default',
    created_at: '2025-01-02T00:00:00Z',
  },
];

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { mockListScoped, mockDeleteScoped, mockGetSDKClient } = vi.hoisted(() => ({
  mockListScoped: vi.fn(),
  mockDeleteScoped: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
  setJobs: vi.fn(),
  setFleetNodes: vi.fn(),
  setSessions: vi.fn(),
  setTasks: vi.fn(),
  setHealth: vi.fn(),
  setJobsLoading: vi.fn(),
  setFleetNodesLoading: vi.fn(),
  setSessionsLoading: vi.fn(),
  setTasksLoading: vi.fn(),
  setActiveTenant: vi.fn(),
}));

import SSHKeysView from '../src/views/security/SSHKeysView.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SSHKeysView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListScoped.mockResolvedValue({ items: mockKeys });
    mockDeleteScoped.mockResolvedValue(undefined);
    mockGetSDKClient.mockResolvedValue({
      ssh: {
        keys: {
          listScoped: mockListScoped,
          deleteScoped: mockDeleteScoped,
          create: vi.fn(),
          import_: vi.fn(),
          exportPublic: vi.fn(),
        },
        knownHosts: {
          list: vi.fn().mockResolvedValue({ items: [] }),
          add: vi.fn(),
          remove: vi.fn(),
          verify: vi.fn(),
        },
        authorized: {
          emit: vi.fn(),
          verify: vi.fn(),
        },
      },
    });
  });

  it('renders the SSH Keys page heading', async () => {
    render(SSHKeysView);
    await waitFor(() => {
      expect(screen.getByText('SSH Keys')).toBeInTheDocument();
    });
  });

  it('renders scope tab labels', async () => {
    render(SSHKeysView);
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /My Keys/i })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: /Tenant Keys/i })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: /Service Keys/i })).toBeInTheDocument();
    });
  });

  it('renders key rows after loading', async () => {
    render(SSHKeysView);
    await waitFor(() => {
      expect(screen.getByText('laptop-key')).toBeInTheDocument();
      expect(screen.getByText('server-key')).toBeInTheDocument();
    });
  });

  it('calls listScoped with user scope by default', async () => {
    render(SSHKeysView);
    await waitFor(() => {
      expect(mockListScoped).toHaveBeenCalledWith(
        expect.objectContaining({ scope: 'user' }),
      );
    });
  });

  it('refetches with tenant scope when Tenant Keys tab is clicked', async () => {
    render(SSHKeysView);
    await waitFor(() => screen.getByText('laptop-key'));

    mockListScoped.mockResolvedValueOnce({ items: [] });

    await fireEvent.click(screen.getByRole('tab', { name: /Tenant Keys/i }));

    await waitFor(() => {
      expect(mockListScoped).toHaveBeenCalledWith(
        expect.objectContaining({ scope: 'tenant' }),
      );
    });
  });

  it('shows empty state CTA when no keys are returned', async () => {
    mockListScoped.mockResolvedValue({ items: [] });
    render(SSHKeysView);
    await waitFor(() => {
      expect(screen.getByText('No SSH keys found')).toBeInTheDocument();
    });
  });

  it('shows "SSH not configured" banner on SSHUnavailableError', async () => {
    mockListScoped.mockRejectedValue(new SSHUnavailableError('xkms not configured'));
    render(SSHKeysView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        /SSH is not configured/i,
      );
    });
  });

  it('shows generic error message on unexpected errors', async () => {
    mockListScoped.mockRejectedValue(new Error('Network timeout'));
    render(SSHKeysView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Network timeout');
    });
  });

  it('opens delete confirmation when Delete is clicked', async () => {
    render(SSHKeysView);
    await waitFor(() => screen.getByText('laptop-key'));

    const deleteButtons = screen.getAllByRole('button', { name: /Delete key/i });
    await fireEvent.click(deleteButtons[0]);

    expect(screen.getByRole('dialog', { name: /Delete key confirmation/i })).toBeInTheDocument();
  });

  it('calls deleteScoped with correct args on confirmation', async () => {
    render(SSHKeysView);
    await waitFor(() => screen.getByText('laptop-key'));

    const deleteButtons = screen.getAllByRole('button', { name: /Delete key/i });
    await fireEvent.click(deleteButtons[0]);

    const confirmButton = screen.getByRole('button', { name: /^Delete$/ });
    await fireEvent.click(confirmButton);

    await waitFor(() => {
      expect(mockDeleteScoped).toHaveBeenCalledWith('user', 'default', 'laptop-key');
    });
  });

  it('shows Generate and Import Key buttons', async () => {
    render(SSHKeysView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Generate SSH key/i })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /Import SSH key/i })).toBeInTheDocument();
    });
  });
});
