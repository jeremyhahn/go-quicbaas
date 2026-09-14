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
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SessionEventKind discriminates the payload carried by a SessionEvent. Each
// kind pairs with exactly one of the `*Payload` pointer fields on
// SessionEvent; consumers switch on Kind and then dereference the matching
// pointer.
type SessionEventKind string

const (
	// KindPromptSubmitted records a prompt sent to the agent by an operator
	// or the orchestrator's control loop.
	KindPromptSubmitted SessionEventKind = "prompt_submitted"

	// KindAssistantChunk records one fragment of assistant output. Long
	// responses produce many KindAssistantChunk events in sequence; the
	// final fragment carries StopReason.
	KindAssistantChunk SessionEventKind = "assistant_chunk"

	// KindAgentThought records a fragment of the agent's chain-of-thought
	// (extended thinking / reasoning) emitted while working a turn.
	KindAgentThought SessionEventKind = "agent_thought"

	// KindToolInvocation records the agent invoking a tool (Read, Write,
	// Edit, Grep, Bash, etc.) with its arguments.
	KindToolInvocation SessionEventKind = "tool_invocation"

	// KindToolResult records the result of a prior tool invocation. Paired
	// with the invocation via InvocationID.
	KindToolResult SessionEventKind = "tool_result"

	// KindFileOp records a file read/write/edit produced by a tool.
	// Reported alongside KindToolResult so attachers can render diffs.
	KindFileOp SessionEventKind = "file_op"

	// KindSystemMessage records orchestrator-internal events: session
	// resumed after daemon restart, policy enforcement decisions,
	// non-fatal warnings surfaced to operators.
	KindSystemMessage SessionEventKind = "system_message"

	// KindOperatorJoined records an operator attaching to the session. The
	// payload carries operator identity + interface (cli, web, signal).
	KindOperatorJoined SessionEventKind = "operator_joined"

	// KindOperatorLeft records an operator detaching.
	KindOperatorLeft SessionEventKind = "operator_left"

	// KindSessionClosed records session termination. Always the last event
	// appended to a session log. Payload carries the termination reason.
	KindSessionClosed SessionEventKind = "session_closed"

	// KindSessionResumed records a daemon-side session reattach after a
	// daemon or central restart.
	KindSessionResumed SessionEventKind = "session_resumed"

	// KindAccountRotated records that the session was moved onto a different
	// pooled provider account. It is its own kind rather than a plain system
	// message because it is the point at which the conversation's cost
	// attribution changes hands: an operator reading a transcript needs to see
	// exactly where one account's segment ended and the next began.
	KindAccountRotated SessionEventKind = "account_rotated"

	// KindInferenceSubmitted records a stateless inference prompt submitted
	// through the fleet-routed Infer path. Distinct from KindPromptSubmitted
	// which requires a prior session Spawn.
	KindInferenceSubmitted SessionEventKind = "inference_submitted"

	// KindInferenceCompleted records the successful completion of a stateless
	// inference call. Carries the model name and stop reason from the backend.
	KindInferenceCompleted SessionEventKind = "inference_completed"

	// KindTurnStarted marks the orchestrator accepting an agent turn for
	// background execution. It is appended synchronously, before the prompt
	// dispatch returns its acknowledgement, so an attacher that subscribes
	// immediately after the ack always observes the turn.
	KindTurnStarted SessionEventKind = "turn_started"

	// KindTurnCompleted marks an accepted turn finishing successfully. It is
	// the terminal event of a turn and carries the same TurnID as the
	// KindTurnStarted that opened it.
	KindTurnCompleted SessionEventKind = "turn_completed"

	// KindTurnFailed marks an accepted turn ending in an error. Terminal for
	// the turn; carries the failure text so operators see why the agent
	// stopped without having to correlate server logs.
	KindTurnFailed SessionEventKind = "turn_failed"
)

// Turn lifecycle status values. They label a turn in the acknowledgement
// returned by AgentService.PromptSessionAsync and in the turn lifecycle
// events, so a consumer reading either surface sees the same vocabulary.
const (
	// TurnStatusAccepted reports that the orchestrator persisted the turn and
	// dispatched it; the agent's output will arrive on the session event log.
	TurnStatusAccepted = "accepted"

	// TurnStatusCompleted reports a turn that finished successfully.
	TurnStatusCompleted = "completed"

	// TurnStatusFailed reports a turn that ended in an error.
	TurnStatusFailed = "failed"
)

