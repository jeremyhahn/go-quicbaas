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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
)

// =============================================================================
// Job lease — one node owns one job at a time, durably
// =============================================================================
//
// A JOB THAT NO NODE DURABLY OWNS IS A JOB TWO NODES CAN RUN. Before this
// record existed a job lived only in the process that started it: a second
// node resuming the same task had no way to learn that a first node was still
// on it, and a node that died mid-job left nothing behind that said so. The
// lease is that record. It is taken before the first session is spawned, kept
// alive by a heartbeat while the loop runs, and RELEASED — never deleted —
// when the loop ends, so a reader always finds a record that says what
// happened rather than an absence that could mean anything.
//
// THREE WAYS TO TAKE A HELD LEASE, AND NO FOURTH. A contender is admitted when
// the holder released, when the holder's generation is lower, or when the
// holder's heartbeat is older than the TTL. A lower generation is an earlier
// attempt at the same work, which a later attempt supersedes by definition; a
// stale heartbeat is a holder that stopped proving it was alive. Anything
// else — a live holder at the same or a newer generation — declines, and the
// loser learns who holds it.
//
// The one exception is the holder itself: a job re-acquiring its own lease at
// its own generation is admitted, because that is what resumption after a
// restart is — the same job, on the node that owns it, picking up where it
// left off — and refusing it would strand every job whose node restarted
// inside one TTL.
//
// WRITTEN THROUGH cas.Writer.Handover, the same primitive the task claim
// arbiter uses, for the same reason: a compare and a write evaluated together
// is one proposal; a read followed by a write is two, and the window between
// them is where a second owner gets in.

// JobLeasePrefix is the system-plane keyspace holding job leases.
const JobLeasePrefix = "__sys:orch/joblease/"

// unscopedProjectSegment stands in for the project segment of a lease key when
// the job has no project. A key needs a segment there and validateKeySegment
// refuses an empty one; a fixed marker keeps unscoped jobs in one subtree.
const unscopedProjectSegment = "-"

// DefaultJobLeaseTTL is the heartbeat age past which a lease is stale when
// the host configures none.
const DefaultJobLeaseTTL = 90 * time.Second

// SegmentHash encodes a value the caller does not control as a key segment.
//
// A workflow name or a task's source reference is whatever the source calls
// it, and it may contain the key separator by construction. Hashing yields a
// fixed-width segment that is stable across nodes without an escaping scheme
// every contender would have to spell the same way. SHA-256 for collision
// resistance, not secrecy: the value is carried verbatim on the record and is
// never recovered from the key.
func SegmentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// JobLease records which job, on which node, owns one unit of work.
type JobLease struct {
	// TenantID scopes the lease. On the value as well as the key so a
	// fleet-wide listing can attribute every record without parsing keys.
	TenantID string `json:"tenant_id"`

	// ProjectID is the project the job runs under; empty for an unscoped
	// job.
	ProjectID string `json:"project_id,omitempty"`

	// Workflow names the workflow the job runs; hashed into the key.
	Workflow string `json:"workflow,omitempty"`

	// Ref identifies the work: a task's source reference, the task ID when
	// the task has no source, or the job ID when the job is bound to no
	// task. Hashed into the key.
	Ref string `json:"ref"`

	// TaskID is the task the job is an attempt at, empty for a job started
	// directly.
	TaskID string `json:"task_id,omitempty"`

	// JobID is the job holding the lease.
	JobID string `json:"job_id"`

	// Generation orders holders of the same lease: the attempt ordinal for a
	// bound job, 1 for a job with no task. A later generation supersedes an
	// earlier one.
	Generation int `json:"generation"`

	// Node names the daemon running the job's control loop.
	Node string `json:"node,omitempty"`

	// AcquiredAt is the Unix-nanosecond instant the lease was taken.
	AcquiredAt int64 `json:"acquired_at"`

	// HeartbeatAt is the Unix-nanosecond instant the holder last proved it
	// was alive.
	HeartbeatAt int64 `json:"heartbeat_at"`

	// Backend names the agent backend the job's work runs on, when the job
	// knows it before it starts.
	//
	// IT IS ON THE LEASE SO A SHARED BACKEND CAN BE BOUNDED. A project quota
	// bounds one project and a fleet quota bounds worker slots; neither can
	// express "this MODEL SERVER can serve two things at once", which is what
	// a locally hosted model is. Measured on the burn-in fleet 2026-09-14:
	// five open pull requests on ONE project each got a review at the same
	// moment — reviews are OffBudget, so nothing capped them — and all five
	// queued on a single GPU. A trivial prompt that normally answers in two
	// seconds took 115, and no review finished in 37 minutes.
	//
	// Empty means the job did not declare a backend and no backend quota
	// applies to it, which is every job that existed before this field.
	Backend string `json:"backend,omitempty"`

	// OffBudget marks a lease that does not occupy one of its project's
	// Quotas.MaxConcurrency slots, in either direction: it is admitted while
	// the budget is full, and it does not fill the budget itself.
	//
	// IT IS ON THE RECORD AND NOT JUST IN THE ADMITTING NODE'S HEAD because
	// the budget has three readers — this store's own admission check and
	// the two callers of CountLiveJobLeases — and a node counting somebody
	// else's lease has only the record to read. All three go through
	// CountsAgainstBudget so they cannot disagree; a budget honoured in one
	// place and ignored in another admits a job that then cannot run, which
	// is the intermittent, unreproducible kind of scheduling bug.
	//
	// See JobLeasePolicy.OffProjectBudget for which jobs set it and why.
	OffBudget bool `json:"off_budget,omitempty"`

	// AwaitingSlot marks an off-budget lease whose job has asked to spend a
	// slot again and was refused because the project is full: an sdlc job in
	// reviewing whose review or CI asked for another worker round
	// (JobStateSpec.OffProjectBudget). It is QUEUED DEMAND, and every reader
	// counts it against the budget so new work cannot take the slot it is
	// waiting for — without it, a busy project's dispatcher refills each slot
	// the moment it frees and the round waits for ever. Leases that are
	// awaiting do not count against EACH OTHER, which is what lets the first
	// of them to ask after a slot frees take it (see HoldsSlot).
	AwaitingSlot bool `json:"awaiting_slot,omitempty"`

	// Released marks a lease the holder gave up. The record stays; a reader
	// finds a released lease, never a missing one.
	Released bool `json:"released,omitempty"`

	// ReleasedAt is the Unix-nanosecond instant of the release.
	ReleasedAt int64 `json:"released_at,omitempty"`
}

