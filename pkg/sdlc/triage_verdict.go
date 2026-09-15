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
	"strconv"
	"strings"
)

// =============================================================================
// The triage verdict is read from structure, never from prose
// =============================================================================
//
// This file is the same shape as turn.go's stop declaration, deliberately and
// not by coincidence: THE FACTORY DOES NOT GREP PROSE FOR A DECISION THAT
// COSTS SOMETHING. A stop declaration decides whether a task spends an
// attempt; a triage declaration decides whether an issue is handed to a
// worker at all. Both are read from a marker line and a code in a closed set,
// and a reply this binary cannot read is never given the benefit of the
// doubt.
//
// THE SAFE DIRECTION IS "NOT READY". Every way of failing to understand the
// agent — an unknown code, a duplicate with no reference, no declaration at
// all — lands on a disposition that leaves the issue for a person. The cost
// of wrongly withholding the trigger label is a human glance; the cost of
// wrongly applying it is a worker run, a review round, and an issue the
// factory churns on. Those are not the same price, and the table prices them
// accordingly.

// TriageDeclarationMarker introduces the agent's verdict. It is the whole
// machine-readable surface of the reply: everything else is for a person.
const TriageDeclarationMarker = "SDLC-TRIAGE:"

// TriageDuplicateMarker introduces the issue a duplicate verdict points at.
// It is a SECOND marker rather than a field packed into the first, so the
// verdict line stays one code from a closed set and the reference can be
// absent without making the verdict unparseable.
const TriageDuplicateMarker = "SDLC-TRIAGE-DUPLICATE:"

// TriageCloseMarker is how a not-actionable verdict says the issue is a STALE
// REPORT — the work is already on the default branch — rather than something a
// person still has to decide.
//
// IT IS A SECOND MARKER RATHER THAN A FIFTH VERDICT CODE because the verdict
// is unchanged: no worker is spawned either way, and every deployment that
// does not turn closing on behaves exactly as before. What the marker adds is
// WHICH KIND of not-actionable it was, which is the only thing that decides
// whether the issue may be closed. A not-actionable carrying no marker is
// never closed, whatever the binding allows.
const TriageCloseMarker = "SDLC-TRIAGE-CLOSE:"

// TriageCloseReasonAlreadyDone is the one value TriageCloseMarker may carry.
// Anything else reads as absent, so a model inventing a reason cannot close an
// issue by inventing one.
const TriageCloseReasonAlreadyDone = "already-done"

// The verdict codes an agent may declare. A CLOSED SET — a code outside it
// is refused rather than passed through.
const (
	// TriageCodeReady is an issue that states a verifiable objective, is
	// scoped to one pull request, depends on no other open issue, is not a
	// duplicate, and is not already done.
	TriageCodeReady = "ready"

	// TriageCodeNeedsInfo is an issue that could be worked once a named
	// thing is supplied.
	TriageCodeNeedsInfo = "needs-info"

	// TriageCodeNotActionable is an issue no worker should be spawned for:
	// already done, out of scope, or a decision reserved for a person.
	TriageCodeNotActionable = "not-actionable"

	// TriageCodeDuplicate is an issue another open issue in the same
	// project already covers. It REQUIRES a reference; see
	// TriageDuplicateMarker.
	TriageCodeDuplicate = "duplicate"
)

// TriageOutcome is what one triage reply WAS.
type TriageOutcome string

const (
	// TriageOutcomeReady is a declared ready verdict.
	TriageOutcomeReady TriageOutcome = "ready"

	// TriageOutcomeNeedsInfo is a declared needs-info verdict.
	TriageOutcomeNeedsInfo TriageOutcome = "needs_info"

	// TriageOutcomeNotActionable is a declared not-actionable verdict.
	TriageOutcomeNotActionable TriageOutcome = "not_actionable"

	// TriageOutcomeDuplicate is a declared duplicate carrying the issue it
	// duplicates.
	TriageOutcomeDuplicate TriageOutcome = "duplicate"

	// TriageOutcomeUnreadable is a declaration this binary cannot act on: a
	// code outside the vocabulary, or a duplicate naming no issue. The
	// agent stated a verdict and only the verdict is unreadable, so the
	// issue is left for a person rather than retried.
	TriageOutcomeUnreadable TriageOutcome = "unreadable"

	// TriageOutcomeUndeclared is a reply that declared nothing at all. It is
	// NOT read as any verdict, least of all ready: prose that sounds
	// approving is exactly what this file exists not to act on.
	TriageOutcomeUndeclared TriageOutcome = "undeclared"
)

// String returns the outcome's name.
func (o TriageOutcome) String() string { return string(o) }

// TriageDisposition is what the runner DOES about a verdict. There are
// exactly three, because there are exactly three things triage may do to an
// issue — and none of them is closing it.
type TriageDisposition string

