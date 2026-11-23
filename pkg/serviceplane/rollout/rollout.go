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

// Package rollout moves a service from the release it runs to a signed
// target release. Each service has two slots, A and B; the active slot
// serves the service's name. One leader-elected controller drives a rollout
// through a Strategy, and records every phase in the service's slot record
// through the store's Guard, so a second controller that races it loses the
// claim and never runs. ABSwitch is the first strategy: fill the inactive
// slot, appraise it, switch the name; an appraisal failure rolls back.
package rollout

import (
	"context"
	"errors"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
)

// Sentinels for errors.Is.
var (
	// ErrNotLeader marks a rollout asked of a controller that is not leader.
	ErrNotLeader = errors.New("rollout: this controller is not the leader")
	// ErrBusy marks a rollout of a service another rollout holds.
	ErrBusy = errors.New("rollout: another rollout holds the service")
	// ErrRolledBack marks a rollout that failed and was rolled back.
	ErrRolledBack = errors.New("rollout: rolled back")
	// ErrRollback marks a rollback that itself failed; the slots need an
	// operator.
	ErrRollback = errors.New("rollout: rollback failed")
	// ErrRelease marks a target release the controller refuses.
	ErrRelease = errors.New("rollout: release refused")
	// ErrConfig marks a controller or strategy built without a required part.
	ErrConfig = errors.New("rollout: incomplete configuration")
	// ErrStore marks a slot record read or write that failed.
	ErrStore = errors.New("rollout: store unavailable")
)

// Slot names one of a service's two slots.
type Slot string

const (
	// SlotA is the first slot; a service's first release fills it.
	SlotA Slot = "a"
	// SlotB is the second slot.
	SlotB Slot = "b"
)

// Other answers the slot that is not s; the empty slot's other is A.
func (s Slot) Other() Slot {
	if s == SlotA {
		return SlotB
	}
	return SlotA
}

// Phase is where a rollout stands.
type Phase string

const (
	// PhaseFill starts the target release in the inactive slot.
	PhaseFill Phase = "fill"
	// PhaseAppraise checks the filled slot against the release's reference
	// values.
	PhaseAppraise Phase = "appraise"
	// PhaseSwitch moves the service's name to the filled slot.
	PhaseSwitch Phase = "switch"
	// PhaseDone is a rollout that switched.
	PhaseDone Phase = "done"
	// PhaseRolledBack is a rollout that failed and was undone.
	PhaseRolledBack Phase = "rolled_back"
	// PhaseFailed is a rollout whose rollback failed too.
	PhaseFailed Phase = "failed"
)

// Terminal reports whether no controller works the phase any more.
func (p Phase) Terminal() bool {
	return p == PhaseDone || p == PhaseRolledBack || p == PhaseFailed
}

// Rollout is one move of a service from its current release to a target.
type Rollout struct {
	Tenant  string `json:"tenant"`
	Service string `json:"service"`
	// Strategy names the strategy that planned the rollout.
	Strategy string `json:"strategy"`
	// From is the active slot when the rollout began; empty for a first
	// release.
	From Slot `json:"from,omitempty"`
	// To is the slot the target release fills.
	To Slot `json:"to"`
	// Target is the release the rollout moves to.
	Target serviceplane.Release `json:"target"`
	Phase  Phase                `json:"phase"`
	// Owner is the controller that claimed the rollout.
	Owner string `json:"owner"`
	// Epoch counts the claims on the service's rollouts; each Roll and
	// Resume takes the next one, so a stale claim can never write.
	Epoch uint64 `json:"epoch"`
	// Reason says why a rollout rolled back or failed.
	Reason string `json:"reason,omitempty"`
}

// Slots is a service's slot record.
type Slots struct {
	Tenant  string `json:"tenant"`
	Service string `json:"service"`
	// Active is the slot that serves the name; empty before any release.
	Active Slot `json:"active,omitempty"`
	// Releases is the release each slot last held.
	Releases map[Slot]serviceplane.Release `json:"releases,omitempty"`
	// Rollout is the latest rollout; a non-terminal one holds the service.
	Rollout *Rollout `json:"rollout,omitempty"`
}

