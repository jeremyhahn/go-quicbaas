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
	"slices"
	"strings"
	"testing"
)

// =============================================================================
// The triage declaration is read from structure, never from prose
// =============================================================================

// TestParseTriageDeclaration_ReadsTheClosedSet pins the parse: the code, the
// duplicate reference, and the cases where there is nothing to read.
func TestParseTriageDeclaration_ReadsTheClosedSet(t *testing.T) {
	tests := []struct {
		name          string
		reply         string
		wantCode      string
		wantDeclared  bool
		wantDuplicate int
	}{
		{
			name:         "a ready verdict on the closing line",
			reply:        "The objective is verifiable and scoped.\n\nSDLC-TRIAGE: ready",
			wantCode:     TriageCodeReady,
			wantDeclared: true,
		},
		{
			name:         "needs-info",
			reply:        "No reproduction is given.\n\nSDLC-TRIAGE: needs-info",
			wantCode:     TriageCodeNeedsInfo,
			wantDeclared: true,
		},
		{
			name:         "not-actionable",
			reply:        "main already carries this; see README.md:1.\n\nSDLC-TRIAGE: not-actionable",
			wantCode:     TriageCodeNotActionable,
			wantDeclared: true,
		},
		{
			name:          "a duplicate carries the issue it duplicates",
			reply:         "Same stale README as #25.\n\nSDLC-TRIAGE-DUPLICATE: #25\nSDLC-TRIAGE: duplicate",
			wantCode:      TriageCodeDuplicate,
			wantDeclared:  true,
			wantDuplicate: 25,
		},
		{
			name:          "a duplicate reference without the hash",
			reply:         "SDLC-TRIAGE-DUPLICATE: 52\nSDLC-TRIAGE: duplicate",
			wantCode:      TriageCodeDuplicate,
			wantDeclared:  true,
			wantDuplicate: 52,
		},
		{
			name:         "decoration around the declaration is tolerated",
			reply:        "done\n\n**`SDLC-TRIAGE: ready`**\n",
			wantCode:     TriageCodeReady,
			wantDeclared: true,
		},
		{
			name:         "the last declaration wins over one quoted mid-text",
			reply:        "I considered `SDLC-TRIAGE: ready` but the objective is vague.\n\nSDLC-TRIAGE: needs-info",
			wantCode:     TriageCodeNeedsInfo,
			wantDeclared: true,
		},
		{
			name:         "an unknown code is read as declared and left to the table",
			reply:        "SDLC-TRIAGE: maybe",
			wantCode:     "maybe",
			wantDeclared: true,
		},
		{
			name:         "a reply with no declaration declares nothing",
			reply:        "I think this issue looks fine and is ready to work on.",
			wantCode:     "",
			wantDeclared: false,
		},
		{
			name:         "an empty reply declares nothing",
			reply:        "",
			wantCode:     "",
			wantDeclared: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decl := ParseTriageDeclaration(tc.reply)
			if decl.Code != tc.wantCode {
				t.Fatalf("code: want %q, got %q", tc.wantCode, decl.Code)
			}
			if decl.Declared != tc.wantDeclared {
				t.Fatalf("declared: want %v, got %v", tc.wantDeclared, decl.Declared)
			}
			if decl.DuplicateOf != tc.wantDuplicate {
				t.Fatalf("duplicate of: want %d, got %d", tc.wantDuplicate, decl.DuplicateOf)
			}
		})
	}
}

// TestParseTriageDeclaration_RefusesAnUnreadableDuplicateReference proves a
// reference that is not a positive issue number is read as absent rather than
// as issue zero.
func TestParseTriageDeclaration_RefusesAnUnreadableDuplicateReference(t *testing.T) {
	tests := []string{
		"SDLC-TRIAGE-DUPLICATE: #abc\nSDLC-TRIAGE: duplicate",
		"SDLC-TRIAGE-DUPLICATE: #0\nSDLC-TRIAGE: duplicate",
		"SDLC-TRIAGE-DUPLICATE: #-3\nSDLC-TRIAGE: duplicate",
		"SDLC-TRIAGE-DUPLICATE:\nSDLC-TRIAGE: duplicate",
	}
	for _, reply := range tests {
		t.Run(strings.SplitN(reply, "\n", 2)[0], func(t *testing.T) {
			decl := ParseTriageDeclaration(reply)
			if decl.DuplicateOf != 0 {
				t.Fatalf("an unreadable reference is absent, got %d", decl.DuplicateOf)
			}
		})
	}
}

