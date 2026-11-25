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
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// jsonKeys is the sorted json tag names of a struct type.
func jsonKeys(typ reflect.Type) []string {
	keys := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

func TestRunSummary_CarriesOnlyAllowlistedFields(t *testing.T) {
	cases := []struct {
		typ  reflect.Type
		want []string
	}{
		{reflect.TypeOf(RunSummary{}), []string{
			"version", "outcome", "task_id", "job_id", "attempt", "stage", "cause", "fault_class",
			"pull_request", "review_rounds", "checks", "checks_omitted", "git", "conflict_count", "audit_record",
		}},
		{reflect.TypeOf(RunSummaryCheck{}), []string{"index", "program", "exit_code", "withheld_bytes"}},
		{reflect.TypeOf(RunSummaryGit{}), []string{"subcommand", "exit_code", "withheld_bytes"}},
	}
	for _, tc := range cases {
		want := append([]string(nil), tc.want...)
		sort.Strings(want)
		if got := jsonKeys(tc.typ); !reflect.DeepEqual(got, want) {
			t.Errorf("%s keys = %v, want the reviewed allowlist %v", tc.typ.Name(), got, want)
		}
	}
}

// privateMarkers are values that must never reach a public summary. The
// forge-shaped one is built at run time.
func privateMarkers() []string {
	return []string{
		"git.example.com",
		"10.20.30.40",
		"https://bot:hunter2@git.example.com/acme/api.git",
		"qbx_live_9f8e7d6c5b4a3f2e1d0c",
		"ghp_" + strings.Repeat("a1B2c3", 6),
		"/home/builder/.ssh/id_ed25519",
		"/var/lib/quicbaasd/workspaces/tenant-secret-7/task-1",
		"DEPLOY_TOKEN=s3cr3t-v4lue",
		"internal/billing/keys.go",
	}
}

func TestRunSummary_LeaksNothingPrivate(t *testing.T) {
	markers := privateMarkers()
	all := strings.Join(markers, " ")
	failedName := "DEPLOY_TOKEN=s3cr3t-v4lue /home/builder/bin/make test --url=https://bot:hunter2@git.example.com"
	delivery := &Delivery{
		TaskID:      "task-1",
		Branch:      all,
		Base:        all,
		WorkDir:     all,
		PullRequest: DeliveryPullRequest{Number: 7, URL: markers[2], Title: all, Base: all},
		Reviews:     []ReviewRecord{{Findings: all}},
	}
	cases := []struct {
		name string
		in   runSummaryInput
	}{
		{"checkout", runSummaryInput{Err: &CheckoutCommandError{TaskID: "task-1", Command: all, ExitCode: 128, Output: all}}},
		{"publish", runSummaryInput{Err: &PublishCommandError{TaskID: "task-1", Command: all, ExitCode: 1, Output: all}}},
		{"conflict", runSummaryInput{Err: &MergeResultConflictError{TaskID: "task-1", Branch: all, Base: all, Paths: markers, Output: all}}},
		{"no changes", runSummaryInput{Err: &NoChangesError{TaskID: "task-1", WorkDir: all, Reply: all}}},
		{"escalated", runSummaryInput{Err: &EscalatedError{TaskID: "task-1", Reason: all}, ParkKind: orchestration.TaskBlockKindEscalated}},
		{"plain", runSummaryInput{Err: fmt.Errorf("sdlc: %s", all), LastState: orchestration.JobStateExecuting}},
		{"checks", runSummaryInput{
			Err:          &EscalatedError{TaskID: "task-1", Reason: all},
			LastState:    orchestration.JobStateValidating,
			FailedChecks: []orchestration.ValidationResult{{Name: failedName, Message: all, Details: all, ExitCode: 2}},
		}},
		{"delivery", runSummaryInput{Delivery: delivery, LastState: orchestration.JobStateCleanup, FaultClass: all}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.TaskID, tc.in.JobID, tc.in.Attempt = "task-1", "job-1", 1
			summary := summarizeRun(tc.in)
			encoded, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			rendered := summary.Render()
			for _, marker := range append(markers, "tenant-secret-7") {
				if strings.Contains(string(encoded), marker) {
					t.Errorf("the JSON carries %q: %s", marker, encoded)
				}
				if strings.Contains(rendered, marker) {
					t.Errorf("the rendering carries %q:\n%s", marker, rendered)
				}
			}
		})
	}
}

