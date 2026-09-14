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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestScrubAgentEnv_RemovesTheOperatorsSessionAndKeepsTheRest is the contract
// go-quicbaas#218 turns on: an agent subprocess must not inherit the identity
// of the interactive session that started the daemon.
//
// The expectations are written OUT, not derived from agentEnvScrubbed — a test
// that computed them from the same table would pass for an empty table.
func TestScrubAgentEnv_RemovesTheOperatorsSessionAndKeepsTheRest(t *testing.T) {
	environ := []string{
		"HOME=/home/operator",
		"PATH=/usr/bin:/bin",
		"ANTHROPIC_API_KEY=sk-secret",
		"CLAUDECODE=1",
		"CLAUDE_CODE_CHILD_SESSION=1",
		"CLAUDE_CODE_SESSION_ID=60cffd34-c4f9-494c-8ef7-b2833761304e",
		"CLAUDE_CODE_MESSAGING_SOCKET=/run/user/1000/cc.sock",
		"CLAUDE_CONFIG_DIR=/home/operator/.claude-claude1",
		"CLAUDE_PID=582178",
		"CLAUDE_EFFORT=high",
		"CLAUDE_PLUGIN_DATA=/home/operator/.claude/plugins",
		"CODEX_COMPANION_PORT=7777",
		"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat-secret",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"CLAUDE_CODE_USE_VERTEX=0",
		"CLAUDE_CODE_SKIP_BEDROCK_AUTH=1",
		"CLAUDE_CODE_SKIP_VERTEX_AUTH=1",
		"QUICBAAS_SDLC_ENABLED=true",
		"NOT_A_PAIR",
	}

	want := []string{
		"HOME=/home/operator",
		"PATH=/usr/bin:/bin",
		"ANTHROPIC_API_KEY=sk-secret",
		// Credential and provider selection are configuration of THIS daemon,
		// not descriptions of somebody's session. A worker that lost them does
		// not become isolated, it talks to the wrong provider or cannot
		// authenticate at all.
		"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat-secret",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"CLAUDE_CODE_USE_VERTEX=0",
		"CLAUDE_CODE_SKIP_BEDROCK_AUTH=1",
		"CLAUDE_CODE_SKIP_VERTEX_AUTH=1",
		"QUICBAAS_SDLC_ENABLED=true",
		"NOT_A_PAIR",
	}

	got := scrubAgentEnv(environ)
	if len(got) != len(want) {
		t.Fatalf("scrubAgentEnv kept %d entries %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q (order must be preserved)", i, got[i], want[i])
		}
	}
}

// TestScrubAgentEnv_NamePrefixesDoNotOverreach guards the other direction: the
// prefixes are prefixes of a NAME, and a variable that merely starts with the
// same letters is somebody else's.
func TestScrubAgentEnv_NamePrefixesDoNotOverreach(t *testing.T) {
	for _, keep := range []string{
		"CLAUDE_CODEX=1",        // CLAUDE_CODE_ requires the trailing underscore
		"MY_CLAUDECODE=1",       // an exact name is not a substring match
		"CODEX_COMPANIONSHIP=1", // CODEX_COMPANION_ requires the trailing underscore
		"CLAUDE_CONFIG_DIRS=/x", // an exact name is not a prefix
	} {
		got := scrubAgentEnv([]string{keep})
		if len(got) != 1 || got[0] != keep {
			t.Errorf("scrubAgentEnv(%q) = %v, want it kept", keep, got)
		}
	}
}

// TestDefaultProcessFactory_ScrubsEvenWithNothingToAdd is the half a
// map-of-extra-variables cannot express. The fault in #218 was a variable that
// had to be REMOVED, and the factory used to leave cmd.Env nil — inheriting
// everything — whenever the request carried no variables of its own.
func TestDefaultProcessFactory_ScrubsEvenWithNothingToAdd(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "operator-session")
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/operator/.claude-claude1")

	cmd, err := defaultProcessFactory(t.Context(), "true", nil, "", nil)
	if err != nil {
		t.Fatalf("defaultProcessFactory: %v", err)
	}
	if cmd.Env == nil {
		t.Fatal("cmd.Env is nil, so the child inherits os.Environ() verbatim — the fault #218 reports")
	}
	for _, entry := range cmd.Env {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CONFIG_DIR":
			t.Errorf("%s reached the child environment", name)
		}
	}
	if !hasEnvName(cmd.Env, "PATH") {
		t.Error("PATH was scrubbed; the CLI cannot be found without it")
	}
}

// TestDefaultProcessFactory_RequestEnvWinsOverTheScrub proves the positive half
// of the contract: the SDLC module can supply its own value for a name it just took
// away, and exec's last-wins duplicate handling gives that value to the child.
func TestDefaultProcessFactory_RequestEnvWinsOverTheScrub(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/operator/.claude-claude1")

	cmd, err := defaultProcessFactory(t.Context(), "true", nil, "",
		[]string{"CLAUDE_CONFIG_DIR=/var/lib/quicbaas/sdlc/claude-config"})
	if err != nil {
		t.Fatalf("defaultProcessFactory: %v", err)
	}
	var seen []string
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			seen = append(seen, entry)
		}
	}
	if len(seen) != 1 || seen[0] != "CLAUDE_CONFIG_DIR=/var/lib/quicbaas/sdlc/claude-config" {
		t.Fatalf("CLAUDE_CONFIG_DIR entries = %v, want only the SDLC module's", seen)
	}
	// The environment as exec itself resolves it, so the assertion is about
	// the value the child actually gets rather than about the slice.
	resolved, err := resolvedEnv(cmd)
	if err != nil {
		t.Fatalf("resolving the environment: %v", err)
	}
	if resolved["CLAUDE_CONFIG_DIR"] != "/var/lib/quicbaas/sdlc/claude-config" {
		t.Errorf("child CLAUDE_CONFIG_DIR = %q, want the SDLC module's",
			resolved["CLAUDE_CONFIG_DIR"])
	}
}

