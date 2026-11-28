// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// Package quicbaas provides scheduler job storage using QuicBaaS's KVStore interface.
package quicbaas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// systemNamespace is go-qrdb's reserved internal prefix
// (service.InternalKeyPrefixSystem). Records under it are excluded from
// user-facing List and Scan and refused by MVCC restore, so platform-owned
// scheduler state is not enumerable by an ordinary tenant sweep. Bare "sys:"
// bought none of that, which was docs/18 V24.
const systemNamespace = "__sys:"

// Key prefixes for storage partitioning.
const (
	// SystemJobsPrefix stores system-owned jobs.
	SystemJobsPrefix = "__sys:scheduler/jobs/"

	// TenantJobsPrefixFmt is the format for tenant job keys: t:{tenantID}:scheduler/jobs/.
	TenantJobsPrefixFmt = "t:%s:scheduler/jobs/"

	// ExecutionsPrefix stores job execution history.
	ExecutionsPrefix = "__sys:scheduler/executions/"

	// TenantExecutionsPrefixFmt is the format for tenant execution keys: t:{tenantID}:scheduler/executions/.
	TenantExecutionsPrefixFmt = "t:%s:scheduler/executions/"

	// JobIDCounterKey stores the last job ID allocated. See job_ids.go.
	JobIDCounterKey = "__sys:scheduler/meta/job_id_counter"

	// TenantJobIndexPrefix indexes every tenant-owned job so the due scan can
	// find it. A tenant job lives in the tenant's own namespace, which the
	// platform scan of SystemJobsPrefix never reaches; one entry per job,
	// "<prefix><tenantID>/<jobID>", with the tenant ID as its value. The
	// index lives in the reserved namespace, so a tenant cannot enumerate
	// other tenants' jobs through it.
	TenantJobIndexPrefix = "__sys:scheduler/tenant-jobs/"

	// TenantJobIndexBackfillKey records that the one-time backfill of
	// TenantJobIndexPrefix has indexed every tenant job stored before the
	// index existed (see reconcileTenantJobIndex).
	TenantJobIndexBackfillKey = "__sys:scheduler/meta/tenant_job_index_backfilled"
)

// Storage implements scheduler.JobStorage using QuicBaaS's KVStore interface.
type Storage struct {
	kv  interfaces.KVStore
	ids jobIDAllocator

	// reconcileMu guards lastReconcile, when the tenant job index was last
	// reconciled; the due scan reconciles again after reconcileInterval.
	reconcileMu       sync.Mutex
	lastReconcile     time.Time
	reconcileInterval time.Duration

	// reconcileRun serialises reconciliation runs and guards
	// reconcilePending, the key-prefix shards of the current pass not yet
	// reconciled. A run cut short by its budget leaves them for the next
	// run, which resumes the pass instead of restarting it.
	reconcileRun     sync.Mutex
	reconcilePending []reconcileShard

	// tenantCursor is where the due scan starts in the rotation of tenant
	// namespaces plus the system namespace; each sweep advances it, so a
	// namespace with more due jobs than the limit, system included, cannot
	// starve the namespaces after it.
	tenantCursor atomic.Uint64
}

// tenantJobIndexReconcileInterval is how often the due scan reconciles the
// tenant job index with the tenant job records.
const tenantJobIndexReconcileInterval = 5 * time.Minute

// tenantJobIndexReconcileBudget caps one reconciliation run; a sweep also
// gives it at most half of its own remaining time.
const tenantJobIndexReconcileBudget = 10 * time.Second

// ClusteredStorage is a job store for a clustered deployment: a Storage that
// can also arbitrate firings.
//
// It is a DISTINCT TYPE rather than a flag on Storage so the scheduler's
// fail-closed construction actually holds. scheduler.NewWithClustering
// type-asserts scheduler.FiringClaimer; if ClaimFiring lived on Storage, every
// single-node store would satisfy that assertion and the guard would pass at
// boot only to fail at the first contended firing — the exact "fails at first
// claim, not at startup" defect this design exists to avoid. With the method on
// this type alone, a store built without a claim arbiter cannot be mistaken for
// one that has it, and the compiler enforces it.
type ClusteredStorage struct {
	*Storage

	// writer performs the guarded write that decides a firing. The
	// create/read/decide/swap sequence lives in pkg/cas, shared with
	// pkg/sdlc's coordination leases: it was written twice before it was
	// written once, and two spellings of one rule drift silently because both
	// keep passing their own tests.
	writer *cas.Writer

	// marks reads a job's current claim mark for the advance guard, and
	// deletes it when the job goes away.
	marks ClaimReader

	// strandedFirings counts firings the monotonic mark has made
	// unclaimable. See ClusteredStorage.noteIfStranded.
	strandedFirings atomic.Uint64
}

