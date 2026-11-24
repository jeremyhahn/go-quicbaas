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
	"slices"
	"strings"
)

// ForgeBranchInventory is the part of a forge that a branch sweep reads:
// every branch in a repository, and whether one commit is already carried by
// another ref.
//
// It is kept off ForgeBackend on purpose. Adding the methods there would
// break every fake and every adapter that does not serve them yet (Gitea,
// GitLab). A sweep asserts the interface on the backend it is given and
// refuses a forge without it with ErrBranchInventoryUnsupported.
type ForgeBranchInventory interface {
	// ListBranches returns every branch with its tip. It returns all of
	// them or an error, never a partial list.
	ListBranches(ctx context.Context, repo RepoRef) ([]ForgeBranch, error)

	// BranchContained reports whether base already carries headSHA, so
	// that deleting a branch at headSHA loses no commit.
	BranchContained(ctx context.Context, repo RepoRef, base, headSHA string) (bool, error)

	// DeleteBranchAt deletes name only while it points at sha, a condition
	// the forge enforces in the same operation as the delete. It returns
	// an error wrapping ErrBranchMoved when the branch points elsewhere,
	// and one wrapping ErrNotFound when it is already gone.
	DeleteBranchAt(ctx context.Context, repo RepoRef, name, sha string) error
}

// ErrBranchInventoryUnsupported is returned by SweepBranches when the forge
// does not implement ForgeBranchInventory.
var ErrBranchInventoryUnsupported = errors.New("forge does not serve a branch inventory")

// ErrBranchMoved is returned by DeleteBranchAt when the branch no longer
// points at the tip it was asked to delete.
var ErrBranchMoved = errors.New("branch moved from the expected tip")

// FactoryBranchPrefixes are the prefixes the factory names its branches
// under (see issueBranch). A sweep looks at no other branch, so the default
// branch, develop, the merge train's queue/ branches and people's own
// branches are never deleted or counted.
var FactoryBranchPrefixes = []string{"issue/", "feature/", "hotfix/"}

// Reasons a sweep keeps a factory branch.
const (
	BranchKeptOpenPullRequest = "open_pull_request"
	BranchKeptLiveLane        = "live_lane"
	BranchKeptMoved           = "moved"
)

// BranchSweepInput is what a sweep needs to know beyond the forge.
type BranchSweepInput struct {
	// DefaultBranch is the branch whose history makes a branch carried.
	DefaultBranch string
	// OpenPullRequests are the repository's open pull requests. Their heads
	// are kept, and a branch their head commits carry is deleted.
	OpenPullRequests []ForgePullRequest
	// LiveBranches are the branches of lanes still running.
	LiveBranches []string
	// Prefixes limit the sweep. Empty means FactoryBranchPrefixes.
	Prefixes []string
}

// BranchKept is a factory branch the sweep left in place, and why.
type BranchKept struct {
	Name   string
	Reason string
}

// BranchSweepFailure is a carried branch whose delete failed.
type BranchSweepFailure struct {
	Name string
	Err  error
}

// BranchSweep is the outcome of one sweep. Every slice is sorted by
// branch name.
type BranchSweep struct {
	Deleted  []string
	Kept     []BranchKept
	Stranded []ForgeBranch
	Failed   []BranchSweepFailure
}

// StrandedCount is the number of factory branches that are not an open
// pull request head, not a live lane, and not carried by the default branch
// or by any open pull request head.
func (s *BranchSweep) StrandedCount() int {
	if s == nil {
		return 0
	}
	return len(s.Stranded)
}

