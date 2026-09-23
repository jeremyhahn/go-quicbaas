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

package orchestration

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// =============================================================================
// A fault in the environment is not an attempt at the work
// =============================================================================
//
// THE ATTEMPT BUDGET IS A JUDGEMENT ABOUT THE WORK, AND A JOB THAT NEVER
// REACHED A MODEL MADE NO SUCH JUDGEMENT.
//
// A task's budget exists to stop the engine retrying work it cannot do. It
// answers "has enough been tried" — and a job that died because the pool had
// no credential to hand its agent, or because the agent could not reach the
// API, has not tried anything. Charging it is an accounting mistake: a task
// that quietly lost two of three attempts to a configuration error exhausts
// itself on the first real difficulty.
//
// TWO PROPERTIES FOLLOW:
//
//  1. A FAULT DOES NOT SPEND THE BUDGET. Once the condition is resolved and
//     the task runs again, it has every attempt it started with.
//  2. WHETHER IT PARKS DEPENDS ON WHAT WAS PROVEN. Only the resolver's own
//     faults park on sight — each is a fact about the deployment, read
//     directly, that no retry changes. Everything else requeues, because an
//     `api_error` with zero usage is a missing credential OR a 5xx and nothing
//     here separates them; parking the ambiguous case puts a human in front of
//     work that may run on the next sweep. See faultRequeues.
//
// The retry is BOUNDED (MaxConsecutiveEnvironmentFaults), because a retry that
// never succeeds is the same hidden work seen from the other side: cheap per
// iteration, and endless. The bound is what makes preferring retry safe.
//
// AND IT IS CLASSIFIED FROM STRUCTURE, NEVER FROM PROSE. Everything below
// matches a typed error or a typed field: the resolver's own errors, the lease
// store's own errors, and the agent's own terminal reason plus its own token
// accounting (PreFirstTurnFault). Nothing greps a message. A class inferred
// from free text is a guess, and this one decides whether a task spends its
// budget.

// EnvironmentFault names a job failure that happened to the environment
// rather than to the work.
type EnvironmentFault struct {
	// Class is the fault's kind: the agent's own terminal reason for a
	// pre-first-turn failure, or one of the classes below.
	Class string

	// Reason is what the block says, written for somebody who was not
	// watching.
	Reason string

	// RetryNotBefore is when the task should be looked at again, or the zero
	// time when nothing can say.
	//
	// Set only by classes that can name a moment: a pool held until a
	// stated time, a project whose capacity frees when a lease ages out.
	// Every other class either needs a human (so there is no moment to name)
	// or is an agent failure whose cause is unknown, where guessing a wait
	// would be inventing a fact.
	RetryNotBefore time.Time
}

// The classes this package itself produces. The agent's classes are its own
// terminal reasons and are not enumerated here — the agent owns that
// vocabulary, and flattening it into a set of our own would drop any reason it
// adds.
const (
	// FaultNoProviderAccount is a tenant/project with no eligible pooled
	// account for the backend's provider. A configuration fault: the pool is
	// empty, or every account in it is revoked, errored or on a limit hold.
	FaultNoProviderAccount = "no_provider_account"

	// FaultNoAccountResolver is a deployment whose account pool was never
	// wired at all — no secrets manager, so no resolver.
	FaultNoAccountResolver = "no_account_resolver"

	// FaultAccountCredentialMissing is an account the pool selected whose
	// secret envelope is not in the secrets backend: the metadata says the
	// account exists and the credential does not.
	FaultAccountCredentialMissing = "account_credential_missing"

	// FaultAccountRefresherMissing is an account whose refresh policy needs a
	// token refresher no provider registered.
	FaultAccountRefresherMissing = "account_refresher_missing"

	// FaultAccountsCoolingDown is a pool that HAS accounts for this provider
	// and has none free right now: every candidate is on a limit hold that
	// has not lapsed.
	//
	// THE ONE FAULT THAT IS NOT A FAULT. Nothing is misconfigured and nobody
	// needs to act; the rotation is doing what it exists to do. It is
	// classified at all so the attempt is not charged for a job that started
	// no process, and it requeues rather than parking.
	FaultAccountsCoolingDown = "accounts_cooling_down"

	// FaultProjectCapacityExceeded is a project whose concurrency budget was
	// already held when the job asked for its lease. The job started no
	// session; the attempt is refunded and the task waits for a lease to
	// end.
	FaultProjectCapacityExceeded = "project_capacity_exceeded"

	// FaultAccountQuotaExhausted is a pooled account whose metered spend
	// allowance for a window is gone. Nothing is misconfigured: the operator
	// set a cap and it worked. It requeues, because a sliding window frees
	// up on its own, and the refusal carries the moment it does.
	FaultAccountQuotaExhausted = "account_quota_exhausted"

	// FaultAccountBreakerTripped is a pooled account taken out of service by
	// its own failure history — either its cooldown is running, or it is
	// half-open and another caller holds the single trial.
	//
	// It requeues rather than parking. A tripped breaker is a statement
	// about the PROVIDER's recent behavior, not about this deployment's
	// configuration, and the account returns by itself when a trial
	// succeeds.
	FaultAccountBreakerTripped = "account_breaker_tripped"

	// FaultForgeUnreachable is a forge that did not answer a call the job
	// needed: no response inside the call's deadline, a dropped connection,
	// or a gateway reporting the forge behind it unavailable, still so after
	// the adapter's own bounded retry and state reconciliation.
	//
	// IT IS THE FORGE'S HOST, NOT THE WORK. Measured on the burn-in node: a
	// Gitea under CI load missed the client deadline on a listing, a pull
	// request read and a merge, and answered in ~70 ms moments later — and
	// the merge had in fact landed. Charging those attempts spent the task's
	// budget on the forge's load. It requeues, bounded like every other
	// requeueing class, because a loaded forge recovers on its own.
	FaultForgeUnreachable = "forge_unreachable"
)

// ErrForgeUnreachable reports a forge call the forge never answered.
//
// The forge adapters live above this package, so they cannot be named here;
// their transport error unwraps to this sentinel instead, which is the one
// fact classifyEnvironmentFault needs.
var ErrForgeUnreachable = errors.New("orchestration: the forge did not answer")

