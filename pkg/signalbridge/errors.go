// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"errors"
	"fmt"
)

// Error is the canonical typed error for the signalbridge package.
// All public APIs return an *Error (or a wrapped *Error) — no fmt.Errorf
// strings escape from the package.
type Error struct {
	// Op is the failing operation, e.g. "signalbridge.SignalClient.Send".
	Op string
	// Kind is a stable machine-readable discriminator used by callers who
	// switch on error type (for retry policy, exit codes, user-facing
	// messages). See the ErrKind* constants.
	Kind ErrKind
	// Msg is a human-readable description; concatenated with Kind in
	// Error().
	Msg string
	// Cause is the wrapped underlying error, or nil.
	Cause error
}

// Error implements error.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		if e.Msg != "" {
			return fmt.Sprintf("%s: %s (%s): %v", e.Op, e.Msg, e.Kind, e.Cause)
		}
		return fmt.Sprintf("%s (%s): %v", e.Op, e.Kind, e.Cause)
	}
	if e.Msg != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Op, e.Msg, e.Kind)
	}
	return fmt.Sprintf("%s (%s)", e.Op, e.Kind)
}

// Unwrap enables errors.Is/As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ErrKind discriminates typed errors. Callers switch on the kind when
// deciding retry policy, user-visible mapping, or exit status.
type ErrKind string

const (
	// ErrKindInvalidInput indicates a programmer-visible input validation
	// failure (nil pointers, empty required fields, malformed grammar).
	ErrKindInvalidInput ErrKind = "invalid_input"

	// ErrKindNotFound indicates the referenced resource does not exist.
	ErrKindNotFound ErrKind = "not_found"

	// ErrKindConflict indicates a concurrent modification or a conflicting
	// state (e.g. attach when already attached).
	ErrKindConflict ErrKind = "conflict"

	// ErrKindUnauthorized indicates the caller lacks the required
	// credentials or the auth policy denies the action.
	ErrKindUnauthorized ErrKind = "unauthorized"

	// ErrKindVerificationRequired is returned by Register when the
	// go-signal server demands interactive phone verification.
	ErrKindVerificationRequired ErrKind = "verification_required"

	// ErrKindTransport indicates an HTTP/WebSocket transport failure
	// against the go-signal server.
	ErrKindTransport ErrKind = "transport"

	// ErrKindProtocol indicates a Signal Protocol-level failure: untrusted
	// identity, decrypt failure, session corruption.
	ErrKindProtocol ErrKind = "protocol"

	// ErrKindRateLimited indicates the server returned HTTP 429 or the
	// per-conversation rate limiter refused the send.
	ErrKindRateLimited ErrKind = "rate_limited"

	// ErrKindPreKeyDepleted indicates the bot's pre-key inventory is too
	// low to accept new incoming sessions; rotation is required.
	ErrKindPreKeyDepleted ErrKind = "prekey_depleted"

	// ErrKindStorage indicates a failure in the local state store.
	ErrKindStorage ErrKind = "storage"

	// ErrKindParse indicates an intent parser failure on operator input.
	ErrKindParse ErrKind = "parse"

	// ErrKindInternal indicates an unexpected invariant violation inside
	// the package — a bug.
	ErrKindInternal ErrKind = "internal"
)

// newError constructs an *Error with the given op, kind, and message.
func newError(op string, kind ErrKind, msg string) *Error {
	return &Error{Op: op, Kind: kind, Msg: msg}
}

// wrapError wraps an underlying error with an op and kind.
func wrapError(op string, kind ErrKind, cause error, msg string) *Error {
	return &Error{Op: op, Kind: kind, Msg: msg, Cause: cause}
}

// IsKind reports whether err is an *Error with the given kind. Unlike a
// bare errors.Is check, IsKind matches any Error in the chain.
func IsKind(err error, kind ErrKind) bool {
	for err != nil {
		var e *Error
		if !errors.As(err, &e) {
			return false
		}
		if e.Kind == kind {
			return true
		}
		err = e.Cause
	}
	return false
}
