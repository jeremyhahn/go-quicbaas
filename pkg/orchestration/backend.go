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

// Package orchestration provides the Agent Backend interface and implementations for
// orchestrating AI agent backends. It supports ACP-compatible agents (Claude
// Code, Codex) via JSON-RPC over stdio, and stateless backends like Ollama.
package orchestration

import (
	"context"
	"sort"
)

// AgentBackend defines the interface for any agent backend.
// Implementations include ACP-compatible agents (Claude Code, Codex) and Ollama.
type AgentBackend interface {
	// Spawn creates a persistent session. Returns an error if the backend
	// does not support sessions.
	Spawn(ctx context.Context, req *SpawnRequest) (*Session, error)

	// Prompt sends a prompt to the backend. If sessionID is provided, uses
	// an existing session. If empty and the backend supports sessions, creates
	// an ephemeral one. If the backend is stateless, executes directly.
	Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error)

	// Status returns session health information. Returns an error if the
	// backend does not support sessions.
	Status(ctx context.Context, sessionID string) (*SessionStatus, error)

	// Close terminates a session.
	Close(ctx context.Context, sessionID string) error

	// Capabilities reports what this backend supports.
	Capabilities() BackendCapabilities

	// Type returns the backend type identifier (e.g., "acp", "ollama").
	Type() string

	// ToolGuidance returns backend-specific instructions for tool usage
	// that should be appended to worker prompts. Returns empty string if
	// no special guidance is needed. This allows each backend to communicate
	// its tool schema requirements without polluting the generic prompt.
	ToolGuidance() string
}

// BackendKind classifies a backend as an agent (ACP-compatible, persistent
// sessions, tool use, file access), a stateless local model server, or an
// API-based frontier-model inference provider.
type BackendKind string

const (
	// BackendKindAgent indicates an ACP-compatible agent backend that supports
	// persistent sessions, tool use, and file access (e.g., claude, codex, opencode).
	BackendKindAgent BackendKind = "agent"

	// BackendKindModelServer indicates a stateless local model server that only
	// supports prompt/response operations (e.g., ollama).
	BackendKindModelServer BackendKind = "model_server"

	// BackendKindAPIInference indicates a stateless HTTP-based frontier-model
	// inference provider (e.g., anthropic, openai, xai). These backends are
	// constructed from pkg/orchestration/inference.APIInferenceBackend
	// implementations and adapted into the AgentBackend interface so the
	// orchestrator can route to them uniformly.
	BackendKindAPIInference BackendKind = "api_inference"
)

// BackendCapabilities describes what a backend supports.
type BackendCapabilities struct {
	Kind               BackendKind `json:"kind"`
	PersistentSessions bool        `json:"persistent_sessions"`
	ToolUse            bool        `json:"tool_use"`
	FileAccess         bool        `json:"file_access"`
	Streaming          bool        `json:"streaming"`
	ACP                bool        `json:"acp"`
	TeamManagement     bool        `json:"team_management"`
}

// ModelLister is an optional interface that backends may implement to report
// their available models. Ollama implements this by querying /api/tags; ACP
// backends return a curated static list.
type ModelLister interface {
	// ListModels returns the names of available models.
	// Ollama backends enumerate locally-pulled models; ACP backends return their
	// supported model catalog. Returns an empty slice (not an error) when the
	// backend is reachable but has no models installed.
	ListModels(ctx context.Context) ([]string, error)
}