// =============================================================================
// Operator faults — a factory that could not present the work
// =============================================================================
//
// A FAULT IN THE FACTORY IS NOT AN ATTEMPT AT THE WORK EITHER, and it arrives
// by a different door from the classes above. Those are read off a typed
// error on the dispatch path, before a runner exists. These are the runner's
// OWN judgement, made while it holds the job: the project has nowhere to
// check out, the checkout a worker was about to be prompted on is not the one
// the spec described, or the worker itself reported that its workspace or
// tooling made the work impossible.
//
// THE JUDGEMENT TRAVELS ON THE PARK, because the park is the only thing a
// runner can say that outranks the failure accounting (see applyConclusion),
// and a runner that parked for an operator has already proven the condition no
// retry changes. The class is what turns that park from "the engine gave up"
// into "the engine gave up, and here is whose fault it was": the attempt is
// refunded, the block names the class an operator reads, and re-labelling the
// issue after the repair releases the task source the way every other
// environment hold does (taskSourceReleasable).
//
// THEY ARE NOT IN classifyEnvironmentFault AND MUST NOT BE. That function
// reads a typed error, and nothing it can be handed proves a workspace is
// unfit — the proof is a probe a runner ran inside a container, or a
// declaration a worker made. Inferring either from an error string is the one
// thing this whole file refuses to do.

const (
	// FaultWorkspaceUnfit is a workspace a runner refused BEFORE PROMPTING
	// ANYTHING: the project names no workspace path, the daemon could not
	// make the container's mount point under it, or the pre-flight found the
	// checkout absent, empty, holding a different repository, or missing a
	// context repository the project declared.
	//
	// Proven operator-actionable, and proven at zero token cost — which is
	// the whole point of proving it there rather than letting a model
	// discover it (go-quicbaas#908).
	FaultWorkspaceUnfit = "workspace_unfit"

	// FaultWorkerEnvironment is a worker that stopped because its own
	// workspace or tooling made the work impossible, and said so: the
	// `environment` stop code, or a stop code this binary does not know.
	//
	// It is the worker's declaration rather than a probe, and it is trusted
	// for the same reason the park is: the alternative is charging an
	// attempt to a worker that correctly refused to invent a change.
	FaultWorkerEnvironment = "worker_environment"

	// FaultCIHostDefect is a CI host that could not run the job at all: a
	// registry it cannot pull the image from, a container the runtime refuses
	// to create, a module-resolution environment the runner never loaded.
	//
	// It is an operator fault and must be registered as one. An unregistered
	// class is read as a failure of the WORK by design, which would charge the
	// attempt and re-delegate to a coding agent -- the exact 1.5M-token
	// mistake go-quicbaas#898 exists to end.
	FaultCIHostDefect = "ci_host_defect"

	// FaultAgentUnreachable is a spawn the control plane could not deliver
	// because the agent daemon did not answer at all.
	//
	// THE ONE FAULT THE AGENT CANNOT REPORT ABOUT ITSELF. Every other spawn
	// fault reaches the control plane as SpawnFaultEvidence attached by the
	// agent to its gRPC status, and wrapRemoteSpawnFault rebuilds the class
	// from it. An agent that is not listening attaches nothing, so the failure
	// arrived as a bare Unavailable, matched no class, and was charged as a
	// failure of the WORK — which is the opposite of the truth: no session was
	// created, no container started and no token spent.
	//
	// Measured on the burn-in node during a deploy: three tasks were dispatched
	// in the window between the daemon accepting work and the agent finishing
	// registration, each was dispatched twice, and all three went from 4 of 6
	// attempts to 6 of 6 and failed. Nothing about the work was wrong.
	FaultAgentUnreachable = "agent_unreachable"
)

// operatorFaultClasses are the classes that attribute a failure to the
// operator's deployment rather than to the work. Written as a set so a park
// carrying a class nothing produces can be REFUSED rather than believed.
var operatorFaultClasses = map[string]bool{
	FaultWorkspaceUnfit:    true,
	FaultWorkerEnvironment: true,
	FaultCIHostDefect:      true,
	FaultAgentUnreachable:  true,
}

// ParkAwaitsAnAnswer reports whether a park is waiting on a PERSON rather than
// recording a judgement about the work, and is therefore refunded.
//
// ONLY needs_info. The other two parks are deliberately excluded:
// TaskBlockKindBlocked waits on an external CONDITION, which can clear on its
// own and whose park already carries a fault class when the factory caused it;
// and TaskBlockKindEscalated is reached through paths that DO include the work
// having failed. Widening this predicate would start refunding those, so it
// names the one kind whose whole meaning is "a human has been asked something".
func ParkAwaitsAnAnswer(kind TaskBlockKind) bool {
	return kind == TaskBlockKindNeedsInfo
}

// OperatorFaultClass reports whether class blames the factory rather than the
// work, and is therefore refunded rather than charged.
//
// THE FALSE ANSWER IS THE SAFE ONE, deliberately. An unknown class — a typo, a
// record written by a newer binary — is read as a failure of the work: the
// attempt is charged, the budget bounds the retries, and a person sees the
// task stop. Answering true on a class nothing recognised would refund every
// attempt of a genuinely broken change and retry it for ever.
func OperatorFaultClass(class string) bool {
	return operatorFaultClasses[class]
}

// resolverFaultClasses are the classes classifyEnvironmentFault can name that
// did not come from an agent. Written as a set so a caller can REFUSE a class
// nothing produces.
var resolverFaultClasses = map[string]bool{
	FaultNoProviderAccount:        true,
	FaultNoAccountResolver:        true,
	FaultAccountCredentialMissing: true,
	FaultAccountRefresherMissing:  true,
	FaultAccountsCoolingDown:      true,
	FaultProjectCapacityExceeded:  true,
	FaultAccountQuotaExhausted:    true,
	FaultAccountBreakerTripped:    true,
	FaultForgeUnreachable:         true,
}

// KnownFaultClass reports whether class is one this engine can actually
// produce: a dispatch-path fault classifyEnvironmentFault names, an operator
// fault a runner parks with, or an agent's own environmental terminal reason.
// The agent half is asked of the terminal-reason table rather than copied, so
// the two cannot disagree.
func KnownFaultClass(class string) bool {
	return resolverFaultClasses[class] || operatorFaultClasses[class] ||
		IsEnvironmentalTerminalReason(class)
}

// faultClassMeanings say, in one clause, what each of this package's fault
// classes means to somebody who was not watching.
//
// THEY LIVE BESIDE THE CLASSES THEY EXPLAIN. The forge comment (see
// pkg/sdlc/blocked.go) is where they are read, but a copy of this table over
// there would be the copy that goes stale the next time a class is added —
// and a class with no meaning renders as a bare identifier on an issue a
// person is waiting on.
//
// A map rather than a switch so the set can be ranged over: a test asserts
// that every class in resolverFaultClasses has one, which is the only way the
// two stay in step.
var faultClassMeanings = map[string]string{
	FaultNoProviderAccount:        "no pooled provider account was eligible to run this work",
	FaultNoAccountResolver:        "this deployment has no provider account pool wired at all",
	FaultAccountCredentialMissing: "the selected account's credential is missing from the secrets backend",
	FaultAccountRefresherMissing:  "the selected account's credential could not be renewed",
	FaultAccountsCoolingDown:      "every eligible account is on a provider rate-limit cooldown",
	FaultProjectCapacityExceeded:  "the project's concurrency budget was already fully held",
	FaultAccountQuotaExhausted:    "the account's configured spend cap for the window is spent",
	FaultAccountBreakerTripped:    "the account's circuit breaker took it out of service after repeated failures",
	FaultForgeUnreachable:         "the forge did not answer a call the job needed, even after bounded retries",
	FaultWorkspaceUnfit: "the workspace the work needed was not fit: no checkout, an empty one, " +
		"or a declared context repository that is not there",
	FaultWorkerEnvironment: "the worker reported that its own workspace or tooling made the work impossible",
	FaultCIHostDefect: "a CI host could not run the job at all: an image it could not pull, a container " +
		"the runtime refused to create, or a module-resolution environment it never loaded",
	FaultAgentUnreachable: "the agent daemon did not answer the spawn, so the work never started",
}

