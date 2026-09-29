/**
 * FleetNodeDetailView.test.ts
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

const { fleetStoreMock, routerStoreMock } = vi.hoisted(() => {
  const agentData = {
    id: 'agent-xyz-789',
    tenant_id: 'default',
    name: 'alpha-agent',
    host: 'gpu-node-01.example.com',
    backend: 'claude',
    model: 'claude-3-5-sonnet',
    node_type: 'coding',
    status: 'running',
    capabilities: ['code', 'gpu', 'bash'],
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
    fleetStoreMock: {
      nodes: [agentData, agentNoGpu],
      nodesLoading: false,
      onlineNodes: [agentData, agentNoGpu],
    },
    // routerStore exposes flat name/params/hash getters (not a nested `route`).
    routerStoreMock: {
      name: 'fleet-node',
      params: { id: 'agent-xyz-789' },
      hash: '#/fleet/agent-xyz-789',
    },
  };
});

vi.mock('../src/stores/fleetStore.svelte.ts', () => ({
  fleetStore: fleetStoreMock,
  refreshFleet: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: {
    activeTenantID: 'default',
    jobs: [],
    sessions: [],
    fleetNodes: [],
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
  routeFleetNode: vi.fn(),
  navigateTo: vi.fn(),
  // The breadcrumb goes back to the fleet AS THE OPERATOR LEFT IT — narrowed
  // and on the page they were on — so it navigates to a remembered hash
  // rather than to the bare route.
  listReturnHash: vi.fn((path: string) => `#${path}?fleet_status=offline`),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      removeFleetNode: vi.fn().mockResolvedValue(undefined),
    },
    connection: { getTransport: vi.fn().mockReturnValue('rest') },
  }),
}));

import FleetNodeDetailView from '../src/views/orchestrator/FleetNodeDetailView.svelte';
import { navigateTo } from '../src/stores/router.svelte.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('FleetNodeDetailView', () => {
  beforeEach(() => {
    // Reset to a known-good agent ID (flat router API).
    routerStoreMock.name = 'fleet-node';
    routerStoreMock.params = { id: 'agent-xyz-789' };
    routerStoreMock.hash = '#/fleet/agent-xyz-789';
    vi.mocked(navigateTo).mockClear();
  });

  it('renders agent name in the header', () => {
    render(FleetNodeDetailView);
    expect(screen.getByText('alpha-agent')).toBeInTheDocument();
  });

  it('renders the agent ID in the header', () => {
    render(FleetNodeDetailView);
    expect(screen.getByText('agent-xyz-789')).toBeInTheDocument();
  });

  it('renders the backend badge in the identity card', () => {
    render(FleetNodeDetailView);
    const badges = screen.getAllByText('claude');
    expect(badges.length).toBeGreaterThan(0);
  });

  it('renders the host in the identity card', () => {
    render(FleetNodeDetailView);
    expect(screen.getByText('gpu-node-01.example.com')).toBeInTheDocument();
  });

  // The registry records no container for an agent (go-quicbaas#862), so the
  // identity card has no Container row — not even for a stale payload that
  // still carries the old field.
  it('renders no Container row', () => {
    const agent = fleetStoreMock.nodes[0] as Record<string, unknown>;
    agent.container_id = 'abc123def456';
    try {
      render(FleetNodeDetailView);
      expect(screen.queryByText('Container')).not.toBeInTheDocument();
      expect(screen.queryByText('abc123def456')).not.toBeInTheDocument();
    } finally {
      delete agent.container_id;
    }
  });

  it('renders all capability chips including gpu', () => {
    render(FleetNodeDetailView);
    expect(screen.getByRole('listitem', { name: /capability: code/i })).toBeInTheDocument();
    expect(screen.getByRole('listitem', { name: /capability: gpu/i })).toBeInTheDocument();
    expect(screen.getByRole('listitem', { name: /capability: bash/i })).toBeInTheDocument();
  });

  it('gpu capability chip has warning badge class', () => {
    const { container } = render(FleetNodeDetailView);
    const chips = container.querySelectorAll('[role="listitem"]');
    const gpuChip = Array.from(chips).find((el) => el.textContent?.includes('gpu'));
    expect(gpuChip).toBeTruthy();
    expect(gpuChip!.className).toContain('badge-warning');
  });

  it('non-gpu capability chip does not have warning class', () => {
    const { container } = render(FleetNodeDetailView);
    const chips = container.querySelectorAll('[role="listitem"]');
    const codeChip = Array.from(chips).find(
      (el) => el.getAttribute('aria-label') === 'Capability: code',
    );
    expect(codeChip).toBeTruthy();
    expect(codeChip!.className).not.toContain('badge-warning');
  });

  it('Remove button is visible', () => {
    render(FleetNodeDetailView);
    expect(screen.getByRole('button', { name: /remove fleet node/i })).toBeInTheDocument();
  });

  it('clicking Remove button opens the confirm dialog', async () => {
    render(FleetNodeDetailView);
    const removeBtn = screen.getByRole('button', { name: /remove fleet node/i });
    await fireEvent.click(removeBtn);
    expect(screen.getByText('Remove fleet node?')).toBeInTheDocument();
  });

  it('back button returns to the fleet as the operator left it', async () => {
    render(FleetNodeDetailView);
    const backBtn = screen.getByRole('button', { name: /back to fleet/i });
    await fireEvent.click(backBtn);
    // The narrowing survives: sending them to a bare #/fleet would throw away
    // the filter they used to find this agent.
    expect(vi.mocked(navigateTo)).toHaveBeenCalledWith('#/fleet?fleet_status=offline');
  });

  it('shows not-found state when agent ID does not match', () => {
    routerStoreMock.name = 'fleet-node';
    routerStoreMock.params = { id: 'nonexistent-agent' };
    routerStoreMock.hash = '#/fleet/nonexistent-agent';
    render(FleetNodeDetailView);
    expect(screen.getByText(/fleet node not found/i)).toBeInTheDocument();
  });

  it('shows the sessions that ran on THIS agent', () => {
    render(FleetNodeDetailView);
    expect(screen.getByText(/Sessions on this node/i)).toBeInTheDocument();
    // And offers the same question as a full page of the Sessions list.
    expect(
      screen.getByRole('link', { name: 'Open in Sessions' }).getAttribute('href'),
    ).toBe('#/sessions?sessions_node=agent-xyz-789');
  });

  it('shows the jobs that ran on THIS agent', () => {
    render(FleetNodeDetailView);
    expect(screen.getByText(/Jobs on this node/i)).toBeInTheDocument();
    expect(
      screen.getByRole('link', { name: 'Open in Jobs' }).getAttribute('href'),
    ).toBe('#/jobs?jobs_node=agent-xyz-789');
  });

  it('shows Metrics unavailable notice when no telemetry in metadata', () => {
    render(FleetNodeDetailView);
    expect(screen.getByText(/Metrics unavailable/i)).toBeInTheDocument();
  });

  it('raw JSON drawer is initially closed', () => {
    const { container } = render(FleetNodeDetailView);
    expect(container.querySelector('#agent-raw-json-panel')).toBeNull();
  });

  it('raw JSON drawer opens on click and contains agent data', async () => {
    const { container } = render(FleetNodeDetailView);
    const drawerBtn = screen.getByRole('button', { name: /raw node state/i });
    await fireEvent.click(drawerBtn);
    const panel = container.querySelector('#agent-raw-json-panel');
    expect(panel).not.toBeNull();
    expect(panel!.textContent).toContain('agent-xyz-789');
  });
});
