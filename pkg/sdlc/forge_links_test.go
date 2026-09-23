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
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The forge's own issue-to-pull-request link (go-quicbaas#1054)
// =============================================================================

// linkingForge is the fake forge with a forge-side link record: which pull
// requests the forge links to each issue as closing it.
type linkingForge struct {
	*fakeForge
	mu    sync.Mutex
	links map[int][]ForgeIssueLink
	err   error
}

func (f *linkingForge) ListClosingPullRequests(_ context.Context, _ RepoRef, issue int) ([]ForgeIssueLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]ForgeIssueLink(nil), f.links[issue]...), nil
}

func (f *linkingForge) link(issue int, links ...ForgeIssueLink) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[issue] = links
}

func newLinkingSource(t *testing.T) (*IssueSource, *linkingForge, *recordingAdoptionAuditor) {
	t.Helper()
	store := newTestStore(t)
	if err := store.PutBinding(context.Background(), testTenant, testBinding()); err != nil {
		t.Fatal(err)
	}
	forge := &linkingForge{fakeForge: newFakeForge(), links: map[int][]ForgeIssueLink{}}
	source, err := NewIssueSource(store, staticForge{forge: forge}, 10)
	if err != nil {
		t.Fatal(err)
	}
	auditor := &recordingAdoptionAuditor{}
	source.SetAdoptionAuditor(auditor)
	return source, forge, auditor
}

// TestIssueSource_AnIssueTheForgeLinksToAnOpenPullRequestIsNotAdopted: the
// pull request's text says nothing, but the forge records the link. The issue
// is skipped and audited with the pull request's number, then offered again
// once that pull request closes without merging.
func TestIssueSource_AnIssueTheForgeLinksToAnOpenPullRequestIsNotAdopted(t *testing.T) {
	source, forge, auditor := newLinkingSource(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	forge.prs[40] = &ForgePullRequest{Number: 40, Title: "unrelated words", Body: "no keyword here", State: "open"}
	forge.link(25, ForgeIssueLink{PullRequest: 40, Open: true})
	ctx := context.Background()
	project := testOrchProject()

	for sweep := 1; sweep <= 2; sweep++ {
		specs, err := source.Poll(ctx, project)
		if err != nil || len(specs) != 0 {
			t.Fatalf("sweep %d: specs=%v err=%v, want the linked issue skipped", sweep, offeredRefs(specs), err)
		}
	}
	want := []AdoptionSkip{{TenantID: project.TenantID, ProjectID: project.ID, Issue: "acme/api#25", PullRequest: 40}}
	if got := auditor.recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("audited %+v, want the one skip naming the linked pull request", got)
	}

	forge.prs[40].State = "closed"
	forge.link(25, ForgeIssueLink{PullRequest: 40})
	specs, err := source.Poll(ctx, project)
	if err != nil || !reflect.DeepEqual(offeredRefs(specs), []string{"acme/api#25"}) {
		t.Fatalf("specs=%v err=%v, want #25 offered once its linked pull request closed unmerged", offeredRefs(specs), err)
	}
}