// TurnStartedPayload is the payload for KindTurnStarted. It opens a turn: an
// attacher that sees this event without a matching KindTurnCompleted or
// KindTurnFailed knows the agent is still working.
type TurnStartedPayload struct {
	// TurnID correlates this event with the KindTurnCompleted or
	// KindTurnFailed that closes the turn, and with the TurnID returned in
	// the dispatch acknowledgement.
	TurnID string `json:"turn_id"`

	// Backend is the agent backend the turn was dispatched to.
	Backend string `json:"backend,omitempty"`

	// Model is the model resolved for the turn, when known up front.
	Model string `json:"model,omitempty"`
}

// TurnCompletedPayload is the payload for KindTurnCompleted.
type TurnCompletedPayload struct {
	// TurnID matches the TurnStartedPayload that opened the turn.
	TurnID string `json:"turn_id"`

	// StopReason is why the agent stopped ("end_turn", "max_tokens", ...).
	StopReason string `json:"stop_reason,omitempty"`

	// Model is the model that produced the turn, as reported by the backend.
	Model string `json:"model,omitempty"`

	// DurationMS is the wall-clock time the turn took, in milliseconds.
	DurationMS int64 `json:"duration_ms,omitempty"`

	// Usage is the turn's token accounting as the backend reported it, or nil
	// when the backend reported none. It is carried on the transcript as well
	// as in the cost ledger because an operator reading a conversation needs
	// to see what each answer cost without joining against a second store.
	Usage *TokenUsage `json:"usage,omitempty"`
}

// TurnFailedPayload is the payload for KindTurnFailed.
type TurnFailedPayload struct {
	// TurnID matches the TurnStartedPayload that opened the turn.
	TurnID string `json:"turn_id"`

	// Error is the failure text surfaced to operators.
	Error string `json:"error"`

	// DurationMS is the wall-clock time before the failure, in milliseconds.
	DurationMS int64 `json:"duration_ms,omitempty"`
}

// InferenceSubmittedPayload is the payload for KindInferenceSubmitted.
type InferenceSubmittedPayload struct {
	// Backend is the named backend the prompt was dispatched to.
	Backend string `json:"backend"`

	// Model is the model name requested by the caller.
	Model string `json:"model,omitempty"`

	// Prompt is the user prompt text.
	Prompt string `json:"prompt"`

	// SystemPrompt is the optional system message.
	SystemPrompt string `json:"system_prompt,omitempty"`
}

// InferenceCompletedPayload is the payload for KindInferenceCompleted.
type InferenceCompletedPayload struct {
	// Backend is the named backend that produced the response.
	Backend string `json:"backend"`

	// Model is the model that generated the response.
	Model string `json:"model,omitempty"`

	// StopReason is the reason the model stopped generating
	// (e.g., "end_turn", "max_tokens").
	StopReason string `json:"stop_reason,omitempty"`
}

