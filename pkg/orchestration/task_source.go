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

package orchestration

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// TaskSource — where work comes from
// =============================================================================
//
// A TaskSource is the seam through which a module supplies work to the engine
// without the engine learning what the module is. The engine asks every
// registered source, per project, "is there anything new?", and turns each
// answer into a Task with AutoSpawn set, so the project's DefaultWorkflow
// picks it up. The source declares the provenance kind its tasks carry; the
// engine records that kind, indexes on it, and never interprets it.
//
// Re-polling is idempotent by construction rather than by bookkeeping: the
// task store's guarded source index refuses a second task for the same
// (kind, ref) within a tenant and project, so a source may return the same
// spec on every sweep and only the first sweep creates anything.

// TaskSource supplies task specifications for a project.
type TaskSource interface {
	// Name identifies the source in the registry and in logs. It must be
	// unique among registered sources.
	Name() string

	// Poll returns every task the source currently has for the project. A
	// source that cannot reach its backing system returns a
	// TaskSourceUnavailableError so the poller can report the outage once
	// and move on; every other error is reported on every sweep.
	Poll(ctx context.Context, project *Project) ([]TaskSourceSpec, error)
}

// TaskSourceSpec is one unit of work a TaskSource offers. Source is the provenance
// the engine records on the Task; its Kind must be the kind the source
// registered with, and its Ref must be unique for the origin within the
// project.
type TaskSourceSpec struct {
	Source        TaskSourceRef
	Subject       string
	Description   string
	Priority      int
	AttemptBudget int
	Metadata      map[string]string

	// Runner names the JobRunner that drives an attempt at the task. A
	// module supplying work its own runner drives names it here; empty
	// leaves the task to the engine's lead-driven "workflow" runner.
	Runner string

	// Park, when set, adopts the task ALREADY BLOCKED with this reason rather
	// than pending.
	//
	// IT IS ONE WRITE, AND THAT IS THE REASON IT LIVES ON THE SPEC. A source
	// that knows the work cannot start yet — an issue waiting on another
	// repository's release, say — could create the task and then block it, but
	// between those two writes the task is pending and auto-spawning, and the
	// dispatcher may start the very job the park exists to prevent. Declaring
	// the park with the spec closes that window: see CreateTaskRequest.Park.
	//
	// A parked task keeps its AutoSpawn bit and is NOT spawned. The bit is what
	// makes readmission dispatch it, so clearing it would park the task
	// permanently under a condition that was expected to clear.
	Park *BlockReason
}

// Validate checks the spec against the kind its source registered with.
func (spec *TaskSourceSpec) Validate(kind TaskSourceKind) error {
	if spec == nil {
		return &TaskSourceSpecInvalidError{Field: "spec", Message: "must not be nil"}
	}
	if spec.Subject == "" {
		return &TaskSourceSpecInvalidError{Field: "subject", Message: "must not be empty"}
	}
	if spec.Source.Ref == "" {
		return &TaskSourceSpecInvalidError{Field: "source.ref", Message: "must not be empty"}
	}
	if spec.Source.Kind != kind {
		return &TaskSourceSpecInvalidError{
			Field:   "source.kind",
			Message: "is " + string(spec.Source.Kind) + " but the source registered " + string(kind),
		}
	}
	return nil
}

// RegisteredTaskSource pairs a source with the provenance kind it declared.
type RegisteredTaskSource struct {
	Kind   TaskSourceKind
	Source TaskSource
}

// =============================================================================
// Registry
// =============================================================================

// TaskSourceRegistry holds task sources by name. Registration is append-only:
// a name registers once, and a second registration under the same name is
// refused so two modules cannot silently shadow each other.
type TaskSourceRegistry struct {
	mu     sync.RWMutex
	byName map[string]RegisteredTaskSource
}

// NewTaskSourceRegistry creates an empty registry.
func NewTaskSourceRegistry() *TaskSourceRegistry {
	return &TaskSourceRegistry{byName: make(map[string]RegisteredTaskSource)}
}