// Verify the storage types implement their interfaces.
var (
	_ scheduler.JobStorage    = (*Storage)(nil)
	_ scheduler.JobStorage    = (*ClusteredStorage)(nil)
	_ scheduler.FiringClaimer = (*ClusteredStorage)(nil)
)

// NewStorage creates a single-node QuicBaaS-backed job storage.
//
// It carries no firing-claim path, and deliberately does not implement
// scheduler.FiringClaimer. Claims arbitrate between nodes, and a single-node
// deployment has no peer to race, so there is nothing to decide — a different
// topology, not a disabled safeguard.
func NewStorage(kv interfaces.KVStore) *Storage {
	return &Storage{
		kv:  kv,
		ids: &localJobIDs{kv: kv},
		// Construction counts as a reconcile: InitializeJobIDCounter, the
		// open, runs the first one.
		lastReconcile:     time.Now(),
		reconcileInterval: tenantJobIndexReconcileInterval,
	}
}

// NewClusteredStorage creates a job storage for a clustered deployment.
//
// claims is go-qrdb's transaction client, which decides every firing, and marks
// reads the current mark for its guard. Both are
// required: without them there is nothing to arbitrate a contended firing, and a
// cluster running without claims looks perfectly healthy right up to the first
// leadership change. Refusing here means a misconfigured clustered node fails
// to start rather than silently double-firing.
func NewClusteredStorage(kv interfaces.KVStore, claims ClaimTransactor, marks ClaimReader) (*ClusteredStorage, error) {
	if claims == nil || marks == nil {
		return nil, scheduler.ClaimUnsupportedError{Backend: typeName(kv)}
	}
	writer, err := cas.NewWriter(claims, marks, func(op string, cause error) error {
		return &scheduler.ErrStorageError{Op: "ClaimFiring." + op, Cause: cause}
	})
	if err != nil {
		return nil, scheduler.ClaimUnsupportedError{Backend: typeName(kv)}
	}
	// Job IDs are allocated through the same transaction plane that decides
	// firings, so two nodes can never hand out one ID. The writer is separate
	// only so its failures carry the allocation's own error vocabulary.
	idWriter, err := cas.NewWriter(claims, marks, func(op string, cause error) error {
		return &scheduler.ErrStorageError{Op: "AllocateJobID." + op, Cause: cause}
	})
	if err != nil {
		return nil, scheduler.ClaimUnsupportedError{Backend: typeName(kv)}
	}
	return &ClusteredStorage{
		Storage: &Storage{
			kv:                kv,
			ids:               &clusterJobIDs{kv: kv, reader: marks, writer: idWriter},
			lastReconcile:     time.Now(),
			reconcileInterval: tenantJobIndexReconcileInterval,
		},
		writer: writer,
		marks:  marks,
	}, nil
}

// typeName renders a value's dynamic type for error attribution.
func typeName(v any) string {
	return fmt.Sprintf("%T", v)
}

// keyForJob returns the storage key for a job based on ownership.
func keyForJob(job *scheduler.Job) string {
	switch job.OwnerType {
	case scheduler.OwnerTypeSystem, scheduler.OwnerTypeDelegated:
		return fmt.Sprintf("%s%d", SystemJobsPrefix, job.ID)
	case scheduler.OwnerTypeTenant:
		return fmt.Sprintf(TenantJobsPrefixFmt+"%d", job.OwnerID, job.ID)
	default:
		return fmt.Sprintf("%s%d", SystemJobsPrefix, job.ID)
	}
}

