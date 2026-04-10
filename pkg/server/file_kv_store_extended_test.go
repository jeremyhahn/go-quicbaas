//go:build !integration

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// FileKVStore - filenameToKey (not covered in file_kv_store_test.go)
// ---------------------------------------------------------------------------

func TestFileKVStore_FilenameToKey_InvalidHex(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileKVStore(tmpDir)
	require.NoError(t, err)

	t.Run("returns error for invalid hex", func(t *testing.T) {
		_, err := store.filenameToKey("not-valid-hex!")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// FileKVStore - Scan with callback error propagation
// ---------------------------------------------------------------------------

func TestFileKVStore_ScanCallbackError(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileKVStore(tmpDir)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "cb/a", []byte("1")))
	require.NoError(t, store.Put(ctx, "cb/b", []byte("2")))

	t.Run("propagates callback error", func(t *testing.T) {
		callbackErr := &ServerMessageError{Message: "stop scan"}
		err := store.Scan(ctx, "cb/", func(key string, value []byte) error {
			return callbackErr
		})
		require.Error(t, err)
		assert.Equal(t, callbackErr, err)
	})
}
