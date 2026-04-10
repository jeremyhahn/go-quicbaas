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

func TestFIDO2NotAvailableError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &FIDO2NotAvailableError{}
		assert.Equal(t, "FIDO2 service not available", err.Error())
	})
}

func TestFIDO2NotAvailableError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &FIDO2NotAvailableError{}
		assert.True(t, err.Is(&FIDO2NotAvailableError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &FIDO2NotAvailableError{}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestFIDO2RegistrationError_Error(t *testing.T) {
	t.Run("includes username and cause", func(t *testing.T) {
		cause := errors.New("attestation failed")
		err := &FIDO2RegistrationError{Username: "alice", Cause: cause}
		assert.Equal(t, "FIDO2 registration failed for user alice: attestation failed", err.Error())
	})
}

func TestFIDO2RegistrationError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("credential store full")
		err := &FIDO2RegistrationError{Username: "bob", Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}

func TestFIDO2AuthenticationError_Error(t *testing.T) {
	t.Run("includes username and cause", func(t *testing.T) {
		cause := errors.New("invalid signature")
		err := &FIDO2AuthenticationError{Username: "carol", Cause: cause}
		assert.Equal(t, "FIDO2 authentication failed for user carol: invalid signature", err.Error())
	})
}

func TestFIDO2AuthenticationError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("timeout")
		err := &FIDO2AuthenticationError{Username: "dave", Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
	})
}

func TestFIDO2CredentialNotFoundError_Error(t *testing.T) {
	t.Run("includes username", func(t *testing.T) {
		err := &FIDO2CredentialNotFoundError{Username: "eve"}
		assert.Equal(t, "no FIDO2 credentials found for user: eve", err.Error())
	})
}

func TestFIDO2CredentialNotFoundError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &FIDO2CredentialNotFoundError{Username: "test"}
		assert.True(t, err.Is(&FIDO2CredentialNotFoundError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &FIDO2CredentialNotFoundError{Username: "test"}
		assert.False(t, err.Is(errors.New("other")))
	})
}

func TestFIDO2InvalidChallengeError_Error(t *testing.T) {
	t.Run("returns correct message", func(t *testing.T) {
		err := &FIDO2InvalidChallengeError{}
		assert.Equal(t, "FIDO2 challenge verification failed", err.Error())
	})
}

func TestFIDO2InvalidChallengeError_Is(t *testing.T) {
	t.Run("matches same type", func(t *testing.T) {
		err := &FIDO2InvalidChallengeError{}
		assert.True(t, err.Is(&FIDO2InvalidChallengeError{}))
	})

	t.Run("does not match different type", func(t *testing.T) {
		err := &FIDO2InvalidChallengeError{}
		assert.False(t, err.Is(errors.New("other")))
	})
}