// scopeToken returns the discriminator that makes a job ID unique.
//
// A job ID is only unique WITHIN a scope: the system namespace and each tenant
// number their jobs independently, so ID 7 names a system job and a job in
// every tenant. Anything keyed by job must carry this discriminator or it will
// conflate those jobs. The claim mark was first written with a hand-copied
// version of this rule that omitted the scope entirely, so deleting a tenant's
// job 7 erased the system job 7's claim and re-opened a decided firing.
//
// This answers "which scope owns this ID", and is embedded INSIDE a compound
// key. It is deliberately distinct from recordNamespace, which answers a
// different question — see there.
func scopeToken(tenantID string) string {
	if tenantID == "" {
		return "sys:"
	}
	return "t:" + tenantID + ":"
}

// recordNamespace returns the LEADING namespace a top-level scheduler record
// lives under.
//
// Distinct from scopeToken because it answers a different question — "where
// does this record live" rather than "which scope owns this ID" — and the two
// answers genuinely differ for system records. A platform-owned record lives in
// go-qrdb's reserved "__sys:" namespace, which is excluded from user-facing
// List and Scan and refused by MVCC restore; a tenant's record lives in that
// tenant's own namespace, where the tenant is entitled to enumerate it.
//
// Collapsing the two into one function is what a first pass at this rename
// would do, and it produces "__sys:scheduler/claims/__sys:7" — the namespace
// nested inside itself as a discriminator. Two questions, two functions; the
// V4 rule is against two spellings of ONE rule, not against distinguishing
// rules that differ.
func recordNamespace(tenantID string) string {
	if tenantID == "" {
		return systemNamespace
	}
	return "t:" + tenantID + ":"
}

// keyForJobByID returns the storage key for a job ID with tenant context.
func keyForJobByID(id uint64, tenantID string) string {
	return fmt.Sprintf("%sscheduler/jobs/%d", recordNamespace(tenantID), id)
}

// keyForTenantJobIndex returns the index entry that makes a tenant's job
// visible to the due scan. See TenantJobIndexPrefix.
func keyForTenantJobIndex(id uint64, tenantID string) string {
	return fmt.Sprintf("%s%s/%d", TenantJobIndexPrefix, tenantID, id)
}

// parseTenantJobIndexKey returns the job ID a tenant job index entry names.
func parseTenantJobIndexKey(key string) (uint64, bool) {
	rest, ok := strings.CutPrefix(key, TenantJobIndexPrefix)
	if !ok {
		return 0, false
	}
	slash := strings.LastIndexByte(rest, '/')
	if slash < 0 {
		return 0, false
	}
	id, err := strconv.ParseUint(rest[slash+1:], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// keyForExecution returns the storage key for an execution record.
func keyForExecution(exec *scheduler.JobExecution, tenantID string) string {
	if tenantID == "" {
		return fmt.Sprintf("%s%d/%d", ExecutionsPrefix, exec.JobID, exec.ID)
	}
	return fmt.Sprintf(TenantExecutionsPrefixFmt+"%d/%d", tenantID, exec.JobID, exec.ID)
}

// CreateJob stores a new job.
func (s *Storage) CreateJob(ctx context.Context, job *scheduler.Job) error {
	if job == nil {
		return &scheduler.ErrStorageError{Op: "CreateJob", Cause: ErrNilJob}
	}

	// Allocate an ID if the caller did not bring one. System jobs carry a
	// fixed ID at or above scheduler.SystemJobIDFloor and are never allocated.
	if job.ID == 0 {
		id, err := s.ids.next(ctx)
		if err != nil {
			return err
		}
		job.ID = id
	}

	// Set timestamps
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now

	// Marshal job to JSON
	data, err := json.Marshal(job)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "CreateJob.marshal", Cause: err}
	}

	if err := s.indexTenantJob(ctx, "CreateJob", job); err != nil {
		return err
	}

	// Store in QuicBaaS
	key := keyForJob(job)
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &scheduler.ErrStorageError{Op: "CreateJob.put", Cause: err}
	}

	return nil
}

