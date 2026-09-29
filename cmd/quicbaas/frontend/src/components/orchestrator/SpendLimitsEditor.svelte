<script lang="ts">
  /*
    SpendLimitsEditor edits the one spend-limits shape an account, a project
    and a tenant carry (go-quicbaas#1058): sliding windows, each capping
    tokens, list-price dollars or both. The rows and their conversion live in
    lib/spendLimits.ts; this component only lays them out.
  */
  import { Plus, Trash2 } from 'lucide-svelte';
  import { SPEND_WINDOW_PRESETS, emptySpendWindowRow, type SpendWindowRow } from '../../lib/spendLimits.ts';

  let {
    idPrefix,
    rows = $bindable(),
    help = '',
  }: {
    idPrefix: string;
    rows: SpendWindowRow[];
    help?: string;
  } = $props();

  function addRow(): void {
    rows.push(emptySpendWindowRow());
  }

  function removeRow(index: number): void {
    rows.splice(index, 1);
  }

  function applyPreset(row: SpendWindowRow, name: string): void {
    const preset = SPEND_WINDOW_PRESETS.find((p) => p.name === name);
    if (!preset) return;
    row.name = preset.name;
    row.hours = preset.hours;
  }
</script>

<div>
  <div class="flex items-center justify-between">
    <span class="label-text text-xs">Spend limits</span>
    <button class="btn btn-xs btn-ghost gap-1" type="button" onclick={addRow}>
      <Plus class="h-3 w-3" />
      Add window
    </button>
  </div>
  {#if help !== ''}
    <p class="text-xs text-base-content/40 mt-1 mb-2">{help}</p>
  {/if}

  {#if rows.length === 0}
    <p class="text-xs text-base-content/30">No spend limits — nothing is capped.</p>
  {:else}
    <div class="flex flex-col gap-2">
      {#each rows as row, i (row)}
        <div class="flex items-end gap-2 flex-wrap" data-testid="spend-window-row">
          <div class="flex-1 min-w-[7rem]">
            <label class="label py-0.5" for="{idPrefix}-spend-name-{i}">
              <span class="label-text text-[0.65rem]">Window</span>
            </label>
            <input
              id="{idPrefix}-spend-name-{i}"
              type="text"
              list="{idPrefix}-spend-presets"
              class="input input-bordered input-xs w-full font-mono"
              bind:value={row.name}
              onchange={() => { applyPreset(row, row.name); }}
            />
          </div>
          <div class="w-20">
            <label class="label py-0.5" for="{idPrefix}-spend-hours-{i}">
              <span class="label-text text-[0.65rem]">Hours</span>
            </label>
            <input id="{idPrefix}-spend-hours-{i}" type="number" min="0" step="any"
              class="input input-bordered input-xs w-full tabular-nums" bind:value={row.hours} />
          </div>
          <div class="flex-1 min-w-[7rem]">
            <label class="label py-0.5" for="{idPrefix}-spend-tokens-{i}">
              <span class="label-text text-[0.65rem]">Max tokens</span>
            </label>
            <input id="{idPrefix}-spend-tokens-{i}" type="number" min="0" placeholder="0 = not capped"
              class="input input-bordered input-xs w-full tabular-nums" bind:value={row.tokens} />
          </div>
          <div class="flex-1 min-w-[6rem]">
            <label class="label py-0.5" for="{idPrefix}-spend-usd-{i}">
              <span class="label-text text-[0.65rem]">Max $ (list price)</span>
            </label>
            <input id="{idPrefix}-spend-usd-{i}" type="number" min="0" step="0.01" placeholder="0 = not capped"
              class="input input-bordered input-xs w-full tabular-nums" bind:value={row.usd} />
          </div>
          <button
            class="btn btn-xs btn-ghost btn-square text-error hover:bg-error/10"
            type="button"
            onclick={() => { removeRow(i); }}
            aria-label="Remove spend window {i + 1}"
          >
            <Trash2 class="h-3 w-3" />
          </button>
        </div>
      {/each}
    </div>
  {/if}
  <datalist id="{idPrefix}-spend-presets">
    {#each SPEND_WINDOW_PRESETS as preset (preset.name)}
      <option value={preset.name}>{preset.label}</option>
    {/each}
  </datalist>
</div>
