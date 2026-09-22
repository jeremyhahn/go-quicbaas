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
 * DataTable's rendered behaviour: 25 rows in the DOM rather than the whole
 * collection, the pager's arithmetic as the operator sees it, the three-state
 * sort, the debounced search, and — the part a view must never have to think
 * about — the automatic choice between a paging endpoint and one that hands
 * over everything.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import DataTableHarness from './stubs/DataTableHarness.svelte';
import type { PageRequest } from '../src/lib/dataTable.ts';
import type { FilterField } from '../src/lib/filters.ts';

interface HarnessRow {
  id: string;
  name: string;
  count: number;
  status?: string;
}

/** A collection big enough that paging is the only way to see the end of it. */
function makeRows(n: number): HarnessRow[] {
  return Array.from({ length: n }, (_, i) => ({
    id: `id-${String(i).padStart(3, '0')}`,
    name: `row ${String(i).padStart(3, '0')}`,
    count: n - i,
  }));
}

function renderedIDs(): string[] {
  return screen.queryAllByTestId('cell-id').map((el) => el.textContent ?? '');
}

function range(): string {
  return screen.getByTestId('data-table-range').textContent?.trim() ?? '';
}

beforeEach(() => {
  window.location.hash = '#/harness';
});

// ---------------------------------------------------------------------------
// Client mode — paging
// ---------------------------------------------------------------------------

