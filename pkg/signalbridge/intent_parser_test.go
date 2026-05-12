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
	"testing"
)

// TestIntentParser_Grammar exercises every positive grammar branch plus
// the common misspellings and error cases. Table-driven so new commands
// plug in by adding a row.
func TestIntentParser_Grammar(t *testing.T) {
	p := NewIntentParser()

	tests := []struct {
		name     string
		input    string
		attached bool
		wantKind IntentKind
		check    func(t *testing.T, got Intent)
	}{
		// spawn variants
		{name: "spawn basic", input: "spawn claude", wantKind: IntentSpawn,
			check: func(t *testing.T, g Intent) { assertEq(t, "claude", g.Backend) }},
		{name: "spawn with host", input: "spawn claude on node-1", wantKind: IntentSpawn,
			check: func(t *testing.T, g Intent) {
				assertEq(t, "claude", g.Backend)
				assertEq(t, "node-1", g.Host)
			}},
		{name: "spawn with model", input: "spawn claude with opus-4", wantKind: IntentSpawn,
			check: func(t *testing.T, g Intent) { assertEq(t, "opus-4", g.Model) }},
		{name: "spawn with host and model", input: "spawn codex on host-a with gpt-4", wantKind: IntentSpawn,
			check: func(t *testing.T, g Intent) {
				assertEq(t, "host-a", g.Host)
				assertEq(t, "gpt-4", g.Model)
			}},
		{name: "spawn with goal", input: "spawn claude for implement auth middleware", wantKind: IntentSpawn,
			check: func(t *testing.T, g Intent) { assertEq(t, "implement auth middleware", g.Goal) }},
		{name: "spawn alias start", input: "start claude", wantKind: IntentSpawn},
		{name: "spawn missing backend", input: "spawn", wantKind: IntentUnknown},

		// attach
		{name: "attach ok", input: "attach sess-123", wantKind: IntentAttach,
			check: func(t *testing.T, g Intent) { assertEq(t, "sess-123", g.SessionID) }},
		{name: "attach alias connect", input: "connect sess-xyz", wantKind: IntentAttach},
		{name: "attach missing id", input: "attach", wantKind: IntentUnknown},

		// detach
		{name: "detach", input: "detach", wantKind: IntentDetach},
		{name: "detach alias bye", input: "bye", wantKind: IntentDetach},
		{name: "detach alias disconnect", input: "disconnect", wantKind: IntentDetach},

		// prompt
		{name: "prompt explicit", input: "prompt please summarize", wantKind: IntentPrompt,
			check: func(t *testing.T, g Intent) { assertEq(t, "please summarize", g.Prompt) }},
		{name: "prompt alias say", input: "say hi", wantKind: IntentPrompt},
		{name: "prompt alias ask", input: "ask what's up", wantKind: IntentPrompt},

		// implicit prompt when attached
		{name: "implicit prompt attached", input: "make it blue", attached: true, wantKind: IntentPrompt,
			check: func(t *testing.T, g Intent) { assertEq(t, "make it blue", g.Prompt) }},
		{name: "unknown when detached", input: "make it blue", attached: false, wantKind: IntentUnknown},

		// close
		{name: "close", input: "close", wantKind: IntentClose},
		{name: "close alias stop", input: "stop", wantKind: IntentClose},
		{name: "close alias kill", input: "kill", wantKind: IntentClose},
		{name: "close alias quit", input: "quit", wantKind: IntentClose},

		// status, sessions, agents
		{name: "status", input: "status", wantKind: IntentStatus},
		{name: "sessions", input: "sessions", wantKind: IntentSessions},
		{name: "list with no arg = sessions", input: "list", wantKind: IntentSessions},
		{name: "list agents via list", input: "list agents", wantKind: IntentAgents},
		{name: "agents", input: "agents", wantKind: IntentAgents},
		{name: "agents alias", input: "list-agents", wantKind: IntentAgents},

		// workflow
		{name: "workflow ok", input: "workflow nightly-build", wantKind: IntentWorkflow,
			check: func(t *testing.T, g Intent) { assertEq(t, "nightly-build", g.WorkflowName) }},
		{name: "workflow with params", input: "workflow nightly with env=prod region=us-east", wantKind: IntentWorkflow,
			check: func(t *testing.T, g Intent) {
				assertEq(t, "nightly", g.WorkflowName)
				if g.WorkflowParams["env"] != "prod" {
					t.Fatalf("env: got %q", g.WorkflowParams["env"])
				}
			}},
		{name: "workflow missing name", input: "workflow", wantKind: IntentUnknown},

		// help
		{name: "help word", input: "help", wantKind: IntentHelp},
		{name: "help ?", input: "?", wantKind: IntentHelp},
		{name: "help /help", input: "/help", wantKind: IntentHelp},

		// settings
		{name: "settings empty", input: "/settings", wantKind: IntentSettings},
		{name: "settings with kv", input: "/settings stream_tokens=false tool_details=true", wantKind: IntentSettings,
			check: func(t *testing.T, g Intent) {
				if g.Settings["stream_tokens"] != "false" {
					t.Fatalf("stream_tokens: got %q", g.Settings["stream_tokens"])
				}
			}},
		{name: "settings with malformed tokens", input: "/settings foo=bar baz", wantKind: IntentSettings,
			check: func(t *testing.T, g Intent) {
				assertEq(t, "bar", g.Settings["foo"])
				if _, ok := g.Settings["baz"]; ok {
					t.Fatalf("baz should have been skipped")
				}
			}},

		// whitespace edge cases
		{name: "empty input", input: "", wantKind: IntentUnknown},
		{name: "whitespace only", input: "   \t  ", wantKind: IntentUnknown},
		{name: "mixed case command", input: "SpAwN claude", wantKind: IntentSpawn},

		// capitalization preservation in prompt body
		{name: "prompt preserves case", input: "prompt HELLO world", wantKind: IntentPrompt,
			check: func(t *testing.T, g Intent) { assertEq(t, "HELLO world", g.Prompt) }},

		// trailing noise after spawn backend becomes goal
		{name: "spawn trailing = goal", input: "spawn claude write a poem", wantKind: IntentSpawn,
			check: func(t *testing.T, g Intent) { assertEq(t, "write a poem", g.Goal) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Parse(tc.input, tc.attached)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind: got %s, want %s (raw=%q)", got.Kind, tc.wantKind, got.Raw)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

// TestIntentParser_Misspellings covers common operator typos that
// should NOT be silently misclassified — they either route to an
// expected intent alias or fall through as IntentUnknown.
func TestIntentParser_Misspellings(t *testing.T) {
	p := NewIntentParser()
	cases := []struct {
		input string
		want  IntentKind
	}{
		{"stauts", IntentUnknown},
		{"sessinos", IntentUnknown},
		{"atach abc", IntentUnknown},
		{"hlep", IntentUnknown},
		{"Help", IntentHelp},
		{"HELP", IntentHelp},
		{"STOP", IntentClose},
		{"Status", IntentStatus},
	}
	for _, tc := range cases {
		got := p.Parse(tc.input, false)
		if got.Kind != tc.want {
			t.Fatalf("%q: got %s, want %s", tc.input, got.Kind, tc.want)
		}
	}
}

// TestParseKVPairs guards helper semantics.
func TestParseKVPairs(t *testing.T) {
	got := parseKVPairs("a=1 b=two c= =d")
	assertEq(t, "1", got["a"])
	assertEq(t, "two", got["b"])
	if _, ok := got["c"]; ok {
		t.Fatalf("c with trailing = should be skipped: %v", got)
	}
}

// assertEq fails the test unless a==b.
func assertEq(t *testing.T, want, got string) {
	t.Helper()
	if want != got {
		t.Fatalf("got %q, want %q", got, want)
	}
}
