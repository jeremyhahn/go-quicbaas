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
  NodeMembershipDialog adds a node to a shard (mode "add",
  POST /api/v1/cluster/nodes/) or replaces an existing node with another
  (mode "replace", PUT /api/v1/cluster/nodes/{nodeId}/replace). It checks the
  required and numeric fields before it calls, shows a failed call inline,
  and calls ondone after a success so the caller can reload its list. An
  answer for a session that has since been replaced is dropped.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import type { AddNodeRequest, ReplaceNodeRequest } from '@quicbaas/sdk';

  interface Props {
    mode: 'add' | 'replace';
    /** nodeId is the node being replaced; used in mode "replace". */
    nodeId?: number;
    onclose: () => void;
    ondone: () => void;
  }

  let { mode, nodeId, onclose, ondone }: Props = $props();

  const UNAVAILABLE = 'Cluster node membership is not available over this connection';

  let shardId = $state('');
  let newNodeId = $state('');
  let address = $state('');
  let identity = $state('');
  let submitting = $state(false);
  let error = $state('');

  const title = $derived(mode === 'add' ? 'Add node' : `Replace node ${nodeId ?? ''}`);

  /** parseId reads a required whole number, or returns the error to show. */
  function parseId(raw: string, label: string): number | string {
    const v = raw.trim();
    if (v === '') return `${label} is required`;
    if (!/^\d+$/.test(v)) return `${label} must be a whole number`;
    return Number(v);
  }

  async function submit(): Promise<void> {
    error = '';
    const id = parseId(newNodeId, mode === 'add' ? 'Node ID' : 'Replacement node ID');
    if (typeof id === 'string') { error = id; return; }
    const addr = address.trim();
    if (addr === '') { error = `${mode === 'add' ? 'Address' : 'Replacement address'} is required`; return; }
    const ident = identity.trim();

    let addReq: AddNodeRequest | null = null;
    let replaceReq: ReplaceNodeRequest | null = null;
    if (mode === 'add') {
      const shard = parseId(shardId, 'Shard ID');
      if (typeof shard === 'string') { error = shard; return; }
      addReq = { shard_id: shard, node_id: id, address: addr };
      if (ident !== '') addReq.identity = ident;
    } else {
      if (nodeId === undefined) { error = 'No node to replace'; return; }
      replaceReq = { replacement_node_id: id, replacement_address: addr };
      if (ident !== '') replaceReq.replacement_identity = ident;
    }

    const forSession = authStore.sessionKey;
    submitting = true;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      const nodes = client.clusterNodes;
      if (!nodes) { error = UNAVAILABLE; return; }
      let message: string;
      if (addReq) {
        const resp = await nodes.addNode(addReq);
        message = resp.message || `Node ${addReq.node_id} added to shard ${addReq.shard_id}.`;
      } else if (replaceReq && nodeId !== undefined) {
        const resp = await nodes.replaceNode(nodeId, replaceReq);
        message = resp.message || `Node ${nodeId} replaced by node ${replaceReq.replacement_node_id}.`;
      } else {
        return;
      }
      if (forSession !== authStore.sessionKey) return;
      showToast({ message, type: 'success' });
      ondone();
    } catch (err: unknown) {
      if (forSession !== authStore.sessionKey) return;
      error = err instanceof Error ? err.message : String(err);
    } finally {
      if (forSession === authStore.sessionKey) submitting = false;
    }
  }
</script>

<dialog class="modal modal-open" open aria-label={title}>
  <div class="modal-box">
    <h3 class="text-base font-semibold mb-3">{title}</h3>
    <form class="space-y-2" onsubmit={(e) => { e.preventDefault(); void submit(); }}>
      {#if mode === 'add'}
        <label class="form-control">
          <span class="label-text text-xs">Shard ID</span>
          <input class="input input-sm input-bordered" type="text" inputmode="numeric" bind:value={shardId} />
        </label>
      {/if}
      <label class="form-control">
        <span class="label-text text-xs">{mode === 'add' ? 'Node ID' : 'Replacement node ID'}</span>
        <input class="input input-sm input-bordered" type="text" inputmode="numeric" bind:value={newNodeId} />
      </label>
      <label class="form-control">
        <span class="label-text text-xs">{mode === 'add' ? 'Address' : 'Replacement address'}</span>
        <input class="input input-sm input-bordered font-mono" type="text" placeholder="host:port" bind:value={address} />
      </label>
      <label class="form-control">
        <span class="label-text text-xs">{mode === 'add' ? 'Identity' : 'Replacement identity'}</span>
        <input class="input input-sm input-bordered" type="text" placeholder="optional" bind:value={identity} />
      </label>

      {#if error}
        <div class="alert alert-error text-sm" role="alert" data-testid="membership-error">{error}</div>
      {/if}

      <div class="modal-action">
        <button type="button" class="btn btn-ghost" onclick={onclose}>Cancel</button>
        <button type="submit" class="btn btn-primary" disabled={submitting}>
          {#if submitting}<span class="loading loading-spinner loading-xs"></span>{/if}
          {mode === 'add' ? 'Add' : 'Replace'}
        </button>
      </div>
    </form>
  </div>
  <button class="modal-backdrop" onclick={onclose} aria-label="Close node dialog"></button>
</dialog>
