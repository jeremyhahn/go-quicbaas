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

package orchestration

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// Backend failures
// =============================================================================

// ErrTaskStoreBackend indicates the backing KV store failed. It is distinct
// from ErrTaskStoreMarshal (a record this store wrote cannot be decoded) and
// from ErrTaskNotFound (the record is simply absent): a backend failure says
// nothing about the record, only that the store could not be reached.
var ErrTaskStoreBackend = errors.New("task store: backend failure")

// backendError reports a failed KV operation in the task store's error
// vocabulary. The cause is folded into the message because TaskStoreError
// carries a sentinel rather than a wrapped cause.
func backendError(op string, cause error) error {
	return &TaskStoreError{
		Sentinel: ErrTaskStoreBackend,
		Message:  op + ": " + cause.Error(),
	}
}

// =============================================================================
// Key-segment errors
// =============================================================================

// TaskKeySegmentError reports an identifier the task store cannot use as a key
// segment.
//
// It bridges two contracts that must both hold. Callers of the store match the
// store's validation vocabulary (ErrTaskStoreInvalidInput / *TaskStoreError,
// which the REST and gRPC layers already map onto a client error), while the
// key layout in store_keys.go states the isolation rule that was broken
// (ErrInvalidKeySegment / *KeySegmentError). Unwrap returns both, so errors.Is
// and errors.As succeed against either contract without this store having to
// duplicate the validation itself.
type TaskKeySegmentError struct {
	// Invalid restates the violation in the task store's vocabulary.
	Invalid *TaskStoreError

	// Segment is the underlying *KeySegmentError from the key layout.
	Segment error
}

// Error implements the error interface.
func (e *TaskKeySegmentError) Error() string { return e.Invalid.Error() }

// Unwrap exposes both wrapped contracts to errors.Is and errors.As.
func (e *TaskKeySegmentError) Unwrap() []error { return []error{e.Invalid, e.Segment} }

// taskKeyError converts a key-layout violation into the store's dual-contract
// error. Any other error passes through untouched.
func taskKeyError(err error) error {
	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		return err
	}
	return &TaskKeySegmentError{
		Invalid: &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  segment.Field + " " + segment.Message,
		},
		Segment: err,
	}
}

// =============================================================================
// Key derivation
// =============================================================================

// taskRecordKey returns the key holding one task record:
// orch/task/{tenantID}/{taskID}.
func taskRecordKey(tenantID, taskID string) (string, error) {
	key, err := entityKey(taskKeyspace, tenantID, "task_id", taskID)
	if err != nil {
		return "", taskKeyError(err)
	}
	return key, nil
}

// taskScanPrefix returns the prefix covering every task record owned by
// tenantID. A list bounded by this prefix cannot observe another tenant.
func taskScanPrefix(tenantID string) (string, error) {
	prefix, err := tenantPrefix(taskKeyspace, tenantID)
	if err != nil {
		return "", taskKeyError(err)
	}
	return prefix, nil
}

// taskArtifactPrefix returns the prefix covering every offloaded artifact
// payload of one task: orch/taskartifact/{tenantID}/{taskID}/.
func taskArtifactPrefix(tenantID, taskID string) (string, error) {
	key, err := entityKey(taskArtifactKeyspace, tenantID, "task_id", taskID)
	if err != nil {
		return "", taskKeyError(err)
	}
	return key + keySeparator, nil
}

// tenantArtifactPrefix returns the prefix covering every offloaded artifact
// payload owned by tenantID.
func tenantArtifactPrefix(tenantID string) (string, error) {
	prefix, err := tenantPrefix(taskArtifactKeyspace, tenantID)
	if err != nil {
		return "", taskKeyError(err)
	}
	return prefix, nil
}

// taskArtifactKey returns the key holding one offloaded artifact payload.
//
// The artifact name is a key segment like any other, but unlike an identifier
// it is a file-like label that legitimately contains the separator
// ("src/main.go"), so rejecting it the way tenant and task IDs are rejected
// would refuse valid artifacts. The name is therefore encoded with
// base64.RawURLEncoding, which maps any byte string onto a single separator-free
// segment. The encoding is applied here and nowhere else, so the write path
// (SubmitTaskArtifacts) and the read path (GetTaskArtifact, via the Ref this
// function produced) always agree.
func taskArtifactKey(tenantID, taskID, name string) (string, error) {
	prefix, err := taskArtifactPrefix(tenantID, taskID)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", taskKeyError(&KeySegmentError{
			Field:   "artifact_name",
			Value:   name,
			Message: "must not be empty",
		})
	}
	return prefix + base64.RawURLEncoding.EncodeToString([]byte(name)), nil
}