const (
	// TriageDispositionReady applies the binding's trigger label, which is
	// what admits the issue to the factory.
	TriageDispositionReady TriageDisposition = "ready"

	// TriageDispositionNeedsInfo applies the needs-info label and comments
	// with what is missing.
	TriageDispositionNeedsInfo TriageDisposition = "needs_info"

	// TriageDispositionNotActionable comments with the evidence and leaves
	// the issue alone.
	TriageDispositionNotActionable TriageDisposition = "not_actionable"
)

// String returns the disposition's name.
func (d TriageDisposition) String() string { return string(d) }

// TriageDeclaration is what the agent declared, parsed from the whole reply.
type TriageDeclaration struct {
	// Code is the verdict code, lowercased. Empty when none was declared.
	Code string `json:"code,omitempty"`

	// Declared says the agent declared a verdict at all.
	Declared bool `json:"declared,omitempty"`

	// ClosesAsAlreadyDone is a not-actionable verdict that declared the work
	// is already on the default branch. It is advisory: the binding decides
	// whether it is acted on (ProjectBinding.TriageMayClose).
	ClosesAsAlreadyDone bool `json:"closes_as_already_done,omitempty"`

	// DuplicateOf is the issue a duplicate verdict points at, zero when the
	// agent named none or named one that does not read as a positive issue
	// number.
	DuplicateOf int `json:"duplicate_of,omitempty"`
}

// TriageVerdict is what one classified reply means: what it was, what the
// runner does about it, which label key it moves the issue onto, whether it
// comments, and the sentence a person reads above the agent's own words.
type TriageVerdict struct {
	// Outcome is what the reply was.
	Outcome TriageOutcome

	// Disposition is what the runner does about it.
	Disposition TriageDisposition

	// LabelKey is the binding label key the issue is moved onto, empty for a
	// disposition that writes no label.
	LabelKey string

	// Trigger says the disposition applies the binding's TRIGGER label,
	// which is not one of the display label keys and is the only way an
	// issue is ever admitted to the factory.
	Trigger bool

	// Comments says the disposition writes a comment.
	Comments bool

	// ClosesAsAlreadyDone is carried through so the runner can decide,
	// against the binding, whether to close. See TriageCloseMarker.
	ClosesAsAlreadyDone bool

	// DuplicateOf is carried through from the declaration so the comment can
	// name the issue without re-parsing the reply.
	DuplicateOf int

	// Headline is the sentence that introduces the agent's text wherever the
	// outcome is reported.
	Headline string
}

// triageCodeOutcomes is the vocabulary: the code an agent declares, and what
// that reply was. Map-based so a code answers in constant time and the set is
// data a completeness test can walk.
var triageCodeOutcomes = map[string]TriageOutcome{
	TriageCodeReady:         TriageOutcomeReady,
	TriageCodeNeedsInfo:     TriageOutcomeNeedsInfo,
	TriageCodeNotActionable: TriageOutcomeNotActionable,
	TriageCodeDuplicate:     TriageOutcomeDuplicate,
}

// triageVerdicts is the classification table: one row per outcome, keyed for
// constant-time lookup. A test walks it to prove every outcome has a row, and
// a second proves NO row other than ready reaches the trigger label.
var triageVerdicts = map[TriageOutcome]TriageVerdict{
	TriageOutcomeReady: {
		Outcome:     TriageOutcomeReady,
		Disposition: TriageDispositionReady,
		Trigger:     true,
		Headline:    "Triage found this ready to work.",
	},
	TriageOutcomeNeedsInfo: {
		Outcome:     TriageOutcomeNeedsInfo,
		Disposition: TriageDispositionNeedsInfo,
		LabelKey:    LabelNeedsInfo,
		Comments:    true,
		Headline:    "Triage cannot hand this to a worker yet. What is missing:",
	},
	TriageOutcomeNotActionable: {
		Outcome:     TriageOutcomeNotActionable,
		Disposition: TriageDispositionNotActionable,
		Comments:    true,
		Headline:    "Triage found no work to do here. The evidence:",
	},
	TriageOutcomeDuplicate: {
		Outcome:     TriageOutcomeDuplicate,
		Disposition: TriageDispositionNotActionable,
		Comments:    true,
		Headline:    "Triage found this already covered by another open issue.",
	},
	TriageOutcomeUnreadable: {
		Outcome:     TriageOutcomeUnreadable,
		Disposition: TriageDispositionNotActionable,
		Comments:    true,
		Headline: "Triage declared a verdict this binary cannot act on, so the issue " +
			"is left for a person.",
	},
	TriageOutcomeUndeclared: {
		Outcome:     TriageOutcomeUndeclared,
		Disposition: TriageDispositionNotActionable,
		Comments:    true,
		Headline: "Triage ended without a verdict declaration, so nothing could be " +
			"concluded about this issue automatically.",
	},
}

