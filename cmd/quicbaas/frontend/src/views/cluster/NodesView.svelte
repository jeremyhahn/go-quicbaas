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
  /api/v1/cluster/nodes: address, Raft address, role, health, version and
  uptime. Read-only: no node is added or removed from here.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import StatCard from '../../components/core/StatCard.svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { Network, Server, CheckCircle, Crown } from 'lucide-svelte';
  import type { ClusterNodeInfo, ListClusterNodesResponse } from '@quicbaas/sdk';
  import type { Component } from 'svelte';

  const NODE_COLUMNS: Column<ClusterNodeInfo>[] = [
    { key: 'node_id', label: 'Node ID', sortable: true },
    { key: 'address', label: 'Address', sortable: true },
    { key: 'raft_address', label: 'Raft address', sortable: true },
    { key: 'is_leader', label: 'Role', sortable: true },
    { key: 'health_status', label: 'Health', sortable: true },
    { key: 'version', label: 'Version', sortable: true },
    { key: 'uptime_seconds', label: 'Uptime', sortable: true },
  ];

  const UNAVAILABLE = 'Cluster nodes are not available over this connection';

  // PageHeader takes a Svelte 5 Component; lucide-svelte's icon classes do not
  // match that signature in the type checker, though they render the same.
  const HEADER_ICON = Network as unknown as Component<{ class?: string }>;

  let result = $state<ListClusterNodesResponse | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);

  async function load(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      if (!client.cluster) { result = null; error = UNAVAILABLE; return; }
      result = await client.cluster.listNodes();
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load cluster nodes';
    } finally {
      loading = false;
    }
  }

  $effect(() => { void load(); });

  const nodes = $derived(result?.nodes ?? []);
  const healthyCount = $derived(nodes.filter((n) => n.health_status === 'healthy').length);
  const leader = $derived(nodes.find((n) => n.is_leader));

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
  <PageHeader title="Nodes" subtitle="Members of the Raft cluster" icon={HEADER_ICON} />

  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void load(); }}>Retry</button>
    </div>
  {/if}

  {#if loading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else if result}
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
      <StatCard label="Total" value={String(result.total_count)} icon={Server} />
      <StatCard label="Healthy" value={String(healthyCount)} icon={CheckCircle} variant="primary" />
      <StatCard label="Leader" value={leader ? String(leader.node_id) : 'none'} icon={Crown} />
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
              <span class="badge badge-primary badge-xs">Leader</span>
            {:else}
              <span class="text-xs text-base-content/60">Follower</span>
            {/if}
          </td>
          <td>
            <span class="badge badge-xs" class:badge-success={node.health_status === 'healthy'} class:badge-warning={node.health_status !== 'healthy'}>
              {node.health_status}
            </span>
          </td>
          <td class="font-mono text-xs">{node.version}</td>
          <td class="text-xs">{formatUptime(node.uptime_seconds)}</td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>
