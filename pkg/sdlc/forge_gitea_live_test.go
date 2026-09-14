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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Live Gitea suite
// =============================================================================
//
// WHAT THIS SUITE IS FOR, and what it deliberately leaves to the fixtures.
//
// The fixture tests own BEHAVIOUR: read-back, status handling, guard refusals,
// HMAC. They are fast, deterministic, and can stage things a real server will
// not produce on demand. This suite owns SHAPES AND SERVER SEMANTICS — the
// things a fixture cannot know, because a fixture answers whatever it is told
// and a wrong field name round-trips happily through one.
//
// That division is not theoretical. Two real bugs shipped past a green fixture
// suite and were caught by reading the running server's schema: the merge
// option struct is snake_case (`do`, not `Do` — and `do` is required, so every
// merge would have failed), and there is no `draft` field at all, only a title
// prefix. This suite is the standing version of that check.
//
// ISOLATION. Every run derives a unique namespace and prefixes every branch,
// label and issue title with it. Nothing here uses a fixed name, two runs on
// one host cannot collide, and residue from a crashed run is inert rather than
// something to clean up first. Merges target a per-run base branch and NEVER
// the scratch repo's main, so one run cannot change what another sees.
//
// The scratch repo is the agent's own (agent-go-quicbaas/sdlc-itest), so
// "an agent writes only its own repo" holds without exception. No product
// repository is touched by anything in this file.

// liveConfig is the environment this suite needs.
type liveConfig struct {
	baseURL string
	token   string
	repo    RepoRef
}

// liveEnv reads the suite's configuration, FAILING rather than skipping when it
// is absent.
//
// The build tag is the opt-in; once a run has opted in, missing configuration
// is a broken run and not a reason to report success quietly. Skip guards are
// forbidden precisely because a suite that skips itself looks identical to one
// that passed.
func liveEnv(t *testing.T) liveConfig {
	t.Helper()

	cfg := liveConfig{
		baseURL: os.Getenv("GITEA_ITEST_BASE_URL"),
		token:   os.Getenv("GITEA_ITEST_TOKEN"),
		repo: RepoRef{
			Owner: os.Getenv("GITEA_ITEST_OWNER"),
			Repo:  os.Getenv("GITEA_ITEST_REPO"),
		},
	}
	missing := make([]string, 0, 4)
	if cfg.baseURL == "" {
		missing = append(missing, "GITEA_ITEST_BASE_URL (e.g. http://host:3001/api/v1)")
	}
	if cfg.token == "" {
		missing = append(missing, "GITEA_ITEST_TOKEN")
	}
	if cfg.repo.Owner == "" {
		missing = append(missing, "GITEA_ITEST_OWNER")
	}
	if cfg.repo.Repo == "" {
		missing = append(missing, "GITEA_ITEST_REPO")
	}
	if len(missing) > 0 {
		t.Fatalf("the live Gitea suite is not configured; set:\n\t%s", strings.Join(missing, "\n\t"))
	}
	return cfg
}

// liveBackend builds the adapter under test against the scratch repo.
func liveBackend(t *testing.T) (*GiteaBackend, liveConfig) {
	t.Helper()
	cfg := liveEnv(t)
	backend, err := NewGiteaBackend(GiteaConfig{BaseURL: cfg.baseURL, Token: cfg.token})
	if err != nil {
		t.Fatalf("NewGiteaBackend: %v", err)
	}
	return backend, cfg
}

// namespace returns an identifier unique to this run and test.
//
// Unique per RUN, not per suite: two runs on one host must not collide, and a
// crashed run must leave nothing a later run trips over. Nanosecond time plus
// the test name is enough and needs no coordination.
func namespace(t *testing.T) string {
	t.Helper()
	name := strings.ToLower(t.Name())
	name = strings.NewReplacer("/", "-", "_", "-", " ", "-").Replace(name)
	if len(name) > 24 {
		name = name[:24]
	}
	return fmt.Sprintf("it-%d-%s", time.Now().UnixNano(), name)
}

// =============================================================================
// Raw helpers — test scaffolding, not product code
// =============================================================================
//
// These reach Gitea directly for setup the adapter deliberately does not
// implement (committing a file, creating a label, opening an issue). They are
// the suite's fixtures, expressed against the real server.

