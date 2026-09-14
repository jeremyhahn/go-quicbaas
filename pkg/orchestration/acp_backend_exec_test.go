// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strings"
	"testing"
)

// codexRealStream is a VERBATIM capture from codex-cli 0.154.0 run as
// `codex exec --json --sandbox danger-full-access --skip-git-repo-check -`
// inside the Software Factory worker image (2026-09-13). The parser is written
// against this rather than against a guess at the schema, which is the whole
// reason the transport works.
const codexRealStream = `{"type":"thread.started","thread_id":"01a09c13-a057-7ad3-b4c7-274cbc93c4d0"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}
{"type":"turn.completed","usage":{"input_tokens":15233,"cached_input_tokens":12160,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}`

func TestParseExecEvents_RealCodexStream(t *testing.T) {
	t.Parallel()
	turn, err := parseExecEvents(codexRealStream)
	if err != nil {
		t.Fatalf("parseExecEvents: %v", err)
	}
	if turn.text != "OK" {
		t.Errorf("text = %q, want %q", turn.text, "OK")
	}
	if turn.stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn", turn.stopReason)
	}
	if turn.usage == nil {
		t.Fatal("usage = nil; the turn.completed event carried accounting and it must be attributed")
	}
	if turn.usage.InputTokens != 15233 || turn.usage.OutputTokens != 5 {
		t.Errorf("usage in/out = %d/%d, want 15233/5", turn.usage.InputTokens, turn.usage.OutputTokens)
	}
	// cached_input_tokens is a CACHE READ, not additional input: attributing it
	// as input would double-count every cached turn.
	if turn.usage.CacheReadInputTokens != 12160 {
		t.Errorf("CacheReadInputTokens = %d, want 12160", turn.usage.CacheReadInputTokens)
	}
}

func TestParseExecEvents_LastAgentMessageWins(t *testing.T) {
	t.Parallel()
	stream := `{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"first thought"}}
{"type":"item.completed","item":{"id":"i1","type":"reasoning","text":"ignored"}}
{"type":"item.completed","item":{"id":"i2","type":"agent_message","text":"final answer"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}`
	turn, err := parseExecEvents(stream)
	if err != nil {
		t.Fatalf("parseExecEvents: %v", err)
	}
	if turn.text != "final answer" {
		t.Errorf("text = %q; a turn that revises itself is answered by its LAST agent message", turn.text)
	}
}

func TestParseExecEvents_ToleratesNoiseAndUnknownEvents(t *testing.T) {
	t.Parallel()
	// A banner, a bare warning line, an event type this build has never seen,
	// and a truncated JSON line. None of them is a parse failure: a CLI that
	// adds an event must not turn a working turn into an error.
	stream := `Codex CLI v0.154.0
warning: something cosmetic
{"type":"thread.started","thread_id":"t"}
{"type":"some.future.event","payload":{"a":1}}
{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"answer"}}
{"type":"turn.completed","usage":{"in
{"type":"turn.completed","usage":{"input_tokens":3,"output_tokens":4}}`
	turn, err := parseExecEvents(stream)
	if err != nil {
		t.Fatalf("parseExecEvents: %v", err)
	}
	if turn.text != "answer" {
		t.Errorf("text = %q, want answer", turn.text)
	}
	if turn.usage == nil || turn.usage.OutputTokens != 4 {
		t.Errorf("usage = %+v; the well-formed completion must still be read", turn.usage)
	}
}

func TestParseExecEvents_EmptyStreamIsNoTurn(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "\n", "not json at all\n"} {
		turn, err := parseExecEvents(in)
		if err != nil {
			t.Fatalf("parseExecEvents(%q): %v", in, err)
		}
		if turn.text != "" {
			t.Errorf("parseExecEvents(%q).text = %q, want empty", in, turn.text)
		}
	}
}

func TestParseExecEvents_TurnFailedIsAnErrorStopReason(t *testing.T) {
	t.Parallel()
	turn, err := parseExecEvents(`{"type":"turn.started"}
{"type":"turn.failed","error":"model refused"}`)
	if err != nil {
		t.Fatalf("parseExecEvents: %v", err)
	}
	if turn.stopReason != "error" {
		t.Errorf("stopReason = %q, want error", turn.stopReason)
	}
	if turn.text != "" {
		t.Errorf("text = %q, want empty for a failed turn", turn.text)
	}
}