// GetJob retrieves a job by ID.
//
// Only an absent record is reported as scheduler.JobNotFoundError. Any other
// KV failure — no leader, a timeout, a sealed barrier — is an
// ErrStorageError: callers act on "not found" by creating the record (system
// job install) or by dropping a write (a deleted job is not resurrected), and
// a transient failure read as absence would overwrite a live record,
// including one mid-run on another node.
func (s *Storage) GetJob(ctx context.Context, id uint64, tenantID string) (*scheduler.Job, error) {
	key := keyForJobByID(id, tenantID)

	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, &scheduler.JobNotFoundError{JobID: id}
		}
		return nil, &scheduler.ErrStorageError{Op: "GetJob.get", Cause: err}
	}

	if len(data) == 0 {
		return nil, &scheduler.JobNotFoundError{JobID: id}
	}

	var job scheduler.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, &scheduler.ErrStorageError{Op: "GetJob.unmarshal", Cause: err}
	}

	return &job, nil
}

// UpdateJob updates an existing job.
func (s *Storage) UpdateJob(ctx context.Context, job *scheduler.Job) error {
	if job == nil {
		return &scheduler.ErrStorageError{Op: "UpdateJob", Cause: ErrNilJob}
	}

	job.UpdatedAt = time.Now()

	data, err := json.Marshal(job)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "UpdateJob.marshal", Cause: err}
	}

	// Keep the tenant-job index consistent with the record being written.
	if err := s.indexTenantJob(ctx, "UpdateJob", job); err != nil {
		return err
	}

	key := keyForJob(job)
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &scheduler.ErrStorageError{Op: "UpdateJob.put", Cause: err}
	}

	return nil
}

// DeleteJob removes a job.
func (s *Storage) DeleteJob(ctx context.Context, id uint64, tenantID string) error {
	key := keyForJobByID(id, tenantID)

	if err := s.kv.Delete(ctx, key); err != nil {
		return &scheduler.ErrStorageError{Op: "DeleteJob.delete", Cause: err}
	}
	if tenantID != "" {
		if err := s.kv.Delete(ctx, keyForTenantJobIndex(id, tenantID)); err != nil && !dberrors.IsNotFound(err) {
			return &scheduler.ErrStorageError{Op: "DeleteJob.index", Cause: err}
		}
	}

	return nil
}

// ListJobs returns jobs matching the query.
func (s *Storage) ListJobs(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
	if query == nil {
		query = &scheduler.JobQuery{}
	}

	// Determine prefix based on query
	var prefix string
	switch {
	case query.OwnerID != "":
		prefix = fmt.Sprintf(TenantJobsPrefixFmt, query.OwnerID)
	case query.OwnerType == scheduler.OwnerTypeSystem || query.OwnerType == scheduler.OwnerTypeDelegated:
		prefix = SystemJobsPrefix
	default:
		prefix = SystemJobsPrefix // Default to system jobs
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}

	jobs := make([]*scheduler.Job, 0)
	var scanErr error

	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// Skip malformed entries
			return nil
		}

		// Apply filters
		if query.Status != "" && job.Status != query.Status {
			return nil
		}
		if query.Handler != "" && job.Handler != query.Handler {
			return nil
		}
		if query.TargetTenantID != "" && job.TargetTenantID != query.TargetTenantID {
			return nil
		}

		// Apply label filters
		if len(query.Labels) > 0 {
			matches := true
			for k, v := range query.Labels {
				if job.Labels == nil || job.Labels[k] != v {
					matches = false
					break
				}
			}
			if !matches {
				return nil
			}
		}

		jobs = append(jobs, &job)

		// Stop if we've reached the limit
		if len(jobs) >= limit {
			return ErrLimitReached
		}

		return nil
	})

	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, "", &scheduler.ErrStorageError{Op: "ListJobs.scan", Cause: err}
	}
	if scanErr != nil {
		return nil, "", &scheduler.ErrStorageError{Op: "ListJobs.scan", Cause: scanErr}
	}

	// Generate cursor for pagination if we hit the limit
	var nextCursor string
	if len(jobs) >= limit && len(jobs) > 0 {
		lastJob := jobs[len(jobs)-1]
		nextCursor = fmt.Sprintf("%d", lastJob.ID)
	}

	return jobs, nextCursor, nil
}