// Key builds the lease's key, validating the raw segments.
func (l *JobLease) Key() (string, error) {
	return jobLeaseKey(l.TenantID, l.ProjectID, l.Workflow, l.Ref)
}

// Live reports whether the lease is held and its heartbeat is within ttl of
// now.
func (l *JobLease) Live(now time.Time, ttl time.Duration) bool {
	return !l.Released && !l.Stale(now, ttl)
}

// CountsAgainstBudget reports whether this lease occupies one of its
// project's Quotas.MaxConcurrency slots at now.
//
// IT IS THE ONE ANSWER EVERY READER OF THE BUDGET USES — the capacity
// store's admission check, the task dispatcher and the task-source poller —
// so a lease that is off the budget is off it everywhere. See
// JobLease.OffBudget.
//
// A lease counts while it holds a slot, and while it is off the budget but
// awaiting one: see JobLease.AwaitingSlot.
func (l *JobLease) CountsAgainstBudget(now time.Time, ttl time.Duration) bool {
	if l == nil || !l.Live(now, ttl) {
		return false
	}
	return !l.OffBudget || l.AwaitingSlot
}

// HoldsSlot reports whether this lease occupies one of its project's
// Quotas.MaxConcurrency slots at now: live and on the budget. A lease that is
// only awaiting a slot does not hold one.
//
// It is what a job taking its slot BACK is counted against. A job already
// waiting must not be refused by another job waiting beside it — two waiters
// that each counted the other would both wait for ever — so it is compared
// with the slots actually held, and new work is compared with those plus the
// queue (CountsAgainstBudget).
func (l *JobLease) HoldsSlot(now time.Time, ttl time.Duration) bool {
	return l != nil && !l.OffBudget && l.Live(now, ttl)
}

// Stale reports whether the heartbeat is older than ttl at now. A released
// lease is not stale — it is finished, which is a different fact.
func (l *JobLease) Stale(now time.Time, ttl time.Duration) bool {
	if l.Released {
		return false
	}
	return time.Unix(0, l.HeartbeatAt).Add(ttl).Before(now)
}

// validate refuses a lease that could not be keyed or attributed.
func (l *JobLease) validate() error {
	if l == nil {
		return &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "lease must not be nil"}
	}
	if l.JobID == "" {
		return &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a lease must name the job holding it"}
	}
	if l.Generation < 1 {
		return &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a lease generation starts at 1"}
	}
	if l.Ref == "" {
		return &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a lease must name the work it holds"}
	}
	_, err := l.Key()
	return err
}

// sameSubject reports whether two leases name the same unit of work.
func (l *JobLease) sameSubject(other *JobLease) bool {
	return l.TenantID == other.TenantID && l.ProjectID == other.ProjectID &&
		l.Workflow == other.Workflow && l.Ref == other.Ref
}

// jobLeaseKey builds the key for one unit of work.
//
// Tenant and project are validated as key segments, exactly as the
// tenant-plane keys are, so a crafted identifier cannot escape its subtree.
// Workflow and ref are hashed because they are not this package's to shape.
func jobLeaseKey(tenantID, projectID, workflow, ref string) (string, error) {
	if err := validateKeySegment("tenant_id", tenantID); err != nil {
		return "", jobLeaseKeyError(err)
	}
	project := unscopedProjectSegment
	if projectID != "" {
		if err := validateKeySegment("project_id", projectID); err != nil {
			return "", jobLeaseKeyError(err)
		}
		project = projectID
	}
	return jobLeaseScope(tenantID, project) + SegmentHash(workflow) + keySeparator + SegmentHash(ref), nil
}

// jobLeaseScope is the prefix every lease under one (tenant, project) shares.
// An empty tenant is the whole keyspace; an empty project is the whole tenant.
func jobLeaseScope(tenantID, projectID string) string {
	if tenantID == "" {
		return JobLeasePrefix
	}
	scope := JobLeasePrefix + tenantID + keySeparator
	if projectID == "" {
		return scope
	}
	return scope + projectID + keySeparator
}

