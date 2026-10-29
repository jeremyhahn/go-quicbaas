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

package server

import "errors"

// Sentinel errors for embedded fleet node lifecycle operations.
var (
	// ErrEmbeddedFleetNodeNotEnabled is returned when an operation requires the
	// embedded fleet node but the config flag is disabled.
	ErrEmbeddedFleetNodeNotEnabled = errors.New("embedded agent: not enabled in configuration")

	// ErrEmbeddedFleetNodeAlreadyStarted is returned when StartEmbeddedFleetNode is
	// called a second time on a Server that already has a running embedded fleet node.
	ErrEmbeddedFleetNodeAlreadyStarted = errors.New("embedded agent: already started")

	// ErrEmbeddedFleetNodeNoCA is returned when embedded fleet node startup requires a
	// certificate authority but s.CA() is nil (TLS disabled or CA not yet
	// initialized).
	ErrEmbeddedFleetNodeNoCA = errors.New("embedded agent: CA not available — ensure TLS is enabled before starting embedded agent")

	// ErrEmbeddedFleetNodeNoRegistry is returned when the fleet registry has not
	// been initialized before StartEmbeddedFleetNode is called.
	ErrEmbeddedFleetNodeNoRegistry = errors.New("embedded agent: fleet registry not available — orchestration must start before embedded agent")
)

// EmbeddedFleetNodeAlreadyStartedError is the typed error returned when
// StartEmbeddedFleetNode is invoked on a Server that already has an active
// embedded fleet node.
type EmbeddedFleetNodeAlreadyStartedError struct {
	AgentID string
}

// Error implements the error interface.
func (e *EmbeddedFleetNodeAlreadyStartedError) Error() string {
	return ErrEmbeddedFleetNodeAlreadyStarted.Error() + ": agent_id=" + e.AgentID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *EmbeddedFleetNodeAlreadyStartedError) Unwrap() error {
	return ErrEmbeddedFleetNodeAlreadyStarted
}

// EmbeddedFleetNodeNotEnabledError is the typed error returned when embedded
// fleet node operations are attempted but the flag is disabled.
type EmbeddedFleetNodeNotEnabledError struct{}

// Error implements the error interface.
func (e *EmbeddedFleetNodeNotEnabledError) Error() string {
	return ErrEmbeddedFleetNodeNotEnabled.Error()
}

// Unwrap returns the sentinel for errors.Is support.
func (e *EmbeddedFleetNodeNotEnabledError) Unwrap() error {
	return ErrEmbeddedFleetNodeNotEnabled
}
