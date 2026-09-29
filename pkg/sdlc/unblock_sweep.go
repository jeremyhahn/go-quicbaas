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
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The sweep — a blocked task is re-examined, not left where nothing looks
// =============================================================================
//
// Every blocked task in a project is read on every pass, its condition
// evaluated, and a task whose condition has been met is readmitted through
// the engine's own verb. That is the payoff of the typed condition in
// readmission.go: a block that names a checkable condition clears itself,
// and nobody has to re-label an issue to say what a check could have said.
//
// FAIL-CLOSED IN EVERY DIRECTION THAT IS NOT A PROVEN SATISFACTION. A
// condition that cannot be read, cannot be decided, or is decided by a person
// leaves the task exactly where it is. The only path that moves a task is a
// check that ran and answered yes, because every other path would release
// work on a condition nobody evaluated — which is the prose behaviour with
// extra machinery in front of it.
//
// EVERY BLOCKED TASK PRODUCES A FINDING, including the ones nothing can
// decide. A pass that reported only what it moved would be silent about the
// tasks that most need a person, which is the state this sweep exists to end.

// BlockedTaskService is the sweeper's whole view of the engine: it lists the
// blocked tasks of a project and readmits the ones whose condition cleared.
// orchestration.AgentService satisfies it.
//
// Readmission is the ENGINE's verb rather than a status write of this
// module's own. A blocked task carries an assignment, a retry hold and a
// block kind that all have to clear together, and orchestration is where
// that is written once.
//
// THE SERVICE'S VERB, NOT THE STORE'S WRITE, and the difference is invisible
// to the compiler: orchestration.TaskStore carries a ReadmitTask of exactly
// this signature, and it makes the same state transition in silence. Only the
// service's verb ANNOUNCES the readmission — it publishes task_readmitted and
// surfaces TaskConcludedReadmitted, whose empty label key is what takes
// `factory:blocked` back off the issue. A sweep wired to the store returns the
// task to the queue while the issue still says it is blocked, and nothing
// downstream repairs that: the label reconciler deliberately lists only the
// CLAIMING labels, which is what stops it posting `abandoned` over every issue
// the factory has ever finished with (go-quicbaas#842).
//
// So the store is refused by name at construction. See NewUnblockSweeper.
type BlockedTaskService interface {
	TaskLister
	ReadmitTask(ctx context.Context, tenantID, taskID string) error

	// SubmitTaskArtifacts records the audit of a readmission an owner's
	// ruling caused: who ruled, and in which comment. See ruling.go.
	SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []orchestration.Artifact) error
}

// The engine's service is the implementation, asserted here so a change to
// either signature is a compile error rather than a wiring failure at boot.
var _ BlockedTaskService = (orchestration.AgentService)(nil)

// isEngineTaskStore reports whether a dependency is the engine's task store
// rather than its service.
//
// IT IS A NAMED REFUSAL OF ONE KNOWN-WRONG IMPLEMENTATION, not a test of
// whether an implementation announces — nothing in the type system can ask
// that. It is decidable and it is exact: orchestration.TaskStore and
// orchestration.AgentService declare CreateTask with different signatures, so
// no single type can be both, and the service can never be caught here.
func isEngineTaskStore(tasks BlockedTaskService) bool {
	_, isStore := tasks.(orchestration.TaskStore)
	return isStore
}

// DefaultUnblockSweepInterval is how often blocked tasks are re-examined.
//
// SLOWER THAN THE REVIEW FLOOR BECAUSE THE LATENCY IT BOUNDS IS CHEAPER. A
// finished change waiting to merge is the most expensive kind of waiting; a
// task whose blocker cleared a few minutes ago is merely idle. The pass is
// also pure reads against the store plus at most one forge resolution per
// project, so the cost is small either way — this interval is chosen for the
// forge's sake, not the store's.
const DefaultUnblockSweepInterval = 10 * time.Minute

// UnblockSweeperConfig wires the sweeper.
type UnblockSweeperConfig struct {
	// Tasks lists blocked tasks and readmits them through the engine's
	// announcing verb. It is orchestration.AgentService, never the task
	// store underneath it.
	Tasks BlockedTaskService

	// Bindings names the forge a project's conditions are checked against.
	Bindings BindingReader

	// Forges builds that forge's adapter.
	Forges ForgeResolver

	// Now is the clock a check reads; nil takes time.Now.
	Now func() time.Time

	// Rulers are the node-configured forge logins, beyond the repository's
	// owner and the binding's own rulers, whose comment on a parked issue is
	// an owner ruling that readmits the task. See RulerPolicy; a malformed
	// policy is refused at construction.
	Rulers RulerPolicy
}

