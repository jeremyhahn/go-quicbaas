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
	"strconv"
	"strings"
)

// =============================================================================
// Run-time prompt material
// =============================================================================
//
// AN AGENT'S PROMPT IS A SHARED HEADER; WHAT A RUN ADDS IS ITS OWN. Every place
// that runs an Agent — a session spawn, a job's lead, a task's attempt, a
// workflow role, an SDLC worker, reviewer or triage — may pass prompt material
// that is appended after the prompt the run resolved (the role's charter, else
// the Agent's own prompt), so similar jobs share one Agent and differ only in
// the dynamic content they carry.
//
// THE APPEND IS COMPOSED IN ONE PLACE, when a session is spawned. Everything
// upstream only carries it, and the durable session record keeps the resolved
// prompt and the append apart, so the audit shows which content ran and a
// resume composes exactly the same text again.

// MaxPromptAppendBytes bounds the prompt material appended to one spawn. It is
// a bound on everything appended — a binding slot's and a job's together —
// because that is what reaches the model.
const MaxPromptAppendBytes = 64 << 10

// PromptAppendSeparator stands between the resolved prompt and each block of
// appended material, so the model reads them as distinct sections.
const PromptAppendSeparator = "\n\n---\n\n"

// PromptAppendTooLargeError refuses prompt material over MaxPromptAppendBytes.
type PromptAppendTooLargeError struct {
	// Field names the request field that carried the material.
	Field string

	// Size is the material's length in bytes.
	Size int

	// Limit is MaxPromptAppendBytes.
	Limit int
}

// Error implements the error interface.
func (e *PromptAppendTooLargeError) Error() string {
	return ErrInvalidInput.Error() + ": " + e.Field + ": " + strconv.Itoa(e.Size) +
		" bytes of appended prompt material exceed the limit of " + strconv.Itoa(e.Limit)
}

// Unwrap returns the sentinel, so every transport answers it as bad input.
func (e *PromptAppendTooLargeError) Unwrap() error { return ErrInvalidInput }

// ValidatePromptAppend refuses material over MaxPromptAppendBytes. field is
// the request field the refusal names.
func ValidatePromptAppend(field, text string) error {
	if len(text) > MaxPromptAppendBytes {
		return &PromptAppendTooLargeError{Field: field, Size: len(text), Limit: MaxPromptAppendBytes}
	}
	return nil
}

// ComposePrompt joins blocks of prompt text in order, separated by
// PromptAppendSeparator and dropping blank ones. It composes a resolved prompt
// with the material appended to it, and joins a binding slot's material with a
// job's, most general first, so the most specific content is read last. Text
// is kept verbatim; a composition of nothing is empty.
func ComposePrompt(blocks ...string) string {
	kept := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block) != "" {
			kept = append(kept, block)
		}
	}
	return strings.Join(kept, PromptAppendSeparator)
}

// EffectiveSystemPrompt is the system prompt the session's process runs with:
// the resolved prompt followed by the run-time material appended to it. A
// resume and a rotation start the replacement process with exactly this.
func (r *SessionRecord) EffectiveSystemPrompt() string {
	return ComposePrompt(r.SystemPrompt, r.PromptAppend)
}