// FaultClassMeaning returns the one-clause explanation of a fault class, and
// whether this package has one. An unknown class — an agent's own terminal
// reason, or one added and not explained — reports false rather than an
// invented sentence.
func FaultClassMeaning(class string) (string, bool) {
	meaning, ok := faultClassMeanings[class]
	return meaning, ok
}

// faultsNeedingAHuman are the classes that park on their first occurrence.
//
// Written as the PARKING set rather than the requeueing one, deliberately. The
// requeueing side is open — it takes every agent reason judged environmental,
// and that set may grow — while this side is closed and entirely ours. A class
// added to classifyEnvironmentFault and forgotten here requeues, which is
// bounded and visible; forgotten in the other direction it would park silently
// forever.
var faultsNeedingAHuman = map[string]bool{
	FaultNoProviderAccount:        true,
	FaultNoAccountResolver:        true,
	FaultAccountCredentialMissing: true,
	FaultAccountRefresherMissing:  true,
}

// faultRequeues reports whether a fault of this class puts the task back in
// the queue rather than parking it for a human.
//
// THE TEST IS "IS THIS PROVEN OPERATOR-ACTIONABLE". Only the resolver's own
// faults park on sight, and they earn it: each is a statement about
// CONFIGURATION, read directly before any process started. Everything else
// requeues, BOUNDED.
func faultRequeues(class string) bool {
	return !faultsNeedingAHuman[class]
}

// classifyEnvironmentFault reports whether a job failed on the environment
// rather than on the work, and names the fault.
//
// ONE PATH, called from exactly one place — where a loop's error becomes a
// job's failure class. The false answer is the ordinary failed attempt:
// charged, retried if the budget allows.
func classifyEnvironmentFault(err error) (EnvironmentFault, bool) {
	if err == nil {
		return EnvironmentFault{}, false
	}

	switch {
	// FIRST, BECAUSE AccountsCoolingDownError UNWRAPS TO BOTH SENTINELS. It
	// deliberately still satisfies errors.Is(ErrNoAccountAvailable), so the
	// narrower test has to come first or the cooldown would be answered as an
	// empty pool and parked.
	case errors.Is(err, ErrAccountsCoolingDown):
		return EnvironmentFault{
			Class:          FaultAccountsCoolingDown,
			Reason:         coolingDownReason(err),
			RetryNotBefore: coolingDownUntil(err),
		}, true

	case errors.Is(err, ErrNoAccountAvailable):
		return EnvironmentFault{
			Class:  FaultNoProviderAccount,
			Reason: noAccountReason(err),
		}, true

	case errors.Is(err, ErrAccountResolverNotConfigured):
		return EnvironmentFault{
			Class: FaultNoAccountResolver,
			Reason: "no provider account pool is wired, so this job had no credential to run " +
				"on and no process was started. The daemon reports this at boot: pooled " +
				"accounts require a secrets manager.",
		}, true

	// THE POOL CHOSE AN ACCOUNT AND COULD NOT HAND OVER ITS CREDENTIAL. Both
	// are raised after selection and both leave the spawn with nothing to run
	// as — so they park for the same reason an empty pool does.
	case errors.Is(err, ErrAccountCredentialNotFound):
		return EnvironmentFault{
			Class: FaultAccountCredentialMissing,
			Reason: "the pool selected a provider account whose credential is missing from the " +
				"secrets backend — the account's metadata exists and its secret does not — so " +
				"no process was started and this attempt was not charged. Re-import the " +
				"account's credential, then readmit the task.",
		}, true

	case errors.Is(err, ErrAccountRefresherNotConfigured):
		return EnvironmentFault{
			Class: FaultAccountRefresherMissing,
			Reason: "the pool selected a provider account whose refresh policy needs a token " +
				"refresher this deployment never registered for its provider, so its credential " +
				"could not be renewed and no process was started. This attempt was not " +
				"charged; fix the provider configuration, then readmit the task.",
		}, true

	// THE SPEND GATE'S TWO REFUSALS. Both are raised before any process
	// started, so neither charges the attempt, and both name the moment the
	// answer changes — a sliding window's oldest entry ageing out, or a
	// breaker's cooldown elapsing — so the task waits exactly that long
	// instead of being retried into the same refusal.
	case errors.Is(err, ErrAccountQuotaExhausted):
		return EnvironmentFault{
			Class:          FaultAccountQuotaExhausted,
			Reason:         quotaExhaustedReason(err),
			RetryNotBefore: quotaExhaustedRetryAt(err),
		}, true

	case errors.Is(err, ErrAccountBreakerOpen), errors.Is(err, ErrAccountBreakerTrialHeld):
		return EnvironmentFault{
			Class:          FaultAccountBreakerTripped,
			Reason:         breakerTrippedReason(err),
			RetryNotBefore: breakerTrippedRetryAt(err),
		}, true

	case errors.Is(err, ErrProjectCapacityExceeded):
		return EnvironmentFault{
			Class:          FaultProjectCapacityExceeded,
			Reason:         capacityReason(err),
			RetryNotBefore: capacityRetryAt(err),
		}, true

	case errors.Is(err, ErrForgeUnreachable):
		return EnvironmentFault{
			Class: FaultForgeUnreachable,
			Reason: "the forge did not answer a call this job needed — no response inside the call's " +
				"deadline, a dropped connection, or a gateway reporting it unavailable — after the " +
				"adapter's bounded retries and read-back reconciliation. That is the forge's host, not " +
				"the work, so this attempt was not charged and the task was requeued.",
		}, true

	case errors.Is(err, ErrAgentUnreachable):
		return EnvironmentFault{
			Class: FaultAgentUnreachable,
			Reason: "the agent daemon did not answer this spawn, so no session was created, no " +
				"container started and no token was spent. This attempt was not charged. It is " +
				"usually a daemon that is restarting or has stopped: confirm the agent is " +
				"registered, then readmit the task.",
		}, true
	}

	if class, ok := PreFirstTurnFault(err); ok {
		return EnvironmentFault{
			Class:  class,
			Reason: preFirstTurnReason(class),
		}, true
	}

	return EnvironmentFault{}, false
}

