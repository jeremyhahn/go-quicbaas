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
// Merge train failures
// =============================================================================

var (
	// ErrTrainUnavailable indicates a project whose binding trains its merges
	// reached a node whose module was built without a train.
	ErrTrainUnavailable = errors.New("sdlc: the merge train is not available on this node")

	// ErrTrainBuild indicates a train's branch could not be built.
	ErrTrainBuild = errors.New("sdlc: the merge train's branch could not be built")

	// ErrTrainLanding indicates a verified train could not be landed.
	ErrTrainLanding = errors.New("sdlc: the merge train could not land")
)

// TrainUnavailableError reports a trained project on a node with no train.
//
// IT FAILS THE DELIVERY RATHER THAN MERGING ONE CHANGE AT A TIME. The binding
// says this project's changes land through the train; a node that quietly
// merged them one by one would be the operator's configuration overruled by
// whichever node happened to run the job.
type TrainUnavailableError struct {
	ProjectID string
}

// Error implements the error interface.
func (e *TrainUnavailableError) Error() string {
	return "sdlc: project " + e.ProjectID + " lands its changes through the merge train, and this node's module has none"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *TrainUnavailableError) Unwrap() error { return ErrTrainUnavailable }

// TrainBuildError reports a train whose branch could not be built for a reason
// that is not any one change's: a fetch the forge did not answer, a push it
// refused. The train is abandoned and its changes wait for the next one.
type TrainBuildError struct {
	ProjectID string
	Train     int
	Reason    string
	Cause     error
}

// Error implements the error interface.
func (e *TrainBuildError) Error() string {
	msg := "sdlc: merge train #" + strconv.Itoa(e.Train) + " of project " + e.ProjectID + " could not be built: " + e.Reason
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is support.
func (e *TrainBuildError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrTrainBuild}
	}
	return []error{ErrTrainBuild, e.Cause}
}

// TrainLandingError reports a verified train the forge would not land.
type TrainLandingError struct {
	ProjectID   string
	Train       int
	PullRequest int
	Reason      string
	Cause       error
}

// Error implements the error interface.
func (e *TrainLandingError) Error() string {
	msg := "sdlc: merge train #" + strconv.Itoa(e.Train) + " of project " + e.ProjectID +
		" (#" + strconv.Itoa(e.PullRequest) + ") could not land: " + e.Reason
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is support.
func (e *TrainLandingError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrTrainLanding}
	}
	return []error{ErrTrainLanding, e.Cause}
}
