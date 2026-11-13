//go:build integration

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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// The Software Factory's forge and module wiring on a real node
// =============================================================================
//
// A fake forge registered under the module's forge factory, a scripted
// orchestration backend and a real node with the module started: a labelled
// issue becomes a Task, the Task becomes an sdlc Job, and the job drives the
// delivery to a merged pull request through the REAL store, the REAL git
// provisioner (KMS-held signing key and all) and the REAL review gate.
//
// The scripted backend plays ONE part: the WORKER, writing the change into
// the checkout it is prompted about. It does NOT clone — nothing outside the
// runner does. The clone is a sequence of git commands the runner execs into
// the container (pkg/sdlc/checkout.go), so what this test exercises is the
// real clone against a real repository, and the daemon's own filesystem is
// named by nothing.

const (
	loopE2EForge       sdlc.ForgeType = "gitea-e2e"
	loopE2ETenant                     = "sdlc-loop-e2e-tenant"
	loopE2EProject                    = "sdlc-loop-e2e-project"
	loopE2EBackend                    = "e2e-agent"
	loopE2EModel                      = "claude-opus-5" // the Anthropic family makes the codex review independent
	loopE2EWorkerAgent                = "e2e-coder"
	loopE2EKeyName                    = "agent-sdlc-loop-e2e"
	loopE2EKeyScope                   = "service"
	loopE2EKeyOwner                   = "sdlc-e2e"
	e2eOwner                          = "jeremyhahn"
	e2eRepo                           = "go-quicbaas"
	e2eWebhookRef                     = "sdlc-e2e-webhook-secret"
	e2eTokenRef                       = "sdlc-e2e-forge-token"
	e2eSecretValue                    = "a-webhook-secret-the-forge-also-holds"
)

var registerLoopForgeOnce sync.Once

// e2eForge is the scripted forge behind the module: issues, one pull request
// per branch, an approval and a green check the test grants.
type e2eForge struct {
	closedIssues []int
	mu           sync.Mutex
	repoDir      string // the bare repository the forge fronts; a pull request's head is what it holds
	issues       map[int]*sdlc.ForgeIssue
	comments     map[int][]sdlc.ForgeComment
	prs          map[int]*sdlc.ForgePullRequest
	approved     map[int]string // pr number -> head approved by "codex"
	refused      map[int]string // pr number -> head "codex" asked changes on
	green        map[string]bool
	nextPR       int
	labels       map[int][]string
	merged       map[int]bool
	statuses     []string

	// The repository-level onboarding surface: the label vocabulary and the
	// webhooks binding a project provisions. Modelled rather than stubbed so
	// the e2e sees the same idempotency a real forge gives.
	repoLabels []sdlc.ForgeLabel
	webhooks   []sdlc.ForgeWebhook

	// verdictOnOpen is the review a pull request carries the moment it is
	// opened: "APPROVED" grants it, "REQUEST_CHANGES" refuses it, and empty
	// leaves it unreviewed. It is how the test speaks as the reviewer and
	// the pipeline without racing the runner to the gate.
	verdictOnOpen string
}

var loopForge = newE2EForge()

// newE2EForge builds an empty scripted forge.
func newE2EForge() *e2eForge {
	return &e2eForge{
		issues: map[int]*sdlc.ForgeIssue{}, comments: map[int][]sdlc.ForgeComment{}, prs: map[int]*sdlc.ForgePullRequest{},
		approved: map[int]string{}, refused: map[int]string{}, green: map[string]bool{}, nextPR: 500,
		labels: map[int][]string{}, merged: map[int]bool{},
	}
}

// reset returns the forge to an empty state between scenarios; the factory is
// registered once per process, so the instance behind it is shared.
func (f *e2eForge) reset(repoDir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repoDir = repoDir
	f.verdictOnOpen = ""
	f.nextPR = 500
	f.statuses = nil
	f.issues = map[int]*sdlc.ForgeIssue{}
	f.comments = map[int][]sdlc.ForgeComment{}
	f.prs = map[int]*sdlc.ForgePullRequest{}
	f.approved = map[int]string{}
	f.refused = map[int]string{}
	f.green = map[string]bool{}
	f.labels = map[int][]string{}
	f.merged = map[int]bool{}
	f.repoLabels = nil
	f.webhooks = nil
}

func (f *e2eForge) Type() sdlc.ForgeType { return loopE2EForge }
func (f *e2eForge) Capabilities() sdlc.ForgeCapabilities {
	return sdlc.ForgeCapabilities{}
}

func (f *e2eForge) AuthenticatedUser(context.Context) (*sdlc.TokenIdentity, error) {
	return &sdlc.TokenIdentity{Username: "agent-e2e"}, nil
}

func (f *e2eForge) ListIssues(_ context.Context, _ sdlc.RepoRef, filter sdlc.IssueFilter) ([]sdlc.ForgeIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sdlc.ForgeIssue
	for _, issue := range f.issues {
		if filter.State != "" && issue.State != filter.State {
			continue
		}
		if len(filter.Labels) > 0 && !hasLabel(issue.Labels, filter.Labels[0]) {
			continue
		}
		out = append(out, *issue)
	}
	return out, nil
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func (f *e2eForge) GetIssue(_ context.Context, _ sdlc.RepoRef, number int) (*sdlc.ForgeIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	issue, ok := f.issues[number]
	if !ok {
		return nil, &sdlc.NotFoundError{Entity: "issue", ID: strconv.Itoa(number)}
	}
	copied := *issue
	copied.Labels = append([]string(nil), issue.Labels...)
	return &copied, nil
}

// CreateIssue files an issue in the fake, numbered after every issue it holds.
// The loop never files one; the method completes the forge contract.
func (f *e2eForge) CreateIssue(_ context.Context, _ sdlc.RepoRef, spec sdlc.IssueSpec) (*sdlc.ForgeIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	number := 1
	for existing := range f.issues {
		number = max(number, existing+1)
	}
	issue := &sdlc.ForgeIssue{Number: number, Title: spec.Title, Body: spec.Body, State: "open", Labels: append([]string(nil), spec.Labels...)}
	f.issues[number] = issue
	copied := *issue
	return &copied, nil
}

// ListCollaborators reports no collaborators: the loop's bindings name no
// rulers of their own.
func (f *e2eForge) ListCollaborators(context.Context, sdlc.RepoRef) ([]sdlc.ForgeUser, error) {
	return nil, nil
}

// CloseIssue records a close so the loop test can assert triage closed what it
// said it would.
func (f *e2eForge) CloseIssue(_ context.Context, _ sdlc.RepoRef, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closedIssues = append(f.closedIssues, number)
	return nil
}

func (f *e2eForge) CommentOnIssue(_ context.Context, _ sdlc.RepoRef, number int, body string) (*sdlc.ForgeComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := sdlc.ForgeComment{ID: int64(len(f.comments[number]) + 1), Body: body, Author: "quicbaas", Created: time.Now()}
	f.comments[number] = append(f.comments[number], c)
	return &c, nil
}

func (f *e2eForge) ListIssueComments(_ context.Context, _ sdlc.RepoRef, number int) ([]sdlc.ForgeComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sdlc.ForgeComment(nil), f.comments[number]...), nil
}

// CommentOnPullRequest shares the issue discussion: the scripted forge, like
// Gitea, keeps one discussion per number.
func (f *e2eForge) CommentOnPullRequest(ctx context.Context, repo sdlc.RepoRef, number int, body string) (*sdlc.ForgeComment, error) {
	return f.CommentOnIssue(ctx, repo, number, body)
}

// ListPullRequestComments reads the discussion CommentOnPullRequest writes.
func (f *e2eForge) ListPullRequestComments(ctx context.Context, repo sdlc.RepoRef, number int) ([]sdlc.ForgeComment, error) {
	return f.ListIssueComments(ctx, repo, number)
}

func (f *e2eForge) SetIssueLabels(_ context.Context, _ sdlc.RepoRef, number int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue, ok := f.issues[number]; ok {
		issue.Labels = append([]string(nil), labels...)
	}
	return nil
}

func (f *e2eForge) AddIssueLabels(_ context.Context, _ sdlc.RepoRef, number int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue, ok := f.issues[number]; ok {
		issue.Labels = append(issue.Labels, labels...)
	}
	return nil
}

// AddPullRequestLabels shares the issue labels, for the reason
// CommentOnPullRequest gives.
func (f *e2eForge) AddPullRequestLabels(ctx context.Context, repo sdlc.RepoRef, number int, labels []string) error {
	return f.AddIssueLabels(ctx, repo, number, labels)
}

// RemovePullRequestLabel removes what AddPullRequestLabels adds.
func (f *e2eForge) RemovePullRequestLabel(ctx context.Context, repo sdlc.RepoRef, number int, label string) error {
	return f.RemoveIssueLabel(ctx, repo, number, label)
}

func (f *e2eForge) RemoveIssueLabel(_ context.Context, _ sdlc.RepoRef, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue, ok := f.issues[number]; ok {
		kept := issue.Labels[:0]
		for _, l := range issue.Labels {
			if l != label {
				kept = append(kept, l)
			}
		}
		issue.Labels = kept
	}
	return nil
}

func (f *e2eForge) CreateBranch(_ context.Context, _ sdlc.RepoRef, name, _ string) (*sdlc.ForgeBranch, error) {
	return &sdlc.ForgeBranch{Name: name}, nil
}
func (f *e2eForge) GetBranch(_ context.Context, _ sdlc.RepoRef, name string) (*sdlc.ForgeBranch, error) {
	return &sdlc.ForgeBranch{Name: name}, nil
}
func (f *e2eForge) DeleteBranch(context.Context, sdlc.RepoRef, string) error { return nil }

