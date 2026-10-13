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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
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
		TriageQuestionMarker,
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
	// AN ISSUE THAT ASKS FOR A RULING IS THE UNRESOLVED QUESTION ITSELF. On
	// 2026-09-21 the 8B triage model marked two owner-ruling issues ready and
	// the poller adopted one; the charter now names that case so a small
	// model does not have to infer it from "depends on a decision".
	if !strings.Contains(strings.Join(strings.Fields(charter), " "), "asks the owner to rule, decide, or choose between options") {
		t.Fatal("the charter must say that an issue asking the owner to rule is needs-info, never ready")
	}
	// A TRACKER IS NEVER WORK. On 2026-09-25 triage marked two "Release
	// readiness" trackers ready (go-blockstore#75, go-frost#30) although their
	// bodies said they were a checklist, not work, and both workers stopped on
	// owner questions. The charter names the tracker, its signs, and its
	// verdict, so a model does not have to infer them.
	flat := strings.Join(strings.Fields(charter), " ")
	for _, want := range []string{
		"An issue that tracks other work is never itself work",
		"it lists child issues, or carries checkboxes over several changes",
		"Its verdict is `not-actionable`, left for a person, with a one-line reason",
		"it is a tracker rather than work",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("the charter must state %q: a tracking checklist is not-actionable, never ready", want)
		}
	}
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

// TestTriagePromptRevisionPinned fails whenever the charter or TriagePrompt's
// own text changes without TriagePromptRevision changing with it, so a verdict
// marker never names a prompt it was not written under. The renders cover
// both closing branches, a re-judged discussion, and the empty sections.
func TestTriagePromptRevisionPinned(t *testing.T) {
	asked := time.Unix(1_700_000_000, 0).UTC()
	full := TriagePromptSpec{
		Repo:  RepoRef{Owner: "acme", Repo: "api"},
		Issue: ForgeIssue{Number: 7, Title: "Fix the encoder", Body: "Encode panics."},
		Comments: []ForgeComment{
			{Body: "Which overload?\n<!-- " + triageCommentMarker + " -->", Author: "agent", Created: asked},
			{Body: "The streaming one.", Author: "author", Created: asked.Add(time.Hour)},
		},
		OpenIssues:   []ForgeIssue{{Number: 7, Title: "Fix the encoder", State: "open"}},
		Verification: []string{"go test ./..."},
		Facts:        "## Facts\n\nnone",
	}
	closing := full
	closing.MayClose = true
	bare := TriagePromptSpec{Repo: full.Repo, Issue: ForgeIssue{Number: 7, Title: "Fix the encoder"}}

	template := TriageCharter()
	for _, spec := range []TriagePromptSpec{full, closing, bare} {
		prompt, err := TriagePrompt(spec)
		if err != nil {
			t.Fatalf("TriagePrompt: %v", err)
		}
		template += "\x00" + prompt
	}
	digest := sha256.Sum256([]byte(template))
	if got := hex.EncodeToString(digest[:])[:len(TriagePromptRevision)]; got != TriagePromptRevision {
		t.Fatalf("the triage prompt changed: set TriagePromptRevision to %q", got)
	}
}

// =============================================================================
// The per-issue prompt
// =============================================================================

// TestTriagePrompt_CarriesEverythingTheVerdictNeeds proves the rendered
// prompt names the issue under triage, the other open issues a duplicate is
// judged against, and the verification commands check seven reads.
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

// TestTriagePrompt_MarksTheAnswerToTriagesLastVerdict proves a re-triage says
// why it is one: the comment triage last posted is marked as the verdict, and
// every comment after it as the answer, with an opening line that says a ruling
// given there is settled. Measured need: the NPU re-asked go-luks2#12 for a
// ruling posted directly under its previous verdict.
func TestTriagePrompt_MarksTheAnswerToTriagesLastVerdict(t *testing.T) {
	spec := TriagePromptSpec{
		Repo:  RepoRef{Owner: "jeremyhahn", Repo: "go-luks2"},
		Issue: ForgeIssue{Number: 12, Title: "Adopt Go 1.27.0", State: "open"},
		Comments: []ForgeComment{
			{Body: "Blocked pending the owner's ruling.", Author: "agent-company"},
			{Body: "Triage cannot hand this to a worker yet.\n\n<!-- " + triageCommentMarker + " verdict=not_actionable -->", Author: "agent-go-luks2"},
			{Body: "Ruling: 1.27.0 is the standard.", Author: "jeremyhahn"},
		},
	}
	prompt, err := TriagePrompt(spec)
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}
	discussion := prompt[strings.Index(prompt, "### Discussion"):]
	for _, want := range []string{
		"a ruling given there is settled",
		"### Comment 2 — @agent-go-luks2 -- TRIAGE'S LAST VERDICT",
		"### Comment 3 — @jeremyhahn -- POSTED AFTER TRIAGE'S LAST VERDICT: THIS IS THE ANSWER TO IT",
	} {
		if !strings.Contains(discussion, want) {
			t.Fatalf("discussion must carry %q:\n%s", want, discussion)
		}
	}
	if strings.Contains(discussion, "### Comment 1 — @agent-company --") {
		t.Fatalf("a comment before the verdict must carry no mark:\n%s", discussion)
	}
}

