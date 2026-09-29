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

package sdlc

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The merge result
// =============================================================================
//
// Verification used to judge the branch as the container held it while the
// forge's checks judged the merge of that branch with its base. These tests
// drive the REAL git command builder against a scripted container, so what
// they measure is the argument vectors the daemon actually emits — a merge
// that committed something, or a worktree that outlived its verification,
// would fail them rather than pass quietly.

// mergeResultWorktree is where a checkout's throwaway worktree goes.
func mergeResultWorktree(plan publishPlan) string {
	return path.Join(path.Dir(plan.WorkDir), mergeResultDirPrefix+path.Base(plan.WorkDir))
}

// TestMergeResult_ABranchLevelWithItsBaseCostsTheRefreshAndTheCount is the
// inertness property, and the one every attempt in the fleet takes: a branch
// that is not behind is verified exactly as it was before — no worktree, no
// merge, nothing to clean up — for the price of refreshing the base and
// counting against it.
func TestMergeResult_ABranchLevelWithItsBaseCostsTheRefreshAndTheCount(t *testing.T) {
	container := &fakeContainer{checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"), ahead: true}
	pub := newTestPublisher(container, nil)

	merged, err := pub.MergeResult(testPublishPlan())
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	if merged.Behind != 0 {
		t.Fatalf("behind = %d, want a branch level with its base", merged.Behind)
	}
	if merged.WorkDir != "" {
		t.Errorf("a level branch was given a worktree at %q", merged.WorkDir)
	}
	if merged.Note() != "" {
		t.Errorf("a level branch put a note in front of the evidence: %q", merged.Note())
	}
	if len(container.worktreeAdds) != 0 || len(container.merges) != 0 {
		t.Errorf("a level branch cost a worktree (%+v) or a merge (%+v)", container.worktreeAdds, container.merges)
	}
	// AND THE COUNT WAS AGAINST A BASE THIS CONTAINER HAD JUST FETCHED.
	// "level" read off the checkout's initialization-time ref is the answer
	// that sends a stale tree to verification calling itself the merge result.
	if len(container.fetches) != 1 {
		t.Fatalf("fetches = %+v, want exactly the refresh of the base", container.fetches)
	}
	if got := container.fetches[0]; got.Remote != gitRemote || len(got.Refs) != 1 || got.Refs[0] != "main" {
		t.Errorf("the refresh fetched %+v, want just the base from %q", got, gitRemote)
	}
	if err := merged.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if len(container.worktreeRemovals) != 0 {
		t.Errorf("a level branch removed a worktree it never made: %v", container.worktreeRemovals)
	}
}

// TestMergeResult_ABranchBehindItsBaseIsVerifiedOnTheMergeResult is the whole
// point of #982. The tree the commands run on is a THROWAWAY WORKTREE of HEAD
// with the base merged in — the same tree the forge's "Merge Result" job
// judges — and the merge COMMITS NOTHING, so the branch the worker owns is
// exactly as it was.
func TestMergeResult_ABranchBehindItsBaseIsVerifiedOnTheMergeResult(t *testing.T) {
	container := &fakeContainer{checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"), ahead: true, behind: 2}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	if merged.Behind != 2 {
		t.Fatalf("behind = %d, want the count git gave", merged.Behind)
	}
	want := mergeResultWorktree(plan)
	if merged.WorkDir != want {
		t.Fatalf("work dir = %q, want the throwaway worktree %q", merged.WorkDir, want)
	}
	// BESIDE THE CHECKOUT, NOT INSIDE IT: a worktree inside is untracked
	// content the publish step's "git add --all" would commit.
	if strings.HasPrefix(merged.WorkDir, plan.WorkDir+"/") {
		t.Errorf("the worktree %q is inside the checkout %q", merged.WorkDir, plan.WorkDir)
	}
	if len(container.worktreeAdds) != 1 {
		t.Fatalf("worktree adds = %+v, want exactly one", container.worktreeAdds)
	}
	if container.worktreeAdds[0].Ref != "HEAD" {
		t.Errorf("the worktree was made from %q, want HEAD", container.worktreeAdds[0].Ref)
	}
	if len(container.merges) != 1 {
		t.Fatalf("merges = %+v, want exactly one", container.merges)
	}
	merge := container.merges[0]
	if merge.Dir != want {
		t.Errorf("the merge ran in %q, want the throwaway worktree", merge.Dir)
	}
	// THE COMMIT, NOT THE REF. What was counted is what is merged, so a base
	// that moves again between the two cannot have verification judge a tree
	// it never counted.
	if merge.Ref != defaultBaseHead {
		t.Errorf("the merge named %q, want the commit the base's fetch landed on", merge.Ref)
	}
	flags := strings.Join(merge.Args, " ")
	if !strings.Contains(flags, "--no-commit") {
		t.Errorf("the merge committed: %q", flags)
	}
	if len(container.commits) != 0 || len(container.pushes) != 0 {
		t.Errorf("preparing a merge result touched the branch: commits=%v pushes=%+v", container.commits, container.pushes)
	}
}

// TestMergeResult_TheWorktreeNeverOutlivesTheVerification: Release removes it.
// A throwaway worktree left behind is a stale merge of a base that has since
// moved, and the next round would find the directory already there.
func TestMergeResult_TheWorktreeNeverOutlivesTheVerification(t *testing.T) {
	container := &fakeContainer{checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"), ahead: true, behind: 1}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	if err := merged.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := merged.Release(); err != nil {
		t.Fatalf("a second Release: %v", err)
	}

	want := mergeResultWorktree(plan)
	removed := 0
	for _, dir := range container.worktreeRemovals {
		if dir == want {
			removed++
		}
	}
	// EXACTLY ONE REMOVAL. The pre-clean asks git what worktrees it holds
	// and removes nothing when there is none, so the only removal is
	// Release's — and a second Release adds nothing, which is what makes a
	// deferred Release safe next to an explicit one.
	if removed != 1 {
		t.Fatalf("removals of %q = %d, want the release exactly once: %v",
			want, removed, container.worktreeRemovals)
	}
	if len(container.worktrees) != 0 {
		t.Errorf("a worktree outlived the verification: %v", container.worktrees)
	}
}

// TestMergeResult_AMergeThatDoesNotApplyIsATypedRefusal: a branch that will
// not merge is not a verification failure. It says nothing about the work, so
// reporting it as a failed build would send the worker looking for a defect
// that is not there — and the conflicting paths are named, because "merge
// conflict" alone is a sentence a person has to reproduce the merge to act on.
func TestMergeResult_AMergeThatDoesNotApplyIsATypedRefusal(t *testing.T) {
	container := &fakeContainer{
		checkout:       path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		ahead:          true,
		behind:         3,
		mergeConflicts: []string{"pkg/thing/thing.go", "go.mod"},
	}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if merged != nil {
		t.Fatalf("a refused merge answered with a tree to judge: %+v", merged)
	}
	var conflict *MergeResultConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a typed merge conflict", err)
	}
	if !errors.Is(err, ErrRunner) {
		t.Errorf("the refusal does not unwrap to the runner sentinel: %v", err)
	}
	if conflict.Branch != plan.Branch || conflict.Base != gitRemote+"/"+plan.Base {
		t.Errorf("the refusal names %q and %q, want the branch and its base", conflict.Branch, conflict.Base)
	}
	for _, want := range []string{"pkg/thing/thing.go", "go.mod"} {
		if !strings.Contains(conflict.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%s", want, conflict.Error())
		}
	}
	// AND IT LEAVES NOTHING BEHIND. The worktree was made before the merge
	// was attempted, so a refusal that kept it would leak one per round.
	if len(container.worktrees) != 0 {
		t.Errorf("a refused merge left its worktree behind: %v", container.worktrees)
	}
}

