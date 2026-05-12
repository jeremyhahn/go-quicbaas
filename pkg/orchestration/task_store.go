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
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// taskEntry wraps serialized task data in a pointer-comparable struct.
// sync.Map.CompareAndSwap requires comparable values; []byte is not
// comparable, but *taskEntry pointers are compared by identity which
// provides the exact CAS semantics needed for lock-free updates.
type taskEntry struct {
	data []byte
}

// inMemoryTaskStore implements TaskStore using sync.Map for lock-free
// concurrent access. This is the default store used when no external
// persistence backend is configured.
type inMemoryTaskStore struct {
	tasks       sync.Map // map[string]*taskEntry (keyed by "tenantID/taskID")
	count       atomic.Int64
	kvStore     interfaces.KVStore // optional; used for large artifact storage
	inlineLimit int64              // max artifact bytes stored inline (default InlineLimit)
}

// NewInMemoryTaskStore creates a new in-memory task store.
func NewInMemoryTaskStore() TaskStore {
	return &inMemoryTaskStore{inlineLimit: InlineLimit}
}

// NewInMemoryTaskStoreWithKV creates a task store that offloads large artifacts
// (> inlineLimit) to the provided KV store.  A limit of 0 uses the package-level
// InlineLimit default.
func NewInMemoryTaskStoreWithKV(kv interfaces.KVStore, limit int64) TaskStore {
	if limit <= 0 {
		limit = InlineLimit
	}
	return &inMemoryTaskStore{kvStore: kv, inlineLimit: limit}
}

// taskKey builds a composite key from tenant and task IDs.
func taskKey(tenantID, taskID string) string {
	return tenantID + "/" + taskID
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

// CreateTask creates a new task. Returns error if task ID already exists.
func (s *inMemoryTaskStore) CreateTask(ctx context.Context, task *Task) error {
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
	if task.TenantID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}

	now := time.Now().UnixNano()
	task.CreatedAt = now
	task.UpdatedAt = now
	task.Version = 1

	if task.Status == "" {
		task.Status = TaskStatusPending
	}

	data, err := json.Marshal(task)
	if err != nil {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreMarshal,
			Message:  "failed to marshal task: " + err.Error(),
		}
	}

	key := taskKey(task.TenantID, task.ID)
	if _, loaded := s.tasks.LoadOrStore(key, &taskEntry{data: data}); loaded {
		return &TaskAlreadyExistsError{TaskID: task.ID}
	}

	s.count.Add(1)
	return nil
}

// GetTask retrieves a task by ID within a tenant namespace.
func (s *inMemoryTaskStore) GetTask(ctx context.Context, tenantID, taskID string) (*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if taskID == "" {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task ID must not be empty",
		}
	}

	key := taskKey(tenantID, taskID)
	val, ok := s.tasks.Load(key)
	if !ok {
		return nil, &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
	}

	entry, ok := val.(*taskEntry)
	if !ok {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreMarshal,
			Message:  "task value is not *taskEntry",
		}
	}

	var task Task
	if err := json.Unmarshal(entry.data, &task); err != nil {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreMarshal,
			Message:  "failed to unmarshal task: " + err.Error(),
		}
	}

	return &task, nil
}

// UpdateTask updates an existing task. Uses CAS for conflict detection.
// The task's Version field must match the stored version; otherwise a
// TaskVersionConflictError is returned.
func (s *inMemoryTaskStore) UpdateTask(ctx context.Context, task *Task) error {
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
	if task.TenantID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}

	key := taskKey(task.TenantID, task.ID)

	// Load-compare-swap loop for lock-free CAS
	for {
		existing, ok := s.tasks.Load(key)
		if !ok {
			return &TaskNotFoundError{TenantID: task.TenantID, TaskID: task.ID}
		}

		oldEntry, ok := existing.(*taskEntry)
		if !ok {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "task value is not *taskEntry",
			}
		}

		var current Task
		if err := json.Unmarshal(oldEntry.data, &current); err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to unmarshal existing task: " + err.Error(),
			}
		}

		if task.Version != current.Version {
			return &TaskVersionConflictError{
				TaskID:          task.ID,
				ExpectedVersion: task.Version,
				ActualVersion:   current.Version,
			}
		}

		task.Version = current.Version + 1
		task.UpdatedAt = time.Now().UnixNano()

		newData, err := json.Marshal(task)
		if err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to marshal updated task: " + err.Error(),
			}
		}

		if s.tasks.CompareAndSwap(key, oldEntry, &taskEntry{data: newData}) {
			return nil
		}
		// CompareAndSwap failed (another goroutine changed it), retry
	}
}

