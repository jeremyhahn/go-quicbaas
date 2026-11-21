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
	"slices"
	"testing"
)

var sweepRepo = RepoRef{Owner: "acme", Repo: "widgets"}

// sweepForge is a forge with a branch inventory over maps. The embedded
// ForgeBackend is nil: a sweep reaching any other method panics the test.
type sweepForge struct {
	ForgeBackend

	branches []ForgeBranch
	listErr  error

	// contained holds "base...sha" pairs BranchContained reports true for;
	// containErr is what it answers instead.
	contained  map[string]bool
	containErr error

	// moved is the tip GetBranch reports in place of the listed one.
	moved map[string]string

	deleteErr map[string]error
	deleted   []string
}

func (f *sweepForge) ListBranches(context.Context, RepoRef) ([]ForgeBranch, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return slices.Clone(f.branches), nil
}

func (f *sweepForge) BranchContained(_ context.Context, _ RepoRef, base, headSHA string) (bool, error) {
	if f.containErr != nil {
		return false, f.containErr
	}
	return f.contained[base+"..."+headSHA], nil
}

func (f *sweepForge) GetBranch(_ context.Context, _ RepoRef, name string) (*ForgeBranch, error) {
	if sha, ok := f.moved[name]; ok {
		return &ForgeBranch{Name: name, SHA: sha}, nil
	}
	for _, b := range f.branches {
		if b.Name == name && !slices.Contains(f.deleted, name) {
			return &ForgeBranch{Name: b.Name, SHA: b.SHA}, nil
		}
	}
	return nil, fmt.Errorf("branch %s: %w", name, ErrNotFound)
}

func (f *sweepForge) DeleteBranch(_ context.Context, _ RepoRef, name string) error {
	if err := f.deleteErr[name]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, name)
	return nil
}

func sweepOrFail(t *testing.T, forge ForgeBackend, in BranchSweepInput) *BranchSweep {
	t.Helper()
	if in.DefaultBranch == "" {
		in.DefaultBranch = "main"
	}
	sweep, err := SweepBranches(context.Background(), forge, sweepRepo, in)
	if err != nil {
		t.Fatalf("SweepBranches: %v", err)
	}
	return sweep
}

func keptNames(sweep *BranchSweep) []string {
	var names []string
	for _, k := range sweep.Kept {
		names = append(names, k.Name)
	}
	return names
}

func TestSweepBranches_DeletesBranchCarriedByMain(t *testing.T) {
	forge := &sweepForge{
		branches:  []ForgeBranch{{Name: "issue/12", SHA: "a1"}},
		contained: map[string]bool{"main...a1": true},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{})
	if !slices.Equal(sweep.Deleted, []string{"issue/12"}) || !slices.Equal(forge.deleted, []string{"issue/12"}) {
		t.Fatalf("deleted = %v (forge %v), want issue/12", sweep.Deleted, forge.deleted)
	}
	if sweep.StrandedCount() != 0 || len(sweep.Kept) != 0 || len(sweep.Failed) != 0 {
		t.Errorf("sweep = %+v, want only the delete", sweep)
	}
}

func TestSweepBranches_DeletesBranchCarriedByOpenPullRequestHead(t *testing.T) {
	forge := &sweepForge{
		branches: []ForgeBranch{
			{Name: "issue/12", SHA: "pr-head"},
			{Name: "issue/12-ckpt", SHA: "c1"},
		},
		contained: map[string]bool{"pr-head...c1": true},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{OpenPullRequests: []ForgePullRequest{
		{Number: 5, HeadRef: "issue/12", HeadSHA: "pr-head", HeadRepo: sweepRepo},
	}})
	if !slices.Equal(forge.deleted, []string{"issue/12-ckpt"}) {
		t.Fatalf("deleted = %v, want the checkpoint the pull request head carries", forge.deleted)
	}
	if !slices.Equal(keptNames(sweep), []string{"issue/12"}) {
		t.Errorf("kept = %+v, want the pull request head", sweep.Kept)
	}
}