// jobLeaseKeyError renders a key-segment refusal in this record's vocabulary.
func jobLeaseKeyError(err error) error {
	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		return err
	}
	return &JobLeaseError{
		Sentinel: ErrJobLeaseInvalidInput,
		Message:  segment.Field + " " + segment.Message,
		Cause:    err,
	}
}

// jobLeaseAdmits is the ONE decision about a held lease: whether next may take
// it from held at now. Both stores ask it, so they cannot disagree.
func jobLeaseAdmits(held, next *JobLease, now time.Time, ttl time.Duration) bool {
	switch {
	case held.Released:
		return true
	case held.Generation < next.Generation:
		return true
	case held.Generation == next.Generation && held.JobID == next.JobID:
		// The holder re-acquiring its own lease: resumption, not contention.
		return true
	default:
		return held.Stale(now, ttl)
	}
}

// decodeJobLease reads a stored record, refusing one that names no holder.
func decodeJobLease(held []byte) (*JobLease, error) {
	var current JobLease
	if err := json.Unmarshal(held, &current); err != nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseUnreadable, Message: "lease record does not decode", Cause: err}
	}
	if current.JobID == "" || current.Generation < 1 {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseUnreadable, Message: "lease record names no holder"}
	}
	return &current, nil
}

// encodeJobLease writes a record.
func encodeJobLease(lease *JobLease) ([]byte, error) {
	data, err := json.Marshal(lease)
	if err != nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.encode", Cause: err}
	}
	return data, nil
}

// =============================================================================
// The store
// =============================================================================

// JobLeaseStore holds job leases and decides contention over them.
//
// Two implementations, chosen by the caller for the store's consistency
// domain, for the reason TaskClaimArbiter has two: a transaction-plane store
// for a replicated KV, and a single-process store for a KV that lives in this
// process and nowhere else. Neither is a fallback for the other.
type JobLeaseStore interface {
	// Acquire takes the lease for this job, or reports who holds it with a
	// JobLeaseTakenError. On success the lease's timestamps are filled in.
	Acquire(ctx context.Context, lease *JobLease) error

	// Heartbeat proves the holder is alive. A JobSupersededError means another
	// job took the lease and this one must stop. A released lease is left
	// alone.
	Heartbeat(ctx context.Context, lease *JobLease) error

	// Release marks the lease given up. Idempotent, and a no-op when another
	// job holds the record: releasing a lease one no longer holds must not
	// end the work of whoever does.
	Release(ctx context.Context, lease *JobLease) error

	// Rebudget writes lease.OffBudget and lease.AwaitingSlot onto the record
	// this job holds, moving it off or back onto its project's budget without
	// giving up the lease. A JobSupersededError means another job holds the
	// record; a released record is left alone. A capacity decorator refuses a
	// move BACK ONTO the budget the project has no slot for.
	Rebudget(ctx context.Context, lease *JobLease) error

	// Get reads the lease for one unit of work.
	Get(ctx context.Context, tenantID, projectID, workflow, ref string) (*JobLease, error)

	// List returns every lease under (tenantID, projectID), sorted by key. An
	// empty projectID lists the tenant; an empty tenantID lists everything.
	List(ctx context.Context, tenantID, projectID string) ([]*JobLease, error)
}

// jobLeaseClock is the clock a store reads; injectable so a test can age a
// heartbeat without waiting for it.
type jobLeaseClock func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

// =============================================================================
// The transaction-plane store
// =============================================================================

// transactionJobLeaseStore decides leases through go-qrdb's transaction plane.
type transactionJobLeaseStore struct {
	writer *cas.Writer
	kv     cas.Reader
	ttl    time.Duration
	now    jobLeaseClock
}

// Compile-time interface satisfaction check.
var _ JobLeaseStore = (*transactionJobLeaseStore)(nil)

// NewTransactionJobLeaseStore builds a lease store over go-qrdb's transaction
// and KV planes, failing closed on a missing handle or a non-positive TTL.
func NewTransactionJobLeaseStore(tx cas.Transactor, kv cas.Reader, ttl time.Duration) (JobLeaseStore, error) {
	if ttl <= 0 {
		return nil, &JobLeaseError{
			Sentinel: ErrJobLeaseInvalidInput,
			Message:  "a lease TTL must be positive; a zero TTL makes every holder stale on the next read",
		}
	}
	writer, err := cas.NewWriter(tx, kv, func(op string, cause error) error {
		return &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease." + op, Cause: cause}
	})
	switch {
	case errors.Is(err, cas.ErrNilTransactor):
		return nil, &JobLeaseError{
			Sentinel: ErrJobLeaseInvalidInput,
			Message:  "a lease is decided by transaction; a store without a transactor decides nothing",
		}
	case errors.Is(err, cas.ErrNilReader):
		return nil, &JobLeaseError{
			Sentinel: ErrJobLeaseInvalidInput,
			Message:  "a guarded write compares against the record it read; a store without a reader cannot guard",
		}
	case err != nil:
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "NewTransactionJobLeaseStore", Cause: err}
	}
	return &transactionJobLeaseStore{writer: writer, kv: kv, ttl: ttl, now: utcNow}, nil
}

