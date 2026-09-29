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

// =============================================================================
// The vocabulary: verdict, signature, and the facts a verdict is read from
// =============================================================================

// Verdict says what a red check means, and therefore who has to act on it.
type Verdict string

const (
	// VerdictGenuine is the change's own failure. It reaches the coding agent,
	// which is what a red check has always done. It is also the DEFAULT: see
	// the package comment for why every other verdict has to be earned.
	VerdictGenuine Verdict = "genuine"

	// VerdictVoid is not a verdict on the change at all — a runner died
	// holding the job, a status write raced the runner's shutdown, the host
	// evicted the container. The check is re-run and no agent is woken.
	//
	// The caller BOUNDS the re-runs. A verdict that says "ask the machine
	// again" is only safe while the asking stops: a permanently broken host
	// answers void every time, and an unbounded loop would spend CI forever
	// and tell nobody.
	VerdictVoid Verdict = "void"

	// VerdictHostDefect is the machine, not the change: an unreachable
	// registry, a container the runtime cannot create, a module-resolution
	// environment that is not there. An operator is told and no agent is
	// woken, because no change an agent could author would fix it.
	VerdictHostDefect Verdict = "host_defect"
)

// String returns the verdict's name.
func (v Verdict) String() string { return string(v) }

// ReachesAgent reports whether this verdict is one a coding agent is woken
// for. Exactly one is.
func (v Verdict) ReachesAgent() bool { return v == VerdictGenuine }

// verdictPrecedence ranks the verdicts for Aggregate, highest wins.
//
// GENUINE OUTRANKS EVERYTHING. One real test failure among a dozen
// infrastructure faults is still a real test failure and must reach the
// agent; suppressing it because the fleet was also unwell would merge — or
// endlessly re-run — a broken change.
//
// HOST DEFECT OUTRANKS VOID, because a host that cannot start a container
// will not start one by being asked again, and the re-run budget spent
// finding that out is time an operator was not told.
var verdictPrecedence = map[Verdict]int{
	VerdictVoid:       1,
	VerdictHostDefect: 2,
	VerdictGenuine:    3,
}

// Signature names the mechanically identifiable pattern a verdict rests on.
//
// It is reported alongside the verdict because an operator reading "host
// defect" needs to know WHICH defect, and because a classification nobody can
// trace is a classification nobody will trust.
type Signature string

const (
	// SignatureUnrecognised is a red this package has no rule for. It pairs
	// with VerdictGenuine: an unexplained failure is the change's own.
	SignatureUnrecognised Signature = "unrecognised"

	// SignatureCancelled is a check the forge reports as cancelled. A
	// cancellation is an act from outside — a human, a supersession, a runner
	// shutting down — and never the tests' own answer about the change.
	SignatureCancelled Signature = "cancelled"

	// SignatureRunnerDied is a log that stops mid-job: no end-of-job marker
	// anywhere in its tail, and no failure of the change visible in it either.
	SignatureRunnerDied Signature = "runner_died"

	// SignatureStatusRacedShutdown is a log whose own last word is that the
	// job SUCCEEDED, against a conclusion of failure. The status write lost a
	// race with the runner's shutdown; the job did not fail.
	SignatureStatusRacedShutdown Signature = "status_raced_shutdown"

	// SignatureHostEviction is a SIGKILL (exit 137) with no out-of-memory
	// kill inside the job's own cgroup: the host took the container away.
	SignatureHostEviction Signature = "host_eviction"

	// SignatureRegistryUnreachable is a job image that could not be pulled.
	SignatureRegistryUnreachable Signature = "registry_unreachable"

	// SignatureContainerCreateFailed is a container the OCI runtime refused
	// to create — a missing bind-mount source, a cgroup, a ulimit.
	SignatureContainerCreateFailed Signature = "container_create_failed"

	// SignatureModuleResolution is a private module the toolchain could not
	// resolve, which on this fleet means the host was provisioned without the
	// credentials or proxy settings that resolve it.
	SignatureModuleResolution Signature = "module_resolution"

	// SignatureRunnerTeardown is a job whose every step and post-step
	// finished green and which the runner then failed while tearing its own
	// job container down — a daemon that timed out removing it, a volume
	// still in use (go-quicbaas#1011).
	SignatureRunnerTeardown Signature = "runner_teardown"

	// SignatureRunnerLost is a runner that announced it was going away, or
	// lost the server, while it held the job.
	SignatureRunnerLost Signature = "runner_lost"
)

// String returns the signature's name.
func (s Signature) String() string { return string(s) }

// Conclusion is a FINISHED check's own terminal answer, as the forge reports
// it.
//
// This package's own vocabulary rather than the forge module's, and
// deliberately so: the classifier must be usable — and testable — with no
// forge, no adapter and no transport anywhere in the build.
type Conclusion string