// GetJobsDueForExecution returns jobs that need to run.
func (s *Storage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int, filter scheduler.DueJobFilter) ([]*scheduler.Job, error) {
	if limit <= 0 {
		limit = 100
	}

	// A job counts toward limit only when it is due and the caller can
	// dispatch it: ineligible jobs are skipped and the scan continues, so
	// they cannot fill the page and starve eligible ones behind them.
	eligible := func(job *scheduler.Job) bool {
		return job.Status == scheduler.JobStatusPending &&
			!job.NextRun.IsZero() &&
			job.NextRun.Before(before) &&
			(filter == nil || filter(job))
	}

	// Older nodes in a rolling upgrade can still write tenant jobs without
	// an index entry, so the index is reconciled periodically, not only at
	// open. Reconciliation gets part of the sweep's budget and never fails
	// the sweep: a run that does not finish is retried by the next sweep,
	// which resumes from the entries already written, and the jobs already
	// indexed, system jobs included, dispatch meanwhile.
	if s.reconcileDue() {
		s.reconcileWithinBudget(ctx)
	}

	// Tenant jobs live in their tenants' namespaces; the index names them.
	// The index is collected first and each tenant's jobs are read after it
	// in one prefix scan per tenant, so no read runs inside the store's
	// iteration and a sweep costs one scan per tenant, not one read per
	// retained job.
	indexed := make(map[string]map[uint64]struct{})
	var tenants []string
	err := s.kv.Scan(ctx, TenantJobIndexPrefix, func(key string, value []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, ok := parseTenantJobIndexKey(key)
		if !ok || len(value) == 0 {
			return nil
		}
		tenantID := string(value)
		ids, seen := indexed[tenantID]
		if !seen {
			ids = make(map[uint64]struct{})
			indexed[tenantID] = ids
			tenants = append(tenants, tenantID)
		}
		ids[id] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, &scheduler.ErrStorageError{Op: "GetJobsDueForExecution.index_scan", Cause: err}
	}

	// The scan starts at a rotating namespace and wraps around. The system
	// namespace is one slot of the rotation, after the tenants, so each
	// namespace, system included, is first within len(tenants)+1 sweeps and
	// a full page of one cannot starve the others. The tenant list is sorted
	// so its order does not depend on the store's iteration order.
	slices.Sort(tenants)
	namespaces := make([]string, 0, len(tenants)+1)
	namespaces = append(namespaces, tenants...)
	namespaces = append(namespaces, systemDueNamespace)
	start := int((s.tenantCursor.Add(1) - 1) % uint64(len(namespaces)))
	dueJobs := make([]*scheduler.Job, 0)
	for i := range namespaces {
		ns := namespaces[(start+i)%len(namespaces)]
		if ns == systemDueNamespace {
			dueJobs, err = s.collectSystemDue(ctx, eligible, dueJobs, limit)
		} else {
			dueJobs, err = s.collectTenantDue(ctx, ns, indexed[ns], eligible, dueJobs, limit)
		}
		if err != nil {
			return nil, err
		}
		if len(dueJobs) >= limit {
			break
		}
	}

	return dueJobs, nil
}

// systemDueNamespace marks the system namespace in the due-scan rotation. The
// empty string is never a tenant ID: the index scan skips empty values.
const systemDueNamespace = ""

// collectSystemDue appends the eligible system jobs to due until it holds
// limit jobs.
func (s *Storage) collectSystemDue(ctx context.Context, eligible func(*scheduler.Job) bool, due []*scheduler.Job, limit int) ([]*scheduler.Job, error) {
	err := s.kv.Scan(ctx, SystemJobsPrefix, func(key string, value []byte) error {
		// The store does not check ctx between entries: a canceled or
		// expired sweep stops here rather than at the end of the prefix.
		if err := ctx.Err(); err != nil {
			return err
		}
		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// Skip malformed entries
			return nil
		}

		if eligible(&job) {
			due = append(due, &job)
			if len(due) >= limit {
				return ErrLimitReached
			}
		}

		return nil
	})
	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, &scheduler.ErrStorageError{Op: "GetJobsDueForExecution.scan", Cause: err}
	}
	return due, nil
}

