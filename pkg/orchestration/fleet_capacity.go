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
	"strconv"
	"time"
)

// =============================================================================
// Fleet capacity — how many worker slots the deployment actually has
// =============================================================================
//
// NOTHING USED TO KNOW THIS NUMBER, which is why N projects each inside their
// own Quotas.MaxConcurrency oversubscribed the fleet on 2026-09-12. Admission's
// second input is the deployment-wide slot count, and it has TWO possible
// sources that this file reduces to ONE answer:
//
//   - THE FLEET ITSELF. Every daemon reports how many orchestration jobs it can
//     run at once (AgentRegistration.MaxConcurrentJobs, refreshed on every
//     heartbeat through AgentObservation), and the fleet's capacity is the sum
//     over the daemons that are still alive. This is the honest number: it
//     shrinks when a host is drained and grows when one is added, with nothing
//     to keep in step by hand.
//
//   - THE OPERATOR. orchestration.fleet.max_concurrent_jobs, for a deployment
//     whose daemons predate the reported field, or whose operator wants a
//     ceiling BELOW what the hardware could serve — a shared host, a metered
//     uplink, a deliberately slow rollout.
//
// AN UNMEASURED VALUE IS NOT ZERO, and that rule decides every edge here. A
// fleet nobody could measure must read as "this axis has nothing to say", never
// as "there are no slots": the second reading refuses every job in the
// deployment while wearing the costume of a safety bound. So the fleet sum is
// reported as KNOWN only when every live daemon reported a count — one daemon
// that reported nothing makes the sum a lower bound on reality, and enforcing a
// lower bound would refuse work the fleet would have served.
//
// THE TWO SOURCES COMBINE IN ONE PLACE, NewFleetCapacity, and the rule is the
// smaller of the ones that are known. Two bounds that both hold means the
// tighter one binds; a bound nobody set does not compete with one somebody did.

// FleetCapacitySource names where a capacity answer came from, so an operator
// reading a refusal knows which number to change.
type FleetCapacitySource string

const (
	// FleetCapacityUndeclared means no source could answer: the fleet axis is
	// left out of admission entirely, exactly as it was before this existed.
	FleetCapacityUndeclared FleetCapacitySource = "undeclared"

	// FleetCapacityOperator means the count came from
	// orchestration.fleet.max_concurrent_jobs.
	FleetCapacityOperator FleetCapacitySource = "operator"

	// FleetCapacityFleet means the count was summed from the daemons that are
	// alive right now.
	FleetCapacityFleet FleetCapacitySource = "fleet"
)

// String returns the source's name.
func (s FleetCapacitySource) String() string { return string(s) }

// FleetCapacity is what the deployment's worker slots amount to at one instant.
type FleetCapacity struct {
	// Slots is how many orchestration jobs the deployment may run at once.
	// Meaningful only when Known reports true.
	Slots int `json:"slots"`

	// Source names where Slots came from, and is FleetCapacityUndeclared when
	// nothing could answer.
	Source FleetCapacitySource `json:"source"`

	// Agents is how many live daemons the fleet sum was taken over, and
	// Unreported how many of them named no slot count. Both are zero for an
	// operator-declared capacity, which is taken over no daemons at all.
	Agents     int `json:"agents,omitempty"`
	Unreported int `json:"unreported,omitempty"`
}

// Known reports whether any source answered. A false answer leaves the fleet
// axis out of admission; it does NOT mean the fleet has no slots.
func (c FleetCapacity) Known() bool {
	return c.Source != FleetCapacityUndeclared && c.Source != "" && c.Slots > 0
}

// Describe renders the capacity for an operator's log line.
func (c FleetCapacity) Describe() string {
	if !c.Known() {
		return "undeclared (admission leaves the fleet axis out)"
	}
	out := strconv.Itoa(c.Slots) + " job slot(s) from " + c.Source.String()
	if c.Source == FleetCapacityFleet {
		out += ", summed over " + strconv.Itoa(c.Agents) + " live daemon(s)"
	}
	return out
}

// FleetCapacityReader answers how many worker slots the deployment has.
//
// AN INTERFACE BECAUSE THERE ARE TWO HONEST ANSWERS AND A RULE FOR COMBINING
// THEM, and because the same answer is needed at two enforcement points — the
// sweeps' admission plan and the lease store's last-ditch refusal. One reader
// shared between them is what keeps those two from bounding the fleet by
// different numbers.
type FleetCapacityReader interface {
	// FleetCapacity answers at one instant. It returns an error and no answer
	// whenever it could not establish one; see the package rule on failing
	// closed with an error rather than with a number.
	FleetCapacity(ctx context.Context, now time.Time) (FleetCapacity, error)
}

