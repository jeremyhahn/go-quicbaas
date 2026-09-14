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

package orchestration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// =============================================================================
// Fixtures
// =============================================================================

// fakeClock is a clock a test can move, so a heartbeat can be aged across a
// TTL without waiting for it.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Now().UTC()}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// leaseStoreCase builds one kind of store over an injectable clock.
type leaseStoreCase struct {
	name  string
	build func(t *testing.T, ttl time.Duration) (JobLeaseStore, *fakeClock)
}

// leaseStoreCases yields both stores: the transaction-plane store over the
// package's shared qrdb node, and the single-process store. Every semantic
// test runs against both, because the decision they share must not drift.
func leaseStoreCases() []leaseStoreCase {
	return []leaseStoreCase{
		{
			name: "transaction",
			build: func(t *testing.T, ttl time.Duration) (JobLeaseStore, *fakeClock) {
				t.Helper()
				n := claimNode(t)
				store, err := NewTransactionJobLeaseStore(n.Transaction, n.KV, ttl)
				if err != nil {
					t.Fatalf("NewTransactionJobLeaseStore: %v", err)
				}
				clock := newFakeClock()
				store.(*transactionJobLeaseStore).now = clock.Now
				return store, clock
			},
		},
		{
			name: "process",
			build: func(t *testing.T, ttl time.Duration) (JobLeaseStore, *fakeClock) {
				t.Helper()
				store, err := NewProcessJobLeaseStore(ttl)
				if err != nil {
					t.Fatalf("NewProcessJobLeaseStore: %v", err)
				}
				clock := newFakeClock()
				store.(*processJobLeaseStore).now = clock.Now
				return store, clock
			},
		},
	}
}

// testLease builds a lease over a subject no other test shares.
func testLease(ref, jobID string, generation int, node string) *JobLease {
	return &JobLease{
		TenantID:   "acme",
		ProjectID:  "proj-1",
		Workflow:   "pipeline",
		Ref:        ref,
		TaskID:     "task-" + ref,
		JobID:      jobID,
		Generation: generation,
		Node:       node,
	}
}

const leaseTestTTL = time.Minute

// =============================================================================
// Generation ordering
// =============================================================================

// TestJobLease_LaterGenerationSupersedesAndTheHolderLearns proves the
// ordering: a later attempt takes the lease from a live earlier one, the
// earlier one is told on its next heartbeat, and it cannot take the lease
// back.
func TestJobLease_LaterGenerationSupersedesAndTheHolderLearns(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("gen")

			first := testLease(ref, "job-1", 1, "node-a")
			if err := store.Acquire(ctx, first); err != nil {
				t.Fatalf("generation 1 acquire: %v", err)
			}
			if first.AcquiredAt == 0 || first.HeartbeatAt == 0 {
				t.Fatal("a won acquire must fill the timestamps in")
			}

			second := testLease(ref, "job-2", 2, "node-b")
			if err := store.Acquire(ctx, second); err != nil {
				t.Fatalf("generation 2 must supersede a live generation 1: %v", err)
			}

			err := store.Heartbeat(ctx, first)
			var superseded *JobSupersededError
			if !errors.As(err, &superseded) {
				t.Fatalf("the superseded holder's heartbeat must say so, got %T: %v", err, err)
			}
			if superseded.By != 2 || superseded.Node != "node-b" || superseded.JobID != "job-1" {
				t.Fatalf("superseded error names the wrong successor: %+v", superseded)
			}
			if !errors.Is(err, ErrJobSuperseded) {
				t.Fatal("JobSupersededError must unwrap to ErrJobSuperseded")
			}

			err = store.Acquire(ctx, testLease(ref, "job-1", 1, "node-a"))
			var taken *JobLeaseTakenError
			if !errors.As(err, &taken) {
				t.Fatalf("generation 1 must not take the lease back from 2, got %T: %v", err, err)
			}
			if taken.HeldBy != "node-b" || taken.Generation != 2 || taken.HeldByJobID != "job-2" || taken.JobID != "job-1" {
				t.Fatalf("taken error names the wrong holder: %+v", taken)
			}
			if !errors.Is(err, ErrJobLeaseTaken) {
				t.Fatal("JobLeaseTakenError must unwrap to ErrJobLeaseTaken")
			}
		})
	}
}

