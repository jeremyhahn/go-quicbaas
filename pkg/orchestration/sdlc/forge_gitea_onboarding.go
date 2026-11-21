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
// Gitea: the onboarding surface
// =============================================================================
//
// Every write here follows the file's standing rule — read back and compare,
// because Gitea has documented cases of answering 200 for a write that did not
// happen, the label routes among them.

// Compile-time proof the onboarding plane is complete for this forge.
var _ ForgeOnboarding = (*GiteaBackend)(nil)

// =============================================================================
// Labels
// =============================================================================

// listLabels reads every label the repository defines, following pages.
//
// One reader for the whole adapter: EnsureLabels, Labels and resolveLabelIDs
// all go through it, so the three can never disagree about which labels exist
// or about how far the listing got.
func (g *GiteaBackend) listLabels(ctx context.Context, repo RepoRef) ([]giteaLabel, error) {
	all := make([]giteaLabel, 0, giteaDefaultPageSize)
	for page := 1; page <= maxLabelPages; page++ {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(giteaDefaultPageSize))
		query.Set("page", strconv.Itoa(page))

		var defined []giteaLabel
		if err := g.do(ctx, forgeRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/labels",
			query:     query,
			out:       &defined,
			operation: "ListLabels",
		}); err != nil {
			return nil, err
		}
		all = append(all, defined...)
		if len(defined) < giteaDefaultPageSize {
			return all, nil
		}
	}
	// The page ceiling was reached with a full last page, so there is more the
	// listing has not seen. Reported rather than returned short, for the reason
	// IssueFilter.Limit documents: a short list with a nil error is
	// indistinguishable from a complete one, and a caller would read a label it
	// defines as missing and try to create a duplicate.
	return nil, &IncompleteResponseError{
		Op: "ListLabels " + repo.String(),
		Missing: "end of the label listing within " +
			strconv.Itoa(maxLabelPages*giteaDefaultPageSize) + " labels, the adapter's page ceiling",
	}
}

// Labels implements ForgeOnboarding.
func (g *GiteaBackend) Labels(ctx context.Context, repo RepoRef) ([]ForgeLabel, error) {
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

// EnsureLabels implements ForgeOnboarding: it creates what is missing and
// touches nothing else.
//
// READ, THEN CREATE ONLY THE DIFFERENCE, THEN READ BACK. Creating
// unconditionally and treating the duplicate-name rejection as success would
// be shorter and would hide the case that matters — a repository that already
// carries the label under a different colour, which must be left alone rather
// than reported as created.
func (g *GiteaBackend) EnsureLabels(ctx context.Context, repo RepoRef, specs []LabelSpec) error {
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
				// The leading '#' is what Gitea's own colour validator
				// accepts in every version the adapter targets; LabelSpec
				// normalizes it away so the module compares on the bare hex.
				"name":        spec.Name,
				"color":       "#" + spec.Color,
				"description": spec.Description,
			},
			operation: "CreateLabel",
		}); err != nil {
			return err
		}
	}

	// Read-back over the whole vocabulary rather than per creation: one call
	// proves every label landed, and proves it against the same listing the
	// conformance pass will read.
	after, err := g.Labels(ctx, repo)
	if err != nil {
		return err
	}
	if stillMissing := missingLabels(after, specs); len(stillMissing) > 0 {
		names := make([]string, 0, len(stillMissing))
		for _, spec := range stillMissing {
			names = append(names, spec.Name)
		}
		return &ForgeWriteNotAppliedError{
			Type: ForgeGitea, Operation: "EnsureLabels", Subject: repo.String(),
			Want: "labels " + strings.Join(names, " "),
			Got:  "[" + strings.Join(labelNames(after), " ") + "]",
		}
	}
	return nil
}

// =============================================================================
// Webhooks
// =============================================================================

// Webhooks implements ForgeOnboarding.
//
// Gitea's hook listing is not paginated in any version the adapter targets —
// a repository holds a handful of deliveries, not thousands — so this is one
// call rather than a page loop.
func (g *GiteaBackend) Webhooks(ctx context.Context, repo RepoRef) ([]ForgeWebhook, error) {
	hooks, err := g.listHooks(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := make([]ForgeWebhook, 0, len(hooks))
	for _, hook := range hooks {
		out = append(out, ForgeWebhook{
			ID:     strconv.FormatInt(hook.ID, 10),
			URL:    hook.Config["url"],
			Active: hook.Active,
			Events: append([]string(nil), hook.Events...),
		})
	}
	return out, nil
}

// listHooks reads the repository's configured deliveries.
func (g *GiteaBackend) listHooks(ctx context.Context, repo RepoRef) ([]giteaHook, error) {
	var hooks []giteaHook
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/hooks",
		out:       &hooks,
		operation: "ListWebhooks",
	}); err != nil {
		return nil, err
	}
	return hooks, nil
}

