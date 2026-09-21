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
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cifault"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Golden equivalence for the sdlc prompt set
// =============================================================================
//
// THE ACCEPTANCE TEST FOR go-quicbaas#803. Every one of these files was
// written by the constants and renderers that stood here before the prompts
// moved into markdown. Extraction is only worth doing if no agent and no
// reader of an issue is told anything different afterwards, and this is how
// that is known rather than believed.
//
// The cases are variable combinations, because a branch is where an
// extraction goes wrong: an issue with no body, a discussion whose blank
// comments must still renumber, one failed check against several, a park
// with no evidence, each remedy in the precedence order.

var updateSDLCGolden = flag.Bool("update-prompt-golden", false,
	"rewrite testdata/prompts goldens from the current renderers")

// assertSDLCGolden compares a rendering to its golden file, byte for byte.
func assertSDLCGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "prompts", name+".golden")
	if *updateSDLCGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s is not byte-identical to its golden\n--- want (%d bytes) ---\n%s\n--- got (%d bytes) ---\n%s",
			name, len(want), string(want), len(got), got)
	}
}

// sdlcGoldenCase is one rendering and the name of the file holding what it
// must produce.
type sdlcGoldenCase struct {
	name   string
	render func(t *testing.T) string
}

func runSDLCGoldenCases(t *testing.T, cases []sdlcGoldenCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSDLCGolden(t, tc.name, tc.render(t))
		})
	}
}

// goldenTime is a fixed instant, so a golden says what the renderer does
// rather than when the test ran.
var goldenTime = time.Date(2026, 9, 12, 8, 30, 0, 0, time.UTC)

// goldenContract is an issue with a body and a discussion that includes a
// blank comment the renderer must skip without leaving a gap in the numbers.
func goldenContract(t *testing.T, body string, comments []ForgeComment) string {
	t.Helper()
	rendered, err := IssueContract(IssueContractSpec{
		Repo:        RepoRef{Owner: "acme", Repo: "api"},
		IssueNumber: 42,
		Title:       "Serve /healthz",
		Body:        body,
		Comments:    comments,
	})
	if err != nil {
		t.Fatalf("render the issue contract: %v", err)
	}
	return rendered
}

