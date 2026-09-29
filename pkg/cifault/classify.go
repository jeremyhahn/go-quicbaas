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

import "strings"

// =============================================================================
// Classify — the whole of this package's public behaviour
// =============================================================================

// Classify answers what one finished, non-green check means.
//
// PURE. It reads its argument, no clock, no forge and no state, so the same
// inputs always give the same verdict and the fixture corpus under testdata is
// the whole test harness.
//
// It returns an error only for a question it will not answer — a green check,
// a conclusion it has never been taught. Those are call-site bugs, and
// answering them with a plausible verdict would hide the call site forever.
func Classify(in Input) (Finding, error) {
	handler, known := conclusionHandlers[in.Conclusion]
	if !known {
		return Finding{}, &InvalidInputError{
			Field:   "conclusion",
			Message: "no rule covers a conclusion of " + quoted(string(in.Conclusion)) + "; teach this package before asking it",
		}
	}
	view := newLogView(in.Log)
	finding, err := handler(in, view)
	if err != nil {
		return Finding{}, err
	}
	return vetoVoid(finding, view), nil
}

// vetoVoid downgrades a void verdict over a log that shows the change failing.
//
// THE ONE INVARIANT THIS PACKAGE HOLDS ABOVE ITS OWN RULES. Each rule reasons
// from one signature, and a signature can be right about the machine while
// being wrong about the change: a host really did evict a container, a runner
// really did die — and the log still contains a test the change broke. Void
// means "ask the machine again", and asking again will produce that same
// failure, forever, without anyone being told.
//
// So the veto sits AFTER every rule rather than inside each of them. It was
// added because the corpus test found the hole: a genuine test failure on a
// host that also reported an out-of-cgroup SIGKILL classified as an eviction,
// which is precisely the auto-retry-a-broken-change outcome this package
// exists to prevent.
//
// It reads the change's own failure only — a failed test, a panic, a non-zero
// recipe, a workflow error annotation — and NOT the runner's "Job failed"
// line, which a runner writes for any non-zero exit including a SIGKILL and
// which therefore says nothing about why.
func vetoVoid(finding Finding, view logView) Finding {
	if finding.Verdict != VerdictVoid {
		return finding
	}
	line, failed := view.failureEvidence()
	if !failed {
		return finding
	}
	return Finding{Verdict: VerdictGenuine, Signature: SignatureUnrecognised, Evidence: line}
}

// conclusionHandlers dispatches on the check's own terminal answer. A
// conclusion absent from the map is refused rather than guessed at, which is
// why this is a map and not a switch with a default arm.
var conclusionHandlers = map[Conclusion]func(Input, logView) (Finding, error){
	ConclusionSuccess:   refuseGreen,
	ConclusionCancelled: classifyCancelled,
	ConclusionFailure:   classifyFailure,
}

// refuseGreen refuses the one conclusion that is not a failure at all.
func refuseGreen(Input, logView) (Finding, error) {
	return Finding{}, &InvalidInputError{
		Field:   "conclusion",
		Message: "a green check is not a failure; there is nothing to classify",
	}
}

// classifyCancelled answers a cancelled check.
//
// VOID ON ITS OWN FACTS. A cancellation is an act from outside the tests — a
// person, a supersession by a newer head, a runner shutting down — so it is
// never the tests' answer about the change. Today's gate treats it identically
// to a failure and re-delegates to the agent for work that was already
// superseded.
//
// It reads no log, because no log could make a cancellation into a verdict.
// What can still overrule it is vetoVoid: a cancelled run whose log shows a
// test the change broke did break that test, and the cancellation does not
// unbreak it.
func classifyCancelled(Input, logView) (Finding, error) {
	return Finding{
		Verdict:   VerdictVoid,
		Signature: SignatureCancelled,
		Evidence:  "the forge reports this check cancelled; a cancellation is not a verdict on the change",
	}, nil
}

// classifyFailure answers a red check by trying each rule in order.
func classifyFailure(in Input, view logView) (Finding, error) {
	for _, rule := range failureRules {
		if finding, matched := rule(in, view); matched {
			return finding, nil
		}
	}
	// NOTHING MATCHED, SO IT IS THE CHANGE'S. See the package comment: a red
	// this package cannot explain belongs to whoever wrote the change, because
	// the other reading auto-retries a broken change forever.
	return Finding{
		Verdict:   VerdictGenuine,
		Signature: SignatureUnrecognised,
		Evidence:  view.last(),
	}, nil
}

