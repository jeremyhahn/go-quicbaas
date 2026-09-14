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
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// TestSerenaMCPConfig_PointsAtTheCheckoutROOT is the regression guard for the
// spelling that looks right and silently does nothing.
//
// `--project-from-cwd` makes serena walk up from the working directory looking
// for a `.git`. The working directory is WorkspaceCheckoutRoot, which HOLDS the
// clones rather than being one — a worker gets the project repository and every
// context repository side by side beneath it — so there is no `.git` at that
// level, serena activates no project at all, and every symbol query fails
// against a server that started perfectly. Measured on the burn-in node's own
// workspace: `/workspace` contained `go-truststrap/` and `handbook/`, and no
// `.git`.
func TestSerenaMCPConfig_PointsAtTheCheckoutRoot(t *testing.T) {
	t.Parallel()
	args := serenaServerArgs(serenaContextClaudeCode)
	if slices.Contains(args, "--project-from-cwd") {
		t.Fatal("--project-from-cwd activates nothing: the working directory holds the clones, it is not one of them")
	}
	i := slices.Index(args, "--project")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("args = %v, want --project with a value", args)
	}
	if args[i+1] != WorkspaceCheckoutRoot {
		t.Errorf("--project = %q, want the checkout root %q; a second spelling of this path is a second thing to keep in step",
			args[i+1], WorkspaceCheckoutRoot)
	}
}

// TestSerenaMCPConfig_ClaudeGetsInlineJSONNamingTheRoot: Claude Code takes
// "JSON files or strings", and the string is built here so the configuration and
// the checkout root cannot disagree — which they could when the JSON lived in a
// Dockerfile heredoc no test could read.
func TestSerenaMCPConfig_ClaudeGetsInlineJSONNamingTheRoot(t *testing.T) {
	t.Parallel()
	var cfg struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	raw := SerenaClaudeMCPConfig()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("the inline config is not valid JSON: %v\n%s", err, raw)
	}
	srv, ok := cfg.MCPServers["serena"]
	if !ok {
		t.Fatalf("no serena server in %s", raw)
	}
	if srv.Command != serenaCommand || srv.Type != "stdio" {
		t.Errorf("server = %+v, want the %q command over stdio", srv, serenaCommand)
	}
	if !slices.Contains(srv.Args, WorkspaceCheckoutRoot) {
		t.Errorf("args = %v, want the checkout root %q", srv.Args, WorkspaceCheckoutRoot)
	}
	if !slices.Contains(srv.Args, serenaContextClaudeCode) {
		t.Errorf("args = %v, want the claude-code context; the other CLI's context advertises tools this one cannot call", srv.Args)
	}
}

// TestSerenaMCPConfig_CodexGetsTOMLOverridesNamingTheRoot: codex has no
// --mcp-config, only `-c <dotted.key>=<TOML>`, and parses each value as TOML. An
// array of quoted strings is valid in both TOML and JSON, which is why the
// marshalled args can be passed through as they stand — this test is what keeps
// that true if the args ever grow a value where the two disagree.
func TestSerenaMCPConfig_CodexGetsTOMLOverridesNamingTheRoot(t *testing.T) {
	t.Parallel()
	args := SerenaCodexConfigArgs()
	if len(args) != 4 || args[0] != "-c" || args[2] != "-c" {
		t.Fatalf("args = %v, want two -c overrides", args)
	}
	if args[1] != `mcp_servers.serena.command="serena"` {
		t.Errorf("command override = %q", args[1])
	}
	const prefix = "mcp_servers.serena.args="
	if !strings.HasPrefix(args[3], prefix) {
		t.Fatalf("args override = %q, want the %q key", args[3], prefix)
	}
	// The value must be an array of plain quoted strings — the one shape TOML
	// and JSON spell identically.
	var decoded []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(args[3], prefix)), &decoded); err != nil {
		t.Fatalf("the args value is not an array of strings, so TOML and JSON no longer agree: %v", err)
	}
	if !slices.Equal(decoded, serenaServerArgs(serenaContextCodex)) {
		t.Errorf("decoded = %v, want %v", decoded, serenaServerArgs(serenaContextCodex))
	}
	if !slices.Contains(decoded, WorkspaceCheckoutRoot) {
		t.Errorf("decoded = %v, want the checkout root %q", decoded, WorkspaceCheckoutRoot)
	}
}

// TestSerenaMCPConfig_BothCLIsGetTheSameServer: the two renderings exist because
// the CLIs accept different shapes, not because they should behave differently.
// Only the context may differ.
func TestSerenaMCPConfig_BothCLIsGetTheSameServer(t *testing.T) {
	t.Parallel()
	claude := serenaServerArgs(serenaContextClaudeCode)
	codex := serenaServerArgs(serenaContextCodex)
	if len(claude) != len(codex) {
		t.Fatalf("claude = %v, codex = %v; only the context may differ", claude, codex)
	}
	for i := range claude {
		if claude[i] == codex[i] {
			continue
		}
		if claude[i] == serenaContextClaudeCode && codex[i] == serenaContextCodex {
			continue
		}
		t.Errorf("argv %d differs: %q vs %q; only the context may differ", i, claude[i], codex[i])
	}
}