// liveRequest performs a raw API call against the scratch repo.
func liveRequest(t *testing.T, cfg liveConfig, method, path string, body any, out any) {
	t.Helper()

	var payload *strings.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding %s %s: %v", method, path, err)
		}
		payload = strings.NewReader(string(encoded))
	} else {
		payload = strings.NewReader("")
	}

	req, err := http.NewRequestWithContext(context.Background(), method, cfg.baseURL+path, payload)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "token "+cfg.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decoding %s %s: %v", method, path, err)
		}
	}
}

// liveStatus performs a raw call and returns the status code instead of failing
// on a non-2xx, for the cases where the REFUSAL is the thing under test.
func liveStatus(t *testing.T, cfg liveConfig, method, path string, body any) (int, string) {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding %s %s: %v", method, path, err)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, cfg.baseURL+path, strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "token "+cfg.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	message := make([]byte, 512)
	n, _ := resp.Body.Read(message)
	return resp.StatusCode, string(message[:n])
}

// commitFile writes a file on a branch and returns the resulting commit SHA.
//
// This is how the suite moves a branch head, which is what the merge guard has
// to be tested against.
func commitFile(t *testing.T, cfg liveConfig, branch, path, content, message string) string {
	t.Helper()

	var created struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	liveRequest(t, cfg, http.MethodPost, repoPath(cfg.repo)+"/contents/"+path, map[string]any{
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
		"message": message,
	}, &created)

	if created.Commit.SHA == "" {
		t.Fatalf("committing %s on %s returned no SHA", path, branch)
	}
	return created.Commit.SHA
}

// createLabel defines a label in the scratch repo and removes it afterwards.
//
// Labels are the one thing this suite creates that it MUST clean up. Branches
// and issues accumulate harmlessly under unique namespaces, but every leftover
// label enlarges the vocabulary that resolveLabelIDs pages through on every
// run of every project, so leaking one per run turns a persistent scratch repo
// into a slowly failing one.
func createLabel(t *testing.T, cfg liveConfig, name string) string {
	t.Helper()

	var created struct {
		ID int64 `json:"id"`
	}
	liveRequest(t, cfg, http.MethodPost, repoPath(cfg.repo)+"/labels", map[string]any{
		"name":  name,
		"color": "#ededed",
	}, &created)

	t.Cleanup(func() {
		if created.ID == 0 {
			return
		}
		liveRequest(t, cfg, http.MethodDelete,
			fmt.Sprintf("%s/labels/%d", repoPath(cfg.repo), created.ID), nil, nil)
	})
	return name
}

// createIssue opens an issue and returns its number.
func createIssue(t *testing.T, cfg liveConfig, title string) int {
	t.Helper()
	var created struct {
		Number int `json:"number"`
	}
	liveRequest(t, cfg, http.MethodPost, repoPath(cfg.repo)+"/issues", map[string]any{
		"title": title,
		"body":  "opened by the go-quicbaas live forge suite",
	}, &created)
	if created.Number == 0 {
		t.Fatal("creating an issue returned no number")
	}
	return created.Number
}

