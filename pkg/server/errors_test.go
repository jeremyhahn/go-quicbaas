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

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServerCauseError_Error(t *testing.T) {
	t.Run("formats message with cause", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := &ServerCauseError{Message: "failed to connect", Cause: cause}
		assert.Equal(t, "failed to connect: connection refused", err.Error())
	})
}

func TestServerCauseError_Unwrap(t *testing.T) {
	t.Run("returns underlying cause", func(t *testing.T) {
		cause := errors.New("disk full")
		err := &ServerCauseError{Message: "write failed", Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}

func TestServerMessageError_Error(t *testing.T) {
	t.Run("returns message", func(t *testing.T) {
		err := &ServerMessageError{Message: "server is shutting down"}
		assert.Equal(t, "server is shutting down", err.Error())
	})

	t.Run("returns empty message", func(t *testing.T) {
		err := &ServerMessageError{}
		assert.Equal(t, "", err.Error())
	})
}

func TestSentinelErrors(t *testing.T) {
	t.Run("ErrDataDirRequired", func(t *testing.T) {
		assert.Equal(t, "data directory is required", ErrDataDirRequired.Error())
	})

	t.Run("ErrServerNotRunning", func(t *testing.T) {
		assert.Equal(t, "server is not running", ErrServerNotRunning.Error())
	})

	t.Run("ErrServerAlreadyRunning", func(t *testing.T) {
		assert.Equal(t, "server is already running", ErrServerAlreadyRunning.Error())
	})

	t.Run("ErrInvalidServerMode", func(t *testing.T) {
		assert.Equal(t, "invalid server mode", ErrInvalidServerMode.Error())
	})

	t.Run("ErrMCPShutdownFailed", func(t *testing.T) {
		assert.Equal(t, "failed to shutdown MCP server", ErrMCPShutdownFailed.Error())
	})

	t.Run("ErrRAGCloseFailed", func(t *testing.T) {
		assert.Equal(t, "failed to close RAG system", ErrRAGCloseFailed.Error())
	})

	t.Run("ErrUnsupportedTransport", func(t *testing.T) {
		assert.Equal(t, "unsupported qrdb transport type", ErrUnsupportedTransport.Error())
	})

	t.Run("ErrTransportCreationFailed", func(t *testing.T) {
		assert.Equal(t, "qrdb transport creation failed", ErrTransportCreationFailed.Error())
	})

	t.Run("ErrNetworkTransportMissingAddress", func(t *testing.T) {
		assert.Equal(t, "address is required for network qrdb transport", ErrNetworkTransportMissingAddress.Error())
	})

	t.Run("ErrUnixTransportMissingSocket", func(t *testing.T) {
		assert.Equal(t, "unix socket path is required for unix qrdb transport", ErrUnixTransportMissingSocket.Error())
	})
}

func TestUnsupportedTransportError(t *testing.T) {
	t.Run("formats message with transport name", func(t *testing.T) {
		err := &UnsupportedTransportError{Transport: "websocket"}
		assert.Equal(t, "unsupported qrdb transport type: websocket", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		err := &UnsupportedTransportError{Transport: "websocket"}
		assert.True(t, errors.Is(err, ErrUnsupportedTransport))
	})

	t.Run("errors.As extracts typed error", func(t *testing.T) {
		var target *UnsupportedTransportError
		err := &UnsupportedTransportError{Transport: "ftp"}
		assert.True(t, errors.As(err, &target))
		assert.Equal(t, "ftp", target.Transport)
	})
}

func TestTransportCreationError(t *testing.T) {
	t.Run("formats message with transport and cause", func(t *testing.T) {
		cause := errors.New("dial refused")
		err := &TransportCreationError{Transport: "grpc", Cause: cause}
		assert.Equal(t, "qrdb grpc transport creation failed: dial refused", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		cause := errors.New("timeout")
		err := &TransportCreationError{Transport: "rest", Cause: cause}
		assert.True(t, errors.Is(err, ErrTransportCreationFailed))
	})

	t.Run("errors.As extracts typed error", func(t *testing.T) {
		var target *TransportCreationError
		cause := errors.New("TLS handshake failed")
		err := &TransportCreationError{Transport: "quic", Cause: cause}
		assert.True(t, errors.As(err, &target))
		assert.Equal(t, "quic", target.Transport)
		assert.Equal(t, cause, target.Cause)
	})
}

func TestNetworkTransportMissingAddressError(t *testing.T) {
	t.Run("formats message with transport name", func(t *testing.T) {
		err := &NetworkTransportMissingAddressError{Transport: "grpc"}
		assert.Equal(t, "address is required for grpc qrdb transport", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		err := &NetworkTransportMissingAddressError{Transport: "rest"}
		assert.True(t, errors.Is(err, ErrNetworkTransportMissingAddress))
	})

	t.Run("errors.As extracts typed error", func(t *testing.T) {
		var target *NetworkTransportMissingAddressError
		err := &NetworkTransportMissingAddressError{Transport: "mcp"}
		assert.True(t, errors.As(err, &target))
		assert.Equal(t, "mcp", target.Transport)
	})
}

func TestUnixTransportMissingSocketError(t *testing.T) {
	t.Run("formats message", func(t *testing.T) {
		err := &UnixTransportMissingSocketError{}
		assert.Equal(t, "unix socket path is required for unix qrdb transport", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		err := &UnixTransportMissingSocketError{}
		assert.True(t, errors.Is(err, ErrUnixTransportMissingSocket))
	})

	t.Run("errors.As extracts typed error", func(t *testing.T) {
		var target *UnixTransportMissingSocketError
		err := &UnixTransportMissingSocketError{}
		assert.True(t, errors.As(err, &target))
	})
}

func TestShardedClientClosedError(t *testing.T) {
	t.Run("formats message", func(t *testing.T) {
		err := &ShardedClientClosedError{}
		assert.Equal(t, "sharded client is closed", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		err := &ShardedClientClosedError{}
		assert.True(t, errors.Is(err, ErrShardedClientClosed))
	})
}

func TestInsecureBootstrapPasswordError(t *testing.T) {
	t.Run("formats message with reason", func(t *testing.T) {
		err := &InsecureBootstrapPasswordError{Reason: "too short"}
		assert.Equal(t, "insecure bootstrap admin password: too short", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		err := &InsecureBootstrapPasswordError{Reason: "default value"}
		assert.True(t, errors.Is(err, ErrInsecureBootstrapPassword))
	})
}
