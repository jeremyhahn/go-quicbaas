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
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The merge result — verify what the GATE will judge
// =============================================================================
//
// VERIFICATION AND THE GATE USED TO JUDGE DIFFERENT TREES. Verification ran
// the binding's commands on the branch exactly as the container held it; the
// forge's "Merge Result" job runs its checks on the pull request MERGED WITH
// ITS BASE. A branch that is behind its base by a commit fixing an
// environment-sensitive test therefore failed here and passed there, and the
// round of feedback the worker got was about a defect main had already fixed.
//
// Measured on go-quicbaas #965 (2026-09-22, attempt 3): a branch head one
// commit behind `origin/main` failed two tests that 8218676c had already
// fixed, and the attempt spent a whole round — six minutes and one of its
// turns — merging `origin/main` to make the same commands pass. The same
// thing cost #966 a round earlier the same evening.
//
// So the tree verification judges is the tree the gate will judge: the
// branch with its base merged in, prepared in a THROWAWAY WORKTREE and never
// committed. The pushed branch is untouched and the worker still owns git —
// nothing here commits, amends, rebases or pushes anything.
//
// THE BASE IS REFRESHED BEFORE IT IS COUNTED AGAINST. The container fetched
// the base when the attempt was initialized — BEFORE the worker's turn, which
// is minutes or hours of wall clock and, on a busy trunk, several merges ago.
// Counting against that ref answers "level" for a branch the gate will judge
// against a base that has since moved, which is the very round this file
// exists to end; so the base is fetched here, and the COMMIT that fetch
// landed on is what the count and the merge both name.
//
// WORK THE CHECKOUT HAS NOT COMMITTED IS CARRIED INTO THE MERGE RESULT, not
// used as a reason to abandon it. A worktree made from HEAD would hold the
// tree as it was BEFORE the worker's turn, so the working tree is first
// snapshotted into a commit OBJECT (SnapshotWorkTree) and the worktree is made
// from that. Verifying the checkout instead would judge the stale tree, which
// is the wasted round this file exists to end — and off the ephemeral tier
// PushTurn commits nothing between turns, so a dirty checkout is not a corner
// case there but the ORDINARY state of every attempt.
//
// WHEN THE BRANCH IS LEVEL WITH ITS BASE NOTHING ELSE HAPPENS. There is no
// worktree, no snapshot and no merge — one fetch and one count — and
// verification runs exactly as it did before.

// mergeResultDirPrefix names the throwaway worktree, which is a SIBLING of
// the project checkout rather than a directory inside it.
//
// BESIDE, NOT INSIDE, for two reasons. A worktree inside the checkout is
// untracked content the publish step's `git add --all` would commit into the
// delivery. And the checkout root is where the context repositories live —
// the handbook the conventions are in, beside the project, which every
// `../handbook/...` path in this repository resolves through — so a worktree
// beside the project sees the same neighbours the project does, and a build
// that reads one is not judged against a directory that vanished.
const mergeResultDirPrefix = ".merge-result-"

// mergeResultDir is where the throwaway worktree for a checkout goes.
func mergeResultDir(workDir string) string {
	return path.Join(path.Dir(workDir), mergeResultDirPrefix+path.Base(workDir))
}

// mergeResultIndex is the throwaway index the worker's working tree is staged
// into — a COPY of the worker's own (SeedSnapshotIndex) — and it sits BESIDE
// the checkout for the same reasons the worktree does: a file inside it is
// untracked content the publish step's `git add --all` would commit into the
// delivery, and this one would carry a copy of the very work it is staging.
func mergeResultIndex(workDir string) string {
	return mergeResultDir(workDir) + ".index"
}

// gitIndexFileEnv is how git is told to stage somewhere that is not the
// repository's own index.
const gitIndexFileEnv = "GIT_INDEX_FILE"

// mergeResultSnapshotMessage names the throwaway snapshot commit. Nobody will
// ever read it from a log — it is reachable only from a worktree that is
// removed minutes later — but an object with no message is harder to explain
// than one that says what it was for.
const mergeResultSnapshotMessage = "merge-result snapshot of the working tree (never pushed)"

