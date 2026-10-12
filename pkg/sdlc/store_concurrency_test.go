package sdlc

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// contenders is how many writers race against one read of a record.
const contenders = 8

// gatedReadStore holds the reads of an armed window until all of them have
// arrived, so every contender provably reads the same record before any of
// them writes. Once the window releases, later reads pass straight through:
// a writer that re-reads after losing is not held a second time.
type gatedReadStore struct {
	qrdbsdk.AtomicKVStore

	mu      sync.Mutex
	pending int
	release chan struct{}
}

// arm opens a window that gathers the next n reads.
func (s *gatedReadStore) arm(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = n
	s.release = make(chan struct{})
}

// Get reads the key, then waits until the window's last read has arrived.
func (s *gatedReadStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.AtomicKVStore.Get(ctx, key)
	s.mu.Lock()
	if s.pending == 0 {
		s.mu.Unlock()
		return value, err
	}
	release := s.release
	s.pending--
	if s.pending == 0 {
		close(release)
	}
	s.mu.Unlock()
	select {
	case <-release:
		return value, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// gatedStore installs a gated store as testTenant's and returns both.
func gatedStore(t *testing.T) (*Store, *gatedReadStore) {
	t.Helper()
	resolver := newMemoryResolver()
	gated := &gatedReadStore{AtomicKVStore: qrdbsdk.NewMemoryKVStore()}
	resolver.stores[testTenant] = gated
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	return store, gated
}

// guardedWrite is one record kind's guarded write, raced by contenders that
// all hold the same read of the record.
type guardedWrite struct {
	name string
	// seed writes the record every contender reads; nil races a create.
	seed func(ctx context.Context, store *Store) error
	// write makes contender i's write, marked with mark.
	write func(ctx context.Context, store *Store, mark string) error
	// stored reads back the mark of the write the store kept.
	stored func(ctx context.Context, store *Store) (string, error)
}

func guardedWrites() []guardedWrite {
	var delivery *Delivery
	var binding *ProjectBinding
	var entry *TrainEntry
	var ledger *TrainLedger
	newEntry := func() *TrainEntry {
		return &TrainEntry{ProjectID: testProject, TaskID: testTask, PullRequest: 5, HeadSHA: "seed",
			Target: "main", Branch: "issue/5", State: TrainEntryQueued}
	}
	storedEntry := func(ctx context.Context, store *Store) (string, error) {
		got, err := store.GetTrainEntry(ctx, testTenant, testProject, testTask)
		if err != nil {
			return "", err
		}
		return got.HeadSHA, nil
	}
	storedLedger := func(ctx context.Context, store *Store) (string, error) {
		got, err := store.GetTrainLedger(ctx, testTenant, testProject)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(got.Next), nil
	}
	return []guardedWrite{
		{
			name: "UpdateDelivery",
			seed: func(ctx context.Context, store *Store) error {
				d := &Delivery{TaskID: testTask, ProjectID: testProject, Attempt: 1, Branch: "issue/1-x", Base: "main"}
				if err := store.PutDelivery(ctx, testTenant, d); err != nil {
					return err
				}
				delivery = d
				return nil
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				mine := *delivery
				mine.HeadSHA = mark
				return store.UpdateDelivery(ctx, testTenant, &mine)
			},
			stored: func(ctx context.Context, store *Store) (string, error) {
				got, err := store.GetDelivery(ctx, testTenant, testTask)
				if err != nil {
					return "", err
				}
				return got.HeadSHA, nil
			},
		},
		{
			name: "UpdateBinding",
			seed: func(ctx context.Context, store *Store) error {
				b := testBinding()
				if err := store.PutBinding(ctx, testTenant, b); err != nil {
					return err
				}
				binding = b
				return nil
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				mine := *binding
				mine.WorkerPromptAppend = mark
				return store.UpdateBinding(ctx, testTenant, &mine)
			},
			stored: func(ctx context.Context, store *Store) (string, error) {
				got, err := store.GetBinding(ctx, testTenant, testProject)
				if err != nil {
					return "", err
				}
				return got.WorkerPromptAppend, nil
			},
		},
		{
			name: "SaveTrainEntry create",
			write: func(ctx context.Context, store *Store, mark string) error {
				mine := newEntry()
				mine.HeadSHA = mark
				return store.SaveTrainEntry(ctx, testTenant, mine)
			},
			stored: storedEntry,
		},
		{
			name: "SaveTrainEntry update",
			seed: func(ctx context.Context, store *Store) error {
				e := newEntry()
				if err := store.SaveTrainEntry(ctx, testTenant, e); err != nil {
					return err
				}
				entry = e
				return nil
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				mine := *entry
				mine.HeadSHA = mark
				return store.SaveTrainEntry(ctx, testTenant, &mine)
			},
			stored: storedEntry,
		},
		{
			name: "SaveTrainLedger create",
			write: func(ctx context.Context, store *Store, mark string) error {
				next, err := strconv.Atoi(mark)
				if err != nil {
					return err
				}
				return store.SaveTrainLedger(ctx, testTenant, &TrainLedger{ProjectID: testProject, Next: next})
			},
			stored: storedLedger,
		},
		{
			name: "SaveTrainLedger update",
			seed: func(ctx context.Context, store *Store) error {
				l := &TrainLedger{ProjectID: testProject, Next: 1}
				if err := store.SaveTrainLedger(ctx, testTenant, l); err != nil {
					return err
				}
				ledger = l
				return nil
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				next, err := strconv.Atoi(mark)
				if err != nil {
					return err
				}
				mine := *ledger
				mine.Next = next
				return store.SaveTrainLedger(ctx, testTenant, &mine)
			},
			stored: storedLedger,
		},
	}
}

// TestStore_ConcurrentGuardedWritesAgainstOneRead races contenders that all
// read the same version of a record. The store must apply exactly one write
// and refuse every other with a version conflict; a check that is not one
// atomic step with the write lets several pass and silently loses all but
// the last.
func TestStore_ConcurrentGuardedWritesAgainstOneRead(t *testing.T) {
	for _, tc := range guardedWrites() {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			store, gated := gatedStore(t)
			if tc.seed != nil {
				if err := tc.seed(ctx, store); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			gated.arm(contenders)
			errs := make([]error, contenders)
			var wg sync.WaitGroup
			for i := range contenders {
				wg.Add(1)
				go func() {
					defer wg.Done()
					errs[i] = tc.write(ctx, store, strconv.Itoa(i+2))
				}()
			}
			wg.Wait()

			winner := -1
			for i, err := range errs {
				switch {
				case err == nil && winner < 0:
					winner = i
				case err == nil:
					t.Fatalf("contenders %d and %d both passed the guard (%v)", winner, i, errs)
				case !errors.Is(err, ErrVersionConflict):
					t.Fatalf("contender %d: %v, want a version conflict", i, err)
				}
			}
			if winner < 0 {
				t.Fatalf("no contender won: %v", errs)
			}
			got, err := tc.stored(ctx, store)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if want := strconv.Itoa(winner + 2); got != want {
				t.Fatalf("stored write %q, want the winner's %q", got, want)
			}
		})
	}
}

// TestStore_ConcurrentPutDeliveryCountsEveryReplacement races unconditional
// replacements. Each must land, and each must take its own version: two
// that both claim the version after the one they read would let a reader of
// the first pass the guard of the second.
func TestStore_ConcurrentPutDeliveryCountsEveryReplacement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, gated := gatedStore(t)

	gated.arm(contenders)
	versions := make([]int64, contenders)
	errs := make([]error, contenders)
	var wg sync.WaitGroup
	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := &Delivery{TaskID: testTask, ProjectID: testProject, Attempt: i + 1, Branch: "issue/1-x", Base: "main"}
			errs[i] = store.PutDelivery(ctx, testTenant, d)
			versions[i] = d.Version
		}()
	}
	wg.Wait()

	seen := map[int64]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("PutDelivery %d: %v", i, err)
		}
		if versions[i] < 1 || versions[i] > contenders || seen[versions[i]] {
			t.Fatalf("versions %v, want each of 1..%d exactly once", versions, contenders)
		}
		seen[versions[i]] = true
	}
	got, err := store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != contenders {
		t.Fatalf("stored version %d, want %d", got.Version, contenders)
	}
}

