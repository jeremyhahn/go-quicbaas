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
)

// =============================================================================
// Triage failures
// =============================================================================
//
// Each is a TYPE carrying the fields an operator needs, unwrapping to a
// sentinel a caller can test. A triage failure is reported per issue wherever
// it can be: one issue the agent could not be read on must not cost the whole
// sweep, because the sweep is the only path the other issues have.

var (
	// ErrTriageVerdict indicates one issue could not be given a verdict.
	ErrTriageVerdict = errors.New("sdlc: triage produced no usable verdict")

	// ErrTriageNotBound indicates a project with no forge binding was asked
	// to be triaged.
	ErrTriageNotBound = errors.New("sdlc: project has no forge binding to triage")

	// ErrTriageSession indicates the triage run could not hold the session
	// it reads the repository through.
	ErrTriageSession = errors.New("sdlc: triage session unavailable")
)

// TriageVerdictError reports one issue the run could not conclude about.
type TriageVerdictError struct {
	// Issue is the issue number, zero when the failure is not about one.
	Issue int

	// Reason states what went wrong, in the runner's own words.
	Reason string

	// Cause is the underlying failure, nil when there is none.
	Cause error
}

// Error implements the error interface.
func (e *TriageVerdictError) Error() string {
	msg := ErrTriageVerdict.Error()
	if e.Issue > 0 {
		msg += ": issue #" + strconv.Itoa(e.Issue)
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
func (e *TriageVerdictError) Unwrap() error { return ErrTriageVerdict }

// TriageNotBoundError reports a project that cannot be triaged because
// nothing says which repository it delivers to.
type TriageNotBoundError struct {
	ProjectID string
}

// Error implements the error interface.
func (e *TriageNotBoundError) Error() string {
	return ErrTriageNotBound.Error() + ": project " + e.ProjectID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TriageNotBoundError) Unwrap() error { return ErrTriageNotBound }

// TriageSessionError reports a run that could not hold the session it reads
// the repository through.
type TriageSessionError struct {
	ProjectID string
	Message   string
}

// Error implements the error interface.
func (e *TriageSessionError) Error() string {
	msg := ErrTriageSession.Error()
	if e.ProjectID != "" {
		msg += ": project " + e.ProjectID
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TriageSessionError) Unwrap() error { return ErrTriageSession }
