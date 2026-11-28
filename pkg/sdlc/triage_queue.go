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
	"sort"
	"strings"
)

// =============================================================================
// Ready-queue hygiene — open issues the queue cannot rank or pick up
// =============================================================================
//
// These functions are pure: they read issues already listed from the forge
// and decide, without calling it, which open issues are missing what the
// ready queue needs and which low-priority issues should wait.

// QueueGap names one thing an open issue lacks for the ready queue.
type QueueGap string

const (
	// QueueGapUnlabelled: the issue carries no labels at all.
	QueueGapUnlabelled QueueGap = "unlabelled"
	// QueueGapNoMilestone: the issue is in no milestone.
	QueueGapNoMilestone QueueGap = "no-milestone"
	// QueueGapNoTrigger: the issue lacks the binding's trigger label, so the
	// factory never picks it up.
	QueueGapNoTrigger QueueGap = "no-trigger"
)

// QueueHygieneFinding is one open, unclaimed issue and the gaps it has.
type QueueHygieneFinding struct {
	Number int
	Title  string
	Gaps   []QueueGap
}

// QueueHygiene decides which open, unclaimed issues have a queue gap. Closed
// issues and issues carrying any claiming label are skipped: they are not
// waiting in the queue. Each remaining issue with at least one gap is
// reported, its gaps in the order unlabelled, no milestone, no trigger.
// NoTrigger is judged only when triggerLabel is not empty. Labels match
// case-insensitively after trimming. Findings are sorted by issue number.
func QueueHygiene(issues []ForgeIssue, triggerLabel string, claiming []string) []QueueHygieneFinding {
	trigger := normalLabel(triggerLabel)
	claimed := make(map[string]bool, len(claiming))
	for _, label := range claiming {
		if l := normalLabel(label); l != "" {
			claimed[l] = true
		}
	}
	var findings []QueueHygieneFinding
	for i := range issues {
		issue := &issues[i]
		if issueClosed(issue) {
			continue
		}
		hasTrigger, isClaimed := false, false
		for _, label := range issue.Labels {
			l := normalLabel(label)
			if claimed[l] {
				isClaimed = true
				break
			}
			if trigger != "" && l == trigger {
				hasTrigger = true
			}
		}
		if isClaimed {
			continue
		}
		var gaps []QueueGap
		if len(issue.Labels) == 0 {
			gaps = append(gaps, QueueGapUnlabelled)
		}
		if strings.TrimSpace(issue.Milestone) == "" {
			gaps = append(gaps, QueueGapNoMilestone)
		}
		if trigger != "" && !hasTrigger {
			gaps = append(gaps, QueueGapNoTrigger)
		}
		if len(gaps) == 0 {
			continue
		}
		findings = append(findings, QueueHygieneFinding{Number: issue.Number, Title: issue.Title, Gaps: gaps})
	}
	sort.SliceStable(findings, func(a, b int) bool { return findings[a].Number < findings[b].Number })
	return findings
}

// QueueContext is the state of the queue a low-priority issue is held
// against.
type QueueContext struct {
	// StuckPRs is the number of open pull requests the factory cannot land.
	StuckPRs int
	// DueMilestoneWork is true when milestoned work is waiting.
	DueMilestoneWork bool
}

// HoldLowPriority decides whether a low-priority issue waits: true when the
// issue carries priority:low, is in no milestone, and the queue has stuck
// pull requests or milestoned work waiting. Every other issue, and a nil
// one, is not held.
func HoldLowPriority(issue *ForgeIssue, ctx QueueContext) bool {
	if issue == nil || strings.TrimSpace(issue.Milestone) != "" {
		return false
	}
	if ctx.StuckPRs <= 0 && !ctx.DueMilestoneWork {
		return false
	}
	for _, label := range issue.Labels {
		if normalLabel(label) == "priority:low" {
			return true
		}
	}
	return false
}

func normalLabel(label string) string { return strings.ToLower(strings.TrimSpace(label)) }
