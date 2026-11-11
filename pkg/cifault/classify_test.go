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

package cifault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// The fixture corpus
// =============================================================================
//
// Every .log under testdata is a REAL Gitea Actions job log from the
// 2026-09-12 incident, trimmed to the tail the classifier actually reads. The
// trim is the only edit: no line was written, reworded or reordered, so a rule
// that passes here passes against what the fleet really emitted. Provenance is
// recorded per case in the `source` field below, because a fixture nobody can
// trace back is a fixture nobody can re-derive.
//
// Sizes are deliberate: the classifier is handed a bounded tail in production
// (pkg/sdlc reads at most DefaultChecksClassifyBytes per check), and a 43,787
// line log committed to the tree would be a liability rather than evidence.

// fixture reads one testdata log.
func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	if len(raw) == 0 {
		t.Fatalf("fixture %s is empty", name)
	}
	return string(raw)
}

// ownersOfThisFleet is the private-module owner every go-quicbaas project on
// this fleet depends on.
var ownersOfThisFleet = []string{"jeremyhahn"}

// TestClassify_RealJobLogs is the corpus test: one real log per signature,
// each asserted to the verdict its signature carries.
func TestClassify_RealJobLogs(t *testing.T) {
	cases := []struct {
		name          string
		source        string
		file          string
		conclusion    Conclusion
		runner        RunnerState
		owners        []string
		wantVerdict   Verdict
		wantSignature Signature
		wantEvidence  string
	}{
		{
			name: "runner killed mid test suite",
			source: "go-quicbaas Actions job 77459 (task 67418, job \"test\", runner ci-runner-02), " +
				"last 30 of 43,787 lines. Reported red with zero FAIL lines in the whole log.",
			file:          "runner-died-mid-suite.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictVoid,
			wantSignature: SignatureRunnerDied,
			wantEvidence:  "pkg/api/websocket",
		},
		{
			name: "runner killed mid security analysis",
			source: "go-quicbaas Actions job 77461 (task 67420, job \"security\", runner ci-runner-02), " +
				"last 25 of 350 lines. Truncates inside gosec's package walk.",
			file:          "runner-died-mid-analysis.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictVoid,
			wantSignature: SignatureRunnerDied,
			wantEvidence:  "gosec",
		},
		{
			name: "status write raced the runner's shutdown",
			source: "go-quicbaas Actions job for task 67328 (\"Unit Tests\"), last 20 of 180,059 lines. " +
				"Ends \"Job succeeded\" then \"context canceled\" while the API reported failure.",
			file:          "status-raced-shutdown.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictVoid,
			wantSignature: SignatureStatusRacedShutdown,
			wantEvidence:  "Job succeeded",
		},
		{
			name: "runner failed tearing down a job that passed",
			source: "go-luks2 PR #203 \"CI / Unit Tests\" of 2026-09-22 (go-quicbaas#1011). The three " +
				"teardown lines are quoted verbatim from the issue, elisions included; the green " +
				"tail before them is status-raced-shutdown.log's own, the run log having been " +
				"replaced when an operator re-ran the job.",
			file:          "runner-teardown.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictVoid,
			wantSignature: SignatureRunnerTeardown,
			wantEvidence:  "Error while stop job container",
		},
		{
			name: "runner failed tearing down a job that passed, with its finally error trailing",
			source: "go-quicbaas PR for #1011, \"CI / Unit Tests\" of 2026-09-23 (task 79769), last 31 " +
				"lines as the forge review quoted them, the lab forge address replaced by " +
				"198.51.100.50. The runner writes one more line after \"Job failed\": the " +
				"teardown error again, from its finally block.",
			file:          "runner-teardown-finally.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictVoid,
			wantSignature: SignatureRunnerTeardown,
			wantEvidence:  "Error occurred running finally",
		},
		{
			name: "a failed step stays genuine however the runner tore the job down",
			source: "genuine-test-failure.log with the go-luks2 teardown lines inserted between its " +
				"cleanup marker and its \"Job failed\": a step ended \"Process completed with exit " +
				"code 1\", and that is the change's answer.",
			file:          "genuine-failure-with-teardown.log",
			conclusion:    ConclusionFailure,
			owners:        ownersOfThisFleet,
			wantVerdict:   VerdictGenuine,
			wantSignature: SignatureUnrecognised,
		},
		{
			name: "registry unreachable over TLS",
			source: "go-quicbaas Actions job 77520 (task 67425, job \"base\", runner ci-runner-01), whole log " +
				"(20 lines). The host had no insecure-registries entry for the forge's own registry.",
			file:          "registry-unreachable.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictHostDefect,
			wantSignature: SignatureRegistryUnreachable,
			wantEvidence:  "failed to pull image",
		},
		{
			name: "OCI runtime refused to create the container",
			source: "go-quicbaas Actions job 77604 (task 67462, job \"merge\", runner ci-runner-01), whole log " +
				"(23 lines). The host advertised lxcfs bind-mounts it did not have.",
			file:          "container-create-failed.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictHostDefect,
			wantSignature: SignatureContainerCreateFailed,
			wantEvidence:  "runc create failed",
		},
		{
			name: "private module could not be resolved",
			source: "go-quicbaas Actions job 77532 (task 67430, job \"lint\", runner ci-runner-01), last 45 of " +
				"453 lines. act_runner ignored its mounted config, so the module settings were unset.",
			file:          "module-resolution.log",
			conclusion:    ConclusionFailure,
			owners:        ownersOfThisFleet,
			wantVerdict:   VerdictHostDefect,
			wantSignature: SignatureModuleResolution,
			wantEvidence:  "unknown revision",
		},
		{
			name: "genuine test failure",
			source: "go-quicbaas race-detector job of 2026-09-09, last 45 of 540 lines. A real " +
				"--- FAIL in pkg/fleet.",
			file:          "genuine-test-failure.log",
			conclusion:    ConclusionFailure,
			owners:        ownersOfThisFleet,
			wantVerdict:   VerdictGenuine,
			wantSignature: SignatureUnrecognised,
		},
		{
			name: "genuine failure through a make recipe",
			source: "go-qrdb Actions job 77312 (job \"lint\"), last 45 of 444 lines. A build gate " +
				"failed and make returned 1.",
			file:          "genuine-make-error.log",
			conclusion:    ConclusionFailure,
			owners:        ownersOfThisFleet,
			wantVerdict:   VerdictGenuine,
			wantSignature: SignatureUnrecognised,
		},
		{
			name: "genuine failure whose evidence is outside the excerpt",
			source: "the same go-quicbaas race-detector log, trimmed to its last 30 lines instead of 45 " +
				"so the FAIL lines fall outside the window. The runner's own \"Job failed\" is the " +
				"only thing left, and it has to be enough.",
			file:          "genuine-marker-only.log",
			conclusion:    ConclusionFailure,
			owners:        ownersOfThisFleet,
			wantVerdict:   VerdictGenuine,
			wantSignature: SignatureUnrecognised,
		},
		{
			name: "a private-module failure with no private owners declared is the change's own",
			source: "job 77532 again, classified for a project that declares no private module owners: " +
				"\"unknown revision\" on a public module is a tag nobody pushed.",
			file:          "module-resolution.log",
			conclusion:    ConclusionFailure,
			wantVerdict:   VerdictGenuine,
			wantSignature: SignatureUnrecognised,
		},
		{
			name: "a private-module failure for another owner is the change's own",
			source: "job 77532 again, with an unrelated owner declared private. The unresolved module " +
				"is not theirs, so the rule must not fire.",
			file:          "module-resolution.log",
			conclusion:    ConclusionFailure,
			owners:        []string{"someone-else"},
			wantVerdict:   VerdictGenuine,
			wantSignature: SignatureUnrecognised,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			finding, err := Classify(Input{
				Conclusion:          tc.conclusion,
				Log:                 fixture(t, tc.file),
				Runner:              tc.runner,
				PrivateModuleOwners: tc.owners,
			})
			if err != nil {
				t.Fatalf("Classify: unexpected error: %v (source: %s)", err, tc.source)
			}
			if finding.Verdict != tc.wantVerdict {
				t.Errorf("verdict = %q, want %q\nevidence: %s\nsource: %s",
					finding.Verdict, tc.wantVerdict, finding.Evidence, tc.source)
			}
			if finding.Signature != tc.wantSignature {
				t.Errorf("signature = %q, want %q\nevidence: %s",
					finding.Signature, tc.wantSignature, finding.Evidence)
			}
			if tc.wantEvidence != "" && !strings.Contains(finding.Evidence, tc.wantEvidence) {
				t.Errorf("evidence %q does not quote %q", finding.Evidence, tc.wantEvidence)
			}
			if finding.Evidence == "" {
				t.Error("a finding with no evidence cannot be checked by an operator")
			}
		})
	}
}

