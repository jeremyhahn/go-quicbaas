// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Lifecycle hooks — the module at the five moments of a Job
// =============================================================================
//
// The engine calls each moment once, on the state it belongs to, for a job
// bound to a Task this module's source adopted. Every hook resolves the same
// things — the Project (repository URL, signing identity), its binding (the
// forge), the Task (the issue) and the Delivery (this attempt's branch) —
// and acts through the forge adapter. THERE IS NO GIT HERE: the commit, its
// signature and the push happen inside the worker's container (publish.go),
// and what reaches these hooks is the head they produced. A hook error fails
// the job with FailureClassHook; nothing here retries a job.
//
//   Prepare    the attempt's branch planned, the Delivery written, the issue
//              labelled in progress — and nothing on the filesystem
//   Reviewable the pull request opened (or refreshed) against the head the
//              container already pushed, head + PR recorded and published as
//              Task artifacts, issue labelled in review
//   Complete   merge, bound to the head the review approved
//   Discard    nothing; the container the worker ran in was the workspace
//   Blocked    the park reason posted to the issue, labelled accordingly

// DefaultMergeAttempts bounds the retries on a not-ready merge.
const DefaultMergeAttempts = 6

// DefaultMergeBackoff is the first wait between merge retries; each retry
// doubles it.
const DefaultMergeBackoff = 2 * time.Second

// RetryWaiter paces merge retries.
type RetryWaiter interface {
	Wait(ctx context.Context, attempt int) error
}

type backoffWaiter struct {
	base time.Duration
}

