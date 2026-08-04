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

package main

// This file declares the shared typed errors used by the quicbaas CLI command
// surfaces so that no format-string errors escape the package. Callers
// distinguish failure modes with errors.Is (sentinel match) or errors.As
// (typed match).

// SentinelDetailError wraps a package sentinel error with a contextual detail
// string while preserving errors.Is(err, Sentinel). It is the typed
// replacement for the format-string pattern that wrapped a sentinel with extra
// context.
type SentinelDetailError struct {
	// Sentinel is the wrapped package sentinel (e.g. ErrSSHInvalidKeyType).
	Sentinel error
	// Detail is the human-readable context appended after the sentinel text.
	Detail string
}

// Error implements the error interface.
func (e *SentinelDetailError) Error() string {
	if e.Detail == "" {
		return e.Sentinel.Error()
	}
	return e.Sentinel.Error() + ": " + e.Detail
}

// Unwrap returns the wrapped sentinel for errors.Is support.
func (e *SentinelDetailError) Unwrap() error {
	return e.Sentinel
}

// OpError wraps an underlying error with the failing operation's context. It is
// the typed replacement for the format-string "op: %w" pattern and preserves
// errors.Is/As traversal against the wrapped cause.
type OpError struct {
	// Op names the failing operation, e.g. "agent deploy: build CSR".
	Op string
	// Cause is the wrapped underlying error.
	Cause error
}

// Error implements the error interface.
func (e *OpError) Error() string {
	return e.Op + ": " + e.Cause.Error()
}

// Unwrap returns the wrapped cause for errors.Is/As support.
func (e *OpError) Unwrap() error {
	return e.Cause
}

// MessageError is a simple typed error carrying a fully-formed message with no
// wrapped cause. It is the typed replacement for a bare format-string error.
type MessageError struct {
	// Message is the complete human-readable error text.
	Message string
}

// Error implements the error interface.
func (e *MessageError) Error() string {
	return e.Message
}
