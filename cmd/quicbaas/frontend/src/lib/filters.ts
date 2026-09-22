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
 * Faceted filtering: the vocabulary a list can be narrowed by, the counts
 * beside each value, the rules for selecting them, and the query parameters
 * that make a narrowed list a link somebody can paste.
 *
 * WHY A FIELD HAS THREE NAMES. The same facet is spelled differently in three
 * places and pretending otherwise would force one of them to be wrong:
 *
 *   key       what the LINK says            `tasks_status=pending,blocked`
 *   param     what the SERVER is asked      `statuses=pending,blocked`
 *   facetKey  what the ANSWER counts under  `facets.status.pending = 12`
 *
 * The link reads as the operator thinks ("status"), the parameter is the
 * plural the endpoint actually takes ("statuses"), and both default to `key`
 * so a field whose three names agree declares one.
 *
 * All of it is pure and lives outside the components: the arithmetic of which
 * chips are lit, what a click does to a multi-select group, and what the hash
 * then says is far cheaper to pin down here than through a rendered table.
 */

import { adoptFacets as adoptWireFacets, type PageFacets } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** FilterOption is one value a field can be narrowed to. */
export interface FilterOption {
  value: string;
  label: string;
}

/**
 * Facets is what a list endpoint counted, per field, over the set its search
 * matched — `{status: {pending: 12, blocked: 3}}`. An endpoint that counts
 * nothing sends none, and the chips fall back to the declared vocabulary
 * without counts rather than disappearing.
 *
 * It is the SDK's own `PageFacets` under a shorter name, not a second
 * declaration of it: the counts arrive through the SDK and a parallel type
 * here would be one drift away from a lie.
 */
export type Facets = PageFacets;

/** FilterValues is the operator's selection, per field key. */
export type FilterValues = Record<string, string[]>;

/**
 * The control a field's values are picked with.
 *
 *   chips   a lit-or-not button per value — a BOUNDED vocabulary (a status,
 *           a mode, a backend), where seeing every value at once is the point
 *   select  a picker — an UNBOUNDED one (a project id, an agent id), where a
 *           chip per value would be a thousand chips
 *
 * 'auto' decides by counting: chips while the vocabulary fits, a picker once
 * it does not. Multi-valued fields are always chips, because every filter the
 * API takes a list of is an enumeration.
 */
export type FilterControl = 'chips' | 'select' | 'auto';

/** How many options a field may offer before 'auto' reaches for a picker. */
export const CHIP_LIMIT = 10;

/**
 * FilterField declares one facet of a list.
 *
 * `rowValue` is what makes a field work on a list the server does not filter:
 * the table computes the counts and applies the narrowing itself, from the
 * collection in hand. A server-paged list leaves it out and the field travels
 * as a query parameter instead.
 */
export interface FilterField<T = never> {
  /** key names the field in the link and, by default, in the answer's facets. */
  key: string;
  /** label is what the chip group is called. */
  label: string;
  /** param is the query parameter the server takes. Defaults to `key`. */
  param?: string;
  /** facetKey is the member of `facets` holding the counts. Defaults to `key`. */
  facetKey?: string;
  /** multi says the API takes a list, so the chips are multi-select. */
  multi?: boolean;
  /** options is the known vocabulary, offered when the answer counts nothing. */
  options?: readonly FilterOption[];
  /** control overrides the chips-or-picker decision. */
  control?: FilterControl;
  /**
   * rowValue reads the field's value(s) out of a row, for a list this table
   * filters itself. Absent means the field is the server's to apply.
   */
  rowValue?: (row: T) => string | readonly string[] | null | undefined;
}

/** FacetOption is one rendered chip: a value, its label, and its count. */
export interface FacetOption extends FilterOption {
  /** count is how many records carry the value, or null when nobody counted. */
  count: number | null;
}

/** ActiveFilter is one selected value, as the removable chips above a table. */
export interface ActiveFilter {
  /** field is the key of the field the value belongs to. */
  field: string;
  /** fieldLabel and valueLabel are what the chip reads. */
  fieldLabel: string;
  value: string;
  valueLabel: string;
}

// ---------------------------------------------------------------------------
// Names
// ---------------------------------------------------------------------------

/** apiParam is the query parameter a field is sent to the server as. */
export function apiParam(field: FilterField): string {
  return field.param ?? field.key;
}

/** facetKey is the member of `facets` a field's counts live under. */
export function facetKey(field: FilterField): string {
  return field.facetKey ?? field.key;
}

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

/**
 * humanLabel turns a wire value into something to read: `in_progress` reads
 * "in progress", `sdlc` reads "sdlc". It is only ever used for a value nobody
 * declared a label for — a state the server grew since this build.
 */
export function humanLabel(value: string): string {
  return value.replace(/[_-]+/g, ' ').trim();
}

