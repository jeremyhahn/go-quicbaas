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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// GitHub: issues, comments, labels and collaborators
// =============================================================================

type githubIssue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	State     string        `json:"state"`
	Labels    []githubLabel `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	User      *githubUser `json:"user"`
	Assignee  *githubUser `json:"assignee"`
	HTMLURL   string      `json:"html_url"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`

	// PullRequest is present only on an issue that is a pull request.
	PullRequest json.RawMessage `json:"pull_request"`
}

// isPullRequest reports whether the issues listing returned a pull request.
func (i *githubIssue) isPullRequest() bool {
	return len(i.PullRequest) > 0 && string(i.PullRequest) != "null"
}

// toForgeIssue converts GitHub's shape to the module's.
func (i *githubIssue) toForgeIssue() *ForgeIssue {
	issue := &ForgeIssue{
		Number:  i.Number,
		Title:   i.Title,
		Body:    i.Body,
		State:   i.State,
		URL:     i.HTMLURL,
		Created: i.CreatedAt,
		Updated: i.UpdatedAt,
	}
	if i.Milestone != nil {
		issue.Milestone = i.Milestone.Title
	}
	if i.User != nil {
		issue.Author = i.User.Login
	}
	if i.Assignee != nil {
		issue.Assignee = i.Assignee.Login
	}
	for _, label := range i.Labels {
		issue.Labels = append(issue.Labels, label.Name)
	}
	return issue
}

// githubIssuePath renders the API path of one issue.
func githubIssuePath(repo RepoRef, number int) string {
	return repoPath(repo) + "/issues/" + strconv.Itoa(number)
}

// ListIssues returns issues matching filter.
//
// GitHub returns pull requests from the issues endpoint and has no parameter
// to exclude them, so they are dropped here by their pull_request key. That
// filtering happens after paging, which is why a page may come back short in
// the middle of a listing and why the Link header, not the page length, ends
// it (forgePages).
func (g *GitHubBackend) ListIssues(ctx context.Context, repo RepoRef, filter IssueFilter) ([]ForgeIssue, error) {
	query := url.Values{}
	state := filter.State
	if state == "" {
		state = "open"
	}
	query.Set("state", state)
	if len(filter.Labels) > 0 {
		query.Set("labels", strings.Join(filter.Labels, ","))
	}
	if !filter.Since.IsZero() {
		query.Set("since", filter.Since.UTC().Format(time.RFC3339))
	}

	var issues []ForgeIssue
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListIssues",
		path:      repoPath(repo) + "/issues",
		query:     query,
		maxPages:  maxIssuePages,
		exhausted: backendError("ListIssues", errIssuePagesExhausted),
	}, func(page []githubIssue) bool {
		for i := range page {
			if page[i].isPullRequest() {
				continue
			}
			issues = append(issues, *page[i].toForgeIssue())
			if filter.Limit > 0 && len(issues) >= filter.Limit {
				// The CALLER's bound, reached; see the Gitea adapter.
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return issues, nil
}

// GetIssue returns one issue.
func (g *GitHubBackend) GetIssue(ctx context.Context, repo RepoRef, number int) (*ForgeIssue, error) {
	var raw githubIssue
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      githubIssuePath(repo, number),
		out:       &raw,
		operation: "GetIssue",
	}); err != nil {
		return nil, err
	}
	return raw.toForgeIssue(), nil
}

// CreateIssue implements ForgeBackend.
//
// The labels are checked against the repository's vocabulary BEFORE the
// write. GitHub's create route would otherwise invent any label it does not
// know, which turns a typo in a project's label vocabulary into a permanent
// label on the repository.
func (g *GitHubBackend) CreateIssue(ctx context.Context, repo RepoRef, spec IssueSpec) (*ForgeIssue, error) {
	if strings.TrimSpace(spec.Title) == "" {
		return nil, &InvalidInputError{Field: "title", Message: "an issue needs a title"}
	}
	labels, err := g.resolveLabelNames(ctx, repo, spec.Labels)
	if err != nil {
		return nil, err
	}

	body := map[string]any{"title": spec.Title}
	if spec.Body != "" {
		body["body"] = spec.Body
	}
	if len(labels) > 0 {
		body["labels"] = labels
	}
	var raw githubIssue
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/issues",
		body:      body,
		out:       &raw,
		operation: "CreateIssue",
	}); err != nil {
		return nil, err
	}

	issue := raw.toForgeIssue()
	if raw.Number == 0 || raw.Title != spec.Title {
		return nil, &ForgeWriteNotAppliedError{
			Type: ForgeGitHub, Operation: "CreateIssue", Subject: repo.String(),
			Want: truncate(spec.Title, 80), Got: truncate(raw.Title, 80),
		}
	}
	if missing := missingIssueLabels(spec.Labels, issue.Labels); len(missing) > 0 {
		return nil, &ForgeWriteNotAppliedError{
			Type: ForgeGitHub, Operation: "CreateIssue", Subject: repo.String() + "#" + strconv.Itoa(raw.Number),
			Want: "labels " + strings.Join(spec.Labels, ","), Got: "labels " + strings.Join(issue.Labels, ","),
		}
	}
	return issue, nil
}

// CommentOnIssue posts a comment and returns it as GitHub stored it. The
// response is the read-back, as on Gitea.
func (g *GitHubBackend) CommentOnIssue(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, &InvalidInputError{Field: "body", Message: "a comment must say something"}
	}

	var raw githubComment
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      githubIssuePath(repo, number) + "/comments",
		body:      map[string]string{"body": body},
		out:       &raw,
		operation: "CommentOnIssue",
	}); err != nil {
		return nil, err
	}

	if raw.ID == 0 || raw.Body != body {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitHub,
			Operation: "CommentOnIssue",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      truncate(body, 80),
			Got:       truncate(raw.Body, 80),
		}
	}
	comment := raw.toForgeComment()
	return &comment, nil
}

// CloseIssue closes an issue, verified by the response's state.
func (g *GitHubBackend) CloseIssue(ctx context.Context, repo RepoRef, number int) error {
	var raw githubIssue
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPatch,
		path:      githubIssuePath(repo, number),
		body:      map[string]string{"state": "closed"},
		out:       &raw,
		operation: "CloseIssue",
	}); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(raw.State), "closed") {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitHub,
			Operation: "CloseIssue",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "closed",
			Got:       raw.State,
		}
	}
	return nil
}

// ListIssueComments returns an issue's discussion, oldest first, which is the
// order GitHub lists it in. Paged to the end; a listing that never ends is a
// fault, for the reason the Gitea adapter gives.
func (g *GitHubBackend) ListIssueComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error) {
	var comments []ForgeComment
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListIssueComments",
		path:      githubIssuePath(repo, number) + "/comments",
		maxPages:  maxIssuePages,
		exhausted: backendError("ListIssueComments", errCommentPagesExhausted),
	}, func(page []githubComment) bool {
		for i := range page {
			comments = append(comments, page[i].toForgeComment())
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return comments, nil
}

// SetIssueLabels replaces an issue's labels and verifies the result by
// reading the issue back. Labels the repository does not define are refused
// before the write, for the reason CreateIssue gives.
func (g *GitHubBackend) SetIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	names, err := g.resolveLabelNames(ctx, repo, labels)
	if err != nil {
		return err
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPut,
		path:      githubIssuePath(repo, number) + "/labels",
		body:      map[string][]string{"labels": names},
		operation: "SetIssueLabels",
	}); err != nil {
		return err
	}

	issue, err := g.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	return verifyLabelsSet(ForgeGitHub, repo, number, labels, issue.Labels)
}

// AddIssueLabels adds labels to an issue, leaving the others alone, and
// verifies every requested label is present.
func (g *GitHubBackend) AddIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	names, err := g.resolveLabelNames(ctx, repo, labels)
	if err != nil {
		return err
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      githubIssuePath(repo, number) + "/labels",
		body:      map[string][]string{"labels": names},
		operation: "AddIssueLabels",
	}); err != nil {
		return err
	}

	issue, err := g.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	return verifyLabelsAdded(ForgeGitHub, repo, number, labels, issue.Labels)
}

// RemoveIssueLabel removes one label from an issue and verifies it is gone.
// A label the issue does not carry answers 404, which is the state asked for.
func (g *GitHubBackend) RemoveIssueLabel(ctx context.Context, repo RepoRef, number int, label string) error {
	if strings.TrimSpace(label) == "" {
		return &InvalidInputError{Field: "label", Message: "must not be empty"}
	}
	names, err := g.resolveLabelNames(ctx, repo, []string{label})
	if err != nil {
		return err
	}

	err = g.do(ctx, forgeRequest{
		method:    http.MethodDelete,
		path:      githubIssuePath(repo, number) + "/labels/" + url.PathEscape(names[0]),
		operation: "RemoveIssueLabel",
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	issue, err := g.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	return verifyLabelRemoved(ForgeGitHub, repo, number, label, issue.Labels)
}

// resolveLabelNames maps label names onto the repository's own spelling of
// them, refusing a name it does not define.
//
// GitHub's label routes take names, so nothing needs translating — but its
// add and replace routes CREATE a label they do not recognise. Checking the
// vocabulary first is what keeps a typo out of the repository.
func (g *GitHubBackend) resolveLabelNames(ctx context.Context, repo RepoRef, names []string) ([]string, error) {
	if len(names) == 0 {
		return []string{}, nil
	}
	defined, err := g.listLabels(ctx, repo)
	if err != nil {
		return nil, err
	}
	known := make([]string, 0, len(defined))
	for _, label := range defined {
		known = append(known, label.Name)
	}
	return canonicalLabelNames(repo, known, names)
}

// listLabels reads every label the repository defines, following pages. The
// single reader behind Labels, EnsureLabels and resolveLabelNames.
func (g *GitHubBackend) listLabels(ctx context.Context, repo RepoRef) ([]githubLabel, error) {
	all := make([]githubLabel, 0, githubPageSize)
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListLabels",
		path:      repoPath(repo) + "/labels",
		maxPages:  maxLabelPages,
		exhausted: &IncompleteResponseError{
			Op: "ListLabels " + repo.String(),
			Missing: "end of the label listing within " +
				strconv.Itoa(maxLabelPages*githubPageSize) + " labels, the adapter's page ceiling",
		},
	}, func(page []githubLabel) bool {
		all = append(all, page...)
		return true
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// ListCollaborators implements ForgeCollaborators, paginated to the end.
//
// GitHub's listing carries no display name, so FullName is empty.
func (g *GitHubBackend) ListCollaborators(ctx context.Context, repo RepoRef) ([]ForgeUser, error) {
	users := make([]ForgeUser, 0, githubPageSize)
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListCollaborators",
		path:      repoPath(repo) + "/collaborators",
		maxPages:  maxCollaboratorPages,
		exhausted: &IncompleteResponseError{
			Op: "ListCollaborators " + repo.String(),
			Missing: "end of the collaborator listing within " +
				strconv.Itoa(maxCollaboratorPages*githubPageSize) + " accounts, the adapter's page ceiling",
		},
	}, func(page []githubUser) bool {
		for _, user := range page {
			users = append(users, ForgeUser{Login: user.Login, FullName: user.Name})
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return users, nil
}
