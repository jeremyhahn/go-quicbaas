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

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// TestMain dispatches the faked executables before the suite runs. PATH resolves
// this test binary as `docker` inside the cases below, and without the dispatch
// every docker call the recipe makes would re-enter the whole suite.
func TestMain(m *testing.M) {
	gate.DispatchFake()
	os.Exit(m.Run())
}

// cleanupRecipes are the clean-docker recipes a fixture repository can carry.
const (
	// faithful is the shape the real recipe has: one daemon call decides what
	// exists so its failure propagates, removal failures accumulate rather than
	// only the last one counting, and nothing is forced.
	faithful = "\t@present=$$(docker images --format '{{.Repository}}:{{.Tag}}') || exit $$?; \\\n" +
		"\tstatus=0; \\\n" +
		"\tfor image in $(DOCKER_LOCAL_IMAGES); do \\\n" +
		"\t\tif printf '%s\\n' \"$$present\" | grep -qxF \"$$image\"; then \\\n" +
		"\t\t\tdocker rmi \"$$image\" || status=1; \\\n" +
		"\t\tfi; \\\n" +
		"\tdone; \\\n" +
		"\texit $$status\n"

	// prunesTheHost is #3 itself: on a shared runner this deletes every unused
	// image and named volume on the host, including other repositories'.
	prunesTheHost = "\t@docker system prune -af --volumes\n"

	// forcesRemoval untags a fixed tag out from under a concurrent checkout of
	// the same repository on the same runner.
	forcesRemoval = "\t@for image in $(DOCKER_LOCAL_IMAGES); do docker rmi -f \"$$image\"; done\n"

	// swallowsFailures is the shell trap the failure checks exist for: a `for`
	// loop reports only its last command's status.
	swallowsFailures = "\t@for image in $(DOCKER_LOCAL_IMAGES); do docker rmi \"$$image\" || true; done\n"

	// removesBlind never asks the daemon what exists, so an unreachable daemon
	// reads as "no such image" and the run reports success.
	removesBlind = "\t@docker images --format '{{.Repository}}:{{.Tag}}' > /dev/null; \\\n" +
		"\tfor image in $(DOCKER_LOCAL_IMAGES); do docker rmi \"$$image\" || true; done\n"

	// reachesTooFar brings a Compose stack down as well, which is beyond
	// removing this repository's image tags.
	reachesTooFar = "\t@docker compose -f c.yml down --volumes; \\\n" +
		"\tfor image in $(DOCKER_LOCAL_IMAGES); do docker rmi \"$$image\"; done\n"

	// doesNothing is cleanup that has quietly stopped cleaning up. It reports
	// success on every path, including the one where the daemon is unreachable.
	doesNothing = "\t@true\n"
)

// writeRepo builds a fixture repository whose coverage is correct — every tag it
// builds is listed and nothing else is — and whose clean-docker carries the given
// recipe. The behaviour cases then report on the recipe alone.
func writeRepo(t *testing.T, recipe string) string {
	t.Helper()
	dir := t.TempDir()
	body := "DOCKER_IMAGE := fixture\nDOCKER_TAG := 9.9.9\nDOCKER_REGISTRY := registry.invalid\n\n" +
		"DOCKER_LOCAL_IMAGES := fixture-a:latest fixture-b:latest fixture-compose:latest\n\n" +
		"print-%:\n\t@echo \"$($*)\"\n\n" +
		".PHONY: build\nbuild:\n" +
		"\tdocker build \\\n\t\t-t fixture-a:latest \\\n\t\t.\n" +
		"\tdocker build -t fixture-b:latest .\n\n" +
		".PHONY: clean-docker\nclean-docker:\n" + recipe
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture Makefile: %v", err)
	}
	compose := "services:\n  built:\n    image: fixture-compose:latest\n    build:\n      context: .\n" +
		"  pulled:\n    image: redis:7-alpine\n"
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("writing the fixture compose file: %v", err)
	}
	return dir
}

