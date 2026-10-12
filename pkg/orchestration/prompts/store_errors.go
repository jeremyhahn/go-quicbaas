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

package prompts

import (
	"errors"
	"strconv"
)

// =============================================================================
// Store, resolver and seeder sentinels
// =============================================================================

var (
	// ErrInvalidInput indicates a caller-supplied value the store refused.
	ErrInvalidInput = errors.New("prompts: invalid input")

	// ErrNotFound indicates no record at the addressed scope.
	ErrNotFound = errors.New("prompts: record not found")

	// ErrVersionConflict indicates a write against a version the record no
	// longer carries.
	ErrVersionConflict = errors.New("prompts: version conflict")

	// ErrUnknownRevision indicates a rollback to a version the record's
	// history does not hold.
	ErrUnknownRevision = errors.New("prompts: unknown revision")

	// ErrCollision indicates a record slot already held by another prompt.
	ErrCollision = errors.New("prompts: record slot collision")

	// ErrBackend indicates the tenant's store could not be reached or failed.
	ErrBackend = errors.New("prompts: backend failure")

	// ErrGuardedWriteUnsupported indicates a tenant store that cannot decide
	// a contested write. Every write is refused over such a store rather than
	// emulated with a read and an unguarded put; it is carried inside a
	// BackendError.
	ErrGuardedWriteUnsupported = errors.New("prompts: store cannot perform guarded writes")

	// ErrResolve indicates a set that could not be resolved for a job.
	ErrResolve = errors.New("prompts: resolve failed")

	// ErrSeed indicates a tenant whose prompt set could not be seeded.
	ErrSeed = errors.New("prompts: seed failed")
)

// =============================================================================
// Typed errors
// =============================================================================

// InvalidInputError reports a caller-supplied value that was refused.
type InvalidInputError struct {
	Field   string
	Value   string
	Message string
}

// Error implements the error interface.
func (e *InvalidInputError) Error() string {
	return ErrInvalidInput.Error() + ": " + e.Field + " " + strconv.Quote(e.Value) + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidInputError) Unwrap() error { return ErrInvalidInput }

// NotFoundError reports no record for a prompt at one scope. It is the
// ordinary answer for a prompt nobody overrode there, not a fault.
type NotFoundError struct {
	Ref Ref
}

// Error implements the error interface.
func (e *NotFoundError) Error() string {
	return ErrNotFound.Error() + ": " + e.Ref.String()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NotFoundError) Unwrap() error { return ErrNotFound }

// VersionConflictError reports a write made against a version somebody else
// already replaced. Nothing was written.
type VersionConflictError struct {
	Ref      Ref
	Expected int64
}

// Error implements the error interface.
func (e *VersionConflictError) Error() string {
	return ErrVersionConflict.Error() + ": " + e.Ref.String() +
		" changed underneath this write (expected version " + strconv.FormatInt(e.Expected, 10) + ")"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *VersionConflictError) Unwrap() error { return ErrVersionConflict }

// UnknownRevisionError reports a rollback to a version the record's bounded
// history does not hold: never written, or old enough to have been dropped.
type UnknownRevisionError struct {
	Ref     Ref
	Version int64
}

// Error implements the error interface.
func (e *UnknownRevisionError) Error() string {
	return ErrUnknownRevision.Error() + ": " + e.Ref.String() + " has no revision " +
		strconv.FormatInt(e.Version, 10)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownRevisionError) Unwrap() error { return ErrUnknownRevision }

// CollisionError reports a record slot that already holds another prompt.
// Slots are addressed by a 64-bit digest of the prompt's identity, so two
// identities can in principle share one; the write is refused rather than
// replacing the other prompt.
type CollisionError struct {
	Ref  Ref
	Held Ref
}

// Error implements the error interface.
func (e *CollisionError) Error() string {
	return ErrCollision.Error() + ": " + e.Ref.String() + " addresses the slot held by " + e.Held.String()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *CollisionError) Unwrap() error { return ErrCollision }

// BackendError reports a store failure: the tenant's barrier sealed, the
// resolver refused, the key-value store failed.
type BackendError struct {
	Op    string
	Cause error
}

// Error implements the error interface.
func (e *BackendError) Error() string {
	return ErrBackend.Error() + ": " + e.Op + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *BackendError) Unwrap() []error { return []error{ErrBackend, e.Cause} }

func backendError(op string, cause error) error {
	return &BackendError{Op: op, Cause: cause}
}

// ResolveError reports a set that could not be resolved for a job. A job
// that cannot learn its instructions is refused rather than launched on
// instructions nobody chose.
type ResolveError struct {
	TenantID  string
	ProjectID string
	Set       SetKind
	Prompt    string
	Cause     error
}

// Error implements the error interface.
func (e *ResolveError) Error() string {
	message := ErrResolve.Error() + ": " + subject(e.Set, e.Prompt) + " for tenant " + e.TenantID
	if e.ProjectID != "" {
		message += " project " + e.ProjectID
	}
	return message + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *ResolveError) Unwrap() []error { return []error{ErrResolve, e.Cause} }

// SeedError reports a tenant whose prompt set could not be seeded or
// reconciled.
type SeedError struct {
	TenantID string
	Set      SetKind
	Prompt   string
	Cause    error
}

// Error implements the error interface.
func (e *SeedError) Error() string {
	return ErrSeed.Error() + ": " + subject(e.Set, e.Prompt) + " for tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *SeedError) Unwrap() []error { return []error{ErrSeed, e.Cause} }
