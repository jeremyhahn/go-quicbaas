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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

func TestParseReviewVerdict_ReadsTheDeclarationAndTheFindings(t *testing.T) {
	for name, tc := range map[string]struct {
		reply        string
		wantVerdict  ReviewVerdict
		wantFindings string
	}{
		"approve": {
			"The error paths are covered.\n\n" + ReviewVerdictMarker + " " + ReviewCodeApprove,
			VerdictApprove, "The error paths are covered.",
		},
		"request changes": {
			"pkg/a.go:12 swallows the cause.\n\n" + ReviewVerdictMarker + " " + ReviewCodeRequestChanges,
			VerdictRequestChanges, "pkg/a.go:12 swallows the cause.",
		},
		"comment": {
			"I cannot judge the cryptography.\n\n" + ReviewVerdictMarker + " " + ReviewCodeComment,
			VerdictComment, "I cannot judge the cryptography.",
		},
		"decorated by the agent": {
			"findings\n\n> **`" + ReviewVerdictMarker + " approve`**",
			VerdictApprove, "findings",
		},
		"uppercased": {
			"findings\n\n" + ReviewVerdictMarker + " APPROVE",
			VerdictApprove, "findings",
		},
		"trailing blank lines": {
			"findings\n\n" + ReviewVerdictMarker + " approve\n\n   \n",
			VerdictApprove, "findings",
		},
	} {
		t.Run(name, func(t *testing.T) {
			outcome, err := ParseReviewVerdict(tc.reply)
			if err != nil {
				t.Fatalf("ParseReviewVerdict: %v", err)
			}
			if outcome.Verdict != tc.wantVerdict {
				t.Fatalf("verdict = %s, want %s", outcome.Verdict, tc.wantVerdict)
			}
			if outcome.Findings != tc.wantFindings {
				t.Fatalf("findings = %q, want %q", outcome.Findings, tc.wantFindings)
			}
			if !outcome.Declared {
				t.Fatal("Declared is false on a parsed verdict")
			}
		})
	}
}

func TestParseReviewVerdict_TheLastDeclarationWins(t *testing.T) {
	// A reviewer quoting the charter, or quoting a previous round's
	// declaration inside its findings, must not have the quote read as this
	// round's answer.
	reply := "The charter says to end with\n\n    " + ReviewVerdictMarker + " approve\n\n" +
		"but the error handling here is wrong.\n\n" + ReviewVerdictMarker + " " + ReviewCodeRequestChanges
	outcome, err := ParseReviewVerdict(reply)
	if err != nil {
		t.Fatalf("ParseReviewVerdict: %v", err)
	}
	if outcome.Verdict != VerdictRequestChanges {
		t.Fatalf("verdict = %s; a quoted declaration was read as the answer", outcome.Verdict)
	}
	if !strings.Contains(outcome.Findings, "error handling here is wrong") {
		t.Fatalf("findings = %q", outcome.Findings)
	}
}

func TestParseReviewVerdict_RefusesEverythingThatIsNotAVerdict(t *testing.T) {
	for name, reply := range map[string]string{
		"no declaration":    "Looks good to me, ship it.",
		"empty reply":       "",
		"unknown code":      "findings\n\n" + ReviewVerdictMarker + " lgtm",
		"no findings":       ReviewVerdictMarker + " approve",
		"blank findings":    "   \n\n" + ReviewVerdictMarker + " approve",
		"prose approval":    "I approve of this change.",
		"code fence only":   "```\n" + ReviewVerdictMarker + " approve\n```",
		"no findings above": "\n\n" + ReviewVerdictMarker + " request-changes",
	} {
		t.Run(name, func(t *testing.T) {
			outcome, err := ParseReviewVerdict(reply)
			if !errors.Is(err, ErrReview) {
				t.Fatalf("err = %v, want ErrReview", err)
			}
			if outcome.Verdict != "" {
				t.Fatalf("a refused reply still carried a verdict: %+v", outcome)
			}
		})
	}
}

func TestParseReviewVerdict_ACodeFenceWithNothingAboveItIsNotAnApproval(t *testing.T) {
	// Called out separately because it is the one shape that LOOKS like a
	// well-formed approval: the declaration parses, and only the empty
	// findings refuse it.
	outcome, err := ParseReviewVerdict("```\n" + ReviewVerdictMarker + " approve\n```")
	if !errors.Is(err, ErrReview) {
		t.Fatalf("err = %v, want ErrReview", err)
	}
	if outcome.Findings != "" {
		t.Fatalf("findings = %q, want empty", outcome.Findings)
	}
}