// awaitMergeable waits until Gitea has finished computing a pull request's
// mergeability.
//
// Gitea computes it asynchronously and answers 405 "Please try again later"
// until it has, so a merge issued right after creating a pull request fails for
// a reason that says nothing about whether the merge is allowed. Polling for
// the condition is the same pattern the unit conventions require; the deadline
// is a failure bound, not the synchronization.
func awaitMergeable(t *testing.T, backend *GiteaBackend, cfg liveConfig, number int) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for {
		pull, err := backend.GetPullRequest(context.Background(), cfg.repo, number)
		if err != nil {
			t.Fatalf("reading the pull request while waiting for mergeability: %v", err)
		}
		if pull.Mergeable {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("gitea never reported pull request #%d mergeable", number)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// mergeWithRetry merges, retrying while the forge reports it is not ready.
//
// This is the contract ErrForgeNotReady exists to express, exercised the way
// the merge engine must exercise it: Gitea recomputes mergeability
// asynchronously and answers "try again later" until it has, so a single
// attempt races the server's own bookkeeping. Waiting for Mergeable first
// narrows the window but does not close it — the state can go stale between
// the poll and the proposal — which is precisely why the error is retryable
// rather than fatal.
//
// Every OTHER error fails immediately: retrying a refusal would turn a real
// verdict into a timeout.
func mergeWithRetry(t *testing.T, backend *GiteaBackend, cfg liveConfig, number int, spec MergeSpec, head string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for attempt := 1; ; attempt++ {
		err := backend.MergePullRequest(context.Background(), cfg.repo, number, spec, head)
		if err == nil {
			return
		}
		if !errors.Is(err, ErrForgeNotReady) {
			t.Fatalf("merging at the approved head: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("gitea never became ready to merge #%d after %d attempts: %v", number, attempt, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// baseBranch cuts a per-run base branch from the scratch repo's default branch.
//
// Runs merge into THIS, never into the scratch repo's main: a run that merged
// into main would change what every other run sees, which is the collision
// per-run isolation exists to prevent.
func baseBranch(t *testing.T, backend *GiteaBackend, cfg liveConfig, ns string) string {
	t.Helper()
	name := ns + "-base"
	if _, err := backend.CreateBranch(context.Background(), cfg.repo, name, "main"); err != nil {
		t.Fatalf("cutting the per-run base branch: %v", err)
	}
	return name
}

// =============================================================================
// The headline: does THIS server honour head_commit_id?
// =============================================================================

// TestLive_MergeRefusesAStaleApprovedHead is the verification the merge slice
// deferred to this suite.
//
// The adapter sends head_commit_id so the FORGE refuses a merge whose branch
// moved after the review. Gitea 1.27.1's schema lists the field — but a field
// present in a schema and a field the server acts on are different claims, and
// only a real merge attempt settles the second one.
//
// If this test fails while the pre-read is disabled, the guard is
// check-then-act with a window, and the adapter's doc comment is wrong and must
// be corrected rather than defended.
func TestLive_MergeRefusesAStaleApprovedHead(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	base := baseBranch(t, backend, cfg, ns)
	work := ns + "-work"
	if _, err := backend.CreateBranch(ctx, cfg.repo, work, base); err != nil {
		t.Fatalf("cutting the work branch: %v", err)
	}

	// The tree a reviewer would have approved.
	approved := commitFile(t, cfg, work, ns+"-first.txt", "first\n", "first change")

	pull, err := backend.CreatePullRequest(ctx, cfg.repo, PullRequestSpec{
		Title:   ns + " merge guard",
		Body:    "live suite",
		HeadRef: work,
		BaseRef: base,
	})
	if err != nil {
		t.Fatalf("opening the pull request: %v", err)
	}
	if pull.HeadSHA != approved {
		t.Fatalf("the pull request opened at %s, not the commit just pushed (%s)", pull.HeadSHA, approved)
	}

	// A commit lands after the approval.
	moved := commitFile(t, cfg, work, ns+"-second.txt", "second\n", "second change")
	if moved == approved {
		t.Fatal("the second commit did not move the branch; the test would prove nothing")
	}

	// The adapter's own pre-read should refuse this before proposing anything.
	err = backend.MergePullRequest(ctx, cfg.repo, pull.Number,
		MergeSpec{Method: "squash", CommitTitle: ns}, approved)
	if err == nil {
		t.Fatal("a merge against a stale approved head succeeded")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("error = %v, want the moved-head refusal", err)
	}

	// And the pull request must still be open: refusing must not half-merge.
	after, err := backend.GetPullRequest(ctx, cfg.repo, pull.Number)
	if err != nil {
		t.Fatalf("re-reading the pull request: %v", err)
	}
	if after.Merged {
		t.Fatal("the pull request merged despite the refusal")
	}
}

// TestLive_TheServerItselfRefusesAStaleHead pins the property the adapter's
// doc comment RELIES on, independently of the adapter.
//
// The adapter's pre-read refuses a moved head before proposing anything, so the
// test above passes whether or not Gitea honours head_commit_id. That makes it
// silent about the thing that actually matters: the pre-read is check-then-act
// with a window, and only the SERVER-side guard closes it.
//
// So this proposes the merge directly, with a stale head_commit_id, and asserts
// the server refuses. Measured 2026-08-20 against Gitea 1.27.1: HTTP 409, "head
// out of date". If a future Gitea drops or ignores the field, this fails and
// the adapter's claim of an atomic guard must be corrected rather than
// defended.
func TestLive_TheServerItselfRefusesAStaleHead(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	base := baseBranch(t, backend, cfg, ns)
	work := ns + "-work"
	if _, err := backend.CreateBranch(ctx, cfg.repo, work, base); err != nil {
		t.Fatalf("cutting the work branch: %v", err)
	}
	stale := commitFile(t, cfg, work, ns+"-first.txt", "first\n", "first change")

	pull, err := backend.CreatePullRequest(ctx, cfg.repo, PullRequestSpec{
		Title: ns + " server guard", HeadRef: work, BaseRef: base,
	})
	if err != nil {
		t.Fatalf("opening the pull request: %v", err)
	}

	moved := commitFile(t, cfg, work, ns+"-second.txt", "second\n", "second change")
	if moved == stale {
		t.Fatal("the second commit did not move the branch; the test would prove nothing")
	}

	// Wait for Gitea to finish recomputing after the head moved, or the merge
	// is refused with 405 "try again later" and this test would credit the
	// stale-head guard for a refusal it had nothing to do with.
	awaitMergeable(t, backend, cfg, pull.Number)

	// Straight to the merge endpoint, no pre-read: only the server decides.
	status, body := liveStatus(t, cfg, http.MethodPost,
		fmt.Sprintf("%s/pulls/%d/merge", repoPath(cfg.repo), pull.Number),
		map[string]any{
			"do":             "squash",
			"head_commit_id": stale,
		})

	// EXACTLY 409, not merely "some error". Accepting any 4xx would report the
	// guard as verified when the merge had actually been refused for an
	// unrelated reason — a permission change, a validation error, a schema
	// drift — which is the assert-the-reason rule this suite exists to uphold.
	// Measured 2026-08-20 against 1.27.1: 409 "head out of date".
	if status != http.StatusConflict {
		t.Fatalf("server answered HTTP %d (%s), want 409 head-out-of-date; "+
			"anything else means this test is no longer proving head_commit_id is honoured",
			status, strings.TrimSpace(body))
	}

	after, err := backend.GetPullRequest(ctx, cfg.repo, pull.Number)
	if err != nil {
		t.Fatalf("re-reading the pull request: %v", err)
	}
	if after.Merged {
		t.Fatal("the pull request merged despite the server refusing")
	}
}

// TestLive_MergeAtTheApprovedHeadSucceeds is the other half: the guard must not
// refuse a merge that IS at the approved tree, or the module could never land
// anything.
//
// It also proves the merge request's field names are the ones Gitea binds —
// "do" is required, so a merge sent with the Go field names fails outright, and
// that is the bug the live schema caught.
func TestLive_MergeAtTheApprovedHeadSucceeds(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	base := baseBranch(t, backend, cfg, ns)
	work := ns + "-work"
	if _, err := backend.CreateBranch(ctx, cfg.repo, work, base); err != nil {
		t.Fatalf("cutting the work branch: %v", err)
	}
	approved := commitFile(t, cfg, work, ns+"-only.txt", "only\n", "the only change")

	pull, err := backend.CreatePullRequest(ctx, cfg.repo, PullRequestSpec{
		Title:   ns + " merge success",
		HeadRef: work,
		BaseRef: base,
	})
	if err != nil {
		t.Fatalf("opening the pull request: %v", err)
	}

	awaitMergeable(t, backend, cfg, pull.Number)
	mergeWithRetry(t, backend, cfg, pull.Number, MergeSpec{
		Method:              "squash",
		CommitTitle:         ns + " squashed",
		CommitMessage:       "landed by the live forge suite",
		ExpectSourceDeleted: true,
	}, approved)

	// MergePullRequest already read back that it merged; assert the squash
	// produced a merge commit too, which is what the strategy asked for.
	merged, err := backend.GetPullRequest(ctx, cfg.repo, pull.Number)
	if err != nil {
		t.Fatalf("re-reading the merged pull request: %v", err)
	}
	if !merged.Merged {
		t.Fatal("the pull request reports unmerged after a successful merge")
	}
}

// =============================================================================
// Shapes the fixtures cannot confirm
// =============================================================================

// TestLive_DraftIsMarkedTheWayGiteaRecognises settles the second schema bug
// against the running server.
//
// CreatePullRequestOption has no draft field; Gitea uses a title prefix, and
// the prefix is server-configurable. The adapter verifies by read-back, so this
// test proves the prefix it sends is the one THIS server acts on.
func TestLive_DraftIsMarkedTheWayGiteaRecognises(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	base := baseBranch(t, backend, cfg, ns)
	work := ns + "-work"
	if _, err := backend.CreateBranch(ctx, cfg.repo, work, base); err != nil {
		t.Fatalf("cutting the work branch: %v", err)
	}
	commitFile(t, cfg, work, ns+"-draft.txt", "draft\n", "draft change")

	pull, err := backend.CreatePullRequest(ctx, cfg.repo, PullRequestSpec{
		Title:   ns + " draft",
		HeadRef: work,
		BaseRef: base,
		Draft:   true,
	})
	if err != nil {
		t.Fatalf("opening a draft pull request: %v", err)
	}
	if !pull.Draft {
		t.Fatal("the pull request is not a draft; this server's WIP prefix differs from the one sent")
	}
}

// TestLive_LabelsRoundTripThroughTheirIDs proves the label write reaches the
// server in the form it binds.
//
// Gitea's endpoint takes label IDs; sending names is a silent no-op that still
// answers 200. A fixture cannot tell the difference — the read-back here is the
// real server's own view.
func TestLive_LabelsRoundTripThroughTheirIDs(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	label := createLabel(t, cfg, ns+"-ready")
	issue := createIssue(t, cfg, ns+" labels")

	if err := backend.SetIssueLabels(ctx, cfg.repo, issue, []string{label}); err != nil {
		t.Fatalf("setting labels: %v", err)
	}

	read, err := backend.GetIssue(ctx, cfg.repo, issue)
	if err != nil {
		t.Fatalf("re-reading the issue: %v", err)
	}
	if normalizedLabels(read.Labels) != normalizedLabels([]string{label}) {
		t.Errorf("labels = %v, want %v", read.Labels, []string{label})
	}

	// Clearing must also take effect, which the read-back inside the adapter
	// verifies; this asserts the server's own view agrees.
	if err := backend.SetIssueLabels(ctx, cfg.repo, issue, nil); err != nil {
		t.Fatalf("clearing labels: %v", err)
	}
	cleared, err := backend.GetIssue(ctx, cfg.repo, issue)
	if err != nil {
		t.Fatalf("re-reading the cleared issue: %v", err)
	}
	if len(cleared.Labels) != 0 {
		t.Errorf("labels = %v after clearing, want none", cleared.Labels)
	}
}

// TestLive_AnUndefinedLabelIsRefused proves the vocabulary check works against
// the real repository rather than a fixture's idea of one.
func TestLive_AnUndefinedLabelIsRefused(t *testing.T) {
	backend, cfg := liveBackend(t)
	ns := namespace(t)
	issue := createIssue(t, cfg, ns+" undefined label")

	err := backend.SetIssueLabels(context.Background(), cfg.repo, issue, []string{ns + "-never-defined"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// TestLive_CommentRoundTrips covers the write whose response is its own
// read-back.
func TestLive_CommentRoundTrips(t *testing.T) {
	backend, cfg := liveBackend(t)
	ns := namespace(t)
	issue := createIssue(t, cfg, ns+" comment")

	body := "live suite comment " + ns
	comment, err := backend.CommentOnIssue(context.Background(), cfg.repo, issue, body)
	if err != nil {
		t.Fatalf("commenting: %v", err)
	}
	if comment.ID == 0 || comment.Body != body {
		t.Errorf("comment = %+v, want the body that was sent", comment)
	}
}

// TestLive_BranchLifecycleReadsBack covers create, get and delete against a
// server that actually applies them.
func TestLive_BranchLifecycleReadsBack(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	name := ns + "-lifecycle"
	created, err := backend.CreateBranch(ctx, cfg.repo, name, "main")
	if err != nil {
		t.Fatalf("creating the branch: %v", err)
	}
	if created.SHA == "" {
		t.Fatal("the created branch reports no commit")
	}

	got, err := backend.GetBranch(ctx, cfg.repo, name)
	if err != nil {
		t.Fatalf("reading the branch: %v", err)
	}
	if got.SHA != created.SHA {
		t.Errorf("branch moved between create and read: %s -> %s", created.SHA, got.SHA)
	}

	if err := backend.DeleteBranch(ctx, cfg.repo, name); err != nil {
		t.Fatalf("deleting the branch: %v", err)
	}
	if _, err := backend.GetBranch(ctx, cfg.repo, name); !errors.Is(err, ErrNotFound) {
		t.Errorf("error after deletion = %v, want ErrNotFound", err)
	}

	// Deleting again must succeed: cleanup runs after a merge that may already
	// have removed the branch.
	if err := backend.DeleteBranch(ctx, cfg.repo, name); err != nil {
		t.Errorf("deleting an already-absent branch reported failure: %v", err)
	}
}

// TestLive_IssuesEndpointExcludesPullRequests proves the filter works on the
// real server, where a PR genuinely IS an issue.
//
// A fixture can only confirm the query string; this confirms Gitea honours it.
func TestLive_IssuesEndpointExcludesPullRequests(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	base := baseBranch(t, backend, cfg, ns)
	work := ns + "-work"
	if _, err := backend.CreateBranch(ctx, cfg.repo, work, base); err != nil {
		t.Fatalf("cutting the work branch: %v", err)
	}
	commitFile(t, cfg, work, ns+"-pr.txt", "pr\n", "pr change")

	pull, err := backend.CreatePullRequest(ctx, cfg.repo, PullRequestSpec{
		Title: ns + " should not appear as an issue", HeadRef: work, BaseRef: base,
	})
	if err != nil {
		t.Fatalf("opening the pull request: %v", err)
	}

	// Limit is a TOTAL now, not a page size — it used to mean "fifty per page,
	// all pages" and means "at most fifty issues" today. It is kept as a bound
	// rather than dropped because this test only needs to see the pull request
	// absent from a listing, and the scratch repo accumulates issues across
	// runs.
	issues, err := backend.ListIssues(ctx, cfg.repo, IssueFilter{State: "open", Limit: 50})
	if err != nil {
		t.Fatalf("listing issues: %v", err)
	}
	for _, issue := range issues {
		if issue.Number == pull.Number {
			t.Fatalf("pull request #%d was returned as an issue; the dispatcher would work it as one", pull.Number)
		}
	}
}

// closeIssue closes one issue, so a test that opens many does not leave them
// accumulating in the shared scratch repo forever.
func closeIssue(t *testing.T, cfg liveConfig, number int) {
	t.Helper()
	liveRequest(t, cfg, http.MethodPatch,
		repoPath(cfg.repo)+"/issues/"+strconv.Itoa(number),
		map[string]any{"state": "closed"}, nil)
}

// TestLive_AnUnboundedListingPagesPastTheFirstPage is the live proof for
// IssueFilter.Limit == 0.
//
// IT HAS TO BE LIVE. Fixtures test behaviour and live tests test shapes and
// semantics — WP2's split, and paging is squarely the second kind. A fixture
// paginates exactly the way the fixture was written to paginate, so it can only
// confirm the adapter's own arithmetic; what it cannot establish is that GITEA
// honours a `page` parameter on this endpoint at all, that its page size is
// what the adapter assumes, or that a partial page really is how it signals the
// end. Every one of those is a fact about the server, and the single-page bug
// this closes existed precisely because nobody had checked one against a real
// instance.
//
// The scratch repo is shared and accumulates across runs, so the test measures
// what is already there and opens only as many issues as it needs to cross the
// boundary — then closes them again.
func TestLive_AnUnboundedListingPagesPastTheFirstPage(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	existing, err := backend.ListIssues(ctx, cfg.repo, IssueFilter{State: "open"})
	if err != nil {
		t.Fatalf("counting the issues already open: %v", err)
	}

	// Enough to put the total past one page with room to spare, so the result
	// cannot be explained by an off-by-one at the boundary.
	want := giteaDefaultPageSize + 5
	needed := want - len(existing)
	if needed < 5 {
		needed = 5
	}

	opened := make(map[int]bool, needed)
	t.Cleanup(func() {
		for number := range opened {
			closeIssue(t, cfg, number)
		}
	})
	for i := 0; i < needed; i++ {
		opened[createIssue(t, cfg, fmt.Sprintf("%s paging %d", ns, i))] = true
	}

	issues, err := backend.ListIssues(ctx, cfg.repo, IssueFilter{State: "open"})
	if err != nil {
		t.Fatalf("listing every open issue: %v", err)
	}

	// REPORT THE EVIDENCE, because a live test that only says PASS makes
	// whoever reads it re-derive what the server actually did. These three
	// numbers are the proof that more than one page was fetched, and they are
	// what a reviewer needs without opening the repository.
	t.Logf("scratch repo held %d open issues; opened %d more; unbounded listing returned %d across %d page(s) of %d",
		len(existing), len(opened), len(issues),
		(len(issues)+giteaDefaultPageSize-1)/giteaDefaultPageSize, giteaDefaultPageSize)

	// THE PROPERTY: more than one page came back. Under the old single-page
	// behaviour this is exactly where the count stopped.
	if len(issues) <= giteaDefaultPageSize {
		t.Fatalf("an unbounded listing returned %d issues, which is no more than one page of %d — "+
			"either paging did not happen or Gitea ignored the page parameter",
			len(issues), giteaDefaultPageSize)
	}

	// AND NOTHING THIS TEST OPENED WAS MISSED. A count alone would pass on a
	// listing that returned the right number of the wrong issues, and the
	// issues most likely to be dropped are the ones past the first page.
	seen := 0
	for _, issue := range issues {
		if opened[issue.Number] {
			seen++
		}
	}
	if seen != len(opened) {
		t.Fatalf("the listing returned %d of the %d issues this test opened; "+
			"issues past the first page are being dropped", seen, len(opened))
	}
}

// TestLive_APagePastTheEndReturnsEmpty verifies the ONE server fact the
// end-of-listing probe rests on, because the probe's own branch cannot be
// reached against a real repository.
//
// ListIssues asks for one page past its ceiling to tell "this repository holds
// exactly ceiling×pageSize issues" apart from "there are more". That branch
// only executes when every page up to the ceiling came back full, which at the
// shipped constants means fifty thousand open issues — not something to
// manufacture on a scratch repo. So the branch is covered by fixtures and
// mutation, in both directions, and what is verified HERE is the assumption
// underneath it: that Gitea answers a page past the end with an empty array,
// rather than an error, a 404, or a repeat of the last page.
//
// If Gitea answered any of those instead, the probe would misread a finished
// listing as a truncated one and report a healthy repository as a broken forge
// — which is precisely the false-fault this whole area has produced twice
// already. The assumption is the part a fixture cannot establish, so it is the
// part that belongs here.
func TestLive_APagePastTheEndReturnsEmpty(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()

	issues, err := backend.ListIssues(ctx, cfg.repo, IssueFilter{State: "open"})
	if err != nil {
		t.Fatalf("listing every open issue: %v", err)
	}

	// One page past the last one that could hold anything.
	past := (len(issues) / giteaDefaultPageSize) + 2

	var beyond []giteaIssue
	liveRequest(t, cfg, http.MethodGet,
		fmt.Sprintf("%s/issues?state=open&type=issues&limit=%d&page=%d",
			repoPath(cfg.repo), giteaDefaultPageSize, past),
		nil, &beyond)

	t.Logf("repository holds %d open issues; page %d (past the end) returned %d",
		len(issues), past, len(beyond))

	if len(beyond) != 0 {
		t.Fatalf("page %d past the end returned %d issues; the probe would read a finished "+
			"listing as a truncated one and report a healthy repository as broken", past, len(beyond))
	}
}

// TestLive_ABoundedListingStopsAtItsTotal proves the other half of the changed
// meaning against the real server: Limit bounds the TOTAL, not the page.
func TestLive_ABoundedListingStopsAtItsTotal(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	opened := make(map[int]bool, 3)
	t.Cleanup(func() {
		for number := range opened {
			closeIssue(t, cfg, number)
		}
	})
	for i := 0; i < 3; i++ {
		opened[createIssue(t, cfg, fmt.Sprintf("%s bounded %d", ns, i))] = true
	}

	issues, err := backend.ListIssues(ctx, cfg.repo, IssueFilter{State: "open", Limit: 2})
	if err != nil {
		t.Fatalf("listing issues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want the caller's 2 — under the old meaning this was a page size "+
			"and would have returned everything on the first page", len(issues))
	}
	t.Logf("bounded listing returned %d issues for Limit 2, from a repository holding more", len(issues))
}

// TestLive_CheckRunsReadThroughCommitStatuses proves the endpoint answers for a
// real commit.
//
// An empty result is a PASS: the scratch repo has no CI attached, and "no runs
// yet" is the honest answer. What must not happen is an error, which the
// dispatcher would read as an unreachable forge.
func TestLive_CheckRunsReadThroughCommitStatuses(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	branch := ns + "-status"
	if _, err := backend.CreateBranch(ctx, cfg.repo, branch, "main"); err != nil {
		t.Fatalf("creating the branch: %v", err)
	}
	sha := commitFile(t, cfg, branch, ns+"-status.txt", "status\n", "status change")

	runs, err := backend.ListCheckRuns(ctx, cfg.repo, sha)
	if err != nil {
		t.Fatalf("listing check runs: %v", err)
	}
	for _, run := range runs {
		if run.HeadSHA != sha {
			t.Errorf("run %s reports commit %s, want %s", run.ID, run.HeadSHA, sha)
		}
	}
}

// TestLive_CancelCheckRunIsRefusedByThePlatform proves the capability flag
// matches the real server: Gitea publishes no cancel route.
func TestLive_CancelCheckRunIsRefusedByThePlatform(t *testing.T) {
	backend, cfg := liveBackend(t)
	err := backend.CancelCheckRun(context.Background(), cfg.repo, "1")
	if !errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("error = %v, want ErrForgeUnsupported", err)
	}
}

// =============================================================================
// Webhooks
// =============================================================================

// TestLive_EnsureWebhookCreatesThenUpdates covers the hook shapes against the
// real API.
//
// DELIVERY is not exercised here. Verifying a real delivery needs a listener
// the Gitea host can reach, which the runner cannot be relied on to provide;
// creating the hook and confirming the server's own view of it is what this
// suite can honestly assert. The HMAC verification that would authenticate such
// a delivery is covered exhaustively by the fixture suite, where every refusal
// path can be driven — including the ones a real delivery would never produce.
func TestLive_EnsureWebhookCreatesThenUpdates(t *testing.T) {
	backend, cfg := liveBackend(t)
	ctx := context.Background()
	ns := namespace(t)

	// Unique per run: two runs must not fight over one hook, and the URL is
	// what EnsureWebhook matches on.
	url := "http://module.invalid/hook/" + ns
	secret := []byte("live-suite-secret-" + ns)

	if err := backend.EnsureWebhook(ctx, cfg.repo, WebhookSpec{
		URL: url, Events: []string{"issues"},
	}, secret); err != nil {
		t.Fatalf("creating the webhook: %v", err)
	}

	// Ensuring again with different events must UPDATE the existing delivery,
	// not add a second to the same URL — which would double every event.
	if err := backend.EnsureWebhook(ctx, cfg.repo, WebhookSpec{
		URL: url, Events: []string{"issues", "pull_request"},
	}, secret); err != nil {
		t.Fatalf("updating the webhook: %v", err)
	}

	var hooks []struct {
		ID     int64             `json:"id"`
		Active bool              `json:"active"`
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	liveRequest(t, cfg, http.MethodGet, repoPath(cfg.repo)+"/hooks", nil, &hooks)

	matching := 0
	for _, hook := range hooks {
		if hook.Config["url"] == url {
			matching++
			if !hook.Active {
				t.Error("the delivery is inactive; nothing would arrive")
			}
			// Subset: Gitea expands an event into its family (asking for
			// "issues" yields issue_assign, issue_comment, issue_label,
			// issue_milestone and issues), so equality would fail against a
			// server that applied the write correctly.
			if missing := missingEvents([]string{"issues", "pull_request"}, hook.Events); len(missing) > 0 {
				t.Errorf("events = %v, missing %v from the updated set", hook.Events, missing)
			}
		}
	}
	if matching != 1 {
		t.Fatalf("%d deliveries point at %s, want exactly 1", matching, url)
	}

	// Clean up this run's hook: unlike branches and labels, a hook that
	// survives keeps trying to deliver to an unreachable URL forever.
	for _, hook := range hooks {
		if hook.Config["url"] == url {
			liveRequest(t, cfg, http.MethodDelete,
				fmt.Sprintf("%s/hooks/%d", repoPath(cfg.repo), hook.ID), nil, nil)
		}
	}
}
