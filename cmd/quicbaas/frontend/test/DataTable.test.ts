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

interface HarnessRow {
  id: string;
  name: string;
  count: number;
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
