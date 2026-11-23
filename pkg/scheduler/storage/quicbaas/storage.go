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
	"strconv"
	"strings"
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
	// "<prefix><tenantID>/<jobID>", whose value is a JSON summary of the job
	// (tenant, status, next run; see tenantJobIndexEntry). The due scan reads
	// only the records of entries that are pending and due, or whose value
	// is a bare tenant ID with no summary. The index lives in the reserved
	// namespace, so a tenant cannot enumerate other tenants' jobs through it.
	//
	// A node still on an earlier release updates a record without updating
	// its summary. An entry it left pending makes the scan read that record
	// each tick until the next store open reconciles the summary, which is
	// no worse than reading every record.
	TenantJobIndexPrefix = "__sys:scheduler/tenant-jobs/"
)

// Storage implements scheduler.JobStorage using QuicBaaS's KVStore interface.
type Storage struct {
	kv  interfaces.KVStore
	ids jobIDAllocator
}

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
			kv:  kv,
			ids: &clusterJobIDs{kv: kv, reader: marks, writer: idWriter},
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

// tenantJobIndexEntry is a tenant-job index value: the job's tenant and
// enough of its state for the due scan to pass over a job that cannot be
// due without reading its record.
type tenantJobIndexEntry struct {
	TenantID string              `json:"tenant"`
	Status   scheduler.JobStatus `json:"status"`
	NextRun  time.Time           `json:"next_run"`
}

func tenantJobIndexValue(job *scheduler.Job, tenantID string) ([]byte, error) {
	return json.Marshal(tenantJobIndexEntry{TenantID: tenantID, Status: job.Status, NextRun: job.NextRun})
}

// parseTenantJobIndexValue reads an index value. A value with no summary
// (a bare tenant ID) reports summarized false: its job is read to decide.
func parseTenantJobIndexValue(value []byte) (entry tenantJobIndexEntry, summarized bool) {
	if len(value) > 0 && value[0] == '{' {
		if err := json.Unmarshal(value, &entry); err == nil && entry.TenantID != "" {
			return entry, true
		}
	}
	return tenantJobIndexEntry{TenantID: string(value)}, false
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
		s.unsummarizeTenantJob(ctx, job)
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
		s.unsummarizeTenantJob(ctx, job)
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

// GetJobsDueForExecution returns jobs that need to run. The scan covers the
// system namespace and then the tenant jobs the tenant-job index names,
// under one limit. An index value is a JSON summary (tenant, status, next
// run): only the records of entries that are pending and due, or that have
// no summary, are read, and the record decides. A due
// job that eligible rejects is skipped and does not use up the limit, so jobs
// the caller can never run cannot crowd out others; a nil eligible admits
// every job.
func (s *Storage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int, eligible func(*scheduler.Job) bool) ([]*scheduler.Job, error) {
	if limit <= 0 {
		limit = 100
	}

	// isDue admits a pending job whose next run has passed and that eligible
	// accepts; both the namespace scan and the tenant index scan use it.
	isDue := func(job *scheduler.Job) bool {
		return job.Status == scheduler.JobStatusPending &&
			!job.NextRun.IsZero() &&
			job.NextRun.Before(before) &&
			(eligible == nil || eligible(job))
	}

	dueJobs := make([]*scheduler.Job, 0)

	visit := func(key string, value []byte) error {
		// Keep only job records: the marker must open a namespace segment
		// ("__sys:" or "t:{tenant}:") and be followed by a decimal job ID,
		// so execution records and a tenant's own keys are skipped.
		i := strings.LastIndex(key, jobKeyMarker)
		if i <= 0 || key[i-1] != ':' {
			return nil
		}
		if _, err := strconv.ParseUint(key[i+len(jobKeyMarker):], 10, 64); err != nil {
			return nil
		}

		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// Skip malformed entries
			return nil
		}

		if isDue(&job) {
			dueJobs = append(dueJobs, &job)
			if len(dueJobs) >= limit {
				return ErrLimitReached
			}
		}

		return nil
	}

	if err := s.kv.Scan(ctx, SystemJobsPrefix, visit); err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, &scheduler.ErrStorageError{Op: "GetJobsDueForExecution.scan", Cause: err}
	}
	if len(dueJobs) >= limit {
		return dueJobs, nil
	}

	// Tenant jobs live in their tenants' namespaces; the index names them.
	// The entries are collected first and the jobs read after the scan, so
	// no read runs inside the store's iteration.
	type tenantJobRef struct {
		id       uint64
		tenantID string
	}
	var refs []tenantJobRef
	err := s.kv.Scan(ctx, TenantJobIndexPrefix, func(key string, value []byte) error {
		id, ok := parseTenantJobIndexKey(key)
		if !ok || len(value) == 0 {
			return nil
		}
		entry, summarized := parseTenantJobIndexValue(value)
		if summarized && (entry.Status != scheduler.JobStatusPending ||
			entry.NextRun.IsZero() || !entry.NextRun.Before(before)) {
			// The summary says this job cannot be due: its record is not read.
			return nil
		}
		refs = append(refs, tenantJobRef{id: id, tenantID: entry.TenantID})
		return nil
	})
	if err != nil {
		return nil, &scheduler.ErrStorageError{Op: "GetJobsDueForExecution.index_scan", Cause: err}
	}

	for _, ref := range refs {
		job, err := s.GetJob(ctx, ref.id, ref.tenantID)
		if err != nil {
			if notFound := (*scheduler.JobNotFoundError)(nil); errors.As(err, &notFound) {
				// Created index-first and not yet written, or deleted.
				continue
			}
			return nil, err
		}
		if isDue(job) {
			dueJobs = append(dueJobs, job)
			if len(dueJobs) >= limit {
				break
			}
		}
	}

	return dueJobs, nil
}

