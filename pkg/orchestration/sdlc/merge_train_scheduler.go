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

package sdlc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// What wakes a train
// =============================================================================
//
// A change queued for the train nudges this scheduler, which starts ONE train
// job for the project. The lease keeps it to one (TrainLeaseSubject): a nudge
// that finds a train already running is refused by the engine and ignored here,
// because the running train forms the next one from the queue when it ends.
//
// A periodic pass is the backstop for the one window that leaves: a change
// queued, and its nudge refused, in the moment between a running train reading
// an empty queue and ending. It is cheap by construction — it reads each
// trained project's queue and starts a job only where something waits or a
// train is in flight with no job driving it.

// DefaultTrainSweepInterval is how often the backstop pass runs.
const DefaultTrainSweepInterval = 5 * time.Minute

// DefaultTrainFormationInterval is the least time between two train jobs the
// scheduler starts for one project.
//
// A TRAIN IS NOT CHEAP TO START: it takes a number, a job record and a
// container. Nothing else bounded how often one was started, so a change that
// was re-queued as fast as it was ejected started a train every thirty seconds
// for an hour (go-luks2, trains #79 to #152, X15). A nudge inside the interval
// starts nothing; the change it was for still waits in the queue, and the
// delivery waiting on it nudges again on its next poll.
const DefaultTrainFormationInterval = time.Minute

// TrainSchedulerConfig wires the scheduler.
type TrainSchedulerConfig struct {
	// Bindings decides whether a project is trained at all.
	Bindings BindingReader

	// Trains is read by the backstop to see whether anything waits.
	Trains TrainStore

	// Jobs is the engine the train job is started on.
	Jobs TriageJobStarter

	// FormationInterval is the least time between two train jobs started for
	// one project. Zero takes DefaultTrainFormationInterval.
	FormationInterval time.Duration

	// Now is the scheduler's clock. Nil takes time.Now.
	Now func() time.Time
}

// TrainScheduler turns a nudge into a train job.
type TrainScheduler struct {
	bindings BindingReader
	trains   TrainStore
	jobs     TriageJobStarter

	interval time.Duration
	now      func() time.Time

	// started holds, per tenant and project, the Unix-nanosecond instant the
	// last train job was started for it: a *atomic.Int64 claimed by
	// compare-and-swap, so concurrent nudges start at most one.
	started sync.Map

	// failures are the per-tenant and per-project failures a sweep deferred,
	// drained by SweepFailures. Only the sweep goroutine touches them.
	failures []error
}

var _ TrainNudger = (*TrainScheduler)(nil)

// NewTrainScheduler builds the scheduler; every collaborator is required.
func NewTrainScheduler(cfg TrainSchedulerConfig) (*TrainScheduler, error) {
	switch {
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "only a trained project has a train to run"}
	case cfg.Trains == nil:
		return nil, &InvalidInputError{Field: "trains", Message: "the backstop reads the queue to decide whether anything waits"}
	case cfg.Jobs == nil:
		return nil, &InvalidInputError{Field: "jobs", Message: "a nudge that starts no job changes nothing"}
	case cfg.FormationInterval < 0:
		return nil, &InvalidInputError{Field: "formation_interval", Message: "must not be negative"}
	}
	interval := cfg.FormationInterval
	if interval == 0 {
		interval = DefaultTrainFormationInterval
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &TrainScheduler{bindings: cfg.Bindings, trains: cfg.Trains, jobs: cfg.Jobs, interval: interval, now: now}, nil
}

// NudgeTrain implements TrainNudger: start the project's train now.
//
// An unbound or untrained project is a QUIET no-op, and so is a project whose
// train is already running, or whose last train was started less than the
// formation interval ago.
func (s *TrainScheduler) NudgeTrain(ctx context.Context, tenantID, projectID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return &InvalidInputError{Field: "tenant_id", Message: "a train is scoped to a tenant"}
	}
	if strings.TrimSpace(projectID) == "" {
		return &InvalidInputError{Field: "project_id", Message: "a train is per project"}
	}
	binding, err := s.bindings.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if !binding.TrainEnabled() {
		return nil
	}
	claim, claimed := s.claimFormation(tenantID, projectID)
	if !claimed {
		return nil
	}
	_, err = s.jobs.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Runner:      TrainRunnerName,
		Description: "Run the merge train of " + binding.Repo().String(),
	})
	if err == nil || jobAlreadyRunning(err) {
		return nil
	}
	// A START THAT FAILED STARTED NOTHING, so it does not hold the interval
	// against the next nudge.
	claim.release()
	return backendError("StartJob", err)
}

