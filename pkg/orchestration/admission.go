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
	"sort"
	"strconv"
	"time"
)

// =============================================================================
// Admission — three inputs, decided in one place
// =============================================================================
//
// CONCURRENCY USED TO BE BOUNDED BY ONE INPUT, AND IT WAS THE WRONG NUMBER OF
// INPUTS BY TWO. Project.Quotas.MaxConcurrency was enforced independently by
// the task-source poller and the task dispatcher, each doing its own
// arithmetic, and neither knew anything outside its project. Ten autonomous
// projects each permitted three concurrent jobs will run thirty of them on a
// fleet with eight worker slots — observed 2026-09-12 — and every one of the
// thirty will also draw on one shared pool of provider accounts that may have
// no usage headroom left at all.
//
// So admission is decided HERE, once per sweep, from three inputs:
//
//  1. THE PROJECT'S OWN QUOTA. Unchanged, and it stays a hard ceiling: no
//     redistribution below may lift a project above what its operator set.
//
//  2. FLEET CAPACITY. The deployment-wide count of jobs that may run at once,
//     measured against the live job leases of EVERY tenant — the same lease
//     records the per-project count already reads, so there is one measurement
//     of "running" and not a second bookkeeping surface to drift from it.
//
//  3. PROVIDER USAGE HEADROOM. Whether the project's pooled accounts can serve
//     another session at all. SUBSCRIPTION USAGE, NEVER A PRICE: the owner's
//     ruling is subscription accounts only, ConsumptionEntry.CostMicros has no
//     production writer, and a money axis summing zeroes would admit forever
//     while looking like a cap (go-quicbaas#791). The signals read are the ones
//     that exist: the sliding token windows in account_quota.go, and the
//     AvailableAt cooldown a provider refusal writes.
//
// WHY A PLAN RATHER THAN A PREDICATE. The fleet axis cannot be answered for one
// project in isolation — a fair division needs to know who else is asking — so
// one Plan call reads the whole fleet and both sweeps then consult the same
// plan. That also removes the duplicated per-project arithmetic: the poller's
// "limit - inflight - pending" and the dispatcher's "limit - inflight" are now
// two questions asked of one decision.
//
// FAIRNESS IS A REQUIREMENT, NOT A CONSEQUENCE. A fleet-wide cap handed out
// first-come would let one busy project hold every slot indefinitely, which is
// strictly worse than the per-project behaviour it replaces. The division is
// therefore MAX-MIN FAIR over the projects that are asking, with the residual
// rotated by least-served: see fairShares.
//
// FAIL CLOSED, AND NEVER WITH A NUMBER. Every read failure returns an error and
// no plan. A plan that reported "unbounded" because it could not count leases
// would be the oversubscription this file exists to prevent, wearing the
// costume of a successful measurement.

// AdmissionAxis names which of the three inputs decided an admission.
type AdmissionAxis string

const (
	// AdmissionAxisUnbounded means nothing bounds the project: it has no
	// concurrency quota and the deployment declared no fleet capacity.
	AdmissionAxisUnbounded AdmissionAxis = "unbounded"

	// AdmissionAxisProjectQuota means the project's own MaxConcurrency
	// decided.
	AdmissionAxisProjectQuota AdmissionAxis = "project_quota"

	// AdmissionAxisFleetCapacity means the deployment's worker slots decided:
	// the project is within its own quota and the fleet is not.
	AdmissionAxisFleetCapacity AdmissionAxis = "fleet_capacity"

	// AdmissionAxisProviderHeadroom means no account in the project's pool can
	// serve another session, so admission is DEFERRED rather than refused —
	// the decision carries the time it changes.
	AdmissionAxisProviderHeadroom AdmissionAxis = "provider_headroom"
)

// String returns the axis name.
func (a AdmissionAxis) String() string { return string(a) }

