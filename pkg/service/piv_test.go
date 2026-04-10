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

func TestPIVNotAvailableError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &PIVNotAvailableError{}
		assert.Equal(t, "PIV service not available", err.Error())
	})
}

func TestPIVNotAvailableError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &PIVNotAvailableError{}
		assert.True(t, err.Is(&PIVNotAvailableError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &PIVNotAvailableError{}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestPIVSlotNotFoundError_Error(t *testing.T) {
	t.Run("includes slot identifier", func(t *testing.T) {
		err := &PIVSlotNotFoundError{Slot: "9a"}
		assert.Equal(t, "PIV slot not found: 9a", err.Error())
	})
}

func TestPIVSlotNotFoundError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &PIVSlotNotFoundError{Slot: "9a"}
		assert.True(t, err.Is(&PIVSlotNotFoundError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &PIVSlotNotFoundError{Slot: "9a"}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestPIVInvalidSlotError_Error(t *testing.T) {
	t.Run("includes slot identifier", func(t *testing.T) {
		err := &PIVInvalidSlotError{Slot: "ff"}
		assert.Equal(t, "invalid PIV slot: ff", err.Error())
	})
}

func TestPIVInvalidSlotError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &PIVInvalidSlotError{Slot: "ff"}
		assert.True(t, err.Is(&PIVInvalidSlotError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &PIVInvalidSlotError{Slot: "ff"}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestPIVKeyGenerationError_Error(t *testing.T) {
	t.Run("includes slot and cause", func(t *testing.T) {
		cause := errors.New("hardware error")
		err := &PIVKeyGenerationError{Slot: "9a", Cause: cause}
		assert.Equal(t, "PIV key generation failed for slot 9a: hardware error", err.Error())
	})
}

func TestPIVKeyGenerationError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("device busy")
		err := &PIVKeyGenerationError{Slot: "9c", Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}

func TestPIVCertificateOperationError_Error(t *testing.T) {
	t.Run("includes operation slot and cause", func(t *testing.T) {
		cause := errors.New("expired")
		err := &PIVCertificateOperationError{Slot: "9d", Operation: "import", Cause: cause}
		assert.Equal(t, "PIV certificate import failed for slot 9d: expired", err.Error())
	})
}

func TestPIVCertificateOperationError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("invalid format")
		err := &PIVCertificateOperationError{Slot: "9e", Operation: "export", Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}
