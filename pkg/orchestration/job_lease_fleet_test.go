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
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// erroringFleetCapacity is a reader that will not answer. Its refusal must reach
// the caller as an error rather than as an admitting number.
type erroringFleetCapacity struct {
	err error
}

func (c *erroringFleetCapacity) FleetCapacity(context.Context, time.Time) (FleetCapacity, error) {
	return FleetCapacity{}, c.err
}

// fleetLeaseRig is a lease store bounded by BOTH capacity axes, in the order the
// server wires them: the project check first, the fleet check outside it.
type fleetLeaseRig struct {
	store    JobLeaseStore
	projects ProjectStore
	clock    *fakeClock
}

func newFleetLeaseRig(t *testing.T, slots int) *fleetLeaseRig {
	t.Helper()

	inner, err := NewProcessJobLeaseStore(leaseTestTTL)
	require.NoError(t, err)
	clock := newFakeClock()
	inner.(*processJobLeaseStore).now = clock.Now

	projects := NewInMemoryProjectStore()
	capped, err := NewProjectCapacityJobLeaseStore(inner, projects, leaseTestTTL)
	require.NoError(t, err)
	capped.(*projectCapacityJobLeaseStore).now = clock.Now

	capacity, err := NewDeclaredFleetCapacity(slots)
	require.NoError(t, err)
	store, err := NewFleetCapacityJobLeaseStore(capped, capacity, leaseTestTTL)
	require.NoError(t, err)
	store.(*fleetCapacityJobLeaseStore).now = clock.Now

	return &fleetLeaseRig{store: store, projects: projects, clock: clock}
}

// project registers an autonomous project with its own concurrency quota.
func (r *fleetLeaseRig) project(t *testing.T, projectID string, maxConcurrency int) {
	t.Helper()
	require.NoError(t, r.projects.Create(context.Background(), &Project{
		ID: projectID, TenantID: "acme", Name: projectID,
		Quotas: ProjectQuotas{MaxConcurrency: maxConcurrency},
	}))
}

// acquire takes one lease for a project.
func (r *fleetLeaseRig) acquire(projectID string, n int) error {
	ref := projectID + "-" + strconv.Itoa(n)
	return r.store.Acquire(context.Background(), &JobLease{
		TenantID: "acme", ProjectID: projectID, Ref: ref, JobID: ref, Generation: 1,
	})
}

// =============================================================================
// The aggregate bound — the failure observed on 2026-09-12
// =============================================================================

// TestFleetCapacity_ProjectsInsideTheirOwnQuotaCannotExceedTheFleet is the whole
// point of the axis, stated as the failure it prevents: three projects each
// permitted two concurrent jobs would run six on a fleet with four slots, and
// every per-project check would have been satisfied.
func TestFleetCapacity_ProjectsInsideTheirOwnQuotaCannotExceedTheFleet(t *testing.T) {
	rig := newFleetLeaseRig(t, 4)
	for _, projectID := range []string{"p1", "p2", "p3"} {
		rig.project(t, projectID, 2)
	}

	admitted, refused := 0, 0
	var last error
	for round := 0; round < 2; round++ {
		for _, projectID := range []string{"p1", "p2", "p3"} {
			if err := rig.acquire(projectID, round); err != nil {
				refused++
				last = err
				continue
			}
			admitted++
		}
	}

	assert.Equal(t, 4, admitted, "the fleet's four slots bound the aggregate, whatever each project's quota permits")
	assert.Equal(t, 2, refused)

	var exceeded *FleetCapacityExceededError
	require.ErrorAs(t, last, &exceeded)
	assert.Equal(t, 4, exceeded.Live)
	assert.Equal(t, 4, exceeded.Limit)
	assert.Equal(t, FleetCapacityOperator, exceeded.Source)
	assert.False(t, exceeded.RetryAt.IsZero(), "a refusal says when the slot could come back")
	assert.ErrorIs(t, last, ErrFleetCapacityExceeded)
	assert.NotErrorIs(t, last, ErrProjectCapacityExceeded,
		"the fleet refusal must send an operator to the fleet's number, not to a project quota")
}

