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
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// =============================================================================
// GitHub: the onboarding surface
// =============================================================================

// Compile-time proof the onboarding plane is complete for this forge.
var _ ForgeOnboarding = (*GitHubBackend)(nil)

// Labels implements ForgeOnboarding.
func (g *GitHubBackend) Labels(ctx context.Context, repo RepoRef) ([]ForgeLabel, error) {
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

// EnsureLabels implements ForgeOnboarding: read, create only the difference,
// read back — for the reasons the Gitea adapter gives.
func (g *GitHubBackend) EnsureLabels(ctx context.Context, repo RepoRef, specs []LabelSpec) error {
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
			path:   repoPath(repo) + "/labels",
			body: map[string]any{
				// GitHub takes the bare hex and refuses a leading '#'.
				"name":        spec.Name,
				"color":       spec.Color,
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
	return verifyLabelsDefined(ForgeGitHub, repo, after, specs)
}

// Webhooks implements ForgeOnboarding.
func (g *GitHubBackend) Webhooks(ctx context.Context, repo RepoRef) ([]ForgeWebhook, error) {
	hooks, err := g.listHooks(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := make([]ForgeWebhook, 0, len(hooks))
	for _, hook := range hooks {
		out = append(out, ForgeWebhook{
			ID:     strconv.FormatInt(hook.ID, 10),
			URL:    hook.Config.URL,
			Active: hook.Active,
			Events: append([]string(nil), hook.Events...),
		})
	}
	return out, nil
}

// DeleteWebhook implements ForgeOnboarding, verified by read-back. Every
// delivery to hookURL is deleted, and a repository that is gone already holds
// none — see the Gitea adapter for both.
func (g *GitHubBackend) DeleteWebhook(ctx context.Context, repo RepoRef, hookURL string) error {
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
		if hook.Config.URL != hookURL {
			continue
		}
		err := g.do(ctx, forgeRequest{
			method:    http.MethodDelete,
			path:      repoPath(repo) + "/hooks/" + strconv.FormatInt(hook.ID, 10),
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
	if findHook(after, hookURL) != nil {
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitHub, Operation: "DeleteWebhook", Subject: repo.String(),
			Want: "no delivery to " + hookURL,
			Got:  "a delivery to " + hookURL,
		}
	}
	return nil
}

// githubContent is one entry of GitHub's contents listing.
type githubContent struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// RepoFiles implements ForgeOnboarding. Like Gitea's, GitHub's contents route
// answers with an array for a directory and an object for a file, and an
// absent path is the desired state rather than a fault.
func (g *GitHubBackend) RepoFiles(ctx context.Context, repo RepoRef, ref, dir string) ([]ForgeFile, error) {
	query := url.Values{}
	if trimmed := strings.TrimSpace(ref); trimmed != "" {
		query.Set("ref", trimmed)
	}
	path := repoPath(repo) + "/contents"
	if escaped := giteaContentsPath(dir); escaped != "" {
		path += "/" + escaped
	}

	var body json.RawMessage
	err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      path,
		query:     query,
		out:       &body,
		operation: "ListContents",
	})
	if errors.Is(err, ErrNotFound) {
		return []ForgeFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return []ForgeFile{}, nil
	}

	var entries []githubContent
	if err := json.Unmarshal(body, &entries); err != nil {
		var single githubContent
		if singleErr := json.Unmarshal(body, &single); singleErr != nil {
			return nil, backendError("ListContents.decode", singleErr)
		}
		entries = []githubContent{single}
	}

	files := make([]ForgeFile, 0, len(entries))
	for _, entry := range entries {
		files = append(files, ForgeFile{
			Path:  entry.Path,
			Name:  entry.Name,
			IsDir: entry.Type == "dir",
		})
	}
	return files, nil
}

// githubBranchProtection is GitHub's branch protection, narrowed to the
// required status checks.
type githubBranchProtection struct {
	RequiredStatusChecks *struct {
		Contexts []string `json:"contexts"`
		Checks   []struct {
			Context string `json:"context"`
		} `json:"checks"`
	} `json:"required_status_checks"`
}

// BranchProtection implements ForgeOnboarding. An unprotected branch is nil
// and no error; GitHub answers 404 for one.
//
// Status checks are required exactly when required_status_checks is present.
// GitHub reports each one twice over — the legacy contexts list and the
// checks list that adds an app — so the two are merged without duplicates.
func (g *GitHubBackend) BranchProtection(ctx context.Context, repo RepoRef, branch string) (*ForgeBranchProtection, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, &InvalidInputError{Field: "branch", Message: "a protection rule protects a named branch"}
	}
	var rule githubBranchProtection
	err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/branches/" + refPath(branch) + "/protection",
		out:       &rule,
		operation: "GetBranchProtection",
	})
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	protection := &ForgeBranchProtection{Branch: branch}
	checks := rule.RequiredStatusChecks
	if checks == nil {
		return protection, nil
	}
	protection.RequireStatusChecks = true
	seen := map[string]bool{}
	add := func(context string) {
		if context != "" && !seen[context] {
			seen[context] = true
			protection.RequiredContexts = append(protection.RequiredContexts, context)
		}
	}
	for _, context := range checks.Contexts {
		add(context)
	}
	for _, check := range checks.Checks {
		add(check.Context)
	}
	return protection, nil
}
