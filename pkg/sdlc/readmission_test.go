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

package sdlc

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Encoding
// =============================================================================

// TestUnblockCondition_RoundTripsEveryKind fails when a kind is added
// without an encoding fixture: a kind whose round trip nothing checks is a
// kind whose stored form nothing checks.
func TestUnblockCondition_RoundTripsEveryKind(t *testing.T) {
	conditions := []UnblockCondition{
		{Kind: UnblockPipelineGreen, Subject: "deadbeefcafe"},
		{Kind: UnblockForgeIssueState, Subject: "acme/api#41", Target: "closed"},
		{Kind: UnblockReleaseTag, Subject: "acme/api", Target: "v1.2.3"},
		{Kind: UnblockOwnerRuling},
		{Kind: UnblockOperatorRepair},
	}
	if len(conditions) != len(unblockKinds) {
		t.Fatalf("%d kinds are defined but %d are round-tripped here", len(unblockKinds), len(conditions))
	}
	for _, want := range conditions {
		reason, err := ComposeBlockedReason(want, "some detail")
		if err != nil {
			t.Fatalf("ComposeBlockedReason(%s): %v", want.Kind, err)
		}
		if !strings.HasPrefix(reason, want.Describe()+": some detail ") {
			t.Fatalf("reason %q does not lead with the description", reason)
		}
		got, err := ParseUnblockCondition(reason)
		if err != nil || got != want {
			t.Fatalf("round trip of %+v gave %+v, %v", want, got, err)
		}
		if want.Kind.MachineCheckable() == (want.Kind.Awaiting() != "") {
			t.Fatalf("kind %s is both machine-checkable and awaited, or neither", want.Kind)
		}
		if !want.Kind.Valid() {
			t.Fatalf("defined kind %s reports itself invalid", want.Kind)
		}
	}
	reason, err := ComposeBlockedReason(UnblockCondition{Kind: UnblockOwnerRuling}, "")
	if err != nil || strings.Contains(reason, ": ") {
		t.Fatalf("empty detail: %q, %v", reason, err)
	}
}

// TestParseUnblockCondition_FindsMarkerWrappedInProse is the fixture an
// end-anchored parser gets WRONG.
//
// A park reason does not reach the sweep as this package composed it: the
// engine appends its own sentence when the attempt budget runs out
// (orchestration applyDeath), so a marker arrives with text on BOTH sides. A
// parser that required it at the end would report the factory's own death
// evidence as carrying no condition — silently, and on exactly the tasks
// that most need triage.
func TestParseUnblockCondition_FindsMarkerWrappedInProse(t *testing.T) {
	want := UnblockCondition{Kind: UnblockOperatorRepair}
	composed, err := ComposeBlockedReason(want, "the worker died mid-implementation")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}
	stored := composed + "\n\nThe attempt budget is spent; readmit the task to try again."
	if strings.HasSuffix(stored, unblockMarkerClose) {
		t.Fatalf("the fixture does not wrap the marker, so it cannot distinguish an "+
			"end-anchored parser from a searching one: %q", stored)
	}
	got, err := ParseUnblockCondition(stored)
	if err != nil || got != want {
		t.Fatalf("ParseUnblockCondition(%q) = %+v, %v", stored, got, err)
	}
}

// TestUnblockCondition_RefusesReservedCharactersInValues proves the refusal
// is what makes parsing unambiguous rather than a tidiness rule.
//
// The second half is the part that matters: it hand-builds the marker the
// refused condition WOULD have produced and shows the parser reads it as a
// different, malformed thing. Without the encode-time refusal that text
// would be stored, and the task would carry a condition nobody wrote.
func TestUnblockCondition_RefusesReservedCharactersInValues(t *testing.T) {
	smuggled := UnblockCondition{Kind: UnblockPipelineGreen, Subject: "cafe;target=closed"}
	if _, err := smuggled.Encode(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Encode accepted a subject carrying a field separator: %v", err)
	}
	forged := unblockMarkerOpen + "kind=" + string(UnblockPipelineGreen) +
		";subject=" + smuggled.Subject + unblockMarkerClose
	got, err := ParseUnblockCondition(forged)
	if err == nil {
		t.Fatalf("the forged marker parsed cleanly as %+v, so the encode-time refusal guards nothing", got)
	}
	if !errors.Is(err, ErrUnblockConditionMalformed) {
		t.Fatalf("got %v, want a malformed-condition error", err)
	}
}

