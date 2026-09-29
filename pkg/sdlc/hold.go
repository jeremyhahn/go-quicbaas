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

import "strings"

// HoldLabel is the portfolio's work-queue hold: handbook labels.md §3,
// "blocked; the blocker is recorded on the issue". Only the party named on the
// issue clears it.
//
// A HOLD IS ABSOLUTE, NOT ADVISORY. Nothing in this module is that party: a
// triage verdict is a model's opinion about the work, and a task source is a
// poller. Measured on go-quicbaas#1058, 2026-09-23: an issue filed
// `task:blocked` as lead-driven work was judged ready by the triage backstop,
// relabelled `task:ready`, adopted, and refused by the worker — an attempt
// charged for work nobody had handed over. So triage never selects a held
// issue, its verdict writer re-reads the issue and refuses one, and adoption
// refuses one whatever its other labels say.
//
// It is a portfolio constant rather than a binding field because it is not
// the module's display vocabulary: the module never writes it, and a binding
// that could rename it could also switch the hold off.
const HoldLabel = "task:blocked"

// issueHeld reports whether an issue carries the portfolio hold.
//
// Compared case-insensitively and trimmed, unlike the trigger: a mistake in
// either direction here must fall on the side of leaving the issue alone.
func issueHeld(issue *ForgeIssue) bool {
	return heldBy(issue, HoldLabel) != ""
}

// triageHold names the label that puts an issue out of triage's reach, or ""
// when there is none: the portfolio hold, or the binding's own
// parked-for-a-person or escalated label.
//
// ESCALATED IS A HOLD TOO (#1060). It says a person is the next step, and a
// new comment or a merge is not that person clearing it: before this, an
// escalated issue was re-judged on either and could be promoted straight to
// the trigger. It comes back to triage when a person takes the label off.
func triageHold(binding *ProjectBinding, issue *ForgeIssue) string {
	if label := heldBy(issue, HoldLabel); label != "" {
		return label
	}
	if binding == nil {
		return ""
	}
	for _, key := range triageHoldKeys {
		if label := heldBy(issue, binding.Forge.Label(key)); label != "" {
			return label
		}
	}
	return ""
}

// triageHoldKeys are the binding label keys that hold an issue against triage.
var triageHoldKeys = []string{LabelBlocked, LabelEscalated}

// heldBy returns the issue's label matching hold, or "" when it carries none.
func heldBy(issue *ForgeIssue, hold string) string {
	hold = strings.TrimSpace(hold)
	if issue == nil || hold == "" {
		return ""
	}
	for _, label := range issue.Labels {
		if strings.EqualFold(strings.TrimSpace(label), hold) {
			return label
		}
	}
	return ""
}