// =============================================================================
// The dangerous direction
// =============================================================================
//
// Calling a genuine failure "void" auto-retries a broken change: the engine
// pays for CI round after round and the defect never reaches anybody who could
// fix it. Calling a void run "genuine" costs one wasted agent attempt, which
// is what happens today anyway. The tests below are therefore deliberately
// out of proportion to the rest of the file.

// genuineFixtures are the real logs whose red belongs to the change.
//
// evidenceVisible says whether the change's own failure — a FAIL line, a
// panic, a non-zero recipe — is inside the excerpt. It is not a property of
// the run but of how far back the excerpt reaches, and it is exactly what
// vetoVoid needs to see, so the two fixtures that carry it and the one that
// does not are both here on purpose.
var genuineFixtures = []struct {
	file            string
	evidenceVisible bool
}{
	{"genuine-test-failure.log", true},
	{"genuine-make-error.log", true},
	{"genuine-marker-only.log", false},
	{"genuine-failure-with-teardown.log", true},
}

// hostileRunnerStates are every runner state a caller could plausibly hand the
// classifier alongside a genuine failure, including the ones a void rule reads.
func hostileRunnerStates() map[string]RunnerState {
	return map[string]RunnerState{
		"nothing known":              {},
		"runner not reporting":       {ReportingKnown: true, Reporting: false},
		"runner still reporting":     {ReportingKnown: true, Reporting: true},
		"sigkill, oom scope unknown": {ExitCode: sigkillExitCode},
		"sigkill, oom in the job":    {ExitCode: sigkillExitCode, OOM: OOMJob},
		"sigkill, no oom at all":     {ExitCode: sigkillExitCode, OOM: OOMNone},
		"sigkill, oom on the host":   {ExitCode: sigkillExitCode, OOM: OOMHost},
		"ordinary non-zero exit":     {ExitCode: 1},
	}
}