func TestACPTransport_SelectorHonoursCLIMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  ACPBackendConfig
		want ACPTransport
	}{
		{"zero value is jsonrpc", ACPBackendConfig{}, ACPTransportJSONRPC},
		{"CLIMode means stream-json", ACPBackendConfig{CLIMode: true}, ACPTransportStreamJSON},
		{"explicit exec", ACPBackendConfig{Transport: ACPTransportExec}, ACPTransportExec},
		{"explicit stream-json", ACPBackendConfig{Transport: ACPTransportStreamJSON}, ACPTransportStreamJSON},
		// A caller mid-migration that sets both gets the boolean's meaning,
		// which is the older and narrower statement.
		{"CLIMode wins over exec", ACPBackendConfig{CLIMode: true, Transport: ACPTransportExec}, ACPTransportStreamJSON},
		{"unknown transport falls back to jsonrpc", ACPBackendConfig{Transport: ACPTransport("wat")}, ACPTransportJSONRPC},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.cfg.transport(); got != tc.want {
				t.Errorf("transport() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExecTurnArgs_ShapeAndPromptStaysOffArgv(t *testing.T) {
	t.Parallel()
	cfg := ACPBackendConfig{
		ExecArgs:      []string{"exec", "--json", "--skip-git-repo-check"},
		ExecModelFlag: "--model",
		ExecStdinArg:  "-",
	}
	got := cfg.ExecTurnArgs("gpt-5.5", []string{"--extra"})
	want := []string{"exec", "--json", "--skip-git-repo-check", "--model", "gpt-5.5", "--extra", "-"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	// No model and no flag must not leave a dangling argument.
	bare := ACPBackendConfig{ExecArgs: []string{"exec"}}.ExecTurnArgs("", nil)
	if strings.Join(bare, " ") != "exec" {
		t.Errorf("argv = %v, want just [exec]", bare)
	}
	// A model with no flag to carry it is dropped rather than passed bare,
	// which would land as a positional PROMPT on most CLIs.
	noFlag := ACPBackendConfig{ExecArgs: []string{"exec"}}.ExecTurnArgs("gpt-5.5", nil)
	if strings.Join(noFlag, " ") != "exec" {
		t.Errorf("argv = %v; a model with no ExecModelFlag must not become a positional argument", noFlag)
	}
}

func TestExecTurnInput_SystemPromptIsCarriedOnStdin(t *testing.T) {
	t.Parallel()
	// An exec CLI has no system-prompt flag, so it has to travel in the text.
	got := execTurnInput("BE TERSE", "session default", "do the thing")
	if !strings.Contains(got, "BE TERSE") {
		t.Error("the request's system prompt is missing")
	}
	if strings.Contains(got, "session default") {
		t.Error("the session default must not win over the request's own system prompt")
	}
	if !strings.HasSuffix(got, "do the thing") {
		t.Errorf("the prompt must come last, got %q", got)
	}
	// Falls back to the session's.
	if !strings.Contains(execTurnInput("", "session default", "p"), "session default") {
		t.Error("the session system prompt must be used when the request names none")
	}
	// Nothing to add means the prompt is passed through untouched, so a CLI
	// that is sensitive to leading content sees exactly what the caller sent.
	if execTurnInput("", "", "just this") != "just this" {
		t.Error("with no system prompt the input must be the prompt verbatim")
	}
}

func TestCredentialDirSpec_ConfiguredNeedsBothPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		spec CredentialDirSpec
		want bool
	}{
		{CredentialDirSpec{}, false},
		{CredentialDirSpec{Rel: ".codex"}, false},
		{CredentialDirSpec{Target: "/home/agent/.codex"}, false},
		{CredentialDirSpec{Rel: ".codex", Target: "/home/agent/.codex"}, true},
	}
	for _, tc := range cases {
		if got := tc.spec.configured(); got != tc.want {
			t.Errorf("configured(%+v) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}

func TestCredentialDirSpec_ReadOnlyIsTheDefaultAndWritableIsOptIn(t *testing.T) {
	t.Parallel()
	// A credential the agent cannot alter is the default, and must stay so:
	// nothing should become writable by forgetting a field.
	if (CredentialDirSpec{Rel: ".config/opencode", Target: "/x"}).Writable {
		t.Error("Writable must default to false")
	}
	// codex refuses to start against a read-only CODEX_HOME, so its spec opts
	// in explicitly. See the field's doc comment for the measured error.
	codex := CredentialDirSpec{Rel: ".codex", Target: "/home/agent/.codex", HomeEnv: "CODEX_HOME", Writable: true}
	if !codex.configured() || !codex.Writable || codex.HomeEnv != "CODEX_HOME" {
		t.Errorf("codex spec = %+v; want configured, writable, CODEX_HOME", codex)
	}
}
