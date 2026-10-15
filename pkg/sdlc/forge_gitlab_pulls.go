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
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// GitLab: branches, tags and merge requests
// =============================================================================

// gitlabDraftPrefix marks a merge request as a draft. GitLab's create route
// takes no draft field; the title prefix is how a draft is opened.
const gitlabDraftPrefix = "Draft: "

type gitlabBranch struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// toForgeBranch converts GitLab's shape to the module's.
func (b *gitlabBranch) toForgeBranch() *ForgeBranch {
	return &ForgeBranch{Name: b.Name, SHA: b.Commit.ID}
}

// gitlabBranchPath renders the API path of one branch.
func gitlabBranchPath(repo RepoRef, name string) string {
	return gitlabProjectPath(repo) + "/repository/branches/" + gitlabRefPath(name)
}

// GetTag returns one tag and the commit it names. GitLab peels an annotated
// tag itself: commit.id is always the commit, never the tag object.
func (g *GitLabBackend) GetTag(ctx context.Context, repo RepoRef, tag string) (*ForgeTag, error) {
	if strings.TrimSpace(tag) == "" {
		return nil, &InvalidInputError{Field: "tag", Message: "a tag read names the tag"}
	}
	var raw gitlabBranch
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabProjectPath(repo) + "/repository/tags/" + gitlabRefPath(tag),
		out:       &raw,
		operation: "GetTag",
	}); err != nil {
		return nil, err
	}
	if raw.Commit.ID == "" {
		return nil, &IncompleteResponseError{Op: "GetTag " + repo.String() + " " + tag, Missing: "commit.id"}
	}
	return &ForgeTag{Name: tag, SHA: raw.Commit.ID}, nil
}

// CreateBranch creates a branch at fromRef — a branch, tag or commit — and
// verifies it by reading it back.
func (g *GitLabBackend) CreateBranch(ctx context.Context, repo RepoRef, name, fromRef string) (*ForgeBranch, error) {
	if name == "" {
		return nil, &InvalidInputError{Field: "name", Message: "a branch must be named"}
	}
	if fromRef == "" {
		return nil, &InvalidInputError{Field: "from_ref", Message: "a branch must be cut from somewhere"}
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      gitlabProjectPath(repo) + "/repository/branches",
		body:      map[string]string{"branch": name, "ref": fromRef},
		operation: "CreateBranch",
	}); err != nil {
		return nil, err
	}

	branch, err := g.GetBranch(ctx, repo, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &ForgeWriteNotAppliedError{
				Type:      ForgeGitLab,
				Operation: "CreateBranch",
				Subject:   repo.String() + " " + name,
				Want:      "branch present",
				Got:       "absent",
			}
		}
		return nil, err
	}
	return branch, nil
}

// GetBranch returns one branch.
func (g *GitLabBackend) GetBranch(ctx context.Context, repo RepoRef, name string) (*ForgeBranch, error) {
	var raw gitlabBranch
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabBranchPath(repo, name),
		out:       &raw,
		operation: "GetBranch",
	}); err != nil {
		return nil, err
	}
	return raw.toForgeBranch(), nil
}

