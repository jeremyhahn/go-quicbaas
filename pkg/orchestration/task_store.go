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
	"maps"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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

// taskSourceUnscopedProject is the project segment of a source index entry for
// a task that is not scoped to a project. A fixed segment keeps the key shape
// uniform; validateKeySegment refuses it as a real project ID because it is
// reserved here.
const taskSourceUnscopedProject = "_"

// taskSourcePredecessorKey is the task metadata key naming the task a new task
// took its source over from. It is metadata rather than a field for the same
// reason attemptHistoryKey is: it is provenance a person reads, not state the
// engine branches on.
const taskSourcePredecessorKey = "predecessor_task"

// taskSourcePredecessorBlockReasonKey names, on a task that took its source
// over from a predecessor still blocked on an environment fault, the fault
// class that predecessor was held on. It is set only for that case — a
// terminal predecessor leaves it empty — so the sweep can name the reason a
// re-label re-armed the origin.
const taskSourcePredecessorBlockReasonKey = "predecessor_block_reason"

// taskSourceIndexKey returns the key reserving one task source:
// orch/task-source/{tenantID}/{projectID}/{kind}/{ref}. The ref is a
// caller-supplied identifier that may legitimately contain the separator
// (an issue reference such as "owner/repo#12"), so it is encoded the same way
// artifact names are, onto a single separator-free segment.
func taskSourceIndexKey(tenantID, projectID string, source *TaskSourceRef) (string, error) {
	prefix, err := taskSourcePrefix(tenantID)
	if err != nil {
		return "", err
	}
	if projectID == "" {
		projectID = taskSourceUnscopedProject
	}
	if err := validateKeySegment("project_id", projectID); err != nil {
		return "", taskKeyError(err)
	}
	if err := source.Validate(); err != nil {
		return "", err
	}
	return prefix + projectID + keySeparator + string(source.Kind) + keySeparator +
		base64.RawURLEncoding.EncodeToString([]byte(source.Ref)), nil
}