func TestSDLCPrompts_Golden(t *testing.T) {
	comments := []ForgeComment{
		{Body: "the store answers in under a millisecond", Author: "owner", Created: goldenTime},
		{Body: "   "},
		{Body: "use the existing router", Author: "reviewer"},
		{Body: "and keep the handler in its own file", Created: goldenTime.Add(time.Hour)},
	}

	reviews := []ReviewRecord{
		{Reviewer: "codex", Verdict: VerdictRequestChanges, HeadSHA: "abc123", Findings: "the error is not typed"},
		{Reviewer: "claude-bot", Verdict: VerdictRequestChanges, HeadSHA: "abc123"},
		{Reviewer: "someone", Verdict: VerdictApprove, HeadSHA: "abc123"},
	}

	oneCheck := ChecksFailure{
		HeadSHA: "abc123",
		Failed:  []CheckRecord{{Name: "build", FinishedAt: goldenTime}},
	}
	manyChecks := ChecksFailure{
		HeadSHA: "abc123",
		Failed: []CheckRecord{
			{Name: "build", FinishedAt: goldenTime},
			{Name: "test", FinishedAt: goldenTime.Add(time.Minute)},
			{Name: "vet"},
		},
	}

	failures := []orchestration.ValidationResult{
		{Name: "make build", Passed: true},
		{Name: "make test", Passed: false, Message: "2 packages failed", Details: "--- FAIL: TestThing"},
		{Name: "make lint", Passed: false},
	}

	spokenTurn := AgentTurn{
		Attempt: 2, Elapsed: 90 * time.Second, Outcome: TurnOutcomeQuestion,
		Reply: "I need to know which router to use.\n\n" + TurnStopMarker + " " + TurnStopQuestion,
	}
	spokenTurn.ReplyBytes = len(spokenTurn.Reply)
	silentTurn := AgentTurn{Attempt: 3, Elapsed: 5 * time.Second, Outcome: TurnOutcomeSilent}

	runSDLCGoldenCases(t, []sdlcGoldenCase{
		{"worker_charter", func(*testing.T) string { return WorkerCharter() }},
		{"reviewer_charter", func(*testing.T) string { return ReviewerCharter() }},
		{"issue_contract_full", func(t *testing.T) string {
			return goldenContract(t, "Serve /healthz and return 200 when the store answers.", comments)
		}},
		{"issue_contract_no_body", func(t *testing.T) string { return goldenContract(t, "", comments) }},
		{"issue_contract_no_comments", func(t *testing.T) string {
			return goldenContract(t, "Serve /healthz.", nil)
		}},
		{"issue_contract_only_blank_comments", func(t *testing.T) string {
			return goldenContract(t, "Serve /healthz.", []ForgeComment{{Body: "  "}})
		}},
		{"round_findings", func(*testing.T) string {
			return "## Round " + itoaGolden(2) + " findings\n\nthe error is not typed"
		}},
		{"verification_failed", func(*testing.T) string {
			return "## Verification failed\n\n" + renderFailures(failures)
		}},
		{"review_findings", func(*testing.T) string {
			return "## Review findings\n\n" + renderFailures(failures)
		}},
		{"changes_requested", func(*testing.T) string { return findingsFor(reviews, "abc123") }},
		{"changes_requested_none", func(*testing.T) string { return findingsFor(nil, "abc123") }},
		{"ci_red_one_check", func(*testing.T) string {
			return oneCheck.Findings(nil, nil, DefaultChecksLogBytes)
		}},
		{"ci_red_many_checks", func(*testing.T) string {
			return manyChecks.Findings(nil,
				map[string]string{"build": "undefined: Handler\n", "test": "  "}, DefaultChecksLogBytes)
		}},
		// A day when the fleet is unwell: one real failure, one runner that
		// died and one host that could not start the container. The worker is
		// told which is its own and told not to chase the other two.
		{"ci_red_with_infrastructure", func(*testing.T) string {
			return manyChecks.Findings(ChecksTriage{
				{Check: CheckRecord{Name: "build"}, Finding: cifault.Finding{Verdict: cifault.VerdictGenuine, Signature: cifault.SignatureUnrecognised}},
				{Check: CheckRecord{Name: "test"}, Finding: cifault.Finding{Verdict: cifault.VerdictVoid, Signature: cifault.SignatureRunnerDied}},
				{Check: CheckRecord{Name: "vet"}, Finding: cifault.Finding{Verdict: cifault.VerdictHostDefect, Signature: cifault.SignatureContainerCreateFailed}},
			}, map[string]string{"build": "undefined: Handler\n", "test": "ok\tpkg/api\t1.0s\n"}, DefaultChecksLogBytes)
		}},
		{"prior_turn_spoken", func(*testing.T) string { return renderPriorTurn(spokenTurn) }},
		{"prior_turn_silent", func(*testing.T) string { return renderPriorTurn(silentTurn) }},
		{"silent_turn_note", func(*testing.T) string { return silentTurnNote(5 * time.Second) }},
		{"session_rolled_note", func(*testing.T) string { return sessionRolledNote(24*time.Minute+4*time.Second, 1) }},
		{"repeated_prompt", func(t *testing.T) string {
			reason, err := repeatedPromptReason()
			if err != nil {
				t.Fatalf("compose the repeated-prompt reason: %v", err)
			}
			return reason
		}},
		{"park_reason_spoken", func(t *testing.T) string {
			reason, err := TurnParkReason(turnVerdicts[TurnOutcomeQuestion], spokenTurn)
			if err != nil {
				t.Fatalf("compose the park reason: %v", err)
			}
			return reason
		}},
		{"park_reason_silent", func(t *testing.T) string {
			reason, err := TurnParkReason(turnVerdicts[TurnOutcomeEnvironment], silentTurn)
			if err != nil {
				t.Fatalf("compose the park reason: %v", err)
			}
			return reason
		}},
		{"outcome_needs_info", func(t *testing.T) string {
			return renderOutcome(outcomeReport{
				binding: goldenBinding(), task: goldenTask(t), attempt: 1, kind: OutcomeNeedsInfo,
				message:    goldenReason(t, UnblockOwnerRuling, "the worker asked which router to use"),
				iterations: 2, delivery: goldenDelivery(),
			})
		}},
		{"outcome_blocked_self_clearing", func(t *testing.T) string {
			return renderOutcome(outcomeReport{
				binding: goldenBinding(), task: goldenTask(t), attempt: 2, kind: OutcomeBlocked,
				message: goldenReasonOn(t, UnblockCondition{Kind: UnblockPipelineGreen, Subject: "abc123"},
					"continuous integration is red"),
			})
		}},
		{"outcome_exhausted", func(t *testing.T) string {
			return renderOutcome(outcomeReport{
				binding: goldenBinding(), task: goldenExhaustedTask(t), attempt: 3, kind: OutcomeExhausted,
				message: "the worker produced no changes",
			})
		}},
		{"outcome_readmitted", func(t *testing.T) string {
			return renderOutcome(outcomeReport{
				binding: goldenBinding(), task: goldenTask(t), attempt: 1, kind: OutcomeReadmitted,
			})
		}},
		{"outcome_no_evidence", func(*testing.T) string {
			return renderOutcome(outcomeReport{kind: OutcomeBlocked})
		}},
		{"outcome_with_cause", func(t *testing.T) string {
			task := goldenTask(t)
			task.BlockFaultClass = orchestration.FaultNoAccountResolver
			return renderOutcome(outcomeReport{
				binding: goldenBinding(), task: task, attempt: 1, kind: OutcomeBlocked,
				message: goldenReason(t, UnblockOperatorRepair, "the account pool is empty"),
			})
		}},
	})
}

