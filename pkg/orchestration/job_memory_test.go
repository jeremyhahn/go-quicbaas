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
	"strings"
	"testing"
)

func TestJobMemory_New_IsEmpty(t *testing.T) {
	mem := NewJobMemory()
	if !mem.IsEmpty() {
		t.Fatal("expected new JobMemory to be empty")
	}
	if mem.Serialize() != "" {
		t.Fatal("expected Serialize on empty memory to return empty string")
	}
}

func TestJobMemory_AddSuccess(t *testing.T) {
	mem := NewJobMemory()
	mem.AddSuccess("used table-driven tests")

	if mem.IsEmpty() {
		t.Fatal("expected memory to be non-empty after AddSuccess")
	}
	if len(mem.SuccessfulApproaches) != 1 {
		t.Fatalf("expected 1 successful approach, got %d", len(mem.SuccessfulApproaches))
	}
	if mem.SuccessfulApproaches[0] != "used table-driven tests" {
		t.Fatalf("unexpected approach: %s", mem.SuccessfulApproaches[0])
	}

	output := mem.Serialize()
	if !strings.Contains(output, "### What worked:") {
		t.Fatal("expected 'What worked' section in serialized output")
	}
	if !strings.Contains(output, "- used table-driven tests") {
		t.Fatal("expected approach in serialized output")
	}
}

func TestJobMemory_AddSuccess_InvalidEmpty(t *testing.T) {
	mem := NewJobMemory()
	// Adding an empty string is technically valid input; verify it stores
	mem.AddSuccess("")

	if len(mem.SuccessfulApproaches) != 1 {
		t.Fatalf("expected 1 successful approach for empty string, got %d", len(mem.SuccessfulApproaches))
	}
}

func TestJobMemory_AddFailure(t *testing.T) {
	mem := NewJobMemory()
	mem.AddFailure("implement auth", "basic auth with plaintext", "security review rejected", 2)

	if mem.IsEmpty() {
		t.Fatal("expected memory to be non-empty after AddFailure")
	}
	if len(mem.FailedApproaches) != 1 {
		t.Fatalf("expected 1 failed approach, got %d", len(mem.FailedApproaches))
	}

	fa := mem.FailedApproaches[0]
	if fa.WorkItemTitle != "implement auth" {
		t.Fatalf("unexpected work item title: %s", fa.WorkItemTitle)
	}
	if fa.Approach != "basic auth with plaintext" {
		t.Fatalf("unexpected approach: %s", fa.Approach)
	}
	if fa.Reason != "security review rejected" {
		t.Fatalf("unexpected reason: %s", fa.Reason)
	}
	if fa.Iteration != 2 {
		t.Fatalf("unexpected iteration: %d", fa.Iteration)
	}

	output := mem.Serialize()
	if !strings.Contains(output, "### What failed (do NOT repeat these):") {
		t.Fatal("expected 'What failed' section in serialized output")
	}
	if !strings.Contains(output, `"implement auth"`) {
		t.Fatal("expected work item title in serialized output")
	}
	if !strings.Contains(output, "FAILED because: security review rejected") {
		t.Fatal("expected failure reason in serialized output")
	}
	if !strings.Contains(output, "(iteration 2)") {
		t.Fatal("expected iteration number in serialized output")
	}
}

func TestJobMemory_AddFailure_InvalidIteration(t *testing.T) {
	mem := NewJobMemory()
	// Negative iteration -- still stored, the caller controls iteration values
	mem.AddFailure("task", "approach", "reason", -1)

	if len(mem.FailedApproaches) != 1 {
		t.Fatalf("expected 1 failed approach, got %d", len(mem.FailedApproaches))
	}
	if mem.FailedApproaches[0].Iteration != -1 {
		t.Fatalf("expected iteration -1, got %d", mem.FailedApproaches[0].Iteration)
	}
}

func TestJobMemory_AddConstraint(t *testing.T) {
	mem := NewJobMemory()
	mem.AddConstraint("must use TLS 1.3 minimum")

	if mem.IsEmpty() {
		t.Fatal("expected memory to be non-empty after AddConstraint")
	}
	if len(mem.Constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(mem.Constraints))
	}

	output := mem.Serialize()
	if !strings.Contains(output, "### Discovered constraints:") {
		t.Fatal("expected 'Discovered constraints' section in serialized output")
	}
	if !strings.Contains(output, "- must use TLS 1.3 minimum") {
		t.Fatal("expected constraint in serialized output")
	}
}

func TestJobMemory_AddConstraint_InvalidEmpty(t *testing.T) {
	mem := NewJobMemory()
	mem.AddConstraint("")

	if len(mem.Constraints) != 1 {
		t.Fatalf("expected 1 constraint for empty string, got %d", len(mem.Constraints))
	}
}

