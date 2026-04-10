//go:build !integration && !iam

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ===========================================================================
// TestableApp stub tests (testable_stub.go)
// Unique names to avoid collision with server_unit_test.go's TestTestableApp.
// ===========================================================================

func TestNewTestableApp_WrapServer(t *testing.T) {
	t.Run("wraps server and returns it", func(t *testing.T) {
		srv := withTempServer(t)
		app := NewTestableApp(srv)
		require.NotNil(t, app)
		assert.Equal(t, srv, app.GetServer())
	})

	t.Run("wraps nil server without panic", func(t *testing.T) {
		app := NewTestableApp(nil)
		require.NotNil(t, app)
		assert.Nil(t, app.GetServer())
	})
}

func TestTestableApp_WaitForLeader_ReturnsNil(t *testing.T) {
	t.Run("returns nil for any shard", func(t *testing.T) {
		app := NewTestableApp(nil)
		assert.NoError(t, app.WaitForLeader(1))
		assert.NoError(t, app.WaitForLeader(999))
	})
}

func TestTestableApp_GetLeaderID_ReturnsZero(t *testing.T) {
	t.Run("returns zero and nil for any shard", func(t *testing.T) {
		app := NewTestableApp(nil)
		id, err := app.GetLeaderID(1)
		assert.NoError(t, err)
		assert.Equal(t, uint64(0), id)
	})
}

func TestTestableApp_IsLeader_ReturnsFalse(t *testing.T) {
	t.Run("returns false for any shard", func(t *testing.T) {
		app := NewTestableApp(nil)
		assert.False(t, app.IsLeader(1))
		assert.False(t, app.IsLeader(0))
	})
}

func TestTestableApp_GetNodeHostInfo_ReturnsNil(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		app := NewTestableApp(nil)
		assert.Nil(t, app.GetNodeHostInfo())
	})
}

func TestTestableApp_WaitForShards_ReturnsNil(t *testing.T) {
	t.Run("returns nil for any shard list", func(t *testing.T) {
		app := NewTestableApp(nil)
		assert.NoError(t, app.WaitForShards([]uint64{1, 2, 3}))
		assert.NoError(t, app.WaitForShards(nil))
	})
}

// ===========================================================================
// FileBackend stub tests (file_backend_stub.go)
// ===========================================================================

func TestNewFileBackend_ReturnsNil(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		result := NewFileBackend(nil, 1, nil)
		assert.Nil(t, result)
	})
}

func TestFileBackend_Close_Noop(t *testing.T) {
	t.Run("returns nil on zero-value struct", func(t *testing.T) {
		fb := &FileBackend{}
		assert.NoError(t, fb.Close())
	})
}

// ===========================================================================
// LocalBackend stub tests (local_backend_stub.go)
// ===========================================================================

func TestNewLocalBackend_ReturnsNil(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		result := NewLocalBackend(1, nil)
		assert.Nil(t, result)
	})
}

func TestLocalBackend_Close_Noop(t *testing.T) {
	t.Run("returns nil on zero-value struct", func(t *testing.T) {
		lb := &LocalBackend{}
		assert.NoError(t, lb.Close())
	})
}

// ===========================================================================
// MemoryBackend stub tests (memory_backend_stub.go)
// ===========================================================================

func TestNewMemoryBackend_ReturnsNil(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		result := NewMemoryBackend(nil, 1, nil)
		assert.Nil(t, result)
	})
}

func TestMemoryBackend_Close_Noop(t *testing.T) {
	t.Run("returns nil on zero-value struct", func(t *testing.T) {
		mb := &MemoryBackend{}
		assert.NoError(t, mb.Close())
	})
}

// ===========================================================================
// lifecycle_raft_stub.go: validateRouting, kvShards, stopBootstrap, stopDevCluster
// ===========================================================================

func TestValidateRouting_EmptyPins_NoError(t *testing.T) {
	t.Run("nil pins returns nil", func(t *testing.T) {
		assert.NoError(t, validateRouting([]uint64{1, 2}, nil))
	})

	t.Run("empty pins map returns nil", func(t *testing.T) {
		assert.NoError(t, validateRouting([]uint64{1}, map[string]uint64{}))
	})
}

func TestValidateRouting_InvalidPin_ReturnsServerMessageError(t *testing.T) {
	t.Run("pin to non-existent shard returns typed error", func(t *testing.T) {
		err := validateRouting([]uint64{1, 2}, map[string]uint64{"users": 5})
		require.Error(t, err)
		var sme *ServerMessageError
		require.True(t, errors.As(err, &sme))
		assert.Contains(t, sme.Message, "users")
		assert.Contains(t, sme.Message, "5")
	})
}

