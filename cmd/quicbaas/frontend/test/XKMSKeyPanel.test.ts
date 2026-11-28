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
import { APIError } from '@quicbaas/sdk';
import type { XKMSKeyInfo, XKMSKeyVersion } from '@quicbaas/sdk';

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

import XKMSKeyPanel from '../src/components/security/XKMSKeyPanel.svelte';

const PEM = '-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkq\n-----END PUBLIC KEY-----';

function info(over: Partial<XKMSKeyInfo> = {}): XKMSKeyInfo {
  return {
    key_id: 'signer',
    key_type: 'rsa',
    algorithm: 'RS256',
    backend: 'pkcs8',
    key_size: 2048,
    key_version: 2,
    public_key_pem: PEM,
    ...over,
  };
}

const VERSIONS: XKMSKeyVersion[] = [
  { version: 1, status: 'disabled', created_at: '2025-05-01T00:00:00Z' },
  { version: 2, status: 'enabled', created_at: '2025-05-03T00:00:00Z' },
];

type Fn = ReturnType<typeof vi.fn>;
let keys: Record<string, Fn>;

function newKeys(): Record<string, Fn> {
  return {
    getKey: vi.fn().mockResolvedValue(info()),
    listKeyVersions: vi.fn().mockResolvedValue({ key_id: 'signer', versions: VERSIONS, total: 2 }),
    enableKeyVersion: vi.fn().mockResolvedValue({ key_id: 'signer', version: 1, status: 'enabled' }),
    disableKeyVersion: vi.fn().mockResolvedValue({ key_id: 'signer', version: 2, status: 'disabled' }),
    enableAllKeyVersions: vi.fn().mockResolvedValue({ key_id: 'signer', count: 2, message: 'all enabled' }),
    disableAllKeyVersions: vi.fn().mockResolvedValue({ key_id: 'signer', count: 2, message: 'all disabled' }),
    rotateKey: vi.fn().mockResolvedValue({ success: true, key_id: 'signer', key_version: 3 }),
    deleteKey: vi.fn().mockResolvedValue({ success: true }),
    encrypt: vi.fn(),
    decrypt: vi.fn(),
    sign: vi.fn(),
    verify: vi.fn(),
  };
}

function renderPanel(keyId = 'signer') {
  const onclose = vi.fn();
  const ondeleted = vi.fn();
  const r = render(XKMSKeyPanel, { props: { keyId, backend: 'pkcs8', onclose, ondeleted } });
  return { ...r, onclose, ondeleted };
}

async function openTab(name: string): Promise<void> {
  await fireEvent.click(screen.getByRole('tab', { name }));
}