// collectTenantDue appends the eligible indexed jobs of tenantID to due until
// it holds limit jobs.
func (s *Storage) collectTenantDue(ctx context.Context, tenantID string, ids map[uint64]struct{}, eligible func(*scheduler.Job) bool, due []*scheduler.Job, limit int) ([]*scheduler.Job, error) {
	err := s.kv.Scan(ctx, recordNamespace(tenantID)+"scheduler/jobs/", func(key string, value []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		keyTenant, id, ok := splitTenantJobKey(key)
		if !ok || keyTenant != tenantID {
			return nil
		}
		if _, listed := ids[id]; !listed {
			return nil
		}
		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// A malformed job is skipped, as in the system scan, so
			// it cannot block other tenants' jobs.
			return nil
		}
		if eligible(&job) {
			due = append(due, &job)
			if len(due) >= limit {
				return ErrLimitReached
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, &scheduler.ErrStorageError{Op: "GetJobsDueForExecution.tenant_scan", Cause: err}
	}
	return due, nil
}

// reconcileDue reports whether reconcileInterval has passed since the tenant
// job index was last reconciled.
func (s *Storage) reconcileDue() bool {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	return time.Since(s.lastReconcile) >= s.reconcileInterval
}

// markReconciled records that the tenant job index was just reconciled.
func (s *Storage) markReconciled() {
	s.reconcileMu.Lock()
	s.lastReconcile = time.Now()
	s.reconcileMu.Unlock()
}

// executionIDAt derives an execution ID from t in Unix nanoseconds. A time
// before the epoch has no unsigned ID and is refused.
func executionIDAt(t time.Time) (uint64, error) {
	return conv.SafeInt64ToUint64(t.UnixNano())
}

// SaveExecution stores a job execution record.
func (s *Storage) SaveExecution(ctx context.Context, exec *scheduler.JobExecution) error {
	if exec == nil {
		return &scheduler.ErrStorageError{Op: "SaveExecution", Cause: ErrNilExecution}
	}

	if exec.ID == 0 {
		id, err := executionIDAt(time.Now())
		if err != nil {
			return &scheduler.ErrStorageError{Op: "SaveExecution", Cause: err}
		}
		exec.ID = id
	}

	data, err := json.Marshal(exec)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "SaveExecution.marshal", Cause: err}
	}

	// A tenant-owned job's executions live in that tenant's keyspace, where
	// GetExecutions reads tenant history; system and delegated runs stay in
	// the system keyspace.
	key := keyForExecution(exec, exec.TenantID)

	if err := s.kv.Put(ctx, key, data); err != nil {
		return &scheduler.ErrStorageError{Op: "SaveExecution.put", Cause: err}
	}

	return nil
}

// GetExecutions retrieves execution history for a job.
func (s *Storage) GetExecutions(ctx context.Context, jobID uint64, tenantID string, limit int) ([]*scheduler.JobExecution, error) {
	if limit <= 0 {
		limit = 100
	}

	var prefix string
	if tenantID == "" {
		prefix = fmt.Sprintf("%s%d/", ExecutionsPrefix, jobID)
	} else {
		prefix = fmt.Sprintf(TenantExecutionsPrefixFmt+"%d/", tenantID, jobID)
	}

	executions := make([]*scheduler.JobExecution, 0)

	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var exec scheduler.JobExecution
		if err := json.Unmarshal(value, &exec); err != nil {
			// Skip malformed entries
			return nil
		}

		executions = append(executions, &exec)
		if len(executions) >= limit {
			return ErrLimitReached
		}

		return nil
	})

	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, &scheduler.ErrStorageError{Op: "GetExecutions.scan", Cause: err}
	}

	return executions, nil
}

// InitializeJobIDCounter makes the durable job ID counter ready for
// allocation, seeding it from the highest job ID already stored when no
// counter has ever been persisted.
//
// The scheduler engine calls it at Start, so a store whose counter cannot be
// read stops the engine from starting rather than letting it allocate from a
// guess: an ID reissued after a restart is the key of a live job, and
// CreateJob would overwrite it.
//
// It is also where the store opens, so it runs the tenant-job index
// reconciliation (reconcileTenantJobIndex) before the first due scan, within
// its budget. A reconciliation that does not finish does not stop the engine
// from starting: the first sweep resumes it, and jobs already indexed,
// system jobs included, dispatch meanwhile.
func (s *Storage) InitializeJobIDCounter(ctx context.Context) error {
	if err := s.ids.initialize(ctx); err != nil {
		return err
	}
	// Reconciliation does not gate opening: a run that does not finish in
	// its budget leaves the index to the first sweep, which resumes it.
	if !s.reconcileWithinBudget(ctx) {
		s.reconcileMu.Lock()
		s.lastReconcile = time.Time{}
		s.reconcileMu.Unlock()
	}
	return nil
}

