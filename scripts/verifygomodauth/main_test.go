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

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// TestMain dispatches the faked executables before the suite runs: PATH
// resolves this test binary as `docker` inside the cases.
func TestMain(m *testing.M) {
	gate.DispatchFake()
	os.Exit(m.Run())
}

// repoRoot is this repository, whose real Makefile the gate verifies.
const repoRoot = "../.."

// fixtureFiles are what a fixture tree copies from this repository: the
// Makefile the targets live in, the toolchain pins it includes, and the
// compose files and Dockerfiles the wiring check reads.
var fixtureFiles = []string{
	"Makefile",
	"pkg/version/toolchain.mk",
	"test/integration/docker-compose.integration.yml",
	"test/integration/docker-compose.seal.yml",
	"test/integration/Dockerfile",
	"test/integration/Dockerfile.node",
	"test/integration/Dockerfile.seal",
	"test/e2e/docker-compose.e2e.yml",
	"test/e2e/Dockerfile",
}

// mutatedRepo copies fixtureFiles into a fixture tree after applying one
// replacement to file, so a case can break exactly one part of the contract.
// The replacement must apply, or the case would be testing the unbroken tree.
func mutatedRepo(t *testing.T, file, old, replacement string) string {
	t.Helper()
	dir := t.TempDir()
	mutated := false
	for _, rel := range fixtureFiles {
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		body := string(raw)
		if rel == file {
			if !strings.Contains(body, old) {
				t.Fatalf("%s no longer contains %q — update this case", rel, old)
			}
			body = strings.ReplaceAll(body, old, replacement)
			mutated = true
		}
		writeFixture(t, dir, rel, body)
	}
	if !mutated {
		t.Fatalf("%s is not a fixture file", file)
	}
	// integration-test backs these up before it builds.
	writeFixture(t, dir, "go.mod", "module fixture\n")
	writeFixture(t, dir, "sdk/go/go.mod", "module fixture\n")
	return dir
}

func writeFixture(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("preparing %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", rel, err)
	}
}

func workspace(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	if err := gate.InstallFakes(filepath.Join(work, "bin")); err != nil {
		t.Fatalf("installing the fakes: %v", err)
	}
	return work
}

func TestCheckPassesOnThisRepository(t *testing.T) {
	var stdout, stderr strings.Builder
	failures, err := check(repoRoot, workspace(t), &stdout, &stderr)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failures != 0 {
		t.Fatalf("the repository's Makefile reported %d failure(s):\n%s", failures, stderr.String())
	}
	for _, tc := range targetCases {
		if !strings.Contains(stdout.String(), "case: "+tc.target+" stages") {
			t.Fatalf("the %s case was not run:\n%s", tc.target, stdout.String())
		}
	}
}