func TestSweepBranches_KeepsOpenPullRequestHead(t *testing.T) {
	forge := &sweepForge{
		branches:  []ForgeBranch{{Name: "issue/7", SHA: "h7"}},
		contained: map[string]bool{"main...h7": true},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{OpenPullRequests: []ForgePullRequest{
		{Number: 7, HeadRef: "issue/7", HeadSHA: "h7", HeadRepo: sweepRepo},
	}})
	if len(forge.deleted) != 0 {
		t.Fatalf("deleted = %v, want an open pull request head kept even when main carries it", forge.deleted)
	}
	if len(sweep.Kept) != 1 || sweep.Kept[0] != (BranchKept{Name: "issue/7", Reason: BranchKeptOpenPullRequest}) {
		t.Errorf("kept = %+v", sweep.Kept)
	}
}

func TestSweepBranches_KeepsLiveLaneBranch(t *testing.T) {
	forge := &sweepForge{
		branches:  []ForgeBranch{{Name: "feature/9", SHA: "f9"}},
		contained: map[string]bool{"main...f9": true},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{LiveBranches: []string{"feature/9"}})
	if len(forge.deleted) != 0 {
		t.Fatalf("deleted = %v, want the live lane kept", forge.deleted)
	}
	if len(sweep.Kept) != 1 || sweep.Kept[0] != (BranchKept{Name: "feature/9", Reason: BranchKeptLiveLane}) {
		t.Errorf("kept = %+v", sweep.Kept)
	}
}

func TestSweepBranches_CountsStrandedBranch(t *testing.T) {
	forge := &sweepForge{branches: []ForgeBranch{{Name: "hotfix/3", SHA: "s3"}}}
	sweep := sweepOrFail(t, forge, BranchSweepInput{})
	if sweep.StrandedCount() != 1 || sweep.Stranded[0] != (ForgeBranch{Name: "hotfix/3", SHA: "s3"}) {
		t.Fatalf("stranded = %+v, want hotfix/3", sweep.Stranded)
	}
	if len(forge.deleted) != 0 || len(sweep.Deleted) != 0 {
		t.Errorf("deleted = %v, want nothing deleted", forge.deleted)
	}
}

func TestSweepBranches_LeaseKeepsMovedBranch(t *testing.T) {
	forge := &sweepForge{
		branches:  []ForgeBranch{{Name: "issue/4", SHA: "old"}},
		contained: map[string]bool{"main...old": true},
		moved:     map[string]string{"issue/4": "new"},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{})
	if len(forge.deleted) != 0 {
		t.Fatalf("deleted = %v, want a moved branch kept", forge.deleted)
	}
	if len(sweep.Kept) != 1 || sweep.Kept[0] != (BranchKept{Name: "issue/4", Reason: BranchKeptMoved}) {
		t.Errorf("kept = %+v", sweep.Kept)
	}
}

func TestSweepBranches_IgnoresBranchesOutsidePrefixes(t *testing.T) {
	forge := &sweepForge{
		branches: []ForgeBranch{
			{Name: "main", SHA: "m"}, {Name: "develop", SHA: "d"},
			{Name: "queue/7", SHA: "q"}, {Name: "me/spike", SHA: "s"},
		},
		contained: map[string]bool{"main...m": true, "main...d": true, "main...q": true},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{})
	if len(forge.deleted) != 0 || len(sweep.Deleted) != 0 || len(sweep.Kept) != 0 ||
		sweep.StrandedCount() != 0 || len(sweep.Failed) != 0 {
		t.Fatalf("sweep = %+v (deleted %v), want nothing touched or counted", sweep, forge.deleted)
	}
}

func TestSweepBranches_DeleteFailureIsRecordedAndSweepContinues(t *testing.T) {
	refused := errors.New("forbidden")
	forge := &sweepForge{
		branches:  []ForgeBranch{{Name: "issue/1", SHA: "a"}, {Name: "issue/2", SHA: "b"}},
		contained: map[string]bool{"main...a": true, "main...b": true},
		deleteErr: map[string]error{"issue/1": refused},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{})
	if len(sweep.Failed) != 1 || sweep.Failed[0].Name != "issue/1" || !errors.Is(sweep.Failed[0].Err, refused) {
		t.Fatalf("failed = %+v, want issue/1 refused", sweep.Failed)
	}
	if !slices.Equal(sweep.Deleted, []string{"issue/2"}) {
		t.Errorf("deleted = %v, want the sweep to go on to issue/2", sweep.Deleted)
	}
}

