/**
 * json_large_integers.test.ts
 *
 * The identity guarantee: a 64-bit id survives the wire.
 *
 * These assert on the REWRITTEN TEXT as well as the parsed value, because the
 * two failure modes are opposite and only the text distinguishes them — a
 * rewrite that quotes too little rounds an id, and one that quotes too much
 * corrupts an issue body that happens to contain digits.
 */

import { describe, it, expect } from 'vitest';
import {
  parseJSONPreservingLargeIntegers,
  quoteUnrepresentableIntegers,
} from '../src/json.js';

// Measured on the burn-in node — a real forge issue id for go-truststrap
// issue #24. It is ~198x Number.MAX_SAFE_INTEGER.
const REAL_ITEM_ID = '1788567351228350294';

describe('parseJSONPreservingLargeIntegers', () => {
  it('returns a uint64 id exactly, as a string', () => {
    const parsed = parseJSONPreservingLargeIntegers<{ id: string }>(
      `{"id": ${REAL_ITEM_ID}}`,
    );
    expect(parsed.id).toBe(REAL_ITEM_ID);
  });

  it('demonstrates the rounding this exists to prevent', () => {
    // The control: plain JSON.parse silently changes the identifier.
    const naive = JSON.parse(`{"id": ${REAL_ITEM_ID}}`) as { id: number };
    expect(String(naive.id)).not.toBe(REAL_ITEM_ID);
  });

  it('leaves representable integers as numbers', () => {
    const parsed = parseJSONPreservingLargeIntegers<{
      attempts: number;
      budget: number;
      zero: number;
      negative: number;
      max_safe: number;
    }>('{"attempts": 3, "budget": 5, "zero": 0, "negative": -42, "max_safe": 9007199254740991}');
    expect(parsed.attempts).toBe(3);
    expect(parsed.budget).toBe(5);
    expect(parsed.zero).toBe(0);
    expect(parsed.negative).toBe(-42);
    expect(parsed.max_safe).toBe(9007199254740991);
  });

  it('quotes the first unrepresentable integer and not the last representable one', () => {
    // 9007199254740991 is MAX_SAFE_INTEGER; 9007199254740993 is the first odd
    // integer above it that a double cannot hold.
    expect(quoteUnrepresentableIntegers('[9007199254740991,9007199254740993]')).toBe(
      '[9007199254740991,"9007199254740993"]',
    );
  });

  it('leaves floats alone — their imprecision is not an identity hazard', () => {
    expect(quoteUnrepresentableIntegers('{"cost": 0.1234567890123456789}')).toBe(
      '{"cost": 0.1234567890123456789}',
    );
    expect(quoteUnrepresentableIntegers('{"n": 1.5e300}')).toBe('{"n": 1.5e300}');
  });

  it('preserves a large NEGATIVE integer with its sign', () => {
    expect(quoteUnrepresentableIntegers('{"n": -1788567351228350294}')).toBe(
      '{"n": "-1788567351228350294"}',
    );
  });

  it('does not rewrite digits inside a string, even one that looks like JSON', () => {
    // This is the case a regular expression gets wrong. A forge issue body
    // routinely contains pasted JSON.
    const wire = `{"id": ${REAL_ITEM_ID}, "body": "the server answered {\\"id\\": 1788573392453174691}"}`;
    const parsed = parseJSONPreservingLargeIntegers<{ id: string; body: string }>(wire);
    expect(parsed.id).toBe(REAL_ITEM_ID);
    expect(parsed.body).toBe('the server answered {"id": 1788573392453174691}');
  });

  it('handles escaped backslashes and quotes without losing the string boundary', () => {
    const wire = String.raw`{"a": "ends with a backslash \\", "id": 1788567351228350294}`;
    const parsed = parseJSONPreservingLargeIntegers<{ a: string; id: string }>(wire);
    expect(parsed.a).toBe('ends with a backslash \\');
    expect(parsed.id).toBe(REAL_ITEM_ID);
  });

  it('preserves ids nested in arrays and objects', () => {
    const wire = `{"items":[{"id":${REAL_ITEM_ID},"attempts":2},{"id":1788573392453174691,"attempts":1}]}`;
    const parsed = parseJSONPreservingLargeIntegers<{
      items: { id: string; attempts: number }[];
    }>(wire);
    expect(parsed.items.map((i) => i.id)).toEqual([REAL_ITEM_ID, '1788573392453174691']);
    expect(parsed.items.map((i) => i.attempts)).toEqual([2, 1]);
  });

  it('rejects malformed JSON exactly as JSON.parse does', () => {
    expect(() => parseJSONPreservingLargeIntegers('{not json')).toThrow(SyntaxError);
    expect(() => parseJSONPreservingLargeIntegers('')).toThrow(SyntaxError);
  });

  it('leaves an unterminated string to JSON.parse rather than mangling it', () => {
    // The scanner must not loop or silently "repair" — it hands the input on
    // and JSON.parse reports it.
    expect(() => parseJSONPreservingLargeIntegers('{"a": "unterminated')).toThrow(SyntaxError);
  });
});
