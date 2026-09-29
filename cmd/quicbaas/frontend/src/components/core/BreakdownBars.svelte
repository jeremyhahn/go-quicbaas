<!--
  BreakdownBars shows how a set of records splits across a handful of states:
  one row per state with its name, a bar, and the count, the bars scaled to
  the largest count. Each bar takes its colour from the state's status tone
  (the same colour StatusBadge uses for that state), so "failed" is the same
  red here as in every table.

  It replaces a doughnut and a bar chart on the dashboard. A doughnut
  cannot be compared across slices at a glance and needs a legend; a list
  of labelled bars reads top to bottom and needs no legend.
-->
<script lang="ts">
  import { themeStore } from '../../stores/theme.svelte.ts';
  import { STATUS_TONES } from '../../lib/theme/tokens.ts';
  import { statusToVariant } from './statusVariant.ts';
  import type { StatusVariant } from './StatusBadge.svelte';

  export interface BreakdownRow {
    /** label is the state as the operator reads it. */
    label: string;
    /** count is how many records are in that state. */
    count: number;
  }

  let {
    rows,
    ariaLabel,
  }: {
    rows: readonly BreakdownRow[];
    ariaLabel: string;
  } = $props();

  const TONE: Readonly<Record<StatusVariant, keyof (typeof STATUS_TONES)['light']>> = {
    success: 'success',
    error: 'error',
    warning: 'warning',
    info: 'info',
    primary: 'primary',
    ghost: 'neutral',
    neutral: 'neutral',
  };

  const largest = $derived(rows.reduce((max, row) => Math.max(max, row.count), 0));
  const tones = $derived(STATUS_TONES[themeStore.theme === 'light' ? 'light' : 'dark']);

  /** share is a row's bar length as a percentage of the largest count. */
  function share(count: number): number {
    return largest === 0 ? 0 : (count / largest) * 100;
  }
</script>

<ul class="space-y-2.5" aria-label={ariaLabel}>
  {#each rows as row (row.label)}
    <li
      class="grid grid-cols-[minmax(5.5rem,8rem)_minmax(0,1fr)_2.5rem] items-center gap-3 text-sm"
      class:opacity-60={row.count === 0}
      data-testid="breakdown-row"
    >
      <span class="truncate">{row.label}</span>
      <span class="h-2 overflow-hidden rounded-full bg-base-300/70" aria-hidden="true">
        <span
          class="block h-full rounded-full"
          style:width="{share(row.count)}%"
          style:background-color={tones[TONE[statusToVariant(row.label)]].fg}
          data-testid="breakdown-bar"
        ></span>
      </span>
      <span class="text-right font-medium tabular-nums">{row.count}</span>
    </li>
  {/each}
</ul>
