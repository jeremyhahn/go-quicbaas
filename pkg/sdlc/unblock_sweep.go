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
}

// UnblockSweeper re-examines blocked tasks and readmits the ones whose typed
// condition has been satisfied.
type UnblockSweeper struct {
	tasks    BlockedTaskService
	bindings BindingReader
	forges   ForgeResolver
	now      func() time.Time

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
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &UnblockSweeper{tasks: cfg.Tasks, bindings: cfg.Bindings, forges: cfg.Forges, now: now}, nil
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
		l.forge, l.resolved = forge, true
	}
	return unblockScope{forge: l.forge, repo: repo, now: l.sweeper.now}, nil
}
