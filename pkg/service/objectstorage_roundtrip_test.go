package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// putThenGet stores value under key through the service and returns the
// bytes a later GetObject reads back.
func putThenGet(t *testing.T, svc ObjectStorageService, ctx context.Context, key string, value []byte) []byte {
	t.Helper()

	_, err := svc.PutObject(ctx, &PutObjectRequest{Key: key, Value: value, TenantID: "tenant1"})
	require.NoError(t, err)

	resp, err := svc.GetObject(ctx, &GetObjectRequest{Key: key})
	require.NoError(t, err)
	return resp.Value
}

func TestObjectStorage_OverwriteReadsNewValue(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant1")

	assert.Equal(t, []byte("first"), putThenGet(t, svc, ctx, "overwrite:key", []byte("first")))
	assert.Equal(t, []byte("second"), putThenGet(t, svc, ctx, "overwrite:key", []byte("second")))
}

// backendObjectCount is the number of objects the mock backend holds.
func backendObjectCount(s *objstoreMockStorage) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.objects)
}

// TestObjectStorage_OverwriteRemovesSupersededBackendObject checks that
// overwriting a key, sequentially or from concurrent writers, leaves one
// backend object per key.
func TestObjectStorage_OverwriteRemovesSupersededBackendObject(t *testing.T) {
	svc, _, _, storage := createObjstoreTestService(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant1")

	for i := 0; i < 3; i++ {
		assert.Equal(t, []byte("same"), putThenGet(t, svc, ctx, "overwrite:key", []byte("same")))
	}
	assert.Equal(t, 1, backendObjectCount(storage))

	const writers = 16
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			_, err := svc.PutObject(ctx, &PutObjectRequest{Key: "race:key", Value: []byte(strconv.Itoa(w)), TenantID: "tenant1"})
			assert.NoError(t, err)
		}(w)
	}
	wg.Wait()
	assert.Equal(t, 2, backendObjectCount(storage))
}

// TestObjectStorage_OverwriteWaitsForReader checks that an overwrite of a
// key waits for a reader that has resolved the key's current backend
// object, rather than removing that object under it.
func TestObjectStorage_OverwriteWaitsForReader(t *testing.T) {
	svc, _, _, storage := createObjstoreTestService(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant1")

	_, err := svc.PutObject(ctx, &PutObjectRequest{Key: "reader:key", Value: []byte("old"), TenantID: "tenant1"})
	require.NoError(t, err)

	// The next backend read signals entered and waits for release; later
	// reads go straight through.
	var block atomic.Bool
	block.Store(true)
	entered := make(chan struct{})
	release := make(chan struct{})
	storage.getWithContextFn = func(_ context.Context, key string) (io.ReadCloser, error) {
		if block.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
		storage.mu.RLock()
		defer storage.mu.RUnlock()
		data, ok := storage.objects[key]
		if !ok {
			return nil, errors.New("not found")
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	type getResult struct {
		value []byte
		err   error
	}
	readDone := make(chan getResult, 1)
	go func() {
		resp, err := svc.GetObject(ctx, &GetObjectRequest{Key: "reader:key"})
		if err != nil {
			readDone <- getResult{err: err}
			return
		}
		readDone <- getResult{value: resp.Value}
	}()
	<-entered

	putDone := make(chan struct{})
	var putErr error
	go func() {
		defer close(putDone)
		_, putErr = svc.PutObject(ctx, &PutObjectRequest{Key: "reader:key", Value: []byte("new"), TenantID: "tenant1"})
	}()

	// The timeout only shows the writer is still blocked; it is not used
	// to order anything.
	select {
	case <-putDone:
		t.Fatal("overwrite finished while a reader of the replaced object was in flight")
	case <-time.After(100 * time.Millisecond):
	}
	assert.Equal(t, 1, backendObjectCount(storage), "the replaced object is still stored")

	close(release)
	got := <-readDone
	require.NoError(t, got.err)
	assert.Equal(t, []byte("old"), got.value)

	<-putDone
	require.NoError(t, putErr)
	resp, err := svc.GetObject(ctx, &GetObjectRequest{Key: "reader:key"})
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), resp.Value)
	assert.Equal(t, 1, backendObjectCount(storage))
}

func TestObjectStorage_SpecialCharacterKeyRoundTrips(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant1")

	key := "special:key with spaces/and-dashes_and.dots@symbols"
	assert.Equal(t, []byte("special"), putThenGet(t, svc, ctx, key, []byte("special")))
}

func TestObjectStorage_GetObjectNonexistentKeyFails(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant1")

	_, err := svc.GetObject(ctx, &GetObjectRequest{Key: "missing:key"})
	require.Error(t, err)
}

func TestObjectStorage_ConcurrentPutGet(t *testing.T) {
	svc, _, _, _ := createObjstoreTestService(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant1")

	const workers = 20
	const perWorker = 10

	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker*2)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := "concurrent:" + strconv.Itoa(w) + ":" + strconv.Itoa(i)
				value := []byte(key)
				if _, err := svc.PutObject(ctx, &PutObjectRequest{Key: key, Value: value, TenantID: "tenant1"}); err != nil {
					errs <- err
					continue
				}
				resp, err := svc.GetObject(ctx, &GetObjectRequest{Key: key})
				if err != nil {
					errs <- err
					continue
				}
				if string(resp.Value) != key {
					errs <- valueMismatchError{key: key}
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// valueMismatchError reports a GetObject that returned bytes other than those
// its PutObject stored.
type valueMismatchError struct{ key string }

func (e valueMismatchError) Error() string { return "value mismatch for key " + e.key }