// =============================================================================
// The operator's declaration
// =============================================================================

// declaredFleetCapacity answers with the number an operator configured.
type declaredFleetCapacity struct {
	slots int
}

// Compile-time interface satisfaction check.
var _ FleetCapacityReader = (*declaredFleetCapacity)(nil)

// NewDeclaredFleetCapacity returns the reader for an operator-configured slot
// count. Zero leaves the axis undeclared; a negative count is refused, because
// a bound below zero cannot be told apart downstream from the undeclared case
// and is a number somebody wrote on purpose and got wrong.
func NewDeclaredFleetCapacity(slots int) (FleetCapacityReader, error) {
	if slots < 0 {
		return nil, &AdmissionConfigError{
			Field:   "fleet_capacity",
			Message: "must not be negative; use zero to leave the fleet axis undeclared",
		}
	}
	return &declaredFleetCapacity{slots: slots}, nil
}

// FleetCapacity implements FleetCapacityReader.
func (c *declaredFleetCapacity) FleetCapacity(_ context.Context, _ time.Time) (FleetCapacity, error) {
	if c.slots <= 0 {
		return FleetCapacity{Source: FleetCapacityUndeclared}, nil
	}
	return FleetCapacity{Slots: c.slots, Source: FleetCapacityOperator}, nil
}

// =============================================================================
// The fleet's own report
// =============================================================================

// DefaultFleetAgentStaleAfter is the age past which a daemon's last heartbeat
// stops counting its slots towards the fleet's capacity. It is the same bound
// session reconciliation uses to decide an agent has left, so "alive" means one
// thing across the engine.
const DefaultFleetAgentStaleAfter = DefaultAgentHeartbeatTimeout

// agentFleetCapacity sums the slots the live daemons of every tenant report.
type agentFleetCapacity struct {
	tenants    TenantLister
	agents     AgentRegistry
	staleAfter time.Duration
}

// Compile-time interface satisfaction check.
var _ FleetCapacityReader = (*agentFleetCapacity)(nil)

// NewAgentFleetCapacity returns the reader that measures the fleet from its own
// daemons. A zero staleAfter takes DefaultFleetAgentStaleAfter.
func NewAgentFleetCapacity(tenants TenantLister, agents AgentRegistry, staleAfter time.Duration) (FleetCapacityReader, error) {
	switch {
	case tenants == nil:
		return nil, &AdmissionConfigError{Field: "tenants", Message: "the fleet is measured across every tenant's daemons"}
	case agents == nil:
		return nil, &AdmissionConfigError{Field: "agents", Message: "the slots a daemon reports are read from the agent registry"}
	case staleAfter < 0:
		return nil, &AdmissionConfigError{Field: "stale_after", Message: "must not be negative; zero takes DefaultFleetAgentStaleAfter"}
	}
	if staleAfter == 0 {
		staleAfter = DefaultFleetAgentStaleAfter
	}
	return &agentFleetCapacity{tenants: tenants, agents: agents, staleAfter: staleAfter}, nil
}

// FleetCapacity implements FleetCapacityReader.
//
// A TENANT OR A REGISTRY THAT CANNOT BE READ FAILS THE WHOLE MEASUREMENT. A
// partial sum is a smaller fleet than the real one, and enforcing it would
// refuse work precisely when a store is already struggling.
func (c *agentFleetCapacity) FleetCapacity(ctx context.Context, now time.Time) (FleetCapacity, error) {
	tenantIDs, err := c.tenants.ListTenantIDs(ctx)
	if err != nil {
		return FleetCapacity{}, &AdmissionUndecidableError{Input: "tenants", Cause: err}
	}
	capacity := FleetCapacity{Source: FleetCapacityFleet}
	for _, tenantID := range tenantIDs {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return FleetCapacity{}, ctxErr
		}
		registered, listErr := c.agents.ListAgents(agentTenantContext(ctx, tenantID), tenantID, nil)
		if listErr != nil {
			return FleetCapacity{}, &AdmissionUndecidableError{Input: "agents of tenant " + tenantID, Cause: listErr}
		}
		for _, agent := range registered {
			if !AgentAlive(agent, now, c.staleAfter) {
				continue
			}
			capacity.Agents++
			if agent.MaxConcurrentJobs <= 0 {
				capacity.Unreported++
				continue
			}
			capacity.Slots += agent.MaxConcurrentJobs
		}
	}
	// AN UNMEASURED VALUE IS NOT ZERO. A fleet with no live daemon has not
	// reported that it has no slots — the registry may simply be empty in a
	// deployment whose work runs beside the control plane — and a fleet whose
	// daemons did not all report a count has produced a lower bound, not a
	// bound. Both read as "this axis has nothing to say".
	if capacity.Agents == 0 || capacity.Unreported > 0 || capacity.Slots <= 0 {
		return FleetCapacity{
			Source:     FleetCapacityUndeclared,
			Agents:     capacity.Agents,
			Unreported: capacity.Unreported,
		}, nil
	}
	return capacity, nil
}