// TestTriagePrompt_ALongDiscussionKeepsItsNewestComments proves the budget:
// the oldest comments are dropped with a count, the newest -- triage's last
// verdict and the answer to it -- survive whole, and a thread that fits is
// untouched.
func TestTriagePrompt_ALongDiscussionKeepsItsNewestComments(t *testing.T) {
	big := strings.Repeat("x", triageDiscussionBudget/2)
	comments := []ForgeComment{
		{Body: "first " + big, Author: "a"},
		{Body: "second " + big, Author: "b"},
		{Body: "third " + big, Author: "c"},
		{Body: "verdict <!-- " + triageCommentMarker + " -->", Author: "agent-go-luks2"},
		{Body: "the answer", Author: "jeremyhahn"},
	}
	prompt, err := TriagePrompt(TriagePromptSpec{
		Repo: RepoRef{Owner: "o", Repo: "r"}, Issue: ForgeIssue{Number: 23, Title: "t", State: "open"}, Comments: comments,
	})
	if err != nil {
		t.Fatalf("TriagePrompt: %v", err)
	}
	if strings.Contains(prompt, "first ") || strings.Contains(prompt, "second ") {
		t.Fatal("the oldest comments must be dropped")
	}
	for _, want := range []string{"2 earlier comment(s) omitted", "third ", "the answer", "THIS IS THE ANSWER TO IT"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt must carry %q", want)
		}
	}
	kept, omitted := budgetTriageComments(comments[3:], triageDiscussionBudget)
	if omitted != 0 || len(kept) != 2 {
		t.Fatalf("a thread that fits is untouched, got kept=%d omitted=%d", len(kept), omitted)
	}
	one, omitted := budgetTriageComments([]ForgeComment{{Body: strings.Repeat("y", triageDiscussionBudget*2)}}, triageDiscussionBudget)
	if omitted != 0 || len(one) != 1 {
		t.Fatal("the newest comment is never omitted, however large")
	}
}

// TestTriagePrompt_AFirstTriageMarksNothing proves the marks and the opening
// line appear only on a re-triage: a thread triage has never judged, and a
// thread whose newest comment IS triage's verdict, both render flat.
func TestTriagePrompt_AFirstTriageMarksNothing(t *testing.T) {
	for name, comments := range map[string][]ForgeComment{
		"never judged":      {{Body: "Still broken on main.", Author: "jeremyhahn"}},
		"verdict newest":    {{Body: "Still broken.", Author: "jeremyhahn"}, {Body: "<!-- " + triageCommentMarker + " verdict=needs_info -->", Author: "agent-go-luks2"}},
		"only blanks after": {{Body: "<!-- " + triageCommentMarker + " -->", Author: "agent-go-luks2"}, {Body: "   ", Author: "jeremyhahn"}},
	} {
		prompt, err := TriagePrompt(TriagePromptSpec{
			Repo:     RepoRef{Owner: "jeremyhahn", Repo: "go-luks2"},
			Issue:    ForgeIssue{Number: 12, Title: "Adopt Go 1.27.0", State: "open"},
			Comments: comments,
		})
		if err != nil {
			t.Fatalf("%s: TriagePrompt: %v", name, err)
		}
		for _, absent := range []string{"THIS IS THE ANSWER TO IT", "a ruling given there is settled"} {
			if strings.Contains(prompt, absent) {
				t.Fatalf("%s: a first triage must not carry %q:\n%s", name, absent, prompt)
			}
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