describe('XKMSKeyPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    keys = newKeys();
    mockGetSDKClient.mockResolvedValue({ xkmsKeys: keys });
  });

  it('shows the key details and its public key PEM', async () => {
    renderPanel();
    await waitFor(() => expect(screen.getByText('RS256')).toBeInTheDocument());
    expect(keys.getKey).toHaveBeenCalledTimes(1);
    expect(keys.getKey).toHaveBeenCalledWith('signer', 'pkcs8');
    expect(screen.getByText('2048')).toBeInTheDocument();
    expect(screen.getByTestId('key-version')).toHaveTextContent('2');
    expect(screen.getByTestId('public-key-pem').textContent).toBe(PEM);
  });

  it('lists the versions and toggles one version, reloading the list', async () => {
    renderPanel();
    await openTab('Versions');
    await waitFor(() => expect(screen.getByText('v2')).toBeInTheDocument());
    expect(keys.listKeyVersions).toHaveBeenCalledWith('signer');
    await fireEvent.click(screen.getByRole('button', { name: 'Disable version 2' }));
    await waitFor(() => expect(keys.disableKeyVersion).toHaveBeenCalledWith('signer', 2));
    await waitFor(() => expect(keys.listKeyVersions).toHaveBeenCalledTimes(2));
    await fireEvent.click(screen.getByRole('button', { name: 'Enable version 1' }));
    await waitFor(() => expect(keys.enableKeyVersion).toHaveBeenCalledWith('signer', 1));
    await waitFor(() => expect(keys.listKeyVersions).toHaveBeenCalledTimes(3));
  });

  it('enables and disables all versions only after confirmation', async () => {
    renderPanel();
    await openTab('Versions');
    await waitFor(() => screen.getByText('v2'));
    await fireEvent.click(screen.getByRole('button', { name: 'Enable all versions' }));
    expect(keys.enableAllKeyVersions).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm enable all' }));
    await waitFor(() => expect(keys.enableAllKeyVersions).toHaveBeenCalledWith('signer'));
    await waitFor(() => expect(keys.listKeyVersions).toHaveBeenCalledTimes(2));
    await fireEvent.click(screen.getByRole('button', { name: 'Disable all versions' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel all versions' }));
    expect(keys.disableAllKeyVersions).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Disable all versions' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm disable all' }));
    await waitFor(() => expect(keys.disableAllKeyVersions).toHaveBeenCalledWith('signer'));
  });

  it('rotates only after confirmation, shows the new version and reloads the versions', async () => {
    renderPanel();
    await waitFor(() => screen.getByText('RS256'));
    await fireEvent.click(screen.getByRole('button', { name: 'Rotate key' }));
    expect(keys.rotateKey).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm rotate' }));
    await waitFor(() => expect(keys.rotateKey).toHaveBeenCalledWith('signer'));
    await waitFor(() => expect(screen.getByTestId('rotated-version')).toHaveTextContent('3'));
    await waitFor(() => expect(keys.listKeyVersions).toHaveBeenCalledWith('signer'));
    expect(keys.getKey).toHaveBeenCalledTimes(2);
  });

  it('deletes only after confirmation and emits ondeleted; Cancel makes no call', async () => {
    const { ondeleted } = renderPanel();
    await waitFor(() => screen.getByText('RS256'));
    await fireEvent.click(screen.getByRole('button', { name: 'Delete key' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel key action' }));
    expect(keys.deleteKey).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Delete key' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm delete' }));
    await waitFor(() => expect(keys.deleteKey).toHaveBeenCalledWith('signer'));
    await waitFor(() => expect(ondeleted).toHaveBeenCalledWith('signer'));
  });

  it('encrypts UTF-8 text as base64 and decrypts it back', async () => {
    keys.encrypt.mockResolvedValue({ ciphertext: 'Y2lwaGVy', nonce: 'bm9uY2U=', tag: 'dGFn' });
    keys.decrypt.mockResolvedValue({ plaintext: btoa('hello') });
    renderPanel();
    await openTab('Operations');
    await fireEvent.input(screen.getByLabelText('Plaintext'), { target: { value: 'hello' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Encrypt' }));
    await waitFor(() => expect(keys.encrypt).toHaveBeenCalledWith('signer', { plaintext: 'aGVsbG8=' }));
    await waitFor(() => expect(screen.getByLabelText('Ciphertext')).toHaveValue('Y2lwaGVy'));
    await fireEvent.click(screen.getByRole('button', { name: 'Decrypt' }));
    await waitFor(() => expect(keys.decrypt).toHaveBeenCalledWith('signer', {
      ciphertext: 'Y2lwaGVy', nonce: 'bm9uY2U=', tag: 'dGFn',
    }));
    await waitFor(() => expect(screen.getByTestId('decrypted-text')).toHaveTextContent('hello'));
    expect(mockShowToast).not.toHaveBeenCalled();
  });

  it('signs base64 data and verifies it, reporting invalid for changed text', async () => {
    keys.sign.mockResolvedValue({ signature: 'c2ln', algorithm: 'RS256' });
    keys.verify.mockImplementation((_id: string, req: { data: string }) =>
      Promise.resolve({ valid: req.data === btoa('message') }));
    renderPanel();
    await openTab('Operations');
    await fireEvent.input(screen.getByLabelText('Data'), { target: { value: 'message' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Sign' }));
    await waitFor(() => expect(keys.sign).toHaveBeenCalledWith('signer', { data: btoa('message') }));
    await waitFor(() => expect(screen.getByLabelText('Signature')).toHaveValue('c2ln'));
    await fireEvent.click(screen.getByRole('button', { name: 'Verify' }));
    await waitFor(() => expect(keys.verify).toHaveBeenCalledWith('signer', { data: btoa('message'), signature: 'c2ln' }));
    await waitFor(() => expect(screen.getByTestId('verify-result')).toHaveTextContent('Valid'));
    await fireEvent.input(screen.getByLabelText('Data'), { target: { value: 'changed' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Verify' }));
    await waitFor(() => expect(screen.getByTestId('verify-result')).toHaveTextContent('Invalid'));
  });

  it('refuses empty input with no call', async () => {
    renderPanel();
    await openTab('Operations');
    await fireEvent.click(screen.getByRole('button', { name: 'Encrypt' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Decrypt' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Sign' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Verify' }));
    expect(keys.encrypt).not.toHaveBeenCalled();
    expect(keys.decrypt).not.toHaveBeenCalled();
    expect(keys.sign).not.toHaveBeenCalled();
    expect(keys.verify).not.toHaveBeenCalled();
  });

  it.each([403, 404])('shows a %i inline in its tab', async (status) => {
    keys.getKey.mockRejectedValue(new APIError(status, 'key denied'));
    keys.listKeyVersions.mockRejectedValue(new APIError(status, 'versions denied'));
    keys.encrypt.mockRejectedValue(new APIError(status, 'encrypt denied'));
    renderPanel();
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('key denied'));
    await openTab('Versions');
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('versions denied'));
    await openTab('Operations');
    await fireEvent.input(screen.getByLabelText('Plaintext'), { target: { value: 'x' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Encrypt' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('encrypt denied'));
  });

  it('shows the not-available state when the connection has no xkmsKeys', async () => {
    mockGetSDKClient.mockResolvedValue({ xkmsKeys: null });
    renderPanel();
    await waitFor(() => expect(screen.getByText(/not available on this connection/i)).toBeInTheDocument());
    expect(keys.getKey).not.toHaveBeenCalled();
  });

  it('drops a response that arrives after the key changed', async () => {
    let resolveOld: (v: unknown) => void = () => {};
    keys.getKey.mockImplementation((id: string) => {
      if (id === 'old-key') return new Promise((resolve) => { resolveOld = resolve; });
      return Promise.resolve(info({ key_id: id, algorithm: 'NEW-ALG' }));
    });
    const { rerender, onclose, ondeleted } = renderPanel('old-key');
    await waitFor(() => expect(keys.getKey).toHaveBeenCalledWith('old-key', 'pkcs8'));
    await rerender({ keyId: 'new-key', backend: 'pkcs8', onclose, ondeleted });
    await waitFor(() => expect(screen.getByText('NEW-ALG')).toBeInTheDocument());
    resolveOld(info({ key_id: 'old-key', algorithm: 'OLD-ALG' }));
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByText('OLD-ALG')).not.toBeInTheDocument();
    expect(screen.getByText('NEW-ALG')).toBeInTheDocument();
  });

  it('calls onclose from the close button', async () => {
    const { onclose } = renderPanel();
    await fireEvent.click(screen.getByRole('button', { name: 'Close key panel' }));
    expect(onclose).toHaveBeenCalledTimes(1);
  });
});
