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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// GitLab: issues, notes, labels and members
// =============================================================================

type gitlabIssue struct {
	IID         int      `json:"iid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"`
	Labels      []string `json:"labels"`
	Milestone   *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	Author    *gitlabUser `json:"author"`
	Assignee  *gitlabUser `json:"assignee"`
	WebURL    string      `json:"web_url"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// gitlabStates maps the module's issue and pull request states onto GitLab's,
// which spells an open one "opened". Read in reverse by gitlabState.
var gitlabStates = map[string]string{
	"open":   "opened",
	"closed": "closed",
	"all":    "all",
}

// gitlabState renders GitLab's state of an issue or merge request in the
// module's vocabulary: "opened" is "open", and "locked" — an open discussion
// nobody may add to — is still open work.
func gitlabState(state string) string {
	switch state {
	case "opened", "locked":
		return "open"
	default:
		return state
	}
}

// toForgeIssue converts GitLab's shape to the module's.
func (i *gitlabIssue) toForgeIssue() *ForgeIssue {
	issue := &ForgeIssue{
		Number:  i.IID,
		Title:   i.Title,
		Body:    i.Description,
		State:   gitlabState(i.State),
		Labels:  i.Labels,
		URL:     i.WebURL,
		Created: i.CreatedAt,
		Updated: i.UpdatedAt,
	}
	if i.Milestone != nil {
		issue.Milestone = i.Milestone.Title
	}
	if i.Author != nil {
		issue.Author = i.Author.Username
	}
	if i.Assignee != nil {
		issue.Assignee = i.Assignee.Username
	}
	return issue
}

// gitlabIssuePath renders the API path of one issue.
func gitlabIssuePath(repo RepoRef, number int) string {
	return gitlabProjectPath(repo) + "/issues/" + strconv.Itoa(number)
}

// ListIssues returns issues matching filter. GitLab's issues listing holds
// issues only — merge requests are listed apart — so nothing is filtered out.
func (g *GitLabBackend) ListIssues(ctx context.Context, repo RepoRef, filter IssueFilter) ([]ForgeIssue, error) {
	state := filter.State
	if state == "" {
		state = "open"
	}
	gitlabStateName, ok := gitlabStates[state]
	if !ok {
		return nil, &InvalidInputError{Field: "state", Message: "an issue state is open, closed or all, not " + state}
	}
	query := url.Values{}
	query.Set("state", gitlabStateName)
	if len(filter.Labels) > 0 {
		query.Set("labels", strings.Join(filter.Labels, ","))
	}
	if !filter.Since.IsZero() {
		query.Set("updated_after", filter.Since.UTC().Format(time.RFC3339))
	}

	var issues []ForgeIssue
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListIssues",
		path:      gitlabProjectPath(repo) + "/issues",
		query:     query,
		maxPages:  maxIssuePages,
		exhausted: backendError("ListIssues", errIssuePagesExhausted),
	}, func(page []gitlabIssue) bool {
		for i := range page {
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
func (g *GitLabBackend) GetIssue(ctx context.Context, repo RepoRef, number int) (*ForgeIssue, error) {
	var raw gitlabIssue
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabIssuePath(repo, number),
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
// write: GitLab's create route invents any label it does not know, as
// GitHub's does.
func (g *GitLabBackend) CreateIssue(ctx context.Context, repo RepoRef, spec IssueSpec) (*ForgeIssue, error) {
	if strings.TrimSpace(spec.Title) == "" {
		return nil, &InvalidInputError{Field: "title", Message: "an issue needs a title"}
	}
	labels, err := g.resolveLabelNames(ctx, repo, spec.Labels)
	if err != nil {
		return nil, err
	}

	body := map[string]any{"title": spec.Title}
	if spec.Body != "" {
		body["description"] = spec.Body
	}
	if len(labels) > 0 {
		body["labels"] = strings.Join(labels, ",")
	}
	var raw gitlabIssue
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      gitlabProjectPath(repo) + "/issues",
		body:      body,
		out:       &raw,
		operation: "CreateIssue",
	}); err != nil {
		return nil, err
	}

	issue := raw.toForgeIssue()
	if raw.IID == 0 || raw.Title != spec.Title {
		return nil, &ForgeWriteNotAppliedError{
			Type: ForgeGitLab, Operation: "CreateIssue", Subject: repo.String(),
			Want: truncate(spec.Title, 80), Got: truncate(raw.Title, 80),
		}
	}
	if missing := missingIssueLabels(spec.Labels, issue.Labels); len(missing) > 0 {
		return nil, &ForgeWriteNotAppliedError{
			Type: ForgeGitLab, Operation: "CreateIssue", Subject: repo.String() + "#" + strconv.Itoa(raw.IID),
			Want: "labels " + strings.Join(spec.Labels, ","), Got: "labels " + strings.Join(issue.Labels, ","),
		}
	}
	return issue, nil
}

// CommentOnIssue posts a note and returns it as GitLab stored it; the response
// is the read-back.
func (g *GitLabBackend) CommentOnIssue(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error) {
	return g.postNote(ctx, "CommentOnIssue", gitlabIssuePath(repo, number), fmt.Sprintf("%s#%d", repo, number), body)
}

// postNote posts a note on the issue or merge request at path, verified by
// the echoed body. subject names the object in a refusal.
func (g *GitLabBackend) postNote(ctx context.Context, operation, path, subject, body string) (*ForgeComment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, &InvalidInputError{Field: "body", Message: "a comment must say something"}
	}

	var raw gitlabNote
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      path + "/notes",
		body:      map[string]string{"body": body},
		out:       &raw,
		operation: operation,
	}); err != nil {
		return nil, err
	}

	if raw.ID == 0 || raw.Body != body {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: operation,
			Subject:   subject,
			Want:      truncate(body, 80),
			Got:       truncate(raw.Body, 80),
		}
	}
	comment := raw.toForgeComment()
	return &comment, nil
}

// CloseIssue closes an issue, verified by the response's state. Closing a
// closed issue is accepted by GitLab and answers its closed state.
func (g *GitLabBackend) CloseIssue(ctx context.Context, repo RepoRef, number int) error {
	var raw gitlabIssue
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPut,
		path:      gitlabIssuePath(repo, number),
		body:      map[string]string{"state_event": "close"},
		out:       &raw,
		operation: "CloseIssue",
	}); err != nil {
		return err
	}
	if raw.State != "closed" {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitLab,
			Operation: "CloseIssue",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "closed",
			Got:       raw.State,
		}
	}
	return nil
}

// ListIssueComments returns an issue's discussion, oldest first, paged to the
// end.
//
// GitLab lists its own system notes — "added label", "changed the
// description" — beside what people wrote. They are the forge narrating, not
// the discussion, and are dropped.
func (g *GitLabBackend) ListIssueComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error) {
	notes, err := g.listNotes(ctx, "ListIssueComments", gitlabIssuePath(repo, number), errCommentPagesExhausted)
	if err != nil {
		return nil, err
	}
	return gitlabComments(notes), nil
}

// listNotes reads every note on the issue or merge request at path, oldest
// first, GitLab's own system notes included.
func (g *GitLabBackend) listNotes(ctx context.Context, operation, path string, exhausted error) ([]gitlabNote, error) {
	query := url.Values{}
	query.Set("sort", "asc")
	query.Set("order_by", "created_at")
	var notes []gitlabNote
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: operation,
		path:      path + "/notes",
		query:     query,
		maxPages:  maxIssuePages,
		exhausted: backendError(operation, exhausted),
	}, func(page []gitlabNote) bool {
		notes = append(notes, page...)
		return true
	})
	if err != nil {
		return nil, err
	}
	return notes, nil
}

// gitlabComments returns the notes people wrote, as comments. System notes
// are GitLab's record of events — a label added, a commit pushed — not
// anything a person said.
func gitlabComments(notes []gitlabNote) []ForgeComment {
	var comments []ForgeComment
	for i := range notes {
		if notes[i].System {
			continue
		}
		comments = append(comments, notes[i].toForgeComment())
	}
	return comments
}

// SetIssueLabels replaces an issue's labels and verifies the result by reading
// the issue back. Labels the repository does not define are refused before
// the write, for the reason CreateIssue gives.
func (g *GitLabBackend) SetIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	names, err := g.resolveLabelNames(ctx, repo, labels)
	if err != nil {
		return err
	}
	if err := g.updateLabels(ctx, gitlabIssuePath(repo, number), "SetIssueLabels", "labels", names); err != nil {
		return err
	}
	current, err := g.issueLabels(ctx, repo, number)
	if err != nil {
		return err
	}
	return verifyLabelsSet(ForgeGitLab, repo, number, labels, current)
}

// AddIssueLabels adds labels to an issue; see addLabels.
func (g *GitLabBackend) AddIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	return g.addLabels(ctx, repo, number, labels, gitlabLabelled{
		operation: "AddIssueLabels",
		path:      gitlabIssuePath(repo, number),
		read:      g.issueLabels,
	})
}

// RemoveIssueLabel removes one label from an issue; see removeLabel.
func (g *GitLabBackend) RemoveIssueLabel(ctx context.Context, repo RepoRef, number int, label string) error {
	return g.removeLabel(ctx, repo, number, label, gitlabLabelled{
		operation: "RemoveIssueLabel",
		path:      gitlabIssuePath(repo, number),
		read:      g.issueLabels,
	})
}

// gitlabLabelled is one object GitLab labels — an issue or a merge request,
// which GitLab numbers apart — as the label edits address it: the operation
// name, the route the edit is sent to, and the read that verifies it.
type gitlabLabelled struct {
	operation string
	path      string
	read      func(ctx context.Context, repo RepoRef, number int) ([]string, error)
}

// addLabels adds labels with add_labels, leaving the others alone, and
// verifies every requested label is present.
func (g *GitLabBackend) addLabels(ctx context.Context, repo RepoRef, number int, labels []string, target gitlabLabelled) error {
	if len(labels) == 0 {
		return nil
	}
	names, err := g.resolveLabelNames(ctx, repo, labels)
	if err != nil {
		return err
	}
	if err := g.updateLabels(ctx, target.path, target.operation, "add_labels", names); err != nil {
		return err
	}
	current, err := target.read(ctx, repo, number)
	if err != nil {
		return err
	}
	return verifyLabelsAdded(ForgeGitLab, repo, number, labels, current)
}

// removeLabel removes one label with remove_labels and verifies it is gone.
// Removing a label the object does not carry changes nothing, which is the
// state asked for.
func (g *GitLabBackend) removeLabel(ctx context.Context, repo RepoRef, number int, label string, target gitlabLabelled) error {
	if strings.TrimSpace(label) == "" {
		return &InvalidInputError{Field: "label", Message: "must not be empty"}
	}
	names, err := g.resolveLabelNames(ctx, repo, []string{label})
	if err != nil {
		return err
	}
	if err := g.updateLabels(ctx, target.path, target.operation, "remove_labels", names); err != nil {
		return err
	}
	current, err := target.read(ctx, repo, number)
	if err != nil {
		return err
	}
	return verifyLabelRemoved(ForgeGitLab, repo, number, label, current)
}

// issueLabels reads an issue's labels back.
func (g *GitLabBackend) issueLabels(ctx context.Context, repo RepoRef, number int) ([]string, error) {
	issue, err := g.GetIssue(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	return issue.Labels, nil
}

// updateLabels sends one label edit to path: field is labels (replace),
// add_labels or remove_labels, each a comma-separated list.
func (g *GitLabBackend) updateLabels(ctx context.Context, path, operation, field string, names []string) error {
	return g.do(ctx, forgeRequest{
		method:    http.MethodPut,
		path:      path,
		body:      map[string]string{field: strings.Join(names, ",")},
		operation: operation,
	})
}

// resolveLabelNames maps label names onto the repository's own spelling of
// them, refusing a name it does not define; see canonicalLabelNames.
func (g *GitLabBackend) resolveLabelNames(ctx context.Context, repo RepoRef, names []string) ([]string, error) {
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

// listLabels reads every label the project can use, following pages. The
// single reader behind Labels, EnsureLabels and resolveLabelNames.
//
// GitLab lists group labels beside the project's own by default, and a group
// label is as usable on an issue as a project label, so both are read.
func (g *GitLabBackend) listLabels(ctx context.Context, repo RepoRef) ([]gitlabLabel, error) {
	all := make([]gitlabLabel, 0, gitlabPageSize)
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListLabels",
		path:      gitlabProjectPath(repo) + "/labels",
		maxPages:  maxLabelPages,
		exhausted: &IncompleteResponseError{
			Op: "ListLabels " + repo.String(),
			Missing: "end of the label listing within " +
				strconv.Itoa(maxLabelPages*gitlabPageSize) + " labels, the adapter's page ceiling",
		},
	}, func(page []gitlabLabel) bool {
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
// It reads /members/all, which includes the members a project inherits from
// its groups: they hold the same access as direct members.
func (g *GitLabBackend) ListCollaborators(ctx context.Context, repo RepoRef) ([]ForgeUser, error) {
	users := make([]ForgeUser, 0, gitlabPageSize)
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListCollaborators",
		path:      gitlabProjectPath(repo) + "/members/all",
		maxPages:  maxCollaboratorPages,
		exhausted: &IncompleteResponseError{
			Op: "ListCollaborators " + repo.String(),
			Missing: "end of the collaborator listing within " +
				strconv.Itoa(maxCollaboratorPages*gitlabPageSize) + " accounts, the adapter's page ceiling",
		},
	}, func(page []gitlabUser) bool {
		for _, user := range page {
			users = append(users, ForgeUser{Login: user.Username, FullName: user.Name})
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return users, nil
}