// AgentAlive reports whether a registration is a daemon that will take work:
// its slots count towards the fleet, and the dispatcher may place a session on
// it.
//
// THREE WAYS TO NOT BE, and each one is a daemon that will not take work:
// administratively disabled, reported unreachable, or silent for longer than the
// heartbeat bound. A record with no sign of life at all does not count either —
// registration stamps LastSeen, so one without it was never written by a
// daemon that started.
//
// THE SILENCE BOUND IS WHAT KEEPS A DEAD DAEMON OUT OF DISPATCH. Nothing marks a
// crashed daemon offline — it cannot say so itself — so a record whose last
// heartbeat is older than the bound still reads "running"; the dispatcher that
// trusted the status routed every review to a connection that was refused.
func AgentAlive(agent *AgentRegistration, now time.Time, staleAfter time.Duration) bool {
	switch {
	case agent == nil:
		return false
	case agent.DisabledAt != 0, agent.Status == AgentStatusDisabled:
		return false
	case agent.Status == AgentStatusOffline, agent.Status == AgentStatusError:
		return false
	case agent.LastSeen <= 0:
		return false
	default:
		return now.Sub(time.Unix(0, agent.LastSeen)) <= staleAfter
	}
}

// =============================================================================
// The combination
// =============================================================================

// combinedFleetCapacity reduces the operator's declaration and the fleet's own
// report to one answer.
type combinedFleetCapacity struct {
	declared int
	measured FleetCapacityReader
}

// Compile-time interface satisfaction check.
var _ FleetCapacityReader = (*combinedFleetCapacity)(nil)

// NewFleetCapacity returns the reader both enforcement points consult: the
// operator's declared slot count, the fleet's own measurement, or the tighter of
// the two when both answer.
//
// THE RULE LIVES HERE AND NOWHERE ELSE. Two callers each deciding how to
// reconcile a configured number with a measured one is how a deployment ends up
// bounded by different numbers at the sweep and at the lease, which is the
// two-parallel-planes failure the no-cruft policy names.
//
// A nil measured reader is an ordinary case, not a fault: a deployment whose
// daemons report nothing has only the operator's number.
func NewFleetCapacity(declared int, measured FleetCapacityReader) (FleetCapacityReader, error) {
	if declared < 0 {
		return nil, &AdmissionConfigError{
			Field:   "fleet_capacity",
			Message: "must not be negative; use zero to leave the operator's declaration out",
		}
	}
	return &combinedFleetCapacity{declared: declared, measured: measured}, nil
}

// FleetCapacity implements FleetCapacityReader.
func (c *combinedFleetCapacity) FleetCapacity(ctx context.Context, now time.Time) (FleetCapacity, error) {
	operator := FleetCapacity{Source: FleetCapacityUndeclared}
	if c.declared > 0 {
		operator = FleetCapacity{Slots: c.declared, Source: FleetCapacityOperator}
	}
	if c.measured == nil {
		return operator, nil
	}
	fleet, err := c.measured.FleetCapacity(ctx, now)
	if err != nil {
		return FleetCapacity{}, err
	}
	switch {
	case !fleet.Known():
		// The fleet could not be measured, so the operator's number — if there
		// is one — is the only bound. Carry the daemon counts anyway: an
		// operator reading "0 slots from undeclared, 4 live daemon(s), 4
		// unreported" is being told exactly which daemons to upgrade.
		operator.Agents, operator.Unreported = fleet.Agents, fleet.Unreported
		return operator, nil
	case !operator.Known(), fleet.Slots < operator.Slots:
		return fleet, nil
	default:
		// The operator asked for less than the hardware could serve. Their
		// number binds, and the measurement rides along so the log line can
		// say what was left on the table.
		operator.Agents, operator.Unreported = fleet.Agents, fleet.Unreported
		return operator, nil
	}
}
