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

package serviceplane

import (
	"context"
	"errors"
	"fmt"
)

// Roller rolls a tenant's service out to a signed release: the one
// leader-elected rollout controller (rollout.PlaneRoller), which verifies
// the release, fills the service's inactive slot, appraises it and
// switches to it through FillSlot, SwitchSlot and DrainSlot. It answers the
// service the release is for.
type Roller interface {
	Roll(ctx context.Context, tenant string, sr *SignedRelease) (service string, err error)
}

// RollerFunc adapts a function to a Roller.
type RollerFunc func(ctx context.Context, tenant string, sr *SignedRelease) (string, error)

// Roll calls f.
func (f RollerFunc) Roll(ctx context.Context, tenant string, sr *SignedRelease) (string, error) {
	return f(ctx, tenant, sr)
}

// Redeploy moves a deployed service of tenant to the signed release sr:
// the release (image, manifest, policy, reference values, provenance) is
// rolled out through the plane's Roller into the service's inactive slot,
// appraised, and switched to; a failure rolls back and leaves the active
// slot serving. A plane built without a Roller refuses with ErrConfig.
func (p *Plane) Redeploy(ctx context.Context, tenant string, sr *SignedRelease) (*Service, error) {
	if p.roller == nil {
		return nil, &ServiceError{Op: string(VerbRedeploy), Tenant: tenant, Sentinel: ErrConfig,
			Cause: errors.New("a redeploy rolls a release out through the rollout controller, and this plane has none")}
	}
	if sr == nil {
		return nil, &ServiceError{Op: string(VerbRedeploy), Tenant: tenant, Sentinel: ErrSignature,
			Cause: errors.New("no signed release")}
	}
	name, err := p.roller.Roll(ctx, tenant, sr)
	if err != nil {
		var se *ServiceError
		if errors.As(err, &se) {
			return nil, err
		}
		return nil, &ServiceError{Op: string(VerbRedeploy), Tenant: tenant, Name: name, Sentinel: ErrRollout, Cause: err}
	}
	return p.Get(ctx, tenant, name)
}

// FillSlot starts rel in slot beside the service's active instance,
// replacing what the slot held, under the rollout claim epoch. The service
// keeps serving from its active slot: its state, manifest and grants do
// not change. An epoch older than the newest the service took is refused
// with ErrFenced, and so is filling the active slot.
func (p *Plane) FillSlot(ctx context.Context, epoch uint64, slot string, rel *Release) error {
	if rel == nil || slot == "" {
		return &ServiceError{Op: "fill", Sentinel: ErrInvalidManifest, Cause: errors.New("a slot and a release are required")}
	}
	m := &rel.Manifest
	if rel.Kind != ReleaseService {
		return &ServiceError{Op: "fill", Tenant: m.Tenant, Name: m.Name, Sentinel: ErrInvalidManifest,
			Cause: fmt.Errorf("a %s release is staged by the node, not filled into a slot", rel.Kind)}
	}
	if m.Class == ClassBroker {
		return &ServiceError{Op: "fill", Tenant: m.Tenant, Name: m.Name, Sentinel: ErrBrokerClass}
	}
	cur, prior, err := p.slotClaim(ctx, "fill", m.Tenant, m.Name, epoch)
	if err != nil {
		return err
	}
	if slot == cur.svc.Slot {
		cause := fmt.Errorf("slot %s is the active slot", slot)
		return p.slotSettle(ctx, "fill", cur, prior, ErrTransition, cause, nil)
	}
	if held, ok := cur.svc.Slots[slot]; ok {
		inst := slotInstance(cur.svc, slot, &held)
		if err := p.held(ctx, "fill", &cur, func(c context.Context) error { return p.launcher.Halt(c, inst) }); err != nil {
			if lostClaim(err) {
				return err
			}
			return p.slotSettle(ctx, "fill", cur, prior, ErrLaunch, fmt.Errorf("drain slot %s: %w", slot, err), nil)
		}
	}
	inst := slotInstance(cur.svc, slot, rel)
	if err := p.held(ctx, "fill", &cur, func(c context.Context) error { return p.launcher.Launch(c, inst) }); err != nil {
		if lostClaim(err) {
			return err
		}
		return p.slotSettle(ctx, "fill", cur, prior, ErrLaunch, err, func(next *Service) {
			delete(next.Slots, slot)
		})
	}
	return p.slotSettle(ctx, "fill", cur, prior, nil, nil, func(next *Service) {
		next.Slots[slot] = *rel
	})
}

