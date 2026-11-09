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

// Command verifygomodauth proves that no module credential reaches a Docker
// test build (#1031).
//
// The image builds resolve every module through the module proxy and the
// checksum database. They still mount a `gomodauth` BuildKit secret, so
// `make stage-gomodauth` stages that file for them, and the file is always
// empty and 0600: whatever credential the host holds -- a FORGE_READ_TOKEN,
// an agent tokens file, a runner's GIT_CONFIG_* variables -- none is written
// into it, printed, or put on a docker command line. Every target whose
// compose file mounts the secret stages the file before its build, so its
// path exists, and leaves it empty afterwards, on the failure path too.
//
// Each case runs the real Makefile. Docker is faked through PATH (see
// scripts/internal/gate/fakedocker.go), and the fake records the staged file's
// size at every call, so "the build saw an empty file" is observed rather than
// inferred. The token is a fixed, obviously fake string; the gate strips the
// caller's own GIT_CONFIG_* and FORGE_READ_TOKEN from the environment so a
// real credential is never read.
//
// Interruption is exercised too: the fake holds the build call open, the gate
// sends SIGINT to make's process group as a Ctrl-C would, and the file must
// still come out empty. The gate also checks that every service the staged
// compose files build is handed the secret and that its Dockerfile mounts it,
// so each keeps a path to the file.
//
// Not run end to end: integration-test-tenantbarrier (it issues certificates
// into the working tree before its build), and integration-bench past its
// build (it sleeps and runs the benchmarks against the stack). Both stage and
// clear with the same lines the exercised targets use, and the wiring check
// covers the images they build.
//
// EXIT: 0 the contract holds · 1 a check failed · 2 the gate could not run.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// fakeToken is the credential every case offers the Makefile. Token-shaped, and distinctive enough
// that finding it anywhere in the output is unambiguous.
const fakeToken = "gate0verifygomodauth0not0a0credential" //nolint:gosec // G101: a fixed fixture string the gate searches for, not a credential

// gateOrigin is the forge origin the cases set beside the token, as a host
// with a read token does in its environment or its tokens file.
const gateOrigin = "http://gitea.invalid:3001"

// gateOriginEnv is gateOrigin in the form a case's environment takes it.
var gateOriginEnv = []string{"GITEA_HTTP_ORIGIN=" + gateOrigin}

// strippedEnv are the variables a case must not inherit: the caller's own
// credentials, and the knobs each case sets for itself.
var strippedEnv = []string{
	"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0",
	"FORGE_READ_TOKEN", "GITEA_HTTP_ORIGIN", "GOMODAUTH_ENV",
	"GOMODAUTH_TOKENS_FILE", "GOMODAUTH_TOKEN_KEY", "GOMODAUTH_OWNER", "HOME",
	"GOMOD_BACKUP", "SDK_GOMOD_BACKUP",
}

func main() {
	gate.DispatchFake()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// WorkspaceError reports that the gate could not build the throwaway tree its
// cases run in.
type WorkspaceError struct {
	Err error
}

func (e *WorkspaceError) Error() string { return "cannot prepare the gate workspace: " + e.Err.Error() }

func (e *WorkspaceError) Unwrap() error { return e.Err }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verifygomodauth", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root whose stage-gomodauth contract is verified")
	if err := fs.Parse(args); err != nil {
		return gate.ExitToolError
	}

	work, err := os.MkdirTemp("", "verifygomodauth-")
	if err != nil {
		fmt.Fprintln(stderr, "verify-gomodauth:", (&WorkspaceError{Err: err}).Error())
		return gate.ExitToolError
	}
	defer func() {
		if rerr := os.RemoveAll(work); rerr != nil {
			fmt.Fprintln(stderr, "verify-gomodauth: cannot remove the workspace:", rerr)
		}
	}()

	if err := gate.InstallFakes(filepath.Join(work, "bin")); err != nil {
		fmt.Fprintln(stderr, "verify-gomodauth:", (&WorkspaceError{Err: err}).Error())
		return gate.ExitToolError
	}

	failures, err := check(*root, work, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "verify-gomodauth:", err)
		return gate.ExitToolError
	}
	if failures != 0 {
		fmt.Fprintf(stderr, "verify-gomodauth: %d check(s) failed\n", failures)
		return gate.ExitFindings
	}
	fmt.Fprintln(stdout, "verify-gomodauth: no credential reaches an image build, and the staged secret file is empty and 0600")
	return gate.ExitClean
}

