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
	"strconv"
	"strings"
)

// This file translates the two ACP wire dialects — Claude Code's stream-json
// lines and the JSON-RPC ACP session/update notifications — into structured
// SessionEvents at the source, so the session transcript shows assistant
// text, chain-of-thought, and tool activity as it happens instead of raw
// JSON blobs.

// emitStreamJSONEvents translates one stream-json message into the structured
// session events it represents and delivers them to cb. The mapping:
//
//	assistant text block      -> KindAssistantChunk
//	assistant thinking block  -> KindAgentThought
//	assistant tool_use block  -> KindToolInvocation
//	user tool_result block    -> KindToolResult
//	system (subtype != init)  -> KindSystemMessage (info)
//	system init / result      -> no event (init is noise; the turn lifecycle
//	                             events already cover the result)
func emitStreamJSONEvents(cb SessionEventCallback, sessionID string, msg *streamJSONMessage) {
	if cb == nil || msg == nil {
		return
	}
	switch msg.Type {
	case "assistant":
		if msg.Message == nil {
			return
		}
		for _, block := range msg.Message.Content {
			switch block.Type {
			case "text":
				cb(&SessionEvent{
					SessionID: sessionID,
					Kind:      KindAssistantChunk,
					AssistantChunk: &AssistantChunkPayload{
						Text:       block.Text,
						StopReason: msg.Message.StopReason,
						Model:      msg.Message.Model,
					},
				})
			case "thinking":
				cb(&SessionEvent{
					SessionID: sessionID,
					Kind:      KindAgentThought,
					Thought:   &AgentThoughtPayload{Text: block.Thinking},
				})
			case "tool_use":
				cb(&SessionEvent{
					SessionID: sessionID,
					Kind:      KindToolInvocation,
					ToolInvocation: &ToolInvocationPayload{
						InvocationID: block.ID,
						Name:         block.Name,
						Args:         block.Input,
					},
				})
			}
		}
	case "user":
		if msg.Message == nil {
			return
		}
		for _, block := range msg.Message.Content {
			if block.Type != "tool_result" {
				continue
			}
			cb(&SessionEvent{
				SessionID: sessionID,
				Kind:      KindToolResult,
				ToolResult: &ToolResultPayload{
					InvocationID: block.ToolUseID,
					Content:      flattenToolResultContent(block.Content),
					IsError:      block.IsError,
				},
			})
		}
	case "system":
		// The init message announces model/session wiring the transcript
		// already records elsewhere — skip it as noise.
		if msg.Subtype == "init" {
			return
		}
		cb(&SessionEvent{
			SessionID: sessionID,
			Kind:      KindSystemMessage,
			System: &SystemMessagePayload{
				Level:   "info",
				Code:    msg.Subtype,
				Message: compactJSON(msg),
			},
		})
	}
	// "result" intentionally emits nothing: turn_completed/turn_failed
	// lifecycle events already cover the turn outcome.
}

// streamJSONTurnBlocks reduces one stream-json message to the content blocks
// a PromptResponse carries for it. It reads the same decoded blocks
// emitStreamJSONEvents turns into session events: the assistant's text,
// reasoning and tool calls, and the tool results the CLI reports back on a
// "user" message. A non-text block keeps its identifiers and size, not its
// payload (see ContentBlock).
func streamJSONTurnBlocks(msg *streamJSONMessage) []ContentBlock {
	if msg == nil || msg.Message == nil {
		return nil
	}
	var blocks []ContentBlock
	for _, c := range msg.Message.Content {
		if block, ok := streamJSONTurnBlock(msg.Type, c); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// streamJSONTurnBlock converts one block of a message of the given type, and
// reports false for a block a turn does not carry (for example the text the
// CLI echoes on a "user" message).
func streamJSONTurnBlock(msgType string, c streamJSONContent) (ContentBlock, bool) {
	if c.Type == ContentBlockUnreadable {
		return ContentBlock{Type: ContentBlockUnreadable, Bytes: c.unreadableBytes}, true
	}
	if msgType == "user" {
		if c.Type != ContentBlockToolResult {
			return ContentBlock{}, false
		}
		return ContentBlock{
			Type:      ContentBlockToolResult,
			ToolUseID: c.ToolUseID,
			IsError:   c.IsError,
			Bytes:     len(c.Content),
		}, true
	}
	if msgType != "assistant" {
		return ContentBlock{}, false
	}
	switch c.Type {
	case ContentBlockText:
		return ContentBlock{Type: ContentBlockText, Text: c.Text}, true
	case ContentBlockThinking:
		return ContentBlock{Type: ContentBlockThinking, Bytes: len(c.Thinking)}, true
	case ContentBlockToolUse:
		return ContentBlock{
			Type:      ContentBlockToolUse,
			ToolName:  c.Name,
			ToolUseID: c.ID,
			Bytes:     len(c.Input),
		}, true
	}
	// A block type this package does not know (redacted_thinking, say) is
	// still counted under its own name.
	return ContentBlock{Type: c.Type}, true
}

// flattenToolResultContent renders a stream-json tool_result "content" field
// — either a JSON string or an array of {type,text} blocks — as plain text.
// An unrecognized shape is passed through as its raw JSON so nothing is
// silently dropped from the transcript.
func flattenToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, block := range blocks {
			b.WriteString(block.Text)
		}
		return b.String()
	}
	return string(raw)
}

// compactJSON marshals v for a human-scannable system-message body. A marshal
// failure yields an empty string rather than an error: the emitting paths are
// best-effort observability, never turn control flow.
func compactJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

// acpSessionUpdateParams mirrors the params object of a JSON-RPC ACP
// session/update notification.
type acpSessionUpdateParams struct {
	SessionID string           `json:"sessionId,omitempty"`
	Update    acpSessionUpdate `json:"update"`
}

