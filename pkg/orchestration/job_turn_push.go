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

import "context"

// =============================================================================
// TurnPusher — the engine publishes a turn's work, the agent does not
// =============================================================================
//
// A WORKSPACE THAT LIVES IN RAM MAKES PUBLICATION A CORRECTNESS PROPERTY. On
// the ephemeral storage tier (workspace_storage.go) the checkout is a tmpfs
// inside the container: a restart, an eviction or an OOM kill takes every
// commit that was never pushed, and the attempt resumes from a head that is
// missing work a person can see was done. On the persistent tier the same
// commits are still sitting on the host afterwards, which is why nothing has
// needed this until now.
//
// THE ENGINE OWNS THE WHEN, THE RUNNER OWNS THE HOW. This seam is the moment —
// "a turn just touched the tree" — and nothing else. The implementation is the
// SDLC runner's existing publisher, which commits signed and pushes INSIDE the
// worker's container; there is no second git implementation and none on the
// daemon. See pkg/orchestration/sdlc/publish.go.
//
// ASKING THE AGENT TO PUSH WOULD NOT BE THE SAME THING. An agent that is told
// to push is an agent that may forget, may push the wrong ref, and costs tokens
// to be reminded. Deterministic work belongs in Go.

// TurnPusher publishes the work a turn produced.
//
// PushTurn is called after a turn that touched the checkout, and only then. It
// is expected to be idempotent — a turn that committed nothing new publishes
// nothing — and to be a no-op for a job whose workspace is not ephemeral: the
// implementation, not the engine, knows the job's tier.
type TurnPusher interface {
	// PushTurn publishes whatever the turn just left in the checkout. It
	// returns an error only when the work could NOT be published; "there was
	// nothing to publish" is a success.
	PushTurn(ctx context.Context) error
}

// turnPusherHolder boxes the interface so it can live in an atomic.Pointer.
// The value is installed once, from the runner's goroutine, and read from the
// control loop's collector as well — a plain field would be a data race the
// day a second reader appeared rather than the day it was written.
type turnPusherHolder struct {
	pusher TurnPusher
}

// TurnPushError reports that a turn's work could not be published.
//
// IT NAMES THE JOB BECAUSE THE JOB IS WHERE THE WORK IS. Under an ephemeral
// workspace this error is the last moment anything knows the commits exist:
// swallowed, it becomes a job that resumes from an older head with no record
// of what went missing.
type TurnPushError struct {
	// JobID is the job whose turn could not be published.
	JobID string

	// Cause is what the publication failed with.
	Cause error
}

// Error implements the error interface. A TurnPushError with no cause still
// reports the job, because the job is the part a person needs: this error is
// the last record that the work existed.
func (e *TurnPushError) Error() string {
	message := "job " + e.JobID + ": the turn's work could not be published"
	if e.Cause == nil {
		return message
	}
	return message + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying publication failure to errors.Is/As.
func (e *TurnPushError) Unwrap() error {
	return e.Cause
}

// auditTurnPushFailed is the audit event a failed publication files on the
// job, so the job's own page names the work at risk rather than only the
// daemon's log.
const auditTurnPushFailed = "turn:push_failed"

// SetTurnPusher installs the publication a completed turn triggers.
//
// A runner calls it once, right after the spawn that produced the worker's
// container — the same moment it calls SetContainer, and for the same reason:
// both describe where this job's work physically is. A run with none installed
// pushes nothing, which is every lead-driven job and every job on a persistent
// workspace.
func (r *JobRun) SetTurnPusher(pusher TurnPusher) {
	r.turnPusher.Store(&turnPusherHolder{pusher: pusher})
}

// PushTurn publishes the work the turn just produced, and is the ONLY place
// the engine decides that a turn's work should be published.
//
// A run with no pusher installed returns nil without doing anything, so every
// caller can invoke it unconditionally after a productive turn rather than
// each one re-deciding whether this job has an ephemeral workspace.
//
// A FAILURE IS LOUD AND IT IS FATAL TO THE TURN. It is logged at error level
// with the job ID, filed on the job's own audit trail, and RETURNED, because
// on an ephemeral workspace an unpublished commit is data that no longer
// exists after the next restart. Continuing to the next turn would let the
// attempt keep spending tokens building on work it has already lost.
func (r *JobRun) PushTurn(ctx context.Context) error {
	holder := r.turnPusher.Load()
	if holder == nil || holder.pusher == nil {
		return nil
	}
	if err := holder.pusher.PushTurn(ctx); err != nil {
		pushErr := &TurnPushError{JobID: r.JobID, Cause: err}
		if r.Logger != nil {
			r.Logger.Errorf("%s; an ephemeral workspace loses it when the container goes", pushErr.Error())
		}
		r.Audit(auditTurnPushFailed, pushErr.Error())
		return pushErr
	}
	return nil
}
