//go:build !integration

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

package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPINNotAvailableError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &PINNotAvailableError{}
		assert.Equal(t, "PIN service not available", err.Error())
	})
}

func TestPINNotAvailableError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &PINNotAvailableError{}
		assert.True(t, err.Is(&PINNotAvailableError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &PINNotAvailableError{}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestPINVerificationError_Error(t *testing.T) {
	t.Run("SO PIN type", func(t *testing.T) {
		err := &PINVerificationError{PINType: "SO"}
		assert.Equal(t, "SO PIN verification failed", err.Error())
	})

	t.Run("User PIN type", func(t *testing.T) {
		err := &PINVerificationError{PINType: "User"}
		assert.Equal(t, "User PIN verification failed", err.Error())
	})
}

func TestPINVerificationError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &PINVerificationError{PINType: "SO"}
		assert.True(t, err.Is(&PINVerificationError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &PINVerificationError{PINType: "SO"}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestPINLockedOutError_Error(t *testing.T) {
	t.Run("with recovery seconds", func(t *testing.T) {
		err := &PINLockedOutError{RecoverySeconds: 300}
		assert.Equal(t, "PIN locked out, automatic recovery pending", err.Error())
	})

	t.Run("without recovery - SO PIN reset required", func(t *testing.T) {
		err := &PINLockedOutError{RecoverySeconds: 0}
		assert.Equal(t, "PIN locked out, SO PIN reset required", err.Error())
	})
}

func TestPINLockedOutError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &PINLockedOutError{RecoverySeconds: 60}
		assert.True(t, err.Is(&PINLockedOutError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &PINLockedOutError{}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestPINChangeError_Error(t *testing.T) {
	t.Run("includes PIN type and cause", func(t *testing.T) {
		cause := errors.New("too short")
		err := &PINChangeError{PINType: "User", Cause: cause}
		assert.Equal(t, "User PIN change failed: too short", err.Error())
	})
}

func TestPINChangeError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("policy violation")
		err := &PINChangeError{PINType: "SO", Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}

func TestPINPolicyViolationError_Error(t *testing.T) {
	t.Run("with detail", func(t *testing.T) {
		err := &PINPolicyViolationError{Detail: "minimum 6 digits required"}
		assert.Equal(t, "PIN policy violation: minimum 6 digits required", err.Error())
	})

	t.Run("without detail", func(t *testing.T) {
		err := &PINPolicyViolationError{}
		assert.Equal(t, "PIN does not meet policy requirements", err.Error())
	})
}

func TestPINPolicyViolationError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &PINPolicyViolationError{Detail: "test"}
		assert.True(t, err.Is(&PINPolicyViolationError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &PINPolicyViolationError{}
		assert.False(t, err.Is(errors.New("other")))
	})
}
