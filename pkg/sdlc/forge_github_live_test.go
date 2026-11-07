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

//go:build integration

package sdlc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Live GitHub suite
// =============================================================================
//
// The GitHub counterpart of the live Gitea suite (forge_gitea_live_test.go),
// with the same division of labour: the fixture tests own BEHAVIOUR, this
// suite owns SHAPES AND SERVER SEMANTICS — what a fixture cannot know because
// it answers whatever it is told.
//
// ISOLATION. Every test derives a unique namespace (namespace, shared with the
// Gitea suite because it is forge-neutral) and prefixes every branch, label,
// pull request and hook with it. Merges target a per-run base branch cut from
// the scratch repo's default branch and NEVER the default branch itself.
// t.Cleanup closes what a test opened and deletes the branches, labels and
// hooks it made; a cleanup that fails is reported with t.Errorf, not hidden.
//
// The suite needs two accounts, because GitHub refuses an author's approval
// of their own pull request: GITHUB_ITEST_TOKEN authors, and
// GITHUB_ITEST_REVIEWER_TOKEN, a collaborator on the scratch repo, reviews.

// ghRateFloor is the core rate-limit headroom a test needs before it starts.
// Below it a run would fail midway with a 403 that looks like a refusal.
const ghRateFloor = 200

// ghConfig is the environment this suite needs.
type ghConfig struct {
	baseURL       string
	token         string
	reviewerToken string
	repo          RepoRef
}

// ghSuite is one test's pair of adapters on the scratch repo.
type ghSuite struct {
	cfg      ghConfig
	author   *GitHubBackend
	reviewer *GitHubBackend
}

// ghLiveEnv reads the suite's configuration, FAILING rather than skipping
// when any of it is absent, for the reason liveEnv gives.
func ghLiveEnv(t *testing.T) ghConfig {
	t.Helper()

	cfg := ghConfig{
		baseURL:       os.Getenv("GITHUB_ITEST_BASE_URL"),
		token:         os.Getenv("GITHUB_ITEST_TOKEN"),
		reviewerToken: os.Getenv("GITHUB_ITEST_REVIEWER_TOKEN"),
		repo: RepoRef{
			Owner: os.Getenv("GITHUB_ITEST_OWNER"),
			Repo:  os.Getenv("GITHUB_ITEST_REPO"),
		},
	}
	missing := make([]string, 0, 4)
	if cfg.token == "" {
		missing = append(missing, "GITHUB_ITEST_TOKEN")
	}
	if cfg.repo.Owner == "" {
		missing = append(missing, "GITHUB_ITEST_OWNER")
	}
	if cfg.repo.Repo == "" {
		missing = append(missing, "GITHUB_ITEST_REPO")
	}
	if cfg.reviewerToken == "" {
		missing = append(missing, "GITHUB_ITEST_REVIEWER_TOKEN (a second account: GitHub refuses self-approval)")
	}
	if len(missing) > 0 {
		t.Fatalf("the live GitHub suite is not configured; set:\n\t%s", strings.Join(missing, "\n\t"))
	}
	if cfg.baseURL == "" {
		cfg.baseURL = githubDefaultBaseURL
	}
	cfg.baseURL = strings.TrimRight(cfg.baseURL, "/")
	return cfg
}

// ghLiveBackend builds the author's and the reviewer's adapters, after
// checking the author has the rate-limit headroom to finish the test.
func ghLiveBackend(t *testing.T) ghSuite {
	t.Helper()
	cfg := ghLiveEnv(t)
	ghRateGuard(t, cfg)

	author, err := NewGitHubBackend(GitHubConfig{BaseURL: cfg.baseURL, Token: cfg.token})
	if err != nil {
		t.Fatalf("NewGitHubBackend (author): %v", err)
	}
	reviewer, err := NewGitHubBackend(GitHubConfig{BaseURL: cfg.baseURL, Token: cfg.reviewerToken})
	if err != nil {
		t.Fatalf("NewGitHubBackend (reviewer): %v", err)
	}
	return ghSuite{cfg: cfg, author: author, reviewer: reviewer}
}

