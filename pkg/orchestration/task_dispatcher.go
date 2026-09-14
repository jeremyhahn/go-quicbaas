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
	"sort"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// TaskDispatcher — the queue's second chance
// =============================================================================
//
// A TASK IS DISPATCHED MORE THAN ONCE OR IT IS NOT DISPATCHED AT ALL. Auto-spawn
// runs at task creation, when the project may be at capacity, no account may be
// free and no agent online; a task that loses that one moment stays pending
// forever with an empty SpawnedJobID and nothing to look at. The dispatcher is
// the standing answer: it sweeps every autonomous project at the poller's
// cadence, works out how many jobs the project may still run, and starts that
// many — highest priority first, oldest first within a priority.
//
// WHY IT REFUSES OUT LOUD. Every task the dispatcher looks at and declines
// leaves its answer on the record, in Task.DispatchReason, so "why is my task
// still pending?" is a field on the task rather than a hunt through logs. The
// reason is written under the store's compare-and-swap and only when it
// changed, so a project sitting at capacity for an hour writes nothing after
// the first sweep and logs nothing after the first refusal.
//
// WHY IT IS NOT THE CAPACITY BOUNDARY. The lease is (job_lease.go): the
// dispatcher counts live leases and the lease store admits or refuses. Counting
// here narrows the window the lease store's own count already states it has —
// it does not close it, and a job the dispatcher starts may still be refused
// its lease.

// TaskSpawner starts one job as an attempt at a task. The agent service's
// auto-spawn path satisfies it.
type TaskSpawner interface {
	// SpawnTask dispatches task as a job and returns the job's ID. The task
	// record's SpawnedJobID, SpawnedAt and Attempts are the spawner's to
	// update; the dispatcher reads them back on the next sweep.
	SpawnTask(ctx context.Context, task *Task) (jobID string, err error)
}

// TaskDispatchStore is the narrow task surface a dispatcher needs: the queue
// it reads and the record it writes a refusal onto. TaskStore satisfies it.
type TaskDispatchStore interface {
	ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error)
	GetTask(ctx context.Context, tenantID, taskID string) (*Task, error)
	UpdateTask(ctx context.Context, task *Task) error
}

// =============================================================================
// Dispatch reasons
// =============================================================================

// dispatchReasonAtCapacity says the project's live leases already hold every
// slot its quota allows.
func dispatchReasonAtCapacity(inflight, limit int) string {
	return "project at capacity: " + strconv.Itoa(inflight) + " of " +
		strconv.Itoa(limit) + " concurrent jobs are running"
}

// dispatchReasonAdmission renders an admission decision as the reason recorded
// on a task the dispatcher will not start.
//
// THE DECISION ALREADY WROTE THE SENTENCE. Admission knows which of the three
// axes decided and what the numbers were; re-deriving prose here from the slot
// count alone would name the project's quota for a refusal the fleet made, and
// send an operator to raise a limit that was not the one in the way.
//
// A decision with slots left has no refusal to render, and says so rather than
// returning an empty string that a caller might write onto a task as a reason.
func dispatchReasonAdmission(decision AdmissionDecision) string {
	if decision.Reason != "" {
		return "not admitted (" + decision.Axis.String() + "): " + decision.Reason
	}
	if decision.Bounded && decision.Entitlement <= 0 {
		return "not admitted (" + decision.Axis.String() + ")"
	}
	return "admitted, but there were more ready tasks than the " +
		strconv.Itoa(decision.Entitlement) + " slot(s) this sweep held"
}

// dispatchReasonDeferred says the task is held until an instant in the future.
func dispatchReasonDeferred(until int64) string {
	return "deferred until " + time.Unix(0, until).UTC().Format(time.RFC3339)
}

// dispatchReasonBudgetExhausted says the task has spent every attempt its
// budget allows.
func dispatchReasonBudgetExhausted(task *Task) string {
	return "attempt budget exhausted: " + strconv.Itoa(task.ChargedAttempts()) +
		" of " + strconv.Itoa(task.EffectiveAttemptBudget()) + " attempts charged"
}