// GetTag answers "not released" for every tag. The e2e loop's issues declare no
// release dependency, so nothing here reads it; a tag that EXISTED would be the
// wrong default, since it would satisfy a version pin nobody published.
func (f *e2eForge) GetTag(_ context.Context, repo sdlc.RepoRef, tag string) (*sdlc.ForgeTag, error) {
	return nil, &sdlc.NotFoundError{Entity: "tag", ID: repo.String() + "@" + tag}
}

func (f *e2eForge) CreatePullRequest(_ context.Context, _ sdlc.RepoRef, spec sdlc.PullRequestSpec) (*sdlc.ForgePullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr := &sdlc.ForgePullRequest{Number: f.nextPR, Title: spec.Title, Body: spec.Body, State: "open", HeadRef: spec.HeadRef, BaseRef: spec.BaseRef,
		URL: "http://forge.e2e/pulls/" + strconv.Itoa(f.nextPR), HeadSHA: f.headOf(spec.HeadRef)}
	f.prs[pr.Number] = pr
	f.nextPR++
	switch f.verdictOnOpen {
	case "APPROVED":
		f.approved[pr.Number] = pr.HeadSHA
		f.green[pr.HeadSHA] = true
	case "REQUEST_CHANGES":
		f.refused[pr.Number] = pr.HeadSHA
		f.green[pr.HeadSHA] = true
	}
	return pr, nil
}

// headOf answers what the remote holds for a branch, as the real forge does
// for a pull request's head after a push. Empty until a push landed.
func (f *e2eForge) headOf(branch string) string {
	if f.repoDir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", f.repoDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ListOpenPullRequests answers the head-to-pull-request lookup the review
// dispatcher makes when a pipeline delivery names only a commit. Heads are
// resolved from the real repository the same way GetPullRequest resolves them,
// so the loop's answer moves with the pushes it makes.
func (f *e2eForge) ListOpenPullRequests(_ context.Context, _ sdlc.RepoRef) ([]sdlc.ForgePullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	numbers := make([]int, 0, len(f.prs))
	for number := range f.prs {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	open := make([]sdlc.ForgePullRequest, 0, len(numbers))
	for _, number := range numbers {
		if f.merged[number] {
			continue
		}
		pr := f.prs[number]
		copied := *pr
		if head := f.headOf(pr.HeadRef); head != "" {
			copied.HeadSHA = head
			pr.HeadSHA = head
		}
		open = append(open, copied)
	}
	return open, nil
}

func (f *e2eForge) GetPullRequest(_ context.Context, _ sdlc.RepoRef, number int) (*sdlc.ForgePullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prs[number]
	if !ok {
		return nil, &sdlc.NotFoundError{Entity: "pull request", ID: strconv.Itoa(number)}
	}
	copied := *pr
	copied.Merged = f.merged[number]
	if head := f.headOf(pr.HeadRef); head != "" {
		copied.HeadSHA = head
		pr.HeadSHA = head
	}
	return &copied, nil
}

func (f *e2eForge) MergePullRequest(_ context.Context, _ sdlc.RepoRef, number int, _ sdlc.MergeSpec, expectedHead string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prs[number]
	if !ok {
		return &sdlc.NotFoundError{Entity: "pull request", ID: strconv.Itoa(number)}
	}
	if pr.HeadSHA != expectedHead {
		return &sdlc.HeadMovedError{Subject: strconv.Itoa(number), Reviewed: expectedHead, Current: pr.HeadSHA}
	}
	f.merged[number] = true
	pr.State = "closed"
	return nil
}

func (f *e2eForge) ListReviews(_ context.Context, _ sdlc.RepoRef, number int) ([]sdlc.ForgeReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sdlc.ForgeReview
	if head, ok := f.refused[number]; ok {
		out = append(out, sdlc.ForgeReview{ID: 1, Reviewer: "codex", State: "REQUEST_CHANGES",
			Body: "keep the change inside the task's scope", HeadSHA: head, Submitted: time.Now()})
	}
	if head, ok := f.approved[number]; ok {
		out = append(out, sdlc.ForgeReview{ID: 2, Reviewer: "codex", State: "APPROVED", Body: "lgtm", HeadSHA: head, Submitted: time.Now()})
	}
	return out, nil
}

func (f *e2eForge) ListCheckRuns(_ context.Context, _ sdlc.RepoRef, sha string) ([]sdlc.ForgeCheckRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.green[sha] {
		return nil, nil
	}
	return []sdlc.ForgeCheckRun{{ID: "1", Name: "ci", HeadSHA: sha, Status: "completed", Conclusion: "success", Completed: time.Now()}}, nil
}

func (f *e2eForge) CheckRunLogs(context.Context, sdlc.RepoRef, sdlc.ForgeCheckRun, int) (string, error) {
	return "", nil
}

// SubmitReview records a verdict as the review runner posts one, and stores
// it where ListReviews finds it so a second pass sees the first pass's
// answer exactly as it would on a real forge.
func (f *e2eForge) SubmitReview(_ context.Context, _ sdlc.RepoRef, number int, submission sdlc.ReviewSubmission) (*sdlc.ForgeReview, error) {
	if err := submission.Validate(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch submission.Verdict {
	case sdlc.VerdictApprove:
		f.approved[number] = submission.HeadSHA
	case sdlc.VerdictRequestChanges:
		f.refused[number] = submission.HeadSHA
	}
	return &sdlc.ForgeReview{
		ID: 3, Reviewer: "codex", State: string(submission.Verdict),
		Body: submission.Body, HeadSHA: submission.HeadSHA, Submitted: time.Now(),
	}, nil
}

// SetCommitStatus records a published check state. The scenarios drive the
// pipeline through green() rather than through this, so it only has to be
// truthful about what it was told.
func (f *e2eForge) SetCommitStatus(_ context.Context, _ sdlc.RepoRef, sha string, status sdlc.CommitStatus) error {
	if err := status.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, sha+" "+status.Context+"="+status.Status.String())
	return nil
}

func (f *e2eForge) ListPullRequestFiles(context.Context, sdlc.RepoRef, int) ([]sdlc.ForgeChangedFile, error) {
	return []sdlc.ForgeChangedFile{{Path: "pkg/widget/widget.go", Status: "modified", Additions: 4, Deletions: 1}}, nil
}

func (f *e2eForge) ClosePullRequest(_ context.Context, _ sdlc.RepoRef, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prs[number]
	if !ok {
		return &sdlc.NotFoundError{Entity: "pull request", ID: strconv.Itoa(number)}
	}
	pr.State = "closed"
	return nil
}

func (f *e2eForge) CancelCheckRun(context.Context, sdlc.RepoRef, string) error { return nil }
func (f *e2eForge) RerunCheckRun(context.Context, sdlc.RepoRef, sdlc.ForgeCheckRun) error {
	return nil
}
func (f *e2eForge) VerifyWebhook([]byte, string, []byte) error { return nil }
func (f *e2eForge) ParseWebhook(string, []byte) (*sdlc.ForgeEvent, error) {
	return &sdlc.ForgeEvent{Kind: sdlc.EventIssueOpened, Repo: sdlc.RepoRef{Owner: e2eOwner, Repo: e2eRepo}, IssueNumber: 1}, nil
}

// EnsureWebhook models the adapters' contract: create or bring into line,
// idempotently, so a repeat bind leaves ONE delivery.
func (f *e2eForge) EnsureWebhook(_ context.Context, _ sdlc.RepoRef, spec sdlc.WebhookSpec, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.webhooks {
		if f.webhooks[i].URL == spec.URL {
			f.webhooks[i].Active = true
			f.webhooks[i].Events = append([]string(nil), spec.Events...)
			return nil
		}
	}
	f.webhooks = append(f.webhooks, sdlc.ForgeWebhook{
		ID: strconv.Itoa(len(f.webhooks) + 1), URL: spec.URL, Active: true,
		Events: append([]string(nil), spec.Events...),
	})
	return nil
}

// =============================================================================
// The onboarding surface
// =============================================================================

func (f *e2eForge) Labels(context.Context, sdlc.RepoRef) ([]sdlc.ForgeLabel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sdlc.ForgeLabel(nil), f.repoLabels...), nil
}

// EnsureLabels creates what is missing and leaves every existing label
// exactly as it is — the contract every adapter implements.
func (f *e2eForge) EnsureLabels(_ context.Context, _ sdlc.RepoRef, specs []sdlc.LabelSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	have := make(map[string]bool, len(f.repoLabels))
	for _, held := range f.repoLabels {
		have[strings.ToLower(held.Name)] = true
	}
	for _, spec := range specs {
		normalized := spec.Normalize()
		if normalized.Name == "" || have[strings.ToLower(normalized.Name)] {
			continue
		}
		have[strings.ToLower(normalized.Name)] = true
		f.repoLabels = append(f.repoLabels, sdlc.ForgeLabel{
			Name: normalized.Name, Color: normalized.Color, Description: normalized.Description,
		})
	}
	return nil
}

func (f *e2eForge) Webhooks(context.Context, sdlc.RepoRef) ([]sdlc.ForgeWebhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sdlc.ForgeWebhook(nil), f.webhooks...), nil
}

