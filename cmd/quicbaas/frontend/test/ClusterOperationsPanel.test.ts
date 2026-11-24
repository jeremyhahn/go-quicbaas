/**
 * ClusterOperationsPanel unit tests: the rebalance plan, a rebalance start
 * with its progress poll, a migration start with its progress poll and a
 * confirmed cancel, inline errors for a failed call, and the session guard
 * that drops a late answer and stops a poll.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import { flushSync, tick } from 'svelte';
import { APIError } from '@quicbaas/sdk';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import ClusterOperationsPanel from '../src/components/cluster/ClusterOperationsPanel.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';

const PLAN = { current_shards: 2, target_shards: 4, strategy: 'balanced', estimated_time: 120, timestamp: '2026-11-21T00:00:00Z' };

const cluster = {
  rebalancePlan: vi.fn(),
  startRebalance: vi.fn(),
  rebalanceStatus: vi.fn(),
  startMigration: vi.fn(),
  migrationStatus: vi.fn(),
  cancelMigration: vi.fn(),
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

function migrationStatus(status: string, percent: number): Record<string, unknown> {
  return {
    operation_id: 'mig-1', status, keys_processed: 10, bytes_processed: 100,
    percent_complete: percent, elapsed_time: 3, timestamp: '2026-11-21T00:00:00Z',
  };
}

async function startMigration(source = '1', target = '2'): Promise<void> {
  await fireEvent.input(screen.getByLabelText('Source shard'), { target: { value: source } });
  await fireEvent.input(screen.getByLabelText('Target shard'), { target: { value: target } });
  await fireEvent.click(screen.getByRole('button', { name: 'Start migration' }));
}

beforeEach(() => {
  vi.clearAllMocks();
  for (const fn of Object.values(cluster)) fn.mockReset();
  setAuthenticated({ access_token: sessionToken('usr_A') });
  cluster.rebalancePlan.mockResolvedValue(PLAN);
  cluster.startRebalance.mockResolvedValue({ operation_id: 'reb-1', started: true, status: 'started', timestamp: PLAN.timestamp });
  cluster.startMigration.mockResolvedValue({
    operation_id: 'mig-1', status: 'started', source_shard_id: 1, target_shard_id: 2, estimated_time: 60, timestamp: PLAN.timestamp,
  });
  cluster.cancelMigration.mockResolvedValue({ operation_id: 'mig-1', cancelled: true, message: 'cancelled' });
  vi.mocked(getSDKClient).mockResolvedValue({ cluster } as never);
});

afterEach(() => {
  vi.useRealTimers();
  setUnauthenticated();
});

describe('ClusterOperationsPanel', () => {
  it('shows the rebalance plan', async () => {
    render(ClusterOperationsPanel);
    await fireEvent.click(screen.getByRole('button', { name: 'Show plan' }));
    await waitFor(() => expect(screen.getByTestId('rebalance-plan')).toHaveTextContent(/2.*4/));
    expect(screen.getByTestId('rebalance-plan')).toHaveTextContent('balanced');
    expect(screen.getByTestId('rebalance-plan')).toHaveTextContent('120');
    expect(cluster.rebalancePlan).toHaveBeenCalledTimes(1);
  });

  it('starts a rebalance with the form fields and polls its progress to a terminal status', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    cluster.rebalanceStatus
      .mockResolvedValueOnce({ operation_id: 'reb-1', status: 'in_progress', percent_complete: 40, elapsed_time: 1, timestamp: PLAN.timestamp })
      .mockResolvedValueOnce({ operation_id: 'reb-1', status: 'completed', percent_complete: 100, elapsed_time: 3, timestamp: PLAN.timestamp });
    render(ClusterOperationsPanel);
    await fireEvent.input(screen.getByLabelText('Strategy'), { target: { value: 'balanced' } });
    await fireEvent.input(screen.getByLabelText('Target shard count'), { target: { value: '4' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Start rebalance' }));
    await waitFor(() => expect(screen.getByTestId('rebalance-progress')).toHaveTextContent(/40/));
    expect(cluster.startRebalance).toHaveBeenCalledWith({ strategy: 'balanced', target_shard_count: 4 });
    expect(cluster.rebalanceStatus).toHaveBeenCalledWith('reb-1');
    await vi.advanceTimersByTimeAsync(2000);
    await waitFor(() => expect(screen.getByTestId('rebalance-progress')).toHaveTextContent(/completed/));
    await vi.advanceTimersByTimeAsync(6000);
    expect(cluster.rebalanceStatus).toHaveBeenCalledTimes(2);
  });

  it('starts a rebalance with the server defaults when the form is empty', async () => {
    cluster.rebalanceStatus.mockResolvedValue({ operation_id: 'reb-1', status: 'completed', percent_complete: 100, elapsed_time: 1, timestamp: PLAN.timestamp });
    render(ClusterOperationsPanel);
    await fireEvent.click(screen.getByRole('button', { name: 'Start rebalance' }));
    await waitFor(() => expect(cluster.startRebalance).toHaveBeenCalledWith({}));
  });

  it('rejects a non-numeric target shard count without a call', async () => {
    render(ClusterOperationsPanel);
    await fireEvent.input(screen.getByLabelText('Target shard count'), { target: { value: 'many' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Start rebalance' }));
    await waitFor(() => expect(screen.getByTestId('rebalance-error')).toHaveTextContent(/number/));
    expect(cluster.startRebalance).not.toHaveBeenCalled();
  });

  it('starts a migration and polls its progress to a terminal status', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    cluster.migrationStatus
      .mockResolvedValueOnce(migrationStatus('in_progress', 25))
      .mockResolvedValueOnce(migrationStatus('completed', 100));
    render(ClusterOperationsPanel);
    await fireEvent.input(screen.getByLabelText('Entity types'), { target: { value: 'kv, documents' } });
    await startMigration();
    await waitFor(() => expect(screen.getByTestId('migration-progress')).toHaveTextContent(/25/));
    expect(cluster.startMigration).toHaveBeenCalledWith({ source_shard_id: 1, target_shard_id: 2, entity_types: ['kv', 'documents'] });
    expect(cluster.migrationStatus).toHaveBeenCalledWith('mig-1');
    await vi.advanceTimersByTimeAsync(2000);
    await waitFor(() => expect(screen.getByTestId('migration-progress')).toHaveTextContent(/completed/));
    await vi.advanceTimersByTimeAsync(6000);
    expect(cluster.migrationStatus).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole('button', { name: 'Cancel migration' })).toBeNull();
  });

  it('rejects a missing or non-numeric shard without a call', async () => {
    render(ClusterOperationsPanel);
    await startMigration('', '2');
    await waitFor(() => expect(screen.getByTestId('migration-error')).toHaveTextContent(/Source shard/));
    await startMigration('1', 'x');
    await waitFor(() => expect(screen.getByTestId('migration-error')).toHaveTextContent(/Target shard/));
    expect(cluster.startMigration).not.toHaveBeenCalled();
  });

  it('cancels a migration after confirmation and stops the poll', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    cluster.migrationStatus.mockResolvedValue(migrationStatus('in_progress', 25));
    render(ClusterOperationsPanel);
    await startMigration();
    await waitFor(() => expect(screen.getByTestId('migration-progress')).toHaveTextContent(/25/));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel migration' }));
    expect(cluster.cancelMigration).not.toHaveBeenCalled();
    const dialog = screen.getByRole('dialog', { name: 'Cancel migration confirmation' });
    expect(dialog).toHaveTextContent('mig-1');
    await fireEvent.click(screen.getByRole('button', { name: 'Confirm cancel' }));
    await waitFor(() => expect(cluster.cancelMigration).toHaveBeenCalledWith('mig-1'));
    await waitFor(() => expect(screen.getByTestId('migration-progress')).toHaveTextContent(/cancelled/));
    const calls = cluster.migrationStatus.mock.calls.length;
    await vi.advanceTimersByTimeAsync(6000);
    expect(cluster.migrationStatus).toHaveBeenCalledTimes(calls);
  });

  it('keeps the migration when the cancel is dismissed', async () => {
    cluster.migrationStatus.mockResolvedValue(migrationStatus('in_progress', 25));
    render(ClusterOperationsPanel);
    await startMigration();
    await waitFor(() => expect(screen.getByTestId('migration-progress')).toHaveTextContent(/25/));
    await fireEvent.click(screen.getByRole('button', { name: 'Cancel migration' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Keep running' }));
    expect(screen.queryByRole('dialog', { name: 'Cancel migration confirmation' })).toBeNull();
    expect(cluster.cancelMigration).not.toHaveBeenCalled();
  });

  it.each([403, 503])('shows a %i from the plan inline', async (status) => {
    cluster.rebalancePlan.mockRejectedValue(new APIError(status, 'plan refused'));
    render(ClusterOperationsPanel);
    await fireEvent.click(screen.getByRole('button', { name: 'Show plan' }));
    await waitFor(() => expect(screen.getByTestId('rebalance-error')).toHaveTextContent('plan refused'));
  });

  it.each([403, 503])('shows a %i from a migration start inline', async (status) => {
    cluster.startMigration.mockRejectedValue(new APIError(status, 'migration refused'));
    render(ClusterOperationsPanel);
    await startMigration();
    await waitFor(() => expect(screen.getByTestId('migration-error')).toHaveTextContent('migration refused'));
    expect(cluster.migrationStatus).not.toHaveBeenCalled();
  });

  it('stops a poll on an error', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    cluster.rebalanceStatus.mockRejectedValue(new APIError(503, 'status unavailable'));
    render(ClusterOperationsPanel);
    await fireEvent.click(screen.getByRole('button', { name: 'Start rebalance' }));
    await waitFor(() => expect(screen.getByTestId('rebalance-error')).toHaveTextContent('status unavailable'));
    await vi.advanceTimersByTimeAsync(6000);
    expect(cluster.rebalanceStatus).toHaveBeenCalledTimes(1);
  });

  it('shows that cluster operations are not available when the client has none', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({ cluster: null } as never);
    render(ClusterOperationsPanel);
    await fireEvent.click(screen.getByRole('button', { name: 'Show plan' }));
    await waitFor(() => expect(screen.getByTestId('rebalance-error')).toHaveTextContent('not available over this connection'));
  });

  it('drops an answer still in flight when the session changes', async () => {
    const plan = deferred<typeof PLAN>();
    cluster.rebalancePlan.mockReturnValueOnce(plan.promise);
    render(ClusterOperationsPanel);
    await fireEvent.click(screen.getByRole('button', { name: 'Show plan' }));
    await waitFor(() => expect(cluster.rebalancePlan).toHaveBeenCalledTimes(1));
    setAuthenticated({ access_token: sessionToken('usr_B') });
    flushSync();
    plan.resolve(PLAN);
    await plan.promise;
    await tick();
    expect(screen.queryByTestId('rebalance-plan')).toBeNull();
  });

  it('stops a poll when the session changes', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    cluster.migrationStatus.mockResolvedValue(migrationStatus('in_progress', 25));
    render(ClusterOperationsPanel);
    await startMigration();
    await waitFor(() => expect(screen.getByTestId('migration-progress')).toHaveTextContent(/25/));
    setAuthenticated({ access_token: sessionToken('usr_B') });
    flushSync();
    expect(screen.queryByTestId('migration-progress')).toBeNull();
    const calls = cluster.migrationStatus.mock.calls.length;
    await vi.advanceTimersByTimeAsync(6000);
    expect(cluster.migrationStatus).toHaveBeenCalledTimes(calls);
  });

  it('stops a poll on unmount', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    cluster.migrationStatus.mockResolvedValue(migrationStatus('in_progress', 25));
    const { unmount } = render(ClusterOperationsPanel);
    await startMigration();
    await waitFor(() => expect(cluster.migrationStatus).toHaveBeenCalledTimes(1));
    unmount();
    await vi.advanceTimersByTimeAsync(6000);
    expect(cluster.migrationStatus).toHaveBeenCalledTimes(1);
  });
});