// dispatchReasonSpawnFailed says the spawner refused the task.
//
// A REFUSAL THE ENGINE CAN NAME IS RENDERED AS THAT NAME, not as the error's
// own sentence. classifyEnvironmentFault already turns a spend cap, a
// cooldown or a full project into prose written for somebody who was not
// watching; putting err.Error() on the task instead would show the same
// condition twice in two vocabularies — once here, once in the block reason
// the next conclusion writes — and only one of them reads like an
// explanation.
//
// Everything the classifier does not recognize keeps the error's own text.
// That is the case where the engine has nothing better to say, and inventing
// a category for it would hide a real fault behind a soothing sentence.
func dispatchReasonSpawnFailed(err error) string {
	if fault, classified := classifyEnvironmentFault(err); classified && fault.Reason != "" {
		return "spawn refused: " + fault.Reason
	}
	return "spawn refused: " + err.Error()
}

// =============================================================================
// Dispatcher
// =============================================================================

// TaskDispatcherConfig wires a TaskDispatcher. Every collaborator is required.
type TaskDispatcherConfig struct {
	// Tenants enumerates the tenants a sweep visits.
	Tenants TenantLister

	// Projects lists a tenant's projects.
	Projects ProjectLister

	// Tasks is the queue the dispatcher reads and the record it writes a
	// refusal onto.
	Tasks TaskDispatchStore

	// Leases is where the inflight count comes from.
	Leases JobLeaseStore

	// LeaseTTL decides which of a project's leases still count as live. It
	// must be the TTL the lease store itself was built with, or the two
	// disagree about how many jobs are running.
	LeaseTTL time.Duration

	// Spawner starts the jobs.
	Spawner TaskSpawner

	// Admission decides how many jobs each project may hold, from all three
	// inputs at once: the project's quota, the fleet's worker slots and the
	// provider pool's usage headroom (see admission.go).
	//
	// Nil leaves the dispatcher bounded by the project quota alone, which is
	// the behaviour of every deployment that has not declared a fleet capacity
	// or pooled an account. It is optional rather than required for that
	// reason and no other: a controller is always the better answer when one
	// can be built.
	Admission *AdmissionController

	// Logger reports what each sweep did.
	Logger interfaces.Logger
}

// TaskDispatcher starts jobs for the pending, auto-spawning tasks of every
// autonomous project, bounded by the project's concurrency quota.
type TaskDispatcher struct {
	tenants   TenantLister
	projects  ProjectLister
	tasks     TaskDispatchStore
	leases    JobLeaseStore
	ttl       time.Duration
	spawner   TaskSpawner
	admission *AdmissionController
	logger    interfaces.Logger

	// now is the dispatcher's clock, injectable so a test can age a
	// deferral without waiting for it.
	now func() time.Time
}

// NewTaskDispatcher builds a dispatcher; every collaborator is required.
func NewTaskDispatcher(cfg TaskDispatcherConfig) (*TaskDispatcher, error) {
	switch {
	case cfg.Tenants == nil:
		return nil, &TaskDispatcherConfigError{Field: "tenants"}
	case cfg.Projects == nil:
		return nil, &TaskDispatcherConfigError{Field: "projects"}
	case cfg.Tasks == nil:
		return nil, &TaskDispatcherConfigError{Field: "tasks"}
	case cfg.Leases == nil:
		return nil, &TaskDispatcherConfigError{Field: "leases"}
	case cfg.LeaseTTL <= 0:
		return nil, &TaskDispatcherConfigError{Field: "lease_ttl", Message: "must be positive"}
	case cfg.Spawner == nil:
		return nil, &TaskDispatcherConfigError{Field: "spawner"}
	case cfg.Logger == nil:
		return nil, &TaskDispatcherConfigError{Field: "logger"}
	}
	return &TaskDispatcher{
		tenants:   cfg.Tenants,
		projects:  cfg.Projects,
		tasks:     cfg.Tasks,
		leases:    cfg.Leases,
		ttl:       cfg.LeaseTTL,
		spawner:   cfg.Spawner,
		admission: cfg.Admission,
		logger:    cfg.Logger,
		now:       utcNow,
	}, nil
}