describe('DataTable paging', () => {
  it('renders one page of rows, not the whole collection', () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    expect(renderedIDs()).toHaveLength(25);
    expect(renderedIDs()[0]).toBe('id-000');
    expect(range()).toBe('1–25 of 60');
  });

  it('walks first, previous, next and last', async () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });

    await fireEvent.click(screen.getByLabelText('Next page'));
    expect(renderedIDs()[0]).toBe('id-025');
    expect(range()).toBe('26–50 of 60');

    await fireEvent.click(screen.getByLabelText('Last page'));
    expect(renderedIDs()).toHaveLength(10);
    expect(range()).toBe('51–60 of 60');

    await fireEvent.click(screen.getByLabelText('Previous page'));
    expect(range()).toBe('26–50 of 60');

    await fireEvent.click(screen.getByLabelText('First page'));
    expect(range()).toBe('1–25 of 60');
  });

  it('disables the pager at each end rather than paging past it', async () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    expect(screen.getByLabelText('First page')).toBeDisabled();
    expect(screen.getByLabelText('Previous page')).toBeDisabled();
    expect(screen.getByLabelText('Next page')).not.toBeDisabled();

    await fireEvent.click(screen.getByLabelText('Last page'));
    expect(screen.getByLabelText('Next page')).toBeDisabled();
    expect(screen.getByLabelText('Last page')).toBeDisabled();
  });

  it('changes page size and keeps the current rows in view', async () => {
    render(DataTableHarness, { props: { rows: makeRows(120) } });
    await fireEvent.click(screen.getByLabelText('Next page'));
    expect(renderedIDs()[0]).toBe('id-025');

    await fireEvent.change(screen.getByLabelText('Rows per page'), { target: { value: '50' } });
    expect(renderedIDs()).toHaveLength(50);
    // Row 25 sits on the first page of fifty, which is where the operator stays.
    expect(renderedIDs()[0]).toBe('id-000');
    expect(range()).toBe('1–50 of 120');
  });

  it('shows the empty state for a collection with no rows at all', () => {
    render(DataTableHarness, { props: { rows: [] } });
    expect(screen.getByText('No rows yet')).toBeInTheDocument();
    expect(range()).toBe('0 of 0');
  });

  it('offers no pager for a collection that fits on one page', () => {
    // Half the tables in this app are a handful of rows in a panel; a page
    // selector over three of them is noise. The count is still reported.
    render(DataTableHarness, { props: { rows: makeRows(3) } });
    expect(range()).toBe('1–3 of 3');
    expect(screen.queryByLabelText('Next page')).toBeNull();
    expect(screen.queryByLabelText('Rows per page')).toBeNull();
  });

  it('shows the spinner, not the empty state, while a view is loading', () => {
    render(DataTableHarness, { props: { rows: [], loading: true } });
    expect(screen.queryByText('No rows yet')).not.toBeInTheDocument();
    expect(document.querySelector('.loading-spinner')).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Client mode — sorting
// ---------------------------------------------------------------------------

describe('DataTable sorting', () => {
  it('cycles a header ascending, descending, then back to the collection order', async () => {
    render(DataTableHarness, { props: { rows: makeRows(4) } });
    const header = screen.getByLabelText('Sort by Count');

    await fireEvent.click(header);
    expect(screen.queryAllByTestId('cell-count').map((e) => e.textContent)).toEqual(['1', '2', '3', '4']);

    await fireEvent.click(header);
    expect(screen.queryAllByTestId('cell-count').map((e) => e.textContent)).toEqual(['4', '3', '2', '1']);

    await fireEvent.click(header);
    expect(screen.queryAllByTestId('cell-count').map((e) => e.textContent)).toEqual(['4', '3', '2', '1']);
    expect(renderedIDs()).toEqual(['id-000', 'id-001', 'id-002', 'id-003']);
  });

  it('reports the sort to assistive technology on the header cell', async () => {
    const { container } = render(DataTableHarness, { props: { rows: makeRows(4) } });
    const headers = Array.from(container.querySelectorAll('th'));
    const countHeader = headers[2];
    expect(countHeader?.getAttribute('aria-sort')).toBe('none');

    await fireEvent.click(screen.getByLabelText('Sort by Count'));
    expect(countHeader?.getAttribute('aria-sort')).toBe('ascending');
    await fireEvent.click(screen.getByLabelText('Sort by Count'));
    expect(countHeader?.getAttribute('aria-sort')).toBe('descending');
  });

  it('offers no sort control for a column that did not declare one', () => {
    render(DataTableHarness, { props: { rows: makeRows(4) } });
    expect(screen.queryByLabelText('Sort by ID')).toBeNull();
  });

  it('returns to the first page when the sort changes', async () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    await fireEvent.click(screen.getByLabelText('Next page'));
    expect(range()).toBe('26–50 of 60');
    await fireEvent.click(screen.getByLabelText('Sort by Name'));
    expect(range()).toBe('1–25 of 60');
  });

  it('starts from the sort a view declared', () => {
    render(DataTableHarness, { props: { rows: makeRows(4), defaultSort: 'count:desc' } });
    expect(screen.queryAllByTestId('cell-count').map((e) => e.textContent)).toEqual(['4', '3', '2', '1']);
  });
});

// ---------------------------------------------------------------------------
// Client mode — search
// ---------------------------------------------------------------------------

describe('DataTable search', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it('waits for the typing to stop before it filters', async () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    const box = screen.getByTestId('data-table-search');

    await fireEvent.input(box, { target: { value: 'row 04' } });
    // Still the unfiltered page: a keystroke must not re-run the table.
    expect(renderedIDs()).toHaveLength(25);

    await vi.advanceTimersByTimeAsync(120);
    expect(renderedIDs()).toHaveLength(25);

    await vi.advanceTimersByTimeAsync(200);
    expect(renderedIDs()).toEqual([
      'id-040', 'id-041', 'id-042', 'id-043', 'id-044',
      'id-045', 'id-046', 'id-047', 'id-048', 'id-049',
    ]);
    expect(range()).toBe('1–10 of 10');
  });

  it('runs one search for a burst of keystrokes', async () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    const box = screen.getByTestId('data-table-search');
    await fireEvent.input(box, { target: { value: 'r' } });
    await vi.advanceTimersByTimeAsync(100);
    await fireEvent.input(box, { target: { value: 'row 059' } });
    await vi.advanceTimersByTimeAsync(300);
    expect(renderedIDs()).toEqual(['id-059']);
  });

  it('says a search matched nothing without claiming the tenant has nothing', async () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    await fireEvent.input(screen.getByTestId('data-table-search'), { target: { value: 'nothing here' } });
    await vi.advanceTimersByTimeAsync(300);
    expect(screen.getByTestId('data-table-no-match')).toBeInTheDocument();
    expect(screen.queryByText('No rows yet')).not.toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Server mode
// ---------------------------------------------------------------------------

describe('DataTable server mode', () => {
  it('renders the page the endpoint returned and reports the server total', async () => {
    const load = vi.fn(async (req: PageRequest) => ({
      rows: makeRows(1987).slice(req.offset, req.offset + req.limit),
      total: 1987,
      limit: req.limit,
      offset: req.offset,
      next_offset: req.offset + req.limit,
    }));

    render(DataTableHarness, { props: { load, itemsKey: 'rows' } });

    await waitFor(() => { expect(range()).toBe('1–25 of 1,987'); });
    expect(renderedIDs()).toHaveLength(25);
    expect(load).toHaveBeenCalledWith({ limit: 25, offset: 0, sort: '', q: '' });
  });

  it('asks the endpoint for the next page rather than slicing locally', async () => {
    const load = vi.fn(async (req: PageRequest) => ({
      rows: makeRows(1987).slice(req.offset, req.offset + req.limit),
      total: 1987,
    }));
    render(DataTableHarness, { props: { load, itemsKey: 'rows' } });
    await waitFor(() => { expect(renderedIDs()).toHaveLength(25); });

    await fireEvent.click(screen.getByLabelText('Next page'));
    await waitFor(() => {
      expect(load).toHaveBeenLastCalledWith({ limit: 25, offset: 25, sort: '', q: '' });
    });
    await waitFor(() => { expect(range()).toBe('26–50 of 1,987'); });
  });

  it('sends the sort and the search to the endpoint', async () => {
    vi.useFakeTimers();
    try {
      const load = vi.fn(async () => ({ rows: makeRows(25), total: 400 }));
      render(DataTableHarness, { props: { load, itemsKey: 'rows' } });
      await vi.advanceTimersByTimeAsync(0);

      await fireEvent.click(screen.getByLabelText('Sort by Count'));
      await vi.advanceTimersByTimeAsync(0);
      expect(load).toHaveBeenLastCalledWith({ limit: 25, offset: 0, sort: 'count', q: '' });

      await fireEvent.click(screen.getByLabelText('Sort by Count'));
      await vi.advanceTimersByTimeAsync(0);
      expect(load).toHaveBeenLastCalledWith({ limit: 25, offset: 0, sort: 'count:desc', q: '' });

      await fireEvent.input(screen.getByTestId('data-table-search'), { target: { value: 'burn' } });
      await vi.advanceTimersByTimeAsync(300);
      expect(load).toHaveBeenLastCalledWith({ limit: 25, offset: 0, sort: 'count:desc', q: 'burn' });
    } finally {
      vi.useRealTimers();
    }
  });

  it('falls back to client paging when the endpoint returns no total', async () => {
    const load = vi.fn(async () => makeRows(60));
    render(DataTableHarness, { props: { load } });

    await waitFor(() => { expect(range()).toBe('1–25 of 60'); });
    expect(load).toHaveBeenCalledTimes(1);

    // The endpoint handed over everything, so page two is a local slice and
    // must not cost a second round trip.
    await fireEvent.click(screen.getByLabelText('Next page'));
    expect(renderedIDs()[0]).toBe('id-025');
    expect(load).toHaveBeenCalledTimes(1);
  });

  it('falls back for an envelope that carries rows but no total', async () => {
    const load = vi.fn(async () => ({ rows: makeRows(30) }));
    render(DataTableHarness, { props: { load, itemsKey: 'rows' } });
    await waitFor(() => { expect(range()).toBe('1–25 of 30'); });
    expect(load).toHaveBeenCalledTimes(1);
  });

  it('re-reads when the reload key changes, and reconsiders the mode', async () => {
    const load = vi.fn(async () => makeRows(4));
    const { rerender } = render(DataTableHarness, { props: { load, reloadKey: 'tenant-a' } });
    await waitFor(() => { expect(load).toHaveBeenCalledTimes(1); });

    await rerender({ load, reloadKey: 'tenant-b' });
    await waitFor(() => { expect(load).toHaveBeenCalledTimes(2); });
  });

  it('reports a failed read instead of rendering an empty table silently', async () => {
    const load = vi.fn(async () => { throw new Error('permission denied'); });
    render(DataTableHarness, { props: { load } });
    await waitFor(() => { expect(screen.getByRole('alert')).toHaveTextContent('permission denied'); });
  });

  it('does not call a failed read an empty collection', async () => {
    // "Nothing here yet — create your first one" is a claim about data. A read
    // nobody completed supports no such claim, so the empty state stays away
    // and the alert is the only answer on screen.
    const load = vi.fn(async () => { throw new Error('permission denied'); });
    render(DataTableHarness, { props: { load } });
    await waitFor(() => { expect(screen.getByTestId('data-table-unread')).toBeInTheDocument(); });
    expect(screen.queryByTestId('data-table-no-match')).toBeNull();
    expect(screen.queryByText(/Nothing here yet/i)).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// URL state
// ---------------------------------------------------------------------------

describe('DataTable URL state', () => {
  it('records the page in the route hash so a reload lands on it', async () => {
    render(DataTableHarness, { props: { rows: makeRows(120), urlKey: 'rows' } });
    await fireEvent.click(screen.getByLabelText('Next page'));
    await waitFor(() => { expect(window.location.hash).toContain('rows_page=2'); });
    expect(window.location.hash.startsWith('#/harness')).toBe(true);
  });

  it('opens on the page, sort and search a shared link names', () => {
    window.location.hash = '#/harness?rows_page=2&rows_sort=count:desc&rows_q=row';
    render(DataTableHarness, { props: { rows: makeRows(120), urlKey: 'rows' } });
    expect(range()).toBe('26–50 of 120');
    expect((screen.getByTestId('data-table-search') as HTMLInputElement).value).toBe('row');
    expect(screen.queryAllByTestId('cell-count')[0]?.textContent).toBe('95');
  });

  it('leaves the hash alone for a table nobody has touched', async () => {
    render(DataTableHarness, { props: { rows: makeRows(120), urlKey: 'rows' } });
    await waitFor(() => { expect(renderedIDs()).toHaveLength(25); });
    expect(window.location.hash).toBe('#/harness');
  });
});

// ---------------------------------------------------------------------------
// The pager, above the table and below it
// ---------------------------------------------------------------------------

describe('DataTable pager placement', () => {
  it('renders the pager at the TOP as well as the bottom', () => {
    // A pager only under twenty-five rows is below the fold on every laptop
    // in the building, which is how a paged table gets reported as having no
    // pagination at all.
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    expect(screen.getByTestId('data-table-pager-top')).toBeInTheDocument();
    expect(screen.getByTestId('data-table-pager-bottom')).toBeInTheDocument();
    expect(screen.getByLabelText('Next page')).toBeInTheDocument();
    expect(screen.getByLabelText('Next page (bottom)')).toBeInTheDocument();
  });

  it('drives one piece of state from either end', async () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    await fireEvent.click(screen.getByLabelText('Next page (bottom)'));
    // The top pager agrees, because there is nothing for them to disagree on.
    expect(range()).toBe('26–50 of 60');
    expect(screen.getByTestId('data-table-page').textContent?.trim()).toBe('2 / 3');
    expect(screen.getByTestId('data-table-page-bottom').textContent?.trim()).toBe('2 / 3');

    await fireEvent.click(screen.getByLabelText('First page'));
    expect(screen.getByTestId('data-table-range-bottom').textContent?.trim()).toBe('1–25 of 60');
  });

  it('puts the row-count label and the size selector at the top', () => {
    render(DataTableHarness, { props: { rows: makeRows(60) } });
    const top = screen.getByTestId('data-table-pager-top');
    expect(top).toContainElement(screen.getByTestId('data-table-range'));
    expect(top).toContainElement(screen.getByLabelText('Rows per page'));
    // One size selector, not two: the bottom pager repeats the paging
    // controls, not every control on the page.
    expect(screen.queryAllByLabelText('Rows per page')).toHaveLength(1);
  });

  it('keeps the pager once the operator has CHOSEN a size', async () => {
    // Growing the page past the collection must not hide the control that put
    // them there and strand them at 100 rows with no way back to 25.
    render(DataTableHarness, { props: { rows: makeRows(30) } });
    await fireEvent.change(screen.getByLabelText('Rows per page'), { target: { value: '100' } });
    expect(range()).toBe('1–30 of 30');
    expect(screen.getByLabelText('Next page')).toBeInTheDocument();
    expect(screen.getByLabelText('Rows per page')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// The sort affordance
// ---------------------------------------------------------------------------

describe('DataTable sort affordance', () => {
  it('marks an unsorted sortable header as sortable anyway', () => {
    // A header that looks like plain text until somebody happens to click it
    // is a control nobody finds. The neutral icon is the affordance.
    render(DataTableHarness, { props: { rows: makeRows(4) } });
    const header = screen.getByTestId('data-table-sort-count');
    expect(header).toHaveAttribute('data-sort', 'none');
    expect(header.querySelector('svg')).not.toBeNull();
    expect(header).toHaveAttribute('title', 'Sort by Count');
  });

  it('turns the icon into the direction it is sorting', async () => {
    render(DataTableHarness, { props: { rows: makeRows(4) } });
    await fireEvent.click(screen.getByLabelText('Sort by Count'));
    expect(screen.getByTestId('data-table-sort-count')).toHaveAttribute('data-sort', 'asc');
    await fireEvent.click(screen.getByLabelText('Sort by Count'));
    expect(screen.getByTestId('data-table-sort-count')).toHaveAttribute('data-sort', 'desc');
    await fireEvent.click(screen.getByLabelText('Sort by Count'));
    expect(screen.getByTestId('data-table-sort-count')).toHaveAttribute('data-sort', 'none');
  });

  it('gives an unsortable header no affordance at all', () => {
    // Which is how the two are told apart at a glance.
    render(DataTableHarness, { props: { rows: makeRows(4) } });
    expect(screen.queryByTestId('data-table-sort-id')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Faceted filters
// ---------------------------------------------------------------------------

const STATUS_FILTER: FilterField<HarnessRow> = {
  key: 'status',
  label: 'Status',
  param: 'statuses',
  multi: true,
  options: [
    { value: 'open', label: 'open' },
    { value: 'shut', label: 'shut' },
  ],
  rowValue: (row) => row.status ?? '',
};

/** Six rows: four open, two shut. */
function statusRows(): HarnessRow[] {
  return makeRows(6).map((row, i) => ({ ...row, status: i < 4 ? 'open' : 'shut' }));
}

describe('DataTable filters', () => {
  it('offers no filter bar for a table that declares none', () => {
    render(DataTableHarness, { props: { rows: makeRows(4) } });
    expect(screen.queryByTestId('filter-bar')).toBeNull();
  });

  it('counts the collection itself and narrows it, in client mode', async () => {
    render(DataTableHarness, { props: { rows: statusRows(), filters: [STATUS_FILTER] } });
    expect(screen.getByTestId('filter-count-status-open')).toHaveTextContent('4');
    expect(screen.getByTestId('filter-count-status-shut')).toHaveTextContent('2');

    await fireEvent.click(screen.getByTestId('filter-chip-status-shut'));
    expect(renderedIDs()).toEqual(['id-004', 'id-005']);
    // The count is of what MATCHED, as it is for the search: "1–2 of 6" would
    // claim four rows are on a page that does not exist.
    expect(range()).toBe('1–2 of 2');
  });

  it('counts the SEARCHED set, not the set its own filter left', async () => {
    render(DataTableHarness, { props: { rows: statusRows(), filters: [STATUS_FILTER] } });
    await fireEvent.click(screen.getByTestId('filter-chip-status-open'));
    // A facet counted after its own filter would read "open 4, shut 0" and
    // tell the operator nothing about what picking `shut` would do.
    expect(screen.getByTestId('filter-count-status-shut')).toHaveTextContent('2');
  });

  it('sends the selection to a paging endpoint and returns to page one', async () => {
    const load = vi.fn(async () => ({ rows: makeRows(25), total: 400 }));
    render(DataTableHarness, { props: { load, itemsKey: 'rows', filters: [STATUS_FILTER] } });
    await waitFor(() => { expect(load).toHaveBeenCalled(); });

    await fireEvent.click(screen.getByLabelText('Next page'));
    await waitFor(() => {
      expect(load).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 25 }));
    });

    await fireEvent.click(screen.getByTestId('filter-chip-status-open'));
    await waitFor(() => {
      // Page 3 of the old filter is not page 3 of the new one, and landing on
      // an empty page is how an operator concludes a filter is broken.
      expect(load).toHaveBeenLastCalledWith({
        limit: 25, offset: 0, sort: '', q: '', filters: { status: ['open'] },
      });
    });
  });

  it('carries the counts the ENDPOINT answered with', async () => {
    const load = vi.fn(async () => ({
      rows: makeRows(3),
      total: 3,
      facets: { status: { open: 91, shut: 8 } },
    }));
    render(DataTableHarness, { props: { load, itemsKey: 'rows', filters: [STATUS_FILTER] } });
    await waitFor(() => {
      expect(screen.getByTestId('filter-count-status-open')).toHaveTextContent('91');
    });
  });

  it('offers the vocabulary uncounted when the endpoint counts nothing', async () => {
    const load = vi.fn(async () => ({ rows: makeRows(3), total: 3 }));
    render(DataTableHarness, { props: { load, itemsKey: 'rows', filters: [STATUS_FILTER] } });
    await waitFor(() => {
      expect(screen.getByTestId('filter-chip-status-open')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('filter-count-status-open')).toBeNull();
  });

  it('says a FILTER emptied the table rather than that the tenant is empty', async () => {
    const open = statusRows().map((row) => ({ ...row, status: 'open' }));
    render(DataTableHarness, { props: { rows: open, filters: [STATUS_FILTER] } });
    await fireEvent.click(screen.getByTestId('filter-chip-status-shut'));
    expect(screen.getByTestId('data-table-no-match')).toBeInTheDocument();
    expect(screen.queryByText('No rows yet')).toBeNull();
  });

  it('puts the selection in the hash, so a narrowed table is a link', async () => {
    render(DataTableHarness, {
      props: { rows: statusRows(), filters: [STATUS_FILTER], urlKey: 'rows' },
    });
    await fireEvent.click(screen.getByTestId('filter-chip-status-shut'));
    await waitFor(() => {
      expect(window.location.hash).toBe('#/harness?rows_status=shut');
    });
  });

  it('opens narrowed when the link says so', () => {
    window.location.hash = '#/harness?rows_status=shut';
    render(DataTableHarness, {
      props: { rows: statusRows(), filters: [STATUS_FILTER], urlKey: 'rows' },
    });
    expect(renderedIDs()).toEqual(['id-004', 'id-005']);
    expect(screen.getByTestId('active-filters')).toHaveTextContent('Status: shut');
  });
});
