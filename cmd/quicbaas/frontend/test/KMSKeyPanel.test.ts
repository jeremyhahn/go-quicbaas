/**
 * KMSKeyPanel.test.ts
 *
 * Verifies that KMSKeyPanel, opened for one named KMS key:
 *   - Shows the key's metadata and edits its description and tags.
 *   - Shows and saves the key's access policy.
 *   - Shows, sets and (after confirmation) removes the rotation policy.
 *   - Encrypts UTF-8 text with the key and decrypts it back.
 *   - Shows a 403/404 inline, never throws.
 *   - Shows a not-available state when the connection has no kmsAdmin.
 *   - Drops a response that arrives after the key changed.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';
import type {
  KMSKeyMetadata,
  KMSKeyPolicy,
  KMSRotationPolicy,
} from '@quicbaas/sdk';

const { mockGetSDKClient } = vi.hoisted(() => ({
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import KMSKeyPanel from '../src/components/security/KMSKeyPanel.svelte';

// 2025-05-03T00:00:00Z in Unix seconds.
const T2025 = 1746230400;

function meta(over: Partial<KMSKeyMetadata> = {}): KMSKeyMetadata {
  return {
    key_id: 'app-key',
    name: 'app-key',
    description: 'Primary key',
    algorithm: 'AES-256-GCM',
    status: 'ENABLED',
    version: 3,
    exportable: false,
    deletion_protection: true,
    created_at: T2025,
    updated_at: T2025,
    tags: { env: 'prod', team: 'core' },
    owner: 'alice',
    ...over,
  };
}

function policy(over: Partial<KMSKeyPolicy> = {}): KMSKeyPolicy {
  return {
    key_id: 'app-key',
    version: 7,
    allowed_operations: ['encrypt', 'decrypt'],
    allowed_principals: ['svc-a'],
    denied_principals: ['svc-x'],
    expires_at: null,
    rotation_required: true,
    max_usage_count: null,
    require_mfa: false,
    updated_at: T2025,
    ...over,
  };
}

const rotation: KMSRotationPolicy = {
  enabled: true,
  interval_seconds: 86400,
  retain_versions: 5,
  auto_rotate: true,
  next_rotation_unix: T2025,
};

interface Admin {
  getKeyMetadata: ReturnType<typeof vi.fn>;
  updateKeyMetadata: ReturnType<typeof vi.fn>;
  getKeyPolicy: ReturnType<typeof vi.fn>;
  setKeyPolicy: ReturnType<typeof vi.fn>;
  getKeyRotationPolicy: ReturnType<typeof vi.fn>;
  setKeyRotationPolicy: ReturnType<typeof vi.fn>;
  deleteKeyRotationPolicy: ReturnType<typeof vi.fn>;
}

let admin: Admin;
let kms: { encrypt: ReturnType<typeof vi.fn>; decrypt: ReturnType<typeof vi.fn> };

function newAdmin(): Admin {
  return {
    getKeyMetadata: vi.fn().mockResolvedValue({ metadata: meta(), timestamp: T2025 }),
    updateKeyMetadata: vi.fn(),
    getKeyPolicy: vi.fn().mockResolvedValue({ policy: policy(), timestamp: T2025 }),
    setKeyPolicy: vi.fn().mockResolvedValue({ key_id: 'app-key', policy_version: 8, timestamp: T2025 }),
    getKeyRotationPolicy: vi.fn().mockResolvedValue({ key_id: 'app-key', policy: rotation, timestamp: T2025 }),
    setKeyRotationPolicy: vi.fn(),
    deleteKeyRotationPolicy: vi.fn().mockResolvedValue({ key_id: 'app-key', timestamp: T2025 }),
  };
}

function renderPanel(keyName = 'app-key') {
  const onclose = vi.fn();
  const r = render(KMSKeyPanel, { props: { keyName, onclose } });
  return { ...r, onclose };
}

async function openTab(name: string): Promise<void> {
  await fireEvent.click(screen.getByRole('tab', { name }));
}

describe('KMSKeyPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    admin = newAdmin();
    kms = { encrypt: vi.fn(), decrypt: vi.fn() };
    mockGetSDKClient.mockResolvedValue({ kms, kmsAdmin: admin });
  });

  // 1
  it('loads the metadata once and shows its fields', async () => {
    renderPanel();
    await waitFor(() => expect(screen.getByText('AES-256-GCM')).toBeInTheDocument());
    expect(admin.getKeyMetadata).toHaveBeenCalledTimes(1);
    expect(admin.getKeyMetadata).toHaveBeenCalledWith('app-key');
    expect(screen.getByText('ENABLED')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('alice')).toBeInTheDocument();
    expect(screen.getByText('env=prod')).toBeInTheDocument();
    expect(screen.getByText('team=core')).toBeInTheDocument();
    expect(screen.getAllByText(/2025/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/1970/)).not.toBeInTheDocument();
  });

  // 2
  it('saves only the changed metadata fields and shows the result', async () => {
    admin.updateKeyMetadata.mockResolvedValue({
      metadata: meta({ description: 'Rotated key', tags: { env: 'prod', owner: 'ops' } }),
      timestamp: T2025,
    });
    renderPanel();
    await waitFor(() => screen.getByText('AES-256-GCM'));
    await fireEvent.click(screen.getByRole('button', { name: 'Edit metadata' }));
    await fireEvent.input(screen.getByLabelText('Description'), { target: { value: 'Rotated key' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Remove tag team' }));
    await fireEvent.input(screen.getByLabelText('Tag key'), { target: { value: 'owner' } });
    await fireEvent.input(screen.getByLabelText('Tag value'), { target: { value: 'ops' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add tag' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save metadata' }));
    await waitFor(() => expect(admin.updateKeyMetadata).toHaveBeenCalledTimes(1));
    expect(admin.updateKeyMetadata).toHaveBeenCalledWith('app-key', {
      description: 'Rotated key',
      tags: { owner: 'ops' },
      remove_tags: ['team'],
    });
    await waitFor(() => expect(screen.getByText('Rotated key')).toBeInTheDocument());
    expect(screen.getByText('owner=ops')).toBeInTheDocument();
  });

  it('sends only the description when no tag changed', async () => {
    admin.updateKeyMetadata.mockResolvedValue({ metadata: meta({ description: 'New' }), timestamp: T2025 });
    renderPanel();
    await waitFor(() => screen.getByText('AES-256-GCM'));
    await fireEvent.click(screen.getByRole('button', { name: 'Edit metadata' }));
    await fireEvent.input(screen.getByLabelText('Description'), { target: { value: 'New' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save metadata' }));
    await waitFor(() => expect(admin.updateKeyMetadata).toHaveBeenCalledWith('app-key', { description: 'New' }));
  });

  // 3
  it('shows the access policy with Never and Unlimited for null fields', async () => {
    renderPanel();
    await openTab('Policy');
    await waitFor(() => expect(admin.getKeyPolicy).toHaveBeenCalledWith('app-key'));
    await waitFor(() => expect(screen.getByText('encrypt, decrypt')).toBeInTheDocument());
    expect(screen.getByText('svc-a')).toBeInTheDocument();
    expect(screen.getByText('svc-x')).toBeInTheDocument();
    expect(screen.getByText('Never')).toBeInTheDocument();
    expect(screen.getByText('Unlimited')).toBeInTheDocument();
    expect(screen.getByTestId('policy-mfa')).toHaveTextContent('No');
    expect(screen.getByTestId('policy-rotation-required')).toHaveTextContent('Yes');
  });

  it('saves the access policy with the loaded version and trimmed principals', async () => {
    renderPanel();
    await openTab('Policy');
    await waitFor(() => screen.getByText('encrypt, decrypt'));
    await fireEvent.click(screen.getByRole('button', { name: 'Edit policy' }));
    await fireEvent.input(screen.getByLabelText('Allowed principals'), { target: { value: ' svc-a , svc-b,, ' } });
    await fireEvent.input(screen.getByLabelText('Max usage'), { target: { value: '100' } });
    await fireEvent.click(screen.getByLabelText('Require MFA'));
    await fireEvent.click(screen.getByRole('button', { name: 'Save policy' }));
    await waitFor(() => expect(admin.setKeyPolicy).toHaveBeenCalledTimes(1));
    expect(admin.setKeyPolicy).toHaveBeenCalledWith('app-key', {
      version: 7,
      allowed_operations: ['encrypt', 'decrypt'],
      allowed_principals: ['svc-a', 'svc-b'],
      denied_principals: ['svc-x'],
      expires_at: null,
      rotation_required: true,
      max_usage_count: 100,
      require_mfa: true,
    });
  });

  it('rejects a non-numeric max usage without a call', async () => {
    renderPanel();
    await openTab('Policy');
    await waitFor(() => screen.getByText('encrypt, decrypt'));
    await fireEvent.click(screen.getByRole('button', { name: 'Edit policy' }));
    await fireEvent.input(screen.getByLabelText('Max usage'), { target: { value: 'lots' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save policy' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/Max usage/));
    expect(admin.setKeyPolicy).not.toHaveBeenCalled();
  });

  // 4
  it('shows "No rotation policy" when the key has none', async () => {
    admin.getKeyRotationPolicy.mockResolvedValue({ key_id: 'app-key', timestamp: T2025 });
    renderPanel();
    await openTab('Rotation');
    await waitFor(() => expect(screen.getByText('No rotation policy')).toBeInTheDocument());
    expect(admin.getKeyRotationPolicy).toHaveBeenCalledWith('app-key');
  });

  it('sets a rotation policy', async () => {
    admin.getKeyRotationPolicy.mockResolvedValue({ key_id: 'app-key', timestamp: T2025 });
    admin.setKeyRotationPolicy.mockResolvedValue({
      key_id: 'app-key',
      policy: { enabled: true, interval_seconds: 3600, retain_versions: 2, auto_rotate: false },
      timestamp: T2025,
    });
    renderPanel();
    await openTab('Rotation');
    await waitFor(() => screen.getByText('No rotation policy'));
    await fireEvent.input(screen.getByLabelText('Interval (seconds)'), { target: { value: '3600' } });
    await fireEvent.input(screen.getByLabelText('Retain versions'), { target: { value: '2' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Set rotation policy' }));
    await waitFor(() => expect(admin.setKeyRotationPolicy).toHaveBeenCalledTimes(1));
    expect(admin.setKeyRotationPolicy).toHaveBeenCalledWith('app-key', {
      enabled: true,
      interval_seconds: 3600,
      retain_versions: 2,
      auto_rotate: false,
    });
    await waitFor(() => expect(screen.getByTestId('rotation-interval')).toHaveTextContent('3600'));
  });

  it('rejects an interval that is not a positive integer without a call', async () => {
    renderPanel();
    await openTab('Rotation');
    await waitFor(() => screen.getByTestId('rotation-interval'));
    await fireEvent.input(screen.getByLabelText('Interval (seconds)'), { target: { value: '-5' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Set rotation policy' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/Interval/));
    await fireEvent.input(screen.getByLabelText('Interval (seconds)'), { target: { value: '1.5' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Set rotation policy' }));
    expect(admin.setKeyRotationPolicy).not.toHaveBeenCalled();
  });

  it('removes the rotation policy only after confirmation', async () => {
    renderPanel();
    await openTab('Rotation');
    await waitFor(() => expect(screen.getByTestId('rotation-interval')).toHaveTextContent('86400'));
    await fireEvent.click(screen.getByRole('button', { name: 'Remove rotation policy' }));
    expect(admin.deleteKeyRotationPolicy).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm remove' }));
    await waitFor(() => expect(admin.deleteKeyRotationPolicy).toHaveBeenCalledWith('app-key'));
    await waitFor(() => expect(screen.getByText('No rotation policy')).toBeInTheDocument());
  });

  // 5
  it('encrypts UTF-8 text as base64 and decrypts it back', async () => {
    kms.encrypt.mockResolvedValue({ ciphertext: 'Y2lwaGVy', key_id: 'app-key', algorithm: 'AES-256-GCM' });
    kms.decrypt.mockResolvedValue({ plaintext: btoa('hello'), key_id: 'app-key' });
    renderPanel();
    await openTab('Encrypt');
    await fireEvent.input(screen.getByLabelText('Plaintext'), { target: { value: 'hello' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Encrypt' }));
    await waitFor(() => expect(kms.encrypt).toHaveBeenCalledWith({ key_id: 'app-key', plaintext: 'aGVsbG8=' }));
    await waitFor(() => expect(screen.getByLabelText('Ciphertext')).toHaveValue('Y2lwaGVy'));
    await fireEvent.click(screen.getByRole('button', { name: 'Decrypt' }));
    await waitFor(() => expect(kms.decrypt).toHaveBeenCalledWith({ key_id: 'app-key', ciphertext: 'Y2lwaGVy' }));
    await waitFor(() => expect(screen.getByTestId('decrypted-text')).toHaveTextContent('hello'));
  });

  it('makes no encrypt or decrypt call for empty input', async () => {
    renderPanel();
    await openTab('Encrypt');
    await fireEvent.click(screen.getByRole('button', { name: 'Encrypt' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Decrypt' }));
    expect(kms.encrypt).not.toHaveBeenCalled();
    expect(kms.decrypt).not.toHaveBeenCalled();
  });

  // 6
  it.each([403, 404])('shows a %i inline in the tab', async (status) => {
    admin.getKeyMetadata.mockRejectedValue(new APIError(status, 'metadata denied'));
    admin.getKeyPolicy.mockRejectedValue(new APIError(status, 'policy denied'));
    kms.encrypt.mockRejectedValue(new APIError(status, 'encrypt denied'));
    renderPanel();
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('metadata denied'));
    await openTab('Policy');
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('policy denied'));
    await openTab('Encrypt');
    await fireEvent.input(screen.getByLabelText('Plaintext'), { target: { value: 'x' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Encrypt' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('encrypt denied'));
  });

  // 7
  it('shows a not-available state when the connection has no kmsAdmin', async () => {
    kms.encrypt.mockResolvedValue({ ciphertext: 'Y2lwaGVy', key_id: 'app-key', algorithm: 'AES-256-GCM' });
    mockGetSDKClient.mockResolvedValue({ kms, kmsAdmin: null });
    renderPanel();
    await waitFor(() => expect(screen.getByText(/not available on this connection/i)).toBeInTheDocument());
    await openTab('Policy');
    await waitFor(() => expect(screen.getByText(/not available on this connection/i)).toBeInTheDocument());
    await openTab('Rotation');
    await waitFor(() => expect(screen.getByText(/not available on this connection/i)).toBeInTheDocument());
    expect(admin.getKeyMetadata).not.toHaveBeenCalled();
    expect(admin.getKeyPolicy).not.toHaveBeenCalled();
    expect(admin.getKeyRotationPolicy).not.toHaveBeenCalled();
    await openTab('Encrypt');
    await fireEvent.input(screen.getByLabelText('Plaintext'), { target: { value: 'hello' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Encrypt' }));
    await waitFor(() => expect(screen.getByLabelText('Ciphertext')).toHaveValue('Y2lwaGVy'));
  });

  // 8
  it('drops a response that arrives after the key changed', async () => {
    let resolveOld: (v: unknown) => void = () => {};
    admin.getKeyMetadata.mockImplementation((name: string) => {
      if (name === 'old-key') {
        return new Promise((resolve) => { resolveOld = resolve; });
      }
      return Promise.resolve({ metadata: meta({ key_id: name, name, algorithm: 'NEW-ALG' }), timestamp: T2025 });
    });
    const { rerender, onclose } = renderPanel('old-key');
    await waitFor(() => expect(admin.getKeyMetadata).toHaveBeenCalledWith('old-key'));
    await rerender({ keyName: 'new-key', onclose });
    await waitFor(() => expect(screen.getByText('NEW-ALG')).toBeInTheDocument());
    resolveOld({ metadata: meta({ key_id: 'old-key', name: 'old-key', algorithm: 'OLD-ALG' }), timestamp: T2025 });
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
