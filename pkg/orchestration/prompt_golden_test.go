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
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// Golden equivalence for the workflow prompt set
// =============================================================================
//
// THESE FILES WERE WRITTEN BY THE BUILDERS BEFORE THEY BECAME ASSEMBLERS OVER
// EMBEDDED MARKDOWN. They are the whole acceptance test for go-quicbaas#803:
// moving every instruction out of Go and into files a person can edit is only
// an improvement if no agent is told anything different afterwards, and the
// one way to know that is to keep what the old code said and compare.
//
// A case here is one variable combination, not one builder: the branches are
// where an extraction goes wrong — an omitted workspace paragraph, a plural
// that stopped agreeing, a section that lost its blank line.

var updateGolden = flag.Bool("update-prompt-golden", false,
	"rewrite testdata/prompts goldens from the current builders")

// goldenCase is one rendering and the file that holds what it must produce.
type goldenCase struct {
	name   string
	render func() string
}

// assertGolden compares a rendering to its golden file, byte for byte.
func assertGolden(t *testing.T, dir, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", dir, name+".golden")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s is not byte-identical to its golden\n--- want (%d bytes) ---\n%s\n--- got (%d bytes) ---\n%s",
			name, len(want), string(want), len(got), got)
	}
}

// runGoldenCases renders every case and compares it.
func runGoldenCases(t *testing.T, dir string, cases []goldenCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertGolden(t, dir, tc.name, tc.render())
		})
	}
}

func TestWorkflowPrompts_Golden(t *testing.T) {
	item := &WorkItem{
		ID:          "wi-1",
		ProjectID:   "proj-7",
		Title:       "Add a health endpoint",
		Description: "Serve /healthz and return 200 when the store answers.",
	}
	bare := &WorkItem{Title: "Add a health endpoint"}

	tasks := []*Task{
		{Subject: "write the handler", Status: TaskStatusCompleted, Result: &TaskResult{Output: "handler.go written"}},
		{Subject: "wire the route", Status: TaskStatusFailed},
	}

	backends := []*AgentBackendInfo{
		{Name: "claude", Type: "acp", Capabilities: BackendCapabilities{
			PersistentSessions: true, ToolUse: true, FileAccess: true, Streaming: true, ACP: true,
		}},
		{Name: "ollama", Type: "http"},
	}

	results := []ValidationResult{
		{Name: "go build", Passed: true},
		{Name: "go test", Passed: false, Message: "2 tests failed"},
	}

	workflow := &WorkflowDefinition{Agents: []AgentRoleSpec{
		{Role: "lead", Backend: "claude", Capabilities: []string{"persistent_sessions", "tool_use"}},
		{Role: "worker", Backend: "ollama"},
	}}

	artifacts := []*TaskResult{
		{Output: "first artifact"},
		nil,
		{Output: "second artifact"},
	}

	memory := NewJobMemory()
	memory.AddSuccess("used the store's own client")
	memory.AddFailure("wire the route", "hand-rolled mux", "the router is generated", 2)
	memory.AddConstraint("no new dependencies")
	memory.AddFeedback("name the error type")

	feedbackOnly := NewJobMemory()
	feedbackOnly.AddFeedback("name the error type")

	transcript := &ReplayTranscript{
		SessionID:    "sess-42",
		SystemPrompt: "You are a careful worker.",
		Turns: []ReplayTurn{
			{Role: ReplayRoleUser, Text: "add the endpoint"},
			{Role: ReplayRoleAssistant, Text: "done"},
		},
	}
	truncated := &ReplayTranscript{
		SessionID: "sess-42",
		Turns:     []ReplayTurn{{Role: ReplayRoleAssistant, Text: "done"}},
		Omitted:   3,
	}

	runGoldenCases(t, "prompts", []goldenCase{
		{"worker_prompt_full", func() string {
			return buildWorkerPrompt(item, "/workspace/proj-7", "Prefer the Bash tool for builds.")
		}},
		{"worker_prompt_minimal", func() string { return buildWorkerPrompt(bare, "", "") }},
		{"reviewer_prompt", func() string { return buildReviewPrompt("ship the health endpoint", tasks) }},
		{"reviewer_prompt_no_tasks", func() string { return buildReviewPrompt("ship the health endpoint", nil) }},
		{"reviewer_system", func() string { return reviewerSystemPrompt() }},
		{"planner_system", func() string { return plannerSystemPrompt() }},
		{"decomposition_prompt_full", func() string {
			return buildDecompositionPrompt("ship the health endpoint", []string{"proj-7", "proj-8"}, backends...)
		}},
		{"decomposition_prompt_bare", func() string {
			return buildDecompositionPrompt("ship the health endpoint", nil)
		}},
		{"validation_prompt", func() string {
			return buildValidationPrompt(ReviewCriterion{Name: "tests pass", Config: "go test ./..."}, artifacts)
		}},
		{"validation_prompt_no_artifacts", func() string {
			return buildValidationPrompt(ReviewCriterion{Name: "tests pass", Config: "go test ./..."}, nil)
		}},
		{"validation_summary", func() string { return buildValidationSummary(results) }},
		{"capabilities_context", func() string { return buildCapabilitiesContext(workflow) }},
		{"capabilities_context_empty", func() string { return buildCapabilitiesContext(&WorkflowDefinition{}) }},
		{"memory_full", func() string { return memory.Serialize() }},
		{"memory_feedback_only", func() string { return feedbackOnly.Serialize() }},
		{"resume_priming", func() string { return transcript.PrimingMessage() }},
		{"resume_priming_truncated", func() string { return truncated.PrimingMessage() }},
		{"handshake", func() string { return (spawnPlan{}).handshakeContent() }},
	})
}
