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
 * FilterBar as the operator meets it: the counts beside each value, what a
 * click does to a group, the chips that say what is narrowing the list and
 * undo it, and the keyboard and assistive-technology contract — a chip is a
 * button with `aria-pressed`, not a div that happens to change colour.
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import FilterBarHarness from './stubs/FilterBarHarness.svelte';
import type { FilterField } from '../src/lib/filters.ts';

const STATUS: FilterField = {
  key: 'status',
  label: 'Status',
  param: 'statuses',
  multi: true,
  options: [
    { value: 'pending', label: 'pending' },
    { value: 'blocked', label: 'blocked' },
    { value: 'failed', label: 'failed' },
  ],
};

const PROJECT: FilterField = {
  key: 'project',
  label: 'Project',
  param: 'project_id',
  control: 'select',
  options: [
    { value: 'p1', label: 'go-truststrap' },
    { value: 'p2', label: 'go-qrdb' },
  ],
};

const DISPATCH: FilterField = {
  key: 'dispatch',
  label: 'Dispatch',
  options: [
    { value: 'true', label: 'auto-spawned' },
    { value: 'false', label: 'queued' },
  ],
};

const FACETS = { status: { pending: 12, blocked: 3 } };

let changes: unknown[] = [];

beforeEach(() => {
  changes = [];
});

function onchange(next: unknown): void {
  changes.push(next);
}

// ---------------------------------------------------------------------------
// What it offers
// ---------------------------------------------------------------------------