// AdmissionDecision is what one project may start right now, and which of the
// three inputs said so.
//
// IT ANSWERS TWO QUESTIONS, not one, because the two enforcement points ask
// different ones: the dispatcher asks how many QUEUED tasks it may start
// (DispatchSlots), and the poller asks how many NEW tasks it may adopt
// (AdoptionSlots) — which is the same entitlement less the queue that has
// already claimed part of it.
type AdmissionDecision struct {
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`

	// Entitlement is how many concurrent jobs this project may hold beyond
	// the ones it is already running. Meaningful only when Bounded is true.
	Entitlement int `json:"entitlement"`

	// Bounded reports whether any axis bounds the project. False means the
	// project has no quota and the deployment declared no fleet capacity, so
	// there is no number to enforce — the pre-existing behaviour of an
	// unlimited project, preserved exactly.
	Bounded bool `json:"bounded"`

	// Axis names what decided. For an admitting decision it names the
	// tightest axis considered, so a log line says which number is close.
	Axis AdmissionAxis `json:"axis"`

	// Deferred reports a decision that is not a refusal but a wait: the
	// provider pool has no headroom now and RetryAt says when that changes.
	// A deferred project's share is released to the others.
	Deferred bool `json:"deferred,omitempty"`

	// RetryAt is when a deferred decision would change, zero when unknown.
	RetryAt time.Time `json:"retry_at,omitempty"`

	// Reason is the operator-facing sentence for the decision.
	Reason string `json:"reason,omitempty"`

	// The measurements the decision was computed from, carried so a refusal
	// can be explained without re-reading anything.
	ProjectLimit    int `json:"project_limit"`
	ProjectInflight int `json:"project_inflight"`
	ProjectPending  int `json:"project_pending"`
	FleetLimit      int `json:"fleet_limit"`
	FleetInflight   int `json:"fleet_inflight"`
}

// DispatchSlots is how many of the project's already-queued tasks may be
// started now.
func (d AdmissionDecision) DispatchSlots() int {
	if !d.Bounded {
		return unboundedSlots
	}
	return d.Entitlement
}

// AdoptionSlots is how many NEW tasks the project may take on now: its
// entitlement less the queued tasks that have already claimed part of it.
//
// Clamped at zero. A project whose queue already exceeds its entitlement is
// not owed anything, and a negative capacity read as a signed count elsewhere
// would be a cap that admits.
func (d AdmissionDecision) AdoptionSlots() int {
	if !d.Bounded {
		return unboundedSlots
	}
	slots := d.Entitlement - d.ProjectPending
	if slots < 0 {
		return 0
	}
	return slots
}

// unboundedSlots is the slot count reported for a project no axis bounds. It
// is a large finite number rather than a sentinel so every caller's `i < slots`
// comparison stays ordinary arithmetic; no realistic sweep offers this many
// specs, and a caller that wants the distinction reads Bounded.
const unboundedSlots = int(^uint(0) >> 1)

// FleetPlan is one sweep's admission decision for every project of every
// tenant, plus the fleet totals it was computed from.
//
// Read-only once built. Both sweeps may consult one plan concurrently.
type FleetPlan struct {
	// At is the instant the plan was computed for.
	At time.Time

	// Capacity is the whole capacity answer the plan was computed from,
	// including which source supplied it — so a log line names the number an
	// operator would have to change.
	Capacity FleetCapacity

	// FleetLimit is the deployment capacity in force, 0 when undeclared. It is
	// Capacity.Slots, carried separately because every decision quotes it.
	FleetLimit int

	// FleetInflight is how many jobs the whole deployment was running.
	FleetInflight int

	// FleetFree is the capacity left, 0 when FleetLimit is 0 (undeclared
	// capacity has no free count, and reporting one would invite arithmetic
	// against a number nobody set).
	FleetFree int

	byProject map[string]AdmissionDecision
}

// For returns the decision for one project. A project the plan does not name —
// created between the plan and the question — is reported as unbounded, which
// is what it would have been had no plan existed at all; admitting a project
// the plan never measured is preferable to stalling one on a stale reading.
func (p *FleetPlan) For(tenantID, projectID string) AdmissionDecision {
	if p == nil {
		return AdmissionDecision{TenantID: tenantID, ProjectID: projectID, Axis: AdmissionAxisUnbounded}
	}
	if decision, ok := p.byProject[projectPlanKey(tenantID, projectID)]; ok {
		return decision
	}
	return AdmissionDecision{
		TenantID:  tenantID,
		ProjectID: projectID,
		Axis:      AdmissionAxisUnbounded,
		Reason:    "the project was not in this sweep's admission plan",
	}
}

// Decisions returns every decision, ordered by tenant then project, so two
// plans over the same state produce comparable reports.
func (p *FleetPlan) Decisions() []AdmissionDecision {
	if p == nil {
		return nil
	}
	out := make([]AdmissionDecision, 0, len(p.byProject))
	for _, decision := range p.byProject {
		out = append(out, decision)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TenantID != out[j].TenantID {
			return out[i].TenantID < out[j].TenantID
		}
		return out[i].ProjectID < out[j].ProjectID
	})
	return out
}

// projectPlanKey addresses one project's decision. The separator cannot occur
// in either identifier — both are validated against the keyspace charset — so
// two distinct projects can never collide on one key.
func projectPlanKey(tenantID, projectID string) string {
	return tenantID + "/" + projectID
}

// =============================================================================
// Provider headroom
// =============================================================================

// PoolHeadroom is what a project's pooled provider accounts can serve now.
type PoolHeadroom struct {
	// Pooled is how many accounts the project could draw on at all.
	Pooled int

	// Ready is how many of them may serve another session now.
	Ready int

	// RetryAt is the earliest instant at which an account that cannot serve
	// now is expected to be able to, zero when none names one.
	RetryAt time.Time

	// Reason is the operator-facing sentence for a pool with no ready
	// account.
	Reason string
}

// Unmetered reports a project whose account axis bounds nothing: no account is
// pooled for it at all.
//
// AN EMPTY POOL IS NOT A SPENT POOL, and conflating them would stop every
// deployment that does not use account pooling. The honest reading of "nobody
// configured an account here" is that this axis has nothing to say, exactly as
// AccountSpendPolicy treats an account nobody configured a limit for.
func (h PoolHeadroom) Unmetered() bool { return h.Pooled == 0 }

// HasHeadroom reports whether the pool can serve another session.
func (h PoolHeadroom) HasHeadroom() bool { return h.Unmetered() || h.Ready > 0 }

// PoolHeadroomReader answers whether one project's pooled provider accounts
// can serve more work.
//
// A NARROW SEAM ON PURPOSE. Admission must not re-derive which accounts are
// eligible or what a spent window is; those rules live in Account.Selectable
// and QuotaAuthority, and a second copy here would be the copy that disagrees.
// The production implementation composes exactly those two — see
// NewAccountPoolHeadroom.
type PoolHeadroomReader interface {
	// Headroom answers for one project at one instant. It returns an error
	// and no answer whenever it could not establish one.
	Headroom(ctx context.Context, tenantID, projectID string, now time.Time) (PoolHeadroom, error)
}

// =============================================================================
// The controller
// =============================================================================

// AdmissionControllerConfig wires an AdmissionController.
type AdmissionControllerConfig struct {
	// Tenants enumerates the tenants whose projects are planned for.
	Tenants TenantLister

	// Projects lists a tenant's projects.
	Projects ProjectLister

	// Queue reads each project's pending tasks — the demand already adopted.
	Queue TaskQueueReader

	// Leases counts running jobs, per project and fleet-wide. Both counts
	// come from this one store so "running" means one thing.
	Leases JobLeaseStore

	// LeaseTTL decides which leases still count as live. It must be the TTL
	// the lease store itself was built with.
	LeaseTTL time.Duration

	// Capacity answers how many worker slots the deployment has. Nil, or a
	// reader whose answer is not Known, leaves the fleet axis out of every
	// decision — which is the pre-existing behaviour, and the right one: a
	// capacity nobody could establish must not read as no capacity.
	//
	// See fleet_capacity.go. The production reader combines what the fleet's
	// own daemons report with what the operator declared.
	Capacity FleetCapacityReader

	// Headroom answers the provider axis. Nil leaves that axis out, which is
	// correct for a deployment that pools no accounts.
	Headroom PoolHeadroomReader

	// Now is the clock; nil takes utcNow.
	Now func() time.Time
}

// AdmissionController computes the fleet's admission plan.
type AdmissionController struct {
	tenants  TenantLister
	projects ProjectLister
	queue    TaskQueueReader
	leases   JobLeaseStore
	ttl      time.Duration
	capacity FleetCapacityReader
	headroom PoolHeadroomReader
	now      func() time.Time
}

// NewAdmissionController builds the controller. Every store is required; the
// capacity and the headroom reader are the two axes a deployment may leave
// undeclared.
func NewAdmissionController(cfg AdmissionControllerConfig) (*AdmissionController, error) {
	switch {
	case cfg.Tenants == nil:
		return nil, &AdmissionConfigError{Field: "tenants", Message: "a fleet plan enumerates every tenant's projects"}
	case cfg.Projects == nil:
		return nil, &AdmissionConfigError{Field: "projects", Message: "a fleet plan reads each tenant's projects"}
	case cfg.Queue == nil:
		return nil, &AdmissionConfigError{Field: "queue", Message: "the demand a project has already adopted is read from its queue"}
	case cfg.Leases == nil:
		return nil, &AdmissionConfigError{Field: "leases", Message: "running jobs are counted from the lease store, per project and fleet-wide"}
	case cfg.LeaseTTL <= 0:
		return nil, &AdmissionConfigError{Field: "lease_ttl", Message: "must be positive; a zero TTL makes every holder stale on the next read"}
	}
	now := cfg.Now
	if now == nil {
		now = utcNow
	}
	return &AdmissionController{
		tenants:  cfg.Tenants,
		projects: cfg.Projects,
		queue:    cfg.Queue,
		leases:   cfg.Leases,
		ttl:      cfg.LeaseTTL,
		capacity: cfg.Capacity,
		headroom: cfg.Headroom,
		now:      now,
	}, nil
}

// FleetCapacity reads the deployment's worker-slot capacity at now, or an
// undeclared answer when no reader is wired.
func (c *AdmissionController) FleetCapacity(ctx context.Context, now time.Time) (FleetCapacity, error) {
	if c.capacity == nil {
		return FleetCapacity{Source: FleetCapacityUndeclared}, nil
	}
	return c.capacity.FleetCapacity(ctx, now)
}

// projectDemand is one project's ask, assembled before any division happens.
type projectDemand struct {
	decision AdmissionDecision

	// demand is how many more concurrent jobs the project wants: its own
	// unused quota, or every free slot when it declared no quota.
	demand int

	// unbounded marks a project with no quota of its own, whose demand is
	// therefore every slot it can get.
	unbounded bool

	// live is how many jobs the project is running and lastStart the most
	// recent instant one of them was taken. Both are the residual
	// tie-break's inputs: see fairShares.
	live      int
	lastStart int64
}

// Plan reads the whole fleet and decides what every autonomous project may
// start.
//
// A tenant or project that cannot be read fails the WHOLE plan rather than
// being skipped. A partial plan would divide the fleet among the projects it
// could see and hand each of them a larger share than it is owed, which is the
// oversubscription this exists to prevent.
func (c *AdmissionController) Plan(ctx context.Context) (*FleetPlan, error) {
	now := c.now()

	// THE CAPACITY IS READ ONCE PER PLAN, before anything is divided. It is a
	// measurement of the fleet — a sum over the daemons alive at this instant —
	// so reading it per project would divide a different fleet for each one and
	// hand out more slots in total than any of those fleets had.
	capacity, err := c.FleetCapacity(ctx, now)
	if err != nil {
		return nil, err
	}

	// NEITHER NEW AXIS IN FORCE MEANS NO PLAN, AND THAT IS NOT A SHORTCUT. A
	// plan's value is that it divides one fleet among every project at once, and
	// the price of that is that ONE unreadable project fails the WHOLE plan —
	// necessarily, because a partial plan would divide the fleet among the
	// projects it could see and hand each a larger share than it is owed.
	//
	// When the fleet capacity is unknown and no pool is metered there is nothing
	// to divide and nothing to defer on, so that price buys nothing: it would
	// only widen one project's store outage into a stalled sweep for every other
	// project, which is strictly worse than the per-project arithmetic the
	// sweeps already do. So the answer is "no plan", the sweeps fall back to
	// exactly what they did before this existed, and the blast radius of a
	// failed read is unchanged for a deployment that declared neither axis.
	if !capacity.Known() && c.headroom == nil {
		return nil, nil
	}

	fleetInflight, err := c.countFleetInflight(ctx, now)
	if err != nil {
		return nil, err
	}

	demands, err := c.collectDemand(ctx, now, capacity, fleetInflight)
	if err != nil {
		return nil, err
	}

	plan := &FleetPlan{
		At:            now,
		Capacity:      capacity,
		FleetLimit:    capacity.Slots,
		FleetInflight: fleetInflight,
		byProject:     make(map[string]AdmissionDecision, len(demands)),
	}
	if capacity.Known() {
		plan.FleetFree = capacity.Slots - fleetInflight
		if plan.FleetFree < 0 {
			plan.FleetFree = 0
		}
		applyFairShares(demands, plan.FleetFree)
	}
	for _, demand := range demands {
		decision := demand.decision
		plan.byProject[projectPlanKey(decision.TenantID, decision.ProjectID)] = decision
	}
	return plan, nil
}

// countFleetInflight counts the live job leases of every tenant.
//
// ONE LEASE LISTING FOR THE WHOLE FLEET rather than a sum of per-project
// listings. A sum taken project by project is taken at as many instants as
// there are projects, and the interleaved acquisitions between them are
// counted zero times or twice.
func (c *AdmissionController) countFleetInflight(ctx context.Context, now time.Time) (int, error) {
	held, err := c.leases.List(ctx, "", "")
	if err != nil {
		return 0, &AdmissionUndecidableError{Input: "fleet_leases", Cause: err}
	}
	live := 0
	for _, lease := range held {
		if lease != nil && lease.Live(now, c.ttl) {
			live++
		}
	}
	return live, nil
}

// collectDemand builds one projectDemand per autonomous project, with every
// axis except the fleet division already applied.
func (c *AdmissionController) collectDemand(ctx context.Context, now time.Time, capacity FleetCapacity, fleetInflight int) ([]*projectDemand, error) {
	tenantIDs, err := c.tenants.ListTenantIDs(ctx)
	if err != nil {
		return nil, &AdmissionUndecidableError{Input: "tenants", Cause: err}
	}
	var demands []*projectDemand
	for _, tenantID := range tenantIDs {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		tenantCtx := ContextWithTenant(ctx, tenantID)
		projects, listErr := c.projects.ListByTenant(tenantCtx, tenantID)
		if listErr != nil {
			return nil, &AdmissionUndecidableError{Input: "projects of tenant " + tenantID, Cause: listErr}
		}
		for _, project := range projects {
			if project == nil || !project.Autonomous {
				continue
			}
			demand, demandErr := c.projectDemand(tenantCtx, project, now, capacity, fleetInflight)
			if demandErr != nil {
				return nil, demandErr
			}
			demands = append(demands, demand)
		}
	}
	// Ordered so a plan over the same state is byte-identical twice running,
	// which the residual tie-break below depends on for its own determinism.
	sort.Slice(demands, func(i, j int) bool {
		left, right := demands[i].decision, demands[j].decision
		if left.TenantID != right.TenantID {
			return left.TenantID < right.TenantID
		}
		return left.ProjectID < right.ProjectID
	})
	return demands, nil
}

// projectDemand measures one project and applies the two axes that need no
// knowledge of any other project: its own quota, and its provider headroom.
func (c *AdmissionController) projectDemand(ctx context.Context, project *Project, now time.Time, capacity FleetCapacity, fleetInflight int) (*projectDemand, error) {
	live, lastStart, err := c.countProjectInflight(ctx, project, now)
	if err != nil {
		return nil, err
	}
	pending, err := c.countProjectPending(ctx, project)
	if err != nil {
		return nil, err
	}

	limit := project.Quotas.MaxConcurrency
	demand := &projectDemand{
		live:      live,
		lastStart: lastStart,
		unbounded: limit <= 0,
		decision: AdmissionDecision{
			TenantID:        project.TenantID,
			ProjectID:       project.ID,
			Axis:            AdmissionAxisUnbounded,
			ProjectLimit:    limit,
			ProjectInflight: live,
			ProjectPending:  pending,
			FleetLimit:      capacity.Slots,
			FleetInflight:   fleetInflight,
		},
	}

	// The project's own quota, which is a CEILING no redistribution lifts.
	if limit > 0 {
		remaining := limit - live
		if remaining < 0 {
			remaining = 0
		}
		demand.demand = remaining
		demand.decision.Bounded = true
		demand.decision.Entitlement = remaining
		demand.decision.Axis = AdmissionAxisProjectQuota
		if remaining == 0 {
			demand.decision.Reason = "the project's " + strconv.Itoa(limit) +
				" concurrent job(s) are all held by live leases"
		}
	}

	// The provider axis. A project with no headroom is DEFERRED and asks for
	// nothing, so the fleet division below hands its share to a project that
	// can actually use it.
	headroom, err := c.projectHeadroom(ctx, project, now)
	if err != nil {
		return nil, err
	}
	if !headroom.HasHeadroom() {
		demand.demand = 0
		demand.unbounded = false
		demand.decision.Bounded = true
		demand.decision.Entitlement = 0
		demand.decision.Deferred = true
		demand.decision.Axis = AdmissionAxisProviderHeadroom
		demand.decision.RetryAt = headroom.RetryAt
		demand.decision.Reason = headroom.Reason
	}
	return demand, nil
}

// projectHeadroom asks the provider axis, treating an unwired reader as an
// axis the deployment did not declare.
func (c *AdmissionController) projectHeadroom(ctx context.Context, project *Project, now time.Time) (PoolHeadroom, error) {
	if c.headroom == nil {
		return PoolHeadroom{}, nil
	}
	headroom, err := c.headroom.Headroom(ctx, project.TenantID, project.ID, now)
	if err != nil {
		return PoolHeadroom{}, &AdmissionUndecidableError{
			Input: "provider headroom of project " + project.ID, Cause: err,
		}
	}
	return headroom, nil
}

// countProjectInflight counts one project's live leases and reports when the
// most recent of them was taken.
func (c *AdmissionController) countProjectInflight(ctx context.Context, project *Project, now time.Time) (int, int64, error) {
	held, err := c.leases.List(ctx, project.TenantID, project.ID)
	if err != nil {
		return 0, 0, &AdmissionUndecidableError{Input: "leases of project " + project.ID, Cause: err}
	}
	live := 0
	var lastStart int64
	for _, lease := range held {
		if lease == nil {
			continue
		}
		// The last start is read from EVERY lease, live or not. A project
		// whose jobs all finished a minute ago has been served more recently
		// than one that has never run, and the residual tie-break needs that
		// difference; counting only live leases would make the two identical.
		if lease.AcquiredAt > lastStart {
			lastStart = lease.AcquiredAt
		}
		if lease.Live(now, c.ttl) {
			live++
		}
	}
	return live, lastStart, nil
}

// countProjectPending counts the tasks already waiting for the dispatcher.
func (c *AdmissionController) countProjectPending(ctx context.Context, project *Project) (int, error) {
	pending, err := c.queue.ListTasks(ctx, project.TenantID, &TaskFilter{
		ProjectID: project.ID,
		Status:    TaskStatusPending,
	})
	if err != nil {
		return 0, &AdmissionUndecidableError{Input: "queue of project " + project.ID, Cause: err}
	}
	return CountPendingUndispatched(pending), nil
}

// =============================================================================
// The fair division
// =============================================================================
//
// THE FAIRNESS PROPERTY THIS GUARANTEES, stated so it can be tested:
//
//  1. SAFETY. The shares handed out sum to at most the free fleet slots, and
//     no project's share exceeds its own unused quota. So the jobs running
//     after a sweep are at most the fleet capacity, however many projects are
//     each inside their own quota — which is the aggregate bound that did not
//     exist before.
//
//  2. MAX-MIN FAIRNESS. A project asking for no more than an equal share of
//     the free slots receives everything it asked for. Capacity that a project
//     does not want is redistributed among those that do, so no slot is idled
//     to preserve a share nobody claimed, and no project's share is reduced to
//     satisfy another project asking for more than its equal share.
//
//  3. NO STARVATION. When free slots are fewer than the projects asking, the
//     indivisible residual goes to the projects with the fewest live jobs,
//     least-recently-started first. Taking a slot makes a project the most
//     recently started, which puts it behind every other claimant on the next
//     sweep, so every asking project is served within ⌈P/F⌉ sweeps of P
//     projects sharing F free slots. A busy project can hold the whole fleet
//     only while nobody else is asking, and yields on the first sweep after
//     somebody does.

// applyFairShares divides free among the demands and writes each project's
// share into its decision.
func applyFairShares(demands []*projectDemand, free int) {
	shares := fairShares(demands, free)
	for i, demand := range demands {
		share := shares[i]
		decision := &demand.decision

		// A project already refused or deferred by an axis of its own keeps
		// that answer: the fleet has nothing to add to "your quota is full"
		// or "your accounts are spent", and overwriting the axis would send an
		// operator to the wrong number.
		if decision.Bounded && decision.Entitlement == 0 {
			continue
		}
		if share < decision.Entitlement || !decision.Bounded {
			decision.Axis = AdmissionAxisFleetCapacity
			if share == 0 {
				decision.Reason = "the fleet's " + strconv.Itoa(decision.FleetLimit) +
					" worker slot(s) are held by " + strconv.Itoa(decision.FleetInflight) +
					" live job(s), and this project's fair share of what is left is none"
			}
		}
		decision.Bounded = true
		decision.Entitlement = share
	}
}

// fairShares computes the max-min fair division of free among demands,
// returning one share per demand in the order given.
//
// EXACT INTEGER MAX-MIN FAIRNESS, in two steps.
//
//  1. THE LEVEL. Find the largest L such that giving every claimant
//     min(ask, L) fits in free. That allocation IS the max-min fair one over
//     the reals, rounded down: every claimant asking for L or less gets
//     everything it asked for, and every claimant asking for more gets exactly
//     L — nobody's share is reduced to raise a share that is already larger.
//     Found by binary search on L rather than by filling one slot at a time, so
//     the cost is logarithmic in the largest demand rather than linear in the
//     fleet's size.
//
//  2. THE RESIDUAL. Integer flooring leaves free - Σ min(ask, L) slots over,
//     strictly fewer than the number of claimants still short of their demand
//     (otherwise L+1 would have fit, and L was maximal). So each of them takes
//     at most one more, handed out in the LEAST-SERVED order — which is what
//     makes the division starvation-free rather than merely fair on average,
//     because taking a slot makes a project the most recently served and puts it
//     behind the others on the next sweep.
//
// A GREEDY PASS OVER ASCENDING DEMAND IS NOT THIS, and was the first attempt:
// giving each claimant in turn remaining/unserved hands the accumulated rounding
// to whoever is visited last, so two projects with identical demand and three
// free slots got one and two by position rather than by having been served less.
func fairShares(demands []*projectDemand, free int) []int {
	shares := make([]int, len(demands))
	if free <= 0 {
		return shares
	}

	// Claimants are the indices with a nonzero ask. An unbounded project asks
	// for everything, which in a division of `free` means `free`.
	claimants := make([]int, 0, len(demands))
	asks := make([]int, len(demands))
	highest := 0
	for i, demand := range demands {
		ask := demand.demand
		if demand.unbounded {
			ask = free
		}
		if ask <= 0 {
			continue
		}
		asks[i] = ask
		if ask > highest {
			highest = ask
		}
		claimants = append(claimants, i)
	}
	if len(claimants) == 0 {
		return shares
	}

	level := fairLevel(claimants, asks, free, highest)
	remaining := free
	for _, index := range claimants {
		share := asks[index]
		if share > level {
			share = level
		}
		shares[index] = share
		remaining -= share
	}
	if remaining <= 0 {
		return shares
	}

	// The residual: one slot each, least-served first, to claimants still short
	// of what they asked for.
	residual := make([]int, 0, len(claimants))
	for _, index := range claimants {
		if shares[index] < asks[index] {
			residual = append(residual, index)
		}
	}
	sort.SliceStable(residual, func(a, b int) bool {
		return lessServed(demands[residual[a]], demands[residual[b]])
	})
	for _, index := range residual {
		if remaining == 0 {
			break
		}
		shares[index]++
		remaining--
	}
	return shares
}

// fairLevel is the largest per-claimant cap that fits in free.
//
// Monotone in the cap — raising it can only raise the sum — so a binary search
// finds the largest feasible one. The search's upper bound is the highest demand
// because no cap above it changes any allocation.
func fairLevel(claimants []int, asks []int, free, highest int) int {
	fits := func(level int) bool {
		total := 0
		for _, index := range claimants {
			share := asks[index]
			if share > level {
				share = level
			}
			total += share
			if total > free {
				return false
			}
		}
		return true
	}
	low, high := 0, highest
	for low < high {
		// Rounded UP so the search cannot stall at low == mid when the two
		// differ by one, which is the shape that loops forever.
		mid := low + (high-low+1)/2
		if fits(mid) {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}

// lessServed orders two projects by how little the fleet has served them:
// fewest live jobs first, then least recently started, then project id.
//
// The project id is the last term rather than the only one it could be: it
// makes the order total, so a sort over it is deterministic, while contributing
// nothing to the rotation — a project cannot improve its position by its name
// once either of the two real terms separates it.
func lessServed(left, right *projectDemand) bool {
	switch {
	case left.live != right.live:
		return left.live < right.live
	case left.lastStart != right.lastStart:
		return left.lastStart < right.lastStart
	case left.decision.TenantID != right.decision.TenantID:
		return left.decision.TenantID < right.decision.TenantID
	default:
		return left.decision.ProjectID < right.decision.ProjectID
	}
}
