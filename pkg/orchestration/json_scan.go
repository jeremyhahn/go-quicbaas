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

import "strings"

// extractBalanced returns the first substring of text that begins at openDelim
// and ends at its matching closeDelim, or "" when there is no balanced pair.
//
// Delimiters inside JSON string literals are data, not structure. Scanning
// model output without that distinction truncates an object at the first
// closing delimiter that appears inside a string — and agents write braces when
// they discuss code and quotes when they quote identifiers, so this is the
// common case rather than an edge case. The caller then reports an unparseable
// reply, and a retry produces exactly the same truncation.
//
// Escapes are tracked for the same reason: a `\"` inside a literal would
// otherwise flip the scanner's idea of whether it is inside one, and every
// delimiter after it would be counted the wrong way round.
//
// Scanning starts at the opening delimiter, so quotes in the prose before it
// are never seen — surrounding prose cannot put the scanner into a string.
func extractBalanced(text string, openDelim, closeDelim byte) string {
	start := strings.IndexByte(text, openDelim)
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(text); i++ {
		c := text[i]
		switch {
		case escaped:
			// The previous byte was a backslash inside a literal, so this one
			// is data whatever it looks like — a quote here does not close it.
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// Delimiters inside a literal are content.
		case c == openDelim:
			depth++
		case c == closeDelim:
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}

	return ""
}