// TestMergeResult_AMergeThatFailedWithoutConflictingIsACommandFailure: a
// non-zero `git merge` is TWO different events wearing one exit status, and
// only one of them is the worker's to resolve. A merge that never reached the
// trees — the worktree that is not there, the configuration git refuses, the
// disk that filled — says nothing about the branch, so reporting it as a
// conflict would send the worker looking for conflicting paths in a branch
// that merges cleanly, which is a search that cannot end.
func TestMergeResult_AMergeThatFailedWithoutConflictingIsACommandFailure(t *testing.T) {
	container := &fakeContainer{
		checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		behind:   2,
		mergeErr: errors.New("fatal: could not read Username for 'https://forge': No such device or address"),
	}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if merged != nil {
		t.Fatalf("a failed merge answered with a tree to judge: %+v", merged)
	}
	var conflict *MergeResultConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("a merge git left no unmerged path for was reported as a conflict: %v", err)
	}
	var failed *PublishCommandError
	if !errors.As(err, &failed) {
		t.Fatalf("err = %v, want a typed command failure", err)
	}
	if !errors.Is(err, ErrRunner) {
		t.Errorf("the failure does not unwrap to the runner sentinel: %v", err)
	}
	// IT NAMES THE COMMAND THAT FAILED, which is the whole value of reporting
	// it as a command: a person reads what git was asked and what it said.
	if !strings.Contains(failed.Command, "merge") {
		t.Errorf("the failure names %q, want the merge", failed.Command)
	}
	if !strings.Contains(failed.Error(), "No such device or address") {
		t.Errorf("the failure does not carry what git said:\n%s", failed.Error())
	}
	// AND IT STILL LEAVES NOTHING BEHIND. The worktree was made before the
	// merge was attempted either way.
	if len(container.worktrees) != 0 {
		t.Errorf("a failed merge left its worktree behind: %v", container.worktrees)
	}
}