// TestFleetCapacity_TheProjectAxisStillBindsBelowTheFleet: the fleet bound is an
// ADDITIONAL ceiling, not a replacement. A project at its own quota is refused by
// its own quota even when the fleet has room, or the per-project setting would
// have stopped meaning anything.
func TestFleetCapacity_TheProjectAxisStillBindsBelowTheFleet(t *testing.T) {
	rig := newFleetLeaseRig(t, 10)
	rig.project(t, "p1", 1)

	require.NoError(t, rig.acquire("p1", 0))
	err := rig.acquire("p1", 1)

	var exceeded *ProjectCapacityExceededError
	require.ErrorAs(t, err, &exceeded)
	assert.Equal(t, 1, exceeded.Limit)
}

// TestFleetCapacity_AReleasedOrStaleHolderFreesASlot: the fleet count is over
// LIVE leases, so a job that finished or a node that died returns its slot — the
// same liveness rule the project axis uses, from the same records.
func TestFleetCapacity_AReleasedOrStaleHolderFreesASlot(t *testing.T) {
	rig := newFleetLeaseRig(t, 1)
	rig.project(t, "p1", 0)

	first := &JobLease{TenantID: "acme", ProjectID: "p1", Ref: "a", JobID: "job-a", Generation: 1}
	require.NoError(t, rig.store.Acquire(context.Background(), first))
	require.Error(t, rig.acquire("p1", 1), "the fleet's one slot is held")

	require.NoError(t, rig.store.Release(context.Background(), first))
	require.NoError(t, rig.acquire("p1", 1), "a released lease frees the slot")

	rig.clock.Advance(leaseTestTTL + time.Second)
	assert.NoError(t, rig.acquire("p1", 2), "a holder that stopped heartbeating frees the slot")
}

// TestFleetCapacity_TheHolderIsNotCountedAgainstItself: a heartbeat re-acquires
// the lease it already holds, and a count that included it would refuse the very
// holder sitting at the limit.
func TestFleetCapacity_TheHolderIsNotCountedAgainstItself(t *testing.T) {
	rig := newFleetLeaseRig(t, 1)
	rig.project(t, "p1", 0)

	lease := &JobLease{TenantID: "acme", ProjectID: "p1", Ref: "a", JobID: "job-a", Generation: 1}
	require.NoError(t, rig.store.Acquire(context.Background(), lease))
	assert.NoError(t, rig.store.Acquire(context.Background(), lease))
}

// TestFleetCapacity_BoundsAJobWithNoProject: the fleet's slots are held by
// whatever is running. An unscoped job occupies one exactly as a project's job
// does, and it is the one shape the project axis deliberately does not check.
func TestFleetCapacity_BoundsAJobWithNoProject(t *testing.T) {
	rig := newFleetLeaseRig(t, 1)

	require.NoError(t, rig.store.Acquire(context.Background(),
		&JobLease{TenantID: "acme", Ref: "a", JobID: "job-a", Generation: 1}))
	err := rig.store.Acquire(context.Background(),
		&JobLease{TenantID: "acme", Ref: "b", JobID: "job-b", Generation: 1})

	assert.ErrorIs(t, err, ErrFleetCapacityExceeded)
}

// TestFleetCapacity_CountsEveryTenant: the fleet is the deployment, not a tenant.
// A count scoped to the acquiring lease's tenant would let two tenants each fill
// the fleet.
func TestFleetCapacity_CountsEveryTenant(t *testing.T) {
	rig := newFleetLeaseRig(t, 1)

	require.NoError(t, rig.store.Acquire(context.Background(),
		&JobLease{TenantID: "acme", Ref: "a", JobID: "job-a", Generation: 1}))
	err := rig.store.Acquire(context.Background(),
		&JobLease{TenantID: "globex", Ref: "b", JobID: "job-b", Generation: 1})

	assert.ErrorIs(t, err, ErrFleetCapacityExceeded)
}