// invocation is one `make` run of a case.
type invocation struct {
	// targets are the make goals, run in order.
	targets []string
	// permissiveUmask runs make under umask 022, the common default, so a
	// file created without an explicit mode comes out world-readable.
	permissiveUmask bool
	// preStagedMode, when non-zero, creates the staged file with these
	// permission bits (and some content) before make runs.
	preStagedMode os.FileMode
	// env is added to the stripped caller environment.
	env []string
	// makeVars are passed on make's command line.
	makeVars []string
	// tokensFile, when non-empty, is written as the agent tokens file.
	tokensFile string
	// failMatch, when non-empty, makes every docker call containing it fail.
	failMatch string
	// hangMatch, when non-empty, makes the docker call containing it wait
	// until it is signalled.
	hangMatch string
}

// outcome is what a run left behind.
type outcome struct {
	status int
	output string
	staged string
	// stagedExists reports whether the staged file was there after the run;
	// stagedMode is its permission bits.
	stagedExists bool
	stagedMode   os.FileMode
	calls        string
	// watch holds the staged file's size at each docker call, in order.
	watch []watchedCall
}

type watchedCall struct {
	call string
	size int64
}

// caseDir makes a fresh directory for one case under work.
func caseDir(work, name string) (string, error) {
	dir, err := os.MkdirTemp(work, name+"-")
	if err != nil {
		return "", &WorkspaceError{Err: err}
	}
	return dir, nil
}

// makeCommand builds the make run of one case: the caller's environment with
// its credentials stripped, HOME and the go.mod backups inside the case
// directory, and the fake docker first on PATH.
func makeCommand(root, dir, work, staged string, inv invocation) *exec.Cmd {
	args := append([]string{"--no-print-directory"}, inv.targets...)
	args = append(args,
		"GOMODAUTH_ENV="+staged,
		"GOMOD_BACKUP="+filepath.Join(dir, "gomod-backup"),
		"SDK_GOMOD_BACKUP="+filepath.Join(dir, "sdk-gomod-backup"),
	)
	args = append(args, inv.makeVars...)
	cmd := exec.Command("make", args...)
	if inv.permissiveUmask {
		cmd = exec.Command("sh", append([]string{"-c", `umask 022 && exec make "$@"`, "sh"}, args...)...)
	}
	cmd.Dir = root
	cmd.Env = append(stripEnv(os.Environ()),
		"HOME="+filepath.Join(dir, "home"),
		"PATH="+filepath.Join(work, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		gate.FakeDirEnv+"="+dir,
	)
	cmd.Env = append(cmd.Env, inv.env...)
	return cmd
}

// invoke runs one case in its own directory under work.
func invoke(root, work, name string, inv invocation) (outcome, error) {
	dir, staged, err := prepareCase(work, name, inv)
	if err != nil {
		return outcome{}, err
	}
	cmd := makeCommand(root, dir, work, staged, inv)
	var sink strings.Builder
	cmd.Stdout = &sink
	cmd.Stderr = &sink
	status, err := exitStatus(cmd.Run())
	if err != nil {
		return outcome{}, err
	}
	return collect(dir, staged, status, sink.String())
}

// prepareCase makes one case's directory: its HOME and tokens file, the fake's
// control files, and any pre-existing staged file. It returns the directory and
// the staged file's path.
func prepareCase(work, name string, inv invocation) (string, string, error) {
	dir, err := caseDir(work, name)
	if err != nil {
		return "", "", err
	}
	home := filepath.Join(dir, "home")
	staged := filepath.Join(dir, "gomodauth.env")
	if err := os.MkdirAll(filepath.Join(home, ".agent-identity"), 0o700); err != nil {
		return "", "", &WorkspaceError{Err: err}
	}
	if inv.tokensFile != "" {
		if err := os.WriteFile(filepath.Join(home, ".agent-identity", "tokens.env"), []byte(inv.tokensFile), 0o600); err != nil {
			return "", "", &WorkspaceError{Err: err}
		}
	}
	for name, content := range map[string]string{
		"calls.log":  "",
		"watch.path": staged + "\n",
		"fail.match": inv.failMatch + "\n",
		"hang.match": inv.hangMatch + "\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			return "", "", &WorkspaceError{Err: err}
		}
	}
	if inv.preStagedMode != 0 {
		if err := os.WriteFile(staged, []byte("stale\n"), 0o600); err != nil {
			return "", "", &WorkspaceError{Err: err}
		}
		if err := os.Chmod(staged, inv.preStagedMode); err != nil {
			return "", "", &WorkspaceError{Err: err}
		}
	}
	return dir, staged, nil
}

