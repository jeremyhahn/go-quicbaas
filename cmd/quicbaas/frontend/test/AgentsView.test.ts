/**
 * AgentsView.test.ts
 *
 * The Agents page reaches every Agent catalog operation the SDK builds:
 * list, create, edit, delete and used-by for Agents and for agent pools,
 * all scoped to the active tenant. Edit merges the form onto the stored
 * record, so fields the form does not show are kept.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/svelte';
import type { Agent, AgentPool } from '@quicbaas/sdk';
import { reactiveTenantStore, gate, settle } from './tenantSwitch.svelte.ts';

vi.mock('../src/stores/sdkClient.ts', () => ({ getSDKClient: vi.fn() }));
vi.mock('../src/stores/orchestrator.svelte.ts', async () => {
  const { reactiveTenantStore } = await import('./tenantSwitch.svelte.ts');
  return { orchestratorStore: reactiveTenantStore };
});
vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

import AgentsView from '../src/views/orchestrator/AgentsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

const api = {
  listAgents: vi.fn(),
  getAgent: vi.fn(),
  createAgent: vi.fn(),
  updateAgent: vi.fn(),
  deleteAgent: vi.fn(),
  agentUsedBy: vi.fn(),
  listPools: vi.fn(),
  getPool: vi.fn(),
  createPool: vi.fn(),
  updatePool: vi.fn(),
  deletePool: vi.fn(),
  poolUsedBy: vi.fn(),
};

function agent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'coder',
    name: 'Coder',
    roles: ['coding'],
    backend: 'claude',
    model: 'opus',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function pool(overrides: Partial<AgentPool> = {}): AgentPool {
  return {
    id: 'main-pool',
    name: 'Main pool',
    agents: ['coder', 'reviewer'],
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function mockClient(agents: Agent[] = [], pools: AgentPool[] = []): void {
  api.listAgents.mockResolvedValue(agents);
  api.listPools.mockResolvedValue(pools);
  vi.mocked(getSDKClient).mockResolvedValue({ agents: api } as never);
}

function input(id: string, value: string): Promise<boolean> {
  const el = document.getElementById(id) as HTMLInputElement;
  return fireEvent.input(el, { target: { value } });
}

/** labelled finds the element with an aria-label; a modal <dialog> hides its content from role queries. */
function labelled(label: string): HTMLElement {
  const found = document.querySelector(`[aria-label="${label}"]`);
  if (!found) throw new Error(`nothing labelled ${label}`);
  return found as HTMLElement;
}

function button(text: string): HTMLButtonElement {
  const found = Array.from(document.querySelectorAll('button')).find((b) => b.textContent?.trim() === text);
  if (!found) throw new Error(`no button ${text}`);
  return found as HTMLButtonElement;
}

beforeEach(() => {
  vi.clearAllMocks();
  reactiveTenantStore.activeTenantID = 'acme';
  api.createAgent.mockImplementation(async (a: Agent) => a);
  api.updateAgent.mockImplementation(async (_id: string, a: Agent) => a);
  api.deleteAgent.mockResolvedValue(undefined);
  api.createPool.mockImplementation(async (p: AgentPool) => p);
  api.updatePool.mockImplementation(async (_id: string, p: AgentPool) => p);
  api.deletePool.mockResolvedValue(undefined);
  mockClient([agent(), agent({ id: 'reviewer', name: 'Reviewer', roles: ['review'] })], [pool()]);
});

