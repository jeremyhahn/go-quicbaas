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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Parked issues
// =============================================================================
//
// AN ISSUE THAT WAITS ON A PERSON MUST STAND BEHIND A PARKED TASK, because the
// attention inbox is built from parked tasks and nothing else. Triage labels
// an issue needs-info or escalated WITHOUT adopting it — it was never ready,
// so no task exists — and an issue whose task completed, failed or was deleted
// keeps whatever park label it carried. Either way the one record of the
// question was a forge label. Measured on the burn-in node, 2026-09-25: five
// active projects idle with pending=0, eighteen labelled issues waiting on the
// owner, and one of them in the inbox.
//
// So the source offers every park-labelled issue as a task ADOPTED PARKED
// (TaskSourceSpec.Park — one write, never dispatchable in between), carrying
// the question triage last asked. The engine's source reservation is what
// keeps this idempotent: an issue whose task is still live is "already
// adopted" and nothing is created, and an issue whose task has ended gets a
// fresh parked one on the next sweep. That makes the interval sweep both the
// path for a new verdict and the backfill for every issue labelled before this
// existed.

// parkedSpecs offers a parked task for every open issue carrying a park label
// (see parkLabels) that the triggered listing did not already cover.
//
// A TRIGGERED ISSUE IS LEFT TO THE TRIGGERED LISTING, which already parks it
// through the readiness gate when it cannot run; offering it twice would only
// race the two parks for one reservation. A cluster member is left alone for
// the reason Poll leaves it: its work is its anchor's task.
func (s *IssueSource) parkedSpecs(
	ctx context.Context,
	project *orchestration.Project,
	binding *ProjectBinding,
	forge ForgeBackend,
	inputs PriorityInputs,
	triggered map[int]bool,
) ([]orchestration.TaskSourceSpec, error) {
	repo := binding.Repo()
	seen := map[int]bool{}
	var specs []orchestration.TaskSourceSpec
	for _, park := range parkLabels(binding) {
		issues, err := forge.ListIssues(ctx, repo, IssueFilter{State: "open", Labels: []string{park.label}, Limit: s.pageLimit})
		if err != nil {
			return nil, unavailable(err)
		}
		for i := range issues {
			issue := &issues[i]
			if seen[issue.Number] || triggered[issue.Number] || issueTriggered(binding, issue) || issueClosed(issue) {
				continue
			}
			seen[issue.Number] = true
			spec, ok, err := s.parkedSpec(ctx, project, binding, forge, inputs, issue)
			if err != nil {
				return nil, err
			}
			if ok {
				specs = append(specs, spec)
			}
		}
	}
	return specs, nil
}

// parkedSpec renders one park-labelled issue as a spec adopted parked, and
// false for a cluster member.
func (s *IssueSource) parkedSpec(
	ctx context.Context,
	project *orchestration.Project,
	binding *ProjectBinding,
	forge ForgeBackend,
	inputs PriorityInputs,
	issue *ForgeIssue,
) (orchestration.TaskSourceSpec, bool, error) {
	cluster, err := s.clusterOf(ctx, project, issue.Number)
	if err != nil {
		return orchestration.TaskSourceSpec{}, false, err
	}
	if cluster != nil && cluster.Anchor != issue.Number {
		return orchestration.TaskSourceSpec{}, false, nil
	}
	park, ok := issueParkLabel(binding, issue)
	if !ok {
		return orchestration.TaskSourceSpec{}, false, nil
	}
	repo := binding.Repo()
	comments, err := forge.ListIssueComments(ctx, repo, issue.Number)
	if err != nil {
		return orchestration.TaskSourceSpec{}, false, unavailable(err)
	}
	contract, err := IssueContract(IssueContractSpec{
		Repo: repo, IssueNumber: issue.Number, Title: issue.Title, Body: issue.Body, Comments: comments,
	})
	if err != nil {
		return orchestration.TaskSourceSpec{}, false, err
	}
	reason, err := ComposeBlockedReason(UnblockCondition{Kind: UnblockOwnerRuling}, parkedIssueQuestion(park, comments))
	if err != nil {
		return orchestration.TaskSourceSpec{}, false, err
	}
	return orchestration.TaskSourceSpec{
		Source: orchestration.TaskSourceRef{
			Kind: TaskSourceKindForgeIssue,
			Ref:  IssueRefString(repo, issue.Number),
			URL:  issue.URL,
		},
		Subject:     strings.TrimSpace(issue.Title),
		Description: contract,
		Priority:    inputs.ComputedFor(issue.Labels),
		Runner:      JobRunnerName,
		Park:        &orchestration.BlockReason{Kind: park.kind, Message: reason},
		Metadata:    issueSpecMetadata(repo, binding, issue.Number, nil),
	}, true, nil
}

// parkedIssueQuestion is what the parked task asks the operator: triage's
// last word on the issue under the heading the inbox reads as the question
// (ExtractAsk), or — for an issue triage never spoke on, a person's hold — a
// sentence naming the label that parks it.
//
// The machine markers are stripped: they are addressed to the factory, and an
// unblock marker in a park's detail is refused outright (ComposeBlockedReason)
// because it would inject a condition.
func parkedIssueQuestion(park parkLabel, comments []ForgeComment) string {
	if verdict, ok := lastTriageComment(comments); ok {
		if text := stripMachineMarkers(verdict.Body); text != "" {
			return "### What I need from you\n\n" + text
		}
	}
	return "The issue carries " + strconv.Quote(park.label) +
		" and records no question from triage; it waits on the person that label names."
}

// stripMachineMarkers drops every line that carries a factory, triage or
// unblock marker.
func stripMachineMarkers(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, factoryCommentMarker) || strings.Contains(line, triageCommentMarker) ||
			strings.Contains(line, unblockMarkerOpen) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