// TestClassify_GenuineFailureIsNeverVoid crosses every genuine fixture with
// every runner state a caller could supply, and refuses a void verdict in all
// of them.
//
// The sigkill states are the point: a genuine test failure on a host that ALSO
// evicted something must still reach its author, because the log says what
// broke and re-running it produces the same log.
func TestClassify_GenuineFailureIsNeverVoid(t *testing.T) {
	for _, genuine := range genuineFixtures {
		log := fixture(t, genuine.file)
		for name, runner := range hostileRunnerStates() {
			// A POSITIVELY OBSERVED HOST EVICTION IS THE ONE FACT THAT CAN
			// OUTWEIGH AN EXCERPT. When the caller states that the process
			// was SIGKILLed with no out-of-memory kill of its own AND the
			// excerpt shows nothing failing, void is the right answer: a
			// suite that genuinely fails exits 1, not 137. Where the excerpt
			// DOES show the failure, the veto must hold even then, and that
			// is asserted by the two fixtures above. The excluded pair is
			// pinned by TestClassify_HostEvictionOverridesAnUnexplainedRed.
			evicted := runner.ExitCode == sigkillExitCode &&
				(runner.OOM == OOMNone || runner.OOM == OOMHost)
			if evicted && !genuine.evidenceVisible {
				continue
			}
			for _, owners := range [][]string{nil, ownersOfThisFleet, {""}, {"JeremyHahn"}} {
				t.Run(genuine.file+"/"+name+"/"+strings.Join(owners, ","), func(t *testing.T) {
					finding, err := Classify(Input{
						Conclusion:          ConclusionFailure,
						Log:                 log,
						Runner:              runner,
						PrivateModuleOwners: owners,
					})
					if err != nil {
						t.Fatalf("Classify: %v", err)
					}
					if finding.Verdict == VerdictVoid {
						t.Fatalf("a genuine failure was classified VOID (signature %q, evidence %q): "+
							"this would auto-retry a broken change", finding.Signature, finding.Evidence)
					}
					if !finding.Verdict.ReachesAgent() {
						t.Fatalf("verdict %q does not reach the agent (signature %q, evidence %q)",
							finding.Verdict, finding.Signature, finding.Evidence)
					}
				})
			}
		}
	}
}