func (f *e2eForge) DeleteWebhook(_ context.Context, _ sdlc.RepoRef, hookURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := make([]sdlc.ForgeWebhook, 0, len(f.webhooks))
	for _, hook := range f.webhooks {
		if hook.URL != hookURL {
			kept = append(kept, hook)
		}
	}
	f.webhooks = kept
	return nil
}

// RepoFiles answers an empty listing: the e2e repository carries no
// orchestration workflow, which is the desired state the conformance check
// exists to confirm.
func (f *e2eForge) RepoFiles(context.Context, sdlc.RepoRef, string, string) ([]sdlc.ForgeFile, error) {
	return []sdlc.ForgeFile{}, nil
}

// BranchProtection reports no rule. The capability flag is false for this
// forge, so the conformance pass records the check unsupported rather than
// reading this at all.
func (f *e2eForge) BranchProtection(context.Context, sdlc.RepoRef, string) (*sdlc.ForgeBranchProtection, error) {
	return nil, nil
}

// e2eIssueBody is what a person wrote on the issue. The runner must hand it
// to the worker VERBATIM, so the test asserts on this exact text.
const e2eIssueBody = "The widget drops every second event when the queue is full.\n\nFix it in pkg/widget and add a regression test."

func (f *e2eForge) putIssue(number int, title string, labels ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(labels) == 0 {
		labels = []string{"task:ready"}
	}
	f.issues[number] = &sdlc.ForgeIssue{Number: number, Title: title, Body: e2eIssueBody, State: "open",
		Labels: append([]string(nil), labels...), URL: "http://forge.e2e/issues/" + strconv.Itoa(number)}
}

// setVerdictOnOpen decides what the reviewer says to the next pull request.
func (f *e2eForge) setVerdictOnOpen(verdict string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verdictOnOpen = verdict
}

// approveEveryOpenPullRequest grants the approval and the green check on the
// CURRENT head of every open pull request, as a reviewer does after a round.
func (f *e2eForge) approveEveryOpenPullRequest() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for number, pr := range f.prs {
		head := f.headOf(pr.HeadRef)
		if head == "" {
			head = pr.HeadSHA
		}
		pr.HeadSHA = head
		delete(f.refused, number)
		f.approved[number] = head
		f.green[head] = true
	}
}

// approve grants the independent approval and the green check on the pull
// request's current head, as a reviewer and a pipeline would.
func (f *e2eForge) approve(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if pr, ok := f.prs[number]; ok {
		f.approved[number] = pr.HeadSHA
		f.green[pr.HeadSHA] = true
	}
}

func (f *e2eForge) pullRequests() []*sdlc.ForgePullRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*sdlc.ForgePullRequest, 0, len(f.prs))
	for _, pr := range f.prs {
		copied := *pr
		copied.Merged = f.merged[pr.Number]
		out = append(out, &copied)
	}
	return out
}

func (f *e2eForge) issueLabels(number int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue, ok := f.issues[number]; ok {
		return append([]string(nil), issue.Labels...)
	}
	return nil
}

func (f *e2eForge) commentsOn(number int) []sdlc.ForgeComment {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sdlc.ForgeComment(nil), f.comments[number]...)
}

func registerLoopForge(t *testing.T) {
	t.Helper()
	registerLoopForgeOnce.Do(func() {
		sdlc.RegisterForgeFactory(loopE2EForge, func(sdlc.ForgeConfig, sdlc.ForgeCredentials, sdlc.ForgeTransport) (sdlc.ForgeBackend, error) {
			return loopForge, nil
		})
	})
}

// e2eAgentBackend is the scripted worker behind a REAL orchestration session.
//
// On PROMPT it is the WORKER: it writes the change into the checkout the
// runner made in its container and records what it was told, so the test can
// assert the issue reached it verbatim. It clones NOTHING — the spawn path
// clears an image entrypoint, so a backend that cloned here would be modelling
// something no real image gets to do.
type e2eAgentBackend struct {
	mu sync.Mutex
	// sessions maps a session to the PROJECT CHECKOUT on the host — the
	// directory the runner's clone puts the project in, seen from the daemon
	// side of the bind mount, which is where the worker writes.
	sessions map[string]string
	// peakLive is the most sessions that were live at once: sessions grows
	// on Spawn and shrinks on Close, so this is the high-water mark of it.
	peakLive int
	seq      int
	prompts  []string
	spawns   []*orchestration.SpawnRequest
	// specs holds each spawn's workspace spec, read at spawn while the spec
	// directory still exists, in spawn order.
	specs []orchestration.WorkspaceSpec
	block chan struct{}

	// onPrompt runs after each prompt is recorded, so a scenario can act as
	// the reviewer between rounds.
	onPrompt func(prompt string)

	// runtime is the container runtime this backend's containers live in.
	// The backend registers each container's checkout and signing socket
	// with it at spawn, because those are what an exec into that container
	// resolves against.
	runtime *e2eContainerRuntime
}

func newE2EBackend() *e2eAgentBackend {
	return &e2eAgentBackend{sessions: map[string]string{}, runtime: newE2EContainerRuntime()}
}

// Spawn starts the session and its container. It makes NO checkout: the
// container comes up with an empty /workspace, exactly as a real one does
// once the spawn path has cleared the image's entrypoint.
func (b *e2eAgentBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	spec, err := readSpawnWorkspaceSpec(req)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.seq++
	id := "e2e-session-" + strconv.Itoa(b.seq)
	containerID := "e2e-container-" + strconv.Itoa(b.seq)
	b.sessions[id] = filepath.Join(req.WorkDir, spec.Project.Name)
	b.peakLive = max(b.peakLive, len(b.sessions))
	b.spawns = append(b.spawns, req)
	b.specs = append(b.specs, spec)
	runtime := b.runtime
	b.mu.Unlock()
	// The backend OWNS the container it put the agent in, exactly as the ACP
	// backend does, and reports its id — which is the only way anything else
	// can act inside it afterwards.
	if runtime != nil {
		if err := runtime.register(containerID, req); err != nil {
			return nil, err
		}
	}
	return &orchestration.Session{
		ID: id, Backend: loopE2EBackend, Model: req.Model, Status: orchestration.SessionStateIdle,
		WorkDir: req.WorkDir, ContainerID: containerID, CreatedAt: time.Now().Unix(),
	}, nil
}

// runGit runs one git command, reporting its output on failure.
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New("git " + strings.Join(args, " ") + ": " + err.Error() + ": " + string(out))
	}
	return nil
}

// Prompt is the worker's turn: it writes the change into the checkout.
func (b *e2eAgentBackend) Prompt(ctx context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	b.mu.Lock()
	b.prompts = append(b.prompts, req.Prompt)
	turn := len(b.prompts)
	workDir := b.sessions[req.SessionID]
	block := b.block
	onPrompt := b.onPrompt
	b.mu.Unlock()

	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if workDir == "" {
		return nil, errors.New("e2e backend: the worker session has no checkout")
	}
	// Each turn writes different content, so a later round changes the
	// tree the way a real worker's fix does.
	content := "done by the e2e worker, turn " + strconv.Itoa(turn) + "\n"
	if err := os.WriteFile(filepath.Join(workDir, "WORK.md"), []byte(content), 0o600); err != nil {
		return nil, err
	}
	if onPrompt != nil {
		onPrompt(req.Prompt)
	}
	return &orchestration.PromptResponse{SessionID: req.SessionID, Content: []orchestration.ContentBlock{{Type: "text", Text: "wrote WORK.md"}}}, nil
}

func (b *e2eAgentBackend) Status(_ context.Context, sessionID string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: sessionID, State: orchestration.SessionStateIdle}, nil
}

func (b *e2eAgentBackend) Close(_ context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
	return nil
}

func (b *e2eAgentBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent, PersistentSessions: true, ToolUse: true, FileAccess: true}
}
func (b *e2eAgentBackend) Type() string         { return "e2e" }
func (b *e2eAgentBackend) ToolGuidance() string { return "" }
func (b *e2eAgentBackend) ListModels(context.Context) ([]string, error) {
	return []string{loopE2EModel}, nil
}

// spawnCount is how many sessions the job asked for. For an sdlc job it is
// ONE, always: no lead, no per-work-item worker.
func (b *e2eAgentBackend) spawnCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.spawns)
}

// peakLiveSessions is the most sessions the backend held open at once over
// the job's life: a session closed before the next spawn counts once.
func (b *e2eAgentBackend) peakLiveSessions() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.peakLive
}

// spawnedWorkspaces is, per spawn in order, the work directory, project and
// branch the worker was put on.
func (b *e2eAgentBackend) spawnedWorkspaces() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	workspaces := make([]string, 0, len(b.spawns))
	for i, req := range b.spawns {
		spec := b.specs[i]
		workspaces = append(workspaces, req.WorkDir+" "+spec.Project.Name+" "+spec.Branch.Name)
	}
	return workspaces
}

func (b *e2eAgentBackend) promptTexts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.prompts...)
}

func (b *e2eAgentBackend) promptCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.prompts)
}

func (b *e2eAgentBackend) spawnRequests() []*orchestration.SpawnRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*orchestration.SpawnRequest(nil), b.spawns...)
}