// failureRule examines a red check and answers a Finding when its signature
// is present.
type failureRule func(Input, logView) (Finding, bool)

// failureRules are tried IN ORDER, and the first match decides. The order is
// load-bearing in two places, both learned from the 2026-09-12 corpus:
//
//  1. THE HOST-DEFECT RULES COME FIRST, ahead of ruleRunnerDied. A host that
//     cannot pull the image or create the container never writes an end-of-job
//     marker either — jobs 77520, 77604 and 77625 all stop mid-preamble — so
//     the runner-died rule matches them too, and its verdict is the wrong one:
//     it would re-run them, on the one host that cannot run them, until the
//     budget was gone and nobody had been told.
//
//  2. ruleStatusRacedShutdown COMES AFTER the host-defect rules and before
//     ruleRunnerDied, which it is disjoint from anyway — one needs a marker
//     present, the other needs every marker absent — but stating the order
//     keeps the two from being read as alternatives.
//
//  3. ruleRunnerInfrastructure COMES AFTER the host-defect rules, so a host
//     that also could not pull or create still parks, and BEFORE
//     ruleRunnerDied, so a runner that said why it went away is named for
//     what it said rather than for the silence that followed.
var failureRules = []failureRule{
	ruleRegistryUnreachable,
	ruleContainerCreateFailed,
	ruleModuleResolution,
	ruleRunnerInfrastructure,
	ruleStatusRacedShutdown,
	ruleHostEviction,
	ruleRunnerDied,
}

// registryNeedles are the lines a host that cannot fetch the job image writes.
// Either alone is decisive; both were observed on the same fleet on
// 2026-09-12, the second because the host had no insecure-registries entry for
// the forge's own registry and Docker therefore spoke TLS to a plaintext port.
var registryNeedles = []string{
	"failed to pull image",
	"http response to https client",
}

// ruleRegistryUnreachable answers a job whose image never arrived.
func ruleRegistryUnreachable(_ Input, view logView) (Finding, bool) {
	for _, needle := range registryNeedles {
		if line, found := view.findLine(needle); found {
			return Finding{
				Verdict:   VerdictHostDefect,
				Signature: SignatureRegistryUnreachable,
				Evidence:  line,
			}, true
		}
	}
	return Finding{}, false
}

// containerCreateNeedles are what the OCI runtime writes when it refuses to
// create the job's container. On 2026-09-12 the cause was a host advertising
// lxcfs bind-mounts it did not have installed.
var containerCreateNeedles = []string{
	"oci runtime create failed",
	"runc create failed",
}

// ruleContainerCreateFailed answers a container the runtime would not create.
func ruleContainerCreateFailed(_ Input, view logView) (Finding, bool) {
	for _, needle := range containerCreateNeedles {
		if line, found := view.findLine(needle); found {
			return Finding{
				Verdict:   VerdictHostDefect,
				Signature: SignatureContainerCreateFailed,
				Evidence:  line,
			}, true
		}
	}
	return Finding{}, false
}

// moduleResolutionNeedle is the go toolchain's report that it could not
// resolve a module version.
const moduleResolutionNeedle = "unknown revision"

// ruleModuleResolution answers a PRIVATE module the toolchain could not
// resolve.
//
// THE OWNER TEST IS THE WHOLE RULE, not a refinement of it. The same line on a
// public module is usually a real mistake in the change — a go.mod pinned to a
// tag nobody pushed — and belongs to its author. It is a host defect only for
// a module that no host can resolve without settings an operator provisions,
// which is what Input.PrivateModuleOwners names. Supply no owners and this
// rule never fires, and the failure reaches the agent.
func ruleModuleResolution(in Input, view logView) (Finding, bool) {
	for _, owner := range in.PrivateModuleOwners {
		owner = strings.ToLower(strings.TrimSpace(owner))
		if owner == "" {
			continue
		}
		// BOTH FACTS ON ONE LINE. A checkout's own paths mention the owner on
		// dozens of lines of every log; only the line that also says the
		// toolchain gave up is evidence about a module.
		line, found := view.findLine(moduleResolutionNeedle, "/"+owner+"/")
		if !found {
			continue
		}
		return Finding{
			Verdict:   VerdictHostDefect,
			Signature: SignatureModuleResolution,
			Evidence:  line,
		}, true
	}
	return Finding{}, false
}