// TestClassify_FailureEvidenceVetoesVoid proves the veto directly: take a log
// that IS void — a runner killed mid-suite, no end-of-job marker — and add one
// line of the change's own failure. The verdict must move to genuine.
func TestClassify_FailureEvidenceVetoesVoid(t *testing.T) {
	void := fixture(t, "runner-died-mid-suite.log")
	base, err := Classify(Input{Conclusion: ConclusionFailure, Log: void})
	if err != nil {
		t.Fatalf("Classify base: %v", err)
	}
	if base.Verdict != VerdictVoid {
		t.Fatalf("the base fixture must be void for this test to mean anything; got %q", base.Verdict)
	}

	evidence := map[string]string{
		"a failed go test":      "--- FAIL: TestSomethingReal (0.01s)",
		"a failed package":      "FAIL\tgithub.com/jeremyhahn/go-quicbaas/pkg/fleet\t1.161s",
		"a bare FAIL":           "FAIL",
		"a panic":               "panic: runtime error: invalid memory address",
		"a fatal runtime error": "fatal error: concurrent map writes",
		"a failed recipe":       "make: *** [Makefile:516: vet] Error 1",
		"a workflow annotation": "##[error]Process completed with exit code 1",
		"an actions annotation": "::error::the build did not compile",
	}
	for name, line := range evidence {
		t.Run(name, func(t *testing.T) {
			finding, err := Classify(Input{
				Conclusion: ConclusionFailure,
				Log:        void + "2026-09-12T17:50:33.0000000Z " + line + "\n",
			})
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if finding.Verdict != VerdictGenuine {
				t.Fatalf("verdict = %q with %q in the log, want genuine (evidence %q)",
					finding.Verdict, line, finding.Evidence)
			}
		})
	}
}

// TestClassify_MidLogSuccessMarkerDoesNotVoid proves the whole-line, tail-only
// match on the success marker. A test that prints "Job succeeded" in the
// middle of a suite it then fails must not void the run.
func TestClassify_MidLogSuccessMarkerDoesNotVoid(t *testing.T) {
	stamp := "2026-09-12T17:50:33.0000000Z "
	var b strings.Builder
	b.WriteString(stamp + "=== RUN   TestSchedulerReportsProgress\n")
	b.WriteString(stamp + "Job succeeded\n")
	b.WriteString(stamp + "    scheduler_test.go:88: the report said so\n")
	for i := 0; i < statusMarkerTailLines+4; i++ {
		b.WriteString(stamp + "ok  \tgithub.com/jeremyhahn/go-quicbaas/pkg/filler\t0.10s\n")
	}
	b.WriteString(stamp + "Job failed\n")
	b.WriteString(stamp + "Job 'Unit Tests' failed\n")

	finding, err := Classify(Input{Conclusion: ConclusionFailure, Log: b.String()})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if finding.Verdict != VerdictGenuine {
		t.Fatalf("verdict = %q, want genuine: a mid-log success line must not void a failed run "+
			"(signature %q, evidence %q)", finding.Verdict, finding.Signature, finding.Evidence)
	}
}

// TestClassify_BothMarkersInTheWindowIsGenuine covers the other half of that
// guard: a runner that says both things in its last lines has said the job
// failed.
func TestClassify_BothMarkersInTheWindowIsGenuine(t *testing.T) {
	stamp := "2026-09-12T17:50:33.0000000Z "
	log := stamp + "Cleaning up container for job Unit Tests\n" +
		stamp + "Job succeeded\n" +
		stamp + "Job failed\n"
	finding, err := Classify(Input{Conclusion: ConclusionFailure, Log: log})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if finding.Verdict != VerdictGenuine {
		t.Fatalf("verdict = %q, want genuine (signature %q)", finding.Verdict, finding.Signature)
	}
}