// UnblockSweeper re-examines blocked tasks and readmits the ones whose typed
// condition has been satisfied.
type UnblockSweeper struct {
	tasks    BlockedTaskService
	bindings BindingReader
	forges   ForgeResolver
	now      func() time.Time
	rulers   RulerPolicy

	// failures are the per-tenant and per-project failures a pass deferred,
	// drained by SweepFailures. Only the sweep goroutine touches them, and
	// there is exactly one.
	failures []error
}

// NewUnblockSweeper builds the sweeper; every collaborator is required.
func NewUnblockSweeper(cfg UnblockSweeperConfig) (*UnblockSweeper, error) {
	switch {
	case cfg.Tasks == nil:
		return nil, &InvalidInputError{Field: "tasks", Message: "the sweep reads blocked tasks and readmits them through the engine's readmission verb"}
	case isEngineTaskStore(cfg.Tasks):
		return nil, &InvalidInputError{Field: "tasks", Message: "the engine's task store readmits without announcing it, which leaves the issue labelled blocked; wire the sweep to orchestration.AgentService"}
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the forge a condition is checked against lives on the binding"}
	case cfg.Forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "a condition is checked through the forge adapter"}
	}
	if err := cfg.Rulers.Validate(); err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &UnblockSweeper{
		tasks: cfg.Tasks, bindings: cfg.Bindings, forges: cfg.Forges, now: now,
		rulers: cfg.Rulers,
	}, nil
}

// =============================================================================
// Verdicts
// =============================================================================

// UnblockVerdict is what one pass decided about one blocked task.
type UnblockVerdict string

const (
	// UnblockReleased means the condition was satisfied and the task was
	// readmitted.
	UnblockReleased UnblockVerdict = "released"

	// UnblockWaiting means the condition was evaluated and is not yet
	// satisfied.
	UnblockWaiting UnblockVerdict = "waiting"

	// UnblockAwaitingHuman means the condition is one only a person clears.
	// The finding names who.
	UnblockAwaitingHuman UnblockVerdict = "awaiting_human"

	// UnblockUndecidable means the check could not be run — the forge
	// failed, the subject is gone. Fail-closed: nothing moves.
	UnblockUndecidable UnblockVerdict = "undecidable"

	// UnblockAwaitingDiagnosis means the task is blocked with no typed
	// condition, so a person must diagnose it before it can return.
	//
	// A RECOGNISED CLASS, NOT A DEFECT REPORT. The engine blocks a reaped
	// death precisely BECAUSE nothing diagnosed it, so such a task genuinely
	// has no checkable condition, and attaching a kind to it would fabricate
	// a diagnosis nobody made — the fail-open failure wearing a triage
	// costume, because something downstream would then act on a condition
	// invented to satisfy a format.
	//
	// So this verdict says the true thing: the task is visible, it is
	// reported on every pass, and what it awaits is a person reading the
	// evidence it carries. That is different from UnblockAwaitingHuman,
	// where a condition WAS recorded and names which person and what for.
	UnblockAwaitingDiagnosis UnblockVerdict = "awaiting_diagnosis"

	// UnblockUnreadable means a condition is recorded and cannot be used —
	// malformed, or naming a kind nothing decides.
	UnblockUnreadable UnblockVerdict = "unreadable"

	// UnblockBudgetExhausted means the condition was satisfied and the task
	// was NOT readmitted, because it has no attempts left.
	//
	// Readmitting it would put a task into the queue that dispatch must
	// immediately refuse, on every pass, forever. Leaving it blocked with
	// this verdict says the condition cleared and the budget is what holds
	// it now — the fact an operator needs and the one a silent readmit
	// hides.
	UnblockBudgetExhausted UnblockVerdict = "budget_exhausted"

	// UnblockHeldForLead means the task's issue carries the hold label
	// (HoldLabel), so the task was NOT readmitted whatever else cleared.
	//
	// The hold is how an issue is kept for the lead to deliver — an approved
	// ruling that says "delivery stays lead-driven" leaves it on. Triage
	// already respects it; a sweep that readmitted past it would hand the
	// factory the very work the owner assigned elsewhere, and the owner's
	// ruling comment is exactly what it would readmit on.
	UnblockHeldForLead UnblockVerdict = "held_for_lead"
)

