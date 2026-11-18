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

//go:build integration

package sdlc

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The snapshot's CONTENTS
// =============================================================================
//
// Every other test of the merge result measures the argument vectors the
// publisher emits, which is the right instrument for "was the worktree thrown
// away" and the wrong one for "what is in the tree it judged": `git add --all`
// staged into an EMPTY index drops every file the repository tracks despite an
// ignore rule, and a test that only records the command sees an identical
// vector either way (#982, round 3).
//
// So these tests run REAL GIT against a repository built on a real
// filesystem, which is why they carry the `integration` tag and NOT why they
// are slow: they take tens of milliseconds. The unit tier's rule is not about
// cost, it is about what a unit test is allowed to touch — in memory, no
// process spawned, no path written (`../handbook/software/conventions/
// testing-guidelines.md`) — and a suite that runs git and cp against a
// working tree is outside it however fast it is. The command PLUMBING stays
// in the unit tier, where the fakes record every argument vector
// (merge_result_test.go); this suite owns the one question a recorded vector
// cannot answer, which is what the resulting TREE holds.
//
// Run it with `make integration-test-merge-result`, which builds
// test/integration/Dockerfile.merge-result and runs it there, never on the
// host: the rule is about what a suite TOUCHES, and spawning git against a
// working tree is outside the host however fast it is. Everything it touches
// is under t.TempDir(), and it needs no server, no credential and no network.
//
// CI runs that same target in the `Merge-Result Snapshot` job of ci.yml, on
// every push and every pull request. It has to: `make test` never compiles
// this file, and `make vet-integration` only typechecks it — a suite nothing
// executes measures nothing (integration-testing.md §8).

// snapshotRepo is a checkout that holds the shapes a real working tree holds
// when a turn ends, and answers the plan the publisher runs against it.
type snapshotRepo struct {
	t   *testing.T
	dir string
}

// newSnapshotRepo builds a repository whose committed state includes a file
// that is TRACKED DESPITE AN IGNORE RULE — the shape this repository carries
// seven of, and the one an empty index loses.
func newSnapshotRepo(t *testing.T) *snapshotRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is required to measure what a snapshot contains: %v", err)
	}
	repo := &snapshotRepo{t: t, dir: t.TempDir()}
	repo.git("init", "-q", "-b", "main")
	repo.write(".gitignore", "data/vector/\nstaged-by-hand.txt\n")
	repo.write("README.md", "hello\n")
	repo.write("data/vector/marker.format-version", "13\n")
	// -f is how a tracked-but-ignored file comes to exist, in this fixture
	// and in the repository it models.
	repo.git("add", "-f", ".gitignore", "README.md", "data/vector/marker.format-version")
	repo.git("commit", "-qm", "init")
	return repo
}

// git runs one git command in the repository and fails the test on error.
func (r *snapshotRepo) git(args ...string) string {
	r.t.Helper()
	out, err := r.run(args...)
	if err != nil {
		r.t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
	return out
}

// run is git without the assertion, for the two callers that expect to be
// able to read a failure.
func (r *snapshotRepo) run(args ...string) (string, error) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = snapshotGitEnv(nil)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// write puts a file in the working tree, making its directory as needed.
func (r *snapshotRepo) write(name, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		r.t.Fatalf("MkdirAll %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		r.t.Fatalf("WriteFile %s: %v", name, err)
	}
}

// remove deletes a file from the working tree without staging the deletion,
// which is how a worker deletes a file mid-turn.
func (r *snapshotRepo) remove(name string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, name)); err != nil {
		r.t.Fatalf("Remove %s: %v", name, err)
	}
}

// plan is the publish plan for this checkout. The throwaway index lands
// BESIDE it, which t.TempDir() owns as well.
func (r *snapshotRepo) plan() publishPlan {
	return publishPlan{WorkDir: r.dir, Branch: "issue/982-merge-result", Base: "main"}
}

// snapshotGitEnv isolates git from whatever the host configured — signing,
// hooks, templates — so the fixture answers the same on every machine, and
// adds whatever the publisher asked for.
func snapshotGitEnv(extra map[string]string) []string {
	env := append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=fixture",
		"GIT_AUTHOR_EMAIL=fixture@example.test",
		"GIT_COMMITTER_NAME=fixture",
		"GIT_COMMITTER_EMAIL=fixture@example.test",
		"GIT_AUTHOR_DATE=2026-09-22T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-09-22T00:00:00Z",
	)
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