// TestMergeResult_AConflictProbeThatFailsIsNotLost: the unmerged-paths probe
// is what decides which of those two events a failed merge was, so a probe
// that could not run leaves the question unanswered — and an unanswered
// question is not a "no". The run reports the merge as the command failure it
// can prove, and carries the probe's own failure with it, because that
// failure is the reason the conflict could not be named.
func TestMergeResult_AConflictProbeThatFailsIsNotLost(t *testing.T) {
	container := &fakeContainer{
		checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		behind:   2,
		// The merge DID conflict — and the container cannot say so.
		mergeConflicts:   []string{"pkg/thing/thing.go"},
		conflictProbeErr: errors.New("fatal: this operation must be run in a work tree"),
	}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if merged != nil {
		t.Fatalf("a failed merge answered with a tree to judge: %+v", merged)
	}
	var conflict *MergeResultConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("a conflict nobody could confirm was reported as one: %v", err)
	}
	var failed *PublishCommandError
	if !errors.As(err, &failed) {
		t.Fatalf("err = %v, want a typed command failure", err)
	}
	// BOTH FAILURES REACH THE READER. The merge's exit explains why there is
	// no tree to judge; the probe's explains why the merge is not named a
	// conflict, and dropping it would leave a broken container invisible.
	for _, want := range []string{"merge", "diff --name-only --diff-filter=U", "must be run in a work tree"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the failure does not name %q:\n%s", want, err.Error())
		}
	}
	if len(container.worktrees) != 0 {
		t.Errorf("a failed merge left its worktree behind: %v", container.worktrees)
	}
}

