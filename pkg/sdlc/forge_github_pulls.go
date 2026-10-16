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
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// GitHub: branches, tags and pull requests
// =============================================================================

// maxTagPeels bounds how many annotated-tag objects GetTag follows to reach a
// commit. A tag of a tag is legal git and rare; a chain longer than this is a
// repository nobody should be releasing from.
const maxTagPeels = 4

// errGitHubTagChain reports a tag that did not resolve to a commit within
// maxTagPeels hops.
var errGitHubTagChain = errors.New("tag does not resolve to a commit")

// githubRef is a git reference as GitHub's git database API reports it.
type githubRef struct {
	Ref    string `json:"ref"`
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"object"`
}

type githubBranch struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// refPath escapes a ref name segment by segment: GitHub addresses a branch
// such as "issue/617-x" with its slashes intact.
func refPath(name string) string {
	return giteaContentsPath(name)
}

// GetTag returns one tag, peeled to the commit it names.
//
// An annotated tag's ref points at a tag object rather than a commit, and a
// release check comparing that SHA to a commit would never match.
func (g *GitHubBackend) GetTag(ctx context.Context, repo RepoRef, tag string) (*ForgeTag, error) {
	if strings.TrimSpace(tag) == "" {
		return nil, &InvalidInputError{Field: "tag", Message: "a tag read names the tag"}
	}
	var ref githubRef
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/git/ref/tags/" + refPath(tag),
		out:       &ref,
		operation: "GetTag",
	}); err != nil {
		return nil, err
	}
	object := ref.Object
	for peel := 0; object.Type == "tag"; peel++ {
		if peel == maxTagPeels {
			return nil, backendError("GetTag", errGitHubTagChain)
		}
		var annotated struct {
			Object struct {
				SHA  string `json:"sha"`
				Type string `json:"type"`
			} `json:"object"`
		}
		if err := g.do(ctx, forgeRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/git/tags/" + url.PathEscape(object.SHA),
			out:       &annotated,
			operation: "GetTag",
		}); err != nil {
			return nil, err
		}
		object = annotated.Object
	}
	return &ForgeTag{Name: tag, SHA: object.SHA}, nil
}

// CreateBranch creates a branch at fromRef and verifies it by reading it back.
//
// GitHub creates a ref from a SHA, not from a ref name, so fromRef is resolved
// to its commit first; a fromRef that names nothing is a 404 there.
func (g *GitHubBackend) CreateBranch(ctx context.Context, repo RepoRef, name, fromRef string) (*ForgeBranch, error) {
	if name == "" {
		return nil, &InvalidInputError{Field: "name", Message: "a branch must be named"}
	}
	if fromRef == "" {
		return nil, &InvalidInputError{Field: "from_ref", Message: "a branch must be cut from somewhere"}
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/commits/" + url.PathEscape(fromRef),
		out:       &commit,
		operation: "ResolveRef",
	}); err != nil {
		return nil, err
	}
	if commit.SHA == "" {
		return nil, &IncompleteResponseError{Op: "ResolveRef " + repo.String() + " " + fromRef, Missing: "sha"}
	}

	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/git/refs",
		body:      map[string]string{"ref": "refs/heads/" + name, "sha": commit.SHA},
		operation: "CreateBranch",
	}); err != nil {
		return nil, err
	}

	branch, err := g.GetBranch(ctx, repo, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, &ForgeWriteNotAppliedError{
				Type:      ForgeGitHub,
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
func (g *GitHubBackend) GetBranch(ctx context.Context, repo RepoRef, name string) (*ForgeBranch, error) {
	var raw githubBranch
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/branches/" + refPath(name),
		out:       &raw,
		operation: "GetBranch",
	}); err != nil {
		return nil, err
	}
	return &ForgeBranch{Name: raw.Name, SHA: raw.Commit.SHA}, nil
}

// DeleteBranch removes a branch and verifies its absence. An already-absent
// branch succeeds: GitHub answers 422 "Reference does not exist" for it, and
// 404 when the repository's settings already deleted it on merge.
func (g *GitHubBackend) DeleteBranch(ctx context.Context, repo RepoRef, name string) error {
	err := g.do(ctx, forgeRequest{
		method:    http.MethodDelete,
		path:      repoPath(repo) + "/git/refs/heads/" + refPath(name),
		operation: "DeleteBranch",
	})
	if err != nil && !errors.Is(err, ErrNotFound) && forgeStatusOf(err) != http.StatusUnprocessableEntity {
		return err
	}

	if _, err := g.GetBranch(ctx, repo, name); err == nil {
		return &ForgeWriteNotAppliedError{
			Type:      ForgeGitHub,
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

type githubPull struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Head   struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			Name  string      `json:"name"`
			Owner *githubUser `json:"owner"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	User     *githubUser `json:"user"`
	Draft    bool        `json:"draft"`
	Merged   bool        `json:"merged"`
	MergedAt *time.Time  `json:"merged_at"`
	MergeSHA string      `json:"merge_commit_sha"`

	// Mergeable is null while GitHub is still computing it.
	Mergeable *bool         `json:"mergeable"`
	HTMLURL   string        `json:"html_url"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Labels    []githubLabel `json:"labels"`
}

// toForgePullRequest converts GitHub's shape to the module's.
//
// Two fields need care. The listing route omits "merged" and carries only
// merged_at, so either marks a merge. And GitHub fills merge_commit_sha on an
// OPEN pull request with its test merge, so the SHA is reported only once the
// pull request is merged — otherwise mergeLanded would read an open pull
// request as landed.
func (p *githubPull) toForgePullRequest() *ForgePullRequest {
	var headRepo RepoRef
	if p.Head.Repo != nil && p.Head.Repo.Owner != nil {
		headRepo = RepoRef{Owner: p.Head.Repo.Owner.Login, Repo: p.Head.Repo.Name}
	}
	var author string
	if p.User != nil {
		author = p.User.Login
	}
	merged := p.Merged || p.MergedAt != nil
	var mergeSHA string
	if merged {
		mergeSHA = p.MergeSHA
	}
	pull := &ForgePullRequest{
		HeadRepo:  headRepo,
		Author:    author,
		Number:    p.Number,
		Title:     p.Title,
		Body:      p.Body,
		State:     p.State,
		HeadRef:   p.Head.Ref,
		HeadSHA:   p.Head.SHA,
		BaseRef:   p.Base.Ref,
		Draft:     p.Draft,
		Merged:    merged,
		MergeSHA:  mergeSHA,
		Mergeable: p.Mergeable != nil && *p.Mergeable,
		URL:       p.HTMLURL,
		Created:   p.CreatedAt,
		Updated:   p.UpdatedAt,
	}
	for _, label := range p.Labels {
		pull.Labels = append(pull.Labels, label.Name)
	}
	return pull
}

// pullPath renders the API path of one pull request.
func pullPath(repo RepoRef, number int) string {
	return repoPath(repo) + "/pulls/" + strconv.Itoa(number)
}

// ListOpenPullRequests returns every open pull request on the repository.
func (g *GitHubBackend) ListOpenPullRequests(ctx context.Context, repo RepoRef) ([]ForgePullRequest, error) {
	query := url.Values{}
	query.Set("state", "open")
	var pulls []ForgePullRequest
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListOpenPullRequests",
		path:      repoPath(repo) + "/pulls",
		query:     query,
		maxPages:  maxIssuePages,
		exhausted: backendError("ListOpenPullRequests", errOpenPullPagesExhausted),
	}, func(page []githubPull) bool {
		for i := range page {
			pulls = append(pulls, *page[i].toForgePullRequest())
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return pulls, nil
}

// CreatePullRequest opens a pull request — or adopts the one already open for
// the same head -> base — and verifies it by reading it back. See the Gitea
// adapter and openPullRequest for why adoption is the idempotency.
func (g *GitHubBackend) CreatePullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec) (*ForgePullRequest, error) {
	switch {
	case spec.Title == "":
		return nil, &InvalidInputError{Field: "title", Message: "a pull request must be titled"}
	case spec.HeadRef == "":
		return nil, &InvalidInputError{Field: "head_ref", Message: "a pull request must name its source branch"}
	case spec.BaseRef == "":
		return nil, &InvalidInputError{Field: "base_ref", Message: "a pull request must name its target branch"}
	}

	number, err := g.openPullRequest(ctx, g, repo, spec, func(ctx context.Context) (int, error) {
		return g.postPullRequest(ctx, repo, spec)
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
			Type:      ForgeGitHub,
			Operation: "CreatePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "draft",
			Got:       "ready for review",
		}
	}
	if pull.HeadRef != spec.HeadRef || pull.BaseRef != spec.BaseRef {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitHub,
			Operation: "CreatePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      spec.HeadRef + " -> " + spec.BaseRef,
			Got:       pull.HeadRef + " -> " + pull.BaseRef,
		}
	}

	if len(spec.Labels) > 0 {
		if err := g.SetIssueLabels(ctx, repo, number, spec.Labels); err != nil {
			return nil, err
		}
		pull, err = g.GetPullRequest(ctx, repo, number)
		if err != nil {
			return nil, err
		}
	}
	return pull, nil
}

// postPullRequest sends one create and returns the number GitHub assigned.
// Unlike Gitea, GitHub takes draft as a field of the create.
func (g *GitHubBackend) postPullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec) (int, error) {
	var created githubPull
	if err := g.do(ctx, forgeRequest{
		method: http.MethodPost,
		path:   repoPath(repo) + "/pulls",
		body: map[string]any{
			"title": spec.Title,
			"body":  spec.Body,
			"head":  spec.HeadRef,
			"base":  spec.BaseRef,
			"draft": spec.Draft,
		},
		out:       &created,
		operation: "CreatePullRequest",
	}); err != nil {
		return 0, err
	}
	if created.Number == 0 {
		return 0, &ForgeWriteNotAppliedError{
			Type:      ForgeGitHub,
			Operation: "CreatePullRequest",
			Subject:   repo.String() + " " + spec.HeadRef + " -> " + spec.BaseRef,
			Want:      "a pull request number",
			Got:       "none",
		}
	}
	return created.Number, nil
}

// GetPullRequest returns one pull request.
func (g *GitHubBackend) GetPullRequest(ctx context.Context, repo RepoRef, number int) (*ForgePullRequest, error) {
	var raw githubPull
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      pullPath(repo, number),
		out:       &raw,
		operation: "GetPullRequest",
	}); err != nil {
		return nil, err
	}
	return raw.toForgePullRequest(), nil
}

// ClosePullRequest closes a pull request without merging it, verified by
// read-back. A pull request that is already closed or merged is left as it is.
func (g *GitHubBackend) ClosePullRequest(ctx context.Context, repo RepoRef, number int) error {
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
		method:    http.MethodPatch,
		path:      pullPath(repo, number),
		body:      map[string]string{"state": "closed"},
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
			Type:      ForgeGitHub,
			Operation: "ClosePullRequest",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      "closed",
			Got:       "state " + closed.State,
		}
	}
	return nil
}

// githubMergeMethods maps a MergeStrategy's forge-neutral method name onto
// GitHub's merge_method. Anything absent is refused rather than substituted.
var githubMergeMethods = map[string]string{
	"squash": "squash",
	"merge":  "merge",
	"rebase": "rebase",
}

// MergePullRequest merges according to spec, refusing when the head has moved
// past the tree the caller approved.
//
// expectedHead is sent as "sha", which makes GITHUB refuse the merge with 409
// if the branch moved — the authoritative guard, decided where the race is.
// That refusal becomes HeadMovedError and is never retried. The read before it
// only fails early with both commits named. A 405 ("not mergeable") is
// ForgeNotReadyError, which the caller's bounded merge retry handles.
func (g *GitHubBackend) MergePullRequest(ctx context.Context, repo RepoRef, number int, spec MergeSpec, expectedHead string) error {
	method, ok := githubMergeMethods[strings.ToLower(strings.TrimSpace(spec.Method))]
	if !ok {
		return &ForgeUnsupportedError{Type: ForgeGitHub, Capability: "merge method " + spec.Method}
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

	body := map[string]any{
		"merge_method": method,
		"sha":          expectedHead,
	}
	if spec.CommitTitle != "" {
		body["commit_title"] = spec.CommitTitle
	}
	if spec.CommitMessage != "" {
		body["commit_message"] = spec.CommitMessage
	}
	err = g.do(ctx, forgeRequest{
		method:    http.MethodPut,
		path:      pullPath(repo, number) + "/merge",
		body:      body,
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
			Type:      ForgeGitHub,
			Operation: "MergePullRequest",
			Subject:   subject,
			Want:      "merged",
			Got:       "state " + merged.State,
		}
	}
	return deleteMergedSource(ctx, g, repo, merged, spec)
}

// githubMaxPullFiles is the most files GitHub's pull request files listing
// returns, however many the change touches.
const githubMaxPullFiles = 3000

type githubChangedFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
}

// ListPullRequestFiles returns every file the pull request changes, paged to
// the end; a listing that never ends is a fault.
func (g *GitHubBackend) ListPullRequestFiles(ctx context.Context, repo RepoRef, number int) ([]ForgeChangedFile, error) {
	var files []ForgeChangedFile
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListPullRequestFiles",
		path:      pullPath(repo, number) + "/files",
		maxPages:  maxIssuePages,
		exhausted: backendError("ListPullRequestFiles", errPullFilePagesExhausted),
	}, func(page []githubChangedFile) bool {
		for _, f := range page {
			files = append(files, ForgeChangedFile{
				Path:         f.Filename,
				PreviousPath: f.PreviousFilename,
				Status:       strings.ToLower(f.Status),
				Additions:    f.Additions,
				Deletions:    f.Deletions,
			})
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	// GitHub stops the listing at its cap without saying so: a listing that
	// reaches it may be missing files.
	if len(files) >= githubMaxPullFiles {
		return nil, &ChangedFilesTruncatedError{
			Type:    ForgeGitHub,
			Subject: repo.String() + "#" + strconv.Itoa(number),
			Listed:  len(files),
		}
	}
	return files, nil
}
