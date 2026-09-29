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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Verification judges what the gate will judge (#982)
// =============================================================================
//
// These drive the WHOLE runner, because the property is not "a merge result
// can be prepared" — merge_result_test.go measures that — but "an attempt
// whose branch is behind its base reaches the pull request without spending a
// round on a failure main had already fixed".

// behindTheBase is the rig for a branch trailing its base: the worker's turn
// is committed and pushed (the ephemeral tier), so the checkout is clean and
// HEAD carries the work, which is what makes a worktree of HEAD honest.
func behindTheBase(t *testing.T, behind int, commands ...string) *runnerRig {
	t.Helper()
	rig := newRunnerRig(t)
	rig.storage = ephemeralWorkspaceTier()
	rig.rebindVerification(t, commands...)
	rig.workerWrote()
	// EVERY TURN WRITES SOMETHING, because on this tier the previous turn's
	// work was committed by the post-turn push and the checkout came back
	// clean. A worker that then changed nothing is a different case
	// altogether (the no-change retry), and it is not the one under test.
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.workerWrote() }
	rig.container.mu.Lock()
	rig.container.behind = behind
	rig.container.mu.Unlock()
	return rig
}

// TestRunner_ABranchBehindItsBaseIsVerifiedOnTheMergeResult is the burn-in
// property of #982, in one test: `make test` fails on the branch as pushed and
// passes on the branch merged with its base — which is exactly what CI's
// "Merge Result" job saw on #965 — and the attempt reaches its pull request
// WITHOUT a round.
func TestRunner_ABranchBehindItsBaseIsVerifiedOnTheMergeResult(t *testing.T) {
	rig := behindTheBase(t, 1, "make test")
	rig.container.scriptVerification("make test", &orchestration.ExecResult{
		ExitCode: 1,
		Stdout:   "--- FAIL: TestACPBackend_SpawnContainer_FileBasedAccount_MountsIsolatedDir\n",
		Stderr:   "make: *** [test] Error 1\n",
	})
	rig.container.scriptMergeResultVerification("make test", &orchestration.ExecResult{Stdout: "ok\n"})
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t, "make test")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("prompts = %d, want the first turn and NO round: the base moving is not the worker's defect\n%v",
			len(prompts), prompts)
	}
	audits := strings.Join(rig.auditDetails(auditVerification), "\n")
	if !strings.Contains(audits, "behind=1") || !strings.Contains(audits, "judged=merge_result") {
		t.Errorf("the job does not record what was judged:\n%s", audits)
	}
	rig.container.mu.Lock()
	defer rig.container.mu.Unlock()
	if len(rig.container.worktreeAdds) != 1 {
		t.Fatalf("worktree adds = %+v, want exactly one merge result", rig.container.worktreeAdds)
	}
	if len(rig.container.worktrees) != 0 {
		t.Errorf("the throwaway worktree outlived the attempt: %v", rig.container.worktrees)
	}
	// AND THE BRANCH THE WORKER OWNS IS UNTOUCHED: the merge is never
	// committed, so nothing the merge produced can be pushed.
	for _, merge := range rig.container.merges {
		if !containsArg(merge.Args, "--no-commit") {
			t.Errorf("a merge into the verification worktree committed: %+v", merge)
		}
	}
}

