/**
 * ProjectsView.tenant.test.ts
 *
 * The view must load for the tenant its effect ran for: a switch while the
 * load is in flight re-fetches for the new tenant, and the old tenant's late
 * answer neither renders nor mixes tenants inside one load.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import type { Project } from '@quicbaas/sdk';
import { reactiveTenantStore, gate, settle } from './tenantSwitch.svelte.ts';

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: vi.fn() }));
vi.mock('../src/stores/orchestrator.svelte.ts', async () => {
  const { reactiveTenantStore } = await import('./tenantSwitch.svelte.ts');
  return { orchestratorStore: reactiveTenantStore };
});
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

import ProjectsView from '../src/views/orchestrator/ProjectsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const project = (name: string): Project => ({ id: `id-${name}`, name }) as Project;

describe('ProjectsView tenant switch', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    reactiveTenantStore.activeTenantID = 'acme';
  });

  it('re-fetches for the new tenant and drops the old tenant\'s late answer', async () => {
    const acmeProjects = gate<{ projects: Project[] }>();
    const projectTenants: string[] = [];
    const nodeTenants: string[] = [];
    vi.mocked(getSDKClient).mockResolvedValue({
      projects: {
        listProjects: vi.fn((tenant: string) => {
          projectTenants.push(tenant);
          return tenant === 'acme' ? acmeProjects.promise : Promise.resolve({ projects: [project('other-proj')] });
        }),
      },
      orchestrator: {
        listFleetNodes: vi.fn(async (req: { tenant_id: string }) => {
          nodeTenants.push(req.tenant_id);
          return { nodes: [] };
        }),
      },
    } as never);

    render(ProjectsView);
    await settle();
    expect(projectTenants).toEqual(['acme']);

    reactiveTenantStore.activeTenantID = 'other';
    await settle();
    expect(projectTenants).toEqual(['acme', 'other']);
    expect(nodeTenants).toEqual(['acme', 'other']);
    expect(screen.queryByText('other-proj')).not.toBeNull();

    acmeProjects.open({ projects: [project('acme-proj')] });
    await settle();

    expect(screen.queryByText('acme-proj')).toBeNull();
    expect(screen.queryByText('other-proj')).not.toBeNull();
  });

  it('keeps showing the tenant it loaded for when the answer is current', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({
      projects: { listProjects: vi.fn().mockResolvedValue({ projects: [project('acme-proj')] }) },
      orchestrator: { listFleetNodes: vi.fn().mockResolvedValue({ nodes: [] }) },
    } as never);

    render(ProjectsView);
    await settle();

    expect(screen.queryByText('acme-proj')).not.toBeNull();
  });
});