func TestKvShards_EdgeCases(t *testing.T) {
	t.Run("empty shards with excludes returns default", func(t *testing.T) {
		assert.Equal(t, []uint64{1}, kvShards(nil, 2, 3))
	})

	t.Run("zero-value excludes are ignored", func(t *testing.T) {
		assert.Equal(t, []uint64{1, 2}, kvShards([]uint64{1, 2}, 0))
	})

	t.Run("all excluded returns default shard 1", func(t *testing.T) {
		assert.Equal(t, []uint64{1}, kvShards([]uint64{2, 3}, 2, 3))
	})
}

func TestServer_StopBootstrap_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.stopBootstrap())
	})
}

func TestServer_StopDevCluster_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.stopDevCluster(context.Background()))
	})
}

// ===========================================================================
// lifecycle_vector_stub.go: initVectorStorage, startVector
// ===========================================================================

func TestServer_InitVectorStorage_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.initVectorStorage())
	})
}

func TestServer_StartVector_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.startVector())
	})
}

// ===========================================================================
// lifecycle_rag_noop.go: startRAG, stopRAG, startOllama, stopOllama
// ===========================================================================

func TestServer_StartRAG_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.startRAG())
	})
}

func TestServer_StopRAG_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.stopRAG())
	})
}

func TestServer_StartOllama_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.startOllama())
	})
}

func TestServer_StopOllama_Noop(t *testing.T) {
	t.Run("returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		assert.NoError(t, srv.stopOllama())
	})
}

// ===========================================================================
// lifecycle_embedding_noop.go / lifecycle_rag_noop.go error sentinels
// ===========================================================================

func TestErrEmbeddingNotCompiled_Message(t *testing.T) {
	t.Run("has correct message", func(t *testing.T) {
		assert.Contains(t, ErrEmbeddingNotCompiled.Error(), "embedding support not compiled")
	})
}

func TestErrRAGNotCompiled_Message(t *testing.T) {
	t.Run("has correct message", func(t *testing.T) {
		assert.Contains(t, ErrRAGNotCompiled.Error(), "rag support not compiled")
	})
}

// ===========================================================================
// MemoryKVStore - defensive copy and callback error tests
// Unique names to avoid collision with server_unit_test.go
// ===========================================================================

func TestMemoryKVStore_DefensiveCopyOnPut(t *testing.T) {
	store := NewMemoryKVStore()
	ctx := context.Background()

	t.Run("input mutation after Put does not affect stored value", func(t *testing.T) {
		input := []byte("original")
		require.NoError(t, store.Put(ctx, "def-copy", input))
		input[0] = 'X'

		val, err := store.Get(ctx, "def-copy")
		require.NoError(t, err)
		assert.Equal(t, byte('o'), val[0])
	})
}

func TestMemoryKVStore_DefensiveCopyOnGet(t *testing.T) {
	store := NewMemoryKVStore()
	ctx := context.Background()

	t.Run("mutating returned value does not affect stored value", func(t *testing.T) {
		require.NoError(t, store.Put(ctx, "get-copy", []byte("original")))
		val1, err := store.Get(ctx, "get-copy")
		require.NoError(t, err)
		val1[0] = 'X'

		val2, err := store.Get(ctx, "get-copy")
		require.NoError(t, err)
		assert.Equal(t, byte('o'), val2[0])
	})
}

func TestMemoryKVStore_ScanCallbackError(t *testing.T) {
	store := NewMemoryKVStore()
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "cb/a", []byte("1")))
	require.NoError(t, store.Put(ctx, "cb/b", []byte("2")))

	t.Run("propagates callback error and stops iteration", func(t *testing.T) {
		cbErr := &ServerMessageError{Message: "halt"}
		err := store.Scan(ctx, "cb/", func(key string, value []byte) error {
			return cbErr
		})
		assert.Equal(t, cbErr, err)
	})
}

func TestMemoryKVStore_GetNotFound_DragonError(t *testing.T) {
	store := NewMemoryKVStore()
	ctx := context.Background()

	t.Run("returns DragonError with ErrNotFound code", func(t *testing.T) {
		_, err := store.Get(ctx, "no-key")
		require.Error(t, err)
		var de *dberrors.DragonError
		require.True(t, errors.As(err, &de))
		assert.Equal(t, dberrors.ErrNotFound, de.Code)
		assert.Equal(t, "kv.get", de.Op)
	})
}
