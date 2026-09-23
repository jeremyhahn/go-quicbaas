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
	"reflect"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Closing references
// =============================================================================

// testForgeBase is the fixture repository's web address.
const testForgeBase = "https://forge.test/acme/api"

// TestClosingIssueNumbers_AURLCountsOnlyOnTheBoundForge: an issue URL names a
// forge as well as a repository. The same owner/repo on another host is a
// different issue. A forge served under a subpath matches only with that
// subpath, and with no known address no URL reference counts.
func TestClosingIssueNumbers_AURLCountsOnlyOnTheBoundForge(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "api"}
	text := "Closes https://git.corp/gitea/acme/api/issues/5 and fixes https://git.corp/acme/api/issues/6"
	if got := ClosingIssueNumbers(repo, "https://git.corp/gitea/acme/api/", text); !reflect.DeepEqual(got, []int{5}) {
		t.Fatalf("under a subpath: got %v, want [5]", got)
	}
	if got := ClosingIssueNumbers(repo, "", "Closes https://forge.test/acme/api/issues/7 and closes #8"); !reflect.DeepEqual(got, []int{8}) {
		t.Fatalf("no forge address: got %v, want only the plain reference", got)
	}
}

func TestClosingIssueNumbers_ReadsEveryClosingKeyword(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "api"}
	cases := map[string][]int{
		"Closes #1048":            {1048},
		"closed #1":               {1},
		"close #2":                {2},
		"Fixes #3":                {3},
		"fixed #4 and FIX #5":     {4, 5},
		"fix: #6":                 {6},
		"Resolves #7":             {7},
		"resolved #8, resolve #8": {8},
		"Closes acme/api#9":       {9},
		"Closes ACME/API#10":      {10},
		"Closes https://forge.test/acme/api/issues/11": {11},
		"Closes http://FORGE.test/acme/api/issues/12":  {12},
	}
	for text, want := range cases {
		if got := ClosingIssueNumbers(repo, testForgeBase, text); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}

func TestClosingIssueNumbers_IgnoresWhatClosesNothingHere(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "api"}
	for _, text := range []string{
		"",
		"See #12 for context", // a mention, not a closing keyword
		"prefixes #13",        // "fixes" inside another word
		"Closes other/api#14", // another repository's issue
		"Closes https://forge.test/other/api/issues/15", // the same, by URL
		"Closes #0", // not an issue number
		"Closes the gap described in the thread #16 references", // the keyword names no issue
	} {
		if got := ClosingIssueNumbers(repo, testForgeBase, text); len(got) != 0 {
			t.Errorf("%q: got %v, want nothing", text, got)
		}
	}
}

func TestRepoWebURL_IsReadOffTheForgesOwnIssueURLs(t *testing.T) {
	issues := []ForgeIssue{
		{Number: 3, URL: ""},
		{Number: 4, URL: "https://git.corp/gitea/acme/api/pulls/4"},
		{Number: 5, URL: "https://git.corp/gitea/acme/api/issues/5"},
	}
	if got := repoWebURL(issues); got != "https://git.corp/gitea/acme/api" {
		t.Fatalf("repoWebURL = %q", got)
	}
	if got := repoWebURL(issues[:2]); got != "" {
		t.Fatalf("no issue URL of the right shape: got %q, want empty", got)
	}
}

func TestClosingPullRequests_NamesTheLowestNumberedClaim(t *testing.T) {
	repo := RepoRef{Owner: "acme", Repo: "api"}
	got := closingPullRequests(repo, testForgeBase, []ForgePullRequest{
		{Number: 1052, Title: "duplicate", Body: "Closes #1048"},
		{Number: 1049, Title: "Fixes #1048", Body: "and resolves #1047"},
		{Number: 1060, Body: "unrelated"},
	})
	want := map[int]int{1048: 1049, 1047: 1049}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// =============================================================================
// The issue source skips an issue a pull request already closes
// =============================================================================

type recordingAdoptionAuditor struct {
	mu    sync.Mutex
	skips []AdoptionSkip
}

func (a *recordingAdoptionAuditor) RecordAdoptionSkip(_ context.Context, skip AdoptionSkip) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.skips = append(a.skips, skip)
}