// noAccountReason states which pool came up empty, from the resolver's own
// typed error rather than from its message.
func noAccountReason(err error) string {
	var unavailable *NoAccountAvailableError
	if !errors.As(err, &unavailable) {
		return "no provider account was available for this job, so no process was started. " +
			"Register a pooled account for the backend's provider."
	}

	var b strings.Builder
	b.WriteString("no provider account for ")
	if unavailable.Provider != "" {
		b.WriteString(string(unavailable.Provider))
	} else {
		b.WriteString("this backend's provider")
	}
	b.WriteString(" in tenant ")
	b.WriteString(displayScope(unavailable.TenantID))
	if unavailable.ProjectID != "" {
		b.WriteString(" (project ")
		b.WriteString(unavailable.ProjectID)
		b.WriteString(", and none unscoped)")
	}
	b.WriteString(". No process was started and this attempt was not charged; " +
		"register a pooled account for that provider, then readmit the task.")
	return b.String()
}

// coolingDownUntil is when the pool says it comes back, or the zero time when
// the error carried none. Zero rather than a guess: dispatching immediately is
// the fail-open direction — the task is tried too eagerly rather than hidden.
func coolingDownUntil(err error) time.Time {
	var cooling *AccountsCoolingDownError
	if !errors.As(err, &cooling) {
		return time.Time{}
	}
	return cooling.AvailableAt
}

// coolingDownReason states that the engine is waiting rather than stuck, and
// says WHEN.
func coolingDownReason(err error) string {
	var cooling *AccountsCoolingDownError
	if !errors.As(err, &cooling) {
		return "every provider account for this job is on a rate-limit cooldown; " +
			"the task is retried on a later sweep and this attempt was not charged."
	}

	var b strings.Builder
	b.WriteString("every provider account")
	if cooling.Candidates > 0 {
		b.WriteString(" (")
		b.WriteString(strconv.Itoa(cooling.Candidates))
		b.WriteString(" of them)")
	}
	b.WriteString(" for ")
	if cooling.Provider != "" {
		b.WriteString(string(cooling.Provider))
	} else {
		b.WriteString("this backend's provider")
	}
	b.WriteString(" in tenant ")
	b.WriteString(displayScope(cooling.TenantID))
	b.WriteString(" is on a rate-limit cooldown")
	if !cooling.AvailableAt.IsZero() {
		b.WriteString("; the earliest returns at ")
		b.WriteString(cooling.AvailableAt.UTC().Format(time.RFC3339))
	} else {
		b.WriteString("; none named a return time")
	}
	b.WriteString(". Nothing is misconfigured — the task is retried on a later sweep, " +
		"and this attempt was not charged.")
	return b.String()
}

// capacityRetryAt is when the project's budget could free, from the typed
// error.
func capacityRetryAt(err error) time.Time {
	var exceeded *ProjectCapacityExceededError
	if !errors.As(err, &exceeded) {
		return time.Time{}
	}
	return exceeded.RetryAt
}

// capacityReason states that the project is full rather than broken.
func capacityReason(err error) string {
	var exceeded *ProjectCapacityExceededError
	if !errors.As(err, &exceeded) {
		return "the project's concurrency budget was already held, so no session was started " +
			"and this attempt was not charged; the task is retried when a lease ends."
	}
	return "project " + exceeded.ProjectID + " already holds " + strconv.Itoa(exceeded.Live) +
		" of its " + strconv.Itoa(exceeded.Limit) + " concurrent jobs, so no session was started " +
		"and this attempt was not charged; the task is retried when a lease ends."
}

// =============================================================================
// The spend gate's reasons
// =============================================================================
//
// WRITTEN FOR THE PERSON WATCHING THE ISSUE, not for the operator reading a
// log. Both of these end up quoted verbatim in a forge comment (see
// pkg/sdlc/blocked.go), so each one says three things in order: what stopped,
// what it means, and what happens next without anyone doing anything. A
// refusal that reads like a fault sends somebody to investigate a cap that is
// working exactly as configured.

// quotaExhaustedRetryAt is when the refusing window's oldest entry ages out.
func quotaExhaustedRetryAt(err error) time.Time {
	var exhausted *AccountQuotaExhaustedError
	if !errors.As(err, &exhausted) {
		return time.Time{}
	}
	return exhausted.RetryAt
}

// quotaExhaustedReason states that the cap did its job and says when the
// allowance returns.
func quotaExhaustedReason(err error) string {
	var exhausted *AccountQuotaExhaustedError
	if !errors.As(err, &exhausted) {
		return "the pooled provider account for this job has spent its configured allowance, " +
			"so no session was started and this attempt was not charged; the task is retried " +
			"once the allowance frees up."
	}

	var b strings.Builder
	b.WriteString("the pooled provider account serving this job has spent its ")
	b.WriteString(exhausted.Axis)
	b.WriteString(" allowance for the ")
	b.WriteString(strconv.Quote(exhausted.Window))
	b.WriteString(" window (")
	b.WriteString(strconv.FormatInt(exhausted.Used, 10))
	b.WriteString(" of ")
	b.WriteString(strconv.FormatInt(exhausted.Limit, 10))
	b.WriteString("). Nothing is broken — this is the spend cap configured on the account. ")
	b.WriteString("No session was started and this attempt was not charged")
	if !exhausted.RetryAt.IsZero() {
		b.WriteString("; the window frees up at ")
		b.WriteString(exhausted.RetryAt.UTC().Format(time.RFC3339))
		b.WriteString(" and the task is retried then")
	} else {
		b.WriteString("; raise the account's allowance or add another account to the pool")
	}
	b.WriteString(".")
	return b.String()
}

// breakerTrippedRetryAt is when an open breaker admits its next trial. A
// half-open account whose trial is held elsewhere names no moment: it depends
// on when the other caller's turn reaches the ledger, which nothing here can
// predict, and inventing a wait would be inventing a fact.
func breakerTrippedRetryAt(err error) time.Time {
	var open *AccountBreakerOpenError
	if !errors.As(err, &open) {
		return time.Time{}
	}
	return open.OpenUntil
}

// breakerTrippedReason states which of the two tripped states stopped the job.
func breakerTrippedReason(err error) string {
	var open *AccountBreakerOpenError
	if errors.As(err, &open) {
		var b strings.Builder
		b.WriteString("the pooled provider account serving this job failed ")
		b.WriteString(strconv.Itoa(open.Failures))
		b.WriteString(" times with no success since, so its circuit breaker took it out of service. ")
		b.WriteString("No session was started and this attempt was not charged")
		if !open.OpenUntil.IsZero() {
			b.WriteString("; one caller retries the account at ")
			b.WriteString(open.OpenUntil.UTC().Format(time.RFC3339))
			b.WriteString(" and the task follows if that succeeds")
		}
		b.WriteString(".")
		return b.String()
	}

	var held *AccountBreakerTrialHeldError
	if errors.As(err, &held) {
		return "the pooled provider account serving this job is being retried by another caller " +
			"after " + strconv.Itoa(held.Failures) + " failures, and only one caller may try at a time. " +
			"No session was started and this attempt was not charged; the task is retried once that " +
			"trial reports."
	}

	return "the pooled provider account for this job is out of service on its own failure history, " +
		"so no session was started and this attempt was not charged; the task is retried when the " +
		"account is trialled again."
}