// exitStatus turns the result of running make into its exit status. A make
// killed by a signal reports -1, which no case expects to be 0.
func exitStatus(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return 0, err
	}
	return exit.ExitCode(), nil
}

// collect reads back what a case's run left behind.
func collect(dir, staged string, status int, output string) (outcome, error) {
	out := outcome{status: status, output: output}
	if info, err := os.Stat(staged); err == nil {
		out.stagedExists = true
		out.stagedMode = info.Mode().Perm()
		raw, rerr := os.ReadFile(staged)
		if rerr != nil {
			return outcome{}, &WorkspaceError{Err: rerr}
		}
		out.staged = string(raw)
	} else if !errors.Is(err, os.ErrNotExist) {
		return outcome{}, &WorkspaceError{Err: err}
	}

	calls, err := os.ReadFile(filepath.Join(dir, "calls.log"))
	if err != nil {
		return outcome{}, &WorkspaceError{Err: err}
	}
	out.calls = string(calls)

	watch, err := os.ReadFile(filepath.Join(dir, "watch.log"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return outcome{}, &WorkspaceError{Err: err}
	}
	out.watch = parseWatch(string(watch))
	return out, nil
}

// parseWatch reads the fake's "<call>\t<bytes>" lines.
func parseWatch(raw string) []watchedCall {
	var calls []watchedCall
	for _, line := range strings.Split(raw, "\n") {
		call, size, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil {
			continue
		}
		calls = append(calls, watchedCall{call: call, size: n})
	}
	return calls
}

// stripEnv drops the variables a case must not inherit.
func stripEnv(environ []string) []string {
	drop := map[string]bool{}
	for _, name := range strippedEnv {
		drop[name] = true
	}
	var kept []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			kept = append(kept, kv)
		}
	}
	return kept
}

// targetCase is a Docker target whose compose file mounts the secret.
type targetCase struct {
	target   string
	makeVars []string
	// build is the docker call that builds the image.
	build string
	// after is a docker call the recipe makes once the build is done; the file
	// must already be empty by then. Empty when the case fails the build.
	after string
	// failMatch breaks the build so the failure path is exercised.
	failMatch string
}

var targetCases = []targetCase{
	{target: "integration-test", build: "build --build-arg BUILD_TAGS", failMatch: "build --build-arg BUILD_TAGS"},
	{target: "integration-test-seal", build: "build seal-test", after: "run --rm -T seal-test"},
	{target: "test-e2e-docker", build: "build e2e", after: "run --rm e2e"},
	{target: "test-e2e-acp-docker", makeVars: []string{"GPU_HOST=gpu.invalid"}, build: "build e2e", after: "run --rm e2e"},
}

