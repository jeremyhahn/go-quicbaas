<!--
  DataTable is the one table in this app.

  Every list surface — jobs, tasks, sessions, agents, projects, workflows,
  accounts, keys, secrets, buckets, topics — used to render its own <table>
  over its whole collection, with whatever search and sort somebody happened to
  write that day and, without exception, no paging at all. A tenant with two
  thousand jobs therefore loaded two thousand rows into the DOM before the
  Jobs page painted, and the same operator met a different set of controls on
  every page. This component is the single answer to both: one look, one set of
  keyboard-reachable controls, and 25 rows in the DOM instead of 2,000.

  What a view declares
  ────────────────────
    columns   what the headers say, which of them sort, which of them search
    row       a snippet rendering ONE <tr> — the cells stay the view's own,
              because a job's progress bar and a secret's rotation chip have
              nothing in common and never should
    rows      the collection, for a view that holds its own data
    load      OR a loader, for a view whose endpoint pages

  The two data modes behind one API
  ─────────────────────────────────
  `load` is handed `{limit, offset, sort, q}` and may answer either shape:

    {jobs: [...], total: 1987, limit: 25, offset: 0, next_offset: 25}
        → server mode. The component renders exactly what it was given and
          asks again on every page, sort and search.

    [...] or {jobs: [...]}   (no `total`)
        → client mode. The answer is the whole collection and the component
          pages, sorts and searches it here.

  THE MODE IS DETECTED, NOT CONFIGURED. The orchestrator list endpoints are
  growing `limit`/`offset`/`sort`/`q` in a parallel change; a view must not
  have to be edited on the day that lands, and the UI must work on both sides
  of it. `total` is the only thing that differs between the two answers, so
  `total` is what decides — see adoptPage in lib/dataTable.ts.

  Faceted filters
  ───────────────
  `filters` declares the fields a list narrows by — a status, a project, a
  runner — and the table owns the selection exactly as it owns the sort: it
  goes in the hash, it resets the page, and it reaches the loader as
  `req.filters`. The counts beside each chip come from the answer's `facets`
  member in server mode and are computed here in client mode, so a view gets
  the same control either way and neither has to know which it is.

  State in the URL
  ────────────────
  With `urlKey` set, the current page, page size, sort, search and filters
  live in the route hash
  (`#/tasks?tasks_page=3&tasks_sort=updated_at:desc&tasks_status=pending,blocked`),
  so a reload or a pasted link lands on the same page of the same table,
  narrowed the same way. The hash is rewritten with replaceState: paging is
  not navigation, and Back must leave the view, not walk back through forty
  pages.