func (a *recordingAdoptionAuditor) recorded() []AdoptionSkip {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AdoptionSkip(nil), a.skips...)
}

func offeredRefs(specs []orchestration.TaskSourceSpec) []string {
	refs := make([]string, 0, len(specs))
	for _, spec := range specs {
		refs = append(refs, spec.Source.Ref)
	}
	return refs
}

// TestIssueSource_AnIssueAnOpenPullRequestClosesIsNotAdopted is the incident:
// #1049 said "Closes #1048" and #1048 was adopted anyway. It is skipped, the
// skip is audited once with the pull request's number however many sweeps
// see it, and when the pull request closes without merging the issue is
// offered on the very next sweep.
func TestIssueSource_AnIssueAnOpenPullRequestClosesIsNotAdopted(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	auditor := &recordingAdoptionAuditor{}
	source.SetAdoptionAuditor(auditor)
	forge.putIssue(1048, "Add the thing", "task:ready")
	forge.putIssue(1050, "Another thing", "task:ready")
	forge.prs[1049] = &ForgePullRequest{Number: 1049, Title: "Add the thing", Body: "Closes #1048", State: "open"}
	ctx := context.Background()
	project := testOrchProject()

	for sweep := 1; sweep <= 2; sweep++ {
		specs, err := source.Poll(ctx, project)
		if err != nil {
			t.Fatalf("sweep %d: %v", sweep, err)
		}
		if refs := offeredRefs(specs); !reflect.DeepEqual(refs, []string{"acme/api#1050"}) {
			t.Fatalf("sweep %d offered %v, want only the issue no pull request closes", sweep, refs)
		}
	}
	want := []AdoptionSkip{{TenantID: project.TenantID, ProjectID: project.ID, Issue: "acme/api#1048", PullRequest: 1049}}
	if got := auditor.recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("audited %+v, want the one skip naming pull request #1049", got)
	}

	// The pull request closes WITHOUT merging: nobody is delivering #1048 now.
	forge.prs[1049].State = "closed"
	specs, err := source.Poll(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	if refs := offeredRefs(specs); !reflect.DeepEqual(refs, []string{"acme/api#1048", "acme/api#1050"}) {
		t.Fatalf("offered %v, want #1048 back once its pull request closed unmerged", refs)
	}
}

// TestIssueSource_AReopenedIssueWhosePullRequestMergedIsAdopted: only an open
// pull request holds its issue. The change closing #25 merges and closes the
// issue; a person reopens it because the fix was incomplete. The merged pull
// request is history, and holding the reopened issue for it would mean the
// issue is never adopted again.
func TestIssueSource_AReopenedIssueWhosePullRequestMergedIsAdopted(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	auditor := &recordingAdoptionAuditor{}
	source.SetAdoptionAuditor(auditor)
	issue := forge.putIssue(25, "Add the thing", "task:ready")
	forge.prs[30] = &ForgePullRequest{Number: 30, Body: "Fixes #25", State: "open"}
	ctx := context.Background()
	project := testOrchProject()

	if specs, err := source.Poll(ctx, project); err != nil || len(specs) != 0 {
		t.Fatalf("while the pull request is open: specs=%v err=%v", offeredRefs(specs), err)
	}

	// Between two sweeps the change merges, the forge closes the issue with
	// it, and a person reopens it because the fix was incomplete. The next
	// sweep sees the same open issue it skipped, and the pull request that
	// held it gone from the open listing. The merged pull request still says
	// "Fixes #25", but it holds nothing.
	forge.prs[30].State = "closed"
	forge.prs[30].Merged = true
	issue.State = "closed"
	issue.State = "open"
	specs, err := source.Poll(ctx, project)
	if err != nil || !reflect.DeepEqual(offeredRefs(specs), []string{"acme/api#25"}) {
		t.Fatalf("specs=%v err=%v, want the reopened issue offered", offeredRefs(specs), err)
	}
	want := []AdoptionSkip{{TenantID: project.TenantID, ProjectID: project.ID, Issue: "acme/api#25", PullRequest: 30}}
	if got := auditor.recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("audited %+v, want only the skip while #30 was open", got)
	}
}

