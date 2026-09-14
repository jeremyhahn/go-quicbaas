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
	"sync/atomic"
	"testing"
)

// errTestStoreClosed is how the engine under go-qrdb reports an operation
// against a store that has been closed: it panics with this, rather than
// returning it.
var errTestStoreClosed = errors.New("pebble: closed")

// closingTestKVStore is an in-memory store that can be closed under a job
// still writing to it, and that reports being closed the way the real engine
// does — by panicking.
type closingTestKVStore struct {
	*peerTestKVStore
	closed atomic.Bool
}

func newClosingTestKVStore() *closingTestKVStore {
	return &closingTestKVStore{peerTestKVStore: newPeerTestKVStore()}
}

// Close makes every later write panic, as the daemon closing storage does.
func (s *closingTestKVStore) Close() { s.closed.Store(true) }

// Put writes, or panics when the store has been closed.
func (s *closingTestKVStore) Put(ctx context.Context, key string, value []byte) error {
	if s.closed.Load() {
		panic(errTestStoreClosed)
	}
	return s.peerTestKVStore.Put(ctx, key, value)
}

// persistTo returns a persist hook that writes each snapshot to store, which
// is what launchJob installs.
func persistTo(t *testing.T, store *closingTestKVStore) func(*JobStatus) {
	t.Helper()
	return func(status *JobStatus) {
		if err := store.Put(context.Background(), jobPersistenceKey(status.TenantID, status.JobID), []byte(status.State)); err != nil {
			t.Errorf("persist job status: %v", err)
		}
	}
}

// runExecute runs the envelope and reports whether a panic escaped it. The
// job goroutine launchJob starts has nothing above it, so a panic that
// escapes here is a dead daemon in production.
func runExecute(t *testing.T, run *JobRun, runner JobRunner) error {
	t.Helper()
	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("a panic escaped the envelope and would have taken the daemon down: %v", recovered)
			}
		}()
		err = run.execute(context.Background(), runner)
	}()
	return err
}

// TestJobEnvelope_AClosedStoreUnderAConcludingJobIsReportedNotPanicked proves
// the shutdown race is survivable: the node closes storage while a job is on
// its way out, the terminal status cannot be written, and the job concludes
// with the typed error instead of panicking out of its goroutine.
func TestJobEnvelope_AClosedStoreUnderAConcludingJobIsReportedNotPanicked(t *testing.T) {
	t.Parallel()
	store := newClosingTestKVStore()
	run := newEnvelopeRun(t, JobRunConfig{PersistFn: persistTo(t, store)})
	runner := &fakeRunner{name: "store-closed-conclude", run: func(context.Context, *JobRun) error {
		// The daemon closes storage while the job is still running.
		store.Close()
		return nil
	}}

	err := runExecute(t, run, runner)

	var closed *StoreClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("execute() = %v, want a StoreClosedError", err)
	}
	if !errors.Is(err, ErrStoreClosed) {
		t.Fatalf("execute() = %v, want it to match ErrStoreClosed", err)
	}
	if closed.JobID != run.JobID {
		t.Fatalf("StoreClosedError job = %q, want %q", closed.JobID, run.JobID)
	}
	if closed.Op != "conclude" {
		t.Fatalf("StoreClosedError op = %q, want %q", closed.Op, "conclude")
	}
}

// TestJobEnvelope_AClosedStoreUnderTheRunnerIsNotReportedAsAPanickingRunner
// proves the classification: the store going away is the node shutting down,
// not a runner that panicked, and the job is not marked as having panicked.
func TestJobEnvelope_AClosedStoreUnderTheRunnerIsNotReportedAsAPanickingRunner(t *testing.T) {
	t.Parallel()
	store := newClosingTestKVStore()
	run := newEnvelopeRun(t, JobRunConfig{PersistFn: persistTo(t, store)})
	runner := &fakeRunner{name: "store-closed-mid-run", run: func(_ context.Context, job *JobRun) error {
		store.Close()
		// The runner's own status write is the one that meets the closed
		// store, exactly as a job persisting mid-shutdown does.
		job.snapshot(JobStateExecuting)
		return nil
	}}

	err := runExecute(t, run, runner)

	var closed *StoreClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("execute() = %v, want a StoreClosedError", err)
	}
	var panicked *JobPanicError
	if errors.As(err, &panicked) {
		t.Fatalf("execute() = %v, want the closed store not to be reported as a runner panic", err)
	}
	if status := run.Status(); status != nil && status.FailureClass == FailureClassPanic {
		t.Fatal("a job the store was closed under must not be classified as a panicking runner")
	}
}

