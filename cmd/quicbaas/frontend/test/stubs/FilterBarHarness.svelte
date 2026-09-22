<!--
  FilterBarHarness mounts FilterBar and keeps the selection, so a test can
  click a chip and see what the NEXT render offers — which is the whole
  behaviour, and is invisible to a test that only inspects the callback.
-->
<script lang="ts">
  import FilterBar from '../../src/components/core/FilterBar.svelte';
  import type { Facets, FilterField, FilterValues } from '../../src/lib/filters.ts';

  let {
    fields,
    facets,
    initial = {},
    onchange,
  }: {
    fields: FilterField[];
    facets?: Facets;
    initial?: FilterValues;
    onchange?: (next: FilterValues) => void;
  } = $props();

  let values = $state<FilterValues>(initial);

  function apply(next: FilterValues): void {
    values = next;
    onchange?.(next);
  }
</script>

<FilterBar {fields} {values} {facets} onchange={apply} />
