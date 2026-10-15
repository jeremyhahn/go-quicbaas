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

// This file declares the shared typed errors used by the quicbaasd init and
// canonical command surfaces so that no format-string errors escape the
// package. Callers distinguish failure modes with errors.Is (sentinel match)
// or errors.As (typed match).

// SentinelDetailError wraps a package sentinel error with a contextual detail
// string (e.g. an offending path or value) while preserving
// errors.Is(err, Sentinel). It is the typed replacement for
// the format-string pattern that wrapped a sentinel with extra context.
type SentinelDetailError struct {
	// Sentinel is the wrapped package sentinel (e.g. ErrOfficerFileRead).
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
	// Op names the failing operation, e.g. "init: start server".
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

// RaftHostnameError is a startup configuration error: --raft-port was given
// without an advertised Raft host, and this machine's hostname could not be
// resolved to supply one. Advertising a placeholder such as localhost would
// leave the node unreachable by its peers, so the boot is refused instead.
type RaftHostnameError struct {
	// Cause is the hostname lookup failure, or nil when it returned an empty
	// name.
	Cause error
}

// Error implements the error interface.
func (e *RaftHostnameError) Error() string {
	msg := "cannot derive the advertised Raft address for --raft-port: hostname lookup failed"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	} else {
		msg += ": empty hostname"
	}
	return msg + "; set --raft-address (or raft.raft_address) to the host:port peers dial"
}

// Unwrap returns the hostname lookup failure.
func (e *RaftHostnameError) Unwrap() error { return e.Cause }

// RaftAddressFlagError is a startup configuration error: --raft-port must move
// the port of a configured Raft address, and that address is not host:port.
type RaftAddressFlagError struct {
	// Key is the configuration key holding the address, e.g.
	// "raft.listen_address".
	Key string
	// Address is the configured value that could not be parsed.
	Address string
	// Cause is the parse failure.
	Cause error
}

// Error implements the error interface.
func (e *RaftAddressFlagError) Error() string {
	return "--raft-port cannot replace the port of " + e.Key + " \"" + e.Address + "\": " + e.Cause.Error()
}

// Unwrap returns the parse failure.
func (e *RaftAddressFlagError) Unwrap() error { return e.Cause }
