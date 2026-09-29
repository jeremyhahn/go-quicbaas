/**
 * BreakdownBars.test.ts
 *
 * The dashboard's state breakdown: bars scale to the largest count, a state
 * with nothing in it still has its row (reading 0), and each bar is the
 * colour of that state's status tone.
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import BreakdownBars from '../src/components/core/BreakdownBars.svelte';
import { STATUS_TONES } from '../src/lib/theme/tokens.ts';

function bars(): HTMLElement[] {
  return screen.getAllByTestId('breakdown-bar');
}

describe('BreakdownBars', () => {
  it('scales every bar to the largest count', () => {
    render(BreakdownBars, {
      props: {
        ariaLabel: 'Job states',
        rows: [
          { label: 'executing', count: 2 },
          { label: 'completed', count: 4 },
          { label: 'failed', count: 1 },
        ],
      },
    });
    expect(bars().map((bar) => bar.style.width)).toEqual(['50%', '100%', '25%']);
    expect(screen.getByRole('list', { name: 'Job states' })).toBeInTheDocument();
  });

  it('keeps a row for a state with nothing in it, and draws no bar for all-zero data', () => {
    render(BreakdownBars, {
      props: {
        ariaLabel: 'Agents',
        rows: [
          { label: 'running', count: 0 },
          { label: 'error', count: 0 },
        ],
      },
    });
    const rows = screen.getAllByTestId('breakdown-row');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('0');
    expect(bars().map((bar) => bar.style.width)).toEqual(['0%', '0%']);
  });

  it('colours each bar with its state tone', () => {
    render(BreakdownBars, {
      props: { ariaLabel: 'Job states', rows: [{ label: 'failed', count: 1 }] },
    });
    const expected = STATUS_TONES.dark.error.fg;
    const probe = document.createElement('span');
    probe.style.backgroundColor = expected;
    expect(bars()[0]!.style.backgroundColor).toBe(probe.style.backgroundColor);
  });
});