// check runs every case and returns how many assertions failed, or an error
// when the gate itself could not run.
func check(root, work string, stdout, stderr io.Writer) (int, error) {
	failures := 0
	fail := func(format string, args ...any) {
		fmt.Fprintf(stderr, "  ✗ "+format+"\n", args...)
		failures++
	}
	noLeak := func(what string, out outcome) {
		if strings.Contains(out.output, fakeToken) {
			fail("%s: the token reached the log", what)
		}
		if strings.Contains(out.calls, fakeToken) {
			fail("%s: the token reached a docker command line", what)
		}
	}
	tokensFile := "OTHER_READ_TOKEN=someone0elses0token\nexport FORGE_READ_TOKEN=\"" + fakeToken + "\"\nGITEA_HTTP_ORIGIN=" + gateOrigin + "\n"
	envToken := append([]string{"FORGE_READ_TOKEN=" + fakeToken}, gateOriginEnv...)
	runnerEnv := []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=url.http://x:" + fakeToken + "@gitea.invalid/jeremyhahn/.insteadOf",
		"GIT_CONFIG_VALUE_0=https://github.com/jeremyhahn/",
	}

	// Whatever credential the host holds, stage-gomodauth stages an empty 0600
	// file, exits 0 and puts the token nowhere.
	stagingCases := []struct {
		slug string
		what string
		inv  invocation
	}{
		{slug: "env-token", what: "a FORGE_READ_TOKEN", inv: invocation{env: envToken}},
		{slug: "tokens-file", what: "a tokens file with FORGE_READ_TOKEN and GITEA_HTTP_ORIGIN", inv: invocation{tokensFile: tokensFile}},
		{slug: "runner", what: "a runner's GIT_CONFIG_* variables", inv: invocation{env: runnerEnv}},
		{slug: "bad-token", what: "a value that is not token-shaped", inv: invocation{env: []string{"FORGE_READ_TOKEN=" + fakeToken + "'$(id)"}}},
		{slug: "none", what: "no credential at all", inv: invocation{}},
		{slug: "stale-0644", what: "an existing 0644 file that holds text", inv: invocation{tokensFile: tokensFile, preStagedMode: 0o644}},
	}
	for _, sc := range stagingCases {
		fmt.Fprintf(stdout, "case: with %s, stage-gomodauth stages an empty 0600 file\n", sc.what)
		sc.inv.targets = []string{"stage-gomodauth"}
		out, err := invoke(root, work, sc.slug, sc.inv)
		if err != nil {
			return 0, err
		}
		if out.status != 0 {
			fail("%s: stage-gomodauth exited %d:\n%s", sc.what, out.status, out.output)
		}
		if !out.stagedExists || out.staged != "" {
			fail("%s: the staged file is missing or not empty (%d bytes)", sc.what, len(out.staged))
		}
		if strings.Contains(out.staged, fakeToken) {
			fail("%s: the token reached the staged file", sc.what)
		}
		if out.stagedExists && out.stagedMode != 0o600 {
			fail("%s: the staged file was left with mode %v, want 0600", sc.what, out.stagedMode)
		}
		noLeak(sc.what, out)
	}

	fmt.Fprintln(stdout, "case: clear-gomodauth then stage-gomodauth under umask 022 stages 0600")
	out, err := invoke(root, work, "clear-then-stage", invocation{
		targets:         []string{"clear-gomodauth", "stage-gomodauth"},
		tokensFile:      tokensFile,
		permissiveUmask: true,
	})
	if err != nil {
		return 0, err
	}
	if out.status != 0 || !out.stagedExists || out.staged != "" {
		fail("clear then stage did not leave an empty file (exit %d)", out.status)
	}
	if out.stagedMode != 0o600 {
		fail("clear then stage left mode %v, want 0600", out.stagedMode)
	}

	fmt.Fprintln(stdout, "case: clear-gomodauth under umask 022 creates the file 0600")
	out, err = invoke(root, work, "clear-creates", invocation{
		targets:         []string{"clear-gomodauth"},
		permissiveUmask: true,
	})
	if err != nil {
		return 0, err
	}
	if out.status != 0 || !out.stagedExists || out.staged != "" {
		fail("clear-gomodauth did not leave an empty file (exit %d)", out.status)
	}
	if out.stagedExists && out.stagedMode != 0o600 {
		fail("clear-gomodauth created the file with mode %v, want 0600", out.stagedMode)
	}

	for _, tc := range targetCases {
		fmt.Fprintf(stdout, "case: %s stages an empty secret file for its image build\n", tc.target)
		out, err := invoke(root, work, tc.target, invocation{
			targets:    []string{tc.target},
			makeVars:   tc.makeVars,
			tokensFile: tokensFile,
			env:        append(append([]string{}, envToken...), runnerEnv...),
			failMatch:  tc.failMatch,
		})
		if err != nil {
			return 0, err
		}
		checkTarget(tc, out, fail)
		noLeak(tc.target, out)
	}

	for _, ic := range interruptCases {
		fmt.Fprintf(stdout, "case: interrupting %s during its build leaves the secret file empty\n", ic.target)
		out, reached, err := interrupt(root, work, ic, tokensFile)
		if err != nil {
			return 0, err
		}
		hangSize := int64(-1)
		for _, c := range out.watch {
			if strings.Contains(c.call, ic.hang) {
				hangSize = c.size
			}
		}
		switch {
		case !reached:
			fail("%s never reached %q — this case no longer exercises the interrupt:\n%s", ic.target, ic.hang, out.output)
		case hangSize < 0:
			fail("%s was interrupted in %q without the secret file staged", ic.target, ic.hang)
		case hangSize > 0:
			fail("%s's build saw %d bytes of credentials when interrupted in %q", ic.target, hangSize, ic.hang)
		}
		if !out.stagedExists || out.staged != "" {
			fail("%s left the staged file missing or not empty when interrupted", ic.target)
		}
		noLeak(ic.target+" (interrupted)", out)
	}

	fmt.Fprintln(stdout, "case: every image the staged targets build mounts the secret and reads it")
	wiring, err := checkWiring(root)
	if err != nil {
		return 0, err
	}
	for _, finding := range wiring {
		fail("%s", finding)
	}
	return failures, nil
}

