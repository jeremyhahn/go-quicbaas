/**
 * NodesView unit tests: the view lists the nodes from cluster.listNodes(),
 * counts and marks every shard leader, follows the signed-in session (clears
 * and reloads on a change, drops a late answer for a replaced session), shows version and uptime when reported and "not reported"
 * otherwise (the daemon's real response), claims no health, shows an empty
 * state for no nodes, and on a failed load
 * shows an error alert whose Retry button loads again. Also checks the
 * sidebar mapping and the router for #/nodes.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import { flushSync, tick } from 'svelte';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

import NodesView from '../src/views/cluster/NodesView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { navigate } from '../src/stores/navigation.svelte.ts';
import { routerStore } from '../src/stores/router.svelte.ts';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

const mockListNodes = vi.fn();

// What GET /api/v1/cluster/nodes returns today: the service sets health to
// healthy for every member and reports no version or uptime.
const SERVICE_NODES = {
  nodes: [
    {
      node_id: 1,
      address: '10.0.0.1:7000',
      raft_address: '10.0.0.1:7000',
      is_leader: true,
      health_status: 'healthy',
      version: '',
      uptime_seconds: 0,
    },
    {
      node_id: 2,
      address: '10.0.0.2:7000',
      raft_address: '10.0.0.2:7000',
      is_leader: false,
      health_status: 'healthy',
      version: '',
      uptime_seconds: 0,
    },
  ],
  total_count: 2,
  timestamp: '2026-11-21T00:00:00Z',
};

const REPORTED_NODES = {
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

// Several nodes can lead a shard each; the service builds the list from a map,
// so the order is arbitrary.
const MULTI_LEADER_NODES = {
  nodes: [3, 2, 1].map((id) => ({
    node_id: id,
    address: `10.0.0.${id}:7000`,
    raft_address: `10.0.0.${id}:7000`,
    is_leader: id !== 2,
    health_status: 'healthy',
    version: '',
    uptime_seconds: 0,
  })),
  total_count: 3,
  timestamp: '2026-11-21T00:00:00Z',
};

const OTHER_NODES = {
  nodes: [
    {
      node_id: 9,
      address: '10.0.0.9:7000',
      raft_address: '10.0.0.9:7000',
      is_leader: true,
      health_status: 'healthy',
      version: '',
      uptime_seconds: 0,
    },
  ],
  total_count: 1,
  timestamp: '2026-11-21T00:00:00Z',
};

/** sessionToken builds an unsigned JWT for a session of the given user. */
function sessionToken(uid: string): string {
  const b64url = (o: unknown): string =>
    btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const claims = {
    sub: uid,
    uid,
    tenant_id: 'system',
    roles: ['admin'],
    sid: `sid_${uid}`,
    exp: Math.floor(Date.now() / 1000) + 3600,
  };
  return `${b64url({ alg: 'none' })}.${b64url(claims)}.sig`;
}

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

/** statCard returns the StatCard with the given label. */
function statCard(container: HTMLElement, label: string): Element | undefined {
  return Array.from(container.querySelectorAll('.stat'))
    .find((s) => s.querySelector('.stat-title')?.textContent === label);
}

/** stat returns the value shown on the StatCard with the given label. */
function stat(container: HTMLElement, label: string): string | null | undefined {
  return statCard(container, label)?.querySelector('.stat-title + div')?.textContent;
}

afterEach(() => {
  setUnauthenticated();
});

