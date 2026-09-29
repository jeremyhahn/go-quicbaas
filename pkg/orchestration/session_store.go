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

	// ConcludeSession records the session's TERMINAL state together with the
	// reason it ended, as one atomic mutation.
	//
	// It is separate from UpdateSessionStatus because the ending is the one
	// transition that has to carry an attribution: a record that says only
	// "closed" leaves an operator to infer from the timestamps whether the
	// agent exited, an operator closed it, or the node restarted underneath
	// it — which is exactly the inference that turned 37 stranded records into
	// "37 active sessions". Writing the status and the reason in two calls
	// would leave a window in which the record is closed and unattributed.
	ConcludeSession(ctx context.Context, tenantID, sessionID string, reason SessionConclusionReason) error

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

	// JobID names the orchestration job the session works for, if any. It is
	// how a session detail finds its job and a job detail finds its sessions.
	JobID string `json:"job_id,omitempty"`

	// Backend names the registered backend that owns the session.
	Backend string `json:"backend"`

	// Model is the resolved model, empty when the backend chooses.
	Model string `json:"model,omitempty"`

	// --- Spawn configuration, replayed on resume ---------------------------

	// SystemPrompt is the prompt the spawn resolved: the caller's, the role's
	// charter or the Agent's own. It does not include PromptAppend.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// PromptAppend is the run-time material appended after SystemPrompt. The
	// two are kept apart so the audit shows which content ran, and a resume
	// composes exactly the same system prompt again.
	PromptAppend string `json:"prompt_append,omitempty"`

	// ResolvedAgentID is the tenant Agent the session runs as, empty for a
	// session spawned on a raw backend. Kept so a restored or resumed session
	// still attributes its turns to that Agent's spend.
	ResolvedAgentID string `json:"resolved_agent_id,omitempty"`

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

	// AccountGeneration identifies which copy of AccountID's credential the
	// session currently holds (AccountCredential.Generation), captured at the
	// instant the material was produced for it.
	//
	// It is the session half of the freshness question. Account.CredentialGeneration
	// is the pool half; when the two disagree the pool has replaced the
	// material under a live session, whose next prompt would die against a
	// provider that no longer honours the copy it is still presenting
	// (go-quicbaas#997). Kept in step with the open segment of AccountHistory by
	// OpenAccountSegment, the same way AccountID is.
	//
	// Zero means unknown, and the freshness check falls back to timestamps
	// rather than guessing — see SessionAccountFreshness.
	AccountGeneration int64 `json:"account_generation,omitempty"`

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

	// CredentialLeaseIDs are the DELIVERY GRANTS taken out over this session's
	// pooled provider credential — one per hand-over, so a session that has
	// rotated carries the grant for every account that ever served it.
	//
	// THEY ARE ON THE DURABLE RECORD BECAUSE THE REAPER IS. A session ends by
	// many routes, and the one that matters most for a credential is the route
	// where nothing is left in memory: the node died, the job failed, the
	// daemon left the fleet. session_reconcile.go concludes such a record from
	// the store alone, so the grants have to be readable from the store alone —
	// a lease ID held only in the live map would be lost in exactly the case
	// where revoking it matters most.
	//
	// Empty for a session that used no pooled account, and for one spawned
	// before delivery grants existed; both conclude with nothing to withdraw.
	CredentialLeaseIDs []string `json:"credential_lease_ids,omitempty"`

	// ReservationIDs are the session-slot reservations (account_balancing.go)
	// this session's spawn, resumes and rotations were resolved under. A
	// reservation named here is SATISFIED: the reservation table stops
	// counting it, because this record now counts the session itself. The
	// correlation is by ID and nothing else, so two spawns on one account and
	// one job can never satisfy each other's reservations.
	ReservationIDs []string `json:"reservation_ids,omitempty"`

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

	// ConclusionReason names why the session ended. It is set together with
	// Status == SessionStateClosed and is never cleared: the record is the
	// tombstone, so the attribution has to outlive the process that made it.
	// Empty on a session that has not ended, and on records written before
	// conclusions were attributed.
	ConclusionReason SessionConclusionReason `json:"conclusion_reason,omitempty"`

	// CreatedAt is the spawn time in Unix nanoseconds.
	//
	// STAMPED BY THE STORE WHEN A CALLER SUPPLIES NONE, so no listing can
	// report a session that exists as having been created at the epoch. A
	// backend that reports no spawn time is the ordinary reason for that, and
	// the write is the last moment at which the truth ("this record is being
	// created now") is still available.
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

	// Generation identifies which copy of the account's credential this
	// segment materialized (AccountCredential.Generation). It is per-segment
	// rather than per-record because a rotation takes a FRESH copy — of a
	// different account, with its own generation — and the freshness question
	// is always asked of the segment currently in force.
	//
	// Zero means unknown: a segment opened before generations existed, or one
	// whose account still holds an envelope that predates stamping.
	Generation int64 `json:"generation,omitempty"`
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