// SweepBranches deletes the factory branches that the default branch or an
// open pull request head already carries, and counts the rest as stranded.
//
// A branch is deleted under a lease. It is read again just before the
// delete, and if its tip has moved since the listing it is kept. A delete
// that fails is recorded in Failed and the sweep goes on. A listing or
// containment error fails the whole sweep, because a count built on a
// partial read would report fewer branches than the forge holds.
func SweepBranches(ctx context.Context, forge ForgeBackend, repo RepoRef, in BranchSweepInput) (*BranchSweep, error) {
	if in.DefaultBranch == "" {
		return nil, &InvalidInputError{Field: "default_branch", Message: "a branch sweep needs the default branch"}
	}
	inventory, ok := forge.(ForgeBranchInventory)
	if !ok {
		return nil, ErrBranchInventoryUnsupported
	}
	prefixes := in.Prefixes
	if len(prefixes) == 0 {
		prefixes = FactoryBranchPrefixes
	}

	prHeads := map[string]bool{}
	var carriers []string
	for i := range in.OpenPullRequests {
		pr := &in.OpenPullRequests[i]
		if pr.HeadRef != "" && sameRepo(pr.HeadRepo, repo) {
			prHeads[pr.HeadRef] = true
		}
		if pr.HeadSHA != "" && !slices.Contains(carriers, pr.HeadSHA) {
			carriers = append(carriers, pr.HeadSHA)
		}
	}
	live := map[string]bool{}
	for _, name := range in.LiveBranches {
		live[name] = true
	}

	branches, err := inventory.ListBranches(ctx, repo)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(branches, func(a, b ForgeBranch) int { return strings.Compare(a.Name, b.Name) })

	sweep := &BranchSweep{}
	for _, branch := range branches {
		if !hasAnyPrefix(branch.Name, prefixes) {
			continue
		}
		switch {
		case prHeads[branch.Name]:
			sweep.Kept = append(sweep.Kept, BranchKept{Name: branch.Name, Reason: BranchKeptOpenPullRequest})
			continue
		case live[branch.Name]:
			sweep.Kept = append(sweep.Kept, BranchKept{Name: branch.Name, Reason: BranchKeptLiveLane})
			continue
		}

		carried, err := branchCarried(ctx, inventory, repo, in.DefaultBranch, carriers, branch.SHA)
		if err != nil {
			return nil, err
		}
		if !carried {
			sweep.Stranded = append(sweep.Stranded, branch)
			continue
		}
		deleteCarriedBranch(ctx, forge, repo, branch, sweep)
	}
	return sweep, nil
}

// branchCarried reports whether the default branch or any open pull
// request head carries sha.
func branchCarried(ctx context.Context, inventory ForgeBranchInventory, repo RepoRef, defaultBranch string, carriers []string, sha string) (bool, error) {
	if sha == "" {
		return false, nil
	}
	for _, base := range append([]string{defaultBranch}, carriers...) {
		contained, err := inventory.BranchContained(ctx, repo, base, sha)
		if err != nil {
			return false, err
		}
		if contained {
			return true, nil
		}
	}
	return false, nil
}

// deleteCarriedBranch deletes branch only while it still points at the
// listed tip, and records the outcome on sweep.
func deleteCarriedBranch(ctx context.Context, forge ForgeBackend, repo RepoRef, branch ForgeBranch, sweep *BranchSweep) {
	current, err := forge.GetBranch(ctx, repo, branch.Name)
	switch {
	case errors.Is(err, ErrNotFound):
		sweep.Deleted = append(sweep.Deleted, branch.Name)
		return
	case err != nil:
		sweep.Failed = append(sweep.Failed, BranchSweepFailure{Name: branch.Name, Err: err})
		return
	case current == nil || current.SHA != branch.SHA:
		sweep.Kept = append(sweep.Kept, BranchKept{Name: branch.Name, Reason: BranchKeptMoved})
		return
	}
	if err := forge.DeleteBranch(ctx, repo, branch.Name); err != nil && !errors.Is(err, ErrNotFound) {
		sweep.Failed = append(sweep.Failed, BranchSweepFailure{Name: branch.Name, Err: err})
		return
	}
	sweep.Deleted = append(sweep.Deleted, branch.Name)
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
