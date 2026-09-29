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
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cifault"
)

func gatePolicy() ReviewPolicy {
	return ReviewPolicy{RequiredApprovals: 1, RequireIndependentReviewer: true, RequireGreenChecks: true}
}

func approve(reviewer, family, head string, round int) ReviewRecord {
	return ReviewRecord{Reviewer: reviewer, ModelFamily: family, Verdict: VerdictApprove, HeadSHA: head, Round: round, SubmittedAt: time.Unix(int64(round), 0)}
}

func green(name, head string) CheckRecord {
	return CheckRecord{Name: name, HeadSHA: head, Status: CheckSuccess, FinishedAt: time.Unix(1, 0)}
}

func TestEvaluateReviewGate_EveryRefusalInOrder(t *testing.T) {
	cases := map[string]struct {
		in   GateInput
		want GateBlockReason
	}{
		"policy invalid": {GateInput{Policy: ReviewPolicy{}, PullRequestState: PullRequestOpen, HeadSHA: "h"}, GateBlockedPolicyInvalid},
		"not open":       {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestClosed, HeadSHA: "h"}, GateBlockedPullRequestNotOpen},
		"head unknown":   {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen}, GateBlockedHeadUnknown},
		"errored": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h",
			Reviews: []ReviewRecord{{Reviewer: "codex", Verdict: VerdictError, HeadSHA: "h"}, approve("alice", ModelFamilyOpenAI, "h", 1)}}, GateBlockedReviewErrored},
		"changes requested": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{{Reviewer: "codex", ModelFamily: ModelFamilyOpenAI, Verdict: VerdictRequestChanges, HeadSHA: "h"}}}, GateBlockedChangesRequested},
		"author unknown": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h",
			Reviews: []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)}}, GateBlockedAuthorUnknown},
		"not independent": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("claude-bot", ModelFamilyAnthropic, "h", 1)}}, GateBlockedReviewerNotIndependent},
		// A pooled worker may have been any of its Agents, so a reviewer from
		// ANY of their families is not independent.
		"not independent of a pooled author": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h",
			AuthorModelFamilies: []string{ModelFamilyAnthropic, ModelFamilyOpenAI},
			Reviews:             []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)}}, GateBlockedReviewerNotIndependent},
		// One pool member nobody can name makes the author unknown.
		"a pooled author with an unknown member": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h",
			AuthorModelFamilies: []string{ModelFamilyAnthropic, ""},
			Reviews:             []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)}}, GateBlockedAuthorUnknown},
		"unrecorded family is not independent": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("stranger", "", "h", 1)}}, GateBlockedReviewerNotIndependent},
		"insufficient": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic}}, GateBlockedInsufficientApprovals},
		"stale head ignored": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h2", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h1", 1)}}, GateBlockedInsufficientApprovals},
		// A settled red outranks a missing approval: no reviewer is
		// dispatched to a red head, so reporting the approval first would
		// wait for a review that cannot come (go-quicbaas PR #971).
		"red pipeline before missing approvals": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Checks: []CheckRecord{green("build", "h"), {Name: "test", HeadSHA: "h", Status: CheckFailure}}}, GateBlockedChecksFailed},
		"unfinished pipeline waits behind missing approvals": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Checks: []CheckRecord{{Name: "ci", HeadSHA: "h", Status: CheckPending}}}, GateBlockedInsufficientApprovals},
		"no checks": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)}}, GateBlockedNoChecks},
		"checks incomplete": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)},
			Checks:  []CheckRecord{{Name: "ci", HeadSHA: "h", Status: CheckPending}}}, GateBlockedChecksIncomplete},
		"checks failed": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)},
			Checks:  []CheckRecord{green("build", "h"), {Name: "test", HeadSHA: "h", Status: CheckFailure}}}, GateBlockedChecksFailed},
		"checks on another head do not count": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)},
			Checks:  []CheckRecord{green("ci", "old")}}, GateBlockedNoChecks},
		"clear": {GateInput{Policy: gatePolicy(), PullRequestState: PullRequestOpen, HeadSHA: "h", AuthorModelFamilies: []string{ModelFamilyAnthropic},
			Reviews: []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h", 1)},
			Checks:  []CheckRecord{green("ci", "h")}}, GateClear},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := EvaluateReviewGate(tc.in)
			if got.Reason != tc.want {
				t.Fatalf("reason = %s (%s), want %s", got.Reason, got.Detail, tc.want)
			}
			if got.Mergeable() != (tc.want == GateClear) {
				t.Fatal("Mergeable disagrees with the reason")
			}
			if tc.want == GateClear && got.Approvals != 1 {
				t.Fatalf("approvals = %d", got.Approvals)
			}
		})
	}
}

