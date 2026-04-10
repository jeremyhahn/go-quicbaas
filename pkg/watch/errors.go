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

package watch

// WatchCreateError is returned when creating a watch fails.
type WatchCreateError struct {
	Cause error
}

// Error implements the error interface.
func (e *WatchCreateError) Error() string {
	return "failed to create watch: " + e.Cause.Error()
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *WatchCreateError) Unwrap() error {
	return e.Cause
}