// taskSourcePrefix returns the prefix covering every source index entry owned
// by tenantID.
func taskSourcePrefix(tenantID string) (string, error) {
	prefix, err := tenantPrefix(taskSourceKeyspace, tenantID)
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
// The guarded-create capability
// =============================================================================

// guardedCreator is the write-if-absent capability a create needs from its
// backing store, discovered by type assertion.
//
// Declared as the narrow verb actually used rather than reusing
// qrdbsdk.AtomicKVStore, because that interface embeds go-qrdb's full twelve
// method KVStore including the secondary-index methods. quicbaas's stores speak
// the six-verb interfaces.KVStore, so asserting the wide interface would refuse
// every store this package is ever given — including the tenant-routed one that
// can do the work. This is the capability, not a convenience narrowing.
//
// Type assertion rather than a method on interfaces.KVStore is go-qrdb's own
// prescribed pattern: not every store sits somewhere that can honour a guarded
// write, and forcing the method everywhere would produce stubs that either lie
// about atomicity or report "unsupported" — which a caller cannot tell apart
// from a real refusal.
//
// A store that lacks it gets NO EMULATION. Exists-then-Put is precisely the
// race a guarded create exists to eliminate, so a store that cannot decide a
// create makes CreateTask refuse.
type guardedCreator interface {
	PutIfAbsent(ctx context.Context, key string, value []byte) (stored bool, err error)
}

// guardedDeleter is the delete-if-unchanged capability, the release half of
// guardedCreator, discovered the same way.
//
// It is a SEPARATE interface because consumers differ: the task store only ever
// creates, while the account store both claims and releases a scope pointer.
// Narrowing each consumer to the verbs it uses is the Go form; go-qrdb defines
// the two as one capability and the tenant router forwards them together, so a
// store offering one in practice offers both.
//
// The release half is not optional where a claim can be given back. A guarded
// create paired with an UNGUARDED delete is half a primitive (go-qrdb#41): read
// the value, compare it, then delete, and two releasers both observe the same
// holder — the first deletes, a third party legitimately claims the now-absent
// key, and the second delete removes THEIR value.
type guardedDeleter interface {
	CompareAndDelete(ctx context.Context, key string, expected []byte) (deleted bool, err error)
}

// =============================================================================
// taskStore — the single KV-backed TaskStore
// =============================================================================

// taskStore is the only TaskStore implementation. It holds no copy of the data
// it serves: every task record and every offloaded artifact payload lives in
// the injected interfaces.KVStore, so durability, replication and
// encryption-at-rest are properties of that store alone. Injecting
// NewMemoryTaskKV() yields a fast in-process store; injecting the server's
// qrdb-backed KV yields a durable, replicated one. There is no second
// implementation to keep in sync, and a store rebuilt over the same KV sees
// exactly what its predecessor wrote.
//
// Concurrency, and what Version does and does not guarantee.
//
// Record writes are read-modify-write sequences held under recordMu, the
// per-key mutex from store_keys.go, so every transition is atomic against other
// writers IN THIS PROCESS. That is all a mutex can be, and for ordinary
// transitions it is enough: Version detects a writer that raced from another
// node, which loses the version check and receives TaskVersionConflictError. A
// concurrent overwrite is DETECTED, not prevented, and detection is the right
// contract for an edit whose loser can re-read and re-apply.
//
// A CLAIM IS NOT SUCH AN EDIT, AND IS NOT DECIDED HERE. Two nodes both claiming
// one task is not a lost update to retry — it is two agents doing the same work,
// which is the SDLC module's founding failure (nine workers on go-frost#6). So the
// claim is arbitrated by the injected TaskClaimArbiter before the transition is
// written, on go-qrdb's transaction plane where a compare and a write are one
// Raft proposal. The record transition then belongs to the winner alone and is
// uncontended by construction. See task_claim_arbiter.go for why the mark lives
// on the system plane while this record stays on the tenant's.
//
// VERSION'S GUARANTEE, restated for what it now covers: Version is the count of
// accepted writes to this record, and a given version is admitted for claim AT
// MOST ONCE CLUSTER-WIDE. Every contender reads the same record and so claims
// the same version; the arbiter's mark advances only past versions already
// admitted. Version therefore still detects a raced edit exactly as before, and
// additionally is the generation that makes a claim identifiable rather than
// merely repeated.
type taskStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// arbiter decides which contender may claim a task. It is required rather
	// than optional: an arbiter chosen by default is a silent fallback, and a
	// fleet store that quietly got process-local arbitration would report
	// success while dispatching every contender.
	arbiter TaskClaimArbiter

	// creator is kv's guarded-create capability, asserted once at construction.
	// Nil when the backing store cannot decide a contested create, in which
	// case CreateTask refuses rather than probing and hoping.
	creator guardedCreator

	// releaser is kv's guarded-delete capability, the release half of creator.
	// A source index entry is given back only when it still names the task
	// releasing it, so a second adopter's reservation is never removed by the
	// first adopter's cleanup.
	releaser guardedDeleter

	// inlineLimit is the maximum artifact size, in bytes, embedded directly in
	// a task record. Larger artifacts are written to their own key.
	inlineLimit int64

	// recordMu serializes the read-modify-write sequence on one task record.
	// Writes to different tasks proceed in parallel.
	recordMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ TaskStore = (*taskStore)(nil)

// NewTaskStore returns a TaskStore backed by kv, with claims decided by
// arbiter.
//
// The backing store decides the storage semantics: pass NewMemoryTaskKV() for
// an in-process store, or the server's qrdb-backed KV for a durable, replicated
// one. It MUST be able to decide a guarded create (guardedCreator); a store
// that cannot refuses every create rather than probing and hoping, so
// kvstore.NewMemory() is not a valid backing store for this type.
//
// THE ARBITER IS POSITIONAL AND REQUIRED, which is the point. Claim
// arbitration cannot be inferred from the KV — a replicated store and a memory
// store present the same six verbs — so the caller is the only party that knows
// whether this store serves one process or a fleet. Making it an option with a
// default would put that answer in a line nobody writes and nobody reviews, and
// the default would be wrong for exactly the deployment where being wrong costs
// the most. Pass NewTransactionClaimArbiter for a clustered store and
// NewProcessClaimArbiter for a single-process one; a nil arbiter refuses every
// claim rather than deciding it locally.
func NewTaskStore(kv interfaces.KVStore, arbiter TaskClaimArbiter, opts ...StoreOption) TaskStore {
	cfg := newStoreConfig(opts...)
	store := &taskStore{
		kv:          kv,
		auditor:     cfg.auditor,
		arbiter:     arbiter,
		inlineLimit: cfg.inlineLimit,
	}
	// Asserted once here rather than per call. The capability is a static
	// property of the injected store, so probing it on every create would ask
	// the same question forever and answer it identically.
	if creator, ok := kv.(guardedCreator); ok {
		store.creator = creator
	}
	if releaser, ok := kv.(guardedDeleter); ok {
		store.releaser = releaser
	}
	return store
}

// =============================================================================
// Status transitions
// =============================================================================

// taskTransitions is the one table of admissible status transitions. Every
// transition the store writes is checked here, so the lifecycle is stated once
// and read in one place:
//
//	pending     -> claimed (an agent) | in_progress (the engine) | blocked
//	claimed     -> in_progress | completed | failed | blocked
//	in_progress -> completed | failed | blocked
//	blocked     -> pending (readmit) | failed
//
// completed and cancelled are terminal. FAILED IS NOT, and that is the whole
// point of the one transition out of it.
//
// WHY pending -> in_progress EXISTS: THE ENGINE CLAIMS WHAT IT DISPATCHES.
// claimed means "an agent has taken this and has not started yet", which is a
// state only the two-step agent hand-over passes through — ClaimTask then the
// agent's own start. A job the engine dispatches itself has no such gap: the
// claim and the start are one decision (ClaimTaskSpawn), so the edge it needs
// is straight to in_progress and routing it through claimed would invent a
// claimant that does not exist.
//
// Without this edge the engine had no edge at all, and the task stayed pending
// for the whole life of its job. Measured on the burn-in node (go-truststrap#14,
// 2026-09-21): a task adopted at 02:13:49 and merged at 02:28:06 read `pending`
// for those fourteen minutes and went straight to `completed`. Every reader of
// Task.Status — the API, `orchestrator task get`, the task list, the label
// reconciler's claim sweep — called a running delivery queued, and anything
// reasoning from StatusChangedAt saw a fourteen-minute-old pending task, which
// is also the signature of a stranded binding: a healthy run and a real stall
// were indistinguishable by the record (go-quicbaas#966).
//
// WHY failed -> pending EXISTS. A task fails for two very different reasons and
// this table could not tell them apart: the work was impossible, or the fleet
// broke underneath it. The engine already knows the difference —
// operatorFaultClasses separates them and Task.ChargedAttempts refunds an
// environment fault so a blip does not spend the budget — but all of that
// arithmetic was unreachable once the task landed here, because from a terminal
// state nothing can ever dispatch again. Measured on the burn-in node
// 2026-09-13: of eighteen failed-or-blocked tasks, NINE died from container and
// daemon faults during a deploy window, every one of them permanently, and the
// only recovery was DeleteTask — which frees the source index by destroying the
// task's history. A queue that cannot drain itself after a fleet disturbance is
// not a queue.
//
// WHY blocked -> completed EXISTS. A parked task is sometimes finished by a
// person rather than by another attempt: the owner reads the question, decides
// no further work is needed, and closes it (the attention inbox's
// close_as_done, go-quicbaas#1020). Without this edge the only ways out of
// blocked were "run it again" and "call it a failure", and neither is what was
// decided. IT IS REACHED ONLY BY CloseBlockedTask: CompleteTask still refuses a
// blocked task, because a worker reporting completion for a task that has
// since been parked is a stale report, not a ruling.
//
// failed -> pending IS REACHED ONLY BY ReadmitTask, which is an explicit, audited operator
// decision that records TaskConcludedReadmitted and preserves the prior
// attempts. Nothing readmits a task automatically: a task that failed because
// the work is impossible stays failed until a person says otherwise. That is
// why this is one edge and not a general un-terminal, and why the safer half of
// the fix — concluding an environment fault as blocked rather than failed in the
// first place, so no operator action is needed at all — is still owed.
var taskTransitions = map[TaskStatus]map[TaskStatus]struct{}{
	TaskStatusPending: {
		TaskStatusClaimed:    {},
		TaskStatusInProgress: {},
		TaskStatusBlocked:    {},
	},
	TaskStatusClaimed: {
		TaskStatusInProgress: {},
		TaskStatusCompleted:  {},
		TaskStatusFailed:     {},
		TaskStatusBlocked:    {},
	},
	TaskStatusInProgress: {
		TaskStatusCompleted: {},
		TaskStatusFailed:    {},
		TaskStatusBlocked:   {},
	},
	TaskStatusBlocked: {
		TaskStatusPending:   {},
		TaskStatusCompleted: {},
		TaskStatusFailed:    {},
	},
	TaskStatusFailed: {
		TaskStatusPending: {},
	},
}

// taskTransitionAllowed reports whether a task may move from one status to
// another.
func taskTransitionAllowed(from, to TaskStatus) bool {
	_, ok := taskTransitions[from][to]
	return ok
}

// transition moves a task to target after checking the table, stamping the
// instant of the change. It is the single place a status is written.
func transition(task *Task, target TaskStatus, now int64) error {
	if !taskTransitionAllowed(task.Status, target) {
		return &TaskInvalidStateError{
			TaskID:       task.ID,
			CurrentState: task.Status,
			TargetState:  target,
		}
	}
	task.Status = target
	task.StatusChangedAt = now
	return nil
}

// NewInMemoryTaskStore returns a memory-backed binding of the store returned by
// NewTaskStore — not a second implementation. It is equivalent to
// NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter()) and exists for
// tests and embedded use that need no durability.
//
// The process arbiter is correct here and only here: the whole KV lives in this
// process, so recordMu has already serialized every contender there is.
//
// The backing store is go-qrdb's memory KVStore rather than kvstore.NewMemory()
// because a create must be guarded: that store offers PutIfAbsent, and its
// atomicity is a mutex rather than consensus — honest and sufficient for a
// consistency domain of one process.
func NewInMemoryTaskStore() TaskStore {
	return NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
}

// NewMemoryTaskKV returns an in-process KVStore that can decide a guarded
// create.
//
// It exists because kvstore.NewMemory() cannot: that adapter wraps a raw
// qrdb storage ENGINE, which has no conditional write to delegate to, so a
// task store built over it refuses every create. Tests and embedded callers
// that need a working in-memory task store take this instead.
func NewMemoryTaskKV() interfaces.KVStore {
	return qrdbsdk.NewMemoryKVStore()
}

// NewInMemoryTaskStoreWithKV returns a store whose task records *and* artifact
// payloads both live in kv, with artifacts larger than limit written to their
// own key. A non-positive limit selects InlineLimit.
//
// Named for the in-process use it serves, and it passes the process arbiter to
// match. A caller sharing kv across processes needs NewTaskStore with a real
// arbiter instead.
func NewInMemoryTaskStoreWithKV(kv interfaces.KVStore, limit int64) TaskStore {
	return NewTaskStore(kv, NewProcessClaimArbiter(), WithInlineArtifactLimit(limit))
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

// taskTenantContext binds tenantID to ctx when the context names no tenant.
//
// THE KV UNDERNEATH ROUTES ON THE CONTEXT, NOT ON THE KEY. Every read and write
// below resolves its backing store from the tenant bound to the CONTEXT (pkg/server
// tenantRoutingKVStore -> go-qrdb tenant.StoreResolver), and a barrier-isolated
// tenant's records live in that tenant's own store, so the tenant segment in the
// key is a label rather than an address. A record therefore has to be addressed
// under its OWN tenant on both sides, or the write and the read pick different
// namespaces and each is correct about a different store.
//
// EVERY METHOD HERE ALREADY KNOWS THE TENANT and until now none of them said so.
// The callers that stamped the context themselves were fine — the task-source
// poller (task_source.go), the dispatcher, the job envelope — and the callers
// that did not were reading an empty namespace and being told, truthfully for
// where they looked, that the tenant had no tasks at all. That is go-quicbaas#918:
// the SDLC label-reconciliation sweep runs on a background context with no
// tenant, so ListTasks scanned the shared store, found none of the project's
// tasks, and concluded every issue claiming live work was an orphan whose record
// had been lost — posting `abandoned` over runs that were alive and going on to
// open their pull requests. The sweep's own "fail-closed re-read" re-read the
// same empty namespace, which is why nothing about it looked like a race. The
// unblock sweep read the same way and could never have unblocked anything.
//
// It is the same defect this codebase has now fixed five times, and the same
// shape of fix: agents in #663, OAuth2 applications in #644, pooled accounts in
// #535, projects in #634, and see fleetTenantContext for the reasoning in full.
// Doing it in the store rather than at each caller is what stops there being a
// sixth: a reader that forgets to stamp is invisible until it destroys something.
//
// A context that already names a tenant is left exactly as it is. Which tenant a
// caller may act as is an admission decision made above this layer, and this
// helper must never be the thing that redirects a write into a tenant the caller
// was not admitted to.
func taskTenantContext(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" || TenantFromContext(ctx) != "" {
		return ctx
	}
	return ContextWithTenant(ctx, tenantID)
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

// marshalTask encodes one task record for storage.
//
// IT ZEROES THE DERIVED FIELDS FIRST. Task.AgentID is the task's LATEST
// ATTEMPT's agent, resolved when a listing is read and never a fact the record
// owns; persisting a copy would leave the next attempt's page showing the
// previous attempt's daemon, with nothing to say which it was. Account.Default
// is handled the same way and for the same reason (see marshalAccount).
//
// AND IT ENCODES THE taskRecord, NOT THE Task: Task.MarshalJSON adds the
// accounting derived on read (see task_representation.go), which is arithmetic
// over this record and would only ever be a stale copy of it here.
func marshalTask(task *Task) ([]byte, error) {
	stored := *task
	stored.AgentID = ""
	data, err := json.Marshal((*taskRecord)(&stored))
	if err != nil {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreMarshal,
			Message:  "failed to marshal task: " + err.Error(),
		}
	}
	return data, nil
}

// writeRecord encodes and stores one task record.
func (s *taskStore) writeRecord(ctx context.Context, key string, task *Task) error {
	data, err := marshalTask(task)
	if err != nil {
		return err
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

// mutate performs the locked read-modify-write behind every transition. The
// lock is held across load, transition and write, so the sequence is atomic
// against other writers in this process; the version bump it applies is what
// lets a writer on another node detect that it raced.
//
// Detection is the whole contract here, and it is the right one for an edit —
// a loser re-reads and re-applies. It is NOT sufficient for a claim, which is
// why ClaimTask has the arbiter decide before its mutator returns a record to
// write. Nothing in this function arbitrates.
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
	ctx = taskTenantContext(ctx, tenantID)
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
	if err := task.Source.Validate(); err != nil {
		return err
	}

	now := time.Now().UnixNano()
	task.CreatedAt = now
	task.UpdatedAt = now
	task.StatusChangedAt = now
	task.Version = 1
	if task.Status == "" {
		task.Status = TaskStatusPending
	}

	// THE STORE DECIDES THE CREATE, not a probe followed by a hopeful write.
	//
	// What this guarantees: the record is written only if the key has never
	// been written, evaluated inside go-qrdb's state machine at this entry's
	// position in the committed log. Every replica decides identically, so of N
	// nodes creating one task ID, exactly one succeeds and the rest are told the
	// task already exists — across the fleet, not merely within one process.
	//
	// What the previous shape guaranteed, despite a comment claiming otherwise:
	// nothing beyond this process. Exists and Put under recordMu is two
	// operations with a window between them, and a mutex on one node is
	// invisible to another. The losing write was UNCONDITIONAL, so it did not
	// merely duplicate — it overwrote a task that had since been claimed,
	// resetting Status to pending and emptying AssignedTo. That erased a
	// DECISION, and the next sweep dispatched a second worker onto running work.
	//
	// recordMu is still held so this store's own concurrent creates serialize
	// locally, which keeps the in-process behaviour unchanged; it is no longer
	// what makes the create exclusive.
	if s.creator == nil {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "creates need a store that can decide them; this one cannot guard a create",
		}
	}

	unlock := s.recordMu.lock(key)
	defer unlock()

	// The source is reserved BEFORE the record is written, by the same guarded
	// create that decides the record. Of N tasks adopting one source, exactly
	// one reserves it; the rest are told which task holds it. The reservation
	// is given back if the record itself cannot be created, so a refused
	// duplicate ID does not leave the source unadoptable.
	//
	// It also precedes the marshal, because taking a source over from a
	// terminal holder records that holder on this task as its predecessor, and
	// the record written here must carry it.
	release, err := s.reserveSource(ctx, task)
	if err != nil {
		return err
	}

	data, err := marshalTask(task)
	if err != nil {
		release()
		return err
	}

	stored, err := s.creator.PutIfAbsent(ctx, key, data)
	if err != nil {
		release()
		return backendError("failed to create task", err)
	}
	if !stored {
		release()
		return &TaskAlreadyExistsError{TaskID: task.ID}
	}
	return nil
}

// reserveSource claims the source index entry for task and returns the
// function that gives it back. A task without a source reserves nothing and
// the returned function does nothing.
//
// A RESERVATION BINDS ONLY WHILE ITS HOLDER CAN STILL WORK THE SOURCE. A task
// that reached a terminal status will never run again — the transition table
// admits nothing out of completed, failed or cancelled — so a task that spent
// its attempt budget would otherwise hold its origin unadoptable for the life
// of the record, and re-labelling the issue would do nothing at all (#652).
// The take-over below is what releases it, and it is deliberately here rather
// than at the terminal write: this is the one moment both parties are known,
// so the successor can record the holder it superseded.
func (s *taskStore) reserveSource(ctx context.Context, task *Task) (func(), error) {
	if task.Source == nil {
		return func() {}, nil
	}
	indexKey, err := taskSourceIndexKey(task.TenantID, task.ProjectID, task.Source)
	if err != nil {
		return nil, err
	}
	if s.releaser == nil {
		return nil, &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "a task source needs a store that can release a reservation; this one cannot guard a delete",
		}
	}

	stored, err := s.creator.PutIfAbsent(ctx, indexKey, []byte(task.ID))
	if err != nil {
		return nil, backendError("failed to reserve task source", err)
	}
	if !stored {
		if err := s.supersedeSource(ctx, indexKey, task); err != nil {
			return nil, err
		}
	}

	return func() {
		// Best effort: the create it was guarding has already failed and that
		// failure is what the caller sees. A reservation that could not be
		// given back is reported through the audit trail, not by masking the
		// original error.
		if _, err := s.releaser.CompareAndDelete(ctx, indexKey, []byte(task.ID)); err != nil {
			s.audit(ctx, AuditActionDelete, task.TenantID, task.ID, backendError("failed to release task source", err))
		}
	}, nil
}

