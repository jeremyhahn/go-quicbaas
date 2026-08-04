/**
 * AgentDetailView.test.ts
 *
 * Verifies:
 * 1. Renders agent identity information (backend, host, registered_at, last_seen).
 * 2. Capability chips rendered, GPU chip highlighted when present.
 * 3. Remove button opens confirm dialog.
 * 4. Shows "not found" state when agent ID does not match any agent.
 * 5. Recent sessions and jobs sections render.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

// ---------------------------------------------------------------------------
// Mocks — all vi.hoisted factories must be self-contained (no outer-scope refs).
// ---------------------------------------------------------------------------

const { agentStoreMock, routerStoreMock } = vi.hoisted(() => {
  const agentData = {
    id: 'agent-xyz-789',
    tenant_id: 'default',
    name: 'alpha-agent',
    host: 'gpu-node-01.example.com',
    backend: 'claude',
    model: 'claude-3-5-sonnet',
    agent_type: 'coding',
    status: 'running',
    capabilities: ['code', 'gpu', 'bash'],
    container_id: 'abc123def456',
    work_dir: '/workspace',
    last_seen: Math.floor(Date.now() / 1000) - 30,
    registered_at: 1700000000,
    version: 1,
    metadata: {},
  };
  const agentNoGpu = {
    ...agentData,
    id: 'agent-no-gpu',
    name: 'beta-agent',
    capabilities: ['code', 'bash'],
  };
  return {
    agentStoreMock: {
      agents: [agentData, agentNoGpu],
      agentsLoading: false,
      onlineAgents: [agentData, agentNoGpu],
    },
    // routerStore exposes flat name/params/hash getters (not a nested `route`).
    routerStoreMock: {
      name: 'agent-detail',
      params: { id: 'agent-xyz-789' },
      hash: '#/agents/agent-xyz-789',
    },
  };
});

vi.mock('../src/stores/agentStore.svelte.ts', () => ({
  agentStore: agentStoreMock,
  refreshAgents: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    activeTenantID: 'default',
    jobs: [],
    sessions: [],
    agents: [],
    events: [],
    health: null,
  },
}));

vi.mock('../src/stores/sessionStore.svelte.ts', () => ({
  sessionStore: {
    sessions: [],
    events: [],
  },
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routerStore: routerStoreMock,
  routeFleet: vi.fn(),
  routeSessionDetail: vi.fn(),
  routeAgentDetail: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      removeAgent: vi.fn().mockResolvedValue(undefined),
    },
    connection: { getTransport: vi.fn().mockReturnValue('rest') },
  }),
}));

import AgentDetailView from '../src/views/orchestrator/AgentDetailView.svelte';
import { routeFleet } from '../src/stores/router.svelte.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('AgentDetailView', () => {
  beforeEach(() => {
    // Reset to a known-good agent ID (flat router API).
    routerStoreMock.name = 'agent-detail';
    routerStoreMock.params = { id: 'agent-xyz-789' };
    routerStoreMock.hash = '#/agents/agent-xyz-789';
    vi.mocked(routeFleet).mockClear();
  });

  it('renders agent name in the header', () => {
    render(AgentDetailView);
    expect(screen.getByText('alpha-agent')).toBeInTheDocument();
  });

  it('renders the agent ID in the header', () => {
    render(AgentDetailView);
    expect(screen.getByText('agent-xyz-789')).toBeInTheDocument();
  });

  it('renders the backend badge in the identity card', () => {
    render(AgentDetailView);
    const badges = screen.getAllByText('claude');
    expect(badges.length).toBeGreaterThan(0);
  });

  it('renders the host in the identity card', () => {
    render(AgentDetailView);
    expect(screen.getByText('gpu-node-01.example.com')).toBeInTheDocument();
  });

  it('renders all capability chips including gpu', () => {
    render(AgentDetailView);
    expect(screen.getByRole('listitem', { name: /capability: code/i })).toBeInTheDocument();
    expect(screen.getByRole('listitem', { name: /capability: gpu/i })).toBeInTheDocument();
    expect(screen.getByRole('listitem', { name: /capability: bash/i })).toBeInTheDocument();
  });

  it('gpu capability chip has warning badge class', () => {
    const { container } = render(AgentDetailView);
    const chips = container.querySelectorAll('[role="listitem"]');
    const gpuChip = Array.from(chips).find((el) => el.textContent?.includes('gpu'));
    expect(gpuChip).toBeTruthy();
    expect(gpuChip!.className).toContain('badge-warning');
  });

  it('non-gpu capability chip does not have warning class', () => {
    const { container } = render(AgentDetailView);
    const chips = container.querySelectorAll('[role="listitem"]');
    const codeChip = Array.from(chips).find(
      (el) => el.getAttribute('aria-label') === 'Capability: code',
    );
    expect(codeChip).toBeTruthy();
    expect(codeChip!.className).not.toContain('badge-warning');
  });

  it('Remove button is visible', () => {
    render(AgentDetailView);
    expect(screen.getByRole('button', { name: /remove agent/i })).toBeInTheDocument();
  });

  it('clicking Remove button opens the confirm dialog', async () => {
    render(AgentDetailView);
    const removeBtn = screen.getByRole('button', { name: /remove agent/i });
    await fireEvent.click(removeBtn);
    expect(screen.getByText('Remove Agent?')).toBeInTheDocument();
  });

  it('back button calls routeFleet', async () => {
    render(AgentDetailView);
    const backBtn = screen.getByRole('button', { name: /back to agent fleet/i });
    await fireEvent.click(backBtn);
    expect(vi.mocked(routeFleet)).toHaveBeenCalledTimes(1);
  });

  it('shows not-found state when agent ID does not match', () => {
    routerStoreMock.name = 'agent-detail';
    routerStoreMock.params = { id: 'nonexistent-agent' };
    routerStoreMock.hash = '#/agents/nonexistent-agent';
    render(AgentDetailView);
    expect(screen.getByText(/agent not found/i)).toBeInTheDocument();
  });

  it('shows Recent Sessions section', () => {
    render(AgentDetailView);
    expect(screen.getByText(/Recent Sessions/i)).toBeInTheDocument();
  });

  it('shows Recent Jobs section', () => {
    render(AgentDetailView);
    expect(screen.getByText(/Recent Jobs/i)).toBeInTheDocument();
  });

  it('shows Metrics unavailable notice when no telemetry in metadata', () => {
    render(AgentDetailView);
    expect(screen.getByText(/Metrics unavailable/i)).toBeInTheDocument();
  });

  it('raw JSON drawer is initially closed', () => {
    const { container } = render(AgentDetailView);
    expect(container.querySelector('#agent-raw-json-panel')).toBeNull();
  });

  it('raw JSON drawer opens on click and contains agent data', async () => {
    const { container } = render(AgentDetailView);
    const drawerBtn = screen.getByRole('button', { name: /raw agent state/i });
    await fireEvent.click(drawerBtn);
    const panel = container.querySelector('#agent-raw-json-panel');
    expect(panel).not.toBeNull();
    expect(panel!.textContent).toContain('agent-xyz-789');
  });
});
