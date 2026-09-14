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
	"strings"
	"testing"
)

// TestVerdict_Names covers the string forms the engine records and an
// operator reads.
func TestVerdict_Names(t *testing.T) {
	if VerdictGenuine.String() != "genuine" || VerdictVoid.String() != "void" ||
		VerdictHostDefect.String() != "host_defect" {
		t.Fatalf("verdict names drifted: %q %q %q",
			VerdictGenuine, VerdictVoid, VerdictHostDefect)
	}
	if SignatureRunnerDied.String() != "runner_died" || SignatureUnrecognised.String() != "unrecognised" {
		t.Fatalf("signature names drifted: %q %q", SignatureRunnerDied, SignatureUnrecognised)
	}
	if ConclusionFailure.String() != "failure" || ConclusionCancelled.String() != "cancelled" {
		t.Fatalf("conclusion names drifted: %q %q", ConclusionFailure, ConclusionCancelled)
	}
}

// TestVerdict_UnknownValuesStillRender covers the bad-input half: a verdict,
// signature or conclusion this package never produced must still render,
// because these end up in logs and a panic there helps nobody.
func TestVerdict_UnknownValuesStillRender(t *testing.T) {
	if got := Verdict("invented").String(); got != "invented" {
		t.Errorf("Verdict.String() = %q", got)
	}
	if got := Signature("").String(); got != "" {
		t.Errorf("Signature.String() = %q", got)
	}
	if got := Conclusion("timed_out").String(); got != "timed_out" {
		t.Errorf("Conclusion.String() = %q", got)
	}
}

// TestVerdict_ReachesAgent pins which single verdict wakes a coding agent.
func TestVerdict_ReachesAgent(t *testing.T) {
	if !VerdictGenuine.ReachesAgent() {
		t.Error("a genuine failure must reach the agent")
	}
	for _, verdict := range []Verdict{VerdictVoid, VerdictHostDefect, Verdict(""), Verdict("invented")} {
		if verdict.ReachesAgent() {
			t.Errorf("verdict %q must not reach the agent", verdict)
		}
	}
}

// TestAggregate_PrecedenceAcrossChecks covers the reduction of many failed
// checks on one head to the single action the round takes.
func TestAggregate_PrecedenceAcrossChecks(t *testing.T) {
	cases := map[string]struct {
		findings []Finding
		want     Verdict
	}{
		"every check void": {
			[]Finding{{Verdict: VerdictVoid}, {Verdict: VerdictVoid}}, VerdictVoid,
		},
		"a host defect among void ones": {
			[]Finding{{Verdict: VerdictVoid}, {Verdict: VerdictHostDefect}, {Verdict: VerdictVoid}},
			VerdictHostDefect,
		},
		"one real failure among a fleet of faults": {
			[]Finding{{Verdict: VerdictVoid}, {Verdict: VerdictHostDefect}, {Verdict: VerdictGenuine}},
			VerdictGenuine,
		},
		"a single genuine check": {
			[]Finding{{Verdict: VerdictGenuine}}, VerdictGenuine,
		},
		"a single host defect": {
			[]Finding{{Verdict: VerdictHostDefect}}, VerdictHostDefect,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Aggregate(tc.findings); got != tc.want {
				t.Fatalf("Aggregate = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAggregate_BadInputFailsTowardsTheAgent covers the bad-input half. No
// findings, a nil slice, or a verdict this package never produced must all
// reach the agent rather than quietly becoming an automatic re-run.
func TestAggregate_BadInputFailsTowardsTheAgent(t *testing.T) {
	cases := map[string][]Finding{
		"nil":                     nil,
		"empty":                   {},
		"one unset verdict":       {{}},
		"an invented verdict":     {{Verdict: Verdict("infrastructure-probably")}},
		"an invented beside void": {{Verdict: VerdictVoid}, {Verdict: Verdict("")}},
	}
	for name, findings := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Aggregate(findings); got != VerdictGenuine {
				t.Fatalf("Aggregate = %q, want genuine: an unreadable finding must not void a check", got)
			}
		})
	}
}

// TestInvalidInputError_Reports covers the error's message and its sentinel.
func TestInvalidInputError_Reports(t *testing.T) {
	err := &InvalidInputError{Field: "conclusion", Message: "a green check is not a failure"}
	if !strings.Contains(err.Error(), "conclusion") ||
		!strings.Contains(err.Error(), "a green check is not a failure") {
		t.Fatalf("Error() = %q, want it to name the field and the reason", err.Error())
	}
	if !errors.Is(err, ErrClassify) {
		t.Error("the error does not unwrap to ErrClassify")
	}
}

// TestInvalidInputError_EmptyFieldsStillRender covers the bad-input half: an
// error built with nothing in it must still be an error, not a panic.
func TestInvalidInputError_EmptyFieldsStillRender(t *testing.T) {
	err := &InvalidInputError{}
	if err.Error() == "" {
		t.Fatal("an error with an empty message rendered as the empty string")
	}
	if !errors.Is(err, ErrClassify) {
		t.Error("the error does not unwrap to ErrClassify")
	}
}
