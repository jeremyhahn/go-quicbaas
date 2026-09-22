<!--
  DataTableHarness mounts DataTable with a fixed set of columns and a row
  snippet, so the component's own behaviour — paging, sorting, the search
  debounce, both data modes — can be driven from a test without every case
  restating the markup.
-->
<script lang="ts">
  import DataTable from '../../src/components/core/DataTable.svelte';
  import type { Column, PageRequest } from '../../src/lib/dataTable.ts';
  import type { FilterField } from '../../src/lib/filters.ts';

  interface HarnessRow {
    id: string;
    name: string;
    count: number;
    status?: string;
  }

  let {
    rows = [],
    load,
    filters = [],
    itemsKey,
    reloadKey,
    loading = false,
    pageSize = 25,
    urlKey = '',
    defaultSort = '',
  }: {
    rows?: HarnessRow[];
    load?: (req: PageRequest) => Promise<unknown>;
    filters?: FilterField<HarnessRow>[];
    itemsKey?: string;
    reloadKey?: string | number;
    loading?: boolean;
    pageSize?: number;
    urlKey?: string;
    defaultSort?: string;
  } = $props();

  const columns: Column<HarnessRow>[] = [
    { key: 'id', label: 'ID' },
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'count', label: 'Count', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];
</script>

<DataTable
  {columns}
  {rows}
  {load}
  {filters}
  {itemsKey}
  {reloadKey}
  {loading}
  {pageSize}
  {urlKey}
  {defaultSort}
  rowKey={(r: HarnessRow) => r.id}
  emptyTitle="No rows yet"
  emptyDescription="Nothing has been recorded."
>
  {#snippet row(item: HarnessRow)}
    <tr data-testid="harness-row">
      <td data-testid="cell-id">{item.id}</td>
      <td data-testid="cell-name">{item.name}</td>
      <td data-testid="cell-count">{item.count}</td>
      <td></td>
    </tr>
  {/snippet}
</DataTable>