// SwitchSlot makes slot, which FillSlot filled, the service's active
// instance under the rollout claim epoch: the service's grants are applied
// for the slot's manifest (its name moves to it), and the record then runs
// the slot's release. Switching to the active slot changes nothing. The
// instance Deploy launched holds no slot, so the first switch halts it; a
// slot instance that was active stays up for the next rollout to fill. A
// failed grant leaves the previous instance active, its grants restored as
// Replace restores them.
func (p *Plane) SwitchSlot(ctx context.Context, epoch uint64, tenant, name, slot string) error {
	cur, prior, err := p.slotClaim(ctx, "switch", tenant, name, epoch)
	if err != nil {
		return err
	}
	if slot == cur.svc.Slot {
		return p.slotSettle(ctx, "switch", cur, prior, nil, nil, nil)
	}
	rel, ok := cur.svc.Slots[slot]
	if !ok {
		return p.slotSettle(ctx, "switch", cur, prior, ErrTransition, fmt.Errorf("slot %s holds no release", slot), nil)
	}
	if cur.svc.Slot == "" && prior == StateRunning {
		running := cur.svc
		if err := p.held(ctx, "switch", &cur, func(c context.Context) error { return p.launcher.Halt(c, running) }); err != nil {
			if lostClaim(err) {
				return err
			}
			return p.slotSettle(ctx, "switch", cur, prior, ErrLaunch, fmt.Errorf("halt the deployed instance: %w", err), nil)
		}
	}
	held := cur.svc.Grants
	cleanup := cur.svc.Cleanup
	last, grants, incomplete, err := p.grant(ctx, VerbRedeploy, cur, cur.svc, &rel.Manifest, held)
	if lostClaim(err) {
		return err
	}
	if err != nil {
		return p.slotSettle(ctx, "switch", last, prior, ErrGrant, err, func(next *Service) {
			next.Grants = make(map[GrantKind]string, len(held)+len(grants))
			for k, ref := range held {
				next.Grants[k] = ref
			}
			for k, ref := range grants {
				next.Grants[k] = ref
			}
			next.Cleanup = cleanup || incomplete
			if next.Slot == "" {
				// The deployed instance was halted for the switch.
				next.State = StateHalted
			}
		})
	}
	return p.slotSettle(ctx, "switch", last, prior, nil, nil, func(next *Service) {
		r := rel
		next.Release, next.Manifest, next.Slot = &r, r.Manifest, slot
		next.Grants, next.Cleanup = grants, false
		next.State = StateRunning
		next.Revision++
	})
}

// DrainSlot stops what slot runs under the rollout claim epoch and empties
// it. Draining an empty slot changes nothing; draining the active slot is
// refused.
func (p *Plane) DrainSlot(ctx context.Context, epoch uint64, tenant, name, slot string) error {
	cur, prior, err := p.slotClaim(ctx, "drain", tenant, name, epoch)
	if err != nil {
		return err
	}
	if slot == cur.svc.Slot {
		return p.slotSettle(ctx, "drain", cur, prior, ErrTransition, fmt.Errorf("slot %s is the active slot", slot), nil)
	}
	held, ok := cur.svc.Slots[slot]
	if !ok {
		return p.slotSettle(ctx, "drain", cur, prior, nil, nil, nil)
	}
	inst := slotInstance(cur.svc, slot, &held)
	if err := p.held(ctx, "drain", &cur, func(c context.Context) error { return p.launcher.Halt(c, inst) }); err != nil {
		if lostClaim(err) {
			return err
		}
		return p.slotSettle(ctx, "drain", cur, prior, ErrLaunch, err, nil)
	}
	return p.slotSettle(ctx, "drain", cur, prior, nil, nil, func(next *Service) {
		delete(next.Slots, slot)
	})
}

// slotInstance is the service as it runs in slot: rel's manifest, under
// the service's grants. The launcher tells instances apart by Slot.
func slotInstance(svc *Service, slot string, rel *Release) *Service {
	inst := *svc
	r := *rel
	inst.Release, inst.Manifest, inst.Slot = &r, r.Manifest, slot
	return &inst
}

// slotClaim claims the service for a slot change under epoch, as claim
// does for a verb, and answers the state the change leaves it in. An epoch
// older than the newest the service took is refused with ErrFenced; the
// claim records epoch as the newest.
func (p *Plane) slotClaim(ctx context.Context, op, tenant, name string, epoch uint64) (*stored, State, error) {
	cur, err := p.store.load(ctx, op, tenant, name)
	if err != nil {
		return nil, "", err
	}
	if epoch < cur.svc.Epoch {
		return nil, "", p.fail(op, cur.svc, ErrFenced, fmt.Errorf("epoch %d, the newest is %d", epoch, cur.svc.Epoch))
	}
	legal := false
	for _, s := range allowed[VerbRedeploy] {
		legal = legal || s == cur.svc.State
	}
	if !legal {
		return nil, "", p.fail(op, cur.svc, ErrTransition, &TransitionError{Verb: VerbRedeploy, State: cur.svc.State})
	}
	prior := cur.svc.State
	next := *cur.svc
	next.State, next.Verb, next.Epoch, next.UpdatedAt = StateTransitioning, VerbRedeploy, epoch, p.now()
	st, err := p.store.swap(ctx, op, cur, &next)
	if err != nil {
		return nil, "", err
	}
	return st, prior, nil
}

// slotSettle ends a slot change: the record returns to prior, changed by
// change (which gets its own copy of Slots), and the change's error is
// answered, coded with sentinel; a nil sentinel is success.
func (p *Plane) slotSettle(ctx context.Context, op string, cur *stored, prior State, sentinel, cause error, change func(*Service)) error {
	next := *cur.svc
	next.Slots = make(map[string]Release, len(cur.svc.Slots)+1)
	for s, r := range cur.svc.Slots {
		next.Slots[s] = r
	}
	next.State, next.Verb, next.UpdatedAt = prior, "", p.now()
	if change != nil {
		change(&next)
	}
	if len(next.Slots) == 0 {
		next.Slots = nil
	}
	if cause != nil && next.State == StateHalted {
		next.Reason = cause.Error()
	}
	sctx, cancel := settleCtx(ctx)
	defer cancel()
	if _, err := p.store.swap(sctx, op, cur, &next); err != nil {
		if sentinel == nil {
			return err
		}
		cause = errors.Join(cause, err)
	}
	if sentinel == nil {
		return nil
	}
	return p.fail(op, &next, sentinel, cause)
}