// TestRunner_ABaseThatMovesDuringTheWorkersTurnIsStillCountedAgainst is the
// window the checkout's own fetch cannot see. The container fetched the base
// when the attempt was initialized — before the worker was prompted — and
// `main` moved while the turn ran, which is precisely what happened to #965
// and #966: the fix for the environment-sensitive test landed mid-attempt. A
// count against the ref as initialization left it answers "level", verifies
// the stale tree and spends the round anyway.
func TestRunner_ABaseThatMovesDuringTheWorkersTurnIsStillCountedAgainst(t *testing.T) {
	// Level as of the checkout: nothing this container has fetched says
	// otherwise.
	rig := behindTheBase(t, 0, "make test")
	moved := 1
	wrote := rig.agents.onPrompt
	rig.agents.onPrompt = func(req *orchestration.PromptSessionRequest) {
		wrote(req)
		// The base moves WHILE the worker is working. Nothing in the
		// container knows it until something fetches.
		rig.container.mu.Lock()
		rig.container.behindAfterFetch = &moved
		rig.container.mu.Unlock()
	}
	rig.container.scriptVerification("make test", &orchestration.ExecResult{
		ExitCode: 1,
		Stdout:   "--- FAIL: TestBuildAgentRuntime_OpenVINOBackendReceivesTheContainerRuntime\n",
		Stderr:   "make: *** [test] Error 1\n",
	})
	rig.container.scriptMergeResultVerification("make test", &orchestration.ExecResult{Stdout: "ok\n"})
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t, "make test")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("prompts = %d, want the first turn and NO round: the base moved under the attempt\n%v",
			len(prompts), prompts)
	}
	audits := strings.Join(rig.auditDetails(auditVerification), "\n")
	if !strings.Contains(audits, "behind=1") || !strings.Contains(audits, "judged=merge_result") {
		t.Errorf("the base moving during the turn was never noticed:\n%s", audits)
	}
}

// TestRunner_ABranchLevelWithItsBaseVerifiesExactlyAsBefore is the inertness
// half, and it is the case every attempt in the fleet takes: no worktree, no
// merge, and a verification failure that says nothing about a base.
func TestRunner_ABranchLevelWithItsBaseVerifiesExactlyAsBefore(t *testing.T) {
	rig := behindTheBase(t, 0, "false")
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"false"}
		b.MaxReviewRounds = 1
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}

	err := rig.runner.Run(context.Background(), rig.job(t, "false"))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run err = %v, want an escalation on the failing command", err)
	}
	rig.container.mu.Lock()
	defer rig.container.mu.Unlock()
	if len(rig.container.worktreeAdds) != 0 || len(rig.container.merges) != 0 {
		t.Fatalf("a level branch was merged: worktrees=%+v merges=%+v",
			rig.container.worktreeAdds, rig.container.merges)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the first turn and one round", len(prompts))
	}
	if strings.Contains(prompts[1], "commits behind") || strings.Contains(prompts[1], "commit behind") {
		t.Errorf("a level branch was told to rebase:\n%s", prompts[1])
	}
}

// TestRunner_TheRoundEvidenceLeadsWithHowFarBehindTheBranchIs: when the merge
// result fails too, the work really is wrong — and the worker still reads the
// base having moved FIRST, because a worker that reads the failure first
// starts by fixing the test.
func TestRunner_TheRoundEvidenceLeadsWithHowFarBehindTheBranchIs(t *testing.T) {
	rig := behindTheBase(t, 2, "make test")
	if err := rig.rebind(context.Background(), func(b *ProjectBinding) {
		b.Verification = []string{"make test"}
		b.MaxReviewRounds = 1
		b.Forge.Labels[LabelEscalated] = "factory:escalated"
	}); err != nil {
		t.Fatal(err)
	}
	failing := &orchestration.ExecResult{
		ExitCode: 1,
		Stdout:   "--- FAIL: TestTheWorkersOwnDefect\n",
		Stderr:   "make: *** [test] Error 1\n",
	}
	rig.container.scriptVerification("make test", failing)
	rig.container.scriptMergeResultVerification("make test", failing)

	err := rig.runner.Run(context.Background(), rig.job(t, "make test"))
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("Run err = %v, want an escalation", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("prompts = %d, want the first turn and one round", len(prompts))
	}
	round := prompts[1]
	behind := strings.Index(round, "2 commits behind")
	if behind < 0 {
		t.Fatalf("the round does not say how far behind the branch is:\n%s", round)
	}
	failure := strings.Index(round, "TestTheWorkersOwnDefect")
	if failure < 0 {
		t.Fatalf("the round lost the failure evidence:\n%s", round)
	}
	if behind > failure {
		t.Errorf("the round puts the failure ahead of the base having moved:\n%s", round)
	}
	// AND THE EVIDENCE BOUND IS UNCHANGED: the note goes in FRONT of it, it
	// does not replace it.
	if !strings.Contains(round, "make: *** [test] Error 1") {
		t.Errorf("the round lost what the command said:\n%s", round)
	}
}