beforeEach(() => {
  vi.clearAllMocks();
  setAuthenticated({ access_token: sessionToken('usr_A') });
  // Reset, not only clear: a once-answer a test did not consume must not
  // leak into the next test.
  mockListNodes.mockReset();
  mockListNodes.mockResolvedValue(SERVICE_NODES);
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
    mockListNodes.mockResolvedValue(REPORTED_NODES);
    const { container } = render(NodesView);
    await waitFor(() => {
      expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    });
    const rows = container.querySelectorAll('tbody tr');
    expect(rows[0].textContent).toContain('10.0.0.1:8443');
    expect(rows[0].textContent).toContain('Shard leader');
    expect(rows[0].textContent).toContain('1d 1h');
    expect(rows[0].textContent).toContain('v1.2.3');
    expect(rows[1].textContent).toContain('Follower');
    expect(rows[1].textContent).not.toContain('Leader');
    expect(rows[1].textContent).toContain('1h 2m');
    expect(mockListNodes).toHaveBeenCalledTimes(1);
  });

  it('marks version and uptime as not reported and claims no health', async () => {
    const { container } = render(NodesView);
    await waitFor(() => {
      expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    });
    const rows = container.querySelectorAll('tbody tr');
    for (const row of rows) {
      expect(row.textContent!.split('not reported')).toHaveLength(3);
    }
    expect(container.textContent).not.toContain('healthy');
    expect(container.textContent).not.toContain('Healthy');
    expect(screen.queryByRole('columnheader', { name: /^Health/ })).toBeNull();
    expect(stat(container, 'Total')).toBe('2');
    expect(stat(container, 'Shard leaders')).toBe('1');
  });

  it('counts and lists every shard leader', async () => {
    mockListNodes.mockResolvedValue(MULTI_LEADER_NODES);
    const { container } = render(NodesView);
    await waitFor(() => {
      expect(container.querySelectorAll('tbody tr')).toHaveLength(3);
    });
    expect(stat(container, 'Shard leaders')).toBe('2');
    expect(statCard(container, 'Shard leaders')?.querySelector('.stat-desc')?.textContent).toBe('Nodes 1, 3');
    const leaderRows = Array.from(container.querySelectorAll('tbody tr'))
      .filter((r) => r.textContent!.includes('Shard leader'));
    expect(leaderRows).toHaveLength(2);
  });

  it('clears the previous session\'s nodes and reloads when the session changes', async () => {
    const { container } = render(NodesView);
    await waitFor(() => {
      expect(container.querySelectorAll('tbody tr')).toHaveLength(2);
    });
    expect(container.textContent).toContain('10.0.0.1:7000');
    mockListNodes.mockRejectedValueOnce(new Error('forbidden'));
    setAuthenticated({ access_token: sessionToken('usr_B') });
    flushSync();
    await waitFor(() => expect(mockListNodes).toHaveBeenCalledTimes(2));
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('forbidden');
    });
    expect(container.textContent).not.toContain('10.0.0.1:7000');
    expect(container.querySelectorAll('tbody tr')).toHaveLength(0);
  });

  it('drops a response that arrives after its session was replaced', async () => {
    const first = deferred<typeof SERVICE_NODES>();
    mockListNodes.mockReturnValueOnce(first.promise).mockResolvedValueOnce(OTHER_NODES);
    const { container } = render(NodesView);
    await waitFor(() => expect(mockListNodes).toHaveBeenCalledTimes(1));
    setAuthenticated({ access_token: sessionToken('usr_B') });
    flushSync();
    await waitFor(() => {
      const rows = container.querySelectorAll('tbody tr');
      expect(rows).toHaveLength(1);
      expect(rows[0].textContent).toContain('10.0.0.9:7000');
    });
    first.resolve(SERVICE_NODES);
    await first.promise;
    await tick();
    expect(container.querySelectorAll('tbody tr')).toHaveLength(1);
    expect(container.textContent).not.toContain('10.0.0.1:7000');
  });

  it('shows the empty state when no nodes are reported', async () => {
    mockListNodes.mockResolvedValue({ nodes: [], total_count: 0, timestamp: SERVICE_NODES.timestamp });
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

  it('shows that cluster nodes are not available when the client has none', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({ cluster: null } as never);
    render(NodesView);
    await waitFor(() =>
      expect(screen.getByRole('alert').textContent).toContain(
        'Cluster nodes are not available over this connection',
      ),
    );
    expect(mockListNodes).not.toHaveBeenCalled();
  });
});

