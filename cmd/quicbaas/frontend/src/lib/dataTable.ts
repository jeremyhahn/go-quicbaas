// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * The arithmetic behind DataTable: paging, sorting, searching, the range
 * label, the `sort` query parameter, and the response envelope a paging
 * endpoint answers with.
 *
 * All of it is pure and lives outside the component on purpose. Paging
 * arithmetic is the part of a table that is easy to get subtly wrong — an
 * off-by-one in the last page, a page index that survives a shrinking
 * collection, a clamp that divides by zero — and a function is far cheaper to
 * pin down than a rendered table. The component keeps only the state and the
 * markup.
 */

import { adoptFacets, type Facets, type FilterValues } from './filters.ts';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** The page sizes the selector offers. */
export const PAGE_SIZES: readonly number[] = [25, 50, 100];

/** The page size a table uses until the operator picks another. */
export const DEFAULT_PAGE_SIZE = 25;

/** How long the search box waits after the last keystroke before it queries. */
export const SEARCH_DEBOUNCE_MS = 250;

/** SortDirection is the third state of a header: ascending, descending, off. */
export type SortDirection = 'asc' | 'desc' | 'none';

/**
 * Column declares one column of a table: what it is called, whether it can be
 * sorted or searched, and how to read its value out of a row.
 *
 * `key` is both the client-side sort/search accessor (when `value` is absent)
 * and the field name sent to a paging server, so it is spelled the way the API
 * spells it.
 */
export interface Column<T> {
  /** key identifies the column and names the field a paging server sorts on. */
  key: string;
  /** label is the header text. */
  label: string;
  /** sortable marks the header clickable. */
  sortable?: boolean;
  /** searchable includes the column's value in the search box's match. */
  searchable?: boolean;
  /**
   * value reads the column's sortable/searchable value out of a row. Absent
   * means the row's own `key` member, which covers the flat cases.
   */
  value?: (row: T) => string | number | boolean | null | undefined;
  /** headerClass is applied to the `<th>`. */
  headerClass?: string;
  /**
   * srLabel hides the label visually while keeping it for screen readers —
   * the convention this app already uses for an actions column.
   */
  srLabel?: boolean;
}

/** PageRequest is what a table asks a paging endpoint for. */
export interface PageRequest {
  limit: number;
  offset: number;
  /** sort is `<field>`, `<field>:desc`, or '' for the server's own order. */
  sort: string;
  /** q is the search text, or '' for no search. */
  q: string;
  /**
   * filters is the faceted selection, keyed by the field's `key` — the loader
   * turns it into the endpoint's own parameters (`statuses`, `project_id`).
   *
   * IT IS ABSENT, NOT EMPTY, FOR A TABLE THAT DECLARES NO FILTERS. Most of the
   * tables in this app declare none, and a request that always carried an
   * empty map would make every one of their loaders read as though it might
   * have to care.
   */
  filters?: FilterValues;
}

/**
 * Page is a normalised answer. `total` is the count the server reports, and
 * `null` when it reported none — which is exactly the signal that the endpoint
 * does not page yet and the table must fall back to paging what it was handed.
 *
 * `facets` is what the endpoint counted per filterable field, and is likewise
 * undefined when it counted nothing — a daemon older than the faceted listing,
 * for which the chips fall back to their declared vocabulary.
 */
export interface Page<T> {
  items: T[];
  total: number | null;
  facets?: Facets;
}

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

/**
 * columnValue reads a column's value out of a row: the declared accessor when
 * there is one, the row's own member otherwise. Absent reads as ''.
 */
export function columnValue<T>(row: T, column: Column<T>): string | number | boolean {
  const raw =
    column.value !== undefined
      ? column.value(row)
      : (row as Record<string, unknown>)[column.key];
  if (raw === null || raw === undefined) return '';
  if (typeof raw === 'number' || typeof raw === 'boolean' || typeof raw === 'string') {
    return raw;
  }
  return String(raw);
}

// ---------------------------------------------------------------------------
// Sorting
// ---------------------------------------------------------------------------

/**
 * nextDirection is one click on a header: ascending, then descending, then
 * off. Clicking a different column starts that column at ascending.
 */
export function nextDirection(
  currentKey: string,
  currentDir: SortDirection,
  clickedKey: string,
): { key: string; dir: SortDirection } {
  if (currentKey !== clickedKey) return { key: clickedKey, dir: 'asc' };
  if (currentDir === 'asc') return { key: clickedKey, dir: 'desc' };
  if (currentDir === 'desc') return { key: '', dir: 'none' };
  return { key: clickedKey, dir: 'asc' };
}

