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

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// newWorkerSettingsTestServer builds a server carrying only the orchestration
// configuration the settings renderer reads, with its log captured.
func newWorkerSettingsTestServer(t *testing.T, orch config.OrchestrationConfig) (*Server, *bytes.Buffer) {
	t.Helper()
	srv := &Server{config: &config.Config{Orchestration: orch}}
	return srv, captureServerLog(t, srv)
}

// readRenderedAllow returns the allow list of the settings file in dir.
func readRenderedAllow(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, orchestration.WorkerSettingsFile))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings are not JSON: %v", err)
	}
	return settings.Permissions.Allow
}

// TestRenderWorkerSettings_BootRendersFromConfig: boot writes the settings
// file from configuration, every verification command exactly once, and a
// first write is not warned about.
func TestRenderWorkerSettings_BootRendersFromConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents")
	srv, logs := newWorkerSettingsTestServer(t, config.OrchestrationConfig{
		AgentConfigDir:       dir,
		AgentShellAllow:      []string{"go", "make test"},
		VerificationCommands: []string{"make test", "make lint"},
	})
	if err := srv.renderWorkerSettings(); err != nil {
		t.Fatalf("renderWorkerSettings: %v", err)
	}
	allow := readRenderedAllow(t, dir)
	want := []string{"Bash(go)", "Bash(go *)", "Bash(make test)", "Bash(make test *)", "Bash(make lint)"}
	if !slices.Equal(allow, want) {
		t.Fatalf("allow = %q, want %q", allow, want)
	}
	if strings.Contains(logs.String(), "orchestration.agent_shell_allow") {
		t.Fatalf("a first write was warned about: %s", logs.String())
	}
}

// TestRenderWorkerSettings_StaleFileIsRegeneratedWithAWarning: a hand-edited
// file is overwritten and the WARN names the configuration key to use instead.
func TestRenderWorkerSettings_StaleFileIsRegeneratedWithAWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, orchestration.WorkerSettingsFile)
	if err := os.WriteFile(path, []byte(`{"permissions":{"allow":["Bash(*)"]}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv, logs := newWorkerSettingsTestServer(t, config.OrchestrationConfig{
		AgentConfigDir:       dir,
		VerificationCommands: []string{"make build"},
	})
	if err := srv.renderWorkerSettings(); err != nil {
		t.Fatalf("renderWorkerSettings: %v", err)
	}
	if allow := readRenderedAllow(t, dir); !slices.Equal(allow, []string{"Bash(make build)"}) {
		t.Fatalf("allow = %q, want only the verification command", allow)
	}
	var warned bool
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"level":"WARN"`) && strings.Contains(line, "orchestration.agent_shell_allow") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("no WARN naming orchestration.agent_shell_allow: %s", logs.String())
	}
}

// TestRenderWorkerSettings_UnsetConfigDirWritesNothing: without an engine
// owned directory there is nowhere the engine may write.
func TestRenderWorkerSettings_UnsetConfigDirWritesNothing(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	srv, _ := newWorkerSettingsTestServer(t, config.OrchestrationConfig{
		AgentShellAllow:      []string{"go"},
		VerificationCommands: []string{"make test"},
	})
	if err := srv.renderWorkerSettings(); err != nil {
		t.Fatalf("renderWorkerSettings: %v", err)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unset agent_config_dir wrote %d entries", len(entries))
	}
}

// TestRenderWorkerSettings_WriteFailureFailsBoot: a directory the daemon
// cannot write is a boot error, not a worker that silently has no shell.
func TestRenderWorkerSettings_WriteFailureFailsBoot(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv, _ := newWorkerSettingsTestServer(t, config.OrchestrationConfig{AgentConfigDir: notADir})
	if err := srv.renderWorkerSettings(); !errors.Is(err, orchestration.ErrWorkerSettingsWrite) {
		t.Fatalf("err = %v, want ErrWorkerSettingsWrite", err)
	}
}