// =============================================================================
// taskStore — the single KV-backed TaskStore
// =============================================================================

// taskStore is the only TaskStore implementation. It holds no copy of the data
// it serves: every task record and every offloaded artifact payload lives in
// the injected interfaces.KVStore, so durability, replication and
// encryption-at-rest are properties of that store alone. Injecting
// kvstore.NewMemory() yields a fast in-process store; injecting the server's
// qrdb-backed KV yields a durable, replicated one. There is no second
// implementation to keep in sync, and a store rebuilt over the same KV sees
// exactly what its predecessor wrote.
//
// Concurrency. interfaces.KVStore exposes no compare-and-swap verb, so an
// atomic update is expressed as a read-modify-write held under recordMu, the
// per-key mutex from store_keys.go. That makes every transition — claim,
// complete, fail, artifact submission, versioned update — atomic against other
// writers *in this process*. Across processes or Raft nodes the record's own
// Version counter is the guard: a writer that raced loses the version check and
// receives TaskVersionConflictError, so a concurrent overwrite is *detected*,
// not prevented. Preventing it requires a compare-and-swap verb that qrdb's SDK
// does not currently re-export; when it does, this store swaps the
// read-modify-write for that verb and the version field keeps its meaning.
type taskStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// inlineLimit is the maximum artifact size, in bytes, embedded directly in
	// a task record. Larger artifacts are written to their own key.
	inlineLimit int64

	// recordMu serializes the read-modify-write sequence on one task record.
	// Writes to different tasks proceed in parallel.
	recordMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ TaskStore = (*taskStore)(nil)

// NewTaskStore returns a TaskStore backed by kv.
//
// The backing store decides the storage semantics: pass kvstore.NewMemory() for
// an in-process store, or the server's qrdb-backed KV for a durable, replicated
// one.
func NewTaskStore(kv interfaces.KVStore, opts ...StoreOption) TaskStore {
	cfg := newStoreConfig(opts...)
	return &taskStore{kv: kv, auditor: cfg.auditor, inlineLimit: cfg.inlineLimit}
}

// NewInMemoryTaskStore returns a memory-backed binding of the store returned by
// NewTaskStore — not a second implementation. It is equivalent to
// NewTaskStore(kvstore.NewMemory()) and exists for tests and embedded use that
// need no durability.
func NewInMemoryTaskStore() TaskStore {
	return NewTaskStore(kvstore.NewMemory())
}

// NewInMemoryTaskStoreWithKV returns a store whose task records *and* artifact
// payloads both live in kv, with artifacts larger than limit written to their
// own key. A non-positive limit selects InlineLimit.
func NewInMemoryTaskStoreWithKV(kv interfaces.KVStore, limit int64) TaskStore {
	return NewTaskStore(kv, WithInlineArtifactLimit(limit))
}

// GenerateTaskID produces a random hex-encoded task ID using crypto/rand.
func GenerateTaskID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "failed to generate task ID: " + err.Error(),
		}
	}
	return hex.EncodeToString(b), nil
}

// ready reports whether the store was constructed with a backing KV.
func (s *taskStore) ready() error {
	if s.kv == nil {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "kv store must not be nil",
		}
	}
	return nil
}

// =============================================================================
// Record I/O
// =============================================================================

// load reads and decodes one task record.
func (s *taskStore) load(ctx context.Context, key, tenantID, taskID string) (*Task, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}
		return nil, backendError("failed to read task", err)
	}
	if data == nil {
		return nil, &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
	}

	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreMarshal,
			Message:  "failed to unmarshal task: " + err.Error(),
		}
	}

	// Defence in depth. The key already scopes the read to tenantID, so a
	// record whose body names a different tenant means the key layout was
	// corrupted or bypassed. Report it as absent rather than returning another
	// tenant's task to this caller.
	if task.TenantID != tenantID {
		return nil, &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
	}

	return &task, nil
}

// writeRecord encodes and stores one task record.
func (s *taskStore) writeRecord(ctx context.Context, key string, task *Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreMarshal,
			Message:  "failed to marshal task: " + err.Error(),
		}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return backendError("failed to write task", err)
	}
	return nil
}

