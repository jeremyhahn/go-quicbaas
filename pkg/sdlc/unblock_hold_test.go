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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestSweep_ARulingOnAHeldIssueDoesNotReadmit is the burn-in defect: the owner
// approved a lead-driven workstream in a comment, the issue kept its hold
// label, and the sweep read the approval as a release and put the task back
// in the factory's queue.
func TestSweep_ARulingOnAHeldIssueDoesNotReadmit(t *testing.T) {
	f := newSweepFixture(t)
	task := f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	number := issueNumber(t, task)
	f.forge.putIssue(number, "lead-driven workstream", HoldLabel)
	f.forge.comment(number, 601, "agent-company", "Approved; delivery stays lead-driven.", parkedAt.Add(time.Minute))

	finding := onlyFinding(t, f.sweep(t))

	if finding.Verdict != UnblockHeldForLead {
		t.Fatalf("verdict is %s (%s), want held_for_lead", finding.Verdict, finding.Evidence)
	}
	if got := f.tasks.readmitted(); len(got) != 0 {
		t.Fatalf("a held task was readmitted: %v", got)
	}
}

// TestSweepProject_ASatisfiedConditionOnAHeldIssueStaysBlocked: the hold wins
// over a condition too, not only over a ruling.
func TestSweepProject_ASatisfiedConditionOnAHeldIssueStaysBlocked(t *testing.T) {
	f := newSweepFixture(t)
	dependency := f.forge.putIssue(900, "the dependency")
	dependency.State = "closed"
	task := f.blocked(mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 900), Target: "closed",
	}, "needs the dependency"))
	f.forge.putIssue(issueNumber(t, task), "held for the lead", HoldLabel)

	finding := onlyFinding(t, f.sweep(t))

	if finding.Verdict != UnblockHeldForLead {
		t.Fatalf("verdict is %s (%s), want held_for_lead", finding.Verdict, finding.Evidence)
	}
	if got := f.tasks.readmitted(); len(got) != 0 {
		t.Fatalf("a held task was readmitted: %v", got)
	}
}

// TestSweepProject_AnUnreadableHoldFailsClosed: when the issue cannot be read,
// the sweep cannot tell whether it is held, so it does not readmit.
func TestSweepProject_AnUnreadableHoldFailsClosed(t *testing.T) {
	f := newSweepFixture(t)
	dependency := f.forge.putIssue(900, "the dependency")
	dependency.State = "closed"
	f.blocked(mustCompose(t, UnblockCondition{
		Kind: UnblockForgeIssueState, Subject: IssueRefString(testRepoRef, 900), Target: "closed",
	}, "needs the dependency"))
	f.forge.mu.Lock()
	f.forge.getIssueErr = errors.New("forge unavailable")
	f.forge.mu.Unlock()

	finding := onlyFinding(t, f.sweep(t))

	if finding.Verdict.Released() {
		t.Fatalf("verdict is %s (%s): an unreadable hold released the task", finding.Verdict, finding.Evidence)
	}
	if got := f.tasks.readmitted(); len(got) != 0 {
		t.Fatalf("readmitted %v with the hold unreadable", got)
	}
}

// TestReadmitOnRuling_AHeldIssueIsNotReadmitted is the webhook's half of the
// same rule: a delivered ruling comment on a held issue releases nothing.
func TestReadmitOnRuling_AHeldIssueIsNotReadmitted(t *testing.T) {
	f := newSweepFixture(t)
	task := f.parkedForRuling(orchestration.TaskBlockKindEscalated)
	number := issueNumber(t, task)
	f.forge.putIssue(number, "lead-driven workstream", HoldLabel)
	f.forge.comment(number, 602, "agent-company", "Approved.", parkedAt.Add(time.Minute))

	readmitted, err := f.sweeper.ReadmitOnRuling(context.Background(), testTenant, testProject, number)
	if err != nil {
		t.Fatalf("ReadmitOnRuling: %v", err)
	}
	if readmitted || len(f.tasks.readmitted()) != 0 {
		t.Fatalf("a held task was readmitted through the webhook path")
	}
}

// TestTaskHeldForLeadError_NamesTheIssueAndUnwraps keeps the typed error
// usable with errors.Is and readable in a sweep report.
func TestTaskHeldForLeadError_NamesTheIssueAndUnwraps(t *testing.T) {
	err := &TaskHeldForLeadError{TaskID: "t1", Issue: 1058, Label: HoldLabel}
	if !errors.Is(err, ErrTaskHeldForLead) {
		t.Fatal("TaskHeldForLeadError does not unwrap to ErrTaskHeldForLead")
	}
	got := err.Error()
	for _, want := range []string{"t1", "#1058", HoldLabel} {
		if !strings.Contains(got, want) {
			t.Fatalf("error %q does not name %q", got, want)
		}
	}
}