// reconcileTenantJobIndex indexes every tenant job record that has no
// TenantJobIndexPrefix entry: CreateJob indexes every job it writes, but a
// job written by an earlier release, or by an older node still running in a
// rolling upgrade, has none, and the due scan reads the index alone. So the
// TenantJobIndexBackfillKey marker no longer ends reconciliation: it runs on
// every open and periodically from the due scan, Putting only missing
// entries, and writes the marker when absent.
//
// One pass covers the tenant records in key-prefix shards, starting from
// tenantKeyPrefix. A shard is scanned, its records' index entries are
// checked, and the missing ones are written before the next shard, so every
// finished shard is durable progress. A shard holding more than
// reconcileShardLimit records, or whose scan the budget cuts short, is split
// into one narrower shard per next key byte; the unfinished shards are kept
// on the Storage, so the next run resumes the pass where this one stopped
// even when scanning the whole keyspace takes longer than one budget. A run
// cut short inside a shard's index checks keeps the key of the last record it
// checked on the shard, and the next run resumes after it.
func (s *Storage) reconcileTenantJobIndex(ctx context.Context) error {
	if !s.reconcileRun.TryLock() {
		return &scheduler.ErrStorageError{Op: "reconcileTenantJobIndex", Cause: errReconcileBusy}
	}
	defer s.reconcileRun.Unlock()

	if len(s.reconcilePending) == 0 {
		s.reconcilePending = []reconcileShard{{prefix: tenantKeyPrefix}}
	}
	for len(s.reconcilePending) > 0 {
		if err := ctx.Err(); err != nil {
			return &scheduler.ErrStorageError{Op: "reconcileTenantJobIndex.scan", Cause: err}
		}
		shard := s.reconcilePending[0]
		refs, split, err := s.scanReconcileShard(ctx, shard)
		if split {
			s.reconcilePending = append(shard.children(), s.reconcilePending[1:]...)
		}
		if err != nil {
			return &scheduler.ErrStorageError{Op: "reconcileTenantJobIndex.scan", Cause: err}
		}
		if split {
			continue
		}
		// A store need not deliver keys in order; the cursor needs them sorted.
		slices.SortFunc(refs, func(a, b tenantJobRef) int { return strings.Compare(a.key, b.key) })
		for _, ref := range refs {
			if shard.after != "" && ref.key <= shard.after {
				continue
			}
			indexKey := keyForTenantJobIndex(ref.id, ref.tenantID)
			present, err := s.kv.Exists(ctx, indexKey)
			if err != nil {
				return &scheduler.ErrStorageError{Op: "reconcileTenantJobIndex.index", Cause: err}
			}
			if present {
				s.reconcilePending[0].after = ref.key
				continue
			}
			if err := s.kv.Put(ctx, indexKey, []byte(ref.tenantID)); err != nil {
				return &scheduler.ErrStorageError{Op: "reconcileTenantJobIndex.index", Cause: err}
			}
			s.reconcilePending[0].after = ref.key
		}
		s.reconcilePending = s.reconcilePending[1:]
	}
	s.reconcilePending = nil

	done, err := s.kv.Exists(ctx, TenantJobIndexBackfillKey)
	if err != nil {
		return &scheduler.ErrStorageError{Op: "reconcileTenantJobIndex.marker", Cause: err}
	}
	if !done {
		if err := s.kv.Put(ctx, TenantJobIndexBackfillKey, []byte{1}); err != nil {
			return &scheduler.ErrStorageError{Op: "reconcileTenantJobIndex.marker", Cause: err}
		}
	}
	return nil
}

// reconcileShardLimit is the most tenant records one reconciliation shard
// holds; a larger shard is split before any of it is written.
const reconcileShardLimit = 1024

// reconcileMaxSplitPrefix bounds how long a shard prefix splitting on a
// timed-out scan may grow; a shard at the bound is retried whole, so a
// store too slow for any scan cannot grow the pending list without end.
const reconcileMaxSplitPrefix = 512

// errReconcileBusy reports that another reconciliation run is in progress.
var errReconcileBusy = errors.New("tenant job index reconciliation already running")