// =============================================================================
// Classification
// =============================================================================

// TestClassifyTriage_EveryCodeHasADisposition walks the vocabulary and proves
// each code reaches exactly the disposition the design states.
func TestClassifyTriage_EveryCodeHasADisposition(t *testing.T) {
	tests := []struct {
		name            string
		decl            TriageDeclaration
		wantOutcome     TriageOutcome
		wantDisposition TriageDisposition
	}{
		{
			name:            "ready applies the trigger label",
			decl:            TriageDeclaration{Code: TriageCodeReady, Declared: true},
			wantOutcome:     TriageOutcomeReady,
			wantDisposition: TriageDispositionReady,
		},
		{
			name:            "needs-info labels and comments",
			decl:            TriageDeclaration{Code: TriageCodeNeedsInfo, Declared: true},
			wantOutcome:     TriageOutcomeNeedsInfo,
			wantDisposition: TriageDispositionNeedsInfo,
		},
		{
			name:            "not-actionable comments and leaves it",
			decl:            TriageDeclaration{Code: TriageCodeNotActionable, Declared: true},
			wantOutcome:     TriageOutcomeNotActionable,
			wantDisposition: TriageDispositionNotActionable,
		},
		{
			name:            "a duplicate with a reference is not actionable",
			decl:            TriageDeclaration{Code: TriageCodeDuplicate, Declared: true, DuplicateOf: 25},
			wantOutcome:     TriageOutcomeDuplicate,
			wantDisposition: TriageDispositionNotActionable,
		},
		{
			name:            "a duplicate with no reference is unreadable, never ready",
			decl:            TriageDeclaration{Code: TriageCodeDuplicate, Declared: true},
			wantOutcome:     TriageOutcomeUnreadable,
			wantDisposition: TriageDispositionNotActionable,
		},
		{
			name:            "an unknown code is unreadable, never ready",
			decl:            TriageDeclaration{Code: "maybe", Declared: true},
			wantOutcome:     TriageOutcomeUnreadable,
			wantDisposition: TriageDispositionNotActionable,
		},
		{
			name:            "no declaration at all is undeclared, never ready",
			decl:            TriageDeclaration{},
			wantOutcome:     TriageOutcomeUndeclared,
			wantDisposition: TriageDispositionNotActionable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verdict := ClassifyTriage(tc.decl)
			if verdict.Outcome != tc.wantOutcome {
				t.Fatalf("outcome: want %q, got %q", tc.wantOutcome, verdict.Outcome)
			}
			if verdict.Disposition != tc.wantDisposition {
				t.Fatalf("disposition: want %q, got %q", tc.wantDisposition, verdict.Disposition)
			}
			if verdict.Headline == "" {
				t.Fatal("every verdict introduces itself to a person")
			}
		})
	}
}

// TestClassifyTriage_OnlyReadyIsEverReady is the safety property stated as a
// property rather than as a list: no outcome other than the declared ready
// verdict may apply the trigger label.
func TestClassifyTriage_OnlyReadyIsEverReady(t *testing.T) {
	for outcome, verdict := range triageVerdicts {
		if outcome == TriageOutcomeReady {
			if verdict.Disposition != TriageDispositionReady {
				t.Fatalf("the ready outcome must be ready, got %q", verdict.Disposition)
			}
			continue
		}
		if verdict.Disposition == TriageDispositionReady {
			t.Fatalf("outcome %q must not reach the trigger label", outcome)
		}
	}
}

// TestTriageVerdictTable_IsComplete proves every outcome the classifier can
// produce has a row. An outcome with no row would be one the runner could not
// act on.
func TestTriageVerdictTable_IsComplete(t *testing.T) {
	outcomes := []TriageOutcome{
		TriageOutcomeReady,
		TriageOutcomeNeedsInfo,
		TriageOutcomeNotActionable,
		TriageOutcomeDuplicate,
		TriageOutcomeUnreadable,
		TriageOutcomeUndeclared,
	}
	for _, outcome := range outcomes {
		if _, ok := triageVerdicts[outcome]; !ok {
			t.Fatalf("outcome %q has no verdict row", outcome)
		}
	}
	if len(triageVerdicts) != len(outcomes) {
		t.Fatalf("the table has %d rows for %d outcomes; a row nothing produces is dead", len(triageVerdicts), len(outcomes))
	}
}

