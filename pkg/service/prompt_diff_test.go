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

import (
	"reflect"
	"strings"
	"testing"
)

func TestDiffLines(t *testing.T) {
	cases := map[string]struct {
		from, to string
		want     []PromptDiffLine
	}{
		"equal": {"a\nb\n", "a\nb", []PromptDiffLine{
			{PromptDiffEqual, "a"}, {PromptDiffEqual, "b"},
		}},
		"changed middle": {"a\nb\nc", "a\nx\nc", []PromptDiffLine{
			{PromptDiffEqual, "a"}, {PromptDiffDelete, "b"}, {PromptDiffInsert, "x"}, {PromptDiffEqual, "c"},
		}},
		"appended": {"a", "a\nb", []PromptDiffLine{
			{PromptDiffEqual, "a"}, {PromptDiffInsert, "b"},
		}},
		"from empty": {"", "a", []PromptDiffLine{{PromptDiffInsert, "a"}}},
		"to empty":   {"a", "", []PromptDiffLine{{PromptDiffDelete, "a"}}},
		"both empty": {"", "", []PromptDiffLine{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := diffLines(tc.from, tc.to); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDiffLines_BeyondTheBoundReplacesEverything(t *testing.T) {
	from := strings.Repeat("a\n", 1100)
	to := "a\n" + strings.Repeat("b\n", 1000)
	got := diffLines(from, to)
	if len(got) != 2101 {
		t.Fatalf("got %d lines, want 2101", len(got))
	}
	for _, line := range got[:1100] {
		if line.Op != PromptDiffDelete {
			t.Fatalf("expected the whole body deleted first, got %+v", line)
		}
	}
	for _, line := range got[1100:] {
		if line.Op != PromptDiffInsert {
			t.Fatalf("expected the whole body inserted after, got %+v", line)
		}
	}
}