func TestEvaluateReviewGate_WithoutIndependenceOrChecks(t *testing.T) {
	policy := ReviewPolicy{RequiredApprovals: 2}
	in := GateInput{Policy: policy, PullRequestState: PullRequestOpen, HeadSHA: "h",
		Reviews: []ReviewRecord{approve("a", "", "h", 1), approve("b", "", "h", 1)}}
	if got := EvaluateReviewGate(in); !got.Mergeable() || got.Approvals != 2 {
		t.Fatalf("two plain approvals: %+v", got)
	}
	in.Reviews = in.Reviews[:1]
	if got := EvaluateReviewGate(in); got.Reason != GateBlockedInsufficientApprovals || !strings.Contains(got.Detail, "1 of 2") {
		t.Fatalf("one of two: %+v", got)
	}
}

func TestGateBlockReason_Pending(t *testing.T) {
	for reason, want := range map[GateBlockReason]bool{
		GateBlockedInsufficientApprovals: true, GateBlockedReviewerNotIndependent: true, GateBlockedNoChecks: true,
		GateBlockedChecksIncomplete: true, GateBlockedReviewErrored: true,
		GateBlockedChangesRequested: false, GateBlockedChecksFailed: false, GateBlockedPolicyInvalid: false, GateClear: false,
	} {
		if reason.Pending() != want {
			t.Fatalf("%s.Pending() = %v", reason, reason.Pending())
		}
	}
	if GateClear.String() != "clear" || !GateClear.Clear() {
		t.Fatal("GateClear")
	}
}

func TestLatestVerdictsForHead_SupersedesByRoundThenTimeThenSeverity(t *testing.T) {
	reviews := []ReviewRecord{
		{Reviewer: "codex", Verdict: VerdictError, HeadSHA: "h", Round: 1, SubmittedAt: time.Unix(1, 0)},
		{Reviewer: "codex", Verdict: VerdictApprove, HeadSHA: "h", Round: 1, SubmittedAt: time.Unix(2, 0)},
		{Reviewer: "alice", Verdict: VerdictApprove, HeadSHA: "h", Round: 1, SubmittedAt: time.Unix(5, 0)},
		{Reviewer: "alice", Verdict: VerdictRequestChanges, HeadSHA: "h", Round: 2, SubmittedAt: time.Unix(3, 0)},
		{Reviewer: "bob", Verdict: VerdictApprove, HeadSHA: "h", Round: 1, SubmittedAt: time.Unix(4, 0)},
		{Reviewer: "bob", Verdict: VerdictRequestChanges, HeadSHA: "h", Round: 1, SubmittedAt: time.Unix(4, 0)},
		{Reviewer: "old", Verdict: VerdictApprove, HeadSHA: "stale", Round: 9},
	}
	got := latestVerdictsForHead(reviews, "h")
	if len(got) != 3 || got[0].Reviewer != "alice" || got[1].Reviewer != "bob" || got[2].Reviewer != "codex" {
		t.Fatalf("verdicts = %+v", got)
	}
	if got[0].Verdict != VerdictRequestChanges {
		t.Fatal("a later round did not supersede an earlier one")
	}
	if got[1].Verdict != VerdictRequestChanges {
		t.Fatal("a same-tick tie did not resolve to the harder verdict")
	}
	if got[2].Verdict != VerdictApprove {
		t.Fatal("a later retry did not supersede the errored review")
	}
	if verdictSeverity("mystery") != 1 {
		t.Fatal("unknown verdict severity")
	}
	findings := findingsFor(reviews, "h")
	if !strings.Contains(findings, "Review by alice requested changes") || !strings.Contains(findings, "Review by bob") || strings.Contains(findings, "codex") {
		t.Fatalf("findings = %q", findings)
	}
	if !strings.Contains(findings, "no findings were written") {
		t.Fatal("empty findings not called out")
	}
}

