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
  import { X } from 'lucide-svelte';
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

  const active = $derived(activeFilters(fields, values, facets));
  const anyActive = $derived(activeCount(values) > 0);

  function optionsFor(field: FilterField) {
    return fieldOptions(field, facets);
  }

  function pick(field: FilterField, value: string): void {
    onchange(toggleValue(values, field, value));
  }

  function choose(field: FilterField, value: string): void {
    onchange(setValue(values, field.key, value));
  }
</script>

<div class="space-y-2" data-testid="filter-bar">
  {#if anyActive}
    <!-- The selection, repeated where it cannot be missed and can be undone. -->
    <div class="flex flex-wrap items-center gap-1" data-testid="active-filters">
      <span class="text-xs uppercase tracking-wider text-base-content/40 mr-1">Filtered by</span>
      {#each active as chip (`${chip.field}:${chip.value}`)}
        <span class="badge badge-sm badge-primary gap-1 pr-1">
          <span>{chip.fieldLabel}: {chip.valueLabel}</span>
          <button
            type="button"
            class="btn btn-ghost btn-xs btn-circle h-4 w-4 min-h-0 p-0"
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
        class="btn btn-ghost btn-xs"
        data-testid="filter-clear-all"
        onclick={() => onchange(clearAll())}
      >Clear all</button>
    </div>
  {/if}

  <div class="flex flex-wrap items-start gap-x-5 gap-y-2">
    {#each fields as field (field.key)}
      {@const options = optionsFor(field)}
      {@const chosen = selected(values, field.key)}
      <div class="flex items-center flex-wrap gap-1" data-testid="filter-field-{field.key}">
        <span class="text-xs uppercase tracking-wider text-base-content/40 mr-1">{field.label}</span>

        {#if usesChips(field, options.length)}
          <div class="flex flex-wrap gap-1" role="group" aria-label="Filter by {field.label}">
            {#each options as option (option.value)}
              {@const on = isSelected(values, field.key, option.value)}
              <button
                type="button"
                class="btn btn-xs gap-1 normal-case"
                class:btn-primary={on}
                class:btn-ghost={!on}
                class:border-base-300={!on}
                aria-pressed={on}
                title="Filter by {field.label}: {option.label}"
                data-testid="filter-chip-{field.key}-{option.value}"
                onclick={() => pick(field, option.value)}
              >
                <span>{option.label}</span>
                {#if option.count !== null}
                  <span class="opacity-60 font-mono" data-testid="filter-count-{field.key}-{option.value}">{option.count}</span>
                {/if}
              </button>
            {/each}
            {#if options.length === 0}
              <span class="text-xs text-base-content/30 italic">none</span>
            {/if}
          </div>
        {:else}
          <select
            class="select select-bordered select-xs max-w-[14rem]"
            aria-label="Filter by {field.label}"
            data-testid="filter-select-{field.key}"
            value={chosen[0] ?? ''}
            onchange={(e) => choose(field, (e.currentTarget as HTMLSelectElement).value)}
          >
            <option value="">All {field.label.toLowerCase()}</option>
            {#each options as option (option.value)}
              <option value={option.value}>
                {option.label}{option.count === null ? '' : ` (${option.count})`}
              </option>
            {/each}
          </select>
        {/if}

        {#if chosen.length > 0}
          <button
            type="button"
            class="btn btn-ghost btn-xs btn-circle"
            aria-label="Clear {field.label} filter"
            title="Clear {field.label} filter"
            data-testid="filter-clear-{field.key}"
            onclick={() => onchange(clearField(values, field.key))}
          >
            <X class="h-3 w-3" />
          </button>
        {/if}
      </div>
    {/each}
  </div>
</div>
