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

// Command verifycleandocker pins the three halves of clean-docker's contract: it
// covers every image this repository creates under a name it writes literally, it
// removes those and nothing else, and it never reports success after a removal
// failed.
//
// This target used to be `docker system prune -af --volumes`, which on the
// shared CI runners deleted every unused image and named volume on the host and
// repeatedly destroyed another job's image (#3). The scope checks are what keep
// it from drifting back. The coverage check requires DOCKER_LOCAL_IMAGES to
// equal the inventory scripts/internal/dockerinv derives from the build commands
// themselves — in both directions, because a tag the list holds but no build
// produces is another repository's, and reaching it is the failure #3 is about.
// That inventory is derived from producers alone, so a `docker rmi` this
// repository happens to run cannot authorize the tag it removes; the fixture
// below holds that line, since a removal-sourced inventory would let every
// cleanup tag vouch for itself. The failure checks cover the shell trap that a
// `for` loop reports only its last command's status, so an early `docker rmi`
// failure followed by an absent tag would otherwise exit 0.
//
// Docker is faked through PATH by this binary impersonating itself — see
// scripts/internal/gate/fakedocker.go. No daemon, no images, no host state.
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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/dockerinv"
	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

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

// ThinInventoryError reports that the repository derived too few image
// identities for the behaviour cases to say anything. They need one tag the
// daemon reports and one it does not, so fewer than two means the gate has
// stopped exercising cleanup rather than that cleanup is correct.
type ThinInventoryError struct {
	Count int
}

func (e *ThinInventoryError) Error() string {
	return fmt.Sprintf("expected the inventory to derive at least two images, derived %d", e.Count)
}

// gateRun accumulates the failures of one pass so every check reports rather
// than only the first.
type gateRun struct {
	stdout   io.Writer
	stderr   io.Writer
	failures int
}

func (g *gateRun) fail(format string, args ...any) {
	fmt.Fprintf(g.stderr, "  ✗ "+format+"\n", args...)
	g.failures++
}

func (g *gateRun) announce(name string) {
	fmt.Fprintln(g.stdout, "case: "+name)
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verifycleandocker", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root whose clean-docker target is verified")
	if err := fs.Parse(args); err != nil {
		return gate.ExitToolError
	}

	work, err := os.MkdirTemp("", "verifycleandocker-")
	if err != nil {
		fmt.Fprintln(stderr, "verify-clean-docker:", (&WorkspaceError{Err: err}).Error())
		return gate.ExitToolError
	}
	defer func() {
		if rerr := os.RemoveAll(work); rerr != nil {
			fmt.Fprintln(stderr, "verify-clean-docker: cannot remove the workspace:", rerr)
		}
	}()

	if err := gate.InstallFakes(filepath.Join(work, "bin")); err != nil {
		fmt.Fprintln(stderr, "verify-clean-docker:", (&WorkspaceError{Err: err}).Error())
		return gate.ExitToolError
	}

	g := &gateRun{stdout: stdout, stderr: stderr}
	if err := check(g, *root, work); err != nil {
		fmt.Fprintln(stderr, "verify-clean-docker:", err)
		// A pass that could not finish AND found something wrong reports the
		// finding: the contract is broken either way, and calling it a tool
		// error would tell CI the gate never looked.
		if g.failures != 0 {
			fmt.Fprintf(stderr, "verify-clean-docker: %d check(s) failed\n", g.failures)
			return gate.ExitFindings
		}
		return gate.ExitToolError
	}
	if g.failures != 0 {
		fmt.Fprintf(stderr, "verify-clean-docker: %d check(s) failed\n", g.failures)
		return gate.ExitFindings
	}
	fmt.Fprintln(stdout, "verify-clean-docker: clean-docker covers this project's images, only those, and fails closed")
	return gate.ExitClean
}

// check runs every case, returning an error only when the gate itself could not
// run. A contract violation is recorded on g and does not stop the pass.
func check(g *gateRun, root, work string) error {
	tags, err := checkCoverage(g, root)
	if err != nil {
		return err
	}
	if err := checkDerivation(g, work); err != nil {
		return err
	}
	return checkBehaviour(g, root, work, tags)
}

