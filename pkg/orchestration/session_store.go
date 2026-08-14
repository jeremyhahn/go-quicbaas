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
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// AgentStore — Interface
// =============================================================================

// AgentStore persists the durable description of an agent session.
//
// The record it holds is not a cache of live process state: it is everything
// the orchestrator needs to address a session after a restart and, for a
// persistent session, to reconstitute the agent process that served it. The
// live process itself is owned by a backend and dies with the daemon; the
// record outlives it.
//
// Every method is tenant-explicit. A session is reachable only through the
// tenant that owns it, so one tenant can neither read nor mutate another's
// records. ListAllSessions is the single deliberate exception: it is the
// boot-time recovery scan, which by definition runs before any tenant context
// exists and must see every tenant's sessions to restore them.
type AgentStore interface {
	// PutSession persists a session record, creating or updating it. The
	// owning tenant is taken from the record.
	PutSession(ctx context.Context, record *SessionRecord) error

	// GetSession retrieves one session record owned by tenantID.
	GetSession(ctx context.Context, tenantID, sessionID string) (*SessionRecord, error)

	// DeleteSession removes one session record owned by tenantID.
	DeleteSession(ctx context.Context, tenantID, sessionID string) error

	// ListSessions returns every session record owned by tenantID.
	ListSessions(ctx context.Context, tenantID string) ([]*SessionRecord, error)

	// ListAllSessions returns every session record across every tenant. It
	// exists for boot-time recovery, which runs before any tenant context is
	// available; request-scoped callers must use ListSessions.
	ListAllSessions(ctx context.Context) ([]*SessionRecord, error)

	// UpdateSessionStatus updates the status of a persisted session and
	// stamps its last-activity time.
	UpdateSessionStatus(ctx context.Context, tenantID, sessionID string, status SessionState) error

	// UpdateSessionActivity stamps the last-activity time of a session.
	UpdateSessionActivity(ctx context.Context, tenantID, sessionID string) error
}

// =============================================================================
// SessionRecord
// =============================================================================

// ResumeMode names the mechanism by which a session's conversation context is
// carried into a replacement agent process.
type ResumeMode string

const (
	// ResumeModeNone means the session has not been resumed. It is the state
	// of a session whose original process is still live.
	ResumeModeNone ResumeMode = ""

	// ResumeModeNative means the agent CLI reconstituted the conversation from
	// its own on-disk session store (claude --resume <id>). Context fidelity is
	// complete: every prior turn, tool call and tool result is restored.
	ResumeModeNative ResumeMode = "native"

	// ResumeModeReplay means a fresh agent process was primed with the prior
	// turns read back from the durable session event log. Fidelity is bounded
	// by ReplayBudget: the oldest turns are dropped first, and the agent is
	// told how many were omitted.
	ResumeModeReplay ResumeMode = "replay"
)

