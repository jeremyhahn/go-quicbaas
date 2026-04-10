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
	"github.com/stretchr/testify/require"
)

func TestCANotAvailableError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &CANotAvailableError{}
		assert.Equal(t, "CA service not available", err.Error())
	})
}

func TestCANotAvailableError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &CANotAvailableError{}
		target := &CANotAvailableError{}
		assert.True(t, err.Is(target))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &CANotAvailableError{}
		target := errors.New("other error")
		assert.False(t, err.Is(target))
	})
}

func TestCASigningError_Error(t *testing.T) {
	t.Run("includes cause message", func(t *testing.T) {
		cause := errors.New("invalid key")
		err := &CASigningError{Cause: cause}
		assert.Equal(t, "CA signing failed: invalid key", err.Error())
	})
}

func TestCASigningError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("key expired")
		err := &CASigningError{Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}

func TestCARevocationError_Error(t *testing.T) {
	t.Run("includes serial number and cause", func(t *testing.T) {
		cause := errors.New("not found")
		err := &CARevocationError{SerialNumber: "ABC123", Cause: cause}
		assert.Equal(t, "CA revocation failed for certificate ABC123: not found", err.Error())
	})
}

func TestCARevocationError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("storage error")
		err := &CARevocationError{SerialNumber: "XYZ", Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}

func TestCACertificateNotFoundError_Error(t *testing.T) {
	t.Run("with identity", func(t *testing.T) {
		err := &CACertificateNotFoundError{Identity: "admin@example.com"}
		assert.Equal(t, "CA certificate not found for identity: admin@example.com", err.Error())
	})

	t.Run("without identity", func(t *testing.T) {
		err := &CACertificateNotFoundError{}
		assert.Equal(t, "CA certificate not found", err.Error())
	})
}

func TestCACertificateNotFoundError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &CACertificateNotFoundError{Identity: "test"}
		target := &CACertificateNotFoundError{}
		assert.True(t, err.Is(target))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &CACertificateNotFoundError{Identity: "test"}
		target := errors.New("other")
		assert.False(t, err.Is(target))
	})
}

func TestCAInvalidCSRError_Error(t *testing.T) {
	t.Run("includes cause", func(t *testing.T) {
		cause := errors.New("malformed PEM")
		err := &CAInvalidCSRError{Cause: cause}
		assert.Equal(t, "invalid CSR: malformed PEM", err.Error())
	})
}

func TestCAInvalidCSRError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("parse error")
		err := &CAInvalidCSRError{Cause: cause}
		require.Equal(t, cause, err.Unwrap())
	})
}

func TestCACRLGenerationError_Error(t *testing.T) {
	t.Run("includes cause", func(t *testing.T) {
		cause := errors.New("signing key unavailable")
		err := &CACRLGenerationError{Cause: cause}
		assert.Equal(t, "CRL generation failed: signing key unavailable", err.Error())
	})
}

func TestCACRLGenerationError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("disk full")
		err := &CACRLGenerationError{Cause: cause}
		require.Equal(t, cause, err.Unwrap())
	})
}
