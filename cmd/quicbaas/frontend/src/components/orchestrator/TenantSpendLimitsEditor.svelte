<!--
  TenantSpendLimitsEditor edits the tenant's OWN spend limits
  (go-quicbaas#1058): sliding windows over every turn charged to the tenant,
  pooled or not. A spent window defers admission for the whole tenant until it
  frees, which is why saving says so.

  It reads the stored limits rather than rebuilding them from the report's
  window usage, so what is edited is exactly what is enforced. Rows and their
  conversion are lib/spendLimits.ts's; this component lays them out.
-->
<script lang="ts">
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
    onclose,
    onsaved,
  }: {
    tenantID: string;
    onclose: () => void;
    onsaved: () => void;
  } = $props();

  let rows = $state<SpendWindowRow[]>([]);
  let loaded = $state(false);
  let error = $state('');
  let saving = $state(false);

  // The tenant the rows were read for. Save is refused for any other, so a
  // tenant switch can never write one tenant's limits onto another.
  let readFor = $state('');

  $effect(() => {
    const tenant = tenantID;
    loaded = false;
    rows = [];
    error = '';
    void load(tenant);
  });

  async function load(tenant: string): Promise<void> {
    try {
      const client = await getSDKClient();
      const policy = await client.orchestrator.getTenantSpendPolicy({ tenant_id: tenant });
      if (tenant !== tenantID) return;
      rows = spendRowsFrom(policy.limits);
      readFor = tenant;
      loaded = true;
    } catch (err: unknown) {
      if (tenant !== tenantID) return;
      error = spendErrorMessage(err, 'the tenant spend limits');
    }
  }

  async function save(): Promise<void> {
    if (!loaded || readFor !== tenantID) return;
    const problem = spendRowsProblem(rows);
    if (problem !== null) {
      error = problem;
      return;
    }
    saving = true;
    error = '';
    try {
      const client = await getSDKClient();
      await client.orchestrator.setTenantSpendPolicy({ tenant_id: tenantID, limits: spendLimitsFromRows(rows) });
      showToast('success', rows.length === 0 ? 'Tenant spend limits removed' : 'Tenant spend limits saved');
      onsaved();
    } catch (err: unknown) {
      error = spendErrorMessage(err, 'the tenant spend limits');
    } finally {
      saving = false;
    }
  }
</script>

<div class="mt-3 space-y-3 rounded-field border border-base-300/80 bg-base-100 px-4 py-3" data-testid="tenant-limits-editor">
  {#if !loaded && error === ''}
    <div class="flex items-center gap-2 text-sm text-base-content/60">
      <span class="loading loading-spinner loading-xs text-primary"></span> Loading limits…
    </div>
  {:else if loaded}
    <SpendLimitsEditor
      idPrefix="tenant-limits"
      bind:rows
      help="Every turn charged to the tenant counts, pooled or local. A spent window holds new work for the whole tenant until it frees."
    />
  {/if}
  {#if error !== ''}
    <p role="alert" class="text-sm text-error" data-testid="tenant-limits-error">{error}</p>
  {/if}
  <div class="flex justify-end gap-2">
    <button class="btn btn-sm btn-ghost" type="button" onclick={onclose}>Cancel</button>
    <button class="btn btn-sm btn-primary" type="button" disabled={!loaded || saving} onclick={() => { void save(); }}>
      {saving ? 'Saving…' : 'Save limits'}
    </button>
  </div>
</div>