// displayScope names an empty tenant as the system scope rather than printing
// nothing, so the reason does not read as a truncated sentence.
func displayScope(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return SystemTenantID
	}
	return tenantID
}

// preFirstTurnReason says what an agent's own terminal reason means for the
// task. What is interpolated is a constant selected by exact match against the
// agent's declared enum — never the bytes the agent wrote.
func preFirstTurnReason(class string) string {
	named := "exited on `" + class + "`"
	if class == TerminalReasonUnclassified {
		named = "exited on a reason this build does not recognize (see the daemon log)"
	}
	return "the agent " + named + " before its first model turn: it consumed " +
		"no input tokens, so nothing about this task was ever attempted. This is an " +
		"environment fault — credentials, network, or the agent itself — and the attempt " +
		"was not charged against the task's budget. Resolve it, then readmit the task."
}

// =============================================================================
// The outcome of one attempt
// =============================================================================

// DefaultAttemptBudget bounds a task's charged attempts when the task declares
// no budget of its own.
const DefaultAttemptBudget = 3

// MaxConsecutiveEnvironmentFaults bounds how many times in a row a task may be
// requeued on an environment fault before the engine stops and tells somebody.
//
// THE BOUND IS WHAT MAKES "RETRY" THE SAFE DEFAULT. Three, because it is the
// smallest number that distinguishes a blip from a condition: one is noise,
// two can still be one outage seen twice, and by three consecutive jobs with
// no model turn between them something is wrong that watching will not fix.
// Counted CONSECUTIVELY, so a task that faults, succeeds, and faults again is
// not walking toward a wall it never reaches.
const MaxConsecutiveEnvironmentFaults = 3

// MaxRetryDeferral caps how long a fault may hold a task out of the queue.
//
// THE CAP IS ABOUT TRUST, NOT PATIENCE. The moment comes from a provider hold
// read off an account record, and a clock that is wrong in the wrong direction
// would otherwise hide a task for years. The engine waits as long as it is
// asked to and no longer than a day; past that it dispatches, faults again,
// and the consecutive bound takes over as the visible answer.
const MaxRetryDeferral = 24 * time.Hour

// ChargedAttempts is how many attempts count against the budget: every job
// dispatched, less those refunded as environment faults.
func (t *Task) ChargedAttempts() int {
	charged := t.Attempts - t.EnvironmentFaults - t.QuestionAttempts
	if charged < 0 {
		return 0
	}
	return charged
}

// EffectiveAttemptBudget is the task's budget, or the engine default when the
// task declares none.
func (t *Task) EffectiveAttemptBudget() int {
	if t.AttemptBudget > 0 {
		return t.AttemptBudget
	}
	return DefaultAttemptBudget
}

// BudgetExhausted reports whether the task has spent every charged attempt
// its budget allows.
func (t *Task) BudgetExhausted() bool {
	return t.ChargedAttempts() >= t.EffectiveAttemptBudget()
}

// Deferred reports whether the task must not be dispatched at now, a
// Unix-nanosecond instant.
func (t *Task) Deferred(now int64) bool {
	return t.RetryNotBefore > now
}

// faultOutcome is what one environment fault does to one task.
//
// THE TWO DECISIONS ARE DERIVED TOGETHER because deriving them apart lets them
// disagree: whether the attempt comes back and whether the task requeues are
// the same question asked twice.
type faultOutcome struct {
	// refund gives the job's attempt back to the budget.
	refund bool

	// requeue puts the task back in the queue instead of parking it.
	requeue bool

	// bounded means the task hit MaxConsecutiveEnvironmentFaults. It parks,
	// and this attempt is CHARGED — the retries stop being free at the same
	// moment they stop being tried.
	bounded bool

	// counts means this requeue consumes one of the bound's three.
	counts bool

	// notBefore is when the task may be dispatched again, zero for now.
	notBefore time.Time
}

// faultOutcomeFor decides both halves for this fault against this task. A
// zero fault (no class) is not an environment fault and yields the zero
// outcome: charged, the ordinary failed attempt.
func faultOutcomeFor(task *Task, fault EnvironmentFault) faultOutcome {
	if fault.Class == "" {
		return faultOutcome{}
	}
	if !faultRequeues(fault.Class) {
		// Proven operator-actionable: park now, attempt still given back
		// because nothing was attempted.
		return faultOutcome{refund: true}
	}
	// EVERY REQUEUE COUNTS, INCLUDING A COOLDOWN. With the wait in place the
	// bound means what it should: three holds waited OUT is a saturated pool
	// a human should hear about. Patience is not punished; only futility is.
	if task != nil && task.ConsecutiveEnvironmentFaults+1 >= MaxConsecutiveEnvironmentFaults {
		return faultOutcome{bounded: true}
	}
	return faultOutcome{
		refund: true, requeue: true, counts: true,
		// Zero for every class that cannot say when to come back. A fault
		// with no time waits not at all and is bounded by the three-strike
		// count alone, which is the fail-closed direction.
		notBefore: fault.RetryNotBefore,
	}
}

// clampRetryDeferral bounds how far out a deferral may be set, and answers zero
// for a moment that has already passed.
func clampRetryDeferral(at, now time.Time) time.Time {
	if at.IsZero() || !at.After(now) {
		return time.Time{}
	}
	if ceiling := now.Add(MaxRetryDeferral); at.After(ceiling) {
		return ceiling
	}
	return at
}

// boundedFaultReason composes the block reason for a task that hit the
// consecutive bound.
func boundedFaultReason(task *Task, fault EnvironmentFault) string {
	consecutive := MaxConsecutiveEnvironmentFaults
	if task != nil && task.ConsecutiveEnvironmentFaults+1 < consecutive {
		consecutive = task.ConsecutiveEnvironmentFaults + 1
	}
	reason := strconv.Itoa(consecutive) + " consecutive environmental failures — " + fault.Class
	if detail := strings.TrimSpace(fault.Reason); detail != "" {
		reason += "\n\nThe last one: " + detail
	}
	return reason + "\n\nEach was a job the environment failed rather than the work, so none was charged " +
		"against the attempt budget and the task was retried. Three in a row is not a blip; " +
		"this one IS charged, and nothing will retry it until the condition is resolved and " +
		"the task readmitted."
}