// mergeResult is what verification is about to judge, and why.
type mergeResult struct {
	// Behind is how many commits the branch is behind its base. Zero is the
	// ordinary case: the branch is level, and nothing below applies.
	Behind int

	// Base is the branch counted against, named as the worker knows it.
	Base string

	// WorkDir is the throwaway worktree holding the merge result, empty only
	// when the branch is level with its base and there was nothing to
	// prepare. A merge result that could not be prepared is an ERROR, never
	// an empty WorkDir the caller might read as "judge the checkout".
	WorkDir string

	// Uncommitted records that the checkout held work no commit carries, so
	// the merge result was built on a SNAPSHOT of the working tree rather
	// than on HEAD. See SnapshotWorkTree.
	Uncommitted bool

	// release removes the worktree and puts back whatever the preparation
	// moved. Nil when there was nothing to undo.
	//
	// IT ANSWERS WHAT IT COULD NOT UNDO. A removal that failed leaves a
	// worktree in a checkout the next attempt will use, so the caller has to
	// be able to say so; silence would make "cleaned up" and "could not"
	// look identical from the job afterwards.
	release func() error
}

// Release undoes the preparation. It is nil-safe and idempotent, and answers
// what could not be undone.
func (m *mergeResult) Release() error {
	if m == nil || m.release == nil {
		return nil
	}
	release := m.release
	m.release = nil
	return release()
}

// Note is the line that leads a round's evidence when the branch is behind
// its base, and empty when it is not.
//
// IT GOES IN FRONT OF THE FAILURES, not after them. A worker reading a test
// failure first will fix the test; a worker reading "your branch is 1 commit
// behind main" first knows to look at the base before it writes anything,
// which is the round this whole file exists to save.
func (m *mergeResult) Note() string {
	if m == nil || m.Behind == 0 {
		return ""
	}
	commits := "commits"
	if m.Behind == 1 {
		commits = "commit"
	}
	note := "Your branch is " + strconv.Itoa(m.Behind) + " " + commits + " behind `" + m.Base +
		"`; rebase or merge before fixing anything below."
	if m.WorkDir == "" {
		return note
	}
	note += "\n\nThe commands below ran on the MERGE RESULT — your branch with `" +
		gitRemote + "/" + m.Base + "` merged in"
	if m.Uncommitted {
		note += ", carrying the work your checkout has not committed"
	}
	note += " — which is the tree the forge's checks judge, so nothing they report is " +
		"explained by the base having moved."
	return note
}

// FetchBase brings the base up to date and answers the COMMIT the fetch
// landed on.
//
// THE CHECKOUT'S FETCH IS TOO OLD TO COUNT AGAINST. It ran when the attempt
// was initialized, before the worker was prompted; a base that moved during
// the turn — which is exactly how #965 and #966 lost their rounds, main
// fixing an environment-sensitive test while the attempt was running — is
// invisible to it. Counting against a stale ref answers "level" for a branch
// that is not, and verification then judges the stale tree and calls it the
// merge result.
//
// IT ANSWERS A COMMIT, NOT A REF, and everything after it names that commit.
// A base that moves again between the count and the merge would otherwise
// have verification judge a tree it never counted — the same confusion one
// layer down.
func (p *publisher) FetchBase(plan publishPlan) (string, error) {
	args := credentialHelperArgs(p.project.Git.RepoURL)
	args = append(args, "fetch", gitRemote, plan.Base)
	if _, err := p.git(plan, args...); err != nil {
		return "", err
	}
	// FETCH_HEAD is the commit THAT fetch landed on, which is stronger than
	// re-reading the remote-tracking ref: it cannot be answered by whatever
	// the ref happened to hold if the fetch updated nothing.
	result, err := p.git(plan, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", err
	}
	commit := result.Output()
	if commit == "" {
		return "", &PublishCommandError{
			TaskID:  p.taskID,
			Command: "rev-parse FETCH_HEAD",
			Output:  "the fetch of " + gitRemote + "/" + plan.Base + " landed on no commit",
		}
	}
	return commit, nil
}

