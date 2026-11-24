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
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeTaskCreator records the task an adoption asks the engine for.
type fakeTaskCreator struct {
	created []*orchestration.CreateTaskRequest
	err     error
}

func (f *fakeTaskCreator) CreateTask(_ context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.created = append(f.created, req)
	return &orchestration.Task{
		ID: "task-adopted", TenantID: req.TenantID, ProjectID: req.ProjectID, Subject: req.Subject,
		Source: req.Source, Metadata: req.Metadata, Status: orchestration.TaskStatusPending,
	}, nil
}

// adoptionRig is an adopter over a fake forge holding go-luks2#141 (held by
// triage) and its open pull request #194 on a branch whose name the issue
// title would never produce.
type adoptionRig struct {
	adopter *IssueAdopter
	tasks   *fakeTaskCreator
	forge   *fakeForge
}

func newAdoptionRig(t *testing.T) *adoptionRig {
	t.Helper()
	forge := newFakeForge()
	forge.putIssue(141, "Raise pkg/luks2 coverage to the gate", "factory:escalated", HoldLabel, "area:tests")
	forge.prs[194] = &ForgePullRequest{Number: 194, State: "open", HeadRef: "issue/141-coverage-gate",
		BaseRef: "main", HeadSHA: "02b6c1ea", URL: "http://forge.test/acme/api/pulls/194"}
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	tasks := &fakeTaskCreator{}
	adopter, err := NewIssueAdopter(tasks, &fakeBindings{binding: binding}, &fakeForges{forge: forge})
	if err != nil {
		t.Fatalf("NewIssueAdopter: %v", err)
	}
	return &adoptionRig{adopter: adopter, tasks: tasks, forge: forge}
}

// TestIssueAdopter_AdoptsTheIssueOnThePullRequestsOwnBranch is X6: the task
// is created for the issue's source, the branch comes from the named pull
// request's head, the issue is handed back and the ruling is posted.
func TestIssueAdopter_AdoptsTheIssueOnThePullRequestsOwnBranch(t *testing.T) {
	rig := newAdoptionRig(t)
	adoption, err := rig.adopter.Adopt(context.Background(), testTenant, AdoptIssueRequest{
		ProjectID: testProject, Issue: 141, PullRequest: 194, Ruling: "Land #194 as it stands.",
	})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if adoption.TaskID != "task-adopted" || adoption.PullRequest != 194 || adoption.Branch != "issue/141-coverage-gate" {
		t.Fatalf("adoption = %+v", adoption)
	}
	if len(rig.tasks.created) != 1 {
		t.Fatalf("tasks created = %d, want one", len(rig.tasks.created))
	}
	req := rig.tasks.created[0]
	if req.Source == nil || req.Source.Kind != TaskSourceKindForgeIssue || req.Source.Ref != IssueRefString(testRepoRef, 141) {
		t.Fatalf("source = %+v, want the issue", req.Source)
	}
	if req.ProjectID != testProject || req.Runner != JobRunnerName || !req.AutoSpawn {
		t.Fatalf("request = %+v, want an auto-spawned sdlc task of the project", req)
	}
	if req.Subject != "Raise pkg/luks2 coverage to the gate" {
		t.Fatalf("subject = %q, want the issue's own title — the branch no longer depends on it", req.Subject)
	}
	if req.Metadata[MetadataPullRequestBranch] != "issue/141-coverage-gate" || req.Metadata[MetadataPullRequest] != "194" ||
		req.Metadata[MetadataPullRequestTarget] != "main" || req.Metadata[MetadataIssueNumber] != "141" {
		t.Fatalf("metadata = %v", req.Metadata)
	}
	if !strings.Contains(req.Description, "Land #194 as it stands.") {
		t.Fatalf("the ruling is not in the contract:\n%s", req.Description)
	}
	labels := rig.forge.issueLabels(141)
	if !slices.Contains(labels, "task:ready") || slices.Contains(labels, "factory:escalated") || slices.Contains(labels, HoldLabel) {
		t.Fatalf("labels = %v, want the trigger on and every hold off", labels)
	}
	if !slices.Contains(labels, "area:tests") {
		t.Fatalf("labels = %v, want an unowned label left alone", labels)
	}
	comments := rig.forge.commentsOn(141)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, factoryCommentMarker+"adopt task=task-adopted") ||
		!strings.Contains(comments[0].Body, "pull request #194") {
		t.Fatalf("comments = %+v, want the ruling posted with the factory marker", comments)
	}
}

