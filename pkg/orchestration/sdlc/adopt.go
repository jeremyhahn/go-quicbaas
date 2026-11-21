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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Adopting an issue — the operator verb that hands one back to the factory
// =============================================================================
//
// THERE WAS NO WAY BACK FOR AN ISSUE NO TASK HOLDS. The attention inbox and
// task_readmit act on a BLOCKED task; an issue whose task completed (go-luks2
// #209, its pull request #210 still open) or that triage holds with no task at
// all (#141, factory:escalated, pull request #194 open) had no QuicBaaS path.
// The workaround was a raw POST of a task with a hand-built source, and the
// SUBJECT had to be set to the pull request's slug, because the branch is
// planned from the subject and a subject that differed cut a rival branch.
//
// ADOPTION NAMES THE PULL REQUEST BY NUMBER, AND THE BRANCH COMES FROM ITS
// HEAD. The task carries the pull request's head ref and base as metadata,
// and the delivery's branch plan takes them over the strategy's (see
// branchPlanFor), so the attempt resumes the pull request that exists instead
// of guessing a name for one.

// Task metadata an adoption pins the delivery's branch with.
const (
	// MetadataPullRequest is the pull request an adopted task resumes.
	MetadataPullRequest = "pull_request"

	// MetadataPullRequestBranch is that pull request's head branch, which
	// the delivery works on instead of one the strategy would plan.
	MetadataPullRequestBranch = "pull_request_branch"

	// MetadataPullRequestTarget is that pull request's base branch, which
	// the delivery targets.
	MetadataPullRequestTarget = "pull_request_target"
)

// AdoptIssueRequest is an operator's instruction to adopt one issue.
type AdoptIssueRequest struct {
	// ProjectID is the bound project the issue belongs to.
	ProjectID string `json:"project_id"`

	// Issue is the issue number on the project's repository.
	Issue int `json:"issue"`

	// PullRequest, when set, is the open pull request the task resumes: its
	// head branch becomes the delivery's branch.
	PullRequest int `json:"pull_request,omitempty"`

	// Ruling, when set, is the operator's instruction: it is appended to the
	// task's contract and posted on the issue.
	Ruling string `json:"ruling,omitempty"`
}

// Validate refuses a request that names nothing to adopt.
func (r AdoptIssueRequest) Validate() error {
	switch {
	case strings.TrimSpace(r.ProjectID) == "":
		return &InvalidInputError{Field: "project_id", Message: "an adoption names the project the issue belongs to"}
	case r.Issue <= 0:
		return &InvalidInputError{Field: "issue", Message: "issues are numbered from one"}
	case r.PullRequest < 0:
		return &InvalidInputError{Field: "pull_request", Message: "pull requests are numbered from one; omit it to adopt none"}
	}
	return nil
}

// Adoption is what an adoption did.
type Adoption struct {
	// TaskID is the task created for the issue.
	TaskID string `json:"task_id"`

	// Issue is the adopted issue's number.
	Issue int `json:"issue"`

	// PullRequest is the pull request the task resumes, zero for none.
	PullRequest int `json:"pull_request,omitempty"`

	// Branch is the branch the delivery works on when a pull request was
	// named; empty lets the strategy plan one.
	Branch string `json:"branch,omitempty"`

	// Superseded is the terminal task that held the issue before, if any.
	Superseded string `json:"superseded,omitempty"`
}

// AdoptionTaskCreator creates the adopted task. orchestration.AgentService
// satisfies it.
type AdoptionTaskCreator interface {
	CreateTask(ctx context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error)
}

var _ AdoptionTaskCreator = (orchestration.AgentService)(nil)

// IssueAdopter adopts issues on an operator's instruction.
type IssueAdopter struct {
	tasks    AdoptionTaskCreator
	bindings BindingReader
	forges   ForgeResolver
	ranker   *Ranker
}

// NewIssueAdopter wires the adopter; every collaborator is required.
func NewIssueAdopter(tasks AdoptionTaskCreator, bindings BindingReader, forges ForgeResolver) (*IssueAdopter, error) {
	switch {
	case tasks == nil:
		return nil, &InvalidInputError{Field: "tasks", Message: "an adoption creates the issue's task through the engine"}
	case bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the issue's repository and labels live on the project's binding"}
	case forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "the issue and its pull request are read through the forge adapter"}
	}
	ranker, err := NewRanker(bindings)
	if err != nil {
		return nil, err
	}
	return &IssueAdopter{tasks: tasks, bindings: bindings, forges: forges, ranker: ranker}, nil
}

