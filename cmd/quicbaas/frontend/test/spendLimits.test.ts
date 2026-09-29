import { describe, expect, it } from 'vitest';
import {
  emptySpendWindowRow,
  spendLimitsFromRows,
  spendRowsEqual,
  spendRowsFrom,
  spendRowsProblem,
} from '../src/lib/spendLimits.ts';

// go-quicbaas#1058: the one spend-limits shape, edited as rows.
describe('spendLimits rows', () => {
  const weekly = { windows: [{ name: 'weekly', size: 604_800_000_000_000, max_tokens: 5_000_000, max_equivalent_micros: 250_000_000 }] };

  it('round-trips the wire shape through rows', () => {
    const rows = spendRowsFrom(weekly);
    expect(rows).toEqual([{ name: 'weekly', hours: 168, tokens: 5_000_000, usd: 250 }]);
    expect(spendLimitsFromRows(rows)).toEqual(weekly);
    expect(spendRowsEqual(rows, weekly)).toBe(true);
  });

  it('leaves an axis capped at zero off the wire', () => {
    expect(spendLimitsFromRows([{ name: 'daily', hours: 24, tokens: 0, usd: 12.5 }])).toEqual({
      windows: [{ name: 'daily', size: 86_400_000_000_000, max_equivalent_micros: 12_500_000 }],
    });
  });

  it('reads no limits as no rows', () => {
    expect(spendRowsFrom(undefined)).toEqual([]);
    expect(spendRowsEqual([], {})).toBe(true);
    expect(spendRowsEqual([emptySpendWindowRow()], {})).toBe(false);
  });

  it('names the first row that cannot be saved', () => {
    expect(spendRowsProblem([{ name: 'daily', hours: 24, tokens: 1, usd: 0 }])).toBeNull();
    expect(spendRowsProblem([{ name: ' ', hours: 24, tokens: 1, usd: 0 }])).toMatch(/needs a name/);
    expect(spendRowsProblem([{ name: 'a', hours: 0, tokens: 1, usd: 0 }])).toMatch(/positive length/);
    expect(spendRowsProblem([{ name: 'a', hours: 1, tokens: -1, usd: 5 }])).toMatch(/cannot be negative/);
    expect(spendRowsProblem([{ name: 'a', hours: 1, tokens: 0, usd: 0 }])).toMatch(/caps nothing/);
    expect(
      spendRowsProblem([
        { name: 'a', hours: 1, tokens: 1, usd: 0 },
        { name: 'a', hours: 2, tokens: 1, usd: 0 },
      ]),
    ).toMatch(/used twice/);
  });
});
