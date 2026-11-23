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

package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
)

// keyPrefix is where slot records live in the platform shard.
const keyPrefix = "serviceplane/rollouts/"

func slotsKey(tenant, service string) string { return keyPrefix + tenant + "/" + service }

// Controller drives rollouts. Only the leader runs one, and each phase is
// written through the Guard against the record the controller read, so of
// two controllers that race on a service exactly one runs it.
type Controller struct {
	// ID names this controller in the rollouts it claims.
	ID string
	// Guard arbitrates every slot record write.
	Guard serviceplane.Guard
	// KV reads slot records.
	KV serviceplane.Reader
	// Leader gates every step; nil means always leader (a single node).
	Leader scheduler.LeaderChecker
	// Verifier is the keyring release signatures must verify against.
	Verifier serviceplane.Keyring
	// Strategy plans and drives each rollout.
	Strategy Strategy
}

func (c *Controller) check() error {
	if strings.TrimSpace(c.ID) == "" || c.Guard == nil || c.KV == nil || c.Strategy == nil || len(c.Verifier) == 0 {
		return fmt.Errorf("%w: a controller needs an id, a guard, a reader, a keyring and a strategy", ErrConfig)
	}
	return nil
}

func (c *Controller) leader() error {
	if c.Leader == nil {
		return nil
	}
	ok, err := c.Leader.IsLeader()
	if err != nil || !ok {
		return ErrNotLeader
	}
	return nil
}

// Slots answers the service's slot record; a service never rolled out has
// an empty one.
func (c *Controller) Slots(ctx context.Context, tenant, service string) (*Slots, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	slots, _, err := c.load(ctx, tenant, service)
	return slots, err
}

// Roll verifies the signed release and moves the tenant's service to it.
func (c *Controller) Roll(ctx context.Context, tenant string, sr *serviceplane.SignedRelease) (*Slots, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	if err := c.leader(); err != nil {
		return nil, err
	}
	target, err := c.Verifier.VerifyRelease(sr)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRelease, err)
	}
	if tenant == "" || target.Manifest.Tenant != tenant {
		return nil, fmt.Errorf("%w: the release is for tenant %q, not %q", ErrRelease, target.Manifest.Tenant, tenant)
	}
	if target.Kind != serviceplane.ReleaseService {
		return nil, fmt.Errorf("%w: a %s release is staged by the node, not rolled out here", ErrRelease, target.Kind)
	}
	slots, raw, err := c.load(ctx, tenant, target.Manifest.Name)
	if err != nil {
		return nil, err
	}
	if slots.Rollout != nil && !slots.Rollout.Phase.Terminal() {
		return nil, fmt.Errorf("%w: %s is %s under %s", ErrBusy, target.Manifest.Name, slots.Rollout.Phase, slots.Rollout.Owner)
	}
	ro, err := c.Strategy.Plan(slots, target)
	if err != nil {
		return nil, err
	}
	ro.Owner = c.ID
	slots.Rollout = ro
	if raw, err = c.write(ctx, slots, raw); err != nil {
		return nil, err
	}
	return c.drive(ctx, slots, raw)
}

// Resume takes over the service's unfinished rollout, as a new leader does
// after the one that claimed it stepped down.
func (c *Controller) Resume(ctx context.Context, tenant, service string) (*Slots, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	if err := c.leader(); err != nil {
		return nil, err
	}
	slots, raw, err := c.load(ctx, tenant, service)
	if err != nil {
		return nil, err
	}
	ro := slots.Rollout
	if ro == nil || ro.Phase.Terminal() {
		return slots, nil
	}
	if ro.Strategy != c.Strategy.Name() {
		return nil, fmt.Errorf("%w: the rollout runs %s, this controller %s", ErrConfig, ro.Strategy, c.Strategy.Name())
	}
	ro.Owner = c.ID
	if raw, err = c.write(ctx, slots, raw); err != nil {
		return nil, err
	}
	return c.drive(ctx, slots, raw)
}

// drive steps the claimed rollout to a terminal phase, writing each phase.
// A lost write means another controller took the record: this one stops.
func (c *Controller) drive(ctx context.Context, slots *Slots, raw []byte) (*Slots, error) {
	ro := slots.Rollout
	for !ro.Phase.Terminal() {
		if err := c.leader(); err != nil {
			return slots, err
		}
		next, stepErr := c.Strategy.Step(ctx, ro)
		if stepErr != nil {
			sentinel := ErrRolledBack
			ro.Reason = stepErr.Error()
			// Rollback sees the phase that failed: what it undoes depends
			// on it (a failed switch points the name back). The terminal
			// phase is set after.
			if rbErr := c.Strategy.Rollback(ctx, ro); rbErr != nil {
				sentinel = ErrRollback
				ro.Phase = PhaseFailed
				ro.Reason += "; rollback: " + rbErr.Error()
			} else {
				ro.Phase = PhaseRolledBack
			}
			if _, err := c.write(ctx, slots, raw); err != nil {
				return slots, errors.Join(fmt.Errorf("%w: %w", sentinel, stepErr), err)
			}
			return slots, fmt.Errorf("%w: %w", sentinel, stepErr)
		}
		ro.Phase = next
		if next == PhaseDone {
			if slots.Releases == nil {
				slots.Releases = map[Slot]serviceplane.Release{}
			}
			slots.Releases[ro.To] = ro.Target
			slots.Active = ro.To
		}
		var err error
		if raw, err = c.write(ctx, slots, raw); err != nil {
			return slots, err
		}
	}
	return slots, nil
}

func (c *Controller) load(ctx context.Context, tenant, service string) (*Slots, []byte, error) {
	if tenant == "" || service == "" || strings.Contains(tenant, "/") || strings.Contains(service, "/") {
		return nil, nil, fmt.Errorf("%w: a tenant and a service are required", ErrRelease)
	}
	raw, err := c.KV.Get(ctx, slotsKey(tenant, service))
	if err != nil && !qberrors.IsNotFound(err) {
		return nil, nil, fmt.Errorf("%w: %w", ErrStore, err)
	}
	if err != nil || raw == nil {
		return &Slots{Tenant: tenant, Service: service}, nil, nil
	}
	var slots Slots
	if err := json.Unmarshal(raw, &slots); err != nil {
		return nil, nil, fmt.Errorf("%w: slot record: %w", ErrStore, err)
	}
	return &slots, raw, nil
}

// write stores slots in place of held (nil: the record must be absent) and
// answers the bytes written.
func (c *Controller) write(ctx context.Context, slots *Slots, held []byte) ([]byte, error) {
	next, err := json.Marshal(slots)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStore, err)
	}
	key := slotsKey(slots.Tenant, slots.Service)
	var ok bool
	if held == nil {
		ok, err = c.Guard.Create(ctx, key, next)
	} else {
		ok, err = c.Guard.Swap(ctx, key, held, next)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStore, err)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s changed under this controller", ErrBusy, slots.Tenant, slots.Service)
	}
	return next, nil
}
