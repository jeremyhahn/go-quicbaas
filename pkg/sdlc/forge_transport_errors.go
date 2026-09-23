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
	"errors"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Forge transport errors
// =============================================================================

var (
	// ErrForgeUnanswered indicates the forge did not answer a call: no
	// response inside the call's deadline, a dropped connection, or a gateway
	// reporting the forge behind it unavailable.
	ErrForgeUnanswered = errors.New("sdlc: the forge did not answer")

	// ErrForgeRetriesExhausted indicates a call that is safe to repeat went
	// unanswered on every attempt its retry bound allows.
	ErrForgeRetriesExhausted = errors.New("sdlc: the forge did not answer within the retry bound")

	// ErrForgeWriteUnconfirmed indicates a write the forge did not answer and
	// that is not safe to repeat, so whether it took effect is unknown.
	ErrForgeWriteUnconfirmed = errors.New("sdlc: the forge did not confirm a write that is not safe to repeat")
)

// ForgeUnansweredError reports one forge call that got no usable answer.
//
// IT IS THE FORGE'S HOST, NOT THE WORK, and it says so to the engine: it
// unwraps to orchestration.ErrForgeUnreachable, which is how a job that ends
// on it is attributed to the environment and refunded rather than charged.
// Every error built on top of one — a retry bound spent, a write that could
// not be confirmed — keeps it in its chain for the same reason.
type ForgeUnansweredError struct {
	Type      ForgeType
	Operation string

	// Cause is what the transport reported: a deadline, a reset, or the
	// gateway's own status as a GiteaAPIError.
	Cause error
}

// Error implements the error interface.
func (e *ForgeUnansweredError) Error() string {
	msg := "sdlc: " + string(e.Type) + " did not answer " + e.Operation
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinels and the cause for errors.Is/As support.
func (e *ForgeUnansweredError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrForgeUnanswered, ErrBackend, orchestration.ErrForgeUnreachable}
	}
	return []error{ErrForgeUnanswered, ErrBackend, orchestration.ErrForgeUnreachable, e.Cause}
}

// ForgeRetriesExhaustedError reports a repeatable call that went unanswered
// on every attempt.
type ForgeRetriesExhaustedError struct {
	Type      ForgeType
	Operation string
	Attempts  int

	// Last is the final attempt's error.
	Last error
}

// Error implements the error interface.
func (e *ForgeRetriesExhaustedError) Error() string {
	msg := "sdlc: " + string(e.Type) + " did not answer " + e.Operation + " in " +
		strconv.Itoa(e.Attempts) + " attempt(s)"
	if e.Last != nil {
		msg += ": " + e.Last.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the last attempt's error for errors.Is/As
// support.
func (e *ForgeRetriesExhaustedError) Unwrap() []error {
	if e.Last == nil {
		return []error{ErrForgeRetriesExhausted}
	}
	return []error{ErrForgeRetriesExhausted, e.Last}
}

// ForgeWriteUnconfirmedError reports a write the forge did not answer and
// that the adapter will not repeat, because repeating it could apply it
// twice.
//
// It is the honest answer where no read can settle the question: the write
// may have landed. A caller that can settle it by its own read does so; the
// engine attributes it to the environment either way, through Cause.
type ForgeWriteUnconfirmedError struct {
	Type      ForgeType
	Operation string
	Subject   string

	// Observed is what a reconciling read found, empty when no read ran.
	Observed string

	// Cause is the unanswered call.
	Cause error
}

// Error implements the error interface.
func (e *ForgeWriteUnconfirmedError) Error() string {
	msg := "sdlc: " + string(e.Type) + " did not confirm " + e.Operation + " on " + e.Subject +
		"; it is not repeated because it is not safe to apply twice"
	if e.Observed != "" {
		msg += " (observed " + e.Observed + ")"
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *ForgeWriteUnconfirmedError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrForgeWriteUnconfirmed}
	}
	return []error{ErrForgeWriteUnconfirmed, e.Cause}
}