// TestIssueSource_AnUnreadablePullRequestListingAdoptsNothing: the sweep cannot
// tell which issues are already being closed, so it offers none of them and
// reports the forge outage the poller classifies once.
func TestIssueSource_AnUnreadablePullRequestListingAdoptsNothing(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	forge.listOpenPRsErr = &BackendError{Op: "ListOpenPullRequests", Cause: errors.New("connection refused")}

	specs, err := source.Poll(context.Background(), testOrchProject())
	var unavailable *orchestration.TaskSourceUnavailableError
	if !errors.As(err, &unavailable) || specs != nil {
		t.Fatalf("specs=%v err=%v, want the outage and no adoption", offeredRefs(specs), err)
	}
}

// unreadablePullRequests is a forge whose pull-request reads fail, or answer
// nothing, while every other call is the fake's.
type unreadablePullRequests struct {
	*fakeForge
	err error
}

func (f *unreadablePullRequests) GetPullRequest(context.Context, RepoRef, int) (*ForgePullRequest, error) {
	return nil, f.err
}

type staticForge struct{ forge ForgeBackend }

func (s staticForge) ForgeFor(context.Context, string, ForgeConfig) (ForgeBackend, error) {
	return s.forge, nil
}

// TestIssueSource_ADepartedPullRequestIsNotRead: only an open pull request
// holds its issue, so a claimant that leaves the open listing is not read back
// to learn how it left. Whether the forge cannot answer for it, answers
// nothing, or no longer has it, the issue is offered again.
func TestIssueSource_ADepartedPullRequestIsNotRead(t *testing.T) {
	cases := map[string]error{
		"unreachable": &BackendError{Op: "GetPullRequest", Cause: errors.New("timeout")},
		"no answer":   nil,
		"gone":        &NotFoundError{Entity: "pull request", ID: "30"},
	}
	for name, readErr := range cases {
		t.Run(name, func(t *testing.T) {
			store := newTestStore(t)
			if err := store.PutBinding(context.Background(), testTenant, testBinding()); err != nil {
				t.Fatal(err)
			}
			fake := newFakeForge()
			forge := &unreadablePullRequests{fakeForge: fake, err: readErr}
			source, err := NewIssueSource(store, staticForge{forge: forge}, 10)
			if err != nil {
				t.Fatal(err)
			}
			fake.putIssue(25, "Add the thing", "task:ready")
			fake.prs[30] = &ForgePullRequest{Number: 30, Body: "Closes #25", State: "open"}
			ctx := context.Background()
			if specs, err := source.Poll(ctx, testOrchProject()); err != nil || len(specs) != 0 {
				t.Fatalf("while open: specs=%v err=%v", offeredRefs(specs), err)
			}

			fake.prs[30].State = "closed"
			specs, err := source.Poll(ctx, testOrchProject())
			if err != nil || !reflect.DeepEqual(offeredRefs(specs), []string{"acme/api#25"}) {
				t.Fatalf("specs=%v err=%v, want #25 offered", offeredRefs(specs), err)
			}
		})
	}
}

func TestReadiness_AClaimedIssueNamesItsPullRequest(t *testing.T) {
	forge := newFakeForge()
	issue := forge.putIssue(25, "Add the thing", "task:ready")
	scope := ReadinessScope{
		Binding: testBinding(), Forge: forge, Repo: RepoRef{Owner: "acme", Repo: "api"},
		Closing: map[int]ClosingClaim{25: {PullRequest: 1049}},
	}
	finding, err := NewReadinessEvaluator(nil).Evaluate(context.Background(), scope, issue)
	if err != nil {
		t.Fatal(err)
	}
	if finding.Verdict != ReadinessClosedByPullRequest || finding.Claim.PullRequest != 1049 || finding.Verdict.Adoptable() {
		t.Fatalf("finding = %+v, want a non-adoptable claim by #1049", finding)
	}
}

func TestIssueSource_SetAdoptionAuditorIgnoresNil(t *testing.T) {
	source, _, _ := newIssueSourceRig(t)
	auditor := &recordingAdoptionAuditor{}
	source.SetAdoptionAuditor(auditor)
	source.SetAdoptionAuditor(nil)
	if source.auditor != auditor {
		t.Fatal("a nil auditor replaced the one wired")
	}
}