// TestMergeResult_UncommittedWorkIsCarriedIntoTheMergeResult is the
// persistent tier's half of #982. PushTurn commits nothing off the ephemeral
// tier, so the worker's turn is STILL UNCOMMITTED in the checkout when
// verification runs — which made "dirty, therefore verify the branch as it
// stands" the ordinary path there and left the wasted round exactly where it
// was. The work is snapshotted into an object instead, and the merge result
// is that snapshot merged with the base.
func TestMergeResult_UncommittedWorkIsCarriedIntoTheMergeResult(t *testing.T) {
	container := &fakeContainer{
		checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		behind:   1,
		changed:  true,
	}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	if merged.WorkDir != mergeResultWorktree(plan) {
		t.Fatalf("a dirty checkout was verified on %q, want the merge result", merged.WorkDir)
	}
	if !merged.Uncommitted {
		t.Error("the run does not record that the merge result carries uncommitted work")
	}
	// THE WORKTREE IS MADE FROM THE SNAPSHOT, NOT FROM HEAD. A worktree of
	// HEAD holds the tree as it was before the turn, so this is the single
	// assertion that says the worker's actual work was what got judged.
	if len(container.snapshots) != 1 {
		t.Fatalf("snapshots = %+v, want exactly one snapshot of the working tree", container.snapshots)
	}
	snapshot := container.snapshots[0]
	if len(container.worktreeAdds) != 1 || container.worktreeAdds[0].Ref != snapshot.Commit {
		t.Fatalf("the merge result was made from %+v, want the snapshot %q", container.worktreeAdds, snapshot.Commit)
	}
	if snapshot.Parent != "HEAD" {
		t.Errorf("the snapshot is parented on %q, want HEAD", snapshot.Parent)
	}
	// AND IT WAS TAKEN IN THE CHECKOUT, which is the only tree that holds the
	// work: a snapshot taken anywhere else would carry someone else's.
	if snapshot.WorkDir != plan.WorkDir {
		t.Errorf("the snapshot was taken in %q, want the worker's checkout", snapshot.WorkDir)
	}
	// AND IT WAS STAGED SOMEWHERE THAT IS NOT THE WORKER'S INDEX, beside the
	// checkout rather than inside it: an index within the checkout is
	// untracked content the publish step's `git add --all` would commit into
	// the delivery, carrying a second copy of the very work it staged.
	if snapshot.Index != mergeResultIndex(plan.WorkDir) {
		t.Errorf("the snapshot staged into %q, want the throwaway index beside the checkout", snapshot.Index)
	}
	if strings.HasPrefix(snapshot.Index, plan.WorkDir+"/") {
		t.Errorf("the throwaway index %q is inside the checkout the delivery is committed from", snapshot.Index)
	}
	// AND IT IS NOT SIGNED. The object is scaffolding no one will ever pull
	// or verify, and a signature is a provenance claim about work someone
	// stands behind.
	if !containsArg(snapshot.Config, "commit.gpgsign=false") {
		t.Errorf("the snapshot ran under %v, want signing stated off", snapshot.Config)
	}
	// AND NOTHING THE WORKER OWNS MOVED. No commit, and the checkout is as
	// dirty as the worker left it: the snapshot is an object, not a commit on
	// the branch.
	if len(container.commits) != 0 {
		t.Errorf("the snapshot committed to the worker's branch: %v", container.commits)
	}
	if !container.changed {
		t.Error("the snapshot staged the worker's work out of its own checkout")
	}
	if note := merged.Note(); !strings.Contains(note, "1 commit behind") ||
		!strings.Contains(note, "has not committed") {
		t.Errorf("the note does not say what was judged:\n%s", note)
	}
	// AND THE THROWAWAY INDEX GOES AWAY WITH THE WORKTREE. It sits beside a
	// checkout that OUTLIVES the job on a persistent workspace tier.
	if err := merged.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !removedPath(container.removals, mergeResultIndex(plan.WorkDir)) {
		t.Errorf("the throwaway index was left beside the checkout: %+v", container.removals)
	}
	if len(container.worktrees) != 0 {
		t.Errorf("the throwaway worktree outlived its verification: %v", container.worktrees)
	}
}

// TestMergeResult_ASnapshotThatCannotBeWrittenIsRefused: a merge result that
// could not be prepared must not fall back to the stale tree, because that
// failure is reported to the worker as its own defect — the exact round #982
// exists to end. It is a typed refusal, and it leaves nothing behind.
func TestMergeResult_ASnapshotThatCannotBeWrittenIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name      string
		container *fakeContainer
	}{
		{"the tree cannot be written", &fakeContainer{writeTreeErr: errors.New("fatal: unable to write new index file")}},
		{"the commit object cannot be written", &fakeContainer{commitTreeErr: errors.New("fatal: unable to write commit object")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.container.checkout = path.Join(orchestration.WorkspaceCheckoutRoot, "api")
			tc.container.behind = 1
			tc.container.changed = true
			pub := newTestPublisher(tc.container, nil)
			plan := testPublishPlan()

			merged, err := pub.MergeResult(plan)
			if merged != nil {
				t.Fatalf("a snapshot that was never written was judged: %+v", merged)
			}
			var refused *PublishCommandError
			if !errors.As(err, &refused) {
				t.Fatalf("err = %v, want a typed command refusal", err)
			}
			if len(tc.container.worktrees) != 0 {
				t.Errorf("a refused snapshot left a worktree behind: %v", tc.container.worktrees)
			}
			if !removedPath(tc.container.removals, mergeResultIndex(plan.WorkDir)) {
				t.Errorf("a refused snapshot left its index behind: %+v", tc.container.removals)
			}
		})
	}
}