// TestJobLease_LiveHolderDeclinesASameGenerationContender proves plain
// contention: two jobs at one generation, the first wins, the second is
// declined and told who holds it.
func TestJobLease_LiveHolderDeclinesASameGenerationContender(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("contend")

			if err := store.Acquire(ctx, testLease(ref, "job-1", 1, "node-a")); err != nil {
				t.Fatalf("first acquire: %v", err)
			}
			err := store.Acquire(ctx, testLease(ref, "job-2", 1, "node-b"))
			var taken *JobLeaseTakenError
			if !errors.As(err, &taken) {
				t.Fatalf("expected JobLeaseTakenError, got %T: %v", err, err)
			}
			if taken.HeldBy != "node-a" || taken.HeldByJobID != "job-1" {
				t.Fatalf("loser was not told who holds the lease: %+v", taken)
			}
		})
	}
}

// TestJobLease_NineContendersAdmitExactlyOne is the race: nine jobs, one
// subject, one generation, all asking at once.
func TestJobLease_NineContendersAdmitExactlyOne(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("race")

			var (
				wg      sync.WaitGroup
				start   = make(chan struct{})
				results = make([]error, nineWorkerRace)
			)
			for i := 0; i < nineWorkerRace; i++ {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					<-start
					results[n] = store.Acquire(ctx, testLease(ref, "job-"+itoa(n), 1, "node-"+itoa(n)))
				}(i)
			}
			close(start)
			wg.Wait()

			winners := 0
			winner := ""
			for n, err := range results {
				if err == nil {
					winners++
					winner = "job-" + itoa(n)
				}
			}
			if winners != 1 {
				t.Fatalf("expected exactly one winner, got %d", winners)
			}
			for _, err := range results {
				if err == nil {
					continue
				}
				var taken *JobLeaseTakenError
				if !errors.As(err, &taken) {
					t.Fatalf("a loser got %T rather than JobLeaseTakenError: %v", err, err)
				}
				if taken.HeldByJobID != winner {
					t.Fatalf("a loser was told the wrong winner: %s vs %s", taken.HeldByJobID, winner)
				}
			}
		})
	}
}

// =============================================================================
// Stale heartbeat handover
// =============================================================================

// TestJobLease_StaleHeartbeatHandsOver proves a holder that stopped proving
// itself alive loses the lease to a contender at the SAME generation, and
// that a holder still heartbeating does not.
func TestJobLease_StaleHeartbeatHandsOver(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, clock := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("stale")

			holder := testLease(ref, "job-1", 1, "node-a")
			if err := store.Acquire(ctx, holder); err != nil {
				t.Fatalf("acquire: %v", err)
			}

			// Within the TTL a heartbeat keeps it.
			clock.Advance(leaseTestTTL / 2)
			if err := store.Heartbeat(ctx, holder); err != nil {
				t.Fatalf("heartbeat inside the TTL: %v", err)
			}
			clock.Advance(leaseTestTTL / 2)
			if err := store.Acquire(ctx, testLease(ref, "job-2", 1, "node-b")); !errors.Is(err, ErrJobLeaseTaken) {
				t.Fatalf("a heartbeating holder must not be taken: %v", err)
			}

			// Past the TTL with no heartbeat, the contender takes it.
			clock.Advance(leaseTestTTL + time.Nanosecond)
			contender := testLease(ref, "job-2", 1, "node-b")
			if err := store.Acquire(ctx, contender); err != nil {
				t.Fatalf("a stale holder must hand over: %v", err)
			}
			if err := store.Heartbeat(ctx, holder); !errors.Is(err, ErrJobSuperseded) {
				t.Fatalf("the stale holder must learn it lost, got %v", err)
			}
		})
	}
}

// TestJobLease_TheHolderReacquiresItsOwnLease proves resumption: the same
// job at the same generation is admitted while its own heartbeat is fresh,
// because a restart inside one TTL is the same job continuing.
func TestJobLease_TheHolderReacquiresItsOwnLease(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("resume")

			if err := store.Acquire(ctx, testLease(ref, "job-1", 1, "node-a")); err != nil {
				t.Fatalf("acquire: %v", err)
			}
			if err := store.Acquire(ctx, testLease(ref, "job-1", 1, "node-a")); err != nil {
				t.Fatalf("the holder re-acquiring its own lease must be admitted: %v", err)
			}
		})
	}
}

