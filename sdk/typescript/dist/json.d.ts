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
export declare function parseJSONPreservingLargeIntegers<T>(text: string): T;
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
export declare function quoteUnrepresentableIntegers(text: string): string;
//# sourceMappingURL=json.d.ts.map