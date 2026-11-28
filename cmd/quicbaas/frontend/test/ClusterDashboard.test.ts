/**
 * ClusterDashboard unit tests: the status cards, the leadership card (the
 * cluster leader loaded once, "No leader" on a 503, the shard-leader lookup
 * and its input check) and the operations panel below them.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import ClusterDashboard from '../src/views/cluster/ClusterDashboard.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

const STATUS = {
  node_id: 1,
  raft_address: '10.0.0.1:7000',
  total_nodes: 3,
  shards: [{ shard_id: 1, leader_id: 1, state: 'healthy' }],
  healthy: true,
  timestamp: '2026-11-21T00:00:00Z',
};

const LEADER = {
  leader: {
    node_id: 1, address: '10.0.0.1:8443', raft_address: '10.0.0.1:7000', is_leader: true,
    health_status: 'healthy', version: '', uptime_seconds: 0,
  },
  timestamp: STATUS.timestamp,
};

const cluster = {
  status: vi.fn(),
  leader: vi.fn(),
  shardLeader: vi.fn(),
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

beforeEach(() => {
  vi.clearAllMocks();
  for (const fn of Object.values(cluster)) fn.mockReset();
  setAuthenticated({ access_token: sessionToken('usr_A') });
  cluster.status.mockResolvedValue(STATUS);
  cluster.leader.mockResolvedValue(LEADER);
  cluster.shardLeader.mockResolvedValue({ shard_id: 2, leader_id: 3, leader_address: '10.0.0.3:7000', term: 7, valid: true });
  vi.mocked(getSDKClient).mockResolvedValue({ cluster } as never);
});

afterEach(() => {
  setUnauthenticated();
});

describe('ClusterDashboard', () => {
  it('loads the cluster leader once and shows it', async () => {
    render(ClusterDashboard);
    await waitFor(() => expect(screen.getByTestId('cluster-leader')).toHaveTextContent('10.0.0.1:8443'));
    expect(screen.getByTestId('cluster-leader')).toHaveTextContent(/node 1/);
    expect(cluster.leader).toHaveBeenCalledTimes(1);
  });

  it('shows No leader on a 503 while the status cards still render', async () => {
    cluster.leader.mockRejectedValue(new APIError(503, 'no leader elected'));
    const { container } = render(ClusterDashboard);
    await waitFor(() => expect(screen.getByTestId('cluster-leader')).toHaveTextContent('No leader'));
    await waitFor(() => expect(container.querySelectorAll('.stat')).toHaveLength(3));
    expect(screen.queryByText('no leader elected')).toBeNull();
  });

  it('shows another leader error inline', async () => {
    cluster.leader.mockRejectedValue(new APIError(403, 'forbidden'));
    render(ClusterDashboard);
    await waitFor(() => expect(screen.getByTestId('cluster-leader')).toHaveTextContent('forbidden'));
  });

  it('looks up the leader of the entered shard', async () => {
    render(ClusterDashboard);
    await fireEvent.input(screen.getByLabelText('Shard ID'), { target: { value: '2' } });
    await fireEvent.click(screen.getByRole('button', { name: /Look up shard leader/ }));
    await waitFor(() => expect(screen.getByTestId('shard-leader')).toHaveTextContent('10.0.0.3:7000'));
    expect(screen.getByTestId('shard-leader')).toHaveTextContent('term 7');
    expect(cluster.shardLeader).toHaveBeenCalledWith(2);
  });

  it('makes no lookup for a non-numeric shard id', async () => {
    render(ClusterDashboard);
    await fireEvent.input(screen.getByLabelText('Shard ID'), { target: { value: 'abc' } });
    await fireEvent.click(screen.getByRole('button', { name: /Look up shard leader/ }));
    await waitFor(() => expect(screen.getByTestId('shard-leader-error')).toHaveTextContent('whole number'));
    expect(cluster.shardLeader).not.toHaveBeenCalled();
  });

  it('renders the operations panel', async () => {
    render(ClusterDashboard);
    expect(screen.getByTestId('cluster-operations')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Show plan' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Start migration' })).toBeInTheDocument();
  });

  it('shows that leadership is not available when the client has no cluster', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({ cluster: null } as never);
    render(ClusterDashboard);
    await waitFor(() => expect(screen.getByTestId('cluster-leader')).toHaveTextContent('not available over this connection'));
  });
});