// coverage is what a repository creates set against what its cleanup removes.
type coverage struct {
	// created is the inventory, derived from the build commands rather than from
	// DOCKER_LOCAL_IMAGES, which is what lets this see a drift in that list.
	created []string
	// missing is created but never cleaned: images left behind.
	missing []string
	// extra is cleaned but never created: tags outside this repository's own.
	extra []string
}

// compareCoverage derives the inventory for the repository at root and diffs it
// against that repository's DOCKER_LOCAL_IMAGES, in both directions.
//
// A derivation failure is returned rather than reported as "nothing missing":
// an inventory that quietly dropped a producer would read here as coverage.
func compareCoverage(root string) (coverage, error) {
	created, err := dockerinv.Derive(root)
	if err != nil {
		return coverage{}, err
	}
	listed, err := gate.MakeVar(root, "DOCKER_LOCAL_IMAGES")
	if err != nil {
		// An empty DOCKER_LOCAL_IMAGES is a legitimate fixture: it is how the
		// gate asks what an uncovered repository looks like.
		var empty *gate.EmptyMakeVarError
		if !errors.As(err, &empty) {
			return coverage{}, err
		}
		listed = ""
	}
	cleaned := map[string]bool{}
	for _, tag := range strings.Fields(listed) {
		cleaned[tag] = true
	}
	createdSet := map[string]bool{}
	for _, tag := range created {
		createdSet[tag] = true
	}

	cov := coverage{created: created}
	for _, tag := range created {
		if !cleaned[tag] {
			cov.missing = append(cov.missing, tag)
		}
	}
	for tag := range cleaned {
		if !createdSet[tag] {
			cov.extra = append(cov.extra, tag)
		}
	}
	return cov, nil
}

// checkCoverage is the first case: what this repository creates and what
// clean-docker removes must be the same set. It returns the derived inventory so
// the behaviour cases take their tags from producers rather than from the list
// under test — a tag added to DOCKER_LOCAL_IMAGES without a producer cannot
// vouch for itself.
func checkCoverage(g *gateRun, root string) ([]string, error) {
	g.announce("cleanup covers exactly the images this repository creates")
	cov, err := compareCoverage(root)
	if err != nil {
		// The behaviour cases take their tags from here, so an inventory that
		// could not be derived stops the pass rather than letting it run
		// against a tag set nobody established.
		g.fail("the image inventory could not be derived: %v", err)
		return nil, &ThinInventoryError{Count: 0}
	}
	if len(cov.created) == 0 {
		g.fail("derived no image identities at all — the inventory has stopped matching the build commands")
	}
	if len(cov.missing) > 0 {
		g.fail("this repository creates images clean-docker leaves behind: %s", strings.Join(cov.missing, " "))
		g.fail("add them to DOCKER_LOCAL_IMAGES in the Makefile")
	}
	if len(cov.extra) > 0 {
		g.fail("clean-docker removes tags this repository does not create: %s", strings.Join(cov.extra, " "))
		g.fail("cleanup may only reach this repository's own images — see #3")
	}
	if len(cov.created) < 2 {
		return nil, &ThinInventoryError{Count: len(cov.created)}
	}
	return cov.created, nil
}