// Wait waits base × 2^(attempt-1), or returns early when ctx ends.
func (w backoffWaiter) Wait(ctx context.Context, attempt int) error {
	if attempt < 1 {
		attempt = 1
	}
	timer := time.NewTimer(w.base << uint(attempt-1))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// HooksConfig wires the hook set.
type HooksConfig struct {
	Projects   ProjectReader
	Bindings   BindingReader
	Deliveries DeliveryStore
	Tasks      TaskReader
	Forges     ForgeResolver

	// MergeAttempts bounds the not-ready retries; zero takes the default.
	MergeAttempts int

	// Waiter paces the retries; nil takes exponential backoff.
	Waiter RetryWaiter

	// Surfacing bounds the Blocked hook's post to the issue.
	Surfacing SurfacePolicy
}

// Hooks is the module's orchestration.JobLifecycleHooks.
type Hooks struct {
	projects   ProjectReader
	bindings   BindingReader
	deliveries DeliveryStore
	tasks      TaskReader
	forges     ForgeResolver
	attempts   int
	waiter     RetryWaiter
	surfacing  SurfacePolicy
	now        func() time.Time
}

var _ orchestration.JobLifecycleHooks = (*Hooks)(nil)

// NewHooks builds the hook set; every collaborator is required.
func NewHooks(cfg HooksConfig) (*Hooks, error) {
	switch {
	case cfg.Projects == nil:
		return nil, &InvalidInputError{Field: "projects", Message: "the repository and signing identity live on the project"}
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the forge lives on the binding"}
	case cfg.Deliveries == nil:
		return nil, &InvalidInputError{Field: "deliveries", Message: "the attempt's branch and pull request are recorded on the delivery"}
	case cfg.Tasks == nil:
		return nil, &InvalidInputError{Field: "tasks", Message: "the issue is the task's provenance"}
	case cfg.Forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "pull requests and labels are written through the forge adapter"}
	case cfg.MergeAttempts < 0:
		return nil, &InvalidInputError{Field: "merge_attempts", Message: "must not be negative"}
	}
	if err := cfg.Surfacing.Validate(); err != nil {
		return nil, err
	}
	attempts := cfg.MergeAttempts
	if attempts == 0 {
		attempts = DefaultMergeAttempts
	}
	waiter := cfg.Waiter
	if waiter == nil {
		waiter = backoffWaiter{base: DefaultMergeBackoff}
	}
	return &Hooks{
		projects: cfg.Projects, bindings: cfg.Bindings, deliveries: cfg.Deliveries, tasks: cfg.Tasks,
		forges: cfg.Forges, attempts: attempts, waiter: waiter,
		surfacing: cfg.Surfacing.Resolved(), now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// attempt is everything a hook resolves about the job it was called for.
type attempt struct {
	project *orchestration.Project
	binding *ProjectBinding
	task    *orchestration.Task
	repo    RepoRef
	issue   int
	forge   ForgeBackend
}

// resolve loads the project, binding, task and forge for a bound job.
//
// THE REPO COMES FROM THE TASK AND THE IDENTITY COMES FROM THE BINDING, and
// those are two different sources of truth for one write. issueOfTask parses
// owner/repo out of the task's Source.Ref, which a caller supplies through
// CreateTask, while the forge below is resolved from binding.Forge and carries
// the project's credentials. Where a project token can reach more than one
// repository, a task naming a foreign repo would be commented and labelled
// under this project's identity.
//
// Tracked as go-quicbaas#841, which refuses a parsed repo that is not
// binding.Repo() here and in resolveConcluded. It is a behaviour change on this
// hot path, so it is its own change and its own review rather than a line
// slipped into an unrelated one. The label reconciliation sweep is NOT a way
// in: indexTasksByIssue skips every task whose parsed repo is not the
// binding's, so nothing it concludes can be aimed elsewhere.
func (h *Hooks) resolve(ctx context.Context, job *orchestration.JobContext) (*attempt, error) {
	if job == nil || job.TaskID == "" {
		return nil, &InvalidInputError{Field: "job", Message: "the sdlc hooks serve jobs bound to a task, and this job is bound to none"}
	}
	project, err := h.projects.Get(ctx, job.TenantID, job.ProjectID)
	if err != nil {
		return nil, err
	}
	binding, err := h.bindings.GetBinding(ctx, job.TenantID, job.ProjectID)
	if err != nil {
		return nil, err
	}
	task, err := h.tasks.GetTask(ctx, job.TenantID, job.TaskID)
	if err != nil {
		return nil, err
	}
	repo, issue, err := issueOfTask(task)
	if err != nil {
		return nil, err
	}
	forge, err := h.forges.ForgeFor(ctx, job.TenantID, binding.Forge)
	if err != nil {
		return nil, err
	}
	if forge == nil {
		return nil, NewForgeNotRegisteredError(binding.Forge.Type)
	}
	return &attempt{project: project, binding: binding, task: task, repo: repo, issue: issue, forge: forge}, nil
}

// branchPlanFor plans the attempt's branch from the binding's strategy.
//
// The project's default branch is a DEFAULT the strategy reads, never an
// override applied behind it: a plan that named its own base and target keeps
// them, and only the refs the strategy left to the project are filled in. The
// override this replaced made every plan single-ref, which is why gitflow —
// cutting a hotfix from the release line and targeting it there while a
// feature cuts from develop — could not be expressed at all.
func branchPlanFor(a *attempt, issue *ForgeIssue) (BranchPlan, error) {
	strategy, err := LookupStrategy(a.binding.Strategy)
	if err != nil {
		return BranchPlan{}, err
	}
	ref := IssueRef{Number: a.issue, Title: a.task.Subject}
	if issue != nil {
		ref.Labels = issue.Labels
		ref.Milestone = issue.Milestone
	}
	plan, err := strategy.PlanBranch(ref)
	if err != nil {
		return BranchPlan{}, err
	}
	return plan.Resolve(a.project.Git.DefaultBranch), nil
}

// loadIssue reads the forge issue the attempt mirrors.
func (h *Hooks) loadIssue(ctx context.Context, a *attempt) (*ForgeIssue, error) {
	issue, err := a.forge.GetIssue(ctx, a.repo, a.issue)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, &NotFoundError{Entity: "issue", ID: IssueRefString(a.repo, a.issue)}
	}
	return issue, nil
}

// =============================================================================
// Prepare
// =============================================================================

// Prepare implements orchestration.JobLifecycleHooks: the attempt's branch
// is planned, the Delivery records it, and the issue is labelled in
// progress. NOTHING is materialised on the daemon's filesystem.
//
// The empty work directory is deliberate and is the whole point of the
// return: the worker runs in a container that clones the project repository
// and the project's context repositories itself at start, side by side, so
// the container decides its own workdir and the engine has no directory to
// spawn into here.
//
// The runner takes over from here: it starts the attempt's signing agent,
// materializes the workspace spec and spawns the container, and is the one
// thing that knows a path.
func (h *Hooks) Prepare(ctx context.Context, job *orchestration.JobContext) (string, error) {
	a, err := h.resolve(ctx, job)
	if err != nil {
		return "", err
	}
	if job.Attempt <= 0 {
		return "", &InvalidInputError{Field: "attempt", Message: "a bound job's attempt is numbered from one"}
	}
	issue, err := h.loadIssue(ctx, a)
	if err != nil {
		return "", err
	}
	plan, err := branchPlanFor(a, issue)
	if err != nil {
		return "", err
	}
	prior, err := h.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}
	// A later attempt continues the branch an earlier one pushed, so the
	// pull request and its reviews carry over.
	delivery := &Delivery{
		TaskID: job.TaskID, ProjectID: job.ProjectID, Attempt: job.Attempt,
		Branch: plan.Branch, Base: plan.Base, Target: plan.Target,
		RequiresPullRequest: plan.RequiresPullRequest, DeleteAfterMerge: plan.DeleteAfterMerge,
	}
	// The no-change turn carries over REGARDLESS of the branch, because it
	// is a fact about the task's last attempt rather than about the branch
	// that attempt worked on — and an attempt that changed nothing pushed no
	// head, so the branch-scoped carry-over below never sees it.
	if prior != nil {
		delivery.LastTurn = prior.LastTurn
	}
	if prior != nil && prior.Branch == plan.Branch && prior.HeadSHA != "" {
		delivery.HeadSHA = prior.HeadSHA
		delivery.PullRequest = prior.PullRequest
		delivery.Reviews = prior.Reviews
		delivery.Checks = prior.Checks
	}
	if err := h.deliveries.PutDelivery(ctx, job.TenantID, delivery); err != nil {
		return "", err
	}
	if err := h.moveLabelsOn(ctx, a, issue, LabelInProgress); err != nil {
		return "", err
	}
	return "", nil
}