// arm prepares the workspace the behaviour cases run in.
func arm(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	if err := gate.InstallFakes(filepath.Join(work, "bin")); err != nil {
		t.Fatalf("installing the fakes: %v", err)
	}
	return work
}

func TestCheckPassesARepositoryThatHoldsTheContract(t *testing.T) {
	var stdout, stderr strings.Builder
	g := &gateRun{stdout: &stdout, stderr: &stderr}
	if err := check(g, writeRepo(t, faithful), arm(t)); err != nil {
		t.Fatalf("check: %v", err)
	}
	if g.failures != 0 {
		t.Fatalf("a faithful repository reported %d failure(s):\n%s", g.failures, stderr.String())
	}
	// Every case must have been announced, or a silently skipped one would read
	// as a pass.
	for _, want := range []string{
		"cleanup covers exactly the images this repository creates",
		"a multiline build and a compose image: tag are both seen as gaps",
		"a cleanup tag this repository does not create is rejected",
		"a 'docker rmi' does not make the tag it removes ours",
		"a producer in a nested script is still seen",
		"a build tag that cannot be resolved to a literal fails the inventory",
		"only this project's tags, and only the ones the daemon reports",
		"an early removal failure survives a later absent tag",
		"an unreachable daemon is not read as 'no such image'",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("the case %q was not run:\n%s", want, stdout.String())
		}
	}
}

func TestCheckFailsOnEachWayCleanupCanReachTooFarOrHideAFailure(t *testing.T) {
	// THE CASES THAT MATTER. Each recipe is a real failure mode; #3 is the first
	// of them, and a gate that cannot report any of these is worse than none.
	cases := []struct {
		name   string
		recipe string
		want   string
	}{
		{name: "pruning the host", recipe: prunesTheHost, want: "pruned the host"},
		{name: "forcing removal of a shared fixed tag", recipe: forcesRemoval, want: "forced removal of a shared fixed tag"},
		{name: "swallowing a removal failure", recipe: swallowsFailures, want: "reported success after 'docker rmi' failed"},
		{name: "removing without asking what exists", recipe: removesBlind, want: "reported success after 'docker images' failed"},
		{name: "tearing down beyond the image tags", recipe: reachesTooFar, want: "tore down resources beyond this project's image tags"},
		{name: "not cleaning up at all", recipe: doesNothing, want: "the project tag the daemon reported was not removed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := writeRepo(t, tc.recipe)
			// Only the behaviour cases are at issue here: the derivation cases
			// run against their own fixtures and are covered once, by the
			// compliant pass above.
			cov, err := compareCoverage(repo)
			if err != nil {
				t.Fatalf("compareCoverage: %v", err)
			}
			var stdout, stderr strings.Builder
			g := &gateRun{stdout: &stdout, stderr: &stderr}
			if err := checkBehaviour(g, repo, arm(t), cov.created); err != nil {
				t.Fatalf("checkBehaviour: %v", err)
			}
			if g.failures == 0 {
				t.Fatalf("%s must fail the gate", tc.name)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("the failure does not report %q:\n%s", tc.want, stderr.String())
			}
		})
	}
}

