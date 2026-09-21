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
 * The arithmetic behind DataTable. These are the cases a rendered table makes
 * expensive to reach: the last page of an uneven collection, a page index that
 * outlived the rows under it, an envelope that does not page.
 */

import { describe, it, expect } from 'vitest';
import {
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
  adoptPage,
  clampPage,
  columnValue,
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
  type TableState,
} from '../src/lib/dataTable.ts';

interface Row {
  id: string;
  name: string;
  count: number;
}

const rows: Row[] = [
  { id: 'c', name: 'charlie', count: 9 },
  { id: 'a', name: 'alpha', count: 10 },
  { id: 'b', name: 'Bravo', count: 2 },
];

const columns: Column<Row>[] = [
  { key: 'id', label: 'ID', sortable: true, searchable: true },
  { key: 'name', label: 'Name', sortable: true, searchable: true },
  { key: 'count', label: 'Count', sortable: true },
];

const defaults: TableState = { page: 0, size: DEFAULT_PAGE_SIZE, sort: '', q: '' };

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

describe('columnValue', () => {
  it('reads the row member named by the column key', () => {
    expect(columnValue(rows[0] as Row, columns[1] as Column<Row>)).toBe('charlie');
  });

  it('prefers a declared accessor and reads absent values as empty', () => {
    const derived: Column<Row> = { key: 'label', label: 'Label', value: (r) => r.name.toUpperCase() };
    expect(columnValue(rows[1] as Row, derived)).toBe('ALPHA');
    const missing: Column<Row> = { key: 'nope', label: 'Nope' };
    expect(columnValue(rows[1] as Row, missing)).toBe('');
  });
});

// ---------------------------------------------------------------------------
// Sorting
// ---------------------------------------------------------------------------

describe('nextDirection', () => {
  it('cycles one column ascending, descending, off', () => {
    const first = nextDirection('', 'none', 'name');
    expect(first).toEqual({ key: 'name', dir: 'asc' });
    const second = nextDirection(first.key, first.dir, 'name');
    expect(second).toEqual({ key: 'name', dir: 'desc' });
    const third = nextDirection(second.key, second.dir, 'name');
    expect(third).toEqual({ key: '', dir: 'none' });
  });

  it('starts a different column ascending rather than inheriting a direction', () => {
    expect(nextDirection('name', 'desc', 'count')).toEqual({ key: 'count', dir: 'asc' });
  });
});

describe('sortRows', () => {
  it('orders text case-insensitively in both directions', () => {
    expect(sortRows(rows, columns, 'name', 'asc').map((r) => r.id)).toEqual(['a', 'b', 'c']);
    expect(sortRows(rows, columns, 'name', 'desc').map((r) => r.id)).toEqual(['c', 'b', 'a']);
  });

  it('orders numbers numerically, not as text', () => {
    expect(sortRows(rows, columns, 'count', 'asc').map((r) => r.count)).toEqual([2, 9, 10]);
  });

  it('keeps the collection order when sorting is off or the column is unknown', () => {
    expect(sortRows(rows, columns, 'name', 'none').map((r) => r.id)).toEqual(['c', 'a', 'b']);
    expect(sortRows(rows, columns, 'nothing', 'asc').map((r) => r.id)).toEqual(['c', 'a', 'b']);
  });

  it('does not mutate the collection it was handed', () => {
    const original = [...rows];
    sortRows(rows, columns, 'name', 'asc');
    expect(rows).toEqual(original);
  });
});

// ---------------------------------------------------------------------------
// Searching
// ---------------------------------------------------------------------------

describe('filterRows', () => {
  it('matches searchable columns case-insensitively', () => {
    expect(filterRows(rows, columns, 'BRA').map((r) => r.id)).toEqual(['b']);
  });

  it('ignores columns that did not declare themselves searchable', () => {
    expect(filterRows(rows, columns, '10')).toHaveLength(0);
  });

  it('keeps every row for an empty query or a table with nothing searchable', () => {
    expect(filterRows(rows, columns, '   ')).toHaveLength(3);
    const unsearchable: Column<Row>[] = [{ key: 'id', label: 'ID' }];
    expect(filterRows(rows, unsearchable, 'zzz')).toHaveLength(3);
  });
});

// ---------------------------------------------------------------------------
// Paging
// ---------------------------------------------------------------------------