// TestRunner_ABranchThatDoesNotMergeIsRefusedNotFailed: a branch whose merge
// conflicts says nothing about the work, so it must not arrive as a
// verification failure — which would send the worker a round of feedback about
// its own commands and spend one of the attempt's turns on a defect that is
// not there.
func TestRunner_ABranchThatDoesNotMergeIsRefusedNotFailed(t *testing.T) {
	rig := behindTheBase(t, 1, "true")
	rig.container.mu.Lock()
	rig.container.mergeConflicts = []string{"pkg/sdlc/runner.go"}
	rig.container.mu.Unlock()

	err := rig.runner.Run(context.Background(), rig.job(t, "true"))
	var conflict *MergeResultConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Run err = %v, want a typed merge conflict", err)
	}
	if !strings.Contains(conflict.Error(), "pkg/sdlc/runner.go") {
		t.Errorf("the refusal does not name the conflicting path:\n%s", conflict.Error())
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("prompts = %d, want no round spent on a merge the worker must do itself", len(prompts))
	}
	rig.container.mu.Lock()
	defer rig.container.mu.Unlock()
	if len(rig.container.worktrees) != 0 {
		t.Errorf("a refused merge left its worktree behind: %v", rig.container.worktrees)
	}
}

// cleanupSeamAgents records the context every exec arrived on, which is the
// one question the detached cleanup seam exists to answer.
type cleanupSeamAgents struct {
	orchestration.AgentService

	live     []bool
	bounded  []bool
	sessions []string
}

func (a *cleanupSeamAgents) ExecSession(ctx context.Context, req *orchestration.ExecSessionRequest) (*orchestration.ExecResult, error) {
	_, bounded := ctx.Deadline()
	a.live = append(a.live, ctx.Err() == nil)
	a.bounded = append(a.bounded, bounded)
	a.sessions = append(a.sessions, req.SessionID)
	return &orchestration.ExecResult{}, nil
}

// TestRunnerCleanupExec_OutlivesTheStageAndStaysBounded is why the removal
// has a seam of its own. A verification that timed out or a daemon that is
// shutting down leaves the stage's context already done — which is exactly
// when the throwaway worktree has to go, because the checkout outlives the
// job on a persistent workspace tier. It is DETACHED, not unbounded: an undo
// with no deadline on a dying daemon is a hang.
func TestRunnerCleanupExec_OutlivesTheStageAndStaysBounded(t *testing.T) {
	agents := &cleanupSeamAgents{}
	run := &runnerRun{job: &orchestration.JobRun{Agents: agents}, sessionID: "session-1"}
	ctx, cancel := context.WithCancel(context.Background())
	cleanup, stage := run.containerCleanupExec(ctx), run.containerExec(ctx)
	cancel() // the verification timed out, or the daemon is going down

	spec := &orchestration.ExecSpec{Cmd: []string{"git", "worktree", "list", "--porcelain"}}
	if _, err := stage(spec); err != nil {
		t.Fatalf("the stage seam: %v", err)
	}
	if _, err := cleanup(spec); err != nil {
		t.Fatalf("the cleanup seam: %v", err)
	}

	if len(agents.live) != 2 {
		t.Fatalf("execs = %d, want the stage seam and the cleanup seam", len(agents.live))
	}
	if agents.live[0] {
		t.Error("the stage seam carried a live context after cancellation; the test proves nothing")
	}
	if !agents.live[1] {
		t.Error("the cleanup seam was cancelled with the stage, so the worktree stays behind")
	}
	if !agents.bounded[1] {
		t.Error("the cleanup seam is unbounded: an undo on a dying daemon must not hang")
	}
	if agents.sessions[1] != "session-1" {
		t.Errorf("the cleanup ran in session %q, want the worker's own", agents.sessions[1])
	}
}

