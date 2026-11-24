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
)

// =============================================================================
// The descent, beyond the first hop
// =============================================================================
//
// THE WALK ASKS THE FORGE TWICE PER UPSTREAM and the two questions are different:
// `satisfied` asks whether the upstream is CLOSED, and `follow` then reads the
// same issue's body and comments to learn what IT is waiting for. A failure of
// the first is already covered; a failure of the SECOND is a separate path, and
// it is the one a transient outage between two calls produces. Every test here
// answers the first question and fails the second, which no globally-failing
// double can express.
//
// WHY IT MATTERS THAT THESE ARE UNDECIDABLE AND NOT SATISFIED. An unread chain is
// a chain whose blockers are unknown, and the permissive reading — "nothing came
// back, so nothing is blocking" — releases work against a dependency nobody
// checked. That is the fail-open failure the readmission vocabulary refuses by
// name, one layer down.

// flakyForge answers a bounded number of questions about each issue and then
// stops, so a test can let the state check through and fail the descent that
// follows it.
type flakyForge struct {
	*fakeForge

	// getIssueBudget is how many GetIssue calls each issue number answers
	// before the forge starts failing. An issue absent from the map is
	// unlimited.
	getIssueBudget map[int]int

	// listCommentsBudget is how many ListIssueComments calls are answered in
	// total; zero is unlimited. The walk's first call is the ROOT issue's, made
	// by walkDependencies, so a budget of one fails the first descent.
	listCommentsBudget int
	listCommentsCalls  int

	fault error
}

func newFlakyForge(base *fakeForge) *flakyForge {
	return &flakyForge{
		fakeForge:      base,
		getIssueBudget: map[int]int{},
		fault:          errors.New("gateway timeout"),
	}
}

func (f *flakyForge) GetIssue(ctx context.Context, repo RepoRef, number int) (*ForgeIssue, error) {
	if budget, bounded := f.getIssueBudget[number]; bounded {
		if budget <= 0 {
			return nil, f.fault
		}
		f.getIssueBudget[number] = budget - 1
	}
	return f.fakeForge.GetIssue(ctx, repo, number)
}

func (f *flakyForge) ListIssueComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error) {
	f.listCommentsCalls++
	if f.listCommentsBudget > 0 && f.listCommentsCalls > f.listCommentsBudget {
		return nil, f.fault
	}
	return f.fakeForge.ListIssueComments(ctx, repo, number)
}

// flakyScope is the rig's scope with the flaky forge substituted in.
func flakyScope(rig *readinessRig, forge ForgeBackend) ReadinessScope {
	scope := rig.scope()
	scope.Forge = forge
	return scope
}

// TestReadinessDescent_AnUpstreamThatAnswersItsStateButNotItsBodyIsUndecidable:
// the upstream is open — so it blocks — and the read of what IT waits for
// failed. The chain's blockers are unknown, and unknown is not satisfied.
func TestReadinessDescent_AnUpstreamThatAnswersItsStateButNotItsBodyIsUndecidable(t *testing.T) {
	t.Parallel()

	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	rig.forge.putIssue(2, "upstream")

	forge := newFlakyForge(rig.forge)
	// One answer for #2: the state check. The descent's own read fails.
	forge.getIssueBudget[2] = 1

	finding, err := rig.evaluator.Evaluate(context.Background(), flakyScope(rig, forge), root)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if finding.Verdict != ReadinessUndecidable {
		t.Fatalf("verdict = %s, want undecidable (%q)", finding.Verdict, finding.Evidence)
	}
}

// TestReadinessDescent_AnUpstreamDiscussionFailingAtDepthIsAnOutage: the
// upstream's COMMENTS are read during the descent, because a dependency
// discovered after filing is written there. A backend failure reading them is the
// forge being down, which is returned rather than recorded per issue.
func TestReadinessDescent_AnUpstreamDiscussionFailingAtDepthIsAnOutage(t *testing.T) {
	t.Parallel()

	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	rig.forge.putIssue(2, "upstream")

	forge := newFlakyForge(rig.forge)
	forge.fault = backendError("ListIssueComments", errors.New("503"))
	// One answer: the root issue's own comments, read by walkDependencies. The
	// descent's read of #2's discussion fails.
	forge.listCommentsBudget = 1

	if _, err := rig.evaluator.Evaluate(context.Background(), flakyScope(rig, forge), root); !errors.Is(err, ErrBackend) {
		t.Fatalf("a backend outage during the descent must still be returned as one: %v", err)
	}
}

// TestReadinessDescent_AFailureTwoHopsUpIsUndecidable: A waits on B waits on C,
// and C will not say whether it is closed. Nothing about A is decidable, and the
// permissive reading would start A against a chain nobody finished reading.
func TestReadinessDescent_AFailureTwoHopsUpIsUndecidable(t *testing.T) {
	t.Parallel()

	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	second := rig.forge.putIssue(2, "middle")
	second.Body = "Blocked by: #3\n"
	rig.forge.putIssue(3, "far upstream")

	forge := newFlakyForge(rig.forge)
	// #3 answers nothing at all, so the failure is in the SECOND level's own
	// satisfaction check rather than in the first level's.
	forge.getIssueBudget[3] = 0

	finding, err := rig.evaluator.Evaluate(context.Background(), flakyScope(rig, forge), root)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if finding.Verdict != ReadinessUndecidable {
		t.Fatalf("verdict = %s, want undecidable (%q)", finding.Verdict, finding.Evidence)
	}
}

// TestReadinessDescent_ASatisfiedUpstreamTwoHopsUpIsNotDescendedInto: a CLOSED
// issue is done, so nothing it once waited for can block anybody — and following
// it would turn every finished chain in a repository's history into forge traffic
// on every sweep. Proved at the second level, where the descent's own loop makes
// the decision.
func TestReadinessDescent_ASatisfiedUpstreamTwoHopsUpIsNotDescendedInto(t *testing.T) {
	t.Parallel()

	rig := newReadinessRig(t)
	root := rig.issue(1, "Blocked by: #2\n")
	second := rig.forge.putIssue(2, "middle")
	second.Body = "Blocked by: #3\n"
	closed := rig.forge.putIssue(3, "already done")
	closed.State = "closed"
	// #4 is what #3 would lead to if a closed issue were descended into, and it
	// is deliberately absent from the forge: reaching it at all would fail the
	// read, so the verdict itself proves the descent stopped.
	closed.Body = "Blocked by: #4\n"

	forge := newFlakyForge(rig.forge)

	finding, err := rig.evaluator.Evaluate(context.Background(), flakyScope(rig, forge), root)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if finding.Verdict != ReadinessPark {
		t.Fatalf("verdict = %s, want park on the open upstream #2 (%q)", finding.Verdict, finding.Evidence)
	}
}