/**
 * fieldOptions is the chips a field offers: its declared vocabulary first, in
 * the order it was declared, then any value the answer counted that nobody
 * declared — a state added server-side after this bundle was built, which must
 * still be selectable rather than invisible.
 *
 * A declared value the answer did not count reads `0`, not "gone": telling an
 * operator there are no failed jobs is the answer, and hiding the chip would
 * make the question unaskable.
 */
export function fieldOptions(field: FilterField, facets?: Facets): FacetOption[] {
  const counts = facets?.[facetKey(field)];
  const declared = field.options ?? [];
  const options: FacetOption[] = declared.map((option) => ({
    value: option.value,
    label: option.label,
    count: counts === undefined ? null : (counts[option.value] ?? 0),
  }));

  if (counts === undefined) return options;

  const seen = new Set(declared.map((option) => option.value));
  const extra = Object.keys(counts)
    .filter((value) => value !== '' && !seen.has(value))
    .sort();
  for (const value of extra) {
    options.push({ value, label: humanLabel(value), count: counts[value] ?? 0 });
  }
  return options;
}

/**
 * usesChips decides how a field is picked. A multi-valued field is always
 * chips; a single-valued one is chips while its vocabulary is small enough to
 * read at a glance, and a picker once it is not.
 */
export function usesChips(field: FilterField, optionCount: number): boolean {
  if (field.control === 'chips') return true;
  if (field.control === 'select') return false;
  if (field.multi === true) return true;
  return optionCount <= CHIP_LIMIT;
}

// ---------------------------------------------------------------------------
// Selection
// ---------------------------------------------------------------------------

/** selected is the values chosen for one field, never undefined. */
export function selected(values: FilterValues, key: string): string[] {
  return values[key] ?? [];
}

/** isSelected says whether one value of one field is chosen. */
export function isSelected(values: FilterValues, key: string, value: string): boolean {
  return selected(values, key).includes(value);
}

/**
 * toggleValue is one click on a chip: a multi-valued field adds or removes the
 * value, a single-valued one replaces its selection — and clicking the chip
 * that is already lit clears it, because a filter you cannot switch off with
 * the control you switched it on with is a trap.
 */
export function toggleValue(
  values: FilterValues,
  field: FilterField,
  value: string,
): FilterValues {
  const current = selected(values, field.key);
  if (field.multi !== true) {
    return normalise({ ...values, [field.key]: current[0] === value ? [] : [value] });
  }
  const next = current.includes(value)
    ? current.filter((held) => held !== value)
    : [...current, value];
  return normalise({ ...values, [field.key]: next });
}

/** setValue replaces a field's selection outright — what a picker does. */
export function setValue(values: FilterValues, key: string, value: string): FilterValues {
  return normalise({ ...values, [key]: value === '' ? [] : [value] });
}

/** clearField drops one field's selection. */
export function clearField(values: FilterValues, key: string): FilterValues {
  return normalise({ ...values, [key]: [] });
}

/** clearValue drops one value — what the × on an active chip does. */
export function clearValue(values: FilterValues, key: string, value: string): FilterValues {
  return normalise({ ...values, [key]: selected(values, key).filter((held) => held !== value) });
}

/** clearAll drops every selection. */
export function clearAll(): FilterValues {
  return {};
}

/** activeCount is how many values are selected across every field. */
export function activeCount(values: FilterValues): number {
  return Object.values(values).reduce((sum, held) => sum + held.length, 0);
}

/**
 * activeFilters lists the selections as the removable chips above a table, in
 * the order the fields were declared. A value the field never declared still
 * appears — it came out of a link, and a filter that is narrowing the list
 * without saying so is the worst of both.
 */
export function activeFilters(
  fields: readonly FilterField[],
  values: FilterValues,
  facets?: Facets,
): ActiveFilter[] {
  const active: ActiveFilter[] = [];
  for (const field of fields) {
    const options = fieldOptions(field, facets);
    for (const value of selected(values, field.key)) {
      const option = options.find((candidate) => candidate.value === value);
      active.push({
        field: field.key,
        fieldLabel: field.label,
        value,
        valueLabel: option?.label ?? humanLabel(value),
      });
    }
  }
  return active;
}

/** normalise drops the empty selections, so an untouched field holds nothing. */
function normalise(values: FilterValues): FilterValues {
  const kept: FilterValues = {};
  for (const [key, held] of Object.entries(values)) {
    if (held.length > 0) kept[key] = held;
  }
  return kept;
}

// ---------------------------------------------------------------------------
// Client-side narrowing
// ---------------------------------------------------------------------------

/** rowValues reads a row's value(s) for one field, always as a list. */
export function rowValues<T>(row: T, field: FilterField<T>): string[] {
  if (field.rowValue === undefined) return [];
  const raw = field.rowValue(row);
  if (raw === null || raw === undefined) return [];
  if (typeof raw === 'string') return raw === '' ? [] : [raw];
  return raw.filter((value) => value !== '');
}