// Adopt creates the task for req's issue and hands the issue back to the
// factory.
//
// THE TASK IS CREATED FIRST, because creating it is what claims the issue:
// the store's source reservation supersedes a terminal holder and refuses a
// live one (orchestration.TaskSourceAlreadyAdoptedError), so a person told
// "adopted" is never looking at an issue some other task still owns. Only
// then do the labels move — trigger on, every park label off, as a ruling's
// hand-back moves them — and the ruling is posted. A failure after the task
// exists is an AdoptionIncompleteError naming it: the task runs, and the
// display it could not update is the next attempt's Prepare to correct.
func (a *IssueAdopter) Adopt(ctx context.Context, tenantID string, req AdoptIssueRequest) (*Adoption, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, &InvalidInputError{Field: "tenant_id", Message: "an adoption is made in a tenant"}
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	binding, err := a.bindings.GetBinding(ctx, tenantID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	forge, err := a.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return nil, err
	}
	if forge == nil {
		return nil, NewForgeNotRegisteredError(binding.Forge.Type)
	}
	repo := binding.Repo()
	issue, err := a.openIssue(ctx, forge, repo, req.Issue)
	if err != nil {
		return nil, err
	}
	metadata := issueSpecMetadata(repo, binding, issue.Number, nil)
	adoption := &Adoption{Issue: issue.Number}
	if req.PullRequest > 0 {
		pull, pullErr := a.openPullRequest(ctx, forge, repo, req.PullRequest)
		if pullErr != nil {
			return nil, pullErr
		}
		adoption.PullRequest, adoption.Branch = pull.Number, strings.TrimSpace(pull.HeadRef)
		metadata[MetadataPullRequest] = strconv.Itoa(pull.Number)
		metadata[MetadataPullRequestBranch] = adoption.Branch
		if base := strings.TrimSpace(pull.BaseRef); base != "" {
			metadata[MetadataPullRequestTarget] = base
		}
	}
	contract, err := a.contract(ctx, forge, repo, issue, req.Ruling)
	if err != nil {
		return nil, err
	}
	inputs, err := a.ranker.Inputs(ctx, tenantID, binding)
	if err != nil {
		return nil, err
	}
	task, err := a.tasks.CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:    tenantID,
		ProjectID:   req.ProjectID,
		Subject:     strings.TrimSpace(issue.Title),
		Description: contract,
		Source: &orchestration.TaskSourceRef{
			Kind: TaskSourceKindForgeIssue,
			Ref:  IssueRefString(repo, issue.Number),
			URL:  issue.URL,
		},
		Priority:  inputs.ComputedFor(issue.Labels),
		Runner:    JobRunnerName,
		AutoSpawn: true,
		Metadata:  metadata,
	})
	if err != nil {
		return nil, err
	}
	adoption.TaskID = task.ID
	adoption.Superseded = task.PredecessorTaskID()
	if err := handIssueBack(ctx, forge, binding, repo, issue); err != nil {
		return adoption, &AdoptionIncompleteError{TaskID: task.ID, Step: "moving the issue's labels", Cause: err}
	}
	if ruling := strings.TrimSpace(req.Ruling); ruling != "" {
		if _, err := forge.CommentOnIssue(ctx, repo, issue.Number, adoptionComment(adoption, ruling)); err != nil {
			return adoption, &AdoptionIncompleteError{TaskID: task.ID, Step: "posting the ruling", Cause: err}
		}
	}
	return adoption, nil
}

// openIssue reads the issue and refuses one that is not open.
func (a *IssueAdopter) openIssue(ctx context.Context, forge ForgeBackend, repo RepoRef, number int) (*ForgeIssue, error) {
	issue, err := forge.GetIssue(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, &NotFoundError{Entity: "issue", ID: IssueRefString(repo, number)}
	}
	if issueClosed(issue) {
		return nil, &AdoptionRefusedError{Subject: IssueRefString(repo, number), Reason: "the issue is closed; reopen it first"}
	}
	return issue, nil
}