// SaveExecution stores a job execution record.
// executionIDAt derives an execution ID from t in Unix nanoseconds. A time
// before the epoch has no unsigned ID and is refused.
func executionIDAt(t time.Time) (uint64, error) {
	return conv.SafeInt64ToUint64(t.UnixNano())
}

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

	// The execution is stored in its job's namespace ("" is the system
	// keyspace), where GetExecutions reads the job's history.
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
// It is also where the store opens, so it reconciles the tenant-job index
// (backfillTenantJobIndex) before the first due scan; a reconcile that fails
// stops the engine from starting for the same reason.
func (s *Storage) InitializeJobIDCounter(ctx context.Context) error {
	if err := s.ids.initialize(ctx); err != nil {
		return err
	}
	return s.backfillTenantJobIndex(ctx)
}

// backfillTenantJobIndex reconciles the tenant-job index at every store
// open: CreateJob and UpdateJob index every job they write, but a node still
// on an earlier release does not, and the due scan reads the index alone.
// Each open indexes every tenant job record it finds (a Put of the same
// entry, so repeating it is safe); once every node has restarted on this
// release, every job an old writer stored is indexed.
func (s *Storage) backfillTenantJobIndex(ctx context.Context) error {
	// Entries are collected first and written after the scan, so no write
	// runs inside the store's iteration. Each entry carries the summary of
	// the record it indexes; a record that does not decode gets the bare
	// tenant ID, so the due scan reads it.
	type tenantJobRef struct {
		id       uint64
		tenantID string
		value    []byte
	}
	var refs []tenantJobRef
	err := s.kv.Scan(ctx, tenantKeyPrefix, func(key string, record []byte) error {
		tenantID, id, ok := splitTenantJobKey(key)
		if !ok {
			return nil
		}
		value := []byte(tenantID)
		var job scheduler.Job
		if err := json.Unmarshal(record, &job); err == nil {
			if summary, err := tenantJobIndexValue(&job, tenantID); err == nil {
				value = summary
			}
		}
		refs = append(refs, tenantJobRef{id: id, tenantID: tenantID, value: value})
		return nil
	})
	if err != nil {
		return &scheduler.ErrStorageError{Op: "backfillTenantJobIndex.scan", Cause: err}
	}
	for _, ref := range refs {
		if err := s.kv.Put(ctx, keyForTenantJobIndex(ref.id, ref.tenantID), ref.value); err != nil {
			return &scheduler.ErrStorageError{Op: "backfillTenantJobIndex.index", Cause: err}
		}
	}
	return nil
}

// indexTenantJob writes a tenant job's TenantJobIndexPrefix entry. Callers
// write it before the job record: an entry whose job is missing is skipped
// by the due scan, while a job without its entry would never run.
func (s *Storage) indexTenantJob(ctx context.Context, op string, job *scheduler.Job) error {
	tenantID := scheduler.JobTenantID(job)
	if tenantID == "" {
		return nil
	}
	value, err := tenantJobIndexValue(job, tenantID)
	if err != nil {
		return &scheduler.ErrStorageError{Op: op + ".index_marshal", Cause: err}
	}
	if err := s.kv.Put(ctx, keyForTenantJobIndex(job.ID, tenantID), value); err != nil {
		return &scheduler.ErrStorageError{Op: op + ".index", Cause: err}
	}
	return nil
}

// unsummarizeTenantJob rewrites a tenant job's index entry as the bare
// tenant ID after its record write failed, so the summary never claims a
// state the record does not hold: a bare value makes the due scan read the
// record. It is best-effort; its own error is ignored.
func (s *Storage) unsummarizeTenantJob(ctx context.Context, job *scheduler.Job) {
	tenantID := scheduler.JobTenantID(job)
	if tenantID == "" {
		return
	}
	_ = s.kv.Put(ctx, keyForTenantJobIndex(job.ID, tenantID), []byte(tenantID))
}

// Close performs cleanup operations.
// Currently a no-op as KVStore lifecycle is managed externally.
func (s *Storage) Close() error {
	return nil
}