describe('paging arithmetic', () => {
  it('counts the pages of an uneven collection', () => {
    expect(pageCount(1987, 25)).toBe(80);
    expect(pageCount(50, 25)).toBe(2);
    expect(pageCount(0, 25)).toBe(1);
  });

  it('clamps a page index that outlived the rows under it', () => {
    expect(clampPage(40, 3, 25)).toBe(0);
    expect(clampPage(79, 1987, 25)).toBe(79);
    expect(clampPage(-4, 1987, 25)).toBe(0);
  });

  it('slices the rows of one page, including a short last page', () => {
    const many = Array.from({ length: 53 }, (_, i) => ({ id: String(i), name: 'n', count: i }));
    expect(sliceRows(many, 0, 25)).toHaveLength(25);
    expect(sliceRows(many, 2, 25)).toHaveLength(3);
    expect(sliceRows(many, 2, 25)[0]?.count).toBe(50);
  });

  it('slices a page past the end back to the last page that exists', () => {
    const many = Array.from({ length: 53 }, (_, i) => ({ id: String(i), name: 'n', count: i }));
    expect(sliceRows(many, 99, 25)[0]?.count).toBe(50);
  });

  it('labels the range the way the pager reads it', () => {
    expect(rangeLabel(0, 25, 1987)).toBe('1–25 of 1,987');
    expect(rangeLabel(50, 3, 53)).toBe('51–53 of 53');
    expect(rangeLabel(0, 0, 0)).toBe('0 of 0');
  });

  it('offers exactly the three page sizes the pager renders', () => {
    expect(PAGE_SIZES).toEqual([25, 50, 100]);
    expect(DEFAULT_PAGE_SIZE).toBe(25);
  });
});

// ---------------------------------------------------------------------------
// The sort parameter
// ---------------------------------------------------------------------------

describe('sort parameter', () => {
  it('renders and reads back the wire spelling', () => {
    expect(sortParam('started_at', 'desc')).toBe('started_at:desc');
    expect(sortParam('started_at', 'asc')).toBe('started_at');
    expect(sortParam('started_at', 'none')).toBe('');
    expect(parseSortParam('started_at:desc')).toEqual({ key: 'started_at', dir: 'desc' });
    expect(parseSortParam('started_at')).toEqual({ key: 'started_at', dir: 'asc' });
  });

  it('reads nonsense as no sort rather than a sort on nothing', () => {
    expect(parseSortParam('')).toEqual({ key: '', dir: 'none' });
    expect(parseSortParam('  ')).toEqual({ key: '', dir: 'none' });
    expect(parseSortParam(':desc')).toEqual({ key: '', dir: 'none' });
    expect(parseSortParam('name:sideways')).toEqual({ key: 'name', dir: 'asc' });
  });
});

// ---------------------------------------------------------------------------
// The response envelope
// ---------------------------------------------------------------------------

describe('adoptPage', () => {
  it('reports server mode when the envelope carries a total', () => {
    const page = adoptPage<Row>({ jobs: [rows[0]], total: 1987, limit: 25, offset: 0, next_offset: 25 }, 'jobs');
    expect(page.total).toBe(1987);
    expect(page.items).toHaveLength(1);
  });

  it('reports client mode for the shapes that do not page', () => {
    expect(adoptPage<Row>(rows).total).toBeNull();
    expect(adoptPage<Row>(rows).items).toHaveLength(3);
    expect(adoptPage<Row>({ jobs: rows }, 'jobs').total).toBeNull();
  });

  it('finds the rows without being told the member name', () => {
    const page = adoptPage<Row>({ timestamp: 5, sessions: rows, total: 3 });
    expect(page.items).toHaveLength(3);
    expect(page.total).toBe(3);
  });

  it('answers empty for a body that carries no rows at all', () => {
    expect(adoptPage<Row>(null).items).toEqual([]);
    expect(adoptPage<Row>('nope').items).toEqual([]);
    expect(adoptPage<Row>({ jobs: null, total: 0 }, 'jobs')).toEqual({ items: [], total: 0 });
  });
});

// ---------------------------------------------------------------------------
// URL state
// ---------------------------------------------------------------------------

describe('URL state', () => {
  it('round-trips page, size, sort and search through the hash', () => {
    const state: TableState = { page: 2, size: 50, sort: 'started_at:desc', q: 'burn' };
    const hash = writeState('#/jobs', 'jobs', state, defaults);
    expect(hash).toContain('jobs_page=3');
    expect(hash).toContain('jobs_size=50');
    expect(readState(hash, 'jobs', defaults)).toEqual(state);
  });

  it('leaves the route path and foreign parameters untouched', () => {
    const hash = writeState('#/tasks?project=p1', 'tasks', { page: 1, size: 25, sort: '', q: '' }, defaults);
    expect(hash.startsWith('#/tasks?')).toBe(true);
    expect(hash).toContain('project=p1');
    expect(hash).toContain('tasks_page=2');
  });

  it('writes nothing for a table still in its default state', () => {
    expect(writeState('#/jobs', 'jobs', defaults, defaults)).toBe('#/jobs');
  });

  it('falls back to the defaults for a hand-edited link it cannot read', () => {
    const state = readState('#/jobs?jobs_page=abc&jobs_size=7', 'jobs', defaults);
    expect(state.page).toBe(0);
    expect(state.size).toBe(DEFAULT_PAGE_SIZE);
    expect(readState('#/jobs', 'jobs', defaults)).toEqual(defaults);
  });

  it('keeps two tables on one route out of one another\'s state', () => {
    const first = writeState('#/dashboard', 'jobs', { page: 1, size: 25, sort: '', q: '' }, defaults);
    const both = writeState(first, 'agents', { page: 3, size: 25, sort: '', q: '' }, defaults);
    expect(readState(both, 'jobs', defaults).page).toBe(1);
    expect(readState(both, 'agents', defaults).page).toBe(3);
  });
});
