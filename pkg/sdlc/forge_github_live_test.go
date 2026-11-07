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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
