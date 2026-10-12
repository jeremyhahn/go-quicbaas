/**
 * AccountsView.tenant.test.ts
 *
 * The view must load for the tenant its effect ran for: a switch while the
 * load is in flight re-fetches for the new tenant, and the old tenant's late
 * answer neither renders nor mixes tenants inside one load.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import type { Account } from '@quicbaas/sdk';
import { reactiveTenantStore, gate, settle } from './tenantSwitch.svelte.ts';

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: vi.fn() }));
vi.mock('../src/stores/orchestrator.svelte.ts', async () => {
  const { reactiveTenantStore } = await import('./tenantSwitch.svelte.ts');
  return { orchestratorStore: reactiveTenantStore };
});
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

import AccountsView from '../src/views/orchestrator/AccountsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const account = (name: string): Account =>
  ({
    id: `id-${name}`,
    provider: 'claude',
    display_name: name,
    default: false,
    status: 'active',
    refresh_policy: 'oauth_refresh',
  }) as Account;

describe('AccountsView tenant switch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    reactiveTenantStore.activeTenantID = 'acme';
  });

  it('re-fetches for the new tenant and drops the old tenant\'s late answer', async () => {
    const acmeAccounts = gate<{ accounts: Account[] }>();
    const accountTenants: string[] = [];
    const projectTenants: string[] = [];
    vi.mocked(getSDKClient).mockResolvedValue({
      accounts: {
        listAccounts: vi.fn((req: { tenant_id: string }) => {
          accountTenants.push(req.tenant_id);
          return req.tenant_id === 'acme'
            ? acmeAccounts.promise
            : Promise.resolve({ accounts: [account('other-acct')] });
        }),
      },
      projects: {
        listProjects: vi.fn(async (tenant: string) => {
          projectTenants.push(tenant);
          return { projects: [] };
        }),
      },
    } as never);

    render(AccountsView);
    await settle();
    expect(accountTenants).toEqual(['acme']);

    reactiveTenantStore.activeTenantID = 'other';
    await settle();
    expect(accountTenants).toEqual(['acme', 'other']);
    expect(projectTenants).toEqual(['acme', 'other']);
    expect(screen.queryByText('other-acct')).not.toBeNull();

    acmeAccounts.open({ accounts: [account('acme-acct')] });
    await settle();

    expect(screen.queryByText('acme-acct')).toBeNull();
    expect(screen.queryByText('other-acct')).not.toBeNull();
  });

  it('renders the answer for the tenant it loaded for', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({
      accounts: { listAccounts: vi.fn().mockResolvedValue({ accounts: [account('acme-acct')] }) },
      projects: { listProjects: vi.fn().mockResolvedValue({ projects: [] }) },
    } as never);

    render(AccountsView);
    await settle();

    expect(screen.queryByText('acme-acct')).not.toBeNull();
  });
});
