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
export declare function parseJSONPreservingLargeIntegers<T>(text: string): T;
/**
 * quoteUnrepresentableIntegers rewrites JSON text so that every integer literal
 * that does not survive a `number` round-trip is wrapped in double quotes.
 *
 * Exported for the tests, which assert on the rewritten TEXT: the rewrite is
 * the part that has to be exactly right, and asserting only on the parsed
 * object cannot distinguish "left the literal alone" from "quoted something it
 * should not have".
 */
export declare function quoteUnrepresentableIntegers(text: string): string;
//# sourceMappingURL=json.d.ts.map