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
	"fmt"
	"sort"
	"strings"
)

// =============================================================================
// Label read-back
// =============================================================================
//
// Every adapter verifies a label write by reading the issue back, because a
// forge's 200 for a label write is not proof it took effect. The checks below
// are the forge-neutral half of that: given what was asked and what the read
// returned, is the write applied.

// normalizedLabels renders a label set in a comparable form.
//
// Sorted and lowercased because neither order nor case carries meaning: a
// forge returns labels in its own order, and comparing raw slices would report
// a difference where there is none — a read-back that cries wolf gets ignored,
// which costs more than having no read-back at all.
func normalizedLabels(labels []string) string {
	if len(labels) == 0 {
		return "[]"
	}
	normalized := make([]string, 0, len(labels))
	for _, label := range labels {
		normalized = append(normalized, normalizedLabel(label))
	}
	sort.Strings(normalized)
	return "[" + strings.Join(normalized, " ") + "]"
}

// normalizedLabel is one label in the form normalizedLabels compares.
func normalizedLabel(label string) string {
	return strings.ToLower(strings.TrimSpace(label))
}

// verifyLabelsSet checks a replace: the issue carries exactly want.
func verifyLabelsSet(forge ForgeType, repo RepoRef, number int, want, got []string) error {
	wanted, held := normalizedLabels(want), normalizedLabels(got)
	if wanted == held {
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type:      forge,
		Operation: "SetIssueLabels",
		Subject:   fmt.Sprintf("%s#%d", repo, number),
		Want:      wanted,
		Got:       held,
	}
}

// verifyLabelsAdded checks an add: every label in added is PRESENT. The set is
// not compared, because labels the call did not name are supposed to still be
// there.
func verifyLabelsAdded(forge ForgeType, repo RepoRef, number int, added, got []string) error {
	present := make(map[string]bool, len(got))
	for _, label := range got {
		present[normalizedLabel(label)] = true
	}
	var missing []string
	for _, label := range added {
		if !present[normalizedLabel(label)] {
			missing = append(missing, label)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type:      forge,
		Operation: "AddIssueLabels",
		Subject:   fmt.Sprintf("%s#%d", repo, number),
		Want:      normalizedLabels(added),
		Got:       normalizedLabels(got) + " (missing " + normalizedLabels(missing) + ")",
	}
}

// verifyLabelRemoved checks a removal: label is no longer on the issue.
func verifyLabelRemoved(forge ForgeType, repo RepoRef, number int, label string, got []string) error {
	wanted := normalizedLabel(label)
	for _, held := range got {
		if normalizedLabel(held) == wanted {
			return &ForgeWriteNotAppliedError{
				Type:      forge,
				Operation: "RemoveIssueLabel",
				Subject:   fmt.Sprintf("%s#%d", repo, number),
				Want:      "without " + wanted,
				Got:       normalizedLabels(got),
			}
		}
	}
	return nil
}

// canonicalLabelNames maps label names onto the repository's own spelling of
// them, refusing a name the repository does not define.
//
// A forge whose label routes take names but CREATE a label they do not
// recognise checks its vocabulary through this first: it is what keeps a
// typo in a project's label vocabulary from becoming a permanent label on the
// repository.
func canonicalLabelNames(repo RepoRef, defined, names []string) ([]string, error) {
	byName := make(map[string]string, len(defined))
	for _, label := range defined {
		byName[strings.ToLower(label)] = label
	}

	resolved := make([]string, 0, len(names))
	for _, name := range names {
		canonical, ok := byName[strings.ToLower(name)]
		if !ok {
			return nil, &InvalidInputError{
				Field:   "labels",
				Message: "repository " + repo.String() + " defines no label named " + name,
			}
		}
		resolved = append(resolved, canonical)
	}
	return resolved, nil
}
