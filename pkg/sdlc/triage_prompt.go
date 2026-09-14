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

import (
	"embed"
	"strconv"
	"strings"
)

// =============================================================================
// The triage charter is editable text, not a Go constant
// =============================================================================
//
// THE PROMPT IS THE BEHAVIOUR. A triage agent's judgement is tuned by editing
// prose, and prose that lives in a Go string literal is prose nobody edits:
// it has to be re-escaped, it cannot be diffed as text, and every wording
// change reads as a code change in review. So the charter is markdown in
// prompts/, embedded at build time, and a test compares TriageCharter with
// the file so the two cannot drift.
//
// THIS IS A TEMPORARY HOME, AND THE FOLLOW-UP IS NAMED. The owner's design
// calls for a generic prompt-set package with a system -> tenant -> project
// inheritance chain, so a project can tune its own definition of "ready"
// without forking the charter. When this file was written nothing under
// pkg/orchestration or pkg/sdlc implemented it; go-quicbaas#803 (scope 1 of
// 4) landed it in parallel, and it brings pkg/orchestration/prompts plus a
// per-owner set at pkg/sdlc/promptdefaults/sdlc with a spec registry,
// validator, renderer and golden tests.
//
// SO THIS CHARTER MUST MOVE INTO THAT SET rather than stay a second way of
// embedding a prompt in one package — see go-quicbaas#883. It is deliberately
// NOT registered there yet: the two changes were in flight at the same time,
// and coupling this one to an unmerged four-part refactor would block a
// working capability on churn it does not depend on. The shape here was
// chosen to make that migration a rename: ONE charter, ONE accessor, no
// inheritance mechanism of its own to unpick. TriageCharter is the single
// call site that changes, and the markdown moves as a file.

//go:embed prompts/triage.md
var triagePrompts embed.FS

// triageCharter is the embedded charter, read once at init. A read failure
// is a build that embedded nothing, which is a programming error rather than
// a runtime condition — but it is reported through TriageCharter returning
// empty and the runner refusing the spawn, never a panic in a daemon.
var triageCharter = mustReadTriageCharter()

// mustReadTriageCharter reads the embedded charter at package init.
func mustReadTriageCharter() string {
	raw, err := triagePrompts.ReadFile("prompts/triage.md")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// TriageCharter is the triage agent's system prompt: what it decides, the
// checks it works through, what it may not do, and the declaration the
// factory reads its verdict from.
func TriageCharter() string { return triageCharter }

// =============================================================================
// The per-issue prompt
// =============================================================================

// TriagePromptSpec is everything one issue's triage turn is told.
type TriagePromptSpec struct {
	// Repo is the repository the issue is in.
	Repo RepoRef

	// Issue is the issue under triage.
	Issue ForgeIssue

	// Comments is the issue's discussion, in order.
	Comments []ForgeComment

	// OpenIssues is every other open issue in the same project: the set a
	// duplicate is judged against. The issue under triage is filtered out
	// here rather than by the caller, so no call site can forget and offer
	// an issue to itself.
	OpenIssues []ForgeIssue

	// Verification is the binding's verification commands, so the agent can
	// judge whether they apply to this issue at all.
	Verification []string
}

// Validate checks the spec names an issue that can be triaged.
func (s TriagePromptSpec) Validate() error {
	switch {
	case s.Repo.Owner == "" || s.Repo.Repo == "":
		return &InvalidInputError{Field: "repo", Message: "triage names the repository the issue is in"}
	case s.Issue.Number <= 0:
		return &InvalidInputError{Field: "issue_number", Message: "issues are numbered from one"}
	case strings.TrimSpace(s.Issue.Title) == "":
		return &InvalidInputError{Field: "title", Message: "an issue with no title states nothing to judge"}
	}
	return nil
}

// TriagePrompt renders the turn for one issue: the issue itself, its
// discussion, the other open issues it might duplicate, and what the project
// verifies a finished change with.
func TriagePrompt(spec TriagePromptSpec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("Triage this issue.\n\n")
	b.WriteString("Repository: " + spec.Repo.String() + "\n")
	b.WriteString("Issue: #" + strconv.Itoa(spec.Issue.Number) + "\n")

	b.WriteString("\n## " + strings.TrimSpace(spec.Issue.Title) + "\n")
	if body := strings.TrimSpace(spec.Issue.Body); body != "" {
		b.WriteString("\n" + body + "\n")
	}

	b.WriteString("\n## Discussion\n\n")
	writeTriageComments(&b, spec.Comments)

	b.WriteString("\n## Other open issues in this project\n\n")
	writeTriageCandidates(&b, spec.Issue.Number, spec.OpenIssues)

	b.WriteString("\n## This project's verification commands\n\n")
	writeTriageVerification(&b, spec.Verification)

	return b.String(), nil
}

// writeTriageComments renders the issue's discussion, or says there is none.
func writeTriageComments(b *strings.Builder, comments []ForgeComment) {
	rendered := 0
	for _, comment := range comments {
		if strings.TrimSpace(comment.Body) == "" {
			continue
		}
		rendered++
		b.WriteString("### Comment " + strconv.Itoa(rendered) + commentAttribution(comment) + "\n\n")
		b.WriteString(strings.TrimSpace(comment.Body) + "\n\n")
	}
	if rendered == 0 {
		b.WriteString("(none)\n")
	}
}

// writeTriageCandidates renders the duplicate candidates: every OPEN issue
// that is not the one under triage.
//
// Both filters are here rather than at the call site. An issue offered to
// itself is the one duplicate verdict that is always wrong, and a closed
// issue is not work this one duplicates — a reader of the rendered prompt can
// see both rules held without knowing what the caller passed.
func writeTriageCandidates(b *strings.Builder, under int, issues []ForgeIssue) {
	rendered := 0
	for _, issue := range issues {
		if issue.Number == under || issue.Number <= 0 || issueClosed(&issue) {
			continue
		}
		rendered++
		b.WriteString("- #" + strconv.Itoa(issue.Number) + " " + strings.TrimSpace(issue.Title) + "\n")
	}
	if rendered == 0 {
		b.WriteString("(none)\n")
	}
}

// writeTriageVerification renders the binding's verification commands, or
// says the project configures none.
func writeTriageVerification(b *strings.Builder, commands []string) {
	rendered := 0
	for _, command := range commands {
		if strings.TrimSpace(command) == "" {
			continue
		}
		rendered++
		b.WriteString("- `" + strings.TrimSpace(command) + "`\n")
	}
	if rendered == 0 {
		b.WriteString("(none configured)\n")
	}
}
