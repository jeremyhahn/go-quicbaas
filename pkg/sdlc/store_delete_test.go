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

// guardedDelete is one store record whose delete must not erase a write it
// did not see.
type guardedDelete struct {
	name string
	// key is the record the write lands on.
	key string
	// reads is how many store reads the write and the delete make before
	// either writes. Zero means the write is a blind upsert with no read to
	// hold, so only the interleaving test can place it inside the delete's
	// window.
	reads int
	// refused says the delete reports a version conflict when the write lands
	// inside its window. A delete that finds the record is no longer its own
	// to remove succeeds instead, and leaves it.
	refused bool
	// seeded is the mark of the seeded record.
	seeded string
	// seed writes the record both the write and the delete read.
	seed func(ctx context.Context, store *Store) error
	// write rewrites the seeded record, marked with mark.
	write func(ctx context.Context, store *Store, mark string) error
	// remove deletes the record.
	remove func(ctx context.Context, store *Store) error
	// stored reads back the mark of the stored record, or ErrNotFound.
	stored func(ctx context.Context, store *Store) (string, error)
}

func guardedDeletes() []guardedDelete {
	var binding *ProjectBinding
	var entry *TrainEntry
	seedCluster := func() *IssueCluster {
		return &IssueCluster{ProjectID: testProject, Anchor: 5, Members: []int{12}, Head: "seed"}
	}
	storedCluster := func(issue int) func(ctx context.Context, store *Store) (string, error) {
		return func(ctx context.Context, store *Store) (string, error) {
			got, err := store.GetIssueCluster(ctx, testTenant, testProject, issue)
			if err != nil {
				return "", err
			}
			return got.Head, nil
		}
	}
	return []guardedDelete{
		{
			name:    "DeleteBinding",
			key:     bindingKey(testProject),
			reads:   2,
			refused: true,
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
			remove: func(ctx context.Context, store *Store) error {
				return store.DeleteBinding(ctx, testTenant, testProject)
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
			name:    "DeleteTrainEntry",
			key:     trainEntryRecordKey(testProject, testTask),
			reads:   2,
			refused: true,
			seeded:  "seed",
			seed: func(ctx context.Context, store *Store) error {
				e := &TrainEntry{ProjectID: testProject, TaskID: testTask, PullRequest: 5, HeadSHA: "seed",
					Target: "main", Branch: "issue/5", State: TrainEntryQueued}
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
			remove: func(ctx context.Context, store *Store) error {
				return store.DeleteTrainEntry(ctx, testTenant, testProject, testTask)
			},
			stored: func(ctx context.Context, store *Store) (string, error) {
				got, err := store.GetTrainEntry(ctx, testTenant, testProject, testTask)
				if err != nil {
					return "", err
				}
				return got.HeadSHA, nil
			},
		},
		{
			name:    "DeleteReviewerOverride",
			key:     reviewerOverrideRecordKey(testProject, 5),
			reads:   2,
			refused: true,
			seeded:  "seed",
			seed: func(ctx context.Context, store *Store) error {
				return store.PutReviewerOverride(ctx, testTenant, &ReviewerOverride{ProjectID: testProject, PullRequest: 5, Profile: "seed"})
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				return store.PutReviewerOverride(ctx, testTenant, &ReviewerOverride{ProjectID: testProject, PullRequest: 5, Profile: mark})
			},
			remove: func(ctx context.Context, store *Store) error {
				return store.DeleteReviewerOverride(ctx, testTenant, testProject, 5)
			},
			stored: func(ctx context.Context, store *Store) (string, error) {
				got, err := store.GetReviewerOverride(ctx, testTenant, testProject, 5)
				if err != nil {
					return "", err
				}
				return got.Profile, nil
			},
		},
		{
			name:    "DeleteReviewerOverrides",
			key:     reviewerOverrideRecordKey(testProject, 5),
			reads:   2,
			refused: true,
			seeded:  "seed",
			seed: func(ctx context.Context, store *Store) error {
				return store.PutReviewerOverride(ctx, testTenant, &ReviewerOverride{ProjectID: testProject, PullRequest: 5, Profile: "seed"})
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				return store.PutReviewerOverride(ctx, testTenant, &ReviewerOverride{ProjectID: testProject, PullRequest: 5, Profile: mark})
			},
			remove: func(ctx context.Context, store *Store) error {
				return store.DeleteReviewerOverrides(ctx, testTenant, testProject)
			},
			stored: func(ctx context.Context, store *Store) (string, error) {
				got, err := store.GetReviewerOverride(ctx, testTenant, testProject, 5)
				if err != nil {
					return "", err
				}
				return got.Profile, nil
			},
		},
		{
			name:    "DeleteIssueCluster refiled by its anchor",
			key:     issueClusterRecordKey(testProject, 5),
			reads:   0,
			refused: true,
			seeded:  "seed",
			seed: func(ctx context.Context, store *Store) error {
				return store.PutIssueCluster(ctx, testTenant, seedCluster())
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				refiled := seedCluster()
				refiled.Head = mark
				return store.PutIssueCluster(ctx, testTenant, refiled)
			},
			remove: func(ctx context.Context, store *Store) error {
				return store.DeleteIssueCluster(ctx, testTenant, seedCluster())
			},
			stored: storedCluster(5),
		},
		{
			name:    "DeleteIssueCluster member refiled elsewhere",
			key:     issueClusterRecordKey(testProject, 12),
			reads:   0,
			refused: false,
			seeded:  "seed",
			seed: func(ctx context.Context, store *Store) error {
				return store.PutIssueCluster(ctx, testTenant, seedCluster())
			},
			write: func(ctx context.Context, store *Store, mark string) error {
				return store.PutIssueCluster(ctx, testTenant, &IssueCluster{ProjectID: testProject, Anchor: 12, Members: []int{20}, Head: mark})
			},
			remove: func(ctx context.Context, store *Store) error {
				return store.DeleteIssueCluster(ctx, testTenant, seedCluster())
			},
			stored: storedCluster(12),
		},
	}
}

// TestStore_DeleteRacingAWrite releases a write and a delete together after
// both have read the same record. An acknowledged write must survive: the
// delete either went first, or is refused because the record it checked
// changed. A delete that alone was acknowledged leaves no record.
func TestStore_DeleteRacingAWrite(t *testing.T) {
	for _, tc := range guardedDeletes() {
		if tc.reads == 0 {
			// Nothing holds a blind upsert behind the delete's read.
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			store, gated := gatedStore(t)
			if err := tc.seed(ctx, store); err != nil {
				t.Fatalf("seed: %v", err)
			}

			gated.arm(tc.reads)
			var writeErr, removeErr error
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				writeErr = tc.write(ctx, store, "raced")
			}()
			go func() {
				defer wg.Done()
				removeErr = tc.remove(ctx, store)
			}()
			wg.Wait()

			for op, err := range map[string]error{"write": writeErr, "delete": removeErr} {
				if err != nil && !errors.Is(err, ErrVersionConflict) {
					t.Fatalf("%s: %v, want success or a version conflict", op, err)
				}
			}
			got, err := tc.stored(ctx, store)
			switch {
			case writeErr == nil:
				if err != nil || got != "raced" {
					t.Fatalf("acknowledged write stored as %q, %v (delete: %v)", got, err, removeErr)
				}
			case removeErr == nil:
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("acknowledged delete left %q, %v", got, err)
				}
			default:
				t.Fatalf("neither was acknowledged: write %v, delete %v", writeErr, removeErr)
			}
		})
	}
}