// Acquire implements JobLeaseStore.
func (s *transactionJobLeaseStore) Acquire(ctx context.Context, lease *JobLease) error {
	if err := lease.validate(); err != nil {
		return err
	}
	key, err := lease.Key()
	if err != nil {
		return err
	}
	now := s.now()
	next := *lease
	next.AcquiredAt = now.UnixNano()
	next.HeartbeatAt = now.UnixNano()
	next.Released = false
	next.ReleasedAt = 0
	data, err := encodeJobLease(&next)
	if err != nil {
		return err
	}

	// Captured from the decline branch so the loser's error names the holder.
	var holder *JobLease
	won, err := s.writer.Handover(ctx, key, data, func(held []byte) (cas.Decision, error) {
		current, decodeErr := decodeJobLease(held)
		if decodeErr != nil {
			return cas.Decline, decodeErr
		}
		if !current.sameSubject(&next) {
			return cas.Decline, &JobLeaseError{Sentinel: ErrJobLeaseSubjectMismatch, Message: "record at " + key}
		}
		if !jobLeaseAdmits(current, &next, now, s.ttl) {
			holder = current
			return cas.Decline, nil
		}
		return cas.Take, nil
	})
	if err != nil {
		return err
	}
	if !won {
		return jobLeaseTaken(&next, holder)
	}
	*lease = next
	return nil
}

// Heartbeat implements JobLeaseStore.
func (s *transactionJobLeaseStore) Heartbeat(ctx context.Context, lease *JobLease) error {
	return s.rewrite(ctx, lease, func(held *JobLease, now time.Time) (*JobLease, bool, error) {
		if held.JobID != lease.JobID {
			return nil, false, &JobSupersededError{JobID: lease.JobID, By: held.Generation, Node: held.Node}
		}
		if held.Released {
			return nil, false, nil
		}
		next := *held
		next.HeartbeatAt = now.UnixNano()
		return &next, true, nil
	})
}

// Rebudget implements JobLeaseStore.
func (s *transactionJobLeaseStore) Rebudget(ctx context.Context, lease *JobLease) error {
	return s.rewrite(ctx, lease, rebudgetRewrite(lease))
}

// Release implements JobLeaseStore.
func (s *transactionJobLeaseStore) Release(ctx context.Context, lease *JobLease) error {
	return s.rewrite(ctx, lease, func(held *JobLease, now time.Time) (*JobLease, bool, error) {
		if held.JobID != lease.JobID || held.Generation != lease.Generation || held.Released {
			return nil, false, nil
		}
		next := *held
		next.Released = true
		next.ReleasedAt = now.UnixNano()
		return &next, true, nil
	})
}

// leaseRewrite decides what a guarded rewrite writes over held. It returns
// the next record and true to write, false to leave the record alone, or an
// error to stop.
type leaseRewrite func(held *JobLease, now time.Time) (*JobLease, bool, error)

// rebudgetRewrite is the ONE decision both stores make for Rebudget: the
// holder's own record takes the budget standing lease asks for, and proves it
// is alive in the same write. HeartbeatAt advances with it because the
// transaction store's value-compare swap relies on no record recurring.
func rebudgetRewrite(lease *JobLease) leaseRewrite {
	return func(held *JobLease, now time.Time) (*JobLease, bool, error) {
		if held.JobID != lease.JobID {
			return nil, false, &JobSupersededError{JobID: lease.JobID, By: held.Generation, Node: held.Node}
		}
		if held.Released {
			return nil, false, nil
		}
		next := *held
		next.OffBudget = lease.OffBudget
		next.AwaitingSlot = lease.AwaitingSlot
		next.HeartbeatAt = now.UnixNano()
		return &next, true, nil
	}
}

// rewrite performs a read-decide-swap on the lease's record until the swap
// wins or the decision says stop.
//
// Value-compare Swap is sound here because the record is monotonic: every
// write advances HeartbeatAt or sets Released, so bytes once written do not
// recur.
func (s *transactionJobLeaseStore) rewrite(ctx context.Context, lease *JobLease, decide leaseRewrite) error {
	if err := lease.validate(); err != nil {
		return err
	}
	key, err := lease.Key()
	if err != nil {
		return err
	}
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		held, err := s.read(ctx, key)
		if err != nil {
			return err
		}
		current, err := decodeJobLease(held)
		if err != nil {
			return err
		}
		if !current.sameSubject(lease) {
			return &JobLeaseError{Sentinel: ErrJobLeaseSubjectMismatch, Message: "record at " + key}
		}
		next, write, err := decide(current, s.now())
		if err != nil {
			return err
		}
		if !write {
			return nil
		}
		data, err := encodeJobLease(next)
		if err != nil {
			return err
		}
		won, err := s.writer.Swap(ctx, key, held, data)
		if err != nil {
			return err
		}
		if won {
			*lease = *next
			return nil
		}
	}
	return &JobLeaseError{
		Sentinel: ErrJobLeaseBackend,
		Message:  "lease.swap: the record at " + key + " changed under every attempt",
		Cause:    cas.ErrStalled,
	}
}

// read fetches one record, naming a missing one.
func (s *transactionJobLeaseStore) read(ctx context.Context, key string) ([]byte, error) {
	held, err := s.kv.Get(ctx, key)
	if err != nil {
		var missing *transport.KeyNotFoundError
		if errors.As(err, &missing) {
			return nil, &JobLeaseNotFoundError{Key: key}
		}
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.read", Cause: err}
	}
	if len(held) == 0 {
		return nil, &JobLeaseNotFoundError{Key: key}
	}
	return held, nil
}

