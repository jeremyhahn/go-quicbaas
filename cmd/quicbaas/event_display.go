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

// Package main provides orchestrator CLI commands for QuicBaaS agent management.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// eventDisplay renders orchestration events in a docker-compose-style format
// with color-coded role prefixes and parsed JSON payloads.
type eventDisplay struct {
	w             io.Writer
	colorMap      map[string]string // sessionID -> ANSI color
	sessionRole   map[string]string // sessionID -> role name (lead, coder, reviewer)
	nextColor     int
	maxLabel      int    // longest role label seen, for alignment
	streaming     bool   // currently mid-line in a streaming chunk
	streamingRole string // role of current streaming line
}

var displayColors = []string{
	"\033[36m", // cyan
	"\033[33m", // yellow
	"\033[35m", // magenta
	"\033[32m", // green
	"\033[91m", // bright red
	"\033[93m", // bright yellow
	"\033[95m", // bright magenta
	"\033[94m", // bright blue
}

const colorReset = "\033[0m"
const colorDim = "\033[90m"

// newEventDisplay creates a new event display that writes to w.
func newEventDisplay(w io.Writer) *eventDisplay {
	return &eventDisplay{
		w:           w,
		colorMap:    make(map[string]string),
		sessionRole: make(map[string]string),
		maxLabel:    8, // "reviewer" length as default
	}
}

// assignColor returns or assigns an ANSI color for a session.
func (d *eventDisplay) assignColor(sessionID string) string {
	if c, ok := d.colorMap[sessionID]; ok {
		return c
	}
	c := displayColors[d.nextColor%len(displayColors)]
	d.nextColor++
	d.colorMap[sessionID] = c
	return c
}

// registerSession maps a session to a role and updates maxLabel.
func (d *eventDisplay) registerSession(sessionID, role string) {
	d.sessionRole[sessionID] = role
	d.assignColor(sessionID)
	if len(role) > d.maxLabel {
		d.maxLabel = len(role)
	}
}

// prefix returns the formatted prefix like "[coder   ] | ".
// The role is right-padded to maxLabel width for alignment.
func (d *eventDisplay) prefix(role string) string {
	color := "\033[36m" // default cyan
	for sid, r := range d.sessionRole {
		if r == role {
			color = d.assignColor(sid)
			break
		}
	}
	padded := role + strings.Repeat(" ", d.maxLabel-len(role))
	return fmt.Sprintf("%s[%s]%s | ", color, padded, colorReset)
}

// roleFor looks up the role for a session, returns "worker" if unknown.
func (d *eventDisplay) roleFor(sessionID string) string {
	if r, ok := d.sessionRole[sessionID]; ok {
		return r
	}
	return "worker"
}

// endStreaming finishes any in-progress streaming line.
func (d *eventDisplay) endStreaming() {
	if d.streaming {
		fmt.Fprintln(d.w)
		d.streaming = false
		d.streamingRole = ""
	}
}

// parseKeyValues parses key=value pairs from the beginning of a string.
// Returns the map and any remaining string (typically JSON starting with '{').
// Values are space-delimited unless they are the last value before a JSON blob
// or the end of the string.
func parseKeyValues(s string) (map[string]string, string) {
	result := make(map[string]string)
	s = strings.TrimSpace(s)

	for len(s) > 0 {
		// Check if remaining string starts with JSON.
		if s[0] == '{' || s[0] == '[' {
			return result, s
		}

		// Find the equals sign for the next key.
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			// No more key=value pairs; rest is remainder.
			return result, s
		}

		key := s[:eqIdx]
		// Reject keys with spaces (means we've hit a non-kv segment).
		if strings.ContainsRune(key, ' ') {
			return result, s
		}

		rest := s[eqIdx+1:]

		// Find the next key=value boundary by looking for " key=" pattern.
		// Scan for the next space followed by a word and '='.
		valueEnd := len(rest)
		for i := 0; i < len(rest); i++ {
			if rest[i] == ' ' {
				// Check if what follows looks like "key=".
				remaining := rest[i+1:]
				if len(remaining) > 0 && (remaining[0] == '{' || remaining[0] == '[') {
					// JSON blob follows.
					valueEnd = i
					break
				}
				nextEq := strings.IndexByte(remaining, '=')
				if nextEq > 0 && !strings.ContainsRune(remaining[:nextEq], ' ') {
					valueEnd = i
					break
				}
			}
		}

		result[key] = strings.TrimSpace(rest[:valueEnd])
		if valueEnd < len(rest) {
			s = strings.TrimSpace(rest[valueEnd:])
		} else {
			s = ""
		}
	}

	return result, ""
}

