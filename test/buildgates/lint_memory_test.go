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

package buildgates

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stubLinter stands in for golangci-lint: it records the GOMEMLIMIT it was
// started with and its arguments, one per line, then exits with
// STUB_LINT_EXIT, or kills itself with SIGKILL when that is "kill".
const stubLinter = `#!/bin/sh
printf 'GOMEMLIMIT=%s\n' "${GOMEMLIMIT-unset}" > "$STUB_LINT_OUT"
printf '%s\n' "$@" >> "$STUB_LINT_OUT"
case "$STUB_LINT_EXIT" in kill) kill -9 $$ ;; esac
exit "${STUB_LINT_EXIT:-0}"
`

// runLintGo runs `make lint-go` with the stub linter as GOLINT and
// LINT_MEMORY_MAX pointing at a fixture holding memoryMax, or at a missing
// file when memoryMax is nil. It returns make's combined output, the lines
// the stub recorded (GOMEMLIMIT first, then each argument) and the run error.
func runLintGo(t *testing.T, memoryMax *string, exit string) (string, []string, error) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "golangci-lint")
	if err := os.WriteFile(stub, []byte(stubLinter), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "missing", "memory.max")
	if memoryMax != nil {
		fixture = filepath.Join(dir, "memory.max")
		if err := os.WriteFile(fixture, []byte(*memoryMax), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	record := filepath.Join(dir, "stub.out")

	cmd := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot, "lint-go",
		"GOLINT="+stub, "LINT_MEMORY_MAX="+fixture)
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "GOMEMLIMIT=")
	})
	cmd.Env = append(env, "GOPROXY=off", "STUB_LINT_OUT="+record, "STUB_LINT_EXIT="+exit)
	out, err := cmd.CombinedOutput()

	var recorded []string
	if body, readErr := os.ReadFile(record); readErr == nil {
		recorded = strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	}
	return string(out), recorded, err
}

// TestLintGoBoundsMemoryByTheCgroupCap checks the environment and arguments
// the lint recipe gives golangci-lint for each kind of cgroup memory.max: a
// numeric cap sets GOMEMLIMIT to three quarters of it, a cap of 8 GiB or less
// also adds --concurrency=2, and an unbounded or unreadable cap sets neither.
func TestLintGoBoundsMemoryByTheCgroupCap(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		name        string
		memoryMax   *string
		gomemlimit  string
		concurrency bool
	}{
		{name: "8 GiB cap", memoryMax: str("8589934592\n"), gomemlimit: "GOMEMLIMIT=6442450944", concurrency: true},
		{name: "16 GiB cap", memoryMax: str("17179869184\n"), gomemlimit: "GOMEMLIMIT=12884901888"},
		{name: "unbounded", memoryMax: str("max\n"), gomemlimit: "GOMEMLIMIT=unset"},
		{name: "unreadable", memoryMax: nil, gomemlimit: "GOMEMLIMIT=unset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, recorded, err := runLintGo(t, tc.memoryMax, "0")
			if err != nil {
				t.Fatalf("make lint-go: %v\n%s", err, out)
			}
			if len(recorded) == 0 {
				t.Fatalf("the stub linter was not run:\n%s", out)
			}
			if recorded[0] != tc.gomemlimit {
				t.Errorf("linter environment has %s, want %s", recorded[0], tc.gomemlimit)
			}
			args := recorded[1:]
			for _, want := range []string{"run", "./..."} {
				if !slices.Contains(args, want) {
					t.Errorf("linter args %q lack %q", args, want)
				}
			}
			hasConcurrency := slices.ContainsFunc(args, func(a string) bool {
				return strings.HasPrefix(a, "--concurrency=")
			})
			switch {
			case tc.concurrency && !slices.Contains(args, "--concurrency=2"):
				t.Errorf("linter args %q lack --concurrency=2", args)
			case !tc.concurrency && hasConcurrency:
				t.Errorf("linter args %q set --concurrency= for a cap above 8 GiB or none", args)
			}
		})
	}
}

// TestLintGoKeepsTheLinterExitCode checks that the memory wrapper still hands
// golangci-lint's exit back to make: success passes, findings fail with the
// findings message, and a signal death fails with the KILLED message.
func TestLintGoKeepsTheLinterExitCode(t *testing.T) {
	unbounded := "max\n"
	cases := []struct {
		name    string
		exit    string
		message string
	}{
		{name: "clean", exit: "0"},
		{name: "findings", exit: "1", message: "golangci-lint reported findings (exit 1)"},
		{name: "killed", exit: "kill", message: "KILLED by signal 9 (exit 137)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := runLintGo(t, &unbounded, tc.exit)
			if tc.message == "" {
				if err != nil {
					t.Fatalf("make lint-go failed on a clean linter exit: %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("make lint-go passed when the linter exited %s:\n%s", tc.exit, out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("make lint-go output lacks %q:\n%s", tc.message, out)
			}
		})
	}
}
