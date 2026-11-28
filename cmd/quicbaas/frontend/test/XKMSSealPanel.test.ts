// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import { APIError } from '@quicbaas/sdk';
import type { XKMSSealResponse } from '@quicbaas/sdk';
import { setUnauthenticated } from '../src/stores/auth.svelte.ts';

const { mockGetSDKClient, mockShowToast } = vi.hoisted(() => ({
  mockGetSDKClient: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

import XKMSSealPanel from '../src/components/security/XKMSSealPanel.svelte';

// base64 of the UTF-8 text "héllo".
const DATA_TEXT = 'héllo';
const DATA_B64 = 'aMOpbGxv';
const AAD_B64 = btoa('ctx');

const ENVELOPE: XKMSSealResponse = {
  backend: 'pkcs8',
  ciphertext: 'Y2lwaGVy',
  nonce: 'bm9uY2U=',
  tag: 'dGFn',
  key_id: 'vault-key',
  key_version: 3,
  metadata: { purpose: 'test' },
};

type Fn = ReturnType<typeof vi.fn>;
let xkms: Record<string, Fn>;

function newXKMS(): Record<string, Fn> {
  return {
    canSeal: vi.fn().mockResolvedValue({ can_seal: true, backend: 'pkcs8', message: 'sealing ready' }),
    seal: vi.fn().mockResolvedValue(ENVELOPE),
    unseal: vi.fn().mockResolvedValue({ plaintext: DATA_B64 }),
  };
}

function toastText(): string {
  return JSON.stringify(mockShowToast.mock.calls);
}

function input(label: string, value: string): Promise<boolean> {
  return fireEvent.input(screen.getByLabelText(label), { target: { value } });
}

async function rendered(backend = 'pkcs8'): Promise<void> {
  render(XKMSSealPanel, { props: { backend } });
  await waitFor(() => expect(mockGetSDKClient).toHaveBeenCalled());
}

async function sealFilled(): Promise<void> {
  await rendered();
  await input('Seal key ID', 'vault-key');
  await input('Data', DATA_TEXT);
}

describe('XKMSSealPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    xkms = newXKMS();
    mockGetSDKClient.mockResolvedValue({ xkms });
  });

  it('calls nothing on mount and checks the capability for the form backend', async () => {
    await rendered('pkcs8');
    expect(xkms.canSeal).not.toHaveBeenCalled();
    expect(xkms.seal).not.toHaveBeenCalled();
    expect(xkms.unseal).not.toHaveBeenCalled();
    expect((screen.getByLabelText('Seal backend') as HTMLInputElement).value).toBe('pkcs8');
    expect((screen.getByLabelText('Unseal backend') as HTMLInputElement).value).toBe('pkcs8');
    await fireEvent.click(screen.getByRole('button', { name: 'Check seal capability' }));
    await waitFor(() => expect(xkms.canSeal).toHaveBeenCalledWith('pkcs8'));
    const cap = await screen.findByTestId('seal-capability');
    expect(cap).toHaveTextContent('Yes');
    expect(cap).toHaveTextContent('sealing ready');
  });

  it('seals the UTF-8 data as base64 with the backend and key id, and shows the envelope', async () => {
    await sealFilled();
    await fireEvent.click(screen.getByRole('button', { name: 'Seal' }));
    await waitFor(() => expect(xkms.seal).toHaveBeenCalledWith({
      backend: 'pkcs8', key_id: 'vault-key', data: DATA_B64,
    }));
    const env = await screen.findByTestId('sealed-envelope');
    expect(JSON.parse((env as HTMLTextAreaElement).value)).toEqual(ENVELOPE);
  });

  it('sends aad as base64 and the password only when they are set', async () => {
    await sealFilled();
    await input('Seal AAD', 'ctx');
    await input('Seal password', 's3cret');
    await fireEvent.click(screen.getByRole('button', { name: 'Seal' }));
    await waitFor(() => expect(xkms.seal).toHaveBeenCalledWith({
      backend: 'pkcs8', key_id: 'vault-key', data: DATA_B64, aad: AAD_B64, password: 's3cret',
    }));
    expect((screen.getByLabelText('Seal password') as HTMLInputElement).type).toBe('password');
    await screen.findByTestId('sealed-envelope');
    expect(toastText()).not.toContain('s3cret');
  });

  it('refuses an empty backend, key id or data without a call', async () => {
    await rendered('');
    await fireEvent.click(screen.getByRole('button', { name: 'Seal' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Backend is required');
    await input('Seal backend', 'pkcs8');
    await fireEvent.click(screen.getByRole('button', { name: 'Seal' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Key ID is required');
    await input('Seal key ID', 'vault-key');
    await fireEvent.click(screen.getByRole('button', { name: 'Seal' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Data is required');
    expect(xkms.seal).not.toHaveBeenCalled();
  });

  it('uses the envelope for unseal and shows the UTF-8 plaintext', async () => {
    await sealFilled();
    await fireEvent.click(screen.getByRole('button', { name: 'Seal' }));
    await screen.findByTestId('sealed-envelope');
    await fireEvent.click(screen.getByRole('button', { name: 'Use for unseal' }));
    expect((screen.getByLabelText('Unseal key ID') as HTMLInputElement).value).toBe('vault-key');
    await input('Unseal AAD', 'ctx');
    await fireEvent.click(screen.getByRole('button', { name: 'Unseal' }));
    await waitFor(() => expect(xkms.unseal).toHaveBeenCalledWith({
      backend: 'pkcs8', key_id: 'vault-key',
      ciphertext: 'Y2lwaGVy', nonce: 'bm9uY2U=', tag: 'dGFn',
      key_version: 3, metadata: { purpose: 'test' }, aad: AAD_B64,
    }));
    expect(await screen.findByTestId('unsealed-plaintext')).toHaveTextContent(DATA_TEXT);
    expect(toastText()).not.toContain(DATA_TEXT);
  });

  it('shows plaintext that is not UTF-8 as base64', async () => {
    xkms.unseal.mockResolvedValue({ plaintext: '/w==' });
    await rendered();
    await input('Unseal key ID', 'vault-key');
    await input('Sealed envelope (JSON)', JSON.stringify({ ciphertext: 'Y2lwaGVy' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Unseal' }));
    const out = await screen.findByTestId('unsealed-plaintext');
    expect(out).toHaveTextContent('/w==');
    expect(screen.getByText(/base64, not UTF-8/)).toBeInTheDocument();
  });

  it('refuses envelope text that is not JSON or has no ciphertext without a call', async () => {
    await rendered();
    await input('Unseal key ID', 'vault-key');
    await input('Sealed envelope (JSON)', 'not json');
    await fireEvent.click(screen.getByRole('button', { name: 'Unseal' }));
    expect(screen.getByRole('alert')).toHaveTextContent('JSON');
    await input('Sealed envelope (JSON)', '{"nonce":"abc"}');
    await fireEvent.click(screen.getByRole('button', { name: 'Unseal' }));
    expect(screen.getByRole('alert')).toHaveTextContent('ciphertext');
    expect(xkms.unseal).not.toHaveBeenCalled();
  });

  it('clears the password and plaintext when the session changes', async () => {
    await rendered();
    await input('Unseal key ID', 'vault-key');
    await input('Unseal password', 'pw-1');
    await input('Sealed envelope (JSON)', JSON.stringify({ ciphertext: 'Y2lwaGVy' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Unseal' }));
    await screen.findByTestId('unsealed-plaintext');
    setUnauthenticated();
    flushSync();
    await waitFor(() => expect(screen.queryByTestId('unsealed-plaintext')).not.toBeInTheDocument());
    expect((screen.getByLabelText('Unseal password') as HTMLInputElement).value).toBe('');
  });

  it('shows a 403 from canSeal, seal and unseal inline', async () => {
    xkms.canSeal.mockRejectedValue(new APIError(403, 'capability denied'));
    xkms.seal.mockRejectedValue(new APIError(403, 'seal denied'));
    xkms.unseal.mockRejectedValue(new APIError(403, 'unseal denied'));
    await sealFilled();
    await fireEvent.click(screen.getByRole('button', { name: 'Check seal capability' }));
    expect(await screen.findByText('capability denied')).toBeInTheDocument();
    await fireEvent.click(screen.getByRole('button', { name: 'Seal' }));
    expect(await screen.findByText('seal denied')).toBeInTheDocument();
    await input('Unseal key ID', 'vault-key');
    await input('Sealed envelope (JSON)', JSON.stringify({ ciphertext: 'Y2lwaGVy' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Unseal' }));
    expect(await screen.findByText('unseal denied')).toBeInTheDocument();
  });

  it('shows the not-available state and makes no call without xkms', async () => {
    mockGetSDKClient.mockResolvedValue({ xkms: null });
    render(XKMSSealPanel, { props: { backend: 'pkcs8' } });
    expect(await screen.findByTestId('xkms-seal-not-available')).toBeInTheDocument();
    expect(xkms.canSeal).not.toHaveBeenCalled();
    expect(xkms.seal).not.toHaveBeenCalled();
    expect(xkms.unseal).not.toHaveBeenCalled();
  });
});
