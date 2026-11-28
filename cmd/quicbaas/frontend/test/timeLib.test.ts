/**
 * timeLib.test.ts
 *
 * The shared timestamp helpers (src/lib/time.ts).
 *
 * hasInstant is pinned directly rather than through a rendered view because
 * the bug it guards against is silent: Go's zero time is a syntactically valid
 * date, so every formatter renders it and nothing fails loudly (#550).
 */

import { describe, it, expect } from 'vitest';
import { formatDateTime, formatRelative, formatTimeOnly, hasInstant, toMillis } from '../src/lib/time.ts';

describe('hasInstant', () => {
  it('rejects Go’s zero time, which reaches the browser as a real date', () => {
    // Measured on the burn-in node: `retry_not_before` is a time.Time that
    // omitempty does NOT omit, so a task with no deferral shipped
    // "0001-01-01T00:00:00Z" and the view rendered "12/31/1" as though the
    // server had decided something.
    expect(hasInstant('0001-01-01T00:00:00Z')).toBe(false);
  });

  it('accepts a real timestamp', () => {
    expect(hasInstant('2026-09-05T01:55:26.478484118Z')).toBe(true);
  });

  it('treats absent, null and empty as absent', () => {
    expect(hasInstant(undefined)).toBe(false);
    expect(hasInstant(null)).toBe(false);
    expect(hasInstant('')).toBe(false);
  });

  it('treats a numeric zero as absent — an int64 UnixNano that was never set', () => {
    // The generic Task model carries `retry_not_before` as int64 nanoseconds
    // with omitempty; when it is present it is 0 only because nothing set it.
    expect(hasInstant(0)).toBe(false);
    expect(hasInstant(-1)).toBe(false);
    expect(hasInstant(Number.NaN)).toBe(false);
  });

  it('accepts a positive numeric timestamp in any unit', () => {
    expect(hasInstant(1_778_017_609)).toBe(true);
    expect(hasInstant(1_778_017_609_149_476_600)).toBe(true);
  });
});

describe('toMillis on Go’s zero time', () => {
  it('returns null for Go’s zero time', () => {
    expect(toMillis('0001-01-01T00:00:00Z')).toBeNull();
    expect(toMillis('0001-01-01T00:00:00.000Z')).toBeNull();
  });

  it('renders the zero time as the em-dash the formatters render for a missing field', () => {
    for (const format of [formatDateTime, formatTimeOnly, formatRelative]) {
      expect(format('0001-01-01T00:00:00Z')).toBe(format(null));
      expect(format('0001-01-01T00:00:00Z')).toBe('—');
    }
  });

  it('leaves a real timestamp near the epoch unaffected', () => {
    expect(toMillis('1970-01-01T00:00:01Z')).toBe(1000);
    expect(formatDateTime('1970-01-01T00:00:01Z')).not.toBe('—');
  });

  it('still parses a real ISO timestamp', () => {
    expect(toMillis('2026-09-05T01:55:26Z')).toBe(Date.parse('2026-09-05T01:55:26Z'));
  });
});