// TestIssueSource_AReopenedIssueTheForgeLinksToAMergedPullRequestIsAdopted:
// the forge's link outlives the merge, so a reopened issue still carries it.
// A merged pull request is history and holds nothing; only an open one would.
func TestIssueSource_AReopenedIssueTheForgeLinksToAMergedPullRequestIsAdopted(t *testing.T) {
	source, forge, auditor := newLinkingSource(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	forge.link(25, ForgeIssueLink{PullRequest: 41}, ForgeIssueLink{PullRequest: 40, Merged: true})

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil || !reflect.DeepEqual(offeredRefs(specs), []string{"acme/api#25"}) {
		t.Fatalf("specs=%v err=%v, want the reopened issue offered despite its merged link", offeredRefs(specs), err)
	}
	if skips := auditor.recorded(); len(skips) != 0 {
		t.Fatalf("audited %+v, want no skip for a merged link", skips)
	}
}

// TestIssueSource_AnUnreadableForgeLinkAdoptsNothing: a link read the forge
// cannot answer is an outage, not "nothing links this issue".
func TestIssueSource_AnUnreadableForgeLinkAdoptsNothing(t *testing.T) {
	source, forge, _ := newLinkingSource(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	forge.err = &BackendError{Op: "ListClosingPullRequests", Cause: errors.New("timeout")}

	specs, err := source.Poll(context.Background(), testOrchProject())
	var unavailable *orchestration.TaskSourceUnavailableError
	if !errors.As(err, &unavailable) || specs != nil {
		t.Fatalf("specs=%v err=%v, want the outage and no adoption", offeredRefs(specs), err)
	}
}

func TestClaimFromLinks_OnlyAnOpenPullRequestClaims(t *testing.T) {
	cases := []struct {
		name  string
		links []ForgeIssueLink
		want  ClosingClaim
		ok    bool
	}{
		{"none", nil, ClosingClaim{}, false},
		{"closed unmerged", []ForgeIssueLink{{PullRequest: 3}}, ClosingClaim{}, false},
		{"merged", []ForgeIssueLink{{PullRequest: 9, Merged: true}, {PullRequest: 7, Merged: true}}, ClosingClaim{}, false},
		{"lowest open, merged ignored", []ForgeIssueLink{{PullRequest: 2, Merged: true}, {PullRequest: 12, Open: true}, {PullRequest: 11, Open: true}}, ClosingClaim{PullRequest: 11}, true},
	}
	for _, tc := range cases {
		got, ok := claimFromLinks(tc.links)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got %+v %t, want %+v %t", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// TestGitea_ListClosingPullRequests_ReadsTheIssueTimeline pins the adapter
// against Gitea's timeline shape: only a "pull_ref" whose action is "closes"
// and whose ref_issue is a pull request of this repository is a link. A
// mention, a neutered reference and another repository's pull request are
// not.
func TestGitea_ListClosingPullRequests_ReadsTheIssueTimeline(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	ref := func(number int, state string, merged bool, owner, name string) map[string]any {
		return map[string]any{
			"number": number, "state": state,
			"pull_request": map[string]any{"merged": merged},
			"repository":   map[string]any{"owner": owner, "name": name},
		}
	}
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/25/timeline", http.StatusOK, []map[string]any{
		{"type": "comment", "body": "hello"},
		{"type": "pull_ref", "ref_action": "closes", "ref_issue": ref(40, "open", false, "jeremyhahn", "go-quicbaas")},
		{"type": "pull_ref", "ref_action": "closes", "ref_issue": ref(38, "closed", true, "JeremyHahn", "Go-QuicBaaS")},
		{"type": "pull_ref", "ref_action": "closes", "ref_issue": ref(37, "closed", false, "jeremyhahn", "go-quicbaas")},
		{"type": "pull_ref", "ref_action": "none", "ref_issue": ref(36, "open", false, "jeremyhahn", "go-quicbaas")},
		{"type": "pull_ref", "ref_action": "neutered", "ref_issue": ref(35, "open", false, "jeremyhahn", "go-quicbaas")},
		{"type": "pull_ref", "ref_action": "closes", "ref_issue": ref(34, "open", false, "someone", "else")},
		{"type": "issue_ref", "ref_action": "closes", "ref_issue": map[string]any{"number": 33, "state": "open"}},
	})

	links, err := backend.ListClosingPullRequests(context.Background(), testRepo, 25)
	if err != nil {
		t.Fatalf("ListClosingPullRequests: %v", err)
	}
	want := []ForgeIssueLink{
		{PullRequest: 40, Open: true},
		{PullRequest: 38, Merged: true},
		{PullRequest: 37},
	}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("links = %+v, want %+v", links, want)
	}
}

func TestGitea_ListClosingPullRequests_AFailedReadIsAFault(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/25/timeline", http.StatusForbidden, map[string]any{"message": "no"})

	if _, err := backend.ListClosingPullRequests(context.Background(), testRepo, 25); err == nil {
		t.Fatal("a refused timeline read reported no links instead of failing")
	}
}