// =============================================================================
// Release is a field
// =============================================================================

// TestJobLease_ReleaseIsAFieldNeverADelete proves the record survives its
// release, that a released lease admits the next holder, that release is
// idempotent, and that a non-holder's release changes nothing.
func TestJobLease_ReleaseIsAFieldNeverADelete(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("release")

			holder := testLease(ref, "job-1", 1, "node-a")
			if err := store.Acquire(ctx, holder); err != nil {
				t.Fatalf("acquire: %v", err)
			}

			// A non-holder's release is a no-op.
			if err := store.Release(ctx, testLease(ref, "job-9", 1, "node-z")); err != nil {
				t.Fatalf("a non-holder's release must not fail: %v", err)
			}
			got, err := store.Get(ctx, "acme", "proj-1", "pipeline", ref)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Released {
				t.Fatal("a non-holder released the holder's lease")
			}

			if err := store.Release(ctx, holder); err != nil {
				t.Fatalf("release: %v", err)
			}
			if !holder.Released || holder.ReleasedAt == 0 {
				t.Fatal("release must be reflected on the caller's lease")
			}
			got, err = store.Get(ctx, "acme", "proj-1", "pipeline", ref)
			if err != nil {
				t.Fatalf("the record must survive its release: %v", err)
			}
			if !got.Released || got.ReleasedAt == 0 || got.JobID != "job-1" {
				t.Fatalf("released record is wrong: %+v", got)
			}

			// Idempotent.
			if err := store.Release(ctx, holder); err != nil {
				t.Fatalf("second release: %v", err)
			}

			// A heartbeat on a released lease is left alone rather than failed.
			if err := store.Heartbeat(ctx, holder); err != nil {
				t.Fatalf("heartbeat after release must be a no-op: %v", err)
			}

			// The next holder, at the same generation, is admitted.
			if err := store.Acquire(ctx, testLease(ref, "job-2", 1, "node-b")); err != nil {
				t.Fatalf("a released lease must admit the next holder: %v", err)
			}
		})
	}
}

// TestJobLease_MissingRecordIsNamed proves a heartbeat, release or get on a
// lease that was never taken reports the absence rather than inventing one.
func TestJobLease_MissingRecordIsNamed(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			ref := uniqueTaskID("missing")
			never := testLease(ref, "job-1", 1, "node-a")

			for name, err := range map[string]error{
				"heartbeat": store.Heartbeat(ctx, never),
				"release":   store.Release(ctx, never),
			} {
				var missing *JobLeaseNotFoundError
				if !errors.As(err, &missing) {
					t.Fatalf("%s: expected JobLeaseNotFoundError, got %T: %v", name, err, err)
				}
				if !errors.Is(err, ErrJobLeaseNotFound) {
					t.Fatalf("%s: must unwrap to ErrJobLeaseNotFound", name)
				}
			}
			if _, err := store.Get(ctx, "acme", "proj-1", "pipeline", ref); !errors.Is(err, ErrJobLeaseNotFound) {
				t.Fatalf("get: expected ErrJobLeaseNotFound, got %v", err)
			}
		})
	}
}

// =============================================================================
// Keys and listing
// =============================================================================