// Run sweeps immediately and then once per interval until ctx is done. A
// non-positive interval is refused: a dispatcher that never sweeps is a queue
// nobody drains.
func (d *TaskDispatcher) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return &TaskDispatcherConfigError{Field: "sweep_interval", Message: "must be positive"}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := d.Sweep(ctx); err != nil && ctx.Err() == nil {
			d.logger.Warnf("task dispatcher: sweep failed: %s", err.Error())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Sweep visits every tenant's autonomous projects once. It returns an error
// only when tenants cannot be enumerated; a per-project failure is logged and
// the sweep continues, because one project's outage must not stop every other
// project's queue from draining.
func (d *TaskDispatcher) Sweep(ctx context.Context) error {
	plan, err := d.plan(ctx)
	if err != nil {
		return err
	}
	tenantIDs, err := d.tenants.ListTenantIDs(ctx)
	if err != nil {
		return &TaskDispatchSweepError{Cause: err}
	}
	for _, tenantID := range tenantIDs {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		tenantCtx := ContextWithTenant(ctx, tenantID)
		projects, listErr := d.projects.ListByTenant(tenantCtx, tenantID)
		if listErr != nil {
			d.logger.Warnf("task dispatcher: tenant %s: listing projects failed: %s", tenantID, listErr.Error())
			continue
		}
		for _, project := range projects {
			if project == nil || !project.Autonomous {
				continue
			}
			d.sweepProject(tenantCtx, project, plan)
		}
	}
	return nil
}

// SweepProject dispatches for one project now. It is what a push path calls
// after adopting work, so a nudge does not wait for the next tick. A project
// that is not autonomous is swept for nothing, exactly as the interval sweep
// treats it.
func (d *TaskDispatcher) SweepProject(ctx context.Context, tenantID, projectID string) error {
	if tenantID == "" || projectID == "" {
		return &TaskDispatcherConfigError{Field: "project", Message: "a project sweep names its tenant and project"}
	}
	plan, err := d.plan(ctx)
	if err != nil {
		return err
	}
	tenantCtx := ContextWithTenant(ctx, tenantID)
	projects, err := d.projects.ListByTenant(tenantCtx, tenantID)
	if err != nil {
		return &TaskDispatchSweepError{Cause: err}
	}
	for _, project := range projects {
		if project == nil || project.ID != projectID {
			continue
		}
		if !project.Autonomous {
			return nil
		}
		d.sweepProject(tenantCtx, project, plan)
		return nil
	}
	return &TaskDispatchProjectNotFoundError{TenantID: tenantID, ProjectID: projectID}
}

// plan computes this sweep's admission plan, or nil when no controller is
// wired.
//
// A PLAN THAT COULD NOT BE COMPUTED ENDS THE SWEEP rather than degrading it to
// the project axis. Falling back would dispatch against a fleet nobody
// measured, which is the oversubscription the controller exists to prevent —
// and it would do it precisely when the fleet is under stress, since that is
// when a store fails to answer.
func (d *TaskDispatcher) plan(ctx context.Context) (*FleetPlan, error) {
	if d.admission == nil {
		return nil, nil
	}
	plan, err := d.admission.Plan(ctx)
	if err != nil {
		return nil, &TaskDispatchSweepError{Cause: err}
	}
	return plan, nil
}

// sweepProject dispatches as many of one project's pending tasks as admission
// leaves room for, and leaves a reason on every task it declines.
func (d *TaskDispatcher) sweepProject(ctx context.Context, project *Project, plan *FleetPlan) {
	pending, err := d.tasks.ListTasks(ctx, project.TenantID, &TaskFilter{
		ProjectID: project.ID,
		Status:    TaskStatusPending,
	})
	if err != nil {
		d.logger.Warnf("task dispatcher: project %s: listing tasks failed: %s", project.ID, err.Error())
		return
	}

	now := d.now().UnixNano()
	ready, held, bound := partitionDispatchCandidates(pending, now)

	// A task naming a job that no longer exists is repaired BEFORE slots are
	// computed, so a sweep that frees one can dispatch it in the same pass.
	ready = append(ready, d.repairStrandedBindings(ctx, project, bound)...)
	sortDispatchCandidates(ready)

	slots, inflight, refusal, err := d.dispatchSlots(ctx, project, plan)
	if err != nil {
		d.logger.Warnf("task dispatcher: project %s: counting live leases failed: %s", project.ID, err.Error())
		return
	}
	if slots > len(ready) {
		slots = len(ready)
	}

	dispatched := 0
	for i, task := range ready {
		if i >= slots {
			d.refuse(ctx, project, task, refusal)
			continue
		}
		jobID, spawnErr := d.spawner.SpawnTask(ctx, task)
		if spawnErr != nil {
			d.refuse(ctx, project, task, dispatchReasonSpawnFailed(spawnErr))
			continue
		}
		dispatched++
		d.clearReason(ctx, project.TenantID, task)
		d.logger.Infof("task dispatcher: project %s: task %s dispatched as job %s", project.ID, task.ID, jobID)
	}
	for _, holding := range held {
		d.refuse(ctx, project, holding.task, holding.reason)
	}
	if len(pending) > 0 {
		d.logger.Infof("task dispatcher: project %s: pending=%d inflight=%d slots=%d dispatched=%d",
			project.ID, len(pending), inflight, slots, dispatched)
	}
}

// dispatchSlots is how many pending tasks this project may start now, the
// inflight count for the log line, and the reason to record on everything it
// cannot start.
//
// TWO SOURCES, ONE ANSWER. With a controller the answer is the plan's, which
// already considered all three inputs. Without one it is the project quota
// against the project's own live leases — today's arithmetic, kept verbatim so
// a deployment that declares no fleet capacity behaves exactly as it did.
func (d *TaskDispatcher) dispatchSlots(ctx context.Context, project *Project, plan *FleetPlan) (slots, inflight int, refusal string, err error) {
	if plan != nil {
		decision := plan.For(project.TenantID, project.ID)
		return decision.DispatchSlots(), decision.ProjectInflight, dispatchReasonAdmission(decision), nil
	}
	// THE COUNT IS READ EVEN FOR A PROJECT WITH NO QUOTA, and its failure still
	// skips the project. An unbounded project's slot count does not depend on
	// the number, but "the lease store cannot be read" is not a fact about one
	// project's quota — it is a fleet the dispatcher cannot see, and dispatching
	// into it is dispatching blind.
	inflight, err = CountLiveJobLeases(ctx, d.leases, project.TenantID, project.ID, d.now(), d.ttl)
	if err != nil {
		return 0, 0, "", err
	}
	limit := project.Quotas.MaxConcurrency
	if limit <= 0 {
		return unboundedSlots, inflight, "", nil
	}
	slots = limit - inflight
	if slots < 0 {
		slots = 0
	}
	return slots, inflight, dispatchReasonAtCapacity(inflight, limit), nil
}

// heldTask pairs a task the dispatcher will not start with the reason.
type heldTask struct {
	task   *Task
	reason string
}

// partitionDispatchCandidates splits a project's pending tasks into the ones
// the dispatcher may start now and the ones it is holding, with the reason.
// Tasks that are not the dispatcher's — no auto-spawn, or a job already
// dispatched — appear in neither: the dispatcher has no opinion about them.
func partitionDispatchCandidates(pending []*Task, now int64) (ready []*Task, held []heldTask, bound []*Task) {
	for _, task := range pending {
		switch {
		case task == nil || !task.AutoSpawn:
			continue
		case task.SpawnedJobID != "":
			// A pending task naming a job is the NORMAL state for a moment:
			// ClaimTaskSpawn writes the binding before the job starts, so the
			// claim deduplicates concurrent spawners. It is not dispatchable and
			// it is not held either — there is no reason to record about a task
			// whose attempt is under way. But if that job is gone, the task is
			// stranded silently, so it is returned for the sweep to examine
			// against the lease store rather than dropped on the floor here.
			bound = append(bound, task)
		case task.BudgetExhausted():
			held = append(held, heldTask{task: task, reason: dispatchReasonBudgetExhausted(task)})
		case task.Deferred(now):
			held = append(held, heldTask{task: task, reason: dispatchReasonDeferred(task.RetryNotBefore)})
		default:
			ready = append(ready, task)
		}
	}
	return ready, held, bound
}

// repairStrandedBindings frees the tasks whose named job no longer exists, and
// returns the ones it made dispatchable in this sweep.
//
// THE REAPER CANNOT DO THIS, AND THE CODE USED TO CLAIM IT COULD. ClaimTaskSpawn's
// own comment says a claim held by a spawner that died leaves "a task naming a
// job with no live lease, which is the state the reaper already exists to
// resolve". It does not: JobReaper.Sweep LISTS LEASES and examines the
// unreleased ones, so a task whose lease was never acquired — the process died
// between the claim and Acquire — or whose lease was correctly released has no
// lease for the reaper to find. Nothing then looks at the task again. It stays
// pending, is skipped by the binding guard above, and is never dispatched and
// never explained: pending=4 inflight=0 slots=0 dispatched=0 with not one
// refusal line, measured on the burn-in node.
//
// THE GRACE PERIOD IS THE LEASE TTL, and it is what makes this safe. The claim
// is deliberately written BEFORE the job starts, so a task legitimately names a
// job whose lease does not exist yet; clearing that binding would let a second
// spawner charge a second attempt for work already starting. The TTL is the
// system's own answer to "how long before a holder is presumed dead" — the same
// clock the reaper judges staleness by — so a binding younger than it is left
// alone no matter what the lease store says.
//
// A LEASE STORE THAT CANNOT BE READ REPAIRS NOTHING. Treating an unreadable
// store as "no live leases" would free every bound task in the fleet at once,
// which is the opposite of what a store outage should cause.
func (d *TaskDispatcher) repairStrandedBindings(ctx context.Context, project *Project, bound []*Task) []*Task {
	if len(bound) == 0 {
		return nil
	}
	now := d.now()
	cutoff := now.Add(-d.ttl).UnixNano()

	// Only tasks old enough to judge are worth a lease read.
	var candidates []*Task
	for _, task := range bound {
		if task.SpawnedAt != 0 && task.SpawnedAt < cutoff {
			candidates = append(candidates, task)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	leases, err := d.leases.List(ctx, project.TenantID, project.ID)
	if err != nil {
		d.logger.Warnf("task dispatcher: project %s: cannot check %d bound task(s) for a stranded binding: %s",
			project.ID, len(candidates), err.Error())
		return nil
	}
	alive := make(map[string]struct{}, len(leases))
	for _, lease := range leases {
		if lease != nil && lease.Live(now, d.ttl) {
			alive[lease.JobID] = struct{}{}
		}
	}

	var freed []*Task
	for _, task := range candidates {
		if _, running := alive[task.SpawnedJobID]; running {
			continue
		}
		repaired, err := d.tasks.GetTask(ctx, project.TenantID, task.ID)
		if err != nil {
			d.logger.Warnf("task dispatcher: project %s: task %s names the vanished job %s and could not be re-read: %s",
				project.ID, task.ID, task.SpawnedJobID, err.Error())
			continue
		}
		// Re-read under the same conditions: another node may have concluded
		// this attempt between the list and now, in which case there is nothing
		// to free.
		if repaired.Status != TaskStatusPending || repaired.SpawnedJobID != task.SpawnedJobID {
			continue
		}
		jobID := repaired.SpawnedJobID
		recordAttemptHistory(repaired, jobID)
		if err := d.tasks.UpdateTask(ctx, repaired); err != nil {
			d.logger.Warnf("task dispatcher: project %s: task %s names the vanished job %s and could not be freed: %s",
				project.ID, task.ID, jobID, err.Error())
			continue
		}
		d.logger.Infof("task dispatcher: project %s: task %s named job %s, which holds no live lease; the attempt is closed and the task is dispatchable again",
			project.ID, repaired.ID, jobID)
		freed = append(freed, repaired)
	}
	return freed
}

// sortDispatchCandidates orders tasks the way the dispatcher starts them:
// highest priority first, oldest first within a priority, and by ID last so
// two tasks created in the same nanosecond still have one order.
func sortDispatchCandidates(tasks []*Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		switch {
		case tasks[i].Priority != tasks[j].Priority:
			return tasks[i].Priority > tasks[j].Priority
		case tasks[i].CreatedAt != tasks[j].CreatedAt:
			return tasks[i].CreatedAt < tasks[j].CreatedAt
		default:
			return tasks[i].ID < tasks[j].ID
		}
	})
}

// refuse records why a task was not dispatched and says so once. A reason
// that has not changed is neither written nor logged: a project sitting at
// capacity must not rewrite its queue on every sweep.
func (d *TaskDispatcher) refuse(ctx context.Context, project *Project, task *Task, reason string) {
	if task.DispatchReason == reason {
		return
	}
	changed, err := SetTaskDispatchReason(ctx, d.tasks, project.TenantID, task.ID, reason)
	if err != nil {
		d.logger.Warnf("task dispatcher: project %s: task %s: recording the dispatch reason failed: %s",
			project.ID, task.ID, err.Error())
		return
	}
	task.DispatchReason = reason
	if changed {
		d.logger.Warnf("task dispatcher: project %s: task %s not dispatched: %s", project.ID, task.ID, reason)
	}
}

// clearReason removes a stale refusal from a task that has just been
// dispatched.
func (d *TaskDispatcher) clearReason(ctx context.Context, tenantID string, task *Task) {
	if task.DispatchReason == "" {
		return
	}
	if _, err := SetTaskDispatchReason(ctx, d.tasks, tenantID, task.ID, ""); err != nil {
		d.logger.Warnf("task dispatcher: task %s: clearing the dispatch reason failed: %s", task.ID, err.Error())
		return
	}
	task.DispatchReason = ""
}

// TaskDispatchReasonStore is the narrow task surface a dispatch-reason write
// needs. TaskStore and TaskDispatchStore both satisfy it.
type TaskDispatchReasonStore interface {
	GetTask(ctx context.Context, tenantID, taskID string) (*Task, error)
	UpdateTask(ctx context.Context, task *Task) error
}

// SetTaskDispatchReason writes reason onto a task's DispatchReason under the
// store's compare-and-swap, reporting whether it changed anything.
//
// THERE IS ONE WRITER OF THIS FIELD, and both paths that answer "why is this
// task not running?" go through it: the refusal auto-spawn records at
// creation, and the standing answer the dispatcher records on every sweep.
// Two writers would be two chances to disagree about when a reason is stale.
//
// Three rules, each of which one of those callers needs:
//
//   - An unchanged reason is not written. A project sitting at capacity for
//     an hour must not rewrite its queue on every sweep.
//   - A task that has since been dispatched is not given a NEW reason — its
//     refusal is stale the moment a job exists for it — but it may always
//     have one CLEARED, which is exactly what a successful dispatch does.
//   - A conflict re-reads and re-applies rather than overwriting whatever
//     changed underneath, bounded by cas.MaxAttempts.
func SetTaskDispatchReason(ctx context.Context, store TaskDispatchReasonStore, tenantID, taskID, reason string) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		current, err := store.GetTask(ctx, tenantID, taskID)
		if err != nil {
			return false, err
		}
		if current.DispatchReason == reason {
			return false, nil
		}
		if reason != "" && current.SpawnedJobID != "" {
			return false, nil
		}
		current.DispatchReason = reason
		err = store.UpdateTask(ctx, current)
		if err == nil {
			return true, nil
		}
		var conflict *TaskVersionConflictError
		if !errors.As(err, &conflict) {
			return false, err
		}
		lastErr = err
	}
	return false, lastErr
}