-->
<script lang="ts" generics="T">
  import type { Snippet } from 'svelte';
  import { untrack } from 'svelte';
  import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-svelte';
  import EmptyState from './EmptyState.svelte';
  import FilterBar from './FilterBar.svelte';
  import {
    activeCount,
    computeFacets,
    filterByFacets,
    readFilters,
    writeFilters,
    type Facets,
    type FilterField,
    type FilterValues,
  } from '../../lib/filters.ts';
  import {
    DEFAULT_PAGE_SIZE,
    PAGE_SIZES,
    SEARCH_DEBOUNCE_MS,
    adoptPage,
    clampPage,
    filterRows,
    nextDirection,
    pageCount,
    parseSortParam,
    rangeLabel,
    readState,
    sliceRows,
    sortParam,
    sortRows,
    writeState,
    type Column,
    type PageRequest,
    type SortDirection,
    type TableState,
  } from '../../lib/dataTable.ts';

  let {
    columns,
    row,
    rows = [],
    load,
    filters = [],
    itemsKey,
    reloadKey,
    loading = false,
    narrowed = false,
    pageSize = DEFAULT_PAGE_SIZE,
    defaultSort = '',
    urlKey = '',
    rowKey,
    searchPlaceholder = 'Search…',
    searchLabel = 'Search table',
    ariaLabel = '',
    noMatchText = 'No rows match the current filter.',
    emptyIcon,
    emptyTitle = 'Nothing here yet',
    emptyDescription = '',
    emptyActionLabel = '',
    onemptyaction,
    empty,
    toolbar,
  }: {
    /** columns declares the headers and which of them sort or search. */
    columns: Column<T>[];
    /** row renders one `<tr>` for a row of the current page. */
    row: Snippet<[T]>;
    /** rows is the collection, for a view that holds its own data. */
    rows?: readonly T[];
    /** load reads one page, for a view whose endpoint pages. */
    load?: (req: PageRequest) => Promise<unknown>;
    /** filters declares the facets this list can be narrowed by. */
    filters?: FilterField<T>[];
    /** itemsKey names the envelope member holding the rows (`jobs`, `tasks`). */
    itemsKey?: string;
    /** reloadKey forces a re-read when it changes — the active tenant, say. */
    reloadKey?: string | number;
    /** loading shows the spinner for a view that fetches its own rows. */
    loading?: boolean;
    /**
     * narrowed says the view's OWN filters are active. A table handed a
     * pre-filtered collection cannot tell "this tenant has no sessions" from
     * "no session is both closed and on claude", and telling the second
     * operator to spawn their first session is a lie.
     */
    narrowed?: boolean;
    pageSize?: number;
    /** defaultSort is the initial sort, as `<field>` or `<field>:desc`. */
    defaultSort?: string;
    /** urlKey puts page/size/sort/search in the route hash under this prefix. */
    urlKey?: string;
    /** rowKey gives each row its identity for keyed rendering. */
    rowKey?: (row: T) => string | number;
    searchPlaceholder?: string;
    searchLabel?: string;
    ariaLabel?: string;
    noMatchText?: string;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    emptyIcon?: new (...args: any[]) => any;
    emptyTitle?: string;
    emptyDescription?: string;
    emptyActionLabel?: string;
    onemptyaction?: () => void;
    /** empty replaces the default EmptyState for a collection with no rows. */
    empty?: Snippet;
    /** toolbar renders the view's own filters beside the search box. */
    toolbar?: Snippet;
  } = $props();

  // -------------------------------------------------------------------------
  // State
  // -------------------------------------------------------------------------

  // Read once, on purpose: the page size, the declared sort and the url key a
  // view is constructed with are the table's starting point, not a channel the
  // view keeps writing to. untrack says so to the compiler and to the reader.
  const defaults: TableState = untrack(() => ({
    page: 0,
    size: PAGE_SIZES.includes(pageSize) ? pageSize : DEFAULT_PAGE_SIZE,
    sort: defaultSort,
    q: '',
  }));

  const initial: TableState = untrack(() =>
    urlKey !== '' && typeof window !== 'undefined'
      ? readState(window.location.hash, urlKey, defaults)
      : defaults,
  );
  const initialSort = parseSortParam(initial.sort);

  let page = $state(initial.page);
  let size = $state(initial.size);
  let sortKey = $state(initialSort.key);
  let sortDir = $state<SortDirection>(initialSort.dir);
  /** searchInput is what the operator is typing; query is what the table ran. */
  let searchInput = $state(initial.q);
  let query = $state(initial.q);
  /** The faceted selection, read from the link the operator arrived on. */
  let filterValues = $state<FilterValues>(
    untrack(() =>
      urlKey !== '' && typeof window !== 'undefined'
        ? readFilters(window.location.hash, urlKey, filters)
        : {},
    ),
  );
  /**
   * sizeTouched records that the operator chose a page size. Once they have,
   * the pager stays on screen even where the rows now fit on one page —
   * hiding the control somebody just used would strand them at 100 rows with
   * no way back to 25.
   */
  let sizeTouched = $state(false);

  /** What the endpoint counted per facet, absent when it counted nothing. */
  let serverFacets = $state<Facets | undefined>(undefined);
  /** Rows the loader answered with, when it answered a paged envelope. */
  let serverRows = $state<T[]>([]);
  let serverTotal = $state<number | null>(null);
  /** Rows the loader answered with when it turned out not to page. */
  let fallbackRows = $state<T[]>([]);
  /** fellBack records that the endpoint answered without a `total`. */
  let fellBack = $state(false);
  let serverBusy = $state(false);
  let loadError = $state('');
  let lastReloadKey: string | number | undefined = undefined;
  /** inflight orders concurrent reads so a late answer cannot win. */
  let inflight = 0;

  // -------------------------------------------------------------------------
  // Derived view of the data
  // -------------------------------------------------------------------------

  const serverMode = $derived(load !== undefined && !fellBack && serverTotal !== null);

  /** The collection the component pages itself, in client mode. */
  const collection = $derived<readonly T[]>(load !== undefined ? fallbackRows : rows);

  /**
   * searched is the client-mode collection after the SEARCH and before the
   * facets. The counts are taken over it, because a facet counted after its
   * own filter was applied would read "pending 12" and every other status 0.
   */
  const searched = $derived<readonly T[]>(
    serverMode ? [] : filterRows(collection, columns, query),
  );

  const matched = $derived(
    serverMode
      ? serverRows
      : sortRows(filterByFacets(searched, filters, filterValues), columns, sortKey, sortDir),
  );

  /** The counts the chips carry: the server's in server mode, ours otherwise. */
  const facets = $derived<Facets | undefined>(
    serverMode ? serverFacets : computeFacets(searched, filters),
  );

  const total = $derived(serverMode ? (serverTotal ?? 0) : matched.length);
  const safePage = $derived(serverMode ? page : clampPage(page, total, size));
  const visible = $derived(serverMode ? serverRows : sliceRows(matched, safePage, size));
  const offset = $derived(safePage * size);
  const lastPage = $derived(pageCount(total, size) - 1);
  const busy = $derived(loading || (serverBusy && visible.length === 0));
  /** filtered says the emptiness is the search's or a facet's doing. */
  const filtered = $derived(query.trim() !== '' || activeCount(filterValues) > 0);

  /**
   * hasFilters is a BOOLEAN, deliberately, and the read effect uses it rather
   * than `filters.length`. A view builds its field list from data it loads —
   * the project names behind a picker — so the ARRAY is a new one every time
   * that arrives; depending on it would re-read the page each time a label
   * showed up. The answer to "does this table facet at all" does not change.
   */
  const hasFilters = $derived(filters.length > 0);
  /**
   * The search box appears only where it can act: over a column that declared
   * itself searchable, or over an endpoint that searches server-side. A box
   * that matched nothing anyone declared would quietly empty the table.
   */
  const searchable = $derived(load !== undefined || columns.some((c) => c.searchable === true));
  /**
   * The pager's controls appear only where there is something to page. Half
   * the tables in this app are a handful of rows inside a panel — a key's
   * grants, a project's agent roles — and a page selector over three rows is
   * noise that makes the consistent treatment worth less, not more. The range
   * label stays either way: "3 of 3" is still the answer to how many there are.
   *
   * Once the operator has CHOSEN a size the pager stays, whatever the count:
   * the control that put them on 100 rows has to still be there to put them
   * back.
   */
  const pageable = $derived(total > size || sizeTouched);
  const hasRows = $derived(serverMode ? total > 0 : collection.length > 0);

  // -------------------------------------------------------------------------
  // Search debounce
  // -------------------------------------------------------------------------

  $effect(() => {
    const typed = searchInput;
    if (typed === untrack(() => query)) return;
    const timer = setTimeout(() => {
      query = typed;
      page = 0;
    }, SEARCH_DEBOUNCE_MS);
    return () => { clearTimeout(timer); };
  });

  // -------------------------------------------------------------------------
  // Server reads
  // -------------------------------------------------------------------------

  $effect(() => {
    const loader = load;
    const key = reloadKey;
    const chosen = filterValues;
    const faceted = hasFilters;
    const req: PageRequest = {
      limit: size,
      offset: page * size,
      sort: sortParam(sortKey, sortDir),
      q: query,
    };
    // Absent rather than empty for a table with no facets — see PageRequest.
    if (faceted) req.filters = chosen;
    if (loader === undefined) return;
    untrack(() => {
      if (key !== lastReloadKey) {
        lastReloadKey = key;
        fellBack = false;
      } else if (fellBack) {
        // The endpoint does not page: it already handed over everything, and
        // asking again for page two would fetch the same rows a second time.
        return;
      }
      void runLoad(loader, req);
    });
  });

  async function runLoad(
    loader: (req: PageRequest) => Promise<unknown>,
    req: PageRequest,
  ): Promise<void> {
    const id = ++inflight;
    serverBusy = true;
    try {
      const answered = adoptPage<T>(await loader(req), itemsKey);
      if (id !== inflight) return;
      loadError = '';
      if (answered.total === null) {
        fallbackRows = answered.items;
        serverRows = [];
        serverTotal = null;
        // An endpoint that does not page does not count either; the chips
        // fall back to counting the collection it handed over.
        serverFacets = undefined;
        fellBack = true;
        return;
      }
      serverRows = answered.items;
      serverTotal = answered.total;
      serverFacets = answered.facets;
      fallbackRows = [];
      fellBack = false;
    } catch (err: unknown) {
      if (id !== inflight) return;
      loadError = err instanceof Error ? err.message : String(err);
    } finally {
      if (id === inflight) serverBusy = false;
    }
  }

  // -------------------------------------------------------------------------
  // URL state
  // -------------------------------------------------------------------------

  $effect(() => {
    const state: TableState = {
      page,
      size,
      sort: sortParam(sortKey, sortDir),
      q: query,
    };
    const chosen = filterValues;
    if (urlKey === '' || typeof window === 'undefined') return;
    // One rewrite for both halves: two effects racing to replaceState would
    // each preserve what the other wrote, but only by reading a hash the
    // browser had not finished updating.
    const next = writeFilters(
      writeState(window.location.hash, urlKey, state, defaults),
      urlKey,
      filters,
      chosen,
    );
    if (next !== window.location.hash) {
      window.history.replaceState(window.history.state, '', next);
    }
  });

  // -------------------------------------------------------------------------
  // Controls
  // -------------------------------------------------------------------------

  function toggleSort(key: string): void {
    const next = nextDirection(sortKey, sortDir, key);
    sortKey = next.key;
    sortDir = next.dir;
    page = 0;
  }

  /**
   * sortState is what a header's icon shows. 'none' is NOT "no icon": an
   * unsorted sortable column carries the neutral ⇅, because a header that
   * looks like text until you happen to click it is a control nobody finds.
   * A column that does not sort carries nothing at all, which is how the two
   * are told apart at a glance.
   */
  function sortState(key: string): SortDirection {
    if (sortKey !== key || sortDir === 'none') return 'none';
    return sortDir;
  }

  function ariaSort(key: string): 'ascending' | 'descending' | 'none' {
    if (sortKey !== key || sortDir === 'none') return 'none';
    return sortDir === 'asc' ? 'ascending' : 'descending';
  }

  function goto(target: number): void {
    page = Math.min(Math.max(0, target), Math.max(0, lastPage));
  }

  /**
   * applyFilters takes the whole next selection from FilterBar and returns to
   * the first page: page 3 of the old filter is not page 3 of the new one, and
   * landing on an empty page is how an operator concludes a filter is broken.
   */
  function applyFilters(next: FilterValues): void {
    filterValues = next;
    page = 0;
  }

  function changeSize(next: number): void {
    // Keep the first row of the current page in view: growing the page size
    // must not throw the operator back to the top of a 2,000-row list.
    const first = safePage * size;
    size = next;
    sizeTouched = true;
    page = Math.floor(first / next);
  }

  function identity(item: T, index: number): string | number {
    return rowKey !== undefined ? rowKey(item) : index;
  }
