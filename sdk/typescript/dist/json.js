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
 * parseJSONPreservingLargeIntegers parses JSON text, returning any integer
 * literal that cannot be represented exactly as a `number` as its decimal
 * string instead.
 *
 * Every other value — including floats, whose imprecision is inherent rather
 * than an identity hazard — parses exactly as `JSON.parse` would.
 *
 * Throws the same `SyntaxError` as `JSON.parse` for malformed input: this is a
 * parse, and input that is not JSON must fail as loudly here as it would there.
 */
export function parseJSONPreservingLargeIntegers(text) {
    return JSON.parse(quoteUnrepresentableIntegers(text));
}
/**
 * quoteUnrepresentableIntegers rewrites JSON text so that every integer literal
 * that does not survive a `number` round-trip is wrapped in double quotes.
 *
 * Exported for the tests, which assert on the rewritten TEXT: the rewrite is
 * the part that has to be exactly right, and asserting only on the parsed
 * object cannot distinguish "left the literal alone" from "quoted something it
 * should not have".
 */
export function quoteUnrepresentableIntegers(text) {
    let out = '';
    let i = 0;
    while (i < text.length) {
        const ch = text[i];
        // A string literal is copied verbatim, escapes included. This is the
        // clause that makes the rewrite safe against payloads whose text content
        // looks like JSON.
        if (ch === '"') {
            const end = endOfStringLiteral(text, i);
            out += text.slice(i, end);
            i = end;
            continue;
        }
        // Outside a string, a digit or a minus sign can only begin a number
        // literal: JSON object keys are always strings, and the only other bare
        // tokens are `true`, `false` and `null`, none of which start this way.
        if (ch === '-' || (ch >= '0' && ch <= '9')) {
            const end = endOfNumberLiteral(text, i);
            const literal = text.slice(i, end);
            out += needsQuoting(literal) ? `"${literal}"` : literal;
            i = end;
            continue;
        }
        out += ch;
        i++;
    }
    return out;
}
/**
 * endOfStringLiteral returns the index one past the closing quote of the string
 * literal starting at `start`, which must be its opening quote.
 *
 * An unterminated literal returns the end of the input rather than throwing:
 * the caller's `JSON.parse` reports malformed input, and reporting it twice in
 * two different vocabularies helps nobody.
 */
function endOfStringLiteral(text, start) {
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
function endOfNumberLiteral(text, start) {
    let i = start;
    if (text[i] === '-' || text[i] === '+')
        i++;
    while (i < text.length && isDigit(text[i]))
        i++;
    if (text[i] === '.') {
        i++;
        while (i < text.length && isDigit(text[i]))
            i++;
    }
    if (text[i] === 'e' || text[i] === 'E') {
        i++;
        if (text[i] === '-' || text[i] === '+')
            i++;
        while (i < text.length && isDigit(text[i]))
            i++;
    }
    return i;
}
function isDigit(ch) {
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
function needsQuoting(literal) {
    if (literal.includes('.') || literal.includes('e') || literal.includes('E')) {
        return false;
    }
    const parsed = Number(literal);
    if (!Number.isSafeInteger(parsed))
        return true;
    // -0 is the one exact value whose round-trip disagrees with its literal
    // (`String(-0)` is "0"). It is representable, so quoting it would turn a
    // number into a string for no gain.
    if (parsed === 0)
        return false;
    return String(parsed) !== literal;
}
//# sourceMappingURL=json.js.map