// =============================================================================
// Reviewable
// =============================================================================

// Reviewable implements orchestration.JobLifecycleHooks: open or refresh the
// pull request against the head the container published, record it, publish
// it as a Task artifact and move the issue's label.
//
// IT DOES NO GIT. The commit, its signature and the push all happened inside
// the worker's container before this ran, and the head they produced is on
// the Delivery — so what is left here is exactly the half only the daemon can
// do, which is talking to the forge as the project's agent identity.
func (h *Hooks) Reviewable(ctx context.Context, job *orchestration.JobContext) error {
	a, err := h.resolve(ctx, job)
	if err != nil {
		return err
	}
	delivery, err := h.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
	if err != nil {
		return err
	}
	if delivery.HeadSHA == "" {
		return &NoPublishedHeadError{TaskID: job.TaskID}
	}
	if !delivery.RequiresPullRequest {
		return h.landDirect(ctx, job, a, delivery)
	}
	var pr *ForgePullRequest
	if delivery.PullRequest.Opened() {
		pr, err = a.forge.GetPullRequest(ctx, a.repo, delivery.PullRequest.Number)
	} else {
		pr, err = a.forge.CreatePullRequest(ctx, a.repo, PullRequestSpec{
			Title:   a.task.Subject,
			Body:    pullRequestBody(a, job),
			HeadRef: delivery.Branch,
			BaseRef: delivery.Target,
		})
	}
	if err != nil {
		return err
	}
	if pr == nil {
		return &ForgeWriteNotAppliedError{Type: a.forge.Type(), Operation: "CreatePullRequest", Subject: delivery.Subject(a.repo)}
	}
	if err := h.updateDelivery(ctx, job, delivery, func(d *Delivery) {
		d.PullRequest = DeliveryPullRequest{
			Number: pr.Number, URL: pr.URL, State: pullRequestStateFor(pr), Title: pr.Title, Base: pr.BaseRef,
		}
		if d.PullRequest.Base == "" {
			d.PullRequest.Base = delivery.Target
		}
	}); err != nil {
		return err
	}
	if err := h.publish(ctx, job, delivery); err != nil {
		return err
	}
	return h.moveLabels(ctx, a, LabelInReview)
}

