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

import "errors"

// ErrWorkerSettingsWrite indicates the worker settings file could not be
// rendered or written into the agent configuration directory.
var ErrWorkerSettingsWrite = errors.New("orchestration: worker settings write failed")

// WorkerSettingsWriteError indicates the worker settings file at Path could
// not be rendered or written.
type WorkerSettingsWriteError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *WorkerSettingsWriteError) Error() string {
	return withCause(ErrWorkerSettingsWrite.Error()+": "+e.Path, e.Cause)
}

// Unwrap returns the sentinel and the cause for errors.Is/As.
func (e *WorkerSettingsWriteError) Unwrap() []error {
	return sentinelAndCause(ErrWorkerSettingsWrite, e.Cause)
}