// supersedeSource takes a contended source reservation over from a holder that
// can no longer work it, and records that holder on task as its predecessor.
// It returns a TaskSourceAlreadyAdoptedError, naming the holder and the status
// it is in, when the reservation still binds.
//
// BOTH HALVES ARE GUARDED, the same way projectStore.reserveName clears an
// abandoned name: the delete removes only the entry this caller observed, and
// the re-claim cannot overwrite whatever replaced it. A contender that changed
// the entry underneath is therefore never overwritten — the loop looks again
// and either refuses or takes over what it now sees.
func (s *taskStore) supersedeSource(ctx context.Context, indexKey string, task *Task) error {
	var (
		holderID   string
		status     TaskStatus
		faultClass string
	)
	for attempt := 0; attempt < cas.MaxAttempts; attempt++ {
		var err error
		if holderID, err = s.sourceHolder(ctx, indexKey); err != nil {
			return err
		}
		status = ""
		faultClass = ""
		if holderID != "" {
			if status, faultClass, err = s.sourceHolderState(ctx, task.TenantID, holderID); err != nil {
				return err
			}
			if !taskSourceReleasable(status, faultClass) {
				break
			}
			removed, err := s.releaser.CompareAndDelete(ctx, indexKey, []byte(holderID))
			if err != nil {
				return backendError("failed to release the task source held by "+holderID, err)
			}
			if !removed {
				continue
			}
		}
		claimed, err := s.creator.PutIfAbsent(ctx, indexKey, []byte(task.ID))
		if err != nil {
			return backendError("failed to reserve task source", err)
		}
		if !claimed {
			continue
		}
		if holderID != "" {
			// A predecessor still blocked on an environment fault names the
			// class it was held on; a terminal predecessor names none.
			blockReason := ""
			if status == TaskStatusBlocked {
				blockReason = faultClass
			}
			recordSourcePredecessor(task, holderID, blockReason)
		}
		return nil
	}
	return &TaskSourceAlreadyAdoptedError{
		TenantID:              task.TenantID,
		ProjectID:             task.ProjectID,
		Kind:                  task.Source.Kind,
		Ref:                   task.Source.Ref,
		TaskID:                holderID,
		HolderStatus:          status,
		HolderBlockFaultClass: faultClass,
	}
}