// SessionEvent is the canonical envelope stored in the session event log.
// Every kind of session state change is recorded as a SessionEvent and
// delivered to attachers in strict per-session sequence order.
//
// Exactly one of the payload pointer fields is non-nil, corresponding to
// Kind. The rest are nil and marshal out via `omitempty`. Consumers switch
// on Kind and dereference the matching payload.
type SessionEvent struct {
	// Sequence is a monotonically increasing per-session counter. Gaps are
	// not allowed; consumers validate monotonicity before delivering an
	// event to subscribers. Starts at 1.
	Sequence uint64 `json:"sequence"`

	// SessionID is the session this event belongs to.
	SessionID string `json:"session_id"`

	// TenantID is the tenant namespace. Used to scope event visibility to
	// operators within the tenant.
	TenantID string `json:"tenant_id"`

	// Timestamp is when the event was produced. UTC.
	Timestamp time.Time `json:"timestamp"`

	// Source identifies the producer. Free-form but follows a convention:
	//   agent:<backend>         - the agent itself (assistant chunks, tool calls)
	//   operator:<user_id>      - a human operator (prompts, joins, leaves)
	//   system:<component>      - orchestrator internals (session_resumed, etc.)
	//   daemon:<agent_id>       - the daemon forwarding events
	Source string `json:"source,omitempty"`

	// Kind selects the payload.
	Kind SessionEventKind `json:"kind"`

	// Exactly one of these is populated; the rest are nil.
	Prompt         *PromptSubmittedPayload `json:"prompt,omitempty"`
	AssistantChunk *AssistantChunkPayload  `json:"assistant_chunk,omitempty"`
	Thought        *AgentThoughtPayload    `json:"thought,omitempty"`
	ToolInvocation *ToolInvocationPayload  `json:"tool_invocation,omitempty"`
	ToolResult     *ToolResultPayload      `json:"tool_result,omitempty"`
	FileOp         *FileOpPayload          `json:"file_op,omitempty"`
	System         *SystemMessagePayload   `json:"system,omitempty"`
	OperatorJoined *OperatorJoinedPayload  `json:"operator_joined,omitempty"`
	OperatorLeft   *OperatorLeftPayload    `json:"operator_left,omitempty"`
	Closed         *SessionClosedPayload   `json:"closed,omitempty"`
	Resumed        *SessionResumedPayload  `json:"resumed,omitempty"`
	TurnStarted    *TurnStartedPayload     `json:"turn_started,omitempty"`
	TurnCompleted  *TurnCompletedPayload   `json:"turn_completed,omitempty"`
	TurnFailed     *TurnFailedPayload      `json:"turn_failed,omitempty"`
}

// PromptSubmittedPayload is the payload for KindPromptSubmitted.
type PromptSubmittedPayload struct {
	// Prompt is the text the operator (or lead agent) submitted.
	Prompt string `json:"prompt"`

	// Model is the model name the caller requested, or empty to use the
	// session default.
	Model string `json:"model,omitempty"`

	// SystemPrompt is an optional system message applied to this prompt.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// TruncatedBytes is non-zero when Prompt or SystemPrompt exceeded
	// MaxSessionEventTextBytes and was clipped on the way into the log. The
	// stored text ends in a visible truncation marker.
	TruncatedBytes int64 `json:"truncated_bytes,omitempty"`
}

// AssistantChunkPayload carries one fragment of assistant output.
type AssistantChunkPayload struct {
	// Text is the assistant output for this chunk.
	Text string `json:"text"`

	// StopReason is non-empty only on the terminal chunk of a response
	// (e.g., "end_turn", "tool_use", "max_tokens"). Consumers use this to
	// know when to finalize a coalesced rendering (e.g., stop editing a
	// Signal message or close an SSE paragraph).
	StopReason string `json:"stop_reason,omitempty"`

	// Model is the model that produced this chunk.
	Model string `json:"model,omitempty"`

	// Usage is the turn's token accounting as the backend reported it, set
	// only on the aggregate chunk a non-streaming turn produces. A streaming
	// turn reports its usage on the TurnCompletedPayload that closes it,
	// because the count is not known until the last fragment has been sent.
	Usage *TokenUsage `json:"usage,omitempty"`

	// TruncatedBytes is non-zero when Text exceeded MaxSessionEventTextBytes
	// and was clipped on the way into the log.
	TruncatedBytes int64 `json:"truncated_bytes,omitempty"`
}

// AgentThoughtPayload carries one fragment of agent reasoning.
type AgentThoughtPayload struct {
	Text string `json:"text"`

	// TruncatedBytes is non-zero when Text exceeded MaxSessionEventTextBytes
	// and was clipped on the way into the log.
	TruncatedBytes int64 `json:"truncated_bytes,omitempty"`
}

// ToolInvocationPayload describes a tool call the agent made.
type ToolInvocationPayload struct {
	// InvocationID is the daemon's ID for this invocation. Used by
	// ToolResult events to reference their invocation.
	InvocationID string `json:"invocation_id"`

	// Name is the tool name ("Read", "Bash", "Edit", etc.).
	Name string `json:"name"`

	// Args is the tool's input as JSON. Rendered collapsed in the UI; click
	// to expand. Args larger than MaxSessionEventArgsBytes are REPLACED with
	// a marker object — structured input cannot be clipped mid-value and stay
	// parseable — and TruncatedBytes records the original size.
	Args json.RawMessage `json:"args,omitempty"`

	// TruncatedBytes is non-zero when Args was replaced for size.
	TruncatedBytes int64 `json:"truncated_bytes,omitempty"`
}