// =============================================================================
// The spawn claim
// =============================================================================
//
// A TASK'S OPEN ATTEMPT IS CLAIMED BEFORE THE JOB EXISTS, NOT AFTER. Two
// spawners look at one pending task all the time — auto-spawn runs at creation
// while the dispatcher is mid-sweep over a list that was read a moment earlier,
// and a push sweep can arrive between the two. Each of them decides "no job yet"
// from the snapshot it holds, and a spawner that starts the job first and
// records it second gives the other one a whole job's worth of window to decide
// the same thing (#670).
//
// The lease is not the deduplicator. It is the capacity gate, and by the time it
// refuses the second job the attempt is already spent: both spawners charged the
// task, the second one's record write lost the version check, and the backoff
// that follows pushes the next dispatch out past the point where a restart could
// still resume the work.
//
// So the claim writes the job's identity onto the task FIRST, under the store's
// version check, and the job is started with the id the claim recorded.
// Task.SpawnedJobID already means "the task's open attempt" — it is what
// recordAttemptHistory clears when the attempt concludes — so the claim is not a
// new piece of state, only that state written a moment earlier. A spawner whose
// claim finds a job already there is not a failure to report: it observes the
// winner's job and starts nothing.
//
// A claim that is never released would be worse than the race it removes, so the
// only path that takes one and then cannot start its job releases it
// (ReleaseTaskSpawnClaim). What that leaves — a claim held by a spawner whose
// process died between the claim and the run — is a task naming a job with no
// live lease, which is the state the reaper already exists to resolve, rather
// than a new kind of stuck.

