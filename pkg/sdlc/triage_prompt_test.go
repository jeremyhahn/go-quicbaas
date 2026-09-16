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
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// The charter
// =============================================================================

// TestTriageCharter_IsEditableTextCarryingTheContract proves the prompt is
// the embedded markdown and that it still states the parts the runner's
// behaviour depends on. A charter that stopped naming the declaration would
// leave every reply undeclared, and the runner would correctly refuse to
// mark anything ready — silently, and for every project at once.
func TestTriageCharter_IsEditableTextCarryingTheContract(t *testing.T) {
	charter := TriageCharter()
	if strings.TrimSpace(charter) == "" {
		t.Fatal("the triage charter is empty; the go:embed did not take")
	}
	required := []string{
		TriageDeclarationMarker,
		TriageDuplicateMarker,
		TriageCodeReady,
		TriageCodeNeedsInfo,
		TriageCodeNotActionable,
		TriageCodeDuplicate,
	}
	for _, want := range required {
		if !strings.Contains(charter, want) {
			t.Fatalf("the charter must state %q, or the runner cannot read a reply", want)
		}
	}
	// THE CLOSE IS GATED, NOT FORBIDDEN (owner decision 2026-09-15). The
	// charter used to say "Never propose closing the issue" outright; it now
	// makes closing conditional on the project enabling it, because a project
	// that wants its stale reports closed should be able to say so. What must
	// NOT be lost is the default and the reason for it — an agent reading this
	// with no other context has to come away knowing it may not ask by
	// default, and why closing is weightier than labelling.
	if !strings.Contains(charter, "unless you are told closing is") {
		t.Fatal("the charter must state that closing is off unless the project enabled it")
	}
	if !strings.Contains(charter, "carries no social weight") {
		t.Fatal("the charter must keep the reason closing is treated differently from labelling")
	}
	// The marker the runner parses, and the one reason it may carry.
	if !strings.Contains(charter, TriageCloseMarker) {
		t.Fatalf("the charter must state %q, or an agent cannot ask for a close", TriageCloseMarker)
	}
	if !strings.Contains(charter, TriageCloseReasonAlreadyDone) {
		t.Fatalf("the charter must state %q, the only reason that closes", TriageCloseReasonAlreadyDone)
	}
}

// TestTriageCharter_IsNotAGoConstant proves the prompt lives in editable
// markdown rather than in a string literal. It reads the embedded file
// directly and compares: a maintainer who moved the text into a .go constant
// and left a stub file would fail here.
func TestTriageCharter_IsNotAGoConstant(t *testing.T) {
	raw, err := triagePrompts.ReadFile("prompts/triage.md")
	if err != nil {
		t.Fatalf("the charter must be an embedded file: %v", err)
	}
	if len(raw) < 1024 {
		t.Fatalf("the embedded charter is %d bytes; the prompt does not live in the file", len(raw))
	}
	if TriageCharter() != strings.TrimSpace(string(raw)) {
		t.Fatal("TriageCharter must serve the embedded file, not a copy that can drift from it")
	}
}

// =============================================================================
// The per-issue prompt
// =============================================================================

// TestTriagePrompt_CarriesEverythingTheVerdictNeeds proves the rendered
// prompt names the issue under triage, the other open issues a duplicate is
// judged against, and the verification commands check six reads.
func TestTriagePrompt_CarriesEverythingTheVerdictNeeds(t *testing.T) {
	spec := TriagePromptSpec{
		Repo:         RepoRef{Owner: "jeremyhahn", Repo: "go-truststrap"},
		Issue:        ForgeIssue{Number: 52, Title: "README is stale", Body: "The README describes the old CLI.", State: "open"},
		OpenIssues:   []ForgeIssue{{Number: 25, Title: "README is stale", State: "open"}, {Number: 60, Title: "Add ARI support", State: "open"}},
		Comments:     []ForgeComment{{Body: "Still broken on main.", Author: "jeremyhahn"}},
		Verification: []string{"make test", "gofmt -l ."},
	}
	prompt, err := TriagePrompt(spec)
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}
	for _, want := range []string{
		"jeremyhahn/go-truststrap",
		"#52",
		"README is stale",
		"The README describes the old CLI.",
		"Still broken on main.",
		"#25",
		"#60",
		"make test",
		"gofmt -l .",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt must carry %q:\n%s", want, prompt)
		}
	}
}

// TestTriagePrompt_SaysAnIssueIsNeverItsOwnDuplicate guards the one duplicate
// verdict that is always wrong.
//
// THE MECHANISM CHANGED AND THE GUARANTEE DID NOT. The list used to exclude
// the subject, which made it differ for every issue and destroyed the shared
// prefix the whole section now exists to provide. It includes the subject
// instead, and SAYS SO — a list that silently contained the issue under triage
// would invite exactly the mistake excluding it used to prevent.
func TestTriagePrompt_SaysAnIssueIsNeverItsOwnDuplicate(t *testing.T) {
	spec := TriagePromptSpec{
		Repo:       RepoRef{Owner: "o", Repo: "r"},
		Issue:      ForgeIssue{Number: 52, Title: "README is stale", State: "open"},
		OpenIssues: []ForgeIssue{{Number: 52, Title: "README is stale", State: "open"}},
	}
	prompt, err := TriagePrompt(spec)
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}
	section := triageCandidateSection(t, prompt)
	if !strings.Contains(section, "#52") {
		t.Fatalf("the list must carry every open issue, subject included, or it is not shared:\n%s", section)
	}
	if !strings.Contains(section, "never a duplicate of itself") {
		t.Fatalf("the prompt includes the subject in its own list without saying so:\n%s", section)
	}
}