// ToolResultPayload carries the result of a prior ToolInvocation.
type ToolResultPayload struct {
	// InvocationID links back to the ToolInvocationPayload that initiated
	// this result.
	InvocationID string `json:"invocation_id"`

	// Content is the tool's output, usually text. Large outputs (>1 MB)
	// are truncated with a marker and a continuation URL.
	Content string `json:"content,omitempty"`

	// IsError indicates the tool exited non-zero / returned an error shape.
	IsError bool `json:"is_error,omitempty"`

	// TruncatedBytes is non-zero when Content was clipped for size.
	TruncatedBytes int64 `json:"truncated_bytes,omitempty"`
}

// FileOpPayload records a file mutation produced by a tool. Reported
// alongside KindToolResult so attachers can render diffs.
type FileOpPayload struct {
	// Op is "read", "write", "edit", or "delete".
	Op string `json:"op"`

	// Path is the absolute workspace path.
	Path string `json:"path"`

	// Size is the resulting file size in bytes, when known.
	Size int64 `json:"size,omitempty"`

	// Diff is a unified-diff snippet for "edit" operations. Omitted when
	// the operation isn't a modification.
	Diff string `json:"diff,omitempty"`

	// InvocationID ties this FileOp back to the tool invocation that
	// produced it.
	InvocationID string `json:"invocation_id,omitempty"`
}

// SystemMessagePayload carries orchestrator-level events: warnings, policy
// denials, session lifecycle transitions.
type SystemMessagePayload struct {
	// Level is "info", "warn", or "error".
	Level string `json:"level"`

	// Message is the human-readable message.
	Message string `json:"message"`

	// Code is an optional machine-readable tag (e.g., "sandbox_violation",
	// "tenant_mismatch").
	Code string `json:"code,omitempty"`
}

// OperatorJoinedPayload records an operator attaching to a session.
type OperatorJoinedPayload struct {
	// OperatorID is the IAM subject / user ID of the attaching operator.
	OperatorID string `json:"operator_id"`

	// DisplayName is the operator's friendly name if available.
	DisplayName string `json:"display_name,omitempty"`

	// Interface is "cli", "web", "signal", or "sdk".
	Interface string `json:"interface"`
}

// OperatorLeftPayload records an operator detaching. Symmetric to
// OperatorJoinedPayload.
type OperatorLeftPayload struct {
	OperatorID  string `json:"operator_id"`
	DisplayName string `json:"display_name,omitempty"`
	Interface   string `json:"interface"`
}

// SessionClosedPayload is the final event on a session log.
type SessionClosedPayload struct {
	// Reason is "user", "operator", "error", "timeout", or "tenant_revoked".
	Reason string `json:"reason"`

	// Detail is optional free-form context (e.g., the error message).
	Detail string `json:"detail,omitempty"`
}

// SessionResumedPayload records a daemon-side session reattach after a
// process restart.
type SessionResumedPayload struct {
	// ResumedBy identifies what triggered the resume ("daemon_restart",
	// "central_restart", "manual").
	ResumedBy string `json:"resumed_by"`
}

// =============================================================================
// Session Metadata
// =============================================================================