// acpSessionUpdate is the discriminated update body. Content is raw because
// its shape depends on SessionUpdate: an object {type,text} for message /
// thought chunks, an array of tool-call content blocks for tool_call_update.
type acpSessionUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content,omitempty"`
	ToolCallID    string          `json:"toolCallId,omitempty"`
	Title         string          `json:"title,omitempty"`
	Kind          string          `json:"kind,omitempty"`
	Status        string          `json:"status,omitempty"`
	RawInput      json.RawMessage `json:"rawInput,omitempty"`
	Entries       []acpPlanEntry  `json:"entries,omitempty"`
}

// acpPlanEntry is one step of an ACP plan update.
type acpPlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// contentText decodes the {type,text} object shape of Content. Returns ""
// when Content is absent or shaped differently (e.g., a tool-call block
// array).
func (u *acpSessionUpdate) contentText() string {
	if len(u.Content) == 0 {
		return ""
	}
	var c struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(u.Content, &c); err != nil {
		return ""
	}
	return c.Text
}

// toolCallText decodes the tool-call content-block array shape of Content and
// concatenates every text fragment, covering both the nested ACP shape
// {type:"content",content:{type:"text",text}} and flattened {type:"text",text}
// blocks.
func (u *acpSessionUpdate) toolCallText() string {
	if len(u.Content) == 0 {
		return ""
	}
	var blocks []struct {
		Type    string `json:"type"`
		Text    string `json:"text,omitempty"`
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content,omitempty"`
	}
	if err := json.Unmarshal(u.Content, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, block := range blocks {
		b.WriteString(block.Text)
		b.WriteString(block.Content.Text)
	}
	return b.String()
}

// Terminal tool_call_update statuses in the ACP dialect.
const (
	acpToolCallStatusCompleted = "completed"
	acpToolCallStatusFailed    = "failed"
)

// emitACPSessionUpdate translates one parsed session/update notification into
// its structured session event and delivers it to cb. The mapping:
//
//	agent_message_chunk            -> KindAssistantChunk
//	agent_thought_chunk            -> KindAgentThought
//	tool_call                      -> KindToolInvocation
//	tool_call_update (terminal)    -> KindToolResult
//	tool_call_update (in progress) -> no event
//	plan                           -> KindSystemMessage (code "plan")
//	anything else                  -> KindSystemMessage (code = the update
//	                                  value) so nothing is silently dropped
func emitACPSessionUpdate(cb SessionEventCallback, sessionID string, u *acpSessionUpdate) {
	if cb == nil || u == nil {
		return
	}
	switch u.SessionUpdate {
	case "agent_message_chunk":
		cb(&SessionEvent{
			SessionID:      sessionID,
			Kind:           KindAssistantChunk,
			AssistantChunk: &AssistantChunkPayload{Text: u.contentText()},
		})
	case "agent_thought_chunk":
		cb(&SessionEvent{
			SessionID: sessionID,
			Kind:      KindAgentThought,
			Thought:   &AgentThoughtPayload{Text: u.contentText()},
		})
	case "tool_call":
		name := u.Title
		if name == "" {
			name = u.Kind
		}
		cb(&SessionEvent{
			SessionID: sessionID,
			Kind:      KindToolInvocation,
			ToolInvocation: &ToolInvocationPayload{
				InvocationID: u.ToolCallID,
				Name:         name,
				Args:         u.RawInput,
			},
		})
	case "tool_call_update":
		if u.Status != acpToolCallStatusCompleted && u.Status != acpToolCallStatusFailed {
			return // non-terminal progress updates emit nothing
		}
		cb(&SessionEvent{
			SessionID: sessionID,
			Kind:      KindToolResult,
			ToolResult: &ToolResultPayload{
				InvocationID: u.ToolCallID,
				Content:      u.toolCallText(),
				IsError:      u.Status == acpToolCallStatusFailed,
			},
		})
	case "plan":
		cb(&SessionEvent{
			SessionID: sessionID,
			Kind:      KindSystemMessage,
			System: &SystemMessagePayload{
				Level:   "info",
				Code:    "plan",
				Message: summarizePlanEntries(u.Entries),
			},
		})
	default:
		cb(&SessionEvent{
			SessionID: sessionID,
			Kind:      KindSystemMessage,
			System: &SystemMessagePayload{
				Level:   "info",
				Code:    u.SessionUpdate,
				Message: compactJSON(u),
			},
		})
	}
}

// summarizePlanEntries renders a plan update as one short human-readable
// line: "plan (2 steps): fix parser [in_progress]; run tests [pending]".
func summarizePlanEntries(entries []acpPlanEntry) string {
	if len(entries) == 0 {
		return "plan updated (no entries)"
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		p := e.Content
		if e.Status != "" {
			p += " [" + e.Status + "]"
		}
		parts = append(parts, p)
	}
	return "plan (" + strconv.Itoa(len(entries)) + " steps): " + strings.Join(parts, "; ")
}

// emitPromptResponseChunks emits one KindAssistantChunk per text content
// block of a completed response. It serves the dialect paths that produce the
// whole answer in one shot (one-shot CLI, JSON-RPC result fallback, remote
// frames) so their transcripts match the streaming dialects'.
func emitPromptResponseChunks(cb SessionEventCallback, resp *PromptResponse) {
	if cb == nil || resp == nil {
		return
	}
	for _, block := range resp.Content {
		if block.Type != "text" || block.Text == "" {
			continue
		}
		cb(&SessionEvent{
			SessionID: resp.SessionID,
			Kind:      KindAssistantChunk,
			AssistantChunk: &AssistantChunkPayload{
				Text:       block.Text,
				StopReason: resp.StopReason,
				Model:      resp.Model,
			},
		})
	}
}