func TestUnblockCondition_Refusals(t *testing.T) {
	cases := map[string]struct {
		cond UnblockCondition
		want error
	}{
		"unknown kind":          {UnblockCondition{Kind: "nope"}, ErrUnblockKindUnknown},
		"missing subject":       {UnblockCondition{Kind: UnblockPipelineGreen}, ErrInvalidInput},
		"forbidden target":      {UnblockCondition{Kind: UnblockPipelineGreen, Subject: "s", Target: "t"}, ErrInvalidInput},
		"forbidden subject":     {UnblockCondition{Kind: UnblockOwnerRuling, Subject: "s"}, ErrInvalidInput},
		"reserved chars":        {UnblockCondition{Kind: UnblockPipelineGreen, Subject: "a;b"}, ErrInvalidInput},
		"issue with no target":  {UnblockCondition{Kind: UnblockForgeIssueState, Subject: "acme/api#1"}, ErrInvalidInput},
		"issue with no subject": {UnblockCondition{Kind: UnblockForgeIssueState, Target: "closed"}, ErrInvalidInput},
		"issue state the forge never reports": {
			UnblockCondition{Kind: UnblockForgeIssueState, Subject: "acme/api#1", Target: "nearly_done"}, ErrInvalidInput,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.cond.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate = %v, want %v", err, tc.want)
			}
			if _, err := tc.cond.Encode(); !errors.Is(err, tc.want) {
				t.Fatalf("Encode = %v, want %v", err, tc.want)
			}
		})
	}
	if got := (UnblockCondition{Kind: "nope"}).Describe(); !strings.Contains(got, "nope") {
		t.Fatalf("Describe of an unknown kind does not name it: %q", got)
	}
	if _, err := ComposeBlockedReason(UnblockCondition{Kind: UnblockOwnerRuling}, "x unblock[kind=owner_ruling]"); !errors.Is(err, ErrUnblockConditionMalformed) {
		t.Fatalf("marker injection accepted: %v", err)
	}
	if _, err := ComposeBlockedReason(UnblockCondition{Kind: "nope"}, "detail"); !errors.Is(err, ErrUnblockKindUnknown) {
		t.Fatalf("a reason was composed for a kind nothing defines: %v", err)
	}

	// A subject that is well formed as text and names nothing ENCODES. The
	// check reports it on every pass; refusing it here would move a dangling
	// reference's discovery from "undecidable, visibly" to "never mentioned".
	if _, err := (UnblockCondition{Kind: UnblockForgeIssueState, Subject: "41", Target: "closed"}).Encode(); err != nil {
		t.Fatalf("a bare issue number was refused at encode time: %v", err)
	}
}

func TestParseUnblockCondition_Refusals(t *testing.T) {
	cases := map[string]error{
		"no marker at all":                                                                               ErrUnblockConditionAbsent,
		"unclosed unblock[kind=owner_ruling":                                                             ErrUnblockConditionMalformed,
		"bad field unblock[kind=owner_ruling;x]":                                                         ErrUnblockConditionMalformed,
		"dup unblock[kind=owner_ruling;kind=owner_ruling]":                                               ErrUnblockConditionMalformed,
		"unknown field unblock[kind=owner_ruling;who=me]":                                                ErrUnblockConditionMalformed,
		"unknown kind unblock[kind=nope]":                                                                ErrUnblockKindUnknown,
		"unusable unblock[kind=pipeline_green]":                                                          ErrUnblockConditionMalformed,
		"field the kind does not read unblock[kind=owner_ruling;subject=7]":                              ErrUnblockConditionMalformed,
		"state the forge never reports unblock[kind=forge_issue_state;subject=acme/api#7;target=nearly]": ErrUnblockConditionMalformed,
	}
	for reason, want := range cases {
		if _, err := ParseUnblockCondition(reason); !errors.Is(err, want) {
			t.Fatalf("%q: err=%v, want %v", reason, err, want)
		}
	}
	var condErr *UnblockConditionError
	_, err := ParseUnblockCondition("nothing")
	if !errors.As(err, &condErr) || condErr.Error() == "" {
		t.Fatalf("typed error: %v", err)
	}
	var kindErr *UnblockKindError
	if _, err := ParseUnblockCondition("unblock[kind=nope]"); !errors.As(err, &kindErr) || !strings.Contains(kindErr.Error(), "nope") {
		t.Fatalf("kind error: %v", err)
	}
}

