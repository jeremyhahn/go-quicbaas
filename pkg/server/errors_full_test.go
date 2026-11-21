//go:build !integration

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ShardedClientClosedError - errors.As extraction
// ---------------------------------------------------------------------------

func TestShardedClientClosedError_ErrorsAs(t *testing.T) {
	t.Run("extracts typed error with errors.As", func(t *testing.T) {
		var target *ShardedClientClosedError
		err := &ShardedClientClosedError{}
		require.True(t, errors.As(err, &target))
	})
}

// ---------------------------------------------------------------------------
// QRDBClientNotInitializedError - Unwrap and errors.As
// ---------------------------------------------------------------------------

func TestQRDBClientNotInitializedError_Unwrap(t *testing.T) {
	t.Run("unwraps to sentinel error", func(t *testing.T) {
		err := &QRDBClientNotInitializedError{}
		assert.True(t, errors.Is(err, ErrQRDBClientNotInitialized))
	})

	t.Run("extracts typed error with errors.As", func(t *testing.T) {
		var target *QRDBClientNotInitializedError
		err := &QRDBClientNotInitializedError{}
		require.True(t, errors.As(err, &target))
	})
}

// ---------------------------------------------------------------------------
// ShardClientError - Unwrap, errors.As, and field access
// ---------------------------------------------------------------------------

func TestShardClientError_UnwrapAndAs(t *testing.T) {
	t.Run("unwraps to sentinel error", func(t *testing.T) {
		inner := &ServerMessageError{Message: "dial error"}
		err := &ShardClientError{ShardID: 99, Cause: inner}
		assert.True(t, errors.Is(err, ErrShardClientFailed))
	})

	t.Run("extracts typed error with errors.As", func(t *testing.T) {
		var target *ShardClientError
		inner := &ServerMessageError{Message: "timeout"}
		err := &ShardClientError{ShardID: 7, Cause: inner}
		require.True(t, errors.As(err, &target))
		assert.Equal(t, uint64(7), target.ShardID)
		assert.Equal(t, inner, target.Cause)
	})

	t.Run("error message includes cause", func(t *testing.T) {
		inner := &ServerMessageError{Message: "connection reset"}
		err := &ShardClientError{ShardID: 3, Cause: inner}
		assert.Contains(t, err.Error(), "failed to get qrdb client for shard")
		assert.Contains(t, err.Error(), "connection reset")
	})
}

// ---------------------------------------------------------------------------
// StorageInitError - errors.As extraction and sentinel checks
// ---------------------------------------------------------------------------

func TestStorageInitError_ErrorsAs(t *testing.T) {
	t.Run("extracts typed error with errors.As", func(t *testing.T) {
		var target *StorageInitError
		inner := &ServerMessageError{Message: "permission denied"}
		err := &StorageInitError{Backend: "pebble", Err: inner}
		require.True(t, errors.As(err, &target))
		assert.Equal(t, "pebble", target.Backend)
	})

	t.Run("unwraps to inner error via errors.Is", func(t *testing.T) {
		err := &StorageInitError{Backend: "file", Err: ErrDataDirRequired}
		assert.True(t, errors.Is(err, ErrDataDirRequired))
	})
}

// ---------------------------------------------------------------------------
// DatabaseStartError - errors.As extraction and sentinel checks
// ---------------------------------------------------------------------------

func TestDatabaseStartError_ErrorsAs(t *testing.T) {
	t.Run("extracts typed error with errors.As", func(t *testing.T) {
		var target *DatabaseStartError
		inner := &ServerMessageError{Message: "migration failed"}
		err := &DatabaseStartError{Err: inner}
		require.True(t, errors.As(err, &target))
		assert.Equal(t, inner, target.Err)
	})

	t.Run("unwraps to inner error via errors.Is", func(t *testing.T) {
		err := &DatabaseStartError{Err: ErrServerNotRunning}
		assert.True(t, errors.Is(err, ErrServerNotRunning))
	})
}

// ---------------------------------------------------------------------------
// Sentinel errors - comprehensive check for remaining sentinels
// ---------------------------------------------------------------------------

func TestSentinelErrors_Remaining(t *testing.T) {
	t.Run("ErrShardedClientClosed", func(t *testing.T) {
		assert.Equal(t, "sharded client is closed", ErrShardedClientClosed.Error())
	})

	t.Run("ErrQRDBClientNotInitialized", func(t *testing.T) {
		assert.Equal(t, "qrdb SDK client not initialized", ErrQRDBClientNotInitialized.Error())
	})

	t.Run("ErrShardClientFailed", func(t *testing.T) {
		assert.Equal(t, "failed to get qrdb client for shard", ErrShardClientFailed.Error())
	})

	t.Run("all sentinel errors are distinct", func(t *testing.T) {
		sentinels := []error{
			ErrDataDirRequired,
			ErrServerNotRunning,
			ErrServerAlreadyRunning,
			ErrInvalidServerMode,
			ErrMCPShutdownFailed,
			ErrRAGCloseFailed,
			ErrShardedClientClosed,
			ErrUnsupportedTransport,
			ErrTransportCreationFailed,
			ErrNetworkTransportMissingAddress,
			ErrUnixTransportMissingSocket,
			ErrQRDBClientNotInitialized,
			ErrShardClientFailed,
		}
		seen := make(map[string]struct{}, len(sentinels))
		for _, s := range sentinels {
			msg := s.Error()
			_, dup := seen[msg]
			assert.False(t, dup, "duplicate sentinel message: %s", msg)
			seen[msg] = struct{}{}
		}
	})
}