// taskMutator applies one state transition to the record loaded under the key
// lock. It returns the record to persist: the same pointer for an in-place
// transition, or the caller's own record for UpdateTask.
type taskMutator func(current *Task, now int64) (*Task, error)

// mutate performs the locked read-modify-write that stands in for a
// compare-and-swap. The lock is held across load, transition and write, so the
// sequence is atomic against other writers in this process; the version bump it
// applies is what lets a writer on another node detect that it raced.
func (s *taskStore) mutate(ctx context.Context, tenantID, taskID string, apply taskMutator) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}

	key, err := taskRecordKey(tenantID, taskID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(key)
	defer unlock()

	current, err := s.load(ctx, key, tenantID, taskID)
	if err != nil {
		return err
	}

	now := time.Now().UnixNano()
	next, err := apply(current, now)
	if err != nil {
		return err
	}

	next.UpdatedAt = now
	next.Version = current.Version + 1

	return s.writeRecord(ctx, key, next)
}

// =============================================================================
// CreateTask
// =============================================================================

// CreateTask creates a new task. Returns error if task ID already exists.
func (s *taskStore) CreateTask(ctx context.Context, task *Task) error {
	var tenantID, taskID string
	if task != nil {
		tenantID, taskID = task.TenantID, task.ID
	}
	err := s.create(ctx, task)
	s.audit(ctx, AuditActionCreate, tenantID, taskID, err)
	return err
}

func (s *taskStore) create(ctx context.Context, task *Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if task == nil {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task must not be nil",
		}
	}
	if task.ID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task ID must not be empty",
		}
	}

	key, err := taskRecordKey(task.TenantID, task.ID)
	if err != nil {
		return err
	}

	now := time.Now().UnixNano()
	task.CreatedAt = now
	task.UpdatedAt = now
	task.Version = 1
	if task.Status == "" {
		task.Status = TaskStatusPending
	}

	// The existence probe and the write are one atomic sequence, so two
	// concurrent creates of the same ID cannot both observe an empty key.
	unlock := s.recordMu.lock(key)
	defer unlock()

	exists, err := s.kv.Exists(ctx, key)
	if err != nil {
		return backendError("failed to probe task", err)
	}
	if exists {
		return &TaskAlreadyExistsError{TaskID: task.ID}
	}

	return s.writeRecord(ctx, key, task)
}

// =============================================================================
// GetTask / ListTasks
// =============================================================================

// GetTask retrieves a task by ID within a tenant namespace.
func (s *taskStore) GetTask(ctx context.Context, tenantID, taskID string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}

	key, err := taskRecordKey(tenantID, taskID)
	if err != nil {
		return nil, err
	}
	return s.load(ctx, key, tenantID, taskID)
}

// errTaskScanStop ends a task scan early once a filter limit is reached. It
// never escapes ListTasks.
var errTaskScanStop = errors.New("task store: scan complete")

// ListTasks lists tasks matching the filter within a tenant, in key order.
func (s *taskStore) ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}

	// The scan is bounded to this tenant's subtree, so no other tenant's tasks
	// are reachable from here.
	prefix, err := taskScanPrefix(tenantID)
	if err != nil {
		return nil, err
	}

	tasks := make([]*Task, 0)
	scanErr := s.kv.Scan(ctx, prefix, func(_ string, value []byte) error {
		var task Task
		if err := json.Unmarshal(value, &task); err != nil {
			// One unreadable record must not hide the rest of the tenant's
			// work queue, so the listing skips it.
			return nil
		}
		if task.TenantID != tenantID {
			// Defence in depth against a corrupted key: a record filed under
			// this tenant that claims another owner is not this tenant's to
			// see.
			return nil
		}
		if !taskMatchesFilter(&task, filter) {
			return nil
		}

		tasks = append(tasks, &task)

		if filter != nil && filter.Limit > 0 && len(tasks) >= filter.Limit {
			return errTaskScanStop
		}
		return nil
	})
	if scanErr != nil && !errors.Is(scanErr, errTaskScanStop) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, backendError("failed to list tasks", scanErr)
	}

	return tasks, nil
}

// taskMatchesFilter reports whether task satisfies every populated field of
// filter. A nil filter matches everything.
func taskMatchesFilter(task *Task, filter *TaskFilter) bool {
	if filter == nil {
		return true
	}
	if filter.Status != "" && task.Status != filter.Status {
		return false
	}
	if filter.TeamName != "" && task.TeamName != filter.TeamName {
		return false
	}
	if filter.AssignedTo != "" && task.AssignedTo != filter.AssignedTo {
		return false
	}
	if filter.CreatedBy != "" && task.CreatedBy != filter.CreatedBy {
		return false
	}
	return true
}

