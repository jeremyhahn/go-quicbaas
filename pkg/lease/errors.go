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

package lease

import (
	stderrors "errors"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Lease validation errors — the vocabulary of a REFUSAL.
//
// Manager.Validate is the server-side enforcement point: a caller holding a
// lease ID asks whether the grant behind it is still good, and the answer has
// to distinguish the three ways it can fail. They are different facts about
// the world and they lead to different caller behaviour:
//
//   - NOT FOUND    — the grant never existed, or its tombstone has aged out.
//   - REVOKED      — somebody took it away. Never retryable; a new grant must
//                    be issued by whoever is entitled to issue one.
//   - EXPIRED      — it ran out of time. A renewal was possible until it did.
//
// Each is wrapped in a *QBError carrying the code every transport already maps,
// so a refusal reaches a client as the right status rather than as a generic
// internal error. The inner type is what a Go caller matches on with errors.As.
// =============================================================================

// LeaseNotFoundError reports that no lease is stored under the given ID.
type LeaseNotFoundError struct {
	// LeaseID is the identifier that resolved to nothing.
	LeaseID string
}

// Error implements the error interface.
func (e *LeaseNotFoundError) Error() string {
	return "lease not found: " + e.LeaseID
}

// NewLeaseNotFoundError builds the coded error for an unknown lease.
func NewLeaseNotFoundError(op, leaseID string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   op,
		Err:  &LeaseNotFoundError{LeaseID: leaseID},
	}
}

// LeaseRevokedError reports that a lease was revoked and may never be used
// again.
//
// A REVOKED LEASE IS A TOMBSTONE, NOT AN ABSENCE. Revocation deliberately
// keeps the record so that this answer can be given: "this grant was taken
// away at 14:02" is an auditable fact, while "no such lease" is indistinguish-
// able from a typo. The tombstone is purged only after the retention window.
type LeaseRevokedError struct {
	// LeaseID is the revoked lease.
	LeaseID string

	// RevokedAt is when it was revoked.
	RevokedAt time.Time
}

// Error implements the error interface.
func (e *LeaseRevokedError) Error() string {
	return "lease " + e.LeaseID + " was revoked at " + e.RevokedAt.UTC().Format(time.RFC3339)
}

// NewLeaseRevokedError builds the coded error for a revoked lease.
func NewLeaseRevokedError(op, leaseID string, revokedAt time.Time) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotAllowed,
		Op:   op,
		Err:  &LeaseRevokedError{LeaseID: leaseID, RevokedAt: revokedAt},
	}
}

// LeaseExpiredError reports that a lease's TTL elapsed.
//
// IT IS ANSWERED FROM THE RECORD, NOT FROM THE SWEEP. The expiration loop is
// a reaper, not the authority: a lease that ran out one millisecond ago is
// refused here even though no pass has visited it yet. Enforcement that waited
// for the sweep would make the TTL advisory.
type LeaseExpiredError struct {
	// LeaseID is the expired lease.
	LeaseID string

	// ExpiredAt is when its TTL elapsed.
	ExpiredAt time.Time
}

// Error implements the error interface.
func (e *LeaseExpiredError) Error() string {
	return "lease " + e.LeaseID + " expired at " + e.ExpiredAt.UTC().Format(time.RFC3339)
}

// NewLeaseExpiredError builds the coded error for an expired lease.
func NewLeaseExpiredError(op, leaseID string, expiredAt time.Time) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotAllowed,
		Op:   op,
		Err:  &LeaseExpiredError{LeaseID: leaseID, ExpiredAt: expiredAt},
	}
}

// LeaseCorruptError reports a stored lease record that could not be decoded.
//
// It is INTERNAL rather than not-found on purpose: a record that exists and
// cannot be read is a storage fault an operator must see, and reporting it as
// an absence would let a corrupted grant be silently re-issued.
type LeaseCorruptError struct {
	// LeaseID is the undecodable lease.
	LeaseID string

	// Cause is the decode failure.
	Cause error
}

// Error implements the error interface.
func (e *LeaseCorruptError) Error() string {
	return "lease " + e.LeaseID + " could not be decoded: " + e.Cause.Error()
}

// Unwrap exposes the decode failure.
func (e *LeaseCorruptError) Unwrap() error { return e.Cause }

// NewLeaseCorruptError builds the coded error for an undecodable lease record.
func NewLeaseCorruptError(op, leaseID string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &LeaseCorruptError{LeaseID: leaseID, Cause: cause},
	}
}

// LeaseStoreError reports that the backing store refused an operation.
//
// Distinct from every error above: those are answers ABOUT a lease, this is
// the store failing to answer at all. A caller that treats an outage as a
// refusal would revoke access on a transient fault; one that treats a refusal
// as an outage would retry forever.
type LeaseStoreError struct {
	// Operation names what was attempted ("get", "put", "delete", "scan").
	Operation string

	// LeaseID is the lease involved, empty for whole-keyspace operations.
	LeaseID string

	// Cause is the store's own error.
	Cause error
}

// Error implements the error interface.
func (e *LeaseStoreError) Error() string {
	msg := "lease store " + e.Operation + " failed"
	if e.LeaseID != "" {
		msg += " for " + e.LeaseID
	}
	return msg + ": " + e.Cause.Error()
}

// Unwrap exposes the store's own error.
func (e *LeaseStoreError) Unwrap() error { return e.Cause }

// NewLeaseStoreError builds the coded error for a failed store operation.
func NewLeaseStoreError(op, operation, leaseID string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err:  &LeaseStoreError{Operation: operation, LeaseID: leaseID, Cause: cause},
	}
}

// IsLeaseRevoked reports whether err is (or wraps) a revoked-lease refusal.
func IsLeaseRevoked(err error) bool {
	var revoked *LeaseRevokedError
	return stderrors.As(err, &revoked)
}

// IsLeaseExpired reports whether err is (or wraps) an expired-lease refusal.
func IsLeaseExpired(err error) bool {
	var expired *LeaseExpiredError
	return stderrors.As(err, &expired)
}

// IsLeaseNotFound reports whether err is (or wraps) an unknown-lease refusal.
func IsLeaseNotFound(err error) bool {
	var missing *LeaseNotFoundError
	return stderrors.As(err, &missing)
}