// DeleteTask removes a task.
func (s *inMemoryTaskStore) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if taskID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task ID must not be empty",
		}
	}

	key := taskKey(tenantID, taskID)
	if _, loaded := s.tasks.LoadAndDelete(key); !loaded {
		return &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
	}

	s.count.Add(-1)
	return nil
}

// ListTasks lists tasks matching the filter within a tenant.
func (s *inMemoryTaskStore) ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}

	prefix := tenantID + "/"
	tasks := make([]*Task, 0)
	var unmarshalErr error

	s.tasks.Range(func(key, value interface{}) bool {
		if ctx.Err() != nil {
			return false
		}

		keyStr, ok := key.(string)
		if !ok || !strings.HasPrefix(keyStr, prefix) {
			return true
		}

		entry, ok := value.(*taskEntry)
		if !ok {
			return true
		}

		var task Task
		if err := json.Unmarshal(entry.data, &task); err != nil {
			unmarshalErr = &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to unmarshal task: " + err.Error(),
			}
			return false
		}

		// Apply filters
		if filter != nil {
			if filter.Status != "" && task.Status != filter.Status {
				return true
			}
			if filter.TeamName != "" && task.TeamName != filter.TeamName {
				return true
			}
			if filter.AssignedTo != "" && task.AssignedTo != filter.AssignedTo {
				return true
			}
			if filter.CreatedBy != "" && task.CreatedBy != filter.CreatedBy {
				return true
			}
		}

		tasks = append(tasks, &task)

		// Apply limit
		if filter != nil && filter.Limit > 0 && len(tasks) >= filter.Limit {
			return false
		}

		return true
	})

	if unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return tasks, nil
}

// ClaimTask atomically claims a task for an agent using compare-and-swap.
// The task must be in pending status and not already assigned.
func (s *inMemoryTaskStore) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if taskID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task ID must not be empty",
		}
	}
	if agentID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "agent ID must not be empty",
		}
	}

	key := taskKey(tenantID, taskID)

	// Atomic CAS loop: check pending + unassigned, then claim
	for {
		existing, ok := s.tasks.Load(key)
		if !ok {
			return &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}

		oldEntry, ok := existing.(*taskEntry)
		if !ok {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "task value is not *taskEntry",
			}
		}

		var task Task
		if err := json.Unmarshal(oldEntry.data, &task); err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to unmarshal task: " + err.Error(),
			}
		}

		if task.Status == TaskStatusClaimed {
			return &TaskAlreadyClaimedError{
				TaskID:       taskID,
				ClaimedBy:    task.AssignedTo,
				RequestingBy: agentID,
			}
		}

		if task.Status != TaskStatusPending {
			return &TaskInvalidStateError{
				TaskID:       taskID,
				CurrentState: task.Status,
				TargetState:  TaskStatusClaimed,
			}
		}

		// Check dependencies are resolved
		if err := s.checkDependenciesResolved(ctx, tenantID, task.Dependencies); err != nil {
			return err
		}

		now := time.Now().UnixNano()
		task.Status = TaskStatusClaimed
		task.AssignedTo = agentID
		task.ClaimedAt = now
		task.UpdatedAt = now
		task.Version++

		newData, err := json.Marshal(&task)
		if err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to marshal claimed task: " + err.Error(),
			}
		}

		if s.tasks.CompareAndSwap(key, oldEntry, &taskEntry{data: newData}) {
			return nil
		}
		// CAS failed, retry
	}
}