// TestParseBlockedReason_SplitsTheDetailFromTheCondition covers the surface
// the forge comment renders from.
func TestParseBlockedReason_SplitsTheDetailFromTheCondition(t *testing.T) {
	want := UnblockCondition{Kind: UnblockForgeIssueState, Subject: "acme/api#41", Target: "closed"}
	reason, err := ComposeBlockedReason(want, "the SDK route it needs lands there")
	if err != nil {
		t.Fatalf("ComposeBlockedReason: %v", err)
	}
	got, detail, err := ParseBlockedReason(reason)
	if err != nil || got != want {
		t.Fatalf("ParseBlockedReason = %+v, %v", got, err)
	}
	if detail != "the SDK route it needs lands there" {
		t.Fatalf("detail = %q; the condition's own sentence and the marker must both be gone", detail)
	}
	if strings.Contains(detail, unblockMarkerOpen) {
		t.Fatalf("the marker reached a reader: %q", detail)
	}

	// A reason nothing here composed yields its whole text and says so.
	prose := "the runner could not reach the forge"
	condition, whole, err := ParseBlockedReason(prose)
	if !errors.Is(err, ErrUnblockConditionAbsent) {
		t.Fatalf("a prose reason reported %v, want an absent condition", err)
	}
	if whole != prose || condition.Kind != "" {
		t.Fatalf("prose reason = %+v, %q", condition, whole)
	}
}

// =============================================================================
// The kind registry
// =============================================================================

// TestUnblockKinds_EveryKindHasExactlyOneDecider is the completeness test,
// and it is written so that it CAN fail.
//
// The trap it avoids: a completeness test that walks the set and asserts
// each member "is handled" passes forever when the handling has a permissive
// default. Here the property is an exclusive OR — a kind is decided by a
// check or by a named person, never both and never neither — so a kind added
// without a decider fails this test, and Validate refuses it at the door on
// top.
func TestUnblockKinds_EveryKindHasExactlyOneDecider(t *testing.T) {
	for kind, spec := range unblockKinds {
		hasCheck := spec.check != nil
		hasPerson := spec.awaiting != ""
		if hasCheck == hasPerson {
			t.Errorf("kind %s has check=%v and awaiting=%q: exactly one must decide it, because "+
				"neither means nothing will ever evaluate it and both means nobody knows which did",
				kind, hasCheck, spec.awaiting)
		}
		if spec.describe == nil {
			t.Errorf("kind %s renders no description, so a person meeting it sees a bare identifier", kind)
		}
		if kind.MachineCheckable() != hasCheck {
			t.Errorf("kind %s reports MachineCheckable=%v with check=%v", kind, kind.MachineCheckable(), hasCheck)
		}
		if !kind.Valid() {
			t.Errorf("defined kind %s reports itself invalid", kind)
		}
	}
	if UnblockKind("the_stars_align").Valid() || UnblockKind("the_stars_align").MachineCheckable() {
		t.Error("an undefined kind reported itself valid or checkable")
	}
	if UnblockKind("the_stars_align").Awaiting() != "" {
		t.Error("an undefined kind named a party being waited on")
	}
}

// TestUnblockCondition_UndecidedKindRefusedAtValidation exercises the
// refusing default with a kind that exists and has no decider.
//
// It installs one for the duration of the test rather than waiting for
// somebody to add a real one, because the whole value of the refusing
// default is what it does to a kind nobody has finished writing.
func TestUnblockCondition_UndecidedKindRefusedAtValidation(t *testing.T) {
	const undecided UnblockKind = "test_undecided"
	unblockKinds[undecided] = unblockKindSpec{describe: func(UnblockCondition) string { return "a kind nobody finished" }}
	t.Cleanup(func() { delete(unblockKinds, undecided) })

	err := UnblockCondition{Kind: undecided}.Validate()
	if !errors.Is(err, ErrUnblockKindUnhandled) {
		t.Fatalf("a kind with neither a check nor a person was accepted: %v", err)
	}
	var kindErr *UnblockKindError
	if !errors.As(err, &kindErr) || !strings.Contains(kindErr.Error(), "nothing decides it") {
		t.Fatalf("an unhandled kind renders as %v, which reads like a bad record", err)
	}
}