func TestJobMemory_AddFeedback(t *testing.T) {
	mem := NewJobMemory()
	mem.AddFeedback("error handling needs wrapping with context")

	if mem.IsEmpty() {
		t.Fatal("expected memory to be non-empty after AddFeedback")
	}
	if len(mem.ReviewFeedback) != 1 {
		t.Fatalf("expected 1 feedback entry, got %d", len(mem.ReviewFeedback))
	}

	output := mem.Serialize()
	if !strings.Contains(output, "### Reviewer feedback:") {
		t.Fatal("expected 'Reviewer feedback' section in serialized output")
	}
	if !strings.Contains(output, "- error handling needs wrapping with context") {
		t.Fatal("expected feedback in serialized output")
	}
}

func TestJobMemory_AddFeedback_InvalidEmpty(t *testing.T) {
	mem := NewJobMemory()
	mem.AddFeedback("")

	if len(mem.ReviewFeedback) != 1 {
		t.Fatalf("expected 1 feedback entry for empty string, got %d", len(mem.ReviewFeedback))
	}
}

func TestJobMemory_Serialize_Empty(t *testing.T) {
	mem := NewJobMemory()
	result := mem.Serialize()

	if result != "" {
		t.Fatalf("expected empty string for empty memory, got: %q", result)
	}
}

func TestJobMemory_Serialize_Full(t *testing.T) {
	mem := NewJobMemory()
	mem.AddSuccess("interface-based dependency injection")
	mem.AddSuccess("table-driven tests")
	mem.AddFailure("implement cache", "in-memory map", "not thread-safe", 1)
	mem.AddFailure("implement cache", "sync.Map", "poor iteration performance", 2)
	mem.AddConstraint("must support concurrent reads")
	mem.AddConstraint("memory budget is 512MB")
	mem.AddFeedback("add context propagation to all methods")
	mem.AddFeedback("use structured logging")

	output := mem.Serialize()

	// Verify the header is present
	if !strings.HasPrefix(output, "## Previous Attempts Context\n") {
		t.Fatal("expected output to start with '## Previous Attempts Context'")
	}

	// Verify all four sections are present
	sections := []string{
		"### What worked:",
		"### What failed (do NOT repeat these):",
		"### Discovered constraints:",
		"### Reviewer feedback:",
	}
	for _, section := range sections {
		if !strings.Contains(output, section) {
			t.Fatalf("expected section %q in serialized output", section)
		}
	}

	// Verify all entries are present
	entries := []string{
		"- interface-based dependency injection",
		"- table-driven tests",
		`"implement cache": in-memory map - FAILED because: not thread-safe (iteration 1)`,
		`"implement cache": sync.Map - FAILED because: poor iteration performance (iteration 2)`,
		"- must support concurrent reads",
		"- memory budget is 512MB",
		"- add context propagation to all methods",
		"- use structured logging",
	}
	for _, entry := range entries {
		if !strings.Contains(output, entry) {
			t.Fatalf("expected entry %q in serialized output", entry)
		}
	}

	// Verify section ordering: worked < failed < constraints < feedback
	workedIdx := strings.Index(output, "### What worked:")
	failedIdx := strings.Index(output, "### What failed")
	constraintsIdx := strings.Index(output, "### Discovered constraints:")
	feedbackIdx := strings.Index(output, "### Reviewer feedback:")

	if workedIdx >= failedIdx {
		t.Fatal("expected 'What worked' before 'What failed'")
	}
	if failedIdx >= constraintsIdx {
		t.Fatal("expected 'What failed' before 'Discovered constraints'")
	}
	if constraintsIdx >= feedbackIdx {
		t.Fatal("expected 'Discovered constraints' before 'Reviewer feedback'")
	}
}

func TestJobMemory_Serialize_NoRepeats(t *testing.T) {
	mem := NewJobMemory()

	// Add duplicates for each category
	mem.AddSuccess("approach A")
	mem.AddSuccess("approach A")
	mem.AddSuccess("approach A")

	mem.AddFailure("task1", "bad approach", "it broke", 1)
	mem.AddFailure("task1", "bad approach", "it broke", 1)

	mem.AddConstraint("no unsafe operations")
	mem.AddConstraint("no unsafe operations")

	mem.AddFeedback("needs more tests")
	mem.AddFeedback("needs more tests")

	// Verify slice lengths reflect dedup
	if len(mem.SuccessfulApproaches) != 1 {
		t.Fatalf("expected 1 successful approach after dedup, got %d", len(mem.SuccessfulApproaches))
	}
	if len(mem.FailedApproaches) != 1 {
		t.Fatalf("expected 1 failed approach after dedup, got %d", len(mem.FailedApproaches))
	}
	if len(mem.Constraints) != 1 {
		t.Fatalf("expected 1 constraint after dedup, got %d", len(mem.Constraints))
	}
	if len(mem.ReviewFeedback) != 1 {
		t.Fatalf("expected 1 feedback after dedup, got %d", len(mem.ReviewFeedback))
	}

	// Verify serialized output has exactly one occurrence of each entry
	output := mem.Serialize()
	if count := strings.Count(output, "- approach A"); count != 1 {
		t.Fatalf("expected 1 occurrence of 'approach A' in output, got %d", count)
	}
	if count := strings.Count(output, "bad approach"); count != 1 {
		t.Fatalf("expected 1 occurrence of 'bad approach' in output, got %d", count)
	}
	if count := strings.Count(output, "no unsafe operations"); count != 1 {
		t.Fatalf("expected 1 occurrence of 'no unsafe operations' in output, got %d", count)
	}
	if count := strings.Count(output, "needs more tests"); count != 1 {
		t.Fatalf("expected 1 occurrence of 'needs more tests' in output, got %d", count)
	}
}

