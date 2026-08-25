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

package orchestration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The inputs below are chosen so that a scanner which ignores string literals
// gets them WRONG. That is the whole point of the issue this file closes: a
// BALANCED brace pair inside a string ("return {} or an error?") is handled
// correctly by naive counting purely by luck, and an EVEN number of quotes in
// prose toggles a broken scanner back into the right state. Only the unbalanced
// and odd forms tell the two implementations apart.

// TestExtractJSONFromText_UnbalancedBraceInsideString is the regression test for
// the reported defect: a lone '}' in a field value must not end the object.
func TestExtractJSONFromText_UnbalancedBraceInsideString(t *testing.T) {
	t.Parallel()

	input := `{"decision":"needs_info","question":"should an empty result be } or an error?","reason":"the issue does not say"}`

	got := extractJSONFromText(input)
	require.Equal(t, input, got,
		"the object was cut at the brace inside the question field")

	// The real consequence is that the result parses and keeps every field; a
	// truncated object loses "reason" entirely.
	var decoded map[string]string
	require.NoError(t, json.Unmarshal([]byte(got), &decoded))
	require.Equal(t, "the issue does not say", decoded["reason"])
}

// TestExtractJSONFromText_UnbalancedOpenBraceInsideString covers the mirror
// case: a stray '{' in prose must not add depth the object never closes.
func TestExtractJSONFromText_UnbalancedOpenBraceInsideString(t *testing.T) {
	t.Parallel()

	input := `{"note":"the block starts with { and runs on","ok":"yes"}`

	got := extractJSONFromText(input)
	require.Equal(t, input, got,
		"a '{' inside a string raised the depth, so the object never closed")

	var decoded map[string]string
	require.NoError(t, json.Unmarshal([]byte(got), &decoded))
	require.Equal(t, "yes", decoded["ok"])
}

// TestExtractJSONFromText_EscapedQuoteThenBrace pins the escape handling.
//
// The value contains an ESCAPED quote followed by an unbalanced '}'. A scanner
// that lets `\"` toggle its in-string state believes it has left the literal and
// then treats that brace as structure.
func TestExtractJSONFromText_EscapedQuoteThenBrace(t *testing.T) {
	t.Parallel()

	input := `{"say":"he wrote \"end with } here\" and stopped","tail":"kept"}`

	got := extractJSONFromText(input)
	require.Equal(t, input, got,
		"an escaped quote flipped the scanner out of the string literal")

	var decoded map[string]string
	require.NoError(t, json.Unmarshal([]byte(got), &decoded))
	require.Equal(t, "kept", decoded["tail"])
}

// TestExtractJSONFromText_OddQuotesInSurroundingProse checks that prose before
// the object cannot put the scanner into a string.
//
// The prose carries an ODD number of quotes, so a scanner that started counting
// from the beginning of the text would be inside a literal by the time the
// object begins and would ignore its braces entirely.
func TestExtractJSONFromText_OddQuotesInSurroundingProse(t *testing.T) {
	t.Parallel()

	object := `{"decision":"approve"}`
	input := `The agent replied "with an unclosed quotation and then ` + object + ` at the end.`

	require.Equal(t, object, extractJSONFromText(input))
}

// TestExtractJSONFromText_NestedObject keeps the ordinary case working: the
// first COMPLETE top-level object, not the first inner one.
func TestExtractJSONFromText_NestedObject(t *testing.T) {
	t.Parallel()

	input := `prefix {"outer":{"inner":"v"},"after":1} suffix`
	require.Equal(t, `{"outer":{"inner":"v"},"after":1}`, extractJSONFromText(input))
}

// TestExtractJSONFromText_NoObject covers the empty results.
func TestExtractJSONFromText_NoObject(t *testing.T) {
	t.Parallel()

	require.Empty(t, extractJSONFromText(""))
	require.Empty(t, extractJSONFromText("no object here"))
	require.Empty(t, extractJSONFromText(`{"never":"closed"`))
	require.Empty(t, extractJSONFromText(`{"closer":"is inside a string }"`),
		"a closing brace inside a string must not complete an unterminated object")
}

// TestExtractJSON_UnbalancedBracketInsideString is the array-scanner half of the
// same defect.
func TestExtractJSON_UnbalancedBracketInsideString(t *testing.T) {
	t.Parallel()

	input := `[{"title":"handle the ] case","id":1},{"title":"second","id":2}]`

	got := extractJSON(input)
	require.Equal(t, input, got,
		"the array was cut at the bracket inside the first title")

	var decoded []map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &decoded))
	require.Len(t, decoded, 2, "the second element was lost with the truncation")
}

// TestExtractJSON_EscapedQuoteThenBracket pins escapes for the array scanner.
func TestExtractJSON_EscapedQuoteThenBracket(t *testing.T) {
	t.Parallel()

	input := `[{"say":"he wrote \"index ] here\" once","id":1}]`

	got := extractJSON(input)
	require.Equal(t, input, got)

	var decoded []map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &decoded))
	require.Len(t, decoded, 1)
}

// TestExtractJSON_NoArray covers the empty results for the array scanner.
func TestExtractJSON_NoArray(t *testing.T) {
	t.Parallel()

	require.Empty(t, extractJSON(""))
	require.Empty(t, extractJSON("no array here"))
	require.Empty(t, extractJSON(`["never","closed"`))
}

// TestExtractBalanced_DelimitersAreParameters guards the one thing sharing a
// scanner could get wrong: the object and array callers must not be able to
// close each other's structures.
func TestExtractBalanced_DelimitersAreParameters(t *testing.T) {
	t.Parallel()

	require.Equal(t, `{"a":[1,2]}`, extractBalanced(`x {"a":[1,2]} y`, '{', '}'))
	require.Equal(t, `[{"a":1}]`, extractBalanced(`x [{"a":1}] y`, '[', ']'))

	// An object scanner must not stop at a ']' and an array scanner must not
	// stop at a '}'.
	require.Empty(t, extractBalanced(`{"a":[1,2]`, '{', '}'))
	require.Empty(t, extractBalanced(`[{"a":1}`, '[', ']'))
}
