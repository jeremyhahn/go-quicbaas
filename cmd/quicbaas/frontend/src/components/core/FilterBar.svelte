<!--
  FilterBar is the faceted narrowing above every list in this app.

  What the operator sees, and why
  ──────────────────────────────
  A chip group per field, each chip carrying the COUNT of records behind it,
  so the question "how many jobs failed" is answered before anything is
  clicked. That is the whole point of faceting over a select box: the control
  tells you what is there, and a value with nothing behind it reads `0`
  instead of disappearing and making the question unaskable.

  Above the groups, the current selection is repeated as removable chips. A
  filter you can only switch off by finding the control that set it is a trap
  on a page with five groups; a filter you cannot SEE is worse, because the
  list is lying about how much there is and nothing on screen says so.

  Chips or a picker
  ─────────────────
  A bounded vocabulary — a status, a mode, a backend — is chips, because
  seeing all of it at once is the point. An unbounded one — a project id, an
  agent id — is a picker, because a chip per project is a thousand chips. The
  decision is `usesChips` in lib/filters.ts and a field may override it.

  One row per chip field, and a lid on how wide any one of them gets
  ────────────────────────────────────────────────────────────────
  A list with a large enumeration — the job lifecycle is thirteen states —
  used to spill thirteen chips into the SAME flex-wrap run as every other
  field, so "State" bled into "Runner" bled into the project picker with
  nothing marking where one question ended and the next began. Two things
  fix that without touching the URL grammar or the loader contract a single
  field's chips still travel as: each chip field gets its OWN row inside one
  shared panel, with a fixed label column so every row's chips start on the
  same edge, and wrapping happens within a field, never across two. The
  pickers (project, agent) are one control each, so they share a single
  final row rather than spending a full-width row apiece; and a field whose
  vocabulary exceeds VISIBLE_LIMIT shows the first few plus a "+N more"
  disclosure rather than every value at once. A value the operator has
  actually selected is never among the hidden ones — collapsing a filter
  must never hide evidence that it is narrowing the list.

  The counts come from the answer
  ───────────────────────────────
  `facets` is what the endpoint counted over the set its search matched. An
  endpoint that counts nothing sends none, and the chips fall back to the
  declared vocabulary WITHOUT counts rather than vanishing — the same
  graceful degradation DataTable applies to `total`. A list the table pages
  itself has its facets counted locally instead, and the two are
  indistinguishable from here.
-->
<script lang="ts">
  import { X, ChevronDown, ChevronUp } from 'lucide-svelte';
  import { themeStore } from '../../stores/theme.svelte.ts';
  import { STATUS_TONES } from '../../lib/theme/tokens.ts';
  import {
    activeFilters,
    activeCount,
    clearAll,
    clearField,
    clearValue,
    fieldOptions,
    isSelected,
    selected,
    setValue,
    toggleValue,
    usesChips,
    type Facets,
    type FilterField,
    type FilterValues,
  } from '../../lib/filters.ts';

  let {
    fields,
    values,
    facets,
    onchange,
  }: {
    /** fields declares the facets this list can be narrowed by. */
    fields: readonly FilterField[];
    /** values is the current selection, keyed by field. */
    values: FilterValues;
    /** facets is what the answer counted, absent when nobody counted. */
    facets?: Facets;
    /** onchange hands the whole next selection back to the owner. */
    onchange: (next: FilterValues) => void;
  } = $props();

  /**
   * VISIBLE_LIMIT is how many chips a field shows before offering "+N more".
   * Eight sits comfortably in one or two wrapped rows at the narrowest width
   * this app targets (1024px) for every declared vocabulary except the job
   * lifecycle (13 states) and the task lifecycle (7, so it never truncates).
   */
  const VISIBLE_LIMIT = 8;

  const active = $derived(activeFilters(fields, values, facets));
  const anyActive = $derived(activeCount(values) > 0);
  const tone = $derived(STATUS_TONES[themeStore.theme === 'light' ? 'light' : 'dark'].primary);

  /**
   * expanded tracks which fields have had their "+N more" disclosure opened.
   * Collapsed options are hidden with CSS (not omitted from the DOM), so a
   * caller that already has a `data-testid="filter-chip-…"` handle on a
   * value past VISIBLE_LIMIT — from a hand-edited link, or a test — can
   * still find and click it; only the VISUAL row height changes.
   */
  let expanded = $state<Record<string, boolean>>({});

  const chipFields = $derived(fields.filter((field) => usesChips(field, optionsFor(field).length)));
  const pickerFields = $derived(fields.filter((field) => !usesChips(field, optionsFor(field).length)));

  function optionsFor(field: FilterField) {
    return fieldOptions(field, facets);
  }

  function pick(field: FilterField, value: string): void {
    onchange(toggleValue(values, field, value));
  }

  function choose(field: FilterField, value: string): void {
    onchange(setValue(values, field.key, value));
  }

  function toggleExpanded(key: string): void {
    expanded = { ...expanded, [key]: !expanded[key] };
  }

  /** hiddenBeyondLimit is how many of a field's options a collapsed row is holding back. */
  function hiddenBeyondLimit(field: FilterField, options: readonly { value: string }[]): number {
    if (options.length <= VISIBLE_LIMIT) return 0;
    let hidden = 0;
    for (let i = VISIBLE_LIMIT; i < options.length; i++) {
      const option = options[i];
      if (option !== undefined && !isSelected(values, field.key, option.value)) hidden += 1;
    }
    return hidden;
  }

  /** collapsedAway says whether ONE option is presently hidden behind "+N more". */
  function collapsedAway(field: FilterField, index: number, value: string): boolean {
    if (expanded[field.key]) return false;
    if (index < VISIBLE_LIMIT) return false;
    return !isSelected(values, field.key, value);
  }