// DeleteBranch removes a branch and verifies its absence. An already-absent
// branch — deleted by the merge that landed it — answers 404 and succeeds.
func (g *GitLabBackend) DeleteBranch(ctx context.Context, repo RepoRef, name string) error {
	err := g.do(ctx, forgeRequest{
		method:    http.MethodDelete,
		path:      gitlabBranchPath(repo, name),
		operation: "DeleteBranch",
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	if _, err := g.GetBranch(ctx, repo, name); err == nil {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: "DeleteBranch",
			Subject:   repo.String() + " " + name,
			Want:      "absent",
			Got:       "branch present",
		}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

type gitlabMergeRequest struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	State        string `json:"state"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	SHA          string `json:"sha"`
	Draft        bool   `json:"draft"`

	// MergeCommitSHA is null for a fast-forward or squash-only merge, which
	// leaves SquashCommitSHA or the head itself as the landed commit.
	MergeCommitSHA  string `json:"merge_commit_sha"`
	SquashCommitSHA string `json:"squash_commit_sha"`

	SourceProjectID int64 `json:"source_project_id"`
	TargetProjectID int64 `json:"target_project_id"`

	// DetailedMergeStatus is "mergeable" when nothing blocks the merge, and
	// names the blocker otherwise ("checking", "ci_still_running", ...).
	DetailedMergeStatus string `json:"detailed_merge_status"`

	Author    *gitlabUser `json:"author"`
	WebURL    string      `json:"web_url"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Labels    []string    `json:"labels"`
}

// toForgePullRequest converts GitLab's shape to the module's. repo is the
// project the merge request was read from.
//
// A merged merge request is reported closed and Merged, the way the other
// forges report it. The landed commit is the merge commit, else the squash
// commit, else — a fast-forward — the head itself, and it is reported only
// once merged, so mergeLanded never reads an open merge request as landed.
//
// The source project is named only when it IS this project. GitLab reports a
// fork by id alone, and resolving the id would cost a read per merge request
// for an answer only "is it ours" consumes: a zero HeadRepo is a fork, never
// adopted and never cleaned up after.
func (m *gitlabMergeRequest) toForgePullRequest(repo RepoRef) *ForgePullRequest {
	pull := &ForgePullRequest{
		Number:    m.IID,
		Title:     m.Title,
		Body:      m.Description,
		State:     gitlabState(m.State),
		HeadRef:   m.SourceBranch,
		HeadSHA:   m.SHA,
		BaseRef:   m.TargetBranch,
		Draft:     m.Draft,
		Mergeable: m.DetailedMergeStatus == "mergeable",
		URL:       m.WebURL,
		Created:   m.CreatedAt,
		Updated:   m.UpdatedAt,
		Labels:    m.Labels,
	}
	if m.SourceProjectID != 0 && m.SourceProjectID == m.TargetProjectID {
		pull.HeadRepo = repo
	}
	if m.Author != nil {
		pull.Author = m.Author.Username
	}
	if m.State == "merged" {
		pull.State = "closed"
		pull.Merged = true
		pull.MergeSHA = firstNonEmpty(m.MergeCommitSHA, m.SquashCommitSHA, m.SHA)
	}
	return pull
}

// firstNonEmpty returns the first of values that is not empty.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// gitlabMergeRequestPath renders the API path of one merge request.
func gitlabMergeRequestPath(repo RepoRef, number int) string {
	return gitlabProjectPath(repo) + "/merge_requests/" + strconv.Itoa(number)
}

// CommentOnPullRequest posts a note on a merge request's discussion. GitLab
// numbers merge requests apart from issues, so this never touches an issue's
// notes, even one that shares the number.
func (g *GitLabBackend) CommentOnPullRequest(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error) {
	return g.postNote(ctx, "CommentOnPullRequest", gitlabMergeRequestPath(repo, number), fmt.Sprintf("%s!%d", repo, number), body)
}

// ListPullRequestComments returns the notes people wrote on a merge request,
// oldest first, whole. GitLab's system notes are left out, as for an issue.
func (g *GitLabBackend) ListPullRequestComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error) {
	notes, err := g.listNotes(ctx, "ListPullRequestComments", gitlabMergeRequestPath(repo, number), errCommentPagesExhausted)
	if err != nil {
		return nil, err
	}
	return gitlabComments(notes), nil
}

// ListOpenPullRequests returns every open merge request on the project.
func (g *GitLabBackend) ListOpenPullRequests(ctx context.Context, repo RepoRef) ([]ForgePullRequest, error) {
	query := url.Values{}
	query.Set("state", "opened")
	var pulls []ForgePullRequest
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListOpenPullRequests",
		path:      gitlabProjectPath(repo) + "/merge_requests",
		query:     query,
		maxPages:  maxIssuePages,
		exhausted: backendError("ListOpenPullRequests", errOpenPullPagesExhausted),
	}, func(page []gitlabMergeRequest) bool {
		for i := range page {
			pulls = append(pulls, *page[i].toForgePullRequest(repo))
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return pulls, nil
}

// CreatePullRequest opens a merge request — or adopts the one already open for
// the same head -> base — and verifies it by reading it back. See
// openPullRequest for why adoption is the idempotency.
func (g *GitLabBackend) CreatePullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec) (*ForgePullRequest, error) {
	switch {
	case spec.Title == "":
		return nil, &InvalidInputError{Field: "title", Message: "a pull request must be titled"}
	case spec.HeadRef == "":
		return nil, &InvalidInputError{Field: "head_ref", Message: "a pull request must name its source branch"}
	case spec.BaseRef == "":
		return nil, &InvalidInputError{Field: "base_ref", Message: "a pull request must name its target branch"}
	}

	number, err := g.openPullRequest(ctx, g, repo, spec, func(ctx context.Context) (int, error) {
		return g.postMergeRequest(ctx, repo, spec)
	})
	if err != nil {
		return nil, err
	}

	pull, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	if spec.Draft && !pull.Draft {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: "CreatePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "draft",
			Got:       "ready for review",
		}
	}
	if pull.HeadRef != spec.HeadRef || pull.BaseRef != spec.BaseRef {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: "CreatePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      spec.HeadRef + " -> " + spec.BaseRef,
			Got:       pull.HeadRef + " -> " + pull.BaseRef,
		}
	}

	if len(spec.Labels) > 0 {
		if err := g.setMergeRequestLabels(ctx, repo, number, spec.Labels); err != nil {
			return nil, err
		}
	}
	return pull, nil
}

