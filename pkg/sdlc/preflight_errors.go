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

import "errors"

// ErrPreflight indicates a preflight that reached no verdict: its job could
// not be run, or could not be followed to its end. A preflight that RAN and
// went red is not this error; it is a result whose Passed is false.
var ErrPreflight = errors.New("sdlc: preflight reached no verdict")

// PreflightError reports a preflight that reached no verdict.
type PreflightError struct {
	// ProjectID is the project the preflight was for.
	ProjectID string

	// JobID is the preflight job, empty when none was started.
	JobID string

	// Reason states what went wrong, in the caller's own words.
	Reason string

	// Cause is the underlying failure, nil when there is none.
	Cause error
}

// Error implements the error interface.
func (e *PreflightError) Error() string {
	msg := ErrPreflight.Error() + ": project " + e.ProjectID
	if e.JobID != "" {
		msg += ": job " + e.JobID
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PreflightError) Unwrap() error { return ErrPreflight }