// landDirect is the carve-out path: a change class the strategy exempted from
// review was pushed straight onto the target branch by the container, so
// there is nothing to open and nothing to review — only the record and the
// label.
//
// The work still happened on its own branch and is still one signed commit;
// what the carve-out removes is the pull request and its review round.
func (h *Hooks) landDirect(ctx context.Context, job *orchestration.JobContext, a *attempt, delivery *Delivery) error {
	if err := h.publish(ctx, job, delivery); err != nil {
		return err
	}
	return h.moveLabels(ctx, a, LabelMerged)
}

// deliveryCommitMessage is the one commit the attempt's changes land as. It
// is built on the daemon and run in the container: the message is the record
// of what the attempt was, and the issue it closes is the contract.
func deliveryCommitMessage(a *attempt, job *orchestration.JobContext) string {
	return strings.TrimSpace(a.task.Subject) + "\n\nCloses #" + strconv.Itoa(a.issue) +
		"\n\nTask: " + job.TaskID + " (attempt " + strconv.Itoa(job.Attempt) + ")"
}

// pullRequestBody closes the issue and names the task.
func pullRequestBody(a *attempt, job *orchestration.JobContext) string {
	return "Closes #" + strconv.Itoa(a.issue) + "\n\nOpened by the Software Factory for task `" + job.TaskID +
		"` (attempt " + strconv.Itoa(job.Attempt) + ").\n"
}

// publish records the pull request, the head and the review rounds as Task
// artifacts.
//
// THE ARTIFACTS ARE THE ONLY THING A GENERIC READER GETS. The Delivery is this
// module's record and nothing outside it may import the module; the engine's
// job page reads these three names and nothing else, which is why the review
// rounds are published here rather than left where only a review can see them
// (go-quicbaas#796). The names are the engine's constants, so a rename is a
// compile error rather than a page that quietly stops showing a pull request.
func (h *Hooks) publish(ctx context.Context, job *orchestration.JobContext, delivery *Delivery) error {
	var artifacts []orchestration.Artifact
	// A carve-out change has no pull request, and an artifact naming one
	// with an empty URL and number zero is worse than none: it reads as a
	// pull request the reader cannot open.
	// EVERY NAME IS SCOPED TO THE ATTEMPT. SubmitTaskArtifacts replaces by
	// name and a retried task keeps one record, so a flat name had attempt 3
	// overwrite attempt 1's delivery and attempt 1's job page then showed
	// attempt 3's pull request as its own.
	attempt := job.Attempt
	if delivery.PullRequest.Opened() {
		artifacts = append(artifacts, orchestration.Artifact{
			Kind: orchestration.ArtifactKindURL,
			Name: orchestration.ArtifactNameForAttempt(orchestration.ArtifactNamePullRequest, attempt),
			Ref:  orchestration.ArtifactRef{Path: delivery.PullRequest.URL},
			Metadata: map[string]string{
				"number":  strconv.Itoa(delivery.PullRequest.Number),
				"state":   delivery.PullRequest.State.String(),
				"branch":  delivery.Branch,
				"attempt": strconv.Itoa(attempt),
			},
		})
	}
	if delivery.HeadSHA != "" {
		artifacts = append(artifacts, orchestration.Artifact{
			Kind:     orchestration.ArtifactKindCommit,
			Name:     orchestration.ArtifactNameForAttempt(orchestration.ArtifactNameHead, attempt),
			Ref:      orchestration.ArtifactRef{SHA: delivery.HeadSHA, Branch: delivery.Branch},
			Metadata: map[string]string{"attempt": strconv.Itoa(attempt)},
		})
	}
	reviews, err := reviewRoundsArtifact(delivery, attempt)
	if err != nil {
		return err
	}
	if reviews != nil {
		artifacts = append(artifacts, *reviews)
	}
	return h.tasks.SubmitTaskArtifacts(ctx, job.TenantID, job.TaskID, artifacts)
}

