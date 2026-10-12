// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
 * Lossless parsing of JSON that carries 64-bit integer identifiers.
 *
 * THE PROBLEM IS AN IDENTITY ONE, NOT A ROUNDING ONE. QuicBaaS mints entity
 * identifiers as Go `uint64` (Snowflake), and Go's encoder writes them as bare
 * JSON numbers. A real one measured on the burn-in node:
 *
 *   {"id": 1788567351228350294, ...}
 *
 * `JSON.parse` produces IEEE-754 doubles, so every integer above
 * `Number.MAX_SAFE_INTEGER` (9007199254740991) is silently rounded — that id
 * arrives as 1788567351228350200. Nothing throws; the value simply stops being
 * the identifier the server issued, and the next request built from it asks for
 * a record that does not exist. A UI that navigates by id would 404 on every
 * row while displaying an id that looks entirely plausible.
 *
 * `parseJSONPreservingLargeIntegers` quotes exactly those literals before
 * parsing, so an unrepresentable integer arrives as its exact decimal STRING
 * and a representable one arrives unchanged as a number.
 *
 * IDENTITIES BECOME STRINGS, QUANTITIES STAY NUMBERS. Go `time.UnixNano`
 * timestamps exceed 2^53 too, and those are quantities: rounding one costs
 * less than a microsecond, and callers compare and convert them as numbers.
 * So the rewrite is keyed on the member name, with no per-endpoint table:
 *
 *   - an integer under a key named `id`, `ids`, `*_id`, `*_ids`, `*Id`,
 *     `*Ids`, `*ID` or `*IDs` — directly or inside an array under that key —
 *     is an identity, and arrives as its exact decimal string when it is
 *     past 2^53;
 *   - an integer under any other key (`created_at`, `timestamp`, `size`,
 *     `sequence`, …) is a quantity, and parses exactly as `JSON.parse` would;
 *   - an integer with no key at all (a bare top-level value, or an array at
 *     the top level) has no name to judge it by, and is kept exact.
 *
 * Every 64-bit integer the daemon serialises was checked against this rule
 * when it was adopted (#555): the id-named ones are identities and every other
 * one is a timestamp, a counter or a size.
 *
 * WHY A SCANNER AND NOT A REGULAR EXPRESSION. The obvious shortcut — rewriting
 * `/"id":\s*(\d+)/` — cannot tell a number from text that looks like one, and
 * these payloads carry free-form user text: a forge issue body routinely
 * contains pasted JSON. A regular expression
 * would rewrite inside that string, corrupting the very field an operator is
 * reading. The scanner below copies string literals verbatim and only ever
 * examines tokens in value position, so the content of a string cannot
 * influence the result.
 */

/**
 * parseJSONPreservingLargeIntegers parses JSON text, returning any identity
 * integer that cannot be represented exactly as a `number` as its decimal
 * string instead.
 *
 * Every other value — quantities such as UnixNano timestamps, and floats,
 * whose imprecision is inherent rather than an identity hazard — parses
 * exactly as `JSON.parse` would.
 *
 * Throws the same `SyntaxError` as `JSON.parse` for malformed input: this is a
 * parse, and input that is not JSON must fail as loudly here as it would there.
 */
export function parseJSONPreservingLargeIntegers<T>(text: string): T {
  return JSON.parse(quoteUnrepresentableIntegers(text)) as T;
}

/**
 * quoteUnrepresentableIntegers rewrites JSON text so that every identity
 * integer literal (see the key rule above) that does not survive a `number`
 * round-trip is wrapped in double quotes.
 *
 * Exported for the tests, which assert on the rewritten TEXT: the rewrite is
 * the part that has to be exactly right, and asserting only on the parsed
 * object cannot distinguish "left the literal alone" from "quoted something it
 * should not have".
 */
export function quoteUnrepresentableIntegers(text: string): string {
  // One frame per open container. An object frame's `key` is the member name
  // last read in it; an array frame's `key` is the name the array itself sits
  // under, so every element inherits it.
  const frames: ContainerFrame[] = [];
  let out = '';
  let i = 0;

  while (i < text.length) {
    const ch = text[i] as string;
    const top = frames[frames.length - 1];

    // A string literal is copied verbatim, escapes included. This is the
    // clause that makes the rewrite safe against payloads whose text content
    // looks like JSON.
    if (ch === '"') {
      const end = endOfStringLiteral(text, i);
      const literal = text.slice(i, end);
      if (top?.kind === 'object' && top.expectingKey) {
        top.key = decodeKey(literal);
      }
      out += literal;
      i = end;
      continue;
    }

    // Outside a string, a digit or a minus sign can only begin a number
    // literal: JSON object keys are always strings, and the only other bare
    // tokens are `true`, `false` and `null`, none of which start this way.
    if (ch === '-' || (ch >= '0' && ch <= '9')) {
      const end = endOfNumberLiteral(text, i);
      const literal = text.slice(i, end);
      const quote = isIdentityKey(top?.key) && needsQuoting(literal);
      out += quote ? `"${literal}"` : literal;
      i = end;
      continue;
    }

    switch (ch) {
      case '{':
        frames.push({ kind: 'object', key: undefined, expectingKey: true });
        break;
      case '[':
        frames.push({ kind: 'array', key: top?.key, expectingKey: false });
        break;
      case '}':
      case ']':
        frames.pop();
        break;
      case ':':
        if (top) top.expectingKey = false;
        break;
      case ',':
        if (top?.kind === 'object') top.expectingKey = true;
        break;
    }

    out += ch;
    i++;
  }

  return out;
}

/** ContainerFrame is the scanner's view of one open object or array. */
interface ContainerFrame {
  kind: 'object' | 'array';
  key: string | undefined;
  expectingKey: boolean;
}

// IDENTITY_KEY matches the member names whose integers are identities:
// `id`, `ids`, `*_id`, `*_ids`, `*Id`, `*Ids`, `*ID`, `*IDs`.
const IDENTITY_KEY = /(^|_)ids?$|(Id|ID)s?$/;

/**
 * isIdentityKey reports whether an integer under `key` names an entity. A
 * value with no key — the top level, or an array there — has nothing to judge
 * it by, so it is kept exact: a rounded quantity is cheap, a rounded id is not.
 */
function isIdentityKey(key: string | undefined): boolean {
  return key === undefined || IDENTITY_KEY.test(key);
}

/**
 * decodeKey returns the member name a key literal spells. Only a literal with
 * an escape needs `JSON.parse`; an unterminated one is left to the caller's
 * parse to reject, as `endOfStringLiteral` explains.
 */
function decodeKey(literal: string): string {
  if (!literal.includes('\\')) return literal.slice(1, -1);
  try {
    return JSON.parse(literal) as string;
  } catch {
    return literal;
  }
}

/**
 * endOfStringLiteral returns the index one past the closing quote of the string
 * literal starting at `start`, which must be its opening quote.
 *
 * An unterminated literal returns the end of the input rather than throwing:
 * the caller's `JSON.parse` reports malformed input, and reporting it twice in
 * two different vocabularies helps nobody.
 */
function endOfStringLiteral(text: string, start: number): number {
  let i = start + 1;
  while (i < text.length) {
    const ch = text[i];
    if (ch === '\\') {
      // A backslash consumes whatever follows it, so an escaped quote does not
      // end the literal. \uXXXX needs no special case: its four hex digits are
      // ordinary characters that cannot be an unescaped quote.
      i += 2;
      continue;
    }
    if (ch === '"') {
      return i + 1;
    }
    i++;
  }
  return text.length;
}

/**
 * endOfNumberLiteral returns the index one past the number literal starting at
 * `start`, spanning the fraction and exponent when present.
 */
function endOfNumberLiteral(text: string, start: number): number {
  let i = start;
  if (text[i] === '-' || text[i] === '+') i++;
  while (i < text.length && isDigit(text[i])) i++;
  if (text[i] === '.') {
    i++;
    while (i < text.length && isDigit(text[i])) i++;
  }
  if (text[i] === 'e' || text[i] === 'E') {
    i++;
    if (text[i] === '-' || text[i] === '+') i++;
    while (i < text.length && isDigit(text[i])) i++;
  }
  return i;
}

function isDigit(ch: string | undefined): boolean {
  return ch !== undefined && ch >= '0' && ch <= '9';
}

/**
 * needsQuoting reports whether a number literal loses information when parsed
 * as a `number`.
 *
 * The test is a ROUND TRIP rather than a digit count, because a digit count
 * answers the wrong question at both ends: 9007199254740993 is 16 digits and
 * unrepresentable, while 10000000000000000 is 17 digits and exact. Comparing
 * `String(Number(literal))` against the literal is precise by construction —
 * it asks the only question that matters, which is whether the value survives.
 *
 * Floats are left alone deliberately: their imprecision is inherent to the
 * representation, not an identifier being corrupted, and quoting them would
 * change the type of every measurement this SDK returns.
 */
function needsQuoting(literal: string): boolean {
  if (literal.includes('.') || literal.includes('e') || literal.includes('E')) {
    return false;
  }
  const parsed = Number(literal);
  if (!Number.isSafeInteger(parsed)) return true;
  // -0 is the one exact value whose round-trip disagrees with its literal
  // (`String(-0)` is "0"). It is representable, so quoting it would turn a
  // number into a string for no gain.
  if (parsed === 0) return false;
  return String(parsed) !== literal;
}