</script>

<!--
  The pager, defined ONCE and rendered twice — above the table and below it.
  Two render sites, one piece of state: whichever one the operator reaches
  for, the other agrees, because there is nothing for them to disagree about.

  THE TOP ONE IS THE POINT. A pager only at the bottom of twenty-five rows is
  below the fold on every laptop in the building, which is how a paged table
  gets reported as "no pagination at all".

  `place` distinguishes the two for assistive technology and for tests: two
  buttons with the identical accessible name in one document is exactly the
  ambiguity a screen-reader user cannot resolve.
-->
{#snippet pagerControls(place: 'top' | 'bottom')}
  {@const suffix = place === 'top' ? '' : ' (bottom)'}
  <div
    class="flex flex-wrap items-center justify-between gap-2 text-xs tabular-nums text-base-content/60"
    data-testid="data-table-pager-{place}"
  >
    <span data-testid={place === 'top' ? 'data-table-range' : 'data-table-range-bottom'}>
      {rangeLabel(offset, visible.length, total)}
    </span>

    <div class="flex items-center gap-2">
      {#if place === 'top' && pageable}
        <label class="flex items-center gap-1">
          <span>Rows</span>
          <select
            class="select select-xs w-16"
            aria-label="Rows per page"
            data-testid="data-table-page-size"
            value={size}
            onchange={(e) => changeSize(Number((e.currentTarget as HTMLSelectElement).value))}
          >
            {#each PAGE_SIZES as option (option)}
              <option value={option}>{option}</option>
            {/each}
          </select>
        </label>
      {/if}

      {#if pageable}
        <nav class="join" aria-label="Table pages{suffix}">
          <button
            type="button"
            class="btn btn-xs join-item"
            aria-label="First page{suffix}"
            disabled={safePage <= 0}
            onclick={() => goto(0)}
          >&laquo;</button>
          <button
            type="button"
            class="btn btn-xs join-item"
            aria-label="Previous page{suffix}"
            disabled={safePage <= 0}
            onclick={() => goto(safePage - 1)}
          >&lsaquo;</button>
          <span
            class="btn btn-xs join-item no-animation pointer-events-none"
            data-testid={place === 'top' ? 'data-table-page' : 'data-table-page-bottom'}
          >
            {safePage + 1} / {lastPage + 1}
          </span>
          <button
            type="button"
            class="btn btn-xs join-item"
            aria-label="Next page{suffix}"
            disabled={safePage >= lastPage}
            onclick={() => goto(safePage + 1)}
          >&rsaquo;</button>
          <button
            type="button"
            class="btn btn-xs join-item"
            aria-label="Last page{suffix}"
            disabled={safePage >= lastPage}
            onclick={() => goto(lastPage)}
          >&raquo;</button>
        </nav>
      {/if}
    </div>
  </div>
{/snippet}

<div class="space-y-3" data-testid="data-table">
  <!-- Toolbar: search and the view's own controls. -->
  {#if searchable || toolbar}
    <div class="flex items-center gap-2 flex-wrap">
      {#if searchable}
        <input
          type="search"
          class="input input-sm w-full bg-base-100 sm:max-w-xs"
          placeholder={searchPlaceholder}
          aria-label={searchLabel}
          data-testid="data-table-search"
          bind:value={searchInput}
        />
      {/if}
      {#if toolbar}
        {@render toolbar()}
      {/if}
      <div class="flex-1"></div>
    </div>
  {/if}

  {#if hasFilters}
    <FilterBar fields={filters} values={filterValues} {facets} onchange={applyFilters} />
  {/if}

  {#if loadError !== ''}
    <div class="alert alert-error text-sm" role="alert">
      <span>{loadError}</span>
    </div>
  {/if}

  {@render pagerControls('top')}

  <div class="overflow-x-auto rounded-box border border-base-300/80 bg-base-100">
    <table class="table table-sm table-zebra w-full" aria-label={ariaLabel || undefined} aria-busy={busy}>
      <thead>
        <tr class="bg-base-200/70">
          {#each columns as column (column.key)}
            {@const direction = sortState(column.key)}
            <th class={column.headerClass ?? ''} aria-sort={ariaSort(column.key)}>
              {#if column.sortable}
                <button
                  type="button"
                  class="group inline-flex items-center gap-1 font-semibold hover:text-primary"
                  class:text-base-content={direction !== 'none'}
                  onclick={() => toggleSort(column.key)}
                  aria-label="Sort by {column.label}"
                  title="Sort by {column.label}"
                  data-testid="data-table-sort-{column.key}"
                  data-sort={direction}
                >
                  <span>{column.label}</span>
                  {#if direction === 'asc'}
                    <ArrowUp class="h-3 w-3 text-primary" aria-hidden="true" />
                  {:else if direction === 'desc'}
                    <ArrowDown class="h-3 w-3 text-primary" aria-hidden="true" />
                  {:else}
                    <ChevronsUpDown
                      class="h-3 w-3 opacity-40 group-hover:opacity-100"
                      aria-hidden="true"
                    />
                  {/if}
                </button>
              {:else if column.srLabel}
                <span class="sr-only">{column.label}</span>
              {:else}
                {column.label}
              {/if}
            </th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#if busy}
          <!-- A few placeholder rows the shape of the real ones, so the table
               does not collapse and re-expand when the answer arrives. -->
          <tr>
            <td colspan={columns.length} class="py-3">
              <span class="flex items-center gap-2 text-xs text-base-content/60">
                <span class="loading loading-spinner loading-xs text-primary"></span>
                Loading…
              </span>
            </td>
          </tr>
          {#each [0, 1, 2] as placeholder (placeholder)}
            <tr aria-hidden="true">
              {#each columns as column (column.key)}
                <td><div class="skeleton h-3 rounded-sm {placeholder === 1 ? 'w-2/3' : 'w-4/5'}"></div></td>
              {/each}
            </tr>
          {/each}
        {:else if visible.length === 0}
          <tr>
            <td colspan={columns.length}>
              {#if loadError !== ''}
                <!--
                  A FAILED READ IS NOT AN EMPTY COLLECTION. The alert above
                  says what went wrong; telling the operator there is nothing
                  here, or inviting them to create their first one, would be a
                  claim about data nobody managed to read.
                -->
                <div class="py-10 text-center text-sm text-base-content/60" data-testid="data-table-unread">
                  Nothing loaded.
                </div>
              {:else if hasRows || filtered || narrowed}
                <div class="py-10 text-center text-sm text-base-content/60" data-testid="data-table-no-match">
                  {noMatchText}
                </div>
              {:else if empty}
                {@render empty()}
              {:else}
                <EmptyState
                  icon={emptyIcon}
                  title={emptyTitle}
                  description={emptyDescription}
                  actionLabel={emptyActionLabel}
                  onaction={onemptyaction}
                />
              {/if}
            </td>
          </tr>
        {:else}
          {#each visible as item, index (identity(item, index))}
            {@render row(item)}
          {/each}
        {/if}
      </tbody>
    </table>
  </div>

  {@render pagerControls('bottom')}
</div>
