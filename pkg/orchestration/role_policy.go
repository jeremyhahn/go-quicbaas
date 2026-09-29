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
	"strings"
)

// =============================================================================
// Which ROLE gets a scarce accelerator
// =============================================================================
//
// A POOL ANSWERS "WHICH AGENT", AND THAT IS ONLY HALF THE QUESTION. Ordering
// agents by preference decides where work goes when several places could take
// it. It cannot decide who goes FIRST when one place is worth more to one kind
// of work than another — and on a fleet with one large accelerator and several
// small ones, that is the question that matters. The large card is where code
// review belongs; triage would happily fill it and starve the thing it was
// bought for.
//
// THE POLICY IS DATA AND IT IS TUNABLE WHILE THE FLEET RUNS. It rides on the
// agent, which is a stored, editable record — so "reserve the big card for
// review, but let triage use it when review is idle" is an edit, not a
// redeploy. That is the whole point: the right split depends on the queue, and
// the queue changes hourly.
//
// IDLE CAPACITY IS NOT RESERVED CAPACITY. An agent preferred by review does
// not sit empty while triage queues: it yields whatever review is not asking
// for. Reserving unconditionally would trade one starvation for another, and
// the measured cost of an idle accelerator is the same whichever role is not
// using it.

// RolePolicy says who an Agent is FOR, and on what terms it helps everybody
// else when it sits in a pool shared between roles.
//
// The zero value is "no preference": every role competes on equal terms, which
// is how every Agent behaves unless an operator says otherwise.
type RolePolicy struct {
	// Prefer is the role this agent is held for. Empty prefers nobody.
	Prefer AgentRole `json:"prefer,omitempty"`

	// Reserve is how many of the agent's slots are held for Prefer even
	// when it has no queued work. Zero — the useful default — holds none, so
	// an idle agent is fully available to other roles.
	//
	// IT EXISTS FOR LATENCY, NOT FOR FAIRNESS. A review that must start within
	// seconds cannot wait for a triage turn to finish on a model that takes
	// minutes to unload, and the only way to promise that is to keep a slot
	// empty. Set it when the preferred role's RESPONSE TIME matters more than
	// the accelerator's utilisation; leave it zero otherwise.
	Reserve int `json:"reserve,omitempty"`

	// Exclusive refuses every role but Prefer, queue or no queue.
	//
	// THE ESCAPE HATCH, AND THE WASTEFUL ONE. It is right when sharing is
	// impossible rather than merely undesirable — an agent whose model takes
	// minutes to swap, or an accelerator that cannot hold two models at once —
	// and wrong whenever the cost is only preference, because it idles the
	// hardware for a role that is not asking.
	Exclusive bool `json:"exclusive,omitempty"`
}

// PrefersNobody reports whether this policy expresses no preference at all.
func (p RolePolicy) PrefersNobody() bool { return strings.TrimSpace(string(p.Prefer)) == "" }

// Validate refuses a policy that cannot mean anything.
func (p RolePolicy) Validate() error {
	if p.Reserve < 0 {
		return &InputValidationError{Field: "role_policy.reserve", Message: "use zero to reserve nothing, not a negative"}
	}
	if p.PrefersNobody() && (p.Reserve > 0 || p.Exclusive) {
		return &InputValidationError{
			Field:   "role_policy.prefer",
			Message: "reserving or excluding needs a role to reserve FOR; name one or drop the terms",
		}
	}
	if !p.PrefersNobody() && !p.Prefer.Valid() {
		return &InputValidationError{Field: "role_policy.prefer", Message: "unknown role " + strconv.Quote(string(p.Prefer))}
	}
	return nil
}

// RoleDemandReader answers how much work a role currently has waiting.
//
// IT IS "WAITING", NOT "RUNNING", and the difference is the whole mechanism.
// Live jobs are already counted by ProfileLoadReader against the concurrency
// cap; what this adds is whether the preferred role is ASKING for more. A
// agent yields to another role exactly when its preferred role's queue is
// empty, so the signal has to be the queue.
type RoleDemandReader interface {
	// QueuedForRole returns how many jobs of this role are waiting to start
	// across the tenant. Zero means the role is not asking for anything.
	QueuedForRole(ctx context.Context, tenantID string, role AgentRole) (int, error)
}

// admits reports whether an agent will take work of this role right now,
// given how loaded it is and what its preferred role is waiting on.
//
// A NIL DEMAND READER MEANS "NOBODY IS WAITING", which makes every non-
// exclusive agent shareable. That is the correct degradation: a deployment
// that cannot measure its queues should not have its accelerators idled by a
// policy it cannot evaluate.
func (p RolePolicy) admits(ctx context.Context, demand RoleDemandReader, tenantID string, role AgentRole, capacity, live int) (bool, string) {
	if p.PrefersNobody() || role == p.Prefer {
		return true, ""
	}
	if p.Exclusive {
		return false, "held exclusively for " + p.Prefer.String()
	}
	// Slots kept empty for the preferred role are unavailable to anybody else,
	// whatever the queue says — that is what reserving means.
	if p.Reserve > 0 && capacity > 0 && live >= capacity-p.Reserve {
		return false, strconv.Itoa(p.Reserve) + " slot(s) reserved for " + p.Prefer.String()
	}
	if demand == nil {
		return true, ""
	}
	queued, err := demand.QueuedForRole(ctx, tenantID, p.Prefer)
	if err != nil {
		// AN UNREADABLE QUEUE ADMITS THE WORK. Refusing on a failed read would
		// stop the fleet on a storage blip, and the concurrency cap still
		// bounds what actually starts.
		return true, ""
	}
	if queued > 0 {
		return false, "yielding to " + strconv.Itoa(queued) + " queued " + p.Prefer.String() + " job(s)"
	}
	return true, ""
}