// openPullRequest reads the pull request and refuses one that is not open or
// that names no head branch.
func (a *IssueAdopter) openPullRequest(ctx context.Context, forge ForgeBackend, repo RepoRef, number int) (*ForgePullRequest, error) {
	pull, err := forge.GetPullRequest(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	subject := repo.String() + "!" + strconv.Itoa(number)
	if pull == nil {
		return nil, &NotFoundError{Entity: "pull request", ID: subject}
	}
	if pullRequestStateFor(pull) != PullRequestOpen {
		return nil, &AdoptionRefusedError{Subject: subject, Reason: "the pull request is " + pullRequestStateFor(pull).String() + ", not open"}
	}
	if strings.TrimSpace(pull.HeadRef) == "" {
		return nil, &AdoptionRefusedError{Subject: subject, Reason: "the forge reports no head branch for it"}
	}
	return pull, nil
}

// contract renders the task's description: the issue's contract, and the
// operator's ruling after it.
func (a *IssueAdopter) contract(ctx context.Context, forge ForgeBackend, repo RepoRef, issue *ForgeIssue, ruling string) (string, error) {
	comments, err := forge.ListIssueComments(ctx, repo, issue.Number)
	if err != nil {
		return "", err
	}
	contract, err := IssueContract(IssueContractSpec{
		Repo: repo, IssueNumber: issue.Number, Title: issue.Title, Body: issue.Body, Comments: comments,
	})
	if err != nil {
		return "", err
	}
	if ruling = strings.TrimSpace(ruling); ruling != "" {
		contract += "\n\n## " + attentionRulingActor + "\n\n" + ruling
	}
	return contract, nil
}

// adoptionComment is the comment an adoption with a ruling posts on the issue.
// The factory marker keeps the unblock sweep from reading it back as a fresh
// owner ruling.
func adoptionComment(adoption *Adoption, ruling string) string {
	var b strings.Builder
	b.WriteString(factoryCommentMarker + "adopt task=" + adoption.TaskID + " -->\n")
	b.WriteString("**" + attentionRulingActor + "**\n\n")
	b.WriteString(ruling + "\n\n")
	b.WriteString("The issue is adopted as task `" + adoption.TaskID + "`")
	if adoption.PullRequest > 0 {
		b.WriteString(", resuming pull request #" + strconv.Itoa(adoption.PullRequest) + " on `" + adoption.Branch + "`")
	}
	b.WriteString(".")
	return b.String()
}

// pinnedBranch applies an adopted pull request's head and base to a branch
// plan, when the task carries them.
//
// THE PULL REQUEST'S BRANCH IS A FACT, THE STRATEGY'S IS A GUESS. The strategy
// plans a name from the issue; an adopted pull request already has one, and a
// plan that differs cuts a rival branch the old pull request's commits can
// never be pushed over.
func pinnedBranch(plan BranchPlan, task *orchestration.Task) BranchPlan {
	if task == nil {
		return plan
	}
	branch := strings.TrimSpace(task.Metadata[MetadataPullRequestBranch])
	if branch == "" {
		return plan
	}
	plan.Branch = branch
	plan.RequiresPullRequest = true
	if target := strings.TrimSpace(task.Metadata[MetadataPullRequestTarget]); target != "" {
		plan.Target = target
	}
	return plan
}

// =============================================================================
// Errors
// =============================================================================

// ErrAdoptionRefused is returned when the forge's state rules an adoption out.
var ErrAdoptionRefused = errors.New("sdlc: the adoption was refused")

// ErrAdoptionIncomplete is returned when an adoption created its task and
// could not finish the forge writes after it.
var ErrAdoptionIncomplete = errors.New("sdlc: the adoption did not finish")

// AdoptionRefusedError reports an issue or pull request an adoption cannot
// act on as it stands.
type AdoptionRefusedError struct {
	// Subject names the issue or pull request.
	Subject string

	// Reason says why, for the operator.
	Reason string
}

// Error implements the error interface.
func (e *AdoptionRefusedError) Error() string {
	return "sdlc: " + e.Subject + " cannot be adopted: " + e.Reason
}

// Unwrap returns the sentinel for errors.Is support.
func (e *AdoptionRefusedError) Unwrap() error { return ErrAdoptionRefused }

// AdoptionIncompleteError reports an adoption whose task exists but whose
// forge writes after it did not all land.
type AdoptionIncompleteError struct {
	// TaskID is the task the adoption created. It runs.
	TaskID string

	// Step is the write that failed.
	Step string

	// Cause is the forge's refusal.
	Cause error
}

// Error implements the error interface.
func (e *AdoptionIncompleteError) Error() string {
	msg := "sdlc: task " + e.TaskID + " was adopted, but " + e.Step + " failed"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is support.
func (e *AdoptionIncompleteError) Unwrap() []error { return []error{ErrAdoptionIncomplete, e.Cause} }
