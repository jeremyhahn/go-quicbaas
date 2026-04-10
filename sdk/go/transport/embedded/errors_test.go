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

package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestErrTransportClosed(t *testing.T) {
	t.Run("is a connection error", func(t *testing.T) {
		code := sdkerrors.Code(ErrTransportClosed)
		assert.Equal(t, sdkerrors.CodeConnection, code)
	})

	t.Run("message contains expected text", func(t *testing.T) {
		msg := ErrTransportClosed.Error()
		assert.Contains(t, msg, "transport is closed")
		assert.Contains(t, msg, "CONNECTION")
	})
}

func TestErrNilService(t *testing.T) {
	t.Run("is an invalid input error", func(t *testing.T) {
		assert.True(t, sdkerrors.IsInvalidInput(ErrNilService))
	})

	t.Run("message contains expected text", func(t *testing.T) {
		msg := ErrNilService.Error()
		assert.Contains(t, msg, "service is required")
		assert.Contains(t, msg, "INVALID_INPUT")
	})
}

func TestErrStreamClosed(t *testing.T) {
	t.Run("is a connection error", func(t *testing.T) {
		code := sdkerrors.Code(ErrStreamClosed)
		assert.Equal(t, sdkerrors.CodeConnection, code)
	})

	t.Run("message contains expected text", func(t *testing.T) {
		msg := ErrStreamClosed.Error()
		assert.Contains(t, msg, "stream is closed")
		assert.Contains(t, msg, "CONNECTION")
	})
}

func TestErrSendOnClosedStream(t *testing.T) {
	t.Run("is a connection error", func(t *testing.T) {
		code := sdkerrors.Code(ErrSendOnClosedStream)
		assert.Equal(t, sdkerrors.CodeConnection, code)
	})

	t.Run("message contains expected text", func(t *testing.T) {
		msg := ErrSendOnClosedStream.Error()
		assert.Contains(t, msg, "cannot send on closed stream")
		assert.Contains(t, msg, "CONNECTION")
	})
}

func TestErrRecvOnClosedStream(t *testing.T) {
	t.Run("is a connection error", func(t *testing.T) {
		code := sdkerrors.Code(ErrRecvOnClosedStream)
		assert.Equal(t, sdkerrors.CodeConnection, code)
	})

	t.Run("message contains expected text", func(t *testing.T) {
		msg := ErrRecvOnClosedStream.Error()
		assert.Contains(t, msg, "cannot receive on closed stream")
		assert.Contains(t, msg, "CONNECTION")
	})
}

func TestUnknownMethodError(t *testing.T) {
	t.Run("error output contains service and method", func(t *testing.T) {
		err := NewUnknownMethodError("KVService", "DoStuff")
		require.NotNil(t, err)

		msg := err.Error()
		assert.Contains(t, msg, "KVService")
		assert.Contains(t, msg, "DoStuff")
		assert.Contains(t, msg, "UNIMPLEMENTED")
		assert.Contains(t, msg, "unknown method")
	})

	t.Run("fields are set correctly", func(t *testing.T) {
		err := NewUnknownMethodError("AuthService", "Login")
		assert.Equal(t, "AuthService", err.Service)
		assert.Equal(t, "Login", err.Method)
	})
}

func TestInvalidRequestTypeError(t *testing.T) {
	t.Run("error output contains all fields", func(t *testing.T) {
		err := NewInvalidRequestTypeError("KVService", "Put", "*PutRequest", "string")
		require.NotNil(t, err)

		msg := err.Error()
		assert.Contains(t, msg, "KVService")
		assert.Contains(t, msg, "Put")
		assert.Contains(t, msg, "*PutRequest")
		assert.Contains(t, msg, "string")
		assert.Contains(t, msg, "INVALID_INPUT")
	})

	t.Run("fields are set correctly", func(t *testing.T) {
		err := NewInvalidRequestTypeError("HealthService", "Check", "*HealthRequest", "int")
		assert.Equal(t, "HealthService", err.Service)
		assert.Equal(t, "Check", err.Method)
		assert.Equal(t, "*HealthRequest", err.ExpectedType)
		assert.Equal(t, "int", err.ActualType)
	})
}

func TestServiceNotConfiguredError(t *testing.T) {
	t.Run("error output contains service name", func(t *testing.T) {
		err := NewServiceNotConfiguredError("KMSService")
		require.NotNil(t, err)

		msg := err.Error()
		assert.Contains(t, msg, "KMSService")
		assert.Contains(t, msg, "UNAVAILABLE")
		assert.Contains(t, msg, "service not configured")
	})

	t.Run("field is set correctly", func(t *testing.T) {
		err := NewServiceNotConfiguredError("SecretsService")
		assert.Equal(t, "SecretsService", err.Service)
	})
}

func TestStreamNotSupportedError(t *testing.T) {
	t.Run("error output contains service and method", func(t *testing.T) {
		err := NewStreamNotSupportedError("KVService", "Get")
		require.NotNil(t, err)

		msg := err.Error()
		assert.Contains(t, msg, "KVService")
		assert.Contains(t, msg, "Get")
		assert.Contains(t, msg, "UNIMPLEMENTED")
		assert.Contains(t, msg, "streaming not supported")
	})

	t.Run("fields are set correctly", func(t *testing.T) {
		err := NewStreamNotSupportedError("HealthService", "Liveness")
		assert.Equal(t, "HealthService", err.Service)
		assert.Equal(t, "Liveness", err.Method)
	})
}