// interleavingKV runs one write just after a reader's Get of one key, so a
// test can place the write exactly inside a delete's read-check-delete
// window.
type interleavingKV struct {
	qrdbsdk.AtomicKVStore

	mu      sync.Mutex
	key     string
	between func()
}

func (s *interleavingKV) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.AtomicKVStore.Get(ctx, key)
	s.mu.Lock()
	var between func()
	if key == s.key {
		between = s.between
		s.between = nil
	}
	s.mu.Unlock()
	if between != nil {
		between()
	}
	return value, err
}

func (s *interleavingKV) interleave(key string, write func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = key
	s.between = write
}

func interleavingStore(t *testing.T) (*Store, *interleavingKV) {
	t.Helper()
	resolver := newMemoryResolver()
	kv := &interleavingKV{AtomicKVStore: qrdbsdk.NewMemoryKVStore()}
	resolver.stores[testTenant] = kv
	store, err := NewStore(resolver)
	if err != nil {
		t.Fatal(err)
	}
	return store, kv
}

// TestStore_DeleteKeepsARecordChangedAfterItsRead lands a write after the
// delete read the record and before it deleted: the write survives, and the
// delete is refused unless the record is no longer its own to remove.
func TestStore_DeleteKeepsARecordChangedAfterItsRead(t *testing.T) {
	for _, tc := range guardedDeletes() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, kv := interleavingStore(t)
			if err := tc.seed(ctx, store); err != nil {
				t.Fatalf("seed: %v", err)
			}
			var writeErr error
			kv.interleave(tc.key, func() { writeErr = tc.write(ctx, store, "between") })

			err := tc.remove(ctx, store)
			if tc.refused && !errors.Is(err, ErrVersionConflict) {
				t.Fatalf("delete over a changed record = %v, want a version conflict", err)
			}
			if !tc.refused && err != nil {
				t.Fatalf("delete over a record no longer its own = %v, want nil", err)
			}
			if writeErr != nil {
				t.Fatalf("interleaved write: %v", writeErr)
			}
			if got, err := tc.stored(ctx, store); err != nil || got != "between" {
				t.Fatalf("stored after the delete: %q, %v", got, err)
			}
		})
	}
}

