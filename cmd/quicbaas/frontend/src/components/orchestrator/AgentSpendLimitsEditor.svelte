<!--
  AgentSpendLimitsEditor edits one tenant Agent's OWN spend limits: sliding
  windows over every turn of every session that ran as the Agent. A spent
  window passes a pool's work to its next member, and refuses a spawn naming
  the Agent alone until it frees.

  It reads the stored Agent and writes it back whole with only its limits
  changed, so saving here can never drop another field of the Agent. Rows and
  their conversion are lib/spendLimits.ts's, shared with projects, accounts
  and the tenant.
-->
<script lang="ts">
  import type { Agent } from '@quicbaas/sdk';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import SpendLimitsEditor from './SpendLimitsEditor.svelte';
  import { spendErrorMessage } from '../../lib/cost.ts';
  import {
    spendLimitsFromRows,
    spendRowsFrom,
    spendRowsProblem,
    type SpendWindowRow,
  } from '../../lib/spendLimits.ts';

  let {
    tenantID,
    agentID,
    onclose,
    onsaved,
  }: {
    tenantID: string;
    agentID: string;
    onclose: () => void;
    onsaved: () => void;
  } = $props();

  let rows = $state<SpendWindowRow[]>([]);
  let agent = $state<Agent | null>(null);
  let error = $state('');
  let saving = $state(false);

  // The tenant and Agent the rows were read for. Save is refused for any
  // other, so a switch can never write one Agent's limits onto another.
  let readFor = $state('');

  $effect(() => {
    const key = `${tenantID}/${agentID}`;
    agent = null;
    rows = [];
    error = '';
    void load(key, tenantID, agentID);
  });

  async function load(key: string, tenant: string, id: string): Promise<void> {
    try {
      const client = await getSDKClient();
      const stored = await client.agents.getAgent(id, tenant);
      if (key !== `${tenantID}/${agentID}`) return;
      agent = stored;
      rows = spendRowsFrom(stored.spend);
      readFor = key;
    } catch (err: unknown) {
      if (key !== `${tenantID}/${agentID}`) return;
      error = spendErrorMessage(err, `Agent ${id}'s spend limits`);
    }
  }

  async function save(): Promise<void> {
    if (agent === null || readFor !== `${tenantID}/${agentID}`) return;
    const problem = spendRowsProblem(rows);
    if (problem !== null) {
      error = problem;
      return;
    }
    saving = true;
    error = '';
    try {
      const client = await getSDKClient();
      await client.agents.updateAgent(agentID, { ...agent, spend: spendLimitsFromRows(rows) }, tenantID);
      showToast('success', rows.length === 0 ? `Agent ${agentID}'s spend limits removed` : `Agent ${agentID}'s spend limits saved`);
      onsaved();
    } catch (err: unknown) {
      error = spendErrorMessage(err, `Agent ${agentID}'s spend limits`);
    } finally {
      saving = false;
    }
  }
</script>

<div class="mt-3 space-y-3 rounded-field border border-base-300/80 bg-base-100 px-4 py-3" data-testid="agent-limits-editor">
  {#if agent === null && error === ''}
    <div class="flex items-center gap-2 text-sm text-base-content/60">
      <span class="loading loading-spinner loading-xs text-primary"></span> Loading limits…
    </div>
  {:else if agent !== null}
    <SpendLimitsEditor
      idPrefix="agent-limits"
      bind:rows
      help="Every turn of every session that runs as this Agent counts, pooled or local. A spent window passes a pool's work to its next Agent, and holds work that names this Agent alone until it frees."
    />
  {/if}
  {#if error !== ''}
    <p role="alert" class="text-sm text-error" data-testid="agent-limits-error">{error}</p>
  {/if}
  <div class="flex justify-end gap-2">
    <button class="btn btn-sm btn-ghost" type="button" onclick={onclose}>Cancel</button>
    <button class="btn btn-sm btn-primary" type="button" disabled={agent === null || saving} onclick={() => { void save(); }}>
      {saving ? 'Saving…' : 'Save limits'}
    </button>
  </div>
</div>
