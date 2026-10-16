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

package apimodule

import (
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// SurfaceComposingError reports a module surface consulted before the
// backing engine finished composing. Authentication fails closed on it.
type SurfaceComposingError struct {
	// Surface names the consulted surface.
	Surface string
}

// Error describes the composing state.
func (e *SurfaceComposingError) Error() string {
	return "quicbaas module: " + e.Surface + " is not yet available (server still composing)"
}

// ErrorCode classifies the composing state as unavailable, the class REST
// (503) and gRPC (Unavailable) answer it with.
func (e *SurfaceComposingError) ErrorCode() qberrors.ErrorCode {
	return qberrors.ErrUnavailable
}