// TestJobLeaseKey_HashesTheSegmentsItDoesNotOwn proves a ref or workflow
// carrying the separator still yields a well-formed key, and that an
// unscoped job lands in its own subtree.
func TestJobLeaseKey_HashesTheSegmentsItDoesNotOwn(t *testing.T) {
	key, err := jobLeaseKey("acme", "proj-1", "ci / build (push)", "refs/heads/feature/x")
	if err != nil {
		t.Fatalf("jobLeaseKey: %v", err)
	}
	if !strings.HasPrefix(key, JobLeasePrefix+"acme/proj-1/") {
		t.Fatalf("key is not under the tenant/project scope: %s", key)
	}
	rest := strings.TrimPrefix(key, JobLeasePrefix)
	if got := strings.Count(rest, keySeparator); got != 3 {
		t.Fatalf("expected 3 separators after the prefix, got %d in %s", got, rest)
	}
	if strings.Contains(rest, "refs") || strings.Contains(rest, "build") {
		t.Fatalf("a hashed segment leaked its value: %s", rest)
	}

	unscoped, err := jobLeaseKey("acme", "", "", "job-1")
	if err != nil {
		t.Fatalf("unscoped key: %v", err)
	}
	if !strings.HasPrefix(unscoped, JobLeasePrefix+"acme/"+unscopedProjectSegment+"/") {
		t.Fatalf("an unscoped job must use the marker segment: %s", unscoped)
	}
	if SegmentHash("a") == SegmentHash("b") || len(SegmentHash("a")) != 64 {
		t.Fatal("SegmentHash must be a distinct fixed-width hex digest")
	}
}

// TestJobLeaseKey_RefusesASegmentThatEscapes proves a tenant or project
// carrying the separator is refused rather than written into another
// subtree.
func TestJobLeaseKey_RefusesASegmentThatEscapes(t *testing.T) {
	for name, args := range map[string][2]string{
		"empty tenant":   {"", "proj-1"},
		"tenant escape":  {"acme/other", "proj-1"},
		"project escape": {"acme", "proj/other"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := jobLeaseKey(args[0], args[1], "wf", "ref")
			if !errors.Is(err, ErrJobLeaseInvalidInput) {
				t.Fatalf("expected ErrJobLeaseInvalidInput, got %v", err)
			}
			var segment *KeySegmentError
			if !errors.As(err, &segment) {
				t.Fatalf("the refusal must carry the segment error, got %T", err)
			}
		})
	}
}

// TestJobLease_ListScopes proves listing by tenant and by project, sorted.
func TestJobLease_ListScopes(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			tenant := uniqueTaskID("tenant")

			for i, project := range []string{"p1", "p1", "p2"} {
				lease := &JobLease{TenantID: tenant, ProjectID: project, Ref: "ref-" + itoa(i), JobID: "job-" + itoa(i), Generation: 1}
				if err := store.Acquire(ctx, lease); err != nil {
					t.Fatalf("acquire %d: %v", i, err)
				}
			}
			all, err := store.List(ctx, tenant, "")
			if err != nil {
				t.Fatalf("list tenant: %v", err)
			}
			if len(all) != 3 {
				t.Fatalf("expected 3 leases in the tenant, got %d", len(all))
			}
			p1, err := store.List(ctx, tenant, "p1")
			if err != nil {
				t.Fatalf("list project: %v", err)
			}
			if len(p1) != 2 {
				t.Fatalf("expected 2 leases in p1, got %d", len(p1))
			}
			for _, lease := range p1 {
				if lease.ProjectID != "p1" || lease.TenantID != tenant {
					t.Fatalf("a listed lease is outside the scope: %+v", lease)
				}
			}
			if _, err := store.List(ctx, "", "p1"); !errors.Is(err, ErrJobLeaseInvalidInput) {
				t.Fatalf("a project listing without its tenant must be refused, got %v", err)
			}
		})
	}
}

// =============================================================================
// Refusals
// =============================================================================

// TestJobLease_RefusesAnUnidentifiableLease proves the fields a decision
// depends on are required rather than defaulted.
func TestJobLease_RefusesAnUnidentifiableLease(t *testing.T) {
	for _, tc := range leaseStoreCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := tc.build(t, leaseTestTTL)
			ctx := context.Background()
			for name, lease := range map[string]*JobLease{
				"nil":             nil,
				"no job":          {TenantID: "acme", Ref: "r", Generation: 1},
				"zero generation": {TenantID: "acme", Ref: "r", JobID: "j"},
				"no ref":          {TenantID: "acme", JobID: "j", Generation: 1},
				"no tenant":       {Ref: "r", JobID: "j", Generation: 1},
			} {
				t.Run(name, func(t *testing.T) {
					for op, err := range map[string]error{
						"acquire":   store.Acquire(ctx, lease),
						"heartbeat": store.Heartbeat(ctx, lease),
						"release":   store.Release(ctx, lease),
					} {
						if !errors.Is(err, ErrJobLeaseInvalidInput) {
							t.Fatalf("%s: expected ErrJobLeaseInvalidInput, got %v", op, err)
						}
					}
				})
			}
		})
	}
}

