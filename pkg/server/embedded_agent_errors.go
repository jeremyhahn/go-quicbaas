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

// Sentinel errors for embedded agent lifecycle operations.
var (
	// ErrEmbeddedAgentNotEnabled is returned when an operation requires the
	// embedded agent but the config flag is disabled.
	ErrEmbeddedAgentNotEnabled = errors.New("embedded agent: not enabled in configuration")

	// ErrEmbeddedAgentAlreadyStarted is returned when StartEmbeddedAgent is
	// called a second time on a Server that already has a running embedded agent.
	ErrEmbeddedAgentAlreadyStarted = errors.New("embedded agent: already started")

	// ErrEmbeddedAgentNoCA is returned when embedded agent startup requires a
	// certificate authority but s.xkmsCA is nil (TLS disabled or CA not yet
	// initialized).
	ErrEmbeddedAgentNoCA = errors.New("embedded agent: CA not available — ensure TLS is enabled before starting embedded agent")

	// ErrEmbeddedAgentNoRegistry is returned when the fleet registry has not
	// been initialized before StartEmbeddedAgent is called.
	ErrEmbeddedAgentNoRegistry = errors.New("embedded agent: fleet registry not available — orchestration must start before embedded agent")
)

// EmbeddedAgentAlreadyStartedError is the typed error returned when
// StartEmbeddedAgent is invoked on a Server that already has an active
// embedded agent.
type EmbeddedAgentAlreadyStartedError struct {
	AgentID string
}

// Error implements the error interface.
func (e *EmbeddedAgentAlreadyStartedError) Error() string {
	return ErrEmbeddedAgentAlreadyStarted.Error() + ": agent_id=" + e.AgentID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *EmbeddedAgentAlreadyStartedError) Unwrap() error {
	return ErrEmbeddedAgentAlreadyStarted
}

// EmbeddedAgentNotEnabledError is the typed error returned when embedded
// agent operations are attempted but the flag is disabled.
type EmbeddedAgentNotEnabledError struct{}

// Error implements the error interface.
func (e *EmbeddedAgentNotEnabledError) Error() string {
	return ErrEmbeddedAgentNotEnabled.Error()
}

// Unwrap returns the sentinel for errors.Is support.
func (e *EmbeddedAgentNotEnabledError) Unwrap() error {
	return ErrEmbeddedAgentNotEnabled
}
