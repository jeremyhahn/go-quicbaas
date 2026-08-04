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
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatCard from '../../components/core/StatCard.svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { Server, Network, HardDrive, CheckCircle, AlertTriangle } from 'lucide-svelte';
  import { formatDateTime } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Wire shape returned by GET /api/v1/cluster/status
  // ---------------------------------------------------------------------------

  interface ClusterStatusResponse {
    node_id: number;
    raft_address: string;
    leader_id?: string;
    total_nodes?: number;
    shards?: Array<{ shard_id: number; leader_id: number; state: string }>;
    healthy: boolean;
    timestamp: string;
  }

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
      <div class="overflow-x-auto rounded-box border border-base-300/50">
        <table class="table table-sm table-zebra">
          <thead>
            <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
              <th>Shard</th>
              <th>Leader</th>
              <th>State</th>
            </tr>
          </thead>
          <tbody>
            {#each (status.shards ?? []) as shard (shard.shard_id)}
              <tr class="hover">
                <td class="font-mono text-xs">{shard.shard_id}</td>
                <td class="font-mono text-xs">{shard.leader_id}</td>
                <td>
                  <span class="badge badge-xs" class:badge-success={shard.state === 'healthy'} class:badge-warning={shard.state !== 'healthy'}>
                    {shard.state}
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
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
</div>