// sourceHolder returns the task ID a source index entry names, empty when the
// entry is gone.
func (s *taskStore) sourceHolder(ctx context.Context, indexKey string) (string, error) {
	holder, err := s.kv.Get(ctx, indexKey)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return "", nil
		}
		return "", backendError("failed to read task source holder", err)
	}
	return string(holder), nil
}

// sourceHolderState reads the status of the task holding a reservation and,
// for a blocked holder, the environment fault class it is held on. An empty
// status means the holder has no record: the reservation outlived the task
// that made it, which is residue a delete could not finish, and it holds
// nothing back. The fault class is what tells a transient environment hold,
// which a re-label supersedes, from a deliberate operator hold, which binds.
func (s *taskStore) sourceHolderState(ctx context.Context, tenantID, holderID string) (TaskStatus, string, error) {
	key, err := taskRecordKey(tenantID, holderID)
	if err != nil {
		// The entry holds a value that is not even a legal key segment, so
		// nothing it names can be loaded.
		return "", "", nil
	}
	holder, err := s.load(ctx, key, tenantID, holderID)
	var missing *TaskNotFoundError
	if errors.As(err, &missing) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return holder.Status, holder.BlockFaultClass, nil
}

// taskSourceReleasable reports whether a reservation held by a task in this
// status has stopped binding. The terminal statuses are the ones the
// transition table admits nothing out of, so a holder in one of them can never
// work its source again; an empty status is a holder with no record at all.
//
// A BLOCKED HOLDER RELEASES ONLY WHEN ITS BLOCK IS A TRANSIENT ENVIRONMENT
// FAULT. A block is otherwise a deliberate operator hold and binds the source:
// re-labelling must not supersede a policy or verification hold. But a block
// that a classified environment fault produced — the pool had no resolver, no
// account, or the run tripped the consecutive-fault bound — is a condition a
// person resolves out of band and then re-arms the only way a forge operator
// can, by re-labelling the issue. blockFaultClass is set only for that case
// (KnownFaultClass is the union of the resolver-fault set and the
// environmental terminal reasons); an operator hold carries none and stays
// held (#739).
func taskSourceReleasable(status TaskStatus, blockFaultClass string) bool {
	switch status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled, "":
		return true
	case TaskStatusBlocked:
		return KnownFaultClass(blockFaultClass)
	default:
		return false
	}
}