/**
 * matchesFilters says whether a row survives the selection, for a list this
 * table narrows itself. Values WITHIN a field are OR — "pending or blocked" —
 * and the fields are AND, which is what faceted search means everywhere and
 * what an operator picking two statuses expects.
 *
 * A field the row cannot answer for (no `rowValue`) narrows nothing here: it
 * is the server's to apply, and applying it twice would be wrong the moment
 * the two disagreed.
 */
export function matchesFilters<T>(
  row: T,
  fields: readonly FilterField<T>[],
  values: FilterValues,
): boolean {
  for (const field of fields) {
    if (field.rowValue === undefined) continue;
    const chosen = selected(values, field.key);
    if (chosen.length === 0) continue;
    const held = rowValues(row, field);
    if (!chosen.some((value) => held.includes(value))) return false;
  }
  return true;
}

/** filterByFacets keeps the rows that survive every selection. */
export function filterByFacets<T>(
  rows: readonly T[],
  fields: readonly FilterField<T>[],
  values: FilterValues,
): T[] {
  if (activeCount(values) === 0) return [...rows];
  return rows.filter((row) => matchesFilters(row, fields, values));
}

/**
 * computeFacets counts a collection, for a list the server does not count.
 *
 * THE COUNTS ARE TAKEN OVER THE SEARCHED SET, NOT THE FILTERED ONE — the
 * caller passes the rows the search matched and nothing else. A facet counted
 * after its own filter was applied would read "pending 12" and every other
 * status 0, which tells the operator nothing about what picking one would do.
 */
export function computeFacets<T>(
  rows: readonly T[],
  fields: readonly FilterField<T>[],
): Facets {
  const facets: Facets = {};
  for (const field of fields) {
    if (field.rowValue === undefined) continue;
    const counts: Record<string, number> = {};
    for (const row of rows) {
      for (const value of rowValues(row, field)) {
        counts[value] = (counts[value] ?? 0) + 1;
      }
    }
    facets[facetKey(field)] = counts;
  }
  return facets;
}

/**
 * adoptFacets reads the `facets` member out of a list answer. An endpoint that
 * sends none, or sends something that is not a count map, reports undefined —
 * which is the signal for the chips to offer the declared vocabulary without
 * counts rather than to render nothing.
 */
export function adoptFacets(raw: unknown): Facets | undefined {
  if (raw === null || typeof raw !== 'object') return undefined;
  return adoptWireFacets((raw as Record<string, unknown>)['facets']);
}

// ---------------------------------------------------------------------------
// URL state
// ---------------------------------------------------------------------------

/**
 * filterParamName is what a field is called in the hash: the table's own key,
 * an underscore, and the field's key — `tasks_status`, `jobs_runner`. The
 * prefix is what keeps two tables on one route from reading each other's
 * filters, exactly as it does for their pages.
 */
export function filterParamName(urlKey: string, key: string): string {
  return urlKey === '' ? key : `${urlKey}_${key}`;
}

/**
 * readFilters pulls a selection out of a hash such as
 * `#/tasks?tasks_status=pending,blocked&tasks_project=p1`.
 *
 * A single-valued field takes the FIRST value of a hand-edited list rather
 * than refusing: a link is not a form, and half a filter is better than an
 * error page.
 */
export function readFilters(
  hash: string,
  urlKey: string,
  fields: readonly FilterField[],
): FilterValues {
  const queryAt = hash.indexOf('?');
  if (queryAt === -1) return {};
  const params = new URLSearchParams(hash.slice(queryAt + 1));

  const values: FilterValues = {};
  for (const field of fields) {
    const raw = params.get(filterParamName(urlKey, field.key));
    if (raw === null) continue;
    const parsed = raw
      .split(',')
      .map((value) => value.trim())
      .filter((value) => value !== '');
    if (parsed.length === 0) continue;
    values[field.key] = field.multi === true ? parsed : parsed.slice(0, 1);
  }
  return values;
}

/**
 * writeFilters renders a hash carrying the selection, leaving the route path
 * and every foreign parameter — the table's page, another table's state,
 * `?project=` — as it found them. An unselected field is deleted rather than
 * written empty, so an untouched filter leaves no trail.
 *
 * The commas stay commas. `URLSearchParams` would escape them to `%2C`, which
 * is legal, round-trips, and makes a shared link unreadable; a comma is a
 * permitted sub-delimiter in a query and this is exactly what it is for.
 */
export function writeFilters(
  hash: string,
  urlKey: string,
  fields: readonly FilterField[],
  values: FilterValues,
): string {
  const bare = hash.replace(/^#/, '');
  const queryAt = bare.indexOf('?');
  const path = queryAt === -1 ? bare : bare.slice(0, queryAt);
  const params = new URLSearchParams(queryAt === -1 ? '' : bare.slice(queryAt + 1));

  for (const field of fields) {
    const name = filterParamName(urlKey, field.key);
    const held = selected(values, field.key);
    if (held.length === 0) {
      params.delete(name);
      continue;
    }
    params.set(name, held.join(','));
  }

  const query = params.toString().replace(/%2C/g, ',');
  return query === '' ? `#${path}` : `#${path}?${query}`;
}