// Get implements JobLeaseStore.
func (s *transactionJobLeaseStore) Get(ctx context.Context, tenantID, projectID, workflow, ref string) (*JobLease, error) {
	key, err := jobLeaseKey(tenantID, projectID, workflow, ref)
	if err != nil {
		return nil, err
	}
	held, err := s.read(ctx, key)
	if err != nil {
		return nil, err
	}
	return decodeJobLease(held)
}

// List implements JobLeaseStore.
func (s *transactionJobLeaseStore) List(ctx context.Context, tenantID, projectID string) ([]*JobLease, error) {
	if tenantID == "" && projectID != "" {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a project listing needs its tenant"}
	}
	records, err := s.kv.Scan(ctx, jobLeaseScope(tenantID, projectID))
	if err != nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.scan", Cause: err}
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	leases := make([]*JobLease, 0, len(keys))
	for _, key := range keys {
		lease, err := decodeJobLease(records[key])
		if err != nil {
			return nil, err
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

// =============================================================================
// The single-process store
// =============================================================================

// processJobLeaseStore decides leases inside one process.
//
// THIS ARBITRATES NOTHING ACROSS NODES. It is the complete answer for a
// deployment whose orchestration state lives in this process — a standalone
// boot — where the only contention is between goroutines, and it exists so
// the caller must say which domain its store serves. A mutex rather than a
// lock-free structure: every operation is a read-decide-write on one record,
// which is a compare-and-swap in all but name, and the map is small.
type processJobLeaseStore struct {
	mu     sync.Mutex
	leases map[string]*JobLease
	ttl    time.Duration
	now    jobLeaseClock
}

// Compile-time interface satisfaction check.
var _ JobLeaseStore = (*processJobLeaseStore)(nil)

// NewProcessJobLeaseStore returns the lease store for a single-process domain.
func NewProcessJobLeaseStore(ttl time.Duration) (JobLeaseStore, error) {
	if ttl <= 0 {
		return nil, &JobLeaseError{
			Sentinel: ErrJobLeaseInvalidInput,
			Message:  "a lease TTL must be positive; a zero TTL makes every holder stale on the next read",
		}
	}
	return &processJobLeaseStore{leases: map[string]*JobLease{}, ttl: ttl, now: utcNow}, nil
}

// Acquire implements JobLeaseStore.
func (s *processJobLeaseStore) Acquire(_ context.Context, lease *JobLease) error {
	if err := lease.validate(); err != nil {
		return err
	}
	key, err := lease.Key()
	if err != nil {
		return err
	}
	now := s.now()
	next := *lease
	next.AcquiredAt = now.UnixNano()
	next.HeartbeatAt = now.UnixNano()
	next.Released = false
	next.ReleasedAt = 0

	s.mu.Lock()
	defer s.mu.Unlock()
	if held, ok := s.leases[key]; ok {
		if !held.sameSubject(&next) {
			return &JobLeaseError{Sentinel: ErrJobLeaseSubjectMismatch, Message: "record at " + key}
		}
		if !jobLeaseAdmits(held, &next, now, s.ttl) {
			return jobLeaseTaken(&next, held)
		}
	}
	stored := next
	s.leases[key] = &stored
	*lease = next
	return nil
}

// Heartbeat implements JobLeaseStore.
func (s *processJobLeaseStore) Heartbeat(_ context.Context, lease *JobLease) error {
	return s.rewrite(lease, func(held *JobLease, now time.Time) (*JobLease, bool, error) {
		if held.JobID != lease.JobID {
			return nil, false, &JobSupersededError{JobID: lease.JobID, By: held.Generation, Node: held.Node}
		}
		if held.Released {
			return nil, false, nil
		}
		next := *held
		next.HeartbeatAt = now.UnixNano()
		return &next, true, nil
	})
}

// Rebudget implements JobLeaseStore.
func (s *processJobLeaseStore) Rebudget(_ context.Context, lease *JobLease) error {
	return s.rewrite(lease, rebudgetRewrite(lease))
}

// Release implements JobLeaseStore.
func (s *processJobLeaseStore) Release(_ context.Context, lease *JobLease) error {
	return s.rewrite(lease, func(held *JobLease, now time.Time) (*JobLease, bool, error) {
		if held.JobID != lease.JobID || held.Generation != lease.Generation || held.Released {
			return nil, false, nil
		}
		next := *held
		next.Released = true
		next.ReleasedAt = now.UnixNano()
		return &next, true, nil
	})
}

// rewrite is the single-process read-decide-write.
func (s *processJobLeaseStore) rewrite(lease *JobLease, decide leaseRewrite) error {
	if err := lease.validate(); err != nil {
		return err
	}
	key, err := lease.Key()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.leases[key]
	if !ok {
		return &JobLeaseNotFoundError{Key: key}
	}
	if !held.sameSubject(lease) {
		return &JobLeaseError{Sentinel: ErrJobLeaseSubjectMismatch, Message: "record at " + key}
	}
	next, write, err := decide(held, s.now())
	if err != nil {
		return err
	}
	if !write {
		return nil
	}
	stored := *next
	s.leases[key] = &stored
	*lease = *next
	return nil
}

// Get implements JobLeaseStore.
func (s *processJobLeaseStore) Get(_ context.Context, tenantID, projectID, workflow, ref string) (*JobLease, error) {
	key, err := jobLeaseKey(tenantID, projectID, workflow, ref)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.leases[key]
	if !ok {
		return nil, &JobLeaseNotFoundError{Key: key}
	}
	copied := *held
	return &copied, nil
}

// List implements JobLeaseStore.
func (s *processJobLeaseStore) List(_ context.Context, tenantID, projectID string) ([]*JobLease, error) {
	if tenantID == "" && projectID != "" {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a project listing needs its tenant"}
	}
	scope := jobLeaseScope(tenantID, projectID)
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.leases))
	for key := range s.leases {
		if strings.HasPrefix(key, scope) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	leases := make([]*JobLease, 0, len(keys))
	for _, key := range keys {
		copied := *s.leases[key]
		leases = append(leases, &copied)
	}
	return leases, nil
}

// =============================================================================
// Project capacity
// =============================================================================

// projectCapacityJobLeaseStore refuses a lease that would put a project over
// its Quotas.MaxConcurrency, then defers to the store beneath it.
//
// THE COUNT AND THE ACQUIRE ARE TWO OPERATIONS, and the window between them is
// stated rather than hidden: two nodes admitting the last slot at once can
// both count max-1 and both acquire. The quota is a budget a project declares
// about itself, not an isolation boundary, and closing that window would take
// a multi-key transaction the guarded-write primitive does not offer. The
// lease TTL bounds how long a dead holder counts against the budget.
type projectCapacityJobLeaseStore struct {
	JobLeaseStore
	projects ProjectStore
	ttl      time.Duration
	now      jobLeaseClock
}

// NewProjectCapacityJobLeaseStore wraps store so Acquire honours each
// project's MaxConcurrency. A nil project store is refused: a capacity check
// with nothing to read quotas from would admit everything while claiming to
// check.
func NewProjectCapacityJobLeaseStore(store JobLeaseStore, projects ProjectStore, ttl time.Duration) (JobLeaseStore, error) {
	if store == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the lease store it admits into"}
	}
	if projects == nil {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the project store that holds the quotas"}
	}
	if ttl <= 0 {
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the TTL that decides which leases are live"}
	}
	return &projectCapacityJobLeaseStore{JobLeaseStore: store, projects: projects, ttl: ttl, now: utcNow}, nil
}

// Acquire implements JobLeaseStore.
func (s *projectCapacityJobLeaseStore) Acquire(ctx context.Context, lease *JobLease) error {
	if err := lease.validate(); err != nil {
		return err
	}
	if lease.ProjectID != "" && !lease.OffBudget {
		if err := s.admit(ctx, lease, (*JobLease).CountsAgainstBudget); err != nil {
			return err
		}
	}
	return s.JobLeaseStore.Acquire(ctx, lease)
}

// Rebudget implements JobLeaseStore.
//
// ONLY A MOVE BACK ONTO THE BUDGET IS ADMITTED. Leaving it frees a slot and is
// never refused. Returning is counted against the slots other jobs HOLD, not
// the queue: the job asking may itself be queued (AwaitingSlot), and a queue
// that counted its own members would never move. See JobLease.HoldsSlot.
func (s *projectCapacityJobLeaseStore) Rebudget(ctx context.Context, lease *JobLease) error {
	if err := lease.validate(); err != nil {
		return err
	}
	if lease.ProjectID != "" && !lease.OffBudget {
		if err := s.admit(ctx, lease, (*JobLease).HoldsSlot); err != nil {
			return err
		}
	}
	return s.JobLeaseStore.Rebudget(ctx, lease)
}

// admit compares the project's leases that occupy the budget — by counts —
// against its quota.
//
// AN OFF-BUDGET CONTENDER IS NEVER ASKED HERE. A job that does not spend a
// worker slot cannot be the job that overruns the budget, and queueing it
// behind the work it exists to unblock is how a project at max_concurrency 1
// stops reviewing its own pull requests: the review waits for the worker, and
// the next worker takes the slot the review was waiting for. See
// JobLease.OffBudget.
func (s *projectCapacityJobLeaseStore) admit(ctx context.Context, lease *JobLease, counts func(*JobLease, time.Time, time.Duration) bool) error {
	project, err := s.projects.Get(ctx, lease.TenantID, lease.ProjectID)
	if err != nil {
		var missing *ProjectStoreNotFoundError
		if errors.As(err, &missing) {
			// A job under a project this store does not hold has no quota to
			// honour; the role matrix, not this check, decides whether it
			// may run at all.
			return nil
		}
		return &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.capacity: project " + lease.ProjectID, Cause: err}
	}
	limit := project.Quotas.MaxConcurrency
	if limit <= 0 {
		return nil
	}
	leases, err := s.List(ctx, lease.TenantID, lease.ProjectID)
	if err != nil {
		return err
	}
	now := s.now()
	live := 0
	for _, held := range leases {
		if held.JobID != lease.JobID && counts(held, now, s.ttl) {
			live++
		}
	}
	if live >= limit {
		return &ProjectCapacityExceededError{
			TenantID:  lease.TenantID,
			ProjectID: lease.ProjectID,
			Live:      live,
			Limit:     limit,
			RetryAt:   now.Add(s.ttl),
		}
	}
	return nil
}

// =============================================================================
// Fleet capacity
// =============================================================================

// fleetCapacityJobLeaseStore refuses a lease that would put the WHOLE
// DEPLOYMENT over its worker-slot capacity, then defers to the store beneath
// it.
//
// WHY THIS EXISTS BESIDE THE ADMISSION PLAN RATHER THAN INSTEAD OF IT. The plan
// is computed once per sweep and divides the fleet fairly among the projects
// asking; this is the last-ditch check at the one point every job must pass
// through, and it catches what no sweep sees — a job started directly, an
// auto-spawned task, a second node planning against the same fleet from its own
// reading. It is the exact counterpart of projectCapacityJobLeaseStore, which
// does the same job for the project axis, and it reads its capacity from the
// SAME FleetCapacityReader the plan reads, so the two cannot bound the fleet by
// different numbers.
//
// THE COUNT AND THE ACQUIRE ARE TWO OPERATIONS, with the same stated window the
// project check has: two nodes admitting the last slot at once can both count
// limit-1 and both acquire. The lease TTL bounds how long a dead holder counts.
//
// AN UNDECLARED CAPACITY ADMITS. A deployment that could not be measured has
// not said it has no slots, and refusing every job on an unmeasured fleet would
// be the outage this file exists to prevent, inverted.
type fleetCapacityJobLeaseStore struct {
	JobLeaseStore
	capacity FleetCapacityReader
	ttl      time.Duration
	now      jobLeaseClock
}

// NewFleetCapacityJobLeaseStore wraps store so Acquire honours the
// deployment's worker-slot capacity. A nil capacity reader is refused: a
// capacity check with nothing to read the bound from would admit everything
// while claiming to check.
func NewFleetCapacityJobLeaseStore(store JobLeaseStore, capacity FleetCapacityReader, ttl time.Duration) (JobLeaseStore, error) {
	switch {
	case store == nil:
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the lease store it admits into"}
	case capacity == nil:
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the reader that answers how many worker slots the fleet has"}
	case ttl <= 0:
		return nil, &JobLeaseError{Sentinel: ErrJobLeaseInvalidInput, Message: "a capacity check needs the TTL that decides which leases are live"}
	}
	return &fleetCapacityJobLeaseStore{JobLeaseStore: store, capacity: capacity, ttl: ttl, now: utcNow}, nil
}

// Acquire implements JobLeaseStore.
//
// EVERY LEASE IS CHECKED, including one with no project. The fleet's slots are
// held by whatever is running, and an unscoped job occupies one exactly as a
// project's job does — which is also why the count below is taken over every
// tenant rather than over the lease's own.
func (s *fleetCapacityJobLeaseStore) Acquire(ctx context.Context, lease *JobLease) error {
	if err := lease.validate(); err != nil {
		return err
	}
	if err := s.admit(ctx, lease); err != nil {
		return err
	}
	return s.JobLeaseStore.Acquire(ctx, lease)
}

// admit compares the deployment's live leases against its capacity.
func (s *fleetCapacityJobLeaseStore) admit(ctx context.Context, lease *JobLease) error {
	now := s.now()
	capacity, err := s.capacity.FleetCapacity(ctx, now)
	if err != nil {
		return &JobLeaseError{Sentinel: ErrJobLeaseBackend, Message: "lease.fleet_capacity", Cause: err}
	}
	if !capacity.Known() {
		return nil
	}
	held, err := s.List(ctx, "", "")
	if err != nil {
		return err
	}
	live := 0
	for _, other := range held {
		// The lease being renewed is not a second holder of its own slot. The
		// project check makes the same exclusion for the same reason: a
		// heartbeat that counted itself would refuse the holder at the limit.
		if other != nil && other.JobID != lease.JobID && other.Live(now, s.ttl) {
			live++
		}
	}
	if live >= capacity.Slots {
		return &FleetCapacityExceededError{
			Live:    live,
			Limit:   capacity.Slots,
			Source:  capacity.Source,
			RetryAt: now.Add(s.ttl),
		}
	}
	return nil
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrJobLeaseInvalidInput indicates a lease or a store handle that could
	// not be used.
	ErrJobLeaseInvalidInput = errors.New("job lease: invalid input")

	// ErrJobLeaseUnreadable indicates a stored record that does not decode
	// or names no holder.
	ErrJobLeaseUnreadable = errors.New("job lease: record unreadable")

	// ErrJobLeaseSubjectMismatch indicates a record at the lease's key that
	// names different work.
	ErrJobLeaseSubjectMismatch = errors.New("job lease: record names other work")

	// ErrJobLeaseBackend indicates the store beneath the lease failed.
	ErrJobLeaseBackend = errors.New("job lease: backend failure")

	// ErrJobLeaseTaken indicates a live holder declined the contender.
	ErrJobLeaseTaken = errors.New("job lease: held by another job")

	// ErrJobSuperseded indicates a newer job took the lease from the holder.
	ErrJobSuperseded = errors.New("job lease: superseded")

	// ErrJobLeaseNotFound indicates no record exists for the work.
	ErrJobLeaseNotFound = errors.New("job lease: not found")

	// ErrProjectCapacityExceeded indicates the project's MaxConcurrency is
	// already held by live leases.
	ErrProjectCapacityExceeded = errors.New("job lease: project concurrency exceeded")

	// ErrBackendCapacityExceeded indicates a backend's concurrency limit is
	// already held by live leases. See backendCapacityJobLeaseStore: it
	// bounds a shared model server, which neither the project nor the fleet
	// quota can express.
	ErrBackendCapacityExceeded = errors.New("job lease: backend concurrency exceeded")

	// ErrFleetCapacityExceeded indicates the deployment's worker slots are all
	// held by live leases, whatever any single project's quota permits.
	ErrFleetCapacityExceeded = errors.New("job lease: fleet capacity exceeded")
)

// JobLeaseError is a lease store failure with a sentinel and a message.
type JobLeaseError struct {
	Sentinel error
	Message  string
	Cause    error
}

// Error implements the error interface.
func (e *JobLeaseError) Error() string {
	msg := e.Sentinel.Error()
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the sentinel and the cause to errors.Is and errors.As.
func (e *JobLeaseError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Sentinel}
	}
	return []error{e.Sentinel, e.Cause}
}