// parseSessionUpdate extracts session ID and parsed JSON-RPC params.update
// from a notification message like "session=abc123 {json...}".
func parseSessionUpdate(message string) (sessionID string, update map[string]interface{}, ok bool) {
	kv, jsonStr := parseKeyValues(message)
	sessionID = kv["session"]
	if sessionID == "" || jsonStr == "" {
		return "", nil, false
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &envelope); err != nil {
		return sessionID, nil, false
	}

	// Navigate params.update in the JSON-RPC envelope.
	params, _ := envelope["params"].(map[string]interface{})
	if params == nil {
		// Maybe the message IS the update directly (no envelope).
		update, _ = envelope["update"].(map[string]interface{})
		if update == nil {
			// Try the envelope itself as the update.
			return sessionID, envelope, true
		}
		return sessionID, update, true
	}
	update, _ = params["update"].(map[string]interface{})
	if update == nil {
		return sessionID, params, true
	}
	return sessionID, update, true
}

// render displays a single job event in docker-compose style.
func (d *eventDisplay) render(evt *orchestratorSDK.JobEvent) {
	switch evt.Type {
	case "agent:notification:session/update":
		d.renderSessionUpdate(evt)

	case "agent:jsonrpc_result":
		d.renderJSONRPCResult(evt)

	case "worker:prompt_start":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		role := kv["role"]
		if role == "" {
			role = "worker"
		}
		sessionID := kv["session"]
		if sessionID != "" {
			d.registerSession(sessionID, role)
		}
		task := kv["task"]
		fmt.Fprintf(d.w, "%sStarting: %s\n", d.prefix(role), task)

	case "worker:prompt_complete":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		role := kv["role"]
		if role == "" {
			role = "worker"
		}
		task := kv["task"]
		fmt.Fprintf(d.w, "%sFinished: %s\n", d.prefix(role), task)

	case "worker:session_spawn":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		role := kv["role"]
		if role == "" {
			role = "worker"
		}
		sessionID := kv["session"]
		if sessionID != "" {
			d.registerSession(sessionID, role)
		}
		fmt.Fprintf(d.w, "%sSpawning session\n", d.prefix(role))

	case "worker:session_spawned":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		role := kv["role"]
		if role == "" {
			role = "worker"
		}
		sessionID := kv["session"]
		if sessionID != "" {
			d.registerSession(sessionID, role)
		}
		fmt.Fprintf(d.w, "%sSession ready\n", d.prefix(role))

	case "lead:decompose_start":
		d.endStreaming()
		d.registerSession("lead", "lead")
		fmt.Fprintf(d.w, "%sDecomposing task...\n", d.prefix("lead"))

	case "lead:work_item":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		title := kv["title"]
		fmt.Fprintf(d.w, "%s-> %s\n", d.prefix("lead"), title)

	case "lead:session_spawned":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		sessionID := kv["session"]
		if sessionID != "" {
			d.registerSession(sessionID, "lead")
		}
		fmt.Fprintf(d.w, "%sSession ready\n", d.prefix("lead"))

	case "orchestration:job_started":
		d.endStreaming()
		fmt.Fprintf(d.w, "%sJob started\n", d.prefix("system"))

	case "orchestration:decomposed":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		items := kv["work_items"]
		iteration := kv["iteration"]
		fmt.Fprintf(d.w, "%sDecomposed into %s tasks (iteration %s)\n", d.prefix("system"), items, iteration)

	case "orchestration:scheduled":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		items := kv["items"]
		waves := kv["waves"]
		fmt.Fprintf(d.w, "%sScheduled %s items in %s waves\n", d.prefix("system"), items, waves)

	case "orchestration:task_assigned":
		// Skip -- too noisy, prompt_start covers this.
		return

	case "orchestration:task_completed":
		// Skip -- prompt_complete covers this.
		return

	case "orchestration:review_decision":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		approved := kv["approved"]
		feedback := kv["feedback"]
		if approved == "true" {
			fmt.Fprintf(d.w, "%sReview: APPROVED\n", d.prefix("system"))
		} else {
			fmt.Fprintf(d.w, "%sReview: REJECTED -- %s\n", d.prefix("system"), feedback)
		}

	case "orchestration:redelegate":
		d.endStreaming()
		kv, _ := parseKeyValues(evt.Message)
		iteration := kv["iteration"]
		fmt.Fprintf(d.w, "%sRe-delegating (iteration %s)\n", d.prefix("system"), iteration)

	case "orchestration:validation_result":
		// Skip -- internal detail.
		return

	default:
		d.endStreaming()
		prefix := d.prefix("system")
		if evt.Message != "" {
			fmt.Fprintf(d.w, "%s[%s] %s\n", prefix, evt.Type, evt.Message)
		} else {
			fmt.Fprintf(d.w, "%s[%s]\n", prefix, evt.Type)
		}
	}
}