// SessionRecord is the durable description of an agent session.
//
// Spawn-time configuration is stored verbatim so a resumed session runs under
// exactly the parameters the operator asked for. The transcript is NOT stored
// here — it lives in the SessionEventLog, which is the single source of truth
// for conversation content; this record only points at it.
type SessionRecord struct {
	// SessionID is the operator-visible session identifier. It is stable
	// across a resume: a reconstituted process is registered under this same
	// ID so callers never have to learn a new one.
	SessionID string `json:"session_id"`

	// TenantID owns the record and forms the first segment of its key.
	TenantID string `json:"tenant_id"`

	// ProjectID scopes the session to a registered project, if any.
	ProjectID string `json:"project_id,omitempty"`

	// Backend names the registered backend that owns the session.
	Backend string `json:"backend"`

	// Model is the resolved model, empty when the backend chooses.
	Model string `json:"model,omitempty"`

	// --- Spawn configuration, replayed on resume ---------------------------

	// SystemPrompt is the agent's system prompt.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// WorkDir is the agent's working directory.
	WorkDir string `json:"work_dir"`

	// Env is the operator-supplied environment for the agent process. It is
	// the pre-spawn environment: values derived at spawn time (the encoded MCP
	// server list, whose certificate paths are ephemeral) are stripped before
	// the record is written, so a resume re-derives them against freshly
	// minted material instead of reusing a deleted certificate.
	Env map[string]string `json:"env,omitempty"`

	// ExtraArgs are additional CLI arguments for the agent command.
	ExtraArgs []string `json:"extra_args,omitempty"`

	// MCPServers is the requested MCP server configuration. Presets are
	// re-resolved on resume; see Env.
	MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`

	// SandboxMode is the resolved sandbox mode the session runs under.
	SandboxMode SandboxMode `json:"sandbox_mode,omitempty"`

	// TrustLevel is the integrity tier the spawn RESOLVED to against the
	// operator's TrustPolicy — not the tier the caller asked for. It is
	// replayed on resume so a reconstituted process runs at the tier the
	// session was admitted at rather than dropping to the zero value; the
	// resume path re-resolves it, so a floor the operator has raised since
	// applies there too. Absent on records written before the trust axis
	// existed, which decode to TrustLevelDefault and resolve to the floor.
	TrustLevel TrustLevel `json:"trust_level,omitempty"`

	// ContainerImage is the sandbox image, when one applies.
	ContainerImage string `json:"container_image,omitempty"`

	// ExtraMounts are the policy-authorized extra bind mounts the session
	// was spawned with, replayed verbatim on resume. They were validated
	// against the mount policy in force at spawn time.
	ExtraMounts []ContainerMount `json:"extra_mounts,omitempty"`

	// ContainerID is the container the session was launched in, when the
	// orchestrator (rather than the backend) owns the container lifecycle.
	ContainerID string `json:"container_id,omitempty"`

	// --- Pooled provider account ------------------------------------------

	// AccountID is the pooled provider account the session was RESOLVED to at
	// spawn time — not what the caller asked for. A spawn that asked for
	// strategy-based selection (AccountAuto) records the account the strategy
	// actually chose, so a resume replays that same account instead of running
	// selection again against a pool whose usage has since moved. Without it a
	// resumed session can silently continue on a different provider account,
	// which both breaks a static pin and corrupts per-account cost
	// attribution. Empty when the spawn did not use the account pool.
	AccountID string `json:"account_id,omitempty"`

	// AccountStrategy is the name of the AccountSelectionStrategy that chose
	// AccountID ("pinned" when the caller named the account outright). It is
	// retained for audit — it is what lets a cost anomaly be traced back to the
	// policy that produced it — and is deliberately NOT replayed as a
	// selection input on resume.
	AccountStrategy string `json:"account_strategy,omitempty"`

	// AccountAuto records that the spawn asked for STRATEGY-BASED selection
	// rather than naming an account. It is the rotation policy's resolve scope:
	// together with Backend (which yields the provider) and ProjectID it is
	// everything needed to re-run the original candidate query later, which is
	// what lets a rotation pick a replacement from the same pool the spawn drew
	// from instead of inventing a new one.
	//
	// It is deliberately separate from AccountID. A session whose AccountID is
	// set but AccountAuto is false was PINNED by its operator; rotating it
	// would defeat the pin, so it is not rotatable unless AccountAllow names an
	// explicit pool. See CanRotateAccount.
	AccountAuto bool `json:"account_auto,omitempty"`

	// AccountAllow optionally restricts the accounts this session may rotate
	// through — "these three seats, nothing else". It is additive: empty means
	// the resolve scope above defines the pool, which is the behavior of every
	// session spawned before rotation existed.
	AccountAllow []string `json:"account_allow,omitempty"`

	// AccountHistory is the ordered record of which account served which part
	// of the conversation.
	//
	// AccountID is the account in force NOW. Once a session can rotate, that
	// single field can no longer answer "what was this conversation billed to"
	// — the answer is a sequence, and charging one account for another's tokens
	// because the scalar moved is exactly the corruption the resume path was
	// built to avoid. Every segment here is closed with its end time before the
	// next one opens, so a per-account total is a sum over segments rather than
	// a guess. See CostTracker and turn_usage_recorder.go.
	AccountHistory []AccountAssignment `json:"account_history,omitempty"`

	// --- Placement ---------------------------------------------------------

	// HostAffinity records the daemon host a remote session landed on, so a
	// follow-up call re-resolves to the same instance.
	HostAffinity string `json:"host_affinity,omitempty"`

	// BackendLocal is true when the session was spawned on the local
	// in-process backend rather than dispatched to a remote daemon. Only a
	// local session's process dies with this daemon, so only a local session
	// is a resume candidate.
	BackendLocal bool `json:"backend_local,omitempty"`

	// --- Lifecycle ---------------------------------------------------------

	// Persistent is true when the session holds a long-running agent process
	// that carries conversation context across turns. A non-persistent session
	// runs a fresh process per prompt and has no context to restore, so it is
	// never resumed.
	Persistent bool `json:"persistent,omitempty"`

	// Status is the last known session state.
	Status SessionState `json:"status"`

	// ResumeMode records how the session's context was restored the last time
	// it was resumed. Empty for a session that has never been resumed.
	ResumeMode ResumeMode `json:"resume_mode,omitempty"`

	// ResumeError carries the reason the most recent resume attempt failed.
	// It is set together with Status == SessionStateError so an operator sees
	// why a session cannot continue instead of silently talking to a fresh,
	// context-free agent.
	ResumeError string `json:"resume_error,omitempty"`

	// CreatedAt is the spawn time in Unix nanoseconds.
	CreatedAt int64 `json:"created_at"`

	// LastActivity is the last turn's time in Unix nanoseconds.
	LastActivity int64 `json:"last_activity"`
}

// AccountAssignment is one contiguous stretch of a session served by one
// pooled provider account.
type AccountAssignment struct {
	// AccountID is the account that served this stretch.
	AccountID string `json:"account_id"`

	// Strategy names the AccountSelectionStrategy that chose it ("pinned" when
	// the caller named it, "rotation" spellings when a limit moved the session
	// here).
	Strategy string `json:"strategy,omitempty"`

	// From is when the account took over, in Unix nanoseconds.
	From int64 `json:"from"`

	// To is when it stopped serving, in Unix nanoseconds. Zero marks the
	// segment currently in force — there is exactly one such segment on a live
	// record.
	To int64 `json:"to,omitempty"`

	// Reason states why the segment ended (e.g. the redacted provider limit
	// signal). Empty on the open segment.
	Reason string `json:"reason,omitempty"`
}

// CanRotateAccount reports whether the session may be moved onto a different
// pooled account, and why not when it may not.
//
// Three preconditions, all of them about intent rather than mechanism: the
// session must actually be using the pool, it must not be a deliberate pin, and
// it must be persistent-or-one-shot on a local backend (a remote daemon owns
// its own sessions and rotates them itself).
func (r *SessionRecord) CanRotateAccount() (bool, string) {
	switch {
	case r == nil:
		return false, "no session record"
	case r.AccountID == "":
		return false, "the session does not use the pooled provider account subsystem"
	case !r.AccountAuto && len(r.AccountAllow) == 0:
		return false, "the session is pinned to a named account; set account_auto or account_allow to authorize rotation"
	case !r.BackendLocal:
		return false, "the session runs on a remote daemon, which owns its own account rotation"
	case r.Status == SessionStateClosed:
		return false, "the session is closed"
	default:
		return true, ""
	}
}

// OpenAccountSegment closes the account segment currently in force at endedAt
// and opens a new one for accountID. It is the single place AccountID and
// AccountHistory move together, so the scalar and the history can never
// disagree.
//
// A record with no history yet is backfilled with the account it already holds,
// starting at the session's creation time — that is the truth for every session
// spawned before rotation existed, and it keeps a first rotation from silently
// dropping the original account's segment.
func (r *SessionRecord) OpenAccountSegment(accountID, strategy, reason string, at int64) {
	if r == nil || accountID == "" {
		return
	}
	if len(r.AccountHistory) == 0 && r.AccountID != "" {
		from := r.CreatedAt
		if from == 0 {
			from = at
		}
		r.AccountHistory = append(r.AccountHistory, AccountAssignment{
			AccountID: r.AccountID,
			Strategy:  r.AccountStrategy,
			From:      from,
		})
	}
	for i := range r.AccountHistory {
		if r.AccountHistory[i].To == 0 {
			r.AccountHistory[i].To = at
			r.AccountHistory[i].Reason = reason
		}
	}
	r.AccountHistory = append(r.AccountHistory, AccountAssignment{
		AccountID: accountID,
		Strategy:  strategy,
		From:      at,
	})
	r.AccountID = accountID
	r.AccountStrategy = strategy
}

// Resumable reports whether the record describes a session that this daemon
// can reconstitute: a persistent session that ran on this daemon's local
// backend and was not deliberately closed.
//
// A remote session is excluded because its process lives on another daemon and
// may well still be running — that session is re-addressed, not resumed.
func (r *SessionRecord) Resumable() bool {
	if r == nil || !r.Persistent || !r.BackendLocal {
		return false
	}
	return r.Status != SessionStateClosed
}

// Clone returns a deep copy of the record. Callers hand records to the store
// and keep using them, so the store never shares mutable maps or slices with
// its callers.
func (r *SessionRecord) Clone() *SessionRecord {
	if r == nil {
		return nil
	}
	out := *r
	if r.Env != nil {
		out.Env = make(map[string]string, len(r.Env))
		for k, v := range r.Env {
			out.Env[k] = v
		}
	}
	if r.ExtraArgs != nil {
		out.ExtraArgs = append([]string(nil), r.ExtraArgs...)
	}
	if r.MCPServers != nil {
		out.MCPServers = append([]MCPServerConfig(nil), r.MCPServers...)
	}
	if r.ExtraMounts != nil {
		out.ExtraMounts = append([]ContainerMount(nil), r.ExtraMounts...)
	}
	if r.AccountAllow != nil {
		out.AccountAllow = append([]string(nil), r.AccountAllow...)
	}
	if r.AccountHistory != nil {
		out.AccountHistory = append([]AccountAssignment(nil), r.AccountHistory...)
	}
	return &out
}

// =============================================================================
// Key derivation
// =============================================================================

// normalizeStoreTenant substitutes the reserved system tenant for an unset
// one.
//
// The tenant is the first segment of every record key, so an empty value would
// file the record under a malformed key and collapse distinct callers into one
// scope. Resolving it here — to the same identifier StorageTenant and the REST,
// gRPC and MCP transports already substitute — means an untenanted caller and
// an untenanted request address the same records instead of two disjoint sets.
func normalizeStoreTenant(tenantID string) string {
	if tenantID == "" {
		return SystemTenantID
	}
	return tenantID
}

// sessionRecordKey returns the key holding one session record:
// orch/session/{tenantID}/{sessionID}.
func sessionRecordKey(tenantID, sessionID string) (string, error) {
	key, err := entityKey(sessionKeyspace, normalizeStoreTenant(tenantID), "session_id", sessionID)
	if err != nil {
		return "", sessionKeyError(err)
	}
	return key, nil
}

// sessionKeyError restates a key-layout violation in the agent store's own
// error vocabulary while preserving the layout error for errors.As.
func sessionKeyError(err error) error {
	var segment *KeySegmentError
	if !errors.As(err, &segment) {
		return err
	}
	return &AgentStoreError{
		Sentinel: ErrAgentStoreInvalidInput,
		Message:  segment.Field + ": " + segment.Message,
		Cause:    err,
	}
}

// =============================================================================
// sessionStore — the single KV-backed AgentStore
// =============================================================================

// sessionStore is the only AgentStore implementation. It keeps no copy of the
// data it serves: every method reads from, and writes to, the injected
// interfaces.KVStore. Durability, replication and encryption at rest are
// therefore properties of the injected store alone — an in-memory KV makes this
// a fast test double, and the server's qrdb-backed KV makes the very same code
// durable, replicated and barrier-encrypted.
type sessionStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// recordMu serializes the read-modify-write sequence on one session
	// record so a status update and an activity stamp cannot clobber each
	// other. Writes to different sessions proceed in parallel.
	recordMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ AgentStore = (*sessionStore)(nil)

// NewAgentStore returns an AgentStore backed by kv.
//
// The backing store decides the storage semantics: pass kvstore.NewMemory() for
// an in-process store, or the server's qrdb-backed KV for a durable, replicated
// one. There is no second implementation to keep in sync.
func NewAgentStore(kv interfaces.KVStore, opts ...StoreOption) AgentStore {
	cfg := newStoreConfig(opts...)
	return &sessionStore{kv: kv, auditor: cfg.auditor}
}

// NewInMemoryAgentStore returns a memory-backed binding of the store returned
// by NewAgentStore — not a second implementation. It is equivalent to
// NewAgentStore(kvstore.NewMemory()) and exists for tests and embedded use that
// need no durability.
func NewInMemoryAgentStore() AgentStore {
	return NewAgentStore(kvstore.NewMemory())
}

// ready reports whether the store was constructed with a backing KV.
func (s *sessionStore) ready() error {
	if s.kv == nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreRequired,
			Message:  "kv store must not be nil",
		}
	}
	return nil
}

// =============================================================================
// PutSession
// =============================================================================

// PutSession creates or replaces a session record.
func (s *sessionStore) PutSession(ctx context.Context, record *SessionRecord) error {
	var tenantID, sessionID string
	if record != nil {
		tenantID, sessionID = record.TenantID, record.SessionID
	}
	err := s.put(ctx, record)
	s.audit(ctx, AuditActionCreate, tenantID, sessionID, err)
	return err
}

func (s *sessionStore) put(ctx context.Context, record *SessionRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	if record == nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreInvalidInput,
			Message:  "session record must not be nil",
		}
	}

	key, err := sessionRecordKey(record.TenantID, record.SessionID)
	if err != nil {
		return err
	}

	// The stored body always names the tenant its key files it under, so the
	// ownership check on read is meaningful even for a caller that supplied no
	// tenant. The caller's record is left untouched.
	stored := record.Clone()
	stored.TenantID = normalizeStoreTenant(record.TenantID)

	data, err := json.Marshal(stored)
	if err != nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreMarshal,
			Message:  "failed to marshal session record: " + err.Error(),
			Cause:    err,
		}
	}

	unlock := s.recordMu.lock(key)
	defer unlock()

	if err := s.kv.Put(ctx, key, data); err != nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreUnavailable,
			Message:  "failed to write session record: " + err.Error(),
			Cause:    err,
		}
	}
	return nil
}

// =============================================================================
// GetSession / ListSessions / ListAllSessions
// =============================================================================

// GetSession reads one session record owned by tenantID.
func (s *sessionStore) GetSession(ctx context.Context, tenantID, sessionID string) (*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	key, err := sessionRecordKey(tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.load(ctx, key, tenantID, sessionID)
}

// load reads and decodes one session record.
func (s *sessionStore) load(ctx context.Context, key, tenantID, sessionID string) (*SessionRecord, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &AgentStoreError{
				Sentinel: ErrAgentStoreNotFound,
				Message:  "session not found: " + sessionID,
			}
		}
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreUnavailable,
			Message:  "failed to read session record: " + err.Error(),
			Cause:    err,
		}
	}
	if len(data) == 0 {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreNotFound,
			Message:  "session not found: " + sessionID,
		}
	}

	record, decodeErr := decodeSessionRecord(data)
	if decodeErr != nil {
		return nil, decodeErr
	}

	// Defence in depth. The key already scopes the read to tenantID, so a
	// record whose body names a different tenant means the key layout was
	// corrupted or bypassed. Report it as absent rather than handing another
	// tenant's session to this caller.
	if record.TenantID != normalizeStoreTenant(tenantID) {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreNotFound,
			Message:  "session not found: " + sessionID,
		}
	}
	return record, nil
}

// decodeSessionRecord decodes one stored record.
func decodeSessionRecord(data []byte) (*SessionRecord, error) {
	var record SessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreMarshal,
			Message:  "failed to unmarshal session record: " + err.Error(),
			Cause:    err,
		}
	}
	return &record, nil
}

// ListSessions returns every session owned by tenantID, oldest first.
func (s *sessionStore) ListSessions(ctx context.Context, tenantID string) ([]*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}

	// The scan is bounded to this tenant's subtree, so no other tenant's
	// records are reachable from here.
	tenantID = normalizeStoreTenant(tenantID)
	prefix, err := tenantPrefix(sessionKeyspace, tenantID)
	if err != nil {
		return nil, sessionKeyError(err)
	}
	return s.scan(ctx, prefix, tenantID)
}

// ListAllSessions returns every session across every tenant, oldest first. It
// is the boot-time recovery scan; request-scoped callers must use ListSessions.
func (s *sessionStore) ListAllSessions(ctx context.Context) ([]*SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.scan(ctx, sessionKeyspace, "")
}

// scan reads every record under prefix. When wantTenant is non-empty, a record
// whose body names a different owner is skipped as defence in depth against a
// corrupted key.
func (s *sessionStore) scan(ctx context.Context, prefix, wantTenant string) ([]*SessionRecord, error) {
	var (
		records   []*SessionRecord
		decodeErr error
	)
	scanErr := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		if err := ctx.Err(); err != nil {
			decodeErr = err
			return err
		}
		record, err := decodeSessionRecord(value)
		if err != nil {
			decodeErr = err
			return err
		}
		if wantTenant != "" && record.TenantID != wantTenant {
			return nil
		}
		// A cross-tenant scan derives the owner from the key so a record with
		// an unset body tenant is still attributable.
		if record.TenantID == "" {
			record.TenantID = tenantFromSessionKey(key)
		}
		records = append(records, record)
		return nil
	})
	if decodeErr != nil {
		return nil, decodeErr
	}
	if scanErr != nil {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreUnavailable,
			Message:  "failed to list session records: " + scanErr.Error(),
			Cause:    scanErr,
		}
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt != records[j].CreatedAt {
			return records[i].CreatedAt < records[j].CreatedAt
		}
		return records[i].SessionID < records[j].SessionID
	})
	return records, nil
}

// tenantFromSessionKey extracts the tenant segment of a session record key.
func tenantFromSessionKey(key string) string {
	rest := strings.TrimPrefix(key, sessionKeyspace)
	if idx := strings.Index(rest, keySeparator); idx >= 0 {
		return rest[:idx]
	}
	return ""
}

// =============================================================================
// DeleteSession
// =============================================================================

// DeleteSession removes one session record owned by tenantID.
func (s *sessionStore) DeleteSession(ctx context.Context, tenantID, sessionID string) error {
	err := s.remove(ctx, tenantID, sessionID)
	s.audit(ctx, AuditActionDelete, tenantID, sessionID, err)
	return err
}

func (s *sessionStore) remove(ctx context.Context, tenantID, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	key, err := sessionRecordKey(tenantID, sessionID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(key)
	defer unlock()

	if _, err := s.load(ctx, key, tenantID, sessionID); err != nil {
		return err
	}
	if err := s.kv.Delete(ctx, key); err != nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreUnavailable,
			Message:  "failed to delete session record: " + err.Error(),
			Cause:    err,
		}
	}
	return nil
}

// =============================================================================
// Updates
// =============================================================================

// UpdateSessionStatus sets the session's status and stamps its activity time.
func (s *sessionStore) UpdateSessionStatus(ctx context.Context, tenantID, sessionID string, status SessionState) error {
	err := s.mutate(ctx, tenantID, sessionID, func(record *SessionRecord) {
		record.Status = status
		record.LastActivity = time.Now().UnixNano()
	})
	s.audit(ctx, AuditActionUpdate, tenantID, sessionID, err)
	return err
}

// UpdateSessionActivity stamps the session's activity time.
func (s *sessionStore) UpdateSessionActivity(ctx context.Context, tenantID, sessionID string) error {
	err := s.mutate(ctx, tenantID, sessionID, func(record *SessionRecord) {
		record.LastActivity = time.Now().UnixNano()
	})
	s.audit(ctx, AuditActionUpdate, tenantID, sessionID, err)
	return err
}

// mutate applies apply to the stored record under the record's own lock, so the
// read-modify-write sequence is atomic against other writers in this process.
func (s *sessionStore) mutate(ctx context.Context, tenantID, sessionID string, apply func(*SessionRecord)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	key, err := sessionRecordKey(tenantID, sessionID)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(key)
	defer unlock()

	record, err := s.load(ctx, key, tenantID, sessionID)
	if err != nil {
		return err
	}
	apply(record)

	data, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreMarshal,
			Message:  "failed to marshal session record: " + marshalErr.Error(),
			Cause:    marshalErr,
		}
	}
	if putErr := s.kv.Put(ctx, key, data); putErr != nil {
		return &AgentStoreError{
			Sentinel: ErrAgentStoreUnavailable,
			Message:  "failed to write session record: " + putErr.Error(),
			Cause:    putErr,
		}
	}
	return nil
}

// =============================================================================
// Audit
// =============================================================================

// audit emits exactly one store event per mutating call. A failed attempt is
// recorded with Success false, which is the signal an auditor needs to show
// that a change was attempted and rejected.
func (s *sessionStore) audit(ctx context.Context, action, tenantID, sessionID string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   tenantID,
		Resource:   AuditResourceSession,
		ResourceID: sessionID,
		Action:     action,
		Success:    err == nil,
	})
}