</script>

{#snippet clearFieldButton(field: FilterField)}
  <button
    type="button"
    class="btn btn-ghost btn-xs btn-circle"
    aria-label="Clear {field.label} filter"
    title="Clear {field.label} filter"
    data-testid="filter-clear-{field.key}"
    onclick={() => onchange(clearField(values, field.key))}
  >
    <X class="h-3.5 w-3.5" />
  </button>
{/snippet}

<div class="space-y-2" data-testid="filter-bar">
  {#if anyActive}
    <!-- The selection, repeated where it cannot be missed and can be undone. -->
    <div class="flex flex-wrap items-center gap-1.5" data-testid="active-filters">
      <span class="mr-0.5 text-xs font-medium text-base-content/60">Filtered by</span>
      {#each active as chip (`${chip.field}:${chip.value}`)}
        <span
          class="inline-flex h-6 items-center gap-1 rounded-full pl-2.5 pr-1 text-xs font-medium leading-none"
          style:background-color={tone.bg}
          style:color={tone.fg}
        >
          <span>{chip.fieldLabel}: {chip.valueLabel}</span>
          <button
            type="button"
            class="inline-flex h-4 w-4 items-center justify-center rounded-full hover:bg-black/10"
            style:color={tone.fg}
            aria-label="Remove filter {chip.fieldLabel} {chip.valueLabel}"
            title="Remove this filter"
            data-testid="active-filter-remove-{chip.field}-{chip.value}"
            onclick={() => onchange(clearValue(values, chip.field, chip.value))}
          >
            <X class="h-3 w-3" />
          </button>
        </span>
      {/each}
      <button
        type="button"
        class="btn btn-ghost btn-xs text-base-content/70"
        data-testid="filter-clear-all"
        onclick={() => onchange(clearAll())}
      >Clear all</button>
    </div>
  {/if}

  <div class="divide-y divide-base-300/70 rounded-box border border-base-300/80 bg-base-200/60">
    {#each chipFields as field (field.key)}
      {@const options = optionsFor(field)}
      {@const chosen = selected(values, field.key)}
      {@const hidden = hiddenBeyondLimit(field, options)}
      <div
        class="flex flex-col gap-1.5 px-3 py-2 sm:flex-row sm:items-center sm:gap-3"
        data-testid="filter-field-{field.key}"
      >
        <span class="w-20 shrink-0 text-xs font-medium text-base-content/60">{field.label}</span>

        <div class="flex flex-1 flex-wrap items-center gap-1.5" role="group" aria-label="Filter by {field.label}">
          {#each options as option, index (option.value)}
            {@const on = isSelected(values, field.key, option.value)}
            <button
              type="button"
              class="h-7 items-center gap-1.5 rounded-full border px-3 text-xs font-medium leading-none transition-colors hover:border-base-content/30 {collapsedAway(field, index, option.value) ? 'hidden' : 'inline-flex'}"
              class:border-transparent={on}
              class:border-base-300={!on}
              class:bg-base-100={!on}
              class:text-base-content={!on}
              style:background-color={on ? tone.bg : undefined}
              style:color={on ? tone.fg : undefined}
              aria-pressed={on}
              title="Filter by {field.label}: {option.label}"
              data-testid="filter-chip-{field.key}-{option.value}"
              onclick={() => pick(field, option.value)}
            >
              <span>{option.label}</span>
              {#if option.count !== null}
                <span
                  class="tabular-nums {on ? 'opacity-80' : 'text-base-content/60'}"
                  data-testid="filter-count-{field.key}-{option.value}"
                >{option.count}</span>
              {/if}
            </button>
          {/each}
          {#if options.length === 0}
            <span class="text-xs italic text-base-content/50">none</span>
          {/if}
          {#if hidden > 0}
            <button
              type="button"
              class="inline-flex h-7 items-center gap-0.5 rounded-full px-2 text-xs font-medium text-primary hover:underline"
              data-testid="filter-more-{field.key}"
              onclick={() => toggleExpanded(field.key)}
            >
              {#if expanded[field.key]}
                Show less <ChevronUp class="h-3 w-3" />
              {:else}
                {hidden} more <ChevronDown class="h-3 w-3" />
              {/if}
            </button>
          {/if}
        </div>

        {#if chosen.length > 0}
          <div class="hidden sm:block">{@render clearFieldButton(field)}</div>
        {/if}
      </div>
    {/each}

    {#if pickerFields.length > 0}
      <div class="flex flex-wrap items-center gap-x-6 gap-y-2 px-3 py-2">
        {#each pickerFields as field (field.key)}
          {@const options = optionsFor(field)}
          {@const chosen = selected(values, field.key)}
          <div class="flex min-w-0 items-center gap-3" data-testid="filter-field-{field.key}">
            <span class="w-20 shrink-0 text-xs font-medium text-base-content/60 sm:w-auto">{field.label}</span>
            <select
              class="select select-sm w-full min-w-[12rem] max-w-[16rem] bg-base-100"
              aria-label="Filter by {field.label}"
              data-testid="filter-select-{field.key}"
              value={chosen[0] ?? ''}
              onchange={(e) => choose(field, (e.currentTarget as HTMLSelectElement).value)}
            >
              <option value="">Any {field.label.toLowerCase()}</option>
              {#each options as option (option.value)}
                <option value={option.value}>
                  {option.label}{option.count === null ? '' : ` (${option.count})`}
                </option>
              {/each}
            </select>
            {#if chosen.length > 0}
              {@render clearFieldButton(field)}
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