// renderSessionUpdate handles agent:notification:session/update events.
func (d *eventDisplay) renderSessionUpdate(evt *orchestratorSDK.JobEvent) {
	sessionID, update, ok := parseSessionUpdate(evt.Message)
	if !ok {
		// Fallback: try to parse the message as direct JSON.
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(evt.Message), &payload); err != nil {
			d.endStreaming()
			role := d.roleFor(sessionID)
			fmt.Fprintf(d.w, "%s%s\n", d.prefix(role), evt.Message)
			return
		}
		update = payload
		// Try to get session from metadata.
		if evt.Metadata != nil {
			sessionID = evt.Metadata["session_id"]
		}
	}

	role := d.roleFor(sessionID)
	sessionUpdate, _ := extractMapString(update, "sessionUpdate")

	switch sessionUpdate {
	case "tool_call", "tool_call_update":
		d.renderToolCall(role, update)
	case "agent_message_chunk":
		d.renderMessageChunk(role, update)
	case "agent_thought_chunk":
		d.renderThoughtChunk(role, update)
	case "available_commands_update":
		// Skip silently.
		return
	default:
		// For unknown update types, check for direct type field (flat JSON).
		directType, _ := extractMapString(update, "type")
		switch directType {
		case "tool_call", "tool_call_update":
			d.renderToolCall(role, update)
		case "agent_message_chunk":
			d.renderMessageChunk(role, update)
		case "agent_thought_chunk":
			d.renderThoughtChunk(role, update)
		default:
			// Unknown session update; show raw.
			if sessionUpdate != "" {
				d.endStreaming()
				fmt.Fprintf(d.w, "%s[%s] %s\n", d.prefix(role), sessionUpdate, evt.Message)
			}
		}
	}
}

// renderToolCall formats tool call events like [WRITE] /path (status).
func (d *eventDisplay) renderToolCall(role string, update map[string]interface{}) {
	status, _ := extractMapString(update, "status")
	// Skip pending status to reduce noise.
	if status == "pending" {
		return
	}

	d.endStreaming()

	title, _ := extractMapString(update, "title")
	tag := toolTag(title)

	// Extract first location path.
	path := extractFirstLocationPath(update)

	if status == "failed" {
		errMsg := extractToolError(update)
		desc := title
		if path != "" {
			desc = title + " " + path
		}
		if errMsg != "" {
			fmt.Fprintf(d.w, "%s\033[91m[FAIL]  %s -- %s%s\n", d.prefix(role), desc, errMsg, colorReset)
		} else {
			fmt.Fprintf(d.w, "%s\033[91m[FAIL]  %s%s\n", d.prefix(role), desc, colorReset)
		}
		return
	}

	if path != "" {
		fmt.Fprintf(d.w, "%s[%s] %s (%s)\n", d.prefix(role), tag, path, status)
	} else if title != "" {
		fmt.Fprintf(d.w, "%s[%s] (%s)\n", d.prefix(role), tag, status)
	}
}

// renderMessageChunk handles streaming agent message text.
func (d *eventDisplay) renderMessageChunk(role string, update map[string]interface{}) {
	text := extractChunkText(update)
	if text == "" {
		return
	}

	if !d.streaming || d.streamingRole != role {
		d.endStreaming()
		fmt.Fprintf(d.w, "%s>>> ", d.prefix(role))
		d.streaming = true
		d.streamingRole = role
	}
	fmt.Fprint(d.w, text)
}

// renderThoughtChunk handles streaming agent thought text.
func (d *eventDisplay) renderThoughtChunk(role string, update map[string]interface{}) {
	text := extractChunkText(update)
	if text == "" {
		return
	}

	if !d.streaming || d.streamingRole != role+"_thought" {
		d.endStreaming()
		fmt.Fprintf(d.w, "%s%s... ", d.prefix(role), colorDim)
		d.streaming = true
		d.streamingRole = role + "_thought"
	}
	fmt.Fprintf(d.w, "%s%s", colorDim, text)
}

