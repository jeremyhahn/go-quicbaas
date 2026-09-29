/**
 * DynamicSecretsView.test.ts
 *
 * Verifies that DynamicSecretsView mounts, calls listRoles/listLeases on
 * init, renders the role and lease lists, exposes create-role /
 * delete-role / generate-credential / renew-lease / revoke-lease
 * affordances that call the corresponding DynamicSecretsClient methods
 * with the right arguments, confirm-gates destructive actions, and
 * degrades gracefully when a fetch fails. Also verifies the generated
 * credential is discarded (never re-rendered) once its dialog is closed.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import type { DynamicSecretRole, LeaseInfo } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import DynamicSecretsView from '../src/views/security/DynamicSecretsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const mockCreateRole = vi.fn();
const mockGetRole = vi.fn();
const mockListRoles = vi.fn();
const mockDeleteRole = vi.fn();
const mockGenerateCredential = vi.fn();
const mockRenewLease = vi.fn();
const mockRevokeLease = vi.fn();
const mockListLeases = vi.fn();

function mockClient(): void {
  vi.mocked(getSDKClient).mockResolvedValue({
    dynamicSecrets: {
      createRole: mockCreateRole,
      getRole: mockGetRole,
      listRoles: mockListRoles,
      deleteRole: mockDeleteRole,
      generateCredential: mockGenerateCredential,
      renewLease: mockRenewLease,
      revokeLease: mockRevokeLease,
      listLeases: mockListLeases,
    },
  } as never);
}

function role(overrides: Partial<DynamicSecretRole> = {}): DynamicSecretRole {
  return {
    name: 'db-readonly',
    generator_type: 'postgres',
    default_ttl: '1h0m0s',
    max_ttl: '24h0m0s',
    timestamp: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function lease(overrides: Partial<LeaseInfo> = {}): LeaseInfo {
  return {
    lease_id: 'lease-abc',
    path: 'db-readonly',
    ttl: '1h0m0s',
    expiry: '2026-01-01T01:00:00Z',
    renewable: true,
    renew_count: 0,
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockListRoles.mockResolvedValue({ roles: [], count: 0, timestamp: '' });
  mockListLeases.mockResolvedValue({ leases: [], count: 0, timestamp: '' });
  mockCreateRole.mockResolvedValue(role());
  mockGetRole.mockResolvedValue(role());
  mockDeleteRole.mockResolvedValue({ success: true, deleted_at: '', timestamp: '' });
  mockGenerateCredential.mockResolvedValue({
    credential_id: 'cred-1',
    username: 'app_readonly_x1',
    password: 'super-secret-once',
    lease_id: 'lease-new',
    lease_ttl: '1h0m0s',
    lease_expiry: '2026-01-01T01:00:00Z',
    renewable: true,
    timestamp: '',
  });
  mockRenewLease.mockResolvedValue({
    lease_id: 'lease-abc',
    lease_ttl: '1h0m0s',
    lease_expiry: '',
    renew_count: 1,
    renewed_at: '',
    timestamp: '',
  });
  mockRevokeLease.mockResolvedValue({ success: true, revoked_at: '', timestamp: '' });
  mockClient();
});

describe('DynamicSecretsView', () => {
  it('calls listRoles and listLeases on mount', async () => {
    render(DynamicSecretsView);
    await waitFor(() => {
      expect(mockListRoles).toHaveBeenCalled();
      expect(mockListLeases).toHaveBeenCalled();
    });
  });

  it('renders the page heading with icon', async () => {
    const { container } = render(DynamicSecretsView);
    await waitFor(() => {
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('Dynamic Secrets');
      expect(container.querySelector('header.page-header svg')).not.toBeNull();
    });
  });

  it('renders empty states when zero roles and leases are returned', async () => {
    render(DynamicSecretsView);
    await waitFor(() => {
      expect(screen.queryByText(/no roles found/i)).toBeTruthy();
      expect(screen.queryByText(/no active leases/i)).toBeTruthy();
    });
  });

  it('degrades gracefully when listRoles fails', async () => {
    mockListRoles.mockRejectedValue(new Error('roles boom'));
    render(DynamicSecretsView);
    await waitFor(() => {
      expect(screen.getByText('roles boom')).toBeInTheDocument();
    });
  });

  it('degrades gracefully when listLeases fails', async () => {
    mockListLeases.mockRejectedValue(new Error('leases boom'));
    render(DynamicSecretsView);
    await waitFor(() => {
      expect(screen.getByText('leases boom')).toBeInTheDocument();
    });
  });

  it('renders role and lease rows when present', async () => {
    mockListRoles.mockResolvedValue({ roles: [role({ name: 'db-readonly' })], count: 1, timestamp: '' });
    mockListLeases.mockResolvedValue({ leases: [lease({ path: 'lease-path' })], count: 1, timestamp: '' });
    render(DynamicSecretsView);
    await waitFor(() => {
      expect(screen.getByText('db-readonly')).toBeInTheDocument();
      expect(screen.getByText('lease-abc')).toBeInTheDocument();
    });
  });
});

describe('DynamicSecretsView — role CRUD', () => {
  it('creates a role with the entered fields', async () => {
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(mockListRoles).toHaveBeenCalled());

    const createBtn = container.querySelector('button[aria-label="Create role"]') as HTMLButtonElement;
    await fireEvent.click(createBtn);
    await waitFor(() => expect(container.querySelector('#role-name')).not.toBeNull());

    await fireEvent.input(container.querySelector('#role-name') as HTMLInputElement, {
      target: { value: 'db-admin' },
    });
    await fireEvent.input(container.querySelector('#role-generator') as HTMLInputElement, {
      target: { value: 'postgres' },
    });

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockCreateRole).toHaveBeenCalled());
    const req = mockCreateRole.mock.calls[0][0];
    expect(req.name).toBe('db-admin');
    expect(req.generator_type).toBe('postgres');
    expect(req.default_ttl).toBe('1h');
    expect(req.max_ttl).toBe('24h');
  });

  it('rejects submit client-side when name is missing', async () => {
    const { container } = render(DynamicSecretsView);
    const createBtn = container.querySelector('button[aria-label="Create role"]') as HTMLButtonElement;
    await fireEvent.click(createBtn);
    await waitFor(() => expect(container.querySelector('#role-name')).not.toBeNull());

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(screen.getByText(/role name is required/i)).toBeInTheDocument());
    expect(mockCreateRole).not.toHaveBeenCalled();
  });

  it('deletes a role after confirmation', async () => {
    mockListRoles.mockResolvedValue({ roles: [role({ name: 'db-readonly' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('db-readonly')).toBeInTheDocument());

    const deleteBtn = container.querySelector('button[aria-label="Delete role db-readonly"]') as HTMLButtonElement;
    await fireEvent.click(deleteBtn);
    await waitFor(() => expect(screen.getByText('Delete Role?')).toBeInTheDocument());

    const confirmBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Delete',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);

    await waitFor(() => expect(mockDeleteRole).toHaveBeenCalledWith('db-readonly'));
  });

  it('does not delete a role when the confirmation dialog is cancelled', async () => {
    mockListRoles.mockResolvedValue({ roles: [role({ name: 'db-readonly' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('db-readonly')).toBeInTheDocument());

    const deleteBtn = container.querySelector('button[aria-label="Delete role db-readonly"]') as HTMLButtonElement;
    await fireEvent.click(deleteBtn);
    await waitFor(() => expect(screen.getByText('Delete Role?')).toBeInTheDocument());

    const cancelBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Cancel',
    ) as HTMLButtonElement;
    await fireEvent.click(cancelBtn);

    expect(mockDeleteRole).not.toHaveBeenCalled();
  });
});

describe('DynamicSecretsView — generate credential (mint-once)', () => {
  it('generates a credential and shows the one-time secret result', async () => {
    mockListRoles.mockResolvedValue({ roles: [role({ name: 'db-readonly' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('db-readonly')).toBeInTheDocument());

    const generateBtn = container.querySelector('button[aria-label="Generate credential for db-readonly"]') as HTMLButtonElement;
    await fireEvent.click(generateBtn);
    await waitFor(() => expect(container.querySelector('#generate-ttl')).not.toBeNull());

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Generate',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockGenerateCredential).toHaveBeenCalledWith('db-readonly', { ttl: undefined }));
    await waitFor(() => {
      expect(screen.getByText(/shown only once/i)).toBeInTheDocument();
      expect(screen.getByText('super-secret-once')).toBeInTheDocument();
    });
  });

  it('discards the generated secret from component state once the dialog is closed', async () => {
    mockListRoles.mockResolvedValue({ roles: [role({ name: 'db-readonly' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('db-readonly')).toBeInTheDocument());

    const generateBtn = container.querySelector('button[aria-label="Generate credential for db-readonly"]') as HTMLButtonElement;
    await fireEvent.click(generateBtn);
    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Generate',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);
    await waitFor(() => expect(screen.getByText('super-secret-once')).toBeInTheDocument());

    const doneBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Done',
    ) as HTMLButtonElement;
    await fireEvent.click(doneBtn);

    expect(screen.queryByText('super-secret-once')).not.toBeInTheDocument();

    // Re-opening the dialog must not resurrect the previous secret — it can
    // only be produced by another generateCredential call.
    const generateBtnAgain = container.querySelector('button[aria-label="Generate credential for db-readonly"]') as HTMLButtonElement;
    await fireEvent.click(generateBtnAgain);
    expect(screen.queryByText('super-secret-once')).not.toBeInTheDocument();
    expect(container.querySelector('#generate-ttl')).not.toBeNull();
  });

  it('surfaces an error when credential generation fails', async () => {
    mockGenerateCredential.mockRejectedValue(new Error('role exhausted'));
    mockListRoles.mockResolvedValue({ roles: [role({ name: 'db-readonly' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('db-readonly')).toBeInTheDocument());

    const generateBtn = container.querySelector('button[aria-label="Generate credential for db-readonly"]') as HTMLButtonElement;
    await fireEvent.click(generateBtn);
    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Generate',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(screen.getByText('role exhausted')).toBeInTheDocument());
  });
});

describe('DynamicSecretsView — lease management', () => {
  it('renews a lease', async () => {
    mockListLeases.mockResolvedValue({ leases: [lease({ lease_id: 'lease-abc' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('lease-abc')).toBeInTheDocument());

    const renewBtn = container.querySelector('button[aria-label="Renew lease lease-abc"]') as HTMLButtonElement;
    await fireEvent.click(renewBtn);
    await waitFor(() => expect(screen.getByText('Renew Lease')).toBeInTheDocument());

    const submitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Renew',
    ) as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => expect(mockRenewLease).toHaveBeenCalledWith('lease-abc', { increment: undefined }));
  });

  it('disables renew for a non-renewable lease', async () => {
    mockListLeases.mockResolvedValue({
      leases: [lease({ lease_id: 'lease-frozen', renewable: false })],
      count: 1,
      timestamp: '',
    });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('lease-frozen')).toBeInTheDocument());

    const renewBtn = container.querySelector('button[aria-label="Renew lease lease-frozen"]') as HTMLButtonElement;
    expect(renewBtn.disabled).toBe(true);
  });

  it('revokes a lease after confirmation', async () => {
    mockListLeases.mockResolvedValue({ leases: [lease({ lease_id: 'lease-abc' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('lease-abc')).toBeInTheDocument());

    const revokeBtn = container.querySelector('button[aria-label="Revoke lease lease-abc"]') as HTMLButtonElement;
    await fireEvent.click(revokeBtn);
    await waitFor(() => expect(screen.getByText('Revoke Lease?')).toBeInTheDocument());

    const confirmBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Revoke',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);

    await waitFor(() => expect(mockRevokeLease).toHaveBeenCalledWith('lease-abc'));
  });

  it('does not revoke a lease when the confirmation dialog is cancelled', async () => {
    mockListLeases.mockResolvedValue({ leases: [lease({ lease_id: 'lease-abc' })], count: 1, timestamp: '' });
    const { container } = render(DynamicSecretsView);
    await waitFor(() => expect(screen.getByText('lease-abc')).toBeInTheDocument());

    const revokeBtn = container.querySelector('button[aria-label="Revoke lease lease-abc"]') as HTMLButtonElement;
    await fireEvent.click(revokeBtn);
    await waitFor(() => expect(screen.getByText('Revoke Lease?')).toBeInTheDocument());

    const cancelBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Cancel',
    ) as HTMLButtonElement;
    await fireEvent.click(cancelBtn);

    expect(mockRevokeLease).not.toHaveBeenCalled();
  });
});
