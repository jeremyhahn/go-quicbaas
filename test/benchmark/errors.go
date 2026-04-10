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

package benchmark

import (
	"errors"
	"strconv"
)

// ErrKeyNotFound is returned when a requested key does not exist.
var ErrKeyNotFound = errors.New("key not found")

// WorkerError represents an error from a specific worker iteration.
type WorkerError struct {
	WorkerID  int
	Iteration int
	Cause     error
}

// Error implements the error interface.
func (e *WorkerError) Error() string {
	return "worker " + strconv.Itoa(e.WorkerID) + " iteration " + strconv.Itoa(e.Iteration) + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *WorkerError) Unwrap() error {
	return e.Cause
}