// BehindBase is how many commits the base has that the branch does not. It is
// the one question asked of a branch that is level with its base, and its
// answer decides whether anything else in this file runs.
//
// It counts against the COMMIT FetchBase answered, never against the ref, so
// the tree that is counted is the tree that is merged.
func (p *publisher) BehindBase(plan publishPlan, base string) (int, error) {
	span := "HEAD.." + base
	result, err := p.probe(plan, "rev-list", "--count", span)
	if err != nil {
		return 0, err
	}
	if !result.Succeeded() {
		// A base the container cannot resolve is not "level": answering zero
		// would go on to judge the stale tree and call it the merge result.
		return 0, &PublishCommandError{
			TaskID:   p.taskID,
			Command:  "rev-list --count " + span,
			ExitCode: result.ExitCode,
			Output:   strings.TrimSpace(result.Stderr),
		}
	}
	count, convErr := strconv.Atoi(result.Output())
	if convErr != nil {
		return 0, &PublishCommandError{
			TaskID:  p.taskID,
			Command: "rev-list --count " + span,
			Output:  "git answered " + strconv.Quote(result.Output()) + " for a count",
		}
	}
	return count, nil
}

// MergeResult prepares the tree verification judges, and answers what it did.
//
// A BRANCH LEVEL WITH ITS BASE COSTS THE REFRESH AND THE COUNT AND NOTHING
// ELSE — no snapshot, no worktree, no merge, no cleanup — which is the
// overwhelmingly common case and must stay cheap.
//
// A CHECKOUT WITH UNCOMMITTED WORK IS SNAPSHOTTED, NOT ABANDONED. The
// worktree is made from a commit object carrying the working tree exactly as
// it stands, so the merge result is the worker's actual work merged with the
// base. Verifying the checkout itself instead would judge the STALE tree and
// spend the round this file exists to save — and since PushTurn commits
// nothing off the ephemeral tier, that was the ordinary path there, not a
// corner (#982, round 2).
//
// A MERGE THAT DOES NOT APPLY IS REFUSED, with the conflicting paths named.
// The attempt cannot be judged on a tree that does not exist, and the branch
// genuinely needs the worker's hands on it; a verification FAILURE would say
// the work is wrong, which is a different and false claim.
func (p *publisher) MergeResult(plan publishPlan) (*mergeResult, error) {
	base, err := p.FetchBase(plan)
	if err != nil {
		return nil, err
	}
	behind, err := p.BehindBase(plan, base)
	if err != nil {
		return nil, err
	}
	merged := &mergeResult{Behind: behind, Base: plan.Base}
	if behind == 0 {
		return merged, nil
	}
	changed, err := p.HasChanges(plan)
	if err != nil {
		return nil, err
	}
	merged.Uncommitted = changed
	dir := mergeResultDir(plan.WorkDir)
	// release undoes everything the preparation put beside the checkout. Both
	// removals are attempted and both answers are kept: dropping one to
	// report the other is how the survivor goes unnoticed.
	release := func() error {
		return errors.Join(p.RemoveMergeResult(plan, dir), p.RemoveSnapshotIndex(plan))
	}
	// What an earlier round left behind is REMOVED, never adopted: that
	// worktree holds a merge of a base that has since moved, and `worktree
	// add` would refuse the directory anyway. A removal that FAILS stops the
	// preparation here, because the add is going to fail on it next.
	if err := release(); err != nil {
		return nil, err
	}
	// THE WORKTREE IS MADE FROM THE WORK, whichever object carries it: HEAD
	// when the branch already holds the turn, and a snapshot of the working
	// tree when it does not.
	from := "HEAD"
	if changed {
		snapshot, err := p.SnapshotWorkTree(plan)
		if err != nil {
			return nil, withCleanup(err, release())
		}
		from = snapshot
	}
	if _, err := p.git(plan, "worktree", "add", "--detach", dir, from); err != nil {
		return nil, withCleanup(err, release())
	}
	inWorktree := plan
	inWorktree.WorkDir = dir
	// --no-commit because nothing here is a commit anyone will ever see, and
	// --no-ff so the tree is the merge of the two sides even when the branch
	// happens to carry no commit of its own. The base is named by the COMMIT
	// the fetch landed on, so this merges what was counted.
	result, err := p.probe(inWorktree, "merge", "--no-commit", "--no-ff", base)
	if err != nil {
		return nil, withCleanup(err, release())
	}
	if !result.Succeeded() {
		return nil, withCleanup(p.mergeFailure(plan, inWorktree, base, result), release())
	}
	merged.WorkDir = dir
	merged.release = release
	return merged, nil
}

