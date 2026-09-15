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
	"time"
)

// =============================================================================
// Backend capacity — bounding a model server that several projects share
// =============================================================================
//
// WHAT THE OTHER TWO QUOTAS CANNOT SAY. A project quota bounds one project's
// concurrent work and the fleet quota bounds worker slots across the
// deployment. Neither can express "THIS MODEL SERVER can serve two things at
// once", and for a locally hosted model that is the only limit that matters:
// it is one GPU holding one model, serving every project that names it.
//
// THE FAILURE IT EXISTS TO STOP, MEASURED ON THE BURN-IN FLEET 2026-09-14.
// go-luks2 had five open pull requests, every one green and unjudged. The
// review floor dispatched a review for each in the same pass — correctly, by
// its own rules — and because a review is OffBudget by construction
// (ReviewRunner.OffProjectBudget), no quota counted them. All five landed on
// one 27B model on one GPU. Symptoms: the GPU pinned at 99%, every review
// container idle at 0.08% CPU waiting on the model, a trivial prompt that
// normally answers in about two seconds taking 115, and not one review
// finished after 37 minutes. Nothing was broken; everything was queued behind
// everything else.
//
// WHY IT IS A LEASE DECORATOR AND NOT A CHECK IN THE DISPATCHER. The lease is
// the one record every node reads, so a limit enforced here holds across the
// fleet and across restarts; a counter in the dispatcher would bound only the
// node that happened to run it, and two nodes would each admit the limit. It
// also composes: the same wrap bounds a worker backend, not just a reviewer,
// because the field it reads is on every lease.
//
// AN OFF-BUDGET LEASE IS STILL COUNTED HERE, and that is the whole point.
// OffBudget says "this job does not spend a PROJECT's worker slot" — a review
// must not queue behind the work it unblocks. It does not say the job is free:
// a review occupies the model exactly as a worker does. Reading OffBudget here
// would reproduce the bug this file exists to fix.

// BackendCapacityExceededError reports a backend at its concurrency limit.
type BackendCapacityExceededError struct {
	TenantID string
	Backend  string
	Live     int
	Limit    int
	RetryAt  time.Time
}

// Error implements the error interface.
func (e *BackendCapacityExceededError) Error() string {
	return "job lease: backend concurrency exceeded: tenant=" + e.TenantID +
		" backend=" + e.Backend +
		" live=" + strconv.Itoa(e.Live) + " limit=" + strconv.Itoa(e.Limit)
}

// Unwrap ties the typed error to its sentinel.
func (e *BackendCapacityExceededError) Unwrap() error { return ErrBackendCapacityExceeded }

// backendCapacityJobLeaseStore refuses a lease that would put a backend over
// its concurrency limit, then defers to the store beneath it.
type backendCapacityJobLeaseStore struct {
	JobLeaseStore
	limits map[string]int
	ttl    time.Duration
	now    jobLeaseClock
}

// NewBackendCapacityJobLeaseStore wraps store so Acquire honours a
// per-backend concurrency limit.
//
// limits maps a backend name to the most leases that backend may hold live at
// once. A backend absent from the map, or mapped to zero or less, is
// unlimited — the same shape as a project quota of zero, so a deployment that
// configures nothing behaves exactly as it did before this existed. An empty
// or nil map is accepted and makes the wrap a pass-through rather than an
// error, because "no backend is capped" is a legitimate configuration.
func NewBackendCapacityJobLeaseStore(store JobLeaseStore, limits map[string]int, ttl time.Duration) (JobLeaseStore, error) {
	if store == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the lease store it admits into"}
	}
	if ttl <= 0 {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the TTL that decides which leases are live"}
	}
	normalised := make(map[string]int, len(limits))
	for backend, limit := range limits {
		name := strings.ToLower(strings.TrimSpace(backend))
		if name == "" || limit <= 0 {
			continue
		}
		normalised[name] = limit
	}
	return &backendCapacityJobLeaseStore{JobLeaseStore: store, limits: normalised, ttl: ttl, now: utcNow}, nil
}

// Acquire implements JobLeaseStore.
func (s *backendCapacityJobLeaseStore) Acquire(ctx context.Context, lease *JobLease) error {
	if err := lease.validate(); err != nil {
		return err
	}
	if err := s.admit(ctx, lease); err != nil {
		return err
	}
	return s.JobLeaseStore.Acquire(ctx, lease)
}

// admit counts this backend's live leases across every project of the tenant.
//
// ACROSS PROJECTS, DELIBERATELY: the model server is shared, so counting only
// the contender's own project would let N projects each run the limit.
func (s *backendCapacityJobLeaseStore) admit(ctx context.Context, lease *JobLease) error {
	backend := strings.ToLower(strings.TrimSpace(lease.Backend))
	if backend == "" {
		return nil
	}
	limit, capped := s.limits[backend]
	if !capped {
		return nil
	}
	leases, err := s.List(ctx, lease.TenantID, "")
	if err != nil {
		return err
	}
	now := s.now()
	live := 0
	for _, held := range leases {
		if held.JobID == lease.JobID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(held.Backend), backend) {
			continue
		}
		// NOT CountsAgainstBudget: that answers the PROJECT budget question
		// and excludes an off-budget lease, which is every review. A review
		// occupies the model like anything else.
		if held.Live(now, s.ttl) {
			live++
		}
	}
	if live >= limit {
		return &BackendCapacityExceededError{
			TenantID: lease.TenantID,
			Backend:  backend,
			Live:     live,
			Limit:    limit,
			RetryAt:  now.Add(s.ttl),
		}
	}
	return nil
}

// LiveJobsForBackend implements ProfileLoadReader over the SAME count the cap
// enforces.
//
// PLACEMENT AND ADMISSION MUST AGREE ABOUT "BUSY". If a placer counted
// differently — its own gauge, a heartbeat, a sampled metric — it would admit
// work the cap then refuses, and the operator would see a pool that selected a
// profile and a lease that rejected it, with no single number to check. There
// is one definition of live, and this is it.
func (s *backendCapacityJobLeaseStore) LiveJobsForBackend(ctx context.Context, tenantID, backend string) (int, error) {
	name := strings.ToLower(strings.TrimSpace(backend))
	if name == "" {
		return 0, nil
	}
	leases, err := s.List(ctx, tenantID, "")
	if err != nil {
		return 0, err
	}
	now := s.now()
	live := 0
	for _, held := range leases {
		if !strings.EqualFold(strings.TrimSpace(held.Backend), name) {
			continue
		}
		if held.Live(now, s.ttl) {
			live++
		}
	}
	return live, nil
}

var _ ProfileLoadReader = (*backendCapacityJobLeaseStore)(nil)
