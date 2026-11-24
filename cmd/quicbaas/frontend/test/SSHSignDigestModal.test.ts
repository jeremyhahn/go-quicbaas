/**
 * SSHSignDigestModal.test.ts
 *
 * Verifies that SignDigestModal sends a hex digest to keys.sign as a number[]
 * of its bytes, refuses malformed input with no call, and renders the
 * signature format and blob (a string as is, an array as base64).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SSHKey } from '@quicbaas/sdk';

const key: SSHKey = {
  name: 'laptop-key',
  algorithm: 'ed25519',
  fingerprint: 'SHA256:abc',
  scope: 'user',
  owner: 'alice',
  created_at: '2025-01-01T00:00:00Z',
};

const { mockSign, mockGetSDKClient } = vi.hoisted(() => ({
  mockSign: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import SignDigestModal from '../src/views/security/ssh/SignDigestModal.svelte';

async function signWith(digest: string): Promise<void> {
  render(SignDigestModal, { props: { key, scope: 'user', owner: 'alice', onClose: vi.fn() } });
  await fireEvent.input(screen.getByLabelText(/^Digest \(hex\)/), { target: { value: digest } });
  await fireEvent.click(screen.getByRole('button', { name: 'Sign digest' }));
}

describe('SignDigestModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockSign.mockResolvedValue({ format: 'ssh-ed25519', blob: 'c2lnbmF0dXJl' });
    mockGetSDKClient.mockResolvedValue({ ssh: { keys: { sign: mockSign } } });
  });

  it('sends a valid hex digest as a number[] of its bytes', async () => {
    await signWith('00ff10Ab');
    await waitFor(() => {
      expect(mockSign).toHaveBeenCalledWith('user', 'alice', 'laptop-key', { digest: [0, 255, 16, 171] });
    });
  });

  it.each([
    ['odd length', 'abc'],
    ['non-hex', 'zz'],
    ['empty', ''],
    ['over 128 chars', 'a'.repeat(130)],
  ])('refuses %s input with no call', async (_label, digest) => {
    await signWith(digest);
    expect(screen.getByRole('alert')).toHaveTextContent('Digest must be 2-128 hex characters of even length');
    expect(mockSign).not.toHaveBeenCalled();
  });

  it('accepts a 128-char digest', async () => {
    await signWith('ab'.repeat(64));
    await waitFor(() => expect(mockSign).toHaveBeenCalled());
  });

  it('shows a string blob as is and the format', async () => {
    await signWith('abcd');
    await waitFor(() => {
      expect(screen.getByLabelText('Signature (base64)')).toHaveTextContent('c2lnbmF0dXJl');
      expect(screen.getByLabelText('Signature format')).toHaveTextContent('ssh-ed25519');
    });
  });

  it('shows an array blob as base64', async () => {
    mockSign.mockResolvedValue({ format: 'rsa-sha2-256', blob: [104, 105] });
    await signWith('abcd');
    await waitFor(() => {
      expect(screen.getByLabelText('Signature (base64)')).toHaveTextContent('aGk=');
      expect(screen.getByLabelText('Signature format')).toHaveTextContent('rsa-sha2-256');
    });
  });

  it('shows a sign error inline', async () => {
    mockSign.mockRejectedValue(new Error('key is not a signer'));
    await signWith('abcd');
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('key is not a signer');
    });
  });
});
