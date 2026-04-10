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

// ACP JSON-RPC message types for the Agent Client Protocol.

// JSONRPCVersion is the JSON-RPC protocol version.
const JSONRPCVersion = "2.0"

// JSONRPCMessage is the base JSON-RPC 2.0 message.
type JSONRPCMessage struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Method  string        `json:"method,omitempty"`
	Params  any           `json:"params,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// InitializeParams are sent to initialize the ACP connection.
type InitializeParams struct {
	ProtocolVersion int        `json:"protocolVersion"`
	ClientInfo      ClientInfo `json:"clientInfo"`
	Capabilities    any        `json:"capabilities,omitempty"`
}

// ClientInfo identifies the ACP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is returned from the initialize method.
type InitializeResult struct {
	ProtocolVersion   int        `json:"protocolVersion"`
	AgentCapabilities any        `json:"agentCapabilities,omitempty"`
	AgentInfo         ServerInfo `json:"agentInfo"`
}

// ServerInfo identifies the ACP server/agent.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SessionNewParams creates a new ACP session.
type SessionNewParams struct {
	CWD          string            `json:"cwd"`
	MCPServers   []MCPServerConfig `json:"mcpServers"`
	Model        string            `json:"model,omitempty"`
	SystemPrompt string            `json:"systemPrompt,omitempty"`
}

// SessionNewResult is returned from session/new.
type SessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// PromptContent represents a content block in a prompt array.
type PromptContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// SessionPromptParams sends a prompt to an ACP session.
type SessionPromptParams struct {
	SessionID string          `json:"sessionId"`
	Prompt    []PromptContent `json:"prompt"`
}

// SessionUpdate is an event from the agent during prompt execution.
type SessionUpdate struct {
	Type      string       `json:"type"`
	SessionID string       `json:"sessionId"`
	Content   ContentBlock `json:"content,omitempty"`
	ToolCall  *ToolCall    `json:"toolCall,omitempty"`
}

// ToolCall represents an agent requesting a tool execution.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

// ToolResult is sent back to the agent after tool execution.
type ToolResult struct {
	ToolCallID string `json:"toolCallId"`
	Content    string `json:"content"`
	IsError    bool   `json:"isError,omitempty"`
}

// SessionPromptResult is the final result of a prompt.
type SessionPromptResult struct {
	SessionID  string         `json:"sessionId"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stopReason,omitempty"`
}

// SessionCloseParams closes an ACP session.
type SessionCloseParams struct {
	SessionID string `json:"sessionId"`
}
