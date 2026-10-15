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
)

// memoryBranches is an in-memory forgeBranches: branch name to head SHA.
type memoryBranches struct {
	heads   map[string]string
	readErr error
	deleted []string
}

func (m *memoryBranches) GetBranch(_ context.Context, _ RepoRef, name string) (*ForgeBranch, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	sha, ok := m.heads[name]
	if !ok {
		return nil, &NotFoundError{Entity: "branch", ID: name}
	}
	return &ForgeBranch{Name: name, SHA: sha}, nil
}

func (m *memoryBranches) DeleteBranch(_ context.Context, _ RepoRef, name string) error {
	m.deleted = append(m.deleted, name)
	delete(m.heads, name)
	return nil
}

// TestDeleteMergedSource_DeletesOnlyAnUnmovedBranchOfThisRepository proves the
// source branch is deleted when it still points at the merged head, and left
// alone when it moved, belongs to a fork, or the spec did not ask.
func TestDeleteMergedSource_DeletesOnlyAnUnmovedBranchOfThisRepository(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	merged := &ForgePullRequest{HeadRepo: repo, HeadRef: "feature", HeadSHA: "abc123"}
	ask := MergeSpec{ExpectSourceDeleted: true}

	branches := &memoryBranches{heads: map[string]string{"feature": "abc123"}}
	if err := deleteMergedSource(context.Background(), branches, repo, merged, ask); err != nil {
		t.Fatalf("deleteMergedSource: %v", err)
	}
	if len(branches.deleted) != 1 {
		t.Fatalf("deleted %v, want the source branch", branches.deleted)
	}

	for name, tc := range map[string]struct {
		pull *ForgePullRequest
		spec MergeSpec
		head string
	}{
		"moved":     {pull: merged, spec: ask, head: "def456"},
		"fork":      {pull: &ForgePullRequest{HeadRepo: RepoRef{Owner: "fork", Repo: "widgets"}, HeadRef: "feature", HeadSHA: "abc123"}, spec: ask, head: "abc123"},
		"not asked": {pull: merged, spec: MergeSpec{}, head: "abc123"},
	} {
		branches := &memoryBranches{heads: map[string]string{"feature": tc.head}}
		if err := deleteMergedSource(context.Background(), branches, repo, tc.pull, tc.spec); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(branches.deleted) != 0 {
			t.Errorf("%s: deleted %v, want nothing", name, branches.deleted)
		}
	}
}

// TestDeleteMergedSource_ReportsAnUnreadableBranch proves a branch read that
// fails for any reason but absence is reported, while an absent branch — the
// forge already deleted it — is success.
func TestDeleteMergedSource_ReportsAnUnreadableBranch(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	merged := &ForgePullRequest{HeadRepo: repo, HeadRef: "feature", HeadSHA: "abc123"}
	ask := MergeSpec{ExpectSourceDeleted: true}

	outage := errors.New("forge unreachable")
	if err := deleteMergedSource(context.Background(), &memoryBranches{readErr: outage}, repo, merged, ask); !errors.Is(err, outage) {
		t.Errorf("err = %v, want the read failure", err)
	}
	if err := deleteMergedSource(context.Background(), &memoryBranches{heads: map[string]string{}}, repo, merged, ask); err != nil {
		t.Errorf("an absent branch: %v, want success", err)
	}
}

// TestVerifyCommitStatus_ConfirmsTheNewestStatusOfTheContext proves the
// read-back accepts the newest status under the written context and ignores
// a check run that merely shares the name.
func TestVerifyCommitStatus_ConfirmsTheNewestStatusOfTheContext(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	written := CommitStatus{Context: "quicbaas/review", Status: CheckSuccess}
	earlier := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	runs := []ForgeCheckRun{
		{ID: "status:1", Name: "quicbaas/review", Status: "completed", Conclusion: "failure", Completed: earlier},
		{ID: "status:2", Name: "quicbaas/review", Status: "completed", Conclusion: "success", Completed: earlier.Add(time.Minute)},
		{ID: "77", Name: "quicbaas/review", Status: "completed", Conclusion: "failure", Completed: earlier.Add(time.Hour)},
	}
	if err := verifyCommitStatus(ForgeGitLab, repo, "abc123", written, runs); err != nil {
		t.Fatalf("verifyCommitStatus: %v", err)
	}
}

// TestVerifyCommitStatus_RefusesAMissingOrDifferentStatus proves a status the
// commit does not carry, or carries in another state, is reported as a write
// that did not land.
func TestVerifyCommitStatus_RefusesAMissingOrDifferentStatus(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "widgets"}
	written := CommitStatus{Context: "quicbaas/review", Status: CheckSuccess}
	for name, runs := range map[string][]ForgeCheckRun{
		"missing":   {{ID: "status:1", Name: "other", Status: "completed", Conclusion: "success"}},
		"different": {{ID: "status:1", Name: "quicbaas/review", Status: "in_progress"}},
	} {
		err := verifyCommitStatus(ForgeGitLab, repo, "abc123", written, runs)
		var notApplied *ForgeWriteNotAppliedError
		if !errors.As(err, &notApplied) || notApplied.Type != ForgeGitLab {
			t.Errorf("%s: err = %v, want ForgeWriteNotAppliedError from gitlab", name, err)
		}
	}
}