// TestStore_UpdateDeliveryRefusesAWrongVersion refuses a version that is not
// the stored one, and a non-zero version for a record that does not exist,
// leaving the store as it was.
func TestStore_UpdateDeliveryRefusesAWrongVersion(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	d := &Delivery{TaskID: testTask, ProjectID: testProject, Attempt: 1, Branch: "issue/1-x", Base: "main"}
	if err := store.PutDelivery(ctx, testTenant, d); err != nil {
		t.Fatal(err)
	}

	ahead := *d
	ahead.Version = 5
	ahead.HeadSHA = "ahead"
	var conflict *VersionConflictError
	if err := store.UpdateDelivery(ctx, testTenant, &ahead); !errors.As(err, &conflict) || conflict.Expected != 5 {
		t.Fatalf("UpdateDelivery at version 5 over version 1 = %v, want a version conflict expecting 5", err)
	}
	absent := &Delivery{TaskID: "task-absent", ProjectID: testProject, Attempt: 1, Branch: "b", Version: 3}
	if err := store.UpdateDelivery(ctx, testTenant, absent); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("UpdateDelivery at version 3 of an absent record = %v, want a version conflict", err)
	}
	got, err := store.GetDelivery(ctx, testTenant, testTask)
	if err != nil || got.Version != 1 || got.HeadSHA != "" {
		t.Fatalf("stored after refusals: %+v, %v", got, err)
	}
	if _, err := store.GetDelivery(ctx, testTenant, "task-absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refused create left a record: %v", err)
	}
}

// failingGuardKV is an AtomicKVStore whose guarded writes fail outright.
type failingGuardKV struct {
	qrdbsdk.AtomicKVStore
}