// String returns the verdict's name.
func (v UnblockVerdict) String() string { return string(v) }

// Released reports whether this verdict returned a task to the queue.
func (v UnblockVerdict) Released() bool { return v == UnblockReleased }

// UnblockFinding is one blocked task's outcome from a pass.
type UnblockFinding struct {
	// TaskID is the task examined.
	TaskID string

	// Kind is the condition's kind, empty when none could be read.
	Kind UnblockKind

	// Verdict is what the sweep decided.
	Verdict UnblockVerdict

	// Evidence is the operator-facing reason for the verdict.
	Evidence string
}

// UnblockReport is a whole pass.
type UnblockReport struct {
	// Findings is one entry per blocked task, ordered by task id so two
	// passes over the same state produce comparable reports.
	Findings []UnblockFinding
}

// Released returns the findings that returned a task to the queue.
func (r UnblockReport) Released() []UnblockFinding {
	var released []UnblockFinding
	for _, finding := range r.Findings {
		if finding.Verdict.Released() {
			released = append(released, finding)
		}
	}
	return released
}

// Count returns how many findings carry a verdict.
func (r UnblockReport) Count(v UnblockVerdict) int {
	n := 0
	for _, finding := range r.Findings {
		if finding.Verdict == v {
			n++
		}
	}
	return n
}

// =============================================================================
// The pass
// =============================================================================

// Sweep visits every bound project of every tenant and reports how many tasks
// it returned to the queue.
//
// THIS SWEEP EXISTED AND WAS NEVER RUN. It was written, documented and tested,
// and nothing anywhere constructed it — so a task that blocked on a checkable
// condition stayed blocked however completely that condition cleared, and the
// only thing that ever moved one was a person. A fleet accumulates blocked
// tasks faster than a person clears them, which is how every project ends up
// with nothing runnable. It had no tenant-wide entry point either, which is
// the mechanical reason it could not be mounted beside the other backstops.
//
// THE ONE FAILURE THAT STOPS IT is not being able to enumerate the tenants:
// the pass then does not know what it was meant to visit, and reporting a
// clean pass would be a lie. Everything narrower costs that one tenant or
// project a pass and nothing else, exactly as the review floor does.
func (s *UnblockSweeper) Sweep(ctx context.Context, tenants orchestration.TenantLister) (int, error) {
	if tenants == nil {
		return 0, &InvalidInputError{
			Field:   "tenants",
			Message: "a pass that enumerates no tenant visits nothing while claiming a clean pass",
		}
	}
	tenantIDs, err := tenants.ListTenantIDs(ctx)
	if err != nil {
		return 0, backendError("ListTenantIDs", err)
	}
	released := 0
	for _, tenantID := range tenantIDs {
		if err := ctx.Err(); err != nil {
			return released, err
		}
		// The tenant travels on the CONTEXT as well as the argument: the
		// engine guards its verbs on the bound tenant, so a sweep passing
		// only the argument readmits nothing while reporting itself healthy.
		tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)
		bindings, listErr := s.bindings.ListBindings(tenantCtx, tenantID)
		if listErr != nil {
			s.failures = append(s.failures, listErr)
			continue
		}
		for _, binding := range bindings {
			if binding == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return released, err
			}
			report, sweepErr := s.SweepProject(tenantCtx, tenantID, binding.ProjectID)
			if sweepErr != nil {
				s.failures = append(s.failures, sweepErr)
				continue
			}
			released += len(report.Released())
		}
	}
	return released, nil
}

// SweepFailures drains the per-tenant and per-project failures the last pass
// deferred, so the caller logs them where it logs everything else.
func (s *UnblockSweeper) SweepFailures() []error {
	failures := s.failures
	s.failures = nil
	return failures
}

// SweepProject re-examines every blocked task in one project and readmits
// the ones whose condition has cleared.
//
// Its error return is for faults that end the PASS — a store that cannot
// list, a readmission that cannot be written. A condition that cannot be
// decided is a FINDING: one task with an unreachable dependency must not
// stop every other task being examined.
func (s *UnblockSweeper) SweepProject(ctx context.Context, tenantID, projectID string) (UnblockReport, error) {
	if tenantID == "" || projectID == "" {
		return UnblockReport{}, &InvalidInputError{Field: "project", Message: "a sweep is scoped to one project of one tenant"}
	}
	tasks, err := s.tasks.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
		ProjectID: projectID,
		Status:    orchestration.TaskStatusBlocked,
	})
	if err != nil {
		return UnblockReport{}, err
	}

	// The forge is resolved AT MOST ONCE per pass, and only when a condition
	// needs it: a project whose blocked tasks all await a person must not
	// fail its sweep because the forge is unreachable.
	scope := &lazyScope{sweeper: s, tenantID: tenantID, projectID: projectID}

	report := UnblockReport{Findings: make([]UnblockFinding, 0, len(tasks))}
	for _, task := range tasks {
		finding, err := s.examine(ctx, tenantID, task, scope)
		if err != nil {
			return UnblockReport{}, err
		}
		report.Findings = append(report.Findings, finding)
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		return report.Findings[i].TaskID < report.Findings[j].TaskID
	})
	return report, nil
}

