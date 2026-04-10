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

package quicbaas

import "errors"

// Sentinel errors for storage operations.
var (
	// ErrNilJob indicates a nil job was passed to a storage operation.
	ErrNilJob = errors.New("job cannot be nil")

	// ErrNilExecution indicates a nil execution was passed to a storage operation.
	ErrNilExecution = errors.New("execution cannot be nil")

	// ErrLimitReached is an internal sentinel used to stop iteration.
	ErrLimitReached = errors.New("limit reached")
)