// renderJSONRPCResult handles agent:jsonrpc_result events.
func (d *eventDisplay) renderJSONRPCResult(evt *orchestratorSDK.JobEvent) {
	d.endStreaming()

	// The message may be prefixed with "session=<id> {json}".
	kv, jsonStr := parseKeyValues(evt.Message)
	sessionID := kv["session"]
	role := d.roleFor(sessionID)

	if jsonStr == "" {
		// Try parsing the entire message as JSON.
		jsonStr = evt.Message
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		fmt.Fprintf(d.w, "%sDone\n", d.prefix(role))
		return
	}

	usage, _ := payload["usage"].(map[string]interface{})
	if usage != nil {
		if total, ok := usage["totalTokens"].(float64); ok {
			fmt.Fprintf(d.w, "%sDone (%.0f tokens)\n", d.prefix(role), total)
			return
		}
	}

	// Check result-level usage.
	result, _ := payload["result"].(map[string]interface{})
	if result != nil {
		usage, _ = result["usage"].(map[string]interface{})
		if usage != nil {
			if total, ok := usage["totalTokens"].(float64); ok {
				fmt.Fprintf(d.w, "%sDone (%.0f tokens)\n", d.prefix(role), total)
				return
			}
		}
	}

	fmt.Fprintf(d.w, "%sDone\n", d.prefix(role))
}

// renderAudit renders a single audit event with timestamp prefix.
func (d *eventDisplay) renderAudit(timestamp, eventType, details string) {
	// Construct a synthetic JobEvent and render through the same logic,
	// then prepend the timestamp.
	synth := &orchestratorSDK.JobEvent{
		Type:    eventType,
		Message: details,
	}

	// Capture the rendered output to prepend timestamp.
	var buf strings.Builder
	origW := d.w
	d.w = &buf
	d.render(synth)
	d.w = origW

	output := buf.String()
	if output == "" {
		return
	}

	// Prepend timestamp to each line of output.
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	for _, line := range lines {
		fmt.Fprintf(origW, "%s %s\n", timestamp, line)
	}
}

// stateChange renders a state transition line.
func (d *eventDisplay) stateChange(state string, iteration, tasks, completed, failed int) {
	d.endStreaming()
	fmt.Fprintf(d.w, "%sState: %s (iteration=%d tasks=%d completed=%d failed=%d)\n",
		d.prefix("system"), state, iteration, tasks, completed, failed)
}

// toolTag returns an uppercase tag for the tool title.
func toolTag(title string) string {
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(lower, "write"):
		return "WRITE"
	case strings.Contains(lower, "read"):
		return "READ"
	case strings.Contains(lower, "bash"):
		return "BASH"
	case strings.Contains(lower, "edit"):
		return "EDIT"
	case strings.Contains(lower, "glob"):
		return "GLOB"
	case strings.Contains(lower, "grep"):
		return "GREP"
	case strings.Contains(lower, "list"):
		return "LIST"
	default:
		return "TOOL"
	}
}

// extractFirstLocationPath pulls the first path from locations array.
func extractFirstLocationPath(update map[string]interface{}) string {
	locations, _ := update["locations"].([]interface{})
	if len(locations) > 0 {
		loc, _ := locations[0].(map[string]interface{})
		if loc != nil {
			path, _ := loc["path"].(string)
			return path
		}
	}
	return ""
}

// extractToolError extracts error text from rawOutput.error or content[].content.text.
func extractToolError(update map[string]interface{}) string {
	// Try rawOutput.error first.
	rawOutput, _ := update["rawOutput"].(map[string]interface{})
	if rawOutput != nil {
		errStr, _ := rawOutput["error"].(string)
		if errStr != "" {
			return errStr
		}
	}

	// Try content array.
	content, _ := update["content"].([]interface{})
	for _, c := range content {
		cm, _ := c.(map[string]interface{})
		if cm == nil {
			continue
		}
		inner, _ := cm["content"].(map[string]interface{})
		if inner != nil {
			text, _ := inner["text"].(string)
			if text != "" {
				return text
			}
		}
		// Direct text field.
		text, _ := cm["text"].(string)
		if text != "" {
			return text
		}
	}

	return ""
}

// extractChunkText extracts text from a streaming chunk update.
// Handles both flat "text" field and nested content[].content.text.
func extractChunkText(update map[string]interface{}) string {
	// Direct text field.
	if text, ok := update["text"].(string); ok && text != "" {
		return text
	}

	// Nested content.text.
	content, _ := update["content"].(map[string]interface{})
	if content != nil {
		if text, ok := content["text"].(string); ok {
			return text
		}
	}

	// Array content[].content.text.
	contentArr, _ := update["content"].([]interface{})
	for _, c := range contentArr {
		cm, _ := c.(map[string]interface{})
		if cm == nil {
			continue
		}
		inner, _ := cm["content"].(map[string]interface{})
		if inner != nil {
			if text, ok := inner["text"].(string); ok {
				return text
			}
		}
		if text, ok := cm["text"].(string); ok {
			return text
		}
	}

	return ""
}

// extractMapString safely retrieves a string field from a JSON object map.
func extractMapString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