// examine decides one blocked task's fate.
func (s *UnblockSweeper) examine(ctx context.Context, tenantID string, task *orchestration.Task, scope *lazyScope) (UnblockFinding, error) {
	if task == nil {
		return UnblockFinding{}, &InvalidInputError{Field: "task", Message: "the store listed a nil task among a project's blocked work"}
	}
	finding := UnblockFinding{TaskID: task.ID}

	// AN OWNER'S RULING IS READ FIRST, because it answers every park it
	// applies to whatever else the record says: a needs_info or escalated
	// task carries no checkable condition, and the person it waits on has
	// already spoken. A ruling that cannot be read leaves the task to the
	// ordinary examination below — fail-closed, and reported.
	if awaitsRuling(task) {
		ruled, rulingErr := s.readmitOnRuling(ctx, tenantID, task, scope)
		var held *TaskHeldForLeadError
		if errors.As(rulingErr, &held) {
			finding.Verdict = UnblockHeldForLead
			finding.Evidence = held.Error()
			return finding, nil
		}
		if rulingErr != nil {
			s.failures = append(s.failures, rulingErr)
		}
		if ruled != nil {
			finding.Kind = UnblockOwnerRuling
			finding.Verdict = UnblockReleased
			finding.Evidence = "owner ruling by " + ruled.Author + " in comment " +
				strconv.FormatInt(ruled.ID, 10) + " — readmitted"
			return finding, nil
		}
	}

	condition, err := ParseUnblockCondition(task.BlockedReason)
	if err != nil {
		if errors.Is(err, ErrUnblockConditionAbsent) {
			// No condition was recorded, which is the ordinary shape of
			// anything the engine parked: a death nobody diagnosed cannot
			// state what would clear it. The evidence it DOES carry is the
			// diagnosis material, so it is quoted rather than summarised.
			finding.Verdict = UnblockAwaitingDiagnosis
			finding.Evidence = "no checkable condition was recorded, so a person must diagnose this " +
				"before it can return to the queue: " + task.BlockedReason
			return finding, nil
		}
		finding.Verdict = UnblockUnreadable
		finding.Evidence = err.Error()
		return finding, nil
	}
	finding.Kind = condition.Kind

	satisfied, err := s.check(ctx, task, condition, scope)
	if err != nil {
		finding.Verdict, finding.Evidence = verdictForCheckError(condition, err)
		return finding, nil
	}
	if !satisfied {
		finding.Verdict = UnblockWaiting
		finding.Evidence = condition.Describe()
		return finding, nil
	}

	// SATISFIED, AND STILL NOT NECESSARILY RETURNABLE. A task with no
	// attempts left cannot be dispatched, so readmitting it would put work
	// in the queue that dispatch refuses on every pass. The budget, not the
	// condition, is what holds it now, and the finding says so rather than
	// hiding it behind a readmission that achieves nothing.
	if task.BudgetExhausted() {
		finding.Verdict = UnblockBudgetExhausted
		finding.Evidence = condition.Describe() +
			" — satisfied, but the task has spent its attempt budget and was not readmitted"
		return finding, nil
	}

	// A HELD ISSUE STAYS WITH THE LEAD even once its condition clears. The
	// hold is read only here, when a readmission is about to happen, so a
	// sweep that releases nothing never needs the forge for it.
	if err := s.refuseIfHeld(ctx, task, scope); err != nil {
		var held *TaskHeldForLeadError
		if errors.As(err, &held) {
			finding.Verdict = UnblockHeldForLead
			finding.Evidence = held.Error()
			return finding, nil
		}
		finding.Verdict = UnblockUndecidable
		finding.Evidence = "the issue's hold could not be read, so the task stays blocked: " + err.Error()
		return finding, nil
	}

	// THE ENGINE'S VERB, so the readmission is announced as every other one
	// is and the issue's `factory:blocked` comes off with it.
	if err := s.tasks.ReadmitTask(ctx, tenantID, task.ID); err != nil {
		return UnblockFinding{}, err
	}
	finding.Verdict = UnblockReleased
	finding.Evidence = condition.Describe() + " — satisfied"
	return finding, nil
}

