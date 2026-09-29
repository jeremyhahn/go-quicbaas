<!--
  CostSettingsPanel is the tenant's runtime cost settings (go-quicbaas#1058):
  the electricity rate and each local engine's device draw, which every local
  turn is costed from:

    power_watts × turn seconds / 3.6e6 kWh × $/kWh

  It shows the rate IN FORCE and where it came from, so an empty field reads
  as "inheriting $0.12 from the default" rather than as "free". Saving
  replaces the tenant's settings; clearing a field inherits the configuration.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { Plus, Trash2 } from 'lucide-svelte';
  import type { CostSettings } from '@quicbaas/sdk';
  import {
    type CostSettingsForm,
    costSettingsForm,
    costSettingsRequest,
    electricitySourceLabel,
    spendErrorMessage,
  } from '../../lib/cost.ts';

  let { tenantID }: { tenantID: string } = $props();

  let settings = $state<CostSettings | null>(null);
  let form = $state<CostSettingsForm>({ price: '', engines: [] });
  let error = $state('');
  let saved = $state(false);
  let saving = $state(false);

  // The tenant the panel shows. A reply for a tenant the operator has left is
  // discarded rather than shown as this tenant's settings.
  let shownTenant = '';

  // A TENANT SWITCH INVALIDATES THE FORM AT ONCE. Keeping the previous
  // tenant's values while the next tenant loads would let Save replace
  // tenant B's settings with tenant A's (go-quicbaas#1058 review); the form
  // is cleared, and Save stays disabled until this tenant's read succeeds.
  $effect(() => {
    const tenant = tenantID;
    shownTenant = tenant;
    settings = null;
    form = { price: '', engines: [] };
    error = '';
    saved = false;
    void load(tenant);
  });

  // Save is possible only over settings read for the tenant on screen.
  const canSave = $derived(settings !== null && settings.settings.tenant_id === tenantID && !saving);

  async function load(tenant: string): Promise<void> {
    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.getCostSettings({ tenant_id: tenant });
      if (tenant !== shownTenant) return;
      settings = resp;
      form = costSettingsForm(resp);
    } catch (err: unknown) {
      if (tenant !== shownTenant) return;
      error = spendErrorMessage(err, 'cost settings');
    }
  }

  async function save(): Promise<void> {
    if (!canSave) return;
    saved = false;
    const built = costSettingsRequest(tenantID, form);
    if ('error' in built) {
      error = built.error;
      return;
    }
    saving = true;
    error = '';
    const tenant = tenantID;
    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.setCostSettings(built.request);
      if (tenant !== shownTenant) return;
      settings = resp;
      form = costSettingsForm(resp);
      saved = true;
    } catch (err: unknown) {
      if (tenant !== shownTenant) return;
      error = spendErrorMessage(err, 'cost settings');
    } finally {
      saving = false;
    }
  }

  function addEngine(): void {
    form.engines.push({ engine: '', watts: '' });
  }

  function removeEngine(index: number): void {
    form.engines.splice(index, 1);
  }

  const inherited = $derived(
    Object.entries(settings?.engine_power_watts ?? {})
      .filter(([engine]) => !(engine in (settings?.settings.engine_power_watts ?? {})))
      .sort(([a], [b]) => a.localeCompare(b)),
  );
</script>

<!--
  Two columns on a wide screen: what is in force (and why) on the left, the
  form that changes it on the right. The rate reads as a single figure with
  its unit; each engine draw is a row with its unit beside the field, so
  "35" is never ambiguous about being watts.
-->
<section class="card rounded-box bg-base-200" data-testid="cost-settings" aria-labelledby="cost-settings-title">
  <div class="card-body grid gap-6 p-5 lg:grid-cols-[minmax(0,2fr)_minmax(0,3fr)] lg:gap-10">
    <div class="space-y-3">
      <h3 id="cost-settings-title" class="text-[0.9375rem] font-semibold">Local inference cost</h3>
      <p class="max-w-prose text-sm text-base-content/60">
        A local turn is costed from the energy its device drew × the electricity price. Where the
        fleet node running it reports device telemetry, that energy is measured; otherwise it is
        estimated as the engine's draw below × the turn's duration. A model priced per token in the
        price table keeps its per-token price.
      </p>

      {#if settings}
        <div class="rounded-field border border-base-300/80 bg-base-100 px-4 py-3" data-testid="cost-settings-rate">
          <div class="text-xs font-medium text-base-content/60">Electricity in force</div>
          <div class="mt-0.5">
            <strong class="display text-2xl tabular-nums">${settings.electricity.price_per_kwh}/kWh</strong>
          </div>
          <div class="mt-0.5 text-xs text-base-content/60">({electricitySourceLabel(settings.electricity.source)})</div>
        </div>
      {/if}
    </div>

    <div class="space-y-5">
      <label class="block max-w-xs" for="cost-settings-price">
        <span class="mb-1.5 block text-sm font-medium">Electricity price ($/kWh)</span>
        <input
          id="cost-settings-price"
          type="text"
          inputmode="decimal"
          class="input input-sm w-full bg-base-100 tabular-nums"
          placeholder="inherit"
          bind:value={form.price}
        />
      </label>

      <div class="space-y-2">
        <div class="flex items-center justify-between gap-3">
          <span class="text-sm font-medium">Engine draw while serving</span>
          <button class="btn btn-ghost btn-xs gap-1 text-primary" type="button" onclick={addEngine}>
            <Plus class="h-3.5 w-3.5" /> Engine
          </button>
        </div>
        {#if form.engines.length > 0}
          <div class="grid grid-cols-[minmax(0,1fr)_7.5rem_2rem] items-center gap-x-2 gap-y-2">
            <span class="text-xs text-base-content/60">Engine name</span>
            <span class="text-xs text-base-content/60">Watts</span>
            <span></span>
            {#each form.engines as row, i (i)}
              <input
                type="text"
                class="input input-sm w-full bg-base-100 font-mono"
                placeholder="openvino"
                aria-label={`Engine ${i + 1}`}
                bind:value={row.engine}
              />
              <label class="input input-sm w-full bg-base-100">
                <input
                  type="text"
                  inputmode="decimal"
                  class="tabular-nums"
                  placeholder="0"
                  aria-label={`Watts for engine ${i + 1}`}
                  bind:value={row.watts}
                />
                <span class="text-xs text-base-content/60" aria-hidden="true">W</span>
              </label>
              <button
                class="btn btn-ghost btn-sm btn-square text-error"
                type="button"
                aria-label={`Remove engine ${i + 1}`}
                onclick={() => removeEngine(i)}
              >
                <Trash2 class="h-3.5 w-3.5" />
              </button>
            {/each}
          </div>
        {:else}
          <p class="text-xs text-base-content/60">This tenant sets no engine draw of its own.</p>
        {/if}
        {#if inherited.length > 0}
          <p class="text-xs text-base-content/60" data-testid="cost-settings-inherited">
            Inherited: {inherited.map(([engine, watts]) => `${engine} ${watts} W`).join(' · ')}
          </p>
        {/if}
      </div>

      {#if error !== ''}
        <div role="alert" class="alert alert-error py-2 text-sm">{error}</div>
      {/if}

      <div class="flex items-center gap-3 border-t border-base-300/80 pt-4">
        <button class="btn btn-primary btn-sm min-w-20" type="button" disabled={!canSave} onclick={save}>
          {saving ? 'Saving…' : 'Save'}
        </button>
        {#if saved}
          <span class="text-sm text-success" role="status">Saved.</span>
        {/if}
      </div>
    </div>
  </div>
</section>