// postMergeRequest sends one create and returns the iid GitLab assigned.
func (g *GitLabBackend) postMergeRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec) (int, error) {
	title := spec.Title
	if spec.Draft && !strings.HasPrefix(title, gitlabDraftPrefix) {
		title = gitlabDraftPrefix + title
	}
	var created gitlabMergeRequest
	if err := g.do(ctx, forgeRequest{
		method: http.MethodPost,
		path:   gitlabProjectPath(repo) + "/merge_requests",
		body: map[string]any{
			"title":         title,
			"description":   spec.Body,
			"source_branch": spec.HeadRef,
			"target_branch": spec.BaseRef,
		},
		out:       &created,
		operation: "CreatePullRequest",
	}); err != nil {
		return 0, err
	}
	if created.IID == 0 {
		return 0, &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: "CreatePullRequest",
			Subject:   repo.String() + " " + spec.HeadRef + " -> " + spec.BaseRef,
			Want:      "a pull request number",
			Got:       "none",
		}
	}
	return created.IID, nil
}

// setMergeRequestLabels replaces a merge request's labels, verified by reading
// its labels back. A merge request's labels live on the merge request, not on
// an issue of the same number as they do on Gitea and GitHub.
func (g *GitLabBackend) setMergeRequestLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	names, err := g.resolveLabelNames(ctx, repo, labels)
	if err != nil {
		return err
	}
	var raw struct {
		Labels []string `json:"labels"`
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPut,
		path:      gitlabMergeRequestPath(repo, number),
		body:      map[string]string{"labels": strings.Join(names, ",")},
		out:       &raw,
		operation: "SetIssueLabels",
	}); err != nil {
		return err
	}
	return verifyLabelsSet(ForgeGitLab, repo, number, labels, raw.Labels)
}

// GetPullRequest returns one merge request.
func (g *GitLabBackend) GetPullRequest(ctx context.Context, repo RepoRef, number int) (*ForgePullRequest, error) {
	var raw gitlabMergeRequest
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabMergeRequestPath(repo, number),
		out:       &raw,
		operation: "GetPullRequest",
	}); err != nil {
		return nil, err
	}
	return raw.toForgePullRequest(repo), nil
}

// AddPullRequestLabels adds labels to a merge request through the merge
// request's own route, never the issue that shares its number; see addLabels.
func (g *GitLabBackend) AddPullRequestLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	return g.addLabels(ctx, repo, number, labels, gitlabLabelled{
		operation: "AddPullRequestLabels",
		path:      gitlabMergeRequestPath(repo, number),
		read:      g.mergeRequestLabels,
	})
}

