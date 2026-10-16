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

package service

import "strings"

// maxPromptDiffCells bounds the longest-common-subsequence table a diff
// builds. Prompts are a few hundred lines at most; a pair of bodies beyond
// the bound is answered as one whole replacement rather than a table that
// size.
const maxPromptDiffCells = 1 << 20

// diffLines returns the line edits that turn from into to: the longest
// common subsequence of their lines kept, everything else deleted from
// from or inserted from to.
func diffLines(from, to string) []PromptDiffLine {
	a, b := splitPromptLines(from), splitPromptLines(to)
	if len(a)*len(b) > maxPromptDiffCells {
		return replaceAllLines(a, b)
	}

	// common[i][j] is the length of the longest common subsequence of
	// a[i:] and b[j:].
	common := make([][]int, len(a)+1)
	for i := range common {
		common[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}

	out := make([]PromptDiffLine, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, PromptDiffLine{Op: PromptDiffEqual, Text: a[i]})
			i++
			j++
		case common[i+1][j] >= common[i][j+1]:
			out = append(out, PromptDiffLine{Op: PromptDiffDelete, Text: a[i]})
			i++
		default:
			out = append(out, PromptDiffLine{Op: PromptDiffInsert, Text: b[j]})
			j++
		}
	}
	return append(out, replaceAllLines(a[i:], b[j:])...)
}

// replaceAllLines deletes every line of a and inserts every line of b.
func replaceAllLines(a, b []string) []PromptDiffLine {
	out := make([]PromptDiffLine, 0, len(a)+len(b))
	for _, line := range a {
		out = append(out, PromptDiffLine{Op: PromptDiffDelete, Text: line})
	}
	for _, line := range b {
		out = append(out, PromptDiffLine{Op: PromptDiffInsert, Text: line})
	}
	return out
}

// splitPromptLines splits a body into lines; a final newline ends the last
// line rather than starting an empty one.
func splitPromptLines(body string) []string {
	if body == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}