// Register adds src under its name, declaring kind as the provenance of every
// task it emits. The kind becomes a valid TaskSourceKind engine-wide; two
// sources may share a kind, since the guarded index keys on (kind, ref) and
// sources sharing a kind share that reference space by declaration.
func (r *TaskSourceRegistry) Register(kind TaskSourceKind, src TaskSource) error {
	if src == nil {
		return &TaskSourceRegistrationError{Message: "source must not be nil"}
	}
	name := src.Name()
	if name == "" {
		return &TaskSourceRegistrationError{Message: "source name must not be empty"}
	}
	if kind == "" {
		return &TaskSourceRegistrationError{Name: name, Message: "kind must not be empty"}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[name]; exists {
		return &TaskSourceAlreadyRegisteredError{Name: name}
	}
	r.byName[name] = RegisteredTaskSource{Kind: kind, Source: src}
	taskSourceKinds.add(kind)
	return nil
}

// Lookup returns the source registered under name.
func (r *TaskSourceRegistry) Lookup(name string) (RegisteredTaskSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.byName[name]
	return reg, ok
}

// Sources returns every registered source, ordered by name so a sweep visits
// them in the same order every time.
func (r *TaskSourceRegistry) Sources() []RegisteredTaskSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]RegisteredTaskSource, 0, len(names))
	for _, name := range names {
		out = append(out, r.byName[name])
	}
	return out
}

// taskSourceKindSet is the engine-wide set of kinds registered sources have
// declared. It is separate from any one registry so a kind stays valid for
// the task store no matter which registry the declaring source lives in.
type taskSourceKindSet struct {
	mu    sync.RWMutex
	kinds map[TaskSourceKind]struct{}
}

func (s *taskSourceKindSet) add(kind TaskSourceKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kinds == nil {
		s.kinds = make(map[TaskSourceKind]struct{})
	}
	s.kinds[kind] = struct{}{}
}

func (s *taskSourceKindSet) registered(kind TaskSourceKind) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.kinds[kind]
	return ok
}

// taskSourceKinds is the engine-wide declared-kind set.
var taskSourceKinds taskSourceKindSet

// kindRegistered reports whether any registered source declared kind. It is
// a method on the registry so TaskSourceKind.Validate reads naturally, but
// the set itself is engine-wide.
func (r *TaskSourceRegistry) kindRegistered(kind TaskSourceKind) bool {
	return taskSourceKinds.registered(kind)
}

// taskSources is the engine's registry: the one a module registers into and
// the one the server's poller sweeps.
var taskSources = NewTaskSourceRegistry()

// RegisterTaskSource registers src in the engine's registry, declaring kind
// as the provenance of the tasks it emits.
func RegisterTaskSource(kind TaskSourceKind, src TaskSource) error {
	return taskSources.Register(kind, src)
}

// LookupTaskSource returns the engine-registered source under name.
func LookupTaskSource(name string) (RegisteredTaskSource, bool) {
	return taskSources.Lookup(name)
}

// TaskSources returns every engine-registered source, ordered by name.
func TaskSources() []RegisteredTaskSource {
	return taskSources.Sources()
}

// DefaultTaskSourceRegistry returns the engine's registry, for a poller that
// should sweep exactly what modules registered.
func DefaultTaskSourceRegistry() *TaskSourceRegistry {
	return taskSources
}

// =============================================================================
// Poller
// =============================================================================

// TaskCreator is the one verb the poller needs from the agent service.
type TaskCreator interface {
	CreateTask(ctx context.Context, req *CreateTaskRequest) (*Task, error)
}

// TaskQueueReader lists a project's tasks. The poller reads the queue to
// learn how much of a project's concurrency the tasks it already adopted
// have spoken for; TaskStore satisfies it.
type TaskQueueReader interface {
	ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error)
}

// TenantLister enumerates the tenants whose projects a sweep visits.
type TenantLister interface {
	ListTenantIDs(ctx context.Context) ([]string, error)
}

// ProjectLister lists a tenant's projects; ProjectStore satisfies it.
type ProjectLister interface {
	ListByTenant(ctx context.Context, tenantID string) ([]*Project, error)
}