// TestMergeResult_TheSnapshotIndexIsSeededFromTheWorkersOwn: the throwaway
// index is a COPY of the checkout's, taken BEFORE anything is staged into it.
// An index that starts empty makes `git add --all` skip every file the
// repository tracks despite an ignore rule — seven of them here — so the
// snapshot would record them as deleted and verification would judge a tree
// the delivery would never produce (#982, round 3). What the resulting tree
// actually holds is measured against real git in
// merge_result_snapshot_integration_test.go, which the unit tier does not
// build (`make integration-test-merge-result`); this is the ordering and the
// plumbing, which is what a fake can hold.
func TestMergeResult_TheSnapshotIndexIsSeededFromTheWorkersOwn(t *testing.T) {
	container := &fakeContainer{
		checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		behind:   1,
		changed:  true,
	}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	if _, err := pub.MergeResult(plan); err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	if len(container.copies) != 1 {
		t.Fatalf("copies = %+v, want the worker's index copied exactly once", container.copies)
	}
	seed := container.copies[0]
	// FROM WHERE GIT SAYS THE INDEX IS, not from an assumed ".git/index":
	// a checkout that is itself a linked worktree keeps it elsewhere.
	if seed.From != path.Join(".git", "index") {
		t.Errorf("the throwaway index was seeded from %q, want the index git named", seed.From)
	}
	if seed.To != mergeResultIndex(plan.WorkDir) {
		t.Errorf("the seed landed in %q, want the throwaway index %q", seed.To, mergeResultIndex(plan.WorkDir))
	}
	if seed.Dir != plan.WorkDir {
		t.Errorf("the seed was taken in %q, want the worker's checkout", seed.Dir)
	}
	// AND BEFORE THE STAGING, which is the whole point: seeding an index
	// after `add --all` has already skipped the ignored-but-tracked files
	// puts them back as they were COMMITTED, not as the worker left them.
	if len(container.snapshots) != 1 {
		t.Fatalf("snapshots = %+v, want exactly one", container.snapshots)
	}
	if container.snapshots[0].Seed != seed.From {
		t.Errorf("the snapshot staged into an index seeded from %q, want the worker's own before staging",
			container.snapshots[0].Seed)
	}
}

// TestMergeResult_AnIndexThatCannotBeSeededIsRefused: a snapshot staged into
// an index that was never seeded is a tree missing files nobody touched, so
// the failure to seed is a typed refusal rather than something to carry on
// from — and it leaves nothing beside the checkout.
func TestMergeResult_AnIndexThatCannotBeSeededIsRefused(t *testing.T) {
	container := &fakeContainer{
		checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		behind:   1,
		changed:  true,
		copyErr:  errors.New("cp: cannot open '.git/index' for reading"),
	}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if merged != nil {
		t.Fatalf("a snapshot with an unseeded index was judged: %+v", merged)
	}
	var refused *PublishCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a typed command refusal", err)
	}
	if len(container.snapshots) != 0 {
		t.Errorf("a tree was staged into an index that was never seeded: %+v", container.snapshots)
	}
	if len(container.worktrees) != 0 {
		t.Errorf("a refused seed left a worktree behind: %v", container.worktrees)
	}
	if !removedPath(container.removals, mergeResultIndex(plan.WorkDir)) {
		t.Errorf("a refused seed left its index behind: %+v", container.removals)
	}
}

// removedPath reports whether a path was removed through the cleanup seam.
func removedPath(removals []string, want string) bool {
	for _, removal := range removals {
		if removal == want {
			return true
		}
	}
	return false
}