// =============================================================================
// The worker's container
// =============================================================================
//
// e2eContainerRuntime is the second scripted part, beside the forge: the
// container the worker's session runs in. The runner execs git into it and
// nothing else, so that is what it implements — the command runs on the HOST
// side of the bind mount, with the container path it named translated back
// across that mount and SSH_AUTH_SOCK pointing at the socket the spawn
// mounted, which is exactly what a real `docker exec` resolves for it.
//
// THE TRANSLATION IS THE ASSERTION. Only paths under the container's checkout
// root translate; anything else is refused, so a runner that named a daemon
// path — the host side of the mount, an OS temporary directory, a worktree —
// fails here rather than quietly working because the two happen to be the
// same filesystem.
//
// Every other verb refuses. A runner that reached for one would be reaching
// past the seam this phase built.
type e2eContainerRuntime struct {
	mu         sync.Mutex
	containers map[string]*e2eContainer
	execs      []e2eExecCall
}

// e2eContainer is one running container: where its /workspace is on the host,
// what path the container itself calls that directory, and the agent socket
// its environment names.
type e2eContainer struct {
	// hostDir is the daemon-side directory bind-mounted at checkoutRoot. The
	// daemon created it EMPTY; everything in it was put there by a command
	// the runner ran inside the container.
	hostDir string

	// checkoutRoot is what the container calls hostDir.
	checkoutRoot string

	// mounts are the container's other mounts, keyed by container path: the
	// spec directory the daemon wrote (allowed_signers lives there) and a
	// stand-in for the credential tier the runner delivers the forge token to.
	mounts []orchestration.ContainerMount

	authSock string
}

// hostPath translates a path the container named into the host path behind
// the bind mount, refusing anything the mount does not cover.
func (c *e2eContainer) hostPath(containerDir string) (string, error) {
	if containerDir == c.checkoutRoot {
		return c.hostDir, nil
	}
	rel, err := filepath.Rel(c.checkoutRoot, containerDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("the command named " + containerDir +
			", which is outside this container's checkout root " + c.checkoutRoot)
	}
	return filepath.Join(c.hostDir, rel), nil
}

// hostArg translates an argument naming a path under one of the container's
// mounts, alone or as the value of a key=value git config, into the host path
// behind it; any other argument is returned as is.
func (c *e2eContainer) hostArg(arg string) string {
	key, value, isConfig := strings.Cut(arg, "=")
	if !isConfig {
		key, value = "", arg
	}
	for _, mount := range c.mounts {
		rest, ok := strings.CutPrefix(value, mount.Target+"/")
		if !ok {
			continue
		}
		host := filepath.Join(mount.Source, rest)
		if isConfig {
			return key + "=" + host
		}
		return host
	}
	return arg
}

// e2eExecCall is one command the runner ran in a container.
type e2eExecCall struct {
	ContainerID string
	WorkDir     string
	Cmd         []string
	ExitCode    int
}

// forgeCredentialStep reports whether call is the runner delivering the forge
// token to the container's credential tier, or revoking it from there.
func (call e2eExecCall) forgeCredentialStep() bool {
	return call.Cmd[len(call.Cmd)-1] == orchestration.WorkspaceForgeCredentialFile
}

func newE2EContainerRuntime() *e2eContainerRuntime {
	return &e2eContainerRuntime{containers: map[string]*e2eContainer{}}
}

// register records what a spawn put in the container: the checkout the
// entrypoint made, the path the container knows it by, and the signing
// socket the daemon mounted.
func (r *e2eContainerRuntime) register(containerID string, req *orchestration.SpawnRequest) error {
	spec, err := readSpawnWorkspaceSpec(req)
	if err != nil {
		return err
	}
	credentialDir := req.WorkDir + ".credentials"
	if err := os.MkdirAll(credentialDir, 0o700); err != nil {
		return err
	}
	container := &e2eContainer{
		hostDir:      req.WorkDir,
		checkoutRoot: spec.CheckoutRoot,
		mounts:       []orchestration.ContainerMount{{Source: credentialDir, Target: orchestration.WorkspaceCredentialDir}},
	}
	for _, mount := range req.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSigningAgentSocket {
			container.authSock = mount.Source
		}
		container.mounts = append(container.mounts, mount)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.containers[containerID] = container
	return nil
}

// Exec runs one command in the container's checkout.
func (r *e2eContainerRuntime) Exec(ctx context.Context, containerID string, spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	r.mu.Lock()
	container, ok := r.containers[containerID]
	r.mu.Unlock()
	if !ok {
		return nil, &orchestration.ContainerNotFoundError{ContainerID: containerID}
	}
	// The runner names the CONTAINER's path; the process runs on the host
	// side of the bind mount. Refusing anything the mount does not cover is
	// what proves the runner never passed a daemon path.
	workDir, translateErr := container.hostPath(spec.WorkDir)
	if translateErr != nil {
		return nil, &orchestration.ContainerExecError{ContainerID: containerID, Cause: translateErr}
	}
	args := make([]string, 0, len(spec.Cmd)-1)
	for _, arg := range spec.Cmd[1:] {
		args = append(args, container.hostArg(arg))
	}
	cmd := exec.CommandContext(ctx, spec.Cmd[0], args...)
	cmd.Dir = workDir
	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK="+container.authSock)
	for key, value := range spec.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	result := &orchestration.ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return nil, &orchestration.ContainerExecError{ContainerID: containerID, Cause: runErr}
		}
		result.ExitCode = exitErr.ExitCode()
	}
	r.mu.Lock()
	r.execs = append(r.execs, e2eExecCall{
		ContainerID: containerID, WorkDir: spec.WorkDir,
		Cmd: append([]string(nil), spec.Cmd...), ExitCode: result.ExitCode,
	})
	r.mu.Unlock()
	return result, nil
}

// commandsRun renders every command the runner ran in a container.
func (r *e2eContainerRuntime) commandsRun() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.execs))
	for _, e := range r.execs {
		out = append(out, strings.Join(e.Cmd, " "))
	}
	return out
}

// Everything below is the rest of the ContainerRuntime seam. The runner uses
// exactly one verb, so the rest refuse rather than pretending.
func (r *e2eContainerRuntime) Run(context.Context, *orchestration.ContainerSpec) (string, error) {
	return "", errNoContainerLifecycleInThisTest
}

func (r *e2eContainerRuntime) RunInteractive(context.Context, *orchestration.ContainerSpec) (*orchestration.InteractiveProcess, error) {
	return nil, errNoContainerLifecycleInThisTest
}
func (r *e2eContainerRuntime) Stop(context.Context, string) error   { return nil }
func (r *e2eContainerRuntime) Remove(context.Context, string) error { return nil }
func (r *e2eContainerRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return nil, errNoContainerLifecycleInThisTest
}
func (r *e2eContainerRuntime) IsRunning(context.Context, string) (bool, error) { return true, nil }
func (r *e2eContainerRuntime) IsAvailable() bool                               { return true }
func (r *e2eContainerRuntime) Pull(context.Context, string) error              { return nil }

// errNoContainerLifecycleInThisTest is what the verbs the runner does not use
// answer. The backend owns this container's lifecycle, as an ACP backend owns
// a real one.
var errNoContainerLifecycleInThisTest = errors.New("the backend owns this container's lifecycle")

var _ orchestration.ContainerRuntime = (*e2eContainerRuntime)(nil)

// readSpawnWorkspaceSpec reads the workspace spec the daemon mounted for a
// spawn — the same file the container can read at $QUICBAAS_WORKSPACE_SPEC.
func readSpawnWorkspaceSpec(req *orchestration.SpawnRequest) (orchestration.WorkspaceSpec, error) {
	var spec orchestration.WorkspaceSpec
	specDir := ""
	for _, mount := range req.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSpecDir {
			specDir = mount.Source
		}
	}
	if specDir == "" {
		return spec, errors.New("e2e container: the daemon mounted no workspace spec")
	}
	raw, err := os.ReadFile(filepath.Join(specDir, orchestration.WorkspaceSpecFile))
	if err != nil {
		return spec, err
	}
	return spec, json.Unmarshal(raw, &spec)
}

// startFactoryNode boots a node with the engine and the module on fast
// cadences over dataDir. reapTTL is orchestration.reap_lease_ttl; nodeID
// names the node on its leases.
func startFactoryNode(t *testing.T, dataDir string, init bool, reapTTL time.Duration, nodeID uint64) *server.Server {
	t.Helper()
	requireTestTrustDomain(t)
	f := &inlineInitFlags{
		barrierStrategy: "passphrase", secret: "test-barrier-secret-12345",
		soPin: "so-pin-1234", userPin: "user-pin-1234", credentialStrategy: "barrier", dataDir: dataDir,
	}
	if init {
		require.NoError(t, runInlineInit(&cobra.Command{Use: "init"}, f))
	}
	cfg := buildInitConfig(dataDir, f, true)
	cfg.Raft.NodeID = nodeID
	cfg.Orchestration.Enabled = true
	cfg.Orchestration.SweepInterval = 150 * time.Millisecond
	cfg.Orchestration.ReapLeaseTTL = reapTTL
	cfg.Orchestration.VerificationCommands = []string{"test -f WORK.md"}
	// The worker's checkout and the workspace spec beside it are bind-mount
	// sources, and the mount policy is deny-by-default. The operator admits
	// the root this test's project workspaces live under, exactly as a real
	// operator admits the projects root.
	cfg.Orchestration.Sandbox.AllowedMountRoots = []string{factoryWorkspaceRoot(t)}
	// The signing agent's socket is NOT under the workspace: a unix socket
	// address is a fixed-size kernel field and a managed workspace does not
	// fit in it (go-quicbaas#628). The operator names a short, dedicated root
	// instead, which the mount policy admits by construction — no allowlist
	// entry for it here, on purpose.
	cfg.Orchestration.SocketsRoot = factorySocketsRoot(t)
	cfg.SDLC.Enabled = true
	// The node's public origin: binding a project provisions a forge webhook
	// pointing at it, and the module refuses to start without one rather
	// than guess an address no forge could reach.
	cfg.SDLC.Webhook.PublicURL = "https://factory.e2e"
	cfg.SDLC.AllowLocalRepos = true
	cfg.SDLC.Surfacing.RetryInterval = 10 * time.Millisecond

	srv, err := server.NewServer(cfg, server.WithSealPassphrase([]byte(f.secret)))
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	require.NotNil(t, srv.SDLCService(), "the sdlc module did not start")
	return srv
}

