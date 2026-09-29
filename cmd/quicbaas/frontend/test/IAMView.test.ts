/**
 * IAMView.test.ts
 *
 * Verifies that IAMView:
 *   - Renders the page heading and the four tabs.
 *   - Shows the users table when the SDK returns data.
 *   - Opens the Invite User dialog when the button is clicked.
 *   - Calls sdk.iam.createUser with the correct shape.
 *   - Shows an error message when the API call fails.
 *   - Flags retired permissions on the Roles tab and reconciles built-in roles.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { IAMUser, IAMTenant, IAMRole, IAMPolicy } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

// Production reality: user.roles contains role UUIDs, not role names.
// The view must resolve each UUID to its name via the loaded roles list.
const mockUsers: IAMUser[] = [
  {
    id: 'user-1',
    tenant_id: 'default',
    username: 'alice',
    email: 'alice@example.com',
    roles: ['role-1'],   // UUID, not name
    lifecycle: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: 'user-2',
    tenant_id: 'default',
    username: 'bob',
    email: 'bob@example.com',
    roles: ['role-2'],   // UUID, not name
    lifecycle: 'pending',
    created_at: '2025-01-02T00:00:00Z',
    updated_at: '2025-01-02T00:00:00Z',
  },
];

const mockTenants: IAMTenant[] = [
  {
    id: 'default',
    name: 'default',
    description: 'Default tenant',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
];

const mockRoles: IAMRole[] = [
  {
    id: 'role-1',
    tenant_id: 'default',
    name: 'admin',
    description: 'Full access',
    permissions: ['read', 'write', 'delete'],
    // Drift: the tenant was seeded with 'kv:put' but this build renamed it.
    retired_permissions: ['kv:put'],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: 'role-2',
    tenant_id: 'default',
    name: 'viewer',
    description: 'Read-only access',
    permissions: ['read'],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
];

const mockPolicies: IAMPolicy[] = [
  {
    name: 'default-policy',
    tenant_id: 'default',
    description: 'Default ABAC policy',
    rules: [],
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
];

// ---------------------------------------------------------------------------
// Hoisted mocks (safe to reference inside vi.mock factories)
// ---------------------------------------------------------------------------

const { mockCreateUser, mockGetSDKClient, mockReconcileRoles, mockShowToast } = vi.hoisted(() => ({
  mockCreateUser: vi.fn(),
  mockGetSDKClient: vi.fn(),
  mockReconcileRoles: vi.fn(),
  mockShowToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: mockShowToast,
}));

import IAMView from '../src/views/security/IAMView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { routerStore, routeIAMTab } from '../src/stores/router.svelte.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('IAMView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateUser.mockResolvedValue({ user: mockUsers[0] });
    mockReconcileRoles.mockResolvedValue({ tenant_id: 'system', changed: true, roles: [{ role_id: 'role-1', name: 'admin', added: ['kv:write'], removed: ['kv:put'] }] });
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listUsers: vi.fn().mockResolvedValue({ users: mockUsers, count: 2 }),
        listTenants: vi.fn().mockResolvedValue({ tenants: mockTenants, count: 1 }),
        listRoles: vi.fn().mockResolvedValue({ roles: mockRoles, count: 1 }),
        listPolicies: vi.fn().mockResolvedValue({ policies: mockPolicies, count: 1 }),
        createUser: mockCreateUser,
        deleteUser: vi.fn().mockResolvedValue(undefined),
        createTenant: vi.fn().mockResolvedValue({ tenant: mockTenants[0] }),
        reconcileRoles: mockReconcileRoles,
      },
    });
  });

  it('renders the page heading "IAM"', async () => {
    render(IAMView);
    await waitFor(() => {
      expect(screen.getByText('IAM')).toBeInTheDocument();
    });
  });

  it('renders all four tab labels', async () => {
    render(IAMView);
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: /Users/i })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: /Tenants/i })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: /Roles/i })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: /Policies/i })).toBeInTheDocument();
    });
  });

  it('renders user rows from the API response', async () => {
    render(IAMView);
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
      expect(screen.getByText('bob')).toBeInTheDocument();
    });
  });

  it('shows each account\'s lifecycle as its status, not a derived enabled flag', async () => {
    render(IAMView);
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    // A pending account must read as pending: the old boolean rendered every
    // account without an explicit false as "active".
    expect(screen.getByText('active')).toBeInTheDocument();
    expect(screen.getByText('pending')).toBeInTheDocument();
  });

  it('shows "Invite User" button on the Users tab', async () => {
    render(IAMView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Invite User/i })).toBeInTheDocument();
    });
  });

  it('opens the Invite User dialog when the button is clicked', async () => {
    render(IAMView);
    await waitFor(() => screen.getByRole('button', { name: /Invite User/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Invite User/i }));
    expect(screen.getByRole('dialog', { name: /Invite user dialog/i })).toBeInTheDocument();
  });

  // Bug #57 regression: button label MUST match dialog title (no Create vs Invite mismatch).
  it('button label "Invite User" matches dialog title "Invite User"', async () => {
    render(IAMView);
    await waitFor(() => screen.getByRole('button', { name: /Invite User/i }));
    const cta = screen.getByRole('button', { name: /Invite User/i });
    // Strip leading "+ " prefix used in the visual treatment.
    const ctaText = (cta.textContent ?? '').replace(/^\s*\+\s*/, '').trim();
    await fireEvent.click(cta);
    const dialog = await screen.findByRole('dialog', { name: /Invite user dialog/i });
    const heading = dialog.querySelector('h3');
    expect(heading).not.toBeNull();
    const headingText = (heading?.textContent ?? '').trim();
    expect(ctaText).toBe(headingText);
    // And neither side should mention "Create" — the flow is invite-based.
    expect(ctaText).not.toMatch(/Create/i);
    expect(headingText).not.toMatch(/Create/i);
  });

  it('calls iam.createUser with passwordless invite shape on form submit', async () => {
    render(IAMView);
    await waitFor(() => screen.getByRole('button', { name: /Invite User/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Invite User/i }));

    await fireEvent.input(screen.getByLabelText('Username'), { target: { value: 'charlie' } });
    await fireEvent.input(screen.getByLabelText('Email'), { target: { value: 'charlie@example.com' } });
    // No password field — auth is FIDO2/PIV-only via the invite flow.
    await fireEvent.click(screen.getByRole('button', { name: /Send Invite/i }));

    await waitFor(() => {
      expect(mockCreateUser).toHaveBeenCalledWith(
        // IAMView defaults selectedTenantID to qrdb's canonical 'system' tenant
        // (the legacy 'default' tenant was retired).
        'system',
        expect.objectContaining({
          username: 'charlie',
          email: 'charlie@example.com',
        }),
      );
      // The invite payload MUST NOT carry a password field — auth is passwordless.
      const callArgs = mockCreateUser.mock.calls[0][1];
      expect(callArgs).not.toHaveProperty('password');
    });
  });

  it('shows validation error when required fields are empty on create', async () => {
    render(IAMView);
    await waitFor(() => screen.getByRole('button', { name: /Invite User/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Invite User/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Send Invite/i }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument();
    });
  });

  it('shows an error message when listUsers fails', async () => {
    mockGetSDKClient.mockResolvedValueOnce({
      iam: {
        listUsers: vi.fn().mockRejectedValue(new Error('Server error')),
        listTenants: vi.fn().mockResolvedValue({ tenants: [], count: 0 }),
        listRoles: vi.fn().mockResolvedValue({ roles: [], count: 0 }),
        listPolicies: vi.fn().mockResolvedValue({ policies: [], count: 0 }),
        createUser: vi.fn(),
        deleteUser: vi.fn(),
        createTenant: vi.fn(),
      },
    });
    render(IAMView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Server error');
    });
  });

  // -------------------------------------------------------------------------
  // Bug C regression: Users table must bind to 'username' and 'created_at'
  // fields on the IAMUser object. The API returns 'name' and 'created' which
  // the SDK now normalizes to 'username' and 'created_at' respectively.
  // -------------------------------------------------------------------------

  it('renders the username column from user.username (not undefined)', async () => {
    render(IAMView);
    await waitFor(() => {
      // Both usernames must appear in the table — not empty, not "undefined".
      const cells = screen.getAllByText(/^alice$|^bob$/);
      expect(cells.length).toBeGreaterThanOrEqual(2);
    });
  });

  it('does not render "undefined" in any username or created cell', async () => {
    render(IAMView);
    await waitFor(() => {
      expect(screen.getByText('alice')).toBeInTheDocument();
    });
    // No cell should ever contain the string "undefined".
    const tableBody = screen.queryAllByRole('cell');
    for (const cell of tableBody) {
      expect(cell.textContent).not.toBe('undefined');
    }
  });

  // BUG 2 regression: user.roles contains role UUIDs from the backend; the view
  // must resolve each UUID to the human-readable role name via the loaded roles
  // list and never display raw UUIDs to operators.
  it('resolves role UUIDs to role names in the Users table', async () => {
    render(IAMView);
    await waitFor(() => {
      // alice carries role-1 (UUID) which must render as 'admin' (name).
      expect(screen.getByText('admin')).toBeInTheDocument();
      // bob carries role-2 (UUID) which must render as 'viewer' (name).
      expect(screen.getByText('viewer')).toBeInTheDocument();
      // Raw UUIDs must NOT be visible to operators.
      expect(screen.queryByText('role-1')).not.toBeInTheDocument();
      expect(screen.queryByText('role-2')).not.toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // Issue #573: built-in roles are seeded once and never reconciled, so a
  // permission rename in the server build strands every existing tenant. The
  // Roles tab must surface that drift and offer the repair.
  //
  // Selecting a tab pushes it into the location hash, so this block restores
  // the Users tab afterwards — the sibling tests above render the Users table.
  // -------------------------------------------------------------------------

  describe('Roles tab — drift and reconcile', () => {
    afterEach(async () => {
      routeIAMTab('users');
      await waitFor(() => {
        expect(routerStore.params['tab']).toBe('users');
      });
    });

    async function openRolesTab(): Promise<void> {
      render(IAMView);
      await waitFor(() => screen.getByRole('tab', { name: /Roles/i }));
      await fireEvent.click(screen.getByRole('tab', { name: /Roles/i }));
      await waitFor(() => screen.getByRole('button', { name: /Reconcile roles/i }));
    }

    it('renders retired permissions as warning badges', async () => {
      await openRolesTab();
      const retired = await screen.findByTitle(/no longer defines "kv:put"/i);
      expect(retired).toBeInTheDocument();
      expect(retired.textContent).toBe('kv:put');
      expect(retired.className).toContain('badge-warning');
    });

    it('calls iam.reconcileRoles for the selected tenant and reports the change count', async () => {
      await openRolesTab();
      await fireEvent.click(screen.getByRole('button', { name: /Reconcile roles/i }));

      await waitFor(() => {
        // IAMView defaults selectedTenantID to qrdb's canonical 'system' tenant.
        expect(mockReconcileRoles).toHaveBeenCalledWith('system');
        expect(mockShowToast).toHaveBeenCalledWith(
          expect.objectContaining({ type: 'success', message: expect.stringContaining('Reconciled 1 role') }),
        );
      });
    });

    it('reports "already up to date" when reconcile changed nothing', async () => {
      mockReconcileRoles.mockResolvedValue({ tenant_id: 'system', changed: false, roles: [] });
      await openRolesTab();
      await fireEvent.click(screen.getByRole('button', { name: /Reconcile roles/i }));

      await waitFor(() => {
        expect(mockShowToast).toHaveBeenCalledWith(
          expect.objectContaining({ type: 'success', message: expect.stringContaining('already up to date') }),
        );
      });
    });

    it('surfaces an error toast when reconcile fails', async () => {
      mockReconcileRoles.mockRejectedValue(new Error('permission denied'));
      await openRolesTab();
      await fireEvent.click(screen.getByRole('button', { name: /Reconcile roles/i }));

      await waitFor(() => {
        expect(mockShowToast).toHaveBeenCalledWith(
          expect.objectContaining({ type: 'error', message: 'permission denied' }),
        );
      });
    });
  });

  it('renders created_at as a date string in the Created column', async () => {
    render(IAMView);
    await waitFor(() => {
      // The created_at '2025-01-01T00:00:00Z' must render as a date string.
      // toLocaleDateString produces locale-dependent output, but it is never empty.
      const allCells = screen.queryAllByRole('cell');
      const createdCells = allCells.filter((c) =>
        /2025|Jan|1\/1|01\/01|1-1/.test(c.textContent ?? ''),
      );
      expect(createdCells.length).toBeGreaterThan(0);
    });
  });
});