// TestMergeResult_ABaseTheContainerCannotResolveIsRefused: answering "level"
// for a count git could not give would go on to judge the STALE tree and call
// it the merge result, which is the exact confusion #982 closed.
func TestMergeResult_ABaseTheContainerCannotResolveIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *orchestration.ExecResult
	}{
		{"an unresolvable base", &orchestration.ExecResult{ExitCode: 128, Stderr: "fatal: ambiguous argument 'origin/main'\n"}},
		{"a count that is not a number", &orchestration.ExecResult{Stdout: "no idea\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counted := false
			pub := &publisher{
				exec: func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
					if len(spec.Cmd) > 1 && spec.Cmd[1] == "rev-list" {
						counted = true
						return tc.result, nil
					}
					// THE FETCH HAS TO LAND ON A COMMIT for the count to be
					// reached at all. A container that answered nothing here
					// would refuse in FetchBase, and this test would pass
					// without the count ever running.
					if len(spec.Cmd) > 2 && spec.Cmd[1] == "rev-parse" && spec.Cmd[2] == "FETCH_HEAD" {
						return &orchestration.ExecResult{Stdout: defaultBaseHead + "\n"}, nil
					}
					return &orchestration.ExecResult{}, nil
				},
				project: testOrchProject(),
				taskID:  testTask,
			}
			merged, err := pub.MergeResult(testPublishPlan())
			if merged != nil {
				t.Fatalf("a count git could not give was judged as level: %+v", merged)
			}
			if !counted {
				t.Fatal("the count never ran, so this case refused something else")
			}
			var refused *PublishCommandError
			if !errors.As(err, &refused) {
				t.Fatalf("err = %v, want a typed command refusal", err)
			}
			// AND THE REFUSAL NAMES THE COMMAND THAT COULD NOT ANSWER, which
			// is the whole of what an operator reads it for.
			if !strings.Contains(refused.Command, "rev-list --count") {
				t.Errorf("the refusal names %q, want the count that failed", refused.Command)
			}
		})
	}
}

// TestMergeResultNote_LeadsWithHowFarBehindTheBranchIs is the evidence
// property: the worker reads the base having moved BEFORE it reads a single
// test failure, because a worker that reads the failure first fixes the test.
func TestMergeResultNote_LeadsWithHowFarBehindTheBranchIs(t *testing.T) {
	merged := &mergeResult{Behind: 1, Base: "main", WorkDir: "/workspace/.merge-result-api"}
	note := merged.Note()
	if !strings.HasPrefix(note, "Your branch is 1 commit behind `main`") {
		t.Fatalf("the note does not lead with the count:\n%s", note)
	}
	if !strings.Contains(note, "MERGE RESULT") {
		t.Errorf("the note does not say which tree was judged:\n%s", note)
	}

	many := &mergeResult{Behind: 4, Base: "main", WorkDir: "/workspace/.merge-result-api"}
	if !strings.Contains(many.Note(), "4 commits behind") {
		t.Errorf("the note does not pluralise:\n%s", many.Note())
	}
	if (&mergeResult{}).Note() != "" {
		t.Error("a level branch produced a note")
	}
}

// TestLeadWith_KeepsTheLeadInFrontWithoutAnEmptyParagraph: the lead goes
// ahead of the evidence, and neither half being absent produces stray blank
// lines in what the worker reads.
func TestLeadWith_KeepsTheLeadInFrontWithoutAnEmptyParagraph(t *testing.T) {
	if got := leadWith("behind", "failures"); got != "behind\n\nfailures" {
		t.Errorf("leadWith = %q", got)
	}
	if got := leadWith("", "failures"); got != "failures" {
		t.Errorf("leadWith with no lead = %q", got)
	}
	if got := leadWith("behind", "  \n"); got != "behind" {
		t.Errorf("leadWith with no body = %q", got)
	}
}