func TestCompareCoverageSeesADriftInEitherDirection(t *testing.T) {
	cases := []struct {
		name        string
		cleaned     string
		wantMissing []string
		wantExtra   []string
	}{
		{
			name:    "everything built is listed",
			cleaned: "fixture-a:latest fixture-b:latest fixture-compose:latest",
		},
		{
			// An image cleanup leaves behind: the first half of #3's cost.
			name:        "a built tag that cleanup does not know about",
			cleaned:     "fixture-a:latest fixture-compose:latest",
			wantMissing: []string{"fixture-b:latest"},
		},
		{
			// A tag nothing here builds is another repository's, and reaching
			// it is the whole failure #3 is about.
			name:      "a cleanup tag no build produces",
			cleaned:   "fixture-a:latest fixture-b:latest fixture-compose:latest someone-elses/image:latest",
			wantExtra: []string{"someone-elses/image:latest"},
		},
		{
			name:        "an empty list leaves everything behind",
			cleaned:     "",
			wantMissing: []string{"fixture-a:latest", "fixture-b:latest", "fixture-compose:latest"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			body := "DOCKER_IMAGE := fixture\nDOCKER_TAG := 9.9.9\nDOCKER_REGISTRY := registry.invalid\n\n" +
				"DOCKER_LOCAL_IMAGES := " + tc.cleaned + "\n\n" +
				"print-%:\n\t@echo \"$($*)\"\n\n" +
				".PHONY: build\nbuild:\n\tdocker build -t fixture-a:latest .\n\tdocker build -t fixture-b:latest .\n"
			if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
				t.Fatalf("writing the fixture Makefile: %v", err)
			}
			compose := "services:\n  built:\n    image: fixture-compose:latest\n    build:\n      context: .\n"
			if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose), 0o644); err != nil {
				t.Fatalf("writing the fixture compose file: %v", err)
			}

			cov, err := compareCoverage(dir)
			if err != nil {
				t.Fatalf("compareCoverage: %v", err)
			}
			assertSet(t, "missing", cov.missing, tc.wantMissing)
			assertSet(t, "extra", cov.extra, tc.wantExtra)
		})
	}
}

func TestCompareCoverageRefusesAnInventoryItCannotComplete(t *testing.T) {
	// An inventory that quietly dropped a producer would read here as coverage,
	// so the failure has to propagate rather than become "nothing missing".
	dir := t.TempDir()
	body := "DOCKER_IMAGE := fixture\nDOCKER_TAG := 9.9.9\nDOCKER_REGISTRY := registry.invalid\n\n" +
		"DOCKER_LOCAL_IMAGES :=\n\nprint-%:\n\t@echo \"$($*)\"\n\n" +
		".PHONY: build\nbuild:\n\tdocker build -t \"$$RUNTIME_TAG\" .\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture Makefile: %v", err)
	}
	if _, err := compareCoverage(dir); err == nil {
		t.Fatal("an unresolvable producer must fail compareCoverage")
	}
}

func TestWriteFixtureProducesEveryShapeTheDerivationMustSee(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fx")
	if err := writeFixture(fixture{
		dir:     dir,
		cleaned: "listed:latest",
		built:   "fixture-multiline:latest",
		removed: "someone-elses/image:latest",
		nested:  "fixture-nested:latest",
	}); err != nil {
		t.Fatalf("writeFixture: %v", err)
	}

	makefile, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		t.Fatalf("reading the fixture Makefile: %v", err)
	}
	// The build must be spread over continuation lines, because a
	// line-at-a-time scan would miss it and that is the point of the case.
	if !strings.Contains(string(makefile), "docker build \\\n") {
		t.Fatalf("the fixture build is not spread over continuation lines:\n%s", makefile)
	}
	if !strings.Contains(string(makefile), "docker rmi someone-elses/image:latest") {
		t.Fatalf("the fixture does not remove the foreign tag:\n%s", makefile)
	}
	script, err := os.ReadFile(filepath.Join(dir, "test", "tools", "build-image.sh"))
	if err != nil {
		t.Fatalf("reading the nested producer: %v", err)
	}
	if !strings.Contains(string(script), "docker build -t fixture-nested:latest") {
		t.Fatalf("the nested producer does not build the tag:\n%s", script)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Fatalf("the fixture compose file is missing: %v", err)
	}
}