// =============================================================================
// Job failure classes
// =============================================================================
//
// JobStatus.FailureClass carries WHY a job failed, as a class rather than as
// prose, so the task's accounting never parses an error string. The
// environment classes above are one family; these are the others.

const (
	// JobFailureClassAbandoned marks a job whose node stopped heartbeating
	// and is not running it: the reaper concluded it. Charged — the job may
	// have run for forty minutes and died mid-turn, and refunding on that
	// evidence would give a task unlimited attempts at whatever kills its
	// jobs.
	JobFailureClassAbandoned = "abandoned"

	// JobFailureClassSuperseded marks a job that lost its lease to a newer
	// generation, or never got it. The task belongs to whoever holds the
	// lease; this job writes nothing to it.
	JobFailureClassSuperseded = "superseded"

	// JobFailureClassStalled marks a job the stall detector ended: past its
	// state's idle threshold with no addressable session. Charged, like any
	// failure of the work.
	JobFailureClassStalled = "stalled"
)

// classifyJobFailure names the class a loop's terminal error belongs to, and
// the environment fault when the class is one.
func classifyJobFailure(err error) (string, EnvironmentFault) {
	if err == nil {
		return "", EnvironmentFault{}
	}
	var (
		superseded *JobSupersededError
		taken      *JobLeaseTakenError
		stalled    *JobStalledError
	)
	switch {
	case errors.As(err, &superseded), errors.As(err, &taken):
		return JobFailureClassSuperseded, EnvironmentFault{}
	case errors.As(err, &stalled):
		return JobFailureClassStalled, EnvironmentFault{}
	}
	if fault, ok := classifyEnvironmentFault(err); ok {
		return fault.Class, fault
	}
	return "", EnvironmentFault{}
}

// =============================================================================
// concludeAttempt — one job's outcome, written to its task once
// =============================================================================

// attemptHistoryKey is the task metadata key holding every job that has been
// concluded against the task, oldest first, comma-separated.
const attemptHistoryKey = "attempt_history"

// budgetExhaustedReason is the Result.Metadata["reason"] a task fails with
// when its last charged attempt is spent.
const budgetExhaustedReason = "budget_exhausted"

// attemptConclusion is everything the accounting needs to know about one
// finished job.
type attemptConclusion struct {
	// status is the job's terminal status: its State decides completion, its
	// FailureClass decides how a failure is charged, its Error is the detail
	// recorded on the task.
	status *JobStatus

	// fault is the environment fault the FailureClass names, zero otherwise.
	// It carries what the status cannot: the moment a cooldown lifts.
	fault EnvironmentFault

	// lease is the lease the job held, released once the task is written.
	// Nil when the job held none.
	lease *JobLease

	// park is the runner's own judgement that the task must wait for a
	// person, nil when it made none. It OUTRANKS the failure accounting:
	// see applyConclusion.
	park *BlockReason
}

// taskAttemptOpen reports whether a task can still have a job concluded
// against it.
func taskAttemptOpen(status TaskStatus) bool {
	switch status {
	case TaskStatusPending, TaskStatusClaimed, TaskStatusInProgress:
		return true
	default:
		return false
	}
}

// concludeAttempt writes one job's outcome to its task.
//
// SYNCHRONOUS, ON THE GOROUTINE THAT RAN THE JOB, and the reaper's on a dead
// one. The task is written ONCE, through the store's compare-and-swap on
// Task.Version: a conflict re-reads and re-derives from the fresh record
// rather than overwriting whatever changed underneath, so a concurrent
// block, readmit or artifact write is preserved and this conclusion still
// lands exactly once.
//
// THE GUARD IS THE JOB, NOT THE STATUS. A task accepts a conclusion only from
// the job it currently names in SpawnedJobID and only while its attempt is
// open; anything else is a report about a run the task has already moved past
// — a slow node concluding after the reaper did — and is refused with an
// AttemptConcludedError rather than applied twice. The lease is released
// either way, because the holder is finished whichever answer it got.
func (s *agentServiceImpl) concludeAttempt(ctx context.Context, tenantID, taskID string, c attemptConclusion) error {
	if c.status == nil || c.status.JobID == "" {
		return &TaskStoreError{Sentinel: ErrTaskStoreInvalidInput, Message: "a conclusion must carry the job's terminal status"}
	}
	if taskID == "" {
		return &TaskStoreError{Sentinel: ErrTaskStoreInvalidInput, Message: "a conclusion must name its task"}
	}
	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}
	if c.status.FailureClass == JobFailureClassSuperseded {
		// The lease belongs to another job now, and so does the task.
		// Nothing to write, nothing to release.
		return nil
	}

	concluded, err := s.writeConclusion(ctx, tenantID, taskID, c)
	s.surfaceConclusion(ctx, tenantID, concluded, c.status.JobID)
	if releaseErr := s.releaseConcludedLease(ctx, c.lease); releaseErr != nil {
		if err != nil {
			return err
		}
		return releaseErr
	}
	return err
}

// taskConclusionSurfaceTimeout bounds the whole announcement of one
// conclusion, on a context of its own.
//
// A CONTEXT OF ITS OWN because the conclusion is already durable: the caller's
// context is the job's conclude budget or the reaper's sweep, and a task that
// has stopped must still reach the person watching it when the run that
// stopped it is being torn down. BOUNDED because an observer talks to a forge
// over the network, and a forge that will not answer must not hold a job
// goroutine or a reaper sweep open for ever.
const taskConclusionSurfaceTimeout = 2 * time.Minute

// surfaceConclusion announces a concluded task to the registered observers.
//
// IT NEVER CHANGES THE OUTCOME. The task is written by the time this runs, so
// an observer that could not report is logged and nothing else: a forge outage
// must not turn a concluded task into an unconcluded one. An empty reason is a
// write that did not conclude the task — a requeue, or a conclusion another
// node had already applied — and announces nothing.
func (s *agentServiceImpl) surfaceConclusion(ctx context.Context, tenantID string, concluded taskConclusionRecord, jobID string) {
	if concluded.reason == "" || concluded.task == nil {
		return
	}
	surfaceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), taskConclusionSurfaceTimeout)
	defer cancel()
	err := NotifyTaskConcluded(surfaceCtx, TaskConclusion{
		TenantID: tenantID, Reason: concluded.reason, Task: concluded.task, JobID: jobID,
	})
	if err != nil {
		s.logger.Warnf("orchestration: task %s concluded %s but was not surfaced: %s",
			concluded.task.ID, concluded.reason, err.Error())
	}
}

// taskConclusionRecord pairs the reason a write concluded a task with the
// record it wrote, so the announcement reads the same bytes the store holds
// rather than re-reading and racing whatever happened next.
type taskConclusionRecord struct {
	reason TaskConclusionReason
	task   *Task
}