// checkDerivation is the group of cases that hold the derivation honest, each
// against a self-contained fixture repository.
func checkDerivation(g *gateRun, work string) error {
	// A build spread over continuation lines and a Compose service's explicit
	// `image:` are both easy for a line-at-a-time scan to miss, and missing
	// either would let the coverage check pass while cleanup leaves images
	// behind. Neither is listed in this fixture, so both must come back as gaps.
	g.announce("a multiline build and a compose image: tag are both seen as gaps")
	gaps := filepath.Join(work, "gaps")
	if err := writeFixture(fixture{dir: gaps, built: "fixture-multiline:latest"}); err != nil {
		return err
	}
	if cov, err := compareCoverage(gaps); err != nil {
		g.fail("the fixture inventory could not be derived: %v", err)
	} else {
		if !contains(cov.missing, "fixture-multiline:latest") {
			g.fail("a 'docker build -t' spread over continuation lines was not derived")
		}
		if !contains(cov.missing, "fixture-compose:latest") {
			g.fail("the explicit image: of a building compose service was not derived")
		}
		if contains(cov.missing, "redis:7-alpine") {
			g.fail("claimed an image the repository only pulls")
		}
	}

	// The other direction. A tag nothing here builds is another repository's,
	// and cleanup reaching it is the whole failure #3 is about — so listing one
	// has to fail verification, not be blessed by it.
	g.announce("a cleanup tag this repository does not create is rejected")
	foreign := filepath.Join(work, "foreign")
	if err := writeFixture(fixture{
		dir:     foreign,
		cleaned: "fixture-multiline:latest fixture-compose:latest someone-elses/image:latest",
		built:   "fixture-multiline:latest",
	}); err != nil {
		return err
	}
	if cov, err := compareCoverage(foreign); err != nil {
		g.fail("the fixture inventory could not be derived: %v", err)
	} else {
		if !contains(cov.extra, "someone-elses/image:latest") {
			g.fail("a cleanup tag no build produces was not reported as out of scope")
		}
		if len(cov.missing) > 0 {
			g.fail("reported a gap in a fixture that lists everything it builds: %s", strings.Join(cov.missing, " "))
		}
	}

	// Ownership has to come from producers alone. A repository that removes a
	// tag has said nothing about creating it, so pairing `docker rmi
	// someone-elses/...` with the same entry in DOCKER_LOCAL_IMAGES must not
	// launder it into scope — otherwise every cleanup tag authorizes itself and
	// the check means nothing.
	g.announce("a 'docker rmi' does not make the tag it removes ours")
	laundered := filepath.Join(work, "laundered")
	if err := writeFixture(fixture{
		dir:     laundered,
		cleaned: "fixture-multiline:latest fixture-compose:latest someone-elses/image:latest",
		built:   "fixture-multiline:latest",
		removed: "someone-elses/image:latest",
	}); err != nil {
		return err
	}
	if cov, err := compareCoverage(laundered); err != nil {
		g.fail("the fixture inventory could not be derived: %v", err)
	} else {
		if contains(cov.created, "someone-elses/image:latest") {
			g.fail("a 'docker rmi' argument was treated as an image this repository creates")
		}
		if !contains(cov.extra, "someone-elses/image:latest") {
			g.fail("a foreign tag removed but never built was not reported as out of scope")
		}
	}

	// The scan has to reach every script in the repository, not just
	// `scripts/*.sh`. A build hidden a few directories down is an image cleanup
	// would leave behind, and a scan that cannot see it reports the coverage
	// check as clean.
	g.announce("a producer in a nested script is still seen")
	nested := filepath.Join(work, "nested")
	if err := writeFixture(fixture{
		dir:    nested,
		built:  "fixture-multiline:latest",
		nested: "fixture-nested:latest",
	}); err != nil {
		return err
	}
	if cov, err := compareCoverage(nested); err != nil {
		g.fail("the fixture inventory could not be derived: %v", err)
	} else if !contains(cov.missing, "fixture-nested:latest") {
		g.fail("a 'docker build -t' in a script below the top level was not derived")
	}

	// A producer named by a variable resolved at run time cannot be cleaned up
	// by name, so it must break the inventory rather than drop out of it —
	// otherwise an image that escapes cleanup reads here as "nothing missing".
	g.announce("a build tag that cannot be resolved to a literal fails the inventory")
	unresolved := filepath.Join(work, "unresolved")
	if err := writeFixture(fixture{dir: unresolved, built: `"$$RUNTIME_TAG"`}); err != nil {
		return err
	}
	if _, err := compareCoverage(unresolved); err == nil {
		g.fail("an unresolvable build tag was silently dropped from the inventory")
	}
	return nil
}