// TestJobEnvelope_ARunnerPanicIsStillReportedAsAPanic proves the guard is
// narrow: a panic that is not the store going away is still the runner's,
// still typed, and still fails the job.
func TestJobEnvelope_ARunnerPanicIsStillReportedAsAPanic(t *testing.T) {
	t.Parallel()
	run := newEnvelopeRun(t, JobRunConfig{})
	runner := &fakeRunner{name: "store-closed-real-panic", run: func(context.Context, *JobRun) error {
		panic("index out of range")
	}}

	err := runExecute(t, run, runner)

	var panicked *JobPanicError
	if !errors.As(err, &panicked) {
		t.Fatalf("execute() = %v, want a JobPanicError", err)
	}
	if errors.Is(err, ErrStoreClosed) {
		t.Fatalf("execute() = %v, want a runner panic not to be mistaken for a closed store", err)
	}
	if status := run.Status(); status == nil || status.FailureClass != FailureClassPanic {
		t.Fatalf("a panicking runner fails its job as a panic, got %+v", status)
	}
}

// TestGuardStoreClosed_ReturnsNilWhenNothingPanics proves the guard is
// transparent on the ordinary path.
func TestGuardStoreClosed_ReturnsNilWhenNothingPanics(t *testing.T) {
	t.Parallel()
	ran := false
	if closed := guardStoreClosed("test", "job-1", func() { ran = true }); closed != nil {
		t.Fatalf("guardStoreClosed() = %v, want nil", closed)
	}
	if !ran {
		t.Fatal("guardStoreClosed must run the function it guards")
	}
}

// TestGuardStoreClosed_ReRaisesAPanicThatIsNotAClosedStore proves the guard
// hides nothing else: a bug still crashes, because a swallowed bug is a
// daemon running on state nothing checked.
func TestGuardStoreClosed_ReRaisesAPanicThatIsNotAClosedStore(t *testing.T) {
	t.Parallel()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		closed := guardStoreClosed("test", "job-1", func() { panic("nil map write") })
		t.Fatalf("guardStoreClosed returned %v instead of re-raising", closed)
	}()
	if recovered != "nil map write" {
		t.Fatalf("re-raised value = %v, want the original panic", recovered)
	}
}

// TestIsStoreClosed_RecognizesHowTheStoreReportsItself proves every way the
// layers below report a closed store is recognized, whether it arrives as an
// error or as a recovered panic value.
func TestIsStoreClosed_RecognizesHowTheStoreReportsItself(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value any
	}{
		{"pebble panic value", errTestStoreClosed},
		{"pebble panic string", "pebble: closed"},
		{"qrdb backend", errors.New("backend: closed")},
		{"qrdb sdk client", errors.New("sdk: client is closed")},
		{"wrapped", &JobPersistenceError{JobID: "j", Message: "put failed: pebble: closed"}},
		{"our own", &StoreClosedError{JobID: "j", Op: "conclude"}},
		{"mixed case", "Pebble: Closed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !IsStoreClosed(tc.value) {
				t.Fatalf("IsStoreClosed(%v) = false, want true", tc.value)
			}
		})
	}
}

// TestIsStoreClosed_RejectsWhatIsNotAClosedStore proves the recognizer does
// not widen: an unrelated failure, an unrelated panic value and nothing at
// all are not the store going away.
func TestIsStoreClosed_RejectsWhatIsNotAClosedStore(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value any
	}{
		{"nil", nil},
		{"unrelated error", errors.New("connection refused")},
		{"closed network connection", errors.New("use of closed network connection")},
		{"unrelated panic string", "index out of range"},
		{"unrelated panic value", 42},
		{"a job error", &JobPanicError{JobID: "j", Runner: "r", Value: "boom"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsStoreClosed(tc.value) {
				t.Fatalf("IsStoreClosed(%v) = true, want false", tc.value)
			}
		})
	}
}

// TestStoreClosedError_MessageNamesTheJobAndOperation proves the error says
// which job's which write met the closed store, and matches the sentinel.
func TestStoreClosedError_MessageNamesTheJobAndOperation(t *testing.T) {
	t.Parallel()
	err := &StoreClosedError{JobID: "job-7", Op: "conclude", Value: "pebble: closed"}
	const want = "orchestration: store closed: op=conclude job=job-7: pebble: closed"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrStoreClosed) {
		t.Fatal("a StoreClosedError matches ErrStoreClosed")
	}

	bare := &StoreClosedError{Op: "drain"}
	if bare.Error() != "orchestration: store closed: op=drain" {
		t.Fatalf("Error() = %q, want the job and value omitted when unknown", bare.Error())
	}
}