// TaskSourcePollerConfig wires a TaskSourcePoller.
type TaskSourcePollerConfig struct {
	Sources  *TaskSourceRegistry
	Tenants  TenantLister
	Projects ProjectLister
	Tasks    TaskCreator
	Logger   interfaces.Logger

	// Queue and Leases are what the adoption cap is computed from: the
	// tasks already waiting and the jobs already running. Both are
	// required — a poller that cannot count adopts without a bound, which
	// is the flood the cap exists to prevent.
	Queue  TaskQueueReader
	Leases JobLeaseStore

	// LeaseTTL decides which of a project's leases still count as live. It
	// must be the TTL the lease store itself was built with.
	LeaseTTL time.Duration

	// Admission decides how much of the fleet each project may hold, from all
	// three inputs at once: the project's quota, the deployment's worker slots
	// and the provider pool's usage headroom (see admission.go).
	//
	// Nil bounds adoption by the project quota alone, which is what every
	// deployment that has declared no fleet capacity and pooled no account
	// already does.
	Admission *AdmissionController
}

// TaskSourcePoller sweeps every registered source against every autonomous
// project and creates one auto-spawning Task per new spec, up to the
// project's remaining concurrency.
type TaskSourcePoller struct {
	sources   *TaskSourceRegistry
	tenants   TenantLister
	projects  ProjectLister
	tasks     TaskCreator
	queue     TaskQueueReader
	leases    JobLeaseStore
	ttl       time.Duration
	admission *AdmissionController
	logger    interfaces.Logger

	// now is the poller's clock, injectable so a test can age a heartbeat
	// without waiting for it.
	now func() time.Time

	// outages tracks which sources are currently unreachable so an outage is
	// reported when it begins and when it ends, not on every sweep.
	outageMu sync.Mutex
	outages  map[string]struct{}
}

// NewTaskSourcePoller builds a poller; every collaborator is required.
func NewTaskSourcePoller(cfg TaskSourcePollerConfig) (*TaskSourcePoller, error) {
	switch {
	case cfg.Sources == nil:
		return nil, &TaskSourcePollerConfigError{Field: "sources"}
	case cfg.Tenants == nil:
		return nil, &TaskSourcePollerConfigError{Field: "tenants"}
	case cfg.Projects == nil:
		return nil, &TaskSourcePollerConfigError{Field: "projects"}
	case cfg.Tasks == nil:
		return nil, &TaskSourcePollerConfigError{Field: "tasks"}
	case cfg.Queue == nil:
		return nil, &TaskSourcePollerConfigError{Field: "queue"}
	case cfg.Leases == nil:
		return nil, &TaskSourcePollerConfigError{Field: "leases"}
	case cfg.LeaseTTL <= 0:
		return nil, &TaskSourcePollerConfigError{Field: "lease_ttl", Message: "must be positive"}
	case cfg.Logger == nil:
		return nil, &TaskSourcePollerConfigError{Field: "logger"}
	}
	return &TaskSourcePoller{
		sources:   cfg.Sources,
		tenants:   cfg.Tenants,
		projects:  cfg.Projects,
		tasks:     cfg.Tasks,
		queue:     cfg.Queue,
		leases:    cfg.Leases,
		ttl:       cfg.LeaseTTL,
		admission: cfg.Admission,
		logger:    cfg.Logger,
		now:       utcNow,
		outages:   make(map[string]struct{}),
	}, nil
}

