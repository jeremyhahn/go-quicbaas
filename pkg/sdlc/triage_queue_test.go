// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"reflect"
	"testing"
)

const testTrigger = "sdlc:ready"

var testClaiming = []string{"sdlc:in-progress", "sdlc:in-review"}

// TestQueueHygiene_EachGap: each gap is reported alone, and all three together.
func TestQueueHygiene_EachGap(t *testing.T) {
	cases := []struct {
		name  string
		issue ForgeIssue
		want  []QueueGap
	}{
		{"no milestone", ForgeIssue{Number: 1, State: "open", Labels: []string{testTrigger}}, []QueueGap{QueueGapNoMilestone}},
		{"no trigger", ForgeIssue{Number: 2, State: "open", Labels: []string{"bug"}, Milestone: "v1"}, []QueueGap{QueueGapNoTrigger}},
		{"unlabelled", ForgeIssue{Number: 3, State: "open", Milestone: "v1"}, []QueueGap{QueueGapUnlabelled, QueueGapNoTrigger}},
		{"all three", ForgeIssue{Number: 4, State: "open"}, []QueueGap{QueueGapUnlabelled, QueueGapNoMilestone, QueueGapNoTrigger}},
		{"none", ForgeIssue{Number: 5, State: "open", Labels: []string{" SDLC:Ready "}, Milestone: "v1"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := QueueHygiene([]ForgeIssue{tc.issue}, testTrigger, testClaiming)
			if tc.want == nil {
				if len(got) != 0 {
					t.Fatalf("findings = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0].Number != tc.issue.Number || !reflect.DeepEqual(got[0].Gaps, tc.want) {
				t.Fatalf("findings = %+v, want one for #%d with %v", got, tc.issue.Number, tc.want)
			}
		})
	}
}

// TestQueueHygiene_UnlabelledAlone: with no trigger label configured, an
// unlabelled, milestoned issue has the unlabelled gap only.
func TestQueueHygiene_UnlabelledAlone(t *testing.T) {
	got := QueueHygiene([]ForgeIssue{{Number: 9, State: "open", Milestone: "v1"}}, "", nil)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Gaps, []QueueGap{QueueGapUnlabelled}) {
		t.Fatalf("findings = %+v, want #9 unlabelled only", got)
	}
}

// TestQueueHygiene_SkipsClaimedAndClosed: claimed and closed issues are not
// waiting in the queue and are never reported.
func TestQueueHygiene_SkipsClaimedAndClosed(t *testing.T) {
	issues := []ForgeIssue{
		{Number: 1, State: "closed"},
		{Number: 2, State: "CLOSED", Labels: []string{"bug"}},
		{Number: 3, State: "open", Labels: []string{"sdlc:in-progress"}},
		{Number: 4, State: "open", Labels: []string{" SDLC:In-Review "}},
		{Number: 5, State: "open"},
	}
	got := QueueHygiene(issues, testTrigger, testClaiming)
	if len(got) != 1 || got[0].Number != 5 {
		t.Fatalf("findings = %+v, want only #5", got)
	}
}

// TestQueueHygiene_OrderIsDeterministic: findings sort by issue number
// whatever order the forge listed them in, and carry the title.
func TestQueueHygiene_OrderIsDeterministic(t *testing.T) {
	issues := []ForgeIssue{
		{Number: 30, Title: "c", State: "open"},
		{Number: 10, Title: "a", State: "open"},
		{Number: 20, Title: "b", State: "open"},
	}
	got := QueueHygiene(issues, testTrigger, testClaiming)
	var numbers []int
	for _, f := range got {
		numbers = append(numbers, f.Number)
	}
	if !reflect.DeepEqual(numbers, []int{10, 20, 30}) || got[0].Title != "a" {
		t.Fatalf("findings = %+v, want #10, #20, #30 in order", got)
	}
	if issues[0].Number != 30 {
		t.Fatal("QueueHygiene reordered its input")
	}
}

// TestHoldLowPriority_True: a priority:low issue with no milestone waits
// while pull requests are stuck or milestoned work is due.
func TestHoldLowPriority_True(t *testing.T) {
	low := &ForgeIssue{Labels: []string{" Priority:Low "}}
	for _, ctx := range []QueueContext{
		{StuckPRs: 1},
		{DueMilestoneWork: true},
		{StuckPRs: 3, DueMilestoneWork: true},
	} {
		if !HoldLowPriority(low, ctx) {
			t.Fatalf("HoldLowPriority(low, %+v) = false, want true", ctx)
		}
	}
}

// TestHoldLowPriority_False: every other issue or queue state is not held.
func TestHoldLowPriority_False(t *testing.T) {
	busy := QueueContext{StuckPRs: 2, DueMilestoneWork: true}
	cases := []struct {
		name  string
		issue *ForgeIssue
		ctx   QueueContext
	}{
		{"nil issue", nil, busy},
		{"not low", &ForgeIssue{Labels: []string{"priority:high"}}, busy},
		{"unlabelled", &ForgeIssue{}, busy},
		{"low with milestone", &ForgeIssue{Labels: []string{"priority:low"}, Milestone: "v1"}, busy},
		{"low, quiet queue", &ForgeIssue{Labels: []string{"priority:low"}}, QueueContext{}},
		{"low, negative stuck count", &ForgeIssue{Labels: []string{"priority:low"}}, QueueContext{StuckPRs: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if HoldLowPriority(tc.issue, tc.ctx) {
				t.Fatalf("HoldLowPriority = true, want false")
			}
		})
	}
}
