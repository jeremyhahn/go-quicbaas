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

import "context"

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

	// AccountExclude removes accounts from selection for this one spawn.
	// Rotation sets it to the account that just hit a limit so a replacement
	// can never be the same account, independently of when its hold lapses.
	AccountExclude []string `json:"account_exclude,omitempty"`

	// AccountHomeDir is set by ACPBackend.Spawn itself (never by a caller)
	// when the resolved account carries file-based credentials (e.g.
	// OpenCode's auth.json). It names a fresh, per-session temporary
	// directory holding those files; spawnJSONRPC/spawnContainerJSONRPC use
	// it in place of a bare isolation directory / the host's shared config
	// mount when non-empty. Not serialized: it is spawn-time wiring state,
	// not caller-supplied configuration.
	AccountHomeDir string `json:"-"`
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
	CreatedAt int64        `json:"created_at"`

	// AccountID and AccountStrategy report which pooled provider account (if
	// any) this session was spawned against and which AccountSelectionStrategy
	// chose it ("pinned" when the caller set SpawnRequest.AccountID directly).
	// Both are empty when the spawn did not use the account pool. Callers that
	// persist a durable session record are expected to copy these through so a
	// cost anomaly can be traced back to the account and policy that produced
	// it.
	AccountID       string `json:"account_id,omitempty"`
	AccountStrategy string `json:"account_strategy,omitempty"`
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

// SessionStatus contains health information about a session.
type SessionStatus struct {
	SessionID    string       `json:"session_id"`
	State        SessionState `json:"state"`
	LastActivity int64        `json:"last_activity"`
	Uptime       int64        `json:"uptime"`
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
