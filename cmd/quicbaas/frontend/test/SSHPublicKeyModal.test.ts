/**
 * SSHPublicKeyModal.test.ts
 *
 * Verifies that PublicKeyModal loads the key details (keys.getScoped) and the
 * exported public key (keys.exportPublic) when it opens, copies the exported
 * line, and keeps the list answer's public_key visible when the export fails.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SSHKey } from '@quicbaas/sdk';

const key: SSHKey = {
  name: 'laptop-key',
  algorithm: 'ed25519',
  fingerprint: 'SHA256:listfingerprint',
  public_key: 'ssh-ed25519 AAAAlist user@laptop',
  scope: 'user',
  owner: 'alice',
  created_at: '2025-01-01T00:00:00Z',
};

const { mockGetScoped, mockExportPublic, mockGetSDKClient, mockWriteText } = vi.hoisted(() => ({
  mockGetScoped: vi.fn(),
  mockExportPublic: vi.fn(),
  mockGetSDKClient: vi.fn(),
  mockWriteText: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import PublicKeyModal from '../src/views/security/ssh/PublicKeyModal.svelte';

describe('PublicKeyModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: mockWriteText },
      configurable: true,
    });
    mockWriteText.mockResolvedValue(undefined);
    mockGetScoped.mockResolvedValue({
      ...key,
      fingerprint: 'SHA256:detailfingerprint',
      comment: 'work laptop',
    });
    mockExportPublic.mockResolvedValue({
      algorithm: 'ssh-ed25519',
      line: 'ssh-ed25519 AAAAexported user@laptop',
      fingerprint: 'SHA256:exportedfingerprint',
    });
    mockGetSDKClient.mockResolvedValue({
      ssh: { keys: { getScoped: mockGetScoped, exportPublic: mockExportPublic } },
    });
  });

  it('loads the details and the export with the key scope, owner and name', async () => {
    render(PublicKeyModal, { props: { key, scope: 'user', owner: 'alice', onClose: vi.fn() } });
    await waitFor(() => {
      expect(mockGetScoped).toHaveBeenCalledWith('user', 'alice', 'laptop-key');
      expect(mockExportPublic).toHaveBeenCalledWith('user', 'alice', 'laptop-key');
    });
  });

  it('shows the detail fields, the exported line and fingerprint', async () => {
    render(PublicKeyModal, { props: { key, scope: 'user', owner: 'alice', onClose: vi.fn() } });
    await waitFor(() => {
      expect(screen.getByLabelText('Public key text')).toHaveTextContent('ssh-ed25519 AAAAexported user@laptop');
    });
    const details = screen.getByLabelText('Key details');
    expect(details).toHaveTextContent('laptop-key');
    expect(details).toHaveTextContent('user');
    expect(details).toHaveTextContent('alice');
    expect(details).toHaveTextContent('ed25519');
    expect(details).toHaveTextContent('SHA256:detailfingerprint');
    expect(details).toHaveTextContent('work laptop');
    expect(details).toHaveTextContent('SHA256:exportedfingerprint');
  });

  it('copies the exported line', async () => {
    render(PublicKeyModal, { props: { key, scope: 'user', owner: 'alice', onClose: vi.fn() } });
    await waitFor(() => {
      expect(screen.getByLabelText('Public key text')).toHaveTextContent('AAAAexported');
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Copy public key to clipboard' }));
    expect(mockWriteText).toHaveBeenCalledWith('ssh-ed25519 AAAAexported user@laptop');
  });

  it('keeps the list public key visible when the export fails', async () => {
    mockExportPublic.mockRejectedValue(new Error('export refused'));
    render(PublicKeyModal, { props: { key, scope: 'user', owner: 'alice', onClose: vi.fn() } });
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('export refused');
    });
    expect(screen.getByLabelText('Public key text')).toHaveTextContent('ssh-ed25519 AAAAlist user@laptop');
  });
});