// ParseTriageDeclaration reads the agent's verdict out of a reply.
//
// THE LAST DECLARATION OF EACH MARKER WINS, and each is found rather than
// assumed to be the final line — the same rule, for the same reason, as
// ParseTurnStop: a model that adds a trailing blank line, a closing code
// fence or a signature after its declaration has still declared it, and a
// reply that quotes the marker while thinking aloud and then declares for
// real is read on its real declaration.
func ParseTriageDeclaration(reply string) TriageDeclaration {
	var decl TriageDeclaration
	lines := strings.Split(reply, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := triageDeclarationLine(lines[i])
		switch {
		case !decl.Declared && strings.HasPrefix(line, TriageDeclarationMarker):
			decl.Code = strings.ToLower(triageMarkerValue(line, TriageDeclarationMarker))
			decl.Declared = true
		case decl.DuplicateOf == 0 && strings.HasPrefix(line, TriageDuplicateMarker):
			decl.DuplicateOf = parseTriageIssueRef(triageMarkerValue(line, TriageDuplicateMarker))
		case !decl.ClosesAsAlreadyDone && strings.HasPrefix(line, TriageCloseMarker):
			decl.ClosesAsAlreadyDone = strings.EqualFold(
				triageMarkerValue(line, TriageCloseMarker), TriageCloseReasonAlreadyDone)
		}
		if decl.Declared && decl.DuplicateOf != 0 && decl.ClosesAsAlreadyDone {
			break
		}
	}
	return decl
}

// triageDeclarationLine strips the decoration a model puts around a
// declaration — quoting, emphasis, code fencing — leaving the marker at the
// start of the line where the prefix test can find it.
//
// THE DUPLICATE MARKER IS TESTED FIRST WHEREVER BOTH PREFIXES MATCH, which is
// why callers must compare against it before the shorter one: "SDLC-TRIAGE-"
// does not share a prefix with "SDLC-TRIAGE:", so the two are disjoint and
// the order is free. Stated because a marker pair that DID share a prefix
// would silently read every duplicate line as a verdict line.
func triageDeclarationLine(raw string) string {
	line := strings.TrimSpace(raw)
	line = strings.TrimPrefix(line, "> ")
	return strings.TrimSpace(strings.Trim(line, "*`"))
}

// triageMarkerValue is the text after a marker, stripped of the punctuation a
// model wraps it in.
func triageMarkerValue(line, marker string) string {
	value := strings.TrimSpace(strings.TrimPrefix(line, marker))
	return strings.Trim(value, "`'\"*. ")
}

// parseTriageIssueRef reads an issue number an agent wrote as "#25" or "25".
// Anything that is not a positive integer reads as ABSENT rather than as
// issue zero: a duplicate pointing nowhere must not render a comment
// referring a person to "#0".
func parseTriageIssueRef(value string) int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "#")
	number, err := strconv.Atoi(trimmed)
	if err != nil || number <= 0 {
		return 0
	}
	return number
}

// ClassifyTriage decides what a reply was and what the runner does about it.
//
// ONE TABLE LOOKUP, TWICE: the declared code names the outcome, and the
// outcome names the verdict. There is no path that reads the prose.
func ClassifyTriage(decl TriageDeclaration) TriageVerdict {
	verdict := triageVerdicts[classifyTriageOutcome(decl)]
	verdict.DuplicateOf = decl.DuplicateOf
	verdict.ClosesAsAlreadyDone = decl.ClosesAsAlreadyDone
	return verdict
}

// classifyTriageOutcome is the outcome half of ClassifyTriage.
func classifyTriageOutcome(decl TriageDeclaration) TriageOutcome {
	if !decl.Declared {
		return TriageOutcomeUndeclared
	}
	outcome, known := triageCodeOutcomes[decl.Code]
	if !known {
		return TriageOutcomeUnreadable
	}
	// A DUPLICATE THAT NAMES NO ISSUE IS UNREADABLE. The whole value of the
	// verdict is telling a person which issue to look at instead; without
	// that it is a not-actionable with no evidence, and the six identical
	// go-truststrap issues are exactly the case where "which one" is the
	// answer somebody needs.
	if outcome == TriageOutcomeDuplicate && decl.DuplicateOf <= 0 {
		return TriageOutcomeUnreadable
	}
	return outcome
}

// TriageComment renders what a person reads on the issue: the verdict's
// headline, the issue a duplicate points at, and the agent's own evidence.
//
// A verdict that writes no comment renders none — the ready path speaks
// through its label, and a comment on every ready issue would be noise on
// the one verdict that is already visible.
func TriageComment(verdict TriageVerdict, evidence string) string {
	if !verdict.Comments {
		return ""
	}
	var b strings.Builder
	b.WriteString(verdict.Headline)
	if verdict.Outcome == TriageOutcomeDuplicate && verdict.DuplicateOf > 0 {
		b.WriteString(" It duplicates #" + strconv.Itoa(verdict.DuplicateOf) + ".")
	}
	b.WriteString("\n")
	if body := strings.TrimSpace(evidence); body != "" {
		b.WriteString("\n" + body + "\n")
	}
	return b.String()
}