// AddCredentialLease records a delivery grant on the record, ignoring an empty
// ID and never recording the same grant twice.
//
// Append-only, deliberately: a superseded grant is REVOKED, not forgotten. The
// record has to keep naming it so a conclusion that retries — or one that runs
// after a restart, against a record it did not write — withdraws every grant
// the session ever held rather than only the last one.
func (r *SessionRecord) AddCredentialLease(leaseID string) {
	if r == nil || leaseID == "" {
		return
	}
	for _, existing := range r.CredentialLeaseIDs {
		if existing == leaseID {
			return
		}
	}
	r.CredentialLeaseIDs = append(r.CredentialLeaseIDs, leaseID)
}

// AddReservation records a session-slot reservation this session was resolved
// under. Empty and repeated IDs are ignored.
func (r *SessionRecord) AddReservation(reservationID string) {
	if r == nil || reservationID == "" {
		return
	}
	for _, existing := range r.ReservationIDs {
		if existing == reservationID {
			return
		}
	}
	r.ReservationIDs = append(r.ReservationIDs, reservationID)
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
//
// generation is the credential copy the NEW segment materialized
// (AccountCredential.Generation), zero when the rotation could not learn one.
// It moves with AccountID for the same reason the scalar does: a session that
// has rotated is holding the replacement account's material, and comparing it
// against the account it left would report every rotated session as stale
// forever.
func (r *SessionRecord) OpenAccountSegment(accountID, strategy, reason string, at, generation int64) {
	if r == nil || accountID == "" {
		return
	}
	if len(r.AccountHistory) == 0 && r.AccountID != "" {
		from := r.CreatedAt
		if from == 0 {
			from = at
		}
		r.AccountHistory = append(r.AccountHistory, AccountAssignment{
			AccountID:  r.AccountID,
			Strategy:   r.AccountStrategy,
			From:       from,
			Generation: r.AccountGeneration,
		})
	}
	for i := range r.AccountHistory {
		if r.AccountHistory[i].To == 0 {
			r.AccountHistory[i].To = at
			r.AccountHistory[i].Reason = reason
		}
	}
	r.AccountHistory = append(r.AccountHistory, AccountAssignment{
		AccountID:  accountID,
		Strategy:   strategy,
		From:       at,
		Generation: generation,
	})
	r.AccountID = accountID
	r.AccountStrategy = strategy
	r.AccountGeneration = generation
}

// SetAccountGeneration records which copy of the account currently in force
// the session now holds, on both the scalar and the open account segment.
//
// It exists for the one event that changes the answer WITHOUT changing the
// account: a resume. Resuming re-materializes the same account's credential, so
// the restored process presents whatever the pool holds at that moment, not
// what the record was written with. Left unrecorded, the record would keep
// naming a superseded copy and the next freshness check would roll a session
// that had just been restored holding perfectly current material
// (go-quicbaas#997).
//
// A generation that does not advance the record is ignored: the caller either
// learned nothing (zero) or is holding an older copy than the record already
// names, and overwriting a known generation with either would lose the only
// evidence the check has.
func (r *SessionRecord) SetAccountGeneration(generation int64) {
	if r == nil || generation <= r.AccountGeneration {
		return
	}
	r.AccountGeneration = generation
	for i := range r.AccountHistory {
		if r.AccountHistory[i].To == 0 {
			r.AccountHistory[i].Generation = generation
		}
	}
}

// Resumable reports whether the record describes a session that this daemon
// can reconstitute: a persistent session that ran on this daemon's local
// backend and was not deliberately closed.
//
// A remote session is excluded because its process lives on another daemon and
// may well still be running — that session is re-addressed, not resumed.
//
// AN ERRORED SESSION IS NOT RESUMABLE EITHER, and excluding it is the whole
// point of having recorded the error. recordResumeFailure deliberately writes
// SessionStateError and the ResumeError that explains it; treating that record
// as resumable on the next boot rewrote the status to RESUMABLE, so the
// operator-visible failure disappeared and the same session was offered for
// resume again -- looping on the identical error, with nothing left on the
// record to say why. A session that failed to come back keeps saying so until
// somebody acts on it.
func (r *SessionRecord) Resumable() bool {
	if r == nil || !r.Persistent || !r.BackendLocal {
		return false
	}
	return r.Status != SessionStateClosed && r.Status != SessionStateError
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
// guardedSwapper is the compare-and-set capability a terminal transition needs
// from its backing store, discovered by type assertion like guardedCreator and
// guardedDeleter beside it (task_store.go).
//
// IT IS THE ONLY THING THAT MAKES A CONCLUSION SINGULAR ACROSS DAEMONS. The
// reconciliation sweep runs on every node, so two of them — or a sweep and an
// operator's close — can read the same open record and both decide to end it.
// A local mutex cannot arbitrate that, and the guard inside ConcludeSession
// only helps the caller that happens to READ after the other one WROTE. The
// swap is what makes the read and the write one step.
type guardedSwapper interface {
	CompareAndSwap(ctx context.Context, key string, expected, next []byte) (swapped bool, err error)
}

// maxConclusionSwapAttempts bounds the compare-and-set retry. Each loss means
// another writer committed, and the re-read either finds the record already
// concluded (done) or finds a non-terminal change to swap over. A handful of
// attempts is far more than a fleet can contend on one session; the bound
// exists so a pathological store cannot spin here for ever.
const maxConclusionSwapAttempts = 5

type sessionStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor

	// swapper is the store's guarded-swap capability, asserted once at
	// construction. Nil when the backing store cannot decide one — which in
	// this tree means an in-process store (pkg/kvstore.Store is a map behind a
	// mutex), and an in-process store is not shared with another daemon, so
	// the record lock below IS the whole arbitration there. A store that two
	// daemons can both reach is the tenant-routed qrdb one, and that one has
	// the verb.
	swapper guardedSwapper

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
	store := &sessionStore{kv: kv, auditor: cfg.auditor}
	if swapper, ok := kv.(guardedSwapper); ok {
		store.swapper = swapper
	}
	return store
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
	// THE SPAWN TIME IS NEVER ZERO ON A RECORD THAT EXISTS. Backends disagree
	// about whether they report one at all, and a record written without it
	// listed as created at the epoch for the rest of its life — the caller's
	// own record is left untouched, so this is the stored truth rather than a
	// mutation of the caller's value.
	if stored.CreatedAt == 0 {
		stored.CreatedAt = time.Now().UnixNano()
	}

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

// ConcludeSession records the terminal state and the reason together.
//
// Idempotent by construction: a record that is already closed keeps the reason
// it ended with. A second conclusion cannot overwrite the first, so a late
// nudge — an exit observer arriving after an operator's close — cannot
// re-attribute a deliberate ending to a crash.
func (s *sessionStore) ConcludeSession(
	ctx context.Context, tenantID, sessionID string, reason SessionConclusionReason) error {
	var err error
	if s.swapper != nil {
		err = s.concludeGuarded(ctx, tenantID, sessionID, reason)
	} else {
		// See the note on sessionStore.swapper: a store with no guarded swap is
		// an in-process one, which no second daemon can reach, so the record
		// lock inside mutate is the whole arbitration.
		err = s.mutate(ctx, tenantID, sessionID, func(record *SessionRecord) {
			if concludedRecord(record) {
				return
			}
			applySessionConclusion(record, reason)
		})
	}
	s.audit(ctx, AuditActionUpdate, tenantID, sessionID, err)
	return err
}

// concludedRecord reports whether a record already carries a complete ending.
func concludedRecord(record *SessionRecord) bool {
	return record.Status == SessionStateClosed && record.ConclusionReason != ""
}

// applySessionConclusion writes the terminal state and its attribution together.
func applySessionConclusion(record *SessionRecord, reason SessionConclusionReason) {
	record.Status = SessionStateClosed
	record.ConclusionReason = reason
	record.LastActivity = time.Now().UnixNano()
}

// concludeGuarded commits the terminal transition with a compare-and-set, so
// the read that decides and the write that commits are one step on the store's
// own plane.
//
// THE FIRST ENDING WINS, AND ACROSS DAEMONS RATHER THAN ONLY WITHIN ONE. The
// previous commit was a plain Put behind a process-local mutex, so two daemons
// that both read an open record — a sweep and an operator's close, which is the
// ordinary contention now that the sweep runs on every node — both wrote, and
// the later one replaced the first attribution. An operator's deliberate close
// came back reported as `job_concluded`, and the tombstone an auditor reads
// named the wrong cause.
//
// A losing swap is not a failure: it means somebody else committed, so the loop
// re-reads. Either that writer concluded the session, in which case this call
// has nothing left to do and says so, or it wrote something else and this
// attempt swaps over the value it actually observed.
func (s *sessionStore) concludeGuarded(
	ctx context.Context, tenantID, sessionID string, reason SessionConclusionReason) error {
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

	for attempt := 0; attempt < maxConclusionSwapAttempts; attempt++ {
		current, readErr := s.loadRaw(ctx, key, tenantID, sessionID)
		if readErr != nil {
			return readErr
		}
		if concludedRecord(current.record) {
			// Somebody's ending is already on the record, complete with its
			// attribution. Rewriting it is the exact thing this method exists
			// to refuse.
			return nil
		}

		next := current.record.Clone()
		applySessionConclusion(next, reason)
		encoded, marshalErr := json.Marshal(next)
		if marshalErr != nil {
			return &AgentStoreError{
				Sentinel: ErrAgentStoreMarshal,
				Message:  "failed to marshal session record: " + marshalErr.Error(),
				Cause:    marshalErr,
			}
		}

		swapped, swapErr := s.swapper.CompareAndSwap(ctx, key, current.raw, encoded)
		if swapErr != nil {
			return &AgentStoreError{
				Sentinel: ErrAgentStoreUnavailable,
				Message:  "failed to write session record: " + swapErr.Error(),
				Cause:    swapErr,
			}
		}
		if swapped {
			return nil
		}
	}

	return &AgentStoreError{
		Sentinel: ErrAgentStoreUnavailable,
		Message: "session " + sessionID + " could not be concluded: the record changed under " +
			"every attempt to commit the ending",
	}
}

// rawSessionRecord is a decoded record together with the exact bytes it was
// decoded from, which is what a compare-and-set has to compare against.
type rawSessionRecord struct {
	record *SessionRecord
	raw    []byte
}

// loadRaw is load, keeping the encoded form. Re-encoding the decoded record
// would NOT do: a byte-for-byte match is what the swap is guarding on, and a
// round trip through the struct normalises field order and drops anything a
// newer writer added that this build does not know about.
func (s *sessionStore) loadRaw(
	ctx context.Context, key, tenantID, sessionID string) (*rawSessionRecord, error) {
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
	if record.TenantID != normalizeStoreTenant(tenantID) {
		return nil, &AgentStoreError{
			Sentinel: ErrAgentStoreNotFound,
			Message:  "session not found: " + sessionID,
		}
	}
	return &rawSessionRecord{record: record, raw: data}, nil
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
