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
 * A faceted selection that lives in the route hash, for a list that is NOT a
 * DataTable.
 *
 * DataTable owns this for every table in the app, and most lists are tables.
 * Three are not — the fleet is a card grid, projects and accounts are
 * expandable rows — and they are lists all the same: the operator arrives
 * wanting the offline agents, the autonomous projects, the exhausted
 * accounts. Rebuilding them as tables to get a filter would throw away the
 * layout that suits what they show; writing the hash plumbing three more
 * times would be three more chances to spell `?fleet_status=` differently.
 *
 * So the plumbing is here, once, and the chips above a grid are the same
 * FilterBar with the same URL grammar as the chips above a table.
 */

import {
  clearAll,
  readFilters,
  writeFilters,
  type FilterField,
  type FilterValues,
} from './filters.ts';

export class FilterState {
  /** values is the current selection — read it, do not write it. */
  values = $state<FilterValues>({});

  readonly #urlKey: string;
  readonly #fields: readonly FilterField[];

  /**
   * The state opens on whatever the link says, so a pasted address lands on
   * the same narrowed list — the same contract DataTable gives a table.
   *
   * `fields` is only read for its KEYS and its multi-valuedness, so a caller
   * may pass the declaration with empty option lists; the options a picker
   * offers are the component's business, not the state's.
   */
  constructor(urlKey: string, fields: readonly FilterField[]) {
    this.#urlKey = urlKey;
    this.#fields = fields;
    if (typeof window !== 'undefined') {
      this.values = readFilters(window.location.hash, urlKey, fields);
    }
  }

  /**
   * apply takes the whole next selection and records it in the hash with
   * replaceState — narrowing a list is not navigation, and Back must leave
   * the page rather than walk back through every chip that was clicked.
   */
  apply(next: FilterValues): void {
    this.values = next;
    if (typeof window === 'undefined') return;
    const hash = writeFilters(window.location.hash, this.#urlKey, this.#fields, next);
    if (hash !== window.location.hash) {
      window.history.replaceState(window.history.state, '', hash);
    }
  }

  /** clear drops every selection. */
  clear(): void {
    this.apply(clearAll());
  }
}