// TestTriageCodeOutcomes_MapsOnlyTheClosedSet proves the code vocabulary is
// exactly the four the prompt names.
func TestTriageCodeOutcomes_MapsOnlyTheClosedSet(t *testing.T) {
	want := map[string]TriageOutcome{
		TriageCodeReady:         TriageOutcomeReady,
		TriageCodeNeedsInfo:     TriageOutcomeNeedsInfo,
		TriageCodeNotActionable: TriageOutcomeNotActionable,
		TriageCodeDuplicate:     TriageOutcomeDuplicate,
	}
	if len(triageCodeOutcomes) != len(want) {
		t.Fatalf("the code set has %d entries, want %d", len(triageCodeOutcomes), len(want))
	}
	for code, outcome := range want {
		if got := triageCodeOutcomes[code]; got != outcome {
			t.Fatalf("code %q: want %q, got %q", code, outcome, got)
		}
	}
}

// =============================================================================
// The verdict a person reads
// =============================================================================

// TestTriageComment_NamesWhatIsMissing proves the comment carries the agent's
// own evidence rather than a generic sentence, and that a duplicate names the
// issue it duplicates.
func TestTriageComment_NamesWhatIsMissing(t *testing.T) {
	tests := []struct {
		name     string
		verdict  TriageVerdict
		evidence string
		contains []string
	}{
		{
			name:     "needs-info carries the evidence",
			verdict:  ClassifyTriage(TriageDeclaration{Code: TriageCodeNeedsInfo, Declared: true}),
			evidence: "The issue names no file and no reproduction.",
			contains: []string{"The issue names no file and no reproduction."},
		},
		{
			name: "needs-info numbers the questions it asks",
			verdict: ClassifyTriage(TriageDeclaration{Code: TriageCodeNeedsInfo, Declared: true,
				Questions: []string{"Which overload?", "Which version?"}}),
			evidence: "The issue names no overload and no version.",
			contains: []string{"Questions for the author", "1. Which overload?\n", "2. Which version?\n"},
		},
		{
			name:     "a duplicate names the issue it duplicates",
			verdict:  ClassifyTriage(TriageDeclaration{Code: TriageCodeDuplicate, Declared: true, DuplicateOf: 25}),
			evidence: "Same stale README.",
			contains: []string{"#25", "Same stale README."},
		},
		{
			name:     "an undeclared reply says so plainly",
			verdict:  ClassifyTriage(TriageDeclaration{}),
			evidence: "",
			contains: []string{"declaration"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := TriageComment(tc.verdict, tc.evidence)
			if strings.TrimSpace(body) == "" {
				t.Fatal("a verdict a person must act on cannot be an empty comment")
			}
			for _, want := range tc.contains {
				if !strings.Contains(body, want) {
					t.Fatalf("comment must contain %q:\n%s", want, body)
				}
			}
		})
	}
}

// TestTriageComment_RefusesToCommentOnReady proves the ready path writes a
// label and nothing else: a comment on every ready issue would be noise on
// the one verdict that already speaks through the label.
func TestTriageComment_RefusesToCommentOnReady(t *testing.T) {
	verdict := ClassifyTriage(TriageDeclaration{Code: TriageCodeReady, Declared: true})
	if verdict.Comments {
		t.Fatal("the ready verdict does not comment")
	}
	if body := TriageComment(verdict, "anything"); body != "" {
		t.Fatalf("a ready verdict renders no comment, got %q", body)
	}
}

// =============================================================================
// Errors
// =============================================================================

// TestTriageErrors_AreTypedAndUnwrap proves each failure is a type carrying
// its own fields and unwrapping to a sentinel, never a formatted string.
func TestTriageErrors_AreTypedAndUnwrap(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
		contains string
	}{
		{
			name:     "an issue that could not be triaged",
			err:      &TriageVerdictError{Issue: 12, Reason: "the session answered nothing"},
			sentinel: ErrTriageVerdict,
			contains: "12",
		},
		{
			name:     "a project with no binding",
			err:      &TriageNotBoundError{ProjectID: "proj-1"},
			sentinel: ErrTriageNotBound,
			contains: "proj-1",
		},
		{
			name:     "a session the run could not hold",
			err:      &TriageSessionError{ProjectID: "proj-1", Message: "the spawn returned no session"},
			sentinel: ErrTriageSession,
			contains: "the spawn returned no session",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.sentinel) {
				t.Fatalf("%T must unwrap to its sentinel", tc.err)
			}
			if !strings.Contains(tc.err.Error(), tc.contains) {
				t.Fatalf("error must name %q, got %q", tc.contains, tc.err.Error())
			}
		})
	}
}