func TestSweepBranches_ListingErrorFailsWhole(t *testing.T) {
	listing := errors.New("listing cut short")
	forge := &sweepForge{listErr: listing}
	sweep, err := SweepBranches(context.Background(), forge, sweepRepo, BranchSweepInput{DefaultBranch: "main"})
	if !errors.Is(err, listing) || sweep != nil {
		t.Fatalf("sweep = %+v, err = %v, want the listing error and no sweep", sweep, err)
	}

	contain := errors.New("compare failed")
	forge = &sweepForge{branches: []ForgeBranch{{Name: "issue/1", SHA: "a"}}, containErr: contain}
	if _, err := SweepBranches(context.Background(), forge, sweepRepo, BranchSweepInput{DefaultBranch: "main"}); !errors.Is(err, contain) {
		t.Errorf("err = %v, want the containment error", err)
	}
	if len(forge.deleted) != 0 {
		t.Errorf("deleted = %v, want nothing", forge.deleted)
	}

	if _, err := SweepBranches(context.Background(), forge, sweepRepo, BranchSweepInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, want an empty default branch refused", err)
	}
}

func TestSweepBranches_RefusesForgeWithoutInventory(t *testing.T) {
	_, err := SweepBranches(context.Background(), &fakeForge{}, sweepRepo, BranchSweepInput{DefaultBranch: "main"})
	if !errors.Is(err, ErrBranchInventoryUnsupported) {
		t.Fatalf("err = %v, want ErrBranchInventoryUnsupported", err)
	}
}

func TestSweepBranches_EndStateIsOpenPullRequestsPlusLiveLanes(t *testing.T) {
	fork := RepoRef{Owner: "someone", Repo: "widgets"}
	forge := &sweepForge{
		branches: []ForgeBranch{
			{Name: "main", SHA: "m"},
			{Name: "queue/3", SHA: "q"},
			{Name: "issue/10", SHA: "h10"},
			{Name: "issue/10-ckpt-1", SHA: "c10"},
			{Name: "issue/11", SHA: "l11"},
			{Name: "issue/12", SHA: "merged12"},
			{Name: "feature/13", SHA: "h13"},
			{Name: "hotfix/14", SHA: "merged14"},
			{Name: "issue/15", SHA: "fork15"},
		},
		contained: map[string]bool{
			"h10...c10":       true,
			"main...merged12": true,
			"main...merged14": true,
			"h13...fork15":    true,
		},
	}
	sweep := sweepOrFail(t, forge, BranchSweepInput{
		OpenPullRequests: []ForgePullRequest{
			{Number: 10, HeadRef: "issue/10", HeadSHA: "h10", HeadRepo: sweepRepo},
			{Number: 13, HeadRef: "feature/13", HeadSHA: "h13", HeadRepo: sweepRepo},
			// A fork's branch of the same name is not this repository's.
			{Number: 16, HeadRef: "issue/15", HeadSHA: "f16", HeadRepo: fork},
		},
		LiveBranches: []string{"issue/11"},
	})

	if want := []string{"feature/13", "issue/10", "issue/11"}; !slices.Equal(keptNames(sweep), want) {
		t.Errorf("kept = %+v, want the pull request heads plus the live lane %v", sweep.Kept, want)
	}
	if want := []string{"hotfix/14", "issue/10-ckpt-1", "issue/12", "issue/15"}; !slices.Equal(sweep.Deleted, want) {
		t.Errorf("deleted = %v, want %v", sweep.Deleted, want)
	}
	if sweep.StrandedCount() != 0 || len(sweep.Failed) != 0 {
		t.Errorf("stranded = %+v, failed = %+v, want none", sweep.Stranded, sweep.Failed)
	}
}