func TestReviewCodes_TheCharterTeachesOnlyCodesTheParserKnows(t *testing.T) {
	charter := ReviewerCharter(sdlcPrompts)
	// A charter naming a code the parser does not know would be a reviewer
	// stopping for a reason nothing could act on.
	for code := range reviewCodeVerdicts {
		if !strings.Contains(charter, "`"+code+"`") {
			t.Fatalf("the charter does not teach %q", code)
		}
	}
	if !strings.Contains(charter, ReviewVerdictMarker) {
		t.Fatal("the charter does not teach the marker")
	}
	// And every code the charter teaches is one the parser reads.
	for _, code := range []string{ReviewCodeApprove, ReviewCodeRequestChanges, ReviewCodeComment} {
		if _, known := reviewCodeVerdicts[code]; !known {
			t.Fatalf("the parser does not know %q", code)
		}
	}
}

// TestReviewerCharter_SaysCIAlreadyPassedAndForbidsARerun is go-quicbaas#927's
// prompt half, asserted the way the worker charter's stop markers are: the
// dispatcher now guarantees the head is green before a reviewer is spawned, and
// a reviewer that is not TOLD that spends a model re-running the suite to find
// out. Both halves are required — "CI passed" alone still leaves running the
// tests the careful thing to do.
func TestReviewerCharter_SaysCIAlreadyPassedAndForbidsARerun(t *testing.T) {
	charter := ReviewerCharter(sdlcPrompts)
	for _, marker := range []string{ReviewerCIGreenMarker, ReviewerNoRerunMarker} {
		if !strings.Contains(charter, marker) {
			t.Fatalf("the reviewer charter does not carry %q", marker)
		}
	}
	// The markers are on the prompt SPECIFICATION too, which is what refuses a
	// tenant's edit that drops them. A marker asserted only here would be a
	// marker an operator could edit away.
	spec, ok := sdlcPrompts.Spec().Prompt("reviewer_charter")
	if !ok {
		t.Fatal("the sdlc set declares no reviewer_charter prompt")
	}
	declared := map[string]bool{}
	for _, marker := range spec.Markers {
		declared[marker.Literal] = true
	}
	for _, want := range []string{ReviewVerdictMarker, ReviewerCIGreenMarker, ReviewerNoRerunMarker,
		ReviewCodeApprove, ReviewCodeRequestChanges, ReviewCodeComment} {
		if !declared[want] {
			t.Fatalf("reviewer_charter does not declare %q as a marker, so an edit could drop it", want)
		}
	}
}

// TestReviewerCharter_RefusesABodyThatDropsTheCIInstruction proves the marker
// is load-bearing rather than decorative: the registry must reject an edited
// charter that no longer tells the reviewer the pipeline has already run.
func TestReviewerCharter_RefusesABodyThatDropsTheCIInstruction(t *testing.T) {
	spec, ok := sdlcPrompts.Spec().Prompt("reviewer_charter")
	if !ok {
		t.Fatal("the sdlc set declares no reviewer_charter prompt")
	}
	stripped := strings.ReplaceAll(spec.Default, ReviewerNoRerunMarker, "Run whatever you like")
	if stripped == spec.Default {
		t.Fatal("the shipped charter does not contain the marker, so this test asserts nothing")
	}
	if err := prompts.Validate(SetSDLC, spec, stripped); err == nil {
		t.Fatal("a charter that no longer forbids a pipeline re-run was accepted")
	}
	if err := prompts.Validate(SetSDLC, spec, spec.Default); err != nil {
		t.Fatalf("the shipped charter does not satisfy its own specification: %v", err)
	}
}

func TestReviewOutcome_SubmissionBindsToTheHead(t *testing.T) {
	outcome := ReviewOutcome{Verdict: VerdictApprove, Findings: "read it"}
	submission := outcome.Submission("head-1")
	if submission.HeadSHA != "head-1" || submission.Body != "read it" || submission.Verdict != VerdictApprove {
		t.Fatalf("submission = %+v", submission)
	}
	if err := submission.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
