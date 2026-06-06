// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"strings"
	"unicode"
)

// IntentKind discriminates parsed operator commands.
type IntentKind string

const (
	IntentUnknown  IntentKind = "unknown"
	IntentSpawn    IntentKind = "spawn"
	IntentAttach   IntentKind = "attach"
	IntentDetach   IntentKind = "detach"
	IntentPrompt   IntentKind = "prompt"
	IntentClose    IntentKind = "close"
	IntentStatus   IntentKind = "status"
	IntentSessions IntentKind = "sessions"
	IntentAgents   IntentKind = "agents"
	IntentWorkflow IntentKind = "workflow"
	IntentHelp     IntentKind = "help"
	IntentSettings IntentKind = "settings"
)

// Intent is the parsed result of operator input. Fields are present
// only when the associated Kind uses them.
type Intent struct {
	Kind IntentKind
	// Raw is the input text before parsing, retained for error messages.
	Raw string

	// Backend is set for IntentSpawn (e.g. "claude", "codex").
	Backend string
	// Host is set for IntentSpawn when "on <host>" is present.
	Host string
	// Model is set for IntentSpawn when "with <model>" is present.
	Model string
	// Goal is set for IntentSpawn when "for <text>" is present. Used as
	// a system prompt when spawning.
	Goal string

	// SessionID is set for IntentAttach.
	SessionID string

	// Prompt is set for IntentPrompt.
	Prompt string

	// WorkflowName is set for IntentWorkflow.
	WorkflowName string
	// WorkflowParams is set for IntentWorkflow when "with key=val ..."
	// is present.
	WorkflowParams map[string]string

	// Settings is set for IntentSettings, one per "key=value" token.
	Settings map[string]string
}

// IntentParser parses operator commands. The parser is context-aware:
// when a conversation has an attached session, unrecognized input is
// interpreted as an IntentPrompt rather than IntentUnknown.
type IntentParser struct{}

// NewIntentParser constructs a parser.
func NewIntentParser() *IntentParser {
	return &IntentParser{}
}

// Parse tokenizes and classifies input. attached indicates whether the
// originating conversation currently has a session attached.
func (p *IntentParser) Parse(input string, attached bool) Intent {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Intent{Kind: IntentUnknown, Raw: input}
	}

	// /settings handled independently of normal commands.
	if strings.HasPrefix(trimmed, "/settings") {
		return parseSettings(trimmed, input)
	}

	// Lowercase only the first token for keyword matching; keep the
	// rest verbatim so prompt bodies preserve user capitalization.
	firstTok, rest := splitFirstToken(trimmed)
	cmd := strings.ToLower(firstTok)

	switch cmd {
	case "spawn", "start":
		return parseSpawn(rest, input)
	case "attach", "connect":
		return parseAttach(rest, input)
	case "detach", "disconnect", "bye":
		return Intent{Kind: IntentDetach, Raw: input}
	case "prompt", "say", "ask":
		return Intent{Kind: IntentPrompt, Raw: input, Prompt: strings.TrimSpace(rest)}
	case "close", "stop", "kill", "end", "quit":
		return Intent{Kind: IntentClose, Raw: input}
	case "status":
		return Intent{Kind: IntentStatus, Raw: input}
	case "sessions", "list-sessions", "list":
		// "list" with no further arg is sessions; "list agents" is agents.
		if strings.EqualFold(strings.TrimSpace(rest), "agents") {
			return Intent{Kind: IntentAgents, Raw: input}
		}
		return Intent{Kind: IntentSessions, Raw: input}
	case "agents", "list-agents":
		return Intent{Kind: IntentAgents, Raw: input}
	case "workflow", "wf", "run":
		return parseWorkflow(rest, input)
	case "help", "?", "/help":
		return Intent{Kind: IntentHelp, Raw: input}
	}

	// Unknown first token: when attached, treat the whole input as a
	// prompt. This is the ergonomic behavior operators expect from a
	// chat interface.
	if attached {
		return Intent{Kind: IntentPrompt, Raw: input, Prompt: trimmed}
	}
	return Intent{Kind: IntentUnknown, Raw: input}
}

// parseSpawn handles: spawn <backend> [on <host>] [with <model>] [for <goal>]
func parseSpawn(rest, raw string) Intent {
	intent := Intent{Kind: IntentSpawn, Raw: raw}
	backend, rest := splitFirstToken(strings.TrimSpace(rest))
	if backend == "" {
		return Intent{Kind: IntentUnknown, Raw: raw}
	}
	intent.Backend = backend

	// Greedy left-to-right: pull off on/with/for sections.
	rest = strings.TrimSpace(rest)
	for rest != "" {
		tok, after := splitFirstToken(rest)
		after = strings.TrimSpace(after)
		switch strings.ToLower(tok) {
		case "on":
			val, next := splitFirstToken(after)
			intent.Host = val
			rest = strings.TrimSpace(next)
		case "with":
			val, next := splitFirstToken(after)
			intent.Model = val
			rest = strings.TrimSpace(next)
		case "for":
			intent.Goal = after
			rest = ""
		default:
			// Unknown trailing token — treat it as the goal.
			intent.Goal = rest
			rest = ""
		}
	}
	return intent
}

// parseAttach handles: attach <session-id>
func parseAttach(rest, raw string) Intent {
	sessionID := strings.TrimSpace(rest)
	if sessionID == "" {
		return Intent{Kind: IntentUnknown, Raw: raw}
	}
	return Intent{Kind: IntentAttach, Raw: raw, SessionID: sessionID}
}

// parseWorkflow handles: workflow <name> [with k=v k=v...]
func parseWorkflow(rest, raw string) Intent {
	name, after := splitFirstToken(strings.TrimSpace(rest))
	if name == "" {
		return Intent{Kind: IntentUnknown, Raw: raw}
	}
	intent := Intent{Kind: IntentWorkflow, Raw: raw, WorkflowName: name}
	after = strings.TrimSpace(after)
	if strings.HasPrefix(strings.ToLower(after), "with ") {
		after = after[len("with "):]
		intent.WorkflowParams = parseKVPairs(after)
	}
	return intent
}

// parseSettings handles: /settings [k=v ...]
func parseSettings(trimmed, raw string) Intent {
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "/settings"))
	intent := Intent{Kind: IntentSettings, Raw: raw}
	if rest == "" {
		return intent
	}
	intent.Settings = parseKVPairs(rest)
	return intent
}

// splitFirstToken splits on the first run of whitespace, returning
// (first, rest). Both are right-trimmed.
func splitFirstToken(s string) (string, string) {
	s = strings.TrimLeft(s, " \t")
	for i, r := range s {
		if unicode.IsSpace(r) {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// parseKVPairs parses "k1=v1 k2=v2 ..." into a map. Malformed tokens
// (missing '=') are ignored so garbage at the tail of a command line
// doesn't produce an IntentUnknown.
func parseKVPairs(s string) map[string]string {
	out := map[string]string{}
	for _, tok := range strings.Fields(s) {
		eq := strings.IndexByte(tok, '=')
		if eq <= 0 || eq == len(tok)-1 {
			continue
		}
		out[tok[:eq]] = tok[eq+1:]
	}
	return out
}
