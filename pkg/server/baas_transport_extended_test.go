//go:build !integration

package server

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ---------------------------------------------------------------------------
// Scan error propagation
// ---------------------------------------------------------------------------

// ScanCallbackError is a typed error returned by the scan callback.
type ScanCallbackError struct {
	Key string
}

// Error implements the error interface.
func (e *ScanCallbackError) Error() string {
	return "scan callback error for key: " + e.Key
}

func TestBaasTransport_Scan_ErrorPropagation(t *testing.T) {
	store := NewMemoryKVStore()
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "err/a", []byte("1")))
	require.NoError(t, store.Put(ctx, "err/b", []byte("2")))

	t.Run("scan propagates store scan error", func(t *testing.T) {
		errStore := &errorOnScanKVStore{
			KVStore: store,
			scanErr: &ScanCallbackError{Key: "injected"},
		}
		errBt := newBaasTransport(errStore, nil, nil, nil, nil)

		result, err := errBt.Scan(ctx, "err/")
		require.Error(t, err)
		assert.Nil(t, result)
		var scanErr *ScanCallbackError
		assert.True(t, errors.As(err, &scanErr))
	})
}

// errorOnScanKVStore wraps a KVStore and always returns an error from Scan.
type errorOnScanKVStore struct {
	interfaces.KVStore
	scanErr error
}

// Scan overrides the embedded KVStore's Scan to return an error.
func (e *errorOnScanKVStore) Scan(_ context.Context, _ string, _ func(string, []byte) error) error {
	return e.scanErr
}

// ---------------------------------------------------------------------------
// baasTransportStore: storeForShard error path
// ---------------------------------------------------------------------------

func TestBaasTransportStore_StoreForShard_CreateStoreError(t *testing.T) {
	t.Run("returns error when createStore fails (invalid pebble path)", func(t *testing.T) {
		s := newBaasTransportStore(&config.StorageConfig{
			Backend: config.StorageBackendPebble,
			DataDir: "/dev/null/impossible-path",
		})

		store, err := s.storeForShard(99)
		require.Error(t, err)
		assert.Nil(t, store)

		var storeErr *BaasTransportStoreError
		assert.True(t, errors.As(err, &storeErr))
		assert.Equal(t, uint64(99), storeErr.ShardID)
		assert.Equal(t, "pebble", storeErr.Backend)
	})
}

// ---------------------------------------------------------------------------
// baasTransportStore: createStore error paths
// ---------------------------------------------------------------------------

func TestBaasTransportStore_CreateStore_PebbleError(t *testing.T) {
	t.Run("returns BaasTransportStoreError for invalid pebble path", func(t *testing.T) {
		s := newBaasTransportStore(&config.StorageConfig{
			Backend: config.StorageBackendPebble,
			DataDir: "/dev/null/invalid",
		})

		store, err := s.createStore(42)
		require.Error(t, err)
		assert.Nil(t, store)

		var storeErr *BaasTransportStoreError
		require.True(t, errors.As(err, &storeErr))
		assert.Equal(t, uint64(42), storeErr.ShardID)
		assert.Equal(t, "pebble", storeErr.Backend)
		assert.NotNil(t, storeErr.Unwrap())
	})
}

func TestBaasTransportStore_CreateStore_FileBackendMkdirError(t *testing.T) {
	t.Run("returns BaasTransportStoreError when MkdirAll fails", func(t *testing.T) {
		s := newBaasTransportStore(&config.StorageConfig{
			Backend: config.StorageBackendFile,
			DataDir: "/dev/null/impossible",
		})

		store, err := s.createStore(7)
		require.Error(t, err)
		assert.Nil(t, store)

		var storeErr *BaasTransportStoreError
		require.True(t, errors.As(err, &storeErr))
		assert.Equal(t, uint64(7), storeErr.ShardID)
		assert.Equal(t, "file", storeErr.Backend)
	})
}

