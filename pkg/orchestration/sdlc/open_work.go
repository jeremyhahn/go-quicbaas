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
	"sort"
	"strings"
	"sync"
	"time"
)

// A project's OPEN WORK is what the factory has in flight on its forge: the
// pull requests the project's agent opened and has not landed, and the issues
// carrying a label the module writes. It is read from the forge, not from
// quicbaas records, because its purpose is to show an operator (or the
// watchdog) the forge as a person sees it — including work whose task is gone.

// CIState is a pull request head's pipeline state, reduced across its checks
// by the merge gate's own definition of green (evaluateChecks).
type CIState string

const (
	// CISuccess is every judged check on the head finished green.
	CISuccess CIState = "success"

	// CIFailure is a check on the head finished red.
	CIFailure CIState = "failure"

	// CIPending is a check on the head still running.
	CIPending CIState = "pending"

	// CINone is no check covering the head.
	CINone CIState = "none"
)

// ciStates maps the gate's check verdict to the state reported.
var ciStates = map[GateBlockReason]CIState{
	GateClear:                   CISuccess,
	GateBlockedChecksFailed:     CIFailure,
	GateBlockedChecksIncomplete: CIPending,
	GateBlockedNoChecks:         CINone,
}

// ReviewState is a pull request head's review state, reduced across each
// reviewer's operative verdict on the head (latestVerdictsForHead).
type ReviewState string

const (
	// ReviewApproved is at least one approval on the head and no request
	// for changes.
	ReviewApproved ReviewState = "approved"

	// ReviewChangesRequested is a reviewer's standing request for changes on
	// the head.
	ReviewChangesRequested ReviewState = "changes_requested"

	// ReviewAwaiting is no verdict on the head yet.
	ReviewAwaiting ReviewState = "awaiting_review"
)

// OpenPullRequest is one open pull request the project's agent opened.
type OpenPullRequest struct {
	Number      int         `json:"number"`
	Title       string      `json:"title"`
	HeadRef     string      `json:"head_ref"`
	HeadSHA     string      `json:"head_sha"`
	CIState     CIState     `json:"ci_state"`
	ReviewState ReviewState `json:"review_state"`
	URL         string      `json:"url,omitempty"`
	Created     time.Time   `json:"created"`

	// AgeSeconds is how long the pull request has been open when the report
	// was read.
	AgeSeconds int64 `json:"age_seconds"`
}

// OpenIssue is one open issue carrying a label the module writes.
type OpenIssue struct {
	Number  int       `json:"number"`
	Title   string    `json:"title"`
	Labels  []string  `json:"labels"`
	URL     string    `json:"url,omitempty"`
	Created time.Time `json:"created"`

	// AgeSeconds is how long the issue has been open when the report was
	// read.
	AgeSeconds int64 `json:"age_seconds"`
}

// OpenWork is one bound project's open work on its forge.
type OpenWork struct {
	ProjectID string `json:"project_id"`

	// Repo is the bound repository as "owner/repo".
	Repo string `json:"repo"`

	// Agent is the forge account the binding's token authenticates as; the
	// pull requests listed are the ones it opened.
	Agent string `json:"agent"`

	PullRequests []OpenPullRequest `json:"pull_requests"`
	Issues       []OpenIssue       `json:"issues"`
	ReadAt       time.Time         `json:"read_at"`
}

// openWorkFanOut bounds the pull requests whose checks and reviews are read
// at once, so a project with many open pull requests is read quickly without
// flooding its forge.
const openWorkFanOut = 4

// ReadOpenWork reads binding's open work from forge as of now.
//
// WHOLE OR NOTHING. A pull request whose checks or reviews cannot be read
// fails the read rather than being reported in an unknown state: a dashboard
// row that silently says less than the forge knows is the prefix every forge
// listing in this module refuses to return.
func ReadOpenWork(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, now time.Time) (*OpenWork, error) {
	if forge == nil {
		return nil, &InvalidInputError{Field: "forge", Message: "an open-work read needs the project's forge"}
	}
	if binding == nil {
		return nil, &InvalidInputError{Field: "binding", Message: "an open-work read needs the project's binding"}
	}
	repo := binding.Forge.RepoRef()
	agent, err := forge.AuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}
	pulls, err := forge.ListOpenPullRequests(ctx, repo)
	if err != nil {
		return nil, err
	}
	openPulls, err := readAgentPulls(ctx, forge, binding, agent.Username, pulls, now)
	if err != nil {
		return nil, err
	}
	issues, err := forge.ListIssues(ctx, repo, IssueFilter{State: "open"})
	if err != nil {
		return nil, err
	}
	return &OpenWork{
		ProjectID:    binding.ProjectID,
		Repo:         repo.String(),
		Agent:        agent.Username,
		PullRequests: openPulls,
		Issues:       labelledIssues(binding.Forge, issues, now),
		ReadAt:       now.UTC(),
	}, nil
}