// JobLeaseTakenError reports the holder a contender lost to.
type JobLeaseTakenError struct {
	// JobID is the contender that was declined.
	JobID string

	// HeldBy names the node holding the lease.
	HeldBy string

	// HeldByJobID is the job holding it.
	HeldByJobID string

	// Generation is the holder's generation.
	Generation int
}

// jobLeaseTaken builds the loser's error from the record that declined it.
func jobLeaseTaken(contender, holder *JobLease) error {
	taken := &JobLeaseTakenError{JobID: contender.JobID}
	if holder != nil {
		taken.HeldBy = holder.Node
		taken.HeldByJobID = holder.JobID
		taken.Generation = holder.Generation
	}
	return taken
}

// Error implements the error interface.
func (e *JobLeaseTakenError) Error() string {
	return ErrJobLeaseTaken.Error() + ": job=" + e.JobID + " held_by=" + e.HeldBy +
		" holder_job=" + e.HeldByJobID + " generation=" + strconv.Itoa(e.Generation)
}

// Unwrap returns the sentinel for errors.Is support.
func (e *JobLeaseTakenError) Unwrap() error { return ErrJobLeaseTaken }

// JobSupersededError reports that a newer job took the lease from a running
// one, which must stop.
type JobSupersededError struct {
	// JobID is the job that was superseded.
	JobID string

	// By is the generation now holding the lease.
	By int

	// Node names the node holding it.
	Node string
}