// RemovePullRequestLabel removes one label from a merge request through the
// merge request's own route; see removeLabel.
func (g *GitLabBackend) RemovePullRequestLabel(ctx context.Context, repo RepoRef, number int, label string) error {
	return g.removeLabel(ctx, repo, number, label, gitlabLabelled{
		operation: "RemovePullRequestLabel",
		path:      gitlabMergeRequestPath(repo, number),
		read:      g.mergeRequestLabels,
	})
}

// mergeRequestLabels reads a merge request's labels back.
func (g *GitLabBackend) mergeRequestLabels(ctx context.Context, repo RepoRef, number int) ([]string, error) {
	pull, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	return pull.Labels, nil
}

// ClosePullRequest closes a merge request without merging it, verified by
// read-back. A merge request that is already closed or merged is left as it
// is.
func (g *GitLabBackend) ClosePullRequest(ctx context.Context, repo RepoRef, number int) error {
	if number <= 0 {
		return &InvalidInputError{Field: "pull_request", Message: "pull requests are numbered from one"}
	}
	current, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return err
	}
	if pullRequestStateFor(current) != PullRequestOpen {
		return nil
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPut,
		path:      gitlabMergeRequestPath(repo, number),
		body:      map[string]string{"state_event": "close"},
		operation: "ClosePullRequest",
	}); err != nil {
		return err
	}
	closed, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return err
	}
	if pullRequestStateFor(closed) == PullRequestOpen {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: "ClosePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "closed",
			Got:       "state " + closed.State,
		}
	}
	return nil
}

// gitlabMergeMethods maps a MergeStrategy's forge-neutral method name onto the
// project merge methods that land it that way.
//
// GitLab fixes the merge method per PROJECT, not per merge: the merge route
// cannot choose one. So a requested method is honoured only when the project
// is configured to produce it, and refused otherwise rather than landed some
// other way. "merge" is a merge commit, which both "merge" and the
// semi-linear "rebase_merge" produce; "rebase" is a linear history, which only
// fast-forward produces. Squash is a per-merge flag and works with any
// method (see gitlabSquashOptions).
var gitlabMergeMethods = map[string][]string{
	"merge":  {"merge", "rebase_merge"},
	"rebase": {"ff"},
	"squash": {"merge", "rebase_merge", "ff"},
}

// gitlabProject is the project settings a merge reads.
type gitlabProject struct {
	MergeMethod  string `json:"merge_method"`
	SquashOption string `json:"squash_option"`
}

// MergePullRequest merges according to spec, refusing when the head has moved
// past the tree the caller approved.
//
// expectedHead is sent as "sha", which makes GITLAB refuse the merge with 409
// if the source branch moved — the authoritative guard, decided where the
// race is. That refusal becomes HeadMovedError and is never retried. 405, 406
// and 422 ("not mergeable yet") are ForgeNotReadyError, which the caller's
// bounded merge retry handles. The adapter never asks GitLab to rebase the
// branch: a rebase moves the head the review approved.
func (g *GitLabBackend) MergePullRequest(ctx context.Context, repo RepoRef, number int, spec MergeSpec, expectedHead string) error {
	method := strings.ToLower(strings.TrimSpace(spec.Method))
	accepted, ok := gitlabMergeMethods[method]
	if !ok {
		return &ForgeUnsupportedError{Type: ForgeGitLab, Capability: "merge method " + spec.Method}
	}
	if expectedHead == "" {
		return &InvalidInputError{
			Field:   "expected_head",
			Message: "a merge must name the tree that was approved, or a later commit lands on an older review",
		}
	}

	subject := repo.String() + "#" + strconv.Itoa(number)
	pull, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return err
	}
	if mergeLanded(pull) {
		// A merge that landed under an earlier, unanswered attempt still owes
		// its branch cleanup.
		return deleteMergedSource(ctx, g, repo, pull, spec)
	}
	if pull.HeadSHA != expectedHead {
		return &HeadMovedError{Subject: subject, Reviewed: shortSHA(expectedHead), Current: shortSHA(pull.HeadSHA)}
	}
	if err := g.checkMergeMethod(ctx, repo, method, accepted); err != nil {
		return err
	}

	err = g.do(ctx, forgeRequest{
		method:    http.MethodPut,
		path:      gitlabMergeRequestPath(repo, number) + "/merge",
		body:      gitlabMergeBody(repo, pull, spec, method, expectedHead),
		operation: "MergePullRequest",
	})
	var moved *HeadMovedError
	switch {
	case errors.As(err, &moved):
		return g.headMoved(ctx, g, repo, number, expectedHead)
	case errors.Is(err, ErrForgeUnanswered) || forgeInternalError(err):
		if err := g.reconcileMerge(ctx, g, repo, number, err); err != nil {
			return err
		}
	case err != nil:
		return err
	}

	merged, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return err
	}
	if !merged.Merged {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: "MergePullRequest",
			Subject:   subject,
			Want:      "merged",
			Got:       "state " + merged.State,
		}
	}
	// should_remove_source_branch asked GitLab to delete the branch; this
	// confirms it, and deletes it when a reconciled merge never carried the
	// request.
	return deleteMergedSource(ctx, g, repo, merged, spec)
}