describe('NodesView membership', () => {
  const clusterNodes = {
    addNode: vi.fn(),
    removeNode: vi.fn(),
    replaceNode: vi.fn(),
    listNonLeaderNodes: vi.fn(),
  };
  const NON_LEADER_NODES = { ...SERVICE_NODES, nodes: [SERVICE_NODES.nodes[1]], total_count: 1 };

  beforeEach(() => {
    for (const fn of Object.values(clusterNodes)) fn.mockReset();
    clusterNodes.addNode.mockResolvedValue({ success: true, message: 'added', shard_id: 1, node_id: 3, timestamp: SERVICE_NODES.timestamp });
    clusterNodes.removeNode.mockResolvedValue({ success: true, message: 'removed', shard_id: 1, node_id: 2, timestamp: SERVICE_NODES.timestamp });
    clusterNodes.replaceNode.mockResolvedValue({ success: true, message: 'replaced', affected_shards: [1], timestamp: SERVICE_NODES.timestamp });
    clusterNodes.listNonLeaderNodes.mockResolvedValue(NON_LEADER_NODES);
    vi.mocked(getSDKClient).mockResolvedValue({ cluster: { listNodes: mockListNodes }, clusterNodes } as never);
  });

  async function rendered(): Promise<HTMLElement> {
    const { container } = render(NodesView);
    await waitFor(() => expect(container.querySelectorAll('tbody tr')).toHaveLength(2));
    await waitFor(() => expect(screen.getByRole('button', { name: /Add node/ })).not.toBeDisabled());
    return container;
  }

  it('adds a node through the dialog and reloads the list', async () => {
    await rendered();
    await fireEvent.click(screen.getByRole('button', { name: /Add node/ }));
    const dialog = screen.getByRole('dialog', { name: 'Add node' });
    expect(dialog).toBeInTheDocument();
    await fireEvent.input(screen.getByLabelText('Shard ID'), { target: { value: '1' } });
    await fireEvent.input(screen.getByLabelText('Node ID'), { target: { value: '3' } });
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: '10.0.0.3:7000' } });
    await fireEvent.input(screen.getByLabelText('Identity'), { target: { value: 'node-3' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    await waitFor(() => expect(clusterNodes.addNode).toHaveBeenCalledWith({
      shard_id: 1, node_id: 3, address: '10.0.0.3:7000', identity: 'node-3',
    }));
    await waitFor(() => expect(mockListNodes).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole('dialog', { name: 'Add node' })).toBeNull();
  });

  it('checks the add fields before it calls', async () => {
    await rendered();
    await fireEvent.click(screen.getByRole('button', { name: /Add node/ }));
    await fireEvent.input(screen.getByLabelText('Shard ID'), { target: { value: 'one' } });
    await fireEvent.input(screen.getByLabelText('Node ID'), { target: { value: '3' } });
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: '10.0.0.3:7000' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    await waitFor(() => expect(screen.getByTestId('membership-error')).toHaveTextContent('Shard ID must be a whole number'));
    expect(clusterNodes.addNode).not.toHaveBeenCalled();
  });

  it('shows a failed add inline and keeps the dialog open', async () => {
    clusterNodes.addNode.mockRejectedValue(new Error('node already a member'));
    await rendered();
    await fireEvent.click(screen.getByRole('button', { name: /Add node/ }));
    await fireEvent.input(screen.getByLabelText('Shard ID'), { target: { value: '1' } });
    await fireEvent.input(screen.getByLabelText('Node ID'), { target: { value: '2' } });
    await fireEvent.input(screen.getByLabelText('Address'), { target: { value: '10.0.0.2:7000' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    await waitFor(() => expect(screen.getByTestId('membership-error')).toHaveTextContent('node already a member'));
    expect(screen.getByRole('dialog', { name: 'Add node' })).toBeInTheDocument();
    expect(mockListNodes).toHaveBeenCalledTimes(1);
  });

  it('removes a node from a shard after confirmation and reloads', async () => {
    await rendered();
    await fireEvent.click(screen.getByRole('button', { name: 'Remove node 2' }));
    const dialog = screen.getByRole('dialog', { name: 'Remove node confirmation' });
    expect(dialog).toHaveTextContent('10.0.0.2:7000');
    await fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(screen.getByTestId('remove-error')).toHaveTextContent('Shard ID is required'));
    expect(clusterNodes.removeNode).not.toHaveBeenCalled();
    await fireEvent.input(screen.getByLabelText('Shard ID'), { target: { value: '1' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(clusterNodes.removeNode).toHaveBeenCalledWith(2, 1));
    await waitFor(() => expect(mockListNodes).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole('dialog', { name: 'Remove node confirmation' })).toBeNull();
  });

  it('makes no remove call when the confirmation is cancelled', async () => {
    await rendered();
    await fireEvent.click(screen.getByRole('button', { name: 'Remove node 2' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog', { name: 'Remove node confirmation' })).toBeNull();
    expect(clusterNodes.removeNode).not.toHaveBeenCalled();
  });

  it('replaces a node through the dialog and reloads', async () => {
    await rendered();
    await fireEvent.click(screen.getByRole('button', { name: 'Replace node 2' }));
    expect(screen.getByRole('dialog', { name: 'Replace node 2' })).toBeInTheDocument();
    await fireEvent.input(screen.getByLabelText('Replacement node ID'), { target: { value: '4' } });
    await fireEvent.input(screen.getByLabelText('Replacement address'), { target: { value: '10.0.0.4:7000' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Replace' }));
    await waitFor(() => expect(clusterNodes.replaceNode).toHaveBeenCalledWith(2, {
      replacement_node_id: 4, replacement_address: '10.0.0.4:7000',
    }));
    await waitFor(() => expect(mockListNodes).toHaveBeenCalledTimes(2));
  });

  it('lists through the non-leader route while the filter is on', async () => {
    const container = await rendered();
    await fireEvent.click(screen.getByLabelText('Non-leader only'));
    await waitFor(() => expect(clusterNodes.listNonLeaderNodes).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(container.querySelectorAll('tbody tr')).toHaveLength(1));
    expect(container.querySelector('tbody tr')!.textContent).toContain('10.0.0.2:7000');
    await fireEvent.click(screen.getByLabelText('Non-leader only'));
    await waitFor(() => expect(mockListNodes).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(container.querySelectorAll('tbody tr')).toHaveLength(2));
    expect(clusterNodes.listNonLeaderNodes).toHaveBeenCalledTimes(1);
  });

  it('disables the actions and still lists when the client has no clusterNodes', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({ cluster: { listNodes: mockListNodes } } as never);
    const { container } = render(NodesView);
    await waitFor(() => expect(container.querySelectorAll('tbody tr')).toHaveLength(2));
    expect(screen.getByRole('button', { name: /Add node/ })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Remove node 1' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Replace node 1' })).toBeDisabled();
    expect(screen.getByLabelText('Non-leader only')).toBeDisabled();
    expect(screen.queryByRole('alert')).toBeNull();
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