// TestIssueAdopter_WithoutAPullRequestTheStrategyPlansTheBranch: no pull
// request named, no branch pinned and no ruling posted.
func TestIssueAdopter_WithoutAPullRequestTheStrategyPlansTheBranch(t *testing.T) {
	rig := newAdoptionRig(t)
	adoption, err := rig.adopter.Adopt(context.Background(), testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if adoption.Branch != "" || adoption.PullRequest != 0 {
		t.Fatalf("adoption = %+v, want no branch pinned", adoption)
	}
	if _, pinned := rig.tasks.created[0].Metadata[MetadataPullRequestBranch]; pinned {
		t.Fatalf("metadata = %v, want no branch pinned", rig.tasks.created[0].Metadata)
	}
	if comments := rig.forge.commentsOn(141); len(comments) != 0 {
		t.Fatalf("comments = %+v, want none without a ruling", comments)
	}
}

// TestIssueAdopter_RefusesWhatItCannotAdopt: bad input, a closed issue, a
// pull request that is not open or has no head branch, and a missing issue
// are all refused before any task exists.
func TestIssueAdopter_RefusesWhatItCannotAdopt(t *testing.T) {
	for name, tc := range map[string]struct {
		tenant  string
		req     AdoptIssueRequest
		prepare func(*fakeForge)
		want    error
	}{
		"no tenant":           {"", AdoptIssueRequest{ProjectID: testProject, Issue: 141}, nil, ErrInvalidInput},
		"no project":          {testTenant, AdoptIssueRequest{Issue: 141}, nil, ErrInvalidInput},
		"no issue":            {testTenant, AdoptIssueRequest{ProjectID: testProject}, nil, ErrInvalidInput},
		"negative pull":       {testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141, PullRequest: -1}, nil, ErrInvalidInput},
		"missing issue":       {testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 999}, nil, ErrNotFound},
		"missing pull":        {testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141, PullRequest: 999}, nil, ErrNotFound},
		"closed issue":        {testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141}, func(f *fakeForge) { f.issues[141].State = "closed" }, ErrAdoptionRefused},
		"merged pull":         {testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141, PullRequest: 194}, func(f *fakeForge) { f.prs[194].Merged, f.prs[194].State = true, "closed" }, ErrAdoptionRefused},
		"closed pull":         {testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141, PullRequest: 194}, func(f *fakeForge) { f.prs[194].State = "closed" }, ErrAdoptionRefused},
		"pull with no branch": {testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141, PullRequest: 194}, func(f *fakeForge) { f.prs[194].HeadRef = " " }, ErrAdoptionRefused},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newAdoptionRig(t)
			if tc.prepare != nil {
				tc.prepare(rig.forge)
			}
			_, err := rig.adopter.Adopt(context.Background(), tc.tenant, tc.req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(rig.tasks.created) != 0 {
				t.Fatalf("a task was created for a refused adoption: %+v", rig.tasks.created)
			}
		})
	}
}

// TestIssueAdopter_AHeldSourceIsTheEnginesRefusal: a live task still holding
// the issue is refused by the store, and nothing is written to the forge.
func TestIssueAdopter_AHeldSourceIsTheEnginesRefusal(t *testing.T) {
	rig := newAdoptionRig(t)
	rig.tasks.err = &orchestration.TaskSourceAlreadyAdoptedError{TaskID: "task-live", HolderStatus: orchestration.TaskStatusBlocked}
	_, err := rig.adopter.Adopt(context.Background(), testTenant, AdoptIssueRequest{ProjectID: testProject, Issue: 141, Ruling: "go"})
	if !errors.Is(err, orchestration.ErrTaskSourceAlreadyAdopted) {
		t.Fatalf("err = %v, want the store's refusal", err)
	}
	if adds := rig.forge.labelAdds; len(adds) != 0 {
		t.Fatalf("labels added = %v, want the issue untouched", adds)
	}
	if comments := rig.forge.commentsOn(141); len(comments) != 0 {
		t.Fatalf("comments = %+v, want none", comments)
	}
}