// =============================================================================
// Where the worker's checkout lives
// =============================================================================
//
// The daemon makes no clone, but it does make the DIRECTORY the container's
// checkout is bind-mounted through, and the mount policy is deny-by-default.
// So the test owns one root for this process, admits it in the operator
// configuration, and gives each project its own directory under it — which is
// what a real deployment does with its projects root.

var (
	factoryRootOnce sync.Once
	factoryRoot     string
	factoryRootErr  error
	factoryProjects atomic.Int64

	factorySocketsOnce sync.Once
	factorySockets     string
	factorySocketsErr  error
)

// factoryWorkspaceRoot is the process-wide root the operator admits. It
// outlives any one test because a project's recorded workspace path must
// survive the node restart the resume scenario performs.
func factoryWorkspaceRoot(t *testing.T) string {
	t.Helper()
	factoryRootOnce.Do(func() {
		factoryRoot, factoryRootErr = os.MkdirTemp("", "sdlc-factory-workspaces-*")
	})
	require.NoError(t, factoryRootErr)
	return factoryRoot
}

// factorySocketsRoot is the process-wide root the daemon binds its
// per-session signing sockets under.
//
// It is short deliberately and it is NOT a temp directory named after the
// test: t.TempDir() embeds the test's name, and these names are long enough
// on their own to spend the socket path's whole 107-byte budget.
func factorySocketsRoot(t *testing.T) string {
	t.Helper()
	factorySocketsOnce.Do(func() {
		factorySockets, factorySocketsErr = os.MkdirTemp("", "qbs")
	})
	require.NoError(t, factorySocketsErr)
	return factorySockets
}

// TestMain removes the process-wide roots once the last test that could name
// them has returned. Neither root can be a t.TempDir(): the workspace root
// must outlive the test that records a project's workspace path so the
// killed-daemon resume scenario finds it again, and the sockets root must stay
// short enough for a unix socket path. That process-wide lifetime is the
// design; the missing teardown was not, and it left one directory pair per
// suite run behind in the system temp directory (go-quicbaas#688).
func TestMain(m *testing.M) {
	code := m.Run()
	for _, err := range removeFactoryRoots(factoryRoot, factorySockets) {
		fmt.Fprintf(os.Stderr, "sdlc e2e: %v\n", err)
	}
	os.Exit(code)
}

// removeFactoryRoots deletes each root named, skipping the empty name a root
// no test asked for carries, and returns one error per root it could not
// remove. It reports rather than fails because m.Run has already returned by
// the time TestMain calls it: no *testing.T is left to fail.
func removeFactoryRoots(roots ...string) []error {
	var errs []error
	for _, root := range roots {
		if root == "" {
			continue
		}
		if err := os.RemoveAll(root); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// TestRemoveFactoryRoots_DeletesEachPopulatedRootAndSkipsTheUnusedOne is the
// teardown TestMain runs: a root full of a worker's checkout goes, and the
// empty name a root no test asked for carries is skipped rather than treated
// as the current directory.
func TestRemoveFactoryRoots_DeletesEachPopulatedRootAndSkipsTheUnusedOne(t *testing.T) {
	workspaces := filepath.Join(t.TempDir(), "sdlc-factory-workspaces-test")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaces, "project-1", "checkout"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workspaces, "project-1", "checkout", "README"), []byte("x"), 0o600))
	sockets := filepath.Join(t.TempDir(), "qbs-test")
	require.NoError(t, os.MkdirAll(sockets, 0o700))

	require.Empty(t, removeFactoryRoots(workspaces, "", sockets))

	require.NoDirExists(t, workspaces)
	require.NoDirExists(t, sockets)
}

// TestRemoveFactoryRoots_ReportsARootItCannotRemove covers the branch whose
// failure TestMain prints: a name that is not a directory it can unlink. The
// error is returned rather than swallowed, and the roots after it are still
// removed — one stuck root must not strand the rest.
func TestRemoveFactoryRoots_ReportsARootItCannotRemove(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "regular-file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))
	removable := filepath.Join(dir, "removable")
	require.NoError(t, os.MkdirAll(removable, 0o700))

	errs := removeFactoryRoots(filepath.Join(notADir, "child"), removable)

	require.Len(t, errs, 1)
	require.ErrorContains(t, errs[0], "child")
	require.NoDirExists(t, removable)
}

// factoryProjectWorkspace is one project's workspace under that root.
func factoryProjectWorkspace(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(factoryWorkspaceRoot(t), "project-"+strconv.FormatInt(factoryProjects.Add(1), 10))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	return dir
}

// bareFactoryRepo seeds the project's repository: one commit on main.
func bareFactoryRepo(t *testing.T) string {
	t.Helper()
	seed := t.TempDir()
	git := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", seed}, args...)...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("init", "--initial-branch=main")
	git("config", "user.name", "seed")
	git("config", "user.email", "seed@example.com")
	git("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(seed, "README"), []byte("seed\n"), 0o600))
	git("add", "README")
	git("commit", "-m", "seed")
	bare := filepath.Join(t.TempDir(), "remote.git")
	git("clone", "--bare", seed, bare)
	return bare
}

// ensureFactoryTenant provisions the e2e tenant in IAM. The task-source
// poller sweeps the tenants IAM enumerates, so a project whose tenant IAM
// has never heard of is swept for nothing, exactly as in production.
func ensureFactoryTenant(t *testing.T, srv *server.Server) {
	t.Helper()
	provider, ok := srv.Service().(interface {
		GetTenantManager(ctx context.Context) (iam.TenantManager, error)
	})
	require.True(t, ok, "the service does not expose the IAM tenant manager")
	tenants, err := provider.GetTenantManager(context.Background())
	require.NoError(t, err)
	if _, err := tenants.GetTenant(context.Background(), loopE2ETenant); err == nil {
		return
	}
	_, err = tenants.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: loopE2ETenant, Name: "Software Factory e2e"})
	require.NoError(t, err)
}

// registerFactoryProject stores the secrets, mints the signing key in the
// node's KMS-backed SSH store, registers the scripted backend and a fleet
// agent for it, creates the orchestration Project with the sdlc workflow
// as its default, and binds the forge. It returns the project id.
// defineFactoryWorker defines, in the factory tenant's agent catalog, the
// coding Agent the project binding names as its worker, and returns the
// reference to it. The Agent runs on the scripted backend and answers for a
// model of a known family, so the review policy's independence rule has a
// family to compare an approval against. A node reused across scenarios
// already has it.
func defineFactoryWorker(t *testing.T, ctx context.Context, agents orchestration.AgentService) orchestration.AgentRef {
	t.Helper()
	accessor, ok := agents.(orchestration.AgentDirectoryAccessor)
	require.True(t, ok, "the agent service does not expose its agent catalog")
	directory := accessor.AgentDirectory()
	require.NotNil(t, directory, "the agent catalog is not wired")
	ref := orchestration.AgentRefTo(loopE2EWorkerAgent)
	if _, err := directory.GetAgent(ctx, loopE2ETenant, loopE2EWorkerAgent); err == nil {
		return ref
	}
	_, err := directory.CreateAgent(ctx, loopE2ETenant, &orchestration.Agent{
		ID: loopE2EWorkerAgent, Name: "Factory e2e coder",
		Roles:   []orchestration.AgentRole{orchestration.AgentRoleCoding},
		Backend: loopE2EBackend, Model: loopE2EModel,
	})
	require.NoError(t, err, "defining the worker Agent")
	return ref
}

