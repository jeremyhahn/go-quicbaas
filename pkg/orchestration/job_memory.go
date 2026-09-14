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
	"fmt"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// JobMemory accumulates context across iterations so agents don't repeat mistakes.
// It is serialized into the decomposition prompt on re-delegation.
// Thread-safety is not required; only the control loop writes to it (single goroutine).
type JobMemory struct {
	SuccessfulApproaches []string         `json:"successful_approaches,omitempty"`
	FailedApproaches     []FailedApproach `json:"failed_approaches,omitempty"`
	Constraints          []string         `json:"constraints,omitempty"`
	ReviewFeedback       []string         `json:"review_feedback,omitempty"`

	// dedup sets prevent duplicate entries (not serialized)
	successSet    map[string]struct{}
	failedSet     map[string]struct{}
	constraintSet map[string]struct{}
	feedbackSet   map[string]struct{}
}

// FailedApproach records a failed work item with context for why it failed.
type FailedApproach struct {
	WorkItemTitle string `json:"work_item_title"`
	Approach      string `json:"approach"`
	Reason        string `json:"reason"`
	Iteration     int    `json:"iteration"`
}

// dedupKey produces a unique key for deduplication.
func (f FailedApproach) dedupKey() string {
	return fmt.Sprintf("%s|%s|%s|%d", f.WorkItemTitle, f.Approach, f.Reason, f.Iteration)
}

// NewJobMemory creates an empty job memory.
func NewJobMemory() *JobMemory {
	return &JobMemory{
		successSet:    make(map[string]struct{}),
		failedSet:     make(map[string]struct{}),
		constraintSet: make(map[string]struct{}),
		feedbackSet:   make(map[string]struct{}),
	}
}

// AddSuccess records a successful approach. Duplicate entries are ignored.
func (m *JobMemory) AddSuccess(approach string) {
	if _, exists := m.successSet[approach]; exists {
		return
	}
	m.successSet[approach] = struct{}{}
	m.SuccessfulApproaches = append(m.SuccessfulApproaches, approach)
}

// AddFailure records a failed approach with context. Duplicate entries
// (same title, approach, reason, and iteration) are ignored.
func (m *JobMemory) AddFailure(title, approach, reason string, iteration int) {
	fa := FailedApproach{
		WorkItemTitle: title,
		Approach:      approach,
		Reason:        reason,
		Iteration:     iteration,
	}
	key := fa.dedupKey()
	if _, exists := m.failedSet[key]; exists {
		return
	}
	m.failedSet[key] = struct{}{}
	m.FailedApproaches = append(m.FailedApproaches, fa)
}

// AddConstraint records a discovered constraint. Duplicate entries are ignored.
func (m *JobMemory) AddConstraint(constraint string) {
	if _, exists := m.constraintSet[constraint]; exists {
		return
	}
	m.constraintSet[constraint] = struct{}{}
	m.Constraints = append(m.Constraints, constraint)
}

// AddFeedback records reviewer feedback. Duplicate entries are ignored.
func (m *JobMemory) AddFeedback(feedback string) {
	if _, exists := m.feedbackSet[feedback]; exists {
		return
	}
	m.feedbackSet[feedback] = struct{}{}
	m.ReviewFeedback = append(m.ReviewFeedback, feedback)
}

// Serialize produces a human-readable markdown summary for injection into prompts.
// Returns an empty string if no context has been accumulated.
func (m *JobMemory) Serialize() string {
	if m.IsEmpty() {
		return ""
	}

	var b strings.Builder

	b.WriteString(workflowPrompts.MustRender("memory_header", nil))
	b.WriteString("\n")

	writeMemorySection(&b, "memory_worked", m.SuccessfulApproaches)

	if len(m.FailedApproaches) > 0 {
		b.WriteString("\n")
		b.WriteString(workflowPrompts.MustRender("memory_failed", nil))
		b.WriteString("\n")
		for _, f := range m.FailedApproaches {
			b.WriteString(workflowPrompts.MustRender("memory_failed_item", prompts.Vars{
				"title":     strconv.Quote(f.WorkItemTitle),
				"approach":  f.Approach,
				"reason":    f.Reason,
				"iteration": strconv.Itoa(f.Iteration),
			}))
			b.WriteString("\n")
		}
	}

	writeMemorySection(&b, "memory_constraints", m.Constraints)
	writeMemorySection(&b, "memory_feedback", m.ReviewFeedback)

	return b.String()
}

// writeMemorySection writes one bulleted section, or nothing when the
// section has nothing in it. The bullet is punctuation rather than
// instruction, so it stays here; the heading is a prompt.
func writeMemorySection(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(workflowPrompts.MustRender(heading, nil))
	b.WriteString("\n")
	for _, item := range items {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}
}

// IsEmpty returns true if no context has been accumulated.
func (m *JobMemory) IsEmpty() bool {
	return len(m.SuccessfulApproaches) == 0 &&
		len(m.FailedApproaches) == 0 &&
		len(m.Constraints) == 0 &&
		len(m.ReviewFeedback) == 0
}

// RebuildDedupSets reconstructs the internal deduplication maps from the
// serialized slices. This must be called after deserializing a JobMemory
// from JSON, because the unexported map fields are not included in JSON
// encoding and will be nil after unmarshal.
func (m *JobMemory) RebuildDedupSets() {
	m.successSet = make(map[string]struct{}, len(m.SuccessfulApproaches))
	for _, s := range m.SuccessfulApproaches {
		m.successSet[s] = struct{}{}
	}

	m.failedSet = make(map[string]struct{}, len(m.FailedApproaches))
	for _, f := range m.FailedApproaches {
		m.failedSet[f.dedupKey()] = struct{}{}
	}

	m.constraintSet = make(map[string]struct{}, len(m.Constraints))
	for _, c := range m.Constraints {
		m.constraintSet[c] = struct{}{}
	}

	m.feedbackSet = make(map[string]struct{}, len(m.ReviewFeedback))
	for _, f := range m.ReviewFeedback {
		m.feedbackSet[f] = struct{}{}
	}
}
