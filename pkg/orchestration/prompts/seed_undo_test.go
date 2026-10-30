// Copyright (c) 2026 Jeremy Hahn

package prompts

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// cancelingKVStore ends the caller's context on its second guarded create
// and fails it, as a request whose client went away mid-seed does, and
// records the context every Delete ran on.
type cancelingKVStore struct {
	qrdbsdk.AtomicKVStore
	cancel        context.CancelFunc
	creates       int
	deleteCtxErrs []error
}

func (s *cancelingKVStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	s.creates++
	if s.creates == 2 {
		s.cancel()
		return false, context.Canceled
	}
	return s.AtomicKVStore.PutIfAbsent(ctx, key, value)
}

func (s *cancelingKVStore) Delete(ctx context.Context, key string) error {
	s.deleteCtxErrs = append(s.deleteCtxErrs, ctx.Err())
	return s.AtomicKVStore.Delete(ctx, key)
}

// TestSeeder_UndoOutlivesACanceledSeed proves a seed that fails because its
// context ended still deletes what it wrote: the undo runs detached.
func TestSeeder_UndoOutlivesACanceledSeed(t *testing.T) {
	f := newStoreFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tenantStore := &cancelingKVStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore(), cancel: cancel}
	f.resolver.stores[testTenant] = tenantStore

	err := newTestSeeder(t, f.store).SeedTenant(ctx, testTenant)
	if !errors.Is(err, ErrSeed) || !errors.Is(err, context.Canceled) {
		t.Fatalf("SeedTenant: %v, want a seed error caused by the cancellation", err)
	}
	if len(tenantStore.deleteCtxErrs) != 1 || tenantStore.deleteCtxErrs[0] != nil {
		t.Fatalf("undo deletes ran on contexts with errors %v, want one delete on a live context", tenantStore.deleteCtxErrs)
	}
	if _, err := f.store.Get(context.Background(), charterRef(testTenant, "")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the charter the canceled seed wrote survived: %v", err)
	}
}