// TestUnblockKindError_DistinguishesUnknownFromUnhandled keeps a bad record
// apart from an unfinished kind: both leave the task blocked and they summon
// different people.
func TestUnblockKindError_DistinguishesUnknownFromUnhandled(t *testing.T) {
	unknown := &UnblockKindError{Kind: "k", Sentinel: ErrUnblockKindUnknown}
	unhandled := &UnblockKindError{Kind: "k", Sentinel: ErrUnblockKindUnhandled}
	if unknown.Error() == unhandled.Error() {
		t.Fatalf("both render as %q, so a reader cannot tell bad data from a gap in this package", unknown.Error())
	}
	if !errors.Is(unknown, ErrUnblockKindUnknown) || !errors.Is(unhandled, ErrUnblockKindUnhandled) {
		t.Fatal("the errors do not match their sentinels")
	}
}

// TestUnblockConditionError_NamesTheTaskWhenItKnowsIt guards against an
// error inventing a record id it was never given.
func TestUnblockConditionError_NamesTheTaskWhenItKnowsIt(t *testing.T) {
	anonymous := (&UnblockConditionError{Reason: "text", Detail: "detail", Sentinel: ErrUnblockConditionAbsent}).Error()
	if strings.Contains(anonymous, "task ") {
		t.Fatalf("a parse error with no task id invented one: %q", anonymous)
	}
	named := (&UnblockConditionError{TaskID: "t-17", Reason: "text", Detail: "detail", Sentinel: ErrUnblockConditionAbsent}).Error()
	if !strings.Contains(named, "task t-17") {
		t.Fatalf("an error carrying a task id does not render it: %q", named)
	}
}

func TestIssueContract_RendersIssueAndDiscussion(t *testing.T) {
	when := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	contract, err := IssueContract(IssueContractSpec{
		Repo: testRepoRef, IssueNumber: 25, Title: "  Add the thing  ", Body: "Do it well.",
		Comments: []ForgeComment{{Body: "first", Author: "alice", Created: when}, {Body: "   "}, {Body: "second"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Repository: acme/api\n", "Issue: #25\n", "## Add the thing\n", "Do it well.\n",
		"### Comment 1 — @alice, 2026-09-05T12:00:00Z\n", "first\n", "### Comment 2\n", "second\n",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("contract lacks %q:\n%s", want, contract)
		}
	}
	if strings.Contains(contract, "Comment 3") {
		t.Fatal("a blank comment was rendered")
	}
	none, err := IssueContract(IssueContractSpec{Repo: testRepoRef, IssueNumber: 1, Title: "t"})
	if err != nil || !strings.Contains(none, "(none)") {
		t.Fatalf("no discussion: %q, %v", none, err)
	}
}

func TestIssueContract_Refusals(t *testing.T) {
	for name, spec := range map[string]IssueContractSpec{
		"repo":   {IssueNumber: 1, Title: "t"},
		"number": {Repo: testRepoRef, Title: "t"},
		"title":  {Repo: testRepoRef, IssueNumber: 1, Title: " "},
	} {
		if _, err := IssueContract(spec); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if !strings.Contains(WorkerCharter(), "Software Factory") {
		t.Fatal("the charter must name the Software Factory")
	}
}

func TestIssueRef_RoundTrip(t *testing.T) {
	ref := IssueRefString(testRepoRef, 25)
	if ref != "acme/api#25" {
		t.Fatalf("ref = %q", ref)
	}
	repo, n, err := ParseIssueRef(ref)
	if err != nil || repo != testRepoRef || n != 25 {
		t.Fatalf("parse = %v %d %v", repo, n, err)
	}
	for _, bad := range []string{"", "acme/api", "acme#25", "/api#25", "acme/api#0", "acme/api#x"} {
		if _, _, err := ParseIssueRef(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%q accepted: %v", bad, err)
		}
	}
	task := testTaskRecord(25)
	if _, n, err := issueOfTask(task); err != nil || n != 25 {
		t.Fatalf("issueOfTask = %d, %v", n, err)
	}
	task.Source.Kind = "operator"
	if _, _, err := issueOfTask(task); !errors.Is(err, ErrTaskNotForgeIssue) {
		t.Fatalf("operator task accepted: %v", err)
	}
	if _, _, err := issueOfTask(nil); !errors.Is(err, ErrTaskNotForgeIssue) {
		t.Fatalf("nil task: %v", err)
	}
	var typed *TaskNotForgeIssueError
	task.Source = nil
	if _, _, err := issueOfTask(task); !errors.As(err, &typed) || typed.TaskID != testTask {
		t.Fatalf("sourceless task: %v", err)
	}
}