func TestCheckFailsOnEachWayTheContractCanBreak(t *testing.T) {
	cases := []struct {
		name        string
		file        string
		old         string
		replacement string
		want        string
	}{
		{
			// #1031 itself: a host with only a tokens file stages nothing.
			name:        "ignoring the agent tokens file",
			file:        "Makefile",
			old:         "GOMODAUTH_TOKENS_FILE ?= $(HOME)/.agent-identity/tokens.env",
			replacement: "GOMODAUTH_TOKENS_FILE ?= /nonexistent/tokens.env",
			want:        "not the section-6 variables built from the tokens file",
		},
		{
			// go-quicbaas#1130: the tree must name no forge.
			name:        "a default forge origin",
			file:        "Makefile",
			old:         "GITEA_HTTP_ORIGIN ?=\n",
			replacement: "GITEA_HTTP_ORIGIN ?= http://forge.invalid:3001\n",
			want:        "a token with no origin was staged against a default forge",
		},
		{
			name:        "ignoring the tokens file's origin",
			file:        "Makefile",
			old:         `if [ -z "$$origin" ] && [ -r "$(GOMODAUTH_TOKENS_FILE)" ]; then \`,
			replacement: `if false; then \`,
			want:        "the tokens file's GITEA_HTTP_ORIGIN was not used",
		},
		{
			name:        "never clearing the staged file",
			file:        "Makefile",
			old:         `GOMODAUTH_CLEAR = { umask 077; : > "$(GOMODAUTH_ENV)"; } 2>/dev/null || true`,
			replacement: "GOMODAUTH_CLEAR = true",
			want:        "left the credentials in the staged file",
		},
		{
			name:        "printing the token",
			file:        "Makefile",
			old:         `origin="$(GITEA_HTTP_ORIGIN)"; \`,
			replacement: `echo "$$token"; origin="$(GITEA_HTTP_ORIGIN)"; \`,
			want:        "the token reached the log",
		},
		{
			name:        "a target that never stages",
			file:        "Makefile",
			old:         "\t@$(MAKE) --no-print-directory stage-gomodauth\n\t@COMPOSE_FILE=\"test/e2e/docker-compose.e2e.yml\"; \\",
			replacement: "\t@COMPOSE_FILE=\"test/e2e/docker-compose.e2e.yml\"; \\",
			want:        "test-e2e-docker never staged the credentials",
		},
		{
			name:        "staging world-readable",
			file:        "Makefile",
			old:         "\tumask 077; \\\n\t{ \\\n\t\tprintf 'export GOPRIVATE=%s\\n' \"$${GOPRIVATE:-github.com/$(GOMODAUTH_OWNER)/*}\"",
			replacement: "\tchmod 644 \"$(GOMODAUTH_ENV)\"; \\\n\t{ \\\n\t\tprintf 'export GOPRIVATE=%s\\n' \"$${GOPRIVATE:-github.com/$(GOMODAUTH_OWNER)/*}\"",
			want:        "want 0600",
		},
		{
			// The review finding: truncating keeps an existing file's mode.
			name:        "writing the token into an existing file without fixing its mode",
			file:        "Makefile",
			old:         `@umask 077; : > "$(GOMODAUTH_ENV)" && chmod 600 "$(GOMODAUTH_ENV)"`,
			replacement: `@umask 077; : > "$(GOMODAUTH_ENV)"`,
			want:        "staging over an existing 0644 file left mode",
		},
		{
			name:        "clearing under the caller's umask",
			file:        "Makefile",
			old:         `GOMODAUTH_CLEAR = { umask 077; : > "$(GOMODAUTH_ENV)"; }`,
			replacement: `GOMODAUTH_CLEAR = { : > "$(GOMODAUTH_ENV)"; }`,
			want:        "clear-gomodauth created the file with mode",
		},
		{
			name:        "integration-bench clearing only on the normal path",
			file:        "Makefile",
			old:         "\t@trap '$(GOMODAUTH_CLEAR)' EXIT; \\\n\t$(DOCKER_COMPOSE) -f test/integration/docker-compose.integration.yml up -d; \\",
			replacement: "\t@$(DOCKER_COMPOSE) -f test/integration/docker-compose.integration.yml up -d; \\",
			want:        "integration-bench left the credentials in the staged file when interrupted",
		},
		{
			name:        "a built service Compose never hands the secret",
			file:        "test/integration/docker-compose.seal.yml",
			old:         "awskms vault}\"\n      secrets:\n        - gomodauth\n",
			replacement: "awskms vault}\"\n",
			want:        "service seal-test is not handed the gomodauth secret",
		},
		{
			name:        "a Dockerfile that never reads the secret",
			file:        "test/integration/Dockerfile.seal",
			old:         "RUN --mount=type=secret,id=gomodauth \\\n",
			replacement: "RUN \\\n",
			want:        "Dockerfile.seal never reads the gomodauth secret",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr strings.Builder
			failures, err := check(mutatedRepo(t, tc.file, tc.old, tc.replacement), workspace(t), &stdout, &stderr)
			if err != nil {
				t.Fatalf("check: %v", err)
			}
			if failures == 0 {
				t.Fatalf("%s must fail the gate", tc.name)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("the failure does not report %q:\n%s", tc.want, stderr.String())
			}
		})
	}
}

func TestCheckReportsAToolErrorWithoutAMakefile(t *testing.T) {
	var stdout, stderr strings.Builder
	if _, err := check(t.TempDir(), workspace(t), &stdout, &stderr); err == nil {
		t.Fatal("a tree without a Makefile must be a tool error, not a verdict")
	}
}

func TestRunReportsTheVerdictAndTheStatus(t *testing.T) {
	t.Run("this repository", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if status := run([]string{"-root", repoRoot}, &stdout, &stderr); status != gate.ExitClean {
			t.Fatalf("run exited %d, want %d\nstderr: %s", status, gate.ExitClean, stderr.String())
		}
		if !strings.Contains(stdout.String(), "cleared after every build") {
			t.Fatalf("the pass was not reported: %q", stdout.String())
		}
	})

	t.Run("a Makefile that keeps the credentials", func(t *testing.T) {
		root := mutatedRepo(t, "Makefile", `GOMODAUTH_CLEAR = { umask 077; : > "$(GOMODAUTH_ENV)"; } 2>/dev/null || true`, "GOMODAUTH_CLEAR = true")
		var stdout, stderr strings.Builder
		if status := run([]string{"-root", root}, &stdout, &stderr); status != gate.ExitFindings {
			t.Fatalf("run exited %d, want %d", status, gate.ExitFindings)
		}
		if !strings.Contains(stderr.String(), "check(s) failed") {
			t.Fatalf("the count of failures was not reported: %q", stderr.String())
		}
	})

	t.Run("an unknown flag", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if status := run([]string{"-nonesuch"}, &stdout, &stderr); status != gate.ExitToolError {
			t.Fatalf("run exited %d, want %d", status, gate.ExitToolError)
		}
	})

	t.Run("a tree without a Makefile", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if status := run([]string{"-root", t.TempDir()}, &stdout, &stderr); status != gate.ExitToolError {
			t.Fatalf("run exited %d, want %d", status, gate.ExitToolError)
		}
	})
}

func TestSectionSixBuildsTheRewriteFromTheOrigin(t *testing.T) {
	got := sectionSix("tok", "https://gitea.invalid:8443/")
	want := "export GOPRIVATE=github.com/jeremyhahn/*\n" +
		"export GIT_CONFIG_COUNT=1\n" +
		"export GIT_CONFIG_KEY_0=url.https://x:tok@gitea.invalid:8443/jeremyhahn/.insteadOf\n" +
		"export GIT_CONFIG_VALUE_0=https://github.com/jeremyhahn/\n"
	if got != want {
		t.Fatalf("sectionSix = %q, want %q", got, want)
	}
}

func TestStripEnvDropsTheCallersCredentials(t *testing.T) {
	got := stripEnv([]string{
		"PATH=/bin", "GIT_CONFIG_KEY_0=secret", "GITEA_READ_TOKEN=secret",
		"HOME=/root", "GOPRIVATE=x", "GIT_CONFIG_KEY_00=kept",
	})
	want := []string{"PATH=/bin", "GIT_CONFIG_KEY_00=kept"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("stripEnv = %v, want %v", got, want)
	}
	if got := stripEnv(nil); len(got) != 0 {
		t.Fatalf("stripEnv(nil) = %v, want empty", got)
	}
}

func TestParseWatchReadsCallsAndSkipsMalformedLines(t *testing.T) {
	got := parseWatch("compose build e2e\t42\nnot a watch line\ncompose run\tNaN\ncompose run --rm e2e\t0\n")
	want := []watchedCall{{call: "compose build e2e", size: 42}, {call: "compose run --rm e2e", size: 0}}
	if len(got) != len(want) {
		t.Fatalf("parseWatch = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseWatch[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestCheckTargetReportsABuildThatIsNeverReached(t *testing.T) {
	var reported []string
	fail := func(format string, args ...any) { reported = append(reported, format) }
	checkTarget(targetCase{target: "t", build: "build x", after: "run x"},
		outcome{output: stagedMarker, stagedExists: true}, fail)
	if len(reported) != 1 || !strings.Contains(reported[0], "never reached its image build") {
		t.Fatalf("reported %q, want one never-reached finding", reported)
	}
}

func TestCheckTargetReportsCredentialsStillPresentAfterTheBuild(t *testing.T) {
	var reported []string
	fail := func(format string, args ...any) { reported = append(reported, format) }
	checkTarget(targetCase{target: "t", build: "build x", after: "run x"}, outcome{
		output:       stagedMarker,
		stagedExists: true,
		watch:        []watchedCall{{call: "compose build x", size: 10}, {call: "compose run x", size: 10}},
	}, fail)
	if len(reported) != 1 || !strings.Contains(reported[0], "still held the credentials") {
		t.Fatalf("reported %q, want one still-held finding", reported)
	}
}

func TestWorkspaceErrorCarriesItsCause(t *testing.T) {
	cause := errors.New("read-only file system")
	err := &WorkspaceError{Err: cause}
	if !errors.Is(err, cause) {
		t.Fatal("WorkspaceError must unwrap to the underlying failure")
	}
	if !strings.Contains(err.Error(), "read-only file system") {
		t.Fatalf("WorkspaceError.Error() omits the cause: %s", err.Error())
	}
}

func TestRunLeavesTheHostsGomodBackupsUntouched(t *testing.T) {
	// The review finding: the integration-test cases used to write the real
	// recipe's fixed backup paths, which a concurrent run restores into its own
	// go.mod. Each case now backs up inside its own directory, so whatever the
	// host has there -- present or not, whatever it holds -- is left alone.
	hostBackups := []string{"/tmp/quicbaas-gomod-backup", "/tmp/quicbaas-sdk-gomod-backup"}
	snapshot := func() map[string]string {
		state := map[string]string{}
		for _, path := range hostBackups {
			raw, err := os.ReadFile(path)
			switch {
			case err == nil:
				state[path] = "present:" + string(raw)
			case errors.Is(err, os.ErrNotExist):
				state[path] = "absent"
			default:
				t.Fatalf("reading %s: %v", path, err)
			}
		}
		return state
	}
	before := snapshot()

	var stdout, stderr strings.Builder
	failures, err := check(mutatedRepo(t, "Makefile", "GOMODAUTH_OWNER ?= jeremyhahn", "GOMODAUTH_OWNER ?= jeremyhahn"), workspace(t), &stdout, &stderr)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failures != 0 {
		t.Fatalf("the fixture copy of this repository reported %d failure(s):\n%s", failures, stderr.String())
	}

	after := snapshot()
	for _, path := range hostBackups {
		if before[path] != after[path] {
			t.Fatalf("%s changed across the run (%d bytes of state before, %d after)", path, len(before[path]), len(after[path]))
		}
	}
}

func TestInterruptReportsATargetThatNeverReachesTheCall(t *testing.T) {
	// A hang that never matches must not block the gate: make exits on its
	// own, and the case reports the call as never reached.
	ic := interruptCase{target: "stage-gomodauth", hang: "a docker call no recipe makes"}
	out, reached, err := interrupt(repoRoot, workspace(t), ic, "")
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if reached {
		t.Fatal("a call that was never made was reported as reached")
	}
	if out.status != 0 {
		t.Fatalf("stage-gomodauth exited %d:\n%s", out.status, out.output)
	}
}

func TestExitStatusReadsMakesResult(t *testing.T) {
	if status, err := exitStatus(nil); status != 0 || err != nil {
		t.Fatalf("exitStatus(nil) = (%d, %v), want (0, nil)", status, err)
	}
	cause := errors.New("exec: \"make\": executable file not found in $PATH")
	if _, err := exitStatus(cause); !errors.Is(err, cause) {
		t.Fatalf("a failure to run make must be returned, got %v", err)
	}
}

func TestMountsSecretAcceptsBothComposeForms(t *testing.T) {
	cases := map[string]bool{
		"[gomodauth]":                          true,
		"[other, gomodauth]":                   true,
		"[{source: gomodauth, target: x}]":     true,
		"[other]":                              false,
		"[{source: other, target: gomodauth}]": false,
		"[]":                                   false,
	}
	for doc, want := range cases {
		var secrets []yaml.Node
		if err := yaml.Unmarshal([]byte(doc), &secrets); err != nil {
			t.Fatalf("parsing %s: %v", doc, err)
		}
		if got := mountsSecret(secrets); got != want {
			t.Fatalf("mountsSecret(%s) = %v, want %v", doc, got, want)
		}
	}
}

func TestCheckWiringReportsAnUnreadableComposeFile(t *testing.T) {
	_, err := checkWiring(t.TempDir())
	var composeErr *ComposeFileError
	if !errors.As(err, &composeErr) {
		t.Fatalf("a missing compose file must be a ComposeFileError, got %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the ComposeFileError must carry its cause, got %v", err)
	}
}

func TestCheckWiringReportsAShorthandBuild(t *testing.T) {
	root := mutatedRepo(t, "test/e2e/docker-compose.e2e.yml",
		"    build:\n      context: ../..\n      dockerfile: test/e2e/Dockerfile\n",
		"    build: ../..\n    x-unused:\n      dockerfile: test/e2e/Dockerfile\n")
	findings, err := checkWiring(root)
	if err != nil {
		t.Fatalf("checkWiring: %v", err)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "service e2e builds without declaring the gomodauth secret") {
		t.Fatalf("findings = %q, want the shorthand build reported", findings)
	}
}