// TestNewJobLeaseStore_RefusesAMissingHandleOrTTL proves both constructors
// fail closed.
func TestNewJobLeaseStore_RefusesAMissingHandleOrTTL(t *testing.T) {
	n := claimNode(t)
	for name, args := range map[string]struct {
		tx  cas.Transactor
		kv  cas.Reader
		ttl time.Duration
	}{
		"no transactor": {nil, n.KV, leaseTestTTL},
		"no reader":     {n.Transaction, nil, leaseTestTTL},
		"zero ttl":      {n.Transaction, n.KV, 0},
	} {
		t.Run("transaction/"+name, func(t *testing.T) {
			store, err := NewTransactionJobLeaseStore(args.tx, args.kv, args.ttl)
			if !errors.Is(err, ErrJobLeaseInvalidInput) || store != nil {
				t.Fatalf("expected a refusal, got store=%v err=%v", store, err)
			}
		})
	}
	if store, err := NewProcessJobLeaseStore(0); !errors.Is(err, ErrJobLeaseInvalidInput) || store != nil {
		t.Fatalf("process store must refuse a zero TTL, got store=%v err=%v", store, err)
	}
}

// TestJobLease_UnreadableRecordIsRefused proves a record that decodes to no
// holder fails closed rather than being handed over.
func TestJobLease_UnreadableRecordIsRefused(t *testing.T) {
	n := claimNode(t)
	store, err := NewTransactionJobLeaseStore(n.Transaction, n.KV, leaseTestTTL)
	if err != nil {
		t.Fatalf("NewTransactionJobLeaseStore: %v", err)
	}
	ctx := context.Background()
	ref := uniqueTaskID("garbage")
	key, err := jobLeaseKey("acme", "proj-1", "pipeline", ref)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if err := n.KV.Put(ctx, key, []byte("{not json")); err != nil {
		t.Fatalf("put garbage: %v", err)
	}
	if err := store.Acquire(ctx, testLease(ref, "job-1", 1, "node-a")); !errors.Is(err, ErrJobLeaseUnreadable) {
		t.Fatalf("expected ErrJobLeaseUnreadable, got %v", err)
	}
	if _, err := store.Get(ctx, "acme", "proj-1", "pipeline", ref); !errors.Is(err, ErrJobLeaseUnreadable) {
		t.Fatalf("get: expected ErrJobLeaseUnreadable, got %v", err)
	}
}

// =============================================================================
// Project capacity
// =============================================================================