func registerFactoryProject(t *testing.T, srv *server.Server, backend *e2eAgentBackend, repo string) string {
	t.Helper()
	ctx := tenantctx.WithTenant(context.Background(), loopE2ETenant)
	orchCtx := orchestration.ContextWithTenant(ctx, loopE2ETenant)
	ensureFactoryTenant(t, srv)
	secrets := srv.Service().Secrets()
	require.NotNil(t, secrets)
	for ref, value := range map[string]string{e2eWebhookRef: e2eSecretValue, e2eTokenRef: "a-forge-api-token"} {
		_, err := secrets.StoreSecret(ctx, &service.StoreSecretRequest{SecretID: ref, Data: []byte(value)})
		require.NoError(t, err)
	}

	sshClient := srv.Service().SSHClient()
	require.NotNil(t, sshClient, "the node has no SSH key store to mint the signing key in")
	if _, err := sshClient.Keys().Get(ctx, xkmsssh.GetKeyRequest{Scope: loopE2EKeyScope, Owner: loopE2EKeyOwner, Name: loopE2EKeyName}); err != nil {
		_, err := sshClient.Keys().Generate(ctx, xkmsssh.GenerateRequest{
			Scope: loopE2EKeyScope, Owner: loopE2EKeyOwner, Name: loopE2EKeyName, Algorithm: "ed25519", Comment: "sdlc factory e2e",
		})
		require.NoError(t, err, "minting the signing key")
	}

	agents := srv.Service().Agent()
	require.NotNil(t, agents)
	require.NoError(t, agents.RegisterBackend(orchCtx, &orchestration.RegisterBackendRequest{Name: loopE2EBackend, Backend: backend}))
	workerRef := defineFactoryWorker(t, orchCtx, agents)
	// The node's own runtime is whatever docker it found; this test's worker
	// runs in the scripted container the backend owns, so the engine is
	// pointed at that one. Without it the runner has nowhere to commit — which
	// is the refusal, not a fallback.
	runtimeSetter, ok := agents.(interface {
		SetContainerRuntime(orchestration.ContainerRuntime)
	})
	require.True(t, ok, "the agent service does not accept a container runtime")
	runtimeSetter.SetContainerRuntime(backend.runtime)
	// NO FLEET AGENT IS REGISTERED. A module runner spawns its own sessions
	// and its job lease is the capacity check, so auto-spawn needs neither a
	// matched fleet node nor a default workflow. A dispatch that refused
	// here would say so on the Task.

	projects := srv.Service().Project()
	require.NotNil(t, projects)
	var projectID string
	if existing, getErr := projects.GetProjectByName(ctx, &service.GetProjectByNameRequest{TenantID: loopE2ETenant, Name: loopE2EProject}); getErr == nil && existing != nil && existing.Project != nil {
		projectID = existing.Project.ID
	} else {
		created, createErr := projects.CreateProject(ctx, &service.CreateProjectRequest{
			TenantID: loopE2ETenant, Name: loopE2EProject,
			WorkspacePath: factoryProjectWorkspace(t),
			GitRepoURL:    "file://" + repo, GitDefaultBranch: "main",
			GitAuthorName: "agent-" + loopE2EProject, GitAuthorEmail: "agent-" + loopE2EProject + "@example.com",
			GitSigningEnabled: true, GitSigningKeyName: loopE2EKeyName, GitSigningKeyScope: loopE2EKeyScope, GitSigningKeyOwner: loopE2EKeyOwner,
		})
		require.NoError(t, createErr)
		projectID = created.Project.ID
	}

	if _, err := srv.SDLCService().Binding(ctx, loopE2ETenant, projectID); err == nil {
		return projectID
	}
	_, bindErr := srv.SDLCService().Bind(ctx, loopE2ETenant, &sdlc.ProjectBinding{
		ProjectID: projectID,
		Forge: sdlc.ForgeConfig{
			Type: loopE2EForge, BaseURL: "http://forge.e2e", Owner: e2eOwner, Repo: e2eRepo,
			TriggerLabel:     "task:ready",
			Labels:           map[string]string{sdlc.LabelInProgress: "factory:in-progress", sdlc.LabelInReview: "factory:in-review", sdlc.LabelMerged: "factory:merged", sdlc.LabelEscalated: "factory:escalated"},
			WebhookSecretRef: e2eWebhookRef, TokenSecretRef: e2eTokenRef,
		},
		Strategy: sdlc.GitHubFlowName,
		// The worker Agent's MODEL is what the independence rule is decided
		// against: an approval from the family that wrote the code is not an
		// independent review. See defineFactoryWorker.
		Worker:       workerRef,
		Verification: []string{"test -f WORK.md"},
		Review: sdlc.ReviewPolicy{RequiredApprovals: 1, RequireIndependentReviewer: true, RequireGreenChecks: true,
			Reviewers: map[string]string{"codex": sdlc.ModelFamilyOpenAI}},
		MaxReviewRounds: 3,
	}, service.BindOptions{})
	require.NoError(t, bindErr, "binding the project")
	return projectID
}

// =============================================================================
// The sdlc runner, end to end
// =============================================================================

// factoryRig is one scenario's node: the module started, the forge scripted,
// the project bound.
type factoryRig struct {
	srv       *server.Server
	backend   *e2eAgentBackend
	runtime   *e2eContainerRuntime
	projectID string
	dataDir   string
	repo      string
}

// startFactory boots a node with the module and a bound project, on a forge
// holding one triggered issue.
func startFactory(t *testing.T, issue int, labels ...string) *factoryRig {
	t.Helper()
	registerLoopForge(t)
	repo := bareFactoryRepo(t)
	loopForge.reset(repo)
	loopForge.putIssue(issue, "Fix the widget", labels...)
	dataDir := t.TempDir()
	srv := startFactoryNode(t, dataDir, true, time.Hour, 1)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	backend := newE2EBackend()
	projectID := registerFactoryProject(t, srv, backend, repo)
	return &factoryRig{srv: srv, backend: backend, runtime: backend.runtime, projectID: projectID, dataDir: dataDir, repo: repo}
}

// orchCtx is the tenant-scoped context the engine's services are called with.
func factoryCtx() context.Context {
	return orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), loopE2ETenant), loopE2ETenant)
}

// awaitJob waits for the delivery job, the one the sdlc runner drives, to
// reach a terminal state and returns it.
//
// THE RUNNER IS PART OF THE QUESTION. A node that starts with a project
// already bound runs its backstop boot pass, and that pass starts a triage job
// which finds nothing to judge and completes within milliseconds. A wait for
// any job in the wanted state returns on that one while the delivery is still
// initializing, and the caller then reads a forge the delivery has not
// touched yet.
func awaitJob(t *testing.T, srv *server.Server, want orchestration.JobState) *orchestration.JobStatus {
	t.Helper()
	var found *orchestration.JobStatus
	require.Eventually(t, func() bool {
		jobs, err := srv.Service().Agent().ListJobs(factoryCtx(), loopE2ETenant, nil)
		if err != nil || len(jobs) == 0 {
			return false
		}
		for _, job := range jobs {
			if job.State == want && job.Runner == sdlc.JobRunnerName {
				found = job
				return true
			}
		}
		return false
	}, 90*time.Second, 50*time.Millisecond, "no %s job reached %s", sdlc.JobRunnerName, want)
	return found
}

// TestSDLCRunner_IssueToMergedPullRequestThroughOneWorker is the whole point
// of the runner.
//
// A labelled issue becomes a Task, the Task becomes an sdlc Job, and the job
// drives one worker to a merged pull request. The three assertions that
// distinguish this runner from the lead-driven loop it replaced:
//
//   - EXACTLY ONE session. No lead, so nothing decomposes and nothing is
//     assigned to a fleet — no fleet node is even registered.
//   - The worker's prompt contains the ISSUE BODY VERBATIM. The loop this
//     replaced handed the worker an LLM's paraphrase framed "Execute the
//     following task", and the worker never saw what a person wrote.
//   - The job's declared states never include decomposing.
func TestSDLCRunner_IssueToMergedPullRequestThroughOneWorker(t *testing.T) {
	rig := startFactory(t, 71)
	loopForge.setVerdictOnOpen("APPROVED")

	job := awaitJob(t, rig.srv, orchestration.JobStateCompleted)
	require.Equal(t, sdlc.JobRunnerName, job.Runner, "the job was not driven by the sdlc runner")

	require.Equal(t, 1, rig.backend.spawnCount(), "an sdlc job spawns exactly one worker session")
	require.Equal(t, 1, rig.backend.promptCount(), "a first-round delivery is one turn")
	prompts := rig.backend.promptTexts()
	require.Contains(t, prompts[0], e2eIssueBody, "the worker was not handed the issue body verbatim")
	require.Contains(t, prompts[0], "Issue: #71")
	require.NotContains(t, prompts[0], "Execute the following task",
		"the worker was handed a decomposition framing; nothing decomposes an issue")
	require.NotContains(t, prompts[0], "work_items", "the lead's decomposition contract reached the worker")

	spawn := rig.backend.spawnRequests()[0]
	require.True(t, spawn.Persistent, "the worker session must carry its context across review rounds")
	require.Contains(t, spawn.SystemPrompt, "SDLC worker charter")
	require.NotEmpty(t, spawn.Env[orchestration.WorkspaceSpecEnv], "the container was told nothing to clone")

	prs := loopForge.pullRequests()
	require.Len(t, prs, 1, "expected exactly one pull request")
	require.True(t, prs[0].Merged, "the pull request was not merged")

	delivery, err := rig.srv.SDLCService().Delivery(factoryCtx(), loopE2ETenant, taskOfJob(t, rig.srv, job))
	require.NoError(t, err)
	require.Equal(t, "issue/71-fix-the-widget", delivery.Branch)
	require.NotEmpty(t, delivery.HeadSHA, "no head was recorded for the reviewed change")
	require.Contains(t, loopForge.issueLabels(71), "factory:merged")
	t.Logf("OBSERVED: sessions=%d prompts=%d branch=%s head=%s merged=%v",
		rig.backend.spawnCount(), rig.backend.promptCount(), delivery.Branch, delivery.HeadSHA, prs[0].Merged)
}

// taskOfJob returns the task the job is an attempt at.
func taskOfJob(t *testing.T, srv *server.Server, job *orchestration.JobStatus) string {
	t.Helper()
	require.NotNil(t, job.Config, "the job carries no config")
	require.NotEmpty(t, job.Config.TaskID, "the job is not an attempt at a task")
	return job.Config.TaskID
}