/**
 * sortRows orders a copy of `rows` by one column. A direction of 'none', or a
 * key no declared column carries, leaves the order the caller gave — the
 * collection's own order is a meaningful default (jobs arrive newest first)
 * and must survive a header being switched off.
 */
export function sortRows<T>(
  rows: readonly T[],
  columns: readonly Column<T>[],
  key: string,
  dir: SortDirection,
): T[] {
  if (dir === 'none' || key === '') return [...rows];
  const column = columns.find((c) => c.key === key);
  if (column === undefined) return [...rows];
  const sign = dir === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => sign * compareValues(columnValue(a, column), columnValue(b, column)));
}

/**
 * compareValues orders two cell values. Numbers compare numerically and
 * everything else compares as text, case-insensitively, so a column of mixed
 * numbers never sorts "10" before "9".
 */
function compareValues(
  a: string | number | boolean,
  b: string | number | boolean,
): number {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  if (typeof a === 'boolean' && typeof b === 'boolean') {
    return a === b ? 0 : a ? 1 : -1;
  }
  return String(a).localeCompare(String(b), undefined, { sensitivity: 'base' });
}

// ---------------------------------------------------------------------------
// Searching
// ---------------------------------------------------------------------------

/**
 * filterRows keeps the rows whose searchable columns contain `q`, matched
 * case-insensitively. An empty query, or a table that declares no searchable
 * column, keeps every row: a search that silently emptied a table because
 * nobody declared a column would be worse than no search at all.
 */
export function filterRows<T>(
  rows: readonly T[],
  columns: readonly Column<T>[],
  q: string,
): T[] {
  const needle = q.trim().toLowerCase();
  if (needle === '') return [...rows];
  const searchable = columns.filter((c) => c.searchable === true);
  if (searchable.length === 0) return [...rows];
  return rows.filter((row) =>
    searchable.some((c) => String(columnValue(row, c)).toLowerCase().includes(needle)),
  );
}

// ---------------------------------------------------------------------------
// Paging
// ---------------------------------------------------------------------------

/** pageCount is how many pages `total` rows fill, never fewer than one. */
export function pageCount(total: number, size: number): number {
  if (size <= 0) return 1;
  return Math.max(1, Math.ceil(total / size));
}

/**
 * clampPage keeps a zero-based page index inside the collection. A table
 * sitting on page 40 when a filter cuts the collection to three rows must land
 * on the last page that exists, not render an empty one.
 */
export function clampPage(page: number, total: number, size: number): number {
  if (!Number.isFinite(page) || page < 0) return 0;
  return Math.min(Math.trunc(page), pageCount(total, size) - 1);
}

/** sliceRows returns the rows of one zero-based page. */
export function sliceRows<T>(rows: readonly T[], page: number, size: number): T[] {
  if (size <= 0) return [...rows];
  const start = clampPage(page, rows.length, size) * size;
  return rows.slice(start, start + size);
}

/**
 * rangeLabel is the pager's "1–25 of 1,987". `offset` is zero-based, `shown`
 * is how many rows this page actually rendered, and an empty collection reads
 * "0 of 0" rather than "1–0 of 0".
 */
export function rangeLabel(offset: number, shown: number, total: number): string {
  if (total <= 0 || shown <= 0) return `0 of ${total.toLocaleString()}`;
  const first = offset + 1;
  const last = offset + shown;
  return `${first.toLocaleString()}–${last.toLocaleString()} of ${total.toLocaleString()}`;
}

// ---------------------------------------------------------------------------
// The `sort` query parameter
// ---------------------------------------------------------------------------

/** sortParam renders the sort state as `<field>` / `<field>:desc` / ''. */
export function sortParam(key: string, dir: SortDirection): string {
  if (key === '' || dir === 'none') return '';
  return dir === 'desc' ? `${key}:desc` : key;
}

/** parseSortParam reads back what sortParam wrote. */
export function parseSortParam(raw: string): { key: string; dir: SortDirection } {
  const value = raw.trim();
  if (value === '') return { key: '', dir: 'none' };
  const colon = value.indexOf(':');
  if (colon === -1) return { key: value, dir: 'asc' };
  const key = value.slice(0, colon);
  const dir = value.slice(colon + 1).toLowerCase() === 'desc' ? 'desc' : 'asc';
  if (key === '') return { key: '', dir: 'none' };
  return { key, dir };
}

// ---------------------------------------------------------------------------
// The response envelope
// ---------------------------------------------------------------------------