// SessionMeta is the top-level record for a session, stored alongside the
// event log under sessions/{tenant}/{session_id}/meta. Mutable fields
// (Status, LatestSequence, LastActivity) are updated on each append;
// immutable fields (CreatedAt, CreatedBy, Backend, Model) are set once at
// session start.
type SessionMeta struct {
	SessionID      string            `json:"session_id"`
	TenantID       string            `json:"tenant_id"`
	ProjectID      string            `json:"project_id,omitempty"`
	Backend        string            `json:"backend"`
	Model          string            `json:"model,omitempty"`
	Host           string            `json:"host,omitempty"` // reachable agent daemon host, when remote
	Status         SessionState      `json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	CreatedBy      string            `json:"created_by,omitempty"`
	LastActivity   time.Time         `json:"last_activity"`
	LatestSequence uint64            `json:"latest_sequence"`
	EventCount     uint64            `json:"event_count"`
	Labels         map[string]string `json:"labels,omitempty"`

	// OldestSequence is the lowest sequence still stored. It is 0 until the
	// first prune and equal to the first retained turn's opening sequence
	// afterwards, so a reader knows where the surviving transcript starts
	// rather than inferring it from a scan that finds nothing.
	OldestSequence uint64 `json:"oldest_sequence,omitempty"`

	// TurnStarts holds the sequence each retained turn opened at, oldest
	// first. It is the index the retention prune works from: dropping the
	// oldest turn is "delete every event below TurnStarts[1]", which needs no
	// scan and no second store. Bounded by the tenant's retention setting.
	TurnStarts []uint64 `json:"turn_starts,omitempty"`
}

// SessionFilter narrows a ListSessions query.
type SessionFilter struct {
	// Backend filters by backend name. Empty = any.
	Backend string

	// Status filters by session state. Empty = any.
	Status SessionState

	// CreatedAfter filters to sessions created on or after this time.
	// Zero value = no lower bound.
	CreatedAfter time.Time

	// CreatedBefore filters to sessions created on or before this time.
	// Zero value = no upper bound.
	CreatedBefore time.Time

	// Labels, when non-empty, requires every key/value to be present in
	// SessionMeta.Labels.
	Labels map[string]string
}

// =============================================================================
// Errors
// =============================================================================

// ErrSessionLogGap indicates the event log received a non-monotonic sequence
// number. The caller has either a misbehaving daemon or a bug upstream;
// either way we refuse the append so downstream replay stays gapless.
var ErrSessionLogGap = errors.New("orchestration session log: sequence gap")

// ErrSessionLogConcluded reports an append to a session that has already ended.
//
// KindSessionClosed is the last event a session has, and this is what makes
// that mechanical rather than documentary. It is returned by
// AppendNextSessionEvent, so every writer meets it on the one path they share
// — a caller that prompts a dead session gets a refusal instead of a write that
// silently lands after the ending, or on top of it.
var ErrSessionLogConcluded = errors.New("orchestration session log: session already concluded")

// ErrSessionLogNotFound indicates a lookup for a session that has no events
// or metadata on record. Distinct from a session whose log is empty mid-lifetime.
var ErrSessionLogNotFound = errors.New("orchestration session log: session not found")

// SessionLogAppendError wraps underlying storage failures on append so
// callers can distinguish persistence problems from gap / validation errors.
type SessionLogAppendError struct {
	SessionID string
	Sequence  uint64
	Cause     error
}

func (e *SessionLogAppendError) Error() string {
	return fmt.Sprintf("orchestration session log: append session=%s seq=%d: %v", e.SessionID, e.Sequence, e.Cause)
}

func (e *SessionLogAppendError) Unwrap() error { return e.Cause }

// Session event validation sentinels for MemorySessionEventLog.Append.
var (
	// ErrSessionEventNil indicates a nil event was passed to Append.
	ErrSessionEventNil = errors.New("nil event")
	// ErrSessionEventMissingIDs indicates an event was missing its
	// session_id or tenant_id.
	ErrSessionEventMissingIDs = errors.New("session_id and tenant_id required")
)

// SessionLogError wraps a session event-log operation failure (validation,
// marshalling, persistence) with a contextual op string. It is the typed
// replacement for fmt.Errorf in the session-log subsystem and unwraps to the
// underlying cause for errors.Is/As traversal.
type SessionLogError struct {
	// Op names the failing operation, e.g. "unmarshal event".
	Op string
	// Cause is the wrapped underlying error, or nil for pure validation
	// failures.
	Cause error
}

// Error implements the error interface.
func (e *SessionLogError) Error() string {
	if e.Cause != nil {
		return "orchestration session log: " + e.Op + ": " + e.Cause.Error()
	}
	return "orchestration session log: " + e.Op
}

// Unwrap returns the wrapped cause, if any.
func (e *SessionLogError) Unwrap() error { return e.Cause }
