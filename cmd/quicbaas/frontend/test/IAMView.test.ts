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
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

/** An unsigned session token whose tenant claim is `tenantID`. */
function sessionToken(tenantID: string): string {
  const b64url = (o: unknown): string =>
    btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const claims = {
    sub: 'usr_1',
    tenant_id: tenantID,
    roles: ['operator'],
    sid: 'sid_1',
    exp: Math.floor(Date.now() / 1000) + 3600,
  };
  return `${b64url({ alg: 'none' })}.${b64url(claims)}.sig`;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('IAMView', () => {
  afterEach(() => {
    setUnauthenticated();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    // The operator is signed in to tenant 'acme'; IAMView must administer it.
    setAuthenticated({ access_token: sessionToken('acme') });
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

  describe('tenant selector (#312)', () => {
    function stubIAM(listTenants: ReturnType<typeof vi.fn>): ReturnType<typeof vi.fn> {
      const listUsers = vi.fn().mockResolvedValue({ users: [], count: 0 });
      mockGetSDKClient.mockResolvedValue({
        iam: {
          listUsers,
          listTenants,
          listRoles: vi.fn().mockResolvedValue({ roles: [], count: 0 }),
          listPolicies: vi.fn().mockResolvedValue({ policies: [], count: 0 }),
        },
      });
      return listUsers;
    }

    function optionValues(select: HTMLSelectElement): string[] {
      return Array.from(select.options).map((o) => o.value);
    }

    it('selects the session tenant and injects no system option when listing is refused', async () => {
      const listTenants = vi.fn().mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403 }));
      const listUsers = stubIAM(listTenants);

      render(IAMView);

      await waitFor(() => expect(listTenants).toHaveBeenCalled());
      const select = screen.getByLabelText('Tenant') as HTMLSelectElement;
      expect(select.value).toBe('acme');
      expect(optionValues(select)).toEqual(['acme']);
      expect(listUsers).toHaveBeenCalledWith('acme');
      expect(listUsers).not.toHaveBeenCalledWith('system');
      // A refused listing is not an error on the Users tab.
      expect(screen.queryByText('forbidden')).not.toBeInTheDocument();
    });

    it('adds the tenants a permitted principal may enumerate, keeping the session tenant selected', async () => {
      const listTenants = vi.fn().mockResolvedValue({
        tenants: [
          { id: 'globex', name: 'Globex', description: '', created_at: '', updated_at: '' },
          { id: 'acme', name: 'Acme', description: '', created_at: '', updated_at: '' },
        ],
        count: 2,
      });
      stubIAM(listTenants);

      render(IAMView);

      const select = screen.getByLabelText('Tenant') as HTMLSelectElement;
      await waitFor(() => expect(optionValues(select)).toEqual(['globex', 'acme']));
      expect(select.value).toBe('acme');
      expect(optionValues(select)).not.toContain('system');
    });

    it('keeps system selected for an operator signed in to the system tenant', async () => {
      setAuthenticated({ access_token: sessionToken('system') });
      const listTenants = vi.fn().mockRejectedValue(Object.assign(new Error('forbidden'), { status: 403 }));
      const listUsers = stubIAM(listTenants);

      render(IAMView);

      await waitFor(() => expect(listTenants).toHaveBeenCalled());
      const select = screen.getByLabelText('Tenant') as HTMLSelectElement;
      expect(select.value).toBe('system');
      expect(optionValues(select)).toEqual(['system']);
      expect(listUsers).toHaveBeenCalledWith('system');
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
        // IAMView starts on the session's own tenant (#312).
        'acme',
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
    mockGetSDKClient.mockResolvedValue({
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
  // #2372: the Roles dialog lists the user's roles and revokes them.
  // -------------------------------------------------------------------------

  describe('Roles dialog — revoke', () => {
    function stubRevoke(
      revokeRoleFromUser: ReturnType<typeof vi.fn>,
      users: IAMUser[] = mockUsers,
    ): ReturnType<typeof vi.fn> {
      const listUsers = vi.fn().mockResolvedValue({ users, count: users.length });
      mockGetSDKClient.mockResolvedValue({
        iam: {
          listUsers,
          listTenants: vi.fn().mockResolvedValue({ tenants: mockTenants, count: 1 }),
          listRoles: vi.fn().mockResolvedValue({ roles: mockRoles, count: 2 }),
          listPolicies: vi.fn().mockResolvedValue({ policies: mockPolicies, count: 1 }),
          assignRoleToUser: vi.fn().mockResolvedValue(undefined),
          revokeRoleFromUser,
        },
      });
      return listUsers;
    }

    async function openRolesDialog(username: string): Promise<HTMLElement> {
      render(IAMView);
      const open = await screen.findByRole('button', { name: `Assign role to ${username}` });
      await fireEvent.click(open);
      return screen.getByRole('dialog', { name: /Assign role dialog/i });
    }

    it('lists the user\'s roles by name', async () => {
      stubRevoke(vi.fn().mockResolvedValue(undefined));
      const dialog = await openRolesDialog('alice');
      expect(dialog.querySelector('h3')?.textContent).toBe('Roles of alice');
      const list = screen.getByRole('list', { name: 'Current roles' });
      expect(list.textContent).toContain('admin');
      expect(list.textContent).not.toContain('role-1');
      expect(screen.getByRole('button', { name: 'Revoke role admin from alice' })).toBeInTheDocument();
    });

    it('revokes the role by its UUID and shows the success toast', async () => {
      const revoke = vi.fn().mockResolvedValue(undefined);
      const listUsers = stubRevoke(revoke);
      await openRolesDialog('alice');
      const callsBefore = listUsers.mock.calls.length;
      await fireEvent.click(screen.getByRole('button', { name: 'Revoke role admin from alice' }));

      await waitFor(() => {
        expect(revoke).toHaveBeenCalledWith('acme', 'user-1', 'role-1');
        expect(mockShowToast).toHaveBeenCalledWith({ message: 'Role "admin" revoked from "alice"', type: 'success' });
        expect(screen.queryByRole('button', { name: 'Revoke role admin from alice' })).not.toBeInTheDocument();
        expect(listUsers.mock.calls.length).toBeGreaterThan(callsBefore);
      });
    });

    it('shows the error and keeps the role when the revoke is rejected', async () => {
      stubRevoke(vi.fn().mockRejectedValue(new Error('revoke denied')));
      const dialog = await openRolesDialog('alice');
      await fireEvent.click(screen.getByRole('button', { name: 'Revoke role admin from alice' }));

      await waitFor(() => {
        expect(dialog.querySelector('[role="alert"]')?.textContent).toBe('revoke denied');
      });
      const button = screen.getByRole('button', { name: 'Revoke role admin from alice' });
      expect(button).not.toBeDisabled();
      expect(mockShowToast).not.toHaveBeenCalled();
    });

    it('shows "No roles assigned" for a user with no roles', async () => {
      stubRevoke(vi.fn(), [{ ...mockUsers[0], roles: [] }]);
      const dialog = await openRolesDialog('alice');
      expect(dialog.textContent).toContain('No roles assigned');
      expect(screen.queryByRole('button', { name: /^Revoke role/ })).not.toBeInTheDocument();
    });
  });

  describe('Consents dialog', () => {
    const granted = {
      id: 'c-1', user_id: 'user-1', tenant_id: 'acme', purpose: 'marketing', status: 'granted',
      version: 'v1', granted_at: '2026-01-02T03:04:05Z', created: '2026-01-02T03:04:05Z', updated: '2026-01-02T03:04:05Z',
    };
    const withdrawn = {
      id: 'c-2', user_id: 'user-1', tenant_id: 'acme', purpose: 'analytics', status: 'withdrawn',
      version: 'v2', granted_at: '2026-01-01T00:00:00Z', withdrawn_at: '2026-02-01T00:00:00Z',
      created: '2026-01-01T00:00:00Z', updated: '2026-02-01T00:00:00Z',
    };

    function stubConsents(overrides: Record<string, ReturnType<typeof vi.fn>>): void {
      mockGetSDKClient.mockResolvedValue({
        iam: {
          listUsers: vi.fn().mockResolvedValue({ users: mockUsers, count: mockUsers.length }),
          listTenants: vi.fn().mockResolvedValue({ tenants: mockTenants, count: 1 }),
          listRoles: vi.fn().mockResolvedValue({ roles: mockRoles, count: 2 }),
          listPolicies: vi.fn().mockResolvedValue({ policies: mockPolicies, count: 1 }),
          listUserConsents: vi.fn().mockResolvedValue([granted, withdrawn]),
          grantConsent: vi.fn().mockResolvedValue(granted),
          withdrawConsent: vi.fn().mockResolvedValue({ ...granted, status: 'withdrawn' }),
          ...overrides,
        },
      });
    }

    async function openConsentsDialog(username: string): Promise<HTMLElement> {
      render(IAMView);
      const open = await screen.findByRole('button', { name: `Consents of ${username}` });
      await fireEvent.click(open);
      return screen.getByRole('dialog', { name: /Consents dialog/i });
    }

    it('opens from the user row and lists the user\'s consents', async () => {
      const list = vi.fn().mockResolvedValue([granted, withdrawn]);
      stubConsents({ listUserConsents: list });
      const dialog = await openConsentsDialog('alice');
      expect(dialog.querySelector('h3')?.textContent).toBe('Consents of alice');
      const table = await screen.findByRole('table', { name: 'User consents' });
      expect(list).toHaveBeenCalledWith('acme', 'user-1');
      expect(table.textContent).toContain('marketing');
      expect(table.textContent).toContain('granted');
      expect(table.textContent).toContain('v1');
      expect(table.textContent).toContain('analytics');
      expect(table.textContent).toContain('withdrawn');
      expect(screen.getByRole('button', { name: 'Withdraw consent marketing of alice' })).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: 'Withdraw consent analytics of alice' })).not.toBeInTheDocument();
    });

    it('shows "No consents recorded" for a user with none', async () => {
      stubConsents({ listUserConsents: vi.fn().mockResolvedValue([]) });
      const dialog = await openConsentsDialog('alice');
      await waitFor(() => expect(dialog.textContent).toContain('No consents recorded'));
    });

    it('grants a consent with the purpose and version, toasts and reloads', async () => {
      const list = vi.fn().mockResolvedValue([]);
      const grant = vi.fn().mockResolvedValue(granted);
      stubConsents({ listUserConsents: list, grantConsent: grant });
      await openConsentsDialog('alice');
      await waitFor(() => expect(list).toHaveBeenCalledTimes(1));
      await fireEvent.change(screen.getByLabelText('Purpose'), { target: { value: 'marketing' } });
      await fireEvent.input(screen.getByLabelText('Version'), { target: { value: 'v1' } });
      await fireEvent.click(screen.getByRole('button', { name: 'Grant' }));

      await waitFor(() => {
        expect(grant).toHaveBeenCalledWith('acme', 'user-1', { purpose: 'marketing', version: 'v1' });
        expect(mockShowToast).toHaveBeenCalledWith({ message: 'Consent "marketing" granted for "alice"', type: 'success' });
        expect(list).toHaveBeenCalledTimes(2);
      });
    });

    it('withdraws a granted consent by its purpose, toasts and reloads', async () => {
      const list = vi.fn().mockResolvedValue([granted]);
      const withdraw = vi.fn().mockResolvedValue({ ...granted, status: 'withdrawn' });
      stubConsents({ listUserConsents: list, withdrawConsent: withdraw });
      await openConsentsDialog('alice');
      await fireEvent.click(await screen.findByRole('button', { name: 'Withdraw consent marketing of alice' }));

      await waitFor(() => {
        expect(withdraw).toHaveBeenCalledWith('acme', 'user-1', 'marketing');
        expect(mockShowToast).toHaveBeenCalledWith({ message: 'Consent "marketing" withdrawn for "alice"', type: 'success' });
        expect(list).toHaveBeenCalledTimes(2);
      });
    });

    it('shows the SDK error in the dialog when the withdraw is rejected', async () => {
      stubConsents({
        listUserConsents: vi.fn().mockResolvedValue([granted]),
        withdrawConsent: vi.fn().mockRejectedValue(new Error('withdraw denied')),
      });
      const dialog = await openConsentsDialog('alice');
      await fireEvent.click(await screen.findByRole('button', { name: 'Withdraw consent marketing of alice' }));

      await waitFor(() => {
        expect(dialog.querySelector('[role="alert"]')?.textContent).toBe('withdraw denied');
      });
      expect(mockShowToast).not.toHaveBeenCalled();
    });

    it('shows the SDK error in the dialog when the list fails', async () => {
      stubConsents({ listUserConsents: vi.fn().mockRejectedValue(new Error('consents unavailable')) });
      const dialog = await openConsentsDialog('alice');
      await waitFor(() => {
        expect(dialog.querySelector('[role="alert"]')?.textContent).toBe('consents unavailable');
      });
    });
  });

  describe('Sessions dialog', () => {
    const session = {
      id: 's-1', created: '2026-01-02T03:04:05Z', last_active: '2026-01-03T03:04:05Z',
      expires_at: '2026-02-02T03:04:05Z', mfa_verified: true, ip_address: '203.0.113.7',
      user_agent: 'Mozilla/5.0 TestBrowser',
      device_info: { id: 'd-1', name: 'Work laptop', type: 'desktop', fingerprint: 'fp', trusted: true },
    };

    function stubSessions(overrides: Record<string, ReturnType<typeof vi.fn>>): void {
      mockGetSDKClient.mockResolvedValue({
        iam: {
          listUsers: vi.fn().mockResolvedValue({ users: mockUsers, count: mockUsers.length }),
          listTenants: vi.fn().mockResolvedValue({ tenants: mockTenants, count: 1 }),
          listRoles: vi.fn().mockResolvedValue({ roles: mockRoles, count: 2 }),
          listPolicies: vi.fn().mockResolvedValue({ policies: mockPolicies, count: 1 }),
          listUserSessions: vi.fn().mockResolvedValue({ sessions: [session] }),
          revokeAllUserSessions: vi.fn().mockResolvedValue(undefined),
          ...overrides,
        },
      });
    }

    async function openSessionsDialog(username: string): Promise<HTMLElement> {
      render(IAMView);
      const open = await screen.findByRole('button', { name: `Sessions of ${username}` });
      await fireEvent.click(open);
      return screen.getByRole('dialog', { name: /Sessions dialog/i });
    }

    it('opens from the user row and lists the user\'s sessions', async () => {
      const list = vi.fn().mockResolvedValue({ sessions: [session] });
      stubSessions({ listUserSessions: list });
      const dialog = await openSessionsDialog('alice');
      expect(dialog.querySelector('h3')?.textContent).toBe('Sessions of alice');
      const table = await screen.findByRole('table', { name: 'User sessions' });
      expect(list).toHaveBeenCalledWith('acme', 'user-1');
      expect(table.textContent).toContain('203.0.113.7');
      expect(table.textContent).toContain('Mozilla/5.0 TestBrowser');
      expect(table.textContent).toContain('verified');
      expect(table.textContent).toContain('Work laptop');
    });

    it('shows "No active sessions" for a user with none', async () => {
      stubSessions({ listUserSessions: vi.fn().mockResolvedValue({ sessions: [] }) });
      const dialog = await openSessionsDialog('alice');
      await waitFor(() => expect(dialog.textContent).toContain('No active sessions'));
    });

    it('revokes all sessions after confirmation, toasts and reloads', async () => {
      const list = vi.fn().mockResolvedValue({ sessions: [session] });
      const revoke = vi.fn().mockResolvedValue(undefined);
      stubSessions({ listUserSessions: list, revokeAllUserSessions: revoke });
      await openSessionsDialog('alice');
      await screen.findByRole('table', { name: 'User sessions' });
      await fireEvent.click(screen.getByRole('button', { name: 'Revoke all sessions' }));
      expect(revoke).not.toHaveBeenCalled();
      screen.getByRole('dialog', { name: 'Revoke sessions confirmation' });
      await fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));

      await waitFor(() => {
        expect(revoke).toHaveBeenCalledWith('acme', 'user-1');
        expect(mockShowToast).toHaveBeenCalledWith({ message: 'All sessions of "alice" revoked', type: 'success' });
        expect(list).toHaveBeenCalledTimes(2);
      });
      expect(screen.queryByRole('dialog', { name: 'Revoke sessions confirmation' })).not.toBeInTheDocument();
    });

    it('shows the SDK error in the dialog when the list fails', async () => {
      stubSessions({ listUserSessions: vi.fn().mockRejectedValue(new Error('sessions unavailable')) });
      const dialog = await openSessionsDialog('alice');
      await waitFor(() => {
        expect(dialog.querySelector('[role="alert"]')?.textContent).toBe('sessions unavailable');
      });
    });

    it('shows the SDK error in the dialog when the revoke is rejected', async () => {
      stubSessions({ revokeAllUserSessions: vi.fn().mockRejectedValue(new Error('revoke denied')) });
      const dialog = await openSessionsDialog('alice');
      await screen.findByRole('table', { name: 'User sessions' });
      await fireEvent.click(screen.getByRole('button', { name: 'Revoke all sessions' }));
      await fireEvent.click(screen.getByRole('button', { name: 'Revoke' }));
      await waitFor(() => {
        expect(dialog.querySelector('[role="alert"]')?.textContent).toBe('revoke denied');
      });
      expect(mockShowToast).not.toHaveBeenCalled();
    });
  });

  describe('MFA dialog', () => {
    const enabled = {
      enabled: true,
      method: 'totp',
      enrolled_at: '2026-01-02T03:04:05Z',
      last_verified: '2026-02-03T04:05:06Z',
      backup_codes_total: 10,
      backup_codes_remaining: 7,
    };
    const disabled = { enabled: false };

    function stubMFA(overrides: Record<string, ReturnType<typeof vi.fn>>): void {
      mockGetSDKClient.mockResolvedValue({
        iam: {
          listUsers: vi.fn().mockResolvedValue({ users: mockUsers, count: mockUsers.length }),
          listTenants: vi.fn().mockResolvedValue({ tenants: mockTenants, count: 1 }),
          listRoles: vi.fn().mockResolvedValue({ roles: mockRoles, count: 2 }),
          listPolicies: vi.fn().mockResolvedValue({ policies: mockPolicies, count: 1 }),
          getMFAStatus: vi.fn().mockResolvedValue(enabled),
          disableMFA: vi.fn().mockResolvedValue(undefined),
          ...overrides,
        },
      });
    }

    async function openMFADialog(username: string): Promise<HTMLElement> {
      render(IAMView);
      const open = await screen.findByRole('button', { name: `MFA of ${username}` });
      await fireEvent.click(open);
      return screen.getByRole('dialog', { name: /^MFA dialog$/ });
    }

    it('opens from the user row and shows every status field', async () => {
      const get = vi.fn().mockResolvedValue(enabled);
      stubMFA({ getMFAStatus: get });
      const dialog = await openMFADialog('alice');
      expect(dialog.querySelector('h3')?.textContent).toBe('MFA of alice');
      const table = await screen.findByRole('table', { name: 'MFA status' });
      expect(get).toHaveBeenCalledWith('acme', 'user-1');
      expect(table.textContent).toContain('Enabled');
      expect(table.textContent).toContain('totp');
      expect(table.textContent).toContain('Enrolled');
      expect(table.textContent).toContain('Last verified');
      expect(table.textContent).toContain('7 of 10 remaining');
      expect(screen.getByRole('button', { name: 'Disable MFA' })).toBeInTheDocument();
    });

    it('hides the Disable button when MFA is off', async () => {
      stubMFA({ getMFAStatus: vi.fn().mockResolvedValue(disabled) });
      await openMFADialog('alice');
      const table = await screen.findByRole('table', { name: 'MFA status' });
      expect(table.textContent).toContain('Disabled');
      expect(table.textContent).not.toContain('Method');
      expect(screen.queryByRole('button', { name: 'Disable MFA' })).not.toBeInTheDocument();
    });

    it('disables MFA after confirmation, toasts and reloads', async () => {
      const get = vi.fn().mockResolvedValueOnce(enabled).mockResolvedValue(disabled);
      const disable = vi.fn().mockResolvedValue(undefined);
      stubMFA({ getMFAStatus: get, disableMFA: disable });
      await openMFADialog('alice');
      await fireEvent.click(await screen.findByRole('button', { name: 'Disable MFA' }));
      expect(disable).not.toHaveBeenCalled();
      const confirm = screen.getByRole('dialog', { name: 'Disable MFA confirmation' });
      const confirmButton = Array.from(confirm.querySelectorAll('button')).find((b) => b.textContent === 'Disable');
      expect(confirmButton).toBeDefined();
      if (confirmButton) await fireEvent.click(confirmButton);

      await waitFor(() => {
        expect(disable).toHaveBeenCalledWith('acme', 'user-1');
        expect(mockShowToast).toHaveBeenCalledWith({ message: 'MFA disabled for "alice"', type: 'success' });
        expect(get).toHaveBeenCalledTimes(2);
      });
      await waitFor(() => {
        expect(screen.queryByRole('button', { name: 'Disable MFA' })).not.toBeInTheDocument();
      });
      expect(screen.queryByRole('dialog', { name: 'Disable MFA confirmation' })).not.toBeInTheDocument();
    });

    it('shows the SDK error in the dialog when the status load fails', async () => {
      stubMFA({ getMFAStatus: vi.fn().mockRejectedValue(new Error('mfa unavailable')) });
      const dialog = await openMFADialog('alice');
      await waitFor(() => {
        expect(dialog.querySelector('[role="alert"]')?.textContent).toBe('mfa unavailable');
      });
    });

    it('shows the SDK error in the dialog when the disable is rejected', async () => {
      stubMFA({ disableMFA: vi.fn().mockRejectedValue(new Error('disable denied')) });
      const dialog = await openMFADialog('alice');
      await fireEvent.click(await screen.findByRole('button', { name: 'Disable MFA' }));
      const confirm = screen.getByRole('dialog', { name: 'Disable MFA confirmation' });
      const confirmButton = Array.from(confirm.querySelectorAll('button')).find((b) => b.textContent === 'Disable');
      if (confirmButton) await fireEvent.click(confirmButton);
      await waitFor(() => {
        expect(dialog.querySelector('[role="alert"]')?.textContent).toBe('disable denied');
      });
      expect(mockShowToast).not.toHaveBeenCalled();
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
        // IAMView starts on the session's own tenant (#312).
        expect(mockReconcileRoles).toHaveBeenCalledWith('acme');
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

describe('Provision tenant', () => {
  let provisionTenant: ReturnType<typeof vi.fn>;
  let listTenants: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.clearAllMocks();
    setAuthenticated({ access_token: sessionToken('acme') });
    provisionTenant = vi.fn().mockResolvedValue({
      tenant_id: 'globex',
      admin_user_id: 'usr_admin',
      admin_username: 'root',
      admin_role_id: 'role_admin',
      admin_pending: true,
      invite_token: 'inv-secret-token',
      invite_expires_at: '2026-11-13T00:00:00Z',
      timestamp: '2026-11-12T00:00:00Z',
    });
    listTenants = vi.fn().mockResolvedValue({ tenants: mockTenants, count: 1 });
    mockGetSDKClient.mockResolvedValue({
      iam: {
        listUsers: vi.fn().mockResolvedValue({ users: mockUsers, count: 2 }),
        listTenants,
        listRoles: vi.fn().mockResolvedValue({ roles: mockRoles, count: 1 }),
        listPolicies: vi.fn().mockResolvedValue({ policies: mockPolicies, count: 1 }),
        createTenant: vi.fn(),
        provisionTenant,
      },
    });
  });

  afterEach(() => {
    routeIAMTab('users');
    setUnauthenticated();
  });

  async function openDialog(): Promise<void> {
    render(IAMView);
    await fireEvent.click(screen.getByRole('tab', { name: /Tenants/i }));
    await fireEvent.click(await screen.findByRole('button', { name: 'Provision tenant' }));
    await screen.findByRole('dialog', { name: 'Provision tenant dialog' });
  }

  async function submit(): Promise<void> {
    await fireEvent.click(screen.getByRole('button', { name: 'Provision' }));
  }

  it('sends the invite-mode fields and never a CSR', async () => {
    await openDialog();
    await fireEvent.input(screen.getByLabelText('Tenant ID'), { target: { value: 'globex' } });
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Globex' } });
    await fireEvent.input(screen.getByLabelText('Admin username'), { target: { value: 'root' } });
    await fireEvent.input(screen.getByLabelText('Invite expiry (hours)'), { target: { value: '48' } });
    await submit();
    await waitFor(() => expect(provisionTenant).toHaveBeenCalledTimes(1));
    const req = provisionTenant.mock.calls[0][0];
    expect(req).toEqual({
      tenant_id: 'globex',
      name: 'Globex',
      admin_username: 'root',
      admin_enrollment: 'invite',
      invite_expiry_hours: 48,
    });
    expect(req).not.toHaveProperty('admin_csr_pem');
    expect(req).not.toHaveProperty('isolation_type');
  });

  it('requires the CSR in cert mode and sends it', async () => {
    await openDialog();
    await fireEvent.input(screen.getByLabelText('Tenant ID'), { target: { value: 'globex' } });
    await fireEvent.click(screen.getByLabelText('Certificate (CSR)'));
    await submit();
    expect(await screen.findByRole('alert')).toHaveTextContent(/CSR PEM is required/);
    expect(provisionTenant).not.toHaveBeenCalled();
    const csr = '-----BEGIN CERTIFICATE REQUEST-----\nabc\n-----END CERTIFICATE REQUEST-----';
    await fireEvent.input(screen.getByLabelText('Admin CSR PEM'), { target: { value: csr } });
    await submit();
    await waitFor(() => expect(provisionTenant).toHaveBeenCalledTimes(1));
    const req = provisionTenant.mock.calls[0][0];
    expect(req).toEqual({ tenant_id: 'globex', admin_enrollment: 'cert', admin_csr_pem: csr });
    expect(req).not.toHaveProperty('invite_expiry_hours');
  });

  it('calls nothing without a tenant id', async () => {
    await openDialog();
    await submit();
    expect(await screen.findByRole('alert')).toHaveTextContent('Tenant ID is required');
    expect(provisionTenant).not.toHaveBeenCalled();
  });

  it('shows the invite token once, clears it on close and reloads the list', async () => {
    await openDialog();
    await fireEvent.input(screen.getByLabelText('Tenant ID'), { target: { value: 'globex' } });
    await submit();
    const result = await screen.findByRole('dialog', { name: 'Tenant provisioned dialog' });
    expect(result).toHaveTextContent('usr_admin');
    expect(result).toHaveTextContent('role_admin');
    expect(result).toHaveTextContent(/shown once/);
    expect(screen.getByLabelText('Invite token')).toHaveValue('inv-secret-token');
    expect(screen.getByRole('button', { name: 'Copy invite token' })).toBeInTheDocument();
    const loadsBefore = listTenants.mock.calls.length;
    await fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Tenant provisioned dialog' })).toBeNull());
    expect(screen.queryByDisplayValue('inv-secret-token')).toBeNull();
    await waitFor(() => expect(listTenants.mock.calls.length).toBeGreaterThan(loadsBefore));
  });

  it('shows an SDK error in the alert', async () => {
    provisionTenant.mockRejectedValue(new Error('tenant already exists'));
    await openDialog();
    await fireEvent.input(screen.getByLabelText('Tenant ID'), { target: { value: 'globex' } });
    await submit();
    expect(await screen.findByRole('alert')).toHaveTextContent('tenant already exists');
    expect(screen.getByRole('dialog', { name: 'Provision tenant dialog' })).toBeInTheDocument();
  });
});
