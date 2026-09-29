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

package softwarefactoryworker

import (
	"errors"
	"strconv"
	"strings"
)

// ErrInvalidBuild is the sentinel every error in this package unwraps to: the
// build as described cannot be made.
var ErrInvalidBuild = errors.New("worker image: invalid build")

// PinsMissingError reports toolchain pins that are not set.
type PinsMissingError struct {
	Missing []string
}

// Error implements the error interface.
func (e *PinsMissingError) Error() string {
	return ErrInvalidBuild.Error() + ": no toolchain pin for " + strings.Join(e.Missing, ", ") +
		" (pkg/version/toolchain.mk)"
}

// Unwrap returns the sentinel.
func (e *PinsMissingError) Unwrap() error { return ErrInvalidBuild }

// InvalidVersionError reports a version that cannot be part of a tag or a
// build argument.
type InvalidVersionError struct {
	What    string
	Version string
}

// Error implements the error interface.
func (e *InvalidVersionError) Error() string {
	return ErrInvalidBuild.Error() + ": " + e.What + " version " + strconv.Quote(e.Version) +
		" is not [0-9A-Za-z][0-9A-Za-z.-]*"
}

// Unwrap returns the sentinel.
func (e *InvalidVersionError) Unwrap() error { return ErrInvalidBuild }

// InvalidBuildError reports a build field that is out of range.
type InvalidBuildError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *InvalidBuildError) Error() string {
	return ErrInvalidBuild.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel.
func (e *InvalidBuildError) Unwrap() error { return ErrInvalidBuild }