// TestTriagePrompt_TheProjectStablePrefixComesFirst is the property that makes
// prefix caching work, and it is worth a test because it is invisible: a
// reordering that looks cosmetic silently costs a full prefill per issue.
//
// A triage turn is ~94% prefill (5,600-7,500 tokens in, 24-44 out), so two
// issues from one sweep must share every byte up to the subject. Measured
// before this ordering: the prompts diverged on the THIRD line.
func TestTriagePrompt_TheProjectStablePrefixComesFirst(t *testing.T) {
	open := []ForgeIssue{
		{Number: 52, Title: "README is stale", State: "open"},
		{Number: 77, Title: "Parse returns an untyped error", State: "open"},
	}
	base := TriagePromptSpec{
		Repo:         RepoRef{Owner: "o", Repo: "r"},
		OpenIssues:   open,
		Verification: []string{"make build", "make test"},
		MayClose:     true,
	}
	first, second := base, base
	first.Issue = open[0]
	second.Issue = open[1]

	a, err := TriagePrompt(first)
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}
	b, err := TriagePrompt(second)
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}

	shared := 0
	for shared < len(a) && shared < len(b) && a[shared] == b[shared] {
		shared++
	}
	marker := strings.Index(a, "## The issue to triage:")
	if marker < 0 {
		t.Fatalf("the per-issue section is not marked:\n%s", a)
	}
	if shared < marker {
		t.Fatalf("prompts diverge at byte %d, before the per-issue section at %d; "+
			"everything above it must be identical across a sweep", shared, marker)
	}
	// And the subject must genuinely come last, or the ordering bought nothing.
	if strings.Index(a, "### "+open[0].Title) < marker {
		t.Fatal("the issue body appears above the per-issue marker")
	}
}

// TestTriagePrompt_OmitsClosedIssuesFromDuplicateCandidates proves a closed
// issue is not a duplicate candidate: work already closed is not work this
// issue duplicates.
func TestTriagePrompt_OmitsClosedIssuesFromDuplicateCandidates(t *testing.T) {
	spec := TriagePromptSpec{
		Repo:  RepoRef{Owner: "o", Repo: "r"},
		Issue: ForgeIssue{Number: 52, Title: "README is stale", State: "open"},
		OpenIssues: []ForgeIssue{
			{Number: 25, Title: "old and closed", State: "closed"},
			{Number: 26, Title: "open and comparable", State: "open"},
		},
	}
	prompt, err := TriagePrompt(spec)
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}
	section := triageCandidateSection(t, prompt)
	if strings.Contains(section, "#25") {
		t.Fatalf("a closed issue is not a duplicate candidate:\n%s", section)
	}
	if !strings.Contains(section, "#26") {
		t.Fatalf("an open issue is:\n%s", section)
	}
}

// TestTriagePrompt_RefusesASpecItCannotRender proves the invalid inputs are
// typed refusals rather than a prompt that quietly names nothing.
func TestTriagePrompt_RefusesASpecItCannotRender(t *testing.T) {
	tests := []struct {
		name  string
		spec  TriagePromptSpec
		field string
	}{
		{
			name:  "no repository",
			spec:  TriagePromptSpec{Issue: ForgeIssue{Number: 1, Title: "t"}},
			field: "repo",
		},
		{
			name:  "no issue number",
			spec:  TriagePromptSpec{Repo: RepoRef{Owner: "o", Repo: "r"}, Issue: ForgeIssue{Title: "t"}},
			field: "issue_number",
		},
		{
			name:  "no title",
			spec:  TriagePromptSpec{Repo: RepoRef{Owner: "o", Repo: "r"}, Issue: ForgeIssue{Number: 1}},
			field: "title",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TriagePrompt(tc.spec)
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("want *InvalidInputError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("field: want %q, got %q", tc.field, invalid.Field)
			}
		})
	}
}

// TestTriagePrompt_StatesWhenTheProjectVerifiesNothing proves an absent
// verification list is stated rather than rendered as an empty heading the
// agent has to interpret.
func TestTriagePrompt_StatesWhenTheProjectVerifiesNothing(t *testing.T) {
	spec := TriagePromptSpec{
		Repo:  RepoRef{Owner: "o", Repo: "r"},
		Issue: ForgeIssue{Number: 1, Title: "t", State: "open"},
	}
	prompt, err := TriagePrompt(spec)
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}
	if !strings.Contains(prompt, "(none configured)") {
		t.Fatalf("a project with no verification commands says so:\n%s", prompt)
	}
}

// triageCandidateSection returns the duplicate-candidate section of a
// rendered prompt, so a test can assert about it without matching text that
// legitimately appears elsewhere.
func triageCandidateSection(t *testing.T, prompt string) string {
	t.Helper()
	const heading = "## Open issues in this project"
	start := strings.Index(prompt, heading)
	if start < 0 {
		t.Fatalf("prompt has no duplicate-candidate section:\n%s", prompt)
	}
	rest := prompt[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}
