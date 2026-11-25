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

package errors

import (
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDescribesRequest_RequestClasses pins the classes whose message reaches
// the caller.
func TestDescribesRequest_RequestClasses(t *testing.T) {
	for _, code := range []ErrorCode{
		ErrNotFound, ErrTimeout, ErrDeadlineExceeded, ErrInvalidInput, ErrValidation,
		ErrForbidden, ErrNotAllowed, ErrCrossTenantAccess, ErrUnauthorized,
		ErrConflict, ErrAlreadyExists,
	} {
		assert.True(t, DescribesRequest(code), "code %s", code)
	}
}

// TestDescribesRequest_ServerClasses pins that infrastructure, internal and
// unknown classes are sanitized, including a code no table names.
func TestDescribesRequest_ServerClasses(t *testing.T) {
	for _, code := range []ErrorCode{
		ErrInternal, ErrUnknown, ErrStorage, ErrRaft, ErrUnavailable,
		ErrLeaderNotAvailable, ErrQuorumLost, ErrDataLoss, ErrConfiguration,
		ErrorCode(9999),
	} {
		assert.False(t, DescribesRequest(code), "code %s", code)
	}
}

type callerSafeTestError struct{}

func (callerSafeTestError) Error() string { return "enable the widget" }
func (callerSafeTestError) CallerSafe()   {}

func TestIsCallerSafe(t *testing.T) {
	wrapped := &QBError{Code: ErrPreconditionFailed, Op: "test.Op", Err: callerSafeTestError{}}
	assert.True(t, IsCallerSafe(wrapped), "a QBError wrapping a CallerSafe error")
	assert.True(t, IsCallerSafe(fmt.Errorf("outer: %w", wrapped)), "through fmt wrapping")
	assert.False(t, IsCallerSafe(stderrors.New("plain")), "a plain error")
	assert.False(t, IsCallerSafe(&QBError{Code: ErrPreconditionFailed, Op: "test.Op", Err: stderrors.New("x")}))
	assert.False(t, IsCallerSafe(nil))
}
