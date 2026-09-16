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

import (
	"errors"
	"strconv"
	"strings"
)

// =============================================================================
// Agent profile sentinel errors
// =============================================================================

var (
	// ErrAgentProfileNotFound indicates a named profile is not defined.
	ErrAgentProfileNotFound = errors.New("agent profile: not found")

	// ErrAgentPoolNotFound indicates a named pool is not defined.
	ErrAgentPoolNotFound = errors.New("agent pool: not found")

	// ErrAgentProfileInUse indicates a profile cannot be removed because a
	// pool still names it.
	ErrAgentProfileInUse = errors.New("agent profile: in use")

	// ErrAgentPoolExhausted indicates every profile in a pool is at its
	// concurrency limit.
	//
	// IT IS NOT A FAILURE, AND CALLERS MUST BE ABLE TO TELL. A pool that is
	// entirely full is working exactly as designed and clears on its own as
	// jobs finish; a pool naming a profile nobody defined never clears. A
	// dispatcher that retries the first and gives up on the second needs the
	// two to be distinguishable without reading a message.
	ErrAgentPoolExhausted = errors.New("agent pool: every profile at its limit")

	// ErrAgentProfileStore indicates an internal profile-store failure.
	ErrAgentProfileStore = errors.New("agent profile: store error")
)

// =============================================================================
// Typed errors
// =============================================================================

// ProfileNotFoundError reports a profile nobody defined. Pool names the pool
// that referred to it, when the reference came from one.
type ProfileNotFoundError struct {
	Name string
	Pool string
}

// Error implements the error interface.
func (e *ProfileNotFoundError) Error() string {
	msg := ErrAgentProfileNotFound.Error() + ": " + strconv.Quote(e.Name)
	if e.Pool != "" && e.Pool != e.Name {
		msg += " (referred to by pool " + strconv.Quote(e.Pool) + ")"
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProfileNotFoundError) Unwrap() error { return ErrAgentProfileNotFound }

// PoolNotFoundError reports a pool nobody defined.
type PoolNotFoundError struct {
	Name string
}

// Error implements the error interface.
func (e *PoolNotFoundError) Error() string {
	return ErrAgentPoolNotFound.Error() + ": " + strconv.Quote(e.Name)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PoolNotFoundError) Unwrap() error { return ErrAgentPoolNotFound }

// ProfileInUseError reports a profile still named by at least one pool. It
// carries the pools so the operator can act on the refusal without a second
// query.
type ProfileInUseError struct {
	Name  string
	Pools []string
}

// Error implements the error interface.
func (e *ProfileInUseError) Error() string {
	return ErrAgentProfileInUse.Error() + ": " + strconv.Quote(e.Name) +
		" is named by pool(s): " + strings.Join(e.Pools, ", ")
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProfileInUseError) Unwrap() error { return ErrAgentProfileInUse }

// ProfilePoolExhaustedError reports every profile in a pool at its limit. Full
// names each one with the load that filled it, because "the fleet is slow" and
// "the NPU is saturated and the spill target is too" are the same symptom with
// different remedies.
type ProfilePoolExhaustedError struct {
	Pool string
	Full []string
}

// Error implements the error interface.
func (e *ProfilePoolExhaustedError) Error() string {
	return ErrAgentPoolExhausted.Error() + ": pool " + strconv.Quote(e.Pool) +
		": " + strings.Join(e.Full, ", ")
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProfilePoolExhaustedError) Unwrap() error { return ErrAgentPoolExhausted }

// AgentProfileStoreError indicates an internal store failure (marshal,
// corruption, backing store unavailable).
type AgentProfileStoreError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *AgentProfileStoreError) Error() string {
	return ErrAgentProfileStore.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AgentProfileStoreError) Unwrap() error { return ErrAgentProfileStore }