// =============================================================================
// UpdateTask
// =============================================================================

// UpdateTask updates an existing task. The task's Version must match the stored
// version; otherwise a TaskVersionConflictError is returned and nothing is
// written.
func (s *taskStore) UpdateTask(ctx context.Context, task *Task) error {
	var tenantID, taskID string
	if task != nil {
		tenantID, taskID = task.TenantID, task.ID
	}
	err := s.update(ctx, task)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

func (s *taskStore) update(ctx context.Context, task *Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task == nil {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task must not be nil",
		}
	}
	if task.ID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task ID must not be empty",
		}
	}

	return s.mutate(ctx, task.TenantID, task.ID, func(current *Task, _ int64) (*Task, error) {
		if task.Version != current.Version {
			return nil, &TaskVersionConflictError{
				TaskID:          task.ID,
				ExpectedVersion: task.Version,
				ActualVersion:   current.Version,
			}
		}
		return task, nil
	})
}

// =============================================================================
// ClaimTask / CompleteTask / FailTask
// =============================================================================

// ClaimTask claims a task for an agent. The task must be pending, unclaimed and
// have all of its dependencies completed.
func (s *taskStore) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	err := s.claim(ctx, tenantID, taskID, agentID)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

func (s *taskStore) claim(ctx context.Context, tenantID, taskID, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if agentID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "agent ID must not be empty",
		}
	}

	return s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		if current.Status == TaskStatusClaimed {
			return nil, &TaskAlreadyClaimedError{
				TaskID:       taskID,
				ClaimedBy:    current.AssignedTo,
				RequestingBy: agentID,
			}
		}
		if current.Status != TaskStatusPending {
			return nil, &TaskInvalidStateError{
				TaskID:       taskID,
				CurrentState: current.Status,
				TargetState:  TaskStatusClaimed,
			}
		}
		if err := s.checkDependenciesResolved(ctx, tenantID, current.Dependencies); err != nil {
			return nil, err
		}

		current.Status = TaskStatusClaimed
		current.AssignedTo = agentID
		current.ClaimedAt = now
		return current, nil
	})
}

// CompleteTask marks a claimed or in-progress task as completed with a result.
func (s *taskStore) CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error {
	err := s.finish(ctx, tenantID, taskID, TaskStatusCompleted, result)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

// FailTask marks a claimed or in-progress task as failed with a reason.
func (s *taskStore) FailTask(ctx context.Context, tenantID, taskID, reason string) error {
	err := s.fail(ctx, tenantID, taskID, reason)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

func (s *taskStore) fail(ctx context.Context, tenantID, taskID, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reason == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "failure reason must not be empty",
		}
	}
	return s.finish(ctx, tenantID, taskID, TaskStatusFailed, &TaskResult{Output: reason})
}

// finish drives the single terminal transition shared by CompleteTask and
// FailTask: both require a claimed or in-progress task and both stamp a result
// and a completion time.
func (s *taskStore) finish(ctx context.Context, tenantID, taskID string, target TaskStatus, result *TaskResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		if current.Status != TaskStatusClaimed && current.Status != TaskStatusInProgress {
			return nil, &TaskInvalidStateError{
				TaskID:       taskID,
				CurrentState: current.Status,
				TargetState:  target,
			}
		}

		current.Status = target
		current.Result = result
		current.CompletedAt = now
		return current, nil
	})
}

// checkDependenciesResolved verifies every dependency task is completed. A
// dependency that cannot be read is treated as unresolved: a task must never
// start on the strength of a dependency this store could not confirm.
func (s *taskStore) checkDependenciesResolved(ctx context.Context, tenantID string, dependencies []string) error {
	for _, depID := range dependencies {
		depTask, err := s.GetTask(ctx, tenantID, depID)
		if err != nil {
			return &TaskDependencyBlockedError{
				TaskID:               depID,
				UnresolvedDependency: depID,
			}
		}
		if depTask.Status != TaskStatusCompleted {
			return &TaskDependencyBlockedError{
				TaskID:               depID,
				UnresolvedDependency: depID,
			}
		}
	}
	return nil
}

// =============================================================================
// Artifacts
// =============================================================================