// checkTarget asserts one Docker target's contract, reporting through fail.
func checkTarget(tc targetCase, out outcome, fail func(string, ...any)) {
	if tc.failMatch != "" && out.status == 0 {
		fail("%s reported success after its image build failed", tc.target)
	}
	if tc.failMatch == "" && out.status != 0 {
		fail("%s exited %d against a daemon that answered every call:\n%s", tc.target, out.status, out.output)
	}
	buildAt := -1
	for i, c := range out.watch {
		if strings.Contains(c.call, tc.build) {
			buildAt = i
		}
	}
	switch {
	case buildAt < 0:
		fail("%s never reached its image build (%q) — this case no longer exercises it", tc.target, tc.build)
	case out.watch[buildAt].size < 0:
		fail("%s never staged the secret file: it was missing at its image build", tc.target)
	case out.watch[buildAt].size > 0:
		fail("%s built its image with %d bytes in the secret file; the build must see an empty file",
			tc.target, out.watch[buildAt].size)
	}
	if tc.after != "" && buildAt >= 0 {
		seen := false
		for _, c := range out.watch[buildAt+1:] {
			if strings.Contains(c.call, tc.after) {
				seen = true
				if c.size != 0 {
					fail("%s's secret file was not empty at %q, after its build", tc.target, tc.after)
				}
				break
			}
		}
		if !seen {
			fail("%s never reached %q after its build", tc.target, tc.after)
		}
	}
	if !out.stagedExists || out.staged != "" {
		fail("%s left the staged file missing or not empty", tc.target)
	}
}