// SnapshotWorkTree turns everything the checkout holds — its commits, plus
// every change no commit carries, modified file and new file alike — into ONE
// COMMIT OBJECT parented on HEAD, and answers that commit.
//
// IT TOUCHES NOTHING THE WORKER OWNS. The staging happens in a throwaway
// index beside the checkout (GIT_INDEX_FILE), so the worker's own index, its
// working tree, its branch and its HEAD are exactly as they were afterwards;
// the object is written but no ref moves, so nothing here can be pushed and
// the commit is reachable only from the worktree that is thrown away minutes
// later — after which it is an unreachable object, which is git's own to
// prune and not a file anyone has to clean up. `git add --all` records
// deletions as well as additions, so the tree that comes out is the working
// tree as it stands.
//
// THE THROWAWAY INDEX IS SEEDED FROM THE WORKER'S OWN (SeedSnapshotIndex),
// because `git add --all` into an EMPTY one is not the staging the delivery
// does. See that function: an empty index drops every file this repository
// tracks despite an ignore rule.
//
// IT IS NEVER SIGNED. A signature is a provenance claim about work a person
// or an agent stands behind; this object is scaffolding no one will pull,
// review or verify, and signing it would also make verification depend on the
// signing agent being alive. The author is the project's identity so git has
// a name to record, and gpgsign is stated OFF rather than left to the image.
func (p *publisher) SnapshotWorkTree(plan publishPlan) (string, error) {
	if err := p.SeedSnapshotIndex(plan); err != nil {
		return "", err
	}
	env := map[string]string{gitIndexFileEnv: mergeResultIndex(plan.WorkDir)}
	if _, err := p.gitEnv(plan, env, "add", "--all"); err != nil {
		return "", err
	}
	tree, err := p.gitEnv(plan, env, "write-tree")
	if err != nil {
		return "", err
	}
	if tree.Output() == "" {
		return "", &PublishCommandError{
			TaskID:  p.taskID,
			Command: "write-tree",
			Output:  "git staged the working tree and named no tree for it",
		}
	}
	args := append(gitConfigArgs(p.project, nil), "commit-tree", tree.Output(),
		"-p", "HEAD", "-m", mergeResultSnapshotMessage)
	snapshot, err := p.git(plan, args...)
	if err != nil {
		return "", err
	}
	if snapshot.Output() == "" {
		return "", &PublishCommandError{
			TaskID:  p.taskID,
			Command: "commit-tree",
			Output:  "git wrote no commit for the snapshot of the working tree",
		}
	}
	return snapshot.Output(), nil
}

// SeedSnapshotIndex puts the worker's own index into the throwaway one, so
// what is staged on top of it is what the DELIVERY would stage.
//
// AN EMPTY INDEX IS NOT AN EMPTY OPINION. `git add --all` skips a file the
// ignore rules match unless the index already holds it — and a file being
// TRACKED DESPITE AN IGNORE RULE is ordinary: this repository carries seven
// of them (`git ls-files -i -c --exclude-standard`, the vector-store markers
// under cmd/, pkg/server/ and sdk/). Staged into an index that starts empty
// they are not "unchanged", they are ABSENT: the snapshot tree records them
// as deleted, and the merge result verification judges is then a tree missing
// files the branch never touched — a failure the worker cannot explain, or a
// modify/delete conflict against a base that changed one of them, on work
// that is fine. The delivery has no such gap: Commit stages into the
// repository's real index, which already holds every tracked entry.
//
// COPYING THE INDEX IS WHAT MAKES THE TWO AGREE, and it carries the one thing
// re-reading HEAD would lose: a file the worker DELIBERATELY staged with
// `git add -f` despite an ignore rule is in the index and not in HEAD, and
// the delivery would commit it.
//
// IT IS A COPY, SO THE WORKER'S INDEX IS NEVER THE ONE WRITTEN TO. Everything
// after this stages into the copy, named by GIT_INDEX_FILE. The copy also
// REPLACES the destination outright, so an index an earlier round left beside
// the checkout cannot change the answer — it is removed all the same, because
// a stale one there is litter on a persistent workspace tier.
func (p *publisher) SeedSnapshotIndex(plan publishPlan) error {
	// --git-path answers where THIS checkout keeps its index, which is not
	// always ".git/index": a checkout that is itself a linked worktree keeps
	// it somewhere else entirely, and git is the only thing that knows where.
	result, err := p.git(plan, "rev-parse", "--git-path", "index")
	if err != nil {
		return err
	}
	index := result.Output()
	if index == "" {
		return &PublishCommandError{
			TaskID:  p.taskID,
			Command: "rev-parse --git-path index",
			Output:  "git named no index file for the checkout",
		}
	}
	_, err = p.run(plan, copyCommand, index, mergeResultIndex(plan.WorkDir))
	return err
}

