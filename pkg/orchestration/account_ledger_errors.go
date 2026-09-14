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

import "errors"

// =============================================================================
// Ledger errors
// =============================================================================
//
// Money and quota code is where a fallback is most tempting and most damaging.
// Every failure below is therefore a REFUSAL that names what could not be done,
// and none of them has a numeric form: a read that fails returns an error and
// no report, never a report reading zero. A caller that cannot tell "this
// account has spent nothing" from "I could not find out what this account has
// spent" will eventually treat the second as the first, and the first evidence
// of that is the invoice.

// Sentinel errors for the consumption ledger.
var (
	// ErrConsumptionInvalid indicates an entry or scope that cannot be stored
	// or queried as given.
	ErrConsumptionInvalid = errors.New("ledger: invalid consumption record")

	// ErrConsumptionConflict indicates one idempotency key presented with two
	// different records.
	ErrConsumptionConflict = errors.New("ledger: conflicting record for entry id")

	// ErrConsumptionWrite indicates an entry could not be durably stored.
	ErrConsumptionWrite = errors.New("ledger: consumption write failed")

	// ErrConsumptionRead indicates consumption could not be read back. It is
	// never converted into a zero total; see the note above.
	ErrConsumptionRead = errors.New("ledger: consumption read failed")

	// ErrConsumptionUnguarded indicates a backing store that cannot decide a
	// contested write, and therefore cannot promise exactly-once accounting.
	ErrConsumptionUnguarded = errors.New("ledger: store cannot decide a contested record")
)

// ConsumptionInvalidError reports an entry or scope this ledger refuses.
type ConsumptionInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ConsumptionInvalidError) Error() string {
	return ErrConsumptionInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ConsumptionInvalidError) Unwrap() error { return ErrConsumptionInvalid }

// ConsumptionConflictError reports two different records presented under one
// idempotency key.
//
// It carries the plane the collision was detected on because the two planes
// fail differently: a chain-plane conflict means the attribution changed, a
// window-plane conflict means the account or the moment did, and an operator
// chasing a double charge needs to know which.
type ConsumptionConflictError struct {
	EntryID string
	Plane   string
	Key     string
}

// Error implements the error interface.
func (e *ConsumptionConflictError) Error() string {
	return ErrConsumptionConflict.Error() + ": entry " + e.EntryID +
		" on the " + e.Plane + " plane already holds a different record at " + e.Key
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ConsumptionConflictError) Unwrap() error { return ErrConsumptionConflict }

// ConsumptionWriteError reports a failed durable write, naming the plane so a
// partial record is diagnosable rather than merely known to be partial.
type ConsumptionWriteError struct {
	EntryID string
	Plane   string
	Key     string
	Cause   error
}

// Error implements the error interface.
func (e *ConsumptionWriteError) Error() string {
	base := ErrConsumptionWrite.Error() + ": entry " + e.EntryID +
		" on the " + e.Plane + " plane at " + e.Key
	if e.Cause != nil {
		return base + ": " + e.Cause.Error()
	}
	return base
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ConsumptionWriteError) Unwrap() error { return ErrConsumptionWrite }

// ConsumptionReadError reports consumption that could not be read or decoded.
//
// A decode failure is included deliberately. cost_tracker.go's per-agent scan
// skips a corrupted counter with a warning, which is defensible for an advisory
// budget; here it would produce a total that is wrong and presented as right.
// One unreadable record fails the whole roll-up.
type ConsumptionReadError struct {
	Key     string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ConsumptionReadError) Error() string {
	base := ErrConsumptionRead.Error() + ": " + e.Key
	if e.Message != "" {
		base += ": " + e.Message
	}
	if e.Cause != nil {
		return base + ": " + e.Cause.Error()
	}
	return base
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ConsumptionReadError) Unwrap() error { return ErrConsumptionRead }

// ConsumptionUnguardedError reports a store that cannot decide a contested
// record.
//
// NO EMULATION, for the same reason CreateTask refuses one: exists-then-put is
// precisely the race exactly-once accounting exists to remove, and a ledger
// that emulated it would be a double charge waiting for two hosts to record one
// turn at the same moment.
type ConsumptionUnguardedError struct {
	Message string
}

// Error implements the error interface.
func (e *ConsumptionUnguardedError) Error() string {
	return ErrConsumptionUnguarded.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ConsumptionUnguardedError) Unwrap() error { return ErrConsumptionUnguarded }