// reviewRoundsArtifact is the delivery's recorded verdicts, as the engine's
// wire shape. It is nil when no reviewer has spoken yet — an empty rounds
// array would render as "reviewed, no findings", which is not the same thing.
func reviewRoundsArtifact(delivery *Delivery, attempt int) (*orchestration.Artifact, error) {
	if len(delivery.Reviews) == 0 {
		return nil, nil
	}
	rounds := make([]orchestration.JobReviewRound, 0, len(delivery.Reviews))
	for _, review := range delivery.Reviews {
		rounds = append(rounds, orchestration.JobReviewRound{
			Round:       review.Round,
			Reviewer:    review.Reviewer,
			Verdict:     review.Verdict.String(),
			HeadSHA:     review.HeadSHA,
			Findings:    review.Findings,
			SubmittedAt: review.SubmittedAt.UTC().Format(time.RFC3339),
		})
	}
	content, err := json.Marshal(rounds)
	if err != nil {
		return nil, &ReviewArtifactError{TaskID: delivery.TaskID, Cause: err}
	}
	return &orchestration.Artifact{
		Kind:     orchestration.ArtifactKindInline,
		Name:     orchestration.ArtifactNameForAttempt(orchestration.ArtifactNameReviews, attempt),
		MIMEType: "application/json",
		Size:     int64(len(content)),
		Content:  content,
		Metadata: map[string]string{"attempt": strconv.Itoa(attempt)},
	}, nil
}

// updateDelivery applies mutate under the delivery's version guard,
// re-reading and re-applying on a conflict, bounded.
func (h *Hooks) updateDelivery(ctx context.Context, job *orchestration.JobContext, delivery *Delivery, mutate func(*Delivery)) error {
	for attempt := 1; ; attempt++ {
		mutate(delivery)
		err := h.deliveries.UpdateDelivery(ctx, job.TenantID, delivery)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrVersionConflict) || attempt >= deliveryWriteAttempts {
			return err
		}
		fresh, readErr := h.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
		if readErr != nil {
			return readErr
		}
		*delivery = *fresh
	}
}

// =============================================================================
// Complete
// =============================================================================

