//go:build rag

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

package watcher

import "errors"

var (
	// ErrNoPaths is returned when no paths are configured to watch.
	ErrNoPaths = errors.New("no paths configured to watch")

	// ErrInvalidDebounceDelay is returned when the debounce delay is negative.
	ErrInvalidDebounceDelay = errors.New("debounce delay cannot be negative")

	// ErrInvalidBatchInterval is returned when the batch interval is negative.
	ErrInvalidBatchInterval = errors.New("batch interval cannot be negative")

	// ErrInvalidMaxDepth is returned when max depth is negative.
	ErrInvalidMaxDepth = errors.New("max depth cannot be negative")

	// ErrAlreadyStarted is returned when trying to start an already running watcher.
	ErrAlreadyStarted = errors.New("watcher already started")

	// ErrNotStarted is returned when trying to stop a watcher that hasn't been started.
	ErrNotStarted = errors.New("watcher not started")

	// ErrPathNotWatched is returned when trying to remove a path that isn't being watched.
	ErrPathNotWatched = errors.New("path is not being watched")
)

// WatcherCauseError wraps a watcher error with a cause.
type WatcherCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *WatcherCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *WatcherCauseError) Unwrap() error {
	return e.Cause
}