describe('AgentsView — load', () => {
  it('lists agents and pools for the active tenant and renders both tables', async () => {
    render(AgentsView);
    const agentsTable = await screen.findByTestId('agents-table');
    await waitFor(() => expect(within(agentsTable).getByText('Reviewer')).toBeInTheDocument());
    expect(api.listAgents).toHaveBeenCalledWith('acme');
    expect(api.listPools).toHaveBeenCalledWith('acme');
    expect(within(agentsTable).getAllByText('opus')).toHaveLength(2);
    expect(within(agentsTable).getByText('review')).toBeInTheDocument();
    const poolsTable = screen.getByTestId('pools-table');
    expect(within(poolsTable).getByText('Main pool')).toBeInTheDocument();
    expect(within(poolsTable).getByText('coder → reviewer')).toBeInTheDocument();
  });

  it('reloads on a tenant change and drops the old tenant\'s late answer', async () => {
    const acme = gate<Agent[]>();
    api.listAgents.mockImplementation((tenant: string) =>
      tenant === 'acme' ? acme.promise : Promise.resolve([agent({ id: 'other-agent', name: 'Other agent' })]),
    );
    render(AgentsView);
    await settle();
    reactiveTenantStore.activeTenantID = 'other';
    await settle();
    expect(api.listAgents.mock.calls.map((c) => c[0])).toEqual(['acme', 'other']);
    expect(api.listPools.mock.calls.map((c) => c[0])).toEqual(['acme', 'other']);
    await waitFor(() => expect(screen.getByText('Other agent')).toBeInTheDocument());

    acme.open([agent({ id: 'acme-agent', name: 'Acme agent' })]);
    await settle();
    expect(screen.queryByText('Acme agent')).toBeNull();
    expect(screen.getByText('Other agent')).toBeInTheDocument();
  });

  it('shows the not-available state and calls nothing when the connection has no agents', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({ agents: null } as never);
    render(AgentsView);
    expect(await screen.findByText('Agents are not available on this connection.')).toBeInTheDocument();
    expect(api.listAgents).not.toHaveBeenCalled();
    expect(api.listPools).not.toHaveBeenCalled();
  });

  it('shows a thrown listAgents error inline', async () => {
    api.listAgents.mockRejectedValue(new Error('not supported on this transport'));
    render(AgentsView);
    expect(await screen.findByText(/Failed to load agents: not supported on this transport/)).toBeInTheDocument();
  });
});

