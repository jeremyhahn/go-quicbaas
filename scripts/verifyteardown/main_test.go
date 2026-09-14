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
// this test binary as `docker` and `nvidia-smi` inside the cases below, and
// without the dispatch every docker call the recipe makes would re-enter the
// whole suite.
func TestMain(m *testing.M) {
	gate.DispatchFake()
	os.Exit(m.Run())
}

// recipes are the integration-test recipes a fixture repository can carry. Each
// is one answer to "does the stack come down when the run leaves early?".
const (
	// armedTrap is the shape the real recipe has: the teardown is armed BEFORE
	// anything is created, so it runs on every exit path.
	armedTrap = "\ttrap 'docker compose -f c.yml down --volumes --remove-orphans' EXIT; \\\n" +
		"\tdocker compose -f c.yml build --no-cache --pull; \\\n" +
		"\tdocker compose -f c.yml up -d\n"

	// teardownAtTheEnd is the defect: the stack comes down only on the path
	// where the tests actually ran.
	teardownAtTheEnd = "\tdocker compose -f c.yml build --no-cache --pull; \\\n" +
		"\tdocker compose -f c.yml up -d; \\\n" +
		"\tdocker compose -f c.yml down --volumes --remove-orphans\n"

	// prunesTheHost is #3 itself: reclaiming this job's leftovers by deleting
	// every unused image and volume on a shared runner.
	prunesTheHost = "\ttrap 'docker system prune -af --volumes' EXIT; \\\n" +
		"\tdocker compose -f c.yml build --no-cache --pull\n"

	// neverBuilds leaves before the step the case breaks, so the gate is no
	// longer exercising what it claims to.
	neverBuilds = "\ttrap 'docker compose -f c.yml down --volumes --remove-orphans' EXIT; \\\n" +
		"\tdocker compose -f c.yml up -d\n"

	// succeedsAnyway never fails at all, so the gate cannot learn anything from
	// the exit path.
	succeedsAnyway = "\tdocker compose -f c.yml version\n"
)

// writeRepo builds a fixture repository whose integration-test target carries
// the given recipe. The recipe probes for a GPU first, as the real one does, so
// the faked nvidia-smi is exercised too.
func writeRepo(t *testing.T, recipe string) string {
	t.Helper()
	dir := t.TempDir()
	body := ".PHONY: integration-test\nintegration-test:\n" +
		"\t@if command -v nvidia-smi > /dev/null 2>&1 && nvidia-smi > /dev/null 2>&1; then \\\n" +
		"\t\techo \"gpu detected\"; \\\n" +
		"\tfi\n" +
		"\t@set -e; \\\n" + recipe
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture Makefile: %v", err)
	}
	return dir
}

// arm prepares the workspace the gate's case runs in: the fakes on PATH and the
// build step marked as the one that fails.
func arm(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	if err := gate.InstallFakes(filepath.Join(work, "bin")); err != nil {
		t.Fatalf("installing the fakes: %v", err)
	}
	return work
}

func TestCheckPassesWhenTheStackComesDownOnTheEarlyExit(t *testing.T) {
	var stdout, stderr strings.Builder
	failures, err := check(writeRepo(t, armedTrap), arm(t), &stdout, &stderr)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failures != 0 {
		t.Fatalf("a recipe that arms its teardown reported %d failure(s):\n%s", failures, stderr.String())
	}
	if !strings.Contains(stdout.String(), "case: a failed image build still brings the stack down") {
		t.Fatalf("the case was not announced: %q", stdout.String())
	}
}

func TestCheckFailsWhenTeardownDependsOnReachingTheEnd(t *testing.T) {
	// THE CASE THAT MATTERS, and the defect #3 is about: nothing behind this job
	// collects what it leaves, so a build that fails must still bring the stack
	// down.
	var stdout, stderr strings.Builder
	failures, err := check(writeRepo(t, teardownAtTheEnd), arm(t), &stdout, &stderr)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failures == 0 {
		t.Fatal("a recipe that tears down only on the success path must fail the gate")
	}
	if !strings.Contains(stderr.String(), "the stack was left up after the build failed") {
		t.Fatalf("the failure does not say what went wrong:\n%s", stderr.String())
	}
	// The diagnostic has to carry the calls, or the reader cannot see it.
	if !strings.Contains(stderr.String(), "--- docker calls ---") {
		t.Fatalf("the failure carries no diagnostic:\n%s", stderr.String())
	}
}