func TestRunReportsTheVerdictAndTheStatus(t *testing.T) {
	t.Run("a compliant repository", func(t *testing.T) {
		var stdout, stderr strings.Builder
		status := run([]string{"-root", writeRepo(t, faithful)}, &stdout, &stderr)
		if status != gate.ExitClean {
			t.Fatalf("run exited %d, want %d\nstderr: %s", status, gate.ExitClean, stderr.String())
		}
		if !strings.Contains(stdout.String(), "covers this project's images, only those, and fails closed") {
			t.Fatalf("the pass was not reported: %q", stdout.String())
		}
	})

	t.Run("a repository that prunes the host", func(t *testing.T) {
		var stdout, stderr strings.Builder
		status := run([]string{"-root", writeRepo(t, prunesTheHost)}, &stdout, &stderr)
		if status != gate.ExitFindings {
			t.Fatalf("run exited %d, want %d", status, gate.ExitFindings)
		}
		if !strings.Contains(stderr.String(), "check(s) failed") {
			t.Fatalf("the count of failures was not reported: %q", stderr.String())
		}
	})

	t.Run("a root that is not a repository", func(t *testing.T) {
		var stdout, stderr strings.Builder
		status := run([]string{"-root", t.TempDir()}, &stdout, &stderr)
		if status != gate.ExitFindings {
			t.Fatalf("run exited %d, want %d", status, gate.ExitFindings)
		}
	})

	t.Run("an unknown flag", func(t *testing.T) {
		var stdout, stderr strings.Builder
		if status := run([]string{"-nonesuch"}, &stdout, &stderr); status != gate.ExitToolError {
			t.Fatalf("run exited %d, want %d", status, gate.ExitToolError)
		}
	})
}

func TestCheckCoverageStopsWhenTheInventoryIsTooThinToSayAnything(t *testing.T) {
	// The behaviour cases need one tag the daemon reports and one it does not.
	// Fewer than two derived means the gate has stopped exercising cleanup, which
	// is a tool error and not a clean tree.
	dir := t.TempDir()
	body := "DOCKER_IMAGE := fixture\nDOCKER_TAG := 9.9.9\nDOCKER_REGISTRY := registry.invalid\n\n" +
		"DOCKER_LOCAL_IMAGES := only:one\n\nprint-%:\n\t@echo \"$($*)\"\n\n" +
		".PHONY: build\nbuild:\n\tdocker build -t only:one .\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture Makefile: %v", err)
	}

	var stdout, stderr strings.Builder
	g := &gateRun{stdout: &stdout, stderr: &stderr}
	_, err := checkCoverage(g, dir)
	var thin *ThinInventoryError
	if !errors.As(err, &thin) {
		t.Fatalf("checkCoverage on a one-image repository = %v, want *ThinInventoryError", err)
	}
	if thin.Count != 1 {
		t.Fatalf("the error does not carry the count: %+v", thin)
	}
}

func TestCallInspectorsReadTheRecipesCalls(t *testing.T) {
	calls := []string{
		"images --format {{.Repository}}:{{.Tag}}",
		"rmi fixture-a:latest",
		"compose -f c.yml down --volumes",
		"rmi -f fixture-b:latest",
		"system prune -af --volumes",
	}
	if !calledExactly(calls, "rmi fixture-a:latest") {
		t.Fatal("calledExactly missed a call the recipe made")
	}
	// Exact, not substring: a tag the daemon never reported must not be read as
	// removed because a longer call happens to contain its name.
	if calledExactly(calls, "rmi fixture-a") {
		t.Fatal("calledExactly matched a prefix of a call")
	}
	if !calledContaining(calls, "system prune") {
		t.Fatal("calledContaining missed the prune")
	}
	if !calledComposeDown(calls) {
		t.Fatal("calledComposeDown missed the teardown")
	}
	if !calledForcedRemoval(calls) {
		t.Fatal("calledForcedRemoval missed the forced removal")
	}
	// And none of them fire on a faithful run.
	faithfulCalls := []string{"images --format {{.Repository}}:{{.Tag}}", "rmi fixture-a:latest"}
	if calledContaining(faithfulCalls, "system prune") ||
		calledComposeDown(faithfulCalls) ||
		calledForcedRemoval(faithfulCalls) {
		t.Fatal("a faithful run tripped one of the scope checks")
	}
}

// assertSet compares two collections as sets, since neither the inventory's
// order nor a Makefile list's is part of the contract.
func assertSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSet := map[string]bool{}
	for _, v := range got {
		gotSet[v] = true
	}
	wantSet := map[string]bool{}
	for _, v := range want {
		wantSet[v] = true
	}
	for v := range wantSet {
		if !gotSet[v] {
			t.Fatalf("%s omits %q; got %q", label, v, got)
		}
	}
	for v := range gotSet {
		if !wantSet[v] {
			t.Fatalf("%s reports %q, which is not expected; got %q", label, v, got)
		}
	}
}