// TaskSpawnClaim is the answer to "may I be this task's open attempt?".
//
// Claimed distinguishes the winner from an observer: the winner minted JobID
// and must start that job, an observer is reporting the job the task already
// names and must start nothing. JobID is the task's open attempt either way,
// which is what makes a spawn idempotent to its caller.
type TaskSpawnClaim struct {
	// JobID is the job the task names as its open attempt.
	JobID string

	// Attempt is the ordinal this attempt was charged, read back from the
	// record the claim wrote rather than from the caller's snapshot.
	Attempt int

	// Claimed is true only for the spawner that took the claim.
	Claimed bool
}

// ClaimTaskSpawn claims task as jobID's attempt, or reports the attempt some
// other spawner already claimed. On either outcome task is refreshed from the
// stored record, so the caller's copy carries the version, the job and the
// attempt count the store agreed to.
//
// The claim charges the attempt, because the attempt and the job it runs as are
// one decision: charging them separately is what let one task be charged twice.
func ClaimTaskSpawn(ctx context.Context, store TaskDispatchReasonStore, task *Task, jobID string, now int64) (TaskSpawnClaim, error) {
	switch {
	case store == nil:
		return TaskSpawnClaim{}, &TaskSpawnClaimError{Message: "a claim needs a task store"}
	case task == nil:
		return TaskSpawnClaim{}, &TaskSpawnClaimError{Message: "a claim names the task it is an attempt at"}
	case jobID == "":
		return TaskSpawnClaim{}, &TaskSpawnClaimError{TaskID: task.ID, Message: "a claim names the job it claims for"}
	}

	var lastErr error
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		current, err := store.GetTask(ctx, task.TenantID, task.ID)
		if err != nil {
			return TaskSpawnClaim{}, err
		}
		if current.SpawnedJobID != "" {
			*task = *current
			return TaskSpawnClaim{JobID: current.SpawnedJobID, Attempt: current.Attempts}, nil
		}

		current.SpawnedJobID = jobID
		current.SpawnedAt = now
		current.Attempts++
		// A task that has just been dispatched is not waiting for anything,
		// so the reason it was waiting is stale from here on.
		current.DispatchReason = ""

		err = store.UpdateTask(ctx, current)
		if err == nil {
			*task = *current
			return TaskSpawnClaim{JobID: jobID, Attempt: current.Attempts, Claimed: true}, nil
		}
		var conflict *TaskVersionConflictError
		if !errors.As(err, &conflict) {
			return TaskSpawnClaim{}, err
		}
		// Someone wrote between the read and the write. Re-read: the usual
		// reason is the other spawner's claim, which the next pass observes.
		lastErr = err
	}
	return TaskSpawnClaim{}, &TaskSpawnClaimError{
		TaskID:  task.ID,
		Message: "the record changed under every attempt to claim it",
		Cause:   lastErr,
	}
}

