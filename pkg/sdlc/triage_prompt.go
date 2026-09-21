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

	// MayClose tells the agent this project has enabled closing, so the
	// charter's close marker is available to it.
	//
	// IT IS TOLD RATHER THAN LEFT TO INFER. The charter forbids asking for a
	// close by default; an agent that guessed the project had opted in would
	// be guessing about the one action that is not reversible.
	MayClose bool

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

// TriagePrompt renders the turn for one issue: what is true of the whole
// project, then the issue itself.
//
// PROJECT-STABLE CONTENT COMES FIRST, AND THAT ORDER IS THE POINT.
//
// A triage turn is ~94% PREFILL — the prompt runs 5,600-7,500 tokens and the
// answer is two lines, 24-44 tokens. Measured on an 8B: 28.7 s of prefill
// against 1.9 s of decode. So the only optimisation that matters is not
// prefilling the same tokens twice, and every serving runtime in use here
// (llama.cpp, ollama, vLLM, OVMS) caches a prompt PREFIX and reuses it when
// the next prompt starts with the same bytes.
//
// The sections above the issue are identical for EVERY issue in one sweep:
// the repository, the open-issue list a duplicate is judged against, the
// project's verification commands, and its closing policy. Ordered this way, a
// sweep over twenty issues prefills that block once instead of twenty times.
//
// The previous order defeated it completely. `Issue: #<n>` sat on the THIRD
// line, so two prompts in the same sweep diverged after about forty tokens and
// shared essentially no prefix, while the genuinely stable sections sat at the
// END where a cache can never reach them.
//
// IT IS ALSO THE BETTER ORDER FOR THE ANSWER, which is why this is not a
// trade. The decisive evidence — this issue, its body, its discussion — now
// lands nearest the question, where attention is strongest. A model asked to
// judge one issue reads that issue last and answers immediately after it.
func TriagePrompt(spec TriagePromptSpec) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder

	// ---- project-stable prefix: identical for every issue in a sweep ----
	b.WriteString("Triage one issue in this repository.\n\n")
	b.WriteString("Repository: " + spec.Repo.String() + "\n")

	// EVERY OPEN ISSUE, INCLUDING THE ONE BEING TRIAGED. Excluding the subject
	// would make this list differ per issue and destroy the shared prefix,
	// which is the whole reason the section sits up here — so the heading says
	// plainly what the list is, and the line below tells the agent the one
	// thing that follows from including it. An "Other open issues" heading
	// that silently contained the subject would invite exactly the mistake it
	// has to avoid.
	b.WriteString("\n## Open issues in this project\n\n")
	b.WriteString("This list includes the issue you are triaging. An issue is never a " +
		"duplicate of itself. Each entry is a number and a title only: you have " +
		"not read these issues, so a duplicate verdict must rest on the titles " +
		"describing the same defect or change, not on a shared area.\n\n")
	writeTriageCandidates(&b, 0, spec.OpenIssues)

	b.WriteString("\n## This project's verification commands\n\n")
	writeTriageVerification(&b, spec.Verification)

	// STATED EVERY TURN, IN BOTH DIRECTIONS. Saying nothing when closing is
	// off would leave the agent to infer the default from the charter alone,
	// and the one action that cannot be undone is not a good place for
	// inference.
	b.WriteString("\n## Closing\n\n")
	if spec.MayClose {
		b.WriteString("Closing IS enabled for this project. If — and only if — you find " +
			"the work is already present on the default branch and you can cite the file " +
			"and line, you may add the `SDLC-TRIAGE-CLOSE: already-done` line described in " +
			"your charter. For every other not-actionable reason, leave it off.\n")
	} else {
		b.WriteString("Closing is NOT enabled for this project. Do not ask for the issue to " +
			"be closed; say what you found and leave it open for a person.\n")
	}

	// ---- everything below varies per issue, and the cache ends here ----
	b.WriteString("\n## The issue to triage: #" + strconv.Itoa(spec.Issue.Number) + "\n")
	b.WriteString("\n### " + strings.TrimSpace(spec.Issue.Title) + "\n")
	if body := strings.TrimSpace(spec.Issue.Body); body != "" {
		b.WriteString("\n" + body + "\n")
	}

	b.WriteString("\n### Discussion\n\n")
	writeTriageComments(&b, spec.Comments)

	// The instruction goes LAST, after the evidence it applies to. Anything
	// appended here is outside every cached prefix, so keep it short.
	b.WriteString("\nJudge issue #" + strconv.Itoa(spec.Issue.Number) +
		" against the charter and end with its verdict declaration.\n")

	return b.String(), nil
}

// writeTriageComments renders the issue's discussion, or says there is none.
//
// A RE-TRIAGE SAYS WHY IT IS ONE. An issue triage has already judged is
// re-judged only because somebody spoke after that verdict (spokenToSinceTriage),
// and what they said is the answer to it: the fact needs-info asked for, or the
// ruling an escalation waited on. Rendered flat, that answer is one comment
// among many, and a small model reads the old verdict's "blocked on an owner
// ruling" over the ruling posted right under it -- measured on go-luks2#12
// (2026-09-21): the NPU re-asked for a ruling the thread already carried. So
// the comments after triage's last verdict are marked as the answer, and the
// discussion opens by saying which verdict they answer.
func writeTriageComments(b *strings.Builder, comments []ForgeComment) {
	lastVerdict := lastTriageVerdictIndex(comments)
	if lastVerdict >= 0 && spokenAfter(comments, lastVerdict) {
		b.WriteString("Triage has judged this issue before; its last verdict is the comment " +
			"marked below. It is being judged AGAIN because somebody spoke after that " +
			"verdict, and what they said is the answer to it: a ruling given there is " +
			"settled, a fact supplied there is supplied. Do not ask for either again.\n\n")
	}
	rendered := 0
	for i, comment := range comments {
		if strings.TrimSpace(comment.Body) == "" {
			continue
		}
		rendered++
		heading := "### Comment " + strconv.Itoa(rendered) + commentAttribution(comment)
		switch {
		case lastVerdict >= 0 && i == lastVerdict:
			heading += " -- TRIAGE'S LAST VERDICT"
		case lastVerdict >= 0 && i > lastVerdict:
			heading += " -- POSTED AFTER TRIAGE'S LAST VERDICT: THIS IS THE ANSWER TO IT"
		}
		b.WriteString(heading + "\n\n")
		b.WriteString(strings.TrimSpace(comment.Body) + "\n\n")
	}
	if rendered == 0 {
		b.WriteString("(none)\n")
	}
}

// lastTriageVerdictIndex is the index of the newest comment triage itself
// posted, or -1 when triage has never spoken on this issue.
func lastTriageVerdictIndex(comments []ForgeComment) int {
	last := -1
	for i, comment := range comments {
		if strings.Contains(comment.Body, triageCommentMarker) {
			last = i
		}
	}
	return last
}

// spokenAfter reports whether any non-empty comment follows index last.
func spokenAfter(comments []ForgeComment, last int) bool {
	for _, comment := range comments[last+1:] {
		if strings.TrimSpace(comment.Body) != "" {
			return true
		}
	}
	return false
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
