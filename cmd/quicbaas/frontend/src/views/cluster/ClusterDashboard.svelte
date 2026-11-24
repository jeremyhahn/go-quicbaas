<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  ClusterDashboard shows the current consensus mode, node identity, and shard
  count from the /api/v1/cluster/status endpoint. In standalone mode (no Raft
  peers configured) it surfaces the standalone badge so operators know the
  node is running in single-node mode rather than in a degraded cluster.
  Below the status it shows the cluster leader with a lookup of one shard's
  leader, and ClusterOperationsPanel for rebalances and migrations.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import StatCard from '../../components/core/StatCard.svelte';
  import ClusterOperationsPanel from '../../components/cluster/ClusterOperationsPanel.svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { Server, Network, HardDrive, CheckCircle, AlertTriangle, Crown, Search } from 'lucide-svelte';
  import { formatDateTime } from '../../lib/time.ts';
  import { APIError } from '@quicbaas/sdk';
  import type { ClusterLeaderResponse, ShardLeaderResponse } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // Wire shape returned by GET /api/v1/cluster/status
  // ---------------------------------------------------------------------------

  /** ShardStatus is one Raft shard as the status endpoint reports it. */
  interface ShardStatus {
    shard_id: number;
    leader_id: number;
    state: string;
  }

  interface ClusterStatusResponse {
    node_id: number;
    raft_address: string;
    leader_id?: string;
    total_nodes?: number;
    shards?: ShardStatus[];
    healthy: boolean;
    timestamp: string;
  }

  const SHARD_COLUMNS: Column<ShardStatus>[] = [
    { key: 'shard_id', label: 'Shard', sortable: true },
    { key: 'leader_id', label: 'Leader', sortable: true },
    { key: 'state', label: 'State', sortable: true },
  ];

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let status: ClusterStatusResponse | null = $state(null);
  let loading = $state(false);
  let error: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadStatus(): Promise<void> {
    loading = true;
    error = null;
    try {
      // Use the authenticated SDK cluster client so the bearer token is
      // attached (a raw fetch omitted it and 401'd under default-deny authz).
      const client = await getSDKClient();
      status = (await client.cluster.status()) as ClusterStatusResponse;
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load cluster status';
    } finally {
      loading = false;
    }
  }

  $effect(() => { void loadStatus(); });

  // ---------------------------------------------------------------------------
  // Leadership: the cluster leader (GET /api/v1/cluster/leader, a 503 when the
  // cluster has none) and a lookup of one shard's leader. A session change
  // clears both and drops any answer still in flight.
  // ---------------------------------------------------------------------------

  const LEADERSHIP_UNAVAILABLE = 'Cluster leadership is not available over this connection';

  let leader = $state<ClusterLeaderResponse | null>(null);
  let noLeader = $state(false);
  let leaderLoading = $state(false);
  let leaderError = $state('');
  let shardInput = $state('');
  let shardLeader = $state<ShardLeaderResponse | null>(null);
  let shardLeaderLoading = $state(false);
  let shardLeaderError = $state('');

  async function loadLeader(forSession: string): Promise<void> {
    leaderLoading = true;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      if (!client.cluster) { leaderError = LEADERSHIP_UNAVAILABLE; return; }
      const resp = await client.cluster.leader();
      if (forSession !== authStore.sessionKey) return;
      leader = resp;
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      if (e instanceof APIError && e.status === 503) noLeader = true;
      else leaderError = e instanceof Error ? e.message : 'Failed to load the cluster leader';
    } finally {
      if (forSession === authStore.sessionKey) leaderLoading = false;
    }
  }

  async function lookUpShardLeader(): Promise<void> {
    shardLeaderError = '';
    const raw = shardInput.trim();
    if (!/^\d+$/.test(raw)) {
      shardLeader = null;
      shardLeaderError = raw === '' ? 'Shard ID is required' : 'Shard ID must be a whole number';
      return;
    }
    const forSession = authStore.sessionKey;
    shardLeaderLoading = true;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      if (!client.cluster) { shardLeaderError = LEADERSHIP_UNAVAILABLE; return; }
      const resp = await client.cluster.shardLeader(Number(raw));
      if (forSession !== authStore.sessionKey) return;
      shardLeader = resp;
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      shardLeader = null;
      shardLeaderError = e instanceof Error ? e.message : 'Failed to look up the shard leader';
    } finally {
      if (forSession === authStore.sessionKey) shardLeaderLoading = false;
    }
  }

  $effect(() => {
    const forSession = authStore.sessionKey;
    leader = null;
    noLeader = false;
    leaderError = '';
    shardLeader = null;
    shardLeaderError = '';
    void loadLeader(forSession);
  });

  // ---------------------------------------------------------------------------
  // Derived display helpers
  // ---------------------------------------------------------------------------

  function clusterMode(s: ClusterStatusResponse): string {
    if (!s.shards || s.shards.length === 0) return 'standalone';
    return 'raft';
  }

  function shardCount(s: ClusterStatusResponse): number {
    return s.shards?.length ?? 0;
  }

  function nodeCount(s: ClusterStatusResponse): number {
    return s.total_nodes ?? (s.node_id !== 0 ? 1 : 0);
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Cluster" subtitle="Consensus mode, node identity, and shard topology" icon={Server} />

  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadStatus(); }}>Retry</button>
    </div>
  {/if}

  {#if loading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else if status}
    <!-- Mode badge -->
    <div class="flex items-center gap-3 mb-2">
      {#if clusterMode(status) === 'standalone'}
        <span class="badge badge-info badge-sm">standalone</span>
        <span class="text-sm text-base-content/60">Single-node mode — no Raft peers configured.</span>
      {:else}
        <span class="badge badge-success badge-sm">raft</span>
        <span class="text-sm text-base-content/60">Multi-node Raft consensus active.</span>
      {/if}
      {#if status.healthy}
        <CheckCircle class="h-4 w-4 text-success" aria-label="Cluster healthy" />
      {:else}
        <AlertTriangle class="h-4 w-4 text-warning" aria-label="Cluster not fully healthy" />
      {/if}
    </div>

    <!-- Stat cards -->
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
      <StatCard
        label="Mode"
        value={clusterMode(status)}
        icon={Server}
      />
      <StatCard
        label="Node ID"
        value={status.node_id === 0 ? '—' : String(status.node_id)}
        icon={Network}
        variant="primary"
      />
      <StatCard
        label="Shards"
        value={String(shardCount(status))}
        icon={HardDrive}
      />
    </div>

    <!-- Detail table -->
    {#if clusterMode(status) === 'raft'}
      <DataTable
        columns={SHARD_COLUMNS}
        rows={(status.shards ?? [])}
        rowKey={(shard: ShardStatus) => shard.shard_id}
        ariaLabel="Raft shards"
        emptyTitle="No shards reported."
      >
        {#snippet row(shard: ShardStatus)}
          <tr class="hover">
            <td class="font-mono text-xs">{shard.shard_id}</td>
            <td class="font-mono text-xs">{shard.leader_id}</td>
            <td>
              <span class="badge badge-xs" class:badge-success={shard.state === 'healthy'} class:badge-warning={shard.state !== 'healthy'}>
                {shard.state}
              </span>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {:else}
      <div class="card bg-base-200 shadow-sm">
        <div class="card-body py-4 gap-2">
          <p class="text-sm text-base-content/60">
            This node is running in <strong>standalone mode</strong>. Raft clustering is supported
            but no peer nodes have been joined. To form a multi-node cluster, add nodes via
            <code class="font-mono text-xs">POST /api/v1/cluster/nodes</code> or configure
            peer addresses in the daemon configuration file.
          </p>
          <p class="text-sm text-base-content/40">
            Node count: {nodeCount(status)} &nbsp;&bull;&nbsp;
            Timestamp: {formatDateTime(status.timestamp)}
          </p>
        </div>
      </div>
    {/if}
  {:else if !loading && !error}
    <div class="text-center py-10 text-base-content/30 text-sm">No cluster status available.</div>
  {/if}

  <!-- Leadership -->
  <section class="card bg-base-200 shadow-sm" aria-label="Leadership">
    <div class="card-body gap-3">
      <h2 class="card-title text-base"><Crown class="h-4 w-4" /> Leadership</h2>
      <div class="text-sm" data-testid="cluster-leader">
        {#if leaderLoading}
          <span class="loading loading-spinner loading-xs"></span>
        {:else if leader}
          Cluster leader: node <span class="font-mono font-semibold">{leader.leader.node_id}</span>
          at <span class="font-mono">{leader.leader.address}</span>
          {#if leader.leader.raft_address && leader.leader.raft_address !== leader.leader.address}
            (Raft <span class="font-mono">{leader.leader.raft_address}</span>)
          {/if}
        {:else if noLeader}
          <span class="badge badge-warning badge-sm">No leader</span>
          <span class="text-base-content/60">The cluster has no elected leader right now.</span>
        {:else if leaderError}
          <span class="text-error">{leaderError}</span>
        {/if}
      </div>

      <form class="flex flex-wrap items-end gap-2" onsubmit={(e) => { e.preventDefault(); void lookUpShardLeader(); }}>
        <label class="form-control">
          <span class="label-text text-xs">Shard ID</span>
          <input class="input input-sm input-bordered w-32" type="text" inputmode="numeric" bind:value={shardInput} />
        </label>
        <button type="submit" class="btn btn-sm btn-ghost" disabled={shardLeaderLoading}>
          {#if shardLeaderLoading}<span class="loading loading-spinner loading-xs"></span>{:else}<Search class="h-4 w-4" />{/if}
          Look up shard leader
        </button>
      </form>
      {#if shardLeader}
        <p class="text-sm" data-testid="shard-leader">
          Shard <span class="font-mono">{shardLeader.shard_id}</span>:
          {#if shardLeader.valid}
            leader node <span class="font-mono font-semibold">{shardLeader.leader_id}</span>
            {#if shardLeader.leader_address}at <span class="font-mono">{shardLeader.leader_address}</span>{/if}
            {#if shardLeader.term !== undefined}&nbsp;&bull;&nbsp; term {shardLeader.term}{/if}
          {:else}
            <span class="badge badge-warning badge-sm">No leader</span>
          {/if}
        </p>
      {/if}
      {#if shardLeaderError}
        <div class="alert alert-error text-sm" role="alert" data-testid="shard-leader-error">{shardLeaderError}</div>
      {/if}
    </div>
  </section>

  <!-- Long operations -->
  <ClusterOperationsPanel />
</div>