var errGuardDown = errors.New("guard down")

func (failingGuardKV) PutIfAbsent(context.Context, string, []byte) (bool, error) {
	return false, errGuardDown
}

func (failingGuardKV) CompareAndSwap(context.Context, string, []byte, []byte) (bool, error) {
	return false, errGuardDown
}

func (failingGuardKV) CompareAndDelete(context.Context, string, []byte) (bool, error) {
	return false, errGuardDown
}

// plainKV is a KVStore that offers no guarded writes.
type plainKV struct {
	qrdbsdk.KVStore
}

// TestStore_GuardedWriteBackendFailures reports a guarded write that the
// store failed, a store that cannot guard at all, and a record that does not
// decode as backend failures, never as a conflict or a success.
func TestStore_GuardedWriteBackendFailures(t *testing.T) {
	ctx := context.Background()
	seeded := func(t *testing.T) (*Store, *memoryResolver, *Delivery) {
		t.Helper()
		resolver := newMemoryResolver()
		store, err := NewStore(resolver)
		if err != nil {
			t.Fatal(err)
		}
		d := &Delivery{TaskID: testTask, ProjectID: testProject, Attempt: 1, Branch: "issue/1-x", Base: "main"}
		if err := store.PutDelivery(ctx, testTenant, d); err != nil {
			t.Fatal(err)
		}
		return store, resolver, d
	}

	t.Run("guarded write fails", func(t *testing.T) {
		store, resolver, d := seeded(t)
		held := resolver.stores[testTenant].(qrdbsdk.AtomicKVStore)
		resolver.stores[testTenant] = failingGuardKV{AtomicKVStore: held}
		mine := *d
		mine.HeadSHA = "lost"
		if err := store.UpdateDelivery(ctx, testTenant, &mine); !errors.Is(err, ErrBackend) || !errors.Is(err, errGuardDown) {
			t.Fatalf("UpdateDelivery = %v, want ErrBackend wrapping the store's failure", err)
		}
		if mine.Version != d.Version {
			t.Fatalf("a failed write advanced the caller's version to %d", mine.Version)
		}
		fresh := &Delivery{TaskID: "task-new", ProjectID: testProject, Attempt: 1, Branch: "b"}
		if err := store.PutDelivery(ctx, testTenant, fresh); !errors.Is(err, ErrBackend) || !errors.Is(err, errGuardDown) {
			t.Fatalf("PutDelivery = %v, want ErrBackend wrapping the store's failure", err)
		}
		got, err := store.GetDelivery(ctx, testTenant, testTask)
		if err != nil || got.HeadSHA != "" || got.Version != d.Version {
			t.Fatalf("stored after a failed write: %+v, %v", got, err)
		}
	})

	t.Run("store cannot guard", func(t *testing.T) {
		store, resolver, d := seeded(t)
		resolver.stores[testTenant] = plainKV{KVStore: resolver.stores[testTenant]}
		binding := testBinding()
		binding.UpdatedAt = time.Now()
		writes := map[string]error{
			"PutDelivery":     store.PutDelivery(ctx, testTenant, &Delivery{TaskID: "t2", Attempt: 1, Branch: "b"}),
			"UpdateDelivery":  store.UpdateDelivery(ctx, testTenant, d),
			"PutBinding":      store.PutBinding(ctx, testTenant, testBinding()),
			"UpdateBinding":   store.UpdateBinding(ctx, testTenant, binding),
			"SaveTrainEntry":  store.SaveTrainEntry(ctx, testTenant, &TrainEntry{ProjectID: testProject, TaskID: testTask, PullRequest: 5, HeadSHA: "a", Target: "main", Branch: "b", State: TrainEntryQueued}),
			"SaveTrainLedger": store.SaveTrainLedger(ctx, testTenant, &TrainLedger{ProjectID: testProject, Next: 1}),
		}
		for op, err := range writes {
			if !errors.Is(err, ErrBackend) || !errors.Is(err, ErrGuardedWriteUnsupported) {
				t.Errorf("%s over a plain store = %v, want ErrBackend wrapping ErrGuardedWriteUnsupported", op, err)
			}
		}
		if _, err := store.GetDelivery(ctx, testTenant, testTask); err != nil {
			t.Fatalf("reads still work over a plain store: %v", err)
		}
	})

	t.Run("record does not decode", func(t *testing.T) {
		store, resolver, d := seeded(t)
		if err := resolver.stores[testTenant].Put(ctx, deliveryKey(testTask), []byte("{not json")); err != nil {
			t.Fatal(err)
		}
		if _, err := store.GetDelivery(ctx, testTenant, testTask); !errors.Is(err, ErrBackend) {
			t.Fatalf("GetDelivery of a damaged record = %v, want ErrBackend", err)
		}
		if err := store.UpdateDelivery(ctx, testTenant, d); !errors.Is(err, ErrBackend) {
			t.Fatalf("UpdateDelivery over a damaged record = %v, want ErrBackend", err)
		}
	})
}
