/**
 * GenerateKeyModal.test.ts
 *
 * Verifies that GenerateKeyModal:
 *   - Renders the form with name, algorithm, scope, owner fields.
 *   - Calls sdk.ssh.keys.create with the correct arguments on submit.
 *   - Shows an inline name error when SSHDuplicateError is returned.
 *   - Calls onClose when Cancel is clicked.
 *   - Shows validation error when name is missing.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { SSHDuplicateError, SSHInvalidError } from '@quicbaas/sdk';
import type { SSHKey } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { mockCreate, mockGetSDKClient } = vi.hoisted(() => ({
  mockCreate: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import GenerateKeyModal from '../src/views/security/ssh/GenerateKeyModal.svelte';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const createdKey: SSHKey = {
  name: 'test-key',
  algorithm: 'ed25519',
  fingerprint: 'SHA256:test',
  public_key: 'ssh-ed25519 AAAA...',
  scope: 'user',
  owner: 'default',
  created_at: '2025-01-01T00:00:00Z',
};

function renderModal(
  overrides: { onClose?: () => void; onCreated?: (key: SSHKey) => void } = {},
) {
  const onClose = overrides.onClose ?? vi.fn();
  const onCreated = overrides.onCreated ?? vi.fn();
  render(GenerateKeyModal, {
    props: { scope: 'user', owner: 'default', onClose, onCreated },
  });
  return { onClose, onCreated };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('GenerateKeyModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreate.mockResolvedValue(createdKey);
    mockGetSDKClient.mockResolvedValue({
      ssh: {
        keys: {
          create: mockCreate,
        },
      },
    });
  });

  it('renders the dialog with correct heading', () => {
    renderModal();
    expect(screen.getByRole('dialog', { name: /Generate SSH key dialog/i })).toBeInTheDocument();
    expect(screen.getByText('Generate SSH Key')).toBeInTheDocument();
  });

  it('renders name, algorithm, scope, and owner fields', () => {
    renderModal();
    expect(screen.getByLabelText(/Name/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Algorithm/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Scope/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/Owner/i)).toBeInTheDocument();
  });

  it('scope and owner fields are read-only', () => {
    renderModal();
    const scopeInput = screen.getByLabelText(/Scope/i) as HTMLInputElement;
    const ownerInput = screen.getByLabelText(/Owner/i) as HTMLInputElement;
    expect(scopeInput.readOnly).toBe(true);
    expect(ownerInput.readOnly).toBe(true);
  });

  it('calls ssh.keys.create with correct args on submit', async () => {
    const { onCreated } = renderModal();

    await fireEvent.input(screen.getByLabelText(/Name/i), { target: { value: 'my-key' } });
    await fireEvent.click(screen.getByRole('button', { name: /Generate key/i }));

    await waitFor(() => {
      expect(mockCreate).toHaveBeenCalledWith(
        expect.objectContaining({
          name: 'my-key',
          algorithm: 'ed25519',
          scope: 'user',
        }),
      );
      expect(onCreated).toHaveBeenCalledWith(createdKey);
    });
  });

  it('shows validation error when name is empty', async () => {
    renderModal();
    await fireEvent.click(screen.getByRole('button', { name: /Generate key/i }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/Name is required/i);
    });
  });

  it('shows name error for invalid slug', async () => {
    renderModal();
    await fireEvent.input(screen.getByLabelText(/Name/i), { target: { value: 'My Key!' } });
    await fireEvent.click(screen.getByRole('button', { name: /Generate key/i }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/lowercase/i);
    });
  });

  it('shows inline name error on SSHDuplicateError', async () => {
    mockCreate.mockRejectedValue(new SSHDuplicateError('key exists'));
    renderModal();

    await fireEvent.input(screen.getByLabelText(/Name/i), { target: { value: 'my-key' } });
    await fireEvent.click(screen.getByRole('button', { name: /Generate key/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/already exists/i);
    });
    expect(mockCreate).toHaveBeenCalled();
  });

  it('shows inline name error on SSHInvalidError', async () => {
    mockCreate.mockRejectedValue(new SSHInvalidError('invalid name format'));
    renderModal();

    await fireEvent.input(screen.getByLabelText(/Name/i), { target: { value: 'my-key' } });
    await fireEvent.click(screen.getByRole('button', { name: /Generate key/i }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/invalid name format/i);
    });
  });

  it('calls onClose when Cancel is clicked', async () => {
    const { onClose } = renderModal();
    await fireEvent.click(screen.getByRole('button', { name: /Cancel/i }));
    expect(onClose).toHaveBeenCalled();
  });
});
