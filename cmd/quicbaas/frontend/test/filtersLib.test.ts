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
 * The arithmetic behind faceted filtering: which chips are offered, what a
 * click does to a group, what the link then says, and what the counts are
 * counted over.
 *
 * These are the parts that are easy to get quietly wrong — a single-valued
 * field that accumulates, a facet counted after its own filter so every other
 * value reads zero, a link whose commas came back escaped — and every one of
 * them is a pure function precisely so it can be pinned down here rather than
 * through a rendered page.
 */

import { describe, it, expect } from 'vitest';
import {
  activeFilters,
  activeCount,
  adoptFacets,
  clearAll,
  clearField,
  clearValue,
  computeFacets,
  fieldOptions,
  filterByFacets,
  humanLabel,
  isSelected,
  matchesFilters,
  readFilters,
  rowValues,
  setValue,
  toggleValue,
  usesChips,
  writeFilters,
  type FilterField,
} from '../src/lib/filters.ts';
import { JOB_STATES } from '../src/lib/listFilters.ts';
import { ACTIVE_JOB_STATES } from '../src/stores/orchestrator.svelte.ts';

interface Row {
  id: string;
  status: string;
  tags: string[];
}

const STATUS: FilterField<Row> = {
  key: 'status',
  label: 'Status',
  param: 'statuses',
  multi: true,
  options: [
    { value: 'pending', label: 'pending' },
    { value: 'failed', label: 'failed' },
  ],
  rowValue: (row) => row.status,
};

const PROJECT: FilterField<Row> = {
  key: 'project',
  label: 'Project',
  param: 'project_id',
  options: [{ value: 'p1', label: 'go-truststrap' }],
  control: 'select',
};

const rows: Row[] = [
  { id: 'a', status: 'pending', tags: ['x'] },
  { id: 'b', status: 'pending', tags: ['x', 'y'] },
  { id: 'c', status: 'failed', tags: [] },
];

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

describe('fieldOptions', () => {
  it('counts each declared value from the answer, zero included', () => {
    const options = fieldOptions(STATUS, { status: { pending: 7 } });
    expect(options).toEqual([
      { value: 'pending', label: 'pending', count: 7 },
      // A declared value nobody counted reads 0 rather than vanishing: "no
      // failed tasks" is the answer, and hiding the chip unasks the question.
      { value: 'failed', label: 'failed', count: 0 },
    ]);
  });

  it('offers a value the server counted that this build never declared', () => {
    // A state added server-side after this bundle shipped must still be
    // selectable, or the UI silently loses a filter the API supports.
    const options = fieldOptions(STATUS, { status: { quarantined: 3 } });
    expect(options.map((o) => o.value)).toEqual(['pending', 'failed', 'quarantined']);
    expect(options[2]).toEqual({ value: 'quarantined', label: 'quarantined', count: 3 });
  });

  it('offers the declared vocabulary UNCOUNTED when nobody counted', () => {
    const options = fieldOptions(STATUS, undefined);
    expect(options.map((o) => o.count)).toEqual([null, null]);
  });

  it('reads an undeclared value as words rather than as wire text', () => {
    expect(humanLabel('in_progress')).toBe('in progress');
    expect(humanLabel('re-delegating')).toBe('re delegating');
    expect(humanLabel('')).toBe('');
  });
});

