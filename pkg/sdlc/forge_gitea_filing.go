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
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxCollaboratorPages bounds the collaborator listing. A repository granting
// more than maxCollaboratorPages*giteaDefaultPageSize accounts is reported as
// an incomplete listing rather than returned short.
const maxCollaboratorPages = 20

// giteaCreateIssueBody is Gitea's create-issue request. Labels are IDs, not
// names: Gitea's endpoint takes nothing else.
type giteaCreateIssueBody struct {
	Title  string  `json:"title"`
	Body   string  `json:"body,omitempty"`
	Labels []int64 `json:"labels,omitempty"`
}

// giteaCollaborator is one entry of Gitea's collaborator listing.
type giteaCollaborator struct {
	Login    string `json:"login"`
	FullName string `json:"full_name"`
}

// CreateIssue implements ForgeBackend.
//
// The labels are resolved to IDs BEFORE the write, so a label the repository
// does not define is refused with its name and nothing is filed. The response
// is the read-back: Gitea echoes the created issue, and one that came back
// without a number, with a different title or short of a requested label did
// not land as asked.
func (g *GiteaBackend) CreateIssue(ctx context.Context, repo RepoRef, spec IssueSpec) (*ForgeIssue, error) {
	if strings.TrimSpace(spec.Title) == "" {
		return nil, &InvalidInputError{Field: "title", Message: "an issue needs a title"}
	}
	ids, err := g.resolveLabelIDs(ctx, repo, spec.Labels)
	if err != nil {
		return nil, err
	}

	var raw giteaIssue
	if err := g.do(ctx, giteaRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/issues",
		body:      giteaCreateIssueBody{Title: spec.Title, Body: spec.Body, Labels: ids},
		out:       &raw,
		operation: "CreateIssue",
	}); err != nil {
		return nil, err
	}

	issue := raw.toForgeIssue()
	if raw.Number == 0 || raw.Title != spec.Title {
		return nil, &ForgeWriteNotAppliedError{
			Type: ForgeGitea, Operation: "CreateIssue", Subject: repo.String(),
			Want: truncate(spec.Title, 80), Got: truncate(raw.Title, 80),
		}
	}
	if missing := missingIssueLabels(spec.Labels, issue.Labels); len(missing) > 0 {
		return nil, &ForgeWriteNotAppliedError{
			Type: ForgeGitea, Operation: "CreateIssue", Subject: repo.String() + "#" + strconv.Itoa(raw.Number),
			Want: "labels " + strings.Join(spec.Labels, ","), Got: "labels " + strings.Join(issue.Labels, ","),
		}
	}
	return issue, nil
}

// missingIssueLabels returns the wanted labels an issue does not carry,
// compared case-insensitively as forges compare label names.
func missingIssueLabels(want, have []string) []string {
	carried := make(map[string]struct{}, len(have))
	for _, label := range have {
		carried[strings.ToLower(label)] = struct{}{}
	}
	var missing []string
	for _, label := range want {
		if _, ok := carried[strings.ToLower(label)]; !ok {
			missing = append(missing, label)
		}
	}
	return missing
}

// ListCollaborators implements ForgeCollaborators, paginated to the end.
func (g *GiteaBackend) ListCollaborators(ctx context.Context, repo RepoRef) ([]ForgeUser, error) {
	users := make([]ForgeUser, 0, giteaDefaultPageSize)
	for page := 1; page <= maxCollaboratorPages; page++ {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(giteaDefaultPageSize))
		query.Set("page", strconv.Itoa(page))

		var listed []giteaCollaborator
		if err := g.do(ctx, giteaRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/collaborators",
			query:     query,
			out:       &listed,
			operation: "ListCollaborators",
		}); err != nil {
			return nil, err
		}
		for _, user := range listed {
			users = append(users, ForgeUser(user))
		}
		if len(listed) < giteaDefaultPageSize {
			return users, nil
		}
	}
	return nil, &IncompleteResponseError{
		Op: "ListCollaborators " + repo.String(),
		Missing: "end of the collaborator listing within " +
			strconv.Itoa(maxCollaboratorPages*giteaDefaultPageSize) + " accounts, the adapter's page ceiling",
	}
}
