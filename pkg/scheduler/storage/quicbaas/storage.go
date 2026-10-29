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
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

	return nil
}

// ListJobs returns jobs matching the query.
func (s *Storage) ListJobs(ctx context.Context, query *scheduler.JobQuery) ([]*scheduler.Job, string, error) {
	if query == nil {
		query = &scheduler.JobQuery{}
	}

	scans := jobScans(query)

	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}

	// The cursor names the last job on the previous page by scope and ID: a
	// job ID is unique only within its scope, and a tenant listing merges the
	// system scope with the tenant's, so the ID alone would skip a job that
	// shares its ID with one already returned. The scan does not promise key
	// order, and "jobs/10" sorts before "jobs/9" anyway, so pages are cut by
	// (scope, ID) rather than by scan position.
	var after jobPosition
	if query.Cursor != "" {
		pos, err := parseJobCursor(query.Cursor)
		if err != nil {
			return nil, "", &scheduler.ErrStorageError{Op: "ListJobs.cursor", Cause: err}
		}
		after = pos
	}

	listed := make([]listedJob, 0)

	var scope string
	visit := func(key string, value []byte) error {
		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// Skip malformed entries
			return nil
		}
		pos := jobPosition{scope: scope, id: job.ID}
		if query.Cursor != "" && pos.compare(after) <= 0 {
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

		listed = append(listed, listedJob{pos: pos, job: &job})
		return nil
	}
	for _, scan := range scans {
		scope = scan.scope
		if err := s.kv.Scan(ctx, scan.prefix, visit); err != nil {
			return nil, "", &scheduler.ErrStorageError{Op: "ListJobs.scan", Cause: err}
		}
	}

	slices.SortFunc(listed, func(a, b listedJob) int {
		return a.pos.compare(b.pos)
	})

	// A cursor is returned only when another page exists, so callers can
	// read "no cursor" as "no more jobs".
	var nextCursor string
	if len(listed) > limit {
		listed = listed[:limit]
		nextCursor = listed[limit-1].pos.cursor()
	}

	jobs := make([]*scheduler.Job, len(listed))
	for i, entry := range listed {
		jobs[i] = entry.job
	}
	return jobs, nextCursor, nil
}

// jobPosition is a job's place in a ListJobs page order. A job ID is unique
// only within its scope (see scopeToken), so the position pairs the two.
type jobPosition struct {
	scope string
	id    uint64
}

// compare orders positions by scope, then by ID.
func (p jobPosition) compare(other jobPosition) int {
	return cmp.Or(strings.Compare(p.scope, other.scope), cmp.Compare(p.id, other.id))
}

// cursor renders the position as a ListJobs cursor: "<scope>/<id>".
func (p jobPosition) cursor() string {
	return p.scope + "/" + strconv.FormatUint(p.id, 10)
}

// parseJobCursor reads a cursor written by jobPosition.cursor. The ID is split
// at the last "/" so a tenant ID that holds one still parses.
func parseJobCursor(cursor string) (jobPosition, error) {
	sep := strings.LastIndex(cursor, "/")
	if sep < 0 {
		return jobPosition{}, ErrInvalidCursor
	}
	scope := cursor[:sep]
	if !isScopeToken(scope) {
		return jobPosition{}, ErrInvalidCursor
	}
	id, err := strconv.ParseUint(cursor[sep+1:], 10, 64)
	if err != nil {
		return jobPosition{}, ErrInvalidCursor
	}
	return jobPosition{scope: scope, id: id}, nil
}

// isScopeToken reports whether s is a value scopeToken returns.
func isScopeToken(s string) bool {
	if s == scopeToken("") {
		return true
	}
	tenantID, ok := strings.CutPrefix(s, "t:")
	if !ok {
		return false
	}
	tenantID, ok = strings.CutSuffix(tenantID, ":")
	return ok && tenantID != ""
}

// listedJob is a job read by ListJobs with its page position.
type listedJob struct {
	pos jobPosition
	job *scheduler.Job
}

// jobScan is one key range a ListJobs query reads, with the scope (see
// scopeToken) whose job IDs it holds.
type jobScan struct {
	prefix string
	scope  string
}

// jobScans returns the key ranges a ListJobs query must scan. A tenant's own
// jobs live under its tenant prefix while system and delegated jobs aimed at
// it live under the system prefix, so a query scoped only by target tenant
// scans both.
func jobScans(query *scheduler.JobQuery) []jobScan {
	system := jobScan{prefix: SystemJobsPrefix, scope: scopeToken("")}
	tenant := func(tenantID string) jobScan {
		return jobScan{prefix: fmt.Sprintf(TenantJobsPrefixFmt, tenantID), scope: scopeToken(tenantID)}
	}
	switch {
	case query.OwnerID != "":
		return []jobScan{tenant(query.OwnerID)}
	case query.OwnerType == "" && query.TargetTenantID != "":
		return []jobScan{system, tenant(query.TargetTenantID)}
	default:
		return []jobScan{system}
	}
}

// GetJobsDueForExecution returns jobs that need to run.
func (s *Storage) GetJobsDueForExecution(ctx context.Context, before time.Time, limit int) ([]*scheduler.Job, error) {
	if limit <= 0 {
		limit = 100
	}

	// Scan system jobs prefix for pending jobs
	dueJobs := make([]*scheduler.Job, 0)

	err := s.kv.Scan(ctx, SystemJobsPrefix, func(key string, value []byte) error {
		var job scheduler.Job
		if err := json.Unmarshal(value, &job); err != nil {
			// Skip malformed entries
			return nil
		}

		// Check if job is due for execution
		isDue := job.Status == scheduler.JobStatusPending &&
			!job.NextRun.IsZero() &&
			job.NextRun.Before(before)

		if isDue {
			dueJobs = append(dueJobs, &job)
			if len(dueJobs) >= limit {
				return ErrLimitReached
			}
		}

		return nil
	})

	if err != nil && !errors.Is(err, ErrLimitReached) {
		return nil, &scheduler.ErrStorageError{Op: "GetJobsDueForExecution.scan", Cause: err}
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

	// Determine tenant ID from job ownership if we need to store in tenant keyspace
	// For now, store in system keyspace
	key := keyForExecution(exec, "")

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
func (s *Storage) InitializeJobIDCounter(ctx context.Context) error {
	return s.ids.initialize(ctx)
}

// Close performs cleanup operations.
// Currently a no-op as KVStore lifecycle is managed externally.
func (s *Storage) Close() error {
	return nil
}