func TestJobMemory_MultipleIterations(t *testing.T) {
	mem := NewJobMemory()

	// Same task and approach failing across different iterations should be tracked separately
	mem.AddFailure("build API", "REST with gin", "gin dependency conflict", 1)
	mem.AddFailure("build API", "REST with gin", "gin version mismatch", 2)
	mem.AddFailure("build API", "REST with chi", "missing middleware", 3)

	if len(mem.FailedApproaches) != 3 {
		t.Fatalf("expected 3 failed approaches across iterations, got %d", len(mem.FailedApproaches))
	}

	// Verify each iteration is tracked with its own reason
	output := mem.Serialize()
	if !strings.Contains(output, "(iteration 1)") {
		t.Fatal("expected iteration 1 in output")
	}
	if !strings.Contains(output, "(iteration 2)") {
		t.Fatal("expected iteration 2 in output")
	}
	if !strings.Contains(output, "(iteration 3)") {
		t.Fatal("expected iteration 3 in output")
	}

	// Verify the different reasons are present
	if !strings.Contains(output, "gin dependency conflict") {
		t.Fatal("expected first failure reason in output")
	}
	if !strings.Contains(output, "gin version mismatch") {
		t.Fatal("expected second failure reason in output")
	}
	if !strings.Contains(output, "missing middleware") {
		t.Fatal("expected third failure reason in output")
	}

	// Verify same title+approach+reason+iteration is deduped
	mem.AddFailure("build API", "REST with gin", "gin dependency conflict", 1)
	if len(mem.FailedApproaches) != 3 {
		t.Fatalf("expected 3 failed approaches after adding duplicate, got %d", len(mem.FailedApproaches))
	}
}

func TestJobMemory_Serialize_PartialSections(t *testing.T) {
	// Only successes and constraints, no failures or feedback
	mem := NewJobMemory()
	mem.AddSuccess("used context propagation")
	mem.AddConstraint("FedRAMP compliance required")

	output := mem.Serialize()

	if !strings.Contains(output, "### What worked:") {
		t.Fatal("expected 'What worked' section")
	}
	if !strings.Contains(output, "### Discovered constraints:") {
		t.Fatal("expected 'Discovered constraints' section")
	}
	if strings.Contains(output, "### What failed") {
		t.Fatal("did not expect 'What failed' section when no failures added")
	}
	if strings.Contains(output, "### Reviewer feedback:") {
		t.Fatal("did not expect 'Reviewer feedback' section when no feedback added")
	}
}

func TestJobMemory_IsEmpty_AfterAddAndNothingElse(t *testing.T) {
	tests := []struct {
		name string
		add  func(m *JobMemory)
	}{
		{
			name: "success only",
			add:  func(m *JobMemory) { m.AddSuccess("approach") },
		},
		{
			name: "failure only",
			add:  func(m *JobMemory) { m.AddFailure("t", "a", "r", 1) },
		},
		{
			name: "constraint only",
			add:  func(m *JobMemory) { m.AddConstraint("c") },
		},
		{
			name: "feedback only",
			add:  func(m *JobMemory) { m.AddFeedback("f") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem := NewJobMemory()
			tc.add(mem)
			if mem.IsEmpty() {
				t.Fatal("expected memory to be non-empty")
			}
		})
	}
}

func TestFailedApproach_DedupKey(t *testing.T) {
	fa1 := FailedApproach{WorkItemTitle: "t", Approach: "a", Reason: "r", Iteration: 1}
	fa2 := FailedApproach{WorkItemTitle: "t", Approach: "a", Reason: "r", Iteration: 1}
	fa3 := FailedApproach{WorkItemTitle: "t", Approach: "a", Reason: "r", Iteration: 2}

	if fa1.dedupKey() != fa2.dedupKey() {
		t.Fatal("identical FailedApproach values should produce the same dedup key")
	}
	if fa1.dedupKey() == fa3.dedupKey() {
		t.Fatal("different iterations should produce different dedup keys")
	}
}