// recordSourcePredecessor names, on task, the task it took its source over
// from, and — when that predecessor was still blocked on an environment fault
// — the class it was held on. The metadata map is copied rather than written
// through, because the caller's map may be the one a TaskSource offered the
// spec with and is not this store's to mutate.
func recordSourcePredecessor(task *Task, predecessorID, blockReason string) {
	metadata := make(map[string]string, len(task.Metadata)+2)
	maps.Copy(metadata, task.Metadata)
	metadata[taskSourcePredecessorKey] = predecessorID
	if blockReason != "" {
		metadata[taskSourcePredecessorBlockReasonKey] = blockReason
	}
	task.Metadata = metadata
}

// PredecessorTaskID names the task this one took its source over from. It is
// empty for a task that adopted its source first, and for a task with no
// source at all.
func (t *Task) PredecessorTaskID() string {
	if t.Metadata == nil {
		return ""
	}
	return t.Metadata[taskSourcePredecessorKey]
}

// PredecessorBlockReason names the environment fault class the predecessor was
// held on when this task took its source over. It is empty for a task with no
// predecessor and for one whose predecessor was terminal rather than blocked.
func (t *Task) PredecessorBlockReason() string {
	if t.Metadata == nil {
		return ""
	}
	return t.Metadata[taskSourcePredecessorBlockReasonKey]
}