// DeleteWebhook implements ForgeOnboarding, verified by read-back.
func (g *GiteaBackend) DeleteWebhook(ctx context.Context, repo RepoRef, hookURL string) error {
	if strings.TrimSpace(hookURL) == "" {
		return &InvalidInputError{Field: "url", Message: "naming no delivery would delete every one of them"}
	}
	hooks, err := g.listHooks(ctx, repo)
	if err != nil {
		// A REPOSITORY THAT IS NOT THERE HOLDS NO DELIVERY, so offboarding it
		// has already succeeded. Reporting a fault here would trap the
		// operator: unbinding refuses until the webhook is gone, and a
		// repository somebody deleted on the forge could then never be
		// unbound at all. A forge that is merely DOWN answers 5xx and is
		// still refused, which is the case worth retrying.
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}

	// EVERY match, not the first. A repository that somehow carries two
	// deliveries to the same URL — an earlier onboarding against a forge that
	// duplicated on retry — must come out of this call with none, or the
	// offboarded project keeps receiving deliveries nobody can explain.
	deleted := 0
	for _, hook := range hooks {
		if hook.Config["url"] != hookURL {
			continue
		}
		if err := g.do(ctx, forgeRequest{
			method:    http.MethodDelete,
			path:      repoPath(repo) + "/hooks/" + strconv.FormatInt(hook.ID, 10),
			operation: "DeleteWebhook",
		}); err != nil {
			// A delivery another actor removed between the listing and the
			// delete is the desired state, reached by someone else.
			if errors.Is(err, ErrNotFound) {
				continue
			}
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
	for _, hook := range after {
		if hook.Config["url"] == hookURL {
			return &ForgeWriteNotAppliedError{
				Type: ForgeGitea, Operation: "DeleteWebhook", Subject: repo.String(),
				Want: "no delivery to " + hookURL,
				Got:  "a delivery to " + hookURL,
			}
		}
	}
	return nil
}

// =============================================================================
// Repository tree
// =============================================================================

// giteaContent is one entry of Gitea's contents listing.
type giteaContent struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// RepoFiles implements ForgeOnboarding.
//
// Gitea's contents route answers with an ARRAY for a directory and an OBJECT
// for a file, so the body is decoded in two steps. A caller that named a file
// gets that one entry back rather than an error: "is this path present" is the
// question the stale-workflow check asks, and answering it differently
// depending on whether the operator named a directory or a file would make the
// check's own configuration a trap.
func (g *GiteaBackend) RepoFiles(ctx context.Context, repo RepoRef, ref, dir string) ([]ForgeFile, error) {
	query := url.Values{}
	if trimmed := strings.TrimSpace(ref); trimmed != "" {
		query.Set("ref", trimmed)
	}

	var body json.RawMessage
	err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/contents/" + giteaContentsPath(dir),
		query:     query,
		out:       &body,
		operation: "ListContents",
	})
	if err != nil {
		// AN ABSENT DIRECTORY IS THE DESIRED STATE, NOT A FAULT. The whole
		// point of the stale-workflow check is that .github/workflows should
		// eventually not exist at all, and a repository that reached that
		// state must not be the one the check errors on.
		if errors.Is(err, ErrNotFound) {
			return []ForgeFile{}, nil
		}
		return nil, err
	}
	if len(body) == 0 {
		return []ForgeFile{}, nil
	}

	var entries []giteaContent
	if err := json.Unmarshal(body, &entries); err != nil {
		var single giteaContent
		if singleErr := json.Unmarshal(body, &single); singleErr != nil {
			return nil, backendError("ListContents.decode", singleErr)
		}
		entries = []giteaContent{single}
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

// giteaContentsPath escapes a repository path segment by segment.
//
// url.PathEscape on the whole path would turn its separators into %2F and
// address a file whose name contains slashes, which no repository has.
func giteaContentsPath(dir string) string {
	trimmed := strings.Trim(strings.TrimSpace(dir), "/")
	if trimmed == "" {
		return ""
	}
	segments := strings.Split(trimmed, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return strings.Join(escaped, "/")
}

// =============================================================================
// Branch protection
// =============================================================================

// giteaBranchProtection is Gitea's protected-branch rule, narrowed to the
// parts an orchestrator's correctness depends on.
type giteaBranchProtection struct {
	BranchName          string   `json:"branch_name"`
	RuleName            string   `json:"rule_name"`
	EnableStatusCheck   bool     `json:"enable_status_check"`
	StatusCheckContexts []string `json:"status_check_contexts"`
}

// BranchProtection implements ForgeOnboarding. An unprotected branch is nil
// and no error.
func (g *GiteaBackend) BranchProtection(ctx context.Context, repo RepoRef, branch string) (*ForgeBranchProtection, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, &InvalidInputError{Field: "branch", Message: "a protection rule protects a named branch"}
	}
	var rule giteaBranchProtection
	err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/branch_protections/" + url.PathEscape(branch),
		out:       &rule,
		operation: "GetBranchProtection",
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	name := rule.BranchName
	if name == "" {
		name = rule.RuleName
	}
	if name == "" {
		name = branch
	}
	return &ForgeBranchProtection{
		Branch:              name,
		RequireStatusChecks: rule.EnableStatusCheck,
		RequiredContexts:    append([]string(nil), rule.StatusCheckContexts...),
	}, nil
}
