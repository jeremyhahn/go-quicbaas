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

package orchestration

import (
	"context"
	"errors"
	"testing"
)

// TestHandleReview_RefusesEveryRouteToClaude pins the owner's ruling of
// 2026-09-13: claude reviews nothing in this fleet.
//
// THREE ROUTES REACH THE REVIEW PROMPT and each could land on Claude Code by a
// different road — a workflow that names claude for its reviewer role, or either
// of the lead fallbacks, which take the lead's backend when no reviewer is
// configured. The guard is on the resolved backend for exactly that reason, so
// this walks all three rather than the one that was reported.
func TestHandleReview_RefusesEveryRouteToClaude(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		lead     string
		reviewer string
		refused  bool
	}{
		{"reviewer role names claude", "ollama", "claude", true},
		{"reviewer role names claude-code", "ollama", "claude-code", true},
		{"no reviewer, claude lead falls back to itself", "claude", "", true},
		{"no reviewer, claude-code lead", "claude-code", "", true},
		{"codex reviewer over a claude lead is allowed", "claude", "codex", false},
		{"no reviewer, non-claude lead is allowed", "ollama", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := &controlLoopMockService{
				decomposeResponse: &DecomposeTaskResponse{
					WorkItems: []*WorkItem{{ID: "r", Description: `{"approved": true, "feedback": "ok"}`}},
				},
				tasks: []*Task{{ID: "task-1", Status: TaskStatusCompleted, Result: &TaskResult{Output: "done"}}},
			}
			cl := NewControlLoop(svc, &controlLoopMockLogger{})
			cl.job.Config = newTestConfig()
			cl.job.Config.LeadBackend = tc.lead
			cl.job.JobID = "job-under-test"
			cl.taskIDs = []string{"task-1"}
			cl.reviewerBackend = tc.reviewer

			_, err := cl.handleReview(context.Background())

			var refused *ReviewBackendRefusedError
			if tc.refused {
				if !errors.As(err, &refused) {
					t.Fatalf("want ReviewBackendRefusedError, got %v", err)
				}
				// The refusal must name the backend an operator has to change.
				if refused.Backend == "" {
					t.Error("the refusal names no backend, so nobody knows what to reconfigure")
				}
				if refused.JobID != "job-under-test" {
					t.Errorf("JobID = %q, want the job under test", refused.JobID)
				}
				return
			}
			if errors.As(err, &refused) {
				t.Fatalf("review on %q/%q was refused and should not be: %v", tc.lead, tc.reviewer, err)
			}
		})
	}
}

// TestResolvedReviewBackend_MirrorsTheRouting: a guard that models the routing
// loosely passes the case it exists to catch. The configured reviewer wins; with
// none, both remaining routes end on the lead.
func TestResolvedReviewBackend_MirrorsTheRouting(t *testing.T) {
	t.Parallel()
	cl := NewControlLoop(&controlLoopMockService{}, &controlLoopMockLogger{})
	cl.job.Config = newTestConfig()
	cl.job.Config.LeadBackend = "claude"

	if got := cl.resolvedReviewBackend(); got != "claude" {
		t.Errorf("with no reviewer the review runs on the lead's backend; got %q", got)
	}
	cl.reviewerBackend = "codex"
	if got := cl.resolvedReviewBackend(); got != "codex" {
		t.Errorf("a configured reviewer wins; got %q", got)
	}
}