// TestParseTriageDeclaration_CloseMarker pins the marker that asks for a close.
//
// IT IS THE ONE IRREVERSIBLE THING TRIAGE CAN ASK FOR, so the parser must be
// strict: only the exact reason counts, and a verdict carrying no marker never
// closes however the binding is configured.
func TestParseTriageDeclaration_CloseMarker(t *testing.T) {
	asks := ParseTriageDeclaration("evidence\n\nSDLC-TRIAGE-CLOSE: already-done\nSDLC-TRIAGE: not-actionable")
	if !asks.ClosesAsAlreadyDone {
		t.Fatal("the close marker was not read")
	}
	if asks.Code != TriageCodeNotActionable {
		t.Fatalf("the verdict must still be not-actionable, got %q", asks.Code)
	}

	// Decorated the way a model writes it.
	if d := ParseTriageDeclaration("> **SDLC-TRIAGE-CLOSE:** `already-done`\nSDLC-TRIAGE: not-actionable"); !d.ClosesAsAlreadyDone {
		t.Fatal("a decorated marker must still be read")
	}

	// A REASON NOBODY DEFINED DOES NOT CLOSE. A model inventing its own
	// justification must not be able to close an issue with it.
	for _, invented := range []string{"out-of-scope", "wontfix", "stale", "", "yes"} {
		d := ParseTriageDeclaration("SDLC-TRIAGE-CLOSE: " + invented + "\nSDLC-TRIAGE: not-actionable")
		if d.ClosesAsAlreadyDone {
			t.Fatalf("reason %q must not close", invented)
		}
	}

	// No marker at all.
	if d := ParseTriageDeclaration("SDLC-TRIAGE: not-actionable"); d.ClosesAsAlreadyDone {
		t.Fatal("a bare not-actionable must never close")
	}
}

func TestParseTriageDeclaration_CollectsQuestions(t *testing.T) {
	long := strings.Repeat("é", triageMaxQuestionLen+40)
	tests := []struct {
		name  string
		reply string
		want  []string
	}{
		{
			name: "several questions, in reply order",
			reply: "Two facts are missing.\n\nSDLC-TRIAGE-QUESTION: Which overload?\n" +
				"> **SDLC-TRIAGE-QUESTION: Which version?**\nSDLC-TRIAGE: needs-info",
			want: []string{"Which overload?", "Which version?"},
		},
		{
			name:  "an empty question line asks nothing",
			reply: "SDLC-TRIAGE-QUESTION:   \nSDLC-TRIAGE-QUESTION: Which overload?\nSDLC-TRIAGE: needs-info",
			want:  []string{"Which overload?"},
		},
		{
			name:  "an over-long question is cut",
			reply: "SDLC-TRIAGE-QUESTION: " + long + "\nSDLC-TRIAGE: needs-info",
			want:  []string{long[:triageMaxQuestionLen*len("é")]},
		},
		{
			name: "questions past the bound are dropped",
			reply: "SDLC-TRIAGE-QUESTION: q1\nSDLC-TRIAGE-QUESTION: q2\nSDLC-TRIAGE-QUESTION: q3\n" +
				"SDLC-TRIAGE-QUESTION: q4\nSDLC-TRIAGE-QUESTION: q5\nSDLC-TRIAGE-QUESTION: q6\nSDLC-TRIAGE: needs-info",
			want: []string{"q1", "q2", "q3", "q4", "q5"},
		},
		{
			name:  "a reply with no question",
			reply: "The issue names no overload.\n\nSDLC-TRIAGE: needs-info",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decl := ParseTriageDeclaration(tc.reply)
			if !slices.Equal(decl.Questions, tc.want) {
				t.Fatalf("Questions = %q, want %q", decl.Questions, tc.want)
			}
			if verdict := ClassifyTriage(decl); !slices.Equal(verdict.Questions, tc.want) {
				t.Fatalf("the verdict carries %q, want %q", verdict.Questions, tc.want)
			}
			if !decl.Declared || decl.Code != TriageCodeNeedsInfo {
				t.Fatalf("the questions must not disturb the verdict line, got %+v", decl)
			}
		})
	}
}
