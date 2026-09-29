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
	"strings"
	"testing"
)

// greenTail is a job whose steps and post-steps all finished green, up to the
// runner's own cleanup marker. What the runner writes after it is the case.
const greenTail = "2026-09-12T17:47:29.8119539Z Success - Post Checkout code\n" +
	"2026-09-12T17:47:29.8818432Z Cleaning up container for job Unit Tests\n"

// TestRunnerInfraSignatures_EveryRowIsVoidAfterAGreenJob drives every row of
// the table through Classify: each is the runner's failure, not the change's,
// and each names the signature its row carries.
func TestRunnerInfraSignatures_EveryRowIsVoidAfterAGreenJob(t *testing.T) {
	for needle, row := range runnerInfraSignatures {
		t.Run(needle, func(t *testing.T) {
			line := "##[error]" + needle
			for _, also := range row.alsoOnLine {
				line += ": " + also
			}
			finding, err := Classify(Input{
				Conclusion: ConclusionFailure,
				Log:        greenTail + "2026-09-12T17:47:59.8831204Z " + line + "\n2026-09-12T17:47:59.9052876Z Job failed\n",
			})
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if finding.Verdict != VerdictVoid || finding.Signature != row.signature {
				t.Fatalf("finding = %+v, want void / %s", finding, row.signature)
			}
			if !strings.Contains(strings.ToLower(finding.Evidence), needle) {
				t.Errorf("evidence %q does not quote the runner's line", finding.Evidence)
			}
		})
	}
}

// TestRunnerInfraSignatures_AFailedStepStaysGenuine is the other class: a step
// that exited non-zero is the change's answer, whatever the runner did next.
func TestRunnerInfraSignatures_AFailedStepStaysGenuine(t *testing.T) {
	for _, step := range []string{
		"##[error]Process completed with exit code 1.",
		"Process completed with exit code 2.",
		"--- FAIL: TestOpen (0.01s)",
	} {
		t.Run(step, func(t *testing.T) {
			log := "2026-09-12T17:47:20.0000000Z " + step + "\n" + greenTail +
				"2026-09-12T17:47:59.8831204Z ##[error]Error while stop job container: Error response from daemon: remove GITEA-ACTIONS-TASK-73374-x: volume is in use\n" +
				"2026-09-12T17:47:59.9052876Z Job failed\n"
			finding, err := Classify(Input{Conclusion: ConclusionFailure, Log: log})
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if finding.Verdict != VerdictGenuine {
				t.Fatalf("finding = %+v; a failed step was voided and would be re-run forever", finding)
			}
		})
	}
}

// TestRunnerInfraLine_VolumeInUseNeedsTheDaemonsRefusal pins the one row with
// a co-needle: a project's own output mentioning a busy volume is not the
// runner's teardown.
func TestRunnerInfraLine_VolumeInUseNeedsTheDaemonsRefusal(t *testing.T) {
	if _, infra := runnerInfraLine("create_test.go:41: volume is in use, retrying"); infra {
		t.Fatal("a test's own mention of a busy volume was read as the runner's teardown")
	}
	signature, infra := runnerInfraLine("error response from daemon: remove gitea-actions-task-1: volume is in use")
	if !infra || signature != SignatureRunnerTeardown {
		t.Fatalf("runnerInfraLine = %q, %v; want the teardown", signature, infra)
	}
}

// TestFailureLine_ProcessExit reads the exit code rather than the words.
func TestFailureLine_ProcessExit(t *testing.T) {
	for line, want := range map[string]bool{
		"##[error]process completed with exit code 1.":  true,
		"process completed with exit code 137.":         true,
		"process completed with exit code 0.":           false,
		"process completed with exit code 0":            false,
		"process completed with exit code .":            false,
		"##[error]error while stop job container: boom": false,
	} {
		if got := failureLine(line); got != want {
			t.Errorf("failureLine(%q) = %v, want %v", line, got, want)
		}
	}
}

// TestRunnerInfraNeedles_AreTheWholeTableInOrder guards the determinism a map
// cannot give by itself.
func TestRunnerInfraNeedles_AreTheWholeTableInOrder(t *testing.T) {
	if len(runnerInfraNeedles) != len(runnerInfraSignatures) {
		t.Fatalf("%d needles for %d rows", len(runnerInfraNeedles), len(runnerInfraSignatures))
	}
	for i := 1; i < len(runnerInfraNeedles); i++ {
		if runnerInfraNeedles[i-1] >= runnerInfraNeedles[i] {
			t.Fatalf("needles out of order at %d: %q, %q", i, runnerInfraNeedles[i-1], runnerInfraNeedles[i])
		}
	}
	for needle := range runnerInfraSignatures {
		if needle != strings.ToLower(needle) {
			t.Errorf("needle %q is not lower-case; it would never match a lower-cased line", needle)
		}
	}
}
