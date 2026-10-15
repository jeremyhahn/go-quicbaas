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
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Compile-time proof the adapter offers onboarding.
var _ ForgeOnboarding = (*GitLabBackend)(nil)

// Labels returns every label the project defines, colours as bare lowercase
// hex like the other adapters report them.
func (g *GitLabBackend) Labels(ctx context.Context, repo RepoRef) ([]ForgeLabel, error) {
	defined, err := g.listLabels(ctx, repo)
	if err != nil {
		return nil, err
	}
	labels := make([]ForgeLabel, 0, len(defined))
	for _, label := range defined {
		labels = append(labels, ForgeLabel{
			Name:        label.Name,
			Color:       strings.ToLower(strings.TrimPrefix(label.Color, "#")),
			Description: label.Description,
		})
	}
	return labels, nil
}

// EnsureLabels creates every spec the project does not already define,
// verified by read-back. Existing labels are left exactly as they are.
func (g *GitLabBackend) EnsureLabels(ctx context.Context, repo RepoRef, specs []LabelSpec) error {
	if len(specs) == 0 {
		return nil
	}
	for _, spec := range specs {
		if err := spec.Validate(); err != nil {
			return err
		}
	}

	defined, err := g.Labels(ctx, repo)
	if err != nil {
		return err
	}
	missing := missingLabels(defined, specs)
	if len(missing) == 0 {
		return nil
	}

	for _, spec := range missing {
		if err := g.do(ctx, forgeRequest{
			method: http.MethodPost,
			path:   gitlabProjectPath(repo) + "/labels",
			body: map[string]any{
				// GitLab takes a '#'-prefixed hex colour.
				"name":        spec.Name,
				"color":       "#" + spec.Color,
				"description": spec.Description,
			},
			operation: "CreateLabel",
		}); err != nil {
			return err
		}
	}

	after, err := g.Labels(ctx, repo)
	if err != nil {
		return err
	}
	return verifyLabelsDefined(ForgeGitLab, repo, after, specs)
}

// Webhooks returns the project's hooks, each with the event names its flags
// deliver. Never their tokens: GitLab does not return them.
func (g *GitLabBackend) Webhooks(ctx context.Context, repo RepoRef) ([]ForgeWebhook, error) {
	hooks, err := g.listHooks(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := make([]ForgeWebhook, 0, len(hooks))
	for i := range hooks {
		out = append(out, ForgeWebhook{
			ID:     gitlabID(hooks[i].ID),
			URL:    hooks[i].URL,
			Active: hooks[i].active(),
			Events: hooks[i].events(),
		})
	}
	return out, nil
}

// DeleteWebhook removes every hook delivering to hookURL, verified by
// read-back. Removing a hook that is not there is not an error.
func (g *GitLabBackend) DeleteWebhook(ctx context.Context, repo RepoRef, hookURL string) error {
	if strings.TrimSpace(hookURL) == "" {
		return &InvalidInputError{Field: "url", Message: "naming no delivery would delete every one of them"}
	}
	hooks, err := g.listHooks(ctx, repo)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	deleted := 0
	for _, hook := range hooks {
		if hook.URL != hookURL {
			continue
		}
		err := g.do(ctx, forgeRequest{
			method:    http.MethodDelete,
			path:      gitlabProjectPath(repo) + "/hooks/" + gitlabID(hook.ID),
			operation: "DeleteWebhook",
		})
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		deleted++
	}
	if deleted == 0 {
		return nil
	}

	after, err := g.listHooks(ctx, repo)
	if err != nil {
		return err
	}
	if gitlabHookTo(after, hookURL) != nil {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitLab, Operation: "DeleteWebhook", Subject: repo.String(),
			Want: "no delivery to " + hookURL,
			Got:  "a delivery to " + hookURL,
		}
	}
	return nil
}

// gitlabTreeEntry is one entry of a repository tree listing.
type gitlabTreeEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// RepoFiles lists the entries directly under dir at ref. An absent directory
// is an empty list.
func (g *GitLabBackend) RepoFiles(ctx context.Context, repo RepoRef, ref, dir string) ([]ForgeFile, error) {
	query := url.Values{}
	if trimmed := strings.Trim(strings.TrimSpace(dir), "/"); trimmed != "" {
		query.Set("path", trimmed)
	}
	if trimmed := strings.TrimSpace(ref); trimmed != "" {
		query.Set("ref", trimmed)
	}

	files := []ForgeFile{}
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListContents",
		path:      gitlabProjectPath(repo) + "/repository/tree",
		query:     query,
		maxPages:  maxLabelPages,
		exhausted: &IncompleteResponseError{
			Op:      "ListContents " + repo.String(),
			Missing: "entries beyond " + strconv.Itoa(maxLabelPages*gitlabPageSize),
		},
	}, func(page []gitlabTreeEntry) bool {
		for _, entry := range page {
			files = append(files, ForgeFile{
				Path:  entry.Path,
				Name:  entry.Name,
				IsDir: entry.Type == "tree",
			})
		}
		return true
	})
	if errors.Is(err, ErrNotFound) {
		return []ForgeFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	return files, nil
}

// BranchProtection refuses: a GitLab protected branch names no required
// status context. See Capabilities.
func (g *GitLabBackend) BranchProtection(_ context.Context, _ RepoRef, branch string) (*ForgeBranchProtection, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, &InvalidInputError{Field: "branch", Message: "a protection rule protects a named branch"}
	}
	return nil, &ForgeUnsupportedError{
		Type:       ForgeGitLab,
		Capability: "reading the status contexts protecting a branch (GitLab protects branches by access level only)",
	}
}
