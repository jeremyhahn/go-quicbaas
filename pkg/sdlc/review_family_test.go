// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package sdlc

import "testing"

// TestReviewerFamily_ClaudeIsRefusedRegardlessOfPolicy pins the owner ruling of
// 2026-09-13 at the SDLC seam: claude reviews nothing in this fleet.
//
// The point is the word REGARDLESS. ReviewerNotIndependentError compares the
// reviewer against the worker and is gated on
// ReviewPolicy.RequireIndependentReviewer, so an operator who switches that off
// would otherwise admit a Claude Code reviewer over Claude Code work — a
// binding that looks configured, an approval the gate counts, and the author's
// own family judging the change.
func TestReviewerFamily_ClaudeIsRefusedRegardlessOfPolicy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		backend string
		refused bool
	}{
		{"claude", true},
		{"claude-code", true},
		{"codex", false},
		{"ollama", false},
	}
	for _, tc := range cases {
		t.Run(tc.backend, func(t *testing.T) {
			t.Parallel()
			got := ModelFamily(tc.backend, "") == ModelFamilyAnthropic
			if got != tc.refused {
				t.Errorf("backend %q resolves to anthropic=%v, want refused=%v",
					tc.backend, got, tc.refused)
			}
		})
	}
}

// TestReviewerFamilyRefusedError_NamesWhatToChange: a refusal an operator cannot
// act on is a refusal that becomes a support question.
func TestReviewerFamilyRefusedError_NamesWhatToChange(t *testing.T) {
	t.Parallel()
	err := &ReviewerFamilyRefusedError{ProjectID: "proj-1", Backend: "claude", Family: ModelFamilyAnthropic}
	msg := err.Error()
	for _, want := range []string{"proj-1", "claude", ModelFamilyAnthropic, "reviewer.backend"} {
		if !contains(msg, want) {
			t.Errorf("the refusal does not mention %q: %s", want, msg)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
