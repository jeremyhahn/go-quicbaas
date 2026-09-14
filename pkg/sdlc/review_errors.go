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
// Review runner errors
// =============================================================================

// ErrReview indicates the review runner refused or could not produce a
// review.
var ErrReview = errors.New("sdlc review: refused")

// ReviewVerdictUnreadableError reports a reviewer turn that states no
// verdict: no declaration, an unknown code, or a verdict with no findings.
//
// IT IS A REFUSAL, NOT A RETRY SIGNAL. The runner records it on the delivery
// as VerdictError, which the gate already fails closed on, and publishes a
// red commit status — because a reviewer that could not state an answer has
// not approved anything, and the alternative reading ("no verdict yet")
// leaves the change waiting on a review that already finished.
type ReviewVerdictUnreadableError struct {
	// Reason says what was wrong with the reply.
	Reason string
}

// Error implements the error interface.
func (e *ReviewVerdictUnreadableError) Error() string {
	return ErrReview.Error() + ": " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ReviewVerdictUnreadableError) Unwrap() error { return ErrReview }

// ReviewerNotConfiguredError reports a review asked for on a project that
// binds no reviewer.
//
// A refusal rather than a silent skip: a project nobody configured a reviewer
// for is a project whose pull requests will never be reviewed by this module,
// and a dispatch that quietly did nothing would leave an operator waiting for
// a verdict that was never going to come.
type ReviewerNotConfiguredError struct {
	// ProjectID is the project whose binding names no reviewer.
	ProjectID string
}

// Error implements the error interface.
func (e *ReviewerNotConfiguredError) Error() string {
	return ErrReview.Error() + ": project " + strconv.Quote(e.ProjectID) +
		" binds no reviewer, so nothing in this module can produce a verdict for it"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ReviewerNotConfiguredError) Unwrap() error { return ErrReview }

// ReviewerNotIndependentError reports a reviewer drawn from the same model
// family as the worker, under a policy that requires independence.
//
// REFUSED BEFORE THE SESSION IS SPAWNED, not after the verdict is posted. The
// gate would discard such an approval anyway (countIndependentApprovals), so
// running the review would spend a model's budget to produce a verdict that
// can never clear the pull request — and, worse, would leave a green commit
// status beside a gate that is still blocked, which reads to a person as the
// module disagreeing with itself.
type ReviewerNotIndependentError struct {
	// ProjectID is the project whose binding pairs them.
	ProjectID string

	// Family is the model family both sides share.
	Family string
}

// Error implements the error interface.
func (e *ReviewerNotIndependentError) Error() string {
	return ErrReview.Error() + ": project " + strconv.Quote(e.ProjectID) +
		" requires an independent reviewer but its reviewer and its worker are both " +
		strconv.Quote(e.Family)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ReviewerNotIndependentError) Unwrap() error { return ErrReview }

// ReviewSessionError reports a review with no usable reviewer session.
type ReviewSessionError struct {
	// PullRequest is the pull request the review is of.
	PullRequest int

	// Message says what was wrong with the session.
	Message string
}

// Error implements the error interface.
func (e *ReviewSessionError) Error() string {
	return ErrReview.Error() + ": pull request #" + strconv.Itoa(e.PullRequest) + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ReviewSessionError) Unwrap() error { return ErrReview }