// TestMergeResult_TheBaseIsRefreshedBeforeItIsCounted is the window a count
// against the checkout's own ref cannot see. The container fetched the base
// when the attempt was initialized — before the worker was prompted — and
// main moved during the turn, which is how #965 and #966 lost their rounds in
// the first place. Counting against the stale ref answers "level" and sends
// the stale tree to verification under the name of the merge result.
func TestMergeResult_TheBaseIsRefreshedBeforeItIsCounted(t *testing.T) {
	moved := 2
	container := &fakeContainer{
		checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		ahead:    true,
		// What this container last saw: a branch level with its base.
		behind: 0,
		// What the refresh finds: the base two commits further on.
		behindAfterFetch: &moved,
	}
	pub := newTestPublisher(container, nil)
	plan := testPublishPlan()

	merged, err := pub.MergeResult(plan)
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	if merged.Behind != 2 {
		t.Fatalf("behind = %d, want the count against the base AS REFRESHED", merged.Behind)
	}
	if merged.WorkDir != mergeResultWorktree(plan) {
		t.Fatalf("work dir = %q, want the merge result a refreshed count asked for", merged.WorkDir)
	}
	if len(container.merges) != 1 {
		t.Fatalf("merges = %+v, want exactly one", container.merges)
	}
	// THE COMMIT THE REFRESH LANDED ON IS WHAT WAS MERGED. Naming the ref
	// instead would let a base that moves again between the count and the
	// merge have verification judge a tree it never counted.
	if container.merges[0].Ref != "base-head-moved" {
		t.Errorf("the merge named %q, want the commit the refresh landed on", container.merges[0].Ref)
	}
	if err := merged.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

// TestMergeResult_ABaseThatCannotBeRefreshedIsRefused: a fetch that failed
// leaves the container holding whatever it last saw, and counting against
// that is the stale answer this whole file exists to stop. It is a typed
// refusal, never a silent fall back to the ref that is already there.
func TestMergeResult_ABaseThatCannotBeRefreshedIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *orchestration.ExecResult
		cmd    string
	}{
		{
			name:   "a fetch that failed",
			cmd:    "fetch",
			result: &orchestration.ExecResult{ExitCode: 128, Stderr: "fatal: could not read from remote repository\n"},
		},
		{
			name:   "a fetch that landed on no commit",
			cmd:    "rev-parse",
			result: &orchestration.ExecResult{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counted := false
			pub := &publisher{
				exec: func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
					switch {
					case containsArg(spec.Cmd, tc.cmd):
						return tc.result, nil
					case containsArg(spec.Cmd, "rev-list"):
						counted = true
					}
					return &orchestration.ExecResult{Stdout: "0\n"}, nil
				},
				project: testOrchProject(),
				taskID:  testTask,
			}
			merged, err := pub.MergeResult(testPublishPlan())
			if merged != nil {
				t.Fatalf("a base that was never refreshed was judged: %+v", merged)
			}
			var refused *PublishCommandError
			if !errors.As(err, &refused) {
				t.Fatalf("err = %v, want a typed command refusal", err)
			}
			if counted {
				t.Error("the branch was counted against a base the container could not refresh")
			}
		})
	}
}

// TestMergeResult_TheWorktreeIsRemovedEvenWhenTheAttemptWasCancelled is why
// the removal has a seam of its own. The stage's context is already done when
// a verification times out or the daemon is shutting down — which is exactly
// when the removal matters, because the worktree sits in a checkout that
// OUTLIVES the job on a persistent workspace tier and the next attempt on
// that node finds it in the way.
func TestMergeResult_TheWorktreeIsRemovedEvenWhenTheAttemptWasCancelled(t *testing.T) {
	container := &fakeContainer{checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"), ahead: true, behind: 1}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pub := newTestPublisher(container, nil)
	// exec carries the stage's context, the way the runner binds it; cleanup
	// is the detached seam, which by construction outlives it.
	pub.exec = func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return container.exec("container-1", spec)
	}
	pub.cleanup = func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		return container.exec("container-1", spec)
	}

	merged, err := pub.MergeResult(testPublishPlan())
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	cancel() // the verification timed out, or the daemon is going down

	if err := merged.Release(); err != nil {
		t.Fatalf("Release after cancellation: %v", err)
	}
	if len(container.worktrees) != 0 {
		t.Errorf("a cancelled attempt left its worktree behind: %v", container.worktrees)
	}
}

// TestMergeResult_ACancelledAttemptWithNoCleanupSeamSaysSo is the other half:
// a publisher given no detached seam falls back to the stage's own, and what
// it must NOT do is report a removal that never happened. The caller is told,
// because a worktree left behind with nobody told is how the next attempt
// inherits it.
func TestMergeResult_ACancelledAttemptWithNoCleanupSeamSaysSo(t *testing.T) {
	container := &fakeContainer{checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"), ahead: true, behind: 1}
	ctx, cancel := context.WithCancel(context.Background())
	pub := &publisher{
		exec: func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return container.exec("container-1", spec)
		},
		project: testOrchProject(),
		taskID:  testTask,
	}

	merged, err := pub.MergeResult(testPublishPlan())
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	cancel()

	if err := merged.Release(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Release = %v, want the cancellation that stopped the removal", err)
	}
}