// TestClassify_UnreadableLogIsGenuine proves that absence of evidence is not
// evidence. A check whose log could not be read — the forge cannot serve one,
// the adapter refused the target URL, the run logged nothing — reaches the
// agent, exactly as it does today.
func TestClassify_UnreadableLogIsGenuine(t *testing.T) {
	for name, log := range map[string]string{
		"no log at all":     "",
		"whitespace only":   "   \n\t\n   ",
		"one partial line":  "2026-09-12T17:50:33.0000000Z ",
		"blank lines only":  "\n\n\n",
		"nothing but stamp": "2026-09-12T17:50:33.0000000Z\n",
	} {
		t.Run(name, func(t *testing.T) {
			for stateName, runner := range hostileRunnerStates() {
				if runner.ExitCode == sigkillExitCode &&
					(runner.OOM == OOMNone || runner.OOM == OOMHost) {
					// That state is a host eviction on its own facts, not on
					// the log's; it has its own test.
					continue
				}
				finding, err := Classify(Input{Conclusion: ConclusionFailure, Log: log, Runner: runner})
				if err != nil {
					t.Fatalf("Classify: %v", err)
				}
				if finding.Verdict != VerdictGenuine {
					t.Fatalf("%s with %s: verdict = %q, want genuine (signature %q)",
						name, stateName, finding.Verdict, finding.Signature)
				}
			}
		})
	}
}

// =============================================================================
// Each rule, in isolation
// =============================================================================

// TestClassify_HostEviction covers the exit-137 rule across every OOM scope.
func TestClassify_HostEviction(t *testing.T) {
	cases := map[string]struct {
		runner      RunnerState
		wantVerdict Verdict
	}{
		"sigkill with no oom is an eviction":    {RunnerState{ExitCode: sigkillExitCode, OOM: OOMNone}, VerdictVoid},
		"sigkill with a host oom is collateral": {RunnerState{ExitCode: sigkillExitCode, OOM: OOMHost}, VerdictVoid},
		"sigkill with the job's own oom is the change's appetite": {
			RunnerState{ExitCode: sigkillExitCode, OOM: OOMJob}, VerdictGenuine,
		},
		"sigkill with an unknown oom scope earns nothing": {
			RunnerState{ExitCode: sigkillExitCode}, VerdictGenuine,
		},
		"a plain non-zero exit is not an eviction": {RunnerState{ExitCode: 1, OOM: OOMNone}, VerdictGenuine},
		"a clean exit is not an eviction":          {RunnerState{OOM: OOMNone}, VerdictGenuine},
	}
	// A log that ends properly, so only the runner state can decide.
	log := "2026-09-12T17:50:33.0000000Z Cleaning up container for job Unit Tests\n" +
		"2026-09-12T17:50:34.0000000Z Job failed\n"
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			finding, err := Classify(Input{Conclusion: ConclusionFailure, Log: log, Runner: tc.runner})
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if finding.Verdict != tc.wantVerdict {
				t.Fatalf("verdict = %q, want %q (signature %q, evidence %q)",
					finding.Verdict, tc.wantVerdict, finding.Signature, finding.Evidence)
			}
		})
	}
}

// TestClassify_RunnerStillReportingBlocksTheVoid proves the runner-state
// corroboration is read: the same truncated log, with the engine positively
// observing that the runner never stopped reporting, is not a dead runner.
func TestClassify_RunnerStillReportingBlocksTheVoid(t *testing.T) {
	log := fixture(t, "runner-died-mid-suite.log")

	dead, err := Classify(Input{
		Conclusion: ConclusionFailure,
		Log:        log,
		Runner:     RunnerState{ReportingKnown: true, Reporting: false},
	})
	if err != nil {
		t.Fatalf("Classify (not reporting): %v", err)
	}
	if dead.Verdict != VerdictVoid || dead.Signature != SignatureRunnerDied {
		t.Fatalf("a runner observed silent over a truncated log: verdict %q signature %q, want void/runner_died",
			dead.Verdict, dead.Signature)
	}

	alive, err := Classify(Input{
		Conclusion: ConclusionFailure,
		Log:        log,
		Runner:     RunnerState{ReportingKnown: true, Reporting: true},
	})
	if err != nil {
		t.Fatalf("Classify (reporting): %v", err)
	}
	if alive.Verdict != VerdictGenuine {
		t.Fatalf("a runner observed still reporting: verdict %q, want genuine", alive.Verdict)
	}
}