func TestCheckFailsOnEachWayTheContractCanBreak(t *testing.T) {
	cases := []struct {
		name   string
		recipe string
		want   string
	}{
		{
			name:   "pruning the host",
			recipe: prunesTheHost,
			want:   "pruned the host",
		},
		{
			name:   "never reaching the step the case breaks",
			recipe: neverBuilds,
			want:   "never reached the image build",
		},
		{
			name:   "reporting success when the build failed",
			recipe: succeedsAnyway,
			want:   "reported success after the image build failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			failures, err := check(writeRepo(t, tc.recipe), arm(t), &stdout, &stderr)
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

func TestRunReportsTheVerdictAndTheStatus(t *testing.T) {
	t.Run("a compliant repository", func(t *testing.T) {
		var stdout, stderr strings.Builder
		status := run([]string{"-root", writeRepo(t, armedTrap)}, &stdout, &stderr)
		if status != gate.ExitClean {
			t.Fatalf("run exited %d, want %d\nstderr: %s", status, gate.ExitClean, stderr.String())
		}
		if !strings.Contains(stdout.String(), "the integration stack is torn down on the early-exit path too") {
			t.Fatalf("the pass was not reported: %q", stdout.String())
		}
	})

	t.Run("a repository that leaves the stack up", func(t *testing.T) {
		var stdout, stderr strings.Builder
		status := run([]string{"-root", writeRepo(t, teardownAtTheEnd)}, &stdout, &stderr)
		if status != gate.ExitFindings {
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
}

func TestRunLeavesTheHostsGomodBackupsAsItFoundThem(t *testing.T) {
	// The real recipe copies go.mod to fixed paths under /tmp. Whatever was
	// there before this gate ran has to still be there after, and whatever was
	// not must not be left behind.
	before := map[string]bool{}
	for _, path := range gomodBackups {
		_, err := os.Stat(path)
		before[path] = err == nil
	}

	var stdout, stderr strings.Builder
	if status := run([]string{"-root", writeRepo(t, armedTrap)}, &stdout, &stderr); status != gate.ExitClean {
		t.Fatalf("run exited %d: %s", status, stderr.String())
	}

	for _, path := range gomodBackups {
		_, err := os.Stat(path)
		if (err == nil) != before[path] {
			t.Fatalf("%s existed=%v before the run and existed=%v after", path, before[path], err == nil)
		}
	}
}

func TestAbsentBackupsListsOnlyWhatIsNotThere(t *testing.T) {
	absent := absentBackups()
	for _, path := range absent {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s was reported absent but stat says %v", path, err)
		}
	}
}

func TestCallIndexHelpersReadTheOrderOfTheCalls(t *testing.T) {
	calls := []string{
		"compose -f c.yml down --volumes --remove-orphans",
		"compose -f c.yml build --no-cache --pull",
		"compose -f c.yml up -d",
		"compose -f c.yml down --volumes --remove-orphans",
	}
	if got := lastCallIndex(calls, buildMarker); got != 1 {
		t.Fatalf("lastCallIndex(%q) = %d, want 1", buildMarker, got)
	}
	// The teardown BEFORE the failure says nothing about the exit path; only a
	// later one does.
	if got := firstCallIndexAfter(calls, teardownMarker, 1); got != 3 {
		t.Fatalf("firstCallIndexAfter after the build = %d, want 3", got)
	}
	if got := lastCallIndex(calls, "system prune"); got != -1 {
		t.Fatalf("lastCallIndex of an absent call = %d, want -1", got)
	}
	if got := firstCallIndexAfter(calls, teardownMarker, 3); got != -1 {
		t.Fatalf("firstCallIndexAfter past the end = %d, want -1", got)
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

func TestRunFailsWhenTheTargetItVerifiesDoesNotExist(t *testing.T) {
	// `make integration-test` in a tree with no such target exits non-zero
	// before reaching the build, which is the "no longer exercising it" finding
	// rather than a pass.
	var stdout, stderr strings.Builder
	status := run([]string{"-root", t.TempDir()}, &stdout, &stderr)
	if status != gate.ExitFindings {
		t.Fatalf("run exited %d, want %d", status, gate.ExitFindings)
	}
	if !strings.Contains(stderr.String(), "never reached the image build") {
		t.Fatalf("the failure does not say the gate stopped exercising the recipe:\n%s", stderr.String())
	}
	// And the diagnostic has to carry the recipe's own output, or the reader
	// cannot see why.
	if !strings.Contains(stderr.String(), "--- recipe output ---") {
		t.Fatalf("the failure carries no diagnostic:\n%s", stderr.String())
	}
}