// TestSDLCRunner_ChangesRequestedRunsAnotherRoundOnTheSameBranch: the review
// sends the work back and the second round lands on the same branch and
// workspace, through the same pull request. The runner closes the worker on
// entry to reviewing (go-quicbaas#938), so the round spawns a fresh one: two
// spawns over the job's life, but never more than WorkerSessionCount live at
// once.
func TestSDLCRunner_ChangesRequestedRunsAnotherRoundOnTheSameBranch(t *testing.T) {
	rig := startFactory(t, 72)
	loopForge.setVerdictOnOpen("REQUEST_CHANGES")
	// Round one opens the pull request on the head the reviewer refuses.
	// The fake forge keeps that head on the pull request until an approval.
	var refused *sdlc.ForgePullRequest
	require.Eventually(t, func() bool {
		prs := loopForge.pullRequests()
		if len(prs) != 1 || prs[0].HeadSHA == "" {
			return false
		}
		refused = prs[0]
		return true
	}, 90*time.Second, 50*time.Millisecond, "round one never opened its pull request")
	// The reviewer approves only once round two has pushed a new head.
	require.Eventually(t, func() bool {
		return rig.backend.promptCount() >= 2 && branchHeadOrEmpty(t, rig.repo, refused.HeadRef) != refused.HeadSHA
	}, 90*time.Second, 50*time.Millisecond, "the second round never pushed a new head")
	loopForge.approveEveryOpenPullRequest()

	job := awaitJob(t, rig.srv, orchestration.JobStateCompleted)
	require.Equal(t, sdlc.JobRunnerName, job.Runner)
	require.Equal(t, sdlc.WorkerSessionCount, rig.backend.peakLiveSessions(), "a review round held two workers at once")
	require.Equal(t, 2, rig.backend.spawnCount(), "each round spawns one worker, since reviewing released the first")
	workspaces := rig.backend.spawnedWorkspaces()
	require.Equal(t, workspaces[0], workspaces[1], "the review round ran on a different workspace or branch")
	require.GreaterOrEqual(t, rig.backend.promptCount(), 2, "the findings never reached the worker")
	require.Len(t, loopForge.pullRequests(), 1, "a review round opened a second pull request")
	t.Logf("OBSERVED: sessions=%d peak=%d prompts=%d iteration=%d",
		rig.backend.spawnCount(), rig.backend.peakLiveSessions(), rig.backend.promptCount(), job.Iteration)
}

// TestSDLCRunner_TrunkBasedCarveOutLandsWithoutAPullRequest: a change class
// the project exempted is pushed straight to the trunk. The work still has
// its own signed branch; what the carve-out removes is the review round.
func TestSDLCRunner_TrunkBasedCarveOutLandsWithoutAPullRequest(t *testing.T) {
	rig := startFactory(t, 73, "task:ready", "docs")
	ctx := tenantctx.WithTenant(context.Background(), loopE2ETenant)
	binding, err := rig.srv.SDLCService().Binding(ctx, loopE2ETenant, rig.projectID)
	require.NoError(t, err)
	binding.Strategy = sdlc.TrunkBasedName
	_, err = rig.srv.SDLCService().Bind(ctx, loopE2ETenant, binding, service.BindOptions{})
	require.NoError(t, err)

	job := awaitJob(t, rig.srv, orchestration.JobStateCompleted)
	require.Empty(t, loopForge.pullRequests(), "a carve-out opened a pull request")
	delivery, err := rig.srv.SDLCService().Delivery(factoryCtx(), loopE2ETenant, taskOfJob(t, rig.srv, job))
	require.NoError(t, err)
	require.False(t, delivery.RequiresPullRequest, "a docs change under trunk-based was planned for review")
	// The work landed ON THE TRUNK: the push named the target as its remote
	// branch, so main now carries the head the delivery recorded and no
	// topic branch reached the remote at all.
	require.Equal(t, delivery.HeadSHA, headOfBranch(t, rig.repo, "main"), "the carve-out did not land on the trunk")
	require.Empty(t, branchHeadOrEmpty(t, rig.repo, delivery.Branch), "the carve-out published a topic branch as well")
	require.Contains(t, loopForge.issueLabels(73), "factory:merged")
}

// headOfBranch reads a branch head straight out of the bare repository.
func headOfBranch(t *testing.T, repoDir, branch string) string {
	t.Helper()
	head := branchHeadOrEmpty(t, repoDir, branch)
	require.NotEmpty(t, head, "branch %s is not in the remote", branch)
	return head
}

// branchHeadOrEmpty is headOfBranch for a branch that may not exist.
func branchHeadOrEmpty(t *testing.T, repoDir, branch string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestSDLCRunner_GitflowFeatureAndHotfixTakeDifferentRefs: the strategy owns
// both halves. A feature cuts from develop and targets it; a hotfix cuts from
// the release line, targets it, and is BACK-MERGED into develop behind it.
func TestSDLCRunner_GitflowFeatureAndHotfixTakeDifferentRefs(t *testing.T) {
	rig := startFactory(t, 74, "task:ready", "hotfix")
	seedBranch(t, rig.repo, "develop")
	ctx := tenantctx.WithTenant(context.Background(), loopE2ETenant)
	binding, err := rig.srv.SDLCService().Binding(ctx, loopE2ETenant, rig.projectID)
	require.NoError(t, err)
	binding.Strategy = sdlc.GitFlowName
	_, err = rig.srv.SDLCService().Bind(ctx, loopE2ETenant, binding, service.BindOptions{})
	require.NoError(t, err)
	loopForge.setVerdictOnOpen("APPROVED")

	awaitJob(t, rig.srv, orchestration.JobStateCompleted)

	prs := loopForge.pullRequests()
	var hotfix, backMerge *sdlc.ForgePullRequest
	for _, pr := range prs {
		switch {
		case pr.HeadRef == "hotfix/74-fix-the-widget":
			hotfix = pr
		case pr.BaseRef == sdlc.GitFlowDevelopBranch && pr.HeadRef == "main":
			backMerge = pr
		}
	}
	require.NotNil(t, hotfix, "no hotfix pull request: %+v", prs)
	require.Equal(t, "main", hotfix.BaseRef, "a hotfix must target the release line")
	require.NotNil(t, backMerge, "the hotfix was not back-merged into develop: %+v", prs)
	t.Logf("OBSERVED: hotfix #%d -> %s, back-merge #%d %s -> %s",
		hotfix.Number, hotfix.BaseRef, backMerge.Number, backMerge.HeadRef, backMerge.BaseRef)
}

// seedBranch creates a branch in the bare repository at its current main.
func seedBranch(t *testing.T, repoDir, name string) {
	t.Helper()
	require.NoError(t, runGit(repoDir, "branch", name, "main"))
}

// TestSDLCRunner_AKilledWorkerResumesAndFinishesTheSameBranch: the node dies
// mid-turn and its successor picks the job up. The runner restarts at
// initializing, where Prepare CONTINUES the branch the attempt already
// planned — one branch, one pull request, whatever happened to the process.
func TestSDLCRunner_AKilledWorkerResumesAndFinishesTheSameBranch(t *testing.T) {
	registerLoopForge(t)
	repo := bareFactoryRepo(t)
	loopForge.reset(repo)
	loopForge.putIssue(75, "Fix the widget")
	loopForge.setVerdictOnOpen("APPROVED")
	dataDir := t.TempDir()

	srv := startFactoryNode(t, dataDir, true, 2*time.Second, 1)
	blocked := newE2EBackend()
	blocked.block = make(chan struct{})
	registerFactoryProject(t, srv, blocked, repo)

	require.Eventually(t, func() bool { return blocked.promptCount() > 0 }, 90*time.Second, 50*time.Millisecond,
		"the worker was never prompted before the node was killed")
	// The node dies with the worker's turn still in flight. The blocked
	// backend is never released: shutdown cancels the job's context, which is
	// exactly what a killed node does to the turn it was in the middle of.
	require.NoError(t, srv.Shutdown(context.Background()))

	// A HARD kill is the one thing an in-process test cannot stage: this
	// process does not die, so the runner's teardown still runs and releases
	// the attempt's agent. What a killed daemon leaves is staged directly —
	// a session directory holding a socket whose listener is gone, which is
	// exactly what survives when the process holding it never returns.
	// go-quicbaas#633: the successor sweeps it at startup, because the
	// sockets root outlives the daemon and nothing else would ever remove it.
	orphan := plantKilledSigningSession(t, factorySocketsRoot(t))

	successor := startFactoryNode(t, dataDir, false, 2*time.Second, 1)
	t.Cleanup(func() { _ = successor.Shutdown(context.Background()) })
	require.NoDirExists(t, orphan,
		"the session a killed daemon left survived the successor's startup: %s", orphan)

	resumed := newE2EBackend()
	registerFactoryProject(t, successor, resumed, repo)

	awaitJob(t, successor, orchestration.JobStateCompleted)
	prs := loopForge.pullRequests()
	require.Len(t, prs, 1, "the resumed attempt opened a second pull request: %+v", prs)
	require.True(t, prs[0].Merged)
	require.Equal(t, "issue/75-fix-the-widget", prs[0].HeadRef, "the resumed attempt cut a different branch")

	// And the attempt this node did finish left nothing of its own behind.
	requireNoDeadSigningSessions(t, factorySocketsRoot(t))
	t.Logf("OBSERVED: resumed to a merged pull request on %s, sockets root swept", prs[0].HeadRef)
}

// plantKilledSigningSession stages the residue of a daemon that died holding
// an attempt's signing agent: a session directory named the way the runner
// names one, holding a bound socket whose listener is gone. Closing the
// listener WITHOUT unlinking is the difference between a kill and a release.
func plantKilledSigningSession(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "df769f4a7dd5")
	require.NoError(t, os.Mkdir(dir, 0o700))
	socket := filepath.Join(dir, orchestration.WorkspaceSigningAgentSocketFile)
	require.LessOrEqual(t, len(socket), orchestration.MaxSigningSocketPath,
		"the staged socket path is over the kernel's sun_path budget")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	require.NoError(t, err)
	ln.SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())
	require.FileExists(t, socket, "the dead socket did not survive its listener")
	return dir
}