// newSnapshotPublisher is a publisher whose seam runs the command for real,
// in the directory and with the environment the publisher named.
func newSnapshotPublisher(t *testing.T) *publisher {
	t.Helper()
	return &publisher{
		exec: func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
			if len(spec.Cmd) == 0 {
				return nil, &InvalidInputError{Field: "cmd", Message: "nothing to run"}
			}
			cmd := exec.Command(spec.Cmd[0], spec.Cmd[1:]...)
			cmd.Dir = spec.WorkDir
			cmd.Env = snapshotGitEnv(spec.Env)
			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			result := &orchestration.ExecResult{}
			if err := cmd.Run(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					return nil, err
				}
				result.ExitCode = exitErr.ExitCode()
			}
			result.Stdout = stdout.String()
			result.Stderr = stderr.String()
			return result, nil
		},
		project: testOrchProject(),
		taskID:  testTask,
	}
}

// TestSnapshotWorkTree_CarriesEveryFileTheDeliveryWouldCommit is the
// regression for #982 round 3. The snapshot is the tree verification judges,
// so it has to hold what the DELIVERY would commit — including the files this
// repository tracks despite an ignore rule, which `git add --all` skips
// unless the index already holds them. A snapshot missing them records them
// as DELETED, and verification then judges a tree with files the branch never
// touched removed: a failure the worker cannot explain, or a modify/delete
// conflict against a base that changed one of them.
func TestSnapshotWorkTree_CarriesEveryFileTheDeliveryWouldCommit(t *testing.T) {
	repo := newSnapshotRepo(t)
	// The worker's turn: it edited a tracked-but-ignored file, wrote a new
	// source file, deleted one, and staged an ignored file BY HAND, which is
	// the one thing re-reading HEAD instead of the index would lose.
	repo.write("data/vector/marker.format-version", "14\n")
	repo.write("pkg/new_feature.go", "package pkg\n")
	repo.write("staged-by-hand.txt", "deliberate\n")
	repo.git("add", "-f", "staged-by-hand.txt")
	repo.remove("README.md")
	statusBefore := repo.git("status", "--porcelain")
	headBefore := repo.git("rev-parse", "HEAD")

	pub := newSnapshotPublisher(t)
	snapshot, err := pub.SnapshotWorkTree(repo.plan())
	if err != nil {
		t.Fatalf("SnapshotWorkTree: %v", err)
	}

	files := strings.Split(repo.git("ls-tree", "-r", "--name-only", snapshot), "\n")
	for _, want := range []string{
		".gitignore",
		"data/vector/marker.format-version", // tracked despite the ignore rule
		"pkg/new_feature.go",                // written this turn
		"staged-by-hand.txt",                // staged by hand despite the rule
	} {
		if !containsArg(files, want) {
			t.Errorf("the snapshot is missing %q, which the delivery would commit: %v", want, files)
		}
	}
	if containsArg(files, "README.md") {
		t.Errorf("the snapshot kept a file the worker deleted: %v", files)
	}
	// AND IT CARRIES THE WORKING-TREE CONTENT of the tracked-ignored file,
	// not the committed one: "present but stale" is the same wasted round as
	// "absent", one layer quieter.
	if got := repo.git("show", snapshot+":data/vector/marker.format-version"); got != "14" {
		t.Errorf("the snapshot holds %q for the tracked-ignored file, want the working tree's %q", got, "14")
	}
	// AND NOTHING THE WORKER OWNS MOVED: not its HEAD, not its index, not
	// its working tree.
	if got := repo.git("rev-parse", "HEAD"); got != headBefore {
		t.Errorf("the snapshot moved HEAD to %q, want %q", got, headBefore)
	}
	if got := repo.git("status", "--porcelain"); got != statusBefore {
		t.Errorf("the snapshot changed the worker's index:\n%s\nwant:\n%s", got, statusBefore)
	}
	// AND IT IS EXACTLY THE DELIVERY'S TREE. Staging the same working tree
	// the way Commit does must name the same tree object, which is the whole
	// property: verification judges what the gate will.
	repo.git("add", "--all")
	delivered := repo.git("write-tree")
	if got := repo.git("rev-parse", snapshot+"^{tree}"); got != delivered {
		t.Errorf("the snapshot tree %q is not the tree the delivery would commit %q", got, delivered)
	}
}

// TestSnapshotWorkTree_ACheckoutGitCannotReadIsRefused: a snapshot that could
// not be taken must be a typed refusal, because the alternative is verifying
// the stale tree and reporting the difference to the worker as its own defect.
func TestSnapshotWorkTree_ACheckoutGitCannotReadIsRefused(t *testing.T) {
	repo := newSnapshotRepo(t)
	notACheckout := t.TempDir()
	plan := repo.plan()
	plan.WorkDir = notACheckout

	pub := newSnapshotPublisher(t)
	snapshot, err := pub.SnapshotWorkTree(plan)
	if snapshot != "" {
		t.Fatalf("a directory that is not a checkout was snapshotted as %q", snapshot)
	}
	var refused *PublishCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a typed command refusal", err)
	}
	if _, statErr := os.Stat(mergeResultIndex(notACheckout)); statErr == nil {
		t.Error("a refused snapshot left a throwaway index beside the directory")
	}
}