// ReleaseTaskSpawnClaim gives back a claim whose job could not be started,
// refunding the attempt it charged. A task that no longer names jobID has
// nothing to release — the attempt concluded, or another claim replaced it —
// and releasing then would hand the task's open attempt away.
func ReleaseTaskSpawnClaim(ctx context.Context, store TaskDispatchReasonStore, task *Task, jobID string) error {
	switch {
	case store == nil:
		return &TaskSpawnClaimError{Message: "a release needs a task store"}
	case task == nil:
		return &TaskSpawnClaimError{Message: "a release names the task it is giving back"}
	case jobID == "":
		return &TaskSpawnClaimError{TaskID: task.ID, Message: "a release names the job it claimed for"}
	}

	var lastErr error
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		current, err := store.GetTask(ctx, task.TenantID, task.ID)
		if err != nil {
			return err
		}
		if current.SpawnedJobID != jobID {
			*task = *current
			return nil
		}

		current.SpawnedJobID = ""
		current.SpawnedAt = 0
		if current.Attempts > 0 {
			current.Attempts--
		}

		err = store.UpdateTask(ctx, current)
		if err == nil {
			*task = *current
			return nil
		}
		var conflict *TaskVersionConflictError
		if !errors.As(err, &conflict) {
			return err
		}
		lastErr = err
	}
	return &TaskSpawnClaimError{
		TaskID:  task.ID,
		Message: "the record changed under every attempt to release the claim",
		Cause:   lastErr,
	}
}