describe('usesChips', () => {
  it('always uses chips for a multi-valued field', () => {
    expect(usesChips(STATUS, 40)).toBe(true);
  });

  it('uses a picker for a single field with an unbounded vocabulary', () => {
    const many = { key: 'agent', label: 'Agent' };
    expect(usesChips(many, 4)).toBe(true);
    expect(usesChips(many, 40)).toBe(false);
  });

  it('honours an explicit control over the count', () => {
    expect(usesChips(PROJECT, 1)).toBe(false);
    expect(usesChips({ key: 'k', label: 'K', control: 'chips' }, 500)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Selection
// ---------------------------------------------------------------------------

describe('toggleValue', () => {
  it('accumulates values for a multi-valued field', () => {
    let values = toggleValue({}, STATUS, 'pending');
    values = toggleValue(values, STATUS, 'failed');
    expect(values).toEqual({ status: ['pending', 'failed'] });
  });

  it('switches a lit chip off with the control that switched it on', () => {
    const values = toggleValue({ status: ['pending', 'failed'] }, STATUS, 'pending');
    expect(values).toEqual({ status: ['failed'] });
  });

  it('replaces rather than accumulates for a single-valued field', () => {
    const values = toggleValue({ project: ['p1'] }, PROJECT, 'p2');
    expect(values).toEqual({ project: ['p2'] });
  });

  it('clears a single-valued field when its own value is clicked again', () => {
    expect(toggleValue({ project: ['p1'] }, PROJECT, 'p1')).toEqual({});
  });

  it('leaves no empty entry behind, so an untouched field holds nothing', () => {
    const values = toggleValue({ status: ['pending'] }, STATUS, 'pending');
    expect(Object.keys(values)).toEqual([]);
    expect(activeCount(values)).toBe(0);
  });
});

describe('setValue, clearValue, clearField and clearAll', () => {
  it('sets and clears one field from a picker', () => {
    expect(setValue({}, 'project', 'p1')).toEqual({ project: ['p1'] });
    expect(setValue({ project: ['p1'] }, 'project', '')).toEqual({});
  });

  it('drops one value, one field, or everything', () => {
    const values = { status: ['pending', 'failed'], project: ['p1'] };
    expect(clearValue(values, 'status', 'pending')).toEqual({
      status: ['failed'],
      project: ['p1'],
    });
    expect(clearField(values, 'status')).toEqual({ project: ['p1'] });
    expect(clearAll()).toEqual({});
  });

  it('is unmoved by a field or value nobody selected', () => {
    expect(clearValue({}, 'status', 'nope')).toEqual({});
    expect(clearField({ project: ['p1'] }, 'status')).toEqual({ project: ['p1'] });
    expect(isSelected({}, 'status', 'pending')).toBe(false);
  });
});

describe('activeFilters', () => {
  it('lists the selection as removable chips, in the declared order', () => {
    const active = activeFilters([STATUS, PROJECT], {
      project: ['p1'],
      status: ['failed'],
    }, { status: { failed: 2 } });
    expect(active).toEqual([
      { field: 'status', fieldLabel: 'Status', value: 'failed', valueLabel: 'failed' },
      { field: 'project', fieldLabel: 'Project', value: 'p1', valueLabel: 'go-truststrap' },
    ]);
  });

  it('shows a value out of a link that no vocabulary declared', () => {
    // A filter narrowing the list without saying so is worse than an odd chip.
    const active = activeFilters([STATUS], { status: ['quarantined'] });
    expect(active[0]?.valueLabel).toBe('quarantined');
  });
});

// ---------------------------------------------------------------------------
// Client-side narrowing
// ---------------------------------------------------------------------------

describe('matchesFilters and filterByFacets', () => {
  it('is OR within a field and AND across fields', () => {
    const TAGS: FilterField<Row> = {
      key: 'tag',
      label: 'Tag',
      multi: true,
      rowValue: (row) => row.tags,
    };
    const kept = filterByFacets(rows, [STATUS, TAGS], {
      status: ['pending', 'failed'],
      tag: ['y'],
    });
    expect(kept.map((r) => r.id)).toEqual(['b']);
  });

  it('keeps every row when nothing is selected', () => {
    expect(filterByFacets(rows, [STATUS], {})).toHaveLength(3);
  });

  it('leaves a field the row cannot answer for to the server', () => {
    // Applying it here as well would be wrong the moment the two disagreed.
    expect(matchesFilters(rows[0]!, [PROJECT], { project: ['p9'] })).toBe(true);
  });

  it('reads a missing, empty or multi value the same way', () => {
    const field: FilterField<Row> = { key: 'tag', label: 'Tag', rowValue: (r) => r.tags };
    expect(rowValues(rows[2]!, field)).toEqual([]);
    expect(rowValues(rows[1]!, field)).toEqual(['x', 'y']);
    expect(rowValues(rows[0]!, { key: 'k', label: 'K' })).toEqual([]);
    expect(rowValues(rows[0]!, { key: 'k', label: 'K', rowValue: () => null })).toEqual([]);
    expect(rowValues(rows[0]!, { key: 'k', label: 'K', rowValue: () => '' })).toEqual([]);
  });
});

describe('computeFacets', () => {
  it('counts the collection it is handed', () => {
    expect(computeFacets(rows, [STATUS])).toEqual({ status: { pending: 2, failed: 1 } });
  });

  it('counts nothing for a field the server owns', () => {
    expect(computeFacets(rows, [PROJECT])).toEqual({});
  });

  it('counts a row under every value it carries', () => {
    const TAGS: FilterField<Row> = { key: 'tag', label: 'Tag', rowValue: (r) => r.tags };
    expect(computeFacets(rows, [TAGS])).toEqual({ tag: { x: 2, y: 1 } });
  });
});

describe('adoptFacets', () => {
  it('reads the counts out of a listing answer', () => {
    expect(adoptFacets({ items: [], facets: { status: { pending: 3 } } })).toEqual({
      status: { pending: 3 },
    });
  });

  it('reports undefined for an answer that counted nothing', () => {
    // Undefined is "nobody counted" and drives the uncounted fallback; an
    // empty map would mean "every value has none", which is a different claim.
    expect(adoptFacets({ items: [] })).toBeUndefined();
    expect(adoptFacets(null)).toBeUndefined();
    expect(adoptFacets('nope')).toBeUndefined();
    expect(adoptFacets({ facets: 'nope' })).toBeUndefined();
    expect(adoptFacets({ facets: {} })).toBeUndefined();
  });

  it('keeps the numbers and drops whatever else turns up', () => {
    const facets = adoptFacets({ facets: { status: { ok: 2, bad: 'many' }, junk: 4 } });
    expect(facets).toEqual({ status: { ok: 2 } });
  });
});

// ---------------------------------------------------------------------------
// The link
// ---------------------------------------------------------------------------

describe('readFilters and writeFilters', () => {
  it('round-trips a selection through a hash, commas intact', () => {
    const hash = writeFilters('#/tasks', 'tasks', [STATUS, PROJECT], {
      status: ['pending', 'failed'],
      project: ['p1'],
    });
    // The comma stays a comma: %2C is legal and round-trips, and makes a
    // pasted link unreadable.
    expect(hash).toBe('#/tasks?tasks_status=pending,failed&tasks_project=p1');
    expect(readFilters(hash, 'tasks', [STATUS, PROJECT])).toEqual({
      status: ['pending', 'failed'],
      project: ['p1'],
    });
  });

  it('leaves the path and every foreign parameter exactly as it found them', () => {
    const hash = writeFilters(
      '#/tasks?tasks_page=3&jobs_status=failed',
      'tasks',
      [STATUS],
      { status: ['pending'] },
    );
    expect(hash).toContain('tasks_page=3');
    expect(hash).toContain('jobs_status=failed');
    expect(hash.startsWith('#/tasks?')).toBe(true);
  });

  it('deletes rather than writes an unselected field', () => {
    const hash = writeFilters('#/tasks?tasks_status=pending', 'tasks', [STATUS], {});
    expect(hash).toBe('#/tasks');
  });

  it('reads nothing out of a hash with no query at all', () => {
    expect(readFilters('#/tasks', 'tasks', [STATUS])).toEqual({});
  });

  it('takes the first value of a hand-edited list for a single field', () => {
    // A link is not a form: half a filter beats an error page.
    expect(readFilters('#/t?t_project=a,b', 't', [PROJECT])).toEqual({ project: ['a'] });
  });

  it('ignores an empty or blank value rather than narrowing to nothing', () => {
    expect(readFilters('#/t?t_status=', 't', [STATUS])).toEqual({});
    expect(readFilters('#/t?t_status=,%20,', 't', [STATUS])).toEqual({});
  });

  it('keeps two tables on one route out of one another\'s filters', () => {
    const hash = '#/board?tasks_status=pending&jobs_state=failed';
    expect(readFilters(hash, 'tasks', [STATUS])).toEqual({ status: ['pending'] });
    expect(readFilters(hash, 'jobs', [{ key: 'state', label: 'State', multi: true }])).toEqual({
      state: ['failed'],
    });
  });
});

// ---------------------------------------------------------------------------
// The vocabularies
// ---------------------------------------------------------------------------

describe('JOB_STATES', () => {
  it('offers every state a job can still leave, plus the terminals', () => {
    // The chips are spelled out in listFilters.ts so no view test has to
    // re-export the store's constant; this is the check that stands in for
    // the import that would have kept them in step.
    for (const state of ACTIVE_JOB_STATES) {
      expect(JOB_STATES).toContain(state);
    }
    expect(JOB_STATES).toContain('completed');
    expect(JOB_STATES).toContain('failed');
  });

  it('offers the active states before the terminal ones', () => {
    // The question is nearly always about what is still running.
    expect(JOB_STATES.indexOf('executing')).toBeLessThan(JOB_STATES.indexOf('completed'));
  });
});