func TestCheckCoverageReportsEachDirectionOfDrift(t *testing.T) {
	cases := []struct {
		name    string
		cleaned string
		builds  string
		want    []string
	}{
		{
			name:    "an image cleanup leaves behind",
			cleaned: "fixture-a:latest",
			builds:  "\tdocker build -t fixture-a:latest .\n\tdocker build -t fixture-b:latest .\n",
			want: []string{
				"creates images clean-docker leaves behind: fixture-b:latest",
				"add them to DOCKER_LOCAL_IMAGES in the Makefile",
			},
		},
		{
			name:    "a tag cleanup reaches that nothing here builds",
			cleaned: "fixture-a:latest fixture-b:latest someone-elses/image:latest",
			builds:  "\tdocker build -t fixture-a:latest .\n\tdocker build -t fixture-b:latest .\n",
			want: []string{
				"removes tags this repository does not create: someone-elses/image:latest",
				"cleanup may only reach this repository's own images — see #3",
			},
		},
		{
			name:    "a repository that builds nothing at all",
			cleaned: "",
			builds:  "\t@true\n",
			want:    []string{"derived no image identities at all"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			body := "DOCKER_IMAGE := fixture\nDOCKER_TAG := 9.9.9\nDOCKER_REGISTRY := registry.invalid\n\n" +
				"DOCKER_LOCAL_IMAGES := " + tc.cleaned + "\n\nprint-%:\n\t@echo \"$($*)\"\n\n" +
				".PHONY: build\nbuild:\n" + tc.builds
			if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
				t.Fatalf("writing the fixture Makefile: %v", err)
			}

			var stdout, stderr strings.Builder
			g := &gateRun{stdout: &stdout, stderr: &stderr}
			// The verdict recorded on g is what this case is about. The
			// returned tags and the thin-inventory error are the pass-control
			// path, asserted by TestCheckCoverageStopsWhenTheInventoryIsTooThin.
			tags, stop := checkCoverage(g, dir)
			if len(tags) != 0 && stop != nil {
				t.Fatalf("checkCoverage returned both tags %q and a stop %v", tags, stop)
			}
			if g.failures == 0 {
				t.Fatalf("%s must fail the coverage check", tc.name)
			}
			for _, want := range tc.want {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("the failure omits %q:\n%s", want, stderr.String())
				}
			}
		})
	}
}

func TestWorkspaceFailuresAreReportedAndNotWorkedAround(t *testing.T) {
	cause := errors.New("read-only file system")
	err := &WorkspaceError{Err: cause}
	if !errors.Is(err, cause) {
		t.Fatal("WorkspaceError must unwrap to the underlying failure")
	}
	if !strings.Contains(err.Error(), "read-only file system") {
		t.Fatalf("WorkspaceError.Error() omits the cause: %s", err.Error())
	}

	// A fixture that cannot be written stops the pass rather than being skipped:
	// a case that did not run is not a case that passed.
	occupied := filepath.Join(t.TempDir(), "occupied")
	if werr := os.WriteFile(occupied, []byte("not a directory\n"), 0o644); werr != nil {
		t.Fatalf("preparing the fixture: %v", werr)
	}
	if ferr := writeFixture(fixture{dir: occupied, built: "x:1"}); ferr == nil {
		t.Fatal("writeFixture must fail when its directory cannot be created")
	}
	if cerr := writeControl(filepath.Join(occupied, "nested"), map[string]string{"images.rc": "0\n"}); cerr == nil {
		t.Fatal("writeControl must fail when the file cannot be written")
	}
}

func TestThinInventoryErrorCarriesTheCount(t *testing.T) {
	err := &ThinInventoryError{Count: 1}
	if !strings.Contains(err.Error(), "at least two") {
		t.Fatalf("the error does not say what was expected: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "derived 1") {
		t.Fatalf("the error does not carry the count: %s", err.Error())
	}
}