// =============================================================================
// Capacity, shared with the task-source poller
// =============================================================================

// CountLiveJobLeases returns how much of a project's Quotas.MaxConcurrency
// budget is spent at now: its live leases, excluding the ones that are off
// the budget.
//
// IT IS A BUDGET COUNT AND NOT A JOB COUNT, which is the distinction both
// callers want — each of them subtracts it from MaxConcurrency to decide how
// much more work to admit. It answers through JobLease.CountsAgainstBudget,
// the same predicate the capacity store's own admission check uses, so the
// three readers of one budget cannot disagree about it.
func CountLiveJobLeases(ctx context.Context, leases JobLeaseStore, tenantID, projectID string, now time.Time, ttl time.Duration) (int, error) {
	held, err := leases.List(ctx, tenantID, projectID)
	if err != nil {
		return 0, err
	}
	live := 0
	for _, lease := range held {
		if lease.CountsAgainstBudget(now, ttl) {
			live++
		}
	}
	return live, nil
}

// CountPendingUndispatched returns how many of a project's tasks are waiting
// for the dispatcher: pending, asking to be spawned, and not yet spawned.
func CountPendingUndispatched(tasks []*Task) int {
	waiting := 0
	for _, task := range tasks {
		if task != nil && task.AutoSpawn && task.SpawnedJobID == "" {
			waiting++
		}
	}
	return waiting
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrTaskDispatcherConfig indicates a dispatcher missing a collaborator.
	ErrTaskDispatcherConfig = errors.New("task dispatcher: invalid configuration")

	// ErrTaskDispatchSweep indicates a sweep could not enumerate its tenants
	// or a tenant's projects.
	ErrTaskDispatchSweep = errors.New("task dispatcher: sweep failed")

	// ErrTaskDispatchProjectNotFound indicates a project sweep named a
	// project the tenant does not have.
	ErrTaskDispatchProjectNotFound = errors.New("task dispatcher: project not found")

	// ErrTaskSpawnClaim indicates a spawn claim that could not be decided:
	// the caller does not hold the task's open attempt.
	ErrTaskSpawnClaim = errors.New("task spawn claim: undecided")
)

