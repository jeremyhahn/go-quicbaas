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

// TestModelFamily_QwenIsItsOwnFamily is what makes a locally served reviewer
// usable at all.
//
// AN UNKNOWN FAMILY IS REFUSED, NOT WAVED THROUGH. The independence rule reads
// `family == "" || family == <author's family>` as "not independent", so before
// qwen was classified a qwen reviewer failed with ReviewerNotIndependentError
// no matter how it was configured — the check cannot prove independence from a
// family it cannot name.
//
// THE BACKEND NAME CARRIES IT, because the served models are tagged for their
// JOB rather than their lineage: review-bf16, review-q8, review-q4_K_M. Not one
// of them contains "qwen", so no prefix rule can classify them and the backend
// name is the only signal there is.
func TestModelFamily_QwenIsItsOwnFamily(t *testing.T) {
	cases := []struct {
		backend, model, want string
	}{
		{"qwen", "review-q4_K_M:latest", ModelFamilyQwen},
		{"qwen", "review-bf16:latest", ModelFamilyQwen},
		{"qwen", "", ModelFamilyQwen},
		{"", "qwen3.8:27b-bf16", ModelFamilyQwen},
		{"", "qwen/qwen3-coder", ModelFamilyQwen},
	}
	for _, c := range cases {
		if got := ModelFamily(c.backend, c.model); got != c.want {
			t.Fatalf("ModelFamily(%q, %q) = %q, want %q", c.backend, c.model, got, c.want)
		}
	}
}

// TestModelFamily_QwenIsIndependentOfTheAuthor is the property the whole change
// exists for: a qwen reviewer must differ from the Claude worker's family, and
// must not collide with codex's either — the two reviewers have to stay
// distinguishable so the gate can tell which one approved.
func TestModelFamily_QwenIsIndependentOfTheAuthor(t *testing.T) {
	worker := ModelFamily("claude", "claude-opus-5")
	qwen := ModelFamily("qwen", "review-q8:latest")
	codex := ModelFamily("codex", "")

	if qwen == worker {
		t.Fatalf("a qwen reviewer must not share the Claude worker's family (%q)", worker)
	}
	if qwen == codex {
		t.Fatalf("qwen and codex must stay distinguishable, both are %q", qwen)
	}
	if qwen == "" {
		t.Fatal("an empty family is refused by the independence rule")
	}
}

// TestModelFamily_UnknownStaysUnknown guards the refusal that protects the
// gate: a backend nothing recognises must NOT acquire a family, or an
// unreviewed change could be approved by an unidentifiable model.
func TestModelFamily_UnknownStaysUnknown(t *testing.T) {
	for _, c := range [][2]string{{"mystery", "some-model"}, {"", ""}, {"", "llama3.3:70b"}} {
		if got := ModelFamily(c[0], c[1]); got != "" {
			t.Fatalf("ModelFamily(%q, %q) = %q, want empty", c[0], c[1], got)
		}
	}
}