// TestProjectCapacity_RefusesTheJobOverTheQuota proves the typed refusal, that
// a released or stale lease frees the slot, and that a project without a
// quota is unlimited.
func TestProjectCapacity_RefusesTheJobOverTheQuota(t *testing.T) {
	ctx := context.Background()
	projects := NewInMemoryProjectStore()
	if err := projects.Create(ctx, &Project{ID: "proj-1", TenantID: "acme", Name: "capped", Quotas: ProjectQuotas{MaxConcurrency: 1}}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := projects.Create(ctx, &Project{ID: "proj-open", TenantID: "acme", Name: "open"}); err != nil {
		t.Fatalf("create open project: %v", err)
	}
	inner, err := NewProcessJobLeaseStore(leaseTestTTL)
	if err != nil {
		t.Fatalf("inner store: %v", err)
	}
	clock := newFakeClock()
	inner.(*processJobLeaseStore).now = clock.Now
	store, err := NewProjectCapacityJobLeaseStore(inner, projects, leaseTestTTL)
	if err != nil {
		t.Fatalf("NewProjectCapacityJobLeaseStore: %v", err)
	}
	store.(*projectCapacityJobLeaseStore).now = clock.Now

	first := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "a", JobID: "job-1", Generation: 1}
	if err := store.Acquire(ctx, first); err != nil {
		t.Fatalf("first job under the quota: %v", err)
	}
	err = store.Acquire(ctx, &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "b", JobID: "job-2", Generation: 1})
	var exceeded *ProjectCapacityExceededError
	if !errors.As(err, &exceeded) {
		t.Fatalf("expected ProjectCapacityExceededError, got %T: %v", err, err)
	}
	if exceeded.Live != 1 || exceeded.Limit != 1 || exceeded.ProjectID != "proj-1" || exceeded.RetryAt.IsZero() {
		t.Fatalf("refusal is wrong: %+v", exceeded)
	}
	if !errors.Is(err, ErrProjectCapacityExceeded) {
		t.Fatal("must unwrap to ErrProjectCapacityExceeded")
	}

	// The holder re-acquiring is not counted against itself.
	if err := store.Acquire(ctx, &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "a", JobID: "job-1", Generation: 1}); err != nil {
		t.Fatalf("the holder must not be refused by its own lease: %v", err)
	}

	// A released lease frees the slot.
	if err := store.Release(ctx, first); err != nil {
		t.Fatalf("release: %v", err)
	}
	second := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "b", JobID: "job-2", Generation: 1}
	if err := store.Acquire(ctx, second); err != nil {
		t.Fatalf("after a release the slot is free: %v", err)
	}

	// A stale lease frees the slot too.
	clock.Advance(leaseTestTTL + time.Second)
	if err := store.Acquire(ctx, &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "c", JobID: "job-3", Generation: 1}); err != nil {
		t.Fatalf("after the holder went stale the slot is free: %v", err)
	}

	// No quota, unknown project, no project: unlimited.
	for name, lease := range map[string]*JobLease{
		"open project":    {TenantID: "acme", ProjectID: "proj-open", Ref: "x", JobID: "job-x", Generation: 1},
		"unknown project": {TenantID: "acme", ProjectID: "proj-none", Ref: "y", JobID: "job-y", Generation: 1},
		"no project":      {TenantID: "acme", Ref: "z", JobID: "job-z", Generation: 1},
	} {
		if err := store.Acquire(ctx, lease); err != nil {
			t.Fatalf("%s must not be capped: %v", name, err)
		}
	}
}

// TestNewProjectCapacityJobLeaseStore_RefusesMissingDependencies proves the
// wrapper cannot be built in a shape that admits everything while claiming to
// check.
func TestNewProjectCapacityJobLeaseStore_RefusesMissingDependencies(t *testing.T) {
	inner, err := NewProcessJobLeaseStore(leaseTestTTL)
	if err != nil {
		t.Fatalf("inner store: %v", err)
	}
	projects := NewInMemoryProjectStore()
	for name, args := range map[string]struct {
		store    JobLeaseStore
		projects ProjectStore
		ttl      time.Duration
	}{
		"no store":    {nil, projects, leaseTestTTL},
		"no projects": {inner, nil, leaseTestTTL},
		"zero ttl":    {inner, projects, 0},
	} {
		t.Run(name, func(t *testing.T) {
			store, err := NewProjectCapacityJobLeaseStore(args.store, args.projects, args.ttl)
			if !errors.Is(err, ErrJobLeaseInvalidInput) || store != nil {
				t.Fatalf("expected a refusal, got store=%v err=%v", store, err)
			}
		})
	}
	if err := (&projectCapacityJobLeaseStore{JobLeaseStore: inner, projects: projects, ttl: leaseTestTTL, now: utcNow}).Acquire(context.Background(), nil); !errors.Is(err, ErrJobLeaseInvalidInput) {
		t.Fatalf("a nil lease must be refused before the quota is read, got %v", err)
	}
}

// TestJobLease_LiveAndStale pins the record's own predicates.
func TestJobLease_LiveAndStale(t *testing.T) {
	now := time.Now().UTC()
	fresh := &JobLease{HeartbeatAt: now.UnixNano()}
	if !fresh.Live(now, time.Minute) || fresh.Stale(now, time.Minute) {
		t.Fatal("a fresh lease is live")
	}
	old := &JobLease{HeartbeatAt: now.Add(-2 * time.Minute).UnixNano()}
	if old.Live(now, time.Minute) || !old.Stale(now, time.Minute) {
		t.Fatal("an old lease is stale")
	}
	released := &JobLease{HeartbeatAt: now.Add(-2 * time.Minute).UnixNano(), Released: true}
	if released.Live(now, time.Minute) || released.Stale(now, time.Minute) {
		t.Fatal("a released lease is neither live nor stale")
	}
}