func TestBaasTransportStore_CreateStore_FileBackendSuccess(t *testing.T) {
	t.Run("creates file store with valid directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		s := newBaasTransportStore(&config.StorageConfig{
			Backend: config.StorageBackendFile,
			DataDir: tmpDir,
		})

		store, err := s.createStore(3)
		require.NoError(t, err)
		assert.NotNil(t, store)

		// Verify the store works
		ctx := context.Background()
		require.NoError(t, store.Put(ctx, "test-key", []byte("test-val")))
		val, getErr := store.Get(ctx, "test-key")
		require.NoError(t, getErr)
		assert.Equal(t, []byte("test-val"), val)
	})
}

func TestBaasTransportStore_CreateStore_DefaultMemory(t *testing.T) {
	t.Run("default backend creates MemoryKVStore", func(t *testing.T) {
		s := newBaasTransportStore(nil)

		store, err := s.createStore(1)
		require.NoError(t, err)
		assert.NotNil(t, store)
		assert.IsType(t, &MemoryKVStore{}, store)
	})
}

// ---------------------------------------------------------------------------
// baasTransportStore: transportForShard vector/timeseries injection
// ---------------------------------------------------------------------------

func TestBaasTransportStore_TransportForShard_VectorInjection(t *testing.T) {
	t.Run("injects vector storage for matching shard", func(t *testing.T) {
		vs := &mockVectorStorage{}
		s := newBaasTransportStore(nil)
		s.vectorStorage = vs
		s.vectorShardID = 10

		tr, err := s.transportForShard(10)
		require.NoError(t, err)
		assert.NotNil(t, tr)
		assert.Same(t, vs, tr.vectorStorage)
	})

	t.Run("does not inject vector storage for non-matching shard", func(t *testing.T) {
		vs := &mockVectorStorage{}
		s := newBaasTransportStore(nil)
		s.vectorStorage = vs
		s.vectorShardID = 10

		tr, err := s.transportForShard(20)
		require.NoError(t, err)
		assert.NotNil(t, tr)
		assert.Nil(t, tr.vectorStorage)
	})

	t.Run("nil vector storage not injected even for matching shard", func(t *testing.T) {
		s := newBaasTransportStore(nil)
		s.vectorShardID = 10

		tr, err := s.transportForShard(10)
		require.NoError(t, err)
		assert.NotNil(t, tr)
		assert.Nil(t, tr.vectorStorage)
	})
}

func TestBaasTransportStore_TransportForShard_TimeSeriesInjection(t *testing.T) {
	t.Run("injects timeseries storage for matching shard", func(t *testing.T) {
		ts := &mockTimeSeriesStorage{}
		s := newBaasTransportStore(nil)
		s.timeSeriesStorage = ts
		s.timeSeriesShardID = 15

		tr, err := s.transportForShard(15)
		require.NoError(t, err)
		assert.NotNil(t, tr)
		assert.Same(t, ts, tr.timeSeriesStorage)
	})

	t.Run("does not inject timeseries storage for non-matching shard", func(t *testing.T) {
		ts := &mockTimeSeriesStorage{}
		s := newBaasTransportStore(nil)
		s.timeSeriesStorage = ts
		s.timeSeriesShardID = 15

		tr, err := s.transportForShard(25)
		require.NoError(t, err)
		assert.NotNil(t, tr)
		assert.Nil(t, tr.timeSeriesStorage)
	})
}

func TestBaasTransportStore_TransportForShard_BothInjected(t *testing.T) {
	t.Run("injects both when same shard", func(t *testing.T) {
		vs := &mockVectorStorage{}
		ts := &mockTimeSeriesStorage{}
		s := newBaasTransportStore(nil)
		s.vectorStorage = vs
		s.vectorShardID = 5
		s.timeSeriesStorage = ts
		s.timeSeriesShardID = 5

		tr, err := s.transportForShard(5)
		require.NoError(t, err)
		assert.Same(t, vs, tr.vectorStorage)
		assert.Same(t, ts, tr.timeSeriesStorage)
	})
}

func TestBaasTransportStore_TransportForShard_StoreError(t *testing.T) {
	t.Run("returns error when storeForShard fails", func(t *testing.T) {
		s := newBaasTransportStore(&config.StorageConfig{
			Backend: config.StorageBackendPebble,
			DataDir: "/dev/null/impossible",
		})

		tr, err := s.transportForShard(1)
		require.Error(t, err)
		assert.Nil(t, tr)

		var storeErr *BaasTransportStoreError
		assert.True(t, errors.As(err, &storeErr))
	})
}