// verdictForCheckError keeps a gap in THIS package apart from a fault in the
// world, and both apart from a condition that was never a check's to decide.
//
// All three leave the task blocked. They differ in who has to act: an
// undecidable condition waits on an outage clearing, an unreadable one waits
// on somebody finishing a kind or fixing a record, and a human condition
// waits on the person it names — so reporting any of them as another sends
// an operator after something that is not there.
func verdictForCheckError(condition UnblockCondition, err error) (UnblockVerdict, string) {
	var human *UnblockNotCheckableError
	switch {
	case errors.As(err, &human):
		return UnblockAwaitingHuman, condition.Describe() + "; cleared by " + human.Awaiting + ", not by a check"
	case errors.Is(err, ErrUnblockKindUnknown), errors.Is(err, ErrUnblockKindUnhandled):
		return UnblockUnreadable, err.Error()
	default:
		return UnblockUndecidable, "the condition could not be decided, so the task stays blocked: " + err.Error()
	}
}

// CheckCondition reports whether one condition is satisfied for one task.
//
// THE THREE ANSWERS ARE KEPT APART, and that is why this is a method rather
// than a predicate. Satisfied and not-satisfied are the two a boolean can
// carry; the third — "no check can decide this, a person must" — is returned
// as UnblockNotCheckableError so a caller cannot receive it as an ordinary
// "not yet" and wait forever on something nothing will ever clear.
//
// Exported because "why is this one still blocked" is a question the API and
// an operator ask about a single task, and it should not require running a
// project-wide sweep to answer.
func (s *UnblockSweeper) CheckCondition(ctx context.Context, tenantID string, task *orchestration.Task, condition UnblockCondition) (bool, error) {
	if task == nil {
		return false, &InvalidInputError{Field: "task", Message: "a condition is checked in the context of the task that carries it"}
	}
	return s.check(ctx, task, condition, &lazyScope{sweeper: s, tenantID: tenantID, projectID: task.ProjectID})
}

// check validates the condition and hands it to its kind's decider.
func (s *UnblockSweeper) check(ctx context.Context, task *orchestration.Task, condition UnblockCondition, scope *lazyScope) (bool, error) {
	if err := condition.Validate(); err != nil {
		return false, err
	}
	spec := unblockKinds[condition.Kind]
	if spec.check == nil {
		return false, &UnblockNotCheckableError{Kind: condition.Kind, Awaiting: spec.awaiting}
	}
	resolved, err := scope.resolve(ctx, task)
	if err != nil {
		return false, err
	}
	return spec.check(ctx, resolved, condition)
}

// lazyScope resolves the project's forge on first use and holds it for the
// rest of the pass.
//
// Not concurrency-safe, and does not need to be: a pass is one goroutine
// walking one project's blocked tasks in order.
type lazyScope struct {
	sweeper   *UnblockSweeper
	tenantID  string
	projectID string

	forge    ForgeBackend
	binding  *ProjectBinding
	resolved bool
}

// resolve returns the scope one check runs in: the project's forge and the
// repository the task was adopted from.
func (l *lazyScope) resolve(ctx context.Context, task *orchestration.Task) (unblockScope, error) {
	repo, _, err := issueOfTask(task)
	if err != nil {
		return unblockScope{}, err
	}
	if !l.resolved {
		binding, err := l.sweeper.bindings.GetBinding(ctx, l.tenantID, l.projectID)
		if err != nil {
			return unblockScope{}, err
		}
		forge, err := l.sweeper.forges.ForgeFor(ctx, l.tenantID, binding.Forge)
		if err != nil {
			return unblockScope{}, err
		}
		if forge == nil {
			return unblockScope{}, NewForgeNotRegisteredError(binding.Forge.Type)
		}
		l.forge, l.binding, l.resolved = forge, binding, true
	}
	return unblockScope{forge: l.forge, repo: repo, now: l.sweeper.now}, nil
}

// =============================================================================
// Owner rulings
// =============================================================================