// TestJobLeaseErrors_Render pins the error strings carry their fields.
func TestJobLeaseErrors_Render(t *testing.T) {
	cause := errors.New("boom")
	wrapped := &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.swap", Cause: cause}
	if !errors.Is(wrapped, ErrJobLeaseBackend) || !errors.Is(wrapped, cause) {
		t.Fatal("JobLeaseError must unwrap to both the sentinel and the cause")
	}
	if !strings.Contains(wrapped.Error(), "lease.swap") || !strings.Contains(wrapped.Error(), "boom") {
		t.Fatalf("message lost: %s", wrapped.Error())
	}
	bare := &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput}
	if bare.Error() != ErrJobLeaseInvalidInput.Error() || len(bare.Unwrap()) != 1 {
		t.Fatalf("a bare error renders its sentinel alone: %s", bare.Error())
	}
	capacity := &ProjectCapacityExceededError{TenantID: "acme", ProjectID: "p", Live: 2, Limit: 2}
	if !strings.Contains(capacity.Error(), "live=2") || !strings.Contains(capacity.Error(), "limit=2") {
		t.Fatalf("capacity error lost its counts: %s", capacity.Error())
	}
	if (&JobLeaseNotFoundError{Key: "k"}).Error() != ErrJobLeaseNotFound.Error()+": k" {
		t.Fatal("not-found error must name the key")
	}
}

// =============================================================================
// The off-budget lease — one budget, three readers, one answer
// =============================================================================
//
// A lease that does not spend a worker slot has to be off the budget in BOTH
// directions and at EVERY reader. A contender admitted by one reader and
// counted by another is a job that is let in and then cannot run, which is
// the intermittent kind of scheduling bug nobody can reproduce — so each
// reader is driven here against the real capacity store rather than against a
// stand-in for it.

