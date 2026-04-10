//go:build !integration

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// StorageInitError - not covered in errors_test.go
// ---------------------------------------------------------------------------

func TestStorageInitError_Format(t *testing.T) {
	t.Run("formats message with backend and error", func(t *testing.T) {
		inner := &ServerMessageError{Message: "open failed"}
		err := &StorageInitError{Backend: "pebble", Err: inner}
		assert.Contains(t, err.Error(), "storage init (pebble)")
		assert.Contains(t, err.Error(), "open failed")
	})

	t.Run("unwrap returns inner error", func(t *testing.T) {
		inner := &ServerMessageError{Message: "cause"}
		err := &StorageInitError{Backend: "file", Err: inner}
		assert.Equal(t, inner, err.Unwrap())
	})
}

// ---------------------------------------------------------------------------
// DatabaseStartError - not covered in errors_test.go
// ---------------------------------------------------------------------------

func TestDatabaseStartError_Format(t *testing.T) {
	t.Run("formats message with error", func(t *testing.T) {
		inner := &ServerMessageError{Message: "schema migration failed"}
		err := &DatabaseStartError{Err: inner}
		assert.Contains(t, err.Error(), "database subsystem init")
		assert.Contains(t, err.Error(), "schema migration failed")
	})

	t.Run("unwrap returns inner error", func(t *testing.T) {
		inner := &ServerMessageError{Message: "cause"}
		err := &DatabaseStartError{Err: inner}
		assert.Equal(t, inner, err.Unwrap())
	})
}

// ---------------------------------------------------------------------------
// QRDBClientNotInitializedError - not covered in errors_test.go
// ---------------------------------------------------------------------------

func TestQRDBClientNotInitializedError_Format(t *testing.T) {
	t.Run("returns expected message", func(t *testing.T) {
		err := &QRDBClientNotInitializedError{}
		assert.Contains(t, err.Error(), "qrdb SDK client")
	})
}

// ---------------------------------------------------------------------------
// ShardClientError - not covered in errors_test.go
// ---------------------------------------------------------------------------

func TestShardClientError_Format(t *testing.T) {
	t.Run("includes cause message", func(t *testing.T) {
		inner := &ServerMessageError{Message: "shard not found"}
		err := &ShardClientError{ShardID: 5, Cause: inner}
		assert.Contains(t, err.Error(), "shard not found")
	})
}