// Error implements the error interface.
func (e *JobSupersededError) Error() string {
	return ErrJobSuperseded.Error() + ": job=" + e.JobID + " by_generation=" + strconv.Itoa(e.By) + " node=" + e.Node
}

// Unwrap returns the sentinel for errors.Is support.
func (e *JobSupersededError) Unwrap() error { return ErrJobSuperseded }

// JobLeaseNotFoundError reports a lease that was never written.
type JobLeaseNotFoundError struct {
	Key string
}

// Error implements the error interface.
func (e *JobLeaseNotFoundError) Error() string {
	return ErrJobLeaseNotFound.Error() + ": " + e.Key
}

// Unwrap returns the sentinel for errors.Is support.
func (e *JobLeaseNotFoundError) Unwrap() error { return ErrJobLeaseNotFound }

// ProjectCapacityExceededError reports a project whose concurrency budget is
// spent.
type ProjectCapacityExceededError struct {
	TenantID  string
	ProjectID string

	// Live is how many leases were live when the check ran.
	Live int

	// Limit is the project's MaxConcurrency.
	Limit int

	// RetryAt is when a live lease could have gone stale, so a caller with
	// somewhere to put the wait can wait exactly that long.
	RetryAt time.Time
}

// Error implements the error interface.
func (e *ProjectCapacityExceededError) Error() string {
	return ErrProjectCapacityExceeded.Error() + ": tenant=" + e.TenantID + " project=" + e.ProjectID +
		" live=" + strconv.Itoa(e.Live) + " limit=" + strconv.Itoa(e.Limit)
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ProjectCapacityExceededError) Unwrap() error { return ErrProjectCapacityExceeded }

