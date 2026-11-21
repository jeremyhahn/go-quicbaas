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
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The train's git, as argument vectors
// =============================================================================
//
// The builder is driven against a scripted container: every command it runs is
// recorded, and each answers what git would. What is proven is the SHAPE of the
// build — a merge per change, never a rebase or a force, a conflict aborted
// rather than resolved, and a push through the one publication path.

// scriptedGit answers git commands by their subcommand line, configuration
// stripped. A command with no script succeeds with no output.
type scriptedGit struct {
	answers map[string]*orchestration.ExecResult
	ran     [][]string

	// specs are the commands exactly as they were run, configuration and
	// environment included.
	specs []*orchestration.ExecSpec
}

func (g *scriptedGit) exec(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	_, args := splitGitConfig(spec.Cmd[1:])
	g.ran = append(g.ran, args)
	g.specs = append(g.specs, spec)
	if answer, ok := g.answers[strings.Join(args, " ")]; ok {
		return answer, nil
	}
	return &orchestration.ExecResult{}, nil
}

// ranLine reports whether a command whose line starts with prefix ran.
func (g *scriptedGit) ranLine(prefix string) bool {
	for _, args := range g.ran {
		if strings.HasPrefix(strings.Join(args, " "), prefix) {
			return true
		}
	}
	return false
}

func newScriptedBuilder(git *scriptedGit) *containerTrainBuilder {
	project := testOrchProject()
	return &containerTrainBuilder{
		gitSteps: gitSteps{exec: git.exec, taskID: "merge_train:proj-1"},
		publisher: &publisher{exec: git.exec, project: project, taskID: "merge_train:proj-1",
			admit: func() error { return nil }},
		project: project,
		workDir: "/workspace/api",
		target:  "main",
	}
}

var buildCar = TrainCar{PullRequest: 101, Title: "Fix the widget", Branch: "issue/1-widget", HeadSHA: "abc123"}

func TestTrainBuild_AChangeIsMergedAtItsApprovedHead(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "abc123\n"},
		"rev-parse HEAD":       {Stdout: "merged1\n"},
	}}
	builder := newScriptedBuilder(git)
	commit, ejection, err := builder.Add(buildCar)
	if err != nil || ejection != nil {
		t.Fatalf("Add = %q %+v %v", commit, ejection, err)
	}
	if commit != "merged1" {
		t.Fatalf("commit = %q, want the train's new head", commit)
	}
	if !git.ranLine("fetch origin refs/heads/issue/1-widget") {
		t.Fatalf("the change's branch was not fetched: %v", git.ran)
	}
	var merge []string
	for _, args := range git.ran {
		if len(args) > 0 && args[0] == "merge" {
			merge = args
		}
	}
	if !slices.Contains(merge, "--no-ff") || merge[len(merge)-1] != "abc123" ||
		!slices.Contains(merge, "Merge #101: Fix the widget") {
		t.Fatalf("merge = %v, want a no-fast-forward merge of the approved head", merge)
	}
	for _, forbidden := range []string{"rebase", "reset", "push --force", "commit --amend"} {
		if git.ranLine(forbidden) {
			t.Fatalf("the build ran %q", forbidden)
		}
	}
}

func TestTrainBuild_ABranchThatMovedIsEjectedNotMerged(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "def456\n"},
	}}
	_, ejection, err := newScriptedBuilder(git).Add(buildCar)
	if err != nil {
		t.Fatal(err)
	}
	if ejection == nil || ejection.Reason != TrainEjectHeadMoved {
		t.Fatalf("ejection = %+v, want head moved", ejection)
	}
	if git.ranLine("merge") {
		t.Fatal("a head nobody approved was merged onto the train")
	}
}

func TestTrainBuild_AConflictIsAbortedAndNamed(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "abc123\n"},
		"merge --no-ff --no-edit -m Merge #101: Fix the widget abc123": {ExitCode: 1, Stdout: "CONFLICT (content)"},
		"diff --name-only --diff-filter=U":                             {Stdout: "pkg/widget.go\ndocs/widget.md\n"},
	}}
	_, ejection, err := newScriptedBuilder(git).Add(buildCar)
	if err != nil {
		t.Fatal(err)
	}
	if ejection == nil || ejection.Reason != TrainEjectConflict ||
		!strings.Contains(ejection.Detail, "pkg/widget.go") || !strings.Contains(ejection.Detail, "docs/widget.md") {
		t.Fatalf("ejection = %+v, want a conflict naming both files", ejection)
	}
	if !git.ranLine("merge --abort") {
		t.Fatal("the conflicted merge was left in the train")
	}
}

// TestTrainBuild_AMergeThatFailsForAnotherReasonIsAnError proves a failure
// that is not a conflict is not blamed on the change: with nothing to abort,
// the abort fails and the build reports it.
func TestTrainBuild_AMergeThatFailsForAnotherReasonIsAnError(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "abc123\n"},
		"merge --no-ff --no-edit -m Merge #101: Fix the widget abc123": {ExitCode: 128, Stderr: "error: gpg failed to sign the data"},
		"merge --abort": {ExitCode: 128, Stderr: "fatal: There is no merge to abort"},
	}}
	_, ejection, err := newScriptedBuilder(git).Add(buildCar)
	if ejection != nil || err == nil {
		t.Fatalf("Add = %+v %v, want an error and no ejection", ejection, err)
	}
}

func TestTrainBuild_AFailedFetchIsAnError(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"fetch origin refs/heads/issue/1-widget": {ExitCode: 128, Stderr: "fatal: unable to access"},
	}}
	if _, _, err := newScriptedBuilder(git).Add(buildCar); err == nil {
		t.Fatal("a fetch the forge refused was not reported")
	}
}

func TestTrainBuild_PushIsAPlainPushOfTheTrainBranch(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse refs/heads/queue/3": {Stdout: "merged2\n"},
	}}
	builder := newScriptedBuilder(git)
	head, err := builder.Push("queue/3")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if head != "merged2" {
		t.Fatalf("head = %q", head)
	}
	if !git.ranLine("push origin queue/3") {
		t.Fatalf("pushes = %v, want a plain push of queue/3", git.ran)
	}
	if git.ranLine("push --force") || git.ranLine("push -f") {
		t.Fatal("the train force-pushed")
	}
}

func TestTrainBuild_PushRefusedByTheLedgerPushesNothing(t *testing.T) {
	git := &scriptedGit{}
	builder := newScriptedBuilder(git)
	builder.publisher.admit = func() error { return errors.New("the ledger names another train") }
	if _, err := builder.Push("queue/3"); err == nil {
		t.Fatal("a train the ledger no longer names was pushed")
	}
	if git.ranLine("push") {
		t.Fatal("a refused train reached git push")
	}
}

func TestTrainBuild_BaseIsTheCheckoutsHead(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{"rev-parse HEAD": {Stdout: "base9\n"}}}
	base, err := newScriptedBuilder(git).Base()
	if err != nil || base != "base9" {
		t.Fatalf("Base = %q %v", base, err)
	}
}
