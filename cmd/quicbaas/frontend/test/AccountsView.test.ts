/**
 * AccountsView.test.ts
 *
 * Verifies that AccountsView mounts, calls listAccounts on init, exposes a
 * create-account affordance, renders the empty state when no accounts are
 * returned, surfaces status/default badges and last_error, and that the
 * create/rotate credential envelopes respect the refresh_policy contract
 * (refresh_token required for oauth_refresh, forbidden for static_token).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import type { Account, Project } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: { activeTenantID: 'default' },
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import AccountsView from '../src/views/orchestrator/AccountsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const mockListAccounts = vi.fn();
const mockCreateAccount = vi.fn();
const mockRotateAccountCredential = vi.fn();
const mockSetDefaultAccount = vi.fn();
const mockRevokeAccount = vi.fn();
const mockDeleteAccount = vi.fn();
const mockGetAccountUsage = vi.fn();
const mockListProjects = vi.fn();

function mockClient(accounts: Account[] = [], projects: Project[] = []): void {
  mockListAccounts.mockResolvedValue({ accounts, total: accounts.length });
  mockListProjects.mockResolvedValue({ projects, total: projects.length });
  vi.mocked(getSDKClient).mockResolvedValue({
    accounts: {
      listAccounts: mockListAccounts,
      createAccount: mockCreateAccount,
      rotateAccountCredential: mockRotateAccountCredential,
      setDefaultAccount: mockSetDefaultAccount,
      revokeAccount: mockRevokeAccount,
      deleteAccount: mockDeleteAccount,
      getAccountUsage: mockGetAccountUsage,
    },
    projects: {
      listProjects: mockListProjects,
    },
  } as never);
}

function account(overrides: Partial<Account> = {}): Account {
  return {
    id: 'acct-1',
    tenant_id: 'default',
    provider: 'claude',
    display_name: 'alice-claude',
    default: false,
    status: 'active',
    refresh_policy: 'oauth_refresh',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockCreateAccount.mockResolvedValue({ account: account() });
  mockRotateAccountCredential.mockResolvedValue({ account_id: 'acct-1' });
  mockSetDefaultAccount.mockResolvedValue({ account_id: 'acct-1' });
  mockRevokeAccount.mockResolvedValue({ account: account({ status: 'revoked' }) });
  mockDeleteAccount.mockResolvedValue({ account_id: 'acct-1', deleted: true });
  mockGetAccountUsage.mockResolvedValue({ usage: { total_tokens: 0, budget_limit: 0, percent_used: 0, last_updated: 0 } });
  mockClient([]);
});

describe('AccountsView', () => {
  it('calls listAccounts on mount', async () => {
    render(AccountsView);
    await waitFor(() => {
      expect(mockListAccounts).toHaveBeenCalledWith({ tenant_id: 'default' });
    });
  });

  it('renders the page heading with icon and subtitle', async () => {
    const { container } = render(AccountsView);
    await waitFor(() => {
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('Accounts');
      const tile = container.querySelector('div.bg-primary\\/10');
      expect(tile).not.toBeNull();
      expect(tile!.querySelector('svg')).not.toBeNull();
    });
  });

  it('exposes a create-account button', async () => {
    render(AccountsView);
    await waitFor(() => {
      const candidates = screen.getAllByRole('button');
      const createBtn = candidates.find((b) =>
        /create|new|\+|add/i.test(b.textContent ?? '') && /account/i.test(b.textContent ?? ''),
      );
      expect(createBtn).toBeTruthy();
    });
  });

  it('renders an empty state when zero accounts are returned', async () => {
    render(AccountsView);
    await waitFor(() => {
      expect(screen.queryByText(/no accounts/i)).toBeTruthy();
    });
  });

  it('renders gracefully when listAccounts fails (non-fatal, like projects)', async () => {
    mockListAccounts.mockRejectedValue(new Error('boom'));
    render(AccountsView);
    await waitFor(() => {
      expect(screen.getByText(/Failed to load accounts/i)).toBeInTheDocument();
    });
  });
});

describe('AccountsView — list rendering', () => {
  it('shows provider, status, default, and project scope', async () => {
    mockClient([
      account({ id: 'acct-1', display_name: 'alice-claude', default: true, status: 'active' }),
      account({ id: 'acct-2', display_name: 'bob-codex', provider: 'codex', status: 'revoked', default: false, refresh_policy: 'static_token' }),
    ]);
    render(AccountsView);
    await waitFor(() => {
      expect(screen.getByText('alice-claude')).toBeInTheDocument();
      expect(screen.getByText('bob-codex')).toBeInTheDocument();
      expect(screen.getByText('Claude (Anthropic)')).toBeInTheDocument();
      expect(screen.getByText('Codex (OpenAI)')).toBeInTheDocument();
      expect(screen.getByText('default')).toBeInTheDocument();
      expect(screen.getAllByText('Tenant-wide').length).toBeGreaterThan(0);
    });
  });

  it('surfaces last_error only when status is error', async () => {
    mockClient([
      account({ id: 'acct-err', display_name: 'broken-acct', status: 'error', last_error: 'refresh failed: invalid_grant' }),
      account({ id: 'acct-ok', display_name: 'ok-acct', status: 'active' }),
    ]);
    render(AccountsView);
    await waitFor(() => {
      expect(screen.getByText('refresh failed: invalid_grant')).toBeInTheDocument();
    });
  });

  it('resolves project_id to the project name when scoped', async () => {
    mockClient(
      [account({ id: 'acct-scoped', project_id: 'proj-1' })],
      [{ id: 'proj-1', tenant_id: 'default', name: 'alpha', created_at: '', updated_at: '' }],
    );
    render(AccountsView);
    await waitFor(() => {
      expect(screen.getByText('alpha')).toBeInTheDocument();
    });
  });
});

describe('AccountsView — create dialog', () => {
  async function openCreateDialog(container: HTMLElement): Promise<void> {
    const openBtn = await waitFor(() =>
      screen.getAllByRole('button').find(
        (b) => /create|new|\+|add/i.test(b.textContent ?? '') && /account/i.test(b.textContent ?? ''),
      ),
    );
    await fireEvent.click(openBtn as HTMLElement);
    await waitFor(() => expect(container.querySelector('#acct-display-name')).not.toBeNull());
  }

  it('sends an oauth_refresh credential with refresh_token and token_endpoint', async () => {
    const { container } = render(AccountsView);
    await openCreateDialog(container);

    await fireEvent.input(container.querySelector('#acct-display-name') as HTMLInputElement, {
      target: { value: 'my-account' },
    });
    await fireEvent.input(container.querySelector('#acct-refresh-token') as HTMLInputElement, {
      target: { value: 'refresh-abc' },
    });
    await fireEvent.input(container.querySelector('#acct-token-endpoint') as HTMLInputElement, {
      target: { value: 'https://provider.example.com/oauth/token' },
    });

    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(createBtn);

    await waitFor(() => expect(mockCreateAccount).toHaveBeenCalled());
    const req = mockCreateAccount.mock.calls[0][0];
    expect(req.refresh_policy).toBe('oauth_refresh');
    expect(req.credential.refresh_token).toBe('refresh-abc');
    expect(req.credential.token_endpoint).toBe('https://provider.example.com/oauth/token');
  });

  it('sends a static_token credential with access_token and OMITS refresh_token entirely', async () => {
    const { container } = render(AccountsView);
    await openCreateDialog(container);

    await fireEvent.input(container.querySelector('#acct-display-name') as HTMLInputElement, {
      target: { value: 'my-static-account' },
    });
    await fireEvent.change(container.querySelector('#acct-refresh-policy') as HTMLSelectElement, {
      target: { value: 'static_token' },
    });
    await waitFor(() => expect(container.querySelector('#acct-access-token')).not.toBeNull());
    await fireEvent.input(container.querySelector('#acct-access-token') as HTMLInputElement, {
      target: { value: 'sk-static-123' },
    });

    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(createBtn);

    await waitFor(() => expect(mockCreateAccount).toHaveBeenCalled());
    const req = mockCreateAccount.mock.calls[0][0];
    expect(req.refresh_policy).toBe('static_token');
    expect(req.credential.access_token).toBe('sk-static-123');
    expect(req.credential.refresh_token).toBeUndefined();
    expect(req.credential.token_endpoint).toBeUndefined();
  });

  it('rejects submit client-side when a required oauth_refresh field is missing', async () => {
    const { container } = render(AccountsView);
    await openCreateDialog(container);

    await fireEvent.input(container.querySelector('#acct-display-name') as HTMLInputElement, {
      target: { value: 'incomplete-account' },
    });
    // Leave refresh_token and token_endpoint blank.
    const createBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Create',
    ) as HTMLButtonElement;
    await fireEvent.click(createBtn);

    await waitFor(() => expect(screen.getByText(/refresh token is required/i)).toBeInTheDocument());
    expect(mockCreateAccount).not.toHaveBeenCalled();
  });
});

describe('AccountsView — actions', () => {
  it('calls setDefaultAccount when Set Default is clicked', async () => {
    mockClient([account({ id: 'acct-1', display_name: 'alice-claude' })]);
    const { container } = render(AccountsView);
    const btn = await waitFor(() => {
      const el = container.querySelector('button[aria-label="Set alice-claude as default"]');
      expect(el).not.toBeNull();
      return el as HTMLButtonElement;
    });
    await fireEvent.click(btn);
    await waitFor(() => expect(mockSetDefaultAccount).toHaveBeenCalledWith('default', 'acct-1'));
  });

  it('calls revokeAccount when Revoke is clicked', async () => {
    mockClient([account({ id: 'acct-1', display_name: 'alice-claude' })]);
    const { container } = render(AccountsView);
    const btn = await waitFor(() => {
      const el = container.querySelector('button[aria-label="Revoke alice-claude"]');
      expect(el).not.toBeNull();
      return el as HTMLButtonElement;
    });
    await fireEvent.click(btn);
    await waitFor(() => expect(mockRevokeAccount).toHaveBeenCalledWith({ tenant_id: 'default', account_id: 'acct-1' }));
  });

  it('calls deleteAccount after confirming the delete dialog', async () => {
    mockClient([account({ id: 'acct-1', display_name: 'alice-claude' })]);
    const { container } = render(AccountsView);
    const trashBtn = await waitFor(() => {
      const el = container.querySelector('button[aria-label="Delete alice-claude"]');
      expect(el).not.toBeNull();
      return el as HTMLButtonElement;
    });
    await fireEvent.click(trashBtn);
    await waitFor(() => expect(container.querySelector('dialog[aria-label="Confirm delete account"]')).not.toBeNull());
    const confirmBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Delete',
    ) as HTMLButtonElement;
    await fireEvent.click(confirmBtn);
    await waitFor(() => expect(mockDeleteAccount).toHaveBeenCalledWith('default', 'acct-1'));
  });

  it('rotate dialog for a static_token account only shows the access token field', async () => {
    mockClient([account({ id: 'acct-1', display_name: 'alice-static', refresh_policy: 'static_token' })]);
    const { container } = render(AccountsView);
    const rotateBtn = await waitFor(() => {
      const el = container.querySelector('button[aria-label="Rotate credential for alice-static"]');
      expect(el).not.toBeNull();
      return el as HTMLButtonElement;
    });
    await fireEvent.click(rotateBtn);
    await waitFor(() => expect(container.querySelector('#rotate-access-token')).not.toBeNull());
    expect(container.querySelector('#rotate-refresh-token')).toBeNull();
  });

  it('rotate dialog sends a replacement credential for an oauth_refresh account', async () => {
    mockClient([account({ id: 'acct-1', display_name: 'alice-claude', refresh_policy: 'oauth_refresh' })]);
    const { container } = render(AccountsView);
    const rotateBtn = await waitFor(() => {
      const el = container.querySelector('button[aria-label="Rotate credential for alice-claude"]');
      expect(el).not.toBeNull();
      return el as HTMLButtonElement;
    });
    await fireEvent.click(rotateBtn);
    await waitFor(() => expect(container.querySelector('#rotate-refresh-token')).not.toBeNull());

    await fireEvent.input(container.querySelector('#rotate-refresh-token') as HTMLInputElement, {
      target: { value: 'new-refresh-token' },
    });
    await fireEvent.input(container.querySelector('#rotate-token-endpoint') as HTMLInputElement, {
      target: { value: 'https://provider.example.com/oauth/token' },
    });

    const rotateSubmitBtn = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === 'Rotate',
    ) as HTMLButtonElement;
    await fireEvent.click(rotateSubmitBtn);

    await waitFor(() => expect(mockRotateAccountCredential).toHaveBeenCalled());
    const req = mockRotateAccountCredential.mock.calls[0][0];
    expect(req.account_id).toBe('acct-1');
    expect(req.credential.refresh_token).toBe('new-refresh-token');
  });
});
