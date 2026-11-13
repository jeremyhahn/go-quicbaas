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
	"strings"
)

// =============================================================================
// Dependency-graph errors
// =============================================================================
//
// TWO SENTINELS, AND BOTH MEAN "NO SWEEP WILL FIX THIS". They are kept apart
// from a forge outage — see readinessRefusal — because a graph defect parks the
// work for a person and an outage does not, and telling an operator to go look
// at an issue that is perfectly well formed wastes exactly the attention this
// whole mechanism is trying to save.

var (
	// ErrDependencyCycle indicates a chain of declared dependencies that
	// closes on itself. A blocks B blocks A can never start, and an
	// undetected one is an unbounded re-examination — or, given a caller that
	// acts on unsatisfied dependencies, an unbounded spawn.
	ErrDependencyCycle = errors.New("sdlc: the declared dependencies form a cycle")

	// ErrDependencyDepthExceeded indicates a dependency chain longer than the
	// policy's bound. Refused rather than truncated: a truncated walk would
	// report a graph satisfiable on the strength of the part it looked at.
	ErrDependencyDepthExceeded = errors.New("sdlc: the declared dependency chain is longer than the depth bound")
)

// DependencyCycleError reports a cycle, naming the whole chain.
//
// THE CHAIN, NOT JUST THE REFERENCE IT CLOSED ON. A person handed "issue #4 is
// in a cycle" has to reconstruct the path by hand across however many
// repositories it crosses; the walk already knows it, so it says it.
type DependencyCycleError struct {
	// Chain is the dependency path in order, ending with the reference that
	// was already on it.
	Chain []string
}

// Error implements the error interface.
func (e *DependencyCycleError) Error() string {
	return ErrDependencyCycle.Error() + ": " + strings.Join(e.Chain, " -> ")
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DependencyCycleError) Unwrap() error { return ErrDependencyCycle }

// DependencyDepthExceededError reports a chain past the bound, naming both.
type DependencyDepthExceededError struct {
	// Chain is the path walked when the bound was reached.
	Chain []string

	// Limit is the bound that was exceeded.
	Limit int
}

// Error implements the error interface.
func (e *DependencyDepthExceededError) Error() string {
	return ErrDependencyDepthExceeded.Error() + " of " + strconv.Itoa(e.Limit) + ": " +
		strings.Join(e.Chain, " -> ")
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *DependencyDepthExceededError) Unwrap() error { return ErrDependencyDepthExceeded }