// checkMergeMethod refuses a merge the project's settings would land some
// other way than method asks.
func (g *GitLabBackend) checkMergeMethod(ctx context.Context, repo RepoRef, method string, accepted []string) error {
	var project gitlabProject
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabProjectPath(repo),
		out:       &project,
		operation: "GetProject",
	}); err != nil {
		return err
	}
	if !slices.Contains(accepted, project.MergeMethod) {
		return &ForgeUnsupportedError{
			Type:       ForgeGitLab,
			Capability: "merge method " + method + " on a project configured for " + project.MergeMethod,
		}
	}
	if method == "squash" && project.SquashOption == "never" {
		return &ForgeUnsupportedError{Type: ForgeGitLab, Capability: "squash merge on a project that never squashes"}
	}
	return nil
}

// gitlabMergeBody renders the merge route's body.
func gitlabMergeBody(repo RepoRef, pull *ForgePullRequest, spec MergeSpec, method, expectedHead string) map[string]any {
	body := map[string]any{
		"sha":    expectedHead,
		"squash": method == "squash",
	}
	// Only this project's branch is the merge's to delete; see
	// deleteMergedSource.
	if spec.ExpectSourceDeleted && sameRepo(pull.HeadRepo, repo) {
		body["should_remove_source_branch"] = true
	}
	if message := strings.TrimSpace(strings.Join([]string{spec.CommitTitle, spec.CommitMessage}, "\n\n")); message != "" {
		if method == "squash" {
			body["squash_commit_message"] = message
		} else {
			body["merge_commit_message"] = message
		}
	}
	return body
}

type gitlabDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
}

// toForgeChangedFile converts GitLab's shape to the module's. GitLab reports
// no line counts, so they are counted from the diff's own lines; a diff GitLab
// collapsed as too large counts as none.
func (d *gitlabDiff) toForgeChangedFile() ForgeChangedFile {
	file := ForgeChangedFile{Path: d.NewPath, Status: "modified"}
	switch {
	case d.NewFile:
		file.Status = "added"
	case d.DeletedFile:
		file.Status = "deleted"
		file.Path = d.OldPath
	case d.RenamedFile:
		file.Status = "renamed"
		file.PreviousPath = d.OldPath
	}
	for _, line := range strings.Split(d.Diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			file.Additions++
		case strings.HasPrefix(line, "-"):
			file.Deletions++
		}
	}
	return file
}

// ListPullRequestFiles returns every file the merge request changes, paged to
// the end; a listing that never ends is a fault.
func (g *GitLabBackend) ListPullRequestFiles(ctx context.Context, repo RepoRef, number int) ([]ForgeChangedFile, error) {
	var files []ForgeChangedFile
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListPullRequestFiles",
		path:      gitlabMergeRequestPath(repo, number) + "/diffs",
		maxPages:  maxIssuePages,
		exhausted: backendError("ListPullRequestFiles", errPullFilePagesExhausted),
	}, func(page []gitlabDiff) bool {
		for i := range page {
			files = append(files, page[i].toForgeChangedFile())
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