// SpawnRequest contains parameters for spawning a new agent session.
type SpawnRequest struct {
	ProjectID    string            `json:"project_id"`
	WorkDir      string            `json:"work_dir"`
	Model        string            `json:"model,omitempty"`
	MCPServers   []MCPServerConfig `json:"mcp_servers,omitempty"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	ExtraArgs    []string          `json:"extra_args,omitempty"` // Additional CLI args for the agent command

	// Workspace names the workspace a remote host provisions for the
	// session. The orchestrator provisions an in-process spawn's workspace
	// itself and hands the backend WorkDir; a remote backend forwards this to
	// the agent that executes the session, which provisions it under its own
	// projects root and reports the directory as the session's WorkDir.
	Workspace *SessionWorkspace `json:"workspace,omitempty"`

	// Persistent requests a persistent session where the agent maintains
	// conversation context across multiple prompts. When false, each prompt
	// spawns a fresh one-shot process. Configured per-agent in the workflow
	// YAML via the "persistent_sessions" capability.
	Persistent bool `json:"persistent,omitempty"`

	// Container sandbox fields.
	//
	// SandboxMode is the authoritative sandbox selector for new callers. It
	// takes precedence over Sandboxed when non-Default. Sandboxed is retained
	// for backward compatibility with existing callers: when SandboxMode is
	// Default and Sandboxed is true, it is treated as SandboxModeRequired.
	SandboxMode    SandboxMode `json:"sandbox_mode,omitempty"`
	Sandboxed      bool        `json:"sandboxed,omitempty"`
	ContainerImage string      `json:"container_image,omitempty"`

	// ExtraMounts are additional, policy-authorized host bind mounts for the
	// sandbox container. The orchestrator validates them against its
	// MountPolicy before the backend ever sees the request; backends append
	// them to the container spec verbatim.
	ExtraMounts []ContainerMount `json:"extra_mounts,omitempty"`

	// Storage is the project's RESOLVED workspace storage tier — which
	// filesystem the checkout is mounted on and how much memory the
	// container gets. The orchestrator reads it from the durable project
	// record before dispatching, exactly as it resolves the sandbox mode and
	// the trust level; a backend applies it through ApplyWorkspaceStorage
	// rather than interpreting it. Inert at its zero value, so a spawn for a
	// project with no storage configuration builds the spec it always did.
	Storage WorkspaceStoragePlan `json:"storage,omitempty"`

	// Trust dimension (see container_trust.go and trust_policy.go). These
	// mirror the ContainerSpec trust fields and are copied onto the spec by
	// the sandbox spawn path. Every one is inert at its zero value, so a
	// spawn that does not ask for trusted computing behaves exactly as it did
	// before the dimension existed.
	//
	// ImageDigest pins ContainerImage to an immutable "sha256:<hex>" content
	// digest; the runtime then launches repository@digest instead of the
	// mutable tag and verifies the created container against it.
	ImageDigest string `json:"image_digest,omitempty"`

	// TrustLevel is the integrity tier required of this session. The
	// orchestrator resolves TrustLevelDefault against its TrustPolicy before
	// dispatching, the same way it resolves SandboxModeDefault.
	TrustLevel TrustLevel `json:"trust_level,omitempty"`

	// TrustLabel is the operator-assigned workload identity folded into the
	// launch measurement.
	TrustLabel string `json:"trust_label,omitempty"`

	// ExpectedPCRs is the golden PCR set a Tier 1 host quote is checked
	// against, keyed by PCR index.
	ExpectedPCRs map[uint32][]byte `json:"expected_pcrs,omitempty"`

	// Account pool fields (see account.go / account_resolver.go).
	//
	// AccountID pins the spawn to a specific pooled provider account (one of
	// several e.g. Anthropic/Claude logins). Leaving both AccountID and
	// AccountAuto unset preserves the pre-account-pool default behavior
	// exactly: the backend authenticates however its underlying CLI already
	// does (the host user's global ~/.claude, ~/.codex, ~/.config/opencode).
	AccountID string `json:"account_id,omitempty"`

	// AccountAuto requests strategy-based account selection (static or
	// least-usage — see AccountSelectionStrategy) when AccountID is empty.
	AccountAuto bool `json:"account_auto,omitempty"`

	// AccountStrategy selects a specific registered AccountSelectionStrategy
	// by name for this spawn. Empty selects the tenant's configured default.
	// Ignored unless AccountAuto is true.
	AccountStrategy string `json:"account_strategy,omitempty"`

	// AccountAllow optionally restricts selection to these accounts — the
	// rotation pool chosen for this agent. Empty means the whole
	// tenant/project/provider pool is eligible, which is the pre-existing
	// behavior.
	AccountAllow []string `json:"account_allow,omitempty"`

	// ReplacesSessionID names the session a respawn reconstitutes, so the
	// account slot the session already holds is not counted against its own
	// resume. Set by spawnRequestFromRecord; never sent over the wire.
	ReplacesSessionID string `json:"-"`

	// AccountExclude removes accounts from selection for this one spawn.
	// Rotation sets it to the account that just hit a limit so a replacement
	// can never be the same account, independently of when its hold lapses.
	AccountExclude []string `json:"account_exclude,omitempty"`

	// JobID names the orchestration job this spawn works for, empty when the
	// caller is running none. It is not a dispatch input: it is the SCOPE of
	// the credential delivery grant the spawn's account resolution takes out
	// (see account_lease.go), so a job's deliveries are listable and revocable
	// as a set.
	JobID string `json:"job_id,omitempty"`

	// AccountHomeDir is set by ACPBackend.Spawn itself (never by a caller)
	// when the resolved account carries file-based credentials (e.g.
	// OpenCode's auth.json). It names a fresh, per-session temporary
	// directory holding those files; spawnJSONRPC/spawnContainerJSONRPC use
	// it in place of a bare isolation directory / the host's shared config
	// mount when non-empty. Not serialized: it is spawn-time wiring state,
	// not caller-supplied configuration.
	AccountHomeDir string `json:"-"`

	// SigningRelay, when set, asks the agent runtime to serve the session's
	// signing socket from a relay over its FleetRuntime link rather than from
	// a mount of the control plane's socket (go-quicbaas#1013). Only the
	// agent's spawn server sets it, from the wire; a local backend never reads
	// it. Not serialized: it is spawn-time wiring, not configuration.
	SigningRelay *SigningRelaySpec `json:"-"`
}

// EffectiveSandboxMode returns the sandbox mode the backend should honor,
// accounting for legacy callers that set Sandboxed=true without populating
// SandboxMode.
func (r *SpawnRequest) EffectiveSandboxMode() SandboxMode {
	if r.SandboxMode != SandboxModeDefault {
		return r.SandboxMode
	}
	if r.Sandboxed {
		return SandboxModeRequired
	}
	return SandboxModeDefault
}

// SessionEventCallback receives structured transcript events emitted by a
// backend while a turn runs. The backend populates SessionID, Kind, and the
// matching payload; the consumer stamps TenantID, Source, Sequence, and
// Timestamp before persisting or forwarding.
type SessionEventCallback func(event *SessionEvent)

// PromptRequest contains parameters for prompting an agent.
type PromptRequest struct {
	SessionID     string               `json:"session_id,omitempty"`
	ProjectID     string               `json:"project_id,omitempty"`
	Prompt        string               `json:"prompt"`
	Model         string               `json:"model,omitempty"`
	SystemPrompt  string               `json:"system_prompt,omitempty"`
	EventCallback SessionEventCallback `json:"-"` // Real-time event streaming (not serialized)
}

// PromptResponse contains the agent's response.
type PromptResponse struct {
	SessionID  string         `json:"session_id,omitempty"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`

	// Usage is the token accounting the agent published for this turn, or nil
	// when the backend's CLI publishes none. It is what the orchestrator
	// attributes to the turn's tenant / account / project / job — see
	// agentServiceImpl.recordTurnUsage. Nil means "not measured", not "zero":
	// no usage is recorded for such a turn rather than an estimate.
	Usage *TokenUsage `json:"usage,omitempty"`
}

// Session represents an active agent session.
type Session struct {
	ID        string       `json:"id"`
	ProjectID string       `json:"project_id"`
	Backend   string       `json:"backend"`
	Model     string       `json:"model,omitempty"`
	Status    SessionState `json:"status"`
	PID       int          `json:"pid,omitempty"`
	WorkDir   string       `json:"work_dir"`

	// CreatedAt is the spawn instant in UNIX NANOSECONDS, and every backend
	// owes the same unit. See the unit contract below: this is the field a
	// mixed-unit record starts from, because the orchestrator copies it onto
	// the durable record verbatim while writing LastActivity from its own
	// clock (orchestrator.go).
	CreatedAt int64 `json:"created_at"`

	// AccountID and AccountStrategy report which pooled provider account (if
	// any) this session was spawned against and which AccountSelectionStrategy
	// chose it ("pinned" when the caller set SpawnRequest.AccountID directly).
	// Both are empty when the spawn did not use the account pool. Callers that
	// persist a durable session record are expected to copy these through so a
	// cost anomaly can be traced back to the account and policy that produced
	// it.
	AccountID       string `json:"account_id,omitempty"`
	AccountStrategy string `json:"account_strategy,omitempty"`

	// AccountGeneration identifies WHICH COPY of the pooled account's
	// credential this session materialized (AccountCredential.Generation),
	// read from the envelope at the instant the material was produced — before
	// the agent process was started, not after it answered.
	//
	// A caller that persists a durable session record is expected to copy it
	// through. It is what lets the daemon ask, from the record alone, whether
	// the pool has moved under a live session and roll the session BEFORE its
	// next prompt fails against a credential the provider has stopped
	// honouring (SessionAccountFreshness, go-quicbaas#997).
	//
	// It travels the remote-daemon boundary too (SpawnSessionResponse's
	// account_generation): a remote session materializes its credential on the
	// daemon, so the daemon is the only party that can say which copy it took,
	// and a remote session that reported none would be left on the timestamp
	// comparison this field exists to replace.
	//
	// Zero when the spawn used no pooled account, or when the account's
	// envelope predates generation stamping; both are read as UNKNOWN by the
	// freshness check, which then falls back to the account's rotation
	// timestamp rather than declaring anything stale.
	AccountGeneration int64 `json:"account_generation,omitempty"`

	// CredentialLeaseID is the DELIVERY GRANT the spawn took out over the
	// pooled account's credential material (see account_lease.go). Empty when
	// the spawn used no pooled account, or when the deployment wired no leaser.
	//
	// A caller that persists a durable session record MUST copy it through:
	// the session's ending revokes the grants named on the record, and a grant
	// that reached no record is one no reaper can withdraw.
	CredentialLeaseID string `json:"credential_lease_id,omitempty"`

	// ReservationID is the session-slot reservation the spawn's account
	// resolution took (account_balancing.go). A caller that persists a
	// durable session record MUST copy it through (SessionRecord.
	// AddReservation): the record naming it is what satisfies the
	// reservation, and one that reaches no record holds its slot until it
	// expires.
	ReservationID string `json:"reservation_id,omitempty"`

	// ContainerID names the container this session's agent runs in, when the
	// BACKEND owns that container's lifecycle. It is empty for a session
	// that runs as a host process, and empty on the orchestrator's own
	// detached-container path, which knows the id without being told.
	//
	// It is reported because a container nobody can name is a container
	// nothing can act in: the engine's publish path runs the delivery's
	// commit and push inside this container (ExecSession), and a backend
	// that started one and kept the id to itself would leave the daemon
	// with no way to reach it. Callers persisting a durable session record
	// copy it through so the id survives a resume.
	ContainerID string `json:"container_id,omitempty"`
}

// SessionState represents the state of a session.
type SessionState string

const (
	// SessionStateActive indicates the session is actively processing.
	SessionStateActive SessionState = "active"

	// SessionStateIdle indicates the session is idle and waiting for input.
	SessionStateIdle SessionState = "idle"

	// SessionStateResumable indicates the session's durable record survived a
	// daemon restart but its agent process did not. The session is addressable
	// and its transcript is intact; the process is recreated by the next
	// prompt, which restores the conversation before the turn runs.
	SessionStateResumable SessionState = "resumable"

	// SessionStateClosed indicates the session has been terminated.
	SessionStateClosed SessionState = "closed"

	// SessionStateError indicates the session encountered an error.
	SessionStateError SessionState = "error"
)

// sessionStates is every lifecycle state a session may report, keyed for O(1)
// validation of a value that arrived from a caller.
var sessionStates = map[SessionState]struct{}{
	SessionStateActive:    {},
	SessionStateIdle:      {},
	SessionStateResumable: {},
	SessionStateClosed:    {},
	SessionStateError:     {},
}

// Valid reports whether s is a state this daemon uses. The empty value is NOT
// valid: it is the absence of a state, and every caller that means "any" says
// so by omitting the field rather than by naming nothing.
func (s SessionState) Valid() bool {
	_, ok := sessionStates[s]
	return ok
}

// SessionStates lists every session state, sorted, so a transport or a
// configuration validator can render the set rather than keep a copy of it
// that drifts.
func SessionStates() []string {
	states := make([]string, 0, len(sessionStates))
	for state := range sessionStates {
		states = append(states, string(state))
	}
	sort.Strings(states)
	return states
}

// =============================================================================
// The unit every session instant is in
// =============================================================================
//
// UNIX NANOSECONDS. Session.CreatedAt, SessionStatus.LastActivity,
// SessionInfo's pair of both, and SessionRecord's pair of both are all
// int64 nanoseconds since the epoch — the unit time.Time.UnixNano() produces
// and time.Unix(0, n) reads back.
//
// It is written down because it was not obeyed. Two fields of ONE live
// session record disagreed: created_at arrived in seconds from the
// codex-exec backend, which reported sess.created.Unix(), while
// last_activity arrived in nanoseconds from the orchestrator's own clock.
// The browser's formatter guessed the unit by magnitude and so rendered both
// plausibly, which is worse than rendering one of them wrongly — the guess
// is invisible, and a reader believes a timestamp (go-quicbaas#855).
//
// A producer that has only seconds to offer must still multiply. A consumer
// must never sniff magnitude to decide: there is one unit, and a value that
// does not fit it is a defect in the producer.
//
// EVERY SPAWN PATH, NOT MOST OF THEM. The host ACP backend has five, and two
// of them (spawnContainerJSONRPC, spawnOneShotCLI) already used UnixNano while
// three (spawnJSONRPC — the dispatcher's default — spawnCLI, and
// spawnContainerCLI) used Unix. Converting Prompt's lastActivity without them
// was worse than leaving all four alone: it turned a record that was
// consistently in the wrong unit into one whose two fields disagreed, which is
// the defect itself rather than a step toward fixing it. Caught in review of
// PR#859; the unit is now asserted per backend in
// session_instant_unit_test.go.

// SessionStatus contains health information about a session.
type SessionStatus struct {
	SessionID string       `json:"session_id"`
	State     SessionState `json:"state"`

	// LastActivity is the instant the session was last known to be doing
	// something, in UNIX NANOSECONDS.
	LastActivity int64 `json:"last_activity"`

	// Uptime is a DURATION IN SECONDS, not an instant — the one field here
	// that is neither nanoseconds nor a point in time.
	//
	// Seconds because two things downstream say so and one of them converts:
	// the proto field is named `uptime_seconds` (api/proto/orchestrator.proto)
	// and pkg/api/rest/handlers.go multiplies it by time.Second to render it.
	// The codex-exec backend reported milliseconds into it, which is how this
	// comment first claimed milliseconds — the contract was inferred from the
	// one producer that disobeyed it instead of from the consumer that acts
	// on it (go-quicbaas#855, review of PR#859).
	Uptime int64 `json:"uptime"`
}

// ContentBlock represents a block of content in a response.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// StopReasonEndTurn is the stop reason reported for a turn the agent finished
// on its own. Every backend reports it so callers can distinguish a completed
// turn from one cut short (for example by a token limit) without knowing which
// agent dialect produced it.
const StopReasonEndTurn = "end_turn"

// MCPServerConfig configures an MCP server made available to an agent session.
//
// Two server shapes are expressed by this one struct:
//
//   - stdio servers — the agent launches a local process: Command + Args
//     (+ Env for that process's environment).
//   - HTTP servers — the agent connects to a running endpoint: URL (+ Env,
//     used to carry connection material such as the agent's mTLS cert/key
//     paths for the QuicBaaS MCP endpoint preset).
//
// Env round-trips losslessly through the proto (`map<string,string> env`) and
// the ACP `session/new` params so credential material configured at spawn
// reaches the agent intact. The wire `command` field is retained for backward
// compatibility with callers (and the proto bridge) that encoded an endpoint
// URL into Command before URL existed; new callers should set URL explicitly.
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}