func TestForgeRecordConversion(t *testing.T) {
	policy := ReviewPolicy{Reviewers: map[string]string{"codex": ModelFamilyOpenAI}}
	when := time.Date(2026, 9, 5, 1, 2, 3, 0, time.UTC)
	rec, ok := reviewRecordFor(ForgeReview{ID: 7, Reviewer: "codex", State: "approved", Body: "lgtm", HeadSHA: "h", Submitted: when}, policy, 2)
	if !ok || rec.Verdict != VerdictApprove || rec.ExternalID != "7" || rec.ModelFamily != ModelFamilyOpenAI || rec.Round != 2 || !rec.SubmittedAt.Equal(when) {
		t.Fatalf("record = %+v ok=%v", rec, ok)
	}
	if _, ok := reviewRecordFor(ForgeReview{Reviewer: "x", State: "COMMENT", HeadSHA: "h"}, policy, 1); ok {
		t.Fatal("a comment carried a verdict")
	}
	if _, ok := reviewRecordFor(ForgeReview{Reviewer: "", State: "APPROVED", HeadSHA: "h"}, policy, 1); ok {
		t.Fatal("an anonymous review carried a verdict")
	}
	if rec, ok := reviewRecordFor(ForgeReview{Reviewer: "x", State: "request_changes", HeadSHA: "h"}, policy, 1); !ok || rec.Verdict != VerdictRequestChanges || rec.ExternalID != "" {
		t.Fatalf("request_changes: %+v %v", rec, ok)
	}

	checks := []ForgeCheckRun{
		{ID: "1", Name: "ci", HeadSHA: "h", Status: "completed", Conclusion: "failure", Completed: when},
		{ID: "2", Name: "ci", HeadSHA: "h", Status: "completed", Conclusion: "success", Completed: when.Add(time.Minute)},
		{ID: "x", Name: "lint", HeadSHA: "h", Status: "in_progress"},
		{ID: "", Name: "", HeadSHA: "h"},
	}
	records := checkRecordsFor(checks, "h", when.Add(time.Hour))
	if len(records) != 2 || records[0].Name != "ci" || records[0].Status != CheckSuccess || records[0].ForgeRunID != "2" || !records[0].FinishedAt.Equal(when.Add(time.Minute)) {
		t.Fatalf("records = %+v", records)
	}
	if records[1].Name != "lint" || records[1].Status != CheckPending || !records[1].FinishedAt.IsZero() {
		t.Fatalf("pending record = %+v", records[1])
	}
	completedNoTime := checkRecordsFor([]ForgeCheckRun{{ID: "3", Name: "ci", Status: "completed", Conclusion: "cancelled"}}, "h", when)
	if completedNoTime[0].Status != CheckCancelled || !completedNoTime[0].FinishedAt.Equal(when) {
		t.Fatalf("cancelled without a time: %+v", completedNoTime[0])
	}
	if !checkSupersedes(ForgeCheckRun{ID: "b", Completed: when.Add(time.Second)}, ForgeCheckRun{ID: "a", Completed: when}) {
		t.Fatal("non-numeric ids must fall back to completion time")
	}
	if !checkSupersedes(ForgeCheckRun{ID: "a", Started: when.Add(time.Second)}, ForgeCheckRun{ID: "b", Started: when}) {
		t.Fatal("equal completion must fall back to start time")
	}
	if pullRequestStateFor(&ForgePullRequest{Merged: true}) != PullRequestMerged || pullRequestStateFor(&ForgePullRequest{State: "closed"}) != PullRequestClosed || pullRequestStateFor(&ForgePullRequest{State: "open"}) != PullRequestOpen {
		t.Fatal("pullRequestStateFor")
	}
	if !sameChecks(records, records) || sameChecks(records, records[:1]) || sameChecks(records, completedNoTime) {
		t.Fatal("sameChecks")
	}
}