// formationClaim is one project's formation slot, taken by claimFormation.
type formationClaim struct {
	slot     *atomic.Int64
	at       int64
	previous int64
}

// release puts back the instant the claim replaced, unless a later claim has
// taken the slot since.
func (c formationClaim) release() {
	c.slot.CompareAndSwap(c.at, c.previous)
}

// claimFormation takes the project's formation slot when the interval since
// the last start has passed.
func (s *TrainScheduler) claimFormation(tenantID, projectID string) (formationClaim, bool) {
	held, _ := s.started.LoadOrStore(formationKey(tenantID, projectID), new(atomic.Int64))
	// started holds nothing but *atomic.Int64; a slot of any other type is
	// one this scheduler cannot claim.
	slot, ok := held.(*atomic.Int64)
	if !ok {
		return formationClaim{}, false
	}
	now := s.now().UnixNano()
	for {
		previous := slot.Load()
		if previous != 0 && now-previous < int64(s.interval) {
			return formationClaim{}, false
		}
		if slot.CompareAndSwap(previous, now) {
			return formationClaim{slot: slot, at: now, previous: previous}, true
		}
	}
}

// formationKey is the rate limiter's key for one project.
func formationKey(tenantID, projectID string) string { return tenantID + "\x00" + projectID }

// Sweep nudges every trained project whose queue holds a change or whose
// ledger holds a train, and reports how many it nudged.
//
// THE ONE FAILURE THAT STOPS IT is not being able to enumerate the tenants,
// for the reason the triage sweep gives: a pass that does not know what it was
// meant to visit must not report a clean one.
func (s *TrainScheduler) Sweep(ctx context.Context, tenants orchestration.TenantLister) (int, error) {
	if tenants == nil {
		return 0, &InvalidInputError{Field: "tenants", Message: "a sweep that enumerates no tenant visits nothing while claiming a clean pass"}
	}
	tenantIDs, err := tenants.ListTenantIDs(ctx)
	if err != nil {
		return 0, backendError("ListTenantIDs", err)
	}
	nudged := 0
	for _, tenantID := range tenantIDs {
		if err := ctx.Err(); err != nil {
			return nudged, err
		}
		// The engine guards StartJob on the tenant bound to the CONTEXT; see
		// TriageScheduler.Sweep.
		tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)
		bindings, err := s.bindings.ListBindings(tenantCtx, tenantID)
		if err != nil {
			s.failures = append(s.failures, err)
			continue
		}
		for _, binding := range bindings {
			if binding == nil || !binding.TrainEnabled() {
				continue
			}
			waiting, err := s.waiting(tenantCtx, tenantID, binding.ProjectID)
			if err != nil {
				s.failures = append(s.failures, err)
				continue
			}
			if !waiting {
				continue
			}
			if err := s.NudgeTrain(tenantCtx, tenantID, binding.ProjectID); err != nil {
				s.failures = append(s.failures, err)
				continue
			}
			nudged++
		}
	}
	return nudged, nil
}

// waiting reports whether a project has a train in flight or a change queued.
func (s *TrainScheduler) waiting(ctx context.Context, tenantID, projectID string) (bool, error) {
	ledger, err := s.trains.GetTrainLedger(ctx, tenantID, projectID)
	if err != nil {
		return false, err
	}
	if ledger.Active != nil {
		return true, nil
	}
	entries, err := s.trains.ListTrainEntries(ctx, tenantID, projectID)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.State == TrainEntryQueued || entry.State == TrainEntryRiding {
			return true, nil
		}
	}
	return false, nil
}

// SweepFailures returns and clears the failures the last sweeps deferred.
func (s *TrainScheduler) SweepFailures() []error {
	failures := s.failures
	s.failures = nil
	return failures
}