// TestMergeResult_AWorktreeThatWillNotGoAwayIsReported: "there is no such
// worktree" and "the removal failed" are both a non-zero `worktree remove`,
// and telling them apart is the whole reason the run asks git what worktrees
// it holds first. A failure reported as "already gone" is how one survives
// into the next attempt with nobody told.
func TestMergeResult_AWorktreeThatWillNotGoAwayIsReported(t *testing.T) {
	container := &fakeContainer{
		checkout:          path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		ahead:             true,
		behind:            1,
		worktreeRemoveErr: errors.New("fatal: failed to delete '.merge-result-api': Device or resource busy"),
	}
	pub := newTestPublisher(container, nil)

	merged, err := pub.MergeResult(testPublishPlan())
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	releaseErr := merged.Release()
	var refused *PublishCommandError
	if !errors.As(releaseErr, &refused) {
		t.Fatalf("Release = %v, want a typed refusal naming the removal", releaseErr)
	}
	if !strings.Contains(refused.Command, "worktree remove") {
		t.Errorf("the refusal names %q, want the removal that failed", refused.Command)
	}
}

// TestMergeResult_AnIndexThatWillNotGoAwayIsReported is the worktree
// property for the other thing the preparation leaves beside the checkout. A
// removal that failed and was swallowed leaves a stale index next to a
// checkout that OUTLIVES the job on a persistent workspace tier, and an
// operator who cannot read it here has nowhere else to.
func TestMergeResult_AnIndexThatWillNotGoAwayIsReported(t *testing.T) {
	plan := testPublishPlan()
	container := &fakeContainer{
		checkout: path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		behind:   1,
		changed:  true,
	}
	pub := newTestPublisher(container, nil)

	merged, err := pub.MergeResult(plan)
	if err != nil {
		t.Fatalf("MergeResult: %v", err)
	}
	// The removal starts failing only AFTER the preparation, which is the
	// case that matters: the merge result was verified and the index it was
	// staged in is what will not go away.
	container.removeErr = errors.New("rm: cannot remove: Read-only file system")

	releaseErr := merged.Release()
	var refused *PublishCommandError
	if !errors.As(releaseErr, &refused) {
		t.Fatalf("Release = %v, want a typed refusal naming the removal", releaseErr)
	}
	if !strings.Contains(refused.Command, mergeResultIndex(plan.WorkDir)) {
		t.Errorf("the refusal names %q, want the index that would not go away", refused.Command)
	}
	// AND THE WORKTREE WAS STILL REMOVED. Both undos are attempted; the one
	// that could be done is not abandoned because the other could not.
	if len(container.worktrees) != 0 {
		t.Errorf("a failed index removal stopped the worktree from going away: %v", container.worktrees)
	}
}

// TestMergeResult_APreCleanThatFailsStopsThePreparation is the same
// distinction one step earlier: a worktree an earlier round left behind that
// will not go away is not a tree to verify on, and `worktree add` is going to
// fail on it next anyway.
func TestMergeResult_APreCleanThatFailsStopsThePreparation(t *testing.T) {
	plan := testPublishPlan()
	container := &fakeContainer{
		checkout:          path.Join(orchestration.WorkspaceCheckoutRoot, "api"),
		ahead:             true,
		behind:            1,
		worktrees:         map[string]string{mergeResultWorktree(plan): "HEAD"},
		worktreeRemoveErr: errors.New("fatal: failed to delete: Device or resource busy"),
	}
	pub := newTestPublisher(container, nil)

	merged, err := pub.MergeResult(plan)
	if merged != nil {
		t.Fatalf("a stale worktree was adopted as the merge result: %+v", merged)
	}
	var refused *PublishCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a typed refusal", err)
	}
	if len(container.worktreeAdds) != 0 {
		t.Errorf("the preparation went on after the pre-clean failed: %+v", container.worktreeAdds)
	}
}