// goldenReason composes a park reason the way a caller does.
func goldenReason(t *testing.T, kind UnblockKind, detail string) string {
	t.Helper()
	return goldenReasonOn(t, UnblockCondition{Kind: kind}, detail)
}

// goldenReasonOn composes a park reason for a condition that names a subject.
func goldenReasonOn(t *testing.T, condition UnblockCondition, detail string) string {
	t.Helper()
	reason, err := ComposeBlockedReason(condition, detail)
	if err != nil {
		t.Fatalf("compose a blocked reason: %v", err)
	}
	return reason
}

// goldenBinding is a binding with the labels a transition line needs.
func goldenBinding() *ProjectBinding {
	return &ProjectBinding{
		Forge: ForgeConfig{
			TriggerLabel: "task:ready",
			Labels: map[string]string{
				LabelNeedsInfo:  "factory:needs-info",
				LabelBlocked:    "factory:blocked",
				LabelEscalated:  "factory:escalated",
				LabelInProgress: "factory:in-progress",
			},
		},
	}
}

// goldenTask is a task with a history and a budget left.
func goldenTask(t *testing.T) *orchestration.Task {
	t.Helper()
	return &orchestration.Task{
		ID: "task-1", Attempts: 1, AttemptBudget: 3,
		Metadata: map[string]string{"attempt_history": "job-1"},
	}
}

// goldenExhaustedTask is a task whose budget is spent.
func goldenExhaustedTask(t *testing.T) *orchestration.Task {
	t.Helper()
	return &orchestration.Task{
		ID: "task-1", Attempts: 3, AttemptBudget: 3, EnvironmentFaults: 1,
		Metadata: map[string]string{"attempt_history": "job-1,job-2,job-3"},
	}
}

// goldenDelivery is an attempt with a pull request open.
func goldenDelivery() *Delivery {
	return &Delivery{PullRequest: DeliveryPullRequest{Number: 7, URL: "http://forge.test/acme/api/pulls/7"}}
}

// itoaGolden keeps the round-findings case honest about its number without
// pulling strconv into the fixture list.
func itoaGolden(n int) string {
	if n < 0 || n > 9 {
		return "?"
	}
	return string(rune('0' + n))
}