// SubmitTaskArtifacts appends or replaces output artifacts on a task.
// Submission is at-most-once per name: an artifact whose name already exists
// overwrites the prior value. Artifacts whose inline Content exceeds the
// store's inline limit are written to their own key, their Kind is rewritten to
// kv_key and their Content is cleared.
func (s *taskStore) SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []Artifact) error {
	err := s.submitArtifacts(ctx, tenantID, taskID, artifacts)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

func (s *taskStore) submitArtifacts(ctx context.Context, tenantID, taskID string, artifacts []Artifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" || taskID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID and task ID are required",
		}
	}

	// Validate every artifact before mutating any state, so a bad artifact in
	// the batch cannot leave the task half-updated.
	for i := range artifacts {
		if err := validateArtifact(&artifacts[i]); err != nil {
			return err
		}
	}

	return s.mutate(ctx, tenantID, taskID, func(current *Task, _ int64) (*Task, error) {
		// Name -> index over the stored artifacts so a resubmission overwrites
		// instead of appending a duplicate.
		index := make(map[string]int, len(current.OutputArtifacts))
		for i, a := range current.OutputArtifacts {
			index[a.Name] = i
		}

		for _, artifact := range artifacts {
			stored, err := s.offloadArtifact(ctx, tenantID, taskID, artifact)
			if err != nil {
				return nil, err
			}
			if i, exists := index[stored.Name]; exists {
				current.OutputArtifacts[i] = stored
				continue
			}
			index[stored.Name] = len(current.OutputArtifacts)
			current.OutputArtifacts = append(current.OutputArtifacts, stored)
		}
		return current, nil
	})
}

// offloadArtifact writes an oversized inline artifact to its own key and
// returns the reference form to embed in the task record. Artifacts within the
// inline limit, and artifacts that are already references, pass through
// unchanged.
func (s *taskStore) offloadArtifact(ctx context.Context, tenantID, taskID string, artifact Artifact) (Artifact, error) {
	if artifact.Kind != ArtifactKindInline || int64(len(artifact.Content)) <= s.inlineLimit {
		return artifact, nil
	}

	key, err := taskArtifactKey(tenantID, taskID, artifact.Name)
	if err != nil {
		return Artifact{}, err
	}
	if err := s.kv.Put(ctx, key, artifact.Content); err != nil {
		return Artifact{}, backendError("failed to store artifact", err)
	}

	artifact.Kind = ArtifactKindKVKey
	artifact.Ref = ArtifactRef{Path: key}
	artifact.Content = nil
	return artifact, nil
}

// GetTaskArtifact retrieves a named artifact, resolving an offloaded payload to
// its full byte content before returning.
func (s *taskStore) GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	task, err := s.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}

	prefix, err := taskArtifactPrefix(tenantID, taskID)
	if err != nil {
		return nil, err
	}

	for _, artifact := range task.OutputArtifacts {
		if artifact.Name != name {
			continue
		}
		// Only a reference this store wrote — one inside this task's own
		// artifact subtree — is dereferenced. A kv_key ref pointing anywhere
		// else is returned unresolved rather than turned into an arbitrary
		// read of the shared keyspace on the caller's behalf.
		if artifact.Kind == ArtifactKindKVKey && strings.HasPrefix(artifact.Ref.Path, prefix) {
			return s.resolveArtifact(ctx, taskID, artifact)
		}
		resolved := artifact
		return &resolved, nil
	}

	return nil, &ArtifactNotFoundError{TaskID: taskID, Name: name}
}

// resolveArtifact reads an offloaded payload and returns the artifact with its
// content and size filled in.
func (s *taskStore) resolveArtifact(ctx context.Context, taskID string, artifact Artifact) (*Artifact, error) {
	data, err := s.kv.Get(ctx, artifact.Ref.Path)
	if err != nil {
		if qerrors.IsNotFound(err) {
			// The record outlived its payload; the artifact is gone.
			return nil, &ArtifactNotFoundError{TaskID: taskID, Name: artifact.Name}
		}
		return nil, backendError("failed to read artifact", err)
	}
	if data == nil {
		return nil, &ArtifactNotFoundError{TaskID: taskID, Name: artifact.Name}
	}

	artifact.Content = data
	artifact.Size = int64(len(data))
	return &artifact, nil
}

// ListTaskArtifacts returns every output artifact's metadata without resolving
// offloaded payloads: kind, name, MIME type, size, ref and metadata only.
func (s *taskStore) ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	task, err := s.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}

	result := make([]Artifact, len(task.OutputArtifacts))
	for i, artifact := range task.OutputArtifacts {
		if artifact.Kind == ArtifactKindKVKey {
			artifact.Content = nil
		}
		result[i] = artifact
	}
	return result, nil
}