// Complete implements orchestration.JobLifecycleHooks: merge the pull
// request, bound to the head the review approved.
func (h *Hooks) Complete(ctx context.Context, job *orchestration.JobContext) error {
	a, err := h.resolve(ctx, job)
	if err != nil {
		return err
	}
	delivery, err := h.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
	if err != nil {
		return err
	}
	if !delivery.RequiresPullRequest {
		// The carve-out already landed the change on the target branch and
		// labelled the issue; there is no pull request to merge.
		return nil
	}
	if !delivery.PullRequest.Opened() {
		return &InvalidInputError{Field: "delivery.pull_request", Message: "no pull request to merge for task " + job.TaskID}
	}
	subject := delivery.Subject(a.repo)
	pr, err := a.forge.GetPullRequest(ctx, a.repo, delivery.PullRequest.Number)
	if err != nil {
		return err
	}
	if pr == nil {
		return &NotFoundError{Entity: "pull request", TenantID: job.TenantID, ID: subject}
	}
	if !pr.Merged {
		if delivery.HeadSHA == "" {
			return &HeadMovedError{Subject: subject, Reviewed: "", Current: pr.HeadSHA}
		}
		if pr.HeadSHA != "" && pr.HeadSHA != delivery.HeadSHA {
			return &HeadMovedError{Subject: subject, Reviewed: delivery.HeadSHA, Current: pr.HeadSHA}
		}
		strategy, err := LookupStrategy(a.binding.Strategy)
		if err != nil {
			return err
		}
		spec, err := strategy.PlanMerge(PullRequestRef{
			Number: pr.Number, Title: pr.Title, IssueNumber: a.issue, SourceBranch: delivery.Branch,
		})
		if err != nil {
			return err
		}
		// Branch lifecycle is the BRANCH plan's decision, made once when the
		// branch was cut; the merge spec only carries it to the forge.
		spec.ExpectSourceDeleted = delivery.DeleteAfterMerge
		if err := h.merge(ctx, a.forge, a.repo, pr.Number, spec, delivery.HeadSHA); err != nil {
			return err
		}
	}
	if err := h.updateDelivery(ctx, job, delivery, func(d *Delivery) {
		d.PullRequest.State = PullRequestMerged
	}); err != nil {
		return err
	}
	if err := h.publish(ctx, job, delivery); err != nil {
		return err
	}
	return h.moveLabels(ctx, a, LabelMerged)
}

// merge issues the merge, retrying only while the forge reports itself
// not ready; every other refusal is final.
//
// NOT READY MEANS TWO THINGS ON GITEA, and only the pull request tells them
// apart. Gitea answers 405 "Please try again later" for every pull-request
// status except mergeable: still being checked, in conflict with its base, or
// errored while checking. Its API's `mergeable` is false for all three, and no
// field says which one applies. What separates them is TIME: a check settles
// in seconds and a conflict never does. So the bounded retry runs exactly as
// before, and once it is spent the pull request is read one more time.
// settledRefusal describes what that read decides.
func (h *Hooks) merge(ctx context.Context, forge ForgeBackend, repo RepoRef, number int, spec MergeSpec, approvedHead string) error {
	subject := repo.String() + "#" + strconv.Itoa(number)
	var lastNotReady error
	for attempt := 1; attempt <= h.attempts; attempt++ {
		err := forge.MergePullRequest(ctx, repo, number, spec, approvedHead)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrForgeNotReady) {
			return err
		}
		lastNotReady = err
		if attempt == h.attempts {
			break
		}
		if waitErr := h.waiter.Wait(ctx, attempt); waitErr != nil {
			return waitErr
		}
	}
	return h.settledRefusal(ctx, forge, repo, number, approvedHead,
		&MergeNotReadyExhaustedError{Subject: subject, Attempts: h.attempts, Last: lastNotReady})
}

