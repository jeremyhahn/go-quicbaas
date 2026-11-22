/**
 * NodesView unit tests: the view lists the nodes from cluster.listNodes(),
 * marks the leader, shows an empty state for no nodes, and on a failed load
 * shows an error alert whose Retry button loads again. Also checks the
 * sidebar mapping and the router for #/nodes.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

import NodesView from '../src/views/cluster/NodesView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { navigate } from '../src/stores/navigation.svelte.ts';
import { routerStore } from '../src/stores/router.svelte.ts';

const mockListNodes = vi.fn();

const TWO_NODES = {
  nodes: [
    {
      node_id: 1,
      address: '10.0.0.1:8443',
      raft_address: '10.0.0.1:7000',
      is_leader: true,
      health_status: 'healthy',
      version: 'v1.2.3',
      uptime_seconds: 90061,
    },
    {
      node_id: 2,
      address: '10.0.0.2:8443',
      raft_address: '10.0.0.2:7000',
      is_leader: false,
      health_status: 'degraded',
      version: 'v1.2.3',
      uptime_seconds: 3720,
    },
  ],
  total_count: 2,
  timestamp: '2026-11-21T00:00:00Z',
};

beforeEach(() => {
  vi.clearAllMocks();
  mockListNodes.mockResolvedValue(TWO_NODES);
  vi.mocked(getSDKClient).mockResolvedValue({
    cluster: { listNodes: mockListNodes },
  } as never);
});

describe('NodesView', () => {
  it('renders the page heading with icon', async () => {
    const { container } = render(NodesView);
    await waitFor(() => {
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('Nodes');
      expect(container.querySelector('header.page-header svg')).not.toBeNull();
    });
  });

  it('renders one row per node with the Leader badge on the leader', async () => {
    const { container } = render(NodesView);
    await waitFor(() => {
      expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    });
    const rows = container.querySelectorAll('tbody tr');
    expect(rows[0].textContent).toContain('10.0.0.1:8443');
    expect(rows[0].textContent).toContain('Leader');
    expect(rows[0].textContent).toContain('1d 1h');
    expect(rows[1].textContent).toContain('Follower');
    expect(rows[1].textContent).not.toContain('Leader');
    expect(rows[1].textContent).toContain('1h 2m');
    expect(mockListNodes).toHaveBeenCalledTimes(1);
  });

  it('shows the empty state when no nodes are reported', async () => {
    mockListNodes.mockResolvedValue({ nodes: [], total_count: 0, timestamp: TWO_NODES.timestamp });
    render(NodesView);
    await waitFor(() => {
      expect(screen.getByText('No nodes reported')).toBeInTheDocument();
    });
  });

  it('shows an error alert whose Retry loads again', async () => {
    mockListNodes.mockRejectedValueOnce(new Error('cluster unavailable'));
    render(NodesView);
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('cluster unavailable');
    });
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(mockListNodes).toHaveBeenCalledTimes(2));
  });
});

describe('Nodes routing', () => {
  it('navigate("nodes") routes to #/nodes', () => {
    window.location.hash = '#/dashboard';
    navigate('nodes');
    expect(window.location.hash).toBe('#/nodes');
  });

  it('the router maps #/nodes to the nodes route', async () => {
    window.location.hash = '#/dashboard';
    await waitFor(() => expect(routerStore.name).toBe('dashboard'));
    window.location.hash = '#/nodes';
    await waitFor(() => expect(routerStore.name).toBe('nodes'));
  });
});
