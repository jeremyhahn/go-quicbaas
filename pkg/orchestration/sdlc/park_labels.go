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

import "github.com/jeremyhahn/go-quicbaas/pkg/orchestration"

// parkLabel is one forge label that says an issue waits on a person, and the
// park kind its task is filed under.
type parkLabel struct {
	label string
	kind  orchestration.TaskBlockKind
}

// parkLabels are the labels that park an issue for a person, in precedence
// order: an issue carrying two is filed under the first.
//
// THEY ARE THE SET THE ATTENTION INBOX IS ANSWERABLE FOR. Triage writes
// needs-info and escalated, a parked job writes the binding's blocked label,
// and a person writes the portfolio hold. Each one idles the issue until
// somebody rules, so each one must stand behind a parked task the inbox can
// show — and a ruling given through the inbox takes every one of them off,
// because the operator giving it is the person they were waiting for.
func parkLabels(binding *ProjectBinding) []parkLabel {
	candidates := []parkLabel{
		{label: binding.Forge.Label(LabelEscalated), kind: orchestration.TaskBlockKindEscalated},
		{label: binding.Forge.Label(LabelNeedsInfo), kind: orchestration.TaskBlockKindNeedsInfo},
		{label: binding.Forge.Label(LabelBlocked), kind: orchestration.TaskBlockKindBlocked},
		{label: HoldLabel, kind: orchestration.TaskBlockKindBlocked},
	}
	labels := make([]parkLabel, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.label == "" || seen[candidate.label] {
			continue
		}
		seen[candidate.label] = true
		labels = append(labels, candidate)
	}
	return labels
}

// issueParkLabel returns the park label an issue carries, by precedence, and
// false when it carries none.
func issueParkLabel(binding *ProjectBinding, issue *ForgeIssue) (parkLabel, bool) {
	for _, park := range parkLabels(binding) {
		if held := heldBy(issue, park.label); held != "" {
			return parkLabel{label: held, kind: park.kind}, true
		}
	}
	return parkLabel{}, false
}