/**
 * adoptPage normalises whatever a list endpoint answered with.
 *
 * THE PRESENCE OF `total` IS THE MODE SWITCH. A paging endpoint answers
 * `{ <items>: [...], total, limit, offset, next_offset }`; an endpoint that
 * does not page yet answers a bare array, or the same object without `total`.
 * Reporting `total: null` for the second shape is what lets one table work
 * against both — before and after the API grows paging — without the view
 * being told which world it is in.
 *
 * `itemsKey` names the member holding the rows (`jobs`, `tasks`, …). Absent,
 * the first array-valued member is taken, which covers every envelope the
 * orchestrator returns without each caller having to spell it.
 */
export function adoptPage<T>(raw: unknown, itemsKey?: string): Page<T> {
  if (Array.isArray(raw)) return { items: raw as T[], total: null };
  if (raw === null || typeof raw !== 'object') return { items: [], total: null };

  const record = raw as Record<string, unknown>;
  const total = typeof record['total'] === 'number' ? record['total'] : null;
  const facets = adoptFacets(record);

  if (itemsKey !== undefined) {
    const named = record[itemsKey];
    return withFacets({ items: Array.isArray(named) ? (named as T[]) : [], total }, facets);
  }
  for (const value of Object.values(record)) {
    if (Array.isArray(value)) return withFacets({ items: value as T[], total }, facets);
  }
  return withFacets({ items: [], total }, facets);
}

/**
 * withFacets attaches the counts only when there are some. An answer with no
 * `facets` member leaves the property ABSENT rather than setting it to an
 * empty map, because the chips read undefined as "nobody counted" and an
 * empty map as "every value has none".
 */
function withFacets<T>(page: Page<T>, facets: Facets | undefined): Page<T> {
  if (facets === undefined) return page;
  return { ...page, facets };
}

// ---------------------------------------------------------------------------
// URL state
// ---------------------------------------------------------------------------

/** TableState is the part of a table's state a link can carry. */
export interface TableState {
  page: number;
  size: number;
  sort: string;
  q: string;
}

/**
 * The query-parameter names a table writes into the hash, prefixed by the
 * table's own key so two tables on one route never read each other's page.
 */
export function stateParamNames(urlKey: string): Record<keyof TableState, string> {
  const prefix = urlKey === '' ? '' : `${urlKey}_`;
  return { page: `${prefix}page`, size: `${prefix}size`, sort: `${prefix}sort`, q: `${prefix}q` };
}

/**
 * readState pulls a table's state out of a hash such as
 * `#/jobs?jobs_page=3&jobs_sort=started_at:desc`. Anything missing or
 * unreadable falls back to the defaults the caller passes, so a hand-edited
 * link can never put a table into a state it cannot render.
 */
export function readState(hash: string, urlKey: string, defaults: TableState): TableState {
  const names = stateParamNames(urlKey);
  const queryAt = hash.indexOf('?');
  if (queryAt === -1) return { ...defaults };
  const params = new URLSearchParams(hash.slice(queryAt + 1));

  const page = Number.parseInt(params.get(names.page) ?? '', 10);
  const size = Number.parseInt(params.get(names.size) ?? '', 10);
  return {
    // The link carries the human's 1-based page; the state is 0-based.
    page: Number.isFinite(page) && page > 0 ? page - 1 : defaults.page,
    size: PAGE_SIZES.includes(size) ? size : defaults.size,
    sort: params.get(names.sort) ?? defaults.sort,
    q: params.get(names.q) ?? defaults.q,
  };
}

/**
 * writeState renders a hash carrying the table's state, leaving the route path
 * and every foreign query parameter — another table's state, `?project=` —
 * exactly as it found them. Defaults are omitted rather than written, so an
 * untouched table leaves no trail in the address bar.
 */
export function writeState(
  hash: string,
  urlKey: string,
  state: TableState,
  defaults: TableState,
): string {
  const bare = hash.replace(/^#/, '');
  const queryAt = bare.indexOf('?');
  const path = queryAt === -1 ? bare : bare.slice(0, queryAt);
  const params = new URLSearchParams(queryAt === -1 ? '' : bare.slice(queryAt + 1));
  const names = stateParamNames(urlKey);

  setOrDelete(params, names.page, state.page === defaults.page ? '' : String(state.page + 1));
  setOrDelete(params, names.size, state.size === defaults.size ? '' : String(state.size));
  setOrDelete(params, names.sort, state.sort === defaults.sort ? '' : state.sort);
  setOrDelete(params, names.q, state.q === defaults.q ? '' : state.q);

  const query = params.toString();
  return query === '' ? `#${path}` : `#${path}?${query}`;
}

function setOrDelete(params: URLSearchParams, name: string, value: string): void {
  if (value === '') {
    params.delete(name);
    return;
  }
  params.set(name, value);
}
