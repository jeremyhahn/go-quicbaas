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

package placement

import (
	"sort"
	"strconv"
	"strings"
)

// InvalidRequestError reports a request the engine refuses before touching
// the store: a missing identifier, a malformed constraint, a negative
// resource amount.
type InvalidRequestError struct {
	// Reason describes what is invalid.
	Reason string
}

// Error implements error.
func (e *InvalidRequestError) Error() string {
	return "placement: invalid request: " + e.Reason
}

// NodeNotFoundError reports a node that is not registered, or whose lease
// has expired.
type NodeNotFoundError struct {
	// NodeID is the node that was looked up.
	NodeID string
}

// Error implements error.
func (e *NodeNotFoundError) Error() string {
	return "placement: node not found: " + e.NodeID
}

// PlacementNotFoundError reports a workload that has no placement.
type PlacementNotFoundError struct {
	// WorkloadID is the workload that was looked up.
	WorkloadID string
}

// Error implements error.
func (e *PlacementNotFoundError) Error() string {
	return "placement: no placement for workload: " + e.WorkloadID
}

// PlacementConflictError reports a Place call for a workload that is already
// placed on a node it must leave — the node's lease expired, or the node is
// draining. Place never moves a workload implicitly: the caller stops it,
// calls Release, and places it again.
type PlacementConflictError struct {
	// WorkloadID is the already-placed workload.
	WorkloadID string

	// NodeID is the node it is placed on.
	NodeID string

	// Reason says why the existing placement cannot be returned.
	Reason string
}

// Error implements error.
func (e *PlacementConflictError) Error() string {
	return "placement: workload " + e.WorkloadID + " is already placed on node " + e.NodeID +
		" (" + e.Reason + "); release it before placing it again"
}

// NoFeasibleNodeError reports that no node passed every filter. Rejections
// maps each considered node to the first filter it failed, so the caller can
// see why nothing fits without re-deriving the decision.
type NoFeasibleNodeError struct {
	// WorkloadID is the workload that could not be placed.
	WorkloadID string

	// Rejections maps a node ID to the reason it was filtered out.
	Rejections map[string]string
}

// Error implements error.
func (e *NoFeasibleNodeError) Error() string {
	if len(e.Rejections) == 0 {
		return "placement: no feasible node for workload " + e.WorkloadID + ": no live nodes are registered"
	}
	ids := make([]string, 0, len(e.Rejections))
	for id := range e.Rejections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id+": "+e.Rejections[id])
	}
	return "placement: no feasible node for workload " + e.WorkloadID + " (" + strings.Join(parts, "; ") + ")"
}

// ContendedError reports a placement or release that lost its guarded write
// on every one of the engine's bounded attempts. Nothing was committed.
type ContendedError struct {
	// Op is the operation that gave up.
	Op string

	// Attempts is how many guarded writes were tried.
	Attempts int
}

// Error implements error.
func (e *ContendedError) Error() string {
	return "placement: " + e.Op + " abandoned after " + strconv.Itoa(e.Attempts) + " contended attempts"
}

// StoreError wraps a go-qrdb failure during a placement operation.
type StoreError struct {
	// Op is the store operation that failed.
	Op string

	// Err is the underlying error.
	Err error
}

// Error implements error.
func (e *StoreError) Error() string {
	return "placement: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *StoreError) Unwrap() error {
	return e.Err
}

// CorruptRecordError reports a stored record that does not decode. The
// engine fails closed on it rather than skipping it: a skipped allocation
// record would let a node be overcommitted.
type CorruptRecordError struct {
	// Key is the record's key.
	Key string

	// Err is the decode error.
	Err error
}

// Error implements error.
func (e *CorruptRecordError) Error() string {
	return "placement: corrupt record " + e.Key + ": " + e.Err.Error()
}

// Unwrap returns the decode error.
func (e *CorruptRecordError) Unwrap() error {
	return e.Err
}