const (
	// ConclusionSuccess is a green check. Classify refuses it.
	ConclusionSuccess Conclusion = "success"

	// ConclusionFailure is a red check.
	ConclusionFailure Conclusion = "failure"

	// ConclusionCancelled is a check stopped before it could answer.
	ConclusionCancelled Conclusion = "cancelled"
)

// String returns the conclusion's name.
func (c Conclusion) String() string { return string(c) }

// OOMScope says where the kernel's out-of-memory killer acted, if it did.
//
// The distinction is the whole of the exit-137 rule. A SIGKILL is the same
// signal whether the change ate the memory or the host wanted the machine
// back, and those are opposite verdicts, so the scope is carried explicitly
// rather than inferred.
type OOMScope string

const (
	// OOMUnknown is the zero value: nothing was observed. It earns no
	// verdict, which is why exit 137 alone does not void a check.
	OOMUnknown OOMScope = ""

	// OOMNone is a positive observation that no out-of-memory kill happened
	// in the job's cgroup.
	OOMNone OOMScope = "none"

	// OOMJob is an out-of-memory kill INSIDE the job's own cgroup: the
	// change's own appetite, and the change's own failure.
	OOMJob OOMScope = "job"

	// OOMHost is an out-of-memory kill elsewhere on the host: the job was
	// collateral.
	OOMHost OOMScope = "host"
)

// RunnerState is what the engine knows about the machine that executed a
// check, beyond what the check's own log says.
//
// THE ZERO VALUE MEANS "NOTHING IS KNOWN" AND EARNS NO VERDICT. Every field
// is either a positive observation or guarded by one, because a classifier
// that read an unset bool as evidence would void checks on the strength of a
// caller forgetting to fill in a struct.
type RunnerState struct {
	// Reporting says whether the runner was still sending progress when the
	// check reached its conclusion. Read only when ReportingKnown is set.
	Reporting bool

	// ReportingKnown says whether Reporting was observed at all.
	ReportingKnown bool

	// ExitCode is the job process's exit status; zero when unknown, which is
	// indistinguishable from a clean exit and safe either way — a clean exit
	// matches no rule here.
	ExitCode int

	// OOM says where an out-of-memory kill happened, when one did.
	OOM OOMScope
}

// sigkillExitCode is 128 + SIGKILL, the exit status a container reports when
// something outside it decided it should stop existing.
const sigkillExitCode = 137

// Input is everything Classify reads about one finished check.
type Input struct {
	// Conclusion is the check's terminal answer. Required.
	Conclusion Conclusion

	// Log is the TAIL of the job's log, as the forge served it.
	//
	// A TAIL, AND THE RULES DEPEND ON IT BEING ONE. "No end-of-job marker
	// anywhere in this text" only means "the runner never reached the end of
	// the job" when the text runs to where the log stops. Handing this the
	// HEAD of a log would void every check whose run was longer than the
	// excerpt.
	//
	// Empty is allowed and answers genuine: a log that could not be read is
	// no evidence of a runner fault.
	Log string

	// Runner is what the engine knows about the machine that ran the check.
	// The zero value is "nothing known" and is always safe.
	Runner RunnerState

	// PrivateModuleOwners are the forge owners whose Go modules this project
	// resolves through private credentials — in practice the project's own
	// owner, whose other repositories it depends on.
	//
	// It is what makes the module-resolution signature a HOST defect rather
	// than the change's. "unknown revision v0.2.0-alpha" on a public module
	// is very often a real mistake in the change — a tag that was never
	// pushed — and must reach its author. On a module only this fleet's hosts
	// can resolve, the same line means the host is missing the settings that
	// resolve it. With no owners supplied the rule cannot fire, and the
	// failure reaches the agent.
	PrivateModuleOwners []string
}

// Finding is the classifier's answer about one finished check.
type Finding struct {
	// Verdict is what the red means.
	Verdict Verdict

	// Signature is the pattern the verdict rests on.
	Signature Signature

	// Evidence is the log line — or the stated fact, where no line says it —
	// the verdict rests on, so an operator can check the classifier's work.
	//
	// It is quoted from a job log and is therefore untrusted text. Callers
	// that publish it redact it first; this package does not, because it does
	// not own the redaction policy.
	Evidence string
}

// Aggregate reduces the findings of every failed check on one head to the one
// action the round takes. It answers VerdictGenuine for no findings at all,
// which is the safe reading of "something was red and nothing explained it".
func Aggregate(findings []Finding) Verdict {
	if len(findings) == 0 {
		return VerdictGenuine
	}
	worst := VerdictVoid
	for _, finding := range findings {
		verdict := finding.Verdict
		if _, known := verdictPrecedence[verdict]; !known {
			// A verdict this package never produced is not evidence of
			// anything, and the unsafe reading of "not evidence" is void.
			verdict = VerdictGenuine
		}
		if verdictPrecedence[verdict] > verdictPrecedence[worst] {
			worst = verdict
		}
	}
	return worst
}