// releaseSource gives back a deleted task's source index entry, if it still
// names that task. A task without a source holds nothing.
func (s *taskStore) releaseSource(ctx context.Context, task *Task) error {
	if task.Source == nil {
		return nil
	}
	indexKey, err := taskSourceIndexKey(task.TenantID, task.ProjectID, task.Source)
	if err != nil {
		return err
	}
	if s.releaser == nil {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "releasing a task source needs a store that can guard a delete",
		}
	}
	if _, err := s.releaser.CompareAndDelete(ctx, indexKey, []byte(task.ID)); err != nil {
		return backendError("failed to release task source", err)
	}
	return nil
}

// =============================================================================
// GetTask / ListTasks
// =============================================================================

// GetTask retrieves a task by ID within a tenant namespace.
func (s *taskStore) GetTask(ctx context.Context, tenantID, taskID string) (*Task, error) {
	ctx = taskTenantContext(ctx, tenantID)
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
// Records that cannot be decoded are left out; ListTaskRecords names them.
func (s *taskStore) ListTasks(ctx context.Context, tenantID string, filter *TaskFilter) ([]*Task, error) {
	listing, err := s.ListTaskRecords(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return listing.Tasks, nil
}

// ListTaskRecords lists tasks matching the filter within a tenant, in key
// order, and names every record under the tenant it could not decode.
func (s *taskStore) ListTaskRecords(ctx context.Context, tenantID string, filter *TaskFilter) (*TaskListing, error) {
	ctx = taskTenantContext(ctx, tenantID)
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

	listing := &TaskListing{Tasks: make([]*Task, 0)}
	scanErr := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		var task Task
		if err := json.Unmarshal(value, &task); err != nil {
			// One unreadable record must not hide the rest of the tenant's
			// work queue, so the listing carries on past it — but it is
			// NAMED rather than dropped, because a reader deciding from a
			// task's absence cannot tell a record it could not read from
			// one that does not exist.
			listing.Unreadable = append(listing.Unreadable, idFromKey(key, prefix))
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

		listing.Tasks = append(listing.Tasks, &task)

		if filter != nil && filter.Limit > 0 && len(listing.Tasks) >= filter.Limit {
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

	return listing, nil
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
	if filter.ProjectID != "" && task.ProjectID != filter.ProjectID {
		return false
	}
	if len(filter.Statuses) > 0 && !taskStatusIn(task.Status, filter.Statuses) {
		return false
	}
	if filter.AutoSpawn != nil && task.AutoSpawn != *filter.AutoSpawn {
		return false
	}
	// THE JOB BINDING IS THE LATEST ATTEMPT'S, not any attempt's. A job page
	// asking "which task is this" means the task this job is the CURRENT
	// attempt at; matching the whole history instead would answer with a task
	// that has since been re-dispatched somewhere else.
	if filter.JobID != "" && task.LatestAttemptJobID() != filter.JobID {
		return false
	}
	// AgentID is deliberately NOT matched here: it is derived from the
	// attempt's job when a listing is paged (ListTasksPage), so a store that
	// matched it would be matching a field the record never carries. See
	// TaskFilter.AgentID.
	return true
}

// taskStatusIn reports whether status is one of statuses.
func taskStatusIn(status TaskStatus, statuses []TaskStatus) bool {
	for _, candidate := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
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
	ctx = taskTenantContext(ctx, tenantID)
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

	return s.mutate(ctx, task.TenantID, task.ID, func(current *Task, now int64) (*Task, error) {
		if task.Version != current.Version {
			return nil, &TaskVersionConflictError{
				TaskID:          task.ID,
				ExpectedVersion: task.Version,
				ActualVersion:   current.Version,
			}
		}
		if task.Status != current.Status {
			task.StatusChangedAt = now
		}
		return task, nil
	})
}

// =============================================================================
// ClaimTask / CompleteTask / FailTask
// =============================================================================

// ClaimTask claims a task for an agent. The task must be pending, unclaimed and
// have all of its dependencies completed, and the claim must be admitted by the
// store's arbiter — which is what makes the winner single across the fleet
// rather than merely across this process.
func (s *taskStore) ClaimTask(ctx context.Context, tenantID, taskID, agentID string) error {
	ctx = taskTenantContext(ctx, tenantID)
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
	if s.arbiter == nil {
		// Fail closed rather than decide locally. A store wired without an
		// arbiter cannot tell whether it is alone, and claiming on that
		// assumption is the defect this path exists to remove.
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "claims need an arbiter; a store without one cannot decide who won",
		}
	}

	// A winner that dies between taking the mark and writing the record leaves
	// the task pending with its claim already spent, so no contender can take
	// it again. That is WP10's invariant exactly — a claim marker with no live
	// run and no completion — and it is reaped there by run-is-authority, never
	// by a TTL. Recorded here so WP10's implementer finds the case from the
	// code rather than from the plan.
	return s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		if current.Status == TaskStatusClaimed {
			return nil, &TaskAlreadyClaimedError{
				TaskID:       taskID,
				ClaimedBy:    current.AssignedTo,
				RequestingBy: agentID,
			}
		}
		if !taskTransitionAllowed(current.Status, TaskStatusClaimed) {
			return nil, &TaskInvalidStateError{
				TaskID:       taskID,
				CurrentState: current.Status,
				TargetState:  TaskStatusClaimed,
			}
		}
		if err := s.checkDependenciesResolved(ctx, tenantID, current.Dependencies); err != nil {
			return nil, err
		}

		// The arbiter is consulted LAST, after every precondition the record
		// itself can answer. That ordering is what keeps the mark meaningful:
		// it advances on claims the record would have accepted and on nothing
		// else, so a claim refused for being already-claimed or blocked never
		// moves it.
		//
		// The version claimed is the one just read. Every contender reads the
		// same record, so every contender claims the same version, and exactly
		// one of them is admitted for it.
		verdict, err := s.arbiter.Admit(ctx, &TaskClaim{
			TenantID: tenantID,
			TaskID:   taskID,
			AgentID:  agentID,
			Version:  current.Version,
		})
		if err != nil {
			// Undecidable, not lost. The caller must not treat this as
			// contention and must not proceed.
			return nil, err
		}
		if !verdict.Admitted {
			// A loss is a verdict, never an error, and it names the holder so
			// a dispatcher can stand down instead of retrying. Claim loss is
			// deliberately NOT a retryable class — ErrForgeNotReady is this
			// package's only one, and widening that set would turn a permanent
			// refusal into a timeout.
			return nil, &TaskAlreadyClaimedError{
				TaskID:       taskID,
				ClaimedBy:    verdict.HeldBy,
				RequestingBy: agentID,
			}
		}

		if err := transition(current, TaskStatusClaimed, now); err != nil {
			return nil, err
		}
		current.AssignedTo = agentID
		current.ClaimedAt = now
		return current, nil
	})
}

// CompleteTask marks a claimed or in-progress task as completed with a result.
func (s *taskStore) CompleteTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error {
	ctx = taskTenantContext(ctx, tenantID)
	err := s.finish(ctx, tenantID, taskID, TaskStatusCompleted, result)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

// FailTask marks a claimed or in-progress task as failed with a reason.
func (s *taskStore) FailTask(ctx context.Context, tenantID, taskID, reason string) error {
	ctx = taskTenantContext(ctx, tenantID)
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

// finish drives the terminal transition shared by CompleteTask and FailTask.
// The table decides which starting states admit it: completion needs a claimed
// or in-progress task, failure additionally retires a blocked one. Both stamp
// a result and a completion time.
func (s *taskStore) finish(ctx context.Context, tenantID, taskID string, target TaskStatus, result *TaskResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		// blocked -> completed belongs to CloseBlockedTask alone; see the
		// transition table.
		if target == TaskStatusCompleted && current.Status == TaskStatusBlocked {
			return nil, &TaskInvalidStateError{TaskID: taskID, CurrentState: current.Status, TargetState: target}
		}
		if err := transition(current, target, now); err != nil {
			return nil, err
		}
		current.Result = result
		current.CompletedAt = now
		return current, nil
	})
}

// =============================================================================
// BlockTask / ReadmitTask
// =============================================================================

// BlockTask parks a pending, claimed or in-progress task. The kind must be a
// member of the closed enum and the reason must not be empty: a block that
// cannot say why it happened cannot be acted on by the person it waits for.
func (s *taskStore) BlockTask(ctx context.Context, tenantID, taskID string, kind TaskBlockKind, reason string) error {
	ctx = taskTenantContext(ctx, tenantID)
	err := s.block(ctx, tenantID, taskID, kind, reason)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

func (s *taskStore) block(ctx context.Context, tenantID, taskID string, kind TaskBlockKind, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := kind.Validate(); err != nil {
		return err
	}
	if reason == "" {
		return &TaskStoreError{
			Sentinel: ErrTaskStoreInvalidInput,
			Message:  "block reason must not be empty",
		}
	}

	return s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		if err := transition(current, TaskStatusBlocked, now); err != nil {
			return nil, err
		}
		current.BlockKind = kind
		current.BlockedReason = reason
		// An operator hold is not a classified environment fault; clearing the
		// class keeps a re-label from mistaking a deliberate hold for a
		// transient one it may supersede.
		current.BlockFaultClass = ""
		// The park names itself from the same closed vocabulary an attempt's
		// conclusion does, so a reader of the record never has to tell an
		// operator hold from an engine one by inference.
		current.ConclusionReason = blockKindConclusions[kind]
		return current, nil
	})
}