// TestIssueAdopter_AFailedForgeWriteAfterTheTaskNamesTheTask: the task runs;
// the operator is told which write did not land and which task exists.
func TestIssueAdopter_AFailedForgeWriteAfterTheTaskNamesTheTask(t *testing.T) {
	for name, prepare := range map[string]func(*fakeForge){
		"labels":  func(f *fakeForge) { f.addLabelErr = errors.New("forge refused the label") },
		"comment": func(f *fakeForge) { f.commentErr = errors.New("forge refused the comment") },
	} {
		t.Run(name, func(t *testing.T) {
			rig := newAdoptionRig(t)
			prepare(rig.forge)
			adoption, err := rig.adopter.Adopt(context.Background(), testTenant, AdoptIssueRequest{
				ProjectID: testProject, Issue: 141, Ruling: "go",
			})
			var incomplete *AdoptionIncompleteError
			if !errors.As(err, &incomplete) || !errors.Is(err, ErrAdoptionIncomplete) || incomplete.TaskID != "task-adopted" {
				t.Fatalf("err = %v, want AdoptionIncompleteError naming the task", err)
			}
			if adoption == nil || adoption.TaskID == "" {
				t.Fatal("the adoption that created a task did not return it")
			}
		})
	}
}

func TestNewIssueAdopter_RefusesEachMissingCollaborator(t *testing.T) {
	tasks, bindings, forges := &fakeTaskCreator{}, &fakeBindings{}, &fakeForges{}
	for name, build := range map[string]func() (*IssueAdopter, error){
		"tasks":    func() (*IssueAdopter, error) { return NewIssueAdopter(nil, bindings, forges) },
		"bindings": func() (*IssueAdopter, error) { return NewIssueAdopter(tasks, nil, forges) },
		"forges":   func() (*IssueAdopter, error) { return NewIssueAdopter(tasks, bindings, nil) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := build(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestPinnedBranch_ThePullRequestsBranchOverridesTheStrategys: an adopted
// task's plan works on the pull request's head and targets its base; any
// other task keeps the strategy's plan.
func TestPinnedBranch_ThePullRequestsBranchOverridesTheStrategys(t *testing.T) {
	planned := BranchPlan{Branch: "issue/141-raise-pkg-luks2-coverage", Base: "main", Target: "main", DeleteAfterMerge: true}
	adopted := &orchestration.Task{Metadata: map[string]string{
		MetadataPullRequestBranch: "issue/141-coverage-gate", MetadataPullRequestTarget: "develop",
	}}
	got := pinnedBranch(planned, adopted)
	if got.Branch != "issue/141-coverage-gate" || got.Target != "develop" || !got.RequiresPullRequest || !got.DeleteAfterMerge {
		t.Fatalf("plan = %+v", got)
	}
	for name, task := range map[string]*orchestration.Task{
		"no task":     nil,
		"no metadata": {},
		"no branch":   {Metadata: map[string]string{MetadataPullRequestTarget: "develop"}},
	} {
		if got := pinnedBranch(planned, task); got != planned {
			t.Fatalf("%s: plan = %+v, want the strategy's untouched", name, got)
		}
	}
}

func TestAdoptionErrors_ReadAsOneLine(t *testing.T) {
	refused := (&AdoptionRefusedError{Subject: "acme/api#1", Reason: "the issue is closed"}).Error()
	if refused != "sdlc: acme/api#1 cannot be adopted: the issue is closed" {
		t.Fatalf("refused = %q", refused)
	}
	incomplete := (&AdoptionIncompleteError{TaskID: "t1", Step: "posting the ruling", Cause: errors.New("503")}).Error()
	if incomplete != "sdlc: task t1 was adopted, but posting the ruling failed: 503" {
		t.Fatalf("incomplete = %q", incomplete)
	}
}

// TestBranchPlanFor_AnAdoptedTaskWorksOnItsPullRequestsBranch: the delivery's
// plan, as Prepare makes it, names the adopted pull request's head — not the
// slug the strategy would cut from the task's subject.
func TestBranchPlanFor_AnAdoptedTaskWorksOnItsPullRequestsBranch(t *testing.T) {
	task := testTaskRecord(141)
	task.Subject = "Raise pkg/luks2 coverage to the gate"
	task.Metadata = map[string]string{MetadataPullRequestBranch: "issue/141-coverage-gate"}
	a := &attempt{project: testOrchProject(), binding: testBinding(), task: task, issue: 141}

	plan, err := branchPlanFor(a, nil)
	if err != nil {
		t.Fatalf("branchPlanFor: %v", err)
	}
	if plan.Branch != "issue/141-coverage-gate" || plan.Target == "" || plan.Base == "" {
		t.Fatalf("plan = %+v, want the pull request's branch with the project's refs filled in", plan)
	}

	task.Metadata = nil
	plan, err = branchPlanFor(a, nil)
	if err != nil {
		t.Fatalf("branchPlanFor: %v", err)
	}
	if plan.Branch == "issue/141-coverage-gate" {
		t.Fatal("a task with no adopted pull request was planned onto it")
	}
}