// CompleteTask marks a task as completed with a result.
// The task must be in claimed or in_progress status.
func (s *inMemoryTaskStore) CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if taskID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task ID must not be empty",
		}
	}

	key := taskKey(tenantID, taskID)

	for {
		existing, ok := s.tasks.Load(key)
		if !ok {
			return &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}

		oldEntry, ok := existing.(*taskEntry)
		if !ok {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "task value is not *taskEntry",
			}
		}

		var task Task
		if err := json.Unmarshal(oldEntry.data, &task); err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to unmarshal task: " + err.Error(),
			}
		}

		if task.Status != TaskStatusClaimed && task.Status != TaskStatusInProgress {
			return &TaskInvalidStateError{
				TaskID:       taskID,
				CurrentState: task.Status,
				TargetState:  TaskStatusCompleted,
			}
		}

		now := time.Now().UnixNano()
		task.Status = TaskStatusCompleted
		task.Result = result
		task.CompletedAt = now
		task.UpdatedAt = now
		task.Version++

		newData, err := json.Marshal(&task)
		if err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to marshal completed task: " + err.Error(),
			}
		}

		if s.tasks.CompareAndSwap(key, oldEntry, &taskEntry{data: newData}) {
			return nil
		}
		// CAS failed, retry
	}
}

// FailTask marks a task as failed with a reason.
// The task must be in claimed or in_progress status.
func (s *inMemoryTaskStore) FailTask(ctx context.Context, tenantID, taskID, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}
	if taskID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "task ID must not be empty",
		}
	}
	if reason == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "failure reason must not be empty",
		}
	}

	key := taskKey(tenantID, taskID)

	for {
		existing, ok := s.tasks.Load(key)
		if !ok {
			return &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}

		oldEntry, ok := existing.(*taskEntry)
		if !ok {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "task value is not *taskEntry",
			}
		}

		var task Task
		if err := json.Unmarshal(oldEntry.data, &task); err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to unmarshal task: " + err.Error(),
			}
		}

		if task.Status != TaskStatusClaimed && task.Status != TaskStatusInProgress {
			return &TaskInvalidStateError{
				TaskID:       taskID,
				CurrentState: task.Status,
				TargetState:  TaskStatusFailed,
			}
		}

		now := time.Now().UnixNano()
		task.Status = TaskStatusFailed
		task.Result = &TaskResult{Output: reason}
		task.CompletedAt = now
		task.UpdatedAt = now
		task.Version++

		newData, err := json.Marshal(&task)
		if err != nil {
			return &TaskStoreError{
				Sentinel: ErrTaskStoreMarshal,
				Message:  "failed to marshal failed task: " + err.Error(),
			}
		}

		if s.tasks.CompareAndSwap(key, oldEntry, &taskEntry{data: newData}) {
			return nil
		}
		// CAS failed, retry
	}
}

// DeleteTenantTasks deletes ALL tasks for a tenant (cascade delete).
func (s *inMemoryTaskStore) DeleteTenantTasks(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "tenant ID must not be empty",
		}
	}

	prefix := tenantID + "/"
	var deleted int64

	s.tasks.Range(func(key, value interface{}) bool {
		if ctx.Err() != nil {
			return false
		}

		keyStr, ok := key.(string)
		if !ok {
			return true
		}

		if strings.HasPrefix(keyStr, prefix) {
			s.tasks.Delete(key)
			deleted++
		}

		return true
	})

	s.count.Add(-deleted)
	return nil
}

// artifactKVKey returns the KV store key for a large artifact.
func artifactKVKey(tenantID, taskID, name string) string {
	return "artifact:" + tenantID + ":" + taskID + ":" + name
}