// errReconcileShardFull stops a shard scan that passed reconcileShardLimit.
var errReconcileShardFull = errors.New("reconciliation shard full")

// reconcileShard is one key-prefix slice of a reconciliation pass. An exact
// shard covers only the key equal to prefix: splitting a prefix shard into
// one child per next byte leaves that key out of every child.
type reconcileShard struct {
	prefix string
	exact  bool
	// after is the key of the last record whose index entry a cut-short run
	// already checked or wrote; the next run resumes after it.
	after string
}

// children returns the shards that together cover s: the key equal to its
// prefix and one prefix shard per possible next byte.
func (s reconcileShard) children() []reconcileShard {
	out := make([]reconcileShard, 0, 257)
	out = append(out, reconcileShard{prefix: s.prefix, exact: true})
	for b := 0; b < 256; b++ {
		out = append(out, reconcileShard{prefix: s.prefix + string([]byte{byte(b)})})
	}
	return out
}

// tenantJobRef names one tenant job record.
type tenantJobRef struct {
	id       uint64
	tenantID string
	// key is the record's key, the shard's resume cursor.
	key string
}

// scanReconcileShard returns the tenant job records shard covers. split
// reports that the shard must be divided: it holds more than
// reconcileShardLimit records, or its scan was cut short by ctx (err is then
// set too), so the next attempt scans narrower prefixes.
func (s *Storage) scanReconcileShard(ctx context.Context, shard reconcileShard) (refs []tenantJobRef, split bool, err error) {
	if shard.exact {
		present, err := s.kv.Exists(ctx, shard.prefix)
		if err != nil || !present {
			return nil, false, err
		}
		if tenantID, id, ok := splitTenantJobKey(shard.prefix); ok {
			refs = append(refs, tenantJobRef{id: id, tenantID: tenantID, key: shard.prefix})
		}
		return refs, false, nil
	}
	full := false
	err = s.kv.Scan(ctx, shard.prefix, func(key string, _ []byte) error {
		if tenantID, id, ok := splitTenantJobKey(key); ok {
			if len(refs) >= reconcileShardLimit {
				full = true
				return errReconcileShardFull
			}
			refs = append(refs, tenantJobRef{id: id, tenantID: tenantID, key: key})
		}
		return ctx.Err()
	})
	if full {
		return nil, true, nil
	}
	if err != nil {
		// A prefix scan the budget cut is split so the next run scans less,
		// also when no record reached the callback: a store may fetch the
		// whole prefix before its first callback.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, len(shard.prefix) < reconcileMaxSplitPrefix, ctxErr
		}
		return nil, false, err
	}
	return refs, false, nil
}

// reconcileWithinBudget runs one reconciliation of the tenant job index under
// a budget of at most half of ctx's remaining time (and at most
// tenantJobIndexReconcileBudget), so the caller keeps the rest for its own
// work. It reports whether the reconciliation completed; a run cut short, or
// one that failed, is retried by the next caller and resumes from the entries
// already written.
func (s *Storage) reconcileWithinBudget(ctx context.Context) bool {
	budget := tenantJobIndexReconcileBudget
	if deadline, ok := ctx.Deadline(); ok {
		if half := time.Until(deadline) / 2; half < budget {
			budget = half
		}
	}
	if budget <= 0 {
		return false
	}
	rctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if err := s.reconcileTenantJobIndex(rctx); err != nil {
		return false
	}
	s.markReconciled()
	return true
}

// indexTenantJob writes a tenant job's TenantJobIndexPrefix entry. Callers
// write it before the job record: an entry whose job is missing is skipped
// by the due scan, while a job without its entry would never run.
func (s *Storage) indexTenantJob(ctx context.Context, op string, job *scheduler.Job) error {
	tenantID := scheduler.JobTenantID(job)
	if tenantID == "" {
		return nil
	}
	if err := s.kv.Put(ctx, keyForTenantJobIndex(job.ID, tenantID), []byte(tenantID)); err != nil {
		return &scheduler.ErrStorageError{Op: op + ".index", Cause: err}
	}
	return nil
}

// Close performs cleanup operations.
// Currently a no-op as KVStore lifecycle is managed externally.
func (s *Storage) Close() error {
	return nil
}