// copyCommand copies the worker's index into the throwaway one. Git has no
// command that copies an index, and the two alternatives both lose something:
// `read-tree HEAD` drops what the worker staged, and `read-tree $(write-tree)`
// refuses the intent-to-add and unmerged entries a working checkout may hold.
const copyCommand = "cp"

// withCleanup keeps the failure that matters and never drops the one that
// happened while undoing it. A cleanup that succeeded — the ordinary case —
// leaves the original error exactly as typed as it was.
func withCleanup(err, cleanupErr error) error {
	if cleanupErr == nil {
		return err
	}
	return errors.Join(err, cleanupErr)
}

// cleanupGit runs one git command that UNDOES something this publisher did,
// through the seam that survives the attempt's context, and refuses on a
// non-zero exit.
//
// IT IS NOT A PROBE. "the worktree is already gone" is answered by asking git
// what worktrees it holds (WorktreeRegistered), not by shrugging at an exit
// status — so every non-zero exit here is a removal that FAILED, and a failed
// removal that read as "already gone" is precisely how a worktree survives
// into the next attempt with nobody told.
func (p *publisher) cleanupGit(plan publishPlan, args ...string) (*orchestration.ExecResult, error) {
	return p.cleanupRun(plan, append([]string{gitCommand}, args...)...)
}

// cleanupRun is the seam itself: one command that UNDOES something this
// publisher did, run on a context that outlives the attempt's, refusing on a
// non-zero exit. Not every undo git can do — a file git never tracked is
// removed by the one tool that removes files.
func (p *publisher) cleanupRun(plan publishPlan, cmd ...string) (*orchestration.ExecResult, error) {
	run := p.cleanup
	if run == nil {
		run = p.exec
	}
	return p.runWith(run, plan, cmd...)
}

// run is one non-git program that is part of PREPARING a merge result rather
// than undoing one, so it goes through the attempt's own seam: a preparation
// issued after the attempt was cancelled has nothing left to prepare for.
func (p *publisher) run(plan publishPlan, cmd ...string) (*orchestration.ExecResult, error) {
	return p.runWith(p.exec, plan, cmd...)
}

// runWith runs one non-git command in the checkout through the seam it is
// given, refusing on a non-zero exit with the command named.
func (p *publisher) runWith(
	run func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error),
	plan publishPlan,
	cmd ...string,
) (*orchestration.ExecResult, error) {
	result, err := run(&orchestration.ExecSpec{
		Cmd:     cmd,
		WorkDir: plan.WorkDir,
	})
	if err != nil {
		return nil, err
	}
	if !result.Succeeded() {
		return nil, &PublishCommandError{
			TaskID:   p.taskID,
			Command:  strings.Join(cmd, " "),
			ExitCode: result.ExitCode,
			Output:   strings.TrimSpace(result.Stderr + "\n" + result.Stdout),
		}
	}
	return result, nil
}

// WorktreeRegistered reports whether git holds a worktree at dir.
//
// IT IS WHAT TELLS "NOTHING TO REMOVE" FROM "COULD NOT REMOVE". Both are a
// non-zero `worktree remove`, and treating them alike either loses a real
// failure or reports one every time there was simply nothing there. A
// worktree whose DIRECTORY is gone but whose registration is not is still
// listed, and `worktree remove --force` still removes it, which is the
// crashed-container case answered correctly.
func (p *publisher) WorktreeRegistered(plan publishPlan, dir string) (bool, error) {
	result, err := p.cleanupGit(plan, "worktree", "list", "--porcelain")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(result.Output(), "\n") {
		if strings.TrimSpace(line) == "worktree "+dir {
			return true, nil
		}
	}
	return false, nil
}

