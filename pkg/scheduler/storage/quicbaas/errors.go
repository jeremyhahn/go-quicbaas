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

package quicbaas

import (
	"errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// Sentinel errors for storage operations.
var (
	// ErrNilJob indicates a nil job was passed to a storage operation.
	ErrNilJob = errors.New("job cannot be nil")

	// ErrNilExecution indicates a nil execution was passed to a storage operation.
	ErrNilExecution = errors.New("execution cannot be nil")

	// ErrLimitReached is an internal sentinel used to stop iteration.
	ErrLimitReached = errors.New("limit reached")

	// ErrInvalidCursor indicates a ListJobs cursor that is not a job ID.
	ErrInvalidCursor = errors.New("cursor is not a job ID")

	// ErrNilClaim indicates a nil firing claim was passed to ClaimFiring.
	ErrNilClaim = errors.New("firing claim cannot be nil")

	// ErrNodeNeverLed indicates the test qrdb node never won its election.
	ErrNodeNeverLed = errors.New("qrdb test node never became leader")

	// ErrUnreadableClaimMark indicates a job's claim mark exists but its
	// value could not be read back — either it reads as nothing, or it decodes
	// to a mark that does not say where the job stands. The claim fails
	// closed: without knowing where the mark stands there is no basis for
	// claiming past it.
	//
	// It IS cas.ErrUnreadable rather than a second name for it. The guarded
	// write raises that cause for the empty-value case and this package raises
	// it for the meaningless-value case; two names for one condition is the
	// drift the shared primitive exists to remove, and a caller matching on
	// either would otherwise miss half the cases.
	ErrUnreadableClaimMark = cas.ErrUnreadable

	// ErrUnreadableJobIDCounter indicates the job ID counter record exists
	// but reads as nothing. Allocation fails closed: an ID allocated past a
	// counter that cannot be read is an ID allocated from a guess, and a
	// guess can land on a live job.
	ErrUnreadableJobIDCounter = errors.New("job ID counter exists but could not be read")

	// ErrJobIDSpaceExhausted indicates the allocatable job ID range — every
	// ID below scheduler.SystemJobIDFloor — has been used up. Allocating past
	// it would hand out an ID reserved for a system job.
	ErrJobIDSpaceExhausted = errors.New("job ID space below the system job floor is exhausted")
)