// TaskDispatcherConfigError reports a dispatcher built without a collaborator.
type TaskDispatcherConfigError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *TaskDispatcherConfigError) Error() string {
	if e.Message == "" {
		return ErrTaskDispatcherConfig.Error() + ": " + e.Field + " is required"
	}
	return ErrTaskDispatcherConfig.Error() + ": " + e.Field + " " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskDispatcherConfigError) Unwrap() error { return ErrTaskDispatcherConfig }

// TaskDispatchSweepError reports a sweep that could not read what it sweeps.
type TaskDispatchSweepError struct {
	Cause error
}

// Error implements the error interface.
func (e *TaskDispatchSweepError) Error() string {
	return ErrTaskDispatchSweep.Error() + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *TaskDispatchSweepError) Unwrap() []error {
	return []error{ErrTaskDispatchSweep, e.Cause}
}

// TaskDispatchProjectNotFoundError reports a SweepProject for a project the
// tenant does not have.
type TaskDispatchProjectNotFoundError struct {
	TenantID  string
	ProjectID string
}

// Error implements the error interface.
func (e *TaskDispatchProjectNotFoundError) Error() string {
	return ErrTaskDispatchProjectNotFound.Error() + ": tenant " + e.TenantID + " has no project " + e.ProjectID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskDispatchProjectNotFoundError) Unwrap() error { return ErrTaskDispatchProjectNotFound }

// TaskSpawnClaimError reports a spawn claim or release that could not be
// decided — a caller that named nothing to claim with, or a record too
// contended to settle. Fail-closed: the caller holds nothing.
type TaskSpawnClaimError struct {
	// TaskID names the task, when the caller named one.
	TaskID string

	// Message says what could not be decided.
	Message string

	// Cause is the store error the last attempt saw, when there was one.
	Cause error
}

// Error implements the error interface.
func (e *TaskSpawnClaimError) Error() string {
	msg := ErrTaskSpawnClaim.Error() + ": " + e.Message
	if e.TaskID != "" {
		msg += ": task=" + e.TaskID
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel, and the cause when there is one, for
// errors.Is/As support.
func (e *TaskSpawnClaimError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrTaskSpawnClaim}
	}
	return []error{ErrTaskSpawnClaim, e.Cause}
}

// =============================================================================
// The agent service as a spawner
// =============================================================================

var _ TaskSpawner = (*agentServiceImpl)(nil)

// SpawnTask implements TaskSpawner: it dispatches an already-persisted task
// as a job, exactly as auto-spawn does at creation time.
//
// THERE IS ONE DISPATCH PATH, not two. The dispatcher's second chance at a
// task must route it the way its first chance would have, choose the same
// runner, and charge the same attempt; anything reimplemented here would be a
// second answer to those questions. `Task.Runner` and `Task.Routing` are on
// the record precisely so a later dispatch needs no request to read them off.
func (s *agentServiceImpl) SpawnTask(ctx context.Context, task *Task) (string, error) {
	if task == nil {
		return "", &AgentServiceError{Sentinel: ErrAgentServiceUnavailable, Message: "a spawn names the task it is an attempt at"}
	}
	if err := s.autoSpawnTask(ctx, task); err != nil {
		return "", err
	}
	if task.SpawnedJobID == "" {
		// The spawn claim records the attempt before the job exists, so a
		// spawn that reports no error names a job here — its own, or the one
		// another spawner claimed first. Reporting success without one would
		// leave the dispatcher free to start a second job on the next sweep.
		return "", &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "a job was started for task " + task.ID + " but the task record did not take it",
		}
	}
	return task.SpawnedJobID, nil
}
