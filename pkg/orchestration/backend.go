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

// BackendKind classifies a backend as either an agent (ACP-compatible, persistent
// sessions, tool use, file access) or a model server (stateless prompt/response).
type BackendKind string

const (
	// BackendKindAgent indicates an ACP-compatible agent backend that supports
	// persistent sessions, tool use, and file access (e.g., claude, codex, opencode).
	BackendKindAgent BackendKind = "agent"

	// BackendKindModelServer indicates a stateless model server that only supports
	// prompt/response operations (e.g., ollama).
	BackendKindModelServer BackendKind = "model_server"
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
	Sandboxed      bool   `json:"sandboxed,omitempty"`
	ContainerImage string `json:"container_image,omitempty"`
}

// AgentEventCallback is called by backends during prompt execution to stream
// real-time events (tool calls, responses, file operations). The system always
// calls this -- display filtering happens downstream in the CLI/GUI layer.
type AgentEventCallback func(sessionID, eventType, details string)

// PromptRequest contains parameters for prompting an agent.
type PromptRequest struct {
	SessionID     string             `json:"session_id,omitempty"`
	ProjectID     string             `json:"project_id,omitempty"`
	Prompt        string             `json:"prompt"`
	Model         string             `json:"model,omitempty"`
	SystemPrompt  string             `json:"system_prompt,omitempty"`
	EventCallback AgentEventCallback `json:"-"` // Real-time event streaming (not serialized)
}

// PromptResponse contains the agent's response.
type PromptResponse struct {
	SessionID  string         `json:"session_id,omitempty"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`
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
}

// SessionState represents the state of a session.
type SessionState string

const (
	// SessionStateActive indicates the session is actively processing.
	SessionStateActive SessionState = "active"

	// SessionStateIdle indicates the session is idle and waiting for input.
	SessionStateIdle SessionState = "idle"

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

// MCPServerConfig configures an MCP server for an agent session.
type MCPServerConfig struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}
