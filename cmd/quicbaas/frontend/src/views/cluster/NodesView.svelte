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
  check. The "Non-leader only" filter lists through
  GET /api/v1/cluster/nodes/non-leader instead. Membership changes go through
  client.clusterNodes: Add node and Replace open NodeMembershipDialog, and
  Remove asks for the shard and a confirmation before
  DELETE /api/v1/cluster/nodes/{nodeId}. When the connection has no
  clusterNodes client the actions are disabled and the list still shows.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import StatCard from '../../components/core/StatCard.svelte';
  import NodeMembershipDialog from '../../components/cluster/NodeMembershipDialog.svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import { Network, Server, Crown, Plus, Trash2, Replace } from 'lucide-svelte';
  import type { ClusterNodeInfo, ListClusterNodesResponse } from '@quicbaas/sdk';
  import type { Component } from 'svelte';

  const NODE_COLUMNS: Column<ClusterNodeInfo>[] = [
    { key: 'node_id', label: 'Node ID', sortable: true },
    { key: 'address', label: 'Address', sortable: true },
    { key: 'raft_address', label: 'Raft address', sortable: true },
    { key: 'is_leader', label: 'Role', sortable: true },
    { key: 'version', label: 'Version', sortable: true },
    { key: 'uptime_seconds', label: 'Uptime', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  const UNAVAILABLE = 'Cluster nodes are not available over this connection';
  const MEMBERSHIP_UNAVAILABLE = 'Node membership changes are not available over this connection';
  const NOT_REPORTED = 'not reported';

  // PageHeader takes a Svelte 5 Component; lucide-svelte's icon classes do not
  // match that signature in the type checker, though they render the same.
  const HEADER_ICON = Network as unknown as Component<{ class?: string }>;

  let result = $state<ListClusterNodesResponse | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let nonLeaderOnly = $state(false);
  let membershipAvailable = $state(false);

  // Membership actions.
  let dialog = $state<{ mode: 'add' | 'replace'; nodeId?: number } | null>(null);
  let nodeToRemove = $state<ClusterNodeInfo | null>(null);
  let removeShardId = $state('');
  let removeError = $state('');
  let removing = $state(false);

  async function load(forSession: string, nonLeader: boolean): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      membershipAvailable = !!client.clusterNodes;
      let resp: ListClusterNodesResponse;
      if (nonLeader && client.clusterNodes) {
        resp = await client.clusterNodes.listNonLeaderNodes();
      } else {
        if (!client.cluster) { result = null; error = UNAVAILABLE; return; }
        resp = await client.cluster.listNodes();
      }
      // An answer for a session that has since been replaced belongs to the
      // principal who asked for it, not to whoever is signed in now.
      if (forSession !== authStore.sessionKey || nonLeader !== nonLeaderOnly) return;
      result = resp;
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      error = e instanceof Error ? e.message : 'Failed to load cluster nodes';
    } finally {
      if (forSession === authStore.sessionKey) loading = false;
    }
  }

  function reload(): void {
    void load(authStore.sessionKey, nonLeaderOnly);
  }

  $effect(() => {
    // Read synchronously so the effect re-runs when the session or the filter
    // changes, and clear first so a new principal never sees the previous
    // one's nodes.
    const forSession = authStore.sessionKey;
    const nonLeader = nonLeaderOnly;
    result = null;
    error = null;
    dialog = null;
    nodeToRemove = null;
    void load(forSession, nonLeader);
  });

  function openRemove(node: ClusterNodeInfo): void {
    nodeToRemove = node;
    removeShardId = '';
    removeError = '';
  }

  async function confirmRemove(): Promise<void> {
    const node = nodeToRemove;
    if (!node) return;
    const raw = removeShardId.trim();
    if (!/^\d+$/.test(raw)) {
      removeError = raw === '' ? 'Shard ID is required' : 'Shard ID must be a whole number';
      return;
    }
    const shardId = Number(raw);
    const forSession = authStore.sessionKey;
    removing = true;
    removeError = '';
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      if (!client.clusterNodes) { removeError = MEMBERSHIP_UNAVAILABLE; return; }
      const resp = await client.clusterNodes.removeNode(node.node_id, shardId);
      if (forSession !== authStore.sessionKey) return;
      showToast({ message: resp.message || `Node ${node.node_id} removed from shard ${shardId}.`, type: 'success' });
      nodeToRemove = null;
      reload();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      removeError = e instanceof Error ? e.message : 'Failed to remove the node';
    } finally {
      if (forSession === authStore.sessionKey) removing = false;
    }
  }

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

  <div class="flex flex-wrap items-center gap-3">
    <button
      class="btn btn-sm btn-primary"
      onclick={() => { dialog = { mode: 'add' }; }}
      disabled={!membershipAvailable}
      title={membershipAvailable ? 'Add a node to a shard' : MEMBERSHIP_UNAVAILABLE}
    >
      <Plus class="h-4 w-4" /> Add node
    </button>
    <label class="label cursor-pointer gap-2">
      <input type="checkbox" class="toggle toggle-sm" bind:checked={nonLeaderOnly} disabled={!membershipAvailable} />
      <span class="label-text text-sm">Non-leader only</span>
    </label>
  </div>

  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={reload}>Retry</button>
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
          <td class="text-right whitespace-nowrap">
            <button
              class="btn btn-xs btn-ghost"
              onclick={() => { dialog = { mode: 'replace', nodeId: node.node_id }; }}
              disabled={!membershipAvailable}
              aria-label={`Replace node ${node.node_id}`}
              title={`Replace node ${node.node_id}`}
            >
              <Replace class="h-3.5 w-3.5" />
            </button>
            <button
              class="btn btn-xs btn-ghost text-error"
              onclick={() => openRemove(node)}
              disabled={!membershipAvailable}
              aria-label={`Remove node ${node.node_id}`}
              title={`Remove node ${node.node_id}`}
            >
              <Trash2 class="h-3.5 w-3.5" />
            </button>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>

{#if dialog}
  <NodeMembershipDialog
    mode={dialog.mode}
    nodeId={dialog.nodeId}
    onclose={() => { dialog = null; }}
    ondone={() => { dialog = null; reload(); }}
  />
{/if}

<!-- Remove node confirm dialog -->
{#if nodeToRemove}
  <dialog class="modal modal-open" open aria-label="Remove node confirmation">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-2">Remove node?</h3>
      <p class="text-sm text-base-content/70 mb-3">
        Remove node <span class="font-mono font-semibold">{nodeToRemove.node_id}</span>
        (<span class="font-mono">{nodeToRemove.address}</span>) from a shard? Its replica of that shard is dropped.
      </p>
      <label class="form-control">
        <span class="label-text text-xs">Shard ID</span>
        <input class="input input-sm input-bordered" type="text" inputmode="numeric" bind:value={removeShardId} />
      </label>
      {#if removeError}
        <div class="alert alert-error text-sm mt-2" role="alert" data-testid="remove-error">{removeError}</div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { nodeToRemove = null; }}>Cancel</button>
        <button class="btn btn-error" onclick={() => { void confirmRemove(); }} disabled={removing}>
          {#if removing}<span class="loading loading-spinner loading-xs"></span>{/if}
          Remove
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { nodeToRemove = null; }} aria-label="Close remove dialog"></button>
  </dialog>
{/if}
