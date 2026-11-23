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
  NodesView lists the members of the Raft cluster from GET
  /api/v1/cluster/nodes: address, Raft address, whether it leads any Raft
  shard (several nodes can, one per shard), and the version and
  uptime when the daemon reports them. The endpoint's health field is not
  shown: the daemon fills it from Raft membership, not from a liveness
  check. Read-only: no node is added or removed from here.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import StatCard from '../../components/core/StatCard.svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { Network, Server, Crown } from 'lucide-svelte';
  import type { ClusterNodeInfo, ListClusterNodesResponse } from '@quicbaas/sdk';
  import type { Component } from 'svelte';

  const NODE_COLUMNS: Column<ClusterNodeInfo>[] = [
    { key: 'node_id', label: 'Node ID', sortable: true },
    { key: 'address', label: 'Address', sortable: true },
    { key: 'raft_address', label: 'Raft address', sortable: true },
    { key: 'is_leader', label: 'Role', sortable: true },
    { key: 'version', label: 'Version', sortable: true },
    { key: 'uptime_seconds', label: 'Uptime', sortable: true },
  ];

  const UNAVAILABLE = 'Cluster nodes are not available over this connection';
  const NOT_REPORTED = 'not reported';

  // PageHeader takes a Svelte 5 Component; lucide-svelte's icon classes do not
  // match that signature in the type checker, though they render the same.
  const HEADER_ICON = Network as unknown as Component<{ class?: string }>;

  let result = $state<ListClusterNodesResponse | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);

  async function load(forSession: string): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      if (!client.cluster) { result = null; error = UNAVAILABLE; return; }
      const resp = await client.cluster.listNodes();
      // An answer for a session that has since been replaced belongs to the
      // principal who asked for it, not to whoever is signed in now.
      if (forSession !== authStore.sessionKey) return;
      result = resp;
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      error = e instanceof Error ? e.message : 'Failed to load cluster nodes';
    } finally {
      if (forSession === authStore.sessionKey) loading = false;
    }
  }

  $effect(() => {
    // Read synchronously so the effect re-runs when the session changes, and
    // clear first so a new principal never sees the previous one's nodes.
    const forSession = authStore.sessionKey;
    result = null;
    error = null;
    void load(forSession);
  });

  const nodes = $derived(result?.nodes ?? []);
  const leaderIDs = $derived(nodes.filter((n) => n.is_leader).map((n) => n.node_id).sort((a, b) => a - b));

  /** formatUptime renders seconds as `Xd Yh`, `Yh Zm` or `Zm`. */
  function formatUptime(seconds: number): string {
    const total = Math.max(0, Math.floor(seconds || 0));
    const days = Math.floor(total / 86400);
    const hours = Math.floor((total % 86400) / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Nodes" subtitle="Raft cluster membership and which nodes lead a shard. Liveness, version and uptime are not measured by the daemon and show as not reported when absent." icon={HEADER_ICON} />

  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void load(authStore.sessionKey); }}>Retry</button>
    </div>
  {/if}

  {#if loading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else if result}
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <StatCard label="Total" value={String(result.total_count)} icon={Server} />
      <StatCard label="Shard leaders" value={String(leaderIDs.length)} description={leaderIDs.length > 0 ? `Nodes ${leaderIDs.join(', ')}` : 'No shard leader reported'} icon={Crown} />
    </div>

    <DataTable
      columns={NODE_COLUMNS}
      rows={nodes}
      rowKey={(node: ClusterNodeInfo) => node.node_id}
      ariaLabel="Cluster nodes"
      emptyTitle="No nodes reported"
    >
      {#snippet row(node: ClusterNodeInfo)}
        <tr class="hover">
          <td class="font-mono text-xs">{node.node_id}</td>
          <td class="font-mono text-xs">{node.address}</td>
          <td class="font-mono text-xs">{node.raft_address}</td>
          <td>
            {#if node.is_leader}
              <span class="badge badge-primary badge-xs">Shard leader</span>
            {:else}
              <span class="text-xs text-base-content/60">Follower</span>
            {/if}
          </td>
          <td class={node.version ? 'font-mono text-xs' : 'text-xs text-base-content/60'}>{node.version ? node.version : NOT_REPORTED}</td>
          <td class={node.uptime_seconds > 0 ? 'text-xs' : 'text-xs text-base-content/60'}>{node.uptime_seconds > 0 ? formatUptime(node.uptime_seconds) : NOT_REPORTED}</td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>
