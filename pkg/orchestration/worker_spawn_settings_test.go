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
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// readWorkerAllow parses the allow list out of a rendered settings file.
func readWorkerAllow(t *testing.T, data []byte) []string {
	t.Helper()
	var settings workerSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings are not JSON: %v\n%s", err, data)
	}
	return settings.Permissions.Allow
}

// TestRenderWorkerSettings_PrefixesAndExactVerificationCommands pins the rule
// syntax: a prefix allows the bare command and the command with arguments, a
// verification command is one exact rule, and a command named both ways is
// still listed once.
func TestRenderWorkerSettings_PrefixesAndExactVerificationCommands(t *testing.T) {
	t.Parallel()
	data, err := RenderWorkerSettings(
		[]string{"go", "git status", "make test"},
		[]string{"make test", "make lint", "make lint"},
	)
	if err != nil {
		t.Fatalf("RenderWorkerSettings: %v", err)
	}
	want := []string{
		"Bash(go)", "Bash(go *)",
		"Bash(git status)", "Bash(git status *)",
		"Bash(make test)", "Bash(make test *)",
		"Bash(make lint)",
	}
	if got := readWorkerAllow(t, data); !slices.Equal(got, want) {
		t.Fatalf("allow = %q, want %q", got, want)
	}
}

// TestRenderWorkerSettings_EmptyAllowListKeepsOnlyVerification proves the
// default does not widen: with no prefixes the worker may run its
// verification commands and nothing else, and with neither the allow list is
// empty rather than absent.
func TestRenderWorkerSettings_EmptyAllowListKeepsOnlyVerification(t *testing.T) {
	t.Parallel()
	data, err := RenderWorkerSettings(nil, []string{"make build"})
	if err != nil {
		t.Fatalf("RenderWorkerSettings: %v", err)
	}
	if got := readWorkerAllow(t, data); !slices.Equal(got, []string{"Bash(make build)"}) {
		t.Fatalf("allow = %q, want only the verification command", got)
	}

	data, err = RenderWorkerSettings(nil, nil)
	if err != nil {
		t.Fatalf("RenderWorkerSettings: %v", err)
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("settings are not JSON: %v", err)
	}
	if got := string(raw["permissions"]["allow"]); got != "[]" {
		t.Fatalf("an empty allow list rendered as %s, want []", got)
	}
}

// TestWriteWorkerSettings_CreatesTheDirectoryAndFile covers a fresh data dir:
// the directory is created owner-only, the file is written owner-only, and a
// first write is not reported as a replacement.
func TestWriteWorkerSettings_CreatesTheDirectoryAndFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "agents", "claude")
	replaced, err := WriteWorkerSettings(dir, []string{"go"}, []string{"make test"})
	if err != nil {
		t.Fatalf("WriteWorkerSettings: %v", err)
	}
	if replaced {
		t.Fatalf("a first write reported a replacement")
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir mode = %o, want 700", perm)
	}
	path := filepath.Join(dir, WorkerSettingsFile)
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	want := []string{"Bash(go)", "Bash(go *)", "Bash(make test)"}
	if got := readWorkerAllow(t, data); !slices.Equal(got, want) {
		t.Fatalf("allow = %q, want %q", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir holds %d entries, want only %s", len(entries), WorkerSettingsFile)
	}

	replaced, err = WriteWorkerSettings(dir, []string{"go"}, []string{"make test"})
	if err != nil || replaced {
		t.Fatalf("an up-to-date file was rewritten: replaced=%v err=%v", replaced, err)
	}
}

// TestWriteWorkerSettings_RegeneratesAStaleFile proves the file is the
// engine's: a hand-edited one is overwritten and reported as replaced.
func TestWriteWorkerSettings_RegeneratesAStaleFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, WorkerSettingsFile)
	if err := os.WriteFile(path, []byte(`{"permissions":{"allow":["Bash(*)"]}}`), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	replaced, err := WriteWorkerSettings(dir, nil, []string{"make test"})
	if err != nil {
		t.Fatalf("WriteWorkerSettings: %v", err)
	}
	if !replaced {
		t.Fatalf("a hand-edited file was not reported as replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if got := readWorkerAllow(t, data); !slices.Equal(got, []string{"Bash(make test)"}) {
		t.Fatalf("allow = %q, want the rendered list", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file mode = %o, want 600", perm)
	}
}

// TestWriteWorkerSettings_TypedErrors covers both failure points: a directory
// that cannot be created and a settings path that cannot be read.
func TestWriteWorkerSettings_TypedErrors(t *testing.T) {
	t.Parallel()
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	settingsIsADir := t.TempDir()
	if err := os.Mkdir(filepath.Join(settingsIsADir, WorkerSettingsFile), 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for name, dir := range map[string]string{
		"directory is a file":     notADir,
		"settings is a directory": settingsIsADir,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			replaced, err := WriteWorkerSettings(dir, []string{"go"}, nil)
			if !errors.Is(err, ErrWorkerSettingsWrite) {
				t.Fatalf("err = %v, want ErrWorkerSettingsWrite", err)
			}
			var typed *WorkerSettingsWriteError
			if !errors.As(err, &typed) || typed.Path != filepath.Join(dir, WorkerSettingsFile) {
				t.Fatalf("err = %#v, want a WorkerSettingsWriteError naming the file", err)
			}
			if replaced {
				t.Fatalf("a failed write reported a replacement")
			}
		})
	}
}

// TestApplyWorkerSpawnEnvironment_SandboxedSpawnIgnoresTheSettings proves the
// settings file changes nothing for a container worker: it is still spawned
// with full control and no permission mode.
func TestApplyWorkerSpawnEnvironment_SandboxedSpawnIgnoresTheSettings(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := WriteWorkerSettings(dir, []string{"go"}, []string{"make test"}); err != nil {
		t.Fatalf("WriteWorkerSettings: %v", err)
	}
	req := &SpawnSessionRequest{Backend: "claude", SandboxMode: SandboxModeRequired}
	ApplyWorkerSpawnEnvironment(req, dir)
	if !slices.Contains(req.ExtraArgs, WorkerFullControlFlag) {
		t.Fatalf("sandboxed spawn args = %q, want %s", req.ExtraArgs, WorkerFullControlFlag)
	}
	if slices.Contains(req.ExtraArgs, "--permission-mode") {
		t.Fatalf("sandboxed spawn args = %q, want no permission mode", req.ExtraArgs)
	}
}