func TestEvaluateChecksFailure_SettlesOnlyOnAStandingFailure(t *testing.T) {
	now := time.Unix(10_000, 0)
	window := 5 * time.Minute
	failed := CheckRecord{Name: "test", HeadSHA: "h", Status: CheckFailure, FinishedAt: now.Add(-window - time.Second)}
	ok := green("build", "h")
	if _, settled := evaluateChecksFailure([]CheckRecord{ok, failed}, "", now, window); settled {
		t.Fatal("no head")
	}
	if _, settled := evaluateChecksFailure([]CheckRecord{ok}, "h", now, window); settled {
		t.Fatal("all green settled as a failure")
	}
	if _, settled := evaluateChecksFailure([]CheckRecord{failed, {Name: "lint", HeadSHA: "h", Status: CheckPending}}, "h", now, window); settled {
		t.Fatal("a running check settled")
	}
	fresh := failed
	fresh.FinishedAt = now.Add(-time.Second)
	if _, settled := evaluateChecksFailure([]CheckRecord{ok, fresh}, "h", now, window); settled {
		t.Fatal("a red inside the window settled")
	}
	noTime := failed
	noTime.FinishedAt = time.Time{}
	if _, settled := evaluateChecksFailure([]CheckRecord{noTime}, "h", now, window); settled {
		t.Fatal("a finish with no time settled")
	}
	failure, settled := evaluateChecksFailure([]CheckRecord{ok, failed, {Name: "other-head", HeadSHA: "x", Status: CheckFailure}}, "h", now, window)
	if !settled || len(failure.Failed) != 1 || failure.Failed[0].Name != "test" || !failure.SettledAt.Equal(failed.FinishedAt) {
		t.Fatalf("settled failure = %+v %v", failure, settled)
	}
	// A LOG SHAPED LIKE A REAL ONE, because the triage reads it now: the
	// runner's own end-of-job marker is what says this red is the change's
	// rather than a runner that died mid-job (go-quicbaas#898).
	logs := map[string]string{"test": "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ failed at line 3\nJob failed"}
	triage := TriageChecksFailure(failure, logs, nil)
	text := failure.Findings(triage, logs, DefaultChecksLogBytes)
	for _, want := range []string{"red on this pull request's current head (h)", "1 required check failed", "FAILED: test", "failed at line 3", "```"} {
		if !strings.Contains(text, want) {
			t.Fatalf("findings lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatal("a credential reached the findings")
	}
	two := ChecksFailure{HeadSHA: "h", Failed: []CheckRecord{failed, {Name: "lint"}}}
	if !strings.Contains(two.Findings(nil, nil, DefaultChecksLogBytes), "2 required checks failed") {
		t.Fatal("plural")
	}
}

func TestChecksLogs_ExcerptAndRead(t *testing.T) {
	raw := "2026-09-05T00:00:00.000Z line one\r\n2026-09-05T00:00:01.000Z line two\n2026-09-05T00:00:02.000Z line three"
	if got := cifault.StripTimestamps(raw); got != "line one\nline two\nline three" {
		t.Fatalf("stripped = %q", got)
	}
	if got := cifault.StripTimestamps("partial first\nsecond"); got != "second" {
		t.Fatalf("an unstamped first line of a multi-line log must be dropped as a partial: %q", got)
	}
	if got := checksLogExcerpt("a\nb\nc\nd", 3); got != "d" {
		t.Fatalf("tail excerpt = %q", got)
	}
	if got := checksLogExcerpt("abcdef", 0); got != "abcdef" {
		t.Fatalf("unbounded = %q", got)
	}
	if got := checksLogExcerpt("héllo", 3); got == "" || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("rune boundary = %q", got)
	}

	forge := newFakeForge()
	forge.logs["test"] = "2026-09-05T00:00:00Z boom\n2026-09-05T00:00:01Z bang"
	failure := ChecksFailure{HeadSHA: "h", Failed: []CheckRecord{{Name: "test"}, {Name: "absent"}}}
	logs := checksFailureLogs(context.Background(), forge, testRepoRef, failure, []ForgeCheckRun{{Name: "test"}}, 1024)
	if logs["test"] != "boom\nbang" || len(logs) != 1 {
		t.Fatalf("logs = %+v", logs)
	}
	if checksFailureLogs(context.Background(), nil, testRepoRef, failure, nil, 1024) != nil {
		t.Fatal("nil forge")
	}
	forge.caps.CheckRunLogs = false
	if checksFailureLogs(context.Background(), forge, testRepoRef, failure, nil, 1024) != nil {
		t.Fatal("forge without log capability")
	}
	forge.caps.CheckRunLogs = true
	if checksFailureLogs(context.Background(), forge, testRepoRef, failure, nil, 0) != nil {
		t.Fatal("zero budget")
	}
}

// TestChecksGreenForReview_ClearsOnlyOnAFinishedGreenPipeline is the dispatch
// gate's decision table. It is the same question EvaluateReviewGate's check half
// answers, and it must answer it identically — a dispatcher that disagreed with
// the merge gate would either review a head the gate refuses or withhold a
// review from a head the gate would land.
func TestChecksGreenForReview_ClearsOnlyOnAFinishedGreenPipeline(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	gated := ReviewPolicy{RequiredApprovals: 1, RequireGreenChecks: true}
	head := "abcdef1234567890"
	done := ForgeCheckRun{ID: "1", Name: "build", HeadSHA: head, Status: "completed", Conclusion: "success", Completed: now}
	failed := ForgeCheckRun{ID: "2", Name: "test", HeadSHA: head, Status: "completed", Conclusion: "failure", Completed: now}
	running := ForgeCheckRun{ID: "3", Name: "test", HeadSHA: head, Status: "in_progress"}
	skipped := ForgeCheckRun{ID: "4", Name: "docker", HeadSHA: head, Status: "completed", Conclusion: "skipped", Completed: now}

	for name, tc := range map[string]struct {
		runs []ForgeCheckRun
		want GateBlockReason
	}{
		"every check green": {[]ForgeCheckRun{done}, GateClear},
		// A job the pipeline skipped is passed over: it neither blocks a
		// green head nor stands in for one. go-quicbaas's Docker job is
		// skipped on every pull request; read as unfinished, it held PR #970
		// green and unreviewed for an hour (2026-09-21).
		"green and a skipped one": {[]ForgeCheckRun{done, skipped}, GateClear},
		"only a skipped one":      {[]ForgeCheckRun{skipped}, GateBlockedNoChecks},
		"skipped and a red one":   {[]ForgeCheckRun{skipped, failed}, GateBlockedChecksFailed},
		"skipped and running":     {[]ForgeCheckRun{skipped, running}, GateBlockedChecksIncomplete},
		"no check at all":         {nil, GateBlockedNoChecks},
		"still running":           {[]ForgeCheckRun{running}, GateBlockedChecksIncomplete},
		"green and running":       {[]ForgeCheckRun{done, running}, GateBlockedChecksIncomplete},
		"one finished red":        {[]ForgeCheckRun{failed}, GateBlockedChecksFailed},
		"green and a red one":     {[]ForgeCheckRun{done, failed}, GateBlockedChecksFailed},
		// A re-run supersedes the run it replaces, by the same rule the merge
		// gate applies: a head whose failed job was restarted and passed is
		// green, and a gate that read the old run would never review it.
		"a red superseded by a green re-run": {[]ForgeCheckRun{
			{ID: "2", Name: "test", HeadSHA: head, Status: "completed", Conclusion: "failure", Completed: now},
			{ID: "9", Name: "test", HeadSHA: head, Status: "completed", Conclusion: "success", Completed: now.Add(time.Minute)},
		}, GateClear},
	} {
		t.Run(name, func(t *testing.T) {
			reason, detail := ChecksGreenForReview(gated, head, tc.runs, now)
			if reason != tc.want {
				t.Fatalf("reason = %q (%s), want %q", reason, detail, tc.want)
			}
			if reason.Clear() != (tc.want == GateClear) {
				t.Fatalf("Clear() disagrees with the reason: %q", reason)
			}
			if !reason.Clear() && detail == "" {
				t.Fatal("a refusal with no detail tells an operator nothing")
			}
			// THE ANSWER MUST MATCH THE MERGE GATE'S. Same records, same
			// function underneath, so the two cannot drift.
			wantSame, _ := evaluateChecks(checkRecordsFor(tc.runs, head, now), head)
			if reason != wantSame {
				t.Fatalf("the dispatch gate said %q where the merge gate says %q", reason, wantSame)
			}
		})
	}
}

// TestChecksGreenForReview_Refusals covers the two inputs that are not a
// pipeline verdict at all.
func TestChecksGreenForReview_Refusals(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	// A project whose policy does not require green checks has said its checks
	// do not decide a merge, so they do not decide a review either — and that
	// holds with no check published at all.
	ungated := ReviewPolicy{RequiredApprovals: 1}
	if reason, _ := ChecksGreenForReview(ungated, "abc", nil, now); !reason.Clear() {
		t.Fatalf("an ungated project was refused: %q", reason)
	}
	// A head that is not known cannot have a verdict attributed to it, and
	// "no head" must not read as "nothing failed".
	gated := ReviewPolicy{RequiredApprovals: 1, RequireGreenChecks: true}
	for _, head := range []string{"", "   "} {
		reason, detail := ChecksGreenForReview(gated, head, nil, now)
		if reason != GateBlockedHeadUnknown {
			t.Fatalf("head %q: reason = %q (%s), want %q", head, reason, detail, GateBlockedHeadUnknown)
		}
	}
}