// RemoveMergeResult throws the merge result away, and answers only what it
// could not do: a worktree that was never there is the ordinary answer, and
// one that would not go away is a failure the caller has to be able to state.
func (p *publisher) RemoveMergeResult(plan publishPlan, dir string) error {
	registered, err := p.WorktreeRegistered(plan, dir)
	if err != nil || !registered {
		return err
	}
	_, err = p.cleanupGit(plan, "worktree", "remove", "--force", dir)
	return err
}

// RemoveSnapshotIndex throws away the index the working tree was staged in.
//
// "rm -f" IS THE WHOLE ANSWER, and it is the one undo here that git cannot
// do: the file is not in any repository, so no git command removes it. A file
// that was never there exits zero, which is the ordinary case and not a
// failure; anything else is a real failure, and the checkout it would be left
// beside OUTLIVES the job on a persistent workspace tier.
func (p *publisher) RemoveSnapshotIndex(plan publishPlan) error {
	_, err := p.cleanupRun(plan, removeCommand, "-f", mergeResultIndex(plan.WorkDir))
	return err
}

// removeCommand removes the throwaway index, which is a file no repository
// tracks and so a file no git command removes.
const removeCommand = "rm"

// conflictPaths are the paths a refused merge left unmerged, which is both
// the one thing the worker needs to know to resolve them and the one thing
// that says the merge CONFLICTED rather than merely failed.
//
// A PROBE THAT CANNOT ANSWER RETURNS ITS FAILURE INSTEAD OF AN EMPTY LIST.
// "git named no unmerged path" and "git could not be asked" are different
// facts, and reading the second as the first is how a broken container comes
// to be reported as a branch that conflicts, or the other way about. The
// caller decides what each one means, which it cannot do from a list alone.
func (p *publisher) conflictPaths(inWorktree publishPlan) ([]string, error) {
	const probe = "diff --name-only --diff-filter=U"
	result, err := p.probe(inWorktree, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, &PublishCommandError{
			TaskID:  p.taskID,
			Command: probe,
			Output:  "git named no answer for the unmerged paths",
		}
	}
	if !result.Succeeded() {
		return nil, &PublishCommandError{
			TaskID:   p.taskID,
			Command:  probe,
			ExitCode: result.ExitCode,
			Output:   strings.TrimSpace(result.Stderr),
		}
	}
	var paths []string
	for _, line := range strings.Split(result.Output(), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths, nil
}

// mergeFailure says which of TWO DIFFERENT EVENTS a non-zero `git merge` was,
// because they wear the same exit status and call for opposite responses.
//
// A CONFLICT IS ONLY EVER REPORTED WHEN GIT NAMED THE UNMERGED PATHS. The
// refusal tells the worker to put its hands on the branch, so it must be a
// claim about the branch: a merge that failed because the worktree was not
// there, because git refused the image's configuration, or because the disk
// filled is a COMMAND that failed, and answering it with "resolve your
// conflicts" sends the worker after a conflict that does not exist —
// unfindable, because the branch merges cleanly.
//
// THE PROBE'S OWN FAILURE IS NEVER SWALLOWED. When git could not be asked
// which paths are unmerged, that failure is the reason the merge is being
// reported as a command failure rather than as a conflict, so it travels
// alongside it; dropping it would leave the command failure unexplained and
// the broken probe invisible.
func (p *publisher) mergeFailure(plan, inWorktree publishPlan, base string, result *orchestration.ExecResult) error {
	conflicts, probeErr := p.conflictPaths(inWorktree)
	if probeErr == nil && len(conflicts) > 0 {
		return &MergeResultConflictError{
			TaskID: p.taskID,
			Branch: plan.Branch,
			Base:   gitRemote + "/" + plan.Base,
			Paths:  conflicts,
			Output: strings.TrimSpace(result.Stderr + "\n" + result.Stdout),
		}
	}
	failed := &PublishCommandError{
		TaskID:   p.taskID,
		Command:  "merge --no-commit --no-ff " + base,
		ExitCode: result.ExitCode,
		Output:   strings.TrimSpace(result.Stderr + "\n" + result.Stdout),
	}
	if probeErr != nil {
		return errors.Join(failed, probeErr)
	}
	return failed
}
