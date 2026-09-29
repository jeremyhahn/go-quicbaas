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

package mcp

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCMCP_ReviewerToolsAndTransforms(t *testing.T) {
	for method, want := range map[string]string{
		"Reviewer": "sdlc_reviewer", "SetReviewer": "sdlc_reviewer_set", "ClearReviewer": "sdlc_reviewer_clear",
	} {
		if got, ok := sdlcToolName(method); !ok || got != want {
			t.Fatalf("%s → %q %v", method, got, ok)
		}
		selection, err := transformSDLCResponse(method, map[string]any{
			"project_id": "p1", "pull_request": 41, "profile": "qwen", "overridden": true, "status_context": "quicbaas/review/qwen",
		})
		if err != nil || selection.(*sdlc.ReviewerSelection).PullRequest != 41 || !selection.(*sdlc.ReviewerSelection).Overridden {
			t.Fatalf("%s transform: %+v %v", method, selection, err)
		}
	}
	if _, err := transformSDLCResponse("Reviewer", map[string]any{"pull_request": "forty-one"}); err == nil {
		t.Fatal("a mistyped selection decoded")
	}
}

// TestSDLCMCP_TheBindingCarriesTheReviewerProfiles proves the MCP decode keeps
// every alternate and the presence of counts_toward_merge, so a rebind
// through MCP writes back what it read.
func TestSDLCMCP_TheBindingCarriesTheReviewerProfiles(t *testing.T) {
	raw := map[string]any{
		"project_id": "p1",
		"reviewer":   map[string]any{"agent_id": "codex-reviewer", "login": "codex", "token_secret_ref": "codex-tok"},
		"reviewers": []any{
			map[string]any{"name": "qwen", "agent_id": "qwen-reviewer", "login": "qwen-reviewer", "token_secret_ref": "qwen-tok", "counts_toward_merge": false},
			map[string]any{"name": "unset", "agent_id": "codex-reviewer", "login": "u", "token_secret_ref": "u-tok"},
		},
		"worker": map[string]any{"pool_id": "coders"},
		"triage": map[string]any{"agent_id": "openvino-triage"},
	}
	decoded, err := transformSDLCResponse("Binding", raw)
	if err != nil {
		t.Fatal(err)
	}
	binding := decoded.(*sdlc.ProjectBinding)
	if binding.Reviewer.Agent != "codex-reviewer" || len(binding.Reviewers) != 2 || binding.Reviewers[0].Agent != "qwen-reviewer" {
		t.Fatalf("binding = %+v", binding)
	}
	if binding.Worker.PoolID != "coders" || binding.Triage.AgentID != "openvino-triage" {
		t.Fatalf("binding = %+v", binding)
	}
	if c := binding.Reviewers[0].CountsTowardMerge; c == nil || *c {
		t.Fatal("an explicit false was lost")
	}
	if binding.Reviewers[1].CountsTowardMerge != nil {
		t.Fatal("an unset counts_toward_merge was invented")
	}
}
