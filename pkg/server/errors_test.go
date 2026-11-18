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

	t.Run("ErrShardedClientNoNode", func(t *testing.T) {
		assert.Equal(t, "sharded client has no embedded qrdb node client", ErrShardedClientNoNode.Error())
	})
}

func TestShardedClientNoNodeError(t *testing.T) {
	t.Run("formats message with shard", func(t *testing.T) {
		err := &ShardedClientNoNodeError{ShardID: 3}
		assert.Equal(t, "sharded client has no embedded qrdb node client for shard 3", err.Error())
	})

	t.Run("unwraps to sentinel", func(t *testing.T) {
		err := &ShardedClientNoNodeError{ShardID: 1}
		assert.True(t, errors.Is(err, ErrShardedClientNoNode))
		assert.False(t, errors.Is(err, ErrShardedClientClosed))
	})

	t.Run("errors.As extracts typed error", func(t *testing.T) {
		var target *ShardedClientNoNodeError
		var err error = &ShardedClientNoNodeError{ShardID: 9}
		if assert.True(t, errors.As(err, &target)) {
			assert.Equal(t, uint64(9), target.ShardID)
		}
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