// settledRefusal classifies a merge that the forge answered not-ready for the
// whole retry bound. It decides from one read of the pull request, made after
// the bound is spent.
//
// A PULL REQUEST THAT IS STILL OPEN, STILL ON THE APPROVED HEAD AND STILL NOT
// MERGEABLE CONFLICTS WITH ITS BASE. The forge has given that answer for the
// whole backoff, about a minute by default. A mergeability check takes
// seconds, so this is the forge's settled verdict and not a check still in
// progress. Measured on go-truststrap#64 (2026-09-22): the PR was
// codex-approved and green, main was five commits ahead with a real conflict,
// and the merge retried to exhaustion twice. Both times the attempt was charged
// as a failure of the work. Reported as a MergeConflictError, it becomes a
// merge round for the worker instead: the base is merged into the branch.
//
// Every other case keeps the answer it had:
//   - The forge reports the PR mergeable but still refused to merge it. The
//     forge really is not ready, so the error stays exhausted.
//   - The head moved. That is a HeadMovedError, the same refusal Complete makes
//     before it merges.
//   - The PR was merged in the meantime. That is the outcome that was asked
//     for, and Complete records it the same way it records a PR it found
//     already merged.
//   - The PR was closed. Merging the base cannot resolve that.
func (h *Hooks) settledRefusal(ctx context.Context, forge ForgeBackend, repo RepoRef, number int, approvedHead string,
	exhausted *MergeNotReadyExhaustedError) error {
	pr, err := forge.GetPullRequest(ctx, repo, number)
	if err != nil {
		return err
	}
	if pr == nil {
		return &NotFoundError{Entity: "pull request", ID: exhausted.Subject}
	}
	if pr.Merged {
		return nil
	}
	if pr.HeadSHA != approvedHead {
		return &HeadMovedError{Subject: exhausted.Subject, Reviewed: approvedHead, Current: pr.HeadSHA}
	}
	if pr.Mergeable || pr.State == "closed" {
		return exhausted
	}
	return &MergeConflictError{Subject: exhausted.Subject, Base: pr.BaseRef, Head: approvedHead, Attempts: exhausted.Attempts}
}

// =============================================================================
// Discard
// =============================================================================

// Discard implements orchestration.JobLifecycleHooks: nothing. The daemon
// materialised nothing to tear down, and the Delivery stays as the record of
// the attempt.
//
// The runner owns the teardown: it has the container push whatever it has
// not published yet, then closes the session and the container goes with it.
func (h *Hooks) Discard(_ context.Context, job *orchestration.JobContext, _ bool) error {
	if job == nil || job.TaskID == "" {
		return &InvalidInputError{Field: "job", Message: "the sdlc hooks serve jobs bound to a task, and this job is bound to none"}
	}
	return nil
}

// =============================================================================
// Blocked
// =============================================================================

// Blocked implements orchestration.JobLifecycleHooks: the park reason is
// posted to the issue, and the issue AND ITS PULL REQUEST are labelled for it.
func (h *Hooks) Blocked(ctx context.Context, job *orchestration.JobContext, reason orchestration.BlockReason) error {
	a, err := h.resolve(ctx, job)
	if err != nil {
		return err
	}
	delivery, err := h.deliveryOrNil(ctx, job)
	if err != nil {
		return err
	}
	body := renderBlocked(a.binding, a.task, job.Attempt, reason, delivery)
	if _, err := postWithRetry(ctx, a.forge, a.repo, a.issue, body, h.surfacing); err != nil {
		return err
	}
	return h.markParked(ctx, a, delivery, blockedLabelKey(reason.Kind))
}