// containsArg reports whether an argument vector carries one flag.
func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

// behindTheBaseUncommitted is the rig for a branch trailing its base on the
// PERSISTENT workspace tier, where the worker's turn is still sitting in the
// checkout as uncommitted work when verification runs.
//
// IT IS NOT A CORNER, IT IS THE TIER. PushTurn commits and pushes only on the
// ephemeral tier, where the tmpfs is the sole copy of the work; off it the
// checkout stays dirty between turns by design. So a fallback that verifies
// the branch as committed whenever the checkout is dirty does not fire on an
// exotic attempt — it fires on EVERY attempt on this tier, which is the whole
// of #982 left unclosed.
func behindTheBaseUncommitted(t *testing.T, behind int, commands ...string) *runnerRig {
	t.Helper()
	rig := newRunnerRig(t)
	rig.storage = orchestration.ProjectStorage{WorkspaceTier: orchestration.WorkspaceTierPersistent}
	rig.rebindVerification(t, commands...)
	rig.workerWrote()
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { rig.workerWrote() }
	rig.container.mu.Lock()
	rig.container.behind = behind
	rig.container.mu.Unlock()
	return rig
}

// TestRunner_ADirtyCheckoutBehindItsBaseIsStillVerifiedOnTheMergeResult is the
// persistent tier's half of #982's burn-in property. The worker's work is
// uncommitted, the branch is behind, and `make test` fails on the stale tree
// and passes on the merge result — and the attempt must still reach its pull
// request without a round, because the failure is the base's and not the
// worker's whether or not a commit happens to carry the work.
func TestRunner_ADirtyCheckoutBehindItsBaseIsStillVerifiedOnTheMergeResult(t *testing.T) {
	rig := behindTheBaseUncommitted(t, 1, "make test")
	rig.container.scriptVerification("make test", &orchestration.ExecResult{
		ExitCode: 1,
		Stdout:   "--- FAIL: TestACPBackend_SpawnContainer_FileBasedAccount_MountsIsolatedDir\n",
		Stderr:   "make: *** [test] Error 1\n",
	})
	rig.container.scriptMergeResultVerification("make test", &orchestration.ExecResult{Stdout: "ok\n"})
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t, "make test")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("prompts = %d, want the first turn and NO round: a dirty checkout is the persistent tier's ordinary state, not a reason to judge the stale tree\n%v",
			len(prompts), prompts)
	}
	audits := strings.Join(rig.auditDetails(auditVerification), "\n")
	if !strings.Contains(audits, "behind=1") || !strings.Contains(audits, "judged=merge_result") {
		t.Errorf("the job does not record that the merge result was judged:\n%s", audits)
	}
	rig.container.mu.Lock()
	defer rig.container.mu.Unlock()
	// THE WORK THE WORKER HAD NOT COMMITTED IS IN THE TREE THAT WAS JUDGED.
	// A worktree made from HEAD would hold the tree as it was BEFORE the turn,
	// so the one thing that proves the snapshot was used is that the worktree
	// was made from something other than HEAD.
	if len(rig.container.worktreeAdds) != 1 {
		t.Fatalf("worktree adds = %+v, want exactly one merge result", rig.container.worktreeAdds)
	}
	if ref := rig.container.worktreeAdds[0].Ref; ref == "HEAD" {
		t.Errorf("the merge result was made from HEAD, which is the tree before the worker touched it")
	}
	// AND NO COMMIT ON THE BRANCH CARRIES THE SNAPSHOT. It is an object the
	// throwaway worktree points at, never a commit on the worker's branch:
	// the delivery must carry what the worker wrote, under the message the
	// delivery chose, and nothing else.
	for _, message := range rig.container.commits {
		if strings.Contains(message, "snapshot") {
			t.Errorf("a snapshot reached the worker's branch as a commit: %q", message)
		}
	}
	if len(rig.container.worktrees) != 0 {
		t.Errorf("the throwaway worktree outlived the attempt: %v", rig.container.worktrees)
	}
}