// releaseConcludedLease gives the lease back after the task is written.
func (s *agentServiceImpl) releaseConcludedLease(ctx context.Context, lease *JobLease) error {
	if lease == nil || s.jobLeases == nil {
		return nil
	}
	return s.jobLeases.Release(ctx, lease)
}

// writeConclusion is the CAS loop around applyConclusion. It returns what the
// write concluded, so the announcement that follows names the same outcome the
// store now holds.
func (s *agentServiceImpl) writeConclusion(ctx context.Context, tenantID, taskID string, c attemptConclusion) (taskConclusionRecord, error) {
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
		if err != nil {
			return taskConclusionRecord{}, err
		}
		if task.SpawnedJobID != c.status.JobID || !taskAttemptOpen(task.Status) {
			return taskConclusionRecord{}, &AttemptConcludedError{TaskID: taskID, JobID: c.status.JobID, SpawnedJobID: task.SpawnedJobID, Status: task.Status}
		}
		reason := applyConclusion(task, c, time.Now().UTC())
		// The reason travels WITH the record, not only to the observers: a
		// job page read hours later has no conclusion event to consult, and
		// re-deriving the reason from the resting status cannot tell an
		// abandoned attempt from an operator's hold. An empty reason is a
		// requeue — the task did not conclude — and clears any earlier one.
		task.ConclusionReason = reason
		err = s.taskStore.UpdateTask(ctx, task)
		var conflict *TaskVersionConflictError
		if errors.As(err, &conflict) {
			continue
		}
		if err != nil {
			return taskConclusionRecord{}, err
		}
		return taskConclusionRecord{reason: reason, task: task}, nil
	}
	return taskConclusionRecord{}, &TaskStoreError{
		Sentinel: ErrTaskVersionConflict,
		Message:  "task " + taskID + " changed under every attempt to conclude job " + c.status.JobID,
	}
}

// applyConclusion derives the task's next record from the job's outcome and
// names what it concluded.
//
// EVERY BRANCH ENDS THE ATTEMPT: SpawnedJobID moves into the history so a
// second report about the same job is refused, and the outcome decides where
// the task rests.
//
// THE RETURNED REASON IS THE TASK'S CONCLUSION, NOT THE ATTEMPT'S. An empty
// reason means this attempt ended but the task did not: it is back in the
// queue and nothing has concluded about it yet. Every non-empty answer is a
// member of the closed vocabulary in task_conclusion.go, and is what the
// registered observers are told.
func applyConclusion(task *Task, c attemptConclusion, now time.Time) TaskConclusionReason {
	nowNanos := now.UnixNano()
	recordAttemptHistory(task, c.status.JobID)
	task.Result = &TaskResult{
		Output: c.status.Error,
		Metadata: map[string]string{
			"job_id":     c.status.JobID,
			"job_status": string(c.status.State),
		},
	}
	if c.status.FailureClass != "" {
		task.Result.Metadata["failure_class"] = c.status.FailureClass
	}

	if c.status.State != JobStateFailed {
		task.Status = TaskStatusCompleted
		task.CompletedAt = nowNanos
		task.ConsecutiveEnvironmentFaults = 0
		task.RetryNotBefore = 0
		task.BlockedReason = ""
		task.BlockKind = ""
		task.BlockFaultClass = ""
		// A SATISFIED ATTEMPT COMPLETES THE TASK AND SAYS WHY. The work was
		// already present, so there is nothing to merge and nobody to ask —
		// but concluding it `completed` would post nothing (OutcomeSilent) and
		// leave a stale issue looking like delivered work. `already_satisfied`
		// is the same terminal state with the finding attached, so a person
		// closes or restates the issue instead of it being adopted again.
		if c.status.Satisfied {
			return TaskConcludedAlreadySatisfied
		}
		// A SUPERSEDED ATTEMPT ENDS THE SAME WAY, for the same reason: its
		// pull request was closed without merging, so there is nothing to
		// merge and no round that could change that (go-quicbaas#1054).
		if c.status.Superseded {
			return TaskConcludedSuperseded
		}
		return TaskConcludedCompleted
	}

	// THE RUNNER'S OWN JUDGEMENT COMES FIRST — ahead of the reaper's death,
	// ahead of the fault classes, ahead of the budget. A park says another
	// attempt would run the same prompt against the same unanswered
	// question; requeueing it anyway is the loop go-quicbaas#787 measured.
	//
	// AHEAD OF THE DEATH IS THE PART THAT IS EASY TO GET WRONG, and getting
	// it wrong loses the judgement across a crash rather than within a run.
	// A daemon that dies after writing the failed status but before applying
	// this conclusion leaves the lease reaper to rebuild it, and the reaper
	// stamps every record it concludes `abandoned` (abandonedJobStatus). If
	// the death were tested first, a task the runner had already parked for
	// a person would be read as "the node stopped, nobody diagnosed it" and
	// requeued. The park is a diagnosis, and it was made before the node
	// died; nothing that happened afterwards un-makes it.
	//
	// The fault classes cannot answer this case either — nothing about the
	// environment failed — so a park that fell through to them would be
	// charged and retried like any other failure of the work.
	if c.park != nil {
		task.ConsecutiveEnvironmentFaults = 0
		task.RetryNotBefore = 0
		// AND A PARK THE RUNNER ATTRIBUTED TO THE FACTORY IS REFUNDED. The
		// park already stops the retry; what the class adds is that the
		// attempt was never an attempt. A job that died because its project
		// had no workspace, or because the checkout it was about to prompt on
		// was not there, made no judgement about the work — and charging it
		// spends a budget on the operator's mistake, so the first real
		// difficulty after the repair meets a task that has nothing left
		// (go-quicbaas#908).
		//
		// A class this binary does not recognise is CHARGED. See
		// OperatorFaultClass: the direction that cannot hide a broken change
		// is the one an unknown class takes.
		if OperatorFaultClass(c.park.FaultClass) {
			task.EnvironmentFaults++
		}
		// AND A PARK THAT IS WAITING ON A PERSON IS REFUNDED TOO. A worker
		// that stops to ask a question, or on a decision the convention
		// reserves for the owner, has not judged the work either — and unlike
		// a failure, this park will NEVER retry on its own: needs_info is
		// terminal until a person acts. So the budget bounds nothing here.
		// All charging it does is spend the allowance, so that when the owner
		// finally answers, the readmission meets a task with nothing left and
		// needs a second manual step to move.
		//
		// Measured on the burn-in fleet (2026-09-14): three go-luks2 tasks sat
		// at seven charged attempts against a default budget of three, having
		// spent them asking the owner rulings that were never answered.
		if ParkAwaitsAnAnswer(c.park.Kind) {
			task.QuestionAttempts++
		}
		// The class is written onto the block as well, so the forge comment
		// names the cause and re-labelling the issue after the repair
		// releases the task source (taskSourceReleasable). An operator hold
		// with no class stays held, exactly as before.
		block(task, c.park.Kind, c.park.Message, c.park.FaultClass)
		return parkConclusion(*c.park)
	}

	if c.status.FailureClass == JobFailureClassAbandoned {
		return applyDeath(task, c)
	}

	outcome := faultOutcomeFor(task, c.fault)
	// The bounded reason counts the run that reached the bound, so it is
	// composed before the counters move.
	bounded := boundedFaultReason(task, c.fault)
	if outcome.refund {
		task.EnvironmentFaults++
	}
	if outcome.counts {
		task.ConsecutiveEnvironmentFaults++
	} else {
		task.ConsecutiveEnvironmentFaults = 0
	}
	// THE WAIT IS PERSISTED ON THE TASK, not held in the loop, because the
	// thing that must respect it is the next dispatcher — possibly on another
	// node, certainly after this process could have restarted.
	task.RetryNotBefore = 0
	if deferral := clampRetryDeferral(outcome.notBefore, now); !deferral.IsZero() {
		task.RetryNotBefore = deferral.UnixNano()
	}

	switch {
	case outcome.bounded:
		block(task, TaskBlockKindBlocked, bounded, c.fault.Class)
		return TaskConcludedFaultBounded
	case c.fault.Class != "" && !outcome.requeue:
		block(task, TaskBlockKindBlocked, c.fault.Reason, c.fault.Class)
		return TaskConcludedFaultHeld
	case outcome.requeue:
		requeue(task)
		return ""
	case task.BudgetExhausted():
		task.Status = TaskStatusFailed
		task.CompletedAt = nowNanos
		task.Result.Metadata["reason"] = budgetExhaustedReason
		return TaskConcludedBudgetExhausted
	default:
		requeue(task)
		return ""
	}
}