func TestRunSummary_ClassifiesEachTypedError(t *testing.T) {
	cases := []struct {
		name      string
		in        runSummaryInput
		outcome   RunOutcome
		cause     string
		stage     ArtifactStage
		git       string
		conflicts int
		audit     string
		fault     string
	}{
		{"checkout", runSummaryInput{Err: &CheckoutCommandError{Command: "fetch origin", ExitCode: 128, Output: "x"}},
			RunOutcomeFailed, "checkout_failed", StageBranch, "fetch", 0, auditCheckout, ""},
		{"no changes", runSummaryInput{Err: &NoChangesError{}},
			RunOutcomeFailed, "no_changes", StageCommit, "", 0, "", ""},
		{"conflict", runSummaryInput{Err: &MergeResultConflictError{Paths: []string{"a", "b"}}},
			RunOutcomeFailed, "merge_conflict", StageVerification, "merge", 2, auditMergeConflict, ""},
		{"publish", runSummaryInput{Err: &PublishCommandError{Command: "--force push", ExitCode: 1}},
			RunOutcomeFailed, "publish_failed", StagePullRequest, "git", 0, "", ""},
		{"escalated", runSummaryInput{Err: &EscalatedError{}, ParkKind: orchestration.TaskBlockKindEscalated, LastState: orchestration.JobStateReviewing},
			RunOutcomeParked, "escalated", StageReview, "", 0, "", ""},
		{"escalated with no kind", runSummaryInput{Err: fmt.Errorf("wrapped: %w", &EscalatedError{}), LastState: orchestration.JobStateValidating},
			RunOutcomeParked, "escalated", StageVerification, "", 0, "", ""},
		{"needs info", runSummaryInput{ParkKind: orchestration.TaskBlockKindNeedsInfo, LastState: orchestration.JobStateExecuting},
			RunOutcomeParked, "needs_info", StageCommit, "", 0, "", ""},
		{"plain", runSummaryInput{Err: errors.New("boom"), LastState: orchestration.JobStateRedelegating},
			RunOutcomeFailed, "error", StageCommit, "", 0, "", ""},
		{"completed", runSummaryInput{LastState: orchestration.JobStateCleanup},
			RunOutcomeCompleted, "", StageMerge, "", 0, "", ""},
		{"operator fault kept", runSummaryInput{Err: &EscalatedError{}, ParkKind: orchestration.TaskBlockKindEscalated, FaultClass: orchestration.FaultWorkerEnvironment, LastState: orchestration.JobStateInitializing},
			RunOutcomeParked, "escalated", StageBranch, "", 0, "", orchestration.FaultWorkerEnvironment},
		{"made-up fault dropped", runSummaryInput{Err: &EscalatedError{}, ParkKind: orchestration.TaskBlockKindEscalated, FaultClass: "made_up_fault"},
			RunOutcomeParked, "escalated", "", "", 0, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := summarizeRun(tc.in)
			if s.Version != runSummaryVersion || s.Outcome != tc.outcome || s.Cause != tc.cause || s.Stage != tc.stage ||
				s.ConflictCount != tc.conflicts || s.AuditRecord != tc.audit || s.FaultClass != tc.fault {
				t.Fatalf("summary = %+v, want outcome %q cause %q stage %q conflicts %d audit %q fault %q",
					s, tc.outcome, tc.cause, tc.stage, tc.conflicts, tc.audit, tc.fault)
			}
			gotGit := ""
			if s.Git != nil {
				gotGit = s.Git.Subcommand
			}
			if gotGit != tc.git {
				t.Fatalf("git = %q, want %q", gotGit, tc.git)
			}
		})
	}
}

func TestRunSummary_ProgramIsTheCommandsBaseName(t *testing.T) {
	cases := map[string]string{
		"make test":               "make",
		"A=1 B=2 go test ./...":   "go",
		"./scripts/check.sh --x":  "check.sh",
		"/usr/bin/env FOO=1 make": "env",
		"":                        "command",
		"$(curl evil)":            "command",
		"`id`":                    "command",
	}
	for command, want := range cases {
		if got := checkProgram(command); got != want {
			t.Errorf("checkProgram(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestRunSummary_BoundsTheChecks(t *testing.T) {
	failed := make([]orchestration.ValidationResult, 11)
	for i := range failed {
		failed[i] = orchestration.ValidationResult{Name: "make test", ExitCode: 2, Message: "out"}
	}
	s := summarizeRun(runSummaryInput{Err: &EscalatedError{}, FailedChecks: failed, JobID: "job-1"})
	if len(s.Checks) != runSummaryMaxChecks || s.ChecksOmitted != 3 {
		t.Fatalf("checks = %d, omitted = %d; want %d and 3", len(s.Checks), s.ChecksOmitted, runSummaryMaxChecks)
	}
	if s.Checks[7].Index != 8 || s.AuditRecord != auditVerification {
		t.Fatalf("summary = %+v", s)
	}
	if rendered := s.Render(); !strings.Contains(rendered, "\n- and 3 more failed check(s)\n") {
		t.Fatalf("the rendering does not count the omitted checks:\n%s", rendered)
	}
}

func TestHooks_RecordRunSummaryWritesOneInlineArtifactPerAttempt(t *testing.T) {
	rig := newHooksRig(t)
	job := &orchestration.JobContext{TenantID: testTenant, TaskID: testTask, Attempt: 2}
	summary := summarizeRun(runSummaryInput{
		TaskID: testTask, JobID: "job-2", Attempt: 2, Err: &NoChangesError{}, LastState: orchestration.JobStateExecuting,
	})
	if err := rig.hooks.recordRunSummary(context.Background(), job, summary); err != nil {
		t.Fatalf("recordRunSummary: %v", err)
	}
	artifacts := rig.tasks.submitted(testTask)
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want one", artifacts)
	}
	got := artifacts[0]
	if got.Name != "run_summary@a2" || got.Kind != orchestration.ArtifactKindInline || got.MIMEType != "application/json" ||
		got.Metadata["attempt"] != "2" || got.Metadata["outcome"] != string(RunOutcomeFailed) {
		t.Fatalf("artifact = %+v", got)
	}
	var decoded RunSummary
	if err := json.Unmarshal(got.Content, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, summary) {
		t.Fatalf("decoded = %+v, want %+v", decoded, summary)
	}

	if err := rig.hooks.recordRunSummary(context.Background(), nil, summary); err != nil {
		t.Fatalf("a nil job: %v", err)
	}
	if err := rig.hooks.recordRunSummary(context.Background(), &orchestration.JobContext{TenantID: testTenant}, summary); err != nil {
		t.Fatalf("a job with no task: %v", err)
	}
	if n := len(rig.tasks.submitted(testTask)); n != 1 {
		t.Fatalf("artifacts = %d after the no-op records, want 1", n)
	}
}