// statusMarkerTailLines is how close to the end of the log a SUCCESS marker
// must sit to be read as the runner's own last word.
//
// A WINDOW, BECAUSE THIS RULE IS THE ONE THAT CAN VOID A GENUINE FAILURE. The
// marker is matched whole-line already, but a test that prints exactly "Job
// succeeded" in the middle of a suite it then fails would otherwise void the
// whole run. The runner writes its marker within a few lines of the end —
// three, in the captured instance — so anything further back is somebody
// else's output.
const statusMarkerTailLines = 12

// ruleStatusRacedShutdown answers a log whose own last word is success
// against a conclusion of failure.
//
// Captured on 2026-09-12 (task 67328): the log ends "Job succeeded", then a
// docker ping failing with "context canceled", then "context canceled" — the
// runner was going away as it wrote the status, and the forge recorded the
// failure of the WRITE as the failure of the job.
func ruleStatusRacedShutdown(_ Input, view logView) (Finding, bool) {
	if view.empty() {
		return Finding{}, false
	}
	from := view.tailFrom(statusMarkerTailLines)
	line, succeeded := view.markerIn(from, markerSucceeded)
	if !succeeded {
		return Finding{}, false
	}
	// BOTH MARKERS IN THE WINDOW MEANS GENUINE. Whatever produced a success
	// line, a runner that also says the job failed has said the job failed.
	if _, failed := view.markerIn(from, markerFailed); failed {
		return Finding{}, false
	}
	return Finding{
		Verdict:   VerdictVoid,
		Signature: SignatureStatusRacedShutdown,
		Evidence:  line,
	}, true
}

// ruleHostEviction answers a container the host took away.
//
// THE OOM SCOPE DECIDES, NOT THE EXIT CODE. Exit 137 is SIGKILL and says only
// that something outside the process ended it. Killed for the memory it asked
// for inside its own cgroup, that is the change's appetite and the change's
// failure; killed with no out-of-memory event of its own, the host wanted the
// machine back. With the scope unknown the rule does not fire, because a bare
// 137 is exactly as likely to be a change that allocates without bound.
func ruleHostEviction(in Input, _ logView) (Finding, bool) {
	if in.Runner.ExitCode != sigkillExitCode {
		return Finding{}, false
	}
	if in.Runner.OOM != OOMNone && in.Runner.OOM != OOMHost {
		return Finding{}, false
	}
	return Finding{
		Verdict:   VerdictVoid,
		Signature: SignatureHostEviction,
		Evidence: "the job exited 137 (SIGKILL) and the out-of-memory scope is " +
			quoted(string(in.Runner.OOM)) + ", so nothing inside the job asked for the kill",
	}, true
}

// ruleRunnerDied answers a log that simply stops.
//
// FOUR CONDITIONS, AND EVERY ONE OF THEM IS A GUARD AGAINST VOIDING A REAL
// FAILURE:
//
//   - The log carries at least one line. An unreadable log is not evidence
//     that a runner died; it is evidence of nothing.
//   - No end-of-job marker anywhere in it. A runner writes one whatever the
//     answer, so one present means the runner reached the end and its answer
//     stands — even when the log excerpt is too short to show what failed.
//   - No failure of the change visible in it. A log that already shows a
//     failed test, a panic or a non-zero recipe has something in it for its
//     author to read, and re-running it would only produce the same log.
//   - The runner is not KNOWN to have still been reporting. A runner still
//     sending progress did not die, so the missing marker has some other
//     cause and this rule has no business naming it.
//
// Captured twice on 2026-09-12: job 77459 truncates after "ok
// .../pkg/api/websocket" with zero FAIL lines in 43,787 lines, and job 77461
// truncates in the middle of gosec's package walk. Both were reported red.
func ruleRunnerDied(in Input, view logView) (Finding, bool) {
	if view.empty() {
		return Finding{}, false
	}
	if in.Runner.ReportingKnown && in.Runner.Reporting {
		return Finding{}, false
	}
	if _, ended := view.markerIn(0, markerSucceeded, markerFailed, markerCleanup); ended {
		return Finding{}, false
	}
	if _, failed := view.failureEvidence(); failed {
		return Finding{}, false
	}
	return Finding{
		Verdict:   VerdictVoid,
		Signature: SignatureRunnerDied,
		Evidence: "the log stops at " + quoted(view.last()) +
			" with no end-of-job marker and nothing in it failing",
	}, true
}

// quoted wraps s in double quotes for an error or evidence string.
func quoted(s string) string { return "\"" + s + "\"" }