// hasEnvName reports whether environ carries a NAME=... entry.
func hasEnvName(environ []string, name string) bool {
	for _, entry := range environ {
		if got, _, ok := strings.Cut(entry, "="); ok && got == name {
			return true
		}
	}
	return false
}

// resolvedEnv runs the command's environment through exec's own duplicate
// handling by starting a process with it and reading what that process saw.
func resolvedEnv(cmd *exec.Cmd) (map[string]string, error) {
	probe := exec.Command(os.Args[0], "-test.run=TestScrubAgentEnv_NoSuchTest")
	probe.Env = cmd.Env
	out := map[string]string{}
	// exec.Cmd.Environ() applies the same dedup rules Start does.
	for _, entry := range probe.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok {
			out[name] = value
		}
	}
	return out, nil
}

// TestAgentProcessEnv_IsTheOneMergeEveryExecSiteUses pins the shared contract:
// the daemon's environment minus the session that started it, plus the
// caller's own variables last.
func TestAgentProcessEnv_IsTheOneMergeEveryExecSiteUses(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_MESSAGING_SOCKET", "/run/user/1000/cc.sock")

	got := agentProcessEnv([]string{"QUICBAAS_ACCOUNT_ID=acct-a"})
	if hasEnvName(got, "CLAUDECODE") || hasEnvName(got, "CLAUDE_CODE_MESSAGING_SOCKET") {
		t.Error("the operator's session variables survived the merge")
	}
	if !hasEnvName(got, "PATH") || !hasEnvName(got, "HOME") {
		t.Error("PATH or HOME was dropped; the CLI needs both to run at all")
	}
	if got[len(got)-1] != "QUICBAAS_ACCOUNT_ID=acct-a" {
		t.Errorf("last entry = %q, want the caller's variable; exec resolves duplicates to the last",
			got[len(got)-1])
	}
}

// TestNewStdioTransport_ScrubsTheInheritedSession runs a REAL subprocess and
// reads the environment it actually saw.
//
// The JSON-RPC path had its own env merge that only ran when the caller
// supplied variables, so a spawn with none inherited everything — the same
// shape of fault as the CLI factory's, in a second place. Asserting on the
// child's own view is what makes this test independent of that merge's code.
func TestNewStdioTransport_ScrubsTheInheritedSession(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", "/home/operator/.claude-claude1")

	// A FIFO rather than a regular file polled for content: opening the read
	// end BLOCKS until the child opens the write end, so the handoff is a
	// synchronisation primitive and the test never sleeps (CLAUDE.md).
	fifo := filepath.Join(t.TempDir(), "env.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	// Writes what the child inherited, then holds stdin open so the transport
	// has a live process to close.
	transport, err := NewStdioTransport("sh",
		[]string{"-c", "env > " + fifo + "; cat"}, "", nil)
	if err != nil {
		t.Fatalf("NewStdioTransport: %v", err)
	}
	defer func() { _ = transport.Close() }()

	type readResult struct {
		body []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		body, readErr := os.ReadFile(fifo)
		done <- readResult{body: body, err: readErr}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var got readResult
	select {
	case got = <-done:
	case <-ctx.Done():
		t.Fatal("the child never wrote its environment")
	}
	if got.err != nil || len(got.body) == 0 {
		t.Fatalf("the child wrote no environment: %v", got.err)
	}
	child := string(got.body)
	for _, name := range []string{"CLAUDECODE=", "CLAUDE_CONFIG_DIR="} {
		if strings.Contains(child, name) {
			t.Errorf("%s reached the JSON-RPC child process", name)
		}
	}
	if !strings.Contains(child, "PATH=") {
		t.Error("PATH did not reach the child")
	}
}

// TestSpawnCLI_ExtraArgsReachTheArgv is the argv assertion go-quicbaas#218
// turns on, at the layer that actually builds the command line.
//
// The burn-in captured the worker's argv from /proc and found no
// --permission-mode in it. This runs the real spawn through the real argv
// builder and reads the arguments the process factory was handed.
func TestSpawnCLI_ExtraArgsReachTheArgv(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")
	inner := backend.processFactory
	var argv []string
	backend.processFactory = func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		argv = append([]string(nil), args...)
		return inner(ctx, command, args, workDir, env)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := backend.Spawn(ctx, &SpawnRequest{
		ProjectID:    "proj",
		WorkDir:      t.TempDir(),
		Model:        "claude-sonnet-4-5-20250514",
		SystemPrompt: "charter",
		Persistent:   true,
		ExtraArgs:    []string{"--permission-mode", "acceptEdits"},
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if !argvHasPair(argv, "--permission-mode", "acceptEdits") {
		t.Fatalf("argv = %v, want the permission grant the caller stated", argv)
	}
	for _, arg := range argv {
		if arg == "--dangerously-skip-permissions" {
			t.Error("argv grants Bash; the ExecutionGuard allowlist is the authority for that")
		}
	}
}

// argvHasPair reports whether argv carries flag immediately followed by value.
func argvHasPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}