describe('AgentsView — agents', () => {
  async function openNewAgent(): Promise<void> {
    render(AgentsView);
    await screen.findByText('Reviewer');
    await fireEvent.click(screen.getByRole('button', { name: 'New agent' }));
    await screen.findByTestId('agent-edit-dialog');
  }

  it('creates an agent with the required fields plus description and reasoning effort', async () => {
    await openNewAgent();
    await input('agent-id', 'triager');
    await input('agent-name', 'Triager');
    await fireEvent.click(screen.getByLabelText('triage'));
    await fireEvent.click(screen.getByLabelText('general'));
    await input('agent-backend', 'codex');
    await input('agent-model', 'gpt-5');
    await input('agent-description', 'Sorts new issues');
    await fireEvent.change(document.getElementById('agent-reasoning-effort') as HTMLSelectElement, {
      target: { value: 'high' },
    });
    await fireEvent.click(button('Create'));

    await waitFor(() => expect(api.createAgent).toHaveBeenCalledTimes(1));
    expect(api.createAgent.mock.calls[0]).toEqual([
      {
        id: 'triager',
        name: 'Triager',
        roles: ['triage', 'general'],
        backend: 'codex',
        model: 'gpt-5',
        description: 'Sorts new issues',
        reasoning_effort: 'high',
      },
      'acme',
    ]);
    await waitFor(() => expect(screen.queryByTestId('agent-edit-dialog')).toBeNull());
    expect(api.listAgents).toHaveBeenCalledTimes(2);
  });

  it('refuses a missing required field with no call', async () => {
    await openNewAgent();
    await input('agent-id', 'triager');
    await input('agent-name', 'Triager');
    await fireEvent.click(screen.getByLabelText('triage'));
    await input('agent-backend', 'codex');
    await fireEvent.click(button('Create'));
    expect((await screen.findByTestId('agent-edit-error')).textContent).toMatch(/model is required/i);
    expect(api.createAgent).not.toHaveBeenCalled();
  });

  it('refuses an agent with no role with no call', async () => {
    await openNewAgent();
    await input('agent-id', 'triager');
    await input('agent-name', 'Triager');
    await input('agent-backend', 'codex');
    await input('agent-model', 'gpt-5');
    await fireEvent.click(button('Create'));
    expect((await screen.findByTestId('agent-edit-error')).textContent).toMatch(/at least one role/i);
    expect(api.createAgent).not.toHaveBeenCalled();
  });

  it('edits through getAgent and keeps the fields the form does not show', async () => {
    const spend = { windows: [{ window: 'daily', limit_micros: 5_000_000 }] } as unknown as Agent['spend'];
    const stored = agent({ spend, image: 'worker:1', description: 'old' });
    api.getAgent.mockResolvedValue(stored);
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit agent coder' }));
    await screen.findByTestId('agent-edit-dialog');
    expect(api.getAgent).toHaveBeenCalledWith('coder', 'acme');
    expect((document.getElementById('agent-id') as HTMLInputElement).readOnly).toBe(true);
    expect((document.getElementById('agent-name') as HTMLInputElement).value).toBe('Coder');
    await input('agent-model', 'sonnet');
    await fireEvent.click(button('Save'));

    await waitFor(() => expect(api.updateAgent).toHaveBeenCalledTimes(1));
    const [id, sent, tenant] = api.updateAgent.mock.calls[0];
    expect(id).toBe('coder');
    expect(tenant).toBe('acme');
    expect(sent).toMatchObject({ id: 'coder', model: 'sonnet', description: 'old', image: 'worker:1', spend });
    await waitFor(() => expect(api.listAgents).toHaveBeenCalledTimes(2));
  });

  it('shows used-by as kind, id and slot', async () => {
    api.agentUsedBy.mockResolvedValue([{ kind: 'pool', id: 'main-pool', slot: 'member' }]);
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Used by agent coder' }));
    const dialog = await screen.findByTestId('usage-dialog');
    expect(api.agentUsedBy).toHaveBeenCalledWith('coder', 'acme');
    expect(within(dialog).getByText('pool')).toBeInTheDocument();
    expect(within(dialog).getByText('main-pool')).toBeInTheDocument();
    expect(within(dialog).getByText('member')).toBeInTheDocument();
  });

  it('shows "Not used" for an empty used-by answer', async () => {
    api.agentUsedBy.mockResolvedValue([]);
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Used by agent coder' }));
    const dialog = await screen.findByTestId('usage-dialog');
    expect(within(dialog).getByText('Not used')).toBeInTheDocument();
  });

  it('deletes after confirmation and reloads', async () => {
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Delete agent coder' }));
    await waitFor(() => labelled('Confirm delete agent'));
    expect(api.deleteAgent).not.toHaveBeenCalled();
    await fireEvent.click(button('Delete'));
    await waitFor(() => expect(api.deleteAgent).toHaveBeenCalledWith('coder', 'acme'));
    await waitFor(() => expect(api.listAgents).toHaveBeenCalledTimes(2));
  });

  it('shows a refused delete inline and does not reload', async () => {
    api.deleteAgent.mockRejectedValue(new Error('agent coder is in use by pool main-pool'));
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Delete agent coder' }));
    await fireEvent.click(button('Delete'));
    expect((await screen.findByTestId('delete-error')).textContent).toContain('in use by pool main-pool');
    expect(labelled('Confirm delete agent')).toBeInTheDocument();
    expect(api.listAgents).toHaveBeenCalledTimes(1);
  });
});