// TestClassify_CancelledIsAlwaysVoid covers the conclusion the gate treats as
// a failure today. A cancellation is never the tests' answer, whatever the log
// says, so the rule does not read one.
func TestClassify_CancelledIsAlwaysVoid(t *testing.T) {
	logs := map[string]string{
		"no log":                   "",
		"a host defect in it":      fixture(t, "container-create-failed.log"),
		"a truncated suite in it":  fixture(t, "runner-died-mid-suite.log"),
		"the runner said it ended": "2026-09-12T17:50:34.0000000Z Job failed\n",
	}
	for name, log := range logs {
		t.Run(name, func(t *testing.T) {
			finding, err := Classify(Input{Conclusion: ConclusionCancelled, Log: log})
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if finding.Verdict != VerdictVoid || finding.Signature != SignatureCancelled {
				t.Fatalf("verdict %q signature %q, want void/cancelled", finding.Verdict, finding.Signature)
			}
			if finding.Evidence == "" {
				t.Error("a cancelled finding must still say why it is void")
			}
		})
	}
}

// TestClassify_CancelledYieldsToAVisibleFailure covers the one thing that
// overrules a cancellation: the tests in it had already failed. Stopping a run
// does not unbreak what it broke.
func TestClassify_CancelledYieldsToAVisibleFailure(t *testing.T) {
	finding, err := Classify(Input{
		Conclusion: ConclusionCancelled,
		Log:        fixture(t, "genuine-test-failure.log"),
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if finding.Verdict != VerdictGenuine {
		t.Fatalf("verdict = %q, want genuine (signature %q, evidence %q)",
			finding.Verdict, finding.Signature, finding.Evidence)
	}
}

// TestClassify_HostEvictionOverridesAnUnexplainedRed pins the deliberate
// asymmetry the safety matrix excludes: a positively observed SIGKILL with no
// out-of-memory kill of the job's own is void when the excerpt explains
// nothing, and is NOT void when the excerpt shows the change failing.
//
// Safe because the caller bounds the re-runs: a host that keeps evicting
// answers void every time and escalates to an operator once the budget is
// spent, so the worst case is a bounded number of CI runs rather than a change
// that never reaches its author.
func TestClassify_HostEvictionOverridesAnUnexplainedRed(t *testing.T) {
	evicted := RunnerState{ExitCode: sigkillExitCode, OOM: OOMNone}

	unexplained, err := Classify(Input{
		Conclusion: ConclusionFailure,
		Log:        fixture(t, "genuine-marker-only.log"),
		Runner:     evicted,
	})
	if err != nil {
		t.Fatalf("Classify (unexplained): %v", err)
	}
	if unexplained.Verdict != VerdictVoid || unexplained.Signature != SignatureHostEviction {
		t.Fatalf("an excerpt explaining nothing: verdict %q signature %q, want void/host_eviction",
			unexplained.Verdict, unexplained.Signature)
	}

	explained, err := Classify(Input{
		Conclusion: ConclusionFailure,
		Log:        fixture(t, "genuine-test-failure.log"),
		Runner:     evicted,
	})
	if err != nil {
		t.Fatalf("Classify (explained): %v", err)
	}
	if explained.Verdict != VerdictGenuine {
		t.Fatalf("an excerpt showing the failure: verdict %q, want genuine (evidence %q)",
			explained.Verdict, explained.Evidence)
	}
}

// =============================================================================
// Refusals
// =============================================================================

// TestClassify_RefusesQuestionsItWillNotAnswer covers the error paths: a green
// check and a conclusion this package was never taught.
func TestClassify_RefusesQuestionsItWillNotAnswer(t *testing.T) {
	cases := map[string]Conclusion{
		"a green check":         ConclusionSuccess,
		"an empty conclusion":   "",
		"an unknown verb":       "skipped",
		"a plausible neighbour": "timed_out",
		"mixed case failure":    "FAILURE",
	}
	for name, conclusion := range cases {
		t.Run(name, func(t *testing.T) {
			finding, err := Classify(Input{Conclusion: conclusion, Log: "2026-09-12T17:50:34.0000000Z Job failed\n"})
			if err == nil {
				t.Fatalf("Classify(%q) returned verdict %q and no error", conclusion, finding.Verdict)
			}
			if !errors.Is(err, ErrClassify) {
				t.Errorf("error %v does not unwrap to ErrClassify", err)
			}
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("error %v is not an *InvalidInputError", err)
			}
			if invalid.Field != "conclusion" {
				t.Errorf("Field = %q, want \"conclusion\"", invalid.Field)
			}
			if invalid.Message == "" {
				t.Error("a refusal with no message tells the call site nothing")
			}
			if finding.Verdict != "" {
				t.Errorf("a refused classification returned verdict %q; it must return the zero Finding",
					finding.Verdict)
			}
		})
	}
}