// deliveryOrNil reads the job's delivery, answering nil for a job that has
// none rather than an error: a task parked before its branch was cut has no
// delivery, and that is not a failure to surface the park.
func (h *Hooks) deliveryOrNil(ctx context.Context, job *orchestration.JobContext) (*Delivery, error) {
	delivery, err := h.deliveries.GetDelivery(ctx, job.TenantID, job.TaskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return delivery, nil
}

// =============================================================================
// Labels
// =============================================================================

// markParked makes a park VISIBLE ON BOTH OBJECTS a person might be watching:
// the issue's display label moves to the park's, and the pull request is
// labelled with the same one.
//
// THE PULL REQUEST IS THE HALF THAT WAS MISSING, and review-round exhaustion
// is what exposed it. A change whose rounds are spent leaves an OPEN pull
// request behind — the branch is pushed on every non-completed exit, precisely
// so the work is recoverable — and until now nothing on that pull request said
// the engine had stopped. It read exactly like a change waiting for review,
// which is the most expensive thing an autonomous loop can leave lying around:
// the next reader either waits for a verdict nobody will ask for, or reviews
// and merges a change the engine already refused.
//
// ADD-ONLY ON THE PULL REQUEST, deliberately. The issue's labels are a display
// STATE the module owns and moves; a pull request never carried the trigger
// label and may carry labels a person put there, so there is nothing of the
// module's to remove and removing anything else would be destroying a human's
// annotation. AddIssueLabels is idempotent, which is what lets an interrupted
// escalation re-run this without doubling anything.
func (h *Hooks) markParked(ctx context.Context, a *attempt, delivery *Delivery, key string) error {
	if err := h.moveLabels(ctx, a, key); err != nil {
		return err
	}
	return h.labelPullRequest(ctx, a, delivery, key)
}

// labelPullRequest adds the label for key to the delivery's pull request.
//
// Nothing happens for a delivery with no pull request — a carve-out change, or
// a park before the branch was cut — or for a binding that names no label for
// key, which is the same silence moveLabels keeps for the same reason: a
// binding in the field that never configured a label must not start failing
// its parks.
func (h *Hooks) labelPullRequest(ctx context.Context, a *attempt, delivery *Delivery, key string) error {
	if delivery == nil || !delivery.PullRequest.Opened() {
		return nil
	}
	// A MERGED OR CLOSED PULL REQUEST IS LEFT ALONE. The label says "the
	// engine stopped and a person owns this now", and that is a statement
	// about an open change; stamping it on something already landed would
	// describe the wrong object.
	if delivery.PullRequest.State != PullRequestOpen {
		return nil
	}
	desired := a.binding.Forge.Label(key)
	if desired == "" {
		return nil
	}
	return a.forge.AddIssueLabels(ctx, a.repo, delivery.PullRequest.Number, []string{desired})
}

// moveLabels makes the issue display exactly one of the module's labels:
// every other label the module owns (the trigger included) is removed and
// the one for key is added when the binding names it. Labels the module does
// not own are never touched.
func (h *Hooks) moveLabels(ctx context.Context, a *attempt, key string) error {
	if len(a.binding.Forge.OwnedLabels()) == 0 {
		return nil
	}
	issue, err := h.loadIssue(ctx, a)
	if err != nil {
		return err
	}
	return h.moveLabelsOn(ctx, a, issue, key)
}

// moveLabelsOn is moveLabels on an issue the caller already read, so a hook
// that needed the issue for its own decision does not fetch it twice.
func (h *Hooks) moveLabelsOn(ctx context.Context, a *attempt, issue *ForgeIssue, key string) error {
	owned := a.binding.Forge.OwnedLabels()
	if len(owned) == 0 {
		return nil
	}
	desired := a.binding.Forge.Label(key)
	present := false
	stale := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		if label == desired {
			present = true
			continue
		}
		if owned[strings.ToLower(strings.TrimSpace(label))] {
			stale = append(stale, label)
		}
	}

	// THE ADD GOES FIRST, AND THE ORDER IS THE WHOLE SAFETY PROPERTY.
	//
	// A move is two or more forge calls and any of them can be the one that
	// fails. Removing first meant a remove that landed and an add that did not
	// left the issue carrying NO label this module owns — and an issue with no
	// lifecycle label is invisible to both directions of the reconciliation
	// sweep: it is not in the claiming listing, so no claim is examined, and a
	// terminal task is not a lost hand-over, so nothing restores it. The
	// display drift became permanent at exactly the moment the repair failed.
	//
	// Adding first cannot strand anything. If the add fails nothing has been
	// removed, so the issue still carries the label that made it findable. If
	// the add lands and a remove does not, the issue carries both — visibly
	// wrong, still claiming, and picked up and converged by the next sweep,
	// which is what labelsAgree is for. Over-labelled is recoverable;
	// unlabelled is not.
	if desired != "" && !present {
		if err := a.forge.AddIssueLabels(ctx, a.repo, a.issue, []string{desired}); err != nil {
			return err
		}
	}
	for _, label := range stale {
		if err := a.forge.RemoveIssueLabel(ctx, a.repo, a.issue, label); err != nil {
			return err
		}
	}
	return nil
}