// requireNoDeadSigningSessions asserts the sockets root holds no session
// directory whose agent has stopped answering. A session that is still live
// belongs to a node this process has not shut down yet and is not an orphan.
func requireNoDeadSigningSessions(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		socket := filepath.Join(root, entry.Name(), orchestration.WorkspaceSigningAgentSocketFile)
		conn, dialErr := net.DialTimeout("unix", socket, 2*time.Second)
		require.NoError(t, dialErr,
			"a dead signing session was left under the sockets root: %s", filepath.Dir(socket))
		require.NoError(t, conn.Close())
	}
}

// =============================================================================
// The container commits, signs and pushes — the daemon never runs git
// =============================================================================

// TestSDLCRunner_TheContainerCommitsSignsAndPushes is go-quicbaas#608 proved
// on a real node.
//
// It asserts the three things the phase moved, each of which was previously
// done on the daemon against the host side of a bind mount:
//
//   - The commit and the push are COMMANDS RUN IN THE WORKER'S CONTAINER,
//     through the runtime's exec seam, in the directory the CONTAINER calls
//     its checkout. A daemon-side git would have named a host path, which
//     this container refuses.
//   - The commit is SIGNED, and the signature verifies against the tenant's
//     KMS-held signing key — through the ephemeral agent whose socket the
//     spawn mounted, with the key itself never leaving the node's key store.
//   - The daemon's own git provisioner is never asked to touch the workspace:
//     the module has no workspace port left to ask through, and every git
//     process that ran is accounted for by the container's own exec log.
func TestSDLCRunner_TheContainerCommitsSignsAndPushes(t *testing.T) {
	rig := startFactory(t, 76)
	loopForge.setVerdictOnOpen("APPROVED")

	job := awaitJob(t, rig.srv, orchestration.JobStateCompleted)
	delivery, err := rig.srv.SDLCService().Delivery(factoryCtx(), loopE2ETenant, taskOfJob(t, rig.srv, job))
	require.NoError(t, err)
	require.NotEmpty(t, delivery.HeadSHA, "no head was published")

	// Every git command ran inside the container, under the path the
	// CONTAINER calls its checkout root — the clone in the root itself, and
	// everything after it in the project checkout under it. A daemon-side git
	// would have named a host path, which this container refuses.
	containerDir := filepath.Join(orchestration.WorkspaceCheckoutRoot, loopE2EProject)
	commands := rig.runtime.commandsRun()
	require.NotEmpty(t, commands, "no command was run in the worker's container")
	var sawClone, sawCommit, sawPush, sawCredential bool
	for _, call := range rig.runtime.execs {
		require.Contains(t, []string{orchestration.WorkspaceCheckoutRoot, containerDir}, call.WorkDir,
			"a command named %q, which is neither the checkout root nor the container's checkout", call.WorkDir)
		// Beside git and the binding's verification, the runner delivers the
		// forge token to the container's credential tier and revokes it; those
		// run there and nothing else does.
		if call.forgeCredentialStep() {
			sawCredential = true
			continue
		}
		require.Contains(t, []string{"git", "sh"}, call.Cmd[0],
			"the runner ran something unaccounted for in the container: %v", call.Cmd)
		if strings.Contains(strings.Join(call.Cmd, " "), " clone ") {
			sawClone = true
		}
		joined := strings.Join(call.Cmd, " ")
		if strings.Contains(joined, " commit ") {
			sawCommit = true
			require.Contains(t, joined, "commit.gpgsign=true", "the commit was not signed")
			require.Contains(t, joined, "user.signingkey=key::", "the commit named no signing key")
		}
		if strings.Contains(joined, " push ") {
			sawPush = true
		}
	}
	require.True(t, sawClone, "nothing cloned in the container: %v", commands)
	require.True(t, sawCommit, "nothing committed in the container: %v", commands)
	require.True(t, sawPush, "nothing pushed from the container: %v", commands)
	require.True(t, sawCredential, "the forge token never reached the container: %v", commands)

	// The commit that reached the remote carries a signature, and git accepts
	// it against the tenant's signing key.
	raw := gitOut(t, rig.repo, "cat-file", "commit", delivery.HeadSHA)
	require.Contains(t, raw, "BEGIN SSH SIGNATURE",
		"the head the container published carries no signature:\n%s", raw)
	require.Contains(t, raw, "agent-"+loopE2EProject+"@example.com",
		"the commit was not authored as the project's agent identity:\n%s", raw)

	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	entry := "agent-" + loopE2EProject + "@example.com " + tenantSigningPublicKey(t, rig.srv)
	require.NoError(t, os.WriteFile(allowed, []byte(entry+"\n"), 0o600))
	verified := gitOut(t, rig.repo, "-c", "gpg.ssh.allowedSignersFile="+allowed, "verify-commit", delivery.HeadSHA)
	require.Contains(t, verified, "Good",
		"git did not accept the signature against the tenant's signing key:\n%s", verified)

	// And nothing signed anything on the daemon: the module holds no git
	// port, so the only signing agent that ran is the one the spawn mounted,
	// and it is gone with the attempt.
	spawn := rig.backend.spawnRequests()[0]
	var socket string
	for _, mount := range spawn.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSigningAgentSocket {
			socket = mount.Source
		}
	}
	require.NotEmpty(t, socket, "the signing socket was not mounted: %+v", spawn.ExtraMounts)
	require.Equal(t, orchestration.WorkspaceSigningAgentSocket, spawn.Env[orchestration.WorkspaceSigningAgentEnv],
		"SSH_AUTH_SOCK does not name the socket inside the container")
	require.Equal(t, factorySocketsRoot(t), filepath.Dir(filepath.Dir(socket)),
		"the socket was bound outside the daemon's sockets root: %s", socket)
	require.LessOrEqual(t, len(socket), orchestration.MaxSigningSocketPath,
		"the socket path is over the kernel's sun_path budget")
	_, statErr := os.Stat(filepath.Dir(socket))
	require.True(t, os.IsNotExist(statErr),
		"the signing agent's directory outlived the job: %v", statErr)

	t.Logf("OBSERVED: %d container command(s), head %s signed and verified", len(commands), delivery.HeadSHA)
}

// TestSDLCRunner_TheContainerRunsTheVerificationCommands is go-quicbaas#623
// proved on a real node.
//
// The binding's verification is a tenant repository's own build and tests.
// It used to run on the DAEMON, in the host side of the bind mount, under the
// daemon's toolchain. It now runs where the work is:
//
//   - The binding's command appears in the container's exec log, run by the
//     shell asked for by name, in the directory the CONTAINER calls its
//     checkout. This container refuses any other directory, so a daemon path
//     could not have produced a passing run.
//   - No verification named the daemon's side of the bind mount. The runner
//     still RECORDS that directory on the job, for a reader on the daemon,
//     but nothing it executes uses the name: the validator is handed the
//     job's container instead.
//   - The operator's allowlist still decides. The command that ran is the one
//     orchestration.verification_commands admits.
func TestSDLCRunner_TheContainerRunsTheVerificationCommands(t *testing.T) {
	rig := startFactory(t, 77)
	loopForge.setVerdictOnOpen("APPROVED")

	awaitJob(t, rig.srv, orchestration.JobStateCompleted)

	containerDir := filepath.Join(orchestration.WorkspaceCheckoutRoot, loopE2EProject)
	hostDir := rig.backend.spawnRequests()[0].WorkDir
	require.NotEmpty(t, hostDir, "the spawn named no host mount")

	var verifications []e2eExecCall
	for _, call := range rig.runtime.execs {
		if call.Cmd[0] == "sh" && !call.forgeCredentialStep() {
			verifications = append(verifications, call)
		}
	}
	require.NotEmpty(t, verifications,
		"the binding's verification never ran in the container: %v", rig.runtime.commandsRun())
	for _, call := range verifications {
		require.Equal(t, []string{"sh", "-c", "test -f WORK.md"}, call.Cmd,
			"the verification command was not the operator-allowed one")
		require.Equal(t, containerDir, call.WorkDir,
			"verification ran in %q, which is not the container's checkout", call.WorkDir)
		require.NotEqual(t, hostDir, call.WorkDir,
			"verification named the daemon's side of the bind mount")
		require.Zero(t, call.ExitCode, "the verification the worker's own checkout satisfies failed")
	}

	t.Logf("OBSERVED: %d verification command(s) in %s", len(verifications), containerDir)
}

// tenantSigningPublicKey exports the public half of the key the project signs
// with, from the node's own KMS-backed SSH store.
func tenantSigningPublicKey(t *testing.T, srv *server.Server) string {
	t.Helper()
	ctx := tenantctx.WithTenant(context.Background(), loopE2ETenant)
	sshClient := srv.Service().SSHClient()
	require.NotNil(t, sshClient)
	pub, err := sshClient.Keys().ExportPublic(ctx, xkmsssh.ExportRequest{
		Scope: loopE2EKeyScope, Owner: loopE2EKeyOwner, Name: loopE2EKeyName,
	})
	require.NoError(t, err, "exporting the tenant signing key")
	return strings.TrimSpace(pub.Line)
}

// gitOut runs a git command in a repository and returns its combined output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}
