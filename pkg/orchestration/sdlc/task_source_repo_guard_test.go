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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

var foreignRepoRef = RepoRef{Owner: "other", Repo: "api"}

// requireSourceRepoMismatch fails unless err is the typed foreign-repo refusal.
func requireSourceRepoMismatch(t *testing.T, err error) {
	t.Helper()
	var mismatch *TaskSourceRepoMismatchError
	if !errors.As(err, &mismatch) || !errors.Is(err, ErrTaskSourceRepoMismatch) {
		t.Fatalf("err = %v, want TaskSourceRepoMismatchError", err)
	}
	if mismatch.Source != foreignRepoRef || mismatch.Binding != testRepoRef {
		t.Fatalf("mismatch details = %+v", mismatch)
	}
}

// TestAttentionService_Resolve_RefusesATaskNamingAForeignRepository proves
// both forge-writing rulings (the comment, the label hand-back) refuse a task
// whose source names a repository the project is not bound to, before the
// forge sees a call.
func TestAttentionService_Resolve_RefusesATaskNamingAForeignRepository(t *testing.T) {
	for name, resolution := range map[string]AttentionResolution{
		"comment":   {Action: AttentionAnswer, Answer: "Use P-256."},
		"hand back": {Action: AttentionReadmit},
	} {
		t.Run(name, func(t *testing.T) {
			task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
			task.Source.Ref = IssueRefString(foreignRepoRef, 77)
			f := newAttentionFixture(t, task)
			f.forge.putIssue(77, "the parked issue", "factory:needs-info")
			forges := &fakeForges{forge: f.forge}
			f.service.forges = forges

			_, err := f.service.Resolve(context.Background(), testTenant, task.ID, resolution)

			requireSourceRepoMismatch(t, err)
			if got := forges.resolvedTokenRefs(); len(got) != 0 {
				t.Errorf("forge credentials resolved for a foreign repo: %v", got)
			}
			if len(f.forge.comments[77]) != 0 {
				t.Errorf("a comment reached the forge: %v", f.forge.comments[77])
			}
			if labels := f.forge.issueLabels(77); !slices.Equal(labels, []string{"factory:needs-info"}) {
				t.Errorf("labels changed on a foreign repo's task: %v", labels)
			}
			if calls := f.tasks.callLog(); len(calls) != 0 {
				t.Errorf("call log = %v, want the task untouched", calls)
			}
		})
	}
}

// TestAttentionService_Resolve_ProceedsForTheBoundRepository is the matching
// half: the same rulings reach the forge when the source names the binding's
// repository.
func TestAttentionService_Resolve_ProceedsForTheBoundRepository(t *testing.T) {
	task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)
	f.forge.putIssue(77, "the parked issue", "factory:needs-info")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID,
		AttentionResolution{Action: AttentionAnswer, Answer: "Use P-256."}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(f.forge.comments[77]) == 0 {
		t.Error("the ruling comment never reached the forge")
	}
}

// TestLazyScope_Resolve_RefusesAForeignRepository proves the unblock sweep
// neither resolves forge credentials nor builds a scope for a task naming a
// repository other than the binding's, including after the scope has cached
// the binding for an earlier, matching task.
func TestLazyScope_Resolve_RefusesAForeignRepository(t *testing.T) {
	f := newSweepFixture(t)
	forges, ok := f.sweeper.forges.(*fakeForges)
	if !ok {
		t.Fatalf("sweeper forges are %T, want *fakeForges", f.sweeper.forges)
	}
	foreign := testTaskRecord(41)
	foreign.Source.Ref = IssueRefString(foreignRepoRef, 41)

	fresh := &lazyScope{sweeper: f.sweeper, tenantID: testTenant, projectID: testProject}
	_, err := fresh.resolve(context.Background(), foreign)
	requireSourceRepoMismatch(t, err)
	if got := forges.resolvedTokenRefs(); len(got) != 0 {
		t.Fatalf("forge credentials resolved for a foreign repo: %v", got)
	}

	scope, err := fresh.resolve(context.Background(), testTaskRecord(42))
	if err != nil {
		t.Fatalf("matching repo: %v", err)
	}
	if scope.repo != testRepoRef || scope.forge == nil {
		t.Fatalf("scope = %+v", scope)
	}
	_, err = fresh.resolve(context.Background(), foreign)
	requireSourceRepoMismatch(t, err)
}
