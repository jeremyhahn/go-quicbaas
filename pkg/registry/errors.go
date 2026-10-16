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

package registry

import "strconv"

// RegistryError wraps a backend failure during a registry operation.
type RegistryError struct {
	// Op is the registry operation that failed (grant, put, range, ...).
	Op string

	// Err is the underlying backend error.
	Err error
}

// Error returns a human-readable description.
func (e *RegistryError) Error() string {
	return "registry: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *RegistryError) Unwrap() error {
	return e.Err
}

// InvalidInstanceError is returned when a ServiceInstance or a request is
// missing required fields or carries a malformed one.
type InvalidInstanceError struct {
	// Reason describes what is invalid.
	Reason string
}

// Error returns a human-readable description.
func (e *InvalidInstanceError) Error() string {
	return "registry: invalid service instance: " + e.Reason
}

// RegistryRequiresRaftError reports a qrdb node that does not serve the
// lease, MVCC and watch planes service discovery is built on. go-qrdb serves
// them only in its raft run mode; a standalone node gates them off.
type RegistryRequiresRaftError struct {
	// Mode is the run mode the node reports, e.g. "standalone".
	Mode string
}

// Error implements error.
func (e *RegistryRequiresRaftError) Error() string {
	return "registry: service discovery requires a raft qrdb node, but the node runs in " +
		strconv.Quote(e.Mode) + " mode"
}

// LeaseNotFoundError is returned by Renew and Deregister when the lease does
// not back a registration of the caller's tenant: it never existed, it has
// already expired or been revoked, or it belongs to another tenant. The three
// cases are deliberately indistinguishable, so a caller cannot probe for
// another tenant's lease IDs.
type LeaseNotFoundError struct {
	// LeaseID is the lease the caller named.
	LeaseID int64
}

// Error returns a human-readable description.
func (e *LeaseNotFoundError) Error() string {
	return "registry: lease " + strconv.FormatInt(e.LeaseID, 10) + " does not back a registration of this tenant"
}

// WatchCompactedError ends a watch whose start revision is older than the
// store's retained history, so the events in between can no longer be
// replayed. The caller resynchronises with Resolve and watches again from
// the revision Resolve returns.
type WatchCompactedError struct {
	// Service is the watched service.
	Service string

	// FromRevision is the revision the watch asked to start from.
	FromRevision int64

	// Err is the underlying store error.
	Err error
}

// Error returns a human-readable description.
func (e *WatchCompactedError) Error() string {
	return "registry: watch of " + e.Service + " from revision " +
		strconv.FormatInt(e.FromRevision, 10) + " is older than the retained history; resolve again and watch from its revision"
}

// Unwrap returns the underlying store error.
func (e *WatchCompactedError) Unwrap() error {
	return e.Err
}

// WatchEndedError ends a watch the store cancelled for a reason other than
// compaction (for example a watcher evicted for falling behind). Events may
// have been lost, so the caller resynchronises with Resolve.
type WatchEndedError struct {
	// Service is the watched service.
	Service string

	// Err is the underlying store error.
	Err error
}

// Error returns a human-readable description.
func (e *WatchEndedError) Error() string {
	return "registry: watch of " + e.Service + " ended by the store: " + e.Err.Error()
}

// Unwrap returns the underlying store error.
func (e *WatchEndedError) Unwrap() error {
	return e.Err
}