// readAgentPulls keeps the pull requests agent opened and reads each one's
// CI and review state, openWorkFanOut at a time, ordered by number.
func readAgentPulls(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, agent string, pulls []ForgePullRequest, now time.Time) ([]OpenPullRequest, error) {
	mine := make([]ForgePullRequest, 0, len(pulls))
	for _, pr := range pulls {
		if strings.EqualFold(pr.Author, agent) {
			mine = append(mine, pr)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Number < mine[j].Number })

	out := make([]OpenPullRequest, len(mine))
	errs := make([]error, len(mine))
	sem := make(chan struct{}, openWorkFanOut)
	var wg sync.WaitGroup
	for i := range mine {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i], errs[i] = readOpenPull(ctx, forge, binding, mine[i], now)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// readOpenPull reads one pull request's CI and review state on its head.
func readOpenPull(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, pr ForgePullRequest, now time.Time) (OpenPullRequest, error) {
	repo := binding.Forge.RepoRef()
	runs, err := forge.ListCheckRuns(ctx, repo, pr.HeadSHA)
	if err != nil {
		return OpenPullRequest{}, err
	}
	reviews, err := forge.ListReviews(ctx, repo, pr.Number)
	if err != nil {
		return OpenPullRequest{}, err
	}
	return OpenPullRequest{
		Number:      pr.Number,
		Title:       pr.Title,
		HeadRef:     pr.HeadRef,
		HeadSHA:     pr.HeadSHA,
		CIState:     ciStateOf(runs, pr.HeadSHA, now),
		ReviewState: reviewStateOf(reviews, binding.Review, pr.HeadSHA),
		URL:         pr.URL,
		Created:     pr.Created.UTC(),
		AgeSeconds:  ageSeconds(pr.Created, now),
	}, nil
}

// ciStateOf reduces a head's check runs to one state, by the merge gate's
// definition of green.
func ciStateOf(runs []ForgeCheckRun, headSHA string, now time.Time) CIState {
	reason, _ := evaluateChecks(checkRecordsFor(runs, headSHA, now), headSHA)
	return ciStates[reason]
}

// reviewStateOf reduces a pull request's reviews to one state on its head: a
// standing request for changes outranks any approval.
func reviewStateOf(reviews []ForgeReview, policy ReviewPolicy, headSHA string) ReviewState {
	records := make([]ReviewRecord, 0, len(reviews))
	for _, review := range reviews {
		if record, ok := reviewRecordFor(review, policy, 0); ok {
			records = append(records, record)
		}
	}
	state := ReviewAwaiting
	for _, verdict := range latestVerdictsForHead(records, headSHA) {
		switch verdict.Verdict {
		case VerdictRequestChanges:
			return ReviewChangesRequested
		case VerdictApprove:
			state = ReviewApproved
		}
	}
	return state
}

// labelledIssues keeps the open issues carrying a label the module writes for
// the binding (its display labels and trigger) or the portfolio's hold label,
// ordered by number.
func labelledIssues(cfg ForgeConfig, issues []ForgeIssue, now time.Time) []OpenIssue {
	owned := cfg.OwnedLabels()
	owned[strings.ToLower(HoldLabel)] = true
	out := make([]OpenIssue, 0, len(issues))
	for _, issue := range issues {
		if !carriesAny(issue.Labels, owned) {
			continue
		}
		out = append(out, OpenIssue{
			Number:     issue.Number,
			Title:      issue.Title,
			Labels:     append([]string(nil), issue.Labels...),
			URL:        issue.URL,
			Created:    issue.Created.UTC(),
			AgeSeconds: ageSeconds(issue.Created, now),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// carriesAny reports whether labels holds any label in owned, compared
// case-insensitively.
func carriesAny(labels []string, owned map[string]bool) bool {
	for _, label := range labels {
		if owned[strings.ToLower(strings.TrimSpace(label))] {
			return true
		}
	}
	return false
}

// ageSeconds is how long ago since was, as of now; never negative.
func ageSeconds(since, now time.Time) int64 {
	if since.IsZero() || now.Before(since) {
		return 0
	}
	return int64(now.Sub(since) / time.Second)
}