// ReadmitTask returns a blocked task to pending. The block, the assignment
// and the retry hold are cleared together: readmission is a person saying
// "go now", and a task that came back holding a stale claim or a future
// RetryNotBefore would sit in the queue looking dispatchable while nothing
// could take it.
func (s *taskStore) ReadmitTask(ctx context.Context, tenantID, taskID string) error {
	ctx = taskTenantContext(ctx, tenantID)
	err := s.readmit(ctx, tenantID, taskID)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

func (s *taskStore) readmit(ctx context.Context, tenantID, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	return s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		if err := transition(current, TaskStatusPending, now); err != nil {
			return nil, err
		}
		current.BlockKind = ""
		current.BlockedReason = ""
		current.BlockFaultClass = ""
		current.AssignedTo = ""
		current.ClaimedAt = 0
		current.RetryNotBefore = 0
		current.ConclusionReason = TaskConcludedReadmitted

		// READMISSION MUST END THE OLD ATTEMPT'S BINDING, OR THE TASK BECOMES
		// INVISIBLE. The dispatcher treats a task that still names a
		// SpawnedJobID as one with a live attempt and skips it — not as a
		// refusal with a reason, but with a bare `continue`, so the task is
		// neither dispatched nor explained. It sits at pending for ever and
		// nothing in the log says why. Measured on the burn-in node: five
		// readmitted tasks against pending=5 inflight=1 slots=0 dispatched=0
		// with not one refusal line to name them.
		//
		// Readmission runs only from blocked or failed, and neither has an
		// open attempt (taskAttemptOpen), so there is no live job whose
		// conclusion this could steal — the job that held this binding is
		// already finished or gone.
		//
		// recordAttemptHistory rather than a bare clear, because the job id is
		// the only record that the attempt happened: a task blocked by the
		// block verb never reached applyConclusion, so nothing else has
		// written it to the history.
		if current.SpawnedJobID != "" {
			recordAttemptHistory(current, current.SpawnedJobID)
		}

		// READMISSION MUST GRANT AN ALLOWANCE, OR IT ONLY LOOKS LIKE RECOVERY.
		// Moving the task to pending restores its STATE; the dispatcher then
		// asks whether it may spend an attempt, and a task readmitted at
		// 3-of-3 charged is held forever with "attempt budget exhausted" —
		// pending, visible, and never dispatched. Measured on the burn-in node:
		// twelve readmitted tasks sat at pending=12 inflight=0 dispatched=0
		// until this was added.
		//
		// THE HISTORY IS NOT REWRITTEN. Attempts keeps every attempt the task
		// ever made and EnvironmentFaults is untouched — claiming the spent
		// attempts had been environment faults would be a lie the refund
		// arithmetic would then repeat everywhere. Instead the budget is raised
		// to what is already charged PLUS a fresh allowance of the ordinary
		// size, which is exactly what an operator means by "try this again".
		//
		// It is bounded and visible: each readmission is one explicit, audited
		// decision that grows the budget by one allowance, so a task nobody can
		// fix cannot loop — somebody has to keep choosing to readmit it, and
		// the record says how often they did.
		if current.BudgetExhausted() {
			current.AttemptBudget = current.ChargedAttempts() + DefaultAttemptBudget
		}
		return current, nil
	})
}