// =============================================================================
// DeleteTask / DeleteTenantTasks
// =============================================================================

// DeleteTask removes a task and the artifact payloads it owns.
func (s *taskStore) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	err := s.remove(ctx, tenantID, taskID)
	s.audit(ctx, AuditActionDelete, tenantID, taskID, err)
	return err
}

func (s *taskStore) remove(ctx context.Context, tenantID, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}

	key, err := taskRecordKey(tenantID, taskID)
	if err != nil {
		return err
	}
	prefix, err := taskArtifactPrefix(tenantID, taskID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(key)
	defer unlock()

	// Confirms both that the task exists and that it belongs to this tenant.
	if _, err := s.load(ctx, key, tenantID, taskID); err != nil {
		return err
	}

	// The record goes first. A failure after this point leaves orphaned
	// payloads, which waste space; the reverse order would leave a live record
	// pointing at payloads that no longer exist.
	if err := s.kv.Delete(ctx, key); err != nil {
		return backendError("failed to delete task", err)
	}
	return s.deleteKeysUnder(ctx, prefix)
}

// DeleteTenantTasks deletes every task owned by a tenant, together with that
// tenant's artifact payloads.
func (s *taskStore) DeleteTenantTasks(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}

	prefix, err := taskScanPrefix(tenantID)
	if err != nil {
		s.audit(ctx, AuditActionDelete, tenantID, "", err)
		return err
	}
	artifacts, err := tenantArtifactPrefix(tenantID)
	if err != nil {
		s.audit(ctx, AuditActionDelete, tenantID, "", err)
		return err
	}

	// The scan is bounded to this tenant's subtree and yields identifiers only,
	// so the cascade can never reach another tenant and never holds more than
	// this tenant's key list in memory.
	taskIDs, err := s.scanTaskIDs(ctx, prefix)
	if err != nil {
		s.audit(ctx, AuditActionDelete, tenantID, "", err)
		return err
	}

	for _, taskID := range taskIDs {
		key, keyErr := taskRecordKey(tenantID, taskID)
		if keyErr != nil {
			// The key came from a scan of valid keys, so this cannot happen
			// unless the keyspace was written to directly. Skip rather than
			// abandon the rest of the cascade.
			continue
		}

		unlock := s.recordMu.lock(key)
		delErr := s.kv.Delete(ctx, key)
		unlock()

		if delErr != nil {
			wrapped := backendError("failed to delete task", delErr)
			s.audit(ctx, AuditActionDelete, tenantID, taskID, wrapped)
			return wrapped
		}
		s.audit(ctx, AuditActionDelete, tenantID, taskID, nil)
	}

	// Sweeping the whole tenant artifact subtree also reclaims payloads whose
	// record was already gone.
	if err := s.deleteKeysUnder(ctx, artifacts); err != nil {
		s.audit(ctx, AuditActionDelete, tenantID, "", err)
		return err
	}
	return nil
}

// scanTaskIDs streams one tenant's task keys and returns their identifiers.
func (s *taskStore) scanTaskIDs(ctx context.Context, prefix string) ([]string, error) {
	var taskIDs []string
	scanErr := s.kv.Scan(ctx, prefix, func(key string, _ []byte) error {
		taskIDs = append(taskIDs, idFromKey(key, prefix))
		return nil
	})
	if scanErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, backendError("failed to list tasks", scanErr)
	}
	return taskIDs, nil
}

// deleteKeysUnder removes every key under prefix. Keys are listed before any is
// removed so the delete loop does not mutate the range it is walking.
func (s *taskStore) deleteKeysUnder(ctx context.Context, prefix string) error {
	keys, err := s.kv.List(ctx, prefix)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return backendError("failed to list artifacts", err)
	}
	for _, key := range keys {
		if err := s.kv.Delete(ctx, key); err != nil {
			return backendError("failed to delete artifact", err)
		}
	}
	return nil
}

// =============================================================================
// Audit
// =============================================================================

// audit emits exactly one store event per mutating call. A rejected or failed
// attempt is recorded with Success false, which is the signal an auditor needs
// to show that a change was attempted and refused.
func (s *taskStore) audit(ctx context.Context, action, tenantID, taskID string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceTask,
		ResourceID: taskID,
		Action:     action,
		Success:    err == nil,
	})
}