// TestFleetCapacity_AnUndeclaredCapacityAdmits: a deployment nobody measured has
// not said it has no slots, and refusing every job on an unmeasured fleet would
// be the outage this check exists to prevent, inverted.
func TestFleetCapacity_AnUndeclaredCapacityAdmits(t *testing.T) {
	rig := newFleetLeaseRig(t, 0)
	rig.project(t, "p1", 0)

	for i := 0; i < 5; i++ {
		require.NoError(t, rig.acquire("p1", i))
	}
}

// TestFleetCapacity_AnUnreadableCapacityRefuses is the other half of the same
// rule, and the distinction that matters: "undeclared" is an answer and admits,
// while "the reader FAILED" is not an answer and must not be read as one.
func TestFleetCapacity_AnUnreadableCapacityRefuses(t *testing.T) {
	inner, err := NewProcessJobLeaseStore(leaseTestTTL)
	require.NoError(t, err)
	broken := errors.New("registry down")
	store, err := NewFleetCapacityJobLeaseStore(inner, &erroringFleetCapacity{err: broken}, leaseTestTTL)
	require.NoError(t, err)

	err = store.Acquire(context.Background(),
		&JobLease{TenantID: "acme", Ref: "a", JobID: "job-a", Generation: 1})
	assert.ErrorIs(t, err, ErrJobLeaseBackend)
	assert.ErrorIs(t, err, broken)
}

// TestFleetCapacity_RefusesAnInvalidLeaseBeforeCounting: validation is the first
// gate, so a malformed lease is answered by its own fault rather than by a
// capacity reading it never needed.
func TestFleetCapacity_RefusesAnInvalidLeaseBeforeCounting(t *testing.T) {
	rig := newFleetLeaseRig(t, 1)

	err := rig.store.Acquire(context.Background(), &JobLease{TenantID: "acme", Ref: "", JobID: ""})
	assert.ErrorIs(t, err, ErrJobLeaseInvalidInput)
	assert.NotErrorIs(t, err, ErrFleetCapacityExceeded)
}

// TestNewFleetCapacityJobLeaseStore_RefusesMissingDependencies proves the wrapper
// cannot be built in a shape that admits everything while claiming to check.
func TestNewFleetCapacityJobLeaseStore_RefusesMissingDependencies(t *testing.T) {
	inner, err := NewProcessJobLeaseStore(leaseTestTTL)
	require.NoError(t, err)
	capacity, err := NewDeclaredFleetCapacity(4)
	require.NoError(t, err)

	cases := map[string]func() (JobLeaseStore, error){
		"no store":    func() (JobLeaseStore, error) { return NewFleetCapacityJobLeaseStore(nil, capacity, leaseTestTTL) },
		"no capacity": func() (JobLeaseStore, error) { return NewFleetCapacityJobLeaseStore(inner, nil, leaseTestTTL) },
		"no ttl":      func() (JobLeaseStore, error) { return NewFleetCapacityJobLeaseStore(inner, capacity, 0) },
		"negative ttl": func() (JobLeaseStore, error) {
			return NewFleetCapacityJobLeaseStore(inner, capacity, -time.Second)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := build()
			assert.ErrorIs(t, err, ErrJobLeaseInvalidInput)
		})
	}
}

// TestFleetCapacityExceededError_NamesTheFleetsNumber: the message an operator
// reads has to say which bound was in the way and where it came from, or they
// raise a project quota against a fleet that cannot serve it.
func TestFleetCapacityExceededError_NamesTheFleetsNumber(t *testing.T) {
	t.Parallel()

	err := &FleetCapacityExceededError{Live: 8, Limit: 8, Source: FleetCapacityFleet}
	assert.Contains(t, err.Error(), "live=8")
	assert.Contains(t, err.Error(), "limit=8")
	assert.Contains(t, err.Error(), "source=fleet")
	assert.ErrorIs(t, err, ErrFleetCapacityExceeded)
}