// SubmitTaskArtifacts appends or replaces output artifacts on a task.
// Artifacts whose Content exceeds inlineLimit are promoted to the KV store;
// their Kind is rewritten to kv_key and Content is cleared.
func (s *inMemoryTaskStore) SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []Artifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" || taskID == "" {
		return &TaskStoreError{Sentinel: ErrTaskStoreInvalidInput, Message: "tenant ID and task ID are required"}
	}

	// Validate all artifacts before mutating any state.
	for i := range artifacts {
		if err := validateArtifact(&artifacts[i]); err != nil {
			return err
		}
	}

	key := taskKey(tenantID, taskID)

	for {
		existing, ok := s.tasks.Load(key)
		if !ok {
			return &TaskNotFoundError{TenantID: tenantID, TaskID: taskID}
		}

		oldEntry, ok := existing.(*taskEntry)
		if !ok {
			return &TaskStoreError{Sentinel: ErrTaskStoreMarshal, Message: "task value is not *taskEntry"}
		}

		var task Task
		if err := json.Unmarshal(oldEntry.data, &task); err != nil {
			return &TaskStoreError{Sentinel: ErrTaskStoreMarshal, Message: "failed to unmarshal task: " + err.Error()}
		}

		// Build name → index map for the existing artifact slice so we can
		// overwrite rather than append duplicates.
		index := make(map[string]int, len(task.OutputArtifacts))
		for i, a := range task.OutputArtifacts {
			index[a.Name] = i
		}

		for _, a := range artifacts {
			promoted := a

			// Promote large inline artifacts to KV storage.
			if promoted.Kind == ArtifactKindInline && int64(len(promoted.Content)) > s.inlineLimit {
				if s.kvStore == nil {
					return &ArtifactTooLargeError{
						Name:  promoted.Name,
						Size:  int64(len(promoted.Content)),
						Limit: s.inlineLimit,
					}
				}
				kvKey := artifactKVKey(tenantID, taskID, promoted.Name)
				if putErr := s.kvStore.Put(ctx, kvKey, promoted.Content); putErr != nil {
					return &TaskStoreError{Sentinel: ErrTaskStoreMarshal, Message: "failed to store artifact in KV: " + putErr.Error()}
				}
				promoted.Kind = ArtifactKindKVKey
				promoted.Ref = ArtifactRef{Path: kvKey}
				promoted.Content = nil
			}

			if idx, exists := index[promoted.Name]; exists {
				task.OutputArtifacts[idx] = promoted
			} else {
				index[promoted.Name] = len(task.OutputArtifacts)
				task.OutputArtifacts = append(task.OutputArtifacts, promoted)
			}
		}

		task.UpdatedAt = time.Now().UnixNano()
		task.Version++

		newData, err := json.Marshal(&task)
		if err != nil {
			return &TaskStoreError{Sentinel: ErrTaskStoreMarshal, Message: "failed to marshal task with artifacts: " + err.Error()}
		}

		if s.tasks.CompareAndSwap(key, oldEntry, &taskEntry{data: newData}) {
			return nil
		}
		// CAS failed; another writer raced — retry.
	}
}

// GetTaskArtifact retrieves a named artifact, resolving kv_key Refs to their
// full byte content before returning.
func (s *inMemoryTaskStore) GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	task, err := s.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}

	for _, a := range task.OutputArtifacts {
		if a.Name != name {
			continue
		}

		// Resolve KV-stored content on demand.
		if a.Kind == ArtifactKindKVKey && a.Ref.Path != "" && s.kvStore != nil {
			data, getErr := s.kvStore.Get(ctx, a.Ref.Path)
			if getErr != nil {
				return nil, &TaskStoreError{Sentinel: ErrTaskStoreMarshal, Message: "failed to fetch artifact from KV: " + getErr.Error()}
			}
			resolved := a
			resolved.Content = data
			resolved.Size = int64(len(data))
			return &resolved, nil
		}

		result := a
		return &result, nil
	}

	return nil, &ArtifactNotFoundError{TaskID: taskID, Name: name}
}

// ListTaskArtifacts returns all output artifact metadata without resolving
// large Refs to their content (returns Kind, Name, MIMEType, Size, Ref, Metadata).
func (s *inMemoryTaskStore) ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	task, err := s.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}

	// Return a shallow copy with Content omitted for kv_key artifacts.
	result := make([]Artifact, len(task.OutputArtifacts))
	for i, a := range task.OutputArtifacts {
		stripped := a
		if a.Kind == ArtifactKindKVKey {
			stripped.Content = nil
		}
		result[i] = stripped
	}
	return result, nil
}

// checkDependenciesResolved verifies all dependency tasks are completed.
func (s *inMemoryTaskStore) checkDependenciesResolved(ctx context.Context, tenantID string, dependencies []string) error {
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

// Compile-time interface satisfaction check.
var _ TaskStore = (*inMemoryTaskStore)(nil)
