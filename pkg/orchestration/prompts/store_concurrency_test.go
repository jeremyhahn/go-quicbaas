// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package prompts

import (
	"context"
	"errors"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// pairedReadStore holds every call of the armed verb until a second call of
// it has also been made, so two writers are guaranteed to reach the same
// point before either writes: the interleaving a version guard exists to
// decide. An edit is paired at its read; a create, which reads nothing
// first, at its guarded create.
type pairedReadStore struct {
	qrdbsdk.AtomicKVStore

	mu      sync.Mutex
	armed   string
	waiting int
	release chan struct{}
}

func (s *pairedReadStore) arm(verb string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armed = verb
}

// meet waits for the partner call of verb when verb is armed.
func (s *pairedReadStore) meet(ctx context.Context, verb string) error {
	s.mu.Lock()
	if s.armed != verb {
		s.mu.Unlock()
		return nil
	}
	if s.release == nil {
		s.release = make(chan struct{})
	}
	release := s.release
	s.waiting++
	if s.waiting == 2 {
		close(release)
		s.release, s.waiting = nil, 0
	}
	s.mu.Unlock()
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Get reads first and then waits for its partner, so neither caller can
// write before both have read.
func (s *pairedReadStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.AtomicKVStore.Get(ctx, key)
	if meetErr := s.meet(ctx, "Get"); meetErr != nil {
		return nil, meetErr
	}
	return value, err
}

// PutIfAbsent waits for its partner before it writes, so both creates reach
// the store's guard together.
func (s *pairedReadStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if err := s.meet(ctx, "PutIfAbsent"); err != nil {
		return false, err
	}
	return s.AtomicKVStore.PutIfAbsent(ctx, key, value)
}

// TestStore_ConcurrentSavesAgainstOneVersion races two writers holding the
// same expected version. The store, not the read before it, must decide:
// exactly one wins, the other is told it conflicted, and the history never
// holds the same version twice.
func TestStore_ConcurrentSavesAgainstOneVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing bool
		expected int64
		pair     string
	}{
		{name: "create", expected: 0, pair: "PutIfAbsent"},
		{name: "edit", existing: true, expected: 1, pair: "Get"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newStoreFixture(t)
			ref := charterRef(testTenant, testProject)
			paired := &pairedReadStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore()}
			f.resolver.stores[testTenant] = paired
			if tc.existing {
				if _, err := f.store.Save(ctx, ref, edit("v1 STOP:", 0)); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			paired.arm(tc.pair)
			bodies := []string{"alice STOP:", "bob STOP:"}
			errs := make([]error, len(bodies))
			var wg sync.WaitGroup
			for i, body := range bodies {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, errs[i] = f.store.Save(ctx, ref, edit(body, tc.expected))
				}()
			}
			wg.Wait()
			paired.arm("")

			won, conflicted := 0, 0
			for _, err := range errs {
				switch {
				case err == nil:
					won++
				case errors.Is(err, ErrVersionConflict):
					conflicted++
				default:
					t.Fatalf("Save: unexpected error %v", err)
				}
			}
			if won != 1 || conflicted != 1 {
				t.Fatalf("won=%d conflicted=%d, want exactly one of each (%v)", won, conflicted, errs)
			}
			history, err := f.store.History(ctx, ref)
			if err != nil {
				t.Fatalf("History: %v", err)
			}
			seen := map[int64]bool{}
			for _, revision := range history {
				if seen[revision.Version] {
					t.Fatalf("version %d recorded twice: %+v", revision.Version, history)
				}
				seen[revision.Version] = true
			}
			if want := int(tc.expected) + 1; len(history) != want {
				t.Fatalf("history holds %d revisions, want %d", len(history), want)
			}
		})
	}
}

// plainKVStore exposes only the plain verbs, hiding any guarded ones the
// store underneath has.
type plainKVStore struct {
	qrdbsdk.KVStore
}

// TestStore_RefusesAStoreWithoutGuardedWrites asserts a write fails closed
// over a store that cannot decide a contested write, rather than degrading
// to a read followed by an unguarded put.
func TestStore_RefusesAStoreWithoutGuardedWrites(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	f.resolver.stores[testTenant] = plainKVStore{KVStore: qrdbsdk.NewMemoryKVStore()}
	ref := charterRef(testTenant, testProject)

	_, err := f.store.Save(ctx, ref, edit("v1 STOP:", 0))
	if !errors.Is(err, ErrBackend) || !errors.Is(err, ErrGuardedWriteUnsupported) {
		t.Fatalf("Save over a plain store = %v, want ErrBackend wrapping ErrGuardedWriteUnsupported", err)
	}
	if _, err := f.store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after a refused write = %v, want ErrNotFound", err)
	}
}

// TestStore_UndecodableRecordIsABackendFailure asserts damaged bytes in a
// slot surface as a backend failure on read, list and write, never as an
// empty slot a write could then overwrite.
func TestStore_UndecodableRecordIsABackendFailure(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	ref := charterRef(testTenant, testProject)
	kv, err := f.resolver.Resolve(ctx, testTenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Put(ctx, recordKey(ref), []byte("{damaged")); err != nil {
		t.Fatal(err)
	}

	if _, err := f.store.Get(ctx, ref); !errors.Is(err, ErrBackend) {
		t.Fatalf("Get = %v, want ErrBackend", err)
	}
	if _, err := f.store.List(ctx, testTenant, testProject, ""); !errors.Is(err, ErrBackend) {
		t.Fatalf("List = %v, want ErrBackend", err)
	}
	if _, err := f.store.Save(ctx, ref, edit("v1 STOP:", 0)); !errors.Is(err, ErrBackend) {
		t.Fatalf("Save = %v, want ErrBackend", err)
	}
}
