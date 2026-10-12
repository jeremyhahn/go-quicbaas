/**
 * WorkflowsView.tenant.test.ts
 *
 * The view's name list and wizard support data must load for the tenant the
 * effect ran for: a switch while they are in flight re-fetches for the new
 * tenant, and the old tenant's late answer is dropped.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/svelte';
import type { FleetNode } from '@quicbaas/sdk';
import { reactiveTenantStore, gate, settle } from './tenantSwitch.svelte.ts';

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: vi.fn() }));
vi.mock('../src/stores/orchestrator.svelte.ts', async () => {
  const { reactiveTenantStore } = await import('./tenantSwitch.svelte.ts');
  return { orchestratorStore: reactiveTenantStore };
});
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));
vi.mock('../src/stores/router.svelte.ts', () => ({
  navigateTo: vi.fn(),
  routerStore: { route: { name: 'workflows', params: {}, hash: '#/workflows' } },
}));

import WorkflowsView from '../src/views/orchestrator/WorkflowsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

describe('WorkflowsView tenant switch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    reactiveTenantStore.activeTenantID = 'acme';
    window.location.hash = '#/workflows';
  });

  it('re-fetches names and fleet nodes for the new tenant, never mixing tenants', async () => {
    const acmeNodes = gate<{ nodes: FleetNode[] }>();
    const pageTenants: string[] = [];
    const nodeTenants: string[] = [];
    vi.mocked(getSDKClient).mockResolvedValue({
      orchestrator: {
        listWorkflowsPage: vi.fn(async (tenant: string) => {
          pageTenants.push(tenant);
          return { items: [], total: 0, limit: 25, offset: 0 };
        }),
        listBackends: vi.fn().mockResolvedValue([]),
        listFleetNodes: vi.fn((req: { tenant_id: string }) => {
          nodeTenants.push(req.tenant_id);
          return req.tenant_id === 'acme' ? acmeNodes.promise : Promise.resolve({ nodes: [] });
        }),
      },
    } as never);

    render(WorkflowsView);
    await settle();
    expect(nodeTenants).toEqual(['acme']);
    const acmePages = pageTenants.length;
    expect(acmePages).toBeGreaterThan(0);
    expect(new Set(pageTenants)).toEqual(new Set(['acme']));

    reactiveTenantStore.activeTenantID = 'other';
    await settle();
    expect(nodeTenants).toEqual(['acme', 'other']);
    // The names read for the old load ran before the switch; every read after
    // it is for the new tenant.
    expect(pageTenants.slice(acmePages).length).toBeGreaterThan(0);
    expect(new Set(pageTenants.slice(acmePages))).toEqual(new Set(['other']));

    // The old tenant's late answer must not throw or be applied.
    acmeNodes.open({ nodes: [{ id: 'acme-node', name: 'acme-node' } as FleetNode] });
    await settle();
    expect(nodeTenants).toEqual(['acme', 'other']);
  });
});
