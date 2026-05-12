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

//go:build desktop

package main

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// GUISessionIDRequiredError indicates a binding method was called without
// the required session ID argument.
type GUISessionIDRequiredError struct{}

// Error implements the error interface.
func (e *GUISessionIDRequiredError) Error() string {
	return "session ID is required"
}

// NewGUISessionIDRequiredError wraps GUISessionIDRequiredError in a QBError.
func NewGUISessionIDRequiredError(op string) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &GUISessionIDRequiredError{},
	}
}

// GUIAgentIDRequiredError indicates a binding method was called without
// the required agent ID argument.
type GUIAgentIDRequiredError struct{}

// Error implements the error interface.
func (e *GUIAgentIDRequiredError) Error() string {
	return "agent ID is required"
}

// NewGUIAgentIDRequiredError wraps GUIAgentIDRequiredError in a QBError.
func NewGUIAgentIDRequiredError(op string) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &GUIAgentIDRequiredError{},
	}
}

// GUIAgentNotFoundError indicates an agent lookup returned an empty record.
type GUIAgentNotFoundError struct {
	AgentID string
}

// Error implements the error interface.
func (e *GUIAgentNotFoundError) Error() string {
	return "agent not found: " + e.AgentID
}

// NewGUIAgentNotFoundError wraps GUIAgentNotFoundError in a QBError.
func NewGUIAgentNotFoundError(op, agentID string) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   op,
		Err:  &GUIAgentNotFoundError{AgentID: agentID},
	}
}

// GUIEmbeddedAlreadyRunningError indicates an embedded server was already
// started when Start was called again.
type GUIEmbeddedAlreadyRunningError struct{}

// Error implements the error interface.
func (e *GUIEmbeddedAlreadyRunningError) Error() string {
	return "embedded server already running"
}

// NewGUIEmbeddedAlreadyRunningError wraps the typed error in a QBError.
func NewGUIEmbeddedAlreadyRunningError(op string) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrConflict,
		Op:   op,
		Err:  &GUIEmbeddedAlreadyRunningError{},
	}
}

// GUIEmbeddedStartError indicates the embedded server failed to start.
type GUIEmbeddedStartError struct {
	Cause error
}

// Error implements the error interface.
func (e *GUIEmbeddedStartError) Error() string {
	if e.Cause != nil {
		return "embedded server start failed: " + e.Cause.Error()
	}
	return "embedded server start failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *GUIEmbeddedStartError) Unwrap() error {
	return e.Cause
}

// NewGUIEmbeddedStartError wraps GUIEmbeddedStartError in a QBError.
func NewGUIEmbeddedStartError(op string, cause error) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrInternal,
		Op:   op,
		Err:  &GUIEmbeddedStartError{Cause: cause},
	}
}

// GUIEmbeddedStopError indicates a failure during embedded server shutdown.
type GUIEmbeddedStopError struct {
	Cause error
}

// Error implements the error interface.
func (e *GUIEmbeddedStopError) Error() string {
	if e.Cause != nil {
		return "embedded server stop failed: " + e.Cause.Error()
	}
	return "embedded server stop failed"
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *GUIEmbeddedStopError) Unwrap() error {
	return e.Cause
}

// NewGUIEmbeddedStopError wraps GUIEmbeddedStopError in a QBError.
func NewGUIEmbeddedStopError(op string, cause error) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrInternal,
		Op:   op,
		Err:  &GUIEmbeddedStopError{Cause: cause},
	}
}

// GUIRequestRequiredError indicates a binding method was called without
// the required SDK request struct. Wails dispatches JSON-encoded method
// arguments; when the frontend sends the wrong shape (e.g. a bare string
// where an object is expected), Wails fails to unmarshal and the request
// pointer arrives nil. This error lets the binding reject that case with
// a typed, operator-scoped value rather than panicking with a nil-pointer
// dereference.
type GUIRequestRequiredError struct{}

// Error implements the error interface.
func (e *GUIRequestRequiredError) Error() string {
	return "request is required"
}

// NewGUIRequestRequiredError wraps GUIRequestRequiredError in a QBError.
func NewGUIRequestRequiredError(op string) *errors.QBError {
	return &errors.QBError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &GUIRequestRequiredError{},
	}
}
