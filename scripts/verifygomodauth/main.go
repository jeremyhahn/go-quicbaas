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

// Command verifygomodauth proves that the Docker test targets can fetch the
// private first-party modules on a developer or agent host, and that the Gitea
// token they need never outlives the image build (#1031).
//
// The image builds read a `gomodauth` BuildKit secret: a file of the
// dependency-resolution.md section-6 variables that `make stage-gomodauth`
// writes. It used to be built only from the variables a Gitea runner injects,
// so on any other host it was written empty, `go mod download` inside the image
// asked the public proxy for a tag that is not mirrored, and the build failed
// with "unknown revision". The target now also builds it from a Gitea read
// token -- FORGE_READ_TOKEN, else the agent tokens file -- and every target
// whose compose file mounts the secret stages it before its build and empties
// it once the build is done, on the failure path too.
//
// Each case runs the real Makefile. Docker is faked through PATH (see
// scripts/internal/gate/fakedocker.go), and the fake records the staged file's
// size at every call, so "it held the credentials during the build and nothing
// after" is observed rather than inferred. The token is a fixed, obviously fake
// string; the gate strips the caller's own GIT_CONFIG_*, GOPRIVATE and
// FORGE_READ_TOKEN from the environment so a real credential is never read.
//
// Interruption is exercised too: the fake holds the build call open, the gate
// sends SIGINT to make's process group as a Ctrl-C would, and the file must
// still come out empty. Staging the host file proves nothing unless the image
// reads it, so the gate also checks that every service the staged compose
// files build is handed the secret and that its Dockerfile mounts it.
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

// fakeToken is what every case stages. Token-shaped, and distinctive enough
// that finding it anywhere in the output is unambiguous.
const fakeToken = "gate0verifygomodauth0not0a0credential" //nolint:gosec // G101: a fixed fixture string the gate searches for, not a credential

// stagedSize is the size of the section-6 file stage-gomodauth writes for the
// target cases' token and origin. stage-gomodauth runs silent (DS-2534), so a
// target proves it staged by the staged file holding exactly that many bytes
// at its image build, as the fake docker records.
var stagedSize = int64(len(sectionSix(fakeToken, gateOrigin)))

// gateOrigin is the forge every staging case names. The Makefile carries no
// default origin, so each case passes one explicitly, as a real host does from
// its environment or its tokens file.
const gateOrigin = "http://gitea.invalid:3001"

// gateOriginEnv is gateOrigin in the form a case's environment takes it.
var gateOriginEnv = []string{"GITEA_HTTP_ORIGIN=" + gateOrigin}