// TestStore_DeleteAfterAnotherDeleteIsANoOp lands a second delete inside the
// first one's window: the record is gone either way, so both succeed.
func TestStore_DeleteAfterAnotherDeleteIsANoOp(t *testing.T) {
	for _, tc := range guardedDeletes() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, kv := interleavingStore(t)
			if err := tc.seed(ctx, store); err != nil {
				t.Fatalf("seed: %v", err)
			}
			var otherErr error
			kv.interleave(tc.key, func() { otherErr = tc.remove(ctx, store) })

			if err := tc.remove(ctx, store); err != nil {
				t.Fatalf("delete that lost to another delete = %v, want nil", err)
			}
			if otherErr != nil {
				t.Fatalf("interleaved delete: %v", otherErr)
			}
			if _, err := tc.stored(ctx, store); !errors.Is(err, ErrNotFound) {
				t.Fatalf("record after both deletes: %v, want not found", err)
			}
		})
	}
}

// TestStore_DeleteBackendFailures keeps the record when the store cannot
// decide the delete, and refuses a store that offers no guarded delete.
func TestStore_DeleteBackendFailures(t *testing.T) {
	for _, tc := range guardedDeletes() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			resolver := newMemoryResolver()
			store, err := NewStore(resolver)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.seed(ctx, store); err != nil {
				t.Fatalf("seed: %v", err)
			}
			held := resolver.stores[testTenant]

			resolver.stores[testTenant] = failingGuardKV{AtomicKVStore: held.(qrdbsdk.AtomicKVStore)}
			if err := tc.remove(ctx, store); !errors.Is(err, ErrBackend) || !errors.Is(err, errGuardDown) {
				t.Fatalf("delete over a failing guard = %v, want ErrBackend wrapping the store's failure", err)
			}

			resolver.stores[testTenant] = plainKV{KVStore: held}
			if err := tc.remove(ctx, store); !errors.Is(err, ErrBackend) || !errors.Is(err, ErrGuardedWriteUnsupported) {
				t.Fatalf("delete over a plain store = %v, want ErrBackend wrapping ErrGuardedWriteUnsupported", err)
			}

			resolver.stores[testTenant] = held
			if got, err := tc.stored(ctx, store); err != nil || got != tc.seeded {
				t.Fatalf("record after failed deletes: %q, %v", got, err)
			}
		})
	}
}

// TestStore_ConcurrentReviewerOverridePutsKeepOneWholeWrite shows that
// PutReviewerOverride is a blind upsert: concurrent puts that all read the
// same record each replace it whole, so the store keeps exactly one
// contender's write and the original creation stamp, and nothing is merged
// from a stale read.
func TestStore_ConcurrentReviewerOverridePutsKeepOneWholeWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, gated := gatedStore(t)
	seed := &ReviewerOverride{ProjectID: testProject, PullRequest: 5, Profile: "seed"}
	if err := store.PutReviewerOverride(ctx, testTenant, seed); err != nil {
		t.Fatal(err)
	}

	gated.arm(contenders)
	errs := make([]error, contenders)
	var wg sync.WaitGroup
	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.PutReviewerOverride(ctx, testTenant,
				&ReviewerOverride{ProjectID: testProject, PullRequest: 5, Profile: strconv.Itoa(i)})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	got, err := store.GetReviewerOverride(ctx, testTenant, testProject, 5)
	if err != nil {
		t.Fatal(err)
	}
	if n, convErr := strconv.Atoi(got.Profile); convErr != nil || n < 0 || n >= contenders {
		t.Fatalf("stored profile %q, want one contender's", got.Profile)
	}
	if !got.CreatedAt.Equal(seed.CreatedAt) {
		t.Fatalf("created at %v, want the seed's %v", got.CreatedAt, seed.CreatedAt)
	}
}