// readmitOnRuling readmits a task parked on an owner decision when its issue
// carries a ruling newer than the park, and returns the ruling comment; nil
// when there is none. See ruling.go for what counts as one.
//
// READMIT FIRST, AUDIT SECOND. The transition table lets exactly one
// readmission of one park through, so a webhook and a sweep that both find
// the same ruling cannot both readmit: the loser is refused as an invalid
// state and reports nothing. Auditing first would instead leave a record of
// a readmission that may never have happened, and — because the record is
// what marks a comment spent — a task that could never be readmitted by the
// ruling that was meant to readmit it.
func (s *UnblockSweeper) readmitOnRuling(ctx context.Context, tenantID string, task *orchestration.Task, scope *lazyScope) (*ForgeComment, error) {
	_, number, err := issueOfTask(task)
	if err != nil {
		// A task adopted from no forge issue has nowhere a ruling could be
		// posted; it is left to the ordinary examination, not failed.
		return nil, nil
	}
	resolved, err := scope.resolve(ctx, task)
	if err != nil {
		return nil, err
	}
	comments, err := resolved.forge.ListIssueComments(ctx, resolved.repo, number)
	if err != nil {
		return nil, err
	}
	ruling, ok := findRuling(task, comments, rulerSet(scope.binding, s.rulers.For(tenantID)))
	if !ok {
		return nil, nil
	}
	// A ruling on a HELD issue is the owner's decision for the lead — "delivery
	// stays lead-driven" is itself such a ruling — not a release to the factory.
	if err := s.refuseIfHeld(ctx, task, scope); err != nil {
		return nil, err
	}
	if err := s.tasks.ReadmitTask(ctx, tenantID, task.ID); err != nil {
		if errors.Is(err, orchestration.ErrTaskInvalidState) {
			// Somebody else readmitted it first — the other half of this
			// same ruling, or an operator. Either way it is not parked.
			return nil, nil
		}
		return nil, err
	}
	if err := s.tasks.SubmitTaskArtifacts(ctx, tenantID, task.ID, []orchestration.Artifact{rulingArtifact(task, ruling)}); err != nil {
		return &ruling, err
	}
	return &ruling, nil
}

// refuseIfHeld returns a TaskHeldForLeadError when the task's issue carries
// the hold label, and the read error when the issue cannot be read. A task
// adopted from no forge issue has no issue to hold it.
func (s *UnblockSweeper) refuseIfHeld(ctx context.Context, task *orchestration.Task, scope *lazyScope) error {
	_, number, err := issueOfTask(task)
	if err != nil {
		return nil
	}
	resolved, err := scope.resolve(ctx, task)
	if err != nil {
		return err
	}
	issue, err := resolved.forge.GetIssue(ctx, resolved.repo, number)
	if err != nil {
		return err
	}
	if label := heldBy(issue, HoldLabel); label != "" {
		return &TaskHeldForLeadError{TaskID: task.ID, Issue: number, Label: label}
	}
	return nil
}

// ReadmitOnRuling examines the parked task adopted from one issue and
// readmits it when the issue carries an owner's ruling. It is the webhook's
// half of the ruling path: a verified issue-comment delivery calls it so the
// ruling takes effect at once rather than on the next sweep, and the sweep
// stays the backstop for a delivery that never arrived.
//
// The delivery is only a HINT: the comments are re-read from the forge here,
// never taken from the payload. It reports whether a task was readmitted.
func (s *UnblockSweeper) ReadmitOnRuling(ctx context.Context, tenantID, projectID string, issue int) (bool, error) {
	if tenantID == "" || projectID == "" || issue <= 0 {
		return false, &InvalidInputError{Field: "issue", Message: "a ruling is read from one issue of one project"}
	}
	ctx = orchestration.ContextWithTenant(ctx, tenantID)
	tasks, err := s.tasks.ListTasks(ctx, tenantID, &orchestration.TaskFilter{
		ProjectID: projectID,
		Status:    orchestration.TaskStatusBlocked,
	})
	if err != nil {
		return false, err
	}
	scope := &lazyScope{sweeper: s, tenantID: tenantID, projectID: projectID}
	for _, task := range tasks {
		if !awaitsRuling(task) {
			continue
		}
		if _, number, err := issueOfTask(task); err != nil || number != issue {
			continue
		}
		ruling, err := s.readmitOnRuling(ctx, tenantID, task, scope)
		var held *TaskHeldForLeadError
		if errors.As(err, &held) {
			return false, nil
		}
		if err != nil {
			return ruling != nil, err
		}
		return ruling != nil, nil
	}
	return false, nil
}