// strippedEnv are the variables a case must not inherit: the caller's own
// credentials, and the knobs each case sets for itself.
var strippedEnv = []string{
	"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GOPRIVATE",
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
	fmt.Fprintln(stdout, "verify-gomodauth: module credentials are staged on any host and cleared after every build")
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

// sectionSix is the staged file stage-gomodauth must write for a token and an
// origin: exactly the three dependency-resolution.md section-6 variables.
func sectionSix(token, origin string) string {
	origin = strings.TrimSuffix(origin, "/")
	scheme, hostport, _ := strings.Cut(origin, "://")
	return "export GIT_CONFIG_COUNT=1\n" +
		"export GIT_CONFIG_KEY_0=url." + scheme + "://x:" + token + "@" + hostport + "/jeremyhahn/.insteadOf\n" +
		"export GIT_CONFIG_VALUE_0=https://github.com/jeremyhahn/\n"
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
	tokensFile := "OTHER_READ_TOKEN=someone0elses0token\nexport FORGE_READ_TOKEN=\"" + fakeToken + "\"\n"

	fmt.Fprintln(stdout, "case: a token in the agent tokens file is staged as the section-6 variables")
	out, err := invoke(root, work, "tokens-file", invocation{targets: []string{"stage-gomodauth"}, tokensFile: tokensFile, env: gateOriginEnv})
	if err != nil {
		return 0, err
	}
	if out.status != 0 {
		fail("stage-gomodauth exited %d:\n%s", out.status, out.output)
	}
	if want := sectionSix(fakeToken, gateOrigin); out.staged != want {
		fail("the staged file is not the section-6 variables built from the tokens file (%d bytes, want %d)", len(out.staged), len(want))
	}
	if out.stagedMode != 0o600 {
		fail("the staged file has mode %v, want 0600", out.stagedMode)
	}
	noLeak("tokens file", out)

	fmt.Fprintln(stdout, "case: an existing world-readable file is made 0600 before the token goes in")
	out, err = invoke(root, work, "stale-0644", invocation{
		targets:       []string{"stage-gomodauth"},
		tokensFile:    tokensFile,
		env:           gateOriginEnv,
		preStagedMode: 0o644,
	})
	if err != nil {
		return 0, err
	}
	if out.status != 0 || out.staged != sectionSix(fakeToken, gateOrigin) {
		fail("staging over an existing file did not write the section-6 variables (exit %d)", out.status)
	}
	if out.stagedMode != 0o600 {
		fail("staging over an existing 0644 file left mode %v, want 0600", out.stagedMode)
	}

	fmt.Fprintln(stdout, "case: clear-gomodauth then stage-gomodauth under umask 022 stages 0600")
	out, err = invoke(root, work, "clear-then-stage", invocation{
		targets:         []string{"clear-gomodauth", "stage-gomodauth"},
		tokensFile:      tokensFile,
		env:             gateOriginEnv,
		permissiveUmask: true,
	})
	if err != nil {
		return 0, err
	}
	if out.status != 0 || out.staged != sectionSix(fakeToken, gateOrigin) {
		fail("clear then stage did not write the section-6 variables (exit %d)", out.status)
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

	fmt.Fprintln(stdout, "case: FORGE_READ_TOKEN wins over the tokens file and the origin is overridable")
	envToken := fakeToken + "0env"
	out, err = invoke(root, work, "env-token", invocation{
		targets:    []string{"stage-gomodauth"},
		tokensFile: tokensFile,
		env:        []string{"FORGE_READ_TOKEN=" + envToken, "GITEA_HTTP_ORIGIN=https://gitea.invalid:8443/"},
	})
	if err != nil {
		return 0, err
	}
	if out.status != 0 {
		fail("stage-gomodauth exited %d:\n%s", out.status, out.output)
	}
	if out.staged != sectionSix(envToken, "https://gitea.invalid:8443") {
		fail("the staged file was not built from FORGE_READ_TOKEN against the overridden origin")
	}
	noLeak("environment token", out)

	fmt.Fprintln(stdout, "case: a token with no origin anywhere is refused and nothing is staged")
	out, err = invoke(root, work, "no-origin", invocation{targets: []string{"stage-gomodauth"}, tokensFile: tokensFile})
	if err != nil {
		return 0, err
	}
	if out.status == 0 {
		fail("a token with no origin was staged against a default forge — the Makefile must name none")
	}
	if !strings.Contains(out.output, "no Gitea origin is set") {
		fail("refusing a token with no origin did not say why:\n%s", out.output)
	}
	if out.staged != "" {
		fail("a token with no origin left something in the staged file")
	}
	noLeak("no origin", out)

	fmt.Fprintln(stdout, "case: the origin in the tokens file is used when the environment sets none")
	fileOrigin := "https://forge.invalid:8443"
	out, err = invoke(root, work, "file-origin", invocation{
		targets:    []string{"stage-gomodauth"},
		tokensFile: tokensFile + "export GITEA_HTTP_ORIGIN=\"" + fileOrigin + "/\"\n",
	})
	if err != nil {
		return 0, err
	}
	if out.status != 0 || out.staged != sectionSix(fakeToken, fileOrigin) {
		fail("the tokens file's GITEA_HTTP_ORIGIN was not used (exit %d):\n%s", out.status, out.output)
	}
	noLeak("tokens-file origin", out)

	fmt.Fprintln(stdout, "case: a Gitea runner's own variables are staged as injected")
	runnerKey := "url.http://x:" + fakeToken + "@gitea.invalid/jeremyhahn/.insteadOf"
	out, err = invoke(root, work, "runner", invocation{
		targets:    []string{"stage-gomodauth"},
		tokensFile: "FORGE_READ_TOKEN=ignored0token\n",
		env: []string{
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=" + runnerKey, "GIT_CONFIG_VALUE_0=https://github.com/jeremyhahn/",
		},
	})
	if err != nil {
		return 0, err
	}
	want := "export GIT_CONFIG_COUNT=1\n" +
		"export GIT_CONFIG_KEY_0=" + runnerKey + "\nexport GIT_CONFIG_VALUE_0=https://github.com/jeremyhahn/\n"
	if out.status != 0 || out.staged != want {
		fail("the runner's variables were not staged as injected (exit %d)", out.status)
	}
	noLeak("runner", out)

	fmt.Fprintln(stdout, "case: with no credentials anywhere the file is empty and the build still proceeds")
	out, err = invoke(root, work, "none", invocation{targets: []string{"stage-gomodauth"}})
	if err != nil {
		return 0, err
	}
	if out.status != 0 {
		fail("stage-gomodauth without credentials exited %d — GitHub builds rely on the public proxy:\n%s", out.status, out.output)
	}
	if !out.stagedExists || out.staged != "" {
		fail("without credentials the staged file must exist and be empty")
	}

	fmt.Fprintln(stdout, "case: a value that is not token-shaped is refused without being echoed")
	bad := fakeToken + "'$(id)"
	out, err = invoke(root, work, "bad-token", invocation{targets: []string{"stage-gomodauth"}, env: []string{"FORGE_READ_TOKEN=" + bad}})
	if err != nil {
		return 0, err
	}
	if out.status == 0 {
		fail("a token that would break the sourced file was staged")
	}
	if out.staged != "" {
		fail("a refused token left something in the staged file")
	}
	noLeak("malformed token", out)

	for _, tc := range targetCases {
		fmt.Fprintf(stdout, "case: %s stages the credentials for its build and clears them after\n", tc.target)
		out, err := invoke(root, work, tc.target, invocation{
			targets:    []string{tc.target},
			makeVars:   tc.makeVars,
			tokensFile: tokensFile,
			env:        gateOriginEnv,
			failMatch:  tc.failMatch,
		})
		if err != nil {
			return 0, err
		}
		checkTarget(tc, out, fail)
		noLeak(tc.target, out)
	}

	for _, ic := range interruptCases {
		fmt.Fprintf(stdout, "case: interrupting %s during its build still clears the credentials\n", ic.target)
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
		case hangSize <= 0:
			fail("%s was interrupted in %q without the credentials staged", ic.target, ic.hang)
		}
		if !out.stagedExists || out.staged != "" {
			fail("%s left the credentials in the staged file when interrupted", ic.target)
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
	case out.watch[buildAt].size <= 0:
		fail("%s never staged the credentials: its image build saw an empty credentials file", tc.target)
	case out.watch[buildAt].size != stagedSize:
		fail("%s built its image with %d bytes of credentials, not the %d-byte section-6 file stage-gomodauth writes",
			tc.target, out.watch[buildAt].size, stagedSize)
	}
	if tc.after != "" && buildAt >= 0 {
		seen := false
		for _, c := range out.watch[buildAt+1:] {
			if strings.Contains(c.call, tc.after) {
				seen = true
				if c.size != 0 {
					fail("%s still held the credentials at %q, after its build", tc.target, tc.after)
				}
				break
			}
		}
		if !seen {
			fail("%s never reached %q after its build", tc.target, tc.after)
		}
	}
	if !out.stagedExists || out.staged != "" {
		fail("%s left the credentials in the staged file", tc.target)
	}
}