// CloseBlockedTask completes a blocked task without readmitting it: an
// operator has ruled that no further work is needed. The block is cleared so
// the completed record does not still read as parked, and the conclusion is
// the ordinary `completed`.
func (s *taskStore) CloseBlockedTask(ctx context.Context, tenantID, taskID string, result *TaskResult) error {
	ctx = taskTenantContext(ctx, tenantID)
	err := s.closeBlocked(ctx, tenantID, taskID, result)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return err
}

func (s *taskStore) closeBlocked(ctx context.Context, tenantID, taskID string, result *TaskResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		if current.Status != TaskStatusBlocked {
			return nil, &TaskInvalidStateError{TaskID: taskID, CurrentState: current.Status, TargetState: TaskStatusCompleted}
		}
		if err := transition(current, TaskStatusCompleted, now); err != nil {
			return nil, err
		}
		current.Result = result
		current.CompletedAt = now
		current.BlockKind = ""
		current.BlockedReason = ""
		current.BlockFaultClass = ""
		current.AssignedTo = ""
		current.ClaimedAt = 0
		current.RetryNotBefore = 0
		current.ConclusionReason = TaskConcludedCompleted
		return current, nil
	})
}

// BumpReviewRoundBudget raises a blocked task's ReviewRoundBonus by one,
// bounded by MaxReviewRoundBonus.
func (s *taskStore) BumpReviewRoundBudget(ctx context.Context, tenantID, taskID string) (int, error) {
	ctx = taskTenantContext(ctx, tenantID)
	bonus, err := s.bumpReviewRoundBudget(ctx, tenantID, taskID)
	s.audit(ctx, AuditActionUpdate, tenantID, taskID, err)
	return bonus, err
}

func (s *taskStore) bumpReviewRoundBudget(ctx context.Context, tenantID, taskID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var bonus int
	err := s.mutate(ctx, tenantID, taskID, func(current *Task, now int64) (*Task, error) {
		if current.Status != TaskStatusBlocked {
			return nil, &TaskInvalidStateError{TaskID: taskID, CurrentState: current.Status, TargetState: TaskStatusBlocked}
		}
		if current.ReviewRoundBonus >= MaxReviewRoundBonus {
			return nil, &ReviewRoundBudgetExhaustedError{TaskID: taskID, Max: MaxReviewRoundBonus}
		}
		current.ReviewRoundBonus++
		bonus = current.ReviewRoundBonus
		return current, nil
	})
	if err != nil {
		return 0, err
	}
	return bonus, nil
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
	ctx = taskTenantContext(ctx, tenantID)
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
	ctx = taskTenantContext(ctx, tenantID)
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
	ctx = taskTenantContext(ctx, tenantID)
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
	ctx = taskTenantContext(ctx, tenantID)
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

	// Confirms both that the task exists and that it belongs to this tenant,
	// and yields the source whose reservation goes with the record.
	task, err := s.load(ctx, key, tenantID, taskID)
	if err != nil {
		return err
	}

	// The record goes first. A failure after this point leaves orphaned
	// payloads, an orphaned claim mark or an orphaned source reservation,
	// which waste space; the reverse order would leave a live record pointing
	// at payloads that no longer exist, a live record with no mark — a task
	// claimable twice — or a live record whose source could be adopted again.
	if err := s.kv.Delete(ctx, key); err != nil {
		return backendError("failed to delete task", err)
	}
	if err := s.forgetClaim(ctx, tenantID, taskID); err != nil {
		return err
	}
	if err := s.releaseSource(ctx, task); err != nil {
		return err
	}
	return s.deleteKeysUnder(ctx, prefix)
}

// forgetClaim discards a deleted task's claim mark.
//
// A store with no arbiter wrote no mark, so there is nothing to forget and
// nothing to report. This is not the claim path's nil check in disguise: there
// the absence means a decision cannot be made, here it means no state exists.
func (s *taskStore) forgetClaim(ctx context.Context, tenantID, taskID string) error {
	if s.arbiter == nil {
		return nil
	}
	return s.arbiter.Forget(ctx, tenantID, taskID)
}

// DeleteTenantTasks deletes every task owned by a tenant, together with that
// tenant's artifact payloads.
func (s *taskStore) DeleteTenantTasks(ctx context.Context, tenantID string) error {
	ctx = taskTenantContext(ctx, tenantID)
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
	sources, err := taskSourcePrefix(tenantID)
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
		// The mark follows its record, for the reason DeleteTask gives. A mark
		// left behind by a cascade is debt nobody will ever collect: the task
		// it named is gone, so nothing will ever look at it again.
		if forgetErr := s.forgetClaim(ctx, tenantID, taskID); forgetErr != nil {
			s.audit(ctx, AuditActionDelete, tenantID, taskID, forgetErr)
			return forgetErr
		}
		s.audit(ctx, AuditActionDelete, tenantID, taskID, nil)
	}

	// Sweeping the whole tenant artifact subtree also reclaims payloads whose
	// record was already gone; the same holds for source reservations.
	if err := s.deleteKeysUnder(ctx, artifacts); err != nil {
		s.audit(ctx, AuditActionDelete, tenantID, "", err)
		return err
	}
	if err := s.deleteKeysUnder(ctx, sources); err != nil {
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
