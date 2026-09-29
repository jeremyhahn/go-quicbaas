/**
 * spendLimits.ts — editing the one spend-limits shape an account, a project
 * and a tenant carry (go-quicbaas#1058).
 *
 * A window is edited as a row a person can read: a length in hours, a token
 * cap and a dollar cap, where zero leaves that axis unmetered. The wire shape
 * is nanoseconds and micro-dollars; the conversion lives here, once, so every
 * editor that sets limits converts the same way.
 */

import type { SpendLimits, SpendLimitWindow } from '@quicbaas/sdk';

const NANOS_PER_HOUR = 3_600_000_000_000;
const MICROS_PER_DOLLAR = 1_000_000;

/** SpendWindowRow is one editable window. */
export interface SpendWindowRow {
  name: string;
  /** hours is the window's length. */
  hours: number;
  /** tokens caps every metered token; 0 leaves the axis unmetered. */
  tokens: number;
  /** usd caps the list-price equivalent in dollars; 0 leaves the axis unmetered. */
  usd: number;
}

/** SPEND_WINDOW_PRESETS are the windows an operator most often sets. */
export const SPEND_WINDOW_PRESETS: { label: string; name: string; hours: number }[] = [
  { label: '5 hours', name: 'five-hour', hours: 5 },
  { label: 'Daily', name: 'daily', hours: 24 },
  { label: 'Weekly', name: 'weekly', hours: 168 },
  { label: 'Monthly (30 days)', name: 'monthly', hours: 720 },
];

/** emptySpendWindowRow is a new daily window with no caps yet. */
export function emptySpendWindowRow(): SpendWindowRow {
  return { name: 'daily', hours: 24, tokens: 0, usd: 0 };
}

/** spendRowsFrom renders stored limits as editable rows. */
export function spendRowsFrom(limits: SpendLimits | null | undefined): SpendWindowRow[] {
  return (limits?.windows ?? []).map((window) => ({
    name: window.name,
    hours: window.size / NANOS_PER_HOUR,
    tokens: window.max_tokens ?? 0,
    usd: (window.max_equivalent_micros ?? 0) / MICROS_PER_DOLLAR,
  }));
}

/** spendLimitsFromRows converts rows to the wire shape. */
export function spendLimitsFromRows(rows: SpendWindowRow[]): SpendLimits {
  return {
    windows: rows.map((row): SpendLimitWindow => {
      const window: SpendLimitWindow = {
        name: row.name.trim(),
        size: Math.round(row.hours * NANOS_PER_HOUR),
      };
      const tokens = Math.floor(row.tokens);
      if (tokens > 0) window.max_tokens = tokens;
      const micros = Math.round(row.usd * MICROS_PER_DOLLAR);
      if (micros > 0) window.max_equivalent_micros = micros;
      return window;
    }),
  };
}

/**
 * spendRowsProblem names the first row that cannot be saved, or null. The
 * server refuses the same things; saying so before the round trip is kinder.
 */
export function spendRowsProblem(rows: SpendWindowRow[]): string | null {
  const seen = new Set<string>();
  for (const [i, row] of rows.entries()) {
    const label = `Spend window ${i + 1}`;
    const name = row.name.trim();
    if (name === '') return `${label} needs a name.`;
    if (seen.has(name)) return `${label}: the name "${name}" is used twice.`;
    seen.add(name);
    if (!(row.hours > 0)) return `${label} needs a positive length.`;
    if (row.tokens < 0 || row.usd < 0) return `${label}: a cap cannot be negative.`;
    if (!(row.tokens > 0) && !(row.usd > 0)) return `${label} caps nothing: set tokens, dollars or both.`;
  }
  return null;
}

/** spendRowsEqual says whether rows describe the same limits as stored ones. */
export function spendRowsEqual(rows: SpendWindowRow[], limits: SpendLimits | null | undefined): boolean {
  return JSON.stringify(spendLimitsFromRows(rows)) === JSON.stringify(spendLimitsFromRows(spendRowsFrom(limits)));
}