// cappedCapacityStore is the real capacity store over a real lease store, on
// a project capped at one concurrent job.
func cappedCapacityStore(t *testing.T, limit int) (JobLeaseStore, *fakeClock) {
	t.Helper()
	ctx := context.Background()
	projects := NewInMemoryProjectStore()
	if err := projects.Create(ctx, &Project{
		TenantID: "acme", ID: "proj-1", Name: "capped",
		Quotas: ProjectQuotas{MaxConcurrency: limit},
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	inner, err := NewProcessJobLeaseStore(leaseTestTTL)
	if err != nil {
		t.Fatalf("inner store: %v", err)
	}
	clock := newFakeClock()
	inner.(*processJobLeaseStore).now = clock.Now
	store, err := NewProjectCapacityJobLeaseStore(inner, projects, leaseTestTTL)
	if err != nil {
		t.Fatalf("NewProjectCapacityJobLeaseStore: %v", err)
	}
	store.(*projectCapacityJobLeaseStore).now = clock.Now
	return store, clock
}

func TestProjectCapacity_AdmitsAnOffBudgetLeaseAtTheLimit(t *testing.T) {
	ctx := context.Background()
	store, _ := cappedCapacityStore(t, 1)

	// The project's one slot is taken by work that does spend it.
	worker := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-25", JobID: "job-worker", Generation: 1}
	if err := store.Acquire(ctx, worker); err != nil {
		t.Fatalf("the worker under the quota: %v", err)
	}

	// A budgeted contender is refused, which is the control: the project
	// really is at its limit.
	budgeted := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-26", JobID: "job-budgeted", Generation: 1}
	if err := store.Acquire(ctx, budgeted); !errors.Is(err, ErrProjectCapacityExceeded) {
		t.Fatalf("a budgeted contender at the limit got %v, want the quota refusal", err)
	}

	// THE PROPERTY: the off-budget contender is admitted anyway.
	review := &JobLease{
		TenantID: "acme", ProjectID: "proj-1", Ref: "review:proj-1:#41",
		JobID: "job-review", Generation: 1, OffBudget: true,
	}
	if err := store.Acquire(ctx, review); err != nil {
		t.Fatalf("an off-budget lease was refused at the limit: %v", err)
	}
}

func TestProjectCapacity_AnOffBudgetLeaseDoesNotFillTheBudget(t *testing.T) {
	ctx := context.Background()
	store, _ := cappedCapacityStore(t, 1)

	// The other direction. The off-budget lease is held first, so if it
	// counted, the worker below would be refused.
	review := &JobLease{
		TenantID: "acme", ProjectID: "proj-1", Ref: "review:proj-1:#41",
		JobID: "job-review", Generation: 1, OffBudget: true,
	}
	if err := store.Acquire(ctx, review); err != nil {
		t.Fatalf("acquire the off-budget lease: %v", err)
	}
	worker := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-25", JobID: "job-worker", Generation: 1}
	if err := store.Acquire(ctx, worker); err != nil {
		t.Fatalf("the project's only slot was consumed by an off-budget lease: %v", err)
	}

	// And the slot is now genuinely spent: a second worker is refused.
	next := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-26", JobID: "job-next", Generation: 1}
	if err := store.Acquire(ctx, next); !errors.Is(err, ErrProjectCapacityExceeded) {
		t.Fatalf("the budget stopped being enforced: %v", err)
	}
}

func TestCountLiveJobLeases_AgreesWithTheCapacityStoreAboutTheBudget(t *testing.T) {
	// THE THIRD READER, and the one that is easiest to leave behind: the
	// task dispatcher and the task-source poller both subtract this count
	// from MaxConcurrency. A count that included the off-budget lease would
	// have them hold work back for a slot that is free.
	ctx := context.Background()
	store, clock := cappedCapacityStore(t, 2)

	leases := []*JobLease{
		{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-25", JobID: "job-worker", Generation: 1},
		{TenantID: "acme", ProjectID: "proj-1", Ref: "review:proj-1:#41", JobID: "job-review", Generation: 1, OffBudget: true},
	}
	for _, lease := range leases {
		if err := store.Acquire(ctx, lease); err != nil {
			t.Fatalf("acquire %s: %v", lease.JobID, err)
		}
	}

	spent, err := CountLiveJobLeases(ctx, store, "acme", "proj-1", clock.Now(), leaseTestTTL)
	if err != nil {
		t.Fatalf("CountLiveJobLeases: %v", err)
	}
	if spent != 1 {
		t.Fatalf("the budget reads %d of 2 spent, want 1: the off-budget review was counted", spent)
	}

	// And the two readers agree in the limit case: with the budget full of
	// budgeted work, the count says so and the store refuses.
	second := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-26", JobID: "job-worker-2", Generation: 1}
	if err := store.Acquire(ctx, second); err != nil {
		t.Fatalf("the second worker is within the quota of 2: %v", err)
	}
	spent, err = CountLiveJobLeases(ctx, store, "acme", "proj-1", clock.Now(), leaseTestTTL)
	if err != nil {
		t.Fatalf("CountLiveJobLeases: %v", err)
	}
	third := &JobLease{TenantID: "acme", ProjectID: "proj-1", Ref: "issue-27", JobID: "job-worker-3", Generation: 1}
	refused := errors.Is(store.Acquire(ctx, third), ErrProjectCapacityExceeded)
	if (spent >= 2) != refused {
		t.Fatalf("the count says %d spent of 2 and the store %s; the two readers disagree",
			spent, map[bool]string{true: "refused", false: "admitted"}[refused])
	}
}

func TestJobLease_CountsAgainstBudget(t *testing.T) {
	now := time.Unix(0, 0).Add(time.Hour)
	fresh := now.UnixNano()
	cases := []struct {
		name  string
		lease *JobLease
		want  bool
	}{
		{"nil", nil, false},
		{"live and budgeted", &JobLease{HeartbeatAt: fresh}, true},
		{"live and off budget", &JobLease{HeartbeatAt: fresh, OffBudget: true}, false},
		{"released", &JobLease{HeartbeatAt: fresh, Released: true}, false},
		{"stale", &JobLease{HeartbeatAt: now.Add(-2 * leaseTestTTL).UnixNano()}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lease.CountsAgainstBudget(now, leaseTestTTL); got != tc.want {
				t.Fatalf("CountsAgainstBudget = %v, want %v", got, tc.want)
			}
		})
	}
}