// parkConclusion names what a runner's park concluded, and the answer turns
// on whether the run MANAGED TO ANNOUNCE IT.
//
// A park the run surfaced is `runner_parked`, which the module renders
// silent: the comment is already on the issue, under the marker a second
// write would reuse. A park the run did NOT surface is reported as the plain
// parked_<kind> its block kind names — the same reasons an operator's own
// BlockTask produces, and for the same reason: nothing has told the issue
// yet, so the conclusion seam is the only thing that can.
//
// THE UNSURFACED CASE IS THE ONE THAT MATTERS. It arises exactly when the
// runner's own surfacing broke, which is when a task blocked with nothing on
// its issue is least likely to be noticed and most likely to be waited on.
//
// A kind outside the closed enum cannot reach here — JobRun.Park refuses one
// — but the fallback is the escalation rather than silence, because the
// failure mode to avoid is a stop nobody is told about.
func parkConclusion(park BlockReason) TaskConclusionReason {
	if park.Surfaced {
		return TaskConcludedRunnerParked
	}
	if reason, ok := ConclusionOfBlockKind(park.Kind); ok {
		return reason
	}
	return TaskConcludedParkedEscalated
}

// applyDeath charges a reaped job and decides between another attempt and a
// block an operator can read.
//
// BLOCKED RATHER THAN FAILED when the budget is spent, and that is a policy
// distinction: a failure the loop observed carries a diagnosis, while a death
// carries none — nothing diagnosed the condition and the only thing known is
// that the node stopped. A task that failed on evidence nobody produced is one
// an operator should be able to readmit once they have read it.
func applyDeath(task *Task, c attemptConclusion) TaskConclusionReason {
	task.ConsecutiveEnvironmentFaults = 0
	task.RetryNotBefore = 0
	if task.BudgetExhausted() {
		block(task, TaskBlockKindEscalated, c.status.Error+"\n\nThe attempt budget is spent; readmit the task to try again.", "")
		return TaskConcludedAbandoned
	}
	requeue(task)
	return ""
}

// requeue returns the task to the queue for its next attempt.
func requeue(task *Task) {
	task.Status = TaskStatusPending
	task.AssignedTo = ""
	task.ClaimedAt = 0
	task.BlockedReason = ""
	task.BlockKind = ""
	task.BlockFaultClass = ""
}

// block parks the task with a kind, a reason, and — for a block that came from
// a classified environment fault — that fault's class. faultClass is empty for
// an engine escalation or any block no fault classified; it is what a
// re-adoption reads to tell a transient environment hold from an operator hold.
func block(task *Task, kind TaskBlockKind, reason, faultClass string) {
	task.Status = TaskStatusBlocked
	task.BlockKind = kind
	task.BlockedReason = reason
	task.BlockFaultClass = faultClass
	task.AssignedTo = ""
}

// recordAttemptHistory appends the concluded job to the task's history and
// clears SpawnedJobID so the attempt cannot be concluded twice.
func recordAttemptHistory(task *Task, jobID string) {
	if task.Metadata == nil {
		task.Metadata = map[string]string{}
	}
	if prior := task.Metadata[attemptHistoryKey]; prior != "" {
		task.Metadata[attemptHistoryKey] = prior + "," + jobID
	} else {
		task.Metadata[attemptHistoryKey] = jobID
	}
	task.SpawnedJobID = ""
	task.SpawnedAt = 0
}

// AttemptHistory lists every job concluded against the task, oldest first.
func (t *Task) AttemptHistory() []string {
	if t.Metadata == nil || t.Metadata[attemptHistoryKey] == "" {
		return nil
	}
	return strings.Split(t.Metadata[attemptHistoryKey], ",")
}

// LatestAttemptJobID names the job the task's MOST RECENT attempt ran as: the
// open attempt when one is running, and the last concluded attempt otherwise.
// Empty for a task that has never been dispatched.
//
// It is the binding every "what happened to this task" question resolves
// through — the agent that ran it, the branch it pushed, the review it is
// waiting on — because those are the running attempt's facts while one is
// running and the last one's afterwards.
func (t *Task) LatestAttemptJobID() string {
	if t == nil {
		return ""
	}
	if t.SpawnedJobID != "" {
		return t.SpawnedJobID
	}
	history := t.AttemptHistory()
	if len(history) == 0 {
		return ""
	}
	return history[len(history)-1]
}

// ErrAttemptConcluded indicates a conclusion about a job the task no longer
// names as its open attempt.
var ErrAttemptConcluded = errors.New("task attempt: already concluded")

// AttemptConcludedError reports a conclusion the task refused because its
// open attempt is not the reporting job.
type AttemptConcludedError struct {
	TaskID       string
	JobID        string
	SpawnedJobID string
	Status       TaskStatus
}

// Error implements the error interface.
func (e *AttemptConcludedError) Error() string {
	return ErrAttemptConcluded.Error() + ": task=" + e.TaskID + " job=" + e.JobID +
		" open_job=" + e.SpawnedJobID + " status=" + string(e.Status)
}

// Unwrap returns the sentinel for errors.Is support.
func (e *AttemptConcludedError) Unwrap() error { return ErrAttemptConcluded }