// checkBehaviour is the group of cases that run clean-docker itself against a
// faked daemon.
func checkBehaviour(g *gateRun, root, work string, tags []string) error {
	presentTag, absentTag := tags[0], tags[1]

	g.announce("only this project's tags, and only the ones the daemon reports")
	if err := writeControl(work, map[string]string{
		"images.out": presentTag + "\nsomeone-elses/image:latest\n",
		"images.rc":  "0\n",
		"rmi.fail":   "",
	}); err != nil {
		return err
	}
	status, calls, err := runCleanDocker(root, work)
	if err != nil {
		return err
	}
	if status != 0 {
		g.fail("a clean run exited %d", status)
	}
	if !calledExactly(calls, "rmi "+presentTag) {
		g.fail("the project tag the daemon reported was not removed")
	}
	if calledExactly(calls, "rmi "+absentTag) {
		g.fail("removed a tag the daemon never reported as present")
	}
	if calledContaining(calls, "someone-elses/image") {
		g.fail("touched an image that is not this project's")
	}
	if calledContaining(calls, "system prune") {
		g.fail("pruned the host — see #3")
	}
	for _, beyond := range []string{"volume rm", "network rm"} {
		if calledContaining(calls, beyond) {
			g.fail("tore down resources beyond this project's image tags")
		}
	}
	if calledComposeDown(calls) {
		g.fail("tore down resources beyond this project's image tags")
	}
	// These tags are fixed, so a concurrent checkout on a shared runner can be
	// using one. Forcing would untag it out from under that run.
	if calledForcedRemoval(calls) {
		g.fail("forced removal of a shared fixed tag")
	}

	g.announce("an early removal failure survives a later absent tag")
	if err := writeControl(work, map[string]string{
		"images.out": presentTag + "\n",
		"images.rc":  "0\n",
		"rmi.fail":   presentTag + "\n",
	}); err != nil {
		return err
	}
	status, _, err = runCleanDocker(root, work)
	if err != nil {
		return err
	}
	if status == 0 {
		g.fail("reported success after 'docker rmi' failed")
	}

	g.announce("an unreachable daemon is not read as 'no such image'")
	if err := writeControl(work, map[string]string{
		"images.out": "",
		"images.rc":  "1\n",
		"rmi.fail":   "",
	}); err != nil {
		return err
	}
	status, calls, err = runCleanDocker(root, work)
	if err != nil {
		return err
	}
	if status == 0 {
		g.fail("reported success after 'docker images' failed")
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "rmi") {
			g.fail("removed images without knowing which exist")
			break
		}
	}
	return nil
}

// runCleanDocker runs `make clean-docker` in root against the faked daemon in
// work, returning its exit status and every call the fake recorded.
func runCleanDocker(root, work string) (int, []string, error) {
	callsPath := filepath.Join(work, "calls.log")
	if err := os.WriteFile(callsPath, nil, 0o600); err != nil {
		return 0, nil, &WorkspaceError{Err: err}
	}

	cmd := exec.Command("make", "--no-print-directory", "clean-docker")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(work, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		gate.FakeDirEnv+"="+work,
	)
	// The recipe's own output is the gate's diagnostic when a case fails; the
	// verdict comes from the call log and the status, never from this text.
	var sink strings.Builder
	cmd.Stdout = &sink
	cmd.Stderr = &sink
	status := 0
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return 0, nil, err
		}
		status = exit.ExitCode()
	}

	raw, err := os.ReadFile(callsPath)
	if err != nil {
		return 0, nil, &WorkspaceError{Err: err}
	}
	var calls []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return status, calls, nil
}

// fixture describes a self-contained repository to write.
type fixture struct {
	// dir is where the repository is written.
	dir string
	// cleaned is its DOCKER_LOCAL_IMAGES.
	cleaned string
	// built is the tag its sole Makefile build produces. The build is spread
	// over continuation lines because a line-at-a-time scan would miss it.
	built string
	// removed, when set, is a tag the fixture only ever removes and never
	// builds.
	removed string
	// nested, when set, is built by a script nested well below the top level,
	// where a sweep of `scripts/*.sh` alone would never look.
	nested string
}