// Run sweeps immediately and then once per interval until ctx is done. A
// non-positive interval is refused: a poller that never sweeps is a source
// nobody reads.
func (p *TaskSourcePoller) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return &TaskSourcePollerConfigError{Field: "sweep_interval", Message: "must be positive"}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := p.Sweep(ctx); err != nil && ctx.Err() == nil {
			p.logger.Warnf("task sources: sweep failed: %s", err.Error())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Sweep visits every tenant's projects once. It returns an error only when
// tenants cannot be enumerated; per-project and per-source failures are
// logged and the sweep continues, because one source's outage must not stop
// every other source from being read.
func (p *TaskSourcePoller) Sweep(ctx context.Context) error {
	sources := p.sources.Sources()
	if len(sources) == 0 {
		return nil
	}
	plan, err := p.plan(ctx)
	if err != nil {
		return err
	}
	tenantIDs, err := p.tenants.ListTenantIDs(ctx)
	if err != nil {
		return &TaskSourceSweepError{Cause: err}
	}
	for _, tenantID := range tenantIDs {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		projects, listErr := p.projects.ListByTenant(ContextWithTenant(ctx, tenantID), tenantID)
		if listErr != nil {
			p.logger.Warnf("task sources: tenant %s: listing projects failed: %s", tenantID, listErr.Error())
			continue
		}
		for _, project := range projects {
			if project == nil || !project.Autonomous {
				continue
			}
			// Adoption is intake: every task it creates is new work. A
			// parked project is passed over quietly on the interval sweep,
			// which would otherwise say so once a tick for as long as the
			// project stays parked; the push path below says so out loud.
			if refusal := project.RefuseIntake("adoption of new work"); refusal != nil {
				p.logger.Debugf("task sources: %s", refusal.Error())
				continue
			}
			p.sweepProject(ContextWithTenant(ctx, tenantID), project, sources, plan)
		}
	}
	return nil
}

// SweepProject polls every registered source for one project now. It is
// what a module's push path (a verified webhook delivery, say) calls so the
// poller stays the single path a source's work becomes a Task through; the
// interval sweep is unaffected. A project that is not autonomous is swept for
// nothing, exactly as the interval sweep treats it. A parked project adopts
// nothing and is refused with a *ProjectParkedError, so the caller can tell
// a closed intake from an empty one.
func (p *TaskSourcePoller) SweepProject(ctx context.Context, tenantID, projectID string) error {
	if tenantID == "" || projectID == "" {
		return &TaskSourcePollerConfigError{Field: "project", Message: "a project sweep names its tenant and project"}
	}
	sources := p.sources.Sources()
	if len(sources) == 0 {
		return nil
	}
	plan, err := p.plan(ctx)
	if err != nil {
		return err
	}
	tenantCtx := ContextWithTenant(ctx, tenantID)
	projects, err := p.projects.ListByTenant(tenantCtx, tenantID)
	if err != nil {
		return &TaskSourceSweepError{Cause: err}
	}
	for _, project := range projects {
		if project == nil || project.ID != projectID {
			continue
		}
		if !project.Autonomous {
			return nil
		}
		if refusal := project.RefuseIntake("adoption of new work"); refusal != nil {
			p.logger.Infof("task sources: %s", refusal.Error())
			return refusal
		}
		p.sweepProject(tenantCtx, project, sources, plan)
		return nil
	}
	return &TaskSourceProjectNotFoundError{TenantID: tenantID, ProjectID: projectID}
}

// offeredSpec pairs a spec with the source that offered it, so a project's
// whole offering can be ordered before any of it is adopted.
type offeredSpec struct {
	reg  RegisteredTaskSource
	spec *TaskSourceSpec
}

// sweepProject polls every source for one project and adopts as much of what
// they offer as the project has room for.
//
// THE CAP IS THE POINT. A forge with fifty labelled issues offers fifty specs
// on the first sweep, and a project that may run one job at a time would
// otherwise adopt all fifty — fifty pending tasks, forty-nine of them refused
// on every dispatcher sweep for as long as the label is on the issue. Adopting
// only what the project can still run leaves the rest where they came from,
// where a person can still see and reprioritise them.
func (p *TaskSourcePoller) sweepProject(ctx context.Context, project *Project, sources []RegisteredTaskSource, plan *FleetPlan) {
	capacity, bounded, err := p.adoptionCapacity(ctx, project, plan)
	if err != nil {
		p.logger.Warnf("task sources: project %s: reading the adoption capacity failed: %s", project.ID, err.Error())
		return
	}

	offered := make([]offeredSpec, 0, len(sources))
	for _, reg := range sources {
		if ctx.Err() != nil {
			return
		}
		specs, pollErr := reg.Source.Poll(ctx, project)
		if pollErr != nil {
			p.reportPollError(reg.Source.Name(), project, pollErr)
			continue
		}
		p.clearOutage(reg.Source.Name())
		for i := range specs {
			offered = append(offered, offeredSpec{reg: reg, spec: &specs[i]})
		}
	}

	// Highest priority first, and stable so two specs of equal priority keep
	// the order their sources offered them in.
	sort.SliceStable(offered, func(i, j int) bool {
		return offered[i].spec.Priority > offered[j].spec.Priority
	})

	adopted, parkedAdopted, reserved, refused := 0, 0, 0, 0
	for _, offer := range offered {
		if ctx.Err() != nil {
			return
		}
		// A PARKED SPEC SPENDS NO SLOT. It is adopted blocked and runs
		// nothing until a person readmits it, so counting it against the cap
		// would let a backlog of questions starve the ready work behind it —
		// and refusing it would leave the question with no task, which is
		// exactly the state that hid it from the attention inbox.
		parked := offer.spec.Park != nil
		if bounded && !parked && adopted >= capacity {
			refused++
			continue
		}
		switch p.adopt(ctx, project, offer.reg, offer.spec) {
		case adoptCreated:
			if parked {
				parkedAdopted++
			} else {
				adopted++
			}
		case adoptReserved:
			reserved++
		case adoptFailed:
		}
	}
	if len(offered) > 0 {
		// RESERVED IS ITS OWN COUNT because seen=1 adopted=0 refused=0 told an
		// operator nothing: neither counter covered "a task already holds this
		// origin", so a sweep working exactly as designed read as a poller
		// that had silently stopped (#652).
		p.logger.Infof("task sources: project %s: seen=%d adopted=%d parked=%d reserved=%d refused=%d",
			project.ID, len(offered), adopted, parkedAdopted, reserved, refused)
	}
}

// plan computes this sweep's admission plan, or nil when no controller is
// wired.
//
// A PLAN THAT COULD NOT BE COMPUTED ENDS THE SWEEP. Adopting against the
// project axis alone because the fleet could not be measured would flood the
// queue exactly when the fleet is least able to drain it.
func (p *TaskSourcePoller) plan(ctx context.Context) (*FleetPlan, error) {
	if p.admission == nil {
		return nil, nil
	}
	plan, err := p.admission.Plan(ctx)
	if err != nil {
		return nil, &TaskSourceSweepError{Cause: err}
	}
	return plan, nil
}

// adoptionCapacity is how many new tasks the project has room for this sweep.
//
// WITH A CONTROLLER it is the plan's answer, which already netted off the
// project's running jobs, its queued tasks, its fair share of the fleet's free
// worker slots and its pool's usage headroom. WITHOUT ONE it is today's
// arithmetic verbatim — the concurrency quota less the jobs running and the
// tasks already waiting — so a deployment that declared no fleet capacity
// adopts exactly what it adopted before. An unbounded project has no cap and
// nothing is read at all.
func (p *TaskSourcePoller) adoptionCapacity(ctx context.Context, project *Project, plan *FleetPlan) (capacity int, bounded bool, err error) {
	if plan != nil {
		decision := plan.For(project.TenantID, project.ID)
		if !decision.Bounded {
			return 0, false, nil
		}
		return decision.AdoptionSlots(), true, nil
	}
	limit := project.Quotas.MaxConcurrency
	if limit <= 0 {
		return 0, false, nil
	}
	inflight, err := CountLiveJobLeases(ctx, p.leases, project.TenantID, project.ID, p.now(), p.ttl)
	if err != nil {
		return 0, false, err
	}
	pending, err := p.queue.ListTasks(ctx, project.TenantID, &TaskFilter{
		ProjectID: project.ID,
		Status:    TaskStatusPending,
	})
	if err != nil {
		return 0, false, err
	}
	capacity = limit - inflight - CountPendingUndispatched(pending)
	if capacity < 0 {
		capacity = 0
	}
	return capacity, true, nil
}

// adoptOutcome is what one offered spec produced.
type adoptOutcome int

const (
	// adoptFailed is a spec the sweep could not turn into a task. Every case
	// is reported where it happened.
	adoptFailed adoptOutcome = iota

	// adoptCreated is a spec that became a task.
	adoptCreated

	// adoptReserved is a spec whose origin a task already holds.
	adoptReserved
)

// adopt turns one spec into a Task, reporting what became of it. A spec whose
// origin a live task already holds is the normal outcome of re-polling; a task
// created but not yet spawned is real and stays queryable, so it is reported
// but does not fail the sweep.
func (p *TaskSourcePoller) adopt(ctx context.Context, project *Project, reg RegisteredTaskSource, spec *TaskSourceSpec) adoptOutcome {
	name := reg.Source.Name()
	if err := spec.Validate(reg.Kind); err != nil {
		p.logger.Warnf("task sources: %s: project %s: refused spec: %s", name, project.ID, err.Error())
		return adoptFailed
	}
	source := spec.Source
	task, err := p.tasks.CreateTask(ctx, &CreateTaskRequest{
		TenantID:      project.TenantID,
		ProjectID:     project.ID,
		Subject:       spec.Subject,
		Description:   spec.Description,
		Metadata:      spec.Metadata,
		Source:        &source,
		Priority:      spec.Priority,
		AttemptBudget: spec.AttemptBudget,
		Runner:        spec.Runner,
		AutoSpawn:     true,
		Park:          spec.Park,
	})
	switch {
	case err == nil:
		p.reportAdoption(name, project, &source, task)
		return adoptCreated
	case errors.Is(err, ErrTaskSourceAlreadyAdopted):
		p.reportReservedSource(name, project, err)
		return adoptReserved
	case task != nil:
		// The task exists but auto-spawn found no agent; the dispatcher's
		// next sweep is what starts it, so the task counts as adopted.
		p.logger.Warnf("task sources: %s: project %s: task %s created but not spawned: %s",
			name, project.ID, task.ID, err.Error())
		p.reportSupersession(name, project, task)
		return adoptCreated
	default:
		p.logger.Warnf("task sources: %s: project %s: creating task for %s/%s failed: %s",
			name, project.ID, source.Kind, source.Ref, err.Error())
		return adoptFailed
	}
}

// reportAdoption records the task a spec became.
func (p *TaskSourcePoller) reportAdoption(name string, project *Project, source *TaskSourceRef, task *Task) {
	p.logger.Infof("task sources: %s: project %s: adopted %s/%s as task %s",
		name, project.ID, source.Kind, source.Ref, task.ID)
	p.reportSupersession(name, project, task)
}

// reportSupersession names the task a new one took its origin over from, so
// the run that spent its attempt budget and the run a person asked for by
// re-labelling are one traceable line apart.
func (p *TaskSourcePoller) reportSupersession(name string, project *Project, task *Task) {
	predecessor := task.PredecessorTaskID()
	if predecessor == "" {
		return
	}
	if reason := task.PredecessorBlockReason(); reason != "" {
		p.logger.Infof("task sources: %s: project %s: task %s supersedes task %s, which held the same origin blocked on %s",
			name, project.ID, task.ID, predecessor, reason)
		return
	}
	p.logger.Infof("task sources: %s: project %s: task %s supersedes task %s, which held the same origin",
		name, project.ID, task.ID, predecessor)
}

// reportReservedSource says why an offered spec produced no task.
//
// SILENCE IS THE DEFECT. An origin held by a task that can still work it is
// the ordinary outcome of re-polling and is a debug-level non-event. An origin
// held by a task that can never work it again is not: the reservation should
// have been taken over as the new task was created, so something refused it,
// and an operator re-labelling an issue to no effect deserves to be told which
// task is holding it and what state that task is in (#652).
func (p *TaskSourcePoller) reportReservedSource(name string, project *Project, err error) {
	var adopted *TaskSourceAlreadyAdoptedError
	if errors.As(err, &adopted) && !taskSourceReleasable(adopted.HolderStatus, adopted.HolderBlockFaultClass) {
		p.logger.Debugf("task sources: %s: project %s: origin already adopted: %s", name, project.ID, err.Error())
		return
	}
	p.logger.Warnf("task sources: %s: project %s: the origin is held by a task that is finished with it and was not superseded; delete that task to release it: %s",
		name, project.ID, err.Error())
}

// reportPollError logs an outage once when it begins and every other error
// on every sweep.
func (p *TaskSourcePoller) reportPollError(name string, project *Project, err error) {
	var unavailable *TaskSourceUnavailableError
	if !errors.As(err, &unavailable) {
		p.logger.Warnf("task sources: %s: project %s: poll failed: %s", name, project.ID, err.Error())
		return
	}
	p.outageMu.Lock()
	_, known := p.outages[name]
	if !known {
		p.outages[name] = struct{}{}
	}
	p.outageMu.Unlock()
	if !known {
		p.logger.Warnf("task sources: %s is unavailable and will be skipped until it answers again: %s",
			name, err.Error())
	}
}

// clearOutage records that a source answered again.
func (p *TaskSourcePoller) clearOutage(name string) {
	p.outageMu.Lock()
	_, known := p.outages[name]
	delete(p.outages, name)
	p.outageMu.Unlock()
	if known {
		p.logger.Infof("task sources: %s is available again", name)
	}
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrTaskSourceRegistration indicates a source could not be registered.
	ErrTaskSourceRegistration = errors.New("task source: registration refused")

	// ErrTaskSourceAlreadyRegistered indicates a source name is taken.
	ErrTaskSourceAlreadyRegistered = errors.New("task source: already registered")

	// ErrTaskSourceUnavailable indicates a source cannot reach its backing
	// system right now.
	ErrTaskSourceUnavailable = errors.New("task source: unavailable")

	// ErrTaskSourceSpecInvalid indicates a source returned a spec the engine
	// cannot adopt.
	ErrTaskSourceSpecInvalid = errors.New("task source: invalid task spec")

	// ErrTaskSourcePollerConfig indicates the poller is missing a collaborator.
	ErrTaskSourcePollerConfig = errors.New("task source poller: invalid configuration")

	// ErrTaskSourceSweep indicates a sweep could not enumerate its tenants.
	ErrTaskSourceSweep = errors.New("task source poller: sweep failed")

	// ErrTaskSourceProjectNotFound indicates a project sweep named a project
	// the tenant does not have.
	ErrTaskSourceProjectNotFound = errors.New("task source poller: project not found")
)

// TaskSourceProjectNotFoundError reports a SweepProject for a project the
// tenant does not have.
type TaskSourceProjectNotFoundError struct {
	TenantID  string
	ProjectID string
}

// Error implements the error interface.
func (e *TaskSourceProjectNotFoundError) Error() string {
	return ErrTaskSourceProjectNotFound.Error() + ": tenant " + e.TenantID + " has no project " + e.ProjectID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskSourceProjectNotFoundError) Unwrap() error {
	return ErrTaskSourceProjectNotFound
}

// TaskSourceRegistrationError reports a registration the registry refused for
// a reason other than a duplicate name.
type TaskSourceRegistrationError struct {
	Name    string
	Message string
}

// Error implements the error interface.
func (e *TaskSourceRegistrationError) Error() string {
	if e.Name == "" {
		return ErrTaskSourceRegistration.Error() + ": " + e.Message
	}
	return ErrTaskSourceRegistration.Error() + ": " + e.Name + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskSourceRegistrationError) Unwrap() error {
	return ErrTaskSourceRegistration
}

// TaskSourceAlreadyRegisteredError reports a second registration under a
// name that is already taken.
type TaskSourceAlreadyRegisteredError struct {
	Name string
}

// Error implements the error interface.
func (e *TaskSourceAlreadyRegisteredError) Error() string {
	return ErrTaskSourceAlreadyRegistered.Error() + ": " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskSourceAlreadyRegisteredError) Unwrap() error {
	return ErrTaskSourceAlreadyRegistered
}

// TaskSourceUnavailableError is what a source returns from Poll when its
// backing system cannot be reached. The poller reports it once per outage.
type TaskSourceUnavailableError struct {
	Source string
	Cause  error
}

// Error implements the error interface.
func (e *TaskSourceUnavailableError) Error() string {
	msg := ErrTaskSourceUnavailable.Error() + ": " + e.Source
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *TaskSourceUnavailableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrTaskSourceUnavailable}
	}
	return []error{ErrTaskSourceUnavailable, e.Cause}
}

// TaskSourceSpecInvalidError reports a spec the engine refused to adopt.
type TaskSourceSpecInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *TaskSourceSpecInvalidError) Error() string {
	return ErrTaskSourceSpecInvalid.Error() + ": " + e.Field + " " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskSourceSpecInvalidError) Unwrap() error {
	return ErrTaskSourceSpecInvalid
}

// TaskSourcePollerConfigError reports a poller built without a collaborator.
type TaskSourcePollerConfigError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *TaskSourcePollerConfigError) Error() string {
	if e.Message == "" {
		return ErrTaskSourcePollerConfig.Error() + ": " + e.Field + " is required"
	}
	return ErrTaskSourcePollerConfig.Error() + ": " + e.Field + " " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TaskSourcePollerConfigError) Unwrap() error {
	return ErrTaskSourcePollerConfig
}

// TaskSourceSweepError reports a sweep that could not enumerate tenants.
type TaskSourceSweepError struct {
	Cause error
}

// Error implements the error interface.
func (e *TaskSourceSweepError) Error() string {
	return ErrTaskSourceSweep.Error() + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *TaskSourceSweepError) Unwrap() []error {
	return []error{ErrTaskSourceSweep, e.Cause}
}