// Current answers the release the active slot serves, or nil.
func (s *Slots) Current() *serviceplane.Release {
	if s.Active == "" {
		return nil
	}
	if r, ok := s.Releases[s.Active]; ok {
		return &r
	}
	return nil
}

// Strategy plans and drives a rollout.
type Strategy interface {
	// Name names the strategy in the rollouts it plans.
	Name() string
	// Plan answers the rollout that moves slots to target.
	Plan(slots *Slots, target *serviceplane.Release) (*Rollout, error)
	// Step does the work of the rollout's phase and answers the next phase.
	// An error stops the rollout; the controller then calls Rollback.
	Step(ctx context.Context, r *Rollout) (Phase, error)
	// Rollback undoes a rollout that stopped before it was done.
	Rollback(ctx context.Context, r *Rollout) error
}

// SlotRuntime fills and drains a service's slots.
type SlotRuntime interface {
	// Fill starts release r in the slot, replacing what it held.
	Fill(ctx context.Context, tenant, service string, slot Slot, r *serviceplane.Release) error
	// Drain stops what the slot runs.
	Drain(ctx context.Context, tenant, service string, slot Slot) error
}

// Appraiser checks a filled slot's evidence against the release's
// reference values.
type Appraiser interface {
	Appraise(ctx context.Context, tenant, service string, slot Slot, r *serviceplane.Release) error
}

// NameSwitch points the service's registered name at a slot.
type NameSwitch interface {
	Switch(ctx context.Context, tenant, service string, slot Slot) error
}

// StepError says which phase of a rollout failed.
type StepError struct {
	Phase Phase
	Cause error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("rollout: %s failed: %v", e.Phase, e.Cause)
}

// Unwrap returns the cause.
func (e *StepError) Unwrap() error { return e.Cause }

// ABSwitch fills the inactive slot, appraises it and switches the name to
// it. The previous slot keeps its release, so the next rollout fills it.
type ABSwitch struct {
	Slots     SlotRuntime
	Appraiser Appraiser
	Names     NameSwitch
}

// Name answers "ab-switch".
func (ABSwitch) Name() string { return "ab-switch" }

// Plan targets the slot that is not active.
func (s ABSwitch) Plan(slots *Slots, target *serviceplane.Release) (*Rollout, error) {
	if s.Slots == nil || s.Appraiser == nil || s.Names == nil {
		return nil, fmt.Errorf("%w: ABSwitch needs slots, an appraiser and a name switch", ErrConfig)
	}
	return &Rollout{
		Tenant:   slots.Tenant,
		Service:  slots.Service,
		Strategy: s.Name(),
		From:     slots.Active,
		To:       slots.Active.Other(),
		Target:   *target,
		Phase:    PhaseFill,
	}, nil
}

// Step runs the rollout's phase.
func (s ABSwitch) Step(ctx context.Context, r *Rollout) (Phase, error) {
	var (
		err  error
		next Phase
	)
	switch r.Phase {
	case PhaseFill:
		err, next = s.Slots.Fill(ctx, r.Tenant, r.Service, r.To, &r.Target), PhaseAppraise
	case PhaseAppraise:
		err, next = s.Appraiser.Appraise(ctx, r.Tenant, r.Service, r.To, &r.Target), PhaseSwitch
	case PhaseSwitch:
		err, next = s.Names.Switch(ctx, r.Tenant, r.Service, r.To), PhaseDone
	default:
		return r.Phase, &StepError{Phase: r.Phase, Cause: errors.New("no step for this phase")}
	}
	if err != nil {
		return r.Phase, &StepError{Phase: r.Phase, Cause: err}
	}
	return next, nil
}

// Rollback points the name back at the previous slot when the switch may
// have moved it, and drains the target slot.
func (s ABSwitch) Rollback(ctx context.Context, r *Rollout) error {
	var errs []error
	if r.Phase == PhaseSwitch && r.From != "" {
		errs = append(errs, s.Names.Switch(ctx, r.Tenant, r.Service, r.From))
	}
	errs = append(errs, s.Slots.Drain(ctx, r.Tenant, r.Service, r.To))
	return errors.Join(errs...)
}
