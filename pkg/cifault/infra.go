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
	"sort"
	"strings"
)

// =============================================================================
// Runner infrastructure — the runner's own failures, in one table
// =============================================================================
//
// A runner writes its own errors into the job log in the same annotation the
// change's failures use, "##[error]". Read as a failure of the change, the
// teardown error of go-luks2 PR #203 (2026-09-22) — every step and post-step
// green, then "Error while stop job container: … volume is in use" — vetoed
// every void rule and sent a docs-only change back to its worker, who spent a
// round proving the diff was fine before the task escalated to the owner.
//
// So the runner's own lines are named HERE, once, and read two ways: as the
// signature of ruleRunnerInfrastructure, and as lines failureLine refuses to
// count as the change's failure. A new signature is a new row, never a new
// conditional somewhere else.

// runnerInfraSignature is one row of the table: the signature a needle names,
// and any further text that must sit on the SAME line for the needle to count.
type runnerInfraSignature struct {
	signature  Signature
	alsoOnLine []string
}

// runnerInfraSignatures maps a lower-cased needle onto what it means. Every
// needle is text a runner or its container daemon writes about itself, never
// text a build or a test prints about the change.
var runnerInfraSignatures = map[string]runnerInfraSignature{
	// act_runner, after "Cleaning up container for job …": the daemon did
	// not remove the job container in time, then refused because a volume
	// was still attached. Captured on go-luks2 PR #203.
	"error while stop job container":                       {signature: SignatureRunnerTeardown},
	"timed out waiting for the daemon to remove container": {signature: SignatureRunnerTeardown},
	// Alone, "volume is in use" is ordinary Docker vocabulary a project's own
	// tests can print; only the daemon's refusal to REMOVE is the teardown.
	"volume is in use": {
		signature:  SignatureRunnerTeardown,
		alsoOnLine: []string{"error response from daemon: remove"},
	},
	// The runner service stopping, or losing the server, under a job.
	"the runner has received a shutdown signal": {signature: SignatureRunnerLost},
	"lost communication with the server":        {signature: SignatureRunnerLost},
}

// runnerInfraNeedles is the table's keys in a fixed order, so a line two
// needles match always answers the same signature.
var runnerInfraNeedles = func() []string {
	needles := make([]string, 0, len(runnerInfraSignatures))
	for needle := range runnerInfraSignatures {
		needles = append(needles, needle)
	}
	sort.Strings(needles)
	return needles
}()

// runnerInfraLine reports whether one lower-cased line is the runner's own
// failure, and which.
func runnerInfraLine(lower string) (Signature, bool) {
	for _, needle := range runnerInfraNeedles {
		if !strings.Contains(lower, needle) {
			continue
		}
		row := runnerInfraSignatures[needle]
		matched := true
		for _, also := range row.alsoOnLine {
			if !strings.Contains(lower, also) {
				matched = false
				break
			}
		}
		if matched {
			return row.signature, true
		}
	}
	return "", false
}

// ruleRunnerInfrastructure answers a red the runner caused after, or around,
// a job that did not fail.
//
// VOID ONLY WHEN NOTHING ELSE IN THE LOG FAILED. The runner's line says the
// machine misbehaved; it does not say the steps passed. A step that ended
// "Process completed with exit code N" is the change's answer whatever the
// runner did afterwards, and re-running it would only reproduce it — so the
// failure evidence, which never counts the runner's own lines, decides.
func ruleRunnerInfrastructure(_ Input, view logView) (Finding, bool) {
	if view.empty() {
		return Finding{}, false
	}
	for i := len(view.lower) - 1; i >= 0; i-- {
		signature, infra := runnerInfraLine(view.lower[i])
		if !infra {
			continue
		}
		if _, failed := view.failureEvidence(); failed {
			return Finding{}, false
		}
		return Finding{
			Verdict:   VerdictVoid,
			Signature: signature,
			Evidence:  boundEvidence(view.lines[i]),
		}, true
	}
	return Finding{}, false
}