// FleetCapacityExceededError reports a deployment whose worker slots are all
// held.
//
// IT IS A DIFFERENT ERROR FROM THE PROJECT'S, deliberately, because it sends an
// operator to a different number. "Your project may run three jobs and three are
// running" is fixed by raising a project quota; "the fleet has eight slots and
// eight are running" is fixed by adding a worker, and a shared error would have
// every operator raise their own quota against a fleet that cannot serve it.
type FleetCapacityExceededError struct {
	// Live is how many leases were live across the whole deployment when the
	// check ran.
	Live int

	// Limit is the fleet capacity in force, and Source names where it came
	// from — so the message says whether to add a daemon or raise the
	// configured ceiling.
	Limit  int
	Source FleetCapacitySource

	// RetryAt is when a live lease could have gone stale, so a caller with
	// somewhere to put the wait can wait exactly that long.
	RetryAt time.Time
}

// Error implements the error interface.
func (e *FleetCapacityExceededError) Error() string {
	return ErrFleetCapacityExceeded.Error() + ": live=" + strconv.Itoa(e.Live) +
		" limit=" + strconv.Itoa(e.Limit) + " source=" + e.Source.String()
}

// Unwrap returns the sentinel for errors.Is support.
func (e *FleetCapacityExceededError) Unwrap() error { return ErrFleetCapacityExceeded }