// writeFixture writes the fixture repository, plus a compose file that builds
// fixture-compose:latest and pulls redis:7-alpine.
func writeFixture(f fixture) error {
	if err := os.MkdirAll(f.dir, 0o750); err != nil {
		return &WorkspaceError{Err: err}
	}
	if f.nested != "" {
		// A SHELL SCRIPT ON PURPOSE, and never executed: it is input to the
		// producer scan, which reads the Makefile and shell scripts because
		// that is where every producer in this tree lives — the container
		// entrypoints, the devcontainer hooks and .github/scripts are all
		// shell by contract. The fixture has to be the shape the scanner must
		// read, not the shape this gate is written in.
		toolsDir := filepath.Join(f.dir, "test", "tools")
		if err := os.MkdirAll(toolsDir, 0o750); err != nil {
			return &WorkspaceError{Err: err}
		}
		script := "#!/usr/bin/env bash\ndocker build -t " + f.nested + " .\n"
		if err := os.WriteFile(filepath.Join(toolsDir, "build-image.sh"), []byte(script), 0o600); err != nil {
			return &WorkspaceError{Err: err}
		}
	}

	var mk strings.Builder
	mk.WriteString("DOCKER_IMAGE := fixture\n")
	mk.WriteString("DOCKER_TAG := 9.9.9\n")
	mk.WriteString("DOCKER_REGISTRY := registry.invalid\n")
	mk.WriteString("DOCKER_LOCAL_IMAGES := " + f.cleaned + "\n\n")
	mk.WriteString("print-%:\n\t@echo \"$($*)\"\n\n")
	mk.WriteString(".PHONY: fixture-build\nfixture-build:\n")
	mk.WriteString("\tdocker build \\\n")
	mk.WriteString("\t\t-f Dockerfile.fixture \\\n")
	mk.WriteString("\t\t-t " + f.built + " \\\n")
	mk.WriteString("\t\t.\n")
	if f.removed != "" {
		mk.WriteString("\n.PHONY: fixture-remove\nfixture-remove:\n")
		mk.WriteString("\tdocker rmi " + f.removed + "\n")
	}
	if err := os.WriteFile(filepath.Join(f.dir, "Makefile"), []byte(mk.String()), 0o600); err != nil {
		return &WorkspaceError{Err: err}
	}

	compose := "services:\n" +
		"  built:\n" +
		"    image: fixture-compose:latest\n" +
		"    build:\n" +
		"      context: .\n" +
		"  pulled:\n" +
		"    image: redis:7-alpine\n"
	if err := os.WriteFile(filepath.Join(f.dir, "docker-compose.yml"), []byte(compose), 0o600); err != nil {
		return &WorkspaceError{Err: err}
	}
	return nil
}

// writeControl sets the faked daemon's behaviour for the next case.
func writeControl(work string, files map[string]string) error {
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0o600); err != nil {
			return &WorkspaceError{Err: err}
		}
	}
	return nil
}

// calledExactly reports whether the recipe made exactly this call.
func calledExactly(calls []string, want string) bool {
	return contains(calls, want)
}

// calledContaining reports whether any call mentions the text.
func calledContaining(calls []string, text string) bool {
	for _, call := range calls {
		if strings.Contains(call, text) {
			return true
		}
	}
	return false
}

// calledComposeDown reports whether the recipe brought a Compose stack down,
// which is beyond removing this project's image tags.
func calledComposeDown(calls []string) bool {
	for _, call := range calls {
		if strings.HasPrefix(call, "compose ") && strings.Contains(call, "down") {
			return true
		}
	}
	return false
}

// calledForcedRemoval reports whether any removal was forced.
func calledForcedRemoval(calls []string) bool {
	for _, call := range calls {
		if !strings.HasPrefix(call, "rmi ") {
			continue
		}
		for _, field := range strings.Fields(call) {
			if field == "-f" || field == "--force" {
				return true
			}
		}
	}
	return false
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