describe('AgentsView — pools', () => {
  async function openNewPool(): Promise<void> {
    render(AgentsView);
    await screen.findByText('Reviewer');
    await fireEvent.click(screen.getByRole('button', { name: 'New pool' }));
    await screen.findByTestId('pool-edit-dialog');
  }

  async function addMember(id: string): Promise<void> {
    await fireEvent.change(document.getElementById('pool-add-agent') as HTMLSelectElement, { target: { value: id } });
    await fireEvent.click(button('Add member'));
  }

  it('creates a pool with the members in the chosen order', async () => {
    await openNewPool();
    await input('pool-id', 'p2');
    await input('pool-name', 'Pool two');
    await input('pool-description', 'Failover');
    await addMember('reviewer');
    await addMember('coder');
    await fireEvent.click(button('Create'));
    await waitFor(() => expect(api.createPool).toHaveBeenCalledTimes(1));
    expect(api.createPool.mock.calls[0]).toEqual([
      { id: 'p2', name: 'Pool two', description: 'Failover', agents: ['reviewer', 'coder'] },
      'acme',
    ]);
    await waitFor(() => expect(api.listPools).toHaveBeenCalledTimes(2));
  });

  it('move up and move down change the order sent', async () => {
    await openNewPool();
    await input('pool-id', 'p2');
    await input('pool-name', 'Pool two');
    await addMember('coder');
    await addMember('reviewer');
    await fireEvent.click(labelled('Move reviewer up'));
    await fireEvent.click(button('Create'));
    await waitFor(() => expect(api.createPool).toHaveBeenCalledTimes(1));
    expect(api.createPool.mock.calls[0][0].agents).toEqual(['reviewer', 'coder']);
  });

  it('move down and remove change the members sent', async () => {
    await openNewPool();
    await input('pool-id', 'p2');
    await input('pool-name', 'Pool two');
    await addMember('coder');
    await addMember('reviewer');
    await fireEvent.click(labelled('Move coder down'));
    await fireEvent.click(labelled('Remove reviewer'));
    await fireEvent.click(button('Create'));
    await waitFor(() => expect(api.createPool).toHaveBeenCalledTimes(1));
    expect(api.createPool.mock.calls[0][0].agents).toEqual(['coder']);
  });

  it('refuses a pool with no member with no call', async () => {
    await openNewPool();
    await input('pool-id', 'p2');
    await input('pool-name', 'Pool two');
    await fireEvent.click(button('Create'));
    expect((await screen.findByTestId('pool-edit-error')).textContent).toMatch(/at least one member/i);
    expect(api.createPool).not.toHaveBeenCalled();
  });

  it('edits through getPool and keeps the fields the form does not show', async () => {
    api.getPool.mockResolvedValue(pool({ created_at: '2025-12-01T00:00:00Z' }));
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Edit pool main-pool' }));
    await screen.findByTestId('pool-edit-dialog');
    expect(api.getPool).toHaveBeenCalledWith('main-pool', 'acme');
    expect((document.getElementById('pool-id') as HTMLInputElement).readOnly).toBe(true);
    await fireEvent.click(labelled('Move reviewer up'));
    await fireEvent.click(button('Save'));
    await waitFor(() => expect(api.updatePool).toHaveBeenCalledTimes(1));
    const [id, sent, tenant] = api.updatePool.mock.calls[0];
    expect(id).toBe('main-pool');
    expect(tenant).toBe('acme');
    expect(sent).toMatchObject({ id: 'main-pool', name: 'Main pool', agents: ['reviewer', 'coder'], created_at: '2025-12-01T00:00:00Z' });
  });

  it('shows pool used-by', async () => {
    api.poolUsedBy.mockResolvedValue([{ kind: 'project', id: 'proj-1', slot: 'worker' }]);
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Used by pool main-pool' }));
    const dialog = await screen.findByTestId('usage-dialog');
    expect(api.poolUsedBy).toHaveBeenCalledWith('main-pool', 'acme');
    expect(within(dialog).getByText('proj-1')).toBeInTheDocument();
    expect(within(dialog).getByText('worker')).toBeInTheDocument();
  });

  it('deletes a pool after confirmation and reloads', async () => {
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Delete pool main-pool' }));
    await waitFor(() => labelled('Confirm delete pool'));
    await fireEvent.click(button('Delete'));
    await waitFor(() => expect(api.deletePool).toHaveBeenCalledWith('main-pool', 'acme'));
    await waitFor(() => expect(api.listPools).toHaveBeenCalledTimes(2));
  });

  it('shows a refused pool delete inline and does not reload', async () => {
    api.deletePool.mockRejectedValue(new Error('pool main-pool is in use by project proj-1'));
    render(AgentsView);
    await fireEvent.click(await screen.findByRole('button', { name: 'Delete pool main-pool' }));
    await fireEvent.click(button('Delete'));
    expect((await screen.findByTestId('delete-error')).textContent).toContain('in use by project proj-1');
    expect(api.listPools).toHaveBeenCalledTimes(1);
  });
});
