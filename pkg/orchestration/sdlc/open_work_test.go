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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openWorkForge is a fake forge holding a factory pull request per CI and
// review state, a pull request somebody else opened, and issues with and
// without the module's labels.
func openWorkForge(now time.Time) *fakeForge {
	forge := newFakeForge()
	opened := now.Add(-2 * time.Hour)
	pr := func(number int, author, head string) *ForgePullRequest {
		return &ForgePullRequest{Number: number, Title: "PR " + head, State: "open", Author: author,
			HeadRef: "issue/" + head, HeadSHA: head, Created: opened}
	}
	forge.prs[12] = pr(12, fakeForgeLogin, "sha-green")
	forge.prs[11] = pr(11, "Agent-Fake", "sha-red") // the forge's logins are case-insensitive
	forge.prs[13] = pr(13, fakeForgeLogin, "sha-running")
	forge.prs[14] = pr(14, fakeForgeLogin, "sha-none")
	forge.prs[20] = pr(20, "a-person", "sha-green")
	forge.prs[15] = &ForgePullRequest{Number: 15, State: "closed", Author: fakeForgeLogin, HeadSHA: "sha-green"}

	done := func(name, conclusion string) ForgeCheckRun {
		return ForgeCheckRun{ID: name, Name: name, Status: "completed", Conclusion: conclusion, Completed: now}
	}
	forge.checks["sha-green"] = []ForgeCheckRun{done("ci", "success")}
	forge.checks["sha-red"] = []ForgeCheckRun{done("ci", "failure")}
	forge.checks["sha-running"] = []ForgeCheckRun{{ID: "ci", Name: "ci", Status: "in_progress"}}

	forge.reviews[12] = []ForgeReview{{ID: 1, Reviewer: "codex", State: "APPROVED", HeadSHA: "sha-green", Submitted: now}}
	forge.reviews[11] = []ForgeReview{
		{ID: 2, Reviewer: "codex", State: "APPROVED", HeadSHA: "sha-red", Submitted: now.Add(-time.Minute)},
		{ID: 3, Reviewer: "claude-bot", State: "REQUEST_CHANGES", HeadSHA: "sha-red", Submitted: now},
	}
	// An approval on an OLD head does not approve the current one.
	forge.reviews[13] = []ForgeReview{{ID: 4, Reviewer: "codex", State: "APPROVED", HeadSHA: "sha-old", Submitted: now}}

	issue := func(number int, labels ...string) *ForgeIssue {
		return &ForgeIssue{Number: number, Title: "issue", State: "open", Labels: labels, Created: opened}
	}
	forge.issues[3] = issue(3, "factory:needs-info")
	forge.issues[1] = issue(1, "Task:Blocked")
	forge.issues[2] = issue(2, "task:ready", "bug")
	forge.issues[4] = issue(4, "bug")
	forge.issues[5] = &ForgeIssue{Number: 5, State: "closed", Labels: []string{"factory:blocked"}}
	return forge
}

func TestReadOpenWork_ListsTheAgentsPullRequestsAndLabelledIssues(t *testing.T) {
	now := time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)
	forge := openWorkForge(now)

	work, err := ReadOpenWork(context.Background(), forge, testBinding(), now)
	require.NoError(t, err)

	assert.Equal(t, testProject, work.ProjectID)
	assert.Equal(t, testBinding().Forge.RepoRef().String(), work.Repo)
	assert.Equal(t, fakeForgeLogin, work.Agent)
	assert.Equal(t, now, work.ReadAt)

	type prState struct {
		number int
		ci     CIState
		review ReviewState
	}
	var got []prState
	for _, pr := range work.PullRequests {
		got = append(got, prState{pr.Number, pr.CIState, pr.ReviewState})
		assert.Equal(t, int64(2*60*60), pr.AgeSeconds)
	}
	assert.Equal(t, []prState{
		{11, CIFailure, ReviewChangesRequested},
		{12, CISuccess, ReviewApproved},
		{13, CIPending, ReviewAwaiting},
		{14, CINone, ReviewAwaiting},
	}, got, "only the agent's open pull requests, by number; a person's PR #20 is not factory work")
	assert.Equal(t, "issue/sha-red", work.PullRequests[0].HeadRef)

	var issues []int
	for _, issue := range work.Issues {
		issues = append(issues, issue.Number)
	}
	assert.Equal(t, []int{1, 2, 3}, issues, "held, triggered and module-labelled open issues; an unlabelled or closed one is not")
	assert.Equal(t, []string{"task:ready", "bug"}, work.Issues[1].Labels)
	assert.Equal(t, int64(2*60*60), work.Issues[0].AgeSeconds)
}

func TestReadOpenWork_RefusesMissingInputs(t *testing.T) {
	now := time.Now()
	_, err := ReadOpenWork(context.Background(), nil, testBinding(), now)
	var invalid *InvalidInputError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "forge", invalid.Field)

	_, err = ReadOpenWork(context.Background(), newFakeForge(), nil, now)
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, "binding", invalid.Field)
}

func TestReadOpenWork_AFailedForgeReadFailsTheWholeRead(t *testing.T) {
	now := time.Now()
	failures := map[string]func(*fakeForge) error{
		"identity": func(f *fakeForge) error {
			f.authErr = errors.New("token revoked")
			return f.authErr
		},
		"pull requests": func(f *fakeForge) error {
			f.listOpenPRsErr = errors.New("gitea down")
			return f.listOpenPRsErr
		},
		"checks": func(f *fakeForge) error {
			f.listChecksErr = errors.New("actions down")
			return f.listChecksErr
		},
		"reviews": func(f *fakeForge) error {
			f.listReviewsErr = errors.New("reviews down")
			return f.listReviewsErr
		},
		"issues": func(f *fakeForge) error {
			f.listIssuesErr = errors.New("issues down")
			return f.listIssuesErr
		},
	}
	for name, fail := range failures {
		t.Run(name, func(t *testing.T) {
			forge := openWorkForge(now)
			want := fail(forge)
			work, err := ReadOpenWork(context.Background(), forge, testBinding(), now)
			assert.ErrorIs(t, err, want)
			assert.Nil(t, work, "no partial report")
		})
	}
}

func TestAgeSeconds(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, int64(90), ageSeconds(now.Add(-90*time.Second), now))
	assert.Zero(t, ageSeconds(time.Time{}, now), "an unknown creation time has no age")
	assert.Zero(t, ageSeconds(now.Add(time.Minute), now), "a clock skewed into the future is not a negative age")
}