describe('FilterBar — the vocabulary and the counts', () => {
  it('renders a chip per value with the count behind it', () => {
    render(FilterBarHarness, { props: { fields: [STATUS], facets: FACETS } });
    expect(screen.getByTestId('filter-count-status-pending')).toHaveTextContent('12');
    expect(screen.getByTestId('filter-count-status-blocked')).toHaveTextContent('3');
    // A declared value nobody counted reads 0 — "there are none" is an answer.
    expect(screen.getByTestId('filter-count-status-failed')).toHaveTextContent('0');
  });

  it('offers the vocabulary without counts when nobody counted', () => {
    render(FilterBarHarness, { props: { fields: [STATUS] } });
    expect(screen.getByTestId('filter-chip-status-pending')).toBeInTheDocument();
    expect(screen.queryByTestId('filter-count-status-pending')).toBeNull();
  });

  it('uses a picker for an unbounded field and chips for a bounded one', () => {
    render(FilterBarHarness, { props: { fields: [STATUS, PROJECT] } });
    expect(screen.getByTestId('filter-select-project')).toBeInTheDocument();
    expect(screen.queryByTestId('filter-chip-project-p1')).toBeNull();
    expect(screen.getByTestId('filter-chip-status-pending')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Selecting
// ---------------------------------------------------------------------------

describe('FilterBar — selecting', () => {
  it('accumulates chips for a field the API takes a list of', async () => {
    render(FilterBarHarness, { props: { fields: [STATUS], onchange } });
    await fireEvent.click(screen.getByTestId('filter-chip-status-pending'));
    await fireEvent.click(screen.getByTestId('filter-chip-status-blocked'));
    expect(changes.at(-1)).toEqual({ status: ['pending', 'blocked'] });
  });

  it('replaces the selection for a field the API takes one of', async () => {
    render(FilterBarHarness, { props: { fields: [DISPATCH], onchange } });
    await fireEvent.click(screen.getByTestId('filter-chip-dispatch-true'));
    await fireEvent.click(screen.getByTestId('filter-chip-dispatch-false'));
    expect(changes.at(-1)).toEqual({ dispatch: ['false'] });
  });

  it('reports a chip as pressed, not merely as a different colour', async () => {
    render(FilterBarHarness, { props: { fields: [STATUS] } });
    const chip = screen.getByTestId('filter-chip-status-pending');
    expect(chip).toHaveAttribute('aria-pressed', 'false');
    await fireEvent.click(chip);
    expect(screen.getByTestId('filter-chip-status-pending')).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('is operable from the keyboard alone', async () => {
    const user = userEvent.setup();
    render(FilterBarHarness, { props: { fields: [STATUS], onchange } });
    // A chip is a <button>, so Tab reaches it and Enter presses it. Nothing
    // in this bar is a click-only div.
    await user.tab();
    expect(screen.getByTestId('filter-chip-status-pending')).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(changes.at(-1)).toEqual({ status: ['pending'] });
  });

  it('carries a choice made in a picker back as that field selection', async () => {
    render(FilterBarHarness, { props: { fields: [PROJECT], onchange } });
    await fireEvent.change(screen.getByTestId('filter-select-project'), {
      target: { value: 'p2' },
    });
    expect(changes.at(-1)).toEqual({ project: ['p2'] });
  });
});

// ---------------------------------------------------------------------------
// Seeing and undoing
// ---------------------------------------------------------------------------

describe('FilterBar — the active selection', () => {
  it('shows what is narrowing the list, by name', () => {
    render(FilterBarHarness, {
      props: { fields: [STATUS, PROJECT], facets: FACETS, initial: { status: ['blocked'], project: ['p1'] } },
    });
    const chips = screen.getByTestId('active-filters');
    expect(chips).toHaveTextContent('Status: blocked');
    // The project reads as its NAME: an operator cannot recognise a uuid.
    expect(chips).toHaveTextContent('Project: go-truststrap');
  });

  it('shows nothing at all when nothing is selected', () => {
    render(FilterBarHarness, { props: { fields: [STATUS] } });
    expect(screen.queryByTestId('active-filters')).toBeNull();
  });

  it('removes one value from its own chip', async () => {
    render(FilterBarHarness, {
      props: { fields: [STATUS], initial: { status: ['pending', 'blocked'] }, onchange },
    });
    await fireEvent.click(screen.getByTestId('active-filter-remove-status-pending'));
    expect(changes.at(-1)).toEqual({ status: ['blocked'] });
  });

  it('clears one field, and clears everything', async () => {
    render(FilterBarHarness, {
      props: {
        fields: [STATUS, PROJECT],
        initial: { status: ['pending'], project: ['p1'] },
        onchange,
      },
    });
    await fireEvent.click(screen.getByTestId('filter-clear-status'));
    expect(changes.at(-1)).toEqual({ project: ['p1'] });

    await fireEvent.click(screen.getByTestId('filter-clear-all'));
    expect(changes.at(-1)).toEqual({});
  });

  it('offers no per-field clear for a field nobody has touched', () => {
    render(FilterBarHarness, { props: { fields: [STATUS] } });
    expect(screen.queryByTestId('filter-clear-status')).toBeNull();
  });

  it('shows and can remove a value a hand-edited link named', () => {
    // A filter narrowing the list with no way to see or undo it is the trap
    // this row exists to prevent.
    render(FilterBarHarness, {
      props: { fields: [STATUS], initial: { status: ['quarantined'] } },
    });
    expect(screen.getByTestId('active-filters')).toHaveTextContent('Status: quarantined');
    expect(
      screen.getByTestId('active-filter-remove-status-quarantined'),
    ).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Degenerate input
// ---------------------------------------------------------------------------

describe('FilterBar — a field with nothing to offer', () => {
  it('says so rather than rendering an empty group', () => {
    const empty: FilterField = { key: 'backend', label: 'Backend', multi: true };
    render(FilterBarHarness, { props: { fields: [empty] } });
    expect(screen.getByTestId('filter-field-backend')).toHaveTextContent('none');
  });

  it('renders nothing at all for a bar with no fields', () => {
    render(FilterBarHarness, { props: { fields: [] } });
    expect(screen.getByTestId('filter-bar').textContent?.trim()).toBe('');
  });
});
