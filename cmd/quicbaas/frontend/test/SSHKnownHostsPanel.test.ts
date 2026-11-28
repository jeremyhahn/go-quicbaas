/**
 * SSHKnownHostsPanel.test.ts
 *
 * Verifies that KnownHostsPanel lists, adds and removes host-key pins and
 * that its per-row Verify action calls knownHosts.verify and shows the
 * outcome: match, mismatch, no pin, or another error.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SSHPin } from '@quicbaas/sdk';
import { SSHHostMismatchError, SSHNotFoundError } from '@quicbaas/sdk';

const pins: SSHPin[] = [
  {
    host: 'github.com',
    fingerprint: 'SHA256:githubfingerprint',
    algorithm: 'ssh-ed25519',
    scope: 'user',
    owner: 'alice',
    source: 'manual',
    added_at: '2025-01-01T00:00:00Z',
  },
];

const { mockList, mockAdd, mockRemove, mockVerify, mockGetSDKClient } = vi.hoisted(() => ({
  mockList: vi.fn(),
  mockAdd: vi.fn(),
  mockRemove: vi.fn(),
  mockVerify: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import KnownHostsPanel from '../src/views/security/ssh/KnownHostsPanel.svelte';

async function openPanel(owner = 'alice'): Promise<void> {
  render(KnownHostsPanel, { props: { scope: 'user', owner } });
  await fireEvent.click(screen.getByRole('button', { name: /Known Hosts/ }));
  await waitFor(() => screen.getByText('github.com'));
}

async function verifyWith(fingerprint: string): Promise<void> {
  await fireEvent.click(screen.getByRole('button', { name: 'Verify host key for github.com' }));
  await fireEvent.input(screen.getByLabelText(/^Presented fingerprint/), { target: { value: fingerprint } });
  await fireEvent.click(screen.getByRole('button', { name: 'Check host fingerprint' }));
}

describe('KnownHostsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockList.mockResolvedValue({ items: pins });
    mockAdd.mockResolvedValue(undefined);
    mockRemove.mockResolvedValue(undefined);
    mockVerify.mockResolvedValue(undefined);
    mockGetSDKClient.mockResolvedValue({
      ssh: {
        knownHosts: { list: mockList, add: mockAdd, remove: mockRemove, verify: mockVerify },
      },
    });
  });

  it('lists the pins for the scope and owner when opened', async () => {
    await openPanel();
    expect(mockList).toHaveBeenCalledWith({ scope: 'user', owner: 'alice' });
  });

  it('adds a pin and reloads', async () => {
    await openPanel();
    await fireEvent.click(screen.getByRole('button', { name: 'Add host pin' }));
    await fireEvent.input(screen.getByLabelText(/^Host\s*\*?$/), { target: { value: 'gitlab.com' } });
    await fireEvent.input(screen.getByLabelText(/^Host key/), { target: { value: 'ssh-ed25519 AAAAgl' } });
    const addButtons = screen.getAllByRole('button', { name: 'Add host pin' });
    await fireEvent.click(addButtons[addButtons.length - 1]);
    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith({
        host: 'gitlab.com', public_key: 'ssh-ed25519 AAAAgl', scope: 'user', owner: 'alice',
      });
    });
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('removes a pin after confirmation', async () => {
    await openPanel();
    await fireEvent.click(screen.getByRole('button', { name: 'Remove host pin for github.com' }));
    await fireEvent.click(screen.getByRole('button', { name: /^Remove$/ }));
    await waitFor(() => {
      expect(mockRemove).toHaveBeenCalledWith('github.com', { scope: 'user', owner: 'alice' });
    });
  });

  it('verifies a host key with the scope, owner and fingerprint', async () => {
    await openPanel();
    await verifyWith('SHA256:githubfingerprint');
    await waitFor(() => {
      expect(mockVerify).toHaveBeenCalledWith('github.com', {
        scope: 'user', owner: 'alice', fingerprint: 'SHA256:githubfingerprint',
      });
      expect(screen.getByText('Fingerprint matches the pin')).toBeInTheDocument();
    });
  });

  it('omits an empty owner from verify', async () => {
    await openPanel('');
    await verifyWith('SHA256:x');
    await waitFor(() => {
      expect(mockVerify).toHaveBeenCalledWith('github.com', { scope: 'user', fingerprint: 'SHA256:x' });
    });
  });

  it('shows a mismatch on SSHHostMismatchError', async () => {
    mockVerify.mockRejectedValue(new SSHHostMismatchError('github.com', 'SHA256:a', 'SHA256:b'));
    await openPanel();
    await verifyWith('SHA256:b');
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Fingerprint does NOT match the pin');
    });
  });

  it('shows "No pin for this host" on SSHNotFoundError', async () => {
    mockVerify.mockRejectedValue(new SSHNotFoundError('no pin'));
    await openPanel();
    await verifyWith('SHA256:b');
    await waitFor(() => {
      expect(screen.getByText('No pin for this host')).toBeInTheDocument();
    });
  });

  it('shows another verify error inline', async () => {
    mockVerify.mockRejectedValue(new Error('wails: verify not available'));
    await openPanel();
    await verifyWith('SHA256:b');
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('wails: verify not available');
    });
  });

  it('refuses an empty fingerprint', async () => {
    await openPanel();
    await fireEvent.click(screen.getByRole('button', { name: 'Verify host key for github.com' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Check host fingerprint' }));
    expect(screen.getByText('Fingerprint is required')).toBeInTheDocument();
    expect(mockVerify).not.toHaveBeenCalled();
  });
});