// ---------------------------------------------------------------------------
// baasTransportStore: Close error path
// ---------------------------------------------------------------------------

// failingCloseKVStore is a KVStore that returns an error on Close.
type failingCloseKVStore struct {
	MemoryKVStore
	closeErr error
}

// Close returns the configured error.
func (f *failingCloseKVStore) Close() error {
	return f.closeErr
}

// FailingCloseError is a typed error for close failures.
type FailingCloseError struct {
	Msg string
}

// Error implements the error interface.
func (e *FailingCloseError) Error() string {
	return "close failed: " + e.Msg
}

func TestBaasTransportStore_Close_Error(t *testing.T) {
	t.Run("returns first error from closer", func(t *testing.T) {
		s := newBaasTransportStore(nil)
		closeErr := &FailingCloseError{Msg: "disk full"}
		failStore := &failingCloseKVStore{closeErr: closeErr}
		s.stores.Store(uint64(1), failStore)

		err := s.Close()
		require.Error(t, err)
		var fcErr *FailingCloseError
		assert.True(t, errors.As(err, &fcErr))
		assert.Equal(t, "disk full", fcErr.Msg)
	})

	t.Run("captures first error and continues closing others", func(t *testing.T) {
		s := newBaasTransportStore(nil)
		err1 := &FailingCloseError{Msg: "first"}
		err2 := &FailingCloseError{Msg: "second"}
		s.stores.Store(uint64(1), &failingCloseKVStore{closeErr: err1})
		s.stores.Store(uint64(2), &failingCloseKVStore{closeErr: err2})

		err := s.Close()
		require.Error(t, err)

		// Only the first error is returned, but both stores are deleted
		var fcErr *FailingCloseError
		assert.True(t, errors.As(err, &fcErr))

		// Verify all stores were deleted from the map
		count := 0
		s.stores.Range(func(_, _ any) bool {
			count++
			return true
		})
		assert.Equal(t, 0, count)
	})

	t.Run("close succeeds with mix of closeable and non-closeable stores", func(t *testing.T) {
		s := newBaasTransportStore(nil)
		// MemoryKVStore does not implement Close, so it's skipped
		s.stores.Store(uint64(1), NewMemoryKVStore())
		// failingCloseKVStore implements Close and succeeds
		s.stores.Store(uint64(2), &failingCloseKVStore{closeErr: nil})

		err := s.Close()
		require.NoError(t, err)

		count := 0
		s.stores.Range(func(_, _ any) bool {
			count++
			return true
		})
		assert.Equal(t, 0, count)
	})
}

// ---------------------------------------------------------------------------
// baasTransportStore: storeForShard race condition path
// ---------------------------------------------------------------------------

func TestBaasTransportStore_StoreForShard_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent storeForShard calls return same store", func(t *testing.T) {
		s := newBaasTransportStore(nil)
		const goroutines = 10

		stores := make([]interfaces.KVStore, goroutines)
		errs := make([]error, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)

		// Signal channel to start all goroutines simultaneously
		start := make(chan struct{})

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				<-start
				stores[idx], errs[idx] = s.storeForShard(1)
			}(i)
		}

		close(start)
		wg.Wait()

		// All should succeed
		for i := 0; i < goroutines; i++ {
			require.NoError(t, errs[i])
			assert.NotNil(t, stores[i])
		}

		// All should return the same store instance
		for i := 1; i < goroutines; i++ {
			assert.Same(t, stores[0], stores[i])
		}
	})
}

// ---------------------------------------------------------------------------
// baasTransportStore: dbService injection
// ---------------------------------------------------------------------------

func TestBaasTransportStore_TransportForShard_DBServiceInjection(t *testing.T) {
	t.Run("dbService is passed through to transport", func(t *testing.T) {
		s := newBaasTransportStore(nil)
		// dbService is nil by default, verify the transport gets nil
		tr, err := s.transportForShard(1)
		require.NoError(t, err)
		assert.Nil(t, tr.dbService)
	})
}