// ghRateGuard fails the test when the author token's core rate limit is
// nearly spent, naming the remaining count and when it resets.
func ghRateGuard(t *testing.T, cfg ghConfig) {
	t.Helper()

	var limits struct {
		Resources struct {
			Core struct {
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"core"`
		} `json:"resources"`
	}
	ghRequest(t, cfg, cfg.token, http.MethodGet, "/rate_limit", nil, &limits)
	core := limits.Resources.Core
	if core.Remaining < ghRateFloor {
		t.Fatalf("the author token has %d core requests left (need %d); the limit resets at %s",
			core.Remaining, ghRateFloor, time.Unix(core.Reset, 0).UTC().Format(time.RFC3339))
	}
}

// ghSend performs one raw call and returns its status and body. The token
// goes in the Authorization header only: an error names the method, the path
// and the status, never the credential.
func ghSend(t *testing.T, cfg ghConfig, token, method, path string, body any) (int, []byte) {
	t.Helper()

	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding %s %s: %v", method, path, err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, cfg.baseURL+path, payload)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", githubMediaType)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: the call failed", method, path)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Errorf("closing the body of %s %s: %v", method, path, closeErr)
	}
	if err != nil {
		t.Fatalf("reading %s %s: %v", method, path, err)
	}
	return resp.StatusCode, raw
}

// ghRequest is ghSend for a call that must succeed: a non-2xx status fails
// the test, and a 2xx body is decoded into out when out is not nil.
func ghRequest(t *testing.T, cfg ghConfig, token, method, path string, body, out any) {
	t.Helper()

	status, raw := ghSend(t, cfg, token, method, path, body)
	if status < 200 || status > 299 {
		t.Fatalf("%s %s: HTTP %d", method, path, status)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decoding %s %s: %v", method, path, err)
		}
	}
}

// ghCleanupCall is a cleanup's raw call: a status outside ok is reported
// with t.Errorf, so the test fails without hiding the residue it left.
func ghCleanupCall(t *testing.T, cfg ghConfig, method, path string, body any, ok ...int) {
	t.Helper()

	status, _ := ghSend(t, cfg, cfg.token, method, path, body)
	if status >= 200 && status <= 299 {
		return
	}
	for _, allowed := range ok {
		if status == allowed {
			return
		}
	}
	t.Errorf("cleanup %s %s: HTTP %d; the scratch repo keeps this residue", method, path, status)
}

// ghCommitFile creates path on branch through the contents API and returns
// the new commit's SHA. Paths are namespaced, so the file never exists yet.
func ghCommitFile(t *testing.T, cfg ghConfig, branch, path, content, message string) string {
	t.Helper()

	var created struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	ghRequest(t, cfg, cfg.token, http.MethodPut, repoPath(cfg.repo)+"/contents/"+path, map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}, &created)
	if created.Commit.SHA == "" {
		t.Fatalf("committing %s on %s returned no SHA", path, branch)
	}
	return created.Commit.SHA
}

// ghCreateLabel defines a repo label for this test and deletes it on cleanup.
func ghCreateLabel(t *testing.T, cfg ghConfig, name string) string {
	t.Helper()

	ghRequest(t, cfg, cfg.token, http.MethodPost, repoPath(cfg.repo)+"/labels", map[string]any{
		"name":  name,
		"color": "ededed",
	}, nil)
	t.Cleanup(func() { ghDeleteLabel(t, cfg, name) })
	return name
}

// ghDeleteLabel removes a repo label; one already gone is not residue.
func ghDeleteLabel(t *testing.T, cfg ghConfig, name string) {
	t.Helper()
	ghCleanupCall(t, cfg, http.MethodDelete,
		repoPath(cfg.repo)+"/labels/"+url.PathEscape(name), nil, http.StatusNotFound)
}

// ghDelivery is one entry of a hook's delivery log.
type ghDelivery struct {
	ID    int64  `json:"id"`
	GUID  string `json:"guid"`
	Event string `json:"event"`
}

// ghHookDeliveries lists the deliveries GitHub has attempted for a hook.
func ghHookDeliveries(t *testing.T, cfg ghConfig, hookID int64) []ghDelivery {
	t.Helper()

	var deliveries []ghDelivery
	ghRequest(t, cfg, cfg.token, http.MethodGet,
		fmt.Sprintf("%s/hooks/%d/deliveries", repoPath(cfg.repo), hookID), nil, &deliveries)
	return deliveries
}

// ghHookDelivery reads one delivery back: the request headers GitHub sent
// and the payload, as raw bytes for a signature check.
func ghHookDelivery(t *testing.T, cfg ghConfig, hookID, deliveryID int64) (http.Header, []byte) {
	t.Helper()

	var delivery struct {
		Request struct {
			Headers map[string]string `json:"headers"`
			Payload json.RawMessage   `json:"payload"`
		} `json:"request"`
	}
	ghRequest(t, cfg, cfg.token, http.MethodGet,
		fmt.Sprintf("%s/hooks/%d/deliveries/%d", repoPath(cfg.repo), hookID, deliveryID), nil, &delivery)

	headers := http.Header{}
	for name, value := range delivery.Request.Headers {
		headers.Set(name, value)
	}
	return headers, delivery.Request.Payload
}

// ghEventually polls check every interval until it reports done or timeout
// passes, failing the test with what on timeout. It waits on a ticker and a
// timer, never on a sleep.
func ghEventually(t *testing.T, timeout, interval time.Duration, what string, check func() bool) {
	t.Helper()

	if check() {
		return
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		case <-tick.C:
			if check() {
				return
			}
		}
	}
}

// ghDefaultBranch reads the scratch repo's default branch.
func ghDefaultBranch(t *testing.T, cfg ghConfig) string {
	t.Helper()

	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	ghRequest(t, cfg, cfg.token, http.MethodGet, repoPath(cfg.repo), nil, &repo)
	if repo.DefaultBranch == "" {
		t.Fatal("the scratch repo reports no default branch")
	}
	return repo.DefaultBranch
}

// ghBranch cuts name from from with the adapter and deletes it on cleanup.
// A branch a squash merge already removed is not residue.
func ghBranch(t *testing.T, s ghSuite, name, from string) string {
	t.Helper()

	if _, err := s.author.CreateBranch(context.Background(), s.cfg.repo, name, from); err != nil {
		t.Fatalf("cutting branch %s from %s: %v", name, from, err)
	}
	t.Cleanup(func() {
		ghCleanupCall(t, s.cfg, http.MethodDelete,
			repoPath(s.cfg.repo)+"/git/refs/heads/"+name, nil,
			http.StatusNotFound, http.StatusUnprocessableEntity)
	})
	return name
}

// ghBaseBranch cuts this test's base branch from the repo's default branch.
// Every merge in the suite targets it, never the default branch.
func ghBaseBranch(t *testing.T, s ghSuite, ns string) string {
	t.Helper()
	return ghBranch(t, s, ns+"-base", ghDefaultBranch(t, s.cfg))
}

// ghClosePullOnCleanup closes the pull request when the test ends, unless
// it already closed or merged.
func ghClosePullOnCleanup(t *testing.T, s ghSuite, number int) {
	t.Helper()

	t.Cleanup(func() {
		path := fmt.Sprintf("%s/pulls/%d", repoPath(s.cfg.repo), number)
		status, raw := ghSend(t, s.cfg, s.cfg.token, http.MethodGet, path, nil)
		if status != http.StatusOK {
			t.Errorf("cleanup GET %s: HTTP %d", path, status)
			return
		}
		var pull struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(raw, &pull); err != nil {
			t.Errorf("cleanup decoding %s: %v", path, err)
			return
		}
		if pull.State == "open" {
			ghCleanupCall(t, s.cfg, http.MethodPatch, path, map[string]string{"state": "closed"})
		}
	})
}

// ghPull is one test's open pull request: a work branch off the per-run
// base, one commit on it, and the pull request between them.
type ghPull struct {
	base   string
	work   string
	head   string
	number int
}

// ghOpenPull cuts the base and work branches, commits one file on the work
// branch, and opens the pull request, closing it on cleanup.
func ghOpenPull(t *testing.T, s ghSuite, ns, title string) ghPull {
	t.Helper()

	base := ghBaseBranch(t, s, ns)
	work := ghBranch(t, s, ns+"-work", base)
	head := ghCommitFile(t, s.cfg, work, ns+"-first.txt", "first\n", "first change")

	pull, err := s.author.CreatePullRequest(context.Background(), s.cfg.repo, PullRequestSpec{
		Title:   ns + " " + title,
		Body:    "live GitHub suite",
		HeadRef: work,
		BaseRef: base,
	})
	if err != nil {
		t.Fatalf("opening the pull request: %v", err)
	}
	ghClosePullOnCleanup(t, s, pull.Number)
	if pull.HeadSHA != head {
		t.Fatalf("the pull request opened at %s, not the commit just pushed (%s)", pull.HeadSHA, head)
	}
	return ghPull{base: base, work: work, head: head, number: pull.Number}
}

// ghAwaitHead waits until GitHub reports the pull request's head at sha:
// a push reaches the pull request asynchronously.
func ghAwaitHead(t *testing.T, s ghSuite, number int, sha string) {
	t.Helper()

	ghEventually(t, 30*time.Second, 500*time.Millisecond, "the pull request head to reach "+sha, func() bool {
		pull, err := s.author.GetPullRequest(context.Background(), s.cfg.repo, number)
		if err != nil {
			t.Fatalf("reading the pull request: %v", err)
		}
		return pull.HeadSHA == sha
	})
}

// ghApprove submits the reviewer's approval at head.
func ghApprove(t *testing.T, s ghSuite, number int, head string) {
	t.Helper()

	if _, err := s.reviewer.SubmitReview(context.Background(), s.cfg.repo, number, ReviewSubmission{
		Verdict: VerdictApprove,
		Body:    "approved by the live GitHub suite",
		HeadSHA: head,
	}); err != nil {
		t.Fatalf("the reviewer approving at %s: %v", head, err)
	}
}

// TestLiveGitHub_PullRequestHeadFollowsAPush opens a pull request, pushes a
// second commit, and reads the new head back from the open listing.
func TestLiveGitHub_PullRequestHeadFollowsAPush(t *testing.T) {
	s := ghLiveBackend(t)
	ns := namespace(t)
	pull := ghOpenPull(t, s, ns, "head follows push")

	moved := ghCommitFile(t, s.cfg, pull.work, ns+"-second.txt", "second\n", "second change")
	if moved == pull.head {
		t.Fatal("the second commit did not move the branch; the test would prove nothing")
	}
	ghAwaitHead(t, s, pull.number, moved)

	open, err := s.author.ListOpenPullRequests(context.Background(), s.cfg.repo)
	if err != nil {
		t.Fatalf("listing open pull requests: %v", err)
	}
	for _, listed := range open {
		if listed.Number != pull.number {
			continue
		}
		if listed.HeadSHA != moved {
			t.Fatalf("the listing reports head %s, want the pushed commit %s", listed.HeadSHA, moved)
		}
		if listed.HeadRef != pull.work || listed.BaseRef != pull.base {
			t.Fatalf("the listing reports %s -> %s, want %s -> %s",
				listed.HeadRef, listed.BaseRef, pull.work, pull.base)
		}
		return
	}
	t.Fatalf("pull request #%d is missing from the open listing", pull.number)
}

// TestLiveGitHub_StaleApprovalIsRefusedThenTheApprovedHeadSquashes approves
// head A, pushes head B, and checks a merge expecting A is refused and
// merges nothing; then approves B and squash-merges at B, deleting the branch.
func TestLiveGitHub_StaleApprovalIsRefusedThenTheApprovedHeadSquashes(t *testing.T) {
	s := ghLiveBackend(t)
	ctx := context.Background()
	ns := namespace(t)
	pull := ghOpenPull(t, s, ns, "stale approval")

	ghApprove(t, s, pull.number, pull.head)
	moved := ghCommitFile(t, s.cfg, pull.work, ns+"-second.txt", "second\n", "second change")
	ghAwaitHead(t, s, pull.number, moved)

	err := s.author.MergePullRequest(ctx, s.cfg.repo, pull.number,
		MergeSpec{Method: "squash", CommitTitle: ns + " stale"}, pull.head)
	if !errors.Is(err, ErrHeadMoved) {
		t.Fatalf("merging at the stale approved head: error = %v, want the moved-head refusal", err)
	}
	after, err := s.author.GetPullRequest(ctx, s.cfg.repo, pull.number)
	if err != nil {
		t.Fatalf("re-reading the pull request: %v", err)
	}
	if after.Merged || after.State != "open" {
		t.Fatalf("after the refusal the pull request is state %q merged %v, want open and unmerged",
			after.State, after.Merged)
	}

	ghApprove(t, s, pull.number, moved)
	spec := MergeSpec{
		Method:              "squash",
		CommitTitle:         ns + " squashed",
		CommitMessage:       "landed by the live GitHub suite",
		ExpectSourceDeleted: true,
	}
	ghEventually(t, 60*time.Second, time.Second, "GitHub to accept the merge", func() bool {
		err := s.author.MergePullRequest(ctx, s.cfg.repo, pull.number, spec, moved)
		if errors.Is(err, ErrForgeNotReady) {
			return false
		}
		if err != nil {
			t.Fatalf("merging at the approved head: %v", err)
		}
		return true
	})

	merged, err := s.author.GetPullRequest(ctx, s.cfg.repo, pull.number)
	if err != nil {
		t.Fatalf("re-reading the merged pull request: %v", err)
	}
	if !merged.Merged || merged.MergeSHA == "" {
		t.Fatalf("merged = %v, merge sha %q; want a merged pull request with its commit", merged.Merged, merged.MergeSHA)
	}
	var commit struct {
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	ghRequest(t, s.cfg, s.cfg.token, http.MethodGet, repoPath(s.cfg.repo)+"/commits/"+merged.MergeSHA, nil, &commit)
	if len(commit.Parents) != 1 {
		t.Fatalf("the merge commit has %d parents; a squash has exactly 1", len(commit.Parents))
	}
	if _, err := s.author.GetBranch(ctx, s.cfg.repo, pull.work); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reading the merged source branch: error = %v, want it deleted (ErrNotFound)", err)
	}
}

// TestLiveGitHub_SelfApprovalIsRefused checks GitHub refuses the author's
// own approval and the adapter reports it as a typed API refusal: a
// *BackendError wrapping a *ForgeAPIError with status 422.
func TestLiveGitHub_SelfApprovalIsRefused(t *testing.T) {
	s := ghLiveBackend(t)
	ns := namespace(t)
	pull := ghOpenPull(t, s, ns, "self approval")

	_, err := s.author.SubmitReview(context.Background(), s.cfg.repo, pull.number, ReviewSubmission{
		Verdict: VerdictApprove,
		Body:    "the author approving their own work",
		HeadSHA: pull.head,
	})
	if err == nil {
		t.Fatal("GitHub accepted the author's approval of their own pull request")
	}
	var apiErr *ForgeAPIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v (%T), want a *BackendError wrapping a *ForgeAPIError", err, err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d for a self-approval", apiErr.Status, http.StatusUnprocessableEntity)
	}
}

// TestLiveGitHub_CommitStatusReadsBackAsACheckRun publishes a commit status
// on the head and reads it back through ListCheckRuns by its context.
func TestLiveGitHub_CommitStatusReadsBackAsACheckRun(t *testing.T) {
	s := ghLiveBackend(t)
	ctx := context.Background()
	ns := namespace(t)
	pull := ghOpenPull(t, s, ns, "commit status")

	statusContext := ns + "/live"
	if err := s.author.SetCommitStatus(ctx, s.cfg.repo, pull.head, CommitStatus{
		Context:     statusContext,
		Status:      CheckSuccess,
		Description: "published by the live GitHub suite",
	}); err != nil {
		t.Fatalf("publishing the commit status: %v", err)
	}

	runs, err := s.author.ListCheckRuns(ctx, s.cfg.repo, pull.head)
	if err != nil {
		t.Fatalf("listing check runs: %v", err)
	}
	for _, run := range runs {
		if run.Name != statusContext {
			continue
		}
		if run.Status != "completed" || run.Conclusion != "success" {
			t.Fatalf("the status reads back as %s/%s, want completed/success", run.Status, run.Conclusion)
		}
		if run.HeadSHA != pull.head {
			t.Fatalf("the status reads back on %s, want %s", run.HeadSHA, pull.head)
		}
		return
	}
	t.Fatalf("no check run named %s on %s among %d runs", statusContext, pull.head, len(runs))
}

// TestLiveGitHub_LabelsRoundTripOnAPullRequest adds two provisioned labels
// to a pull request, removes one, and reads each state back.
func TestLiveGitHub_LabelsRoundTripOnAPullRequest(t *testing.T) {
	s := ghLiveBackend(t)
	ctx := context.Background()
	ns := namespace(t)
	pull := ghOpenPull(t, s, ns, "labels")
	kept := ghCreateLabel(t, s.cfg, ns+"-kept")
	dropped := ghCreateLabel(t, s.cfg, ns+"-dropped")

	if err := s.author.AddIssueLabels(ctx, s.cfg.repo, pull.number, []string{kept, dropped}); err != nil {
		t.Fatalf("adding the labels: %v", err)
	}
	ghWantLabels(t, s, pull.number, kept, dropped)

	if err := s.author.RemoveIssueLabel(ctx, s.cfg.repo, pull.number, dropped); err != nil {
		t.Fatalf("removing a label: %v", err)
	}
	ghWantLabels(t, s, pull.number, kept)
}

// ghWantLabels fails unless the pull request carries exactly want.
func ghWantLabels(t *testing.T, s ghSuite, number int, want ...string) {
	t.Helper()

	pull, err := s.author.GetPullRequest(context.Background(), s.cfg.repo, number)
	if err != nil {
		t.Fatalf("reading the pull request's labels: %v", err)
	}
	if len(pull.Labels) != len(want) {
		t.Fatalf("labels = %v, want %v", pull.Labels, want)
	}
	held := make(map[string]bool, len(pull.Labels))
	for _, label := range pull.Labels {
		held[label] = true
	}
	for _, label := range want {
		if !held[label] {
			t.Fatalf("labels = %v, missing %s", pull.Labels, label)
		}
	}
}

// TestLiveGitHub_AnUndefinedLabelIsRefused checks a label the repo does not
// define is refused before any write, as on Gitea.
func TestLiveGitHub_AnUndefinedLabelIsRefused(t *testing.T) {
	s := ghLiveBackend(t)
	ns := namespace(t)
	pull := ghOpenPull(t, s, ns, "undefined label")

	err := s.author.AddIssueLabels(context.Background(), s.cfg.repo, pull.number, []string{ns + "-never-defined"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// TestLiveGitHub_WebhookIsSignedAndUpdatedInPlace provisions a hook with a
// secret, ensures it again with more events, and checks there is one hook.
// It then reads GitHub's ping delivery back and verifies its signature, and
// checks a one-byte change to the payload is refused.
func TestLiveGitHub_WebhookIsSignedAndUpdatedInPlace(t *testing.T) {
	s := ghLiveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	hookURL := "https://module.invalid/hook/" + ns
	secret := []byte("live-suite-secret-" + ns)
	t.Cleanup(func() {
		if err := s.author.DeleteWebhook(context.Background(), s.cfg.repo, hookURL); err != nil {
			t.Errorf("cleanup deleting the webhook: %v", err)
		}
	})

	if err := s.author.EnsureWebhook(ctx, s.cfg.repo, WebhookSpec{
		URL: hookURL, Events: []string{"issues"},
	}, secret); err != nil {
		t.Fatalf("creating the webhook: %v", err)
	}
	if err := s.author.EnsureWebhook(ctx, s.cfg.repo, WebhookSpec{
		URL: hookURL, Events: []string{"issues", "pull_request"},
	}, secret); err != nil {
		t.Fatalf("updating the webhook: %v", err)
	}

	hooks, err := s.author.Webhooks(ctx, s.cfg.repo)
	if err != nil {
		t.Fatalf("listing webhooks: %v", err)
	}
	var hookID int64
	matching := 0
	for _, hook := range hooks {
		if hook.URL != hookURL {
			continue
		}
		matching++
		id, err := strconv.ParseInt(hook.ID, 10, 64)
		if err != nil {
			t.Fatalf("hook id %q is not numeric: %v", hook.ID, err)
		}
		hookID = id
	}
	if matching != 1 {
		t.Fatalf("%d hooks point at %s, want exactly 1", matching, hookURL)
	}

	var ping ghDelivery
	ghEventually(t, 60*time.Second, 2*time.Second, "GitHub to record the ping delivery", func() bool {
		for _, delivery := range ghHookDeliveries(t, s.cfg, hookID) {
			if delivery.Event == "ping" {
				ping = delivery
				return true
			}
		}
		return false
	})

	headers, payload := ghHookDelivery(t, s.cfg, hookID, ping.ID)
	signature := headers.Get(GitHubSignatureHeader)
	if signature == "" {
		t.Fatalf("the ping delivery carries no %s header", GitHubSignatureHeader)
	}
	if err := s.author.VerifyWebhook(payload, signature, secret); err != nil {
		t.Fatalf("verifying the ping delivery: %v", err)
	}

	if len(payload) == 0 {
		t.Fatal("the ping delivery's payload is empty; there is nothing to alter")
	}
	altered := bytes.Clone(payload)
	altered[len(altered)/2] ^= 0x01
	var unverified *WebhookUnverifiedError
	if err := s.author.VerifyWebhook(altered, signature, secret); !errors.As(err, &unverified) {
		t.Fatalf("verifying an altered payload: error = %v, want a *WebhookUnverifiedError", err)
	}
}

// TestLiveGitHub_BranchProtectionReadIsAProtectionOrATypedRefusal reads the
// default branch's protection. It returns a rule, nil for an unprotected
// branch, or — on a GitHub Free private repo, which answers 403 "Upgrade to
// GitHub Pro" — a *BackendError wrapping a *ForgeAPIError with status 403,
// which a caller can tell apart from "unprotected". Anything else fails.
func TestLiveGitHub_BranchProtectionReadIsAProtectionOrATypedRefusal(t *testing.T) {
	s := ghLiveBackend(t)
	branch := ghDefaultBranch(t, s.cfg)

	protection, err := s.author.BranchProtection(context.Background(), s.cfg.repo, branch)
	if err == nil {
		if protection != nil && protection.Branch != branch {
			t.Fatalf("protection names branch %q, want %q", protection.Branch, branch)
		}
		return
	}
	var apiErr *ForgeAPIError
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v (%T), want a *BackendError wrapping a *ForgeAPIError", err, err)
	}
	if apiErr.Status != http.StatusForbidden {
		t.Fatalf("status = %d (%s), want %d for a plan that cannot read protection",
			apiErr.Status, apiErr.Message, http.StatusForbidden)
	}
}
